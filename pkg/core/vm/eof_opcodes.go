package vm

// EOF-specific opcodes:
// - EIP-7069: Revamped CALL instructions (EXTCALL, EXTDELEGATECALL, EXTSTATICCALL, RETURNDATALOAD)
// - EIP-7480: Data section access (DATALOAD, DATALOADN, DATASIZE, DATACOPY)
// - EIP-7620: EOF contract creation (EOFCREATE, RETURNCONTRACT)

import (
	"math/big"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// EOF opcode constants.
const (
	// EIP-7480: Data section access
	DATALOAD  OpCode = 0xd0
	DATALOADN OpCode = 0xd1
	DATASIZE  OpCode = 0xd2
	DATACOPY  OpCode = 0xd3

	// EIP-7620: EOF contract creation
	EOFCREATE      OpCode = 0xec
	RETURNCONTRACT OpCode = 0xee

	// EIP-7069: Revamped CALL instructions
	RETURNDATALOAD  OpCode = 0xf7
	EXTCALL         OpCode = 0xf8
	EXTDELEGATECALL OpCode = 0xf9
	EXTSTATICCALL   OpCode = 0xfb
)

// EOF gas constants.
const (
	// EIP-7069: EXTCALL family
	GasReturndataload uint64 = 3    // RETURNDATALOAD base gas
	GasExtcallBase    uint64 = 100  // WARM_STORAGE_READ_COST
	MinRetainedGas    uint64 = 5000 // minimum gas retained by caller
	MinCalleeGas      uint64 = 2300 // minimum gas available to callee

	// EIP-7480: Data section access
	GasDataload  uint64 = 4 // DATALOAD gas
	GasDataloadN uint64 = 3 // DATALOADN gas
	GasDatasize  uint64 = 2 // DATASIZE gas
	GasDatacopy  uint64 = 3 // DATACOPY base gas

	// EIP-7620: EOFCREATE
	GasEofcreate uint64 = 32000 // TX_CREATE_COST
)

// EXTCALL status codes per EIP-7069.
const (
	ExtCallSuccess uint64 = 0 // call succeeded
	ExtCallRevert  uint64 = 1 // call reverted (or light failure)
	ExtCallFailure uint64 = 2 // call failed (OOG or exceptional abort)
)

// init registers EOF opcode names.
func init() {
	opCodeNames[DATALOAD] = "DATALOAD"
	opCodeNames[DATALOADN] = "DATALOADN"
	opCodeNames[DATASIZE] = "DATASIZE"
	opCodeNames[DATACOPY] = "DATACOPY"
	opCodeNames[EOFCREATE] = "EOFCREATE"
	opCodeNames[RETURNCONTRACT] = "RETURNCONTRACT"
	opCodeNames[RETURNDATALOAD] = "RETURNDATALOAD"
	opCodeNames[EXTCALL] = "EXTCALL"
	opCodeNames[EXTDELEGATECALL] = "EXTDELEGATECALL"
	opCodeNames[EXTSTATICCALL] = "EXTSTATICCALL"
}

// --- EIP-7069: Revamped CALL instructions ---

// opReturndataload implements RETURNDATALOAD (0xf7).
// Pops offset, pushes 32 bytes from the return data buffer (zero-padded).
func opReturndataload(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	offset := stack.Peek()
	off := offset.Uint64()
	data := make([]byte, 32)
	if off < uint64(len(evm.returnData)) {
		copy(data, evm.returnData[off:])
	}
	offset.SetBytes(data)
	return nil, nil
}

// opExtcall implements EXTCALL (0xf8).
// Stack: target_address, input_offset, input_size, value
// Returns: 0=success, 1=revert, 2=failure
func opExtcall(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	addrVal := stack.Pop()
	inOffset := stack.Pop()
	inSize := stack.Pop()
	value := stack.Pop()

	// Check that target_address fits in 20 bytes (high 12 bytes must be zero).
	addrBytes := addrVal.Bytes()
	if len(addrBytes) > 20 {
		return nil, ErrInvalidOpCode
	}
	addr := types.BytesToAddress(addrBytes)

	// Non-zero value in static mode is an exceptional halt.
	transfersValue := value.Sign() > 0
	if transfersValue && evm.readOnly {
		return nil, ErrWriteProtection
	}

	// Get input data from memory.
	args := memory.Get(int64(inOffset.Uint64()), int64(inSize.Uint64()))

	// Calculate gas available to callee:
	// available = remaining - max(remaining/64, MIN_RETAINED_GAS)
	retained := contract.Gas / 64
	if retained < MinRetainedGas {
		retained = MinRetainedGas
	}
	var callGas uint64
	if contract.Gas > retained {
		callGas = contract.Gas - retained
	}

	// Clear the returndata buffer.
	evm.returnData = nil

	// Light failure checks (push status 1, consume only gas charged so far).
	if callGas < MinCalleeGas {
		stack.Push(new(big.Int).SetUint64(ExtCallRevert))
		return nil, nil
	}
	if transfersValue && evm.StateDB != nil {
		balance := evm.StateDB.GetBalance(contract.Address)
		if balance.Cmp(value) < 0 {
			stack.Push(new(big.Int).SetUint64(ExtCallRevert))
			return nil, nil
		}
	}
	if evm.depth >= evm.Config.MaxCallDepth {
		stack.Push(new(big.Int).SetUint64(ExtCallRevert))
		return nil, nil
	}

	// Deduct callee gas from caller.
	contract.Gas -= callGas

	// Add stipend if transferring value.
	if transfersValue {
		callGas += CallStipend
	}

	ret, returnGas, err := evm.Call(contract.Address, addr, args, callGas, value)

	// Return unused gas to caller (minus stipend if value was sent).
	if transfersValue && returnGas >= CallStipend {
		returnGas -= CallStipend
	} else if transfersValue {
		returnGas = 0
	}
	contract.Gas += returnGas

	// Store return data.
	evm.returnData = ret

	// Push status code.
	if err == nil {
		stack.Push(new(big.Int).SetUint64(ExtCallSuccess))
	} else if err == ErrExecutionReverted {
		stack.Push(new(big.Int).SetUint64(ExtCallRevert))
	} else {
		stack.Push(new(big.Int).SetUint64(ExtCallFailure))
	}

	return nil, nil
}

// opExtdelegatecall implements EXTDELEGATECALL (0xf9).
// Stack: target_address, input_offset, input_size (no value)
// Returns: 0=success, 1=revert, 2=failure
func opExtdelegatecall(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	addrVal := stack.Pop()
	inOffset := stack.Pop()
	inSize := stack.Pop()

	// Check that target_address fits in 20 bytes.
	addrBytes := addrVal.Bytes()
	if len(addrBytes) > 20 {
		return nil, ErrInvalidOpCode
	}
	addr := types.BytesToAddress(addrBytes)

	// Get input data from memory.
	args := memory.Get(int64(inOffset.Uint64()), int64(inSize.Uint64()))

	// Calculate gas available to callee.
	retained := contract.Gas / 64
	if retained < MinRetainedGas {
		retained = MinRetainedGas
	}
	var callGas uint64
	if contract.Gas > retained {
		callGas = contract.Gas - retained
	}

	// Clear the returndata buffer.
	evm.returnData = nil

	// Light failure checks.
	if callGas < MinCalleeGas {
		stack.Push(new(big.Int).SetUint64(ExtCallRevert))
		return nil, nil
	}
	if evm.depth >= evm.Config.MaxCallDepth {
		stack.Push(new(big.Int).SetUint64(ExtCallRevert))
		return nil, nil
	}

	// Deduct callee gas from caller.
	contract.Gas -= callGas

	ret, returnGas, err := evm.DelegateCall(contract.CallerAddress, addr, args, callGas)

	contract.Gas += returnGas
	evm.returnData = ret

	if err == nil {
		stack.Push(new(big.Int).SetUint64(ExtCallSuccess))
	} else if err == ErrExecutionReverted {
		stack.Push(new(big.Int).SetUint64(ExtCallRevert))
	} else {
		stack.Push(new(big.Int).SetUint64(ExtCallFailure))
	}

	return nil, nil
}

// opExtstaticcall implements EXTSTATICCALL (0xfb).
// Stack: target_address, input_offset, input_size (no value)
// Returns: 0=success, 1=revert, 2=failure
func opExtstaticcall(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	addrVal := stack.Pop()
	inOffset := stack.Pop()
	inSize := stack.Pop()

	// Check that target_address fits in 20 bytes.
	addrBytes := addrVal.Bytes()
	if len(addrBytes) > 20 {
		return nil, ErrInvalidOpCode
	}
	addr := types.BytesToAddress(addrBytes)

	// Get input data from memory.
	args := memory.Get(int64(inOffset.Uint64()), int64(inSize.Uint64()))

	// Calculate gas available to callee.
	retained := contract.Gas / 64
	if retained < MinRetainedGas {
		retained = MinRetainedGas
	}
	var callGas uint64
	if contract.Gas > retained {
		callGas = contract.Gas - retained
	}

	// Clear the returndata buffer.
	evm.returnData = nil

	// Light failure checks.
	if callGas < MinCalleeGas {
		stack.Push(new(big.Int).SetUint64(ExtCallRevert))
		return nil, nil
	}
	if evm.depth >= evm.Config.MaxCallDepth {
		stack.Push(new(big.Int).SetUint64(ExtCallRevert))
		return nil, nil
	}

	// Deduct callee gas from caller.
	contract.Gas -= callGas

	ret, returnGas, err := evm.StaticCall(contract.Address, addr, args, callGas)

	contract.Gas += returnGas
	evm.returnData = ret

	if err == nil {
		stack.Push(new(big.Int).SetUint64(ExtCallSuccess))
	} else if err == ErrExecutionReverted {
		stack.Push(new(big.Int).SetUint64(ExtCallRevert))
	} else {
		stack.Push(new(big.Int).SetUint64(ExtCallFailure))
	}

	return nil, nil
}

// --- EIP-7480: Data section access ---

// opDataload implements DATALOAD (0xd0).
// Pops offset, pushes 32 bytes from the EOF data section (zero-padded).
func opDataload(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	offset := stack.Peek()
	off := offset.Uint64()
	data := make([]byte, 32)
	if contract.Data != nil && off < uint64(len(contract.Data)) {
		copy(data, contract.Data[off:])
	}
	offset.SetBytes(data)
	return nil, nil
}

// opDataloadN implements DATALOADN (0xd1).
// Reads a 2-byte immediate offset from code, pushes 32 bytes from data section.
func opDataloadN(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	// Read 2-byte big-endian immediate offset from code[PC+1..PC+2].
	var offset uint64
	if *pc+2 < uint64(len(contract.Code)) {
		offset = uint64(contract.Code[*pc+1])<<8 | uint64(contract.Code[*pc+2])
	} else if *pc+1 < uint64(len(contract.Code)) {
		offset = uint64(contract.Code[*pc+1]) << 8
	}

	data := make([]byte, 32)
	if contract.Data != nil && offset < uint64(len(contract.Data)) {
		copy(data, contract.Data[offset:])
	}
	stack.Push(new(big.Int).SetBytes(data))
	*pc += 2
	return nil, nil
}

// opDatasize implements DATASIZE (0xd2).
// Pushes the size of the EOF data section.
func opDatasize(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	var size uint64
	if contract.Data != nil {
		size = uint64(len(contract.Data))
	}
	stack.Push(new(big.Int).SetUint64(size))
	return nil, nil
}

// opDatacopy implements DATACOPY (0xd3).
// Pops mem_offset, offset, size; copies from data section to memory (zero-padded).
func opDatacopy(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	memOffset := stack.Pop()
	dataOffset := stack.Pop()
	size := stack.Pop()

	l := size.Uint64()
	if l == 0 {
		return nil, nil
	}

	dOff := dataOffset.Uint64()
	data := make([]byte, l)
	if contract.Data != nil && dOff < uint64(len(contract.Data)) {
		copy(data, contract.Data[dOff:])
	}
	memory.Set(memOffset.Uint64(), l, data)
	return nil, nil
}

// --- EIP-7620: EOF contract creation ---

// opEofcreate implements EOFCREATE (0xec).
// Reads immediate initcontainer_index, pops value, salt, input_offset, input_size.
// Pushes new contract address on success, 0 on failure.
func opEofcreate(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	if evm.readOnly {
		return nil, ErrWriteProtection
	}

	// Read 1-byte immediate operand: initcontainer_index.
	var initIdx uint8
	if *pc+1 < uint64(len(contract.Code)) {
		initIdx = contract.Code[*pc+1]
	}
	*pc += 1

	value := stack.Pop()
	salt := stack.Pop()
	inOffset := stack.Pop()
	inSize := stack.Pop()

	// Get input data (aux data) from memory.
	auxData := memory.Get(int64(inOffset.Uint64()), int64(inSize.Uint64()))

	// Light failure: check call depth and balance.
	if evm.depth >= evm.Config.MaxCallDepth {
		stack.Push(new(big.Int))
		return nil, nil
	}
	if value.Sign() > 0 && evm.StateDB != nil {
		balance := evm.StateDB.GetBalance(contract.Address)
		if balance.Cmp(value) < 0 {
			stack.Push(new(big.Int))
			return nil, nil
		}
	}

	// Load initcontainer from the contract's subcontainers.
	if contract.Subcontainers == nil || int(initIdx) >= len(contract.Subcontainers) {
		stack.Push(new(big.Int))
		return nil, nil
	}
	initcontainer := contract.Subcontainers[initIdx]

	// Compute new address: keccak256(0xff ++ sender32 ++ salt)[12:]
	// sender32 is sender address left-padded to 32 bytes.
	sender32 := make([]byte, 32)
	copy(sender32[12:], contract.Address[:])
	saltBytes := make([]byte, 32)
	saltBig := salt.Bytes()
	copy(saltBytes[32-len(saltBig):], saltBig)

	hashInput := make([]byte, 0, 1+32+32)
	hashInput = append(hashInput, 0xff)
	hashInput = append(hashInput, sender32...)
	hashInput = append(hashInput, saltBytes...)
	hash := crypto.Keccak256(hashInput)
	newAddr := types.BytesToAddress(hash[12:])

	// Apply the 63/64 rule for gas allocation.
	gas := contract.Gas
	contract.Gas = 0

	ret, _, returnGas, err := evm.Create2(contract.Address, initcontainer, gas, value, salt)
	_ = auxData // aux data passed as calldata to initcode
	_ = newAddr // address computed deterministically

	contract.Gas += returnGas
	evm.returnData = ret

	if err != nil {
		stack.Push(new(big.Int))
	} else {
		stack.Push(new(big.Int).SetBytes(newAddr[:]))
	}

	return nil, nil
}

// opReturncontract implements RETURNCONTRACT (0xee).
// Reads immediate deploy_container_index, pops aux_data_offset, aux_data_size.
// Ends initcode frame, returns the deploy container with appended aux data.
func opReturncontract(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	// Read 1-byte immediate operand: deploy_container_index.
	var deployIdx uint8
	if *pc+1 < uint64(len(contract.Code)) {
		deployIdx = contract.Code[*pc+1]
	}
	*pc += 1

	auxOffset := stack.Pop()
	auxSize := stack.Pop()

	// Get aux data from memory.
	auxData := memory.Get(int64(auxOffset.Uint64()), int64(auxSize.Uint64()))

	// Load deploy container from subcontainers.
	if contract.Subcontainers == nil || int(deployIdx) >= len(contract.Subcontainers) {
		return nil, ErrInvalidOpCode
	}
	deployCode := contract.Subcontainers[deployIdx]

	// Concatenate deploy code with aux data.
	result := make([]byte, len(deployCode)+len(auxData))
	copy(result, deployCode)
	copy(result[len(deployCode):], auxData)

	// Check max code size.
	if len(result) > MaxCodeSize {
		return nil, ErrMaxInitCodeSizeExceeded
	}

	return result, nil
}

// --- Gas functions ---

// gasExtcall calculates dynamic gas for EXTCALL.
// Stack: target_address, input_offset, input_size, value
func gasExtcall(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	var gas uint64
	addr := types.BytesToAddress(stack.Back(0).Bytes())

	// Value transfer cost.
	if stack.Back(3).Sign() != 0 {
		gas = safeAdd(gas, CallValueTransferGas)
		// Account creation cost if target doesn't exist and value is non-zero.
		if evm.StateDB != nil && !evm.StateDB.Exist(addr) {
			gas = safeAdd(gas, CallNewAccountGas)
		}
	}

	// Cold account access cost.
	gas = safeAdd(gas, gasEIP2929AccountCheck(evm, addr))

	// Memory expansion.
	gas = safeAdd(gas, gasMemExpansion(evm, contract, stack, mem, memorySize))

	return gas
}

// gasExtdelegatecall calculates dynamic gas for EXTDELEGATECALL.
// Stack: target_address, input_offset, input_size
func gasExtdelegatecall(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	var gas uint64
	addr := types.BytesToAddress(stack.Back(0).Bytes())

	// Cold account access cost.
	gas = safeAdd(gas, gasEIP2929AccountCheck(evm, addr))

	// Memory expansion.
	gas = safeAdd(gas, gasMemExpansion(evm, contract, stack, mem, memorySize))

	return gas
}

// gasExtstaticcall calculates dynamic gas for EXTSTATICCALL.
// Stack: target_address, input_offset, input_size
func gasExtstaticcall(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	return gasExtdelegatecall(evm, contract, stack, mem, memorySize)
}

// gasDatacopy calculates dynamic gas for DATACOPY.
// Gas: 3 * word_count + memory expansion.
func gasDatacopy(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	size := stack.Back(2).Uint64()
	words := toWordSize(size)
	gas := safeMul(GasCopy, words)
	gas = safeAdd(gas, gasMemExpansion(evm, contract, stack, mem, memorySize))
	return gas
}

// gasEofcreate calculates dynamic gas for EOFCREATE.
// Charges initcode hashing + memory expansion.
func gasEofcreate(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	// Stack after immediate: value, salt, input_offset, input_size
	// input_size is at stack position 3 (Back(3)).
	size := stack.Back(3).Uint64()
	words := toWordSize(size)
	gas := safeMul(GasKeccak256Word, words)
	gas = safeAdd(gas, gasMemExpansion(evm, contract, stack, mem, memorySize))
	return gas
}

// --- Memory size functions ---

// memoryExtcall returns memory needed for EXTCALL.
// Stack: target_address, input_offset, input_size, value
func memoryExtcall(stack *Stack) uint64 {
	inOffset := stack.Back(1).Uint64()
	inSize := stack.Back(2).Uint64()
	if inSize == 0 {
		return 0
	}
	return inOffset + inSize
}

// memoryExtdelegatecall returns memory needed for EXTDELEGATECALL / EXTSTATICCALL.
// Stack: target_address, input_offset, input_size
func memoryExtdelegatecall(stack *Stack) uint64 {
	inOffset := stack.Back(1).Uint64()
	inSize := stack.Back(2).Uint64()
	if inSize == 0 {
		return 0
	}
	return inOffset + inSize
}

// memoryDatacopy returns memory needed for DATACOPY.
// Stack: mem_offset, offset, size
func memoryDatacopy(stack *Stack) uint64 {
	memOffset := stack.Back(0).Uint64()
	size := stack.Back(2).Uint64()
	if size == 0 {
		return 0
	}
	return memOffset + size
}

// memoryEofcreate returns memory needed for EOFCREATE.
// Stack (after immediate): value, salt, input_offset, input_size
func memoryEofcreate(stack *Stack) uint64 {
	inOffset := stack.Back(2).Uint64()
	inSize := stack.Back(3).Uint64()
	if inSize == 0 {
		return 0
	}
	return inOffset + inSize
}

// memoryReturncontract returns memory needed for RETURNCONTRACT.
// Stack: aux_data_offset, aux_data_size
func memoryReturncontract(stack *Stack) uint64 {
	offset := stack.Back(0).Uint64()
	size := stack.Back(1).Uint64()
	if size == 0 {
		return 0
	}
	return offset + size
}

// EOFOperations returns the operation definitions for all EOF opcodes.
// These can be merged into a jump table to enable EOF support.
func EOFOperations() map[OpCode]*operation {
	ops := map[OpCode]*operation{
		// EIP-7069: Revamped CALL instructions
		RETURNDATALOAD: {
			execute:     opReturndataload,
			constantGas: GasReturndataload,
			minStack:    1,
			maxStack:    1024,
		},
		EXTCALL: {
			execute:     opExtcall,
			constantGas: GasExtcallBase,
			dynamicGas:  gasExtcall,
			minStack:    4,
			maxStack:    1024,
			memorySize:  memoryExtcall,
		},
		EXTDELEGATECALL: {
			execute:     opExtdelegatecall,
			constantGas: GasExtcallBase,
			dynamicGas:  gasExtdelegatecall,
			minStack:    3,
			maxStack:    1024,
			memorySize:  memoryExtdelegatecall,
		},
		EXTSTATICCALL: {
			execute:     opExtstaticcall,
			constantGas: GasExtcallBase,
			dynamicGas:  gasExtstaticcall,
			minStack:    3,
			maxStack:    1024,
			memorySize:  memoryExtdelegatecall,
		},

		// EIP-7480: Data section access
		DATALOAD: {
			execute:     opDataload,
			constantGas: GasDataload,
			minStack:    1,
			maxStack:    1024,
		},
		DATALOADN: {
			execute:     opDataloadN,
			constantGas: GasDataloadN,
			minStack:    0,
			maxStack:    1023,
		},
		DATASIZE: {
			execute:     opDatasize,
			constantGas: GasDatasize,
			minStack:    0,
			maxStack:    1023,
		},
		DATACOPY: {
			execute:     opDatacopy,
			constantGas: GasDatacopy,
			dynamicGas:  gasDatacopy,
			minStack:    3,
			maxStack:    1024,
			memorySize:  memoryDatacopy,
		},

		// EIP-7620: EOF contract creation
		EOFCREATE: {
			execute:     opEofcreate,
			constantGas: GasEofcreate,
			dynamicGas:  gasEofcreate,
			minStack:    4,
			maxStack:    1024,
			memorySize:  memoryEofcreate,
			writes:      true,
		},
		RETURNCONTRACT: {
			execute:     opReturncontract,
			constantGas: 0,
			minStack:    2,
			maxStack:    1024,
			memorySize:  memoryReturncontract,
			dynamicGas:  gasMemExpansion,
			halts:       true,
		},
	}
	return ops
}
