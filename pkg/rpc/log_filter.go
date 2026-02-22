package rpc

import (
	"encoding/binary"
	"errors"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// LogFilterConfig holds configuration for the log filter engine.
type LogFilterConfig struct {
	// MaxTopics is the maximum number of topic positions per filter.
	MaxTopics int
	// MaxBlocks is the maximum block range a filter can span.
	MaxBlocks uint64
	// MaxLogs is the maximum number of logs the engine will index.
	MaxLogs int
}

// DefaultLogFilterConfig returns a LogFilterConfig with sensible defaults.
func DefaultLogFilterConfig() LogFilterConfig {
	return LogFilterConfig{
		MaxTopics: 4,
		MaxBlocks: 10000,
		MaxLogs:   100000,
	}
}

// LogFilterSpec describes a log filter's matching criteria.
type LogFilterSpec struct {
	ID        string
	FromBlock uint64
	ToBlock   uint64
	Addresses []types.Address
	Topics    [][]types.Hash
}

// FilteredLog represents a log event that has been indexed by the engine.
type FilteredLog struct {
	Address     types.Address
	Topics      []types.Hash
	Data        []byte
	BlockNumber uint64
	TxHash      types.Hash
	TxIndex     uint64
	LogIndex    uint64
}

// LogFilterEngine manages log filters and provides log indexing/querying.
type LogFilterEngine struct {
	mu      sync.RWMutex
	config  LogFilterConfig
	filters map[string]*LogFilterSpec
	logs    []FilteredLog
	nextSeq uint64
}

// NewLogFilterEngine creates a new LogFilterEngine with the given config.
func NewLogFilterEngine(config LogFilterConfig) *LogFilterEngine {
	return &LogFilterEngine{
		config:  config,
		filters: make(map[string]*LogFilterSpec),
	}
}

// CreateFilter creates a new log filter and returns its ID.
// Returns an error if the filter exceeds configured limits.
func (e *LogFilterEngine) CreateFilter(filter LogFilterSpec) (string, error) {
	// Validate topic count.
	if len(filter.Topics) > e.config.MaxTopics {
		return "", errors.New("too many topic positions")
	}

	// Validate block range.
	if filter.ToBlock > 0 && filter.FromBlock > filter.ToBlock {
		return "", errors.New("fromBlock exceeds toBlock")
	}
	if filter.ToBlock > 0 && (filter.ToBlock-filter.FromBlock) > e.config.MaxBlocks {
		return "", errors.New("block range exceeds maximum")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	id := e.generateID()
	filter.ID = id
	stored := filter // copy
	e.filters[id] = &stored
	return id, nil
}

// DeleteFilter removes a filter by ID. Returns an error if not found.
func (e *LogFilterEngine) DeleteFilter(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, ok := e.filters[id]; !ok {
		return errors.New("filter not found")
	}
	delete(e.filters, id)
	return nil
}

// GetFilterLogs returns all indexed logs matching the given filter ID.
func (e *LogFilterEngine) GetFilterLogs(id string) []FilteredLog {
	e.mu.RLock()
	defer e.mu.RUnlock()

	filter, ok := e.filters[id]
	if !ok {
		return nil
	}

	var result []FilteredLog
	for _, log := range e.logs {
		if MatchesFilter(log, *filter) {
			result = append(result, log)
		}
	}
	return result
}

// AddLog adds a log to the engine's index. If the engine has reached
// its MaxLogs capacity, the log is silently dropped.
func (e *LogFilterEngine) AddLog(log FilteredLog) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if len(e.logs) >= e.config.MaxLogs {
		return
	}
	e.logs = append(e.logs, log)
}

// MatchesFilter checks whether a log matches the given filter criteria.
// Address matching is OR-based (match any listed address).
// Topic matching follows Ethereum convention: AND across positions,
// OR within each position. Empty/nil positions are wildcards.
func MatchesFilter(log FilteredLog, filter LogFilterSpec) bool {
	// Block range check.
	if log.BlockNumber < filter.FromBlock {
		return false
	}
	if filter.ToBlock > 0 && log.BlockNumber > filter.ToBlock {
		return false
	}

	// Address filter: log must match any listed address.
	if len(filter.Addresses) > 0 {
		found := false
		for _, addr := range filter.Addresses {
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
	for i, topicSet := range filter.Topics {
		if len(topicSet) == 0 {
			continue // wildcard position
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

// ActiveFilters returns the number of currently active filters.
func (e *LogFilterEngine) ActiveFilters() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.filters)
}

// LogCount returns the total number of indexed logs.
func (e *LogFilterEngine) LogCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.logs)
}

// generateID produces a unique filter ID. Caller must hold e.mu.
func (e *LogFilterEngine) generateID() string {
	e.nextSeq++
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], e.nextSeq)
	h := crypto.Keccak256Hash(buf[:])
	return h.Hex()
}
