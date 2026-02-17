package core

import (
	"math/big"
	"testing"

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
func makeBlock(parent *types.Block, txs []*types.Transaction) *types.Block {
	parentHeader := parent.Header()
	header := &types.Header{
		ParentHash: parent.Hash(),
		Number:     new(big.Int).Add(parentHeader.Number, big.NewInt(1)),
		GasLimit:   parentHeader.GasLimit,
		GasUsed:    uint64(len(txs)) * TxGas,
		Time:       parentHeader.Time + 12,
		Difficulty: new(big.Int),
		BaseFee:    CalcBaseFee(parentHeader),
		UncleHash:  EmptyUncleHash,
	}
	var body *types.Body
	if len(txs) > 0 {
		body = &types.Body{Transactions: txs}
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

	block1 := makeBlock(bc.Genesis(), []*types.Transaction{tx})
	err = bc.InsertBlock(block1)
	if err != nil {
		t.Fatalf("InsertBlock with tx: %v", err)
	}

	if bc.CurrentBlock().NumberU64() != 1 {
		t.Errorf("head = %d, want 1", bc.CurrentBlock().NumberU64())
	}

	// Verify state was updated.
	st := bc.State()
	bal := st.GetBalance(receiver)
	if bal.Cmp(big.NewInt(100)) != 0 {
		t.Errorf("receiver balance = %s, want 100", bal)
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
	badParent := &types.Header{
		ParentHash: types.Hash{0xff},
		Number:     big.NewInt(1),
		GasLimit:   gen.GasLimit(),
		Time:       gen.Time() + 12,
		Difficulty: new(big.Int),
		BaseFee:    CalcBaseFee(gen.Header()),
		UncleHash:  EmptyUncleHash,
	}
	badBlock := types.NewBlock(badParent, nil)
	err := bc.InsertBlock(badBlock)
	if err == nil {
		t.Fatal("expected error for block with unknown parent")
	}

	// Block with wrong number (skips a block).
	badNumber := &types.Header{
		ParentHash: gen.Hash(),
		Number:     big.NewInt(5), // should be 1
		GasLimit:   gen.GasLimit(),
		Time:       gen.Time() + 12,
		Difficulty: new(big.Int),
		BaseFee:    CalcBaseFee(gen.Header()),
		UncleHash:  EmptyUncleHash,
	}
	badBlock = types.NewBlock(badNumber, nil)
	err = bc.InsertBlock(badBlock)
	if err == nil {
		t.Fatal("expected error for block with wrong number")
	}

	// Block with timestamp not greater than parent.
	badTime := &types.Header{
		ParentHash: gen.Hash(),
		Number:     big.NewInt(1),
		GasLimit:   gen.GasLimit(),
		Time:       0, // same as genesis
		Difficulty: new(big.Int),
		BaseFee:    CalcBaseFee(gen.Header()),
		UncleHash:  EmptyUncleHash,
	}
	badBlock = types.NewBlock(badTime, nil)
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
