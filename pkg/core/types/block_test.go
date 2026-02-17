package types

import (
	"math/big"
	"testing"
)

func TestNewBlockEmpty(t *testing.T) {
	header := &Header{
		Number:     big.NewInt(1),
		Difficulty: big.NewInt(0),
		GasLimit:   30_000_000,
		Time:       1700000000,
	}
	block := NewBlock(header, nil)

	if block.NumberU64() != 1 {
		t.Fatalf("expected number 1, got %d", block.NumberU64())
	}
	if block.GasLimit() != 30_000_000 {
		t.Fatal("GasLimit mismatch")
	}
	if block.Time() != 1700000000 {
		t.Fatal("Time mismatch")
	}
	if len(block.Transactions()) != 0 {
		t.Fatal("empty block should have 0 transactions")
	}
	if len(block.Uncles()) != 0 {
		t.Fatal("empty block should have 0 uncles")
	}
	if block.Withdrawals() != nil {
		t.Fatal("empty block should have nil withdrawals")
	}
}

func TestNewBlockWithBody(t *testing.T) {
	header := &Header{
		Number:     big.NewInt(100),
		Difficulty: big.NewInt(0),
		GasLimit:   30_000_000,
		GasUsed:    42000,
		BaseFee:    big.NewInt(1_000_000_000),
	}

	to := HexToAddress("0xdead")
	tx1 := NewTransaction(&LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(100),
	})
	tx2 := NewTransaction(&DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     1,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(10),
		Gas:       21000,
		To:        &to,
		Value:     big.NewInt(200),
	})

	uncle := &Header{
		Number:     big.NewInt(99),
		Difficulty: big.NewInt(0),
	}

	withdrawal := &Withdrawal{
		Index:          0,
		ValidatorIndex: 42,
		Address:        HexToAddress("0xvalidator"),
		Amount:         1_000_000_000,
	}

	body := &Body{
		Transactions: []*Transaction{tx1, tx2},
		Uncles:       []*Header{uncle},
		Withdrawals:  []*Withdrawal{withdrawal},
	}

	block := NewBlock(header, body)

	if block.NumberU64() != 100 {
		t.Fatalf("expected number 100, got %d", block.NumberU64())
	}
	if len(block.Transactions()) != 2 {
		t.Fatalf("expected 2 transactions, got %d", len(block.Transactions()))
	}
	if len(block.Uncles()) != 1 {
		t.Fatalf("expected 1 uncle, got %d", len(block.Uncles()))
	}
	if len(block.Withdrawals()) != 1 {
		t.Fatalf("expected 1 withdrawal, got %d", len(block.Withdrawals()))
	}
	if block.Withdrawals()[0].ValidatorIndex != 42 {
		t.Fatal("withdrawal validator index mismatch")
	}
	if block.GasUsed() != 42000 {
		t.Fatal("GasUsed mismatch")
	}
	if block.BaseFee().Int64() != 1_000_000_000 {
		t.Fatal("BaseFee mismatch")
	}
}

func TestBlockAccessors(t *testing.T) {
	parentHash := HexToHash("0xparent")
	coinbase := HexToAddress("0xminer")

	header := &Header{
		ParentHash: parentHash,
		UncleHash:  EmptyUncleHash,
		Coinbase:   coinbase,
		Root:       EmptyRootHash,
		TxHash:     EmptyRootHash,
		Number:     big.NewInt(50),
		Difficulty: big.NewInt(1000),
		Extra:      []byte("hello"),
	}
	block := NewBlock(header, nil)

	if block.ParentHash() != parentHash {
		t.Fatal("ParentHash mismatch")
	}
	if block.UncleHash() != EmptyUncleHash {
		t.Fatal("UncleHash mismatch")
	}
	if block.Coinbase() != coinbase {
		t.Fatal("Coinbase mismatch")
	}
	if block.Root() != EmptyRootHash {
		t.Fatal("Root mismatch")
	}
	if block.TxHash() != EmptyRootHash {
		t.Fatal("TxHash mismatch")
	}
	if block.Difficulty().Int64() != 1000 {
		t.Fatal("Difficulty mismatch")
	}
	if string(block.Extra()) != "hello" {
		t.Fatal("Extra mismatch")
	}
}

func TestBlockHeaderCopyIndependence(t *testing.T) {
	header := &Header{
		Number:     big.NewInt(1),
		Difficulty: big.NewInt(100),
	}
	block := NewBlock(header, nil)

	// Mutate original header.
	header.Number.SetInt64(999)
	header.Difficulty.SetInt64(999)

	if block.NumberU64() != 1 {
		t.Fatal("Block number should be independent of original header")
	}
	if block.Difficulty().Int64() != 100 {
		t.Fatal("Block difficulty should be independent of original header")
	}
}

func TestBlockNumber(t *testing.T) {
	block := NewBlock(&Header{Number: big.NewInt(42), Difficulty: big.NewInt(0)}, nil)
	if block.Number().Int64() != 42 {
		t.Fatal("Number() mismatch")
	}
	if block.NumberU64() != 42 {
		t.Fatal("NumberU64() mismatch")
	}
}

func TestBlockNumberNil(t *testing.T) {
	block := NewBlock(&Header{Difficulty: big.NewInt(0)}, nil)
	if block.Number().Int64() != 0 {
		t.Fatal("nil Number should return 0")
	}
	if block.NumberU64() != 0 {
		t.Fatal("nil Number should return 0 for NumberU64")
	}
}

func TestBlockBaseFeeNil(t *testing.T) {
	block := NewBlock(&Header{Number: big.NewInt(0), Difficulty: big.NewInt(0)}, nil)
	if block.BaseFee() != nil {
		t.Fatal("nil BaseFee should return nil")
	}
}

func TestBlockHash(t *testing.T) {
	block := NewBlock(&Header{Number: big.NewInt(1), Difficulty: big.NewInt(0)}, nil)
	h1 := block.Hash()
	h2 := block.Hash()
	if h1 != h2 {
		t.Fatal("Block hash should be consistent")
	}
}

func TestBlockSize(t *testing.T) {
	block := NewBlock(&Header{Number: big.NewInt(1), Difficulty: big.NewInt(0)}, nil)
	size := block.Size()
	if size == 0 {
		t.Fatal("Block size should be non-zero")
	}
	if block.Size() != size {
		t.Fatal("Block size should be cached")
	}
}

func TestWithdrawalStruct(t *testing.T) {
	w := Withdrawal{
		Index:          1,
		ValidatorIndex: 100,
		Address:        HexToAddress("0xvalidator"),
		Amount:         32_000_000_000,
	}
	if w.Index != 1 {
		t.Fatal("Index mismatch")
	}
	if w.ValidatorIndex != 100 {
		t.Fatal("ValidatorIndex mismatch")
	}
	if w.Amount != 32_000_000_000 {
		t.Fatal("Amount mismatch")
	}
}
