package core

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func newTestTx(gas uint64, data []byte) *types.Transaction {
	to := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	return types.NewTransaction(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Gas:       gas,
		GasFeeCap: big.NewInt(1_000_000_000),
		GasTipCap: big.NewInt(1_000_000),
		To:        &to,
		Value:     big.NewInt(0),
		Data:      data,
	})
}

func newTestBlobTx(gas uint64, blobHashes []types.Hash) *types.Transaction {
	to := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	return types.NewTransaction(&types.BlobTx{
		ChainID:    big.NewInt(1),
		Gas:        gas,
		GasFeeCap:  big.NewInt(1_000_000_000),
		GasTipCap:  big.NewInt(1_000_000),
		To:         to,
		Value:      big.NewInt(0),
		BlobFeeCap: big.NewInt(1_000_000),
		BlobHashes: blobHashes,
	})
}

func TestValidateTransactionGasCapsBasic(t *testing.T) {
	cfg := DefaultGasCapConfig()

	// Valid transaction.
	tx := newTestTx(21000, nil)
	if err := ValidateTransactionGasCaps(tx, cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Zero gas.
	zeroTx := newTestTx(0, nil)
	if err := ValidateTransactionGasCaps(zeroTx, cfg); err == nil {
		t.Fatal("expected error for zero gas transaction")
	}
}

func TestValidateTransactionGasCapsExceedsTxCap(t *testing.T) {
	cfg := DefaultGasCapConfig()

	// Transaction gas exceeds MaxTransactionGas.
	tx := newTestTx(MaxTransactionGas+1, nil)
	err := ValidateTransactionGasCaps(tx, cfg)
	if err == nil {
		t.Fatal("expected error for gas exceeding tx cap")
	}

	// Exactly at the cap should be fine.
	tx = newTestTx(MaxTransactionGas, nil)
	if err := ValidateTransactionGasCaps(tx, cfg); err != nil {
		t.Fatalf("gas exactly at cap should be valid: %v", err)
	}
}

func TestValidateTransactionGasCapsBlobCount(t *testing.T) {
	cfg := DefaultGasCapConfig()

	// Too many blobs.
	hashes := make([]types.Hash, cfg.MaxBlobsPerTx+1)
	for i := range hashes {
		hashes[i] = types.HexToHash("0x01abcdef1234567890abcdef1234567890abcdef1234567890abcdef12345678")
	}
	tx := newTestBlobTx(21000, hashes)
	if err := ValidateTransactionGasCaps(tx, cfg); err == nil {
		t.Fatal("expected error for too many blobs")
	}

	// Exactly at blob cap should be fine.
	hashes = hashes[:cfg.MaxBlobsPerTx]
	tx = newTestBlobTx(21000, hashes)
	if err := ValidateTransactionGasCaps(tx, cfg); err != nil {
		t.Fatalf("blob count at cap should be valid: %v", err)
	}
}

func TestValidateBlockGasLimit(t *testing.T) {
	cfg := DefaultGasCapConfig()

	parent := &types.Header{GasLimit: 30_000_000}

	// Valid: no change.
	header := &types.Header{GasLimit: 30_000_000}
	if err := ValidateBlockGasLimit(parent, header, cfg); err != nil {
		t.Fatalf("no change should be valid: %v", err)
	}

	// Valid: small increase.
	maxDelta := parent.GasLimit / cfg.GasLimitBoundDivisor
	header = &types.Header{GasLimit: parent.GasLimit + maxDelta}
	if err := ValidateBlockGasLimit(parent, header, cfg); err != nil {
		t.Fatalf("max delta increase should be valid: %v", err)
	}

	// Invalid: too large increase.
	header = &types.Header{GasLimit: parent.GasLimit + maxDelta + 1}
	if err := ValidateBlockGasLimit(parent, header, cfg); err == nil {
		t.Fatal("expected error for too large gas limit change")
	}

	// Invalid: below minimum.
	header = &types.Header{GasLimit: cfg.MinBlockGasLimit - 1}
	if err := ValidateBlockGasLimit(parent, header, cfg); err == nil {
		t.Fatal("expected error for gas limit below minimum")
	}
}

func TestValidateBlockGasUsage(t *testing.T) {
	header := &types.Header{GasLimit: 100000}

	// Transactions fit within limit.
	txs := []*types.Transaction{
		newTestTx(40000, nil),
		newTestTx(50000, nil),
	}
	if err := ValidateBlockGasUsage(header, txs); err != nil {
		t.Fatalf("transactions should fit: %v", err)
	}

	// Transactions exceed limit.
	txs = append(txs, newTestTx(20000, nil))
	if err := ValidateBlockGasUsage(header, txs); err == nil {
		t.Fatal("expected error for exceeding block gas limit")
	}
}

func TestDynamicGasLimitAdjustment(t *testing.T) {
	// Exactly at target (50% utilization): no change.
	result := DynamicGasLimitAdjustment(30_000_000, 15_000_000, 0)
	if result != 30_000_000 {
		t.Fatalf("expected no change at target, got %d", result)
	}

	// Over-utilized: gas limit increases.
	result = DynamicGasLimitAdjustment(30_000_000, 25_000_000, 0)
	if result <= 30_000_000 {
		t.Fatalf("expected increase for over-utilization, got %d", result)
	}

	// Under-utilized: gas limit decreases.
	result = DynamicGasLimitAdjustment(30_000_000, 5_000_000, 0)
	if result >= 30_000_000 {
		t.Fatalf("expected decrease for under-utilization, got %d", result)
	}

	// With explicit target: moves toward target.
	result = DynamicGasLimitAdjustment(30_000_000, 15_000_000, 60_000_000)
	if result <= 30_000_000 {
		t.Fatalf("expected increase toward target, got %d", result)
	}
}

func TestDynamicGasLimitAdjustmentBounded(t *testing.T) {
	// Max increase is bounded by 1/1024.
	parentLimit := uint64(30_000_000)
	maxDelta := parentLimit / GasLimitBoundDivisor
	result := DynamicGasLimitAdjustment(parentLimit, parentLimit, 0) // Full block
	if result > parentLimit+maxDelta {
		t.Fatalf("increase should not exceed max delta: result=%d, max=%d", result, parentLimit+maxDelta)
	}
}

func TestGasCapConfigForFork(t *testing.T) {
	// Pre-Prague: default config but no fork-specific blob schedule.
	prePrague := &ChainConfig{
		ChainID: big.NewInt(1),
	}
	cfg := GasCapConfigForFork(prePrague, 1000)
	if cfg.MaxTxGas != MaxTransactionGas {
		t.Fatalf("expected default max tx gas, got %d", cfg.MaxTxGas)
	}

	// With Prague and BPO1 active: max blobs should be BPO1.
	withBPO1 := &ChainConfig{
		ChainID:    big.NewInt(1),
		PragueTime: newUint64(0),
		BPO1Time:   newUint64(0),
	}
	cfg = GasCapConfigForFork(withBPO1, 1000)
	if cfg.MaxBlobsPerTx != int(BPO1BlobSchedule.Max) {
		t.Fatalf("expected BPO1 max blobs %d, got %d", BPO1BlobSchedule.Max, cfg.MaxBlobsPerTx)
	}
}

func TestValidateTransactionGasWithFork(t *testing.T) {
	// Pre-Prague: only zero gas check.
	prePrague := &ChainConfig{ChainID: big.NewInt(1)}
	tx := newTestTx(MaxTransactionGas+1, nil)
	if err := ValidateTransactionGasWithFork(tx, prePrague, 1000); err != nil {
		t.Fatalf("pre-Prague should not enforce EIP-7825: %v", err)
	}

	// Post-Prague: EIP-7825 enforced.
	postPrague := &ChainConfig{
		ChainID:    big.NewInt(1),
		PragueTime: newUint64(0),
	}
	if err := ValidateTransactionGasWithFork(tx, postPrague, 1000); err == nil {
		t.Fatal("post-Prague should enforce EIP-7825 tx gas cap")
	}
}

func TestValidateGasCapInvariant(t *testing.T) {
	cfg := DefaultGasCapConfig()

	// Block gas limit smaller than tx cap.
	tx := newTestTx(500000, nil)
	if err := ValidateGasCapInvariant(tx, 100000, cfg); err == nil {
		t.Fatal("expected error when tx gas exceeds block limit")
	}

	// Block gas limit larger than tx cap, tx within tx cap.
	if err := ValidateGasCapInvariant(tx, 100_000_000, cfg); err != nil {
		t.Fatalf("tx within caps should be valid: %v", err)
	}
}

func TestEstimateBlockTxCapacity(t *testing.T) {
	if cap := EstimateBlockTxCapacity(30_000_000, 21000); cap != 1428 {
		t.Fatalf("expected 1428 txs, got %d", cap)
	}
	if cap := EstimateBlockTxCapacity(30_000_000, 0); cap != 0 {
		t.Fatalf("expected 0 txs for zero avg gas, got %d", cap)
	}
}

func TestValidateAllGasCaps(t *testing.T) {
	cfg := DefaultGasCapConfig()

	// Valid transaction.
	tx := newTestTx(21000, nil)
	result := ValidateAllGasCaps(tx, 30_000_000, cfg)
	if !result.Ok() {
		t.Fatalf("expected no errors, got: %v", result.Error())
	}

	// Transaction with multiple violations.
	tx = newTestTx(0, nil)
	result = ValidateAllGasCaps(tx, 30_000_000, cfg)
	if result.Ok() {
		t.Fatal("expected errors for zero gas tx")
	}
	if len(result.Errors) < 1 {
		t.Fatalf("expected at least 1 error, got %d", len(result.Errors))
	}
}

func TestMoveTowardTarget(t *testing.T) {
	// Move up.
	result := moveTowardTarget(30_000_000, 60_000_000)
	if result <= 30_000_000 {
		t.Fatalf("expected increase, got %d", result)
	}

	// Move down.
	result = moveTowardTarget(60_000_000, 30_000_000)
	if result >= 60_000_000 {
		t.Fatalf("expected decrease, got %d", result)
	}

	// Already at target.
	result = moveTowardTarget(30_000_000, 30_000_000)
	if result != 30_000_000 {
		t.Fatalf("expected no change, got %d", result)
	}

	// Minimum gas limit protection.
	result = moveTowardTarget(MinGasLimit+1, 0)
	if result < MinGasLimit {
		t.Fatalf("should not go below MinGasLimit, got %d", result)
	}
}
