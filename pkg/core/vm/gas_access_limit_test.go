package vm

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestNewAccessGasTracker(t *testing.T) {
	tracker := NewAccessGasTracker(30_000_000, 0.25)
	if tracker == nil {
		t.Fatal("NewAccessGasTracker returned nil")
	}
	// 30M * 0.25 = 7.5M access gas limit.
	if tracker.AccessGasLimit() != 7_500_000 {
		t.Fatalf("access gas limit: got %d, want 7500000", tracker.AccessGasLimit())
	}
	if tracker.AccessGasUsed() != 0 {
		t.Fatal("initial access gas used should be 0")
	}
	if tracker.RemainingAccessGas() != 7_500_000 {
		t.Fatalf("remaining: got %d, want 7500000", tracker.RemainingAccessGas())
	}
}

func TestNewAccessGasTrackerInvalidRatio(t *testing.T) {
	// Invalid ratio should default to 0.25.
	tracker := NewAccessGasTracker(30_000_000, 0)
	if tracker.AccessGasLimit() != 7_500_000 {
		t.Fatalf("zero ratio should default to 0.25: got limit %d", tracker.AccessGasLimit())
	}

	tracker2 := NewAccessGasTracker(30_000_000, -0.5)
	if tracker2.AccessGasLimit() != 7_500_000 {
		t.Fatalf("negative ratio should default to 0.25: got limit %d", tracker2.AccessGasLimit())
	}

	tracker3 := NewAccessGasTracker(30_000_000, 1.5)
	if tracker3.AccessGasLimit() != 7_500_000 {
		t.Fatalf("ratio > 1 should default to 0.25: got limit %d", tracker3.AccessGasLimit())
	}
}

func TestNewAccessGasTrackerWithConfig(t *testing.T) {
	cfg := DefaultAccessGasConfig()
	cfg.AccessGasRatio = 0.5

	tracker, err := NewAccessGasTrackerWithConfig(20_000_000, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 20M * 0.5 = 10M.
	if tracker.AccessGasLimit() != 10_000_000 {
		t.Fatalf("access gas limit: got %d, want 10000000", tracker.AccessGasLimit())
	}

	// Invalid ratio should return error.
	cfg.AccessGasRatio = 0
	_, err = NewAccessGasTrackerWithConfig(20_000_000, cfg)
	if err != ErrInvalidAccessRatio {
		t.Fatalf("expected ErrInvalidAccessRatio, got %v", err)
	}

	cfg.AccessGasRatio = 1.1
	_, err = NewAccessGasTrackerWithConfig(20_000_000, cfg)
	if err != ErrInvalidAccessRatio {
		t.Fatalf("expected ErrInvalidAccessRatio, got %v", err)
	}
}

func TestChargeAccessGasColdAccount(t *testing.T) {
	tracker := NewAccessGasTracker(30_000_000, 0.25)
	addr := types.BytesToAddress([]byte{0x01})
	zeroSlot := types.Hash{}

	// Cold account access (address-level, not slot-level).
	cost, err := tracker.ChargeAccessGas(addr, zeroSlot, false)
	if err != nil {
		t.Fatalf("ChargeAccessGas cold account: %v", err)
	}
	if cost != ColdAccountAccessGas {
		t.Fatalf("cold account gas: got %d, want %d", cost, ColdAccountAccessGas)
	}
	if tracker.AccessGasUsed() != ColdAccountAccessGas {
		t.Fatalf("used: got %d, want %d", tracker.AccessGasUsed(), ColdAccountAccessGas)
	}
}

func TestChargeAccessGasWarmAccount(t *testing.T) {
	tracker := NewAccessGasTracker(30_000_000, 0.25)
	addr := types.BytesToAddress([]byte{0x01})
	zeroSlot := types.Hash{}

	// First access: cold.
	_, err := tracker.ChargeAccessGas(addr, zeroSlot, false)
	if err != nil {
		t.Fatalf("first access: %v", err)
	}

	// Second access: warm.
	cost, err := tracker.ChargeAccessGas(addr, zeroSlot, false)
	if err != nil {
		t.Fatalf("second access: %v", err)
	}
	if cost != WarmAccessGas {
		t.Fatalf("warm account gas: got %d, want %d", cost, WarmAccessGas)
	}
}

func TestChargeAccessGasColdSload(t *testing.T) {
	tracker := NewAccessGasTracker(30_000_000, 0.25)
	addr := types.BytesToAddress([]byte{0x01})
	slot := types.HexToHash("0x01")

	// Cold SLOAD.
	cost, err := tracker.ChargeAccessGas(addr, slot, false)
	if err != nil {
		t.Fatalf("cold SLOAD: %v", err)
	}
	if cost != ColdSloadGas {
		t.Fatalf("cold SLOAD gas: got %d, want %d", cost, ColdSloadGas)
	}
}

func TestChargeAccessGasWarmSload(t *testing.T) {
	tracker := NewAccessGasTracker(30_000_000, 0.25)
	addr := types.BytesToAddress([]byte{0x01})
	slot := types.HexToHash("0x01")

	// First access: cold.
	tracker.WarmSlot(addr, slot)

	// Now warm.
	cost, err := tracker.ChargeAccessGas(addr, slot, false)
	if err != nil {
		t.Fatalf("warm SLOAD: %v", err)
	}
	if cost != WarmAccessGas {
		t.Fatalf("warm SLOAD gas: got %d, want %d", cost, WarmAccessGas)
	}
}

func TestChargeAccessGasColdSstore(t *testing.T) {
	tracker := NewAccessGasTracker(30_000_000, 0.25)
	addr := types.BytesToAddress([]byte{0x01})
	slot := types.HexToHash("0x01")

	// Cold SSTORE.
	cost, err := tracker.ChargeAccessGas(addr, slot, true)
	if err != nil {
		t.Fatalf("cold SSTORE: %v", err)
	}
	if cost != ColdSstoreGas {
		t.Fatalf("cold SSTORE gas: got %d, want %d", cost, ColdSstoreGas)
	}
}

func TestChargeAccessGasWarmSstore(t *testing.T) {
	tracker := NewAccessGasTracker(30_000_000, 0.25)
	addr := types.BytesToAddress([]byte{0x01})
	slot := types.HexToHash("0x01")

	// First write warms the slot.
	_, err := tracker.ChargeAccessGas(addr, slot, true)
	if err != nil {
		t.Fatalf("first SSTORE: %v", err)
	}

	// Second write to same slot: warm.
	cost, err := tracker.ChargeAccessGas(addr, slot, true)
	if err != nil {
		t.Fatalf("second SSTORE: %v", err)
	}
	if cost != WarmAccessGas {
		t.Fatalf("warm SSTORE gas: got %d, want %d", cost, WarmAccessGas)
	}
}

func TestAccessGasExhaustion(t *testing.T) {
	// Use a very small access gas budget.
	tracker := NewAccessGasTracker(10000, 0.5) // limit = 5000
	addr := types.BytesToAddress([]byte{0x01})
	slot := types.HexToHash("0x01")

	// Cold SSTORE costs 5000, exactly at the limit.
	cost, err := tracker.ChargeAccessGas(addr, slot, true)
	if err != nil {
		t.Fatalf("charge at limit: %v", err)
	}
	if cost != ColdSstoreGas {
		t.Fatalf("cost: got %d, want %d", cost, ColdSstoreGas)
	}

	if !tracker.IsAccessExhausted() {
		t.Fatal("should be exhausted after using entire budget")
	}
	if tracker.RemainingAccessGas() != 0 {
		t.Fatalf("remaining: got %d, want 0", tracker.RemainingAccessGas())
	}

	// Any further charge should fail.
	addr2 := types.BytesToAddress([]byte{0x02})
	_, err = tracker.ChargeAccessGas(addr2, types.Hash{}, false)
	if err == nil {
		t.Fatal("should fail when access gas exhausted")
	}
}

func TestAccessGasOverflow(t *testing.T) {
	// Budget allows one cold account access (2600) but not two.
	tracker := NewAccessGasTracker(20000, 0.25) // limit = 5000
	addr1 := types.BytesToAddress([]byte{0x01})
	addr2 := types.BytesToAddress([]byte{0x02})

	_, err := tracker.ChargeAccessGas(addr1, types.Hash{}, false)
	if err != nil {
		t.Fatalf("first charge: %v", err)
	}
	// 5000 - 2600 = 2400 remaining. Another cold account access (2600) should fail.
	_, err = tracker.ChargeAccessGas(addr2, types.Hash{}, false)
	if err == nil {
		t.Fatal("should fail: insufficient access gas for second cold account")
	}
	// Used should not have changed.
	if tracker.AccessGasUsed() != ColdAccountAccessGas {
		t.Fatalf("used should be unchanged: got %d", tracker.AccessGasUsed())
	}
}

func TestWarmAddress(t *testing.T) {
	tracker := NewAccessGasTracker(30_000_000, 0.25)
	addr := types.BytesToAddress([]byte{0xaa})

	if tracker.IsWarm(addr) {
		t.Fatal("address should not be warm initially")
	}

	tracker.WarmAddress(addr)
	if !tracker.IsWarm(addr) {
		t.Fatal("address should be warm after WarmAddress")
	}

	// Accessing a pre-warmed address should cost warm gas.
	cost, err := tracker.ChargeAccessGas(addr, types.Hash{}, false)
	if err != nil {
		t.Fatalf("charge warm address: %v", err)
	}
	if cost != WarmAccessGas {
		t.Fatalf("pre-warmed address gas: got %d, want %d", cost, WarmAccessGas)
	}
}

func TestWarmSlot(t *testing.T) {
	tracker := NewAccessGasTracker(30_000_000, 0.25)
	addr := types.BytesToAddress([]byte{0xbb})
	slot := types.HexToHash("0x42")

	if tracker.IsSlotWarm(addr, slot) {
		t.Fatal("slot should not be warm initially")
	}

	tracker.WarmSlot(addr, slot)
	if !tracker.IsSlotWarm(addr, slot) {
		t.Fatal("slot should be warm after WarmSlot")
	}
	// WarmSlot also warms the address.
	if !tracker.IsWarm(addr) {
		t.Fatal("address should be warm after WarmSlot")
	}
}

func TestResetForBlock(t *testing.T) {
	tracker := NewAccessGasTracker(30_000_000, 0.25)
	addr := types.BytesToAddress([]byte{0x01})
	slot := types.HexToHash("0x01")

	// Use some gas and warm some addresses.
	tracker.ChargeAccessGas(addr, slot, false)
	tracker.WarmAddress(types.BytesToAddress([]byte{0x02}))

	if tracker.AccessGasUsed() == 0 {
		t.Fatal("should have used some gas")
	}

	// Reset for a new block with different gas limit.
	tracker.ResetForBlock(60_000_000)

	if tracker.AccessGasUsed() != 0 {
		t.Fatal("used should be 0 after reset")
	}
	// 60M * 0.25 = 15M.
	if tracker.AccessGasLimit() != 15_000_000 {
		t.Fatalf("new limit: got %d, want 15000000", tracker.AccessGasLimit())
	}
	if tracker.IsWarm(addr) {
		t.Fatal("address should not be warm after reset")
	}
	if tracker.IsSlotWarm(addr, slot) {
		t.Fatal("slot should not be warm after reset")
	}
}

func TestAccessGasDifferentSlotsSameAddress(t *testing.T) {
	tracker := NewAccessGasTracker(30_000_000, 0.25)
	addr := types.BytesToAddress([]byte{0x01})
	slot1 := types.HexToHash("0x01")
	slot2 := types.HexToHash("0x02")

	// Cold access to slot1.
	cost1, _ := tracker.ChargeAccessGas(addr, slot1, false)
	if cost1 != ColdSloadGas {
		t.Fatalf("slot1 cold: got %d, want %d", cost1, ColdSloadGas)
	}

	// Slot2 is still cold even though addr is warm.
	cost2, _ := tracker.ChargeAccessGas(addr, slot2, false)
	if cost2 != ColdSloadGas {
		t.Fatalf("slot2 cold: got %d, want %d", cost2, ColdSloadGas)
	}

	// Slot1 is now warm.
	cost3, _ := tracker.ChargeAccessGas(addr, slot1, false)
	if cost3 != WarmAccessGas {
		t.Fatalf("slot1 warm: got %d, want %d", cost3, WarmAccessGas)
	}
}

func TestAccessGasConcurrency(t *testing.T) {
	tracker := NewAccessGasTracker(30_000_000, 1.0) // large budget

	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			addr := types.BytesToAddress([]byte{byte(idx)})
			slot := types.HexToHash("0x01")
			_, err := tracker.ChargeAccessGas(addr, slot, false)
			if err != nil {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrent access error: %v", err)
	}

	// All 100 cold SLOAD accesses should have been charged.
	expectedUsed := uint64(100) * ColdSloadGas
	if tracker.AccessGasUsed() != expectedUsed {
		t.Fatalf("concurrent used: got %d, want %d", tracker.AccessGasUsed(), expectedUsed)
	}
}

func TestDefaultAccessGasConfig(t *testing.T) {
	cfg := DefaultAccessGasConfig()
	if cfg.ColdAccountGas != ColdAccountAccessGas {
		t.Fatalf("ColdAccountGas: got %d, want %d", cfg.ColdAccountGas, ColdAccountAccessGas)
	}
	if cfg.ColdSloadGas != ColdSloadGas {
		t.Fatalf("ColdSloadGas: got %d, want %d", cfg.ColdSloadGas, ColdSloadGas)
	}
	if cfg.WarmAccessGas != WarmAccessGas {
		t.Fatalf("WarmAccessGas: got %d, want %d", cfg.WarmAccessGas, WarmAccessGas)
	}
	if cfg.ColdSstoreGas != ColdSstoreGas {
		t.Fatalf("ColdSstoreGas: got %d, want %d", cfg.ColdSstoreGas, ColdSstoreGas)
	}
	if cfg.AccessGasRatio != 0.25 {
		t.Fatalf("AccessGasRatio: got %f, want 0.25", cfg.AccessGasRatio)
	}
}

func TestAccessGasFullRatio(t *testing.T) {
	// Ratio of 1.0 means entire block gas limit is access gas.
	tracker := NewAccessGasTracker(10_000_000, 1.0)
	if tracker.AccessGasLimit() != 10_000_000 {
		t.Fatalf("limit with ratio 1.0: got %d, want 10000000", tracker.AccessGasLimit())
	}
}

func TestAccessGasZeroSlotReadAfterWrite(t *testing.T) {
	tracker := NewAccessGasTracker(30_000_000, 0.25)
	addr := types.BytesToAddress([]byte{0x01})
	slot := types.HexToHash("0x01")

	// Cold write.
	cost1, _ := tracker.ChargeAccessGas(addr, slot, true)
	if cost1 != ColdSstoreGas {
		t.Fatalf("cold write: got %d, want %d", cost1, ColdSstoreGas)
	}

	// Read same slot: should be warm now.
	cost2, _ := tracker.ChargeAccessGas(addr, slot, false)
	if cost2 != WarmAccessGas {
		t.Fatalf("read after write: got %d, want %d", cost2, WarmAccessGas)
	}
}

func TestIsAccessExhaustedInitially(t *testing.T) {
	tracker := NewAccessGasTracker(30_000_000, 0.25)
	if tracker.IsAccessExhausted() {
		t.Fatal("should not be exhausted initially")
	}
}

func TestAccessGasWriteToZeroSlot(t *testing.T) {
	// Writing with slot == zero hash but isWrite=true should be treated as
	// a slot-level operation.
	tracker := NewAccessGasTracker(30_000_000, 0.25)
	addr := types.BytesToAddress([]byte{0x01})
	zeroSlot := types.Hash{}

	cost, err := tracker.ChargeAccessGas(addr, zeroSlot, true)
	if err != nil {
		t.Fatalf("write to zero slot: %v", err)
	}
	if cost != ColdSstoreGas {
		t.Fatalf("write to zero slot cost: got %d, want %d", cost, ColdSstoreGas)
	}

	// Second write to same zero slot: warm.
	cost2, err := tracker.ChargeAccessGas(addr, zeroSlot, true)
	if err != nil {
		t.Fatalf("second write to zero slot: %v", err)
	}
	if cost2 != WarmAccessGas {
		t.Fatalf("warm write to zero slot: got %d, want %d", cost2, WarmAccessGas)
	}
}
