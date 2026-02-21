package vm

import (
	"errors"
	"testing"
)

func TestGasPoolTrackerNew(t *testing.T) {
	tracker := NewGasPoolTracker(1000, 500, 200)
	if tracker.Limit(DimExecution) != 1000 {
		t.Fatalf("expected execution limit 1000, got %d", tracker.Limit(DimExecution))
	}
	if tracker.Limit(DimCalldata) != 500 {
		t.Fatalf("expected calldata limit 500, got %d", tracker.Limit(DimCalldata))
	}
	if tracker.Limit(DimBlob) != 200 {
		t.Fatalf("expected blob limit 200, got %d", tracker.Limit(DimBlob))
	}
	if tracker.TotalGasUsed() != 0 {
		t.Fatalf("expected zero total gas used, got %d", tracker.TotalGasUsed())
	}
}

func TestGasPoolTrackerNewUniform(t *testing.T) {
	tracker := NewGasPoolTrackerUniform(777)
	for _, dim := range []GasDimension{DimExecution, DimCalldata, DimBlob} {
		if tracker.Limit(dim) != 777 {
			t.Fatalf("expected limit 777 for %s, got %d", dim, tracker.Limit(dim))
		}
	}
}

func TestGasPoolTrackerSubGas(t *testing.T) {
	tracker := NewGasPoolTracker(1000, 500, 200)

	if err := tracker.SubGas(DimExecution, 300); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tracker.GasUsed(DimExecution) != 300 {
		t.Fatalf("expected 300 used, got %d", tracker.GasUsed(DimExecution))
	}
	if tracker.GasRemaining(DimExecution) != 700 {
		t.Fatalf("expected 700 remaining, got %d", tracker.GasRemaining(DimExecution))
	}
}

func TestGasPoolTrackerSubGasOutOfGas(t *testing.T) {
	tracker := NewGasPoolTracker(100, 100, 100)

	if err := tracker.SubGas(DimExecution, 101); err == nil {
		t.Fatal("expected out of gas error")
	} else if !errors.Is(err, ErrGasPoolOutOfGas) {
		t.Fatalf("expected ErrGasPoolOutOfGas, got %v", err)
	}
	// Ensure no gas was consumed.
	if tracker.GasUsed(DimExecution) != 0 {
		t.Fatalf("expected 0 used after failed sub, got %d", tracker.GasUsed(DimExecution))
	}
}

func TestGasPoolTrackerSubGasInvalidDim(t *testing.T) {
	tracker := NewGasPoolTracker(100, 100, 100)
	if err := tracker.SubGas(GasDimension(99), 10); err == nil {
		t.Fatal("expected error for invalid dimension")
	} else if !errors.Is(err, ErrGasPoolInvalidDim) {
		t.Fatalf("expected ErrGasPoolInvalidDim, got %v", err)
	}
}

func TestGasPoolTrackerSubGasMulti(t *testing.T) {
	tracker := NewGasPoolTracker(1000, 500, 200)

	if err := tracker.SubGasMulti(100, 50, 20); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tracker.GasUsed(DimExecution) != 100 {
		t.Fatalf("expected 100, got %d", tracker.GasUsed(DimExecution))
	}
	if tracker.GasUsed(DimCalldata) != 50 {
		t.Fatalf("expected 50, got %d", tracker.GasUsed(DimCalldata))
	}
	if tracker.GasUsed(DimBlob) != 20 {
		t.Fatalf("expected 20, got %d", tracker.GasUsed(DimBlob))
	}
}

func TestGasPoolTrackerSubGasMultiAtomicFailure(t *testing.T) {
	tracker := NewGasPoolTracker(1000, 500, 200)

	// Calldata dimension is too small.
	if err := tracker.SubGasMulti(100, 501, 20); err == nil {
		t.Fatal("expected out of gas error for calldata")
	}
	// Verify no gas was consumed in any dimension (atomicity).
	if tracker.GasUsed(DimExecution) != 0 {
		t.Fatalf("expected 0 execution after atomic failure, got %d", tracker.GasUsed(DimExecution))
	}
	if tracker.GasUsed(DimCalldata) != 0 {
		t.Fatalf("expected 0 calldata after atomic failure, got %d", tracker.GasUsed(DimCalldata))
	}
	if tracker.GasUsed(DimBlob) != 0 {
		t.Fatalf("expected 0 blob after atomic failure, got %d", tracker.GasUsed(DimBlob))
	}
}

func TestGasPoolTrackerAddGas(t *testing.T) {
	tracker := NewGasPoolTracker(1000, 500, 200)
	_ = tracker.SubGas(DimExecution, 500)

	tracker.AddGas(DimExecution, 200)
	if tracker.GasUsed(DimExecution) != 300 {
		t.Fatalf("expected 300 after refund, got %d", tracker.GasUsed(DimExecution))
	}
}

func TestGasPoolTrackerAddGasUnderflow(t *testing.T) {
	tracker := NewGasPoolTracker(1000, 500, 200)
	_ = tracker.SubGas(DimExecution, 50)

	// Refund more than used should clamp to zero.
	tracker.AddGas(DimExecution, 100)
	if tracker.GasUsed(DimExecution) != 0 {
		t.Fatalf("expected 0 after over-refund, got %d", tracker.GasUsed(DimExecution))
	}
}

func TestGasPoolTrackerTotalGasUsed(t *testing.T) {
	tracker := NewGasPoolTracker(1000, 500, 200)
	_ = tracker.SubGas(DimExecution, 100)
	_ = tracker.SubGas(DimCalldata, 50)
	_ = tracker.SubGas(DimBlob, 25)

	if tracker.TotalGasUsed() != 175 {
		t.Fatalf("expected total 175, got %d", tracker.TotalGasUsed())
	}
}

func TestGasPoolTrackerSetLimit(t *testing.T) {
	tracker := NewGasPoolTracker(100, 100, 100)

	if err := tracker.SetLimit(DimExecution, 500); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tracker.Limit(DimExecution) != 500 {
		t.Fatalf("expected limit 500, got %d", tracker.Limit(DimExecution))
	}

	// Invalid dimension.
	if err := tracker.SetLimit(GasDimension(99), 500); err == nil {
		t.Fatal("expected error for invalid dimension")
	}
}

func TestGasPoolTrackerSnapshot(t *testing.T) {
	tracker := NewGasPoolTracker(1000, 500, 200)
	_ = tracker.SubGas(DimExecution, 100)

	snap := tracker.Snapshot()
	if !snap.Valid() {
		t.Fatal("snapshot should be valid")
	}

	// Use more gas after snapshot.
	_ = tracker.SubGas(DimExecution, 200)
	if tracker.GasUsed(DimExecution) != 300 {
		t.Fatalf("expected 300, got %d", tracker.GasUsed(DimExecution))
	}

	// Revert to snapshot.
	if err := tracker.Revert(snap); err != nil {
		t.Fatalf("unexpected revert error: %v", err)
	}
	if tracker.GasUsed(DimExecution) != 100 {
		t.Fatalf("expected 100 after revert, got %d", tracker.GasUsed(DimExecution))
	}
}

func TestGasPoolTrackerNestedSnapshots(t *testing.T) {
	tracker := NewGasPoolTracker(1000, 500, 200)

	_ = tracker.SubGas(DimExecution, 50)
	snap1 := tracker.Snapshot()

	_ = tracker.SubGas(DimExecution, 100)
	snap2 := tracker.Snapshot()

	_ = tracker.SubGas(DimExecution, 150)

	// Current state: 50 + 100 + 150 = 300 used.
	if tracker.GasUsed(DimExecution) != 300 {
		t.Fatalf("expected 300, got %d", tracker.GasUsed(DimExecution))
	}

	// Revert to snap2: should go back to 150.
	if err := tracker.Revert(snap2); err != nil {
		t.Fatalf("revert snap2 error: %v", err)
	}
	if tracker.GasUsed(DimExecution) != 150 {
		t.Fatalf("expected 150 after snap2 revert, got %d", tracker.GasUsed(DimExecution))
	}

	// snap2 reverted snap1 should still be revertible.
	if err := tracker.Revert(snap1); err != nil {
		t.Fatalf("revert snap1 error: %v", err)
	}
	if tracker.GasUsed(DimExecution) != 50 {
		t.Fatalf("expected 50 after snap1 revert, got %d", tracker.GasUsed(DimExecution))
	}
}

func TestGasPoolTrackerRevertInvalidSnapshot(t *testing.T) {
	tracker := NewGasPoolTracker(1000, 500, 200)

	// Create and revert a snapshot.
	snap := tracker.Snapshot()
	if err := tracker.Revert(snap); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Second revert should fail (snapshot stack was trimmed).
	if err := tracker.Revert(snap); err == nil {
		t.Fatal("expected error on double revert")
	}
}

func TestGasPoolTrackerMergeUsage(t *testing.T) {
	parent := NewGasPoolTracker(1000, 500, 200)
	_ = parent.SubGas(DimExecution, 100)

	child := NewGasPoolTracker(500, 250, 100)
	_ = child.SubGas(DimExecution, 200)
	_ = child.SubGas(DimCalldata, 50)

	if err := parent.MergeUsage(child); err != nil {
		t.Fatalf("unexpected merge error: %v", err)
	}
	if parent.GasUsed(DimExecution) != 300 {
		t.Fatalf("expected 300 exec after merge, got %d", parent.GasUsed(DimExecution))
	}
	if parent.GasUsed(DimCalldata) != 50 {
		t.Fatalf("expected 50 calldata after merge, got %d", parent.GasUsed(DimCalldata))
	}
}

func TestGasPoolTrackerMergeUsageOverflow(t *testing.T) {
	parent := NewGasPoolTracker(100, 100, 100)
	_ = parent.SubGas(DimExecution, 50)

	child := NewGasPoolTracker(200, 200, 200)
	_ = child.SubGas(DimExecution, 60) // 50 + 60 = 110 > parent limit 100

	if err := parent.MergeUsage(child); err == nil {
		t.Fatal("expected merge overflow error")
	}
	// Parent state should not have changed.
	if parent.GasUsed(DimExecution) != 50 {
		t.Fatalf("expected 50 unchanged after failed merge, got %d", parent.GasUsed(DimExecution))
	}
}

func TestGasPoolTrackerMergeNil(t *testing.T) {
	parent := NewGasPoolTracker(1000, 500, 200)
	if err := parent.MergeUsage(nil); err != nil {
		t.Fatalf("unexpected error merging nil: %v", err)
	}
}

func TestGasPoolTrackerApplyChildFrame(t *testing.T) {
	parent := NewGasPoolTracker(6400, 3200, 1600)
	_ = parent.SubGas(DimExecution, 400) // 6000 remaining

	child := parent.ApplyChildFrame(0)

	// EIP-150: child gets floor(6000 * 63 / 64) = 5906 for execution.
	execRemaining := uint64(6000)
	expectedChild := execRemaining - execRemaining/64
	if child.Limit(DimExecution) != expectedChild {
		t.Fatalf("expected child exec limit %d, got %d", expectedChild, child.Limit(DimExecution))
	}

	// Calldata: floor(3200 * 63 / 64) = 3150.
	cdRemaining := uint64(3200)
	expectedCD := cdRemaining - cdRemaining/64
	if child.Limit(DimCalldata) != expectedCD {
		t.Fatalf("expected child calldata limit %d, got %d", expectedCD, child.Limit(DimCalldata))
	}
}

func TestGasPoolTrackerApplyChildFrameWithStipend(t *testing.T) {
	parent := NewGasPoolTracker(6400, 3200, 1600)

	child := parent.ApplyChildFrame(2300) // CallStipend
	// Child execution: floor(6400 * 63 / 64) + 2300
	execRemaining := uint64(6400)
	base := execRemaining - execRemaining/64
	expected := base + 2300
	if child.Limit(DimExecution) != expected {
		t.Fatalf("expected child exec limit %d with stipend, got %d", expected, child.Limit(DimExecution))
	}
}

func TestGasPoolTrackerUtilization(t *testing.T) {
	tracker := NewGasPoolTracker(1000, 500, 200)
	_ = tracker.SubGas(DimExecution, 500)

	util := tracker.Utilization(DimExecution)
	if util < 0.499 || util > 0.501 {
		t.Fatalf("expected ~0.5 utilization, got %f", util)
	}

	// Zero limit dimension.
	zeroTracker := NewGasPoolTracker(0, 100, 100)
	if zeroTracker.Utilization(DimExecution) != 0 {
		t.Fatal("expected 0 utilization for zero-limit dimension")
	}
}

func TestGasPoolTrackerPeakUsed(t *testing.T) {
	tracker := NewGasPoolTracker(1000, 500, 200)
	_ = tracker.SubGas(DimExecution, 500)
	tracker.AddGas(DimExecution, 300) // 200 used now

	if tracker.PeakUsed(DimExecution) != 500 {
		t.Fatalf("expected peak 500, got %d", tracker.PeakUsed(DimExecution))
	}
	if tracker.GasUsed(DimExecution) != 200 {
		t.Fatalf("expected current 200, got %d", tracker.GasUsed(DimExecution))
	}
}

func TestGasPoolTrackerIsExhausted(t *testing.T) {
	tracker := NewGasPoolTracker(100, 100, 100)
	if tracker.IsExhausted() {
		t.Fatal("should not be exhausted initially")
	}

	_ = tracker.SubGas(DimExecution, 100)
	if !tracker.IsExhausted() {
		t.Fatal("should be exhausted after using all execution gas")
	}
}

func TestGasPoolTrackerCopy(t *testing.T) {
	tracker := NewGasPoolTracker(1000, 500, 200)
	_ = tracker.SubGas(DimExecution, 300)
	_ = tracker.Snapshot()

	cp := tracker.Copy()
	if cp.GasUsed(DimExecution) != 300 {
		t.Fatalf("copy should have 300 exec used, got %d", cp.GasUsed(DimExecution))
	}
	if cp.SnapshotCount() != 1 {
		t.Fatalf("copy should have 1 snapshot, got %d", cp.SnapshotCount())
	}

	// Mutate original; copy should be unaffected.
	_ = tracker.SubGas(DimExecution, 100)
	if cp.GasUsed(DimExecution) != 300 {
		t.Fatalf("copy should still have 300 after original mutation, got %d", cp.GasUsed(DimExecution))
	}
}

func TestGasPoolTrackerReset(t *testing.T) {
	tracker := NewGasPoolTracker(1000, 500, 200)
	_ = tracker.SubGas(DimExecution, 500)
	_ = tracker.Snapshot()

	tracker.Reset(2000, 0, 0)
	if tracker.GasUsed(DimExecution) != 0 {
		t.Fatalf("expected 0 after reset, got %d", tracker.GasUsed(DimExecution))
	}
	if tracker.Limit(DimExecution) != 2000 {
		t.Fatalf("expected new limit 2000, got %d", tracker.Limit(DimExecution))
	}
	// Calldata limit unchanged (passed 0).
	if tracker.Limit(DimCalldata) != 500 {
		t.Fatalf("expected calldata limit 500 unchanged, got %d", tracker.Limit(DimCalldata))
	}
	if tracker.SnapshotCount() != 0 {
		t.Fatalf("expected 0 snapshots after reset, got %d", tracker.SnapshotCount())
	}
}

func TestGasPoolTrackerVectors(t *testing.T) {
	tracker := NewGasPoolTracker(1000, 500, 200)
	_ = tracker.SubGas(DimExecution, 100)
	_ = tracker.SubGas(DimCalldata, 50)
	_ = tracker.SubGas(DimBlob, 25)

	usage := tracker.UsageVector()
	if usage != [3]uint64{100, 50, 25} {
		t.Fatalf("unexpected usage vector: %v", usage)
	}
	limits := tracker.LimitVector()
	if limits != [3]uint64{1000, 500, 200} {
		t.Fatalf("unexpected limit vector: %v", limits)
	}
	remaining := tracker.RemainingVector()
	if remaining != [3]uint64{900, 450, 175} {
		t.Fatalf("unexpected remaining vector: %v", remaining)
	}
}

func TestGasPoolTrackerString(t *testing.T) {
	tracker := NewGasPoolTracker(1000, 500, 200)
	_ = tracker.SubGas(DimExecution, 100)
	s := tracker.String()
	if s == "" {
		t.Fatal("expected non-empty string representation")
	}
	// Verify it contains key info.
	expected := "GasPoolTracker{exec:100/1000, calldata:0/500, blob:0/200}"
	if s != expected {
		t.Fatalf("expected %q, got %q", expected, s)
	}
}

func TestGasDimensionString(t *testing.T) {
	tests := []struct {
		dim  GasDimension
		want string
	}{
		{DimExecution, "execution"},
		{DimCalldata, "calldata"},
		{DimBlob, "blob"},
		{GasDimension(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.dim.String(); got != tt.want {
			t.Errorf("GasDimension(%d).String() = %q, want %q", tt.dim, got, tt.want)
		}
	}
}

func TestGasDimensionValid(t *testing.T) {
	if !DimExecution.Valid() {
		t.Fatal("DimExecution should be valid")
	}
	if !DimCalldata.Valid() {
		t.Fatal("DimCalldata should be valid")
	}
	if !DimBlob.Valid() {
		t.Fatal("DimBlob should be valid")
	}
	if GasDimension(3).Valid() {
		t.Fatal("dimension 3 should be invalid")
	}
}

func TestGasPoolTrackerTotalRemaining(t *testing.T) {
	tracker := NewGasPoolTracker(1000, 500, 200)
	_ = tracker.SubGas(DimExecution, 100)

	total := tracker.TotalGasRemaining()
	// 900 + 500 + 200 = 1600
	if total != 1600 {
		t.Fatalf("expected 1600 total remaining, got %d", total)
	}
}

func TestGasPoolTrackerTotalLimit(t *testing.T) {
	tracker := NewGasPoolTracker(1000, 500, 200)
	if tracker.TotalLimit() != 1700 {
		t.Fatalf("expected total limit 1700, got %d", tracker.TotalLimit())
	}
}

func TestGasPoolTrackerExhaustExactly(t *testing.T) {
	tracker := NewGasPoolTracker(100, 100, 100)
	// Use exact limit.
	if err := tracker.SubGas(DimExecution, 100); err != nil {
		t.Fatalf("should succeed at exact limit: %v", err)
	}
	if tracker.GasRemaining(DimExecution) != 0 {
		t.Fatalf("expected 0 remaining, got %d", tracker.GasRemaining(DimExecution))
	}
	// One more unit should fail.
	if err := tracker.SubGas(DimExecution, 1); err == nil {
		t.Fatal("expected out of gas after exhaustion")
	}
}

func TestGasPoolTrackerSnapshotAcrossDimensions(t *testing.T) {
	tracker := NewGasPoolTracker(1000, 500, 200)
	_ = tracker.SubGas(DimExecution, 100)
	_ = tracker.SubGas(DimCalldata, 50)
	_ = tracker.SubGas(DimBlob, 25)

	snap := tracker.Snapshot()

	_ = tracker.SubGas(DimExecution, 200)
	_ = tracker.SubGas(DimCalldata, 100)
	_ = tracker.SubGas(DimBlob, 50)

	// Revert should restore all dimensions.
	if err := tracker.Revert(snap); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tracker.GasUsed(DimExecution) != 100 {
		t.Fatalf("expected 100 exec, got %d", tracker.GasUsed(DimExecution))
	}
	if tracker.GasUsed(DimCalldata) != 50 {
		t.Fatalf("expected 50 calldata, got %d", tracker.GasUsed(DimCalldata))
	}
	if tracker.GasUsed(DimBlob) != 25 {
		t.Fatalf("expected 25 blob, got %d", tracker.GasUsed(DimBlob))
	}
}

func TestGasPoolTrackerInvalidDimReturnsZero(t *testing.T) {
	tracker := NewGasPoolTracker(100, 100, 100)
	invalid := GasDimension(42)

	if tracker.GasUsed(invalid) != 0 {
		t.Fatal("expected 0 for invalid dim GasUsed")
	}
	if tracker.GasRemaining(invalid) != 0 {
		t.Fatal("expected 0 for invalid dim GasRemaining")
	}
	if tracker.Limit(invalid) != 0 {
		t.Fatal("expected 0 for invalid dim Limit")
	}
	if tracker.PeakUsed(invalid) != 0 {
		t.Fatal("expected 0 for invalid dim PeakUsed")
	}
	if tracker.Utilization(invalid) != 0 {
		t.Fatal("expected 0 for invalid dim Utilization")
	}
}
