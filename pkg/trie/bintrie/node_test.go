package bintrie

import (
	"bytes"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestSerializeDeserializeInternalNode(t *testing.T) {
	leftHash := types.HexToHash("1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	rightHash := types.HexToHash("fedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321")

	node := &InternalNode{
		depth: 5,
		left:  HashedNode(leftHash),
		right: HashedNode(rightHash),
	}

	serialized := SerializeNode(node)

	if serialized[0] != nodeTypeInternal {
		t.Errorf("Expected type byte %d, got %d", nodeTypeInternal, serialized[0])
	}
	if len(serialized) != 65 {
		t.Errorf("Expected serialized length 65, got %d", len(serialized))
	}

	deserialized, err := DeserializeNode(serialized, 5)
	if err != nil {
		t.Fatalf("Failed to deserialize: %v", err)
	}

	internalNode, ok := deserialized.(*InternalNode)
	if !ok {
		t.Fatalf("Expected InternalNode, got %T", deserialized)
	}

	if internalNode.depth != 5 {
		t.Errorf("Expected depth 5, got %d", internalNode.depth)
	}
	if internalNode.left.Hash() != leftHash {
		t.Errorf("Left hash mismatch: %x vs %x", internalNode.left.Hash(), leftHash)
	}
	if internalNode.right.Hash() != rightHash {
		t.Errorf("Right hash mismatch: %x vs %x", internalNode.right.Hash(), rightHash)
	}
}

func TestSerializeDeserializeStemNode(t *testing.T) {
	stem := make([]byte, StemSize)
	for i := range stem {
		stem[i] = byte(i)
	}

	var values [StemNodeWidth][]byte
	values[0] = types.HexToHash("0101010101010101010101010101010101010101010101010101010101010101").Bytes()
	values[10] = types.HexToHash("0202020202020202020202020202020202020202020202020202020202020202").Bytes()
	values[255] = types.HexToHash("0303030303030303030303030303030303030303030303030303030303030303").Bytes()

	node := &StemNode{
		Stem:   stem,
		Values: values[:],
		depth:  10,
	}

	serialized := SerializeNode(node)

	if serialized[0] != nodeTypeStem {
		t.Errorf("Expected type byte %d, got %d", nodeTypeStem, serialized[0])
	}
	if !bytes.Equal(serialized[1:1+StemSize], stem) {
		t.Error("Stem mismatch in serialized data")
	}

	deserialized, err := DeserializeNode(serialized, 10)
	if err != nil {
		t.Fatalf("Failed to deserialize: %v", err)
	}

	stemNode, ok := deserialized.(*StemNode)
	if !ok {
		t.Fatalf("Expected StemNode, got %T", deserialized)
	}

	if !bytes.Equal(stemNode.Stem, stem) {
		t.Error("Stem mismatch after deserialization")
	}
	if !bytes.Equal(stemNode.Values[0], values[0]) {
		t.Error("Value at index 0 mismatch")
	}
	if !bytes.Equal(stemNode.Values[10], values[10]) {
		t.Error("Value at index 10 mismatch")
	}
	if !bytes.Equal(stemNode.Values[255], values[255]) {
		t.Error("Value at index 255 mismatch")
	}

	for i := range StemNodeWidth {
		if i == 0 || i == 10 || i == 255 {
			continue
		}
		if stemNode.Values[i] != nil {
			t.Errorf("Expected nil at index %d, got %x", i, stemNode.Values[i])
		}
	}
}

func TestDeserializeEmptyNode(t *testing.T) {
	deserialized, err := DeserializeNode([]byte{}, 0)
	if err != nil {
		t.Fatalf("Failed to deserialize empty: %v", err)
	}
	if _, ok := deserialized.(Empty); !ok {
		t.Fatalf("Expected Empty, got %T", deserialized)
	}
}

func TestDeserializeInvalidType(t *testing.T) {
	invalidData := []byte{99, 0, 0, 0}
	_, err := DeserializeNode(invalidData, 0)
	if err == nil {
		t.Fatal("Expected error for invalid type byte")
	}
}

func TestDeserializeInvalidLength(t *testing.T) {
	invalidData := []byte{nodeTypeInternal, 0, 0}
	_, err := DeserializeNode(invalidData, 0)
	if err == nil {
		t.Fatal("Expected error for invalid data length")
	}
	if err.Error() != "invalid serialized node length" {
		t.Errorf("Expected 'invalid serialized node length', got: %v", err)
	}
}

func TestKeyToPath(t *testing.T) {
	tests := []struct {
		name     string
		depth    int
		key      []byte
		expected []byte
		wantErr  bool
	}{
		{
			name:     "depth 0",
			depth:    0,
			key:      []byte{0x80},
			expected: []byte{1},
			wantErr:  false,
		},
		{
			name:     "depth 7",
			depth:    7,
			key:      []byte{0xFF},
			expected: []byte{1, 1, 1, 1, 1, 1, 1, 1},
			wantErr:  false,
		},
		{
			name:     "depth crossing byte boundary",
			depth:    10,
			key:      []byte{0xFF, 0x00},
			expected: []byte{1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0},
			wantErr:  false,
		},
		{
			name:     "max valid depth",
			depth:    StemSize * 8,
			key:      make([]byte, HashSize),
			expected: make([]byte, StemSize*8+1),
			wantErr:  false,
		},
		{
			name:    "depth too large",
			depth:   StemSize*8 + 1,
			key:     make([]byte, HashSize),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := keyToPath(tt.depth, tt.key)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error")
				}
				return
			}
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}
			if !bytes.Equal(path, tt.expected) {
				t.Errorf("Path mismatch: expected %v, got %v", tt.expected, path)
			}
		})
	}
}

func TestEmptyNodeOperations(t *testing.T) {
	e := Empty{}

	// Get returns nil
	val, err := e.Get(nil, nil)
	if err != nil || val != nil {
		t.Fatalf("Empty.Get should return nil, nil; got %v, %v", val, err)
	}

	// Copy returns empty
	cp := e.Copy()
	if _, ok := cp.(Empty); !ok {
		t.Fatalf("Empty.Copy should return Empty, got %T", cp)
	}

	// Hash returns zero
	if e.Hash() != (types.Hash{}) {
		t.Fatal("Empty hash should be zero")
	}

	// GetHeight returns 0
	if e.GetHeight() != 0 {
		t.Fatal("Empty height should be 0")
	}
}

func TestHashedNodeHash(t *testing.T) {
	h := types.HexToHash("abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789")
	hn := HashedNode(h)
	if hn.Hash() != h {
		t.Fatalf("HashedNode hash mismatch: %x vs %x", hn.Hash(), h)
	}
}

func TestHashedNodeCopy(t *testing.T) {
	h := types.HexToHash("abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789")
	hn := HashedNode(h)
	cp := hn.Copy()
	if cpHn, ok := cp.(HashedNode); !ok {
		t.Fatalf("HashedNode.Copy should return HashedNode, got %T", cp)
	} else if types.Hash(cpHn) != h {
		t.Fatal("copy hash mismatch")
	}
}

func TestStemNodeCopy(t *testing.T) {
	stem := make([]byte, StemSize)
	stem[0] = 0xAA
	var values [StemNodeWidth][]byte
	values[5] = oneKey[:]

	sn := &StemNode{
		Stem:   stem,
		Values: values[:],
		depth:  3,
	}

	cp := sn.Copy()
	cpSn := cp.(*StemNode)
	if !bytes.Equal(cpSn.Stem, sn.Stem) {
		t.Fatal("stem mismatch")
	}
	if !bytes.Equal(cpSn.Values[5], sn.Values[5]) {
		t.Fatal("value mismatch")
	}

	// Modify copy, original should be unchanged
	cpSn.Stem[0] = 0xBB
	if sn.Stem[0] != 0xAA {
		t.Fatal("copy should be independent")
	}
}

func TestInternalNodeCopy(t *testing.T) {
	n := &InternalNode{
		depth: 2,
		left:  HashedNode(oneKey),
		right: HashedNode(twoKey),
	}

	cp := n.Copy()
	cpN := cp.(*InternalNode)
	if cpN.depth != 2 {
		t.Fatalf("depth mismatch: %d", cpN.depth)
	}
	if cpN.left.Hash() != oneKey {
		t.Fatal("left hash mismatch")
	}
	if cpN.right.Hash() != twoKey {
		t.Fatal("right hash mismatch")
	}
}

func TestStemNodeKey(t *testing.T) {
	stem := make([]byte, StemSize)
	stem[0] = 0x42
	var values [StemNodeWidth][]byte
	sn := &StemNode{Stem: stem, Values: values[:]}

	key := sn.Key(100)
	if len(key) != HashSize {
		t.Fatalf("key length should be %d, got %d", HashSize, len(key))
	}
	if key[0] != 0x42 {
		t.Fatalf("first byte should be 0x42, got 0x%02x", key[0])
	}
	if key[StemSize] != 100 {
		t.Fatalf("leaf index should be 100, got %d", key[StemSize])
	}
}
