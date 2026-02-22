package consensus

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func makeTestAttestation(slot Slot, committeeIdx uint64, bits []byte) *PoolAttestation {
	epoch := Epoch(uint64(slot) / 32)
	return &PoolAttestation{
		Slot:            slot,
		CommitteeIndex:  committeeIdx,
		AggregationBits: bits,
		BeaconBlockRoot: types.HexToHash("0xabcd"),
		Source: Checkpoint{
			Epoch: epoch,
			Root:  types.HexToHash("0x1111"),
		},
		Target: Checkpoint{
			Epoch: epoch,
			Root:  types.HexToHash("0x2222"),
		},
		Signature: types.HexToHash("0x3333"),
	}
}

func TestAttestationPool_AddAndSize(t *testing.T) {
	pool := NewAttestationPool(nil)
	pool.SetCurrentSlot(10)
	pool.SetJustifiedCheckpoint(Checkpoint{Epoch: 0, Root: types.HexToHash("0x1111")})

	att := makeTestAttestation(5, 0, []byte{0x01})
	err := pool.Add(att)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	if pool.Size() != 1 {
		t.Errorf("expected size 1, got %d", pool.Size())
	}
}

func TestAttestationPool_AddNil(t *testing.T) {
	pool := NewAttestationPool(nil)
	err := pool.Add(nil)
	if err != ErrPoolAttNil {
		t.Errorf("expected ErrPoolAttNil, got %v", err)
	}
}

func TestAttestationPool_AddEmptyBits(t *testing.T) {
	pool := NewAttestationPool(nil)
	att := makeTestAttestation(5, 0, nil)
	err := pool.Add(att)
	if err != ErrPoolAttNoBits {
		t.Errorf("expected ErrPoolAttNoBits, got %v", err)
	}
}

func TestAttestationPool_SlotValidation(t *testing.T) {
	pool := NewAttestationPool(nil)
	pool.SetCurrentSlot(100)
	pool.SetJustifiedCheckpoint(Checkpoint{Epoch: Epoch(100 / 32), Root: types.HexToHash("0x1111")})

	// Too old (slot 100 - 32 = 68, attestation at slot 50 is too old).
	att := makeTestAttestation(50, 0, []byte{0x01})
	att.Source.Epoch = Epoch(100 / 32)
	att.Target.Epoch = Epoch(50 / 32)
	err := pool.Add(att)
	if err != ErrPoolAttSlotTooOld && err != ErrPoolAttTargetEpoch {
		// The target epoch won't match either, but slot-too-old fires first.
		t.Errorf("expected slot too old or target epoch error, got %v", err)
	}

	// Future slot.
	att2 := makeTestAttestation(200, 0, []byte{0x01})
	att2.Source.Epoch = Epoch(100 / 32)
	att2.Target.Epoch = Epoch(200 / 32)
	err = pool.Add(att2)
	if err != ErrPoolAttFutureSlot {
		t.Errorf("expected ErrPoolAttFutureSlot, got %v", err)
	}
}

func TestAttestationPool_SourceEpochValidation(t *testing.T) {
	pool := NewAttestationPool(nil)
	pool.SetCurrentSlot(100)
	pool.SetJustifiedCheckpoint(Checkpoint{Epoch: 2, Root: types.HexToHash("0x1111")})

	// Source epoch doesn't match justified.
	att := makeTestAttestation(96, 0, []byte{0x01})
	att.Source.Epoch = 1 // should be 2
	err := pool.Add(att)
	if err != ErrPoolAttSourceEpoch {
		t.Errorf("expected ErrPoolAttSourceEpoch, got %v", err)
	}
}

func TestAttestationPool_TargetEpochValidation(t *testing.T) {
	pool := NewAttestationPool(nil)
	pool.SetCurrentSlot(100)
	pool.SetJustifiedCheckpoint(Checkpoint{Epoch: 3, Root: types.HexToHash("0x1111")})

	// Target epoch doesn't match slot's epoch.
	att := makeTestAttestation(96, 0, []byte{0x01})
	att.Source.Epoch = 3
	att.Target.Epoch = 5 // slot 96 is epoch 3, not 5
	err := pool.Add(att)
	if err != ErrPoolAttTargetEpoch {
		t.Errorf("expected ErrPoolAttTargetEpoch, got %v", err)
	}
}

func TestAttestationPool_Aggregation(t *testing.T) {
	pool := NewAttestationPool(nil)
	pool.SetCurrentSlot(10)
	pool.SetJustifiedCheckpoint(Checkpoint{Epoch: 0, Root: types.HexToHash("0x1111")})

	// Add two attestations with same data but non-overlapping bits.
	att1 := makeTestAttestation(5, 0, []byte{0x01}) // bit 0
	att2 := makeTestAttestation(5, 0, []byte{0x02}) // bit 1

	if err := pool.Add(att1); err != nil {
		t.Fatalf("Add att1 failed: %v", err)
	}
	if err := pool.Add(att2); err != nil {
		t.Fatalf("Add att2 failed: %v", err)
	}

	// Should aggregate into a single attestation (size 1, not 2).
	if pool.Size() != 1 {
		t.Errorf("expected size 1 after aggregation, got %d", pool.Size())
	}

	// Verify aggregated bits.
	atts := pool.GetForBlock(10)
	if len(atts) != 1 {
		t.Fatalf("expected 1 attestation, got %d", len(atts))
	}
	if atts[0].AggregationBits[0] != 0x03 {
		t.Errorf("expected aggregated bits 0x03, got 0x%02x", atts[0].AggregationBits[0])
	}
}

func TestAttestationPool_NoAggregationOverlapping(t *testing.T) {
	pool := NewAttestationPool(nil)
	pool.SetCurrentSlot(10)
	pool.SetJustifiedCheckpoint(Checkpoint{Epoch: 0, Root: types.HexToHash("0x1111")})

	// Add two attestations with overlapping bits -- should not aggregate.
	att1 := makeTestAttestation(5, 0, []byte{0x03}) // bits 0,1
	att2 := makeTestAttestation(5, 0, []byte{0x02}) // bit 1 (overlaps)

	if err := pool.Add(att1); err != nil {
		t.Fatal(err)
	}
	if err := pool.Add(att2); err != nil {
		t.Fatal(err)
	}

	// Should have 2 separate attestations.
	if pool.Size() != 2 {
		t.Errorf("expected size 2 (no aggregation due to overlap), got %d", pool.Size())
	}
}

func TestAttestationPool_Deduplication(t *testing.T) {
	pool := NewAttestationPool(nil)
	pool.SetCurrentSlot(10)
	pool.SetJustifiedCheckpoint(Checkpoint{Epoch: 0, Root: types.HexToHash("0x1111")})

	att := makeTestAttestation(5, 0, []byte{0x01})
	if err := pool.Add(att); err != nil {
		t.Fatal(err)
	}

	// Mark as included.
	pool.MarkIncluded(att)

	if pool.Size() != 0 {
		t.Errorf("expected size 0 after marking included, got %d", pool.Size())
	}

	// Adding same data again should be silently dropped.
	att2 := makeTestAttestation(5, 0, []byte{0x02})
	if err := pool.Add(att2); err != nil {
		t.Fatalf("Add after include should not error, got %v", err)
	}
	if pool.Size() != 0 {
		t.Errorf("expected size 0 after adding duplicate, got %d", pool.Size())
	}
}

func TestAttestationPool_Pruning(t *testing.T) {
	cfg := DefaultAttestationPoolConfig()
	cfg.PruneSlots = 10
	pool := NewAttestationPool(cfg)
	pool.SetJustifiedCheckpoint(Checkpoint{Epoch: 0, Root: types.HexToHash("0x1111")})

	pool.SetCurrentSlot(5)
	att := makeTestAttestation(3, 0, []byte{0x01})
	if err := pool.Add(att); err != nil {
		t.Fatal(err)
	}

	if pool.Size() != 1 {
		t.Errorf("expected size 1, got %d", pool.Size())
	}

	// Advance slot past the prune threshold.
	pool.SetCurrentSlot(20)

	// Attestation at slot 3 should now be pruned (20 - 10 = 10 > 3).
	if pool.Size() != 0 {
		t.Errorf("expected size 0 after pruning, got %d", pool.Size())
	}
}

func TestAttestationPool_GetForBlock(t *testing.T) {
	pool := NewAttestationPool(nil)
	pool.SetCurrentSlot(40)
	pool.SetJustifiedCheckpoint(Checkpoint{Epoch: 1, Root: types.HexToHash("0x1111")})

	// Add attestations with different coverage.
	for i := 0; i < 5; i++ {
		bits := make([]byte, 4)
		for j := 0; j <= i; j++ {
			bits[0] |= 1 << uint(j)
		}
		att := makeTestAttestation(Slot(32+i), uint64(i), bits)
		att.Source.Epoch = 1
		att.Target.Epoch = 1
		if err := pool.Add(att); err != nil {
			t.Fatalf("Add att %d failed: %v", i, err)
		}
	}

	result := pool.GetForBlock(40)
	if len(result) != 5 {
		t.Fatalf("expected 5 attestations, got %d", len(result))
	}

	// Should be sorted by coverage (descending).
	for i := 1; i < len(result); i++ {
		if result[i-1].bitCount() < result[i].bitCount() {
			t.Errorf("attestations not sorted by coverage: %d < %d at positions %d, %d",
				result[i-1].bitCount(), result[i].bitCount(), i-1, i)
		}
	}
}

func TestAttestationPool_GetForBlock_InclusionDelay(t *testing.T) {
	pool := NewAttestationPool(nil)
	pool.SetCurrentSlot(10)
	pool.SetJustifiedCheckpoint(Checkpoint{Epoch: 0, Root: types.HexToHash("0x1111")})

	// Attestation at slot 10 -- cannot be included at slot 10 (need +1 delay).
	att := makeTestAttestation(10, 0, []byte{0x01})
	if err := pool.Add(att); err != nil {
		t.Fatal(err)
	}

	result := pool.GetForBlock(10)
	if len(result) != 0 {
		t.Errorf("expected 0 attestations (inclusion delay not met), got %d", len(result))
	}

	// Should be available at slot 11.
	result = pool.GetForBlock(11)
	if len(result) != 1 {
		t.Errorf("expected 1 attestation at slot 11, got %d", len(result))
	}
}

func TestAttestationPool_GetForBlock_MaxLimit(t *testing.T) {
	pool := NewAttestationPool(nil)
	pool.SetCurrentSlot(200)
	pool.SetJustifiedCheckpoint(Checkpoint{Epoch: 5, Root: types.HexToHash("0x1111")})

	// Add more than MaxAttestationsPerBlock attestations.
	for i := 0; i < 200; i++ {
		att := &PoolAttestation{
			Slot:            Slot(170 + (i % 20)),
			CommitteeIndex:  uint64(i),
			AggregationBits: []byte{0x01},
			BeaconBlockRoot: types.HexToHash("0xabcd"),
			Source:          Checkpoint{Epoch: 5, Root: types.HexToHash("0x1111")},
			Target:          Checkpoint{Epoch: Epoch(uint64(170+(i%20)) / 32)},
			Signature:       types.HexToHash("0x3333"),
		}
		if err := pool.Add(att); err != nil {
			t.Fatalf("Add att %d failed: %v", i, err)
		}
	}

	result := pool.GetForBlock(200)
	if len(result) > MaxAttestationsPerBlock {
		t.Errorf("expected at most %d attestations, got %d",
			MaxAttestationsPerBlock, len(result))
	}
}

func TestAttestationPool_PoolFull(t *testing.T) {
	cfg := DefaultAttestationPoolConfig()
	cfg.MaxPoolSize = 3
	pool := NewAttestationPool(cfg)
	pool.SetCurrentSlot(10)
	pool.SetJustifiedCheckpoint(Checkpoint{Epoch: 0, Root: types.HexToHash("0x1111")})

	for i := 0; i < 3; i++ {
		att := makeTestAttestation(5, uint64(i), []byte{0x01})
		if err := pool.Add(att); err != nil {
			t.Fatalf("Add %d failed: %v", i, err)
		}
	}

	// Pool should be full.
	att := makeTestAttestation(5, 99, []byte{0x01})
	err := pool.Add(att)
	if err != ErrPoolFull {
		t.Errorf("expected ErrPoolFull, got %v", err)
	}
}

func TestAttestationPool_ThreadSafety(t *testing.T) {
	pool := NewAttestationPool(nil)
	pool.SetCurrentSlot(100)
	pool.SetJustifiedCheckpoint(Checkpoint{Epoch: 3, Root: types.HexToHash("0x1111")})

	var wg sync.WaitGroup
	errs := make(chan error, 200)

	// Concurrent adds.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			att := &PoolAttestation{
				Slot:            Slot(96 + (idx % 4)),
				CommitteeIndex:  uint64(idx),
				AggregationBits: []byte{byte(idx + 1)},
				BeaconBlockRoot: types.HexToHash("0xabcd"),
				Source:          Checkpoint{Epoch: 3, Root: types.HexToHash("0x1111")},
				Target:          Checkpoint{Epoch: Epoch(uint64(96+(idx%4)) / 32)},
				Signature:       types.HexToHash("0x3333"),
			}
			if err := pool.Add(att); err != nil {
				errs <- err
			}
		}(i)
	}

	// Concurrent reads.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = pool.GetForBlock(100)
			_ = pool.Size()
			_ = pool.KeyCount()
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent error: %v", err)
	}
}

func TestAttestationPool_KeyCount(t *testing.T) {
	pool := NewAttestationPool(nil)
	pool.SetCurrentSlot(10)
	pool.SetJustifiedCheckpoint(Checkpoint{Epoch: 0, Root: types.HexToHash("0x1111")})

	// Add two attestations with different committee indices (different keys).
	att1 := makeTestAttestation(5, 0, []byte{0x01})
	att2 := makeTestAttestation(5, 1, []byte{0x01})

	pool.Add(att1)
	pool.Add(att2)

	if pool.KeyCount() != 2 {
		t.Errorf("expected 2 keys, got %d", pool.KeyCount())
	}
}

func TestAttestationPool_BitCount(t *testing.T) {
	att := &PoolAttestation{
		AggregationBits: []byte{0xFF, 0x0F},
	}
	count := att.bitCount()
	if count != 12 {
		t.Errorf("expected 12 bits, got %d", count)
	}

	att2 := &PoolAttestation{
		AggregationBits: []byte{0x00},
	}
	if att2.bitCount() != 0 {
		t.Errorf("expected 0 bits, got %d", att2.bitCount())
	}
}

func TestPopcount(t *testing.T) {
	tests := []struct {
		b    byte
		want int
	}{
		{0x00, 0},
		{0x01, 1},
		{0xFF, 8},
		{0x55, 4},
		{0xAA, 4},
		{0x80, 1},
	}
	for _, tt := range tests {
		got := popcount(tt.b)
		if got != tt.want {
			t.Errorf("popcount(0x%02x) = %d, want %d", tt.b, got, tt.want)
		}
	}
}

func TestAttestationPool_MarkIncludedNil(t *testing.T) {
	pool := NewAttestationPool(nil)
	// Should not panic.
	pool.MarkIncluded(nil)
}

func TestAttestationPool_GetForBlockEmpty(t *testing.T) {
	pool := NewAttestationPool(nil)
	result := pool.GetForBlock(10)
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d", len(result))
	}
}

func TestAttestationPool_PruneIncludedMarkers(t *testing.T) {
	cfg := DefaultAttestationPoolConfig()
	cfg.PruneSlots = 5
	pool := NewAttestationPool(cfg)
	pool.SetJustifiedCheckpoint(Checkpoint{Epoch: 0, Root: types.HexToHash("0x1111")})

	pool.SetCurrentSlot(5)
	att := makeTestAttestation(3, 0, []byte{0x01})
	pool.Add(att)
	pool.MarkIncluded(att)

	// Advance past prune threshold.
	pool.SetCurrentSlot(20)

	// The included marker for slot 3 should be cleaned up.
	pool.mu.RLock()
	_, exists := pool.included[att.dataKey()]
	pool.mu.RUnlock()

	if exists {
		t.Error("expected included marker to be pruned")
	}
}
