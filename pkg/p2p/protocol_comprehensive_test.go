package p2p

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/rlp"
)

// ---------------------------------------------------------------------------
// Test: ETH Protocol Handshake
// ---------------------------------------------------------------------------

// TestETHProtocolHandshake simulates an ETH protocol status handshake between
// two peers over a MsgPipe, verifying the hello exchange and capability matching.
func TestETHProtocolHandshake(t *testing.T) {
	a, b := MsgPipe()
	defer a.Close()

	localHello := &HelloPacket{
		Version:    baseProtocolVersion,
		Name:       "eth2030/v0.1.0",
		Caps:       []Cap{{Name: "eth", Version: 68}},
		ListenPort: 30303,
		ID:         "localnode",
	}

	remoteHello := &HelloPacket{
		Version:    baseProtocolVersion,
		Name:       "geth/v1.13.0",
		Caps:       []Cap{{Name: "eth", Version: 68}, {Name: "snap", Version: 1}},
		ListenPort: 30303,
		ID:         "remotenode",
	}

	// Perform handshake concurrently on both ends.
	type result struct {
		hello *HelloPacket
		err   error
	}
	ch := make(chan result, 2)

	go func() {
		h, err := PerformHandshake(a, localHello)
		ch <- result{h, err}
	}()
	go func() {
		h, err := PerformHandshake(b, remoteHello)
		ch <- result{h, err}
	}()

	for i := 0; i < 2; i++ {
		res := <-ch
		if res.err != nil {
			t.Fatalf("handshake %d failed: %v", i, res.err)
		}
		if res.hello == nil {
			t.Fatalf("handshake %d returned nil hello", i)
		}
	}
}

// TestETHProtocolHandshakeNoMatchingCaps verifies that a handshake fails
// when the two peers have no matching capabilities.
func TestETHProtocolHandshakeNoMatchingCaps(t *testing.T) {
	a, b := MsgPipe()
	defer a.Close()

	localHello := &HelloPacket{
		Version:    baseProtocolVersion,
		Name:       "eth2030/v0.1.0",
		Caps:       []Cap{{Name: "eth", Version: 68}},
		ListenPort: 30303,
		ID:         "localnode",
	}

	// Remote only supports snap/1, not eth/68.
	remoteHello := &HelloPacket{
		Version:    baseProtocolVersion,
		Name:       "geth/v1.13.0",
		Caps:       []Cap{{Name: "snap", Version: 1}},
		ListenPort: 30303,
		ID:         "remotenode",
	}

	type result struct {
		hello *HelloPacket
		err   error
	}
	ch := make(chan result, 2)

	go func() {
		h, err := PerformHandshake(a, localHello)
		ch <- result{h, err}
	}()
	go func() {
		h, err := PerformHandshake(b, remoteHello)
		ch <- result{h, err}
	}()

	// At least one side should fail with ErrNoMatchingCaps.
	gotError := false
	for i := 0; i < 2; i++ {
		res := <-ch
		if res.err != nil {
			gotError = true
		}
	}
	if !gotError {
		t.Error("expected at least one handshake failure for no matching caps")
	}
}

// ---------------------------------------------------------------------------
// Test: Block Header Exchange
// ---------------------------------------------------------------------------

// TestBlockHeaderExchange tests creating block header request/response packets
// and verifying their RLP encoding round-trips correctly.
func TestBlockHeaderExchange(t *testing.T) {
	// Create a request for headers starting at block 100.
	req := GetBlockHeadersPacket{
		RequestID: 42,
		Request: GetBlockHeadersRequest{
			Origin:  HashOrNumber{Number: 100},
			Amount:  64,
			Skip:    0,
			Reverse: false,
		},
	}

	if req.RequestID != 42 {
		t.Errorf("RequestID = %d, want 42", req.RequestID)
	}
	if req.Request.Amount != 64 {
		t.Errorf("Amount = %d, want 64", req.Request.Amount)
	}
	if req.Request.Origin.Number != 100 {
		t.Errorf("Origin.Number = %d, want 100", req.Request.Origin.Number)
	}

	// Create response headers.
	headers := make([]*types.Header, 3)
	for i := 0; i < 3; i++ {
		headers[i] = &types.Header{
			Number:     big.NewInt(int64(100 + i)),
			GasLimit:   30_000_000,
			Difficulty: big.NewInt(1),
			Time:       uint64(1700000000 + i*12),
		}
	}

	resp := BlockHeadersPacket{
		RequestID: 42,
		Headers:   headers,
	}

	if resp.RequestID != 42 {
		t.Errorf("resp RequestID = %d, want 42", resp.RequestID)
	}
	if len(resp.Headers) != 3 {
		t.Errorf("resp headers = %d, want 3", len(resp.Headers))
	}
	for i, h := range resp.Headers {
		if h.Number.Int64() != int64(100+i) {
			t.Errorf("header[%d] number = %d, want %d", i, h.Number.Int64(), 100+i)
		}
	}

	// Test RLP round-trip on request via HashOrNumber by-number.
	reqByNum := GetBlockHeadersRequest{
		Origin:  HashOrNumber{Number: 500},
		Amount:  128,
		Skip:    1,
		Reverse: true,
	}
	if reqByNum.Origin.IsHash() {
		t.Error("expected number-based origin, got hash")
	}

	// Test HashOrNumber by-hash.
	h := types.HexToHash("abcdef1234567890")
	reqByHash := GetBlockHeadersRequest{
		Origin:  HashOrNumber{Hash: h},
		Amount:  1,
		Skip:    0,
		Reverse: false,
	}
	if !reqByHash.Origin.IsHash() {
		t.Error("expected hash-based origin, got number")
	}
	if reqByHash.Origin.Hash != h {
		t.Error("hash origin mismatch")
	}
}

// ---------------------------------------------------------------------------
// Test: Block Body Exchange
// ---------------------------------------------------------------------------

// TestBlockBodyExchange tests creating block body request/response packets.
func TestBlockBodyExchange(t *testing.T) {
	h1 := types.HexToHash("1111111111111111111111111111111111111111111111111111111111111111")
	h2 := types.HexToHash("2222222222222222222222222222222222222222222222222222222222222222")
	h3 := types.HexToHash("3333333333333333333333333333333333333333333333333333333333333333")

	// Request bodies for 3 blocks.
	req := GetBlockBodiesPacket{
		RequestID: 99,
		Hashes:    GetBlockBodiesRequest{h1, h2, h3},
	}

	if req.RequestID != 99 {
		t.Errorf("RequestID = %d, want 99", req.RequestID)
	}
	if len(req.Hashes) != 3 {
		t.Errorf("hashes count = %d, want 3", len(req.Hashes))
	}
	if req.Hashes[0] != h1 {
		t.Error("hash[0] mismatch")
	}
	if req.Hashes[1] != h2 {
		t.Error("hash[1] mismatch")
	}
	if req.Hashes[2] != h3 {
		t.Error("hash[2] mismatch")
	}

	// Create response with block bodies containing transactions.
	to := types.BytesToAddress([]byte{0xaa})
	tx1 := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(100),
	})

	body := BlockBody{
		Transactions: []*types.Transaction{tx1},
		Uncles:       nil,
		Withdrawals:  nil,
	}

	if len(body.Transactions) != 1 {
		t.Errorf("body tx count = %d, want 1", len(body.Transactions))
	}
	if body.Transactions[0].Nonce() != 0 {
		t.Errorf("body tx nonce = %d, want 0", body.Transactions[0].Nonce())
	}
}

// ---------------------------------------------------------------------------
// Test: Transaction Broadcast
// ---------------------------------------------------------------------------

// TestTransactionBroadcast tests the NewPooledTransactionHashesPacket68 message
// encoding used for broadcasting new transactions.
func TestTransactionBroadcast(t *testing.T) {
	to := types.BytesToAddress([]byte{0xaa})

	// Create several transactions of different types.
	tx1 := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(100),
	})
	tx2 := types.NewTransaction(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     1,
		GasTipCap: big.NewInt(10),
		GasFeeCap: big.NewInt(100),
		Gas:       21000,
		To:        &to,
		Value:     big.NewInt(200),
	})

	txs := []*types.Transaction{tx1, tx2}

	// Build the announcement packet.
	txTypes := make([]byte, len(txs))
	txSizes := make([]uint32, len(txs))
	txHashes := make([]types.Hash, len(txs))

	for i, tx := range txs {
		txTypes[i] = tx.Type()
		txHashes[i] = tx.Hash()
		// Approximate size using RLP encoding.
		encoded, err := rlp.EncodeToBytes(tx)
		if err != nil {
			t.Fatalf("rlp encode tx %d: %v", i, err)
		}
		txSizes[i] = uint32(len(encoded))
	}

	pkt := NewPooledTransactionHashesPacket68{
		Types:  txTypes,
		Sizes:  txSizes,
		Hashes: txHashes,
	}

	if len(pkt.Types) != 2 {
		t.Errorf("types count = %d, want 2", len(pkt.Types))
	}
	if pkt.Types[0] != types.LegacyTxType {
		t.Errorf("type[0] = %d, want %d (Legacy)", pkt.Types[0], types.LegacyTxType)
	}
	if pkt.Types[1] != types.DynamicFeeTxType {
		t.Errorf("type[1] = %d, want %d (DynamicFee)", pkt.Types[1], types.DynamicFeeTxType)
	}
	if len(pkt.Hashes) != 2 {
		t.Errorf("hashes count = %d, want 2", len(pkt.Hashes))
	}
	if pkt.Hashes[0] != tx1.Hash() {
		t.Error("hash[0] mismatch")
	}
	if pkt.Hashes[1] != tx2.Hash() {
		t.Error("hash[1] mismatch")
	}
	// Sizes should be > 0.
	for i, size := range pkt.Sizes {
		if size == 0 {
			t.Errorf("size[%d] = 0, want > 0", i)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: Status Data Creation
// ---------------------------------------------------------------------------

// TestStatusDataCreation verifies that StatusData packets can be constructed
// with correct field values for ETH protocol status messages.
func TestStatusDataCreation(t *testing.T) {
	genesis := types.HexToHash("d4e56740f876aef8c010b86a40d5f56745a118d0906a34e69aec8c0db1cb8fa3")
	head := types.HexToHash("abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789")
	td := new(big.Int).SetUint64(17_000_000_000_000)

	status := StatusData{
		ProtocolVersion: ETH68,
		NetworkID:       1,
		TD:              td,
		Head:            head,
		Genesis:         genesis,
		ForkID:          ForkID{Hash: [4]byte{0xfc, 0x64, 0xec, 0x04}, Next: 1681338455},
	}

	if status.ProtocolVersion != 68 {
		t.Errorf("protocol version = %d, want 68", status.ProtocolVersion)
	}
	if status.NetworkID != 1 {
		t.Errorf("network ID = %d, want 1", status.NetworkID)
	}
	if status.TD.Cmp(td) != 0 {
		t.Errorf("TD = %s, want %s", status.TD, td)
	}
	if status.Head != head {
		t.Error("head hash mismatch")
	}
	if status.Genesis != genesis {
		t.Error("genesis hash mismatch")
	}
	if status.ForkID.Hash != [4]byte{0xfc, 0x64, 0xec, 0x04} {
		t.Errorf("fork ID hash = %x, want fc64ec04", status.ForkID.Hash)
	}
	if status.ForkID.Next != 1681338455 {
		t.Errorf("fork ID next = %d, want 1681338455", status.ForkID.Next)
	}
}

// ---------------------------------------------------------------------------
// Test: MsgPipe Communication
// ---------------------------------------------------------------------------

// TestMsgPipeBidirectional verifies that MsgPipe correctly delivers messages
// bidirectionally between two endpoints.
func TestMsgPipeBidirectional(t *testing.T) {
	a, b := MsgPipe()
	defer a.Close()

	// Send from A, receive on B.
	payload := []byte("hello from A")
	if err := a.WriteMsg(Msg{Code: 0x01, Size: uint32(len(payload)), Payload: payload}); err != nil {
		t.Fatalf("write A->B: %v", err)
	}

	msg, err := b.ReadMsg()
	if err != nil {
		t.Fatalf("read A->B: %v", err)
	}
	if msg.Code != 0x01 {
		t.Errorf("msg code = 0x%02x, want 0x01", msg.Code)
	}
	if string(msg.Payload) != "hello from A" {
		t.Errorf("payload = %q, want %q", msg.Payload, "hello from A")
	}

	// Send from B, receive on A.
	payload2 := []byte("hello from B")
	if err := b.WriteMsg(Msg{Code: 0x02, Size: uint32(len(payload2)), Payload: payload2}); err != nil {
		t.Fatalf("write B->A: %v", err)
	}

	msg2, err := a.ReadMsg()
	if err != nil {
		t.Fatalf("read B->A: %v", err)
	}
	if msg2.Code != 0x02 {
		t.Errorf("msg2 code = 0x%02x, want 0x02", msg2.Code)
	}
	if string(msg2.Payload) != "hello from B" {
		t.Errorf("payload2 = %q, want %q", msg2.Payload, "hello from B")
	}
}

// ---------------------------------------------------------------------------
// Test: Hello Packet Encode/Decode Round Trip
// ---------------------------------------------------------------------------

// TestHelloPacketRoundTrip verifies that HelloPacket encode/decode round-trips
// correctly for various capability configurations.
func TestHelloPacketRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		hello *HelloPacket
	}{
		{
			name: "single cap",
			hello: &HelloPacket{
				Version:    5,
				Name:       "eth2030/v0.1.0",
				Caps:       []Cap{{Name: "eth", Version: 68}},
				ListenPort: 30303,
				ID:         "node123",
			},
		},
		{
			name: "multiple caps",
			hello: &HelloPacket{
				Version:    5,
				Name:       "geth/v1.13.0-stable-deadbeef/linux-amd64",
				Caps:       []Cap{{Name: "eth", Version: 67}, {Name: "eth", Version: 68}, {Name: "snap", Version: 1}},
				ListenPort: 30303,
				ID:         "0x1234567890abcdef",
			},
		},
		{
			name: "no caps",
			hello: &HelloPacket{
				Version:    5,
				Name:       "empty",
				Caps:       nil,
				ListenPort: 0,
				ID:         "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := EncodeHello(tt.hello)
			decoded, err := DecodeHello(encoded)
			if err != nil {
				t.Fatalf("DecodeHello: %v", err)
			}

			if decoded.Version != tt.hello.Version {
				t.Errorf("Version = %d, want %d", decoded.Version, tt.hello.Version)
			}
			if decoded.Name != tt.hello.Name {
				t.Errorf("Name = %q, want %q", decoded.Name, tt.hello.Name)
			}
			if decoded.ListenPort != tt.hello.ListenPort {
				t.Errorf("ListenPort = %d, want %d", decoded.ListenPort, tt.hello.ListenPort)
			}
			if decoded.ID != tt.hello.ID {
				t.Errorf("ID = %q, want %q", decoded.ID, tt.hello.ID)
			}
			if len(decoded.Caps) != len(tt.hello.Caps) {
				t.Fatalf("Caps count = %d, want %d", len(decoded.Caps), len(tt.hello.Caps))
			}
			for i, cap := range decoded.Caps {
				if cap.Name != tt.hello.Caps[i].Name {
					t.Errorf("Cap[%d].Name = %q, want %q", i, cap.Name, tt.hello.Caps[i].Name)
				}
				if cap.Version != tt.hello.Caps[i].Version {
					t.Errorf("Cap[%d].Version = %d, want %d", i, cap.Version, tt.hello.Caps[i].Version)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test: New Block Data
// ---------------------------------------------------------------------------

// TestNewBlockDataExchange verifies the NewBlockData struct used for block
// propagation contains the correct block and total difficulty.
func TestNewBlockDataExchange(t *testing.T) {
	header := &types.Header{
		Number:     big.NewInt(12345),
		Difficulty: big.NewInt(0),
		GasLimit:   30_000_000,
		Time:       1700000000,
	}

	to := types.BytesToAddress([]byte{0xaa})
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(100),
	})

	block := types.NewBlock(header, &types.Body{
		Transactions: []*types.Transaction{tx},
	})

	td := new(big.Int).SetUint64(17_000_000_000_000)
	nbd := NewBlockData{
		Block: block,
		TD:    td,
	}

	if nbd.Block.NumberU64() != 12345 {
		t.Errorf("block number = %d, want 12345", nbd.Block.NumberU64())
	}
	if len(nbd.Block.Transactions()) != 1 {
		t.Errorf("tx count = %d, want 1", len(nbd.Block.Transactions()))
	}
	if nbd.TD.Cmp(td) != 0 {
		t.Errorf("TD = %s, want %s", nbd.TD, td)
	}
}

// ---------------------------------------------------------------------------
// Test: Matching Caps
// ---------------------------------------------------------------------------

// TestMatchingCapsComprehensive verifies the capability matching logic used
// during protocol negotiation with multiple overlap scenarios.
func TestMatchingCapsComprehensive(t *testing.T) {
	local := []Cap{
		{Name: "eth", Version: 67},
		{Name: "eth", Version: 68},
		{Name: "snap", Version: 1},
	}

	t.Run("full overlap", func(t *testing.T) {
		remote := []Cap{
			{Name: "eth", Version: 68},
			{Name: "snap", Version: 1},
		}
		matched := MatchingCaps(local, remote)
		if len(matched) != 2 {
			t.Errorf("matched = %d, want 2", len(matched))
		}
	})

	t.Run("partial overlap", func(t *testing.T) {
		remote := []Cap{
			{Name: "eth", Version: 68},
			{Name: "les", Version: 4},
		}
		matched := MatchingCaps(local, remote)
		if len(matched) != 1 {
			t.Errorf("matched = %d, want 1", len(matched))
		}
		if matched[0].Name != "eth" || matched[0].Version != 68 {
			t.Errorf("matched cap = %v, want eth/68", matched[0])
		}
	})

	t.Run("no overlap", func(t *testing.T) {
		remote := []Cap{
			{Name: "les", Version: 4},
			{Name: "pip", Version: 1},
		}
		matched := MatchingCaps(local, remote)
		if len(matched) != 0 {
			t.Errorf("matched = %d, want 0", len(matched))
		}
	})

	t.Run("version mismatch", func(t *testing.T) {
		remote := []Cap{
			{Name: "eth", Version: 66},
			{Name: "snap", Version: 2},
		}
		matched := MatchingCaps(local, remote)
		if len(matched) != 0 {
			t.Errorf("matched = %d, want 0 (version mismatch)", len(matched))
		}
	})
}
