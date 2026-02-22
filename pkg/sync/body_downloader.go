// body_downloader.go implements block body download with transaction
// and withdrawal list validation, concurrent multi-peer downloads,
// priority ordering (prefer older blocks), and retry logic with peer
// rotation.
package sync

import (
	"errors"
	"fmt"
	gosync "sync"
	"sync/atomic"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Body downloader configuration constants.
const (
	DefaultBDLBatchSize     = 64   // Bodies per batch request.
	DefaultBDLMaxConcurrent = 4    // Maximum concurrent peer downloads.
	DefaultBDLRetryLimit    = 3    // Max retries per batch.
	DefaultBDLTimeout       = 15 * time.Second
)

// Body downloader errors.
var (
	ErrBDLClosed          = errors.New("body downloader: closed")
	ErrBDLRunning         = errors.New("body downloader: already running")
	ErrBDLNoPeers         = errors.New("body downloader: no peers available")
	ErrBDLTxRootMismatch  = errors.New("body downloader: transaction root mismatch")
	ErrBDLWdRootMismatch  = errors.New("body downloader: withdrawals root mismatch")
	ErrBDLBodyMissing     = errors.New("body downloader: body not returned for hash")
	ErrBDLRetryExhausted  = errors.New("body downloader: retry limit exhausted")
)

// BDLPeerInfo tracks a peer used for body downloads.
type BDLPeerInfo struct {
	ID         string
	Fetcher    BodySource
	ActiveJobs int
	Failures   int
	LastUsed   time.Time
}

// BDLConfig configures the BodyDownloader.
type BDLConfig struct {
	BatchSize     int
	MaxConcurrent int
	RetryLimit    int
}

// DefaultBDLConfig returns sensible defaults.
func DefaultBDLConfig() BDLConfig {
	return BDLConfig{
		BatchSize:     DefaultBDLBatchSize,
		MaxConcurrent: DefaultBDLMaxConcurrent,
		RetryLimit:    DefaultBDLRetryLimit,
	}
}

// bodyRequest tracks a pending body download keyed by header hash.
type bodyRequest struct {
	Header   *types.Header
	Hash     types.Hash
	Priority uint64 // block number; lower = higher priority
	Retries  int
	PeerID   string
}

// BDLProgress tracks body download status.
type BDLProgress struct {
	Requested  uint64 // Bodies requested.
	Downloaded uint64 // Bodies successfully downloaded.
	Validated  uint64 // Bodies that passed validation.
	Failed     uint64 // Bodies that failed validation.
	Pending    int    // Currently pending requests.
	StartTime  time.Time
}

// BodyDownloader coordinates block body downloads from multiple peers.
// Requests are keyed by header hash and prioritized by block number
// (older blocks first).
type BodyDownloader struct {
	mu       gosync.Mutex
	config   BDLConfig
	closed   atomic.Bool
	running  atomic.Bool
	progress BDLProgress

	// Peer pool for concurrent downloads.
	peers map[string]*BDLPeerInfo

	// Fallback single body source (used when no peers registered).
	fallback BodySource

	// Pending requests, indexed by block hash.
	pending map[types.Hash]*bodyRequest

	// Downloaded bodies, indexed by block hash.
	bodies map[types.Hash]*types.Body
}

// NewBodyDownloader creates a new body downloader.
func NewBodyDownloader(config BDLConfig, fallback BodySource) *BodyDownloader {
	return &BodyDownloader{
		config:   config,
		fallback: fallback,
		peers:    make(map[string]*BDLPeerInfo),
		pending:  make(map[types.Hash]*bodyRequest),
		bodies:   make(map[types.Hash]*types.Body),
	}
}

// AddPeer registers a peer for body downloads.
func (bd *BodyDownloader) AddPeer(id string, fetcher BodySource) {
	bd.mu.Lock()
	defer bd.mu.Unlock()
	bd.peers[id] = &BDLPeerInfo{ID: id, Fetcher: fetcher}
}

// RemovePeer removes a peer from the download pool.
func (bd *BodyDownloader) RemovePeer(id string) {
	bd.mu.Lock()
	defer bd.mu.Unlock()
	delete(bd.peers, id)
}

// selectPeerLocked picks the best available peer (fewest active jobs,
// fewest failures). Caller must hold bd.mu.
func (bd *BodyDownloader) selectPeerLocked() *BDLPeerInfo {
	var best *BDLPeerInfo
	for _, p := range bd.peers {
		if bd.config.MaxConcurrent > 0 && p.ActiveJobs >= bd.config.MaxConcurrent {
			continue
		}
		if best == nil {
			best = p
			continue
		}
		if p.ActiveJobs < best.ActiveJobs {
			best = p
		} else if p.ActiveJobs == best.ActiveJobs && p.Failures < best.Failures {
			best = p
		}
	}
	return best
}

// ScheduleHeaders adds headers whose bodies need to be downloaded.
// Bodies are prioritized by block number (lower numbers first).
func (bd *BodyDownloader) ScheduleHeaders(headers []*types.Header) {
	bd.mu.Lock()
	defer bd.mu.Unlock()

	for _, h := range headers {
		hash := h.Hash()
		if _, exists := bd.pending[hash]; exists {
			continue
		}
		if _, exists := bd.bodies[hash]; exists {
			continue
		}
		bd.pending[hash] = &bodyRequest{
			Header:   h,
			Hash:     hash,
			Priority: h.Number.Uint64(),
		}
		bd.progress.Requested++
	}
	bd.progress.Pending = len(bd.pending)
}

// DownloadBodies downloads bodies for all scheduled headers. It fetches
// in priority order (oldest blocks first), validates tx and withdrawal
// roots, and retries with peer rotation on failure.
func (bd *BodyDownloader) DownloadBodies() error {
	if bd.closed.Load() {
		return ErrBDLClosed
	}

	bd.mu.Lock()
	bd.progress.StartTime = time.Now()
	bd.mu.Unlock()

	for {
		if bd.closed.Load() {
			return ErrBDLClosed
		}

		// Collect next batch of pending requests (oldest first).
		batch := bd.nextBatch()
		if len(batch) == 0 {
			return nil // All done.
		}

		// Build hash list for the batch.
		hashes := make([]types.Hash, len(batch))
		for i, req := range batch {
			hashes[i] = req.Hash
		}

		// Fetch bodies.
		bodies, err := bd.fetchBodiesWithRetry(hashes)
		if err != nil {
			return err
		}

		// Validate and store each body.
		for i, req := range batch {
			if i >= len(bodies) || bodies[i] == nil {
				bd.handleRetry(req)
				continue
			}

			body := bodies[i]
			if err := bd.validateBody(req.Header, body); err != nil {
				bd.mu.Lock()
				bd.progress.Failed++
				bd.mu.Unlock()
				bd.handleRetry(req)
				continue
			}

			bd.mu.Lock()
			bd.bodies[req.Hash] = body
			delete(bd.pending, req.Hash)
			bd.progress.Downloaded++
			bd.progress.Validated++
			bd.progress.Pending = len(bd.pending)
			bd.mu.Unlock()
		}
	}
}

// nextBatch returns the next batch of pending requests, sorted by
// priority (lowest block number first).
func (bd *BodyDownloader) nextBatch() []*bodyRequest {
	bd.mu.Lock()
	defer bd.mu.Unlock()

	batchSize := bd.config.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultBDLBatchSize
	}

	// Collect all pending requests.
	all := make([]*bodyRequest, 0, len(bd.pending))
	for _, req := range bd.pending {
		all = append(all, req)
	}
	if len(all) == 0 {
		return nil
	}

	// Sort by priority (block number, ascending).
	for i := 1; i < len(all); i++ {
		for j := i; j > 0 && all[j].Priority < all[j-1].Priority; j-- {
			all[j], all[j-1] = all[j-1], all[j]
		}
	}

	if len(all) > batchSize {
		all = all[:batchSize]
	}
	return all
}

// validateBody verifies a body against its header. Checks:
//   - Transaction root matches header.TxHash
//   - Withdrawal root matches header.WithdrawalsHash (if present)
func (bd *BodyDownloader) validateBody(header *types.Header, body *types.Body) error {
	// Validate transaction root.
	txRoot := computeTxRoot(body.Transactions)
	if txRoot != header.TxHash {
		return fmt.Errorf("%w: block %d got %s, want %s",
			ErrBDLTxRootMismatch, header.Number.Uint64(), txRoot.Hex(), header.TxHash.Hex())
	}

	// Validate withdrawals root if the header has a WithdrawalsHash.
	if header.WithdrawalsHash != nil {
		wdRoot := types.WithdrawalsRoot(body.Withdrawals)
		if wdRoot != *header.WithdrawalsHash {
			return fmt.Errorf("%w: block %d got %s, want %s",
				ErrBDLWdRootMismatch, header.Number.Uint64(), wdRoot.Hex(), header.WithdrawalsHash.Hex())
		}
	}

	return nil
}

// computeTxRoot computes a simple transaction root by hashing the
// concatenated transaction hashes. This mirrors the simplified approach
// used by the types package.
func computeTxRoot(txs []*types.Transaction) types.Hash {
	if len(txs) == 0 {
		return types.EmptyRootHash
	}
	var data []byte
	for _, tx := range txs {
		h := tx.Hash()
		data = append(data, h[:]...)
	}
	return crypto.Keccak256Hash(data)
}

// fetchBodiesWithRetry fetches bodies with retry and peer rotation.
func (bd *BodyDownloader) fetchBodiesWithRetry(hashes []types.Hash) ([]*types.Body, error) {
	retries := bd.config.RetryLimit
	if retries <= 0 {
		retries = DefaultBDLRetryLimit
	}

	var lastErr error
	for attempt := 0; attempt < retries; attempt++ {
		if bd.closed.Load() {
			return nil, ErrBDLClosed
		}

		fetcher := bd.pickFetcher()
		if fetcher == nil {
			return nil, ErrBDLNoPeers
		}

		bodies, err := fetcher.FetchBodies(hashes)
		if err == nil && len(bodies) == len(hashes) {
			return bodies, nil
		}
		if err == nil {
			err = fmt.Errorf("%w: got %d bodies for %d hashes",
				ErrBDLBodyMissing, len(bodies), len(hashes))
		}
		lastErr = err
	}
	return nil, fmt.Errorf("%w: %v", ErrBDLRetryExhausted, lastErr)
}

// pickFetcher selects a BodySource from registered peers or the fallback.
func (bd *BodyDownloader) pickFetcher() BodySource {
	bd.mu.Lock()
	defer bd.mu.Unlock()

	peer := bd.selectPeerLocked()
	if peer != nil {
		peer.ActiveJobs++
		peer.LastUsed = time.Now()
		return peer.Fetcher
	}
	return bd.fallback
}

// handleRetry re-queues a failed request or marks it permanently failed.
func (bd *BodyDownloader) handleRetry(req *bodyRequest) {
	bd.mu.Lock()
	defer bd.mu.Unlock()

	req.Retries++
	if req.Retries >= bd.config.RetryLimit {
		delete(bd.pending, req.Hash)
		bd.progress.Failed++
	} else {
		req.PeerID = "" // Allow reassignment.
	}
	bd.progress.Pending = len(bd.pending)
}

// GetBody returns a downloaded body by block hash.
func (bd *BodyDownloader) GetBody(hash types.Hash) *types.Body {
	bd.mu.Lock()
	defer bd.mu.Unlock()
	return bd.bodies[hash]
}

// Progress returns the current body download progress.
func (bd *BodyDownloader) Progress() BDLProgress {
	bd.mu.Lock()
	defer bd.mu.Unlock()
	return bd.progress
}

// PendingCount returns the number of pending body requests.
func (bd *BodyDownloader) PendingCount() int {
	bd.mu.Lock()
	defer bd.mu.Unlock()
	return len(bd.pending)
}

// DownloadedCount returns the number of bodies downloaded.
func (bd *BodyDownloader) DownloadedCount() int {
	bd.mu.Lock()
	defer bd.mu.Unlock()
	return len(bd.bodies)
}

// Close shuts down the body downloader.
func (bd *BodyDownloader) Close() {
	bd.closed.Store(true)
}

// Reset clears all state.
func (bd *BodyDownloader) Reset() {
	bd.mu.Lock()
	defer bd.mu.Unlock()
	bd.pending = make(map[types.Hash]*bodyRequest)
	bd.bodies = make(map[types.Hash]*types.Body)
	bd.progress = BDLProgress{}
}
