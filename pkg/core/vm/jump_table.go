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

// memoryExtcodecopy returns the required memory size for EXTCODECOPY.
// Stack: addr, memOffset, codeOffset, length
func memoryExtcodecopy(stack *Stack) uint64 {
	return stack.Back(1).Uint64() + stack.Back(3).Uint64()
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

// memoryMcopy returns the required memory size for MCOPY.
// Stack: dest, src, size. We need max(dest+size, src+size).
func memoryMcopy(stack *Stack) uint64 {
	dest := stack.Back(0).Uint64()
	src := stack.Back(1).Uint64()
	size := stack.Back(2).Uint64()
	if size == 0 {
		return 0
	}
	destEnd := dest + size
	srcEnd := src + size
	if destEnd > srcEnd {
		return destEnd
	}
	return srcEnd
}

// gasMcopy calculates dynamic gas for MCOPY: 3 * wordSize + memory expansion.
func gasMcopy(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	size := stack.Back(2).Uint64()
	words := (size + 31) / 32
	copyGas := safeMul(GasCopy, words)
	return safeAdd(copyGas, gasMemExpansion(evm, contract, stack, mem, memorySize))
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

	// Arithmetic (Gverylow=3 for ADD, SUB, MUL; Glow=5 for DIV, SDIV, MOD, SMOD, SIGNEXTEND; Gmid=8 for ADDMOD, MULMOD)
	tbl[STOP] = &operation{execute: opStop, constantGas: GasStop, minStack: 0, maxStack: 1024, halts: true}
	tbl[ADD] = &operation{execute: opAdd, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[MUL] = &operation{execute: opMul, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[SUB] = &operation{execute: opSub, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[DIV] = &operation{execute: opDiv, constantGas: GasLow, minStack: 2, maxStack: 1024}
	tbl[SDIV] = &operation{execute: opSdiv, constantGas: GasLow, minStack: 2, maxStack: 1024}
	tbl[MOD] = &operation{execute: opMod, constantGas: GasLow, minStack: 2, maxStack: 1024}
	tbl[SMOD] = &operation{execute: opSmod, constantGas: GasLow, minStack: 2, maxStack: 1024}
	tbl[ADDMOD] = &operation{execute: opAddmod, constantGas: GasMid, minStack: 3, maxStack: 1024}
	tbl[MULMOD] = &operation{execute: opMulmod, constantGas: GasMid, minStack: 3, maxStack: 1024}
	tbl[EXP] = &operation{execute: opExp, constantGas: GasHigh, dynamicGas: gasExp, minStack: 2, maxStack: 1024}
	tbl[SIGNEXTEND] = &operation{execute: opSignExtend, constantGas: GasLow, minStack: 2, maxStack: 1024}

	// Comparison (Gverylow=3)
	tbl[LT] = &operation{execute: opLt, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[GT] = &operation{execute: opGt, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[SLT] = &operation{execute: opSlt, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[SGT] = &operation{execute: opSgt, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[EQ] = &operation{execute: opEq, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[ISZERO] = &operation{execute: opIsZero, constantGas: GasVerylow, minStack: 1, maxStack: 1024}

	// Bitwise (Gverylow=3)
	tbl[AND] = &operation{execute: opAnd, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[OR] = &operation{execute: opOr, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[XOR] = &operation{execute: opXor, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[NOT] = &operation{execute: opNot, constantGas: GasVerylow, minStack: 1, maxStack: 1024}
	tbl[BYTE] = &operation{execute: opByte, constantGas: GasVerylow, minStack: 2, maxStack: 1024}

	// Environment (Gbase=2 for info opcodes, Gverylow=3 for data access)
	tbl[ADDRESS] = &operation{execute: opAddress, constantGas: GasBase, minStack: 0, maxStack: 1023}
	tbl[ORIGIN] = &operation{execute: opOrigin, constantGas: GasBase, minStack: 0, maxStack: 1023}
	tbl[CALLER] = &operation{execute: opCaller, constantGas: GasBase, minStack: 0, maxStack: 1023}
	tbl[CALLVALUE] = &operation{execute: opCallValue, constantGas: GasBase, minStack: 0, maxStack: 1023}
	tbl[CALLDATALOAD] = &operation{execute: opCalldataLoad, constantGas: GasVerylow, minStack: 1, maxStack: 1024}
	tbl[CALLDATASIZE] = &operation{execute: opCalldataSize, constantGas: GasBase, minStack: 0, maxStack: 1023}
	tbl[CALLDATACOPY] = &operation{execute: opCalldataCopy, constantGas: GasVerylow, minStack: 3, maxStack: 1024, memorySize: memoryCalldataCopy, dynamicGas: gasCopy}
	tbl[CODESIZE] = &operation{execute: opCodeSize, constantGas: GasBase, minStack: 0, maxStack: 1023}
	tbl[CODECOPY] = &operation{execute: opCodeCopy, constantGas: GasVerylow, minStack: 3, maxStack: 1024, memorySize: memoryCodeCopy, dynamicGas: gasCopy}
	tbl[GASPRICE] = &operation{execute: opGasPrice, constantGas: GasBase, minStack: 0, maxStack: 1023}

	// Block (Gbase=2 for info opcodes, Gext=20 for BLOCKHASH)
	tbl[BLOCKHASH] = &operation{execute: opBlockhash, constantGas: GasExt, minStack: 1, maxStack: 1024}
	tbl[COINBASE] = &operation{execute: opCoinbase, constantGas: GasBase, minStack: 0, maxStack: 1023}
	tbl[TIMESTAMP] = &operation{execute: opTimestamp, constantGas: GasBase, minStack: 0, maxStack: 1023}
	tbl[NUMBER] = &operation{execute: opNumber, constantGas: GasBase, minStack: 0, maxStack: 1023}
	tbl[PREVRANDAO] = &operation{execute: opPrevRandao, constantGas: GasBase, minStack: 0, maxStack: 1023}
	tbl[GASLIMIT] = &operation{execute: opGasLimit, constantGas: GasBase, minStack: 0, maxStack: 1023}

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
	tbl[KECCAK256] = &operation{execute: opKeccak256, constantGas: GasKeccak256, minStack: 2, maxStack: 1024, memorySize: memoryKeccak256, dynamicGas: gasSha3}

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
			dynamicGas:  makeGasLog(uint64(n)),
		}
	}

	// Ext code
	tbl[EXTCODESIZE] = &operation{execute: opExtcodesize, constantGas: GasBalanceCold, minStack: 1, maxStack: 1024}
	tbl[EXTCODECOPY] = &operation{execute: opExtcodecopy, constantGas: GasBalanceCold, minStack: 4, maxStack: 1024, memorySize: memoryExtcodecopy, dynamicGas: gasExtCodeCopyCopy}

	// CALL-family
	tbl[CALL] = &operation{execute: opCall, constantGas: GasCallCold, minStack: 7, maxStack: 1024, memorySize: memoryCall, dynamicGas: gasMemExpansion}
	tbl[CALLCODE] = &operation{execute: opCallCode, constantGas: GasCallCold, minStack: 7, maxStack: 1024, memorySize: memoryCallCode, dynamicGas: gasMemExpansion}

	// CREATE
	tbl[CREATE] = &operation{execute: opCreate, constantGas: 0, minStack: 3, maxStack: 1024, memorySize: memoryCreate, dynamicGas: gasCreateDynamic, writes: true}

	// Return / Invalid / Selfdestruct
	tbl[RETURN] = &operation{execute: opReturn, constantGas: GasReturn, minStack: 2, maxStack: 1024, halts: true, memorySize: memoryReturn, dynamicGas: gasMemExpansion}
	tbl[INVALID] = &operation{execute: opInvalid, constantGas: 0, minStack: 0, maxStack: 1024}
	tbl[SELFDESTRUCT] = &operation{execute: opSelfdestruct, constantGas: GasSelfdestruct, minStack: 1, maxStack: 1024, halts: true, writes: true}

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
	tbl[RETURNDATASIZE] = &operation{execute: opReturndataSize, constantGas: GasBase, minStack: 0, maxStack: 1023}
	tbl[RETURNDATACOPY] = &operation{execute: opReturndataCopy, constantGas: GasVerylow, minStack: 3, maxStack: 1024, memorySize: memoryReturndataCopy, dynamicGas: gasCopy}
	tbl[STATICCALL] = &operation{execute: opStaticCall, constantGas: GasCallCold, minStack: 6, maxStack: 1024, memorySize: memoryStaticCall, dynamicGas: gasMemExpansion}
	return tbl
}

// NewConstantinopleJumpTable returns the Constantinople fork jump table.
func NewConstantinopleJumpTable() JumpTable {
	tbl := NewByzantiumJumpTable()
	// Constantinople added SHL, SHR, SAR, EXTCODEHASH, CREATE2.
	tbl[SHL] = &operation{execute: opSHL, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[SHR] = &operation{execute: opSHR, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[SAR] = &operation{execute: opSAR, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[EXTCODEHASH] = &operation{execute: opExtcodehash, constantGas: GasBalanceCold, minStack: 1, maxStack: 1024}
	tbl[CREATE2] = &operation{execute: opCreate2, constantGas: 0, minStack: 4, maxStack: 1024, memorySize: memoryCreate2, dynamicGas: gasCreate2Dynamic, writes: true}
	return tbl
}

// NewIstanbulJumpTable returns the Istanbul fork jump table.
func NewIstanbulJumpTable() JumpTable {
	tbl := NewConstantinopleJumpTable()
	// Istanbul added CHAINID and SELFBALANCE.
	tbl[CHAINID] = &operation{execute: opChainID, constantGas: GasBase, minStack: 0, maxStack: 1023}
	tbl[SELFBALANCE] = &operation{execute: opSelfBalance, constantGas: GasLow, minStack: 0, maxStack: 1023}
	return tbl
}

// NewBerlinJumpTable returns the Berlin fork jump table.
func NewBerlinJumpTable() JumpTable {
	tbl := NewIstanbulJumpTable()

	// EIP-2929: warm/cold gas accounting. The constant gas becomes the warm
	// cost (100), and a dynamic gas function adds the extra cold penalty
	// when the address/slot is accessed for the first time.
	tbl[SLOAD] = &operation{execute: opSload, constantGas: WarmStorageReadCost, dynamicGas: gasSloadEIP2929, minStack: 1, maxStack: 1024}
	tbl[BALANCE] = &operation{execute: opBalance, constantGas: WarmStorageReadCost, dynamicGas: gasBalanceEIP2929, minStack: 1, maxStack: 1024}
	tbl[EXTCODESIZE] = &operation{execute: opExtcodesize, constantGas: WarmStorageReadCost, dynamicGas: gasExtCodeSizeEIP2929, minStack: 1, maxStack: 1024}
	tbl[EXTCODECOPY] = &operation{execute: opExtcodecopy, constantGas: WarmStorageReadCost, dynamicGas: gasExtCodeCopyEIP2929, minStack: 4, maxStack: 1024, memorySize: memoryExtcodecopy}
	tbl[EXTCODEHASH] = &operation{execute: opExtcodehash, constantGas: WarmStorageReadCost, dynamicGas: gasExtCodeHashEIP2929, minStack: 1, maxStack: 1024}
	tbl[CALL] = &operation{execute: opCall, constantGas: WarmStorageReadCost, dynamicGas: gasCallEIP2929, minStack: 7, maxStack: 1024, memorySize: memoryCall}
	tbl[CALLCODE] = &operation{execute: opCallCode, constantGas: WarmStorageReadCost, dynamicGas: gasCallCodeEIP2929, minStack: 7, maxStack: 1024, memorySize: memoryCallCode}
	tbl[STATICCALL] = &operation{execute: opStaticCall, constantGas: WarmStorageReadCost, dynamicGas: gasStaticCallEIP2929, minStack: 6, maxStack: 1024, memorySize: memoryStaticCall}
	tbl[DELEGATECALL] = &operation{execute: opDelegateCall, constantGas: WarmStorageReadCost, dynamicGas: gasDelegateCallEIP2929, minStack: 6, maxStack: 1024, memorySize: memoryDelegateCall}
	tbl[SSTORE] = &operation{execute: opSstore, constantGas: 0, dynamicGas: gasSstoreEIP2929, minStack: 2, maxStack: 1024, writes: true}
	tbl[SELFDESTRUCT] = &operation{execute: opSelfdestruct, constantGas: GasSelfdestruct, dynamicGas: gasSelfdestructEIP2929, minStack: 1, maxStack: 1024, halts: true, writes: true}

	return tbl
}

// NewLondonJumpTable returns the London fork jump table.
func NewLondonJumpTable() JumpTable {
	tbl := NewBerlinJumpTable()
	// London added BASEFEE.
	tbl[BASEFEE] = &operation{execute: opBaseFee, constantGas: GasBase, minStack: 0, maxStack: 1023}
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

	// EIP-1153: transient storage
	tbl[TLOAD] = &operation{execute: opTload, constantGas: GasTload, minStack: 1, maxStack: 1024}
	tbl[TSTORE] = &operation{execute: opTstore, constantGas: GasTstore, minStack: 2, maxStack: 1024, writes: true}

	// EIP-5656: memory copy
	tbl[MCOPY] = &operation{execute: opMcopy, constantGas: GasMcopyBase, minStack: 3, maxStack: 1024, memorySize: memoryMcopy, dynamicGas: gasMcopy}

	// EIP-4844: blob hash
	tbl[BLOBHASH] = &operation{execute: opBlobHash, constantGas: GasBlobHash, minStack: 1, maxStack: 1024}

	// EIP-7516: blob base fee
	tbl[BLOBBASEFEE] = &operation{execute: opBlobBaseFee, constantGas: GasBlobBaseFee, minStack: 0, maxStack: 1023}

	return tbl
}

// NewPragueJumpTable returns the Prague fork jump table.
func NewPragueJumpTable() JumpTable {
	tbl := NewCancunJumpTable()
	// Prague includes EIP-7702, EIP-7685, and other enhancements.
	return tbl
}

// NewGlamsterdanJumpTable returns the Glamsterdan fork jump table.
// EIP-7904: compute gas cost increases for underpriced opcodes.
func NewGlamsterdanJumpTable() JumpTable {
	tbl := NewPragueJumpTable()

	// EIP-7904: DIV 5 -> 15
	tbl[DIV] = &operation{execute: opDiv, constantGas: GasDivGlamsterdan, minStack: 2, maxStack: 1024}
	// EIP-7904: SDIV 5 -> 20
	tbl[SDIV] = &operation{execute: opSdiv, constantGas: GasSdivGlamsterdan, minStack: 2, maxStack: 1024}
	// EIP-7904: MOD 5 -> 12
	tbl[MOD] = &operation{execute: opMod, constantGas: GasModGlamsterdan, minStack: 2, maxStack: 1024}
	// EIP-7904: MULMOD 8 -> 11
	tbl[MULMOD] = &operation{execute: opMulmod, constantGas: GasMulmodGlamsterdan, minStack: 3, maxStack: 1024}
	// EIP-7904: KECCAK256 constant 30 -> 45 (dynamic gas unchanged)
	tbl[KECCAK256] = &operation{execute: opKeccak256, constantGas: GasKeccak256Glamsterdan, minStack: 2, maxStack: 1024, memorySize: memoryKeccak256, dynamicGas: gasSha3}

	return tbl
}
