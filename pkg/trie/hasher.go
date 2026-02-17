package trie

import (
	"github.com/eth2028/eth2028/crypto"
	"github.com/eth2028/eth2028/rlp"
)

// hasher computes the hash of trie nodes.
type hasher struct{}

// newHasher creates a new hasher.
func newHasher() *hasher {
	return &hasher{}
}

// hash computes the hash of a node. If the RLP-encoded node is less than 32
// bytes, the raw RLP encoding is returned (inline node). Otherwise, the
// Keccak-256 hash of the encoding is returned as a hashNode.
//
// If force is true, the hash is always computed even if the encoded node is
// less than 32 bytes (used for the root node).
func (h *hasher) hash(n node, force bool) (node, node) {
	if hash, dirty := n.cache(); hash != nil && !dirty {
		return hash, n
	}
	collapsed, cached := h.hashChildren(n)
	hashed, err := h.store(collapsed, force)
	if err != nil {
		panic("hasher: " + err.Error())
	}
	// Cache the hash on the original node.
	cachedHash, _ := hashed.(hashNode)
	switch cn := cached.(type) {
	case *shortNode:
		cn.flags.hash = cachedHash
		cn.flags.dirty = false
	case *fullNode:
		cn.flags.hash = cachedHash
		cn.flags.dirty = false
	}
	return hashed, cached
}

// hashChildren replaces child nodes with their hashes or inline encodings.
// Returns the collapsed (for hashing) and cached (for keeping in trie) versions.
func (h *hasher) hashChildren(original node) (node, node) {
	switch n := original.(type) {
	case *shortNode:
		collapsed, cached := n.copy(), n.copy()
		// The key needs to be compact-encoded for RLP serialization.
		collapsed.Key = hexToCompact(n.Key)
		// Hash the child if it is not a valueNode.
		if _, ok := n.Val.(valueNode); !ok {
			childH, childC := h.hash(n.Val, false)
			collapsed.Val = childH
			cached.Val = childC
		}
		return collapsed, cached
	case *fullNode:
		collapsed, cached := n.copy(), n.copy()
		for i := 0; i < 16; i++ {
			if n.Children[i] != nil {
				childH, childC := h.hash(n.Children[i], false)
				collapsed.Children[i] = childH
				cached.Children[i] = childC
			}
		}
		return collapsed, cached
	default:
		return n, n
	}
}

// store RLP-encodes a node and either returns the raw bytes (if < 32 bytes)
// or the Keccak-256 hash.
func (h *hasher) store(n node, force bool) (node, error) {
	if _, ok := n.(hashNode); ok {
		return n, nil
	}
	if _, ok := n.(valueNode); ok {
		return n, nil
	}

	enc, err := encodeNode(n)
	if err != nil {
		return nil, err
	}
	if len(enc) < 32 && !force {
		return n, nil
	}
	hash := crypto.Keccak256(enc)
	return hashNode(hash), nil
}

// encodeNode RLP-encodes a trie node for hashing/storage.
// shortNode => 2-element list [compactKey, val]
// fullNode  => 17-element list [child0..child15, value]
func encodeNode(n node) ([]byte, error) {
	switch n := n.(type) {
	case *shortNode:
		return encodeShortNode(n)
	case *fullNode:
		return encodeFullNode(n)
	case hashNode:
		// hashNode is already "encoded" as its own reference.
		return []byte(n), nil
	case valueNode:
		return rlp.EncodeToBytes([]byte(n))
	default:
		return nil, nil
	}
}

// encodeShortNode encodes a short node as a 2-element RLP list.
// The key should already be in compact encoding.
func encodeShortNode(n *shortNode) ([]byte, error) {
	// Encode the key as an RLP string.
	keyEnc, err := rlp.EncodeToBytes(n.Key)
	if err != nil {
		return nil, err
	}
	// Encode the value/child.
	valEnc, err := encodeNodeValue(n.Val)
	if err != nil {
		return nil, err
	}
	// Build the list payload.
	payload := append(keyEnc, valEnc...)
	return wrapListPayload(payload), nil
}

// encodeFullNode encodes a full node as a 17-element RLP list.
func encodeFullNode(n *fullNode) ([]byte, error) {
	var payload []byte
	for i := 0; i < 17; i++ {
		child := n.Children[i]
		enc, err := encodeNodeValue(child)
		if err != nil {
			return nil, err
		}
		payload = append(payload, enc...)
	}
	return wrapListPayload(payload), nil
}

// encodeNodeValue encodes a node for inclusion in a parent node's RLP.
// - nil / empty => RLP empty string (0x80)
// - valueNode => RLP string of the value bytes
// - hashNode => RLP string of the 32-byte hash
// - *shortNode / *fullNode (inline) => the raw RLP encoding of the node
func encodeNodeValue(n node) ([]byte, error) {
	if n == nil {
		return []byte{0x80}, nil // RLP empty string
	}
	switch n := n.(type) {
	case valueNode:
		return rlp.EncodeToBytes([]byte(n))
	case hashNode:
		return rlp.EncodeToBytes([]byte(n))
	case *shortNode:
		// Inline node: encode the node itself and return the raw RLP.
		return encodeShortNode(n)
	case *fullNode:
		return encodeFullNode(n)
	default:
		return []byte{0x80}, nil
	}
}

// wrapListPayload wraps the given payload bytes in an RLP list header.
func wrapListPayload(payload []byte) []byte {
	n := len(payload)
	if n <= 55 {
		buf := make([]byte, 1+n)
		buf[0] = 0xc0 + byte(n)
		copy(buf[1:], payload)
		return buf
	}
	lenBytes := putUintBigEndian(uint64(n))
	buf := make([]byte, 1+len(lenBytes)+n)
	buf[0] = 0xf7 + byte(len(lenBytes))
	copy(buf[1:], lenBytes)
	copy(buf[1+len(lenBytes):], payload)
	return buf
}

// putUintBigEndian encodes u as big-endian with no leading zeros.
func putUintBigEndian(u uint64) []byte {
	switch {
	case u < (1 << 8):
		return []byte{byte(u)}
	case u < (1 << 16):
		return []byte{byte(u >> 8), byte(u)}
	case u < (1 << 24):
		return []byte{byte(u >> 16), byte(u >> 8), byte(u)}
	case u < (1 << 32):
		return []byte{byte(u >> 24), byte(u >> 16), byte(u >> 8), byte(u)}
	default:
		return []byte{byte(u >> 56), byte(u >> 48), byte(u >> 40), byte(u >> 32),
			byte(u >> 24), byte(u >> 16), byte(u >> 8), byte(u)}
	}
}
