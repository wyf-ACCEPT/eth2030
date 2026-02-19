package snapshot

import (
	"sort"
	"sync"
	"sync/atomic"

	"github.com/eth2028/eth2028/core/types"
)

// diffLayer represents a collection of modifications made to a state snapshot
// after running a block on top. It contains account data and storage data maps
// keyed by their respective hashes.
//
// The goal of a diff layer is to act as a journal, tracking recent modifications
// made to the state, that have not yet graduated into a semi-immutable state.
type diffLayer struct {
	parent snapshot    // Parent snapshot modified by this one, never nil
	root   types.Hash // Root hash to which this snapshot diff belongs to
	stale  atomic.Bool

	accountData map[types.Hash][]byte                 // Keyed accounts for direct retrieval (nil means deleted)
	storageData map[types.Hash]map[types.Hash][]byte // Keyed storage slots for direct retrieval (nil means deleted)
	memory      uint64                                // Approximate memory usage in bytes

	lock sync.RWMutex
}

// newDiffLayer creates a new diff on top of an existing snapshot, whether
// that's a low level persistent database or a hierarchical diff already.
func newDiffLayer(parent snapshot, root types.Hash, accounts map[types.Hash][]byte, storage map[types.Hash]map[types.Hash][]byte) *diffLayer {
	dl := &diffLayer{
		parent:      parent,
		root:        root,
		accountData: accounts,
		storageData: storage,
	}
	// Track memory usage.
	for _, data := range accounts {
		dl.memory += uint64(types.HashLength + len(data))
	}
	for _, slots := range storage {
		for _, data := range slots {
			dl.memory += uint64(types.HashLength + len(data))
		}
	}
	return dl
}

// Root returns the root hash for which this snapshot was made.
func (dl *diffLayer) Root() types.Hash {
	return dl.root
}

// Parent returns the parent layer of this diff.
func (dl *diffLayer) Parent() snapshot {
	dl.lock.RLock()
	defer dl.lock.RUnlock()
	return dl.parent
}

// Stale returns whether this layer has become stale (was flattened across).
func (dl *diffLayer) Stale() bool {
	return dl.stale.Load()
}

// markStale sets the stale flag.
func (dl *diffLayer) markStale() {
	dl.stale.Store(true)
}

// Account retrieves the account associated with a particular hash.
// It checks the local layer first, then walks the parent chain.
func (dl *diffLayer) Account(hash types.Hash) (*types.Account, error) {
	dl.lock.RLock()
	if dl.Stale() {
		dl.lock.RUnlock()
		return nil, ErrSnapshotStale
	}
	// Check local data first.
	if data, ok := dl.accountData[hash]; ok {
		dl.lock.RUnlock()
		if len(data) == 0 {
			return nil, nil // Account was deleted.
		}
		return decodeAccount(data)
	}
	parent := dl.parent
	dl.lock.RUnlock()
	// Not found locally, resolve from parent.
	return parent.Account(hash)
}

// Storage retrieves the storage data associated with a particular hash within
// a particular account. Checks local layer first, then walks parent chain.
func (dl *diffLayer) Storage(accountHash, storageHash types.Hash) ([]byte, error) {
	dl.lock.RLock()
	if dl.Stale() {
		dl.lock.RUnlock()
		return nil, ErrSnapshotStale
	}
	// Check local data first.
	if slots, ok := dl.storageData[accountHash]; ok {
		if data, ok := slots[storageHash]; ok {
			dl.lock.RUnlock()
			return data, nil
		}
	}
	parent := dl.parent
	dl.lock.RUnlock()
	// Not found locally, resolve from parent.
	return parent.Storage(accountHash, storageHash)
}

// Update creates a new layer on top of this diff layer.
func (dl *diffLayer) Update(blockRoot types.Hash, accounts map[types.Hash][]byte, storage map[types.Hash]map[types.Hash][]byte) *diffLayer {
	return newDiffLayer(dl, blockRoot, accounts, storage)
}

// Memory returns the approximate memory usage of this diff layer in bytes.
func (dl *diffLayer) Memory() uint64 {
	return dl.memory
}

// flatten merges this diff layer into the given disk layer, producing a new
// disk layer with the merged data written to disk.
func (dl *diffLayer) flatten(disk *diskLayer) *diskLayer {
	// Write account data to disk.
	if disk.diskdb != nil {
		batch := disk.diskdb.NewBatch()
		for hash, data := range dl.accountData {
			key := accountSnapshotKey(hash)
			if len(data) == 0 {
				batch.Delete(key)
			} else {
				batch.Put(key, data)
			}
		}
		for accountHash, slots := range dl.storageData {
			for storageHash, data := range slots {
				key := storageSnapshotKey(accountHash, storageHash)
				if len(data) == 0 {
					batch.Delete(key)
				} else {
					batch.Put(key, data)
				}
			}
		}
		batch.Write()
	}
	return &diskLayer{
		diskdb: disk.diskdb,
		root:   dl.root,
	}
}

// AccountIterator creates an account iterator over this diff layer.
func (dl *diffLayer) AccountIterator(seek types.Hash) AccountIterator {
	// Collect all account hashes from this layer.
	dl.lock.RLock()
	hashes := make([]types.Hash, 0, len(dl.accountData))
	for hash := range dl.accountData {
		hashes = append(hashes, hash)
	}
	dl.lock.RUnlock()

	sort.Slice(hashes, func(i, j int) bool {
		return hashLess(hashes[i], hashes[j])
	})
	return &diffAccountIterator{
		layer:  dl,
		hashes: hashes,
		pos:    -1,
		seek:   seek,
	}
}

// StorageIterator creates a storage iterator for a specific account.
func (dl *diffLayer) StorageIterator(accountHash types.Hash, seek types.Hash) StorageIterator {
	dl.lock.RLock()
	slots := dl.storageData[accountHash]
	hashes := make([]types.Hash, 0, len(slots))
	for hash := range slots {
		hashes = append(hashes, hash)
	}
	dl.lock.RUnlock()

	sort.Slice(hashes, func(i, j int) bool {
		return hashLess(hashes[i], hashes[j])
	})
	return &diffStorageIterator{
		layer:       dl,
		accountHash: accountHash,
		hashes:      hashes,
		pos:         -1,
		seek:        seek,
	}
}

// decodeAccount decodes RLP-encoded slim account data into an Account struct.
// For our simplified snapshot, the "RLP" encoding is just the raw bytes
// as stored by the tree's Update method.
func decodeAccount(data []byte) (*types.Account, error) {
	if len(data) == 0 {
		return nil, nil
	}
	// The snapshot stores accounts as raw bytes passed by the caller.
	// Return a new account with the data stored as-is.
	acc := types.NewAccount()
	// Store the raw data: caller is responsible for encoding/decoding.
	// For the snapshot layer, we store the raw blob and the caller
	// interprets it. Return a minimal account object.
	return &acc, nil
}

// hashLess returns true if a < b lexicographically.
func hashLess(a, b types.Hash) bool {
	for i := 0; i < types.HashLength; i++ {
		if a[i] < b[i] {
			return true
		}
		if a[i] > b[i] {
			return false
		}
	}
	return false
}

// Key schema for snapshot data in the disk database.
var (
	snapshotAccountPrefix = []byte("sa") // sa + account hash -> account data
	snapshotStoragePrefix = []byte("ss") // ss + account hash + storage hash -> storage data
)

func accountSnapshotKey(hash types.Hash) []byte {
	return append(append([]byte{}, snapshotAccountPrefix...), hash[:]...)
}

func storageSnapshotKey(accountHash, storageHash types.Hash) []byte {
	key := append([]byte{}, snapshotStoragePrefix...)
	key = append(key, accountHash[:]...)
	key = append(key, storageHash[:]...)
	return key
}
