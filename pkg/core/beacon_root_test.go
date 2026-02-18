package core

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

func TestProcessBeaconBlockRoot(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	beaconRoot := types.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	header := &types.Header{
		Number:           big.NewInt(1),
		Time:             1000,
		ParentBeaconRoot: &beaconRoot,
	}

	ProcessBeaconBlockRoot(statedb, header)

	// timestamp_idx = 1000 % 8191 = 1000
	// root_idx = 1000 + 8191 = 9191
	timestampSlot := uint64ToHash(1000)
	rootSlot := uint64ToHash(9191)

	// Verify timestamp is stored at timestamp_idx.
	storedTimestamp := statedb.GetState(BeaconRootAddress, timestampSlot)
	expectedTimestamp := uint64ToHash(1000)
	if storedTimestamp != expectedTimestamp {
		t.Fatalf("timestamp mismatch: got %s, want %s", storedTimestamp.Hex(), expectedTimestamp.Hex())
	}

	// Verify beacon root is stored at root_idx.
	storedRoot := statedb.GetState(BeaconRootAddress, rootSlot)
	if storedRoot != beaconRoot {
		t.Fatalf("beacon root mismatch: got %s, want %s", storedRoot.Hex(), beaconRoot.Hex())
	}
}

func TestBeaconBlockRootRingBuffer(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	root1 := types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	root2 := types.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222")

	// First write at time=100.
	header1 := &types.Header{
		Number:           big.NewInt(1),
		Time:             100,
		ParentBeaconRoot: &root1,
	}
	ProcessBeaconBlockRoot(statedb, header1)

	// Verify first write.
	timestampSlot1 := uint64ToHash(100 % historyBufferLength)
	rootSlot1 := uint64ToHash(100%historyBufferLength + historyBufferLength)

	storedRoot := statedb.GetState(BeaconRootAddress, rootSlot1)
	if storedRoot != root1 {
		t.Fatalf("first root mismatch: got %s, want %s", storedRoot.Hex(), root1.Hex())
	}

	// Second write at time = 100 + 8191 (wraps to same slot).
	wrappedTime := uint64(100 + historyBufferLength)
	header2 := &types.Header{
		Number:           big.NewInt(2),
		Time:             wrappedTime,
		ParentBeaconRoot: &root2,
	}
	ProcessBeaconBlockRoot(statedb, header2)

	// The timestamp slot should now have the new timestamp, same index.
	timestampSlot2 := uint64ToHash(wrappedTime % historyBufferLength)
	if timestampSlot1 != timestampSlot2 {
		t.Fatalf("ring buffer slots should be the same: slot1=%s, slot2=%s",
			timestampSlot1.Hex(), timestampSlot2.Hex())
	}

	// Verify the old root is overwritten.
	storedRoot = statedb.GetState(BeaconRootAddress, rootSlot1)
	if storedRoot != root2 {
		t.Fatalf("ring buffer should overwrite old root: got %s, want %s", storedRoot.Hex(), root2.Hex())
	}

	// Verify the timestamp is updated to the new value.
	storedTimestamp := statedb.GetState(BeaconRootAddress, timestampSlot1)
	expectedTimestamp := uint64ToHash(wrappedTime)
	if storedTimestamp != expectedTimestamp {
		t.Fatalf("ring buffer should overwrite old timestamp: got %s, want %s",
			storedTimestamp.Hex(), expectedTimestamp.Hex())
	}
}

func TestBeaconBlockRootNilParentBeaconRoot(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	// Header with nil ParentBeaconRoot (pre-Cancun or missing).
	header := &types.Header{
		Number: big.NewInt(1),
		Time:   1000,
	}

	ProcessBeaconBlockRoot(statedb, header)

	// Nothing should be stored.
	timestampSlot := uint64ToHash(1000 % historyBufferLength)
	rootSlot := uint64ToHash(1000%historyBufferLength + historyBufferLength)

	storedTimestamp := statedb.GetState(BeaconRootAddress, timestampSlot)
	if storedTimestamp != (types.Hash{}) {
		t.Fatalf("expected zero hash for nil ParentBeaconRoot, got %s", storedTimestamp.Hex())
	}
	storedRoot := statedb.GetState(BeaconRootAddress, rootSlot)
	if storedRoot != (types.Hash{}) {
		t.Fatalf("expected zero hash for nil ParentBeaconRoot, got %s", storedRoot.Hex())
	}
}

func TestBeaconBlockRootNotCalledPreCancun(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	beaconRoot := types.HexToHash("0xdeadbeef00000000000000000000000000000000000000000000000000000000")

	// Use a chain config where Cancun is NOT active.
	preCancunConfig := &ChainConfig{
		ChainID:                 big.NewInt(1),
		HomesteadBlock:          big.NewInt(0),
		ByzantiumBlock:          big.NewInt(0),
		ConstantinopleBlock:     big.NewInt(0),
		IstanbulBlock:           big.NewInt(0),
		BerlinBlock:             big.NewInt(0),
		LondonBlock:             big.NewInt(0),
		TerminalTotalDifficulty: big.NewInt(0),
		ShanghaiTime:            newUint64(0),
		CancunTime:              nil, // Cancun NOT active
	}

	header := &types.Header{
		Number:           big.NewInt(1),
		GasLimit:         10_000_000,
		Time:             1000,
		BaseFee:          big.NewInt(1_000_000_000),
		Coinbase:         types.HexToAddress("0xfee"),
		ParentBeaconRoot: &beaconRoot,
	}

	block := types.NewBlock(header, &types.Body{})
	proc := NewStateProcessor(preCancunConfig)
	_, err := proc.Process(block, statedb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Beacon root should NOT be stored because Cancun is not active.
	timestampSlot := uint64ToHash(1000 % historyBufferLength)
	rootSlot := uint64ToHash(1000%historyBufferLength + historyBufferLength)

	storedTimestamp := statedb.GetState(BeaconRootAddress, timestampSlot)
	if storedTimestamp != (types.Hash{}) {
		t.Fatalf("beacon root should NOT be stored pre-Cancun, got timestamp %s", storedTimestamp.Hex())
	}
	storedRoot := statedb.GetState(BeaconRootAddress, rootSlot)
	if storedRoot != (types.Hash{}) {
		t.Fatalf("beacon root should NOT be stored pre-Cancun, got root %s", storedRoot.Hex())
	}
}

func TestBeaconBlockRootCalledPostCancun(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	beaconRoot := types.HexToHash("0xdeadbeef00000000000000000000000000000000000000000000000000000000")

	header := &types.Header{
		Number:           big.NewInt(1),
		GasLimit:         10_000_000,
		Time:             1000,
		BaseFee:          big.NewInt(1_000_000_000),
		Coinbase:         types.HexToAddress("0xfee"),
		ParentBeaconRoot: &beaconRoot,
	}

	block := types.NewBlock(header, &types.Body{})
	proc := NewStateProcessor(TestConfig) // TestConfig has all forks active
	_, err := proc.Process(block, statedb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Beacon root SHOULD be stored because Cancun is active.
	timestampSlot := uint64ToHash(1000 % historyBufferLength)
	rootSlot := uint64ToHash(1000%historyBufferLength + historyBufferLength)

	storedTimestamp := statedb.GetState(BeaconRootAddress, timestampSlot)
	expectedTimestamp := uint64ToHash(1000)
	if storedTimestamp != expectedTimestamp {
		t.Fatalf("timestamp should be stored post-Cancun: got %s, want %s",
			storedTimestamp.Hex(), expectedTimestamp.Hex())
	}
	storedRoot := statedb.GetState(BeaconRootAddress, rootSlot)
	if storedRoot != beaconRoot {
		t.Fatalf("beacon root should be stored post-Cancun: got %s, want %s",
			storedRoot.Hex(), beaconRoot.Hex())
	}
}

func TestBeaconBlockRootMultipleSlots(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	// Write at different timestamps to different ring buffer slots.
	timestamps := []uint64{100, 200, 300, 8190, 8191}
	roots := []types.Hash{
		types.HexToHash("0x0100000000000000000000000000000000000000000000000000000000000000"),
		types.HexToHash("0x0200000000000000000000000000000000000000000000000000000000000000"),
		types.HexToHash("0x0300000000000000000000000000000000000000000000000000000000000000"),
		types.HexToHash("0x0400000000000000000000000000000000000000000000000000000000000000"),
		types.HexToHash("0x0500000000000000000000000000000000000000000000000000000000000000"),
	}

	for i, ts := range timestamps {
		root := roots[i]
		header := &types.Header{
			Number:           big.NewInt(int64(i + 1)),
			Time:             ts,
			ParentBeaconRoot: &root,
		}
		ProcessBeaconBlockRoot(statedb, header)
	}

	// Verify all entries are stored at correct slots.
	for i, ts := range timestamps {
		idx := ts % historyBufferLength
		timestampSlot := uint64ToHash(idx)
		rootSlot := uint64ToHash(idx + historyBufferLength)

		storedTimestamp := statedb.GetState(BeaconRootAddress, timestampSlot)
		expectedTimestamp := uint64ToHash(ts)
		if storedTimestamp != expectedTimestamp {
			t.Errorf("timestamp[%d]: got %s, want %s", i, storedTimestamp.Hex(), expectedTimestamp.Hex())
		}

		storedRoot := statedb.GetState(BeaconRootAddress, rootSlot)
		if storedRoot != roots[i] {
			t.Errorf("root[%d]: got %s, want %s", i, storedRoot.Hex(), roots[i].Hex())
		}
	}
}

func TestBeaconBlockRootTimestampZero(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	beaconRoot := types.HexToHash("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	header := &types.Header{
		Number:           big.NewInt(0),
		Time:             0,
		ParentBeaconRoot: &beaconRoot,
	}

	ProcessBeaconBlockRoot(statedb, header)

	// timestamp_idx = 0 % 8191 = 0
	// root_idx = 0 + 8191 = 8191
	timestampSlot := uint64ToHash(0)
	rootSlot := uint64ToHash(historyBufferLength)

	storedTimestamp := statedb.GetState(BeaconRootAddress, timestampSlot)
	// Timestamp 0 maps to the zero hash, which is the same as an empty slot.
	// The function should still store it (all zeros is a valid timestamp value).
	expectedTimestamp := uint64ToHash(0)
	if storedTimestamp != expectedTimestamp {
		t.Fatalf("timestamp at slot 0: got %s, want %s", storedTimestamp.Hex(), expectedTimestamp.Hex())
	}

	storedRoot := statedb.GetState(BeaconRootAddress, rootSlot)
	if storedRoot != beaconRoot {
		t.Fatalf("root at slot 0: got %s, want %s", storedRoot.Hex(), beaconRoot.Hex())
	}
}

func TestUint64ToHash(t *testing.T) {
	tests := []struct {
		input    uint64
		expected types.Hash
	}{
		{0, types.Hash{}},
		{1, types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")},
		{255, types.HexToHash("0x00000000000000000000000000000000000000000000000000000000000000ff")},
		{256, types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000100")},
		{8191, types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000001fff")},
	}

	for _, tt := range tests {
		got := uint64ToHash(tt.input)
		if got != tt.expected {
			t.Errorf("uint64ToHash(%d): got %s, want %s", tt.input, got.Hex(), tt.expected.Hex())
		}
	}
}
