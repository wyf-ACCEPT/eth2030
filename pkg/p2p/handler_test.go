package p2p

import (
	"math/big"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/rlp"
)

// ---------------------------------------------------------------------------
// Mock backend for testing handlers
// ---------------------------------------------------------------------------

type mockBackend struct {
	headers   map[types.Hash]*types.Header
	byNumber  map[uint64]*types.Header
	bodies    map[types.Hash]*BlockBody
	receipts  map[types.Hash][]*types.Receipt
	pooledTxs map[types.Hash]*types.Transaction

	// Recorded broadcast calls.
	newBlocks      []newBlockCall
	newBlockHashes [][]NewBlockHashesEntry
	transactions   [][]*types.Transaction
	pooledTxHashes []pooledTxHashCall
}

type newBlockCall struct {
	Peer  *Peer
	Block *types.Block
	TD    *big.Int
}

type pooledTxHashCall struct {
	Peer   *Peer
	Types  []byte
	Sizes  []uint32
	Hashes []types.Hash
}

func newMockBackend() *mockBackend {
	return &mockBackend{
		headers:   make(map[types.Hash]*types.Header),
		byNumber:  make(map[uint64]*types.Header),
		bodies:    make(map[types.Hash]*BlockBody),
		receipts:  make(map[types.Hash][]*types.Receipt),
		pooledTxs: make(map[types.Hash]*types.Transaction),
	}
}

func (m *mockBackend) GetHeaderByHash(hash types.Hash) *types.Header {
	return m.headers[hash]
}

func (m *mockBackend) GetHeaderByNumber(number uint64) *types.Header {
	return m.byNumber[number]
}

func (m *mockBackend) GetBlockBody(hash types.Hash) *BlockBody {
	return m.bodies[hash]
}

func (m *mockBackend) GetReceipts(hash types.Hash) []*types.Receipt {
	return m.receipts[hash]
}

func (m *mockBackend) GetPooledTransaction(hash types.Hash) *types.Transaction {
	return m.pooledTxs[hash]
}

func (m *mockBackend) HandleNewBlock(peer *Peer, block *types.Block, td *big.Int) {
	m.newBlocks = append(m.newBlocks, newBlockCall{Peer: peer, Block: block, TD: td})
}

func (m *mockBackend) HandleNewBlockHashes(peer *Peer, hashes []NewBlockHashesEntry) {
	m.newBlockHashes = append(m.newBlockHashes, hashes)
}

func (m *mockBackend) HandleTransactions(peer *Peer, txs []*types.Transaction) {
	m.transactions = append(m.transactions, txs)
}

func (m *mockBackend) HandleNewPooledTransactionHashes(peer *Peer, txTypes []byte, sizes []uint32, hashes []types.Hash) {
	m.pooledTxHashes = append(m.pooledTxHashes, pooledTxHashCall{
		Peer: peer, Types: txTypes, Sizes: sizes, Hashes: hashes,
	})
}

// addHeaders inserts a contiguous range of headers into the mock backend.
func (m *mockBackend) addHeaders(start, count uint64) []*types.Header {
	headers := make([]*types.Header, count)
	for i := uint64(0); i < count; i++ {
		num := start + i
		h := &types.Header{
			Number:     new(big.Int).SetUint64(num),
			Difficulty: big.NewInt(1),
			GasLimit:   30_000_000,
		}
		headers[i] = h
		hash := h.Hash()
		m.headers[hash] = h
		m.byNumber[num] = h
	}
	return headers
}

func testPeer() *Peer {
	return NewPeer("test-peer", "127.0.0.1:30303", nil)
}

// ---------------------------------------------------------------------------
// Handler registry tests
// ---------------------------------------------------------------------------

func TestHandlerRegistry_DefaultHandlers(t *testing.T) {
	reg := NewHandlerRegistry()

	codes := []uint64{
		GetBlockHeadersMsg, BlockHeadersMsg,
		GetBlockBodiesMsg, BlockBodiesMsg,
		GetReceiptsMsg, ReceiptsMsg,
		GetPooledTransactionsMsg, PooledTransactionsMsg,
		NewBlockHashesMsg, NewBlockMsg,
		TransactionsMsg, NewPooledTransactionHashesMsg,
	}
	for _, code := range codes {
		if h := reg.Lookup(code); h == nil {
			t.Errorf("no handler registered for 0x%02x (%s)", code, MessageName(code))
		}
	}
}

func TestHandlerRegistry_UnknownCode(t *testing.T) {
	reg := NewHandlerRegistry()
	peer := testPeer()
	msg := Message{Code: 0xFF}
	err := reg.Handle(newMockBackend(), peer, msg)
	if err == nil {
		t.Fatal("expected error for unknown code")
	}
}

func TestHandlerRegistry_CustomHandler(t *testing.T) {
	reg := NewHandlerRegistry()
	called := false
	reg.Register(StatusMsg, func(_ Backend, _ *Peer, _ Message) error {
		called = true
		return nil
	})
	err := reg.Handle(nil, testPeer(), Message{Code: StatusMsg})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if !called {
		t.Error("custom handler was not called")
	}
}

// ---------------------------------------------------------------------------
// GetBlockHeaders handler tests
// ---------------------------------------------------------------------------

func TestHandleGetBlockHeaders_ByNumber(t *testing.T) {
	backend := newMockBackend()
	backend.addHeaders(0, 10) // blocks 0-9

	peer := testPeer()
	pkt := GetBlockHeadersPacket{
		RequestID: 1,
		Request: GetBlockHeadersRequest{
			Origin: HashOrNumber{Number: 3},
			Amount: 4,
			Skip:   0,
		},
	}
	msg, err := EncodeMessage(GetBlockHeadersMsg, pkt)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	reg := NewHandlerRegistry()
	if err := reg.Handle(backend, peer, msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	resp := peer.LastResponse(BlockHeadersMsg)
	if resp == nil {
		t.Fatal("no response stored")
	}
	bhp, ok := resp.(BlockHeadersPacket)
	if !ok {
		t.Fatalf("response type: %T", resp)
	}
	if bhp.RequestID != 1 {
		t.Errorf("RequestID = %d, want 1", bhp.RequestID)
	}
	if len(bhp.Headers) != 4 {
		t.Fatalf("got %d headers, want 4", len(bhp.Headers))
	}
	for i, h := range bhp.Headers {
		want := uint64(3 + i)
		if h.Number.Uint64() != want {
			t.Errorf("header[%d].Number = %d, want %d", i, h.Number.Uint64(), want)
		}
	}
}

func TestHandleGetBlockHeaders_ByHash(t *testing.T) {
	backend := newMockBackend()
	headers := backend.addHeaders(0, 10)
	// Request by hash of block 5.
	originHash := headers[5].Hash()

	peer := testPeer()
	pkt := GetBlockHeadersPacket{
		RequestID: 2,
		Request: GetBlockHeadersRequest{
			Origin: HashOrNumber{Hash: originHash},
			Amount: 3,
			Skip:   0,
		},
	}
	msg, err := EncodeMessage(GetBlockHeadersMsg, pkt)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	reg := NewHandlerRegistry()
	if err := reg.Handle(backend, peer, msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	bhp := peer.LastResponse(BlockHeadersMsg).(BlockHeadersPacket)
	if len(bhp.Headers) != 3 {
		t.Fatalf("got %d headers, want 3", len(bhp.Headers))
	}
	if bhp.Headers[0].Number.Uint64() != 5 {
		t.Errorf("first header number = %d, want 5", bhp.Headers[0].Number.Uint64())
	}
}

func TestHandleGetBlockHeaders_Reverse(t *testing.T) {
	backend := newMockBackend()
	backend.addHeaders(0, 10)

	peer := testPeer()
	pkt := GetBlockHeadersPacket{
		RequestID: 3,
		Request: GetBlockHeadersRequest{
			Origin:  HashOrNumber{Number: 8},
			Amount:  4,
			Skip:    0,
			Reverse: true,
		},
	}
	msg, err := EncodeMessage(GetBlockHeadersMsg, pkt)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	reg := NewHandlerRegistry()
	if err := reg.Handle(backend, peer, msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	bhp := peer.LastResponse(BlockHeadersMsg).(BlockHeadersPacket)
	if len(bhp.Headers) != 4 {
		t.Fatalf("got %d headers, want 4", len(bhp.Headers))
	}
	// Expect: 8, 7, 6, 5
	expected := []uint64{8, 7, 6, 5}
	for i, h := range bhp.Headers {
		if h.Number.Uint64() != expected[i] {
			t.Errorf("header[%d] = %d, want %d", i, h.Number.Uint64(), expected[i])
		}
	}
}

func TestHandleGetBlockHeaders_WithSkip(t *testing.T) {
	backend := newMockBackend()
	backend.addHeaders(0, 20)

	peer := testPeer()
	pkt := GetBlockHeadersPacket{
		RequestID: 4,
		Request: GetBlockHeadersRequest{
			Origin: HashOrNumber{Number: 0},
			Amount: 5,
			Skip:   2, // every 3rd block
		},
	}
	msg, err := EncodeMessage(GetBlockHeadersMsg, pkt)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	reg := NewHandlerRegistry()
	if err := reg.Handle(backend, peer, msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	bhp := peer.LastResponse(BlockHeadersMsg).(BlockHeadersPacket)
	if len(bhp.Headers) != 5 {
		t.Fatalf("got %d headers, want 5", len(bhp.Headers))
	}
	// Expect: 0, 3, 6, 9, 12
	expected := []uint64{0, 3, 6, 9, 12}
	for i, h := range bhp.Headers {
		if h.Number.Uint64() != expected[i] {
			t.Errorf("header[%d] = %d, want %d", i, h.Number.Uint64(), expected[i])
		}
	}
}

func TestHandleGetBlockHeaders_UnknownOrigin(t *testing.T) {
	backend := newMockBackend()
	peer := testPeer()

	pkt := GetBlockHeadersPacket{
		RequestID: 5,
		Request: GetBlockHeadersRequest{
			Origin: HashOrNumber{Number: 999},
			Amount: 10,
		},
	}
	msg, err := EncodeMessage(GetBlockHeadersMsg, pkt)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	reg := NewHandlerRegistry()
	if err := reg.Handle(backend, peer, msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	bhp := peer.LastResponse(BlockHeadersMsg).(BlockHeadersPacket)
	if len(bhp.Headers) != 0 {
		t.Errorf("got %d headers, want 0 for unknown origin", len(bhp.Headers))
	}
}

func TestHandleGetBlockHeaders_AmountCap(t *testing.T) {
	backend := newMockBackend()
	backend.addHeaders(0, 10)

	peer := testPeer()
	pkt := GetBlockHeadersPacket{
		RequestID: 6,
		Request: GetBlockHeadersRequest{
			Origin: HashOrNumber{Number: 0},
			Amount: MaxHeadersServe + 100, // exceeds cap
		},
	}
	msg, err := EncodeMessage(GetBlockHeadersMsg, pkt)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	reg := NewHandlerRegistry()
	if err := reg.Handle(backend, peer, msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	bhp := peer.LastResponse(BlockHeadersMsg).(BlockHeadersPacket)
	// We only have 10 headers, but the cap should be applied first.
	if len(bhp.Headers) > 10 {
		t.Errorf("got %d headers, expected at most 10 (limited by available data)", len(bhp.Headers))
	}
}

func TestHandleGetBlockHeaders_NilBackend(t *testing.T) {
	peer := testPeer()
	pkt := GetBlockHeadersPacket{
		RequestID: 7,
		Request: GetBlockHeadersRequest{
			Origin: HashOrNumber{Number: 0},
			Amount: 1,
		},
	}
	msg, err := EncodeMessage(GetBlockHeadersMsg, pkt)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	reg := NewHandlerRegistry()
	err = reg.Handle(nil, peer, msg)
	if err != ErrNilBackend {
		t.Errorf("expected ErrNilBackend, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// BlockHeaders response handler tests
// ---------------------------------------------------------------------------

func TestHandleBlockHeaders(t *testing.T) {
	peer := testPeer()
	pkt := BlockHeadersPacket{
		RequestID: 42,
		Headers: []*types.Header{
			{Number: big.NewInt(100), Difficulty: big.NewInt(1)},
		},
	}
	msg, err := EncodeMessage(BlockHeadersMsg, pkt)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	reg := NewHandlerRegistry()
	if err := reg.Handle(nil, peer, msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	resp, ok := peer.GetDeliveredResponse(42)
	if !ok {
		t.Fatal("no delivered response for request ID 42")
	}
	bhp, ok := resp.(*BlockHeadersPacket)
	if !ok {
		t.Fatalf("response type: %T", resp)
	}
	if bhp.RequestID != 42 {
		t.Errorf("RequestID = %d, want 42", bhp.RequestID)
	}
	if len(bhp.Headers) != 1 {
		t.Errorf("headers count = %d, want 1", len(bhp.Headers))
	}
}

// ---------------------------------------------------------------------------
// GetBlockBodies handler tests
// ---------------------------------------------------------------------------

func TestHandleGetBlockBodies(t *testing.T) {
	backend := newMockBackend()
	h1 := types.HexToHash("1111")
	h2 := types.HexToHash("2222")
	h3 := types.HexToHash("3333") // not in backend

	backend.bodies[h1] = &BlockBody{
		Transactions: []*types.Transaction{},
		Uncles:       []*types.Header{},
	}
	backend.bodies[h2] = &BlockBody{
		Transactions: []*types.Transaction{},
		Uncles: []*types.Header{
			{Number: big.NewInt(99), Difficulty: big.NewInt(1)},
		},
	}

	peer := testPeer()
	pkt := GetBlockBodiesPacket{
		RequestID: 10,
		Hashes:    GetBlockBodiesRequest{h1, h2, h3},
	}
	msg, err := EncodeMessage(GetBlockBodiesMsg, pkt)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	reg := NewHandlerRegistry()
	if err := reg.Handle(backend, peer, msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	resp := peer.LastResponse(BlockBodiesMsg)
	if resp == nil {
		t.Fatal("no response stored")
	}
	bbp, ok := resp.(BlockBodiesPacket)
	if !ok {
		t.Fatalf("response type: %T", resp)
	}
	if bbp.RequestID != 10 {
		t.Errorf("RequestID = %d, want 10", bbp.RequestID)
	}
	// h3 not found, so only 2 bodies.
	if len(bbp.Bodies) != 2 {
		t.Fatalf("got %d bodies, want 2", len(bbp.Bodies))
	}
}

func TestHandleBlockBodies_Response(t *testing.T) {
	peer := testPeer()
	pkt := BlockBodiesPacket{
		RequestID: 20,
		Bodies: []*BlockBody{
			{Transactions: nil, Uncles: nil},
		},
	}
	msg, err := EncodeMessage(BlockBodiesMsg, pkt)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	reg := NewHandlerRegistry()
	if err := reg.Handle(nil, peer, msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	resp, ok := peer.GetDeliveredResponse(20)
	if !ok {
		t.Fatal("no delivered response for request ID 20")
	}
	bbp := resp.(*BlockBodiesPacket)
	if bbp.RequestID != 20 {
		t.Errorf("RequestID = %d, want 20", bbp.RequestID)
	}
}

// ---------------------------------------------------------------------------
// GetReceipts handler tests
// ---------------------------------------------------------------------------

func TestHandleGetReceipts(t *testing.T) {
	backend := newMockBackend()
	h1 := types.HexToHash("aaaa")
	h2 := types.HexToHash("bbbb")

	backend.receipts[h1] = []*types.Receipt{
		types.NewReceipt(types.ReceiptStatusSuccessful, 21000),
	}
	// h2 not in backend.

	peer := testPeer()
	pkt := GetReceiptsPacket{
		RequestID: 30,
		Hashes:    GetReceiptsRequest{h1, h2},
	}
	msg, err := EncodeMessage(GetReceiptsMsg, pkt)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	reg := NewHandlerRegistry()
	if err := reg.Handle(backend, peer, msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	resp := peer.LastResponse(ReceiptsMsg).(ReceiptsPacket)
	if resp.RequestID != 30 {
		t.Errorf("RequestID = %d, want 30", resp.RequestID)
	}
	// Only h1 found.
	if len(resp.Receipts) != 1 {
		t.Fatalf("got %d receipt sets, want 1", len(resp.Receipts))
	}
	if len(resp.Receipts[0]) != 1 {
		t.Fatalf("got %d receipts in set 0, want 1", len(resp.Receipts[0]))
	}
	if resp.Receipts[0][0].Status != types.ReceiptStatusSuccessful {
		t.Error("expected successful receipt")
	}
}

func TestHandleReceipts_Response(t *testing.T) {
	peer := testPeer()
	pkt := ReceiptsPacket{
		RequestID: 31,
		Receipts:  [][]*types.Receipt{{}},
	}
	msg, err := EncodeMessage(ReceiptsMsg, pkt)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	reg := NewHandlerRegistry()
	if err := reg.Handle(nil, peer, msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	resp, ok := peer.GetDeliveredResponse(31)
	if !ok {
		t.Fatal("no delivered response for request ID 31")
	}
	rp := resp.(*ReceiptsPacket)
	if rp.RequestID != 31 {
		t.Errorf("RequestID = %d, want 31", rp.RequestID)
	}
}

// ---------------------------------------------------------------------------
// GetPooledTransactions handler tests
// ---------------------------------------------------------------------------

func TestHandleGetPooledTransactions(t *testing.T) {
	backend := newMockBackend()
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(1000),
		Gas:      21000,
		Value:    big.NewInt(0),
	})
	txHash := tx.Hash()
	backend.pooledTxs[txHash] = tx

	missingHash := types.HexToHash("dead")

	peer := testPeer()
	pkt := GetPooledTransactionsPacket{
		RequestID: 40,
		Hashes:    GetPooledTransactionsRequest{txHash, missingHash},
	}
	msg, err := EncodeMessage(GetPooledTransactionsMsg, pkt)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	reg := NewHandlerRegistry()
	if err := reg.Handle(backend, peer, msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	resp := peer.LastResponse(PooledTransactionsMsg).(PooledTransactionsPacket)
	if resp.RequestID != 40 {
		t.Errorf("RequestID = %d, want 40", resp.RequestID)
	}
	if len(resp.Transactions) != 1 {
		t.Fatalf("got %d txs, want 1", len(resp.Transactions))
	}
}

func TestHandlePooledTransactions_Response(t *testing.T) {
	peer := testPeer()
	pkt := PooledTransactionsPacket{
		RequestID:    41,
		Transactions: []*types.Transaction{},
	}
	msg, err := EncodeMessage(PooledTransactionsMsg, pkt)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	reg := NewHandlerRegistry()
	if err := reg.Handle(nil, peer, msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	resp, ok := peer.GetDeliveredResponse(41)
	if !ok {
		t.Fatal("no delivered response for request ID 41")
	}
	ptp := resp.(*PooledTransactionsPacket)
	if ptp.RequestID != 41 {
		t.Errorf("RequestID = %d, want 41", ptp.RequestID)
	}
}

// ---------------------------------------------------------------------------
// Broadcast: NewBlockHashes
// ---------------------------------------------------------------------------

func TestHandleNewBlockHashes(t *testing.T) {
	backend := newMockBackend()
	peer := testPeer()

	entries := []NewBlockHashesEntry{
		{Hash: types.HexToHash("aa"), Number: 50},
		{Hash: types.HexToHash("bb"), Number: 100},
	}
	msg, err := EncodeMessage(NewBlockHashesMsg, entries)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	reg := NewHandlerRegistry()
	if err := reg.Handle(backend, peer, msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	// Backend should have been called.
	if len(backend.newBlockHashes) != 1 {
		t.Fatalf("expected 1 HandleNewBlockHashes call, got %d", len(backend.newBlockHashes))
	}
	if len(backend.newBlockHashes[0]) != 2 {
		t.Errorf("expected 2 entries, got %d", len(backend.newBlockHashes[0]))
	}

	// Peer's head number should be updated to the highest.
	if peer.HeadNumber() != 100 {
		t.Errorf("peer.HeadNumber() = %d, want 100", peer.HeadNumber())
	}
}

// ---------------------------------------------------------------------------
// Broadcast: NewBlock
// ---------------------------------------------------------------------------

// TestHandleNewBlock tests the handleNewBlock handler using a custom-encoded
// message. Block types use custom RLP encoding that the generic rlp.EncodeToBytes
// does not support, so we build the message payload manually.
func TestHandleNewBlock(t *testing.T) {
	backend := newMockBackend()
	peer := testPeer()

	header := &types.Header{
		Number:     big.NewInt(200),
		Difficulty: big.NewInt(0),
		GasLimit:   30_000_000,
	}
	block := types.NewBlock(header, nil)
	td := big.NewInt(99999)

	// Build the message payload using Block's custom EncodeRLP + manual wrapping.
	msg := encodeNewBlockMsg(t, block, td)

	reg := NewHandlerRegistry()
	if err := reg.Handle(backend, peer, msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(backend.newBlocks) != 1 {
		t.Fatalf("expected 1 HandleNewBlock call, got %d", len(backend.newBlocks))
	}
	if backend.newBlocks[0].Block.NumberU64() != 200 {
		t.Errorf("block number = %d, want 200", backend.newBlocks[0].Block.NumberU64())
	}
	if peer.HeadNumber() != 200 {
		t.Errorf("peer.HeadNumber() = %d, want 200", peer.HeadNumber())
	}
	if peer.TD().Cmp(td) != 0 {
		t.Errorf("peer.TD() = %v, want %v", peer.TD(), td)
	}
}

// encodeNewBlockMsg manually constructs a Message for NewBlock by using the
// Block's custom EncodeRLP method and combining it with the TD field.
func encodeNewBlockMsg(t *testing.T, block *types.Block, td *big.Int) Message {
	t.Helper()

	blockRLP, err := block.EncodeRLP()
	if err != nil {
		t.Fatalf("block.EncodeRLP: %v", err)
	}
	tdRLP, err := rlp.EncodeToBytes(td)
	if err != nil {
		t.Fatalf("encode TD: %v", err)
	}

	// NewBlockData is an RLP list: [block_rlp, td_rlp]
	var payload []byte
	payload = append(payload, blockRLP...)
	payload = append(payload, tdRLP...)
	wrapped := rlp.WrapList(payload)

	return Message{
		Code:    NewBlockMsg,
		Size:    uint32(len(wrapped)),
		Payload: wrapped,
	}
}

// ---------------------------------------------------------------------------
// Broadcast: Transactions
// ---------------------------------------------------------------------------

func TestHandleTransactions(t *testing.T) {
	backend := newMockBackend()
	peer := testPeer()

	txs := []*types.Transaction{
		types.NewTransaction(&types.LegacyTx{
			Nonce:    0,
			GasPrice: big.NewInt(100),
			Gas:      21000,
			Value:    big.NewInt(0),
		}),
	}
	msg, err := EncodeMessage(TransactionsMsg, txs)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	reg := NewHandlerRegistry()
	if err := reg.Handle(backend, peer, msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(backend.transactions) != 1 {
		t.Fatalf("expected 1 HandleTransactions call, got %d", len(backend.transactions))
	}
	if len(backend.transactions[0]) != 1 {
		t.Errorf("expected 1 tx, got %d", len(backend.transactions[0]))
	}
}

// ---------------------------------------------------------------------------
// Broadcast: NewPooledTransactionHashes (eth/68)
// ---------------------------------------------------------------------------

func TestHandleNewPooledTransactionHashes(t *testing.T) {
	backend := newMockBackend()
	peer := testPeer()

	pkt := NewPooledTransactionHashesPacket68{
		Types:  []byte{0x02, 0x03},
		Sizes:  []uint32{128, 256},
		Hashes: []types.Hash{types.HexToHash("aa"), types.HexToHash("bb")},
	}
	msg, err := EncodeMessage(NewPooledTransactionHashesMsg, pkt)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	reg := NewHandlerRegistry()
	if err := reg.Handle(backend, peer, msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(backend.pooledTxHashes) != 1 {
		t.Fatalf("expected 1 call, got %d", len(backend.pooledTxHashes))
	}
	call := backend.pooledTxHashes[0]
	if len(call.Types) != 2 || len(call.Sizes) != 2 || len(call.Hashes) != 2 {
		t.Error("unexpected lengths in pooled tx hash call")
	}
}

func TestHandleNewPooledTransactionHashes_MismatchedLengths(t *testing.T) {
	peer := testPeer()

	pkt := NewPooledTransactionHashesPacket68{
		Types:  []byte{0x02},
		Sizes:  []uint32{128, 256}, // length mismatch
		Hashes: []types.Hash{types.HexToHash("aa")},
	}
	msg, err := EncodeMessage(NewPooledTransactionHashesMsg, pkt)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	reg := NewHandlerRegistry()
	err = reg.Handle(nil, peer, msg)
	if err == nil {
		t.Fatal("expected error for mismatched lengths")
	}
}

// ---------------------------------------------------------------------------
// Peer protocol state tests
// ---------------------------------------------------------------------------

func TestPeer_HeadNumber(t *testing.T) {
	p := testPeer()
	if p.HeadNumber() != 0 {
		t.Errorf("initial HeadNumber = %d, want 0", p.HeadNumber())
	}
	p.SetHeadNumber(12345)
	if p.HeadNumber() != 12345 {
		t.Errorf("HeadNumber = %d, want 12345", p.HeadNumber())
	}
}

func TestPeer_LastResponse(t *testing.T) {
	p := testPeer()
	if p.LastResponse(BlockHeadersMsg) != nil {
		t.Error("expected nil for unset last response")
	}

	val := BlockHeadersPacket{RequestID: 99}
	p.SetLastResponse(BlockHeadersMsg, val)

	got := p.LastResponse(BlockHeadersMsg)
	if got == nil {
		t.Fatal("expected non-nil last response")
	}
	bhp, ok := got.(BlockHeadersPacket)
	if !ok {
		t.Fatalf("type = %T", got)
	}
	if bhp.RequestID != 99 {
		t.Errorf("RequestID = %d, want 99", bhp.RequestID)
	}
}

func TestPeer_DeliverResponse(t *testing.T) {
	p := testPeer()

	// Nothing delivered yet.
	_, ok := p.GetDeliveredResponse(1)
	if ok {
		t.Error("expected no delivered response initially")
	}

	p.DeliverResponse(1, "hello")
	v, ok := p.GetDeliveredResponse(1)
	if !ok {
		t.Fatal("expected delivered response")
	}
	if v != "hello" {
		t.Errorf("value = %v, want hello", v)
	}

	// Should be removed after retrieval.
	_, ok = p.GetDeliveredResponse(1)
	if ok {
		t.Error("expected response to be removed after retrieval")
	}
}

// ---------------------------------------------------------------------------
// Request tracker tests
// ---------------------------------------------------------------------------

func TestRequestTracker_TrackAndDeliver(t *testing.T) {
	rt := NewRequestTracker(5 * time.Second)
	defer rt.Close()

	id := rt.NextRequestID()
	ch, err := rt.Track(id)
	if err != nil {
		t.Fatalf("Track: %v", err)
	}

	if rt.Pending() != 1 {
		t.Errorf("Pending = %d, want 1", rt.Pending())
	}

	go func() {
		rt.Deliver(id, "response-value")
	}()

	select {
	case v := <-ch:
		if v != "response-value" {
			t.Errorf("value = %v, want response-value", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for delivery")
	}

	if rt.Pending() != 0 {
		t.Errorf("Pending after deliver = %d, want 0", rt.Pending())
	}
}

func TestRequestTracker_DuplicateRequest(t *testing.T) {
	rt := NewRequestTracker(5 * time.Second)
	defer rt.Close()

	id := rt.NextRequestID()
	_, err := rt.Track(id)
	if err != nil {
		t.Fatalf("first Track: %v", err)
	}

	_, err = rt.Track(id)
	if err != ErrDuplicateRequest {
		t.Errorf("second Track: got %v, want ErrDuplicateRequest", err)
	}
}

func TestRequestTracker_DeliverUnknown(t *testing.T) {
	rt := NewRequestTracker(5 * time.Second)
	defer rt.Close()

	err := rt.Deliver(999, "value")
	if err != ErrUnknownRequest {
		t.Errorf("Deliver unknown: got %v, want ErrUnknownRequest", err)
	}
}

func TestRequestTracker_Cancel(t *testing.T) {
	rt := NewRequestTracker(5 * time.Second)
	defer rt.Close()

	id := rt.NextRequestID()
	ch, _ := rt.Track(id)

	rt.Cancel(id)

	// The channel should be closed.
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed with no value")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for channel close")
	}

	if rt.Pending() != 0 {
		t.Errorf("Pending after cancel = %d, want 0", rt.Pending())
	}
}

func TestRequestTracker_Timeout(t *testing.T) {
	// Use a very short timeout.
	rt := NewRequestTracker(100 * time.Millisecond)
	defer rt.Close()

	id := rt.NextRequestID()
	ch, _ := rt.Track(id)

	// Wait for the expiry loop to clean up.
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed on timeout, got value")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for request expiry")
	}

	if rt.Pending() != 0 {
		t.Errorf("Pending after timeout = %d, want 0", rt.Pending())
	}
}

func TestRequestTracker_NextRequestID(t *testing.T) {
	rt := NewRequestTracker(5 * time.Second)
	defer rt.Close()

	id1 := rt.NextRequestID()
	id2 := rt.NextRequestID()
	id3 := rt.NextRequestID()

	if id2 != id1+1 || id3 != id2+1 {
		t.Errorf("IDs not monotonic: %d, %d, %d", id1, id2, id3)
	}
}

func TestRequestTracker_Close(t *testing.T) {
	rt := NewRequestTracker(5 * time.Second)

	id := rt.NextRequestID()
	ch, _ := rt.Track(id)

	rt.Close()

	// Channel should be closed.
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel closed on Close()")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Close() to unblock channel")
	}

	// Double close should be safe.
	rt.Close()
}

// ---------------------------------------------------------------------------
// Message encoding round-trip tests
// ---------------------------------------------------------------------------

func TestEncodeDecodeGetBlockHeadersPacket(t *testing.T) {
	original := GetBlockHeadersPacket{
		RequestID: 77,
		Request: GetBlockHeadersRequest{
			Origin:  HashOrNumber{Number: 500},
			Amount:  128,
			Skip:    3,
			Reverse: true,
		},
	}
	msg, err := EncodeMessage(GetBlockHeadersMsg, original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var decoded GetBlockHeadersPacket
	if err := DecodeMessage(msg, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.RequestID != 77 {
		t.Errorf("RequestID = %d, want 77", decoded.RequestID)
	}
	if decoded.Request.Origin.Number != 500 {
		t.Errorf("Origin.Number = %d, want 500", decoded.Request.Origin.Number)
	}
	if decoded.Request.Amount != 128 {
		t.Errorf("Amount = %d, want 128", decoded.Request.Amount)
	}
	if decoded.Request.Skip != 3 {
		t.Errorf("Skip = %d, want 3", decoded.Request.Skip)
	}
	if !decoded.Request.Reverse {
		t.Error("Reverse = false, want true")
	}
}

func TestEncodeDecodeGetBlockBodiesPacket(t *testing.T) {
	h1 := types.HexToHash("aaaa")
	h2 := types.HexToHash("bbbb")
	original := GetBlockBodiesPacket{
		RequestID: 88,
		Hashes:    GetBlockBodiesRequest{h1, h2},
	}
	msg, err := EncodeMessage(GetBlockBodiesMsg, original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var decoded GetBlockBodiesPacket
	if err := DecodeMessage(msg, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.RequestID != 88 {
		t.Errorf("RequestID = %d, want 88", decoded.RequestID)
	}
	if len(decoded.Hashes) != 2 {
		t.Fatalf("len(Hashes) = %d, want 2", len(decoded.Hashes))
	}
	if decoded.Hashes[0] != h1 {
		t.Error("Hashes[0] mismatch")
	}
}

func TestEncodeDecodeGetReceiptsPacket(t *testing.T) {
	h := types.HexToHash("cccc")
	original := GetReceiptsPacket{
		RequestID: 99,
		Hashes:    GetReceiptsRequest{h},
	}
	msg, err := EncodeMessage(GetReceiptsMsg, original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var decoded GetReceiptsPacket
	if err := DecodeMessage(msg, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.RequestID != 99 {
		t.Errorf("RequestID = %d, want 99", decoded.RequestID)
	}
	if len(decoded.Hashes) != 1 || decoded.Hashes[0] != h {
		t.Error("hash mismatch")
	}
}

func TestEncodeDecodeGetPooledTransactionsPacket(t *testing.T) {
	h := types.HexToHash("dddd")
	original := GetPooledTransactionsPacket{
		RequestID: 55,
		Hashes:    GetPooledTransactionsRequest{h},
	}
	msg, err := EncodeMessage(GetPooledTransactionsMsg, original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var decoded GetPooledTransactionsPacket
	if err := DecodeMessage(msg, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.RequestID != 55 {
		t.Errorf("RequestID = %d, want 55", decoded.RequestID)
	}
	if len(decoded.Hashes) != 1 || decoded.Hashes[0] != h {
		t.Error("hash mismatch")
	}
}

func TestEncodeDecodeNewBlockHashesEntries(t *testing.T) {
	entries := []NewBlockHashesEntry{
		{Hash: types.HexToHash("ff00"), Number: 42},
		{Hash: types.HexToHash("ff01"), Number: 43},
	}
	msg, err := EncodeMessage(NewBlockHashesMsg, entries)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var decoded []NewBlockHashesEntry
	if err := DecodeMessage(msg, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("len = %d, want 2", len(decoded))
	}
	if decoded[0].Number != 42 || decoded[1].Number != 43 {
		t.Error("number mismatch")
	}
}

func TestEncodeDecodeNewPooledTransactionHashesPacket68(t *testing.T) {
	original := NewPooledTransactionHashesPacket68{
		Types:  []byte{0x02, 0x03},
		Sizes:  []uint32{100, 200},
		Hashes: []types.Hash{types.HexToHash("a1"), types.HexToHash("b2")},
	}
	msg, err := EncodeMessage(NewPooledTransactionHashesMsg, original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var decoded NewPooledTransactionHashesPacket68
	if err := DecodeMessage(msg, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Types) != 2 || len(decoded.Sizes) != 2 || len(decoded.Hashes) != 2 {
		t.Error("length mismatch after decode")
	}
	if decoded.Types[0] != 0x02 || decoded.Types[1] != 0x03 {
		t.Error("types mismatch")
	}
	if decoded.Sizes[0] != 100 || decoded.Sizes[1] != 200 {
		t.Error("sizes mismatch")
	}
}

// ---------------------------------------------------------------------------
// Handler error/edge case tests
// ---------------------------------------------------------------------------

func TestHandleGetBlockHeaders_DecodeError(t *testing.T) {
	reg := NewHandlerRegistry()
	peer := testPeer()
	backend := newMockBackend()

	// Deliberately invalid payload.
	msg := Message{
		Code:    GetBlockHeadersMsg,
		Size:    3,
		Payload: []byte{0xff, 0xff, 0xff},
	}
	err := reg.Handle(backend, peer, msg)
	if err == nil {
		t.Error("expected decode error")
	}
}

func TestHandleGetBlockBodies_NilBackend(t *testing.T) {
	reg := NewHandlerRegistry()
	peer := testPeer()
	pkt := GetBlockBodiesPacket{RequestID: 1, Hashes: GetBlockBodiesRequest{}}
	msg, _ := EncodeMessage(GetBlockBodiesMsg, pkt)
	err := reg.Handle(nil, peer, msg)
	if err != ErrNilBackend {
		t.Errorf("expected ErrNilBackend, got %v", err)
	}
}

func TestHandleGetReceipts_NilBackend(t *testing.T) {
	reg := NewHandlerRegistry()
	peer := testPeer()
	pkt := GetReceiptsPacket{RequestID: 1, Hashes: GetReceiptsRequest{}}
	msg, _ := EncodeMessage(GetReceiptsMsg, pkt)
	err := reg.Handle(nil, peer, msg)
	if err != ErrNilBackend {
		t.Errorf("expected ErrNilBackend, got %v", err)
	}
}

func TestHandleGetPooledTransactions_NilBackend(t *testing.T) {
	reg := NewHandlerRegistry()
	peer := testPeer()
	pkt := GetPooledTransactionsPacket{RequestID: 1, Hashes: GetPooledTransactionsRequest{}}
	msg, _ := EncodeMessage(GetPooledTransactionsMsg, pkt)
	err := reg.Handle(nil, peer, msg)
	if err != ErrNilBackend {
		t.Errorf("expected ErrNilBackend, got %v", err)
	}
}

// Test broadcast handlers with nil backend (should not panic).
func TestBroadcastHandlers_NilBackend(t *testing.T) {
	reg := NewHandlerRegistry()
	peer := testPeer()

	// NewBlockHashes with nil backend.
	entries := []NewBlockHashesEntry{{Hash: types.HexToHash("aa"), Number: 1}}
	msg, _ := EncodeMessage(NewBlockHashesMsg, entries)
	if err := reg.Handle(nil, peer, msg); err != nil {
		t.Errorf("NewBlockHashes nil backend: %v", err)
	}

	// Transactions with nil backend.
	txs := []*types.Transaction{}
	msg, _ = EncodeMessage(TransactionsMsg, txs)
	if err := reg.Handle(nil, peer, msg); err != nil {
		t.Errorf("Transactions nil backend: %v", err)
	}

	// NewPooledTransactionHashes with nil backend.
	pkt := NewPooledTransactionHashesPacket68{
		Types: []byte{}, Sizes: []uint32{}, Hashes: []types.Hash{},
	}
	msg, _ = EncodeMessage(NewPooledTransactionHashesMsg, pkt)
	if err := reg.Handle(nil, peer, msg); err != nil {
		t.Errorf("NewPooledTransactionHashes nil backend: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Verify handler map completeness
// ---------------------------------------------------------------------------

func TestAllEth68Codes_HaveHandlers(t *testing.T) {
	reg := NewHandlerRegistry()
	// StatusMsg (0x00) deliberately has no default handler since it's handled
	// at the handshake level, not in the message loop.
	codes := []struct {
		code uint64
		name string
	}{
		{GetBlockHeadersMsg, "GetBlockHeaders"},
		{BlockHeadersMsg, "BlockHeaders"},
		{GetBlockBodiesMsg, "GetBlockBodies"},
		{BlockBodiesMsg, "BlockBodies"},
		{GetReceiptsMsg, "GetReceipts"},
		{ReceiptsMsg, "Receipts"},
		{GetPooledTransactionsMsg, "GetPooledTransactions"},
		{PooledTransactionsMsg, "PooledTransactions"},
		{NewBlockHashesMsg, "NewBlockHashes"},
		{NewBlockMsg, "NewBlock"},
		{TransactionsMsg, "Transactions"},
		{NewPooledTransactionHashesMsg, "NewPooledTransactionHashes"},
	}
	for _, tt := range codes {
		if reg.Lookup(tt.code) == nil {
			t.Errorf("missing handler for %s (0x%02x)", tt.name, tt.code)
		}
	}
}
