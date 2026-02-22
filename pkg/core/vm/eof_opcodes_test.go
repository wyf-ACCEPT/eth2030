package vm

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// newEOFTestEVM creates an EVM with the Glamsterdan jump table and EOF ops for testing.
func newEOFTestEVM() *EVM {
	blockCtx := BlockContext{
		BlockNumber: big.NewInt(1),
		BaseFee:     big.NewInt(1),
	}
	evm := NewEVM(blockCtx, TxContext{}, Config{})
	jt := NewGlamsterdanJumpTable()
	// Merge EOF operations into the jump table.
	for op, def := range EOFOperations() {
		jt[op] = def
	}
	evm.SetJumpTable(jt)
	return evm
}

// --- EIP-7069: RETURNDATALOAD ---

func TestReturndataload_Basic(t *testing.T) {
	evm := newEOFTestEVM()
	evm.returnData = []byte{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
		0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
		0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
	}

	// PUSH1 0x00, RETURNDATALOAD, PUSH1 0x00, MSTORE, PUSH1 0x20, PUSH1 0x00, RETURN
	code := []byte{
		byte(PUSH1), 0x00,
		byte(RETURNDATALOAD),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(types.Address{}, types.Address{}, nil, 100000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("RETURNDATALOAD failed: %v", err)
	}
	if len(ret) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(ret))
	}
	for i := 0; i < 32; i++ {
		if ret[i] != byte(i+1) {
			t.Fatalf("byte %d: got %02x, want %02x", i, ret[i], i+1)
		}
	}
}

func TestReturndataload_ZeroPadding(t *testing.T) {
	evm := newEOFTestEVM()
	// Only 10 bytes of return data. Reading at offset 5 should give 5 real bytes + 27 zeros.
	evm.returnData = []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a}

	code := []byte{
		byte(PUSH1), 0x05, // offset = 5
		byte(RETURNDATALOAD),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(types.Address{}, types.Address{}, nil, 100000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("RETURNDATALOAD zero-pad failed: %v", err)
	}
	// Bytes 5..9 of return data (0x06..0x0a), rest zero.
	for i := 0; i < 5; i++ {
		if ret[i] != byte(i+6) {
			t.Fatalf("byte %d: got %02x, want %02x", i, ret[i], i+6)
		}
	}
	for i := 5; i < 32; i++ {
		if ret[i] != 0 {
			t.Fatalf("byte %d: got %02x, want 0x00", i, ret[i])
		}
	}
}

func TestReturndataload_OutOfBounds(t *testing.T) {
	evm := newEOFTestEVM()
	evm.returnData = []byte{0x01, 0x02}

	// Offset 100 is way past the end -> all zeros.
	code := []byte{
		byte(PUSH1), 100,
		byte(RETURNDATALOAD),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(types.Address{}, types.Address{}, nil, 100000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("RETURNDATALOAD OOB failed: %v", err)
	}
	for i := range ret {
		if ret[i] != 0 {
			t.Fatalf("byte %d: got %02x, want 0x00", i, ret[i])
		}
	}
}

func TestReturndataload_EmptyReturnData(t *testing.T) {
	evm := newEOFTestEVM()
	evm.returnData = nil

	code := []byte{
		byte(PUSH1), 0x00,
		byte(RETURNDATALOAD),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(types.Address{}, types.Address{}, nil, 100000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("RETURNDATALOAD empty failed: %v", err)
	}
	for i := range ret {
		if ret[i] != 0 {
			t.Fatalf("byte %d: got %02x, want 0x00", i, ret[i])
		}
	}
}

// --- EIP-7069: EXTCALL ---

func TestExtcall_BasicSuccess(t *testing.T) {
	evm := newEOFTestEVM()
	stateDB := NewMockStateDB()
	stateDB.SetBalance(types.Address{0x01}, big.NewInt(1000000))
	// Target exists but has no code -> call succeeds immediately.
	stateDB.CreateAccount(types.Address{0x02})
	evm.StateDB = stateDB

	// EXTCALL: push value=0, input_size=0, input_offset=0, target_address
	code := []byte{
		byte(PUSH1), 0x00, // value = 0
		byte(PUSH1), 0x00, // input_size = 0
		byte(PUSH1), 0x00, // input_offset = 0
		byte(PUSH1), 0x02, // target = 0x02
		byte(EXTCALL),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}

	caller := types.Address{0x01}
	contract := NewContract(caller, caller, nil, 1000000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("EXTCALL basic failed: %v", err)
	}
	result := new(big.Int).SetBytes(ret)
	// Status 0 = success
	if result.Uint64() != ExtCallSuccess {
		t.Fatalf("EXTCALL status = %d, want %d (success)", result.Uint64(), ExtCallSuccess)
	}
}

func TestExtcall_ValueInStaticMode(t *testing.T) {
	evm := newEOFTestEVM()
	evm.readOnly = true

	// EXTCALL with non-zero value in static mode should halt with ErrWriteProtection.
	code := []byte{
		byte(PUSH1), 0x01, // value = 1 (non-zero)
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x02,
		byte(EXTCALL),
	}
	contract := NewContract(types.Address{0x01}, types.Address{0x01}, nil, 1000000)
	contract.Code = code

	_, err := evm.Run(contract, nil)
	if err != ErrWriteProtection {
		t.Fatalf("EXTCALL value in static: got err=%v, want ErrWriteProtection", err)
	}
}

func TestExtcall_InsufficientBalance(t *testing.T) {
	evm := newEOFTestEVM()
	stateDB := NewMockStateDB()
	stateDB.SetBalance(types.Address{0x01}, big.NewInt(0)) // no balance
	stateDB.CreateAccount(types.Address{0x02})
	evm.StateDB = stateDB

	// EXTCALL with value=100, but caller has no balance -> light failure (status 1).
	code := []byte{
		byte(PUSH1), 100, // value = 100
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x02,
		byte(EXTCALL),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(types.Address{0x01}, types.Address{0x01}, nil, 1000000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("EXTCALL insufficient balance: %v", err)
	}
	result := new(big.Int).SetBytes(ret)
	if result.Uint64() != ExtCallRevert {
		t.Fatalf("EXTCALL insufficient balance status = %d, want %d (revert)", result.Uint64(), ExtCallRevert)
	}
}

func TestExtcall_MaxCallDepth(t *testing.T) {
	evm := newEOFTestEVM()
	evm.depth = 1024 // at max depth

	code := []byte{
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x02,
		byte(EXTCALL),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(types.Address{0x01}, types.Address{0x01}, nil, 1000000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("EXTCALL max depth: %v", err)
	}
	result := new(big.Int).SetBytes(ret)
	if result.Uint64() != ExtCallRevert {
		t.Fatalf("EXTCALL max depth status = %d, want %d (revert)", result.Uint64(), ExtCallRevert)
	}
}

func TestExtcall_InvalidAddress(t *testing.T) {
	evm := newEOFTestEVM()

	// Push a 21-byte value (more than 20 bytes) -> exceptional halt.
	code := []byte{
		byte(PUSH1), 0x00, // value
		byte(PUSH1), 0x00, // input_size
		byte(PUSH1), 0x00, // input_offset
		// Push a 21-byte address (high bytes non-zero).
		byte(PUSH21), 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		byte(EXTCALL),
	}
	contract := NewContract(types.Address{0x01}, types.Address{0x01}, nil, 1000000)
	contract.Code = code

	_, err := evm.Run(contract, nil)
	if err != ErrInvalidOpCode {
		t.Fatalf("EXTCALL invalid addr: got err=%v, want ErrInvalidOpCode", err)
	}
}

// --- EIP-7069: EXTDELEGATECALL ---

func TestExtdelegatecall_BasicSuccess(t *testing.T) {
	evm := newEOFTestEVM()
	stateDB := NewMockStateDB()
	stateDB.CreateAccount(types.Address{0x02})
	evm.StateDB = stateDB

	code := []byte{
		byte(PUSH1), 0x00, // input_size
		byte(PUSH1), 0x00, // input_offset
		byte(PUSH1), 0x02, // target
		byte(EXTDELEGATECALL),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	caller := types.Address{0x01}
	contract := NewContract(caller, caller, nil, 1000000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("EXTDELEGATECALL basic failed: %v", err)
	}
	result := new(big.Int).SetBytes(ret)
	if result.Uint64() != ExtCallSuccess {
		t.Fatalf("EXTDELEGATECALL status = %d, want %d", result.Uint64(), ExtCallSuccess)
	}
}

func TestExtdelegatecall_MaxCallDepth(t *testing.T) {
	evm := newEOFTestEVM()
	evm.depth = 1024

	code := []byte{
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x02,
		byte(EXTDELEGATECALL),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(types.Address{0x01}, types.Address{0x01}, nil, 1000000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("EXTDELEGATECALL max depth: %v", err)
	}
	result := new(big.Int).SetBytes(ret)
	if result.Uint64() != ExtCallRevert {
		t.Fatalf("EXTDELEGATECALL max depth status = %d, want %d", result.Uint64(), ExtCallRevert)
	}
}

// --- EIP-7069: EXTSTATICCALL ---

func TestExtstaticcall_BasicSuccess(t *testing.T) {
	evm := newEOFTestEVM()
	stateDB := NewMockStateDB()
	stateDB.CreateAccount(types.Address{0x02})
	evm.StateDB = stateDB

	code := []byte{
		byte(PUSH1), 0x00, // input_size
		byte(PUSH1), 0x00, // input_offset
		byte(PUSH1), 0x02, // target
		byte(EXTSTATICCALL),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	caller := types.Address{0x01}
	contract := NewContract(caller, caller, nil, 1000000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("EXTSTATICCALL basic failed: %v", err)
	}
	result := new(big.Int).SetBytes(ret)
	if result.Uint64() != ExtCallSuccess {
		t.Fatalf("EXTSTATICCALL status = %d, want %d", result.Uint64(), ExtCallSuccess)
	}
}

// --- EIP-7480: DATALOAD ---

func TestDataload_Basic(t *testing.T) {
	evm := newEOFTestEVM()

	dataSection := make([]byte, 64)
	for i := range dataSection {
		dataSection[i] = byte(i + 1)
	}

	// PUSH1 0x00, DATALOAD -> loads data[0:32]
	code := []byte{
		byte(PUSH1), 0x00,
		byte(DATALOAD),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(types.Address{}, types.Address{}, nil, 100000)
	contract.Code = code
	contract.Data = dataSection

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("DATALOAD failed: %v", err)
	}
	for i := 0; i < 32; i++ {
		if ret[i] != byte(i+1) {
			t.Fatalf("byte %d: got %02x, want %02x", i, ret[i], i+1)
		}
	}
}

func TestDataload_Offset(t *testing.T) {
	evm := newEOFTestEVM()

	dataSection := make([]byte, 64)
	for i := range dataSection {
		dataSection[i] = byte(i + 1)
	}

	// DATALOAD at offset 16 -> loads data[16:48]
	code := []byte{
		byte(PUSH1), 16,
		byte(DATALOAD),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(types.Address{}, types.Address{}, nil, 100000)
	contract.Code = code
	contract.Data = dataSection

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("DATALOAD offset failed: %v", err)
	}
	for i := 0; i < 32; i++ {
		if ret[i] != byte(i+17) {
			t.Fatalf("byte %d: got %02x, want %02x", i, ret[i], i+17)
		}
	}
}

func TestDataload_ZeroPadding(t *testing.T) {
	evm := newEOFTestEVM()

	dataSection := []byte{0xaa, 0xbb, 0xcc}

	// DATALOAD at offset 1 -> [0xbb, 0xcc, 0x00..0x00]
	code := []byte{
		byte(PUSH1), 0x01,
		byte(DATALOAD),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(types.Address{}, types.Address{}, nil, 100000)
	contract.Code = code
	contract.Data = dataSection

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("DATALOAD zero-pad failed: %v", err)
	}
	if ret[0] != 0xbb || ret[1] != 0xcc {
		t.Fatalf("first 2 bytes: got %02x %02x, want bb cc", ret[0], ret[1])
	}
	for i := 2; i < 32; i++ {
		if ret[i] != 0 {
			t.Fatalf("byte %d: got %02x, want 0x00", i, ret[i])
		}
	}
}

func TestDataload_OutOfBounds(t *testing.T) {
	evm := newEOFTestEVM()
	dataSection := []byte{0x01, 0x02}

	code := []byte{
		byte(PUSH1), 200, // offset way past end
		byte(DATALOAD),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(types.Address{}, types.Address{}, nil, 100000)
	contract.Code = code
	contract.Data = dataSection

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("DATALOAD OOB failed: %v", err)
	}
	for i := range ret {
		if ret[i] != 0 {
			t.Fatalf("byte %d: got %02x, want 0x00", i, ret[i])
		}
	}
}

func TestDataload_NilData(t *testing.T) {
	evm := newEOFTestEVM()

	code := []byte{
		byte(PUSH1), 0x00,
		byte(DATALOAD),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(types.Address{}, types.Address{}, nil, 100000)
	contract.Code = code
	// contract.Data is nil

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("DATALOAD nil data failed: %v", err)
	}
	for i := range ret {
		if ret[i] != 0 {
			t.Fatalf("byte %d: got %02x, want 0x00", i, ret[i])
		}
	}
}

// --- EIP-7480: DATALOADN ---

func TestDataloadN_Basic(t *testing.T) {
	evm := newEOFTestEVM()

	dataSection := make([]byte, 64)
	for i := range dataSection {
		dataSection[i] = byte(i + 1)
	}

	// DATALOADN with 2-byte immediate offset = 0x0000 (offset 0)
	code := []byte{
		byte(DATALOADN), 0x00, 0x00,
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(types.Address{}, types.Address{}, nil, 100000)
	contract.Code = code
	contract.Data = dataSection

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("DATALOADN failed: %v", err)
	}
	for i := 0; i < 32; i++ {
		if ret[i] != byte(i+1) {
			t.Fatalf("byte %d: got %02x, want %02x", i, ret[i], i+1)
		}
	}
}

func TestDataloadN_NonZeroOffset(t *testing.T) {
	evm := newEOFTestEVM()

	dataSection := make([]byte, 64)
	for i := range dataSection {
		dataSection[i] = byte(i + 1)
	}

	// DATALOADN with offset = 0x0010 (16)
	code := []byte{
		byte(DATALOADN), 0x00, 0x10,
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(types.Address{}, types.Address{}, nil, 100000)
	contract.Code = code
	contract.Data = dataSection

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("DATALOADN offset 16 failed: %v", err)
	}
	for i := 0; i < 32; i++ {
		if ret[i] != byte(i+17) {
			t.Fatalf("byte %d: got %02x, want %02x", i, ret[i], i+17)
		}
	}
}

func TestDataloadN_ZeroPadding(t *testing.T) {
	evm := newEOFTestEVM()

	dataSection := []byte{0xaa, 0xbb, 0xcc, 0xdd}

	// DATALOADN at offset 2 -> [0xcc, 0xdd, 0x00..0x00]
	code := []byte{
		byte(DATALOADN), 0x00, 0x02,
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(types.Address{}, types.Address{}, nil, 100000)
	contract.Code = code
	contract.Data = dataSection

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("DATALOADN zero-pad failed: %v", err)
	}
	if ret[0] != 0xcc || ret[1] != 0xdd {
		t.Fatalf("first bytes: got %02x %02x, want cc dd", ret[0], ret[1])
	}
	for i := 2; i < 32; i++ {
		if ret[i] != 0 {
			t.Fatalf("byte %d: got %02x, want 0x00", i, ret[i])
		}
	}
}

// --- EIP-7480: DATASIZE ---

func TestDatasize_Basic(t *testing.T) {
	evm := newEOFTestEVM()

	code := []byte{
		byte(DATASIZE),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(types.Address{}, types.Address{}, nil, 100000)
	contract.Code = code
	contract.Data = make([]byte, 42)

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("DATASIZE failed: %v", err)
	}
	result := new(big.Int).SetBytes(ret)
	if result.Uint64() != 42 {
		t.Fatalf("DATASIZE = %d, want 42", result.Uint64())
	}
}

func TestDatasize_NilData(t *testing.T) {
	evm := newEOFTestEVM()

	code := []byte{
		byte(DATASIZE),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(types.Address{}, types.Address{}, nil, 100000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("DATASIZE nil failed: %v", err)
	}
	result := new(big.Int).SetBytes(ret)
	if result.Uint64() != 0 {
		t.Fatalf("DATASIZE nil = %d, want 0", result.Uint64())
	}
}

func TestDatasize_EmptyData(t *testing.T) {
	evm := newEOFTestEVM()

	code := []byte{
		byte(DATASIZE),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(types.Address{}, types.Address{}, nil, 100000)
	contract.Code = code
	contract.Data = []byte{}

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("DATASIZE empty failed: %v", err)
	}
	result := new(big.Int).SetBytes(ret)
	if result.Uint64() != 0 {
		t.Fatalf("DATASIZE empty = %d, want 0", result.Uint64())
	}
}

// --- EIP-7480: DATACOPY ---

func TestDatacopy_Basic(t *testing.T) {
	evm := newEOFTestEVM()

	dataSection := []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}

	// DATACOPY: copy 4 bytes from data offset 1 to memory offset 0
	code := []byte{
		byte(PUSH1), 0x04, // size = 4
		byte(PUSH1), 0x01, // data offset = 1
		byte(PUSH1), 0x00, // mem offset = 0
		byte(DATACOPY),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(types.Address{}, types.Address{}, nil, 100000)
	contract.Code = code
	contract.Data = dataSection

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("DATACOPY failed: %v", err)
	}
	expected := []byte{0xbb, 0xcc, 0xdd, 0xee}
	for i := 0; i < 4; i++ {
		if ret[i] != expected[i] {
			t.Fatalf("byte %d: got %02x, want %02x", i, ret[i], expected[i])
		}
	}
}

func TestDatacopy_ZeroLength(t *testing.T) {
	evm := newEOFTestEVM()

	code := []byte{
		byte(PUSH1), 0x00, // size = 0
		byte(PUSH1), 0x00, // data offset = 0
		byte(PUSH1), 0x00, // mem offset = 0
		byte(DATACOPY),
		byte(STOP),
	}
	contract := NewContract(types.Address{}, types.Address{}, nil, 100000)
	contract.Code = code
	contract.Data = []byte{0x01, 0x02}

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("DATACOPY zero-length failed: %v", err)
	}
}

func TestDatacopy_OutOfBoundsZeroPad(t *testing.T) {
	evm := newEOFTestEVM()

	dataSection := []byte{0xaa, 0xbb}

	// Copy 4 bytes from data offset 1 -> [0xbb, 0x00, 0x00, 0x00]
	code := []byte{
		byte(PUSH1), 0x04, // size = 4
		byte(PUSH1), 0x01, // data offset = 1
		byte(PUSH1), 0x00, // mem offset = 0
		byte(DATACOPY),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(types.Address{}, types.Address{}, nil, 100000)
	contract.Code = code
	contract.Data = dataSection

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("DATACOPY OOB zero-pad failed: %v", err)
	}
	if ret[0] != 0xbb {
		t.Fatalf("byte 0: got %02x, want 0xbb", ret[0])
	}
	for i := 1; i < 4; i++ {
		if ret[i] != 0 {
			t.Fatalf("byte %d: got %02x, want 0x00", i, ret[i])
		}
	}
}

func TestDatacopy_NilData(t *testing.T) {
	evm := newEOFTestEVM()

	// Copy 4 bytes from nil data -> all zeros.
	code := []byte{
		byte(PUSH1), 0x04,
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(DATACOPY),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(types.Address{}, types.Address{}, nil, 100000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("DATACOPY nil data failed: %v", err)
	}
	for i := 0; i < 4; i++ {
		if ret[i] != 0 {
			t.Fatalf("byte %d: got %02x, want 0x00", i, ret[i])
		}
	}
}

// --- EIP-7620: EOFCREATE ---

func TestEofcreate_WriteProtection(t *testing.T) {
	evm := newEOFTestEVM()
	evm.readOnly = true

	// EOFCREATE in read-only mode should fail with ErrWriteProtection.
	code := []byte{
		byte(PUSH1), 0x00, // input_size
		byte(PUSH1), 0x00, // input_offset
		byte(PUSH1), 0x00, // salt
		byte(PUSH1), 0x00, // value
		byte(EOFCREATE), 0x00, // initcontainer_index = 0
	}
	contract := NewContract(types.Address{0x01}, types.Address{0x01}, nil, 1000000)
	contract.Code = code

	_, err := evm.Run(contract, nil)
	if err != ErrWriteProtection {
		t.Fatalf("EOFCREATE in readOnly: got err=%v, want ErrWriteProtection", err)
	}
}

func TestEofcreate_MaxCallDepth(t *testing.T) {
	evm := newEOFTestEVM()
	evm.depth = 1024

	code := []byte{
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(EOFCREATE), 0x00,
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(types.Address{0x01}, types.Address{0x01}, nil, 1000000)
	contract.Code = code
	contract.Subcontainers = [][]byte{{byte(STOP)}}

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("EOFCREATE max depth: %v", err)
	}
	result := new(big.Int).SetBytes(ret)
	if result.Sign() != 0 {
		t.Fatalf("EOFCREATE max depth should push 0 (failure), got %d", result.Uint64())
	}
}

func TestEofcreate_NoSubcontainers(t *testing.T) {
	evm := newEOFTestEVM()

	code := []byte{
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(EOFCREATE), 0x00, // index 0, but no subcontainers
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(types.Address{0x01}, types.Address{0x01}, nil, 1000000)
	contract.Code = code
	// No subcontainers set.

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("EOFCREATE no subcontainers: %v", err)
	}
	result := new(big.Int).SetBytes(ret)
	if result.Sign() != 0 {
		t.Fatalf("EOFCREATE no subcontainers should push 0, got %d", result.Uint64())
	}
}

func TestEofcreate_InsufficientBalance(t *testing.T) {
	evm := newEOFTestEVM()
	stateDB := NewMockStateDB()
	stateDB.SetBalance(types.Address{0x01}, big.NewInt(0))
	evm.StateDB = stateDB

	code := []byte{
		byte(PUSH1), 0x00, // input_size
		byte(PUSH1), 0x00, // input_offset
		byte(PUSH1), 0x00, // salt
		byte(PUSH1), 100, // value = 100 (no balance)
		byte(EOFCREATE), 0x00,
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(types.Address{0x01}, types.Address{0x01}, nil, 1000000)
	contract.Code = code
	contract.Subcontainers = [][]byte{{byte(STOP)}}

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("EOFCREATE insufficient balance: %v", err)
	}
	result := new(big.Int).SetBytes(ret)
	if result.Sign() != 0 {
		t.Fatalf("EOFCREATE insufficient balance should push 0, got %d", result.Uint64())
	}
}

// --- EIP-7620: RETURNCONTRACT ---

func TestReturncontract_Basic(t *testing.T) {
	evm := newEOFTestEVM()

	deployCode := []byte{0xef, 0x00, 0x01}
	auxData := []byte{0xaa, 0xbb}

	// Store aux data in memory first, then RETURNCONTRACT.
	code := []byte{
		// Store aux data [0xaa, 0xbb] at memory offset 0.
		byte(PUSH2), 0xaa, 0xbb,
		byte(PUSH1), 0x00,
		byte(MSTORE), // stores at 0..31

		// RETURNCONTRACT: aux_data_size=2, aux_data_offset=30 (0xaa is at byte 30, 0xbb at 31)
		byte(PUSH1), 0x02, // aux_data_size
		byte(PUSH1), 30, // aux_data_offset
		byte(RETURNCONTRACT), 0x00,
	}
	contract := NewContract(types.Address{0x01}, types.Address{0x01}, nil, 100000)
	contract.Code = code
	contract.Subcontainers = [][]byte{deployCode}

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("RETURNCONTRACT failed: %v", err)
	}
	// Result should be deployCode + auxData.
	expected := append([]byte{}, deployCode...)
	expected = append(expected, auxData...)
	if len(ret) != len(expected) {
		t.Fatalf("RETURNCONTRACT: got %d bytes, want %d", len(ret), len(expected))
	}
	for i := range expected {
		if ret[i] != expected[i] {
			t.Fatalf("byte %d: got %02x, want %02x", i, ret[i], expected[i])
		}
	}
}

func TestReturncontract_NoSubcontainers(t *testing.T) {
	evm := newEOFTestEVM()

	code := []byte{
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(RETURNCONTRACT), 0x00,
	}
	contract := NewContract(types.Address{0x01}, types.Address{0x01}, nil, 100000)
	contract.Code = code
	// No subcontainers.

	_, err := evm.Run(contract, nil)
	if err != ErrInvalidOpCode {
		t.Fatalf("RETURNCONTRACT no subcontainers: got err=%v, want ErrInvalidOpCode", err)
	}
}

// --- Gas cost tests ---

func TestReturndataload_GasCost(t *testing.T) {
	ops := EOFOperations()
	op := ops[RETURNDATALOAD]
	if op.constantGas != 3 {
		t.Fatalf("RETURNDATALOAD gas = %d, want 3", op.constantGas)
	}
}

func TestDataload_GasCost(t *testing.T) {
	ops := EOFOperations()
	if ops[DATALOAD].constantGas != 4 {
		t.Fatalf("DATALOAD gas = %d, want 4", ops[DATALOAD].constantGas)
	}
}

func TestDataloadN_GasCost(t *testing.T) {
	ops := EOFOperations()
	if ops[DATALOADN].constantGas != 3 {
		t.Fatalf("DATALOADN gas = %d, want 3", ops[DATALOADN].constantGas)
	}
}

func TestDatasize_GasCost(t *testing.T) {
	ops := EOFOperations()
	if ops[DATASIZE].constantGas != 2 {
		t.Fatalf("DATASIZE gas = %d, want 2", ops[DATASIZE].constantGas)
	}
}

func TestDatacopy_GasCost(t *testing.T) {
	ops := EOFOperations()
	if ops[DATACOPY].constantGas != 3 {
		t.Fatalf("DATACOPY base gas = %d, want 3", ops[DATACOPY].constantGas)
	}
}

func TestExtcall_GasCost(t *testing.T) {
	ops := EOFOperations()
	if ops[EXTCALL].constantGas != 100 {
		t.Fatalf("EXTCALL gas = %d, want 100", ops[EXTCALL].constantGas)
	}
}

func TestEofcreate_GasCost(t *testing.T) {
	ops := EOFOperations()
	if ops[EOFCREATE].constantGas != 32000 {
		t.Fatalf("EOFCREATE gas = %d, want 32000", ops[EOFCREATE].constantGas)
	}
}

// --- EOFOperations table tests ---

func TestEOFOperations_AllPresent(t *testing.T) {
	ops := EOFOperations()
	expected := []OpCode{
		RETURNDATALOAD, EXTCALL, EXTDELEGATECALL, EXTSTATICCALL,
		DATALOAD, DATALOADN, DATASIZE, DATACOPY,
		EOFCREATE, RETURNCONTRACT,
	}
	for _, op := range expected {
		if _, ok := ops[op]; !ok {
			t.Errorf("EOFOperations missing opcode %s (0x%02x)", op, byte(op))
		}
	}
}

func TestEOFOperations_StackBounds(t *testing.T) {
	ops := EOFOperations()

	cases := []struct {
		op       OpCode
		minStack int
		maxStack int
	}{
		{RETURNDATALOAD, 1, 1024},
		{EXTCALL, 4, 1024},
		{EXTDELEGATECALL, 3, 1024},
		{EXTSTATICCALL, 3, 1024},
		{DATALOAD, 1, 1024},
		{DATALOADN, 0, 1023},
		{DATASIZE, 0, 1023},
		{DATACOPY, 3, 1024},
		{EOFCREATE, 4, 1024},
		{RETURNCONTRACT, 2, 1024},
	}

	for _, tc := range cases {
		op := ops[tc.op]
		if op.minStack != tc.minStack {
			t.Errorf("%s minStack = %d, want %d", tc.op, op.minStack, tc.minStack)
		}
		if op.maxStack != tc.maxStack {
			t.Errorf("%s maxStack = %d, want %d", tc.op, op.maxStack, tc.maxStack)
		}
	}
}

func TestEOFOperations_HaltsAndWrites(t *testing.T) {
	ops := EOFOperations()

	if !ops[RETURNCONTRACT].halts {
		t.Error("RETURNCONTRACT should halt")
	}
	if !ops[EOFCREATE].writes {
		t.Error("EOFCREATE should be a write operation")
	}
	if ops[DATALOAD].halts {
		t.Error("DATALOAD should not halt")
	}
	if ops[EXTCALL].halts {
		t.Error("EXTCALL should not halt")
	}
}

// --- Opcode name tests ---

func TestEOFOpcodeNames(t *testing.T) {
	cases := map[OpCode]string{
		DATALOAD:        "DATALOAD",
		DATALOADN:       "DATALOADN",
		DATASIZE:        "DATASIZE",
		DATACOPY:        "DATACOPY",
		EOFCREATE:       "EOFCREATE",
		RETURNCONTRACT:  "RETURNCONTRACT",
		RETURNDATALOAD:  "RETURNDATALOAD",
		EXTCALL:         "EXTCALL",
		EXTDELEGATECALL: "EXTDELEGATECALL",
		EXTSTATICCALL:   "EXTSTATICCALL",
	}
	for op, name := range cases {
		if op.String() != name {
			t.Errorf("opcode 0x%02x: got name %q, want %q", byte(op), op.String(), name)
		}
	}
}

// --- Gas calculation edge cases ---

func TestGasDatacopy_DynamicGas(t *testing.T) {
	// Create a mock setup to test gasDatacopy dynamic gas calculation.
	evm := newEOFTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, nil, 100000)
	stack := NewStack()
	mem := NewMemory()

	// Push in reverse order: size, data_offset, mem_offset
	// Back(0)=mem_offset, Back(1)=data_offset, Back(2)=size
	stack.Push(new(big.Int).SetUint64(64)) // size = 64 bytes = 2 words (Back(2))
	stack.Push(new(big.Int).SetUint64(0))  // data_offset (Back(1))
	stack.Push(new(big.Int).SetUint64(0))  // mem_offset (Back(0))

	// memorySize = mem_offset + size = 0 + 64 = 64
	gas, err := gasDatacopy(evm, contract, stack, mem, 64)
	if err != nil {
		t.Fatalf("gasDatacopy returned error: %v", err)
	}

	// Expected: GasCopy(3) * word_count(2) + memory expansion for 64 bytes.
	// Memory expansion: words=2, cost = 2*3 + (2*2)/512 = 6.
	// Total = 6 (copy) + 6 (mem) = 12, unless memory expansion is already
	// accounted for. The gas function returns the dynamic portion only;
	// verify it is at least the copy cost.
	expectedCopyGas := uint64(3 * 2) // GasCopy * words
	if gas < expectedCopyGas {
		t.Fatalf("gasDatacopy = %d, want at least %d (copy cost)", gas, expectedCopyGas)
	}
}

func TestGasDatacopy_ZeroSize(t *testing.T) {
	evm := newEOFTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, nil, 100000)
	stack := NewStack()
	mem := NewMemory()

	// Push in reverse order: size, data_offset, mem_offset
	stack.Push(new(big.Int).SetUint64(0)) // size = 0 (Back(2))
	stack.Push(new(big.Int).SetUint64(0)) // data_offset (Back(1))
	stack.Push(new(big.Int).SetUint64(0)) // mem_offset (Back(0))

	gas, err := gasDatacopy(evm, contract, stack, mem, 0)
	if err != nil {
		t.Fatalf("gasDatacopy returned error: %v", err)
	}
	if gas != 0 {
		t.Fatalf("gasDatacopy with zero size = %d, want 0", gas)
	}
}

// --- Memory size function tests ---

func TestMemoryExtcall(t *testing.T) {
	stack := NewStack()
	// Push in reverse order: value, input_size, input_offset, target_address
	// so that Back(0)=target, Back(1)=input_offset, Back(2)=input_size, Back(3)=value
	stack.Push(new(big.Int).SetUint64(0))  // value (Back(3))
	stack.Push(new(big.Int).SetUint64(64)) // input_size (Back(2))
	stack.Push(new(big.Int).SetUint64(0))  // input_offset (Back(1))
	stack.Push(new(big.Int).SetUint64(0))  // target_address (Back(0))

	size, overflow := memoryExtcall(stack)
	if overflow {
		t.Fatalf("memoryExtcall overflowed unexpectedly")
	}
	// input_offset(0) + input_size(64) = 64
	if size != 64 {
		t.Fatalf("memoryExtcall = %d, want 64", size)
	}
}

func TestMemoryExtcall_ZeroInputSize(t *testing.T) {
	stack := NewStack()
	// Push in reverse order: value, input_size, input_offset, target_address
	stack.Push(new(big.Int).SetUint64(0)) // value (Back(3))
	stack.Push(new(big.Int).SetUint64(0)) // input_size = 0 (Back(2))
	stack.Push(new(big.Int).SetUint64(0)) // input_offset (Back(1))
	stack.Push(new(big.Int).SetUint64(0)) // target_address (Back(0))

	size, overflow := memoryExtcall(stack)
	if overflow {
		t.Fatalf("memoryExtcall overflowed unexpectedly")
	}
	if size != 0 {
		t.Fatalf("memoryExtcall zero input = %d, want 0", size)
	}
}

func TestMemoryExtdelegatecall(t *testing.T) {
	stack := NewStack()
	// Push in reverse order: input_size, input_offset, target_address
	// Back(0)=target, Back(1)=input_offset, Back(2)=input_size
	stack.Push(new(big.Int).SetUint64(100)) // input_size (Back(2))
	stack.Push(new(big.Int).SetUint64(32))  // input_offset (Back(1))
	stack.Push(new(big.Int).SetUint64(0))   // target_address (Back(0))

	size, overflow := memoryExtdelegatecall(stack)
	if overflow {
		t.Fatalf("memoryExtdelegatecall overflowed unexpectedly")
	}
	if size != 132 { // 32 + 100
		t.Fatalf("memoryExtdelegatecall = %d, want 132", size)
	}
}

func TestMemoryDatacopy(t *testing.T) {
	stack := NewStack()
	// Push in reverse order: size, data_offset, mem_offset
	// Back(0)=mem_offset, Back(1)=data_offset, Back(2)=size
	stack.Push(new(big.Int).SetUint64(50)) // size (Back(2))
	stack.Push(new(big.Int).SetUint64(0))  // data_offset (Back(1))
	stack.Push(new(big.Int).SetUint64(10)) // mem_offset (Back(0))

	size, overflow := memoryDatacopy(stack)
	if overflow {
		t.Fatalf("memoryDatacopy overflowed unexpectedly")
	}
	if size != 60 { // 10 + 50
		t.Fatalf("memoryDatacopy = %d, want 60", size)
	}
}

func TestMemoryDatacopy_ZeroSize(t *testing.T) {
	stack := NewStack()
	// Push in reverse order: size, data_offset, mem_offset
	stack.Push(new(big.Int).SetUint64(0))  // size = 0 (Back(2))
	stack.Push(new(big.Int).SetUint64(0))  // data_offset (Back(1))
	stack.Push(new(big.Int).SetUint64(10)) // mem_offset (Back(0))

	size, overflow := memoryDatacopy(stack)
	if overflow {
		t.Fatalf("memoryDatacopy overflowed unexpectedly")
	}
	if size != 0 {
		t.Fatalf("memoryDatacopy zero size = %d, want 0", size)
	}
}

// --- EXT*CALL gas constant validation ---

func TestExtCallConstants(t *testing.T) {
	if MinRetainedGas != 5000 {
		t.Fatalf("MinRetainedGas = %d, want 5000", MinRetainedGas)
	}
	if MinCalleeGas != 2300 {
		t.Fatalf("MinCalleeGas = %d, want 2300", MinCalleeGas)
	}
	if ExtCallSuccess != 0 {
		t.Fatalf("ExtCallSuccess = %d, want 0", ExtCallSuccess)
	}
	if ExtCallRevert != 1 {
		t.Fatalf("ExtCallRevert = %d, want 1", ExtCallRevert)
	}
	if ExtCallFailure != 2 {
		t.Fatalf("ExtCallFailure = %d, want 2", ExtCallFailure)
	}
}

// --- Extcall with low gas (MinCalleeGas check) ---

func TestExtcall_LowGas(t *testing.T) {
	evm := newEOFTestEVM()

	// Give the contract very little gas so that after deductions,
	// available callee gas < MIN_CALLEE_GAS.
	code := []byte{
		byte(PUSH1), 0x00, // value
		byte(PUSH1), 0x00, // input_size
		byte(PUSH1), 0x00, // input_offset
		byte(PUSH1), 0x02, // target
		byte(EXTCALL),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(types.Address{0x01}, types.Address{0x01}, nil, 200)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("EXTCALL low gas: %v", err)
	}
	result := new(big.Int).SetBytes(ret)
	// With only 200 gas, after constant gas (100) the available gas is very low.
	// min_retained = max(gas/64, 5000) = 5000, so available = gas - 5000 = negative -> 0
	// Since 0 < MIN_CALLEE_GAS (2300), should return status 1 (revert).
	if result.Uint64() != ExtCallRevert {
		t.Fatalf("EXTCALL low gas status = %d, want %d (revert)", result.Uint64(), ExtCallRevert)
	}
}
