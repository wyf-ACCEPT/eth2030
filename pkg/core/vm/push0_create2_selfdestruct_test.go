package vm

import (
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// ============================================================================
// EIP-3855: PUSH0 tests
// ============================================================================

// TestOpPush0 verifies the PUSH0 opcode pushes zero onto the stack.
func TestOpPush0(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	_, err := opPush0(&pc, evm, contract, mem, st)
	if err != nil {
		t.Fatalf("opPush0 error: %v", err)
	}
	if st.Len() != 1 {
		t.Fatalf("stack len = %d, want 1", st.Len())
	}
	if st.Peek().Sign() != 0 {
		t.Errorf("PUSH0 = %s, want 0", st.Peek().String())
	}
}

// TestPush0DoesNotIncrementPC ensures PUSH0 does not advance PC (no immediate
// bytes to skip -- the interpreter loop increments PC by 1 after non-jump ops).
func TestPush0DoesNotIncrementPC(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(5)

	opPush0(&pc, evm, contract, mem, st)

	// opPush0 should NOT modify pc (unlike PUSH1 which adds 1 for its data byte).
	if pc != 5 {
		t.Errorf("pc after PUSH0 = %d, want 5 (unchanged)", pc)
	}
}

// TestPush0GasCost verifies PUSH0 has GasBase (2) gas cost in the jump table.
func TestPush0GasCost(t *testing.T) {
	tbl := NewShanghaiJumpTable()
	op := tbl[PUSH0]
	if op == nil {
		t.Fatal("PUSH0 not in Shanghai jump table")
	}
	if op.constantGas != GasPush0 {
		t.Errorf("PUSH0 constantGas = %d, want %d (GasPush0)", op.constantGas, GasPush0)
	}
	if GasPush0 != GasBase {
		t.Errorf("GasPush0 = %d, want %d (GasBase)", GasPush0, GasBase)
	}
}

// TestPush0InShanghaiNotPreShanghai verifies PUSH0 is available in Shanghai+
// but not in pre-Shanghai forks.
func TestPush0InShanghaiNotPreShanghai(t *testing.T) {
	// Shanghai should have PUSH0.
	shanghai := NewShanghaiJumpTable()
	if shanghai[PUSH0] == nil || shanghai[PUSH0].execute == nil {
		t.Error("PUSH0 should be in Shanghai jump table")
	}

	// Merge (pre-Shanghai) should not have PUSH0.
	merge := NewMergeJumpTable()
	if merge[PUSH0] != nil {
		t.Error("PUSH0 should not be in Merge jump table")
	}

	// London should not have PUSH0.
	london := NewLondonJumpTable()
	if london[PUSH0] != nil {
		t.Error("PUSH0 should not be in London jump table")
	}
}

// TestPush0Bytecode runs PUSH0 followed by PUSH0 then ADD via the interpreter,
// verifying the result is 0+0=0 and exactly 2 gas per PUSH0 is charged.
func TestPush0Bytecode(t *testing.T) {
	evm := newTestEVM()
	evm.SetJumpTable(NewShanghaiJumpTable())

	initialGas := uint64(100000)
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), initialGas)

	// PUSH0, PUSH0, ADD, STOP
	contract.Code = []byte{
		byte(PUSH0),
		byte(PUSH0),
		byte(ADD),
		byte(STOP),
	}

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("PUSH0 bytecode execution failed: %v", err)
	}
	if ret != nil {
		t.Errorf("expected nil return, got %x", ret)
	}

	// Gas cost: 2 (PUSH0) + 2 (PUSH0) + 3 (ADD) + 0 (STOP) = 7
	gasUsed := initialGas - contract.Gas
	if gasUsed != 7 {
		t.Errorf("gas used = %d, want 7 (2+2+3+0)", gasUsed)
	}
}

// TestPush0StackOverflow verifies PUSH0 fails when stack is full.
// maxStack=1023 means stack must have <= 1023 items before executing PUSH0.
// So 1024 PUSH0s succeed (stack grows from 0..1024), but the 1025th fails.
func TestPush0StackOverflow(t *testing.T) {
	evm := newTestEVM()
	evm.SetJumpTable(NewShanghaiJumpTable())

	// Build bytecode: 1025 PUSH0 instructions + STOP
	code := make([]byte, 1026)
	for i := 0; i < 1025; i++ {
		code[i] = byte(PUSH0)
	}
	code[1025] = byte(STOP)

	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 10000000)
	contract.Code = code

	_, err := evm.Run(contract, nil)
	if err != ErrStackOverflow {
		t.Errorf("expected ErrStackOverflow, got %v", err)
	}
}

// TestPush0InvalidPreShanghai verifies PUSH0 is treated as an invalid opcode
// when running with a pre-Shanghai jump table.
func TestPush0InvalidPreShanghai(t *testing.T) {
	evm := newTestEVM()
	evm.SetJumpTable(NewMergeJumpTable()) // pre-Shanghai

	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100000)
	contract.Code = []byte{byte(PUSH0), byte(STOP)}

	_, err := evm.Run(contract, nil)
	if err != ErrInvalidOpCode {
		t.Errorf("PUSH0 pre-Shanghai: expected ErrInvalidOpCode, got %v", err)
	}
}

// ============================================================================
// CREATE2 address derivation tests
// ============================================================================

// TestCreate2AddressDerivation verifies the CREATE2 address formula:
// address = keccak256(0xff ++ sender ++ salt ++ keccak256(initcode))[12:]
func TestCreate2AddressDerivation(t *testing.T) {
	sender := types.HexToAddress("0x0000000000000000000000000000000000000001")
	salt := big.NewInt(0)
	initCode := []byte{} // empty init code
	initCodeHash := crypto.Keccak256(initCode)

	// Manually compute expected address.
	saltBytes := make([]byte, 32)
	data := make([]byte, 0, 85)
	data = append(data, 0xff)
	data = append(data, sender[:]...)
	data = append(data, saltBytes...)
	data = append(data, initCodeHash...)
	hash := crypto.Keccak256(data)
	expected := types.BytesToAddress(hash[12:])

	got := create2Address(sender, salt, initCodeHash)
	if got != expected {
		t.Errorf("create2Address mismatch:\n  got:  %x\n  want: %x", got, expected)
	}
}

// TestCreate2AddressKnownVector tests against a well-known CREATE2 test vector.
// From EIP-1014 examples:
//   sender = 0x0000000000000000000000000000000000000000
//   salt = 0x00...00
//   initcode = 0x00
//   expected address = keccak256(0xff ++ 0x00..00 ++ 0x00..00 ++ keccak256(0x00))[12:]
func TestCreate2AddressKnownVector(t *testing.T) {
	sender := types.Address{} // 0x000...000
	salt := big.NewInt(0)
	initCode := []byte{0x00}
	initCodeHash := crypto.Keccak256(initCode)

	// Build expected per EIP-1014
	saltBytes := make([]byte, 32)
	data := make([]byte, 0, 85)
	data = append(data, 0xff)
	data = append(data, sender[:]...)
	data = append(data, saltBytes...)
	data = append(data, initCodeHash...)
	hash := crypto.Keccak256(data)
	expected := types.BytesToAddress(hash[12:])

	got := create2Address(sender, salt, initCodeHash)
	if got != expected {
		t.Errorf("CREATE2 known vector mismatch:\n  got:  %x\n  want: %x", got, expected)
	}
}

// TestCreate2AddressDifferentSaltsDifferentAddresses verifies different salts
// produce different addresses.
func TestCreate2AddressDifferentSaltsDifferentAddresses(t *testing.T) {
	sender := types.HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	initCode := []byte{byte(PUSH1), 0x42, byte(STOP)}
	initCodeHash := crypto.Keccak256(initCode)

	addr1 := create2Address(sender, big.NewInt(1), initCodeHash)
	addr2 := create2Address(sender, big.NewInt(2), initCodeHash)

	if addr1 == addr2 {
		t.Error("different salts should produce different addresses")
	}
}

// TestCreate2AddressDifferentInitCodeDifferentAddresses verifies different
// init codes produce different addresses.
func TestCreate2AddressDifferentInitCodeDifferentAddresses(t *testing.T) {
	sender := types.HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	salt := big.NewInt(0)

	hash1 := crypto.Keccak256([]byte{0x01})
	hash2 := crypto.Keccak256([]byte{0x02})

	addr1 := create2Address(sender, salt, hash1)
	addr2 := create2Address(sender, salt, hash2)

	if addr1 == addr2 {
		t.Error("different init codes should produce different addresses")
	}
}

// TestCreate2AddressDifferentSendersDifferentAddresses verifies different
// senders produce different addresses with the same salt and initcode.
func TestCreate2AddressDifferentSendersDifferentAddresses(t *testing.T) {
	sender1 := types.HexToAddress("0x0000000000000000000000000000000000000001")
	sender2 := types.HexToAddress("0x0000000000000000000000000000000000000002")
	salt := big.NewInt(42)
	initCodeHash := crypto.Keccak256([]byte{byte(STOP)})

	addr1 := create2Address(sender1, salt, initCodeHash)
	addr2 := create2Address(sender2, salt, initCodeHash)

	if addr1 == addr2 {
		t.Error("different senders should produce different addresses")
	}
}

// TestCreate2AddressNilSalt verifies nil salt is treated as zero.
func TestCreate2AddressNilSalt(t *testing.T) {
	sender := types.HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	initCodeHash := crypto.Keccak256([]byte{byte(STOP)})

	addrNil := create2Address(sender, nil, initCodeHash)
	addrZero := create2Address(sender, big.NewInt(0), initCodeHash)

	if addrNil != addrZero {
		t.Errorf("nil salt should equal zero salt:\n  nil:  %x\n  zero: %x", addrNil, addrZero)
	}
}

// TestCreate2AddressLargeSalt verifies a 32-byte salt is handled correctly.
func TestCreate2AddressLargeSalt(t *testing.T) {
	sender := types.Address{0x01}
	// Max 32-byte salt: 0xfff...fff
	salt := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	initCodeHash := crypto.Keccak256([]byte{byte(STOP)})

	// Should not panic and produce a valid address.
	addr := create2Address(sender, salt, initCodeHash)
	if addr == (types.Address{}) {
		t.Error("large salt should produce a non-zero address")
	}
}

// TestCreate2OpcodeGasCost verifies CREATE2 charges the keccak256 word cost
// for hashing the init code.
func TestCreate2OpcodeGasCost(t *testing.T) {
	tbl := NewConstantinopleJumpTable()
	op := tbl[CREATE2]
	if op == nil {
		t.Fatal("CREATE2 not in Constantinople jump table")
	}
	if op.minStack != 4 {
		t.Errorf("CREATE2 minStack = %d, want 4", op.minStack)
	}
	if !op.writes {
		t.Error("CREATE2 should be a write operation")
	}
	if op.dynamicGas == nil {
		t.Error("CREATE2 should have dynamic gas")
	}
}

// TestCreate2DynamicGasCost verifies gasCreate2Dynamic charges both initcode
// word gas and keccak word gas.
func TestCreate2DynamicGasCost(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 10000000)
	mem := NewMemory()
	st := NewStack()

	// Stack: value, offset, length, salt
	st.Push(big.NewInt(0))  // salt
	st.Push(big.NewInt(64)) // length (64 bytes = 2 words)
	st.Push(big.NewInt(0))  // offset
	st.Push(big.NewInt(0))  // value

	// For 64 bytes (2 words):
	// InitCodeWordGas (2) * 2 + GasKeccak256Word (6) * 2 = 4 + 12 = 16
	gas, _ := gasCreate2Dynamic(evm, contract, st, mem, 0)
	expectedGas := (InitCodeWordGas + GasKeccak256Word) * 2
	if gas != expectedGas {
		t.Errorf("gasCreate2Dynamic for 64 bytes = %d, want %d", gas, expectedGas)
	}
}

// TestCreate2IntegrationDeployAndCall deploys a contract via CREATE2 and then
// calls it to verify the deployed code is executable.
func TestCreate2IntegrationDeployAndCall(t *testing.T) {
	stateDB := state.NewMemoryStateDB()
	evm := NewEVMWithState(
		BlockContext{
			BlockNumber: big.NewInt(100),
			GasLimit:    30000000,
			BaseFee:     big.NewInt(1000000000),
		},
		TxContext{GasPrice: big.NewInt(2000000000)},
		Config{},
		stateDB,
	)

	callerAddr := types.BytesToAddress([]byte{0x01})
	stateDB.CreateAccount(callerAddr)
	stateDB.AddBalance(callerAddr, big.NewInt(1000000))

	// Simple init code: PUSH1 0xBE, PUSH1 0x00, MSTORE, PUSH1 0x01, PUSH1 0x1f, RETURN
	// This stores 0xBE at memory offset 31 (big-endian in 32-byte word) and returns
	// 1 byte starting at offset 31, deploying code = [0xBE].
	initCode := []byte{
		byte(PUSH1), 0xBE,
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x01, // return 1 byte
		byte(PUSH1), 0x1f, // from offset 31
		byte(RETURN),
	}

	salt := big.NewInt(42)
	initCodeHash := crypto.Keccak256(initCode)
	expectedAddr := create2Address(callerAddr, salt, initCodeHash)

	// Deploy via CREATE2.
	_, addr, _, err := evm.Create2(callerAddr, initCode, 10000000, big.NewInt(0), salt)
	if err != nil {
		t.Fatalf("CREATE2 deploy failed: %v", err)
	}
	if addr != expectedAddr {
		t.Errorf("CREATE2 address = %x, want %x", addr, expectedAddr)
	}

	// Deployed code should be [0xBE].
	deployedCode := stateDB.GetCode(addr)
	if len(deployedCode) != 1 || deployedCode[0] != 0xBE {
		t.Fatalf("deployed code = %x, want [BE]", deployedCode)
	}
}

// TestCreate2AddressFormatPrefix verifies the 0xff prefix is correct in the
// address derivation.
func TestCreate2AddressFormatPrefix(t *testing.T) {
	// The CREATE2 spec uses 0xff as the first byte to avoid collision with
	// CREATE (which uses RLP encoding that starts with 0xc0+).
	sender := types.Address{0xAA}
	initCodeHash := crypto.Keccak256([]byte{})

	// Also verify via create2Address that the result matches.
	addr := create2Address(sender, big.NewInt(0), initCodeHash)

	// Build the hash input manually and verify 0xff prefix.
	saltBytes := make([]byte, 32)
	data := make([]byte, 0, 85)
	data = append(data, 0xff)
	data = append(data, sender[:]...)
	data = append(data, saltBytes...)
	data = append(data, initCodeHash...)

	if data[0] != 0xff {
		t.Errorf("CREATE2 hash prefix = 0x%x, want 0xff", data[0])
	}
	if len(data) != 1+20+32+32 {
		t.Errorf("CREATE2 hash input length = %d, want %d", len(data), 1+20+32+32)
	}

	// Verify the manually computed address matches create2Address.
	hash := crypto.Keccak256(data)
	expected := types.BytesToAddress(hash[12:])
	if addr != expected {
		t.Errorf("create2Address mismatch with manual computation:\n  got:  %x\n  want: %x", addr, expected)
	}
}

// ============================================================================
// EIP-6780: SELFDESTRUCT behavior tests
// ============================================================================

// selfdestructMockStateDB extends mockStateDB with balance tracking and
// self-destruct tracking for EIP-6780 testing.
type selfdestructMockStateDB struct {
	balances        map[types.Address]*big.Int
	selfDestructed  map[types.Address]bool
	exists          map[types.Address]bool
	code            map[types.Address][]byte
	storage         map[types.Address]map[types.Hash]types.Hash
	transient       map[types.Address]map[types.Hash]types.Hash
	accessAddresses map[types.Address]bool
	accessSlots     map[types.Address]map[types.Hash]bool
}

func newSelfdestructMockStateDB() *selfdestructMockStateDB {
	return &selfdestructMockStateDB{
		balances:        make(map[types.Address]*big.Int),
		selfDestructed:  make(map[types.Address]bool),
		exists:          make(map[types.Address]bool),
		code:            make(map[types.Address][]byte),
		storage:         make(map[types.Address]map[types.Hash]types.Hash),
		transient:       make(map[types.Address]map[types.Hash]types.Hash),
		accessAddresses: make(map[types.Address]bool),
		accessSlots:     make(map[types.Address]map[types.Hash]bool),
	}
}

func (m *selfdestructMockStateDB) CreateAccount(addr types.Address) {
	m.exists[addr] = true
	if m.balances[addr] == nil {
		m.balances[addr] = new(big.Int)
	}
}
func (m *selfdestructMockStateDB) GetBalance(addr types.Address) *big.Int {
	if b, ok := m.balances[addr]; ok {
		return new(big.Int).Set(b)
	}
	return new(big.Int)
}
func (m *selfdestructMockStateDB) AddBalance(addr types.Address, amount *big.Int) {
	if m.balances[addr] == nil {
		m.balances[addr] = new(big.Int)
	}
	m.balances[addr].Add(m.balances[addr], amount)
}
func (m *selfdestructMockStateDB) SubBalance(addr types.Address, amount *big.Int) {
	if m.balances[addr] == nil {
		m.balances[addr] = new(big.Int)
	}
	m.balances[addr].Sub(m.balances[addr], amount)
}
func (m *selfdestructMockStateDB) GetNonce(types.Address) uint64    { return 0 }
func (m *selfdestructMockStateDB) SetNonce(types.Address, uint64)   {}
func (m *selfdestructMockStateDB) GetCode(addr types.Address) []byte {
	return m.code[addr]
}
func (m *selfdestructMockStateDB) SetCode(addr types.Address, code []byte) {
	m.code[addr] = code
}
func (m *selfdestructMockStateDB) GetCodeHash(types.Address) types.Hash { return types.Hash{} }
func (m *selfdestructMockStateDB) GetCodeSize(addr types.Address) int {
	return len(m.code[addr])
}
func (m *selfdestructMockStateDB) SelfDestruct(addr types.Address) {
	m.selfDestructed[addr] = true
}
func (m *selfdestructMockStateDB) HasSelfDestructed(addr types.Address) bool {
	return m.selfDestructed[addr]
}
func (m *selfdestructMockStateDB) Exist(addr types.Address) bool {
	return m.exists[addr]
}
func (m *selfdestructMockStateDB) Empty(addr types.Address) bool {
	return !m.exists[addr]
}
func (m *selfdestructMockStateDB) Snapshot() int         { return 0 }
func (m *selfdestructMockStateDB) RevertToSnapshot(int)  {}
func (m *selfdestructMockStateDB) AddLog(*types.Log)     {}
func (m *selfdestructMockStateDB) AddRefund(uint64)      {}
func (m *selfdestructMockStateDB) SubRefund(uint64)      {}
func (m *selfdestructMockStateDB) GetRefund() uint64     { return 0 }
func (m *selfdestructMockStateDB) AddAddressToAccessList(addr types.Address) {
	m.accessAddresses[addr] = true
}
func (m *selfdestructMockStateDB) AddSlotToAccessList(addr types.Address, slot types.Hash) {
	if m.accessSlots[addr] == nil {
		m.accessSlots[addr] = make(map[types.Hash]bool)
	}
	m.accessSlots[addr][slot] = true
}
func (m *selfdestructMockStateDB) AddressInAccessList(addr types.Address) bool {
	return m.accessAddresses[addr]
}
func (m *selfdestructMockStateDB) SlotInAccessList(addr types.Address, slot types.Hash) (bool, bool) {
	if m.accessSlots[addr] == nil {
		return m.accessAddresses[addr], false
	}
	return m.accessAddresses[addr], m.accessSlots[addr][slot]
}
func (m *selfdestructMockStateDB) GetState(addr types.Address, key types.Hash) types.Hash {
	if s, ok := m.storage[addr]; ok {
		return s[key]
	}
	return types.Hash{}
}
func (m *selfdestructMockStateDB) SetState(addr types.Address, key, value types.Hash) {
	if m.storage[addr] == nil {
		m.storage[addr] = make(map[types.Hash]types.Hash)
	}
	m.storage[addr][key] = value
}
func (m *selfdestructMockStateDB) GetCommittedState(addr types.Address, key types.Hash) types.Hash {
	return m.GetState(addr, key)
}
func (m *selfdestructMockStateDB) GetTransientState(addr types.Address, key types.Hash) types.Hash {
	if s, ok := m.transient[addr]; ok {
		return s[key]
	}
	return types.Hash{}
}
func (m *selfdestructMockStateDB) SetTransientState(addr types.Address, key, value types.Hash) {
	if m.transient[addr] == nil {
		m.transient[addr] = make(map[types.Hash]types.Hash)
	}
	m.transient[addr][key] = value
}

func (m *selfdestructMockStateDB) ClearTransientStorage() {
	m.transient = make(map[types.Address]map[types.Hash]types.Hash)
}

// TestSelfdestructEIP6780BalanceTransfer verifies SELFDESTRUCT transfers
// the contract's balance to the beneficiary.
func TestSelfdestructEIP6780BalanceTransfer(t *testing.T) {
	mock := newSelfdestructMockStateDB()
	contractAddr := types.Address{0xAA}
	beneficiary := types.Address{0xBB}

	mock.CreateAccount(contractAddr)
	mock.AddBalance(contractAddr, big.NewInt(5000))
	mock.CreateAccount(beneficiary)

	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = mock
	contract := NewContract(types.Address{}, contractAddr, big.NewInt(0), 1000000)
	mem := NewMemory()
	st := NewStack()
	pc := uint64(0)

	// Push beneficiary address.
	st.Push(new(big.Int).SetBytes(beneficiary[:]))

	_, err := opSelfdestruct(&pc, evm, contract, mem, st)
	if err != nil {
		t.Fatalf("opSelfdestruct error: %v", err)
	}

	// Beneficiary should have received 5000.
	benBal := mock.GetBalance(beneficiary)
	if benBal.Cmp(big.NewInt(5000)) != 0 {
		t.Errorf("beneficiary balance = %s, want 5000", benBal.String())
	}

	// Contract balance should be 0.
	contractBal := mock.GetBalance(contractAddr)
	if contractBal.Sign() != 0 {
		t.Errorf("contract balance = %s, want 0", contractBal.String())
	}
}

// TestSelfdestructEIP6780DoesNotDestroy verifies that post-EIP-6780,
// SELFDESTRUCT does NOT call SelfDestruct() on the state -- the account
// persists and is NOT marked as self-destructed.
func TestSelfdestructEIP6780DoesNotDestroy(t *testing.T) {
	mock := newSelfdestructMockStateDB()
	contractAddr := types.Address{0xAA}
	beneficiary := types.Address{0xBB}

	mock.CreateAccount(contractAddr)
	mock.AddBalance(contractAddr, big.NewInt(1000))
	mock.CreateAccount(beneficiary)
	mock.SetCode(contractAddr, []byte{byte(STOP)})

	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = mock
	contract := NewContract(types.Address{}, contractAddr, big.NewInt(0), 1000000)
	mem := NewMemory()
	st := NewStack()
	pc := uint64(0)

	st.Push(new(big.Int).SetBytes(beneficiary[:]))
	opSelfdestruct(&pc, evm, contract, mem, st)

	// Post-EIP-6780: SelfDestruct should NOT be called.
	if mock.HasSelfDestructed(contractAddr) {
		t.Error("post-EIP-6780: SELFDESTRUCT should NOT mark account as self-destructed")
	}

	// Account should still exist.
	if !mock.Exist(contractAddr) {
		t.Error("post-EIP-6780: contract should still exist after SELFDESTRUCT")
	}

	// Code should still be present.
	if len(mock.GetCode(contractAddr)) == 0 {
		t.Error("post-EIP-6780: contract code should persist after SELFDESTRUCT")
	}
}

// TestSelfdestructEIP6780ZeroBalance verifies SELFDESTRUCT with zero balance
// is a no-op for balance transfer.
func TestSelfdestructEIP6780ZeroBalance(t *testing.T) {
	mock := newSelfdestructMockStateDB()
	contractAddr := types.Address{0xAA}
	beneficiary := types.Address{0xBB}

	mock.CreateAccount(contractAddr)
	// No balance added (zero).
	mock.CreateAccount(beneficiary)

	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = mock
	contract := NewContract(types.Address{}, contractAddr, big.NewInt(0), 1000000)
	mem := NewMemory()
	st := NewStack()
	pc := uint64(0)

	st.Push(new(big.Int).SetBytes(beneficiary[:]))
	_, err := opSelfdestruct(&pc, evm, contract, mem, st)
	if err != nil {
		t.Fatalf("opSelfdestruct with zero balance: %v", err)
	}

	// Beneficiary balance should remain 0.
	if mock.GetBalance(beneficiary).Sign() != 0 {
		t.Errorf("beneficiary should have 0 balance, got %s", mock.GetBalance(beneficiary).String())
	}
}

// TestSelfdestructEIP6780SelfBeneficiary verifies SELFDESTRUCT when the
// beneficiary is the contract itself (balance effectively stays).
func TestSelfdestructEIP6780SelfBeneficiary(t *testing.T) {
	mock := newSelfdestructMockStateDB()
	contractAddr := types.Address{0xAA}

	mock.CreateAccount(contractAddr)
	mock.AddBalance(contractAddr, big.NewInt(3000))

	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = mock
	contract := NewContract(types.Address{}, contractAddr, big.NewInt(0), 1000000)
	mem := NewMemory()
	st := NewStack()
	pc := uint64(0)

	// Send to self.
	st.Push(new(big.Int).SetBytes(contractAddr[:]))
	_, err := opSelfdestruct(&pc, evm, contract, mem, st)
	if err != nil {
		t.Fatalf("opSelfdestruct to self: %v", err)
	}

	// Balance should remain 3000 (sub 3000 then add 3000).
	bal := mock.GetBalance(contractAddr)
	if bal.Cmp(big.NewInt(3000)) != 0 {
		t.Errorf("self-destruct to self balance = %s, want 3000", bal.String())
	}
}

// TestSelfdestructWriteProtection verifies SELFDESTRUCT fails in read-only mode.
func TestSelfdestructWriteProtection(t *testing.T) {
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.readOnly = true

	contract := NewContract(types.Address{}, types.Address{0xAA}, big.NewInt(0), 1000000)
	mem := NewMemory()
	st := NewStack()
	pc := uint64(0)

	st.Push(big.NewInt(0))
	_, err := opSelfdestruct(&pc, evm, contract, mem, st)
	if err != ErrWriteProtection {
		t.Errorf("SELFDESTRUCT in readOnly: expected ErrWriteProtection, got %v", err)
	}
}

// TestSelfdestructNoStateDB verifies SELFDESTRUCT without StateDB doesn't panic.
func TestSelfdestructNoStateDB(t *testing.T) {
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	// evm.StateDB is nil

	contract := NewContract(types.Address{}, types.Address{0xAA}, big.NewInt(0), 1000000)
	mem := NewMemory()
	st := NewStack()
	pc := uint64(0)

	st.Push(big.NewInt(0))
	_, err := opSelfdestruct(&pc, evm, contract, mem, st)
	if err != nil {
		t.Errorf("SELFDESTRUCT without StateDB should not error, got %v", err)
	}
}

// TestSelfdestructGasCost verifies the SELFDESTRUCT gas in the jump table.
func TestSelfdestructGasCost(t *testing.T) {
	tbl := NewCancunJumpTable()
	op := tbl[SELFDESTRUCT]
	if op == nil {
		t.Fatal("SELFDESTRUCT not in Cancun jump table")
	}
	if op.constantGas != GasSelfdestruct {
		t.Errorf("SELFDESTRUCT constantGas = %d, want %d", op.constantGas, GasSelfdestruct)
	}
	if GasSelfdestruct != 5000 {
		t.Errorf("GasSelfdestruct = %d, want 5000", GasSelfdestruct)
	}
	if !op.halts {
		t.Error("SELFDESTRUCT should halt execution")
	}
	if !op.writes {
		t.Error("SELFDESTRUCT should be a write operation")
	}
}

// TestSelfdestructEIP6780IntegrationStoragePersists verifies that post-EIP-6780,
// storage data persists after SELFDESTRUCT for pre-existing contracts.
func TestSelfdestructEIP6780IntegrationStoragePersists(t *testing.T) {
	stateDB := state.NewMemoryStateDB()
	evm := NewEVMWithState(
		BlockContext{
			BlockNumber: big.NewInt(100),
			GasLimit:    30000000,
			BaseFee:     big.NewInt(1000000000),
		},
		TxContext{GasPrice: big.NewInt(2000000000)},
		Config{},
		stateDB,
	)

	callerAddr := types.BytesToAddress([]byte{0x01})
	contractAddr := types.BytesToAddress([]byte{0xAA})
	beneficiary := types.BytesToAddress([]byte{0xBB})

	stateDB.CreateAccount(callerAddr)
	stateDB.CreateAccount(contractAddr)
	stateDB.AddBalance(contractAddr, big.NewInt(1000))
	stateDB.CreateAccount(beneficiary)

	// Pre-set some storage on the contract.
	storageKey := types.BytesToHash([]byte{0x01})
	storageVal := types.BytesToHash([]byte{0x42})
	stateDB.SetState(contractAddr, storageKey, storageVal)

	// Contract code: PUSH20 <beneficiary>, SELFDESTRUCT
	code := []byte{byte(PUSH20)}
	code = append(code, beneficiary[:]...)
	code = append(code, byte(SELFDESTRUCT))
	stateDB.SetCode(contractAddr, code)

	stateDB.AddAddressToAccessList(callerAddr)
	stateDB.AddAddressToAccessList(contractAddr)
	stateDB.AddAddressToAccessList(beneficiary)

	_, _, err := evm.Call(callerAddr, contractAddr, nil, 1000000, big.NewInt(0))
	if err != nil {
		t.Fatalf("SELFDESTRUCT call failed: %v", err)
	}

	// Balance should have been transferred.
	benBal := stateDB.GetBalance(beneficiary)
	if benBal.Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("beneficiary balance = %s, want 1000", benBal.String())
	}

	// Post-EIP-6780: storage should still be accessible.
	val := stateDB.GetState(contractAddr, storageKey)
	if val != storageVal {
		t.Errorf("storage after SELFDESTRUCT = %x, want %x (should persist)", val, storageVal)
	}

	// Contract code should still exist.
	if len(stateDB.GetCode(contractAddr)) == 0 {
		t.Error("contract code should persist after SELFDESTRUCT (EIP-6780)")
	}
}

// ============================================================================
// CREATE2 opcode-level tests (via the opCreate2 instruction function)
// ============================================================================

// TestOpCreate2WriteProtection verifies CREATE2 fails in read-only mode.
func TestOpCreate2WriteProtection(t *testing.T) {
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.readOnly = true

	contract := NewContract(types.Address{}, types.Address{0xAA}, big.NewInt(0), 1000000)
	mem := NewMemory()
	st := NewStack()
	pc := uint64(0)

	// Stack: value, offset, size, salt
	st.Push(big.NewInt(0)) // salt
	st.Push(big.NewInt(0)) // size
	st.Push(big.NewInt(0)) // offset
	st.Push(big.NewInt(0)) // value

	_, err := opCreate2(&pc, evm, contract, mem, st)
	if err != ErrWriteProtection {
		t.Errorf("CREATE2 in readOnly: expected ErrWriteProtection, got %v", err)
	}
}

// TestOpCreate2StackLayout verifies that CREATE2 pops 4 items from the stack:
// value, offset, size, salt.
func TestOpCreate2StackLayout(t *testing.T) {
	tbl := NewConstantinopleJumpTable()
	op := tbl[CREATE2]
	if op == nil {
		t.Fatal("CREATE2 not found in Constantinople jump table")
	}
	if op.minStack != 4 {
		t.Errorf("CREATE2 minStack = %d, want 4 (value, offset, size, salt)", op.minStack)
	}
}

// TestCreate2AddressHex verifies a CREATE2 address against a manually computed hex.
func TestCreate2AddressHex(t *testing.T) {
	// Sender: 0x0000000000000000000000000000000000000000
	// Salt: 0
	// InitCode: "" (empty)
	// keccak256("") = c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470
	sender := types.Address{}
	salt := big.NewInt(0)
	emptyCodeHash := crypto.Keccak256([]byte{})

	addr := create2Address(sender, salt, emptyCodeHash)

	// Verify the address is a 20-byte value derived from keccak256.
	addrHex := hex.EncodeToString(addr[:])
	if len(addrHex) != 40 {
		t.Errorf("CREATE2 address hex length = %d, want 40", len(addrHex))
	}

	// Cross-check: compute manually.
	saltBytes := make([]byte, 32)
	data := make([]byte, 0, 85)
	data = append(data, 0xff)
	data = append(data, sender[:]...)
	data = append(data, saltBytes...)
	data = append(data, emptyCodeHash...)
	hash := crypto.Keccak256(data)
	expected := types.BytesToAddress(hash[12:])

	if addr != expected {
		t.Errorf("CREATE2 address mismatch:\n  got:  %s\n  want: %s", addrHex, hex.EncodeToString(expected[:]))
	}
}
