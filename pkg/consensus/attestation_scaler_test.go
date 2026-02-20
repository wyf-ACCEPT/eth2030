package consensus

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// makeScalerAtt creates a test aggregate attestation for scaler tests.
func makeScalerAtt(slot uint64, bitPos int) *AggregateAttestation {
	bits := make([]byte, (bitPos/8)+1)
	bits[bitPos/8] |= 1 << (bitPos % 8)

	var sig [96]byte
	sig[0] = byte(bitPos & 0xFF)
	sig[1] = byte(slot & 0xFF)

	return &AggregateAttestation{
		Data: AttestationData{
			Slot:            Slot(slot),
			BeaconBlockRoot: types.Hash{0xDD},
			Source:          Checkpoint{Epoch: 1, Root: types.Hash{0xEE}},
			Target:          Checkpoint{Epoch: 2, Root: types.Hash{0xFF}},
		},
		AggregationBits: bits,
		Signature:       sig,
	}
}

func TestAttScaler_NewAttestationScaler(t *testing.T) {
	as := NewAttestationScaler(nil)
	if as == nil {
		t.Fatal("expected non-nil scaler")
	}
	if as.config.MaxBufferSize != DefaultMaxBufferSize {
		t.Errorf("expected max buffer %d, got %d", DefaultMaxBufferSize, as.config.MaxBufferSize)
	}
	if as.activeWorkers != DefaultMinWorkers {
		t.Errorf("expected %d workers, got %d", DefaultMinWorkers, as.activeWorkers)
	}
}

func TestAttScaler_Submit(t *testing.T) {
	as := NewAttestationScaler(nil)
	att := makeScalerAtt(1, 0)

	err := as.Submit(att)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if as.BufferSize() != 1 {
		t.Errorf("expected buffer size 1, got %d", as.BufferSize())
	}
}

func TestAttScaler_SubmitNil(t *testing.T) {
	as := NewAttestationScaler(nil)
	err := as.Submit(nil)
	if err != ErrScalerNilAtt {
		t.Errorf("expected ErrScalerNilAtt, got %v", err)
	}
}

func TestAttScaler_SubmitStopped(t *testing.T) {
	as := NewAttestationScaler(nil)
	as.Stop()

	att := makeScalerAtt(1, 0)
	err := as.Submit(att)
	if err != ErrScalerStopped {
		t.Errorf("expected ErrScalerStopped, got %v", err)
	}
}

func TestAttScaler_SubmitBufferFull(t *testing.T) {
	as := NewAttestationScaler(&ScalerConfig{
		MaxBufferSize: 2,
		MinWorkers:    1,
		MaxWorkers:    4,
		SlotsPerEpoch: 32,
		PruneEpochs:   2,
	})

	_ = as.Submit(makeScalerAtt(1, 0))
	_ = as.Submit(makeScalerAtt(1, 1))
	err := as.Submit(makeScalerAtt(1, 2))
	if err != ErrScalerBufferFull {
		t.Errorf("expected ErrScalerBufferFull, got %v", err)
	}

	stats := as.Stats()
	if stats.DroppedCount != 1 {
		t.Errorf("expected 1 dropped, got %d", stats.DroppedCount)
	}
}

func TestAttScaler_DequeueCurrentSlot(t *testing.T) {
	as := NewAttestationScaler(nil)
	as.UpdateSlot(Slot(10))

	// Submit attestations for current slot and older slot.
	_ = as.Submit(makeScalerAtt(10, 0))
	_ = as.Submit(makeScalerAtt(10, 1))
	_ = as.Submit(makeScalerAtt(9, 2))

	// Dequeue 2: should get current slot first.
	result := as.Dequeue(2)
	if len(result) != 2 {
		t.Errorf("expected 2 dequeued, got %d", len(result))
	}

	// Both should be from slot 10.
	for _, att := range result {
		if att.Data.Slot != 10 {
			t.Errorf("expected slot 10, got %d", att.Data.Slot)
		}
	}

	// One attestation should remain.
	if as.BufferSize() != 1 {
		t.Errorf("expected 1 remaining, got %d", as.BufferSize())
	}
}

func TestAttScaler_DequeuePriority(t *testing.T) {
	as := NewAttestationScaler(nil)
	as.UpdateSlot(Slot(10))

	// Submit for slot 9 (recent) and slot 5 (older).
	_ = as.Submit(makeScalerAtt(9, 0))
	_ = as.Submit(makeScalerAtt(5, 1))

	// Dequeue: slot 9 should come before slot 5.
	result := as.Dequeue(2)
	if len(result) != 2 {
		t.Errorf("expected 2 dequeued, got %d", len(result))
	}
	if result[0].Data.Slot != 9 {
		t.Errorf("expected first dequeue from slot 9, got %d", result[0].Data.Slot)
	}
}

func TestAttScaler_DequeueEmpty(t *testing.T) {
	as := NewAttestationScaler(nil)
	result := as.Dequeue(10)
	if len(result) != 0 {
		t.Errorf("expected empty dequeue, got %d", len(result))
	}
}

func TestAttScaler_DequeueZeroCount(t *testing.T) {
	as := NewAttestationScaler(nil)
	_ = as.Submit(makeScalerAtt(1, 0))
	result := as.Dequeue(0)
	if len(result) != 0 {
		t.Errorf("expected empty dequeue for count=0, got %d", len(result))
	}
}

func TestAttScaler_ScaleUp(t *testing.T) {
	as := NewAttestationScaler(&ScalerConfig{
		MaxBufferSize: 100,
		MinWorkers:    2,
		MaxWorkers:    16,
		SlotsPerEpoch: 32,
		PruneEpochs:   2,
	})

	// Fill buffer beyond 75% threshold (76 attestations).
	for i := 0; i < 76; i++ {
		_ = as.Submit(makeScalerAtt(1, i))
	}

	// Trigger scaling check.
	as.UpdateSlot(Slot(1))

	workers := as.ActiveWorkers()
	if workers <= 2 {
		t.Errorf("expected workers > 2 after scale-up, got %d", workers)
	}
}

func TestAttScaler_ScaleDown(t *testing.T) {
	as := NewAttestationScaler(&ScalerConfig{
		MaxBufferSize: 100,
		MinWorkers:    2,
		MaxWorkers:    16,
		SlotsPerEpoch: 32,
		PruneEpochs:   2,
	})

	// Fill buffer above threshold first.
	for i := 0; i < 80; i++ {
		_ = as.Submit(makeScalerAtt(1, i))
	}
	as.UpdateSlot(Slot(1)) // triggers scale up

	// Drain most of the buffer.
	as.Dequeue(75)

	// Update slot to trigger scaling check (buffer now below 25%).
	as.UpdateSlot(Slot(2))

	workers := as.ActiveWorkers()
	if workers > 8 {
		t.Errorf("expected workers <= 8 after scale-down, got %d", workers)
	}
}

func TestAttScaler_PruneOldSlots(t *testing.T) {
	as := NewAttestationScaler(&ScalerConfig{
		MaxBufferSize: 1000,
		MinWorkers:    1,
		MaxWorkers:    4,
		SlotsPerEpoch: 32,
		PruneEpochs:   2, // 64 slots
	})

	// Submit attestations for slot 1.
	_ = as.Submit(makeScalerAtt(1, 0))
	_ = as.Submit(makeScalerAtt(1, 1))

	// Advance to slot 100 (slot 1 is older than 2 epochs = 64 slots).
	as.UpdateSlot(Slot(100))

	// Old attestations should be pruned.
	if as.BufferSize() != 0 {
		t.Errorf("expected buffer size 0 after pruning, got %d", as.BufferSize())
	}
}

func TestAttScaler_PruneKeepsRecent(t *testing.T) {
	as := NewAttestationScaler(&ScalerConfig{
		MaxBufferSize: 1000,
		MinWorkers:    1,
		MaxWorkers:    4,
		SlotsPerEpoch: 32,
		PruneEpochs:   2,
	})

	// Submit for slot 90 and slot 95.
	_ = as.Submit(makeScalerAtt(90, 0))
	_ = as.Submit(makeScalerAtt(95, 1))

	// Advance to slot 100 (both within 64-slot window).
	as.UpdateSlot(Slot(100))

	if as.BufferSize() != 2 {
		t.Errorf("expected buffer size 2 (recent kept), got %d", as.BufferSize())
	}
}

func TestAttScaler_Stats(t *testing.T) {
	as := NewAttestationScaler(nil)

	_ = as.Submit(makeScalerAtt(1, 0))
	_ = as.Submit(makeScalerAtt(1, 1))
	as.UpdateSlot(Slot(1))

	stats := as.Stats()
	if stats.QueueDepth != 2 {
		t.Errorf("expected queue depth 2, got %d", stats.QueueDepth)
	}
	if stats.ActiveWorkers < 1 {
		t.Errorf("expected at least 1 active worker, got %d", stats.ActiveWorkers)
	}
	if stats.CurrentSlot != 1 {
		t.Errorf("expected current slot 1, got %d", stats.CurrentSlot)
	}
}

func TestAttScaler_BufferUtilization(t *testing.T) {
	as := NewAttestationScaler(&ScalerConfig{
		MaxBufferSize: 10,
		MinWorkers:    1,
		MaxWorkers:    4,
		SlotsPerEpoch: 32,
		PruneEpochs:   2,
	})

	_ = as.Submit(makeScalerAtt(1, 0))
	_ = as.Submit(makeScalerAtt(1, 1))

	stats := as.Stats()
	expected := 0.2 // 2/10
	if stats.BufferUtilization < expected-0.01 || stats.BufferUtilization > expected+0.01 {
		t.Errorf("expected utilization ~%.2f, got %.2f", expected, stats.BufferUtilization)
	}
}

func TestAttScaler_ProcessedCount(t *testing.T) {
	as := NewAttestationScaler(nil)
	as.UpdateSlot(Slot(1))

	_ = as.Submit(makeScalerAtt(1, 0))
	_ = as.Submit(makeScalerAtt(1, 1))
	_ = as.Submit(makeScalerAtt(1, 2))

	as.Dequeue(2)

	stats := as.Stats()
	if stats.ProcessedCount != 2 {
		t.Errorf("expected 2 processed, got %d", stats.ProcessedCount)
	}
}

func TestAttScaler_RateAverage(t *testing.T) {
	as := NewAttestationScaler(nil)

	// Submit attestations across multiple slots.
	for slot := uint64(1); slot <= 5; slot++ {
		for i := 0; i < 100; i++ {
			_ = as.Submit(makeScalerAtt(slot, i))
		}
		as.UpdateSlot(Slot(slot))
	}

	stats := as.Stats()
	if stats.AttestationsPerSlot <= 0 {
		t.Errorf("expected positive attestation rate, got %.2f", stats.AttestationsPerSlot)
	}
}

func TestAttScaler_MinMaxWorkers(t *testing.T) {
	// Min workers should not go below config.
	as := NewAttestationScaler(&ScalerConfig{
		MaxBufferSize: 100,
		MinWorkers:    4,
		MaxWorkers:    8,
		SlotsPerEpoch: 32,
		PruneEpochs:   2,
	})

	if as.ActiveWorkers() != 4 {
		t.Errorf("expected initial workers 4, got %d", as.ActiveWorkers())
	}

	// Empty buffer: workers should stay at min.
	as.UpdateSlot(Slot(1))
	if as.ActiveWorkers() < 4 {
		t.Errorf("expected at least 4 workers, got %d", as.ActiveWorkers())
	}
}

func TestAttScaler_StopPreventsNewSubmissions(t *testing.T) {
	as := NewAttestationScaler(nil)

	_ = as.Submit(makeScalerAtt(1, 0))
	as.Stop()

	err := as.Submit(makeScalerAtt(1, 1))
	if err != ErrScalerStopped {
		t.Errorf("expected ErrScalerStopped, got %v", err)
	}

	// Existing entries should still be dequeueable.
	as.mu.Lock()
	as.currentSlot = Slot(1)
	as.mu.Unlock()
	result := as.Dequeue(10)
	if len(result) != 1 {
		t.Errorf("expected 1 entry remaining, got %d", len(result))
	}
}
