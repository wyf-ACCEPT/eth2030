package vm

import (
	"errors"
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// benchStateDB extends mockStateDB with warm access list tracking for
// realistic gas behavior in benchmarks.
type benchStateDB struct {
	mockStateDB
	warmAddrs map[types.Address]bool
	warmSlots map[types.Address]map[types.Hash]bool
	balances  map[types.Address]*big.Int
	codes     map[types.Address][]byte
	nonces    map[types.Address]uint64
}

func newBenchStateDB() *benchStateDB {
	return &benchStateDB{
		mockStateDB: *newMockStateDB(),
		warmAddrs:   make(map[types.Address]bool),
		warmSlots:   make(map[types.Address]map[types.Hash]bool),
		balances:    make(map[types.Address]*big.Int),
		codes:       make(map[types.Address][]byte),
		nonces:      make(map[types.Address]uint64),
	}
}

func (b *benchStateDB) GetBalance(addr types.Address) *big.Int {
	if bal, ok := b.balances[addr]; ok {
		return new(big.Int).Set(bal)
	}
	return new(big.Int)
}

func (b *benchStateDB) AddBalance(addr types.Address, amount *big.Int) {
	if _, ok := b.balances[addr]; !ok {
		b.balances[addr] = new(big.Int)
	}
	b.balances[addr].Add(b.balances[addr], amount)
}

func (b *benchStateDB) SubBalance(addr types.Address, amount *big.Int) {
	if _, ok := b.balances[addr]; !ok {
		b.balances[addr] = new(big.Int)
	}
	b.balances[addr].Sub(b.balances[addr], amount)
}

func (b *benchStateDB) GetNonce(addr types.Address) uint64 { return b.nonces[addr] }
func (b *benchStateDB) SetNonce(addr types.Address, n uint64) { b.nonces[addr] = n }

func (b *benchStateDB) GetCode(addr types.Address) []byte    { return b.codes[addr] }
func (b *benchStateDB) SetCode(addr types.Address, c []byte) { b.codes[addr] = c }

func (b *benchStateDB) Exist(addr types.Address) bool  { return b.warmAddrs[addr] }
func (b *benchStateDB) Empty(addr types.Address) bool  { return !b.warmAddrs[addr] }

func (b *benchStateDB) CreateAccount(addr types.Address) {
	b.warmAddrs[addr] = true
	if _, ok := b.balances[addr]; !ok {
		b.balances[addr] = new(big.Int)
	}
}

func (b *benchStateDB) AddAddressToAccessList(addr types.Address) {
	b.warmAddrs[addr] = true
}

func (b *benchStateDB) AddSlotToAccessList(addr types.Address, slot types.Hash) {
	b.warmAddrs[addr] = true
	if _, ok := b.warmSlots[addr]; !ok {
		b.warmSlots[addr] = make(map[types.Hash]bool)
	}
	b.warmSlots[addr][slot] = true
}

func (b *benchStateDB) AddressInAccessList(addr types.Address) bool {
	return b.warmAddrs[addr]
}

func (b *benchStateDB) SlotInAccessList(addr types.Address, slot types.Hash) (bool, bool) {
	addrOk := b.warmAddrs[addr]
	if s, ok := b.warmSlots[addr]; ok {
		return addrOk, s[slot]
	}
	return addrOk, false
}

// newBenchEVM creates a minimal EVM suitable for benchmarks.
func newBenchEVM() *EVM {
	return NewEVM(
		BlockContext{
			BlockNumber: big.NewInt(1000),
			Time:        1700000000,
			GasLimit:    30_000_000,
			BaseFee:     big.NewInt(1_000_000_000),
			GetHash:     func(n uint64) types.Hash { return types.Hash{} },
		},
		TxContext{
			GasPrice: big.NewInt(2_000_000_000),
		},
		Config{},
	)
}

// newBenchEVMWithState creates a benchmark EVM with a state database.
func newBenchEVMWithState() (*EVM, *benchStateDB) {
	evm := newBenchEVM()
	state := newBenchStateDB()
	evm.StateDB = state
	return evm, state
}

// ---------- Benchmarks ----------

// BenchmarkEVM_ADD benchmarks the ADD opcode (two 256-bit integer addition).
func BenchmarkEVM_ADD(b *testing.B) {
	evm := newBenchEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 1<<62)
	mem := NewMemory()
	pc := uint64(0)

	x := new(big.Int).SetUint64(0xdeadbeef)
	y := new(big.Int).SetUint64(0xcafebabe)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		st := NewStack()
		st.Push(new(big.Int).Set(x))
		st.Push(new(big.Int).Set(y))
		opAdd(&pc, evm, contract, mem, st)
	}
}

// BenchmarkEVM_MUL benchmarks the MUL opcode (two 256-bit integer multiplication).
func BenchmarkEVM_MUL(b *testing.B) {
	evm := newBenchEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 1<<62)
	mem := NewMemory()
	pc := uint64(0)

	x := new(big.Int).SetUint64(0xdeadbeef)
	y := new(big.Int).SetUint64(0xcafebabe)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		st := NewStack()
		st.Push(new(big.Int).Set(x))
		st.Push(new(big.Int).Set(y))
		opMul(&pc, evm, contract, mem, st)
	}
}

// BenchmarkEVM_SHA3 benchmarks the KECCAK256 opcode on 32-byte input.
func BenchmarkEVM_SHA3(b *testing.B) {
	evm := newBenchEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 1<<62)
	pc := uint64(0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mem := NewMemory()
		mem.Resize(64)
		// Write 32 bytes of data at offset 0.
		mem.Set(0, 32, make([]byte, 32))
		st := NewStack()
		st.Push(big.NewInt(0))  // offset
		st.Push(big.NewInt(32)) // size
		opKeccak256(&pc, evm, contract, mem, st)
	}
}

// BenchmarkEVM_SSTORE benchmarks the SSTORE opcode (storage write).
func BenchmarkEVM_SSTORE(b *testing.B) {
	evm, state := newBenchEVMWithState()
	addr := types.Address{0x01}
	state.CreateAccount(addr)
	state.AddAddressToAccessList(addr)
	contract := NewContract(types.Address{}, addr, big.NewInt(0), 1<<62)
	mem := NewMemory()
	pc := uint64(0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		st := NewStack()
		key := new(big.Int).SetUint64(uint64(i))
		val := new(big.Int).SetUint64(uint64(i + 1))
		st.Push(val) // value (pushed first, popped second)
		st.Push(key) // key (pushed second, popped first)
		opSstore(&pc, evm, contract, mem, st)
	}
}

// BenchmarkEVM_SLOAD benchmarks the SLOAD opcode (storage read).
func BenchmarkEVM_SLOAD(b *testing.B) {
	evm, state := newBenchEVMWithState()
	addr := types.Address{0x01}
	state.CreateAccount(addr)

	// Pre-populate some storage.
	for i := 0; i < 100; i++ {
		key := types.BytesToHash(new(big.Int).SetUint64(uint64(i)).Bytes())
		val := types.BytesToHash(new(big.Int).SetUint64(uint64(i + 1)).Bytes())
		state.SetState(addr, key, val)
	}

	contract := NewContract(types.Address{}, addr, big.NewInt(0), 1<<62)
	mem := NewMemory()
	pc := uint64(0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		st := NewStack()
		st.Push(new(big.Int).SetUint64(uint64(i % 100)))
		opSload(&pc, evm, contract, mem, st)
	}
}

// BenchmarkEVM_CALL benchmarks the CALL opcode by calling an empty contract.
func BenchmarkEVM_CALL(b *testing.B) {
	evm, state := newBenchEVMWithState()
	caller := types.Address{0x01}
	callee := types.Address{0x02}

	state.CreateAccount(caller)
	state.AddBalance(caller, new(big.Int).SetUint64(1_000_000_000))
	state.CreateAccount(callee)
	// Set empty code so the call returns immediately.
	state.SetCode(callee, []byte{byte(STOP)})
	state.AddAddressToAccessList(callee)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		evm.Call(caller, callee, nil, 100_000, big.NewInt(0))
	}
}

// BenchmarkEVM_CREATE benchmarks contract creation with minimal init code.
func BenchmarkEVM_CREATE(b *testing.B) {
	evm, state := newBenchEVMWithState()
	creator := types.Address{0x01}
	state.CreateAccount(creator)
	state.AddBalance(creator, new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))

	// Init code: PUSH1 0, PUSH1 0, RETURN (deploys empty contract).
	initCode := []byte{
		byte(PUSH1), 0,
		byte(PUSH1), 0,
		byte(RETURN),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		evm.Create(creator, initCode, 1_000_000, big.NewInt(0))
	}
}

// BenchmarkEVM_MemoryExpansion benchmarks memory growth via MSTORE to
// progressively larger offsets.
func BenchmarkEVM_MemoryExpansion(b *testing.B) {
	evm := newBenchEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 1<<62)
	pc := uint64(0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mem := NewMemory()
		// Expand memory in steps: 32, 64, 128, 256, 512, 1024 bytes.
		for _, offset := range []uint64{0, 32, 96, 224, 480, 992} {
			mem.Resize(offset + 32)
			st := NewStack()
			st.Push(new(big.Int).SetUint64(0xdeadbeef))
			st.Push(new(big.Int).SetUint64(offset))
			opMstore(&pc, evm, contract, mem, st)
		}
	}
}

// BenchmarkEVM_StackOperations benchmarks a mix of PUSH, POP, DUP, and SWAP.
func BenchmarkEVM_StackOperations(b *testing.B) {
	evm := newBenchEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 1<<62)
	mem := NewMemory()
	pc := uint64(0)

	pushFn := makePush(1)
	dupFn := makeDup(1)
	swapFn := makeSwap(1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		st := NewStack()
		// Push two values for SWAP to work with.
		contract.Code = []byte{byte(PUSH1), 0x42, byte(PUSH1), 0x43}
		pc = 0
		pushFn(&pc, evm, contract, st.mem(mem), st)
		pc = 2
		pushFn(&pc, evm, contract, st.mem(mem), st)
		// DUP1
		dupFn(&pc, evm, contract, mem, st)
		// SWAP1
		swapFn(&pc, evm, contract, mem, st)
		// POP twice
		opPop(&pc, evm, contract, mem, st)
		opPop(&pc, evm, contract, mem, st)
		// POP last
		opPop(&pc, evm, contract, mem, st)
	}
}

// mem is a helper that lets Stack satisfy push API compatibility in benchmarks.
func (st *Stack) mem(m *Memory) *Memory { return m }

// BenchmarkEVM_JumpTable benchmarks jump table lookup for all valid opcodes.
func BenchmarkEVM_JumpTable(b *testing.B) {
	jt := NewCancunJumpTable()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Iterate through all 256 possible opcode slots.
		for op := 0; op < 256; op++ {
			entry := jt[OpCode(op)]
			if entry != nil {
				_ = entry.constantGas
			}
		}
	}
}

// ---------- Regular Tests ----------

// TestEVM_GasAccounting verifies gas is properly consumed for various operations.
func TestEVM_GasAccounting(t *testing.T) {
	tests := []struct {
		name    string
		code    []byte
		gasGive uint64
		wantErr error
	}{
		{
			name: "ADD consumes 3 gas",
			code: []byte{
				byte(PUSH1), 1,
				byte(PUSH1), 2,
				byte(ADD),
				byte(STOP),
			},
			gasGive: 100,
			wantErr: nil,
		},
		{
			name: "insufficient gas for ADD",
			code: []byte{
				byte(PUSH1), 1, // 3 gas
				byte(PUSH1), 2, // 3 gas
				byte(ADD),      // 3 gas = 9 total
				byte(STOP),
			},
			gasGive: 8, // not enough
			wantErr: ErrOutOfGas,
		},
		{
			name: "PUSH1+STOP minimal gas",
			code: []byte{
				byte(PUSH1), 0xff,
				byte(STOP),
			},
			// PUSH1 = 3 gas, STOP = 0 gas
			gasGive: 3,
			wantErr: nil,
		},
		{
			name: "PUSH1 insufficient gas",
			code: []byte{
				byte(PUSH1), 0xff,
				byte(STOP),
			},
			gasGive: 2,
			wantErr: ErrOutOfGas,
		},
		{
			name: "MUL consumes 5 gas under Frontier table",
			// In Cancun table, MUL is GasVerylow = 3.
			code: []byte{
				byte(PUSH1), 3,
				byte(PUSH1), 4,
				byte(MUL),
				byte(STOP),
			},
			gasGive: 9, // PUSH1(3) + PUSH1(3) + MUL(3) = 9
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evm := newBenchEVM()
			contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), tt.gasGive)
			contract.Code = tt.code

			_, err := evm.Run(contract, nil)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("got err=%v, want %v", err, tt.wantErr)
			}
		})
	}

	// Verify gas remaining after a sequence of operations.
	t.Run("gas remaining after operations", func(t *testing.T) {
		evm := newBenchEVM()
		// PUSH1(3) + PUSH1(3) + ADD(3) + STOP(0) = 9 gas used.
		code := []byte{
			byte(PUSH1), 10,
			byte(PUSH1), 20,
			byte(ADD),
			byte(STOP),
		}
		initialGas := uint64(100)
		contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), initialGas)
		contract.Code = code

		_, err := evm.Run(contract, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedRemaining := initialGas - (GasPush + GasPush + GasVerylow + GasStop)
		if contract.Gas != expectedRemaining {
			t.Errorf("gas remaining = %d, want %d", contract.Gas, expectedRemaining)
		}
	})
}

// TestEVM_StackOverflow verifies the EVM detects stack overflow.
func TestEVM_StackOverflow(t *testing.T) {
	evm := newBenchEVM()

	// Build code that pushes 1025 values (exceeding the 1024 limit).
	// The stack limit check uses maxStack from the operation definition.
	// PUSH1 has maxStack=1023, meaning it can only be executed when
	// the stack has <= 1023 items (since it adds one item, reaching 1024).
	// So pushing 1024 times succeeds, but the 1025th PUSH1 fails.
	var code []byte
	for i := 0; i < 1025; i++ {
		code = append(code, byte(PUSH1), 0x01)
	}
	code = append(code, byte(STOP))

	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 1<<62)
	contract.Code = code

	_, err := evm.Run(contract, nil)
	if !errors.Is(err, ErrStackOverflow) {
		t.Errorf("expected ErrStackOverflow, got %v", err)
	}
}

// TestEVM_InvalidOpcode verifies invalid opcodes are rejected.
func TestEVM_InvalidOpcode(t *testing.T) {
	tests := []struct {
		name string
		op   byte
	}{
		{"0xEF (not assigned)", 0xef},
		{"0xC0 (not assigned)", 0xc0},
		{"0xFE INVALID opcode", byte(INVALID)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evm := newBenchEVM()
			contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100_000)
			contract.Code = []byte{tt.op}

			_, err := evm.Run(contract, nil)
			if err == nil {
				t.Fatal("expected error for invalid/unassigned opcode, got nil")
			}
			if !errors.Is(err, ErrInvalidOpCode) {
				t.Errorf("expected ErrInvalidOpCode, got %v", err)
			}
		})
	}
}
