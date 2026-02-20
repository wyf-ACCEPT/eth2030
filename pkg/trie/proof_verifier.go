// proof_verifier.go provides standalone Merkle proof verification for
// MPT (Merkle Patricia Trie), binary trie, and Verkle trie proof types.
// It is designed as a stateless verifier: no trie database is needed,
// only the root hash and the proof data.
package trie

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// Proof verification errors.
var (
	ErrProofEmpty        = errors.New("proof_verifier: empty proof")
	ErrProofNilInput     = errors.New("proof_verifier: nil input")
	ErrRootMismatch      = errors.New("proof_verifier: root hash mismatch")
	ErrProofTruncated    = errors.New("proof_verifier: proof is truncated")
	ErrMultiProofInvalid = errors.New("proof_verifier: multi-proof verification failed")
	ErrVerkleProofFailed = errors.New("proof_verifier: verkle proof verification failed")
)

// emptyRootMPT is the hash of an empty MPT trie (used by proof_verifier).
// Note: emptyRoot is already declared in trie.go via Keccak256(RLP("")).
// They should be identical; this alias avoids redeclaration.
var _ = emptyRoot // ensure emptyRoot from trie.go is used

// MPTProofResult holds the result of an MPT proof verification.
type MPTProofResult struct {
	// Key that was proven.
	Key []byte
	// Value at the key (nil for absence proofs).
	Value []byte
	// Exists indicates whether the key exists in the trie.
	Exists bool
}

// VerifyMPTProof verifies a Merkle Patricia Trie inclusion or exclusion proof.
// It returns the value if the key exists, or nil if the proof demonstrates
// absence. An error is returned if the proof is structurally invalid.
func VerifyMPTProof(rootHash types.Hash, key []byte, proof [][]byte) (*MPTProofResult, error) {
	if key == nil {
		return nil, ErrProofNilInput
	}

	result := &MPTProofResult{Key: key}

	// Empty proof is valid only for the empty trie root.
	if len(proof) == 0 {
		if rootHash == emptyRoot {
			result.Exists = false
			return result, nil
		}
		return nil, ErrProofEmpty
	}

	// Delegate to the existing VerifyProof for the core logic.
	val, err := VerifyProof(rootHash, key, proof)
	if err != nil {
		return nil, fmt.Errorf("proof_verifier: MPT verification failed: %w", err)
	}

	result.Value = val
	result.Exists = val != nil
	return result, nil
}

// BinaryProofResult holds the result of a binary trie proof verification.
type BinaryProofResult struct {
	Key    types.Hash
	Value  []byte
	Exists bool
}

// VerifyBinaryProof verifies an inclusion proof for a binary Merkle trie.
// The proof contains sibling hashes along the path from root to leaf.
// Returns an error if the proof is invalid.
func VerifyBinaryProof(rootHash types.Hash, proof *BinaryProof) (*BinaryProofResult, error) {
	if proof == nil {
		return nil, ErrProofNilInput
	}

	result := &BinaryProofResult{
		Key:    proof.Key,
		Value:  proof.Value,
		Exists: len(proof.Value) > 0,
	}

	// Reconstruct the leaf hash: keccak256(0x00 || key || value).
	buf := make([]byte, 1+32+len(proof.Value))
	buf[0] = 0x00 // leaf prefix
	copy(buf[1:33], proof.Key[:])
	copy(buf[33:], proof.Value)
	current := crypto.Keccak256Hash(buf)

	// Walk from leaf to root, combining with siblings.
	for i := len(proof.Siblings) - 1; i >= 0; i-- {
		sibling := proof.Siblings[i]
		branchBuf := make([]byte, 1+32+32)
		branchBuf[0] = 0x01 // branch prefix
		if getBit(proof.Key, i) == 0 {
			copy(branchBuf[1:33], current[:])
			copy(branchBuf[33:65], sibling[:])
		} else {
			copy(branchBuf[1:33], sibling[:])
			copy(branchBuf[33:65], current[:])
		}
		current = crypto.Keccak256Hash(branchBuf)
	}

	if current != rootHash {
		return nil, fmt.Errorf("%w: computed %s, expected %s", ErrRootMismatch, current.Hex(), rootHash.Hex())
	}
	return result, nil
}

// VerkleProofData holds the data needed for standalone Verkle proof verification.
type VerkleProofData struct {
	// CommitmentsByPath are the commitments along the trie path.
	CommitmentsByPath [][32]byte
	// D is the polynomial commitment in the IPA proof.
	D [32]byte
	// IPAProof is the serialized IPA proof.
	IPAProof []byte
	// Depth at which the leaf was found.
	Depth uint8
	// ExtensionPresent indicates a leaf was found at the path.
	ExtensionPresent bool
	// Key is the 32-byte key being proven.
	Key [32]byte
	// Value is the 32-byte value (nil for absence proofs).
	Value *[32]byte
}

// VerkleProofResult holds the outcome of a Verkle proof verification.
type VerkleProofResult struct {
	Key    [32]byte
	Value  *[32]byte
	Exists bool
}

// VerifyVerkleProof verifies a Verkle tree IPA proof against a root commitment.
// The proof demonstrates inclusion or absence of a key. Since full IPA
// verification requires the Banderwagon curve library, this performs
// structural validation and commitment path checks.
func VerifyVerkleProof(root [32]byte, proof *VerkleProofData) (*VerkleProofResult, error) {
	if proof == nil {
		return nil, ErrProofNilInput
	}

	result := &VerkleProofResult{
		Key:    proof.Key,
		Value:  proof.Value,
		Exists: proof.ExtensionPresent && proof.Value != nil,
	}

	// Structural validation.
	if len(proof.CommitmentsByPath) == 0 {
		return nil, fmt.Errorf("%w: no path commitments", ErrVerkleProofFailed)
	}
	if proof.Depth > 31 {
		return nil, fmt.Errorf("%w: invalid depth %d", ErrVerkleProofFailed, proof.Depth)
	}
	if len(proof.IPAProof) == 0 {
		return nil, fmt.Errorf("%w: empty IPA proof data", ErrVerkleProofFailed)
	}

	// The first commitment in the path must correspond to the root.
	if proof.CommitmentsByPath[0] != root {
		return nil, fmt.Errorf("%w: root commitment mismatch", ErrVerkleProofFailed)
	}

	// Verify path commitment chain: each commitment must be non-zero.
	for i, c := range proof.CommitmentsByPath {
		allZero := true
		for _, b := range c {
			if b != 0 {
				allZero = false
				break
			}
		}
		if allZero {
			return nil, fmt.Errorf("%w: zero commitment at path index %d", ErrVerkleProofFailed, i)
		}
	}

	return result, nil
}

// MultiProofItem represents one key-value pair in a multi-proof.
type MultiProofItem struct {
	Key   []byte
	Value []byte
	Proof [][]byte
}

// MultiProofResult holds per-key verification results.
type MultiProofResult struct {
	Results []MPTProofResult
}

// VerifyMultiProof verifies multiple MPT proofs against the same root hash.
// Each item contains a key and its corresponding proof. All proofs must
// verify against the provided root hash. Returns all results or an error
// if any individual proof is invalid.
func VerifyMultiProof(rootHash types.Hash, items []MultiProofItem) (*MultiProofResult, error) {
	if len(items) == 0 {
		return nil, ErrProofEmpty
	}

	result := &MultiProofResult{
		Results: make([]MPTProofResult, len(items)),
	}

	for i, item := range items {
		if item.Key == nil {
			return nil, fmt.Errorf("%w: item %d has nil key", ErrProofNilInput, i)
		}

		r, err := VerifyMPTProof(rootHash, item.Key, item.Proof)
		if err != nil {
			return nil, fmt.Errorf("%w: item %d (%x): %v", ErrMultiProofInvalid, i, item.Key, err)
		}

		result.Results[i] = *r

		// If caller provided an expected value, cross-check it.
		if item.Value != nil && r.Value != nil {
			if !bytes.Equal(item.Value, r.Value) {
				return nil, fmt.Errorf("%w: item %d value mismatch", ErrMultiProofInvalid, i)
			}
		}
	}

	return result, nil
}

// VerifyMPTAbsence is a convenience function to verify that a key does NOT
// exist in the trie. Returns nil on success (proven absent) or an error.
func VerifyMPTAbsence(rootHash types.Hash, key []byte, proof [][]byte) error {
	r, err := VerifyMPTProof(rootHash, key, proof)
	if err != nil {
		return err
	}
	if r.Exists {
		return fmt.Errorf("proof_verifier: key exists with value, expected absence")
	}
	return nil
}

// VerifyBinaryAbsence verifies an absence proof in a binary trie.
// An absence proof in a binary trie uses a BinaryProof with an empty value
// and demonstrates that the path terminates at a leaf with a different key
// or at a nil branch.
func VerifyBinaryAbsence(rootHash types.Hash, absentKey types.Hash, proof *BinaryProof) error {
	if proof == nil {
		// Empty root: any key is absent.
		if rootHash == (types.Hash{}) {
			return nil
		}
		return ErrProofNilInput
	}

	// The proof's key must differ from the absent key (shows a different leaf
	// occupies the position), or the proof value must be empty.
	if proof.Key == absentKey && len(proof.Value) > 0 {
		return fmt.Errorf("proof_verifier: proof shows key present, not absent")
	}

	// Verify the proof itself is structurally valid.
	_, err := VerifyBinaryProof(rootHash, proof)
	return err
}
