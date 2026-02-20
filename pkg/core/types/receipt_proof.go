package types

import (
	"encoding/binary"
	"errors"

	"golang.org/x/crypto/sha3"
)

// Receipt proof error codes.
var (
	ErrReceiptProofIndexOOB = errors.New("receipt proof: index out of bounds")
	ErrReceiptProofEmpty    = errors.New("receipt proof: empty receipt list")
	ErrReceiptProofInvalid  = errors.New("receipt proof: verification failed")
)

// ReceiptProof contains a Merkle proof for a single receipt within a block.
// It allows verification that a receipt at a given index is included in the
// receipts root without needing all receipts.
type ReceiptProof struct {
	Index       int      // receipt index in the block
	ReceiptData []byte   // RLP-encoded receipt
	ProofPath   [][]byte // sibling hashes along the Merkle path (leaf to root)
	RootHash    Hash     // expected Merkle root of the receipt trie
}

// ReceiptProofGenerator generates and verifies Merkle proofs for transaction
// receipts within a block.
type ReceiptProofGenerator struct{}

// NewReceiptProofGenerator creates a new ReceiptProofGenerator.
func NewReceiptProofGenerator() *ReceiptProofGenerator {
	return &ReceiptProofGenerator{}
}

// GenerateProof generates a Merkle proof for the receipt at the given index.
// The proof consists of sibling hashes along the path from the leaf to the root.
func (g *ReceiptProofGenerator) GenerateProof(receipts []*Receipt, index int) (*ReceiptProof, error) {
	if len(receipts) == 0 {
		return nil, ErrReceiptProofEmpty
	}
	if index < 0 || index >= len(receipts) {
		return nil, ErrReceiptProofIndexOOB
	}

	// Encode all receipts and compute leaf hashes.
	leaves := make([][]byte, len(receipts))
	for i, r := range receipts {
		leaves[i] = hashReceiptLeaf(i, encodeReceiptForProof(r))
	}

	// Collect the proof path (sibling hashes at each tree level).
	proofPath := collectMerkleProof(leaves, index)

	root := computeBalancedMerkleRoot(leaves)
	var rootHash Hash
	copy(rootHash[:], root)

	return &ReceiptProof{
		Index:       index,
		ReceiptData: encodeReceiptForProof(receipts[index]),
		ProofPath:   proofPath,
		RootHash:    rootHash,
	}, nil
}

// VerifyProof verifies that a receipt proof is valid against its stated root.
// Returns (true, nil) if the proof is valid.
func (g *ReceiptProofGenerator) VerifyProof(proof *ReceiptProof) (bool, error) {
	if proof == nil {
		return false, ErrReceiptProofInvalid
	}
	if len(proof.ReceiptData) == 0 {
		return false, ErrReceiptProofInvalid
	}

	// Compute the leaf hash for the receipt at the given index.
	current := hashReceiptLeaf(proof.Index, proof.ReceiptData)

	// Walk up the tree using the proof path.
	idx := proof.Index
	for _, sibling := range proof.ProofPath {
		d := sha3.NewLegacyKeccak256()
		if idx%2 == 0 {
			// Current is left child, sibling is right.
			d.Write(current)
			d.Write(sibling)
		} else {
			// Current is right child, sibling is left.
			d.Write(sibling)
			d.Write(current)
		}
		current = d.Sum(nil)
		idx /= 2
	}

	var computedRoot Hash
	copy(computedRoot[:], current)
	if computedRoot != proof.RootHash {
		return false, ErrReceiptProofInvalid
	}
	return true, nil
}

// ComputeReceiptsRoot computes the Merkle root hash for a list of receipts.
// Returns EmptyRootHash for an empty list.
func (g *ReceiptProofGenerator) ComputeReceiptsRoot(receipts []*Receipt) Hash {
	if len(receipts) == 0 {
		return EmptyRootHash
	}
	leaves := make([][]byte, len(receipts))
	for i, r := range receipts {
		leaves[i] = hashReceiptLeaf(i, encodeReceiptForProof(r))
	}
	root := computeBalancedMerkleRoot(leaves)
	var h Hash
	copy(h[:], root)
	return h
}

// BatchGenerateProofs generates a Merkle proof for every receipt in the list.
func (g *ReceiptProofGenerator) BatchGenerateProofs(receipts []*Receipt) ([]*ReceiptProof, error) {
	if len(receipts) == 0 {
		return nil, ErrReceiptProofEmpty
	}
	proofs := make([]*ReceiptProof, len(receipts))
	for i := range receipts {
		p, err := g.GenerateProof(receipts, i)
		if err != nil {
			return nil, err
		}
		proofs[i] = p
	}
	return proofs, nil
}

// encodeReceiptForProof RLP-encodes a receipt for use in proof hashing.
// Returns nil for nil receipts.
func encodeReceiptForProof(r *Receipt) []byte {
	if r == nil {
		return nil
	}
	enc, err := r.EncodeRLP()
	if err != nil {
		return nil
	}
	return enc
}

// hashReceiptLeaf computes the leaf hash for a receipt: keccak256(index || data).
func hashReceiptLeaf(index int, data []byte) []byte {
	d := sha3.NewLegacyKeccak256()
	var idxBuf [8]byte
	binary.BigEndian.PutUint64(idxBuf[:], uint64(index))
	d.Write(idxBuf[:])
	d.Write(data)
	return d.Sum(nil)
}

// computeBalancedMerkleRoot builds a balanced binary Merkle tree. When a
// level has an odd number of nodes, the last node is duplicated so every
// node always has a sibling. This guarantees proof paths have a consistent
// length and every node participates in a hash.
func computeBalancedMerkleRoot(leaves [][]byte) []byte {
	if len(leaves) == 0 {
		return EmptyRootHash[:]
	}
	if len(leaves) == 1 {
		return leaves[0]
	}

	level := make([][]byte, len(leaves))
	copy(level, leaves)

	for len(level) > 1 {
		// Duplicate last element if odd count.
		if len(level)%2 != 0 {
			level = append(level, level[len(level)-1])
		}
		var next [][]byte
		for i := 0; i < len(level); i += 2 {
			d := sha3.NewLegacyKeccak256()
			d.Write(level[i])
			d.Write(level[i+1])
			next = append(next, d.Sum(nil))
		}
		level = next
	}
	return level[0]
}

// collectMerkleProof collects sibling hashes for a balanced Merkle proof.
// When a level has an odd count, the last node is duplicated (consistent
// with computeBalancedMerkleRoot). The result is ordered leaf-to-root.
func collectMerkleProof(leaves [][]byte, index int) [][]byte {
	if len(leaves) <= 1 {
		return nil
	}

	level := make([][]byte, len(leaves))
	copy(level, leaves)

	var proof [][]byte
	idx := index

	for len(level) > 1 {
		// Duplicate last element if odd count.
		if len(level)%2 != 0 {
			level = append(level, level[len(level)-1])
		}

		// The sibling of idx is idx^1 (flip last bit).
		siblingIdx := idx ^ 1
		proof = append(proof, level[siblingIdx])

		// Build next level.
		var next [][]byte
		for i := 0; i < len(level); i += 2 {
			d := sha3.NewLegacyKeccak256()
			d.Write(level[i])
			d.Write(level[i+1])
			next = append(next, d.Sum(nil))
		}
		level = next
		idx /= 2
	}
	return proof
}
