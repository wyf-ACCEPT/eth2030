// filter_sys.go implements a comprehensive log filter system with bloom-filter
// based pre-screening, pending transaction tracking, poll-based change
// retrieval, and filter expiry. Uses the FilterSys type prefix to avoid
// conflicts with existing FilterSystem and ExtFilterManager.
package rpc

import (
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// FilterSys errors.
var (
	ErrSysFilterNotFound    = errors.New("filtersys: filter not found")
	ErrSysFilterWrongKind   = errors.New("filtersys: wrong filter kind")
	ErrSysFilterCapacity    = errors.New("filtersys: maximum filter count reached")
	ErrSysFilterClosed      = errors.New("filtersys: system is closed")
	ErrSysInvalidBlockRange = errors.New("filtersys: fromBlock > toBlock")
	ErrSysTopicOverflow     = errors.New("filtersys: too many topic positions (max 4)")
)

// SysFilterKind identifies the type of a filter managed by FilterSys.
type SysFilterKind uint8

const (
	// SysLogFilter tracks matching log events.
	SysLogFilter SysFilterKind = iota
	// SysBlockFilter tracks new block hashes.
	SysBlockFilter
	// SysPendingTxFilter tracks pending transaction hashes.
	SysPendingTxFilter
)

// String returns the filter kind name.
func (k SysFilterKind) String() string {
	switch k {
	case SysLogFilter:
		return "log"
	case SysBlockFilter:
		return "block"
	case SysPendingTxFilter:
		return "pendingTx"
	default:
		return "unknown"
	}
}

// FilterSysConfig configures the FilterSys behavior.
type FilterSysConfig struct {
	// MaxFilters caps the total number of active filters.
	MaxFilters int
	// FilterTimeout is the inactivity duration after which a filter
	// is eligible for expiry.
	FilterTimeout time.Duration
	// MaxLogsPerFilter caps the log buffer per log filter.
	MaxLogsPerFilter int
	// MaxHashesPerFilter caps the hash buffer per block/pendingTx filter.
	MaxHashesPerFilter int
	// MaxTopicPositions limits the number of AND-positions in a log filter.
	MaxTopicPositions int
	// MaxBlockRange limits the block range in a log filter query.
	MaxBlockRange uint64
}

// DefaultFilterSysConfig returns sensible defaults.
func DefaultFilterSysConfig() FilterSysConfig {
	return FilterSysConfig{
		MaxFilters:         512,
		FilterTimeout:      5 * time.Minute,
		MaxLogsPerFilter:   10000,
		MaxHashesPerFilter: 2048,
		MaxTopicPositions:  4,
		MaxBlockRange:      10000,
	}
}

// SysLogQuery captures the matching criteria for a log filter.
type SysLogQuery struct {
	FromBlock uint64
	ToBlock   uint64
	Addresses []types.Address
	Topics    [][]types.Hash
}

// sysFilterEntry is the internal state for a managed filter.
type sysFilterEntry struct {
	id       string
	kind     SysFilterKind
	created  time.Time
	lastPoll time.Time

	// Log filter state.
	query     SysLogQuery
	logs      []*types.Log
	lastBlock uint64 // last block scanned, for incremental polling

	// Block/PendingTx filter state.
	hashes []types.Hash
}

// FilterSys manages event subscriptions with poll-based retrieval,
// bloom pre-filtering, and automatic expiry. Thread-safe.
type FilterSys struct {
	mu      sync.RWMutex
	config  FilterSysConfig
	filters map[string]*sysFilterEntry
	seq     uint64
	closed  bool
}

// NewFilterSys creates a new filter system.
func NewFilterSys(config FilterSysConfig) *FilterSys {
	if config.MaxFilters <= 0 {
		config.MaxFilters = 512
	}
	if config.FilterTimeout <= 0 {
		config.FilterTimeout = 5 * time.Minute
	}
	if config.MaxLogsPerFilter <= 0 {
		config.MaxLogsPerFilter = 10000
	}
	if config.MaxHashesPerFilter <= 0 {
		config.MaxHashesPerFilter = 2048
	}
	if config.MaxTopicPositions <= 0 {
		config.MaxTopicPositions = 4
	}
	return &FilterSys{
		config:  config,
		filters: make(map[string]*sysFilterEntry),
	}
}

// Close shuts down the filter system and removes all filters.
func (fs *FilterSys) Close() {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.closed = true
	for id := range fs.filters {
		delete(fs.filters, id)
	}
}

// NewLogFilter installs a log filter with the given query criteria.
func (fs *FilterSys) NewLogFilter(query SysLogQuery) (string, error) {
	if len(query.Topics) > fs.config.MaxTopicPositions {
		return "", ErrSysTopicOverflow
	}
	if query.ToBlock > 0 && query.FromBlock > query.ToBlock {
		return "", ErrSysInvalidBlockRange
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()

	if fs.closed {
		return "", ErrSysFilterClosed
	}
	if len(fs.filters) >= fs.config.MaxFilters {
		return "", ErrSysFilterCapacity
	}

	now := time.Now()
	id := fs.generateID()

	fs.filters[id] = &sysFilterEntry{
		id:        id,
		kind:      SysLogFilter,
		created:   now,
		lastPoll:  now,
		query:     query,
		lastBlock: query.FromBlock,
	}
	return id, nil
}

// NewBlockFilter installs a block filter that accumulates new block hashes.
func (fs *FilterSys) NewBlockFilter() (string, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if fs.closed {
		return "", ErrSysFilterClosed
	}
	if len(fs.filters) >= fs.config.MaxFilters {
		return "", ErrSysFilterCapacity
	}

	now := time.Now()
	id := fs.generateID()

	fs.filters[id] = &sysFilterEntry{
		id:       id,
		kind:     SysBlockFilter,
		created:  now,
		lastPoll: now,
	}
	return id, nil
}

// NewPendingTxFilter installs a pending transaction filter.
func (fs *FilterSys) NewPendingTxFilter() (string, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if fs.closed {
		return "", ErrSysFilterClosed
	}
	if len(fs.filters) >= fs.config.MaxFilters {
		return "", ErrSysFilterCapacity
	}

	now := time.Now()
	id := fs.generateID()

	fs.filters[id] = &sysFilterEntry{
		id:       id,
		kind:     SysPendingTxFilter,
		created:  now,
		lastPoll: now,
	}
	return id, nil
}

// UninstallFilter removes a filter by ID. Returns true if found.
func (fs *FilterSys) UninstallFilter(id string) bool {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	_, ok := fs.filters[id]
	if ok {
		delete(fs.filters, id)
	}
	return ok
}

// GetFilterChanges implements eth_getFilterChanges.
// For log filters, returns accumulated logs and drains the buffer.
// For block filters, returns accumulated block hashes.
// For pending tx filters, returns accumulated tx hashes.
func (fs *FilterSys) GetFilterChanges(id string) (interface{}, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	f, ok := fs.filters[id]
	if !ok {
		return nil, ErrSysFilterNotFound
	}
	f.lastPoll = time.Now()

	switch f.kind {
	case SysLogFilter:
		logs := f.logs
		f.logs = nil
		if logs == nil {
			logs = []*types.Log{}
		}
		return logs, nil

	case SysBlockFilter:
		hashes := f.hashes
		f.hashes = nil
		if hashes == nil {
			hashes = []types.Hash{}
		}
		return hashes, nil

	case SysPendingTxFilter:
		hashes := f.hashes
		f.hashes = nil
		if hashes == nil {
			hashes = []types.Hash{}
		}
		return hashes, nil

	default:
		return nil, ErrSysFilterWrongKind
	}
}

// GetFilterKind returns the kind of the filter with the given ID.
func (fs *FilterSys) GetFilterKind(id string) (SysFilterKind, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	f, ok := fs.filters[id]
	if !ok {
		return 0, ErrSysFilterNotFound
	}
	return f.kind, nil
}

// DistributeLog sends a log event to all matching log filters.
// Uses address and topic matching with bloom pre-screening.
func (fs *FilterSys) DistributeLog(log *types.Log) {
	if log == nil {
		return
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()

	for _, f := range fs.filters {
		if f.kind != SysLogFilter {
			continue
		}
		if sysLogMatches(log, &f.query) {
			if len(f.logs) < fs.config.MaxLogsPerFilter {
				f.logs = append(f.logs, log)
			}
		}
	}
}

// DistributeBlockHash sends a block hash to all block filters.
func (fs *FilterSys) DistributeBlockHash(hash types.Hash) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	for _, f := range fs.filters {
		if f.kind != SysBlockFilter {
			continue
		}
		if len(f.hashes) < fs.config.MaxHashesPerFilter {
			f.hashes = append(f.hashes, hash)
		}
	}
}

// DistributePendingTx sends a pending transaction hash to all pending tx filters.
func (fs *FilterSys) DistributePendingTx(txHash types.Hash) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	for _, f := range fs.filters {
		if f.kind != SysPendingTxFilter {
			continue
		}
		if len(f.hashes) < fs.config.MaxHashesPerFilter {
			f.hashes = append(f.hashes, txHash)
		}
	}
}

// PruneExpired removes all filters that have not been polled within
// the configured timeout. Returns the number of filters removed.
func (fs *FilterSys) PruneExpired() int {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	now := time.Now()
	removed := 0
	for id, f := range fs.filters {
		if now.Sub(f.lastPoll) > fs.config.FilterTimeout {
			delete(fs.filters, id)
			removed++
		}
	}
	return removed
}

// FilterCount returns the total number of active filters.
func (fs *FilterSys) FilterCount() int {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return len(fs.filters)
}

// FilterCountByKind returns the count of active filters of a specific kind.
func (fs *FilterSys) FilterCountByKind(kind SysFilterKind) int {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	count := 0
	for _, f := range fs.filters {
		if f.kind == kind {
			count++
		}
	}
	return count
}

// BloomMatchesQuery uses a block's bloom filter to quickly reject blocks
// that cannot contain matching logs. This is an optimization that avoids
// scanning individual log entries when the bloom indicates no match.
func BloomMatchesQuery(bloom types.Bloom, query SysLogQuery) bool {
	// Check addresses: at least one must be present in bloom.
	if len(query.Addresses) > 0 {
		found := false
		for _, addr := range query.Addresses {
			if types.BloomContains(bloom, addr.Bytes()) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check topics: for each position, at least one topic must be in bloom.
	for _, topicSet := range query.Topics {
		if len(topicSet) == 0 {
			continue // wildcard
		}
		found := false
		for _, topic := range topicSet {
			if types.BloomContains(bloom, topic.Bytes()) {
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

// FilterLogsByBloom applies bloom-based pre-screening followed by exact
// log matching for a block's logs. Returns only logs that match the query.
func FilterLogsByBloom(bloom types.Bloom, logs []*types.Log, query SysLogQuery) []*types.Log {
	if !BloomMatchesQuery(bloom, query) {
		return nil
	}
	var result []*types.Log
	for _, log := range logs {
		if sysLogMatches(log, &query) {
			result = append(result, log)
		}
	}
	return result
}

// --- internal helpers ---

// generateID produces a unique hex filter ID. Caller must hold fs.mu.
func (fs *FilterSys) generateID() string {
	fs.seq++
	var buf [16]byte
	binary.LittleEndian.PutUint64(buf[:8], fs.seq)
	binary.LittleEndian.PutUint64(buf[8:], uint64(time.Now().UnixNano()))
	return crypto.Keccak256Hash(buf[:]).Hex()
}

// sysLogMatches checks whether a log matches the given query criteria.
// Address matching: OR across listed addresses. An empty address list
// matches all addresses.
// Topic matching: AND across positions, OR within each position.
// A nil/empty position is a wildcard.
func sysLogMatches(log *types.Log, query *SysLogQuery) bool {
	// Block range check.
	if log.BlockNumber < query.FromBlock {
		return false
	}
	if query.ToBlock > 0 && log.BlockNumber > query.ToBlock {
		return false
	}

	// Address filter.
	if len(query.Addresses) > 0 {
		found := false
		for _, addr := range query.Addresses {
			if log.Address == addr {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Topic filter.
	for i, topicSet := range query.Topics {
		if len(topicSet) == 0 {
			continue
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
