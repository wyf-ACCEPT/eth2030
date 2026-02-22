package snapshot

import "github.com/eth2030/eth2030/core/types"

// AccountIterator is an iterator to step over all the accounts in a snapshot.
type AccountIterator interface {
	// Next steps the iterator forward one element, returning false if exhausted.
	Next() bool

	// Hash returns the hash of the account the iterator is currently at.
	Hash() types.Hash

	// Account returns the RLP-encoded slim account data the iterator is at.
	Account() []byte

	// Release releases associated resources.
	Release()
}

// StorageIterator is an iterator to step over the storage slots in a snapshot.
type StorageIterator interface {
	// Next steps the iterator forward one element, returning false if exhausted.
	Next() bool

	// Hash returns the hash of the storage slot the iterator is currently at.
	Hash() types.Hash

	// Slot returns the storage slot data the iterator is currently at.
	Slot() []byte

	// Release releases associated resources.
	Release()
}

// --- diffLayer account iterator ---

type diffAccountIterator struct {
	layer  *diffLayer
	hashes []types.Hash
	pos    int
	seek   types.Hash
}

func (it *diffAccountIterator) Next() bool {
	for {
		it.pos++
		if it.pos >= len(it.hashes) {
			return false
		}
		// Skip hashes before seek.
		if hashLess(it.hashes[it.pos], it.seek) {
			continue
		}
		return true
	}
}

func (it *diffAccountIterator) Hash() types.Hash {
	if it.pos < 0 || it.pos >= len(it.hashes) {
		return types.Hash{}
	}
	return it.hashes[it.pos]
}

func (it *diffAccountIterator) Account() []byte {
	if it.pos < 0 || it.pos >= len(it.hashes) {
		return nil
	}
	it.layer.lock.RLock()
	defer it.layer.lock.RUnlock()
	return it.layer.accountData[it.hashes[it.pos]]
}

func (it *diffAccountIterator) Release() {}

// --- diffLayer storage iterator ---

type diffStorageIterator struct {
	layer       *diffLayer
	accountHash types.Hash
	hashes      []types.Hash
	pos         int
	seek        types.Hash
}

func (it *diffStorageIterator) Next() bool {
	for {
		it.pos++
		if it.pos >= len(it.hashes) {
			return false
		}
		if hashLess(it.hashes[it.pos], it.seek) {
			continue
		}
		return true
	}
}

func (it *diffStorageIterator) Hash() types.Hash {
	if it.pos < 0 || it.pos >= len(it.hashes) {
		return types.Hash{}
	}
	return it.hashes[it.pos]
}

func (it *diffStorageIterator) Slot() []byte {
	if it.pos < 0 || it.pos >= len(it.hashes) {
		return nil
	}
	it.layer.lock.RLock()
	defer it.layer.lock.RUnlock()
	slots := it.layer.storageData[it.accountHash]
	if slots == nil {
		return nil
	}
	return slots[it.hashes[it.pos]]
}

func (it *diffStorageIterator) Release() {}

// --- diskLayer account iterator ---

type diskAccountIterator struct {
	hashes []types.Hash
	data   map[types.Hash][]byte
	pos    int
}

func (it *diskAccountIterator) Next() bool {
	if it.hashes == nil {
		return false
	}
	it.pos++
	return it.pos < len(it.hashes)
}

func (it *diskAccountIterator) Hash() types.Hash {
	if it.hashes == nil || it.pos < 0 || it.pos >= len(it.hashes) {
		return types.Hash{}
	}
	return it.hashes[it.pos]
}

func (it *diskAccountIterator) Account() []byte {
	if it.hashes == nil || it.pos < 0 || it.pos >= len(it.hashes) {
		return nil
	}
	return it.data[it.hashes[it.pos]]
}

func (it *diskAccountIterator) Release() {}

// --- diskLayer storage iterator ---

type diskStorageIterator struct {
	hashes []types.Hash
	data   map[types.Hash][]byte
	pos    int
}

func (it *diskStorageIterator) Next() bool {
	if it.hashes == nil {
		return false
	}
	it.pos++
	return it.pos < len(it.hashes)
}

func (it *diskStorageIterator) Hash() types.Hash {
	if it.hashes == nil || it.pos < 0 || it.pos >= len(it.hashes) {
		return types.Hash{}
	}
	return it.hashes[it.pos]
}

func (it *diskStorageIterator) Slot() []byte {
	if it.hashes == nil || it.pos < 0 || it.pos >= len(it.hashes) {
		return nil
	}
	return it.data[it.hashes[it.pos]]
}

func (it *diskStorageIterator) Release() {}
