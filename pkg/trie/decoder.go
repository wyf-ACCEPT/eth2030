package trie

import (
	"errors"
	"fmt"
)

var (
	errDecodeInvalid = errors.New("trie: invalid encoded node")
)

// decodeNode decodes an RLP-encoded trie node.
// The hash is the expected hash reference of this node (for caching).
func decodeNode(hash hashNode, data []byte) (node, error) {
	if len(data) == 0 {
		return nil, errDecodeInvalid
	}

	// Decode the RLP.
	elems, err := decodeRLPList(data)
	if err != nil {
		return nil, fmt.Errorf("trie decode: %w", err)
	}

	switch len(elems) {
	case 2:
		return decodeShort(hash, elems)
	case 17:
		return decodeFull(hash, elems)
	default:
		return nil, fmt.Errorf("%w: expected 2 or 17 elements, got %d", errDecodeInvalid, len(elems))
	}
}

// decodeShort decodes a 2-element RLP list into a shortNode.
func decodeShort(hash hashNode, elems [][]byte) (node, error) {
	// First element is the compact-encoded key.
	key := compactToHex(elems[0])

	// Second element is either a value (leaf) or a child reference (extension).
	if hasTerm(key) {
		// Leaf node: value is the second element.
		return &shortNode{
			Key: key,
			Val: valueNode(elems[1]),
			flags: nodeFlag{
				hash:  hash,
				dirty: false,
			},
		}, nil
	}

	// Extension node: second element is a child node reference.
	child, err := decodeRef(elems[1])
	if err != nil {
		return nil, err
	}
	return &shortNode{
		Key: key,
		Val: child,
		flags: nodeFlag{
			hash:  hash,
			dirty: false,
		},
	}, nil
}

// decodeFull decodes a 17-element RLP list into a fullNode.
func decodeFull(hash hashNode, elems [][]byte) (node, error) {
	n := &fullNode{
		flags: nodeFlag{
			hash:  hash,
			dirty: false,
		},
	}
	for i := 0; i < 16; i++ {
		if len(elems[i]) == 0 {
			continue
		}
		child, err := decodeRef(elems[i])
		if err != nil {
			return nil, err
		}
		n.Children[i] = child
	}
	// Element 17 is the value at this branch point.
	if len(elems[16]) > 0 {
		n.Children[16] = valueNode(elems[16])
	}
	return n, nil
}

// decodeRef decodes a child node reference.
// If the data is 32 bytes, it's a hash reference.
// Otherwise, it's an inline node (decode recursively).
func decodeRef(data []byte) (node, error) {
	if len(data) == 0 {
		return nil, nil
	}
	// 32-byte hash reference.
	if len(data) == 32 {
		return hashNode(data), nil
	}
	// Inline node: decode it.
	return decodeNode(nil, data)
}

// decodeLength decodes a big-endian length from the given bytes.
func decodeLength(data []byte, lenLen int) int {
	var length int
	for i := 0; i < lenLen; i++ {
		length = length<<8 | int(data[i])
	}
	return length
}

// decodeRLPList decodes a top-level RLP list into its element byte slices.
func decodeRLPList(data []byte) ([][]byte, error) {
	if len(data) == 0 {
		return nil, errDecodeInvalid
	}

	// Parse the list header.
	prefix := data[0]
	if prefix < 0xc0 {
		return nil, fmt.Errorf("%w: expected list, got string prefix 0x%02x", errDecodeInvalid, prefix)
	}
	var payload []byte

	switch {
	case prefix <= 0xf7:
		length := int(prefix - 0xc0)
		if 1+length > len(data) {
			return nil, errDecodeInvalid
		}
		payload = data[1 : 1+length]
	default:
		lenLen := int(prefix - 0xf7)
		if 1+lenLen > len(data) {
			return nil, errDecodeInvalid
		}
		length := decodeLength(data[1:1+lenLen], lenLen)
		if 1+lenLen+length > len(data) {
			return nil, errDecodeInvalid
		}
		payload = data[1+lenLen : 1+lenLen+length]
	}

	// Parse individual elements from the payload.
	var elems [][]byte
	for len(payload) > 0 {
		elem, rest, err := decodeOneElement(payload)
		if err != nil {
			return nil, err
		}
		elems = append(elems, elem)
		payload = rest
	}
	return elems, nil
}

// decodeOneElement reads one RLP element from the front of data,
// returning the decoded content and remaining data.
func decodeOneElement(data []byte) (content []byte, rest []byte, err error) {
	if len(data) == 0 {
		return nil, nil, errDecodeInvalid
	}

	prefix := data[0]
	switch {
	case prefix <= 0x7f:
		// Single byte.
		return data[:1], data[1:], nil

	case prefix == 0x80:
		// Empty string.
		return nil, data[1:], nil

	case prefix <= 0xb7:
		// Short string.
		length := int(prefix - 0x80)
		if 1+length > len(data) {
			return nil, nil, errDecodeInvalid
		}
		return data[1 : 1+length], data[1+length:], nil

	case prefix <= 0xbf:
		// Long string.
		lenLen := int(prefix - 0xb7)
		if 1+lenLen > len(data) {
			return nil, nil, errDecodeInvalid
		}
		length := decodeLength(data[1:1+lenLen], lenLen)
		end := 1 + lenLen + length
		if end > len(data) {
			return nil, nil, errDecodeInvalid
		}
		return data[1+lenLen : end], data[end:], nil

	case prefix <= 0xf7:
		// Short list: return the list content (without header) for inline node decoding.
		length := int(prefix - 0xc0)
		end := 1 + length
		if end > len(data) {
			return nil, nil, errDecodeInvalid
		}
		// Return the full RLP (including header) for nested node references.
		return data[:end], data[end:], nil

	default:
		// Long list.
		lenLen := int(prefix - 0xf7)
		if 1+lenLen > len(data) {
			return nil, nil, errDecodeInvalid
		}
		length := decodeLength(data[1:1+lenLen], lenLen)
		end := 1 + lenLen + length
		if end > len(data) {
			return nil, nil, errDecodeInvalid
		}
		return data[:end], data[end:], nil
	}
}
