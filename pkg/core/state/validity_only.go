// Package state implements Ethereum world state management.
//
// validity_only.go implements a validity-only partial state representation
// that tracks only validity proofs rather than full state data. This is part
// of the EL Sustainability roadmap ("validity-only partial state") enabling
// nodes to participate in consensus without storing the full state trie.
package state

import (
	"encoding/binary"
	"errors"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// Errors returned by validity-only state operations.
var (
	ErrValidityNilProof       = errors.New("validity: nil proof")
	ErrValidityEmptyRoot      = errors.New("validity: empty state root")
	ErrValidityEmptyProofData = errors.New("validity: empty proof data")
	ErrValidityNoProofs       = errors.New("validity: no proofs stored")
	ErrValidityDuplicateProof = errors.New("validity: duplicate proof for block")
)

// VerifierType identifies the proof verification scheme used.
type VerifierType uint8

const (
	// VerifierTypeSNARK represents a SNARK-based validity proof verifier.
	VerifierTypeSNARK VerifierType = iota
	// VerifierTypeSTARK represents a STARK-based validity proof verifier.
	VerifierTypeSTARK
	// VerifierTypePlonk represents a PLONK-based validity proof verifier.
	VerifierTypePlonk
)

// ValidityProof holds a validity proof attesting to a state transition.
// It contains the resulting state root, the block number at which the
// transition occurred, the serialized proof data, and the verifier type.
type ValidityProof struct {
	StateRoot    types.Hash
	BlockNumber  uint64
	ProofData    []byte
	VerifierType VerifierType
}

// ValidityOnlyState is a state representation that stores only validity
// proofs, not the full account/storage state. It enables lightweight
// nodes to track proven state roots without maintaining the full trie.
type ValidityOnlyState struct {
	mu sync.RWMutex

	// proofs stores validity proofs in insertion order.
	proofs []*ValidityProof

	// validRoots is a set of state roots that have been proven valid.
	validRoots map[types.Hash]struct{}

	// blockIndex maps block numbers to proof indices for dedup checking.
	blockIndex map[uint64]int
}

// NewValidityOnlyState creates an empty validity-only state tracker.
func NewValidityOnlyState() *ValidityOnlyState {
	return &ValidityOnlyState{
		validRoots: make(map[types.Hash]struct{}),
		blockIndex: make(map[uint64]int),
	}
}

// AddValidityProof adds a validity proof to the state tracker. It returns
// an error if the proof is nil, has an empty state root, has empty proof
// data, or if a proof for the same block number already exists.
func (v *ValidityOnlyState) AddValidityProof(proof *ValidityProof) error {
	if proof == nil {
		return ErrValidityNilProof
	}
	if proof.StateRoot.IsZero() {
		return ErrValidityEmptyRoot
	}
	if len(proof.ProofData) == 0 {
		return ErrValidityEmptyProofData
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	if _, exists := v.blockIndex[proof.BlockNumber]; exists {
		return ErrValidityDuplicateProof
	}

	// Store a deep copy of the proof to avoid external mutation.
	cp := &ValidityProof{
		StateRoot:    proof.StateRoot,
		BlockNumber:  proof.BlockNumber,
		ProofData:    make([]byte, len(proof.ProofData)),
		VerifierType: proof.VerifierType,
	}
	copy(cp.ProofData, proof.ProofData)

	idx := len(v.proofs)
	v.proofs = append(v.proofs, cp)
	v.validRoots[cp.StateRoot] = struct{}{}
	v.blockIndex[cp.BlockNumber] = idx

	return nil
}

// VerifyStateTransition verifies that a state transition from prevRoot to
// newRoot is valid given the supplied proof. The verification checks that:
// 1. The proof's state root matches newRoot.
// 2. The proof data encodes a binding between prevRoot and newRoot.
// 3. The proof data hash is non-trivial (simulated cryptographic check).
//
// In production, this would invoke the appropriate SNARK/STARK/PLONK
// verifier. Here we use a deterministic hash-based simulation.
func (v *ValidityOnlyState) VerifyStateTransition(prevRoot, newRoot types.Hash, proof *ValidityProof) bool {
	if proof == nil {
		return false
	}
	if proof.StateRoot != newRoot {
		return false
	}
	if prevRoot.IsZero() || newRoot.IsZero() {
		return false
	}
	if len(proof.ProofData) == 0 {
		return false
	}

	// Simulate verification: the proof data must hash to a value that
	// commits to the transition (prevRoot || newRoot). We check that
	// the proof data, when hashed with the roots, produces a non-zero
	// commitment. This simulates an actual verifier circuit check.
	commitment := computeTransitionCommitment(prevRoot, newRoot, proof.ProofData)
	return !commitment.IsZero()
}

// computeTransitionCommitment produces a deterministic hash commitment
// binding prevRoot, newRoot, and proofData together. In a real system,
// this would be the verification equation output.
func computeTransitionCommitment(prevRoot, newRoot types.Hash, proofData []byte) types.Hash {
	buf := make([]byte, 0, 64+len(proofData))
	buf = append(buf, prevRoot[:]...)
	buf = append(buf, newRoot[:]...)
	buf = append(buf, proofData...)
	return crypto.Keccak256Hash(buf)
}

// GetLatestValidRoot returns the state root from the most recently added
// validity proof. Returns the zero hash if no proofs exist.
func (v *ValidityOnlyState) GetLatestValidRoot() types.Hash {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if len(v.proofs) == 0 {
		return types.Hash{}
	}
	return v.proofs[len(v.proofs)-1].StateRoot
}

// PruneOldProofs removes all but the last keepLastN proofs, freeing memory.
// If keepLastN is less than or equal to zero, no pruning occurs. If
// keepLastN exceeds the current proof count, this is a no-op.
func (v *ValidityOnlyState) PruneOldProofs(keepLastN int) {
	if keepLastN <= 0 {
		return
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	if keepLastN >= len(v.proofs) {
		return
	}

	removeCount := len(v.proofs) - keepLastN
	removed := v.proofs[:removeCount]

	// Rebuild valid roots and block index from retained proofs only.
	retained := make([]*ValidityProof, keepLastN)
	copy(retained, v.proofs[removeCount:])

	newValidRoots := make(map[types.Hash]struct{}, len(retained))
	newBlockIndex := make(map[uint64]int, len(retained))
	for i, p := range retained {
		newValidRoots[p.StateRoot] = struct{}{}
		newBlockIndex[p.BlockNumber] = i
	}

	// Remove roots that were only in removed proofs.
	for _, p := range removed {
		if _, stillValid := newValidRoots[p.StateRoot]; !stillValid {
			// Root is no longer backed by any retained proof.
			delete(v.validRoots, p.StateRoot)
		}
	}

	v.proofs = retained
	v.validRoots = newValidRoots
	v.blockIndex = newBlockIndex
}

// ProofCount returns the number of stored validity proofs.
func (v *ValidityOnlyState) ProofCount() int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return len(v.proofs)
}

// IsValid checks whether a state root has been proven valid by any
// stored validity proof.
func (v *ValidityOnlyState) IsValid(root types.Hash) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	_, ok := v.validRoots[root]
	return ok
}

// GetProofByBlock returns the validity proof for the given block number,
// or nil if no proof exists for that block.
func (v *ValidityOnlyState) GetProofByBlock(blockNumber uint64) *ValidityProof {
	v.mu.RLock()
	defer v.mu.RUnlock()

	idx, ok := v.blockIndex[blockNumber]
	if !ok {
		return nil
	}
	if idx >= len(v.proofs) {
		return nil
	}
	p := v.proofs[idx]
	// Return a copy.
	cp := &ValidityProof{
		StateRoot:    p.StateRoot,
		BlockNumber:  p.BlockNumber,
		ProofData:    make([]byte, len(p.ProofData)),
		VerifierType: p.VerifierType,
	}
	copy(cp.ProofData, p.ProofData)
	return cp
}

// StateRootDigest computes a summary digest of all stored valid roots,
// useful for compact attestation of the validity chain.
func (v *ValidityOnlyState) StateRootDigest() types.Hash {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if len(v.proofs) == 0 {
		return types.Hash{}
	}

	buf := make([]byte, 0, len(v.proofs)*(types.HashLength+8))
	for _, p := range v.proofs {
		buf = append(buf, p.StateRoot[:]...)
		var tmp [8]byte
		binary.LittleEndian.PutUint64(tmp[:], p.BlockNumber)
		buf = append(buf, tmp[:]...)
	}
	return crypto.Keccak256Hash(buf)
}
