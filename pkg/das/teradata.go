// Package das implements data availability sampling.
// This file adds Teradata L2 support for high-throughput L2 data availability,
// targeting the M+ upgrade timeline on the Ethereum 2028 roadmap.
package das

import (
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/eth2028/eth2028/crypto"

	"github.com/eth2028/eth2028/core/types"
)

// Teradata L2 errors.
var (
	ErrTeradataDataTooLarge    = errors.New("teradata: data exceeds maximum size")
	ErrTeradataDataEmpty       = errors.New("teradata: data is empty")
	ErrTeradataNotFound        = errors.New("teradata: data not found")
	ErrTeradataTooManyChains   = errors.New("teradata: maximum L2 chains reached")
	ErrTeradataStorageFull     = errors.New("teradata: total storage limit exceeded")
	ErrTeradataInvalidReceipt  = errors.New("teradata: invalid receipt")
	ErrTeradataInvalidChainID  = errors.New("teradata: invalid chain ID (must be > 0)")
)

// TeradataConfig holds configuration for the TeradataManager.
type TeradataConfig struct {
	// MaxDataSize is the maximum size of a single L2 data blob in bytes.
	MaxDataSize uint64

	// MaxL2Chains is the maximum number of L2 chains that can be registered.
	MaxL2Chains uint64

	// RetentionSlots is how many slots of data to retain before pruning.
	RetentionSlots uint64

	// TotalStorageLimit is the total storage budget in bytes across all chains.
	TotalStorageLimit uint64
}

// DefaultTeradataConfig returns a sensible default configuration.
func DefaultTeradataConfig() TeradataConfig {
	return TeradataConfig{
		MaxDataSize:       4 * 1024 * 1024,  // 4 MiB
		MaxL2Chains:       256,
		RetentionSlots:    8192,              // ~1 day at 12s slots
		TotalStorageLimit: 16 * 1024 * 1024 * 1024, // 16 GiB
	}
}

// TeradataReceipt is returned after successfully storing L2 data. It acts as
// a receipt / proof-of-storage that can be used to retrieve or verify the data.
type TeradataReceipt struct {
	CommitmentHash types.Hash
	L2ChainID      uint64
	Slot           uint64
	Size           uint64
	Timestamp      uint64
}

// L2DataStats tracks aggregate statistics for a single L2 chain.
type L2DataStats struct {
	TotalBlobs  uint64
	TotalBytes  uint64
	AvgBlobSize uint64
	ChainID     uint64
}

// teradataEntry is an internal record linking a commitment to its data.
type teradataEntry struct {
	data      []byte
	receipt   TeradataReceipt
}

// TeradataManager manages teradata-level data availability for L2 chains.
// It is safe for concurrent use.
type TeradataManager struct {
	mu sync.RWMutex

	config TeradataConfig

	// currentSlot is a monotonically increasing slot counter assigned to
	// each stored blob so that pruning can operate on slot boundaries.
	currentSlot uint64

	// store maps commitment hash -> stored entry.
	store map[types.Hash]*teradataEntry

	// chainIndex maps L2 chain ID -> set of commitment hashes.
	chainIndex map[uint64]map[types.Hash]struct{}

	// totalBytes tracks the aggregate bytes stored across all chains.
	totalBytes uint64
}

// NewTeradataManager creates a new TeradataManager with the given config.
func NewTeradataManager(config TeradataConfig) *TeradataManager {
	return &TeradataManager{
		config:     config,
		store:      make(map[types.Hash]*teradataEntry),
		chainIndex: make(map[uint64]map[types.Hash]struct{}),
	}
}

// StoreL2Data stores L2 data and returns a receipt containing the commitment.
func (m *TeradataManager) StoreL2Data(l2ChainID uint64, data []byte) (*TeradataReceipt, error) {
	if l2ChainID == 0 {
		return nil, ErrTeradataInvalidChainID
	}
	if len(data) == 0 {
		return nil, ErrTeradataDataEmpty
	}
	if m.config.MaxDataSize > 0 && uint64(len(data)) > m.config.MaxDataSize {
		return nil, ErrTeradataDataTooLarge
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check chain limit: only count if this is a brand-new chain.
	if _, exists := m.chainIndex[l2ChainID]; !exists {
		if m.config.MaxL2Chains > 0 && uint64(len(m.chainIndex)) >= m.config.MaxL2Chains {
			return nil, ErrTeradataTooManyChains
		}
	}

	// Check total storage limit.
	if m.config.TotalStorageLimit > 0 && m.totalBytes+uint64(len(data)) > m.config.TotalStorageLimit {
		return nil, ErrTeradataStorageFull
	}

	// Compute commitment: Keccak256(chainID_bytes || data).
	chainBytes := uint64ToBytes(l2ChainID)
	commitment := crypto.Keccak256Hash(chainBytes, data)

	// Advance the slot counter for this new blob.
	m.currentSlot++

	now := uint64(time.Now().Unix())

	receipt := TeradataReceipt{
		CommitmentHash: commitment,
		L2ChainID:      l2ChainID,
		Slot:           m.currentSlot,
		Size:           uint64(len(data)),
		Timestamp:      now,
	}

	// Store a copy of the data so the caller can't mutate it.
	stored := make([]byte, len(data))
	copy(stored, data)

	m.store[commitment] = &teradataEntry{
		data:    stored,
		receipt: receipt,
	}

	// Update chain index.
	if m.chainIndex[l2ChainID] == nil {
		m.chainIndex[l2ChainID] = make(map[types.Hash]struct{})
	}
	m.chainIndex[l2ChainID][commitment] = struct{}{}
	m.totalBytes += uint64(len(data))

	return &receipt, nil
}

// RetrieveL2Data retrieves stored L2 data by its commitment hash.
func (m *TeradataManager) RetrieveL2Data(commitment types.Hash) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, ok := m.store[commitment]
	if !ok {
		return nil, ErrTeradataNotFound
	}

	// Return a copy so callers cannot mutate internal state.
	out := make([]byte, len(entry.data))
	copy(out, entry.data)
	return out, nil
}

// VerifyL2Data verifies that the given data matches a receipt's commitment.
func (m *TeradataManager) VerifyL2Data(receipt *TeradataReceipt, data []byte) bool {
	if receipt == nil || len(data) == 0 {
		return false
	}
	if uint64(len(data)) != receipt.Size {
		return false
	}

	chainBytes := uint64ToBytes(receipt.L2ChainID)
	computed := crypto.Keccak256Hash(chainBytes, data)
	return computed == receipt.CommitmentHash
}

// GetL2Stats returns aggregate statistics for a specific L2 chain.
// Returns nil if the chain has no data stored.
func (m *TeradataManager) GetL2Stats(l2ChainID uint64) *L2DataStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	hashes, ok := m.chainIndex[l2ChainID]
	if !ok || len(hashes) == 0 {
		return nil
	}

	stats := &L2DataStats{
		ChainID: l2ChainID,
	}
	for h := range hashes {
		entry, ok := m.store[h]
		if !ok {
			continue
		}
		stats.TotalBlobs++
		stats.TotalBytes += entry.receipt.Size
	}
	if stats.TotalBlobs > 0 {
		stats.AvgBlobSize = stats.TotalBytes / stats.TotalBlobs
	}
	return stats
}

// PruneOldData removes all entries with a slot number strictly less than
// olderThanSlot and returns the number of entries deleted.
func (m *TeradataManager) PruneOldData(olderThanSlot uint64) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	var toDelete []types.Hash
	for h, entry := range m.store {
		if entry.receipt.Slot < olderThanSlot {
			toDelete = append(toDelete, h)
		}
	}

	for _, h := range toDelete {
		entry := m.store[h]
		chainID := entry.receipt.L2ChainID
		m.totalBytes -= entry.receipt.Size

		delete(m.store, h)

		if idx, ok := m.chainIndex[chainID]; ok {
			delete(idx, h)
			if len(idx) == 0 {
				delete(m.chainIndex, chainID)
			}
		}
	}

	return len(toDelete)
}

// ListL2Chains returns a sorted list of all L2 chain IDs that currently have
// stored data.
func (m *TeradataManager) ListL2Chains() []uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	chains := make([]uint64, 0, len(m.chainIndex))
	for id := range m.chainIndex {
		chains = append(chains, id)
	}
	sort.Slice(chains, func(i, j int) bool { return chains[i] < chains[j] })
	return chains
}

// uint64ToBytes converts a uint64 to an 8-byte big-endian slice.
func uint64ToBytes(v uint64) []byte {
	b := make([]byte, 8)
	b[0] = byte(v >> 56)
	b[1] = byte(v >> 48)
	b[2] = byte(v >> 40)
	b[3] = byte(v >> 32)
	b[4] = byte(v >> 24)
	b[5] = byte(v >> 16)
	b[6] = byte(v >> 8)
	b[7] = byte(v)
	return b
}
