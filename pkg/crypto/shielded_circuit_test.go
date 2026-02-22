package crypto

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestShieldedCircuitPedersenCommitBN254(t *testing.T) {
	var rand1, rand2 [32]byte
	rand1[0] = 0x01
	rand2[0] = 0x02

	c1 := PedersenCommitBN254(100, rand1)
	c2 := PedersenCommitBN254(100, rand2)
	c3 := PedersenCommitBN254(200, rand1)

	if c1 == c2 {
		t.Error("different randomness should produce different commitments")
	}
	if c1 == c3 {
		t.Error("different amounts should produce different commitments")
	}
	if c1 == (types.Hash{}) {
		t.Error("commitment should not be zero")
	}
}

func TestShieldedCircuitPedersenDeterministic(t *testing.T) {
	var rand1 [32]byte
	rand1[0] = 0xaa
	c1 := PedersenCommitBN254(42, rand1)
	c2 := PedersenCommitBN254(42, rand1)
	if c1 != c2 {
		t.Error("same inputs should produce same commitment")
	}
}

func TestShieldedCircuitDeriveNullifier(t *testing.T) {
	var sk1, sk2 [32]byte
	sk1[0] = 0x01
	sk2[0] = 0x02

	n1 := DeriveNullifier(sk1, 0)
	n2 := DeriveNullifier(sk2, 0)
	n3 := DeriveNullifier(sk1, 1)

	if n1 == n2 {
		t.Error("different keys should produce different nullifiers")
	}
	if n1 == n3 {
		t.Error("different indices should produce different nullifiers")
	}
	if n1 == (types.Hash{}) {
		t.Error("nullifier should not be zero")
	}
}

func TestShieldedCircuitProveAndVerify(t *testing.T) {
	// Set up a commitment tree and generate a Merkle proof.
	ct := NewCommitmentTree()
	var sk [32]byte
	var randomness [32]byte
	var recipientPK [32]byte
	sk[0] = 0xaa
	randomness[0] = 0xbb
	recipientPK[0] = 0xcc

	amount := uint64(1000)
	commitment := PedersenCommitBN254(amount, randomness)

	idx, root, err := ct.Append(commitment)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	proof, err := ct.MerkleProof(idx)
	if err != nil {
		t.Fatalf("MerkleProof: %v", err)
	}

	// Convert siblings to [][32]byte.
	siblings := make([][32]byte, CommitTreeDepth)
	for i := 0; i < CommitTreeDepth; i++ {
		siblings[i] = proof.Siblings[i]
	}

	witness := &ShieldedTransferWitness{
		SecretKey:       sk,
		Amount:          amount,
		Randomness:      randomness,
		CommitmentIndex: idx,
		MerklePath:      siblings,
		MerkleRoot:      root,
		RecipientPK:     recipientPK,
	}

	circuitProof, err := ProveShieldedTransfer(witness)
	if err != nil {
		t.Fatalf("ProveShieldedTransfer: %v", err)
	}

	if circuitProof.Version != ShieldedCircuitVersion {
		t.Errorf("version: got %d, want %d", circuitProof.Version, ShieldedCircuitVersion)
	}
	if circuitProof.OutputCommitment != commitment {
		t.Error("output commitment mismatch")
	}

	// Verify.
	valid := VerifyShieldedTransfer(
		circuitProof,
		circuitProof.Nullifier,
		circuitProof.OutputCommitment,
		root,
	)
	if !valid {
		t.Error("valid proof should verify")
	}
}

func TestShieldedCircuitProveNilWitness(t *testing.T) {
	_, err := ProveShieldedTransfer(nil)
	if err != ErrShieldedCircuitNilWitness {
		t.Errorf("expected ErrShieldedCircuitNilWitness, got %v", err)
	}
}

func TestShieldedCircuitVerifyNilProof(t *testing.T) {
	if VerifyShieldedTransfer(nil, types.Hash{}, types.Hash{}, types.Hash{}) {
		t.Error("nil proof should not verify")
	}
}

func TestShieldedCircuitVerifyWrongVersion(t *testing.T) {
	proof := &ShieldedCircuitProof{Version: 0xff}
	if VerifyShieldedTransfer(proof, types.Hash{}, types.Hash{}, types.Hash{}) {
		t.Error("wrong version should not verify")
	}
}

func TestShieldedCircuitVerifyMismatchedNullifier(t *testing.T) {
	ct := NewCommitmentTree()
	var sk, randomness, recipientPK [32]byte
	sk[0] = 0x11
	randomness[0] = 0x22
	recipientPK[0] = 0x33

	commitment := PedersenCommitBN254(500, randomness)
	idx, root, _ := ct.Append(commitment)
	proof, _ := ct.MerkleProof(idx)

	siblings := make([][32]byte, CommitTreeDepth)
	for i := 0; i < CommitTreeDepth; i++ {
		siblings[i] = proof.Siblings[i]
	}

	witness := &ShieldedTransferWitness{
		SecretKey:       sk,
		Amount:          500,
		Randomness:      randomness,
		CommitmentIndex: idx,
		MerklePath:      siblings,
		MerkleRoot:      root,
		RecipientPK:     recipientPK,
	}

	circuitProof, _ := ProveShieldedTransfer(witness)

	// Verify with wrong nullifier.
	wrongNullifier := types.HexToHash("0xdead")
	if VerifyShieldedTransfer(circuitProof, wrongNullifier, circuitProof.OutputCommitment, root) {
		t.Error("wrong nullifier should not verify")
	}
}

func TestShieldedCircuitVerifyTamperedRangeProof(t *testing.T) {
	ct := NewCommitmentTree()
	var sk, randomness, recipientPK [32]byte
	sk[0] = 0x44
	randomness[0] = 0x55
	recipientPK[0] = 0x66

	commitment := PedersenCommitBN254(100, randomness)
	idx, root, _ := ct.Append(commitment)
	proof, _ := ct.MerkleProof(idx)

	siblings := make([][32]byte, CommitTreeDepth)
	for i := 0; i < CommitTreeDepth; i++ {
		siblings[i] = proof.Siblings[i]
	}

	witness := &ShieldedTransferWitness{
		SecretKey:       sk,
		Amount:          100,
		Randomness:      randomness,
		CommitmentIndex: idx,
		MerklePath:      siblings,
		MerkleRoot:      root,
		RecipientPK:     recipientPK,
	}

	circuitProof, _ := ProveShieldedTransfer(witness)
	// Tamper with range proof.
	if len(circuitProof.RangeProof) > 20 {
		circuitProof.RangeProof[20] ^= 0xff
	}

	if VerifyShieldedTransfer(circuitProof, circuitProof.Nullifier, circuitProof.OutputCommitment, root) {
		t.Error("tampered range proof should not verify")
	}
}

func TestShieldedCircuitCreateNote(t *testing.T) {
	var sk, randomness, recipientPK [32]byte
	sk[0] = 0x77
	randomness[0] = 0x88
	recipientPK[0] = 0x99

	witness := &ShieldedTransferWitness{
		SecretKey:       sk,
		Amount:          1000,
		Randomness:      randomness,
		CommitmentIndex: 42,
		RecipientPK:     recipientPK,
	}

	note := CreateShieldedNote(witness)
	if note == nil {
		t.Fatal("expected non-nil note")
	}
	if note.Commitment == (types.Hash{}) {
		t.Error("commitment should not be zero")
	}
	if note.Nullifier == (types.Hash{}) {
		t.Error("nullifier should not be zero")
	}
	if note.CommitmentIndex != 42 {
		t.Errorf("index: got %d, want 42", note.CommitmentIndex)
	}
	if len(note.EncryptedAmount) == 0 {
		t.Error("encrypted amount should not be empty")
	}
	if len(note.EncryptedRandom) == 0 {
		t.Error("encrypted randomness should not be empty")
	}
}

func TestShieldedCircuitCreateNoteNilWitness(t *testing.T) {
	note := CreateShieldedNote(nil)
	if note != nil {
		t.Error("expected nil note for nil witness")
	}
}

func TestShieldedCircuitRangeProofVerify(t *testing.T) {
	var randomness [32]byte
	randomness[0] = 0xdd

	rp := generateRangeProof(12345, randomness)
	if !verifyRangeProof(rp) {
		t.Error("valid range proof should verify")
	}
}

func TestShieldedCircuitRangeProofEmpty(t *testing.T) {
	if verifyRangeProof(nil) {
		t.Error("empty range proof should not verify")
	}
	if verifyRangeProof([]byte{0x01}) {
		t.Error("too-short range proof should not verify")
	}
}

func TestShieldedCircuitRangeProofZeroAmount(t *testing.T) {
	var randomness [32]byte
	rp := generateRangeProof(0, randomness)
	if !verifyRangeProof(rp) {
		t.Error("zero amount range proof should verify")
	}
}

func TestShieldedCircuitRangeProofMaxAmount(t *testing.T) {
	var randomness [32]byte
	randomness[0] = 0xee
	rp := generateRangeProof(^uint64(0), randomness)
	if !verifyRangeProof(rp) {
		t.Error("max amount range proof should verify")
	}
}

func TestShieldedCircuitMerkleInclusionVerify(t *testing.T) {
	ct := NewCommitmentTree()
	var randomness [32]byte
	randomness[0] = 0xab

	commitment := PedersenCommitBN254(999, randomness)
	idx, root, _ := ct.Append(commitment)
	proof, _ := ct.MerkleProof(idx)

	siblings := make([][32]byte, CommitTreeDepth)
	for i := 0; i < CommitTreeDepth; i++ {
		siblings[i] = proof.Siblings[i]
	}

	merkleProofData := generateMerkleInclusionProof(commitment, idx, siblings)
	if !verifyMerkleInclusionProof(merkleProofData, commitment, root) {
		t.Error("valid merkle inclusion proof should verify")
	}
}

func TestShieldedCircuitMerkleInclusionEmpty(t *testing.T) {
	if verifyMerkleInclusionProof(nil, types.Hash{}, types.Hash{}) {
		t.Error("empty proof should not verify")
	}
}

func TestShieldedCircuitNewCircuitInstance(t *testing.T) {
	c := NewShieldedTransferCircuit()
	if c == nil {
		t.Error("expected non-nil circuit")
	}
}

func TestShieldedCircuitIntegrationWithNullifierSet(t *testing.T) {
	var sk [32]byte
	sk[0] = 0xfe

	nullifier := DeriveNullifier(sk, 0)

	smt := NewSparseMerkleTree()
	smt.Insert(nullifier)

	if !smt.Contains(nullifier) {
		t.Error("nullifier should be in SMT after insertion")
	}
}

func TestShieldedCircuitIntegrationWithCommitmentTree(t *testing.T) {
	var randomness [32]byte
	randomness[0] = 0xcd

	commitment := PedersenCommitBN254(500, randomness)

	ct := NewCommitmentTree()
	idx, root, err := ct.Append(commitment)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	if !VerifyCommitmentProof(commitment, func() *CommitmentTreeProof {
		p, _ := ct.MerkleProof(idx)
		return p
	}(), root) {
		t.Error("commitment tree proof should verify")
	}
}
