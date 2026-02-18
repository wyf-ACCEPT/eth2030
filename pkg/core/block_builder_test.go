package core

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/bal"
	"github.com/eth2028/eth2028/core/rawdb"
	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

// newLegacyBuilder creates a block builder for testing using the legacy interface.
func newLegacyBuilder(config *ChainConfig, statedb state.StateDB) *BlockBuilder {
	b := NewBlockBuilder(config, nil, nil)
	b.SetState(statedb)
	return b
}

// --- Existing Tests (updated to new API) ---

func TestBlockBuilderSimple(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0x01})
	receiver := types.BytesToAddress([]byte{0x02})

	statedb.AddBalance(sender, big.NewInt(10_000_000))

	builder := newLegacyBuilder(TestConfig, statedb)

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

	block, receipts, err := builder.BuildBlockLegacy(parent, txs, 1700000001, types.BytesToAddress([]byte{0xff}), nil)
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

	builder := newLegacyBuilder(TestConfig, statedb)

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

	block, receipts, err := builder.BuildBlockLegacy(parent, txs, 1700000001, types.BytesToAddress([]byte{0xff}), nil)
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

	builder := newLegacyBuilder(TestConfig, statedb)

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
	block, _, err := builder.BuildBlockLegacy(parent, []*types.Transaction{lowPriceTx, highPriceTx}, 1700000001, types.BytesToAddress([]byte{0xff}), nil)
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
	builder := newLegacyBuilder(TestConfig, statedb)

	parent := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 30_000_000,
		BaseFee:  big.NewInt(1),
	}

	block, receipts, err := builder.BuildBlockLegacy(parent, nil, 1700000001, types.BytesToAddress([]byte{0xff}), nil)
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

			newBaseFee := CalcBaseFee(parent)

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

// --- New Tests (Task 5) ---

// TestBuildBlock_Empty tests building an empty block via the new API.
func TestBuildBlock_Empty(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis(30_000_000, big.NewInt(1000))
	db := rawdb.NewMemoryDB()
	bc, err := NewBlockchain(TestConfig, genesis, statedb, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}

	// No txpool (nil) means no transactions available.
	builder := NewBlockBuilder(TestConfig, bc, nil)

	attrs := &BuildBlockAttributes{
		Timestamp:    12,
		FeeRecipient: types.BytesToAddress([]byte{0xff}),
		GasLimit:     30_000_000,
	}

	block, receipts, err := builder.BuildBlock(genesis.Header(), attrs)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}

	if len(block.Transactions()) != 0 {
		t.Errorf("expected 0 txs, got %d", len(block.Transactions()))
	}
	if len(receipts) != 0 {
		t.Errorf("expected 0 receipts, got %d", len(receipts))
	}
	if block.NumberU64() != 1 {
		t.Errorf("block number = %d, want 1", block.NumberU64())
	}
	if block.GasUsed() != 0 {
		t.Errorf("gas used = %d, want 0", block.GasUsed())
	}
	if block.Coinbase() != attrs.FeeRecipient {
		t.Errorf("coinbase = %v, want %v", block.Coinbase(), attrs.FeeRecipient)
	}
}

// mockTxPool implements TxPoolReader for testing.
type mockTxPool struct {
	txs []*types.Transaction
}

func (p *mockTxPool) Pending() []*types.Transaction {
	return p.txs
}

// TestBuildBlock_WithTransactions tests building a block with transactions from pool.
func TestBuildBlock_WithTransactions(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0x01})
	receiver := types.BytesToAddress([]byte{0x02})
	statedb.AddBalance(sender, big.NewInt(10_000_000))

	genesis := makeGenesis(30_000_000, big.NewInt(1))
	db := rawdb.NewMemoryDB()
	bc, err := NewBlockchain(TestConfig, genesis, statedb, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}

	// Create pending transactions.
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
	pool := &mockTxPool{txs: txs}

	builder := NewBlockBuilder(TestConfig, bc, pool)

	attrs := &BuildBlockAttributes{
		Timestamp:    12,
		FeeRecipient: types.BytesToAddress([]byte{0xff}),
		GasLimit:     30_000_000,
	}

	block, receipts, err := builder.BuildBlock(genesis.Header(), attrs)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
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
	if block.GasUsed() == 0 {
		t.Error("gas used should be > 0 with transactions")
	}
}

// TestBuildBlock_GasLimitEnforcement tests that the block builder respects gas limits.
func TestBuildBlock_GasLimitEnforcement(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0x01})
	receiver := types.BytesToAddress([]byte{0x02})
	statedb.AddBalance(sender, big.NewInt(100_000_000))

	genesis := makeGenesis(50000, big.NewInt(1))
	db := rawdb.NewMemoryDB()
	bc, err := NewBlockchain(TestConfig, genesis, statedb, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}

	// Create 5 txs each requiring 21000 gas.
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
	pool := &mockTxPool{txs: txs}

	builder := NewBlockBuilder(TestConfig, bc, pool)

	attrs := &BuildBlockAttributes{
		Timestamp:    12,
		FeeRecipient: types.BytesToAddress([]byte{0xff}),
		GasLimit:     50000, // Only room for ~2 transactions.
	}

	block, receipts, err := builder.BuildBlock(genesis.Header(), attrs)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}

	// 50000 / 21000 = 2.38, so at most 2 transactions fit.
	if len(block.Transactions()) > 2 {
		t.Errorf("expected at most 2 txs, got %d", len(block.Transactions()))
	}
	if len(receipts) != len(block.Transactions()) {
		t.Errorf("receipt count %d != tx count %d", len(receipts), len(block.Transactions()))
	}
	if block.GasUsed() > block.GasLimit() {
		t.Errorf("gas used %d exceeds gas limit %d", block.GasUsed(), block.GasLimit())
	}
}

// TestCalcBaseFee_Stable tests that base fee is unchanged when parent is at target.
func TestCalcBaseFee_Stable(t *testing.T) {
	parent := &types.Header{
		GasLimit: 30_000_000,
		GasUsed:  15_000_000, // exactly at target (limit/2)
		BaseFee:  big.NewInt(1000),
	}
	newBaseFee := CalcBaseFee(parent)
	if newBaseFee.Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("expected base fee 1000 (unchanged), got %s", newBaseFee)
	}
}

// TestCalcBaseFee_Increase tests base fee increases when parent is over target.
func TestCalcBaseFee_Increase(t *testing.T) {
	parent := &types.Header{
		GasLimit: 30_000_000,
		GasUsed:  25_000_000, // over target
		BaseFee:  big.NewInt(1000),
	}
	newBaseFee := CalcBaseFee(parent)
	if newBaseFee.Cmp(big.NewInt(1000)) <= 0 {
		t.Errorf("expected base fee increase, got %s (was 1000)", newBaseFee)
	}
	// Max increase is 12.5% => at most 1000 * 1.125 = 1125
	maxFee := big.NewInt(1125)
	if newBaseFee.Cmp(maxFee) > 0 {
		t.Errorf("base fee %s exceeds max increase 1125", newBaseFee)
	}
}

// TestCalcBaseFee_Decrease tests base fee decreases when parent is under target.
func TestCalcBaseFee_Decrease(t *testing.T) {
	parent := &types.Header{
		GasLimit: 30_000_000,
		GasUsed:  5_000_000, // under target
		BaseFee:  big.NewInt(1000),
	}
	newBaseFee := CalcBaseFee(parent)
	if newBaseFee.Cmp(big.NewInt(1000)) >= 0 {
		t.Errorf("expected base fee decrease, got %s (was 1000)", newBaseFee)
	}
	// Must not go below minimum of 1 wei.
	if newBaseFee.Cmp(big.NewInt(MinBaseFee)) < 0 {
		t.Errorf("base fee %s below minimum %d", newBaseFee, MinBaseFee)
	}
}

// TestCalcBaseFee_MinimumFloor tests that base fee never goes below 1 wei.
func TestCalcBaseFee_MinimumFloor(t *testing.T) {
	// With a very low base fee and empty block, the decrease should be
	// clamped at 1 wei minimum.
	parent := &types.Header{
		GasLimit: 30_000_000,
		GasUsed:  0, // empty block, maximum decrease
		BaseFee:  big.NewInt(1),
	}
	newBaseFee := CalcBaseFee(parent)
	if newBaseFee.Cmp(big.NewInt(1)) < 0 {
		t.Errorf("base fee %s below minimum 1 wei", newBaseFee)
	}
}

// TestReorg_Simple tests a basic chain reorganization.
func TestReorg_Simple(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis(30_000_000, big.NewInt(1))
	db := rawdb.NewMemoryDB()
	bc, err := NewBlockchain(TestConfig, genesis, statedb, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}

	// Build original chain: genesis -> A1 -> A2
	a1 := makeBlock(genesis, nil)
	a2 := makeBlock(a1, nil)
	if err := bc.InsertBlock(a1); err != nil {
		t.Fatalf("insert A1: %v", err)
	}
	if err := bc.InsertBlock(a2); err != nil {
		t.Fatalf("insert A2: %v", err)
	}

	if bc.CurrentBlock().NumberU64() != 2 {
		t.Fatalf("head = %d, want 2", bc.CurrentBlock().NumberU64())
	}

	// Build fork chain: genesis -> B1 -> B2 -> B3 (longer)
	// B1 needs different timestamp to get different hash.
	emptyBALHash := bal.NewBlockAccessList().Hash()
	b1Header := &types.Header{
		ParentHash:          genesis.Hash(),
		Number:              big.NewInt(1),
		GasLimit:            genesis.GasLimit(),
		GasUsed:             0,
		Time:                genesis.Time() + 6, // different timestamp
		Difficulty:          new(big.Int),
		BaseFee:             CalcBaseFee(genesis.Header()),
		UncleHash:           EmptyUncleHash,
		BlockAccessListHash: &emptyBALHash,
	}
	b1 := types.NewBlock(b1Header, nil)
	b2 := makeBlock(b1, nil)
	b3 := makeBlock(b2, nil)

	// Insert all fork blocks so they are known to the blockchain.
	// B1 is at the same height as A1 but InsertBlock still stores it.
	if err := bc.InsertBlock(b1); err != nil {
		t.Logf("b1 insert (side chain): %v", err)
	}
	// B2 is at the same height as A2.
	if err := bc.InsertBlock(b2); err != nil {
		t.Logf("b2 insert (side chain): %v", err)
	}
	// B3 extends to height 3.
	if err := bc.InsertBlock(b3); err != nil {
		t.Logf("b3 insert: %v", err)
	}

	// Now reorg to B chain (the longer chain).
	err = bc.Reorg(b3)
	if err != nil {
		t.Fatalf("Reorg: %v", err)
	}

	// Head should now be B3.
	if bc.CurrentBlock().NumberU64() != 3 {
		t.Errorf("head = %d, want 3", bc.CurrentBlock().NumberU64())
	}
	if bc.CurrentBlock().Hash() != b3.Hash() {
		t.Errorf("head hash mismatch after reorg")
	}

	// Canonical chain should follow B path.
	if got := bc.GetBlockByNumber(1); got == nil || got.Hash() != b1.Hash() {
		t.Errorf("canonical block 1 should be B1")
	}
	if got := bc.GetBlockByNumber(2); got == nil || got.Hash() != b2.Hash() {
		t.Errorf("canonical block 2 should be B2")
	}
	if got := bc.GetBlockByNumber(3); got == nil || got.Hash() != b3.Hash() {
		t.Errorf("canonical block 3 should be B3")
	}
}
