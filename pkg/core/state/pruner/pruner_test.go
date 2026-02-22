package pruner

import (
	"testing"

	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/types"
)

func TestBloomAddContains(t *testing.T) {
	bf := newBloom(1024)

	key1 := []byte("hello")
	key2 := []byte("world")
	bf.add(key1)
	bf.add(key2)

	if !bf.contains(key1) {
		t.Fatal("expected bloom to contain key1")
	}
	if !bf.contains(key2) {
		t.Fatal("expected bloom to contain key2")
	}
	// A key never added should ideally not match, but bloom filters can
	// have false positives. With a 1024-bit filter and only 2 entries the
	// false positive rate is negligible, so we check this case too.
	absent := []byte("absent")
	if bf.contains(absent) {
		t.Log("bloom false positive for absent key (acceptable)")
	}
}

func TestBloomNoFalseNegatives(t *testing.T) {
	bf := newBloom(1024 * 1024)

	// Add many keys and verify none are missed.
	keys := make([][]byte, 1000)
	for i := range keys {
		keys[i] = []byte{byte(i >> 8), byte(i), 0xAA, 0xBB}
		bf.add(keys[i])
	}
	for _, key := range keys {
		if !bf.contains(key) {
			t.Fatalf("bloom false negative for key %x", key)
		}
	}
}

func TestNewPrunerDefaults(t *testing.T) {
	db := rawdb.NewMemoryDB()
	p := NewPruner(PrunerConfig{}, db)

	if p.config.BloomSize != DefaultBloomSize {
		t.Fatalf("expected default bloom size %d, got %d", DefaultBloomSize, p.config.BloomSize)
	}
}

func TestPruneEmptyDatabase(t *testing.T) {
	db := rawdb.NewMemoryDB()
	p := NewPruner(PrunerConfig{BloomSize: 4096}, db)

	root := types.Hash{}
	deleted, err := p.Prune(root)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Fatalf("expected 0 deletions on empty db, got %d", deleted)
	}
}

func TestPruneMarksAllReachable(t *testing.T) {
	db := rawdb.NewMemoryDB()

	// Populate db with some snapshot entries.
	acctKey1 := append([]byte("sa"), make([]byte, 32)...)
	acctKey2 := append([]byte("sa"), make([]byte, 32)...)
	acctKey2[2] = 0x01

	db.Put(acctKey1, []byte{1})
	db.Put(acctKey2, []byte{2})

	p := NewPruner(PrunerConfig{BloomSize: 1024 * 1024}, db)
	root := types.Hash{}

	// Prune should mark all existing entries as reachable (since we iterate
	// the same prefix we're sweeping) and delete nothing.
	deleted, err := p.Prune(root)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Fatalf("expected 0 deletions (all reachable), got %d", deleted)
	}

	// Verify entries still exist.
	has1, _ := db.Has(acctKey1)
	has2, _ := db.Has(acctKey2)
	if !has1 || !has2 {
		t.Fatal("expected both entries to still exist")
	}
}

func TestPruneByKeys(t *testing.T) {
	db := rawdb.NewMemoryDB()

	// Create some snapshot account entries.
	key1 := append([]byte("sa"), []byte{0x01, 0x02}...)
	key2 := append([]byte("sa"), []byte{0x03, 0x04}...)
	key3 := append([]byte("sa"), []byte{0x05, 0x06}...)

	db.Put(key1, []byte{1})
	db.Put(key2, []byte{2})
	db.Put(key3, []byte{3})

	// Only keep key1 and key3.
	keep := map[string]struct{}{
		string(key1): {},
		string(key3): {},
	}

	p := NewPruner(PrunerConfig{BloomSize: 4096}, db)
	deleted, err := p.PruneByKeys(keep, [][]byte{[]byte("sa")})
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 deletion, got %d", deleted)
	}

	// Verify key2 was deleted.
	has2, _ := db.Has(key2)
	if has2 {
		t.Fatal("expected key2 to be deleted")
	}
	// Verify key1 and key3 still exist.
	has1, _ := db.Has(key1)
	has3, _ := db.Has(key3)
	if !has1 || !has3 {
		t.Fatal("expected key1 and key3 to still exist")
	}
}

func TestPruneByKeysMultiplePrefixes(t *testing.T) {
	db := rawdb.NewMemoryDB()

	// Snapshot account entries.
	acctKey := append([]byte("sa"), []byte{0x01}...)
	// Snapshot storage entries.
	storKey := append([]byte("ss"), []byte{0x02}...)
	// Trie node entries.
	trieKey := append([]byte("t"), []byte{0x03}...)

	db.Put(acctKey, []byte{1})
	db.Put(storKey, []byte{2})
	db.Put(trieKey, []byte{3})

	// Keep only the account entry.
	keep := map[string]struct{}{
		string(acctKey): {},
	}

	p := NewPruner(PrunerConfig{BloomSize: 4096}, db)
	deleted, err := p.PruneByKeys(keep, [][]byte{[]byte("sa"), []byte("ss"), []byte("t")})
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Fatalf("expected 2 deletions, got %d", deleted)
	}

	hasAcct, _ := db.Has(acctKey)
	hasStor, _ := db.Has(storKey)
	hasTrie, _ := db.Has(trieKey)

	if !hasAcct {
		t.Fatal("expected account key to be kept")
	}
	if hasStor {
		t.Fatal("expected storage key to be deleted")
	}
	if hasTrie {
		t.Fatal("expected trie key to be deleted")
	}
}

func TestPruneNilDatabase(t *testing.T) {
	p := &Pruner{config: PrunerConfig{BloomSize: 1024}}
	_, err := p.Prune(types.Hash{})
	if err == nil {
		t.Fatal("expected error for nil database")
	}
}

func TestPruneByKeysNilDatabase(t *testing.T) {
	p := &Pruner{config: PrunerConfig{BloomSize: 1024}}
	_, err := p.PruneByKeys(nil, nil)
	if err == nil {
		t.Fatal("expected error for nil database")
	}
}
