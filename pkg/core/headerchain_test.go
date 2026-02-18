package core

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func genesisHeader() *types.Header {
	blobGasUsed := uint64(0)
	excessBlobGas := uint64(0)
	emptyWHash := types.EmptyRootHash
	return &types.Header{
		Number:          big.NewInt(0),
		GasLimit:        30000000,
		GasUsed:         0,
		Time:            0,
		Difficulty:      new(big.Int),
		BaseFee:         big.NewInt(1000000000),
		WithdrawalsHash: &emptyWHash,
		BlobGasUsed:     &blobGasUsed,
		ExcessBlobGas:   &excessBlobGas,
	}
}

func nextHeader(parent *types.Header) *types.Header {
	blobGasUsed := uint64(0)
	var parentExcess, parentUsed uint64
	if parent.ExcessBlobGas != nil {
		parentExcess = *parent.ExcessBlobGas
	}
	if parent.BlobGasUsed != nil {
		parentUsed = *parent.BlobGasUsed
	}
	excessBlobGas := CalcExcessBlobGas(parentExcess, parentUsed)
	emptyWHash := types.EmptyRootHash
	return &types.Header{
		ParentHash:      parent.Hash(),
		Number:          new(big.Int).Add(parent.Number, big.NewInt(1)),
		GasLimit:        parent.GasLimit,
		GasUsed:         parent.GasLimit / 2, // exactly at target
		Time:            parent.Time + 12,
		Difficulty:      new(big.Int),
		BaseFee:         CalcBaseFee(parent),
		WithdrawalsHash: &emptyWHash,
		BlobGasUsed:     &blobGasUsed,
		ExcessBlobGas:   &excessBlobGas,
	}
}

func TestHeaderChain_Genesis(t *testing.T) {
	genesis := genesisHeader()
	hc := NewHeaderChain(TestConfig, genesis)

	if hc.ChainLength() != 1 {
		t.Fatalf("chain length: want 1, got %d", hc.ChainLength())
	}

	current := hc.CurrentHeader()
	if current.Number.Uint64() != 0 {
		t.Fatalf("current header number: want 0, got %d", current.Number.Uint64())
	}
}

func TestHeaderChain_InsertSingle(t *testing.T) {
	genesis := genesisHeader()
	hc := NewHeaderChain(TestConfig, genesis)

	h1 := nextHeader(genesis)
	n, err := hc.InsertHeaders([]*types.Header{h1})
	if err != nil {
		t.Fatalf("InsertHeaders: %v", err)
	}
	if n != 1 {
		t.Fatalf("inserted: want 1, got %d", n)
	}

	if hc.CurrentHeader().Number.Uint64() != 1 {
		t.Fatalf("current number: want 1, got %d", hc.CurrentHeader().Number.Uint64())
	}
}

func TestHeaderChain_InsertChain(t *testing.T) {
	genesis := genesisHeader()
	hc := NewHeaderChain(TestConfig, genesis)

	headers := make([]*types.Header, 10)
	parent := genesis
	for i := 0; i < 10; i++ {
		headers[i] = nextHeader(parent)
		parent = headers[i]
	}

	n, err := hc.InsertHeaders(headers)
	if err != nil {
		t.Fatalf("InsertHeaders: %v", err)
	}
	if n != 10 {
		t.Fatalf("inserted: want 10, got %d", n)
	}

	if hc.CurrentHeader().Number.Uint64() != 10 {
		t.Fatalf("current number: want 10, got %d", hc.CurrentHeader().Number.Uint64())
	}

	if hc.ChainLength() != 11 { // genesis + 10
		t.Fatalf("chain length: want 11, got %d", hc.ChainLength())
	}
}

func TestHeaderChain_GetByNumber(t *testing.T) {
	genesis := genesisHeader()
	hc := NewHeaderChain(TestConfig, genesis)

	h1 := nextHeader(genesis)
	h2 := nextHeader(h1)
	hc.InsertHeaders([]*types.Header{h1, h2})

	got := hc.GetHeaderByNumber(1)
	if got == nil {
		t.Fatal("GetHeaderByNumber(1): nil")
	}
	if got.Hash() != h1.Hash() {
		t.Fatal("GetHeaderByNumber(1): wrong header")
	}
}

func TestHeaderChain_GetByHash(t *testing.T) {
	genesis := genesisHeader()
	hc := NewHeaderChain(TestConfig, genesis)

	h1 := nextHeader(genesis)
	hc.InsertHeaders([]*types.Header{h1})

	got := hc.GetHeader(h1.Hash())
	if got == nil {
		t.Fatal("GetHeader by hash: nil")
	}
	if got.Number.Uint64() != 1 {
		t.Fatalf("GetHeader by hash: want number 1, got %d", got.Number.Uint64())
	}
}

func TestHeaderChain_HasHeader(t *testing.T) {
	genesis := genesisHeader()
	hc := NewHeaderChain(TestConfig, genesis)

	if !hc.HasHeader(genesis.Hash()) {
		t.Fatal("should have genesis")
	}

	h1 := nextHeader(genesis)
	if hc.HasHeader(h1.Hash()) {
		t.Fatal("should not have h1 before insert")
	}

	hc.InsertHeaders([]*types.Header{h1})
	if !hc.HasHeader(h1.Hash()) {
		t.Fatal("should have h1 after insert")
	}
}

func TestHeaderChain_InvalidHeader(t *testing.T) {
	genesis := genesisHeader()
	hc := NewHeaderChain(TestConfig, genesis)

	badBlobGas := uint64(0)
	badExcess := uint64(0)
	badWHash := types.EmptyRootHash
	bad := &types.Header{
		ParentHash:      genesis.Hash(),
		Number:          big.NewInt(999), // wrong number
		GasLimit:        genesis.GasLimit,
		Time:            genesis.Time + 12,
		Difficulty:      new(big.Int),
		BaseFee:         CalcBaseFee(genesis),
		WithdrawalsHash: &badWHash,
		BlobGasUsed:     &badBlobGas,
		ExcessBlobGas:   &badExcess,
	}

	_, err := hc.InsertHeaders([]*types.Header{bad})
	if err == nil {
		t.Fatal("expected error for invalid header")
	}
}

func TestHeaderChain_UnknownParent(t *testing.T) {
	genesis := genesisHeader()
	hc := NewHeaderChain(TestConfig, genesis)

	orphanBlobGas := uint64(0)
	orphanExcess := uint64(0)
	orphanWHash := types.EmptyRootHash
	orphan := &types.Header{
		ParentHash:      types.Hash{0xff}, // unknown parent
		Number:          big.NewInt(1),
		GasLimit:        genesis.GasLimit,
		Time:            genesis.Time + 12,
		Difficulty:      new(big.Int),
		WithdrawalsHash: &orphanWHash,
		BlobGasUsed:     &orphanBlobGas,
		ExcessBlobGas:   &orphanExcess,
	}

	_, err := hc.InsertHeaders([]*types.Header{orphan})
	if err == nil {
		t.Fatal("expected error for unknown parent")
	}
}

func TestHeaderChain_SetHead(t *testing.T) {
	genesis := genesisHeader()
	hc := NewHeaderChain(TestConfig, genesis)

	headers := make([]*types.Header, 5)
	parent := genesis
	for i := 0; i < 5; i++ {
		headers[i] = nextHeader(parent)
		parent = headers[i]
	}
	hc.InsertHeaders(headers)

	if hc.CurrentHeader().Number.Uint64() != 5 {
		t.Fatal("should be at block 5")
	}

	hc.SetHead(3)
	if hc.CurrentHeader().Number.Uint64() != 3 {
		t.Fatalf("after SetHead(3): want 3, got %d", hc.CurrentHeader().Number.Uint64())
	}

	// Headers 4 and 5 should be gone.
	if hc.GetHeaderByNumber(4) != nil {
		t.Fatal("header 4 should be removed")
	}
	if hc.GetHeaderByNumber(5) != nil {
		t.Fatal("header 5 should be removed")
	}
}

func TestHeaderChain_DuplicateInsert(t *testing.T) {
	genesis := genesisHeader()
	hc := NewHeaderChain(TestConfig, genesis)

	h1 := nextHeader(genesis)
	hc.InsertHeaders([]*types.Header{h1})

	// Re-insert same header should be idempotent.
	n, err := hc.InsertHeaders([]*types.Header{h1})
	if err != nil {
		t.Fatalf("duplicate insert should not error: %v", err)
	}
	if n != 1 {
		t.Fatalf("duplicate insert: want 1, got %d", n)
	}
}

func TestHeaderChain_GetAncestor(t *testing.T) {
	genesis := genesisHeader()
	hc := NewHeaderChain(TestConfig, genesis)

	headers := make([]*types.Header, 5)
	parent := genesis
	for i := 0; i < 5; i++ {
		headers[i] = nextHeader(parent)
		parent = headers[i]
	}
	hc.InsertHeaders(headers)

	// Get 3rd ancestor of block 5.
	ancestor := hc.GetAncestor(headers[4].Hash(), 3)
	if ancestor == nil {
		t.Fatal("GetAncestor: nil")
	}
	if ancestor.Number.Uint64() != 2 {
		t.Fatalf("GetAncestor(5, 3): want block 2, got %d", ancestor.Number.Uint64())
	}

	// Get genesis from block 5.
	genesis2 := hc.GetAncestor(headers[4].Hash(), 5)
	if genesis2 == nil {
		t.Fatal("GetAncestor to genesis: nil")
	}
	if genesis2.Number.Uint64() != 0 {
		t.Fatalf("GetAncestor(5, 5): want block 0, got %d", genesis2.Number.Uint64())
	}
}
