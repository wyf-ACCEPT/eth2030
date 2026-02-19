// Package snapshot implements a journalled, dynamic state dump as a layered
// structure. It consists of one persistent base layer backed by a key-value
// store, on top of which arbitrarily many in-memory diff layers are stacked.
//
// The goal is to allow direct access to account and storage data without
// expensive multi-level trie lookups, and to provide sorted iteration of
// account/storage tries for sync purposes.
package snapshot

import (
	"errors"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

var (
	// ErrSnapshotStale is returned from data accessors if the underlying
	// snapshot layer has been invalidated due to the chain progressing
	// forward far enough to not maintain the layer's original state.
	ErrSnapshotStale = errors.New("snapshot stale")

	// ErrNotFound is returned when the requested account or storage slot
	// does not exist in the snapshot.
	ErrNotFound = errors.New("snapshot: not found")

	// errSnapshotCycle is returned if a snapshot update would form a cycle.
	errSnapshotCycle = errors.New("snapshot cycle")
)

// Snapshot represents the functionality supported by a snapshot storage layer.
type Snapshot interface {
	// Root returns the root hash for which this snapshot was made.
	Root() types.Hash

	// Account retrieves the account associated with a particular hash in
	// the snapshot. Returns nil, nil if the account does not exist.
	Account(hash types.Hash) (*types.Account, error)

	// Storage retrieves the storage data associated with a particular hash,
	// within a particular account.
	Storage(accountHash, storageHash types.Hash) ([]byte, error)
}

// snapshot is the internal version of the snapshot data layer that supports
// additional methods compared to the public API.
type snapshot interface {
	Snapshot

	// Parent returns the parent layer, or nil if the base was reached.
	Parent() snapshot

	// Update creates a new layer on top of the existing snapshot diff tree
	// with the specified data items. The maps are retained by the method.
	Update(blockRoot types.Hash, accounts map[types.Hash][]byte, storage map[types.Hash]map[types.Hash][]byte) *diffLayer

	// Stale returns whether this layer has become stale (was flattened across).
	Stale() bool

	// AccountIterator creates an account iterator over this layer.
	AccountIterator(seek types.Hash) AccountIterator

	// StorageIterator creates a storage iterator over this layer.
	StorageIterator(accountHash types.Hash, seek types.Hash) StorageIterator
}

// Tree is an Ethereum state snapshot tree. It consists of one persistent base
// layer backed by a key-value store, on top of which in-memory diff layers
// are stacked. The diff layers form a linear chain (no branching).
type Tree struct {
	layers map[types.Hash]snapshot // root -> layer
	lock   sync.RWMutex
}

// NewTree creates a new snapshot tree with a disk layer at the given root.
// The db must support reads, writes, batch operations, and iteration.
// Both rawdb.MemoryDB and rawdb.FileDB satisfy this requirement.
func NewTree(db snapshotDB, diskRoot types.Hash) *Tree {
	base := &diskLayer{
		diskdb: db,
		root:   diskRoot,
	}
	t := &Tree{
		layers: map[types.Hash]snapshot{
			diskRoot: base,
		},
	}
	return t
}

// Snapshot retrieves the snapshot at the given root, or nil if no layer
// matches the root.
func (t *Tree) Snapshot(root types.Hash) Snapshot {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return t.layers[root]
}

// Update adds a new diff layer on top of an existing snapshot identified by
// parentRoot. Accounts and storage maps are retained by the layer.
//   - accounts: hash -> RLP-encoded slim account (nil value = deleted)
//   - storage: accountHash -> (storageHash -> value) (nil value = deleted slot)
func (t *Tree) Update(blockRoot, parentRoot types.Hash, accounts map[types.Hash][]byte, storage map[types.Hash]map[types.Hash][]byte) error {
	t.lock.Lock()
	defer t.lock.Unlock()

	// Prevent cycles.
	if blockRoot == parentRoot {
		return errSnapshotCycle
	}
	parent, ok := t.layers[parentRoot]
	if !ok {
		return errors.New("snapshot: unknown parent root")
	}
	// Create a new diff layer.
	diff := newDiffLayer(parent, blockRoot, accounts, storage)
	t.layers[blockRoot] = diff
	return nil
}

// Cap flattens layers above the disk layer until at most `layers` diff layers
// remain above it. Older diff layers are merged downward. If layers == 0, all
// diff layers are flattened into the disk layer.
func (t *Tree) Cap(root types.Hash, layers int) error {
	t.lock.Lock()
	defer t.lock.Unlock()

	snap, ok := t.layers[root]
	if !ok {
		return errors.New("snapshot: unknown root for cap")
	}
	// Collect the chain from root down to the disk layer.
	chain := make([]snapshot, 0)
	for current := snap; current != nil; {
		chain = append(chain, current)
		if _, isDisk := current.(*diskLayer); isDisk {
			break
		}
		current = current.Parent()
	}
	// chain[0] = topmost layer, chain[len-1] = disk layer.
	// Number of diff layers is len(chain) - 1 (excluding disk).
	diffCount := len(chain) - 1
	if diffCount <= layers {
		return nil // Nothing to flatten.
	}
	// Flatten from the bottom up. The bottommost diff layer (just above disk)
	// is at chain[diffCount-1] (index len(chain)-2).
	// We need to flatten (diffCount - layers) diff layers.
	toFlatten := diffCount - layers
	for i := 0; i < toFlatten; i++ {
		// The bottommost diff layer is always at chain[len(chain)-2].
		bottomIdx := len(chain) - 2
		bottom, ok := chain[bottomIdx].(*diffLayer)
		if !ok {
			break
		}
		disk, ok := chain[len(chain)-1].(*diskLayer)
		if !ok {
			break
		}
		// Merge bottom diff layer into disk.
		newDisk := bottom.flatten(disk)
		// Mark old disk layer as stale.
		disk.markStale()
		// Remove the old disk layer and bottom diff from the chain.
		// Replace disk layer entry.
		delete(t.layers, disk.root)
		delete(t.layers, bottom.root)
		t.layers[newDisk.root] = newDisk
		// Mark the flattened diff layer as stale.
		bottom.markStale()
		// Update any diff layers that had the old bottom as parent.
		for _, layer := range t.layers {
			if diff, ok := layer.(*diffLayer); ok {
				if diff.parent == bottom {
					diff.lock.Lock()
					diff.parent = newDisk
					diff.lock.Unlock()
				}
			}
		}
		// Rebuild the chain for next iteration.
		chain = make([]snapshot, 0)
		for current := t.layers[root]; current != nil; {
			chain = append(chain, current)
			if _, isDisk := current.(*diskLayer); isDisk {
				break
			}
			current = current.Parent()
		}
	}
	return nil
}

// Size returns the number of layers in the tree.
func (t *Tree) Size() int {
	t.lock.RLock()
	defer t.lock.RUnlock()
	return len(t.layers)
}
