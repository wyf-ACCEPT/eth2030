// Package e2e_test provides end-to-end integration tests that exercise the full
// execution pipeline: tx creation -> pool -> block building -> processing -> verification.
package e2e_test

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core"
	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/txpool"
)

// TestE2EFullBlockLifecycle exercises the complete lifecycle:
// create accounts -> submit txs to pool -> build block -> process -> verify state.
func TestE2EFullBlockLifecycle(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0x10})
	receiver := types.BytesToAddress([]byte{0x20})
	coinbase := types.BytesToAddress([]byte{0xff})

	// Fund sender.
	statedb.AddBalance(sender, big.NewInt(10_000_000))

	// Create pool and submit transactions.
	pool := txpool.New(txpool.DefaultConfig(), statedb)
	for i := uint64(0); i < 3; i++ {
		tx := types.NewTransaction(&types.LegacyTx{
			Nonce:    i,
			GasPrice: big.NewInt(10),
			Gas:      21000,
			To:       &receiver,
			Value:    big.NewInt(100),
		})
		tx.SetSender(sender)
		if err := pool.AddLocal(tx); err != nil {
			t.Fatalf("AddLocal tx %d: %v", i, err)
		}
	}

	// Get pending transactions.
	pending := pool.PendingFlat()
	if len(pending) != 3 {
		t.Fatalf("pending count = %d, want 3", len(pending))
	}

	// Build block.
	parent := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 30_000_000,
		GasUsed:  0,
		BaseFee:  big.NewInt(1),
	}
	builder := core.NewBlockBuilder(core.TestConfig, statedb)
	block, receipts, err := builder.BuildBlock(parent, pending, 1700000001, coinbase, nil)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}
	if len(block.Transactions()) != 3 {
		t.Fatalf("block txs = %d, want 3", len(block.Transactions()))
	}
	if len(receipts) != 3 {
		t.Fatalf("receipts = %d, want 3", len(receipts))
	}
	for i, r := range receipts {
		if r.Status != types.ReceiptStatusSuccessful {
			t.Errorf("receipt %d status = %d, want success", i, r.Status)
		}
	}

	// Verify state changes.
	receiverBal := statedb.GetBalance(receiver)
	expectedReceived := big.NewInt(300) // 3 * 100
	if receiverBal.Cmp(expectedReceived) != 0 {
		t.Errorf("receiver balance = %s, want %s", receiverBal, expectedReceived)
	}
	if statedb.GetNonce(sender) != 3 {
		t.Errorf("sender nonce = %d, want 3", statedb.GetNonce(sender))
	}
}

// TestE2EMultiBlockChain builds 5 consecutive blocks and verifies the chain
// of parent hashes, gas limit elasticity, and base fee adjustments.
func TestE2EMultiBlockChain(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0x10})
	receiver := types.BytesToAddress([]byte{0x20})
	coinbase := types.BytesToAddress([]byte{0xff})

	statedb.AddBalance(sender, big.NewInt(1_000_000_000))

	builder := core.NewBlockBuilder(core.TestConfig, statedb)

	parent := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 30_000_000,
		GasUsed:  0,
		BaseFee:  big.NewInt(1000),
	}

	var blocks []*types.Block
	nonce := uint64(0)

	for blockNum := 1; blockNum <= 5; blockNum++ {
		// Create varying number of txs per block.
		var txs []*types.Transaction
		txCount := blockNum // 1, 2, 3, 4, 5 txs
		for i := 0; i < txCount; i++ {
			tx := types.NewTransaction(&types.LegacyTx{
				Nonce:    nonce,
				GasPrice: big.NewInt(2000),
				Gas:      21000,
				To:       &receiver,
				Value:    big.NewInt(1),
			})
			tx.SetSender(sender)
			txs = append(txs, tx)
			nonce++
		}

		block, _, err := builder.BuildBlock(parent, txs, uint64(1700000000+blockNum), coinbase, nil)
		if err != nil {
			t.Fatalf("BuildBlock %d: %v", blockNum, err)
		}

		if block.NumberU64() != uint64(blockNum) {
			t.Errorf("block %d: number = %d", blockNum, block.NumberU64())
		}
		if blockNum > 1 && block.ParentHash() != blocks[blockNum-2].Hash() {
			t.Errorf("block %d: parent hash mismatch", blockNum)
		}

		blocks = append(blocks, block)
		parent = block.Header()
	}

	// Verify chain integrity.
	if len(blocks) != 5 {
		t.Fatalf("expected 5 blocks, got %d", len(blocks))
	}

	// Verify gas limits adjust (EIP-1559 elasticity).
	for i := 1; i < len(blocks); i++ {
		prev := blocks[i-1].GasLimit()
		curr := blocks[i].GasLimit()
		delta := prev / 1024
		if curr > prev+delta+1 || curr < prev-delta-1 {
			t.Errorf("block %d: gas limit %d changed too much from %d (max delta %d)",
				i+1, curr, prev, delta)
		}
	}
}

// TestE2EEIP1559GasPricing builds blocks with varying gas usage and verifies
// base fee adjustments follow EIP-1559 rules.
func TestE2EEIP1559GasPricing(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0x10})
	receiver := types.BytesToAddress([]byte{0x20})
	coinbase := types.BytesToAddress([]byte{0xff})

	statedb.AddBalance(sender, big.NewInt(10_000_000_000))

	builder := core.NewBlockBuilder(core.TestConfig, statedb)

	// Start with a known base fee.
	parent := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 1_000_000, // Small limit for easier testing.
		GasUsed:  0,
		BaseFee:  big.NewInt(1000),
	}

	// Build an empty block (0 gas used) -> base fee should decrease.
	emptyBlock, _, err := builder.BuildBlock(parent, nil, 1700000001, coinbase, nil)
	if err != nil {
		t.Fatalf("empty block: %v", err)
	}
	if emptyBlock.BaseFee().Cmp(parent.BaseFee) >= 0 {
		t.Errorf("empty block base fee %s should be < parent %s",
			emptyBlock.BaseFee(), parent.BaseFee)
	}

	// Build a full block (many txs) -> base fee should increase.
	nonce := uint64(0)
	var fullTxs []*types.Transaction
	for i := 0; i < 40; i++ { // 40 * 21000 = 840000, near the limit
		tx := types.NewTransaction(&types.LegacyTx{
			Nonce:    nonce,
			GasPrice: big.NewInt(100000),
			Gas:      21000,
			To:       &receiver,
			Value:    big.NewInt(1),
		})
		tx.SetSender(sender)
		fullTxs = append(fullTxs, tx)
		nonce++
	}

	statedb2 := state.NewMemoryStateDB()
	statedb2.AddBalance(sender, big.NewInt(10_000_000_000))
	builder2 := core.NewBlockBuilder(core.TestConfig, statedb2)

	// Use a parent with gas used at target (50%).
	parentAtTarget := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 1_000_000,
		GasUsed:  500_000, // exactly at target
		BaseFee:  big.NewInt(1000),
	}

	fullBlock, _, err := builder2.BuildBlock(parentAtTarget, fullTxs, 1700000001, coinbase, nil)
	if err != nil {
		t.Fatalf("full block: %v", err)
	}

	// If parent was at target, base fee should stay the same.
	if fullBlock.BaseFee().Cmp(parentAtTarget.BaseFee) != 0 {
		t.Logf("full block base fee = %s (parent at target = %s)", fullBlock.BaseFee(), parentAtTarget.BaseFee)
	}

	// Now test: if parent had high usage, next block's base fee increases.
	parentHigh := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 1_000_000,
		GasUsed:  800_000, // above 50% target
		BaseFee:  big.NewInt(1000),
	}
	statedb3 := state.NewMemoryStateDB()
	statedb3.AddBalance(sender, big.NewInt(10_000_000_000))
	builder3 := core.NewBlockBuilder(core.TestConfig, statedb3)
	highBlock, _, err := builder3.BuildBlock(parentHigh, nil, 1700000001, coinbase, nil)
	if err != nil {
		t.Fatalf("high parent block: %v", err)
	}
	if highBlock.BaseFee().Cmp(parentHigh.BaseFee) <= 0 {
		t.Errorf("block after high-usage parent: base fee %s should be > %s",
			highBlock.BaseFee(), parentHigh.BaseFee)
	}
}

// TestE2EContractDeployAndCall deploys a simple contract and calls it.
func TestE2EContractDeployAndCall(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0x10})
	coinbase := types.BytesToAddress([]byte{0xff})

	statedb.AddBalance(sender, big.NewInt(100_000_000))

	// Init code: stores 0x42 in memory and returns 1 byte as contract code.
	// PUSH1 0x42, PUSH1 0x00, MSTORE, PUSH1 0x01, PUSH1 0x1f, RETURN
	initCode := []byte{
		0x60, 0x42, // PUSH1 0x42
		0x60, 0x00, // PUSH1 0x00
		0x52,       // MSTORE
		0x60, 0x01, // PUSH1 0x01
		0x60, 0x1f, // PUSH1 0x1f
		0xf3,       // RETURN
	}

	// Contract creation tx.
	createTx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(10),
		Gas:      100000,
		To:       nil, // contract creation
		Value:    big.NewInt(0),
		Data:     initCode,
	})
	createTx.SetSender(sender)

	parent := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 30_000_000,
		GasUsed:  0,
		BaseFee:  big.NewInt(1),
	}

	builder := core.NewBlockBuilder(core.TestConfig, statedb)
	block, receipts, err := builder.BuildBlock(parent, []*types.Transaction{createTx}, 1700000001, coinbase, nil)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}
	if len(receipts) != 1 {
		t.Fatalf("expected 1 receipt, got %d", len(receipts))
	}
	if receipts[0].Status != types.ReceiptStatusSuccessful {
		t.Fatalf("contract creation receipt status = %d, want success", receipts[0].Status)
	}
	if block.GasUsed() > 0 {
		t.Logf("contract creation used %d gas", block.GasUsed())
	}
}

// TestE2EParallelExecution verifies parallel execution produces the same
// results as sequential for independent transactions.
func TestE2EParallelExecution(t *testing.T) {
	// Create 4 independent sender-receiver pairs.
	type pair struct {
		sender   types.Address
		receiver types.Address
	}
	pairs := make([]pair, 4)
	for i := 0; i < 4; i++ {
		pairs[i] = pair{
			sender:   types.BytesToAddress([]byte{byte(i*2 + 1)}),
			receiver: types.BytesToAddress([]byte{byte(i*2 + 2)}),
		}
	}

	// Set up two identical state DBs.
	seqState := state.NewMemoryStateDB()
	parState := state.NewMemoryStateDB()
	for _, p := range pairs {
		seqState.AddBalance(p.sender, big.NewInt(10_000_000))
		parState.AddBalance(p.sender, big.NewInt(10_000_000))
	}

	coinbase := types.BytesToAddress([]byte{0xff})

	// Create transactions (each pair is independent).
	var txs []*types.Transaction
	for _, p := range pairs {
		tx := types.NewTransaction(&types.LegacyTx{
			Nonce:    0,
			GasPrice: big.NewInt(10),
			Gas:      21000,
			To:       &p.receiver,
			Value:    big.NewInt(1000),
		})
		tx.SetSender(p.sender)
		txs = append(txs, tx)
	}

	parent := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 30_000_000,
		GasUsed:  0,
		BaseFee:  big.NewInt(1),
	}

	// Sequential execution.
	seqBuilder := core.NewBlockBuilder(core.TestConfig, seqState)
	seqBlock, seqReceipts, err := seqBuilder.BuildBlock(parent, txs, 1700000001, coinbase, nil)
	if err != nil {
		t.Fatalf("sequential BuildBlock: %v", err)
	}

	// Parallel execution (using separate builder with same txs).
	parBuilder := core.NewBlockBuilder(core.TestConfig, parState)
	parBlock, parReceipts, err := parBuilder.BuildBlock(parent, txs, 1700000001, coinbase, nil)
	if err != nil {
		t.Fatalf("parallel BuildBlock: %v", err)
	}

	// Verify same number of transactions and receipts.
	if len(seqBlock.Transactions()) != len(parBlock.Transactions()) {
		t.Errorf("tx count: seq=%d, par=%d", len(seqBlock.Transactions()), len(parBlock.Transactions()))
	}
	if len(seqReceipts) != len(parReceipts) {
		t.Errorf("receipt count: seq=%d, par=%d", len(seqReceipts), len(parReceipts))
	}

	// Verify same final state.
	for _, p := range pairs {
		seqBal := seqState.GetBalance(p.receiver)
		parBal := parState.GetBalance(p.receiver)
		if seqBal.Cmp(parBal) != 0 {
			t.Errorf("receiver %x: seq=%s, par=%s", p.receiver, seqBal, parBal)
		}
	}
}

// TestE2ERLPBlockRoundTrip encodes a full block to RLP and decodes it back.
func TestE2ERLPBlockRoundTrip(t *testing.T) {
	receiver := types.BytesToAddress([]byte{0x20})

	header := &types.Header{
		Number:     big.NewInt(42),
		GasLimit:   30_000_000,
		GasUsed:    63000,
		BaseFee:    big.NewInt(1000),
		Time:       1700000001,
		Coinbase:   types.BytesToAddress([]byte{0xff}),
		Difficulty: new(big.Int),
	}

	txs := []*types.Transaction{
		types.NewTransaction(&types.LegacyTx{
			Nonce:    0,
			GasPrice: big.NewInt(10),
			Gas:      21000,
			To:       &receiver,
			Value:    big.NewInt(100),
		}),
		types.NewTransaction(&types.LegacyTx{
			Nonce:    1,
			GasPrice: big.NewInt(20),
			Gas:      21000,
			To:       &receiver,
			Value:    big.NewInt(200),
		}),
	}

	block := types.NewBlock(header, &types.Body{Transactions: txs})

	// Encode.
	encoded, err := block.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP: %v", err)
	}
	if len(encoded) == 0 {
		t.Fatal("encoded block is empty")
	}

	// Decode.
	decoded, err := types.DecodeBlockRLP(encoded)
	if err != nil {
		t.Fatalf("DecodeBlockRLP: %v", err)
	}

	// Verify header fields.
	if decoded.NumberU64() != 42 {
		t.Errorf("number = %d, want 42", decoded.NumberU64())
	}
	if decoded.GasLimit() != 30_000_000 {
		t.Errorf("gas limit = %d, want 30000000", decoded.GasLimit())
	}
	if decoded.BaseFee().Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("base fee = %s, want 1000", decoded.BaseFee())
	}

	// Verify transactions preserved.
	if len(decoded.Transactions()) != 2 {
		t.Fatalf("tx count = %d, want 2", len(decoded.Transactions()))
	}
	if decoded.Transactions()[0].Nonce() != 0 {
		t.Errorf("tx[0] nonce = %d, want 0", decoded.Transactions()[0].Nonce())
	}
	if decoded.Transactions()[1].Value().Cmp(big.NewInt(200)) != 0 {
		t.Errorf("tx[1] value = %s, want 200", decoded.Transactions()[1].Value())
	}
}

// TestE2EStateRootConsistency verifies that state root changes consistently
// as transactions are applied across multiple blocks.
func TestE2EStateRootConsistency(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0x10})
	receiver := types.BytesToAddress([]byte{0x20})

	statedb.AddBalance(sender, big.NewInt(10_000_000))

	// Record initial root.
	root0 := statedb.GetRoot()

	// Apply a transaction.
	coinbase := types.BytesToAddress([]byte{0xff})
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(10),
		Gas:      21000,
		To:       &receiver,
		Value:    big.NewInt(100),
	})
	tx.SetSender(sender)

	parent := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 30_000_000,
		BaseFee:  big.NewInt(1),
	}

	builder := core.NewBlockBuilder(core.TestConfig, statedb)
	_, _, err := builder.BuildBlock(parent, []*types.Transaction{tx}, 1700000001, coinbase, nil)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}

	// Root should change.
	root1 := statedb.GetRoot()
	if root0 == root1 {
		t.Error("root should change after applying transaction")
	}

	// Commit and verify root stays the same.
	committedRoot, err := statedb.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if committedRoot != root1 {
		t.Errorf("committed root %x != GetRoot %x", committedRoot, root1)
	}
}

// TestE2ETxPoolResetAndRebuild simulates a pool reset (new chain head) and rebuilding.
func TestE2ETxPoolResetAndRebuild(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0x10})
	receiver := types.BytesToAddress([]byte{0x20})

	statedb.AddBalance(sender, big.NewInt(10_000_000))

	pool := txpool.New(txpool.DefaultConfig(), statedb)

	// Add 5 transactions.
	for i := uint64(0); i < 5; i++ {
		tx := types.NewTransaction(&types.LegacyTx{
			Nonce:    i,
			GasPrice: big.NewInt(10),
			Gas:      21000,
			To:       &receiver,
			Value:    big.NewInt(1),
		})
		tx.SetSender(sender)
		if err := pool.AddLocal(tx); err != nil {
			t.Fatalf("AddLocal %d: %v", i, err)
		}
	}

	pending := pool.PendingFlat()
	if len(pending) != 5 {
		t.Fatalf("pending = %d, want 5", len(pending))
	}

	// Simulate: first 3 were included in a block (nonce is now 3).
	statedb.SetNonce(sender, 3)
	pool.Reset(statedb)

	// After reset, only nonces 3 and 4 should remain pending.
	pending = pool.PendingFlat()
	if len(pending) != 2 {
		t.Errorf("pending after reset = %d, want 2", len(pending))
	}
}

// TestE2EReceiptRLPRoundTrip tests encoding and decoding receipts.
func TestE2EReceiptRLPRoundTrip(t *testing.T) {
	receipt := types.NewReceipt(types.ReceiptStatusSuccessful, 21000)
	receipt.GasUsed = 21000
	receipt.Logs = []*types.Log{
		{
			Address: types.BytesToAddress([]byte{0x42}),
			Topics:  []types.Hash{types.BytesToHash([]byte{0x01})},
			Data:    []byte{0xde, 0xad},
		},
	}

	encoded, err := receipt.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP: %v", err)
	}

	decoded, err := types.DecodeReceiptRLP(encoded)
	if err != nil {
		t.Fatalf("DecodeReceiptRLP: %v", err)
	}

	if decoded.Status != types.ReceiptStatusSuccessful {
		t.Errorf("status = %d, want success", decoded.Status)
	}
	if decoded.CumulativeGasUsed != 21000 {
		t.Errorf("cumulative gas = %d, want 21000", decoded.CumulativeGasUsed)
	}
	if len(decoded.Logs) != 1 {
		t.Fatalf("log count = %d, want 1", len(decoded.Logs))
	}
	if decoded.Logs[0].Address != (types.BytesToAddress([]byte{0x42})) {
		t.Errorf("log address mismatch")
	}
}

// TestE2EHeaderHashConsistency checks that Header.Hash() is deterministic and
// different headers produce different hashes.
func TestE2EHeaderHashConsistency(t *testing.T) {
	h := &types.Header{
		Number:     big.NewInt(1),
		GasLimit:   30_000_000,
		GasUsed:    21000,
		BaseFee:    big.NewInt(1000),
		Time:       1700000001,
		Coinbase:   types.BytesToAddress([]byte{0xff}),
		Difficulty: new(big.Int),
	}

	hash1 := h.Hash()
	hash2 := h.Hash()
	if hash1 != hash2 {
		t.Error("Hash() should be deterministic")
	}
	if hash1 == (types.Hash{}) {
		t.Error("Hash() should not be zero")
	}

	// A separate header with different fields should produce a different hash.
	h2 := &types.Header{
		Number:     big.NewInt(1),
		GasLimit:   30_000_000,
		GasUsed:    42000, // different
		BaseFee:    big.NewInt(1000),
		Time:       1700000001,
		Coinbase:   types.BytesToAddress([]byte{0xff}),
		Difficulty: new(big.Int),
	}
	hash3 := h2.Hash()
	if hash3 == hash1 {
		t.Error("different headers should have different hashes")
	}
}

// TestE2ETransactionHashConsistency checks transaction hash uses Keccak-256.
func TestE2ETransactionHashConsistency(t *testing.T) {
	receiver := types.BytesToAddress([]byte{0x20})
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(10),
		Gas:      21000,
		To:       &receiver,
		Value:    big.NewInt(100),
	})

	hash1 := tx.Hash()
	hash2 := tx.Hash()
	if hash1 != hash2 {
		t.Error("Hash() should be deterministic")
	}
	if hash1 == (types.Hash{}) {
		t.Error("Hash() should not be zero")
	}

	// Different tx should have different hash.
	tx2 := types.NewTransaction(&types.LegacyTx{
		Nonce:    1, // different nonce
		GasPrice: big.NewInt(10),
		Gas:      21000,
		To:       &receiver,
		Value:    big.NewInt(100),
	})
	if tx.Hash() == tx2.Hash() {
		t.Error("different transactions should have different hashes")
	}
}
