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

// StateDB provides the EVM with access to Ethereum world state.
// This interface is defined in the vm package to avoid circular imports
// with core/state. Any implementation of core/state.StateDB satisfies it.
type StateDB interface {
	GetState(addr types.Address, key types.Hash) types.Hash
	SetState(addr types.Address, key types.Hash, value types.Hash)
	GetBalance(addr types.Address) *big.Int
	AddLog(log *types.Log)
	Exist(addr types.Address) bool
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
	StateDB   StateDB
	chainID   uint64
	depth     int
	readOnly  bool
	jumpTable JumpTable
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
		jumpTable: NewCancunJumpTable(),
	}
}

// NewEVMWithState creates a new EVM instance with state access.
func NewEVMWithState(blockCtx BlockContext, txCtx TxContext, config Config, stateDB StateDB) *EVM {
	evm := NewEVM(blockCtx, txCtx, config)
	evm.StateDB = stateDB
	return evm
}

// Run executes the contract bytecode using the interpreter loop.
func (evm *EVM) Run(contract *Contract, input []byte) ([]byte, error) {
	contract.Input = input

	var (
		pc    uint64
		stack = NewStack()
		mem   = NewMemory()
	)

	for {
		op := contract.GetOp(pc)
		operation := evm.jumpTable[op]
		if operation == nil || operation.execute == nil {
			return nil, ErrInvalidOpCode
		}

		// Stack validation
		sLen := stack.Len()
		if sLen < operation.minStack {
			return nil, ErrStackUnderflow
		}
		if sLen > operation.maxStack {
			return nil, ErrStackOverflow
		}

		// Memory expansion (if operation defines memorySize)
		if operation.memorySize != nil {
			memSize := operation.memorySize(stack)
			if memSize > 0 {
				// Round up to 32-byte words
				words := (memSize + 31) / 32
				newSize := words * 32
				if uint64(mem.Len()) < newSize {
					// Dynamic gas for memory expansion
					if operation.dynamicGas != nil {
						cost := operation.dynamicGas(evm, contract, stack, mem, memSize)
						if !contract.UseGas(cost) {
							return nil, ErrOutOfGas
						}
					}
					mem.Resize(newSize)
				}
			}
		}

		// Constant gas deduction
		if operation.constantGas > 0 {
			if !contract.UseGas(operation.constantGas) {
				return nil, ErrOutOfGas
			}
		}

		// Execute the opcode
		ret, err := operation.execute(&pc, evm, contract, mem, stack)

		if err != nil {
			if errors.Is(err, ErrExecutionReverted) {
				return ret, err
			}
			return nil, err
		}

		// Handle halting opcodes
		if operation.halts {
			return ret, nil
		}
		if operation.jumps {
			continue
		}

		pc++
	}
}

// Call executes a message call. Stub implementation.
func (evm *EVM) Call(caller types.Address, addr types.Address, input []byte, gas uint64, value *big.Int) ([]byte, uint64, error) {
	return nil, gas, ErrNotImplemented
}

// CallCode executes a CALLCODE operation. Stub implementation.
func (evm *EVM) CallCode(caller types.Address, addr types.Address, input []byte, gas uint64, value *big.Int) ([]byte, uint64, error) {
	return nil, gas, ErrNotImplemented
}

// DelegateCall executes a DELEGATECALL operation. Stub implementation.
func (evm *EVM) DelegateCall(caller types.Address, addr types.Address, input []byte, gas uint64) ([]byte, uint64, error) {
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

// Create2 creates a new contract using CREATE2. Stub implementation.
func (evm *EVM) Create2(caller types.Address, code []byte, gas uint64, endowment *big.Int, salt *big.Int) ([]byte, types.Address, uint64, error) {
	return nil, types.Address{}, gas, ErrNotImplemented
}
