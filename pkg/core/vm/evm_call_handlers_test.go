package vm

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestGasForCallBasic(t *testing.T) {
	// Test basic 63/64 rule: 1000 available, 500 requested.
	child, deduction := GasForCall(1000, 500, false)
	if child != 500 {
		t.Fatalf("child gas: got %d, want 500", child)
	}
	if deduction != 500 {
		t.Fatalf("deduction: got %d, want 500", deduction)
	}
}

func TestGasForCallCapped(t *testing.T) {
	// Request more gas than 63/64 allows.
	// 63/64 of 6400 = 6400 - 100 = 6300
	child, deduction := GasForCall(6400, 10000, false)
	if child != 6300 {
		t.Fatalf("child gas should be capped: got %d, want 6300", child)
	}
	if deduction != 6300 {
		t.Fatalf("deduction should match capped: got %d, want 6300", deduction)
	}
}

func TestGasForCallWithStipend(t *testing.T) {
	// With value transfer, child gets extra 2300 stipend.
	child, deduction := GasForCall(10000, 5000, true)
	expected := uint64(5000 + CallStipend)
	if child != expected {
		t.Fatalf("child gas with stipend: got %d, want %d", child, expected)
	}
	if deduction != 5000 {
		t.Fatalf("deduction should NOT include stipend: got %d, want 5000", deduction)
	}
}

func TestGasForCallWithStipendCapped(t *testing.T) {
	// Capped case with stipend.
	// 63/64 of 640 = 630
	child, deduction := GasForCall(640, 10000, true)
	if child != 630+CallStipend {
		t.Fatalf("child gas: got %d, want %d", child, 630+CallStipend)
	}
	if deduction != 630 {
		t.Fatalf("deduction: got %d, want 630", deduction)
	}
}

func TestReturnGasFromCall(t *testing.T) {
	// Without value: all gas returned.
	ret := ReturnGasFromCall(5000, false)
	if ret != 5000 {
		t.Fatalf("expected 5000, got %d", ret)
	}

	// With value: stipend subtracted.
	ret = ReturnGasFromCall(3000, true)
	expected := uint64(3000 - CallStipend)
	if ret != expected {
		t.Fatalf("expected %d, got %d", expected, ret)
	}

	// With value, less gas than stipend.
	ret = ReturnGasFromCall(1000, true)
	if ret != 0 {
		t.Fatalf("expected 0 when gas < stipend, got %d", ret)
	}
}

func TestIsValueTransfer(t *testing.T) {
	if IsValueTransfer(nil) {
		t.Fatal("nil should not be a value transfer")
	}
	if IsValueTransfer(big.NewInt(0)) {
		t.Fatal("zero should not be a value transfer")
	}
	if !IsValueTransfer(big.NewInt(1)) {
		t.Fatal("positive value should be a value transfer")
	}
	if IsValueTransfer(big.NewInt(-1)) {
		t.Fatal("negative value should not be a value transfer")
	}
}

func TestCallValueGasCost(t *testing.T) {
	// No value: zero gas.
	gas := CallValueGasCost(nil, types.Address{}, nil)
	if gas != 0 {
		t.Fatalf("expected 0, got %d", gas)
	}

	gas = CallValueGasCost(nil, types.Address{}, big.NewInt(0))
	if gas != 0 {
		t.Fatalf("expected 0 for zero value, got %d", gas)
	}

	// Value to existing account: just transfer gas.
	stateDB := newMockStateDBForCallTest()
	target := types.HexToAddress("0x1234")
	stateDB.existing[target] = true

	gas = CallValueGasCost(stateDB, target, big.NewInt(100))
	if gas != CallValueTransferGas {
		t.Fatalf("expected %d, got %d", CallValueTransferGas, gas)
	}

	// Value to non-existing account: transfer + new account gas.
	nonExist := types.HexToAddress("0xdead")
	gas = CallValueGasCost(stateDB, nonExist, big.NewInt(100))
	expected := CallValueTransferGas + CallNewAccountGas
	if gas != expected {
		t.Fatalf("expected %d, got %d", expected, gas)
	}
}

func TestWarmTarget(t *testing.T) {
	stateDB := newMockStateDBForCallTest()
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = stateDB

	addr := types.HexToAddress("0xaaaa")

	// First access: cold.
	wasWarm := WarmTarget(evm, addr)
	if wasWarm {
		t.Fatal("expected cold on first access")
	}

	// Second access: warm.
	wasWarm = WarmTarget(evm, addr)
	if !wasWarm {
		t.Fatal("expected warm on second access")
	}
}

func TestWarmTargetNilStateDB(t *testing.T) {
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	// No state: should return true (treat as warm).
	wasWarm := WarmTarget(evm, types.Address{})
	if !wasWarm {
		t.Fatal("expected warm with nil stateDB")
	}
}

func TestCallMemoryGas(t *testing.T) {
	mem := NewMemory()

	// No memory needed.
	gas, ok := CallMemoryGas(mem, 0, 0, 0, 0)
	if !ok {
		t.Fatal("expected ok for zero sizes")
	}
	if gas != 0 {
		t.Fatalf("expected 0 gas, got %d", gas)
	}

	// Input region only.
	gas, ok = CallMemoryGas(mem, 0, 32, 0, 0)
	if !ok {
		t.Fatal("expected ok")
	}
	if gas == 0 {
		t.Fatal("expected non-zero gas for memory expansion")
	}

	// Return region larger than input.
	gas2, ok := CallMemoryGas(mem, 0, 32, 0, 64)
	if !ok {
		t.Fatal("expected ok")
	}
	if gas2 <= gas {
		t.Fatal("expected more gas for larger return region")
	}
}

func TestCopyReturnData(t *testing.T) {
	mem := NewMemory()
	mem.Resize(64)

	returnData := []byte{0x01, 0x02, 0x03, 0x04}

	// Copy full return data.
	CopyReturnData(mem, 0, 4, returnData)
	data := mem.Data()[0:4]
	for i := 0; i < 4; i++ {
		if data[i] != returnData[i] {
			t.Fatalf("byte %d: got %x, want %x", i, data[i], returnData[i])
		}
	}

	// Truncate: output buffer larger than return data.
	mem2 := NewMemory()
	mem2.Resize(64)
	CopyReturnData(mem2, 10, 20, returnData)
	for i := 0; i < 4; i++ {
		if mem2.Data()[10+i] != returnData[i] {
			t.Fatalf("truncated byte %d mismatch", i)
		}
	}

	// Zero-size: should be a no-op.
	CopyReturnData(mem, 0, 0, returnData)

	// Empty return data: should be a no-op.
	CopyReturnData(mem, 0, 4, nil)
}

func TestNewCallHandler(t *testing.T) {
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	handler := NewCallHandler(evm)
	if handler == nil {
		t.Fatal("expected non-nil handler")
	}
	if handler.maxCallDepth != 1024 {
		t.Fatalf("expected max depth 1024, got %d", handler.maxCallDepth)
	}
}

func TestNewCallHandlerCustomDepth(t *testing.T) {
	evm := NewEVM(BlockContext{}, TxContext{}, Config{MaxCallDepth: 512})
	handler := NewCallHandler(evm)
	if handler.maxCallDepth != 512 {
		t.Fatalf("expected max depth 512, got %d", handler.maxCallDepth)
	}
}

func TestCallHandlerDepthExceeded(t *testing.T) {
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.depth = 1025
	handler := NewCallHandler(evm)

	result := handler.HandleCall(&CallHandlerParams{
		Kind:   CallKindCall,
		Caller: types.HexToAddress("0x01"),
		Target: types.HexToAddress("0x02"),
		Gas:    10000,
	})

	if result.Err == nil {
		t.Fatal("expected depth exceeded error")
	}
}

func TestCallHandlerStaticValueTransfer(t *testing.T) {
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	handler := NewCallHandler(evm)

	result := handler.HandleCall(&CallHandlerParams{
		Kind:     CallKindStaticCall,
		Caller:   types.HexToAddress("0x01"),
		Target:   types.HexToAddress("0x02"),
		Value:    big.NewInt(100),
		IsStatic: true,
		Gas:      10000,
	})

	if result.Err == nil {
		t.Fatal("expected write protection error for value in static call")
	}
}

// mockStateDBForCallTest extends mockStateDB with additional fields for
// CALL-family testing.
type mockStateDBForCallTest struct {
	mockStateDB
	existing   map[types.Address]bool
	accessList map[types.Address]bool
}

func newMockStateDBForCallTest() *mockStateDBForCallTest {
	return &mockStateDBForCallTest{
		mockStateDB: *newMockStateDB(),
		existing:    make(map[types.Address]bool),
		accessList:  make(map[types.Address]bool),
	}
}

func (m *mockStateDBForCallTest) Exist(addr types.Address) bool {
	return m.existing[addr]
}

func (m *mockStateDBForCallTest) AddressInAccessList(addr types.Address) bool {
	return m.accessList[addr]
}

func (m *mockStateDBForCallTest) AddAddressToAccessList(addr types.Address) {
	m.accessList[addr] = true
}

func TestColdAccessGasForCall(t *testing.T) {
	stateDB := newMockStateDBForCallTest()
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = stateDB

	addr := types.HexToAddress("0xaaaa")

	// Cold access.
	gas := ColdAccessGasForCall(evm, addr)
	expected := ColdAccountAccessCost - WarmStorageReadCost
	if gas != expected {
		t.Fatalf("cold gas: got %d, want %d", gas, expected)
	}

	// Warm access (already added).
	gas = ColdAccessGasForCall(evm, addr)
	if gas != 0 {
		t.Fatalf("warm gas: got %d, want 0", gas)
	}
}

func TestCallHandlerPrecompile(t *testing.T) {
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	// Default precompiles include ecrecover at 0x01.
	handler := NewCallHandler(evm)

	result := handler.HandleCall(&CallHandlerParams{
		Kind:   CallKindCall,
		Caller: types.HexToAddress("0xaa"),
		Target: types.BytesToAddress([]byte{0x04}), // identity precompile
		Input:  []byte{0x01, 0x02, 0x03},
		Gas:    100000,
	})

	if result.Err != nil {
		t.Fatalf("identity precompile call failed: %v", result.Err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	if len(result.ReturnData) != 3 {
		t.Fatalf("expected 3 bytes return data, got %d", len(result.ReturnData))
	}
}

func TestCallHandlerPrecompileOutOfGas(t *testing.T) {
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	handler := NewCallHandler(evm)

	// Call ecrecover with only 1 gas (needs 3000).
	result := handler.HandleCall(&CallHandlerParams{
		Kind:   CallKindCall,
		Caller: types.HexToAddress("0xaa"),
		Target: types.BytesToAddress([]byte{0x01}), // ecrecover
		Input:  make([]byte, 128),
		Gas:    1,
	})

	if result.Err == nil {
		t.Fatal("expected out of gas error")
	}
}
