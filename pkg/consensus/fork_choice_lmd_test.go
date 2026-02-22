package consensus

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// Helper to create deterministic hashes for testing.
func lmdHash(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	return h
}

// --- VoteStore tests ---

func TestVoteStoreRecordAndGet(t *testing.T) {
	vs := NewVoteStore()

	vote := &LMDVote{
		ValidatorIndex: 1,
		TargetRoot:     lmdHash(0xAA),
		TargetEpoch:    5,
		Weight:         32,
	}
	if !vs.RecordVote(vote) {
		t.Fatal("expected RecordVote to succeed")
	}
	got := vs.GetVote(1)
	if got == nil {
		t.Fatal("expected vote, got nil")
	}
	if got.TargetRoot != lmdHash(0xAA) {
		t.Errorf("target root mismatch")
	}
	if got.TargetEpoch != 5 {
		t.Errorf("epoch = %d, want 5", got.TargetEpoch)
	}
}

func TestVoteStoreRejectStale(t *testing.T) {
	vs := NewVoteStore()

	vs.RecordVote(&LMDVote{ValidatorIndex: 1, TargetEpoch: 10, Weight: 32})
	if vs.RecordVote(&LMDVote{ValidatorIndex: 1, TargetEpoch: 5, Weight: 32}) {
		t.Error("stale vote should be rejected")
	}
	// Same epoch should succeed (update).
	if !vs.RecordVote(&LMDVote{ValidatorIndex: 1, TargetEpoch: 10, TargetRoot: lmdHash(0xBB), Weight: 32}) {
		t.Error("same-epoch vote should be accepted")
	}
}

func TestVoteStoreAllVotes(t *testing.T) {
	vs := NewVoteStore()
	for i := ValidatorIndex(0); i < 5; i++ {
		vs.RecordVote(&LMDVote{ValidatorIndex: i, TargetEpoch: 1, Weight: 32})
	}
	if vs.Len() != 5 {
		t.Errorf("Len = %d, want 5", vs.Len())
	}
	all := vs.AllVotes()
	if len(all) != 5 {
		t.Errorf("AllVotes len = %d, want 5", len(all))
	}
}

func TestVoteStoreRemoveVote(t *testing.T) {
	vs := NewVoteStore()
	vs.RecordVote(&LMDVote{ValidatorIndex: 1, TargetEpoch: 1, Weight: 32})
	if !vs.RemoveVote(1) {
		t.Error("expected RemoveVote to return true")
	}
	if vs.RemoveVote(1) {
		t.Error("second RemoveVote should return false")
	}
	if vs.GetVote(1) != nil {
		t.Error("vote should be nil after removal")
	}
}

// --- LMDGhostForkChoice tests ---

func TestLMDNewAndEmpty(t *testing.T) {
	fc := NewLMDGhost(LMDGhostConfig{SlotsPerEpoch: 32})
	if fc.BlockCount() != 0 {
		t.Errorf("BlockCount = %d, want 0", fc.BlockCount())
	}
	_, _, err := fc.GetHead()
	if err != ErrLMDEmptyTree {
		t.Errorf("GetHead on empty: got %v, want ErrLMDEmptyTree", err)
	}
}

func TestLMDOnBlockBasic(t *testing.T) {
	fc := NewLMDGhost(LMDGhostConfig{
		JustifiedRoot: lmdHash(0x01),
		SlotsPerEpoch: 32,
	})

	// Add root block.
	if err := fc.OnBlock(lmdHash(0x01), types.Hash{}, 0); err != nil {
		t.Fatalf("OnBlock root: %v", err)
	}
	// Add child.
	if err := fc.OnBlock(lmdHash(0x02), lmdHash(0x01), 1); err != nil {
		t.Fatalf("OnBlock child: %v", err)
	}
	if fc.BlockCount() != 2 {
		t.Errorf("BlockCount = %d, want 2", fc.BlockCount())
	}
}

func TestLMDOnBlockDuplicate(t *testing.T) {
	fc := NewLMDGhost(LMDGhostConfig{SlotsPerEpoch: 32})
	fc.OnBlock(lmdHash(0x01), types.Hash{}, 0)
	err := fc.OnBlock(lmdHash(0x01), types.Hash{}, 0)
	if err != ErrLMDDuplicateBlock {
		t.Errorf("duplicate: got %v, want ErrLMDDuplicateBlock", err)
	}
}

func TestLMDOnBlockUnknownParent(t *testing.T) {
	fc := NewLMDGhost(LMDGhostConfig{SlotsPerEpoch: 32})
	fc.OnBlock(lmdHash(0x01), types.Hash{}, 0)
	err := fc.OnBlock(lmdHash(0x03), lmdHash(0x99), 1)
	if err != ErrLMDUnknownParent {
		t.Errorf("unknown parent: got %v, want ErrLMDUnknownParent", err)
	}
}

func TestLMDProcessAttestation(t *testing.T) {
	fc := NewLMDGhost(LMDGhostConfig{
		JustifiedRoot: lmdHash(0x01),
		SlotsPerEpoch: 32,
	})
	fc.OnBlock(lmdHash(0x01), types.Hash{}, 0)
	fc.OnBlock(lmdHash(0x02), lmdHash(0x01), 1)

	err := fc.ProcessAttestation(0, lmdHash(0x02), 1, 32)
	if err != nil {
		t.Fatalf("ProcessAttestation: %v", err)
	}
	if fc.VoteCount() != 1 {
		t.Errorf("VoteCount = %d, want 1", fc.VoteCount())
	}
}

func TestLMDProcessAttestationUnknownBlock(t *testing.T) {
	fc := NewLMDGhost(LMDGhostConfig{SlotsPerEpoch: 32})
	fc.OnBlock(lmdHash(0x01), types.Hash{}, 0)

	err := fc.ProcessAttestation(0, lmdHash(0x99), 1, 32)
	if err == nil {
		t.Error("expected error for unknown block attestation")
	}
}

func TestLMDProcessAttestationStale(t *testing.T) {
	fc := NewLMDGhost(LMDGhostConfig{
		JustifiedRoot: lmdHash(0x01),
		SlotsPerEpoch: 32,
	})
	fc.OnBlock(lmdHash(0x01), types.Hash{}, 0)
	fc.OnBlock(lmdHash(0x02), lmdHash(0x01), 1)

	fc.ProcessAttestation(0, lmdHash(0x02), 10, 32)
	err := fc.ProcessAttestation(0, lmdHash(0x02), 5, 32)
	if err != ErrLMDStaleAttestation {
		t.Errorf("stale attestation: got %v, want ErrLMDStaleAttestation", err)
	}
}

func TestLMDGetHeadSingleChain(t *testing.T) {
	fc := NewLMDGhost(LMDGhostConfig{
		JustifiedRoot: lmdHash(0x01),
		SlotsPerEpoch: 32,
	})
	fc.OnBlock(lmdHash(0x01), types.Hash{}, 0)
	fc.OnBlock(lmdHash(0x02), lmdHash(0x01), 1)
	fc.OnBlock(lmdHash(0x03), lmdHash(0x02), 2)

	head, reorg, err := fc.GetHead()
	if err != nil {
		t.Fatalf("GetHead: %v", err)
	}
	if head != lmdHash(0x03) {
		t.Errorf("head = %s, want %s", head.Hex(), lmdHash(0x03).Hex())
	}
	if reorg {
		t.Error("expected no reorg on first call")
	}
}

func TestLMDGetHeadForkWithVotes(t *testing.T) {
	// Tree:
	//   0x01 (root)
	//   +-- 0x02 (fork A)
	//   +-- 0x03 (fork B)
	fc := NewLMDGhost(LMDGhostConfig{
		JustifiedRoot: lmdHash(0x01),
		SlotsPerEpoch: 32,
	})
	fc.OnBlock(lmdHash(0x01), types.Hash{}, 0)
	fc.OnBlock(lmdHash(0x02), lmdHash(0x01), 1)
	fc.OnBlock(lmdHash(0x03), lmdHash(0x01), 1)

	// Vote heavily for fork B.
	for i := ValidatorIndex(0); i < 10; i++ {
		fc.ProcessAttestation(i, lmdHash(0x03), 1, 32)
	}
	// Vote lightly for fork A.
	fc.ProcessAttestation(100, lmdHash(0x02), 1, 32)

	head, _, err := fc.GetHead()
	if err != nil {
		t.Fatalf("GetHead: %v", err)
	}
	if head != lmdHash(0x03) {
		t.Errorf("head = %s, want 0x03 (heavier fork)", head.Hex())
	}
}

func TestLMDGetHeadReorgDetection(t *testing.T) {
	fc := NewLMDGhost(LMDGhostConfig{
		JustifiedRoot: lmdHash(0x01),
		SlotsPerEpoch: 32,
	})
	fc.OnBlock(lmdHash(0x01), types.Hash{}, 0)
	fc.OnBlock(lmdHash(0x02), lmdHash(0x01), 1)
	fc.OnBlock(lmdHash(0x03), lmdHash(0x01), 1)

	// Initially head = 0x02 (both equal weight, tiebreak by hash).
	fc.ProcessAttestation(0, lmdHash(0x02), 1, 32)
	head1, reorg1, _ := fc.GetHead()
	if reorg1 {
		t.Error("first GetHead should not be a reorg")
	}

	// Now shift majority weight to 0x03.
	for i := ValidatorIndex(1); i < 20; i++ {
		fc.ProcessAttestation(i, lmdHash(0x03), 2, 32)
	}
	head2, reorg2, _ := fc.GetHead()
	if head2 == head1 {
		t.Error("head should have changed")
	}
	if !reorg2 {
		t.Error("expected reorg when head changes")
	}
}

func TestLMDPrune(t *testing.T) {
	fc := NewLMDGhost(LMDGhostConfig{
		JustifiedRoot: lmdHash(0x01),
		SlotsPerEpoch: 32,
	})
	// Chain: 0x01 -> 0x02 -> 0x03
	//                     -> 0x04
	fc.OnBlock(lmdHash(0x01), types.Hash{}, 0)
	fc.OnBlock(lmdHash(0x02), lmdHash(0x01), 1)
	fc.OnBlock(lmdHash(0x03), lmdHash(0x02), 2)
	fc.OnBlock(lmdHash(0x04), lmdHash(0x02), 2)

	// Prune at 0x02: should remove 0x01, keep 0x02, 0x03, 0x04.
	pruned := fc.Prune(lmdHash(0x02))
	if pruned != 1 {
		t.Errorf("pruned = %d, want 1", pruned)
	}
	if fc.BlockCount() != 3 {
		t.Errorf("BlockCount after prune = %d, want 3", fc.BlockCount())
	}
	if fc.HasBlock(lmdHash(0x01)) {
		t.Error("0x01 should be pruned")
	}
	if !fc.HasBlock(lmdHash(0x02)) {
		t.Error("0x02 should remain")
	}
}

func TestLMDPruneUnknownRoot(t *testing.T) {
	fc := NewLMDGhost(LMDGhostConfig{SlotsPerEpoch: 32})
	fc.OnBlock(lmdHash(0x01), types.Hash{}, 0)

	pruned := fc.Prune(lmdHash(0x99))
	if pruned != 0 {
		t.Errorf("pruning unknown root: pruned = %d, want 0", pruned)
	}
}

func TestLMDSetJustifiedAndFinalized(t *testing.T) {
	fc := NewLMDGhost(LMDGhostConfig{SlotsPerEpoch: 32})
	fc.SetJustified(lmdHash(0xAA), 5)
	fc.SetFinalized(lmdHash(0xBB), 4)

	fc.mu.RLock()
	if fc.justifiedRoot != lmdHash(0xAA) {
		t.Error("justified root mismatch")
	}
	if fc.justifiedEpoch != 5 {
		t.Error("justified epoch mismatch")
	}
	if fc.finalizedRoot != lmdHash(0xBB) {
		t.Error("finalized root mismatch")
	}
	if fc.finalizedEpoch != 4 {
		t.Error("finalized epoch mismatch")
	}
	fc.mu.RUnlock()
}

func TestLMDHasBlock(t *testing.T) {
	fc := NewLMDGhost(LMDGhostConfig{SlotsPerEpoch: 32})
	fc.OnBlock(lmdHash(0x01), types.Hash{}, 0)

	if !fc.HasBlock(lmdHash(0x01)) {
		t.Error("HasBlock should return true for existing block")
	}
	if fc.HasBlock(lmdHash(0x99)) {
		t.Error("HasBlock should return false for non-existing block")
	}
}

func TestLMDGetBlock(t *testing.T) {
	fc := NewLMDGhost(LMDGhostConfig{SlotsPerEpoch: 32})
	fc.OnBlock(lmdHash(0x01), types.Hash{}, 42)

	node := fc.GetBlock(lmdHash(0x01))
	if node == nil {
		t.Fatal("GetBlock returned nil")
	}
	if node.Slot != 42 {
		t.Errorf("Slot = %d, want 42", node.Slot)
	}
	if fc.GetBlock(lmdHash(0x99)) != nil {
		t.Error("GetBlock for unknown should return nil")
	}
}

func TestLMDDeepChainHead(t *testing.T) {
	fc := NewLMDGhost(LMDGhostConfig{
		JustifiedRoot: lmdHash(0x01),
		SlotsPerEpoch: 32,
	})
	// Build a chain of 20 blocks.
	prev := lmdHash(0x01)
	fc.OnBlock(prev, types.Hash{}, 0)
	var last types.Hash
	for i := byte(2); i <= 20; i++ {
		h := lmdHash(i)
		fc.OnBlock(h, prev, uint64(i))
		prev = h
		last = h
	}

	head, _, err := fc.GetHead()
	if err != nil {
		t.Fatalf("GetHead: %v", err)
	}
	if head != last {
		t.Errorf("head = %s, want %s", head.Hex(), last.Hex())
	}
}

func TestLMDConcurrentAccess(t *testing.T) {
	fc := NewLMDGhost(LMDGhostConfig{
		JustifiedRoot: lmdHash(0x01),
		SlotsPerEpoch: 32,
	})
	fc.OnBlock(lmdHash(0x01), types.Hash{}, 0)
	fc.OnBlock(lmdHash(0x02), lmdHash(0x01), 1)

	var wg sync.WaitGroup
	// Concurrent attestations.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			fc.ProcessAttestation(ValidatorIndex(idx), lmdHash(0x02), 1, 32)
		}(i)
	}
	// Concurrent GetHead.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fc.GetHead()
		}()
	}
	wg.Wait()

	if fc.VoteCount() != 50 {
		t.Errorf("VoteCount = %d, want 50", fc.VoteCount())
	}
}

func TestLMDDefaultSlotsPerEpoch(t *testing.T) {
	fc := NewLMDGhost(LMDGhostConfig{})
	fc.mu.RLock()
	if fc.slotsPerEpoch != 32 {
		t.Errorf("default slotsPerEpoch = %d, want 32", fc.slotsPerEpoch)
	}
	fc.mu.RUnlock()
}

func TestLMDVotesAccessor(t *testing.T) {
	fc := NewLMDGhost(LMDGhostConfig{SlotsPerEpoch: 32})
	fc.OnBlock(lmdHash(0x01), types.Hash{}, 0)
	fc.ProcessAttestation(1, lmdHash(0x01), 1, 32)

	vs := fc.Votes()
	if vs.Len() != 1 {
		t.Errorf("Votes().Len() = %d, want 1", vs.Len())
	}
}

func TestLMDMultipleForksSameWeight(t *testing.T) {
	// Test deterministic tiebreaking: with equal weight, lower hash wins.
	fc := NewLMDGhost(LMDGhostConfig{
		JustifiedRoot: lmdHash(0x01),
		SlotsPerEpoch: 32,
	})
	fc.OnBlock(lmdHash(0x01), types.Hash{}, 0)
	fc.OnBlock(lmdHash(0x10), lmdHash(0x01), 1) // higher hash
	fc.OnBlock(lmdHash(0x02), lmdHash(0x01), 1) // lower hash

	// Equal weight.
	fc.ProcessAttestation(0, lmdHash(0x10), 1, 32)
	fc.ProcessAttestation(1, lmdHash(0x02), 1, 32)

	head, _, err := fc.GetHead()
	if err != nil {
		t.Fatalf("GetHead: %v", err)
	}
	// hashLess picks the lower hash (0x02 < 0x10).
	if head != lmdHash(0x02) {
		t.Errorf("tiebreak: head = %s, want 0x02", head.Hex())
	}
}
