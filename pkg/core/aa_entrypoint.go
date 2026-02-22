// Package core implements the Ethereum state transition and block processing.
//
// aa_entrypoint.go implements EIP-4337/7701/7702 account abstraction entrypoint
// logic: UserOperation structs, entrypoint validation, paymaster integration,
// nonce management for smart accounts, gas estimation for UserOps, and the
// bundler interface.
package core

import (
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Entrypoint and account abstraction constants.
const (
	// UserOpGasOverhead is the intrinsic gas overhead per UserOperation bundle
	// entry beyond the per-operation gas limits.
	UserOpGasOverhead uint64 = 21000

	// MinVerificationGas is the minimum gas required for account validation.
	MinVerificationGas uint64 = 10000

	// MinExecutionGas is the minimum gas required for execution phase.
	MinExecutionGas uint64 = 21000

	// MaxPaymasterDataLen is the maximum length of paymaster context data.
	MaxPaymasterDataLen = 65536

	// NonceKeyBits is the number of bits for the nonce key (upper 192 bits
	// of the 256-bit nonce slot, per EIP-4337 nonce model).
	NonceKeyBits = 192

	// NonceSeqBits is the number of bits for the sequential nonce (lower 64 bits).
	NonceSeqBits = 64
)

// Account abstraction errors.
var (
	ErrUserOpSenderZero        = errors.New("aa: sender is zero address")
	ErrUserOpGasTooLow         = errors.New("aa: gas limits below minimum")
	ErrUserOpNonce             = errors.New("aa: invalid nonce")
	ErrUserOpSignature         = errors.New("aa: signature verification failed")
	ErrUserOpPaymasterInvalid  = errors.New("aa: paymaster validation failed")
	ErrUserOpPaymasterDeposit  = errors.New("aa: paymaster insufficient deposit")
	ErrUserOpInsufficientFunds = errors.New("aa: sender insufficient funds")
	ErrUserOpCallReverted      = errors.New("aa: execution reverted")
	ErrUserOpDeployFailed      = errors.New("aa: account deployment failed")
	ErrUserOpPaymasterDataLen  = errors.New("aa: paymaster data exceeds maximum length")
	ErrBundleEmpty             = errors.New("aa: bundle has no operations")
	ErrBundleBeneficiaryZero   = errors.New("aa: beneficiary is zero address")
)

// UserOperation represents an ERC-4337 user operation. It encapsulates the
// intent of a smart account owner, including gas parameters, paymaster info,
// and factory data for account deployment.
type UserOperation struct {
	Sender                        types.Address
	Nonce                         *big.Int
	Factory                       *types.Address // nil if account already deployed
	FactoryData                   []byte
	CallData                      []byte
	CallGasLimit                  uint64
	VerificationGasLimit          uint64
	PreVerificationGas            uint64
	MaxFeePerGas                  *big.Int
	MaxPriorityFeePerGas          *big.Int
	Paymaster                     *types.Address // nil if self-sponsored
	PaymasterVerificationGasLimit uint64
	PaymasterPostOpGasLimit       uint64
	PaymasterData                 []byte
	Signature                     []byte
}

// UserOpHash computes the keccak256 hash of the UserOperation for signing.
// The hash covers: pack(sender, nonce, hashCallData, hashFactoryData,
// callGasLimit, verificationGasLimit, preVerificationGas, maxFeePerGas,
// maxPriorityFeePerGas, hashPaymasterData) || entryPoint || chainID.
func UserOpHash(op *UserOperation, chainID *big.Int) types.Hash {
	// ABI-encode core fields into a deterministic byte sequence.
	var buf []byte

	buf = append(buf, op.Sender[:]...)
	if op.Nonce != nil {
		nonceBytes := op.Nonce.Bytes()
		padded := make([]byte, 32)
		copy(padded[32-len(nonceBytes):], nonceBytes)
		buf = append(buf, padded...)
	} else {
		buf = append(buf, make([]byte, 32)...)
	}

	callDataHash := crypto.Keccak256(op.CallData)
	buf = append(buf, callDataHash...)

	factoryHash := crypto.Keccak256(op.FactoryData)
	buf = append(buf, factoryHash...)

	// Pack gas limits as 32-byte big-endian values.
	buf = appendUint64As32(buf, op.CallGasLimit)
	buf = appendUint64As32(buf, op.VerificationGasLimit)
	buf = appendUint64As32(buf, op.PreVerificationGas)

	buf = appendBigAs32(buf, op.MaxFeePerGas)
	buf = appendBigAs32(buf, op.MaxPriorityFeePerGas)

	pmHash := crypto.Keccak256(op.PaymasterData)
	buf = append(buf, pmHash...)

	// Inner hash of the operation fields.
	innerHash := crypto.Keccak256(buf)

	// Outer hash includes entrypoint and chain ID.
	var outer []byte
	outer = append(outer, innerHash...)
	outer = append(outer, types.AAEntryPoint[:]...)
	outer = appendBigAs32(outer, chainID)

	return types.BytesToHash(crypto.Keccak256(outer))
}

// ValidateUserOp performs static validation of a UserOperation. It checks
// field constraints without touching state; use ValidateUserOpState for
// on-chain checks.
func ValidateUserOp(op *UserOperation) error {
	if op.Sender == (types.Address{}) {
		return ErrUserOpSenderZero
	}
	if op.VerificationGasLimit < MinVerificationGas {
		return ErrUserOpGasTooLow
	}
	if op.CallGasLimit < MinExecutionGas {
		return ErrUserOpGasTooLow
	}
	if op.MaxFeePerGas == nil || op.MaxFeePerGas.Sign() <= 0 {
		return ErrUserOpGasTooLow
	}
	if op.MaxPriorityFeePerGas == nil || op.MaxPriorityFeePerGas.Sign() < 0 {
		return ErrUserOpGasTooLow
	}
	if op.MaxPriorityFeePerGas.Cmp(op.MaxFeePerGas) > 0 {
		return ErrUserOpGasTooLow
	}
	if op.Paymaster != nil && len(op.PaymasterData) > MaxPaymasterDataLen {
		return ErrUserOpPaymasterDataLen
	}
	return nil
}

// ValidateUserOpState validates a UserOperation against on-chain state,
// checking the sender nonce and balance sufficiency for the maximum gas cost.
func ValidateUserOpState(op *UserOperation, statedb state.StateDB, baseFee *big.Int) error {
	// Validate nonce: for smart accounts the nonce is a 256-bit value with
	// key (upper 192 bits) and sequence (lower 64 bits).
	expectedNonce := statedb.GetNonce(op.Sender)
	opSeq := extractNonceSeq(op.Nonce)
	if opSeq != expectedNonce {
		return ErrUserOpNonce
	}

	// Calculate the maximum gas cost the sender (or paymaster) must cover.
	maxGasCost := MaxUserOpGasCost(op, baseFee)

	if op.Paymaster != nil {
		// Paymaster pays: verify paymaster balance covers gas.
		pmBal := statedb.GetBalance(*op.Paymaster)
		if pmBal.Cmp(maxGasCost) < 0 {
			return ErrUserOpPaymasterDeposit
		}
	} else {
		// Self-sponsored: verify sender balance covers gas.
		senderBal := statedb.GetBalance(op.Sender)
		if senderBal.Cmp(maxGasCost) < 0 {
			return ErrUserOpInsufficientFunds
		}
	}
	return nil
}

// MaxUserOpGasCost computes the maximum gas cost for a UserOperation in wei.
// This is used for pre-flight balance checks.
func MaxUserOpGasCost(op *UserOperation, baseFee *big.Int) *big.Int {
	totalGas := op.PreVerificationGas + op.VerificationGasLimit + op.CallGasLimit
	if op.Paymaster != nil {
		totalGas += op.PaymasterVerificationGasLimit + op.PaymasterPostOpGasLimit
	}
	// Effective gas price = min(maxFeePerGas, baseFee + maxPriorityFeePerGas).
	effectivePrice := userOpEffectiveGasPrice(op.MaxFeePerGas, op.MaxPriorityFeePerGas, baseFee)
	return new(big.Int).Mul(new(big.Int).SetUint64(totalGas), effectivePrice)
}

// IncrementSmartNonce increments the sequential portion of a smart account's
// nonce (lower 64 bits) for the given nonce key.
func IncrementSmartNonce(statedb state.StateDB, sender types.Address) {
	current := statedb.GetNonce(sender)
	statedb.SetNonce(sender, current+1)
}

// EncodeNonce2D encodes a 2D nonce from a 192-bit key and a 64-bit sequence
// into a 256-bit nonce per the EIP-4337 nonce model.
func EncodeNonce2D(key *big.Int, seq uint64) *big.Int {
	if key == nil {
		return new(big.Int).SetUint64(seq)
	}
	result := new(big.Int).Lsh(key, NonceSeqBits)
	result.Or(result, new(big.Int).SetUint64(seq))
	return result
}

// DecodeNonce2D splits a 256-bit nonce into a 192-bit key and a 64-bit
// sequential value.
func DecodeNonce2D(nonce *big.Int) (key *big.Int, seq uint64) {
	if nonce == nil {
		return new(big.Int), 0
	}
	mask := new(big.Int).SetUint64(^uint64(0))
	seq = new(big.Int).And(nonce, mask).Uint64()
	key = new(big.Int).Rsh(nonce, NonceSeqBits)
	return key, seq
}

// Bundler is the interface for a UserOperation bundler. A bundler collects
// UserOperations from a mempool, validates them, and packages them into an
// on-chain transaction targeting the entrypoint.
type Bundler interface {
	// AddUserOp validates and adds a UserOperation to the bundler mempool.
	AddUserOp(op *UserOperation) error

	// PendingOps returns UserOperations ready for bundling, sorted by
	// effective gas price descending.
	PendingOps(maxOps int) []*UserOperation

	// BuildBundle creates a bundle transaction from pending operations.
	// The beneficiary receives gas refunds.
	BuildBundle(beneficiary types.Address, baseFee *big.Int) (*types.Transaction, error)

	// RemoveOps removes operations by sender after they have been included
	// on-chain or become invalid.
	RemoveOps(sender types.Address)
}

// PaymasterValidator defines the interface for validating paymaster
// willingness to sponsor a UserOperation.
type PaymasterValidator interface {
	// ValidatePaymasterOp verifies the paymaster is willing and able to pay
	// for the given UserOperation. Returns the paymaster context to be passed
	// to the post-operation step, or an error.
	ValidatePaymasterOp(op *UserOperation, statedb state.StateDB) (context []byte, err error)

	// PostOp is called after execution with the actual gas used. The paymaster
	// may refund unused gas or perform cleanup.
	PostOp(op *UserOperation, context []byte, gasUsed uint64, statedb state.StateDB) error
}

// EstimateUserOpGas estimates the gas limits for a UserOperation by simulating
// each phase. It returns recommended CallGasLimit, VerificationGasLimit, and
// PreVerificationGas values.
func EstimateUserOpGas(op *UserOperation, statedb state.StateDB, baseFee *big.Int) (callGas, verifyGas, preVerifyGas uint64) {
	// Pre-verification gas covers the calldata cost of the UserOp itself.
	preVerifyGas = estimatePreVerificationGas(op)

	// Verification gas: base cost + account deployment if factory is set.
	verifyGas = MinVerificationGas
	if op.Factory != nil && len(op.FactoryData) > 0 {
		// Account deployment adds CREATE overhead.
		verifyGas += 32000 + uint64(len(op.FactoryData))*16
	}
	if op.Paymaster != nil {
		verifyGas += op.PaymasterVerificationGasLimit
	}

	// Execution gas estimate from calldata size as a heuristic.
	callGas = MinExecutionGas
	if len(op.CallData) > 0 {
		// Per-byte gas estimate: 16 gas per non-zero byte, 4 per zero byte.
		callGas += calldataCostEstimate(op.CallData)
	}
	return callGas, verifyGas, preVerifyGas
}

// --- Internal helpers ---

// extractNonceSeq returns the lower 64 bits of a nonce.
func extractNonceSeq(nonce *big.Int) uint64 {
	if nonce == nil {
		return 0
	}
	return nonce.Uint64()
}

// userOpEffectiveGasPrice computes min(maxFee, baseFee + tipCap) for a UserOp.
func userOpEffectiveGasPrice(maxFee, tipCap, baseFee *big.Int) *big.Int {
	if baseFee == nil {
		baseFee = new(big.Int)
	}
	effective := new(big.Int).Add(baseFee, tipCap)
	if effective.Cmp(maxFee) > 0 {
		effective.Set(maxFee)
	}
	return effective
}

// estimatePreVerificationGas estimates the calldata gas for the packed
// UserOperation. This covers the cost of including the operation in the
// bundler's transaction.
func estimatePreVerificationGas(op *UserOperation) uint64 {
	// Base overhead per operation.
	gas := UserOpGasOverhead

	// Calldata cost for the operation fields.
	gas += calldataCostEstimate(op.Sender[:])
	if op.Nonce != nil {
		gas += calldataCostEstimate(op.Nonce.Bytes())
	}
	gas += calldataCostEstimate(op.CallData)
	gas += calldataCostEstimate(op.FactoryData)
	gas += calldataCostEstimate(op.PaymasterData)
	gas += calldataCostEstimate(op.Signature)

	return gas
}

// calldataCostEstimate returns the approximate gas for including data in
// calldata: 4 gas per zero byte, 16 gas per non-zero byte.
func calldataCostEstimate(data []byte) uint64 {
	var gas uint64
	for _, b := range data {
		if b == 0 {
			gas += 4
		} else {
			gas += 16
		}
	}
	return gas
}

// appendUint64As32 appends a uint64 as a 32-byte big-endian value.
func appendUint64As32(buf []byte, v uint64) []byte {
	padded := make([]byte, 32)
	b := new(big.Int).SetUint64(v).Bytes()
	copy(padded[32-len(b):], b)
	return append(buf, padded...)
}

// appendBigAs32 appends a big.Int as a 32-byte big-endian value.
func appendBigAs32(buf []byte, v *big.Int) []byte {
	padded := make([]byte, 32)
	if v != nil {
		b := v.Bytes()
		if len(b) > 32 {
			b = b[len(b)-32:]
		}
		copy(padded[32-len(b):], b)
	}
	return append(buf, padded...)
}
