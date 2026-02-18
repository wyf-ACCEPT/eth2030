package vm

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// TestDivisionByZero verifies that DIV and MOD return 0 when dividing by zero.
func TestDivisionByZero(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	// DIV: 10 / 0 = 0
	st.Push(big.NewInt(0))
	st.Push(big.NewInt(10))
	opDiv(&pc, evm, contract, mem, st)
	if st.Peek().Sign() != 0 {
		t.Errorf("DIV(10, 0) = %s, want 0", st.Peek().String())
	}
	st.Pop()

	// MOD: 10 % 0 = 0
	st.Push(big.NewInt(0))
	st.Push(big.NewInt(10))
	opMod(&pc, evm, contract, mem, st)
	if st.Peek().Sign() != 0 {
		t.Errorf("MOD(10, 0) = %s, want 0", st.Peek().String())
	}
	st.Pop()

	// SDIV: 10 / 0 = 0
	st.Push(big.NewInt(0))
	st.Push(big.NewInt(10))
	opSdiv(&pc, evm, contract, mem, st)
	if st.Peek().Sign() != 0 {
		t.Errorf("SDIV(10, 0) = %s, want 0", st.Peek().String())
	}
	st.Pop()

	// SMOD: 10 % 0 = 0
	st.Push(big.NewInt(0))
	st.Push(big.NewInt(10))
	opSmod(&pc, evm, contract, mem, st)
	if st.Peek().Sign() != 0 {
		t.Errorf("SMOD(10, 0) = %s, want 0", st.Peek().String())
	}
}

// TestSignedOverflow verifies that SDIV(-2^255, -1) returns -2^255.
// In two's complement, -2^255 / -1 would be 2^255 which overflows the
// signed 256-bit range. The EVM spec says the result is -2^255 (no overflow).
func TestSignedOverflow(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	// -2^255 in two's complement is the min signed int256: 0x800...0
	minInt256 := new(big.Int).Lsh(big.NewInt(1), 255)
	// -1 in two's complement uint256 is 0xFFF...F (max uint256)
	negOne := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

	st.Push(negOne)     // -1 as uint256
	st.Push(minInt256)  // -2^255 as uint256 (top bit set)
	opSdiv(&pc, evm, contract, mem, st)

	// Result should be -2^255 (i.e., minInt256 as unsigned) since the overflow
	// is clamped per the EVM yellow paper.
	result := st.Peek()
	if result.Cmp(minInt256) != 0 {
		t.Errorf("SDIV(-2^255, -1) = %s, want %s", result.String(), minInt256.String())
	}
}

// TestReturnDataCopy_OutOfBounds verifies that RETURNDATACOPY reverts
// when the copy range exceeds the return data buffer.
func TestReturnDataCopy_OutOfBounds(t *testing.T) {
	evm, contract, mem, st := setupTest()
	// Set some return data (5 bytes).
	evm.returnData = []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	mem.Resize(64)
	pc := uint64(0)

	// Try to copy 10 bytes starting at offset 0 (only 5 available).
	st.Push(big.NewInt(10)) // length
	st.Push(big.NewInt(0))  // dataOffset
	st.Push(big.NewInt(0))  // memOffset
	_, err := opReturndataCopy(&pc, evm, contract, mem, st)
	if err != ErrReturnDataOutOfBounds {
		t.Errorf("RETURNDATACOPY OOB: got err=%v, want ErrReturnDataOutOfBounds", err)
	}

	// Also test: offset past end of data.
	st.Push(big.NewInt(1))  // length
	st.Push(big.NewInt(10)) // dataOffset (past end)
	st.Push(big.NewInt(0))  // memOffset
	_, err = opReturndataCopy(&pc, evm, contract, mem, st)
	if err != ErrReturnDataOutOfBounds {
		t.Errorf("RETURNDATACOPY past-end: got err=%v, want ErrReturnDataOutOfBounds", err)
	}

	// Valid case: copy exactly 5 bytes from offset 0.
	st.Push(big.NewInt(5)) // length
	st.Push(big.NewInt(0)) // dataOffset
	st.Push(big.NewInt(0)) // memOffset
	_, err = opReturndataCopy(&pc, evm, contract, mem, st)
	if err != nil {
		t.Errorf("RETURNDATACOPY valid: unexpected err=%v", err)
	}
}

// TestSelfDestruct_PostLondon verifies that SELFDESTRUCT transfers balance
// but does NOT delete the account (post-EIP-6780 behavior).
func TestSelfDestruct_PostLondon(t *testing.T) {
	mock := &selfDestructMockState{
		balances: map[types.Address]*big.Int{
			{0x01}: big.NewInt(1000),
		},
		destructed: make(map[types.Address]bool),
	}
	addr := types.Address{0x01}
	beneficiary := types.Address{0x02}

	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	evm.StateDB = mock
	contract := NewContract(types.Address{}, addr, big.NewInt(0), 1000000)
	mem := NewMemory()
	st := NewStack()
	pc := uint64(0)

	// Push beneficiary address onto stack.
	st.Push(new(big.Int).SetBytes(beneficiary[:]))
	_, err := opSelfdestruct(&pc, evm, contract, mem, st)
	if err != nil {
		t.Fatalf("SELFDESTRUCT error: %v", err)
	}

	// Beneficiary should have received the balance.
	benBal := mock.GetBalance(beneficiary)
	if benBal.Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("beneficiary balance = %s, want 1000", benBal.String())
	}

	// Contract balance should be zero.
	contractBal := mock.GetBalance(addr)
	if contractBal.Sign() != 0 {
		t.Errorf("contract balance = %s, want 0", contractBal.String())
	}

	// Post-EIP-6780: SelfDestruct should NOT have been called on the state.
	if mock.destructed[addr] {
		t.Error("post-EIP-6780: SELFDESTRUCT should NOT delete the account")
	}
}

// selfDestructMockState is a mock StateDB that tracks SelfDestruct calls.
type selfDestructMockState struct {
	balances   map[types.Address]*big.Int
	destructed map[types.Address]bool
}

func (m *selfDestructMockState) CreateAccount(types.Address)            {}
func (m *selfDestructMockState) GetBalance(addr types.Address) *big.Int {
	if b, ok := m.balances[addr]; ok {
		return new(big.Int).Set(b)
	}
	return new(big.Int)
}
func (m *selfDestructMockState) AddBalance(addr types.Address, amount *big.Int) {
	if _, ok := m.balances[addr]; !ok {
		m.balances[addr] = new(big.Int)
	}
	m.balances[addr].Add(m.balances[addr], amount)
}
func (m *selfDestructMockState) SubBalance(addr types.Address, amount *big.Int) {
	if _, ok := m.balances[addr]; !ok {
		m.balances[addr] = new(big.Int)
	}
	m.balances[addr].Sub(m.balances[addr], amount)
}
func (m *selfDestructMockState) GetNonce(types.Address) uint64                      { return 0 }
func (m *selfDestructMockState) SetNonce(types.Address, uint64)                     {}
func (m *selfDestructMockState) GetCode(types.Address) []byte                       { return nil }
func (m *selfDestructMockState) SetCode(types.Address, []byte)                      {}
func (m *selfDestructMockState) GetCodeHash(types.Address) types.Hash                          { return types.Hash{} }
func (m *selfDestructMockState) GetCodeSize(types.Address) int                                 { return 0 }
func (m *selfDestructMockState) GetState(types.Address, types.Hash) types.Hash                 { return types.Hash{} }
func (m *selfDestructMockState) SetState(types.Address, types.Hash, types.Hash)                {}
func (m *selfDestructMockState) GetCommittedState(types.Address, types.Hash) types.Hash        { return types.Hash{} }
func (m *selfDestructMockState) GetTransientState(types.Address, types.Hash) types.Hash        { return types.Hash{} }
func (m *selfDestructMockState) SetTransientState(types.Address, types.Hash, types.Hash)       {}
func (m *selfDestructMockState) SelfDestruct(addr types.Address)                               { m.destructed[addr] = true }
func (m *selfDestructMockState) HasSelfDestructed(addr types.Address) bool                     { return m.destructed[addr] }
func (m *selfDestructMockState) Exist(types.Address) bool                                      { return true }
func (m *selfDestructMockState) Empty(types.Address) bool                                      { return false }
func (m *selfDestructMockState) Snapshot() int                                                 { return 0 }
func (m *selfDestructMockState) RevertToSnapshot(int)                                          {}
func (m *selfDestructMockState) AddLog(*types.Log)                                             {}
func (m *selfDestructMockState) AddRefund(uint64)                                              {}
func (m *selfDestructMockState) SubRefund(uint64)                                              {}
func (m *selfDestructMockState) GetRefund() uint64                                             { return 0 }
func (m *selfDestructMockState) AddAddressToAccessList(types.Address)                          {}
func (m *selfDestructMockState) AddSlotToAccessList(types.Address, types.Hash)                 {}
func (m *selfDestructMockState) AddressInAccessList(types.Address) bool                        { return true }
func (m *selfDestructMockState) SlotInAccessList(types.Address, types.Hash) (bool, bool)       { return true, true }

// TestCreate2_AddressCalculation verifies that CREATE2 produces the correct
// deterministic address: keccak256(0xff ++ caller ++ salt ++ keccak256(initCode))[12:].
func TestCreate2_AddressCalculation(t *testing.T) {
	// Test vector from EIP-1014.
	// Address: 0x0000000000000000000000000000000000000000
	// Salt: 0x0000000000000000000000000000000000000000000000000000000000000000
	// InitCode: 0x00 (a single zero byte)
	// Expected: keccak256(0xff ++ address ++ salt ++ keccak256(0x00))[12:]

	caller := types.Address{}
	salt := new(big.Int)
	initCode := []byte{0x00}
	initCodeHash := crypto.Keccak256(initCode)

	addr := create2Address(caller, salt, initCodeHash)

	// Compute expected manually.
	data := make([]byte, 0, 85)
	data = append(data, 0xff)
	data = append(data, caller[:]...)
	saltBytes := make([]byte, 32)
	data = append(data, saltBytes...)
	data = append(data, initCodeHash...)
	hash := crypto.Keccak256(data)
	expectedAddr := types.BytesToAddress(hash[12:])

	if addr != expectedAddr {
		t.Errorf("CREATE2 address = %x, want %x", addr, expectedAddr)
	}

	// Test with non-zero salt.
	salt2 := new(big.Int).SetUint64(0xDEADBEEF)
	initCode2 := []byte{0x60, 0x00, 0x60, 0x00, 0xf3} // PUSH1 0, PUSH1 0, RETURN
	initCodeHash2 := crypto.Keccak256(initCode2)

	addr2 := create2Address(caller, salt2, initCodeHash2)

	data2 := make([]byte, 0, 85)
	data2 = append(data2, 0xff)
	data2 = append(data2, caller[:]...)
	saltBytes2 := make([]byte, 32)
	sb := salt2.Bytes()
	copy(saltBytes2[32-len(sb):], sb)
	data2 = append(data2, saltBytes2...)
	data2 = append(data2, initCodeHash2...)
	hash2 := crypto.Keccak256(data2)
	expectedAddr2 := types.BytesToAddress(hash2[12:])

	if addr2 != expectedAddr2 {
		t.Errorf("CREATE2 address (non-zero salt) = %x, want %x", addr2, expectedAddr2)
	}

	// Test determinism: same inputs produce same address.
	addr3 := create2Address(caller, salt2, initCodeHash2)
	if addr2 != addr3 {
		t.Error("CREATE2 should be deterministic: same inputs produced different addresses")
	}

	// Test different salt produces different address.
	salt3 := new(big.Int).SetUint64(0xCAFEBABE)
	addr4 := create2Address(caller, salt3, initCodeHash2)
	if addr2 == addr4 {
		t.Error("CREATE2 with different salt should produce different address")
	}
}
