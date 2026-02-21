// anchor_chain_tracker.go tracks per-chain anchor state for native rollups
// (EIP-8079). It maintains a registry of L2 chains with their anchor
// configurations, stores anchor history, handles confirmations, and provides
// per-chain metrics. This complements anchor_state.go which provides the
// higher-level proof-verified state management.
package rollup

import (
	"errors"
	"sort"
	"sync"
	"time"
)

// Anchor chain tracker errors.
var (
	ErrChainAlreadyRegistered = errors.New("anchor_tracker: chain already registered")
	ErrChainNotRegistered     = errors.New("anchor_tracker: chain not registered")
	ErrChainMaxReached        = errors.New("anchor_tracker: maximum chains reached")
	ErrChainIDZero            = errors.New("anchor_tracker: chain ID must be non-zero")
	ErrAnchorBlockRegression  = errors.New("anchor_tracker: L1 block must advance")
	ErrAnchorAlreadyConfirmed = errors.New("anchor_tracker: anchor already confirmed")
	ErrAnchorBlockNotFound    = errors.New("anchor_tracker: anchor at block not found")
)

// AnchorChainConfig describes the configuration for a registered chain.
type AnchorChainConfig struct {
	// ChainID is the L2 chain identifier.
	ChainID uint64

	// AnchorAddress is the anchor contract address on L2.
	AnchorAddress [20]byte

	// GenesisRoot is the L2 state root at genesis.
	GenesisRoot [32]byte

	// ConfirmationDepth is the number of L1 blocks before an anchor
	// is considered confirmed.
	ConfirmationDepth uint64

	// MaxGasPerExecution is the gas limit for rollup execution.
	MaxGasPerExecution uint64
}

// AnchorPoint records a single anchor update for a chain.
type AnchorPoint struct {
	// ChainID identifies which chain this anchor belongs to.
	ChainID uint64

	// L1BlockNumber is the L1 block that this anchor references.
	L1BlockNumber uint64

	// L2StateRoot is the L2 state root at this anchor point.
	L2StateRoot [32]byte

	// Timestamp is the unix timestamp when this anchor was recorded.
	Timestamp int64

	// Confirmed indicates whether the anchor has enough L1 confirmations.
	Confirmed bool
}

// AnchorMetrics provides per-chain statistics.
type AnchorMetrics struct {
	// TotalAnchors is the total number of anchors recorded for this chain.
	TotalAnchors uint64

	// ConfirmedAnchors is the count of confirmed anchors.
	ConfirmedAnchors uint64

	// AvgConfirmationDepth is the average confirmation depth across
	// confirmed anchors (0 if no confirmed anchors).
	AvgConfirmationDepth uint64
}

// chainRecord holds all anchor data for a single chain.
type chainRecord struct {
	config  AnchorChainConfig
	anchors []*AnchorPoint
}

// AnchorChainTracker manages anchor state for multiple L2 chains.
// Thread-safe for concurrent access.
type AnchorChainTracker struct {
	mu        sync.RWMutex
	chains    map[uint64]*chainRecord
	maxChains int
}

// NewAnchorChainTracker creates a new tracker with the given maximum number
// of supported chains.
func NewAnchorChainTracker(maxChains int) *AnchorChainTracker {
	if maxChains <= 0 {
		maxChains = 64
	}
	return &AnchorChainTracker{
		chains:    make(map[uint64]*chainRecord),
		maxChains: maxChains,
	}
}

// RegisterChain registers a new L2 chain for anchor tracking.
func (act *AnchorChainTracker) RegisterChain(chainID uint64, config *AnchorChainConfig) error {
	if chainID == 0 {
		return ErrChainIDZero
	}

	act.mu.Lock()
	defer act.mu.Unlock()

	if _, exists := act.chains[chainID]; exists {
		return ErrChainAlreadyRegistered
	}
	if len(act.chains) >= act.maxChains {
		return ErrChainMaxReached
	}

	cfg := *config
	cfg.ChainID = chainID
	if cfg.ConfirmationDepth == 0 {
		cfg.ConfirmationDepth = 64 // default confirmation depth
	}
	if cfg.MaxGasPerExecution == 0 {
		cfg.MaxGasPerExecution = 30_000_000
	}

	act.chains[chainID] = &chainRecord{
		config:  cfg,
		anchors: make([]*AnchorPoint, 0),
	}
	return nil
}

// UpdateAnchor records a new anchor point for a chain. The L1 block number
// must be strictly greater than the previous anchor's L1 block.
func (act *AnchorChainTracker) UpdateAnchor(chainID uint64, l1Block uint64, stateRoot [32]byte) error {
	act.mu.Lock()
	defer act.mu.Unlock()

	rec, ok := act.chains[chainID]
	if !ok {
		return ErrChainNotRegistered
	}

	// Enforce block progression.
	if len(rec.anchors) > 0 {
		last := rec.anchors[len(rec.anchors)-1]
		if l1Block <= last.L1BlockNumber {
			return ErrAnchorBlockRegression
		}
	}

	anchor := &AnchorPoint{
		ChainID:       chainID,
		L1BlockNumber: l1Block,
		L2StateRoot:   stateRoot,
		Timestamp:     time.Now().Unix(),
		Confirmed:     false,
	}
	rec.anchors = append(rec.anchors, anchor)
	return nil
}

// GetLatestAnchor returns the most recent anchor point for a chain.
func (act *AnchorChainTracker) GetLatestAnchor(chainID uint64) (*AnchorPoint, error) {
	act.mu.RLock()
	defer act.mu.RUnlock()

	rec, ok := act.chains[chainID]
	if !ok {
		return nil, ErrChainNotRegistered
	}
	if len(rec.anchors) == 0 {
		return nil, ErrAnchorBlockNotFound
	}

	// Return a copy.
	latest := *rec.anchors[len(rec.anchors)-1]
	return &latest, nil
}

// GetAnchorHistory returns the most recent 'count' anchors for a chain,
// ordered from newest to oldest.
func (act *AnchorChainTracker) GetAnchorHistory(chainID uint64, count int) ([]*AnchorPoint, error) {
	act.mu.RLock()
	defer act.mu.RUnlock()

	rec, ok := act.chains[chainID]
	if !ok {
		return nil, ErrChainNotRegistered
	}
	if count <= 0 || len(rec.anchors) == 0 {
		return []*AnchorPoint{}, nil
	}

	start := len(rec.anchors) - count
	if start < 0 {
		start = 0
	}

	result := make([]*AnchorPoint, 0, len(rec.anchors)-start)
	for i := len(rec.anchors) - 1; i >= start; i-- {
		cp := *rec.anchors[i]
		result = append(result, &cp)
	}
	return result, nil
}

// ConfirmAnchor marks the anchor at the given L1 block number as confirmed.
func (act *AnchorChainTracker) ConfirmAnchor(chainID uint64, l1Block uint64) error {
	act.mu.Lock()
	defer act.mu.Unlock()

	rec, ok := act.chains[chainID]
	if !ok {
		return ErrChainNotRegistered
	}

	for _, a := range rec.anchors {
		if a.L1BlockNumber == l1Block {
			if a.Confirmed {
				return ErrAnchorAlreadyConfirmed
			}
			a.Confirmed = true
			return nil
		}
	}
	return ErrAnchorBlockNotFound
}

// PruneAnchors removes all anchors with L1 block number strictly less than
// beforeBlock. Returns the number of anchors pruned.
func (act *AnchorChainTracker) PruneAnchors(chainID uint64, beforeBlock uint64) int {
	act.mu.Lock()
	defer act.mu.Unlock()

	rec, ok := act.chains[chainID]
	if !ok {
		return 0
	}

	kept := make([]*AnchorPoint, 0, len(rec.anchors))
	pruned := 0
	for _, a := range rec.anchors {
		if a.L1BlockNumber < beforeBlock {
			pruned++
		} else {
			kept = append(kept, a)
		}
	}
	rec.anchors = kept
	return pruned
}

// ActiveChains returns the chain IDs of all registered chains, sorted.
func (act *AnchorChainTracker) ActiveChains() []uint64 {
	act.mu.RLock()
	defer act.mu.RUnlock()

	ids := make([]uint64, 0, len(act.chains))
	for id := range act.chains {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// ChainMetrics returns metrics for a specific chain. Returns nil if the
// chain is not registered.
func (act *AnchorChainTracker) ChainMetrics(chainID uint64) *AnchorMetrics {
	act.mu.RLock()
	defer act.mu.RUnlock()

	rec, ok := act.chains[chainID]
	if !ok {
		return nil
	}

	metrics := &AnchorMetrics{
		TotalAnchors: uint64(len(rec.anchors)),
	}

	var confirmedCount uint64
	var depthSum uint64
	for _, a := range rec.anchors {
		if a.Confirmed {
			confirmedCount++
			// Confirmation depth is the difference between the latest
			// anchor block and this confirmed anchor's block.
			if len(rec.anchors) > 0 {
				latest := rec.anchors[len(rec.anchors)-1]
				if latest.L1BlockNumber >= a.L1BlockNumber {
					depthSum += latest.L1BlockNumber - a.L1BlockNumber
				}
			}
		}
	}
	metrics.ConfirmedAnchors = confirmedCount
	if confirmedCount > 0 {
		metrics.AvgConfirmationDepth = depthSum / confirmedCount
	}

	return metrics
}

// ChainCount returns the number of registered chains.
func (act *AnchorChainTracker) ChainCount() int {
	act.mu.RLock()
	defer act.mu.RUnlock()
	return len(act.chains)
}

// GetChainConfig returns a copy of the config for a registered chain.
func (act *AnchorChainTracker) GetChainConfig(chainID uint64) (*AnchorChainConfig, error) {
	act.mu.RLock()
	defer act.mu.RUnlock()

	rec, ok := act.chains[chainID]
	if !ok {
		return nil, ErrChainNotRegistered
	}
	cfg := rec.config
	return &cfg, nil
}
