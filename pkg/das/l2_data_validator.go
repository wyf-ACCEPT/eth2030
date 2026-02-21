// Package das implements data availability sampling.
// This file adds L2 data validation with cryptographic commitment verification
// for teragas throughput (Gap #34), complementing the TeradataManager and
// BandwidthEnforcer by providing per-chain configuration, commitment
// validation, and throughput metrics.
package das

import (
	"encoding/binary"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// L2 data validator errors.
var (
	ErrL2ValidatorChainNotRegistered = errors.New("l2-validator: chain not registered")
	ErrL2ValidatorChainAlreadyExists = errors.New("l2-validator: chain already registered")
	ErrL2ValidatorMaxChainsReached   = errors.New("l2-validator: maximum chains reached")
	ErrL2ValidatorInvalidCommitment  = errors.New("l2-validator: commitment does not match data")
	ErrL2ValidatorEmptyData          = errors.New("l2-validator: data is empty")
	ErrL2ValidatorDataTooLarge       = errors.New("l2-validator: data exceeds max blob size")
	ErrL2ValidatorInvalidChainID     = errors.New("l2-validator: chain ID must be > 0")
)

// L2ChainConfig holds per-chain validation parameters.
type L2ChainConfig struct {
	// ChainID is the L2 chain identifier.
	ChainID uint64

	// MaxBlobSize is the maximum size of a single data blob in bytes.
	MaxBlobSize uint64

	// RequiredCustody is the minimum number of custody columns required.
	RequiredCustody uint64

	// CompressionCodec names the compression algorithm (e.g. "zstd", "none").
	CompressionCodec string
}

// L2DataReceipt is issued after successful validation and storage.
type L2DataReceipt struct {
	ChainID    uint64
	Slot       uint64
	Commitment types.Hash
	Size       uint64
	Timestamp  int64
	ProofHash  types.Hash
}

// L2ChainMetrics tracks per-chain throughput statistics.
type L2ChainMetrics struct {
	TotalBytes         uint64
	TotalBlobs         uint64
	AvgBlobSize        uint64
	PeakThroughputBps  uint64
}

// l2ChainState is internal per-chain state kept by the validator.
type l2ChainState struct {
	config  L2ChainConfig
	entries []l2DataEntry
	metrics L2ChainMetrics

	// throughput tracking: bytes observed in the current 1-second window.
	windowStart time.Time
	windowBytes uint64
}

// l2DataEntry records a single validated data blob.
type l2DataEntry struct {
	slot       uint64
	commitment types.Hash
	size       uint64
	timestamp  int64
}

// L2DataValidator validates L2 data cryptographic commitments and tracks
// per-chain throughput metrics. It is safe for concurrent use.
type L2DataValidator struct {
	mu        sync.RWMutex
	maxChains int
	chains    map[uint64]*l2ChainState
	timeFunc  func() time.Time
}

// NewL2DataValidator creates a validator that supports up to maxChains
// registered L2 chains. If maxChains <= 0, a default of 256 is used.
func NewL2DataValidator(maxChains int) *L2DataValidator {
	if maxChains <= 0 {
		maxChains = 256
	}
	return &L2DataValidator{
		maxChains: maxChains,
		chains:    make(map[uint64]*l2ChainState),
		timeFunc:  time.Now,
	}
}

// RegisterChain registers an L2 chain with the given configuration.
// Returns an error if the chain is already registered or the limit is reached.
func (v *L2DataValidator) RegisterChain(chainID uint64, config *L2ChainConfig) error {
	if chainID == 0 {
		return ErrL2ValidatorInvalidChainID
	}
	if config == nil {
		config = &L2ChainConfig{ChainID: chainID}
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	if _, exists := v.chains[chainID]; exists {
		return ErrL2ValidatorChainAlreadyExists
	}
	if len(v.chains) >= v.maxChains {
		return ErrL2ValidatorMaxChainsReached
	}

	cfg := *config
	cfg.ChainID = chainID
	if cfg.MaxBlobSize == 0 {
		cfg.MaxBlobSize = 4 * 1024 * 1024 // 4 MiB default
	}

	v.chains[chainID] = &l2ChainState{
		config:      cfg,
		entries:     nil,
		windowStart: v.timeFunc(),
	}
	return nil
}

// ValidateL2Data verifies that commitment == keccak256(chainID || data).
func (v *L2DataValidator) ValidateL2Data(chainID uint64, data []byte, commitment []byte) error {
	if chainID == 0 {
		return ErrL2ValidatorInvalidChainID
	}
	if len(data) == 0 {
		return ErrL2ValidatorEmptyData
	}

	v.mu.RLock()
	cs, ok := v.chains[chainID]
	v.mu.RUnlock()

	if !ok {
		return ErrL2ValidatorChainNotRegistered
	}
	if cs.config.MaxBlobSize > 0 && uint64(len(data)) > cs.config.MaxBlobSize {
		return ErrL2ValidatorDataTooLarge
	}

	expected := computeL2Commitment(chainID, data)
	if len(commitment) != len(expected) {
		return ErrL2ValidatorInvalidCommitment
	}
	for i := range expected {
		if commitment[i] != expected[i] {
			return ErrL2ValidatorInvalidCommitment
		}
	}
	return nil
}

// ValidateAndStore validates data and records it for the given slot.
// Returns an L2DataReceipt on success.
func (v *L2DataValidator) ValidateAndStore(chainID uint64, slot uint64, data []byte) (*L2DataReceipt, error) {
	if chainID == 0 {
		return nil, ErrL2ValidatorInvalidChainID
	}
	if len(data) == 0 {
		return nil, ErrL2ValidatorEmptyData
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	cs, ok := v.chains[chainID]
	if !ok {
		return nil, ErrL2ValidatorChainNotRegistered
	}
	if cs.config.MaxBlobSize > 0 && uint64(len(data)) > cs.config.MaxBlobSize {
		return nil, ErrL2ValidatorDataTooLarge
	}

	commitment := computeL2Commitment(chainID, data)
	var commitHash types.Hash
	copy(commitHash[:], commitment)

	// Compute a proof hash over slot + commitment.
	proofInput := make([]byte, 8+len(commitment))
	binary.BigEndian.PutUint64(proofInput[:8], slot)
	copy(proofInput[8:], commitment)
	proofHash := crypto.Keccak256Hash(proofInput)

	now := v.timeFunc()
	size := uint64(len(data))

	entry := l2DataEntry{
		slot:       slot,
		commitment: commitHash,
		size:       size,
		timestamp:  now.Unix(),
	}
	cs.entries = append(cs.entries, entry)

	// Update metrics.
	cs.metrics.TotalBlobs++
	cs.metrics.TotalBytes += size
	cs.metrics.AvgBlobSize = cs.metrics.TotalBytes / cs.metrics.TotalBlobs

	// Track throughput window.
	v.updateThroughput(cs, size, now)

	return &L2DataReceipt{
		ChainID:    chainID,
		Slot:       slot,
		Commitment: commitHash,
		Size:       size,
		Timestamp:  now.Unix(),
		ProofHash:  proofHash,
	}, nil
}

// updateThroughput updates the peak throughput metric for a chain.
// Must be called with v.mu held.
func (v *L2DataValidator) updateThroughput(cs *l2ChainState, size uint64, now time.Time) {
	elapsed := now.Sub(cs.windowStart)
	if elapsed >= time.Second {
		// Finalize previous window.
		if cs.windowBytes > cs.metrics.PeakThroughputBps {
			cs.metrics.PeakThroughputBps = cs.windowBytes
		}
		cs.windowStart = now
		cs.windowBytes = size
	} else {
		cs.windowBytes += size
		// Check if current window already exceeds peak.
		if cs.windowBytes > cs.metrics.PeakThroughputBps {
			cs.metrics.PeakThroughputBps = cs.windowBytes
		}
	}
}

// GetChainMetrics returns a snapshot of throughput metrics for the given chain.
// Returns nil if the chain is not registered.
func (v *L2DataValidator) GetChainMetrics(chainID uint64) *L2ChainMetrics {
	v.mu.RLock()
	defer v.mu.RUnlock()

	cs, ok := v.chains[chainID]
	if !ok {
		return nil
	}
	m := cs.metrics // copy
	return &m
}

// PruneChainData removes all entries for the given chain that have a slot
// strictly less than beforeSlot. Returns the number of entries removed.
func (v *L2DataValidator) PruneChainData(chainID uint64, beforeSlot uint64) int {
	v.mu.Lock()
	defer v.mu.Unlock()

	cs, ok := v.chains[chainID]
	if !ok {
		return 0
	}

	kept := cs.entries[:0]
	pruned := 0
	for _, e := range cs.entries {
		if e.slot < beforeSlot {
			pruned++
			cs.metrics.TotalBytes -= e.size
			cs.metrics.TotalBlobs--
		} else {
			kept = append(kept, e)
		}
	}
	cs.entries = kept

	// Recompute average.
	if cs.metrics.TotalBlobs > 0 {
		cs.metrics.AvgBlobSize = cs.metrics.TotalBytes / cs.metrics.TotalBlobs
	} else {
		cs.metrics.AvgBlobSize = 0
	}

	return pruned
}

// ActiveChains returns a sorted list of all registered chain IDs.
func (v *L2DataValidator) ActiveChains() []uint64 {
	v.mu.RLock()
	defer v.mu.RUnlock()

	ids := make([]uint64, 0, len(v.chains))
	for id := range v.chains {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// ChainCount returns the number of registered chains.
func (v *L2DataValidator) ChainCount() int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return len(v.chains)
}

// GetChainConfig returns the configuration for a registered chain, or nil.
func (v *L2DataValidator) GetChainConfig(chainID uint64) *L2ChainConfig {
	v.mu.RLock()
	defer v.mu.RUnlock()
	cs, ok := v.chains[chainID]
	if !ok {
		return nil
	}
	cfg := cs.config // copy
	return &cfg
}

// computeL2Commitment computes keccak256(chainID_be8 || data).
func computeL2Commitment(chainID uint64, data []byte) []byte {
	buf := make([]byte, 8+len(data))
	binary.BigEndian.PutUint64(buf[:8], chainID)
	copy(buf[8:], data)
	return crypto.Keccak256(buf)
}
