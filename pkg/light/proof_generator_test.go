package light

import (
	"testing"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

func TestCLProofDefaultConfig(t *testing.T) {
	cfg := DefaultCLProofConfig()

	if cfg.MaxProofDepth != 40 {
		t.Errorf("MaxProofDepth = %d, want 40", cfg.MaxProofDepth)
	}
	if cfg.CacheSize != 1000 {
		t.Errorf("CacheSize = %d, want 1000", cfg.CacheSize)
	}
	if cfg.ProofTTL != 12*time.Second {
		t.Errorf("ProofTTL = %v, want 12s", cfg.ProofTTL)
	}
}

func TestGenerateStateRootProof(t *testing.T) {
	gen := NewCLProofGenerator(DefaultCLProofConfig())

	stateRoot := types.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	slot := uint64(100)

	proof, err := gen.GenerateStateRootProof(slot, stateRoot)
	if err != nil {
		t.Fatalf("GenerateStateRootProof() error = %v", err)
	}

	if proof.Type != CLProofTypeStateRoot {
		t.Errorf("Type = %d, want %d", proof.Type, CLProofTypeStateRoot)
	}
	if proof.Slot != slot {
		t.Errorf("Slot = %d, want %d", proof.Slot, slot)
	}
	if proof.Root.IsZero() {
		t.Error("Root should not be zero")
	}
	if len(proof.Proof) != 40 {
		t.Errorf("len(Proof) = %d, want 40", len(proof.Proof))
	}
	if len(proof.Leaf) == 0 {
		t.Error("Leaf should not be empty")
	}
	if proof.LeafIndex != slot {
		t.Errorf("LeafIndex = %d, want %d", proof.LeafIndex, slot)
	}

	// Verify the generated proof.
	if !VerifyProof(proof) {
		t.Error("VerifyProof() returned false for valid state root proof")
	}
}

func TestGenerateStateRootProofZeroRoot(t *testing.T) {
	gen := NewCLProofGenerator(DefaultCLProofConfig())

	_, err := gen.GenerateStateRootProof(0, types.Hash{})
	if err == nil {
		t.Error("expected error for zero state root, got nil")
	}
}

func TestGenerateValidatorProof(t *testing.T) {
	gen := NewCLProofGenerator(DefaultCLProofConfig())

	slot := uint64(200)
	validatorIndex := uint64(42)
	pubkey := []byte("fake-pubkey-48-bytes-for-testing-only-not-real!!")
	balance := uint64(32_000_000_000) // 32 ETH in Gwei

	proof, err := gen.GenerateValidatorProof(slot, validatorIndex, pubkey, balance)
	if err != nil {
		t.Fatalf("GenerateValidatorProof() error = %v", err)
	}

	if proof.Type != CLProofTypeValidator {
		t.Errorf("Type = %d, want %d", proof.Type, CLProofTypeValidator)
	}
	if proof.Slot != slot {
		t.Errorf("Slot = %d, want %d", proof.Slot, slot)
	}
	if proof.LeafIndex != validatorIndex {
		t.Errorf("LeafIndex = %d, want %d", proof.LeafIndex, validatorIndex)
	}
	if len(proof.Leaf) != 32 {
		t.Errorf("len(Leaf) = %d, want 32 (Keccak256 output)", len(proof.Leaf))
	}
	if !VerifyProof(proof) {
		t.Error("VerifyProof() returned false for valid validator proof")
	}
}

func TestGenerateValidatorProofEmptyPubkey(t *testing.T) {
	gen := NewCLProofGenerator(DefaultCLProofConfig())

	_, err := gen.GenerateValidatorProof(0, 0, nil, 0)
	if err == nil {
		t.Error("expected error for empty pubkey, got nil")
	}

	_, err = gen.GenerateValidatorProof(0, 0, []byte{}, 0)
	if err == nil {
		t.Error("expected error for empty pubkey, got nil")
	}
}

func TestGenerateBalanceProof(t *testing.T) {
	gen := NewCLProofGenerator(DefaultCLProofConfig())

	slot := uint64(300)
	validatorIndex := uint64(99)
	balance := uint64(31_500_000_000)

	proof, err := gen.GenerateBalanceProof(slot, validatorIndex, balance)
	if err != nil {
		t.Fatalf("GenerateBalanceProof() error = %v", err)
	}

	if proof.Type != CLProofTypeBalance {
		t.Errorf("Type = %d, want %d", proof.Type, CLProofTypeBalance)
	}
	if proof.Slot != slot {
		t.Errorf("Slot = %d, want %d", proof.Slot, slot)
	}
	if proof.LeafIndex != validatorIndex {
		t.Errorf("LeafIndex = %d, want %d", proof.LeafIndex, validatorIndex)
	}
	if !VerifyProof(proof) {
		t.Error("VerifyProof() returned false for valid balance proof")
	}
}

func TestVerifyProofValid(t *testing.T) {
	gen := NewCLProofGenerator(DefaultCLProofConfig())

	// Generate multiple proofs and verify each.
	stateRoot := types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	p1, err := gen.GenerateStateRootProof(1, stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyProof(p1) {
		t.Error("state root proof failed verification")
	}

	pubkey := []byte("test-validator-pubkey-48-bytes-here-filling-pad!!")
	p2, err := gen.GenerateValidatorProof(1, 5, pubkey, 32_000_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyProof(p2) {
		t.Error("validator proof failed verification")
	}

	p3, err := gen.GenerateBalanceProof(1, 10, 64_000_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyProof(p3) {
		t.Error("balance proof failed verification")
	}
}

func TestVerifyProofTampered(t *testing.T) {
	gen := NewCLProofGenerator(DefaultCLProofConfig())

	stateRoot := types.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222")
	proof, err := gen.GenerateStateRootProof(50, stateRoot)
	if err != nil {
		t.Fatal(err)
	}

	// Tamper with the leaf.
	tampered := &CLProof{
		Type:      proof.Type,
		Slot:      proof.Slot,
		Root:      proof.Root,
		Proof:     proof.Proof,
		Leaf:      make([]byte, len(proof.Leaf)),
		LeafIndex: proof.LeafIndex,
		Timestamp: proof.Timestamp,
	}
	copy(tampered.Leaf, proof.Leaf)
	tampered.Leaf[0] ^= 0xFF

	if VerifyProof(tampered) {
		t.Error("VerifyProof() returned true for tampered leaf")
	}
}

func TestVerifyProofWrongRoot(t *testing.T) {
	gen := NewCLProofGenerator(DefaultCLProofConfig())

	stateRoot := types.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333")
	proof, err := gen.GenerateStateRootProof(75, stateRoot)
	if err != nil {
		t.Fatal(err)
	}

	// Replace the root with a different hash.
	wrongRoot := &CLProof{
		Type:      proof.Type,
		Slot:      proof.Slot,
		Root:      types.HexToHash("0x4444444444444444444444444444444444444444444444444444444444444444"),
		Proof:     proof.Proof,
		Leaf:      proof.Leaf,
		LeafIndex: proof.LeafIndex,
		Timestamp: proof.Timestamp,
	}

	if VerifyProof(wrongRoot) {
		t.Error("VerifyProof() returned true for wrong root")
	}
}

func TestVerifyProofNilAndEmpty(t *testing.T) {
	if VerifyProof(nil) {
		t.Error("VerifyProof(nil) should return false")
	}

	if VerifyProof(&CLProof{}) {
		t.Error("VerifyProof(empty) should return false")
	}

	if VerifyProof(&CLProof{Leaf: []byte{1}, Proof: nil}) {
		t.Error("VerifyProof(no branch) should return false")
	}
}

func TestProofsGenerated(t *testing.T) {
	gen := NewCLProofGenerator(DefaultCLProofConfig())

	if gen.ProofsGenerated() != 0 {
		t.Errorf("initial ProofsGenerated() = %d, want 0", gen.ProofsGenerated())
	}

	stateRoot := types.HexToHash("0x5555555555555555555555555555555555555555555555555555555555555555")
	_, _ = gen.GenerateStateRootProof(1, stateRoot)
	if gen.ProofsGenerated() != 1 {
		t.Errorf("ProofsGenerated() = %d, want 1", gen.ProofsGenerated())
	}

	pubkey := []byte("test-pubkey-48-bytes-filling-the-required-length!")
	_, _ = gen.GenerateValidatorProof(2, 0, pubkey, 32_000_000_000)
	if gen.ProofsGenerated() != 2 {
		t.Errorf("ProofsGenerated() = %d, want 2", gen.ProofsGenerated())
	}

	_, _ = gen.GenerateBalanceProof(3, 0, 32_000_000_000)
	if gen.ProofsGenerated() != 3 {
		t.Errorf("ProofsGenerated() = %d, want 3", gen.ProofsGenerated())
	}

	// Failed generation should not increment.
	_, _ = gen.GenerateStateRootProof(4, types.Hash{})
	if gen.ProofsGenerated() != 3 {
		t.Errorf("ProofsGenerated() after failure = %d, want 3", gen.ProofsGenerated())
	}
}

func TestProofTTL(t *testing.T) {
	gen := NewCLProofGenerator(DefaultCLProofConfig())

	stateRoot := types.HexToHash("0x6666666666666666666666666666666666666666666666666666666666666666")
	proof, err := gen.GenerateStateRootProof(10, stateRoot)
	if err != nil {
		t.Fatal(err)
	}

	// Proof should have a recent timestamp.
	if time.Since(proof.Timestamp) > 5*time.Second {
		t.Errorf("proof timestamp %v is too old", proof.Timestamp)
	}

	// Verify the timestamp is not zero.
	if proof.Timestamp.IsZero() {
		t.Error("proof timestamp should not be zero")
	}
}

func TestProofDeterminism(t *testing.T) {
	// Two generators with the same config and inputs should produce
	// proofs with the same root (since the branch construction is deterministic).
	gen1 := NewCLProofGenerator(DefaultCLProofConfig())
	gen2 := NewCLProofGenerator(DefaultCLProofConfig())

	stateRoot := types.HexToHash("0x7777777777777777777777777777777777777777777777777777777777777777")

	p1, _ := gen1.GenerateStateRootProof(500, stateRoot)
	p2, _ := gen2.GenerateStateRootProof(500, stateRoot)

	if p1.Root != p2.Root {
		t.Error("deterministic generators produced different roots for same input")
	}
	if string(p1.Leaf) != string(p2.Leaf) {
		t.Error("deterministic generators produced different leaves for same input")
	}
}
