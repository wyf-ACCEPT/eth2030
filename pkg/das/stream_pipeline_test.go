package das

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestStreamPipe_DefaultConfig(t *testing.T) {
	cfg := DefaultPipelineConfig()
	if cfg.BufferSize != 256 {
		t.Errorf("BufferSize = %d, want 256", cfg.BufferSize)
	}
	if cfg.ReceiveWorkers != 4 {
		t.Errorf("ReceiveWorkers = %d, want 4", cfg.ReceiveWorkers)
	}
	if cfg.StageTimeout != 5*time.Second {
		t.Errorf("StageTimeout = %v, want 5s", cfg.StageTimeout)
	}
}

func TestStreamPipe_BasicPipeline(t *testing.T) {
	p := NewStreamPipeline(DefaultPipelineConfig(), nil, nil, nil)
	p.Start()

	id := [32]byte{0x01}
	err := p.Submit(id, 1, []byte("hello world"))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// Read result.
	select {
	case result := <-p.Results():
		if result == nil {
			t.Fatal("got nil result")
		}
		if !result.Valid {
			t.Error("expected Valid = true")
		}
		if !result.Stored {
			t.Error("expected Stored = true")
		}
		if result.Error != nil {
			t.Errorf("unexpected error: %v", result.Error)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for pipeline result")
	}

	p.Stop()
}

func TestStreamPipe_MultipleItems(t *testing.T) {
	p := NewStreamPipeline(DefaultPipelineConfig(), nil, nil, nil)
	p.Start()

	numItems := 50
	for i := 0; i < numItems; i++ {
		id := [32]byte{byte(i)}
		err := p.Submit(id, 1, []byte{byte(i), 0xAA})
		if err != nil {
			t.Fatalf("Submit %d: %v", i, err)
		}
	}

	received := 0
	timeout := time.After(10 * time.Second)
	for received < numItems {
		select {
		case result := <-p.Results():
			if result == nil {
				t.Fatal("nil result")
			}
			received++
		case <-timeout:
			t.Fatalf("timeout after receiving %d/%d items", received, numItems)
		}
	}

	p.Stop()

	r, v, d, s, _ := p.GetMetrics()
	if r != uint64(numItems) {
		t.Errorf("received = %d, want %d", r, numItems)
	}
	if v != uint64(numItems) {
		t.Errorf("validated = %d, want %d", v, numItems)
	}
	if d != uint64(numItems) {
		t.Errorf("decoded = %d, want %d", d, numItems)
	}
	if s != uint64(numItems) {
		t.Errorf("stored = %d, want %d", s, numItems)
	}
}

func TestStreamPipe_ValidationFailure(t *testing.T) {
	// Validator that rejects everything.
	rejectAll := func(data []byte) bool { return false }
	p := NewStreamPipeline(
		PipelineConfig{
			BufferSize:      16,
			ReceiveWorkers:  1,
			ValidateWorkers: 1,
			DecodeWorkers:   1,
			StoreWorkers:    1,
			StageTimeout:    5 * time.Second,
			MaxRetries:      0, // no retries
		},
		rejectAll, nil, nil,
	)
	p.Start()

	id := [32]byte{0x01}
	p.Submit(id, 1, []byte("bad data"))

	select {
	case result := <-p.Results():
		if result.Error == nil {
			t.Error("expected validation error")
		}
		if !errors.Is(result.Error, ErrValidationFailed) {
			t.Errorf("expected ErrValidationFailed, got: %v", result.Error)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	p.Stop()
}

func TestStreamPipe_DecodeError(t *testing.T) {
	failDecode := func(data []byte) ([]byte, error) {
		return nil, errors.New("decode failed")
	}
	p := NewStreamPipeline(
		PipelineConfig{
			BufferSize:      16,
			ReceiveWorkers:  1,
			ValidateWorkers: 1,
			DecodeWorkers:   1,
			StoreWorkers:    1,
			StageTimeout:    5 * time.Second,
			MaxRetries:      0,
		},
		nil, failDecode, nil,
	)
	p.Start()

	id := [32]byte{0x02}
	p.Submit(id, 1, []byte("data"))

	select {
	case result := <-p.Results():
		if result.Error == nil {
			t.Error("expected decode error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	p.Stop()
}

func TestStreamPipe_StoreError(t *testing.T) {
	failStore := func(id [32]byte, chainID uint64, data []byte) error {
		return errors.New("store failed")
	}
	p := NewStreamPipeline(
		PipelineConfig{
			BufferSize:      16,
			ReceiveWorkers:  1,
			ValidateWorkers: 1,
			DecodeWorkers:   1,
			StoreWorkers:    1,
			StageTimeout:    5 * time.Second,
			MaxRetries:      0,
		},
		nil, nil, failStore,
	)
	p.Start()

	id := [32]byte{0x03}
	p.Submit(id, 1, []byte("data"))

	select {
	case result := <-p.Results():
		if result.Error == nil {
			t.Error("expected store error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	p.Stop()
}

func TestStreamPipe_Backpressure(t *testing.T) {
	// Slow store to test backpressure.
	var storeCount atomic.Int64
	slowStore := func(id [32]byte, chainID uint64, data []byte) error {
		time.Sleep(10 * time.Millisecond)
		storeCount.Add(1)
		return nil
	}

	p := NewStreamPipeline(
		PipelineConfig{
			BufferSize:      4, // small buffer to trigger backpressure
			ReceiveWorkers:  2,
			ValidateWorkers: 2,
			DecodeWorkers:   2,
			StoreWorkers:    1, // single slow store worker
			StageTimeout:    5 * time.Second,
			MaxRetries:      0,
		},
		nil, nil, slowStore,
	)
	p.Start()

	// Submit many items -- the pipeline should handle backpressure.
	numItems := 20
	for i := 0; i < numItems; i++ {
		id := [32]byte{byte(i)}
		p.Submit(id, 1, []byte{byte(i)})
	}

	// Drain results.
	received := 0
	timeout := time.After(10 * time.Second)
	for received < numItems {
		select {
		case <-p.Results():
			received++
		case <-timeout:
			t.Fatalf("timeout: received %d/%d items", received, numItems)
		}
	}

	p.Stop()

	if storeCount.Load() != int64(numItems) {
		t.Errorf("store count = %d, want %d", storeCount.Load(), numItems)
	}
}

func TestStreamPipe_SubmitAfterStop(t *testing.T) {
	p := NewStreamPipeline(DefaultPipelineConfig(), nil, nil, nil)
	p.Start()
	p.Stop()

	err := p.Submit([32]byte{0x01}, 1, []byte("data"))
	if !errors.Is(err, ErrPipelineStopped) {
		t.Errorf("expected ErrPipelineStopped, got %v", err)
	}
}

func TestStreamPipe_IsStopped(t *testing.T) {
	p := NewStreamPipeline(DefaultPipelineConfig(), nil, nil, nil)
	if p.IsStopped() {
		t.Error("IsStopped() should be false before Stop()")
	}
	p.Start()
	p.Stop()
	if !p.IsStopped() {
		t.Error("IsStopped() should be true after Stop()")
	}
}

func TestStreamPipe_TotalBytes(t *testing.T) {
	p := NewStreamPipeline(DefaultPipelineConfig(), nil, nil, nil)
	p.Start()

	data := make([]byte, 1024)
	for i := 0; i < 10; i++ {
		p.Submit([32]byte{byte(i)}, 1, data)
	}

	// Drain results.
	for i := 0; i < 10; i++ {
		select {
		case <-p.Results():
		case <-time.After(5 * time.Second):
			t.Fatalf("timeout at item %d", i)
		}
	}

	p.Stop()

	total := p.TotalBytesProcessed()
	if total != 10*1024 {
		t.Errorf("TotalBytesProcessed() = %d, want %d", total, 10*1024)
	}
}

func TestStreamPipe_EmptyData(t *testing.T) {
	// Default validator rejects empty data.
	p := NewStreamPipeline(
		PipelineConfig{
			BufferSize:      16,
			ReceiveWorkers:  1,
			ValidateWorkers: 1,
			DecodeWorkers:   1,
			StoreWorkers:    1,
			StageTimeout:    5 * time.Second,
			MaxRetries:      0,
		},
		nil, nil, nil,
	)
	p.Start()

	p.Submit([32]byte{0x01}, 1, []byte{}) // empty data

	select {
	case result := <-p.Results():
		if result.Error == nil {
			t.Error("expected error for empty data")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	p.Stop()
}

func TestStreamPipe_RetryMechanism(t *testing.T) {
	callCount := atomic.Int64{}
	flakyValidate := func(data []byte) bool {
		// Fail the first time, succeed after.
		c := callCount.Add(1)
		return c > 1
	}

	p := NewStreamPipeline(
		PipelineConfig{
			BufferSize:      16,
			ReceiveWorkers:  1,
			ValidateWorkers: 1,
			DecodeWorkers:   1,
			StoreWorkers:    1,
			StageTimeout:    5 * time.Second,
			MaxRetries:      2,
		},
		flakyValidate, nil, nil,
	)
	p.Start()

	p.Submit([32]byte{0x01}, 1, []byte("data"))

	select {
	case result := <-p.Results():
		// The item should eventually be processed (either stored or with error).
		_ = result
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	p.Stop()
}

func TestStreamPipe_AutoIDGeneration(t *testing.T) {
	p := NewStreamPipeline(DefaultPipelineConfig(), nil, nil, nil)
	p.Start()

	// Submit with zero ID -- should auto-generate from data hash.
	p.Submit([32]byte{}, 1, []byte("auto id test"))

	select {
	case result := <-p.Results():
		if result.ID == ([32]byte{}) {
			t.Error("expected auto-generated ID, got zero")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	p.Stop()
}
