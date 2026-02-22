package rpc

import (
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// FilterConfig holds configuration for the filter system.
type FilterConfig struct {
	// MaxFilters is the maximum number of active filters.
	MaxFilters int
	// FilterTimeout is how long a filter lives without being polled.
	FilterTimeout time.Duration
	// MaxLogs is the maximum number of logs retained per filter.
	MaxLogs int
}

// DefaultFilterConfig returns a FilterConfig with sensible defaults.
func DefaultFilterConfig() FilterConfig {
	return FilterConfig{
		MaxFilters:    100,
		FilterTimeout: 5 * time.Minute,
		MaxLogs:       10000,
	}
}

// FSLogFilter tracks a log subscription with address/topic criteria.
type FSLogFilter struct {
	ID        types.Hash
	FromBlock uint64
	ToBlock   uint64
	Addresses []types.Address
	Topics    [][]types.Hash
	CreatedAt time.Time
	Logs      []*types.Log
}

// FSBlockFilter tracks new block hashes for subscription polling.
type FSBlockFilter struct {
	ID          types.Hash
	CreatedAt   time.Time
	BlockHashes []types.Hash
}

// filterKind distinguishes filter types in the internal map.
type filterKind int

const (
	filterKindLog   filterKind = iota
	filterKindBlock
)

// filterEntry is the internal representation of an installed filter.
type filterEntry struct {
	kind      filterKind
	logFilter *FSLogFilter
	blockFilter *FSBlockFilter
	lastPoll  time.Time
}

// FilterSystem manages log and block filters for the RPC layer.
type FilterSystem struct {
	mu      sync.RWMutex
	config  FilterConfig
	filters map[types.Hash]*filterEntry
	nextSeq uint64
}

// NewFilterSystem creates a new FilterSystem with the given configuration.
func NewFilterSystem(config FilterConfig) *FilterSystem {
	return &FilterSystem{
		config:  config,
		filters: make(map[types.Hash]*filterEntry),
	}
}

// NewFSLogFilter creates a new log filter with the given criteria. Returns
// the filter ID or an error if the maximum number of filters has been reached.
func (fs *FilterSystem) NewFSLogFilter(fromBlock, toBlock uint64, addresses []types.Address, topics [][]types.Hash) (types.Hash, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if len(fs.filters) >= fs.config.MaxFilters {
		return types.Hash{}, errors.New("maximum number of filters reached")
	}

	id := fs.generateID()
	lf := &FSLogFilter{
		ID:        id,
		FromBlock: fromBlock,
		ToBlock:   toBlock,
		Addresses: addresses,
		Topics:    topics,
		CreatedAt: time.Now(),
	}
	fs.filters[id] = &filterEntry{
		kind:      filterKindLog,
		logFilter: lf,
		lastPoll:  time.Now(),
	}
	return id, nil
}

// NewFSBlockFilter creates a new block filter. Returns the filter ID or an
// error if the maximum number of filters has been reached.
func (fs *FilterSystem) NewFSBlockFilter() (types.Hash, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if len(fs.filters) >= fs.config.MaxFilters {
		return types.Hash{}, errors.New("maximum number of filters reached")
	}

	id := fs.generateID()
	bf := &FSBlockFilter{
		ID:        id,
		CreatedAt: time.Now(),
	}
	fs.filters[id] = &filterEntry{
		kind:        filterKindBlock,
		blockFilter: bf,
		lastPoll:    time.Now(),
	}
	return id, nil
}

// GetFilterLogs returns the accumulated logs for a log filter. Returns
// an error if the filter does not exist or is not a log filter.
func (fs *FilterSystem) GetFilterLogs(id types.Hash) ([]*types.Log, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	entry, ok := fs.filters[id]
	if !ok {
		return nil, errors.New("filter not found")
	}
	if entry.kind != filterKindLog {
		return nil, errors.New("not a log filter")
	}
	entry.lastPoll = time.Now()

	logs := entry.logFilter.Logs
	entry.logFilter.Logs = nil
	if logs == nil {
		logs = []*types.Log{}
	}
	return logs, nil
}

// GetFilterBlockHashes returns the accumulated block hashes for a block
// filter. Returns an error if the filter does not exist or is not a block filter.
func (fs *FilterSystem) GetFilterBlockHashes(id types.Hash) ([]types.Hash, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	entry, ok := fs.filters[id]
	if !ok {
		return nil, errors.New("filter not found")
	}
	if entry.kind != filterKindBlock {
		return nil, errors.New("not a block filter")
	}
	entry.lastPoll = time.Now()

	hashes := entry.blockFilter.BlockHashes
	entry.blockFilter.BlockHashes = nil
	if hashes == nil {
		hashes = []types.Hash{}
	}
	return hashes, nil
}

// AddLog distributes a log to all matching log filters. A log matches
// if its address is in the filter's address list (or the list is empty)
// and its topics satisfy the filter's topic criteria.
func (fs *FilterSystem) AddLog(log *types.Log) {
	if log == nil {
		return
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()

	for _, entry := range fs.filters {
		if entry.kind != filterKindLog {
			continue
		}
		lf := entry.logFilter
		if fs.logMatches(log, lf) {
			if len(lf.Logs) < fs.config.MaxLogs {
				lf.Logs = append(lf.Logs, log)
			}
		}
	}
}

// AddBlockHash distributes a block hash to all block filters.
func (fs *FilterSystem) AddBlockHash(hash types.Hash) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	for _, entry := range fs.filters {
		if entry.kind != filterKindBlock {
			continue
		}
		entry.blockFilter.BlockHashes = append(entry.blockFilter.BlockHashes, hash)
	}
}

// UninstallFilter removes a filter. Returns true if the filter existed.
func (fs *FilterSystem) UninstallFilter(id types.Hash) bool {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	_, ok := fs.filters[id]
	if ok {
		delete(fs.filters, id)
	}
	return ok
}

// PruneExpired removes all filters that have not been polled within the
// configured timeout.
func (fs *FilterSystem) PruneExpired() {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	now := time.Now()
	for id, entry := range fs.filters {
		if now.Sub(entry.lastPoll) > fs.config.FilterTimeout {
			delete(fs.filters, id)
		}
	}
}

// FilterCount returns the number of active filters.
func (fs *FilterSystem) FilterCount() int {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return len(fs.filters)
}

// generateID produces a unique filter ID using keccak256 over a sequence
// number and timestamp. Caller must hold fs.mu.
func (fs *FilterSystem) generateID() types.Hash {
	fs.nextSeq++
	var buf [16]byte
	binary.LittleEndian.PutUint64(buf[:8], fs.nextSeq)
	binary.LittleEndian.PutUint64(buf[8:], uint64(time.Now().UnixNano()))
	return crypto.Keccak256Hash(buf[:])
}

// logMatches checks whether a log matches a log filter's criteria.
func (fs *FilterSystem) logMatches(log *types.Log, lf *FSLogFilter) bool {
	// Block range check.
	if log.BlockNumber < lf.FromBlock || (lf.ToBlock > 0 && log.BlockNumber > lf.ToBlock) {
		return false
	}

	// Address filter: log must match any listed address.
	if len(lf.Addresses) > 0 {
		found := false
		for _, addr := range lf.Addresses {
			if log.Address == addr {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Topic filter: AND across positions, OR within each position.
	for i, topicSet := range lf.Topics {
		if len(topicSet) == 0 {
			continue // wildcard
		}
		if i >= len(log.Topics) {
			return false
		}
		matched := false
		for _, t := range topicSet {
			if log.Topics[i] == t {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	return true
}
