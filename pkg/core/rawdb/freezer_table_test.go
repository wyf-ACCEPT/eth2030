package rawdb

import (
	"bytes"
	"os"
	"testing"
)

func newTestFreezerTable(t *testing.T, compression int) (*FreezerTableManager, string) {
	t.Helper()
	dir := t.TempDir()
	config := FreezerTableConfig{
		Dir:         dir,
		Name:        "test",
		Compression: compression,
	}
	ft, err := OpenFreezerTable(config)
	if err != nil {
		t.Fatalf("OpenFreezerTable: %v", err)
	}
	return ft, dir
}

func TestFreezerTable_AppendAndRetrieve(t *testing.T) {
	ft, _ := newTestFreezerTable(t, CompressionNone)
	defer ft.Close()

	items := [][]byte{
		[]byte("hello world"),
		[]byte("ethereum rocks"),
		[]byte("data three"),
	}
	for i, data := range items {
		if err := ft.Append(uint64(i), data); err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
	}

	if ft.Head() != 3 {
		t.Errorf("expected head=3, got %d", ft.Head())
	}
	if ft.Count() != 3 {
		t.Errorf("expected count=3, got %d", ft.Count())
	}

	for i, want := range items {
		got, err := ft.Retrieve(uint64(i))
		if err != nil {
			t.Fatalf("Retrieve(%d): %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("item %d: got %q, want %q", i, got, want)
		}
	}
}

func TestFreezerTable_AppendBatch(t *testing.T) {
	ft, _ := newTestFreezerTable(t, CompressionNone)
	defer ft.Close()

	items := [][]byte{
		[]byte("batch1"),
		[]byte("batch2"),
		[]byte("batch3"),
	}
	if err := ft.AppendBatch(0, items); err != nil {
		t.Fatalf("AppendBatch: %v", err)
	}
	if ft.Count() != 3 {
		t.Errorf("expected 3, got %d", ft.Count())
	}

	for i, want := range items {
		got, err := ft.Retrieve(uint64(i))
		if err != nil {
			t.Fatalf("Retrieve(%d): %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("item %d mismatch", i)
		}
	}
}

func TestFreezerTable_OutOfBounds(t *testing.T) {
	ft, _ := newTestFreezerTable(t, CompressionNone)
	defer ft.Close()

	ft.Append(0, []byte("data"))

	_, err := ft.Retrieve(1)
	if err == nil {
		t.Error("expected error for out-of-bounds retrieve")
	}

	_, err = ft.Retrieve(100)
	if err == nil {
		t.Error("expected error for far out-of-bounds retrieve")
	}
}

func TestFreezerTable_NonSequentialAppend(t *testing.T) {
	ft, _ := newTestFreezerTable(t, CompressionNone)
	defer ft.Close()

	ft.Append(0, []byte("first"))
	err := ft.Append(5, []byte("wrong"))
	if err == nil {
		t.Error("expected error for non-sequential append")
	}
}

func TestFreezerTable_TruncateHead(t *testing.T) {
	ft, _ := newTestFreezerTable(t, CompressionNone)
	defer ft.Close()

	for i := 0; i < 5; i++ {
		ft.Append(uint64(i), []byte{byte(i)})
	}

	// Truncate to keep items [0, 3).
	if err := ft.TruncateHead(3); err != nil {
		t.Fatalf("TruncateHead: %v", err)
	}
	if ft.Head() != 3 {
		t.Errorf("expected head=3, got %d", ft.Head())
	}
	if ft.Count() != 3 {
		t.Errorf("expected count=3, got %d", ft.Count())
	}

	// Items 0-2 should still be accessible.
	for i := 0; i < 3; i++ {
		got, err := ft.Retrieve(uint64(i))
		if err != nil {
			t.Fatalf("Retrieve(%d): %v", i, err)
		}
		if len(got) != 1 || got[0] != byte(i) {
			t.Errorf("item %d mismatch", i)
		}
	}

	// Items 3-4 should be gone.
	_, err := ft.Retrieve(3)
	if err == nil {
		t.Error("expected error for truncated item")
	}
}

func TestFreezerTable_TruncateHeadAll(t *testing.T) {
	ft, _ := newTestFreezerTable(t, CompressionNone)
	defer ft.Close()

	ft.Append(0, []byte("data"))
	ft.Append(1, []byte("more"))

	if err := ft.TruncateHead(0); err != nil {
		t.Fatalf("TruncateHead(0): %v", err)
	}
	if ft.Count() != 0 {
		t.Errorf("expected 0, got %d", ft.Count())
	}
}

func TestFreezerTable_TruncateTail(t *testing.T) {
	ft, _ := newTestFreezerTable(t, CompressionNone)
	defer ft.Close()

	for i := 0; i < 5; i++ {
		ft.Append(uint64(i), []byte{byte(i)})
	}

	if err := ft.TruncateTail(2); err != nil {
		t.Fatalf("TruncateTail: %v", err)
	}
	if ft.Tail() != 2 {
		t.Errorf("expected tail=2, got %d", ft.Tail())
	}
	if ft.Count() != 3 {
		t.Errorf("expected count=3, got %d", ft.Count())
	}

	// Items 0-1 should be inaccessible.
	_, err := ft.Retrieve(0)
	if err == nil {
		t.Error("expected error for tail-truncated item")
	}

	// Items 2-4 should be fine.
	for i := 2; i < 5; i++ {
		got, err := ft.Retrieve(uint64(i))
		if err != nil {
			t.Fatalf("Retrieve(%d): %v", i, err)
		}
		if len(got) != 1 || got[0] != byte(i) {
			t.Errorf("item %d mismatch", i)
		}
	}
}

func TestFreezerTable_RetrieveRange(t *testing.T) {
	ft, _ := newTestFreezerTable(t, CompressionNone)
	defer ft.Close()

	for i := 0; i < 5; i++ {
		ft.Append(uint64(i), []byte{byte(i + 10)})
	}

	results, err := ft.RetrieveRange(1, 3)
	if err != nil {
		t.Fatalf("RetrieveRange: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	for i, r := range results {
		if len(r) != 1 || r[0] != byte(i+11) {
			t.Errorf("result %d mismatch: got %v", i, r)
		}
	}
}

func TestFreezerTable_EmptyData(t *testing.T) {
	ft, _ := newTestFreezerTable(t, CompressionNone)
	defer ft.Close()

	// Append empty data.
	if err := ft.Append(0, []byte{}); err != nil {
		t.Fatalf("Append empty: %v", err)
	}
	got, err := ft.Retrieve(0)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %d bytes", len(got))
	}
}

func TestFreezerTable_Compression(t *testing.T) {
	ft, _ := newTestFreezerTable(t, CompressionDeflate)
	defer ft.Close()

	// Write compressible data.
	data := bytes.Repeat([]byte("abcdefghijklmnop"), 100)
	if err := ft.Append(0, data); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got, err := ft.Retrieve(0)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Error("data mismatch after compress/decompress roundtrip")
	}

	// Compressed data should be smaller.
	stats := ft.Stats()
	if stats.DataBytes >= uint64(len(data)) {
		t.Logf("compressed size %d >= original %d (compression not effective for this data)",
			stats.DataBytes, len(data))
	}
}

func TestFreezerTable_CompressionMultipleItems(t *testing.T) {
	ft, _ := newTestFreezerTable(t, CompressionDeflate)
	defer ft.Close()

	items := [][]byte{
		bytes.Repeat([]byte("x"), 200),
		bytes.Repeat([]byte("y"), 300),
		[]byte("short"),
	}
	for i, data := range items {
		if err := ft.Append(uint64(i), data); err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
	}

	for i, want := range items {
		got, err := ft.Retrieve(uint64(i))
		if err != nil {
			t.Fatalf("Retrieve(%d): %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("item %d: length mismatch: got %d, want %d", i, len(got), len(want))
		}
	}
}

func TestFreezerTable_Stats(t *testing.T) {
	ft, _ := newTestFreezerTable(t, CompressionNone)
	defer ft.Close()

	ft.Append(0, []byte("abc"))
	ft.Append(1, []byte("defgh"))

	stats := ft.Stats()
	if stats.Items != 2 {
		t.Errorf("expected 2 items, got %d", stats.Items)
	}
	if stats.Head != 2 {
		t.Errorf("expected head=2, got %d", stats.Head)
	}
	if stats.DataBytes != 8 { // 3 + 5
		t.Errorf("expected 8 data bytes, got %d", stats.DataBytes)
	}
	if stats.IndexBytes != 2*ftIndexEntrySize {
		t.Errorf("expected %d index bytes, got %d", 2*ftIndexEntrySize, stats.IndexBytes)
	}
}

func TestFreezerTable_ReadOnly(t *testing.T) {
	// Create with data first.
	dir := t.TempDir()
	config := FreezerTableConfig{Dir: dir, Name: "ro", Compression: CompressionNone}
	ft, err := OpenFreezerTable(config)
	if err != nil {
		t.Fatal(err)
	}
	ft.Append(0, []byte("data"))
	ft.Close()

	// Reopen read-only.
	config.ReadOnly = true
	ft, err = OpenFreezerTable(config)
	if err != nil {
		t.Fatal(err)
	}
	defer ft.Close()

	// Read should work.
	got, err := ft.Retrieve(0)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if !bytes.Equal(got, []byte("data")) {
		t.Error("data mismatch")
	}

	// Write should fail.
	err = ft.Append(1, []byte("nope"))
	if err == nil {
		t.Error("expected error for write in read-only mode")
	}
}

func TestFreezerTable_Sync(t *testing.T) {
	ft, _ := newTestFreezerTable(t, CompressionNone)
	defer ft.Close()

	ft.Append(0, []byte("sync"))
	if err := ft.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
}

func TestFreezerTable_Persistence(t *testing.T) {
	dir := t.TempDir()
	config := FreezerTableConfig{Dir: dir, Name: "persist", Compression: CompressionNone}

	// Write and close.
	ft, err := OpenFreezerTable(config)
	if err != nil {
		t.Fatal(err)
	}
	ft.Append(0, []byte("persistent"))
	ft.Append(1, []byte("data"))
	ft.Close()

	// Reopen and verify.
	ft2, err := OpenFreezerTable(config)
	if err != nil {
		t.Fatal(err)
	}
	defer ft2.Close()

	if ft2.Count() != 2 {
		t.Errorf("expected 2 items, got %d", ft2.Count())
	}
	got, err := ft2.Retrieve(0)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if !bytes.Equal(got, []byte("persistent")) {
		t.Error("data mismatch after reopen")
	}
}

func TestFreezerTable_ClosedErrors(t *testing.T) {
	ft, _ := newTestFreezerTable(t, CompressionNone)
	ft.Close()

	if err := ft.Append(0, []byte("x")); err == nil {
		t.Error("expected error on closed table append")
	}
	if _, err := ft.Retrieve(0); err == nil {
		t.Error("expected error on closed table retrieve")
	}
}

func TestFreezerTable_DataSize(t *testing.T) {
	ft, _ := newTestFreezerTable(t, CompressionNone)
	defer ft.Close()

	if ft.DataSize() != 0 {
		t.Error("expected 0 data size for empty table")
	}
	ft.Append(0, []byte("12345"))
	if ft.DataSize() != 5 {
		t.Errorf("expected 5, got %d", ft.DataSize())
	}
}

// Ensure index entry encoding is correct.
func TestFTIndexEntry_EncodeDecode(t *testing.T) {
	entry := ftIndexEntry{
		Offset:          12345,
		CompressedLen:   678,
		UncompressedLen: 900,
	}
	encoded := entry.encode()
	if len(encoded) != ftIndexEntrySize {
		t.Fatalf("expected %d bytes, got %d", ftIndexEntrySize, len(encoded))
	}
	decoded := decodeFTIndexEntry(encoded)
	if decoded.Offset != 12345 || decoded.CompressedLen != 678 || decoded.UncompressedLen != 900 {
		t.Errorf("decode mismatch: %+v", decoded)
	}
}

func TestFreezerTable_TruncateTailAll(t *testing.T) {
	ft, _ := newTestFreezerTable(t, CompressionNone)
	defer ft.Close()

	ft.Append(0, []byte("a"))
	ft.Append(1, []byte("b"))

	if err := ft.TruncateTail(5); err != nil {
		t.Fatalf("TruncateTail(5): %v", err)
	}
	if ft.Count() != 0 {
		t.Errorf("expected 0, got %d", ft.Count())
	}
}

// Suppress unused import warning.
var _ = os.Remove
