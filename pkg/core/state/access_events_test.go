package state

import (
	"math"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestNewAccessEvents(t *testing.T) {
	ae := NewAccessEvents()
	if ae == nil {
		t.Fatal("NewAccessEvents should not return nil")
	}
	if len(ae.branches) != 0 || len(ae.chunks) != 0 {
		t.Fatal("new AccessEvents should have empty maps")
	}
}

func TestAccessEventsAddAccountCold(t *testing.T) {
	ae := NewAccessEvents()
	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

	// First access should charge full branch + chunk costs for both basic data and code hash
	gas := ae.AddAccount(addr, false, math.MaxUint64)
	// Expect: branch read (1900) + chunk read (200) for basic data
	// + chunk read (200) for code hash (branch already warm)
	expected := WitnessBranchReadCost + WitnessChunkReadCost + WitnessChunkReadCost
	if gas != expected {
		t.Fatalf("cold AddAccount gas: got %d, want %d", gas, expected)
	}

	// Second access should be free (warm)
	gas = ae.AddAccount(addr, false, math.MaxUint64)
	if gas != 0 {
		t.Fatalf("warm AddAccount gas: got %d, want 0", gas)
	}
}

func TestAccessEventsAddAccountWrite(t *testing.T) {
	ae := NewAccessEvents()
	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

	gas := ae.AddAccount(addr, true, math.MaxUint64)
	// Expect branch read + write, chunk read + write for basic data, plus chunk read + write for code hash
	expected := WitnessBranchReadCost + WitnessBranchWriteCost +
		WitnessChunkReadCost + WitnessChunkWriteCost +
		WitnessChunkReadCost + WitnessChunkWriteCost
	if gas != expected {
		t.Fatalf("cold write AddAccount gas: got %d, want %d", gas, expected)
	}
}

func TestAccessEventsMessageCallGasCold(t *testing.T) {
	ae := NewAccessEvents()
	addr := types.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")

	gas := ae.MessageCallGas(addr, math.MaxUint64)
	expected := WitnessBranchReadCost + WitnessChunkReadCost
	if gas != expected {
		t.Fatalf("cold MessageCallGas: got %d, want %d", gas, expected)
	}
}

func TestAccessEventsMessageCallGasWarm(t *testing.T) {
	ae := NewAccessEvents()
	addr := types.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")

	// Warm it up
	ae.MessageCallGas(addr, math.MaxUint64)

	// Second call should return WarmStorageReadCost
	gas := ae.MessageCallGas(addr, math.MaxUint64)
	if gas != WarmStorageReadCost {
		t.Fatalf("warm MessageCallGas: got %d, want %d", gas, WarmStorageReadCost)
	}
}

func TestAccessEventsValueTransferGas(t *testing.T) {
	ae := NewAccessEvents()
	caller := types.HexToAddress("0x1111111111111111111111111111111111111111")
	target := types.HexToAddress("0x2222222222222222222222222222222222222222")

	gas := ae.ValueTransferGas(caller, target, math.MaxUint64)
	// Each address gets branch read (1900) + branch write (3000) + chunk read (200) + chunk write (500) = 5600
	// Two different addresses = 2 * 5600 = 11200
	expected := uint64(2) * (WitnessBranchReadCost + WitnessBranchWriteCost + WitnessChunkReadCost + WitnessChunkWriteCost)
	if gas != expected {
		t.Fatalf("ValueTransferGas: got %d, want %d", gas, expected)
	}
}

func TestAccessEventsSlotGasCold(t *testing.T) {
	ae := NewAccessEvents()
	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	slot := types.HexToHash("0000000000000000000000000000000000000000000000000000000000000001")

	gas := ae.SlotGas(addr, slot, false, math.MaxUint64, true)
	if gas == 0 {
		t.Fatal("cold SlotGas should be non-zero")
	}
}

func TestAccessEventsSlotGasWarm(t *testing.T) {
	ae := NewAccessEvents()
	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	slot := types.HexToHash("0000000000000000000000000000000000000000000000000000000000000001")

	// Warm it up
	ae.SlotGas(addr, slot, false, math.MaxUint64, true)

	// Second access should return WarmStorageReadCost
	gas := ae.SlotGas(addr, slot, false, math.MaxUint64, true)
	if gas != WarmStorageReadCost {
		t.Fatalf("warm SlotGas: got %d, want %d", gas, WarmStorageReadCost)
	}
}

func TestAccessEventsSlotGasWrite(t *testing.T) {
	ae := NewAccessEvents()
	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	slot := types.HexToHash("0000000000000000000000000000000000000000000000000000000000000001")

	gasRead := ae.SlotGas(addr, slot, false, math.MaxUint64, true)

	// Reset for write test
	ae2 := NewAccessEvents()
	gasWrite := ae2.SlotGas(addr, slot, true, math.MaxUint64, true)

	if gasWrite <= gasRead {
		t.Fatalf("write gas (%d) should be > read gas (%d)", gasWrite, gasRead)
	}
}

func TestAccessEventsBasicDataGas(t *testing.T) {
	ae := NewAccessEvents()
	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

	gas := ae.BasicDataGas(addr, false, math.MaxUint64, true)
	expected := WitnessBranchReadCost + WitnessChunkReadCost
	if gas != expected {
		t.Fatalf("cold BasicDataGas: got %d, want %d", gas, expected)
	}

	gas = ae.BasicDataGas(addr, false, math.MaxUint64, true)
	if gas != WarmStorageReadCost {
		t.Fatalf("warm BasicDataGas: got %d, want %d", gas, WarmStorageReadCost)
	}
}

func TestAccessEventsCodeHashGas(t *testing.T) {
	ae := NewAccessEvents()
	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

	gas := ae.CodeHashGas(addr, false, math.MaxUint64, true)
	if gas == 0 {
		t.Fatal("cold CodeHashGas should be non-zero")
	}

	gas = ae.CodeHashGas(addr, false, math.MaxUint64, true)
	if gas != WarmStorageReadCost {
		t.Fatalf("warm CodeHashGas: got %d, want %d", gas, WarmStorageReadCost)
	}
}

func TestAccessEventsAddTxOrigin(t *testing.T) {
	ae := NewAccessEvents()
	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

	ae.AddTxOrigin(addr)

	// After AddTxOrigin, basic data should be warm (written)
	gas := ae.BasicDataGas(addr, false, math.MaxUint64, true)
	if gas != WarmStorageReadCost {
		t.Fatalf("after AddTxOrigin, BasicDataGas: got %d, want %d", gas, WarmStorageReadCost)
	}

	// Code hash should be warm (read)
	gas = ae.CodeHashGas(addr, false, math.MaxUint64, true)
	if gas != WarmStorageReadCost {
		t.Fatalf("after AddTxOrigin, CodeHashGas: got %d, want %d", gas, WarmStorageReadCost)
	}
}

func TestAccessEventsAddTxDestination(t *testing.T) {
	ae := NewAccessEvents()
	addr := types.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")

	ae.AddTxDestination(addr, true, false)

	gas := ae.BasicDataGas(addr, false, math.MaxUint64, true)
	if gas != WarmStorageReadCost {
		t.Fatalf("after AddTxDestination, BasicDataGas: got %d, want %d", gas, WarmStorageReadCost)
	}
}

func TestAccessEventsMerge(t *testing.T) {
	ae1 := NewAccessEvents()
	ae2 := NewAccessEvents()

	addr1 := types.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := types.HexToAddress("0x2222222222222222222222222222222222222222")

	ae1.AddTxOrigin(addr1)
	ae2.AddTxOrigin(addr2)

	ae1.Merge(ae2)

	// Both addresses should be warm in ae1
	gas1 := ae1.BasicDataGas(addr1, false, math.MaxUint64, true)
	gas2 := ae1.BasicDataGas(addr2, false, math.MaxUint64, true)

	if gas1 != WarmStorageReadCost {
		t.Fatalf("after merge, addr1 BasicDataGas: got %d, want %d", gas1, WarmStorageReadCost)
	}
	if gas2 != WarmStorageReadCost {
		t.Fatalf("after merge, addr2 BasicDataGas: got %d, want %d", gas2, WarmStorageReadCost)
	}
}

func TestAccessEventsCopy(t *testing.T) {
	ae := NewAccessEvents()
	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	ae.AddTxOrigin(addr)

	cp := ae.Copy()
	if cp == ae {
		t.Fatal("copy should be a different object")
	}

	// Copy should have the same warm addresses
	gas := cp.BasicDataGas(addr, false, math.MaxUint64, true)
	if gas != WarmStorageReadCost {
		t.Fatalf("copy BasicDataGas: got %d, want %d", gas, WarmStorageReadCost)
	}

	// Modifying copy should not affect original
	addr2 := types.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")
	cp.AddTxOrigin(addr2)

	gas = ae.BasicDataGas(addr2, false, math.MaxUint64, true)
	if gas == WarmStorageReadCost {
		t.Fatal("modifying copy should not affect original")
	}
}

func TestAccessEventsKeys(t *testing.T) {
	ae := NewAccessEvents()
	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

	ae.AddTxOrigin(addr)
	keys := ae.Keys()

	if len(keys) == 0 {
		t.Fatal("Keys should return non-empty after AddTxOrigin")
	}

	for _, k := range keys {
		if len(k) != 32 {
			t.Fatalf("each key should be 32 bytes, got %d", len(k))
		}
	}
}

func TestAccessEventsContractCreatePreCheckGas(t *testing.T) {
	ae := NewAccessEvents()
	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

	gas := ae.ContractCreatePreCheckGas(addr, math.MaxUint64)
	if gas == 0 {
		t.Fatal("ContractCreatePreCheckGas should charge non-zero gas for cold access")
	}
}

func TestAccessEventsContractCreateInitGas(t *testing.T) {
	ae := NewAccessEvents()
	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

	consumed, expected := ae.ContractCreateInitGas(addr, math.MaxUint64)
	if expected == 0 {
		t.Fatal("ContractCreateInitGas should charge non-zero gas for cold access")
	}
	if consumed != expected {
		t.Fatalf("consumed (%d) should equal expected (%d) when gas is sufficient", consumed, expected)
	}
}

func TestAccessEventsInsufficientGas(t *testing.T) {
	ae := NewAccessEvents()
	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

	// Provide very limited gas
	gas := ae.BasicDataGas(addr, false, 10, true)
	// touchAddressAndChargeGas returns (availableGas, expectedGas) when gas is insufficient.
	// BasicDataGas returns the expected cost (branch read 1900 + chunk read 200 = 2100),
	// indicating how much gas is actually needed.
	expected := WitnessBranchReadCost + WitnessChunkReadCost
	if gas != expected {
		t.Fatalf("with 10 gas available, BasicDataGas should return expected cost %d, got %d", expected, gas)
	}
}

func TestAccessEventsCodeChunksRangeGas(t *testing.T) {
	ae := NewAccessEvents()
	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

	// No code
	consumed, expected := ae.CodeChunksRangeGas(addr, 0, 0, 0, false, math.MaxUint64)
	if consumed != 0 || expected != 0 {
		t.Fatalf("empty code range: consumed=%d, expected=%d", consumed, expected)
	}

	// Start past code length
	consumed, expected = ae.CodeChunksRangeGas(addr, 100, 50, 50, false, math.MaxUint64)
	if consumed != 0 || expected != 0 {
		t.Fatalf("past code length: consumed=%d, expected=%d", consumed, expected)
	}

	// Normal range
	consumed, expected = ae.CodeChunksRangeGas(addr, 0, 62, 100, false, math.MaxUint64)
	if consumed == 0 || expected == 0 {
		t.Fatal("normal code range should have non-zero gas")
	}
	if consumed != expected {
		t.Fatalf("with sufficient gas: consumed=%d, expected=%d", consumed, expected)
	}
}
