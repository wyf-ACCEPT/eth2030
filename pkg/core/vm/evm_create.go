package vm

// evm_create.go implements CREATE/CREATE2 opcode logic with full contract
// creation semantics: init code execution, code deposit, nonce management,
// collision detection, EIP-170/EIP-7954 code size limits, and EIP-3860
// initcode size limits. This file provides the CreateExecutor which handles
// the complete lifecycle of a contract creation operation.

import (
	"errors"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// Contract creation errors.
var (
	ErrCreateCollision         = errors.New("create: contract address collision")
	ErrCreateNonceOverflow     = errors.New("create: sender nonce overflow")
	ErrCreateInsufficientFunds = errors.New("create: insufficient balance for endowment")
	ErrCreateCodeTooLarge      = errors.New("create: deployed code exceeds max size")
	ErrCreateInitCodeTooLarge  = errors.New("create: init code exceeds max size")
)

// CreateKind identifies whether the creation is via CREATE or CREATE2.
type CreateKind uint8

const (
	CreateKindCreate  CreateKind = iota // Standard CREATE
	CreateKindCreate2                   // Deterministic CREATE2
)

// String returns the human-readable name.
func (ck CreateKind) String() string {
	if ck == CreateKindCreate2 {
		return "CREATE2"
	}
	return "CREATE"
}

// CreateParams encapsulates the parameters for a contract creation operation.
type CreateParams struct {
	Kind     CreateKind
	Caller   types.Address // address initiating the creation
	InitCode []byte        // init code to execute
	Value    *big.Int      // ETH endowment to the new contract
	Salt     *big.Int      // salt for CREATE2 (ignored for CREATE)
	Gas      uint64        // gas available for the creation
}

// CreateResult holds the outcome of a contract creation operation.
type CreateResult struct {
	Address    types.Address // address of the newly created contract
	ReturnData []byte        // data returned by init code (deployed bytecode)
	GasUsed    uint64        // total gas consumed
	GasLeft    uint64        // gas remaining after creation
	Err        error         // nil on success
}

// CreateExecutor handles the complete lifecycle of CREATE/CREATE2 operations
// including address computation, collision detection, init code execution,
// code deposit, and gas accounting.
type CreateExecutor struct {
	maxCodeSize     int  // EIP-170/EIP-7954: max deployed code size
	maxInitCodeSize int  // EIP-3860: max init code size
	eip7610Enabled  bool // EIP-7610: check storage for collision
}

// NewCreateExecutor constructs a CreateExecutor with limits derived from the
// given fork rules.
func NewCreateExecutor(rules ForkRules) *CreateExecutor {
	return &CreateExecutor{
		maxCodeSize:     MaxCodeSizeForFork(rules),
		maxInitCodeSize: MaxInitCodeSizeForFork(rules),
		eip7610Enabled:  rules.IsPrague || rules.IsGlamsterdan,
	}
}

// ComputeAddress derives the new contract address for the given params and
// sender nonce. For CREATE it uses RLP(sender, nonce). For CREATE2 it uses
// keccak256(0xff ++ sender ++ salt ++ keccak256(initCode)).
func (ce *CreateExecutor) ComputeAddress(params *CreateParams, nonce uint64) types.Address {
	if params.Kind == CreateKindCreate2 {
		codeHash := crypto.Keccak256(params.InitCode)
		return create2Address(params.Caller, params.Salt, codeHash)
	}
	return createAddress(params.Caller, nonce)
}

// ValidateInitCode checks that the init code does not exceed the maximum
// allowed size per EIP-3860.
func (ce *CreateExecutor) ValidateInitCode(initCode []byte) error {
	if len(initCode) > ce.maxInitCodeSize {
		return ErrCreateInitCodeTooLarge
	}
	return nil
}

// ValidateDeployedCode checks that the deployed code (returned by init code
// execution) does not exceed the maximum contract size per EIP-170/EIP-7954.
func (ce *CreateExecutor) ValidateDeployedCode(code []byte) error {
	if len(code) > ce.maxCodeSize {
		return ErrCreateCodeTooLarge
	}
	return nil
}

// CheckCollision verifies that deploying to addr would not collide with
// existing state. An address is considered in use if it has a non-zero nonce
// or non-empty code. With EIP-7610, non-empty storage also triggers a collision.
func (ce *CreateExecutor) CheckCollision(stateDB StateDB, addr types.Address) error {
	if stateDB == nil {
		return nil
	}
	// Non-zero nonce means the address has been used.
	if stateDB.GetNonce(addr) != 0 {
		return ErrCreateCollision
	}
	// Non-empty code means there is already a contract deployed.
	codeHash := stateDB.GetCodeHash(addr)
	if codeHash != (types.Hash{}) && codeHash != types.EmptyCodeHash {
		return ErrCreateCollision
	}
	// EIP-7610: non-empty storage is also a collision.
	if ce.eip7610Enabled {
		if HasNonEmptyStorage(stateDB, addr) {
			return ErrCreateCollision
		}
	}
	return nil
}

// CalcCreateGas computes the total upfront gas cost for a contract creation
// operation. This includes the base CREATE gas (32000), init code word gas
// (EIP-3860: 2 gas per 32-byte word), and for CREATE2 the keccak256 hashing
// cost (6 gas per 32-byte word).
func (ce *CreateExecutor) CalcCreateGas(params *CreateParams) uint64 {
	gas := uint64(GasCreate) // 32000 base

	// EIP-3860: init code word gas.
	if len(params.InitCode) > 0 {
		words := toWordSize(uint64(len(params.InitCode)))
		gas = safeAdd(gas, safeMul(InitCodeWordGas, words))
	}

	// CREATE2: additional keccak256 hashing cost for init code.
	if params.Kind == CreateKindCreate2 && len(params.InitCode) > 0 {
		words := toWordSize(uint64(len(params.InitCode)))
		gas = safeAdd(gas, safeMul(GasKeccak256Word, words))
	}

	return gas
}

// CalcCodeDepositGas computes the gas cost for depositing the deployed
// bytecode (200 gas per byte per the Yellow Paper).
func (ce *CreateExecutor) CalcCodeDepositGas(code []byte) uint64 {
	return safeMul(CreateDataGas, uint64(len(code)))
}

// Execute performs the full contract creation lifecycle on the given EVM.
// It handles nonce increment, address computation, collision detection,
// gas deduction, init code execution via the 63/64 rule, code deposit, and
// state revert on failure.
func (ce *CreateExecutor) Execute(evm *EVM, params *CreateParams) *CreateResult {
	result := &CreateResult{GasLeft: params.Gas}

	// Validate init code size.
	if err := ce.ValidateInitCode(params.InitCode); err != nil {
		result.Err = err
		result.GasLeft = 0
		return result
	}

	// Deduct the base creation gas + init code word gas.
	upfrontGas := ce.CalcCreateGas(params)
	if result.GasLeft < upfrontGas {
		result.Err = ErrOutOfGas
		result.GasLeft = 0
		return result
	}
	result.GasLeft -= upfrontGas

	// Compute the contract address.
	var nonce uint64
	if evm.StateDB != nil {
		nonce = evm.StateDB.GetNonce(params.Caller)
	}
	addr := ce.ComputeAddress(params, nonce)
	result.Address = addr

	// Increment sender nonce for CREATE (CREATE2 does not change nonce here;
	// the outer EVM.Create2 handles it).
	if params.Kind == CreateKindCreate && evm.StateDB != nil {
		evm.StateDB.SetNonce(params.Caller, nonce+1)
	}

	// Check for address collision.
	if err := ce.CheckCollision(evm.StateDB, addr); err != nil {
		result.Err = err
		result.GasUsed = params.Gas - result.GasLeft
		return result
	}

	// Snapshot for revert on failure.
	var snapshot int
	if evm.StateDB != nil {
		snapshot = evm.StateDB.Snapshot()
		evm.StateDB.CreateAccount(addr)
		// EIP-161: set nonce to 1 for newly created contracts.
		evm.StateDB.SetNonce(addr, 1)
	}

	// Transfer endowment.
	if params.Value != nil && params.Value.Sign() > 0 {
		if evm.StateDB == nil {
			result.Err = errors.New("create: no state database for value transfer")
			return result
		}
		callerBal := evm.StateDB.GetBalance(params.Caller)
		if callerBal.Cmp(params.Value) < 0 {
			evm.StateDB.RevertToSnapshot(snapshot)
			result.Err = ErrCreateInsufficientFunds
			result.GasUsed = params.Gas - result.GasLeft
			return result
		}
		evm.StateDB.SubBalance(params.Caller, params.Value)
		evm.StateDB.AddBalance(addr, params.Value)
	}

	// Apply the 63/64 gas forwarding rule (EIP-150).
	callGas := result.GasLeft - result.GasLeft/CallGasFraction
	result.GasLeft -= callGas

	// Execute init code.
	contract := NewContract(params.Caller, addr, params.Value, callGas)
	contract.Code = params.InitCode

	evm.depth++
	ret, err := evm.Run(contract, nil)
	evm.depth--

	// Return unused gas from the subcall.
	result.GasLeft += contract.Gas

	if err != nil {
		if evm.StateDB != nil {
			evm.StateDB.RevertToSnapshot(snapshot)
		}
		result.Err = err
		result.ReturnData = ret
		if !errors.Is(err, ErrExecutionReverted) {
			// On non-revert error, all forwarded gas is consumed.
			result.GasLeft = params.Gas - upfrontGas - callGas + contract.Gas
		}
		result.GasUsed = params.Gas - result.GasLeft
		return result
	}

	// Validate deployed code size.
	if len(ret) > 0 {
		if err := ce.ValidateDeployedCode(ret); err != nil {
			if evm.StateDB != nil {
				evm.StateDB.RevertToSnapshot(snapshot)
			}
			result.Err = err
			result.GasLeft = 0
			result.GasUsed = params.Gas
			return result
		}

		// Charge code deposit gas (200 per byte).
		depositGas := ce.CalcCodeDepositGas(ret)
		if result.GasLeft < depositGas {
			if evm.StateDB != nil {
				evm.StateDB.RevertToSnapshot(snapshot)
			}
			result.Err = ErrOutOfGas
			result.GasLeft = 0
			result.GasUsed = params.Gas
			return result
		}
		result.GasLeft -= depositGas

		// Store the deployed code.
		if evm.StateDB != nil {
			evm.StateDB.SetCode(addr, ret)
		}
	}

	result.ReturnData = ret
	result.GasUsed = params.Gas - result.GasLeft
	return result
}

// MaxNonce is the maximum value for a contract nonce (2^64 - 2), reserving
// 2^64 - 1 as a sentinel per EIP-2681.
const MaxNonce = ^uint64(0) - 1

// CheckNonceOverflow returns an error if the nonce is at or above MaxNonce.
func CheckNonceOverflow(nonce uint64) error {
	if nonce >= MaxNonce {
		return ErrCreateNonceOverflow
	}
	return nil
}

// CreateAddressFromNonce is a convenience function for computing the CREATE
// address from a caller and nonce, using the RLP-based derivation defined in
// the Yellow Paper.
func CreateAddressFromNonce(caller types.Address, nonce uint64) types.Address {
	return createAddress(caller, nonce)
}

// Create2AddressFromSaltAndCode computes a CREATE2 address from the sender,
// salt, and init code. The salt must be a 32-byte big.Int.
func Create2AddressFromSaltAndCode(caller types.Address, salt *big.Int, initCode []byte) types.Address {
	codeHash := crypto.Keccak256(initCode)
	return create2Address(caller, salt, codeHash)
}
