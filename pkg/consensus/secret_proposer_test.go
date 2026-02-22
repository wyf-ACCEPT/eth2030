package consensus

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestDefaultSecretProposerConfig(t *testing.T) {
	cfg := DefaultSecretProposerConfig()
	if cfg.LookaheadSlots != 32 {
		t.Errorf("LookaheadSlots: got %d, want 32", cfg.LookaheadSlots)
	}
	if cfg.CommitmentPeriod != 2 {
		t.Errorf("CommitmentPeriod: got %d, want 2", cfg.CommitmentPeriod)
	}
	if cfg.RevealPeriod != 1 {
		t.Errorf("RevealPeriod: got %d, want 1", cfg.RevealPeriod)
	}
}

func TestCommitProposer(t *testing.T) {
	seed := types.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	selector := NewSecretProposerSelector(nil, seed)

	secret := []byte("my-secret-value")
	commitment, err := selector.CommitProposer(42, 100, secret)
	if err != nil {
		t.Fatalf("CommitProposer failed: %v", err)
	}
	if commitment.ValidatorIndex != 42 {
		t.Errorf("ValidatorIndex: got %d, want 42", commitment.ValidatorIndex)
	}
	if commitment.Slot != 100 {
		t.Errorf("Slot: got %d, want 100", commitment.Slot)
	}
	if commitment.CommitHash.IsZero() {
		t.Error("CommitHash should not be zero")
	}
}

func TestRevealProposer(t *testing.T) {
	seed := types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	selector := NewSecretProposerSelector(nil, seed)

	secret := []byte("reveal-me")
	_, err := selector.CommitProposer(7, 50, secret)
	if err != nil {
		t.Fatalf("CommitProposer failed: %v", err)
	}

	// Reveal with correct secret.
	validatorIndex, err := selector.RevealProposer(50, secret)
	if err != nil {
		t.Fatalf("RevealProposer failed: %v", err)
	}
	if validatorIndex != 7 {
		t.Errorf("ValidatorIndex: got %d, want 7", validatorIndex)
	}

	// Check that commitment now has the secret stored.
	c := selector.GetCommitment(50)
	if c == nil {
		t.Fatal("commitment should exist after reveal")
	}
	if c.RevealedAt != 50 {
		t.Errorf("RevealedAt: got %d, want 50", c.RevealedAt)
	}
}

func TestRevealProposerWrongSecret(t *testing.T) {
	seed := types.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222")
	selector := NewSecretProposerSelector(nil, seed)

	secret := []byte("correct-secret")
	_, err := selector.CommitProposer(10, 200, secret)
	if err != nil {
		t.Fatalf("CommitProposer failed: %v", err)
	}

	// Reveal with wrong secret.
	_, err = selector.RevealProposer(200, []byte("wrong-secret"))
	if err != ErrSPWrongSecret {
		t.Errorf("wrong secret: got %v, want ErrSPWrongSecret", err)
	}

	// Reveal for non-existent slot.
	_, err = selector.RevealProposer(999, secret)
	if err != ErrSPNoCommitment {
		t.Errorf("no commitment: got %v, want ErrSPNoCommitment", err)
	}
}

func TestIsCommitted(t *testing.T) {
	seed := types.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333")
	selector := NewSecretProposerSelector(nil, seed)

	if selector.IsCommitted(100) {
		t.Error("slot 100 should not be committed yet")
	}

	_, err := selector.CommitProposer(5, 100, []byte("secret"))
	if err != nil {
		t.Fatalf("CommitProposer failed: %v", err)
	}

	if !selector.IsCommitted(100) {
		t.Error("slot 100 should be committed")
	}
	if selector.IsCommitted(101) {
		t.Error("slot 101 should not be committed")
	}
}

func TestDetermineProposer(t *testing.T) {
	randaoMix := types.HexToHash("0x4444444444444444444444444444444444444444444444444444444444444444")

	idx := DetermineProposer(100, 64, randaoMix)
	if idx >= 64 {
		t.Errorf("proposer index %d should be < 64", idx)
	}

	// Zero validator count.
	idx = DetermineProposer(100, 0, randaoMix)
	if idx != 0 {
		t.Errorf("zero validators: got %d, want 0", idx)
	}

	// Negative (as int but passed as int to the func).
	idx = DetermineProposer(100, -1, randaoMix)
	if idx != 0 {
		t.Errorf("negative validators: got %d, want 0", idx)
	}
}

func TestDetermineProposerDeterministic(t *testing.T) {
	randaoMix := types.HexToHash("0x5555555555555555555555555555555555555555555555555555555555555555")

	idx1 := DetermineProposer(42, 128, randaoMix)
	idx2 := DetermineProposer(42, 128, randaoMix)
	if idx1 != idx2 {
		t.Errorf("same inputs should produce same output: %d != %d", idx1, idx2)
	}
}

func TestDetermineProposerDistribution(t *testing.T) {
	randaoMix := types.HexToHash("0x6666666666666666666666666666666666666666666666666666666666666666")

	// Run 100 different slots and check that we get at least 2 distinct proposers.
	seen := make(map[uint64]bool)
	for slot := uint64(0); slot < 100; slot++ {
		idx := DetermineProposer(slot, 32, randaoMix)
		if idx >= 32 {
			t.Fatalf("slot %d: proposer index %d out of range", slot, idx)
		}
		seen[idx] = true
	}
	if len(seen) < 2 {
		t.Errorf("expected at least 2 distinct proposers across 100 slots, got %d", len(seen))
	}
}

func TestValidateCommitReveal(t *testing.T) {
	secret := []byte("my-secret")
	commitment := &ProposerCommitment{
		ValidatorIndex: 5,
		Slot:           100,
		CommitHash:     computeCommitHash(5, 100, secret),
	}

	// Valid.
	if err := ValidateCommitReveal(commitment, secret, 100); err != nil {
		t.Errorf("valid commit-reveal: %v", err)
	}

	// Nil commitment.
	if err := ValidateCommitReveal(nil, secret, 100); err == nil {
		t.Error("expected error for nil commitment")
	}

	// Wrong secret.
	if err := ValidateCommitReveal(commitment, []byte("wrong"), 100); err == nil {
		t.Error("expected error for wrong secret")
	}

	// Empty secret.
	if err := ValidateCommitReveal(commitment, nil, 100); err == nil {
		t.Error("expected error for empty secret")
	}
}

func TestValidateSecretProposerConfig(t *testing.T) {
	cfg := DefaultSecretProposerConfig()
	if err := ValidateSecretProposerConfig(cfg); err != nil {
		t.Errorf("valid config: %v", err)
	}
	if err := ValidateSecretProposerConfig(nil); err == nil {
		t.Error("expected error for nil config")
	}
}
