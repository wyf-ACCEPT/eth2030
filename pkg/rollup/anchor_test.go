package rollup

import (
	"encoding/binary"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestNewAnchorContract(t *testing.T) {
	ac := NewAnchorContract()
	if ac == nil {
		t.Fatal("NewAnchorContract returned nil")
	}
	state := ac.GetLatestState()
	if state.BlockNumber != 0 {
		t.Errorf("initial BlockNumber = %d, want 0", state.BlockNumber)
	}
}

func TestAnchorContractUpdateState(t *testing.T) {
	ac := NewAnchorContract()
	state := AnchorState{
		LatestBlockHash: types.BytesToHash([]byte{0x01}),
		LatestStateRoot: types.BytesToHash([]byte{0x02}),
		BlockNumber:     100,
		Timestamp:       1000,
	}

	if err := ac.UpdateState(state); err != nil {
		t.Fatalf("UpdateState error: %v", err)
	}

	got := ac.GetLatestState()
	if got.BlockNumber != 100 {
		t.Errorf("BlockNumber = %d, want 100", got.BlockNumber)
	}
	if got.LatestBlockHash != state.LatestBlockHash {
		t.Error("LatestBlockHash mismatch")
	}
	if got.LatestStateRoot != state.LatestStateRoot {
		t.Error("LatestStateRoot mismatch")
	}
	if got.Timestamp != 1000 {
		t.Errorf("Timestamp = %d, want 1000", got.Timestamp)
	}
}

func TestAnchorContractUpdateStateMonotonic(t *testing.T) {
	ac := NewAnchorContract()
	ac.UpdateState(AnchorState{BlockNumber: 100, Timestamp: 1000})

	// Same block number should fail.
	err := ac.UpdateState(AnchorState{BlockNumber: 100, Timestamp: 2000})
	if err != ErrAnchorStaleBlock {
		t.Errorf("same block error = %v, want ErrAnchorStaleBlock", err)
	}

	// Lower block number should fail.
	err = ac.UpdateState(AnchorState{BlockNumber: 50, Timestamp: 2000})
	if err != ErrAnchorStaleBlock {
		t.Errorf("lower block error = %v, want ErrAnchorStaleBlock", err)
	}

	// Higher block number should succeed.
	err = ac.UpdateState(AnchorState{BlockNumber: 101, Timestamp: 2000})
	if err != nil {
		t.Fatalf("higher block error: %v", err)
	}
}

func TestAnchorContractGetAnchorByNumber(t *testing.T) {
	ac := NewAnchorContract()

	hash := types.BytesToHash([]byte{0xaa})
	root := types.BytesToHash([]byte{0xbb})

	ac.UpdateState(AnchorState{
		LatestBlockHash: hash,
		LatestStateRoot: root,
		BlockNumber:     100,
		Timestamp:       1000,
	})

	entry, found := ac.GetAnchorByNumber(100)
	if !found {
		t.Fatal("GetAnchorByNumber(100) not found")
	}
	if entry.BlockHash != hash {
		t.Error("BlockHash mismatch")
	}
	if entry.StateRoot != root {
		t.Error("StateRoot mismatch")
	}
	if entry.Timestamp != 1000 {
		t.Errorf("Timestamp = %d, want 1000", entry.Timestamp)
	}
}

func TestAnchorContractGetAnchorByNumberZero(t *testing.T) {
	ac := NewAnchorContract()
	_, found := ac.GetAnchorByNumber(0)
	if found {
		t.Error("GetAnchorByNumber(0) should not be found")
	}
}

func TestAnchorContractGetAnchorByNumberFuture(t *testing.T) {
	ac := NewAnchorContract()
	ac.UpdateState(AnchorState{BlockNumber: 100})

	_, found := ac.GetAnchorByNumber(200)
	if found {
		t.Error("GetAnchorByNumber(future) should not be found")
	}
}

func TestAnchorContractGetAnchorByNumberExpired(t *testing.T) {
	ac := NewAnchorContract()

	// Fill beyond ring buffer size.
	for i := uint64(1); i <= AnchorRingBufferSize+100; i++ {
		ac.UpdateState(AnchorState{
			LatestBlockHash: types.BytesToHash([]byte{byte(i)}),
			BlockNumber:     i,
			Timestamp:       i * 10,
		})
	}

	// Block 1 should have been overwritten.
	_, found := ac.GetAnchorByNumber(1)
	if found {
		t.Error("GetAnchorByNumber(expired block) should not be found")
	}

	// Recent block should still be available.
	current := ac.GetLatestState().BlockNumber
	_, found = ac.GetAnchorByNumber(current)
	if !found {
		t.Error("GetAnchorByNumber(current) should be found")
	}
}

func TestProcessAnchorData(t *testing.T) {
	ac := NewAnchorContract()

	hash := types.BytesToHash([]byte{0x01, 0x02, 0x03})
	root := types.BytesToHash([]byte{0x04, 0x05, 0x06})
	blockNum := uint64(42)
	timestamp := uint64(12345)

	state := AnchorState{
		LatestBlockHash: hash,
		LatestStateRoot: root,
		BlockNumber:     blockNum,
		Timestamp:       timestamp,
	}
	data := EncodeAnchorData(state)

	if err := ac.ProcessAnchorData(data); err != nil {
		t.Fatalf("ProcessAnchorData error: %v", err)
	}

	got := ac.GetLatestState()
	if got.LatestBlockHash != hash {
		t.Error("LatestBlockHash mismatch after ProcessAnchorData")
	}
	if got.LatestStateRoot != root {
		t.Error("LatestStateRoot mismatch after ProcessAnchorData")
	}
	if got.BlockNumber != blockNum {
		t.Errorf("BlockNumber = %d, want %d", got.BlockNumber, blockNum)
	}
	if got.Timestamp != timestamp {
		t.Errorf("Timestamp = %d, want %d", got.Timestamp, timestamp)
	}
}

func TestProcessAnchorDataTooShort(t *testing.T) {
	ac := NewAnchorContract()
	err := ac.ProcessAnchorData(make([]byte, 79))
	if err != ErrAnchorDataTooShort {
		t.Errorf("ProcessAnchorData(short) error = %v, want ErrAnchorDataTooShort", err)
	}
}

func TestEncodeAnchorData(t *testing.T) {
	state := AnchorState{
		LatestBlockHash: types.BytesToHash([]byte{0xff}),
		LatestStateRoot: types.BytesToHash([]byte{0xaa}),
		BlockNumber:     1000,
		Timestamp:       2000,
	}
	data := EncodeAnchorData(state)

	if len(data) != 80 {
		t.Fatalf("encoded data length = %d, want 80", len(data))
	}

	// Verify block number encoding.
	blockNum := binary.BigEndian.Uint64(data[64:72])
	if blockNum != 1000 {
		t.Errorf("encoded block number = %d, want 1000", blockNum)
	}

	// Verify timestamp encoding.
	ts := binary.BigEndian.Uint64(data[72:80])
	if ts != 2000 {
		t.Errorf("encoded timestamp = %d, want 2000", ts)
	}

	// Verify block hash.
	var hash types.Hash
	copy(hash[:], data[0:32])
	if hash != state.LatestBlockHash {
		t.Error("encoded block hash mismatch")
	}
}

func TestEncodeDecodeAnchorDataRoundtrip(t *testing.T) {
	original := AnchorState{
		LatestBlockHash: types.BytesToHash([]byte{0x01, 0x02, 0x03, 0x04}),
		LatestStateRoot: types.BytesToHash([]byte{0x05, 0x06, 0x07, 0x08}),
		BlockNumber:     999,
		Timestamp:       88888,
	}

	data := EncodeAnchorData(original)
	ac := NewAnchorContract()
	if err := ac.ProcessAnchorData(data); err != nil {
		t.Fatalf("ProcessAnchorData error: %v", err)
	}

	got := ac.GetLatestState()
	if got.LatestBlockHash != original.LatestBlockHash {
		t.Error("roundtrip LatestBlockHash mismatch")
	}
	if got.LatestStateRoot != original.LatestStateRoot {
		t.Error("roundtrip LatestStateRoot mismatch")
	}
	if got.BlockNumber != original.BlockNumber {
		t.Errorf("roundtrip BlockNumber = %d, want %d", got.BlockNumber, original.BlockNumber)
	}
	if got.Timestamp != original.Timestamp {
		t.Errorf("roundtrip Timestamp = %d, want %d", got.Timestamp, original.Timestamp)
	}
}

func TestAnchorConstants(t *testing.T) {
	if AnchorRingBufferSize != 8191 {
		t.Errorf("AnchorRingBufferSize = %d, want 8191", AnchorRingBufferSize)
	}
	if AnchorSlotBlockHash != 0 {
		t.Errorf("AnchorSlotBlockHash = %d, want 0", AnchorSlotBlockHash)
	}
	if AnchorSlotStateRoot != AnchorRingBufferSize {
		t.Errorf("AnchorSlotStateRoot = %d, want %d", AnchorSlotStateRoot, AnchorRingBufferSize)
	}
	if AnchorSlotLatestBlockNumber != AnchorRingBufferSize*2 {
		t.Errorf("AnchorSlotLatestBlockNumber = %d, want %d", AnchorSlotLatestBlockNumber, AnchorRingBufferSize*2)
	}
}

func TestUpdateAnchorAfterExecute(t *testing.T) {
	t.Run("successful execute advances anchor", func(t *testing.T) {
		ac := NewAnchorContract()
		output := &ExecuteOutput{
			PostStateRoot: types.BytesToHash([]byte{0xAA, 0xBB, 0xCC}),
			ReceiptsRoot:  types.BytesToHash([]byte{0xDD}),
			GasUsed:       42000,
			Success:       true,
		}
		if err := ac.UpdateAnchorAfterExecute(output, 1, 1000); err != nil {
			t.Fatalf("UpdateAnchorAfterExecute: %v", err)
		}
		state := ac.GetLatestState()
		if state.BlockNumber != 1 {
			t.Errorf("BlockNumber = %d, want 1", state.BlockNumber)
		}
		if state.LatestStateRoot != output.PostStateRoot {
			t.Error("LatestStateRoot mismatch")
		}
		if state.Timestamp != 1000 {
			t.Errorf("Timestamp = %d, want 1000", state.Timestamp)
		}
	})

	t.Run("failed execute rejected", func(t *testing.T) {
		ac := NewAnchorContract()
		output := &ExecuteOutput{Success: false}
		if err := ac.UpdateAnchorAfterExecute(output, 1, 1000); err == nil {
			t.Fatal("expected error for failed output")
		}
	})

	t.Run("nil output rejected", func(t *testing.T) {
		ac := NewAnchorContract()
		if err := ac.UpdateAnchorAfterExecute(nil, 1, 1000); err == nil {
			t.Fatal("expected error for nil output")
		}
	})
}

func TestAnchorContractFirstUpdateAllowed(t *testing.T) {
	ac := NewAnchorContract()
	// First update with block 0 should work (state.BlockNumber is 0 initially,
	// but the condition allows it because state.BlockNumber == 0).
	err := ac.UpdateState(AnchorState{BlockNumber: 0, Timestamp: 100})
	if err != nil {
		t.Fatalf("first UpdateState with block 0 error: %v", err)
	}
}
