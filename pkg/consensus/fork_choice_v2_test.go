package consensus

import (
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// hashFromByte creates a deterministic hash from a single byte value.
func hashFromByte(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	return h
}

func TestForkChoiceV2_OnBlock_Basic(t *testing.T) {
	justified := Checkpoint{Epoch: 0, Root: hashFromByte(1)}
	finalized := Checkpoint{Epoch: 0, Root: hashFromByte(1)}
	fc := NewForkChoiceV2(justified, finalized)

	// Add genesis block.
	root1 := hashFromByte(1)
	err := fc.OnBlock(0, root1, types.Hash{}, 0, 0)
	if err != nil {
		t.Fatalf("OnBlock genesis: %v", err)
	}

	if !fc.HasBlock(root1) {
		t.Fatal("expected block to exist")
	}
	if fc.BlockCount() != 1 {
		t.Fatalf("expected 1 block, got %d", fc.BlockCount())
	}

	// Add child block.
	root2 := hashFromByte(2)
	err = fc.OnBlock(1, root2, root1, 0, 0)
	if err != nil {
		t.Fatalf("OnBlock child: %v", err)
	}

	if fc.BlockCount() != 2 {
		t.Fatalf("expected 2 blocks, got %d", fc.BlockCount())
	}
}

func TestForkChoiceV2_OnBlock_Duplicate(t *testing.T) {
	fc := NewForkChoiceV2(Checkpoint{}, Checkpoint{})
	root := hashFromByte(1)
	if err := fc.OnBlock(0, root, types.Hash{}, 0, 0); err != nil {
		t.Fatalf("first OnBlock: %v", err)
	}
	err := fc.OnBlock(0, root, types.Hash{}, 0, 0)
	if err != ErrV2DuplicateBlock {
		t.Fatalf("expected ErrV2DuplicateBlock, got %v", err)
	}
}

func TestForkChoiceV2_OnBlock_UnknownParent(t *testing.T) {
	fc := NewForkChoiceV2(Checkpoint{}, Checkpoint{})
	// Add a genesis block first.
	if err := fc.OnBlock(0, hashFromByte(1), types.Hash{}, 0, 0); err != nil {
		t.Fatalf("genesis: %v", err)
	}
	// Try adding with unknown parent.
	err := fc.OnBlock(1, hashFromByte(2), hashFromByte(99), 0, 0)
	if err != ErrV2UnknownParent {
		t.Fatalf("expected ErrV2UnknownParent, got %v", err)
	}
}

func TestForkChoiceV2_GetHead_SingleChain(t *testing.T) {
	root1 := hashFromByte(1)
	justified := Checkpoint{Epoch: 0, Root: root1}
	finalized := Checkpoint{Epoch: 0, Root: root1}
	fc := NewForkChoiceV2(justified, finalized)

	if err := fc.OnBlock(0, root1, types.Hash{}, 0, 0); err != nil {
		t.Fatal(err)
	}

	root2 := hashFromByte(2)
	if err := fc.OnBlock(1, root2, root1, 0, 0); err != nil {
		t.Fatal(err)
	}

	root3 := hashFromByte(3)
	if err := fc.OnBlock(2, root3, root2, 0, 0); err != nil {
		t.Fatal(err)
	}

	// Without any attestations, head should be the leaf with highest root in
	// the only chain.
	head, err := fc.GetHead()
	if err != nil {
		t.Fatalf("GetHead: %v", err)
	}

	// With a single chain, head should be the leaf (root3).
	if head != root3 {
		t.Fatalf("expected head %v, got %v", root3, head)
	}
}

func TestForkChoiceV2_GetHead_WithAttestations(t *testing.T) {
	// Create a fork:
	//   root1 -> root2
	//         -> root3
	// Attest to root2 with more weight.
	root1 := hashFromByte(1)
	justified := Checkpoint{Epoch: 0, Root: root1}
	finalized := Checkpoint{Epoch: 0, Root: root1}
	fc := NewForkChoiceV2(justified, finalized)

	if err := fc.OnBlock(0, root1, types.Hash{}, 0, 0); err != nil {
		t.Fatal(err)
	}

	root2 := hashFromByte(2)
	if err := fc.OnBlock(1, root2, root1, 0, 0); err != nil {
		t.Fatal(err)
	}

	root3 := hashFromByte(3)
	if err := fc.OnBlock(1, root3, root1, 0, 0); err != nil {
		t.Fatal(err)
	}

	// Set balances and cast attestations.
	fc.SetBalance(0, 32_000_000_000)
	fc.SetBalance(1, 32_000_000_000)
	fc.SetBalance(2, 32_000_000_000)

	// Two attestations for root2, one for root3.
	fc.OnAttestation(0, root2, 0)
	fc.OnAttestation(1, root2, 0)
	fc.OnAttestation(2, root3, 0)

	head, err := fc.GetHead()
	if err != nil {
		t.Fatalf("GetHead: %v", err)
	}

	if head != root2 {
		t.Fatalf("expected head root2 %v, got %v", root2, head)
	}
}

func TestForkChoiceV2_GetHead_SwitchOnNewAttestation(t *testing.T) {
	root1 := hashFromByte(1)
	fc := NewForkChoiceV2(
		Checkpoint{Epoch: 0, Root: root1},
		Checkpoint{Epoch: 0, Root: root1},
	)

	if err := fc.OnBlock(0, root1, types.Hash{}, 0, 0); err != nil {
		t.Fatal(err)
	}

	root2 := hashFromByte(2)
	root3 := hashFromByte(3)
	if err := fc.OnBlock(1, root2, root1, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := fc.OnBlock(1, root3, root1, 0, 0); err != nil {
		t.Fatal(err)
	}

	fc.SetBalance(0, 10_000_000_000)
	fc.SetBalance(1, 10_000_000_000)
	fc.SetBalance(2, 30_000_000_000)

	// Initially vote for root2.
	fc.OnAttestation(0, root2, 0)
	fc.OnAttestation(1, root2, 0)
	fc.OnAttestation(2, root2, 0)

	head, _ := fc.GetHead()
	if head != root2 {
		t.Fatalf("expected root2, got %v", head)
	}

	// Switch validator 2 (heaviest) to root3 with a newer epoch.
	fc.OnAttestation(2, root3, 1)

	head, _ = fc.GetHead()
	if head != root3 {
		t.Fatalf("expected root3 after switch, got %v", head)
	}
}

func TestForkChoiceV2_Prune(t *testing.T) {
	root1 := hashFromByte(1)
	fc := NewForkChoiceV2(
		Checkpoint{Epoch: 0, Root: root1},
		Checkpoint{Epoch: 0, Root: root1},
	)

	if err := fc.OnBlock(0, root1, types.Hash{}, 0, 0); err != nil {
		t.Fatal(err)
	}

	root2 := hashFromByte(2)
	if err := fc.OnBlock(32, root2, root1, 1, 0); err != nil {
		t.Fatal(err)
	}

	root3 := hashFromByte(3)
	if err := fc.OnBlock(64, root3, root2, 2, 1); err != nil {
		t.Fatal(err)
	}

	if fc.BlockCount() != 3 {
		t.Fatalf("expected 3 blocks before prune, got %d", fc.BlockCount())
	}

	// Prune to root2 as finalized.
	pruned := fc.Prune(root2)
	if pruned != 1 {
		t.Fatalf("expected 1 pruned, got %d", pruned)
	}

	if fc.BlockCount() != 2 {
		t.Fatalf("expected 2 blocks after prune, got %d", fc.BlockCount())
	}

	if fc.HasBlock(root1) {
		t.Fatal("root1 should be pruned")
	}
	if !fc.HasBlock(root2) {
		t.Fatal("root2 should still exist")
	}
	if !fc.HasBlock(root3) {
		t.Fatal("root3 should still exist")
	}
}

func TestForkChoiceV2_CheckpointUpdates(t *testing.T) {
	fc := NewForkChoiceV2(
		Checkpoint{Epoch: 0, Root: types.Hash{}},
		Checkpoint{Epoch: 0, Root: types.Hash{}},
	)

	cp1 := Checkpoint{Epoch: 1, Root: hashFromByte(1)}
	fc.UpdateJustifiedCheckpoint(cp1)
	if got := fc.GetJustifiedCheckpoint(); got.Epoch != 1 {
		t.Fatalf("expected justified epoch 1, got %d", got.Epoch)
	}

	// Lower epoch should not update.
	cp0 := Checkpoint{Epoch: 0, Root: hashFromByte(2)}
	fc.UpdateJustifiedCheckpoint(cp0)
	if got := fc.GetJustifiedCheckpoint(); got.Epoch != 1 {
		t.Fatalf("justified should remain 1, got %d", got.Epoch)
	}

	cp2 := Checkpoint{Epoch: 2, Root: hashFromByte(3)}
	fc.UpdateFinalizedCheckpoint(cp2)
	if got := fc.GetFinalizedCheckpoint(); got.Epoch != 2 {
		t.Fatalf("expected finalized epoch 2, got %d", got.Epoch)
	}
}

func TestForkChoiceV2_OnAttestation_OlderEpochIgnored(t *testing.T) {
	root1 := hashFromByte(1)
	fc := NewForkChoiceV2(
		Checkpoint{Epoch: 0, Root: root1},
		Checkpoint{Epoch: 0, Root: root1},
	)

	if err := fc.OnBlock(0, root1, types.Hash{}, 0, 0); err != nil {
		t.Fatal(err)
	}

	root2 := hashFromByte(2)
	root3 := hashFromByte(3)
	if err := fc.OnBlock(1, root2, root1, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := fc.OnBlock(1, root3, root1, 0, 0); err != nil {
		t.Fatal(err)
	}

	fc.SetBalance(0, 32_000_000_000)

	// Vote for root3 at epoch 5.
	fc.OnAttestation(0, root3, 5)

	// Attempt to vote for root2 at epoch 3 (older, should be ignored).
	fc.OnAttestation(0, root2, 3)

	head, _ := fc.GetHead()
	if head != root3 {
		t.Fatalf("expected root3 (newer vote), got %v", head)
	}
}

func TestForkChoiceV2_ThreadSafety(t *testing.T) {
	root1 := hashFromByte(1)
	fc := NewForkChoiceV2(
		Checkpoint{Epoch: 0, Root: root1},
		Checkpoint{Epoch: 0, Root: root1},
	)

	if err := fc.OnBlock(0, root1, types.Hash{}, 0, 0); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup

	// Add blocks concurrently.
	for i := byte(2); i < 12; i++ {
		wg.Add(1)
		go func(b byte) {
			defer wg.Done()
			_ = fc.OnBlock(uint64(b), hashFromByte(b), root1, 0, 0)
		}(i)
	}
	wg.Wait()

	// Attest concurrently.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			fc.SetBalance(ValidatorIndex(idx), 32_000_000_000)
			fc.OnAttestation(ValidatorIndex(idx), hashFromByte(byte(idx+2)), 0)
		}(i)
	}
	wg.Wait()

	// GetHead concurrently.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = fc.GetHead()
		}()
	}
	wg.Wait()
}

func TestForkChoiceV2_DeepChain(t *testing.T) {
	root0 := hashFromByte(1)
	fc := NewForkChoiceV2(
		Checkpoint{Epoch: 0, Root: root0},
		Checkpoint{Epoch: 0, Root: root0},
	)

	if err := fc.OnBlock(0, root0, types.Hash{}, 0, 0); err != nil {
		t.Fatal(err)
	}

	prev := root0
	for i := byte(2); i < 100; i++ {
		r := hashFromByte(i)
		if err := fc.OnBlock(uint64(i), r, prev, 0, 0); err != nil {
			t.Fatalf("OnBlock %d: %v", i, err)
		}
		prev = r
	}

	fc.SetBalance(0, 32_000_000_000)
	fc.OnAttestation(0, prev, 0)

	head, err := fc.GetHead()
	if err != nil {
		t.Fatalf("GetHead: %v", err)
	}

	if head != prev {
		t.Fatalf("expected head at tip %v, got %v", prev, head)
	}
}

func TestForkChoiceV2_EmptyStore(t *testing.T) {
	fc := NewForkChoiceV2(Checkpoint{}, Checkpoint{})

	_, err := fc.GetHead()
	if err != ErrV2NoViableHead {
		t.Fatalf("expected ErrV2NoViableHead, got %v", err)
	}
}

func TestForkChoiceV2_PruneNonexistentRoot(t *testing.T) {
	fc := NewForkChoiceV2(Checkpoint{}, Checkpoint{})
	root := hashFromByte(1)
	if err := fc.OnBlock(0, root, types.Hash{}, 0, 0); err != nil {
		t.Fatal(err)
	}

	pruned := fc.Prune(hashFromByte(99))
	if pruned != 0 {
		t.Fatalf("expected 0 pruned for nonexistent root, got %d", pruned)
	}
}

func TestHashGreater(t *testing.T) {
	a := hashFromByte(5)
	b := hashFromByte(3)
	if !hashGreater(a, b) {
		t.Fatal("expected a > b")
	}
	if hashGreater(b, a) {
		t.Fatal("expected b < a")
	}
	if hashGreater(a, a) {
		t.Fatal("expected a == a to return false")
	}
}

func TestForkChoiceV2_GetHead_TieBreaking(t *testing.T) {
	// With equal weight, the block with the higher root hash should win.
	root1 := hashFromByte(1)
	fc := NewForkChoiceV2(
		Checkpoint{Epoch: 0, Root: root1},
		Checkpoint{Epoch: 0, Root: root1},
	)

	if err := fc.OnBlock(0, root1, types.Hash{}, 0, 0); err != nil {
		t.Fatal(err)
	}

	// root_low < root_high lexicographically.
	rootLow := hashFromByte(2)
	rootHigh := hashFromByte(5)

	if err := fc.OnBlock(1, rootLow, root1, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := fc.OnBlock(1, rootHigh, root1, 0, 0); err != nil {
		t.Fatal(err)
	}

	// Equal attestation weight.
	fc.SetBalance(0, 32_000_000_000)
	fc.SetBalance(1, 32_000_000_000)
	fc.OnAttestation(0, rootLow, 0)
	fc.OnAttestation(1, rootHigh, 0)

	head, err := fc.GetHead()
	if err != nil {
		t.Fatalf("GetHead: %v", err)
	}

	// Higher root should win tie.
	if head != rootHigh {
		t.Fatalf("expected rootHigh %v on tie-break, got %v", rootHigh, head)
	}
}

func TestForkChoiceV2_PruneAndContinue(t *testing.T) {
	root1 := hashFromByte(1)
	fc := NewForkChoiceV2(
		Checkpoint{Epoch: 0, Root: root1},
		Checkpoint{Epoch: 0, Root: root1},
	)

	if err := fc.OnBlock(0, root1, types.Hash{}, 0, 0); err != nil {
		t.Fatal(err)
	}
	root2 := hashFromByte(2)
	if err := fc.OnBlock(32, root2, root1, 1, 0); err != nil {
		t.Fatal(err)
	}

	// Prune up to root2.
	fc.Prune(root2)

	// Should be able to continue building on root2.
	root3 := hashFromByte(3)
	if err := fc.OnBlock(64, root3, root2, 1, 0); err != nil {
		t.Fatalf("OnBlock after prune: %v", err)
	}

	fc.UpdateJustifiedCheckpoint(Checkpoint{Epoch: 1, Root: root2})

	fc.SetBalance(0, 32_000_000_000)
	fc.OnAttestation(0, root3, 1)

	head, err := fc.GetHead()
	if err != nil {
		t.Fatalf("GetHead after prune: %v", err)
	}
	if head != root3 {
		t.Fatalf("expected root3 after prune+extend, got %v", head)
	}
}
