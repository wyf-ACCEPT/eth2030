package rawdb

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// testBlock creates a minimal block for testing.
func testBlock(number uint64) *types.Block {
	header := &types.Header{
		Number:     new(big.Int).SetUint64(number),
		Difficulty: big.NewInt(1),
		GasLimit:   8_000_000,
		Time:       1000 + number,
		Extra:      []byte("test"),
		UncleHash:  types.EmptyUncleHash,
		Root:       types.EmptyRootHash,
		TxHash:     types.EmptyRootHash,
	}
	return types.NewBlock(header, nil)
}

// testBlockWithTx creates a block containing a legacy transaction.
func testBlockWithTx(number uint64) *types.Block {
	header := &types.Header{
		Number:     new(big.Int).SetUint64(number),
		Difficulty: big.NewInt(1),
		GasLimit:   8_000_000,
		Time:       1000 + number,
		Extra:      []byte("test"),
		UncleHash:  types.EmptyUncleHash,
		Root:       types.EmptyRootHash,
		TxHash:     types.EmptyRootHash,
	}
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    number,
		GasPrice: big.NewInt(1),
		Gas:      21000,
		Value:    big.NewInt(100),
		V:        big.NewInt(27),
		R:        big.NewInt(1),
		S:        big.NewInt(1),
	})
	body := &types.Body{
		Transactions: []*types.Transaction{tx},
	}
	return types.NewBlock(header, body)
}

func TestChainDB_WriteReadBlock(t *testing.T) {
	db := NewMemoryDB()
	cdb := NewChainDB(db)

	block := testBlock(42)
	if err := cdb.WriteBlock(block); err != nil {
		t.Fatalf("WriteBlock: %v", err)
	}

	got := cdb.ReadBlock(block.Hash())
	if got == nil {
		t.Fatal("ReadBlock returned nil")
	}
	if got.NumberU64() != 42 {
		t.Errorf("block number = %d, want 42", got.NumberU64())
	}
	if got.Hash() != block.Hash() {
		t.Errorf("block hash mismatch: got %s, want %s", got.Hash(), block.Hash())
	}
}

func TestChainDB_WriteReadBlockWithTx(t *testing.T) {
	db := NewMemoryDB()
	cdb := NewChainDB(db)

	block := testBlockWithTx(10)
	if err := cdb.WriteBlock(block); err != nil {
		t.Fatalf("WriteBlock: %v", err)
	}

	got := cdb.ReadBlock(block.Hash())
	if got == nil {
		t.Fatal("ReadBlock returned nil")
	}
	if len(got.Transactions()) != 1 {
		t.Fatalf("transactions count = %d, want 1", len(got.Transactions()))
	}
	// Verify the transaction hash matches.
	origTxHash := block.Transactions()[0].Hash()
	gotTxHash := got.Transactions()[0].Hash()
	if origTxHash != gotTxHash {
		t.Errorf("tx hash mismatch: got %s, want %s", gotTxHash, origTxHash)
	}
}

func TestChainDB_ReadBlockByNumber(t *testing.T) {
	db := NewMemoryDB()
	cdb := NewChainDB(db)

	block := testBlock(5)
	if err := cdb.WriteBlock(block); err != nil {
		t.Fatalf("WriteBlock: %v", err)
	}
	if err := cdb.WriteCanonicalHash(5, block.Hash()); err != nil {
		t.Fatalf("WriteCanonicalHash: %v", err)
	}

	got := cdb.ReadBlockByNumber(5)
	if got == nil {
		t.Fatal("ReadBlockByNumber returned nil")
	}
	if got.Hash() != block.Hash() {
		t.Errorf("hash mismatch")
	}

	// Non-existent number.
	if cdb.ReadBlockByNumber(999) != nil {
		t.Error("expected nil for non-existent block number")
	}
}

func TestChainDB_HasBlock(t *testing.T) {
	db := NewMemoryDB()
	cdb := NewChainDB(db)

	block := testBlock(1)
	if cdb.HasBlock(block.Hash()) {
		t.Error("HasBlock should be false before write")
	}
	if err := cdb.WriteBlock(block); err != nil {
		t.Fatalf("WriteBlock: %v", err)
	}
	if !cdb.HasBlock(block.Hash()) {
		t.Error("HasBlock should be true after write")
	}
}

func TestChainDB_ReadWriteHeader(t *testing.T) {
	db := NewMemoryDB()
	cdb := NewChainDB(db)

	header := &types.Header{
		Number:     big.NewInt(100),
		Difficulty: big.NewInt(50),
		GasLimit:   8_000_000,
		Time:       5000,
		Extra:      []byte("header-test"),
		UncleHash:  types.EmptyUncleHash,
		Root:       types.EmptyRootHash,
	}

	if err := cdb.WriteHeader(header); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}

	hash := header.Hash()
	got := cdb.ReadHeader(hash)
	if got == nil {
		t.Fatal("ReadHeader returned nil")
	}
	if got.Number.Uint64() != 100 {
		t.Errorf("header number = %d, want 100", got.Number.Uint64())
	}
	if got.GasLimit != 8_000_000 {
		t.Errorf("gas limit = %d, want 8000000", got.GasLimit)
	}
}

func TestChainDB_ReadWriteReceipts(t *testing.T) {
	db := NewMemoryDB()
	cdb := NewChainDB(db)

	// Write a block first so the hash->number mapping exists.
	block := testBlock(7)
	if err := cdb.WriteBlock(block); err != nil {
		t.Fatalf("WriteBlock: %v", err)
	}

	receipts := []*types.Receipt{
		{
			Status:            types.ReceiptStatusSuccessful,
			CumulativeGasUsed: 21000,
		},
		{
			Status:            types.ReceiptStatusFailed,
			CumulativeGasUsed: 42000,
		},
	}

	hash := block.Hash()
	if err := cdb.WriteReceipts(hash, 7, receipts); err != nil {
		t.Fatalf("WriteReceipts: %v", err)
	}

	got := cdb.ReadReceipts(hash)
	if got == nil {
		t.Fatal("ReadReceipts returned nil")
	}
	if len(got) != 2 {
		t.Fatalf("receipts count = %d, want 2", len(got))
	}
	if got[0].Status != types.ReceiptStatusSuccessful {
		t.Errorf("receipt[0] status = %d, want 1", got[0].Status)
	}
	if got[1].CumulativeGasUsed != 42000 {
		t.Errorf("receipt[1] cumulative gas = %d, want 42000", got[1].CumulativeGasUsed)
	}
}

func TestChainDB_ReadTransaction(t *testing.T) {
	db := NewMemoryDB()
	cdb := NewChainDB(db)

	block := testBlockWithTx(20)
	if err := cdb.WriteBlock(block); err != nil {
		t.Fatalf("WriteBlock: %v", err)
	}
	if err := cdb.WriteCanonicalHash(20, block.Hash()); err != nil {
		t.Fatalf("WriteCanonicalHash: %v", err)
	}

	txHash := block.Transactions()[0].Hash()
	tx, blockHash, blockNum := cdb.ReadTransaction(txHash)
	if tx == nil {
		t.Fatal("ReadTransaction returned nil")
	}
	if tx.Hash() != txHash {
		t.Errorf("tx hash mismatch")
	}
	if blockHash != block.Hash() {
		t.Errorf("block hash mismatch")
	}
	if blockNum != 20 {
		t.Errorf("block number = %d, want 20", blockNum)
	}

	// Non-existent tx.
	fakeTx, _, _ := cdb.ReadTransaction(types.Hash{0xff, 0xfe})
	if fakeTx != nil {
		t.Error("expected nil for non-existent tx")
	}
}

func TestChainDB_ReadWriteTd(t *testing.T) {
	db := NewMemoryDB()
	cdb := NewChainDB(db)

	// Must write a header first so hash->number mapping exists.
	block := testBlock(3)
	if err := cdb.WriteBlock(block); err != nil {
		t.Fatalf("WriteBlock: %v", err)
	}

	hash := block.Hash()
	td := big.NewInt(123456789)
	if err := cdb.WriteTd(hash, td); err != nil {
		t.Fatalf("WriteTd: %v", err)
	}

	got := cdb.ReadTd(hash)
	if got == nil {
		t.Fatal("ReadTd returned nil")
	}
	if got.Cmp(td) != 0 {
		t.Errorf("td = %v, want %v", got, td)
	}

	// Non-existent td.
	if cdb.ReadTd(types.Hash{0xaa}) != nil {
		t.Error("expected nil for non-existent td")
	}
}

func TestChainDB_CanonicalHash(t *testing.T) {
	db := NewMemoryDB()
	cdb := NewChainDB(db)

	hash := types.HexToHash("0xdeadbeef")
	if err := cdb.WriteCanonicalHash(42, hash); err != nil {
		t.Fatalf("WriteCanonicalHash: %v", err)
	}
	got, err := cdb.ReadCanonicalHash(42)
	if err != nil {
		t.Fatalf("ReadCanonicalHash: %v", err)
	}
	if got != hash {
		t.Errorf("canonical hash mismatch: got %s, want %s", got, hash)
	}

	// Non-existent.
	_, err = cdb.ReadCanonicalHash(999)
	if err == nil {
		t.Error("expected error for non-existent canonical hash")
	}
}

func TestChainDB_HeadBlockHash(t *testing.T) {
	db := NewMemoryDB()
	cdb := NewChainDB(db)

	hash := types.HexToHash("0xcafebabe")
	if err := cdb.WriteHeadBlockHash(hash); err != nil {
		t.Fatalf("WriteHeadBlockHash: %v", err)
	}
	got, err := cdb.ReadHeadBlockHash()
	if err != nil {
		t.Fatalf("ReadHeadBlockHash: %v", err)
	}
	if got != hash {
		t.Errorf("head block hash mismatch")
	}
}

func TestChainDB_CacheEviction(t *testing.T) {
	db := NewMemoryDB()
	cdb := NewChainDB(db)

	// Write more blocks than the cache can hold.
	blocks := make([]*types.Block, blockCacheSize+10)
	for i := range blocks {
		blocks[i] = testBlock(uint64(i))
		if err := cdb.WriteBlock(blocks[i]); err != nil {
			t.Fatalf("WriteBlock(%d): %v", i, err)
		}
	}

	// Even after eviction, blocks should still be readable from the DB.
	for _, block := range blocks {
		got := cdb.ReadBlock(block.Hash())
		if got == nil {
			t.Fatalf("ReadBlock returned nil for block %d after cache eviction", block.NumberU64())
		}
		if got.Hash() != block.Hash() {
			t.Errorf("hash mismatch for block %d", block.NumberU64())
		}
	}
}

func TestChainDB_ReadBlockNonExistent(t *testing.T) {
	db := NewMemoryDB()
	cdb := NewChainDB(db)

	if cdb.ReadBlock(types.Hash{0x01}) != nil {
		t.Error("expected nil for non-existent block")
	}
	if cdb.ReadHeader(types.Hash{0x01}) != nil {
		t.Error("expected nil for non-existent header")
	}
	if cdb.ReadReceipts(types.Hash{0x01}) != nil {
		t.Error("expected nil for non-existent receipts")
	}
}

func TestLRUCache_BasicOperations(t *testing.T) {
	cache := newLRU[string, int](3)

	cache.put("a", 1)
	cache.put("b", 2)
	cache.put("c", 3)

	if v, ok := cache.get("a"); !ok || v != 1 {
		t.Errorf("get(a) = %d, %v; want 1, true", v, ok)
	}

	// Adding a 4th item should evict the LRU ("b" since "a" was just accessed).
	cache.put("d", 4)
	if _, ok := cache.get("b"); ok {
		t.Error("b should have been evicted")
	}
	if v, ok := cache.get("d"); !ok || v != 4 {
		t.Errorf("get(d) = %d, %v; want 4, true", v, ok)
	}
}

func TestLRUCache_Update(t *testing.T) {
	cache := newLRU[string, int](3)
	cache.put("a", 1)
	cache.put("a", 2)

	if v, ok := cache.get("a"); !ok || v != 2 {
		t.Errorf("get(a) after update = %d, %v; want 2, true", v, ok)
	}
}

func TestLRUCache_Remove(t *testing.T) {
	cache := newLRU[string, int](3)
	cache.put("a", 1)
	cache.remove("a")

	if _, ok := cache.get("a"); ok {
		t.Error("a should have been removed")
	}
}

func TestChainDB_TdCacheIsolation(t *testing.T) {
	db := NewMemoryDB()
	cdb := NewChainDB(db)

	block := testBlock(1)
	if err := cdb.WriteBlock(block); err != nil {
		t.Fatalf("WriteBlock: %v", err)
	}
	hash := block.Hash()
	td := big.NewInt(100)
	if err := cdb.WriteTd(hash, td); err != nil {
		t.Fatalf("WriteTd: %v", err)
	}

	// Mutate the original td value.
	td.SetInt64(999)

	// Read should return the stored value, not the mutated one.
	got := cdb.ReadTd(hash)
	if got.Cmp(big.NewInt(100)) != 0 {
		t.Errorf("td = %v, want 100 (value isolation broken)", got)
	}
}
