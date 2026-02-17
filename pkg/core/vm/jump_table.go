package vm

// dynamicGasFunc calculates dynamic gas cost for an operation.
type dynamicGasFunc func(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64

// memorySizeFunc returns the required memory size for an operation.
type memorySizeFunc func(stack *Stack) uint64

// operation represents a single EVM opcode's execution metadata.
type operation struct {
	execute     executionFunc
	constantGas uint64
	dynamicGas  dynamicGasFunc
	minStack    int // minimum stack items required
	maxStack    int // maximum stack items allowed (1024 - net stack items pushed)
	memorySize  memorySizeFunc
	halts       bool // whether this opcode halts execution (STOP, RETURN, SELFDESTRUCT)
	jumps       bool // whether this opcode performs a jump (JUMP, JUMPI)
	writes      bool // whether this opcode modifies state (SSTORE, LOG, CREATE, etc.)
}

// JumpTable maps every possible opcode to its operation definition.
type JumpTable [256]*operation

// Memory size functions for operations that access memory.

func memoryMload(stack *Stack) uint64 {
	return stack.Back(0).Uint64() + 32
}

func memoryMstore(stack *Stack) uint64 {
	return stack.Back(0).Uint64() + 32
}

func memoryMstore8(stack *Stack) uint64 {
	return stack.Back(0).Uint64() + 1
}

func memoryReturn(stack *Stack) uint64 {
	return stack.Back(0).Uint64() + stack.Back(1).Uint64()
}

func memoryKeccak256(stack *Stack) uint64 {
	return stack.Back(0).Uint64() + stack.Back(1).Uint64()
}

func memoryCalldataCopy(stack *Stack) uint64 {
	return stack.Back(0).Uint64() + stack.Back(2).Uint64()
}

func memoryCodeCopy(stack *Stack) uint64 {
	return stack.Back(0).Uint64() + stack.Back(2).Uint64()
}

func memoryLog(stack *Stack) uint64 {
	return stack.Back(0).Uint64() + stack.Back(1).Uint64()
}

func memoryReturndataCopy(stack *Stack) uint64 {
	return stack.Back(0).Uint64() + stack.Back(2).Uint64()
}

// memoryCall returns the required memory size for CALL.
// Stack: gas, addr, value, argsOffset, argsLength, retOffset, retLength
func memoryCall(stack *Stack) uint64 {
	argsEnd := stack.Back(3).Uint64() + stack.Back(4).Uint64()
	retEnd := stack.Back(5).Uint64() + stack.Back(6).Uint64()
	if argsEnd > retEnd {
		return argsEnd
	}
	return retEnd
}

// memoryCallCode returns the required memory size for CALLCODE.
// Same stack layout as CALL.
func memoryCallCode(stack *Stack) uint64 {
	return memoryCall(stack)
}

// memoryDelegateCall returns the required memory size for DELEGATECALL.
// Stack: gas, addr, argsOffset, argsLength, retOffset, retLength (no value)
func memoryDelegateCall(stack *Stack) uint64 {
	argsEnd := stack.Back(2).Uint64() + stack.Back(3).Uint64()
	retEnd := stack.Back(4).Uint64() + stack.Back(5).Uint64()
	if argsEnd > retEnd {
		return argsEnd
	}
	return retEnd
}

// memoryStaticCall returns the required memory size for STATICCALL.
// Same stack layout as DELEGATECALL.
func memoryStaticCall(stack *Stack) uint64 {
	return memoryDelegateCall(stack)
}

// memoryCreate returns the required memory size for CREATE.
// Stack: value, offset, length
func memoryCreate(stack *Stack) uint64 {
	return stack.Back(1).Uint64() + stack.Back(2).Uint64()
}

// memoryCreate2 returns the required memory size for CREATE2.
// Stack: value, offset, length, salt
func memoryCreate2(stack *Stack) uint64 {
	return stack.Back(1).Uint64() + stack.Back(2).Uint64()
}

// gasMemExpansion calculates dynamic gas for memory expansion.
func gasMemExpansion(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	if memorySize == 0 {
		return 0
	}
	words := (memorySize + 31) / 32
	newCost := words*GasMemory + (words*words)/512
	if uint64(mem.Len()) == 0 {
		return newCost
	}
	oldWords := (uint64(mem.Len()) + 31) / 32
	oldCost := oldWords*GasMemory + (oldWords*oldWords)/512
	if newCost > oldCost {
		return newCost - oldCost
	}
	return 0
}

// NewFrontierJumpTable returns the Frontier (genesis) jump table.
func NewFrontierJumpTable() JumpTable {
	var tbl JumpTable

	// Arithmetic
	tbl[STOP] = &operation{execute: opStop, constantGas: GasStop, minStack: 0, maxStack: 1024, halts: true}
	tbl[ADD] = &operation{execute: opAdd, constantGas: GasQuickStep, minStack: 2, maxStack: 1024}
	tbl[MUL] = &operation{execute: opMul, constantGas: GasFastestStep, minStack: 2, maxStack: 1024}
	tbl[SUB] = &operation{execute: opSub, constantGas: GasQuickStep, minStack: 2, maxStack: 1024}
	tbl[DIV] = &operation{execute: opDiv, constantGas: GasFastStep, minStack: 2, maxStack: 1024}
	tbl[SDIV] = &operation{execute: opSdiv, constantGas: GasFastStep, minStack: 2, maxStack: 1024}
	tbl[MOD] = &operation{execute: opMod, constantGas: GasFastStep, minStack: 2, maxStack: 1024}
	tbl[SMOD] = &operation{execute: opSmod, constantGas: GasFastStep, minStack: 2, maxStack: 1024}
	tbl[ADDMOD] = &operation{execute: opAddmod, constantGas: GasMidStep, minStack: 3, maxStack: 1024}
	tbl[MULMOD] = &operation{execute: opMulmod, constantGas: GasMidStep, minStack: 3, maxStack: 1024}
	tbl[EXP] = &operation{execute: opExp, constantGas: GasSlowStep, minStack: 2, maxStack: 1024}
	tbl[SIGNEXTEND] = &operation{execute: opSignExtend, constantGas: GasFastStep, minStack: 2, maxStack: 1024}

	// Comparison
	tbl[LT] = &operation{execute: opLt, constantGas: GasQuickStep, minStack: 2, maxStack: 1024}
	tbl[GT] = &operation{execute: opGt, constantGas: GasQuickStep, minStack: 2, maxStack: 1024}
	tbl[SLT] = &operation{execute: opSlt, constantGas: GasQuickStep, minStack: 2, maxStack: 1024}
	tbl[SGT] = &operation{execute: opSgt, constantGas: GasQuickStep, minStack: 2, maxStack: 1024}
	tbl[EQ] = &operation{execute: opEq, constantGas: GasQuickStep, minStack: 2, maxStack: 1024}
	tbl[ISZERO] = &operation{execute: opIsZero, constantGas: GasQuickStep, minStack: 1, maxStack: 1024}

	// Bitwise
	tbl[AND] = &operation{execute: opAnd, constantGas: GasQuickStep, minStack: 2, maxStack: 1024}
	tbl[OR] = &operation{execute: opOr, constantGas: GasQuickStep, minStack: 2, maxStack: 1024}
	tbl[XOR] = &operation{execute: opXor, constantGas: GasQuickStep, minStack: 2, maxStack: 1024}
	tbl[NOT] = &operation{execute: opNot, constantGas: GasQuickStep, minStack: 1, maxStack: 1024}
	tbl[BYTE] = &operation{execute: opByte, constantGas: GasQuickStep, minStack: 2, maxStack: 1024}

	// Environment
	tbl[ADDRESS] = &operation{execute: opAddress, constantGas: GasQuickStep, minStack: 0, maxStack: 1023}
	tbl[ORIGIN] = &operation{execute: opOrigin, constantGas: GasQuickStep, minStack: 0, maxStack: 1023}
	tbl[CALLER] = &operation{execute: opCaller, constantGas: GasQuickStep, minStack: 0, maxStack: 1023}
	tbl[CALLVALUE] = &operation{execute: opCallValue, constantGas: GasQuickStep, minStack: 0, maxStack: 1023}
	tbl[CALLDATALOAD] = &operation{execute: opCalldataLoad, constantGas: GasQuickStep, minStack: 1, maxStack: 1024}
	tbl[CALLDATASIZE] = &operation{execute: opCalldataSize, constantGas: GasQuickStep, minStack: 0, maxStack: 1023}
	tbl[CALLDATACOPY] = &operation{execute: opCalldataCopy, constantGas: GasQuickStep, minStack: 3, maxStack: 1024, memorySize: memoryCalldataCopy, dynamicGas: gasMemExpansion}
	tbl[CODESIZE] = &operation{execute: opCodeSize, constantGas: GasQuickStep, minStack: 0, maxStack: 1023}
	tbl[CODECOPY] = &operation{execute: opCodeCopy, constantGas: GasQuickStep, minStack: 3, maxStack: 1024, memorySize: memoryCodeCopy, dynamicGas: gasMemExpansion}
	tbl[GASPRICE] = &operation{execute: opGasPrice, constantGas: GasQuickStep, minStack: 0, maxStack: 1023}

	// Block
	tbl[COINBASE] = &operation{execute: opCoinbase, constantGas: GasQuickStep, minStack: 0, maxStack: 1023}
	tbl[TIMESTAMP] = &operation{execute: opTimestamp, constantGas: GasQuickStep, minStack: 0, maxStack: 1023}
	tbl[NUMBER] = &operation{execute: opNumber, constantGas: GasQuickStep, minStack: 0, maxStack: 1023}
	tbl[PREVRANDAO] = &operation{execute: opPrevRandao, constantGas: GasQuickStep, minStack: 0, maxStack: 1023}
	tbl[GASLIMIT] = &operation{execute: opGasLimit, constantGas: GasQuickStep, minStack: 0, maxStack: 1023}

	// Stack, memory, flow
	tbl[POP] = &operation{execute: opPop, constantGas: GasPop, minStack: 1, maxStack: 1024}
	tbl[MLOAD] = &operation{execute: opMload, constantGas: GasMload, minStack: 1, maxStack: 1024, memorySize: memoryMload, dynamicGas: gasMemExpansion}
	tbl[MSTORE] = &operation{execute: opMstore, constantGas: GasMstore, minStack: 2, maxStack: 1024, memorySize: memoryMstore, dynamicGas: gasMemExpansion}
	tbl[MSTORE8] = &operation{execute: opMstore8, constantGas: GasMstore8, minStack: 2, maxStack: 1024, memorySize: memoryMstore8, dynamicGas: gasMemExpansion}
	tbl[JUMP] = &operation{execute: opJump, constantGas: GasJump, minStack: 1, maxStack: 1024, jumps: true}
	tbl[JUMPI] = &operation{execute: opJumpi, constantGas: GasJumpi, minStack: 2, maxStack: 1024, jumps: true}
	tbl[PC] = &operation{execute: opPc, constantGas: GasPc, minStack: 0, maxStack: 1023}
	tbl[MSIZE] = &operation{execute: opMsize, constantGas: GasMsize, minStack: 0, maxStack: 1023}
	tbl[GAS] = &operation{execute: opGas, constantGas: GasGas, minStack: 0, maxStack: 1023}
	tbl[JUMPDEST] = &operation{execute: opJumpdest, constantGas: GasJumpDest, minStack: 0, maxStack: 1024}

	// Push
	tbl[PUSH1] = &operation{execute: opPush1, constantGas: GasPush, minStack: 0, maxStack: 1023}
	for i := 2; i <= 32; i++ {
		tbl[PUSH1+OpCode(i-1)] = &operation{
			execute:     makePush(uint64(i)),
			constantGas: GasPush,
			minStack:    0,
			maxStack:    1023,
		}
	}

	// Dup
	for i := 1; i <= 16; i++ {
		tbl[DUP1+OpCode(i-1)] = &operation{
			execute:     makeDup(i),
			constantGas: GasDup,
			minStack:    i,
			maxStack:    1023,
		}
	}

	// Swap
	for i := 1; i <= 16; i++ {
		tbl[SWAP1+OpCode(i-1)] = &operation{
			execute:     makeSwap(i),
			constantGas: GasSwap,
			minStack:    i + 1,
			maxStack:    1024,
		}
	}

	// Hash
	tbl[KECCAK256] = &operation{execute: opKeccak256, constantGas: GasKeccak256, minStack: 2, maxStack: 1024, memorySize: memoryKeccak256, dynamicGas: gasMemExpansion}

	// State (stubs)
	tbl[BALANCE] = &operation{execute: opBalance, constantGas: GasBalanceCold, minStack: 1, maxStack: 1024}
	tbl[SLOAD] = &operation{execute: opSload, constantGas: GasSloadCold, minStack: 1, maxStack: 1024}
	tbl[SSTORE] = &operation{execute: opSstore, constantGas: GasSstoreSet, minStack: 2, maxStack: 1024, writes: true}

	// Log
	for i := 0; i <= 4; i++ {
		n := i
		tbl[LOG0+OpCode(i)] = &operation{
			execute:     makeLog(n),
			constantGas: GasLog,
			minStack:    2 + n,
			maxStack:    1024,
			writes:      true,
			memorySize:  memoryLog,
			dynamicGas:  gasMemExpansion,
		}
	}

	// Ext code
	tbl[EXTCODESIZE] = &operation{execute: opExtcodesize, constantGas: GasBalanceCold, minStack: 1, maxStack: 1024}
	tbl[EXTCODECOPY] = &operation{execute: opExtcodecopy, constantGas: GasBalanceCold, minStack: 4, maxStack: 1024}

	// CALL-family
	tbl[CALL] = &operation{execute: opCall, constantGas: GasCallCold, minStack: 7, maxStack: 1024, memorySize: memoryCall, dynamicGas: gasMemExpansion}
	tbl[CALLCODE] = &operation{execute: opCallCode, constantGas: GasCallCold, minStack: 7, maxStack: 1024, memorySize: memoryCallCode, dynamicGas: gasMemExpansion}

	// CREATE
	tbl[CREATE] = &operation{execute: opCreate, constantGas: 0, minStack: 3, maxStack: 1024, memorySize: memoryCreate, dynamicGas: gasMemExpansion, writes: true}

	// Return / Invalid
	tbl[RETURN] = &operation{execute: opReturn, constantGas: GasReturn, minStack: 2, maxStack: 1024, halts: true, memorySize: memoryReturn, dynamicGas: gasMemExpansion}
	tbl[INVALID] = &operation{execute: opInvalid, constantGas: 0, minStack: 0, maxStack: 1024}

	return tbl
}

// NewHomesteadJumpTable returns the Homestead fork jump table.
func NewHomesteadJumpTable() JumpTable {
	tbl := NewFrontierJumpTable()
	// Homestead added DELEGATECALL.
	tbl[DELEGATECALL] = &operation{execute: opDelegateCall, constantGas: GasCallCold, minStack: 6, maxStack: 1024, memorySize: memoryDelegateCall, dynamicGas: gasMemExpansion}
	return tbl
}

// NewTangerineWhistleJumpTable returns the Tangerine Whistle (EIP-150) fork jump table.
func NewTangerineWhistleJumpTable() JumpTable {
	tbl := NewHomesteadJumpTable()
	// Gas cost repricing was the main change.
	return tbl
}

// NewSpuriousDragonJumpTable returns the Spurious Dragon fork jump table.
func NewSpuriousDragonJumpTable() JumpTable {
	tbl := NewTangerineWhistleJumpTable()
	return tbl
}

// NewByzantiumJumpTable returns the Byzantium fork jump table.
func NewByzantiumJumpTable() JumpTable {
	tbl := NewSpuriousDragonJumpTable()
	// Byzantium added REVERT, STATICCALL, RETURNDATASIZE, RETURNDATACOPY.
	tbl[REVERT] = &operation{execute: opRevert, constantGas: GasRevert, minStack: 2, maxStack: 1024, halts: true, memorySize: memoryReturn, dynamicGas: gasMemExpansion}
	tbl[RETURNDATASIZE] = &operation{execute: opReturndataSize, constantGas: GasQuickStep, minStack: 0, maxStack: 1023}
	tbl[RETURNDATACOPY] = &operation{execute: opReturndataCopy, constantGas: GasQuickStep, minStack: 3, maxStack: 1024}
	return tbl
}

// NewConstantinopleJumpTable returns the Constantinople fork jump table.
func NewConstantinopleJumpTable() JumpTable {
	tbl := NewByzantiumJumpTable()
	// Constantinople added SHL, SHR, SAR, EXTCODEHASH, CREATE2.
	tbl[SHL] = &operation{execute: opSHL, constantGas: GasQuickStep, minStack: 2, maxStack: 1024}
	tbl[SHR] = &operation{execute: opSHR, constantGas: GasQuickStep, minStack: 2, maxStack: 1024}
	tbl[SAR] = &operation{execute: opSAR, constantGas: GasQuickStep, minStack: 2, maxStack: 1024}
	return tbl
}

// NewIstanbulJumpTable returns the Istanbul fork jump table.
func NewIstanbulJumpTable() JumpTable {
	tbl := NewConstantinopleJumpTable()
	// Istanbul added CHAINID and SELFBALANCE.
	tbl[CHAINID] = &operation{execute: opChainID, constantGas: GasQuickStep, minStack: 0, maxStack: 1023}
	tbl[SELFBALANCE] = &operation{execute: opSelfBalance, constantGas: GasFastStep, minStack: 0, maxStack: 1023}
	return tbl
}

// NewBerlinJumpTable returns the Berlin fork jump table.
func NewBerlinJumpTable() JumpTable {
	tbl := NewIstanbulJumpTable()
	// Berlin introduced EIP-2929 warm/cold gas accounting.
	return tbl
}

// NewLondonJumpTable returns the London fork jump table.
func NewLondonJumpTable() JumpTable {
	tbl := NewBerlinJumpTable()
	// London added BASEFEE.
	tbl[BASEFEE] = &operation{execute: opBaseFee, constantGas: GasQuickStep, minStack: 0, maxStack: 1023}
	return tbl
}

// NewMergeJumpTable returns the Merge (Paris) fork jump table.
func NewMergeJumpTable() JumpTable {
	tbl := NewLondonJumpTable()
	// PREVRANDAO replaces DIFFICULTY (same opcode slot, already mapped).
	return tbl
}

// NewShanghaiJumpTable returns the Shanghai fork jump table.
func NewShanghaiJumpTable() JumpTable {
	tbl := NewMergeJumpTable()
	// Shanghai added PUSH0.
	tbl[PUSH0] = &operation{execute: opPush0, constantGas: GasPush0, minStack: 0, maxStack: 1023}
	return tbl
}

// NewCancunJumpTable returns the Cancun fork jump table.
func NewCancunJumpTable() JumpTable {
	tbl := NewShanghaiJumpTable()
	// Cancun added TLOAD, TSTORE (EIP-1153), MCOPY (EIP-5656),
	// BLOBHASH (EIP-4844), BLOBBASEFEE (EIP-7516).
	// These are registered as stubs (no state DB yet).
	return tbl
}

// NewPragueJumpTable returns the Prague fork jump table.
func NewPragueJumpTable() JumpTable {
	tbl := NewCancunJumpTable()
	// Prague includes EIP-7702, EIP-7685, and other enhancements.
	return tbl
}
