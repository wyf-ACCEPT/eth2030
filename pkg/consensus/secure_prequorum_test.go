package consensus

import (
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func secureBlockRoot(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	return h
}

func makeVRFProof(validatorIdx uint64) []byte {
	return []byte{byte(validatorIdx), 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x01}
}

func makeSecureVote(slot, valIdx uint64, root types.Hash) *SecurePrequorumVote {
	vrf := makeVRFProof(valIdx)
	commitment := ComputeSecureCommitment(slot, valIdx, root, vrf)
	return &SecurePrequorumVote{
		ValidatorIndex: valIdx, Slot: slot, BlockRoot: root,
		VRFProof: vrf, CommitmentHash: commitment,
	}
}

func makeReveal(slot, valIdx uint64, root types.Hash) *SecureVoteReveal {
	return &SecureVoteReveal{
		ValidatorIndex: valIdx, Slot: slot, BlockRoot: root,
		VRFProof: makeVRFProof(valIdx),
	}
}

func TestDefaultSecurePrequorumConfig(t *testing.T) {
	cfg := DefaultSecurePrequorumConfig()
	if cfg.Threshold != 0.67 || cfg.MinValidators != 3 || cfg.ValidatorSetSize != 1000 {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
}

func TestNewSecurePrequorumState_Defaults(t *testing.T) {
	s := NewSecurePrequorumState(SecurePrequorumConfig{})
	cfg := s.Config()
	if cfg.Threshold != 0.67 || cfg.MinValidators != 3 || cfg.ValidatorSetSize != 1000 {
		t.Errorf("invalid defaults: %+v", cfg)
	}
	s2 := NewSecurePrequorumState(SecurePrequorumConfig{Threshold: -1})
	if s2.Config().Threshold != 0.67 {
		t.Error("negative threshold not corrected")
	}
}

func TestCastSecureVote_Basic(t *testing.T) {
	s := NewSecurePrequorumState(DefaultSecurePrequorumConfig())
	vote := makeSecureVote(1, 0, secureBlockRoot(0x01))
	if err := s.CastSecureVote(vote); err != nil {
		t.Fatalf("CastSecureVote: %v", err)
	}
	status := s.ReachSecureQuorum(1)
	if status.CommittedCount != 1 {
		t.Errorf("CommittedCount: want 1, got %d", status.CommittedCount)
	}
}

func TestCastSecureVote_Errors(t *testing.T) {
	s := NewSecurePrequorumState(DefaultSecurePrequorumConfig())
	if err := s.CastSecureVote(nil); err != ErrSecureVoteNil {
		t.Errorf("nil: got %v", err)
	}
	v1 := makeSecureVote(1, 0, secureBlockRoot(0x01))
	v1.Slot = 0
	if err := s.CastSecureVote(v1); err != ErrSecureVoteZeroSlot {
		t.Errorf("zero slot: got %v", err)
	}
	v2 := makeSecureVote(1, 0, types.Hash{})
	v2.BlockRoot = types.Hash{}
	if err := s.CastSecureVote(v2); err != ErrSecureVoteEmptyBlockRoot {
		t.Errorf("empty root: got %v", err)
	}
	v3 := makeSecureVote(1, 0, secureBlockRoot(0x01))
	v3.VRFProof = nil
	if err := s.CastSecureVote(v3); err != ErrSecureVoteEmptyVRF {
		t.Errorf("empty VRF: got %v", err)
	}
	v4 := makeSecureVote(1, 0, secureBlockRoot(0x01))
	v4.CommitmentHash = types.Hash{}
	if err := s.CastSecureVote(v4); err != ErrSecureVoteEmptyCommitment {
		t.Errorf("empty commitment: got %v", err)
	}
	v5 := makeSecureVote(1, 0, secureBlockRoot(0x01))
	v5.CommitmentHash[0] ^= 0xFF
	if err := s.CastSecureVote(v5); err != ErrSecureVoteEmptyCommitment {
		t.Errorf("wrong commitment: got %v", err)
	}
}

func TestCastSecureVote_DuplicateAndSlotFull(t *testing.T) {
	s := NewSecurePrequorumState(DefaultSecurePrequorumConfig())
	vote := makeSecureVote(1, 0, secureBlockRoot(0x01))
	s.CastSecureVote(vote)
	if err := s.CastSecureVote(vote); err != ErrSecureVoteDuplicate {
		t.Errorf("duplicate: got %v", err)
	}
	cfg := DefaultSecurePrequorumConfig()
	cfg.MaxVotesPerSlot = 2
	s2 := NewSecurePrequorumState(cfg)
	s2.CastSecureVote(makeSecureVote(1, 0, secureBlockRoot(1)))
	s2.CastSecureVote(makeSecureVote(1, 1, secureBlockRoot(2)))
	if err := s2.CastSecureVote(makeSecureVote(1, 99, secureBlockRoot(0x99))); err != ErrSecureVoteSlotFull {
		t.Errorf("slot full: got %v", err)
	}
}

func TestRevealCommitment_Basic(t *testing.T) {
	s := NewSecurePrequorumState(DefaultSecurePrequorumConfig())
	root := secureBlockRoot(0x01)
	s.CastSecureVote(makeSecureVote(1, 0, root))
	if err := s.RevealCommitment(makeReveal(1, 0, root)); err != nil {
		t.Fatalf("RevealCommitment: %v", err)
	}
	status := s.ReachSecureQuorum(1)
	if status.RevealedCount != 1 {
		t.Errorf("RevealedCount: want 1, got %d", status.RevealedCount)
	}
}

func TestRevealCommitment_Errors(t *testing.T) {
	s := NewSecurePrequorumState(DefaultSecurePrequorumConfig())
	if err := s.RevealCommitment(nil); err != ErrSecureRevealNil {
		t.Errorf("nil: got %v", err)
	}
	if err := s.RevealCommitment(makeReveal(1, 0, secureBlockRoot(1))); err != ErrSecureRevealNoCommitment {
		t.Errorf("no commitment: got %v", err)
	}
	root := secureBlockRoot(0x01)
	s.CastSecureVote(makeSecureVote(1, 0, root))
	// Mismatch: different block root.
	if err := s.RevealCommitment(makeReveal(1, 0, secureBlockRoot(0x02))); err != ErrSecureRevealMismatch {
		t.Errorf("mismatch: got %v", err)
	}
	s.RevealCommitment(makeReveal(1, 0, root))
	if err := s.RevealCommitment(makeReveal(1, 0, root)); err != ErrSecureRevealDuplicate {
		t.Errorf("duplicate reveal: got %v", err)
	}
}

func TestVerifyVoteCommitment(t *testing.T) {
	vote := makeSecureVote(1, 5, secureBlockRoot(0xAB))
	if !VerifyVoteCommitment(vote) {
		t.Fatal("valid vote should pass")
	}
	if VerifyVoteCommitment(nil) {
		t.Fatal("nil should fail")
	}
	vote.CommitmentHash[0] ^= 0xFF
	if VerifyVoteCommitment(vote) {
		t.Fatal("tampered should fail")
	}
}

func TestComputeVRFWeight(t *testing.T) {
	w := ComputeVRFWeight([]byte{0x01, 0x02, 0x03, 0x04}, 100)
	if w <= 0 || w > 1.0 {
		t.Errorf("weight out of range: %f", w)
	}
	if ComputeVRFWeight(nil, 100) != 0 || ComputeVRFWeight([]byte{}, 100) != 0 || ComputeVRFWeight([]byte{1}, 0) != 0 {
		t.Error("zero inputs should return 0")
	}
	w1 := ComputeVRFWeight([]byte{0xDE, 0xAD}, 10)
	w2 := ComputeVRFWeight([]byte{0xDE, 0xAD}, 100)
	if w2 >= w1 {
		t.Error("larger set should produce smaller weight")
	}
	// Deterministic.
	if ComputeVRFWeight([]byte{1}, 100) != ComputeVRFWeight([]byte{1}, 100) {
		t.Fatal("not deterministic")
	}
}

func TestReachSecureQuorum_Empty(t *testing.T) {
	s := NewSecurePrequorumState(DefaultSecurePrequorumConfig())
	status := s.ReachSecureQuorum(1)
	if status.CommittedCount != 0 || status.QuorumReached {
		t.Error("empty slot should have zero counts and no quorum")
	}
}

func TestReachSecureQuorum_BelowMinValidators(t *testing.T) {
	cfg := DefaultSecurePrequorumConfig()
	cfg.MinValidators = 5
	cfg.ValidatorSetSize = 10
	s := NewSecurePrequorumState(cfg)
	root := secureBlockRoot(0x01)
	for i := uint64(0); i < 3; i++ {
		s.CastSecureVote(makeSecureVote(1, i, root))
		s.RevealCommitment(makeReveal(1, i, root))
	}
	status := s.ReachSecureQuorum(1)
	if status.QuorumReached {
		t.Error("quorum should not be reached below min validators")
	}
}

func TestReachSecureQuorum_WithRevealedVotes(t *testing.T) {
	cfg := DefaultSecurePrequorumConfig()
	cfg.MinValidators = 1
	cfg.ValidatorSetSize = 10
	cfg.Threshold = 0.0001
	s := NewSecurePrequorumState(cfg)
	root := secureBlockRoot(0x01)
	for i := uint64(0); i < 5; i++ {
		s.CastSecureVote(makeSecureVote(1, i, root))
		s.RevealCommitment(makeReveal(1, i, root))
	}
	status := s.ReachSecureQuorum(1)
	if status.RevealedCount != 5 || status.TotalWeight <= 0 || !status.QuorumReached {
		t.Errorf("unexpected status: %+v", status)
	}
}

func TestReachSecureQuorum_OnlyRevealsCount(t *testing.T) {
	cfg := DefaultSecurePrequorumConfig()
	cfg.MinValidators = 1
	cfg.Threshold = 0.0001
	s := NewSecurePrequorumState(cfg)
	root := secureBlockRoot(0x01)
	for i := uint64(0); i < 5; i++ {
		s.CastSecureVote(makeSecureVote(1, i, root))
	}
	s.RevealCommitment(makeReveal(1, 0, root))
	status := s.ReachSecureQuorum(1)
	if status.CommittedCount != 5 || status.RevealedCount != 1 {
		t.Errorf("counts: committed=%d, revealed=%d", status.CommittedCount, status.RevealedCount)
	}
}

func TestGetRevealedVotes(t *testing.T) {
	s := NewSecurePrequorumState(DefaultSecurePrequorumConfig())
	if s.GetRevealedVotes(999) != nil {
		t.Error("empty slot should return nil")
	}
	root := secureBlockRoot(0x01)
	for i := uint64(0); i < 3; i++ {
		s.CastSecureVote(makeSecureVote(1, i, root))
		s.RevealCommitment(makeReveal(1, i, root))
	}
	if len(s.GetRevealedVotes(1)) != 3 {
		t.Error("should have 3 revealed votes")
	}
}

func TestPurgeSecureSlot(t *testing.T) {
	s := NewSecurePrequorumState(DefaultSecurePrequorumConfig())
	s.CastSecureVote(makeSecureVote(1, 0, secureBlockRoot(0x01)))
	s.PurgeSecureSlot(1)
	if s.ReachSecureQuorum(1).CommittedCount != 0 {
		t.Error("slot should be purged")
	}
	s.PurgeSecureSlot(999) // should not panic
}

func TestComputeSecureCommitment_Deterministic(t *testing.T) {
	root := secureBlockRoot(0xAB)
	proof := []byte{0x01, 0x02, 0x03}
	c1 := ComputeSecureCommitment(1, 2, root, proof)
	c2 := ComputeSecureCommitment(1, 2, root, proof)
	if c1 != c2 {
		t.Fatal("not deterministic")
	}
	if c1 == ComputeSecureCommitment(1, 3, root, proof) {
		t.Fatal("different validator should differ")
	}
	if c1 == ComputeSecureCommitment(2, 2, root, proof) {
		t.Fatal("different slot should differ")
	}
	if c1 == ComputeSecureCommitment(1, 2, secureBlockRoot(0xCD), proof) {
		t.Fatal("different root should differ")
	}
	if c1 == ComputeSecureCommitment(1, 2, root, []byte{0x04}) {
		t.Fatal("different VRF proof should differ")
	}
}

func TestSecurePrequorum_MultipleSlots(t *testing.T) {
	cfg := DefaultSecurePrequorumConfig()
	cfg.MinValidators = 1
	cfg.Threshold = 0.0001
	s := NewSecurePrequorumState(cfg)
	root := secureBlockRoot(0x01)
	for i := uint64(0); i < 3; i++ {
		s.CastSecureVote(makeSecureVote(10, i, root))
		s.RevealCommitment(makeReveal(10, i, root))
	}
	s.CastSecureVote(makeSecureVote(20, 0, root))
	if s.ReachSecureQuorum(10).RevealedCount != 3 {
		t.Error("slot 10 should have 3 reveals")
	}
	if s.ReachSecureQuorum(20).RevealedCount != 0 {
		t.Error("slot 20 should have 0 reveals")
	}
}

func TestSecurePrequorum_FullCommitRevealFlow(t *testing.T) {
	cfg := DefaultSecurePrequorumConfig()
	cfg.MinValidators = 3
	cfg.ValidatorSetSize = 10
	cfg.Threshold = 0.0001
	s := NewSecurePrequorumState(cfg)
	root := secureBlockRoot(0x42)
	for i := uint64(0); i < 10; i++ {
		s.CastSecureVote(makeSecureVote(1, i, root))
	}
	status := s.ReachSecureQuorum(1)
	if status.CommittedCount != 10 || status.RevealedCount != 0 {
		t.Errorf("after commits: committed=%d, revealed=%d", status.CommittedCount, status.RevealedCount)
	}
	for i := uint64(0); i < 10; i++ {
		s.RevealCommitment(makeReveal(1, i, root))
	}
	status = s.ReachSecureQuorum(1)
	if status.RevealedCount != 10 || !status.QuorumReached || status.TotalWeight <= 0 {
		t.Errorf("after reveals: %+v", status)
	}
	if len(s.GetRevealedVotes(1)) != 10 {
		t.Error("should have 10 revealed votes")
	}
}

func TestSecurePrequorum_Concurrent(t *testing.T) {
	cfg := DefaultSecurePrequorumConfig()
	cfg.MaxVotesPerSlot = 10_000
	cfg.ValidatorSetSize = 100
	s := NewSecurePrequorumState(cfg)
	root := secureBlockRoot(0x01)
	var wg sync.WaitGroup
	for i := uint64(0); i < 50; i++ {
		wg.Add(1)
		go func(idx uint64) {
			defer wg.Done()
			s.CastSecureVote(makeSecureVote(1, idx, root))
		}(i)
	}
	wg.Wait()
	for i := uint64(0); i < 50; i++ {
		wg.Add(1)
		go func(idx uint64) {
			defer wg.Done()
			s.RevealCommitment(makeReveal(1, idx, root))
		}(i)
	}
	wg.Wait()
	status := s.ReachSecureQuorum(1)
	if status.CommittedCount != 50 || status.RevealedCount != 50 {
		t.Errorf("committed=%d, revealed=%d", status.CommittedCount, status.RevealedCount)
	}
}

func TestSecurePrequorum_ConcurrentReadWrite(t *testing.T) {
	s := NewSecurePrequorumState(DefaultSecurePrequorumConfig())
	root := secureBlockRoot(0x01)
	var wg sync.WaitGroup
	for i := uint64(0); i < 20; i++ {
		wg.Add(1)
		go func(idx uint64) {
			defer wg.Done()
			s.CastSecureVote(makeSecureVote(5, idx, root))
			s.RevealCommitment(makeReveal(5, idx, root))
		}(i)
	}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.ReachSecureQuorum(5)
			s.GetRevealedVotes(5)
		}()
	}
	wg.Wait()
}
