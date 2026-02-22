package consensus

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// makeTestAttV2 creates a test attestation for the v2 pool with distinct data.
func makeTestAttV2(slot Slot, committee uint64, bits []byte) *PoolAttestation {
	epoch := Epoch(uint64(slot) / 32)
	return &PoolAttestation{
		Slot:            slot,
		CommitteeIndex:  committee,
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

func TestAttestationPoolV2_AddAndSize(t *testing.T) {
	pool := NewAttestationPoolV2(nil)
	pool.SetCurrentSlot(10)

	att := makeTestAttV2(5, 0, []byte{0x01})
	if err := pool.Add(att); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if pool.Size() != 1 {
		t.Errorf("size = %d, want 1", pool.Size())
	}
}

func TestAttestationPoolV2_AddNil(t *testing.T) {
	pool := NewAttestationPoolV2(nil)
	if err := pool.Add(nil); err != ErrPoolV2AttNil {
		t.Errorf("expected ErrPoolV2AttNil, got %v", err)
	}
}

func TestAttestationPoolV2_AddEmptyBits(t *testing.T) {
	pool := NewAttestationPoolV2(nil)
	att := makeTestAttV2(5, 0, nil)
	if err := pool.Add(att); err != ErrPoolV2AttNoBits {
		t.Errorf("expected ErrPoolV2AttNoBits, got %v", err)
	}
}

func TestAttestationPoolV2_AddInvalidCommittee(t *testing.T) {
	pool := NewAttestationPoolV2(nil)
	att := makeTestAttV2(5, MaxCommitteesPerSlotV2, []byte{0x01})
	if err := pool.Add(att); err != ErrPoolV2InvalidCommittee {
		t.Errorf("expected ErrPoolV2InvalidCommittee, got %v", err)
	}
}

func TestAttestationPoolV2_DuplicateDetection(t *testing.T) {
	pool := NewAttestationPoolV2(nil)
	pool.SetCurrentSlot(10)

	att := makeTestAttV2(5, 0, []byte{0x01})
	if err := pool.Add(att); err != nil {
		t.Fatalf("first Add failed: %v", err)
	}

	// Same attestation again should be duplicate.
	err := pool.Add(att)
	if err != ErrPoolV2AttDuplicate {
		t.Errorf("expected ErrPoolV2AttDuplicate, got %v", err)
	}
	if pool.Size() != 1 {
		t.Errorf("size should still be 1, got %d", pool.Size())
	}
}

func TestAttestationPoolV2_SlotTooOld(t *testing.T) {
	pool := NewAttestationPoolV2(nil)
	pool.SetCurrentSlot(100)

	// Slot 50 with MaxInclusionDelay=32 is too old (50 + 32 = 82 < 100).
	att := makeTestAttV2(50, 0, []byte{0x01})
	if err := pool.Add(att); err != ErrPoolV2AttSlotTooOld {
		t.Errorf("expected ErrPoolV2AttSlotTooOld, got %v", err)
	}
}

func TestAttestationPoolV2_FutureSlot(t *testing.T) {
	pool := NewAttestationPoolV2(nil)
	pool.SetCurrentSlot(10)

	att := makeTestAttV2(20, 0, []byte{0x01})
	if err := pool.Add(att); err != ErrPoolV2AttFutureSlot {
		t.Errorf("expected ErrPoolV2AttFutureSlot, got %v", err)
	}
}

func TestAttestationPoolV2_PoolCapacity(t *testing.T) {
	cfg := &AttestationPoolV2Config{
		MaxPoolSize:       3,
		MaxInclusionDelay: MaxInclusionDelay,
		SlotsPerEpoch:     32,
	}
	pool := NewAttestationPoolV2(cfg)
	pool.SetCurrentSlot(10)

	for i := uint64(0); i < 3; i++ {
		att := makeTestAttV2(5, i, []byte{byte(i + 1)})
		if err := pool.Add(att); err != nil {
			t.Fatalf("Add %d failed: %v", i, err)
		}
	}
	if pool.Size() != 3 {
		t.Fatalf("size = %d, want 3", pool.Size())
	}

	// Pool is full.
	att := makeTestAttV2(6, 10, []byte{0xff})
	if err := pool.Add(att); err != ErrPoolV2Full {
		t.Errorf("expected ErrPoolV2Full, got %v", err)
	}
}

func TestAttestationPoolV2_Aggregation(t *testing.T) {
	pool := NewAttestationPoolV2(nil)
	pool.SetCurrentSlot(10)

	// First attestation with bit 0 set.
	att1 := makeTestAttV2(5, 0, []byte{0x01})
	if err := pool.Add(att1); err != nil {
		t.Fatalf("Add att1 failed: %v", err)
	}

	// Second attestation with same data key but bit 1 set (non-overlapping).
	att2 := makeTestAttV2(5, 0, []byte{0x02})
	if err := pool.Add(att2); err != nil {
		t.Fatalf("Add att2 failed: %v", err)
	}

	// Size should still be 1 if aggregated.
	if pool.Size() != 1 {
		t.Errorf("size after aggregation = %d, want 1", pool.Size())
	}
}

func TestAttestationPoolV2_GetBestForCommittee(t *testing.T) {
	pool := NewAttestationPoolV2(nil)
	pool.SetCurrentSlot(10)

	// Add attestations with different bit counts for the same committee.
	att1 := makeTestAttV2(5, 0, []byte{0x01}) // 1 bit set
	att1.BeaconBlockRoot = types.HexToHash("0xaa01")
	pool.Add(att1)

	att2 := makeTestAttV2(5, 0, []byte{0x07}) // 3 bits set
	att2.BeaconBlockRoot = types.HexToHash("0xaa02")
	pool.Add(att2)

	best := pool.GetBestForCommittee(5, 0)
	if best == nil {
		t.Fatal("GetBestForCommittee returned nil")
	}
	if best.bitCount() < att1.bitCount() {
		t.Errorf("best bit count = %d, should be >= %d", best.bitCount(), att1.bitCount())
	}
}

func TestAttestationPoolV2_GetBestForCommittee_Empty(t *testing.T) {
	pool := NewAttestationPoolV2(nil)
	best := pool.GetBestForCommittee(5, 0)
	if best != nil {
		t.Error("expected nil for empty pool")
	}
}

func TestAttestationPoolV2_CommitteeCount(t *testing.T) {
	pool := NewAttestationPoolV2(nil)
	pool.SetCurrentSlot(10)

	pool.Add(makeTestAttV2(5, 0, []byte{0x01}))
	pool.Add(makeTestAttV2(5, 1, []byte{0x02}))
	pool.Add(makeTestAttV2(6, 0, []byte{0x03}))

	if pool.CommitteeCount() != 3 {
		t.Errorf("committee count = %d, want 3", pool.CommitteeCount())
	}
}

func TestAttestationPoolV2_GetScoredForBlock(t *testing.T) {
	pool := NewAttestationPoolV2(nil)
	pool.SetCurrentSlot(10)

	// Add attestation at slot 8, inclusion at slot 10 -> delay=2.
	att := makeTestAttV2(8, 0, []byte{0x07}) // 3 bits set
	pool.Add(att)

	scored := pool.GetScoredForBlock(10, 10)
	if len(scored) == 0 {
		t.Fatal("expected at least one scored attestation")
	}

	sa := scored[0]
	if sa.InclusionDelay != 2 {
		t.Errorf("inclusion delay = %d, want 2", sa.InclusionDelay)
	}
	if sa.BitCount != 3 {
		t.Errorf("bit count = %d, want 3", sa.BitCount)
	}
	if sa.Score == 0 {
		t.Error("score should be > 0")
	}
}

func TestAttestationPoolV2_GetScoredForBlock_Ordering(t *testing.T) {
	pool := NewAttestationPoolV2(nil)
	pool.SetCurrentSlot(10)

	// Attestation with high coverage (7 bits) at slot 8 (delay=2).
	highCov := makeTestAttV2(8, 0, []byte{0x7f})
	highCov.BeaconBlockRoot = types.HexToHash("0xaa01")
	pool.Add(highCov)

	// Attestation with low coverage (1 bit) at slot 9 (delay=1).
	lowCov := makeTestAttV2(9, 1, []byte{0x01})
	lowCov.BeaconBlockRoot = types.HexToHash("0xaa02")
	pool.Add(lowCov)

	scored := pool.GetScoredForBlock(10, 10)
	if len(scored) < 2 {
		t.Fatalf("expected at least 2 scored attestations, got %d", len(scored))
	}

	// Higher score should come first.
	if scored[0].Score < scored[1].Score {
		t.Error("scored attestations should be ordered by score descending")
	}
}

func TestAttestationPoolV2_MarkIncludedV2(t *testing.T) {
	pool := NewAttestationPoolV2(nil)
	pool.SetCurrentSlot(10)

	att := makeTestAttV2(8, 0, []byte{0x03})
	pool.Add(att)
	if pool.Size() != 1 {
		t.Fatalf("size before mark = %d, want 1", pool.Size())
	}

	pool.MarkIncludedV2(att, 10)

	// Attestation should be removed from the pool.
	if pool.Size() != 0 {
		t.Errorf("size after mark = %d, want 0", pool.Size())
	}

	// Delay stats should be updated.
	stats := pool.GetDelayStats()
	if stats.TotalAttestations != 1 {
		t.Errorf("total attestations = %d, want 1", stats.TotalAttestations)
	}
	expectedDelay := uint64(10 - 8) // 2
	if stats.TotalDelay != expectedDelay {
		t.Errorf("total delay = %d, want %d", stats.TotalDelay, expectedDelay)
	}
}

func TestAttestationPoolV2_MarkIncludedV2_Nil(t *testing.T) {
	pool := NewAttestationPoolV2(nil)
	// Should not panic.
	pool.MarkIncludedV2(nil, 10)
}

func TestAttestationPoolV2_Pruning(t *testing.T) {
	pool := NewAttestationPoolV2(nil)
	pool.SetCurrentSlot(10)

	att := makeTestAttV2(5, 0, []byte{0x01})
	pool.Add(att)
	if pool.Size() != 1 {
		t.Fatalf("size = %d, want 1", pool.Size())
	}

	// Advance current slot past inclusion window: slot 5 + 32 = 37.
	// At slot 50, slot 5 is too old (50 - 32 = 18 > 5).
	pool.SetCurrentSlot(50)
	if pool.Size() != 0 {
		t.Errorf("size after pruning = %d, want 0", pool.Size())
	}
}

func TestAttestationPoolV2_InclusionDelayStats(t *testing.T) {
	pool := NewAttestationPoolV2(nil)
	pool.SetCurrentSlot(20)

	// Initial stats should be zeroed.
	stats := pool.GetDelayStats()
	if stats.TotalAttestations != 0 || stats.MinDelay != 0 {
		t.Error("initial stats should be zeroed")
	}

	// Include attestation at optimal delay.
	att1 := makeTestAttV2(18, 0, []byte{0x01})
	pool.Add(att1)
	pool.MarkIncludedV2(att1, 19) // delay=1 (optimal)

	// Include attestation at delay=3.
	att2 := makeTestAttV2(15, 1, []byte{0x02})
	pool.Add(att2)
	pool.MarkIncludedV2(att2, 18) // delay=3

	stats = pool.GetDelayStats()
	if stats.TotalAttestations != 2 {
		t.Errorf("total = %d, want 2", stats.TotalAttestations)
	}
	if stats.MinDelay != 1 {
		t.Errorf("min delay = %d, want 1", stats.MinDelay)
	}
	if stats.MaxDelay != 3 {
		t.Errorf("max delay = %d, want 3", stats.MaxDelay)
	}
	if stats.OptimalCount != 1 {
		t.Errorf("optimal count = %d, want 1", stats.OptimalCount)
	}
}

func TestInclusionDelayStats_AverageAndRate(t *testing.T) {
	s := &InclusionDelayStats{
		TotalAttestations: 4,
		TotalDelay:        10,
		OptimalCount:      2,
	}
	avg := s.AverageDelay()
	if avg != 2.5 {
		t.Errorf("average delay = %f, want 2.5", avg)
	}
	rate := s.OptimalRate()
	if rate != 0.5 {
		t.Errorf("optimal rate = %f, want 0.5", rate)
	}

	// Zero case.
	empty := &InclusionDelayStats{}
	if empty.AverageDelay() != 0 {
		t.Error("average delay on empty should be 0")
	}
	if empty.OptimalRate() != 0 {
		t.Error("optimal rate on empty should be 0")
	}
}

func TestAttestationPoolV2_DefaultConfig(t *testing.T) {
	cfg := DefaultAttestationPoolV2Config()
	if cfg.MaxPoolSize != DefaultMaxPoolSizeV2 {
		t.Errorf("MaxPoolSize = %d, want %d", cfg.MaxPoolSize, DefaultMaxPoolSizeV2)
	}
	if cfg.MaxInclusionDelay != MaxInclusionDelay {
		t.Errorf("MaxInclusionDelay = %d, want %d", cfg.MaxInclusionDelay, MaxInclusionDelay)
	}
	if cfg.SlotsPerEpoch != 32 {
		t.Errorf("SlotsPerEpoch = %d, want 32", cfg.SlotsPerEpoch)
	}
}

func TestAttestationPoolV2_IncludedSkipped(t *testing.T) {
	pool := NewAttestationPoolV2(nil)
	pool.SetCurrentSlot(10)

	att := makeTestAttV2(5, 0, []byte{0x01})
	pool.Add(att)
	pool.MarkIncludedV2(att, 7)

	// Adding the same data key again should be silently skipped.
	att2 := makeTestAttV2(5, 0, []byte{0x03})
	err := pool.Add(att2)
	if err != nil {
		t.Errorf("expected nil (silently skipped), got %v", err)
	}
	if pool.Size() != 0 {
		t.Errorf("size should remain 0 after adding already-included data, got %d", pool.Size())
	}
}
