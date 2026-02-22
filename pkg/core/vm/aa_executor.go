package vm

import (
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// AA execution errors.
var (
	ErrAAValidationFailed     = errors.New("aa: validation phase failed")
	ErrAAExecutionFailed      = errors.New("aa: execution phase failed")
	ErrAAPostOpFailed         = errors.New("aa: post-operation phase failed")
	ErrAARoleNotAccepted      = errors.New("aa: role was not accepted during phase")
	ErrAAInsufficientGas      = errors.New("aa: insufficient gas for phase")
	ErrAAPaymasterFailed      = errors.New("aa: paymaster validation failed")
	ErrAANonceMismatch        = errors.New("aa: nonce mismatch")
	ErrAANilAccount           = errors.New("aa: account has no code")
	ErrAANilPaymaster         = errors.New("aa: paymaster has no code")
	ErrAAInvalidTransaction   = errors.New("aa: invalid AA transaction")
	ErrAABundlePartialFailure = errors.New("aa: bundle contains failed transactions")
)

// AATxType is the transaction type byte for EIP-7701 AA transactions.
const AATxType byte = 0x04

// CallerRole identifies the entity performing a call in an AA context.
type CallerRole uint8

const (
	CallerRoleSender    CallerRole = 0
	CallerRolePaymaster CallerRole = 1
	CallerRoleDeployer  CallerRole = 2
	CallerRoleBundler   CallerRole = 3
)

// NonceKey represents a 2D nonce: (key, sequence) per EIP-7701.
type NonceKey struct {
	Key      *big.Int
	Sequence uint64
}

// ValidationResult holds the output of the validation phase.
type ValidationResult struct {
	Success    bool
	GasUsed    uint64
	ReturnData []byte
	// ValidAfter and ValidUntil define a time window for validity.
	ValidAfter uint64
	ValidUntil uint64
}

// AAExecutionResult holds the output of the execution phase.
type AAExecutionResult struct {
	Success    bool
	GasUsed    uint64
	ReturnData []byte
	Reverted   bool
}

// PaymasterResult holds the output of paymaster validation.
type PaymasterResult struct {
	Success    bool
	GasUsed    uint64
	ReturnData []byte
	// PaymasterContext is passed to the post-op phase.
	PaymasterContext []byte
}

// AATx represents an EIP-7701 Account Abstraction transaction.
type AATx struct {
	ChainID       *big.Int
	Sender        types.Address
	Nonce         NonceKey
	Deployer      *types.Address
	DeployerData  []byte
	Paymaster     *types.Address
	PaymasterData []byte

	SenderValidationData []byte
	SenderExecutionData  []byte

	// Per-phase gas limits.
	ValidationGasLimit uint64
	ExecutionGasLimit  uint64
	PostOpGasLimit     uint64

	// Fee parameters.
	MaxPriorityFeePerGas *big.Int
	MaxFeePerGas         *big.Int
}

// AAResult is the outcome of processing a single AA transaction.
type AAResult struct {
	TxHash          types.Hash
	ValidationOK    bool
	ExecutionOK     bool
	PostOpOK        bool
	TotalGasUsed    uint64
	ValidationGas   uint64
	ExecutionGas    uint64
	PostOpGas       uint64
	ReturnData      []byte
	PaymasterResult *PaymasterResult
	Err             error
}

// AAExecutor manages the lifecycle of EIP-7701 AA transactions.
// It is safe for concurrent use.
type AAExecutor struct {
	mu sync.Mutex
}

// NewAAExecutor returns a new thread-safe AA executor.
func NewAAExecutor() *AAExecutor {
	return &AAExecutor{}
}

// ValidatePhase runs the account's validation function per EIP-7701.
// It sets the AA context role to sender validation, executes the account
// code with the validation data as calldata, and checks that the role
// was accepted via ACCEPT_ROLE.
func (ex *AAExecutor) ValidatePhase(
	evm *EVM,
	tx *AATx,
	account types.Address,
) (*ValidationResult, error) {
	ex.mu.Lock()
	defer ex.mu.Unlock()

	if tx == nil {
		return nil, ErrAAInvalidTransaction
	}

	if evm.StateDB == nil {
		return nil, errors.New("aa: no state database")
	}

	// Check that the account has code.
	code := evm.StateDB.GetCode(account)
	if len(code) == 0 {
		return nil, ErrAANilAccount
	}

	// Check gas.
	if tx.ValidationGasLimit == 0 {
		return nil, ErrAAInsufficientGas
	}

	// Set up AA context for the validation role.
	aaCtx := &AAContext{
		CurrentRole:          AARoleSenderValidation,
		RoleAccepted:         false,
		Sender:               tx.Sender,
		SenderValidationData: tx.SenderValidationData,
		SenderExecutionData:  tx.SenderExecutionData,
		SenderValidationGas:  tx.ValidationGasLimit,
		SenderExecutionGas:   tx.ExecutionGasLimit,
		PaymasterPostOpGas:   tx.PostOpGasLimit,
		Nonce:                tx.Nonce.Sequence,
	}

	if tx.MaxPriorityFeePerGas != nil {
		aaCtx.MaxPriorityFeePerGas = new(big.Int).Set(tx.MaxPriorityFeePerGas)
	}
	if tx.MaxFeePerGas != nil {
		aaCtx.MaxFeePerGas = new(big.Int).Set(tx.MaxFeePerGas)
	}
	if tx.Paymaster != nil {
		aaCtx.Paymaster = *tx.Paymaster
		aaCtx.PaymasterData = tx.PaymasterData
		aaCtx.PaymasterValidationGas = tx.ValidationGasLimit / 2
	}
	if tx.Deployer != nil {
		aaCtx.Deployer = *tx.Deployer
		aaCtx.DeployerData = tx.DeployerData
	}

	SetAAContext(evm, aaCtx)
	defer ClearAAContext(evm)

	// Snapshot state for revert on failure.
	snapshot := evm.StateDB.Snapshot()

	// Create the contract for validation.
	contract := NewContract(AAEntryPointAddress, account, new(big.Int), tx.ValidationGasLimit)
	contract.Code = code
	contract.CodeHash = evm.StateDB.GetCodeHash(account)

	// Execute validation.
	evm.depth++
	ret, err := evm.Run(contract, tx.SenderValidationData)
	evm.depth--

	gasUsed := tx.ValidationGasLimit - contract.Gas

	result := &ValidationResult{
		GasUsed:    gasUsed,
		ReturnData: ret,
	}

	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		result.Success = false
		return result, fmt.Errorf("%w: %v", ErrAAValidationFailed, err)
	}

	// Check that the role was accepted.
	if !aaCtx.RoleAccepted {
		evm.StateDB.RevertToSnapshot(snapshot)
		result.Success = false
		return result, ErrAARoleNotAccepted
	}

	result.Success = true
	return result, nil
}

// ExecutePhase runs the actual operation after validation succeeds.
func (ex *AAExecutor) ExecutePhase(
	evm *EVM,
	tx *AATx,
	validationResult *ValidationResult,
) (*AAExecutionResult, error) {
	ex.mu.Lock()
	defer ex.mu.Unlock()

	if tx == nil {
		return nil, ErrAAInvalidTransaction
	}
	if validationResult == nil || !validationResult.Success {
		return nil, ErrAAValidationFailed
	}

	if evm.StateDB == nil {
		return nil, errors.New("aa: no state database")
	}

	code := evm.StateDB.GetCode(tx.Sender)
	if len(code) == 0 {
		return nil, ErrAANilAccount
	}

	if tx.ExecutionGasLimit == 0 {
		return nil, ErrAAInsufficientGas
	}

	// Set up AA context for execution role.
	aaCtx := &AAContext{
		CurrentRole:         AARoleSenderExecution,
		RoleAccepted:        false,
		Sender:              tx.Sender,
		SenderExecutionData: tx.SenderExecutionData,
		SenderExecutionGas:  tx.ExecutionGasLimit,
		Nonce:               tx.Nonce.Sequence,
	}
	if tx.MaxFeePerGas != nil {
		aaCtx.MaxFeePerGas = new(big.Int).Set(tx.MaxFeePerGas)
	}

	SetAAContext(evm, aaCtx)
	defer ClearAAContext(evm)

	snapshot := evm.StateDB.Snapshot()

	contract := NewContract(AAEntryPointAddress, tx.Sender, new(big.Int), tx.ExecutionGasLimit)
	contract.Code = code
	contract.CodeHash = evm.StateDB.GetCodeHash(tx.Sender)

	evm.depth++
	ret, err := evm.Run(contract, tx.SenderExecutionData)
	evm.depth--

	gasUsed := tx.ExecutionGasLimit - contract.Gas

	result := &AAExecutionResult{
		GasUsed:    gasUsed,
		ReturnData: ret,
	}

	if err != nil {
		if errors.Is(err, ErrExecutionReverted) {
			evm.StateDB.RevertToSnapshot(snapshot)
			result.Success = false
			result.Reverted = true
			return result, nil
		}
		evm.StateDB.RevertToSnapshot(snapshot)
		result.Success = false
		return result, fmt.Errorf("%w: %v", ErrAAExecutionFailed, err)
	}

	result.Success = true
	aaCtx.ExecutionStatus = 1 // success
	aaCtx.ExecutionGasUsed = gasUsed
	return result, nil
}

// PostOpPhase runs the post-operation cleanup, typically for paymaster
// finalization. If no paymaster is involved, this is a no-op.
func (ex *AAExecutor) PostOpPhase(
	evm *EVM,
	tx *AATx,
	executionResult *AAExecutionResult,
) error {
	ex.mu.Lock()
	defer ex.mu.Unlock()

	if tx == nil {
		return ErrAAInvalidTransaction
	}

	// If no paymaster, post-op is a no-op.
	if tx.Paymaster == nil {
		return nil
	}

	if evm.StateDB == nil {
		return errors.New("aa: no state database")
	}

	paymasterAddr := *tx.Paymaster
	code := evm.StateDB.GetCode(paymasterAddr)
	if len(code) == 0 {
		return ErrAANilPaymaster
	}

	gasLimit := tx.PostOpGasLimit
	if gasLimit == 0 {
		return nil // no post-op gas allocated
	}

	// Set up AA context for paymaster post-op.
	aaCtx := &AAContext{
		CurrentRole:   AARolePaymasterPostOp,
		RoleAccepted:  false,
		Sender:        tx.Sender,
		Paymaster:     paymasterAddr,
		PaymasterData: tx.PaymasterData,
		Nonce:         tx.Nonce.Sequence,
	}
	if executionResult != nil {
		if executionResult.Success {
			aaCtx.ExecutionStatus = 1
		} else {
			aaCtx.ExecutionStatus = 2
		}
		aaCtx.ExecutionGasUsed = executionResult.GasUsed
	}

	SetAAContext(evm, aaCtx)
	defer ClearAAContext(evm)

	snapshot := evm.StateDB.Snapshot()

	contract := NewContract(AAEntryPointAddress, paymasterAddr, new(big.Int), gasLimit)
	contract.Code = code
	contract.CodeHash = evm.StateDB.GetCodeHash(paymasterAddr)

	// Build post-op calldata: execution status + gas used.
	postOpData := make([]byte, 64)
	if executionResult != nil {
		big.NewInt(int64(aaCtx.ExecutionStatus)).FillBytes(postOpData[:32])
		big.NewInt(int64(executionResult.GasUsed)).FillBytes(postOpData[32:64])
	}

	evm.depth++
	_, err := evm.Run(contract, postOpData)
	evm.depth--

	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		return fmt.Errorf("%w: %v", ErrAAPostOpFailed, err)
	}

	return nil
}

// ValidatePaymaster runs the paymaster's validation function.
func (ex *AAExecutor) ValidatePaymaster(
	evm *EVM,
	tx *AATx,
	paymaster types.Address,
) (*PaymasterResult, error) {
	ex.mu.Lock()
	defer ex.mu.Unlock()

	if tx == nil {
		return nil, ErrAAInvalidTransaction
	}

	if evm.StateDB == nil {
		return nil, errors.New("aa: no state database")
	}

	code := evm.StateDB.GetCode(paymaster)
	if len(code) == 0 {
		return nil, ErrAANilPaymaster
	}

	gasLimit := tx.ValidationGasLimit / 2
	if gasLimit == 0 {
		return nil, ErrAAInsufficientGas
	}

	// Set up AA context for paymaster validation.
	aaCtx := &AAContext{
		CurrentRole:            AARolePaymasterValidation,
		RoleAccepted:           false,
		Sender:                 tx.Sender,
		Paymaster:              paymaster,
		PaymasterData:          tx.PaymasterData,
		PaymasterValidationGas: gasLimit,
		Nonce:                  tx.Nonce.Sequence,
	}

	SetAAContext(evm, aaCtx)
	defer ClearAAContext(evm)

	snapshot := evm.StateDB.Snapshot()

	contract := NewContract(AAEntryPointAddress, paymaster, new(big.Int), gasLimit)
	contract.Code = code
	contract.CodeHash = evm.StateDB.GetCodeHash(paymaster)

	evm.depth++
	ret, err := evm.Run(contract, tx.PaymasterData)
	evm.depth--

	gasUsed := gasLimit - contract.Gas

	result := &PaymasterResult{
		GasUsed:    gasUsed,
		ReturnData: ret,
	}

	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		result.Success = false
		return result, fmt.Errorf("%w: %v", ErrAAPaymasterFailed, err)
	}

	if !aaCtx.RoleAccepted {
		evm.StateDB.RevertToSnapshot(snapshot)
		result.Success = false
		return result, ErrAARoleNotAccepted
	}

	result.Success = true
	result.PaymasterContext = ret
	return result, nil
}

// ProcessBundle processes a bundle of AA transactions sequentially.
// Each transaction goes through validation, execution, and post-op phases.
// Failed transactions are recorded in the results but do not abort the bundle.
func (ex *AAExecutor) ProcessBundle(
	evm *EVM,
	txs []*AATx,
) ([]*AAResult, error) {
	if len(txs) == 0 {
		return nil, nil
	}

	results := make([]*AAResult, len(txs))
	hasFailure := false

	for i, tx := range txs {
		result := &AAResult{}
		results[i] = result

		if tx == nil {
			result.Err = ErrAAInvalidTransaction
			hasFailure = true
			continue
		}

		// Phase 1: Validate sender.
		valResult, err := ex.ValidatePhase(evm, tx, tx.Sender)
		if err != nil {
			result.Err = err
			result.ValidationOK = false
			if valResult != nil {
				result.ValidationGas = valResult.GasUsed
				result.TotalGasUsed += valResult.GasUsed
			}
			hasFailure = true
			continue
		}
		result.ValidationOK = true
		result.ValidationGas = valResult.GasUsed
		result.TotalGasUsed += valResult.GasUsed

		// Phase 1b: Validate paymaster (if present).
		if tx.Paymaster != nil {
			pmResult, err := ex.ValidatePaymaster(evm, tx, *tx.Paymaster)
			result.PaymasterResult = pmResult
			if err != nil {
				result.Err = err
				result.ValidationOK = false
				if pmResult != nil {
					result.TotalGasUsed += pmResult.GasUsed
				}
				hasFailure = true
				continue
			}
			result.TotalGasUsed += pmResult.GasUsed
		}

		// Phase 2: Execute.
		execResult, err := ex.ExecutePhase(evm, tx, valResult)
		if err != nil {
			result.Err = err
			result.ExecutionOK = false
			if execResult != nil {
				result.ExecutionGas = execResult.GasUsed
				result.TotalGasUsed += execResult.GasUsed
				result.ReturnData = execResult.ReturnData
			}
			hasFailure = true
			continue
		}
		result.ExecutionOK = execResult.Success
		result.ExecutionGas = execResult.GasUsed
		result.TotalGasUsed += execResult.GasUsed
		result.ReturnData = execResult.ReturnData

		if !execResult.Success {
			hasFailure = true
		}

		// Phase 3: Post-op.
		if err := ex.PostOpPhase(evm, tx, execResult); err != nil {
			result.PostOpOK = false
			result.Err = err
			hasFailure = true
			continue
		}
		result.PostOpOK = true
	}

	if hasFailure {
		return results, ErrAABundlePartialFailure
	}
	return results, nil
}

// CheckNonce validates the 2D nonce against the current account state.
// The nonce key is stored as a slot in the account's storage:
// slot = keccak256(key) and the value is the expected sequence number.
func (ex *AAExecutor) CheckNonce(
	stateDB StateDB,
	account types.Address,
	nonce NonceKey,
) error {
	if stateDB == nil {
		return errors.New("aa: no state database")
	}

	// For key == 0, use the standard nonce.
	if nonce.Key == nil || nonce.Key.Sign() == 0 {
		currentNonce := stateDB.GetNonce(account)
		if currentNonce != nonce.Sequence {
			return fmt.Errorf("%w: expected %d, got %d",
				ErrAANonceMismatch, nonce.Sequence, currentNonce)
		}
		return nil
	}

	// For non-zero key, compute the storage slot.
	keyBytes := make([]byte, 32)
	nonce.Key.FillBytes(keyBytes)
	slot := types.BytesToHash(keyBytes)

	currentVal := stateDB.GetState(account, slot)
	current := new(big.Int).SetBytes(currentVal[:])

	expected := new(big.Int).SetUint64(nonce.Sequence)
	if current.Cmp(expected) != 0 {
		return fmt.Errorf("%w: key %s expected seq %d, got %s",
			ErrAANonceMismatch, nonce.Key, nonce.Sequence, current)
	}

	return nil
}

// IncrementNonce advances the nonce for the given key.
func (ex *AAExecutor) IncrementNonce(
	stateDB StateDB,
	account types.Address,
	nonce NonceKey,
) {
	if stateDB == nil {
		return
	}

	if nonce.Key == nil || nonce.Key.Sign() == 0 {
		currentNonce := stateDB.GetNonce(account)
		stateDB.SetNonce(account, currentNonce+1)
		return
	}

	keyBytes := make([]byte, 32)
	nonce.Key.FillBytes(keyBytes)
	slot := types.BytesToHash(keyBytes)

	currentVal := stateDB.GetState(account, slot)
	current := new(big.Int).SetBytes(currentVal[:])
	next := new(big.Int).Add(current, big.NewInt(1))

	nextBytes := make([]byte, 32)
	next.FillBytes(nextBytes)
	stateDB.SetState(account, slot, types.BytesToHash(nextBytes))
}
