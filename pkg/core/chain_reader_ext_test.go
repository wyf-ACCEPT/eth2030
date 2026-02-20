package core

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func makeTestBlockForFullChain(num uint64, parentHash types.Hash, txs []*types.Transaction) *types.Block {
	header := &types.Header{
		ParentHash: parentHash,
		Number:     new(big.Int).SetUint64(num),
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(1),
		BaseFee:    big.NewInt(1_000_000_000),
		UncleHash:  EmptyUncleHash,
	}
	body := &types.Body{Transactions: txs}
	return types.NewBlock(header, body)
}

func TestMemoryFullChainAddAndGet(t *testing.T) {
	mfc := NewMemoryFullChain()

	// Create genesis block.
	genesis := makeTestBlockForFullChain(0, types.Hash{}, nil)
	mfc.AddBlock(genesis)

	// Create block 1 with a transaction.
	tx := types.NewTransaction(&types.LegacyTx{Nonce: 0, Value: big.NewInt(0), Gas: 21000, GasPrice: big.NewInt(1_000_000_000)})
	block1 := makeTestBlockForFullChain(1, genesis.Hash(), []*types.Transaction{tx})
	mfc.AddBlock(block1)

	// Verify GetBlockByNumber.
	got := mfc.GetBlockByNumber(0)
	if got == nil {
		t.Fatal("expected genesis block, got nil")
	}
	if got.NumberU64() != 0 {
		t.Fatalf("expected block 0, got %d", got.NumberU64())
	}

	got = mfc.GetBlockByNumber(1)
	if got == nil {
		t.Fatal("expected block 1, got nil")
	}
	if got.NumberU64() != 1 {
		t.Fatalf("expected block 1, got %d", got.NumberU64())
	}

	// CurrentBlock should be block 1.
	cur := mfc.CurrentBlock()
	if cur == nil || cur.NumberU64() != 1 {
		t.Fatalf("expected current block 1, got %v", cur)
	}
}

func TestMemoryFullChainGetTransaction(t *testing.T) {
	mfc := NewMemoryFullChain()

	genesis := makeTestBlockForFullChain(0, types.Hash{}, nil)
	mfc.AddBlock(genesis)

	tx1 := types.NewTransaction(&types.LegacyTx{Nonce: 0, Value: big.NewInt(100), Gas: 21000, GasPrice: big.NewInt(1_000_000_000)})
	tx2 := types.NewTransaction(&types.LegacyTx{Nonce: 1, Value: big.NewInt(200), Gas: 21000, GasPrice: big.NewInt(1_000_000_000)})
	block1 := makeTestBlockForFullChain(1, genesis.Hash(), []*types.Transaction{tx1, tx2})
	mfc.AddBlock(block1)

	// Look up tx1.
	gotTx, blockHash, blockNum, txIdx := mfc.GetTransaction(tx1.Hash())
	if gotTx == nil {
		t.Fatal("expected transaction, got nil")
	}
	if blockHash != block1.Hash() {
		t.Fatalf("expected block hash %s, got %s", block1.Hash().Hex(), blockHash.Hex())
	}
	if blockNum != 1 {
		t.Fatalf("expected block number 1, got %d", blockNum)
	}
	if txIdx != 0 {
		t.Fatalf("expected tx index 0, got %d", txIdx)
	}

	// Look up tx2.
	gotTx, _, _, txIdx = mfc.GetTransaction(tx2.Hash())
	if gotTx == nil {
		t.Fatal("expected transaction 2, got nil")
	}
	if txIdx != 1 {
		t.Fatalf("expected tx index 1, got %d", txIdx)
	}

	// Look up nonexistent tx.
	gotTx, _, _, _ = mfc.GetTransaction(types.Hash{0xDE, 0xAD})
	if gotTx != nil {
		t.Fatal("expected nil for nonexistent transaction")
	}
}

func TestMemoryFullChainGetReceipt(t *testing.T) {
	mfc := NewMemoryFullChain()

	genesis := makeTestBlockForFullChain(0, types.Hash{}, nil)
	mfc.AddBlock(genesis)

	tx1 := types.NewTransaction(&types.LegacyTx{Nonce: 0, Value: big.NewInt(100), Gas: 21000, GasPrice: big.NewInt(1_000_000_000)})
	block1 := makeTestBlockForFullChain(1, genesis.Hash(), []*types.Transaction{tx1})
	mfc.AddBlock(block1)

	receipt := &types.Receipt{
		Status:            types.ReceiptStatusSuccessful,
		CumulativeGasUsed: 21000,
		GasUsed:           21000,
		TxHash:            tx1.Hash(),
	}
	mfc.AddReceipts(block1.Hash(), []*types.Receipt{receipt})

	// Look up receipt.
	gotReceipt, blockHash, blockNum, idx := mfc.GetReceipt(tx1.Hash())
	if gotReceipt == nil {
		t.Fatal("expected receipt, got nil")
	}
	if gotReceipt.GasUsed != 21000 {
		t.Fatalf("expected gas used 21000, got %d", gotReceipt.GasUsed)
	}
	if blockHash != block1.Hash() {
		t.Fatalf("block hash mismatch: %s vs %s", blockHash.Hex(), block1.Hash().Hex())
	}
	if blockNum != 1 {
		t.Fatalf("expected block number 1, got %d", blockNum)
	}
	if idx != 0 {
		t.Fatalf("expected receipt index 0, got %d", idx)
	}

	// Look up nonexistent receipt.
	gotReceipt, _, _, _ = mfc.GetReceipt(types.Hash{0xBB})
	if gotReceipt != nil {
		t.Fatal("expected nil for nonexistent receipt")
	}
}

func TestMemoryFullChainGetTotalDifficulty(t *testing.T) {
	mfc := NewMemoryFullChain()

	genesis := makeTestBlockForFullChain(0, types.Hash{}, nil)
	mfc.AddBlock(genesis)

	block1 := makeTestBlockForFullChain(1, genesis.Hash(), nil)
	mfc.AddBlock(block1)

	block2 := makeTestBlockForFullChain(2, block1.Hash(), nil)
	mfc.AddBlock(block2)

	// Genesis TD should be its difficulty (1).
	td := mfc.GetTotalDifficulty(genesis.Hash(), 0)
	if td == nil {
		t.Fatal("expected TD for genesis, got nil")
	}
	if td.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("expected TD=1 for genesis, got %s", td.String())
	}

	// Block 1 TD = 1 + 1 = 2.
	td = mfc.GetTotalDifficulty(block1.Hash(), 1)
	if td == nil {
		t.Fatal("expected TD for block 1, got nil")
	}
	if td.Cmp(big.NewInt(2)) != 0 {
		t.Fatalf("expected TD=2 for block 1, got %s", td.String())
	}

	// Block 2 TD = 2 + 1 = 3.
	td = mfc.GetTotalDifficulty(block2.Hash(), 2)
	if td == nil {
		t.Fatal("expected TD for block 2, got nil")
	}
	if td.Cmp(big.NewInt(3)) != 0 {
		t.Fatalf("expected TD=3 for block 2, got %s", td.String())
	}

	// Nonexistent block returns nil.
	td = mfc.GetTotalDifficulty(types.Hash{0xFF}, 99)
	if td != nil {
		t.Fatal("expected nil TD for nonexistent block")
	}
}

func TestMemoryFullChainGetCanonicalHash(t *testing.T) {
	mfc := NewMemoryFullChain()

	genesis := makeTestBlockForFullChain(0, types.Hash{}, nil)
	mfc.AddBlock(genesis)

	hash := mfc.GetCanonicalHash(0)
	if hash != genesis.Hash() {
		t.Fatalf("expected genesis hash, got %s", hash.Hex())
	}

	hash = mfc.GetCanonicalHash(999)
	if hash != (types.Hash{}) {
		t.Fatalf("expected zero hash for nonexistent number, got %s", hash.Hex())
	}
}

func TestMemoryFullChainChainHeight(t *testing.T) {
	mfc := NewMemoryFullChain()

	if mfc.ChainHeight() != 0 {
		t.Fatalf("expected height 0, got %d", mfc.ChainHeight())
	}

	genesis := makeTestBlockForFullChain(0, types.Hash{}, nil)
	mfc.AddBlock(genesis)
	if mfc.ChainHeight() != 0 {
		t.Fatalf("expected height 0 after genesis, got %d", mfc.ChainHeight())
	}

	block1 := makeTestBlockForFullChain(1, genesis.Hash(), nil)
	mfc.AddBlock(block1)
	if mfc.ChainHeight() != 1 {
		t.Fatalf("expected height 1, got %d", mfc.ChainHeight())
	}
}

func TestMemoryFullChainBlockRange(t *testing.T) {
	mfc := NewMemoryFullChain()

	genesis := makeTestBlockForFullChain(0, types.Hash{}, nil)
	mfc.AddBlock(genesis)
	block1 := makeTestBlockForFullChain(1, genesis.Hash(), nil)
	mfc.AddBlock(block1)
	block2 := makeTestBlockForFullChain(2, block1.Hash(), nil)
	mfc.AddBlock(block2)

	blocks := mfc.BlockRange(0, 2)
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks in range, got %d", len(blocks))
	}
	if blocks[0].NumberU64() != 0 || blocks[1].NumberU64() != 1 || blocks[2].NumberU64() != 2 {
		t.Fatal("block numbers do not match expected range")
	}

	// Range beyond chain returns what is available.
	blocks = mfc.BlockRange(0, 5)
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks (up to 2), got %d", len(blocks))
	}

	// Reversed range returns nil.
	blocks = mfc.BlockRange(5, 0)
	if blocks != nil {
		t.Fatal("expected nil for reversed range")
	}
}

func TestMemoryFullChainHasBlock(t *testing.T) {
	mfc := NewMemoryFullChain()

	genesis := makeTestBlockForFullChain(0, types.Hash{}, nil)
	mfc.AddBlock(genesis)

	if !mfc.HasBlock(genesis.Hash(), 0) {
		t.Fatal("expected HasBlock=true for genesis")
	}
	if mfc.HasBlock(types.Hash{0xFF}, 0) {
		t.Fatal("expected HasBlock=false for wrong hash")
	}
}

func TestFullChainReaderInterface(t *testing.T) {
	// Verify the MemoryFullChain satisfies FullChainReader.
	var _ FullChainReader = (*MemoryFullChain)(nil)
}
