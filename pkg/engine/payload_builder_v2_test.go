package engine

import (
	"math/big"
	"testing"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// makePayloadTx creates a legacy transaction for payload builder V2 tests.
func makePayloadTx(nonce uint64, gasPrice int64, gas uint64) *types.Transaction {
	to := types.BytesToAddress([]byte{0xcc, 0xdd})
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(gasPrice),
		Gas:      gas,
		To:       &to,
		Value:    big.NewInt(0),
	})
	return tx
}

// makeDynamicPayloadTx creates an EIP-1559 transaction for payload builder V2 tests.
func makeDynamicPayloadTx(nonce uint64, tipCap, feeCap int64, gas uint64) *types.Transaction {
	to := types.BytesToAddress([]byte{0xcc, 0xdd})
	tx := types.NewTransaction(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     nonce,
		GasTipCap: big.NewInt(tipCap),
		GasFeeCap: big.NewInt(feeCap),
		Gas:       gas,
		To:        &to,
		Value:     big.NewInt(0),
	})
	return tx
}

func TestPayloadBuilderV2Basic(t *testing.T) {
	config := PayloadBuilderV2Config{
		BuildDeadline: 5 * time.Second,
		GasLimit:      100_000,
		Coinbase:      types.BytesToAddress([]byte{0x01}),
		ParentHash:    types.BytesToHash([]byte{0x42}),
		ParentNumber:  99,
		Timestamp:     1000,
	}
	pb := NewPayloadBuilderV2(config)

	txs := []*types.Transaction{
		makePayloadTx(0, 100, 21000),
		makePayloadTx(1, 200, 21000),
	}

	pb.StartBuild(txs)
	result, err := pb.WaitResult()
	if err != nil {
		t.Fatalf("WaitResult: %v", err)
	}

	if result.TxCount != 2 {
		t.Fatalf("TxCount: got %d, want 2", result.TxCount)
	}
	if result.GasUsed != 42000 {
		t.Fatalf("GasUsed: got %d, want 42000", result.GasUsed)
	}
	if result.Block == nil {
		t.Fatal("Block should not be nil")
	}
	if result.Block.NumberU64() != 100 {
		t.Fatalf("Block number: got %d, want 100", result.Block.NumberU64())
	}
}

func TestPayloadBuilderV2WithBaseFee(t *testing.T) {
	baseFee := big.NewInt(10)
	config := PayloadBuilderV2Config{
		BuildDeadline: 5 * time.Second,
		GasLimit:      100_000,
		BaseFee:       baseFee,
		ParentHash:    types.BytesToHash([]byte{0x42}),
		ParentNumber:  99,
		Timestamp:     1000,
	}
	pb := NewPayloadBuilderV2(config)

	txs := []*types.Transaction{
		makeDynamicPayloadTx(0, 5, 20, 21000),  // included
		makeDynamicPayloadTx(1, 10, 30, 21000), // included
		makePayloadTx(2, 5, 21000),             // below base fee, excluded
	}

	pb.StartBuild(txs)
	result, err := pb.WaitResult()
	if err != nil {
		t.Fatalf("WaitResult: %v", err)
	}

	if result.TxCount != 2 {
		t.Fatalf("TxCount: got %d, want 2 (1 excluded for low fee)", result.TxCount)
	}
}

func TestPayloadBuilderV2BlockValue(t *testing.T) {
	baseFee := big.NewInt(10)
	config := PayloadBuilderV2Config{
		BuildDeadline: 5 * time.Second,
		GasLimit:      100_000,
		BaseFee:       baseFee,
		ParentHash:    types.BytesToHash([]byte{0x42}),
		ParentNumber:  99,
		Timestamp:     1000,
	}
	pb := NewPayloadBuilderV2(config)

	// tx: effective = min(50, 10+20) = 30, tip = 30-10 = 20, value = 20 * 21000 = 420000
	txs := []*types.Transaction{
		makeDynamicPayloadTx(0, 20, 50, 21000),
	}

	pb.StartBuild(txs)
	result, err := pb.WaitResult()
	if err != nil {
		t.Fatalf("WaitResult: %v", err)
	}

	expected := big.NewInt(20 * 21000)
	if result.BlockValue.Cmp(expected) != 0 {
		t.Fatalf("BlockValue: got %s, want %s", result.BlockValue.String(), expected.String())
	}
}

func TestPayloadBuilderV2GasLimitExclusion(t *testing.T) {
	config := PayloadBuilderV2Config{
		BuildDeadline: 5 * time.Second,
		GasLimit:      50_000, // room for 2 txs at 21000
		ParentHash:    types.BytesToHash([]byte{0x42}),
		ParentNumber:  99,
		Timestamp:     1000,
	}
	pb := NewPayloadBuilderV2(config)

	txs := []*types.Transaction{
		makePayloadTx(0, 300, 21000),
		makePayloadTx(1, 200, 21000),
		makePayloadTx(2, 100, 21000), // won't fit
	}

	pb.StartBuild(txs)
	result, err := pb.WaitResult()
	if err != nil {
		t.Fatalf("WaitResult: %v", err)
	}

	if result.TxCount != 2 {
		t.Fatalf("TxCount: got %d, want 2", result.TxCount)
	}
}

func TestPayloadBuilderV2InclusionTracking(t *testing.T) {
	baseFee := big.NewInt(10)
	config := PayloadBuilderV2Config{
		BuildDeadline: 5 * time.Second,
		GasLimit:      50_000,
		BaseFee:       baseFee,
		ParentHash:    types.BytesToHash([]byte{0x42}),
		ParentNumber:  99,
		Timestamp:     1000,
	}
	pb := NewPayloadBuilderV2(config)

	txs := []*types.Transaction{
		makeDynamicPayloadTx(0, 10, 30, 21000), // included
		makeDynamicPayloadTx(1, 5, 20, 21000),  // included
		makePayloadTx(2, 5, 21000),             // excluded: below base fee
		makePayloadTx(3, 100, 21000),           // excluded: gas limit
	}

	pb.StartBuild(txs)
	pb.WaitResult()

	included := pb.IncludedCount()
	excluded := pb.ExcludedCount()

	if included != 2 {
		t.Fatalf("IncludedCount: got %d, want 2", included)
	}
	if excluded != 2 {
		t.Fatalf("ExcludedCount: got %d, want 2", excluded)
	}

	tracking := pb.InclusionTracking()
	for _, track := range tracking {
		if !track.included && track.reason == "" {
			t.Fatal("excluded tx should have a reason")
		}
	}
}

func TestPayloadBuilderV2Withdrawals(t *testing.T) {
	config := PayloadBuilderV2Config{
		BuildDeadline: 5 * time.Second,
		GasLimit:      100_000,
		ParentHash:    types.BytesToHash([]byte{0x42}),
		ParentNumber:  99,
		Timestamp:     1000,
		Withdrawals: []*Withdrawal{
			{Index: 0, ValidatorIndex: 100, Address: types.BytesToAddress([]byte{0x01}), Amount: 1000},
			{Index: 1, ValidatorIndex: 101, Address: types.BytesToAddress([]byte{0x02}), Amount: 2000},
		},
	}
	pb := NewPayloadBuilderV2(config)

	pb.StartBuild([]*types.Transaction{makePayloadTx(0, 100, 21000)})
	result, err := pb.WaitResult()
	if err != nil {
		t.Fatalf("WaitResult: %v", err)
	}

	withdrawals := result.Block.Withdrawals()
	if len(withdrawals) != 2 {
		t.Fatalf("Withdrawals count: got %d, want 2", len(withdrawals))
	}
	if withdrawals[0].Amount != 1000 || withdrawals[1].Amount != 2000 {
		t.Fatalf("Withdrawal amounts: got %d, %d", withdrawals[0].Amount, withdrawals[1].Amount)
	}
}

func TestPayloadBuilderV2Stop(t *testing.T) {
	config := PayloadBuilderV2Config{
		BuildDeadline: 10 * time.Second,
		GasLimit:      100_000,
		ParentHash:    types.BytesToHash([]byte{0x42}),
		ParentNumber:  99,
		Timestamp:     1000,
	}
	pb := NewPayloadBuilderV2(config)

	txs := []*types.Transaction{
		makePayloadTx(0, 100, 21000),
	}

	pb.StartBuild(txs)
	// Give a moment for the build to run.
	time.Sleep(10 * time.Millisecond)
	pb.Stop()

	// Should still be able to get the result.
	result, err := pb.WaitResult()
	if err != nil {
		t.Fatalf("WaitResult after stop: %v", err)
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}
}

func TestPayloadBuilderV2IsComplete(t *testing.T) {
	config := PayloadBuilderV2Config{
		BuildDeadline: 5 * time.Second,
		GasLimit:      100_000,
		ParentHash:    types.BytesToHash([]byte{0x42}),
		ParentNumber:  99,
		Timestamp:     1000,
	}
	pb := NewPayloadBuilderV2(config)

	if pb.IsComplete() {
		t.Fatal("should not be complete before start")
	}

	pb.StartBuild([]*types.Transaction{makePayloadTx(0, 100, 21000)})
	pb.WaitResult()

	if !pb.IsComplete() {
		t.Fatal("should be complete after WaitResult")
	}
}

func TestPayloadBuilderV2EmptyBuild(t *testing.T) {
	config := PayloadBuilderV2Config{
		BuildDeadline: 5 * time.Second,
		GasLimit:      100_000,
		ParentHash:    types.BytesToHash([]byte{0x42}),
		ParentNumber:  99,
		Timestamp:     1000,
	}
	pb := NewPayloadBuilderV2(config)

	pb.StartBuild(nil)
	result, err := pb.WaitResult()
	// Empty builds should still produce a valid result (an empty block).
	if err != nil {
		t.Fatalf("WaitResult on empty: %v", err)
	}
	if result.TxCount != 0 {
		t.Fatalf("TxCount: got %d, want 0", result.TxCount)
	}
	if result.Block == nil {
		t.Fatal("Block should not be nil even for empty builds")
	}
}

func TestPayloadBuilderV2PayloadValue(t *testing.T) {
	config := PayloadBuilderV2Config{
		BuildDeadline: 5 * time.Second,
		GasLimit:      100_000,
		ParentHash:    types.BytesToHash([]byte{0x42}),
		ParentNumber:  99,
		Timestamp:     1000,
	}
	pb := NewPayloadBuilderV2(config)

	// Before building, value should be zero.
	if pb.PayloadValue().Sign() != 0 {
		t.Fatal("PayloadValue before build should be zero")
	}

	pb.StartBuild([]*types.Transaction{makePayloadTx(0, 100, 21000)})
	pb.WaitResult()

	// Legacy tx with no base fee: entire gas price is tip.
	// value = 100 * 21000 = 2100000
	value := pb.PayloadValue()
	expected := big.NewInt(100 * 21000)
	if value.Cmp(expected) != 0 {
		t.Fatalf("PayloadValue: got %s, want %s", value.String(), expected.String())
	}
}

func TestPayloadBuilderV2DoubleStart(t *testing.T) {
	config := PayloadBuilderV2Config{
		BuildDeadline: 5 * time.Second,
		GasLimit:      100_000,
		ParentHash:    types.BytesToHash([]byte{0x42}),
		ParentNumber:  99,
		Timestamp:     1000,
	}
	pb := NewPayloadBuilderV2(config)

	pb.StartBuild([]*types.Transaction{makePayloadTx(0, 100, 21000)})
	// Second start should be a no-op.
	pb.StartBuild([]*types.Transaction{makePayloadTx(0, 200, 21000)})

	result, err := pb.WaitResult()
	if err != nil {
		t.Fatalf("WaitResult: %v", err)
	}
	// Should have the first build's result.
	if result.TxCount != 1 {
		t.Fatalf("TxCount: got %d, want 1", result.TxCount)
	}
}

func TestPayloadBuilderV2BlobsBundle(t *testing.T) {
	config := PayloadBuilderV2Config{
		BuildDeadline: 5 * time.Second,
		GasLimit:      100_000,
		ParentHash:    types.BytesToHash([]byte{0x42}),
		ParentNumber:  99,
		Timestamp:     1000,
	}
	pb := NewPayloadBuilderV2(config)

	pb.StartBuild([]*types.Transaction{makePayloadTx(0, 100, 21000)})
	result, _ := pb.WaitResult()

	if result.BlobsBundle == nil {
		t.Fatal("BlobsBundle should not be nil")
	}
}

func TestPayloadBuilderV2Receipts(t *testing.T) {
	config := PayloadBuilderV2Config{
		BuildDeadline: 5 * time.Second,
		GasLimit:      100_000,
		ParentHash:    types.BytesToHash([]byte{0x42}),
		ParentNumber:  99,
		Timestamp:     1000,
	}
	pb := NewPayloadBuilderV2(config)

	txs := []*types.Transaction{
		makePayloadTx(0, 100, 21000),
		makePayloadTx(1, 200, 30000),
	}

	pb.StartBuild(txs)
	result, _ := pb.WaitResult()

	if len(result.Receipts) != 2 {
		t.Fatalf("Receipts count: got %d, want 2", len(result.Receipts))
	}
	// Check cumulative gas used.
	if result.Receipts[1].CumulativeGasUsed != 51000 {
		t.Fatalf("CumulativeGasUsed: got %d, want 51000", result.Receipts[1].CumulativeGasUsed)
	}
	// Receipts should have successful status.
	for i, r := range result.Receipts {
		if r.Status != types.ReceiptStatusSuccessful {
			t.Fatalf("receipt[%d].Status: got %d, want %d", i, r.Status, types.ReceiptStatusSuccessful)
		}
	}
}

func TestCalcPayloadValue(t *testing.T) {
	baseFee := big.NewInt(10)

	txs := []*types.Transaction{
		makeDynamicPayloadTx(0, 20, 50, 21000), // tip=20, value=20*21000=420000
		makeDynamicPayloadTx(1, 5, 30, 21000),  // tip=5, value=5*21000=105000
	}

	value := CalcPayloadValue(txs, baseFee)
	expected := big.NewInt(420000 + 105000)
	if value.Cmp(expected) != 0 {
		t.Fatalf("CalcPayloadValue: got %s, want %s", value.String(), expected.String())
	}
}

func TestCalcPayloadValueNilBaseFee(t *testing.T) {
	txs := []*types.Transaction{makePayloadTx(0, 100, 21000)}
	value := CalcPayloadValue(txs, nil)
	if value.Sign() != 0 {
		t.Fatalf("CalcPayloadValue with nil base fee: got %s, want 0", value.String())
	}
}
