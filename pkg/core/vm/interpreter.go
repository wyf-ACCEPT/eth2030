package vm

import (
	"errors"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
)

var (
	ErrNotImplemented       = errors.New("not implemented")
	ErrOutOfGas             = errors.New("out of gas")
	ErrStackOverflow        = errors.New("stack overflow")
	ErrStackUnderflow       = errors.New("stack underflow")
	ErrInvalidJump          = errors.New("invalid jump destination")
	ErrWriteProtection      = errors.New("write protection")
	ErrExecutionReverted    = errors.New("execution reverted")
	ErrMaxCallDepthExceeded = errors.New("max call depth exceeded")
	ErrInvalidOpCode        = errors.New("invalid opcode")
)

// BlockContext provides the EVM with block-level information.
type BlockContext struct {
	BlockNumber *big.Int
	Time        uint64
	Coinbase    types.Address
	GasLimit    uint64
	BaseFee     *big.Int
	PrevRandao  types.Hash
	BlobBaseFee *big.Int
}

// TxContext provides the EVM with transaction-level information.
type TxContext struct {
	Origin     types.Address
	GasPrice   *big.Int
	BlobHashes []types.Hash
}

// Config holds EVM configuration options.
type Config struct {
	Debug        bool
	Tracer       interface{}
	MaxCallDepth int
}

// EVM is the Ethereum Virtual Machine execution environment.
type EVM struct {
	Context   BlockContext
	TxContext TxContext
	Config    Config
	depth     int
	readOnly  bool
}

// NewEVM creates a new EVM instance.
func NewEVM(blockCtx BlockContext, txCtx TxContext, config Config) *EVM {
	if config.MaxCallDepth == 0 {
		config.MaxCallDepth = 1024
	}
	return &EVM{
		Context:   blockCtx,
		TxContext: txCtx,
		Config:    config,
	}
}

// Call executes a message call. Stub implementation.
func (evm *EVM) Call(caller types.Address, addr types.Address, input []byte, gas uint64, value *big.Int) ([]byte, uint64, error) {
	return nil, gas, ErrNotImplemented
}

// StaticCall executes a read-only message call.
func (evm *EVM) StaticCall(caller types.Address, addr types.Address, input []byte, gas uint64) ([]byte, uint64, error) {
	return nil, gas, ErrNotImplemented
}

// Create creates a new contract. Stub implementation.
func (evm *EVM) Create(caller types.Address, code []byte, gas uint64, value *big.Int) ([]byte, types.Address, uint64, error) {
	return nil, types.Address{}, gas, ErrNotImplemented
}
