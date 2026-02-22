package vops

import (
	"math/big"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/witness"
)

// ValidateTransition verifies that the given validity proof correctly
// attests to a state transition from preRoot to postRoot. The proof
// contains the accessed keys and proof data that allow verification
// without replaying the full execution.
func ValidateTransition(preRoot, postRoot types.Hash, proof *ValidityProof) bool {
	if proof == nil {
		return false
	}
	if proof.PreStateRoot != preRoot {
		return false
	}
	if proof.PostStateRoot != postRoot {
		return false
	}
	if len(proof.AccessedKeys) == 0 {
		return false
	}
	if len(proof.ProofData) == 0 {
		return false
	}

	// Verify the proof data is a valid commitment over the accessed keys
	// and state roots. In production this would verify a SNARK/STARK; here
	// we check a Keccak256 binding commitment.
	expected := computeProofCommitment(preRoot, postRoot, proof.AccessedKeys)
	return types.BytesToHash(expected) == types.BytesToHash(proof.ProofData)
}

// BuildValidityProof constructs a ValidityProof for a state transition.
func BuildValidityProof(preRoot, postRoot types.Hash, accessedKeys [][]byte) *ValidityProof {
	commitment := computeProofCommitment(preRoot, postRoot, accessedKeys)
	return &ValidityProof{
		PreStateRoot:  preRoot,
		PostStateRoot: postRoot,
		AccessedKeys:  accessedKeys,
		ProofData:     commitment,
	}
}

// computeProofCommitment derives a binding commitment over roots and keys.
func computeProofCommitment(preRoot, postRoot types.Hash, keys [][]byte) []byte {
	var data []byte
	data = append(data, preRoot[:]...)
	data = append(data, postRoot[:]...)
	for _, k := range keys {
		data = append(data, k...)
	}
	return crypto.Keccak256(data)
}

// BuildPartialStateFromWitness converts an execution witness into a
// PartialState suitable for VOPS validation. The witness captures all
// state reads/writes from block execution, which maps directly to the
// state subset needed for partial validation.
func BuildPartialStateFromWitness(w *witness.ExecutionWitness) *PartialState {
	if w == nil {
		return NewPartialState()
	}

	ps := NewPartialState()
	for _, stemDiff := range w.State {
		// Each stem+suffix pair represents a Verkle tree leaf.
		// We reconstruct account data from the known suffixes.
		for _, sd := range stemDiff.Suffixes {
			if sd.CurrentValue == nil {
				continue
			}
			// The stem identifies an address group; we reconstruct
			// a synthetic address from the stem for tracking.
			var addrBytes [types.AddressLength]byte
			copy(addrBytes[:], stemDiff.Stem[:])
			addr := types.BytesToAddress(addrBytes[:])

			// Ensure account exists in partial state.
			if ps.GetAccount(addr) == nil {
				ps.SetAccount(addr, &AccountState{
					Balance:  new(big.Int),
					CodeHash: types.EmptyCodeHash,
				})
			}
			acct := ps.GetAccount(addr)

			// Map suffixes to account fields per EIP-6800 layout.
			switch sd.Suffix {
			case 0: // version (ignored for now)
			case 1: // balance
				acct.Balance = new(big.Int).SetBytes(sd.CurrentValue[:])
			case 2: // nonce
				acct.Nonce = bytesToUint64(sd.CurrentValue[:])
			case 3: // code hash
				copy(acct.CodeHash[:], sd.CurrentValue[:])
			default:
				// Storage slot or code chunk - store as storage.
				var key types.Hash
				key[0] = sd.Suffix
				ps.SetStorage(addr, key, types.BytesToHash(sd.CurrentValue[:]))
			}
		}
	}
	return ps
}

// bytesToUint64 reads a little-endian uint64 from up to 8 bytes.
func bytesToUint64(b []byte) uint64 {
	var result uint64
	for i := 0; i < len(b) && i < 8; i++ {
		result |= uint64(b[i]) << (8 * uint(i))
	}
	return result
}
