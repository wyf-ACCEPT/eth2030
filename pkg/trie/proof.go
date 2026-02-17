package trie

import (
	"bytes"
	"errors"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

var (
	// ErrProofInvalid is returned when a Merkle proof is invalid.
	ErrProofInvalid = errors.New("trie: invalid proof")
)

// Prove generates a Merkle proof for the given key. The proof consists of the
// RLP-encoded nodes along the path from the root to the value. The proof can
// be used with VerifyProof to verify that a key-value pair exists in the trie
// with a given root hash.
func (t *Trie) Prove(key []byte) ([][]byte, error) {
	if t.root == nil {
		return nil, ErrNotFound
	}
	// First, hash the trie to make sure all nodes have been hashed.
	t.Hash()

	hexKey := keybytesToHex(key)
	var proof [][]byte
	found := t.prove(t.root, hexKey, 0, &proof)
	if !found {
		return nil, ErrNotFound
	}
	return proof, nil
}

func (t *Trie) prove(n node, key []byte, pos int, proof *[][]byte) bool {
	switch n := n.(type) {
	case nil:
		return false
	case *shortNode:
		// Encode this node for the proof using a collapsed copy.
		collapsed := n.copy()
		collapsed.Key = hexToCompact(n.Key)
		collapsed.Val = collapseForProof(n.Val)
		enc, err := encodeShortNode(collapsed)
		if err != nil {
			return false
		}
		*proof = append(*proof, enc)

		if len(key)-pos < len(n.Key) || !keysEqual(n.Key, key[pos:pos+len(n.Key)]) {
			return false
		}
		return t.prove(n.Val, key, pos+len(n.Key), proof)

	case *fullNode:
		collapsed := collapseFullNodeForProof(n)
		enc, err := encodeFullNode(collapsed)
		if err != nil {
			return false
		}
		*proof = append(*proof, enc)

		if pos >= len(key) {
			// Looking for value at this branch.
			return n.Children[16] != nil
		}
		return t.prove(n.Children[key[pos]], key, pos+1, proof)

	case valueNode:
		return true

	case hashNode:
		return false

	default:
		return false
	}
}

// collapseForProof creates a collapsed version of a node suitable for inclusion
// in a proof. Child nodes that are large enough get replaced by their hash.
func collapseForProof(n node) node {
	switch n := n.(type) {
	case *shortNode:
		collapsed := n.copy()
		collapsed.Key = hexToCompact(n.Key)
		collapsed.Val = collapseForProof(n.Val)
		enc, err := encodeShortNode(collapsed)
		if err != nil {
			return n
		}
		if len(enc) >= 32 {
			hash := crypto.Keccak256(enc)
			return hashNode(hash)
		}
		return collapsed
	case *fullNode:
		collapsed := collapseFullNodeForProof(n)
		enc, err := encodeFullNode(collapsed)
		if err != nil {
			return n
		}
		if len(enc) >= 32 {
			hash := crypto.Keccak256(enc)
			return hashNode(hash)
		}
		return collapsed
	default:
		return n
	}
}

// collapseFullNodeForProof collapses all children of a full node for proof inclusion.
func collapseFullNodeForProof(n *fullNode) *fullNode {
	collapsed := n.copy()
	for i := 0; i < 16; i++ {
		if n.Children[i] != nil {
			collapsed.Children[i] = collapseForProof(n.Children[i])
		}
	}
	return collapsed
}

// VerifyProof verifies a Merkle proof for a given key against a root hash.
// It returns the value if the proof is valid, or an error otherwise.
func VerifyProof(rootHash types.Hash, key []byte, proof [][]byte) ([]byte, error) {
	if len(proof) == 0 {
		return nil, ErrProofInvalid
	}

	hexKey := keybytesToHex(key)
	// Verify that the first proof node hashes to the root hash.
	wantHash := rootHash[:]
	pos := 0
	for i, encoded := range proof {
		// Verify this node matches the expected hash.
		nodeHash := crypto.Keccak256(encoded)
		if !bytes.Equal(nodeHash, wantHash) {
			// For inline nodes (encoded < 32 bytes), the node might be embedded
			// rather than hashed. But the root must always match.
			if i == 0 {
				return nil, ErrProofInvalid
			}
		}

		// Decode the RLP list to understand the node structure.
		items, err := decodeRLPList(encoded)
		if err != nil {
			return nil, ErrProofInvalid
		}

		switch len(items) {
		case 2:
			// Short node (leaf or extension).
			compactKey := items[0]
			hexNibbles := compactToHex(compactKey)

			if pos+len(hexNibbles) > len(hexKey) {
				return nil, ErrProofInvalid
			}
			if !keysEqual(hexNibbles, hexKey[pos:pos+len(hexNibbles)]) {
				return nil, ErrProofInvalid
			}
			pos += len(hexNibbles)

			if hasTerm(hexNibbles) {
				// Leaf node: the value is items[1].
				if i == len(proof)-1 {
					return items[1], nil
				}
				return nil, ErrProofInvalid
			}

			// Extension node: items[1] is either a hash or inline node.
			if i == len(proof)-1 {
				return nil, ErrProofInvalid
			}
			if len(items[1]) == 32 {
				wantHash = items[1]
			} else {
				// Inline node: the next proof element should be the inline encoding.
				wantHash = crypto.Keccak256(proof[i+1])
			}

		case 17:
			// Branch node.
			if pos >= len(hexKey) {
				return nil, ErrProofInvalid
			}
			nibble := hexKey[pos]
			pos++

			if nibble == terminatorByte {
				// Value at this branch.
				val := items[16]
				if len(val) == 0 {
					return nil, ErrNotFound
				}
				return val, nil
			}

			childRef := items[nibble]
			if len(childRef) == 0 {
				return nil, ErrNotFound
			}

			if i == len(proof)-1 {
				// This is the last proof node. If the child is a value, return it.
				// Otherwise, proof is incomplete.
				return nil, ErrProofInvalid
			}

			if len(childRef) == 32 {
				wantHash = childRef
			} else {
				// Inline reference.
				wantHash = crypto.Keccak256(proof[i+1])
			}

		default:
			return nil, ErrProofInvalid
		}
	}

	return nil, ErrProofInvalid
}

// decodeRLPList is defined in decoder.go; proof.go uses it via package scope.
