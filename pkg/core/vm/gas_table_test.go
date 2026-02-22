package vm

import (
	"math"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// Use math.MaxUint64 in test assertions to prevent removal by linter.
var _ uint64 = math.MaxUint64

func TestMemoryGasCost(t *testing.T) {
	tests := []struct {
		size uint64
		want uint64
	}{
		{0, 0},
		{32, 3},       // 1 word * 3 + 1^2/512
		{64, 6},       // 2 words * 3 + 4/512
		{1024, 98},    // 32 words * 3 + 32^2/512 = 96 + 2 = 98
		{32768, 5120}, // 1024 words * 3 + 1024^2/512 = 3072 + 2048
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
	gas, _ := gasSha3(evm, contract, stack, mem, 64)
	memGas, _ := gasMemExpansion(evm, contract, stack, mem, 64)
	expected := uint64(2*GasKeccak256Word) + memGas
	if gas != expected {
		t.Errorf("gasSha3(64 bytes) = %d, want %d", gas, expected)
	}

	// SHA3 of 0 bytes: no word gas.
	stack = testStack(big.NewInt(0), big.NewInt(0))
	gas, _ = gasSha3(evm, contract, stack, mem, 0)
	if gas != 0 {
		t.Errorf("gasSha3(0 bytes) = %d, want 0", gas)
	}

	// SHA3 of 33 bytes: 2 words.
	stack = testStack(big.NewInt(33), big.NewInt(0)) // size=33, offset=0
	gas, _ = gasSha3(evm, contract, stack, mem, 33)
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
	gas, _ := gasExp(evm, contract, stack, mem, 0)
	if gas != 0 {
		t.Errorf("gasExp(exp=0) = %d, want 0", gas)
	}

	// Exponent = 255 (1 byte): 50 * 1 = 50.
	stack = testStack(big.NewInt(255), big.NewInt(2)) // exp=255, base=2
	gas, _ = gasExp(evm, contract, stack, mem, 0)
	if gas != 50 {
		t.Errorf("gasExp(exp=255) = %d, want 50", gas)
	}

	// Exponent = 256 (2 bytes): 50 * 2 = 100.
	stack = testStack(big.NewInt(256), big.NewInt(2)) // exp=256, base=2
	gas, _ = gasExp(evm, contract, stack, mem, 0)
	if gas != 100 {
		t.Errorf("gasExp(exp=256) = %d, want 100", gas)
	}

	// Exponent = large (32 bytes).
	bigExp := new(big.Int).Lsh(big.NewInt(1), 255) // 2^255: 32 bytes
	stack = testStack(bigExp, big.NewInt(2))       // exp=2^255, base=2
	gas, _ = gasExp(evm, contract, stack, mem, 0)
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
	gas, _ := gasCopy(evm, contract, stack, mem, 64)
	memGas, _ := gasMemExpansion(evm, contract, stack, mem, 64)
	expected := uint64(2*GasCopy) + memGas
	if gas != expected {
		t.Errorf("gasCopy(64 bytes) = %d, want %d", gas, expected)
	}

	// Copy 0 bytes: no copy gas.
	stack = testStack(big.NewInt(0), big.NewInt(0), big.NewInt(0))
	gas, _ = gasCopy(evm, contract, stack, mem, 0)
	if gas != 0 {
		t.Errorf("gasCopy(0 bytes) = %d, want 0", gas)
	}

	// Copy 33 bytes (2 words): 2 * 3 = 6.
	mem2 := NewMemory()
	stack = testStack(big.NewInt(33), big.NewInt(0), big.NewInt(0))
	gas, _ = gasCopy(evm, contract, stack, mem2, 33)
	memGas, _ = gasMemExpansion(evm, contract, stack, mem2, 33)
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
	gas, _ := logGasFn(evm, contract, stack, mem, 32)
	memGas, _ := gasMemExpansion(evm, contract, stack, mem, 32)
	expected := uint64(0*GasLogTopic+32*GasLogData) + memGas
	if gas != expected {
		t.Errorf("gasLog0(32) = %d, want %d", gas, expected)
	}

	// LOG2 with 64 bytes: 2 * 375 + 64 * 8 = 750 + 512 = 1262, plus mem.
	logGasFn2 := makeGasLog(2)
	mem2 := NewMemory()
	// Stack for LOG2: offset=0, size=64, topic1, topic2
	stack = testStack(big.NewInt(0), big.NewInt(0), big.NewInt(64), big.NewInt(0))
	gas, _ = logGasFn2(evm, contract, stack, mem2, 64)
	memGas, _ = gasMemExpansion(evm, contract, stack, mem2, 64)
	expected = uint64(2*GasLogTopic+64*GasLogData) + memGas
	if gas != expected {
		t.Errorf("gasLog2(64) = %d, want %d", gas, expected)
	}

	// LOG4 with 0 bytes: just topic gas.
	logGasFn4 := makeGasLog(4)
	stack = testStack(big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0))
	gas, _ = logGasFn4(evm, contract, stack, mem, 0)
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
	gas, _ := gasCreateDynamic(evm, contract, stack, mem, 64)
	memGas, _ := gasMemExpansion(evm, contract, stack, mem, 64)
	expected := uint64(2*InitCodeWordGas) + memGas
	if gas != expected {
		t.Errorf("gasCreateDynamic(64) = %d, want %d", gas, expected)
	}

	// CREATE with 0 bytes.
	stack = testStack(big.NewInt(0), big.NewInt(0), big.NewInt(0))
	gas, _ = gasCreateDynamic(evm, contract, stack, mem, 0)
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
	gas, _ := gasCreate2Dynamic(evm, contract, stack, mem, 64)
	memGas, _ := gasMemExpansion(evm, contract, stack, mem, 64)
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
	gas, _ := gasMemExpansion(evm, contract, stack, mem, 0)
	if gas != 0 {
		t.Errorf("gasMemExpansion(0) = %d, want 0", gas)
	}

	// Expansion from 0 to 32 bytes.
	gas, _ = gasMemExpansion(evm, contract, stack, mem, 32)
	// 1 word: 1*3 + 1*1/512 = 3
	if gas != 3 {
		t.Errorf("gasMemExpansion(0->32) = %d, want 3", gas)
	}

	// After memory grows, expansion should be cheaper.
	mem.Resize(64)
	gas, _ = gasMemExpansion(evm, contract, stack, mem, 32)
	if gas != 0 {
		t.Errorf("gasMemExpansion(already big enough) = %d, want 0", gas)
	}

	// Expand from 64 to 128.
	gas, _ = gasMemExpansion(evm, contract, stack, mem, 128)
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
	gas, _ := gasExtCodeCopyCopy(evm, contract, stack, mem, 64)
	memGas, _ := gasMemExpansion(evm, contract, stack, mem, 64)
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
	gasNoValue, _ := gasCallEIP2929(evm, contract, stack, mem, 0)

	// CALL with value=1 (value transfer).
	stack = testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(1), big.NewInt(0), big.NewInt(1000),
	)
	gasWithValue, _ := gasCallEIP2929(evm, contract, stack, mem, 0)

	// The difference should be at least CallValueTransferGas.
	diff := gasWithValue - gasNoValue
	if diff < CallValueTransferGas {
		t.Errorf("CALL value transfer gas difference = %d, want >= %d", diff, CallValueTransferGas)
	}
}

// --- EIP-2929 dynamic gas tests with access list tracking ---

// accessListStateDB is a StateDB implementation with working access list tracking
// for testing EIP-2929 dynamic gas costs. Unlike the simpler mockStateDB in
// instructions_test.go, this mock fully implements warm/cold address and slot tracking.
type accessListStateDB struct {
	warmAddresses map[types.Address]bool
	warmSlots     map[types.Address]map[types.Hash]bool
	balances      map[types.Address]*big.Int
	storage       map[types.Address]map[types.Hash]types.Hash
	exists        map[types.Address]bool
	codes         map[types.Address][]byte
	codeHashes    map[types.Address]types.Hash
	nonces        map[types.Address]uint64
	logs          []*types.Log
}

func newAccessListStateDB() *accessListStateDB {
	return &accessListStateDB{
		warmAddresses: make(map[types.Address]bool),
		warmSlots:     make(map[types.Address]map[types.Hash]bool),
		balances:      make(map[types.Address]*big.Int),
		storage:       make(map[types.Address]map[types.Hash]types.Hash),
		exists:        make(map[types.Address]bool),
		codes:         make(map[types.Address][]byte),
		codeHashes:    make(map[types.Address]types.Hash),
		nonces:        make(map[types.Address]uint64),
	}
}

func (m *accessListStateDB) AddAddressToAccessList(addr types.Address) {
	m.warmAddresses[addr] = true
}

func (m *accessListStateDB) AddSlotToAccessList(addr types.Address, slot types.Hash) {
	m.warmAddresses[addr] = true
	if m.warmSlots[addr] == nil {
		m.warmSlots[addr] = make(map[types.Hash]bool)
	}
	m.warmSlots[addr][slot] = true
}

func (m *accessListStateDB) AddressInAccessList(addr types.Address) bool {
	return m.warmAddresses[addr]
}

func (m *accessListStateDB) SlotInAccessList(addr types.Address, slot types.Hash) (bool, bool) {
	addrOk := m.warmAddresses[addr]
	if slots, ok := m.warmSlots[addr]; ok {
		return addrOk, slots[slot]
	}
	return addrOk, false
}

func (m *accessListStateDB) GetBalance(addr types.Address) *big.Int {
	if b, ok := m.balances[addr]; ok {
		return new(big.Int).Set(b)
	}
	return new(big.Int)
}

func (m *accessListStateDB) AddBalance(addr types.Address, amount *big.Int) {
	if _, ok := m.balances[addr]; !ok {
		m.balances[addr] = new(big.Int)
	}
	m.balances[addr].Add(m.balances[addr], amount)
}

func (m *accessListStateDB) SubBalance(addr types.Address, amount *big.Int) {
	if _, ok := m.balances[addr]; !ok {
		m.balances[addr] = new(big.Int)
	}
	m.balances[addr].Sub(m.balances[addr], amount)
}

func (m *accessListStateDB) GetState(addr types.Address, key types.Hash) types.Hash {
	if s, ok := m.storage[addr]; ok {
		return s[key]
	}
	return types.Hash{}
}

func (m *accessListStateDB) SetState(addr types.Address, key types.Hash, value types.Hash) {
	if m.storage[addr] == nil {
		m.storage[addr] = make(map[types.Hash]types.Hash)
	}
	m.storage[addr][key] = value
}

func (m *accessListStateDB) Exist(addr types.Address) bool {
	return m.exists[addr]
}

func (m *accessListStateDB) CreateAccount(addr types.Address)          { m.exists[addr] = true }
func (m *accessListStateDB) GetNonce(addr types.Address) uint64        { return m.nonces[addr] }
func (m *accessListStateDB) SetNonce(addr types.Address, n uint64)     { m.nonces[addr] = n }
func (m *accessListStateDB) GetCode(addr types.Address) []byte         { return m.codes[addr] }
func (m *accessListStateDB) SetCode(addr types.Address, code []byte)   { m.codes[addr] = code }
func (m *accessListStateDB) GetCodeHash(addr types.Address) types.Hash { return m.codeHashes[addr] }
func (m *accessListStateDB) GetCodeSize(addr types.Address) int        { return len(m.codes[addr]) }
func (m *accessListStateDB) GetCommittedState(addr types.Address, key types.Hash) types.Hash {
	return m.GetState(addr, key)
}
func (m *accessListStateDB) GetTransientState(types.Address, types.Hash) types.Hash {
	return types.Hash{}
}
func (m *accessListStateDB) SetTransientState(types.Address, types.Hash, types.Hash) {}
func (m *accessListStateDB) ClearTransientStorage()                                  {}
func (m *accessListStateDB) SelfDestruct(types.Address)                              {}
func (m *accessListStateDB) HasSelfDestructed(types.Address) bool                    { return false }
func (m *accessListStateDB) Empty(addr types.Address) bool                           { return !m.exists[addr] }
func (m *accessListStateDB) Snapshot() int                                           { return 0 }
func (m *accessListStateDB) RevertToSnapshot(int)                                    {}
func (m *accessListStateDB) AddLog(log *types.Log)                                   { m.logs = append(m.logs, log) }
func (m *accessListStateDB) AddRefund(uint64)                                        {}
func (m *accessListStateDB) SubRefund(uint64)                                        {}
func (m *accessListStateDB) GetRefund() uint64                                       { return 0 }

// newEIP2929TestEVM creates an EVM with an access-list-aware StateDB for EIP-2929 testing.
func newEIP2929TestEVM() (*EVM, *accessListStateDB) {
	db := newAccessListStateDB()
	evm := &EVM{StateDB: db}
	return evm, db
}

func TestGasSloadEIP2929_ColdThenWarm(t *testing.T) {
	evm, _ := newEIP2929TestEVM()
	contract := &Contract{
		Address: types.BytesToAddress([]byte{0x42}),
	}
	mem := NewMemory()

	slot := big.NewInt(1)

	// First access: cold. Dynamic gas = ColdSloadCost - WarmStorageReadCost = 2000.
	stack := testStack(new(big.Int).Set(slot))
	gas, _ := gasSloadEIP2929(evm, contract, stack, mem, 0)
	expectedCold := ColdSloadCost - WarmStorageReadCost
	if gas != expectedCold {
		t.Errorf("SLOAD cold dynamic gas = %d, want %d", gas, expectedCold)
	}

	// Second access: warm. Dynamic gas = 0.
	stack = testStack(new(big.Int).Set(slot))
	gas, _ = gasSloadEIP2929(evm, contract, stack, mem, 0)
	if gas != 0 {
		t.Errorf("SLOAD warm dynamic gas = %d, want 0", gas)
	}
}

func TestGasSloadEIP2929_DifferentSlots(t *testing.T) {
	evm, _ := newEIP2929TestEVM()
	contract := &Contract{
		Address: types.BytesToAddress([]byte{0x42}),
	}
	mem := NewMemory()

	// Access slot 1: cold.
	stack := testStack(big.NewInt(1))
	gas, _ := gasSloadEIP2929(evm, contract, stack, mem, 0)
	if gas != ColdSloadCost-WarmStorageReadCost {
		t.Errorf("SLOAD slot 1 cold = %d, want %d", gas, ColdSloadCost-WarmStorageReadCost)
	}

	// Access slot 2: also cold (different slot).
	stack = testStack(big.NewInt(2))
	gas, _ = gasSloadEIP2929(evm, contract, stack, mem, 0)
	if gas != ColdSloadCost-WarmStorageReadCost {
		t.Errorf("SLOAD slot 2 cold = %d, want %d", gas, ColdSloadCost-WarmStorageReadCost)
	}

	// Access slot 1 again: warm.
	stack = testStack(big.NewInt(1))
	gas, _ = gasSloadEIP2929(evm, contract, stack, mem, 0)
	if gas != 0 {
		t.Errorf("SLOAD slot 1 warm = %d, want 0", gas)
	}
}

func TestGasSloadEIP2929_PreWarmedSlot(t *testing.T) {
	evm, db := newEIP2929TestEVM()
	contract := &Contract{
		Address: types.BytesToAddress([]byte{0x42}),
	}
	mem := NewMemory()

	// Pre-warm the slot via access list (as the processor would do).
	slotHash := types.BytesToHash(big.NewInt(5).Bytes())
	db.AddSlotToAccessList(contract.Address, slotHash)

	// Access the pre-warmed slot: should be warm (0 extra gas).
	stack := testStack(big.NewInt(5))
	gas, _ := gasSloadEIP2929(evm, contract, stack, mem, 0)
	if gas != 0 {
		t.Errorf("SLOAD pre-warmed slot dynamic gas = %d, want 0", gas)
	}
}

func TestGasBalanceEIP2929_ColdThenWarm(t *testing.T) {
	evm, _ := newEIP2929TestEVM()
	contract := &Contract{}
	mem := NewMemory()

	addr := types.BytesToAddress([]byte{0xaa})
	addrInt := new(big.Int).SetBytes(addr[:])

	// First access: cold.
	stack := testStack(new(big.Int).Set(addrInt))
	gas, _ := gasBalanceEIP2929(evm, contract, stack, mem, 0)
	expectedCold := ColdAccountAccessCost - WarmStorageReadCost
	if gas != expectedCold {
		t.Errorf("BALANCE cold dynamic gas = %d, want %d", gas, expectedCold)
	}

	// Second access: warm.
	stack = testStack(new(big.Int).Set(addrInt))
	gas, _ = gasBalanceEIP2929(evm, contract, stack, mem, 0)
	if gas != 0 {
		t.Errorf("BALANCE warm dynamic gas = %d, want 0", gas)
	}
}

func TestGasBalanceEIP2929_PreWarmed(t *testing.T) {
	evm, db := newEIP2929TestEVM()
	contract := &Contract{}
	mem := NewMemory()

	addr := types.BytesToAddress([]byte{0xbb})
	db.AddAddressToAccessList(addr)

	addrInt := new(big.Int).SetBytes(addr[:])
	stack := testStack(new(big.Int).Set(addrInt))
	gas, _ := gasBalanceEIP2929(evm, contract, stack, mem, 0)
	if gas != 0 {
		t.Errorf("BALANCE pre-warmed dynamic gas = %d, want 0", gas)
	}
}

func TestGasExtCodeSizeEIP2929_ColdThenWarm(t *testing.T) {
	evm, _ := newEIP2929TestEVM()
	contract := &Contract{}
	mem := NewMemory()

	addr := types.BytesToAddress([]byte{0xcc})
	addrInt := new(big.Int).SetBytes(addr[:])

	// Cold access.
	stack := testStack(new(big.Int).Set(addrInt))
	gas, _ := gasExtCodeSizeEIP2929(evm, contract, stack, mem, 0)
	if gas != ColdAccountAccessCost-WarmStorageReadCost {
		t.Errorf("EXTCODESIZE cold = %d, want %d", gas, ColdAccountAccessCost-WarmStorageReadCost)
	}

	// Warm access.
	stack = testStack(new(big.Int).Set(addrInt))
	gas, _ = gasExtCodeSizeEIP2929(evm, contract, stack, mem, 0)
	if gas != 0 {
		t.Errorf("EXTCODESIZE warm = %d, want 0", gas)
	}
}

func TestGasExtCodeHashEIP2929_ColdThenWarm(t *testing.T) {
	evm, _ := newEIP2929TestEVM()
	contract := &Contract{}
	mem := NewMemory()

	addr := types.BytesToAddress([]byte{0xdd})
	addrInt := new(big.Int).SetBytes(addr[:])

	// Cold access.
	stack := testStack(new(big.Int).Set(addrInt))
	gas, _ := gasExtCodeHashEIP2929(evm, contract, stack, mem, 0)
	if gas != ColdAccountAccessCost-WarmStorageReadCost {
		t.Errorf("EXTCODEHASH cold = %d, want %d", gas, ColdAccountAccessCost-WarmStorageReadCost)
	}

	// Warm access.
	stack = testStack(new(big.Int).Set(addrInt))
	gas, _ = gasExtCodeHashEIP2929(evm, contract, stack, mem, 0)
	if gas != 0 {
		t.Errorf("EXTCODEHASH warm = %d, want 0", gas)
	}
}

func TestGasExtCodeCopyEIP2929_ColdThenWarm(t *testing.T) {
	evm, _ := newEIP2929TestEVM()
	contract := &Contract{}
	mem := NewMemory()

	addr := types.BytesToAddress([]byte{0xee})
	addrInt := new(big.Int).SetBytes(addr[:])

	// EXTCODECOPY stack: addr, memOffset, codeOffset, length
	// Cold access with 32 bytes copy: cold penalty + 1 word copy gas + mem expansion.
	stack := testStack(big.NewInt(32), big.NewInt(0), big.NewInt(0), new(big.Int).Set(addrInt))
	gas, _ := gasExtCodeCopyEIP2929(evm, contract, stack, mem, 32)
	coldPenalty := ColdAccountAccessCost - WarmStorageReadCost
	copyGas := GasCopy * toWordSize(32)
	memGas, _ := gasMemExpansion(evm, contract, stack, mem, 32)
	expected := coldPenalty + copyGas + memGas
	if gas != expected {
		t.Errorf("EXTCODECOPY cold = %d, want %d (cold=%d copy=%d mem=%d)",
			gas, expected, coldPenalty, copyGas, memGas)
	}

	// Warm access: no cold penalty.
	mem2 := NewMemory()
	stack = testStack(big.NewInt(32), big.NewInt(0), big.NewInt(0), new(big.Int).Set(addrInt))
	gas, _ = gasExtCodeCopyEIP2929(evm, contract, stack, mem2, 32)
	memGas2, _ := gasMemExpansion(evm, contract, stack, mem2, 32)
	expectedWarm := copyGas + memGas2
	if gas != expectedWarm {
		t.Errorf("EXTCODECOPY warm = %d, want %d", gas, expectedWarm)
	}
}

func TestGasCallEIP2929_ColdThenWarm(t *testing.T) {
	evm, db := newEIP2929TestEVM()
	contract := &Contract{}
	mem := NewMemory()

	addr := types.BytesToAddress([]byte{0xff})
	addrInt := new(big.Int).SetBytes(addr[:])
	db.exists[addr] = true // account exists so no CallNewAccountGas

	// CALL stack: gas, addr, value, argsOff, argsLen, retOff, retLen
	// Cold access, no value transfer.
	stack := testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(0), new(big.Int).Set(addrInt), big.NewInt(1000),
	)
	gasCold, _ := gasCallEIP2929(evm, contract, stack, mem, 0)
	expectedCold := ColdAccountAccessCost - WarmStorageReadCost
	if gasCold != expectedCold {
		t.Errorf("CALL cold (no value) = %d, want %d", gasCold, expectedCold)
	}

	// Warm access, no value transfer.
	stack = testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(0), new(big.Int).Set(addrInt), big.NewInt(1000),
	)
	gasWarm, _ := gasCallEIP2929(evm, contract, stack, mem, 0)
	if gasWarm != 0 {
		t.Errorf("CALL warm (no value) = %d, want 0", gasWarm)
	}
}

func TestGasCallEIP2929_ColdWithValue(t *testing.T) {
	evm, db := newEIP2929TestEVM()
	contract := &Contract{}
	mem := NewMemory()

	addr := types.BytesToAddress([]byte{0x11})
	addrInt := new(big.Int).SetBytes(addr[:])
	db.exists[addr] = true

	// CALL with value transfer to existing account.
	stack := testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(1), new(big.Int).Set(addrInt), big.NewInt(1000),
	)
	gas, _ := gasCallEIP2929(evm, contract, stack, mem, 0)
	expected := (ColdAccountAccessCost - WarmStorageReadCost) + CallValueTransferGas
	if gas != expected {
		t.Errorf("CALL cold+value = %d, want %d", gas, expected)
	}
}

func TestGasCallEIP2929_ColdValueNonExistent(t *testing.T) {
	evm, _ := newEIP2929TestEVM()
	contract := &Contract{}
	mem := NewMemory()

	addr := types.BytesToAddress([]byte{0x22})
	addrInt := new(big.Int).SetBytes(addr[:])
	// addr does NOT exist

	// CALL with value transfer to non-existent account.
	stack := testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(1), new(big.Int).Set(addrInt), big.NewInt(1000),
	)
	gas, _ := gasCallEIP2929(evm, contract, stack, mem, 0)
	expected := (ColdAccountAccessCost - WarmStorageReadCost) + CallValueTransferGas + CallNewAccountGas
	if gas != expected {
		t.Errorf("CALL cold+value+new = %d, want %d", gas, expected)
	}
}

func TestGasCallCodeEIP2929_ColdThenWarm(t *testing.T) {
	evm, _ := newEIP2929TestEVM()
	contract := &Contract{}
	mem := NewMemory()

	addr := types.BytesToAddress([]byte{0x33})
	addrInt := new(big.Int).SetBytes(addr[:])

	// CALLCODE stack: gas, addr, value, argsOff, argsLen, retOff, retLen
	// Cold access, no value.
	stack := testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(0), new(big.Int).Set(addrInt), big.NewInt(1000),
	)
	gasCold, _ := gasCallCodeEIP2929(evm, contract, stack, mem, 0)
	expectedCold := ColdAccountAccessCost - WarmStorageReadCost
	if gasCold != expectedCold {
		t.Errorf("CALLCODE cold = %d, want %d", gasCold, expectedCold)
	}

	// Warm access.
	stack = testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(0), new(big.Int).Set(addrInt), big.NewInt(1000),
	)
	gasWarm, _ := gasCallCodeEIP2929(evm, contract, stack, mem, 0)
	if gasWarm != 0 {
		t.Errorf("CALLCODE warm = %d, want 0", gasWarm)
	}
}

func TestGasCallCodeEIP2929_WithValue(t *testing.T) {
	evm, _ := newEIP2929TestEVM()
	contract := &Contract{}
	mem := NewMemory()

	addr := types.BytesToAddress([]byte{0x44})
	addrInt := new(big.Int).SetBytes(addr[:])

	// CALLCODE with value: cold penalty + CallValueTransferGas.
	// CALLCODE does NOT charge CallNewAccountGas.
	stack := testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(1), new(big.Int).Set(addrInt), big.NewInt(1000),
	)
	gas, _ := gasCallCodeEIP2929(evm, contract, stack, mem, 0)
	expected := (ColdAccountAccessCost - WarmStorageReadCost) + CallValueTransferGas
	if gas != expected {
		t.Errorf("CALLCODE cold+value = %d, want %d", gas, expected)
	}
}

func TestGasDelegateCallEIP2929_ColdThenWarm(t *testing.T) {
	evm, _ := newEIP2929TestEVM()
	contract := &Contract{}
	mem := NewMemory()

	addr := types.BytesToAddress([]byte{0x55})
	addrInt := new(big.Int).SetBytes(addr[:])

	// DELEGATECALL stack: gas, addr, argsOff, argsLen, retOff, retLen
	// Cold access.
	stack := testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		new(big.Int).Set(addrInt), big.NewInt(1000),
	)
	gasCold, _ := gasDelegateCallEIP2929(evm, contract, stack, mem, 0)
	expectedCold := ColdAccountAccessCost - WarmStorageReadCost
	if gasCold != expectedCold {
		t.Errorf("DELEGATECALL cold = %d, want %d", gasCold, expectedCold)
	}

	// Warm access.
	stack = testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		new(big.Int).Set(addrInt), big.NewInt(1000),
	)
	gasWarm, _ := gasDelegateCallEIP2929(evm, contract, stack, mem, 0)
	if gasWarm != 0 {
		t.Errorf("DELEGATECALL warm = %d, want 0", gasWarm)
	}
}

func TestGasStaticCallEIP2929_ColdThenWarm(t *testing.T) {
	evm, _ := newEIP2929TestEVM()
	contract := &Contract{}
	mem := NewMemory()

	addr := types.BytesToAddress([]byte{0x66})
	addrInt := new(big.Int).SetBytes(addr[:])

	// STATICCALL stack: gas, addr, argsOff, argsLen, retOff, retLen
	// Cold access.
	stack := testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		new(big.Int).Set(addrInt), big.NewInt(1000),
	)
	gasCold, _ := gasStaticCallEIP2929(evm, contract, stack, mem, 0)
	expectedCold := ColdAccountAccessCost - WarmStorageReadCost
	if gasCold != expectedCold {
		t.Errorf("STATICCALL cold = %d, want %d", gasCold, expectedCold)
	}

	// Warm access.
	stack = testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		new(big.Int).Set(addrInt), big.NewInt(1000),
	)
	gasWarm, _ := gasStaticCallEIP2929(evm, contract, stack, mem, 0)
	if gasWarm != 0 {
		t.Errorf("STATICCALL warm = %d, want 0", gasWarm)
	}
}

func TestGasSstoreEIP2929_ColdSlot(t *testing.T) {
	evm, db := newEIP2929TestEVM()
	contract := &Contract{
		Address: types.BytesToAddress([]byte{0x42}),
	}
	mem := NewMemory()

	// Mark the address as existing but the slot is cold.
	db.exists[contract.Address] = true

	// Stack: slot, value (slot on top for Back(0))
	// Writing 1 to slot 1 (currently zero).
	// Per EIP-2929: cold penalty = ColdSloadCost (2100) + SstoreSet (20000) = 22100.
	// SSTORE has constantGas=0 so all gas is dynamic.
	stack := testStack(big.NewInt(1), big.NewInt(1)) // value=1, slot=1
	gas, _ := gasSstoreEIP2929(evm, contract, stack, mem, 0)
	expectedGas := GasSstoreSet + ColdSloadCost // 20000 + 2100 = 22100
	if gas != expectedGas {
		t.Errorf("SSTORE cold 0->1 gas = %d, want %d", gas, expectedGas)
	}
}

func TestGasSstoreEIP2929_WarmSlot(t *testing.T) {
	evm, db := newEIP2929TestEVM()
	contract := &Contract{
		Address: types.BytesToAddress([]byte{0x42}),
	}
	mem := NewMemory()

	db.exists[contract.Address] = true

	// Pre-warm the slot.
	slotHash := types.BytesToHash(big.NewInt(1).Bytes())
	db.AddSlotToAccessList(contract.Address, slotHash)

	// Warm slot, 0->1: only SstoreSet (20000), no cold penalty.
	stack := testStack(big.NewInt(1), big.NewInt(1)) // value=1, slot=1
	gas, _ := gasSstoreEIP2929(evm, contract, stack, mem, 0)
	expectedGas := GasSstoreSet // 20000, no cold penalty
	if gas != expectedGas {
		t.Errorf("SSTORE warm 0->1 gas = %d, want %d", gas, expectedGas)
	}
}

func TestGasSstoreEIP2929_NoOp(t *testing.T) {
	evm, db := newEIP2929TestEVM()
	contract := &Contract{
		Address: types.BytesToAddress([]byte{0x42}),
	}
	mem := NewMemory()

	db.exists[contract.Address] = true

	// Pre-warm the slot and set current value to 1.
	slotHash := types.BytesToHash(big.NewInt(1).Bytes())
	db.AddSlotToAccessList(contract.Address, slotHash)
	db.SetState(contract.Address, slotHash, types.BytesToHash(big.NewInt(1).Bytes()))

	// Writing same value (1->1): no-op. Warm slot.
	// SstoreGas(one, one, one, false) = WarmStorageReadCost = 100.
	stack := testStack(big.NewInt(1), big.NewInt(1)) // value=1, slot=1
	gas, _ := gasSstoreEIP2929(evm, contract, stack, mem, 0)
	if gas != WarmStorageReadCost {
		t.Errorf("SSTORE warm no-op gas = %d, want %d", gas, WarmStorageReadCost)
	}
}

func TestGasSelfdestructEIP2929_ColdBeneficiary(t *testing.T) {
	evm, db := newEIP2929TestEVM()
	contract := &Contract{
		Address: types.BytesToAddress([]byte{0x42}),
	}
	mem := NewMemory()

	// Set up: contract has no balance, beneficiary is cold.
	db.exists[contract.Address] = true

	beneficiary := types.BytesToAddress([]byte{0x99})
	beneficiaryInt := new(big.Int).SetBytes(beneficiary[:])
	db.exists[beneficiary] = true

	stack := testStack(new(big.Int).Set(beneficiaryInt))
	gas, _ := gasSelfdestructEIP2929(evm, contract, stack, mem, 0)

	// Cold beneficiary: ColdAccountAccessCost - WarmStorageReadCost = 2500.
	expectedCold := ColdAccountAccessCost - WarmStorageReadCost
	if gas != expectedCold {
		t.Errorf("SELFDESTRUCT cold beneficiary = %d, want %d", gas, expectedCold)
	}

	// Second call: warm beneficiary.
	stack = testStack(new(big.Int).Set(beneficiaryInt))
	gas, _ = gasSelfdestructEIP2929(evm, contract, stack, mem, 0)
	if gas != 0 {
		t.Errorf("SELFDESTRUCT warm beneficiary = %d, want 0", gas)
	}
}

func TestGasSelfdestructEIP2929_ColdNewAccount(t *testing.T) {
	evm, db := newEIP2929TestEVM()
	contract := &Contract{
		Address: types.BytesToAddress([]byte{0x42}),
	}
	mem := NewMemory()

	// Contract has balance, beneficiary doesn't exist.
	db.exists[contract.Address] = true
	db.balances[contract.Address] = big.NewInt(1000)

	beneficiary := types.BytesToAddress([]byte{0xab})
	beneficiaryInt := new(big.Int).SetBytes(beneficiary[:])
	// beneficiary does NOT exist

	stack := testStack(new(big.Int).Set(beneficiaryInt))
	gas, _ := gasSelfdestructEIP2929(evm, contract, stack, mem, 0)

	// Cold + new account: cold penalty + CreateBySelfdestructGas.
	expectedGas := (ColdAccountAccessCost - WarmStorageReadCost) + CreateBySelfdestructGas
	if gas != expectedGas {
		t.Errorf("SELFDESTRUCT cold+new = %d, want %d", gas, expectedGas)
	}
}

func TestEIP2929AccountCheck_Helper(t *testing.T) {
	evm, db := newEIP2929TestEVM()
	addr := types.BytesToAddress([]byte{0x01})

	// Cold: returns extra gas and warms.
	gas := gasEIP2929AccountCheck(evm, addr)
	if gas != ColdAccountAccessCost-WarmStorageReadCost {
		t.Errorf("cold account check = %d, want %d", gas, ColdAccountAccessCost-WarmStorageReadCost)
	}
	if !db.AddressInAccessList(addr) {
		t.Error("address should be warmed after cold check")
	}

	// Warm: returns 0.
	gas = gasEIP2929AccountCheck(evm, addr)
	if gas != 0 {
		t.Errorf("warm account check = %d, want 0", gas)
	}
}

func TestEIP2929SlotCheck_Helper(t *testing.T) {
	evm, db := newEIP2929TestEVM()
	addr := types.BytesToAddress([]byte{0x02})
	slot := types.BytesToHash([]byte{0x01})

	// Cold: returns extra gas and warms.
	gas := gasEIP2929SlotCheck(evm, addr, slot)
	if gas != ColdSloadCost-WarmStorageReadCost {
		t.Errorf("cold slot check = %d, want %d", gas, ColdSloadCost-WarmStorageReadCost)
	}
	_, slotWarm := db.SlotInAccessList(addr, slot)
	if !slotWarm {
		t.Error("slot should be warmed after cold check")
	}

	// Warm: returns 0.
	gas = gasEIP2929SlotCheck(evm, addr, slot)
	if gas != 0 {
		t.Errorf("warm slot check = %d, want 0", gas)
	}
}

func TestEIP2929_NilStateDB(t *testing.T) {
	evm := &EVM{} // no StateDB
	addr := types.BytesToAddress([]byte{0x01})
	slot := types.BytesToHash([]byte{0x01})

	// With nil StateDB, helpers return 0 (no cold penalty).
	if gas := gasEIP2929AccountCheck(evm, addr); gas != 0 {
		t.Errorf("nil StateDB account check = %d, want 0", gas)
	}
	if gas := gasEIP2929SlotCheck(evm, addr, slot); gas != 0 {
		t.Errorf("nil StateDB slot check = %d, want 0", gas)
	}
}

func TestBerlinJumpTable_WiringCheck(t *testing.T) {
	tbl := NewBerlinJumpTable()

	// Verify all EIP-2929 opcodes have WarmStorageReadCost as constantGas
	// and their dynamic gas functions are set.
	checks := []struct {
		op   OpCode
		name string
	}{
		{SLOAD, "SLOAD"},
		{BALANCE, "BALANCE"},
		{EXTCODESIZE, "EXTCODESIZE"},
		{EXTCODECOPY, "EXTCODECOPY"},
		{EXTCODEHASH, "EXTCODEHASH"},
		{CALL, "CALL"},
		{CALLCODE, "CALLCODE"},
		{STATICCALL, "STATICCALL"},
		{DELEGATECALL, "DELEGATECALL"},
	}

	for _, c := range checks {
		op := tbl[c.op]
		if op == nil {
			t.Errorf("%s: operation is nil in Berlin table", c.name)
			continue
		}
		if op.constantGas != WarmStorageReadCost {
			t.Errorf("%s: constantGas = %d, want %d (WarmStorageReadCost)",
				c.name, op.constantGas, WarmStorageReadCost)
		}
		if op.dynamicGas == nil {
			t.Errorf("%s: dynamicGas is nil in Berlin table", c.name)
		}
	}

	// SSTORE has constantGas 0 (all dynamic).
	ssOp := tbl[SSTORE]
	if ssOp == nil {
		t.Fatal("SSTORE: operation is nil in Berlin table")
	}
	if ssOp.constantGas != 0 {
		t.Errorf("SSTORE: constantGas = %d, want 0", ssOp.constantGas)
	}
	if ssOp.dynamicGas == nil {
		t.Error("SSTORE: dynamicGas is nil in Berlin table")
	}

	// SELFDESTRUCT retains its base constant gas.
	sdOp := tbl[SELFDESTRUCT]
	if sdOp == nil {
		t.Fatal("SELFDESTRUCT: operation is nil in Berlin table")
	}
	if sdOp.constantGas != GasSelfdestruct {
		t.Errorf("SELFDESTRUCT: constantGas = %d, want %d", sdOp.constantGas, GasSelfdestruct)
	}
	if sdOp.dynamicGas == nil {
		t.Error("SELFDESTRUCT: dynamicGas is nil in Berlin table")
	}
}

func TestEIP2929_TotalGasCosts(t *testing.T) {
	// Verify total gas (constant + dynamic) matches EIP-2929 spec values.
	evm, _ := newEIP2929TestEVM()
	contract := &Contract{
		Address: types.BytesToAddress([]byte{0x42}),
	}
	mem := NewMemory()
	tbl := NewBerlinJumpTable()

	// SLOAD cold: total = WarmStorageReadCost + (ColdSloadCost - WarmStorageReadCost) = ColdSloadCost
	stack := testStack(big.NewInt(1))
	dynamicGas, _ := tbl[SLOAD].dynamicGas(evm, contract, stack, mem, 0)
	totalGas := tbl[SLOAD].constantGas + dynamicGas
	if totalGas != ColdSloadCost {
		t.Errorf("SLOAD cold total = %d, want %d (ColdSloadCost)", totalGas, ColdSloadCost)
	}

	// SLOAD warm (same slot again): total = WarmStorageReadCost
	stack = testStack(big.NewInt(1))
	dynamicGas, _ = tbl[SLOAD].dynamicGas(evm, contract, stack, mem, 0)
	totalGas = tbl[SLOAD].constantGas + dynamicGas
	if totalGas != WarmStorageReadCost {
		t.Errorf("SLOAD warm total = %d, want %d (WarmStorageReadCost)", totalGas, WarmStorageReadCost)
	}

	// BALANCE cold: total = ColdAccountAccessCost
	addr := types.BytesToAddress([]byte{0xaa})
	addrInt := new(big.Int).SetBytes(addr[:])
	stack = testStack(new(big.Int).Set(addrInt))
	dynamicGas, _ = tbl[BALANCE].dynamicGas(evm, contract, stack, mem, 0)
	totalGas = tbl[BALANCE].constantGas + dynamicGas
	if totalGas != ColdAccountAccessCost {
		t.Errorf("BALANCE cold total = %d, want %d (ColdAccountAccessCost)", totalGas, ColdAccountAccessCost)
	}

	// BALANCE warm: total = WarmStorageReadCost
	stack = testStack(new(big.Int).Set(addrInt))
	dynamicGas, _ = tbl[BALANCE].dynamicGas(evm, contract, stack, mem, 0)
	totalGas = tbl[BALANCE].constantGas + dynamicGas
	if totalGas != WarmStorageReadCost {
		t.Errorf("BALANCE warm total = %d, want %d (WarmStorageReadCost)", totalGas, WarmStorageReadCost)
	}
}

func TestPreWarmAccessList(t *testing.T) {
	evm, db := newEIP2929TestEVM()
	sender := types.BytesToAddress([]byte{0x01})
	to := types.BytesToAddress([]byte{0x02})

	evm.PreWarmAccessList(sender, &to)

	// Sender should be warm.
	if !db.AddressInAccessList(sender) {
		t.Error("sender should be in access list after pre-warm")
	}
	// Recipient should be warm.
	if !db.AddressInAccessList(to) {
		t.Error("recipient should be in access list after pre-warm")
	}
	// Precompile addresses (0x01-0x0a) should be warm.
	for i := 1; i <= 10; i++ {
		precompile := types.BytesToAddress([]byte{byte(i)})
		if !db.AddressInAccessList(precompile) {
			t.Errorf("precompile 0x%02x should be in access list after pre-warm", i)
		}
	}
	// Random address should NOT be warm.
	random := types.BytesToAddress([]byte{0xff})
	if db.AddressInAccessList(random) {
		t.Error("random address should NOT be in access list")
	}
}

func TestPreWarmAccessList_NilTo(t *testing.T) {
	evm, db := newEIP2929TestEVM()
	sender := types.BytesToAddress([]byte{0x01})

	// Contract creation: no 'to' address.
	evm.PreWarmAccessList(sender, nil)

	if !db.AddressInAccessList(sender) {
		t.Error("sender should be in access list after pre-warm")
	}
}

// --- Yellow Paper Gas Tier Verification ---

// TestYellowPaperGasTiers verifies that all opcodes are assigned the correct
// gas tier per the Ethereum Yellow Paper Appendix G.
func TestYellowPaperGasTiers(t *testing.T) {
	tbl := NewCancunJumpTable()

	// Gbase = 2: environment and block info opcodes.
	gbaseOps := []struct {
		op   OpCode
		name string
	}{
		{ADDRESS, "ADDRESS"},
		{ORIGIN, "ORIGIN"},
		{CALLER, "CALLER"},
		{CALLVALUE, "CALLVALUE"},
		{CALLDATASIZE, "CALLDATASIZE"},
		{CODESIZE, "CODESIZE"},
		{GASPRICE, "GASPRICE"},
		{COINBASE, "COINBASE"},
		{TIMESTAMP, "TIMESTAMP"},
		{NUMBER, "NUMBER"},
		{PREVRANDAO, "PREVRANDAO"},
		{GASLIMIT, "GASLIMIT"},
		{POP, "POP"},
		{PC, "PC"},
		{MSIZE, "MSIZE"},
		{GAS, "GAS"},
		{CHAINID, "CHAINID"},
		{BASEFEE, "BASEFEE"},
		{RETURNDATASIZE, "RETURNDATASIZE"},
		{BLOBBASEFEE, "BLOBBASEFEE"},
		{PUSH0, "PUSH0"},
	}
	for _, c := range gbaseOps {
		op := tbl[c.op]
		if op == nil {
			t.Errorf("%s: nil in Cancun table", c.name)
			continue
		}
		if op.constantGas != GasBase {
			t.Errorf("%s: constantGas = %d, want %d (Gbase)", c.name, op.constantGas, GasBase)
		}
	}

	// Gverylow = 3: arithmetic, comparison, bitwise, data access opcodes.
	gverylowOps := []struct {
		op   OpCode
		name string
	}{
		{ADD, "ADD"},
		{SUB, "SUB"},
		{MUL, "MUL"},
		{LT, "LT"},
		{GT, "GT"},
		{SLT, "SLT"},
		{SGT, "SGT"},
		{EQ, "EQ"},
		{ISZERO, "ISZERO"},
		{AND, "AND"},
		{OR, "OR"},
		{XOR, "XOR"},
		{NOT, "NOT"},
		{BYTE, "BYTE"},
		{SHL, "SHL"},
		{SHR, "SHR"},
		{SAR, "SAR"},
		{CALLDATALOAD, "CALLDATALOAD"},
		{PUSH1, "PUSH1"},
	}
	for _, c := range gverylowOps {
		op := tbl[c.op]
		if op == nil {
			t.Errorf("%s: nil in Cancun table", c.name)
			continue
		}
		if op.constantGas != GasVerylow {
			t.Errorf("%s: constantGas = %d, want %d (Gverylow)", c.name, op.constantGas, GasVerylow)
		}
	}

	// Glow = 5: division, modulo, sign extension.
	glowOps := []struct {
		op   OpCode
		name string
	}{
		{DIV, "DIV"},
		{SDIV, "SDIV"},
		{MOD, "MOD"},
		{SMOD, "SMOD"},
		{SIGNEXTEND, "SIGNEXTEND"},
		{SELFBALANCE, "SELFBALANCE"},
	}
	for _, c := range glowOps {
		op := tbl[c.op]
		if op == nil {
			t.Errorf("%s: nil in Cancun table", c.name)
			continue
		}
		if op.constantGas != GasLow {
			t.Errorf("%s: constantGas = %d, want %d (Glow)", c.name, op.constantGas, GasLow)
		}
	}

	// Gmid = 8: ADDMOD, MULMOD.
	gmidOps := []struct {
		op   OpCode
		name string
	}{
		{ADDMOD, "ADDMOD"},
		{MULMOD, "MULMOD"},
	}
	for _, c := range gmidOps {
		op := tbl[c.op]
		if op == nil {
			t.Errorf("%s: nil in Cancun table", c.name)
			continue
		}
		if op.constantGas != GasMid {
			t.Errorf("%s: constantGas = %d, want %d (Gmid)", c.name, op.constantGas, GasMid)
		}
	}

	// Verify JUMP = 8 and JUMPI = 10.
	if tbl[JUMP].constantGas != 8 {
		t.Errorf("JUMP: constantGas = %d, want 8", tbl[JUMP].constantGas)
	}
	if tbl[JUMPI].constantGas != 10 {
		t.Errorf("JUMPI: constantGas = %d, want 10", tbl[JUMPI].constantGas)
	}

	// Verify EXP base = 10 (Ghigh).
	if tbl[EXP].constantGas != GasHigh {
		t.Errorf("EXP: constantGas = %d, want %d (Ghigh)", tbl[EXP].constantGas, GasHigh)
	}

	// Verify BLOCKHASH = 20 (Gext).
	if tbl[BLOCKHASH].constantGas != GasExt {
		t.Errorf("BLOCKHASH: constantGas = %d, want %d (Gext)", tbl[BLOCKHASH].constantGas, GasExt)
	}
}

// TestYellowPaperCopyBaseGas verifies that CALLDATACOPY, CODECOPY, and
// RETURNDATACOPY use Gverylow (3) as their base cost, not Gbase (2).
func TestYellowPaperCopyBaseGas(t *testing.T) {
	tbl := NewCancunJumpTable()

	copyOps := []struct {
		op   OpCode
		name string
	}{
		{CALLDATACOPY, "CALLDATACOPY"},
		{CODECOPY, "CODECOPY"},
		{RETURNDATACOPY, "RETURNDATACOPY"},
	}
	for _, c := range copyOps {
		op := tbl[c.op]
		if op == nil {
			t.Errorf("%s: nil in Cancun table", c.name)
			continue
		}
		if op.constantGas != GasVerylow {
			t.Errorf("%s: constantGas = %d, want %d (Gverylow)", c.name, op.constantGas, GasVerylow)
		}
		if op.dynamicGas == nil {
			t.Errorf("%s: dynamicGas is nil (should have gasCopy)", c.name)
		}
	}
}

// TestYellowPaperMemoryExpansionFormula verifies the quadratic memory cost
// formula: memory_cost = memory_size_word^2 / 512 + 3 * memory_size_word
func TestYellowPaperMemoryExpansionFormula(t *testing.T) {
	tests := []struct {
		name string
		size uint64
		want uint64
	}{
		{"0 bytes", 0, 0},
		{"1 word (32 bytes)", 32, 3},              // 1^2/512 + 3*1 = 0 + 3
		{"2 words (64 bytes)", 64, 6},             // 4/512 + 6 = 0 + 6
		{"10 words (320 bytes)", 320, 30},         // 100/512 + 30 = 0 + 30
		{"22 words (704 bytes)", 704, 66},         // 484/512 + 66 = 0 + 66
		{"23 words (736 bytes)", 736, 70},         // 529/512 + 69 = 1 + 69
		{"32 words (1024 bytes)", 1024, 98},       // 1024/512 + 96 = 2 + 96
		{"100 words (3200 bytes)", 3200, 319},     // 10000/512 + 300 = 19 + 300
		{"1024 words (32768 bytes)", 32768, 5120}, // 1048576/512 + 3072 = 2048 + 3072
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MemoryGasCost(tt.size)
			if got != tt.want {
				t.Errorf("MemoryGasCost(%d) = %d, want %d", tt.size, got, tt.want)
			}
		})
	}
}

// TestLogGas_AllTopicCounts verifies LOG0-LOG4 gas: 375 + 375*topics + 8*size.
func TestLogGas_AllTopicCounts(t *testing.T) {
	for topics := uint64(0); topics <= 4; topics++ {
		for _, size := range []uint64{0, 1, 32, 100, 256} {
			expected := GasLog + topics*GasLogTopic + size*GasLogData
			got := LogGas(topics, size)
			if got != expected {
				t.Errorf("LogGas(%d topics, %d bytes) = %d, want %d", topics, size, got, expected)
			}
		}
	}
}

// TestExpGas_ByteLengths verifies EXP gas: 10 + 50 * byte_size_of_exponent.
func TestExpGas_ByteLengths(t *testing.T) {
	tests := []struct {
		name    string
		exp     *big.Int
		wantGas uint64
	}{
		{"zero", big.NewInt(0), GasHigh},                      // 10 + 0
		{"1 (1 byte)", big.NewInt(1), GasHigh + 50},           // 10 + 50
		{"255 (1 byte)", big.NewInt(255), GasHigh + 50},       // 10 + 50
		{"256 (2 bytes)", big.NewInt(256), GasHigh + 100},     // 10 + 100
		{"65535 (2 bytes)", big.NewInt(65535), GasHigh + 100}, // 10 + 100
		{"65536 (3 bytes)", big.NewInt(65536), GasHigh + 150}, // 10 + 150
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpGas(tt.exp)
			if got != tt.wantGas {
				t.Errorf("ExpGas(%s) = %d, want %d", tt.exp.String(), got, tt.wantGas)
			}
		})
	}
}

// TestSha3Gas_WordSizes verifies SHA3/KECCAK256 gas: 30 + 6 * ceil(size/32).
func TestSha3Gas_WordSizes(t *testing.T) {
	tests := []struct {
		size    uint64
		wantGas uint64
	}{
		{0, 30},   // 30 + 6*0
		{1, 36},   // 30 + 6*1
		{31, 36},  // 30 + 6*1
		{32, 36},  // 30 + 6*1
		{33, 42},  // 30 + 6*2
		{64, 42},  // 30 + 6*2
		{65, 48},  // 30 + 6*3
		{256, 78}, // 30 + 6*8
	}

	for _, tt := range tests {
		got := Sha3Gas(tt.size)
		if got != tt.wantGas {
			t.Errorf("Sha3Gas(%d) = %d, want %d", tt.size, got, tt.wantGas)
		}
	}
}

// TestGasConstants_YellowPaper verifies all gas constants match their expected values.
func TestGasConstants_YellowPaper(t *testing.T) {
	// Gas tiers
	if GasBase != 2 {
		t.Errorf("GasBase = %d, want 2", GasBase)
	}
	if GasVerylow != 3 {
		t.Errorf("GasVerylow = %d, want 3", GasVerylow)
	}
	if GasLow != 5 {
		t.Errorf("GasLow = %d, want 5", GasLow)
	}
	if GasMid != 8 {
		t.Errorf("GasMid = %d, want 8", GasMid)
	}
	if GasHigh != 10 {
		t.Errorf("GasHigh = %d, want 10", GasHigh)
	}
	if GasExt != 20 {
		t.Errorf("GasExt = %d, want 20", GasExt)
	}

	// Verify legacy aliases match the named tiers.
	if GasQuickStep != GasBase {
		t.Errorf("GasQuickStep = %d, want GasBase (%d)", GasQuickStep, GasBase)
	}
	if GasFastestStep != GasVerylow {
		t.Errorf("GasFastestStep = %d, want GasVerylow (%d)", GasFastestStep, GasVerylow)
	}
	if GasFastStep != GasLow {
		t.Errorf("GasFastStep = %d, want GasLow (%d)", GasFastStep, GasLow)
	}
	if GasMidStep != GasMid {
		t.Errorf("GasMidStep = %d, want GasMid (%d)", GasMidStep, GasMid)
	}
	if GasSlowStep != GasHigh {
		t.Errorf("GasSlowStep = %d, want GasHigh (%d)", GasSlowStep, GasHigh)
	}
	if GasExtStep != GasExt {
		t.Errorf("GasExtStep = %d, want GasExt (%d)", GasExtStep, GasExt)
	}

	// EIP-2929 cold/warm access costs
	if ColdAccountAccessCost != 2600 {
		t.Errorf("ColdAccountAccessCost = %d, want 2600", ColdAccountAccessCost)
	}
	if ColdSloadCost != 2100 {
		t.Errorf("ColdSloadCost = %d, want 2100", ColdSloadCost)
	}
	if WarmStorageReadCost != 100 {
		t.Errorf("WarmStorageReadCost = %d, want 100", WarmStorageReadCost)
	}

	// SSTORE costs
	if GasSstoreSet != 20000 {
		t.Errorf("GasSstoreSet = %d, want 20000", GasSstoreSet)
	}
	if GasSstoreReset != 2900 {
		t.Errorf("GasSstoreReset = %d, want 2900", GasSstoreReset)
	}

	// KECCAK256 costs
	if GasKeccak256 != 30 {
		t.Errorf("GasKeccak256 = %d, want 30", GasKeccak256)
	}
	if GasKeccak256Word != 6 {
		t.Errorf("GasKeccak256Word = %d, want 6", GasKeccak256Word)
	}

	// LOG costs
	if GasLog != 375 {
		t.Errorf("GasLog = %d, want 375", GasLog)
	}
	if GasLogTopic != 375 {
		t.Errorf("GasLogTopic = %d, want 375", GasLogTopic)
	}
	if GasLogData != 8 {
		t.Errorf("GasLogData = %d, want 8", GasLogData)
	}

	// Memory/copy costs
	if GasMemory != 3 {
		t.Errorf("GasMemory = %d, want 3", GasMemory)
	}
	if GasCopy != 3 {
		t.Errorf("GasCopy = %d, want 3", GasCopy)
	}
}

// --- Safe arithmetic tests ---

func TestSafeAdd(t *testing.T) {
	tests := []struct {
		a, b uint64
		want uint64
	}{
		{0, 0, 0},
		{1, 2, 3},
		{100, 200, 300},
		{math.MaxUint64, 0, math.MaxUint64},
		{0, math.MaxUint64, math.MaxUint64},
		{math.MaxUint64, 1, math.MaxUint64},              // overflow
		{math.MaxUint64, math.MaxUint64, math.MaxUint64}, // overflow
		{math.MaxUint64 - 1, 1, math.MaxUint64},          // exactly max
		{math.MaxUint64 - 1, 2, math.MaxUint64},          // overflow by 1
	}
	for _, tt := range tests {
		got := safeAdd(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("safeAdd(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestSafeMul(t *testing.T) {
	tests := []struct {
		a, b uint64
		want uint64
	}{
		{0, 0, 0},
		{0, 100, 0},
		{100, 0, 0},
		{1, math.MaxUint64, math.MaxUint64},
		{math.MaxUint64, 1, math.MaxUint64},
		{2, math.MaxUint64, math.MaxUint64}, // overflow
		{math.MaxUint64, 2, math.MaxUint64}, // overflow
		{3, 5, 15},
		{1 << 32, 1 << 31, 1 << 63}, // large but no overflow
	}
	for _, tt := range tests {
		got := safeMul(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("safeMul(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
	// Special case: 1<<32 * 1<<32 overflows uint64 (result would be 2^64).
	// safeMul should return MaxUint64.
	got := safeMul(1<<32, 1<<32)
	if got != math.MaxUint64 {
		t.Errorf("safeMul(1<<32, 1<<32) = %d, want MaxUint64", got)
	}
}

func TestMemoryGasCost_Overflow(t *testing.T) {
	// Words > 181,000 should return MaxUint64 to signal overflow.
	// 181,000 words is ~5.8 MB of memory; above that the quadratic term overflows.
	got := MemoryGasCost(181001 * 32) // 181001 words
	if got != math.MaxUint64 {
		t.Errorf("MemoryGasCost(181001 words) = %d, want MaxUint64", got)
	}

	// 181,000 words should NOT overflow.
	got2 := MemoryGasCost(181000 * 32)
	if got2 == math.MaxUint64 {
		t.Error("MemoryGasCost(181000 words) should not overflow")
	}
}

// --- SSTORE EIP-2929 total gas verification ---

func TestGasSstoreEIP2929_TotalGasVerification(t *testing.T) {
	zero := [32]byte{}
	nonZero := [32]byte{0: 1}
	nonZero2 := [32]byte{0: 2}

	tests := []struct {
		name     string
		original [32]byte
		current  [32]byte
		newVal   [32]byte
		cold     bool
		wantGas  uint64
	}{
		{
			name:     "cold create slot (0->1)",
			original: zero, current: zero, newVal: nonZero,
			cold: true, wantGas: GasSstoreSet + ColdSloadCost, // 20000 + 2100 = 22100
		},
		{
			name:     "warm create slot (0->1)",
			original: zero, current: zero, newVal: nonZero,
			cold: false, wantGas: GasSstoreSet, // 20000
		},
		{
			name:     "cold update slot (1->2)",
			original: nonZero, current: nonZero, newVal: nonZero2,
			cold: true, wantGas: GasSstoreReset + ColdSloadCost, // 2900 + 2100 = 5000
		},
		{
			name:     "warm noop (1->1)",
			original: nonZero, current: nonZero, newVal: nonZero,
			cold: false, wantGas: WarmStorageReadCost, // 100
		},
		{
			name:     "cold noop (1->1)",
			original: nonZero, current: nonZero, newVal: nonZero,
			cold: true, wantGas: WarmStorageReadCost + ColdSloadCost, // 100 + 2100 = 2200
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gas, _ := SstoreGas(tt.original, tt.current, tt.newVal, tt.cold)
			if gas != tt.wantGas {
				t.Errorf("SstoreGas() gas = %d, want %d", gas, tt.wantGas)
			}
		})
	}
}

// --- SSTORE dirty slot refund tests ---

func TestSstoreGas_DirtySlotRefunds(t *testing.T) {
	zero := [32]byte{}
	val1 := [32]byte{0: 1}
	val2 := [32]byte{0: 2}

	tests := []struct {
		name       string
		original   [32]byte
		current    [32]byte
		newVal     [32]byte
		wantGas    uint64
		wantRefund int64
	}{
		{
			name:     "dirty: undo clear (orig=1, cur=0, new=2)",
			original: val1, current: zero, newVal: val2,
			wantGas:    WarmStorageReadCost,                // 100
			wantRefund: -int64(SstoreClearsScheduleRefund), // -4800
		},
		{
			name:     "dirty: clear non-zero (orig=1, cur=2, new=0)",
			original: val1, current: val2, newVal: zero,
			wantGas:    WarmStorageReadCost,               // 100
			wantRefund: int64(SstoreClearsScheduleRefund), // +4800
		},
		{
			name:     "dirty: restore original nonzero (orig=1, cur=2, new=1)",
			original: val1, current: val2, newVal: val1,
			wantGas:    WarmStorageReadCost,                                // 100
			wantRefund: int64(GasSstoreReset) - int64(WarmStorageReadCost), // 2900 - 100 = 2800
		},
		{
			name:     "dirty: restore original zero (orig=0, cur=1, new=0)",
			original: zero, current: val1, newVal: zero,
			wantGas:    WarmStorageReadCost,                              // 100
			wantRefund: int64(GasSstoreSet) - int64(WarmStorageReadCost), // 20000 - 100 = 19900
		},
		{
			name:     "dirty: change dirty value (orig=1, cur=2, new=3)",
			original: val1, current: val2, newVal: [32]byte{0: 3},
			wantGas:    WarmStorageReadCost, // 100
			wantRefund: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gas, refund := SstoreGas(tt.original, tt.current, tt.newVal, false)
			if gas != tt.wantGas {
				t.Errorf("gas = %d, want %d", gas, tt.wantGas)
			}
			if refund != tt.wantRefund {
				t.Errorf("refund = %d, want %d", refund, tt.wantRefund)
			}
		})
	}
}

// --- SSTORE cold flag in SstoreGas ---

func TestSstoreGas_ColdFlag(t *testing.T) {
	zero := [32]byte{}
	val1 := [32]byte{0: 1}

	// With cold=true, the gas should include ColdSloadCost on top.
	gasWarm, _ := SstoreGas(zero, zero, val1, false) // 20000
	gasCold, _ := SstoreGas(zero, zero, val1, true)  // 20000 + 2100 = 22100

	if gasCold != gasWarm+ColdSloadCost {
		t.Errorf("cold SstoreGas = %d, want warm(%d) + ColdSloadCost(%d) = %d",
			gasCold, gasWarm, ColdSloadCost, gasWarm+ColdSloadCost)
	}
}

// --- SSTORE second access is warm ---

func TestGasSstoreEIP2929_SecondAccessIsWarm(t *testing.T) {
	stateDB := newAccessListStateDB()
	evm := &EVM{StateDB: stateDB}
	addr := types.BytesToAddress([]byte{0x42})
	contract := &Contract{Address: addr, Gas: 100000}
	stack := NewStack()

	slotNum := big.NewInt(7)
	stack.Push(new(big.Int))              // value (doesn't matter for gas)
	stack.Push(new(big.Int).Set(slotNum)) // slot

	// First access: cold
	gas1, _ := gasSstoreEIP2929(evm, contract, stack, nil, 0)

	// Re-push for second access
	stack.Push(new(big.Int))              // value
	stack.Push(new(big.Int).Set(slotNum)) // same slot

	// Second access: warm (slot was warmed by first call)
	gas2, _ := gasSstoreEIP2929(evm, contract, stack, nil, 0)

	// Cold should be ColdSloadCost more than warm.
	if gas1 != gas2+ColdSloadCost {
		t.Errorf("first access gas = %d, second access gas = %d, difference = %d, want ColdSloadCost = %d",
			gas1, gas2, gas1-gas2, ColdSloadCost)
	}
}

// --- SELFDESTRUCT edge cases ---

func TestGasSelfdestructEIP2929_WarmNoBalance(t *testing.T) {
	stateDB := newAccessListStateDB()
	evm := &EVM{StateDB: stateDB}
	addr := types.BytesToAddress([]byte{0x01})
	beneficiary := types.BytesToAddress([]byte{0x02})
	contract := &Contract{Address: addr, Gas: 100000}

	// Pre-warm beneficiary address.
	stateDB.AddAddressToAccessList(beneficiary)

	stack := NewStack()
	stack.Push(new(big.Int).SetBytes(beneficiary[:]))

	gas, _ := gasSelfdestructEIP2929(evm, contract, stack, nil, 0)
	// Warm address, no balance: dynamic gas should be 0.
	if gas != 0 {
		t.Errorf("selfdestruct warm no balance: gas = %d, want 0", gas)
	}
}

func TestGasSelfdestructEIP2929_ColdExistingNoBalance(t *testing.T) {
	stateDB := newAccessListStateDB()
	evm := &EVM{StateDB: stateDB}
	addr := types.BytesToAddress([]byte{0x01})
	beneficiary := types.BytesToAddress([]byte{0x02})
	contract := &Contract{Address: addr, Gas: 100000}

	// Mark beneficiary as existing but don't add to access list.
	stateDB.exists[beneficiary] = true

	stack := NewStack()
	stack.Push(new(big.Int).SetBytes(beneficiary[:]))

	gas, _ := gasSelfdestructEIP2929(evm, contract, stack, nil, 0)
	// Cold address, existing: ColdAccountAccessCost - WarmStorageReadCost = 2500
	wantGas := ColdAccountAccessCost - WarmStorageReadCost
	if gas != wantGas {
		t.Errorf("selfdestruct cold existing: gas = %d, want %d", gas, wantGas)
	}
}

func TestGasSelfdestructEIP2929_ColdNonExistentWithBalance(t *testing.T) {
	stateDB := newAccessListStateDB()
	evm := &EVM{StateDB: stateDB}
	addr := types.BytesToAddress([]byte{0x01})
	beneficiary := types.BytesToAddress([]byte{0x02})
	contract := &Contract{Address: addr, Gas: 100000}

	// Give the contract balance so it triggers new-account gas.
	stateDB.balances[addr] = big.NewInt(1000)
	// beneficiary does not exist (not in stateDB.exists)

	stack := NewStack()
	stack.Push(new(big.Int).SetBytes(beneficiary[:]))

	gas, _ := gasSelfdestructEIP2929(evm, contract, stack, nil, 0)
	// Cold + non-existent beneficiary with contract having balance:
	// (ColdAccountAccessCost - WarmStorageReadCost) + CreateBySelfdestructGas
	// = 2500 + 25000 = 27500
	wantGas := (ColdAccountAccessCost - WarmStorageReadCost) + CreateBySelfdestructGas
	if gas != wantGas {
		t.Errorf("selfdestruct cold nonexistent with balance: gas = %d, want %d", gas, wantGas)
	}
}

// --- Overflow tests for helper functions ---

func TestSha3Gas_LargeSize(t *testing.T) {
	// Extremely large size should not panic due to overflow.
	// The result should be astronomically large (will trigger out-of-gas in practice).
	gas := Sha3Gas(math.MaxUint64)
	if gas < 1<<60 {
		t.Errorf("Sha3Gas(MaxUint64) = %d, expected very large value (>= 2^60)", gas)
	}
	// Verify no wraparound to a small value (the original bug where toWordSize returned 0).
	if gas <= GasKeccak256 {
		t.Errorf("Sha3Gas(MaxUint64) = %d, must be greater than base cost %d (overflow bug)", gas, GasKeccak256)
	}
}

func TestLogGas_LargeDataSize(t *testing.T) {
	// MaxUint64 data size: safeMul(MaxUint64, 8) should saturate to MaxUint64.
	gas := LogGas(4, math.MaxUint64)
	if gas != math.MaxUint64 {
		t.Errorf("LogGas(4, MaxUint64) = %d, want MaxUint64", gas)
	}
}

func TestCopyGas_LargeSize(t *testing.T) {
	// Extremely large size should not panic due to overflow.
	gas := CopyGas(math.MaxUint64)
	if gas < 1<<60 {
		t.Errorf("CopyGas(MaxUint64) = %d, expected very large value (>= 2^60)", gas)
	}
	// Verify no wraparound to 0 (the original bug where toWordSize overflowed).
	if gas == 0 {
		t.Error("CopyGas(MaxUint64) = 0, overflow bug in toWordSize")
	}
}

func TestExpGas_MaxByteLengthExponent(t *testing.T) {
	// A 256-bit exponent (32 bytes): gas = 10 + 50*32 = 1610
	exp := new(big.Int).Lsh(big.NewInt(1), 255) // 2^255, which is 32 bytes
	gas := ExpGas(exp)
	want := uint64(10 + 50*32)
	if gas != want {
		t.Errorf("ExpGas(2^255) = %d, want %d", gas, want)
	}

	// Zero exponent: gas = GasSlowStep = 10
	gas0 := ExpGas(big.NewInt(0))
	if gas0 != GasSlowStep {
		t.Errorf("ExpGas(0) = %d, want %d", gas0, GasSlowStep)
	}

	// 1-byte exponent (value=1): gas = 10 + 50*1 = 60
	gas1 := ExpGas(big.NewInt(1))
	if gas1 != 60 {
		t.Errorf("ExpGas(1) = %d, want 60", gas1)
	}

	// 1-byte exponent (value=255): gas = 10 + 50*1 = 60
	gas255 := ExpGas(big.NewInt(255))
	if gas255 != 60 {
		t.Errorf("ExpGas(255) = %d, want 60", gas255)
	}

	// 2-byte exponent (value=256): gas = 10 + 50*2 = 110
	gas256 := ExpGas(big.NewInt(256))
	if gas256 != 110 {
		t.Errorf("ExpGas(256) = %d, want 110", gas256)
	}
}

// --- Pre-Berlin (Frontier) CALL/CALLCODE/SELFDESTRUCT dynamic gas tests ---

func TestGasCallFrontier_NoValue(t *testing.T) {
	evm, db := newEIP2929TestEVM()
	contract := &Contract{}
	mem := NewMemory()

	addr := types.BytesToAddress([]byte{0xaa})
	addrInt := new(big.Int).SetBytes(addr[:])
	db.exists[addr] = true

	// CALL stack: gas, addr, value, argsOff, argsLen, retOff, retLen
	// No value transfer: only memory expansion gas.
	stack := testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(0), new(big.Int).Set(addrInt), big.NewInt(1000),
	)
	gas, _ := gasCallFrontier(evm, contract, stack, mem, 0)
	if gas != 0 {
		t.Errorf("gasCallFrontier no value, no mem = %d, want 0", gas)
	}
}

func TestGasCallFrontier_WithValue_ExistingAccount(t *testing.T) {
	evm, db := newEIP2929TestEVM()
	contract := &Contract{}
	mem := NewMemory()

	addr := types.BytesToAddress([]byte{0xbb})
	addrInt := new(big.Int).SetBytes(addr[:])
	db.exists[addr] = true

	// CALL with value=1 to existing account: CallValueTransferGas only.
	stack := testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(1), new(big.Int).Set(addrInt), big.NewInt(1000),
	)
	gas, _ := gasCallFrontier(evm, contract, stack, mem, 0)
	if gas != CallValueTransferGas {
		t.Errorf("gasCallFrontier value+existing = %d, want %d", gas, CallValueTransferGas)
	}
}

func TestGasCallFrontier_WithValue_NonExistentAccount(t *testing.T) {
	evm, _ := newEIP2929TestEVM()
	contract := &Contract{}
	mem := NewMemory()

	addr := types.BytesToAddress([]byte{0xcc})
	addrInt := new(big.Int).SetBytes(addr[:])
	// addr does NOT exist

	// CALL with value=1 to non-existent account: CallValueTransferGas + CallNewAccountGas.
	stack := testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(1), new(big.Int).Set(addrInt), big.NewInt(1000),
	)
	gas, _ := gasCallFrontier(evm, contract, stack, mem, 0)
	expected := CallValueTransferGas + CallNewAccountGas
	if gas != expected {
		t.Errorf("gasCallFrontier value+new = %d, want %d", gas, expected)
	}
}

func TestGasCallFrontier_WithMemoryExpansion(t *testing.T) {
	evm, db := newEIP2929TestEVM()
	contract := &Contract{}
	mem := NewMemory()

	addr := types.BytesToAddress([]byte{0xdd})
	addrInt := new(big.Int).SetBytes(addr[:])
	db.exists[addr] = true

	// CALL with value=1, memory expansion to 64 bytes.
	stack := testStack(
		big.NewInt(0), big.NewInt(64), big.NewInt(0), big.NewInt(0),
		big.NewInt(1), new(big.Int).Set(addrInt), big.NewInt(1000),
	)
	gas, _ := gasCallFrontier(evm, contract, stack, mem, 64)
	memGas, _ := gasMemExpansion(evm, contract, stack, mem, 64)
	expected := CallValueTransferGas + memGas
	if gas != expected {
		t.Errorf("gasCallFrontier value+mem = %d, want %d (value=%d mem=%d)",
			gas, expected, CallValueTransferGas, memGas)
	}
}

func TestGasCallFrontier_NoValue_NonExistentAccount(t *testing.T) {
	evm, _ := newEIP2929TestEVM()
	contract := &Contract{}
	mem := NewMemory()

	addr := types.BytesToAddress([]byte{0xee})
	addrInt := new(big.Int).SetBytes(addr[:])
	// addr does NOT exist, but no value transfer

	// CALL with value=0 to non-existent account: no extra gas (new account gas
	// is only charged when value > 0).
	stack := testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(0), new(big.Int).Set(addrInt), big.NewInt(1000),
	)
	gas, _ := gasCallFrontier(evm, contract, stack, mem, 0)
	if gas != 0 {
		t.Errorf("gasCallFrontier no value+new acct = %d, want 0", gas)
	}
}

func TestGasCallCodeFrontier_NoValue(t *testing.T) {
	evm := &EVM{}
	contract := &Contract{}
	mem := NewMemory()

	// CALLCODE with value=0: no extra gas.
	stack := testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(0), big.NewInt(0), big.NewInt(1000),
	)
	gas, _ := gasCallCodeFrontier(evm, contract, stack, mem, 0)
	if gas != 0 {
		t.Errorf("gasCallCodeFrontier no value = %d, want 0", gas)
	}
}

func TestGasCallCodeFrontier_WithValue(t *testing.T) {
	evm := &EVM{}
	contract := &Contract{}
	mem := NewMemory()

	// CALLCODE with value=1: CallValueTransferGas only (no new account gas).
	stack := testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(1), big.NewInt(0), big.NewInt(1000),
	)
	gas, _ := gasCallCodeFrontier(evm, contract, stack, mem, 0)
	if gas != CallValueTransferGas {
		t.Errorf("gasCallCodeFrontier value = %d, want %d", gas, CallValueTransferGas)
	}
}

func TestGasCallCodeFrontier_NeverChargesNewAccountGas(t *testing.T) {
	evm, _ := newEIP2929TestEVM()
	contract := &Contract{}
	mem := NewMemory()

	// CALLCODE to non-existent address with value: should NOT charge CallNewAccountGas.
	addr := types.BytesToAddress([]byte{0xff})
	addrInt := new(big.Int).SetBytes(addr[:])
	// addr does NOT exist

	stack := testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(1), new(big.Int).Set(addrInt), big.NewInt(1000),
	)
	gas, _ := gasCallCodeFrontier(evm, contract, stack, mem, 0)
	if gas != CallValueTransferGas {
		t.Errorf("gasCallCodeFrontier value+nonexistent = %d, want %d (should NOT charge new account gas)",
			gas, CallValueTransferGas)
	}
}

func TestGasSelfdestructFrontier_NoBalance(t *testing.T) {
	evm, db := newEIP2929TestEVM()
	contract := &Contract{
		Address: types.BytesToAddress([]byte{0x42}),
	}
	mem := NewMemory()

	db.exists[contract.Address] = true
	// Contract has zero balance.

	beneficiary := types.BytesToAddress([]byte{0x99})
	beneficiaryInt := new(big.Int).SetBytes(beneficiary[:])
	// beneficiary does NOT exist, but contract has no balance

	stack := testStack(new(big.Int).Set(beneficiaryInt))
	gas, _ := gasSelfdestructFrontier(evm, contract, stack, mem, 0)
	if gas != 0 {
		t.Errorf("gasSelfdestructFrontier no balance = %d, want 0", gas)
	}
}

func TestGasSelfdestructFrontier_BalanceToExisting(t *testing.T) {
	evm, db := newEIP2929TestEVM()
	contract := &Contract{
		Address: types.BytesToAddress([]byte{0x42}),
	}
	mem := NewMemory()

	db.exists[contract.Address] = true
	db.balances[contract.Address] = big.NewInt(1000)

	beneficiary := types.BytesToAddress([]byte{0x99})
	beneficiaryInt := new(big.Int).SetBytes(beneficiary[:])
	db.exists[beneficiary] = true // beneficiary exists

	stack := testStack(new(big.Int).Set(beneficiaryInt))
	gas, _ := gasSelfdestructFrontier(evm, contract, stack, mem, 0)
	if gas != 0 {
		t.Errorf("gasSelfdestructFrontier balance+existing = %d, want 0", gas)
	}
}

func TestGasSelfdestructFrontier_BalanceToNewAccount(t *testing.T) {
	evm, db := newEIP2929TestEVM()
	contract := &Contract{
		Address: types.BytesToAddress([]byte{0x42}),
	}
	mem := NewMemory()

	db.exists[contract.Address] = true
	db.balances[contract.Address] = big.NewInt(1000)

	beneficiary := types.BytesToAddress([]byte{0xab})
	beneficiaryInt := new(big.Int).SetBytes(beneficiary[:])
	// beneficiary does NOT exist

	stack := testStack(new(big.Int).Set(beneficiaryInt))
	gas, _ := gasSelfdestructFrontier(evm, contract, stack, mem, 0)
	if gas != CreateBySelfdestructGas {
		t.Errorf("gasSelfdestructFrontier balance+new = %d, want %d", gas, CreateBySelfdestructGas)
	}
}

func TestGasSelfdestructFrontier_NilStateDB(t *testing.T) {
	evm := &EVM{} // no StateDB
	contract := &Contract{
		Address: types.BytesToAddress([]byte{0x42}),
	}
	mem := NewMemory()

	stack := testStack(big.NewInt(0x99))
	gas, _ := gasSelfdestructFrontier(evm, contract, stack, mem, 0)
	if gas != 0 {
		t.Errorf("gasSelfdestructFrontier nil StateDB = %d, want 0", gas)
	}
}

func TestFrontierJumpTable_CallWiring(t *testing.T) {
	tbl := NewFrontierJumpTable()

	// CALL should have gasCallFrontier, not plain gasMemExpansion.
	callOp := tbl[CALL]
	if callOp == nil {
		t.Fatal("CALL: operation is nil in Frontier table")
	}
	if callOp.dynamicGas == nil {
		t.Fatal("CALL: dynamicGas is nil in Frontier table")
	}
	if callOp.constantGas != GasCallFrontier {
		t.Errorf("CALL: constantGas = %d, want %d", callOp.constantGas, GasCallFrontier)
	}

	// CALLCODE should have gasCallCodeFrontier.
	callcodeOp := tbl[CALLCODE]
	if callcodeOp == nil {
		t.Fatal("CALLCODE: operation is nil in Frontier table")
	}
	if callcodeOp.dynamicGas == nil {
		t.Fatal("CALLCODE: dynamicGas is nil in Frontier table")
	}

	// SELFDESTRUCT should have gasSelfdestructFrontier.
	sdOp := tbl[SELFDESTRUCT]
	if sdOp == nil {
		t.Fatal("SELFDESTRUCT: operation is nil in Frontier table")
	}
	if sdOp.dynamicGas == nil {
		t.Fatal("SELFDESTRUCT: dynamicGas is nil in Frontier table")
	}
	if sdOp.constantGas != GasSelfdestruct {
		t.Errorf("SELFDESTRUCT: constantGas = %d, want %d", sdOp.constantGas, GasSelfdestruct)
	}
}

func TestFrontierCall_ChargesValueTransfer(t *testing.T) {
	// Verify through the jump table that Frontier CALL correctly charges value transfer gas.
	tbl := NewFrontierJumpTable()
	evm, db := newEIP2929TestEVM()
	contract := &Contract{}
	mem := NewMemory()

	addr := types.BytesToAddress([]byte{0xaa})
	addrInt := new(big.Int).SetBytes(addr[:])
	db.exists[addr] = true

	// CALL with value=0.
	stack := testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(0), new(big.Int).Set(addrInt), big.NewInt(1000),
	)
	gasNoValue, _ := tbl[CALL].dynamicGas(evm, contract, stack, mem, 0)

	// CALL with value=1.
	stack = testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(1), new(big.Int).Set(addrInt), big.NewInt(1000),
	)
	gasWithValue, _ := tbl[CALL].dynamicGas(evm, contract, stack, mem, 0)

	diff := gasWithValue - gasNoValue
	if diff != CallValueTransferGas {
		t.Errorf("Frontier CALL value transfer gas diff = %d, want %d", diff, CallValueTransferGas)
	}
}

func TestFrontierCall_ChargesNewAccountGas(t *testing.T) {
	// Verify that Frontier CALL charges new account gas for value transfer to non-existent account.
	tbl := NewFrontierJumpTable()
	evm, db := newEIP2929TestEVM()
	contract := &Contract{}
	mem := NewMemory()

	existingAddr := types.BytesToAddress([]byte{0xaa})
	existingAddrInt := new(big.Int).SetBytes(existingAddr[:])
	db.exists[existingAddr] = true

	newAddr := types.BytesToAddress([]byte{0xbb})
	newAddrInt := new(big.Int).SetBytes(newAddr[:])
	// newAddr does NOT exist

	// CALL with value=1 to existing account.
	stack := testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(1), new(big.Int).Set(existingAddrInt), big.NewInt(1000),
	)
	gasExisting, _ := tbl[CALL].dynamicGas(evm, contract, stack, mem, 0)

	// CALL with value=1 to non-existent account.
	stack = testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(1), new(big.Int).Set(newAddrInt), big.NewInt(1000),
	)
	gasNew, _ := tbl[CALL].dynamicGas(evm, contract, stack, mem, 0)

	diff := gasNew - gasExisting
	if diff != CallNewAccountGas {
		t.Errorf("Frontier CALL new account gas diff = %d, want %d", diff, CallNewAccountGas)
	}
}
