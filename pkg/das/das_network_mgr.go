// das_network_mgr.go integrates DAS with the P2P network layer, coordinating
// sampling, gossip, and custody per the PeerDAS spec. It provides a high-level
// manager for performing data availability sampling rounds and tracking
// peer quality.
package das

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/sha3"
)

// DAS network manager errors.
var (
	ErrNetworkNotStarted   = errors.New("das/netmgr: network manager not started")
	ErrInvalidSubnet       = errors.New("das/netmgr: invalid subnet ID")
	ErrInvalidSampleCount  = errors.New("das/netmgr: invalid sample count")
	ErrSamplingFailed      = errors.New("das/netmgr: sampling round failed")
	ErrPeerNotFound        = errors.New("das/netmgr: peer not found")
	ErrNoAvailablePeers    = errors.New("das/netmgr: no available peers for column")
	ErrAlreadySubscribed   = errors.New("das/netmgr: already subscribed to subnet")
	ErrColumnPublishFailed = errors.New("das/netmgr: column publish failed")
)

// NetworkConfig configures the DAS network manager.
type NetworkConfig struct {
	// SamplesPerSlot is the minimum number of samples per slot.
	SamplesPerSlot int

	// NumberOfColumns is the total column count.
	NumberOfColumns uint64

	// SubnetCount is the number of gossip subnets.
	SubnetCount uint64

	// AvailabilityThreshold is the fraction (0.0-1.0) of successful samples
	// needed to confirm data availability. Default: 0.5.
	AvailabilityThreshold float64

	// MaxPeerScore is the maximum score a peer can accumulate.
	MaxPeerScore float64

	// MinPeerScore is the minimum score before a peer is considered bad.
	MinPeerScore float64

	// InitialPeerScore is the starting score for new peers.
	InitialPeerScore float64
}

// DefaultNetworkConfig returns the default network manager configuration.
func DefaultNetworkConfig() NetworkConfig {
	return NetworkConfig{
		SamplesPerSlot:        SamplesPerSlot,
		NumberOfColumns:       NumberOfColumns,
		SubnetCount:           DataColumnSidecarSubnetCount,
		AvailabilityThreshold: 0.5,
		MaxPeerScore:          100.0,
		MinPeerScore:          -50.0,
		InitialPeerScore:      10.0,
	}
}

// SampleResult records the outcome of a single column sample request.
type SampleResult struct {
	// ColumnIndex is the column that was sampled.
	ColumnIndex uint64

	// Success indicates whether the sample was retrieved successfully.
	Success bool

	// PeerID is the peer that served (or failed to serve) the sample.
	PeerID [32]byte

	// Latency is how long the sample request took.
	Latency time.Duration
}

// SamplingResult aggregates the outcomes of a full sampling round.
type SamplingResult struct {
	// Slot is the slot for which sampling was performed.
	Slot uint64

	// TotalSamples is the total number of samples requested.
	TotalSamples int

	// Successful is the number of samples retrieved successfully.
	Successful int

	// Failed is the number of samples that failed.
	Failed int

	// AvailabilityConfirmed is true if the threshold was met.
	AvailabilityConfirmed bool

	// Results contains the individual sample outcomes.
	Results []SampleResult

	// Duration is the total time the sampling round took.
	Duration time.Duration
}

// PublishedColumn records a column published to a subnet.
type PublishedColumn struct {
	Column DataColumn
	Subnet uint64
	Time   time.Time
}

// NetworkMetrics tracks operational metrics for the DAS network manager.
type NetworkMetrics struct {
	// TotalSamplingRounds is the total number of sampling rounds performed.
	TotalSamplingRounds uint64

	// TotalSamplesRequested is the total number of individual samples requested.
	TotalSamplesRequested uint64

	// TotalSamplesSucceeded is the total number of successful samples.
	TotalSamplesSucceeded uint64

	// TotalSamplesFailed is the total number of failed samples.
	TotalSamplesFailed uint64

	// AvailabilityConfirmedCount is how many rounds confirmed availability.
	AvailabilityConfirmedCount uint64

	// AvailabilityDeniedCount is how many rounds denied availability.
	AvailabilityDeniedCount uint64
}

// peerScore tracks a peer's quality for DAS queries.
type peerScore struct {
	score        float64
	totalQueries uint64
	successes    uint64
	failures     uint64
	totalLatency time.Duration
}

// DASNetworkManager coordinates DAS sampling, gossip, and custody with
// the P2P network layer. It tracks peer quality, manages subnet subscriptions,
// and provides metrics on sampling performance. All methods are thread-safe.
type DASNetworkManager struct {
	mu      sync.RWMutex
	config  NetworkConfig
	started atomic.Bool

	// subscriptions tracks which subnets this node is subscribed to.
	subscriptions map[uint64]bool

	// peerScores maps peer IDs to their quality scores.
	peerScores map[[32]byte]*peerScore

	// published tracks recently published columns (ring buffer style).
	published []PublishedColumn

	// columnStore caches columns received from sampling for local serving.
	columnStore map[uint64]*DataColumn

	// metrics tracks aggregate operational stats.
	metrics NetworkMetrics

	// custody is the custody subnet manager for peer discovery.
	custody *CustodySubnetManager

	// sampleProvider is a pluggable function that simulates fetching a
	// sample from a peer. In production, this would call the P2P layer.
	// If nil, sampling always fails (used for testing with custom providers).
	sampleProvider SampleProvider
}

// SampleProvider is a function that attempts to fetch a column sample from
// a specific peer. Returns the data column and latency, or an error.
type SampleProvider func(peerID [32]byte, columnIndex uint64) (*DataColumn, time.Duration, error)

// NewDASNetworkManager creates a new DAS network manager.
func NewDASNetworkManager(config NetworkConfig, custody *CustodySubnetManager) *DASNetworkManager {
	return &DASNetworkManager{
		config:        config,
		subscriptions: make(map[uint64]bool),
		peerScores:    make(map[[32]byte]*peerScore),
		columnStore:   make(map[uint64]*DataColumn),
		custody:       custody,
	}
}

// Start starts the network manager.
func (nm *DASNetworkManager) Start() {
	nm.started.Store(true)
}

// Stop stops the network manager.
func (nm *DASNetworkManager) Stop() {
	nm.started.Store(false)
}

// IsStarted returns whether the network manager is running.
func (nm *DASNetworkManager) IsStarted() bool {
	return nm.started.Load()
}

// SetSampleProvider sets the function used to fetch samples from peers.
func (nm *DASNetworkManager) SetSampleProvider(provider SampleProvider) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	nm.sampleProvider = provider
}

// SubscribeToSubnets subscribes the node to the given subnets.
func (nm *DASNetworkManager) SubscribeToSubnets(subnets []uint64) error {
	if !nm.IsStarted() {
		return ErrNetworkNotStarted
	}

	nm.mu.Lock()
	defer nm.mu.Unlock()

	for _, s := range subnets {
		if s >= nm.config.SubnetCount {
			return fmt.Errorf("%w: %d >= %d", ErrInvalidSubnet, s, nm.config.SubnetCount)
		}
		nm.subscriptions[s] = true
	}
	return nil
}

// Subscriptions returns the current set of subscribed subnets.
func (nm *DASNetworkManager) Subscriptions() []uint64 {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	subs := make([]uint64, 0, len(nm.subscriptions))
	for s := range nm.subscriptions {
		subs = append(subs, s)
	}
	sort.Slice(subs, func(i, j int) bool { return subs[i] < subs[j] })
	return subs
}

// IsSubscribed returns true if the node is subscribed to the given subnet.
func (nm *DASNetworkManager) IsSubscribed(subnet uint64) bool {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	return nm.subscriptions[subnet]
}

// PublishColumn publishes a data column to the specified subnet. In production
// this would gossip the column to subnet peers; here it records the publish
// event and stores the column locally.
func (nm *DASNetworkManager) PublishColumn(col DataColumn, subnet uint64) error {
	if !nm.IsStarted() {
		return ErrNetworkNotStarted
	}
	if subnet >= nm.config.SubnetCount {
		return fmt.Errorf("%w: %d >= %d", ErrInvalidSubnet, subnet, nm.config.SubnetCount)
	}
	if uint64(col.Index) >= nm.config.NumberOfColumns {
		return fmt.Errorf("%w: column index %d out of range", ErrColumnPublishFailed, col.Index)
	}

	nm.mu.Lock()
	defer nm.mu.Unlock()

	// Record the publish event.
	nm.published = append(nm.published, PublishedColumn{
		Column: col,
		Subnet: subnet,
		Time:   time.Now(),
	})

	// Store for local serving.
	colCopy := col
	nm.columnStore[uint64(col.Index)] = &colCopy

	return nil
}

// PublishedColumns returns a copy of all published columns.
func (nm *DASNetworkManager) PublishedColumns() []PublishedColumn {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	result := make([]PublishedColumn, len(nm.published))
	copy(result, nm.published)
	return result
}

// PerformSampling initiates a sampling round for the given slot. It selects
// sampleCount random columns (deterministically from the slot), attempts to
// fetch each from an available peer, and returns the aggregate result.
func (nm *DASNetworkManager) PerformSampling(slot uint64, sampleCount int) (*SamplingResult, error) {
	if !nm.IsStarted() {
		return nil, ErrNetworkNotStarted
	}
	if sampleCount <= 0 {
		return nil, fmt.Errorf("%w: %d", ErrInvalidSampleCount, sampleCount)
	}

	start := time.Now()

	// Deterministically select columns to sample.
	columns := nm.selectSampleColumns(slot, sampleCount)

	results := make([]SampleResult, len(columns))
	successful := 0
	failed := 0

	nm.mu.RLock()
	provider := nm.sampleProvider
	nm.mu.RUnlock()

	for i, colIdx := range columns {
		result := SampleResult{ColumnIndex: colIdx}

		// Try to find a peer that custodies this column.
		var peerID [32]byte
		var foundPeer bool
		if nm.custody != nil {
			peers, err := nm.custody.FindPeersForColumn(colIdx)
			if err == nil && len(peers) > 0 {
				// Select best peer by score.
				peerID = nm.selectBestPeer(peers)
				foundPeer = true
			}
		}

		if foundPeer && provider != nil {
			col, latency, err := provider(peerID, colIdx)
			result.PeerID = peerID
			result.Latency = latency
			if err == nil && col != nil {
				result.Success = true
				successful++
				nm.recordPeerSuccess(peerID, latency)
			} else {
				result.Success = false
				failed++
				nm.recordPeerFailure(peerID)
			}
		} else {
			// Check local store as fallback.
			nm.mu.RLock()
			_, localOk := nm.columnStore[colIdx]
			nm.mu.RUnlock()

			if localOk {
				result.Success = true
				successful++
			} else {
				result.Success = false
				failed++
			}
		}

		results[i] = result
	}

	duration := time.Since(start)

	// Determine availability.
	threshold := nm.config.AvailabilityThreshold
	available := false
	if sampleCount > 0 {
		ratio := float64(successful) / float64(sampleCount)
		available = ratio >= threshold
	}

	samplingResult := &SamplingResult{
		Slot:                  slot,
		TotalSamples:          sampleCount,
		Successful:            successful,
		Failed:                failed,
		AvailabilityConfirmed: available,
		Results:               results,
		Duration:              duration,
	}

	// Update metrics.
	nm.mu.Lock()
	nm.metrics.TotalSamplingRounds++
	nm.metrics.TotalSamplesRequested += uint64(sampleCount)
	nm.metrics.TotalSamplesSucceeded += uint64(successful)
	nm.metrics.TotalSamplesFailed += uint64(failed)
	if available {
		nm.metrics.AvailabilityConfirmedCount++
	} else {
		nm.metrics.AvailabilityDeniedCount++
	}
	nm.mu.Unlock()

	return samplingResult, nil
}

// selectSampleColumns deterministically selects random column indices to
// sample for a given slot.
func (nm *DASNetworkManager) selectSampleColumns(slot uint64, count int) []uint64 {
	if nm.config.NumberOfColumns == 0 {
		return nil
	}

	// Seed from slot.
	h := sha3.NewLegacyKeccak256()
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], slot)
	h.Write(buf[:])
	// Add a domain separator.
	h.Write([]byte("das-sampling"))
	seed := h.Sum(nil)

	seen := make(map[uint64]bool)
	columns := make([]uint64, 0, count)
	counter := uint64(0)

	for len(columns) < count {
		sh := sha3.NewLegacyKeccak256()
		sh.Write(seed)
		binary.LittleEndian.PutUint64(buf[:], counter)
		sh.Write(buf[:])
		digest := sh.Sum(nil)

		val := binary.LittleEndian.Uint64(digest[:8])
		col := val % nm.config.NumberOfColumns

		if !seen[col] {
			seen[col] = true
			columns = append(columns, col)
		}
		counter++

		// Safety: prevent infinite loop if count > NumberOfColumns.
		if counter > nm.config.NumberOfColumns*4 && len(columns) >= int(nm.config.NumberOfColumns) {
			break
		}
	}

	sort.Slice(columns, func(i, j int) bool { return columns[i] < columns[j] })
	return columns
}

// selectBestPeer selects the peer with the highest score from the candidates.
func (nm *DASNetworkManager) selectBestPeer(peers [][32]byte) [32]byte {
	if len(peers) == 0 {
		return [32]byte{}
	}

	nm.mu.RLock()
	defer nm.mu.RUnlock()

	best := peers[0]
	bestScore := nm.config.MinPeerScore
	for _, p := range peers {
		ps, ok := nm.peerScores[p]
		if !ok {
			// Unknown peer gets initial score.
			if nm.config.InitialPeerScore > bestScore {
				bestScore = nm.config.InitialPeerScore
				best = p
			}
		} else if ps.score > bestScore {
			bestScore = ps.score
			best = p
		}
	}
	return best
}

// recordPeerSuccess records a successful sample from a peer.
func (nm *DASNetworkManager) recordPeerSuccess(peerID [32]byte, latency time.Duration) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	ps := nm.getOrCreatePeerScore(peerID)
	ps.totalQueries++
	ps.successes++
	ps.totalLatency += latency

	// Reward: +1 score capped at MaxPeerScore.
	ps.score += 1.0
	if ps.score > nm.config.MaxPeerScore {
		ps.score = nm.config.MaxPeerScore
	}
}

// recordPeerFailure records a failed sample from a peer.
func (nm *DASNetworkManager) recordPeerFailure(peerID [32]byte) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	ps := nm.getOrCreatePeerScore(peerID)
	ps.totalQueries++
	ps.failures++

	// Penalize: -5 score, floored at MinPeerScore.
	ps.score -= 5.0
	if ps.score < nm.config.MinPeerScore {
		ps.score = nm.config.MinPeerScore
	}
}

// getOrCreatePeerScore returns the score entry for a peer, creating one if
// it doesn't exist. Caller must hold nm.mu write lock.
func (nm *DASNetworkManager) getOrCreatePeerScore(peerID [32]byte) *peerScore {
	ps, ok := nm.peerScores[peerID]
	if !ok {
		ps = &peerScore{score: nm.config.InitialPeerScore}
		nm.peerScores[peerID] = ps
	}
	return ps
}

// PeerScore returns the current score for a peer. Returns (score, exists).
func (nm *DASNetworkManager) PeerScore(peerID [32]byte) (float64, bool) {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	ps, ok := nm.peerScores[peerID]
	if !ok {
		return 0, false
	}
	return ps.score, true
}

// PeerSuccessRate returns the success rate for a peer's DAS queries.
// Returns (rate, exists).
func (nm *DASNetworkManager) PeerSuccessRate(peerID [32]byte) (float64, bool) {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	ps, ok := nm.peerScores[peerID]
	if !ok {
		return 0, false
	}
	if ps.totalQueries == 0 {
		return 0, true
	}
	return float64(ps.successes) / float64(ps.totalQueries), true
}

// PeerAverageLatency returns the average sample latency for a peer.
// Returns (latency, exists).
func (nm *DASNetworkManager) PeerAverageLatency(peerID [32]byte) (time.Duration, bool) {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	ps, ok := nm.peerScores[peerID]
	if !ok {
		return 0, false
	}
	if ps.successes == 0 {
		return 0, true
	}
	return ps.totalLatency / time.Duration(ps.successes), true
}

// Metrics returns a snapshot of the current network metrics.
func (nm *DASNetworkManager) Metrics() NetworkMetrics {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	return nm.metrics
}

// SamplingSuccessRate returns the overall ratio of successful samples to
// total samples requested.
func (nm *DASNetworkManager) SamplingSuccessRate() float64 {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	if nm.metrics.TotalSamplesRequested == 0 {
		return 0
	}
	return float64(nm.metrics.TotalSamplesSucceeded) / float64(nm.metrics.TotalSamplesRequested)
}

// StoreColumn stores a column locally, making it available for sampling.
func (nm *DASNetworkManager) StoreColumn(col *DataColumn) {
	if col == nil {
		return
	}
	nm.mu.Lock()
	defer nm.mu.Unlock()
	colCopy := *col
	nm.columnStore[uint64(col.Index)] = &colCopy
}

// GetStoredColumn retrieves a locally stored column.
func (nm *DASNetworkManager) GetStoredColumn(columnIndex uint64) (*DataColumn, bool) {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	col, ok := nm.columnStore[columnIndex]
	return col, ok
}
