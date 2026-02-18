package trie

import (
	"fmt"
	"math/rand"
	"testing"
)

// TestRandomOperations performs 1000 random Put/Get/Delete ops, then verifies
// all remaining entries and confirms the root hash is deterministic.
func TestRandomOperations(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	tr := New()
	reference := make(map[string]string)

	for i := 0; i < 1000; i++ {
		keyLen := rng.Intn(20) + 1
		key := make([]byte, keyLen)
		rng.Read(key)

		switch rng.Intn(3) {
		case 0: // Put
			valLen := rng.Intn(50) + 1
			val := make([]byte, valLen)
			rng.Read(val)
			tr.Put(key, val)
			reference[string(key)] = string(val)

		case 1: // Delete
			tr.Delete(key)
			delete(reference, string(key))

		case 2: // Get
			got, err := tr.Get(key)
			want, exists := reference[string(key)]
			if exists {
				if err != nil {
					t.Fatalf("step %d: Get(%x) error: %v", i, key, err)
				}
				if string(got) != want {
					t.Fatalf("step %d: Get(%x) mismatch", i, key)
				}
			} else {
				if err != ErrNotFound {
					t.Fatalf("step %d: Get(%x) should not exist", i, key)
				}
			}
		}
	}

	// Verify all reference entries remain correct.
	for k, v := range reference {
		got, err := tr.Get([]byte(k))
		if err != nil {
			t.Fatalf("final Get(%x) error: %v", k, err)
		}
		if string(got) != v {
			t.Fatalf("final Get(%x) mismatch", k)
		}
	}

	// Rebuild from scratch and compare root hash.
	tr2 := New()
	for k, v := range reference {
		tr2.Put([]byte(k), []byte(v))
	}
	if tr.Hash() != tr2.Hash() {
		t.Fatal("random trie and rebuilt trie have different root hashes")
	}
}

// TestManyKeys inserts 200 keys, verifies, deletes half, verifies remaining.
func TestManyKeys(t *testing.T) {
	tr := New()
	entries := make(map[string]string)
	for i := 0; i < 200; i++ {
		key := fmt.Sprintf("key-%04d", i)
		val := fmt.Sprintf("value-%04d", i)
		tr.Put([]byte(key), []byte(val))
		entries[key] = val
	}

	if tr.Hash() == emptyRoot {
		t.Fatal("root should not be empty")
	}

	for k, v := range entries {
		got, err := tr.Get([]byte(k))
		if err != nil {
			t.Fatalf("Get(%q) error: %v", k, err)
		}
		if string(got) != v {
			t.Fatalf("Get(%q) = %q, want %q", k, got, v)
		}
	}

	for i := 0; i < 100; i++ {
		tr.Delete([]byte(fmt.Sprintf("key-%04d", i)))
	}

	for i := 100; i < 200; i++ {
		key := fmt.Sprintf("key-%04d", i)
		val := fmt.Sprintf("value-%04d", i)
		got, err := tr.Get([]byte(key))
		if err != nil {
			t.Fatalf("Get(%q) after partial delete: %v", key, err)
		}
		if string(got) != val {
			t.Fatalf("Get(%q) = %q, want %q", key, got, val)
		}
	}

	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key-%04d", i)
		if _, err := tr.Get([]byte(key)); err != ErrNotFound {
			t.Fatalf("Get(%q) after delete: err = %v, want ErrNotFound", key, err)
		}
	}
}

// TestRandomPutDeleteRootConsistency verifies the root hash is independent
// of the intermediate mutation order as long as the final state is the same.
func TestRandomPutDeleteRootConsistency(t *testing.T) {
	rng := rand.New(rand.NewSource(99))

	// Build a set of final entries.
	final := make(map[string]string)
	for i := 0; i < 50; i++ {
		key := make([]byte, rng.Intn(10)+1)
		rng.Read(key)
		val := make([]byte, rng.Intn(20)+1)
		rng.Read(val)
		final[string(key)] = string(val)
	}

	// Build trie1: insert all, then delete some, then re-insert.
	tr1 := New()
	for k, v := range final {
		tr1.Put([]byte(k), []byte(v))
	}
	// Add extra keys then delete them.
	for i := 0; i < 20; i++ {
		key := make([]byte, 5)
		rng.Read(key)
		tr1.Put(key, []byte("temp"))
		tr1.Delete(key)
	}
	h1 := tr1.Hash()

	// Build trie2: just insert final state.
	tr2 := New()
	for k, v := range final {
		tr2.Put([]byte(k), []byte(v))
	}
	h2 := tr2.Hash()

	if h1 != h2 {
		t.Fatalf("root hashes differ: %s vs %s", h1.Hex(), h2.Hex())
	}
}
