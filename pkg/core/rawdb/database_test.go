package rawdb

import (
	"testing"
)

// TestDatabaseInterface verifies that MemoryDB satisfies the Database interface.
func TestDatabaseInterface(t *testing.T) {
	var _ Database = (*MemoryDB)(nil)
}

// TestKeyValueStoreInterface verifies MemoryDB satisfies KeyValueStore.
func TestKeyValueStoreInterface(t *testing.T) {
	var _ KeyValueStore = (*MemoryDB)(nil)
}

// TestKeyValueIteratorInterface verifies MemoryDB satisfies KeyValueIterator.
func TestKeyValueIteratorInterface(t *testing.T) {
	var _ KeyValueIterator = (*MemoryDB)(nil)
}

// TestBatcherInterface verifies MemoryDB satisfies Batcher.
func TestBatcherInterface(t *testing.T) {
	var _ Batcher = (*MemoryDB)(nil)
}

// TestErrNotFound checks that ErrNotFound is properly defined.
func TestErrNotFound(t *testing.T) {
	if ErrNotFound == nil {
		t.Fatal("ErrNotFound should not be nil")
	}
	if ErrNotFound.Error() != "not found" {
		t.Fatalf("expected 'not found', got %q", ErrNotFound.Error())
	}
}
