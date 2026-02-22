package consensus

import (
	"errors"
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func makeBlockRoot(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	return h
}

func TestSSFDefaultConfig(t *testing.T) {
	cfg := DefaultSSFConfig()

	if cfg.SlotDuration != 12 {
		t.Errorf("SlotDuration = %d, want 12", cfg.SlotDuration)
	}
	if cfg.VoterLimit != 8192 {
		t.Errorf("VoterLimit = %d, want 8192", cfg.VoterLimit)
	}
	if cfg.FinalityThresholdNum != 2 || cfg.FinalityThresholdDen != 3 {
		t.Errorf("FinalityThreshold = %d/%d, want 2/3",
			cfg.FinalityThresholdNum, cfg.FinalityThresholdDen)
	}
	if cfg.TotalStake == 0 {
		t.Error("TotalStake should be non-zero")
	}
	// Verify it represents ~32M ETH.
	expectedStake := uint64(32_000_000) * GweiPerETH
	if cfg.TotalStake != expectedStake {
		t.Errorf("TotalStake = %d, want %d", cfg.TotalStake, expectedStake)
	}
}

func TestSSFCastVote(t *testing.T) {
	cfg := DefaultSSFConfig()
	cfg.TotalStake = 300 // simplified for testing
	state := NewSSFState(cfg)

	root := makeBlockRoot(0xAA)
	vote := SSFVote{
		ValidatorIndex: 0,
		Slot:           1,
		BlockRoot:      root,
		Stake:          100,
	}

	if err := state.CastVote(vote); err != nil {
		t.Fatalf("CastVote failed: %v", err)
	}

	// Verify the vote is recorded.
	if state.VoteCount(1) != 1 {
		t.Errorf("VoteCount(1) = %d, want 1", state.VoteCount(1))
	}
	if state.StakeForRoot(1, root) != 100 {
		t.Errorf("StakeForRoot = %d, want 100", state.StakeForRoot(1, root))
	}

	// Cast a second vote from a different validator.
	vote2 := SSFVote{
		ValidatorIndex: 1,
		Slot:           1,
		BlockRoot:      root,
		Stake:          50,
	}
	if err := state.CastVote(vote2); err != nil {
		t.Fatalf("CastVote(2) failed: %v", err)
	}
	if state.VoteCount(1) != 2 {
		t.Errorf("VoteCount(1) = %d, want 2", state.VoteCount(1))
	}
	if state.StakeForRoot(1, root) != 150 {
		t.Errorf("StakeForRoot = %d, want 150", state.StakeForRoot(1, root))
	}
}

func TestSSFCheckFinalityNotMet(t *testing.T) {
	cfg := DefaultSSFConfig()
	cfg.TotalStake = 300
	state := NewSSFState(cfg)

	root := makeBlockRoot(0xBB)

	// Cast votes totaling 100 out of 300 (33%, below 2/3).
	state.CastVote(SSFVote{ValidatorIndex: 0, Slot: 5, BlockRoot: root, Stake: 50})
	state.CastVote(SSFVote{ValidatorIndex: 1, Slot: 5, BlockRoot: root, Stake: 50})

	met, err := state.CheckFinality(5, root)
	if err != nil {
		t.Fatalf("CheckFinality error: %v", err)
	}
	if met {
		t.Error("finality should NOT be met with 100/300 stake (33%)")
	}
}

func TestSSFCheckFinalityMet(t *testing.T) {
	cfg := DefaultSSFConfig()
	cfg.TotalStake = 300
	state := NewSSFState(cfg)

	root := makeBlockRoot(0xCC)

	// Cast votes totaling 201 out of 300 (67%, meets 2/3).
	state.CastVote(SSFVote{ValidatorIndex: 0, Slot: 10, BlockRoot: root, Stake: 100})
	state.CastVote(SSFVote{ValidatorIndex: 1, Slot: 10, BlockRoot: root, Stake: 101})

	met, err := state.CheckFinality(10, root)
	if err != nil {
		t.Fatalf("CheckFinality error: %v", err)
	}
	if !met {
		t.Error("finality SHOULD be met with 201/300 stake (67%)")
	}
}

func TestSSFCheckFinalityExact(t *testing.T) {
	cfg := DefaultSSFConfig()
	cfg.TotalStake = 3
	state := NewSSFState(cfg)

	root := makeBlockRoot(0xDD)

	// Exactly 2 out of 3 total stake => meets threshold.
	state.CastVote(SSFVote{ValidatorIndex: 0, Slot: 1, BlockRoot: root, Stake: 2})
	met, err := state.CheckFinality(1, root)
	if err != nil {
		t.Fatalf("CheckFinality error: %v", err)
	}
	if !met {
		t.Error("finality should be met with exactly 2/3 stake")
	}
}

func TestSSFCheckFinalityBelowExact(t *testing.T) {
	cfg := DefaultSSFConfig()
	cfg.TotalStake = 3
	state := NewSSFState(cfg)

	root := makeBlockRoot(0xEE)

	// 1 out of 3 total stake => below threshold.
	state.CastVote(SSFVote{ValidatorIndex: 0, Slot: 1, BlockRoot: root, Stake: 1})
	met, err := state.CheckFinality(1, root)
	if err != nil {
		t.Fatalf("CheckFinality error: %v", err)
	}
	if met {
		t.Error("finality should NOT be met with 1/3 stake")
	}
}

func TestSSFCheckFinalityZeroStake(t *testing.T) {
	cfg := DefaultSSFConfig()
	cfg.TotalStake = 0
	state := NewSSFState(cfg)

	_, err := state.CheckFinality(1, makeBlockRoot(0xFF))
	if !errors.Is(err, ErrSSFZeroStake) {
		t.Errorf("expected ErrSSFZeroStake, got %v", err)
	}
}

func TestSSFFinalize(t *testing.T) {
	cfg := DefaultSSFConfig()
	cfg.TotalStake = 300
	state := NewSSFState(cfg)

	root := makeBlockRoot(0xAA)

	// Add some votes and finalize.
	state.CastVote(SSFVote{ValidatorIndex: 0, Slot: 5, BlockRoot: root, Stake: 200})
	state.CastVote(SSFVote{ValidatorIndex: 1, Slot: 5, BlockRoot: root, Stake: 50})

	if err := state.FinalizeSlot(5, root); err != nil {
		t.Fatalf("FinalizeSlot failed: %v", err)
	}

	// Verify finalized state.
	slot, fRoot := state.GetFinalizedHead()
	if slot != 5 {
		t.Errorf("FinalizedSlot = %d, want 5", slot)
	}
	if fRoot != root {
		t.Errorf("FinalizedRoot mismatch")
	}

	// Verify votes for slot 5 were cleaned up.
	if state.VoteCount(5) != 0 {
		t.Errorf("VoteCount(5) = %d after finalization, want 0", state.VoteCount(5))
	}
}

func TestSSFFinalizedHead(t *testing.T) {
	cfg := DefaultSSFConfig()
	state := NewSSFState(cfg)

	// Initially, finalized head is slot 0 with zero root.
	slot, root := state.GetFinalizedHead()
	if slot != 0 {
		t.Errorf("initial FinalizedSlot = %d, want 0", slot)
	}
	if !root.IsZero() {
		t.Error("initial FinalizedRoot should be zero")
	}

	// Finalize a slot.
	expectedRoot := makeBlockRoot(0x42)
	state.FinalizeSlot(10, expectedRoot)

	slot, root = state.GetFinalizedHead()
	if slot != 10 {
		t.Errorf("FinalizedSlot = %d, want 10", slot)
	}
	if root != expectedRoot {
		t.Errorf("FinalizedRoot = %v, want %v", root, expectedRoot)
	}

	// Finalize a later slot.
	laterRoot := makeBlockRoot(0x43)
	state.FinalizeSlot(20, laterRoot)

	slot, root = state.GetFinalizedHead()
	if slot != 20 {
		t.Errorf("FinalizedSlot = %d, want 20", slot)
	}
	if root != laterRoot {
		t.Errorf("FinalizedRoot mismatch after second finalization")
	}
}

func TestSSFPruneVotes(t *testing.T) {
	cfg := DefaultSSFConfig()
	cfg.TotalStake = 300
	state := NewSSFState(cfg)

	root := makeBlockRoot(0x01)

	// Add votes to slots 1, 5, 10, 15.
	for _, slot := range []uint64{1, 5, 10, 15} {
		state.CastVote(SSFVote{ValidatorIndex: 0, Slot: slot, BlockRoot: root, Stake: 10})
	}

	// Prune votes up to slot 10.
	state.PruneVotes(10)

	// Slots 1, 5, 10 should be pruned.
	for _, slot := range []uint64{1, 5, 10} {
		if state.VoteCount(slot) != 0 {
			t.Errorf("VoteCount(%d) = %d after prune, want 0", slot, state.VoteCount(slot))
		}
	}

	// Slot 15 should still have votes.
	if state.VoteCount(15) != 1 {
		t.Errorf("VoteCount(15) = %d after prune, want 1", state.VoteCount(15))
	}
}

func TestSSFDuplicateVote(t *testing.T) {
	cfg := DefaultSSFConfig()
	cfg.TotalStake = 300
	state := NewSSFState(cfg)

	root := makeBlockRoot(0xAB)
	vote := SSFVote{
		ValidatorIndex: 42,
		Slot:           7,
		BlockRoot:      root,
		Stake:          100,
	}

	// First vote should succeed.
	if err := state.CastVote(vote); err != nil {
		t.Fatalf("first CastVote failed: %v", err)
	}

	// Duplicate vote from same validator for same slot should fail.
	err := state.CastVote(vote)
	if err == nil {
		t.Fatal("expected error for duplicate vote, got nil")
	}
	if !errors.Is(err, ErrSSFDuplicateVote) {
		t.Errorf("expected ErrSSFDuplicateVote, got: %v", err)
	}

	// Same validator voting for a different block root at the same slot
	// should also fail (equivocation).
	differentRoot := makeBlockRoot(0xAC)
	vote2 := SSFVote{
		ValidatorIndex: 42,
		Slot:           7,
		BlockRoot:      differentRoot,
		Stake:          100,
	}
	err = state.CastVote(vote2)
	if err == nil {
		t.Fatal("expected error for equivocation vote, got nil")
	}
	if !errors.Is(err, ErrSSFDuplicateVote) {
		t.Errorf("expected ErrSSFDuplicateVote for equivocation, got: %v", err)
	}

	// Verify only one vote was counted.
	if state.VoteCount(7) != 1 {
		t.Errorf("VoteCount(7) = %d, want 1 (duplicate rejected)", state.VoteCount(7))
	}
}

func TestSSFConcurrentVotes(t *testing.T) {
	cfg := DefaultSSFConfig()
	cfg.TotalStake = 100_000
	state := NewSSFState(cfg)

	root := makeBlockRoot(0xFF)
	numVoters := 1000

	var wg sync.WaitGroup
	errCh := make(chan error, numVoters)

	// Concurrently cast votes from different validators.
	for i := 0; i < numVoters; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			vote := SSFVote{
				ValidatorIndex: ValidatorIndex(idx),
				Slot:           100,
				BlockRoot:      root,
				Stake:          10,
			}
			if err := state.CastVote(vote); err != nil {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	// No errors should occur since all validators are distinct.
	for err := range errCh {
		t.Errorf("unexpected error during concurrent voting: %v", err)
	}

	// All votes should be recorded.
	if state.VoteCount(100) != numVoters {
		t.Errorf("VoteCount(100) = %d, want %d", state.VoteCount(100), numVoters)
	}

	// Total stake for root should be numVoters * 10.
	expectedStake := uint64(numVoters) * 10
	if state.StakeForRoot(100, root) != expectedStake {
		t.Errorf("StakeForRoot = %d, want %d", state.StakeForRoot(100, root), expectedStake)
	}
}

func TestSSFVoteAfterFinalization(t *testing.T) {
	cfg := DefaultSSFConfig()
	cfg.TotalStake = 300
	state := NewSSFState(cfg)

	root := makeBlockRoot(0xA1)
	state.FinalizeSlot(10, root)

	// Voting for an already-finalized slot should fail.
	err := state.CastVote(SSFVote{
		ValidatorIndex: 0,
		Slot:           5,
		BlockRoot:      root,
		Stake:          100,
	})
	if err == nil {
		t.Fatal("expected error voting for finalized slot, got nil")
	}
	if !errors.Is(err, ErrSSFSlotAlreadyFinal) {
		t.Errorf("expected ErrSSFSlotAlreadyFinal, got: %v", err)
	}
}

func TestSSFInvalidVoterIndex(t *testing.T) {
	cfg := DefaultSSFConfig()
	cfg.VoterLimit = 100
	state := NewSSFState(cfg)

	// Voter index at exactly the limit should fail.
	err := state.CastVote(SSFVote{
		ValidatorIndex: 100,
		Slot:           1,
		BlockRoot:      makeBlockRoot(0x01),
		Stake:          10,
	})
	if err == nil {
		t.Fatal("expected error for voter index at limit, got nil")
	}
	if !errors.Is(err, ErrSSFInvalidVoter) {
		t.Errorf("expected ErrSSFInvalidVoter, got: %v", err)
	}
}

func TestSSFMultipleRoots(t *testing.T) {
	cfg := DefaultSSFConfig()
	cfg.TotalStake = 300
	state := NewSSFState(cfg)

	rootA := makeBlockRoot(0xA0)
	rootB := makeBlockRoot(0xB0)

	// Validator 0 votes for root A.
	state.CastVote(SSFVote{ValidatorIndex: 0, Slot: 1, BlockRoot: rootA, Stake: 100})
	// Validator 1 votes for root B.
	state.CastVote(SSFVote{ValidatorIndex: 1, Slot: 1, BlockRoot: rootB, Stake: 50})
	// Validator 2 votes for root A.
	state.CastVote(SSFVote{ValidatorIndex: 2, Slot: 1, BlockRoot: rootA, Stake: 110})

	// Root A: 210/300 (70%) => meets threshold.
	met, _ := state.CheckFinality(1, rootA)
	if !met {
		t.Error("root A should meet finality threshold with 210/300")
	}

	// Root B: 50/300 (16.7%) => does not meet threshold.
	met, _ = state.CheckFinality(1, rootB)
	if met {
		t.Error("root B should NOT meet finality threshold with 50/300")
	}
}

func TestSSFNewStateNilConfig(t *testing.T) {
	state := NewSSFState(nil)
	if state.config == nil {
		t.Fatal("NewSSFState(nil) should use default config")
	}
	if state.config.SlotDuration != 12 {
		t.Errorf("default config SlotDuration = %d, want 12", state.config.SlotDuration)
	}
}
