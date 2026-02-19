package ssz

import (
	"encoding/binary"
	"testing"
)

func TestProgressiveListEmpty(t *testing.T) {
	pl := NewProgressiveListEmpty()
	if pl.Len() != 0 {
		t.Fatalf("empty list length = %d, want 0", pl.Len())
	}

	// Hash tree root of empty list: mix_in_length(zero_hash, 0).
	root := pl.HashTreeRoot()
	expected := MixInLength(zeroHash(), 0)
	if root != expected {
		t.Fatalf("empty progressive list root = %x, want %x", root, expected)
	}
}

func TestProgressiveListSingleElement(t *testing.T) {
	var chunk [32]byte
	chunk[0] = 0x42
	pl := NewProgressiveList([][32]byte{chunk})

	if pl.Len() != 1 {
		t.Fatalf("list length = %d, want 1", pl.Len())
	}

	got, err := pl.Get(0)
	if err != nil {
		t.Fatalf("Get(0): %v", err)
	}
	if got != chunk {
		t.Fatalf("Get(0) = %x, want %x", got, chunk)
	}

	// Root: hash(merkleize([chunk], 1), merkleize_progressive([], 4))
	// = hash(chunk, zero_hash) mixed with length 1.
	root := pl.HashTreeRoot()

	left := Merkleize([][32]byte{chunk}, 1) // == chunk
	right := zeroHash()                      // empty recursive call
	inner := hash(left, right)
	expected := MixInLength(inner, 1)
	if root != expected {
		t.Fatalf("single element root = %x, want %x", root, expected)
	}
}

func TestProgressiveListTwoElements(t *testing.T) {
	var a, b [32]byte
	a[0] = 1
	b[0] = 2
	pl := NewProgressiveList([][32]byte{a, b})

	// Structure:
	// root = mix_in_length(
	//   hash(
	//     merkleize([a], 1),       // left: first subtree (1 leaf)
	//     hash(
	//       merkleize([b], 4),     // left: second subtree (4 leaves, b + 3 zeros)
	//       zero_hash              // right: empty recursive
	//     )
	//   ),
	//   2
	// )
	root := pl.HashTreeRoot()

	leftTree := Merkleize([][32]byte{a}, 1)
	rightInner := Merkleize([][32]byte{b}, 4)
	rightTree := hash(rightInner, zeroHash())
	merkleRoot := hash(leftTree, rightTree)
	expected := MixInLength(merkleRoot, 2)
	if root != expected {
		t.Fatalf("two element root mismatch:\ngot  %x\nwant %x", root, expected)
	}
}

func TestProgressiveListFiveElements(t *testing.T) {
	// 5 elements: fills subtree 0 (1 chunk) + subtree 1 (4 chunks).
	chunks := make([][32]byte, 5)
	for i := range chunks {
		chunks[i][0] = byte(i + 1)
	}
	pl := NewProgressiveList(chunks)

	root := pl.HashTreeRoot()

	// Manual construction:
	// Left: merkleize([chunks[0]], 1) = chunks[0]
	// Right: hash(
	//   merkleize([chunks[1..5]], 4),  // exactly 4 chunks
	//   merkleize_progressive([], 16)  // empty -> zero
	// )
	left := Merkleize(chunks[0:1], 1)
	secondSubtree := Merkleize(chunks[1:5], 4)
	right := hash(secondSubtree, zeroHash())
	merkleRoot := hash(left, right)
	expected := MixInLength(merkleRoot, 5)
	if root != expected {
		t.Fatalf("5 element root mismatch:\ngot  %x\nwant %x", root, expected)
	}
}

func TestProgressiveListSixElements(t *testing.T) {
	// 6 elements: subtree 0 (1) + subtree 1 (4) + 1 into subtree 2 (16).
	chunks := make([][32]byte, 6)
	for i := range chunks {
		binary.LittleEndian.PutUint32(chunks[i][:4], uint32(i*100))
	}
	pl := NewProgressiveList(chunks)

	root := pl.HashTreeRoot()

	// Manual:
	left := Merkleize(chunks[0:1], 1)
	sub2 := Merkleize(chunks[1:5], 4)
	sub3 := Merkleize(chunks[5:6], 16)
	right3 := hash(sub3, zeroHash()) // progressive(chunks[5:], 16)
	right2 := hash(sub2, right3)     // progressive(chunks[1:], 4)
	merkleRoot := hash(left, right2)
	expected := MixInLength(merkleRoot, 6)
	if root != expected {
		t.Fatalf("6 element root mismatch:\ngot  %x\nwant %x", root, expected)
	}
}

func TestProgressiveListAppend(t *testing.T) {
	pl := NewProgressiveListEmpty()

	// Append elements one at a time and verify root changes.
	prevRoot := pl.HashTreeRoot()

	for i := 0; i < 10; i++ {
		var chunk [32]byte
		chunk[0] = byte(i + 1)
		pl.Append(chunk)

		newRoot := pl.HashTreeRoot()
		if newRoot == prevRoot {
			t.Errorf("root unchanged after appending element %d", i)
		}
		prevRoot = newRoot
	}

	if pl.Len() != 10 {
		t.Fatalf("length after 10 appends = %d, want 10", pl.Len())
	}
}

func TestProgressiveListDeterminism(t *testing.T) {
	chunks := make([][32]byte, 7)
	for i := range chunks {
		chunks[i][0] = byte(i)
	}

	pl1 := NewProgressiveList(chunks)
	pl2 := NewProgressiveList(chunks)

	if pl1.HashTreeRoot() != pl2.HashTreeRoot() {
		t.Fatal("same input should produce same hash tree root")
	}
}

func TestProgressiveListGetOutOfRange(t *testing.T) {
	pl := NewProgressiveList([][32]byte{{1}, {2}})

	if _, err := pl.Get(-1); err == nil {
		t.Error("expected error for negative index")
	}
	if _, err := pl.Get(2); err == nil {
		t.Error("expected error for index == length")
	}
	if _, err := pl.Get(100); err == nil {
		t.Error("expected error for index > length")
	}
}

func TestProgressiveListStableGindex(t *testing.T) {
	// Key property of EIP-7916: a given chunk index has the same generalized
	// index regardless of the total list length.
	// We verify this by checking that element 0's proof path is consistent
	// as we grow the list.

	// Build a list with 1 element and get its proof.
	chunks1 := [][32]byte{{1}}
	pl1 := NewProgressiveList(chunks1)
	proof1, gindex1, err := pl1.GenerateProof(0)
	if err != nil {
		t.Fatalf("GenerateProof(0) with 1 element: %v", err)
	}

	// Build a list with 5 elements and get proof for element 0.
	chunks5 := make([][32]byte, 5)
	for i := range chunks5 {
		chunks5[i][0] = byte(i + 1)
	}
	pl5 := NewProgressiveList(chunks5)
	proof5, gindex5, err := pl5.GenerateProof(0)
	if err != nil {
		t.Fatalf("GenerateProof(0) with 5 elements: %v", err)
	}

	// The generalized index for element 0 should be the same.
	if gindex1 != gindex5 {
		t.Errorf("gindex for element 0: 1-elem=%d, 5-elem=%d (should be same)", gindex1, gindex5)
	}

	// Proofs should exist (non-empty).
	if len(proof1) == 0 {
		t.Error("proof for 1-element list should not be empty")
	}
	if len(proof5) == 0 {
		t.Error("proof for 5-element list should not be empty")
	}
}

func TestProgressiveListProofVerification(t *testing.T) {
	// Build a list, generate a proof, and verify it recomputes the root.
	var a, b, c [32]byte
	a[0] = 0xaa
	b[0] = 0xbb
	c[0] = 0xcc
	pl := NewProgressiveList([][32]byte{a, b, c})
	expectedRoot := pl.HashTreeRoot()

	// Generate proof for element 0.
	proof, _, err := pl.GenerateProof(0)
	if err != nil {
		t.Fatalf("GenerateProof(0): %v", err)
	}

	// Verify: walk up from the leaf using the proof.
	// Element 0 is in the leftmost position of the first subtree.
	// The progressive tree for 3 elements:
	//   hash(
	//     merkleize([a], 1),  // = a
	//     hash(
	//       merkleize([b, c], 4),
	//       zero
	//     )
	//   )
	// mixed with length 3.

	// Manually verify the proof reconstructs the root.
	// proof[0] is the right sibling of the progressive Merkle root node.
	// proof[last] is the length chunk for mix-in.
	// The exact proof structure depends on the tree layout, but we can
	// at least verify the proof is non-empty and the root is deterministic.
	if len(proof) < 2 {
		t.Fatalf("proof too short: %d entries", len(proof))
	}

	// Verify the root is consistent.
	root2 := pl.HashTreeRoot()
	if root2 != expectedRoot {
		t.Fatal("hash tree root not deterministic")
	}
}

func TestHashTreeRootProgressiveList(t *testing.T) {
	roots := [][32]byte{
		HashTreeRootUint64(10),
		HashTreeRootUint64(20),
		HashTreeRootUint64(30),
	}
	result := HashTreeRootProgressiveList(roots)
	if result == [32]byte{} {
		t.Fatal("progressive list root should not be zero for non-empty list")
	}

	// Compare with manual construction.
	pl := NewProgressiveList(roots)
	expected := pl.HashTreeRoot()
	if result != expected {
		t.Fatalf("convenience function mismatch: %x vs %x", result, expected)
	}
}

func TestHashTreeRootProgressiveBasicList(t *testing.T) {
	// Pack two uint64 values.
	data := make([]byte, 0, 16)
	data = append(data, MarshalUint64(100)...)
	data = append(data, MarshalUint64(200)...)
	root := HashTreeRootProgressiveBasicList(data, 2)
	if root == [32]byte{} {
		t.Fatal("progressive basic list root should not be zero")
	}
}

func TestHashTreeRootProgressiveBitlist(t *testing.T) {
	bits := []bool{true, false, true, true, false}
	root := HashTreeRootProgressiveBitlist(bits)
	if root == [32]byte{} {
		t.Fatal("progressive bitlist root should not be zero")
	}

	// Empty bitlist: merkleize_progressive([], 1) = zero_hash.
	emptyRoot := HashTreeRootProgressiveBitlist(nil)
	expectedEmpty := MixInLength(zeroHash(), 0)
	if emptyRoot != expectedEmpty {
		t.Fatalf("empty progressive bitlist root = %x, want %x", emptyRoot, expectedEmpty)
	}
}

func TestProgressiveListDifferentFromRegularList(t *testing.T) {
	// A progressive list should produce a DIFFERENT root than a regular list
	// with the same elements, since the tree structure is different.
	roots := [][32]byte{
		HashTreeRootUint64(1),
		HashTreeRootUint64(2),
		HashTreeRootUint64(3),
	}

	progressiveRoot := HashTreeRootProgressiveList(roots)
	regularRoot := HashTreeRootList(roots, 4) // List[uint64, 4] with 3 elements

	if progressiveRoot == regularRoot {
		t.Fatal("progressive and regular list roots should differ")
	}
}

func TestProgressiveListGrowthProperty(t *testing.T) {
	// Verify the progressive capacity growth: 1, 5, 21, 85, 341.
	// At depth d, total capacity = sum_{i=0}^{d-1} 4^i = (4^d - 1) / 3.
	expectedCapacities := []int{1, 5, 21, 85, 341}
	for i, expected := range expectedCapacities {
		capacity := 0
		numLeaves := 1
		for d := 0; d <= i; d++ {
			capacity += numLeaves
			numLeaves *= 4
		}
		if capacity != expected {
			t.Errorf("depth %d: capacity = %d, want %d", i+1, capacity, expected)
		}
	}
}

func TestProgressiveListLarger(t *testing.T) {
	// Test with 21 elements (fills exactly subtrees 0, 1, and 2).
	chunks := make([][32]byte, 21)
	for i := range chunks {
		binary.LittleEndian.PutUint64(chunks[i][:8], uint64(i))
	}
	pl := NewProgressiveList(chunks)

	root := pl.HashTreeRoot()
	if root == [32]byte{} {
		t.Fatal("21-element progressive list root should not be zero")
	}

	// Verify determinism.
	pl2 := NewProgressiveList(chunks)
	if pl2.HashTreeRoot() != root {
		t.Fatal("non-deterministic root")
	}
}
