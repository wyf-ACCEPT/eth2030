package consensus

import (
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestFinalityEquivocationDetectorNew(t *testing.T) {
	d := NewFinalityEquivocationDetector(100)
	if d == nil {
		t.Fatal("expected non-nil detector")
	}
	if d.maxSlotHistory != 100 {
		t.Errorf("expected maxSlotHistory 100, got %d", d.maxSlotHistory)
	}
	if d.EvidenceCount() != 0 {
		t.Errorf("expected 0 evidence, got %d", d.EvidenceCount())
	}
	if d.TrackedVoteCount() != 0 {
		t.Errorf("expected 0 tracked votes, got %d", d.TrackedVoteCount())
	}

	// Zero maxSlotHistory should default to 256.
	d2 := NewFinalityEquivocationDetector(0)
	if d2.maxSlotHistory != 256 {
		t.Errorf("expected default maxSlotHistory 256, got %d", d2.maxSlotHistory)
	}
}

func TestFinalityEquivocationDetectorNoEquivocation(t *testing.T) {
	d := NewFinalityEquivocationDetector(100)

	vote := &BlockFinalityVote{
		Slot:           1,
		BlockRoot:      types.Hash{0x01},
		ValidatorIndex: 0,
		Signature:      []byte{0xAA},
		Timestamp:      1000,
	}

	ev := d.RecordVote(vote)
	if ev != nil {
		t.Error("expected no equivocation for first vote")
	}

	if d.TrackedVoteCount() != 1 {
		t.Errorf("expected 1 tracked vote, got %d", d.TrackedVoteCount())
	}
	if d.EvidenceCount() != 0 {
		t.Errorf("expected 0 evidence, got %d", d.EvidenceCount())
	}
}

func TestFinalityEquivocationDetectorDoubleVote(t *testing.T) {
	d := NewFinalityEquivocationDetector(100)

	vote1 := &BlockFinalityVote{
		Slot:           1,
		BlockRoot:      types.Hash{0x01},
		ValidatorIndex: 5,
		Signature:      []byte{0xAA},
		Timestamp:      1000,
	}
	vote2 := &BlockFinalityVote{
		Slot:           1,
		BlockRoot:      types.Hash{0x02}, // different root
		ValidatorIndex: 5,
		Signature:      []byte{0xBB},
		Timestamp:      1001,
	}

	ev := d.RecordVote(vote1)
	if ev != nil {
		t.Error("first vote should not be equivocation")
	}

	ev = d.RecordVote(vote2)
	if ev == nil {
		t.Fatal("expected equivocation evidence for double vote")
	}
	if ev.ValidatorIndex != 5 {
		t.Errorf("expected validator 5, got %d", ev.ValidatorIndex)
	}
	if ev.Slot != 1 {
		t.Errorf("expected slot 1, got %d", ev.Slot)
	}
	if ev.VoteRoot1 != (types.Hash{0x01}) {
		t.Error("expected VoteRoot1 {0x01}")
	}
	if ev.VoteRoot2 != (types.Hash{0x02}) {
		t.Error("expected VoteRoot2 {0x02}")
	}
	if d.EvidenceCount() != 1 {
		t.Errorf("expected 1 evidence, got %d", d.EvidenceCount())
	}
}

func TestFinalityEquivocationDetectorSameVoteTwice(t *testing.T) {
	d := NewFinalityEquivocationDetector(100)

	vote := &BlockFinalityVote{
		Slot:           1,
		BlockRoot:      types.Hash{0x01},
		ValidatorIndex: 0,
		Signature:      []byte{0xAA},
		Timestamp:      1000,
	}

	d.RecordVote(vote)
	ev := d.RecordVote(vote) // same vote again
	if ev != nil {
		t.Error("same vote twice should not be equivocation")
	}
	if d.EvidenceCount() != 0 {
		t.Errorf("expected 0 evidence, got %d", d.EvidenceCount())
	}
}

func TestFinalityEquivocationDetectorMultipleValidators(t *testing.T) {
	d := NewFinalityEquivocationDetector(100)

	// Validator 0 double-votes.
	d.RecordVote(&BlockFinalityVote{
		Slot: 1, BlockRoot: types.Hash{0x01}, ValidatorIndex: 0,
		Signature: []byte{0xAA}, Timestamp: 1000,
	})
	d.RecordVote(&BlockFinalityVote{
		Slot: 1, BlockRoot: types.Hash{0x02}, ValidatorIndex: 0,
		Signature: []byte{0xBB}, Timestamp: 1001,
	})

	// Validator 1 votes honestly.
	d.RecordVote(&BlockFinalityVote{
		Slot: 1, BlockRoot: types.Hash{0x01}, ValidatorIndex: 1,
		Signature: []byte{0xCC}, Timestamp: 1000,
	})

	// Validator 2 double-votes.
	d.RecordVote(&BlockFinalityVote{
		Slot: 1, BlockRoot: types.Hash{0x01}, ValidatorIndex: 2,
		Signature: []byte{0xDD}, Timestamp: 1000,
	})
	d.RecordVote(&BlockFinalityVote{
		Slot: 1, BlockRoot: types.Hash{0x03}, ValidatorIndex: 2,
		Signature: []byte{0xEE}, Timestamp: 1002,
	})

	if d.EvidenceCount() != 2 {
		t.Errorf("expected 2 evidence, got %d", d.EvidenceCount())
	}

	// Check per-validator evidence.
	ev0 := d.GetEvidence(0)
	if len(ev0) != 1 {
		t.Errorf("expected 1 evidence for validator 0, got %d", len(ev0))
	}
	ev1 := d.GetEvidence(1)
	if len(ev1) != 0 {
		t.Errorf("expected 0 evidence for validator 1, got %d", len(ev1))
	}
	ev2 := d.GetEvidence(2)
	if len(ev2) != 1 {
		t.Errorf("expected 1 evidence for validator 2, got %d", len(ev2))
	}
}

func TestFinalityEquivocationDetectorPruneOldSlots(t *testing.T) {
	d := NewFinalityEquivocationDetector(10)

	// Create equivocation at slot 5.
	d.RecordVote(&BlockFinalityVote{
		Slot: 5, BlockRoot: types.Hash{0x01}, ValidatorIndex: 0,
		Signature: []byte{0xAA}, Timestamp: 5000,
	})
	d.RecordVote(&BlockFinalityVote{
		Slot: 5, BlockRoot: types.Hash{0x02}, ValidatorIndex: 0,
		Signature: []byte{0xBB}, Timestamp: 5001,
	})

	// Create equivocation at slot 20.
	d.RecordVote(&BlockFinalityVote{
		Slot: 20, BlockRoot: types.Hash{0x01}, ValidatorIndex: 1,
		Signature: []byte{0xCC}, Timestamp: 20000,
	})
	d.RecordVote(&BlockFinalityVote{
		Slot: 20, BlockRoot: types.Hash{0x02}, ValidatorIndex: 1,
		Signature: []byte{0xDD}, Timestamp: 20001,
	})

	if d.EvidenceCount() != 2 {
		t.Fatalf("expected 2 evidence before prune, got %d", d.EvidenceCount())
	}

	// Prune at slot 25 with history 10 -> cutoff = 15.
	// Slot 5 should be pruned, slot 20 should remain.
	d.PruneOldSlots(25)

	slotEv5 := d.GetSlotEvidence(5)
	if len(slotEv5) != 0 {
		t.Errorf("expected slot 5 evidence pruned, got %d", len(slotEv5))
	}

	slotEv20 := d.GetSlotEvidence(20)
	if len(slotEv20) != 1 {
		t.Errorf("expected 1 evidence at slot 20, got %d", len(slotEv20))
	}

	// Validator 0's evidence should be pruned.
	ev0 := d.GetEvidence(0)
	if len(ev0) != 0 {
		t.Errorf("expected validator 0 evidence pruned, got %d", len(ev0))
	}

	// Validator 1's evidence should remain.
	ev1 := d.GetEvidence(1)
	if len(ev1) != 1 {
		t.Errorf("expected 1 evidence for validator 1, got %d", len(ev1))
	}
}

func TestFinalityEquivocationDetectorGetEvidence(t *testing.T) {
	d := NewFinalityEquivocationDetector(100)

	// No evidence for unknown validator.
	ev := d.GetEvidence(99)
	if ev != nil {
		t.Error("expected nil for unknown validator")
	}

	// Create evidence.
	d.RecordVote(&BlockFinalityVote{
		Slot: 1, BlockRoot: types.Hash{0x01}, ValidatorIndex: 5,
		Signature: []byte{0xAA}, Timestamp: 1000,
	})
	d.RecordVote(&BlockFinalityVote{
		Slot: 1, BlockRoot: types.Hash{0x02}, ValidatorIndex: 5,
		Signature: []byte{0xBB}, Timestamp: 1001,
	})

	ev = d.GetEvidence(5)
	if len(ev) != 1 {
		t.Fatalf("expected 1 evidence, got %d", len(ev))
	}
	if ev[0].ValidatorIndex != 5 {
		t.Errorf("expected validator 5, got %d", ev[0].ValidatorIndex)
	}
}

func TestFinalityEquivocationDetectorSlotEvidence(t *testing.T) {
	d := NewFinalityEquivocationDetector(100)

	// No evidence for empty slot.
	ev := d.GetSlotEvidence(1)
	if ev != nil {
		t.Error("expected nil for empty slot")
	}

	// Two validators double-vote at slot 10.
	for _, valIdx := range []uint64{3, 7} {
		d.RecordVote(&BlockFinalityVote{
			Slot: 10, BlockRoot: types.Hash{0x01}, ValidatorIndex: valIdx,
			Signature: []byte{byte(valIdx)}, Timestamp: 10000,
		})
		d.RecordVote(&BlockFinalityVote{
			Slot: 10, BlockRoot: types.Hash{0x02}, ValidatorIndex: valIdx,
			Signature: []byte{byte(valIdx + 100)}, Timestamp: 10001,
		})
	}

	ev = d.GetSlotEvidence(10)
	if len(ev) != 2 {
		t.Fatalf("expected 2 evidence at slot 10, got %d", len(ev))
	}
}

func TestFinalityEquivocationDetectorSlashableValidators(t *testing.T) {
	d := NewFinalityEquivocationDetector(100)

	// Create equivocations at slot 5 for validators 10, 3, 7.
	for _, valIdx := range []uint64{10, 3, 7} {
		d.RecordVote(&BlockFinalityVote{
			Slot: 5, BlockRoot: types.Hash{0x01}, ValidatorIndex: valIdx,
			Signature: []byte{byte(valIdx)}, Timestamp: 5000,
		})
		d.RecordVote(&BlockFinalityVote{
			Slot: 5, BlockRoot: types.Hash{0x02}, ValidatorIndex: valIdx,
			Signature: []byte{byte(valIdx + 100)}, Timestamp: 5001,
		})
	}

	slashable := d.SlashableValidators(5)
	if len(slashable) != 3 {
		t.Fatalf("expected 3 slashable validators, got %d", len(slashable))
	}

	// Should be sorted.
	if slashable[0] != 3 || slashable[1] != 7 || slashable[2] != 10 {
		t.Errorf("expected sorted [3, 7, 10], got %v", slashable)
	}

	// No slashable at other slots.
	slashable2 := d.SlashableValidators(99)
	if len(slashable2) != 0 {
		t.Errorf("expected 0 slashable at slot 99, got %d", len(slashable2))
	}
}

func TestFinalityEquivocationDetectorEvidenceCount(t *testing.T) {
	d := NewFinalityEquivocationDetector(100)

	if d.EvidenceCount() != 0 {
		t.Errorf("expected 0 initially, got %d", d.EvidenceCount())
	}

	// Create 3 equivocations.
	for slot := uint64(1); slot <= 3; slot++ {
		d.RecordVote(&BlockFinalityVote{
			Slot: slot, BlockRoot: types.Hash{0x01}, ValidatorIndex: slot,
			Signature: []byte{byte(slot)}, Timestamp: int64(slot * 1000),
		})
		d.RecordVote(&BlockFinalityVote{
			Slot: slot, BlockRoot: types.Hash{0x02}, ValidatorIndex: slot,
			Signature: []byte{byte(slot + 100)}, Timestamp: int64(slot*1000 + 1),
		})
	}

	if d.EvidenceCount() != 3 {
		t.Errorf("expected 3, got %d", d.EvidenceCount())
	}
}

func TestFinalityEquivocationDetectorVerifyEvidence(t *testing.T) {
	d := NewFinalityEquivocationDetector(100)

	// Nil evidence is invalid.
	if d.VerifyEvidence(nil) {
		t.Error("nil evidence should be invalid")
	}

	// Same roots is invalid.
	ev := &FinalityEquivocationEvidence{
		ValidatorIndex: 1,
		Slot:           5,
		VoteRoot1:      types.Hash{0x01},
		VoteRoot2:      types.Hash{0x01}, // same
	}
	if d.VerifyEvidence(ev) {
		t.Error("same roots should be invalid")
	}

	// Slot 0 is invalid.
	ev2 := &FinalityEquivocationEvidence{
		ValidatorIndex: 1,
		Slot:           0,
		VoteRoot1:      types.Hash{0x01},
		VoteRoot2:      types.Hash{0x02},
	}
	if d.VerifyEvidence(ev2) {
		t.Error("slot 0 should be invalid")
	}

	// Valid evidence.
	ev3 := &FinalityEquivocationEvidence{
		ValidatorIndex: 1,
		Slot:           5,
		VoteRoot1:      types.Hash{0x01},
		VoteRoot2:      types.Hash{0x02},
	}
	if !d.VerifyEvidence(ev3) {
		t.Error("valid evidence should pass verification")
	}
}

func TestFinalityEquivocationDetectorConcurrentAccess(t *testing.T) {
	d := NewFinalityEquivocationDetector(100)

	var wg sync.WaitGroup

	// 20 goroutines, each recording honest votes for different validators.
	for i := uint64(0); i < 20; i++ {
		wg.Add(1)
		go func(idx uint64) {
			defer wg.Done()
			d.RecordVote(&BlockFinalityVote{
				Slot:           1,
				BlockRoot:      types.Hash{0x01},
				ValidatorIndex: idx,
				Signature:      []byte{byte(idx)},
				Timestamp:      1000,
			})
		}(i)
	}
	wg.Wait()

	// No equivocations should be detected.
	if d.EvidenceCount() != 0 {
		t.Errorf("expected 0 evidence from honest votes, got %d", d.EvidenceCount())
	}
	if d.TrackedVoteCount() != 20 {
		t.Errorf("expected 20 tracked votes, got %d", d.TrackedVoteCount())
	}

	// Now generate some equivocations concurrently.
	for i := uint64(0); i < 10; i++ {
		wg.Add(1)
		go func(idx uint64) {
			defer wg.Done()
			d.RecordVote(&BlockFinalityVote{
				Slot:           1,
				BlockRoot:      types.Hash{0x02}, // different root
				ValidatorIndex: idx,
				Signature:      []byte{byte(idx + 100)},
				Timestamp:      1001,
			})
		}(i)
	}
	wg.Wait()

	if d.EvidenceCount() != 10 {
		t.Errorf("expected 10 evidence from double votes, got %d", d.EvidenceCount())
	}
}

func TestFinalityEquivocationDetectorNilVote(t *testing.T) {
	d := NewFinalityEquivocationDetector(100)

	ev := d.RecordVote(nil)
	if ev != nil {
		t.Error("nil vote should return nil")
	}
	if d.TrackedVoteCount() != 0 {
		t.Errorf("expected 0 tracked votes after nil, got %d", d.TrackedVoteCount())
	}
}

func TestFinalityEquivocationDetectorDifferentSlots(t *testing.T) {
	d := NewFinalityEquivocationDetector(100)

	// Same validator votes for different roots but in different slots.
	// This is NOT equivocation.
	d.RecordVote(&BlockFinalityVote{
		Slot: 1, BlockRoot: types.Hash{0x01}, ValidatorIndex: 0,
		Signature: []byte{0xAA}, Timestamp: 1000,
	})
	ev := d.RecordVote(&BlockFinalityVote{
		Slot: 2, BlockRoot: types.Hash{0x02}, ValidatorIndex: 0,
		Signature: []byte{0xBB}, Timestamp: 2000,
	})

	if ev != nil {
		t.Error("different slots should not be equivocation")
	}
	if d.EvidenceCount() != 0 {
		t.Errorf("expected 0 evidence, got %d", d.EvidenceCount())
	}
}
