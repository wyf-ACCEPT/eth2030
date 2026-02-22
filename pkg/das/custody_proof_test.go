package das

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestGenerateCustodyProof(t *testing.T) {
	nodeID := [32]byte{0x01, 0x02, 0x03}
	epoch := uint64(42)
	columns := []uint64{0, 5, 10}
	data := []byte("test custody data")

	proof := GenerateCustodyProof(nodeID, epoch, columns, data)

	if proof == nil {
		t.Fatal("proof is nil")
	}
	if proof.NodeID != nodeID {
		t.Error("NodeID mismatch")
	}
	if proof.Epoch != epoch {
		t.Errorf("Epoch = %d, want %d", proof.Epoch, epoch)
	}
	if len(proof.ColumnIndices) != len(columns) {
		t.Errorf("ColumnIndices length = %d, want %d", len(proof.ColumnIndices), len(columns))
	}
	if len(proof.Proof) != 32 {
		t.Errorf("Proof length = %d, want 32", len(proof.Proof))
	}
}

func TestVerifyCustodyProof(t *testing.T) {
	nodeID := [32]byte{0x01}
	columns := []uint64{0, 5, 10}
	data := []byte("test data")

	proof := GenerateCustodyProof(nodeID, 1, columns, data)
	if !VerifyCustodyProof(proof) {
		t.Error("valid proof rejected")
	}
}

func TestVerifyCustodyProofNil(t *testing.T) {
	if VerifyCustodyProof(nil) {
		t.Error("nil proof should not verify")
	}
}

func TestVerifyCustodyProofBadLength(t *testing.T) {
	proof := &CustodyProof{
		ColumnIndices: []uint64{0},
		Proof:         []byte{0x01}, // wrong length
	}
	if VerifyCustodyProof(proof) {
		t.Error("proof with wrong length should not verify")
	}
}

func TestVerifyCustodyProofEmptyColumns(t *testing.T) {
	proof := &CustodyProof{
		ColumnIndices: []uint64{},
		Proof:         make([]byte, 32),
	}
	if VerifyCustodyProof(proof) {
		t.Error("proof with empty columns should not verify")
	}
}

func TestVerifyCustodyProofColumnOutOfRange(t *testing.T) {
	proof := &CustodyProof{
		ColumnIndices: []uint64{0, NumberOfColumns}, // NumberOfColumns is out of range
		Proof:         make([]byte, 32),
	}
	if VerifyCustodyProof(proof) {
		t.Error("proof with out-of-range column should not verify")
	}
}

func TestVerifyCustodyProofDuplicateColumn(t *testing.T) {
	proof := &CustodyProof{
		ColumnIndices: []uint64{5, 5},
		Proof:         make([]byte, 32),
	}
	if VerifyCustodyProof(proof) {
		t.Error("proof with duplicate columns should not verify")
	}
}

func TestVerifyCustodyProofWithData(t *testing.T) {
	nodeID := [32]byte{0x42}
	columns := []uint64{3, 7, 11}
	data := []byte("the actual column data")

	proof := GenerateCustodyProof(nodeID, 99, columns, data)

	// Correct data should verify.
	if !VerifyCustodyProofWithData(proof, data) {
		t.Error("proof with correct data should verify")
	}

	// Wrong data should not verify.
	if VerifyCustodyProofWithData(proof, []byte("wrong data")) {
		t.Error("proof with wrong data should not verify")
	}
}

func TestCustodyProofDeterministic(t *testing.T) {
	nodeID := [32]byte{0xAA}
	columns := []uint64{1, 2, 3}
	data := []byte("deterministic test")

	proof1 := GenerateCustodyProof(nodeID, 10, columns, data)
	proof2 := GenerateCustodyProof(nodeID, 10, columns, data)

	if len(proof1.Proof) != len(proof2.Proof) {
		t.Fatal("proof lengths differ")
	}
	for i := range proof1.Proof {
		if proof1.Proof[i] != proof2.Proof[i] {
			t.Fatal("proofs differ for same inputs")
		}
	}
}

func TestCreateChallenge(t *testing.T) {
	challenger := types.Address{0x01}
	target := types.Address{0x02}

	challenge, err := CreateChallenge(challenger, target, 10, 5, 100)
	if err != nil {
		t.Fatalf("CreateChallenge: %v", err)
	}

	if challenge.Challenger != challenger {
		t.Error("Challenger mismatch")
	}
	if challenge.Target != target {
		t.Error("Target mismatch")
	}
	if challenge.Column != 10 {
		t.Errorf("Column = %d, want 10", challenge.Column)
	}
	if challenge.Epoch != 5 {
		t.Errorf("Epoch = %d, want 5", challenge.Epoch)
	}
	if challenge.Deadline != 100 {
		t.Errorf("Deadline = %d, want 100", challenge.Deadline)
	}
}

func TestCreateChallengeInvalidColumn(t *testing.T) {
	_, err := CreateChallenge(types.Address{}, types.Address{}, NumberOfColumns, 0, 100)
	if err == nil {
		t.Fatal("expected error for invalid column")
	}
}

func TestRespondToChallenge(t *testing.T) {
	challenger := types.Address{0x01}
	target := types.Address{0x02}

	challenge, _ := CreateChallenge(challenger, target, 10, 5, 100)

	// Valid response: proof covers the challenged column and epoch.
	proof := GenerateCustodyProof([32]byte{0x02}, 5, []uint64{10, 20, 30}, []byte("data"))
	if !RespondToChallenge(challenge, proof) {
		t.Error("valid response rejected")
	}
}

func TestRespondToChallengeWrongEpoch(t *testing.T) {
	challenge, _ := CreateChallenge(types.Address{0x01}, types.Address{0x02}, 10, 5, 100)
	proof := GenerateCustodyProof([32]byte{0x02}, 6, []uint64{10}, []byte("data")) // wrong epoch

	if RespondToChallenge(challenge, proof) {
		t.Error("response with wrong epoch should be rejected")
	}
}

func TestRespondToChallengeWrongColumn(t *testing.T) {
	challenge, _ := CreateChallenge(types.Address{0x01}, types.Address{0x02}, 10, 5, 100)
	proof := GenerateCustodyProof([32]byte{0x02}, 5, []uint64{11, 20}, []byte("data")) // missing column 10

	if RespondToChallenge(challenge, proof) {
		t.Error("response missing challenged column should be rejected")
	}
}

func TestRespondToChallengeNil(t *testing.T) {
	challenge, _ := CreateChallenge(types.Address{}, types.Address{}, 10, 5, 100)

	if RespondToChallenge(challenge, nil) {
		t.Error("nil proof should be rejected")
	}
	if RespondToChallenge(nil, &CustodyProof{}) {
		t.Error("nil challenge should be rejected")
	}
}

func TestVerifyCustodyProofWithEpoch(t *testing.T) {
	nodeID := [32]byte{0x01}
	columns := []uint64{0, 5, 10}
	data := []byte("test data")
	proof := GenerateCustodyProof(nodeID, 50, columns, data)

	// Current epoch 100, cutoff 256: epoch 50 should pass.
	if err := VerifyCustodyProofWithEpoch(proof, 100, 256); err != nil {
		t.Fatalf("valid epoch rejected: %v", err)
	}

	// Current epoch 500, cutoff 256: epoch 50 should fail (50 < 500-256=244).
	if err := VerifyCustodyProofWithEpoch(proof, 500, 256); err == nil {
		t.Fatal("old epoch should be rejected")
	}

	// Cutoff 0 means no epoch check.
	if err := VerifyCustodyProofWithEpoch(proof, 500, 0); err != nil {
		t.Fatalf("cutoff 0 should bypass epoch check: %v", err)
	}

	// Invalid proof should fail regardless.
	badProof := &CustodyProof{Proof: []byte{0x01}}
	if err := VerifyCustodyProofWithEpoch(badProof, 100, 256); err == nil {
		t.Fatal("invalid proof should fail")
	}
}

func TestValidateChallengeDeadline(t *testing.T) {
	challenge, _ := CreateChallenge(types.Address{0x01}, types.Address{0x02}, 10, 5, 100)

	// Before deadline.
	if err := ValidateChallengeDeadline(challenge, 50); err != nil {
		t.Fatalf("before deadline rejected: %v", err)
	}

	// At deadline (should fail: currentSlot >= deadline).
	if err := ValidateChallengeDeadline(challenge, 100); err == nil {
		t.Fatal("at deadline should fail")
	}

	// After deadline.
	if err := ValidateChallengeDeadline(challenge, 200); err == nil {
		t.Fatal("after deadline should fail")
	}

	// Nil challenge.
	if err := ValidateChallengeDeadline(nil, 50); err == nil {
		t.Fatal("nil challenge should fail")
	}
}

func TestCustodyProofTracker(t *testing.T) {
	tracker := NewCustodyProofTracker()
	nodeID := [32]byte{0x01}
	columns := []uint64{5, 10}
	data := []byte("custody data")
	proof := GenerateCustodyProof(nodeID, 42, columns, data)

	// First submission should succeed.
	if err := tracker.RecordProof(proof); err != nil {
		t.Fatalf("first submission failed: %v", err)
	}

	// Check seen.
	if !tracker.HasSeen(nodeID, 42, 5) {
		t.Fatal("should have seen (nodeID, 42, 5)")
	}
	if !tracker.HasSeen(nodeID, 42, 10) {
		t.Fatal("should have seen (nodeID, 42, 10)")
	}
	if tracker.HasSeen(nodeID, 42, 15) {
		t.Fatal("should not have seen (nodeID, 42, 15)")
	}

	// Replay should fail.
	if err := tracker.RecordProof(proof); err == nil {
		t.Fatal("replay should fail")
	}

	// Different epoch should succeed.
	proof2 := GenerateCustodyProof(nodeID, 43, columns, data)
	if err := tracker.RecordProof(proof2); err != nil {
		t.Fatalf("different epoch should succeed: %v", err)
	}

	// Nil proof should fail.
	if err := tracker.RecordProof(nil); err == nil {
		t.Fatal("nil proof should fail")
	}
}

func TestCustodyProofTrackerPrune(t *testing.T) {
	tracker := NewCustodyProofTracker()
	nodeID := [32]byte{0x01}

	// Record proofs for multiple epochs.
	for epoch := uint64(1); epoch <= 10; epoch++ {
		proof := GenerateCustodyProof(nodeID, epoch, []uint64{0}, []byte("data"))
		tracker.RecordProof(proof)
	}

	// Prune epochs < 5.
	pruned := tracker.PruneEpoch(5)
	if pruned != 4 {
		t.Errorf("pruned = %d, want 4", pruned)
	}

	// Epoch 4 should no longer be seen.
	if tracker.HasSeen(nodeID, 4, 0) {
		t.Fatal("epoch 4 should have been pruned")
	}

	// Epoch 5 should still be seen.
	if !tracker.HasSeen(nodeID, 5, 0) {
		t.Fatal("epoch 5 should still be present")
	}
}

func TestDeadlineEdgeCases(t *testing.T) {
	// Deadline at slot 0: any current slot >= 0 fails.
	challenge, _ := CreateChallenge(types.Address{0x01}, types.Address{0x02}, 10, 5, 0)
	if err := ValidateChallengeDeadline(challenge, 0); err == nil {
		t.Fatal("deadline 0, current 0 should fail")
	}

	// Deadline at slot 1: current slot 0 should pass.
	challenge2, _ := CreateChallenge(types.Address{0x01}, types.Address{0x02}, 10, 5, 1)
	if err := ValidateChallengeDeadline(challenge2, 0); err != nil {
		t.Fatalf("deadline 1, current 0 should pass: %v", err)
	}
}

func TestChallengeIDDeterministic(t *testing.T) {
	challenger := types.Address{0x01}
	target := types.Address{0x02}

	c1, _ := CreateChallenge(challenger, target, 10, 5, 100)
	c2, _ := CreateChallenge(challenger, target, 10, 5, 100)

	if c1.ID != c2.ID {
		t.Error("challenge ID not deterministic")
	}

	// Different column should produce different ID.
	c3, _ := CreateChallenge(challenger, target, 11, 5, 100)
	if c1.ID == c3.ID {
		t.Error("different column should produce different challenge ID")
	}
}

func TestValidateCustodyChallenge(t *testing.T) {
	challenger := types.Address{0x01}
	target := types.Address{0x02}

	c, _ := CreateChallenge(challenger, target, 5, 100, 200)

	// Valid.
	if err := ValidateCustodyChallenge(c, 150, 100); err != nil {
		t.Errorf("valid challenge: %v", err)
	}

	// Nil.
	if err := ValidateCustodyChallenge(nil, 150, 100); err == nil {
		t.Error("expected error for nil challenge")
	}

	// Deadline passed.
	if err := ValidateCustodyChallenge(c, 250, 100); err == nil {
		t.Error("expected error for deadline passed")
	}

	// Column out of range.
	badCol, _ := CreateChallenge(challenger, target, NumberOfColumns+1, 100, 200)
	if badCol != nil {
		if err := ValidateCustodyChallenge(badCol, 150, 100); err == nil {
			t.Error("expected error for column out of range")
		}
	}
}
