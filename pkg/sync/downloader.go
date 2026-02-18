package sync

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// Default downloader constants.
const (
	DefaultRequestTimeout = 10 * time.Second
	DefaultMaxRetries     = 3
	DefaultBanThreshold   = 5 // failures before banning a peer
)

var (
	ErrDownloaderRunning = errors.New("downloader already running")
	ErrPeerBanned        = errors.New("peer is banned")
	ErrAllPeersBanned    = errors.New("all peers banned")
	ErrMaxRetries        = errors.New("max retries exceeded")
)

// SyncProgress is the public-facing sync progress report.
type SyncProgress struct {
	StartingBlock uint64
	CurrentBlock  uint64
	HighestBlock  uint64
	PulledHeaders uint64
	PulledBodies  uint64
	Syncing       bool
	Percentage    float64
	Mode          string // "full" or "snap"
	Stage         string // human-readable stage name
	SnapProgress  *SnapProgress // snap sync progress (nil during full sync)
}

// DownloaderConfig configures the Downloader.
type DownloaderConfig struct {
	SyncConfig     *Config
	RequestTimeout time.Duration
	MaxRetries     int
	BanThreshold   int
}

// DefaultDownloaderConfig returns a DownloaderConfig with sensible defaults.
func DefaultDownloaderConfig() *DownloaderConfig {
	return &DownloaderConfig{
		SyncConfig:     DefaultConfig(),
		RequestTimeout: DefaultRequestTimeout,
		MaxRetries:     DefaultMaxRetries,
		BanThreshold:   DefaultBanThreshold,
	}
}

// Downloader wraps a Syncer with timeout, retry, and peer ban logic.
type Downloader struct {
	syncer  *Syncer
	config  *DownloaderConfig
	running atomic.Bool

	mu     sync.Mutex
	cancel chan struct{}

	// Peer failure tracking.
	peerMu       sync.Mutex
	peerFailures map[string]int
	banned       map[string]time.Time
}

// NewDownloader creates a new Downloader with the given configuration.
func NewDownloader(cfg *DownloaderConfig) *Downloader {
	if cfg == nil {
		cfg = DefaultDownloaderConfig()
	}
	return &Downloader{
		syncer:       NewSyncer(cfg.SyncConfig),
		config:       cfg,
		cancel:       make(chan struct{}),
		peerFailures: make(map[string]int),
		banned:       make(map[string]time.Time),
	}
}

// SetFetchers configures the data sources for the underlying syncer.
func (d *Downloader) SetFetchers(hf HeaderSource, bf BodySource, ins BlockInserter) {
	d.syncer.SetFetchers(hf, bf, ins)
}

// SetSnapSync configures snap sync components on the underlying syncer.
func (d *Downloader) SetSnapSync(peer SnapPeer, writer StateWriter) {
	d.syncer.SetSnapSync(peer, writer)
}

// Syncer returns the underlying Syncer instance.
func (d *Downloader) Syncer() *Syncer {
	return d.syncer
}

// Start begins syncing to the target block. This runs synchronously.
// Use a goroutine if asynchronous operation is desired.
func (d *Downloader) Start(targetBlock uint64) error {
	if !d.running.CompareAndSwap(false, true) {
		return ErrDownloaderRunning
	}
	defer d.running.Store(false)

	d.mu.Lock()
	d.cancel = make(chan struct{})
	d.mu.Unlock()

	return d.syncWithRetry(targetBlock)
}

// Cancel stops the sync process.
func (d *Downloader) Cancel() {
	d.mu.Lock()
	defer d.mu.Unlock()

	select {
	case <-d.cancel:
	default:
		close(d.cancel)
	}
	d.syncer.Cancel()
}

// Progress returns the current sync status.
func (d *Downloader) Progress() SyncProgress {
	p := d.syncer.GetProgress()
	return SyncProgress{
		StartingBlock: p.StartingBlock,
		CurrentBlock:  p.CurrentBlock,
		HighestBlock:  p.HighestBlock,
		PulledHeaders: p.PulledHeaders,
		PulledBodies:  p.PulledBodies,
		Syncing:       d.syncer.IsSyncing(),
		Percentage:    p.Percentage(),
		Mode:          p.Mode,
		Stage:         StageName(p.Stage),
		SnapProgress:  p.SnapProgress,
	}
}

// IsSyncing returns whether the downloader is actively syncing.
func (d *Downloader) IsSyncing() bool {
	return d.running.Load()
}

// RecordPeerFailure records a failure for a peer and bans if threshold is met.
func (d *Downloader) RecordPeerFailure(peerID string) {
	d.peerMu.Lock()
	defer d.peerMu.Unlock()

	d.peerFailures[peerID]++
	if d.peerFailures[peerID] >= d.config.BanThreshold {
		d.banned[peerID] = time.Now()
	}
}

// IsPeerBanned checks whether a peer is currently banned.
func (d *Downloader) IsPeerBanned(peerID string) bool {
	d.peerMu.Lock()
	defer d.peerMu.Unlock()
	_, ok := d.banned[peerID]
	return ok
}

// ResetPeer removes ban and failure count for a peer.
func (d *Downloader) ResetPeer(peerID string) {
	d.peerMu.Lock()
	defer d.peerMu.Unlock()
	delete(d.peerFailures, peerID)
	delete(d.banned, peerID)
}

// syncWithRetry wraps RunSync with retry logic.
func (d *Downloader) syncWithRetry(targetBlock uint64) error {
	var lastErr error
	for attempt := 0; attempt < d.config.MaxRetries; attempt++ {
		select {
		case <-d.cancel:
			return ErrCancelled
		default:
		}

		// Reset the syncer state if it's not idle (from a previous failed attempt).
		if d.syncer.State() != StateIdle {
			d.syncer.state.Store(StateIdle)
		}

		err := d.syncer.RunSync(targetBlock)
		if err == nil {
			return nil
		}

		lastErr = err

		// Don't retry on cancellation.
		if errors.Is(err, ErrCancelled) {
			return err
		}

		// Brief pause before retry.
		select {
		case <-d.cancel:
			return ErrCancelled
		case <-time.After(time.Duration(attempt+1) * 100 * time.Millisecond):
		}
	}

	return fmt.Errorf("%w: %v", ErrMaxRetries, lastErr)
}

// TimeoutHeaderFetcher wraps a HeaderSource with a per-request timeout.
type TimeoutHeaderFetcher struct {
	inner   HeaderSource
	timeout time.Duration
}

// NewTimeoutHeaderFetcher creates a header fetcher with timeout enforcement.
func NewTimeoutHeaderFetcher(inner HeaderSource, timeout time.Duration) *TimeoutHeaderFetcher {
	return &TimeoutHeaderFetcher{inner: inner, timeout: timeout}
}

// FetchHeaders fetches headers with a timeout. Returns ErrTimeout if the
// request takes longer than the configured timeout.
func (f *TimeoutHeaderFetcher) FetchHeaders(from uint64, count int) ([]*types.Header, error) {
	type result struct {
		headers []*types.Header
		err     error
	}
	ch := make(chan result, 1)
	go func() {
		h, err := f.inner.FetchHeaders(from, count)
		ch <- result{h, err}
	}()

	select {
	case r := <-ch:
		return r.headers, r.err
	case <-time.After(f.timeout):
		return nil, ErrTimeout
	}
}

// TimeoutBodyFetcher wraps a BodySource with a per-request timeout.
type TimeoutBodyFetcher struct {
	inner   BodySource
	timeout time.Duration
}

// NewTimeoutBodyFetcher creates a body fetcher with timeout enforcement.
func NewTimeoutBodyFetcher(inner BodySource, timeout time.Duration) *TimeoutBodyFetcher {
	return &TimeoutBodyFetcher{inner: inner, timeout: timeout}
}

// FetchBodies fetches bodies with a timeout.
func (f *TimeoutBodyFetcher) FetchBodies(hashes []types.Hash) ([]*types.Body, error) {
	type result struct {
		bodies []*types.Body
		err    error
	}
	ch := make(chan result, 1)
	go func() {
		b, err := f.inner.FetchBodies(hashes)
		ch <- result{b, err}
	}()

	select {
	case r := <-ch:
		return r.bodies, r.err
	case <-time.After(f.timeout):
		return nil, ErrTimeout
	}
}
