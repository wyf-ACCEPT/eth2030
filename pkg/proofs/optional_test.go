package proofs

import (
	"testing"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

func optBlockHash(id byte) types.Hash {
	var h types.Hash
	h[0] = 0xBB
	h[31] = id
	return h
}

func submitterAddr(id byte) types.Address {
	var a types.Address
	a[0] = id
	return a
}

func TestOptionalProofPolicy_AcceptAll(t *testing.T) {
	// Empty policy accepts all types.
	p := NewOptionalProofPolicy(nil)
	if !p.IsAccepted("ZK-SNARK") {
		t.Error("empty policy should accept ZK-SNARK")
	}
	if !p.IsAccepted("arbitrary-type") {
		t.Error("empty policy should accept arbitrary types")
	}
}

func TestOptionalProofPolicy_AcceptSpecific(t *testing.T) {
	p := NewOptionalProofPolicy([]string{"ZK-SNARK", "KZG"})
	if !p.IsAccepted("ZK-SNARK") {
		t.Error("should accept ZK-SNARK")
	}
	if !p.IsAccepted("KZG") {
		t.Error("should accept KZG")
	}
	if p.IsAccepted("IPA") {
		t.Error("should not accept IPA")
	}
}

func TestOptionalProofPolicy_AddAcceptedType(t *testing.T) {
	p := NewOptionalProofPolicy([]string{"ZK-SNARK"})
	if p.IsAccepted("IPA") {
		t.Error("IPA should not be accepted before adding")
	}
	p.AddAcceptedType("IPA")
	if !p.IsAccepted("IPA") {
		t.Error("IPA should be accepted after adding")
	}
}

func TestOptionalProofPolicy_AcceptedTypes(t *testing.T) {
	p := NewOptionalProofPolicy([]string{"ZK-SNARK", "KZG"})
	types := p.AcceptedTypes()
	if len(types) != 2 {
		t.Fatalf("expected 2 accepted types, got %d", len(types))
	}
	seen := make(map[string]bool)
	for _, ty := range types {
		seen[ty] = true
	}
	if !seen["ZK-SNARK"] || !seen["KZG"] {
		t.Error("expected ZK-SNARK and KZG in accepted types")
	}
}

func TestSubmitOptionalProof_Valid(t *testing.T) {
	store := NewOptionalProofStore(NewOptionalProofPolicy(nil))
	bh := optBlockHash(1)
	sub := &OptionalProofSubmission{
		BlockHash: bh,
		ProofType: "ZK-SNARK",
		ProofData: []byte{0x01, 0x02, 0x03},
		Submitter: submitterAddr(1),
		Timestamp: time.Now(),
	}
	if err := store.SubmitOptionalProof(sub); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.ProofCount(bh) != 1 {
		t.Fatalf("expected 1 proof, got %d", store.ProofCount(bh))
	}
}

func TestSubmitOptionalProof_NilSubmission(t *testing.T) {
	store := NewOptionalProofStore(nil)
	if err := store.SubmitOptionalProof(nil); err != ErrOptionalNilSubmission {
		t.Errorf("expected ErrOptionalNilSubmission, got %v", err)
	}
}

func TestSubmitOptionalProof_ZeroBlockHash(t *testing.T) {
	store := NewOptionalProofStore(nil)
	sub := &OptionalProofSubmission{
		ProofType: "ZK-SNARK",
		ProofData: []byte{0x01},
		Submitter: submitterAddr(1),
	}
	if err := store.SubmitOptionalProof(sub); err != ErrOptionalZeroBlockHash {
		t.Errorf("expected ErrOptionalZeroBlockHash, got %v", err)
	}
}

func TestSubmitOptionalProof_EmptySubmitter(t *testing.T) {
	store := NewOptionalProofStore(nil)
	sub := &OptionalProofSubmission{
		BlockHash: optBlockHash(1),
		ProofType: "ZK-SNARK",
		ProofData: []byte{0x01},
	}
	if err := store.SubmitOptionalProof(sub); err != ErrOptionalEmptySubmitter {
		t.Errorf("expected ErrOptionalEmptySubmitter, got %v", err)
	}
}

func TestSubmitOptionalProof_EmptyProofType(t *testing.T) {
	store := NewOptionalProofStore(nil)
	sub := &OptionalProofSubmission{
		BlockHash: optBlockHash(1),
		ProofData: []byte{0x01},
		Submitter: submitterAddr(1),
	}
	if err := store.SubmitOptionalProof(sub); err != ErrOptionalEmptyProofType {
		t.Errorf("expected ErrOptionalEmptyProofType, got %v", err)
	}
}

func TestSubmitOptionalProof_EmptyProofData(t *testing.T) {
	store := NewOptionalProofStore(nil)
	sub := &OptionalProofSubmission{
		BlockHash: optBlockHash(1),
		ProofType: "ZK-SNARK",
		Submitter: submitterAddr(1),
	}
	if err := store.SubmitOptionalProof(sub); err != ErrOptionalEmptyProofData {
		t.Errorf("expected ErrOptionalEmptyProofData, got %v", err)
	}
}

func TestSubmitOptionalProof_TypeNotAccepted(t *testing.T) {
	policy := NewOptionalProofPolicy([]string{"ZK-SNARK"})
	store := NewOptionalProofStore(policy)
	sub := &OptionalProofSubmission{
		BlockHash: optBlockHash(1),
		ProofType: "IPA",
		ProofData: []byte{0x01},
		Submitter: submitterAddr(1),
	}
	if err := store.SubmitOptionalProof(sub); err != ErrOptionalProofTypeNotAccepted {
		t.Errorf("expected ErrOptionalProofTypeNotAccepted, got %v", err)
	}
}

func TestSubmitOptionalProof_DuplicateSubmitter(t *testing.T) {
	store := NewOptionalProofStore(nil)
	bh := optBlockHash(1)
	addr := submitterAddr(1)

	sub := &OptionalProofSubmission{
		BlockHash: bh,
		ProofType: "ZK-SNARK",
		ProofData: []byte{0x01},
		Submitter: addr,
	}
	if err := store.SubmitOptionalProof(sub); err != nil {
		t.Fatalf("first submission failed: %v", err)
	}

	// Second submission from same submitter for same block should fail.
	sub2 := &OptionalProofSubmission{
		BlockHash: bh,
		ProofType: "ZK-STARK",
		ProofData: []byte{0x02},
		Submitter: addr,
	}
	if err := store.SubmitOptionalProof(sub2); err != ErrOptionalDuplicateProof {
		t.Errorf("expected ErrOptionalDuplicateProof, got %v", err)
	}
}

func TestGetProofsForBlock(t *testing.T) {
	store := NewOptionalProofStore(nil)
	bh := optBlockHash(1)

	// No proofs yet.
	if proofs := store.GetProofsForBlock(bh); proofs != nil {
		t.Fatalf("expected nil for no proofs, got %d", len(proofs))
	}

	// Submit 3 proofs from different submitters.
	for i := byte(1); i <= 3; i++ {
		store.SubmitOptionalProof(&OptionalProofSubmission{
			BlockHash: bh,
			ProofType: "ZK-SNARK",
			ProofData: []byte{i},
			Submitter: submitterAddr(i),
		})
	}

	proofs := store.GetProofsForBlock(bh)
	if len(proofs) != 3 {
		t.Fatalf("expected 3 proofs, got %d", len(proofs))
	}

	// Verify we get copies (modifying result does not affect store).
	proofs[0] = nil
	stored := store.GetProofsForBlock(bh)
	if stored[0] == nil {
		t.Error("modifying returned slice should not affect store")
	}
}

func TestGetProofsForBlock_MultipleBlocks(t *testing.T) {
	store := NewOptionalProofStore(nil)
	bh1 := optBlockHash(1)
	bh2 := optBlockHash(2)

	store.SubmitOptionalProof(&OptionalProofSubmission{
		BlockHash: bh1,
		ProofType: "ZK-SNARK",
		ProofData: []byte{0x01},
		Submitter: submitterAddr(1),
	})
	store.SubmitOptionalProof(&OptionalProofSubmission{
		BlockHash: bh2,
		ProofType: "ZK-SNARK",
		ProofData: []byte{0x02},
		Submitter: submitterAddr(1),
	})
	store.SubmitOptionalProof(&OptionalProofSubmission{
		BlockHash: bh2,
		ProofType: "IPA",
		ProofData: []byte{0x03},
		Submitter: submitterAddr(2),
	})

	if store.ProofCount(bh1) != 1 {
		t.Errorf("block1: expected 1 proof, got %d", store.ProofCount(bh1))
	}
	if store.ProofCount(bh2) != 2 {
		t.Errorf("block2: expected 2 proofs, got %d", store.ProofCount(bh2))
	}
}

func TestIsBlockVerified(t *testing.T) {
	store := NewOptionalProofStore(nil)
	bh := optBlockHash(1)

	// No proofs: not verified for any threshold.
	if store.IsBlockVerified(bh, 1) {
		t.Error("should not be verified with 0 proofs and minProofs=1")
	}

	// Submit 2 proofs.
	for i := byte(1); i <= 2; i++ {
		store.SubmitOptionalProof(&OptionalProofSubmission{
			BlockHash: bh,
			ProofType: "ZK-SNARK",
			ProofData: []byte{i},
			Submitter: submitterAddr(i),
		})
	}

	if !store.IsBlockVerified(bh, 1) {
		t.Error("should be verified with 2 proofs and minProofs=1")
	}
	if !store.IsBlockVerified(bh, 2) {
		t.Error("should be verified with 2 proofs and minProofs=2")
	}
	if store.IsBlockVerified(bh, 3) {
		t.Error("should not be verified with 2 proofs and minProofs=3")
	}
}

func TestIsBlockVerified_ZeroThreshold(t *testing.T) {
	store := NewOptionalProofStore(nil)
	bh := optBlockHash(1)
	// With 0 minProofs, any block is considered verified.
	if !store.IsBlockVerified(bh, 0) {
		t.Error("should be verified with minProofs=0")
	}
}

func TestProofRewardCalculator_BasicReward(t *testing.T) {
	calc := NewProofRewardCalculator(100, 2)

	// Non-first submitter, unknown proof type.
	reward := calc.CalculateReward("unknown", false)
	if reward != 100 {
		t.Errorf("expected base reward 100, got %d", reward)
	}

	// First submitter, unknown proof type.
	reward = calc.CalculateReward("unknown", true)
	if reward != 200 {
		t.Errorf("expected 200 (100*2), got %d", reward)
	}
}

func TestProofRewardCalculator_ProofTypeMultiplier(t *testing.T) {
	calc := NewProofRewardCalculator(100, 2)

	tests := []struct {
		proofType string
		first     bool
		expected  uint64
	}{
		{"ZK-SNARK", false, 300},   // 100 * 3
		{"ZK-SNARK", true, 600},    // 100 * 3 * 2
		{"ZK-STARK", false, 300},   // 100 * 3
		{"ZK-STARK", true, 600},    // 100 * 3 * 2
		{"IPA", false, 200},        // 100 * 2
		{"IPA", true, 400},         // 100 * 2 * 2
		{"KZG", false, 200},        // 100 * 2
		{"KZG", true, 400},         // 100 * 2 * 2
		{"unknown", false, 100},    // 100 * 1
		{"unknown", true, 200},     // 100 * 1 * 2
	}

	for _, tt := range tests {
		reward := calc.CalculateReward(tt.proofType, tt.first)
		if reward != tt.expected {
			t.Errorf("CalculateReward(%q, %v): got %d, want %d",
				tt.proofType, tt.first, reward, tt.expected)
		}
	}
}

func TestProofRewardCalculator_RewardPool(t *testing.T) {
	calc := NewProofRewardCalculator(100, 2)

	if calc.RewardPool() != 0 {
		t.Fatalf("initial pool should be 0, got %d", calc.RewardPool())
	}

	calc.CalculateReward("unknown", false) // 100
	calc.CalculateReward("unknown", true)  // 200
	calc.CalculateReward("ZK-SNARK", false) // 300

	expected := uint64(600)
	if calc.RewardPool() != expected {
		t.Errorf("expected pool %d, got %d", expected, calc.RewardPool())
	}
}

func TestProofRewardCalculator_Defaults(t *testing.T) {
	calc := NewProofRewardCalculator(0, 0)
	if calc.BaseReward != 100 {
		t.Errorf("default BaseReward: got %d, want 100", calc.BaseReward)
	}
	if calc.FirstSubmitterBonus != 2 {
		t.Errorf("default FirstSubmitterBonus: got %d, want 2", calc.FirstSubmitterBonus)
	}
}

func TestSubmitOptionalProof_AutoTimestamp(t *testing.T) {
	store := NewOptionalProofStore(nil)
	bh := optBlockHash(1)

	before := time.Now()
	store.SubmitOptionalProof(&OptionalProofSubmission{
		BlockHash: bh,
		ProofType: "ZK-SNARK",
		ProofData: []byte{0x01},
		Submitter: submitterAddr(1),
		// Timestamp intentionally left zero.
	})
	after := time.Now()

	proofs := store.GetProofsForBlock(bh)
	if len(proofs) != 1 {
		t.Fatalf("expected 1 proof, got %d", len(proofs))
	}
	ts := proofs[0].Timestamp
	if ts.Before(before) || ts.After(after) {
		t.Errorf("auto-assigned timestamp %v not in range [%v, %v]", ts, before, after)
	}
}

func TestOptionalProofStore_DifferentSubmittersSameBlock(t *testing.T) {
	store := NewOptionalProofStore(nil)
	bh := optBlockHash(1)

	// Different submitters should all succeed for the same block.
	for i := byte(1); i <= 5; i++ {
		err := store.SubmitOptionalProof(&OptionalProofSubmission{
			BlockHash: bh,
			ProofType: "ZK-SNARK",
			ProofData: []byte{i},
			Submitter: submitterAddr(i),
		})
		if err != nil {
			t.Fatalf("submitter %d: unexpected error: %v", i, err)
		}
	}

	if store.ProofCount(bh) != 5 {
		t.Errorf("expected 5 proofs, got %d", store.ProofCount(bh))
	}
	if !store.IsBlockVerified(bh, 3) {
		t.Error("should be verified with 5 proofs and minProofs=3")
	}
}

func TestOptionalProofStore_SameSubmitterDifferentBlocks(t *testing.T) {
	store := NewOptionalProofStore(nil)
	addr := submitterAddr(1)

	// Same submitter, different blocks should all succeed.
	for i := byte(1); i <= 3; i++ {
		err := store.SubmitOptionalProof(&OptionalProofSubmission{
			BlockHash: optBlockHash(i),
			ProofType: "ZK-SNARK",
			ProofData: []byte{i},
			Submitter: addr,
		})
		if err != nil {
			t.Fatalf("block %d: unexpected error: %v", i, err)
		}
	}

	for i := byte(1); i <= 3; i++ {
		if store.ProofCount(optBlockHash(i)) != 1 {
			t.Errorf("block %d: expected 1 proof", i)
		}
	}
}
