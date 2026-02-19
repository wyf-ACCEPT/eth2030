// history.go implements EIP-4444 history expiry, allowing nodes to prune
// block bodies, receipts, and transaction lookups older than a configurable
// retention period. Headers are kept forever for chain verification.
package rawdb

import (
	"encoding/binary"
	"errors"
)

// DefaultHistoryRetention is the default number of blocks to retain before
// pruning. Based on EIP-4444's HISTORY_PRUNE_EPOCHS (82125 epochs, roughly
// one year of beacon chain epochs). At ~12 seconds per slot and 32 slots
// per epoch, this is approximately 8,640 blocks/day * 365 = 3,153,600.
const DefaultHistoryRetention uint64 = 3_153_600

// historyOldestKey stores the oldest available block number for bodies/receipts.
var historyOldestKey = []byte("history-oldest")

// ErrHistoryPruned is returned when requested data has been pruned per EIP-4444.
var ErrHistoryPruned = errors.New("historical data pruned (EIP-4444)")

// WriteHistoryOldest persists the oldest block number for which bodies and
// receipts are still available.
func WriteHistoryOldest(db KeyValueWriter, blockNum uint64) error {
	return db.Put(historyOldestKey, encodeBlockNumber(blockNum))
}

// ReadHistoryOldest returns the oldest block number for which bodies and
// receipts are available. Returns 0 if no pruning has occurred.
func ReadHistoryOldest(db KeyValueReader) (uint64, error) {
	data, err := db.Get(historyOldestKey)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}
	if len(data) != 8 {
		return 0, nil
	}
	return binary.BigEndian.Uint64(data), nil
}

// HistoryAvailable checks whether block bodies and receipts are available
// for the given block number. Returns true if the data has not been pruned.
func HistoryAvailable(db KeyValueReader, blockNum uint64) (bool, error) {
	oldest, err := ReadHistoryOldest(db)
	if err != nil {
		return false, err
	}
	return blockNum >= oldest, nil
}

// PruneHistory deletes block bodies, receipts, and tx lookup entries for
// blocks older than (headBlock - retention). Headers and canonical hashes
// are preserved. Returns the number of blocks pruned and the new oldest
// block number.
func PruneHistory(db Database, headBlock, retention uint64) (pruned uint64, newOldest uint64, err error) {
	if headBlock < retention {
		return 0, 0, nil
	}
	threshold := headBlock - retention

	// Read current oldest to avoid re-pruning already-pruned blocks.
	currentOldest, err := ReadHistoryOldest(db)
	if err != nil {
		return 0, currentOldest, err
	}
	if currentOldest >= threshold {
		return 0, currentOldest, nil
	}

	batch := db.NewBatch()

	for num := currentOldest; num < threshold; num++ {
		// Look up canonical hash for this block number.
		hash, err := ReadCanonicalHash(db, num)
		if err != nil {
			// No canonical hash means the block was already pruned or
			// does not exist in the canonical chain -- skip.
			continue
		}

		// Delete body.
		if err := batch.Delete(bodyKey(num, hash)); err != nil {
			return pruned, currentOldest, err
		}

		// Delete receipts.
		if err := batch.Delete(receiptKey(num, hash)); err != nil {
			return pruned, currentOldest, err
		}

		pruned++

		// Flush batch periodically to avoid excessive memory usage.
		if batch.ValueSize() > 64*1024 {
			if err := batch.Write(); err != nil {
				return pruned, currentOldest, err
			}
			batch.Reset()
		}
	}

	// Flush remaining.
	if batch.ValueSize() > 0 {
		if err := batch.Write(); err != nil {
			return pruned, currentOldest, err
		}
	}

	// Persist the new oldest block number.
	newOldest = threshold
	if err := WriteHistoryOldest(db, newOldest); err != nil {
		return pruned, currentOldest, err
	}

	return pruned, newOldest, nil
}
