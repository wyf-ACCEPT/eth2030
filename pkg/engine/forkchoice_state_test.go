package engine

import (
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// testHash returns a deterministic hash from a byte seed.
func testHash(seed byte) types.Hash {
	var h types.Hash
	h[0] = seed
	return h
}

// testBlockInfo creates a BlockInfo with the given parameters.
func testBlockInfo(hash types.Hash, parentHash types.Hash, number, slot uint64) *BlockInfo {
	return &BlockInfo{
		Hash:       hash,
		ParentHash: parentHash,
		Number:     number,
		Slot:       slot,
	}
}

func TestNewForkchoiceStateManager_Nil(t *testing.T) {
	m := NewForkchoiceStateManager(nil)
	if m == nil {
		t.Fatal("NewForkchoiceStateManager returned nil")
	}
	if m.Head() != (types.Hash{}) {
		t.Error("head should be zero hash with nil genesis")
	}
}

func TestNewForkchoiceStateManager_WithGenesis(t *testing.T) {
	genesisHash := testHash(1)
	genesis := testBlockInfo(genesisHash, types.Hash{}, 0, 0)
	m := NewForkchoiceStateManager(genesis)

	if m.Head() != genesisHash {
		t.Errorf("head = %s, want %s", m.Head().Hex(), genesisHash.Hex())
	}
	if m.SafeHead() != genesisHash {
		t.Errorf("safe head = %s, want %s", m.SafeHead().Hex(), genesisHash.Hex())
	}
	if m.FinalizedHead() != genesisHash {
		t.Errorf("finalized head = %s, want %s", m.FinalizedHead().Hex(), genesisHash.Hex())
	}
}

func TestAddBlock(t *testing.T) {
	m := NewForkchoiceStateManager(nil)
	h := testHash(2)
	info := testBlockInfo(h, types.Hash{}, 1, 1)
	m.AddBlock(info)

	if m.BlockCount() != 1 {
		t.Errorf("block count = %d, want 1", m.BlockCount())
	}

	// Adding nil should be a no-op.
	m.AddBlock(nil)
	if m.BlockCount() != 1 {
		t.Errorf("block count after nil add = %d, want 1", m.BlockCount())
	}
}

func TestProcessForkchoiceUpdate_ZeroHead(t *testing.T) {
	m := NewForkchoiceStateManager(nil)
	err := m.ProcessForkchoiceUpdate(ForkchoiceStateV1{})
	if err != ErrFCStateZeroHead {
		t.Errorf("expected ErrFCStateZeroHead, got %v", err)
	}
}

func TestProcessForkchoiceUpdate_HeadNotFound(t *testing.T) {
	m := NewForkchoiceStateManager(nil)
	err := m.ProcessForkchoiceUpdate(ForkchoiceStateV1{
		HeadBlockHash: testHash(99),
	})
	if err == nil {
		t.Fatal("expected error for unknown head")
	}
}

func TestProcessForkchoiceUpdate_Success(t *testing.T) {
	genesisHash := testHash(1)
	genesis := testBlockInfo(genesisHash, types.Hash{}, 0, 0)
	m := NewForkchoiceStateManager(genesis)

	blockHash := testHash(2)
	block := testBlockInfo(blockHash, genesisHash, 1, 32)
	m.AddBlock(block)

	err := m.ProcessForkchoiceUpdate(ForkchoiceStateV1{
		HeadBlockHash:      blockHash,
		SafeBlockHash:      blockHash,
		FinalizedBlockHash: genesisHash,
	})
	if err != nil {
		t.Fatalf("ProcessForkchoiceUpdate: %v", err)
	}

	if m.Head() != blockHash {
		t.Errorf("head = %s, want %s", m.Head().Hex(), blockHash.Hex())
	}
	if m.SafeHead() != blockHash {
		t.Errorf("safe = %s, want %s", m.SafeHead().Hex(), blockHash.Hex())
	}
	if m.FinalizedHead() != genesisHash {
		t.Errorf("finalized = %s, want %s", m.FinalizedHead().Hex(), genesisHash.Hex())
	}
}

func TestCheckpointUpdates(t *testing.T) {
	genesisHash := testHash(1)
	genesis := testBlockInfo(genesisHash, types.Hash{}, 0, 0)
	m := NewForkchoiceStateManager(genesis)

	safeHash := testHash(2)
	safeBlock := testBlockInfo(safeHash, genesisHash, 1, 64) // epoch 2
	m.AddBlock(safeBlock)

	finHash := testHash(3)
	finBlock := testBlockInfo(finHash, genesisHash, 0, 32) // epoch 1
	m.AddBlock(finBlock)

	err := m.ProcessForkchoiceUpdate(ForkchoiceStateV1{
		HeadBlockHash:      safeHash,
		SafeBlockHash:      safeHash,
		FinalizedBlockHash: finHash,
	})
	if err != nil {
		t.Fatal(err)
	}

	jcp := m.JustifiedCheckpoint()
	if jcp.Root != safeHash {
		t.Errorf("justified root = %s, want %s", jcp.Root.Hex(), safeHash.Hex())
	}
	if jcp.Epoch != 2 { // slot 64 / 32 = 2
		t.Errorf("justified epoch = %d, want 2", jcp.Epoch)
	}

	fcp := m.FinalizedCheckpoint()
	if fcp.Root != finHash {
		t.Errorf("finalized root = %s, want %s", fcp.Root.Hex(), finHash.Hex())
	}
	if fcp.Epoch != 1 { // slot 32 / 32 = 1
		t.Errorf("finalized epoch = %d, want 1", fcp.Epoch)
	}
}

func TestHeadInfo(t *testing.T) {
	genesisHash := testHash(1)
	genesis := testBlockInfo(genesisHash, types.Hash{}, 0, 0)
	m := NewForkchoiceStateManager(genesis)

	info := m.HeadInfo()
	if info == nil {
		t.Fatal("HeadInfo returned nil")
	}
	if info.Hash != genesisHash {
		t.Errorf("HeadInfo hash mismatch")
	}
	if info.Number != 0 {
		t.Errorf("HeadInfo number = %d, want 0", info.Number)
	}
}

func TestHeadInfo_Nil(t *testing.T) {
	m := NewForkchoiceStateManager(nil)
	if m.HeadInfo() != nil {
		t.Error("HeadInfo should be nil when no head is set")
	}
}

func TestIsHeadSafe(t *testing.T) {
	genesisHash := testHash(1)
	genesis := testBlockInfo(genesisHash, types.Hash{}, 0, 0)
	m := NewForkchoiceStateManager(genesis)

	// Initially head == safe == finalized.
	if !m.IsHeadSafe() {
		t.Error("head should be safe initially")
	}

	// Advance head but not safe.
	newHash := testHash(2)
	m.AddBlock(testBlockInfo(newHash, genesisHash, 1, 1))
	m.ProcessForkchoiceUpdate(ForkchoiceStateV1{
		HeadBlockHash:      newHash,
		SafeBlockHash:      genesisHash,
		FinalizedBlockHash: genesisHash,
	})

	if m.IsHeadSafe() {
		t.Error("head should not be safe when safe != head")
	}
}

func TestIsHeadFinalized(t *testing.T) {
	genesisHash := testHash(1)
	genesis := testBlockInfo(genesisHash, types.Hash{}, 0, 0)
	m := NewForkchoiceStateManager(genesis)

	if !m.IsHeadFinalized() {
		t.Error("head should be finalized initially")
	}

	newHash := testHash(2)
	m.AddBlock(testBlockInfo(newHash, genesisHash, 1, 1))
	m.ProcessForkchoiceUpdate(ForkchoiceStateV1{
		HeadBlockHash:      newHash,
		SafeBlockHash:      newHash,
		FinalizedBlockHash: genesisHash,
	})

	if m.IsHeadFinalized() {
		t.Error("head should not be finalized when finalized != head")
	}
}

func TestReorgDetection(t *testing.T) {
	genesisHash := testHash(1)
	genesis := testBlockInfo(genesisHash, types.Hash{}, 0, 0)
	m := NewForkchoiceStateManager(genesis)

	// Build two competing chains from genesis.
	blockA := testHash(2)
	m.AddBlock(testBlockInfo(blockA, genesisHash, 1, 1))

	blockB := testHash(3)
	m.AddBlock(testBlockInfo(blockB, genesisHash, 1, 1))

	// Set head to blockA.
	m.ProcessForkchoiceUpdate(ForkchoiceStateV1{
		HeadBlockHash:      blockA,
		SafeBlockHash:      genesisHash,
		FinalizedBlockHash: genesisHash,
	})

	// Track reorg events.
	var reorgEvents []ReorgEvent
	var mu sync.Mutex
	m.OnReorg(func(e ReorgEvent) {
		mu.Lock()
		reorgEvents = append(reorgEvents, e)
		mu.Unlock()
	})

	// Switch head to blockB (reorg).
	m.ProcessForkchoiceUpdate(ForkchoiceStateV1{
		HeadBlockHash:      blockB,
		SafeBlockHash:      genesisHash,
		FinalizedBlockHash: genesisHash,
	})

	mu.Lock()
	defer mu.Unlock()
	if len(reorgEvents) != 1 {
		t.Fatalf("expected 1 reorg event, got %d", len(reorgEvents))
	}
	if reorgEvents[0].OldHead != blockA {
		t.Errorf("reorg old head = %s, want %s", reorgEvents[0].OldHead.Hex(), blockA.Hex())
	}
	if reorgEvents[0].NewHead != blockB {
		t.Errorf("reorg new head = %s, want %s", reorgEvents[0].NewHead.Hex(), blockB.Hex())
	}
}

func TestNoReorgOnDescendant(t *testing.T) {
	genesisHash := testHash(1)
	genesis := testBlockInfo(genesisHash, types.Hash{}, 0, 0)
	m := NewForkchoiceStateManager(genesis)

	blockA := testHash(2)
	m.AddBlock(testBlockInfo(blockA, genesisHash, 1, 1))

	blockB := testHash(3)
	m.AddBlock(testBlockInfo(blockB, blockA, 2, 2))

	// Set head to blockA, then advance to blockB (descendant, not a reorg).
	m.ProcessForkchoiceUpdate(ForkchoiceStateV1{
		HeadBlockHash:      blockA,
		SafeBlockHash:      genesisHash,
		FinalizedBlockHash: genesisHash,
	})

	var reorgEvents []ReorgEvent
	m.OnReorg(func(e ReorgEvent) {
		reorgEvents = append(reorgEvents, e)
	})

	m.ProcessForkchoiceUpdate(ForkchoiceStateV1{
		HeadBlockHash:      blockB,
		SafeBlockHash:      genesisHash,
		FinalizedBlockHash: genesisHash,
	})

	if len(reorgEvents) != 0 {
		t.Errorf("expected no reorg events, got %d", len(reorgEvents))
	}
}

func TestProposerBoost(t *testing.T) {
	m := NewForkchoiceStateManager(nil)
	blockRoot := testHash(10)

	m.SetProposerBoost(1, blockRoot, 5000)

	boost := m.GetCurrentBoost()
	if boost == nil {
		t.Fatal("GetCurrentBoost returned nil")
	}
	if boost.Slot != 1 {
		t.Errorf("boost slot = %d, want 1", boost.Slot)
	}
	if boost.BoostWeight != 5000 {
		t.Errorf("boost weight = %d, want 5000", boost.BoostWeight)
	}

	// ProposerBoostFor with matching root.
	w := m.ProposerBoostFor(blockRoot)
	if w != 5000 {
		t.Errorf("ProposerBoostFor = %d, want 5000", w)
	}

	// ProposerBoostFor with non-matching root.
	w = m.ProposerBoostFor(testHash(11))
	if w != 0 {
		t.Errorf("ProposerBoostFor(other) = %d, want 0", w)
	}

	// Clear and verify.
	m.ClearProposerBoost()
	if m.GetCurrentBoost() != nil {
		t.Error("boost should be nil after clear")
	}
	if m.ProposerBoostFor(blockRoot) != 0 {
		t.Error("ProposerBoostFor should return 0 after clear")
	}
}

func TestGetForkchoiceState(t *testing.T) {
	genesisHash := testHash(1)
	genesis := testBlockInfo(genesisHash, types.Hash{}, 0, 0)
	m := NewForkchoiceStateManager(genesis)

	state := m.GetForkchoiceState()
	if state.HeadBlockHash != genesisHash {
		t.Errorf("head = %s, want %s", state.HeadBlockHash.Hex(), genesisHash.Hex())
	}
	if state.SafeBlockHash != genesisHash {
		t.Errorf("safe = %s, want %s", state.SafeBlockHash.Hex(), genesisHash.Hex())
	}
	if state.FinalizedBlockHash != genesisHash {
		t.Errorf("finalized = %s, want %s", state.FinalizedBlockHash.Hex(), genesisHash.Hex())
	}
}

func TestStats(t *testing.T) {
	genesisHash := testHash(1)
	genesis := testBlockInfo(genesisHash, types.Hash{}, 0, 0)
	m := NewForkchoiceStateManager(genesis)

	blockHash := testHash(2)
	m.AddBlock(testBlockInfo(blockHash, genesisHash, 1, 1))

	m.ProcessForkchoiceUpdate(ForkchoiceStateV1{
		HeadBlockHash:      blockHash,
		SafeBlockHash:      genesisHash,
		FinalizedBlockHash: genesisHash,
	})

	updateCount, reorgCount := m.Stats()
	if updateCount != 1 {
		t.Errorf("updateCount = %d, want 1", updateCount)
	}
	if reorgCount != 0 {
		t.Errorf("reorgCount = %d, want 0", reorgCount)
	}
}

func TestPruneBeforeNumber(t *testing.T) {
	genesisHash := testHash(1)
	genesis := testBlockInfo(genesisHash, types.Hash{}, 0, 0)
	m := NewForkchoiceStateManager(genesis)

	// Add blocks 1-5.
	prev := genesisHash
	for i := uint64(1); i <= 5; i++ {
		h := testHash(byte(i + 1))
		m.AddBlock(testBlockInfo(h, prev, i, i))
		prev = h
	}

	// Set head to block 5.
	m.ProcessForkchoiceUpdate(ForkchoiceStateV1{
		HeadBlockHash:      prev,
		SafeBlockHash:      prev,
		FinalizedBlockHash: prev,
	})

	// Should have genesis + 5 blocks = 6.
	if m.BlockCount() != 6 {
		t.Fatalf("block count = %d, want 6", m.BlockCount())
	}

	// Prune blocks before number 3. Block 5 (head/safe/finalized) is protected.
	pruned := m.PruneBeforeNumber(3)
	// Genesis (0), block 1, block 2 should be pruned = 3.
	// But head is block 5 which is protected, so pruned is blocks 0, 1, 2.
	// Wait - genesis is at number 0, head/safe/finalized are all prev (block 5).
	// Genesis hash != head hash so it gets pruned.
	if pruned != 3 {
		t.Errorf("pruned = %d, want 3", pruned)
	}
}
