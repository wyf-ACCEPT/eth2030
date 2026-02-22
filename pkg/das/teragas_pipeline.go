// teragas_pipeline.go implements a teragas data pipeline: L2 producers ->
// bandwidth enforcement -> P2P delivery. Addresses gap #34 (Teragas L2).
package das

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrTPPipelineStopped   = errors.New("teragas: pipeline stopped")
	ErrTPNilData           = errors.New("teragas: nil or empty data")
	ErrTPInvalidConfig     = errors.New("teragas: invalid configuration")
	ErrTPBandwidthDenied   = errors.New("teragas: bandwidth denied")
	ErrTPCompressionFailed = errors.New("teragas: compression failed")
	ErrTPReassemblyFailed  = errors.New("teragas: reassembly incomplete")
)

// DataProducer submits data with backpressure awareness.
type DataProducer interface {
	Submit(chainID uint64, data []byte) error
}

// DataConsumer receives processed data.
type DataConsumer interface {
	Receive(ctx context.Context) (*TPDataPacket, error)
}

// PipelineStage is a composable processing stage.
type PipelineStage interface {
	Process(*TPDataPacket) (*TPDataPacket, error)
	Name() string
}

// TPDataPacket flows through the teragas pipeline.
type TPDataPacket struct {
	ChainID     uint64
	Data        []byte
	ChunkIndex  int
	TotalChunks int
	IsChunked   bool
	Compressed  bool
	OrigSize    int
	Timestamp   time.Time
}

// DropPolicy controls behavior when the backpressure channel is full.
type DropPolicy int

const (
	DropOldest  DropPolicy = iota // remove oldest item
	BlockOnFull                   // block sender
)

// TPConfig configures the teragas pipeline.
type TPConfig struct {
	MaxChunkSize       int
	ChannelBufferSize  int
	ConsumerTimeout    time.Duration
	Policy             DropPolicy
	CompressionEnabled bool
	ChunkingEnabled    bool
	BandwidthEnforcer  *BandwidthEnforcer
}

// DefaultTPConfig returns sensible defaults.
func DefaultTPConfig() *TPConfig {
	return &TPConfig{
		MaxChunkSize: 64 * 1024, ChannelBufferSize: 256,
		ConsumerTimeout: 5 * time.Second, Policy: BlockOnFull,
		CompressionEnabled: true, ChunkingEnabled: true,
	}
}

// TPMetricsSnapshot captures pipeline throughput and health metrics.
type TPMetricsSnapshot struct {
	BytesIn, BytesOut, PacketsIn, PacketsOut uint64
	PacketsDropped, BackpressureEvents       uint64
	CompressionSaved, StageErrors            uint64
	LatencySumMs, LatencyCount               int64
}

func (m *TPMetricsSnapshot) AvgLatencyMs() float64 {
	if m.LatencyCount == 0 {
		return 0
	}
	return float64(m.LatencySumMs) / float64(m.LatencyCount)
}

func (m *TPMetricsSnapshot) ThroughputBps(dur time.Duration) float64 {
	if dur <= 0 {
		return 0
	}
	return float64(m.BytesOut) / dur.Seconds()
}

type tpMetrics struct {
	bytesIn, bytesOut, packetsIn, packetsOut atomic.Uint64
	packetsDropped, backpressureEvents       atomic.Uint64
	compressionSaved, stageErrors            atomic.Uint64
	latencySumMs, latencyCount               atomic.Int64
}

func (m *tpMetrics) snapshot() *TPMetricsSnapshot {
	return &TPMetricsSnapshot{
		BytesIn: m.bytesIn.Load(), BytesOut: m.bytesOut.Load(),
		PacketsIn: m.packetsIn.Load(), PacketsOut: m.packetsOut.Load(),
		PacketsDropped: m.packetsDropped.Load(), BackpressureEvents: m.backpressureEvents.Load(),
		CompressionSaved: m.compressionSaved.Load(), StageErrors: m.stageErrors.Load(),
		LatencySumMs: m.latencySumMs.Load(), LatencyCount: m.latencyCount.Load(),
	}
}

// BackpressureChannel is a bounded channel with configurable drop semantics.
type BackpressureChannel struct {
	mu     sync.Mutex
	ch     chan *TPDataPacket
	policy DropPolicy
	closed bool
}

func NewBackpressureChannel(size int, policy DropPolicy) *BackpressureChannel {
	if size <= 0 {
		size = 64
	}
	return &BackpressureChannel{ch: make(chan *TPDataPacket, size), policy: policy}
}

func (bc *BackpressureChannel) Send(ctx context.Context, pkt *TPDataPacket) error {
	if bc.policy == DropOldest {
		select {
		case bc.ch <- pkt:
			return nil
		default:
			select {
			case <-bc.ch: // drain oldest
			default:
			}
			select {
			case bc.ch <- pkt:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	select {
	case bc.ch <- pkt:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (bc *BackpressureChannel) Recv(ctx context.Context) (*TPDataPacket, error) {
	select {
	case pkt, ok := <-bc.ch:
		if !ok {
			return nil, ErrTPPipelineStopped
		}
		return pkt, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (bc *BackpressureChannel) Close() {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if !bc.closed {
		bc.closed = true
		close(bc.ch)
	}
}

func (bc *BackpressureChannel) Len() int { return len(bc.ch) }

type BandwidthGate struct{ enforcer *BandwidthEnforcer }

func NewBandwidthGate(e *BandwidthEnforcer) *BandwidthGate { return &BandwidthGate{enforcer: e} }
func (g *BandwidthGate) Name() string                      { return "bandwidth_gate" }
func (g *BandwidthGate) Process(pkt *TPDataPacket) (*TPDataPacket, error) {
	if g.enforcer == nil {
		return pkt, nil
	}
	if err := g.enforcer.RequestBandwidth(pkt.ChainID, uint64(len(pkt.Data))); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTPBandwidthDenied, err)
	}
	return pkt, nil
}

type CompressionStage struct{}

func (c *CompressionStage) Name() string { return "compression" }
func (c *CompressionStage) Process(pkt *TPDataPacket) (*TPDataPacket, error) {
	if pkt == nil || len(pkt.Data) == 0 {
		return pkt, nil
	}
	pkt.OrigSize = len(pkt.Data)
	pkt.Data = simpleCompress(pkt.Data)
	pkt.Compressed = true
	return pkt, nil
}

// runs of 4+ identical bytes -> [0xFF, byte, hi, lo].
func simpleCompress(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	const marker byte = 0xFF
	result := make([]byte, 0, len(data))
	for i := 0; i < len(data); {
		b := data[i]
		runLen := 1
		for i+runLen < len(data) && data[i+runLen] == b && runLen < 65535 {
			runLen++
		}
		if runLen >= 4 && b != marker {
			result = append(result, marker, b, byte(runLen>>8), byte(runLen&0xFF))
			i += runLen
		} else if b == marker {
			result = append(result, marker, marker, 0, 1)
			i++
		} else {
			result = append(result, b)
			i++
		}
	}
	return result
}

// SimpleDecompress reverses simpleCompress.
func SimpleDecompress(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	const marker byte = 0xFF
	result := make([]byte, 0, len(data)*2)
	for i := 0; i < len(data); {
		if data[i] == marker {
			if i+3 >= len(data) {
				return nil, fmt.Errorf("%w: truncated RLE at %d", ErrTPCompressionFailed, i)
			}
			b := data[i+1]
			count := int(data[i+2])<<8 | int(data[i+3])
			for j := 0; j < count; j++ {
				result = append(result, b)
			}
			i += 4
		} else {
			result = append(result, data[i])
			i++
		}
	}
	return result, nil
}

type ChunkingStage struct{ MaxChunkSize int }

func (cs *ChunkingStage) Name() string { return "chunking" }
func (cs *ChunkingStage) Process(pkt *TPDataPacket) (*TPDataPacket, error) {
	if pkt == nil || len(pkt.Data) == 0 {
		return pkt, nil
	}
	maxSize := cs.MaxChunkSize
	if maxSize <= 0 {
		maxSize = 64 * 1024
	}
	pkt.IsChunked = true
	if len(pkt.Data) <= maxSize {
		pkt.TotalChunks = 1
	} else {
		pkt.TotalChunks = (len(pkt.Data) + maxSize - 1) / maxSize
	}
	return pkt, nil
}

func ChunkData(data []byte, maxSize int) [][]byte {
	if maxSize <= 0 {
		maxSize = 64 * 1024
	}
	if len(data) == 0 {
		return nil
	}
	n := (len(data) + maxSize - 1) / maxSize
	chunks := make([][]byte, n)
	for i := 0; i < n; i++ {
		start := i * maxSize
		end := start + maxSize
		if end > len(data) {
			end = len(data)
		}
		chunk := make([]byte, end-start)
		copy(chunk, data[start:end])
		chunks[i] = chunk
	}
	return chunks
}

type ReassemblyStage struct {
	mu      sync.Mutex
	pending map[reassemblyKey]*reassemblyState
}
type reassemblyKey struct {
	chainID   uint64
	timestamp int64
}
type reassemblyState struct {
	chunks      map[int][]byte
	totalChunks int
	compressed  bool
	origSize    int
}

func NewReassemblyStage() *ReassemblyStage {
	return &ReassemblyStage{pending: make(map[reassemblyKey]*reassemblyState)}
}
func (rs *ReassemblyStage) Name() string { return "reassembly" }
func (rs *ReassemblyStage) Process(pkt *TPDataPacket) (*TPDataPacket, error) {
	if pkt == nil || !pkt.IsChunked || pkt.TotalChunks <= 1 {
		return pkt, nil
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	key := reassemblyKey{pkt.ChainID, pkt.Timestamp.UnixNano()}
	st, ok := rs.pending[key]
	if !ok {
		st = &reassemblyState{chunks: make(map[int][]byte), totalChunks: pkt.TotalChunks,
			compressed: pkt.Compressed, origSize: pkt.OrigSize}
		rs.pending[key] = st
	}
	st.chunks[pkt.ChunkIndex] = pkt.Data
	if len(st.chunks) < st.totalChunks {
		return nil, nil
	}
	var assembled []byte
	for i := 0; i < st.totalChunks; i++ {
		c, exists := st.chunks[i]
		if !exists {
			delete(rs.pending, key)
			return nil, ErrTPReassemblyFailed
		}
		assembled = append(assembled, c...)
	}
	delete(rs.pending, key)
	return &TPDataPacket{ChainID: pkt.ChainID, Data: assembled,
		Compressed: st.compressed, OrigSize: st.origSize, Timestamp: pkt.Timestamp}, nil
}

// TeragasPipeline orchestrates L2 data flow with backpressure and metrics.
type TeragasPipeline struct {
	config  *TPConfig
	stages  []PipelineStage
	input   *BackpressureChannel
	output  *BackpressureChannel
	metrics tpMetrics
	cancel  context.CancelFunc
	ctx     context.Context
	wg      sync.WaitGroup
	stopped atomic.Bool
	started atomic.Bool
}

func NewTeragasPipeline(config *TPConfig) (*TeragasPipeline, error) {
	if config == nil {
		return nil, ErrTPInvalidConfig
	}
	if config.ChannelBufferSize <= 0 {
		config.ChannelBufferSize = 256
	}
	if config.MaxChunkSize <= 0 {
		config.MaxChunkSize = 64 * 1024
	}
	if config.ConsumerTimeout <= 0 {
		config.ConsumerTimeout = 5 * time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	tp := &TeragasPipeline{
		config: config, ctx: ctx, cancel: cancel,
		input:  NewBackpressureChannel(config.ChannelBufferSize, config.Policy),
		output: NewBackpressureChannel(config.ChannelBufferSize, config.Policy),
	}
	if config.BandwidthEnforcer != nil {
		tp.stages = append(tp.stages, NewBandwidthGate(config.BandwidthEnforcer))
	}
	if config.CompressionEnabled {
		tp.stages = append(tp.stages, &CompressionStage{})
	}
	if config.ChunkingEnabled {
		tp.stages = append(tp.stages, &ChunkingStage{MaxChunkSize: config.MaxChunkSize})
	}
	return tp, nil
}

func (tp *TeragasPipeline) AddStage(stage PipelineStage) { tp.stages = append(tp.stages, stage) }

func (tp *TeragasPipeline) Start() {
	if tp.started.Swap(true) {
		return
	}
	tp.wg.Add(1)
	go tp.processLoop()
}

func (tp *TeragasPipeline) processLoop() {
	defer tp.wg.Done()
	for {
		pkt, err := tp.input.Recv(tp.ctx)
		if err != nil {
			return
		}
		tp.metrics.packetsIn.Add(1)
		tp.metrics.bytesIn.Add(uint64(len(pkt.Data)))
		origSize := len(pkt.Data)

		current, stageErr := pkt, error(nil)
		for _, stage := range tp.stages {
			current, stageErr = stage.Process(current)
			if stageErr != nil {
				tp.metrics.stageErrors.Add(1)
				if errors.Is(stageErr, ErrTPBandwidthDenied) {
					tp.metrics.backpressureEvents.Add(1)
				}
				tp.metrics.packetsDropped.Add(1)
				break
			}
			if current == nil {
				break
			}
		}
		if stageErr != nil || current == nil {
			continue
		}
		if current.Compressed && origSize > len(current.Data) {
			tp.metrics.compressionSaved.Add(uint64(origSize - len(current.Data)))
		}
		if current.IsChunked && current.TotalChunks > 1 {
			for i, chunk := range ChunkData(current.Data, tp.config.MaxChunkSize) {
				cpkt := &TPDataPacket{ChainID: current.ChainID, Data: chunk,
					ChunkIndex: i, TotalChunks: current.TotalChunks, IsChunked: true,
					Compressed: current.Compressed, OrigSize: current.OrigSize, Timestamp: current.Timestamp}
				if tp.output.Send(tp.ctx, cpkt) != nil {
					tp.metrics.packetsDropped.Add(1)
					break
				}
				tp.metrics.packetsOut.Add(1)
				tp.metrics.bytesOut.Add(uint64(len(chunk)))
			}
		} else {
			if tp.output.Send(tp.ctx, current) != nil {
				tp.metrics.packetsDropped.Add(1)
				continue
			}
			tp.metrics.packetsOut.Add(1)
			tp.metrics.bytesOut.Add(uint64(len(current.Data)))
		}
		if !current.Timestamp.IsZero() {
			tp.metrics.latencySumMs.Add(time.Since(current.Timestamp).Milliseconds())
			tp.metrics.latencyCount.Add(1)
		}
	}
}

func (tp *TeragasPipeline) Submit(chainID uint64, data []byte) error {
	if tp.stopped.Load() {
		return ErrTPPipelineStopped
	}
	if len(data) == 0 {
		return ErrTPNilData
	}
	return tp.input.Send(tp.ctx, &TPDataPacket{ChainID: chainID, Data: data, Timestamp: time.Now()})
}

func (tp *TeragasPipeline) Receive(ctx context.Context) (*TPDataPacket, error) {
	return tp.output.Recv(ctx)
}

func (tp *TeragasPipeline) Stop() {
	if tp.stopped.Swap(true) {
		return
	}
	tp.cancel()
	tp.input.Close()
	tp.wg.Wait()
	tp.output.Close()
}

func (tp *TeragasPipeline) Metrics() *TPMetricsSnapshot { return tp.metrics.snapshot() }
func (tp *TeragasPipeline) IsStopped() bool             { return tp.stopped.Load() }
func (tp *TeragasPipeline) StageCount() int             { return len(tp.stages) }

func (tp *TeragasPipeline) StageNames() []string {
	names := make([]string, len(tp.stages))
	for i, s := range tp.stages {
		names[i] = s.Name()
	}
	return names
}
