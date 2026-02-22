package trie

import (
	"bytes"
	"testing"
)

// FuzzTrieInsertGet inserts random key-value pairs into a trie and verifies
// that the values can be retrieved correctly. Must not panic.
func FuzzTrieInsertGet(f *testing.F) {
	// Seed corpus: short key-value pairs.
	f.Add([]byte{0x01}, []byte{0x0a})
	f.Add([]byte{0x01, 0x02}, []byte{0x0b, 0x0c})
	f.Add([]byte{0x01, 0x02, 0x03}, []byte{0x0d})
	f.Add([]byte("hello"), []byte("world"))
	f.Add([]byte("ethereum"), []byte("trie"))
	// Empty key.
	f.Add([]byte{}, []byte{0x01})
	// Single byte key and value.
	f.Add([]byte{0xff}, []byte{0xff})

	f.Fuzz(func(t *testing.T, key, value []byte) {
		// Cap sizes to prevent excessive memory usage.
		if len(key) > 128 {
			key = key[:128]
		}
		if len(value) > 128 {
			value = value[:128]
		}

		tr := New()

		// Empty value means delete, so skip that case for insert-get tests.
		if len(value) == 0 {
			// Still exercise Put with empty value (deletion path); must not panic.
			_ = tr.Put(key, value)
			return
		}

		// Insert the key-value pair.
		err := tr.Put(key, value)
		if err != nil {
			// Some errors are expected (e.g., hash node), but no panic.
			return
		}

		// Retrieve and verify.
		got, err := tr.Get(key)
		if err != nil {
			t.Fatalf("Get(%x) after Put failed: %v", key, err)
		}
		if !bytes.Equal(got, value) {
			t.Fatalf("Get(%x) = %x, want %x", key, got, value)
		}
	})
}

// FuzzTrieHash inserts random data into a trie, computes the hash, and
// verifies it is always 32 bytes and deterministic (same insertions produce
// the same hash).
func FuzzTrieHash(f *testing.F) {
	// Seed corpus: sequences of key-value pairs encoded as length-prefixed chunks.
	// Format: each entry is [keyLen(1), key..., valLen(1), val...]
	f.Add([]byte{
		0x01, 0xaa, // key: [0xaa]
		0x01, 0xbb, // val: [0xbb]
	})
	f.Add([]byte{
		0x02, 0x01, 0x02, // key: [0x01, 0x02]
		0x03, 0x0a, 0x0b, 0x0c, // val: [0x0a, 0x0b, 0x0c]
		0x01, 0x03, // key: [0x03]
		0x01, 0x04, // val: [0x04]
	})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		// Parse key-value pairs from data.
		type kv struct {
			key, val []byte
		}
		var pairs []kv
		for len(data) >= 2 {
			keyLen := int(data[0])
			data = data[1:]
			if keyLen > 64 {
				keyLen = 64
			}
			if keyLen > len(data) {
				break
			}
			key := make([]byte, keyLen)
			copy(key, data[:keyLen])
			data = data[keyLen:]

			if len(data) < 1 {
				break
			}
			valLen := int(data[0])
			data = data[1:]
			if valLen > 64 {
				valLen = 64
			}
			if valLen > len(data) {
				break
			}
			val := make([]byte, valLen)
			copy(val, data[:valLen])
			data = data[valLen:]

			// Skip empty values (they trigger delete, not insert).
			if len(val) == 0 {
				continue
			}
			pairs = append(pairs, kv{key, val})
		}

		// Build trie and compute hash.
		tr1 := New()
		for _, p := range pairs {
			_ = tr1.Put(p.key, p.val)
		}
		hash1 := tr1.Hash()

		// Verify hash is always 32 bytes.
		if len(hash1) != 32 {
			t.Fatalf("hash length = %d, want 32", len(hash1))
		}

		// Build a second trie with the same insertions and verify determinism.
		tr2 := New()
		for _, p := range pairs {
			_ = tr2.Put(p.key, p.val)
		}
		hash2 := tr2.Hash()

		if hash1 != hash2 {
			t.Fatalf("non-deterministic hash: %x != %x", hash1, hash2)
		}
	})
}

// FuzzTrieProof inserts data into a trie, generates a proof for a random key,
// and verifies the proof. Must not panic.
func FuzzTrieProof(f *testing.F) {
	// Seed: key to insert, key to prove.
	f.Add([]byte{0x01}, []byte{0xaa}, []byte{0x01})
	f.Add([]byte{0x01, 0x02}, []byte{0xbb}, []byte{0x01, 0x02})
	f.Add([]byte{0x01}, []byte{0xaa}, []byte{0x02}) // prove absent key
	f.Add([]byte("hello"), []byte("world"), []byte("hello"))
	f.Add([]byte("hello"), []byte("world"), []byte("hx")) // absent key

	f.Fuzz(func(t *testing.T, insertKey, insertVal, proofKey []byte) {
		// Cap sizes.
		if len(insertKey) > 64 {
			insertKey = insertKey[:64]
		}
		if len(insertVal) > 64 {
			insertVal = insertVal[:64]
		}
		if len(proofKey) > 64 {
			proofKey = proofKey[:64]
		}
		if len(insertVal) == 0 {
			return
		}

		tr := New()
		err := tr.Put(insertKey, insertVal)
		if err != nil {
			return
		}

		rootHash := tr.Hash()

		// Try to generate a proof for proofKey. Must not panic.
		proof, err := tr.Prove(proofKey)
		if err != nil {
			// Key not found is expected for absent keys. Try absence proof.
			proof, err = tr.ProveAbsence(proofKey)
			if err != nil {
				return
			}
			// Verify absence proof does not panic.
			_, _ = VerifyProof(rootHash, proofKey, proof)
			return
		}

		// Verify the proof. Must not panic.
		val, err := VerifyProof(rootHash, proofKey, proof)
		if err != nil {
			t.Fatalf("VerifyProof failed for existing key %x: %v", proofKey, err)
		}
		if !bytes.Equal(val, insertVal) {
			t.Fatalf("VerifyProof returned %x, want %x", val, insertVal)
		}
	})
}

// FuzzTrieMultiInsertDelete exercises insertion and deletion of multiple
// keys with random data. The trie must remain consistent throughout.
func FuzzTrieMultiInsertDelete(f *testing.F) {
	// Each chunk: op(1) + keyLen(1) + key... + valLen(1) + val...
	// op=0: insert, op=1: delete
	f.Add([]byte{
		0x00,             // insert
		0x02, 0xab, 0xcd, // key
		0x01, 0xef, // value
	})
	f.Add([]byte{
		0x00,       // insert
		0x01, 0x01, // key
		0x01, 0x0a, // value
		0x01,       // delete
		0x01, 0x01, // key
		0x00, // (no value for delete)
	})

	f.Fuzz(func(t *testing.T, data []byte) {
		tr := New()
		inserted := make(map[string][]byte)

		for len(data) >= 3 {
			op := data[0]
			data = data[1:]

			keyLen := int(data[0])
			data = data[1:]
			if keyLen > 32 {
				keyLen = 32
			}
			if keyLen > len(data) {
				break
			}
			key := make([]byte, keyLen)
			copy(key, data[:keyLen])
			data = data[keyLen:]

			if op%2 == 0 {
				// Insert.
				if len(data) < 1 {
					break
				}
				valLen := int(data[0])
				data = data[1:]
				if valLen > 32 {
					valLen = 32
				}
				if valLen > len(data) {
					break
				}
				val := make([]byte, valLen)
				copy(val, data[:valLen])
				data = data[valLen:]

				if len(val) == 0 {
					continue
				}
				_ = tr.Put(key, val)
				inserted[string(key)] = val
			} else {
				// Delete.
				_ = tr.Delete(key)
				delete(inserted, string(key))
			}
		}

		// Verify all inserted keys are retrievable.
		for k, v := range inserted {
			got, err := tr.Get([]byte(k))
			if err != nil {
				t.Fatalf("Get(%x) failed after operations: %v", k, err)
			}
			if !bytes.Equal(got, v) {
				t.Fatalf("Get(%x) = %x, want %x", k, got, v)
			}
		}

		// Hash must not panic.
		h := tr.Hash()
		if len(h) != 32 {
			t.Fatalf("hash length = %d, want 32", len(h))
		}
	})
}
