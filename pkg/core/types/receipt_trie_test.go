package types

import (
	"bytes"
	"math/big"
	"testing"
)

func makeTrieReceipt(txType uint8, status uint64, cumGas uint64, gasUsed uint64) *Receipt {
	return &Receipt{
		Type:              txType,
		Status:            status,
		CumulativeGasUsed: cumGas,
		GasUsed:           gasUsed,
	}
}

func makeTrieReceiptWithLogs(txType uint8, status uint64, cumGas uint64, gasUsed uint64) *Receipt {
	r := makeTrieReceipt(txType, status, cumGas, gasUsed)
	r.Logs = []*Log{
		{
			Address: HexToAddress("0x1234567890abcdef1234567890abcdef12345678"),
			Topics: []Hash{
				HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"),
			},
			Data: []byte{0x01, 0x02, 0x03, 0x04},
		},
	}
	return r
}

func TestNewReceiptTrie(t *testing.T) {
	trie := NewReceiptTrie(DefaultReceiptTrieConfig())
	if trie == nil {
		t.Fatal("NewReceiptTrie returned nil")
	}
	if trie.Size() != 0 {
		t.Errorf("new trie size = %d, want 0", trie.Size())
	}
}

func TestDefaultReceiptTrieConfig(t *testing.T) {
	cfg := DefaultReceiptTrieConfig()
	if !cfg.UseCompactEncoding {
		t.Error("UseCompactEncoding should default to true")
	}
	if cfg.MaxReceiptsPerBlock != 0 {
		t.Errorf("MaxReceiptsPerBlock = %d, want 0", cfg.MaxReceiptsPerBlock)
	}
	if cfg.PruneDepth != 1024 {
		t.Errorf("PruneDepth = %d, want 1024", cfg.PruneDepth)
	}
}

func TestReceiptTrie_InsertAndGet(t *testing.T) {
	trie := NewReceiptTrie(DefaultReceiptTrieConfig())

	r := makeTrieReceipt(0, ReceiptStatusSuccessful, 21000, 21000)
	if err := trie.Insert(0, r); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if trie.Size() != 1 {
		t.Errorf("size = %d, want 1", trie.Size())
	}

	got, err := trie.Get(0)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != ReceiptStatusSuccessful {
		t.Errorf("status = %d, want %d", got.Status, ReceiptStatusSuccessful)
	}
	if got.CumulativeGasUsed != 21000 {
		t.Errorf("cumGas = %d, want 21000", got.CumulativeGasUsed)
	}
}

func TestReceiptTrie_InsertNil(t *testing.T) {
	trie := NewReceiptTrie(DefaultReceiptTrieConfig())
	err := trie.Insert(0, nil)
	if err == nil {
		t.Fatal("expected error for nil receipt")
	}
}

func TestReceiptTrie_InsertReplace(t *testing.T) {
	trie := NewReceiptTrie(DefaultReceiptTrieConfig())

	r1 := makeTrieReceipt(0, ReceiptStatusSuccessful, 21000, 21000)
	r2 := makeTrieReceipt(0, ReceiptStatusFailed, 42000, 42000)

	_ = trie.Insert(0, r1)
	_ = trie.Insert(0, r2) // replace

	if trie.Size() != 1 {
		t.Errorf("size = %d, want 1 after replace", trie.Size())
	}

	got, _ := trie.Get(0)
	if got.Status != ReceiptStatusFailed {
		t.Error("expected replaced receipt to have failed status")
	}
}

func TestReceiptTrie_InsertMaxLimit(t *testing.T) {
	cfg := DefaultReceiptTrieConfig()
	cfg.MaxReceiptsPerBlock = 2
	trie := NewReceiptTrie(cfg)

	_ = trie.Insert(0, makeTrieReceipt(0, 1, 21000, 21000))
	_ = trie.Insert(1, makeTrieReceipt(0, 1, 42000, 21000))

	err := trie.Insert(2, makeTrieReceipt(0, 1, 63000, 21000))
	if err == nil {
		t.Fatal("expected error when exceeding max receipts")
	}

	// Replacing an existing index should still work.
	err = trie.Insert(1, makeTrieReceipt(0, 0, 42000, 21000))
	if err != nil {
		t.Fatalf("replacing at max capacity should work: %v", err)
	}
}

func TestReceiptTrie_GetNotFound(t *testing.T) {
	trie := NewReceiptTrie(DefaultReceiptTrieConfig())
	_, err := trie.Get(999)
	if err == nil {
		t.Fatal("expected error for missing index")
	}
}

func TestReceiptTrie_GetMultiple(t *testing.T) {
	trie := NewReceiptTrie(DefaultReceiptTrieConfig())

	for i := uint64(0); i < 5; i++ {
		r := makeTrieReceipt(0, ReceiptStatusSuccessful, (i+1)*21000, 21000)
		_ = trie.Insert(i, r)
	}

	if trie.Size() != 5 {
		t.Errorf("size = %d, want 5", trie.Size())
	}

	for i := uint64(0); i < 5; i++ {
		got, err := trie.Get(i)
		if err != nil {
			t.Fatalf("Get(%d): %v", i, err)
		}
		if got.CumulativeGasUsed != (i+1)*21000 {
			t.Errorf("Get(%d) cumGas = %d, want %d", i, got.CumulativeGasUsed, (i+1)*21000)
		}
	}
}

func TestReceiptTrie_ComputeRoot_Empty(t *testing.T) {
	trie := NewReceiptTrie(DefaultReceiptTrieConfig())
	root := trie.ComputeRoot()
	if root != EmptyRootHash {
		t.Errorf("empty trie root = %s, want EmptyRootHash", root.Hex())
	}
}

func TestReceiptTrie_ComputeRoot_Single(t *testing.T) {
	trie := NewReceiptTrie(DefaultReceiptTrieConfig())
	_ = trie.Insert(0, makeTrieReceipt(0, 1, 21000, 21000))

	root := trie.ComputeRoot()
	if root.IsZero() {
		t.Error("root should not be zero for non-empty trie")
	}
	if root == EmptyRootHash {
		t.Error("root should differ from empty root hash")
	}
}

func TestReceiptTrie_ComputeRoot_Deterministic(t *testing.T) {
	trie1 := NewReceiptTrie(DefaultReceiptTrieConfig())
	trie2 := NewReceiptTrie(DefaultReceiptTrieConfig())

	receipts := []*Receipt{
		makeTrieReceipt(0, 1, 21000, 21000),
		makeTrieReceiptWithLogs(2, 1, 42000, 21000),
		makeTrieReceipt(0, 0, 63000, 21000),
	}

	// Insert in different orders.
	for i, r := range receipts {
		_ = trie1.Insert(uint64(i), r)
	}
	// Reverse order.
	for i := len(receipts) - 1; i >= 0; i-- {
		_ = trie2.Insert(uint64(i), receipts[i])
	}

	root1 := trie1.ComputeRoot()
	root2 := trie2.ComputeRoot()

	if root1 != root2 {
		t.Errorf("root should be deterministic: %s != %s", root1.Hex(), root2.Hex())
	}
}

func TestReceiptTrie_ComputeRoot_DiffersForDifferentReceipts(t *testing.T) {
	trie1 := NewReceiptTrie(DefaultReceiptTrieConfig())
	trie2 := NewReceiptTrie(DefaultReceiptTrieConfig())

	_ = trie1.Insert(0, makeTrieReceipt(0, 1, 21000, 21000))
	_ = trie2.Insert(0, makeTrieReceipt(0, 0, 21000, 21000)) // different status

	root1 := trie1.ComputeRoot()
	root2 := trie2.ComputeRoot()

	if root1 == root2 {
		t.Error("different receipts should produce different roots")
	}
}

func TestReceiptTrieCompactEncode_Nil(t *testing.T) {
	enc := ReceiptTrieCompactEncode(nil)
	if enc != nil {
		t.Error("compact encode of nil should return nil")
	}
}

func TestReceiptTrieCompactEncode_Simple(t *testing.T) {
	r := makeTrieReceipt(0, 1, 21000, 21000)
	enc := ReceiptTrieCompactEncode(r)
	if len(enc) == 0 {
		t.Fatal("compact encoding should not be empty")
	}
	// Minimum header: 1+1+8+8+4 = 22 bytes
	if len(enc) < 22 {
		t.Errorf("encoded length = %d, expected >= 22", len(enc))
	}
}

func TestReceiptTrieCompactEncodeDecode_Roundtrip(t *testing.T) {
	r := makeTrieReceipt(2, 1, 42000, 21000)
	r.Logs = []*Log{
		{
			Address: HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
			Topics: []Hash{
				HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
				HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
			},
			Data: []byte{0xDE, 0xAD, 0xBE, 0xEF},
		},
		{
			Address: HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
			Topics:  nil,
			Data:    nil,
		},
	}

	enc := ReceiptTrieCompactEncode(r)
	dec, err := ReceiptTrieCompactDecode(enc)
	if err != nil {
		t.Fatalf("CompactDecode: %v", err)
	}

	if dec.Type != r.Type {
		t.Errorf("Type = %d, want %d", dec.Type, r.Type)
	}
	if dec.Status != r.Status {
		t.Errorf("Status = %d, want %d", dec.Status, r.Status)
	}
	if dec.CumulativeGasUsed != r.CumulativeGasUsed {
		t.Errorf("CumulativeGasUsed = %d, want %d", dec.CumulativeGasUsed, r.CumulativeGasUsed)
	}
	if dec.GasUsed != r.GasUsed {
		t.Errorf("GasUsed = %d, want %d", dec.GasUsed, r.GasUsed)
	}
	if len(dec.Logs) != len(r.Logs) {
		t.Fatalf("log count = %d, want %d", len(dec.Logs), len(r.Logs))
	}

	// Check first log.
	if dec.Logs[0].Address != r.Logs[0].Address {
		t.Error("log[0] address mismatch")
	}
	if len(dec.Logs[0].Topics) != 2 {
		t.Errorf("log[0] topics = %d, want 2", len(dec.Logs[0].Topics))
	}
	if dec.Logs[0].Topics[0] != r.Logs[0].Topics[0] {
		t.Error("log[0] topic[0] mismatch")
	}
	if !bytes.Equal(dec.Logs[0].Data, r.Logs[0].Data) {
		t.Error("log[0] data mismatch")
	}

	// Check second log (empty topics and data).
	if dec.Logs[1].Address != r.Logs[1].Address {
		t.Error("log[1] address mismatch")
	}
	if len(dec.Logs[1].Topics) != 0 {
		t.Errorf("log[1] topics = %d, want 0", len(dec.Logs[1].Topics))
	}
	if len(dec.Logs[1].Data) != 0 {
		t.Errorf("log[1] data length = %d, want 0", len(dec.Logs[1].Data))
	}
}

func TestReceiptTrieCompactEncodeDecode_NoLogs(t *testing.T) {
	r := makeTrieReceipt(0, 0, 100000, 50000)
	enc := ReceiptTrieCompactEncode(r)
	dec, err := ReceiptTrieCompactDecode(enc)
	if err != nil {
		t.Fatalf("CompactDecode: %v", err)
	}
	if len(dec.Logs) != 0 {
		t.Errorf("expected 0 logs, got %d", len(dec.Logs))
	}
}

func TestReceiptTrieCompactDecode_TooShort(t *testing.T) {
	_, err := ReceiptTrieCompactDecode([]byte{0x01})
	if err == nil {
		t.Fatal("expected error for too-short data")
	}
}

func TestReceiptTrieCompactDecode_Empty(t *testing.T) {
	_, err := ReceiptTrieCompactDecode(nil)
	if err == nil {
		t.Fatal("expected error for nil data")
	}
}

func TestReceiptTrieCompactEncode_LargeData(t *testing.T) {
	r := makeTrieReceipt(2, 1, 1000000, 500000)
	r.Logs = make([]*Log, 10)
	for i := 0; i < 10; i++ {
		r.Logs[i] = &Log{
			Address: HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc"),
			Topics: []Hash{
				HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
			},
			Data: make([]byte, 256),
		}
	}

	enc := ReceiptTrieCompactEncode(r)
	dec, err := ReceiptTrieCompactDecode(enc)
	if err != nil {
		t.Fatalf("CompactDecode: %v", err)
	}
	if len(dec.Logs) != 10 {
		t.Errorf("log count = %d, want 10", len(dec.Logs))
	}
}

func TestReceiptTrie_Prune(t *testing.T) {
	trie := NewReceiptTrie(DefaultReceiptTrieConfig())

	for i := uint64(0); i < 10; i++ {
		_ = trie.Insert(i, makeTrieReceipt(0, 1, (i+1)*21000, 21000))
	}

	if trie.Size() != 10 {
		t.Fatalf("size before prune = %d, want 10", trie.Size())
	}

	trie.Prune(5) // keep last 5

	if trie.Size() != 5 {
		t.Errorf("size after prune = %d, want 5", trie.Size())
	}

	// The first 5 should be removed (indices 0-4).
	for i := uint64(0); i < 5; i++ {
		if _, err := trie.Get(i); err == nil {
			t.Errorf("index %d should be pruned", i)
		}
	}

	// The last 5 should remain (indices 5-9).
	for i := uint64(5); i < 10; i++ {
		if _, err := trie.Get(i); err != nil {
			t.Errorf("index %d should still exist: %v", i, err)
		}
	}
}

func TestReceiptTrie_PruneAll(t *testing.T) {
	trie := NewReceiptTrie(DefaultReceiptTrieConfig())
	_ = trie.Insert(0, makeTrieReceipt(0, 1, 21000, 21000))
	_ = trie.Insert(1, makeTrieReceipt(0, 1, 42000, 21000))

	trie.Prune(0)

	if trie.Size() != 0 {
		t.Errorf("size after full prune = %d, want 0", trie.Size())
	}
}

func TestReceiptTrie_PruneNegative(t *testing.T) {
	trie := NewReceiptTrie(DefaultReceiptTrieConfig())
	_ = trie.Insert(0, makeTrieReceipt(0, 1, 21000, 21000))

	trie.Prune(-1) // negative should prune all

	if trie.Size() != 0 {
		t.Errorf("size after negative prune = %d, want 0", trie.Size())
	}
}

func TestReceiptTrie_PruneMoreThanExists(t *testing.T) {
	trie := NewReceiptTrie(DefaultReceiptTrieConfig())
	_ = trie.Insert(0, makeTrieReceipt(0, 1, 21000, 21000))

	trie.Prune(100) // keep more than exists

	if trie.Size() != 1 {
		t.Errorf("size should remain 1, got %d", trie.Size())
	}
}

func TestReceiptTrie_Reset(t *testing.T) {
	trie := NewReceiptTrie(DefaultReceiptTrieConfig())
	_ = trie.Insert(0, makeTrieReceipt(0, 1, 21000, 21000))
	_ = trie.Insert(1, makeTrieReceipt(0, 1, 42000, 21000))

	trie.Reset()

	if trie.Size() != 0 {
		t.Errorf("size after reset = %d, want 0", trie.Size())
	}

	root := trie.ComputeRoot()
	if root != EmptyRootHash {
		t.Error("root after reset should be EmptyRootHash")
	}

	// Can insert again after reset.
	err := trie.Insert(0, makeTrieReceipt(0, 1, 21000, 21000))
	if err != nil {
		t.Fatalf("Insert after reset: %v", err)
	}
	if trie.Size() != 1 {
		t.Errorf("size after re-insert = %d, want 1", trie.Size())
	}
}

func TestReceiptTrie_Indices(t *testing.T) {
	trie := NewReceiptTrie(DefaultReceiptTrieConfig())
	_ = trie.Insert(5, makeTrieReceipt(0, 1, 21000, 21000))
	_ = trie.Insert(2, makeTrieReceipt(0, 1, 42000, 21000))
	_ = trie.Insert(8, makeTrieReceipt(0, 1, 63000, 21000))

	indices := trie.Indices()
	if len(indices) != 3 {
		t.Fatalf("expected 3 indices, got %d", len(indices))
	}
	if indices[0] != 2 || indices[1] != 5 || indices[2] != 8 {
		t.Errorf("indices = %v, want [2 5 8]", indices)
	}
}

func TestReceiptTrie_WithRLPEncoding(t *testing.T) {
	cfg := DefaultReceiptTrieConfig()
	cfg.UseCompactEncoding = false
	trie := NewReceiptTrie(cfg)

	r := makeTrieReceiptWithLogs(0, 1, 21000, 21000)
	if err := trie.Insert(0, r); err != nil {
		t.Fatalf("Insert with RLP encoding: %v", err)
	}

	got, err := trie.Get(0)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != 1 {
		t.Errorf("status = %d, want 1", got.Status)
	}

	root := trie.ComputeRoot()
	if root.IsZero() || root == EmptyRootHash {
		t.Error("root should not be empty for non-empty trie with RLP encoding")
	}
}

func TestReceiptTrie_CompactVsRLPDifferentRoots(t *testing.T) {
	compactTrie := NewReceiptTrie(ReceiptTrieConfig{UseCompactEncoding: true})
	rlpTrie := NewReceiptTrie(ReceiptTrieConfig{UseCompactEncoding: false})

	r := makeTrieReceiptWithLogs(0, 1, 21000, 21000)
	_ = compactTrie.Insert(0, r)
	_ = rlpTrie.Insert(0, r)

	compactRoot := compactTrie.ComputeRoot()
	rlpRoot := rlpTrie.ComputeRoot()

	// Different encodings should produce different roots.
	if compactRoot == rlpRoot {
		t.Error("compact and RLP encoding should produce different roots")
	}
}

func TestReceiptTrie_ReceiptWithBlobFields(t *testing.T) {
	trie := NewReceiptTrie(DefaultReceiptTrieConfig())

	r := &Receipt{
		Type:              3, // EIP-4844 blob tx
		Status:            1,
		CumulativeGasUsed: 100000,
		GasUsed:           50000,
		BlobGasUsed:       131072,
		BlobGasPrice:      big.NewInt(1000000000),
		Logs:              []*Log{},
	}

	if err := trie.Insert(0, r); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := trie.Get(0)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.BlobGasUsed != 131072 {
		t.Errorf("BlobGasUsed = %d, want 131072", got.BlobGasUsed)
	}
}

func TestReceiptTrie_ComputeRoot_MerkleTree(t *testing.T) {
	trie := NewReceiptTrie(DefaultReceiptTrieConfig())

	// Insert receipts to test tree with odd number of leaves.
	for i := uint64(0); i < 5; i++ {
		_ = trie.Insert(i, makeTrieReceipt(0, 1, (i+1)*21000, 21000))
	}

	root5 := trie.ComputeRoot()
	if root5.IsZero() {
		t.Error("5-receipt root should not be zero")
	}

	// Adding a 6th receipt should change the root.
	_ = trie.Insert(5, makeTrieReceipt(0, 1, 6*21000, 21000))
	root6 := trie.ComputeRoot()
	if root6 == root5 {
		t.Error("root should change when adding a receipt")
	}
}

func TestReceiptTrie_PruneThenComputeRoot(t *testing.T) {
	trie := NewReceiptTrie(DefaultReceiptTrieConfig())

	for i := uint64(0); i < 10; i++ {
		_ = trie.Insert(i, makeTrieReceipt(0, 1, (i+1)*21000, 21000))
	}

	rootBefore := trie.ComputeRoot()
	trie.Prune(5) // keep last 5
	rootAfter := trie.ComputeRoot()

	if rootBefore == rootAfter {
		t.Error("root should change after pruning")
	}
	if rootAfter.IsZero() || rootAfter == EmptyRootHash {
		t.Error("root after partial prune should not be empty")
	}
}

func TestReceiptTrie_ResetThenComputeRoot(t *testing.T) {
	trie := NewReceiptTrie(DefaultReceiptTrieConfig())
	_ = trie.Insert(0, makeTrieReceipt(0, 1, 21000, 21000))

	trie.Reset()
	root := trie.ComputeRoot()
	if root != EmptyRootHash {
		t.Error("root after reset should be EmptyRootHash")
	}
}
