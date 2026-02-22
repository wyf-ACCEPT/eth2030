// builder_registry.go implements a registry for tracking ePBS builder
// registrations, bid history, and performance statistics. Unlike the
// BuilderMarket (which manages per-slot auctions), the BuilderRegistry
// focuses on long-term builder identity, staking, and historical performance.
//
// Part of the Glamsterdam/Hogota roadmap for ePBS builder management.
package epbs

import (
	"errors"
	"sort"
	"sync"
)

// Registry errors.
var (
	ErrRegistryFull         = errors.New("registry: maximum builders reached")
	ErrRegistryDuplicate    = errors.New("registry: builder already registered")
	ErrRegistryNotFound     = errors.New("registry: builder not found")
	ErrRegistryInactive     = errors.New("registry: builder is inactive")
	ErrRegistryNilInfo      = errors.New("registry: nil builder info")
	ErrRegistryNilBidRecord = errors.New("registry: nil bid record")
	ErrRegistryZeroStake    = errors.New("registry: builder stake is zero")
)

// BuilderInfo contains registration details for a builder in the registry.
type BuilderInfo struct {
	// Address is the builder's Ethereum address (20 bytes).
	Address [20]byte

	// Pubkey is the builder's BLS12-381 public key (48 bytes).
	Pubkey [48]byte

	// FeeRecipient is the address that receives builder fees.
	FeeRecipient [20]byte

	// GasLimit is the builder's preferred gas limit.
	GasLimit uint64

	// RegisteredAt is the Unix timestamp when the builder was registered.
	RegisteredAt int64

	// Active indicates whether the builder is currently active.
	Active bool

	// Stake is the builder's staked amount in Gwei.
	Stake uint64
}

// BuilderBidRecord records a single bid from a builder.
type BuilderBidRecord struct {
	// Slot is the slot the bid was made for.
	Slot uint64

	// Value is the bid value in Gwei.
	Value uint64

	// GasLimit is the gas limit associated with the bid.
	GasLimit uint64

	// Timestamp is the Unix timestamp when the bid was submitted.
	Timestamp int64

	// Won indicates whether this bid won its auction.
	Won bool
}

// BuilderStats contains aggregate performance statistics for a builder.
type BuilderStats struct {
	// Address is the builder's address for identification.
	Address [20]byte

	// TotalBids is the total number of bids submitted.
	TotalBids uint64

	// WonBids is the number of bids that won their auction.
	WonBids uint64

	// TotalValue is the cumulative bid value in Gwei.
	TotalValue uint64

	// WinRate is the ratio of won bids to total bids (0.0 to 1.0).
	WinRate float64

	// AvgBidValue is the average bid value in Gwei.
	AvgBidValue uint64
}

// builderEntry holds a registered builder's info and bid history.
type builderEntry struct {
	info *BuilderInfo
	bids []*BuilderBidRecord
}

// BuilderRegistry tracks builder registrations, bids, and performance.
// All public methods are safe for concurrent use.
type BuilderRegistry struct {
	mu          sync.RWMutex
	maxBuilders int
	builders    map[[20]byte]*builderEntry
}

// NewBuilderRegistry creates a new builder registry with the specified
// maximum number of builders. If maxBuilders <= 0, defaults to 1024.
func NewBuilderRegistry(maxBuilders int) *BuilderRegistry {
	if maxBuilders <= 0 {
		maxBuilders = 1024
	}
	return &BuilderRegistry{
		maxBuilders: maxBuilders,
		builders:    make(map[[20]byte]*builderEntry),
	}
}

// RegisterBuilder adds a new builder to the registry. Returns an error if
// the builder is already registered, the registry is full, or the info is nil.
func (r *BuilderRegistry) RegisterBuilder(info *BuilderInfo) error {
	if info == nil {
		return ErrRegistryNilInfo
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.builders[info.Address]; exists {
		return ErrRegistryDuplicate
	}
	if len(r.builders) >= r.maxBuilders {
		return ErrRegistryFull
	}

	// Make a copy to prevent external mutation.
	cp := *info
	r.builders[info.Address] = &builderEntry{
		info: &cp,
		bids: make([]*BuilderBidRecord, 0, 64),
	}
	return nil
}

// DeregisterBuilder removes a builder from the registry.
func (r *BuilderRegistry) DeregisterBuilder(address [20]byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.builders[address]; !exists {
		return ErrRegistryNotFound
	}
	delete(r.builders, address)
	return nil
}

// GetBuilder returns a copy of the builder info for the given address.
func (r *BuilderRegistry) GetBuilder(address [20]byte) (*BuilderInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.builders[address]
	if !exists {
		return nil, false
	}
	cp := *entry.info
	return &cp, true
}

// ActiveBuilders returns a slice of all active builders in the registry.
func (r *BuilderRegistry) ActiveBuilders() []*BuilderInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var active []*BuilderInfo
	for _, entry := range r.builders {
		if entry.info.Active {
			cp := *entry.info
			active = append(active, &cp)
		}
	}
	return active
}

// BuilderCount returns the total number of registered builders.
func (r *BuilderRegistry) BuilderCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.builders)
}

// RecordBid records a bid from a builder. The builder must be registered.
func (r *BuilderRegistry) RecordBid(address [20]byte, bid *BuilderBidRecord) error {
	if bid == nil {
		return ErrRegistryNilBidRecord
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.builders[address]
	if !exists {
		return ErrRegistryNotFound
	}

	// Copy the bid record to prevent external mutation.
	cp := *bid
	entry.bids = append(entry.bids, &cp)
	return nil
}

// GetBuilderStats computes aggregate performance statistics for a builder.
func (r *BuilderRegistry) GetBuilderStats(address [20]byte) (*BuilderStats, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.builders[address]
	if !exists {
		return nil, ErrRegistryNotFound
	}

	return computeBuilderStats(address, entry.bids), nil
}

// TopBuilders returns the top n builders ranked by win rate (descending).
// Builders with no bids are excluded. If n exceeds the number of qualifying
// builders, all qualifying builders are returned.
func (r *BuilderRegistry) TopBuilders(n int) []*BuilderStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var stats []*BuilderStats
	for addr, entry := range r.builders {
		if len(entry.bids) == 0 {
			continue
		}
		stats = append(stats, computeBuilderStats(addr, entry.bids))
	}

	// Sort by win rate descending, then by total bids descending as tiebreaker.
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].WinRate != stats[j].WinRate {
			return stats[i].WinRate > stats[j].WinRate
		}
		return stats[i].TotalBids > stats[j].TotalBids
	})

	if n > len(stats) {
		n = len(stats)
	}
	if n <= 0 {
		return nil
	}
	return stats[:n]
}

// PruneInactive removes all builders whose most recent activity
// (RegisteredAt or latest bid timestamp) is before the given Unix timestamp.
// Returns the number of builders pruned.
func (r *BuilderRegistry) PruneInactive(beforeTimestamp int64) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	pruned := 0
	for addr, entry := range r.builders {
		latest := entry.info.RegisteredAt
		for _, bid := range entry.bids {
			if bid.Timestamp > latest {
				latest = bid.Timestamp
			}
		}
		if latest < beforeTimestamp {
			delete(r.builders, addr)
			pruned++
		}
	}
	return pruned
}

// computeBuilderStats calculates stats from a builder's bid history.
func computeBuilderStats(address [20]byte, bids []*BuilderBidRecord) *BuilderStats {
	stats := &BuilderStats{
		Address:   address,
		TotalBids: uint64(len(bids)),
	}

	if len(bids) == 0 {
		return stats
	}

	for _, bid := range bids {
		stats.TotalValue += bid.Value
		if bid.Won {
			stats.WonBids++
		}
	}

	stats.WinRate = float64(stats.WonBids) / float64(stats.TotalBids)
	stats.AvgBidValue = stats.TotalValue / stats.TotalBids

	return stats
}
