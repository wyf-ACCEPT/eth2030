package types

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"sync"

	"golang.org/x/crypto/sha3"
)

// Receipt trie errors.
var (
	ErrReceiptTrieFull       = errors.New("receipt trie: max receipts reached")
	ErrReceiptTrieNilReceipt = errors.New("receipt trie: nil receipt")
	ErrReceiptTrieNotFound   = errors.New("receipt trie: receipt not found")
	ErrReceiptTrieCompact    = errors.New("receipt trie: compact decode failed")
)

// ReceiptTrieConfig holds configuration for the receipt trie.
type ReceiptTrieConfig struct {
	UseCompactEncoding  bool // use compact (non-RLP) encoding for receipts
	MaxReceiptsPerBlock int  // maximum receipts per block (0 = unlimited)
	PruneDepth          int  // prune threshold for old entries
}

// DefaultReceiptTrieConfig returns sensible defaults.
func DefaultReceiptTrieConfig() ReceiptTrieConfig {
	return ReceiptTrieConfig{
		UseCompactEncoding:  true,
		MaxReceiptsPerBlock: 0, // unlimited
		PruneDepth:          1024,
	}
}

// receiptTrieEntry stores a receipt with its encoded form for the trie.
type receiptTrieEntry struct {
	receipt *Receipt
	encoded []byte
}

// ReceiptTrie stores receipts in a trie-like structure with compact encoding.
// It provides efficient insertion, retrieval, and Merkle root computation.
// It is safe for concurrent use.
type ReceiptTrie struct {
	mu      sync.RWMutex
	config  ReceiptTrieConfig
	entries map[uint64]*receiptTrieEntry // txIndex -> entry
	order   []uint64                     // insertion order for determinism
}

// NewReceiptTrie creates a new ReceiptTrie with the given config.
func NewReceiptTrie(config ReceiptTrieConfig) *ReceiptTrie {
	return &ReceiptTrie{
		config:  config,
		entries: make(map[uint64]*receiptTrieEntry),
	}
}

// Insert adds a receipt to the trie at the given transaction index.
// If a receipt already exists at that index, it is replaced.
func (rt *ReceiptTrie) Insert(txIndex uint64, receipt *Receipt) error {
	if receipt == nil {
		return ErrReceiptTrieNilReceipt
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	if rt.config.MaxReceiptsPerBlock > 0 && len(rt.entries) >= rt.config.MaxReceiptsPerBlock {
		if _, exists := rt.entries[txIndex]; !exists {
			return fmt.Errorf("%w: limit %d", ErrReceiptTrieFull, rt.config.MaxReceiptsPerBlock)
		}
	}

	var encoded []byte
	if rt.config.UseCompactEncoding {
		encoded = ReceiptTrieCompactEncode(receipt)
	} else {
		var err error
		encoded, err = receipt.EncodeRLP()
		if err != nil {
			return fmt.Errorf("receipt trie: encode error: %w", err)
		}
	}

	if _, exists := rt.entries[txIndex]; !exists {
		rt.order = append(rt.order, txIndex)
	}

	rt.entries[txIndex] = &receiptTrieEntry{
		receipt: receipt,
		encoded: encoded,
	}

	return nil
}

// Get retrieves a receipt by transaction index.
func (rt *ReceiptTrie) Get(txIndex uint64) (*Receipt, error) {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	entry, ok := rt.entries[txIndex]
	if !ok {
		return nil, fmt.Errorf("%w: index %d", ErrReceiptTrieNotFound, txIndex)
	}
	return entry.receipt, nil
}

// ComputeRoot computes the Merkle root of the receipt trie.
// The root is computed as a hash tree: leaves are the Keccak256 hashes of
// each receipt's encoded form, keyed by their transaction index in sorted
// order. If the trie is empty, it returns EmptyRootHash.
func (rt *ReceiptTrie) ComputeRoot() Hash {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	if len(rt.entries) == 0 {
		return EmptyRootHash
	}

	// Collect sorted indices for deterministic ordering.
	indices := make([]uint64, 0, len(rt.entries))
	for idx := range rt.entries {
		indices = append(indices, idx)
	}
	sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })

	// Build leaf hashes: hash(index || encoded_receipt).
	leaves := make([][]byte, len(indices))
	for i, idx := range indices {
		entry := rt.entries[idx]
		idxBuf := make([]byte, 8)
		binary.BigEndian.PutUint64(idxBuf, idx)
		d := sha3.NewLegacyKeccak256()
		d.Write(idxBuf)
		d.Write(entry.encoded)
		leaves[i] = d.Sum(nil)
	}

	// Build binary Merkle tree from leaves.
	root := merkleRoot(leaves)
	var h Hash
	copy(h[:], root)
	return h
}

// merkleRoot computes a binary Merkle root from a list of leaf hashes.
// If there is a single leaf, its hash is the root. Otherwise, adjacent
// pairs are hashed together, and odd leaves are promoted.
func merkleRoot(leaves [][]byte) []byte {
	if len(leaves) == 0 {
		return EmptyRootHash[:]
	}
	if len(leaves) == 1 {
		return leaves[0]
	}

	for len(leaves) > 1 {
		var next [][]byte
		for i := 0; i < len(leaves); i += 2 {
			if i+1 < len(leaves) {
				d := sha3.NewLegacyKeccak256()
				d.Write(leaves[i])
				d.Write(leaves[i+1])
				next = append(next, d.Sum(nil))
			} else {
				// Odd leaf: promote it.
				next = append(next, leaves[i])
			}
		}
		leaves = next
	}
	return leaves[0]
}

// Size returns the number of receipts in the trie.
func (rt *ReceiptTrie) Size() int {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return len(rt.entries)
}

// ReceiptTrieCompactEncode encodes a receipt using a compact binary format.
// Format: [type:1][status:1][cumGas:8][gasUsed:8][logsCount:4][logs...]
// Each log: [addrLen:1][addr:20][topicCount:2][topics:32*N][dataLen:4][data...]
func ReceiptTrieCompactEncode(receipt *Receipt) []byte {
	if receipt == nil {
		return nil
	}

	// Estimate buffer size.
	size := 1 + 1 + 8 + 8 + 4 // header
	for _, log := range receipt.Logs {
		size += 1 + AddressLength + 2 + len(log.Topics)*HashLength + 4 + len(log.Data)
	}

	buf := make([]byte, 0, size)

	// Type byte.
	buf = append(buf, receipt.Type)

	// Status byte (0 or 1).
	buf = append(buf, byte(receipt.Status))

	// CumulativeGasUsed (8 bytes, big-endian).
	var tmp [8]byte
	binary.BigEndian.PutUint64(tmp[:], receipt.CumulativeGasUsed)
	buf = append(buf, tmp[:]...)

	// GasUsed (8 bytes, big-endian).
	binary.BigEndian.PutUint64(tmp[:], receipt.GasUsed)
	buf = append(buf, tmp[:]...)

	// Log count (4 bytes, big-endian).
	var logCount [4]byte
	binary.BigEndian.PutUint32(logCount[:], uint32(len(receipt.Logs)))
	buf = append(buf, logCount[:]...)

	// Encode each log.
	for _, log := range receipt.Logs {
		// Address length (always 20, but encode for extensibility).
		buf = append(buf, byte(AddressLength))
		buf = append(buf, log.Address[:]...)

		// Topic count (2 bytes).
		var tc [2]byte
		binary.BigEndian.PutUint16(tc[:], uint16(len(log.Topics)))
		buf = append(buf, tc[:]...)

		// Topics.
		for _, topic := range log.Topics {
			buf = append(buf, topic[:]...)
		}

		// Data length (4 bytes).
		var dl [4]byte
		binary.BigEndian.PutUint32(dl[:], uint32(len(log.Data)))
		buf = append(buf, dl[:]...)

		// Data.
		buf = append(buf, log.Data...)
	}

	return buf
}

// ReceiptTrieCompactDecode decodes a receipt from compact binary format.
func ReceiptTrieCompactDecode(data []byte) (*Receipt, error) {
	if len(data) < 22 { // minimum: 1+1+8+8+4
		return nil, fmt.Errorf("%w: data too short (%d bytes)", ErrReceiptTrieCompact, len(data))
	}

	r := &Receipt{}
	pos := 0

	// Type.
	r.Type = data[pos]
	pos++

	// Status.
	r.Status = uint64(data[pos])
	pos++

	// CumulativeGasUsed.
	if pos+8 > len(data) {
		return nil, fmt.Errorf("%w: truncated cumulative gas", ErrReceiptTrieCompact)
	}
	r.CumulativeGasUsed = binary.BigEndian.Uint64(data[pos : pos+8])
	pos += 8

	// GasUsed.
	if pos+8 > len(data) {
		return nil, fmt.Errorf("%w: truncated gas used", ErrReceiptTrieCompact)
	}
	r.GasUsed = binary.BigEndian.Uint64(data[pos : pos+8])
	pos += 8

	// Log count.
	if pos+4 > len(data) {
		return nil, fmt.Errorf("%w: truncated log count", ErrReceiptTrieCompact)
	}
	logCount := binary.BigEndian.Uint32(data[pos : pos+4])
	pos += 4

	// Decode logs.
	r.Logs = make([]*Log, 0, logCount)
	for i := uint32(0); i < logCount; i++ {
		log, n, err := decodeCompactLog(data[pos:])
		if err != nil {
			return nil, fmt.Errorf("%w: log %d: %v", ErrReceiptTrieCompact, i, err)
		}
		r.Logs = append(r.Logs, log)
		pos += n
	}

	return r, nil
}

// decodeCompactLog decodes a single log from compact format.
// Returns the log, the number of bytes consumed, and any error.
func decodeCompactLog(data []byte) (*Log, int, error) {
	if len(data) < 1 {
		return nil, 0, errors.New("empty log data")
	}
	pos := 0

	// Address length.
	addrLen := int(data[pos])
	pos++

	if pos+addrLen > len(data) {
		return nil, 0, errors.New("truncated address")
	}
	log := &Log{}
	copy(log.Address[:], data[pos:pos+addrLen])
	pos += addrLen

	// Topic count.
	if pos+2 > len(data) {
		return nil, 0, errors.New("truncated topic count")
	}
	topicCount := binary.BigEndian.Uint16(data[pos : pos+2])
	pos += 2

	// Topics.
	log.Topics = make([]Hash, topicCount)
	for j := uint16(0); j < topicCount; j++ {
		if pos+HashLength > len(data) {
			return nil, 0, errors.New("truncated topic")
		}
		copy(log.Topics[j][:], data[pos:pos+HashLength])
		pos += HashLength
	}

	// Data length.
	if pos+4 > len(data) {
		return nil, 0, errors.New("truncated data length")
	}
	dataLen := binary.BigEndian.Uint32(data[pos : pos+4])
	pos += 4

	// Data.
	if pos+int(dataLen) > len(data) {
		return nil, 0, errors.New("truncated data")
	}
	if dataLen > 0 {
		log.Data = make([]byte, dataLen)
		copy(log.Data, data[pos:pos+int(dataLen)])
	}
	pos += int(dataLen)

	return log, pos, nil
}

// Prune removes the oldest entries, keeping only the last keepLast entries.
// Entries are pruned in insertion order.
func (rt *ReceiptTrie) Prune(keepLast int) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	if keepLast < 0 {
		keepLast = 0
	}
	if len(rt.order) <= keepLast {
		return
	}

	removeCount := len(rt.order) - keepLast
	toRemove := rt.order[:removeCount]
	for _, idx := range toRemove {
		delete(rt.entries, idx)
	}
	rt.order = rt.order[removeCount:]
}

// Reset clears all entries from the trie.
func (rt *ReceiptTrie) Reset() {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	rt.entries = make(map[uint64]*receiptTrieEntry)
	rt.order = nil
}

// Indices returns a sorted list of transaction indices in the trie.
func (rt *ReceiptTrie) Indices() []uint64 {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	indices := make([]uint64, 0, len(rt.entries))
	for idx := range rt.entries {
		indices = append(indices, idx)
	}
	sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })
	return indices
}
