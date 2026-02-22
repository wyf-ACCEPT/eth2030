package verkle

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestGenerateVerkleWitness(t *testing.T) {
	tree := NewInMemoryVerkleTree()

	key := make([]byte, KeySize)
	key[0] = 0x01
	value := make([]byte, ValueSize)
	value[0] = 0xaa

	tree.Put(key, value)

	witness, err := GenerateVerkleWitness(tree, [][]byte{key})
	if err != nil {
		t.Fatalf("GenerateVerkleWitness: %v", err)
	}
	if witness == nil {
		t.Fatal("witness is nil")
	}

	// Witness should start with root commitment (32 bytes).
	if len(witness) < 36 {
		t.Fatalf("witness too short: %d bytes", len(witness))
	}

	// Check number of keys in witness header.
	numKeys := uint32(witness[32])<<24 | uint32(witness[33])<<16 |
		uint32(witness[34])<<8 | uint32(witness[35])
	if numKeys != 1 {
		t.Errorf("numKeys = %d, want 1", numKeys)
	}
}

func TestGenerateVerkleWitnessMultipleKeys(t *testing.T) {
	tree := NewInMemoryVerkleTree()

	keys := make([][]byte, 3)
	for i := range keys {
		key := make([]byte, KeySize)
		key[0] = byte(i + 1)
		keys[i] = key

		val := make([]byte, ValueSize)
		val[0] = byte((i + 1) * 10)
		tree.Put(key, val)
	}

	witness, err := GenerateVerkleWitness(tree, keys)
	if err != nil {
		t.Fatalf("GenerateVerkleWitness: %v", err)
	}

	numKeys := uint32(witness[32])<<24 | uint32(witness[33])<<16 |
		uint32(witness[34])<<8 | uint32(witness[35])
	if numKeys != 3 {
		t.Errorf("numKeys = %d, want 3", numKeys)
	}
}

func TestGenerateVerkleWitnessMissingKey(t *testing.T) {
	tree := NewInMemoryVerkleTree()

	// Key not in tree.
	key := make([]byte, KeySize)
	key[0] = 0xff

	witness, err := GenerateVerkleWitness(tree, [][]byte{key})
	if err != nil {
		t.Fatalf("GenerateVerkleWitness: %v", err)
	}
	// Should still produce a witness (with zero value).
	if witness == nil {
		t.Fatal("witness should not be nil for missing key")
	}
}

func TestVerifyVerkleWitness(t *testing.T) {
	tree := NewInMemoryVerkleTree()

	key := make([]byte, KeySize)
	key[0] = 0x01
	value := make([]byte, ValueSize)
	value[0] = 0xaa

	tree.Put(key, value)

	root, _ := tree.Commit()

	witness, err := GenerateVerkleWitness(tree, [][]byte{key})
	if err != nil {
		t.Fatalf("GenerateVerkleWitness: %v", err)
	}

	if !VerifyVerkleWitness(root, witness, key, value) {
		t.Error("valid witness should verify")
	}
}

func TestVerifyVerkleWitnessWrongRoot(t *testing.T) {
	tree := NewInMemoryVerkleTree()

	key := make([]byte, KeySize)
	key[0] = 0x01
	value := make([]byte, ValueSize)
	value[0] = 0xaa

	tree.Put(key, value)
	tree.Commit()

	witness, _ := GenerateVerkleWitness(tree, [][]byte{key})

	wrongRoot := types.HexToHash("0xdeadbeef")
	if VerifyVerkleWitness(wrongRoot, witness, key, value) {
		t.Error("wrong root should fail verification")
	}
}

func TestVerifyVerkleWitnessShortData(t *testing.T) {
	if VerifyVerkleWitness(types.Hash{}, []byte{0x01, 0x02}, nil, nil) {
		t.Error("short witness data should fail")
	}
}

func TestVerifyVerkleWitnessWrongValue(t *testing.T) {
	tree := NewInMemoryVerkleTree()

	key := make([]byte, KeySize)
	key[0] = 0x01
	value := make([]byte, ValueSize)
	value[0] = 0xaa

	tree.Put(key, value)
	root, _ := tree.Commit()

	witness, _ := GenerateVerkleWitness(tree, [][]byte{key})

	wrongValue := make([]byte, ValueSize)
	wrongValue[0] = 0xbb
	if VerifyVerkleWitness(root, witness, key, wrongValue) {
		t.Error("wrong value should fail verification")
	}
}

func TestVerifyVerkleWitnessKeyNotInWitness(t *testing.T) {
	tree := NewInMemoryVerkleTree()

	key := make([]byte, KeySize)
	key[0] = 0x01
	value := make([]byte, ValueSize)
	value[0] = 0xaa
	tree.Put(key, value)

	root, _ := tree.Commit()
	witness, _ := GenerateVerkleWitness(tree, [][]byte{key})

	// Try verifying a different key.
	otherKey := make([]byte, KeySize)
	otherKey[0] = 0xff
	if VerifyVerkleWitness(root, witness, otherKey, value) {
		t.Error("key not in witness should fail verification")
	}
}

func TestGenerateAndVerifyEmptyKeys(t *testing.T) {
	tree := NewInMemoryVerkleTree()

	witness, err := GenerateVerkleWitness(tree, nil)
	if err != nil {
		t.Fatalf("GenerateVerkleWitness(nil keys): %v", err)
	}

	numKeys := uint32(witness[32])<<24 | uint32(witness[33])<<16 |
		uint32(witness[34])<<8 | uint32(witness[35])
	if numKeys != 0 {
		t.Errorf("numKeys = %d, want 0", numKeys)
	}
}
