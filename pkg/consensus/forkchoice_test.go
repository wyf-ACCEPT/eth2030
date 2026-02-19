package consensus

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// helper: deterministic test hashes.
func testHash(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	return h
}

func TestForkChoiceStore_AddBlock(t *testing.T) {
	fc := NewForkChoiceStore(ForkChoiceConfig{})

	genesis := testHash(0x01)
	parent := types.Hash{} // genesis has no parent in tree

	if err := fc.AddBlock(genesis, parent, 0); err != nil {
		t.Fatalf("failed to add genesis: %v", err)
	}

	if !fc.HasBlock(genesis) {
		t.Fatal("expected genesis to be in store")
	}
	if fc.BlockCount() != 1 {
		t.Fatalf("expected 1 block, got %d", fc.BlockCount())
	}
}

func TestForkChoiceStore_AddBlock_Duplicate(t *testing.T) {
	fc := NewForkChoiceStore(ForkChoiceConfig{})

	genesis := testHash(0x01)
	if err := fc.AddBlock(genesis, types.Hash{}, 0); err != nil {
		t.Fatal(err)
	}

	err := fc.AddBlock(genesis, types.Hash{}, 0)
	if err != ErrDuplicateBlock {
		t.Fatalf("expected ErrDuplicateBlock, got %v", err)
	}
}

func TestForkChoiceStore_AddBlock_UnknownParent(t *testing.T) {
	fc := NewForkChoiceStore(ForkChoiceConfig{})

	genesis := testHash(0x01)
	if err := fc.AddBlock(genesis, types.Hash{}, 0); err != nil {
		t.Fatal(err)
	}

	// Try to add a block with an unknown parent.
	orphan := testHash(0x99)
	unknownParent := testHash(0xaa)
	err := fc.AddBlock(orphan, unknownParent, 1)
	if err != ErrUnknownParent {
		t.Fatalf("expected ErrUnknownParent, got %v", err)
	}
}

func TestForkChoiceStore_GetHead_SingleChain(t *testing.T) {
	fc := NewForkChoiceStore(ForkChoiceConfig{})

	// Linear chain: A -> B -> C
	a := testHash(0x01)
	b := testHash(0x02)
	c := testHash(0x03)

	fc.AddBlock(a, types.Hash{}, 0)
	fc.AddBlock(b, a, 1)
	fc.AddBlock(c, b, 2)

	head := fc.GetHead()
	if head != c {
		t.Fatalf("expected head=%s, got %s", c.Hex(), head.Hex())
	}
}

func TestForkChoiceStore_GetHead_Fork(t *testing.T) {
	fc := NewForkChoiceStore(ForkChoiceConfig{})

	// Fork:
	//   A -> B -> D
	//   A -> C -> E
	a := testHash(0x01)
	b := testHash(0x02)
	c := testHash(0x03)
	d := testHash(0x04)
	e := testHash(0x05)

	fc.AddBlock(a, types.Hash{}, 0)
	fc.AddBlock(b, a, 1)
	fc.AddBlock(c, a, 1)
	fc.AddBlock(d, b, 2)
	fc.AddBlock(e, c, 2)

	// Add more weight to the C->E branch.
	fc.AddAttestation(e, 10)
	fc.AddAttestation(d, 5)

	head := fc.GetHead()
	if head != e {
		t.Fatalf("expected head=%s (heavier branch), got %s", e.Hex(), head.Hex())
	}
}

func TestForkChoiceStore_GetHead_ForkWeightFlip(t *testing.T) {
	fc := NewForkChoiceStore(ForkChoiceConfig{})

	// Fork: A -> B, A -> C
	a := testHash(0x01)
	b := testHash(0x02)
	c := testHash(0x03)

	fc.AddBlock(a, types.Hash{}, 0)
	fc.AddBlock(b, a, 1)
	fc.AddBlock(c, a, 1)

	// Initially B is heavier.
	fc.AddAttestation(b, 10)
	fc.AddAttestation(c, 5)

	head := fc.GetHead()
	if head != b {
		t.Fatalf("expected head=%s, got %s", b.Hex(), head.Hex())
	}

	// Flip: add more weight to C.
	fc.AddAttestation(c, 10)

	head = fc.GetHead()
	if head != c {
		t.Fatalf("expected head=%s after weight flip, got %s", c.Hex(), head.Hex())
	}
}

func TestForkChoiceStore_GetHead_SubtreeWeight(t *testing.T) {
	fc := NewForkChoiceStore(ForkChoiceConfig{})

	// Tree:
	//   A -> B -> D (weight 3)
	//        B -> E (weight 4)
	//   A -> C (weight 5)
	// Subtree B = 0 + 3 + 4 = 7, subtree C = 5
	// So head should be in B's subtree.
	a := testHash(0x01)
	b := testHash(0x02)
	c := testHash(0x03)
	d := testHash(0x04)
	e := testHash(0x05)

	fc.AddBlock(a, types.Hash{}, 0)
	fc.AddBlock(b, a, 1)
	fc.AddBlock(c, a, 1)
	fc.AddBlock(d, b, 2)
	fc.AddBlock(e, b, 2)

	fc.AddAttestation(d, 3)
	fc.AddAttestation(e, 4)
	fc.AddAttestation(c, 5)

	head := fc.GetHead()
	// B subtree weight = 7, C subtree weight = 5. Head should be E (heaviest leaf in B subtree).
	if head != e {
		t.Fatalf("expected head=%s, got %s", e.Hex(), head.Hex())
	}
}

func TestForkChoiceStore_GetHead_JustifiedRoot(t *testing.T) {
	fc := NewForkChoiceStore(ForkChoiceConfig{})

	// Chain: A -> B -> C
	a := testHash(0x01)
	b := testHash(0x02)
	c := testHash(0x03)

	fc.AddBlock(a, types.Hash{}, 0)
	fc.AddBlock(b, a, 1)
	fc.AddBlock(c, b, 2)

	// Set justified root to B; head computation should start from B.
	fc.SetJustified(1, b)

	head := fc.GetHead()
	if head != c {
		t.Fatalf("expected head=%s starting from justified root B, got %s", c.Hex(), head.Hex())
	}
}

func TestForkChoiceStore_GetHead_Empty(t *testing.T) {
	fc := NewForkChoiceStore(ForkChoiceConfig{})

	head := fc.GetHead()
	if head != (types.Hash{}) {
		t.Fatalf("expected zero hash for empty store, got %s", head.Hex())
	}
}

func TestForkChoiceStore_SetJustifiedAndFinalized(t *testing.T) {
	fc := NewForkChoiceStore(ForkChoiceConfig{})

	root := testHash(0xaa)

	fc.SetJustified(5, root)
	if fc.GetJustifiedRoot() != root {
		t.Fatalf("expected justified root=%s, got %s", root.Hex(), fc.GetJustifiedRoot().Hex())
	}

	fc.SetFinalized(3, root)
	if fc.GetFinalizedRoot() != root {
		t.Fatalf("expected finalized root=%s, got %s", root.Hex(), fc.GetFinalizedRoot().Hex())
	}
}

func TestForkChoiceStore_Prune(t *testing.T) {
	fc := NewForkChoiceStore(ForkChoiceConfig{})

	// Tree:
	//   A -> B -> D
	//   A -> C -> E
	a := testHash(0x01)
	b := testHash(0x02)
	c := testHash(0x03)
	d := testHash(0x04)
	e := testHash(0x05)

	fc.AddBlock(a, types.Hash{}, 0)
	fc.AddBlock(b, a, 1)
	fc.AddBlock(c, a, 1)
	fc.AddBlock(d, b, 2)
	fc.AddBlock(e, c, 2)

	if fc.BlockCount() != 5 {
		t.Fatalf("expected 5 blocks before prune, got %d", fc.BlockCount())
	}

	// Prune to B as finalized root. Should keep B and D, remove A, C, E.
	fc.Prune(b)

	if fc.BlockCount() != 2 {
		t.Fatalf("expected 2 blocks after prune, got %d", fc.BlockCount())
	}
	if !fc.HasBlock(b) {
		t.Fatal("expected B to survive prune")
	}
	if !fc.HasBlock(d) {
		t.Fatal("expected D to survive prune")
	}
	if fc.HasBlock(a) {
		t.Fatal("expected A to be pruned")
	}
	if fc.HasBlock(c) {
		t.Fatal("expected C to be pruned")
	}
	if fc.HasBlock(e) {
		t.Fatal("expected E to be pruned")
	}
}

func TestForkChoiceStore_Prune_NonexistentRoot(t *testing.T) {
	fc := NewForkChoiceStore(ForkChoiceConfig{})

	a := testHash(0x01)
	fc.AddBlock(a, types.Hash{}, 0)

	// Pruning to a nonexistent root should be a no-op.
	fc.Prune(testHash(0xff))

	if fc.BlockCount() != 1 {
		t.Fatalf("expected 1 block after no-op prune, got %d", fc.BlockCount())
	}
}

func TestForkChoiceStore_Prune_EntireTree(t *testing.T) {
	fc := NewForkChoiceStore(ForkChoiceConfig{})

	a := testHash(0x01)
	b := testHash(0x02)
	c := testHash(0x03)

	fc.AddBlock(a, types.Hash{}, 0)
	fc.AddBlock(b, a, 1)
	fc.AddBlock(c, b, 2)

	// Prune to the leaf; only C survives.
	fc.Prune(c)

	if fc.BlockCount() != 1 {
		t.Fatalf("expected 1 block after pruning to leaf, got %d", fc.BlockCount())
	}
	if !fc.HasBlock(c) {
		t.Fatal("expected C to survive")
	}
}

func TestForkChoiceStore_HasBlock(t *testing.T) {
	fc := NewForkChoiceStore(ForkChoiceConfig{})

	a := testHash(0x01)
	fc.AddBlock(a, types.Hash{}, 0)

	if !fc.HasBlock(a) {
		t.Fatal("expected HasBlock to return true")
	}
	if fc.HasBlock(testHash(0xff)) {
		t.Fatal("expected HasBlock to return false for unknown hash")
	}
}

func TestForkChoiceStore_BlockCount(t *testing.T) {
	fc := NewForkChoiceStore(ForkChoiceConfig{})

	if fc.BlockCount() != 0 {
		t.Fatalf("expected 0 blocks, got %d", fc.BlockCount())
	}

	fc.AddBlock(testHash(0x01), types.Hash{}, 0)
	fc.AddBlock(testHash(0x02), testHash(0x01), 1)

	if fc.BlockCount() != 2 {
		t.Fatalf("expected 2 blocks, got %d", fc.BlockCount())
	}
}

func TestForkChoiceStore_AddAttestation_UnknownBlock(t *testing.T) {
	fc := NewForkChoiceStore(ForkChoiceConfig{})

	// Adding attestation to unknown block should be a no-op (not crash).
	fc.AddAttestation(testHash(0xff), 10)

	if fc.BlockCount() != 0 {
		t.Fatal("no blocks should have been created")
	}
}

func TestForkChoiceStore_ConcurrentAccess(t *testing.T) {
	fc := NewForkChoiceStore(ForkChoiceConfig{})

	a := testHash(0x01)
	fc.AddBlock(a, types.Hash{}, 0)

	done := make(chan struct{})

	// Writer goroutine.
	go func() {
		for i := byte(2); i < 50; i++ {
			fc.AddBlock(testHash(i), a, uint64(i))
			fc.AddAttestation(testHash(i), 1)
		}
		close(done)
	}()

	// Reader goroutine.
	for i := 0; i < 100; i++ {
		fc.GetHead()
		fc.HasBlock(a)
		fc.BlockCount()
	}

	<-done
}

func TestForkChoiceStore_GetHead_DeepChain(t *testing.T) {
	fc := NewForkChoiceStore(ForkChoiceConfig{})

	prev := testHash(0x01)
	fc.AddBlock(prev, types.Hash{}, 0)

	var last types.Hash
	for i := byte(2); i <= 20; i++ {
		h := testHash(i)
		fc.AddBlock(h, prev, uint64(i))
		prev = h
		last = h
	}

	head := fc.GetHead()
	if head != last {
		t.Fatalf("expected head=%s, got %s", last.Hex(), head.Hex())
	}
}

func TestForkChoiceStore_GetHead_TieBreaking(t *testing.T) {
	fc := NewForkChoiceStore(ForkChoiceConfig{})

	a := testHash(0x01)
	b := testHash(0x02) // 0x02 < 0x03 lexicographically
	c := testHash(0x03)

	fc.AddBlock(a, types.Hash{}, 0)
	fc.AddBlock(b, a, 1)
	fc.AddBlock(c, a, 1)

	// Equal weight: tie-breaking by hash (lower hash wins).
	fc.AddAttestation(b, 5)
	fc.AddAttestation(c, 5)

	head := fc.GetHead()
	if head != b {
		t.Fatalf("expected head=%s (lower hash tie-break), got %s", b.Hex(), head.Hex())
	}
}

func TestForkChoiceStore_Prune_ThenAddBlocks(t *testing.T) {
	fc := NewForkChoiceStore(ForkChoiceConfig{})

	a := testHash(0x01)
	b := testHash(0x02)
	fc.AddBlock(a, types.Hash{}, 0)
	fc.AddBlock(b, a, 1)

	fc.Prune(b)

	// Should be able to add new children to B.
	c := testHash(0x03)
	if err := fc.AddBlock(c, b, 2); err != nil {
		t.Fatalf("failed to add block after prune: %v", err)
	}

	if fc.BlockCount() != 2 {
		t.Fatalf("expected 2 blocks, got %d", fc.BlockCount())
	}

	head := fc.GetHead()
	if head != c {
		t.Fatalf("expected head=%s, got %s", c.Hex(), head.Hex())
	}
}
