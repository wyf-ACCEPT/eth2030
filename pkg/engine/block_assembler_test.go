package engine

import (
	"math/big"
	"testing"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// makeAssemblyTx creates a legacy transaction for block assembler tests.
func makeAssemblyTx(nonce uint64, gasPrice int64, gas uint64) *types.Transaction {
	to := types.BytesToAddress([]byte{0xaa, 0xbb})
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(gasPrice),
		Gas:      gas,
		To:       &to,
		Value:    big.NewInt(0),
	})
	return tx
}

// makeDynamicAssemblyTx creates an EIP-1559 transaction for assembler tests.
func makeDynamicAssemblyTx(nonce uint64, tipCap, feeCap int64, gas uint64) *types.Transaction {
	to := types.BytesToAddress([]byte{0xaa, 0xbb})
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

func TestBlockAssemblerBasic(t *testing.T) {
	config := BlockAssemblerConfig{
		GasLimit: 100_000,
		Timeout:  5 * time.Second,
	}
	ba := NewBlockAssembler(config)

	txs := []*types.Transaction{
		makeAssemblyTx(0, 100, 21000),
		makeAssemblyTx(1, 200, 21000),
		makeAssemblyTx(2, 50, 21000),
	}

	result := ba.Assemble(txs)
	if result.TxCount != 3 {
		t.Fatalf("TxCount: got %d, want 3", result.TxCount)
	}
	if result.GasUsed != 63000 {
		t.Fatalf("GasUsed: got %d, want 63000", result.GasUsed)
	}
	if result.TimedOut {
		t.Fatal("should not have timed out")
	}
}

func TestBlockAssemblerGasLimit(t *testing.T) {
	config := BlockAssemblerConfig{
		GasLimit: 50_000, // only room for 2 txs at 21000 gas each
		Timeout:  5 * time.Second,
	}
	ba := NewBlockAssembler(config)

	txs := []*types.Transaction{
		makeAssemblyTx(0, 300, 21000),
		makeAssemblyTx(1, 200, 21000),
		makeAssemblyTx(2, 100, 21000), // this won't fit
	}

	result := ba.Assemble(txs)
	if result.TxCount != 2 {
		t.Fatalf("TxCount: got %d, want 2", result.TxCount)
	}
	if result.GasUsed != 42000 {
		t.Fatalf("GasUsed: got %d, want 42000", result.GasUsed)
	}
}

func TestBlockAssemblerOrdersByGasPrice(t *testing.T) {
	config := BlockAssemblerConfig{
		GasLimit: 100_000,
		Timeout:  5 * time.Second,
	}
	ba := NewBlockAssembler(config)

	txs := []*types.Transaction{
		makeAssemblyTx(0, 50, 21000),
		makeAssemblyTx(1, 300, 21000),
		makeAssemblyTx(2, 100, 21000),
	}

	result := ba.Assemble(txs)
	if result.TxCount != 3 {
		t.Fatalf("TxCount: got %d, want 3", result.TxCount)
	}
	// Verify ordering: highest gas price first.
	if result.Transactions[0].GasPrice().Int64() != 300 {
		t.Fatalf("first tx gas price: got %d, want 300", result.Transactions[0].GasPrice().Int64())
	}
	if result.Transactions[1].GasPrice().Int64() != 100 {
		t.Fatalf("second tx gas price: got %d, want 100", result.Transactions[1].GasPrice().Int64())
	}
	if result.Transactions[2].GasPrice().Int64() != 50 {
		t.Fatalf("third tx gas price: got %d, want 50", result.Transactions[2].GasPrice().Int64())
	}
}

func TestBlockAssemblerWithBaseFee(t *testing.T) {
	baseFee := big.NewInt(10)
	config := BlockAssemblerConfig{
		GasLimit: 100_000,
		Timeout:  5 * time.Second,
		BaseFee:  baseFee,
	}
	ba := NewBlockAssembler(config)

	txs := []*types.Transaction{
		makeDynamicAssemblyTx(0, 5, 20, 21000),  // effective = min(20, 10+5) = 15
		makeDynamicAssemblyTx(1, 10, 30, 21000), // effective = min(30, 10+10) = 20
		makeAssemblyTx(2, 5, 21000),             // below base fee (5 < 10), should be skipped
	}

	result := ba.Assemble(txs)
	if result.TxCount != 2 {
		t.Fatalf("TxCount: got %d, want 2", result.TxCount)
	}
}

func TestBlockAssemblerCoinbaseReward(t *testing.T) {
	baseFee := big.NewInt(10)
	config := BlockAssemblerConfig{
		GasLimit: 100_000,
		Timeout:  5 * time.Second,
		BaseFee:  baseFee,
		Coinbase: types.BytesToAddress([]byte{0x01}),
	}
	ba := NewBlockAssembler(config)

	// 1 tx: effective = min(50, 10+20) = 30, tip = 30-10 = 20, reward = 20 * 21000 = 420000
	txs := []*types.Transaction{
		makeDynamicAssemblyTx(0, 20, 50, 21000),
	}

	result := ba.Assemble(txs)
	expected := big.NewInt(20 * 21000)
	if result.CoinbaseReward.Cmp(expected) != 0 {
		t.Fatalf("CoinbaseReward: got %s, want %s", result.CoinbaseReward.String(), expected.String())
	}
}

func TestBlockAssemblerBlobGasTracking(t *testing.T) {
	config := BlockAssemblerConfig{
		GasLimit: 1_000_000,
		Timeout:  5 * time.Second,
	}
	ba := NewBlockAssembler(config)

	// Create a blob tx with 1 blob (131072 gas).
	blobHash := types.Hash{0x01}
	blobTx := types.NewTransaction(&types.BlobTx{
		ChainID:    big.NewInt(1),
		Nonce:      0,
		GasTipCap:  big.NewInt(100),
		GasFeeCap:  big.NewInt(200),
		Gas:        50000,
		To:         types.BytesToAddress([]byte{0xaa}),
		Value:      big.NewInt(0),
		BlobFeeCap: big.NewInt(100),
		BlobHashes: []types.Hash{blobHash},
	})

	regularTx := makeAssemblyTx(1, 100, 21000)

	result := ba.Assemble([]*types.Transaction{blobTx, regularTx})
	if result.TxCount != 2 {
		t.Fatalf("TxCount: got %d, want 2", result.TxCount)
	}
	if result.BlobGasUsed != 131072 {
		t.Fatalf("BlobGasUsed: got %d, want 131072", result.BlobGasUsed)
	}
}

func TestBlockAssemblerBlobGasLimit(t *testing.T) {
	config := BlockAssemblerConfig{
		GasLimit: 10_000_000,
		Timeout:  5 * time.Second,
	}
	ba := NewBlockAssembler(config)

	// Create 7 blob txs (MaxBlobGasPerAssembly = 786432 = 6 blobs).
	// 7th blob tx should be excluded.
	var txs []*types.Transaction
	for i := 0; i < 7; i++ {
		blobHash := types.Hash{0x01}
		blobTx := types.NewTransaction(&types.BlobTx{
			ChainID:    big.NewInt(1),
			Nonce:      uint64(i),
			GasTipCap:  big.NewInt(100),
			GasFeeCap:  big.NewInt(200),
			Gas:        50000,
			To:         types.BytesToAddress([]byte{0xaa}),
			Value:      big.NewInt(0),
			BlobFeeCap: big.NewInt(100),
			BlobHashes: []types.Hash{blobHash},
		})
		txs = append(txs, blobTx)
	}

	result := ba.Assemble(txs)
	if result.TxCount != 6 {
		t.Fatalf("TxCount: got %d, want 6 (max blobs)", result.TxCount)
	}
	if result.BlobGasUsed != MaxBlobGasPerAssembly {
		t.Fatalf("BlobGasUsed: got %d, want %d", result.BlobGasUsed, MaxBlobGasPerAssembly)
	}
}

func TestBlockAssemblerEmptyCandidates(t *testing.T) {
	config := BlockAssemblerConfig{
		GasLimit: 100_000,
		Timeout:  5 * time.Second,
	}
	ba := NewBlockAssembler(config)

	result := ba.Assemble(nil)
	if result.TxCount != 0 {
		t.Fatalf("TxCount: got %d, want 0", result.TxCount)
	}
	if result.GasUsed != 0 {
		t.Fatalf("GasUsed: got %d, want 0", result.GasUsed)
	}
	if result.CoinbaseReward.Sign() != 0 {
		t.Fatalf("CoinbaseReward: got %s, want 0", result.CoinbaseReward.String())
	}
}

func TestBlockAssemblerTimeout(t *testing.T) {
	config := BlockAssemblerConfig{
		GasLimit: 1_000_000_000, // huge limit
		Timeout:  1 * time.Nanosecond,
	}
	ba := NewBlockAssembler(config)

	// Create a large batch of transactions.
	var txs []*types.Transaction
	for i := 0; i < 10000; i++ {
		txs = append(txs, makeAssemblyTx(uint64(i), 100, 21000))
	}

	result := ba.Assemble(txs)
	// Due to the extremely short timeout, the assembly should stop early.
	// It may or may not include any transactions, but the key test is
	// that it completes without hanging.
	_ = result
}

func TestCalcNextBaseFee(t *testing.T) {
	tests := []struct {
		name      string
		baseFee   *big.Int
		gasLimit  uint64
		gasUsed   uint64
		wantAbove *big.Int // result should be > this
		wantBelow *big.Int // result should be < this
	}{
		{
			name:      "nil base fee returns initial",
			baseFee:   nil,
			gasLimit:  30_000_000,
			gasUsed:   15_000_000,
			wantAbove: big.NewInt(999_999_999),
			wantBelow: big.NewInt(1_000_000_001),
		},
		{
			name:      "at target: unchanged",
			baseFee:   big.NewInt(1_000_000_000),
			gasLimit:  30_000_000,
			gasUsed:   15_000_000, // exactly 50% = target
			wantAbove: big.NewInt(999_999_999),
			wantBelow: big.NewInt(1_000_000_001),
		},
		{
			name:      "above target: increase",
			baseFee:   big.NewInt(1_000_000_000),
			gasLimit:  30_000_000,
			gasUsed:   30_000_000, // 100% = 2x target
			wantAbove: big.NewInt(1_000_000_000),
			wantBelow: big.NewInt(1_200_000_000), // max 12.5% increase
		},
		{
			name:      "below target: decrease",
			baseFee:   big.NewInt(1_000_000_000),
			gasLimit:  30_000_000,
			gasUsed:   0, // 0% usage
			wantAbove: big.NewInt(800_000_000), // max 12.5% decrease
			wantBelow: big.NewInt(1_000_000_000),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalcNextBaseFee(tt.baseFee, tt.gasLimit, tt.gasUsed)
			if result.Cmp(tt.wantAbove) <= 0 {
				t.Fatalf("result %s <= wantAbove %s", result.String(), tt.wantAbove.String())
			}
			if result.Cmp(tt.wantBelow) >= 0 {
				t.Fatalf("result %s >= wantBelow %s", result.String(), tt.wantBelow.String())
			}
		})
	}
}

func TestCalcNextBaseFeeMinimum(t *testing.T) {
	// Very low base fee and below-target usage should clamp at 7.
	result := CalcNextBaseFee(big.NewInt(7), 30_000_000, 0)
	if result.Cmp(big.NewInt(7)) < 0 {
		t.Fatalf("result %s below minimum 7", result.String())
	}
}

func TestCalcBlobGasUsed(t *testing.T) {
	blobHash := types.Hash{0x01}
	blobTx := types.NewTransaction(&types.BlobTx{
		ChainID:    big.NewInt(1),
		Nonce:      0,
		GasTipCap:  big.NewInt(100),
		GasFeeCap:  big.NewInt(200),
		Gas:        50000,
		To:         types.BytesToAddress([]byte{0xaa}),
		Value:      big.NewInt(0),
		BlobFeeCap: big.NewInt(100),
		BlobHashes: []types.Hash{blobHash, blobHash},
	})
	regularTx := makeAssemblyTx(1, 100, 21000)

	used := CalcBlobGasUsed([]*types.Transaction{blobTx, regularTx})
	if used != 2*BlobGasPerBlob {
		t.Fatalf("CalcBlobGasUsed: got %d, want %d", used, 2*BlobGasPerBlob)
	}
}

func TestBlockAssemblerGasRemaining(t *testing.T) {
	config := BlockAssemblerConfig{
		GasLimit: 100_000,
		Timeout:  5 * time.Second,
	}
	ba := NewBlockAssembler(config)

	ba.Assemble([]*types.Transaction{
		makeAssemblyTx(0, 100, 21000),
	})

	remaining := ba.GasRemaining()
	if remaining != 79000 {
		t.Fatalf("GasRemaining: got %d, want 79000", remaining)
	}
}
