package p2p

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestMessageCodes(t *testing.T) {
	// Verify eth/68 message codes match the spec.
	tests := []struct {
		name string
		code uint64
		want uint64
	}{
		{"StatusMsg", StatusMsg, 0x00},
		{"NewBlockHashesMsg", NewBlockHashesMsg, 0x01},
		{"TransactionsMsg", TransactionsMsg, 0x02},
		{"GetBlockHeadersMsg", GetBlockHeadersMsg, 0x03},
		{"BlockHeadersMsg", BlockHeadersMsg, 0x04},
		{"GetBlockBodiesMsg", GetBlockBodiesMsg, 0x05},
		{"BlockBodiesMsg", BlockBodiesMsg, 0x06},
		{"NewBlockMsg", NewBlockMsg, 0x07},
		{"NewPooledTransactionHashesMsg", NewPooledTransactionHashesMsg, 0x08},
		{"GetPooledTransactionsMsg", GetPooledTransactionsMsg, 0x09},
		{"PooledTransactionsMsg", PooledTransactionsMsg, 0x0a},
		{"GetReceiptsMsg", GetReceiptsMsg, 0x0f},
		{"ReceiptsMsg", ReceiptsMsg, 0x10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.code != tt.want {
				t.Errorf("%s = 0x%02x, want 0x%02x", tt.name, tt.code, tt.want)
			}
		})
	}
}

func TestETH68Version(t *testing.T) {
	if ETH68 != 68 {
		t.Errorf("ETH68 = %d, want 68", ETH68)
	}
}

func TestStatusData(t *testing.T) {
	genesis := types.HexToHash("d4e56740f876aef8c010b86a40d5f56745a118d0906a34e69aec8c0db1cb8fa3")
	head := types.HexToHash("abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789")

	sd := StatusData{
		ProtocolVersion: ETH68,
		NetworkID:       1,
		TD:              big.NewInt(1000000),
		Head:            head,
		Genesis:         genesis,
		ForkID:          ForkID{Hash: [4]byte{0xfc, 0x64, 0xec, 0x04}, Next: 0},
	}

	if sd.ProtocolVersion != ETH68 {
		t.Errorf("ProtocolVersion = %d, want %d", sd.ProtocolVersion, ETH68)
	}
	if sd.NetworkID != 1 {
		t.Errorf("NetworkID = %d, want 1", sd.NetworkID)
	}
	if sd.TD.Cmp(big.NewInt(1000000)) != 0 {
		t.Errorf("TD = %v, want 1000000", sd.TD)
	}
	if sd.Head != head {
		t.Errorf("Head = %v, want %v", sd.Head, head)
	}
	if sd.Genesis != genesis {
		t.Errorf("Genesis = %v, want %v", sd.Genesis, genesis)
	}
	if sd.ForkID.Hash != [4]byte{0xfc, 0x64, 0xec, 0x04} {
		t.Errorf("ForkID.Hash = %x, want fc64ec04", sd.ForkID.Hash)
	}
}

func TestHashOrNumber(t *testing.T) {
	t.Run("by hash", func(t *testing.T) {
		h := types.HexToHash("abcd")
		hon := HashOrNumber{Hash: h}
		if !hon.IsHash() {
			t.Error("IsHash() = false for non-zero hash")
		}
	})

	t.Run("by number", func(t *testing.T) {
		hon := HashOrNumber{Number: 12345}
		if hon.IsHash() {
			t.Error("IsHash() = true for zero hash")
		}
		if hon.Number != 12345 {
			t.Errorf("Number = %d, want 12345", hon.Number)
		}
	})
}

func TestGetBlockHeadersRequest(t *testing.T) {
	req := GetBlockHeadersRequest{
		Origin:  HashOrNumber{Number: 100},
		Amount:  64,
		Skip:    0,
		Reverse: false,
	}
	if req.Amount != 64 {
		t.Errorf("Amount = %d, want 64", req.Amount)
	}
	if req.Origin.IsHash() {
		t.Error("expected number-based origin")
	}
	if req.Origin.Number != 100 {
		t.Errorf("Origin.Number = %d, want 100", req.Origin.Number)
	}
}

func TestGetBlockHeadersPacket(t *testing.T) {
	pkt := GetBlockHeadersPacket{
		RequestID: 42,
		Request: GetBlockHeadersRequest{
			Origin:  HashOrNumber{Number: 500},
			Amount:  128,
			Skip:    1,
			Reverse: true,
		},
	}
	if pkt.RequestID != 42 {
		t.Errorf("RequestID = %d, want 42", pkt.RequestID)
	}
	if pkt.Request.Amount != 128 {
		t.Errorf("Amount = %d, want 128", pkt.Request.Amount)
	}
	if !pkt.Request.Reverse {
		t.Error("Reverse = false, want true")
	}
}

func TestBlockHeadersPacket(t *testing.T) {
	pkt := BlockHeadersPacket{
		RequestID: 42,
		Headers:   []*types.Header{},
	}
	if pkt.RequestID != 42 {
		t.Errorf("RequestID = %d, want 42", pkt.RequestID)
	}
	if len(pkt.Headers) != 0 {
		t.Errorf("len(Headers) = %d, want 0", len(pkt.Headers))
	}
}

func TestGetBlockBodiesPacket(t *testing.T) {
	h1 := types.HexToHash("1111")
	h2 := types.HexToHash("2222")
	pkt := GetBlockBodiesPacket{
		RequestID: 7,
		Hashes:    GetBlockBodiesRequest{h1, h2},
	}
	if pkt.RequestID != 7 {
		t.Errorf("RequestID = %d, want 7", pkt.RequestID)
	}
	if len(pkt.Hashes) != 2 {
		t.Errorf("len(Hashes) = %d, want 2", len(pkt.Hashes))
	}
}

func TestNewBlockData(t *testing.T) {
	header := &types.Header{
		Number:     big.NewInt(100),
		Difficulty: big.NewInt(0),
		GasLimit:   30000000,
	}
	block := types.NewBlock(header, nil)
	nbd := NewBlockData{
		Block: block,
		TD:    big.NewInt(999999),
	}
	if nbd.Block.NumberU64() != 100 {
		t.Errorf("Block.NumberU64() = %d, want 100", nbd.Block.NumberU64())
	}
	if nbd.TD.Cmp(big.NewInt(999999)) != 0 {
		t.Errorf("TD = %v, want 999999", nbd.TD)
	}
}

func TestNewBlockHashesEntry(t *testing.T) {
	h := types.HexToHash("abcdef")
	entry := NewBlockHashesEntry{
		Hash:   h,
		Number: 42,
	}
	if entry.Hash != h {
		t.Errorf("Hash mismatch")
	}
	if entry.Number != 42 {
		t.Errorf("Number = %d, want 42", entry.Number)
	}
}

func TestGetReceiptsPacket(t *testing.T) {
	h := types.HexToHash("aabb")
	pkt := GetReceiptsPacket{
		RequestID: 10,
		Hashes:    GetReceiptsRequest{h},
	}
	if pkt.RequestID != 10 {
		t.Errorf("RequestID = %d, want 10", pkt.RequestID)
	}
	if len(pkt.Hashes) != 1 {
		t.Errorf("len(Hashes) = %d, want 1", len(pkt.Hashes))
	}
}

func TestReceiptsPacket(t *testing.T) {
	pkt := ReceiptsPacket{
		RequestID: 11,
		Receipts:  [][]*types.Receipt{{}},
	}
	if pkt.RequestID != 11 {
		t.Errorf("RequestID = %d, want 11", pkt.RequestID)
	}
	if len(pkt.Receipts) != 1 {
		t.Errorf("len(Receipts) = %d, want 1", len(pkt.Receipts))
	}
}

func TestBlockBody(t *testing.T) {
	body := BlockBody{
		Transactions: nil,
		Uncles:       nil,
		Withdrawals:  nil,
	}
	if body.Transactions != nil {
		t.Error("expected nil Transactions")
	}
}

func TestNewPooledTransactionHashesPacket68(t *testing.T) {
	h := types.HexToHash("1234")
	pkt := NewPooledTransactionHashesPacket68{
		Types:  []byte{0x02},
		Sizes:  []uint32{128},
		Hashes: []types.Hash{h},
	}
	if len(pkt.Types) != 1 {
		t.Errorf("len(Types) = %d, want 1", len(pkt.Types))
	}
	if pkt.Types[0] != 0x02 {
		t.Errorf("Types[0] = 0x%02x, want 0x02", pkt.Types[0])
	}
	if pkt.Sizes[0] != 128 {
		t.Errorf("Sizes[0] = %d, want 128", pkt.Sizes[0])
	}
	if pkt.Hashes[0] != h {
		t.Error("Hashes[0] mismatch")
	}
}

func TestGetPooledTransactionsPacket(t *testing.T) {
	h1 := types.HexToHash("aabb")
	h2 := types.HexToHash("ccdd")
	pkt := GetPooledTransactionsPacket{
		RequestID: 99,
		Hashes:    GetPooledTransactionsRequest{h1, h2},
	}
	if pkt.RequestID != 99 {
		t.Errorf("RequestID = %d, want 99", pkt.RequestID)
	}
	if len(pkt.Hashes) != 2 {
		t.Errorf("len(Hashes) = %d, want 2", len(pkt.Hashes))
	}
	if pkt.Hashes[0] != h1 {
		t.Error("Hashes[0] mismatch")
	}
	if pkt.Hashes[1] != h2 {
		t.Error("Hashes[1] mismatch")
	}
}

func TestPooledTransactionsPacket(t *testing.T) {
	pkt := PooledTransactionsPacket{
		RequestID:    55,
		Transactions: []*types.Transaction{},
	}
	if pkt.RequestID != 55 {
		t.Errorf("RequestID = %d, want 55", pkt.RequestID)
	}
	if len(pkt.Transactions) != 0 {
		t.Errorf("len(Transactions) = %d, want 0", len(pkt.Transactions))
	}
}
