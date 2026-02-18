// Package sync implements block synchronization protocols for the
// eth2028 execution client. It provides a header-first sync strategy
// where headers are downloaded first, validated, then bodies are
// fetched and blocks are executed.
package sync

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// Sync mode constants.
const (
	ModeSnap = "snap" // snap sync (download state, then blocks)
	ModeFull = "full" // full sync (download and execute all blocks)
)

// Default batch sizes.
const (
	DefaultHeaderBatch = 64
	DefaultBodyBatch   = 32
)

// Sync states.
const (
	StateIdle    uint32 = 0
	StateSyncing uint32 = 1
	StateDone    uint32 = 2
)

// SyncStage tracks the high-level stage within the sync pipeline.
// During snap sync the pipeline progresses through multiple stages;
// during full sync it stays in StageBlocks.
const (
	StageNone            uint32 = 0
	StageHeaders         uint32 = 1 // Downloading headers (full sync)
	StageSnapAccounts    uint32 = 2 // Downloading accounts (snap sync)
	StageSnapStorage     uint32 = 3 // Downloading storage (snap sync)
	StageSnapBytecodes   uint32 = 4 // Downloading bytecodes (snap sync)
	StageSnapHealing     uint32 = 5 // Healing trie (snap sync)
	StageBlocks          uint32 = 6 // Downloading remaining blocks (post-snap or full)
	StageCaughtUp        uint32 = 7 // Sync complete, chain is caught up
)

// StageName returns a human-readable name for a sync stage.
func StageName(stage uint32) string {
	switch stage {
	case StageNone:
		return "none"
	case StageHeaders:
		return "downloading headers"
	case StageSnapAccounts:
		return "downloading accounts"
	case StageSnapStorage:
		return "downloading storage"
	case StageSnapBytecodes:
		return "downloading bytecodes"
	case StageSnapHealing:
		return "healing trie"
	case StageBlocks:
		return "downloading blocks"
	case StageCaughtUp:
		return "caught up"
	default:
		return fmt.Sprintf("unknown(%d)", stage)
	}
}

// Maximum allowed future timestamp for a header (15 seconds).
const maxFutureTimestamp = 15

var (
	ErrAlreadySyncing   = errors.New("already syncing")
	ErrNoPeers          = errors.New("no peers available")
	ErrCancelled        = errors.New("sync cancelled")
	ErrInvalidChain     = errors.New("invalid chain received")
	ErrTimeout          = errors.New("sync timeout")
	ErrBadParentHash    = errors.New("invalid parent hash")
	ErrBadBlockNumber   = errors.New("non-contiguous block number")
	ErrFutureTimestamp  = errors.New("header timestamp too far in the future")
	ErrTimestampOrder   = errors.New("header timestamp not after parent")
	ErrBodyHeaderCount  = errors.New("body count does not match header count")
	ErrNoBlockInserter  = errors.New("no block inserter configured")
	ErrNoHeaderFetcher  = errors.New("no header fetcher configured")
	ErrNoBodyFetcher    = errors.New("no body fetcher configured")
	ErrEmptyHeaders     = errors.New("received empty header set")
	ErrInsertionFailed  = errors.New("block insertion failed")
	ErrSnapSyncFailed   = errors.New("snap sync failed, falling back to full sync")
	ErrNoSnapPeerSet    = errors.New("no snap peer configured for snap sync")
	ErrNoStateWriter    = errors.New("no state writer configured for snap sync")
)

// HeaderSource retrieves headers from a remote peer or local chain.
type HeaderSource interface {
	FetchHeaders(from uint64, count int) ([]*types.Header, error)
}

// BodySource retrieves block bodies from a remote peer or local chain.
type BodySource interface {
	FetchBodies(hashes []types.Hash) ([]*types.Body, error)
}

// BlockInserter inserts validated blocks into the local chain.
type BlockInserter interface {
	InsertChain(blocks []*types.Block) (int, error)
	CurrentBlock() *types.Block
}

// Progress tracks the current sync progress.
type Progress struct {
	StartingBlock uint64 // block number where sync started
	CurrentBlock  uint64 // current block being synced
	HighestBlock  uint64 // highest block known from peers
	PulledHeaders uint64 // number of headers downloaded
	PulledBodies  uint64 // number of bodies downloaded
	Stage         uint32 // current sync stage (SyncStage constant)
	Mode          string // sync mode ("full" or "snap")
	SnapProgress  *SnapProgress // snap sync progress (nil for full sync)
}

// Percentage returns the sync completion as a percentage (0-100).
func (p Progress) Percentage() float64 {
	total := p.HighestBlock - p.StartingBlock
	if total == 0 {
		return 100.0
	}
	done := p.CurrentBlock - p.StartingBlock
	return float64(done) / float64(total) * 100.0
}

// Syncer manages the block synchronization process.
type Syncer struct {
	state    atomic.Uint32
	stage    atomic.Uint32
	mu       sync.Mutex
	progress Progress
	mode     string
	config   *Config

	// Interfaces for data fetching and chain insertion.
	headerFetcher HeaderSource
	bodyFetcher   BodySource
	inserter      BlockInserter

	// Snap sync components (set via SetSnapSync).
	snapSyncer  *SnapSyncer
	snapPeer    SnapPeer
	stateWriter StateWriter

	// Legacy callbacks for backward compatibility.
	insertHeaders func(headers []HeaderData) (int, error)
	insertBlocks  func(blocks []BlockData) (int, error)
	currentHeight func() uint64
	hasBlock      func(hash [32]byte) bool

	// Cancel channel.
	cancel chan struct{}
}

// HeaderData represents a downloaded header for sync (legacy format).
type HeaderData struct {
	Number     uint64
	Hash       [32]byte
	ParentHash [32]byte
	Timestamp  uint64
	RLP        []byte // RLP-encoded header
}

// BlockData represents a downloaded block for sync (legacy format).
type BlockData struct {
	Number    uint64
	Hash      [32]byte
	HeaderRLP []byte
	BodyRLP   []byte
}

// Config holds syncer configuration.
type Config struct {
	Mode          string // "full" or "snap"
	BatchSize     int    // headers per batch request
	MaxPending    int    // max pending header requests
	BodyBatchSize int    // bodies per batch request
}

// DefaultConfig returns default sync configuration.
// Snap sync is the default mode; it falls back to full sync if snap
// sync components are not configured or if snap sync encounters errors.
func DefaultConfig() *Config {
	return &Config{
		Mode:          ModeSnap,
		BatchSize:     192,
		MaxPending:    16,
		BodyBatchSize: 128,
	}
}

// NewSyncer creates a new syncer.
func NewSyncer(config *Config) *Syncer {
	if config == nil {
		config = DefaultConfig()
	}
	return &Syncer{
		mode:   config.Mode,
		config: config,
		cancel: make(chan struct{}),
	}
}

// SetFetchers configures the header and body fetchers plus chain inserter.
func (s *Syncer) SetFetchers(hf HeaderSource, bf BodySource, ins BlockInserter) {
	s.headerFetcher = hf
	s.bodyFetcher = bf
	s.inserter = ins
}

// SetSnapSync configures the snap sync components. When mode is ModeSnap,
// RunSync will use these to download state before switching to block sync.
func (s *Syncer) SetSnapSync(peer SnapPeer, writer StateWriter) {
	s.snapPeer = peer
	s.stateWriter = writer
	s.snapSyncer = NewSnapSyncer(writer)
}

// Mode returns the current sync mode ("full" or "snap").
func (s *Syncer) Mode() string {
	return s.mode
}

// Stage returns the current sync stage.
func (s *Syncer) Stage() uint32 {
	return s.stage.Load()
}

// SnapSyncer returns the underlying snap syncer, or nil if not configured.
func (s *Syncer) SnapSyncer() *SnapSyncer {
	return s.snapSyncer
}

// SetCallbacks sets the blockchain interaction callbacks (legacy API).
func (s *Syncer) SetCallbacks(
	insertHeaders func([]HeaderData) (int, error),
	insertBlocks func([]BlockData) (int, error),
	currentHeight func() uint64,
	hasBlock func([32]byte) bool,
) {
	s.insertHeaders = insertHeaders
	s.insertBlocks = insertBlocks
	s.currentHeight = currentHeight
	s.hasBlock = hasBlock
}

// Start begins synchronization to the target block.
func (s *Syncer) Start(targetHeight uint64) error {
	if !s.state.CompareAndSwap(StateIdle, StateSyncing) {
		return ErrAlreadySyncing
	}

	s.mu.Lock()
	s.cancel = make(chan struct{})
	s.mu.Unlock()

	var current uint64
	if s.inserter != nil {
		current = s.inserter.CurrentBlock().NumberU64()
	} else if s.currentHeight != nil {
		current = s.currentHeight()
	}

	s.mu.Lock()
	s.progress = Progress{
		StartingBlock: current,
		CurrentBlock:  current,
		HighestBlock:  targetHeight,
		Mode:          s.mode,
	}
	s.mu.Unlock()

	return nil
}

// Cancel stops the sync process, including any active snap sync.
func (s *Syncer) Cancel() {
	s.mu.Lock()
	cancelCh := s.cancel
	snap := s.snapSyncer
	s.mu.Unlock()

	select {
	case <-cancelCh:
		// already cancelled
	default:
		close(cancelCh)
	}
	if snap != nil {
		snap.Cancel()
	}
	s.state.Store(StateIdle)
}

// State returns the current sync state.
func (s *Syncer) State() uint32 {
	return s.state.Load()
}

// GetProgress returns the current sync progress. If snap sync is active,
// the SnapProgress field is populated with the latest snap sync state.
func (s *Syncer) GetProgress() Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.progress
	if s.snapSyncer != nil && s.snapSyncer.IsRunning() {
		sp := s.snapSyncer.Progress()
		p.SnapProgress = &sp
	}
	return p
}

// IsSyncing returns whether the syncer is actively syncing.
func (s *Syncer) IsSyncing() bool {
	return s.state.Load() == StateSyncing
}

// MarkDone marks the sync as complete.
func (s *Syncer) MarkDone() {
	s.state.Store(StateDone)
}

// RunSync executes the sync pipeline based on the configured mode.
//
// In "full" mode, it downloads headers, fetches bodies, and inserts blocks
// sequentially from genesis to the target.
//
// In "snap" mode, it first downloads the world state at a pivot block
// via snap sync (accounts, storage, bytecodes, healing), then fetches
// and inserts the remaining blocks from the pivot to the target.
// If snap sync encounters an error, it falls back to full sync.
func (s *Syncer) RunSync(targetBlock uint64) error {
	if s.headerFetcher == nil {
		return ErrNoHeaderFetcher
	}
	if s.bodyFetcher == nil {
		return ErrNoBodyFetcher
	}
	if s.inserter == nil {
		return ErrNoBlockInserter
	}

	if err := s.Start(targetBlock); err != nil {
		return err
	}

	if s.mode == ModeSnap {
		err := s.runSnapSync(targetBlock)
		if err == nil {
			return nil
		}
		// Snap sync failed; fall back to full sync from wherever we are.
		s.mu.Lock()
		s.progress.Mode = ModeFull
		s.mu.Unlock()
		s.mode = ModeFull
	}

	return s.runFullSync(targetBlock)
}

// runSnapSync executes the snap sync pipeline: download state at the
// pivot block, then fill in remaining blocks via full sync.
func (s *Syncer) runSnapSync(targetBlock uint64) error {
	if s.snapPeer == nil {
		return ErrNoSnapPeerSet
	}
	if s.stateWriter == nil {
		return ErrNoStateWriter
	}
	if s.snapSyncer == nil {
		s.snapSyncer = NewSnapSyncer(s.stateWriter)
	}

	// Select pivot block.
	pivotNum, err := SelectPivot(targetBlock)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSnapSyncFailed, err)
	}

	// Fetch the pivot header to get the state root.
	s.stage.Store(StageHeaders)
	s.mu.Lock()
	s.progress.Stage = StageHeaders
	s.mu.Unlock()

	pivotHeaders, err := s.headerFetcher.FetchHeaders(pivotNum, 1)
	if err != nil || len(pivotHeaders) == 0 {
		if err == nil {
			err = ErrEmptyHeaders
		}
		return fmt.Errorf("%w: fetch pivot header: %v", ErrSnapSyncFailed, err)
	}
	pivotHeader := pivotHeaders[0]

	// Configure the snap syncer with the pivot.
	s.snapSyncer.SetPivot(pivotHeader)

	// Phase 1: Accounts.
	s.stage.Store(StageSnapAccounts)
	s.mu.Lock()
	s.progress.Stage = StageSnapAccounts
	s.mu.Unlock()

	// Run snap sync in a goroutine so we can monitor cancellation.
	snapErrCh := make(chan error, 1)
	go func() {
		snapErrCh <- s.snapSyncer.Start(s.snapPeer)
	}()

	// Wait for snap sync to finish or be cancelled.
	var snapErr error
	select {
	case <-s.cancel:
		s.snapSyncer.Cancel()
		<-snapErrCh // drain
		s.state.Store(StateIdle)
		return ErrCancelled
	case snapErr = <-snapErrCh:
	}

	if snapErr != nil {
		return fmt.Errorf("%w: %v", ErrSnapSyncFailed, snapErr)
	}

	// Update progress from snap syncer.
	snapProg := s.snapSyncer.Progress()
	s.mu.Lock()
	s.progress.SnapProgress = &snapProg
	// After snap sync the pivot block's state is downloaded. Update the
	// stage tracker to reflect the snap phase that just completed.
	switch s.snapSyncer.Phase() {
	case PhaseComplete:
		s.progress.Stage = StageBlocks
	}
	s.mu.Unlock()

	// Now run full block sync from pivot to target.
	s.stage.Store(StageBlocks)
	s.mu.Lock()
	s.progress.Stage = StageBlocks
	s.mu.Unlock()

	err = s.runFullSync(targetBlock)
	if err != nil {
		return err
	}

	s.stage.Store(StageCaughtUp)
	s.mu.Lock()
	s.progress.Stage = StageCaughtUp
	s.mu.Unlock()

	return nil
}

// updateSnapStage is called periodically by snap sync to update the
// main syncer stage tracking.
func (s *Syncer) updateSnapStage() {
	if s.snapSyncer == nil {
		return
	}
	phase := s.snapSyncer.Phase()
	var stage uint32
	switch phase {
	case PhaseAccounts:
		stage = StageSnapAccounts
	case PhaseStorage:
		stage = StageSnapStorage
	case PhaseBytecode:
		stage = StageSnapBytecodes
	case PhaseHealing:
		stage = StageSnapHealing
	case PhaseComplete:
		stage = StageBlocks
	default:
		return
	}
	s.stage.Store(stage)
	s.mu.Lock()
	s.progress.Stage = stage
	snapProg := s.snapSyncer.Progress()
	s.progress.SnapProgress = &snapProg
	s.mu.Unlock()
}

// runFullSync executes the block-by-block sync loop.
func (s *Syncer) runFullSync(targetBlock uint64) error {
	headerBatch := s.config.BatchSize
	if headerBatch <= 0 {
		headerBatch = DefaultHeaderBatch
	}
	bodyBatch := s.config.BodyBatchSize
	if bodyBatch <= 0 {
		bodyBatch = DefaultBodyBatch
	}

	if s.stage.Load() == StageNone {
		s.stage.Store(StageBlocks)
		s.mu.Lock()
		s.progress.Stage = StageBlocks
		s.mu.Unlock()
	}

	current := s.inserter.CurrentBlock().NumberU64()

	for current < targetBlock {
		// Check cancellation.
		select {
		case <-s.cancel:
			return ErrCancelled
		default:
		}

		// Determine how many headers to fetch.
		remaining := targetBlock - current
		count := uint64(headerBatch)
		if remaining < count {
			count = remaining
		}

		// Fetch headers starting from the next block.
		from := current + 1
		headers, err := s.headerFetcher.FetchHeaders(from, int(count))
		if err != nil {
			s.state.Store(StateIdle)
			return fmt.Errorf("fetch headers from %d: %w", from, err)
		}
		if len(headers) == 0 {
			s.state.Store(StateIdle)
			return ErrEmptyHeaders
		}

		// Validate the header chain.
		parentBlock := s.inserter.CurrentBlock()
		if err := s.processHeaders(headers, parentBlock.Header()); err != nil {
			s.state.Store(StateIdle)
			return err
		}

		s.mu.Lock()
		s.progress.PulledHeaders += uint64(len(headers))
		s.mu.Unlock()

		// Fetch bodies in batches and insert blocks.
		for i := 0; i < len(headers); i += bodyBatch {
			select {
			case <-s.cancel:
				return ErrCancelled
			default:
			}

			end := i + bodyBatch
			if end > len(headers) {
				end = len(headers)
			}
			batch := headers[i:end]

			// Collect hashes for body fetch.
			hashes := make([]types.Hash, len(batch))
			for j, h := range batch {
				hashes[j] = h.Hash()
			}

			bodies, err := s.bodyFetcher.FetchBodies(hashes)
			if err != nil {
				s.state.Store(StateIdle)
				return fmt.Errorf("fetch bodies: %w", err)
			}

			// Assemble full blocks from headers + bodies.
			blocks, err := s.processBodies(batch, bodies)
			if err != nil {
				s.state.Store(StateIdle)
				return err
			}

			// Insert blocks into the chain.
			if err := s.insertBlk(blocks); err != nil {
				s.state.Store(StateIdle)
				return err
			}

			s.mu.Lock()
			s.progress.PulledBodies += uint64(len(blocks))
			s.progress.CurrentBlock = blocks[len(blocks)-1].NumberU64()
			s.mu.Unlock()
		}

		current = s.inserter.CurrentBlock().NumberU64()
	}

	s.stage.Store(StageCaughtUp)
	s.mu.Lock()
	s.progress.Stage = StageCaughtUp
	s.mu.Unlock()

	s.state.Store(StateDone)
	return nil
}

// processHeaders validates a chain of headers against the parent header.
// It checks: parent hash linkage, number sequence, and timestamp ordering.
func (s *Syncer) processHeaders(headers []*types.Header, parent *types.Header) error {
	if len(headers) == 0 {
		return ErrEmptyHeaders
	}

	now := uint64(time.Now().Unix())
	prev := parent

	for i, h := range headers {
		// Validate block number is sequential.
		expectedNum := prev.Number.Uint64() + 1
		if h.Number.Uint64() != expectedNum {
			return fmt.Errorf("%w: header[%d] number %d, expected %d",
				ErrBadBlockNumber, i, h.Number.Uint64(), expectedNum)
		}

		// Validate parent hash links to previous header.
		if h.ParentHash != prev.Hash() {
			return fmt.Errorf("%w: header[%d] parent %s, expected %s",
				ErrBadParentHash, i, h.ParentHash.Hex(), prev.Hash().Hex())
		}

		// Validate timestamp is not too far in the future.
		if h.Time > now+maxFutureTimestamp {
			return fmt.Errorf("%w: header[%d] time %d, now %d",
				ErrFutureTimestamp, i, h.Time, now)
		}

		// Validate timestamp is at or after parent (equal is allowed for fast blocks).
		if h.Time < prev.Time {
			return fmt.Errorf("%w: header[%d] time %d < parent time %d",
				ErrTimestampOrder, i, h.Time, prev.Time)
		}

		prev = h
	}

	return nil
}

// processBodies matches fetched bodies with their corresponding headers
// and assembles full Block objects.
func (s *Syncer) processBodies(headers []*types.Header, bodies []*types.Body) ([]*types.Block, error) {
	if len(bodies) != len(headers) {
		return nil, fmt.Errorf("%w: got %d bodies for %d headers",
			ErrBodyHeaderCount, len(bodies), len(headers))
	}

	blocks := make([]*types.Block, len(headers))
	for i, h := range headers {
		blocks[i] = types.NewBlock(h, bodies[i])
	}

	return blocks, nil
}

// insertBlk calls the BlockInserter to insert a batch of blocks.
func (s *Syncer) insertBlk(blocks []*types.Block) error {
	n, err := s.inserter.InsertChain(blocks)
	if err != nil {
		return fmt.Errorf("%w: inserted %d/%d blocks: %v",
			ErrInsertionFailed, n, len(blocks), err)
	}
	return nil
}

// ProcessHeaders processes a batch of downloaded headers (legacy API).
// Returns the number successfully processed.
func (s *Syncer) ProcessHeaders(headers []HeaderData) (int, error) {
	if s.insertHeaders == nil {
		return 0, errors.New("no insert headers callback")
	}

	select {
	case <-s.cancel:
		return 0, ErrCancelled
	default:
	}

	n, err := s.insertHeaders(headers)
	s.mu.Lock()
	s.progress.PulledHeaders += uint64(n)
	if n > 0 {
		s.progress.CurrentBlock = headers[n-1].Number
	}
	s.mu.Unlock()
	return n, err
}

// ProcessBlocks processes a batch of downloaded blocks (legacy API).
// Returns the number successfully processed.
func (s *Syncer) ProcessBlocks(blocks []BlockData) (int, error) {
	if s.insertBlocks == nil {
		return 0, errors.New("no insert blocks callback")
	}

	select {
	case <-s.cancel:
		return 0, ErrCancelled
	default:
	}

	n, err := s.insertBlocks(blocks)
	s.mu.Lock()
	s.progress.PulledBodies += uint64(n)
	if n > 0 && blocks[n-1].Number > s.progress.CurrentBlock {
		s.progress.CurrentBlock = blocks[n-1].Number
	}

	// Check if sync is complete.
	if s.progress.CurrentBlock >= s.progress.HighestBlock {
		s.state.Store(StateDone)
	}
	s.mu.Unlock()

	return n, err
}

// ValidateHeaderChain is an exported version of processHeaders for use by
// external callers (e.g., the downloader). It validates a chain of headers.
func ValidateHeaderChain(headers []*types.Header, parent *types.Header) error {
	s := &Syncer{}
	return s.processHeaders(headers, parent)
}

// AssembleBlocks is an exported version of processBodies for use by
// external callers. It pairs headers with bodies to build full blocks.
func AssembleBlocks(headers []*types.Header, bodies []*types.Body) ([]*types.Block, error) {
	s := &Syncer{}
	return s.processBodies(headers, bodies)
}
