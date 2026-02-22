package snapshot

import (
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/types"
)

// snapshotDB is the database interface required by the snapshot disk layer.
// It combines key-value store, batch writes, and iteration.
type snapshotDB interface {
	rawdb.KeyValueStore
	NewBatch() rawdb.Batch
	NewIterator(prefix []byte) rawdb.Iterator
}

// diskLayer is a low level persistent snapshot built on top of a key-value
// store. It represents the base layer of the snapshot tree.
type diskLayer struct {
	diskdb snapshotDB // Key-value store containing the base snapshot
	root   types.Hash // Root hash of the base snapshot
	stale  bool       // Signals that the layer became stale (state progressed)
	lock   sync.RWMutex
}

// Root returns the root hash for which this snapshot was made.
func (dl *diskLayer) Root() types.Hash {
	return dl.root
}

// Parent always returns nil as there's no layer below the disk.
func (dl *diskLayer) Parent() snapshot {
	return nil
}

// Stale returns whether this layer has become stale.
func (dl *diskLayer) Stale() bool {
	dl.lock.RLock()
	defer dl.lock.RUnlock()
	return dl.stale
}

// markStale sets the stale flag as true.
func (dl *diskLayer) markStale() {
	dl.lock.Lock()
	defer dl.lock.Unlock()
	dl.stale = true
}

// Account retrieves the account associated with a particular hash from the
// disk database. Returns nil, nil if the account does not exist.
func (dl *diskLayer) Account(hash types.Hash) (*types.Account, error) {
	dl.lock.RLock()
	defer dl.lock.RUnlock()

	if dl.stale {
		return nil, ErrSnapshotStale
	}
	if dl.diskdb == nil {
		return nil, nil
	}
	key := accountSnapshotKey(hash)
	data, err := dl.diskdb.Get(key)
	if err != nil {
		if err == rawdb.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	return decodeAccount(data)
}

// Storage retrieves the storage data associated with a particular hash within
// a particular account from the disk database.
func (dl *diskLayer) Storage(accountHash, storageHash types.Hash) ([]byte, error) {
	dl.lock.RLock()
	defer dl.lock.RUnlock()

	if dl.stale {
		return nil, ErrSnapshotStale
	}
	if dl.diskdb == nil {
		return nil, nil
	}
	key := storageSnapshotKey(accountHash, storageHash)
	data, err := dl.diskdb.Get(key)
	if err != nil {
		if err == rawdb.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

// Update creates a new diff layer on top of the disk layer.
func (dl *diskLayer) Update(blockRoot types.Hash, accounts map[types.Hash][]byte, storage map[types.Hash]map[types.Hash][]byte) *diffLayer {
	return newDiffLayer(dl, blockRoot, accounts, storage)
}

// AccountIterator creates an account iterator over the disk layer.
// It scans the disk database for all snapshot account entries.
func (dl *diskLayer) AccountIterator(seek types.Hash) AccountIterator {
	dl.lock.RLock()
	defer dl.lock.RUnlock()

	if dl.diskdb == nil || dl.stale {
		return &diskAccountIterator{}
	}
	// Iterate over all keys with the snapshot account prefix.
	iter := dl.diskdb.NewIterator(snapshotAccountPrefix)
	var hashes []types.Hash
	data := make(map[types.Hash][]byte)

	for iter.Next() {
		key := iter.Key()
		if len(key) != len(snapshotAccountPrefix)+types.HashLength {
			continue
		}
		var hash types.Hash
		copy(hash[:], key[len(snapshotAccountPrefix):])
		if !hashLess(hash, seek) { // hash >= seek
			hashes = append(hashes, hash)
			val := make([]byte, len(iter.Value()))
			copy(val, iter.Value())
			data[hash] = val
		}
	}
	iter.Release()

	sort.Slice(hashes, func(i, j int) bool {
		return hashLess(hashes[i], hashes[j])
	})
	return &diskAccountIterator{
		hashes: hashes,
		data:   data,
		pos:    -1,
	}
}

// StorageIterator creates a storage iterator for a specific account.
func (dl *diskLayer) StorageIterator(accountHash types.Hash, seek types.Hash) StorageIterator {
	dl.lock.RLock()
	defer dl.lock.RUnlock()

	if dl.diskdb == nil || dl.stale {
		return &diskStorageIterator{}
	}
	// Build the prefix for this account's storage: ss + accountHash
	prefix := append(append([]byte{}, snapshotStoragePrefix...), accountHash[:]...)
	iter := dl.diskdb.NewIterator(prefix)

	var hashes []types.Hash
	data := make(map[types.Hash][]byte)
	prefixLen := len(prefix)

	for iter.Next() {
		key := iter.Key()
		if len(key) != prefixLen+types.HashLength {
			continue
		}
		var hash types.Hash
		copy(hash[:], key[prefixLen:])
		if !hashLess(hash, seek) { // hash >= seek
			hashes = append(hashes, hash)
			val := make([]byte, len(iter.Value()))
			copy(val, iter.Value())
			data[hash] = val
		}
	}
	iter.Release()

	sort.Slice(hashes, func(i, j int) bool {
		return hashLess(hashes[i], hashes[j])
	})
	return &diskStorageIterator{
		hashes: hashes,
		data:   data,
		pos:    -1,
	}
}
