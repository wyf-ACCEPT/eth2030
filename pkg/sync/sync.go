// Package sync implements block synchronization protocols for the
// eth2028 execution client. It provides a header-first sync strategy
// where headers are downloaded first, validated, then bodies are
// fetched and blocks are executed.
package sync

import (
	"errors"
	"sync/atomic"
)

// Sync mode constants.
const (
	ModeSnap = "snap" // snap sync (download state, then blocks)
	ModeFull = "full" // full sync (download and execute all blocks)
)

// Sync states.
const (
	StateIdle     uint32 = 0
	StateSyncing  uint32 = 1
	StateDone     uint32 = 2
)

var (
	ErrAlreadySyncing = errors.New("already syncing")
	ErrNoPeers        = errors.New("no peers available")
	ErrCancelled      = errors.New("sync cancelled")
	ErrInvalidChain   = errors.New("invalid chain received")
	ErrTimeout        = errors.New("sync timeout")
)

// Progress tracks the current sync progress.
type Progress struct {
	StartingBlock uint64 // block number where sync started
	CurrentBlock  uint64 // current block being synced
	HighestBlock  uint64 // highest block known from peers
	PulledHeaders uint64 // number of headers downloaded
	PulledBodies  uint64 // number of bodies downloaded
}

// Syncer manages the block synchronization process.
type Syncer struct {
	state    atomic.Uint32
	progress Progress
	mode     string

	// Callbacks for blockchain interaction.
	insertHeaders func(headers []HeaderData) (int, error)
	insertBlocks  func(blocks []BlockData) (int, error)
	currentHeight func() uint64
	hasBlock      func(hash [32]byte) bool

	// Cancel channel.
	cancel chan struct{}
}

// HeaderData represents a downloaded header for sync.
type HeaderData struct {
	Number     uint64
	Hash       [32]byte
	ParentHash [32]byte
	RLP        []byte // RLP-encoded header
}

// BlockData represents a downloaded block for sync.
type BlockData struct {
	Number     uint64
	Hash       [32]byte
	HeaderRLP  []byte
	BodyRLP    []byte
}

// Config holds syncer configuration.
type Config struct {
	Mode           string // "full" or "snap"
	BatchSize      int    // headers per batch request
	MaxPending     int    // max pending header requests
	BodyBatchSize  int    // bodies per batch request
}

// DefaultConfig returns default sync configuration.
func DefaultConfig() *Config {
	return &Config{
		Mode:          ModeFull,
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
		cancel: make(chan struct{}),
	}
}

// SetCallbacks sets the blockchain interaction callbacks.
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

	s.cancel = make(chan struct{})
	s.progress = Progress{
		StartingBlock: s.currentHeight(),
		CurrentBlock:  s.currentHeight(),
		HighestBlock:  targetHeight,
	}

	return nil
}

// Cancel stops the sync process.
func (s *Syncer) Cancel() {
	select {
	case <-s.cancel:
		// already cancelled
	default:
		close(s.cancel)
	}
	s.state.Store(StateIdle)
}

// State returns the current sync state.
func (s *Syncer) State() uint32 {
	return s.state.Load()
}

// Progress returns the current sync progress.
func (s *Syncer) GetProgress() Progress {
	return s.progress
}

// IsSyncing returns whether the syncer is actively syncing.
func (s *Syncer) IsSyncing() bool {
	return s.state.Load() == StateSyncing
}

// ProcessHeaders processes a batch of downloaded headers.
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
	s.progress.PulledHeaders += uint64(n)
	if n > 0 {
		s.progress.CurrentBlock = headers[n-1].Number
	}
	return n, err
}

// ProcessBlocks processes a batch of downloaded blocks.
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
	s.progress.PulledBodies += uint64(n)
	if n > 0 && blocks[n-1].Number > s.progress.CurrentBlock {
		s.progress.CurrentBlock = blocks[n-1].Number
	}

	// Check if sync is complete.
	if s.progress.CurrentBlock >= s.progress.HighestBlock {
		s.state.Store(StateDone)
	}

	return n, err
}

// MarkDone marks the sync as complete.
func (s *Syncer) MarkDone() {
	s.state.Store(StateDone)
}
