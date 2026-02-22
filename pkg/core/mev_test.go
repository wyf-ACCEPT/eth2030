package core

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// --- Test helpers ---

func mevTestTx(nonce uint64, gasPrice int64, to types.Address, sender types.Address) *types.Transaction {
	toAddr := to
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(gasPrice),
		Gas:      21000,
		To:       &toAddr,
		Value:    big.NewInt(0),
		Data:     nil,
	})
	tx.SetSender(sender)
	return tx
}

func mevTestDynamicTx(nonce uint64, tipCap, feeCap int64, to, sender types.Address) *types.Transaction {
	toAddr := to
	tx := types.NewTransaction(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     nonce,
		GasTipCap: big.NewInt(tipCap),
		GasFeeCap: big.NewInt(feeCap),
		Gas:       21000,
		To:        &toAddr,
		Value:     big.NewInt(0),
	})
	tx.SetSender(sender)
	return tx
}

// --- FlashbotsBundle ---

func TestBundleValidate(t *testing.T) {
	to := types.HexToAddress("0xdead")

	// Valid bundle.
	b := &FlashbotsBundle{
		Transactions: []*types.Transaction{
			mevTestTx(0, 1000, to, types.HexToAddress("0x01")),
		},
		BlockNumber: 100,
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	// Empty bundle.
	empty := &FlashbotsBundle{}
	if err := empty.Validate(); err != ErrEmptyBundle {
		t.Errorf("empty bundle: got %v, want %v", err, ErrEmptyBundle)
	}

	// Too large bundle.
	txs := make([]*types.Transaction, MaxBundleSize+1)
	for i := range txs {
		txs[i] = mevTestTx(uint64(i), 1000, to, types.HexToAddress("0x01"))
	}
	large := &FlashbotsBundle{Transactions: txs}
	if err := large.Validate(); err != ErrBundleTooLarge {
		t.Errorf("large bundle: got %v, want %v", err, ErrBundleTooLarge)
	}

	// Invalid timestamps.
	bad := &FlashbotsBundle{
		Transactions: []*types.Transaction{mevTestTx(0, 1000, to, types.HexToAddress("0x01"))},
		MinTimestamp: 200,
		MaxTimestamp: 100,
	}
	if err := bad.Validate(); err == nil {
		t.Error("bundle with min > max timestamp should fail validation")
	}
}

func TestBundleIsValidAtTime(t *testing.T) {
	to := types.HexToAddress("0xdead")
	tx := mevTestTx(0, 1000, to, types.HexToAddress("0x01"))

	b := &FlashbotsBundle{
		Transactions: []*types.Transaction{tx},
		MinTimestamp: 100,
		MaxTimestamp: 200,
	}

	if !b.IsValidAtTime(150) {
		t.Error("should be valid at time 150")
	}
	if b.IsValidAtTime(50) {
		t.Error("should not be valid at time 50")
	}
	if b.IsValidAtTime(250) {
		t.Error("should not be valid at time 250")
	}
	if !b.IsValidAtTime(100) {
		t.Error("should be valid at exact min time")
	}
	if !b.IsValidAtTime(200) {
		t.Error("should be valid at exact max time")
	}
}

func TestBundleTotalGas(t *testing.T) {
	to := types.HexToAddress("0xdead")
	sender := types.HexToAddress("0x01")
	b := &FlashbotsBundle{
		Transactions: []*types.Transaction{
			mevTestTx(0, 1000, to, sender),
			mevTestTx(1, 1000, to, sender),
			mevTestTx(2, 1000, to, sender),
		},
	}

	total := b.TotalGas()
	if total != 63000 { // 3 * 21000
		t.Errorf("TotalGas = %d, want 63000", total)
	}
}

func TestBundleIsRevertAllowed(t *testing.T) {
	hash1 := types.HexToHash("0xaaaa")
	hash2 := types.HexToHash("0xbbbb")
	hash3 := types.HexToHash("0xcccc")

	b := &FlashbotsBundle{
		RevertingTxHashes: []types.Hash{hash1, hash2},
	}

	if !b.IsRevertAllowed(hash1) {
		t.Error("hash1 should be allowed to revert")
	}
	if !b.IsRevertAllowed(hash2) {
		t.Error("hash2 should be allowed to revert")
	}
	if b.IsRevertAllowed(hash3) {
		t.Error("hash3 should not be allowed to revert")
	}
}

func TestBundleNoTimestampConstraints(t *testing.T) {
	to := types.HexToAddress("0xdead")
	b := &FlashbotsBundle{
		Transactions: []*types.Transaction{mevTestTx(0, 1000, to, types.HexToAddress("0x01"))},
		MinTimestamp: 0,
		MaxTimestamp: 0,
	}

	// No constraints: valid at any time.
	if !b.IsValidAtTime(0) {
		t.Error("should be valid with no constraints at time 0")
	}
	if !b.IsValidAtTime(999999) {
		t.Error("should be valid with no constraints at any time")
	}
}

// --- Sandwich Detection ---

func TestDetectSandwich(t *testing.T) {
	contract := types.HexToAddress("0xdEaD000000000000000000000000000000000000")
	attacker := types.HexToAddress("0xAAAA000000000000000000000000000000000000")
	victim := types.HexToAddress("0xBBBB000000000000000000000000000000000000")

	txs := []*types.Transaction{
		mevTestTx(0, 100, contract, attacker), // front: attacker, high gas
		mevTestTx(0, 50, contract, victim),    // victim: lower gas
		mevTestTx(1, 80, contract, attacker),  // back: attacker
	}

	candidates := DetectSandwich(txs)
	if len(candidates) != 1 {
		t.Fatalf("DetectSandwich: got %d candidates, want 1", len(candidates))
	}

	c := candidates[0]
	if c.Attacker != attacker {
		t.Errorf("Attacker = %v, want %v", c.Attacker, attacker)
	}
	if c.FrontTx != txs[0] {
		t.Error("FrontTx mismatch")
	}
	if c.VictimTx != txs[1] {
		t.Error("VictimTx mismatch")
	}
	if c.BackTx != txs[2] {
		t.Error("BackTx mismatch")
	}
}

func TestDetectSandwichNoPattern(t *testing.T) {
	contract := types.HexToAddress("0xdEaD000000000000000000000000000000000000")
	a := types.HexToAddress("0xAAAA000000000000000000000000000000000000")
	b := types.HexToAddress("0xBBBB000000000000000000000000000000000000")
	c := types.HexToAddress("0xCCCC000000000000000000000000000000000000")

	// All different senders: no sandwich.
	txs := []*types.Transaction{
		mevTestTx(0, 100, contract, a),
		mevTestTx(0, 50, contract, b),
		mevTestTx(0, 80, contract, c),
	}

	candidates := DetectSandwich(txs)
	if len(candidates) != 0 {
		t.Errorf("DetectSandwich with different senders: got %d, want 0", len(candidates))
	}
}

func TestDetectSandwichTooFew(t *testing.T) {
	contract := types.HexToAddress("0xdEaD000000000000000000000000000000000000")
	sender := types.HexToAddress("0xAAAA000000000000000000000000000000000000")

	// Only 2 txs: cannot form sandwich.
	txs := []*types.Transaction{
		mevTestTx(0, 100, contract, sender),
		mevTestTx(1, 80, contract, sender),
	}
	candidates := DetectSandwich(txs)
	if len(candidates) != 0 {
		t.Errorf("DetectSandwich with 2 txs: got %d, want 0", len(candidates))
	}

	// Nil: should return nil.
	candidates = DetectSandwich(nil)
	if candidates != nil {
		t.Error("DetectSandwich(nil) should return nil")
	}
}

func TestDetectSandwichDifferentTargets(t *testing.T) {
	contract1 := types.HexToAddress("0xdEaD000000000000000000000000000000000000")
	contract2 := types.HexToAddress("0xBEEF000000000000000000000000000000000000")
	attacker := types.HexToAddress("0xAAAA000000000000000000000000000000000000")
	victim := types.HexToAddress("0xBBBB000000000000000000000000000000000000")

	// Attacker txs target different contracts: not a sandwich.
	txs := []*types.Transaction{
		mevTestTx(0, 100, contract1, attacker),
		mevTestTx(0, 50, contract1, victim),
		mevTestTx(1, 80, contract2, attacker),
	}

	candidates := DetectSandwich(txs)
	if len(candidates) != 0 {
		t.Errorf("different targets: got %d candidates, want 0", len(candidates))
	}
}

// --- Frontrun Detection ---

func TestDetectFrontrun(t *testing.T) {
	contract := types.HexToAddress("0xdEaD000000000000000000000000000000000000")
	frontrunner := types.HexToAddress("0xAAAA000000000000000000000000000000000000")
	victim := types.HexToAddress("0xBBBB000000000000000000000000000000000000")

	txs := []*types.Transaction{
		mevTestTx(0, 1000, contract, frontrunner), // 10x gas price
		mevTestTx(0, 100, contract, victim),
	}

	candidates := DetectFrontrun(txs, 10)
	if len(candidates) != 1 {
		t.Fatalf("DetectFrontrun: got %d candidates, want 1", len(candidates))
	}

	c := candidates[0]
	if c.GasRatio < 10 {
		t.Errorf("GasRatio = %d, want >= 10", c.GasRatio)
	}
}

func TestDetectFrontrunNoPattern(t *testing.T) {
	contract := types.HexToAddress("0xdEaD000000000000000000000000000000000000")
	a := types.HexToAddress("0xAAAA000000000000000000000000000000000000")
	b := types.HexToAddress("0xBBBB000000000000000000000000000000000000")

	// Similar gas prices: no frontrun.
	txs := []*types.Transaction{
		mevTestTx(0, 100, contract, a),
		mevTestTx(0, 90, contract, b),
	}

	candidates := DetectFrontrun(txs, 10)
	if len(candidates) != 0 {
		t.Errorf("similar gas prices: got %d candidates, want 0", len(candidates))
	}
}

func TestDetectFrontrunSameSender(t *testing.T) {
	contract := types.HexToAddress("0xdEaD000000000000000000000000000000000000")
	sender := types.HexToAddress("0xAAAA000000000000000000000000000000000000")

	// Same sender: not frontrunning.
	txs := []*types.Transaction{
		mevTestTx(0, 1000, contract, sender),
		mevTestTx(1, 100, contract, sender),
	}

	candidates := DetectFrontrun(txs, 10)
	if len(candidates) != 0 {
		t.Errorf("same sender: got %d candidates, want 0", len(candidates))
	}
}

func TestDetectFrontrunEdgeCases(t *testing.T) {
	// Nil input.
	if candidates := DetectFrontrun(nil, 10); candidates != nil {
		t.Error("DetectFrontrun(nil) should return nil")
	}

	// Single tx.
	contract := types.HexToAddress("0xdead")
	txs := []*types.Transaction{mevTestTx(0, 100, contract, types.HexToAddress("0x01"))}
	if candidates := DetectFrontrun(txs, 10); candidates != nil {
		t.Error("single tx should return nil")
	}

	// Zero ratio.
	if candidates := DetectFrontrun(txs, 0); candidates != nil {
		t.Error("zero ratio should return nil")
	}
}

// --- Fair Ordering ---

func TestFairOrdering(t *testing.T) {
	contract := types.HexToAddress("0xdEaD000000000000000000000000000000000000")
	a := types.HexToAddress("0x01")
	b := types.HexToAddress("0x02")
	c := types.HexToAddress("0x03")

	entries := []FairOrderingEntry{
		{Transaction: mevTestTx(0, 100, contract, c), ArrivalTime: 300},
		{Transaction: mevTestTx(0, 500, contract, a), ArrivalTime: 100},
		{Transaction: mevTestTx(0, 200, contract, b), ArrivalTime: 200},
	}

	sorted, violations := FairOrdering(entries, 5)
	if len(sorted) != 3 {
		t.Fatalf("sorted len = %d, want 3", len(sorted))
	}

	// Should be sorted by arrival time.
	if sorted[0].ArrivalTime != 100 {
		t.Errorf("sorted[0].ArrivalTime = %d, want 100", sorted[0].ArrivalTime)
	}
	if sorted[1].ArrivalTime != 200 {
		t.Errorf("sorted[1].ArrivalTime = %d, want 200", sorted[1].ArrivalTime)
	}
	if sorted[2].ArrivalTime != 300 {
		t.Errorf("sorted[2].ArrivalTime = %d, want 300", sorted[2].ArrivalTime)
	}

	// Entries were reordered by 1 position max, within maxDelay=5.
	if len(violations) != 0 {
		t.Errorf("unexpected violations: %v", violations)
	}
}

func TestFairOrderingViolation(t *testing.T) {
	contract := types.HexToAddress("0xdEaD000000000000000000000000000000000000")

	// Original order has a tx delayed 10 positions from its fair position.
	entries := make([]FairOrderingEntry, 12)
	for i := 0; i < 12; i++ {
		sender := types.BytesToAddress([]byte{byte(i + 1)})
		entries[i] = FairOrderingEntry{
			Transaction: mevTestTx(0, int64(100+i), contract, sender),
			ArrivalTime: uint64(i * 10),
		}
	}

	// Swap first and last: puts the earliest arrival at position 11 (delay=11).
	entries[0], entries[11] = entries[11], entries[0]

	_, violations := FairOrdering(entries, 5)
	if len(violations) == 0 {
		t.Error("expected fair ordering violations for large delay")
	}
}

func TestFairOrderingEmpty(t *testing.T) {
	sorted, violations := FairOrdering(nil, 5)
	if sorted != nil {
		t.Error("empty input should return nil sorted")
	}
	if violations != nil {
		t.Error("empty input should return nil violations")
	}
}

func TestFairOrderingConstraints(t *testing.T) {
	contract := types.HexToAddress("0xdead")

	// All same arrival time: any order is fair.
	entries := []FairOrderingEntry{
		{Transaction: mevTestTx(0, 100, contract, types.HexToAddress("0x01")), ArrivalTime: 100},
		{Transaction: mevTestTx(0, 200, contract, types.HexToAddress("0x02")), ArrivalTime: 100},
		{Transaction: mevTestTx(0, 300, contract, types.HexToAddress("0x03")), ArrivalTime: 100},
	}

	sorted, violations := FairOrdering(entries, 0)
	if len(sorted) != 3 {
		t.Fatalf("sorted len = %d, want 3", len(sorted))
	}
	if len(violations) != 0 {
		t.Errorf("same arrival time should have no violations, got %d", len(violations))
	}
}

// --- Backrun Opportunities ---

func TestIdentifyBackrunOpportunities(t *testing.T) {
	contract := types.HexToAddress("0xdEaD000000000000000000000000000000000000")
	trigger := types.HexToAddress("0xAAAA000000000000000000000000000000000000")
	backrunner := types.HexToAddress("0xBBBB000000000000000000000000000000000000")

	txs := []*types.Transaction{
		mevTestTx(0, 100, contract, trigger),    // original tx
		mevTestTx(0, 50, contract, backrunner),  // backrun with lower gas price
	}

	opps := IdentifyBackrunOpportunities(txs)
	if len(opps) != 1 {
		t.Fatalf("got %d opportunities, want 1", len(opps))
	}

	if opps[0].TargetAddress != contract {
		t.Errorf("target = %v, want %v", opps[0].TargetAddress, contract)
	}
}

func TestIdentifyBackrunNoOpportunity(t *testing.T) {
	contract := types.HexToAddress("0xdEaD000000000000000000000000000000000000")
	a := types.HexToAddress("0xAAAA000000000000000000000000000000000000")
	b := types.HexToAddress("0xBBBB000000000000000000000000000000000000")

	// Second tx has higher gas: this looks like frontrunning, not backrunning.
	txs := []*types.Transaction{
		mevTestTx(0, 50, contract, a),
		mevTestTx(0, 500, contract, b),
	}

	opps := IdentifyBackrunOpportunities(txs)
	if len(opps) != 0 {
		t.Errorf("higher gas second tx: got %d opportunities, want 0", len(opps))
	}
}

func TestIdentifyBackrunEmpty(t *testing.T) {
	opps := IdentifyBackrunOpportunities(nil)
	if opps != nil {
		t.Error("nil input should return nil")
	}

	opps = IdentifyBackrunOpportunities([]*types.Transaction{})
	if opps != nil {
		t.Error("empty input should return nil")
	}
}

// --- DefaultMEVProtectionConfig ---

func TestDefaultMEVProtectionConfig(t *testing.T) {
	cfg := DefaultMEVProtectionConfig()
	if !cfg.EnableSandwichDetection {
		t.Error("sandwich detection should be enabled by default")
	}
	if !cfg.EnableFrontrunDetection {
		t.Error("frontrun detection should be enabled by default")
	}
	if !cfg.EnableFairOrdering {
		t.Error("fair ordering should be enabled by default")
	}
	if cfg.MaxGasPriceRatio != 10 {
		t.Errorf("MaxGasPriceRatio = %d, want 10", cfg.MaxGasPriceRatio)
	}
	if cfg.FairOrderMaxDelay != 5 {
		t.Errorf("FairOrderMaxDelay = %d, want 5", cfg.FairOrderMaxDelay)
	}
}
