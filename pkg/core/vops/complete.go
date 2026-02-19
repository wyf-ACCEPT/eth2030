// complete.go extends VOPS with complete validity-only partial statelessness.
// A VOPSValidator can verify state transitions, receipts, and storage proofs
// using only witness data, without access to the full world state.
package vops

import (
	"errors"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

var (
	ErrWitnessNotFound = errors.New("vops: witness not found for state root")
	ErrEmptyWitness    = errors.New("vops: witness data must not be empty")
	ErrEmptyBlock      = errors.New("vops: block data must not be empty")
)

// VOPSValidator validates state transitions using only witness data and
// cryptographic proofs, enabling full stateless verification.
type VOPSValidator struct {
	mu            sync.RWMutex
	witnesses     map[types.Hash][]byte
	accessList    map[types.Address]bool
	storageProofs map[types.Hash][][]byte
}

// NewVOPSValidator creates a new VOPSValidator with empty state.
func NewVOPSValidator() *VOPSValidator {
	return &VOPSValidator{
		witnesses:     make(map[types.Hash][]byte),
		accessList:    make(map[types.Address]bool),
		storageProofs: make(map[types.Hash][][]byte),
	}
}

// AddWitness stores witness data keyed by state root. The witness data
// contains the partial state needed to verify transitions from that root.
func (v *VOPSValidator) AddWitness(stateRoot types.Hash, witness []byte) error {
	if len(witness) == 0 {
		return ErrEmptyWitness
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	// Copy witness data to prevent external mutation.
	cp := make([]byte, len(witness))
	copy(cp, witness)
	v.witnesses[stateRoot] = cp
	return nil
}

// AddAccessListEntry marks an address as accessed during validation.
func (v *VOPSValidator) AddAccessListEntry(addr types.Address) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.accessList[addr] = true
}

// AddStorageProof attaches a Merkle proof for a storage slot.
func (v *VOPSValidator) AddStorageProof(slot types.Hash, proof [][]byte) {
	v.mu.Lock()
	defer v.mu.Unlock()

	// Deep copy the proof slices.
	cp := make([][]byte, len(proof))
	for i, p := range proof {
		node := make([]byte, len(p))
		copy(node, p)
		cp[i] = node
	}
	v.storageProofs[slot] = cp
}

// ValidateTransition verifies a state transition from preRoot to postRoot
// using the stored witness for preRoot and the provided block data. It
// re-derives the expected post-state root by hashing the preRoot witness
// together with the block data and checks against the claimed postRoot.
func (v *VOPSValidator) ValidateTransition(preRoot, postRoot types.Hash, block []byte) (bool, error) {
	if len(block) == 0 {
		return false, ErrEmptyBlock
	}

	v.mu.RLock()
	witnessData, ok := v.witnesses[preRoot]
	v.mu.RUnlock()

	if !ok {
		return false, ErrWitnessNotFound
	}

	// Compute the expected post-state root from the witness + block data.
	// In production this would replay the STF; here we use a binding hash.
	expected := computePostRoot(preRoot, witnessData, block)
	return expected == postRoot, nil
}

// ValidateReceipt verifies that a receipt belongs to a receipt trie by
// checking its hash against the claimed receipt root.
func (v *VOPSValidator) ValidateReceipt(txHash types.Hash, receipt []byte, receiptRoot types.Hash) bool {
	if len(receipt) == 0 {
		return false
	}

	// Compute commitment: Keccak256(txHash || receipt).
	// In production this would verify a Merkle-Patricia proof; here we
	// use a simplified binding check.
	computed := crypto.Keccak256Hash(txHash[:], receipt)
	return computed == receiptRoot
}

// WitnessSize returns the total size in bytes of all stored witnesses.
func (v *VOPSValidator) WitnessSize() int {
	v.mu.RLock()
	defer v.mu.RUnlock()

	total := 0
	for _, w := range v.witnesses {
		total += len(w)
	}
	return total
}

// AccessedAddresses returns all addresses that have been marked in the
// access list, in no particular order.
func (v *VOPSValidator) AccessedAddresses() []types.Address {
	v.mu.RLock()
	defer v.mu.RUnlock()

	addrs := make([]types.Address, 0, len(v.accessList))
	for addr := range v.accessList {
		addrs = append(addrs, addr)
	}
	return addrs
}

// Reset clears all witnesses, access list entries, and storage proofs.
func (v *VOPSValidator) Reset() {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.witnesses = make(map[types.Hash][]byte)
	v.accessList = make(map[types.Address]bool)
	v.storageProofs = make(map[types.Hash][][]byte)
}

// computePostRoot derives the expected post-state root by hashing the
// pre-state root, witness data, and block data together. This is a
// simplified binding commitment; in production this would run the full
// state transition function over the witness data.
func computePostRoot(preRoot types.Hash, witness, block []byte) types.Hash {
	data := make([]byte, 0, len(preRoot)+len(witness)+len(block))
	data = append(data, preRoot[:]...)
	data = append(data, witness...)
	data = append(data, block...)
	return crypto.Keccak256Hash(data)
}
