// chain_downloader.go provides a high-level block chain downloader that
// coordinates peer selection, block range downloads, header chain
// validation, and block announcement handling. It builds on the existing
// sync primitives (Syncer, BlockDownloader) to offer a self-contained
// download manager for full sync.
package sync

import (
	"context"
	"errors"
	"fmt"
	gosync "sync"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// Chain downloader errors.
var (
	ErrChainNoPeers     = errors.New("chain_downloader: no peers available")
	ErrChainInvalidNum  = errors.New("chain_downloader: invalid block number sequence")
	ErrChainParentHash  = errors.New("chain_downloader: parent hash mismatch")
	ErrChainTimeout     = errors.New("chain_downloader: operation timed out")
	ErrChainBadRange    = errors.New("chain_downloader: from > to in download range")
	ErrChainEmptyBatch  = errors.New("chain_downloader: empty header or body batch")
)

// Default chain downloader constants.
const (
	DefaultCDMaxPeers   = 25
	DefaultCDMaxBlocks  = 1024
	DefaultCDBatchSize  = 64
	DefaultCDTimeout    = 15 * time.Second
)

// DownloadConfig configures the ChainDownloader.
type DownloadConfig struct {
	MaxPeers   int           // maximum number of tracked peers
	MaxBlocks  int           // max blocks to download per round
	BatchSize  int           // number of headers per fetch request
	Timeout    time.Duration // per-request timeout
}

// DefaultDownloadConfig returns a DownloadConfig with sensible defaults.
func DefaultDownloadConfig() *DownloadConfig {
	return &DownloadConfig{
		MaxPeers:  DefaultCDMaxPeers,
		MaxBlocks: DefaultCDMaxBlocks,
		BatchSize: DefaultCDBatchSize,
		Timeout:   DefaultCDTimeout,
	}
}

// PeerInfo describes a connected peer's chain state.
type PeerInfo struct {
	ID              string
	Address         string
	HeadHash        [32]byte
	HeadNumber      uint64
	TotalDifficulty uint64
}

// DownloadProgress tracks the state of an in-progress chain download.
type DownloadProgress struct {
	StartBlock     uint64
	CurrentBlock   uint64
	HighestBlock   uint64
	PeersConnected uint64
}

// Percentage returns the download completion as a value in [0, 100].
func (dp *DownloadProgress) Percentage() float64 {
	total := dp.HighestBlock - dp.StartBlock
	if total == 0 {
		return 100.0
	}
	done := dp.CurrentBlock - dp.StartBlock
	return float64(done) / float64(total) * 100.0
}

// ChainDownloader coordinates downloading a contiguous block range from
// a set of peers. It validates header chains, processes bodies, and
// handles block announcements from peers.
type ChainDownloader struct {
	mu     gosync.RWMutex
	config *DownloadConfig

	peers        map[string]*PeerInfo
	announcements []announcement

	progress DownloadProgress

	// Data sources (pluggable for testing).
	headerSource HeaderSource
	bodySource   BodySource
}

// announcement tracks a new block announcement from a peer.
type announcement struct {
	PeerID string
	Hash   [32]byte
	Number uint64
	Time   time.Time
}

// NewChainDownloader creates a new ChainDownloader with the given config.
// If config is nil, defaults are used.
func NewChainDownloader(cfg *DownloadConfig) *ChainDownloader {
	if cfg == nil {
		cfg = DefaultDownloadConfig()
	}
	return &ChainDownloader{
		config: cfg,
		peers:  make(map[string]*PeerInfo),
	}
}

// SetSources configures the header and body data sources.
func (cd *ChainDownloader) SetSources(hs HeaderSource, bs BodySource) {
	cd.mu.Lock()
	defer cd.mu.Unlock()
	cd.headerSource = hs
	cd.bodySource = bs
}

// AddPeer registers a peer with its current chain state. If the peer
// limit is reached, the peer with the lowest total difficulty is replaced.
func (cd *ChainDownloader) AddPeer(p PeerInfo) {
	cd.mu.Lock()
	defer cd.mu.Unlock()

	if _, exists := cd.peers[p.ID]; exists {
		cd.peers[p.ID] = &p
		return
	}

	if cd.config.MaxPeers > 0 && len(cd.peers) >= cd.config.MaxPeers {
		// Evict the peer with the lowest total difficulty.
		var worstID string
		var worstTD uint64
		first := true
		for id, peer := range cd.peers {
			if first || peer.TotalDifficulty < worstTD {
				worstID = id
				worstTD = peer.TotalDifficulty
				first = false
			}
		}
		if p.TotalDifficulty > worstTD {
			delete(cd.peers, worstID)
		} else {
			return // new peer is worse, don't add
		}
	}

	cd.peers[p.ID] = &p
}

// RemovePeer unregisters a peer.
func (cd *ChainDownloader) RemovePeer(id string) {
	cd.mu.Lock()
	defer cd.mu.Unlock()
	delete(cd.peers, id)
}

// PeerCount returns the number of tracked peers.
func (cd *ChainDownloader) PeerCount() int {
	cd.mu.RLock()
	defer cd.mu.RUnlock()
	return len(cd.peers)
}

// SelectBestPeer returns the peer ID with the highest total difficulty.
func (cd *ChainDownloader) SelectBestPeer() (string, error) {
	cd.mu.RLock()
	defer cd.mu.RUnlock()

	if len(cd.peers) == 0 {
		return "", ErrChainNoPeers
	}

	var bestID string
	var bestTD uint64
	for id, p := range cd.peers {
		if p.TotalDifficulty > bestTD {
			bestID = id
			bestTD = p.TotalDifficulty
		}
	}
	return bestID, nil
}

// HandleAnnouncement records a block announcement from a peer and updates
// the peer's head state.
func (cd *ChainDownloader) HandleAnnouncement(peerID string, hash [32]byte, number uint64) {
	cd.mu.Lock()
	defer cd.mu.Unlock()

	ann := announcement{
		PeerID: peerID,
		Hash:   hash,
		Number: number,
		Time:   time.Now(),
	}
	cd.announcements = append(cd.announcements, ann)

	// Cap the announcement queue.
	if len(cd.announcements) > 1000 {
		cd.announcements = cd.announcements[len(cd.announcements)-500:]
	}

	// Update peer head if this is a higher block.
	if p, ok := cd.peers[peerID]; ok {
		if number > p.HeadNumber {
			p.HeadNumber = number
			p.HeadHash = hash
		}
	}
}

// Download fetches and validates blocks in the range [from, to] inclusive.
// It downloads headers in batches, validates the chain, then fetches the
// corresponding bodies. The context allows cancellation. Returns nil
// on success.
func (cd *ChainDownloader) Download(ctx context.Context, from, to uint64) error {
	if from > to {
		return ErrChainBadRange
	}

	cd.mu.Lock()
	if cd.headerSource == nil || cd.bodySource == nil {
		cd.mu.Unlock()
		return errors.New("chain_downloader: sources not configured")
	}
	cd.progress = DownloadProgress{
		StartBlock:     from,
		CurrentBlock:   from,
		HighestBlock:   to,
		PeersConnected: uint64(len(cd.peers)),
	}
	cd.mu.Unlock()

	batchSize := cd.config.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultCDBatchSize
	}

	current := from
	for current <= to {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		count := to - current + 1
		if count > uint64(batchSize) {
			count = uint64(batchSize)
		}

		// Fetch headers.
		headers, err := cd.fetchWithTimeout(ctx, current, int(count))
		if err != nil {
			return fmt.Errorf("chain_downloader: fetch headers at %d: %w", current, err)
		}
		if len(headers) == 0 {
			return ErrChainEmptyBatch
		}

		// Validate the chain linkage within this batch.
		if err := cd.ValidateChain(headers); err != nil {
			return err
		}

		// Fetch bodies.
		hashes := make([]types.Hash, len(headers))
		for i, h := range headers {
			hashes[i] = h.Hash()
		}

		bodies, err := cd.bodySource.FetchBodies(hashes)
		if err != nil {
			return fmt.Errorf("chain_downloader: fetch bodies: %w", err)
		}

		if err := cd.ProcessBatch(headers, bodies); err != nil {
			return err
		}

		// Advance.
		lastNum := headers[len(headers)-1].Number.Uint64()
		current = lastNum + 1

		cd.mu.Lock()
		cd.progress.CurrentBlock = lastNum
		cd.mu.Unlock()
	}

	return nil
}

// fetchWithTimeout fetches headers with context and timeout support.
func (cd *ChainDownloader) fetchWithTimeout(ctx context.Context, from uint64, count int) ([]*types.Header, error) {
	type result struct {
		headers []*types.Header
		err     error
	}
	ch := make(chan result, 1)
	go func() {
		h, err := cd.headerSource.FetchHeaders(from, count)
		ch <- result{h, err}
	}()

	timeout := cd.config.Timeout
	if timeout <= 0 {
		timeout = DefaultCDTimeout
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		return r.headers, r.err
	case <-time.After(timeout):
		return nil, ErrChainTimeout
	}
}

// ValidateChain verifies that a sequence of headers forms a valid chain.
// It checks that block numbers are sequential and parent hashes match.
func (cd *ChainDownloader) ValidateChain(headers []*types.Header) error {
	if len(headers) == 0 {
		return ErrChainEmptyBatch
	}

	for i := 1; i < len(headers); i++ {
		prev := headers[i-1]
		cur := headers[i]

		// Check sequential numbering.
		expectedNum := prev.Number.Uint64() + 1
		if cur.Number.Uint64() != expectedNum {
			return fmt.Errorf("%w: header[%d] number %d, expected %d",
				ErrChainInvalidNum, i, cur.Number.Uint64(), expectedNum)
		}

		// Check parent hash linkage.
		if cur.ParentHash != prev.Hash() {
			return fmt.Errorf("%w: header[%d] parent %s, expected %s",
				ErrChainParentHash, i, cur.ParentHash.Hex(), prev.Hash().Hex())
		}
	}

	return nil
}

// ProcessBatch validates that the headers and bodies arrays have matching
// lengths and that each body corresponds to its header.
func (cd *ChainDownloader) ProcessBatch(headers []*types.Header, bodies []*types.Body) error {
	if len(headers) == 0 {
		return ErrChainEmptyBatch
	}
	if len(bodies) != len(headers) {
		return fmt.Errorf("chain_downloader: body count %d != header count %d",
			len(bodies), len(headers))
	}
	return nil
}

// Progress returns a snapshot of the current download progress.
func (cd *ChainDownloader) Progress() DownloadProgress {
	cd.mu.RLock()
	defer cd.mu.RUnlock()

	p := cd.progress
	p.PeersConnected = uint64(len(cd.peers))
	return p
}

// Announcements returns a copy of the recent block announcements.
func (cd *ChainDownloader) Announcements() []announcement {
	cd.mu.RLock()
	defer cd.mu.RUnlock()

	result := make([]announcement, len(cd.announcements))
	copy(result, cd.announcements)
	return result
}

// GetPeer returns a copy of the peer info for the given ID, or nil.
func (cd *ChainDownloader) GetPeer(id string) *PeerInfo {
	cd.mu.RLock()
	defer cd.mu.RUnlock()

	p, ok := cd.peers[id]
	if !ok {
		return nil
	}
	cp := *p
	return &cp
}

// HighestPeerBlock returns the highest block number known from any peer.
func (cd *ChainDownloader) HighestPeerBlock() uint64 {
	cd.mu.RLock()
	defer cd.mu.RUnlock()

	var highest uint64
	for _, p := range cd.peers {
		if p.HeadNumber > highest {
			highest = p.HeadNumber
		}
	}
	return highest
}
