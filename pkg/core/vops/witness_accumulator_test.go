package vops

import (
	"math/big"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

func TestWitnessAccumulatorNew(t *testing.T) {
	root := types.HexToHash("0x01")
	wa := NewWitnessAccumulator(root)
	if wa == nil {
		t.Fatal("NewWitnessAccumulator returned nil")
	}
	if wa.AccessCount() != 0 {
		t.Errorf("AccessCount = %d, want 0", wa.AccessCount())
	}
}

func TestWitnessAccumulatorRecordAccount(t *testing.T) {
	wa := NewWitnessAccumulator(types.HexToHash("0x01"))
	addr := types.BytesToAddress([]byte{0x10})

	wa.RecordAccountAccess(addr, 5, big.NewInt(1000), types.EmptyCodeHash, types.EmptyRootHash)
	if wa.AccessCount() != 1 {
		t.Errorf("AccessCount = %d, want 1", wa.AccessCount())
	}

	// Duplicate recording should be idempotent.
	wa.RecordAccountAccess(addr, 5, big.NewInt(1000), types.EmptyCodeHash, types.EmptyRootHash)
	if wa.AccessCount() != 1 {
		t.Errorf("AccessCount = %d, want 1 after duplicate", wa.AccessCount())
	}
}

func TestWitnessAccumulatorRecordStorage(t *testing.T) {
	wa := NewWitnessAccumulator(types.HexToHash("0x01"))
	addr := types.BytesToAddress([]byte{0x10})
	slot := types.HexToHash("0xaa")
	value := types.HexToHash("0xbb")

	wa.RecordStorageAccess(addr, slot, value)
	if wa.AccessCount() != 1 {
		t.Errorf("AccessCount = %d, want 1", wa.AccessCount())
	}

	// Duplicate.
	wa.RecordStorageAccess(addr, slot, value)
	if wa.AccessCount() != 1 {
		t.Errorf("AccessCount = %d, want 1 after duplicate", wa.AccessCount())
	}

	// Different slot on same address.
	wa.RecordStorageAccess(addr, types.HexToHash("0xcc"), types.HexToHash("0xdd"))
	if wa.AccessCount() != 2 {
		t.Errorf("AccessCount = %d, want 2", wa.AccessCount())
	}
}

func TestWitnessAccumulatorRecordCode(t *testing.T) {
	wa := NewWitnessAccumulator(types.HexToHash("0x01"))
	addr := types.BytesToAddress([]byte{0x10})
	code := []byte{0x60, 0x00, 0x60, 0x00, 0xf3}

	wa.RecordCodeAccess(addr, code)
	if wa.AccessCount() != 1 {
		t.Errorf("AccessCount = %d, want 1", wa.AccessCount())
	}

	// Verify defensive copy.
	code[0] = 0xFF
	witness := wa.Build()
	for _, e := range witness.Entries {
		if e.Type == AccessTypeCode && e.Address == addr {
			if e.Code[0] == 0xFF {
				t.Error("code was not defensively copied")
			}
		}
	}
}

func TestWitnessAccumulatorBuild(t *testing.T) {
	root := types.HexToHash("0x01")
	wa := NewWitnessAccumulator(root)

	addr1 := types.BytesToAddress([]byte{0x10})
	addr2 := types.BytesToAddress([]byte{0x20})

	wa.RecordAccountAccess(addr1, 1, big.NewInt(500), types.EmptyCodeHash, types.EmptyRootHash)
	wa.RecordStorageAccess(addr1, types.HexToHash("0xaa"), types.HexToHash("0xbb"))
	wa.RecordCodeAccess(addr2, []byte{0x60, 0x00})
	wa.RecordAccountAccess(addr2, 0, big.NewInt(0), crypto.Keccak256Hash([]byte{0x60, 0x00}), types.EmptyRootHash)

	witness := wa.Build()
	if witness == nil {
		t.Fatal("Build returned nil")
	}
	if witness.StateRoot != root {
		t.Errorf("StateRoot = %v, want %v", witness.StateRoot, root)
	}
	if witness.Hash.IsZero() {
		t.Error("witness hash should not be zero")
	}
	// Should have: 2 accounts + 1 storage + 1 code = 4 entries.
	if len(witness.Entries) != 4 {
		t.Fatalf("entries count = %d, want 4", len(witness.Entries))
	}
}

func TestWitnessAccumulatorBuildDeterministic(t *testing.T) {
	root := types.HexToHash("0x01")

	buildWitness := func() *AccumulatedWitness {
		wa := NewWitnessAccumulator(root)
		wa.RecordAccountAccess(types.BytesToAddress([]byte{0x20}), 3, big.NewInt(200), types.EmptyCodeHash, types.EmptyRootHash)
		wa.RecordAccountAccess(types.BytesToAddress([]byte{0x10}), 1, big.NewInt(100), types.EmptyCodeHash, types.EmptyRootHash)
		wa.RecordStorageAccess(types.BytesToAddress([]byte{0x10}), types.HexToHash("0x02"), types.HexToHash("0xbb"))
		wa.RecordStorageAccess(types.BytesToAddress([]byte{0x10}), types.HexToHash("0x01"), types.HexToHash("0xaa"))
		return wa.Build()
	}

	w1 := buildWitness()
	w2 := buildWitness()

	if w1.Hash != w2.Hash {
		t.Error("witness build should be deterministic")
	}
	if len(w1.Entries) != len(w2.Entries) {
		t.Fatal("entry counts differ")
	}
	for i := range w1.Entries {
		if w1.Entries[i].Address != w2.Entries[i].Address {
			t.Errorf("entry %d address differs", i)
		}
		if w1.Entries[i].Type != w2.Entries[i].Type {
			t.Errorf("entry %d type differs", i)
		}
	}
}

func TestWitnessAccumulatorBuildEmpty(t *testing.T) {
	wa := NewWitnessAccumulator(types.HexToHash("0x01"))
	witness := wa.Build()
	if len(witness.Entries) != 0 {
		t.Errorf("entries = %d, want 0", len(witness.Entries))
	}
	// Hash should still be non-zero (commits to state root).
	if witness.Hash.IsZero() {
		t.Error("empty witness hash should not be zero (includes state root)")
	}
}

func TestWitnessAccumulatorAccessLog(t *testing.T) {
	wa := NewWitnessAccumulator(types.HexToHash("0x01"))
	addr1 := types.BytesToAddress([]byte{0x10})
	addr2 := types.BytesToAddress([]byte{0x20})

	wa.RecordAccountAccess(addr1, 0, big.NewInt(0), types.EmptyCodeHash, types.EmptyRootHash)
	wa.RecordStorageAccess(addr1, types.HexToHash("0x01"), types.HexToHash("0xaa"))
	wa.RecordCodeAccess(addr2, []byte{0x60})

	log := wa.AccessLog()
	if len(log) != 3 {
		t.Fatalf("access log length = %d, want 3", len(log))
	}
	if log[0].Type != AccessTypeAccount {
		t.Errorf("log[0].Type = %d, want AccessTypeAccount", log[0].Type)
	}
	if log[1].Type != AccessTypeStorage {
		t.Errorf("log[1].Type = %d, want AccessTypeStorage", log[1].Type)
	}
	if log[2].Type != AccessTypeCode {
		t.Errorf("log[2].Type = %d, want AccessTypeCode", log[2].Type)
	}
}

func TestWitnessAccumulatorReset(t *testing.T) {
	wa := NewWitnessAccumulator(types.HexToHash("0x01"))
	addr := types.BytesToAddress([]byte{0x10})
	wa.RecordAccountAccess(addr, 0, big.NewInt(0), types.EmptyCodeHash, types.EmptyRootHash)
	wa.RecordStorageAccess(addr, types.HexToHash("0x01"), types.HexToHash("0xaa"))

	if wa.AccessCount() != 2 {
		t.Fatalf("AccessCount = %d, want 2 before reset", wa.AccessCount())
	}

	newRoot := types.HexToHash("0x02")
	wa.Reset(newRoot)

	if wa.AccessCount() != 0 {
		t.Errorf("AccessCount = %d, want 0 after reset", wa.AccessCount())
	}
	witness := wa.Build()
	if witness.StateRoot != newRoot {
		t.Errorf("StateRoot = %v, want %v after reset", witness.StateRoot, newRoot)
	}
	if len(witness.Entries) != 0 {
		t.Errorf("entries = %d, want 0 after reset", len(witness.Entries))
	}
}

func TestWitnessAccumulatorWitnessSize(t *testing.T) {
	wa := NewWitnessAccumulator(types.HexToHash("0x01"))
	baseSize := wa.WitnessSize() // just the state root
	if baseSize != 32 {
		t.Errorf("base size = %d, want 32", baseSize)
	}

	addr := types.BytesToAddress([]byte{0x10})
	wa.RecordAccountAccess(addr, 0, big.NewInt(0), types.EmptyCodeHash, types.EmptyRootHash)
	withAccount := wa.WitnessSize()
	if withAccount <= baseSize {
		t.Error("size should increase after recording account")
	}

	wa.RecordStorageAccess(addr, types.HexToHash("0x01"), types.HexToHash("0xaa"))
	withStorage := wa.WitnessSize()
	if withStorage <= withAccount {
		t.Error("size should increase after recording storage")
	}

	wa.RecordCodeAccess(addr, []byte{0x60, 0x00, 0x60, 0x00, 0xf3})
	withCode := wa.WitnessSize()
	if withCode <= withStorage {
		t.Error("size should increase after recording code")
	}
}

func TestWitnessAccumulatorConcurrency(t *testing.T) {
	wa := NewWitnessAccumulator(types.HexToHash("0x01"))
	var wg sync.WaitGroup

	// Concurrent account accesses.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			addr := types.BytesToAddress([]byte{byte(idx)})
			wa.RecordAccountAccess(addr, uint64(idx), big.NewInt(int64(idx*100)), types.EmptyCodeHash, types.EmptyRootHash)
		}(i)
	}

	// Concurrent storage accesses.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			addr := types.BytesToAddress([]byte{byte(idx)})
			slot := types.BytesToHash([]byte{byte(idx)})
			wa.RecordStorageAccess(addr, slot, types.HexToHash("0xaa"))
		}(i)
	}

	// Concurrent code accesses.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			addr := types.BytesToAddress([]byte{byte(idx + 100)})
			wa.RecordCodeAccess(addr, []byte{byte(idx)})
		}(i)
	}

	wg.Wait()

	// All accesses should be recorded (20 accounts + 20 storage + 10 code).
	if wa.AccessCount() != 50 {
		t.Errorf("AccessCount = %d, want 50", wa.AccessCount())
	}

	witness := wa.Build()
	if len(witness.Entries) != 50 {
		t.Errorf("entries = %d, want 50", len(witness.Entries))
	}
}

func TestWitnessAccumulatorBalanceCopy(t *testing.T) {
	wa := NewWitnessAccumulator(types.HexToHash("0x01"))
	addr := types.BytesToAddress([]byte{0x10})
	balance := big.NewInt(1000)

	wa.RecordAccountAccess(addr, 0, balance, types.EmptyCodeHash, types.EmptyRootHash)

	// Mutate original balance.
	balance.SetInt64(9999)

	witness := wa.Build()
	for _, e := range witness.Entries {
		if e.Type == AccessTypeAccount && e.Address == addr {
			if e.Balance.Int64() != 1000 {
				t.Errorf("balance = %d, want 1000 (should be a copy)", e.Balance.Int64())
			}
		}
	}
}

func TestWitnessAccumulatorNilBalance(t *testing.T) {
	wa := NewWitnessAccumulator(types.HexToHash("0x01"))
	addr := types.BytesToAddress([]byte{0x10})

	// Passing nil balance should not panic.
	wa.RecordAccountAccess(addr, 0, nil, types.EmptyCodeHash, types.EmptyRootHash)

	witness := wa.Build()
	if len(witness.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(witness.Entries))
	}
	if witness.Entries[0].Balance != nil {
		t.Errorf("expected nil balance, got %v", witness.Entries[0].Balance)
	}
}

func TestWitnessEntrySorting(t *testing.T) {
	wa := NewWitnessAccumulator(types.HexToHash("0x01"))
	// Record in reverse order.
	wa.RecordAccountAccess(types.BytesToAddress([]byte{0x30}), 0, big.NewInt(0), types.EmptyCodeHash, types.EmptyRootHash)
	wa.RecordAccountAccess(types.BytesToAddress([]byte{0x10}), 0, big.NewInt(0), types.EmptyCodeHash, types.EmptyRootHash)
	wa.RecordAccountAccess(types.BytesToAddress([]byte{0x20}), 0, big.NewInt(0), types.EmptyCodeHash, types.EmptyRootHash)

	witness := wa.Build()
	// Entries should be sorted by address.
	for i := 1; i < len(witness.Entries); i++ {
		if !addressLess(witness.Entries[i-1].Address, witness.Entries[i].Address) {
			t.Errorf("entries not sorted: %v >= %v", witness.Entries[i-1].Address, witness.Entries[i].Address)
		}
	}
}
