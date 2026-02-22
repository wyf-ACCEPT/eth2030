package light

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestCommitteeTrackerInitialize(t *testing.T) {
	tracker := NewCommitteeTracker(DefaultCommitteeTrackerConfig())
	if tracker.IsInitialized() {
		t.Fatal("should not be initialized before Initialize()")
	}
	if err := tracker.Initialize(MakeTestSyncCommittee(0), 0); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if !tracker.IsInitialized() {
		t.Fatal("should be initialized after Initialize()")
	}
	if tracker.CurrentPeriod() != 0 {
		t.Errorf("current period = %d, want 0", tracker.CurrentPeriod())
	}
}

func TestCommitteeTrackerInitializeNil(t *testing.T) {
	tracker := NewCommitteeTracker(DefaultCommitteeTrackerConfig())
	err := tracker.Initialize(nil, 0)
	if err != ErrTrackerNilCommittee {
		t.Errorf("expected ErrTrackerNilCommittee, got %v", err)
	}
}

func TestCommitteeTrackerGetCommittee(t *testing.T) {
	tracker := NewCommitteeTracker(DefaultCommitteeTrackerConfig())
	committee := MakeTestSyncCommittee(5)
	if err := tracker.Initialize(committee, 5); err != nil {
		t.Fatal(err)
	}

	tc, err := tracker.GetCommittee(5)
	if err != nil {
		t.Fatalf("GetCommittee failed: %v", err)
	}
	if tc.Committee != committee {
		t.Error("committee mismatch")
	}
	if tc.Period != 5 {
		t.Errorf("period = %d, want 5", tc.Period)
	}
	if tc.Root.IsZero() {
		t.Error("root should not be zero")
	}
}

func TestCommitteeTrackerGetCommitteeNotInitialized(t *testing.T) {
	tracker := NewCommitteeTracker(DefaultCommitteeTrackerConfig())
	_, err := tracker.GetCommittee(0)
	if err != ErrTrackerNotInitialized {
		t.Errorf("expected ErrTrackerNotInitialized, got %v", err)
	}
}

func TestCommitteeTrackerGetCommitteeUnknownPeriod(t *testing.T) {
	tracker := NewCommitteeTracker(DefaultCommitteeTrackerConfig())
	committee := MakeTestSyncCommittee(0)
	if err := tracker.Initialize(committee, 0); err != nil {
		t.Fatal(err)
	}

	_, err := tracker.GetCommittee(99)
	if err != ErrTrackerNoCommittee {
		t.Errorf("expected ErrTrackerNoCommittee, got %v", err)
	}
}

func TestCommitteeTrackerAdvancePeriod(t *testing.T) {
	tracker := NewCommitteeTracker(CommitteeTrackerConfig{
		MaxRetainedPeriods:   8,
		MinParticipationRate: 67,
		VerifyRoots:          false, // skip root verification for simplicity
	})
	current := MakeTestSyncCommittee(0)
	if err := tracker.Initialize(current, 0); err != nil {
		t.Fatal(err)
	}

	next, err := NextSyncCommittee(current)
	if err != nil {
		t.Fatal(err)
	}

	if err := tracker.AdvancePeriod(next, types.Hash{}); err != nil {
		t.Fatalf("AdvancePeriod failed: %v", err)
	}

	if tracker.CurrentPeriod() != 1 {
		t.Errorf("current period = %d, want 1", tracker.CurrentPeriod())
	}

	tc, err := tracker.GetCommittee(1)
	if err != nil {
		t.Fatalf("GetCommittee(1) failed: %v", err)
	}
	if tc.Period != 1 {
		t.Errorf("tracked period = %d, want 1", tc.Period)
	}
}

func TestCommitteeTrackerAdvancePeriodWithRootVerification(t *testing.T) {
	tracker := NewCommitteeTracker(CommitteeTrackerConfig{
		MaxRetainedPeriods:   8,
		MinParticipationRate: 67,
		VerifyRoots:          true,
	})
	current := MakeTestSyncCommittee(0)
	if err := tracker.Initialize(current, 0); err != nil {
		t.Fatal(err)
	}

	next, err := NextSyncCommittee(current)
	if err != nil {
		t.Fatal(err)
	}

	correctRoot := ComputeCommitteeRoot(next.Pubkeys)
	if err := tracker.AdvancePeriod(next, correctRoot); err != nil {
		t.Fatalf("AdvancePeriod with correct root failed: %v", err)
	}
}

func TestCommitteeTrackerAdvancePeriodBadRoot(t *testing.T) {
	tracker := NewCommitteeTracker(CommitteeTrackerConfig{
		MaxRetainedPeriods:   8,
		MinParticipationRate: 67,
		VerifyRoots:          true,
	})
	current := MakeTestSyncCommittee(0)
	if err := tracker.Initialize(current, 0); err != nil {
		t.Fatal(err)
	}

	next, err := NextSyncCommittee(current)
	if err != nil {
		t.Fatal(err)
	}

	badRoot := types.HexToHash("0xdeadbeef")
	err = tracker.AdvancePeriod(next, badRoot)
	if err != ErrTrackerRootMismatch {
		t.Errorf("expected ErrTrackerRootMismatch, got %v", err)
	}
}

func TestCommitteeTrackerAdvancePeriodGap(t *testing.T) {
	tracker := NewCommitteeTracker(DefaultCommitteeTrackerConfig())
	current := MakeTestSyncCommittee(0)
	if err := tracker.Initialize(current, 0); err != nil {
		t.Fatal(err)
	}

	// Try to advance to period 5 instead of 1.
	skipped := MakeTestSyncCommittee(5)
	err := tracker.AdvancePeriod(skipped, types.Hash{})
	if err != ErrTrackerPeriodGap {
		t.Errorf("expected ErrTrackerPeriodGap, got %v", err)
	}
}

func TestCommitteeTrackerAdvancePeriodNil(t *testing.T) {
	tracker := NewCommitteeTracker(DefaultCommitteeTrackerConfig())
	current := MakeTestSyncCommittee(0)
	if err := tracker.Initialize(current, 0); err != nil {
		t.Fatal(err)
	}

	err := tracker.AdvancePeriod(nil, types.Hash{})
	if err != ErrTrackerNilCommittee {
		t.Errorf("expected ErrTrackerNilCommittee, got %v", err)
	}
}

func TestCommitteeTrackerAdvancePeriodTwice(t *testing.T) {
	cfg := CommitteeTrackerConfig{MaxRetainedPeriods: 8, MinParticipationRate: 67, VerifyRoots: false}
	tracker := NewCommitteeTracker(cfg)
	current := MakeTestSyncCommittee(0)
	if err := tracker.Initialize(current, 0); err != nil {
		t.Fatal(err)
	}
	next, _ := NextSyncCommittee(current)
	if err := tracker.AdvancePeriod(next, types.Hash{}); err != nil {
		t.Fatal(err)
	}
	next2, _ := NextSyncCommittee(next)
	if err := tracker.AdvancePeriod(next2, types.Hash{}); err != nil {
		t.Fatalf("second AdvancePeriod failed: %v", err)
	}
}

func TestCommitteeTrackerCheckAggregationThreshold(t *testing.T) {
	cfg := CommitteeTrackerConfig{MaxRetainedPeriods: 8, MinParticipationRate: 67, VerifyRoots: false}
	tracker := NewCommitteeTracker(cfg)
	if err := tracker.Initialize(MakeTestSyncCommittee(0), 0); err != nil {
		t.Fatal(err)
	}
	if err := tracker.CheckAggregationThreshold(0, MakeCommitteeBits(SyncCommitteeSize), 100); err != nil {
		t.Fatalf("full participation should pass: %v", err)
	}
	if err := tracker.CheckAggregationThreshold(0, MakeCommitteeBits(344), 200); err != nil {
		t.Fatalf("2/3 participation should pass: %v", err)
	}
	err := tracker.CheckAggregationThreshold(0, MakeCommitteeBits(100), 300)
	if err != ErrTrackerThresholdNotMet {
		t.Errorf("expected ErrTrackerThresholdNotMet, got %v", err)
	}
}

func TestCommitteeTrackerCheckAggregationNotInitialized(t *testing.T) {
	tracker := NewCommitteeTracker(DefaultCommitteeTrackerConfig())
	err := tracker.CheckAggregationThreshold(0, nil, 0)
	if err != ErrTrackerNotInitialized {
		t.Errorf("expected ErrTrackerNotInitialized, got %v", err)
	}
}

func TestCommitteeTrackerCheckAggregationUnknownPeriod(t *testing.T) {
	tracker := NewCommitteeTracker(DefaultCommitteeTrackerConfig())
	committee := MakeTestSyncCommittee(0)
	if err := tracker.Initialize(committee, 0); err != nil {
		t.Fatal(err)
	}

	err := tracker.CheckAggregationThreshold(99, MakeCommitteeBits(SyncCommitteeSize), 1000)
	if err != ErrTrackerNoCommittee {
		t.Errorf("expected ErrTrackerNoCommittee, got %v", err)
	}
}

func TestCommitteeTrackerVerifyCommitteeUpdate(t *testing.T) {
	cfg := CommitteeTrackerConfig{MaxRetainedPeriods: 8, MinParticipationRate: 67, VerifyRoots: false}
	tracker := NewCommitteeTracker(cfg)
	committee := MakeTestSyncCommittee(0)
	if err := tracker.Initialize(committee, 0); err != nil {
		t.Fatal(err)
	}
	next, err := NextSyncCommittee(committee)
	if err != nil {
		t.Fatal(err)
	}
	nextRoot := ComputeCommitteeRoot(next.Pubkeys)
	bits := MakeCommitteeBits(SyncCommitteeSize)
	sig := SignSyncCommittee(committee, nextRoot, bits)
	if err := tracker.VerifyCommitteeUpdate(0, next, bits, sig); err != nil {
		t.Fatalf("VerifyCommitteeUpdate failed: %v", err)
	}
}

func TestCommitteeTrackerVerifyCommitteeUpdateBadSig(t *testing.T) {
	cfg := CommitteeTrackerConfig{MaxRetainedPeriods: 8, MinParticipationRate: 67, VerifyRoots: false}
	tracker := NewCommitteeTracker(cfg)
	if err := tracker.Initialize(MakeTestSyncCommittee(0), 0); err != nil {
		t.Fatal(err)
	}
	next, _ := NextSyncCommittee(MakeTestSyncCommittee(0))
	err := tracker.VerifyCommitteeUpdate(0, next, MakeCommitteeBits(SyncCommitteeSize), make([]byte, 32))
	if err != ErrTrackerInvalidUpdate {
		t.Errorf("expected ErrTrackerInvalidUpdate, got %v", err)
	}
}

func TestCommitteeTrackerVerifyCommitteeUpdateNil(t *testing.T) {
	tracker := NewCommitteeTracker(DefaultCommitteeTrackerConfig())
	committee := MakeTestSyncCommittee(0)
	if err := tracker.Initialize(committee, 0); err != nil {
		t.Fatal(err)
	}

	err := tracker.VerifyCommitteeUpdate(0, nil, nil, nil)
	if err != ErrTrackerNilCommittee {
		t.Errorf("expected ErrTrackerNilCommittee, got %v", err)
	}
}

func TestCommitteeTrackerEviction(t *testing.T) {
	tracker := NewCommitteeTracker(CommitteeTrackerConfig{
		MaxRetainedPeriods:   3,
		MinParticipationRate: 67,
		VerifyRoots:          false,
	})
	c0 := MakeTestSyncCommittee(0)
	if err := tracker.Initialize(c0, 0); err != nil {
		t.Fatal(err)
	}

	// Advance through periods 1..4.
	current := c0
	for i := 0; i < 4; i++ {
		next, err := NextSyncCommittee(current)
		if err != nil {
			t.Fatal(err)
		}
		if err := tracker.AdvancePeriod(next, types.Hash{}); err != nil {
			t.Fatalf("AdvancePeriod to %d failed: %v", i+1, err)
		}
		current = next
	}

	// Only the last 3 periods should be retained.
	periods := tracker.TrackedPeriods()
	if len(periods) > 3 {
		t.Errorf("expected <= 3 periods retained, got %d", len(periods))
	}

	// Period 0 should be evicted.
	_, err := tracker.GetCommittee(0)
	if err != ErrTrackerNoCommittee {
		t.Errorf("period 0 should have been evicted, got %v", err)
	}

	// Period 4 should exist.
	_, err = tracker.GetCommittee(4)
	if err != nil {
		t.Errorf("period 4 should exist: %v", err)
	}
}

func TestCommitteeTrackerParticipationHistory(t *testing.T) {
	tracker := NewCommitteeTracker(DefaultCommitteeTrackerConfig())
	committee := MakeTestSyncCommittee(0)
	if err := tracker.Initialize(committee, 0); err != nil {
		t.Fatal(err)
	}

	bits := MakeCommitteeBits(SyncCommitteeSize)
	_ = tracker.CheckAggregationThreshold(0, bits, 100)
	_ = tracker.CheckAggregationThreshold(0, bits, 200)

	history := tracker.ParticipationHistory()
	if len(history) != 2 {
		t.Fatalf("expected 2 records, got %d", len(history))
	}
	if history[0].Slot != 100 {
		t.Errorf("first record slot = %d, want 100", history[0].Slot)
	}
	if !history[0].Sufficient {
		t.Error("full participation should be sufficient")
	}
}

func TestCommitteeTrackerAverageParticipation(t *testing.T) {
	tracker := NewCommitteeTracker(DefaultCommitteeTrackerConfig())
	committee := MakeTestSyncCommittee(0)
	if err := tracker.Initialize(committee, 0); err != nil {
		t.Fatal(err)
	}

	// No records -> 0.
	if avg := tracker.AverageParticipation(); avg != 0 {
		t.Errorf("expected 0 average, got %f", avg)
	}

	// Full participation.
	bits := MakeCommitteeBits(SyncCommitteeSize)
	_ = tracker.CheckAggregationThreshold(0, bits, 100)

	avg := tracker.AverageParticipation()
	if avg < 0.99 || avg > 1.01 {
		t.Errorf("expected ~1.0 average, got %f", avg)
	}
}

func TestCommitteeTrackerStats(t *testing.T) {
	tracker := NewCommitteeTracker(CommitteeTrackerConfig{
		MaxRetainedPeriods:   8,
		MinParticipationRate: 67,
		VerifyRoots:          false,
	})
	committee := MakeTestSyncCommittee(0)
	if err := tracker.Initialize(committee, 0); err != nil {
		t.Fatal(err)
	}

	next, _ := NextSyncCommittee(committee)
	if err := tracker.AdvancePeriod(next, types.Hash{}); err != nil {
		t.Fatal(err)
	}

	updates, failures := tracker.Stats()
	if updates != 1 {
		t.Errorf("updates = %d, want 1", updates)
	}
	if failures != 0 {
		t.Errorf("failures = %d, want 0", failures)
	}

	// Force a failure by trying to advance with wrong period.
	bad := MakeTestSyncCommittee(99)
	_ = tracker.AdvancePeriod(bad, types.Hash{})

	_, failures = tracker.Stats()
	if failures != 1 {
		t.Errorf("failures = %d, want 1", failures)
	}
}

func TestComputeTransitionProof(t *testing.T) {
	r1 := types.HexToHash("0xaabbccdd")
	r2 := types.HexToHash("0xeeff0011")
	p1 := ComputeTransitionProof(r1, r2, 5)
	if p1 != ComputeTransitionProof(r1, r2, 5) {
		t.Error("should be deterministic")
	}
	if p1.IsZero() {
		t.Error("should not be zero")
	}
	if p1 == ComputeTransitionProof(r1, r2, 6) {
		t.Error("different periods should differ")
	}
	if p1 == ComputeTransitionProof(r2, r1, 5) {
		t.Error("different root order should differ")
	}
}

func TestCommitteeTrackerPeakSigners(t *testing.T) {
	cfg := CommitteeTrackerConfig{MaxRetainedPeriods: 8, MinParticipationRate: 50, VerifyRoots: false}
	tracker := NewCommitteeTracker(cfg)
	if err := tracker.Initialize(MakeTestSyncCommittee(0), 0); err != nil {
		t.Fatal(err)
	}
	_ = tracker.CheckAggregationThreshold(0, MakeCommitteeBits(300), 100)
	_ = tracker.CheckAggregationThreshold(0, MakeCommitteeBits(400), 200)
	tc, _ := tracker.GetCommittee(0)
	if tc.PeakSigners != 400 {
		t.Errorf("peak signers = %d, want 400", tc.PeakSigners)
	}
	if tc.UpdateCount != 2 {
		t.Errorf("update count = %d, want 2", tc.UpdateCount)
	}
	if tc.LastAttestSlot != 200 {
		t.Errorf("last attest slot = %d, want 200", tc.LastAttestSlot)
	}
}

func TestCommitteeTrackerDefaultConfig(t *testing.T) {
	cfg := DefaultCommitteeTrackerConfig()
	if cfg.MaxRetainedPeriods != 8 {
		t.Errorf("MaxRetainedPeriods = %d, want 8", cfg.MaxRetainedPeriods)
	}
	if cfg.MinParticipationRate != 67 {
		t.Errorf("MinParticipationRate = %d, want 67", cfg.MinParticipationRate)
	}
	if !cfg.VerifyRoots {
		t.Error("VerifyRoots should default to true")
	}
}

func TestCommitteeTrackerCommitteeRootForPeriod(t *testing.T) {
	tracker := NewCommitteeTracker(DefaultCommitteeTrackerConfig())
	committee := MakeTestSyncCommittee(0)
	if err := tracker.Initialize(committee, 0); err != nil {
		t.Fatal(err)
	}

	root, err := tracker.CommitteeRootForPeriod(0)
	if err != nil {
		t.Fatalf("CommitteeRootForPeriod failed: %v", err)
	}
	expected := ComputeCommitteeRoot(committee.Pubkeys)
	if root != expected {
		t.Error("root mismatch")
	}

	_, err = tracker.CommitteeRootForPeriod(99)
	if err != ErrTrackerNoCommittee {
		t.Errorf("expected ErrTrackerNoCommittee, got %v", err)
	}
}
