package vm

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// --- CALL Gas Tests ---

func TestCallGas_63_64Rule(t *testing.T) {
	tests := []struct {
		name      string
		available uint64
		requested uint64
		want      uint64
	}{
		{
			name:      "requested exceeds cap",
			available: 6400,
			requested: 10000,
			want:      6300, // 6400 - 6400/64 = 6300
		},
		{
			name:      "requested below cap",
			available: 6400,
			requested: 5000,
			want:      5000,
		},
		{
			name:      "all gas available, high request",
			available: 100000,
			requested: 200000,
			want:      98438, // 100000 - 100000/64 = 100000 - 1562 = 98438
		},
		{
			name:      "zero requested",
			available: 6400,
			requested: 0,
			want:      0,
		},
		{
			name:      "zero available",
			available: 0,
			requested: 1000,
			want:      0,
		},
		{
			name:      "requested equals max",
			available: 6400,
			requested: 6300,
			want:      6300, // exactly the cap
		},
		{
			name:      "requested just above max",
			available: 6400,
			requested: 6301,
			want:      6300, // capped
		},
		{
			name:      "small gas pool",
			available: 64,
			requested: 1000,
			want:      63, // 64 - 64/64 = 63
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CallGas(tt.available, tt.requested)
			if got != tt.want {
				t.Errorf("CallGas(%d, %d) = %d, want %d", tt.available, tt.requested, got, tt.want)
			}
		})
	}
}

func TestCallGas_ValueTransfer(t *testing.T) {
	evm, db := newEIP2929TestEVM()
	contract := &Contract{}
	mem := NewMemory()

	addr := types.BytesToAddress([]byte{0xAA})
	addrInt := new(big.Int).SetBytes(addr[:])
	db.exists[addr] = true

	// CALL with value=0: no value transfer gas.
	stack := testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(0), new(big.Int).Set(addrInt), big.NewInt(1000),
	)
	gasNoValue, _ := gasCallEIP2929(evm, contract, stack, mem, 0)

	// Reset for fresh cold check.
	evm2, db2 := newEIP2929TestEVM()
	db2.exists[addr] = true

	// CALL with value=1: adds CallValueTransferGas (9000).
	stack = testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(1), new(big.Int).Set(addrInt), big.NewInt(1000),
	)
	gasWithValue, _ := gasCallEIP2929(evm2, &Contract{}, stack, mem, 0)

	diff := gasWithValue - gasNoValue
	if diff != CallValueTransferGas {
		t.Errorf("value transfer gas difference = %d, want %d (CallValueTransferGas)", diff, CallValueTransferGas)
	}
}

func TestCallGas_NewAccount(t *testing.T) {
	// CALL with value to non-existent account: adds CallNewAccountGas (25000).
	evm, _ := newEIP2929TestEVM()
	contract := &Contract{}
	mem := NewMemory()

	addr := types.BytesToAddress([]byte{0xBB})
	addrInt := new(big.Int).SetBytes(addr[:])
	// addr does NOT exist in state

	// CALL with value=1 to non-existent account.
	stack := testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(1), new(big.Int).Set(addrInt), big.NewInt(1000),
	)
	gas, _ := gasCallEIP2929(evm, contract, stack, mem, 0)

	coldPenalty := ColdAccountAccessCost - WarmStorageReadCost
	expected := coldPenalty + CallValueTransferGas + CallNewAccountGas
	if gas != expected {
		t.Errorf("CALL cold+value+new account gas = %d, want %d", gas, expected)
	}
}

func TestCallGas_NoNewAccountWithoutValue(t *testing.T) {
	// CALL without value to non-existent account: no CallNewAccountGas.
	evm, _ := newEIP2929TestEVM()
	contract := &Contract{}
	mem := NewMemory()

	addr := types.BytesToAddress([]byte{0xCC})
	addrInt := new(big.Int).SetBytes(addr[:])
	// addr does NOT exist

	// CALL with value=0 to non-existent account.
	stack := testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(0), new(big.Int).Set(addrInt), big.NewInt(1000),
	)
	gas, _ := gasCallEIP2929(evm, contract, stack, mem, 0)

	// Should only have cold penalty, no value transfer or new account.
	coldPenalty := ColdAccountAccessCost - WarmStorageReadCost
	if gas != coldPenalty {
		t.Errorf("CALL cold, no value, non-existent gas = %d, want %d", gas, coldPenalty)
	}
}

func TestCallGas_Stipend(t *testing.T) {
	// Verify stipend behavior: when CALL transfers value, 2300 gas is added
	// for free to the subcall, and returned gas is adjusted.

	// The stipend is handled in opCall, not in the gas function.
	// We test via the CallGas + stipend logic directly.
	available := uint64(100000)
	requested := uint64(50000)

	callGas := CallGas(available, requested)
	if callGas != requested {
		t.Fatalf("expected requested gas %d, got %d", requested, callGas)
	}

	// Simulate value transfer: stipend is added.
	callGasWithStipend := callGas + CallStipend
	if callGasWithStipend != requested+CallStipend {
		t.Errorf("call gas with stipend = %d, want %d", callGasWithStipend, requested+CallStipend)
	}

	// Simulate subcall returning all gas: return gas minus stipend.
	returnGas := callGasWithStipend // subcall used nothing
	returnGas -= CallStipend        // subtract stipend before crediting caller
	if returnGas != requested {
		t.Errorf("return gas after stipend removal = %d, want %d", returnGas, requested)
	}
}

// --- DELEGATECALL Gas Tests ---

func TestDelegateCallGas_NoValueTransfer(t *testing.T) {
	// DELEGATECALL never transfers value, so no CallValueTransferGas.
	evm, _ := newEIP2929TestEVM()
	contract := &Contract{}
	mem := NewMemory()

	addr := types.BytesToAddress([]byte{0xDD})
	addrInt := new(big.Int).SetBytes(addr[:])

	// Stack: gas, addr, argsOff, argsLen, retOff, retLen
	stack := testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		new(big.Int).Set(addrInt), big.NewInt(1000),
	)
	gas, _ := gasDelegateCallEIP2929(evm, contract, stack, mem, 0)

	// Only cold penalty, no value transfer.
	coldPenalty := ColdAccountAccessCost - WarmStorageReadCost
	if gas != coldPenalty {
		t.Errorf("DELEGATECALL cold gas = %d, want %d", gas, coldPenalty)
	}
}

// --- STATICCALL Gas Tests ---

func TestStaticCallGas_NoValueTransfer(t *testing.T) {
	// STATICCALL never transfers value.
	evm, _ := newEIP2929TestEVM()
	contract := &Contract{}
	mem := NewMemory()

	addr := types.BytesToAddress([]byte{0xEE})
	addrInt := new(big.Int).SetBytes(addr[:])

	// Stack: gas, addr, argsOff, argsLen, retOff, retLen
	stack := testStack(
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		new(big.Int).Set(addrInt), big.NewInt(1000),
	)
	gas, _ := gasStaticCallEIP2929(evm, contract, stack, mem, 0)

	coldPenalty := ColdAccountAccessCost - WarmStorageReadCost
	if gas != coldPenalty {
		t.Errorf("STATICCALL cold gas = %d, want %d", gas, coldPenalty)
	}
}

// --- CREATE Gas Tests ---

func TestCreateGas_BaseAndInitcode(t *testing.T) {
	tests := []struct {
		name         string
		initCodeSize uint64
		wantWordGas  uint64
	}{
		{"empty init code", 0, 0},
		{"32 bytes (1 word)", 32, 1 * InitCodeWordGas},
		{"33 bytes (2 words)", 33, 2 * InitCodeWordGas},
		{"64 bytes (2 words)", 64, 2 * InitCodeWordGas},
		{"100 bytes (4 words)", 100, 4 * InitCodeWordGas},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evm := &EVM{}
			contract := &Contract{}
			mem := NewMemory()

			// CREATE stack: value, offset, length (length at Back(2) for dynamic gas)
			stack := testStack(new(big.Int).SetUint64(tt.initCodeSize), big.NewInt(0), big.NewInt(0))
			gas, _ := gasCreateDynamic(evm, contract, stack, mem, 0)
			// gasCreateDynamic returns initcode word gas + memory expansion gas.
			// With memorySize=0 from the call, no memory expansion.
			if gas < tt.wantWordGas {
				t.Errorf("gasCreateDynamic(%d bytes) = %d, want >= %d", tt.initCodeSize, gas, tt.wantWordGas)
			}
		})
	}
}

func TestCreate2Gas_WithHashCost(t *testing.T) {
	evm := &EVM{}
	contract := &Contract{}
	mem := NewMemory()

	// CREATE2 with 64 bytes: 2 words * (InitCodeWordGas + Keccak256WordGas) = 2 * (2+6) = 16.
	// Stack: value=0, offset=0, length=64, salt=0
	stack := testStack(big.NewInt(0), big.NewInt(64), big.NewInt(0), big.NewInt(0))
	gas, _ := gasCreate2Dynamic(evm, contract, stack, mem, 64)
	memGas, _ := gasMemExpansion(evm, contract, stack, mem, 64)
	expected := uint64(2*(InitCodeWordGas+GasKeccak256Word)) + memGas
	if gas != expected {
		t.Errorf("gasCreate2Dynamic(64) = %d, want %d", gas, expected)
	}

	// CREATE2 with 0 bytes: no word gas or hash gas.
	stack = testStack(big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0))
	gas, _ = gasCreate2Dynamic(evm, contract, stack, mem, 0)
	if gas != 0 {
		t.Errorf("gasCreate2Dynamic(0) = %d, want 0", gas)
	}

	// CREATE2 with 33 bytes: 2 words * (2+6) = 16.
	mem2 := NewMemory()
	stack = testStack(big.NewInt(0), big.NewInt(33), big.NewInt(0), big.NewInt(0))
	gas, _ = gasCreate2Dynamic(evm, contract, stack, mem2, 33)
	memGas, _ = gasMemExpansion(evm, contract, stack, mem2, 33)
	expected = uint64(2*(InitCodeWordGas+GasKeccak256Word)) + memGas
	if gas != expected {
		t.Errorf("gasCreate2Dynamic(33) = %d, want %d", gas, expected)
	}
}

func TestCreateGas_MaxInitCodeSize(t *testing.T) {
	// EIP-3860: init code larger than MaxInitCodeSize should be rejected.
	evm := NewEVMWithState(
		BlockContext{BlockNumber: big.NewInt(1), GasLimit: 30000000},
		TxContext{},
		Config{},
		newAccessListStateDB(),
	)

	caller := types.BytesToAddress([]byte{0x01})
	bigCode := make([]byte, MaxInitCodeSize+1) // exceeds max

	_, _, gasLeft, err := evm.Create(caller, bigCode, 1000000, big.NewInt(0))
	if err != ErrMaxInitCodeSizeExceeded {
		t.Errorf("expected ErrMaxInitCodeSizeExceeded, got %v", err)
	}
	// Gas should be returned (not consumed) since it fails before deduction.
	if gasLeft != 1000000 {
		t.Errorf("gas left = %d, want 1000000 (all returned)", gasLeft)
	}
}

func TestCreateGas_ExactMaxInitCodeSize(t *testing.T) {
	// Exactly at MaxInitCodeSize should be accepted (not exceed).
	evm := NewEVMWithState(
		BlockContext{BlockNumber: big.NewInt(1), GasLimit: 30000000},
		TxContext{},
		Config{},
		newAccessListStateDB(),
	)

	caller := types.BytesToAddress([]byte{0x01})
	db := evm.StateDB.(*accessListStateDB)
	db.exists[caller] = true
	db.balances[caller] = big.NewInt(0)

	// Exactly MaxInitCodeSize: should not error with ErrMaxInitCodeSizeExceeded.
	code := make([]byte, MaxInitCodeSize)
	// The code is all zeros (STOP), so init code will succeed but return empty.
	_, _, _, err := evm.Create(caller, code, 10000000, big.NewInt(0))
	if err == ErrMaxInitCodeSizeExceeded {
		t.Error("MaxInitCodeSize exactly should not trigger ErrMaxInitCodeSizeExceeded")
	}
}

func TestCreateGas_CodeDepositCost(t *testing.T) {
	// Verify that code deposit costs 200 gas per byte of returned code.
	// CreateDataGas = 200.
	if CreateDataGas != 200 {
		t.Errorf("CreateDataGas = %d, want 200", CreateDataGas)
	}

	// The code deposit cost for 100 bytes would be 20000.
	depositCost := uint64(100) * CreateDataGas
	if depositCost != 20000 {
		t.Errorf("deposit cost for 100 bytes = %d, want 20000", depositCost)
	}
}

// --- SSTORE Gas Tests ---

func TestSstoreGas_AllCases(t *testing.T) {
	var zero, one, two [32]byte
	one[31] = 1
	two[31] = 2

	tests := []struct {
		name       string
		original   [32]byte
		current    [32]byte
		newVal     [32]byte
		cold       bool
		wantGas    uint64
		wantRefund int64
	}{
		// --- No-op cases ---
		{
			name:       "noop: 0 == 0 == 0",
			original:   zero,
			current:    zero,
			newVal:     zero,
			wantGas:    WarmStorageReadCost, // 100
			wantRefund: 0,
		},
		{
			name:       "noop: 1 == 1 == 1",
			original:   one,
			current:    one,
			newVal:     one,
			wantGas:    WarmStorageReadCost, // 100
			wantRefund: 0,
		},
		{
			name:       "noop cold: 1 == 1 == 1",
			original:   one,
			current:    one,
			newVal:     one,
			cold:       true,
			wantGas:    WarmStorageReadCost + ColdSloadCost, // 100 + 2100 = 2200
			wantRefund: 0,
		},

		// --- Clean slot: original == current ---
		{
			name:       "create: 0 -> 1",
			original:   zero,
			current:    zero,
			newVal:     one,
			wantGas:    GasSstoreSet, // 20000
			wantRefund: 0,
		},
		{
			name:       "create cold: 0 -> 1",
			original:   zero,
			current:    zero,
			newVal:     one,
			cold:       true,
			wantGas:    GasSstoreSet + ColdSloadCost, // 20000 + 2100 = 22100
			wantRefund: 0,
		},
		{
			name:       "update: 1 -> 2",
			original:   one,
			current:    one,
			newVal:     two,
			wantGas:    GasSstoreReset, // 2900
			wantRefund: 0,
		},
		{
			name:       "delete: 1 -> 0",
			original:   one,
			current:    one,
			newVal:     zero,
			wantGas:    GasSstoreReset,                    // 2900
			wantRefund: int64(SstoreClearsScheduleRefund), // 4800
		},
		{
			name:       "delete cold: 1 -> 0",
			original:   one,
			current:    one,
			newVal:     zero,
			cold:       true,
			wantGas:    GasSstoreReset + ColdSloadCost,    // 2900 + 2100 = 5000
			wantRefund: int64(SstoreClearsScheduleRefund), // 4800
		},

		// --- Dirty slot: original != current ---
		{
			name:       "dirty noop: orig=0, cur=1, new=1",
			original:   zero,
			current:    one,
			newVal:     one,
			wantGas:    WarmStorageReadCost, // 100
			wantRefund: 0,
		},
		{
			name:       "dirty update: orig=1, cur=2, new=0 (clear dirty)",
			original:   one,
			current:    two,
			newVal:     zero,
			wantGas:    WarmStorageReadCost,               // 100
			wantRefund: int64(SstoreClearsScheduleRefund), // 4800
		},
		{
			name:       "dirty undo clear: orig=1, cur=0, new=2 (undo previous clear)",
			original:   one,
			current:    zero,
			newVal:     two,
			wantGas:    WarmStorageReadCost,                // 100
			wantRefund: -int64(SstoreClearsScheduleRefund), // -4800
		},
		{
			name:       "dirty restore zero: orig=0, cur=1, new=0 (restore to original 0)",
			original:   zero,
			current:    one,
			newVal:     zero,
			wantGas:    WarmStorageReadCost,                              // 100
			wantRefund: int64(GasSstoreSet) - int64(WarmStorageReadCost), // 20000 - 100 = 19900
		},
		{
			name:       "dirty restore non-zero: orig=1, cur=2, new=1 (restore to original 1)",
			original:   one,
			current:    two,
			newVal:     one,
			wantGas:    WarmStorageReadCost,                                // 100
			wantRefund: int64(GasSstoreReset) - int64(WarmStorageReadCost), // 2900 - 100 = 2800
		},
		{
			name:     "dirty restore from zero: orig=1, cur=0, new=1 (undo clear + restore)",
			original: one,
			current:  zero,
			newVal:   one,
			wantGas:  WarmStorageReadCost, // 100
			// Undo clear: -4800, restore: +2800 = -2000
			wantRefund: -int64(SstoreClearsScheduleRefund) + int64(GasSstoreReset) - int64(WarmStorageReadCost),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gas, refund := SstoreGas(tt.original, tt.current, tt.newVal, tt.cold)
			if gas != tt.wantGas {
				t.Errorf("gas = %d, want %d", gas, tt.wantGas)
			}
			if refund != tt.wantRefund {
				t.Errorf("refund = %d, want %d", refund, tt.wantRefund)
			}
		})
	}
}

func TestSstoreRefund_MaxCap(t *testing.T) {
	// EIP-3529: max refund is gasUsed / 5.
	// Verify the constant is correct.
	if MaxRefundQuotient != 5 {
		t.Errorf("MaxRefundQuotient = %d, want 5", MaxRefundQuotient)
	}

	// Verify the cap calculation for various gasUsed values.
	tests := []struct {
		gasUsed   uint64
		refund    uint64
		maxRefund uint64
		capped    uint64
	}{
		{100000, 50000, 20000, 20000}, // 100000/5 = 20000, refund capped
		{100000, 10000, 20000, 10000}, // refund below cap
		{100000, 20000, 20000, 20000}, // refund exactly at cap
		{50000, 25000, 10000, 10000},  // 50000/5 = 10000
		{0, 0, 0, 0},                  // zero gas used
	}

	for _, tt := range tests {
		maxRefund := tt.gasUsed / MaxRefundQuotient
		if maxRefund != tt.maxRefund {
			t.Errorf("gasUsed=%d: max refund = %d, want %d", tt.gasUsed, maxRefund, tt.maxRefund)
		}
		capped := tt.refund
		if capped > maxRefund {
			capped = maxRefund
		}
		if capped != tt.capped {
			t.Errorf("gasUsed=%d, refund=%d: capped = %d, want %d",
				tt.gasUsed, tt.refund, capped, tt.capped)
		}
	}
}

func TestSstoreRefund_ClearsScheduleConstant(t *testing.T) {
	// Per EIP-3529: SSTORE_CLEARS_SCHEDULE = SSTORE_RESET_GAS + ACCESS_LIST_STORAGE_KEY_COST
	// = 2900 + 1900 = 4800
	if SstoreClearsScheduleRefund != 4800 {
		t.Errorf("SstoreClearsScheduleRefund = %d, want 4800", SstoreClearsScheduleRefund)
	}
}

// --- SELFDESTRUCT Gas Tests ---

func TestSelfdestructGas_PostCancun(t *testing.T) {
	// Post-EIP-6780 (Cancun): SELFDESTRUCT has base gas 5000.
	// Dynamic gas includes cold access and new account creation.

	tbl := NewCancunJumpTable()
	sdOp := tbl[SELFDESTRUCT]
	if sdOp == nil {
		t.Fatal("SELFDESTRUCT operation is nil in Cancun table")
	}

	// Base gas is 5000.
	if sdOp.constantGas != GasSelfdestruct {
		t.Errorf("SELFDESTRUCT constantGas = %d, want %d", sdOp.constantGas, GasSelfdestruct)
	}
	if sdOp.dynamicGas == nil {
		t.Fatal("SELFDESTRUCT dynamicGas is nil in Cancun table")
	}

	// Test cold beneficiary with no balance: only cold penalty.
	evm, db := newEIP2929TestEVM()
	contract := &Contract{
		Address: types.BytesToAddress([]byte{0x42}),
	}
	mem := NewMemory()
	db.exists[contract.Address] = true
	// Contract has zero balance.

	beneficiary := types.BytesToAddress([]byte{0x99})
	beneficiaryInt := new(big.Int).SetBytes(beneficiary[:])
	db.exists[beneficiary] = true

	stack := testStack(new(big.Int).Set(beneficiaryInt))
	gas, _ := gasSelfdestructEIP2929(evm, contract, stack, mem, 0)
	expectedCold := ColdAccountAccessCost - WarmStorageReadCost
	if gas != expectedCold {
		t.Errorf("SELFDESTRUCT cold beneficiary, no balance: gas = %d, want %d", gas, expectedCold)
	}

	// Test cold beneficiary that doesn't exist + contract has balance:
	// cold penalty + CreateBySelfdestructGas.
	evm2, db2 := newEIP2929TestEVM()
	contract2 := &Contract{
		Address: types.BytesToAddress([]byte{0x43}),
	}
	db2.exists[contract2.Address] = true
	db2.balances[contract2.Address] = big.NewInt(1000)

	newBeneficiary := types.BytesToAddress([]byte{0xAB})
	newBeneficiaryInt := new(big.Int).SetBytes(newBeneficiary[:])
	// beneficiary does NOT exist

	stack = testStack(new(big.Int).Set(newBeneficiaryInt))
	gas, _ = gasSelfdestructEIP2929(evm2, contract2, stack, mem, 0)
	expected := (ColdAccountAccessCost - WarmStorageReadCost) + CreateBySelfdestructGas
	if gas != expected {
		t.Errorf("SELFDESTRUCT cold+new account: gas = %d, want %d", gas, expected)
	}

	// Test warm beneficiary that exists with contract balance: no extra gas.
	evm3, db3 := newEIP2929TestEVM()
	contract3 := &Contract{
		Address: types.BytesToAddress([]byte{0x44}),
	}
	db3.exists[contract3.Address] = true
	db3.balances[contract3.Address] = big.NewInt(1000)

	warmBeneficiary := types.BytesToAddress([]byte{0xCD})
	warmBeneficiaryInt := new(big.Int).SetBytes(warmBeneficiary[:])
	db3.exists[warmBeneficiary] = true
	db3.AddAddressToAccessList(warmBeneficiary) // pre-warm

	stack = testStack(new(big.Int).Set(warmBeneficiaryInt))
	gas, _ = gasSelfdestructEIP2929(evm3, contract3, stack, mem, 0)
	if gas != 0 {
		t.Errorf("SELFDESTRUCT warm beneficiary, exists: gas = %d, want 0", gas)
	}
}

func TestSelfdestructGas_Constants(t *testing.T) {
	// Verify SELFDESTRUCT-related constants.
	if GasSelfdestruct != 5000 {
		t.Errorf("GasSelfdestruct = %d, want 5000", GasSelfdestruct)
	}
	if SelfdestructGas != 5000 {
		t.Errorf("SelfdestructGas = %d, want 5000", SelfdestructGas)
	}
	if CreateBySelfdestructGas != 25000 {
		t.Errorf("CreateBySelfdestructGas = %d, want 25000", CreateBySelfdestructGas)
	}
}

// --- Gas Constants Verification ---

func TestGasConstants_CallFamily(t *testing.T) {
	if CallValueTransferGas != 9000 {
		t.Errorf("CallValueTransferGas = %d, want 9000", CallValueTransferGas)
	}
	if CallNewAccountGas != 25000 {
		t.Errorf("CallNewAccountGas = %d, want 25000", CallNewAccountGas)
	}
	if CallStipend != 2300 {
		t.Errorf("CallStipend = %d, want 2300", CallStipend)
	}
	if CallGasFraction != 64 {
		t.Errorf("CallGasFraction = %d, want 64", CallGasFraction)
	}
}

func TestGasConstants_Create(t *testing.T) {
	if GasCreate != 32000 {
		t.Errorf("GasCreate = %d, want 32000", GasCreate)
	}
	if InitCodeWordGas != 2 {
		t.Errorf("InitCodeWordGas = %d, want 2", InitCodeWordGas)
	}
	if MaxInitCodeSize != 49152 {
		t.Errorf("MaxInitCodeSize = %d, want 49152", MaxInitCodeSize)
	}
	if MaxCodeSize != 24576 {
		t.Errorf("MaxCodeSize = %d, want 24576", MaxCodeSize)
	}
	if CreateDataGas != 200 {
		t.Errorf("CreateDataGas = %d, want 200", CreateDataGas)
	}
}

// --- CALL-family opcode integration via gas function wiring ---

func TestCallFamilyGasWiring_BerlinTable(t *testing.T) {
	tbl := NewBerlinJumpTable()

	// Verify all CALL-family opcodes use WarmStorageReadCost as constant gas
	// and have proper dynamic gas functions.
	checks := []struct {
		op   OpCode
		name string
	}{
		{CALL, "CALL"},
		{CALLCODE, "CALLCODE"},
		{DELEGATECALL, "DELEGATECALL"},
		{STATICCALL, "STATICCALL"},
	}

	for _, c := range checks {
		op := tbl[c.op]
		if op == nil {
			t.Errorf("%s: nil in Berlin table", c.name)
			continue
		}
		if op.constantGas != WarmStorageReadCost {
			t.Errorf("%s: constantGas = %d, want %d", c.name, op.constantGas, WarmStorageReadCost)
		}
		if op.dynamicGas == nil {
			t.Errorf("%s: dynamicGas is nil", c.name)
		}
		if op.memorySize == nil {
			t.Errorf("%s: memorySize is nil", c.name)
		}
	}
}

func TestCreateGasWiring_BerlinTable(t *testing.T) {
	tbl := NewBerlinJumpTable()

	// CREATE has constantGas = GasCreate (32000), dynamic gas for init code word gas.
	createOp := tbl[CREATE]
	if createOp == nil {
		t.Fatal("CREATE: nil in Berlin table")
	}
	if createOp.constantGas != GasCreate {
		t.Errorf("CREATE: constantGas = %d, want %d", createOp.constantGas, GasCreate)
	}
	if createOp.dynamicGas == nil {
		t.Error("CREATE: dynamicGas is nil")
	}

	// CREATE2 has constantGas = GasCreate (32000), dynamic gas for init code + hash.
	create2Op := tbl[CREATE2]
	if create2Op == nil {
		t.Fatal("CREATE2: nil in Berlin table")
	}
	if create2Op.constantGas != GasCreate {
		t.Errorf("CREATE2: constantGas = %d, want %d", create2Op.constantGas, GasCreate)
	}
	if create2Op.dynamicGas == nil {
		t.Error("CREATE2: dynamicGas is nil")
	}
}

// --- 63/64 Rule Comprehensive Tests ---

func TestCallGas_63_64Rule_Comprehensive(t *testing.T) {
	// Verify the 63/64 rule: gas_available_for_subcall = min(requested, available - available/64).
	tests := []struct {
		available uint64
		requested uint64
		want      uint64
	}{
		// Large gas pools.
		{1000000, 2000000, 984375}, // 1000000 - 1000000/64 = 984375
		{1000000, 984375, 984375},  // exactly at cap
		{1000000, 984374, 984374},  // just under cap

		// Edge cases (integer division: x/64 truncates).
		{1, 1000, 1},     // 1 - 1/64 = 1 - 0 = 1
		{63, 1000, 63},   // 63 - 63/64 = 63 - 0 = 63
		{64, 1000, 63},   // 64 - 64/64 = 64 - 1 = 63
		{128, 1000, 126}, // 128 - 128/64 = 128 - 2 = 126
	}

	for _, tt := range tests {
		got := CallGas(tt.available, tt.requested)
		if got != tt.want {
			t.Errorf("CallGas(%d, %d) = %d, want %d", tt.available, tt.requested, got, tt.want)
		}
	}
}

// --- Memory expansion for CALL-family ---

func TestCallGas_MemoryExpansion(t *testing.T) {
	evm, db := newEIP2929TestEVM()
	contract := &Contract{}
	mem := NewMemory()

	addr := types.BytesToAddress([]byte{0x77})
	addrInt := new(big.Int).SetBytes(addr[:])
	db.exists[addr] = true
	db.AddAddressToAccessList(addr) // pre-warm

	// CALL with input spanning [0, 64) and return spanning [0, 128).
	// Stack: gas, addr, value, argsOff, argsLen, retOff, retLen
	// Memory needed = max(0+64, 0+128) = 128
	stack := testStack(
		big.NewInt(128), big.NewInt(0), big.NewInt(64), big.NewInt(0),
		big.NewInt(0), new(big.Int).Set(addrInt), big.NewInt(1000),
	)
	// Compute expected memory expansion gas for 128 bytes.
	memGas, _ := gasMemExpansion(evm, contract, stack, mem, 128)

	gas, _ := gasCallEIP2929(evm, contract, stack, mem, 128)
	// Warm address (no cold penalty), no value. Should include memory gas.
	if gas != memGas {
		t.Errorf("CALL warm with memory: gas = %d, want %d (memGas)", gas, memGas)
	}
}
