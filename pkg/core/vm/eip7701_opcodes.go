package vm

import (
	"errors"
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// EIP-7701: Native Account Abstraction opcodes.
// CURRENT_ROLE and ACCEPT_ROLE provide smart contract accounts with
// awareness and control over their transaction execution role.

// Opcode values for EIP-7701.
// These are not added to opcodes.go to avoid modifying shared files;
// the team lead will integrate them into the jump table.
const (
	CURRENT_ROLE OpCode = 0xab
	ACCEPT_ROLE  OpCode = 0xac
)

// AA execution role constants matching the EIP-7701 spec roles.
const (
	AARoleSenderDeployment    uint64 = 0xA0
	AARoleSenderValidation    uint64 = 0xA1
	AARolePaymasterValidation uint64 = 0xA2
	AARoleSenderExecution     uint64 = 0xA3
	AARolePaymasterPostOp     uint64 = 0xA4
)

// AAEntryPointAddress is the canonical entry point for AA transactions.
var AAEntryPointAddress = types.HexToAddress("0x0000000000000000000000000000000000007701")

var (
	ErrNoAAContext         = errors.New("no AA transaction context")
	ErrRoleMismatch        = errors.New("ACCEPT_ROLE: frame_role != current_context_role")
	ErrRoleAlreadyAccepted = errors.New("role already accepted")
)

// AAContext holds the transaction-scoped state for EIP-7701 AA transactions.
// It tracks the current execution role and whether each role has been accepted
// via the ACCEPT_ROLE opcode.
type AAContext struct {
	// CurrentRole is the active execution role (one of the AARoleSender* / AARolePaymaster* values).
	CurrentRole uint64

	// RoleAccepted tracks whether ACCEPT_ROLE has been called for the current role.
	RoleAccepted bool

	// Sender is the AA transaction sender address.
	Sender types.Address

	// Paymaster is the optional paymaster address (zero if none).
	Paymaster types.Address

	// Deployer is the optional deployer address (zero if none).
	Deployer types.Address

	// SenderValidationData is the validation data from the transaction.
	SenderValidationData []byte

	// SenderExecutionData is the execution calldata for the sender.
	SenderExecutionData []byte

	// PaymasterData is the paymaster-specific data.
	PaymasterData []byte

	// DeployerData is the deployer-specific data.
	DeployerData []byte

	// Gas limits per phase.
	SenderValidationGas    uint64
	PaymasterValidationGas uint64
	SenderExecutionGas     uint64
	PaymasterPostOpGas     uint64

	// Fee parameters.
	MaxPriorityFeePerGas *big.Int
	MaxFeePerGas         *big.Int

	// Nonce of the AA transaction.
	Nonce uint64

	// TxSigHash is the hash of the transaction for signature verification.
	TxSigHash types.Hash

	// ExecutionStatus records the outcome of the sender execution phase.
	// 0 = not yet executed, 1 = success, 2 = reverted.
	ExecutionStatus uint64

	// ExecutionGasUsed tracks gas consumed during sender execution.
	ExecutionGasUsed uint64
}

// aaContextRegistry maps EVM instances to their AAContext.
// This avoids modifying the shared EVM struct in interpreter.go.
// The team lead should migrate this to a direct EVM.AACtx field.
var (
	aaContextMu       sync.RWMutex
	aaContextRegistry = make(map[*EVM]*AAContext)
)

// SetAAContext associates an AAContext with an EVM instance.
func SetAAContext(evm *EVM, ctx *AAContext) {
	aaContextMu.Lock()
	defer aaContextMu.Unlock()
	if ctx == nil {
		delete(aaContextRegistry, evm)
	} else {
		aaContextRegistry[evm] = ctx
	}
}

// GetAAContext retrieves the AAContext for an EVM instance.
func GetAAContext(evm *EVM) *AAContext {
	aaContextMu.RLock()
	defer aaContextMu.RUnlock()
	return aaContextRegistry[evm]
}

// ClearAAContext removes the AAContext association for an EVM instance.
func ClearAAContext(evm *EVM) {
	SetAAContext(evm, nil)
}

// isValidRole checks if a role value is one of the defined AA roles.
func isValidRole(role uint64) bool {
	return role >= AARoleSenderDeployment && role <= AARolePaymasterPostOp
}

// opCurrentRole implements the CURRENT_ROLE opcode (0xab) per EIP-7701.
// Stack: [] -> [current_context_role]
// Pushes the current AA execution role onto the stack.
// During non-AA transactions, pushes AARoleSenderExecution (0xA3).
func opCurrentRole(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	aaCtx := GetAAContext(evm)
	if aaCtx == nil {
		// Per spec: during non-AA transactions, CURRENT_ROLE returns ROLE_SENDER_EXECUTION.
		stack.Push(new(big.Int).SetUint64(AARoleSenderExecution))
		return nil, nil
	}
	stack.Push(new(big.Int).SetUint64(aaCtx.CurrentRole))
	return nil, nil
}

// opAcceptRole implements the ACCEPT_ROLE opcode (0xac) per EIP-7701.
// Stack: [frame_role, offset, length] -> []
// Equivalent to RETURN but also validates and records role acceptance.
// Reverts if frame_role != current_context_role.
func opAcceptRole(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	frameRole := stack.Pop()
	offset := stack.Pop()
	length := stack.Pop()

	aaCtx := GetAAContext(evm)
	if aaCtx == nil {
		return nil, ErrNoAAContext
	}

	// Validate role matches.
	requestedRole := frameRole.Uint64()
	if requestedRole != aaCtx.CurrentRole {
		return nil, ErrRoleMismatch
	}

	if aaCtx.RoleAccepted {
		return nil, ErrRoleAlreadyAccepted
	}

	// Mark role as accepted.
	aaCtx.RoleAccepted = true

	// Return data from memory, like RETURN.
	ret := memory.Get(int64(offset.Uint64()), int64(length.Uint64()))
	return ret, nil
}

// memoryAcceptRole returns the required memory size for ACCEPT_ROLE.
// Stack: [frame_role, offset, length]
// offset is at Back(1), length at Back(2).
func memoryAcceptRole(stack *Stack) (uint64, bool) {
	offset, overflow := bigUint64WithOverflow(stack.Back(1))
	if overflow {
		return 0, true
	}
	length, overflow := bigUint64WithOverflow(stack.Back(2))
	if overflow {
		return 0, true
	}
	return safeAddU_val(offset, length)
}

// NewAAContext creates a new AAContext from an AA transaction's fields.
func NewAAContext(tx *types.AATx, sigHash types.Hash) *AAContext {
	ctx := &AAContext{
		CurrentRole:            AARoleSenderValidation,
		RoleAccepted:           false,
		Sender:                 tx.Sender,
		SenderValidationData:   tx.SenderValidationData,
		SenderExecutionData:    tx.SenderExecutionData,
		PaymasterData:          tx.PaymasterData,
		DeployerData:           tx.DeployerData,
		SenderValidationGas:    tx.SenderValidationGas,
		PaymasterValidationGas: tx.PaymasterValidationGas,
		SenderExecutionGas:     tx.SenderExecutionGas,
		PaymasterPostOpGas:     tx.PaymasterPostOpGas,
		Nonce:                  tx.Nonce,
		TxSigHash:              sigHash,
	}
	if tx.MaxPriorityFeePerGas != nil {
		ctx.MaxPriorityFeePerGas = new(big.Int).Set(tx.MaxPriorityFeePerGas)
	}
	if tx.MaxFeePerGas != nil {
		ctx.MaxFeePerGas = new(big.Int).Set(tx.MaxFeePerGas)
	}
	if tx.Paymaster != nil {
		ctx.Paymaster = *tx.Paymaster
	}
	if tx.Deployer != nil {
		ctx.Deployer = *tx.Deployer
	}
	return ctx
}

// TransitionRole moves the AA context to the next execution role.
// Resets the RoleAccepted flag for the new role.
func (ctx *AAContext) TransitionRole(newRole uint64) {
	ctx.CurrentRole = newRole
	ctx.RoleAccepted = false
}

// EIP7701Operations returns the operation definitions for CURRENT_ROLE and ACCEPT_ROLE.
// The team lead can merge these into the jump table.
func EIP7701Operations() map[OpCode]*operation {
	return map[OpCode]*operation{
		CURRENT_ROLE: {
			execute:     opCurrentRole,
			constantGas: GasQuickStep,
			minStack:    0,
			maxStack:    1023, // pushes 1 item: 1024 - 1
		},
		ACCEPT_ROLE: {
			execute:     opAcceptRole,
			constantGas: 0,
			minStack:    3,
			maxStack:    1024, // pops 3, pushes 0: 1024 + 3 - 0 = net same
			memorySize:  memoryAcceptRole,
			halts:       true, // like RETURN, ends execution
		},
	}
}
