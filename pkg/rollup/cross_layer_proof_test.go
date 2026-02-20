package rollup

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func makeTestMessage(source, dest LayerID) *CrossLayerMessage {
	return &CrossLayerMessage{
		Source:      source,
		Destination: dest,
		Nonce:       1,
		Sender:      types.BytesToAddress([]byte{0x01, 0x02, 0x03}),
		Target:      types.BytesToAddress([]byte{0x04, 0x05, 0x06}),
		Value:       big.NewInt(1000),
		Data:        []byte{0xaa, 0xbb},
	}
}

func TestComputeMessageHash(t *testing.T) {
	msg := makeTestMessage(LayerL1, LayerL2)
	hash := ComputeMessageHash(msg)
	if hash == ([32]byte{}) {
		t.Fatal("message hash should not be zero")
	}

	// Deterministic.
	hash2 := ComputeMessageHash(msg)
	if hash != hash2 {
		t.Fatal("message hash should be deterministic")
	}

	// Different message produces different hash.
	msg2 := makeTestMessage(LayerL1, LayerL2)
	msg2.Nonce = 999
	hash3 := ComputeMessageHash(msg2)
	if hash == hash3 {
		t.Fatal("different messages should produce different hashes")
	}

	// Nil message.
	nilHash := ComputeMessageHash(nil)
	if nilHash != ([32]byte{}) {
		t.Fatal("nil message hash should be zero")
	}
}

func TestGenerateDepositProof(t *testing.T) {
	gen := NewMessageProofGenerator()
	msg := makeTestMessage(LayerL1, LayerL2)
	stateRoot := [32]byte{0x01, 0x02}

	proof, err := gen.GenerateDepositProof(msg, stateRoot)
	if err != nil {
		t.Fatalf("should generate deposit proof: %v", err)
	}
	if proof == nil {
		t.Fatal("proof should not be nil")
	}
	if len(proof.MerkleProof) == 0 {
		t.Fatal("merkle proof should not be empty")
	}
	if proof.MessageHash == ([32]byte{}) {
		t.Fatal("message hash should not be zero")
	}
}

func TestGenerateDepositProofErrors(t *testing.T) {
	gen := NewMessageProofGenerator()

	// Nil message.
	_, err := gen.GenerateDepositProof(nil, [32]byte{0x01})
	if err != ErrCrossLayerNilMessage {
		t.Fatalf("expected ErrCrossLayerNilMessage, got %v", err)
	}

	// Zero sender.
	msg := makeTestMessage(LayerL1, LayerL2)
	msg.Sender = types.Address{}
	_, err = gen.GenerateDepositProof(msg, [32]byte{0x01})
	if err != ErrCrossLayerZeroSender {
		t.Fatalf("expected ErrCrossLayerZeroSender, got %v", err)
	}

	// Zero state root.
	msg2 := makeTestMessage(LayerL1, LayerL2)
	_, err = gen.GenerateDepositProof(msg2, [32]byte{})
	if err != ErrCrossLayerStateRootZero {
		t.Fatalf("expected ErrCrossLayerStateRootZero, got %v", err)
	}

	// Wrong source layer.
	msg3 := makeTestMessage(LayerL2, LayerL1) // L2 source = not a deposit
	_, err = gen.GenerateDepositProof(msg3, [32]byte{0x01})
	if err != ErrCrossLayerInvalidSource {
		t.Fatalf("expected ErrCrossLayerInvalidSource, got %v", err)
	}
}

func TestGenerateWithdrawalProof(t *testing.T) {
	gen := NewMessageProofGenerator()
	msg := makeTestMessage(LayerL2, LayerL1)
	outputRoot := [32]byte{0x03, 0x04}

	proof, err := gen.GenerateWithdrawalProof(msg, outputRoot)
	if err != nil {
		t.Fatalf("should generate withdrawal proof: %v", err)
	}
	if proof == nil {
		t.Fatal("proof should not be nil")
	}
	if len(proof.MerkleProof) == 0 {
		t.Fatal("merkle proof should not be empty")
	}
}

func TestGenerateWithdrawalProofErrors(t *testing.T) {
	gen := NewMessageProofGenerator()

	// Zero output root.
	msg := makeTestMessage(LayerL2, LayerL1)
	_, err := gen.GenerateWithdrawalProof(msg, [32]byte{})
	if err != ErrCrossLayerOutputRootZero {
		t.Fatalf("expected ErrCrossLayerOutputRootZero, got %v", err)
	}

	// Wrong source layer.
	msg2 := makeTestMessage(LayerL1, LayerL2)
	_, err = gen.GenerateWithdrawalProof(msg2, [32]byte{0x01})
	if err != ErrCrossLayerInvalidSource {
		t.Fatalf("expected ErrCrossLayerInvalidSource, got %v", err)
	}
}

func TestVerifyCrossLayerDepositProof(t *testing.T) {
	gen := NewMessageProofGenerator()
	msg := makeTestMessage(LayerL1, LayerL2)

	// Generate the proof.
	msgHash := ComputeMessageHash(msg)
	dummyRoot := [32]byte{0x01}
	proof, err := gen.GenerateDepositProof(msg, dummyRoot)
	if err != nil {
		t.Fatalf("should generate proof: %v", err)
	}

	// Compute the actual root the proof produces.
	actualRoot := ComputeCrossLayerMerkleRoot(msgHash, proof.MerkleProof, msg.Nonce)

	// Verify against the actual root.
	valid, err := VerifyCrossLayerDepositProof(proof, actualRoot)
	if err != nil {
		t.Fatalf("should verify: %v", err)
	}
	if !valid {
		t.Fatal("deposit proof should be valid")
	}
}

func TestVerifyCrossLayerDepositProofErrors(t *testing.T) {
	// Nil proof.
	_, err := VerifyCrossLayerDepositProof(nil, [32]byte{0x01})
	if err != ErrCrossLayerNilProof {
		t.Fatalf("expected ErrCrossLayerNilProof, got %v", err)
	}

	// Nil message.
	proof := &MessageProof{MerkleProof: [][32]byte{{0x01}}}
	_, err = VerifyCrossLayerDepositProof(proof, [32]byte{0x01})
	if err != ErrCrossLayerNilMessage {
		t.Fatalf("expected ErrCrossLayerNilMessage, got %v", err)
	}

	// Zero state root.
	msg := makeTestMessage(LayerL1, LayerL2)
	proof2 := &MessageProof{
		Message:     msg,
		MessageHash: ComputeMessageHash(msg),
		MerkleProof: [][32]byte{{0x01}},
	}
	_, err = VerifyCrossLayerDepositProof(proof2, [32]byte{})
	if err != ErrCrossLayerStateRootZero {
		t.Fatalf("expected ErrCrossLayerStateRootZero, got %v", err)
	}

	// Empty Merkle proof.
	proof3 := &MessageProof{
		Message:     msg,
		MessageHash: ComputeMessageHash(msg),
	}
	_, err = VerifyCrossLayerDepositProof(proof3, [32]byte{0x01})
	if err != ErrCrossLayerEmptyMerkle {
		t.Fatalf("expected ErrCrossLayerEmptyMerkle, got %v", err)
	}
}

func TestVerifyCrossLayerWithdrawalProof(t *testing.T) {
	gen := NewMessageProofGenerator()
	msg := makeTestMessage(LayerL2, LayerL1)

	msgHash := ComputeMessageHash(msg)
	dummyRoot := [32]byte{0x05}
	proof, err := gen.GenerateWithdrawalProof(msg, dummyRoot)
	if err != nil {
		t.Fatalf("should generate proof: %v", err)
	}

	// Compute the actual root.
	actualRoot := ComputeCrossLayerMerkleRoot(msgHash, proof.MerkleProof, msg.Nonce)

	valid, err := VerifyCrossLayerWithdrawalProof(proof, actualRoot)
	if err != nil {
		t.Fatalf("should verify: %v", err)
	}
	if !valid {
		t.Fatal("withdrawal proof should be valid")
	}
}

func TestVerifyCrossLayerWithdrawalProofErrors(t *testing.T) {
	_, err := VerifyCrossLayerWithdrawalProof(nil, [32]byte{0x01})
	if err != ErrCrossLayerNilProof {
		t.Fatalf("expected ErrCrossLayerNilProof, got %v", err)
	}

	msg := makeTestMessage(LayerL2, LayerL1)
	proof := &MessageProof{
		Message:     msg,
		MessageHash: ComputeMessageHash(msg),
		MerkleProof: [][32]byte{{0x01}},
	}
	_, err = VerifyCrossLayerWithdrawalProof(proof, [32]byte{})
	if err != ErrCrossLayerOutputRootZero {
		t.Fatalf("expected ErrCrossLayerOutputRootZero, got %v", err)
	}
}

func TestVerifyCrossLayerDepositProofHashMismatch(t *testing.T) {
	msg := makeTestMessage(LayerL1, LayerL2)
	proof := &MessageProof{
		Message:     msg,
		MessageHash: [32]byte{0xff}, // wrong hash
		MerkleProof: [][32]byte{{0x01}},
	}
	_, err := VerifyCrossLayerDepositProof(proof, [32]byte{0x01})
	if err != ErrCrossLayerHashMismatch {
		t.Fatalf("expected ErrCrossLayerHashMismatch, got %v", err)
	}
}

func TestVerifyCrossLayerMerkleProof(t *testing.T) {
	leaf := [32]byte{0x01}
	sibling := [32]byte{0x02}
	proof := [][32]byte{sibling}

	// Compute root.
	root := ComputeCrossLayerMerkleRoot(leaf, proof, 0)

	// Verify.
	if !VerifyCrossLayerMerkleProof(leaf, root, proof, 0) {
		t.Fatal("valid Merkle proof should verify")
	}

	// Wrong root.
	if VerifyCrossLayerMerkleProof(leaf, [32]byte{0xff}, proof, 0) {
		t.Fatal("should fail with wrong root")
	}

	// Empty proof.
	if VerifyCrossLayerMerkleProof(leaf, root, nil, 0) {
		t.Fatal("should fail with empty proof")
	}
}

func TestVerifyCrossLayerMerkleProofDeeper(t *testing.T) {
	leaf := [32]byte{0xaa, 0xbb}
	siblings := [][32]byte{{0x01}, {0x02}, {0x03}, {0x04}}

	// Compute root with index = 5 (binary: 0101).
	root := ComputeCrossLayerMerkleRoot(leaf, siblings, 5)

	if !VerifyCrossLayerMerkleProof(leaf, root, siblings, 5) {
		t.Fatal("deeper Merkle proof should verify")
	}

	// Wrong index should fail.
	if VerifyCrossLayerMerkleProof(leaf, root, siblings, 3) {
		t.Fatal("wrong index should fail verification")
	}
}

func TestValidateMessage(t *testing.T) {
	// Valid message.
	msg := makeTestMessage(LayerL1, LayerL2)
	if err := validateMessage(msg); err != nil {
		t.Fatalf("valid message should pass: %v", err)
	}

	// Zero value.
	msg2 := makeTestMessage(LayerL1, LayerL2)
	msg2.Value = big.NewInt(0)
	if err := validateMessage(msg2); err != ErrCrossLayerZeroValue {
		t.Fatalf("expected ErrCrossLayerZeroValue, got %v", err)
	}

	// Nil value.
	msg3 := makeTestMessage(LayerL1, LayerL2)
	msg3.Value = nil
	if err := validateMessage(msg3); err != ErrCrossLayerZeroValue {
		t.Fatalf("expected ErrCrossLayerZeroValue, got %v", err)
	}

	// Zero target.
	msg4 := makeTestMessage(LayerL1, LayerL2)
	msg4.Target = types.Address{}
	if err := validateMessage(msg4); err != ErrCrossLayerZeroTarget {
		t.Fatalf("expected ErrCrossLayerZeroTarget, got %v", err)
	}

	// Invalid source.
	msg5 := makeTestMessage(LayerL1, LayerL2)
	msg5.Source = 99
	if err := validateMessage(msg5); err != ErrCrossLayerInvalidSource {
		t.Fatalf("expected ErrCrossLayerInvalidSource, got %v", err)
	}
}
