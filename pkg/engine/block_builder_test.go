package engine

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// makeLegacyTx creates a legacy transaction with the given gas price and gas limit.
func makeLegacyTx(gasPrice int64, gas uint64, nonce uint64) *types.Transaction {
	to := types.HexToAddress("0xdead000000000000000000000000000000000001")
	return types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(gasPrice),
		Gas:      gas,
		To:       &to,
		Value:    big.NewInt(0),
	})
}

// makeDynamicFeeTx creates an EIP-1559 transaction.
func makeDynamicFeeTx(feeCap, tipCap int64, gas uint64, nonce uint64) *types.Transaction {
	to := types.HexToAddress("0xdead000000000000000000000000000000000002")
	return types.NewTransaction(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     nonce,
		GasTipCap: big.NewInt(tipCap),
		GasFeeCap: big.NewInt(feeCap),
		Gas:       gas,
		To:        &to,
		Value:     big.NewInt(0),
	})
}

func TestBlockBuilderAddTransaction(t *testing.T) {
	bb := NewTxBlockBuilder()
	tx := makeLegacyTx(20, 21000, 0)

	if err := bb.AddTransaction(tx); err != nil {
		t.Fatalf("AddTransaction failed: %v", err)
	}
	if bb.PendingCount() != 1 {
		t.Fatalf("pending count: got %d, want 1", bb.PendingCount())
	}
	if bb.GasUsed() != 21000 {
		t.Fatalf("gas used: got %d, want 21000", bb.GasUsed())
	}
}

func TestBlockBuilderNilTransaction(t *testing.T) {
	bb := NewTxBlockBuilder()
	err := bb.AddTransaction(nil)
	if err != ErrNilTransaction {
		t.Fatalf("expected ErrNilTransaction, got %v", err)
	}
}

func TestBlockBuilderGasLimitEnforcement(t *testing.T) {
	bb := NewTxBlockBuilder()
	bb.SetGasLimit(50000)

	tx1 := makeLegacyTx(10, 30000, 0)
	if err := bb.AddTransaction(tx1); err != nil {
		t.Fatalf("tx1 should succeed: %v", err)
	}

	// This transaction would push total to 51000 > 50000.
	tx2 := makeLegacyTx(10, 21000, 1)
	err := bb.AddTransaction(tx2)
	if err != ErrGasLimitExceeded {
		t.Fatalf("expected ErrGasLimitExceeded, got %v", err)
	}

	// A smaller tx should fit.
	tx3 := makeLegacyTx(10, 20000, 1)
	if err := bb.AddTransaction(tx3); err != nil {
		t.Fatalf("tx3 should succeed: %v", err)
	}
}

func TestBlockBuilderBuildBlock(t *testing.T) {
	bb := NewTxBlockBuilder()

	tx1 := makeLegacyTx(10, 21000, 0)
	tx2 := makeLegacyTx(20, 21000, 1)
	bb.AddTransaction(tx1)
	bb.AddTransaction(tx2)

	parentHash := types.HexToHash("0x1234")
	coinbase := types.HexToAddress("0xfee")

	block, err := bb.BuildBlock(parentHash, 1000, coinbase, 100000)
	if err != nil {
		t.Fatalf("BuildBlock failed: %v", err)
	}
	if block.ParentHash() != parentHash {
		t.Fatalf("parent hash mismatch")
	}
	if block.Coinbase() != coinbase {
		t.Fatalf("coinbase mismatch")
	}
	if block.Time() != 1000 {
		t.Fatalf("timestamp: got %d, want 1000", block.Time())
	}
	if block.GasLimit() != 100000 {
		t.Fatalf("gas limit: got %d, want 100000", block.GasLimit())
	}
	if len(block.Transactions()) != 2 {
		t.Fatalf("tx count: got %d, want 2", len(block.Transactions()))
	}
}

func TestBlockBuilderGasPriceOrdering(t *testing.T) {
	bb := NewTxBlockBuilder()

	// Add transactions in ascending gas price order.
	tx1 := makeLegacyTx(5, 21000, 0)
	tx2 := makeLegacyTx(15, 21000, 1)
	tx3 := makeLegacyTx(10, 21000, 2)
	bb.AddTransaction(tx1)
	bb.AddTransaction(tx2)
	bb.AddTransaction(tx3)

	block, err := bb.BuildBlock(types.Hash{}, 1000, types.Address{}, 1000000)
	if err != nil {
		t.Fatalf("BuildBlock failed: %v", err)
	}

	txs := block.Transactions()
	if len(txs) != 3 {
		t.Fatalf("tx count: got %d, want 3", len(txs))
	}

	// Verify ordering: highest gas price first.
	prices := make([]int64, len(txs))
	for i, tx := range txs {
		prices[i] = tx.GasPrice().Int64()
	}
	if prices[0] != 15 || prices[1] != 10 || prices[2] != 5 {
		t.Fatalf("gas price ordering wrong: got %v, want [15, 10, 5]", prices)
	}
}

func TestBlockBuilderBuildBlockGasFiltering(t *testing.T) {
	bb := NewTxBlockBuilder()

	// Add 3 txs but BuildBlock with a gas limit that can only fit 2.
	tx1 := makeLegacyTx(10, 30000, 0)
	tx2 := makeLegacyTx(20, 30000, 1)
	tx3 := makeLegacyTx(15, 30000, 2)
	bb.AddTransaction(tx1)
	bb.AddTransaction(tx2)
	bb.AddTransaction(tx3)

	// Gas limit of 60000 can fit exactly 2 txs (each 30000 gas).
	block, err := bb.BuildBlock(types.Hash{}, 1000, types.Address{}, 60000)
	if err != nil {
		t.Fatalf("BuildBlock failed: %v", err)
	}

	txs := block.Transactions()
	if len(txs) != 2 {
		t.Fatalf("tx count: got %d, want 2", len(txs))
	}
	// The two highest-priced txs should be included: 20 and 15.
	if txs[0].GasPrice().Int64() != 20 {
		t.Fatalf("tx[0] gas price: got %d, want 20", txs[0].GasPrice().Int64())
	}
	if txs[1].GasPrice().Int64() != 15 {
		t.Fatalf("tx[1] gas price: got %d, want 15", txs[1].GasPrice().Int64())
	}
}

func TestBlockBuilderReset(t *testing.T) {
	bb := NewTxBlockBuilder()
	bb.SetGasLimit(100000)

	tx := makeLegacyTx(10, 21000, 0)
	bb.AddTransaction(tx)

	bb.Reset()

	if bb.PendingCount() != 0 {
		t.Fatalf("pending count after reset: got %d, want 0", bb.PendingCount())
	}
	if bb.GasUsed() != 0 {
		t.Fatalf("gas used after reset: got %d, want 0", bb.GasUsed())
	}
}

func TestBlockBuilderZeroGasLimit(t *testing.T) {
	bb := NewTxBlockBuilder()
	tx := makeLegacyTx(10, 21000, 0)
	bb.AddTransaction(tx)

	_, err := bb.BuildBlock(types.Hash{}, 1000, types.Address{}, 0)
	if err != ErrZeroGasLimit {
		t.Fatalf("expected ErrZeroGasLimit, got %v", err)
	}
}

func TestBlockBuilderDynamicFeeTxOrdering(t *testing.T) {
	bb := NewTxBlockBuilder()

	// Mix legacy and EIP-1559 transactions.
	tx1 := makeLegacyTx(10, 21000, 0)
	tx2 := makeDynamicFeeTx(25, 5, 21000, 1)
	tx3 := makeDynamicFeeTx(15, 3, 21000, 2)
	bb.AddTransaction(tx1)
	bb.AddTransaction(tx2)
	bb.AddTransaction(tx3)

	block, err := bb.BuildBlock(types.Hash{}, 1000, types.Address{}, 1000000)
	if err != nil {
		t.Fatalf("BuildBlock failed: %v", err)
	}

	txs := block.Transactions()
	if len(txs) != 3 {
		t.Fatalf("tx count: got %d, want 3", len(txs))
	}
	// Ordering by effective price (GasFeeCap for EIP-1559, GasPrice for legacy):
	// tx2 feeCap=25, tx3 feeCap=15, tx1 gasPrice=10
	effectivePrices := []int64{
		txEffectivePrice(txs[0]).Int64(),
		txEffectivePrice(txs[1]).Int64(),
		txEffectivePrice(txs[2]).Int64(),
	}
	if effectivePrices[0] != 25 || effectivePrices[1] != 15 || effectivePrices[2] != 10 {
		t.Fatalf("effective price ordering wrong: got %v, want [25, 15, 10]", effectivePrices)
	}
}

func TestBlockBuilderEmptyBuild(t *testing.T) {
	bb := NewTxBlockBuilder()

	block, err := bb.BuildBlock(types.Hash{}, 1000, types.Address{}, 100000)
	if err != nil {
		t.Fatalf("BuildBlock with no txs failed: %v", err)
	}
	if len(block.Transactions()) != 0 {
		t.Fatalf("expected 0 transactions, got %d", len(block.Transactions()))
	}
	if block.GasUsed() != 0 {
		t.Fatalf("gas used: got %d, want 0", block.GasUsed())
	}
}
