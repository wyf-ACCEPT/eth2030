package core

import (
	"errors"
	"fmt"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

var (
	ErrKnownBlock    = errors.New("block already known")
	ErrInsertStopped = errors.New("insert stopped")
)

// HeaderChain manages the canonical header chain with block insertion,
// reorg detection, and chain traversal. It wraps a persistent database
// but can also run entirely in memory for testing.
type HeaderChain struct {
	mu        sync.RWMutex
	config    *ChainConfig
	validator *BlockValidator

	// In-memory canonical chain (number -> header).
	headers map[uint64]*types.Header

	// Hash -> header for all known headers.
	headersByHash map[types.Hash]*types.Header

	// Current head of the canonical chain.
	currentHeader *types.Header
}

// NewHeaderChain creates a new header chain with a genesis header.
func NewHeaderChain(config *ChainConfig, genesis *types.Header) *HeaderChain {
	hc := &HeaderChain{
		config:        config,
		validator:     NewBlockValidator(config),
		headers:       make(map[uint64]*types.Header),
		headersByHash: make(map[types.Hash]*types.Header),
	}
	hc.insertGenesis(genesis)
	return hc
}

// insertGenesis inserts the genesis header without validation.
func (hc *HeaderChain) insertGenesis(genesis *types.Header) {
	hash := genesis.Hash()
	hc.headers[genesis.Number.Uint64()] = genesis
	hc.headersByHash[hash] = genesis
	hc.currentHeader = genesis
}

// CurrentHeader returns the current head of the canonical chain.
func (hc *HeaderChain) CurrentHeader() *types.Header {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	return hc.currentHeader
}

// GetHeader retrieves a header by hash.
func (hc *HeaderChain) GetHeader(hash types.Hash) *types.Header {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	return hc.headersByHash[hash]
}

// GetHeaderByNumber retrieves the canonical header for a block number.
func (hc *HeaderChain) GetHeaderByNumber(number uint64) *types.Header {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	return hc.headers[number]
}

// HasHeader checks if a header with the given hash exists.
func (hc *HeaderChain) HasHeader(hash types.Hash) bool {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	_, ok := hc.headersByHash[hash]
	return ok
}

// InsertHeaders inserts a batch of headers into the chain.
// Headers must be in ascending order and form a contiguous chain.
// Returns the number of headers inserted and any error.
func (hc *HeaderChain) InsertHeaders(headers []*types.Header) (int, error) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	for i, header := range headers {
		if err := hc.insertHeader(header); err != nil {
			return i, fmt.Errorf("header %d: %w", header.Number.Uint64(), err)
		}
	}
	return len(headers), nil
}

// insertHeader inserts a single header. Must be called with hc.mu held.
func (hc *HeaderChain) insertHeader(header *types.Header) error {
	hash := header.Hash()

	// Check if already known.
	if _, ok := hc.headersByHash[hash]; ok {
		return nil // idempotent
	}

	// Find parent.
	parent, ok := hc.headersByHash[header.ParentHash]
	if !ok {
		return fmt.Errorf("%w: parent %v", ErrUnknownParent, header.ParentHash)
	}

	// Validate header against parent.
	if err := hc.validator.ValidateHeader(header, parent); err != nil {
		return err
	}

	// Store the header.
	num := header.Number.Uint64()
	hc.headersByHash[hash] = header

	// Update canonical chain if this extends the current head.
	if num > hc.currentHeader.Number.Uint64() {
		hc.headers[num] = header
		hc.currentHeader = header
	} else if num == hc.currentHeader.Number.Uint64() && hash != hc.currentHeader.Hash() {
		// Same height but different hash — could be a reorg candidate.
		// For now, keep the existing canonical head (first-seen wins).
	}

	return nil
}

// SetHead rewinds the canonical chain to the given block number.
// Headers above the given number are removed from the canonical index.
func (hc *HeaderChain) SetHead(number uint64) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	// Delete all canonical headers above the target.
	current := hc.currentHeader.Number.Uint64()
	for n := current; n > number; n-- {
		if h, ok := hc.headers[n]; ok {
			delete(hc.headersByHash, h.Hash())
			delete(hc.headers, n)
		}
	}

	// Set new head.
	if h, ok := hc.headers[number]; ok {
		hc.currentHeader = h
	}
}

// ChainLength returns the number of headers in the canonical chain.
func (hc *HeaderChain) ChainLength() uint64 {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	return hc.currentHeader.Number.Uint64() + 1
}

// GetAncestor retrieves the Nth ancestor of a given block hash.
// Returns the ancestor header and its hash, or nil if not found.
func (hc *HeaderChain) GetAncestor(hash types.Hash, n uint64) *types.Header {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	header := hc.headersByHash[hash]
	if header == nil {
		return nil
	}

	for i := uint64(0); i < n; i++ {
		parent, ok := hc.headersByHash[header.ParentHash]
		if !ok {
			return nil
		}
		header = parent
	}
	return header
}
