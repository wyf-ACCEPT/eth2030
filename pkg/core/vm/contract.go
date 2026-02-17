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
