package crypto

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestZKTransfer_PedersenCommitDeterministic(t *testing.T) {
	var rand [32]byte
	rand[0] = 0xAA
	c1 := ZKPedersenCommit(100, rand)
	c2 := ZKPedersenCommit(100, rand)
	if c1 != c2 {
		t.Fatal("same inputs must produce same commitment")
	}
}

func TestZKTransfer_PedersenCommitDifferentAmounts(t *testing.T) {
	var rand [32]byte
	rand[0] = 0xBB
	c1 := ZKPedersenCommit(100, rand)
	c2 := ZKPedersenCommit(200, rand)
	if c1 == c2 {
		t.Fatal("different amounts must produce different commitments")
	}
}

func TestZKTransfer_PedersenCommitDifferentRandomness(t *testing.T) {
	var r1, r2 [32]byte
	r1[0] = 0x01
	r2[0] = 0x02
	c1 := ZKPedersenCommit(100, r1)
	c2 := ZKPedersenCommit(100, r2)
	if c1 == c2 {
		t.Fatal("different randomness must produce different commitments")
	}
}

func TestZKTransfer_NullifierDerivation(t *testing.T) {
	var sk [32]byte
	sk[0] = 0xFF
	n1 := ZKNullifier(sk, 0)
	n2 := ZKNullifier(sk, 1)
	if n1 == n2 {
		t.Fatal("different indices must produce different nullifiers")
	}

	// Same sk and index => same nullifier.
	n3 := ZKNullifier(sk, 0)
	if n1 != n3 {
		t.Fatal("same inputs must produce same nullifier")
	}
}

func TestZKTransfer_NullifierDifferentKeys(t *testing.T) {
	var sk1, sk2 [32]byte
	sk1[0] = 0x01
	sk2[0] = 0x02
	n1 := ZKNullifier(sk1, 0)
	n2 := ZKNullifier(sk2, 0)
	if n1 == n2 {
		t.Fatal("different secret keys must produce different nullifiers")
	}
}

func TestZKTransfer_ProveVerifyRoundtrip(t *testing.T) {
	witness := &ZKTransferWitness{
		Amount:        1000,
		CommitmentIdx: 5,
	}
	witness.SenderSK[0] = 0xAA
	witness.RecipientPK[0] = 0xBB
	witness.Randomness[0] = 0xCC

	proof, err := ProveTransfer(witness)
	if err != nil {
		t.Fatalf("ProveTransfer failed: %v", err)
	}
	if proof == nil {
		t.Fatal("proof is nil")
	}
	if len(proof.Nullifiers) != 1 {
		t.Fatalf("expected 1 nullifier, got %d", len(proof.Nullifiers))
	}
	if len(proof.OutputCommitments) != 1 {
		t.Fatalf("expected 1 output commitment, got %d", len(proof.OutputCommitments))
	}

	// Verify the proof.
	valid := VerifyTransferProof(
		proof,
		proof.Nullifiers[0],
		proof.OutputCommitments[0],
		proof.MerkleRoot,
	)
	if !valid {
		t.Fatal("valid proof failed verification")
	}
}

func TestZKTransfer_ProveVerifyWithMerklePath(t *testing.T) {
	witness := &ZKTransferWitness{
		Amount:        500,
		CommitmentIdx: 3,
		MerkleProofVal: []types.Hash{
			types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
			types.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
		},
	}
	witness.SenderSK[0] = 0x11
	witness.RecipientPK[0] = 0x22
	witness.Randomness[0] = 0x33

	proof, err := ProveTransfer(witness)
	if err != nil {
		t.Fatalf("ProveTransfer failed: %v", err)
	}

	valid := VerifyTransferProof(
		proof,
		proof.Nullifiers[0],
		proof.OutputCommitments[0],
		proof.MerkleRoot,
	)
	if !valid {
		t.Fatal("proof with merkle path failed verification")
	}

	// Non-empty Merkle root when path is provided.
	if proof.MerkleRoot.IsZero() {
		t.Fatal("merkle root should be non-zero when path is provided")
	}
}

func TestZKTransfer_VerifyRejectsNilProof(t *testing.T) {
	if VerifyTransferProof(nil, types.Hash{}, types.Hash{}, types.Hash{}) {
		t.Fatal("nil proof should be rejected")
	}
}

func TestZKTransfer_VerifyRejectsWrongNullifier(t *testing.T) {
	witness := &ZKTransferWitness{
		Amount:        100,
		CommitmentIdx: 0,
	}
	witness.SenderSK[0] = 0x01
	witness.Randomness[0] = 0x02

	proof, err := ProveTransfer(witness)
	if err != nil {
		t.Fatalf("ProveTransfer failed: %v", err)
	}

	wrongNullifier := types.HexToHash("0xdead")
	valid := VerifyTransferProof(
		proof,
		wrongNullifier,
		proof.OutputCommitments[0],
		proof.MerkleRoot,
	)
	if valid {
		t.Fatal("wrong nullifier should fail verification")
	}
}

func TestZKTransfer_VerifyRejectsWrongCommitment(t *testing.T) {
	witness := &ZKTransferWitness{
		Amount:        100,
		CommitmentIdx: 0,
	}
	witness.SenderSK[0] = 0x01
	witness.Randomness[0] = 0x02

	proof, err := ProveTransfer(witness)
	if err != nil {
		t.Fatalf("ProveTransfer failed: %v", err)
	}

	wrongCommit := types.HexToHash("0xbeef")
	valid := VerifyTransferProof(
		proof,
		proof.Nullifiers[0],
		wrongCommit,
		proof.MerkleRoot,
	)
	if valid {
		t.Fatal("wrong commitment should fail verification")
	}
}

func TestZKTransfer_VerifyRejectsWrongRoot(t *testing.T) {
	witness := &ZKTransferWitness{
		Amount:        100,
		CommitmentIdx: 0,
	}
	witness.SenderSK[0] = 0x01
	witness.Randomness[0] = 0x02

	proof, err := ProveTransfer(witness)
	if err != nil {
		t.Fatalf("ProveTransfer failed: %v", err)
	}

	wrongRoot := types.HexToHash("0xcafe")
	valid := VerifyTransferProof(
		proof,
		proof.Nullifiers[0],
		proof.OutputCommitments[0],
		wrongRoot,
	)
	if valid {
		t.Fatal("wrong root should fail verification")
	}
}

func TestZKTransfer_ProveNilWitness(t *testing.T) {
	_, err := ProveTransfer(nil)
	if err != ErrZKNilProof {
		t.Fatalf("expected ErrZKNilProof, got %v", err)
	}
}

func TestZKTransfer_RangeProofValid(t *testing.T) {
	var rand [32]byte
	rand[0] = 0xDD
	proof := zkRangeProof(12345, rand)
	if !zkVerifyRangeProof(proof) {
		t.Fatal("valid range proof failed verification")
	}
}

func TestZKTransfer_RangeProofZeroAmount(t *testing.T) {
	var rand [32]byte
	rand[0] = 0xEE
	proof := zkRangeProof(0, rand)
	if !zkVerifyRangeProof(proof) {
		t.Fatal("range proof for zero amount failed verification")
	}
}

func TestZKTransfer_RangeProofMaxAmount(t *testing.T) {
	var rand [32]byte
	rand[0] = 0xFF
	proof := zkRangeProof(^uint64(0), rand)
	if !zkVerifyRangeProof(proof) {
		t.Fatal("range proof for max uint64 failed verification")
	}
}

func TestZKTransfer_RangeProofRejectsEmpty(t *testing.T) {
	if zkVerifyRangeProof(nil) {
		t.Fatal("nil range proof should be rejected")
	}
	if zkVerifyRangeProof([]byte{}) {
		t.Fatal("empty range proof should be rejected")
	}
}

func TestZKTransfer_RangeProofRejectsTruncated(t *testing.T) {
	var rand [32]byte
	proof := zkRangeProof(100, rand)
	truncated := proof[:len(proof)/2]
	if zkVerifyRangeProof(truncated) {
		t.Fatal("truncated range proof should be rejected")
	}
}

func TestZKTransfer_CommitmentNonZero(t *testing.T) {
	var rand [32]byte
	c := ZKPedersenCommit(0, rand)
	if c.IsZero() {
		t.Fatal("commitment should not be zero hash")
	}
}

func TestZKTransfer_DifferentWitnessesProduceDifferentProofs(t *testing.T) {
	w1 := &ZKTransferWitness{Amount: 100, CommitmentIdx: 0}
	w1.SenderSK[0] = 0x01
	w1.Randomness[0] = 0x02

	w2 := &ZKTransferWitness{Amount: 200, CommitmentIdx: 1}
	w2.SenderSK[0] = 0x03
	w2.Randomness[0] = 0x04

	p1, _ := ProveTransfer(w1)
	p2, _ := ProveTransfer(w2)

	if p1.Nullifiers[0] == p2.Nullifiers[0] {
		t.Fatal("different witnesses should produce different nullifiers")
	}
	if p1.OutputCommitments[0] == p2.OutputCommitments[0] {
		t.Fatal("different witnesses should produce different commitments")
	}
}
