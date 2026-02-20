package das

import (
	"testing"
	"time"
)

func TestColumnCustodyManagerComputeAssignment(t *testing.T) {
	params := DefaultCustodyManagerParams()
	var nodeID [32]byte
	copy(nodeID[:], []byte("test-node-001"))
	mgr := NewColumnCustodyManager(params, nodeID)

	assignment, err := mgr.ComputeAssignment(1)
	if err != nil {
		t.Fatalf("ComputeAssignment failed: %v", err)
	}
	if assignment.Epoch != 1 {
		t.Errorf("expected epoch 1, got %d", assignment.Epoch)
	}
	if len(assignment.ColumnIndices) == 0 {
		t.Error("expected non-empty column indices")
	}
	if len(assignment.GroupIndices) == 0 {
		t.Error("expected non-empty group indices")
	}
	// At minimum, CUSTODY_REQUIREMENT groups with at least 1 column each.
	if uint64(len(assignment.GroupIndices)) < params.CustodyRequirement {
		t.Errorf("expected at least %d groups, got %d", params.CustodyRequirement, len(assignment.GroupIndices))
	}
}

func TestColumnCustodyManagerAssignmentDeterminism(t *testing.T) {
	params := DefaultCustodyManagerParams()
	var nodeID [32]byte
	copy(nodeID[:], []byte("determinism-test"))

	mgr1 := NewColumnCustodyManager(params, nodeID)
	mgr2 := NewColumnCustodyManager(params, nodeID)

	a1, err := mgr1.ComputeAssignment(42)
	if err != nil {
		t.Fatalf("ComputeAssignment mgr1: %v", err)
	}
	a2, err := mgr2.ComputeAssignment(42)
	if err != nil {
		t.Fatalf("ComputeAssignment mgr2: %v", err)
	}

	if len(a1.ColumnIndices) != len(a2.ColumnIndices) {
		t.Fatalf("column count mismatch: %d vs %d", len(a1.ColumnIndices), len(a2.ColumnIndices))
	}
	for i := range a1.ColumnIndices {
		if a1.ColumnIndices[i] != a2.ColumnIndices[i] {
			t.Errorf("column mismatch at %d: %d vs %d", i, a1.ColumnIndices[i], a2.ColumnIndices[i])
		}
	}
}

func TestColumnCustodyManagerDifferentNodesGetDifferentAssignments(t *testing.T) {
	params := DefaultCustodyManagerParams()

	var nodeA, nodeB [32]byte
	copy(nodeA[:], []byte("node-alpha"))
	copy(nodeB[:], []byte("node-beta"))

	mgrA := NewColumnCustodyManager(params, nodeA)
	mgrB := NewColumnCustodyManager(params, nodeB)

	aA, err := mgrA.ComputeAssignment(10)
	if err != nil {
		t.Fatalf("ComputeAssignment nodeA: %v", err)
	}
	aB, err := mgrB.ComputeAssignment(10)
	if err != nil {
		t.Fatalf("ComputeAssignment nodeB: %v", err)
	}

	// Very unlikely to be identical for different nodes.
	if len(aA.ColumnIndices) == len(aB.ColumnIndices) {
		same := true
		for i := range aA.ColumnIndices {
			if aA.ColumnIndices[i] != aB.ColumnIndices[i] {
				same = false
				break
			}
		}
		if same {
			t.Error("expected different assignments for different node IDs")
		}
	}
}

func TestColumnCustodyManagerSetEpochAndRotation(t *testing.T) {
	params := DefaultCustodyManagerParams()
	var nodeID [32]byte
	copy(nodeID[:], []byte("rotation-node"))
	mgr := NewColumnCustodyManager(params, nodeID)

	// Set initial epoch.
	if err := mgr.SetEpoch(1); err != nil {
		t.Fatalf("SetEpoch(1) failed: %v", err)
	}

	a1 := mgr.CurrentAssignment()
	if a1 == nil || a1.Epoch != 1 {
		t.Fatal("expected assignment for epoch 1")
	}

	// Rotate to epoch 2.
	if err := mgr.SetEpoch(2); err != nil {
		t.Fatalf("SetEpoch(2) failed: %v", err)
	}

	a2 := mgr.CurrentAssignment()
	if a2 == nil || a2.Epoch != 2 {
		t.Fatal("expected assignment for epoch 2")
	}

	rotation := mgr.GetRotation()
	if rotation == nil {
		t.Fatal("expected non-nil rotation")
	}
	if rotation.PreviousEpoch != 1 {
		t.Errorf("expected previous epoch 1, got %d", rotation.PreviousEpoch)
	}
	if rotation.CurrentEpoch != 2 {
		t.Errorf("expected current epoch 2, got %d", rotation.CurrentEpoch)
	}
	if !rotation.PendingMigration {
		t.Error("expected pending migration after epoch rotation")
	}
}

func TestColumnCustodyManagerInvalidEpochZero(t *testing.T) {
	params := DefaultCustodyManagerParams()
	var nodeID [32]byte
	mgr := NewColumnCustodyManager(params, nodeID)

	_, err := mgr.ComputeAssignment(0)
	if err != ErrInvalidEpoch {
		t.Errorf("expected ErrInvalidEpoch, got %v", err)
	}

	err = mgr.SetEpoch(0)
	if err != ErrInvalidEpoch {
		t.Errorf("expected ErrInvalidEpoch from SetEpoch(0), got %v", err)
	}
}

func TestColumnCustodyManagerStoreAndRetrieve(t *testing.T) {
	params := DefaultCustodyManagerParams()
	var nodeID [32]byte
	copy(nodeID[:], []byte("store-test"))
	mgr := NewColumnCustodyManager(params, nodeID)

	data := []byte("cell-data-for-column-5")
	if err := mgr.StoreColumn(1, 100, 5, data); err != nil {
		t.Fatalf("StoreColumn failed: %v", err)
	}

	if mgr.StoreCount() != 1 {
		t.Errorf("expected store count 1, got %d", mgr.StoreCount())
	}

	col, err := mgr.GetColumn(1, 5)
	if err != nil {
		t.Fatalf("GetColumn failed: %v", err)
	}
	if string(col.Data) != string(data) {
		t.Errorf("data mismatch: %q vs %q", col.Data, data)
	}
	if col.Epoch != 1 || col.Slot != 100 || col.Index != 5 {
		t.Errorf("metadata mismatch: epoch=%d slot=%d index=%d", col.Epoch, col.Slot, col.Index)
	}
}

func TestColumnCustodyManagerStoreDuplicate(t *testing.T) {
	params := DefaultCustodyManagerParams()
	var nodeID [32]byte
	mgr := NewColumnCustodyManager(params, nodeID)

	if err := mgr.StoreColumn(1, 100, 5, []byte("data1")); err != nil {
		t.Fatalf("first StoreColumn failed: %v", err)
	}
	err := mgr.StoreColumn(1, 100, 5, []byte("data2"))
	if err != ErrColumnAlreadyStored {
		t.Errorf("expected ErrColumnAlreadyStored, got %v", err)
	}
}

func TestColumnCustodyManagerGetNonexistent(t *testing.T) {
	params := DefaultCustodyManagerParams()
	var nodeID [32]byte
	mgr := NewColumnCustodyManager(params, nodeID)

	_, err := mgr.GetColumn(1, 99)
	if err != ErrColumnNotInCustody {
		t.Errorf("expected ErrColumnNotInCustody, got %v", err)
	}
}

func TestColumnCustodyManagerIsInCustody(t *testing.T) {
	params := DefaultCustodyManagerParams()
	var nodeID [32]byte
	copy(nodeID[:], []byte("custody-check"))
	mgr := NewColumnCustodyManager(params, nodeID)

	if err := mgr.SetEpoch(5); err != nil {
		t.Fatalf("SetEpoch failed: %v", err)
	}

	a := mgr.CurrentAssignment()
	if a == nil || len(a.ColumnIndices) == 0 {
		t.Fatal("expected non-empty assignment")
	}

	// First assigned column should be in custody.
	if !mgr.IsInCustody(a.ColumnIndices[0]) {
		t.Error("expected first assigned column to be in custody")
	}

	// Column 999999 should not be in custody.
	if mgr.IsInCustody(999999) {
		t.Error("expected column 999999 to NOT be in custody")
	}
}

func TestColumnCustodyManagerGenerateProofResponse(t *testing.T) {
	params := DefaultCustodyManagerParams()
	var nodeID [32]byte
	copy(nodeID[:], []byte("proof-gen"))
	mgr := NewColumnCustodyManager(params, nodeID)

	data := []byte("proof-test-data")
	if err := mgr.StoreColumn(3, 200, 10, data); err != nil {
		t.Fatalf("StoreColumn failed: %v", err)
	}

	resp, err := mgr.GenerateProofResponse(3, 10)
	if err != nil {
		t.Fatalf("GenerateProofResponse failed: %v", err)
	}
	if resp.Epoch != 3 || resp.Column != 10 {
		t.Errorf("expected epoch=3 column=10, got epoch=%d column=%d", resp.Epoch, resp.Column)
	}
	if len(resp.ProofHash) != 32 {
		t.Errorf("expected 32-byte proof hash, got %d", len(resp.ProofHash))
	}
	if string(resp.CellData) != string(data) {
		t.Errorf("cell data mismatch: %q vs %q", resp.CellData, data)
	}

	// Same inputs should produce the same proof hash.
	resp2, err := mgr.GenerateProofResponse(3, 10)
	if err != nil {
		t.Fatalf("second GenerateProofResponse failed: %v", err)
	}
	for i := range resp.ProofHash {
		if resp.ProofHash[i] != resp2.ProofHash[i] {
			t.Error("proof hash not deterministic")
			break
		}
	}
}

func TestColumnCustodyManagerProofResponseNotStored(t *testing.T) {
	params := DefaultCustodyManagerParams()
	var nodeID [32]byte
	mgr := NewColumnCustodyManager(params, nodeID)

	_, err := mgr.GenerateProofResponse(1, 42)
	if err == nil {
		t.Error("expected error for non-stored column proof")
	}
}

func TestColumnCustodyManagerSelectSampleColumns(t *testing.T) {
	params := DefaultCustodyManagerParams()
	var nodeID [32]byte
	copy(nodeID[:], []byte("sampler-node"))
	mgr := NewColumnCustodyManager(params, nodeID)

	cols := mgr.SelectSampleColumns(100)
	if len(cols) != params.SamplesPerSlot {
		t.Errorf("expected %d samples, got %d", params.SamplesPerSlot, len(cols))
	}

	// Verify sorted and in range.
	for i, c := range cols {
		if c >= params.NumberOfColumns {
			t.Errorf("column %d out of range: %d >= %d", i, c, params.NumberOfColumns)
		}
		if i > 0 && cols[i-1] >= c {
			t.Errorf("columns not sorted: %d >= %d", cols[i-1], c)
		}
	}

	// Determinism: same slot produces same columns.
	cols2 := mgr.SelectSampleColumns(100)
	if len(cols) != len(cols2) {
		t.Fatalf("sample count mismatch: %d vs %d", len(cols), len(cols2))
	}
	for i := range cols {
		if cols[i] != cols2[i] {
			t.Errorf("sample mismatch at %d: %d vs %d", i, cols[i], cols2[i])
		}
	}
}

func TestColumnCustodyManagerSamplingDifferentSlots(t *testing.T) {
	params := DefaultCustodyManagerParams()
	var nodeID [32]byte
	copy(nodeID[:], []byte("slot-sampler"))
	mgr := NewColumnCustodyManager(params, nodeID)

	cols1 := mgr.SelectSampleColumns(1)
	cols2 := mgr.SelectSampleColumns(2)

	// Different slots should (very likely) produce different sample sets.
	same := true
	if len(cols1) == len(cols2) {
		for i := range cols1 {
			if cols1[i] != cols2[i] {
				same = false
				break
			}
		}
	} else {
		same = false
	}
	if same {
		t.Error("expected different samples for different slots")
	}
}

func TestColumnCustodyManagerClose(t *testing.T) {
	params := DefaultCustodyManagerParams()
	var nodeID [32]byte
	mgr := NewColumnCustodyManager(params, nodeID)

	mgr.Close()

	err := mgr.SetEpoch(1)
	if err != ErrCustodyManagerClosed {
		t.Errorf("expected ErrCustodyManagerClosed, got %v", err)
	}

	err = mgr.StoreColumn(1, 100, 5, []byte("data"))
	if err != ErrCustodyManagerClosed {
		t.Errorf("expected ErrCustodyManagerClosed from StoreColumn, got %v", err)
	}
}

func TestColumnCustodyManagerStoreCapacityEviction(t *testing.T) {
	params := DefaultCustodyManagerParams()
	params.MaxStoredColumns = 5
	var nodeID [32]byte
	mgr := NewColumnCustodyManager(params, nodeID)

	// Store 5 columns.
	for i := uint64(0); i < 5; i++ {
		if err := mgr.StoreColumn(1, 100+i, i, []byte("data")); err != nil {
			t.Fatalf("StoreColumn %d: %v", i, err)
		}
		// Small delay so StoredAt timestamps differ.
		time.Sleep(time.Millisecond)
	}

	if mgr.StoreCount() != 5 {
		t.Errorf("expected 5 stored, got %d", mgr.StoreCount())
	}

	// Storing one more should evict the oldest.
	if err := mgr.StoreColumn(1, 200, 99, []byte("new")); err != nil {
		t.Fatalf("StoreColumn overflow: %v", err)
	}

	if mgr.StoreCount() != 5 {
		t.Errorf("expected 5 stored after eviction, got %d", mgr.StoreCount())
	}
}

func TestColumnCustodyManagerEpochExpiry(t *testing.T) {
	params := DefaultCustodyManagerParams()
	params.ColumnExpiryEpochs = 10
	var nodeID [32]byte
	copy(nodeID[:], []byte("expiry-test"))
	mgr := NewColumnCustodyManager(params, nodeID)

	// Store a column at epoch 1.
	if err := mgr.StoreColumn(1, 10, 0, []byte("old-data")); err != nil {
		t.Fatalf("StoreColumn: %v", err)
	}

	// Advance to epoch 20 (epoch 1 is older than 10 epochs ago).
	if err := mgr.SetEpoch(20); err != nil {
		t.Fatalf("SetEpoch: %v", err)
	}

	// The epoch 1 column should have been expired.
	if mgr.StoreCount() != 0 {
		t.Errorf("expected 0 stored after expiry, got %d", mgr.StoreCount())
	}
}
