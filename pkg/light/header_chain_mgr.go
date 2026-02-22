// header_chain_mgr.go implements a light client header chain manager with
// finality tracking, header verification against sync committee signatures,
// and chain reorganization support.
//
// The manager wraps the existing HeaderChain with additional functionality:
// - Sync committee signature verification for inserted headers.
// - Finality tracking with monotonic advancement.
// - Chain reorganization detection and rollback.
// - Canonical chain snapshot exports for light client proofs.
//
// Part of the CL roadmap: fast confirmation -> single-slot finality.
package light

import (
	"errors"
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Header chain manager errors.
var (
	ErrMgrNotStarted       = errors.New("header_chain_mgr: manager not started")
	ErrMgrAlreadyStarted   = errors.New("header_chain_mgr: manager already started")
	ErrMgrNilHeader        = errors.New("header_chain_mgr: nil header")
	ErrMgrFinalityRegress  = errors.New("header_chain_mgr: finality cannot regress")
	ErrMgrReorgBelowFinal  = errors.New("header_chain_mgr: reorg below finalized height")
	ErrMgrNoFinalizedHdr   = errors.New("header_chain_mgr: no finalized header")
	ErrMgrInvalidSig       = errors.New("header_chain_mgr: sync committee signature invalid")
	ErrMgrSnapshotEmpty    = errors.New("header_chain_mgr: snapshot range is empty")
	ErrMgrRangeInvalid     = errors.New("header_chain_mgr: invalid block range")
)

// HeaderChainMgrConfig configures the header chain manager.
type HeaderChainMgrConfig struct {
	// MaxHeaders is the maximum number of headers to keep in memory.
	MaxHeaders int

	// VerifySignatures enables sync committee signature checks.
	VerifySignatures bool

	// AllowReorgs enables chain reorganization handling.
	AllowReorgs bool
}

// DefaultHeaderChainMgrConfig returns a config with sensible defaults.
func DefaultHeaderChainMgrConfig() HeaderChainMgrConfig {
	return HeaderChainMgrConfig{
		MaxHeaders:       8192,
		VerifySignatures: true,
		AllowReorgs:      true,
	}
}

// ReorgEvent records a chain reorganization.
type ReorgEvent struct {
	OldHead     types.Hash
	NewHead     types.Hash
	OldHeight   uint64
	NewHeight   uint64
	CommonBlock uint64
	Depth       int
}

// ChainSnapshot captures a range of canonical headers for export.
type ChainSnapshot struct {
	Headers     []*types.Header
	StartBlock  uint64
	EndBlock    uint64
	ChainRoot   types.Hash
	FinalizedAt uint64
}

// HeaderChainMgr manages a light client header chain with finality and reorg
// support. It wraps HeaderChain with sync committee verification and provides
// higher-level chain management. Thread-safe.
type HeaderChainMgr struct {
	mu     sync.RWMutex
	config HeaderChainMgrConfig
	chain  *HeaderChain

	// Committee for signature verification.
	committee *SyncCommittee

	// Finality state.
	finalizedHash   types.Hash
	finalizedNumber uint64

	// Reorg tracking.
	reorgs []ReorgEvent

	// Running state.
	started bool

	// Statistics.
	headersInserted uint64
	sigVerified     uint64
	sigFailed       uint64
}

// NewHeaderChainMgr creates a new header chain manager.
func NewHeaderChainMgr(config HeaderChainMgrConfig) *HeaderChainMgr {
	chainConfig := HeaderChainConfig{
		MaxHeaders:       config.MaxHeaders,
		MaxCheckpoints:   64,
		VerifyParentLink: true,
	}
	return &HeaderChainMgr{
		config: config,
		chain:  NewHeaderChain(chainConfig),
	}
}

// Start initializes the manager. Must be called before inserting headers.
func (m *HeaderChainMgr) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return ErrMgrAlreadyStarted
	}
	m.started = true
	return nil
}

// Stop shuts down the manager.
func (m *HeaderChainMgr) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = false
}

// IsStarted returns whether the manager is running.
func (m *HeaderChainMgr) IsStarted() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.started
}

// SetCommittee updates the sync committee used for signature verification.
func (m *HeaderChainMgr) SetCommittee(committee *SyncCommittee) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.committee = committee
}

// InsertVerifiedHeader inserts a header with optional sync committee signature
// verification. If VerifySignatures is enabled and a committee is set, the
// header's signature is validated before insertion.
func (m *HeaderChainMgr) InsertVerifiedHeader(
	header *types.Header,
	committeeBits []byte,
	signature []byte,
) error {
	if header == nil || header.Number == nil {
		return ErrMgrNilHeader
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return ErrMgrNotStarted
	}

	// Verify sync committee signature if configured.
	if m.config.VerifySignatures && m.committee != nil && len(signature) > 0 {
		signingRoot := header.Hash()
		if err := VerifySyncCommitteeSignature(
			m.committee, signingRoot, committeeBits, signature,
		); err != nil {
			m.sigFailed++
			return ErrMgrInvalidSig
		}
		m.sigVerified++
	}

	// Insert into underlying chain.
	if err := m.chain.InsertHeader(header); err != nil {
		return err
	}
	m.headersInserted++

	return nil
}

// InsertHeaderNoSig inserts a header without signature verification.
// Useful for genesis/checkpoint headers or when signatures are verified
// externally.
func (m *HeaderChainMgr) InsertHeaderNoSig(header *types.Header) error {
	if header == nil || header.Number == nil {
		return ErrMgrNilHeader
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return ErrMgrNotStarted
	}

	if err := m.chain.InsertHeader(header); err != nil {
		return err
	}
	m.headersInserted++
	return nil
}

// SetFinalized marks a header as finalized. Finality can only advance forward.
func (m *HeaderChainMgr) SetFinalized(hash types.Hash, number uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return ErrMgrNotStarted
	}

	if number < m.finalizedNumber {
		return ErrMgrFinalityRegress
	}

	// Verify the header exists in the chain.
	if h := m.chain.GetHeader(hash); h == nil {
		return ErrMgrNilHeader
	}

	if err := m.chain.SetFinalized(hash); err != nil {
		return err
	}

	m.finalizedHash = hash
	m.finalizedNumber = number
	return nil
}

// FinalizedHeader returns the latest finalized header and its number.
func (m *HeaderChainMgr) FinalizedHeader() (*types.Header, uint64) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	header := m.chain.GetHeader(m.finalizedHash)
	return header, m.finalizedNumber
}

// Head returns the canonical chain head.
func (m *HeaderChainMgr) Head() *types.Header {
	return m.chain.Head()
}

// GetHeaderByNumber returns the canonical header at the given block number.
func (m *HeaderChainMgr) GetHeaderByNumber(num uint64) *types.Header {
	return m.chain.GetHeaderByNumber(num)
}

// HandleReorg processes a chain reorganization by finding the common ancestor
// and switching to the new chain. Returns the reorg event if one occurred.
func (m *HeaderChainMgr) HandleReorg(newHeaders []*types.Header) (*ReorgEvent, error) {
	if len(newHeaders) == 0 {
		return nil, ErrMgrNilHeader
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return nil, ErrMgrNotStarted
	}
	if !m.config.AllowReorgs {
		return nil, nil
	}

	oldHead := m.chain.Head()
	if oldHead == nil {
		// No current head; just insert all headers.
		for _, h := range newHeaders {
			if err := m.chain.InsertHeader(h); err != nil {
				if err == ErrHeaderChainDuplicate {
					continue
				}
				return nil, err
			}
		}
		return nil, nil
	}

	oldHeadHash := oldHead.Hash()
	oldHeadNum := oldHead.Number.Uint64()

	// Find the common ancestor by looking for matching parent.
	commonBlock := uint64(0)
	for _, h := range newHeaders {
		if h.Number == nil {
			continue
		}
		num := h.Number.Uint64()
		if num == 0 {
			commonBlock = 0
			break
		}
		existing := m.chain.GetHeader(h.ParentHash)
		if existing != nil {
			commonBlock = existing.Number.Uint64()
			break
		}
	}

	// Check that reorg doesn't go below finalized.
	if commonBlock < m.finalizedNumber {
		return nil, ErrMgrReorgBelowFinal
	}

	// Insert new headers.
	for _, h := range newHeaders {
		if err := m.chain.InsertHeader(h); err != nil {
			if err == ErrHeaderChainDuplicate {
				continue
			}
			return nil, err
		}
	}

	newHead := m.chain.Head()
	if newHead == nil {
		return nil, nil
	}
	newHeadHash := newHead.Hash()
	newHeadNum := newHead.Number.Uint64()

	// Only record a reorg if the head actually changed.
	if newHeadHash == oldHeadHash {
		return nil, nil
	}

	event := &ReorgEvent{
		OldHead:     oldHeadHash,
		NewHead:     newHeadHash,
		OldHeight:   oldHeadNum,
		NewHeight:   newHeadNum,
		CommonBlock: commonBlock,
		Depth:       int(oldHeadNum - commonBlock),
	}
	m.reorgs = append(m.reorgs, *event)
	return event, nil
}

// CanonicalSnapshot exports a range of canonical headers as a ChainSnapshot.
func (m *HeaderChainMgr) CanonicalSnapshot(startBlock, endBlock uint64) (*ChainSnapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if startBlock > endBlock {
		return nil, ErrMgrRangeInvalid
	}

	var headers []*types.Header
	for num := startBlock; num <= endBlock; num++ {
		h := m.chain.GetHeaderByNumber(num)
		if h == nil {
			continue
		}
		headers = append(headers, h)
	}

	if len(headers) == 0 {
		return nil, ErrMgrSnapshotEmpty
	}

	chainRoot := ComputeHeaderChainRoot(headers)

	return &ChainSnapshot{
		Headers:     headers,
		StartBlock:  startBlock,
		EndBlock:    endBlock,
		ChainRoot:   chainRoot,
		FinalizedAt: m.finalizedNumber,
	}, nil
}

// ReorgHistory returns all recorded reorg events.
func (m *HeaderChainMgr) ReorgHistory() []ReorgEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]ReorgEvent, len(m.reorgs))
	copy(result, m.reorgs)
	return result
}

// MgrStats holds header chain manager statistics.
type MgrStats struct {
	HeadersInserted uint64
	SigVerified     uint64
	SigFailed       uint64
	ReorgCount      int
	ChainLen        int
	FinalizedNum    uint64
}

// Stats returns the manager's current statistics.
func (m *HeaderChainMgr) Stats() MgrStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return MgrStats{
		HeadersInserted: m.headersInserted,
		SigVerified:     m.sigVerified,
		SigFailed:       m.sigFailed,
		ReorgCount:      len(m.reorgs),
		ChainLen:        m.chain.Len(),
		FinalizedNum:    m.finalizedNumber,
	}
}

// VerifyChainConsistency checks that the canonical chain from start to end
// has valid parent linkage. Returns the first broken link or nil.
func (m *HeaderChainMgr) VerifyChainConsistency(startBlock, endBlock uint64) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if startBlock > endBlock {
		return ErrMgrRangeInvalid
	}

	var prev *types.Header
	for num := startBlock; num <= endBlock; num++ {
		h := m.chain.GetHeaderByNumber(num)
		if h == nil {
			continue
		}
		if prev != nil && h.ParentHash != prev.Hash() {
			return ErrHeaderVerifyParent
		}
		prev = h
	}
	return nil
}

// ComputeSnapshotRoot computes a Keccak256 Merkle root over a chain snapshot.
// This binds the snapshot to a specific chain state for light client proofs.
func ComputeSnapshotRoot(snap *ChainSnapshot) types.Hash {
	if snap == nil || len(snap.Headers) == 0 {
		return types.Hash{}
	}
	var data []byte
	data = append(data, snap.ChainRoot[:]...)
	data = append(data, byte(snap.FinalizedAt>>56), byte(snap.FinalizedAt>>48),
		byte(snap.FinalizedAt>>40), byte(snap.FinalizedAt>>32),
		byte(snap.FinalizedAt>>24), byte(snap.FinalizedAt>>16),
		byte(snap.FinalizedAt>>8), byte(snap.FinalizedAt))
	data = append(data, byte(snap.StartBlock>>56), byte(snap.StartBlock>>48),
		byte(snap.StartBlock>>40), byte(snap.StartBlock>>32),
		byte(snap.StartBlock>>24), byte(snap.StartBlock>>16),
		byte(snap.StartBlock>>8), byte(snap.StartBlock))
	return crypto.Keccak256Hash(data)
}

// makeTestChainHeaders creates a sequence of linked headers for testing.
// Each header links to the previous via ParentHash, with difficulty=1.
func makeTestChainHeaders(start, count int) []*types.Header {
	headers := make([]*types.Header, count)
	for i := 0; i < count; i++ {
		num := uint64(start + i)
		h := &types.Header{
			Number:     new(big.Int).SetUint64(num),
			Difficulty: big.NewInt(1),
		}
		if i > 0 {
			h.ParentHash = headers[i-1].Hash()
		}
		headers[i] = h
	}
	return headers
}
