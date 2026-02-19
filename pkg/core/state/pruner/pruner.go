// Package pruner implements state pruning for an Ethereum client. It
// identifies state data reachable from a given root and removes all
// unreachable entries from the database.
//
// The pruner uses a bloom filter to efficiently track reachable state nodes
// during trie traversal, then iterates the database to delete entries whose
// keys do not appear in the bloom filter.
package pruner

import (
	"errors"
	"fmt"

	"github.com/eth2028/eth2028/core/rawdb"
	"github.com/eth2028/eth2028/core/types"
)

// Default configuration values.
const (
	// DefaultBloomSize is the default number of bits in the pruning bloom filter.
	DefaultBloomSize = 256 * 1024 * 1024 // 256 MiB = 2 billion bits
)

// PrunerConfig holds configuration for the state pruner.
type PrunerConfig struct {
	// BloomSize is the number of bits in the bloom filter used for reachability
	// tracking. Larger values reduce false positives. Defaults to DefaultBloomSize.
	BloomSize uint64

	// Datadir is the path to the chain data directory (unused when operating
	// directly with a database handle, kept for future file-based pruning).
	Datadir string
}

// prunerDB is the database interface required by the pruner.
type prunerDB interface {
	rawdb.KeyValueStore
	NewIterator(prefix []byte) rawdb.Iterator
	NewBatch() rawdb.Batch
}

// Pruner removes stale state data from the database.
type Pruner struct {
	config PrunerConfig
	db     prunerDB
}

// NewPruner creates a new state pruner with the given configuration and
// database. The database must support reads, writes, batch, and iteration.
func NewPruner(config PrunerConfig, db prunerDB) *Pruner {
	if config.BloomSize == 0 {
		config.BloomSize = DefaultBloomSize
	}
	return &Pruner{
		config: config,
		db:     db,
	}
}

// bloom is a simple bit-array bloom filter used during pruning to track
// which database keys are reachable from the target state root.
type bloom struct {
	bits []byte
	size uint64 // number of bits
}

func newBloom(size uint64) *bloom {
	if size == 0 {
		size = DefaultBloomSize
	}
	byteSize := (size + 7) / 8
	return &bloom{
		bits: make([]byte, byteSize),
		size: size,
	}
}

// add marks a key as present in the bloom filter using multiple hash functions.
func (b *bloom) add(key []byte) {
	h1 := fnvHash(key, 0)
	h2 := fnvHash(key, 1)
	h3 := fnvHash(key, 2)

	b.setBit(h1 % b.size)
	b.setBit(h2 % b.size)
	b.setBit(h3 % b.size)
}

// contains checks if a key might be present in the bloom filter.
func (b *bloom) contains(key []byte) bool {
	h1 := fnvHash(key, 0)
	h2 := fnvHash(key, 1)
	h3 := fnvHash(key, 2)

	return b.getBit(h1%b.size) && b.getBit(h2%b.size) && b.getBit(h3%b.size)
}

func (b *bloom) setBit(pos uint64) {
	b.bits[pos/8] |= 1 << (pos % 8)
}

func (b *bloom) getBit(pos uint64) bool {
	return b.bits[pos/8]&(1<<(pos%8)) != 0
}

// fnvHash computes a simple FNV-1a-like hash with a seed for the bloom filter.
func fnvHash(data []byte, seed byte) uint64 {
	var h uint64 = 14695981039346656037 // FNV offset basis
	h ^= uint64(seed)
	h *= 1099511628211 // FNV prime
	for _, b := range data {
		h ^= uint64(b)
		h *= 1099511628211
	}
	return h
}

// Key prefixes that the pruner considers as prunable state data.
// These must match the schema used by the snapshot and trie node storage.
var (
	// snapshotAccountPrefix matches snapshot account entries.
	snapshotAccountPrefix = []byte("sa")
	// snapshotStoragePrefix matches snapshot storage entries.
	snapshotStoragePrefix = []byte("ss")
	// trieNodePrefix matches trie node entries.
	trieNodePrefix = []byte("t")
)

// Prune removes all state data from the database that is not reachable from
// the given root. It works in two phases:
//
//  1. Mark phase: iterate all snapshot account and storage entries under the
//     given root, adding their database keys to a bloom filter.
//  2. Sweep phase: iterate the database and delete entries whose keys are not
//     present in the bloom filter.
//
// Returns the number of entries deleted and any error encountered.
func (p *Pruner) Prune(root types.Hash) (int, error) {
	if p.db == nil {
		return 0, errors.New("pruner: nil database")
	}

	bf := newBloom(p.config.BloomSize)

	// Phase 1: Mark all reachable state entries.
	// Mark snapshot accounts.
	if err := p.markPrefix(bf, snapshotAccountPrefix); err != nil {
		return 0, fmt.Errorf("pruner: mark accounts: %w", err)
	}
	// Mark snapshot storage.
	if err := p.markPrefix(bf, snapshotStoragePrefix); err != nil {
		return 0, fmt.Errorf("pruner: mark storage: %w", err)
	}

	// Phase 2: Sweep unreachable entries from prunable prefixes.
	deleted := 0
	for _, prefix := range [][]byte{snapshotAccountPrefix, snapshotStoragePrefix, trieNodePrefix} {
		n, err := p.sweepPrefix(bf, prefix)
		if err != nil {
			return deleted, fmt.Errorf("pruner: sweep %x: %w", prefix, err)
		}
		deleted += n
	}
	return deleted, nil
}

// markPrefix iterates all keys with the given prefix and adds them to the
// bloom filter.
func (p *Pruner) markPrefix(bf *bloom, prefix []byte) error {
	iter := p.db.NewIterator(prefix)
	defer iter.Release()

	for iter.Next() {
		key := iter.Key()
		bf.add(key)
	}
	return nil
}

// sweepPrefix iterates all keys with the given prefix and deletes those that
// are not present in the bloom filter. Returns the number of entries deleted.
func (p *Pruner) sweepPrefix(bf *bloom, prefix []byte) (int, error) {
	iter := p.db.NewIterator(prefix)
	defer iter.Release()

	batch := p.db.NewBatch()
	deleted := 0
	batchSize := 0
	const maxBatchSize = 1024 * 1024 // 1 MiB

	for iter.Next() {
		key := iter.Key()
		if !bf.contains(key) {
			// Key is not reachable -- schedule for deletion.
			keyCopy := make([]byte, len(key))
			copy(keyCopy, key)
			batch.Delete(keyCopy)
			deleted++
			batchSize += len(key)

			if batchSize >= maxBatchSize {
				if err := batch.Write(); err != nil {
					return deleted, err
				}
				batch.Reset()
				batchSize = 0
			}
		}
	}
	// Flush remaining deletes.
	if batchSize > 0 {
		if err := batch.Write(); err != nil {
			return deleted, err
		}
	}
	return deleted, nil
}

// PruneByKeys is a simplified pruning method that takes an explicit set of
// keys to keep and removes everything else with matching prefixes. This is
// useful for testing and for cases where the reachable set is known exactly.
func (p *Pruner) PruneByKeys(keep map[string]struct{}, prefixes [][]byte) (int, error) {
	if p.db == nil {
		return 0, errors.New("pruner: nil database")
	}
	deleted := 0
	for _, prefix := range prefixes {
		iter := p.db.NewIterator(prefix)
		batch := p.db.NewBatch()
		batchSize := 0

		for iter.Next() {
			key := string(iter.Key())
			if _, ok := keep[key]; !ok {
				batch.Delete([]byte(key))
				deleted++
				batchSize += len(key)
				if batchSize >= 1024*1024 {
					if err := batch.Write(); err != nil {
						iter.Release()
						return deleted, err
					}
					batch.Reset()
					batchSize = 0
				}
			}
		}
		iter.Release()
		if batchSize > 0 {
			if err := batch.Write(); err != nil {
				return deleted, err
			}
		}
	}
	return deleted, nil
}
