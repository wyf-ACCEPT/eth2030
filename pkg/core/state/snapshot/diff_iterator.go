// diff_iterator.go implements an iterator over snapshot diff layers for state
// synchronization. It supports forward and backward iteration, filtering by
// account prefix, merging multiple diff layers into a combined change set,
// and producing ordered change sets suitable for snap sync.
//
// The DiffIterator provides a unified view over one or more diff layers,
// presenting account and storage changes in hash-sorted order. It is designed
// for snap sync consumers that need to stream state changes efficiently.
package snapshot

import (
	"bytes"
	"sort"

	"github.com/eth2028/eth2028/core/types"
)

// ChangeType indicates whether an entry was created, modified, or deleted.
type ChangeType uint8

const (
	ChangeCreated  ChangeType = iota // New entry
	ChangeModified                   // Value changed
	ChangeDeleted                    // Entry removed (nil data)
)

// String returns a human-readable label for the change type.
func (ct ChangeType) String() string {
	switch ct {
	case ChangeCreated:
		return "created"
	case ChangeModified:
		return "modified"
	case ChangeDeleted:
		return "deleted"
	default:
		return "unknown"
	}
}

// AccountChange describes a change to an account in a diff layer.
type AccountChange struct {
	Hash    types.Hash
	Data    []byte     // nil for deletions
	Change  ChangeType
	LayerID int        // index of the diff layer that contains this change
}

// StorageChange describes a change to a storage slot within an account.
type StorageChange struct {
	AccountHash types.Hash
	SlotHash    types.Hash
	Data        []byte     // nil for deletions
	Change      ChangeType
	LayerID     int
}

// ChangeSet is a collection of account and storage changes from one or more
// diff layers, suitable for snap sync streaming.
type ChangeSet struct {
	Accounts []AccountChange
	Storage  []StorageChange
}

// AccountCount returns the number of account changes.
func (cs *ChangeSet) AccountCount() int {
	return len(cs.Accounts)
}

// StorageCount returns the number of storage changes.
func (cs *ChangeSet) StorageCount() int {
	return len(cs.Storage)
}

// DiffLayerIterator iterates over the contents of one or more diff layers
// in hash-sorted order, producing account and storage changes.
type DiffLayerIterator struct {
	layers  []*diffLayer
	current int        // index into layers (forward mode)
	reverse bool       // if true, iterate backward

	// Account iteration state.
	accountHashes []types.Hash
	accountPos    int
	accountDone   bool

	// Prefix filter (nil means no filter).
	prefix []byte
}

// NewDiffLayerIterator creates an iterator over a single diff layer.
func NewDiffLayerIterator(dl *diffLayer) *DiffLayerIterator {
	return &DiffLayerIterator{
		layers:  []*diffLayer{dl},
		current: 0,
	}
}

// NewMultiDiffLayerIterator creates an iterator over multiple diff layers.
// Layers are processed in the order given (typically oldest to newest).
func NewMultiDiffLayerIterator(layers []*diffLayer) *DiffLayerIterator {
	return &DiffLayerIterator{
		layers:  layers,
		current: 0,
	}
}

// NewReverseDiffLayerIterator creates a backward iterator over diff layers.
func NewReverseDiffLayerIterator(layers []*diffLayer) *DiffLayerIterator {
	start := len(layers) - 1
	if start < 0 {
		start = 0
	}
	return &DiffLayerIterator{
		layers:  layers,
		current: start,
		reverse: true,
	}
}

// SetPrefix sets a prefix filter for account hashes. Only accounts whose
// hash starts with the given prefix will be included in iteration.
func (it *DiffLayerIterator) SetPrefix(prefix []byte) {
	it.prefix = prefix
}

// matchesPrefix returns true if the hash matches the current prefix filter.
func (it *DiffLayerIterator) matchesPrefix(hash types.Hash) bool {
	if len(it.prefix) == 0 {
		return true
	}
	return bytes.HasPrefix(hash[:], it.prefix)
}

// AccountChanges returns all account changes across the configured layers.
// Changes are sorted by hash and de-duplicated: if the same account appears
// in multiple layers, only the latest change is included.
func (it *DiffLayerIterator) AccountChanges() []AccountChange {
	seen := make(map[types.Hash]*AccountChange)

	processLayer := func(idx int, dl *diffLayer) {
		dl.lock.RLock()
		defer dl.lock.RUnlock()

		for hash, data := range dl.accountData {
			if !it.matchesPrefix(hash) {
				continue
			}
			ct := ChangeModified
			if len(data) == 0 {
				ct = ChangeDeleted
			}
			// Latest layer wins (overwrites earlier).
			seen[hash] = &AccountChange{
				Hash:    hash,
				Data:    copyBytes(data),
				Change:  ct,
				LayerID: idx,
			}
		}
	}

	if it.reverse {
		for i := len(it.layers) - 1; i >= 0; i-- {
			processLayer(i, it.layers[i])
		}
	} else {
		for i, dl := range it.layers {
			processLayer(i, dl)
		}
	}

	// Sort by hash.
	changes := make([]AccountChange, 0, len(seen))
	for _, ch := range seen {
		changes = append(changes, *ch)
	}
	sort.Slice(changes, func(i, j int) bool {
		return hashLess(changes[i].Hash, changes[j].Hash)
	})
	return changes
}

// StorageChanges returns all storage changes across the configured layers
// for the given account hash. Changes are sorted by slot hash.
func (it *DiffLayerIterator) StorageChanges(accountHash types.Hash) []StorageChange {
	seen := make(map[types.Hash]*StorageChange)

	processLayer := func(idx int, dl *diffLayer) {
		dl.lock.RLock()
		defer dl.lock.RUnlock()

		slots, ok := dl.storageData[accountHash]
		if !ok {
			return
		}
		for slotHash, data := range slots {
			ct := ChangeModified
			if len(data) == 0 {
				ct = ChangeDeleted
			}
			seen[slotHash] = &StorageChange{
				AccountHash: accountHash,
				SlotHash:    slotHash,
				Data:        copyBytes(data),
				Change:      ct,
				LayerID:     idx,
			}
		}
	}

	if it.reverse {
		for i := len(it.layers) - 1; i >= 0; i-- {
			processLayer(i, it.layers[i])
		}
	} else {
		for i, dl := range it.layers {
			processLayer(i, dl)
		}
	}

	changes := make([]StorageChange, 0, len(seen))
	for _, ch := range seen {
		changes = append(changes, *ch)
	}
	sort.Slice(changes, func(i, j int) bool {
		return hashLess(changes[i].SlotHash, changes[j].SlotHash)
	})
	return changes
}

// AllStorageChanges returns storage changes for all accounts across all layers.
func (it *DiffLayerIterator) AllStorageChanges() []StorageChange {
	// Collect all account hashes that have storage changes.
	acctSet := make(map[types.Hash]struct{})
	for _, dl := range it.layers {
		dl.lock.RLock()
		for acctHash := range dl.storageData {
			acctSet[acctHash] = struct{}{}
		}
		dl.lock.RUnlock()
	}

	var all []StorageChange
	for acctHash := range acctSet {
		changes := it.StorageChanges(acctHash)
		all = append(all, changes...)
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].AccountHash != all[j].AccountHash {
			return hashLess(all[i].AccountHash, all[j].AccountHash)
		}
		return hashLess(all[i].SlotHash, all[j].SlotHash)
	})
	return all
}

// BuildChangeSet produces a complete ChangeSet from all configured layers.
func (it *DiffLayerIterator) BuildChangeSet() *ChangeSet {
	return &ChangeSet{
		Accounts: it.AccountChanges(),
		Storage:  it.AllStorageChanges(),
	}
}

// LayerCount returns the number of diff layers in this iterator.
func (it *DiffLayerIterator) LayerCount() int {
	return len(it.layers)
}

// IsReverse returns true if this is a reverse iterator.
func (it *DiffLayerIterator) IsReverse() bool {
	return it.reverse
}

// --- Helper functions ---

// copyBytes makes a copy of a byte slice (nil-safe).
func copyBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	return cp
}

// MergeDiffLayers collects a chain of diff layers from a snapshot tree,
// walking from the given root down to the disk layer. Returns the layers
// in bottom-up order (oldest first).
func MergeDiffLayers(tree *Tree, root types.Hash) []*diffLayer {
	tree.lock.RLock()
	defer tree.lock.RUnlock()

	var layers []*diffLayer
	snap, ok := tree.layers[root]
	if !ok {
		return nil
	}

	for current := snap; current != nil; {
		if dl, ok := current.(*diffLayer); ok {
			layers = append(layers, dl)
			current = dl.Parent()
		} else {
			break // reached disk layer
		}
	}

	// Reverse to get oldest-first order.
	for i, j := 0, len(layers)-1; i < j; i, j = i+1, j-1 {
		layers[i], layers[j] = layers[j], layers[i]
	}
	return layers
}

// FilterAccountsByPrefix returns account hashes from a diff layer that
// match the given prefix bytes.
func FilterAccountsByPrefix(dl *diffLayer, prefix []byte) []types.Hash {
	dl.lock.RLock()
	defer dl.lock.RUnlock()

	var matches []types.Hash
	for hash := range dl.accountData {
		if len(prefix) == 0 || bytes.HasPrefix(hash[:], prefix) {
			matches = append(matches, hash)
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		return hashLess(matches[i], matches[j])
	})
	return matches
}
