package core

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/bal"
	"github.com/eth2028/eth2028/core/rawdb"
	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

// testChain is a helper that creates a blockchain with a genesis block.
func testChain(t *testing.T) (*Blockchain, *state.MemoryStateDB) {
	t.Helper()
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis(30_000_000, big.NewInt(1))
	db := rawdb.NewMemoryDB()
	bc, err := NewBlockchain(TestConfig, genesis, statedb, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}
	return bc, statedb
}

// makeBlock builds a valid child block of parent with the given transactions.
// It sets the BlockAccessListHash for Amsterdam-compatible blocks (TestConfig).
func makeBlock(parent *types.Block, txs []*types.Transaction) *types.Block {
	parentHeader := parent.Header()
	blobGasUsed := uint64(0)
	var parentExcess, parentUsed uint64
	if parentHeader.ExcessBlobGas != nil {
		parentExcess = *parentHeader.ExcessBlobGas
	}
	if parentHeader.BlobGasUsed != nil {
		parentUsed = *parentHeader.BlobGasUsed
	}
	excessBlobGas := CalcExcessBlobGas(parentExcess, parentUsed)
	emptyWithdrawalsHash := types.EmptyRootHash
	header := &types.Header{
		ParentHash:      parent.Hash(),
		Number:          new(big.Int).Add(parentHeader.Number, big.NewInt(1)),
		GasLimit:        parentHeader.GasLimit,
		GasUsed:         uint64(len(txs)) * TxGas,
		Time:            parentHeader.Time + 12,
		Difficulty:      new(big.Int),
		BaseFee:         CalcBaseFee(parentHeader),
		UncleHash:       EmptyUncleHash,
		WithdrawalsHash: &emptyWithdrawalsHash,
		BlobGasUsed:     &blobGasUsed,
		ExcessBlobGas:   &excessBlobGas,
	}

	// Compute BAL hash for Amsterdam blocks. For test blocks built with
	// makeBlock, we construct a BAL from the transaction senders/recipients.
	if TestConfig.IsAmsterdam(header.Time) {
		blockBAL := bal.NewBlockAccessList()
		// For blocks with transactions, add entries for each tx's sender/recipient.
		// The actual balance/nonce changes will be computed during execution, but we
		// need a deterministic BAL hash that matches what ProcessWithBAL computes.
		// Since makeBlock doesn't execute transactions, we use a simple approach:
		// set the BAL hash to the hash of an empty BAL for empty blocks. For blocks
		// with transactions, the caller must ensure the BAL hash is correct.
		h := blockBAL.Hash()
		header.BlockAccessListHash = &h
	}

	body := &types.Body{
		Transactions: txs,
		Withdrawals:  []*types.Withdrawal{}, // Shanghai+ requires withdrawals
	}
	return types.NewBlock(header, body)
}

// makeBlockWithState builds a valid child block and computes the correct BAL
// hash by executing the transactions against the provided state. Use this for
// blocks that contain transactions when Amsterdam fork is active.
func makeBlockWithState(parent *types.Block, txs []*types.Transaction, statedb *state.MemoryStateDB) *types.Block {
	parentHeader := parent.Header()
	blobGasUsed := uint64(0)
	var pExcess, pUsed uint64
	if parentHeader.ExcessBlobGas != nil {
		pExcess = *parentHeader.ExcessBlobGas
	}
	if parentHeader.BlobGasUsed != nil {
		pUsed = *parentHeader.BlobGasUsed
	}
	excessBlobGas := CalcExcessBlobGas(pExcess, pUsed)
	emptyWHash := types.EmptyRootHash
	header := &types.Header{
		ParentHash:      parent.Hash(),
		Number:          new(big.Int).Add(parentHeader.Number, big.NewInt(1)),
		GasLimit:        parentHeader.GasLimit,
		Time:            parentHeader.Time + 12,
		Difficulty:      new(big.Int),
		BaseFee:         CalcBaseFee(parentHeader),
		UncleHash:       EmptyUncleHash,
		WithdrawalsHash: &emptyWHash,
		BlobGasUsed:     &blobGasUsed,
		ExcessBlobGas:   &excessBlobGas,
	}

	body := &types.Body{
		Transactions: txs,
		Withdrawals:  []*types.Withdrawal{}, // Shanghai+ requires withdrawals (may be empty)
	}

	block := types.NewBlock(header, body)

	// Execute transactions on a copy to compute gas used and BAL hash.
	tmpState := statedb.Copy()
	proc := NewStateProcessor(TestConfig)
	result, err := proc.ProcessWithBAL(block, tmpState)
	if err == nil {
		// Compute gas used from receipts.
		var gasUsed uint64
		for _, r := range result.Receipts {
			gasUsed += r.GasUsed
		}
		header.GasUsed = gasUsed

		// Set BAL hash.
		if result.BlockAccessList != nil {
			h := result.BlockAccessList.Hash()
			header.BlockAccessListHash = &h
		}

		// Set bloom from receipts.
		header.Bloom = types.CreateBloom(result.Receipts)
	}

	return types.NewBlock(header, body)
}

func TestBlockchain_Genesis(t *testing.T) {
	bc, _ := testChain(t)

	// Genesis should be stored.
	gen := bc.Genesis()
	if gen == nil {
		t.Fatal("genesis block is nil")
	}
	if gen.NumberU64() != 0 {
		t.Errorf("genesis number = %d, want 0", gen.NumberU64())
	}

	// Current block should be genesis.
	head := bc.CurrentBlock()
	if head.Hash() != gen.Hash() {
		t.Errorf("head hash = %v, want genesis %v", head.Hash(), gen.Hash())
	}

	// ChainLength should be 1.
	if bc.ChainLength() != 1 {
		t.Errorf("chain length = %d, want 1", bc.ChainLength())
	}

	// HasBlock should find genesis.
	if !bc.HasBlock(gen.Hash()) {
		t.Error("HasBlock(genesis) = false")
	}

	// GetBlockByNumber(0) should return genesis.
	b0 := bc.GetBlockByNumber(0)
	if b0 == nil || b0.Hash() != gen.Hash() {
		t.Error("GetBlockByNumber(0) does not return genesis")
	}
}

func TestBlockchain_InsertSingleBlock(t *testing.T) {
	bc, _ := testChain(t)
	gen := bc.Genesis()

	// Build a valid empty block.
	block1 := makeBlock(gen, nil)

	// Insert it.
	err := bc.InsertBlock(block1)
	if err != nil {
		t.Fatalf("InsertBlock: %v", err)
	}

	// Head should now be block 1.
	head := bc.CurrentBlock()
	if head.NumberU64() != 1 {
		t.Errorf("head number = %d, want 1", head.NumberU64())
	}
	if head.Hash() != block1.Hash() {
		t.Errorf("head hash mismatch")
	}

	// GetBlock should find it.
	if bc.GetBlock(block1.Hash()) == nil {
		t.Error("GetBlock(block1) = nil")
	}

	// ChainLength should be 2.
	if bc.ChainLength() != 2 {
		t.Errorf("chain length = %d, want 2", bc.ChainLength())
	}
}

func TestBlockchain_InsertBlockWithTx(t *testing.T) {
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

	// Create a transaction.
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      21000,
		To:       &receiver,
		Value:    big.NewInt(100),
	})
	tx.SetSender(sender)

	block1 := makeBlockWithState(bc.Genesis(), []*types.Transaction{tx}, statedb)
	err = bc.InsertBlock(block1)
	if err != nil {
		t.Fatalf("InsertBlock with tx: %v", err)
	}

	if bc.CurrentBlock().NumberU64() != 1 {
		t.Errorf("head = %d, want 1", bc.CurrentBlock().NumberU64())
	}

	// Verify state was updated.
	st := bc.State()
	recBal := st.GetBalance(receiver)
	if recBal.Cmp(big.NewInt(100)) != 0 {
		t.Errorf("receiver balance = %s, want 100", recBal)
	}
}

func TestBlockchain_InsertChain(t *testing.T) {
	bc, _ := testChain(t)

	// Build 5 blocks.
	var blocks []*types.Block
	parent := bc.Genesis()
	for i := 0; i < 5; i++ {
		b := makeBlock(parent, nil)
		blocks = append(blocks, b)
		parent = b
	}

	n, err := bc.InsertChain(blocks)
	if err != nil {
		t.Fatalf("InsertChain at %d: %v", n, err)
	}
	if n != 5 {
		t.Errorf("inserted = %d, want 5", n)
	}

	head := bc.CurrentBlock()
	if head.NumberU64() != 5 {
		t.Errorf("head number = %d, want 5", head.NumberU64())
	}

	// ChainLength should be 6 (genesis + 5 blocks).
	if bc.ChainLength() != 6 {
		t.Errorf("chain length = %d, want 6", bc.ChainLength())
	}

	// All blocks should be retrievable by number.
	for i := uint64(0); i <= 5; i++ {
		b := bc.GetBlockByNumber(i)
		if b == nil {
			t.Errorf("GetBlockByNumber(%d) = nil", i)
		} else if b.NumberU64() != i {
			t.Errorf("block at %d has number %d", i, b.NumberU64())
		}
	}
}

func TestBlockchain_GetBlockByNumber(t *testing.T) {
	bc, _ := testChain(t)

	// Insert 3 blocks.
	parent := bc.Genesis()
	for i := 0; i < 3; i++ {
		b := makeBlock(parent, nil)
		if err := bc.InsertBlock(b); err != nil {
			t.Fatalf("InsertBlock %d: %v", i+1, err)
		}
		parent = b
	}

	// Verify each number.
	for i := uint64(0); i <= 3; i++ {
		b := bc.GetBlockByNumber(i)
		if b == nil {
			t.Fatalf("GetBlockByNumber(%d) = nil", i)
		}
		if b.NumberU64() != i {
			t.Errorf("block.Number = %d, want %d", b.NumberU64(), i)
		}
	}

	// Non-existent number returns nil.
	if bc.GetBlockByNumber(999) != nil {
		t.Error("GetBlockByNumber(999) should be nil")
	}
}

func TestBlockchain_GetHashFn(t *testing.T) {
	bc, _ := testChain(t)

	// Build 5 blocks.
	parent := bc.Genesis()
	for i := 0; i < 5; i++ {
		b := makeBlock(parent, nil)
		if err := bc.InsertBlock(b); err != nil {
			t.Fatalf("InsertBlock %d: %v", i+1, err)
		}
		parent = b
	}

	getHash := bc.GetHashFn()

	// All block hashes should be resolvable.
	for i := uint64(0); i <= 5; i++ {
		expected := bc.GetBlockByNumber(i).Hash()
		got := getHash(i)
		if got != expected {
			t.Errorf("GetHashFn(%d) = %v, want %v", i, got, expected)
		}
	}

	// Unknown number returns zero hash.
	if h := getHash(999); h != (types.Hash{}) {
		t.Errorf("GetHashFn(999) = %v, want zero", h)
	}
}

func TestBlockchain_InvalidBlock(t *testing.T) {
	bc, _ := testChain(t)
	gen := bc.Genesis()

	// Block with wrong parent hash.
	badBlobGas1 := uint64(0)
	badExcess1 := uint64(0)
	badWHash1 := types.EmptyRootHash
	badParent := &types.Header{
		ParentHash:      types.Hash{0xff},
		Number:          big.NewInt(1),
		GasLimit:        gen.GasLimit(),
		Time:            gen.Time() + 12,
		Difficulty:      new(big.Int),
		BaseFee:         CalcBaseFee(gen.Header()),
		UncleHash:       EmptyUncleHash,
		WithdrawalsHash: &badWHash1,
		BlobGasUsed:     &badBlobGas1,
		ExcessBlobGas:   &badExcess1,
	}
	badBlock := types.NewBlock(badParent, &types.Body{Withdrawals: []*types.Withdrawal{}})
	err := bc.InsertBlock(badBlock)
	if err == nil {
		t.Fatal("expected error for block with unknown parent")
	}

	// Block with wrong number (skips a block).
	badBlobGas2 := uint64(0)
	badExcess2 := uint64(0)
	badWHash2 := types.EmptyRootHash
	badNumber := &types.Header{
		ParentHash:      gen.Hash(),
		Number:          big.NewInt(5), // should be 1
		GasLimit:        gen.GasLimit(),
		Time:            gen.Time() + 12,
		Difficulty:      new(big.Int),
		BaseFee:         CalcBaseFee(gen.Header()),
		UncleHash:       EmptyUncleHash,
		WithdrawalsHash: &badWHash2,
		BlobGasUsed:     &badBlobGas2,
		ExcessBlobGas:   &badExcess2,
	}
	badBlock = types.NewBlock(badNumber, &types.Body{Withdrawals: []*types.Withdrawal{}})
	err = bc.InsertBlock(badBlock)
	if err == nil {
		t.Fatal("expected error for block with wrong number")
	}

	// Block with timestamp not greater than parent.
	badBlobGas3 := uint64(0)
	badExcess3 := uint64(0)
	badWHash3 := types.EmptyRootHash
	badTime := &types.Header{
		ParentHash:      gen.Hash(),
		Number:          big.NewInt(1),
		GasLimit:        gen.GasLimit(),
		Time:            0, // same as genesis
		Difficulty:      new(big.Int),
		BaseFee:         CalcBaseFee(gen.Header()),
		UncleHash:       EmptyUncleHash,
		WithdrawalsHash: &badWHash3,
		BlobGasUsed:     &badBlobGas3,
		ExcessBlobGas:   &badExcess3,
	}
	badBlock = types.NewBlock(badTime, &types.Body{Withdrawals: []*types.Withdrawal{}})
	err = bc.InsertBlock(badBlock)
	if err == nil {
		t.Fatal("expected error for block with invalid timestamp")
	}

	// Head should still be genesis after all failures.
	if bc.CurrentBlock().NumberU64() != 0 {
		t.Errorf("head = %d after invalid blocks, want 0", bc.CurrentBlock().NumberU64())
	}
}

func TestBlockchain_SetHead(t *testing.T) {
	bc, _ := testChain(t)

	// Build 5 blocks.
	parent := bc.Genesis()
	for i := 0; i < 5; i++ {
		b := makeBlock(parent, nil)
		if err := bc.InsertBlock(b); err != nil {
			t.Fatalf("InsertBlock %d: %v", i+1, err)
		}
		parent = b
	}

	if bc.CurrentBlock().NumberU64() != 5 {
		t.Fatalf("head = %d, want 5", bc.CurrentBlock().NumberU64())
	}

	// Rewind to block 2.
	err := bc.SetHead(2)
	if err != nil {
		t.Fatalf("SetHead(2): %v", err)
	}

	if bc.CurrentBlock().NumberU64() != 2 {
		t.Errorf("head = %d after SetHead(2), want 2", bc.CurrentBlock().NumberU64())
	}

	// Blocks 3, 4, 5 should no longer be in canonical chain.
	for i := uint64(3); i <= 5; i++ {
		if bc.GetBlockByNumber(i) != nil {
			t.Errorf("GetBlockByNumber(%d) should be nil after rewind", i)
		}
	}

	// Blocks 0, 1, 2 should still exist.
	for i := uint64(0); i <= 2; i++ {
		if bc.GetBlockByNumber(i) == nil {
			t.Errorf("GetBlockByNumber(%d) = nil, should still exist", i)
		}
	}

	// ChainLength should be 3.
	if bc.ChainLength() != 3 {
		t.Errorf("chain length = %d, want 3", bc.ChainLength())
	}
}

func TestBlockchain_SetHeadToGenesis(t *testing.T) {
	bc, _ := testChain(t)

	// Build 3 blocks.
	parent := bc.Genesis()
	for i := 0; i < 3; i++ {
		b := makeBlock(parent, nil)
		if err := bc.InsertBlock(b); err != nil {
			t.Fatalf("InsertBlock %d: %v", i+1, err)
		}
		parent = b
	}

	// Rewind all the way to genesis.
	err := bc.SetHead(0)
	if err != nil {
		t.Fatalf("SetHead(0): %v", err)
	}

	if bc.CurrentBlock().NumberU64() != 0 {
		t.Errorf("head = %d, want 0", bc.CurrentBlock().NumberU64())
	}
	if bc.CurrentBlock().Hash() != bc.Genesis().Hash() {
		t.Error("head hash != genesis hash after SetHead(0)")
	}
}

func TestBlockchain_InsertDuplicate(t *testing.T) {
	bc, _ := testChain(t)

	block1 := makeBlock(bc.Genesis(), nil)
	if err := bc.InsertBlock(block1); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	// Inserting the same block again should be idempotent.
	if err := bc.InsertBlock(block1); err != nil {
		t.Fatalf("duplicate insert: %v", err)
	}

	if bc.CurrentBlock().NumberU64() != 1 {
		t.Errorf("head = %d, want 1", bc.CurrentBlock().NumberU64())
	}
}

func TestBlockchain_NilGenesis(t *testing.T) {
	db := rawdb.NewMemoryDB()
	_, err := NewBlockchain(TestConfig, nil, state.NewMemoryStateDB(), db)
	if err != ErrNoGenesis {
		t.Errorf("expected ErrNoGenesis, got %v", err)
	}
}

// --- Block serialization tests ---

func TestBlockchain_WriteReadBlock_EmptyBlock(t *testing.T) {
	bc, _ := testChain(t)
	gen := bc.Genesis()

	// Insert an empty block.
	block1 := makeBlock(gen, nil)
	if err := bc.InsertBlock(block1); err != nil {
		t.Fatalf("InsertBlock: %v", err)
	}

	// Remove block from in-memory cache to force rawdb read.
	hash := block1.Hash()
	bc.mu.Lock()
	delete(bc.blockCache, hash)
	bc.mu.Unlock()

	// GetBlock should fall back to rawdb and reconstruct the block.
	recovered := bc.GetBlock(hash)
	if recovered == nil {
		t.Fatal("GetBlock after cache eviction returned nil")
	}
	if recovered.Hash() != hash {
		t.Errorf("recovered block hash = %v, want %v", recovered.Hash(), hash)
	}
	if recovered.NumberU64() != block1.NumberU64() {
		t.Errorf("recovered block number = %d, want %d", recovered.NumberU64(), block1.NumberU64())
	}
	if recovered.GasLimit() != block1.GasLimit() {
		t.Errorf("recovered gas limit = %d, want %d", recovered.GasLimit(), block1.GasLimit())
	}
	if recovered.Time() != block1.Time() {
		t.Errorf("recovered time = %d, want %d", recovered.Time(), block1.Time())
	}
	if recovered.ParentHash() != block1.ParentHash() {
		t.Errorf("recovered parent hash mismatch")
	}
}

func TestBlockchain_WriteReadBlock_WithTransactions(t *testing.T) {
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

	// Create a transaction.
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      21000,
		To:       &receiver,
		Value:    big.NewInt(100),
	})
	tx.SetSender(sender)

	block1 := makeBlockWithState(bc.Genesis(), []*types.Transaction{tx}, statedb)
	if err := bc.InsertBlock(block1); err != nil {
		t.Fatalf("InsertBlock with tx: %v", err)
	}

	hash := block1.Hash()

	// Remove from in-memory cache.
	bc.mu.Lock()
	delete(bc.blockCache, hash)
	bc.mu.Unlock()

	// Recover from rawdb.
	recovered := bc.GetBlock(hash)
	if recovered == nil {
		t.Fatal("GetBlock returned nil after cache eviction")
	}
	if recovered.Hash() != hash {
		t.Errorf("recovered block hash mismatch")
	}

	// Verify the recovered block has the same number of transactions.
	if len(recovered.Transactions()) != 1 {
		t.Fatalf("recovered block has %d txs, want 1", len(recovered.Transactions()))
	}

	// Verify the recovered transaction data matches.
	recoveredTx := recovered.Transactions()[0]
	originalTx := block1.Transactions()[0]
	if recoveredTx.Nonce() != originalTx.Nonce() {
		t.Errorf("tx nonce = %d, want %d", recoveredTx.Nonce(), originalTx.Nonce())
	}
	if recoveredTx.Gas() != originalTx.Gas() {
		t.Errorf("tx gas = %d, want %d", recoveredTx.Gas(), originalTx.Gas())
	}
	if recoveredTx.Value().Cmp(originalTx.Value()) != 0 {
		t.Errorf("tx value = %s, want %s", recoveredTx.Value(), originalTx.Value())
	}
}

func TestBlockchain_WriteReadBlock_GenesisFromDB(t *testing.T) {
	bc, _ := testChain(t)
	gen := bc.Genesis()
	hash := gen.Hash()

	// Remove genesis from in-memory cache.
	bc.mu.Lock()
	delete(bc.blockCache, hash)
	bc.mu.Unlock()

	// Should still be retrievable from rawdb.
	recovered := bc.GetBlock(hash)
	if recovered == nil {
		t.Fatal("genesis block not recoverable from rawdb")
	}
	if recovered.Hash() != hash {
		t.Errorf("recovered genesis hash = %v, want %v", recovered.Hash(), hash)
	}
	if recovered.NumberU64() != 0 {
		t.Errorf("recovered genesis number = %d, want 0", recovered.NumberU64())
	}
}

// --- State cache tests ---

func TestBlockchain_StateCachePopulated(t *testing.T) {
	bc, _ := testChain(t)

	// Insert blocks up to stateSnapshotInterval to trigger a cache snapshot.
	// stateSnapshotInterval is 16, so block 16 should trigger caching.
	parent := bc.Genesis()
	for i := 0; i < int(stateSnapshotInterval); i++ {
		b := makeBlock(parent, nil)
		if err := bc.InsertBlock(b); err != nil {
			t.Fatalf("InsertBlock %d: %v", i+1, err)
		}
		parent = b
	}

	// Block at stateSnapshotInterval should have a cached state.
	blockN := bc.GetBlockByNumber(stateSnapshotInterval)
	if blockN == nil {
		t.Fatalf("block %d not found", stateSnapshotInterval)
	}

	cached, ok := bc.sc.get(blockN.Hash())
	if !ok {
		t.Fatalf("state cache not populated at block %d", stateSnapshotInterval)
	}
	if cached == nil {
		t.Fatal("cached state is nil")
	}
}

func TestBlockchain_StateCacheNotPopulatedAtNonInterval(t *testing.T) {
	bc, _ := testChain(t)

	// Insert a few blocks (less than stateSnapshotInterval).
	parent := bc.Genesis()
	for i := 0; i < 5; i++ {
		b := makeBlock(parent, nil)
		if err := bc.InsertBlock(b); err != nil {
			t.Fatalf("InsertBlock %d: %v", i+1, err)
		}
		parent = b
	}

	// Blocks 1-5 should NOT have cached states (none are multiples of 16).
	for i := uint64(1); i <= 5; i++ {
		b := bc.GetBlockByNumber(i)
		if b == nil {
			t.Fatalf("block %d not found", i)
		}
		_, ok := bc.sc.get(b.Hash())
		if ok {
			t.Errorf("state cache should not be populated at block %d", i)
		}
	}
}

func TestBlockchain_StateAtUsesCachedState(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0x01})
	receiver := types.BytesToAddress([]byte{0x02})
	statedb.AddBalance(sender, big.NewInt(100_000_000))

	genesis := makeGenesis(30_000_000, big.NewInt(1))
	db := rawdb.NewMemoryDB()
	bc, err := NewBlockchain(TestConfig, genesis, statedb, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}

	// Build stateSnapshotInterval + 2 blocks with a tx in each.
	parent := bc.Genesis()
	for i := 0; i < int(stateSnapshotInterval)+2; i++ {
		tx := types.NewTransaction(&types.LegacyTx{
			Nonce:    uint64(i),
			GasPrice: big.NewInt(1),
			Gas:      21000,
			To:       &receiver,
			Value:    big.NewInt(1),
		})
		tx.SetSender(sender)
		b := makeBlockWithState(parent, []*types.Transaction{tx}, bc.State())
		if err := bc.InsertBlock(b); err != nil {
			t.Fatalf("InsertBlock %d: %v", i+1, err)
		}
		parent = b
	}

	// A cached state should exist at block stateSnapshotInterval.
	blockN := bc.GetBlockByNumber(stateSnapshotInterval)
	_, ok := bc.sc.get(blockN.Hash())
	if !ok {
		t.Fatalf("expected state cache at block %d", stateSnapshotInterval)
	}

	// Now request stateAt for a block after the snapshot.
	// This should use the cached state instead of re-executing from genesis.
	targetBlock := bc.GetBlockByNumber(stateSnapshotInterval + 1)
	bc.mu.RLock()
	st, err := bc.stateAt(targetBlock)
	bc.mu.RUnlock()
	if err != nil {
		t.Fatalf("stateAt(%d): %v", stateSnapshotInterval+1, err)
	}
	if st == nil {
		t.Fatal("stateAt returned nil state")
	}

	// Verify the state reflects all processed transactions.
	memSt := st.(*state.MemoryStateDB)
	recBal := memSt.GetBalance(receiver)
	expectedBal := big.NewInt(int64(stateSnapshotInterval) + 1) // 1 per block
	if recBal.Cmp(expectedBal) != 0 {
		t.Errorf("receiver balance = %s, want %s", recBal, expectedBal)
	}
}

func TestBlockchain_GetBlockFallbackToRawDB(t *testing.T) {
	bc, _ := testChain(t)

	// Insert 3 blocks.
	parent := bc.Genesis()
	blocks := make([]*types.Block, 3)
	for i := 0; i < 3; i++ {
		b := makeBlock(parent, nil)
		if err := bc.InsertBlock(b); err != nil {
			t.Fatalf("InsertBlock %d: %v", i+1, err)
		}
		blocks[i] = b
		parent = b
	}

	// Clear all blocks from in-memory cache.
	bc.mu.Lock()
	for _, b := range blocks {
		delete(bc.blockCache, b.Hash())
	}
	bc.mu.Unlock()

	// All blocks should still be retrievable via rawdb fallback.
	for i, b := range blocks {
		recovered := bc.GetBlock(b.Hash())
		if recovered == nil {
			t.Fatalf("block %d not recoverable from rawdb", i+1)
		}
		if recovered.Hash() != b.Hash() {
			t.Errorf("block %d hash mismatch", i+1)
		}
		if recovered.NumberU64() != b.NumberU64() {
			t.Errorf("block %d number mismatch: got %d, want %d", i+1, recovered.NumberU64(), b.NumberU64())
		}
	}

	// Non-existent hash should still return nil.
	if bc.GetBlock(types.Hash{0xff}) != nil {
		t.Error("GetBlock for non-existent hash should return nil")
	}
}

func TestBlockchain_EncodeDecodeBodyRoundTrip(t *testing.T) {
	sender := types.BytesToAddress([]byte{0x01})
	receiver := types.BytesToAddress([]byte{0x02})

	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(42),
		Gas:      21000,
		To:       &receiver,
		Value:    big.NewInt(100),
	})
	tx.SetSender(sender)

	body := &types.Body{
		Transactions: []*types.Transaction{tx},
	}

	encoded, err := encodeBlockBody(body)
	if err != nil {
		t.Fatalf("encodeBlockBody: %v", err)
	}
	if len(encoded) == 0 {
		t.Fatal("encoded body is empty")
	}

	decoded, err := decodeBlockBody(encoded)
	if err != nil {
		t.Fatalf("decodeBlockBody: %v", err)
	}
	if len(decoded.Transactions) != 1 {
		t.Fatalf("decoded body has %d txs, want 1", len(decoded.Transactions))
	}
	if decoded.Transactions[0].Nonce() != 0 {
		t.Errorf("decoded tx nonce = %d, want 0", decoded.Transactions[0].Nonce())
	}
	if decoded.Transactions[0].Gas() != 21000 {
		t.Errorf("decoded tx gas = %d, want 21000", decoded.Transactions[0].Gas())
	}
	if decoded.Transactions[0].Value().Cmp(big.NewInt(100)) != 0 {
		t.Errorf("decoded tx value = %s, want 100", decoded.Transactions[0].Value())
	}
}

func TestBlockchain_EncodeDecodeEmptyBody(t *testing.T) {
	body := &types.Body{}
	encoded, err := encodeBlockBody(body)
	if err != nil {
		t.Fatalf("encodeBlockBody: %v", err)
	}

	decoded, err := decodeBlockBody(encoded)
	if err != nil {
		t.Fatalf("decodeBlockBody: %v", err)
	}
	if len(decoded.Transactions) != 0 {
		t.Errorf("decoded body has %d txs, want 0", len(decoded.Transactions))
	}
	if len(decoded.Uncles) != 0 {
		t.Errorf("decoded body has %d uncles, want 0", len(decoded.Uncles))
	}
}
