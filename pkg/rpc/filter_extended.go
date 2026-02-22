package rpc

import (
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Extended filter system errors.
var (
	ErrFilterNotFound      = errors.New("filter: not found")
	ErrFilterWrongType     = errors.New("filter: wrong type for operation")
	ErrFilterLimitReached  = errors.New("filter: maximum active filters reached")
	ErrFilterExpired       = errors.New("filter: expired due to inactivity")
	ErrFilterTopicMismatch = errors.New("filter: topic position count exceeds 4")
	ErrFilterBlockRange    = errors.New("filter: invalid block range")
	ErrFilterLogOverflow   = errors.New("filter: log buffer full")
)

// ExtFilterType distinguishes filter kinds in the unified filter manager.
type ExtFilterType uint8

const (
	// ExtLogFilter tracks log events matching address/topic criteria.
	ExtLogFilter ExtFilterType = iota
	// ExtBlockFilter tracks new block hashes.
	ExtBlockFilter
	// ExtPendingTxFilter tracks new pending transaction hashes.
	ExtPendingTxFilter
)

// MaxTopicPositions is the maximum number of topic positions in a log filter.
const MaxTopicPositions = 4

// ExtFilterConfig holds configuration for the extended filter manager.
type ExtFilterConfig struct {
	MaxFilters       int
	FilterTimeout    time.Duration
	MaxLogsPerFilter int
	MaxHashBuffer    int
	PruneInterval    time.Duration
}

// DefaultExtFilterConfig returns sensible defaults.
func DefaultExtFilterConfig() ExtFilterConfig {
	return ExtFilterConfig{
		MaxFilters:       256,
		FilterTimeout:    5 * time.Minute,
		MaxLogsPerFilter: 10000,
		MaxHashBuffer:    1024,
		PruneInterval:    30 * time.Second,
	}
}

// ExtFilter is a unified filter entry managing logs, blocks, or pending txs.
type ExtFilter struct {
	ID        string
	Type      ExtFilterType
	CreatedAt time.Time
	LastPoll  time.Time

	// Log filter fields (only used when Type == ExtLogFilter).
	FromBlock uint64
	ToBlock   uint64
	Addresses []types.Address
	Topics    [][]types.Hash
	Logs      []*types.Log
	LastBlock uint64

	// Block/PendingTx filter fields.
	Hashes []types.Hash
}

// ExtFilterManager manages all filter types with timeout-based lifecycle,
// concurrent-safe access, and automatic pruning of expired filters.
type ExtFilterManager struct {
	mu      sync.RWMutex
	config  ExtFilterConfig
	filters map[string]*ExtFilter
	seq     uint64
	closed  atomic.Bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewExtFilterManager creates a new filter manager with the given config.
func NewExtFilterManager(config ExtFilterConfig) *ExtFilterManager {
	if config.MaxFilters <= 0 {
		config.MaxFilters = 256
	}
	if config.FilterTimeout <= 0 {
		config.FilterTimeout = 5 * time.Minute
	}
	if config.MaxLogsPerFilter <= 0 {
		config.MaxLogsPerFilter = 10000
	}
	if config.MaxHashBuffer <= 0 {
		config.MaxHashBuffer = 1024
	}
	if config.PruneInterval <= 0 {
		config.PruneInterval = 30 * time.Second
	}
	fm := &ExtFilterManager{
		config:  config,
		filters: make(map[string]*ExtFilter),
		stopCh:  make(chan struct{}),
	}
	return fm
}

// StartPruner starts a background goroutine that periodically removes
// expired filters. Call Stop() to terminate.
func (fm *ExtFilterManager) StartPruner() {
	fm.wg.Add(1)
	go func() {
		defer fm.wg.Done()
		ticker := time.NewTicker(fm.config.PruneInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				fm.PruneExpired()
			case <-fm.stopCh:
				return
			}
		}
	}()
}

// Stop halts the background pruner and marks the manager as closed.
func (fm *ExtFilterManager) Stop() {
	if fm.closed.CompareAndSwap(false, true) {
		close(fm.stopCh)
		fm.wg.Wait()
	}
}

// generateID produces a unique hex filter ID.
func (fm *ExtFilterManager) generateID() string {
	fm.seq++
	var buf [16]byte
	binary.LittleEndian.PutUint64(buf[:8], fm.seq)
	binary.LittleEndian.PutUint64(buf[8:], uint64(time.Now().UnixNano()))
	h := crypto.Keccak256Hash(buf[:])
	return h.Hex()
}

// InstallLogFilter creates a new log filter with the given criteria.
func (fm *ExtFilterManager) InstallLogFilter(
	fromBlock, toBlock uint64,
	addresses []types.Address,
	topics [][]types.Hash,
) (string, error) {
	if len(topics) > MaxTopicPositions {
		return "", ErrFilterTopicMismatch
	}
	if toBlock > 0 && fromBlock > toBlock {
		return "", ErrFilterBlockRange
	}

	fm.mu.Lock()
	defer fm.mu.Unlock()

	if len(fm.filters) >= fm.config.MaxFilters {
		return "", ErrFilterLimitReached
	}

	now := time.Now()
	id := fm.generateID()
	fm.filters[id] = &ExtFilter{
		ID:        id,
		Type:      ExtLogFilter,
		CreatedAt: now,
		LastPoll:  now,
		FromBlock: fromBlock,
		ToBlock:   toBlock,
		Addresses: addresses,
		Topics:    topics,
		LastBlock: fromBlock,
	}
	return id, nil
}

// InstallBlockFilter creates a new block filter.
func (fm *ExtFilterManager) InstallBlockFilter() (string, error) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	if len(fm.filters) >= fm.config.MaxFilters {
		return "", ErrFilterLimitReached
	}

	now := time.Now()
	id := fm.generateID()
	fm.filters[id] = &ExtFilter{
		ID:        id,
		Type:      ExtBlockFilter,
		CreatedAt: now,
		LastPoll:  now,
	}
	return id, nil
}

// InstallPendingTxFilter creates a new pending transaction filter.
func (fm *ExtFilterManager) InstallPendingTxFilter() (string, error) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	if len(fm.filters) >= fm.config.MaxFilters {
		return "", ErrFilterLimitReached
	}

	now := time.Now()
	id := fm.generateID()
	fm.filters[id] = &ExtFilter{
		ID:        id,
		Type:      ExtPendingTxFilter,
		CreatedAt: now,
		LastPoll:  now,
	}
	return id, nil
}

// Uninstall removes a filter by ID. Returns true if found.
func (fm *ExtFilterManager) Uninstall(id string) bool {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	_, ok := fm.filters[id]
	if ok {
		delete(fm.filters, id)
	}
	return ok
}

// GetFilterType returns the type of a filter, or an error if not found.
func (fm *ExtFilterManager) GetFilterType(id string) (ExtFilterType, error) {
	fm.mu.RLock()
	defer fm.mu.RUnlock()
	f, ok := fm.filters[id]
	if !ok {
		return 0, ErrFilterNotFound
	}
	return f.Type, nil
}

// PollLogFilter retrieves and drains accumulated logs from a log filter.
func (fm *ExtFilterManager) PollLogFilter(id string) ([]*types.Log, error) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	f, ok := fm.filters[id]
	if !ok {
		return nil, ErrFilterNotFound
	}
	if f.Type != ExtLogFilter {
		return nil, ErrFilterWrongType
	}

	f.LastPoll = time.Now()
	logs := f.Logs
	f.Logs = nil
	if logs == nil {
		logs = []*types.Log{}
	}
	return logs, nil
}

// PollBlockFilter retrieves and drains accumulated block hashes.
func (fm *ExtFilterManager) PollBlockFilter(id string) ([]types.Hash, error) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	f, ok := fm.filters[id]
	if !ok {
		return nil, ErrFilterNotFound
	}
	if f.Type != ExtBlockFilter {
		return nil, ErrFilterWrongType
	}

	f.LastPoll = time.Now()
	hashes := f.Hashes
	f.Hashes = nil
	if hashes == nil {
		hashes = []types.Hash{}
	}
	return hashes, nil
}

// PollPendingTxFilter retrieves and drains accumulated pending tx hashes.
func (fm *ExtFilterManager) PollPendingTxFilter(id string) ([]types.Hash, error) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	f, ok := fm.filters[id]
	if !ok {
		return nil, ErrFilterNotFound
	}
	if f.Type != ExtPendingTxFilter {
		return nil, ErrFilterWrongType
	}

	f.LastPoll = time.Now()
	hashes := f.Hashes
	f.Hashes = nil
	if hashes == nil {
		hashes = []types.Hash{}
	}
	return hashes, nil
}

// DistributeLog sends a log to all matching log filters. A log matches if
// the address is in the filter's list (or list is empty) and topics match
// with AND across positions, OR within each position.
func (fm *ExtFilterManager) DistributeLog(log *types.Log) {
	if log == nil {
		return
	}
	fm.mu.Lock()
	defer fm.mu.Unlock()

	for _, f := range fm.filters {
		if f.Type != ExtLogFilter {
			continue
		}
		if matchesExtFilter(log, f) {
			if len(f.Logs) < fm.config.MaxLogsPerFilter {
				f.Logs = append(f.Logs, log)
			}
		}
	}
}

// DistributeBlockHash sends a block hash to all block filters.
func (fm *ExtFilterManager) DistributeBlockHash(hash types.Hash) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	for _, f := range fm.filters {
		if f.Type != ExtBlockFilter {
			continue
		}
		if len(f.Hashes) < fm.config.MaxHashBuffer {
			f.Hashes = append(f.Hashes, hash)
		}
	}
}

// DistributePendingTx sends a pending tx hash to all pending tx filters.
func (fm *ExtFilterManager) DistributePendingTx(hash types.Hash) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	for _, f := range fm.filters {
		if f.Type != ExtPendingTxFilter {
			continue
		}
		if len(f.Hashes) < fm.config.MaxHashBuffer {
			f.Hashes = append(f.Hashes, hash)
		}
	}
}

// PruneExpired removes all filters that haven't been polled within the
// timeout. Returns the number of filters removed.
func (fm *ExtFilterManager) PruneExpired() int {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	now := time.Now()
	removed := 0
	for id, f := range fm.filters {
		if now.Sub(f.LastPoll) > fm.config.FilterTimeout {
			delete(fm.filters, id)
			removed++
		}
	}
	return removed
}

// Count returns the number of active filters.
func (fm *ExtFilterManager) Count() int {
	fm.mu.RLock()
	defer fm.mu.RUnlock()
	return len(fm.filters)
}

// CountByType returns the number of active filters of the given type.
func (fm *ExtFilterManager) CountByType(ft ExtFilterType) int {
	fm.mu.RLock()
	defer fm.mu.RUnlock()
	count := 0
	for _, f := range fm.filters {
		if f.Type == ft {
			count++
		}
	}
	return count
}

// matchesExtFilter checks if a log matches a filter's criteria.
func matchesExtFilter(log *types.Log, f *ExtFilter) bool {
	// Block range check.
	if log.BlockNumber < f.FromBlock {
		return false
	}
	if f.ToBlock > 0 && log.BlockNumber > f.ToBlock {
		return false
	}

	// Address filter: OR across addresses.
	if len(f.Addresses) > 0 {
		found := false
		for _, addr := range f.Addresses {
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
	for i, topicSet := range f.Topics {
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
