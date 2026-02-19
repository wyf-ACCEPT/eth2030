package types

import (
	"crypto/sha256"
	"encoding/binary"
)

// LogIndexEntry maps a single log to its position within a block and globally.
type LogIndexEntry struct {
	BlockNumber  uint64
	TxIndex      uint16
	LogIndex     uint16
	GlobalIndex  uint64
}

// BlockLogIndex holds the log index for an entire block.
type BlockLogIndex struct {
	Entries         []LogIndexEntry
	BlockNumber     uint64
	FirstGlobalIndex uint64
}

// FilterRow is a bitmap row in the log filter index, used for efficient
// log queries across blocks. Each bit represents a block position.
type FilterRow [32]byte

// FilterMap holds multiple filter rows keyed by topic hash prefix,
// enabling efficient log querying by topic or address.
type FilterMap struct {
	Rows       map[uint16]FilterRow
	StartBlock uint64
	EndBlock   uint64
}

// NewFilterMap creates a new empty FilterMap for the given block range.
func NewFilterMap(startBlock, endBlock uint64) *FilterMap {
	return &FilterMap{
		Rows:       make(map[uint16]FilterRow),
		StartBlock: startBlock,
		EndBlock:   endBlock,
	}
}

// Set marks a bit position in the filter row for the given key.
func (fm *FilterMap) Set(key uint16, bitPos uint) {
	row := fm.Rows[key]
	if bitPos < 256 {
		row[bitPos/8] |= 1 << (bitPos % 8)
	}
	fm.Rows[key] = row
}

// Test checks whether a bit position is set in the filter row for the given key.
func (fm *FilterMap) Test(key uint16, bitPos uint) bool {
	row, ok := fm.Rows[key]
	if !ok {
		return false
	}
	if bitPos >= 256 {
		return false
	}
	return row[bitPos/8]&(1<<(bitPos%8)) != 0
}

// BuildLogIndex constructs a BlockLogIndex from a set of receipts,
// assigning sequential global indices starting from prevGlobalIdx.
func BuildLogIndex(receipts []*Receipt, blockNum uint64, prevGlobalIdx uint64) *BlockLogIndex {
	idx := &BlockLogIndex{
		BlockNumber:      blockNum,
		FirstGlobalIndex: prevGlobalIdx,
	}

	globalIdx := prevGlobalIdx
	for txIdx, receipt := range receipts {
		for logIdx := range receipt.Logs {
			entry := LogIndexEntry{
				BlockNumber: blockNum,
				TxIndex:     uint16(txIdx),
				LogIndex:    uint16(logIdx),
				GlobalIndex: globalIdx,
			}
			idx.Entries = append(idx.Entries, entry)
			globalIdx++
		}
	}
	return idx
}

// topicKey returns the filter map key (uint16) for a topic hash.
func topicKey(topic Hash) uint16 {
	return binary.BigEndian.Uint16(topic[:2])
}

// addressKey returns the filter map key (uint16) for an address.
func addressKey(addr Address) uint16 {
	return binary.BigEndian.Uint16(addr[:2])
}

// QueryLogsByTopics returns logs matching all provided topic sets.
// topics is a list of topic positions; each position is a list of acceptable
// topic values (OR within position, AND across positions).
func QueryLogsByTopics(index *BlockLogIndex, receipts []*Receipt, topics [][]Hash) []*Log {
	if len(topics) == 0 {
		return nil
	}

	var results []*Log
	for _, entry := range index.Entries {
		if int(entry.TxIndex) >= len(receipts) {
			continue
		}
		receipt := receipts[entry.TxIndex]
		if int(entry.LogIndex) >= len(receipt.Logs) {
			continue
		}
		log := receipt.Logs[entry.LogIndex]
		if matchTopics(log, topics) {
			results = append(results, log)
		}
	}
	return results
}

// matchTopics checks if a log matches the given topic filter.
func matchTopics(log *Log, topics [][]Hash) bool {
	for i, topicSet := range topics {
		if len(topicSet) == 0 {
			// Empty set matches any value at this position.
			continue
		}
		if i >= len(log.Topics) {
			return false
		}
		found := false
		for _, t := range topicSet {
			if log.Topics[i] == t {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// QueryLogsByAddress returns all logs emitted by any of the given addresses.
func QueryLogsByAddress(index *BlockLogIndex, receipts []*Receipt, addrs []Address) []*Log {
	if len(addrs) == 0 {
		return nil
	}

	addrSet := make(map[Address]struct{}, len(addrs))
	for _, a := range addrs {
		addrSet[a] = struct{}{}
	}

	var results []*Log
	for _, entry := range index.Entries {
		if int(entry.TxIndex) >= len(receipts) {
			continue
		}
		receipt := receipts[entry.TxIndex]
		if int(entry.LogIndex) >= len(receipt.Logs) {
			continue
		}
		log := receipt.Logs[entry.LogIndex]
		if _, ok := addrSet[log.Address]; ok {
			results = append(results, log)
		}
	}
	return results
}

// ComputeLogIndexRoot computes a SHA-256 merkle root over the log index entries.
func ComputeLogIndexRoot(index *BlockLogIndex) Hash {
	if len(index.Entries) == 0 {
		return Hash{}
	}

	// Serialize each entry as a leaf.
	leaves := make([][32]byte, len(index.Entries))
	for i, entry := range index.Entries {
		var buf [20]byte
		binary.LittleEndian.PutUint64(buf[0:8], entry.BlockNumber)
		binary.LittleEndian.PutUint16(buf[8:10], entry.TxIndex)
		binary.LittleEndian.PutUint16(buf[10:12], entry.LogIndex)
		binary.LittleEndian.PutUint64(buf[12:20], entry.GlobalIndex)
		leaves[i] = sha256.Sum256(buf[:])
	}

	// Merkleize: iteratively hash pairs until one root remains.
	layer := leaves
	for len(layer) > 1 {
		var next [][32]byte
		for i := 0; i < len(layer); i += 2 {
			if i+1 < len(layer) {
				var combined [64]byte
				copy(combined[:32], layer[i][:])
				copy(combined[32:], layer[i+1][:])
				next = append(next, sha256.Sum256(combined[:]))
			} else {
				// Odd element: hash with zero.
				var combined [64]byte
				copy(combined[:32], layer[i][:])
				next = append(next, sha256.Sum256(combined[:]))
			}
		}
		layer = next
	}

	var h Hash
	copy(h[:], layer[0][:])
	return h
}
