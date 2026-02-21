// bandwidth_tracker.go implements network bandwidth monitoring and throttling.
// It tracks per-peer upload/download rates, enforces global bandwidth limits,
// supports priority-based allocation (consensus > blocks > txs > blobs), and
// uses sliding window rate calculation.
package p2p

import (
	"errors"
	"sync"
	"time"
)

// Bandwidth priority classes ordered from highest to lowest priority.
const (
	BandwidthPriorityConsensus = iota // Consensus messages (attestations, proposals).
	BandwidthPriorityBlocks           // Block propagation.
	BandwidthPriorityTxs              // Transaction gossip.
	BandwidthPriorityBlobs            // Blob sidecar data.
	bandwidthPriorityCount            // Sentinel: number of priority classes.
)

// Default bandwidth tracker constants.
const (
	// DefaultGlobalUploadLimit is the default global upload rate limit (100 MB/s).
	DefaultGlobalUploadLimit = 100 * 1024 * 1024

	// DefaultGlobalDownloadLimit is the default global download rate limit (100 MB/s).
	DefaultGlobalDownloadLimit = 100 * 1024 * 1024

	// DefaultPerPeerUploadLimit is the default per-peer upload rate limit (10 MB/s).
	DefaultPerPeerUploadLimit = 10 * 1024 * 1024

	// DefaultPerPeerDownloadLimit is the default per-peer download rate limit (10 MB/s).
	DefaultPerPeerDownloadLimit = 10 * 1024 * 1024

	// DefaultWindowSize is the default sliding window size for rate calculation.
	DefaultWindowSize = 10 * time.Second

	// DefaultBucketCount is the default number of buckets in the sliding window.
	DefaultBucketCount = 10

	// priorityShareConsensus is the bandwidth share for consensus traffic (40%).
	priorityShareConsensus = 40

	// priorityShareBlocks is the bandwidth share for block traffic (30%).
	priorityShareBlocks = 30

	// priorityShareTxs is the bandwidth share for transaction traffic (20%).
	priorityShareTxs = 20

	// priorityShareBlobs is the bandwidth share for blob traffic (10%).
	priorityShareBlobs = 10
)

// Bandwidth tracker errors.
var (
	ErrBWGlobalUploadLimit    = errors.New("bw: global upload limit exceeded")
	ErrBWGlobalDownloadLimit  = errors.New("bw: global download limit exceeded")
	ErrBWPeerUploadLimit      = errors.New("bw: per-peer upload limit exceeded")
	ErrBWPeerDownloadLimit    = errors.New("bw: per-peer download limit exceeded")
	ErrBWPriorityExhausted    = errors.New("bw: priority class bandwidth exhausted")
	ErrBWUnknownPeer          = errors.New("bw: unknown peer")
)

// BandwidthTrackerConfig configures the BandwidthTracker.
type BandwidthTrackerConfig struct {
	GlobalUploadLimit    int64         // Global upload bytes per second limit.
	GlobalDownloadLimit  int64         // Global download bytes per second limit.
	PerPeerUploadLimit   int64         // Per-peer upload bytes per second limit.
	PerPeerDownloadLimit int64         // Per-peer download bytes per second limit.
	WindowSize           time.Duration // Sliding window duration for rate measurement.
	BucketCount          int           // Number of buckets within the window.
}

// DefaultBandwidthTrackerConfig returns production defaults.
func DefaultBandwidthTrackerConfig() BandwidthTrackerConfig {
	return BandwidthTrackerConfig{
		GlobalUploadLimit:    DefaultGlobalUploadLimit,
		GlobalDownloadLimit:  DefaultGlobalDownloadLimit,
		PerPeerUploadLimit:   DefaultPerPeerUploadLimit,
		PerPeerDownloadLimit: DefaultPerPeerDownloadLimit,
		WindowSize:           DefaultWindowSize,
		BucketCount:          DefaultBucketCount,
	}
}

// slidingWindow tracks byte counts over a sliding time window divided into
// discrete buckets for efficient rate calculation.
type slidingWindow struct {
	buckets    []int64
	bucketSize time.Duration
	total      int64
	startTime  time.Time
	headIdx    int // Current bucket index.
}

func newSlidingWindow(windowSize time.Duration, bucketCount int) *slidingWindow {
	if bucketCount <= 0 {
		bucketCount = DefaultBucketCount
	}
	bucketSize := windowSize / time.Duration(bucketCount)
	if bucketSize <= 0 {
		bucketSize = time.Second
	}
	return &slidingWindow{
		buckets:    make([]int64, bucketCount),
		bucketSize: bucketSize,
		startTime:  time.Now(),
	}
}

// advance moves the window head forward to the current time, zeroing expired
// buckets. Returns the current bucket index.
func (sw *slidingWindow) advance(now time.Time) int {
	elapsed := now.Sub(sw.startTime)
	currentBucket := int(elapsed / sw.bucketSize)
	n := len(sw.buckets)

	// If the head has advanced, clear any expired buckets between old head
	// and new position.
	diff := currentBucket - sw.headIdx
	if diff > n {
		diff = n
	}
	if diff > 0 {
		for i := 1; i <= diff; i++ {
			idx := (sw.headIdx + i) % n
			sw.total -= sw.buckets[idx]
			sw.buckets[idx] = 0
		}
		sw.headIdx = currentBucket
	}
	return currentBucket % n
}

// add records bytes into the current time bucket.
func (sw *slidingWindow) add(bytes int64, now time.Time) {
	idx := sw.advance(now)
	sw.buckets[idx] += bytes
	sw.total += bytes
}

// rate returns the current byte rate per second over the sliding window.
func (sw *slidingWindow) rate(now time.Time) float64 {
	sw.advance(now)
	windowSec := float64(len(sw.buckets)) * sw.bucketSize.Seconds()
	if windowSec <= 0 {
		return 0
	}
	return float64(sw.total) / windowSec
}

// totalBytes returns the total bytes tracked in the current window.
func (sw *slidingWindow) totalBytes(now time.Time) int64 {
	sw.advance(now)
	return sw.total
}

// peerBandwidth tracks upload/download rates for a single peer.
type peerBandwidth struct {
	upload   *slidingWindow
	download *slidingWindow
}

// priorityAllocation tracks bandwidth usage per priority class.
type priorityAllocation struct {
	used  [bandwidthPriorityCount]*slidingWindow
	share [bandwidthPriorityCount]int // Percentage share per priority.
}

// BandwidthStats holds a snapshot of bandwidth statistics for a peer.
type BandwidthStats struct {
	PeerID       string
	UploadRate   float64 // Bytes per second upload.
	DownloadRate float64 // Bytes per second download.
	TotalUp      int64   // Total bytes uploaded in window.
	TotalDown    int64   // Total bytes downloaded in window.
}

// GlobalBandwidthStats holds a snapshot of global bandwidth statistics.
type GlobalBandwidthStats struct {
	UploadRate       float64 // Global upload bytes per second.
	DownloadRate     float64 // Global download bytes per second.
	TotalUp          int64   // Total upload bytes in window.
	TotalDown        int64   // Total download bytes in window.
	PeerCount        int     // Number of tracked peers.
	PriorityRates    [bandwidthPriorityCount]float64 // Per-priority upload rate.
}

// BandwidthTracker monitors network bandwidth per peer and globally, enforces
// rate limits, and supports priority-based allocation. All methods are safe
// for concurrent use.
type BandwidthTracker struct {
	mu       sync.RWMutex
	config   BandwidthTrackerConfig
	peers    map[string]*peerBandwidth
	globalUp *slidingWindow
	globalDn *slidingWindow
	priority priorityAllocation
}

// NewBandwidthTracker creates a BandwidthTracker with the given config.
func NewBandwidthTracker(config BandwidthTrackerConfig) *BandwidthTracker {
	if config.GlobalUploadLimit <= 0 {
		config.GlobalUploadLimit = DefaultGlobalUploadLimit
	}
	if config.GlobalDownloadLimit <= 0 {
		config.GlobalDownloadLimit = DefaultGlobalDownloadLimit
	}
	if config.PerPeerUploadLimit <= 0 {
		config.PerPeerUploadLimit = DefaultPerPeerUploadLimit
	}
	if config.PerPeerDownloadLimit <= 0 {
		config.PerPeerDownloadLimit = DefaultPerPeerDownloadLimit
	}
	if config.WindowSize <= 0 {
		config.WindowSize = DefaultWindowSize
	}
	if config.BucketCount <= 0 {
		config.BucketCount = DefaultBucketCount
	}

	bt := &BandwidthTracker{
		config:   config,
		peers:    make(map[string]*peerBandwidth),
		globalUp: newSlidingWindow(config.WindowSize, config.BucketCount),
		globalDn: newSlidingWindow(config.WindowSize, config.BucketCount),
	}

	// Initialize priority allocation.
	bt.priority.share = [bandwidthPriorityCount]int{
		priorityShareConsensus,
		priorityShareBlocks,
		priorityShareTxs,
		priorityShareBlobs,
	}
	for i := 0; i < bandwidthPriorityCount; i++ {
		bt.priority.used[i] = newSlidingWindow(config.WindowSize, config.BucketCount)
	}

	return bt
}

// RegisterPeer adds a peer to the bandwidth tracker.
func (bt *BandwidthTracker) RegisterPeer(peerID string) {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	if _, ok := bt.peers[peerID]; !ok {
		bt.peers[peerID] = &peerBandwidth{
			upload:   newSlidingWindow(bt.config.WindowSize, bt.config.BucketCount),
			download: newSlidingWindow(bt.config.WindowSize, bt.config.BucketCount),
		}
	}
}

// RemovePeer removes a peer from the tracker.
func (bt *BandwidthTracker) RemovePeer(peerID string) {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	delete(bt.peers, peerID)
}

// RecordUpload records an upload of the given number of bytes to a peer
// with the specified priority class. Returns an error if any limit would
// be exceeded.
func (bt *BandwidthTracker) RecordUpload(peerID string, bytes int64, priority int) error {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	now := time.Now()

	// Check global upload limit.
	globalRate := bt.globalUp.rate(now)
	if globalRate+float64(bytes) > float64(bt.config.GlobalUploadLimit) {
		return ErrBWGlobalUploadLimit
	}

	// Check per-peer upload limit.
	pb, ok := bt.peers[peerID]
	if !ok {
		return ErrBWUnknownPeer
	}
	peerRate := pb.upload.rate(now)
	if peerRate+float64(bytes) > float64(bt.config.PerPeerUploadLimit) {
		return ErrBWPeerUploadLimit
	}

	// Check priority allocation.
	if priority >= 0 && priority < bandwidthPriorityCount {
		if err := bt.checkPriorityLocked(priority, bytes, now); err != nil {
			return err
		}
		bt.priority.used[priority].add(bytes, now)
	}

	// Record the bytes.
	pb.upload.add(bytes, now)
	bt.globalUp.add(bytes, now)
	return nil
}

// RecordDownload records a download of the given number of bytes from a peer
// with the specified priority class. Returns an error if any limit would
// be exceeded.
func (bt *BandwidthTracker) RecordDownload(peerID string, bytes int64, priority int) error {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	now := time.Now()

	// Check global download limit.
	globalRate := bt.globalDn.rate(now)
	if globalRate+float64(bytes) > float64(bt.config.GlobalDownloadLimit) {
		return ErrBWGlobalDownloadLimit
	}

	// Check per-peer download limit.
	pb, ok := bt.peers[peerID]
	if !ok {
		return ErrBWUnknownPeer
	}
	peerRate := pb.download.rate(now)
	if peerRate+float64(bytes) > float64(bt.config.PerPeerDownloadLimit) {
		return ErrBWPeerDownloadLimit
	}

	// Record the bytes.
	pb.download.add(bytes, now)
	bt.globalDn.add(bytes, now)
	return nil
}

// checkPriorityLocked verifies that the given priority class has enough
// allocated bandwidth. Caller must hold bt.mu.
func (bt *BandwidthTracker) checkPriorityLocked(priority int, bytes int64, now time.Time) error {
	if priority < 0 || priority >= bandwidthPriorityCount {
		return nil
	}
	share := bt.priority.share[priority]
	// Allocated limit for this priority = globalUploadLimit * share / 100.
	allocated := float64(bt.config.GlobalUploadLimit) * float64(share) / 100.0
	currentRate := bt.priority.used[priority].rate(now)
	if currentRate+float64(bytes) > allocated {
		return ErrBWPriorityExhausted
	}
	return nil
}

// PeerStats returns bandwidth statistics for a specific peer.
func (bt *BandwidthTracker) PeerStats(peerID string) (BandwidthStats, error) {
	bt.mu.RLock()
	defer bt.mu.RUnlock()

	pb, ok := bt.peers[peerID]
	if !ok {
		return BandwidthStats{}, ErrBWUnknownPeer
	}
	now := time.Now()
	return BandwidthStats{
		PeerID:       peerID,
		UploadRate:   pb.upload.rate(now),
		DownloadRate: pb.download.rate(now),
		TotalUp:      pb.upload.totalBytes(now),
		TotalDown:    pb.download.totalBytes(now),
	}, nil
}

// GlobalStats returns a snapshot of global bandwidth statistics.
func (bt *BandwidthTracker) GlobalStats() GlobalBandwidthStats {
	bt.mu.RLock()
	defer bt.mu.RUnlock()

	now := time.Now()
	stats := GlobalBandwidthStats{
		UploadRate:   bt.globalUp.rate(now),
		DownloadRate: bt.globalDn.rate(now),
		TotalUp:      bt.globalUp.totalBytes(now),
		TotalDown:    bt.globalDn.totalBytes(now),
		PeerCount:    len(bt.peers),
	}
	for i := 0; i < bandwidthPriorityCount; i++ {
		stats.PriorityRates[i] = bt.priority.used[i].rate(now)
	}
	return stats
}

// PeerCount returns the number of tracked peers.
func (bt *BandwidthTracker) PeerCount() int {
	bt.mu.RLock()
	defer bt.mu.RUnlock()
	return len(bt.peers)
}

// UploadRate returns the global upload rate in bytes per second.
func (bt *BandwidthTracker) UploadRate() float64 {
	bt.mu.RLock()
	defer bt.mu.RUnlock()
	return bt.globalUp.rate(time.Now())
}

// DownloadRate returns the global download rate in bytes per second.
func (bt *BandwidthTracker) DownloadRate() float64 {
	bt.mu.RLock()
	defer bt.mu.RUnlock()
	return bt.globalDn.rate(time.Now())
}

// PriorityShare returns the bandwidth share percentage for a priority class.
func (bt *BandwidthTracker) PriorityShare(priority int) int {
	bt.mu.RLock()
	defer bt.mu.RUnlock()
	if priority < 0 || priority >= bandwidthPriorityCount {
		return 0
	}
	return bt.priority.share[priority]
}

// PriorityRate returns the current upload rate for a priority class.
func (bt *BandwidthTracker) PriorityRate(priority int) float64 {
	bt.mu.RLock()
	defer bt.mu.RUnlock()
	if priority < 0 || priority >= bandwidthPriorityCount {
		return 0
	}
	return bt.priority.used[priority].rate(time.Now())
}

// AllPeerStats returns bandwidth stats for all tracked peers.
func (bt *BandwidthTracker) AllPeerStats() []BandwidthStats {
	bt.mu.RLock()
	defer bt.mu.RUnlock()

	now := time.Now()
	result := make([]BandwidthStats, 0, len(bt.peers))
	for id, pb := range bt.peers {
		result = append(result, BandwidthStats{
			PeerID:       id,
			UploadRate:   pb.upload.rate(now),
			DownloadRate: pb.download.rate(now),
			TotalUp:      pb.upload.totalBytes(now),
			TotalDown:    pb.download.totalBytes(now),
		})
	}
	return result
}

// PriorityName returns a human-readable name for a priority class.
func PriorityName(priority int) string {
	switch priority {
	case BandwidthPriorityConsensus:
		return "consensus"
	case BandwidthPriorityBlocks:
		return "blocks"
	case BandwidthPriorityTxs:
		return "transactions"
	case BandwidthPriorityBlobs:
		return "blobs"
	default:
		return "unknown"
	}
}
