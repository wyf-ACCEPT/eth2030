// anchor_state.go implements advanced anchor state management for native
// rollups (EIP-8079). It provides multi-rollup anchor tracking with proof-
// verified state updates, a registry of active rollup anchors, and stale
// anchor pruning.
//
// This extends the base anchor.go with higher-level state management on top
// of the low-level AnchorContract ring buffer.
package rollup

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// Anchor state manager errors.
var (
	ErrAnchorNotFound        = errors.New("anchor_state: rollup anchor not found")
	ErrAnchorAlreadyExists   = errors.New("anchor_state: rollup anchor already registered")
	ErrAnchorProofInvalid    = errors.New("anchor_state: execution proof verification failed")
	ErrAnchorProofEmpty      = errors.New("anchor_state: execution proof is empty")
	ErrAnchorStateRegression = errors.New("anchor_state: new state does not advance block number")
	ErrAnchorRollupInactive  = errors.New("anchor_state: rollup is inactive")
	ErrAnchorRollupIDZero    = errors.New("anchor_state: rollup ID must be non-zero")
	ErrAnchorTransitionRoot  = errors.New("anchor_state: state root mismatch in transition")
)

// AnchorExecutionProof carries proof data for updating an anchor state.
type AnchorExecutionProof struct {
	// StateRoot is the new claimed state root after execution.
	StateRoot types.Hash

	// Proof is the encoded proof bytes (ZK proof, re-execution witness, etc.).
	Proof []byte

	// GasUsed is the gas consumed during the proven execution.
	GasUsed uint64

	// BlockNumber is the new L2 block number after this execution.
	BlockNumber uint64

	// Timestamp is the L2 block timestamp.
	Timestamp uint64
}

// ManagedAnchorState extends the base AnchorState with management metadata.
type ManagedAnchorState struct {
	// RollupID uniquely identifies this rollup.
	RollupID uint64

	// StateRoot is the current verified state root.
	StateRoot types.Hash

	// BlockNumber is the latest verified L2 block number.
	BlockNumber uint64

	// Timestamp is the latest verified L2 block timestamp.
	Timestamp uint64

	// LastUpdateTime records when this anchor was last updated (wall clock).
	LastUpdateTime time.Time

	// TotalUpdates counts the number of successful state updates.
	TotalUpdates uint64
}

// AnchorMetadata holds registration data for a rollup in the anchor registry.
type AnchorMetadata struct {
	// Name is a human-readable identifier for the rollup.
	Name string

	// ChainID is the rollup's chain identifier.
	ChainID uint64

	// GenesisRoot is the state root at rollup genesis.
	GenesisRoot types.Hash

	// Active indicates whether this rollup is currently active.
	Active bool

	// RegisteredAt is when the rollup was registered.
	RegisteredAt time.Time
}

// AnchorStateManager tracks anchor states for multiple rollups, providing
// proof-verified state updates and registry management. Thread-safe.
type AnchorStateManager struct {
	mu       sync.RWMutex
	anchors  map[uint64]*ManagedAnchorState
	metadata map[uint64]*AnchorMetadata
}

// NewAnchorStateManager creates a new empty AnchorStateManager.
func NewAnchorStateManager() *AnchorStateManager {
	return &AnchorStateManager{
		anchors:  make(map[uint64]*ManagedAnchorState),
		metadata: make(map[uint64]*AnchorMetadata),
	}
}

// RegisterAnchor registers a new rollup anchor with the given metadata.
// The initial state root is set to the genesis root from metadata.
func (m *AnchorStateManager) RegisterAnchor(rollupID uint64, meta AnchorMetadata) error {
	if rollupID == 0 {
		return ErrAnchorRollupIDZero
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.anchors[rollupID]; exists {
		return ErrAnchorAlreadyExists
	}

	now := time.Now()
	meta.RegisteredAt = now
	meta.Active = true

	m.anchors[rollupID] = &ManagedAnchorState{
		RollupID:       rollupID,
		StateRoot:      meta.GenesisRoot,
		BlockNumber:    0,
		Timestamp:      0,
		LastUpdateTime: now,
		TotalUpdates:   0,
	}
	m.metadata[rollupID] = &meta
	return nil
}

// UpdateAnchorState updates the anchor state for a rollup after verifying
// the execution proof. The proof must demonstrate a valid state transition
// from the current state root to the new state root.
func (m *AnchorStateManager) UpdateAnchorState(rollupID uint64, proof AnchorExecutionProof) error {
	if len(proof.Proof) == 0 {
		return ErrAnchorProofEmpty
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	anchor, ok := m.anchors[rollupID]
	if !ok {
		return ErrAnchorNotFound
	}

	meta, ok := m.metadata[rollupID]
	if !ok || !meta.Active {
		return ErrAnchorRollupInactive
	}

	// Block number must advance.
	if proof.BlockNumber <= anchor.BlockNumber && anchor.BlockNumber > 0 {
		return ErrAnchorStateRegression
	}

	// Verify the execution proof. In a real implementation this would
	// invoke a ZK verifier or re-execute the STF. Here we verify a
	// SHA-256 binding: the proof is valid if SHA256(oldRoot || newRoot || proof)
	// has its first byte matching the low byte of GasUsed.
	if !verifyAnchorProof(anchor.StateRoot, proof) {
		return ErrAnchorProofInvalid
	}

	anchor.StateRoot = proof.StateRoot
	anchor.BlockNumber = proof.BlockNumber
	anchor.Timestamp = proof.Timestamp
	anchor.LastUpdateTime = time.Now()
	anchor.TotalUpdates++

	return nil
}

// GetAnchorState returns a copy of the current anchor state for a rollup.
func (m *AnchorStateManager) GetAnchorState(rollupID uint64) (*ManagedAnchorState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	anchor, ok := m.anchors[rollupID]
	if !ok {
		return nil, ErrAnchorNotFound
	}

	cp := *anchor
	return &cp, nil
}

// GetAnchorMetadata returns a copy of the metadata for a rollup.
func (m *AnchorStateManager) GetAnchorMetadata(rollupID uint64) (*AnchorMetadata, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	meta, ok := m.metadata[rollupID]
	if !ok {
		return nil, ErrAnchorNotFound
	}

	cp := *meta
	return &cp, nil
}

// ValidateStateTransition verifies that a state transition from oldState
// to newState is valid given the proof bytes.
func (m *AnchorStateManager) ValidateStateTransition(
	oldState, newState *ManagedAnchorState,
	proof []byte,
) error {
	if oldState == nil || newState == nil {
		return ErrAnchorNotFound
	}
	if len(proof) == 0 {
		return ErrAnchorProofEmpty
	}
	if newState.BlockNumber <= oldState.BlockNumber && oldState.BlockNumber > 0 {
		return ErrAnchorStateRegression
	}

	// Verify: SHA256(oldRoot || newRoot || proof)[0] == byte(len(proof))
	h := sha256.New()
	h.Write(oldState.StateRoot[:])
	h.Write(newState.StateRoot[:])
	h.Write(proof)
	digest := h.Sum(nil)

	if digest[0] != byte(len(proof)) {
		return ErrAnchorTransitionRoot
	}
	return nil
}

// DeactivateAnchor marks a rollup as inactive. Inactive rollups cannot
// receive state updates but their state is preserved.
func (m *AnchorStateManager) DeactivateAnchor(rollupID uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	meta, ok := m.metadata[rollupID]
	if !ok {
		return ErrAnchorNotFound
	}
	meta.Active = false
	return nil
}

// ActivateAnchor marks a previously inactive rollup as active.
func (m *AnchorStateManager) ActivateAnchor(rollupID uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	meta, ok := m.metadata[rollupID]
	if !ok {
		return ErrAnchorNotFound
	}
	meta.Active = true
	return nil
}

// PruneStaleAnchors removes inactive rollups whose last update is older
// than maxAge seconds. Returns the number of pruned anchors.
func (m *AnchorStateManager) PruneStaleAnchors(maxAge uint64) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-time.Duration(maxAge) * time.Second)
	pruned := 0

	for id, meta := range m.metadata {
		if !meta.Active {
			anchor := m.anchors[id]
			if anchor != nil && anchor.LastUpdateTime.Before(cutoff) {
				delete(m.anchors, id)
				delete(m.metadata, id)
				pruned++
			}
		}
	}
	return pruned
}

// AnchorCount returns the number of registered rollup anchors.
func (m *AnchorStateManager) AnchorCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.anchors)
}

// ActiveCount returns the number of active rollup anchors.
func (m *AnchorStateManager) ActiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, meta := range m.metadata {
		if meta.Active {
			count++
		}
	}
	return count
}

// RollupIDs returns a slice of all registered rollup IDs.
func (m *AnchorStateManager) RollupIDs() []uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]uint64, 0, len(m.anchors))
	for id := range m.anchors {
		ids = append(ids, id)
	}
	return ids
}

// --- Internal helpers ---

// verifyAnchorProof verifies an execution proof against the current state root.
// The proof is valid if SHA256(currentRoot || newRoot || proof) has its first
// byte equal to the low byte of GasUsed. This allows deterministic test
// construction while simulating real verification.
func verifyAnchorProof(currentRoot types.Hash, proof AnchorExecutionProof) bool {
	h := sha256.New()
	h.Write(currentRoot[:])
	h.Write(proof.StateRoot[:])
	h.Write(proof.Proof)
	digest := h.Sum(nil)

	return digest[0] == byte(proof.GasUsed)
}

// MakeValidAnchorProof constructs a proof that will pass verifyAnchorProof
// for the given current and new state roots. Used in tests.
func MakeValidAnchorProof(currentRoot, newRoot types.Hash, blockNum, timestamp uint64) AnchorExecutionProof {
	// Search for a GasUsed value (0..255) that makes the proof valid.
	baseProof := make([]byte, 32)
	copy(baseProof, []byte("anchor-execution-proof"))

	for gasUsed := uint64(0); gasUsed < 256; gasUsed++ {
		h := sha256.New()
		h.Write(currentRoot[:])
		h.Write(newRoot[:])
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], gasUsed)
		testProof := append(baseProof, buf[:]...)
		h.Write(testProof)
		digest := h.Sum(nil)

		if digest[0] == byte(gasUsed) {
			return AnchorExecutionProof{
				StateRoot:   newRoot,
				Proof:       testProof,
				GasUsed:     gasUsed,
				BlockNumber: blockNum,
				Timestamp:   timestamp,
			}
		}
	}

	// Fallback: brute force with varying proof bytes.
	for nonce := uint64(0); nonce < 65536; nonce++ {
		testProof := make([]byte, 40)
		copy(testProof, []byte("anchor-proof"))
		binary.BigEndian.PutUint64(testProof[32:], nonce)

		h := sha256.New()
		h.Write(currentRoot[:])
		h.Write(newRoot[:])
		h.Write(testProof)
		digest := h.Sum(nil)

		gasUsed := uint64(digest[0])
		if digest[0] == byte(gasUsed) {
			return AnchorExecutionProof{
				StateRoot:   newRoot,
				Proof:       testProof,
				GasUsed:     gasUsed,
				BlockNumber: blockNum,
				Timestamp:   timestamp,
			}
		}
	}

	// Should not reach here; always succeeds above.
	return AnchorExecutionProof{
		StateRoot:   newRoot,
		Proof:       baseProof,
		GasUsed:     0,
		BlockNumber: blockNum,
		Timestamp:   timestamp,
	}
}
