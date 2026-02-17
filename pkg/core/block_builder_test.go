package core

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

func TestBlockBuilderSimple(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0x01})
	receiver := types.BytesToAddress([]byte{0x02})

	statedb.AddBalance(sender, big.NewInt(10_000_000))

	builder := NewBlockBuilder(TestConfig, statedb)

	parent := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 30_000_000,
		GasUsed:  0,
		BaseFee:  big.NewInt(1),
	}

	// Create transactions.
	var txs []*types.Transaction
	for i := uint64(0); i < 3; i++ {
		tx := types.NewTransaction(&types.LegacyTx{
			Nonce:    i,
			GasPrice: big.NewInt(10),
			Gas:      21000,
			To:       &receiver,
			Value:    big.NewInt(100),
		})
		tx.SetSender(sender)
		txs = append(txs, tx)
	}

	block, receipts, err := builder.BuildBlock(parent, txs, 1700000001, types.BytesToAddress([]byte{0xff}), nil)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}

	if block.NumberU64() != 1 {
		t.Errorf("block number = %d, want 1", block.NumberU64())
	}
	if len(block.Transactions()) != 3 {
		t.Errorf("tx count = %d, want 3", len(block.Transactions()))
	}
	if len(receipts) != 3 {
		t.Errorf("receipt count = %d, want 3", len(receipts))
	}
	for i, r := range receipts {
		if r.Status != types.ReceiptStatusSuccessful {
			t.Errorf("receipt %d status = %d, want success", i, r.Status)
		}
	}
}

func TestBlockBuilderGasLimit(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0x01})
	receiver := types.BytesToAddress([]byte{0x02})

	statedb.AddBalance(sender, big.NewInt(100_000_000))

	builder := NewBlockBuilder(TestConfig, statedb)

	parent := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 50000, // Very small gas limit.
		GasUsed:  0,
		BaseFee:  big.NewInt(1),
	}

	// Create 5 txs, but only 2 should fit (50000 / 21000 = ~2.3).
	var txs []*types.Transaction
	for i := uint64(0); i < 5; i++ {
		tx := types.NewTransaction(&types.LegacyTx{
			Nonce:    i,
			GasPrice: big.NewInt(10),
			Gas:      21000,
			To:       &receiver,
			Value:    big.NewInt(1),
		})
		tx.SetSender(sender)
		txs = append(txs, tx)
	}

	block, receipts, err := builder.BuildBlock(parent, txs, 1700000001, types.BytesToAddress([]byte{0xff}), nil)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}

	// Gas limit is ~50000 (may change slightly due to calcGasLimit).
	// Should fit at most 2 transactions.
	if len(block.Transactions()) > 2 {
		t.Errorf("expected at most 2 txs, got %d", len(block.Transactions()))
	}
	if len(receipts) != len(block.Transactions()) {
		t.Errorf("receipt count %d != tx count %d", len(receipts), len(block.Transactions()))
	}
}

func TestBlockBuilderGasPriceOrdering(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender1 := types.BytesToAddress([]byte{0x01})
	sender2 := types.BytesToAddress([]byte{0x02})
	receiver := types.BytesToAddress([]byte{0x03})

	statedb.AddBalance(sender1, big.NewInt(100_000_000))
	statedb.AddBalance(sender2, big.NewInt(100_000_000))

	builder := NewBlockBuilder(TestConfig, statedb)

	parent := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 30_000_000,
		BaseFee:  big.NewInt(1),
	}

	// Create txs with different gas prices.
	lowPriceTx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      21000,
		To:       &receiver,
		Value:    big.NewInt(1),
	})
	lowPriceTx.SetSender(sender1)

	highPriceTx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(100),
		Gas:      21000,
		To:       &receiver,
		Value:    big.NewInt(1),
	})
	highPriceTx.SetSender(sender2)

	// Submit low price first, but high price should be included first.
	block, _, err := builder.BuildBlock(parent, []*types.Transaction{lowPriceTx, highPriceTx}, 1700000001, types.BytesToAddress([]byte{0xff}), nil)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}

	if len(block.Transactions()) != 2 {
		t.Fatalf("expected 2 txs, got %d", len(block.Transactions()))
	}

	// First tx should be the high-price one.
	firstTx := block.Transactions()[0]
	if firstTx.GasPrice().Cmp(big.NewInt(100)) != 0 {
		t.Errorf("first tx gas price = %s, want 100 (should be ordered by price)", firstTx.GasPrice())
	}
}

func TestBlockBuilderEmptyBlock(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	builder := NewBlockBuilder(TestConfig, statedb)

	parent := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 30_000_000,
		BaseFee:  big.NewInt(1),
	}

	block, receipts, err := builder.BuildBlock(parent, nil, 1700000001, types.BytesToAddress([]byte{0xff}), nil)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}

	if len(block.Transactions()) != 0 {
		t.Errorf("expected 0 txs, got %d", len(block.Transactions()))
	}
	if len(receipts) != 0 {
		t.Errorf("expected 0 receipts, got %d", len(receipts))
	}
}

func TestCalcBaseFee(t *testing.T) {
	tests := []struct {
		name     string
		gasLimit uint64
		gasUsed  uint64
		baseFee  int64
		expect   string // "increase", "decrease", "same"
	}{
		{"at target", 30_000_000, 15_000_000, 1000, "same"},
		{"above target", 30_000_000, 20_000_000, 1000, "increase"},
		{"below target", 30_000_000, 10_000_000, 1000, "decrease"},
		{"empty block", 30_000_000, 0, 1000, "decrease"},
		{"full block", 30_000_000, 30_000_000, 1000, "increase"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := &types.Header{
				GasLimit: tt.gasLimit,
				GasUsed:  tt.gasUsed,
				BaseFee:  big.NewInt(tt.baseFee),
			}

			newBaseFee := calcBaseFee(parent)

			switch tt.expect {
			case "increase":
				if newBaseFee.Cmp(big.NewInt(tt.baseFee)) <= 0 {
					t.Errorf("expected increase, got %s (was %d)", newBaseFee, tt.baseFee)
				}
			case "decrease":
				if newBaseFee.Cmp(big.NewInt(tt.baseFee)) >= 0 {
					t.Errorf("expected decrease, got %s (was %d)", newBaseFee, tt.baseFee)
				}
			case "same":
				if newBaseFee.Cmp(big.NewInt(tt.baseFee)) != 0 {
					t.Errorf("expected same, got %s (was %d)", newBaseFee, tt.baseFee)
				}
			}
		})
	}
}

func TestCalcGasLimit(t *testing.T) {
	// At target usage, gas limit stays roughly the same.
	limit := calcGasLimit(30_000_000, 15_000_000)
	if limit < 29_000_000 || limit > 31_000_000 {
		t.Errorf("gas limit = %d, expected ~30M", limit)
	}

	// High usage increases limit.
	limit = calcGasLimit(30_000_000, 25_000_000)
	if limit <= 30_000_000 {
		t.Errorf("expected increase, got %d", limit)
	}

	// Low usage decreases limit.
	limit = calcGasLimit(30_000_000, 5_000_000)
	if limit >= 30_000_000 {
		t.Errorf("expected decrease, got %d", limit)
	}
}

func TestEffectiveGasPrice(t *testing.T) {
	receiver := types.BytesToAddress([]byte{0x01})

	// Legacy tx: gas price is the effective price.
	legacyTx := types.NewTransaction(&types.LegacyTx{
		GasPrice: big.NewInt(100),
		Gas:      21000,
		To:       &receiver,
	})
	price := effectiveGasPrice(legacyTx, big.NewInt(50))
	if price.Cmp(big.NewInt(100)) != 0 {
		t.Errorf("legacy effective price = %s, want 100", price)
	}

	// EIP-1559 tx: effective = min(feeCap, baseFee + tip).
	eip1559Tx := types.NewTransaction(&types.DynamicFeeTx{
		GasTipCap: big.NewInt(10),
		GasFeeCap: big.NewInt(100),
		Gas:       21000,
		To:        &receiver,
	})
	// baseFee=50, tip=10 -> effective=60 (< feeCap=100)
	price = effectiveGasPrice(eip1559Tx, big.NewInt(50))
	if price.Cmp(big.NewInt(60)) != 0 {
		t.Errorf("1559 effective price = %s, want 60", price)
	}

	// baseFee=95, tip=10 -> effective=100 (capped at feeCap=100)
	price = effectiveGasPrice(eip1559Tx, big.NewInt(95))
	if price.Cmp(big.NewInt(100)) != 0 {
		t.Errorf("1559 capped price = %s, want 100", price)
	}
}
