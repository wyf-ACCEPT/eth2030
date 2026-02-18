package core

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/bal"
	"github.com/eth2028/eth2028/core/rawdb"
	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

// --- BAL (EIP-7928) Integration Tests ---

// TestProcessWithBAL_EmptyBlock verifies that an empty block produces an
// empty BAL when Amsterdam is active.
func TestProcessWithBAL_EmptyBlock(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	proc := NewStateProcessor(TestConfig)

	genesis := makeGenesis(30_000_000, big.NewInt(1))
	block := makeBlock(genesis, nil)

	result, err := proc.ProcessWithBAL(block, statedb)
	if err != nil {
		t.Fatalf("ProcessWithBAL: %v", err)
	}
	if result.BlockAccessList == nil {
		t.Fatal("expected non-nil BAL for Amsterdam block")
	}
	if result.BlockAccessList.Len() != 0 {
		t.Errorf("expected empty BAL, got %d entries", result.BlockAccessList.Len())
	}
}

// TestProcessWithBAL_WithTransactions verifies that transactions produce
// BAL entries tracking balance and nonce changes.
func TestProcessWithBAL_WithTransactions(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0x01})
	receiver := types.BytesToAddress([]byte{0x02})
	statedb.AddBalance(sender, big.NewInt(10_000_000))

	proc := NewStateProcessor(TestConfig)

	genesis := makeGenesis(30_000_000, big.NewInt(1))

	// Create a value transfer transaction.
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      21000,
		To:       &receiver,
		Value:    big.NewInt(100),
	})
	tx.SetSender(sender)

	// Use a copy of state for block building so the original state is
	// preserved for ProcessWithBAL re-execution.
	buildState := statedb.Copy()
	block := makeBlockWithState(genesis, []*types.Transaction{tx}, buildState)

	// Re-execute against the original (unmodified) state.
	result, err := proc.ProcessWithBAL(block, statedb)
	if err != nil {
		t.Fatalf("ProcessWithBAL: %v", err)
	}
	if result.BlockAccessList == nil {
		t.Fatal("expected non-nil BAL")
	}
	if result.BlockAccessList.Len() == 0 {
		t.Error("expected non-empty BAL for block with transactions")
	}

	// Verify there are entries for both sender and receiver.
	found := make(map[types.Address]bool)
	for _, entry := range result.BlockAccessList.Entries {
		found[entry.Address] = true
	}
	if !found[sender] {
		t.Error("BAL should contain sender address")
	}
	if !found[receiver] {
		t.Error("BAL should contain receiver address")
	}
}

// TestProcessWithBAL_PreAmsterdam verifies that BAL is nil when Amsterdam
// is not active.
func TestProcessWithBAL_PreAmsterdam(t *testing.T) {
	// Config without Amsterdam fork.
	config := &ChainConfig{
		ChainID:                 big.NewInt(1337),
		HomesteadBlock:          big.NewInt(0),
		ByzantiumBlock:          big.NewInt(0),
		ConstantinopleBlock:     big.NewInt(0),
		IstanbulBlock:           big.NewInt(0),
		BerlinBlock:             big.NewInt(0),
		LondonBlock:             big.NewInt(0),
		TerminalTotalDifficulty: big.NewInt(0),
		ShanghaiTime:            newUint64(0),
		CancunTime:              newUint64(0),
		PragueTime:              newUint64(0),
		AmsterdamTime:           nil, // Amsterdam NOT active
	}

	statedb := state.NewMemoryStateDB()
	proc := NewStateProcessor(config)

	genesis := makeGenesis(30_000_000, big.NewInt(1))
	block := makeBlock(genesis, nil)
	// Remove BAL hash from the header since Amsterdam is not active.
	header := block.Header()
	header.BlockAccessListHash = nil
	block = types.NewBlock(header, &types.Body{Withdrawals: []*types.Withdrawal{}})

	result, err := proc.ProcessWithBAL(block, statedb)
	if err != nil {
		t.Fatalf("ProcessWithBAL: %v", err)
	}
	if result.BlockAccessList != nil {
		t.Error("BAL should be nil for pre-Amsterdam block")
	}
}

// TestBALHash_Computed verifies the BAL hash computation is deterministic.
func TestBALHash_Computed(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0x01})
	receiver := types.BytesToAddress([]byte{0x02})
	statedb.AddBalance(sender, big.NewInt(10_000_000))

	proc := NewStateProcessor(TestConfig)

	genesis := makeGenesis(30_000_000, big.NewInt(1))

	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      21000,
		To:       &receiver,
		Value:    big.NewInt(100),
	})
	tx.SetSender(sender)

	// Use a copy for block building to preserve original state.
	buildState := statedb.Copy()
	block := makeBlockWithState(genesis, []*types.Transaction{tx}, buildState)

	// Process twice with fresh state copies to verify determinism.
	state1 := statedb.Copy()
	result1, err := proc.ProcessWithBAL(block, state1)
	if err != nil {
		t.Fatalf("first ProcessWithBAL: %v", err)
	}
	hash1 := result1.BlockAccessList.Hash()

	state2 := statedb.Copy()
	result2, err := proc.ProcessWithBAL(block, state2)
	if err != nil {
		t.Fatalf("second ProcessWithBAL: %v", err)
	}
	hash2 := result2.BlockAccessList.Hash()

	if hash1 != hash2 {
		t.Errorf("BAL hash not deterministic: %s != %s", hash1.Hex(), hash2.Hex())
	}

	// Verify the hash is not the empty BAL hash (since we have transactions).
	emptyHash := bal.NewBlockAccessList().Hash()
	if hash1 == emptyHash {
		t.Error("BAL hash should not be the empty BAL hash for a block with transactions")
	}
}

// TestBlockBuilder_SetsBALHash verifies that the block builder sets the
// BlockAccessListHash in the header when Amsterdam is active.
func TestBlockBuilder_SetsBALHash(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0x01})
	receiver := types.BytesToAddress([]byte{0x02})
	statedb.AddBalance(sender, big.NewInt(10_000_000))

	builder := newLegacyBuilder(TestConfig, statedb)

	parent := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 30_000_000,
		GasUsed:  0,
		BaseFee:  big.NewInt(1),
	}

	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(10),
		Gas:      21000,
		To:       &receiver,
		Value:    big.NewInt(100),
	})
	tx.SetSender(sender)

	block, _, err := builder.BuildBlockLegacy(parent, []*types.Transaction{tx}, 1700000001, types.BytesToAddress([]byte{0xff}), nil)
	if err != nil {
		t.Fatalf("BuildBlockLegacy: %v", err)
	}

	if block.Header().BlockAccessListHash == nil {
		t.Fatal("block builder should set BlockAccessListHash when Amsterdam is active")
	}
}

// TestBlockBuilder_EmptyBlock_SetsBALHash verifies that even an empty block
// gets a BAL hash when Amsterdam is active.
func TestBlockBuilder_EmptyBlock_SetsBALHash(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	builder := newLegacyBuilder(TestConfig, statedb)

	parent := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 30_000_000,
		GasUsed:  0,
		BaseFee:  big.NewInt(1),
	}

	block, _, err := builder.BuildBlockLegacy(parent, nil, 1700000001, types.BytesToAddress([]byte{0xff}), nil)
	if err != nil {
		t.Fatalf("BuildBlockLegacy: %v", err)
	}

	if block.Header().BlockAccessListHash == nil {
		t.Fatal("empty block should have BlockAccessListHash set when Amsterdam is active")
	}

	// Verify it matches the empty BAL hash.
	emptyHash := bal.NewBlockAccessList().Hash()
	if *block.Header().BlockAccessListHash != emptyHash {
		t.Errorf("empty block BAL hash = %s, want %s", block.Header().BlockAccessListHash.Hex(), emptyHash.Hex())
	}
}

// TestBlockBuilder_BuildBlock_SetsBALHash tests the new BuildBlock API sets BAL hash.
func TestBlockBuilder_BuildBlock_SetsBALHash(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0x01})
	receiver := types.BytesToAddress([]byte{0x02})
	statedb.AddBalance(sender, big.NewInt(10_000_000))

	genesis := makeGenesis(30_000_000, big.NewInt(1))
	db := rawdb.NewMemoryDB()
	bc, err := NewBlockchain(TestConfig, genesis, statedb, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}

	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(10),
		Gas:      21000,
		To:       &receiver,
		Value:    big.NewInt(100),
	})
	tx.SetSender(sender)

	pool := &mockTxPool{txs: []*types.Transaction{tx}}
	builder := NewBlockBuilder(TestConfig, bc, pool)

	attrs := &BuildBlockAttributes{
		Timestamp:    12,
		FeeRecipient: types.BytesToAddress([]byte{0xff}),
		GasLimit:     30_000_000,
	}

	block, _, err := builder.BuildBlock(genesis.Header(), attrs)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}

	if block.Header().BlockAccessListHash == nil {
		t.Fatal("BuildBlock should set BlockAccessListHash when Amsterdam is active")
	}
}

// TestValidator_RejectsBALMismatch verifies that ValidateBlockAccessList
// rejects a block with a mismatched BAL hash.
func TestValidator_RejectsBALMismatch(t *testing.T) {
	v := NewBlockValidator(TestConfig)

	wrongHash := types.HexToHash("0xdeadbeef")
	computedHash := types.HexToHash("0xcafebabe")

	header := &types.Header{
		Time:                12,
		BlockAccessListHash: &wrongHash,
	}

	err := v.ValidateBlockAccessList(header, &computedHash)
	if err == nil {
		t.Fatal("expected error for BAL hash mismatch")
	}
}

// TestValidator_RejectsMissingBALHash verifies that post-Amsterdam blocks
// must include a BlockAccessListHash.
func TestValidator_RejectsMissingBALHash(t *testing.T) {
	v := NewBlockValidator(TestConfig)

	header := &types.Header{
		Time:                12,
		BlockAccessListHash: nil, // missing
	}

	computedHash := types.HexToHash("0xcafebabe")
	err := v.ValidateBlockAccessList(header, &computedHash)
	if err == nil {
		t.Fatal("expected error for missing BAL hash in post-Amsterdam block")
	}
}

// TestValidator_AcceptsNilBALPreAmsterdam verifies that pre-Amsterdam blocks
// must NOT have a BlockAccessListHash.
func TestValidator_AcceptsNilBALPreAmsterdam(t *testing.T) {
	config := &ChainConfig{
		ChainID:       big.NewInt(1337),
		AmsterdamTime: nil, // Amsterdam NOT active
	}
	v := NewBlockValidator(config)

	header := &types.Header{
		Time:                12,
		BlockAccessListHash: nil,
	}

	err := v.ValidateBlockAccessList(header, nil)
	if err != nil {
		t.Fatalf("pre-Amsterdam block should not require BAL hash: %v", err)
	}
}

// TestValidator_RejectsBALInPreAmsterdam verifies that a pre-Amsterdam block
// with a BlockAccessListHash is rejected.
func TestValidator_RejectsBALInPreAmsterdam(t *testing.T) {
	config := &ChainConfig{
		ChainID:       big.NewInt(1337),
		AmsterdamTime: nil, // Amsterdam NOT active
	}
	v := NewBlockValidator(config)

	someHash := types.HexToHash("0xdeadbeef")
	header := &types.Header{
		Time:                12,
		BlockAccessListHash: &someHash,
	}

	err := v.ValidateBlockAccessList(header, nil)
	if err == nil {
		t.Fatal("pre-Amsterdam block should not have BAL hash")
	}
}

// TestValidator_AcceptsCorrectBAL verifies that a matching BAL hash passes validation.
func TestValidator_AcceptsCorrectBAL(t *testing.T) {
	v := NewBlockValidator(TestConfig)

	h := bal.NewBlockAccessList().Hash()
	header := &types.Header{
		Time:                12,
		BlockAccessListHash: &h,
	}

	err := v.ValidateBlockAccessList(header, &h)
	if err != nil {
		t.Fatalf("valid BAL hash rejected: %v", err)
	}
}

// TestBlockchain_BALValidation_EndToEnd verifies the full pipeline:
// build a block, insert it, and verify the BAL hash is validated.
func TestBlockchain_BALValidation_EndToEnd(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0x01})
	receiver := types.BytesToAddress([]byte{0x02})
	statedb.AddBalance(sender, big.NewInt(10_000_000))

	genesis := makeGenesis(30_000_000, big.NewInt(1))
	db := rawdb.NewMemoryDB()
	bc, err := NewBlockchain(TestConfig, genesis, statedb, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}

	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(10),
		Gas:      21000,
		To:       &receiver,
		Value:    big.NewInt(100),
	})
	tx.SetSender(sender)

	pool := &mockTxPool{txs: []*types.Transaction{tx}}
	builder := NewBlockBuilder(TestConfig, bc, pool)

	attrs := &BuildBlockAttributes{
		Timestamp:    12,
		FeeRecipient: types.BytesToAddress([]byte{0xff}),
		GasLimit:     30_000_000,
	}

	// Build the block (sets correct BAL hash).
	block, _, err := builder.BuildBlock(genesis.Header(), attrs)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}

	// Verify the block has a BAL hash.
	if block.Header().BlockAccessListHash == nil {
		t.Fatal("built block should have BAL hash")
	}

	// Insert the block (validates BAL hash during insertion).
	if err := bc.InsertBlock(block); err != nil {
		t.Fatalf("InsertBlock: %v", err)
	}

	// Verify block was inserted.
	if bc.CurrentBlock().NumberU64() != 1 {
		t.Errorf("head = %d, want 1", bc.CurrentBlock().NumberU64())
	}
}

// TestBlockchain_RejectsWrongBALHash verifies that a block with the wrong
// BAL hash is rejected during insertion.
func TestBlockchain_RejectsWrongBALHash(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0x01})
	receiver := types.BytesToAddress([]byte{0x02})
	statedb.AddBalance(sender, big.NewInt(10_000_000))

	genesis := makeGenesis(30_000_000, big.NewInt(1))
	db := rawdb.NewMemoryDB()
	bc, err := NewBlockchain(TestConfig, genesis, statedb, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}

	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(10),
		Gas:      21000,
		To:       &receiver,
		Value:    big.NewInt(100),
	})
	tx.SetSender(sender)

	pool := &mockTxPool{txs: []*types.Transaction{tx}}
	builder := NewBlockBuilder(TestConfig, bc, pool)

	attrs := &BuildBlockAttributes{
		Timestamp:    12,
		FeeRecipient: types.BytesToAddress([]byte{0xff}),
		GasLimit:     30_000_000,
	}

	// Build a valid block.
	block, _, err := builder.BuildBlock(genesis.Header(), attrs)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}

	// Tamper with the BAL hash.
	header := block.Header()
	wrongHash := types.HexToHash("0xdeadbeefdeadbeef")
	header.BlockAccessListHash = &wrongHash
	tamperedBlock := types.NewBlock(header, &types.Body{Transactions: block.Transactions(), Withdrawals: []*types.Withdrawal{}})

	// Insert should fail.
	err = bc.InsertBlock(tamperedBlock)
	if err == nil {
		t.Fatal("expected error for block with wrong BAL hash")
	}
}

// TestBAL_OnlyRequiredWhenForkActive verifies the conditional behavior:
// BAL hash required post-Amsterdam, not required pre-Amsterdam.
func TestBAL_OnlyRequiredWhenForkActive(t *testing.T) {
	// Create two configs: one with Amsterdam, one without.
	amsterdamTime := uint64(100)
	config := &ChainConfig{
		ChainID:                 big.NewInt(1337),
		HomesteadBlock:          big.NewInt(0),
		ByzantiumBlock:          big.NewInt(0),
		ConstantinopleBlock:     big.NewInt(0),
		IstanbulBlock:           big.NewInt(0),
		BerlinBlock:             big.NewInt(0),
		LondonBlock:             big.NewInt(0),
		TerminalTotalDifficulty: big.NewInt(0),
		ShanghaiTime:            newUint64(0),
		CancunTime:              newUint64(0),
		PragueTime:              newUint64(0),
		AmsterdamTime:           &amsterdamTime,
	}

	v := NewBlockValidator(config)

	// Pre-Amsterdam block (time 50): should NOT require BAL hash.
	preHeader := &types.Header{Time: 50}
	if err := v.ValidateBlockAccessList(preHeader, nil); err != nil {
		t.Errorf("pre-Amsterdam should not require BAL: %v", err)
	}

	// Post-Amsterdam block (time 200): should require BAL hash.
	postHeader := &types.Header{Time: 200, BlockAccessListHash: nil}
	if err := v.ValidateBlockAccessList(postHeader, nil); err == nil {
		t.Error("post-Amsterdam block without BAL hash should be rejected")
	}

	// Post-Amsterdam block with correct BAL hash: should pass.
	emptyHash := bal.NewBlockAccessList().Hash()
	postHeaderValid := &types.Header{Time: 200, BlockAccessListHash: &emptyHash}
	if err := v.ValidateBlockAccessList(postHeaderValid, &emptyHash); err != nil {
		t.Errorf("post-Amsterdam block with correct BAL should pass: %v", err)
	}
}
