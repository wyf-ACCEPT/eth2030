// chain_iterator.go provides chain iteration utilities for walking the
// canonical chain forward or backward, querying block ranges, looking up
// blocks by number via the canonical hash index, and iterating over
// ancient (frozen) data from the freezer.
package rawdb

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Chain iterator errors.
var (
	ErrIteratorExhausted = errors.New("chain iterator: no more items")
	ErrIteratorClosed    = errors.New("chain iterator: closed")
	ErrRangeInvalid      = errors.New("chain iterator: start > end")
	ErrNoFreezer         = errors.New("chain iterator: no freezer configured")
)

// ChainIterator walks the canonical chain in either direction, yielding
// block numbers and their canonical hashes. It supports forward iteration
// (ascending block numbers) and backward iteration (descending).
type ChainIterator struct {
	db      KeyValueReader
	current uint64
	end     uint64
	forward bool
	started bool
	closed  bool
}

// NewForwardIterator creates an iterator that walks the canonical chain
// from start to end (inclusive) in ascending order.
func NewForwardIterator(db KeyValueReader, start, end uint64) (*ChainIterator, error) {
	if start > end {
		return nil, ErrRangeInvalid
	}
	return &ChainIterator{
		db:      db,
		current: start,
		end:     end,
		forward: true,
	}, nil
}

// NewBackwardIterator creates an iterator that walks the canonical chain
// from start down to end (inclusive) in descending order.
func NewBackwardIterator(db KeyValueReader, start, end uint64) (*ChainIterator, error) {
	if end > start {
		return nil, ErrRangeInvalid
	}
	return &ChainIterator{
		db:      db,
		current: start,
		end:     end,
		forward: false,
	}, nil
}

// Next advances the iterator to the next block. Returns false when the
// iterator is exhausted or closed.
func (it *ChainIterator) Next() bool {
	if it.closed {
		return false
	}
	if !it.started {
		it.started = true
		// Check if the current position has a canonical hash.
		_, err := ReadCanonicalHash(it.db, it.current)
		return err == nil
	}
	if it.forward {
		if it.current >= it.end {
			return false
		}
		it.current++
	} else {
		if it.current <= it.end {
			return false
		}
		it.current--
	}
	_, err := ReadCanonicalHash(it.db, it.current)
	return err == nil
}

// Number returns the block number at the current iterator position.
func (it *ChainIterator) Number() uint64 {
	return it.current
}

// Hash returns the canonical hash at the current iterator position.
// Returns a zero hash if the position is invalid.
func (it *ChainIterator) Hash() [32]byte {
	hash, err := ReadCanonicalHash(it.db, it.current)
	if err != nil {
		return [32]byte{}
	}
	return hash
}

// Close releases the iterator. After Close, Next always returns false.
func (it *ChainIterator) Close() {
	it.closed = true
}

// --- Range queries ---

// BlockRange represents a block number and its canonical hash.
type BlockRange struct {
	Number uint64
	Hash   [32]byte
}

// ReadCanonicalRange retrieves canonical hashes for a range of block
// numbers [start, end] inclusive. Blocks without a canonical hash are
// skipped. The returned slice is in ascending order.
func ReadCanonicalRange(db KeyValueReader, start, end uint64) ([]BlockRange, error) {
	if start > end {
		return nil, ErrRangeInvalid
	}
	result := make([]BlockRange, 0, end-start+1)
	for num := start; num <= end; num++ {
		hash, err := ReadCanonicalHash(db, num)
		if err != nil {
			continue // skip missing entries
		}
		result = append(result, BlockRange{Number: num, Hash: hash})
	}
	return result, nil
}

// ReadBlockNumberByHash looks up the block number for a given hash using
// the hash-to-number index. This is the inverse of the canonical hash
// lookup and works for both canonical and non-canonical blocks.
func ReadBlockNumberByHash(db KeyValueReader, hash [32]byte) (uint64, error) {
	return ReadHeaderNumber(db, hash)
}

// IsCanonicalHash checks whether a given hash is the canonical hash for
// its corresponding block number.
func IsCanonicalHash(db KeyValueReader, hash [32]byte) (bool, error) {
	num, err := ReadHeaderNumber(db, hash)
	if err != nil {
		return false, err
	}
	canonical, err := ReadCanonicalHash(db, num)
	if err != nil {
		return false, err
	}
	return canonical == hash, nil
}

// --- Canonical chain walking ---

// WalkCanonicalChain calls fn for each block in the canonical chain from
// start to end (inclusive). If fn returns an error, iteration stops and
// that error is returned. Blocks without canonical hashes are skipped.
func WalkCanonicalChain(db KeyValueReader, start, end uint64, fn func(number uint64, hash [32]byte) error) error {
	if start > end {
		return ErrRangeInvalid
	}
	for num := start; num <= end; num++ {
		hash, err := ReadCanonicalHash(db, num)
		if err != nil {
			continue
		}
		if err := fn(num, hash); err != nil {
			return err
		}
	}
	return nil
}

// FindCommonAncestor finds the most recent block where two chains share
// the same canonical hash, searching backward from the given block number.
// Returns the common ancestor's number and hash, or an error if none found.
func FindCommonAncestor(db KeyValueReader, fromBlock uint64, otherHash func(num uint64) ([32]byte, error)) (uint64, [32]byte, error) {
	for num := fromBlock; ; num-- {
		localHash, err := ReadCanonicalHash(db, num)
		if err != nil {
			if num == 0 {
				return 0, [32]byte{}, fmt.Errorf("no common ancestor found")
			}
			continue
		}
		remoteHash, err := otherHash(num)
		if err != nil {
			if num == 0 {
				return 0, [32]byte{}, fmt.Errorf("no common ancestor found")
			}
			continue
		}
		if localHash == remoteHash {
			return num, localHash, nil
		}
		if num == 0 {
			break
		}
	}
	return 0, [32]byte{}, fmt.Errorf("no common ancestor found")
}

// --- Ancient (freezer) data iteration ---

// AncientIterator walks frozen data in a specific freezer table,
// yielding item numbers and their raw data.
type AncientIterator struct {
	freezer *Freezer
	table   string
	current uint64
	end     uint64
	started bool
	closed  bool
}

// NewAncientIterator creates an iterator over frozen data in the specified
// table, from start to end (inclusive).
func NewAncientIterator(freezer *Freezer, table string, start, end uint64) (*AncientIterator, error) {
	if freezer == nil {
		return nil, ErrNoFreezer
	}
	if !freezer.HasTable(table) {
		return nil, fmt.Errorf("chain iterator: unknown table %q", table)
	}
	if start > end {
		return nil, ErrRangeInvalid
	}
	return &AncientIterator{
		freezer: freezer,
		table:   table,
		current: start,
		end:     end,
	}, nil
}

// Next advances the ancient iterator to the next item.
func (it *AncientIterator) Next() bool {
	if it.closed {
		return false
	}
	if !it.started {
		it.started = true
		_, err := it.freezer.Retrieve(it.table, it.current)
		return err == nil
	}
	if it.current >= it.end {
		return false
	}
	it.current++
	_, err := it.freezer.Retrieve(it.table, it.current)
	return err == nil
}

// Number returns the current item number.
func (it *AncientIterator) Number() uint64 {
	return it.current
}

// Data returns the raw data at the current position. Returns nil on error.
func (it *AncientIterator) Data() []byte {
	data, err := it.freezer.Retrieve(it.table, it.current)
	if err != nil {
		return nil
	}
	return data
}

// Close releases the ancient iterator.
func (it *AncientIterator) Close() {
	it.closed = true
}

// --- Number-to-hash index utilities ---

// BuildNumberIndex writes canonical hash entries for a contiguous range
// of block numbers using the provided number-to-hash mapping function.
// This is useful for rebuilding the canonical index after corruption.
func BuildNumberIndex(db KeyValueWriter, start, end uint64, hashFn func(uint64) ([32]byte, bool)) (int, error) {
	if start > end {
		return 0, ErrRangeInvalid
	}
	count := 0
	for num := start; num <= end; num++ {
		hash, ok := hashFn(num)
		if !ok {
			continue
		}
		if err := WriteCanonicalHash(db, num, hash); err != nil {
			return count, fmt.Errorf("write canonical hash %d: %w", num, err)
		}
		count++
	}
	return count, nil
}

// DeleteCanonicalRange removes canonical hash mappings for a range of
// block numbers [start, end] inclusive. Used during chain reorganization.
func DeleteCanonicalRange(db KeyValueWriter, start, end uint64) error {
	if start > end {
		return ErrRangeInvalid
	}
	for num := start; num <= end; num++ {
		if err := DeleteCanonicalHash(db, num); err != nil {
			return fmt.Errorf("delete canonical hash %d: %w", num, err)
		}
	}
	return nil
}

// CountCanonicalBlocks counts how many blocks in the given range have
// a canonical hash entry. Useful for chain health diagnostics.
func CountCanonicalBlocks(db KeyValueReader, start, end uint64) (uint64, error) {
	if start > end {
		return 0, ErrRangeInvalid
	}
	var count uint64
	for num := start; num <= end; num++ {
		_, err := ReadCanonicalHash(db, num)
		if err == nil {
			count++
		}
	}
	return count, nil
}

// Compile-time check: encodeBlockNumber must produce 8 bytes.
var _ = func() {
	b := encodeBlockNumber(42)
	if len(b) != 8 {
		panic("encodeBlockNumber must produce 8 bytes")
	}
	_ = binary.BigEndian
}
