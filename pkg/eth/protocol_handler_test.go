package eth

import (
	"errors"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// TestProtocolHandler_RegisterPeer tests peer registration.
func TestProtocolHandler_RegisterPeer(t *testing.T) {
	bc := newMockBlockchain()
	tp := newMockTxPool()
	ph := NewProtocolHandler(bc, tp, 1)

	if err := ph.RegisterPeer("peer-1", ETH68); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ph.PeerCount() != 1 {
		t.Fatalf("want 1 peer, got %d", ph.PeerCount())
	}
}

// TestProtocolHandler_UnregisterPeer tests peer removal.
func TestProtocolHandler_UnregisterPeer(t *testing.T) {
	bc := newMockBlockchain()
	tp := newMockTxPool()
	ph := NewProtocolHandler(bc, tp, 1)

	ph.RegisterPeer("peer-1", ETH68)
	ph.UnregisterPeer("peer-1")
	if ph.PeerCount() != 0 {
		t.Fatalf("want 0 peers after unregister, got %d", ph.PeerCount())
	}
}

// TestProtocolHandler_GetPeerState tests peer state retrieval.
func TestProtocolHandler_GetPeerState(t *testing.T) {
	bc := newMockBlockchain()
	tp := newMockTxPool()
	ph := NewProtocolHandler(bc, tp, 1)

	ph.RegisterPeer("peer-1", ETH68)
	state := ph.GetPeerState("peer-1")
	if state == nil {
		t.Fatal("expected non-nil peer state")
	}
	if state.PeerID != "peer-1" {
		t.Fatalf("want peer-1, got %s", state.PeerID)
	}
	if state.Version != ETH68 {
		t.Fatalf("want version %d, got %d", ETH68, state.Version)
	}
}

// TestProtocolHandler_GetPeerState_NotFound tests missing peer state.
func TestProtocolHandler_GetPeerState_NotFound(t *testing.T) {
	bc := newMockBlockchain()
	tp := newMockTxPool()
	ph := NewProtocolHandler(bc, tp, 1)

	state := ph.GetPeerState("nonexistent")
	if state != nil {
		t.Fatal("expected nil for non-existent peer")
	}
}

// TestProtocolHandler_HandleNewBlockHashes tests block hash announcement processing.
func TestProtocolHandler_HandleNewBlockHashes(t *testing.T) {
	bc := newMockBlockchain()
	tp := newMockTxPool()
	ph := NewProtocolHandler(bc, tp, 1)
	ph.RegisterPeer("peer-1", ETH68)

	// Genesis block is known; create an unknown hash.
	unknownHash := types.HexToHash("0xdeadbeef")
	entries := []BlockHashEntry{
		{Hash: bc.genesis.Hash(), Number: 0},
		{Hash: unknownHash, Number: 1},
	}

	result, err := ph.HandleNewBlockHashes("peer-1", entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Unknown) != 1 {
		t.Fatalf("want 1 unknown, got %d", len(result.Unknown))
	}
	if result.Unknown[0].Hash != unknownHash {
		t.Fatalf("want unknown hash %s, got %s", unknownHash.Hex(), result.Unknown[0].Hash.Hex())
	}
}

// TestProtocolHandler_HandleNewBlockHashes_TooMany tests exceeding the limit.
func TestProtocolHandler_HandleNewBlockHashes_TooMany(t *testing.T) {
	bc := newMockBlockchain()
	tp := newMockTxPool()
	ph := NewProtocolHandler(bc, tp, 1)

	entries := make([]BlockHashEntry, MaxNewBlockHashes+1)
	_, err := ph.HandleNewBlockHashes("peer-1", entries)
	if !errors.Is(err, ErrProtoHandlerTooMany) {
		t.Fatalf("want ErrProtoHandlerTooMany, got %v", err)
	}
}

// TestProtocolHandler_HandleNewBlock tests new block processing.
func TestProtocolHandler_HandleNewBlock(t *testing.T) {
	bc := newMockBlockchain()
	tp := newMockTxPool()
	ph := NewProtocolHandler(bc, tp, 1)
	ph.RegisterPeer("peer-1", ETH68)

	block := makeTestBlock(1, bc.genesis.Hash(), nil)
	td := big.NewInt(2)

	err := ph.HandleNewBlock("peer-1", block, td)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify peer state was updated.
	state := ph.GetPeerState("peer-1")
	if state.HeadHash != block.Hash() {
		t.Fatalf("want head hash %s, got %s", block.Hash().Hex(), state.HeadHash.Hex())
	}
	if state.HeadNumber != 1 {
		t.Fatalf("want head number 1, got %d", state.HeadNumber)
	}
	if state.TD.Cmp(td) != 0 {
		t.Fatalf("want TD %s, got %s", td, state.TD)
	}

	// Verify block was inserted.
	if !bc.HasBlock(block.Hash()) {
		t.Fatal("expected block to be inserted")
	}
}

// TestProtocolHandler_HandleNewBlock_NilBlock tests nil block rejection.
func TestProtocolHandler_HandleNewBlock_NilBlock(t *testing.T) {
	bc := newMockBlockchain()
	tp := newMockTxPool()
	ph := NewProtocolHandler(bc, tp, 1)

	err := ph.HandleNewBlock("peer-1", nil, big.NewInt(1))
	if !errors.Is(err, ErrProtoHandlerInvalidMsg) {
		t.Fatalf("want ErrProtoHandlerInvalidMsg, got %v", err)
	}
}

// TestProtocolHandler_HandleTransactions tests transaction processing.
func TestProtocolHandler_HandleTransactions(t *testing.T) {
	bc := newMockBlockchain()
	tp := newMockTxPool()
	ph := NewProtocolHandler(bc, tp, 1)

	txs := []*types.Transaction{
		makeTestTx(0),
		makeTestTx(1),
		makeTestTx(2),
	}

	result := ph.HandleTransactions("peer-1", txs)
	if result.Added != 3 {
		t.Fatalf("want 3 added, got %d", result.Added)
	}
	if result.Rejected != 0 {
		t.Fatalf("want 0 rejected, got %d", result.Rejected)
	}
}

// TestProtocolHandler_HandleTransactions_Truncation tests tx list truncation.
func TestProtocolHandler_HandleTransactions_Truncation(t *testing.T) {
	bc := newMockBlockchain()
	tp := newMockTxPool()
	ph := NewProtocolHandler(bc, tp, 1)

	// Create more than MaxTransactionsPerMsg.
	txs := make([]*types.Transaction, MaxTransactionsPerMsg+10)
	for i := range txs {
		txs[i] = makeTestTx(uint64(i))
	}

	result := ph.HandleTransactions("peer-1", txs)
	if result.Added != MaxTransactionsPerMsg {
		t.Fatalf("want %d added (truncated), got %d", MaxTransactionsPerMsg, result.Added)
	}
}

// TestProtocolHandler_HandlePooledTransactions tests pooled tx processing.
func TestProtocolHandler_HandlePooledTransactions(t *testing.T) {
	bc := newMockBlockchain()
	tp := newMockTxPool()
	ph := NewProtocolHandler(bc, tp, 1)

	tx1 := makeTestTx(0)
	tx2 := makeTestTx(1)
	requested := []types.Hash{tx1.Hash(), tx2.Hash(), types.HexToHash("0xmissing")}
	received := []*types.Transaction{tx1, tx2}

	result := ph.HandlePooledTransactions("peer-1", requested, received)
	if result.Requested != 3 {
		t.Fatalf("want 3 requested, got %d", result.Requested)
	}
	if result.Received != 2 {
		t.Fatalf("want 2 received, got %d", result.Received)
	}
	if len(result.Missing) != 1 {
		t.Fatalf("want 1 missing, got %d", len(result.Missing))
	}
}

// TestProtocolHandler_HandleGetBlockHeaders tests header response.
func TestProtocolHandler_HandleGetBlockHeaders(t *testing.T) {
	bc := newMockBlockchain()
	// Add a few blocks.
	for i := uint64(1); i <= 5; i++ {
		parent := bc.byNumber[i-1]
		block := makeTestBlock(i, parent.Hash(), nil)
		bc.addBlock(block)
	}

	tp := newMockTxPool()
	ph := NewProtocolHandler(bc, tp, 1)

	result := ph.HandleGetBlockHeaders(types.Hash{}, 0, false, 3, 0, false)
	if result.Count != 3 {
		t.Fatalf("want 3 headers, got %d", result.Count)
	}
	if result.Headers[0].Number.Uint64() != 0 {
		t.Fatalf("want first header at 0, got %d", result.Headers[0].Number.Uint64())
	}
	if result.Headers[2].Number.Uint64() != 2 {
		t.Fatalf("want last header at 2, got %d", result.Headers[2].Number.Uint64())
	}
}

// TestProtocolHandler_HandleGetBlockHeaders_Reverse tests reverse header fetch.
func TestProtocolHandler_HandleGetBlockHeaders_Reverse(t *testing.T) {
	bc := newMockBlockchain()
	for i := uint64(1); i <= 5; i++ {
		parent := bc.byNumber[i-1]
		block := makeTestBlock(i, parent.Hash(), nil)
		bc.addBlock(block)
	}

	tp := newMockTxPool()
	ph := NewProtocolHandler(bc, tp, 1)

	result := ph.HandleGetBlockHeaders(types.Hash{}, 5, false, 3, 0, true)
	if result.Count != 3 {
		t.Fatalf("want 3 headers, got %d", result.Count)
	}
	if result.Headers[0].Number.Uint64() != 5 {
		t.Fatalf("want first header at 5, got %d", result.Headers[0].Number.Uint64())
	}
	if result.Headers[2].Number.Uint64() != 3 {
		t.Fatalf("want last header at 3, got %d", result.Headers[2].Number.Uint64())
	}
}

// TestProtocolHandler_HandleGetBlockHeaders_MaxLimit tests header count capping.
func TestProtocolHandler_HandleGetBlockHeaders_MaxLimit(t *testing.T) {
	bc := newMockBlockchain()
	tp := newMockTxPool()
	ph := NewProtocolHandler(bc, tp, 1)

	result := ph.HandleGetBlockHeaders(types.Hash{}, 0, false, MaxHeaders+100, 0, false)
	// Should not exceed MaxHeaders (capped internally).
	if result.Count > int(MaxHeaders) {
		t.Fatalf("expected at most %d headers, got %d", MaxHeaders, result.Count)
	}
}

// TestProtocolHandler_HandleGetBlockBodies tests body response.
func TestProtocolHandler_HandleGetBlockBodies(t *testing.T) {
	bc := newMockBlockchain()
	tp := newMockTxPool()
	ph := NewProtocolHandler(bc, tp, 1)

	hashes := []types.Hash{bc.genesis.Hash(), types.HexToHash("0xmissing")}
	result := ph.HandleGetBlockBodies(hashes)
	if result.Count != 2 {
		t.Fatalf("want 2 bodies (one empty), got %d", result.Count)
	}
}

// TestProtocolHandler_BestPeer tests best peer selection by TD.
func TestProtocolHandler_BestPeer(t *testing.T) {
	bc := newMockBlockchain()
	tp := newMockTxPool()
	ph := NewProtocolHandler(bc, tp, 1)

	ph.RegisterPeer("peer-1", ETH68)
	ph.RegisterPeer("peer-2", ETH68)

	// Give peer-2 higher TD.
	ps1 := ph.GetPeerState("peer-1")
	ps1.TD = big.NewInt(100)
	ps2 := ph.GetPeerState("peer-2")
	ps2.TD = big.NewInt(200)

	best := ph.BestPeer()
	if best == nil {
		t.Fatal("expected non-nil best peer")
	}
	if best.PeerID != "peer-2" {
		t.Fatalf("want peer-2 as best, got %s", best.PeerID)
	}
}

// TestProtocolHandler_Stop tests stopping the handler.
func TestProtocolHandler_Stop(t *testing.T) {
	bc := newMockBlockchain()
	tp := newMockTxPool()
	ph := NewProtocolHandler(bc, tp, 1)

	ph.Stop()
	if !ph.IsStopped() {
		t.Fatal("expected handler to be stopped")
	}

	err := ph.RegisterPeer("peer-1", ETH68)
	if !errors.Is(err, ErrProtoHandlerStopped) {
		t.Fatalf("want ErrProtoHandlerStopped, got %v", err)
	}
}

// TestPeerSyncState_BlockKnown tests the block known tracking.
func TestPeerSyncState_BlockKnown(t *testing.T) {
	ps := NewPeerSyncState("peer-1", ETH68)
	hash := types.HexToHash("0xaabbccdd")

	if ps.IsBlockKnown(hash) {
		t.Fatal("block should not be known initially")
	}
	ps.MarkBlockKnown(hash)
	if !ps.IsBlockKnown(hash) {
		t.Fatal("block should be known after marking")
	}
}

// TestPeerSyncState_SetHead tests head update.
func TestPeerSyncState_SetHead(t *testing.T) {
	ps := NewPeerSyncState("peer-1", ETH68)
	hash := types.HexToHash("0x1234")
	td := big.NewInt(42)

	ps.SetHead(hash, 100, td)
	if ps.HeadHash != hash {
		t.Fatalf("want head hash %s, got %s", hash.Hex(), ps.HeadHash.Hex())
	}
	if ps.HeadNumber != 100 {
		t.Fatalf("want head number 100, got %d", ps.HeadNumber)
	}
	if ps.TD.Cmp(td) != 0 {
		t.Fatalf("want TD %s, got %s", td, ps.TD)
	}
}
