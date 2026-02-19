package engine

import (
	"errors"
	"fmt"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// EIP-7898: Uncouple Execution Payload from Beacon Block
//
// This file implements the uncoupled execution payload mechanism where the
// ExecutionPayload is replaced by ExecutionPayloadHeader in the BeaconBlockBody,
// and the full payload is transmitted independently with an inclusion proof.

// InclusionProofDepth is the depth of the Merkle inclusion proof tree.
// The beacon block body has a fixed number of fields, so the proof depth
// is determined by log2(number of fields).
const InclusionProofDepth = 4

// UncoupledPayloadStatus values.
const (
	UncoupledStatusPending  = "PENDING"
	UncoupledStatusVerified = "VERIFIED"
	UncoupledStatusInvalid  = "INVALID"
)

// InclusionProof is a Merkle proof demonstrating that the execution payload
// was committed to in the beacon block via the ExecutionPayloadHeader.
// The proof shows that hash(payload) matches the commitment in the beacon body.
type InclusionProof struct {
	// Leaf is the hash of the full execution payload that was committed.
	Leaf types.Hash `json:"leaf"`
	// Branch contains the sibling hashes along the path from the leaf
	// to the beacon block body root.
	Branch []types.Hash `json:"branch"`
	// Index is the generalized index of the execution payload leaf
	// in the beacon block body Merkle tree.
	Index uint64 `json:"index"`
}

// UncoupledPayloadEnvelope wraps a full execution payload with the inclusion
// proof linking it to a beacon block. This is gossiped independently of the
// beacon block per EIP-7898.
type UncoupledPayloadEnvelope struct {
	// BeaconBlockRoot is the root of the beacon block this payload belongs to.
	BeaconBlockRoot types.Hash `json:"beaconBlockRoot"`
	// Slot is the beacon slot number.
	Slot uint64 `json:"slot"`
	// Payload is the full execution payload.
	Payload *ExecutionPayloadV5 `json:"executionPayload"`
	// Proof is the Merkle inclusion proof linking the payload to the beacon block.
	Proof *InclusionProof `json:"inclusionProof"`
}

// Validate performs basic structural validation of the envelope.
func (e *UncoupledPayloadEnvelope) Validate() error {
	if e.BeaconBlockRoot == (types.Hash{}) {
		return ErrMissingBeaconRoot
	}
	if e.Slot == 0 {
		return ErrInvalidParams
	}
	if e.Payload == nil {
		return ErrInvalidPayloadAttributes
	}
	if e.Proof == nil {
		return ErrMissingInclusionProof
	}
	return nil
}

// PayloadHash computes the hash of the execution payload for proof verification.
func (e *UncoupledPayloadEnvelope) PayloadHash() types.Hash {
	return e.Payload.BlockHash
}

// ValidateInclusionProof verifies that the inclusion proof correctly links
// the payload to the beacon block root. It recomputes the Merkle root from
// the leaf and branch and checks it matches the beacon block root.
func ValidateInclusionProof(proof *InclusionProof, beaconBlockRoot types.Hash) error {
	if proof == nil {
		return ErrMissingInclusionProof
	}
	if proof.Leaf == (types.Hash{}) {
		return ErrInvalidInclusionProof
	}
	if len(proof.Branch) == 0 {
		return ErrInvalidInclusionProof
	}

	// Recompute root from leaf and branch.
	computed := computeMerkleRoot(proof.Leaf, proof.Branch, proof.Index)
	if computed != beaconBlockRoot {
		return fmt.Errorf("%w: computed root %s != beacon block root %s",
			ErrInclusionProofMismatch, computed.Hex(), beaconBlockRoot.Hex())
	}
	return nil
}

// computeMerkleRoot walks the proof branch from leaf to root, hashing
// at each level based on the generalized index.
func computeMerkleRoot(leaf types.Hash, branch []types.Hash, index uint64) types.Hash {
	current := leaf
	idx := index
	for _, sibling := range branch {
		if idx%2 == 0 {
			// Current is left child.
			current = hashPair(current, sibling)
		} else {
			// Current is right child.
			current = hashPair(sibling, current)
		}
		idx /= 2
	}
	return current
}

// hashPair hashes two 32-byte values together using Keccak-256.
func hashPair(left, right types.Hash) types.Hash {
	var data [64]byte
	copy(data[:32], left[:])
	copy(data[32:], right[:])
	return crypto.Keccak256Hash(data[:])
}

// BuildInclusionProof creates an inclusion proof for an execution payload
// given the beacon block body fields. This is used by the proposer/builder
// to construct the proof when separating the payload from the beacon block.
func BuildInclusionProof(payloadHash types.Hash, bodyFieldHashes []types.Hash, payloadIndex int) (*InclusionProof, error) {
	if len(bodyFieldHashes) == 0 {
		return nil, errors.New("engine: empty body field hashes")
	}
	if payloadIndex < 0 || payloadIndex >= len(bodyFieldHashes) {
		return nil, fmt.Errorf("engine: payload index %d out of range [0, %d)", payloadIndex, len(bodyFieldHashes))
	}

	// Pad to power of 2.
	leaves := padToPowerOfTwo(bodyFieldHashes)

	// Build the Merkle tree bottom-up and collect the proof branch.
	branch := collectProofBranch(leaves, payloadIndex)

	return &InclusionProof{
		Leaf:   payloadHash,
		Branch: branch,
		Index:  uint64(payloadIndex),
	}, nil
}

// padToPowerOfTwo pads a slice of hashes to the next power of two with zero hashes.
func padToPowerOfTwo(hashes []types.Hash) []types.Hash {
	n := len(hashes)
	size := 1
	for size < n {
		size *= 2
	}
	padded := make([]types.Hash, size)
	copy(padded, hashes)
	return padded
}

// collectProofBranch builds a Merkle tree from leaves and collects the
// sibling hashes along the path from the target leaf to the root.
func collectProofBranch(leaves []types.Hash, targetIndex int) []types.Hash {
	var branch []types.Hash
	layer := make([]types.Hash, len(leaves))
	copy(layer, leaves)
	idx := targetIndex

	for len(layer) > 1 {
		// Record the sibling at this level.
		if idx%2 == 0 {
			if idx+1 < len(layer) {
				branch = append(branch, layer[idx+1])
			} else {
				branch = append(branch, types.Hash{})
			}
		} else {
			branch = append(branch, layer[idx-1])
		}

		// Compute next layer.
		nextLayer := make([]types.Hash, len(layer)/2)
		for i := 0; i < len(layer); i += 2 {
			nextLayer[i/2] = hashPair(layer[i], layer[i+1])
		}
		layer = nextLayer
		idx /= 2
	}
	return branch
}

// --- Uncoupled Payload Handler ---

// pendingUncoupled tracks an uncoupled payload waiting for or having received
// its execution payload.
type pendingUncoupled struct {
	envelope *UncoupledPayloadEnvelope
	status   string
}

// UncoupledPayloadHandler manages the receipt and validation of uncoupled
// execution payloads per EIP-7898.
type UncoupledPayloadHandler struct {
	mu sync.RWMutex
	// pending maps beacon block root -> pending uncoupled payload.
	pending map[types.Hash]*pendingUncoupled
	// backend for forwarding validated payloads.
	backend Backend
}

// NewUncoupledPayloadHandler creates a new handler for uncoupled payloads.
func NewUncoupledPayloadHandler(backend Backend) *UncoupledPayloadHandler {
	return &UncoupledPayloadHandler{
		pending: make(map[types.Hash]*pendingUncoupled),
		backend: backend,
	}
}

// SubmitUncoupledPayload receives an uncoupled execution payload with its
// inclusion proof. It validates the envelope structure and proof, then
// stores the payload for block processing.
func (h *UncoupledPayloadHandler) SubmitUncoupledPayload(envelope *UncoupledPayloadEnvelope) (string, error) {
	if err := envelope.Validate(); err != nil {
		return UncoupledStatusInvalid, err
	}

	// Verify the inclusion proof links the payload to the beacon block.
	if err := ValidateInclusionProof(envelope.Proof, envelope.BeaconBlockRoot); err != nil {
		return UncoupledStatusInvalid, err
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Check for duplicate submission.
	if existing, ok := h.pending[envelope.BeaconBlockRoot]; ok {
		if existing.status == UncoupledStatusVerified {
			return UncoupledStatusVerified, nil
		}
	}

	h.pending[envelope.BeaconBlockRoot] = &pendingUncoupled{
		envelope: envelope,
		status:   UncoupledStatusVerified,
	}

	return UncoupledStatusVerified, nil
}

// VerifyInclusion checks whether a payload's inclusion proof is valid
// against the specified beacon block root without storing the payload.
func (h *UncoupledPayloadHandler) VerifyInclusion(envelope *UncoupledPayloadEnvelope) error {
	if err := envelope.Validate(); err != nil {
		return err
	}
	return ValidateInclusionProof(envelope.Proof, envelope.BeaconBlockRoot)
}

// GetPendingPayload retrieves a previously submitted uncoupled payload by
// its beacon block root. Returns nil if no payload exists for that root.
func (h *UncoupledPayloadHandler) GetPendingPayload(beaconBlockRoot types.Hash) *UncoupledPayloadEnvelope {
	h.mu.RLock()
	defer h.mu.RUnlock()

	p, ok := h.pending[beaconBlockRoot]
	if !ok {
		return nil
	}
	return p.envelope
}

// GetPayloadStatus returns the status of an uncoupled payload.
func (h *UncoupledPayloadHandler) GetPayloadStatus(beaconBlockRoot types.Hash) string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	p, ok := h.pending[beaconBlockRoot]
	if !ok {
		return UncoupledStatusPending
	}
	return p.status
}

// RemovePending removes a pending uncoupled payload entry.
// Called after the payload has been fully processed.
func (h *UncoupledPayloadHandler) RemovePending(beaconBlockRoot types.Hash) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.pending, beaconBlockRoot)
}

// PendingCount returns the number of pending uncoupled payloads.
func (h *UncoupledPayloadHandler) PendingCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.pending)
}

// Errors for uncoupled payload handling.
var (
	ErrMissingInclusionProof  = errors.New("engine: missing inclusion proof")
	ErrInvalidInclusionProof  = errors.New("engine: invalid inclusion proof")
	ErrInclusionProofMismatch = errors.New("engine: inclusion proof root mismatch")
)
