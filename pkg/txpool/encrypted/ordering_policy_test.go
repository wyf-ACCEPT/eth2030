package encrypted

import (
	"math/big"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// helper to create an orderable entry with given timestamp and gas price.
func makeOrderableEntry(timestamp uint64, gasPrice int64) OrderableEntry {
	to := types.HexToAddress("0xabcdef1234567890abcdef1234567890abcdef12")
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(gasPrice),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(0),
	})
	return OrderableEntry{
		Commit: &CommitEntry{
			Commit: &CommitTx{
				Timestamp: timestamp,
			},
		},
		Transaction: tx,
	}
}

// helper to create an orderable entry with EIP-1559 style tip cap.
func makeEIP1559OrderableEntry(timestamp uint64, tipCap int64) OrderableEntry {
	to := types.HexToAddress("0xabcdef1234567890abcdef1234567890abcdef12")
	tx := types.NewTransaction(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     0,
		GasTipCap: big.NewInt(tipCap),
		GasFeeCap: big.NewInt(tipCap * 2),
		Gas:       21000,
		To:        &to,
		Value:     big.NewInt(0),
	})
	return OrderableEntry{
		Commit: &CommitEntry{
			Commit: &CommitTx{
				Timestamp: timestamp,
			},
		},
		Transaction: tx,
	}
}

func TestTimeBasedOrdering(t *testing.T) {
	policy := &TimeBasedOrdering{}
	if policy.Name() != "time-based" {
		t.Errorf("Name() = %q, want %q", policy.Name(), "time-based")
	}

	entries := []OrderableEntry{
		makeOrderableEntry(300, 100),
		makeOrderableEntry(100, 500), // earliest commit, lowest fee
		makeOrderableEntry(200, 300),
	}

	sorted := policy.Order(entries)
	if len(sorted) != 3 {
		t.Fatalf("sorted len = %d, want 3", len(sorted))
	}

	// Should be ordered by timestamp ascending.
	if sorted[0].Commit.Commit.Timestamp != 100 {
		t.Errorf("sorted[0].Timestamp = %d, want 100", sorted[0].Commit.Commit.Timestamp)
	}
	if sorted[1].Commit.Commit.Timestamp != 200 {
		t.Errorf("sorted[1].Timestamp = %d, want 200", sorted[1].Commit.Commit.Timestamp)
	}
	if sorted[2].Commit.Commit.Timestamp != 300 {
		t.Errorf("sorted[2].Timestamp = %d, want 300", sorted[2].Commit.Commit.Timestamp)
	}

	// Original should be unmodified.
	if entries[0].Commit.Commit.Timestamp != 300 {
		t.Error("TimeBasedOrdering modified original slice")
	}
}

func TestTimeBasedOrderingEmpty(t *testing.T) {
	policy := &TimeBasedOrdering{}
	sorted := policy.Order(nil)
	if len(sorted) != 0 {
		t.Errorf("sorted nil len = %d, want 0", len(sorted))
	}
}

func TestFeeBasedOrdering(t *testing.T) {
	policy := &FeeBasedOrdering{}
	if policy.Name() != "fee-based" {
		t.Errorf("Name() = %q, want %q", policy.Name(), "fee-based")
	}

	entries := []OrderableEntry{
		makeOrderableEntry(100, 100), // lowest fee
		makeOrderableEntry(200, 500), // highest fee
		makeOrderableEntry(300, 300), // middle fee
	}

	sorted := policy.Order(entries)
	if len(sorted) != 3 {
		t.Fatalf("sorted len = %d, want 3", len(sorted))
	}

	// Should be ordered by gas price descending (highest first).
	fee0 := effectiveGasPrice(sorted[0].Transaction)
	fee1 := effectiveGasPrice(sorted[1].Transaction)
	fee2 := effectiveGasPrice(sorted[2].Transaction)

	if fee0.Cmp(big.NewInt(500)) != 0 {
		t.Errorf("sorted[0] fee = %v, want 500", fee0)
	}
	if fee1.Cmp(big.NewInt(300)) != 0 {
		t.Errorf("sorted[1] fee = %v, want 300", fee1)
	}
	if fee2.Cmp(big.NewInt(100)) != 0 {
		t.Errorf("sorted[2] fee = %v, want 100", fee2)
	}
}

func TestFeeBasedOrderingEIP1559(t *testing.T) {
	policy := &FeeBasedOrdering{}
	entries := []OrderableEntry{
		makeEIP1559OrderableEntry(100, 10),
		makeEIP1559OrderableEntry(200, 50),
		makeEIP1559OrderableEntry(300, 30),
	}

	sorted := policy.Order(entries)

	// Should be ordered by tip cap descending.
	tip0 := sorted[0].Transaction.GasTipCap()
	tip1 := sorted[1].Transaction.GasTipCap()
	tip2 := sorted[2].Transaction.GasTipCap()

	if tip0.Cmp(big.NewInt(50)) != 0 {
		t.Errorf("sorted[0] tip = %v, want 50", tip0)
	}
	if tip1.Cmp(big.NewInt(30)) != 0 {
		t.Errorf("sorted[1] tip = %v, want 30", tip1)
	}
	if tip2.Cmp(big.NewInt(10)) != 0 {
		t.Errorf("sorted[2] tip = %v, want 10", tip2)
	}
}

func TestHybridOrdering(t *testing.T) {
	// Pure time-based (FeeWeight = 0): should match time-based ordering.
	timePure := &HybridOrdering{FeeWeight: 0.0}
	if timePure.Name() != "hybrid" {
		t.Errorf("Name() = %q, want %q", timePure.Name(), "hybrid")
	}

	entries := []OrderableEntry{
		makeOrderableEntry(300, 500), // late commit, high fee
		makeOrderableEntry(100, 100), // early commit, low fee
		makeOrderableEntry(200, 300), // middle
	}

	sorted := timePure.Order(entries)
	// With FeeWeight=0, should be ordered by timestamp ascending.
	if sorted[0].Commit.Commit.Timestamp != 100 {
		t.Errorf("hybrid(0.0) sorted[0].Timestamp = %d, want 100", sorted[0].Commit.Commit.Timestamp)
	}
	if sorted[2].Commit.Commit.Timestamp != 300 {
		t.Errorf("hybrid(0.0) sorted[2].Timestamp = %d, want 300", sorted[2].Commit.Commit.Timestamp)
	}

	// Pure fee-based (FeeWeight = 1): should match fee-based ordering.
	feePure := &HybridOrdering{FeeWeight: 1.0}
	sorted = feePure.Order(entries)
	fee0 := effectiveGasPrice(sorted[0].Transaction)
	fee2 := effectiveGasPrice(sorted[2].Transaction)
	if fee0.Cmp(big.NewInt(500)) != 0 {
		t.Errorf("hybrid(1.0) sorted[0] fee = %v, want 500", fee0)
	}
	if fee2.Cmp(big.NewInt(100)) != 0 {
		t.Errorf("hybrid(1.0) sorted[2] fee = %v, want 100", fee2)
	}
}

func TestHybridOrderingBalanced(t *testing.T) {
	// With FeeWeight=0.5: earlier commit with slightly lower fee should still win.
	hybrid := &HybridOrdering{FeeWeight: 0.3}

	entries := []OrderableEntry{
		makeOrderableEntry(100, 200), // early, moderate fee
		makeOrderableEntry(300, 500), // late, high fee
	}

	sorted := hybrid.Order(entries)
	// Early commit with moderate fee should rank higher when time has 70% weight.
	if sorted[0].Commit.Commit.Timestamp != 100 {
		t.Errorf("hybrid(0.3): expected early commit first, got timestamp=%d",
			sorted[0].Commit.Commit.Timestamp)
	}
}

func TestHybridOrderingEmpty(t *testing.T) {
	hybrid := &HybridOrdering{FeeWeight: 0.5}
	sorted := hybrid.Order(nil)
	if len(sorted) != 0 {
		t.Errorf("sorted nil len = %d, want 0", len(sorted))
	}
}

func TestHybridOrderingClamp(t *testing.T) {
	// Negative weight should be clamped to 0.
	neg := &HybridOrdering{FeeWeight: -0.5}
	entries := []OrderableEntry{
		makeOrderableEntry(300, 500),
		makeOrderableEntry(100, 100),
	}
	sorted := neg.Order(entries)
	if sorted[0].Commit.Commit.Timestamp != 100 {
		t.Error("negative FeeWeight should behave like 0 (time-based)")
	}

	// Weight > 1 should be clamped to 1.
	over := &HybridOrdering{FeeWeight: 2.0}
	sorted = over.Order(entries)
	fee0 := effectiveGasPrice(sorted[0].Transaction)
	if fee0.Cmp(big.NewInt(500)) != 0 {
		t.Error("FeeWeight > 1 should behave like 1.0 (fee-based)")
	}
}

func TestDecryptionCoordinatorMultiParty(t *testing.T) {
	dc := NewDecryptionCoordinator()

	// Start a 3-of-5 round with 2-second window.
	roundID := dc.StartRound(3, 5, 2*time.Second)

	// Submit 3 shares.
	for i := 1; i <= 3; i++ {
		share := []byte{byte(i), 0xAA, 0xBB}
		if err := dc.SubmitShare(roundID, i, share); err != nil {
			t.Fatalf("SubmitShare(%d): %v", i, err)
		}
	}

	// Check share count.
	count, err := dc.ShareCount(roundID)
	if err != nil {
		t.Fatalf("ShareCount: %v", err)
	}
	if count != 3 {
		t.Errorf("ShareCount = %d, want 3", count)
	}

	// Check threshold met.
	met, err := dc.HasThreshold(roundID)
	if err != nil {
		t.Fatalf("HasThreshold: %v", err)
	}
	if !met {
		t.Error("HasThreshold should be true with 3 shares (threshold=3)")
	}

	// Finalize with result.
	result := []byte("decrypted transaction data")
	if err := dc.FinalizeRound(roundID, result); err != nil {
		t.Fatalf("FinalizeRound: %v", err)
	}

	// Get result.
	got, err := dc.GetRoundResult(roundID)
	if err != nil {
		t.Fatalf("GetRoundResult: %v", err)
	}
	if string(got) != string(result) {
		t.Errorf("GetRoundResult = %q, want %q", got, result)
	}

	// Double finalize should fail.
	if err := dc.FinalizeRound(roundID, result); err != ErrAlreadyFinalized {
		t.Errorf("double FinalizeRound: got %v, want %v", err, ErrAlreadyFinalized)
	}
}

func TestDecryptionCoordinatorInsufficientShares(t *testing.T) {
	dc := NewDecryptionCoordinator()
	roundID := dc.StartRound(3, 5, 2*time.Second)

	// Submit only 2 shares (below threshold).
	dc.SubmitShare(roundID, 1, []byte{0x01})
	dc.SubmitShare(roundID, 2, []byte{0x02})

	met, _ := dc.HasThreshold(roundID)
	if met {
		t.Error("HasThreshold should be false with only 2 shares")
	}

	// Finalize should fail with insufficient shares.
	err := dc.FinalizeRound(roundID, []byte("test"))
	if err != ErrNoShares {
		t.Errorf("FinalizeRound with insufficient shares: got %v, want %v", err, ErrNoShares)
	}
}

func TestDecryptionCoordinatorRoundNotFound(t *testing.T) {
	dc := NewDecryptionCoordinator()

	_, err := dc.ShareCount(999)
	if err != ErrRoundNotFound {
		t.Errorf("ShareCount(999): got %v, want %v", err, ErrRoundNotFound)
	}

	err = dc.SubmitShare(999, 1, []byte{0x01})
	if err != ErrRoundNotFound {
		t.Errorf("SubmitShare(999): got %v, want %v", err, ErrRoundNotFound)
	}

	_, err = dc.HasThreshold(999)
	if err != ErrRoundNotFound {
		t.Errorf("HasThreshold(999): got %v, want %v", err, ErrRoundNotFound)
	}

	err = dc.FinalizeRound(999, nil)
	if err != ErrRoundNotFound {
		t.Errorf("FinalizeRound(999): got %v, want %v", err, ErrRoundNotFound)
	}

	_, err = dc.GetRoundResult(999)
	if err != ErrRoundNotFound {
		t.Errorf("GetRoundResult(999): got %v, want %v", err, ErrRoundNotFound)
	}
}

func TestRevealWindowTiming(t *testing.T) {
	now := time.Now()
	rw := &RevealWindow{
		Start:    now,
		Duration: 100 * time.Millisecond,
	}

	// Window should be open now.
	if !rw.IsOpen(now) {
		t.Error("window should be open at start time")
	}
	if rw.IsClosed(now) {
		t.Error("window should not be closed at start time")
	}

	// Window should be open just before deadline.
	justBefore := now.Add(99 * time.Millisecond)
	if !rw.IsOpen(justBefore) {
		t.Error("window should be open just before deadline")
	}

	// Window should be closed after duration.
	after := now.Add(101 * time.Millisecond)
	if rw.IsOpen(after) {
		t.Error("window should not be open after duration")
	}
	if !rw.IsClosed(after) {
		t.Error("window should be closed after duration")
	}

	// Before start.
	before := now.Add(-1 * time.Millisecond)
	if rw.IsOpen(before) {
		t.Error("window should not be open before start")
	}
}

func TestDecryptionCoordinatorGetShares(t *testing.T) {
	dc := NewDecryptionCoordinator()
	roundID := dc.StartRound(2, 3, 5*time.Second)

	dc.SubmitShare(roundID, 1, []byte{0xAA})
	dc.SubmitShare(roundID, 2, []byte{0xBB})

	shares, err := dc.GetRoundShares(roundID)
	if err != nil {
		t.Fatalf("GetRoundShares: %v", err)
	}
	if len(shares) != 2 {
		t.Fatalf("got %d shares, want 2", len(shares))
	}
	if shares[1][0] != 0xAA {
		t.Errorf("share[1] = %x, want AA", shares[1])
	}
	if shares[2][0] != 0xBB {
		t.Errorf("share[2] = %x, want BB", shares[2])
	}
}

func TestDecryptionCoordinatorMultipleRounds(t *testing.T) {
	dc := NewDecryptionCoordinator()

	r1 := dc.StartRound(2, 3, 5*time.Second)
	r2 := dc.StartRound(3, 5, 5*time.Second)

	if r1 == r2 {
		t.Error("round IDs should be unique")
	}

	// Submit to different rounds.
	dc.SubmitShare(r1, 1, []byte{0x01})
	dc.SubmitShare(r2, 1, []byte{0x02})

	c1, _ := dc.ShareCount(r1)
	c2, _ := dc.ShareCount(r2)
	if c1 != 1 || c2 != 1 {
		t.Errorf("share counts = (%d, %d), want (1, 1)", c1, c2)
	}
}

func TestRevealWindowClosedSubmit(t *testing.T) {
	dc := NewDecryptionCoordinator()
	// Window of 1 millisecond - will close almost immediately.
	roundID := dc.StartRound(1, 1, 1*time.Millisecond)

	// Wait for window to close.
	time.Sleep(5 * time.Millisecond)

	err := dc.SubmitShare(roundID, 1, []byte{0x01})
	if err != ErrRevealWindowClosed {
		t.Errorf("SubmitShare after window: got %v, want %v", err, ErrRevealWindowClosed)
	}
}

func TestGetRoundResultNotFinalized(t *testing.T) {
	dc := NewDecryptionCoordinator()
	roundID := dc.StartRound(1, 1, 5*time.Second)

	_, err := dc.GetRoundResult(roundID)
	if err != ErrRevealWindowOpen {
		t.Errorf("GetRoundResult before finalize: got %v, want %v", err, ErrRevealWindowOpen)
	}
}
