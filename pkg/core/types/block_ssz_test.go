package types

import (
	"math/big"
	"testing"
)

func makeTestHeader() *Header {
	bgu := uint64(131072)
	ebg := uint64(0)
	return &Header{
		ParentHash:  HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
		UncleHash:   EmptyUncleHash,
		Coinbase:    HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		Root:        HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
		TxHash:      HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333"),
		ReceiptHash: HexToHash("0x4444444444444444444444444444444444444444444444444444444444444444"),
		Difficulty:  big.NewInt(0),
		Number:      big.NewInt(1000),
		GasLimit:    30000000,
		GasUsed:     21000,
		Time:        1700000000,
		Extra:       []byte("eth2030"),
		BaseFee:     big.NewInt(1000000000),
		BlobGasUsed: &bgu,
		ExcessBlobGas: &ebg,
	}
}

func makeTestBlock() *Block {
	header := makeTestHeader()
	addr := HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	// Use DynamicFeeTx (type 2) instead of LegacyTx to avoid SSZ
	// type-byte ambiguity on roundtrip (legacy nonce byte can collide
	// with typed tx type prefixes).
	tx := NewTransaction(&DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     1,
		GasTipCap: big.NewInt(2000000000),
		GasFeeCap: big.NewInt(20000000000),
		Gas:       21000,
		To:        &addr,
		Value:     big.NewInt(1000000000000000000),
		V:         big.NewInt(1),
		R:         big.NewInt(12345),
		S:         big.NewInt(67890),
	})

	body := &Body{
		Transactions: []*Transaction{tx},
		Withdrawals: []*Withdrawal{
			{Index: 1, ValidatorIndex: 100, Address: addr, Amount: 32000000000},
		},
	}

	return NewBlock(header, body)
}

func TestHeaderSSZRoundtrip(t *testing.T) {
	header := makeTestHeader()

	encoded, err := HeaderToSSZ(header)
	if err != nil {
		t.Fatalf("HeaderToSSZ error: %v", err)
	}
	if len(encoded) == 0 {
		t.Fatal("encoded header is empty")
	}

	decoded, err := SSZToHeader(encoded)
	if err != nil {
		t.Fatalf("SSZToHeader error: %v", err)
	}

	if decoded.ParentHash != header.ParentHash {
		t.Errorf("ParentHash mismatch: %v != %v", decoded.ParentHash, header.ParentHash)
	}
	if decoded.Coinbase != header.Coinbase {
		t.Errorf("Coinbase mismatch")
	}
	if decoded.Root != header.Root {
		t.Errorf("Root mismatch")
	}
	if decoded.GasLimit != header.GasLimit {
		t.Errorf("GasLimit mismatch: %d != %d", decoded.GasLimit, header.GasLimit)
	}
	if decoded.GasUsed != header.GasUsed {
		t.Errorf("GasUsed mismatch: %d != %d", decoded.GasUsed, header.GasUsed)
	}
	if decoded.Time != header.Time {
		t.Errorf("Time mismatch: %d != %d", decoded.Time, header.Time)
	}
	if decoded.Number.Cmp(header.Number) != 0 {
		t.Errorf("Number mismatch: %v != %v", decoded.Number, header.Number)
	}
	if decoded.BaseFee.Cmp(header.BaseFee) != 0 {
		t.Errorf("BaseFee mismatch: %v != %v", decoded.BaseFee, header.BaseFee)
	}
	if string(decoded.Extra) != string(header.Extra) {
		t.Errorf("Extra mismatch: %q != %q", decoded.Extra, header.Extra)
	}
	if decoded.BlobGasUsed == nil || *decoded.BlobGasUsed != *header.BlobGasUsed {
		t.Errorf("BlobGasUsed mismatch")
	}
}

func TestHeaderSSZEmptyExtra(t *testing.T) {
	header := makeTestHeader()
	header.Extra = nil

	encoded, err := HeaderToSSZ(header)
	if err != nil {
		t.Fatalf("HeaderToSSZ error: %v", err)
	}

	decoded, err := SSZToHeader(encoded)
	if err != nil {
		t.Fatalf("SSZToHeader error: %v", err)
	}

	if len(decoded.Extra) != 0 {
		t.Errorf("expected empty Extra, got %d bytes", len(decoded.Extra))
	}
}

func TestBlockSSZRoundtrip(t *testing.T) {
	block := makeTestBlock()

	encoded, err := BlockToSSZ(block)
	if err != nil {
		t.Fatalf("BlockToSSZ error: %v", err)
	}
	if len(encoded) == 0 {
		t.Fatal("encoded block is empty")
	}

	decoded, err := SSZToBlock(encoded)
	if err != nil {
		t.Fatalf("SSZToBlock error: %v", err)
	}

	// Verify header.
	if decoded.NumberU64() != block.NumberU64() {
		t.Errorf("block number mismatch: %d != %d", decoded.NumberU64(), block.NumberU64())
	}
	if decoded.GasLimit() != block.GasLimit() {
		t.Errorf("gas limit mismatch")
	}
	if decoded.ParentHash() != block.ParentHash() {
		t.Errorf("parent hash mismatch")
	}

	// Verify transactions.
	if len(decoded.Transactions()) != len(block.Transactions()) {
		t.Fatalf("tx count mismatch: %d != %d", len(decoded.Transactions()), len(block.Transactions()))
	}
	origTx := block.Transactions()[0]
	decTx := decoded.Transactions()[0]
	if origTx.Nonce() != decTx.Nonce() {
		t.Errorf("tx nonce mismatch: %d != %d", origTx.Nonce(), decTx.Nonce())
	}
	if origTx.Gas() != decTx.Gas() {
		t.Errorf("tx gas mismatch")
	}
	if origTx.Value().Cmp(decTx.Value()) != 0 {
		t.Errorf("tx value mismatch")
	}

	// Verify withdrawals.
	if len(decoded.Withdrawals()) != len(block.Withdrawals()) {
		t.Fatalf("withdrawal count mismatch: %d != %d", len(decoded.Withdrawals()), len(block.Withdrawals()))
	}
	origWd := block.Withdrawals()[0]
	decWd := decoded.Withdrawals()[0]
	if origWd.Index != decWd.Index {
		t.Errorf("withdrawal index mismatch")
	}
	if origWd.ValidatorIndex != decWd.ValidatorIndex {
		t.Errorf("withdrawal validator index mismatch")
	}
	if origWd.Address != decWd.Address {
		t.Errorf("withdrawal address mismatch")
	}
	if origWd.Amount != decWd.Amount {
		t.Errorf("withdrawal amount mismatch")
	}
}

func TestBlockSSZEmptyBody(t *testing.T) {
	header := makeTestHeader()
	block := NewBlock(header, &Body{})

	encoded, err := BlockToSSZ(block)
	if err != nil {
		t.Fatalf("BlockToSSZ error: %v", err)
	}

	decoded, err := SSZToBlock(encoded)
	if err != nil {
		t.Fatalf("SSZToBlock error: %v", err)
	}

	if len(decoded.Transactions()) != 0 {
		t.Errorf("expected 0 transactions, got %d", len(decoded.Transactions()))
	}
	if decoded.Withdrawals() != nil {
		t.Errorf("expected nil withdrawals, got %d", len(decoded.Withdrawals()))
	}
}

func TestBlockSSZRoot(t *testing.T) {
	block := makeTestBlock()

	root, err := BlockSSZRoot(block)
	if err != nil {
		t.Fatalf("BlockSSZRoot error: %v", err)
	}
	if root == (Hash{}) {
		t.Fatal("expected non-zero root")
	}

	// Same block should produce the same root.
	root2, err := BlockSSZRoot(block)
	if err != nil {
		t.Fatalf("BlockSSZRoot error: %v", err)
	}
	if root != root2 {
		t.Fatal("root mismatch for same block")
	}
}

func TestHeaderSSZRoot(t *testing.T) {
	header := makeTestHeader()

	root, err := HeaderSSZRoot(header)
	if err != nil {
		t.Fatalf("HeaderSSZRoot error: %v", err)
	}
	if root == (Hash{}) {
		t.Fatal("expected non-zero root")
	}
}

func TestBlockSSZMultipleTransactions(t *testing.T) {
	header := makeTestHeader()
	addr := HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	txs := make([]*Transaction, 3)
	for i := 0; i < 3; i++ {
		txs[i] = NewTransaction(&DynamicFeeTx{
			ChainID:   big.NewInt(1),
			Nonce:     uint64(i),
			GasTipCap: big.NewInt(2000000000),
			GasFeeCap: big.NewInt(20000000000),
			Gas:       21000,
			To:        &addr,
			Value:     big.NewInt(int64(i+1) * 1000000000),
			V:         big.NewInt(1),
			R:         big.NewInt(int64(i*111 + 1)),
			S:         big.NewInt(int64(i*222 + 1)),
		})
	}

	body := &Body{Transactions: txs}
	block := NewBlock(header, body)

	encoded, err := BlockToSSZ(block)
	if err != nil {
		t.Fatalf("BlockToSSZ error: %v", err)
	}

	decoded, err := SSZToBlock(encoded)
	if err != nil {
		t.Fatalf("SSZToBlock error: %v", err)
	}

	if len(decoded.Transactions()) != 3 {
		t.Fatalf("expected 3 txs, got %d", len(decoded.Transactions()))
	}

	for i, tx := range decoded.Transactions() {
		if tx.Nonce() != uint64(i) {
			t.Errorf("tx %d nonce mismatch: %d != %d", i, tx.Nonce(), i)
		}
	}
}

func TestBlockSSZTooShort(t *testing.T) {
	_, err := SSZToBlock([]byte{0x01, 0x02})
	if err == nil {
		t.Fatal("expected error for short data")
	}

	_, err = SSZToHeader([]byte{0x01})
	if err == nil {
		t.Fatal("expected error for short header data")
	}
}
