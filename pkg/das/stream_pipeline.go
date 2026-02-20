// stream_pipeline.go implements a production streaming pipeline for teragas
// L2 throughput. Data flows through stages: Receive -> Validate -> Decode ->
// Store, each backed by a goroutine pool. Bounded channels between stages
// provide natural backpressure: a slow consumer slows the entire pipeline.
package das

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/sha3"
)

// Pipeline errors.
var (
	ErrPipelineStopped  = errors.New("das: pipeline has been stopped")
	ErrStageTimeout     = errors.New("das: stage processing timed out")
	ErrValidationFailed = errors.New("das: validation failed")
	ErrDecodeFailed     = errors.New("das: decode failed")
	ErrStoreFailed      = errors.New("das: store failed")
)

// PipelineConfig configures the streaming pipeline.
type PipelineConfig struct {
	// BufferSize is the channel buffer size between stages.
	BufferSize int
	// ReceiveWorkers is the number of goroutines in the receive stage.
	ReceiveWorkers int
	// ValidateWorkers is the number of goroutines in the validate stage.
	ValidateWorkers int
	// DecodeWorkers is the number of goroutines in the decode stage.
	DecodeWorkers int
	// StoreWorkers is the number of goroutines in the store stage.
	StoreWorkers int
	// StageTimeout is the maximum time a single item can spend in a stage.
	StageTimeout time.Duration
	// MaxRetries is the number of retry attempts for failed stage processing.
	MaxRetries int
}

// DefaultPipelineConfig returns a sensible default pipeline configuration.
func DefaultPipelineConfig() PipelineConfig {
	return PipelineConfig{
		BufferSize:      256,
		ReceiveWorkers:  4,
		ValidateWorkers: 4,
		DecodeWorkers:   4,
		StoreWorkers:    2,
		StageTimeout:    5 * time.Second,
		MaxRetries:      2,
	}
}

// PipelineItem flows through the pipeline, accumulating state at each stage.
type PipelineItem struct {
	// ID uniquely identifies this item (e.g., blob commitment hash).
	ID [32]byte
	// RawData is the received raw bytes.
	RawData []byte
	// Valid indicates whether the item passed validation.
	Valid bool
	// DecodedData holds the decoded payload after the decode stage.
	DecodedData []byte
	// Stored indicates whether the item was successfully persisted.
	Stored bool
	// Error captures any error encountered during processing.
	Error error
	// ChainID is the L2 chain this data belongs to.
	ChainID uint64
	// Retries tracks how many times this item has been retried.
	Retries int
	// Timestamp is when the item entered the pipeline.
	Timestamp time.Time
}

// PipelineMetrics tracks throughput and error statistics.
type PipelineMetrics struct {
	Received       atomic.Uint64
	Validated      atomic.Uint64
	ValidationFail atomic.Uint64
	Decoded        atomic.Uint64
	DecodeFail     atomic.Uint64
	Stored         atomic.Uint64
	StoreFail      atomic.Uint64
	Retried        atomic.Uint64
	Dropped        atomic.Uint64
	TotalBytes     atomic.Uint64
}

// ValidateFunc is a user-supplied validation function.
type ValidateFunc func(data []byte) bool

// DecodeFunc is a user-supplied decode function.
type DecodeFunc func(data []byte) ([]byte, error)

// StoreFunc is a user-supplied storage function.
type StoreFunc func(id [32]byte, chainID uint64, data []byte) error

// StreamPipeline orchestrates the Receive->Validate->Decode->Store stages.
type StreamPipeline struct {
	config  PipelineConfig
	metrics PipelineMetrics

	// Stage channels.
	receiveCh  chan *PipelineItem
	validateCh chan *PipelineItem
	decodeCh   chan *PipelineItem
	storeCh    chan *PipelineItem
	resultCh   chan *PipelineItem

	// User-supplied callbacks.
	validateFn ValidateFunc
	decodeFn   DecodeFunc
	storeFn    StoreFunc

	// Lifecycle.
	stopped atomic.Bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewStreamPipeline creates a new pipeline with the given config and callbacks.
// If callbacks are nil, defaults are used (pass-through validate, no-op decode,
// no-op store).
func NewStreamPipeline(config PipelineConfig, vf ValidateFunc, df DecodeFunc, sf StoreFunc) *StreamPipeline {
	if config.BufferSize <= 0 {
		config.BufferSize = 256
	}
	if config.ReceiveWorkers <= 0 {
		config.ReceiveWorkers = 4
	}
	if config.ValidateWorkers <= 0 {
		config.ValidateWorkers = 4
	}
	if config.DecodeWorkers <= 0 {
		config.DecodeWorkers = 4
	}
	if config.StoreWorkers <= 0 {
		config.StoreWorkers = 2
	}
	if config.StageTimeout <= 0 {
		config.StageTimeout = 5 * time.Second
	}
	if config.MaxRetries < 0 {
		config.MaxRetries = 0
	}

	if vf == nil {
		vf = func(data []byte) bool { return len(data) > 0 }
	}
	if df == nil {
		df = func(data []byte) ([]byte, error) { return data, nil }
	}
	if sf == nil {
		sf = func(_ [32]byte, _ uint64, _ []byte) error { return nil }
	}

	p := &StreamPipeline{
		config:     config,
		receiveCh:  make(chan *PipelineItem, config.BufferSize),
		validateCh: make(chan *PipelineItem, config.BufferSize),
		decodeCh:   make(chan *PipelineItem, config.BufferSize),
		storeCh:    make(chan *PipelineItem, config.BufferSize),
		resultCh:   make(chan *PipelineItem, config.BufferSize),
		validateFn: vf,
		decodeFn:   df,
		storeFn:    sf,
		stopCh:     make(chan struct{}),
	}
	return p
}

// Start launches all pipeline stage workers.
func (p *StreamPipeline) Start() {
	// Receive stage: moves items from receiveCh to validateCh.
	p.startStage(p.config.ReceiveWorkers, p.receiveCh, p.validateCh, p.receiveProcess)
	// Validate stage: validates and forwards to decodeCh.
	p.startStage(p.config.ValidateWorkers, p.validateCh, p.decodeCh, p.validateProcess)
	// Decode stage: decodes and forwards to storeCh.
	p.startStage(p.config.DecodeWorkers, p.decodeCh, p.storeCh, p.decodeProcess)
	// Store stage: persists and sends to resultCh.
	p.startStage(p.config.StoreWorkers, p.storeCh, p.resultCh, p.storeProcess)
}

// startStage launches numWorkers goroutines that read from inCh, process, and
// write to outCh. When inCh is closed and drained, outCh is closed.
func (p *StreamPipeline) startStage(
	numWorkers int,
	inCh <-chan *PipelineItem,
	outCh chan<- *PipelineItem,
	process func(*PipelineItem) *PipelineItem,
) {
	var stageWg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		stageWg.Add(1)
		p.wg.Add(1)
		go func() {
			defer stageWg.Done()
			defer p.wg.Done()
			for item := range inCh {
				if p.stopped.Load() {
					return
				}
				result := process(item)
				if result != nil {
					select {
					case outCh <- result:
					case <-p.stopCh:
						return
					}
				}
			}
		}()
	}
	// Close outCh when all workers in this stage are done.
	go func() {
		stageWg.Wait()
		close(outCh)
	}()
}

// Submit sends a raw data item into the pipeline for processing.
func (p *StreamPipeline) Submit(id [32]byte, chainID uint64, rawData []byte) error {
	if p.stopped.Load() {
		return ErrPipelineStopped
	}
	item := &PipelineItem{
		ID:        id,
		RawData:   rawData,
		ChainID:   chainID,
		Timestamp: time.Now(),
	}
	select {
	case p.receiveCh <- item:
		return nil
	case <-p.stopCh:
		return ErrPipelineStopped
	}
}

// Results returns the channel of completed pipeline items.
func (p *StreamPipeline) Results() <-chan *PipelineItem {
	return p.resultCh
}

// Stop gracefully shuts down the pipeline.
func (p *StreamPipeline) Stop() {
	if p.stopped.Swap(true) {
		return // already stopped
	}
	close(p.stopCh)
	close(p.receiveCh) // triggers cascading close of subsequent channels
	p.wg.Wait()
}

// --- Stage processing functions ---

func (p *StreamPipeline) receiveProcess(item *PipelineItem) *PipelineItem {
	p.metrics.Received.Add(1)
	p.metrics.TotalBytes.Add(uint64(len(item.RawData)))

	// Compute ID from data if zero.
	if item.ID == [32]byte{} && len(item.RawData) > 0 {
		h := sha3.NewLegacyKeccak256()
		h.Write(item.RawData)
		copy(item.ID[:], h.Sum(nil))
	}
	return item
}

func (p *StreamPipeline) validateProcess(item *PipelineItem) *PipelineItem {
	if p.validateFn(item.RawData) {
		item.Valid = true
		p.metrics.Validated.Add(1)
		return item
	}

	// Validation failed -- retry if possible.
	if item.Retries < p.config.MaxRetries {
		item.Retries++
		p.metrics.Retried.Add(1)
		return item // pass through for retry at decode stage
	}

	item.Error = ErrValidationFailed
	p.metrics.ValidationFail.Add(1)
	p.metrics.Dropped.Add(1)
	return item // return with error for result collection
}

func (p *StreamPipeline) decodeProcess(item *PipelineItem) *PipelineItem {
	if item.Error != nil {
		return item // pass through errors
	}
	if !item.Valid {
		// Item was retried but still not valid.
		item.Error = ErrValidationFailed
		p.metrics.Dropped.Add(1)
		return item
	}

	decoded, err := p.decodeFn(item.RawData)
	if err != nil {
		if item.Retries < p.config.MaxRetries {
			item.Retries++
			p.metrics.Retried.Add(1)
			item.Error = fmt.Errorf("%w: %v", ErrDecodeFailed, err)
			return item
		}
		item.Error = fmt.Errorf("%w: %v", ErrDecodeFailed, err)
		p.metrics.DecodeFail.Add(1)
		p.metrics.Dropped.Add(1)
		return item
	}

	item.DecodedData = decoded
	p.metrics.Decoded.Add(1)
	return item
}

func (p *StreamPipeline) storeProcess(item *PipelineItem) *PipelineItem {
	if item.Error != nil {
		return item // pass through errors
	}

	data := item.DecodedData
	if data == nil {
		data = item.RawData
	}

	err := p.storeFn(item.ID, item.ChainID, data)
	if err != nil {
		if item.Retries < p.config.MaxRetries {
			item.Retries++
			p.metrics.Retried.Add(1)
			item.Error = fmt.Errorf("%w: %v", ErrStoreFailed, err)
			return item
		}
		item.Error = fmt.Errorf("%w: %v", ErrStoreFailed, err)
		p.metrics.StoreFail.Add(1)
		p.metrics.Dropped.Add(1)
		return item
	}

	item.Stored = true
	p.metrics.Stored.Add(1)
	return item
}

// GetMetrics returns a snapshot of pipeline metrics.
func (p *StreamPipeline) GetMetrics() (received, validated, decoded, stored, dropped uint64) {
	return p.metrics.Received.Load(),
		p.metrics.Validated.Load(),
		p.metrics.Decoded.Load(),
		p.metrics.Stored.Load(),
		p.metrics.Dropped.Load()
}

// TotalBytesProcessed returns the total bytes that entered the pipeline.
func (p *StreamPipeline) TotalBytesProcessed() uint64 {
	return p.metrics.TotalBytes.Load()
}

// IsStopped returns whether the pipeline has been stopped.
func (p *StreamPipeline) IsStopped() bool {
	return p.stopped.Load()
}
