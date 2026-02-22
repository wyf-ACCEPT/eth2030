package vm

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// mockStateDB is a minimal StateDB mock for testing Cancun opcodes.
type mockStateDB struct {
	transient map[types.Address]map[types.Hash]types.Hash
	storage   map[types.Address]map[types.Hash]types.Hash
}

func newMockStateDB() *mockStateDB {
	return &mockStateDB{
		transient: make(map[types.Address]map[types.Hash]types.Hash),
		storage:   make(map[types.Address]map[types.Hash]types.Hash),
	}
}

func (m *mockStateDB) CreateAccount(types.Address)                             {}
func (m *mockStateDB) GetBalance(types.Address) *big.Int                       { return new(big.Int) }
func (m *mockStateDB) AddBalance(types.Address, *big.Int)                      {}
func (m *mockStateDB) SubBalance(types.Address, *big.Int)                      {}
func (m *mockStateDB) GetNonce(types.Address) uint64                           { return 0 }
func (m *mockStateDB) SetNonce(types.Address, uint64)                          {}
func (m *mockStateDB) GetCode(types.Address) []byte                            { return nil }
func (m *mockStateDB) SetCode(types.Address, []byte)                           {}
func (m *mockStateDB) GetCodeHash(types.Address) types.Hash                    { return types.Hash{} }
func (m *mockStateDB) GetCodeSize(types.Address) int                           { return 0 }
func (m *mockStateDB) SelfDestruct(types.Address)                              {}
func (m *mockStateDB) HasSelfDestructed(types.Address) bool                    { return false }
func (m *mockStateDB) Exist(types.Address) bool                                { return false }
func (m *mockStateDB) Empty(types.Address) bool                                { return true }
func (m *mockStateDB) Snapshot() int                                           { return 0 }
func (m *mockStateDB) RevertToSnapshot(int)                                    {}
func (m *mockStateDB) AddLog(*types.Log)                                       {}
func (m *mockStateDB) AddRefund(uint64)                                        {}
func (m *mockStateDB) SubRefund(uint64)                                        {}
func (m *mockStateDB) GetRefund() uint64                                       { return 0 }
func (m *mockStateDB) AddAddressToAccessList(types.Address)                    {}
func (m *mockStateDB) AddSlotToAccessList(types.Address, types.Hash)           {}
func (m *mockStateDB) AddressInAccessList(types.Address) bool                  { return false }
func (m *mockStateDB) SlotInAccessList(types.Address, types.Hash) (bool, bool) { return false, false }
func (m *mockStateDB) GetCommittedState(addr types.Address, key types.Hash) types.Hash {
	return m.GetState(addr, key)
}

func (m *mockStateDB) GetState(addr types.Address, key types.Hash) types.Hash {
	if s, ok := m.storage[addr]; ok {
		return s[key]
	}
	return types.Hash{}
}

func (m *mockStateDB) SetState(addr types.Address, key types.Hash, value types.Hash) {
	if _, ok := m.storage[addr]; !ok {
		m.storage[addr] = make(map[types.Hash]types.Hash)
	}
	m.storage[addr][key] = value
}

func (m *mockStateDB) GetTransientState(addr types.Address, key types.Hash) types.Hash {
	if s, ok := m.transient[addr]; ok {
		return s[key]
	}
	return types.Hash{}
}

func (m *mockStateDB) SetTransientState(addr types.Address, key types.Hash, value types.Hash) {
	if _, ok := m.transient[addr]; !ok {
		m.transient[addr] = make(map[types.Hash]types.Hash)
	}
	m.transient[addr][key] = value
}

func (m *mockStateDB) ClearTransientStorage() {
	m.transient = make(map[types.Address]map[types.Hash]types.Hash)
}

// setupTestWithState creates an EVM with a mock StateDB.
func setupTestWithState() (*EVM, *Contract, *Memory, *Stack) {
	mock := newMockStateDB()
	addr := types.Address{0x01}
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = mock
	contract := NewContract(types.Address{}, addr, big.NewInt(0), 1000000)
	mem := NewMemory()
	st := NewStack()
	return evm, contract, mem, st
}

func setupTest() (*EVM, *Contract, *Memory, *Stack) {
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	contract := NewContract([20]byte{}, [20]byte{}, big.NewInt(0), 1000000)
	mem := NewMemory()
	st := NewStack()
	return evm, contract, mem, st
}

func TestOpAdd(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(10))
	st.Push(big.NewInt(20))
	opAdd(&pc, evm, contract, mem, st)

	if st.Len() != 1 {
		t.Fatalf("stack len = %d, want 1", st.Len())
	}
	if st.Peek().Int64() != 30 {
		t.Errorf("10 + 20 = %d, want 30", st.Peek().Int64())
	}
}

func TestOpAddOverflow(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	// max uint256 + 1 should wrap to 0
	max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	st.Push(big.NewInt(1))
	st.Push(max)
	opAdd(&pc, evm, contract, mem, st)

	if st.Peek().Sign() != 0 {
		t.Errorf("max + 1 = %s, want 0", st.Peek().String())
	}
}

func TestOpSub(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(7))
	st.Push(big.NewInt(20))
	opSub(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 13 {
		t.Errorf("20 - 7 = %d, want 13", st.Peek().Int64())
	}
}

func TestOpMul(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(6))
	st.Push(big.NewInt(7))
	opMul(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 42 {
		t.Errorf("7 * 6 = %d, want 42", st.Peek().Int64())
	}
}

func TestOpDiv(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(3))
	st.Push(big.NewInt(10))
	opDiv(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 3 {
		t.Errorf("10 / 3 = %d, want 3", st.Peek().Int64())
	}
}

func TestOpDivByZero(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(0))
	st.Push(big.NewInt(10))
	opDiv(&pc, evm, contract, mem, st)

	if st.Peek().Sign() != 0 {
		t.Errorf("10 / 0 = %d, want 0", st.Peek().Int64())
	}
}

func TestOpMod(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(3))
	st.Push(big.NewInt(10))
	opMod(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 1 {
		t.Errorf("10 %% 3 = %d, want 1", st.Peek().Int64())
	}
}

func TestOpLt(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(20))
	st.Push(big.NewInt(10))
	opLt(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 1 {
		t.Errorf("10 < 20 = %d, want 1", st.Peek().Int64())
	}
}

func TestOpGt(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(5))
	st.Push(big.NewInt(10))
	opGt(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 1 {
		t.Errorf("10 > 5 = %d, want 1", st.Peek().Int64())
	}
}

func TestOpEq(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(42))
	st.Push(big.NewInt(42))
	opEq(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 1 {
		t.Errorf("42 == 42 = %d, want 1", st.Peek().Int64())
	}
}

func TestOpIsZero(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(0))
	opIsZero(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 1 {
		t.Errorf("ISZERO(0) = %d, want 1", st.Peek().Int64())
	}
}

func TestOpAnd(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(0x0f))
	st.Push(big.NewInt(0xff))
	opAnd(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 0x0f {
		t.Errorf("0xff & 0x0f = 0x%x, want 0x0f", st.Peek().Int64())
	}
}

func TestOpOr(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(0x0f))
	st.Push(big.NewInt(0xf0))
	opOr(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 0xff {
		t.Errorf("0xf0 | 0x0f = 0x%x, want 0xff", st.Peek().Int64())
	}
}

func TestOpXor(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(0x0f))
	st.Push(big.NewInt(0xff))
	opXor(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 0xf0 {
		t.Errorf("0xff ^ 0x0f = 0x%x, want 0xf0", st.Peek().Int64())
	}
}

func TestOpNot(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(0))
	opNot(&pc, evm, contract, mem, st)

	// NOT(0) should be max uint256
	max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	if st.Peek().Cmp(max) != 0 {
		t.Errorf("NOT(0) = %s, want max uint256", st.Peek().String())
	}
}

func TestOpPush1(t *testing.T) {
	evm, contract, mem, st := setupTest()
	contract.Code = []byte{byte(PUSH1), 0x42}
	pc := uint64(0)

	opPush1(&pc, evm, contract, mem, st)

	if st.Len() != 1 {
		t.Fatalf("stack len = %d, want 1", st.Len())
	}
	if st.Peek().Int64() != 0x42 {
		t.Errorf("PUSH1 0x42 = 0x%x, want 0x42", st.Peek().Int64())
	}
	if pc != 1 {
		t.Errorf("pc = %d, want 1", pc)
	}
}

func TestOpMstoreAndMload(t *testing.T) {
	evm, contract, mem, st := setupTest()
	mem.Resize(64)
	pc := uint64(0)

	// MSTORE: store 0xff at offset 0
	st.Push(big.NewInt(0xff))
	st.Push(big.NewInt(0))
	opMstore(&pc, evm, contract, mem, st)

	// MLOAD: load from offset 0
	st.Push(big.NewInt(0))
	opMload(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 0xff {
		t.Errorf("MLOAD after MSTORE = 0x%x, want 0xff", st.Peek().Int64())
	}
}

func TestOpMstore8(t *testing.T) {
	evm, contract, mem, st := setupTest()
	mem.Resize(32)
	pc := uint64(0)

	st.Push(big.NewInt(0xab))
	st.Push(big.NewInt(0))
	opMstore8(&pc, evm, contract, mem, st)

	if mem.Data()[0] != 0xab {
		t.Errorf("MSTORE8 byte = 0x%x, want 0xab", mem.Data()[0])
	}
}

func TestOpStop(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	ret, err := opStop(&pc, evm, contract, mem, st)
	if err != nil {
		t.Errorf("opStop error: %v", err)
	}
	if ret != nil {
		t.Errorf("opStop return = %v, want nil", ret)
	}
}

func TestOpReturn(t *testing.T) {
	evm, contract, mem, st := setupTest()
	mem.Resize(64)
	pc := uint64(0)

	// Store data first
	mem.Set(0, 4, []byte{0xde, 0xad, 0xbe, 0xef})

	st.Push(big.NewInt(4)) // size
	st.Push(big.NewInt(0)) // offset
	ret, err := opReturn(&pc, evm, contract, mem, st)
	if err != nil {
		t.Errorf("opReturn error: %v", err)
	}
	if len(ret) != 4 || ret[0] != 0xde {
		t.Errorf("opReturn = %x, want deadbeef", ret)
	}
}

func TestOpRevert(t *testing.T) {
	evm, contract, mem, st := setupTest()
	mem.Resize(64)
	pc := uint64(0)

	mem.Set(0, 2, []byte{0xab, 0xcd})

	st.Push(big.NewInt(2))
	st.Push(big.NewInt(0))
	ret, err := opRevert(&pc, evm, contract, mem, st)
	if err != ErrExecutionReverted {
		t.Errorf("opRevert error = %v, want ErrExecutionReverted", err)
	}
	if len(ret) != 2 || ret[0] != 0xab {
		t.Errorf("opRevert = %x, want abcd", ret)
	}
}

func TestOpSdiv(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	// 10 / 3 = 3 (positive case)
	st.Push(big.NewInt(3))
	st.Push(big.NewInt(10))
	opSdiv(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 3 {
		t.Errorf("SDIV(10, 3) = %d, want 3", st.Peek().Int64())
	}
}

func TestOpSmod(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(3))
	st.Push(big.NewInt(10))
	opSmod(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 1 {
		t.Errorf("SMOD(10, 3) = %d, want 1", st.Peek().Int64())
	}
}

// --- TLOAD / TSTORE tests (EIP-1153) ---

func TestOpTload(t *testing.T) {
	evm, contract, mem, st := setupTestWithState()
	pc := uint64(0)

	// TLOAD of an unset key should return zero.
	st.Push(big.NewInt(42))
	opTload(&pc, evm, contract, mem, st)

	if st.Peek().Sign() != 0 {
		t.Errorf("TLOAD(unset key) = %s, want 0", st.Peek().String())
	}
}

func TestOpTstoreAndTload(t *testing.T) {
	evm, contract, mem, st := setupTestWithState()
	pc := uint64(0)

	// TSTORE: store value 0xAA at key 0x01.
	st.Push(big.NewInt(0xAA)) // value
	st.Push(big.NewInt(0x01)) // key
	_, err := opTstore(&pc, evm, contract, mem, st)
	if err != nil {
		t.Fatalf("TSTORE error: %v", err)
	}

	// TLOAD: load from key 0x01.
	st.Push(big.NewInt(0x01))
	opTload(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 0xAA {
		t.Errorf("TLOAD after TSTORE = 0x%x, want 0xAA", st.Peek().Int64())
	}
}

func TestOpTstoreWriteProtection(t *testing.T) {
	evm, contract, mem, st := setupTestWithState()
	evm.readOnly = true
	pc := uint64(0)

	st.Push(big.NewInt(0xAA))
	st.Push(big.NewInt(0x01))
	_, err := opTstore(&pc, evm, contract, mem, st)
	if err != ErrWriteProtection {
		t.Errorf("TSTORE in readOnly got err=%v, want ErrWriteProtection", err)
	}
}

func TestOpTloadNoStateDB(t *testing.T) {
	evm, contract, mem, st := setupTest() // no StateDB
	pc := uint64(0)

	st.Push(big.NewInt(1))
	opTload(&pc, evm, contract, mem, st)

	if st.Peek().Sign() != 0 {
		t.Errorf("TLOAD without StateDB = %s, want 0", st.Peek().String())
	}
}

func TestOpTstoreOverwrite(t *testing.T) {
	evm, contract, mem, st := setupTestWithState()
	pc := uint64(0)

	// TSTORE value 0x11 at key 0x01.
	st.Push(big.NewInt(0x11))
	st.Push(big.NewInt(0x01))
	opTstore(&pc, evm, contract, mem, st)

	// TSTORE value 0x22 at same key 0x01 (overwrite).
	st.Push(big.NewInt(0x22))
	st.Push(big.NewInt(0x01))
	opTstore(&pc, evm, contract, mem, st)

	// TLOAD should return the latest value.
	st.Push(big.NewInt(0x01))
	opTload(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 0x22 {
		t.Errorf("TLOAD after overwrite = 0x%x, want 0x22", st.Peek().Int64())
	}
}

func TestOpTstoreAddressIsolation(t *testing.T) {
	mock := newMockStateDB()
	addr1 := types.Address{0x01}
	addr2 := types.Address{0x02}
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = mock
	mem := NewMemory()
	pc := uint64(0)

	// TSTORE as contract at addr1.
	contract1 := NewContract(types.Address{}, addr1, big.NewInt(0), 1000000)
	st := NewStack()
	st.Push(big.NewInt(0xFF))
	st.Push(big.NewInt(0x01))
	opTstore(&pc, evm, contract1, mem, st)

	// TLOAD from addr2 at same key should return zero.
	contract2 := NewContract(types.Address{}, addr2, big.NewInt(0), 1000000)
	st2 := NewStack()
	st2.Push(big.NewInt(0x01))
	opTload(&pc, evm, contract2, mem, st2)

	if st2.Peek().Sign() != 0 {
		t.Errorf("TLOAD from different address = %s, want 0", st2.Peek().String())
	}

	// TLOAD from addr1 at same key should return 0xFF.
	st3 := NewStack()
	st3.Push(big.NewInt(0x01))
	opTload(&pc, evm, contract1, mem, st3)

	if st3.Peek().Int64() != 0xFF {
		t.Errorf("TLOAD from original address = 0x%x, want 0xFF", st3.Peek().Int64())
	}
}

func TestOpTstoreDoesNotAffectPersistentStorage(t *testing.T) {
	mock := newMockStateDB()
	addr := types.Address{0x01}
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = mock
	contract := NewContract(types.Address{}, addr, big.NewInt(0), 1000000)
	mem := NewMemory()
	st := NewStack()
	pc := uint64(0)

	// TSTORE value at key 0x01.
	st.Push(big.NewInt(0xBB))
	st.Push(big.NewInt(0x01))
	opTstore(&pc, evm, contract, mem, st)

	// SLOAD at same key should return zero (persistent storage is separate).
	st.Push(big.NewInt(0x01))
	opSload(&pc, evm, contract, mem, st)

	if st.Peek().Sign() != 0 {
		t.Errorf("SLOAD after TSTORE = %s, want 0 (transient != persistent)", st.Peek().String())
	}
}

func TestOpTstoreClearTransientStorage(t *testing.T) {
	mock := newMockStateDB()
	addr := types.Address{0x01}
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = mock
	contract := NewContract(types.Address{}, addr, big.NewInt(0), 1000000)
	mem := NewMemory()
	st := NewStack()
	pc := uint64(0)

	// TSTORE a value.
	st.Push(big.NewInt(0xCC))
	st.Push(big.NewInt(0x01))
	opTstore(&pc, evm, contract, mem, st)

	// Clear transient storage (simulates end of transaction).
	mock.ClearTransientStorage()

	// TLOAD should now return zero.
	st.Push(big.NewInt(0x01))
	opTload(&pc, evm, contract, mem, st)

	if st.Peek().Sign() != 0 {
		t.Errorf("TLOAD after clear = %s, want 0", st.Peek().String())
	}
}

func TestOpTstoreNoStateDB(t *testing.T) {
	evm, contract, mem, st := setupTest() // no StateDB
	pc := uint64(0)

	// TSTORE without StateDB should be a no-op (no panic).
	st.Push(big.NewInt(0xAA))
	st.Push(big.NewInt(0x01))
	_, err := opTstore(&pc, evm, contract, mem, st)
	if err != nil {
		t.Errorf("TSTORE without StateDB should not error, got: %v", err)
	}
}

func TestTloadTstoreGasCost(t *testing.T) {
	tbl := NewCancunJumpTable()

	// TLOAD gas should be WarmStorageReadCost (100).
	if tbl[TLOAD].constantGas != 100 {
		t.Errorf("TLOAD gas = %d, want 100", tbl[TLOAD].constantGas)
	}

	// TSTORE gas should be WarmStorageReadCost (100).
	if tbl[TSTORE].constantGas != 100 {
		t.Errorf("TSTORE gas = %d, want 100", tbl[TSTORE].constantGas)
	}

	// TLOAD should have no dynamic gas.
	if tbl[TLOAD].dynamicGas != nil {
		t.Error("TLOAD should have nil dynamicGas")
	}

	// TSTORE should have no dynamic gas.
	if tbl[TSTORE].dynamicGas != nil {
		t.Error("TSTORE should have nil dynamicGas")
	}
}

func TestTstoreMarkedAsWrite(t *testing.T) {
	tbl := NewCancunJumpTable()

	if !tbl[TSTORE].writes {
		t.Error("TSTORE should be marked as a write operation")
	}
}

func TestTloadTstorePreCancunInvalid(t *testing.T) {
	// TLOAD and TSTORE should not be available in Shanghai (pre-Cancun).
	tbl := NewShanghaiJumpTable()

	if tbl[TLOAD] != nil {
		t.Error("TLOAD should not be available in Shanghai jump table")
	}
	if tbl[TSTORE] != nil {
		t.Error("TSTORE should not be available in Shanghai jump table")
	}
}

// TestTloadTstoreEVMRun exercises TLOAD/TSTORE through the interpreter loop.
func TestTloadTstoreEVMRun(t *testing.T) {
	mock := newMockStateDB()
	addr := types.Address{0x42}

	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = mock

	// Bytecode: PUSH1 0xBE, PUSH1 0x01, TSTORE, PUSH1 0x01, TLOAD, PUSH1 0x00, MSTORE, PUSH1 0x20, PUSH1 0x00, RETURN
	// This stores 0xBE at transient key 1, loads it back, writes to memory, and returns it.
	code := []byte{
		byte(PUSH1), 0xBE, // push value
		byte(PUSH1), 0x01, // push key
		byte(TSTORE),      // tstore(key=1, value=0xBE)
		byte(PUSH1), 0x01, // push key
		byte(TLOAD),       // tload(key=1) -> 0xBE on stack
		byte(PUSH1), 0x00, // push memory offset
		byte(MSTORE),      // mstore(0, 0xBE)
		byte(PUSH1), 0x20, // push 32 (return size)
		byte(PUSH1), 0x00, // push 0 (return offset)
		byte(RETURN), // return memory[0:32]
	}

	contract := NewContract(types.Address{}, addr, big.NewInt(0), 100000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("EVM Run error: %v", err)
	}

	if len(ret) != 32 {
		t.Fatalf("return data length = %d, want 32", len(ret))
	}

	// The value 0xBE should be at the last byte of the 32-byte return (big-endian).
	if ret[31] != 0xBE {
		t.Errorf("return data[31] = 0x%x, want 0xBE", ret[31])
	}
}

// --- MCOPY tests (EIP-5656) ---

func TestOpMcopy(t *testing.T) {
	evm, contract, mem, st := setupTest()
	mem.Resize(128)
	pc := uint64(0)

	// Write [0x01, 0x02, 0x03, 0x04] at offset 0.
	mem.Set(0, 4, []byte{0x01, 0x02, 0x03, 0x04})

	// MCOPY: copy 4 bytes from src=0 to dest=32.
	st.Push(big.NewInt(4))  // size
	st.Push(big.NewInt(0))  // src
	st.Push(big.NewInt(32)) // dest
	opMcopy(&pc, evm, contract, mem, st)

	got := mem.Get(32, 4)
	if got[0] != 0x01 || got[1] != 0x02 || got[2] != 0x03 || got[3] != 0x04 {
		t.Errorf("MCOPY result = %x, want 01020304", got)
	}
}

func TestOpMcopyOverlap(t *testing.T) {
	evm, contract, mem, st := setupTest()
	mem.Resize(64)
	pc := uint64(0)

	// Write [0x01, 0x02, 0x03, 0x04] at offset 0.
	mem.Set(0, 4, []byte{0x01, 0x02, 0x03, 0x04})

	// MCOPY with overlapping region: copy 4 bytes from src=0 to dest=2.
	st.Push(big.NewInt(4)) // size
	st.Push(big.NewInt(0)) // src
	st.Push(big.NewInt(2)) // dest
	opMcopy(&pc, evm, contract, mem, st)

	got := mem.Get(2, 4)
	// Should be a copy of the original source data, not corrupted.
	if got[0] != 0x01 || got[1] != 0x02 || got[2] != 0x03 || got[3] != 0x04 {
		t.Errorf("MCOPY overlap result = %x, want 01020304", got)
	}
}

func TestOpMcopyZeroSize(t *testing.T) {
	evm, contract, mem, st := setupTest()
	mem.Resize(32)
	pc := uint64(0)

	mem.Set(0, 4, []byte{0xAA, 0xBB, 0xCC, 0xDD})

	// MCOPY with size=0 should be a no-op.
	st.Push(big.NewInt(0))  // size
	st.Push(big.NewInt(0))  // src
	st.Push(big.NewInt(16)) // dest
	opMcopy(&pc, evm, contract, mem, st)

	// Destination should be untouched (zeros).
	got := mem.Get(16, 4)
	for i, b := range got {
		if b != 0 {
			t.Errorf("MCOPY zero-size modified byte %d: 0x%x, want 0", i, b)
		}
	}
}

// --- BLOBHASH tests (EIP-4844) ---

func TestOpBlobHash(t *testing.T) {
	hash0 := types.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	hash1 := types.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	evm := NewEVM(BlockContext{}, TxContext{BlobHashes: []types.Hash{hash0, hash1}}, Config{})
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 1000000)
	mem := NewMemory()
	st := NewStack()
	pc := uint64(0)

	// Index 0 -> hash0
	st.Push(big.NewInt(0))
	opBlobHash(&pc, evm, contract, mem, st)
	got := types.BytesToHash(st.Pop().Bytes())
	if got != hash0 {
		t.Errorf("BLOBHASH(0) = %s, want %s", got, hash0)
	}

	// Index 1 -> hash1
	st.Push(big.NewInt(1))
	opBlobHash(&pc, evm, contract, mem, st)
	got = types.BytesToHash(st.Pop().Bytes())
	if got != hash1 {
		t.Errorf("BLOBHASH(1) = %s, want %s", got, hash1)
	}
}

func TestOpBlobHashOutOfRange(t *testing.T) {
	hash0 := types.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	evm := NewEVM(BlockContext{}, TxContext{BlobHashes: []types.Hash{hash0}}, Config{})
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 1000000)
	mem := NewMemory()
	st := NewStack()
	pc := uint64(0)

	// Index 5 (out of range) -> zero
	st.Push(big.NewInt(5))
	opBlobHash(&pc, evm, contract, mem, st)

	if st.Peek().Sign() != 0 {
		t.Errorf("BLOBHASH(out of range) = %s, want 0", st.Peek().String())
	}
}

func TestOpBlobHashNoBlobHashes(t *testing.T) {
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 1000000)
	mem := NewMemory()
	st := NewStack()
	pc := uint64(0)

	// No blob hashes -> any index returns zero.
	st.Push(big.NewInt(0))
	opBlobHash(&pc, evm, contract, mem, st)

	if st.Peek().Sign() != 0 {
		t.Errorf("BLOBHASH(no blobs) = %s, want 0", st.Peek().String())
	}
}

// --- BLOBBASEFEE tests (EIP-7516) ---

func TestOpBlobBaseFee(t *testing.T) {
	evm := NewEVM(BlockContext{BlobBaseFee: big.NewInt(1000)}, TxContext{}, Config{})
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 1000000)
	mem := NewMemory()
	st := NewStack()
	pc := uint64(0)

	opBlobBaseFee(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 1000 {
		t.Errorf("BLOBBASEFEE = %d, want 1000", st.Peek().Int64())
	}
}

func TestOpBlobBaseFeeNil(t *testing.T) {
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 1000000)
	mem := NewMemory()
	st := NewStack()
	pc := uint64(0)

	opBlobBaseFee(&pc, evm, contract, mem, st)

	if st.Peek().Sign() != 0 {
		t.Errorf("BLOBBASEFEE(nil) = %s, want 0", st.Peek().String())
	}
}

// --- Jump table wiring tests ---

func TestCancunJumpTableHasNewOpcodes(t *testing.T) {
	tbl := NewCancunJumpTable()

	opcodes := []struct {
		op   OpCode
		name string
	}{
		{TLOAD, "TLOAD"},
		{TSTORE, "TSTORE"},
		{MCOPY, "MCOPY"},
		{BLOBHASH, "BLOBHASH"},
		{BLOBBASEFEE, "BLOBBASEFEE"},
	}

	for _, tc := range opcodes {
		if tbl[tc.op] == nil {
			t.Errorf("%s (0x%02x) not wired in Cancun jump table", tc.name, byte(tc.op))
			continue
		}
		if tbl[tc.op].execute == nil {
			t.Errorf("%s (0x%02x) has nil execute function", tc.name, byte(tc.op))
		}
	}
}

func TestShanghaiJumpTableLacksCancunOpcodes(t *testing.T) {
	tbl := NewShanghaiJumpTable()

	opcodes := []struct {
		op   OpCode
		name string
	}{
		{TLOAD, "TLOAD"},
		{TSTORE, "TSTORE"},
		{MCOPY, "MCOPY"},
		{BLOBHASH, "BLOBHASH"},
		{BLOBBASEFEE, "BLOBBASEFEE"},
	}

	for _, tc := range opcodes {
		if tbl[tc.op] != nil {
			t.Errorf("%s (0x%02x) should not be in Shanghai jump table", tc.name, byte(tc.op))
		}
	}
}
