package vm

import "fmt"

// OpCode is an EVM opcode byte.
type OpCode byte

const (
	STOP       OpCode = 0x00
	ADD        OpCode = 0x01
	MUL        OpCode = 0x02
	SUB        OpCode = 0x03
	DIV        OpCode = 0x04
	SDIV       OpCode = 0x05
	MOD        OpCode = 0x06
	SMOD       OpCode = 0x07
	ADDMOD     OpCode = 0x08
	MULMOD     OpCode = 0x09
	EXP        OpCode = 0x0a
	SIGNEXTEND OpCode = 0x0b

	LT     OpCode = 0x10
	GT     OpCode = 0x11
	SLT    OpCode = 0x12
	SGT    OpCode = 0x13
	EQ     OpCode = 0x14
	ISZERO OpCode = 0x15
	AND    OpCode = 0x16
	OR     OpCode = 0x17
	XOR    OpCode = 0x18
	NOT    OpCode = 0x19
	BYTE   OpCode = 0x1a
	SHL    OpCode = 0x1b
	SHR    OpCode = 0x1c
	SAR    OpCode = 0x1d

	KECCAK256 OpCode = 0x20

	ADDRESS        OpCode = 0x30
	BALANCE        OpCode = 0x31
	ORIGIN         OpCode = 0x32
	CALLER         OpCode = 0x33
	CALLVALUE      OpCode = 0x34
	CALLDATALOAD   OpCode = 0x35
	CALLDATASIZE   OpCode = 0x36
	CALLDATACOPY   OpCode = 0x37
	CODESIZE       OpCode = 0x38
	CODECOPY       OpCode = 0x39
	GASPRICE       OpCode = 0x3a
	EXTCODESIZE    OpCode = 0x3b
	EXTCODECOPY    OpCode = 0x3c
	RETURNDATASIZE OpCode = 0x3d
	RETURNDATACOPY OpCode = 0x3e
	EXTCODEHASH    OpCode = 0x3f

	BLOCKHASH   OpCode = 0x40
	COINBASE    OpCode = 0x41
	TIMESTAMP   OpCode = 0x42
	NUMBER      OpCode = 0x43
	PREVRANDAO  OpCode = 0x44 // was DIFFICULTY pre-merge
	GASLIMIT    OpCode = 0x45
	CHAINID     OpCode = 0x46
	SELFBALANCE OpCode = 0x47
	BASEFEE     OpCode = 0x48
	BLOBHASH    OpCode = 0x49
	BLOBBASEFEE OpCode = 0x4a

	POP     OpCode = 0x50
	MLOAD   OpCode = 0x51
	MSTORE  OpCode = 0x52
	MSTORE8 OpCode = 0x53
	SLOAD   OpCode = 0x54
	SSTORE  OpCode = 0x55
	JUMP    OpCode = 0x56
	JUMPI   OpCode = 0x57
	PC      OpCode = 0x58
	MSIZE   OpCode = 0x59
	GAS     OpCode = 0x5a
	JUMPDEST OpCode = 0x5b
	TLOAD   OpCode = 0x5c // EIP-1153
	TSTORE  OpCode = 0x5d // EIP-1153
	MCOPY   OpCode = 0x5e // EIP-5656

	PUSH0  OpCode = 0x5f
	PUSH1  OpCode = 0x60
	PUSH2  OpCode = 0x61
	PUSH3  OpCode = 0x62
	PUSH4  OpCode = 0x63
	PUSH5  OpCode = 0x64
	PUSH6  OpCode = 0x65
	PUSH7  OpCode = 0x66
	PUSH8  OpCode = 0x67
	PUSH9  OpCode = 0x68
	PUSH10 OpCode = 0x69
	PUSH11 OpCode = 0x6a
	PUSH12 OpCode = 0x6b
	PUSH13 OpCode = 0x6c
	PUSH14 OpCode = 0x6d
	PUSH15 OpCode = 0x6e
	PUSH16 OpCode = 0x6f
	PUSH17 OpCode = 0x70
	PUSH18 OpCode = 0x71
	PUSH19 OpCode = 0x72
	PUSH20 OpCode = 0x73
	PUSH21 OpCode = 0x74
	PUSH22 OpCode = 0x75
	PUSH23 OpCode = 0x76
	PUSH24 OpCode = 0x77
	PUSH25 OpCode = 0x78
	PUSH26 OpCode = 0x79
	PUSH27 OpCode = 0x7a
	PUSH28 OpCode = 0x7b
	PUSH29 OpCode = 0x7c
	PUSH30 OpCode = 0x7d
	PUSH31 OpCode = 0x7e
	PUSH32 OpCode = 0x7f

	DUP1  OpCode = 0x80
	DUP2  OpCode = 0x81
	DUP3  OpCode = 0x82
	DUP4  OpCode = 0x83
	DUP5  OpCode = 0x84
	DUP6  OpCode = 0x85
	DUP7  OpCode = 0x86
	DUP8  OpCode = 0x87
	DUP9  OpCode = 0x88
	DUP10 OpCode = 0x89
	DUP11 OpCode = 0x8a
	DUP12 OpCode = 0x8b
	DUP13 OpCode = 0x8c
	DUP14 OpCode = 0x8d
	DUP15 OpCode = 0x8e
	DUP16 OpCode = 0x8f

	SWAP1  OpCode = 0x90
	SWAP2  OpCode = 0x91
	SWAP3  OpCode = 0x92
	SWAP4  OpCode = 0x93
	SWAP5  OpCode = 0x94
	SWAP6  OpCode = 0x95
	SWAP7  OpCode = 0x96
	SWAP8  OpCode = 0x97
	SWAP9  OpCode = 0x98
	SWAP10 OpCode = 0x99
	SWAP11 OpCode = 0x9a
	SWAP12 OpCode = 0x9b
	SWAP13 OpCode = 0x9c
	SWAP14 OpCode = 0x9d
	SWAP15 OpCode = 0x9e
	SWAP16 OpCode = 0x9f

	LOG0 OpCode = 0xa0
	LOG1 OpCode = 0xa1
	LOG2 OpCode = 0xa2
	LOG3 OpCode = 0xa3
	LOG4 OpCode = 0xa4

	CREATE       OpCode = 0xf0
	CALL         OpCode = 0xf1
	CALLCODE     OpCode = 0xf2
	RETURN       OpCode = 0xf3
	DELEGATECALL OpCode = 0xf4
	CREATE2      OpCode = 0xf5
	STATICCALL   OpCode = 0xfa
	REVERT       OpCode = 0xfd
	INVALID      OpCode = 0xfe
	SELFDESTRUCT OpCode = 0xff
)

var opCodeNames = map[OpCode]string{
	STOP: "STOP", ADD: "ADD", MUL: "MUL", SUB: "SUB",
	DIV: "DIV", SDIV: "SDIV", MOD: "MOD", SMOD: "SMOD",
	ADDMOD: "ADDMOD", MULMOD: "MULMOD", EXP: "EXP", SIGNEXTEND: "SIGNEXTEND",
	LT: "LT", GT: "GT", SLT: "SLT", SGT: "SGT",
	EQ: "EQ", ISZERO: "ISZERO", AND: "AND", OR: "OR",
	XOR: "XOR", NOT: "NOT", BYTE: "BYTE",
	SHL: "SHL", SHR: "SHR", SAR: "SAR",
	KECCAK256: "KECCAK256",
	ADDRESS: "ADDRESS", BALANCE: "BALANCE", ORIGIN: "ORIGIN",
	CALLER: "CALLER", CALLVALUE: "CALLVALUE",
	CALLDATALOAD: "CALLDATALOAD", CALLDATASIZE: "CALLDATASIZE", CALLDATACOPY: "CALLDATACOPY",
	CODESIZE: "CODESIZE", CODECOPY: "CODECOPY", GASPRICE: "GASPRICE",
	EXTCODESIZE: "EXTCODESIZE", EXTCODECOPY: "EXTCODECOPY",
	RETURNDATASIZE: "RETURNDATASIZE", RETURNDATACOPY: "RETURNDATACOPY",
	EXTCODEHASH: "EXTCODEHASH",
	BLOCKHASH: "BLOCKHASH", COINBASE: "COINBASE", TIMESTAMP: "TIMESTAMP",
	NUMBER: "NUMBER", PREVRANDAO: "PREVRANDAO", GASLIMIT: "GASLIMIT",
	CHAINID: "CHAINID", SELFBALANCE: "SELFBALANCE", BASEFEE: "BASEFEE",
	BLOBHASH: "BLOBHASH", BLOBBASEFEE: "BLOBBASEFEE",
	POP: "POP", MLOAD: "MLOAD", MSTORE: "MSTORE", MSTORE8: "MSTORE8",
	SLOAD: "SLOAD", SSTORE: "SSTORE",
	JUMP: "JUMP", JUMPI: "JUMPI", PC: "PC", MSIZE: "MSIZE", GAS: "GAS",
	JUMPDEST: "JUMPDEST", TLOAD: "TLOAD", TSTORE: "TSTORE", MCOPY: "MCOPY",
	PUSH0: "PUSH0",
	PUSH1: "PUSH1", PUSH2: "PUSH2", PUSH3: "PUSH3", PUSH4: "PUSH4",
	PUSH5: "PUSH5", PUSH6: "PUSH6", PUSH7: "PUSH7", PUSH8: "PUSH8",
	PUSH9: "PUSH9", PUSH10: "PUSH10", PUSH11: "PUSH11", PUSH12: "PUSH12",
	PUSH13: "PUSH13", PUSH14: "PUSH14", PUSH15: "PUSH15", PUSH16: "PUSH16",
	PUSH17: "PUSH17", PUSH18: "PUSH18", PUSH19: "PUSH19", PUSH20: "PUSH20",
	PUSH21: "PUSH21", PUSH22: "PUSH22", PUSH23: "PUSH23", PUSH24: "PUSH24",
	PUSH25: "PUSH25", PUSH26: "PUSH26", PUSH27: "PUSH27", PUSH28: "PUSH28",
	PUSH29: "PUSH29", PUSH30: "PUSH30", PUSH31: "PUSH31", PUSH32: "PUSH32",
	DUP1: "DUP1", DUP2: "DUP2", DUP3: "DUP3", DUP4: "DUP4",
	DUP5: "DUP5", DUP6: "DUP6", DUP7: "DUP7", DUP8: "DUP8",
	DUP9: "DUP9", DUP10: "DUP10", DUP11: "DUP11", DUP12: "DUP12",
	DUP13: "DUP13", DUP14: "DUP14", DUP15: "DUP15", DUP16: "DUP16",
	SWAP1: "SWAP1", SWAP2: "SWAP2", SWAP3: "SWAP3", SWAP4: "SWAP4",
	SWAP5: "SWAP5", SWAP6: "SWAP6", SWAP7: "SWAP7", SWAP8: "SWAP8",
	SWAP9: "SWAP9", SWAP10: "SWAP10", SWAP11: "SWAP11", SWAP12: "SWAP12",
	SWAP13: "SWAP13", SWAP14: "SWAP14", SWAP15: "SWAP15", SWAP16: "SWAP16",
	LOG0: "LOG0", LOG1: "LOG1", LOG2: "LOG2", LOG3: "LOG3", LOG4: "LOG4",
	CREATE: "CREATE", CALL: "CALL", CALLCODE: "CALLCODE", RETURN: "RETURN",
	DELEGATECALL: "DELEGATECALL", CREATE2: "CREATE2",
	STATICCALL: "STATICCALL", REVERT: "REVERT",
	INVALID: "INVALID", SELFDESTRUCT: "SELFDESTRUCT",
}

// String returns the name of the opcode.
func (op OpCode) String() string {
	if name, ok := opCodeNames[op]; ok {
		return name
	}
	return fmt.Sprintf("opcode 0x%x", byte(op))
}

// IsPush returns true if the opcode is a PUSH instruction (PUSH1..PUSH32).
func (op OpCode) IsPush() bool {
	return op >= PUSH1 && op <= PUSH32
}
