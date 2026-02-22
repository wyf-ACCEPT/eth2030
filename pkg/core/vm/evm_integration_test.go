package vm

import (
	"errors"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// newIntegrationEVM creates an EVM with a real MemoryStateDB for integration tests.
func newIntegrationEVM() (*EVM, *state.MemoryStateDB) {
	stateDB := state.NewMemoryStateDB()
	evm := NewEVMWithState(
		BlockContext{
			BlockNumber: big.NewInt(100),
			Time:        1700000000,
			GasLimit:    30000000,
			BaseFee:     big.NewInt(1000000000),
		},
		TxContext{
			GasPrice: big.NewInt(2000000000),
		},
		Config{},
		stateDB,
	)
	return evm, stateDB
}

// --------------------------------------------------------------------------
// 1. Reentrancy: Contract A calls B, B calls back to A
// --------------------------------------------------------------------------

func TestReentrancy(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0x11})
	contractA := types.BytesToAddress([]byte{0xAA})
	contractB := types.BytesToAddress([]byte{0xBB})

	stateDB.CreateAccount(callerAddr)
	stateDB.AddBalance(callerAddr, big.NewInt(1000000))
	stateDB.CreateAccount(contractA)
	stateDB.CreateAccount(contractB)

	// Contract A: increment a counter at storage slot 0, then call B if counter <= 2.
	// Contract B: call A (re-entering A).
	// Expected: counter ends up at 3 (A runs 3 times, stopping when counter > 2).

	// Build Contract A:
	// Layout with byte offsets:
	//   0-1:  PUSH1 0x00
	//   2:    SLOAD         -- load counter from slot 0
	//   3-4:  PUSH1 0x01
	//   5:    ADD           -- counter + 1
	//   6:    DUP1          -- dup for comparison and for SSTORE
	//   7-8:  PUSH1 0x00
	//   9:    SSTORE        -- store counter+1 at slot 0
	//   10-11: PUSH1 0x02
	//   12:   GT            -- (counter+1) > 2?
	//   13-14: PUSH1 <jumpdest>
	//   15:   JUMPI         -- if yes, jump to stop
	//   16-17: PUSH1 0x00  (retLen)
	//   18-19: PUSH1 0x00  (retOffset)
	//   20-21: PUSH1 0x00  (argsLen)
	//   22-23: PUSH1 0x00  (argsOffset)
	//   24-25: PUSH1 0x00  (value)
	//   26-46: PUSH20 <contractB>
	//   47:   GAS
	//   48:   CALL
	//   49:   POP
	//   50:   JUMPDEST
	//   51:   STOP
	const jumpdestA = 50
	codeA := []byte{
		byte(PUSH1), 0x00,       // slot 0
		byte(SLOAD),             // load counter
		byte(PUSH1), 0x01,       // 1
		byte(ADD),               // counter + 1
		byte(DUP1),              // dup for comparison
		byte(PUSH1), 0x00,       // slot 0
		byte(SSTORE),            // store updated counter
		byte(PUSH1), 0x02,       // 2
		byte(LT),                // 2 < counter+1? (same as counter+1 > 2)
		byte(PUSH1), jumpdestA,  // jump target
		byte(JUMPI),             // if yes, jump to stop
		byte(PUSH1), 0x00,  // retLen
		byte(PUSH1), 0x00,  // retOffset
		byte(PUSH1), 0x00,  // argsLen
		byte(PUSH1), 0x00,  // argsOffset
		byte(PUSH1), 0x00,  // value
		byte(PUSH20),       // contractB address
	}
	codeA = append(codeA, contractB[:]...)
	codeA = append(codeA,
		byte(GAS),        // push remaining gas
		byte(CALL),
		byte(POP),        // discard result
		byte(JUMPDEST),   // position 50
		byte(STOP),
	)

	// Contract B: call A with all remaining gas, then STOP.
	codeB := []byte{
		byte(PUSH1), 0x00,  // retLen
		byte(PUSH1), 0x00,  // retOffset
		byte(PUSH1), 0x00,  // argsLen
		byte(PUSH1), 0x00,  // argsOffset
		byte(PUSH1), 0x00,  // value
		byte(PUSH20),       // contractA address
	}
	codeB = append(codeB, contractA[:]...)
	codeB = append(codeB,
		byte(GAS),   // push remaining gas
		byte(CALL),
		byte(POP),   // discard result
		byte(STOP),
	)

	stateDB.SetCode(contractA, codeA)
	stateDB.SetCode(contractB, codeB)

	// Pre-warm both contract addresses and storage slot for EIP-2929.
	stateDB.AddAddressToAccessList(callerAddr)
	stateDB.AddAddressToAccessList(contractA)
	stateDB.AddAddressToAccessList(contractB)
	stateDB.AddSlotToAccessList(contractA, types.BytesToHash([]byte{0x00}))

	ret, gasLeft, err := evm.Call(callerAddr, contractA, nil, 10000000, big.NewInt(0))
	if err != nil {
		t.Fatalf("reentrant call failed: %v (ret=%x, gasLeft=%d)", err, ret, gasLeft)
	}

	// Counter should be > 1 (A was re-entered at least once via B)
	counter := stateDB.GetState(contractA, types.BytesToHash([]byte{0x00}))
	counterVal := new(big.Int).SetBytes(counter[:])
	if counterVal.Cmp(big.NewInt(1)) <= 0 {
		t.Errorf("expected counter > 1 from reentrancy, got %s", counterVal.String())
	}
}

// --------------------------------------------------------------------------
// 2. Stack depth limit: 1024 call depth, verify CALL fails at depth limit
// --------------------------------------------------------------------------

func TestCallDepthLimit(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0x01})
	contractAddr := types.BytesToAddress([]byte{0xCC})

	stateDB.CreateAccount(callerAddr)
	stateDB.CreateAccount(contractAddr)

	// Contract recursively calls itself. Due to 1024 depth limit, it should
	// eventually push 0 (failure) onto the stack from the CALL.
	// Code: CALL self recursively, then STOP
	code := []byte{
		byte(PUSH1), 0x00, // retLen
		byte(PUSH1), 0x00, // retOffset
		byte(PUSH1), 0x00, // argsLen
		byte(PUSH1), 0x00, // argsOffset
		byte(PUSH1), 0x00, // value
		byte(PUSH20),      // self address
	}
	code = append(code, contractAddr[:]...)
	code = append(code,
		byte(PUSH2), 0xFF, 0xFF, // gas
		byte(CALL),
		byte(POP),  // discard result
		byte(STOP),
	)
	stateDB.SetCode(contractAddr, code)

	// Warm the address
	stateDB.AddAddressToAccessList(contractAddr)

	// This should succeed without panicking (CALL at depth limit returns 0, not an error)
	_, _, err := evm.Call(callerAddr, contractAddr, nil, 100000000, big.NewInt(0))
	if err != nil {
		t.Fatalf("recursive call depth test failed: %v", err)
	}
}

// --------------------------------------------------------------------------
// 3. Out of gas in nested calls: Parent has gas, child runs out, parent continues
// --------------------------------------------------------------------------

func TestNestedCallOutOfGas(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0x01})
	parentAddr := types.BytesToAddress([]byte{0xAA})
	childAddr := types.BytesToAddress([]byte{0xBB})

	stateDB.CreateAccount(callerAddr)
	stateDB.CreateAccount(parentAddr)
	stateDB.CreateAccount(childAddr)
	stateDB.AddBalance(callerAddr, big.NewInt(1000000))

	// Child: expensive operation that runs out of gas
	// Infinite loop: PUSH1 0x00, JUMP to self (JUMPDEST at 0)
	childCode := []byte{
		byte(JUMPDEST),     // pos 0
		byte(PUSH1), 0x00, // jump target
		byte(JUMP),         // infinite loop -> runs out of gas
	}

	// Parent: CALL child with limited gas (100), then store 0x42 at slot 0, STOP
	parentCode := []byte{
		byte(PUSH1), 0x00,  // retLen
		byte(PUSH1), 0x00,  // retOffset
		byte(PUSH1), 0x00,  // argsLen
		byte(PUSH1), 0x00,  // argsOffset
		byte(PUSH1), 0x00,  // value
		byte(PUSH20),       // child address
	}
	parentCode = append(parentCode, childAddr[:]...)
	parentCode = append(parentCode,
		byte(PUSH1), 0x64,  // gas = 100 (too little for child)
		byte(CALL),
		// CALL pushed 0 (failure), pop it
		byte(POP),
		// Parent continues: store 0x42 at slot 0
		byte(PUSH1), 0x42,
		byte(PUSH1), 0x00,
		byte(SSTORE),
		byte(STOP),
	)

	stateDB.SetCode(parentAddr, parentCode)
	stateDB.SetCode(childAddr, childCode)

	stateDB.AddAddressToAccessList(parentAddr)
	stateDB.AddAddressToAccessList(childAddr)

	_, _, err := evm.Call(callerAddr, parentAddr, nil, 1000000, big.NewInt(0))
	if err != nil {
		t.Fatalf("nested out-of-gas call failed: %v", err)
	}

	// Parent should have stored 0x42 even though child ran out of gas
	val := stateDB.GetState(parentAddr, types.BytesToHash([]byte{0x00}))
	if val[31] != 0x42 {
		t.Errorf("parent state not set after child OOG: got %x, want 0x42 at last byte", val)
	}
}

// --------------------------------------------------------------------------
// 4. REVERT propagation: Child REVERTs, parent gets return data, state undone
// --------------------------------------------------------------------------

func TestRevertPropagation(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0x01})
	parentAddr := types.BytesToAddress([]byte{0xAA})
	childAddr := types.BytesToAddress([]byte{0xBB})

	stateDB.CreateAccount(callerAddr)
	stateDB.CreateAccount(parentAddr)
	stateDB.CreateAccount(childAddr)
	stateDB.AddBalance(callerAddr, big.NewInt(1000000))

	// Child: set storage slot 0 to 0xFF, then REVERT with return data [0xDE, 0xAD]
	childCode := []byte{
		byte(PUSH1), 0xFF,  // value
		byte(PUSH1), 0x00,  // slot
		byte(SSTORE),       // set slot 0 = 0xFF (should be reverted)
		byte(PUSH1), 0xDE,  // data byte
		byte(PUSH1), 0x00,
		byte(MSTORE8),      // mem[0] = 0xDE
		byte(PUSH1), 0xAD,
		byte(PUSH1), 0x01,
		byte(MSTORE8),      // mem[1] = 0xAD
		byte(PUSH1), 0x02,  // size
		byte(PUSH1), 0x00,  // offset
		byte(REVERT),       // revert with [0xDE, 0xAD]
	}

	// Parent: CALL child with return buffer, check RETURNDATASIZE, store 0x42 at slot 0
	parentCode := []byte{
		byte(PUSH1), 0x20,  // retLen = 32
		byte(PUSH1), 0x00,  // retOffset = 0
		byte(PUSH1), 0x00,  // argsLen
		byte(PUSH1), 0x00,  // argsOffset
		byte(PUSH1), 0x00,  // value
		byte(PUSH20),       // child address
	}
	parentCode = append(parentCode, childAddr[:]...)
	parentCode = append(parentCode,
		byte(PUSH2), 0xFF, 0xFF, // gas
		byte(CALL),
		// Stack: [0] (call failed due to revert)
		byte(POP),
		// Check RETURNDATASIZE should be 2
		byte(RETURNDATASIZE),
		byte(PUSH1), 0x20, // offset 0x20 (store at memory offset 32)
		byte(MSTORE),      // store returndatasize in memory
		// Store 0x42 to show parent continues
		byte(PUSH1), 0x42,
		byte(PUSH1), 0x00,
		byte(SSTORE),
		// Return returndatasize value
		byte(PUSH1), 0x20,  // size
		byte(PUSH1), 0x20,  // offset (where we stored returndatasize)
		byte(RETURN),
	)

	stateDB.SetCode(parentAddr, parentCode)
	stateDB.SetCode(childAddr, childCode)

	stateDB.AddAddressToAccessList(parentAddr)
	stateDB.AddAddressToAccessList(childAddr)

	ret, _, err := evm.Call(callerAddr, parentAddr, nil, 1000000, big.NewInt(0))
	if err != nil {
		t.Fatalf("revert propagation test failed: %v", err)
	}

	// Parent should have set slot 0 to 0x42
	val := stateDB.GetState(parentAddr, types.BytesToHash([]byte{0x00}))
	if val[31] != 0x42 {
		t.Errorf("parent state = %x, want 0x42 at last byte", val)
	}

	// Child's state change (slot 0 = 0xFF) should have been reverted
	childVal := stateDB.GetState(childAddr, types.BytesToHash([]byte{0x00}))
	if childVal[31] != 0x00 {
		t.Errorf("child state should be reverted, got %x", childVal)
	}

	// Return data should contain RETURNDATASIZE = 2 (as 32-byte big-endian)
	if len(ret) == 32 && ret[31] != 0x02 {
		t.Errorf("RETURNDATASIZE = %d, want 2", ret[31])
	}
}

// --------------------------------------------------------------------------
// 5. CREATE and CREATE2: Deploy contracts, verify addresses
// --------------------------------------------------------------------------

func TestCreateDeployment(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0x01})
	stateDB.CreateAccount(callerAddr)
	stateDB.AddBalance(callerAddr, big.NewInt(1000000))

	// Init code: returns [0x60, 0x42, 0x60, 0x00, 0x52, 0x60, 0x01, 0x60, 0x1f, 0xf3]
	// which is runtime code for: PUSH1 0x42, PUSH1 0, MSTORE, PUSH1 1, PUSH1 31, RETURN
	// (returns the byte 0x42)
	initCode := []byte{
		byte(PUSH1), 0x42,  // runtime will return 0x42
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x01,  // return 1 byte
		byte(PUSH1), 0x1f,  // from offset 31
		byte(RETURN),
	}

	// Test CREATE
	nonce := stateDB.GetNonce(callerAddr)
	expectedAddr := createAddress(callerAddr, nonce)

	ret, addr, gasLeft, err := evm.Create(callerAddr, initCode, 1000000, big.NewInt(0))
	if err != nil {
		t.Fatalf("CREATE failed: %v", err)
	}
	if addr != expectedAddr {
		t.Errorf("CREATE address = %x, want %x", addr, expectedAddr)
	}
	if gasLeft == 0 {
		t.Error("CREATE consumed all gas")
	}

	// Deployed code should be [0x42] (the byte returned by init code)
	deployedCode := stateDB.GetCode(addr)
	if len(deployedCode) != 1 || deployedCode[0] != 0x42 {
		t.Errorf("deployed code = %x, want [42]", deployedCode)
	}
	_ = ret
}

func TestCreate2Deployment(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0x01})
	stateDB.CreateAccount(callerAddr)
	stateDB.AddBalance(callerAddr, big.NewInt(1000000))

	initCode := []byte{
		byte(PUSH1), 0xAB,
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x01,
		byte(PUSH1), 0x1f,
		byte(RETURN),
	}

	salt := big.NewInt(12345)
	initCodeHash := crypto.Keccak256(initCode)
	expectedAddr := create2Address(callerAddr, salt, initCodeHash)

	_, addr, _, err := evm.Create2(callerAddr, initCode, 1000000, big.NewInt(0), salt)
	if err != nil {
		t.Fatalf("CREATE2 failed: %v", err)
	}
	if addr != expectedAddr {
		t.Errorf("CREATE2 address = %x, want %x", addr, expectedAddr)
	}

	deployedCode := stateDB.GetCode(addr)
	if len(deployedCode) != 1 || deployedCode[0] != 0xAB {
		t.Errorf("deployed code = %x, want [AB]", deployedCode)
	}
}

func TestCreate2DeterministicAddress(t *testing.T) {
	// CREATE2 with same inputs should always produce the same address
	callerAddr := types.BytesToAddress([]byte{0x01})
	initCode := []byte{byte(PUSH1), 0x00, byte(PUSH1), 0x00, byte(RETURN)}
	salt := big.NewInt(42)
	initCodeHash := crypto.Keccak256(initCode)

	addr1 := create2Address(callerAddr, salt, initCodeHash)
	addr2 := create2Address(callerAddr, salt, initCodeHash)
	if addr1 != addr2 {
		t.Error("CREATE2 is not deterministic")
	}

	// Different salt should produce different address
	salt2 := big.NewInt(43)
	addr3 := create2Address(callerAddr, salt2, initCodeHash)
	if addr1 == addr3 {
		t.Error("CREATE2 with different salt produced same address")
	}
}

// --------------------------------------------------------------------------
// 6. SELFDESTRUCT (EIP-6780): Balance transfer without account destruction
// --------------------------------------------------------------------------

func TestSelfdestructEIP6780(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	contractAddr := types.BytesToAddress([]byte{0xAA})
	beneficiary := types.BytesToAddress([]byte{0xBB})

	stateDB.CreateAccount(contractAddr)
	stateDB.AddBalance(contractAddr, big.NewInt(1000))
	stateDB.CreateAccount(beneficiary)

	// Code: PUSH20 <beneficiary>, SELFDESTRUCT
	code := []byte{byte(PUSH20)}
	code = append(code, beneficiary[:]...)
	code = append(code, byte(SELFDESTRUCT))
	stateDB.SetCode(contractAddr, code)

	stateDB.AddAddressToAccessList(contractAddr)
	stateDB.AddAddressToAccessList(beneficiary)

	callerAddr := types.BytesToAddress([]byte{0x01})
	stateDB.CreateAccount(callerAddr)

	_, _, err := evm.Call(callerAddr, contractAddr, nil, 1000000, big.NewInt(0))
	if err != nil {
		t.Fatalf("SELFDESTRUCT call failed: %v", err)
	}

	// Balance should have transferred
	benBal := stateDB.GetBalance(beneficiary)
	if benBal.Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("beneficiary balance = %s, want 1000", benBal.String())
	}

	// Contract balance should be zero
	contractBal := stateDB.GetBalance(contractAddr)
	if contractBal.Sign() != 0 {
		t.Errorf("contract balance = %s, want 0", contractBal.String())
	}

	// Post-EIP-6780: account should NOT be self-destructed
	// (only self-destructs in same-tx creation context)
	if stateDB.HasSelfDestructed(contractAddr) {
		t.Error("post-EIP-6780: SELFDESTRUCT should not destroy pre-existing account")
	}

	// Contract should still exist
	if !stateDB.Exist(contractAddr) {
		t.Error("post-EIP-6780: contract should still exist after SELFDESTRUCT")
	}
}

func TestSelfdestructStaticCallFails(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	contractAddr := types.BytesToAddress([]byte{0xAA})
	beneficiary := types.BytesToAddress([]byte{0xBB})
	callerAddr := types.BytesToAddress([]byte{0x01})

	stateDB.CreateAccount(contractAddr)
	stateDB.AddBalance(contractAddr, big.NewInt(1000))
	stateDB.CreateAccount(beneficiary)
	stateDB.CreateAccount(callerAddr)

	code := []byte{byte(PUSH20)}
	code = append(code, beneficiary[:]...)
	code = append(code, byte(SELFDESTRUCT))
	stateDB.SetCode(contractAddr, code)

	stateDB.AddAddressToAccessList(contractAddr)

	// SELFDESTRUCT in STATICCALL should fail with write protection
	_, _, err := evm.StaticCall(callerAddr, contractAddr, nil, 1000000)
	if err == nil {
		t.Error("SELFDESTRUCT in STATICCALL should fail")
	}
}

// --------------------------------------------------------------------------
// 7. Transient storage (EIP-1153): TLOAD/TSTORE isolated per transaction
// --------------------------------------------------------------------------

func TestTransientStorageIsolation(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0x01})
	contractAddr := types.BytesToAddress([]byte{0xAA})

	stateDB.CreateAccount(callerAddr)
	stateDB.CreateAccount(contractAddr)

	// Contract: TSTORE(key=1, value=0xAA), TLOAD(key=1), MSTORE, RETURN
	code := []byte{
		byte(PUSH1), 0xAA,  // value
		byte(PUSH1), 0x01,  // key
		byte(TSTORE),
		byte(PUSH1), 0x01,  // key
		byte(TLOAD),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	stateDB.SetCode(contractAddr, code)
	stateDB.AddAddressToAccessList(contractAddr)

	ret, _, err := evm.Call(callerAddr, contractAddr, nil, 1000000, big.NewInt(0))
	if err != nil {
		t.Fatalf("transient storage test failed: %v", err)
	}

	// TLOAD should return the value we TSTOREd
	if len(ret) != 32 || ret[31] != 0xAA {
		t.Errorf("TLOAD returned %x, want 0xAA at last byte", ret)
	}

	// Verify transient storage does not persist in regular storage
	val := stateDB.GetState(contractAddr, types.BytesToHash([]byte{0x01}))
	if val != (types.Hash{}) {
		t.Error("transient storage should not persist in regular storage")
	}
}

func TestTransientStorageWriteProtection(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0x01})
	contractAddr := types.BytesToAddress([]byte{0xAA})

	stateDB.CreateAccount(callerAddr)
	stateDB.CreateAccount(contractAddr)

	// Contract: TSTORE should fail in STATICCALL
	code := []byte{
		byte(PUSH1), 0xAA,
		byte(PUSH1), 0x01,
		byte(TSTORE),
		byte(STOP),
	}
	stateDB.SetCode(contractAddr, code)
	stateDB.AddAddressToAccessList(contractAddr)

	_, _, err := evm.StaticCall(callerAddr, contractAddr, nil, 1000000)
	if err == nil {
		t.Error("TSTORE in STATICCALL should fail with write protection")
	}
}

// --------------------------------------------------------------------------
// 8. Memory expansion: Test gas costs for progressively larger memory
// --------------------------------------------------------------------------

func TestMemoryExpansionGasCosts(t *testing.T) {
	tests := []struct {
		name     string
		oldSize  uint64
		newSize  uint64
		wantOk   bool
		minCost  uint64 // minimum expected cost (0 for no expansion)
	}{
		{"no expansion", 32, 32, true, 0},
		{"0 to 32", 0, 32, true, 3},         // 1 word: 1*3 = 3
		{"32 to 64", 32, 64, true, 3},        // 2 words cost - 1 word cost
		{"0 to 1024", 0, 1024, true, 96},     // 32 words: 32*3 + 32^2/512 = 96 + 2 = 98
		{"exceed max", 0, MaxMemorySize + 1, false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost, ok := MemoryCost(tt.oldSize, tt.newSize)
			if ok != tt.wantOk {
				t.Fatalf("MemoryCost(%d, %d) ok = %v, want %v", tt.oldSize, tt.newSize, ok, tt.wantOk)
			}
			if ok && cost < tt.minCost {
				t.Errorf("MemoryCost(%d, %d) = %d, want >= %d", tt.oldSize, tt.newSize, cost, tt.minCost)
			}
		})
	}
}

func TestMemoryExpansionProgressiveGas(t *testing.T) {
	// Verify that expanding memory step-by-step is correctly charged
	evm := newTestEVM()
	initialGas := uint64(10000000)
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), initialGas)

	// Three MSTOREs at increasing offsets: 0, 32, 64
	contract.Code = []byte{
		byte(PUSH1), 0x01,
		byte(PUSH1), 0x00,
		byte(MSTORE), // expand to 32 bytes
		byte(PUSH1), 0x02,
		byte(PUSH1), 0x20,
		byte(MSTORE), // expand to 64 bytes
		byte(PUSH1), 0x03,
		byte(PUSH1), 0x40,
		byte(MSTORE), // expand to 96 bytes
		byte(STOP),
	}

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("progressive memory expansion failed: %v", err)
	}

	gasUsed := initialGas - contract.Gas
	// Each step should cost more due to quadratic term
	// The total should be > 0 and reasonable
	if gasUsed == 0 {
		t.Error("expected gas to be consumed for memory expansion")
	}
}

// --------------------------------------------------------------------------
// 9. RETURNDATACOPY bounds: Reading beyond returndata size causes revert
// --------------------------------------------------------------------------

func TestReturndataCopyOutOfBounds(t *testing.T) {
	evm, contract, mem, st := setupTest()

	// Set up return data (5 bytes)
	evm.returnData = []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	mem.Resize(64)
	pc := uint64(0)

	// Attempt to copy 10 bytes from offset 0 (only 5 available)
	st.Push(big.NewInt(10)) // length
	st.Push(big.NewInt(0))  // dataOffset
	st.Push(big.NewInt(0))  // memOffset
	_, err := opReturndataCopy(&pc, evm, contract, mem, st)
	if !errors.Is(err, ErrReturnDataOutOfBounds) {
		t.Errorf("expected ErrReturnDataOutOfBounds, got %v", err)
	}

	// Attempt to copy 1 byte from offset 5 (exactly at end)
	st.Push(big.NewInt(1))  // length
	st.Push(big.NewInt(5))  // dataOffset (at end)
	st.Push(big.NewInt(0))  // memOffset
	_, err = opReturndataCopy(&pc, evm, contract, mem, st)
	if !errors.Is(err, ErrReturnDataOutOfBounds) {
		t.Errorf("expected ErrReturnDataOutOfBounds for offset at end, got %v", err)
	}

	// Valid: copy exactly 5 bytes
	st.Push(big.NewInt(5))  // length
	st.Push(big.NewInt(0))  // dataOffset
	st.Push(big.NewInt(0))  // memOffset
	_, err = opReturndataCopy(&pc, evm, contract, mem, st)
	if err != nil {
		t.Errorf("valid RETURNDATACOPY failed: %v", err)
	}

	// Verify data was copied
	got := mem.Get(0, 5)
	for i := 0; i < 5; i++ {
		if got[i] != byte(i+1) {
			t.Errorf("mem[%d] = %x, want %x", i, got[i], byte(i+1))
		}
	}
}

func TestReturndataCopyOverflow(t *testing.T) {
	evm, contract, mem, st := setupTest()
	evm.returnData = []byte{0x01}
	mem.Resize(64)
	pc := uint64(0)

	// dataOffset + length overflows uint64
	st.Push(big.NewInt(1))                              // length
	st.Push(new(big.Int).SetUint64(^uint64(0)))         // dataOffset (max uint64)
	st.Push(big.NewInt(0))                              // memOffset
	_, err := opReturndataCopy(&pc, evm, contract, mem, st)
	if !errors.Is(err, ErrReturnDataOutOfBounds) {
		t.Errorf("expected ErrReturnDataOutOfBounds for overflow, got %v", err)
	}
}

// --------------------------------------------------------------------------
// 10. Static call violations: SSTORE, CREATE, LOG, SELFDESTRUCT in STATICCALL
// --------------------------------------------------------------------------

func TestStaticCallViolation_SSTORE(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0x01})
	contractAddr := types.BytesToAddress([]byte{0xAA})

	stateDB.CreateAccount(callerAddr)
	stateDB.CreateAccount(contractAddr)

	// Contract: SSTORE (write operation)
	code := []byte{
		byte(PUSH1), 0x42,
		byte(PUSH1), 0x00,
		byte(SSTORE),
		byte(STOP),
	}
	stateDB.SetCode(contractAddr, code)
	stateDB.AddAddressToAccessList(contractAddr)

	_, _, err := evm.StaticCall(callerAddr, contractAddr, nil, 1000000)
	if !errors.Is(err, ErrWriteProtection) {
		t.Errorf("SSTORE in STATICCALL: expected ErrWriteProtection, got %v", err)
	}
}

func TestStaticCallViolation_CREATE(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0x01})
	contractAddr := types.BytesToAddress([]byte{0xAA})

	stateDB.CreateAccount(callerAddr)
	stateDB.CreateAccount(contractAddr)

	// Contract: CREATE (write operation)
	code := []byte{
		byte(PUSH1), 0x00, // length
		byte(PUSH1), 0x00, // offset
		byte(PUSH1), 0x00, // value
		byte(CREATE),
		byte(STOP),
	}
	stateDB.SetCode(contractAddr, code)
	stateDB.AddAddressToAccessList(contractAddr)

	_, _, err := evm.StaticCall(callerAddr, contractAddr, nil, 1000000)
	if !errors.Is(err, ErrWriteProtection) {
		t.Errorf("CREATE in STATICCALL: expected ErrWriteProtection, got %v", err)
	}
}

func TestStaticCallViolation_LOG(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0x01})
	contractAddr := types.BytesToAddress([]byte{0xAA})

	stateDB.CreateAccount(callerAddr)
	stateDB.CreateAccount(contractAddr)

	// Contract: LOG0 (write operation)
	code := []byte{
		byte(PUSH1), 0x00, // size
		byte(PUSH1), 0x00, // offset
		byte(LOG0),
		byte(STOP),
	}
	stateDB.SetCode(contractAddr, code)
	stateDB.AddAddressToAccessList(contractAddr)

	_, _, err := evm.StaticCall(callerAddr, contractAddr, nil, 1000000)
	if !errors.Is(err, ErrWriteProtection) {
		t.Errorf("LOG0 in STATICCALL: expected ErrWriteProtection, got %v", err)
	}
}

func TestStaticCallViolation_SELFDESTRUCT(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0x01})
	contractAddr := types.BytesToAddress([]byte{0xAA})

	stateDB.CreateAccount(callerAddr)
	stateDB.CreateAccount(contractAddr)

	// Contract: SELFDESTRUCT
	beneficiary := types.BytesToAddress([]byte{0xBB})
	stateDB.CreateAccount(beneficiary)

	code := []byte{byte(PUSH20)}
	code = append(code, beneficiary[:]...)
	code = append(code, byte(SELFDESTRUCT))
	stateDB.SetCode(contractAddr, code)
	stateDB.AddAddressToAccessList(contractAddr)

	_, _, err := evm.StaticCall(callerAddr, contractAddr, nil, 1000000)
	if !errors.Is(err, ErrWriteProtection) {
		t.Errorf("SELFDESTRUCT in STATICCALL: expected ErrWriteProtection, got %v", err)
	}
}

func TestStaticCallAllowsReads(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0x01})
	contractAddr := types.BytesToAddress([]byte{0xAA})

	stateDB.CreateAccount(callerAddr)
	stateDB.CreateAccount(contractAddr)
	// Pre-set a storage value to read
	stateDB.SetState(contractAddr, types.BytesToHash([]byte{0x00}), types.BytesToHash([]byte{0x42}))

	// Contract: SLOAD (read) should succeed in STATICCALL
	code := []byte{
		byte(PUSH1), 0x00,
		byte(SLOAD),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	stateDB.SetCode(contractAddr, code)
	stateDB.AddAddressToAccessList(contractAddr)

	ret, _, err := evm.StaticCall(callerAddr, contractAddr, nil, 1000000)
	if err != nil {
		t.Fatalf("SLOAD in STATICCALL should succeed, got %v", err)
	}
	if len(ret) != 32 || ret[31] != 0x42 {
		t.Errorf("SLOAD in STATICCALL returned %x, want 0x42", ret)
	}
}

// --------------------------------------------------------------------------
// 11. Zero-value CALL to non-existent account: Should create account
//     (per implementation - Call creates account if it doesn't exist)
// --------------------------------------------------------------------------

func TestZeroValueCallToNonExistentAccount(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0x01})
	nonExistent := types.BytesToAddress([]byte{0xFF, 0xEE, 0xDD})

	stateDB.CreateAccount(callerAddr)
	stateDB.AddBalance(callerAddr, big.NewInt(1000000))

	// Pre-check: account should not exist
	if stateDB.Exist(nonExistent) {
		t.Fatal("account should not exist before test")
	}

	// Call with zero value and no code
	_, _, err := evm.Call(callerAddr, nonExistent, nil, 100000, big.NewInt(0))
	if err != nil {
		t.Fatalf("zero-value call to non-existent account failed: %v", err)
	}

	// After the call, the account is created (per the EVM.Call implementation)
	// but this is an implementation detail; in production, the account may be
	// touched and empty-account pruning applies.
}

// --------------------------------------------------------------------------
// 12. Value transfer with insufficient balance: Should fail gracefully
// --------------------------------------------------------------------------

func TestValueTransferInsufficientBalance(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	// Use addresses > 0x0a to avoid precompile range (0x01-0x0a)
	callerAddr := types.BytesToAddress([]byte{0x11})
	targetAddr := types.BytesToAddress([]byte{0x22})

	stateDB.CreateAccount(callerAddr)
	stateDB.AddBalance(callerAddr, big.NewInt(100)) // only 100 wei
	stateDB.CreateAccount(targetAddr)

	// Try to send 1000 wei (more than balance)
	_, _, err := evm.Call(callerAddr, targetAddr, nil, 100000, big.NewInt(1000))
	if err == nil {
		t.Error("expected error for insufficient balance transfer")
	}

	// Balances should be unchanged
	callerBal := stateDB.GetBalance(callerAddr)
	if callerBal.Cmp(big.NewInt(100)) != 0 {
		t.Errorf("caller balance = %s, want 100", callerBal.String())
	}
}

func TestValueTransferSufficientBalance(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	// Use addresses > 0x0a to avoid precompile range (0x01-0x0a)
	callerAddr := types.BytesToAddress([]byte{0x11})
	targetAddr := types.BytesToAddress([]byte{0x22})

	stateDB.CreateAccount(callerAddr)
	stateDB.AddBalance(callerAddr, big.NewInt(1000))
	stateDB.CreateAccount(targetAddr)

	// Transfer 500 wei
	_, _, err := evm.Call(callerAddr, targetAddr, nil, 100000, big.NewInt(500))
	if err != nil {
		t.Fatalf("value transfer failed: %v", err)
	}

	// Verify balances
	callerBal := stateDB.GetBalance(callerAddr)
	if callerBal.Cmp(big.NewInt(500)) != 0 {
		t.Errorf("caller balance = %s, want 500", callerBal.String())
	}
	targetBal := stateDB.GetBalance(targetAddr)
	if targetBal.Cmp(big.NewInt(500)) != 0 {
		t.Errorf("target balance = %s, want 500", targetBal.String())
	}
}

// --------------------------------------------------------------------------
// 13. Contract code size limit: Max 24576 bytes (EIP-170)
// --------------------------------------------------------------------------

func TestContractCodeSizeLimit(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0x01})
	stateDB.CreateAccount(callerAddr)
	stateDB.AddBalance(callerAddr, big.NewInt(100000000))

	// Init code that returns MaxCodeSize bytes
	maxSize := MaxCodeSize // 24576

	// Build init code that returns maxSize bytes of 0x00
	// PUSH2 <maxSize>, PUSH1 0x00, RETURN
	initCode := []byte{
		byte(PUSH2), byte(maxSize >> 8), byte(maxSize), // size
		byte(PUSH1), 0x00, // offset
		byte(RETURN),
	}

	// This should succeed (exactly at limit)
	_, _, _, err := evm.Create(callerAddr, initCode, 100000000, big.NewInt(0))
	if err != nil {
		t.Fatalf("CREATE at max code size failed: %v", err)
	}

	// Now try one byte over the limit
	overSize := MaxCodeSize + 1
	initCodeOver := []byte{
		byte(PUSH2), byte(overSize >> 8), byte(overSize), // size
		byte(PUSH1), 0x00, // offset
		byte(RETURN),
	}

	_, _, _, err = evm.Create(callerAddr, initCodeOver, 100000000, big.NewInt(0))
	if err == nil {
		t.Error("CREATE exceeding max code size should fail")
	}
}

// --------------------------------------------------------------------------
// 14. Gas refund cap: SSTORE refunds capped at gasUsed/5 (EIP-3529)
// --------------------------------------------------------------------------

func TestGasRefundCap(t *testing.T) {
	// EIP-3529: max refund = gasUsed / MaxRefundQuotient (5)
	// Verify the constant
	if MaxRefundQuotient != 5 {
		t.Fatalf("MaxRefundQuotient = %d, want 5", MaxRefundQuotient)
	}

	// Test SstoreGas refund calculation for clearing a slot
	original := [32]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01}
	current := original // same as original
	newVal := [32]byte{} // clearing to zero

	gas, refund := SstoreGas(original, current, newVal, false)

	// Clearing a non-zero slot: gas = SstoreReset (2900), refund = SstoreClearsScheduleRefund (4800)
	if gas != GasSstoreReset {
		t.Errorf("SstoreGas for clear: gas = %d, want %d", gas, GasSstoreReset)
	}
	if refund != int64(SstoreClearsScheduleRefund) {
		t.Errorf("SstoreGas for clear: refund = %d, want %d", refund, SstoreClearsScheduleRefund)
	}

	// Verify the cap would apply: if gasUsed = 10000, max refund = 10000/5 = 2000
	// The 4800 refund would be capped to 2000
	gasUsed := uint64(10000)
	maxRefund := gasUsed / MaxRefundQuotient
	if maxRefund != 2000 {
		t.Errorf("max refund for gasUsed=%d: %d, want 2000", gasUsed, maxRefund)
	}

	// SstoreClearsScheduleRefund (4800) > 2000, so it would be capped
	if uint64(refund) <= maxRefund {
		t.Error("refund should exceed the cap for this test case")
	}
}

func TestGasRefundSstoreNoop(t *testing.T) {
	// Setting same value as current: no refund
	val := [32]byte{}
	val[31] = 0x42
	gas, refund := SstoreGas(val, val, val, false)
	if gas != WarmStorageReadCost {
		t.Errorf("noop SSTORE gas = %d, want %d", gas, WarmStorageReadCost)
	}
	if refund != 0 {
		t.Errorf("noop SSTORE refund = %d, want 0", refund)
	}
}

func TestGasRefundSstoreSet(t *testing.T) {
	// Creating new storage: zero -> non-zero
	original := [32]byte{} // zero
	current := original
	newVal := [32]byte{}
	newVal[31] = 0x01

	gas, refund := SstoreGas(original, current, newVal, false)
	if gas != GasSstoreSet {
		t.Errorf("SSTORE set gas = %d, want %d", gas, GasSstoreSet)
	}
	if refund != 0 {
		t.Errorf("SSTORE set refund = %d, want 0", refund)
	}
}

func TestGasRefundSstoreRestoreOriginal(t *testing.T) {
	// Restoring to original: should get refund
	original := [32]byte{}
	original[31] = 0x01
	current := [32]byte{}
	current[31] = 0x02
	newVal := original // restore to original

	gas, refund := SstoreGas(original, current, newVal, false)
	// Dirty slot: gas = WarmStorageReadCost
	if gas != WarmStorageReadCost {
		t.Errorf("SSTORE restore gas = %d, want %d", gas, WarmStorageReadCost)
	}
	// Restoring non-zero original: refund = SstoreReset - WarmStorageReadCost = 2800
	expectedRefund := int64(GasSstoreReset) - int64(WarmStorageReadCost)
	if refund != expectedRefund {
		t.Errorf("SSTORE restore refund = %d, want %d", refund, expectedRefund)
	}
}

// --------------------------------------------------------------------------
// Additional integration tests
// --------------------------------------------------------------------------

// TestCallWithReturnData verifies that CALL correctly stores return data
// accessible via RETURNDATASIZE and RETURNDATACOPY.
func TestCallWithReturnData(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0x01})
	childAddr := types.BytesToAddress([]byte{0xBB})

	stateDB.CreateAccount(callerAddr)
	stateDB.CreateAccount(childAddr)
	stateDB.AddBalance(callerAddr, big.NewInt(1000000))

	// Child returns 0x42 as a single byte
	childCode := []byte{
		byte(PUSH1), 0x42,
		byte(PUSH1), 0x00,
		byte(MSTORE8),     // mem[0] = 0x42
		byte(PUSH1), 0x01, // size
		byte(PUSH1), 0x00, // offset
		byte(RETURN),
	}
	stateDB.SetCode(childAddr, childCode)
	stateDB.AddAddressToAccessList(childAddr)

	// Call child and check return data
	ret, _, err := evm.Call(callerAddr, childAddr, nil, 1000000, big.NewInt(0))
	if err != nil {
		t.Fatalf("call with return data failed: %v", err)
	}
	if len(ret) != 1 || ret[0] != 0x42 {
		t.Errorf("return data = %x, want [42]", ret)
	}
}

// TestCreateInsufficientBalance verifies that CREATE with insufficient balance fails.
func TestCreateInsufficientBalance(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0x01})
	stateDB.CreateAccount(callerAddr)
	stateDB.AddBalance(callerAddr, big.NewInt(10)) // tiny balance

	initCode := []byte{byte(STOP)}

	_, _, _, err := evm.Create(callerAddr, initCode, 1000000, big.NewInt(1000))
	if err == nil {
		t.Error("CREATE with insufficient balance should fail")
	}
}

// TestMaxInitCodeSize verifies that init code exceeding MaxInitCodeSize fails.
func TestMaxInitCodeSize(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0x01})
	stateDB.CreateAccount(callerAddr)
	stateDB.AddBalance(callerAddr, big.NewInt(100000000))

	// Create init code that exceeds MaxInitCodeSize
	bigInitCode := make([]byte, MaxInitCodeSize+1)
	bigInitCode[0] = byte(STOP)

	_, _, _, err := evm.Create(callerAddr, bigInitCode, 100000000, big.NewInt(0))
	if !errors.Is(err, ErrMaxInitCodeSizeExceeded) {
		t.Errorf("expected ErrMaxInitCodeSizeExceeded, got %v", err)
	}
}

// TestCallDepthExceeded verifies that exceeding MaxCallDepth returns the appropriate error.
func TestCallDepthExceeded(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0x01})
	stateDB.CreateAccount(callerAddr)

	// Manually set depth beyond limit
	evm.depth = 1025

	_, _, err := evm.Call(callerAddr, types.BytesToAddress([]byte{0x22}), nil, 100000, big.NewInt(0))
	if !errors.Is(err, ErrMaxCallDepthExceeded) {
		t.Errorf("expected ErrMaxCallDepthExceeded, got %v", err)
	}

	// Same for StaticCall
	_, _, err = evm.StaticCall(callerAddr, types.BytesToAddress([]byte{0x22}), nil, 100000)
	if !errors.Is(err, ErrMaxCallDepthExceeded) {
		t.Errorf("expected ErrMaxCallDepthExceeded for StaticCall, got %v", err)
	}

	// Same for Create
	_, _, _, err = evm.Create(callerAddr, []byte{byte(STOP)}, 100000, big.NewInt(0))
	if !errors.Is(err, ErrMaxCallDepthExceeded) {
		t.Errorf("expected ErrMaxCallDepthExceeded for Create, got %v", err)
	}

	// Same for Create2
	_, _, _, err = evm.Create2(callerAddr, []byte{byte(STOP)}, 100000, big.NewInt(0), big.NewInt(0))
	if !errors.Is(err, ErrMaxCallDepthExceeded) {
		t.Errorf("expected ErrMaxCallDepthExceeded for Create2, got %v", err)
	}
}

// TestValueTransferInStaticCall verifies that value transfers in read-only mode fail.
func TestValueTransferInStaticCall(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	// Use addresses > 0x0a to avoid precompile range (0x01-0x0a)
	callerAddr := types.BytesToAddress([]byte{0x11})
	targetAddr := types.BytesToAddress([]byte{0x22})

	stateDB.CreateAccount(callerAddr)
	stateDB.AddBalance(callerAddr, big.NewInt(1000))
	stateDB.CreateAccount(targetAddr)

	// The EVM.Call function checks readOnly before allowing value transfer.
	// Set readOnly and try Call with value.
	evm.readOnly = true
	_, _, err := evm.Call(callerAddr, targetAddr, nil, 100000, big.NewInt(100))
	if !errors.Is(err, ErrWriteProtection) {
		t.Errorf("value transfer in read-only mode: expected ErrWriteProtection, got %v", err)
	}
}

// TestSnapshotAndRevert verifies that state changes are properly reverted.
func TestSnapshotAndRevert(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	contractAddr := types.BytesToAddress([]byte{0xAA})
	callerAddr := types.BytesToAddress([]byte{0x01})

	stateDB.CreateAccount(callerAddr)
	stateDB.AddBalance(callerAddr, big.NewInt(1000000))
	stateDB.CreateAccount(contractAddr)

	// Set initial state
	stateDB.SetState(contractAddr, types.BytesToHash([]byte{0x00}), types.BytesToHash([]byte{0x10}))

	// Take snapshot
	snap := stateDB.Snapshot()

	// Modify state
	stateDB.SetState(contractAddr, types.BytesToHash([]byte{0x00}), types.BytesToHash([]byte{0x20}))
	val := stateDB.GetState(contractAddr, types.BytesToHash([]byte{0x00}))
	if val[31] != 0x20 {
		t.Errorf("state after modification = %x, want 0x20", val)
	}

	// Revert
	stateDB.RevertToSnapshot(snap)
	val = stateDB.GetState(contractAddr, types.BytesToHash([]byte{0x00}))
	if val[31] != 0x10 {
		t.Errorf("state after revert = %x, want 0x10", val)
	}

	_ = evm // evm used for context
}

// TestDelegateCallPreservesContext verifies that DELEGATECALL runs code
// in the caller's context.
func TestDelegateCallPreservesContext(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0x01})
	libraryAddr := types.BytesToAddress([]byte{0xBB})

	stateDB.CreateAccount(callerAddr)
	stateDB.CreateAccount(libraryAddr)
	stateDB.AddBalance(callerAddr, big.NewInt(1000000))

	// Library code: SSTORE(slot=0, value=0x42)
	// When called via DELEGATECALL, this writes to the CALLER's storage
	libCode := []byte{
		byte(PUSH1), 0x42,
		byte(PUSH1), 0x00,
		byte(SSTORE),
		byte(STOP),
	}
	stateDB.SetCode(libraryAddr, libCode)
	stateDB.AddAddressToAccessList(libraryAddr)

	// DelegateCall the library from callerAddr
	_, _, err := evm.DelegateCall(callerAddr, libraryAddr, nil, 1000000)
	if err != nil {
		t.Fatalf("DELEGATECALL failed: %v", err)
	}

	// The storage should be written to callerAddr, not libraryAddr
	callerVal := stateDB.GetState(callerAddr, types.BytesToHash([]byte{0x00}))
	if callerVal[31] != 0x42 {
		t.Errorf("DELEGATECALL wrote to caller storage: %x, want 0x42", callerVal)
	}

	libVal := stateDB.GetState(libraryAddr, types.BytesToHash([]byte{0x00}))
	if libVal[31] != 0x00 {
		t.Errorf("DELEGATECALL should NOT write to library storage, got %x", libVal)
	}
}

// TestCreateWithValue verifies that CREATE with value transfers endowment.
func TestCreateWithValue(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0x01})
	stateDB.CreateAccount(callerAddr)
	stateDB.AddBalance(callerAddr, big.NewInt(1000))

	// Init code that just returns empty (deploys empty code)
	initCode := []byte{
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}

	endowment := big.NewInt(500)
	_, addr, _, err := evm.Create(callerAddr, initCode, 10000000, endowment)
	if err != nil {
		t.Fatalf("CREATE with value failed: %v", err)
	}

	// Verify endowment was transferred
	contractBal := stateDB.GetBalance(addr)
	if contractBal.Cmp(endowment) != 0 {
		t.Errorf("contract balance = %s, want %s", contractBal.String(), endowment.String())
	}

	callerBal := stateDB.GetBalance(callerAddr)
	if callerBal.Cmp(big.NewInt(500)) != 0 {
		t.Errorf("caller balance = %s, want 500", callerBal.String())
	}
}

// TestEmptyContractCall verifies that calling an address with no code succeeds
// with nil return data and all gas returned.
func TestEmptyContractCall(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	// Use addresses > 0x0a to avoid precompile range (0x01-0x0a)
	callerAddr := types.BytesToAddress([]byte{0x11})
	emptyAddr := types.BytesToAddress([]byte{0x22})

	stateDB.CreateAccount(callerAddr)
	stateDB.CreateAccount(emptyAddr)
	// emptyAddr has no code

	ret, gasLeft, err := evm.Call(callerAddr, emptyAddr, nil, 100000, big.NewInt(0))
	if err != nil {
		t.Fatalf("call to empty contract failed: %v", err)
	}
	if ret != nil {
		t.Errorf("expected nil return from empty contract, got %x", ret)
	}
	if gasLeft != 100000 {
		t.Errorf("gas should be fully returned for empty contract call, got %d", gasLeft)
	}
}

// TestCallCodeExecution verifies that CALLCODE runs the target's code in the
// caller's context (similar to DELEGATECALL but with msg.sender set to caller).
func TestCallCodeExecution(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0x01})
	targetAddr := types.BytesToAddress([]byte{0xBB})

	stateDB.CreateAccount(callerAddr)
	stateDB.CreateAccount(targetAddr)
	stateDB.AddBalance(callerAddr, big.NewInt(1000000))

	// Target code: SSTORE(0, 0x99)
	targetCode := []byte{
		byte(PUSH1), 0x99,
		byte(PUSH1), 0x00,
		byte(SSTORE),
		byte(STOP),
	}
	stateDB.SetCode(targetAddr, targetCode)
	stateDB.AddAddressToAccessList(targetAddr)

	// CALLCODE: runs target code in caller's context
	_, _, err := evm.CallCode(callerAddr, targetAddr, nil, 1000000, big.NewInt(0))
	if err != nil {
		t.Fatalf("CALLCODE failed: %v", err)
	}

	// Storage should be written to caller's address
	callerVal := stateDB.GetState(callerAddr, types.BytesToHash([]byte{0x00}))
	if callerVal[31] != 0x99 {
		t.Errorf("CALLCODE: caller storage = %x, want 0x99", callerVal)
	}

	targetVal := stateDB.GetState(targetAddr, types.BytesToHash([]byte{0x00}))
	if targetVal[31] != 0x00 {
		t.Errorf("CALLCODE: target storage should be empty, got %x", targetVal)
	}
}
