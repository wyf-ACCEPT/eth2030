package consensus

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// fcsHash creates a deterministic hash from a single byte for testing.
func fcsHash(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	return h
}

func newTestFCS() *ForkChoiceStoreV3 {
	genesis := fcsHash(0x01)
	return NewForkChoiceStoreV3(ForkChoiceStoreV3Config{
		JustifiedCheckpoint: FCSCheckpoint{Epoch: 0, Root: genesis},
		FinalizedCheckpoint: FCSCheckpoint{Epoch: 0, Root: genesis},
		SlotsPerEpoch:       32,
	})
}

func TestFCSV3OnBlockBasic(t *testing.T) {
	fcs := newTestFCS()
	genesis := fcsHash(0x01)

	err := fcs.OnBlock(genesis, types.Hash{}, types.Hash{}, 0, 0, 0)
	if err != nil {
		t.Fatalf("OnBlock genesis: %v", err)
	}
	if !fcs.HasBlock(genesis) {
		t.Fatal("expected genesis block to exist")
	}
	if fcs.BlockCount() != 1 {
		t.Fatalf("expected 1 block, got %d", fcs.BlockCount())
	}

	child := fcsHash(0x02)
	err = fcs.OnBlock(child, genesis, types.Hash{}, 1, 0, 0)
	if err != nil {
		t.Fatalf("OnBlock child: %v", err)
	}
	if fcs.BlockCount() != 2 {
		t.Fatalf("expected 2 blocks, got %d", fcs.BlockCount())
	}
}

func TestFCSV3OnBlockDuplicate(t *testing.T) {
	fcs := newTestFCS()
	genesis := fcsHash(0x01)
	fcs.OnBlock(genesis, types.Hash{}, types.Hash{}, 0, 0, 0)

	err := fcs.OnBlock(genesis, types.Hash{}, types.Hash{}, 0, 0, 0)
	if err != ErrFCSDuplicateBlock {
		t.Fatalf("expected ErrFCSDuplicateBlock, got %v", err)
	}
}

func TestFCSV3OnBlockUnknownParent(t *testing.T) {
	fcs := newTestFCS()
	genesis := fcsHash(0x01)
	fcs.OnBlock(genesis, types.Hash{}, types.Hash{}, 0, 0, 0)

	err := fcs.OnBlock(fcsHash(0x02), fcsHash(0x99), types.Hash{}, 1, 0, 0)
	if err == nil {
		t.Fatal("expected error for unknown parent")
	}
}

func TestFCSV3GetHeadSingleChain(t *testing.T) {
	fcs := newTestFCS()
	genesis := fcsHash(0x01)
	fcs.OnBlock(genesis, types.Hash{}, types.Hash{}, 0, 0, 0)

	b2 := fcsHash(0x02)
	fcs.OnBlock(b2, genesis, types.Hash{}, 1, 0, 0)
	b3 := fcsHash(0x03)
	fcs.OnBlock(b3, b2, types.Hash{}, 2, 0, 0)

	head, err := fcs.GetHead()
	if err != nil {
		t.Fatalf("GetHead: %v", err)
	}
	if head != b3 {
		t.Fatalf("expected head %v, got %v", b3, head)
	}
}

func TestFCSV3ForkHigherWeightWins(t *testing.T) {
	fcs := newTestFCS()
	genesis := fcsHash(0x01)
	fcs.OnBlock(genesis, types.Hash{}, types.Hash{}, 0, 0, 0)

	forkA := fcsHash(0x02)
	forkB := fcsHash(0x03)
	fcs.OnBlock(forkA, genesis, types.Hash{}, 1, 0, 0)
	fcs.OnBlock(forkB, genesis, types.Hash{}, 1, 0, 0)

	fcs.OnAttestation(0, forkA, 1, 32_000_000_000)
	fcs.OnAttestation(1, forkA, 1, 32_000_000_000)
	fcs.OnAttestation(2, forkB, 1, 32_000_000_000)

	head, err := fcs.GetHead()
	if err != nil {
		t.Fatalf("GetHead: %v", err)
	}
	if head != forkA {
		t.Fatalf("expected forkA (higher weight), got %v", head)
	}
}

func TestFCSV3AttestationUpdatesLatestMessage(t *testing.T) {
	fcs := newTestFCS()
	genesis := fcsHash(0x01)
	fcs.OnBlock(genesis, types.Hash{}, types.Hash{}, 0, 0, 0)
	child := fcsHash(0x02)
	fcs.OnBlock(child, genesis, types.Hash{}, 1, 0, 0)

	fcs.OnAttestation(5, child, 1, 32_000_000_000)

	msg := fcs.GetLatestMessage(5)
	if msg == nil {
		t.Fatal("expected latest message, got nil")
	}
	if msg.TargetRoot != child {
		t.Fatalf("expected target root %v, got %v", child, msg.TargetRoot)
	}
	if msg.TargetEpoch != 1 {
		t.Fatalf("expected epoch 1, got %d", msg.TargetEpoch)
	}
	// Non-existent validator message.
	if fcs.GetLatestMessage(999) != nil {
		t.Fatal("expected nil for non-existent validator")
	}
}

func TestFCSV3StaleAttestationRejected(t *testing.T) {
	fcs := newTestFCS()
	genesis := fcsHash(0x01)
	fcs.OnBlock(genesis, types.Hash{}, types.Hash{}, 0, 0, 0)
	child := fcsHash(0x02)
	fcs.OnBlock(child, genesis, types.Hash{}, 1, 0, 0)

	fcs.OnAttestation(0, child, 5, 32_000_000_000)
	err := fcs.OnAttestation(0, child, 3, 32_000_000_000)
	if err != ErrFCSStaleAttestation {
		t.Fatalf("expected ErrFCSStaleAttestation, got %v", err)
	}
}

func TestFCSV3AttestationUnknownBlock(t *testing.T) {
	fcs := newTestFCS()
	genesis := fcsHash(0x01)
	fcs.OnBlock(genesis, types.Hash{}, types.Hash{}, 0, 0, 0)

	err := fcs.OnAttestation(0, fcsHash(0x99), 1, 32_000_000_000)
	if err == nil {
		t.Fatal("expected error for attestation to unknown block")
	}
}

func TestFCSV3GetHeadEmptyStore(t *testing.T) {
	fcs := NewForkChoiceStoreV3(ForkChoiceStoreV3Config{})
	_, err := fcs.GetHead()
	if err != ErrFCSEmptyStore {
		t.Fatalf("expected ErrFCSEmptyStore, got %v", err)
	}
}

func TestFCSV3PruneBeforeFinalized(t *testing.T) {
	fcs := newTestFCS()
	genesis := fcsHash(0x01)
	fcs.OnBlock(genesis, types.Hash{}, types.Hash{}, 0, 0, 0)
	b2 := fcsHash(0x02)
	fcs.OnBlock(b2, genesis, types.Hash{}, 32, 1, 0)
	b3 := fcsHash(0x03)
	fcs.OnBlock(b3, b2, types.Hash{}, 64, 2, 1)
	orphan := fcsHash(0x0A)
	fcs.OnBlock(orphan, genesis, types.Hash{}, 33, 0, 0)

	fcs.SetFinalizedCheckpoint(FCSCheckpoint{Epoch: 1, Root: b2})
	pruned := fcs.PruneBeforeFinalized()
	if pruned < 1 {
		t.Fatalf("expected at least 1 pruned, got %d", pruned)
	}
	if fcs.HasBlock(genesis) {
		t.Fatal("genesis should be pruned")
	}
	if !fcs.HasBlock(b2) {
		t.Fatal("b2 should remain")
	}
	if !fcs.HasBlock(b3) {
		t.Fatal("b3 should remain")
	}
}

func TestFCSV3LMDGHOSTTieBreaking(t *testing.T) {
	fcs := newTestFCS()
	genesis := fcsHash(0x01)
	fcs.OnBlock(genesis, types.Hash{}, types.Hash{}, 0, 0, 0)

	forkLow := fcsHash(0x02)
	forkHigh := fcsHash(0x10)
	fcs.OnBlock(forkLow, genesis, types.Hash{}, 1, 0, 0)
	fcs.OnBlock(forkHigh, genesis, types.Hash{}, 1, 0, 0)

	fcs.OnAttestation(0, forkLow, 1, 32_000_000_000)
	fcs.OnAttestation(1, forkHigh, 1, 32_000_000_000)

	head, err := fcs.GetHead()
	if err != nil {
		t.Fatalf("GetHead: %v", err)
	}
	// fcsHashLess picks the lower hash on tie.
	if head != forkLow {
		t.Fatalf("expected forkLow %v on tie-break, got %v", forkLow, head)
	}
}

func TestFCSV3ReorgWhenWeightShifts(t *testing.T) {
	fcs := newTestFCS()
	genesis := fcsHash(0x01)
	fcs.OnBlock(genesis, types.Hash{}, types.Hash{}, 0, 0, 0)

	forkA := fcsHash(0x02)
	forkB := fcsHash(0x03)
	fcs.OnBlock(forkA, genesis, types.Hash{}, 1, 0, 0)
	fcs.OnBlock(forkB, genesis, types.Hash{}, 1, 0, 0)

	fcs.OnAttestation(0, forkA, 1, 64_000_000_000)
	fcs.OnAttestation(1, forkB, 1, 32_000_000_000)

	head1, _ := fcs.GetHead()
	if head1 != forkA {
		t.Fatalf("expected forkA initially, got %v", head1)
	}

	fcs.OnAttestation(0, forkB, 2, 64_000_000_000)

	head2, _ := fcs.GetHead()
	if head2 != forkB {
		t.Fatalf("expected forkB after re-org, got %v", head2)
	}
}

func TestFCSV3ConcurrentBlockAndAttestation(t *testing.T) {
	fcs := newTestFCS()
	genesis := fcsHash(0x01)
	fcs.OnBlock(genesis, types.Hash{}, types.Hash{}, 0, 0, 0)

	var wg sync.WaitGroup
	for i := byte(2); i < 12; i++ {
		wg.Add(1)
		go func(b byte) {
			defer wg.Done()
			_ = fcs.OnBlock(fcsHash(b), genesis, types.Hash{}, uint64(b), 0, 0)
		}(i)
	}
	wg.Wait()

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = fcs.OnAttestation(ValidatorIndex(idx), fcsHash(byte(idx+2)), 1, 32_000_000_000)
		}(i)
	}
	wg.Wait()

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = fcs.GetHead()
		}()
	}
	wg.Wait()

	if fcs.BlockCount() != 11 {
		t.Fatalf("expected 11 blocks, got %d", fcs.BlockCount())
	}
}

func TestFCSV3MultipleValidatorsSameBlock(t *testing.T) {
	fcs := newTestFCS()
	genesis := fcsHash(0x01)
	fcs.OnBlock(genesis, types.Hash{}, types.Hash{}, 0, 0, 0)
	target := fcsHash(0x02)
	fcs.OnBlock(target, genesis, types.Hash{}, 1, 0, 0)

	for i := ValidatorIndex(0); i < 5; i++ {
		if err := fcs.OnAttestation(i, target, 1, 32_000_000_000); err != nil {
			t.Fatalf("OnAttestation validator %d: %v", i, err)
		}
	}
	if fcs.MessageCount() != 5 {
		t.Fatalf("expected 5 messages, got %d", fcs.MessageCount())
	}
	head, err := fcs.GetHead()
	if err != nil {
		t.Fatalf("GetHead: %v", err)
	}
	if head != target {
		t.Fatalf("expected head to be target, got %v", head)
	}
}

func TestFCSV3GetBlockAndCheckpoints(t *testing.T) {
	fcs := newTestFCS()
	genesis := fcsHash(0x01)
	var sr types.Hash
	sr[31] = 0x01
	fcs.OnBlock(genesis, types.Hash{}, sr, 0, 0, 0)

	node := fcs.GetBlock(genesis)
	if node == nil {
		t.Fatal("expected block node, got nil")
	}
	if node.Root != genesis || node.Slot != 0 || node.StateRoot != sr {
		t.Fatalf("block node mismatch: root=%v slot=%d", node.Root, node.Slot)
	}
	if fcs.GetBlock(fcsHash(0x99)) != nil {
		t.Fatal("expected nil for unknown block")
	}

	// Checkpoints.
	jp := FCSCheckpoint{Epoch: 5, Root: fcsHash(0x55)}
	fcs.SetJustifiedCheckpoint(jp)
	fp := FCSCheckpoint{Epoch: 4, Root: fcsHash(0x44)}
	fcs.SetFinalizedCheckpoint(fp)

	gotJ := fcs.GetJustifiedCheckpoint()
	if gotJ.Epoch != 5 || gotJ.Root != fcsHash(0x55) {
		t.Fatalf("justified checkpoint mismatch: %+v", gotJ)
	}
	gotF := fcs.GetFinalizedCheckpoint()
	if gotF.Epoch != 4 || gotF.Root != fcsHash(0x44) {
		t.Fatalf("finalized checkpoint mismatch: %+v", gotF)
	}
}

func TestFCSV3HeadCacheInvalidation(t *testing.T) {
	fcs := newTestFCS()
	genesis := fcsHash(0x01)
	fcs.OnBlock(genesis, types.Hash{}, types.Hash{}, 0, 0, 0)

	_, _ = fcs.GetHead()

	child := fcsHash(0x02)
	fcs.OnBlock(child, genesis, types.Hash{}, 1, 0, 0)

	head2, err := fcs.GetHead()
	if err != nil {
		t.Fatalf("GetHead: %v", err)
	}
	if head2 != child {
		t.Fatalf("expected head to be child %v, got %v", child, head2)
	}
}

func TestFCSV3IsDescendantOf(t *testing.T) {
	fcs := newTestFCS()
	genesis := fcsHash(0x01)
	fcs.OnBlock(genesis, types.Hash{}, types.Hash{}, 0, 0, 0)
	b2 := fcsHash(0x02)
	fcs.OnBlock(b2, genesis, types.Hash{}, 1, 0, 0)
	b3 := fcsHash(0x03)
	fcs.OnBlock(b3, b2, types.Hash{}, 2, 0, 0)

	fcs.mu.RLock()
	isDesc := fcs.isDescendantOf(b3, genesis)
	isNotDesc := fcs.isDescendantOf(genesis, b3)
	fcs.mu.RUnlock()

	if !isDesc {
		t.Fatal("expected b3 to be a descendant of genesis")
	}
	if isNotDesc {
		t.Fatal("genesis should not be a descendant of b3")
	}
}

func TestFCSV3ComputeFCSBlockRoot(t *testing.T) {
	parent := fcsHash(0xAA)
	root1 := computeFCSBlockRoot(10, parent)
	root2 := computeFCSBlockRoot(10, parent)
	root3 := computeFCSBlockRoot(11, parent)

	if root1 != root2 {
		t.Fatal("same inputs should produce same root")
	}
	if root1 == root3 {
		t.Fatal("different slots should produce different roots")
	}
}

func TestFCSV3DeepChainHead(t *testing.T) {
	fcs := newTestFCS()
	genesis := fcsHash(0x01)
	fcs.OnBlock(genesis, types.Hash{}, types.Hash{}, 0, 0, 0)

	prev := genesis
	var last types.Hash
	for i := byte(2); i < 50; i++ {
		r := fcsHash(i)
		fcs.OnBlock(r, prev, types.Hash{}, uint64(i), 0, 0)
		prev = r
		last = r
	}
	fcs.OnAttestation(0, last, 1, 32_000_000_000)

	head, err := fcs.GetHead()
	if err != nil {
		t.Fatalf("GetHead: %v", err)
	}
	if head != last {
		t.Fatalf("expected head at tip %v, got %v", last, head)
	}
}

func TestFCSV3PruneAndContinue(t *testing.T) {
	fcs := newTestFCS()
	genesis := fcsHash(0x01)
	fcs.OnBlock(genesis, types.Hash{}, types.Hash{}, 0, 0, 0)
	b2 := fcsHash(0x02)
	fcs.OnBlock(b2, genesis, types.Hash{}, 32, 1, 0)

	fcs.SetFinalizedCheckpoint(FCSCheckpoint{Epoch: 1, Root: b2})
	fcs.PruneBeforeFinalized()

	b3 := fcsHash(0x03)
	err := fcs.OnBlock(b3, b2, types.Hash{}, 64, 1, 0)
	if err != nil {
		t.Fatalf("OnBlock after prune: %v", err)
	}
	fcs.SetJustifiedCheckpoint(FCSCheckpoint{Epoch: 1, Root: b2})
	fcs.OnAttestation(0, b3, 2, 32_000_000_000)

	head, err := fcs.GetHead()
	if err != nil {
		t.Fatalf("GetHead: %v", err)
	}
	if head != b3 {
		t.Fatalf("expected b3, got %v", head)
	}
}

func TestFCSV3BestJustifiedUpdated(t *testing.T) {
	fcs := newTestFCS()
	genesis := fcsHash(0x01)
	fcs.OnBlock(genesis, types.Hash{}, types.Hash{}, 0, 0, 0)
	b2 := fcsHash(0x02)
	fcs.OnBlock(b2, genesis, types.Hash{}, 32, 3, 0)
	b3 := fcsHash(0x03)
	fcs.OnBlock(b3, b2, types.Hash{}, 64, 5, 1)

	fcs.mu.RLock()
	bestJ := fcs.bestJustified
	fcs.mu.RUnlock()
	if bestJ.Epoch != 5 {
		t.Fatalf("expected best justified epoch 5, got %d", bestJ.Epoch)
	}
}

func TestFCSV3AdvanceSlotAndDefaults(t *testing.T) {
	fcs := newTestFCS()
	fcs.AdvanceSlot(100)
	fcs.mu.RLock()
	slot := fcs.currentSlot
	fcs.mu.RUnlock()
	if slot != 100 {
		t.Fatalf("expected slot 100, got %d", slot)
	}

	// Default slots per epoch.
	fcs2 := NewForkChoiceStoreV3(ForkChoiceStoreV3Config{})
	fcs2.mu.RLock()
	spe := fcs2.slotsPerEpoch
	fcs2.mu.RUnlock()
	if spe != 32 {
		t.Fatalf("expected default slotsPerEpoch 32, got %d", spe)
	}
}
