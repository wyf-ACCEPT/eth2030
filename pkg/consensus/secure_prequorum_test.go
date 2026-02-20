package consensus

import (
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// --- helpers ---

func secureBlockRoot(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	return h
}

func makeVRFProof(validatorIdx uint64) []byte {
	// Deterministic VRF proof for testing; real VRF proofs would be cryptographic.
	return []byte{byte(validatorIdx), 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x01}
}

func makeSecureVote(slot, validatorIdx uint64, blockRoot types.Hash) *SecurePrequorumVote {
	vrfProof := makeVRFProof(validatorIdx)
	commitment := ComputeSecureCommitment(slot, validatorIdx, blockRoot, vrfProof)
	return &SecurePrequorumVote{
		ValidatorIndex: validatorIdx,
		Slot:           slot,
		BlockRoot:      blockRoot,
		VRFProof:       vrfProof,
		CommitmentHash: commitment,
	}
}

func makeReveal(slot, validatorIdx uint64, blockRoot types.Hash) *SecureVoteReveal {
	vrfProof := makeVRFProof(validatorIdx)
	return &SecureVoteReveal{
		ValidatorIndex: validatorIdx,
		Slot:           slot,
		BlockRoot:      blockRoot,
		VRFProof:       vrfProof,
	}
}

// --- Config tests ---

func TestDefaultSecurePrequorumConfig(t *testing.T) {
	cfg := DefaultSecurePrequorumConfig()
	if cfg.Threshold != 0.67 {
		t.Errorf("Threshold: want 0.67, got %f", cfg.Threshold)
	}
	if cfg.MinValidators != 3 {
		t.Errorf("MinValidators: want 3, got %d", cfg.MinValidators)
	}
	if cfg.ValidatorSetSize != 1000 {
		t.Errorf("ValidatorSetSize: want 1000, got %d", cfg.ValidatorSetSize)
	}
	if cfg.MaxVotesPerSlot != 10_000 {
		t.Errorf("MaxVotesPerSlot: want 10000, got %d", cfg.MaxVotesPerSlot)
	}
	if cfg.Timeout == 0 {
		t.Error("Timeout should be non-zero")
	}
}

func TestNewSecurePrequorumState_Defaults(t *testing.T) {
	state := NewSecurePrequorumState(SecurePrequorumConfig{})
	cfg := state.Config()
	if cfg.Threshold != 0.67 {
		t.Errorf("Threshold should default to 0.67, got %f", cfg.Threshold)
	}
	if cfg.MinValidators != 3 {
		t.Errorf("MinValidators should default to 3, got %d", cfg.MinValidators)
	}
	if cfg.ValidatorSetSize != 1000 {
		t.Errorf("ValidatorSetSize should default to 1000, got %d", cfg.ValidatorSetSize)
	}
}

func TestNewSecurePrequorumState_InvalidThreshold(t *testing.T) {
	state := NewSecurePrequorumState(SecurePrequorumConfig{Threshold: -1})
	cfg := state.Config()
	if cfg.Threshold != 0.67 {
		t.Errorf("negative threshold should be corrected to 0.67, got %f", cfg.Threshold)
	}
}

// --- CastSecureVote tests ---

func TestCastSecureVote_Basic(t *testing.T) {
	state := NewSecurePrequorumState(DefaultSecurePrequorumConfig())
	vote := makeSecureVote(1, 0, secureBlockRoot(0x01))

	if err := state.CastSecureVote(vote); err != nil {
		t.Fatalf("CastSecureVote: %v", err)
	}

	status := state.ReachSecureQuorum(1)
	if status.CommittedCount != 1 {
		t.Errorf("CommittedCount: want 1, got %d", status.CommittedCount)
	}
}

func TestCastSecureVote_NilVote(t *testing.T) {
	state := NewSecurePrequorumState(DefaultSecurePrequorumConfig())
	if err := state.CastSecureVote(nil); err != ErrSecureVoteNil {
		t.Errorf("nil vote: want ErrSecureVoteNil, got %v", err)
	}
}

func TestCastSecureVote_ZeroSlot(t *testing.T) {
	state := NewSecurePrequorumState(DefaultSecurePrequorumConfig())
	vote := makeSecureVote(1, 0, secureBlockRoot(0x01))
	vote.Slot = 0
	if err := state.CastSecureVote(vote); err != ErrSecureVoteZeroSlot {
		t.Errorf("zero slot: want ErrSecureVoteZeroSlot, got %v", err)
	}
}

func TestCastSecureVote_EmptyBlockRoot(t *testing.T) {
	state := NewSecurePrequorumState(DefaultSecurePrequorumConfig())
	vote := makeSecureVote(1, 0, types.Hash{})
	vote.BlockRoot = types.Hash{}
	if err := state.CastSecureVote(vote); err != ErrSecureVoteEmptyBlockRoot {
		t.Errorf("empty block root: want ErrSecureVoteEmptyBlockRoot, got %v", err)
	}
}

func TestCastSecureVote_EmptyVRF(t *testing.T) {
	state := NewSecurePrequorumState(DefaultSecurePrequorumConfig())
	vote := makeSecureVote(1, 0, secureBlockRoot(0x01))
	vote.VRFProof = nil
	if err := state.CastSecureVote(vote); err != ErrSecureVoteEmptyVRF {
		t.Errorf("empty VRF: want ErrSecureVoteEmptyVRF, got %v", err)
	}
}

func TestCastSecureVote_EmptyCommitment(t *testing.T) {
	state := NewSecurePrequorumState(DefaultSecurePrequorumConfig())
	vote := makeSecureVote(1, 0, secureBlockRoot(0x01))
	vote.CommitmentHash = types.Hash{}
	if err := state.CastSecureVote(vote); err != ErrSecureVoteEmptyCommitment {
		t.Errorf("empty commitment: want ErrSecureVoteEmptyCommitment, got %v", err)
	}
}

func TestCastSecureVote_WrongCommitment(t *testing.T) {
	state := NewSecurePrequorumState(DefaultSecurePrequorumConfig())
	vote := makeSecureVote(1, 0, secureBlockRoot(0x01))
	vote.CommitmentHash[0] ^= 0xFF // corrupt the commitment
	if err := state.CastSecureVote(vote); err != ErrSecureVoteEmptyCommitment {
		t.Errorf("wrong commitment: want ErrSecureVoteEmptyCommitment, got %v", err)
	}
}

func TestCastSecureVote_Duplicate(t *testing.T) {
	state := NewSecurePrequorumState(DefaultSecurePrequorumConfig())
	vote := makeSecureVote(1, 0, secureBlockRoot(0x01))

	if err := state.CastSecureVote(vote); err != nil {
		t.Fatalf("first vote: %v", err)
	}
	if err := state.CastSecureVote(vote); err != ErrSecureVoteDuplicate {
		t.Errorf("duplicate: want ErrSecureVoteDuplicate, got %v", err)
	}
}

func TestCastSecureVote_SlotFull(t *testing.T) {
	cfg := DefaultSecurePrequorumConfig()
	cfg.MaxVotesPerSlot = 2
	state := NewSecurePrequorumState(cfg)

	for i := uint64(0); i < 2; i++ {
		vote := makeSecureVote(1, i, secureBlockRoot(byte(i+1)))
		if err := state.CastSecureVote(vote); err != nil {
			t.Fatalf("vote %d: %v", i, err)
		}
	}

	vote := makeSecureVote(1, 99, secureBlockRoot(0x99))
	if err := state.CastSecureVote(vote); err != ErrSecureVoteSlotFull {
		t.Errorf("slot full: want ErrSecureVoteSlotFull, got %v", err)
	}
}

// --- RevealCommitment tests ---

func TestRevealCommitment_Basic(t *testing.T) {
	state := NewSecurePrequorumState(DefaultSecurePrequorumConfig())
	root := secureBlockRoot(0x01)

	// Phase 1: commit.
	vote := makeSecureVote(1, 0, root)
	if err := state.CastSecureVote(vote); err != nil {
		t.Fatalf("CastSecureVote: %v", err)
	}

	// Phase 2: reveal.
	reveal := makeReveal(1, 0, root)
	if err := state.RevealCommitment(reveal); err != nil {
		t.Fatalf("RevealCommitment: %v", err)
	}

	status := state.ReachSecureQuorum(1)
	if status.RevealedCount != 1 {
		t.Errorf("RevealedCount: want 1, got %d", status.RevealedCount)
	}
}

func TestRevealCommitment_NilReveal(t *testing.T) {
	state := NewSecurePrequorumState(DefaultSecurePrequorumConfig())
	if err := state.RevealCommitment(nil); err != ErrSecureRevealNil {
		t.Errorf("nil reveal: want ErrSecureRevealNil, got %v", err)
	}
}

func TestRevealCommitment_NoCommitment(t *testing.T) {
	state := NewSecurePrequorumState(DefaultSecurePrequorumConfig())
	reveal := makeReveal(1, 0, secureBlockRoot(0x01))
	if err := state.RevealCommitment(reveal); err != ErrSecureRevealNoCommitment {
		t.Errorf("no commitment: want ErrSecureRevealNoCommitment, got %v", err)
	}
}

func TestRevealCommitment_Mismatch(t *testing.T) {
	state := NewSecurePrequorumState(DefaultSecurePrequorumConfig())
	root := secureBlockRoot(0x01)

	vote := makeSecureVote(1, 0, root)
	if err := state.CastSecureVote(vote); err != nil {
		t.Fatalf("CastSecureVote: %v", err)
	}

	// Reveal with a different block root.
	reveal := makeReveal(1, 0, secureBlockRoot(0x02))
	if err := state.RevealCommitment(reveal); err != ErrSecureRevealMismatch {
		t.Errorf("mismatch: want ErrSecureRevealMismatch, got %v", err)
	}
}

func TestRevealCommitment_Duplicate(t *testing.T) {
	state := NewSecurePrequorumState(DefaultSecurePrequorumConfig())
	root := secureBlockRoot(0x01)

	vote := makeSecureVote(1, 0, root)
	if err := state.CastSecureVote(vote); err != nil {
		t.Fatalf("commit: %v", err)
	}

	reveal := makeReveal(1, 0, root)
	if err := state.RevealCommitment(reveal); err != nil {
		t.Fatalf("first reveal: %v", err)
	}
	if err := state.RevealCommitment(reveal); err != ErrSecureRevealDuplicate {
		t.Errorf("duplicate reveal: want ErrSecureRevealDuplicate, got %v", err)
	}
}

// --- VerifyVoteCommitment tests ---

func TestVerifyVoteCommitment_Valid(t *testing.T) {
	vote := makeSecureVote(1, 5, secureBlockRoot(0xAB))
	if !VerifyVoteCommitment(vote) {
		t.Fatal("valid vote should pass commitment verification")
	}
}

func TestVerifyVoteCommitment_Nil(t *testing.T) {
	if VerifyVoteCommitment(nil) {
		t.Fatal("nil vote should fail")
	}
}

func TestVerifyVoteCommitment_Tampered(t *testing.T) {
	vote := makeSecureVote(1, 5, secureBlockRoot(0xAB))
	vote.CommitmentHash[0] ^= 0xFF
	if VerifyVoteCommitment(vote) {
		t.Fatal("tampered commitment should fail")
	}
}

// --- ComputeVRFWeight tests ---

func TestComputeVRFWeight_Nonzero(t *testing.T) {
	w := ComputeVRFWeight([]byte{0x01, 0x02, 0x03, 0x04}, 100)
	if w <= 0 {
		t.Errorf("weight should be positive, got %f", w)
	}
	if w > 1.0 {
		t.Errorf("weight should be <= 1.0, got %f", w)
	}
}

func TestComputeVRFWeight_ZeroInputs(t *testing.T) {
	if w := ComputeVRFWeight(nil, 100); w != 0 {
		t.Errorf("nil proof: want 0, got %f", w)
	}
	if w := ComputeVRFWeight([]byte{}, 100); w != 0 {
		t.Errorf("empty proof: want 0, got %f", w)
	}
	if w := ComputeVRFWeight([]byte{0x01}, 0); w != 0 {
		t.Errorf("zero set size: want 0, got %f", w)
	}
}

func TestComputeVRFWeight_Deterministic(t *testing.T) {
	proof := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	w1 := ComputeVRFWeight(proof, 100)
	w2 := ComputeVRFWeight(proof, 100)
	if w1 != w2 {
		t.Fatal("same inputs should produce same weight")
	}
}

func TestComputeVRFWeight_DifferentProofs(t *testing.T) {
	w1 := ComputeVRFWeight([]byte{0x01}, 100)
	w2 := ComputeVRFWeight([]byte{0x02}, 100)
	// Different proofs should (almost certainly) produce different weights.
	if w1 == w2 {
		t.Error("different proofs should usually produce different weights")
	}
}

func TestComputeVRFWeight_ScalesBySetSize(t *testing.T) {
	proof := []byte{0xDE, 0xAD}
	w1 := ComputeVRFWeight(proof, 10)
	w2 := ComputeVRFWeight(proof, 100)
	// Larger set size should produce smaller per-validator weight.
	if w2 >= w1 {
		t.Errorf("larger set should produce smaller weight: %f >= %f", w2, w1)
	}
}

// --- ReachSecureQuorum tests ---

func TestReachSecureQuorum_Empty(t *testing.T) {
	state := NewSecurePrequorumState(DefaultSecurePrequorumConfig())
	status := state.ReachSecureQuorum(1)
	if status.Slot != 1 {
		t.Errorf("Slot: want 1, got %d", status.Slot)
	}
	if status.CommittedCount != 0 || status.RevealedCount != 0 {
		t.Error("counts should be zero for empty slot")
	}
	if status.QuorumReached {
		t.Error("quorum should not be reached for empty slot")
	}
}

func TestReachSecureQuorum_BelowMinValidators(t *testing.T) {
	cfg := DefaultSecurePrequorumConfig()
	cfg.MinValidators = 5
	cfg.ValidatorSetSize = 10
	state := NewSecurePrequorumState(cfg)

	root := secureBlockRoot(0x01)
	// Commit and reveal from 3 validators (below min of 5).
	for i := uint64(0); i < 3; i++ {
		vote := makeSecureVote(1, i, root)
		if err := state.CastSecureVote(vote); err != nil {
			t.Fatalf("commit %d: %v", i, err)
		}
		reveal := makeReveal(1, i, root)
		if err := state.RevealCommitment(reveal); err != nil {
			t.Fatalf("reveal %d: %v", i, err)
		}
	}

	status := state.ReachSecureQuorum(1)
	if status.QuorumReached {
		t.Error("quorum should not be reached below minimum validators")
	}
	if status.RevealedCount != 3 {
		t.Errorf("RevealedCount: want 3, got %d", status.RevealedCount)
	}
}

func TestReachSecureQuorum_WithRevealedVotes(t *testing.T) {
	cfg := DefaultSecurePrequorumConfig()
	cfg.MinValidators = 1
	cfg.ValidatorSetSize = 10
	// Use a very low threshold that any VRF weight can satisfy.
	cfg.Threshold = 0.0001
	state := NewSecurePrequorumState(cfg)

	root := secureBlockRoot(0x01)
	// Commit and reveal from 5 validators.
	for i := uint64(0); i < 5; i++ {
		vote := makeSecureVote(1, i, root)
		if err := state.CastSecureVote(vote); err != nil {
			t.Fatalf("commit %d: %v", i, err)
		}
		reveal := makeReveal(1, i, root)
		if err := state.RevealCommitment(reveal); err != nil {
			t.Fatalf("reveal %d: %v", i, err)
		}
	}

	status := state.ReachSecureQuorum(1)
	if status.RevealedCount != 5 {
		t.Errorf("RevealedCount: want 5, got %d", status.RevealedCount)
	}
	if status.TotalWeight <= 0 {
		t.Error("TotalWeight should be positive after reveals")
	}
	if !status.QuorumReached {
		t.Error("quorum should be reached with low threshold")
	}
}

func TestReachSecureQuorum_OnlyRevealsCount(t *testing.T) {
	cfg := DefaultSecurePrequorumConfig()
	cfg.MinValidators = 1
	cfg.ValidatorSetSize = 10
	cfg.Threshold = 0.0001
	state := NewSecurePrequorumState(cfg)

	root := secureBlockRoot(0x01)
	// Commit from 5 validators but reveal from only 1.
	for i := uint64(0); i < 5; i++ {
		vote := makeSecureVote(1, i, root)
		if err := state.CastSecureVote(vote); err != nil {
			t.Fatalf("commit %d: %v", i, err)
		}
	}
	reveal := makeReveal(1, 0, root)
	if err := state.RevealCommitment(reveal); err != nil {
		t.Fatalf("reveal: %v", err)
	}

	status := state.ReachSecureQuorum(1)
	if status.CommittedCount != 5 {
		t.Errorf("CommittedCount: want 5, got %d", status.CommittedCount)
	}
	if status.RevealedCount != 1 {
		t.Errorf("RevealedCount: want 1, got %d", status.RevealedCount)
	}
}

// --- GetRevealedVotes ---

func TestGetRevealedVotes_Empty(t *testing.T) {
	state := NewSecurePrequorumState(DefaultSecurePrequorumConfig())
	votes := state.GetRevealedVotes(999)
	if votes != nil {
		t.Errorf("expected nil for empty slot, got %v", votes)
	}
}

func TestGetRevealedVotes_AfterReveals(t *testing.T) {
	state := NewSecurePrequorumState(DefaultSecurePrequorumConfig())
	root := secureBlockRoot(0x01)

	for i := uint64(0); i < 3; i++ {
		vote := makeSecureVote(1, i, root)
		if err := state.CastSecureVote(vote); err != nil {
			t.Fatalf("commit %d: %v", i, err)
		}
		reveal := makeReveal(1, i, root)
		if err := state.RevealCommitment(reveal); err != nil {
			t.Fatalf("reveal %d: %v", i, err)
		}
	}

	votes := state.GetRevealedVotes(1)
	if len(votes) != 3 {
		t.Fatalf("expected 3 revealed votes, got %d", len(votes))
	}
}

// --- PurgeSecureSlot ---

func TestPurgeSecureSlot(t *testing.T) {
	state := NewSecurePrequorumState(DefaultSecurePrequorumConfig())
	root := secureBlockRoot(0x01)

	vote := makeSecureVote(1, 0, root)
	if err := state.CastSecureVote(vote); err != nil {
		t.Fatalf("commit: %v", err)
	}

	state.PurgeSecureSlot(1)

	status := state.ReachSecureQuorum(1)
	if status.CommittedCount != 0 {
		t.Errorf("slot should be purged, got %d commits", status.CommittedCount)
	}
}

func TestPurgeSecureSlot_NonExistent(t *testing.T) {
	state := NewSecurePrequorumState(DefaultSecurePrequorumConfig())
	// Should not panic.
	state.PurgeSecureSlot(999)
}

// --- ComputeSecureCommitment ---

func TestComputeSecureCommitment_Deterministic(t *testing.T) {
	root := secureBlockRoot(0xAB)
	proof := []byte{0x01, 0x02, 0x03}

	c1 := ComputeSecureCommitment(1, 2, root, proof)
	c2 := ComputeSecureCommitment(1, 2, root, proof)
	if c1 != c2 {
		t.Fatal("commitment should be deterministic")
	}

	c3 := ComputeSecureCommitment(1, 3, root, proof)
	if c1 == c3 {
		t.Fatal("different validator should produce different commitment")
	}

	c4 := ComputeSecureCommitment(2, 2, root, proof)
	if c1 == c4 {
		t.Fatal("different slot should produce different commitment")
	}

	c5 := ComputeSecureCommitment(1, 2, secureBlockRoot(0xCD), proof)
	if c1 == c5 {
		t.Fatal("different block root should produce different commitment")
	}

	c6 := ComputeSecureCommitment(1, 2, root, []byte{0x04, 0x05})
	if c1 == c6 {
		t.Fatal("different VRF proof should produce different commitment")
	}
}

// --- Multiple slots ---

func TestSecurePrequorum_MultipleSlots(t *testing.T) {
	cfg := DefaultSecurePrequorumConfig()
	cfg.MinValidators = 1
	cfg.ValidatorSetSize = 10
	cfg.Threshold = 0.0001
	state := NewSecurePrequorumState(cfg)

	root := secureBlockRoot(0x01)

	// Slot 10: commit+reveal from 3 validators.
	for i := uint64(0); i < 3; i++ {
		vote := makeSecureVote(10, i, root)
		state.CastSecureVote(vote)
		state.RevealCommitment(makeReveal(10, i, root))
	}

	// Slot 20: commit only from 1 validator.
	vote := makeSecureVote(20, 0, root)
	state.CastSecureVote(vote)

	s10 := state.ReachSecureQuorum(10)
	if s10.RevealedCount != 3 {
		t.Errorf("slot 10 reveals: want 3, got %d", s10.RevealedCount)
	}

	s20 := state.ReachSecureQuorum(20)
	if s20.RevealedCount != 0 {
		t.Errorf("slot 20 reveals: want 0, got %d", s20.RevealedCount)
	}
	if s20.CommittedCount != 1 {
		t.Errorf("slot 20 commits: want 1, got %d", s20.CommittedCount)
	}
}

// --- Concurrency ---

func TestSecurePrequorum_Concurrent(t *testing.T) {
	cfg := DefaultSecurePrequorumConfig()
	cfg.MaxVotesPerSlot = 10_000
	cfg.ValidatorSetSize = 100
	state := NewSecurePrequorumState(cfg)

	root := secureBlockRoot(0x01)
	var wg sync.WaitGroup

	// Concurrent commits.
	for i := uint64(0); i < 50; i++ {
		wg.Add(1)
		go func(idx uint64) {
			defer wg.Done()
			vote := makeSecureVote(1, idx, root)
			_ = state.CastSecureVote(vote)
		}(i)
	}
	wg.Wait()

	// Concurrent reveals.
	for i := uint64(0); i < 50; i++ {
		wg.Add(1)
		go func(idx uint64) {
			defer wg.Done()
			reveal := makeReveal(1, idx, root)
			_ = state.RevealCommitment(reveal)
		}(i)
	}
	wg.Wait()

	status := state.ReachSecureQuorum(1)
	if status.CommittedCount != 50 {
		t.Errorf("CommittedCount: want 50, got %d", status.CommittedCount)
	}
	if status.RevealedCount != 50 {
		t.Errorf("RevealedCount: want 50, got %d", status.RevealedCount)
	}
}

func TestSecurePrequorum_ConcurrentReadWrite(t *testing.T) {
	cfg := DefaultSecurePrequorumConfig()
	cfg.ValidatorSetSize = 100
	state := NewSecurePrequorumState(cfg)

	root := secureBlockRoot(0x01)
	var wg sync.WaitGroup

	// Writers.
	for i := uint64(0); i < 20; i++ {
		wg.Add(1)
		go func(idx uint64) {
			defer wg.Done()
			vote := makeSecureVote(5, idx, root)
			_ = state.CastSecureVote(vote)
			reveal := makeReveal(5, idx, root)
			_ = state.RevealCommitment(reveal)
		}(i)
	}

	// Readers.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = state.ReachSecureQuorum(5)
			_ = state.GetRevealedVotes(5)
		}()
	}

	wg.Wait()
}

// --- Full commit-reveal flow ---

func TestSecurePrequorum_FullCommitRevealFlow(t *testing.T) {
	cfg := DefaultSecurePrequorumConfig()
	cfg.MinValidators = 3
	cfg.ValidatorSetSize = 10
	cfg.Threshold = 0.0001
	state := NewSecurePrequorumState(cfg)

	root := secureBlockRoot(0x42)

	// Phase 1: all validators commit.
	for i := uint64(0); i < 10; i++ {
		vote := makeSecureVote(1, i, root)
		if err := state.CastSecureVote(vote); err != nil {
			t.Fatalf("commit %d: %v", i, err)
		}
	}

	// After commits only, no reveals yet.
	status := state.ReachSecureQuorum(1)
	if status.CommittedCount != 10 {
		t.Errorf("CommittedCount: want 10, got %d", status.CommittedCount)
	}
	if status.RevealedCount != 0 {
		t.Errorf("RevealedCount: want 0, got %d", status.RevealedCount)
	}

	// Phase 2: all validators reveal.
	for i := uint64(0); i < 10; i++ {
		reveal := makeReveal(1, i, root)
		if err := state.RevealCommitment(reveal); err != nil {
			t.Fatalf("reveal %d: %v", i, err)
		}
	}

	status = state.ReachSecureQuorum(1)
	if status.RevealedCount != 10 {
		t.Errorf("RevealedCount: want 10, got %d", status.RevealedCount)
	}
	if !status.QuorumReached {
		t.Error("quorum should be reached with all validators revealed and low threshold")
	}
	if status.TotalWeight <= 0 {
		t.Error("TotalWeight should be positive")
	}

	votes := state.GetRevealedVotes(1)
	if len(votes) != 10 {
		t.Errorf("revealed votes: want 10, got %d", len(votes))
	}
}
