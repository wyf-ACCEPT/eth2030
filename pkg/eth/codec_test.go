package eth

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/p2p"
)

// TestEncodeDecodeTransactions tests round-tripping transactions through
// the encode/decode pipeline.
func TestEncodeDecodeTransactions(t *testing.T) {
	to := types.HexToAddress("0xbbbb")
	txs := []*types.Transaction{
		types.NewTransaction(&types.LegacyTx{
			Nonce:    0,
			GasPrice: big.NewInt(1000000000),
			Gas:      21000,
			To:       &to,
			Value:    big.NewInt(1000),
		}),
		types.NewTransaction(&types.LegacyTx{
			Nonce:    1,
			GasPrice: big.NewInt(2000000000),
			Gas:      42000,
			To:       &to,
			Value:    big.NewInt(2000),
		}),
	}

	msg, err := encodeTransactions(txs)
	if err != nil {
		t.Fatalf("encodeTransactions error: %v", err)
	}
	if msg.Code != p2p.TransactionsMsg {
		t.Fatalf("want code 0x%02x, got 0x%02x", p2p.TransactionsMsg, msg.Code)
	}
	if msg.Size == 0 {
		t.Fatal("encoded msg size should not be 0")
	}

	decoded, err := decodeTransactions(msg)
	if err != nil {
		t.Fatalf("decodeTransactions error: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("want 2 transactions, got %d", len(decoded))
	}
	if decoded[0].Nonce() != 0 {
		t.Fatalf("want nonce 0, got %d", decoded[0].Nonce())
	}
	if decoded[1].Nonce() != 1 {
		t.Fatalf("want nonce 1, got %d", decoded[1].Nonce())
	}
}

// TestEncodeDecodeTransactions_Empty tests round-tripping an empty list.
func TestEncodeDecodeTransactions_Empty(t *testing.T) {
	msg, err := encodeTransactions([]*types.Transaction{})
	if err != nil {
		t.Fatalf("encodeTransactions error: %v", err)
	}

	decoded, err := decodeTransactions(msg)
	if err != nil {
		t.Fatalf("decodeTransactions error: %v", err)
	}
	if len(decoded) != 0 {
		t.Fatalf("want 0 transactions, got %d", len(decoded))
	}
}

// TestEncodeDecodeTransactions_SingleTx tests with a single transaction.
func TestEncodeDecodeTransactions_SingleTx(t *testing.T) {
	txs := []*types.Transaction{
		types.NewTransaction(&types.LegacyTx{
			Nonce:    42,
			GasPrice: big.NewInt(1000000000),
			Gas:      21000,
		}),
	}

	msg, err := encodeTransactions(txs)
	if err != nil {
		t.Fatalf("encodeTransactions error: %v", err)
	}

	decoded, err := decodeTransactions(msg)
	if err != nil {
		t.Fatalf("decodeTransactions error: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("want 1 transaction, got %d", len(decoded))
	}
	if decoded[0].Nonce() != 42 {
		t.Fatalf("want nonce 42, got %d", decoded[0].Nonce())
	}
}

// TestEncodeDecodeNewBlock tests round-tripping a NewBlockData message.
func TestEncodeDecodeNewBlock(t *testing.T) {
	header := &types.Header{
		Number:   big.NewInt(100),
		GasLimit: 30000000,
		GasUsed:  15000000,
		Time:     1700000000,
		BaseFee:  big.NewInt(1000000000),
	}
	block := types.NewBlock(header, nil)
	td := big.NewInt(12345)

	data := &p2p.NewBlockData{
		Block: block,
		TD:    td,
	}

	msg, err := encodeNewBlock(data)
	if err != nil {
		t.Fatalf("encodeNewBlock error: %v", err)
	}
	if msg.Code != p2p.NewBlockMsg {
		t.Fatalf("want code 0x%02x, got 0x%02x", p2p.NewBlockMsg, msg.Code)
	}
	if msg.Size == 0 {
		t.Fatal("encoded msg size should not be 0")
	}

	decoded, err := decodeNewBlock(msg)
	if err != nil {
		t.Fatalf("decodeNewBlock error: %v", err)
	}
	if decoded.Block == nil {
		t.Fatal("decoded block should not be nil")
	}
	if decoded.Block.NumberU64() != 100 {
		t.Fatalf("want block 100, got %d", decoded.Block.NumberU64())
	}
	if decoded.TD.Cmp(td) != 0 {
		t.Fatalf("want TD %s, got %s", td.String(), decoded.TD.String())
	}
}

// TestEncodeDecodeNewBlock_ZeroTD tests with zero total difficulty.
func TestEncodeDecodeNewBlock_ZeroTD(t *testing.T) {
	block := types.NewBlock(&types.Header{
		Number: big.NewInt(1),
	}, nil)
	data := &p2p.NewBlockData{
		Block: block,
		TD:    big.NewInt(0),
	}

	msg, err := encodeNewBlock(data)
	if err != nil {
		t.Fatalf("encodeNewBlock error: %v", err)
	}

	decoded, err := decodeNewBlock(msg)
	if err != nil {
		t.Fatalf("decodeNewBlock error: %v", err)
	}
	if decoded.TD.Sign() != 0 {
		t.Fatalf("want TD 0, got %s", decoded.TD.String())
	}
}

// TestEncodeDecodeNewBlock_NilTD tests with nil total difficulty (defaults to 0).
func TestEncodeDecodeNewBlock_NilTD(t *testing.T) {
	block := types.NewBlock(&types.Header{
		Number: big.NewInt(1),
	}, nil)
	data := &p2p.NewBlockData{
		Block: block,
		TD:    nil,
	}

	msg, err := encodeNewBlock(data)
	if err != nil {
		t.Fatalf("encodeNewBlock error: %v", err)
	}

	decoded, err := decodeNewBlock(msg)
	if err != nil {
		t.Fatalf("decodeNewBlock error: %v", err)
	}
	if decoded.TD == nil {
		t.Fatal("decoded TD should not be nil")
	}
}

// TestDecodeTransactions_InvalidPayload tests decoding invalid payload.
func TestDecodeTransactions_InvalidPayload(t *testing.T) {
	msg := p2p.Msg{
		Code:    p2p.TransactionsMsg,
		Size:    3,
		Payload: []byte{0x01, 0x02, 0x03}, // invalid RLP
	}
	_, err := decodeTransactions(msg)
	if err == nil {
		t.Fatal("expected error for invalid payload")
	}
}

// TestDecodeNewBlock_InvalidPayload tests decoding invalid new block payload.
func TestDecodeNewBlock_InvalidPayload(t *testing.T) {
	msg := p2p.Msg{
		Code:    p2p.NewBlockMsg,
		Size:    3,
		Payload: []byte{0x01, 0x02, 0x03}, // invalid RLP
	}
	_, err := decodeNewBlock(msg)
	if err == nil {
		t.Fatal("expected error for invalid payload")
	}
}

// TestEncodeTransactions_PreservesOrder tests that encoding preserves tx order.
func TestEncodeTransactions_PreservesOrder(t *testing.T) {
	var txs []*types.Transaction
	for i := uint64(0); i < 5; i++ {
		txs = append(txs, types.NewTransaction(&types.LegacyTx{
			Nonce:    i,
			GasPrice: big.NewInt(1000000000),
			Gas:      21000,
		}))
	}

	msg, err := encodeTransactions(txs)
	if err != nil {
		t.Fatalf("encodeTransactions error: %v", err)
	}

	decoded, err := decodeTransactions(msg)
	if err != nil {
		t.Fatalf("decodeTransactions error: %v", err)
	}
	if len(decoded) != 5 {
		t.Fatalf("want 5, got %d", len(decoded))
	}
	for i := uint64(0); i < 5; i++ {
		if decoded[i].Nonce() != i {
			t.Fatalf("tx[%d] nonce: want %d, got %d", i, i, decoded[i].Nonce())
		}
	}
}
