package core

import (
	"sync"
	"testing"
)

func TestMultiGasPoolBasic(t *testing.T) {
	pool := NewMultiGasPool(30_000_000, 7_500_000, 786432)

	exec, cd, blob := pool.Capacity()
	if exec != 30_000_000 || cd != 7_500_000 || blob != 786432 {
		t.Fatalf("unexpected capacity: exec=%d, cd=%d, blob=%d", exec, cd, blob)
	}

	// Subtract gas across all dimensions.
	if err := pool.SubGasMulti(21000, 1000, 131072); err != nil {
		t.Fatalf("SubGasMulti failed: %v", err)
	}

	usedExec, usedCD, usedBlob := pool.Used()
	if usedExec != 21000 || usedCD != 1000 || usedBlob != 131072 {
		t.Fatalf("unexpected usage: exec=%d, cd=%d, blob=%d", usedExec, usedCD, usedBlob)
	}

	availExec, availCD, availBlob := pool.Available()
	if availExec != 30_000_000-21000 || availCD != 7_500_000-1000 || availBlob != 786432-131072 {
		t.Fatalf("unexpected available: exec=%d, cd=%d, blob=%d", availExec, availCD, availBlob)
	}
}

func TestMultiGasPoolExhausted(t *testing.T) {
	pool := NewMultiGasPool(100, 50, 30)

	// Execution gas exhaustion.
	err := pool.SubGasMulti(101, 0, 0)
	if err == nil {
		t.Fatal("expected execution gas exhaustion error")
	}

	// Calldata gas exhaustion.
	err = pool.SubGasMulti(0, 51, 0)
	if err == nil {
		t.Fatal("expected calldata gas exhaustion error")
	}

	// Blob gas exhaustion.
	err = pool.SubGasMulti(0, 0, 31)
	if err == nil {
		t.Fatal("expected blob gas exhaustion error")
	}

	// No gas should have been consumed (all-or-nothing).
	usedExec, usedCD, usedBlob := pool.Used()
	if usedExec != 0 || usedCD != 0 || usedBlob != 0 {
		t.Fatalf("no gas should have been consumed: exec=%d, cd=%d, blob=%d", usedExec, usedCD, usedBlob)
	}
}

func TestMultiGasPoolAllOrNothing(t *testing.T) {
	pool := NewMultiGasPool(100, 50, 30)

	// Execution fits, calldata does not. Nothing should be consumed.
	err := pool.SubGasMulti(50, 60, 10)
	if err == nil {
		t.Fatal("expected calldata gas exhaustion error")
	}

	usedExec, usedCD, usedBlob := pool.Used()
	if usedExec != 0 || usedCD != 0 || usedBlob != 0 {
		t.Fatalf("all-or-nothing violated: exec=%d, cd=%d, blob=%d", usedExec, usedCD, usedBlob)
	}
}

func TestMultiGasPoolAddGas(t *testing.T) {
	pool := NewMultiGasPool(1000, 500, 300)
	_ = pool.SubGasMulti(600, 200, 100)

	pool.AddGasMulti(100, 50, 25)
	usedExec, usedCD, usedBlob := pool.Used()
	if usedExec != 500 || usedCD != 150 || usedBlob != 75 {
		t.Fatalf("unexpected usage after refund: exec=%d, cd=%d, blob=%d", usedExec, usedCD, usedBlob)
	}

	// Over-refund should clamp to zero used.
	pool.AddGasMulti(1000, 1000, 1000)
	usedExec, usedCD, usedBlob = pool.Used()
	if usedExec != 0 || usedCD != 0 || usedBlob != 0 {
		t.Fatalf("over-refund should clamp to zero: exec=%d, cd=%d, blob=%d", usedExec, usedCD, usedBlob)
	}
}

func TestMultiGasPoolReservation(t *testing.T) {
	pool := NewMultiGasPool(1000, 500, 300)

	// Create reservation.
	id, err := pool.Reserve(200, 100, 50)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	if pool.ReservationCount() != 1 {
		t.Fatalf("expected 1 reservation, got %d", pool.ReservationCount())
	}

	// Check available gas decreased.
	avail, _, _ := pool.Available()
	if avail != 800 {
		t.Fatalf("expected 800 available execution gas, got %d", avail)
	}

	// Retrieve reservation.
	r, ok := pool.GetReservation(id)
	if !ok {
		t.Fatal("reservation not found")
	}
	if r.Execution != 200 || r.Calldata != 100 || r.Blob != 50 {
		t.Fatalf("unexpected reservation: %+v", r)
	}
	if r.Total() != 350 {
		t.Fatalf("expected total 350, got %d", r.Total())
	}

	// Commit reservation.
	if err := pool.Commit(id); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
	if pool.ReservationCount() != 0 {
		t.Fatalf("expected 0 reservations after commit, got %d", pool.ReservationCount())
	}

	// Gas should still be consumed.
	usedExec, _, _ := pool.Used()
	if usedExec != 200 {
		t.Fatalf("expected 200 used execution gas after commit, got %d", usedExec)
	}
}

func TestMultiGasPoolRelease(t *testing.T) {
	pool := NewMultiGasPool(1000, 500, 300)

	id, err := pool.Reserve(200, 100, 50)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}

	// Release reservation should return gas.
	if err := pool.Release(id); err != nil {
		t.Fatalf("Release failed: %v", err)
	}

	usedExec, usedCD, usedBlob := pool.Used()
	if usedExec != 0 || usedCD != 0 || usedBlob != 0 {
		t.Fatalf("gas should be returned after release: exec=%d, cd=%d, blob=%d", usedExec, usedCD, usedBlob)
	}

	// Double release should fail.
	if err := pool.Release(id); err == nil {
		t.Fatal("expected error on double release")
	}
}

func TestMultiGasPoolPartialCommit(t *testing.T) {
	pool := NewMultiGasPool(1000, 500, 300)

	id, err := pool.Reserve(200, 100, 50)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}

	// Actually used less than reserved.
	if err := pool.PartialCommit(id, 150, 80, 30); err != nil {
		t.Fatalf("PartialCommit failed: %v", err)
	}

	// Only actual usage should remain.
	usedExec, usedCD, usedBlob := pool.Used()
	if usedExec != 150 || usedCD != 80 || usedBlob != 30 {
		t.Fatalf("expected partial usage: exec=%d, cd=%d, blob=%d", usedExec, usedCD, usedBlob)
	}
}

func TestMultiGasPoolPeakUsage(t *testing.T) {
	pool := NewMultiGasPool(1000, 500, 300)

	_ = pool.SubGasMulti(600, 300, 200)
	pool.AddGasMulti(400, 200, 100)
	_ = pool.SubGasMulti(300, 150, 80)

	peakExec, peakCD, peakBlob := pool.PeakUsage()
	if peakExec != 600 || peakCD != 300 || peakBlob != 200 {
		t.Fatalf("unexpected peak usage: exec=%d, cd=%d, blob=%d", peakExec, peakCD, peakBlob)
	}
}

func TestMultiGasPoolUtilization(t *testing.T) {
	pool := NewMultiGasPool(1000, 500, 200)
	_ = pool.SubGasMulti(500, 250, 100)

	execUtil, cdUtil, blobUtil := pool.Utilization()
	if execUtil != 0.5 || cdUtil != 0.5 || blobUtil != 0.5 {
		t.Fatalf("expected 50%% utilization: exec=%f, cd=%f, blob=%f", execUtil, cdUtil, blobUtil)
	}
}

func TestMultiGasPoolReset(t *testing.T) {
	pool := NewMultiGasPool(1000, 500, 300)
	_ = pool.SubGasMulti(500, 250, 150)
	_, _ = pool.Reserve(100, 50, 25)

	pool.Reset(2000, 1000, 600)

	usedExec, usedCD, usedBlob := pool.Used()
	if usedExec != 0 || usedCD != 0 || usedBlob != 0 {
		t.Fatal("usage should be zero after reset")
	}
	exec, cd, blob := pool.Capacity()
	if exec != 2000 || cd != 1000 || blob != 600 {
		t.Fatal("capacity should reflect new values after reset")
	}
	if pool.ReservationCount() != 0 {
		t.Fatal("reservations should be cleared after reset")
	}
}

func TestMultiGasPoolConcurrent(t *testing.T) {
	pool := NewMultiGasPool(1_000_000, 500_000, 300_000)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = pool.SubGasMulti(100, 50, 30)
			pool.AddGasMulti(50, 25, 15)
		}()
	}
	wg.Wait()

	// After 100 goroutines each adding net 50/25/15, totals should be 5000/2500/1500.
	usedExec, usedCD, usedBlob := pool.Used()
	if usedExec != 5000 || usedCD != 2500 || usedBlob != 1500 {
		t.Fatalf("concurrent usage mismatch: exec=%d, cd=%d, blob=%d", usedExec, usedCD, usedBlob)
	}
}

func TestMultiGasPoolFromDimensions(t *testing.T) {
	dims := GasDimensions{Compute: 30_000_000, Calldata: 7_500_000, Blob: 786432}
	pool := NewMultiGasPoolFromDimensions(dims)

	exec, cd, blob := pool.Capacity()
	if exec != 30_000_000 || cd != 7_500_000 || blob != 786432 {
		t.Fatalf("unexpected capacity from dimensions: exec=%d, cd=%d, blob=%d", exec, cd, blob)
	}
}

func TestPoolGasDimString(t *testing.T) {
	tests := []struct {
		dim  PoolGasDim
		want string
	}{
		{PoolDimExecution, "execution"},
		{PoolDimCalldata, "calldata"},
		{PoolDimBlob, "blob"},
		{PoolGasDim(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.dim.String(); got != tt.want {
			t.Errorf("PoolGasDim(%d).String() = %q, want %q", tt.dim, got, tt.want)
		}
	}
}

func TestMultiGasPoolReservationNotFound(t *testing.T) {
	pool := NewMultiGasPool(1000, 500, 300)

	if err := pool.Commit(999); err == nil {
		t.Fatal("expected error for non-existent reservation commit")
	}
	if err := pool.Release(999); err == nil {
		t.Fatal("expected error for non-existent reservation release")
	}
	if err := pool.PartialCommit(999, 0, 0, 0); err == nil {
		t.Fatal("expected error for non-existent reservation partial commit")
	}
}

func TestMultiGasPoolReserveInsufficientGas(t *testing.T) {
	pool := NewMultiGasPool(100, 50, 30)

	_, err := pool.Reserve(200, 100, 50)
	if err == nil {
		t.Fatal("expected error when reserving more than available")
	}

	// Pool should remain unchanged.
	usedExec, usedCD, usedBlob := pool.Used()
	if usedExec != 0 || usedCD != 0 || usedBlob != 0 {
		t.Fatalf("failed reservation should not consume gas: exec=%d, cd=%d, blob=%d", usedExec, usedCD, usedBlob)
	}
}

func TestMultiGasPoolMultipleReservations(t *testing.T) {
	pool := NewMultiGasPool(1000, 500, 300)

	id1, err := pool.Reserve(200, 100, 50)
	if err != nil {
		t.Fatalf("Reserve 1 failed: %v", err)
	}
	id2, err := pool.Reserve(300, 150, 75)
	if err != nil {
		t.Fatalf("Reserve 2 failed: %v", err)
	}

	if pool.ReservationCount() != 2 {
		t.Fatalf("expected 2 reservations, got %d", pool.ReservationCount())
	}

	// Commit first, release second.
	if err := pool.Commit(id1); err != nil {
		t.Fatalf("Commit id1 failed: %v", err)
	}
	if err := pool.Release(id2); err != nil {
		t.Fatalf("Release id2 failed: %v", err)
	}

	// Only first reservation's gas should be consumed.
	usedExec, usedCD, usedBlob := pool.Used()
	if usedExec != 200 || usedCD != 100 || usedBlob != 50 {
		t.Fatalf("expected only first reservation consumed: exec=%d, cd=%d, blob=%d", usedExec, usedCD, usedBlob)
	}
}
