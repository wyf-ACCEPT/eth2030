package vm

import (
	"math/big"

	"github.com/eth2028/eth2028/core/types"
)

// Contract represents an EVM contract in the context of execution.
type Contract struct {
	CallerAddress types.Address
	Address       types.Address
	Code          []byte
	CodeHash      types.Hash
	Input         []byte
	Gas           uint64
	Value         *big.Int
	jumpdests     map[uint64]bool // cached JUMPDEST analysis

	// EOF fields (EIP-3540, EIP-7480, EIP-7620)
	Data          []byte   // EOF data section (EIP-7480: DATALOAD, DATALOADN, DATASIZE, DATACOPY)
	Subcontainers [][]byte // EOF subcontainers (EIP-7620: EOFCREATE, RETURNCONTRACT)
}

// NewContract creates a new contract for execution.
func NewContract(caller, addr types.Address, value *big.Int, gas uint64) *Contract {
	return &Contract{
		CallerAddress: caller,
		Address:       addr,
		Value:         value,
		Gas:           gas,
	}
}

// GetOp returns the opcode at position n in the contract code.
func (c *Contract) GetOp(n uint64) OpCode {
	if n < uint64(len(c.Code)) {
		return OpCode(c.Code[n])
	}
	return STOP
}

// UseGas attempts to consume the given gas. Returns false if insufficient gas.
func (c *Contract) UseGas(gas uint64) bool {
	if c.Gas < gas {
		return false
	}
	c.Gas -= gas
	return true
}

// SetCallCode sets the code and code hash for a CALL-type execution.
func (c *Contract) SetCallCode(addr *types.Address, hash types.Hash, code []byte) {
	c.Code = code
	c.CodeHash = hash
	if addr != nil {
		c.Address = *addr
	}
}

// validJumpdest checks whether dest is a valid JUMPDEST position in the code.
func (c *Contract) validJumpdest(dest *big.Int) bool {
	udest := dest.Uint64()
	// Quick check that dest is within code bounds and the byte is JUMPDEST.
	if dest.BitLen() > 63 || udest >= uint64(len(c.Code)) {
		return false
	}
	if OpCode(c.Code[udest]) != JUMPDEST {
		return false
	}
	// Perform JUMPDEST analysis to verify this isn't inside PUSH data.
	return c.isCode(udest)
}

// isCode returns true if the given offset is an opcode (not PUSH data).
func (c *Contract) isCode(pos uint64) bool {
	if c.jumpdests == nil {
		c.jumpdests = make(map[uint64]bool)
		c.analyzeJumpdests()
	}
	return c.jumpdests[pos]
}

// analyzeJumpdests scans the code to identify all valid JUMPDEST locations.
func (c *Contract) analyzeJumpdests() {
	for i := uint64(0); i < uint64(len(c.Code)); i++ {
		op := OpCode(c.Code[i])
		if op == JUMPDEST {
			c.jumpdests[i] = true
		}
		if op.IsPush() {
			// Skip the push data bytes.
			i += uint64(op - PUSH1 + 1)
		}
	}
}
