package trie

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestOverlayGet(t *testing.T) {
	ot := NewOverlayTrie(DefaultOverlayConfig())

	// Put data in the old trie.
	key := []byte("account1")
	val := []byte("balance1000")
	ot.OldTrie().Put(key, val)

	// Get should fall back to old trie.
	got, err := ot.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != string(val) {
		t.Errorf("Get = %q, want %q", got, val)
	}
}

func TestOverlayPut(t *testing.T) {
	ot := NewOverlayTrie(DefaultOverlayConfig())

	key := []byte("account1")
	val := []byte("balance2000")

	err := ot.Put(key, val)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := ot.Get(key)
	if err != nil {
		t.Fatalf("Get after Put: %v", err)
	}
	if string(got) != string(val) {
		t.Errorf("Get = %q, want %q", got, val)
	}
}

func TestOverlayGetFromNew(t *testing.T) {
	ot := NewOverlayTrie(DefaultOverlayConfig())

	key := []byte("account1")
	oldVal := []byte("oldbalance")
	newVal := []byte("newbalance")

	// Put in both old and new trie.
	ot.OldTrie().Put(key, oldVal)
	ot.Put(key, newVal)

	// New trie should take priority.
	got, err := ot.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != string(newVal) {
		t.Errorf("Get = %q, want %q (new trie should take priority)", got, newVal)
	}
}

func TestOverlayDelete(t *testing.T) {
	ot := NewOverlayTrie(DefaultOverlayConfig())

	key := []byte("account1")
	val := []byte("balance1000")

	// Put in old trie.
	ot.OldTrie().Put(key, val)

	// Delete from overlay.
	err := ot.Delete(key)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Get should fail even though the key is in the old trie.
	_, err = ot.Get(key)
	if err == nil {
		t.Error("Get should fail after Delete")
	}
}

func TestOverlayHas(t *testing.T) {
	ot := NewOverlayTrie(DefaultOverlayConfig())

	key1 := []byte("existing")
	key2 := []byte("missing")

	ot.OldTrie().Put(key1, []byte("value"))

	has, err := ot.Has(key1)
	if err != nil {
		t.Fatalf("Has: %v", err)
	}
	if !has {
		t.Error("Has should return true for existing key")
	}

	has, err = ot.Has(key2)
	if err != nil {
		t.Fatalf("Has: %v", err)
	}
	if has {
		t.Error("Has should return false for missing key")
	}

	// Delete key1 and check again.
	ot.Delete(key1)
	has, err = ot.Has(key1)
	if err != nil {
		t.Fatalf("Has after delete: %v", err)
	}
	if has {
		t.Error("Has should return false after delete")
	}
}

func TestOverlayHash(t *testing.T) {
	ot := NewOverlayTrie(DefaultOverlayConfig())

	// Empty new trie should produce a consistent hash.
	h1 := ot.Hash()
	h2 := ot.Hash()
	if h1 != h2 {
		t.Error("Hash should be deterministic")
	}

	// Adding data should change the hash.
	ot.Put([]byte("key1"), []byte("val1"))
	h3 := ot.Hash()
	if h3 == h1 {
		t.Error("Hash should change after Put")
	}
}

func TestOverlayMigratedKeys(t *testing.T) {
	ot := NewOverlayTrie(DefaultOverlayConfig())

	if ot.MigratedKeys() != 0 {
		t.Errorf("initial MigratedKeys = %d, want 0", ot.MigratedKeys())
	}

	ot.Put([]byte("key1"), []byte("val1"))
	ot.Put([]byte("key2"), []byte("val2"))
	ot.Put([]byte("key3"), []byte("val3"))

	if ot.MigratedKeys() != 3 {
		t.Errorf("MigratedKeys = %d, want 3", ot.MigratedKeys())
	}
}

func TestOverlayCommit(t *testing.T) {
	ot := NewOverlayTrie(DefaultOverlayConfig())

	ot.Put([]byte("key1"), []byte("val1"))
	ot.Put([]byte("key2"), []byte("val2"))

	hash, err := ot.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if hash == (types.Hash{}) {
		t.Error("Commit should return non-zero hash for non-empty trie")
	}

	// Commit hash should match Hash().
	h := ot.Hash()
	if hash != h {
		t.Errorf("Commit hash %x != Hash() %x", hash, h)
	}
}
