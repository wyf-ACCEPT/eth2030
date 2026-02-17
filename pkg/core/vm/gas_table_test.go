package vm

import (
	"math/big"
	"testing"
)

func TestMemoryGasCost(t *testing.T) {
	tests := []struct {
		size uint64
		want uint64
	}{
		{0, 0},
		{32, 3},           // 1 word * 3 + 1^2/512
		{64, 6},           // 2 words * 3 + 4/512
		{1024, 98},        // 32 words * 3 + 32^2/512 = 96 + 2 = 98
		{32768, 5120},     // 1024 words * 3 + 1024^2/512 = 3072 + 2048
	}

	for _, tt := range tests {
		got := MemoryGasCost(tt.size)
		if got != tt.want {
			t.Errorf("MemoryGasCost(%d) = %d, want %d", tt.size, got, tt.want)
		}
	}
}

func TestMemoryExpansionGas(t *testing.T) {
	// No expansion.
	if gas := MemoryExpansionGas(100, 50); gas != 0 {
		t.Errorf("expected 0 for no expansion, got %d", gas)
	}

	// Expansion from 0 to 32.
	gas := MemoryExpansionGas(0, 32)
	expected := MemoryGasCost(32)
	if gas != expected {
		t.Errorf("MemoryExpansionGas(0, 32) = %d, want %d", gas, expected)
	}

	// Expansion from 32 to 64.
	gas = MemoryExpansionGas(32, 64)
	expected = MemoryGasCost(64) - MemoryGasCost(32)
	if gas != expected {
		t.Errorf("MemoryExpansionGas(32, 64) = %d, want %d", gas, expected)
	}
}

func TestToWordSize(t *testing.T) {
	tests := []struct {
		size uint64
		want uint64
	}{
		{0, 0},
		{1, 1},
		{31, 1},
		{32, 1},
		{33, 2},
		{64, 2},
		{65, 3},
	}
	for _, tt := range tests {
		got := toWordSize(tt.size)
		if got != tt.want {
			t.Errorf("toWordSize(%d) = %d, want %d", tt.size, got, tt.want)
		}
	}
}

func TestCallGas(t *testing.T) {
	// 63/64 rule: available gas 6400, requested 10000.
	// maxGas = 6400 - 6400/64 = 6400 - 100 = 6300
	gas := CallGas(6400, 10000)
	if gas != 6300 {
		t.Errorf("CallGas(6400, 10000) = %d, want 6300", gas)
	}

	// Requested less than max.
	gas = CallGas(6400, 5000)
	if gas != 5000 {
		t.Errorf("CallGas(6400, 5000) = %d, want 5000", gas)
	}
}

func TestSstoreGas(t *testing.T) {
	var zero, one, two [32]byte
	one[31] = 1
	two[31] = 2

	// No-op: current == new.
	gas, refund := SstoreGas(zero, zero, zero, false)
	if gas != WarmStorageReadCost {
		t.Errorf("no-op gas = %d, want %d", gas, WarmStorageReadCost)
	}
	if refund != 0 {
		t.Errorf("no-op refund = %d, want 0", refund)
	}

	// Set: 0 -> 1 (original == current == 0).
	gas, refund = SstoreGas(zero, zero, one, false)
	if gas != GasSstoreSet {
		t.Errorf("set gas = %d, want %d", gas, GasSstoreSet)
	}
	if refund != 0 {
		t.Errorf("set refund = %d, want 0", refund)
	}

	// Reset: 1 -> 2 (original == current == 1).
	gas, refund = SstoreGas(one, one, two, false)
	if gas != GasSstoreReset {
		t.Errorf("reset gas = %d, want %d", gas, GasSstoreReset)
	}

	// Clear: 1 -> 0 (original == current == 1).
	gas, refund = SstoreGas(one, one, zero, false)
	if gas != GasSstoreReset {
		t.Errorf("clear gas = %d, want %d", gas, GasSstoreReset)
	}
	if refund <= 0 {
		t.Errorf("clear refund = %d, expected positive", refund)
	}

	// Cold access should add cold cost.
	gas, _ = SstoreGas(zero, zero, one, true)
	if gas != GasSstoreSet+ColdSloadCost {
		t.Errorf("cold set gas = %d, want %d", gas, GasSstoreSet+ColdSloadCost)
	}
}

func TestLogGas(t *testing.T) {
	// LOG0 with 32 bytes of data.
	gas := LogGas(0, 32)
	expected := GasLog + 0*GasLogTopic + 32*GasLogData
	if gas != expected {
		t.Errorf("LOG0(32) gas = %d, want %d", gas, expected)
	}

	// LOG2 with 64 bytes.
	gas = LogGas(2, 64)
	expected = GasLog + 2*GasLogTopic + 64*GasLogData
	if gas != expected {
		t.Errorf("LOG2(64) gas = %d, want %d", gas, expected)
	}
}

func TestSha3Gas(t *testing.T) {
	// 32 bytes = 1 word.
	gas := Sha3Gas(32)
	expected := GasKeccak256 + 1*GasKeccak256Word
	if gas != expected {
		t.Errorf("Sha3Gas(32) = %d, want %d", gas, expected)
	}

	// 0 bytes.
	gas = Sha3Gas(0)
	if gas != GasKeccak256 {
		t.Errorf("Sha3Gas(0) = %d, want %d", gas, GasKeccak256)
	}
}

func TestExpGas(t *testing.T) {
	// Exponent = 0: just base cost.
	gas := ExpGas(big.NewInt(0))
	if gas != GasSlowStep {
		t.Errorf("ExpGas(0) = %d, want %d", gas, GasSlowStep)
	}

	// Exponent = 255 (1 byte).
	gas = ExpGas(big.NewInt(255))
	expected := GasSlowStep + 50*1
	if gas != expected {
		t.Errorf("ExpGas(255) = %d, want %d", gas, expected)
	}

	// Exponent = 256 (2 bytes).
	gas = ExpGas(big.NewInt(256))
	expected = GasSlowStep + 50*2
	if gas != expected {
		t.Errorf("ExpGas(256) = %d, want %d", gas, expected)
	}
}

func TestCopyGas(t *testing.T) {
	gas := CopyGas(32)
	if gas != GasCopy {
		t.Errorf("CopyGas(32) = %d, want %d", gas, GasCopy)
	}

	gas = CopyGas(33)
	if gas != GasCopy*2 {
		t.Errorf("CopyGas(33) = %d, want %d", gas, GasCopy*2)
	}

	gas = CopyGas(0)
	if gas != 0 {
		t.Errorf("CopyGas(0) = %d, want 0", gas)
	}
}

func TestIsZero(t *testing.T) {
	var zero [32]byte
	if !isZero(zero) {
		t.Error("expected zero")
	}

	nonZero := zero
	nonZero[31] = 1
	if isZero(nonZero) {
		t.Error("expected non-zero")
	}
}

// --- Dynamic gas function tests ---

// testStack creates a stack with the given values (first value is bottom).
func testStack(vals ...*big.Int) *Stack {
	s := NewStack()
	for _, v := range vals {
		s.Push(new(big.Int).Set(v))
	}
	return s
}

func TestGasSha3Dynamic(t *testing.T) {
	evm := &EVM{}
	contract := &Contract{}
	mem := NewMemory()

	// SHA3 stack layout: [..., size, offset] with offset on top.
	// stack.Back(0) = offset, stack.Back(1) = size.

	// SHA3 of 64 bytes: 2 words * 6 = 12 gas (plus mem expansion).
	stack := testStack(big.NewInt(64), big.NewInt(0)) // size=64, offset=0
	gas := gasSha3(evm, contract, stack, mem, 64)
	memGas := gasMemExpansion(evm, contract, stack, mem, 64)
	expected := uint64(2*GasKeccak256Word) + memGas
	if gas != expected {
		t.Errorf("gasSha3(64 bytes) = %d, want %d", gas, expected)
	}

	// SHA3 of 0 bytes: no word gas.
	stack = testStack(big.NewInt(0), big.NewInt(0))
	gas = gasSha3(evm, contract, stack, mem, 0)
	if gas != 0 {
		t.Errorf("gasSha3(0 bytes) = %d, want 0", gas)
	}

	// SHA3 of 33 bytes: 2 words.
	stack = testStack(big.NewInt(33), big.NewInt(0)) // size=33, offset=0
	gas = gasSha3(evm, contract, stack, mem, 33)
	expected = 2 * GasKeccak256Word // mem already expanded
	if gas < expected {
		t.Errorf("gasSha3(33 bytes) = %d, want >= %d", gas, expected)
	}
}

func TestGasExpDynamic(t *testing.T) {
	evm := &EVM{}
	contract := &Contract{}
	mem := NewMemory()

	// EXP stack layout: [..., exponent, base] with base on top.
	// stack.Back(0) = base, stack.Back(1) = exponent.

	// Exponent = 0: no dynamic gas.
	stack := testStack(big.NewInt(0), big.NewInt(2)) // exp=0, base=2
	gas := gasExp(evm, contract, stack, mem, 0)
	if gas != 0 {
		t.Errorf("gasExp(exp=0) = %d, want 0", gas)
	}

	// Exponent = 255 (1 byte): 50 * 1 = 50.
	stack = testStack(big.NewInt(255), big.NewInt(2)) // exp=255, base=2
	gas = gasExp(evm, contract, stack, mem, 0)
	if gas != 50 {
		t.Errorf("gasExp(exp=255) = %d, want 50", gas)
	}

	// Exponent = 256 (2 bytes): 50 * 2 = 100.
	stack = testStack(big.NewInt(256), big.NewInt(2)) // exp=256, base=2
	gas = gasExp(evm, contract, stack, mem, 0)
	if gas != 100 {
		t.Errorf("gasExp(exp=256) = %d, want 100", gas)
	}

	// Exponent = large (32 bytes).
	bigExp := new(big.Int).Lsh(big.NewInt(1), 255) // 2^255: 32 bytes
	stack = testStack(bigExp, big.NewInt(2))         // exp=2^255, base=2
	gas = gasExp(evm, contract, stack, mem, 0)
	if gas != 50*32 {
		t.Errorf("gasExp(exp=2^255) = %d, want %d", gas, 50*32)
	}
}

func TestGasCopyDynamic(t *testing.T) {
	evm := &EVM{}
	contract := &Contract{}
	mem := NewMemory()

	// CALLDATACOPY: stack is memOffset, dataOffset, size.
	// gasCopy reads size from stack.Back(2) = bottom of 3 items.
	// Copy 64 bytes (2 words): 2 * 3 = 6 gas + mem.
	stack := testStack(big.NewInt(64), big.NewInt(0), big.NewInt(0))
	gas := gasCopy(evm, contract, stack, mem, 64)
	memGas := gasMemExpansion(evm, contract, stack, mem, 64)
	expected := uint64(2*GasCopy) + memGas
	if gas != expected {
		t.Errorf("gasCopy(64 bytes) = %d, want %d", gas, expected)
	}

	// Copy 0 bytes: no copy gas.
	stack = testStack(big.NewInt(0), big.NewInt(0), big.NewInt(0))
	gas = gasCopy(evm, contract, stack, mem, 0)
	if gas != 0 {
		t.Errorf("gasCopy(0 bytes) = %d, want 0", gas)
	}

	// Copy 33 bytes (2 words): 2 * 3 = 6.
	mem2 := NewMemory()
	stack = testStack(big.NewInt(33), big.NewInt(0), big.NewInt(0))
	gas = gasCopy(evm, contract, stack, mem2, 33)
	memGas = gasMemExpansion(evm, contract, stack, mem2, 33)
	expected = uint64(2*GasCopy) + memGas
	if gas != expected {
		t.Errorf("gasCopy(33 bytes) = %d, want %d", gas, expected)
	}
}

func TestGasLogDynamic(t *testing.T) {
	evm := &EVM{}
	contract := &Contract{}
	mem := NewMemory()

	// LOG0 with 32 bytes of data: 0 topics * 375 + 32 * 8 = 256, plus mem.
	// Stack: offset=0, size=32
	logGasFn := makeGasLog(0)
	stack := testStack(big.NewInt(32), big.NewInt(0))
	gas := logGasFn(evm, contract, stack, mem, 32)
	memGas := gasMemExpansion(evm, contract, stack, mem, 32)
	expected := uint64(0*GasLogTopic+32*GasLogData) + memGas
	if gas != expected {
		t.Errorf("gasLog0(32) = %d, want %d", gas, expected)
	}

	// LOG2 with 64 bytes: 2 * 375 + 64 * 8 = 750 + 512 = 1262, plus mem.
	logGasFn2 := makeGasLog(2)
	mem2 := NewMemory()
	// Stack for LOG2: offset=0, size=64, topic1, topic2
	stack = testStack(big.NewInt(0), big.NewInt(0), big.NewInt(64), big.NewInt(0))
	gas = logGasFn2(evm, contract, stack, mem2, 64)
	memGas = gasMemExpansion(evm, contract, stack, mem2, 64)
	expected = uint64(2*GasLogTopic+64*GasLogData) + memGas
	if gas != expected {
		t.Errorf("gasLog2(64) = %d, want %d", gas, expected)
	}

	// LOG4 with 0 bytes: just topic gas.
	logGasFn4 := makeGasLog(4)
	stack = testStack(big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0))
	gas = logGasFn4(evm, contract, stack, mem, 0)
	expected = 4 * GasLogTopic
	if gas != expected {
		t.Errorf("gasLog4(0) = %d, want %d", gas, expected)
	}
}

func TestGasCreateDynamic(t *testing.T) {
	evm := &EVM{}
	contract := &Contract{}
	mem := NewMemory()

	// CREATE with 64 bytes of init code: 2 words * InitCodeWordGas(2) = 4, plus mem.
	// Stack: value=0, offset=0, length=64
	stack := testStack(big.NewInt(64), big.NewInt(0), big.NewInt(0))
	gas := gasCreateDynamic(evm, contract, stack, mem, 64)
	memGas := gasMemExpansion(evm, contract, stack, mem, 64)
	expected := uint64(2*InitCodeWordGas) + memGas
	if gas != expected {
		t.Errorf("gasCreateDynamic(64) = %d, want %d", gas, expected)
	}

	// CREATE with 0 bytes.
	stack = testStack(big.NewInt(0), big.NewInt(0), big.NewInt(0))
	gas = gasCreateDynamic(evm, contract, stack, mem, 0)
	if gas != 0 {
		t.Errorf("gasCreateDynamic(0) = %d, want 0", gas)
	}
}

func TestGasCreate2Dynamic(t *testing.T) {
	evm := &EVM{}
	contract := &Contract{}
	mem := NewMemory()

	// CREATE2 with 64 bytes of init code: 2 words * (InitCodeWordGas + Keccak256WordGas) = 2 * (2+6) = 16, plus mem.
	// Stack: value=0, offset=0, length=64, salt=0
	stack := testStack(big.NewInt(0), big.NewInt(64), big.NewInt(0), big.NewInt(0))
	gas := gasCreate2Dynamic(evm, contract, stack, mem, 64)
	memGas := gasMemExpansion(evm, contract, stack, mem, 64)
	expected := uint64(2*(InitCodeWordGas+GasKeccak256Word)) + memGas
	if gas != expected {
		t.Errorf("gasCreate2Dynamic(64) = %d, want %d", gas, expected)
	}
}

func TestGasMemExpansion(t *testing.T) {
	evm := &EVM{}
	contract := &Contract{}
	mem := NewMemory()

	// No expansion needed (memorySize == 0).
	stack := testStack(big.NewInt(0))
	gas := gasMemExpansion(evm, contract, stack, mem, 0)
	if gas != 0 {
		t.Errorf("gasMemExpansion(0) = %d, want 0", gas)
	}

	// Expansion from 0 to 32 bytes.
	gas = gasMemExpansion(evm, contract, stack, mem, 32)
	// 1 word: 1*3 + 1*1/512 = 3
	if gas != 3 {
		t.Errorf("gasMemExpansion(0->32) = %d, want 3", gas)
	}

	// After memory grows, expansion should be cheaper.
	mem.Resize(64)
	gas = gasMemExpansion(evm, contract, stack, mem, 32)
	if gas != 0 {
		t.Errorf("gasMemExpansion(already big enough) = %d, want 0", gas)
	}

	// Expand from 64 to 128.
	gas = gasMemExpansion(evm, contract, stack, mem, 128)
	expected := MemoryGasCost(128) - MemoryGasCost(64)
	if gas != expected {
		t.Errorf("gasMemExpansion(64->128) = %d, want %d", gas, expected)
	}
}

func TestGasExtCodeCopyCopy(t *testing.T) {
	evm := &EVM{}
	contract := &Contract{}
	mem := NewMemory()

	// EXTCODECOPY: stack is addr, memOffset, codeOffset, length.
	// Size at stack.Back(3) = bottom item.
	// 64 bytes (2 words): 2 * 3 = 6, plus mem.
	stack := testStack(big.NewInt(64), big.NewInt(0), big.NewInt(0), big.NewInt(0))
	gas := gasExtCodeCopyCopy(evm, contract, stack, mem, 64)
	memGas := gasMemExpansion(evm, contract, stack, mem, 64)
	expected := uint64(2*GasCopy) + memGas
	if gas != expected {
		t.Errorf("gasExtCodeCopyCopy(64) = %d, want %d", gas, expected)
	}
}

func TestGasCallEIP2929ValueTransfer(t *testing.T) {
	// Verify that CALL with value adds CallValueTransferGas.
	evm := &EVM{}
	contract := &Contract{}
	mem := NewMemory()

	// Stack: gas, addr, value, argsOff, argsLen, retOff, retLen
	// CALL with value=0 (no value transfer).
	stack := testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(0), big.NewInt(0), big.NewInt(1000),
	)
	gasNoValue := gasCallEIP2929(evm, contract, stack, mem, 0)

	// CALL with value=1 (value transfer).
	stack = testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(1), big.NewInt(0), big.NewInt(1000),
	)
	gasWithValue := gasCallEIP2929(evm, contract, stack, mem, 0)

	// The difference should be at least CallValueTransferGas.
	diff := gasWithValue - gasNoValue
	if diff < CallValueTransferGas {
		t.Errorf("CALL value transfer gas difference = %d, want >= %d", diff, CallValueTransferGas)
	}
}
