package eth

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/p2p"
)

// mockBlockchain implements Blockchain for testing.
type mockBlockchain struct {
	blocks   map[types.Hash]*types.Block
	byNumber map[uint64]*types.Block
	current  *types.Block
	genesis  *types.Block
}

func newMockBlockchain() *mockBlockchain {
	genesis := makeTestBlock(0, types.Hash{}, nil)
	bc := &mockBlockchain{
		blocks:   make(map[types.Hash]*types.Block),
		byNumber: make(map[uint64]*types.Block),
		genesis:  genesis,
		current:  genesis,
	}
	bc.addBlock(genesis)
	return bc
}

func (bc *mockBlockchain) addBlock(b *types.Block) {
	bc.blocks[b.Hash()] = b
	bc.byNumber[b.NumberU64()] = b
	if b.NumberU64() > bc.current.NumberU64() {
		bc.current = b
	}
}

func (bc *mockBlockchain) CurrentBlock() *types.Block          { return bc.current }
func (bc *mockBlockchain) GetBlock(hash types.Hash) *types.Block { return bc.blocks[hash] }
func (bc *mockBlockchain) GetBlockByNumber(n uint64) *types.Block { return bc.byNumber[n] }
func (bc *mockBlockchain) HasBlock(hash types.Hash) bool       { _, ok := bc.blocks[hash]; return ok }
func (bc *mockBlockchain) Genesis() *types.Block               { return bc.genesis }
func (bc *mockBlockchain) InsertBlock(b *types.Block) error {
	bc.addBlock(b)
	return nil
}

// mockTxPool implements TxPool for testing.
type mockTxPool struct {
	txs map[types.Hash]*types.Transaction
}

func newMockTxPool() *mockTxPool {
	return &mockTxPool{txs: make(map[types.Hash]*types.Transaction)}
}

func (tp *mockTxPool) AddRemote(tx *types.Transaction) error {
	tp.txs[tx.Hash()] = tx
	return nil
}

func (tp *mockTxPool) Get(hash types.Hash) *types.Transaction {
	return tp.txs[hash]
}

func (tp *mockTxPool) Pending() map[types.Address][]*types.Transaction {
	return nil
}

// makeTestBlock creates a test block at the given number.
func makeTestBlock(num uint64, parent types.Hash, txs []*types.Transaction) *types.Block {
	header := &types.Header{
		Number:     new(big.Int).SetUint64(num),
		ParentHash: parent,
		Difficulty: big.NewInt(1),
		GasLimit:   30_000_000,
		Time:       1000 + num*12,
	}
	var body *types.Body
	if len(txs) > 0 {
		body = &types.Body{Transactions: txs}
	}
	return types.NewBlock(header, body)
}

func makeTestTx(nonce uint64) *types.Transaction {
	to := types.BytesToAddress([]byte{0x01})
	return types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(1_000_000_000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(1000),
	})
}

// setupHandlerPair creates two connected handlers sharing test infrastructure.
// Returns handler1, ethPeer1 (connected to handler2), handler2, ethPeer2 (connected to handler1), cleanup.
func setupHandlerPair(t *testing.T) (*Handler, *EthPeer, *Handler, *EthPeer, func()) {
	t.Helper()

	bc1 := newMockBlockchain()
	bc2 := newMockBlockchain()
	tp1 := newMockTxPool()
	tp2 := newMockTxPool()

	h1 := NewHandler(bc1, tp1, 1)
	h2 := NewHandler(bc2, tp2, 1)

	// Create a message pipe.
	end1, end2 := p2p.MsgPipe()

	peer1 := p2p.NewPeer("peer1", "127.0.0.1:30303", nil)
	peer2 := p2p.NewPeer("peer2", "127.0.0.1:30304", nil)

	ep1 := NewEthPeer(peer1, end1)
	ep2 := NewEthPeer(peer2, end2)

	_ = h1
	_ = h2

	cleanup := func() {
		end1.Close()
		end2.Close()
	}

	return h1, ep1, h2, ep2, cleanup
}

func TestHandler_StatusExchange(t *testing.T) {
	bc1 := newMockBlockchain()
	bc2 := newMockBlockchain()
	tp1 := newMockTxPool()
	tp2 := newMockTxPool()

	_ = NewHandler(bc1, tp1, 1)
	_ = NewHandler(bc2, tp2, 1)

	end1, end2 := p2p.MsgPipe()
	defer end1.Close()
	defer end2.Close()

	peer1 := p2p.NewPeer("peer1", "127.0.0.1:30303", nil)
	peer2 := p2p.NewPeer("peer2", "127.0.0.1:30304", nil)

	ep1 := NewEthPeer(peer1, end1)
	ep2 := NewEthPeer(peer2, end2)

	genesis := bc1.Genesis()

	localStatus := &p2p.StatusData{
		ProtocolVersion: ETH68,
		NetworkID:       1,
		TD:              big.NewInt(1),
		Head:            genesis.Hash(),
		Genesis:         genesis.Hash(),
	}

	// Run handshake concurrently from both sides.
	errCh := make(chan error, 2)

	go func() {
		_, err := ep1.Handshake(localStatus)
		errCh <- err
	}()
	go func() {
		_, err := ep2.Handshake(localStatus)
		errCh <- err
	}()

	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("handshake error: %v", err)
		}
	}

	// Both peers should have updated head info.
	if peer1.Head() != genesis.Hash() {
		t.Errorf("peer1 head = %s, want %s", peer1.Head().Hex(), genesis.Hash().Hex())
	}
	if peer2.Head() != genesis.Hash() {
		t.Errorf("peer2 head = %s, want %s", peer2.Head().Hex(), genesis.Hash().Hex())
	}
	if peer1.Version() != ETH68 {
		t.Errorf("peer1 version = %d, want %d", peer1.Version(), ETH68)
	}
}

func TestHandler_StatusExchange_NetworkMismatch(t *testing.T) {
	end1, end2 := p2p.MsgPipe()
	defer end1.Close()
	defer end2.Close()

	bc := newMockBlockchain()
	genesis := bc.Genesis()

	peer1 := p2p.NewPeer("peer1", "127.0.0.1:30303", nil)
	peer2 := p2p.NewPeer("peer2", "127.0.0.1:30304", nil)

	ep1 := NewEthPeer(peer1, end1)
	ep2 := NewEthPeer(peer2, end2)

	status1 := &p2p.StatusData{
		ProtocolVersion: ETH68,
		NetworkID:       1,
		TD:              big.NewInt(1),
		Head:            genesis.Hash(),
		Genesis:         genesis.Hash(),
	}
	status2 := &p2p.StatusData{
		ProtocolVersion: ETH68,
		NetworkID:       5, // different network
		TD:              big.NewInt(1),
		Head:            genesis.Hash(),
		Genesis:         genesis.Hash(),
	}

	errCh := make(chan error, 2)
	go func() { _, err := ep1.Handshake(status1); errCh <- err }()
	go func() { _, err := ep2.Handshake(status2); errCh <- err }()

	// At least one side should fail with network mismatch.
	var gotMismatch bool
	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			gotMismatch = true
		}
	}
	if !gotMismatch {
		t.Fatal("expected network mismatch error, got none")
	}
}

func TestHandler_GetBlockHeaders(t *testing.T) {
	bc := newMockBlockchain()

	// Build a small chain: genesis -> block1 -> block2 -> block3.
	genesis := bc.Genesis()
	block1 := makeTestBlock(1, genesis.Hash(), nil)
	block2 := makeTestBlock(2, block1.Hash(), nil)
	block3 := makeTestBlock(3, block2.Hash(), nil)
	bc.addBlock(block1)
	bc.addBlock(block2)
	bc.addBlock(block3)

	handler := NewHandler(bc, newMockTxPool(), 1)

	end1, end2 := p2p.MsgPipe()
	defer end1.Close()
	defer end2.Close()

	peer := p2p.NewPeer("test-peer", "127.0.0.1:30303", nil)
	ep := NewEthPeer(peer, end1)

	// Send GetBlockHeaders request from the "remote" side.
	reqPkt := &p2p.GetBlockHeadersPacket{
		RequestID: 42,
		Request: p2p.GetBlockHeadersRequest{
			Origin:  p2p.HashOrNumber{Number: 0},
			Amount:  4,
			Skip:    0,
			Reverse: false,
		},
	}

	// Encode and send the request.
	reqMsg, err := p2p.EncodeMessage(p2p.GetBlockHeadersMsg, reqPkt)
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}

	go func() {
		end2.WriteMsg(p2p.Msg{Code: reqMsg.Code, Size: reqMsg.Size, Payload: reqMsg.Payload})
	}()

	// Read the message on the handler side and process it.
	msg, err := ep.transport.ReadMsg()
	if err != nil {
		t.Fatalf("read msg: %v", err)
	}
	if err := handler.HandleMsg(ep, msg); err != nil {
		t.Fatalf("handle msg: %v", err)
	}

	// Read the response on the "remote" side.
	resp, err := end2.ReadMsg()
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.Code != p2p.BlockHeadersMsg {
		t.Fatalf("expected BlockHeadersMsg (0x%02x), got 0x%02x", p2p.BlockHeadersMsg, resp.Code)
	}

	var pkt p2p.BlockHeadersPacket
	if err := p2p.DecodeMessage(p2p.Message{Code: resp.Code, Size: resp.Size, Payload: resp.Payload}, &pkt); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if pkt.RequestID != 42 {
		t.Errorf("request ID = %d, want 42", pkt.RequestID)
	}
	if len(pkt.Headers) != 4 {
		t.Fatalf("got %d headers, want 4", len(pkt.Headers))
	}

	for i, h := range pkt.Headers {
		if h.Number.Uint64() != uint64(i) {
			t.Errorf("header[%d].Number = %d, want %d", i, h.Number.Uint64(), i)
		}
	}
}

func TestHandler_GetBlockHeaders_Reverse(t *testing.T) {
	bc := newMockBlockchain()
	genesis := bc.Genesis()
	block1 := makeTestBlock(1, genesis.Hash(), nil)
	block2 := makeTestBlock(2, block1.Hash(), nil)
	bc.addBlock(block1)
	bc.addBlock(block2)

	handler := NewHandler(bc, newMockTxPool(), 1)

	end1, end2 := p2p.MsgPipe()
	defer end1.Close()
	defer end2.Close()

	peer := p2p.NewPeer("test-peer", "127.0.0.1:30303", nil)
	ep := NewEthPeer(peer, end1)

	reqPkt := &p2p.GetBlockHeadersPacket{
		RequestID: 7,
		Request: p2p.GetBlockHeadersRequest{
			Origin:  p2p.HashOrNumber{Number: 2},
			Amount:  3,
			Skip:    0,
			Reverse: true,
		},
	}

	reqMsg, err := p2p.EncodeMessage(p2p.GetBlockHeadersMsg, reqPkt)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	go func() {
		end2.WriteMsg(p2p.Msg{Code: reqMsg.Code, Size: reqMsg.Size, Payload: reqMsg.Payload})
	}()

	msg, _ := ep.transport.ReadMsg()
	if err := handler.HandleMsg(ep, msg); err != nil {
		t.Fatalf("handle: %v", err)
	}

	resp, _ := end2.ReadMsg()
	var pkt p2p.BlockHeadersPacket
	p2p.DecodeMessage(p2p.Message{Code: resp.Code, Size: resp.Size, Payload: resp.Payload}, &pkt)

	if len(pkt.Headers) != 3 {
		t.Fatalf("got %d headers, want 3", len(pkt.Headers))
	}
	// Should be in reverse order: 2, 1, 0.
	expected := []uint64{2, 1, 0}
	for i, h := range pkt.Headers {
		if h.Number.Uint64() != expected[i] {
			t.Errorf("header[%d].Number = %d, want %d", i, h.Number.Uint64(), expected[i])
		}
	}
}

func TestHandler_GetBlockBodies(t *testing.T) {
	bc := newMockBlockchain()
	genesis := bc.Genesis()

	tx1 := makeTestTx(0)
	tx2 := makeTestTx(1)
	block1 := makeTestBlock(1, genesis.Hash(), []*types.Transaction{tx1, tx2})
	bc.addBlock(block1)

	handler := NewHandler(bc, newMockTxPool(), 1)

	end1, end2 := p2p.MsgPipe()
	defer end1.Close()
	defer end2.Close()

	peer := p2p.NewPeer("test-peer", "127.0.0.1:30303", nil)
	ep := NewEthPeer(peer, end1)

	reqPkt := &p2p.GetBlockBodiesPacket{
		RequestID: 99,
		Hashes:    p2p.GetBlockBodiesRequest{block1.Hash()},
	}

	reqMsg, err := p2p.EncodeMessage(p2p.GetBlockBodiesMsg, reqPkt)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	go func() {
		end2.WriteMsg(p2p.Msg{Code: reqMsg.Code, Size: reqMsg.Size, Payload: reqMsg.Payload})
	}()

	msg, _ := ep.transport.ReadMsg()
	if err := handler.HandleMsg(ep, msg); err != nil {
		t.Fatalf("handle: %v", err)
	}

	resp, _ := end2.ReadMsg()
	if resp.Code != p2p.BlockBodiesMsg {
		t.Fatalf("expected BlockBodiesMsg, got 0x%02x", resp.Code)
	}

	var pkt p2p.BlockBodiesPacket
	if err := p2p.DecodeMessage(p2p.Message{Code: resp.Code, Size: resp.Size, Payload: resp.Payload}, &pkt); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if pkt.RequestID != 99 {
		t.Errorf("request ID = %d, want 99", pkt.RequestID)
	}
	if len(pkt.Bodies) != 1 {
		t.Fatalf("got %d bodies, want 1", len(pkt.Bodies))
	}
	if len(pkt.Bodies[0].Transactions) != 2 {
		t.Errorf("body has %d txs, want 2", len(pkt.Bodies[0].Transactions))
	}
}

func TestHandler_NewBlockHashes(t *testing.T) {
	bc := newMockBlockchain()
	handler := NewHandler(bc, newMockTxPool(), 1)

	end1, end2 := p2p.MsgPipe()
	defer end1.Close()
	defer end2.Close()

	peer := p2p.NewPeer("test-peer", "127.0.0.1:30303", nil)
	ep := NewEthPeer(peer, end1)

	entries := []p2p.NewBlockHashesEntry{
		{Hash: types.BytesToHash([]byte{0xaa}), Number: 100},
		{Hash: types.BytesToHash([]byte{0xbb}), Number: 101},
	}

	encoded, err := p2p.EncodeMessage(p2p.NewBlockHashesMsg, entries)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	go func() {
		end2.WriteMsg(p2p.Msg{Code: encoded.Code, Size: encoded.Size, Payload: encoded.Payload})
	}()

	msg, _ := ep.transport.ReadMsg()
	if err := handler.HandleMsg(ep, msg); err != nil {
		t.Fatalf("handle: %v", err)
	}
	// No error means announcements were processed without issue.
}

func TestHandler_Transactions(t *testing.T) {
	bc := newMockBlockchain()
	tp := newMockTxPool()
	handler := NewHandler(bc, tp, 1)

	end1, end2 := p2p.MsgPipe()
	defer end1.Close()
	defer end2.Close()

	peer := p2p.NewPeer("test-peer", "127.0.0.1:30303", nil)
	ep := NewEthPeer(peer, end1)

	tx1 := makeTestTx(0)
	tx2 := makeTestTx(1)
	txs := []*types.Transaction{tx1, tx2}

	txMsg, err := encodeTransactions(txs)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	go func() {
		end2.WriteMsg(txMsg)
	}()

	msg, _ := ep.transport.ReadMsg()
	if err := handler.HandleMsg(ep, msg); err != nil {
		t.Fatalf("handle: %v", err)
	}

	// Verify transactions were added to the pool.
	if tp.Get(tx1.Hash()) == nil {
		t.Error("tx1 not found in pool")
	}
	if tp.Get(tx2.Hash()) == nil {
		t.Error("tx2 not found in pool")
	}
}

func TestHandler_NewBlock(t *testing.T) {
	bc := newMockBlockchain()
	handler := NewHandler(bc, newMockTxPool(), 1)

	end1, end2 := p2p.MsgPipe()
	defer end1.Close()
	defer end2.Close()

	peer := p2p.NewPeer("test-peer", "127.0.0.1:30303", nil)
	ep := NewEthPeer(peer, end1)

	genesis := bc.Genesis()
	newBlock := makeTestBlock(1, genesis.Hash(), nil)
	td := big.NewInt(2)

	blockMsg, err := encodeNewBlock(&p2p.NewBlockData{Block: newBlock, TD: td})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	go func() {
		end2.WriteMsg(blockMsg)
	}()

	msg, _ := ep.transport.ReadMsg()
	if err := handler.HandleMsg(ep, msg); err != nil {
		t.Fatalf("handle: %v", err)
	}

	// Block should be in the chain.
	if !bc.HasBlock(newBlock.Hash()) {
		t.Error("new block not inserted into chain")
	}

	// Peer head should be updated.
	if peer.Head() != newBlock.Hash() {
		t.Errorf("peer head = %s, want %s", peer.Head().Hex(), newBlock.Hash().Hex())
	}
}

func TestHandler_UnknownMessage(t *testing.T) {
	bc := newMockBlockchain()
	handler := NewHandler(bc, newMockTxPool(), 1)

	end1, end2 := p2p.MsgPipe()
	defer end1.Close()
	defer end2.Close()

	peer := p2p.NewPeer("test-peer", "127.0.0.1:30303", nil)
	ep := NewEthPeer(peer, end1)

	// Send a message with unknown code.
	go func() {
		end2.WriteMsg(p2p.Msg{Code: 0xFF, Payload: []byte{}})
	}()

	msg, _ := ep.transport.ReadMsg()
	err := handler.HandleMsg(ep, msg)
	if err != nil {
		t.Errorf("unknown message should not error, got: %v", err)
	}
}

func TestHandler_GetBlockBodies_UnknownHash(t *testing.T) {
	bc := newMockBlockchain()
	handler := NewHandler(bc, newMockTxPool(), 1)

	end1, end2 := p2p.MsgPipe()
	defer end1.Close()
	defer end2.Close()

	peer := p2p.NewPeer("test-peer", "127.0.0.1:30303", nil)
	ep := NewEthPeer(peer, end1)

	// Request bodies for a hash that doesn't exist.
	reqPkt := &p2p.GetBlockBodiesPacket{
		RequestID: 55,
		Hashes:    p2p.GetBlockBodiesRequest{types.BytesToHash([]byte{0xde, 0xad})},
	}
	reqMsg, _ := p2p.EncodeMessage(p2p.GetBlockBodiesMsg, reqPkt)

	go func() {
		end2.WriteMsg(p2p.Msg{Code: reqMsg.Code, Size: reqMsg.Size, Payload: reqMsg.Payload})
	}()

	msg, _ := ep.transport.ReadMsg()
	if err := handler.HandleMsg(ep, msg); err != nil {
		t.Fatalf("handle: %v", err)
	}

	resp, _ := end2.ReadMsg()
	var pkt p2p.BlockBodiesPacket
	p2p.DecodeMessage(p2p.Message{Code: resp.Code, Size: resp.Size, Payload: resp.Payload}, &pkt)

	if pkt.RequestID != 55 {
		t.Errorf("request ID = %d, want 55", pkt.RequestID)
	}
	// Should get empty bodies since the hash doesn't exist.
	if len(pkt.Bodies) != 0 {
		t.Errorf("got %d bodies, want 0", len(pkt.Bodies))
	}
}
