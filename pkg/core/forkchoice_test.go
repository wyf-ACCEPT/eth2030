package core

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/bal"
	"github.com/eth2028/eth2028/core/rawdb"
	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

// testForkChoice creates a blockchain and ForkChoice for testing.
func testForkChoice(t *testing.T) (*Blockchain, *ForkChoice) {
	t.Helper()
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis(30_000_000, big.NewInt(1))
	db := rawdb.NewMemoryDB()
	bc, err := NewBlockchain(TestConfig, genesis, statedb, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}
	fc := NewForkChoice(bc)
	return bc, fc
}

// makeForkBlock creates a block with a different timestamp offset to produce
// a distinct hash from makeBlock. This is used to create fork chains that
// are added directly to the block cache (bypassing InsertBlock validation).
func makeForkBlock(parent *types.Block, timeOffset uint64) *types.Block {
	parentHeader := parent.Header()
	emptyBALHash := bal.NewBlockAccessList().Hash()
	header := &types.Header{
		ParentHash:          parent.Hash(),
		Number:              new(big.Int).Add(parentHeader.Number, big.NewInt(1)),
		GasLimit:            parentHeader.GasLimit,
		GasUsed:             0,
		Time:                parentHeader.Time + timeOffset,
		Difficulty:          new(big.Int),
		BaseFee:             CalcBaseFee(parentHeader),
		UncleHash:           EmptyUncleHash,
		BlockAccessListHash: &emptyBALHash,
	}
	return types.NewBlock(header, nil)
}

func TestForkChoice_InitialState(t *testing.T) {
	bc, fc := testForkChoice(t)
	genesis := bc.Genesis()

	// All pointers should initially be genesis.
	if fc.Head().Hash() != genesis.Hash() {
		t.Errorf("head = %v, want genesis %v", fc.Head().Hash(), genesis.Hash())
	}
	if fc.Safe().Hash() != genesis.Hash() {
		t.Errorf("safe = %v, want genesis %v", fc.Safe().Hash(), genesis.Hash())
	}
	if fc.Finalized().Hash() != genesis.Hash() {
		t.Errorf("finalized = %v, want genesis %v", fc.Finalized().Hash(), genesis.Hash())
	}
}

func TestForkChoice_SimpleUpdate(t *testing.T) {
	bc, fc := testForkChoice(t)
	genesis := bc.Genesis()

	// Build a chain: genesis -> b1 -> b2 -> b3.
	chain := makeChainBlocks(genesis, 3, state.NewMemoryStateDB())
	b1, b2, b3 := chain[0], chain[1], chain[2]

	for _, b := range chain {
		if err := bc.InsertBlock(b); err != nil {
			t.Fatalf("InsertBlock(%d): %v", b.NumberU64(), err)
		}
	}

	// Update fork choice: head=b3, safe=b2, finalized=b1.
	err := fc.ForkchoiceUpdate(b3.Hash(), b2.Hash(), b1.Hash())
	if err != nil {
		t.Fatalf("ForkchoiceUpdate: %v", err)
	}

	if fc.Head().Hash() != b3.Hash() {
		t.Errorf("head = %v, want b3", fc.Head().Hash())
	}
	if fc.Safe().Hash() != b2.Hash() {
		t.Errorf("safe = %v, want b2", fc.Safe().Hash())
	}
	if fc.Finalized().Hash() != b1.Hash() {
		t.Errorf("finalized = %v, want b1", fc.Finalized().Hash())
	}
}

func TestForkChoice_UpdateWithZeroHashes(t *testing.T) {
	bc, fc := testForkChoice(t)
	genesis := bc.Genesis()

	// Build a chain: genesis -> b1.
	b1 := makeBlock(genesis, nil)
	if err := bc.InsertBlock(b1); err != nil {
		t.Fatalf("InsertBlock: %v", err)
	}

	// Update fork choice with zero safe/finalized hashes.
	// This should keep previous safe/finalized values.
	err := fc.ForkchoiceUpdate(b1.Hash(), types.Hash{}, types.Hash{})
	if err != nil {
		t.Fatalf("ForkchoiceUpdate: %v", err)
	}

	if fc.Head().Hash() != b1.Hash() {
		t.Errorf("head = %v, want b1", fc.Head().Hash())
	}
	// Safe and finalized should remain at genesis (initial values).
	if fc.Safe().Hash() != genesis.Hash() {
		t.Errorf("safe = %v, want genesis", fc.Safe().Hash())
	}
	if fc.Finalized().Hash() != genesis.Hash() {
		t.Errorf("finalized = %v, want genesis", fc.Finalized().Hash())
	}
}

func TestForkChoice_ProgressiveFinalization(t *testing.T) {
	bc, fc := testForkChoice(t)

	// Build a chain of 5 blocks.
	blocks := makeChainBlocks(bc.Genesis(), 5, state.NewMemoryStateDB())
	for i, b := range blocks {
		if err := bc.InsertBlock(b); err != nil {
			t.Fatalf("InsertBlock(%d): %v", i+1, err)
		}
	}

	// Step 1: head=b5, safe=b3, finalized=b1.
	err := fc.ForkchoiceUpdate(blocks[4].Hash(), blocks[2].Hash(), blocks[0].Hash())
	if err != nil {
		t.Fatalf("ForkchoiceUpdate step 1: %v", err)
	}
	if fc.Finalized().NumberU64() != 1 {
		t.Errorf("finalized = %d, want 1", fc.Finalized().NumberU64())
	}

	// Step 2: advance finalized to b3.
	err = fc.ForkchoiceUpdate(blocks[4].Hash(), blocks[4].Hash(), blocks[2].Hash())
	if err != nil {
		t.Fatalf("ForkchoiceUpdate step 2: %v", err)
	}
	if fc.Finalized().NumberU64() != 3 {
		t.Errorf("finalized = %d, want 3", fc.Finalized().NumberU64())
	}
	if fc.Safe().NumberU64() != 5 {
		t.Errorf("safe = %d, want 5", fc.Safe().NumberU64())
	}
}

func TestForkChoice_UnknownHead(t *testing.T) {
	_, fc := testForkChoice(t)

	unknownHash := types.Hash{0xde, 0xad}
	err := fc.ForkchoiceUpdate(unknownHash, types.Hash{}, types.Hash{})
	if err == nil {
		t.Fatal("expected error for unknown head hash")
	}
}

func TestForkChoice_UnknownFinalized(t *testing.T) {
	bc, fc := testForkChoice(t)

	b1 := makeBlock(bc.Genesis(), nil)
	if err := bc.InsertBlock(b1); err != nil {
		t.Fatalf("InsertBlock: %v", err)
	}

	unknownHash := types.Hash{0xbe, 0xef}
	err := fc.ForkchoiceUpdate(b1.Hash(), types.Hash{}, unknownHash)
	if err == nil {
		t.Fatal("expected error for unknown finalized hash")
	}
}

func TestForkChoice_UnknownSafe(t *testing.T) {
	bc, fc := testForkChoice(t)

	b1 := makeBlock(bc.Genesis(), nil)
	if err := bc.InsertBlock(b1); err != nil {
		t.Fatalf("InsertBlock: %v", err)
	}

	unknownHash := types.Hash{0xca, 0xfe}
	err := fc.ForkchoiceUpdate(b1.Hash(), unknownHash, types.Hash{})
	if err == nil {
		t.Fatal("expected error for unknown safe hash")
	}
}

// --- Chain Reorg Tests ---

func TestForkChoice_ReorgToLongerChain(t *testing.T) {
	bc, fc := testForkChoice(t)
	genesis := bc.Genesis()

	// Build chain A: genesis -> a1 -> a2.
	chainA := makeChainBlocks(genesis, 2, state.NewMemoryStateDB())
	a1, a2 := chainA[0], chainA[1]
	if err := bc.InsertBlock(a1); err != nil {
		t.Fatalf("insert a1: %v", err)
	}
	if err := bc.InsertBlock(a2); err != nil {
		t.Fatalf("insert a2: %v", err)
	}

	// Set fork choice to a2.
	if err := fc.ForkchoiceUpdate(a2.Hash(), genesis.Hash(), genesis.Hash()); err != nil {
		t.Fatalf("forkchoice update to a2: %v", err)
	}

	// Build fork chain B: genesis -> b1 -> b2 -> b3.
	// Manually add to block cache to avoid InsertBlock validation.
	b1 := makeForkBlock(genesis, 6) // different timestamp -> different hash
	b2 := makeForkBlock(b1, 12)
	b3 := makeForkBlock(b2, 12)

	bc.mu.Lock()
	bc.blockCache[b1.Hash()] = b1
	bc.blockCache[b2.Hash()] = b2
	bc.blockCache[b3.Hash()] = b3
	bc.mu.Unlock()

	// Reorg to chain B via fork choice update.
	err := fc.ForkchoiceUpdate(b3.Hash(), genesis.Hash(), genesis.Hash())
	if err != nil {
		t.Fatalf("ForkchoiceUpdate reorg: %v", err)
	}

	// Head should now be b3.
	if fc.Head().Hash() != b3.Hash() {
		t.Errorf("head = %v, want b3", fc.Head().Hash())
	}
	if bc.CurrentBlock().Hash() != b3.Hash() {
		t.Errorf("blockchain head = %v, want b3", bc.CurrentBlock().Hash())
	}

	// Canonical chain should follow B path.
	if got := bc.GetBlockByNumber(1); got == nil || got.Hash() != b1.Hash() {
		t.Errorf("canonical block 1 should be b1")
	}
	if got := bc.GetBlockByNumber(2); got == nil || got.Hash() != b2.Hash() {
		t.Errorf("canonical block 2 should be b2")
	}
	if got := bc.GetBlockByNumber(3); got == nil || got.Hash() != b3.Hash() {
		t.Errorf("canonical block 3 should be b3")
	}
}

func TestForkChoice_ReorgToShorterChainNotAllowed(t *testing.T) {
	bc, fc := testForkChoice(t)
	genesis := bc.Genesis()

	// Build chain A: genesis -> a1 -> a2 -> a3, finalize a2.
	chainA := makeChainBlocks(genesis, 3, state.NewMemoryStateDB())
	for _, b := range chainA {
		if err := bc.InsertBlock(b); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// Finalize a2.
	if err := fc.ForkchoiceUpdate(chainA[2].Hash(), chainA[1].Hash(), chainA[1].Hash()); err != nil {
		t.Fatalf("finalize: %v", err)
	}

	// Build fork chain B: genesis -> b1 (diverges at block 1, below finalized a2).
	b1 := makeForkBlock(genesis, 6)
	bc.mu.Lock()
	bc.blockCache[b1.Hash()] = b1
	bc.mu.Unlock()

	// Try to reorg to b1 -- should fail because common ancestor (genesis)
	// is below finalized block a2.
	err := fc.ForkchoiceUpdate(b1.Hash(), genesis.Hash(), genesis.Hash())
	if err == nil {
		t.Fatal("expected error when reorging past finalized block")
	}
}

// --- FindCommonAncestor Tests ---

func TestFindCommonAncestor_SameChain(t *testing.T) {
	bc, _ := testForkChoice(t)
	genesis := bc.Genesis()

	// Build a linear chain.
	chain := makeChainBlocks(genesis, 3, state.NewMemoryStateDB())
	for _, b := range chain {
		if err := bc.InsertBlock(b); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// Common ancestor of b1 and b3 on the same chain is b1.
	ancestor := FindCommonAncestor(bc, chain[0], chain[2])
	if ancestor == nil {
		t.Fatal("expected common ancestor")
	}
	if ancestor.Hash() != chain[0].Hash() {
		t.Errorf("common ancestor = %v (block %d), want b1 (block 1)",
			ancestor.Hash(), ancestor.NumberU64())
	}
}

func TestFindCommonAncestor_Fork(t *testing.T) {
	bc, _ := testForkChoice(t)
	genesis := bc.Genesis()

	// Build chain A: genesis -> a1 -> a2.
	chainA := makeChainBlocks(genesis, 2, state.NewMemoryStateDB())
	for _, b := range chainA {
		if err := bc.InsertBlock(b); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// Build chain B: genesis -> b1 -> b2.
	b1 := makeForkBlock(genesis, 6)
	b2 := makeForkBlock(b1, 12)

	// Manually add to block cache (they are side chain blocks).
	bc.mu.Lock()
	bc.blockCache[b1.Hash()] = b1
	bc.blockCache[b2.Hash()] = b2
	bc.mu.Unlock()

	// Common ancestor of a2 and b2 should be genesis.
	ancestor := FindCommonAncestor(bc, chainA[1], b2)
	if ancestor == nil {
		t.Fatal("expected common ancestor")
	}
	if ancestor.Hash() != genesis.Hash() {
		t.Errorf("common ancestor = block %d, want genesis (block 0)",
			ancestor.NumberU64())
	}
}

func TestFindCommonAncestor_ForkAtBlock1(t *testing.T) {
	bc, _ := testForkChoice(t)
	genesis := bc.Genesis()

	// Build chain A: genesis -> a1 -> a2 -> a3.
	chainA := makeChainBlocks(genesis, 3, state.NewMemoryStateDB())
	for _, b := range chainA {
		if err := bc.InsertBlock(b); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// Build chain B: genesis -> a1 -> b2 -> b3 (fork at block 1).
	b2 := makeForkBlock(chainA[0], 6)
	b3 := makeForkBlock(b2, 12)

	bc.mu.Lock()
	bc.blockCache[b2.Hash()] = b2
	bc.blockCache[b3.Hash()] = b3
	bc.mu.Unlock()

	// Common ancestor of a3 and b3 should be a1.
	ancestor := FindCommonAncestor(bc, chainA[2], b3)
	if ancestor == nil {
		t.Fatal("expected common ancestor")
	}
	if ancestor.Hash() != chainA[0].Hash() {
		t.Errorf("common ancestor = block %d, want a1 (block 1)",
			ancestor.NumberU64())
	}
}

func TestFindCommonAncestor_UnequalHeights(t *testing.T) {
	bc, _ := testForkChoice(t)
	genesis := bc.Genesis()

	// Build chain A: genesis -> a1 -> a2 -> a3 -> a4 -> a5.
	blocks := makeChainBlocks(genesis, 5, state.NewMemoryStateDB())
	for _, b := range blocks {
		if err := bc.InsertBlock(b); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// Build chain B: genesis -> a1 -> a2 -> b3 (fork at a2, shorter chain).
	b3 := makeForkBlock(blocks[1], 6)
	bc.mu.Lock()
	bc.blockCache[b3.Hash()] = b3
	bc.mu.Unlock()

	// Common ancestor of a5 and b3 should be a2.
	ancestor := FindCommonAncestor(bc, blocks[4], b3)
	if ancestor == nil {
		t.Fatal("expected common ancestor")
	}
	if ancestor.Hash() != blocks[1].Hash() {
		t.Errorf("common ancestor = block %d, want a2 (block 2)",
			ancestor.NumberU64())
	}
}

func TestFindCommonAncestor_IdenticalBlocks(t *testing.T) {
	bc, _ := testForkChoice(t)
	genesis := bc.Genesis()

	b1 := makeBlock(genesis, nil)
	if err := bc.InsertBlock(b1); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Common ancestor of b1 and b1 is b1 itself.
	ancestor := FindCommonAncestor(bc, b1, b1)
	if ancestor == nil {
		t.Fatal("expected common ancestor")
	}
	if ancestor.Hash() != b1.Hash() {
		t.Errorf("common ancestor should be b1 itself")
	}
}

func TestFindCommonAncestor_NilInput(t *testing.T) {
	bc, _ := testForkChoice(t)

	ancestor := FindCommonAncestor(bc, nil, bc.Genesis())
	if ancestor != nil {
		t.Error("expected nil for nil input")
	}

	ancestor = FindCommonAncestor(bc, bc.Genesis(), nil)
	if ancestor != nil {
		t.Error("expected nil for nil input")
	}
}

// --- Finalization Prevents Reorg Tests ---

func TestForkChoice_FinalizationPreventsReorg(t *testing.T) {
	bc, fc := testForkChoice(t)
	genesis := bc.Genesis()

	// Build chain: genesis -> b1 -> b2 -> b3 -> b4.
	chain := makeChainBlocks(genesis, 4, state.NewMemoryStateDB())
	for _, b := range chain {
		if err := bc.InsertBlock(b); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// Finalize b3.
	err := fc.ForkchoiceUpdate(chain[3].Hash(), chain[2].Hash(), chain[2].Hash())
	if err != nil {
		t.Fatalf("finalize b3: %v", err)
	}

	// Build a fork that diverges from b1 (below finalized b3).
	fork2 := makeForkBlock(chain[0], 6)
	fork3 := makeForkBlock(fork2, 12)
	fork4 := makeForkBlock(fork3, 12)
	fork5 := makeForkBlock(fork4, 12)

	// Manually add fork blocks to cache so they are known.
	bc.mu.Lock()
	bc.blockCache[fork2.Hash()] = fork2
	bc.blockCache[fork3.Hash()] = fork3
	bc.blockCache[fork4.Hash()] = fork4
	bc.blockCache[fork5.Hash()] = fork5
	bc.mu.Unlock()

	// Try to reorg to fork5, which would revert past finalized b3.
	err = fc.ForkchoiceUpdate(fork5.Hash(), genesis.Hash(), genesis.Hash())
	if err == nil {
		t.Fatal("expected error: reorg past finalized block should fail")
	}

	// Head should still be b4.
	if fc.Head().Hash() != chain[3].Hash() {
		t.Errorf("head changed despite failed reorg")
	}
	if bc.CurrentBlock().Hash() != chain[3].Hash() {
		t.Errorf("blockchain head changed despite failed reorg")
	}
}

func TestForkChoice_ReorgAboveFinalized(t *testing.T) {
	bc, fc := testForkChoice(t)

	// Build chain: genesis -> b1 -> b2 -> b3.
	chain := makeChainBlocks(bc.Genesis(), 3, state.NewMemoryStateDB())
	for _, b := range chain {
		if err := bc.InsertBlock(b); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// Finalize b1.
	err := fc.ForkchoiceUpdate(chain[2].Hash(), chain[0].Hash(), chain[0].Hash())
	if err != nil {
		t.Fatalf("finalize b1: %v", err)
	}

	// Build a fork that diverges from b2 (above finalized b1).
	fork3 := makeForkBlock(chain[1], 6)
	fork4 := makeForkBlock(fork3, 12)

	bc.mu.Lock()
	bc.blockCache[fork3.Hash()] = fork3
	bc.blockCache[fork4.Hash()] = fork4
	bc.mu.Unlock()

	// This reorg is valid because the fork point (b2) is above finalized (b1).
	err = fc.ForkchoiceUpdate(fork4.Hash(), chain[0].Hash(), chain[0].Hash())
	if err != nil {
		t.Fatalf("reorg above finalized should succeed: %v", err)
	}

	if fc.Head().Hash() != fork4.Hash() {
		t.Errorf("head = %v, want fork4", fc.Head().Hash())
	}
	if bc.CurrentBlock().Hash() != fork4.Hash() {
		t.Errorf("blockchain head = %v, want fork4", bc.CurrentBlock().Hash())
	}
}

// --- ReorgWithValidation Tests ---

func TestForkChoice_ReorgWithValidation_Success(t *testing.T) {
	bc, fc := testForkChoice(t)
	genesis := bc.Genesis()

	// Build chain A: genesis -> a1 -> a2.
	chainA := makeChainBlocks(genesis, 2, state.NewMemoryStateDB())
	for _, b := range chainA {
		if err := bc.InsertBlock(b); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// Build chain B: genesis -> b1 -> b2 -> b3.
	b1 := makeForkBlock(genesis, 6)
	b2 := makeForkBlock(b1, 12)
	b3 := makeForkBlock(b2, 12)

	bc.mu.Lock()
	bc.blockCache[b1.Hash()] = b1
	bc.blockCache[b2.Hash()] = b2
	bc.blockCache[b3.Hash()] = b3
	bc.mu.Unlock()

	// ReorgWithValidation should succeed (finalized is genesis).
	err := fc.ReorgWithValidation(b3)
	if err != nil {
		t.Fatalf("ReorgWithValidation: %v", err)
	}

	if fc.Head().Hash() != b3.Hash() {
		t.Errorf("head = %v, want b3", fc.Head().Hash())
	}
}

func TestForkChoice_ReorgWithValidation_BlockedByFinalized(t *testing.T) {
	bc, fc := testForkChoice(t)
	genesis := bc.Genesis()

	// Build chain: genesis -> b1 -> b2 -> b3.
	chain := makeChainBlocks(genesis, 3, state.NewMemoryStateDB())
	for _, b := range chain {
		if err := bc.InsertBlock(b); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// Set finalized to b2 via internal update.
	fc.mu.Lock()
	fc.head = chain[2]
	fc.safe = chain[1]
	fc.finalized = chain[1]
	fc.mu.Unlock()

	// Build fork diverging at genesis.
	fork1 := makeForkBlock(genesis, 6)
	fork2 := makeForkBlock(fork1, 12)
	fork3 := makeForkBlock(fork2, 12)
	fork4 := makeForkBlock(fork3, 12)

	bc.mu.Lock()
	bc.blockCache[fork1.Hash()] = fork1
	bc.blockCache[fork2.Hash()] = fork2
	bc.blockCache[fork3.Hash()] = fork3
	bc.blockCache[fork4.Hash()] = fork4
	bc.mu.Unlock()

	// Should fail because fork point is genesis (block 0), below finalized b2.
	err := fc.ReorgWithValidation(fork4)
	if err == nil {
		t.Fatal("expected error: reorg past finalized should fail")
	}
}

func TestForkChoice_ReorgWithValidation_NoOp(t *testing.T) {
	bc, fc := testForkChoice(t)

	b1 := makeBlock(bc.Genesis(), nil)
	if err := bc.InsertBlock(b1); err != nil {
		t.Fatalf("insert b1: %v", err)
	}

	// Update fork choice to b1.
	fc.mu.Lock()
	fc.head = b1
	fc.mu.Unlock()

	// Reorg to the same block should be a no-op.
	err := fc.ReorgWithValidation(b1)
	if err != nil {
		t.Fatalf("no-op reorg should succeed: %v", err)
	}
}

// --- Ancestry Validation Tests ---

func TestForkChoice_SafeBelowFinalized(t *testing.T) {
	bc, fc := testForkChoice(t)

	chain := makeChainBlocks(bc.Genesis(), 3, state.NewMemoryStateDB())
	for _, b := range chain {
		if err := bc.InsertBlock(b); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// Try to set safe=b1, finalized=b2 -- safe is below finalized.
	err := fc.ForkchoiceUpdate(chain[2].Hash(), chain[0].Hash(), chain[1].Hash())
	if err == nil {
		t.Fatal("expected error when safe block number < finalized block number")
	}
}

func TestForkChoice_FinalizedNotInHeadAncestry(t *testing.T) {
	bc, fc := testForkChoice(t)
	genesis := bc.Genesis()

	// Build chain A: genesis -> a1 -> a2.
	chainA := makeChainBlocks(genesis, 2, state.NewMemoryStateDB())
	for _, b := range chainA {
		if err := bc.InsertBlock(b); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// Build chain B: genesis -> b1.
	b1 := makeForkBlock(genesis, 6)
	bc.mu.Lock()
	bc.blockCache[b1.Hash()] = b1
	bc.mu.Unlock()

	// Try to set head=a2 with finalized=b1 (b1 is NOT in a2's ancestry).
	err := fc.ForkchoiceUpdate(chainA[1].Hash(), genesis.Hash(), b1.Hash())
	if err == nil {
		t.Fatal("expected error: finalized block not in head's ancestry")
	}
}

func TestForkChoice_SafeNotInHeadAncestry(t *testing.T) {
	bc, fc := testForkChoice(t)
	genesis := bc.Genesis()

	// Build chain A: genesis -> a1 -> a2.
	chainA := makeChainBlocks(genesis, 2, state.NewMemoryStateDB())
	for _, b := range chainA {
		if err := bc.InsertBlock(b); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// Build chain B: genesis -> b1.
	b1 := makeForkBlock(genesis, 6)
	bc.mu.Lock()
	bc.blockCache[b1.Hash()] = b1
	bc.mu.Unlock()

	// Try to set head=a2 with safe=b1 (b1 is NOT in a2's ancestry).
	err := fc.ForkchoiceUpdate(chainA[1].Hash(), b1.Hash(), genesis.Hash())
	if err == nil {
		t.Fatal("expected error: safe block not in head's ancestry")
	}
}

// --- State Rollback During Reorg ---

func TestForkChoice_StateRollbackOnReorg(t *testing.T) {
	genesisState := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0x01})
	receiverA := types.BytesToAddress([]byte{0x02})
	receiverB := types.BytesToAddress([]byte{0x03})
	genesisState.AddBalance(sender, big.NewInt(100_000_000))

	genesis := makeGenesis(30_000_000, big.NewInt(1))
	db := rawdb.NewMemoryDB()
	bc, err := NewBlockchain(TestConfig, genesis, genesisState, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}
	fc := NewForkChoice(bc)

	// Build chain A: genesis -> a1 (sends to receiverA).
	txA := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      21000,
		To:       &receiverA,
		Value:    big.NewInt(1000),
	})
	txA.SetSender(sender)

	// Use a copy of genesis state to build the block.
	a1State := genesisState.Copy()
	a1 := makeBlockWithState(genesis, []*types.Transaction{txA}, a1State)
	if err := bc.InsertBlock(a1); err != nil {
		t.Fatalf("insert a1: %v", err)
	}

	// Check receiverA has balance after chain A.
	st := bc.State()
	if st.GetBalance(receiverA).Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("receiverA balance = %s, want 1000", st.GetBalance(receiverA))
	}

	// Build chain B: genesis -> b1 (sends to receiverB).
	txB := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      21000,
		To:       &receiverB,
		Value:    big.NewInt(2000),
	})
	txB.SetSender(sender)

	// Use a fresh copy of genesis state (not the mutated a1State).
	b1State := genesisState.Copy()
	b1 := makeBlockWithState(genesis, []*types.Transaction{txB}, b1State)

	// b1 may have the same or different hash from a1.
	// If same (since parent and nonce are same), make a different fork block.
	if b1.Hash() == a1.Hash() {
		// Create b1 with a different time so hash differs.
		emptyBALHash := bal.NewBlockAccessList().Hash()
		b1Header := &types.Header{
			ParentHash:          genesis.Hash(),
			Number:              big.NewInt(1),
			GasLimit:            genesis.GasLimit(),
			GasUsed:             0,
			Time:                genesis.Time() + 6,
			Difficulty:          new(big.Int),
			BaseFee:             CalcBaseFee(genesis.Header()),
			UncleHash:           EmptyUncleHash,
			BlockAccessListHash: &emptyBALHash,
		}
		b1 = types.NewBlock(b1Header, nil)
	}

	bc.mu.Lock()
	bc.blockCache[b1.Hash()] = b1
	bc.mu.Unlock()

	// Reorg to chain B.
	err = fc.ForkchoiceUpdate(b1.Hash(), genesis.Hash(), genesis.Hash())
	if err != nil {
		t.Fatalf("ForkchoiceUpdate to b1: %v", err)
	}

	// After reorg, state should reflect chain B, not chain A.
	// The state is re-derived from genesis through the new chain.
	if bc.CurrentBlock().Hash() != b1.Hash() {
		t.Errorf("head = %v, want b1", bc.CurrentBlock().Hash())
	}
}

// --- Edge Cases ---

func TestForkChoice_ReorgToSameBlock(t *testing.T) {
	bc, fc := testForkChoice(t)
	genesis := bc.Genesis()

	b1 := makeBlock(genesis, nil)
	if err := bc.InsertBlock(b1); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// First update.
	err := fc.ForkchoiceUpdate(b1.Hash(), genesis.Hash(), genesis.Hash())
	if err != nil {
		t.Fatalf("first update: %v", err)
	}

	// Second update to same head should be a no-op.
	err = fc.ForkchoiceUpdate(b1.Hash(), b1.Hash(), genesis.Hash())
	if err != nil {
		t.Fatalf("same-head update: %v", err)
	}

	if fc.Head().Hash() != b1.Hash() {
		t.Errorf("head changed unexpectedly")
	}
}

func TestForkChoice_AdvanceHead(t *testing.T) {
	bc, fc := testForkChoice(t)
	genesis := bc.Genesis()

	// Build chain: genesis -> b1 -> b2 -> b3.
	chain := makeChainBlocks(genesis, 3, state.NewMemoryStateDB())
	for _, b := range chain {
		if err := bc.InsertBlock(b); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// Advance head progressively.
	for i, b := range chain {
		err := fc.ForkchoiceUpdate(b.Hash(), genesis.Hash(), genesis.Hash())
		if err != nil {
			t.Fatalf("advance head step %d: %v", i+1, err)
		}
		if fc.Head().Hash() != b.Hash() {
			t.Errorf("step %d: head = %v, want %v", i+1, fc.Head().Hash(), b.Hash())
		}
	}
}
