package das

import (
	"sync"
	"testing"
)

// --- CustodySubnetManager creation ---

func TestNewCustodySubnetManager(t *testing.T) {
	cfg := DefaultCustodyConfig()
	m := NewCustodySubnetManager(cfg)
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	if m.PeerCount() != 0 {
		t.Fatalf("expected 0 peers, got %d", m.PeerCount())
	}
}

func TestDefaultCustodyConfig(t *testing.T) {
	cfg := DefaultCustodyConfig()
	if cfg.CustodyRequirement != CustodyRequirement {
		t.Fatalf("CustodyRequirement = %d, want %d", cfg.CustodyRequirement, CustodyRequirement)
	}
	if cfg.NumberOfColumns != NumberOfColumns {
		t.Fatalf("NumberOfColumns = %d, want %d", cfg.NumberOfColumns, NumberOfColumns)
	}
	if cfg.DataColumnSidecarSubnetCount != DataColumnSidecarSubnetCount {
		t.Fatalf("SubnetCount = %d, want %d", cfg.DataColumnSidecarSubnetCount, DataColumnSidecarSubnetCount)
	}
	if cfg.NumberOfCustodyGroups != NumberOfCustodyGroups {
		t.Fatalf("CustodyGroups = %d, want %d", cfg.NumberOfCustodyGroups, NumberOfCustodyGroups)
	}
}

// --- AssignCustodyForNode ---

func TestAssignCustodyForNode(t *testing.T) {
	m := NewCustodySubnetManager(DefaultCustodyConfig())
	nodeID := [32]byte{0x01, 0x02, 0x03}

	assignment, err := m.AssignCustodyForNode(nodeID, CustodyRequirement)
	if err != nil {
		t.Fatalf("AssignCustodyForNode: %v", err)
	}

	if assignment.NodeID != nodeID {
		t.Fatal("NodeID mismatch")
	}
	if len(assignment.CustodyGroups) != int(CustodyRequirement) {
		t.Fatalf("got %d groups, want %d", len(assignment.CustodyGroups), CustodyRequirement)
	}
	if len(assignment.ColumnIndices) == 0 {
		t.Fatal("expected non-empty column indices")
	}
	if len(assignment.SubnetIDs) == 0 {
		t.Fatal("expected non-empty subnet IDs")
	}

	// All columns in range.
	for _, col := range assignment.ColumnIndices {
		if col >= NumberOfColumns {
			t.Fatalf("column %d out of range", col)
		}
	}

	// All subnets in range.
	for _, s := range assignment.SubnetIDs {
		if s >= DataColumnSidecarSubnetCount {
			t.Fatalf("subnet %d out of range", s)
		}
	}

	// No duplicate columns.
	seen := make(map[uint64]bool)
	for _, c := range assignment.ColumnIndices {
		if seen[c] {
			t.Fatalf("duplicate column %d", c)
		}
		seen[c] = true
	}
}

func TestAssignCustodyForNodeDeterministic(t *testing.T) {
	m := NewCustodySubnetManager(DefaultCustodyConfig())
	nodeID := [32]byte{0xaa, 0xbb}

	a1, _ := m.AssignCustodyForNode(nodeID, CustodyRequirement)
	a2, _ := m.AssignCustodyForNode(nodeID, CustodyRequirement)

	if len(a1.ColumnIndices) != len(a2.ColumnIndices) {
		t.Fatal("non-deterministic column count")
	}
	for i := range a1.ColumnIndices {
		if a1.ColumnIndices[i] != a2.ColumnIndices[i] {
			t.Fatalf("non-deterministic: col[%d] = %d vs %d",
				i, a1.ColumnIndices[i], a2.ColumnIndices[i])
		}
	}
}

func TestAssignCustodyForNodeClampMin(t *testing.T) {
	m := NewCustodySubnetManager(DefaultCustodyConfig())
	nodeID := [32]byte{0x42}

	// Request fewer than CustodyRequirement; should be clamped up.
	assignment, err := m.AssignCustodyForNode(nodeID, 1)
	if err != nil {
		t.Fatalf("AssignCustodyForNode: %v", err)
	}
	if len(assignment.CustodyGroups) < int(CustodyRequirement) {
		t.Fatalf("got %d groups, want at least %d", len(assignment.CustodyGroups), CustodyRequirement)
	}
}

func TestAssignCustodyForNodeExceedsMax(t *testing.T) {
	m := NewCustodySubnetManager(DefaultCustodyConfig())
	nodeID := [32]byte{}

	_, err := m.AssignCustodyForNode(nodeID, NumberOfCustodyGroups+1)
	if err == nil {
		t.Fatal("expected error for exceeding max groups")
	}
}

func TestAssignCustodyForNodeSupernode(t *testing.T) {
	m := NewCustodySubnetManager(DefaultCustodyConfig())
	nodeID := [32]byte{0xff}

	// Supernode: custody all groups.
	assignment, err := m.AssignCustodyForNode(nodeID, NumberOfCustodyGroups)
	if err != nil {
		t.Fatalf("AssignCustodyForNode (supernode): %v", err)
	}
	if len(assignment.ColumnIndices) != int(NumberOfColumns) {
		t.Fatalf("supernode got %d columns, want %d", len(assignment.ColumnIndices), NumberOfColumns)
	}
}

// --- SetLocalNode / LocalAssignment ---

func TestSetLocalNodeAndLocalAssignment(t *testing.T) {
	m := NewCustodySubnetManager(DefaultCustodyConfig())

	// Before setting, should be nil.
	if m.LocalAssignment() != nil {
		t.Fatal("expected nil before SetLocalNode")
	}

	nodeID := [32]byte{0x10, 0x20}
	if err := m.SetLocalNode(nodeID, CustodyRequirement); err != nil {
		t.Fatalf("SetLocalNode: %v", err)
	}

	local := m.LocalAssignment()
	if local == nil {
		t.Fatal("expected non-nil after SetLocalNode")
	}
	if local.NodeID != nodeID {
		t.Fatal("NodeID mismatch")
	}
	if len(local.ColumnIndices) == 0 {
		t.Fatal("expected non-empty columns")
	}
}

// --- ComputeCustodyColumns ---

func TestCustodySubnetManagerComputeColumns(t *testing.T) {
	m := NewCustodySubnetManager(DefaultCustodyConfig())
	nodeID := [32]byte{0x01}

	columns := m.ComputeCustodyColumns(nodeID, CustodyRequirement, 1)

	// With 128 groups and 128 columns, columns_per_group = 1,
	// and CustodyRequirement = 4, so we get exactly 4 columns.
	if len(columns) != int(CustodyRequirement) {
		t.Fatalf("got %d columns, want %d", len(columns), CustodyRequirement)
	}

	// Verify sorted order.
	for i := 1; i < len(columns); i++ {
		if columns[i] <= columns[i-1] {
			t.Fatalf("columns not sorted: [%d]=%d <= [%d]=%d",
				i, columns[i], i-1, columns[i-1])
		}
	}

	// All in range.
	for _, c := range columns {
		if c >= NumberOfColumns {
			t.Fatalf("column %d out of range", c)
		}
	}
}

func TestCustodySubnetManagerComputeColumnsClampsMin(t *testing.T) {
	m := NewCustodySubnetManager(DefaultCustodyConfig())
	nodeID := [32]byte{0x55}

	// Requesting 0 should be clamped to CustodyRequirement.
	columns := m.ComputeCustodyColumns(nodeID, 0, 1)
	if len(columns) < int(CustodyRequirement) {
		t.Fatalf("got %d columns, want at least %d", len(columns), CustodyRequirement)
	}
}

// --- IsInCustody ---

func TestIsInCustody(t *testing.T) {
	m := NewCustodySubnetManager(DefaultCustodyConfig())
	nodeID := [32]byte{0xde, 0xad}

	columns := m.ComputeCustodyColumns(nodeID, CustodyRequirement, 1)
	if len(columns) == 0 {
		t.Fatal("no columns assigned")
	}

	// A column in the set should return true.
	if !m.IsInCustody(nodeID, columns[0]) {
		t.Fatalf("column %d should be in custody", columns[0])
	}

	// Find a column NOT in the set.
	colSet := make(map[uint64]bool)
	for _, c := range columns {
		colSet[c] = true
	}
	for i := uint64(0); i < NumberOfColumns; i++ {
		if !colSet[i] {
			if m.IsInCustody(nodeID, i) {
				t.Fatalf("column %d should NOT be in custody", i)
			}
			break
		}
	}

	// Out-of-range column should return false.
	if m.IsInCustody(nodeID, NumberOfColumns) {
		t.Fatal("out-of-range column should not be in custody")
	}
}

// --- SubnetForColumn ---

func TestSubnetForColumn(t *testing.T) {
	m := NewCustodySubnetManager(DefaultCustodyConfig())

	tests := []struct {
		col  uint64
		want uint64
	}{
		{0, 0},
		{1, 1},
		{127, 127},
		{128, 0},   // wraps around since DataColumnSidecarSubnetCount = 128
		{129, 1},
	}

	for _, tt := range tests {
		got := m.SubnetForColumn(tt.col)
		// With DataColumnSidecarSubnetCount=128, subnet = col % 128.
		expected := tt.col % DataColumnSidecarSubnetCount
		if got != expected {
			t.Errorf("SubnetForColumn(%d) = %d, want %d", tt.col, got, expected)
		}
	}
}

// --- ValidateCustody ---

func TestValidateCustodySuccess(t *testing.T) {
	m := NewCustodySubnetManager(DefaultCustodyConfig())
	nodeID := [32]byte{0x01, 0x02}
	m.SetLocalNode(nodeID, CustodyRequirement)

	local := m.LocalAssignment()
	if local == nil {
		t.Fatal("expected local assignment")
	}

	// Provide all required columns.
	columns := make([]DataColumn, len(local.ColumnIndices))
	for i, idx := range local.ColumnIndices {
		columns[i] = DataColumn{
			Index: ColumnIndex(idx),
			Cells: []Cell{{}},
		}
	}

	if err := m.ValidateCustody(columns); err != nil {
		t.Fatalf("ValidateCustody should pass: %v", err)
	}
}

func TestValidateCustodyMissing(t *testing.T) {
	m := NewCustodySubnetManager(DefaultCustodyConfig())
	nodeID := [32]byte{0x01, 0x02}
	m.SetLocalNode(nodeID, CustodyRequirement)

	// Provide empty set of columns.
	err := m.ValidateCustody(nil)
	if err == nil {
		t.Fatal("expected error for missing columns")
	}
}

func TestValidateCustodyPartial(t *testing.T) {
	m := NewCustodySubnetManager(DefaultCustodyConfig())
	nodeID := [32]byte{0x01, 0x02}
	m.SetLocalNode(nodeID, CustodyRequirement)

	local := m.LocalAssignment()
	// Provide only the first column, omit the rest.
	columns := []DataColumn{
		{Index: ColumnIndex(local.ColumnIndices[0]), Cells: []Cell{{}}},
	}

	err := m.ValidateCustody(columns)
	if err == nil {
		t.Fatal("expected error for partial columns")
	}
}

func TestValidateCustodyNoLocalNode(t *testing.T) {
	m := NewCustodySubnetManager(DefaultCustodyConfig())

	// No local node set: should pass (no requirement).
	if err := m.ValidateCustody(nil); err != nil {
		t.Fatalf("should pass with no local node: %v", err)
	}
}

func TestValidateCustodyOutOfRange(t *testing.T) {
	m := NewCustodySubnetManager(DefaultCustodyConfig())
	nodeID := [32]byte{0x01}
	m.SetLocalNode(nodeID, CustodyRequirement)

	columns := []DataColumn{
		{Index: ColumnIndex(NumberOfColumns), Cells: []Cell{{}}},
	}
	err := m.ValidateCustody(columns)
	if err == nil {
		t.Fatal("expected error for out-of-range column")
	}
}

// --- Peer discovery ---

func TestRegisterAndFindPeers(t *testing.T) {
	m := NewCustodySubnetManager(DefaultCustodyConfig())

	peer1 := [32]byte{0x01}
	peer2 := [32]byte{0x02}

	m.RegisterPeer(peer1, CustodyRequirement)
	m.RegisterPeer(peer2, CustodyRequirement)

	if m.PeerCount() != 2 {
		t.Fatalf("expected 2 peers, got %d", m.PeerCount())
	}

	// Get columns for peer1 and check we can find it.
	cols1 := m.ComputeCustodyColumns(peer1, CustodyRequirement, 1)
	if len(cols1) == 0 {
		t.Fatal("no columns for peer1")
	}

	peers, err := m.FindPeersForColumn(cols1[0])
	if err != nil {
		t.Fatalf("FindPeersForColumn: %v", err)
	}

	found := false
	for _, p := range peers {
		if p == peer1 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("peer1 not found for column %d", cols1[0])
	}
}

func TestFindPeersForColumnNotFound(t *testing.T) {
	m := NewCustodySubnetManager(DefaultCustodyConfig())

	// No peers registered.
	_, err := m.FindPeersForColumn(0)
	if err == nil {
		t.Fatal("expected error with no peers")
	}
}

func TestFindPeersForColumnOutOfRange(t *testing.T) {
	m := NewCustodySubnetManager(DefaultCustodyConfig())
	_, err := m.FindPeersForColumn(NumberOfColumns)
	if err == nil {
		t.Fatal("expected error for out-of-range column")
	}
}

func TestUnregisterPeer(t *testing.T) {
	m := NewCustodySubnetManager(DefaultCustodyConfig())
	peer := [32]byte{0x10}

	m.RegisterPeer(peer, CustodyRequirement)
	if m.PeerCount() != 1 {
		t.Fatalf("expected 1 peer, got %d", m.PeerCount())
	}

	m.UnregisterPeer(peer)
	if m.PeerCount() != 0 {
		t.Fatalf("expected 0 peers after unregister, got %d", m.PeerCount())
	}

	// Finding peers for the unregistered peer's columns should fail.
	cols := m.ComputeCustodyColumns(peer, CustodyRequirement, 1)
	_, err := m.FindPeersForColumn(cols[0])
	if err == nil {
		t.Fatal("expected error after unregister")
	}
}

func TestUnregisterPeerNotRegistered(t *testing.T) {
	m := NewCustodySubnetManager(DefaultCustodyConfig())
	// Should not panic.
	m.UnregisterPeer([32]byte{0xff})
}

func TestReRegisterPeer(t *testing.T) {
	m := NewCustodySubnetManager(DefaultCustodyConfig())
	peer := [32]byte{0x20}

	// Register, then re-register with different group count.
	m.RegisterPeer(peer, CustodyRequirement)
	m.RegisterPeer(peer, CustodyRequirement+2)

	if m.PeerCount() != 1 {
		t.Fatalf("expected 1 peer after re-register, got %d", m.PeerCount())
	}
}

// --- Concurrency ---

func TestCustodySubnetManagerConcurrency(t *testing.T) {
	m := NewCustodySubnetManager(DefaultCustodyConfig())

	var wg sync.WaitGroup

	// Concurrent peer registrations.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := [32]byte{byte(idx)}
			m.RegisterPeer(id, CustodyRequirement)
		}(i)
	}
	wg.Wait()

	if m.PeerCount() != 50 {
		t.Fatalf("expected 50 peers, got %d", m.PeerCount())
	}

	// Concurrent reads.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := [32]byte{byte(idx)}
			m.IsInCustody(id, uint64(idx))
			m.FindPeersForColumn(uint64(idx % int(NumberOfColumns)))
		}(i)
	}
	wg.Wait()

	// Concurrent unregistrations.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := [32]byte{byte(idx)}
			m.UnregisterPeer(id)
		}(i)
	}
	wg.Wait()

	if m.PeerCount() != 0 {
		t.Fatalf("expected 0 peers after unregister all, got %d", m.PeerCount())
	}
}

func TestCustodySubnetManagerConcurrentLocalNode(t *testing.T) {
	m := NewCustodySubnetManager(DefaultCustodyConfig())

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := [32]byte{byte(idx)}
			m.SetLocalNode(id, CustodyRequirement)
			m.LocalAssignment()
		}(i)
	}
	wg.Wait()

	// Should have some local assignment set.
	if m.LocalAssignment() == nil {
		t.Fatal("expected non-nil local assignment after concurrent sets")
	}
}

// --- columnsPerGroup helper ---

func TestColumnsPerGroup(t *testing.T) {
	m := NewCustodySubnetManager(DefaultCustodyConfig())
	cpg := m.columnsPerGroup()
	// 128 / 128 = 1
	if cpg != 1 {
		t.Fatalf("columnsPerGroup = %d, want 1", cpg)
	}

	// Edge case: zero custody groups.
	m2 := NewCustodySubnetManager(CustodyConfig{NumberOfCustodyGroups: 0})
	if m2.columnsPerGroup() != 0 {
		t.Fatal("expected 0 for zero custody groups")
	}
}
