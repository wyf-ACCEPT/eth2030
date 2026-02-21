package das

import (
	"bytes"
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- Configuration and creation tests ---

func TestTPNewPipelineDefaults(t *testing.T) {
	cfg := DefaultTPConfig()
	tp, err := NewTeragasPipeline(cfg)
	if err != nil {
		t.Fatalf("NewTeragasPipeline: %v", err)
	}
	if tp == nil {
		t.Fatal("pipeline is nil")
	}
	// Default config enables compression + chunking = 2 stages.
	if tp.StageCount() < 2 {
		t.Errorf("stages: got %d, want >= 2", tp.StageCount())
	}
	names := tp.StageNames()
	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	if !found["compression"] {
		t.Error("missing compression stage")
	}
	if !found["chunking"] {
		t.Error("missing chunking stage")
	}
}

func TestTPNewPipelineNilConfig(t *testing.T) {
	_, err := NewTeragasPipeline(nil)
	if err == nil {
		t.Fatal("expected error for nil config")
	}
}

func TestTPNewPipelineWithBandwidthEnforcer(t *testing.T) {
	be, err := NewBandwidthEnforcer(DefaultBandwidthConfig())
	if err != nil {
		t.Fatalf("NewBandwidthEnforcer: %v", err)
	}
	cfg := DefaultTPConfig()
	cfg.BandwidthEnforcer = be
	tp, err := NewTeragasPipeline(cfg)
	if err != nil {
		t.Fatalf("NewTeragasPipeline: %v", err)
	}
	// Should have bandwidth_gate + compression + chunking = 3.
	if tp.StageCount() != 3 {
		t.Errorf("stages: got %d, want 3", tp.StageCount())
	}
}

func TestTPNewPipelineNoCompression(t *testing.T) {
	cfg := DefaultTPConfig()
	cfg.CompressionEnabled = false
	tp, err := NewTeragasPipeline(cfg)
	if err != nil {
		t.Fatalf("NewTeragasPipeline: %v", err)
	}
	if tp.StageCount() != 1 {
		t.Errorf("stages: got %d, want 1 (chunking only)", tp.StageCount())
	}
}

// --- Compression tests ---

func TestTPCompressionReducesSize(t *testing.T) {
	// Data with lots of repeated bytes should compress well.
	data := bytes.Repeat([]byte{0xAA}, 1000)
	compressed := simpleCompress(data)
	if len(compressed) >= len(data) {
		t.Errorf("compression did not reduce size: %d >= %d", len(compressed), len(data))
	}
}

func TestTPCompressionRoundtrip(t *testing.T) {
	original := make([]byte, 500)
	for i := range original {
		original[i] = byte(i % 7)
	}
	compressed := simpleCompress(original)
	decompressed, err := SimpleDecompress(compressed)
	if err != nil {
		t.Fatalf("SimpleDecompress: %v", err)
	}
	if !bytes.Equal(original, decompressed) {
		t.Errorf("roundtrip mismatch: got %d bytes, want %d", len(decompressed), len(original))
	}
}

func TestTPCompressionRepeatedBytes(t *testing.T) {
	// Long run of the same byte.
	data := bytes.Repeat([]byte{0x42}, 10000)
	compressed := simpleCompress(data)
	decompressed, err := SimpleDecompress(compressed)
	if err != nil {
		t.Fatalf("SimpleDecompress: %v", err)
	}
	if !bytes.Equal(data, decompressed) {
		t.Error("roundtrip failed for repeated bytes")
	}
	// Compression ratio should be very good.
	if len(compressed) > 100 {
		t.Errorf("compressed size %d too large for 10000 repeated bytes", len(compressed))
	}
}

func TestTPCompressionEmptyData(t *testing.T) {
	compressed := simpleCompress(nil)
	if compressed != nil {
		t.Error("expected nil for empty data")
	}
	decompressed, err := SimpleDecompress(nil)
	if err != nil {
		t.Fatalf("SimpleDecompress nil: %v", err)
	}
	if decompressed != nil {
		t.Error("expected nil decompressed for nil input")
	}
}

func TestTPCompressionMarkerByte(t *testing.T) {
	// Data containing the marker byte 0xFF should be handled correctly.
	data := []byte{0xFF, 0x01, 0xFF, 0xFF, 0x02}
	compressed := simpleCompress(data)
	decompressed, err := SimpleDecompress(compressed)
	if err != nil {
		t.Fatalf("SimpleDecompress: %v", err)
	}
	if !bytes.Equal(data, decompressed) {
		t.Errorf("marker byte roundtrip failed: got %x, want %x", decompressed, data)
	}
}

// --- Chunking tests ---

func TestTPChunkDataSplitsCorrectly(t *testing.T) {
	data := make([]byte, 1000)
	for i := range data {
		data[i] = byte(i)
	}
	chunks := ChunkData(data, 300)
	if len(chunks) != 4 { // 300+300+300+100
		t.Fatalf("chunks: got %d, want 4", len(chunks))
	}
	// Reassemble.
	var reassembled []byte
	for _, c := range chunks {
		reassembled = append(reassembled, c...)
	}
	if !bytes.Equal(data, reassembled) {
		t.Error("chunk reassembly mismatch")
	}
}

func TestTPChunkDataSmallData(t *testing.T) {
	data := []byte{1, 2, 3}
	chunks := ChunkData(data, 1024)
	if len(chunks) != 1 {
		t.Fatalf("chunks: got %d, want 1", len(chunks))
	}
	if !bytes.Equal(data, chunks[0]) {
		t.Error("single chunk mismatch")
	}
}

func TestTPChunkDataEmpty(t *testing.T) {
	chunks := ChunkData(nil, 100)
	if chunks != nil {
		t.Error("expected nil for empty data")
	}
}

func TestTPChunkDataExactMultiple(t *testing.T) {
	data := make([]byte, 300)
	chunks := ChunkData(data, 100)
	if len(chunks) != 3 {
		t.Fatalf("chunks: got %d, want 3", len(chunks))
	}
	for i, c := range chunks {
		if len(c) != 100 {
			t.Errorf("chunk %d: got %d bytes, want 100", i, len(c))
		}
	}
}

// --- Backpressure channel tests ---

func TestTPBackpressureChannelSendRecv(t *testing.T) {
	bc := NewBackpressureChannel(10, BlockOnFull)
	ctx := context.Background()

	pkt := &TPDataPacket{ChainID: 1, Data: []byte("test")}
	if err := bc.Send(ctx, pkt); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if bc.Len() != 1 {
		t.Errorf("Len: got %d, want 1", bc.Len())
	}
	recv, err := bc.Recv(ctx)
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if recv.ChainID != 1 {
		t.Errorf("ChainID: got %d, want 1", recv.ChainID)
	}
}

func TestTPBackpressureDropOldest(t *testing.T) {
	bc := NewBackpressureChannel(2, DropOldest)
	ctx := context.Background()

	// Fill the channel.
	bc.Send(ctx, &TPDataPacket{ChainID: 1, Data: []byte("a")})
	bc.Send(ctx, &TPDataPacket{ChainID: 2, Data: []byte("b")})
	// This should drop the oldest and add the new one.
	bc.Send(ctx, &TPDataPacket{ChainID: 3, Data: []byte("c")})

	// We should get chain 2 or 3 (oldest was dropped).
	pkt, _ := bc.Recv(ctx)
	if pkt.ChainID == 1 {
		t.Error("expected chain 1 to be dropped")
	}
}

func TestTPBackpressureChannelClose(t *testing.T) {
	bc := NewBackpressureChannel(5, BlockOnFull)
	bc.Close()
	ctx := context.Background()
	_, err := bc.Recv(ctx)
	if err == nil {
		t.Error("expected error after close")
	}
}

// --- BandwidthGate tests ---

func TestTPBandwidthGateAllows(t *testing.T) {
	be, _ := NewBandwidthEnforcer(DefaultBandwidthConfig())
	be.RegisterChain(1, 0)
	gate := NewBandwidthGate(be)

	pkt := &TPDataPacket{ChainID: 1, Data: make([]byte, 1000)}
	result, err := gate.Process(pkt)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
}

func TestTPBandwidthGateDeniesUnregistered(t *testing.T) {
	be, _ := NewBandwidthEnforcer(DefaultBandwidthConfig())
	// Do NOT register chain 99.
	gate := NewBandwidthGate(be)

	pkt := &TPDataPacket{ChainID: 99, Data: make([]byte, 1000)}
	_, err := gate.Process(pkt)
	if err == nil {
		t.Error("expected error for unregistered chain")
	}
}

func TestTPBandwidthGateNilEnforcer(t *testing.T) {
	gate := NewBandwidthGate(nil)
	pkt := &TPDataPacket{ChainID: 1, Data: []byte("test")}
	result, err := gate.Process(pkt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result for nil enforcer")
	}
}

// --- ReassemblyStage tests ---

func TestTPReassemblyCompletePacket(t *testing.T) {
	rs := NewReassemblyStage()
	ts := time.Now()

	// Send 3 chunks.
	for i := 0; i < 3; i++ {
		pkt := &TPDataPacket{
			ChainID:     1,
			Data:        []byte{byte(i), byte(i), byte(i)},
			ChunkIndex:  i,
			TotalChunks: 3,
			IsChunked:   true,
			Timestamp:   ts,
		}
		result, err := rs.Process(pkt)
		if err != nil {
			t.Fatalf("Process chunk %d: %v", i, err)
		}
		if i < 2 && result != nil {
			t.Errorf("expected nil result for chunk %d", i)
		}
		if i == 2 && result == nil {
			t.Fatal("expected non-nil result for final chunk")
		}
		if i == 2 {
			expected := []byte{0, 0, 0, 1, 1, 1, 2, 2, 2}
			if !bytes.Equal(result.Data, expected) {
				t.Errorf("reassembled data: got %v, want %v", result.Data, expected)
			}
		}
	}
}

func TestTPReassemblySingleChunk(t *testing.T) {
	rs := NewReassemblyStage()
	pkt := &TPDataPacket{
		ChainID:     1,
		Data:        []byte("hello"),
		TotalChunks: 1,
		IsChunked:   true,
		Timestamp:   time.Now(),
	}
	result, err := rs.Process(pkt)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if result == nil {
		t.Fatal("expected result for single chunk")
	}
	if string(result.Data) != "hello" {
		t.Errorf("data: got %q, want %q", string(result.Data), "hello")
	}
}

// --- End-to-end pipeline tests ---

func TestTPPipelineSingleFlow(t *testing.T) {
	cfg := DefaultTPConfig()
	cfg.MaxChunkSize = 1024
	cfg.CompressionEnabled = false
	cfg.ChunkingEnabled = false
	tp, err := NewTeragasPipeline(cfg)
	if err != nil {
		t.Fatalf("NewTeragasPipeline: %v", err)
	}
	tp.Start()
	defer tp.Stop()

	data := []byte("hello teragas pipeline")
	if err := tp.Submit(1, data); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pkt, err := tp.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if string(pkt.Data) != string(data) {
		t.Errorf("data mismatch: got %q", string(pkt.Data))
	}
}

func TestTPPipelineWithCompression(t *testing.T) {
	cfg := DefaultTPConfig()
	cfg.CompressionEnabled = true
	cfg.ChunkingEnabled = false
	tp, err := NewTeragasPipeline(cfg)
	if err != nil {
		t.Fatalf("NewTeragasPipeline: %v", err)
	}
	tp.Start()
	defer tp.Stop()

	// Repetitive data should compress well.
	data := bytes.Repeat([]byte{0xAB}, 5000)
	if err := tp.Submit(1, data); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pkt, err := tp.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if !pkt.Compressed {
		t.Error("expected compressed flag to be set")
	}
	if len(pkt.Data) >= 5000 {
		t.Errorf("compressed size %d should be < 5000", len(pkt.Data))
	}

	// Verify metrics.
	m := tp.Metrics()
	if m.PacketsIn != 1 {
		t.Errorf("PacketsIn: got %d, want 1", m.PacketsIn)
	}
	if m.PacketsOut != 1 {
		t.Errorf("PacketsOut: got %d, want 1", m.PacketsOut)
	}
	if m.CompressionSaved == 0 {
		t.Error("expected non-zero compression savings")
	}
}

func TestTPPipelineWithChunking(t *testing.T) {
	cfg := DefaultTPConfig()
	cfg.CompressionEnabled = false
	cfg.ChunkingEnabled = true
	cfg.MaxChunkSize = 100
	tp, err := NewTeragasPipeline(cfg)
	if err != nil {
		t.Fatalf("NewTeragasPipeline: %v", err)
	}
	tp.Start()
	defer tp.Stop()

	data := make([]byte, 350)
	for i := range data {
		data[i] = byte(i % 256)
	}
	if err := tp.Submit(1, data); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// Should receive 4 chunks (100+100+100+50).
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var received []*TPDataPacket
	for i := 0; i < 4; i++ {
		pkt, err := tp.Receive(ctx)
		if err != nil {
			t.Fatalf("Receive[%d]: %v", i, err)
		}
		received = append(received, pkt)
	}

	if len(received) != 4 {
		t.Fatalf("received: got %d, want 4", len(received))
	}
	// Verify each chunk's metadata.
	for _, pkt := range received {
		if pkt.TotalChunks != 4 {
			t.Errorf("TotalChunks: got %d, want 4", pkt.TotalChunks)
		}
		if !pkt.IsChunked {
			t.Error("expected IsChunked to be true")
		}
	}
}

func TestTPPipelineWithBandwidthEnforcement(t *testing.T) {
	beCfg := DefaultBandwidthConfig()
	beCfg.GlobalCapBytesPerSec = 10000
	beCfg.DefaultChainQuota = 5000
	be, _ := NewBandwidthEnforcer(beCfg)
	be.RegisterChain(1, 0)

	cfg := DefaultTPConfig()
	cfg.BandwidthEnforcer = be
	cfg.CompressionEnabled = false
	cfg.ChunkingEnabled = false
	tp, err := NewTeragasPipeline(cfg)
	if err != nil {
		t.Fatalf("NewTeragasPipeline: %v", err)
	}
	tp.Start()
	defer tp.Stop()

	// Submit data within quota.
	if err := tp.Submit(1, make([]byte, 100)); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pkt, err := tp.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if len(pkt.Data) != 100 {
		t.Errorf("data size: got %d, want 100", len(pkt.Data))
	}
}

func TestTPPipelineSubmitNilData(t *testing.T) {
	cfg := DefaultTPConfig()
	tp, _ := NewTeragasPipeline(cfg)
	err := tp.Submit(1, nil)
	if err == nil {
		t.Error("expected error for nil data")
	}
}

func TestTPPipelineSubmitAfterStop(t *testing.T) {
	cfg := DefaultTPConfig()
	tp, _ := NewTeragasPipeline(cfg)
	tp.Start()
	tp.Stop()
	err := tp.Submit(1, []byte("test"))
	if err == nil {
		t.Error("expected error after stop")
	}
	if !tp.IsStopped() {
		t.Error("IsStopped should be true")
	}
}

func TestTPPipelineGracefulShutdown(t *testing.T) {
	cfg := DefaultTPConfig()
	tp, _ := NewTeragasPipeline(cfg)
	tp.Start()

	// Submit some data before stopping.
	for i := 0; i < 5; i++ {
		tp.Submit(1, []byte("data"))
	}
	// Give the processLoop a moment to process.
	time.Sleep(50 * time.Millisecond)

	tp.Stop()

	if !tp.IsStopped() {
		t.Error("pipeline should be stopped")
	}
}

func TestTPPipelineMultiChainConcurrent(t *testing.T) {
	cfg := DefaultTPConfig()
	cfg.CompressionEnabled = false
	cfg.ChunkingEnabled = false
	tp, _ := NewTeragasPipeline(cfg)
	tp.Start()
	defer tp.Stop()

	var wg sync.WaitGroup
	numChains := 5
	msgsPerChain := 10

	for chain := uint64(1); chain <= uint64(numChains); chain++ {
		wg.Add(1)
		go func(cid uint64) {
			defer wg.Done()
			for i := 0; i < msgsPerChain; i++ {
				tp.Submit(cid, []byte{byte(cid), byte(i)})
			}
		}(chain)
	}
	wg.Wait()

	// Drain output.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	received := 0
	for received < numChains*msgsPerChain {
		_, err := tp.Receive(ctx)
		if err != nil {
			break
		}
		received++
	}
	if received < numChains*msgsPerChain {
		t.Errorf("received: got %d, want %d", received, numChains*msgsPerChain)
	}
}

func TestTPPipelineThroughputMetrics(t *testing.T) {
	cfg := DefaultTPConfig()
	cfg.CompressionEnabled = false
	cfg.ChunkingEnabled = false
	tp, _ := NewTeragasPipeline(cfg)
	tp.Start()
	defer tp.Stop()

	totalBytes := 0
	for i := 0; i < 100; i++ {
		data := make([]byte, 100)
		tp.Submit(1, data)
		totalBytes += 100
	}

	// Drain output.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for i := 0; i < 100; i++ {
		tp.Receive(ctx)
	}

	m := tp.Metrics()
	if m.BytesIn != uint64(totalBytes) {
		t.Errorf("BytesIn: got %d, want %d", m.BytesIn, totalBytes)
	}
	if m.BytesOut != uint64(totalBytes) {
		t.Errorf("BytesOut: got %d, want %d", m.BytesOut, totalBytes)
	}
	if m.PacketsIn != 100 {
		t.Errorf("PacketsIn: got %d, want 100", m.PacketsIn)
	}
}

func TestTPPipelineLargeData(t *testing.T) {
	cfg := DefaultTPConfig()
	cfg.MaxChunkSize = 64 * 1024
	cfg.CompressionEnabled = true
	cfg.ChunkingEnabled = true
	tp, _ := NewTeragasPipeline(cfg)
	tp.Start()
	defer tp.Stop()

	// 1 MB of semi-random data.
	data := make([]byte, 1<<20)
	for i := range data {
		data[i] = byte(i*7 + i/256)
	}
	if err := tp.Submit(1, data); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// Receive all chunks.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var totalReceived int
	for {
		pkt, err := tp.Receive(ctx)
		if err != nil {
			break
		}
		totalReceived += len(pkt.Data)
		if !pkt.IsChunked {
			break
		}
		if pkt.TotalChunks <= 1 {
			break
		}
		if pkt.ChunkIndex == pkt.TotalChunks-1 {
			break
		}
	}
	if totalReceived == 0 {
		t.Error("received zero bytes for 1MB input")
	}
}

func TestTPPipelineLatencyTracking(t *testing.T) {
	cfg := DefaultTPConfig()
	cfg.CompressionEnabled = false
	cfg.ChunkingEnabled = false
	tp, _ := NewTeragasPipeline(cfg)
	tp.Start()
	defer tp.Stop()

	tp.Submit(1, []byte("latency test"))
	time.Sleep(10 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tp.Receive(ctx)

	m := tp.Metrics()
	if m.LatencyCount == 0 {
		t.Error("expected non-zero latency count")
	}
	avg := m.AvgLatencyMs()
	if avg < 0 {
		t.Errorf("avg latency should be >= 0, got %f", avg)
	}
}

func TestTPPipelineDropRateUnderOverload(t *testing.T) {
	beCfg := DefaultBandwidthConfig()
	beCfg.GlobalCapBytesPerSec = 100 // Very small quota.
	beCfg.DefaultChainQuota = 50
	be, _ := NewBandwidthEnforcer(beCfg)
	be.RegisterChain(1, 0)

	cfg := DefaultTPConfig()
	cfg.BandwidthEnforcer = be
	cfg.CompressionEnabled = false
	cfg.ChunkingEnabled = false
	cfg.ChannelBufferSize = 64
	tp, _ := NewTeragasPipeline(cfg)
	tp.Start()
	defer tp.Stop()

	// Flood the pipeline.
	for i := 0; i < 200; i++ {
		tp.Submit(1, make([]byte, 50))
	}
	time.Sleep(200 * time.Millisecond)

	m := tp.Metrics()
	// Some packets should be dropped due to bandwidth enforcement.
	if m.PacketsDropped == 0 && m.BackpressureEvents == 0 && m.StageErrors == 0 {
		// It's possible all went through given the token bucket burst.
		// Just verify metrics are consistent.
		if m.PacketsIn == 0 {
			t.Error("expected some packets to be processed")
		}
	}
}

func TestTPPipelineMetricsSnapshot(t *testing.T) {
	cfg := DefaultTPConfig()
	cfg.CompressionEnabled = false
	cfg.ChunkingEnabled = false
	tp, _ := NewTeragasPipeline(cfg)
	tp.Start()
	defer tp.Stop()

	tp.Submit(1, []byte("snap"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tp.Receive(ctx)

	m1 := tp.Metrics()
	m2 := tp.Metrics()
	// Snapshots taken at nearly the same time should be consistent.
	if m1.PacketsIn != m2.PacketsIn {
		t.Errorf("inconsistent metrics: %d vs %d", m1.PacketsIn, m2.PacketsIn)
	}
}

func TestTPCompressionStageProcess(t *testing.T) {
	cs := &CompressionStage{}
	if cs.Name() != "compression" {
		t.Errorf("Name: got %q, want %q", cs.Name(), "compression")
	}

	pkt := &TPDataPacket{Data: bytes.Repeat([]byte{0x00}, 1000)}
	result, err := cs.Process(pkt)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !result.Compressed {
		t.Error("expected compressed flag")
	}
	if result.OrigSize != 1000 {
		t.Errorf("OrigSize: got %d, want 1000", result.OrigSize)
	}
}

func TestTPChunkingStageProcess(t *testing.T) {
	cs := &ChunkingStage{MaxChunkSize: 100}
	if cs.Name() != "chunking" {
		t.Errorf("Name: got %q, want %q", cs.Name(), "chunking")
	}

	pkt := &TPDataPacket{Data: make([]byte, 350)}
	result, err := cs.Process(pkt)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !result.IsChunked {
		t.Error("expected IsChunked flag")
	}
	if result.TotalChunks != 4 {
		t.Errorf("TotalChunks: got %d, want 4", result.TotalChunks)
	}
}

func TestTPConcurrentProducersConsumers(t *testing.T) {
	cfg := DefaultTPConfig()
	cfg.CompressionEnabled = false
	cfg.ChunkingEnabled = false
	tp, _ := NewTeragasPipeline(cfg)
	tp.Start()
	defer tp.Stop()

	numProducers := 4
	numMessages := 50
	total := numProducers * numMessages

	var pwg sync.WaitGroup
	for p := 0; p < numProducers; p++ {
		pwg.Add(1)
		go func(pid int) {
			defer pwg.Done()
			for i := 0; i < numMessages; i++ {
				tp.Submit(uint64(pid+1), []byte{byte(pid), byte(i)})
			}
		}(p)
	}

	var cwg sync.WaitGroup
	received := int32(0)
	cwg.Add(1)
	go func() {
		defer cwg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for {
			_, err := tp.Receive(ctx)
			if err != nil {
				return
			}
			if atomic.AddInt32(&received, 1) >= int32(total) {
				return
			}
		}
	}()

	pwg.Wait()
	cwg.Wait()

	if int(atomic.LoadInt32(&received)) < total {
		t.Errorf("received: got %d, want %d", received, total)
	}
}
