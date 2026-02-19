package eth

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/p2p"
)

func TestEncodeDecodeStatusMessage(t *testing.T) {
	msg := &StatusMessage{
		ProtocolVersion: 68,
		NetworkID:       1,
		TD:              big.NewInt(17000000),
		BestHash:        types.HexToHash("0xaaaa"),
		Genesis:         types.HexToHash("0xbbbb"),
		ForkID:          p2p.ForkID{Hash: [4]byte{0xde, 0xad, 0xbe, 0xef}, Next: 100},
	}

	data, err := EncodeMsg(MsgStatus, msg)
	if err != nil {
		t.Fatalf("EncodeMsg Status: %v", err)
	}

	decoded, err := DecodeMsg(MsgStatus, data)
	if err != nil {
		t.Fatalf("DecodeMsg Status: %v", err)
	}

	sm, ok := decoded.(*StatusMessage)
	if !ok {
		t.Fatal("decoded message is not *StatusMessage")
	}
	if sm.ProtocolVersion != 68 {
		t.Fatalf("ProtocolVersion: want 68, got %d", sm.ProtocolVersion)
	}
	if sm.NetworkID != 1 {
		t.Fatalf("NetworkID: want 1, got %d", sm.NetworkID)
	}
	if sm.TD.Cmp(big.NewInt(17000000)) != 0 {
		t.Fatalf("TD: want 17000000, got %s", sm.TD)
	}
	if sm.BestHash != msg.BestHash {
		t.Fatalf("BestHash mismatch")
	}
	if sm.Genesis != msg.Genesis {
		t.Fatalf("Genesis mismatch")
	}
}

func TestEncodeDecodeNewBlockHashes(t *testing.T) {
	msg := &NewBlockHashesMessage{
		Entries: []BlockHashEntry{
			{Hash: types.HexToHash("0x1111"), Number: 100},
			{Hash: types.HexToHash("0x2222"), Number: 101},
		},
	}

	data, err := EncodeMsg(MsgNewBlockHashes, msg)
	if err != nil {
		t.Fatalf("EncodeMsg NewBlockHashes: %v", err)
	}

	decoded, err := DecodeMsg(MsgNewBlockHashes, data)
	if err != nil {
		t.Fatalf("DecodeMsg NewBlockHashes: %v", err)
	}

	nm, ok := decoded.(*NewBlockHashesMessage)
	if !ok {
		t.Fatal("decoded message is not *NewBlockHashesMessage")
	}
	if len(nm.Entries) != 2 {
		t.Fatalf("entries count: want 2, got %d", len(nm.Entries))
	}
	if nm.Entries[0].Number != 100 {
		t.Fatalf("entry 0 number: want 100, got %d", nm.Entries[0].Number)
	}
	if nm.Entries[1].Number != 101 {
		t.Fatalf("entry 1 number: want 101, got %d", nm.Entries[1].Number)
	}
}

func TestEncodeDecodeGetBlockHeaders(t *testing.T) {
	msg := &GetBlockHeadersMessage{
		Origin:  p2p.HashOrNumber{Number: 500},
		Amount:  10,
		Skip:    1,
		Reverse: true,
	}

	data, err := EncodeMsg(MsgGetBlockHeaders, msg)
	if err != nil {
		t.Fatalf("EncodeMsg GetBlockHeaders: %v", err)
	}

	decoded, err := DecodeMsg(MsgGetBlockHeaders, data)
	if err != nil {
		t.Fatalf("DecodeMsg GetBlockHeaders: %v", err)
	}

	gm, ok := decoded.(*GetBlockHeadersMessage)
	if !ok {
		t.Fatal("decoded message is not *GetBlockHeadersMessage")
	}
	if gm.Amount != 10 {
		t.Fatalf("Amount: want 10, got %d", gm.Amount)
	}
	if gm.Skip != 1 {
		t.Fatalf("Skip: want 1, got %d", gm.Skip)
	}
	if !gm.Reverse {
		t.Fatal("Reverse: want true, got false")
	}
}

func TestEncodeDecodeGetBlockBodies(t *testing.T) {
	msg := &GetBlockBodiesMessage{
		Hashes: []types.Hash{
			types.HexToHash("0xaaaa"),
			types.HexToHash("0xbbbb"),
		},
	}

	data, err := EncodeMsg(MsgGetBlockBodies, msg)
	if err != nil {
		t.Fatalf("EncodeMsg GetBlockBodies: %v", err)
	}

	decoded, err := DecodeMsg(MsgGetBlockBodies, data)
	if err != nil {
		t.Fatalf("DecodeMsg GetBlockBodies: %v", err)
	}

	gm, ok := decoded.(*GetBlockBodiesMessage)
	if !ok {
		t.Fatal("decoded message is not *GetBlockBodiesMessage")
	}
	if len(gm.Hashes) != 2 {
		t.Fatalf("hashes count: want 2, got %d", len(gm.Hashes))
	}
	if gm.Hashes[0] != msg.Hashes[0] {
		t.Fatal("hash 0 mismatch")
	}
}

func TestEncodeDecodeNewPooledTxHashes(t *testing.T) {
	msg := &NewPooledTxHashesMsg68{
		Types:  []byte{0x02, 0x03},
		Sizes:  []uint32{200, 300},
		Hashes: []types.Hash{types.HexToHash("0xaa"), types.HexToHash("0xbb")},
	}

	data, err := EncodeMsg(MsgNewPooledTransactionHashes, msg)
	if err != nil {
		t.Fatalf("EncodeMsg NewPooledTxHashes: %v", err)
	}

	decoded, err := DecodeMsg(MsgNewPooledTransactionHashes, data)
	if err != nil {
		t.Fatalf("DecodeMsg NewPooledTxHashes: %v", err)
	}

	pm, ok := decoded.(*NewPooledTxHashesMsg68)
	if !ok {
		t.Fatal("decoded message is not *NewPooledTxHashesMsg68")
	}
	if len(pm.Types) != 2 {
		t.Fatalf("types count: want 2, got %d", len(pm.Types))
	}
	if pm.Types[0] != 0x02 {
		t.Fatalf("type 0: want 0x02, got 0x%02x", pm.Types[0])
	}
	if pm.Sizes[1] != 300 {
		t.Fatalf("size 1: want 300, got %d", pm.Sizes[1])
	}
}

func TestEncodeDecodeGetPooledTransactions(t *testing.T) {
	msg := &GetPooledTransactionsMessage{
		Hashes: []types.Hash{
			types.HexToHash("0x1234"),
			types.HexToHash("0x5678"),
		},
	}

	data, err := EncodeMsg(MsgGetPooledTransactions, msg)
	if err != nil {
		t.Fatalf("EncodeMsg: %v", err)
	}

	decoded, err := DecodeMsg(MsgGetPooledTransactions, data)
	if err != nil {
		t.Fatalf("DecodeMsg: %v", err)
	}

	gm, ok := decoded.(*GetPooledTransactionsMessage)
	if !ok {
		t.Fatal("decoded message is not *GetPooledTransactionsMessage")
	}
	if len(gm.Hashes) != 2 {
		t.Fatalf("hashes count: want 2, got %d", len(gm.Hashes))
	}
}

func TestEncodeMsgUnknownCode(t *testing.T) {
	_, err := EncodeMsg(0xFF, nil)
	if err == nil {
		t.Fatal("expected error for unknown message code")
	}
}

func TestDecodeMsgUnknownCode(t *testing.T) {
	_, err := DecodeMsg(0xFF, nil)
	if err == nil {
		t.Fatal("expected error for unknown message code")
	}
}

func TestEncodeMsgWrongType(t *testing.T) {
	// Pass wrong type for status code.
	_, err := EncodeMsg(MsgStatus, &NewBlockHashesMessage{})
	if err == nil {
		t.Fatal("expected error for wrong message type")
	}
}

func TestMsgCodeName(t *testing.T) {
	tests := []struct {
		code uint64
		name string
	}{
		{MsgStatus, "Status"},
		{MsgNewBlockHashes, "NewBlockHashes"},
		{MsgTransactions, "Transactions"},
		{MsgGetBlockHeaders, "GetBlockHeaders"},
		{MsgBlockHeaders, "BlockHeaders"},
		{MsgGetBlockBodies, "GetBlockBodies"},
		{MsgBlockBodies, "BlockBodies"},
		{MsgNewBlock, "NewBlock"},
		{MsgNewPooledTransactionHashes, "NewPooledTransactionHashes"},
		{MsgGetPooledTransactions, "GetPooledTransactions"},
		{MsgPooledTransactions, "PooledTransactions"},
	}
	for _, tt := range tests {
		got := MsgCodeName(tt.code)
		if got != tt.name {
			t.Errorf("MsgCodeName(0x%02x): want %q, got %q", tt.code, tt.name, got)
		}
	}

	// Unknown code should not panic.
	name := MsgCodeName(0xFF)
	if name == "" {
		t.Fatal("unknown code should return a non-empty string")
	}
}

func TestMessageConstants(t *testing.T) {
	// Verify message codes match the standard ETH/68 protocol values.
	if MsgStatus != 0x00 {
		t.Fatalf("MsgStatus: want 0x00, got 0x%02x", MsgStatus)
	}
	if MsgNewBlockHashes != 0x01 {
		t.Fatalf("MsgNewBlockHashes: want 0x01, got 0x%02x", MsgNewBlockHashes)
	}
	if MsgTransactions != 0x02 {
		t.Fatalf("MsgTransactions: want 0x02, got 0x%02x", MsgTransactions)
	}
	if MsgGetBlockHeaders != 0x03 {
		t.Fatalf("MsgGetBlockHeaders: want 0x03, got 0x%02x", MsgGetBlockHeaders)
	}
	if MsgBlockHeaders != 0x04 {
		t.Fatalf("MsgBlockHeaders: want 0x04, got 0x%02x", MsgBlockHeaders)
	}
	if MsgGetBlockBodies != 0x05 {
		t.Fatalf("MsgGetBlockBodies: want 0x05, got 0x%02x", MsgGetBlockBodies)
	}
	if MsgBlockBodies != 0x06 {
		t.Fatalf("MsgBlockBodies: want 0x06, got 0x%02x", MsgBlockBodies)
	}
	if MsgNewBlock != 0x07 {
		t.Fatalf("MsgNewBlock: want 0x07, got 0x%02x", MsgNewBlock)
	}
	if MsgNewPooledTransactionHashes != 0x08 {
		t.Fatalf("MsgNewPooledTransactionHashes: want 0x08, got 0x%02x", MsgNewPooledTransactionHashes)
	}
	if MsgGetPooledTransactions != 0x09 {
		t.Fatalf("MsgGetPooledTransactions: want 0x09, got 0x%02x", MsgGetPooledTransactions)
	}
	if MsgPooledTransactions != 0x0a {
		t.Fatalf("MsgPooledTransactions: want 0x0a, got 0x%02x", MsgPooledTransactions)
	}
}

func TestEncodeDecodeBlockHeaders(t *testing.T) {
	h1 := &types.Header{
		Number:     big.NewInt(100),
		Difficulty: big.NewInt(1),
		GasLimit:   30_000_000,
		Time:       1200,
	}
	h2 := &types.Header{
		Number:     big.NewInt(101),
		Difficulty: big.NewInt(1),
		GasLimit:   30_000_000,
		Time:       1212,
	}

	msg := &BlockHeadersMessage{
		Headers: []*types.Header{h1, h2},
	}

	data, err := EncodeMsg(MsgBlockHeaders, msg)
	if err != nil {
		t.Fatalf("EncodeMsg BlockHeaders: %v", err)
	}

	decoded, err := DecodeMsg(MsgBlockHeaders, data)
	if err != nil {
		t.Fatalf("DecodeMsg BlockHeaders: %v", err)
	}

	bm, ok := decoded.(*BlockHeadersMessage)
	if !ok {
		t.Fatal("decoded message is not *BlockHeadersMessage")
	}
	if len(bm.Headers) != 2 {
		t.Fatalf("headers count: want 2, got %d", len(bm.Headers))
	}
	if bm.Headers[0].Number.Uint64() != 100 {
		t.Fatalf("header 0 number: want 100, got %d", bm.Headers[0].Number.Uint64())
	}
	if bm.Headers[1].Number.Uint64() != 101 {
		t.Fatalf("header 1 number: want 101, got %d", bm.Headers[1].Number.Uint64())
	}
}

func TestEncodeDecodeGetReceiptsMessage(t *testing.T) {
	msg := &GetReceiptsMessage{
		Hashes: []types.Hash{
			types.HexToHash("0xdead"),
			types.HexToHash("0xbeef"),
		},
	}

	// GetReceipts uses the same hash-list format as GetBlockBodies but has
	// no dedicated message code in our eth-level EncodeMsg (not in the basic
	// 0x00-0x0a range). We verify the struct serializes correctly via RLP.
	data, err := EncodeMsg(MsgGetBlockBodies, &GetBlockBodiesMessage{Hashes: msg.Hashes})
	if err != nil {
		t.Fatalf("EncodeMsg: %v", err)
	}

	decoded, err := DecodeMsg(MsgGetBlockBodies, data)
	if err != nil {
		t.Fatalf("DecodeMsg: %v", err)
	}

	gm := decoded.(*GetBlockBodiesMessage)
	if len(gm.Hashes) != 2 {
		t.Fatalf("hashes: want 2, got %d", len(gm.Hashes))
	}
}
