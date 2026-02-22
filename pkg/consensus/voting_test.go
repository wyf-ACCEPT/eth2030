package consensus

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

func defaultVotingConfig() VotingManagerConfig {
	return VotingManagerConfig{
		QuorumThreshold:     0.67,
		MaxConcurrentRounds: 10,
		VoteTimeout:         5 * time.Second,
	}
}

func testProposalHash(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	return h
}

func TestVotingStartRound(t *testing.T) {
	vm := NewVotingManager(defaultVotingConfig())
	hash := testProposalHash(0x01)

	if err := vm.StartRound(1, hash, 0.67); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if vm.ActiveRounds() != 1 {
		t.Fatalf("expected 1 active round, got %d", vm.ActiveRounds())
	}
}

func TestVotingStartRoundDuplicate(t *testing.T) {
	vm := NewVotingManager(defaultVotingConfig())
	hash := testProposalHash(0x01)

	_ = vm.StartRound(1, hash, 0.67)
	err := vm.StartRound(1, hash, 0.67)

	if !errors.Is(err, ErrVotingRoundExists) {
		t.Fatalf("expected ErrVotingRoundExists, got %v", err)
	}
}

func TestVotingCastVote(t *testing.T) {
	vm := NewVotingManager(defaultVotingConfig())
	hash := testProposalHash(0x01)
	_ = vm.StartRound(1, hash, 0.67)

	vote := &Vote{
		VoterID:      "validator-1",
		Slot:         1,
		ProposalHash: hash,
		Timestamp:    time.Now(),
	}

	if err := vm.CastVote(vote); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	count, err := vm.GetVoteCount(1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 vote, got %d", count)
	}
}

func TestVotingCastVoteDuplicate(t *testing.T) {
	vm := NewVotingManager(defaultVotingConfig())
	hash := testProposalHash(0x01)
	_ = vm.StartRound(1, hash, 0.67)

	vote := &Vote{
		VoterID:      "validator-1",
		Slot:         1,
		ProposalHash: hash,
		Timestamp:    time.Now(),
	}

	_ = vm.CastVote(vote)
	err := vm.CastVote(vote)

	if !errors.Is(err, ErrVotingAlreadyVoted) {
		t.Fatalf("expected ErrVotingAlreadyVoted, got %v", err)
	}
}

func TestVotingCastVoteWrongProposal(t *testing.T) {
	vm := NewVotingManager(defaultVotingConfig())
	hash := testProposalHash(0x01)
	_ = vm.StartRound(1, hash, 0.67)

	vote := &Vote{
		VoterID:      "validator-1",
		Slot:         1,
		ProposalHash: testProposalHash(0xFF),
		Timestamp:    time.Now(),
	}

	err := vm.CastVote(vote)
	if !errors.Is(err, ErrVotingWrongProposal) {
		t.Fatalf("expected ErrVotingWrongProposal, got %v", err)
	}
}

func TestVotingCastVoteNoRound(t *testing.T) {
	vm := NewVotingManager(defaultVotingConfig())

	vote := &Vote{
		VoterID:      "validator-1",
		Slot:         99,
		ProposalHash: testProposalHash(0x01),
		Timestamp:    time.Now(),
	}

	err := vm.CastVote(vote)
	if !errors.Is(err, ErrVotingRoundNotFound) {
		t.Fatalf("expected ErrVotingRoundNotFound, got %v", err)
	}
}

func TestVotingCastVoteFinalized(t *testing.T) {
	vm := NewVotingManager(defaultVotingConfig())
	hash := testProposalHash(0x01)
	_ = vm.StartRound(1, hash, 0.67)
	_ = vm.FinalizeRound(1)

	vote := &Vote{
		VoterID:      "validator-1",
		Slot:         1,
		ProposalHash: hash,
		Timestamp:    time.Now(),
	}

	err := vm.CastVote(vote)
	if !errors.Is(err, ErrVotingRoundFinalized) {
		t.Fatalf("expected ErrVotingRoundFinalized, got %v", err)
	}
}

func TestVotingIsFinalized(t *testing.T) {
	vm := NewVotingManager(defaultVotingConfig())
	hash := testProposalHash(0x01)
	_ = vm.StartRound(1, hash, 0.67)

	if vm.IsFinalized(1) {
		t.Fatal("round should not be finalized yet")
	}

	_ = vm.FinalizeRound(1)

	if !vm.IsFinalized(1) {
		t.Fatal("round should be finalized")
	}
}

func TestVotingIsFinalizedMissing(t *testing.T) {
	vm := NewVotingManager(defaultVotingConfig())
	if vm.IsFinalized(42) {
		t.Fatal("non-existent round should not be finalized")
	}
}

func TestVotingGetVoteCountNoRound(t *testing.T) {
	vm := NewVotingManager(defaultVotingConfig())
	_, err := vm.GetVoteCount(99)
	if !errors.Is(err, ErrVotingRoundNotFound) {
		t.Fatalf("expected ErrVotingRoundNotFound, got %v", err)
	}
}

func TestVotingGetQuorum(t *testing.T) {
	vm := NewVotingManager(defaultVotingConfig())
	hash := testProposalHash(0x01)
	_ = vm.StartRound(1, hash, 0.67)

	q, err := vm.GetQuorum(1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q != 0 {
		t.Fatalf("expected quorum 0, got %f", q)
	}

	// Cast 3 votes.
	for i := 0; i < 3; i++ {
		v := &Vote{
			VoterID:      fmt.Sprintf("v-%d", i),
			Slot:         1,
			ProposalHash: hash,
			Timestamp:    time.Now(),
		}
		_ = vm.CastVote(v)
	}

	q, err = vm.GetQuorum(1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q != 3.0 {
		t.Fatalf("expected quorum 3.0, got %f", q)
	}
}

func TestVotingGetQuorumNoRound(t *testing.T) {
	vm := NewVotingManager(defaultVotingConfig())
	_, err := vm.GetQuorum(99)
	if !errors.Is(err, ErrVotingRoundNotFound) {
		t.Fatalf("expected ErrVotingRoundNotFound, got %v", err)
	}
}

func TestVotingFinalizeRoundNotFound(t *testing.T) {
	vm := NewVotingManager(defaultVotingConfig())
	err := vm.FinalizeRound(99)
	if !errors.Is(err, ErrVotingRoundNotFound) {
		t.Fatalf("expected ErrVotingRoundNotFound, got %v", err)
	}
}

func TestVotingFinalizeRoundAlreadyFinalized(t *testing.T) {
	vm := NewVotingManager(defaultVotingConfig())
	hash := testProposalHash(0x01)
	_ = vm.StartRound(1, hash, 0.67)
	_ = vm.FinalizeRound(1)

	err := vm.FinalizeRound(1)
	if !errors.Is(err, ErrVotingRoundFinalized) {
		t.Fatalf("expected ErrVotingRoundFinalized, got %v", err)
	}
}

func TestVotingExpireRounds(t *testing.T) {
	vm := NewVotingManager(defaultVotingConfig())

	// Create rounds at slots 1-5.
	for i := uint64(1); i <= 5; i++ {
		_ = vm.StartRound(i, testProposalHash(byte(i)), 0.67)
	}

	// Finalize slot 3 so it is not expired.
	_ = vm.FinalizeRound(3)

	// Expire rounds before slot 4 (slots 1, 2 are non-finalized and < 4).
	expired := vm.ExpireRounds(4)
	if expired != 2 {
		t.Fatalf("expected 2 expired rounds, got %d", expired)
	}

	// Slot 3 (finalized) + slot 4, 5 remain.
	if vm.ActiveRounds() != 3 {
		t.Fatalf("expected 3 active rounds, got %d", vm.ActiveRounds())
	}
}

func TestVotingExpireRoundsNone(t *testing.T) {
	vm := NewVotingManager(defaultVotingConfig())
	_ = vm.StartRound(10, testProposalHash(0x10), 0.67)

	expired := vm.ExpireRounds(5)
	if expired != 0 {
		t.Fatalf("expected 0 expired rounds, got %d", expired)
	}
}

func TestVotingGetRound(t *testing.T) {
	vm := NewVotingManager(defaultVotingConfig())
	hash := testProposalHash(0x01)
	_ = vm.StartRound(1, hash, 0.67)

	round, err := vm.GetRound(1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if round.Slot != 1 {
		t.Fatalf("expected slot 1, got %d", round.Slot)
	}
	if round.ProposalHash != hash {
		t.Fatalf("proposal hash mismatch")
	}
}

func TestVotingGetRoundNotFound(t *testing.T) {
	vm := NewVotingManager(defaultVotingConfig())
	_, err := vm.GetRound(99)
	if !errors.Is(err, ErrVotingRoundNotFound) {
		t.Fatalf("expected ErrVotingRoundNotFound, got %v", err)
	}
}

func TestVotingMaxConcurrentRounds(t *testing.T) {
	cfg := defaultVotingConfig()
	cfg.MaxConcurrentRounds = 2
	vm := NewVotingManager(cfg)

	_ = vm.StartRound(1, testProposalHash(0x01), 0.67)
	_ = vm.StartRound(2, testProposalHash(0x02), 0.67)

	err := vm.StartRound(3, testProposalHash(0x03), 0.67)
	if err == nil {
		t.Fatal("expected error when exceeding max concurrent rounds")
	}

	// Finalize one round to free a slot.
	_ = vm.FinalizeRound(1)

	if err := vm.StartRound(3, testProposalHash(0x03), 0.67); err != nil {
		t.Fatalf("unexpected error after finalizing: %v", err)
	}
}

func TestVotingMultipleVoters(t *testing.T) {
	vm := NewVotingManager(defaultVotingConfig())
	hash := testProposalHash(0x01)
	_ = vm.StartRound(1, hash, 0.67)

	for i := 0; i < 10; i++ {
		v := &Vote{
			VoterID:      fmt.Sprintf("voter-%d", i),
			Slot:         1,
			ProposalHash: hash,
			Timestamp:    time.Now(),
		}
		if err := vm.CastVote(v); err != nil {
			t.Fatalf("vote %d failed: %v", i, err)
		}
	}

	count, _ := vm.GetVoteCount(1)
	if count != 10 {
		t.Fatalf("expected 10 votes, got %d", count)
	}
}

func TestVotingConcurrentVotes(t *testing.T) {
	vm := NewVotingManager(defaultVotingConfig())
	hash := testProposalHash(0x01)
	_ = vm.StartRound(1, hash, 0.67)

	var wg sync.WaitGroup
	errCh := make(chan error, 50)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			v := &Vote{
				VoterID:      fmt.Sprintf("voter-%d", id),
				Slot:         1,
				ProposalHash: hash,
				Timestamp:    time.Now(),
			}
			if err := vm.CastVote(v); err != nil {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrent vote error: %v", err)
	}

	count, _ := vm.GetVoteCount(1)
	if count != 50 {
		t.Fatalf("expected 50 votes, got %d", count)
	}
}

func TestVotingConcurrentStartRound(t *testing.T) {
	vm := NewVotingManager(VotingManagerConfig{
		MaxConcurrentRounds: 100,
		VoteTimeout:         5 * time.Second,
	})

	var wg sync.WaitGroup
	successes := make(chan uint64, 20)

	for i := uint64(1); i <= 20; i++ {
		wg.Add(1)
		go func(slot uint64) {
			defer wg.Done()
			if err := vm.StartRound(slot, testProposalHash(byte(slot)), 0.67); err == nil {
				successes <- slot
			}
		}(i)
	}

	wg.Wait()
	close(successes)

	count := 0
	for range successes {
		count++
	}
	if count != 20 {
		t.Fatalf("expected 20 successful round starts, got %d", count)
	}
}

func TestVotingZeroThresholdQuorum(t *testing.T) {
	vm := NewVotingManager(defaultVotingConfig())
	hash := testProposalHash(0x01)
	_ = vm.StartRound(1, hash, 0.0)

	q, err := vm.GetQuorum(1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q != 0 {
		t.Fatalf("expected 0 quorum for zero threshold, got %f", q)
	}
}

func TestVotingActiveRoundsEmpty(t *testing.T) {
	vm := NewVotingManager(defaultVotingConfig())
	if vm.ActiveRounds() != 0 {
		t.Fatalf("expected 0 active rounds, got %d", vm.ActiveRounds())
	}
}
