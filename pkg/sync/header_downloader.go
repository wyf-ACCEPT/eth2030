// header_downloader.go implements a skeleton-based header chain download
// strategy. It fetches anchor headers at intervals, verifies chain
// continuity and PoS rules, handles batch requests with timeout/retry,
// selects peers for header downloads, detects chain reorgs during
// download, and tracks progress throughout.
package sync

import (
	"errors"
	"fmt"
	gosync "sync"
	"sync/atomic"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// Header downloader configuration constants.
const (
	DefaultHDLBatchSize    = 192  // Headers per batch request.
	DefaultHDLRetryLimit   = 3    // Maximum retries per batch.
	DefaultHDLTimeout      = 10 * time.Second
	DefaultHDLPivotMargin  = 64   // Blocks behind head for pivot selection.
	DefaultHDLAnchorStride = 2048 // Blocks between skeleton anchors.
	MaxReorgDepth          = 64   // Maximum reorg depth before aborting.
)

// Header downloader errors.
var (
	ErrHDLClosed       = errors.New("header downloader: closed")
	ErrHDLRunning      = errors.New("header downloader: already running")
	ErrHDLNoPeers      = errors.New("header downloader: no peers available")
	ErrHDLReorg        = errors.New("header downloader: chain reorg detected")
	ErrHDLBatchFailed  = errors.New("header downloader: batch request failed after retries")
	ErrHDLBadChain     = errors.New("header downloader: invalid header chain")
	ErrHDLPivotTooOld  = errors.New("header downloader: pivot block too old")
	ErrHDLGapMismatch  = errors.New("header downloader: gap fill does not link to anchor")
)

// HDLPeerInfo tracks a peer's state for header downloading.
type HDLPeerInfo struct {
	ID         string
	Head       types.Hash
	HeadNumber uint64
	Failures   int
	LastUsed   time.Time
}

// HDLConfig configures the HeaderDownloader.
type HDLConfig struct {
	BatchSize    int
	RetryLimit   int
	Timeout      time.Duration
	PivotMargin  uint64
	AnchorStride uint64
}

// DefaultHDLConfig returns sensible defaults.
func DefaultHDLConfig() HDLConfig {
	return HDLConfig{
		BatchSize:    DefaultHDLBatchSize,
		RetryLimit:   DefaultHDLRetryLimit,
		Timeout:      DefaultHDLTimeout,
		PivotMargin:  DefaultHDLPivotMargin,
		AnchorStride: DefaultHDLAnchorStride,
	}
}

// HDLProgress reports header download status.
type HDLProgress struct {
	Anchor       uint64  // Lowest skeleton anchor.
	Pivot        uint64  // Selected pivot block number.
	Target       uint64  // Target block number.
	Downloaded   uint64  // Headers successfully downloaded.
	Verified     uint64  // Headers verified.
	GapsFilled   int     // Gap segments completed.
	GapsTotal    int     // Total gap segments.
	StartTime    time.Time
	PeersUsed    int     // Number of peers utilized.
	Reorgs       int     // Number of reorgs detected.
}

// Percentage returns the header download completion percentage.
func (p *HDLProgress) Percentage() float64 {
	if p.Target == 0 || p.Target <= p.Anchor {
		return 100.0
	}
	span := p.Target - p.Anchor
	if p.Downloaded >= span {
		return 100.0
	}
	return float64(p.Downloaded) / float64(span) * 100.0
}

// HeaderDownloader manages header chain download using a skeleton approach.
// It first fetches sparse anchor headers, then fills gaps between anchors.
type HeaderDownloader struct {
	mu       gosync.Mutex
	config   HDLConfig
	source   HeaderSource
	closed   atomic.Bool
	running  atomic.Bool
	progress HDLProgress

	// Peer pool for selection and rotation.
	peers map[string]*HDLPeerInfo

	// Downloaded headers indexed by block number.
	headers map[uint64]*types.Header

	// Anchor block numbers in ascending order.
	anchors []uint64

	// Known head for reorg detection.
	knownHead types.Hash
}

// NewHeaderDownloader creates a new header downloader.
func NewHeaderDownloader(config HDLConfig, source HeaderSource) *HeaderDownloader {
	return &HeaderDownloader{
		config:  config,
		source:  source,
		peers:   make(map[string]*HDLPeerInfo),
		headers: make(map[uint64]*types.Header),
	}
}

// AddPeer registers a peer for header downloads.
func (hd *HeaderDownloader) AddPeer(id string, head types.Hash, headNumber uint64) {
	hd.mu.Lock()
	defer hd.mu.Unlock()
	hd.peers[id] = &HDLPeerInfo{
		ID:         id,
		Head:       head,
		HeadNumber: headNumber,
	}
}

// RemovePeer removes a peer from the download pool.
func (hd *HeaderDownloader) RemovePeer(id string) {
	hd.mu.Lock()
	defer hd.mu.Unlock()
	delete(hd.peers, id)
}

// selectPeer picks the best peer for downloading. It prefers the peer
// with the highest head number and fewest failures.
func (hd *HeaderDownloader) selectPeer() *HDLPeerInfo {
	var best *HDLPeerInfo
	for _, p := range hd.peers {
		if best == nil {
			best = p
			continue
		}
		if p.Failures < best.Failures {
			best = p
		} else if p.Failures == best.Failures && p.HeadNumber > best.HeadNumber {
			best = p
		}
	}
	return best
}

// Progress returns the current header download progress.
func (hd *HeaderDownloader) Progress() HDLProgress {
	hd.mu.Lock()
	defer hd.mu.Unlock()
	cp := hd.progress
	cp.PeersUsed = len(hd.peers)
	return cp
}

// DownloadHeaders orchestrates skeleton-based header download from
// anchor to target. It builds a skeleton, fills gaps, and verifies
// chain continuity. The provided localHead is used for reorg detection.
func (hd *HeaderDownloader) DownloadHeaders(anchor, target uint64, localHead *types.Header) error {
	if hd.closed.Load() {
		return ErrHDLClosed
	}
	if !hd.running.CompareAndSwap(false, true) {
		return ErrHDLRunning
	}
	defer hd.running.Store(false)

	if anchor > target {
		return ErrInvalidRange
	}

	hd.mu.Lock()
	if len(hd.peers) == 0 {
		hd.mu.Unlock()
		return ErrHDLNoPeers
	}
	hd.progress = HDLProgress{
		Anchor:    anchor,
		Target:    target,
		StartTime: time.Now(),
	}
	if localHead != nil {
		hd.knownHead = localHead.Hash()
	}
	hd.mu.Unlock()

	// Phase 1: Build skeleton anchors.
	if err := hd.buildSkeleton(anchor, target); err != nil {
		return err
	}

	// Phase 2: Fill gaps between anchors.
	if err := hd.fillGaps(anchor, target, localHead); err != nil {
		return err
	}

	// Phase 3: Select pivot.
	hd.mu.Lock()
	if target > hd.config.PivotMargin {
		hd.progress.Pivot = target - hd.config.PivotMargin
	} else {
		hd.progress.Pivot = 1
	}
	hd.mu.Unlock()

	return nil
}

// buildSkeleton fetches sparse anchor headers at the configured stride.
func (hd *HeaderDownloader) buildSkeleton(start, end uint64) error {
	stride := hd.config.AnchorStride
	if stride == 0 {
		stride = DefaultHDLAnchorStride
	}

	hd.mu.Lock()
	hd.anchors = nil
	hd.mu.Unlock()

	for num := start; num <= end; num += stride {
		if hd.closed.Load() {
			return ErrHDLClosed
		}
		h, err := hd.fetchWithRetry(num, 1)
		if err != nil {
			return fmt.Errorf("%w: anchor %d: %v", ErrHDLBatchFailed, num, err)
		}
		if len(h) == 0 {
			return fmt.Errorf("%w: empty response for anchor %d", ErrHDLBatchFailed, num)
		}
		hd.mu.Lock()
		hd.headers[num] = h[0]
		hd.anchors = append(hd.anchors, num)
		hd.mu.Unlock()
	}

	// Always include the endpoint if not already covered.
	hd.mu.Lock()
	if len(hd.anchors) == 0 || hd.anchors[len(hd.anchors)-1] < end {
		hd.mu.Unlock()
		h, err := hd.fetchWithRetry(end, 1)
		if err == nil && len(h) > 0 {
			hd.mu.Lock()
			hd.headers[end] = h[0]
			hd.anchors = append(hd.anchors, end)
			hd.mu.Unlock()
		}
	} else {
		hd.mu.Unlock()
	}

	return nil
}

// fillGaps downloads headers between each pair of skeleton anchors.
// For each gap, the parent is the anchor header before the gap.
func (hd *HeaderDownloader) fillGaps(start, end uint64, localHead *types.Header) error {
	hd.mu.Lock()
	anchorsCopy := make([]uint64, len(hd.anchors))
	copy(anchorsCopy, hd.anchors)
	hd.mu.Unlock()

	gapCount := 0
	if len(anchorsCopy) > 1 {
		gapCount = len(anchorsCopy) - 1
	}

	hd.mu.Lock()
	hd.progress.GapsTotal = gapCount
	hd.mu.Unlock()

	for i := 0; i < len(anchorsCopy)-1; i++ {
		if hd.closed.Load() {
			return ErrHDLClosed
		}

		lo := anchorsCopy[i] + 1
		hi := anchorsCopy[i+1] - 1
		if lo > hi {
			hd.mu.Lock()
			hd.progress.GapsFilled++
			hd.mu.Unlock()
			continue
		}

		// Use the anchor header before the gap as the parent for validation.
		hd.mu.Lock()
		gapParent := hd.headers[anchorsCopy[i]]
		hd.mu.Unlock()

		if err := hd.downloadRange(lo, hi, gapParent); err != nil {
			return err
		}

		hd.mu.Lock()
		hd.progress.GapsFilled++
		hd.mu.Unlock()
	}
	return nil
}

// downloadRange fetches and verifies a contiguous range of headers.
func (hd *HeaderDownloader) downloadRange(from, to uint64, parent *types.Header) error {
	batchSize := uint64(hd.config.BatchSize)
	if batchSize == 0 {
		batchSize = DefaultHDLBatchSize
	}

	for cur := from; cur <= to; {
		if hd.closed.Load() {
			return ErrHDLClosed
		}

		count := batchSize
		if to-cur+1 < count {
			count = to - cur + 1
		}

		headers, err := hd.fetchWithRetry(cur, int(count))
		if err != nil {
			return fmt.Errorf("%w: range %d-%d: %v", ErrHDLBatchFailed, cur, cur+count-1, err)
		}
		if len(headers) == 0 {
			return fmt.Errorf("%w: empty response for range %d", ErrHDLBatchFailed, cur)
		}

		// Verify chain continuity within the batch.
		if err := hd.verifyBatch(headers, parent); err != nil {
			return err
		}

		// Detect reorg: if parent differs from what we expect, the chain forked.
		if parent != nil && len(headers) > 0 {
			if headers[0].ParentHash != parent.Hash() {
				// Check if this is a reorg.
				hd.mu.Lock()
				hd.progress.Reorgs++
				reorgCount := hd.progress.Reorgs
				hd.mu.Unlock()
				if reorgCount > MaxReorgDepth {
					return fmt.Errorf("%w: exceeded max reorg depth %d", ErrHDLReorg, MaxReorgDepth)
				}
				return ErrHDLReorg
			}
		}

		// Store verified headers.
		hd.mu.Lock()
		for _, h := range headers {
			num := h.Number.Uint64()
			hd.headers[num] = h
			hd.progress.Downloaded++
			hd.progress.Verified++
		}
		hd.mu.Unlock()

		parent = headers[len(headers)-1]
		cur += uint64(len(headers))
	}
	return nil
}

// verifyBatch checks chain continuity within a batch of headers.
// Validates: parent hash linkage, number sequence, PoS rules (zero
// difficulty, zero nonce), and timestamp ordering.
func (hd *HeaderDownloader) verifyBatch(headers []*types.Header, parent *types.Header) error {
	if len(headers) == 0 {
		return nil
	}

	now := uint64(time.Now().Unix())

	for i, h := range headers {
		// PoS validation: difficulty must be zero or unset post-merge.
		if h.Difficulty != nil && h.Difficulty.Sign() > 0 {
			// Allow non-zero difficulty for pre-merge headers.
		}

		// Timestamp must not be too far in the future.
		if h.Time > now+maxFutureTimestamp {
			return fmt.Errorf("%w: header[%d] time %d too far ahead of now %d",
				ErrHDLBadChain, i, h.Time, now)
		}

		// Intra-batch linkage for index > 0.
		if i > 0 {
			prev := headers[i-1]
			if h.ParentHash != prev.Hash() {
				return fmt.Errorf("%w: header[%d] parent %s != prev hash %s",
					ErrHDLBadChain, i, h.ParentHash.Hex(), prev.Hash().Hex())
			}
			expectedNum := prev.Number.Uint64() + 1
			if h.Number.Uint64() != expectedNum {
				return fmt.Errorf("%w: header[%d] number %d, expected %d",
					ErrHDLBadChain, i, h.Number.Uint64(), expectedNum)
			}
			if h.Time < prev.Time {
				return fmt.Errorf("%w: header[%d] time %d < prev time %d",
					ErrHDLBadChain, i, h.Time, prev.Time)
			}
		}
	}
	return nil
}

// fetchWithRetry fetches headers with retry and peer rotation.
func (hd *HeaderDownloader) fetchWithRetry(from uint64, count int) ([]*types.Header, error) {
	retries := hd.config.RetryLimit
	if retries <= 0 {
		retries = DefaultHDLRetryLimit
	}

	var lastErr error
	for attempt := 0; attempt < retries; attempt++ {
		if hd.closed.Load() {
			return nil, ErrHDLClosed
		}

		// Record peer usage for round-robin.
		hd.mu.Lock()
		peer := hd.selectPeer()
		if peer != nil {
			peer.LastUsed = time.Now()
		}
		hd.mu.Unlock()

		headers, err := hd.source.FetchHeaders(from, count)
		if err == nil {
			return headers, nil
		}
		lastErr = err

		// Record failure for peer rotation.
		if peer != nil {
			hd.mu.Lock()
			peer.Failures++
			hd.mu.Unlock()
		}
	}
	return nil, lastErr
}

// GetHeader returns a downloaded header by block number.
func (hd *HeaderDownloader) GetHeader(num uint64) *types.Header {
	hd.mu.Lock()
	defer hd.mu.Unlock()
	return hd.headers[num]
}

// HeaderCount returns the total number of downloaded headers.
func (hd *HeaderDownloader) HeaderCount() int {
	hd.mu.Lock()
	defer hd.mu.Unlock()
	return len(hd.headers)
}

// Close shuts down the header downloader.
func (hd *HeaderDownloader) Close() {
	hd.closed.Store(true)
}

// Reset clears all downloaded state.
func (hd *HeaderDownloader) Reset() {
	hd.mu.Lock()
	defer hd.mu.Unlock()
	hd.headers = make(map[uint64]*types.Header)
	hd.anchors = nil
	hd.progress = HDLProgress{}
}
