package core

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

func TestProcessParentBlockHash(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	parentHash := types.HexToHash("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	parentNumber := uint64(42)

	ProcessParentBlockHash(statedb, parentNumber, parentHash)

	// Verify the hash was stored at the correct slot.
	got := GetHistoricalBlockHash(statedb, parentNumber)
	if got != parentHash {
		t.Fatalf("stored hash = %v, want %v", got, parentHash)
	}
}

func TestProcessParentBlockHash_RingBuffer(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	// Store a hash at slot 0 (block number 0).
	hash0 := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")
	ProcessParentBlockHash(statedb, 0, hash0)

	// Store a hash at slot 0 again via wraparound (block number = HISTORY_SERVE_WINDOW).
	hash1 := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000002")
	ProcessParentBlockHash(statedb, HistoryServeWindow, hash1)

	// The slot should now contain hash1 (overwrites hash0).
	got := GetHistoricalBlockHash(statedb, HistoryServeWindow)
	if got != hash1 {
		t.Fatalf("ring buffer overwrite: got %v, want %v", got, hash1)
	}

	// Reading the same slot with block 0 gives hash1 (slot collision).
	got = GetHistoricalBlockHash(statedb, 0)
	if got != hash1 {
		t.Fatalf("ring buffer slot 0 after overwrite: got %v, want %v", got, hash1)
	}
}

func TestProcessParentBlockHash_MultipleBlocks(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	// Simulate storing parent hashes for blocks 1 through 10.
	hashes := make([]types.Hash, 11)
	for i := uint64(0); i <= 10; i++ {
		h := types.Hash{}
		h[31] = byte(i + 1)
		hashes[i] = h
		ProcessParentBlockHash(statedb, i, h)
	}

	// Verify each hash is retrievable.
	for i := uint64(0); i <= 10; i++ {
		got := GetHistoricalBlockHash(statedb, i)
		if got != hashes[i] {
			t.Errorf("block %d: got %v, want %v", i, got, hashes[i])
		}
	}
}

func TestGetHistoricalBlockHash_NonExistent(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	// Contract doesn't exist yet; should return zero hash.
	got := GetHistoricalBlockHash(statedb, 42)
	if got != (types.Hash{}) {
		t.Fatalf("expected zero hash for non-existent contract, got %v", got)
	}
}

func TestHistoryStorageAddress(t *testing.T) {
	// Verify the address is non-zero.
	if HistoryStorageAddress.IsZero() {
		t.Fatal("HistoryStorageAddress should not be zero")
	}
}

func TestEIP2935_IntegratedWithProcessor(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	// Fund a sender for the block.
	sender := types.HexToAddress("0x1111")
	statedb.AddBalance(sender, new(big.Int).Mul(big.NewInt(10), new(big.Int).SetUint64(1e18)))

	parentHash := types.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")

	header := &types.Header{
		Number:     big.NewInt(100),
		GasLimit:   10_000_000,
		Time:       1000,
		BaseFee:    big.NewInt(1_000_000_000),
		Coinbase:   types.HexToAddress("0xfee"),
		ParentHash: parentHash,
	}

	body := &types.Body{}
	block := types.NewBlock(header, body)

	proc := NewStateProcessor(TestConfig)
	_, err := proc.Process(block, statedb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Since TestConfig has Prague active, the parent hash should be stored.
	// Parent number = 100 - 1 = 99.
	got := GetHistoricalBlockHash(statedb, 99)
	if got != parentHash {
		t.Fatalf("integrated: got %v, want %v", got, parentHash)
	}
}

func TestEIP2935_NotActivePrePrague(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	// Use a config where Prague is not active.
	prePragueConfig := &ChainConfig{
		ChainID:                 big.NewInt(1337),
		HomesteadBlock:          big.NewInt(0),
		EIP150Block:             big.NewInt(0),
		EIP155Block:             big.NewInt(0),
		EIP158Block:             big.NewInt(0),
		ByzantiumBlock:          big.NewInt(0),
		ConstantinopleBlock:     big.NewInt(0),
		PetersburgBlock:         big.NewInt(0),
		IstanbulBlock:           big.NewInt(0),
		BerlinBlock:             big.NewInt(0),
		LondonBlock:             big.NewInt(0),
		TerminalTotalDifficulty: big.NewInt(0),
		ShanghaiTime:            newUint64(0),
		CancunTime:              newUint64(0),
		PragueTime:              nil, // Prague not activated
	}

	parentHash := types.HexToHash("0xdeadbeef00000000000000000000000000000000000000000000000000000000")

	header := &types.Header{
		Number:     big.NewInt(100),
		GasLimit:   10_000_000,
		Time:       1000,
		BaseFee:    big.NewInt(1_000_000_000),
		Coinbase:   types.HexToAddress("0xfee"),
		ParentHash: parentHash,
	}

	body := &types.Body{}
	block := types.NewBlock(header, body)

	proc := NewStateProcessor(prePragueConfig)
	_, err := proc.Process(block, statedb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Prague not active, so history storage should not have the hash.
	got := GetHistoricalBlockHash(statedb, 99)
	if got != (types.Hash{}) {
		t.Fatalf("pre-Prague: expected zero hash, got %v", got)
	}
}

func TestHistoryServeWindowConstant(t *testing.T) {
	if HistoryServeWindow != 8192 {
		t.Fatalf("HistoryServeWindow = %d, want 8192", HistoryServeWindow)
	}
}
