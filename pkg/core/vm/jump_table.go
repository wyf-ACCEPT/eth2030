package vm

// operation represents a single EVM opcode's execution metadata.
type operation struct {
	execute     executionFunc
	constantGas uint64
	minStack    int // minimum stack items required
	maxStack    int // maximum stack items allowed (1024 - items this op pushes net)
}

// JumpTable maps every possible opcode to its operation definition.
type JumpTable [256]*operation

// NewCancunJumpTable returns a jump table populated with Cancun hard fork opcodes.
func NewCancunJumpTable() JumpTable {
	var tbl JumpTable

	// Arithmetic
	tbl[STOP] = &operation{execute: opStop, constantGas: GasStop, minStack: 0, maxStack: 1024}
	tbl[ADD] = &operation{execute: opAdd, constantGas: GasQuickStep, minStack: 2, maxStack: 1024}
	tbl[MUL] = &operation{execute: opMul, constantGas: GasFastestStep, minStack: 2, maxStack: 1024}
	tbl[SUB] = &operation{execute: opSub, constantGas: GasQuickStep, minStack: 2, maxStack: 1024}
	tbl[DIV] = &operation{execute: opDiv, constantGas: GasFastStep, minStack: 2, maxStack: 1024}
	tbl[SDIV] = &operation{execute: opSdiv, constantGas: GasFastStep, minStack: 2, maxStack: 1024}
	tbl[MOD] = &operation{execute: opMod, constantGas: GasFastStep, minStack: 2, maxStack: 1024}
	tbl[SMOD] = &operation{execute: opSmod, constantGas: GasFastStep, minStack: 2, maxStack: 1024}

	// Comparison
	tbl[LT] = &operation{execute: opLt, constantGas: GasQuickStep, minStack: 2, maxStack: 1024}
	tbl[GT] = &operation{execute: opGt, constantGas: GasQuickStep, minStack: 2, maxStack: 1024}
	tbl[EQ] = &operation{execute: opEq, constantGas: GasQuickStep, minStack: 2, maxStack: 1024}
	tbl[ISZERO] = &operation{execute: opIsZero, constantGas: GasQuickStep, minStack: 1, maxStack: 1024}

	// Bitwise
	tbl[AND] = &operation{execute: opAnd, constantGas: GasQuickStep, minStack: 2, maxStack: 1024}
	tbl[OR] = &operation{execute: opOr, constantGas: GasQuickStep, minStack: 2, maxStack: 1024}
	tbl[XOR] = &operation{execute: opXor, constantGas: GasQuickStep, minStack: 2, maxStack: 1024}
	tbl[NOT] = &operation{execute: opNot, constantGas: GasQuickStep, minStack: 1, maxStack: 1024}

	// Stack / memory / flow
	tbl[POP] = &operation{execute: opPop, constantGas: GasPop, minStack: 1, maxStack: 1024}
	tbl[MLOAD] = &operation{execute: opMload, constantGas: GasMload, minStack: 1, maxStack: 1024}
	tbl[MSTORE] = &operation{execute: opMstore, constantGas: GasMstore, minStack: 2, maxStack: 1024}
	tbl[MSTORE8] = &operation{execute: opMstore8, constantGas: GasMstore8, minStack: 2, maxStack: 1024}

	// Push
	tbl[PUSH1] = &operation{execute: opPush1, constantGas: GasPush, minStack: 0, maxStack: 1023}

	// Return / Revert
	tbl[RETURN] = &operation{execute: opReturn, constantGas: GasReturn, minStack: 2, maxStack: 1024}
	tbl[REVERT] = &operation{execute: opRevert, constantGas: GasRevert, minStack: 2, maxStack: 1024}

	return tbl
}
