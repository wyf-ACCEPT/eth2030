package txpool

import (
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// makeJournalTx creates a signed legacy transaction for journal testing.
func makeJournalTx(nonce uint64, gasPrice int64) *types.Transaction {
	to := types.Address{0x01}
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(gasPrice),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(1000),
	})
	// Set a fake sender for deterministic testing.
	addr := types.Address{0xAA, 0xBB, 0xCC}
	tx.SetSender(addr)
	return tx
}

func TestNewTxJournal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "journal.jsonl")

	j, err := NewTxJournal(path)
	if err != nil {
		t.Fatalf("NewTxJournal error: %v", err)
	}
	defer j.Close()

	if j.Path() != path {
		t.Errorf("Path() = %q, want %q", j.Path(), path)
	}
	if !j.Exists() {
		t.Error("journal file should exist after creation")
	}
}

func TestJournalInsertAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.jsonl")

	j, err := NewTxJournal(path)
	if err != nil {
		t.Fatalf("NewTxJournal error: %v", err)
	}

	tx1 := makeJournalTx(0, 1_000_000_000)
	tx2 := makeJournalTx(1, 2_000_000_000)

	if err := j.Insert(tx1, true); err != nil {
		t.Fatalf("Insert tx1: %v", err)
	}
	if err := j.Insert(tx2, false); err != nil {
		t.Fatalf("Insert tx2: %v", err)
	}

	if j.Count() != 2 {
		t.Errorf("Count() = %d, want 2", j.Count())
	}

	j.Close()

	// Load from disk.
	txs, entries, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if len(txs) != 2 {
		t.Fatalf("loaded %d txs, want 2", len(txs))
	}
	if len(entries) != 2 {
		t.Fatalf("loaded %d entries, want 2", len(entries))
	}

	// Verify first transaction.
	if txs[0].Nonce() != 0 {
		t.Errorf("tx[0] nonce = %d, want 0", txs[0].Nonce())
	}
	if !entries[0].Local {
		t.Error("entry[0] should be local")
	}

	// Verify second transaction.
	if txs[1].Nonce() != 1 {
		t.Errorf("tx[1] nonce = %d, want 1", txs[1].Nonce())
	}
	if entries[1].Local {
		t.Error("entry[1] should not be local")
	}
}

func TestJournalLoadNonexistent(t *testing.T) {
	_, _, err := Load("/nonexistent/path/journal.jsonl")
	if err != ErrJournalNotFound {
		t.Errorf("expected ErrJournalNotFound, got %v", err)
	}
}

func TestJournalLoadEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	os.WriteFile(path, []byte{}, 0644)

	txs, entries, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if len(txs) != 0 {
		t.Errorf("expected 0 txs, got %d", len(txs))
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestJournalLoadCorruptEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.jsonl")

	// Write a mix of valid and corrupt entries.
	j, _ := NewTxJournal(path)
	tx := makeJournalTx(0, 1_000_000_000)
	j.Insert(tx, true)
	j.Close()

	// Append corrupt data.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	f.WriteString("this is not json\n")
	f.WriteString("{\"tx_rlp\":\"invalid\"}\n")
	f.Close()

	txs, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	// Only the valid entry should be returned.
	if len(txs) != 1 {
		t.Errorf("expected 1 valid tx, got %d", len(txs))
	}
}

func TestJournalInsertAfterClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.jsonl")

	j, _ := NewTxJournal(path)
	j.Close()

	tx := makeJournalTx(0, 1_000_000_000)
	err := j.Insert(tx, true)
	if err != ErrJournalClosed {
		t.Errorf("expected ErrJournalClosed, got %v", err)
	}
}

func TestJournalDoubleClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.jsonl")

	j, _ := NewTxJournal(path)
	if err := j.Close(); err != nil {
		t.Fatalf("first close error: %v", err)
	}
	// Second close should not panic or error.
	if err := j.Close(); err != nil {
		t.Fatalf("second close error: %v", err)
	}
}

func TestJournalRotate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.jsonl")

	j, _ := NewTxJournal(path)

	// Insert 5 transactions.
	for i := uint64(0); i < 5; i++ {
		j.Insert(makeJournalTx(i, int64(1_000_000_000+i)), true)
	}
	if j.Count() != 5 {
		t.Errorf("Count() = %d, want 5", j.Count())
	}

	// Rotate with only 2 pending transactions (nonces 3 and 4).
	addr := types.Address{0xAA, 0xBB, 0xCC}
	pending := map[types.Address][]*types.Transaction{
		addr: {makeJournalTx(3, 1_000_000_003), makeJournalTx(4, 1_000_000_004)},
	}

	if err := j.Rotate(pending); err != nil {
		t.Fatalf("Rotate error: %v", err)
	}

	// Count should reflect only the 2 remaining entries.
	if j.Count() != 2 {
		t.Errorf("Count() after rotate = %d, want 2", j.Count())
	}

	j.Close()

	// Reload and verify only 2 transactions remain.
	txs, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load after rotate error: %v", err)
	}
	if len(txs) != 2 {
		t.Fatalf("expected 2 txs after rotate, got %d", len(txs))
	}
}

func TestJournalRotateEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.jsonl")

	j, _ := NewTxJournal(path)
	j.Insert(makeJournalTx(0, 1_000_000_000), true)

	// Rotate with empty pending set.
	if err := j.Rotate(nil); err != nil {
		t.Fatalf("Rotate error: %v", err)
	}

	j.Close()

	txs, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if len(txs) != 0 {
		t.Errorf("expected 0 txs after rotate with nil pending, got %d", len(txs))
	}
}

func TestJournalRotateAfterClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.jsonl")

	j, _ := NewTxJournal(path)
	j.Close()

	err := j.Rotate(nil)
	if err != ErrJournalClosed {
		t.Errorf("expected ErrJournalClosed, got %v", err)
	}
}

func TestJournalInsertBatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.jsonl")

	j, _ := NewTxJournal(path)
	defer j.Close()

	txs := []*types.Transaction{
		makeJournalTx(0, 1_000_000_000),
		makeJournalTx(1, 2_000_000_000),
		makeJournalTx(2, 3_000_000_000),
	}

	if err := j.InsertBatch(txs, true); err != nil {
		t.Fatalf("InsertBatch error: %v", err)
	}

	if j.Count() != 3 {
		t.Errorf("Count() = %d, want 3", j.Count())
	}
}

func TestJournalSenderRecovery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.jsonl")

	j, _ := NewTxJournal(path)

	tx := makeJournalTx(0, 1_000_000_000)
	sender := types.Address{0xDE, 0xAD, 0xBE, 0xEF}
	tx.SetSender(sender)

	j.Insert(tx, true)
	j.Close()

	txs, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if len(txs) != 1 {
		t.Fatalf("expected 1 tx, got %d", len(txs))
	}

	recovered := txs[0].Sender()
	if recovered == nil {
		t.Fatal("sender should be restored from journal")
	}
	if *recovered != sender {
		t.Errorf("sender = %x, want %x", *recovered, sender)
	}
}

func TestJournalIsClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.jsonl")

	j, _ := NewTxJournal(path)

	if j.IsClosed() {
		t.Error("journal should not be closed initially")
	}

	j.Close()

	if !j.IsClosed() {
		t.Error("journal should be closed after Close()")
	}
}

func TestJournalRotatePreservesAppend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.jsonl")

	j, _ := NewTxJournal(path)

	addr := types.Address{0xAA, 0xBB, 0xCC}
	pending := map[types.Address][]*types.Transaction{
		addr: {makeJournalTx(0, 1_000_000_000)},
	}
	j.Rotate(pending)

	// After rotation, we should still be able to insert.
	tx := makeJournalTx(1, 2_000_000_000)
	if err := j.Insert(tx, true); err != nil {
		t.Fatalf("Insert after Rotate: %v", err)
	}
	j.Close()

	txs, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	// 1 from rotate + 1 new insert.
	if len(txs) != 2 {
		t.Errorf("expected 2 txs, got %d", len(txs))
	}
}

func TestJournalExistsCheck(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.jsonl")

	j, _ := NewTxJournal(path)
	if !j.Exists() {
		t.Error("Exists() should return true for new journal")
	}
	j.Close()

	os.Remove(path)

	if j.Exists() {
		t.Error("Exists() should return false after file removal")
	}
}

func TestJournalConcurrentInsert(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.jsonl")

	j, _ := NewTxJournal(path)
	defer j.Close()

	// Insert concurrently from multiple goroutines.
	done := make(chan struct{})
	for g := 0; g < 4; g++ {
		go func(offset uint64) {
			for i := uint64(0); i < 10; i++ {
				j.Insert(makeJournalTx(offset+i, int64(1_000_000_000+offset+i)), true)
			}
			done <- struct{}{}
		}(uint64(g) * 100)
	}
	for i := 0; i < 4; i++ {
		<-done
	}

	if j.Count() != 40 {
		t.Errorf("Count() = %d, want 40", j.Count())
	}
}

func TestJournalHashPreserved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.jsonl")

	j, _ := NewTxJournal(path)
	tx := makeJournalTx(42, 5_000_000_000)
	origHash := tx.Hash()
	j.Insert(tx, true)
	j.Close()

	_, entries, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Hash != origHash {
		t.Errorf("hash mismatch: got %x, want %x", entries[0].Hash, origHash)
	}
}
