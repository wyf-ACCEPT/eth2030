package rawdb

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func tempFreezerDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "freezer")
	return dir
}

func TestNewFreezer_CreatesDir(t *testing.T) {
	dir := tempFreezerDir(t)
	f, err := NewFreezer(dir, false)
	if err != nil {
		t.Fatalf("NewFreezer: %v", err)
	}
	defer f.Close()

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatal("freezer directory should exist")
	}
}

func TestNewFreezer_HasAllTables(t *testing.T) {
	f, err := NewFreezer(tempFreezerDir(t), false)
	if err != nil {
		t.Fatalf("NewFreezer: %v", err)
	}
	defer f.Close()

	tables := []string{FreezerHeaderTable, FreezerBodyTable, FreezerReceiptTable, FreezerHashTable}
	for _, name := range tables {
		if !f.HasTable(name) {
			t.Errorf("missing table %q", name)
		}
	}
}

func TestFreeze_SingleItem(t *testing.T) {
	f, err := NewFreezer(tempFreezerDir(t), false)
	if err != nil {
		t.Fatalf("NewFreezer: %v", err)
	}
	defer f.Close()

	data := []byte("block header 0")
	if err := f.Freeze(FreezerHeaderTable, 0, data); err != nil {
		t.Fatalf("Freeze: %v", err)
	}

	got, err := f.Retrieve(FreezerHeaderTable, 0)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("data mismatch: got %q, want %q", got, data)
	}
}

func TestFreeze_Sequential(t *testing.T) {
	f, err := NewFreezer(tempFreezerDir(t), false)
	if err != nil {
		t.Fatalf("NewFreezer: %v", err)
	}
	defer f.Close()

	for i := uint64(0); i < 10; i++ {
		data := fmt.Appendf(nil, "header-%d", i)
		if err := f.Freeze(FreezerHeaderTable, i, data); err != nil {
			t.Fatalf("Freeze(%d): %v", i, err)
		}
	}

	for i := uint64(0); i < 10; i++ {
		got, err := f.Retrieve(FreezerHeaderTable, i)
		if err != nil {
			t.Fatalf("Retrieve(%d): %v", i, err)
		}
		want := fmt.Appendf(nil, "header-%d", i)
		if !bytes.Equal(got, want) {
			t.Errorf("item %d: got %q, want %q", i, got, want)
		}
	}
}

func TestFreeze_NonSequentialFails(t *testing.T) {
	f, err := NewFreezer(tempFreezerDir(t), false)
	if err != nil {
		t.Fatalf("NewFreezer: %v", err)
	}
	defer f.Close()

	// Skip item 0 -- should fail.
	err = f.Freeze(FreezerHeaderTable, 5, []byte("data"))
	if err == nil {
		t.Error("expected error for non-sequential append")
	}
}

func TestFreezeRange_MultipleItems(t *testing.T) {
	f, err := NewFreezer(tempFreezerDir(t), false)
	if err != nil {
		t.Fatalf("NewFreezer: %v", err)
	}
	defer f.Close()

	items := [][]byte{
		[]byte("block-0"),
		[]byte("block-1"),
		[]byte("block-2"),
	}
	if err := f.FreezeRange(FreezerHeaderTable, 0, items); err != nil {
		t.Fatalf("FreezeRange: %v", err)
	}

	for i, want := range items {
		got, err := f.Retrieve(FreezerHeaderTable, uint64(i))
		if err != nil {
			t.Fatalf("Retrieve(%d): %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("item %d: got %q, want %q", i, got, want)
		}
	}
}

func TestFreezeRange_NonSequentialStart(t *testing.T) {
	f, err := NewFreezer(tempFreezerDir(t), false)
	if err != nil {
		t.Fatalf("NewFreezer: %v", err)
	}
	defer f.Close()

	err = f.FreezeRange(FreezerHeaderTable, 3, [][]byte{[]byte("data")})
	if err == nil {
		t.Error("expected error for non-sequential start")
	}
}

func TestRetrieve_OutOfBounds(t *testing.T) {
	f, err := NewFreezer(tempFreezerDir(t), false)
	if err != nil {
		t.Fatalf("NewFreezer: %v", err)
	}
	defer f.Close()

	f.Freeze(FreezerHeaderTable, 0, []byte("data"))

	_, err = f.Retrieve(FreezerHeaderTable, 5)
	if err == nil {
		t.Error("expected out-of-bounds error")
	}
}

func TestRetrieve_EmptyTable(t *testing.T) {
	f, err := NewFreezer(tempFreezerDir(t), false)
	if err != nil {
		t.Fatalf("NewFreezer: %v", err)
	}
	defer f.Close()

	_, err = f.Retrieve(FreezerHeaderTable, 0)
	if err == nil {
		t.Error("expected error for empty table")
	}
}

func TestRetrieve_UnknownTable(t *testing.T) {
	f, err := NewFreezer(tempFreezerDir(t), false)
	if err != nil {
		t.Fatalf("NewFreezer: %v", err)
	}
	defer f.Close()

	_, err = f.Retrieve("nonexistent", 0)
	if err == nil {
		t.Error("expected error for unknown table")
	}
}

func TestTruncateHead(t *testing.T) {
	f, err := NewFreezer(tempFreezerDir(t), false)
	if err != nil {
		t.Fatalf("NewFreezer: %v", err)
	}
	defer f.Close()

	for i := uint64(0); i < 5; i++ {
		f.Freeze(FreezerHeaderTable, i, fmt.Appendf(nil, "header-%d", i))
	}

	// Keep only items 0-2 (truncate at 3).
	if err := f.TruncateHead(3); err != nil {
		t.Fatalf("TruncateHead: %v", err)
	}

	// Items 0-2 should still be accessible.
	for i := uint64(0); i < 3; i++ {
		got, err := f.Retrieve(FreezerHeaderTable, i)
		if err != nil {
			t.Fatalf("Retrieve(%d) after truncate: %v", i, err)
		}
		want := fmt.Appendf(nil, "header-%d", i)
		if !bytes.Equal(got, want) {
			t.Errorf("item %d: got %q, want %q", i, got, want)
		}
	}

	// Items 3-4 should be gone.
	_, err = f.Retrieve(FreezerHeaderTable, 3)
	if err == nil {
		t.Error("expected error for truncated item")
	}
}

func TestTruncateHead_NoOp(t *testing.T) {
	f, err := NewFreezer(tempFreezerDir(t), false)
	if err != nil {
		t.Fatalf("NewFreezer: %v", err)
	}
	defer f.Close()

	f.Freeze(FreezerHeaderTable, 0, []byte("data"))

	// Truncating at or beyond current count is a no-op.
	if err := f.TruncateHead(10); err != nil {
		t.Fatalf("TruncateHead: %v", err)
	}
	got, _ := f.Retrieve(FreezerHeaderTable, 0)
	if !bytes.Equal(got, []byte("data")) {
		t.Error("data should still be accessible")
	}
}

func TestTruncateTail(t *testing.T) {
	f, err := NewFreezer(tempFreezerDir(t), false)
	if err != nil {
		t.Fatalf("NewFreezer: %v", err)
	}
	defer f.Close()

	for i := uint64(0); i < 5; i++ {
		f.Freeze(FreezerHeaderTable, i, fmt.Appendf(nil, "header-%d", i))
	}

	// Remove items 0-1 (keep 2-4).
	if err := f.TruncateTail(2); err != nil {
		t.Fatalf("TruncateTail: %v", err)
	}

	if f.Tail() != 2 {
		t.Errorf("tail = %d, want 2", f.Tail())
	}

	// Items 0-1 should be inaccessible.
	_, err = f.Retrieve(FreezerHeaderTable, 0)
	if err == nil {
		t.Error("expected error for pruned item 0")
	}
	_, err = f.Retrieve(FreezerHeaderTable, 1)
	if err == nil {
		t.Error("expected error for pruned item 1")
	}

	// Items 2-4 should still be accessible.
	for i := uint64(2); i < 5; i++ {
		got, err := f.Retrieve(FreezerHeaderTable, i)
		if err != nil {
			t.Fatalf("Retrieve(%d): %v", i, err)
		}
		want := fmt.Appendf(nil, "header-%d", i)
		if !bytes.Equal(got, want) {
			t.Errorf("item %d: got %q, want %q", i, got, want)
		}
	}
}

func TestTruncateTail_NoOp(t *testing.T) {
	f, err := NewFreezer(tempFreezerDir(t), false)
	if err != nil {
		t.Fatalf("NewFreezer: %v", err)
	}
	defer f.Close()

	f.Freeze(FreezerHeaderTable, 0, []byte("data"))

	if err := f.TruncateTail(0); err != nil {
		t.Fatalf("TruncateTail: %v", err)
	}
	got, _ := f.Retrieve(FreezerHeaderTable, 0)
	if !bytes.Equal(got, []byte("data")) {
		t.Error("data should still be accessible")
	}
}

func TestReadOnly_RejectWrites(t *testing.T) {
	dir := tempFreezerDir(t)

	// Create writable freezer first.
	fw, err := NewFreezer(dir, false)
	if err != nil {
		t.Fatalf("NewFreezer writable: %v", err)
	}
	fw.Freeze(FreezerHeaderTable, 0, []byte("data"))
	fw.Close()

	// Reopen as read-only.
	fr, err := NewFreezer(dir, true)
	if err != nil {
		t.Fatalf("NewFreezer read-only: %v", err)
	}
	defer fr.Close()

	err = fr.Freeze(FreezerHeaderTable, 1, []byte("new"))
	if err != ErrFreezerReadOnly {
		t.Errorf("expected ErrFreezerReadOnly, got %v", err)
	}

	err = fr.TruncateHead(0)
	if err != ErrFreezerReadOnly {
		t.Errorf("TruncateHead: expected ErrFreezerReadOnly, got %v", err)
	}

	err = fr.TruncateTail(1)
	if err != ErrFreezerReadOnly {
		t.Errorf("TruncateTail: expected ErrFreezerReadOnly, got %v", err)
	}
}

func TestClose_RejectsOps(t *testing.T) {
	f, err := NewFreezer(tempFreezerDir(t), false)
	if err != nil {
		t.Fatalf("NewFreezer: %v", err)
	}
	f.Close()

	if err := f.Freeze(FreezerHeaderTable, 0, []byte("data")); err != ErrFreezerClosed {
		t.Errorf("Freeze after close: expected ErrFreezerClosed, got %v", err)
	}
	if _, err := f.Retrieve(FreezerHeaderTable, 0); err != ErrFreezerClosed {
		t.Errorf("Retrieve after close: expected ErrFreezerClosed, got %v", err)
	}
}

func TestClose_Idempotent(t *testing.T) {
	f, err := NewFreezer(tempFreezerDir(t), false)
	if err != nil {
		t.Fatalf("NewFreezer: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestSync(t *testing.T) {
	f, err := NewFreezer(tempFreezerDir(t), false)
	if err != nil {
		t.Fatalf("NewFreezer: %v", err)
	}
	defer f.Close()

	f.Freeze(FreezerHeaderTable, 0, []byte("header"))
	if err := f.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
}

func TestTableSize(t *testing.T) {
	f, err := NewFreezer(tempFreezerDir(t), false)
	if err != nil {
		t.Fatalf("NewFreezer: %v", err)
	}
	defer f.Close()

	data := []byte("twelve bytes") // 12 bytes
	f.Freeze(FreezerHeaderTable, 0, data)

	size, err := f.TableSize(FreezerHeaderTable)
	if err != nil {
		t.Fatalf("TableSize: %v", err)
	}
	// Expected: 12 (data) + 1 * 12 (one index entry) = 24.
	if size != 24 {
		t.Errorf("size = %d, want 24", size)
	}
}

func TestTableSize_UnknownTable(t *testing.T) {
	f, err := NewFreezer(tempFreezerDir(t), false)
	if err != nil {
		t.Fatalf("NewFreezer: %v", err)
	}
	defer f.Close()

	_, err = f.TableSize("no_such_table")
	if err == nil {
		t.Error("expected error for unknown table")
	}
}

func TestItemCount(t *testing.T) {
	f, err := NewFreezer(tempFreezerDir(t), false)
	if err != nil {
		t.Fatalf("NewFreezer: %v", err)
	}
	defer f.Close()

	count, _ := f.ItemCount(FreezerHeaderTable)
	if count != 0 {
		t.Errorf("initial count = %d, want 0", count)
	}

	for i := uint64(0); i < 5; i++ {
		f.Freeze(FreezerHeaderTable, i, []byte("x"))
	}
	count, _ = f.ItemCount(FreezerHeaderTable)
	if count != 5 {
		t.Errorf("count = %d, want 5", count)
	}
}

func TestFrozenCount(t *testing.T) {
	f, err := NewFreezer(tempFreezerDir(t), false)
	if err != nil {
		t.Fatalf("NewFreezer: %v", err)
	}
	defer f.Close()

	if f.Frozen() != 0 {
		t.Errorf("initial frozen = %d, want 0", f.Frozen())
	}

	items := [][]byte{[]byte("a"), []byte("b"), []byte("c")}
	f.FreezeRange(FreezerHeaderTable, 0, items)

	if f.Frozen() != 3 {
		t.Errorf("frozen = %d, want 3", f.Frozen())
	}
}

func TestReopen_Persistence(t *testing.T) {
	dir := tempFreezerDir(t)

	// Write data.
	f1, err := NewFreezer(dir, false)
	if err != nil {
		t.Fatalf("NewFreezer: %v", err)
	}
	f1.Freeze(FreezerHeaderTable, 0, []byte("persistent"))
	f1.Sync()
	f1.Close()

	// Reopen and verify.
	f2, err := NewFreezer(dir, false)
	if err != nil {
		t.Fatalf("NewFreezer reopen: %v", err)
	}
	defer f2.Close()

	got, err := f2.Retrieve(FreezerHeaderTable, 0)
	if err != nil {
		t.Fatalf("Retrieve after reopen: %v", err)
	}
	if !bytes.Equal(got, []byte("persistent")) {
		t.Errorf("data after reopen: got %q, want %q", got, "persistent")
	}
}

func TestLargeItems(t *testing.T) {
	f, err := NewFreezer(tempFreezerDir(t), false)
	if err != nil {
		t.Fatalf("NewFreezer: %v", err)
	}
	defer f.Close()

	// Write a 1MB item.
	data := make([]byte, 1<<20)
	for i := range data {
		data[i] = byte(i % 256)
	}
	if err := f.Freeze(FreezerHeaderTable, 0, data); err != nil {
		t.Fatalf("Freeze large: %v", err)
	}

	got, err := f.Retrieve(FreezerHeaderTable, 0)
	if err != nil {
		t.Fatalf("Retrieve large: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Error("large data mismatch")
	}
}

func TestEmptyItem(t *testing.T) {
	f, err := NewFreezer(tempFreezerDir(t), false)
	if err != nil {
		t.Fatalf("NewFreezer: %v", err)
	}
	defer f.Close()

	if err := f.Freeze(FreezerHeaderTable, 0, []byte{}); err != nil {
		t.Fatalf("Freeze empty: %v", err)
	}

	got, err := f.Retrieve(FreezerHeaderTable, 0)
	if err != nil {
		t.Fatalf("Retrieve empty: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty data, got %d bytes", len(got))
	}
}

func TestMultipleTables(t *testing.T) {
	f, err := NewFreezer(tempFreezerDir(t), false)
	if err != nil {
		t.Fatalf("NewFreezer: %v", err)
	}
	defer f.Close()

	f.Freeze(FreezerHeaderTable, 0, []byte("header-0"))
	f.Freeze(FreezerBodyTable, 0, []byte("body-0"))
	f.Freeze(FreezerReceiptTable, 0, []byte("receipt-0"))
	f.Freeze(FreezerHashTable, 0, []byte("hash-0"))

	tests := []struct {
		table string
		want  string
	}{
		{FreezerHeaderTable, "header-0"},
		{FreezerBodyTable, "body-0"},
		{FreezerReceiptTable, "receipt-0"},
		{FreezerHashTable, "hash-0"},
	}
	for _, tt := range tests {
		got, err := f.Retrieve(tt.table, 0)
		if err != nil {
			t.Fatalf("Retrieve(%s): %v", tt.table, err)
		}
		if string(got) != tt.want {
			t.Errorf("%s: got %q, want %q", tt.table, got, tt.want)
		}
	}
}

func TestTruncateHead_ThenAppend(t *testing.T) {
	f, err := NewFreezer(tempFreezerDir(t), false)
	if err != nil {
		t.Fatalf("NewFreezer: %v", err)
	}
	defer f.Close()

	for i := uint64(0); i < 5; i++ {
		f.Freeze(FreezerHeaderTable, i, fmt.Appendf(nil, "h-%d", i))
	}

	// Truncate to 3.
	f.TruncateHead(3)

	// Should be able to append at item 3 again.
	if err := f.Freeze(FreezerHeaderTable, 3, []byte("h-3-new")); err != nil {
		t.Fatalf("Freeze after truncate: %v", err)
	}
	got, _ := f.Retrieve(FreezerHeaderTable, 3)
	if string(got) != "h-3-new" {
		t.Errorf("got %q, want %q", got, "h-3-new")
	}
}

func TestTruncateTail_Everything(t *testing.T) {
	f, err := NewFreezer(tempFreezerDir(t), false)
	if err != nil {
		t.Fatalf("NewFreezer: %v", err)
	}
	defer f.Close()

	for i := uint64(0); i < 3; i++ {
		f.Freeze(FreezerHeaderTable, i, []byte("x"))
	}

	// Truncate everything.
	if err := f.TruncateTail(3); err != nil {
		t.Fatalf("TruncateTail: %v", err)
	}

	count, _ := f.ItemCount(FreezerHeaderTable)
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}
