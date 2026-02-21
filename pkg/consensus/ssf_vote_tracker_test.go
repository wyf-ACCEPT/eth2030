package consensus

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func makeVTHash(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	return h
}

func makeTrackedVote(vi ValidatorIndex, slot uint64, root types.Hash, stake uint64) *TrackedVote {
	return &TrackedVote{
		ValidatorIndex: vi,
		Slot:           slot,
		BlockRoot:      root,
		Stake:          stake,
	}
}

func TestSSFVoteTrackerRecordVote(t *testing.T) {
	cfg := DefaultSSFVoteTrackerConfig()
	cfg.TotalActiveStake = 300
	vt := NewSSFVoteTracker(cfg)

	root := makeVTHash(0xAA)
	vote := makeTrackedVote(1, 10, root, 100)

	finalized, err := vt.RecordVote(vote)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if finalized {
		t.Fatal("should not be finalized with 100/300 stake")
	}
	if vt.VoteCount(10) != 1 {
		t.Fatalf("expected 1 vote, got %d", vt.VoteCount(10))
	}
}

func TestSSFVoteTrackerSupermajority(t *testing.T) {
	cfg := DefaultSSFVoteTrackerConfig()
	cfg.TotalActiveStake = 300
	vt := NewSSFVoteTracker(cfg)

	root := makeVTHash(0xBB)

	// 100 + 101 = 201 >= 200 (2/3 of 300)
	vt.RecordVote(makeTrackedVote(1, 10, root, 100))
	finalized, err := vt.RecordVote(makeTrackedVote(2, 10, root, 101))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !finalized {
		t.Fatal("expected finality to be reached")
	}
	if !vt.IsFinalized(10) {
		t.Fatal("slot 10 should be finalized")
	}
	if vt.FinalizedRoot(10) != root {
		t.Fatalf("wrong finalized root: %x", vt.FinalizedRoot(10))
	}
}

func TestSSFVoteTrackerEquivocation(t *testing.T) {
	cfg := DefaultSSFVoteTrackerConfig()
	cfg.TotalActiveStake = 300
	vt := NewSSFVoteTracker(cfg)

	root1 := makeVTHash(0xAA)
	root2 := makeVTHash(0xBB)

	vt.RecordVote(makeTrackedVote(1, 10, root1, 50))
	// Same validator, same slot, different root.
	vt.RecordVote(makeTrackedVote(1, 10, root2, 50))

	equivs := vt.DrainEquivocations()
	if len(equivs) != 1 {
		t.Fatalf("expected 1 equivocation, got %d", len(equivs))
	}
	if equivs[0].ValidatorIndex != 1 {
		t.Fatalf("wrong validator: %d", equivs[0].ValidatorIndex)
	}
	if equivs[0].Root1 != root1 || equivs[0].Root2 != root2 {
		t.Fatal("wrong roots in equivocation record")
	}
}

func TestSSFVoteTrackerDuplicateSameRoot(t *testing.T) {
	cfg := DefaultSSFVoteTrackerConfig()
	cfg.TotalActiveStake = 300
	vt := NewSSFVoteTracker(cfg)

	root := makeVTHash(0xCC)
	vt.RecordVote(makeTrackedVote(1, 10, root, 100))
	// Same validator, same root -- no-op, no equivocation.
	vt.RecordVote(makeTrackedVote(1, 10, root, 100))

	if vt.VoteCount(10) != 1 {
		t.Fatalf("expected 1 vote, got %d", vt.VoteCount(10))
	}
	equivs := vt.PeekEquivocations()
	if len(equivs) != 0 {
		t.Fatalf("expected 0 equivocations, got %d", len(equivs))
	}
}

func TestSSFVoteTrackerNilVote(t *testing.T) {
	cfg := DefaultSSFVoteTrackerConfig()
	vt := NewSSFVoteTracker(cfg)

	_, err := vt.RecordVote(nil)
	if err != ErrVTNilVote {
		t.Fatalf("expected ErrVTNilVote, got %v", err)
	}
}

func TestSSFVoteTrackerZeroStake(t *testing.T) {
	cfg := DefaultSSFVoteTrackerConfig()
	cfg.TotalActiveStake = 0
	vt := NewSSFVoteTracker(cfg)

	_, err := vt.RecordVote(makeTrackedVote(1, 10, makeVTHash(0xAA), 100))
	if err == nil {
		t.Fatal("expected error for zero total stake")
	}
}

func TestSSFVoteTrackerSlotAlreadyFinalized(t *testing.T) {
	cfg := DefaultSSFVoteTrackerConfig()
	cfg.TotalActiveStake = 300
	vt := NewSSFVoteTracker(cfg)

	root := makeVTHash(0xDD)
	vt.RecordVote(makeTrackedVote(1, 10, root, 201))

	// Now try to vote for the finalized slot.
	_, err := vt.RecordVote(makeTrackedVote(5, 10, root, 50))
	if err == nil {
		t.Fatal("expected error for finalized slot")
	}
}

func TestSSFVoteTrackerFinalityEvent(t *testing.T) {
	cfg := DefaultSSFVoteTrackerConfig()
	cfg.TotalActiveStake = 300
	vt := NewSSFVoteTracker(cfg)

	root := makeVTHash(0xEE)
	vt.RecordVote(makeTrackedVote(1, 10, root, 201))

	events := vt.DrainFinalityEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 finality event, got %d", len(events))
	}
	if events[0].Slot != 10 {
		t.Fatalf("wrong event slot: %d", events[0].Slot)
	}
	if events[0].BlockRoot != root {
		t.Fatal("wrong event root")
	}
	if events[0].Participation < 60 {
		t.Fatalf("participation too low: %f", events[0].Participation)
	}

	// Events should be drained.
	events2 := vt.DrainFinalityEvents()
	if len(events2) != 0 {
		t.Fatal("events should be drained")
	}
}

func TestSSFVoteTrackerPeekFinalityEvents(t *testing.T) {
	cfg := DefaultSSFVoteTrackerConfig()
	cfg.TotalActiveStake = 300
	vt := NewSSFVoteTracker(cfg)

	vt.RecordVote(makeTrackedVote(1, 10, makeVTHash(0xFF), 201))

	events := vt.PeekFinalityEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1, got %d", len(events))
	}
	// Peek should not drain.
	events2 := vt.PeekFinalityEvents()
	if len(events2) != 1 {
		t.Fatal("peek should not consume events")
	}
}

func TestSSFVoteTrackerExpireOldSlots(t *testing.T) {
	cfg := DefaultSSFVoteTrackerConfig()
	cfg.TotalActiveStake = 300
	cfg.MaxSlotRetention = 5
	vt := NewSSFVoteTracker(cfg)

	root := makeVTHash(0x01)
	for i := uint64(1); i <= 20; i++ {
		vt.RecordVote(makeTrackedVote(ValidatorIndex(i), i, root, 10))
	}

	expired := vt.ExpireOldSlots()
	if expired == 0 {
		t.Fatal("expected some slots to expire")
	}
	// Slots 1 through 14 should be expired (20 - 5 = 15 cutoff).
	if vt.TrackedSlotCount() > 6 {
		t.Fatalf("too many slots remain: %d", vt.TrackedSlotCount())
	}
}

func TestSSFVoteTrackerVoteForExpiredSlot(t *testing.T) {
	cfg := DefaultSSFVoteTrackerConfig()
	cfg.TotalActiveStake = 300
	cfg.MaxSlotRetention = 5
	vt := NewSSFVoteTracker(cfg)

	// Push highest slot to 100.
	vt.RecordVote(makeTrackedVote(1, 100, makeVTHash(0xAA), 10))

	// Now try to vote for slot 1 (way too old).
	_, err := vt.RecordVote(makeTrackedVote(2, 1, makeVTHash(0xBB), 10))
	if err == nil {
		t.Fatal("expected error for expired slot vote")
	}
}

func TestSSFVoteTrackerStakeForRoot(t *testing.T) {
	cfg := DefaultSSFVoteTrackerConfig()
	cfg.TotalActiveStake = 1000
	vt := NewSSFVoteTracker(cfg)

	rootA := makeVTHash(0xAA)
	rootB := makeVTHash(0xBB)

	vt.RecordVote(makeTrackedVote(1, 10, rootA, 100))
	vt.RecordVote(makeTrackedVote(2, 10, rootA, 150))
	vt.RecordVote(makeTrackedVote(3, 10, rootB, 200))

	if vt.StakeForRoot(10, rootA) != 250 {
		t.Fatalf("expected 250, got %d", vt.StakeForRoot(10, rootA))
	}
	if vt.StakeForRoot(10, rootB) != 200 {
		t.Fatalf("expected 200, got %d", vt.StakeForRoot(10, rootB))
	}
}

func TestSSFVoteTrackerTotalVotingStake(t *testing.T) {
	cfg := DefaultSSFVoteTrackerConfig()
	cfg.TotalActiveStake = 1000
	vt := NewSSFVoteTracker(cfg)

	vt.RecordVote(makeTrackedVote(1, 10, makeVTHash(0xAA), 100))
	vt.RecordVote(makeTrackedVote(2, 10, makeVTHash(0xBB), 200))

	if vt.TotalVotingStake(10) != 300 {
		t.Fatalf("expected 300, got %d", vt.TotalVotingStake(10))
	}
}

func TestSSFVoteTrackerParticipation(t *testing.T) {
	cfg := DefaultSSFVoteTrackerConfig()
	cfg.TotalActiveStake = 400
	vt := NewSSFVoteTracker(cfg)

	vt.RecordVote(makeTrackedVote(1, 10, makeVTHash(0xAA), 200))

	p := vt.Participation(10)
	if p != 50.0 {
		t.Fatalf("expected 50.0%%, got %f", p)
	}
}

func TestSSFVoteTrackerLeadingRoot(t *testing.T) {
	cfg := DefaultSSFVoteTrackerConfig()
	cfg.TotalActiveStake = 1000
	vt := NewSSFVoteTracker(cfg)

	rootA := makeVTHash(0xAA)
	rootB := makeVTHash(0xBB)

	vt.RecordVote(makeTrackedVote(1, 10, rootA, 100))
	vt.RecordVote(makeTrackedVote(2, 10, rootB, 200))

	lr, ls := vt.LeadingRoot(10)
	if lr != rootB {
		t.Fatalf("expected rootB as leading, got %x", lr)
	}
	if ls != 200 {
		t.Fatalf("expected 200, got %d", ls)
	}
}

func TestSSFVoteTrackerCollectBatchSignatures(t *testing.T) {
	cfg := DefaultSSFVoteTrackerConfig()
	cfg.TotalActiveStake = 1000
	vt := NewSSFVoteTracker(cfg)

	root := makeVTHash(0xAA)
	otherRoot := makeVTHash(0xBB)

	var sig1, sig2 [96]byte
	sig1[0] = 0x11
	sig2[0] = 0x22

	v1 := makeTrackedVote(1, 10, root, 100)
	v1.Signature = sig1
	v2 := makeTrackedVote(2, 10, root, 200)
	v2.Signature = sig2
	v3 := makeTrackedVote(3, 10, otherRoot, 50)

	vt.RecordVote(v1)
	vt.RecordVote(v2)
	vt.RecordVote(v3)

	batch := vt.CollectBatchSignatures(10, root)
	if batch == nil {
		t.Fatal("expected batch data")
	}
	if batch.Count != 2 {
		t.Fatalf("expected 2 sigs, got %d", batch.Count)
	}

	// No batch for unknown slot.
	if vt.CollectBatchSignatures(99, root) != nil {
		t.Fatal("expected nil for unknown slot")
	}
}

func TestSSFVoteTrackerMultipleSlots(t *testing.T) {
	cfg := DefaultSSFVoteTrackerConfig()
	cfg.TotalActiveStake = 300
	vt := NewSSFVoteTracker(cfg)

	rootA := makeVTHash(0xAA)
	rootB := makeVTHash(0xBB)

	vt.RecordVote(makeTrackedVote(1, 10, rootA, 201))
	vt.RecordVote(makeTrackedVote(1, 11, rootB, 100))

	if !vt.IsFinalized(10) {
		t.Fatal("slot 10 should be finalized")
	}
	if vt.IsFinalized(11) {
		t.Fatal("slot 11 should not be finalized")
	}
	if vt.TrackedSlotCount() != 2 {
		t.Fatalf("expected 2 tracked slots, got %d", vt.TrackedSlotCount())
	}
}

func TestSSFVoteTrackerNonFinalizedRoot(t *testing.T) {
	cfg := DefaultSSFVoteTrackerConfig()
	cfg.TotalActiveStake = 300
	vt := NewSSFVoteTracker(cfg)

	root := vt.FinalizedRoot(99)
	if root != (types.Hash{}) {
		t.Fatal("expected zero hash for non-existent slot")
	}
}

func TestSSFVoteTrackerComputeVoteDigest(t *testing.T) {
	root := makeVTHash(0xAA)
	d1 := computeVoteDigest(1, 10, root)
	d2 := computeVoteDigest(1, 10, root)
	d3 := computeVoteDigest(2, 10, root)

	if d1 != d2 {
		t.Fatal("same inputs should produce same digest")
	}
	if d1 == d3 {
		t.Fatal("different validators should produce different digest")
	}
}

func TestSSFVoteTrackerEquivocationNotDoubleCounted(t *testing.T) {
	cfg := DefaultSSFVoteTrackerConfig()
	cfg.TotalActiveStake = 300
	vt := NewSSFVoteTracker(cfg)

	root1 := makeVTHash(0xAA)
	root2 := makeVTHash(0xBB)

	vt.RecordVote(makeTrackedVote(1, 10, root1, 150))
	vt.RecordVote(makeTrackedVote(1, 10, root2, 150))

	// Only 150 stake should be counted (first vote), not 300.
	if vt.TotalVotingStake(10) != 150 {
		t.Fatalf("expected 150, got %d (equivocation should not double-count)",
			vt.TotalVotingStake(10))
	}
}

func TestSSFVoteTrackerDefaultConfig(t *testing.T) {
	cfg := DefaultSSFVoteTrackerConfig()
	if cfg.SupermajorityNum != 2 || cfg.SupermajorityDen != 3 {
		t.Fatal("default should be 2/3 threshold")
	}
	if cfg.MaxSlotRetention != 64 {
		t.Fatalf("expected 64 retention, got %d", cfg.MaxSlotRetention)
	}
}
