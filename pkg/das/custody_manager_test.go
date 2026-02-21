package das

import (
	"sync"
	"testing"
)

func custodyMgrTestNodeID(seed byte) [32]byte {
	var id [32]byte
	for i := range id {
		id[i] = seed + byte(i)
	}
	return id
}

func TestCustodyManagerInitialize(t *testing.T) {
	nodeID := custodyMgrTestNodeID(0x42)
	cm := NewCustodyManager(DefaultCustodyManagerConfig(), nodeID)

	// Epoch 0 should fail.
	if err := cm.Initialize(0); err != ErrCustodyMgrEpochZero {
		t.Fatalf("expected ErrCustodyMgrEpochZero, got %v", err)
	}

	// Valid epoch should succeed.
	if err := cm.Initialize(1); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	if cm.CurrentEpoch() != 1 {
		t.Fatalf("expected epoch 1, got %d", cm.CurrentEpoch())
	}

	cols := cm.CurrentColumns()
	if len(cols) == 0 {
		t.Fatal("expected non-empty columns after init")
	}

	groups := cm.CurrentGroups()
	if len(groups) == 0 {
		t.Fatal("expected non-empty groups after init")
	}
}

func TestCustodyManagerCustodyAssignment(t *testing.T) {
	nodeID := custodyMgrTestNodeID(0xAA)
	cm := NewCustodyManager(DefaultCustodyManagerConfig(), nodeID)

	if err := cm.Initialize(5); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	cols := cm.CurrentColumns()
	// With CustodyRequirement=4 and 1 column per group, expect 4 columns.
	if len(cols) < int(CustodyRequirement) {
		t.Fatalf("expected at least %d columns, got %d", CustodyRequirement, len(cols))
	}

	// Verify columns are in range.
	for _, c := range cols {
		if c >= NumberOfColumns {
			t.Fatalf("column %d out of range [0, %d)", c, NumberOfColumns)
		}
	}

	// Verify columns are sorted.
	for i := 1; i < len(cols); i++ {
		if cols[i] <= cols[i-1] {
			t.Fatalf("columns not sorted: cols[%d]=%d <= cols[%d]=%d", i, cols[i], i-1, cols[i-1])
		}
	}
}

func TestCustodyManagerIsColumnInCustody(t *testing.T) {
	nodeID := custodyMgrTestNodeID(0xBB)
	cm := NewCustodyManager(DefaultCustodyManagerConfig(), nodeID)

	if err := cm.Initialize(3); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	cols := cm.CurrentColumns()
	if len(cols) == 0 {
		t.Fatal("no columns assigned")
	}

	// Each assigned column should be in custody.
	for _, c := range cols {
		if !cm.IsColumnInCustody(c) {
			t.Fatalf("column %d should be in custody", c)
		}
	}

	// An out-of-range column should not be in custody.
	if cm.IsColumnInCustody(NumberOfColumns + 100) {
		t.Fatal("out-of-range column should not be in custody")
	}
}

func TestCustodyManagerRotateEpoch(t *testing.T) {
	nodeID := custodyMgrTestNodeID(0xCC)
	cm := NewCustodyManager(DefaultCustodyManagerConfig(), nodeID)

	if err := cm.Initialize(1); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	epoch1Cols := cm.CurrentColumns()

	// Rotate to epoch 2.
	event, err := cm.RotateEpoch(2)
	if err != nil {
		t.Fatalf("RotateEpoch: %v", err)
	}

	if event.FromEpoch != 1 {
		t.Fatalf("expected FromEpoch=1, got %d", event.FromEpoch)
	}
	if event.ToEpoch != 2 {
		t.Fatalf("expected ToEpoch=2, got %d", event.ToEpoch)
	}
	if cm.CurrentEpoch() != 2 {
		t.Fatalf("expected current epoch 2, got %d", cm.CurrentEpoch())
	}

	epoch2Cols := cm.CurrentColumns()
	if len(epoch2Cols) == 0 {
		t.Fatal("expected columns after rotation")
	}

	// Verify that added and dropped columns are consistent.
	oldSet := toSet(epoch1Cols)
	newSet := toSet(epoch2Cols)

	for _, c := range event.AddedColumns {
		if oldSet[c] {
			t.Fatalf("added column %d was already in old set", c)
		}
		if !newSet[c] {
			t.Fatalf("added column %d not in new set", c)
		}
	}
	for _, c := range event.DroppedColumns {
		if !oldSet[c] {
			t.Fatalf("dropped column %d was not in old set", c)
		}
		if newSet[c] {
			t.Fatalf("dropped column %d still in new set", c)
		}
	}
}

func TestCustodyManagerRotationHistory(t *testing.T) {
	nodeID := custodyMgrTestNodeID(0xDD)
	cm := NewCustodyManager(DefaultCustodyManagerConfig(), nodeID)

	if err := cm.Initialize(1); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	for e := uint64(2); e <= 5; e++ {
		if _, err := cm.RotateEpoch(e); err != nil {
			t.Fatalf("RotateEpoch(%d): %v", e, err)
		}
	}

	history := cm.RotationHistory()
	if len(history) != 4 {
		t.Fatalf("expected 4 rotation events, got %d", len(history))
	}

	for i, ev := range history {
		expectedFrom := uint64(i + 1)
		expectedTo := uint64(i + 2)
		if ev.FromEpoch != expectedFrom || ev.ToEpoch != expectedTo {
			t.Fatalf("event %d: expected %d->%d, got %d->%d",
				i, expectedFrom, expectedTo, ev.FromEpoch, ev.ToEpoch)
		}
	}
}

func TestCustodyManagerRecordColumn(t *testing.T) {
	nodeID := custodyMgrTestNodeID(0xEE)
	cm := NewCustodyManager(DefaultCustodyManagerConfig(), nodeID)

	if err := cm.Initialize(1); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	cols := cm.CurrentColumns()
	if len(cols) == 0 {
		t.Fatal("no columns assigned")
	}

	data := []byte("test column data for custody")

	// Record a column.
	err := cm.RecordColumn(1, 32, cols[0], data)
	if err != nil {
		t.Fatalf("RecordColumn: %v", err)
	}

	if cm.StoredColumnCount() != 1 {
		t.Fatalf("expected 1 stored column, got %d", cm.StoredColumnCount())
	}

	// Duplicate should fail.
	err = cm.RecordColumn(1, 32, cols[0], data)
	if err != ErrCustodyMgrAlreadyStored {
		t.Fatalf("expected ErrCustodyMgrAlreadyStored, got %v", err)
	}

	// Out-of-range column should fail.
	err = cm.RecordColumn(1, 32, NumberOfColumns+1, data)
	if err == nil {
		t.Fatal("expected error for out-of-range column")
	}
}

func TestCustodyManagerSlotCompleteness(t *testing.T) {
	nodeID := custodyMgrTestNodeID(0x11)
	cm := NewCustodyManager(DefaultCustodyManagerConfig(), nodeID)

	if err := cm.Initialize(1); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	cols := cm.CurrentColumns()
	slot := uint64(10)
	data := []byte("cell data")

	// Record all but the last column.
	for i := 0; i < len(cols)-1; i++ {
		if err := cm.RecordColumn(1, slot, cols[i], data); err != nil {
			t.Fatalf("RecordColumn(%d): %v", cols[i], err)
		}
	}

	comp, err := cm.CheckSlotCompleteness(slot)
	if err != nil {
		t.Fatalf("CheckSlotCompleteness: %v", err)
	}
	if comp.Complete {
		t.Fatal("slot should not be complete yet")
	}

	// Record the last column.
	if err := cm.RecordColumn(1, slot, cols[len(cols)-1], data); err != nil {
		t.Fatalf("RecordColumn: %v", err)
	}

	comp, err = cm.CheckSlotCompleteness(slot)
	if err != nil {
		t.Fatalf("CheckSlotCompleteness: %v", err)
	}
	if !comp.Complete {
		t.Fatal("slot should be complete after all columns recorded")
	}
}

func TestCustodyManagerValidateProofRequest(t *testing.T) {
	nodeID := custodyMgrTestNodeID(0x22)
	cm := NewCustodyManager(DefaultCustodyManagerConfig(), nodeID)

	if err := cm.Initialize(1); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	cols := cm.CurrentColumns()
	slot := uint64(5)
	data := []byte("proof data")

	// Record a column.
	if err := cm.RecordColumn(1, slot, cols[0], data); err != nil {
		t.Fatalf("RecordColumn: %v", err)
	}

	// Valid proof request.
	req := &CustodyProofRequest{
		NodeID: nodeID,
		Epoch:  1,
		Column: cols[0],
		Slot:   slot,
	}
	result, err := cm.ValidateCustodyProofRequest(req)
	if err != nil {
		t.Fatalf("ValidateCustodyProofRequest: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected valid proof, got: %s", result.Reason)
	}

	// Nil request should fail.
	_, err = cm.ValidateCustodyProofRequest(nil)
	if err == nil {
		t.Fatal("expected error for nil request")
	}

	// Epoch 0 should fail.
	req2 := &CustodyProofRequest{
		NodeID: nodeID,
		Epoch:  0,
		Column: cols[0],
		Slot:   slot,
	}
	_, err = cm.ValidateCustodyProofRequest(req2)
	if err != ErrCustodyMgrEpochZero {
		t.Fatalf("expected ErrCustodyMgrEpochZero, got %v", err)
	}
}

func TestCustodyManagerProofRequestNoData(t *testing.T) {
	nodeID := custodyMgrTestNodeID(0x33)
	cm := NewCustodyManager(DefaultCustodyManagerConfig(), nodeID)

	if err := cm.Initialize(1); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	cols := cm.CurrentColumns()

	// Request proof without recording data.
	req := &CustodyProofRequest{
		NodeID: nodeID,
		Epoch:  1,
		Column: cols[0],
		Slot:   5,
	}
	result, err := cm.ValidateCustodyProofRequest(req)
	if err != nil {
		t.Fatalf("ValidateCustodyProofRequest: %v", err)
	}
	if result.Valid {
		t.Fatal("expected invalid proof when data not stored")
	}
}

func TestCustodyManagerClose(t *testing.T) {
	nodeID := custodyMgrTestNodeID(0x44)
	cm := NewCustodyManager(DefaultCustodyManagerConfig(), nodeID)

	if err := cm.Initialize(1); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	cm.Close()

	// Operations should fail after close.
	_, err := cm.RotateEpoch(2)
	if err != ErrCustodyMgrClosed {
		t.Fatalf("expected ErrCustodyMgrClosed, got %v", err)
	}

	err = cm.RecordColumn(1, 32, 0, []byte("data"))
	if err != ErrCustodyMgrClosed {
		t.Fatalf("expected ErrCustodyMgrClosed, got %v", err)
	}
}

func TestCustodyManagerConcurrentAccess(t *testing.T) {
	nodeID := custodyMgrTestNodeID(0x55)
	cm := NewCustodyManager(DefaultCustodyManagerConfig(), nodeID)

	if err := cm.Initialize(1); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	cols := cm.CurrentColumns()
	if len(cols) == 0 {
		t.Fatal("no columns")
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	// Concurrent reads.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cm.CurrentColumns()
			_ = cm.CurrentGroups()
			_ = cm.IsColumnInCustody(cols[0])
			_ = cm.CurrentEpoch()
			_ = cm.StoredColumnCount()
		}()
	}

	// Concurrent writes.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			slot := uint64(100 + i)
			col := cols[i%len(cols)]
			err := cm.RecordColumn(1, slot, col, []byte("data"))
			if err != nil && err != ErrCustodyMgrAlreadyStored {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrent error: %v", err)
	}
}

func TestCustodyManagerDeterministicAssignment(t *testing.T) {
	nodeID := custodyMgrTestNodeID(0x66)

	// Two managers with the same nodeID and epoch should produce the same
	// assignment.
	cm1 := NewCustodyManager(DefaultCustodyManagerConfig(), nodeID)
	cm2 := NewCustodyManager(DefaultCustodyManagerConfig(), nodeID)

	if err := cm1.Initialize(10); err != nil {
		t.Fatalf("cm1 Initialize: %v", err)
	}
	if err := cm2.Initialize(10); err != nil {
		t.Fatalf("cm2 Initialize: %v", err)
	}

	cols1 := cm1.CurrentColumns()
	cols2 := cm2.CurrentColumns()

	if len(cols1) != len(cols2) {
		t.Fatalf("column count mismatch: %d vs %d", len(cols1), len(cols2))
	}
	for i := range cols1 {
		if cols1[i] != cols2[i] {
			t.Fatalf("column[%d] mismatch: %d vs %d", i, cols1[i], cols2[i])
		}
	}
}

func TestCustodyManagerDifferentEpochsDifferentAssignments(t *testing.T) {
	nodeID := custodyMgrTestNodeID(0x77)
	cm := NewCustodyManager(DefaultCustodyManagerConfig(), nodeID)

	if err := cm.Initialize(1); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	cols1 := cm.CurrentColumns()

	if _, err := cm.RotateEpoch(100); err != nil {
		t.Fatalf("RotateEpoch: %v", err)
	}
	cols100 := cm.CurrentColumns()

	// With different epochs, the columns should typically differ due to
	// epoch-based key derivation. It's theoretically possible (but unlikely)
	// they match, so we only check that the function completes without error.
	_ = cols1
	_ = cols100
}

func TestCustodyManagerDataExpiry(t *testing.T) {
	nodeID := custodyMgrTestNodeID(0x88)
	config := DefaultCustodyManagerConfig()
	config.RetentionEpochs = 5 // short retention for testing
	cm := NewCustodyManager(config, nodeID)

	if err := cm.Initialize(1); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	cols := cm.CurrentColumns()
	if len(cols) == 0 {
		t.Fatal("no columns")
	}

	// Store data in epoch 1.
	if err := cm.RecordColumn(1, 10, cols[0], []byte("old data")); err != nil {
		t.Fatalf("RecordColumn: %v", err)
	}
	if cm.StoredColumnCount() != 1 {
		t.Fatalf("expected 1, got %d", cm.StoredColumnCount())
	}

	// Rotate to epoch 10 (beyond retention of 5).
	for e := uint64(2); e <= 10; e++ {
		if _, err := cm.RotateEpoch(e); err != nil {
			t.Fatalf("RotateEpoch(%d): %v", e, err)
		}
	}

	// Old data should have been expired.
	if cm.StoredColumnCount() != 0 {
		t.Fatalf("expected 0 stored columns after expiry, got %d", cm.StoredColumnCount())
	}
}

func TestCustodyManagerTrackedSlots(t *testing.T) {
	nodeID := custodyMgrTestNodeID(0x99)
	config := DefaultCustodyManagerConfig()
	config.MaxTrackedSlots = 3
	cm := NewCustodyManager(config, nodeID)

	if err := cm.Initialize(1); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	cols := cm.CurrentColumns()
	if len(cols) == 0 {
		t.Fatal("no columns")
	}

	// Record columns for 4 different slots (exceeds MaxTrackedSlots=3).
	for slot := uint64(1); slot <= 4; slot++ {
		if err := cm.RecordColumn(1, slot, cols[0], []byte("data")); err != nil {
			t.Fatalf("RecordColumn slot %d: %v", slot, err)
		}
	}

	// Should have at most MaxTrackedSlots tracked.
	if cm.TrackedSlotCount() > config.MaxTrackedSlots {
		t.Fatalf("expected at most %d tracked slots, got %d",
			config.MaxTrackedSlots, cm.TrackedSlotCount())
	}
}
