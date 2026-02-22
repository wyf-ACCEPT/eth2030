package core

import (
	"math/big"
	"testing"

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
	sender := types.BytesToAddress([]byte{0xaa})
	receiver := types.BytesToAddress([]byte{0xab})

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
	sender := types.BytesToAddress([]byte{0xaa})
	receiver := types.BytesToAddress([]byte{0xab})

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
	sender1 := types.BytesToAddress([]byte{0xaa})
	sender2 := types.BytesToAddress([]byte{0xab})
	receiver := types.BytesToAddress([]byte{0xac})

	statedb.AddBalance(sender1, big.NewInt(100_000_000))
	statedb.AddBalance(sender2, big.NewInt(100_000_000))

	builder := newLegacyBuilder(TestConfig, statedb)

	parent := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 30_000_000,
		BaseFee:  big.NewInt(1),
	}

	// Create txs with different gas prices (both above the child base fee).
	lowPriceTx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(10), // above base fee (7)
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
	receiver := types.BytesToAddress([]byte{0xaa})

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
	sender := types.BytesToAddress([]byte{0xaa})
	receiver := types.BytesToAddress([]byte{0xab})
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
	sender := types.BytesToAddress([]byte{0xaa})
	receiver := types.BytesToAddress([]byte{0xab})
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
	// Use makeChainBlocks with a shared state copy so system-level state
	// (EIP-4788/EIP-2935) accumulates correctly across blocks.
	aBlocks := makeChainBlocks(genesis, 2, statedb.Copy())
	a1, a2 := aBlocks[0], aBlocks[1]
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
	// Use a different state copy so the fork produces different block hashes.
	bState := statedb.Copy()
	// Advance the fork state timestamp by building a block with a different
	// timestamp. We use makeBlockWithState for B1 to get a distinct hash,
	// then chain the rest via makeChainBlocks.
	b1Header := &types.Header{
		ParentHash: genesis.Hash(),
		Number:     big.NewInt(1),
		GasLimit:   genesis.GasLimit(),
		Time:       genesis.Time() + 6, // different timestamp -> different hash
		Difficulty: new(big.Int),
		BaseFee:    CalcBaseFee(genesis.Header()),
		UncleHash:  EmptyUncleHash,
	}
	// Process B1 through the state processor to get correct state root, etc.
	emptyWHash := types.EmptyRootHash
	b1Header.WithdrawalsHash = &emptyWHash
	zeroBlobGas := uint64(0)
	var pExcess, pUsed uint64
	if genesis.Header().ExcessBlobGas != nil {
		pExcess = *genesis.Header().ExcessBlobGas
	}
	if genesis.Header().BlobGasUsed != nil {
		pUsed = *genesis.Header().BlobGasUsed
	}
	b1ExcessBlobGas := CalcExcessBlobGas(pExcess, pUsed)
	b1Header.BlobGasUsed = &zeroBlobGas
	b1Header.ExcessBlobGas = &b1ExcessBlobGas
	b1BeaconRoot := types.EmptyRootHash
	b1Header.ParentBeaconRoot = &b1BeaconRoot
	b1RequestsHash := types.EmptyRootHash
	b1Header.RequestsHash = &b1RequestsHash
	// EIP-7706: calldata gas fields.
	b1CalldataGasUsed := uint64(0)
	var pCalldataExcess, pCalldataUsed uint64
	if genesis.Header().CalldataExcessGas != nil {
		pCalldataExcess = *genesis.Header().CalldataExcessGas
	}
	if genesis.Header().CalldataGasUsed != nil {
		pCalldataUsed = *genesis.Header().CalldataGasUsed
	}
	b1CalldataExcessGas := CalcCalldataExcessGas(pCalldataExcess, pCalldataUsed, genesis.Header().GasLimit)
	b1Header.CalldataGasUsed = &b1CalldataGasUsed
	b1Header.CalldataExcessGas = &b1CalldataExcessGas
	b1Body := &types.Body{Withdrawals: []*types.Withdrawal{}}
	b1Block := types.NewBlock(b1Header, b1Body)

	// Execute against bState to get correct fields.
	proc := NewStateProcessor(TestConfig)
	result, procErr := proc.ProcessWithBAL(b1Block, bState)
	if procErr == nil {
		b1Header.GasUsed = 0
		if result.BlockAccessList != nil {
			h := result.BlockAccessList.Hash()
			b1Header.BlockAccessListHash = &h
		}
		b1Header.Bloom = types.CreateBloom(result.Receipts)
		b1Header.ReceiptHash = deriveReceiptsRoot(result.Receipts)
		b1Header.Root = bState.GetRoot()
	}
	b1Header.TxHash = deriveTxsRoot(nil)
	b1 := types.NewBlock(b1Header, b1Body)

	// Build B2, B3 from B1 using makeChainBlocks with the already-advanced state.
	bRest := makeChainBlocks(b1, 2, bState)
	b2, b3 := bRest[0], bRest[1]

	// Insert all fork blocks.
	if err := bc.InsertBlock(b1); err != nil {
		t.Logf("b1 insert (side chain): %v", err)
	}
	if err := bc.InsertBlock(b2); err != nil {
		t.Logf("b2 insert (side chain): %v", err)
	}
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

// ================================================================================
// New comprehensive tests for block builder enhancements.
// ================================================================================

// TestBuildBlock_MixedTransactionTypes tests building a block with legacy, EIP-1559,
// and blob transactions. Verifies that all types are included and properly ordered.
func TestBuildBlock_MixedTransactionTypes(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender1 := types.BytesToAddress([]byte{0xaa})
	sender2 := types.BytesToAddress([]byte{0xab})
	sender3 := types.BytesToAddress([]byte{0xac})
	receiver := types.BytesToAddress([]byte{0xad})
	blobReceiver := types.BytesToAddress([]byte{0xae})

	// Fund senders generously.
	statedb.AddBalance(sender1, big.NewInt(100_000_000_000))
	statedb.AddBalance(sender2, big.NewInt(100_000_000_000))
	statedb.AddBalance(sender3, big.NewInt(100_000_000_000))

	builder := newLegacyBuilder(TestConfig, statedb)

	parent := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 30_000_000,
		GasUsed:  0,
		BaseFee:  big.NewInt(1),
	}

	// Legacy tx from sender1 (effective price = 50).
	legacyTx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(50),
		Gas:      21000,
		To:       &receiver,
		Value:    big.NewInt(100),
	})
	legacyTx.SetSender(sender1)

	// EIP-1559 tx from sender2 (effective price = min(200, 1 + 100) = 101).
	dynTx := types.NewTransaction(&types.DynamicFeeTx{
		Nonce:     0,
		GasTipCap: big.NewInt(100),
		GasFeeCap: big.NewInt(200),
		Gas:       21000,
		To:        &receiver,
		Value:     big.NewInt(100),
	})
	dynTx.SetSender(sender2)

	// EIP-4844 blob tx from sender3 with valid versioned hash.
	blobHash := types.Hash{}
	blobHash[0] = BlobTxHashVersion // 0x01 version byte
	blobTx := types.NewTransaction(&types.BlobTx{
		Nonce:      0,
		GasTipCap:  big.NewInt(10),
		GasFeeCap:  big.NewInt(50),
		Gas:        21000,
		To:         blobReceiver,
		Value:      big.NewInt(1),
		BlobFeeCap: big.NewInt(100), // generous blob fee cap
		BlobHashes: []types.Hash{blobHash},
	})
	blobTx.SetSender(sender3)

	// Submit in arbitrary order.
	allTxs := []*types.Transaction{blobTx, legacyTx, dynTx}
	block, receipts, err := builder.BuildBlockLegacy(parent, allTxs, 1700000001, types.BytesToAddress([]byte{0xff}), nil)
	if err != nil {
		t.Fatalf("BuildBlockLegacy: %v", err)
	}

	// All three transactions should be included.
	if len(block.Transactions()) != 3 {
		t.Fatalf("tx count = %d, want 3", len(block.Transactions()))
	}
	if len(receipts) != 3 {
		t.Fatalf("receipt count = %d, want 3", len(receipts))
	}

	// Verify ordering: regular txs should come before blob txs.
	// Among regular txs, EIP-1559 (effective price=101) comes first, then legacy (50).
	firstTx := block.Transactions()[0]
	if firstTx.Type() == types.BlobTxType {
		t.Error("first transaction should not be a blob tx; regular txs come first")
	}

	// The last transaction should be the blob tx (sorted separately after regulars).
	lastTx := block.Transactions()[2]
	if lastTx.Type() != types.BlobTxType {
		t.Errorf("last transaction should be blob tx, got type %d", lastTx.Type())
	}
}

// TestBuildBlock_BlobGasLimitEnforcement tests that the builder enforces
// MAX_BLOB_GAS_PER_BLOCK = 786432 (6 blobs * 131072 gas each).
func TestBuildBlock_BlobGasLimitEnforcement(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	receiver := types.BytesToAddress([]byte{0xae})

	// Create 4 senders with 3 blobs each (12 blobs total, but max is 6).
	type senderTx struct {
		sender types.Address
		tx     *types.Transaction
	}
	var senderTxs []senderTx
	for i := 0; i < 4; i++ {
		sender := types.BytesToAddress([]byte{byte(0x10 + i)})
		statedb.AddBalance(sender, big.NewInt(100_000_000_000))

		// Create blob hashes with valid version byte.
		blobHashes := make([]types.Hash, 3) // 3 blobs per tx
		for j := range blobHashes {
			blobHashes[j][0] = BlobTxHashVersion
			blobHashes[j][1] = byte(i)
			blobHashes[j][2] = byte(j)
		}

		tx := types.NewTransaction(&types.BlobTx{
			Nonce:      0,
			GasTipCap:  big.NewInt(int64(50 - i*10)), // decreasing priority
			GasFeeCap:  big.NewInt(100),
			Gas:        21000,
			To:         receiver,
			Value:      big.NewInt(1),
			BlobFeeCap: big.NewInt(100),
			BlobHashes: blobHashes,
		})
		tx.SetSender(sender)
		senderTxs = append(senderTxs, senderTx{sender, tx})
	}

	builder := newLegacyBuilder(TestConfig, statedb)

	parent := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 30_000_000,
		GasUsed:  0,
		BaseFee:  big.NewInt(1),
	}

	var txs []*types.Transaction
	for _, st := range senderTxs {
		txs = append(txs, st.tx)
	}

	block, _, err := builder.BuildBlockLegacy(parent, txs, 1700000001, types.BytesToAddress([]byte{0xff}), nil)
	if err != nil {
		t.Fatalf("BuildBlockLegacy: %v", err)
	}

	// Each blob tx uses 3 * 131072 = 393216 blob gas.
	// Max per block is 786432, so at most 2 txs fit (2 * 393216 = 786432).
	var totalBlobGas uint64
	var blobTxCount int
	for _, tx := range block.Transactions() {
		if tx.Type() == types.BlobTxType {
			totalBlobGas += tx.BlobGas()
			blobTxCount++
		}
	}

	if totalBlobGas > MaxBlobGasPerBlock {
		t.Errorf("total blob gas %d exceeds max %d", totalBlobGas, MaxBlobGasPerBlock)
	}
	if blobTxCount > 2 {
		t.Errorf("expected at most 2 blob txs (6 blobs), got %d", blobTxCount)
	}
	if blobTxCount < 2 {
		t.Errorf("expected 2 blob txs to fit, got %d", blobTxCount)
	}
}

// TestBuildBlock_BlobHashValidation tests that blob transactions with invalid
// versioned hash bytes are rejected during block building.
func TestBuildBlock_BlobHashValidation(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0xaa})
	receiver := types.BytesToAddress([]byte{0xab})
	statedb.AddBalance(sender, big.NewInt(100_000_000_000))

	builder := newLegacyBuilder(TestConfig, statedb)

	parent := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 30_000_000,
		GasUsed:  0,
		BaseFee:  big.NewInt(1),
	}

	// Create a blob tx with an invalid version byte (0x00 instead of 0x01).
	invalidBlobHash := types.Hash{}
	invalidBlobHash[0] = 0x00 // wrong version
	badBlobTx := types.NewTransaction(&types.BlobTx{
		Nonce:      0,
		GasTipCap:  big.NewInt(10),
		GasFeeCap:  big.NewInt(50),
		Gas:        21000,
		To:         receiver,
		Value:      big.NewInt(1),
		BlobFeeCap: big.NewInt(100),
		BlobHashes: []types.Hash{invalidBlobHash},
	})
	badBlobTx.SetSender(sender)

	block, _, err := builder.BuildBlockLegacy(parent, []*types.Transaction{badBlobTx}, 1700000001, types.BytesToAddress([]byte{0xff}), nil)
	if err != nil {
		t.Fatalf("BuildBlockLegacy: %v", err)
	}

	// The invalid blob tx should not be included.
	for _, tx := range block.Transactions() {
		if tx.Type() == types.BlobTxType {
			t.Error("blob tx with invalid version byte should not be included")
		}
	}
}

// TestBuildBlock_ExcessBlobGasCalculation tests that the builder correctly
// computes excess blob gas from the parent header.
func TestBuildBlock_ExcessBlobGasCalculation(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	builder := newLegacyBuilder(TestConfig, statedb)

	// Parent with 6 blobs used (786432 blob gas) and 0 excess.
	parentBlobGasUsed := uint64(786432)
	parentExcessBlobGas := uint64(0)
	parent := &types.Header{
		Number:        big.NewInt(0),
		GasLimit:      30_000_000,
		GasUsed:       0,
		BaseFee:       big.NewInt(1),
		BlobGasUsed:   &parentBlobGasUsed,
		ExcessBlobGas: &parentExcessBlobGas,
	}

	block, _, err := builder.BuildBlockLegacy(parent, nil, 1700000001, types.BytesToAddress([]byte{0xff}), nil)
	if err != nil {
		t.Fatalf("BuildBlockLegacy: %v", err)
	}

	// Expected excess = (0 + 786432) - 393216 = 393216
	h := block.Header()
	if h.ExcessBlobGas == nil {
		t.Fatal("ExcessBlobGas should be set on Cancun block")
	}
	expectedExcess := CalcExcessBlobGas(parentExcessBlobGas, parentBlobGasUsed)
	if *h.ExcessBlobGas != expectedExcess {
		t.Errorf("ExcessBlobGas = %d, want %d", *h.ExcessBlobGas, expectedExcess)
	}
}

// TestBuildBlock_BaseFeeCalculation verifies the EIP-1559 base fee calculation
// with specific values: elasticity=2, denominator=8.
func TestBuildBlock_BaseFeeCalculation(t *testing.T) {
	tests := []struct {
		name           string
		parentGasLimit uint64
		parentGasUsed  uint64
		parentBaseFee  int64
		wantBaseFee    int64
	}{
		{
			name:           "at_target_unchanged",
			parentGasLimit: 30_000_000,
			parentGasUsed:  15_000_000,
			parentBaseFee:  1000,
			wantBaseFee:    1000,
		},
		{
			name:           "full_block_max_increase",
			parentGasLimit: 30_000_000,
			parentGasUsed:  30_000_000,
			// delta = (30M - 15M) / 15M / 8 * 1000 = 1000/8 = 125
			// new = 1000 + 125 = 1125
			parentBaseFee: 1000,
			wantBaseFee:   1125,
		},
		{
			name:           "empty_block_max_decrease",
			parentGasLimit: 30_000_000,
			parentGasUsed:  0,
			// delta = 15M / 15M / 8 * 1000 = 125
			// new = 1000 - 125 = 875
			parentBaseFee: 1000,
			wantBaseFee:   875,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := &types.Header{
				GasLimit: tt.parentGasLimit,
				GasUsed:  tt.parentGasUsed,
				BaseFee:  big.NewInt(tt.parentBaseFee),
			}
			result := CalcBaseFee(parent)
			if result.Int64() != tt.wantBaseFee {
				t.Errorf("CalcBaseFee = %d, want %d", result.Int64(), tt.wantBaseFee)
			}
		})
	}
}

// TestBuildBlock_WithdrawalProcessing tests that withdrawals are applied
// correctly during block building: recipient balances are credited and
// the withdrawals root is included in the header.
func TestBuildBlock_WithdrawalProcessing(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis(30_000_000, big.NewInt(1000))
	db := rawdb.NewMemoryDB()
	bc, err := NewBlockchain(TestConfig, genesis, statedb, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}

	validator1 := types.BytesToAddress([]byte{0xaa})
	validator2 := types.BytesToAddress([]byte{0xbb})

	withdrawals := []*types.Withdrawal{
		{
			Index:          0,
			ValidatorIndex: 100,
			Address:        validator1,
			Amount:         1_000_000, // 1M Gwei = 1e15 wei = 0.001 ETH
		},
		{
			Index:          1,
			ValidatorIndex: 200,
			Address:        validator2,
			Amount:         2_000_000, // 2M Gwei = 2e15 wei = 0.002 ETH
		},
	}

	builder := NewBlockBuilder(TestConfig, bc, nil)

	attrs := &BuildBlockAttributes{
		Timestamp:    12,
		FeeRecipient: types.BytesToAddress([]byte{0xff}),
		GasLimit:     30_000_000,
		Withdrawals:  withdrawals,
	}

	block, _, err := builder.BuildBlock(genesis.Header(), attrs)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}

	// Verify withdrawals are in the block body.
	if block.Withdrawals() == nil {
		t.Fatal("block should have withdrawals")
	}
	if len(block.Withdrawals()) != 2 {
		t.Fatalf("withdrawal count = %d, want 2", len(block.Withdrawals()))
	}

	// Verify withdrawals root is set in header.
	h := block.Header()
	if h.WithdrawalsHash == nil {
		t.Fatal("WithdrawalsHash should be set")
	}
	if h.WithdrawalsHash.IsZero() {
		t.Error("WithdrawalsHash should not be zero")
	}

	// Verify the hash matches a recomputation.
	expectedHash := deriveWithdrawalsRoot(withdrawals)
	if *h.WithdrawalsHash != expectedHash {
		t.Errorf("WithdrawalsHash mismatch: got %s, want %s",
			h.WithdrawalsHash.Hex(), expectedHash.Hex())
	}
}

// TestBuildBlock_WithdrawalBalanceCredits verifies that withdrawal recipients
// receive the correct balance (converted from Gwei to Wei).
func TestBuildBlock_WithdrawalBalanceCredits(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	recipient := types.BytesToAddress([]byte{0xcc})

	// Verify recipient starts with zero balance.
	if statedb.GetBalance(recipient).Sign() != 0 {
		t.Fatal("recipient should start with zero balance")
	}

	parent := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 30_000_000,
		GasUsed:  0,
		BaseFee:  big.NewInt(1),
	}

	// Use BuildBlock (not legacy) to process withdrawals via attributes.
	// We set state directly since we are using newLegacyBuilder.
	attrs := &BuildBlockAttributes{
		Timestamp:    1700000001,
		FeeRecipient: types.BytesToAddress([]byte{0xff}),
		GasLimit:     30_000_000,
		Withdrawals: []*types.Withdrawal{
			{
				Index:          0,
				ValidatorIndex: 42,
				Address:        recipient,
				Amount:         5_000_000_000, // 5 Gwei * 1e9 = 5e18 wei = 5 ETH
			},
		},
	}

	bb := NewBlockBuilder(TestConfig, nil, nil)
	bb.SetState(statedb)

	_, _, err := bb.BuildBlock(parent, attrs)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}

	// 5_000_000_000 Gwei = 5 * 10^18 wei
	expectedBalance := new(big.Int).Mul(
		big.NewInt(5_000_000_000),
		big.NewInt(1_000_000_000),
	)
	balance := statedb.GetBalance(recipient)
	if balance.Cmp(expectedBalance) != 0 {
		t.Errorf("recipient balance = %s, want %s", balance, expectedBalance)
	}
}

// TestBuildBlock_TransactionOrdering_EffectiveGasPrice tests that transactions
// are ordered by effective gas price (considering base fee).
func TestBuildBlock_TransactionOrdering_EffectiveGasPrice(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender1 := types.BytesToAddress([]byte{0xaa})
	sender2 := types.BytesToAddress([]byte{0xab})
	sender3 := types.BytesToAddress([]byte{0xac})
	receiver := types.BytesToAddress([]byte{0xad})

	statedb.AddBalance(sender1, big.NewInt(100_000_000_000))
	statedb.AddBalance(sender2, big.NewInt(100_000_000_000))
	statedb.AddBalance(sender3, big.NewInt(100_000_000_000))

	builder := newLegacyBuilder(TestConfig, statedb)

	baseFee := big.NewInt(10)
	parent := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 30_000_000,
		GasUsed:  0,
		BaseFee:  baseFee,
	}

	// Tx A: legacy, gasPrice=20 (effective=20)
	txA := types.NewTransaction(&types.LegacyTx{
		Nonce: 0, GasPrice: big.NewInt(20), Gas: 21000,
		To: &receiver, Value: big.NewInt(1),
	})
	txA.SetSender(sender1)

	// Tx B: EIP-1559, feeCap=100, tip=50 -> effective = min(100, 10+50) = 60
	txB := types.NewTransaction(&types.DynamicFeeTx{
		Nonce: 0, GasTipCap: big.NewInt(50), GasFeeCap: big.NewInt(100), Gas: 21000,
		To: &receiver, Value: big.NewInt(1),
	})
	txB.SetSender(sender2)

	// Tx C: legacy, gasPrice=40 (effective=40)
	txC := types.NewTransaction(&types.LegacyTx{
		Nonce: 0, GasPrice: big.NewInt(40), Gas: 21000,
		To: &receiver, Value: big.NewInt(1),
	})
	txC.SetSender(sender3)

	// Submit in reverse order of priority.
	block, _, err := builder.BuildBlockLegacy(parent,
		[]*types.Transaction{txA, txC, txB},
		1700000001, types.BytesToAddress([]byte{0xff}), nil)
	if err != nil {
		t.Fatalf("BuildBlockLegacy: %v", err)
	}

	if len(block.Transactions()) != 3 {
		t.Fatalf("expected 3 txs, got %d", len(block.Transactions()))
	}

	// Expected order by effective gas price (descending): B(60), C(40), A(20).
	prices := make([]*big.Int, len(block.Transactions()))
	for i, tx := range block.Transactions() {
		prices[i] = effectiveGasPrice(tx, CalcBaseFee(parent))
	}

	for i := 0; i < len(prices)-1; i++ {
		if prices[i].Cmp(prices[i+1]) < 0 {
			t.Errorf("tx %d effective price %s < tx %d effective price %s (should be descending)",
				i, prices[i], i+1, prices[i+1])
		}
	}
}

// TestBuildBlock_BlobTxsSeparateFromRegular tests that blob transactions are
// processed after all regular transactions, maintaining separate ordering pools.
func TestBuildBlock_BlobTxsSeparateFromRegular(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender1 := types.BytesToAddress([]byte{0xaa})
	sender2 := types.BytesToAddress([]byte{0xab})
	receiver := types.BytesToAddress([]byte{0xac})
	blobReceiver := types.BytesToAddress([]byte{0xad})

	statedb.AddBalance(sender1, big.NewInt(100_000_000_000))
	statedb.AddBalance(sender2, big.NewInt(100_000_000_000))

	builder := newLegacyBuilder(TestConfig, statedb)

	parent := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 30_000_000,
		GasUsed:  0,
		BaseFee:  big.NewInt(1),
	}

	// Blob tx with a very high tip (but should still come after regular txs).
	blobHash := types.Hash{}
	blobHash[0] = BlobTxHashVersion
	blobTx := types.NewTransaction(&types.BlobTx{
		Nonce:      0,
		GasTipCap:  big.NewInt(1000), // very high tip
		GasFeeCap:  big.NewInt(2000),
		Gas:        21000,
		To:         blobReceiver,
		Value:      big.NewInt(1),
		BlobFeeCap: big.NewInt(100),
		BlobHashes: []types.Hash{blobHash},
	})
	blobTx.SetSender(sender1)

	// Regular tx with a moderate tip (but still above base fee).
	regularTx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(10), // above base fee but lower than blob tx tip
		Gas:      21000,
		To:       &receiver,
		Value:    big.NewInt(1),
	})
	regularTx.SetSender(sender2)

	block, _, err := builder.BuildBlockLegacy(parent,
		[]*types.Transaction{blobTx, regularTx},
		1700000001, types.BytesToAddress([]byte{0xff}), nil)
	if err != nil {
		t.Fatalf("BuildBlockLegacy: %v", err)
	}

	if len(block.Transactions()) != 2 {
		t.Fatalf("expected 2 txs, got %d", len(block.Transactions()))
	}

	// Regular tx should come first, even though blob tx has higher tip.
	if block.Transactions()[0].Type() == types.BlobTxType {
		t.Error("regular tx should be ordered before blob tx")
	}
	if block.Transactions()[1].Type() != types.BlobTxType {
		t.Error("blob tx should come after regular tx")
	}
}

// TestBuildBlock_RequestsHash tests that the block builder sets the requests
// hash in the header when Prague is active (EIP-7685).
func TestBuildBlock_RequestsHash(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis(30_000_000, big.NewInt(1000))
	db := rawdb.NewMemoryDB()
	bc, err := NewBlockchain(TestConfig, genesis, statedb, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}

	builder := NewBlockBuilder(TestConfig, bc, nil)

	attrs := &BuildBlockAttributes{
		Timestamp:    12,
		FeeRecipient: types.BytesToAddress([]byte{0xff}),
		GasLimit:     30_000_000,
	}

	block, _, err := builder.BuildBlock(genesis.Header(), attrs)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}

	h := block.Header()

	// Prague is active in TestConfig, so requests hash should be set.
	if h.RequestsHash == nil {
		t.Fatal("RequestsHash should be set for Prague block")
	}
	if h.RequestsHash.IsZero() {
		t.Error("RequestsHash should not be zero")
	}
}

// TestBuildBlock_BlobGasHeaderFields tests that the block header contains
// correct BlobGasUsed and ExcessBlobGas fields.
func TestBuildBlock_BlobGasHeaderFields(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0xaa})
	receiver := types.BytesToAddress([]byte{0xab})
	statedb.AddBalance(sender, big.NewInt(100_000_000_000))

	builder := newLegacyBuilder(TestConfig, statedb)

	parent := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 30_000_000,
		GasUsed:  0,
		BaseFee:  big.NewInt(1),
	}

	// Create a valid blob tx with 2 blobs.
	blobHash1 := types.Hash{}
	blobHash1[0] = BlobTxHashVersion
	blobHash1[1] = 0x01
	blobHash2 := types.Hash{}
	blobHash2[0] = BlobTxHashVersion
	blobHash2[1] = 0x02

	blobTx := types.NewTransaction(&types.BlobTx{
		Nonce:      0,
		GasTipCap:  big.NewInt(10),
		GasFeeCap:  big.NewInt(50),
		Gas:        21000,
		To:         receiver,
		Value:      big.NewInt(1),
		BlobFeeCap: big.NewInt(100),
		BlobHashes: []types.Hash{blobHash1, blobHash2},
	})
	blobTx.SetSender(sender)

	block, _, err := builder.BuildBlockLegacy(parent, []*types.Transaction{blobTx}, 1700000001, types.BytesToAddress([]byte{0xff}), nil)
	if err != nil {
		t.Fatalf("BuildBlockLegacy: %v", err)
	}

	h := block.Header()

	// BlobGasUsed should be set for Cancun blocks.
	if h.BlobGasUsed == nil {
		t.Fatal("BlobGasUsed should be set")
	}
	// 2 blobs * 131072 gas each = 262144.
	expectedBlobGas := uint64(2 * GasPerBlob)
	if *h.BlobGasUsed != expectedBlobGas {
		t.Errorf("BlobGasUsed = %d, want %d", *h.BlobGasUsed, expectedBlobGas)
	}

	// ExcessBlobGas should also be set.
	if h.ExcessBlobGas == nil {
		t.Fatal("ExcessBlobGas should be set")
	}
}

// TestBuildBlock_SkipTxExceedingGasLimit tests that individual transactions
// exceeding the remaining block gas are skipped rather than causing failure.
func TestBuildBlock_SkipTxExceedingGasLimit(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender1 := types.BytesToAddress([]byte{0xaa})
	sender2 := types.BytesToAddress([]byte{0xab})
	receiver := types.BytesToAddress([]byte{0xac})

	statedb.AddBalance(sender1, big.NewInt(100_000_000_000))
	statedb.AddBalance(sender2, big.NewInt(100_000_000_000))

	builder := newLegacyBuilder(TestConfig, statedb)

	parent := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 50000, // tight limit
		GasUsed:  0,
		BaseFee:  big.NewInt(1),
	}

	// Tx1: fits (21000 gas).
	tx1 := types.NewTransaction(&types.LegacyTx{
		Nonce: 0, GasPrice: big.NewInt(100), Gas: 21000,
		To: &receiver, Value: big.NewInt(1),
	})
	tx1.SetSender(sender1)

	// Tx2: claims to need 100000 gas (won't fit).
	tx2 := types.NewTransaction(&types.LegacyTx{
		Nonce: 0, GasPrice: big.NewInt(200), Gas: 100000,
		To: &receiver, Value: big.NewInt(1),
	})
	tx2.SetSender(sender2)

	block, _, err := builder.BuildBlockLegacy(parent,
		[]*types.Transaction{tx1, tx2},
		1700000001, types.BytesToAddress([]byte{0xff}), nil)
	if err != nil {
		t.Fatalf("BuildBlockLegacy: %v", err)
	}

	// tx2 (100k gas) won't fit after calcGasLimit reduces the limit from 50000.
	// tx1 (21k gas) may fit depending on the exact limit.
	// The key assertion: no transaction exceeds the block gas limit.
	if block.GasUsed() > block.GasLimit() {
		t.Errorf("gas used %d > gas limit %d", block.GasUsed(), block.GasLimit())
	}
}

// TestCalcExcessBlobGas_FromParent tests the excess blob gas calculation.
func TestCalcExcessBlobGas_FromParent(t *testing.T) {
	tests := []struct {
		name           string
		parentExcess   uint64
		parentUsed     uint64
		expectedExcess uint64
	}{
		{
			name:           "zero_excess_zero_used",
			parentExcess:   0,
			parentUsed:     0,
			expectedExcess: 0, // 0 + 0 < target => 0
		},
		{
			name:           "zero_excess_full_blobs",
			parentExcess:   0,
			parentUsed:     786432, // 6 blobs
			expectedExcess: 786432 - TargetBlobGasPerBlock,
		},
		{
			name:           "below_target_stays_zero",
			parentExcess:   0,
			parentUsed:     131072, // 1 blob
			expectedExcess: 0,      // 131072 < target
		},
		{
			name:           "accumulating_excess",
			parentExcess:   200000,
			parentUsed:     786432,
			expectedExcess: 200000 + 786432 - TargetBlobGasPerBlock,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalcExcessBlobGas(tt.parentExcess, tt.parentUsed)
			if result != tt.expectedExcess {
				t.Errorf("CalcExcessBlobGas(%d, %d) = %d, want %d",
					tt.parentExcess, tt.parentUsed, result, tt.expectedExcess)
			}
		})
	}
}

// TestValidateBlobHashes tests the validateBlobHashes helper.
func TestValidateBlobHashes(t *testing.T) {
	// Valid hashes.
	validHash := types.Hash{}
	validHash[0] = 0x01
	if err := validateBlobHashes([]types.Hash{validHash}); err != nil {
		t.Errorf("valid hash should pass: %v", err)
	}

	// Invalid version byte.
	invalidHash := types.Hash{}
	invalidHash[0] = 0x02
	if err := validateBlobHashes([]types.Hash{invalidHash}); err == nil {
		t.Error("invalid hash version should fail")
	}

	// Empty slice is valid.
	if err := validateBlobHashes(nil); err != nil {
		t.Errorf("empty hashes should pass: %v", err)
	}
}

// TestCalldataFloorDelta tests the EIP-7623 calldata floor delta calculation.
func TestCalldataFloorDelta(t *testing.T) {
	receiver := types.BytesToAddress([]byte{0xaa})

	// Transaction with no calldata: floor = 21000, no delta expected.
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce: 0, GasPrice: big.NewInt(10), Gas: 21000,
		To: &receiver, Value: big.NewInt(1),
	})
	delta := calldataFloorDelta(tx, 21000)
	if delta != 0 {
		t.Errorf("expected delta=0 for empty calldata, got %d", delta)
	}

	// Transaction with large calldata.
	largeData := make([]byte, 1000)
	for i := range largeData {
		largeData[i] = 0xff // all non-zero
	}
	txWithData := types.NewTransaction(&types.LegacyTx{
		Nonce: 0, GasPrice: big.NewInt(10), Gas: 100000,
		To: &receiver, Value: big.NewInt(1), Data: largeData,
	})
	// Floor = 21000 + 1000 * 4 tokens * 10 per token = 21000 + 40000 = 61000
	// If standard gas used is say 30000, delta = 61000 - 30000 = 31000.
	delta = calldataFloorDelta(txWithData, 30000)
	expectedFloor := uint64(21000 + 1000*4*TotalCostFloorPerToken)
	expectedDelta := expectedFloor - 30000
	if delta != expectedDelta {
		t.Errorf("calldataFloorDelta = %d, want %d (floor=%d, standardUsed=30000)",
			delta, expectedDelta, expectedFloor)
	}

	// When standard gas exceeds floor, delta should be 0.
	delta = calldataFloorDelta(txWithData, 100000)
	if delta != 0 {
		t.Errorf("expected delta=0 when standard gas exceeds floor, got %d", delta)
	}
}

// TestSortedTxLists tests the transaction sorting helper that separates
// regular and blob transactions.
func TestSortedTxLists(t *testing.T) {
	receiver := types.BytesToAddress([]byte{0xaa})
	blobReceiver := types.BytesToAddress([]byte{0xab})

	legacyTx := types.NewTransaction(&types.LegacyTx{
		Nonce: 0, GasPrice: big.NewInt(10), Gas: 21000,
		To: &receiver, Value: big.NewInt(1),
	})

	dynTx := types.NewTransaction(&types.DynamicFeeTx{
		Nonce: 0, GasTipCap: big.NewInt(5), GasFeeCap: big.NewInt(50), Gas: 21000,
		To: &receiver, Value: big.NewInt(1),
	})

	blobHash := types.Hash{}
	blobHash[0] = BlobTxHashVersion
	blobTx := types.NewTransaction(&types.BlobTx{
		Nonce: 0, GasTipCap: big.NewInt(20), GasFeeCap: big.NewInt(100),
		Gas: 21000, To: blobReceiver, Value: big.NewInt(1),
		BlobFeeCap: big.NewInt(10), BlobHashes: []types.Hash{blobHash},
	})

	regular, blobs := sortedTxLists(
		[]*types.Transaction{blobTx, legacyTx, dynTx},
		big.NewInt(1),
	)

	if len(regular) != 2 {
		t.Errorf("regular tx count = %d, want 2", len(regular))
	}
	if len(blobs) != 1 {
		t.Errorf("blob tx count = %d, want 1", len(blobs))
	}

	// Regular txs should be sorted by effective gas price descending.
	if len(regular) == 2 {
		p0 := effectiveGasPrice(regular[0], big.NewInt(1))
		p1 := effectiveGasPrice(regular[1], big.NewInt(1))
		if p0.Cmp(p1) < 0 {
			t.Error("regular txs should be sorted by effective gas price descending")
		}
	}
}

// TestBuildBlock_NoBlobGasFieldsPreCancun tests that blob gas fields are not
// set on blocks when Cancun is not active.
func TestBuildBlock_NoBlobGasFieldsPreCancun(t *testing.T) {
	// Create a config where Cancun is not active.
	preCancunConfig := &ChainConfig{
		ChainID:                 big.NewInt(1337),
		HomesteadBlock:          big.NewInt(0),
		EIP150Block:             big.NewInt(0),
		EIP155Block:             big.NewInt(0),
		EIP158Block:             big.NewInt(0),
		ByzantiumBlock:          big.NewInt(0),
		ConstantinopleBlock:     big.NewInt(0),
		PetersburgBlock:         big.NewInt(0),
		IstanbulBlock:           big.NewInt(0),
		BerlinBlock:             big.NewInt(0),
		LondonBlock:             big.NewInt(0),
		TerminalTotalDifficulty: big.NewInt(0),
		ShanghaiTime:            newUint64(0),
		CancunTime:              nil, // Cancun not active
		PragueTime:              nil,
	}

	statedb := state.NewMemoryStateDB()
	builder := NewBlockBuilder(preCancunConfig, nil, nil)
	builder.SetState(statedb)

	parent := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 30_000_000,
		GasUsed:  0,
		BaseFee:  big.NewInt(1),
	}

	attrs := &BuildBlockAttributes{
		Timestamp:    12,
		FeeRecipient: types.BytesToAddress([]byte{0xff}),
		GasLimit:     30_000_000,
	}

	block, _, err := builder.BuildBlock(parent, attrs)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}

	h := block.Header()
	if h.BlobGasUsed != nil {
		t.Error("BlobGasUsed should be nil pre-Cancun")
	}
	if h.ExcessBlobGas != nil {
		t.Error("ExcessBlobGas should be nil pre-Cancun")
	}
}

// TestBuildBlock_EmptyBlockBlobGasZero tests that an empty Cancun block
// has BlobGasUsed = 0.
func TestBuildBlock_EmptyBlockBlobGasZero(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	builder := newLegacyBuilder(TestConfig, statedb)

	parent := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 30_000_000,
		GasUsed:  0,
		BaseFee:  big.NewInt(1),
	}

	block, _, err := builder.BuildBlockLegacy(parent, nil, 1700000001, types.BytesToAddress([]byte{0xff}), nil)
	if err != nil {
		t.Fatalf("BuildBlockLegacy: %v", err)
	}

	h := block.Header()
	if h.BlobGasUsed == nil {
		t.Fatal("BlobGasUsed should be set on Cancun block (TestConfig has Cancun active)")
	}
	if *h.BlobGasUsed != 0 {
		t.Errorf("BlobGasUsed = %d, want 0 for empty block", *h.BlobGasUsed)
	}
}

// TestBuildBlock_BaseFeeFiltersTxs tests that transactions with gasFeeCap
// below the base fee are not included in the block.
func TestBuildBlock_BaseFeeFiltersTxs(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0xaa})
	receiver := types.BytesToAddress([]byte{0xab})
	statedb.AddBalance(sender, big.NewInt(100_000_000_000))

	builder := newLegacyBuilder(TestConfig, statedb)

	// Use a parent that will produce a very high base fee.
	parent := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 30_000_000,
		GasUsed:  30_000_000, // full block -> maximum increase
		BaseFee:  big.NewInt(1000),
	}
	expectedBaseFee := CalcBaseFee(parent)

	// Create a tx with gasFeeCap below the expected base fee.
	lowCapTx := types.NewTransaction(&types.DynamicFeeTx{
		Nonce:     0,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(100), // well below expected base fee
		Gas:       21000,
		To:        &receiver,
		Value:     big.NewInt(1),
	})
	lowCapTx.SetSender(sender)

	block, _, err := builder.BuildBlockLegacy(parent, []*types.Transaction{lowCapTx},
		1700000001, types.BytesToAddress([]byte{0xff}), nil)
	if err != nil {
		t.Fatalf("BuildBlockLegacy: %v", err)
	}

	// The base fee should be above 1000 (increased from full block).
	if expectedBaseFee.Int64() <= 1000 {
		t.Fatalf("expected base fee increase, got %s", expectedBaseFee)
	}

	// The low-cap tx should be filtered out.
	if len(block.Transactions()) != 0 {
		t.Errorf("expected 0 txs (fee cap too low for base fee %s), got %d",
			expectedBaseFee, len(block.Transactions()))
	}
}
