package types

import (
	"testing"
)

// makeTestReceipt creates a receipt with the given parameters for testing.
func makeTestReceiptForProof(status uint64, gasUsed uint64, cumGas uint64) *Receipt {
	return &Receipt{
		Type:              0,
		Status:            status,
		GasUsed:           gasUsed,
		CumulativeGasUsed: cumGas,
		Logs:              []*Log{},
	}
}

// makeTestReceiptWithLogs creates a receipt with logs for testing.
func makeTestReceiptWithLogs(status uint64, gasUsed uint64, cumGas uint64, numLogs int) *Receipt {
	logs := make([]*Log, numLogs)
	for i := 0; i < numLogs; i++ {
		logs[i] = &Log{
			Address: Address{byte(i + 1)},
			Topics:  []Hash{{byte(i)}},
			Data:    []byte{byte(i), byte(i + 1)},
		}
	}
	return &Receipt{
		Type:              0,
		Status:            status,
		GasUsed:           gasUsed,
		CumulativeGasUsed: cumGas,
		Logs:              logs,
	}
}

func TestReceiptProofGenerator_NewGenerator(t *testing.T) {
	g := NewReceiptProofGenerator()
	if g == nil {
		t.Fatal("NewReceiptProofGenerator returned nil")
	}
}

func TestReceiptProofGenerator_GenerateAndVerify(t *testing.T) {
	g := NewReceiptProofGenerator()

	receipts := []*Receipt{
		makeTestReceiptForProof(ReceiptStatusSuccessful, 21000, 21000),
		makeTestReceiptForProof(ReceiptStatusSuccessful, 42000, 63000),
		makeTestReceiptForProof(ReceiptStatusFailed, 30000, 93000),
		makeTestReceiptForProof(ReceiptStatusSuccessful, 50000, 143000),
	}

	for i := range receipts {
		proof, err := g.GenerateProof(receipts, i)
		if err != nil {
			t.Fatalf("GenerateProof(%d) failed: %v", i, err)
		}
		if proof == nil {
			t.Fatalf("GenerateProof(%d) returned nil proof", i)
		}
		if proof.Index != i {
			t.Fatalf("expected index %d, got %d", i, proof.Index)
		}

		valid, err := g.VerifyProof(proof)
		if err != nil {
			t.Fatalf("VerifyProof(%d) failed: %v", i, err)
		}
		if !valid {
			t.Fatalf("VerifyProof(%d) returned false", i)
		}
	}
}

func TestReceiptProofGenerator_SingleReceipt(t *testing.T) {
	g := NewReceiptProofGenerator()

	receipts := []*Receipt{
		makeTestReceiptForProof(ReceiptStatusSuccessful, 21000, 21000),
	}

	proof, err := g.GenerateProof(receipts, 0)
	if err != nil {
		t.Fatalf("GenerateProof failed: %v", err)
	}

	valid, err := g.VerifyProof(proof)
	if err != nil {
		t.Fatalf("VerifyProof failed: %v", err)
	}
	if !valid {
		t.Fatal("VerifyProof returned false for single receipt")
	}
}

func TestReceiptProofGenerator_EmptyReceipts(t *testing.T) {
	g := NewReceiptProofGenerator()

	_, err := g.GenerateProof(nil, 0)
	if err != ErrReceiptProofEmpty {
		t.Fatalf("expected ErrReceiptProofEmpty, got %v", err)
	}

	_, err = g.GenerateProof([]*Receipt{}, 0)
	if err != ErrReceiptProofEmpty {
		t.Fatalf("expected ErrReceiptProofEmpty, got %v", err)
	}
}

func TestReceiptProofGenerator_IndexOutOfBounds(t *testing.T) {
	g := NewReceiptProofGenerator()

	receipts := []*Receipt{
		makeTestReceiptForProof(ReceiptStatusSuccessful, 21000, 21000),
	}

	_, err := g.GenerateProof(receipts, 1)
	if err != ErrReceiptProofIndexOOB {
		t.Fatalf("expected ErrReceiptProofIndexOOB, got %v", err)
	}

	_, err = g.GenerateProof(receipts, -1)
	if err != ErrReceiptProofIndexOOB {
		t.Fatalf("expected ErrReceiptProofIndexOOB for negative index, got %v", err)
	}
}

func TestReceiptProofGenerator_VerifyNilProof(t *testing.T) {
	g := NewReceiptProofGenerator()

	valid, err := g.VerifyProof(nil)
	if valid {
		t.Fatal("expected invalid for nil proof")
	}
	if err != ErrReceiptProofInvalid {
		t.Fatalf("expected ErrReceiptProofInvalid, got %v", err)
	}
}

func TestReceiptProofGenerator_VerifyEmptyData(t *testing.T) {
	g := NewReceiptProofGenerator()

	proof := &ReceiptProof{
		Index:       0,
		ReceiptData: nil,
		ProofPath:   nil,
		RootHash:    Hash{},
	}
	valid, err := g.VerifyProof(proof)
	if valid {
		t.Fatal("expected invalid for empty receipt data")
	}
	if err != ErrReceiptProofInvalid {
		t.Fatalf("expected ErrReceiptProofInvalid, got %v", err)
	}
}

func TestReceiptProofGenerator_TamperedData(t *testing.T) {
	g := NewReceiptProofGenerator()

	receipts := []*Receipt{
		makeTestReceiptForProof(ReceiptStatusSuccessful, 21000, 21000),
		makeTestReceiptForProof(ReceiptStatusSuccessful, 42000, 63000),
		makeTestReceiptForProof(ReceiptStatusFailed, 30000, 93000),
	}

	proof, err := g.GenerateProof(receipts, 1)
	if err != nil {
		t.Fatalf("GenerateProof failed: %v", err)
	}

	// Tamper with receipt data.
	proof.ReceiptData[0] ^= 0xFF
	valid, err := g.VerifyProof(proof)
	if valid {
		t.Fatal("expected invalid for tampered receipt data")
	}
	if err != ErrReceiptProofInvalid {
		t.Fatalf("expected ErrReceiptProofInvalid, got %v", err)
	}
}

func TestReceiptProofGenerator_TamperedRoot(t *testing.T) {
	g := NewReceiptProofGenerator()

	receipts := []*Receipt{
		makeTestReceiptForProof(ReceiptStatusSuccessful, 21000, 21000),
		makeTestReceiptForProof(ReceiptStatusSuccessful, 42000, 63000),
	}

	proof, err := g.GenerateProof(receipts, 0)
	if err != nil {
		t.Fatalf("GenerateProof failed: %v", err)
	}

	// Tamper with root hash.
	proof.RootHash[0] ^= 0xFF
	valid, err := g.VerifyProof(proof)
	if valid {
		t.Fatal("expected invalid for tampered root")
	}
	if err != ErrReceiptProofInvalid {
		t.Fatalf("expected ErrReceiptProofInvalid, got %v", err)
	}
}

func TestReceiptProofGenerator_ComputeReceiptsRoot(t *testing.T) {
	g := NewReceiptProofGenerator()

	// Empty returns EmptyRootHash.
	root := g.ComputeReceiptsRoot(nil)
	if root != EmptyRootHash {
		t.Fatalf("expected EmptyRootHash for nil receipts, got %v", root)
	}

	root = g.ComputeReceiptsRoot([]*Receipt{})
	if root != EmptyRootHash {
		t.Fatalf("expected EmptyRootHash for empty receipts, got %v", root)
	}

	// Non-empty should produce a non-zero hash.
	receipts := []*Receipt{
		makeTestReceiptForProof(ReceiptStatusSuccessful, 21000, 21000),
	}
	root = g.ComputeReceiptsRoot(receipts)
	if root.IsZero() {
		t.Fatal("expected non-zero root for non-empty receipts")
	}
}

func TestReceiptProofGenerator_RootConsistency(t *testing.T) {
	g := NewReceiptProofGenerator()

	receipts := []*Receipt{
		makeTestReceiptForProof(ReceiptStatusSuccessful, 21000, 21000),
		makeTestReceiptForProof(ReceiptStatusSuccessful, 42000, 63000),
		makeTestReceiptForProof(ReceiptStatusFailed, 30000, 93000),
	}

	root := g.ComputeReceiptsRoot(receipts)

	// All proofs should have the same root.
	for i := range receipts {
		proof, err := g.GenerateProof(receipts, i)
		if err != nil {
			t.Fatalf("GenerateProof(%d) failed: %v", i, err)
		}
		if proof.RootHash != root {
			t.Fatalf("proof %d root %v != computed root %v", i, proof.RootHash, root)
		}
	}
}

func TestReceiptProofGenerator_BatchGenerateProofs(t *testing.T) {
	g := NewReceiptProofGenerator()

	receipts := []*Receipt{
		makeTestReceiptForProof(ReceiptStatusSuccessful, 21000, 21000),
		makeTestReceiptForProof(ReceiptStatusSuccessful, 42000, 63000),
		makeTestReceiptForProof(ReceiptStatusFailed, 30000, 93000),
		makeTestReceiptForProof(ReceiptStatusSuccessful, 50000, 143000),
	}

	proofs, err := g.BatchGenerateProofs(receipts)
	if err != nil {
		t.Fatalf("BatchGenerateProofs failed: %v", err)
	}
	if len(proofs) != len(receipts) {
		t.Fatalf("expected %d proofs, got %d", len(receipts), len(proofs))
	}

	for i, proof := range proofs {
		valid, err := g.VerifyProof(proof)
		if err != nil {
			t.Fatalf("VerifyProof(%d) failed: %v", i, err)
		}
		if !valid {
			t.Fatalf("VerifyProof(%d) returned false", i)
		}
	}
}

func TestReceiptProofGenerator_BatchEmpty(t *testing.T) {
	g := NewReceiptProofGenerator()

	_, err := g.BatchGenerateProofs(nil)
	if err != ErrReceiptProofEmpty {
		t.Fatalf("expected ErrReceiptProofEmpty, got %v", err)
	}

	_, err = g.BatchGenerateProofs([]*Receipt{})
	if err != ErrReceiptProofEmpty {
		t.Fatalf("expected ErrReceiptProofEmpty, got %v", err)
	}
}

func TestReceiptProofGenerator_WithLogs(t *testing.T) {
	g := NewReceiptProofGenerator()

	receipts := []*Receipt{
		makeTestReceiptWithLogs(ReceiptStatusSuccessful, 21000, 21000, 3),
		makeTestReceiptWithLogs(ReceiptStatusSuccessful, 42000, 63000, 1),
		makeTestReceiptWithLogs(ReceiptStatusFailed, 30000, 93000, 0),
	}

	for i := range receipts {
		proof, err := g.GenerateProof(receipts, i)
		if err != nil {
			t.Fatalf("GenerateProof(%d) with logs failed: %v", i, err)
		}
		valid, err := g.VerifyProof(proof)
		if err != nil {
			t.Fatalf("VerifyProof(%d) with logs failed: %v", i, err)
		}
		if !valid {
			t.Fatalf("VerifyProof(%d) with logs returned false", i)
		}
	}
}

func TestReceiptProofGenerator_OddNumberReceipts(t *testing.T) {
	g := NewReceiptProofGenerator()

	// 5 receipts = odd number, tests tree promotion logic.
	receipts := make([]*Receipt, 5)
	for i := range receipts {
		receipts[i] = makeTestReceiptForProof(ReceiptStatusSuccessful, uint64(21000*(i+1)), uint64(21000*(i+1)))
	}

	for i := range receipts {
		proof, err := g.GenerateProof(receipts, i)
		if err != nil {
			t.Fatalf("GenerateProof(%d) odd receipts failed: %v", i, err)
		}
		valid, err := g.VerifyProof(proof)
		if err != nil {
			t.Fatalf("VerifyProof(%d) odd receipts failed: %v", i, err)
		}
		if !valid {
			t.Fatalf("VerifyProof(%d) odd receipts returned false", i)
		}
	}
}

func TestReceiptProofGenerator_PowerOfTwoReceipts(t *testing.T) {
	g := NewReceiptProofGenerator()

	// 8 receipts = power of two, perfect binary tree.
	receipts := make([]*Receipt, 8)
	for i := range receipts {
		receipts[i] = makeTestReceiptForProof(ReceiptStatusSuccessful, uint64(21000*(i+1)), uint64(21000*(i+1)))
	}

	proofs, err := g.BatchGenerateProofs(receipts)
	if err != nil {
		t.Fatalf("BatchGenerateProofs failed: %v", err)
	}

	for i, proof := range proofs {
		valid, err := g.VerifyProof(proof)
		if err != nil {
			t.Fatalf("VerifyProof(%d) power-of-two failed: %v", i, err)
		}
		if !valid {
			t.Fatalf("VerifyProof(%d) power-of-two returned false", i)
		}
	}
}

func TestReceiptProofGenerator_DifferentReceiptsDifferentRoots(t *testing.T) {
	g := NewReceiptProofGenerator()

	receipts1 := []*Receipt{
		makeTestReceiptForProof(ReceiptStatusSuccessful, 21000, 21000),
	}
	receipts2 := []*Receipt{
		makeTestReceiptForProof(ReceiptStatusFailed, 42000, 42000),
	}

	root1 := g.ComputeReceiptsRoot(receipts1)
	root2 := g.ComputeReceiptsRoot(receipts2)

	if root1 == root2 {
		t.Fatal("different receipt sets should produce different roots")
	}
}

func TestReceiptProofGenerator_SameReceiptsSameRoot(t *testing.T) {
	g := NewReceiptProofGenerator()

	receipts := []*Receipt{
		makeTestReceiptForProof(ReceiptStatusSuccessful, 21000, 21000),
		makeTestReceiptForProof(ReceiptStatusSuccessful, 42000, 63000),
	}

	root1 := g.ComputeReceiptsRoot(receipts)
	root2 := g.ComputeReceiptsRoot(receipts)

	if root1 != root2 {
		t.Fatal("same receipt sets should produce the same root")
	}
}

func TestReceiptProofGenerator_TwoReceipts(t *testing.T) {
	g := NewReceiptProofGenerator()

	receipts := []*Receipt{
		makeTestReceiptForProof(ReceiptStatusSuccessful, 21000, 21000),
		makeTestReceiptForProof(ReceiptStatusFailed, 42000, 63000),
	}

	for i := range receipts {
		proof, err := g.GenerateProof(receipts, i)
		if err != nil {
			t.Fatalf("GenerateProof(%d) two receipts failed: %v", i, err)
		}
		valid, err := g.VerifyProof(proof)
		if err != nil {
			t.Fatalf("VerifyProof(%d) two receipts failed: %v", i, err)
		}
		if !valid {
			t.Fatalf("VerifyProof(%d) two receipts returned false", i)
		}
	}
}

func TestReceiptProofGenerator_TypedReceipts(t *testing.T) {
	g := NewReceiptProofGenerator()

	receipts := []*Receipt{
		{Type: 0, Status: ReceiptStatusSuccessful, CumulativeGasUsed: 21000, Logs: []*Log{}},
		{Type: DynamicFeeTxType, Status: ReceiptStatusSuccessful, CumulativeGasUsed: 63000, Logs: []*Log{}},
		{Type: BlobTxType, Status: ReceiptStatusSuccessful, CumulativeGasUsed: 93000, Logs: []*Log{}},
	}

	for i := range receipts {
		proof, err := g.GenerateProof(receipts, i)
		if err != nil {
			t.Fatalf("GenerateProof(%d) typed receipts failed: %v", i, err)
		}
		valid, err := g.VerifyProof(proof)
		if err != nil {
			t.Fatalf("VerifyProof(%d) typed receipts failed: %v", i, err)
		}
		if !valid {
			t.Fatalf("VerifyProof(%d) typed receipts returned false", i)
		}
	}
}

func TestReceiptProofGenerator_ThreeReceipts(t *testing.T) {
	g := NewReceiptProofGenerator()

	// 3 receipts tests the odd-count tree at the first level.
	receipts := []*Receipt{
		makeTestReceiptForProof(ReceiptStatusSuccessful, 21000, 21000),
		makeTestReceiptForProof(ReceiptStatusSuccessful, 42000, 63000),
		makeTestReceiptForProof(ReceiptStatusFailed, 30000, 93000),
	}

	for i := range receipts {
		proof, err := g.GenerateProof(receipts, i)
		if err != nil {
			t.Fatalf("GenerateProof(%d) three receipts failed: %v", i, err)
		}
		valid, err := g.VerifyProof(proof)
		if err != nil {
			t.Fatalf("VerifyProof(%d) three receipts failed: %v", i, err)
		}
		if !valid {
			t.Fatalf("VerifyProof(%d) three receipts returned false", i)
		}
	}
}

func TestEncodeReceiptForProof(t *testing.T) {
	r := makeTestReceiptForProof(ReceiptStatusSuccessful, 21000, 21000)
	data := encodeReceiptForProof(r)
	if len(data) == 0 {
		t.Fatal("expected non-empty encoded receipt")
	}

	// Nil receipt should return nil.
	data = encodeReceiptForProof(nil)
	if data != nil {
		t.Fatal("expected nil for nil receipt")
	}
}
