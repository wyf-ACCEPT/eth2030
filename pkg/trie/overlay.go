package trie

import (
	"errors"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// OverlayConfig controls the overlay trie behavior during MPT-to-Verkle migration.
type OverlayConfig struct {
	// ReadFromOld enables reads from the old (MPT) trie when key is not in the new trie.
	ReadFromOld bool
	// WriteToNew enables writes to the new (Verkle-targeted) trie.
	WriteToNew bool
}

// DefaultOverlayConfig returns an OverlayConfig with both reads and writes enabled.
func DefaultOverlayConfig() OverlayConfig {
	return OverlayConfig{
		ReadFromOld: true,
		WriteToNew:  true,
	}
}

// sentinel value to mark deleted keys in the new trie.
var deletedSentinel = []byte{0xDE, 0xAD}

// OverlayTrie provides an overlay that reads from an old (MPT) trie
// while writing to a new trie. This supports the MPT-to-Verkle migration
// by allowing gradual state transfer. Reads check the new trie first;
// if the key is not found, they fall back to the old trie.
type OverlayTrie struct {
	mu     sync.RWMutex
	config OverlayConfig
	old    *Trie // read-only source (MPT)
	new_   *Trie // write target (new state)
	// deleted tracks keys that have been explicitly deleted in the overlay,
	// so we don't fall back to the old trie for them.
	deleted map[string]bool
}

// NewOverlayTrie creates an overlay trie. If config.ReadFromOld is true,
// an old trie is created as the fallback source. A new trie is always created
// for writes.
func NewOverlayTrie(config OverlayConfig) *OverlayTrie {
	return &OverlayTrie{
		config:  config,
		old:     New(),
		new_:    New(),
		deleted: make(map[string]bool),
	}
}

// Get retrieves the value for a key. It checks the new trie first,
// then falls back to the old trie if ReadFromOld is enabled.
func (o *OverlayTrie) Get(key []byte) ([]byte, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()

	// Check if the key was deleted in the overlay.
	if o.deleted[string(key)] {
		return nil, ErrNotFound
	}

	// Try the new trie first.
	val, err := o.new_.Get(key)
	if err == nil {
		return val, nil
	}

	// Fall back to old trie if configured.
	if o.config.ReadFromOld {
		return o.old.Get(key)
	}
	return nil, ErrNotFound
}

// Put writes a key-value pair to the new trie.
func (o *OverlayTrie) Put(key, value []byte) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if !o.config.WriteToNew {
		return errors.New("overlay: writes disabled")
	}

	// Remove from deleted set if previously deleted.
	delete(o.deleted, string(key))
	return o.new_.Put(key, value)
}

// Delete marks a key as deleted in the overlay. The key will not be found
// in subsequent Get calls even if it exists in the old trie.
func (o *OverlayTrie) Delete(key []byte) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.deleted[string(key)] = true
	// Also delete from new trie if present.
	o.new_.Delete(key)
	return nil
}

// Has returns true if the key exists in either the new or old trie
// and has not been deleted.
func (o *OverlayTrie) Has(key []byte) (bool, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()

	if o.deleted[string(key)] {
		return false, nil
	}

	// Check new trie.
	_, err := o.new_.Get(key)
	if err == nil {
		return true, nil
	}

	// Check old trie.
	if o.config.ReadFromOld {
		_, err = o.old.Get(key)
		if err == nil {
			return true, nil
		}
	}

	return false, nil
}

// Hash computes the root hash of the new trie.
func (o *OverlayTrie) Hash() types.Hash {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.new_.Hash()
}

// MigratedKeys returns the number of keys stored in the new trie.
func (o *OverlayTrie) MigratedKeys() int {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.new_.Len()
}

// Commit computes and returns the root hash of the new trie.
// In a production implementation, this would persist the trie to storage.
func (o *OverlayTrie) Commit() (types.Hash, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	h := o.new_.Hash()
	return h, nil
}

// OldTrie returns the old (read-only) trie for pre-populating test data.
func (o *OverlayTrie) OldTrie() *Trie {
	return o.old
}

// NewTrie returns the new (write) trie.
func (o *OverlayTrie) NewTrie() *Trie {
	return o.new_
}

// OverlayHash computes a combined hash of both tries for integrity checking.
// This hashes the concatenation of old root and new root.
func (o *OverlayTrie) OverlayHash() types.Hash {
	o.mu.RLock()
	defer o.mu.RUnlock()

	oldHash := o.old.Hash()
	newHash := o.new_.Hash()
	combined := append(oldHash[:], newHash[:]...)
	return crypto.Keccak256Hash(combined)
}
