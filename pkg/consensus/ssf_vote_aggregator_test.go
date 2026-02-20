package consensus

import (
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// Helper to create a test hash for vote aggregator tests.
func aggTestRoot(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	return h
}

// Helper to create validator stakes map.
func aggTestStakes(n int, stakeEach uint64) map[uint64]uint64 {
	s := make(map[uint64]uint64, n)
	for i := 0; i < n; i++ {
		s[uint64(i)] = stakeEach
	}
	return s
}

// --- SlotTimeline ---

func TestSlotTimeline_DefaultPhases(t *testing.T) {
	tl := DefaultSlotTimeline(10)
	if tl.SlotNumber != 10 {
		t.Errorf("SlotNumber = %d, want 10", tl.SlotNumber)
	}
	if tl.CurrentPhase != PhaseSlotPropose {
		t.Errorf("initial phase = %v, want propose", tl.CurrentPhase)
	}
	if tl.SlotDurationMs != 6000 {
		t.Errorf("SlotDurationMs = %d, want 6000", tl.SlotDurationMs)
	}
	if tl.PhaseDurationMs != 2000 {
		t.Errorf("PhaseDurationMs = %d, want 2000", tl.PhaseDurationMs)
	}
}

func TestSlotTimeline_AdvancePhase(t *testing.T) {
	tl := DefaultSlotTimeline(1)

	// Propose -> Attest.
	ok := tl.AdvancePhase()
	if !ok || tl.CurrentPhase != PhaseSlotAttest {
		t.Errorf("advance 1: ok=%v, phase=%v, want attest", ok, tl.CurrentPhase)
	}

	// Attest -> Finalize.
	ok = tl.AdvancePhase()
	if !ok || tl.CurrentPhase != PhaseSlotFinalize {
		t.Errorf("advance 2: ok=%v, phase=%v, want finalize", ok, tl.CurrentPhase)
	}

	// Finalize -> cannot advance.
	ok = tl.AdvancePhase()
	if ok {
		t.Error("should not advance past finalize phase")
	}
}

func TestSlotTimeline_PhaseAtOffset(t *testing.T) {
	tl := DefaultSlotTimeline(1)

	tests := []struct {
		offsetMs uint64
		want     SlotPhaseKind
	}{
		{0, PhaseSlotPropose},
		{1000, PhaseSlotPropose},
		{1999, PhaseSlotPropose},
		{2000, PhaseSlotAttest},
		{3000, PhaseSlotAttest},
		{4000, PhaseSlotFinalize},
		{5999, PhaseSlotFinalize},
		{6000, PhaseSlotFinalize}, // beyond slot, clamp to finalize
		{9999, PhaseSlotFinalize},
	}

	for _, tt := range tests {
		got := tl.PhaseAtOffset(tt.offsetMs)
		if got != tt.want {
			t.Errorf("PhaseAtOffset(%d) = %v, want %v", tt.offsetMs, got, tt.want)
		}
	}
}

func TestSlotPhaseKind_String(t *testing.T) {
	if PhaseSlotPropose.String() != "propose" {
		t.Errorf("got %q", PhaseSlotPropose.String())
	}
	if PhaseSlotAttest.String() != "attest" {
		t.Errorf("got %q", PhaseSlotAttest.String())
	}
	if PhaseSlotFinalize.String() != "finalize" {
		t.Errorf("got %q", PhaseSlotFinalize.String())
	}
	if SlotPhaseKind(99).String() != "unknown" {
		t.Errorf("got %q", SlotPhaseKind(99).String())
	}
}

// --- QuorumTracker ---

func TestQuorumTracker_Basic(t *testing.T) {
	qt := NewQuorumTracker(1, 300, 2, 3) // 2/3 of 300 = 200 required
	if qt.RequiredStake != 200 {
		t.Errorf("RequiredStake = %d, want 200", qt.RequiredStake)
	}
	if qt.QuorumReached {
		t.Error("quorum should not be reached initially")
	}
}

func TestQuorumTracker_ReachesQuorum(t *testing.T) {
	qt := NewQuorumTracker(1, 300, 2, 3)
	root := aggTestRoot(0xAA)

	reached := qt.RecordVote(root, 100)
	if reached {
		t.Error("100/200 should not reach quorum")
	}

	reached = qt.RecordVote(root, 100)
	if !reached {
		t.Error("200/200 should reach quorum")
	}

	if !qt.QuorumReached {
		t.Error("quorum flag should be set")
	}
	if qt.LeadingRoot != root {
		t.Errorf("leading root mismatch")
	}
}

func TestQuorumTracker_MultipleRoots(t *testing.T) {
	qt := NewQuorumTracker(1, 300, 2, 3)
	rootA := aggTestRoot(0xAA)
	rootB := aggTestRoot(0xBB)

	qt.RecordVote(rootA, 100)
	qt.RecordVote(rootB, 90)
	qt.RecordVote(rootA, 100) // rootA now 200, meets quorum

	if !qt.QuorumReached {
		t.Error("rootA should meet quorum with 200")
	}
	if qt.LeadingRoot != rootA {
		t.Error("leading root should be rootA")
	}
}

func TestQuorumTracker_Progress(t *testing.T) {
	qt := NewQuorumTracker(1, 300, 2, 3)
	root := aggTestRoot(0xCC)

	qt.RecordVote(root, 100)
	progress := qt.Progress()
	if progress < 49.9 || progress > 50.1 {
		t.Errorf("Progress = %f, want ~50.0", progress)
	}

	qt.RecordVote(root, 100)
	progress = qt.Progress()
	if progress < 99.9 || progress > 100.1 {
		t.Errorf("Progress = %f, want ~100.0", progress)
	}
}

func TestQuorumTracker_ZeroRequired(t *testing.T) {
	qt := NewQuorumTracker(1, 0, 2, 0) // den=0 -> required=0
	progress := qt.Progress()
	if progress != 0 {
		t.Errorf("Progress = %f, want 0 when required is 0", progress)
	}
}

// --- SSFCommitteeSlot ---

func TestAssignSSFCommittee_Basic(t *testing.T) {
	validators := []uint64{0, 1, 2, 3, 4}
	stakes := aggTestStakes(5, 100)

	c := AssignSSFCommittee(0, 0, validators, stakes)
	if len(c.Members) != 5 {
		t.Errorf("members count = %d, want 5", len(c.Members))
	}
	if c.TotalStake != 500 {
		t.Errorf("TotalStake = %d, want 500", c.TotalStake)
	}
}

func TestAssignSSFCommittee_RotationBySlot(t *testing.T) {
	validators := []uint64{10, 20, 30}
	stakes := map[uint64]uint64{10: 100, 20: 100, 30: 100}

	c0 := AssignSSFCommittee(0, 0, validators, stakes)
	c1 := AssignSSFCommittee(1, 0, validators, stakes)

	// Slot 0 and slot 1 should produce different orderings.
	if c0.Members[0] == c1.Members[0] && c0.Members[1] == c1.Members[1] {
		t.Error("different slots should produce different committee orderings")
	}
}

func TestAssignSSFCommittee_Empty(t *testing.T) {
	c := AssignSSFCommittee(0, 0, nil, nil)
	if len(c.Members) != 0 {
		t.Error("empty validators should produce empty committee")
	}
}

// --- VoteAggregator ---

func TestVoteAggregator_SubmitVote(t *testing.T) {
	va := NewVoteAggregator(2, 3, 300)
	va.SetValidatorStakes(aggTestStakes(3, 100))
	va.InitSlot(1)

	vote := &AggregatedVote{ValidatorIndex: 0, Slot: 1, BlockRoot: aggTestRoot(0x01)}
	if err := va.SubmitVote(vote); err != nil {
		t.Fatalf("SubmitVote failed: %v", err)
	}
	if va.VoteCount(1) != 1 {
		t.Errorf("VoteCount = %d, want 1", va.VoteCount(1))
	}
}

func TestVoteAggregator_NilVote(t *testing.T) {
	va := NewVoteAggregator(2, 3, 300)
	if err := va.SubmitVote(nil); err != ErrAggNilVote {
		t.Errorf("got %v, want ErrAggNilVote", err)
	}
}

func TestVoteAggregator_DuplicateVote(t *testing.T) {
	va := NewVoteAggregator(2, 3, 300)
	va.SetValidatorStakes(aggTestStakes(3, 100))

	vote := &AggregatedVote{ValidatorIndex: 0, Slot: 1, BlockRoot: aggTestRoot(0x01)}
	va.SubmitVote(vote)
	err := va.SubmitVote(vote)
	if err == nil {
		t.Error("expected duplicate vote error")
	}
}

func TestVoteAggregator_UnknownValidator(t *testing.T) {
	va := NewVoteAggregator(2, 3, 300)
	va.SetValidatorStakes(aggTestStakes(3, 100))

	vote := &AggregatedVote{ValidatorIndex: 99, Slot: 1, BlockRoot: aggTestRoot(0x01)}
	err := va.SubmitVote(vote)
	if err == nil {
		t.Error("expected unknown validator error")
	}
}

func TestVoteAggregator_FinalizedSlotRejectsVotes(t *testing.T) {
	va := NewVoteAggregator(2, 3, 300)
	va.SetValidatorStakes(aggTestStakes(3, 100))
	va.InitSlot(1)

	root := aggTestRoot(0xAA)
	va.SubmitVote(&AggregatedVote{ValidatorIndex: 0, Slot: 1, BlockRoot: root})
	va.SubmitVote(&AggregatedVote{ValidatorIndex: 1, Slot: 1, BlockRoot: root})

	// Decide finality (should finalize with 200/300 >= 2/3).
	decision, err := va.DecideFinality(1)
	if err != nil {
		t.Fatalf("DecideFinality error: %v", err)
	}
	if !decision.IsFinalized {
		t.Error("should be finalized with 2/3 stake")
	}

	// Try voting after finality.
	err = va.SubmitVote(&AggregatedVote{ValidatorIndex: 2, Slot: 1, BlockRoot: root})
	if err == nil {
		t.Error("expected error when voting on finalized slot")
	}
}

func TestVoteAggregator_DecideFinality_NotMet(t *testing.T) {
	va := NewVoteAggregator(2, 3, 300)
	va.SetValidatorStakes(aggTestStakes(3, 100))
	va.InitSlot(1)

	va.SubmitVote(&AggregatedVote{ValidatorIndex: 0, Slot: 1, BlockRoot: aggTestRoot(0x01)})

	decision, err := va.DecideFinality(1)
	if err != nil {
		t.Fatalf("DecideFinality error: %v", err)
	}
	if decision.IsFinalized {
		t.Error("should not be finalized with only 100/300")
	}
}

func TestVoteAggregator_DecideFinality_ZeroStake(t *testing.T) {
	va := NewVoteAggregator(2, 3, 0)
	_, err := va.DecideFinality(1)
	if err != ErrAggZeroTotalStake {
		t.Errorf("got %v, want ErrAggZeroTotalStake", err)
	}
}

func TestVoteAggregator_DecideFinality_EmptySlot(t *testing.T) {
	va := NewVoteAggregator(2, 3, 300)
	decision, err := va.DecideFinality(99)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.IsFinalized {
		t.Error("empty slot should not be finalized")
	}
	if decision.RequiredStake != 200 {
		t.Errorf("RequiredStake = %d, want 200", decision.RequiredStake)
	}
}

func TestVoteAggregator_CheckQuorum(t *testing.T) {
	va := NewVoteAggregator(2, 3, 300)
	va.SetValidatorStakes(aggTestStakes(3, 100))
	va.InitSlot(1)

	root := aggTestRoot(0xBB)
	va.SubmitVote(&AggregatedVote{ValidatorIndex: 0, Slot: 1, BlockRoot: root})
	va.SubmitVote(&AggregatedVote{ValidatorIndex: 1, Slot: 1, BlockRoot: root})

	qt := va.CheckQuorum(1)
	if !qt.QuorumReached {
		t.Error("quorum should be reached with 200/200 required")
	}
	if qt.VoteCount != 2 {
		t.Errorf("VoteCount = %d, want 2", qt.VoteCount)
	}
}

func TestVoteAggregator_AdvanceSlotPhase(t *testing.T) {
	va := NewVoteAggregator(2, 3, 300)
	va.InitSlot(1)

	tl := va.GetTimeline(1)
	if tl.CurrentPhase != PhaseSlotPropose {
		t.Errorf("initial phase = %v, want propose", tl.CurrentPhase)
	}

	ok := va.AdvanceSlotPhase(1)
	if !ok {
		t.Error("should advance from propose to attest")
	}

	tl = va.GetTimeline(1)
	if tl.CurrentPhase != PhaseSlotAttest {
		t.Errorf("phase = %v, want attest", tl.CurrentPhase)
	}
}

func TestVoteAggregator_PurgeSlot(t *testing.T) {
	va := NewVoteAggregator(2, 3, 300)
	va.SetValidatorStakes(aggTestStakes(3, 100))
	va.InitSlot(1)
	va.SubmitVote(&AggregatedVote{ValidatorIndex: 0, Slot: 1, BlockRoot: aggTestRoot(0x01)})

	va.PurgeSlot(1)
	if va.VoteCount(1) != 0 {
		t.Error("purged slot should have 0 votes")
	}
}

func TestVoteAggregator_IsSlotFinalized(t *testing.T) {
	va := NewVoteAggregator(2, 3, 300)
	va.SetValidatorStakes(aggTestStakes(3, 100))

	if va.IsSlotFinalized(1) {
		t.Error("slot 1 should not be finalized initially")
	}

	root := aggTestRoot(0xCC)
	va.SubmitVote(&AggregatedVote{ValidatorIndex: 0, Slot: 1, BlockRoot: root})
	va.SubmitVote(&AggregatedVote{ValidatorIndex: 1, Slot: 1, BlockRoot: root})
	va.DecideFinality(1)

	if !va.IsSlotFinalized(1) {
		t.Error("slot 1 should be finalized after decision")
	}
}

func TestVoteAggregator_ConcurrentVotes(t *testing.T) {
	va := NewVoteAggregator(2, 3, 100_000)
	numValidators := 200
	stakes := make(map[uint64]uint64, numValidators)
	for i := 0; i < numValidators; i++ {
		stakes[uint64(i)] = 10
	}
	va.SetValidatorStakes(stakes)
	va.InitSlot(50)

	root := aggTestRoot(0xFF)
	var wg sync.WaitGroup
	errCh := make(chan error, numValidators)

	for i := 0; i < numValidators; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			vote := &AggregatedVote{
				ValidatorIndex: uint64(idx),
				Slot:           50,
				BlockRoot:      root,
			}
			if err := va.SubmitVote(vote); err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent vote error: %v", err)
	}

	if va.VoteCount(50) != numValidators {
		t.Errorf("VoteCount = %d, want %d", va.VoteCount(50), numValidators)
	}
}

func TestVoteAggregator_DecideFinality_Idempotent(t *testing.T) {
	va := NewVoteAggregator(2, 3, 300)
	va.SetValidatorStakes(aggTestStakes(3, 100))

	root := aggTestRoot(0xDD)
	va.SubmitVote(&AggregatedVote{ValidatorIndex: 0, Slot: 1, BlockRoot: root})
	va.SubmitVote(&AggregatedVote{ValidatorIndex: 1, Slot: 1, BlockRoot: root})

	d1, _ := va.DecideFinality(1)
	d2, _ := va.DecideFinality(1)

	if d1.IsFinalized != d2.IsFinalized || d1.BlockRoot != d2.BlockRoot {
		t.Error("DecideFinality should be idempotent")
	}
}

func TestVoteAggregator_Participation(t *testing.T) {
	va := NewVoteAggregator(2, 3, 300)
	va.SetValidatorStakes(aggTestStakes(3, 100))

	root := aggTestRoot(0xEE)
	va.SubmitVote(&AggregatedVote{ValidatorIndex: 0, Slot: 1, BlockRoot: root})
	va.SubmitVote(&AggregatedVote{ValidatorIndex: 1, Slot: 1, BlockRoot: root})
	va.SubmitVote(&AggregatedVote{ValidatorIndex: 2, Slot: 1, BlockRoot: root})

	decision, _ := va.DecideFinality(1)
	if decision.Participation != 100.0 {
		t.Errorf("Participation = %f, want 100.0", decision.Participation)
	}
}

func TestVoteAggregator_AutoInitSlot(t *testing.T) {
	va := NewVoteAggregator(2, 3, 300)
	va.SetValidatorStakes(aggTestStakes(3, 100))

	// Submit vote without calling InitSlot -- should auto-init.
	vote := &AggregatedVote{ValidatorIndex: 0, Slot: 7, BlockRoot: aggTestRoot(0x07)}
	if err := va.SubmitVote(vote); err != nil {
		t.Fatalf("SubmitVote failed without InitSlot: %v", err)
	}
	if va.VoteCount(7) != 1 {
		t.Errorf("VoteCount = %d, want 1", va.VoteCount(7))
	}
}
