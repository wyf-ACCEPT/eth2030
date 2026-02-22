package core

import (
	"bytes"
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

func makeTestPayload() *ExecutionPayload {
	return &ExecutionPayload{
		Transactions: [][]byte{
			{0x01, 0x02, 0x03, 0x04},
			{0x05, 0x06, 0x07, 0x08},
			{0x09, 0x0a, 0x0b, 0x0c},
		},
		Receipts: [][]byte{
			{0xaa, 0xbb, 0xcc, 0x00, 0x00, 0x00},
			{0xdd, 0xee, 0xff, 0x00, 0x00},
		},
		StateRoot: types.BytesToHash([]byte{0x11, 0x22, 0x33}),
		BlockHash: types.BytesToHash([]byte{0x44, 0x55, 0x66}),
		GasUsed:   21000,
		BaseFee:   1000000000,
	}
}

func TestNewPayloadCompressor(t *testing.T) {
	cfg := CompressorConfig{
		EnableDedup:        true,
		EnableReceiptPrune: true,
		MaxPayloadSize:     1 << 20,
	}
	pc := NewPayloadCompressor(cfg)
	if pc == nil {
		t.Fatal("NewPayloadCompressor returned nil")
	}
}

func TestCompressDecompress_RoundTrip(t *testing.T) {
	cfg := CompressorConfig{EnableDedup: true, EnableReceiptPrune: true}
	pc := NewPayloadCompressor(cfg)
	payload := makeTestPayload()

	compressed, err := pc.CompressPayload(payload)
	if err != nil {
		t.Fatalf("CompressPayload error: %v", err)
	}

	if compressed.OriginalSize == 0 {
		t.Fatal("OriginalSize should not be zero")
	}
	if compressed.CompressedSize == 0 {
		t.Fatal("CompressedSize should not be zero")
	}
	if len(compressed.Data) == 0 {
		t.Fatal("compressed Data should not be empty")
	}

	decompressed, err := pc.DecompressPayload(compressed)
	if err != nil {
		t.Fatalf("DecompressPayload error: %v", err)
	}

	// Verify fixed fields.
	if decompressed.StateRoot != payload.StateRoot {
		t.Errorf("StateRoot mismatch: got %s, want %s", decompressed.StateRoot, payload.StateRoot)
	}
	if decompressed.BlockHash != payload.BlockHash {
		t.Errorf("BlockHash mismatch: got %s, want %s", decompressed.BlockHash, payload.BlockHash)
	}
	if decompressed.GasUsed != payload.GasUsed {
		t.Errorf("GasUsed mismatch: got %d, want %d", decompressed.GasUsed, payload.GasUsed)
	}
	if decompressed.BaseFee != payload.BaseFee {
		t.Errorf("BaseFee mismatch: got %d, want %d", decompressed.BaseFee, payload.BaseFee)
	}

	// Transactions should match (no duplicates in test payload).
	if len(decompressed.Transactions) != len(payload.Transactions) {
		t.Fatalf("transaction count: got %d, want %d", len(decompressed.Transactions), len(payload.Transactions))
	}
	for i, tx := range decompressed.Transactions {
		if !bytes.Equal(tx, payload.Transactions[i]) {
			t.Errorf("tx[%d] mismatch", i)
		}
	}
}

func TestCompressPayload_NilPayload(t *testing.T) {
	pc := NewPayloadCompressor(CompressorConfig{})
	_, err := pc.CompressPayload(nil)
	if err == nil {
		t.Fatal("expected error for nil payload")
	}
}

func TestDecompressPayload_NilCompressed(t *testing.T) {
	pc := NewPayloadCompressor(CompressorConfig{})
	_, err := pc.DecompressPayload(nil)
	if err == nil {
		t.Fatal("expected error for nil compressed")
	}
}

func TestDecompressPayload_InvalidMagic(t *testing.T) {
	pc := NewPayloadCompressor(CompressorConfig{})
	_, err := pc.DecompressPayload(&CompressedPayload{Data: []byte{0, 0, 0, 0}})
	if err == nil {
		t.Fatal("expected error for invalid magic")
	}
}

func TestDecompressPayload_TooShort(t *testing.T) {
	pc := NewPayloadCompressor(CompressorConfig{})
	_, err := pc.DecompressPayload(&CompressedPayload{Data: []byte{0x01}})
	if err == nil {
		t.Fatal("expected error for data too short")
	}
}

func TestCompressPayload_MaxPayloadSize(t *testing.T) {
	cfg := CompressorConfig{MaxPayloadSize: 10} // very small limit
	pc := NewPayloadCompressor(cfg)
	payload := makeTestPayload()

	_, err := pc.CompressPayload(payload)
	if err == nil {
		t.Fatal("expected error for payload exceeding max size")
	}
}

func TestCompressPayload_NoCompression(t *testing.T) {
	cfg := CompressorConfig{} // all compression disabled
	pc := NewPayloadCompressor(cfg)
	payload := makeTestPayload()

	compressed, err := pc.CompressPayload(payload)
	if err != nil {
		t.Fatalf("CompressPayload error: %v", err)
	}
	if compressed.Method != MethodNone {
		t.Errorf("method = %d, want MethodNone (%d)", compressed.Method, MethodNone)
	}
}

func TestCompressPayload_DedupMethod(t *testing.T) {
	cfg := CompressorConfig{EnableDedup: true}
	pc := NewPayloadCompressor(cfg)

	// Create payload with duplicate transactions.
	dup := []byte{0x01, 0x02, 0x03}
	payload := &ExecutionPayload{
		Transactions: [][]byte{dup, {0x04, 0x05}, dup},
		StateRoot:    types.BytesToHash([]byte{0xab}),
		BlockHash:    types.BytesToHash([]byte{0xcd}),
		GasUsed:      100,
		BaseFee:      200,
	}

	compressed, err := pc.CompressPayload(payload)
	if err != nil {
		t.Fatalf("CompressPayload error: %v", err)
	}
	if compressed.Method != MethodTxDedup {
		t.Errorf("method = %d, want MethodTxDedup (%d)", compressed.Method, MethodTxDedup)
	}
}

func TestCompressPayload_ReceiptPruneMethod(t *testing.T) {
	cfg := CompressorConfig{EnableReceiptPrune: true}
	pc := NewPayloadCompressor(cfg)

	payload := &ExecutionPayload{
		Receipts:  [][]byte{{0xaa, 0x00, 0x00, 0x00, 0x00}},
		StateRoot: types.BytesToHash([]byte{0xab}),
		BlockHash: types.BytesToHash([]byte{0xcd}),
		GasUsed:   100,
		BaseFee:   200,
	}

	compressed, err := pc.CompressPayload(payload)
	if err != nil {
		t.Fatalf("CompressPayload error: %v", err)
	}
	if compressed.Method != MethodReceiptPrune {
		t.Errorf("method = %d, want MethodReceiptPrune (%d)", compressed.Method, MethodReceiptPrune)
	}
}

func TestDeduplicateTransactions_NoDuplicates(t *testing.T) {
	txs := [][]byte{{0x01}, {0x02}, {0x03}}
	deduped, indices := DeduplicateTransactions(txs)
	if len(deduped) != 3 {
		t.Fatalf("expected 3 unique txs, got %d", len(deduped))
	}
	if len(indices) != 0 {
		t.Fatalf("expected 0 dup indices, got %d", len(indices))
	}
}

func TestDeduplicateTransactions_WithDuplicates(t *testing.T) {
	tx1 := []byte{0x01, 0x02}
	tx2 := []byte{0x03, 0x04}
	txs := [][]byte{tx1, tx2, tx1, tx2, tx1}

	deduped, indices := DeduplicateTransactions(txs)
	if len(deduped) != 2 {
		t.Fatalf("expected 2 unique txs, got %d", len(deduped))
	}
	if len(indices) != 3 {
		t.Fatalf("expected 3 dup indices, got %d", len(indices))
	}

	// Verify deduped content.
	if !bytes.Equal(deduped[0], tx1) {
		t.Errorf("deduped[0] = %x, want %x", deduped[0], tx1)
	}
	if !bytes.Equal(deduped[1], tx2) {
		t.Errorf("deduped[1] = %x, want %x", deduped[1], tx2)
	}

	// Verify dup indices refer to later occurrences.
	expected := []int{2, 3, 4}
	for i, idx := range indices {
		if idx != expected[i] {
			t.Errorf("indices[%d] = %d, want %d", i, idx, expected[i])
		}
	}
}

func TestDeduplicateTransactions_Empty(t *testing.T) {
	deduped, indices := DeduplicateTransactions(nil)
	if deduped != nil {
		t.Fatal("expected nil for nil input")
	}
	if indices != nil {
		t.Fatal("expected nil indices for nil input")
	}
}

func TestPruneReceipts_TrailingZeros(t *testing.T) {
	receipts := [][]byte{
		{0xaa, 0xbb, 0x00, 0x00, 0x00},
		{0xcc, 0xdd, 0xee},
		{0x00, 0x00, 0x00, 0x00, 0x00},
	}

	pruned := PruneReceipts(receipts)
	if len(pruned) != 3 {
		t.Fatalf("expected 3 pruned receipts, got %d", len(pruned))
	}

	// First: trailing zeros stripped.
	if !bytes.Equal(pruned[0], []byte{0xaa, 0xbb}) {
		t.Errorf("pruned[0] = %x, want aabb", pruned[0])
	}
	// Second: no trailing zeros, unchanged.
	if !bytes.Equal(pruned[1], []byte{0xcc, 0xdd, 0xee}) {
		t.Errorf("pruned[1] = %x, want ccddee", pruned[1])
	}
	// Third: all zeros, keep one byte.
	if !bytes.Equal(pruned[2], []byte{0x00}) {
		t.Errorf("pruned[2] = %x, want 00", pruned[2])
	}
}

func TestPruneReceipts_ShortReceipt(t *testing.T) {
	receipts := [][]byte{{0x01, 0x00, 0x00}}
	pruned := PruneReceipts(receipts)
	// Short receipts (<= 4 bytes) are unchanged.
	if !bytes.Equal(pruned[0], receipts[0]) {
		t.Errorf("short receipt should not be modified: got %x, want %x", pruned[0], receipts[0])
	}
}

func TestPruneReceipts_Empty(t *testing.T) {
	pruned := PruneReceipts(nil)
	if len(pruned) != 0 {
		t.Fatalf("expected 0 pruned receipts, got %d", len(pruned))
	}
}

func TestEstimateCompression_NilPayload(t *testing.T) {
	pc := NewPayloadCompressor(CompressorConfig{})
	ratio := pc.EstimateCompression(nil)
	if ratio != 1.0 {
		t.Errorf("expected ratio 1.0 for nil, got %f", ratio)
	}
}

func TestEstimateCompression_NoCompression(t *testing.T) {
	pc := NewPayloadCompressor(CompressorConfig{})
	payload := makeTestPayload()
	ratio := pc.EstimateCompression(payload)
	if ratio != 1.0 {
		t.Errorf("expected ratio 1.0 with no compression, got %f", ratio)
	}
}

func TestEstimateCompression_WithDedup(t *testing.T) {
	pc := NewPayloadCompressor(CompressorConfig{EnableDedup: true})
	tx := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	payload := &ExecutionPayload{
		Transactions: [][]byte{tx, {0x06, 0x07}, tx},
		StateRoot:    types.BytesToHash([]byte{0xab}),
		BlockHash:    types.BytesToHash([]byte{0xcd}),
		GasUsed:      100,
		BaseFee:      200,
	}

	ratio := pc.EstimateCompression(payload)
	if ratio >= 1.0 {
		t.Errorf("expected ratio < 1.0 with duplicates, got %f", ratio)
	}
	if ratio <= 0.0 {
		t.Errorf("expected ratio > 0.0, got %f", ratio)
	}
}

func TestEstimateCompression_WithReceiptPrune(t *testing.T) {
	pc := NewPayloadCompressor(CompressorConfig{EnableReceiptPrune: true})
	payload := makeTestPayload()
	ratio := pc.EstimateCompression(payload)
	if ratio >= 1.0 {
		t.Errorf("expected ratio < 1.0 with receipt pruning, got %f", ratio)
	}
}

func TestEstimateCompression_EmptyPayload(t *testing.T) {
	pc := NewPayloadCompressor(CompressorConfig{EnableDedup: true, EnableReceiptPrune: true})
	payload := &ExecutionPayload{}
	ratio := pc.EstimateCompression(payload)
	// Only fixed fields (80 bytes), no txs or receipts to compress.
	if ratio != 1.0 {
		t.Errorf("expected ratio 1.0 for empty payload, got %f", ratio)
	}
}

func TestCompressionMethod_Values(t *testing.T) {
	// Verify enum ordering.
	if MethodNone != 0 {
		t.Errorf("MethodNone = %d, want 0", MethodNone)
	}
	if MethodTxDedup != 1 {
		t.Errorf("MethodTxDedup = %d, want 1", MethodTxDedup)
	}
	if MethodReceiptPrune != 2 {
		t.Errorf("MethodReceiptPrune = %d, want 2", MethodReceiptPrune)
	}
	if MethodStateDelta != 3 {
		t.Errorf("MethodStateDelta = %d, want 3", MethodStateDelta)
	}
}

func TestPayloadSize(t *testing.T) {
	p := &ExecutionPayload{
		Transactions: [][]byte{{0x01, 0x02}, {0x03}},
		Receipts:     [][]byte{{0xaa, 0xbb, 0xcc}},
	}
	// 2 + 1 + 3 + 80 = 86
	if s := payloadSize(p); s != 86 {
		t.Errorf("payloadSize = %d, want 86", s)
	}
}

func TestCompressDecompress_EmptyPayload(t *testing.T) {
	pc := NewPayloadCompressor(CompressorConfig{})
	payload := &ExecutionPayload{
		StateRoot: types.BytesToHash([]byte{0x01}),
		BlockHash: types.BytesToHash([]byte{0x02}),
		GasUsed:   42,
		BaseFee:   99,
	}

	compressed, err := pc.CompressPayload(payload)
	if err != nil {
		t.Fatalf("CompressPayload error: %v", err)
	}

	decompressed, err := pc.DecompressPayload(compressed)
	if err != nil {
		t.Fatalf("DecompressPayload error: %v", err)
	}

	if decompressed.StateRoot != payload.StateRoot {
		t.Error("StateRoot mismatch")
	}
	if decompressed.GasUsed != payload.GasUsed {
		t.Error("GasUsed mismatch")
	}
	if decompressed.BaseFee != payload.BaseFee {
		t.Error("BaseFee mismatch")
	}
	if len(decompressed.Transactions) != 0 {
		t.Error("expected empty transactions")
	}
	if len(decompressed.Receipts) != 0 {
		t.Error("expected empty receipts")
	}
}

func TestCompressDecompress_LargePayload(t *testing.T) {
	cfg := CompressorConfig{EnableDedup: true, EnableReceiptPrune: true}
	pc := NewPayloadCompressor(cfg)

	// Generate payload with many transactions.
	txs := make([][]byte, 100)
	for i := range txs {
		txs[i] = make([]byte, 256)
		for j := range txs[i] {
			txs[i][j] = byte(i + j)
		}
	}

	payload := &ExecutionPayload{
		Transactions: txs,
		Receipts:     [][]byte{{0xff, 0x00, 0x00, 0x00, 0x00}},
		StateRoot:    crypto.Keccak256Hash([]byte("state")),
		BlockHash:    crypto.Keccak256Hash([]byte("block")),
		GasUsed:      1000000,
		BaseFee:      50000000000,
	}

	compressed, err := pc.CompressPayload(payload)
	if err != nil {
		t.Fatalf("CompressPayload error: %v", err)
	}

	decompressed, err := pc.DecompressPayload(compressed)
	if err != nil {
		t.Fatalf("DecompressPayload error: %v", err)
	}

	if len(decompressed.Transactions) != len(payload.Transactions) {
		t.Fatalf("tx count: got %d, want %d", len(decompressed.Transactions), len(payload.Transactions))
	}
	if decompressed.StateRoot != payload.StateRoot {
		t.Error("StateRoot mismatch")
	}
}

func TestPayloadCompressor_ThreadSafety(t *testing.T) {
	cfg := CompressorConfig{EnableDedup: true, EnableReceiptPrune: true}
	pc := NewPayloadCompressor(cfg)

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			payload := &ExecutionPayload{
				Transactions: [][]byte{{byte(id), 0x01}, {byte(id), 0x02}},
				Receipts:     [][]byte{{byte(id), 0x00, 0x00, 0x00, 0x00}},
				StateRoot:    types.BytesToHash([]byte{byte(id)}),
				BlockHash:    types.BytesToHash([]byte{byte(id + 1)}),
				GasUsed:      uint64(id * 1000),
				BaseFee:      uint64(id * 100),
			}

			compressed, err := pc.CompressPayload(payload)
			if err != nil {
				t.Errorf("goroutine %d: CompressPayload error: %v", id, err)
				return
			}

			_, err = pc.DecompressPayload(compressed)
			if err != nil {
				t.Errorf("goroutine %d: DecompressPayload error: %v", id, err)
				return
			}

			_ = pc.EstimateCompression(payload)
		}(i)
	}
	wg.Wait()
}

func TestCompressPayload_DedupReducesSize(t *testing.T) {
	tx := make([]byte, 200)
	for i := range tx {
		tx[i] = byte(i)
	}

	payload := &ExecutionPayload{
		Transactions: [][]byte{tx, tx, tx, tx, tx},
		StateRoot:    types.BytesToHash([]byte{0x01}),
		BlockHash:    types.BytesToHash([]byte{0x02}),
		GasUsed:      100,
		BaseFee:      200,
	}

	// Without dedup.
	pcNone := NewPayloadCompressor(CompressorConfig{})
	compNone, err := pcNone.CompressPayload(payload)
	if err != nil {
		t.Fatal(err)
	}

	// With dedup.
	pcDedup := NewPayloadCompressor(CompressorConfig{EnableDedup: true})
	compDedup, err := pcDedup.CompressPayload(payload)
	if err != nil {
		t.Fatal(err)
	}

	if compDedup.CompressedSize >= compNone.CompressedSize {
		t.Errorf("dedup compressed size (%d) should be smaller than no-compression (%d)",
			compDedup.CompressedSize, compNone.CompressedSize)
	}
}
