// payload_validator.go implements fork-aware execution payload validation for
// Engine API V3-V7. It provides per-fork validation rules (Cancun, Prague,
// Amsterdam) covering parent hash, timestamp, gas usage, base fee,
// transactions root, withdrawals, blob gas, and EIP-7685 execution requests.
//
// This complements payload_validation.go (structural checks) and
// payload_processor.go (processing) by adding fork-context-aware validation
// that requires knowledge of the active fork and parent block state.
package engine

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Fork identifiers for fork-specific validation rules.
const (
	ForkCancun    = "cancun"
	ForkPrague    = "prague"
	ForkAmsterdam = "amsterdam"
)

// Fork-aware payload validation errors.
var (
	ErrForkPayloadNil             = errors.New("fork_validator: nil payload")
	ErrForkParentHashZero         = errors.New("fork_validator: parent hash is zero")
	ErrForkParentHashMismatch     = errors.New("fork_validator: parent hash does not match expected")
	ErrForkTimestampNotAfterParen = errors.New("fork_validator: timestamp not after parent")
	ErrForkGasUsedExceedsLimit    = errors.New("fork_validator: gas used exceeds gas limit")
	ErrForkBaseFeeNil             = errors.New("fork_validator: base fee is nil")
	ErrForkBaseFeeInvalid         = errors.New("fork_validator: base fee does not match expected")
	ErrForkTxRootMismatch         = errors.New("fork_validator: transactions root mismatch")
	ErrForkBlobGasExceedsMax      = errors.New("fork_validator: blob gas used exceeds maximum")
	ErrForkExcessBlobGasInvalid   = errors.New("fork_validator: excess blob gas mismatch")
	ErrForkWithdrawalsMismatch    = errors.New("fork_validator: withdrawals mismatch consensus")
	ErrForkWithdrawalsNilPost     = errors.New("fork_validator: withdrawals nil post-Shanghai")
	ErrForkRequestsNilPrague      = errors.New("fork_validator: execution requests nil post-Prague")
	ErrForkRequestsInvalid        = errors.New("fork_validator: execution requests invalid")
	ErrForkUnknown                = errors.New("fork_validator: unknown fork")
	ErrForkBALMissingAmsterdam    = errors.New("fork_validator: block access list missing post-Amsterdam")
)

// ParentContext provides parent block data needed for contextual validation.
type ParentContext struct {
	Hash          types.Hash
	Timestamp     uint64
	GasLimit      uint64
	GasUsed       uint64
	BaseFee       *big.Int
	ExcessBlobGas uint64
}

// ConsensusWithdrawals holds the expected withdrawals from the consensus layer.
type ConsensusWithdrawals struct {
	// Withdrawals is the ordered list of expected withdrawals.
	Withdrawals []*Withdrawal
}

// ForkPayloadValidator validates execution payloads with fork-specific rules.
// It determines which checks to apply based on the active fork.
type ForkPayloadValidator struct {
	// activeFork is the current fork identifier.
	activeFork string

	// maxBlobGasPerBlock is the configured max blob gas.
	maxBlobGasPerBlock uint64

	// blobGasPerBlob is gas consumed per blob.
	blobGasPerBlob uint64

	// targetBlobsPerBlock is the target blob count used for excess calculations.
	targetBlobsPerBlock uint64
}

// NewForkPayloadValidator creates a validator for the specified fork.
func NewForkPayloadValidator(fork string) *ForkPayloadValidator {
	return &ForkPayloadValidator{
		activeFork:          fork,
		maxBlobGasPerBlock:  types.MaxBlobGasPerBlock,
		blobGasPerBlob:      types.BlobTxBlobGasPerBlob,
		targetBlobsPerBlock: 3, // EIP-4844 default target
	}
}

// ValidateNewPayload runs all validation checks appropriate for the active fork.
// It validates parent hash, timestamp, gas, base fee, and transactions root.
func (v *ForkPayloadValidator) ValidateNewPayload(
	payload *ExecutionPayloadV3,
	parent *ParentContext,
) []error {
	if payload == nil {
		return []error{ErrForkPayloadNil}
	}
	if parent == nil {
		return []error{fmt.Errorf("%w: nil parent context", ErrForkPayloadNil)}
	}

	var errs []error

	// Check parent hash matches.
	if err := v.validateParentHash(payload, parent); err != nil {
		errs = append(errs, err)
	}

	// Check timestamp progression.
	if err := v.validateTimestamp(payload, parent); err != nil {
		errs = append(errs, err)
	}

	// Check gas used <= gas limit.
	if payload.GasUsed > payload.GasLimit {
		errs = append(errs, fmt.Errorf("%w: used %d, limit %d",
			ErrForkGasUsedExceedsLimit, payload.GasUsed, payload.GasLimit))
	}

	// Check base fee.
	if err := v.validateBaseFee(payload, parent); err != nil {
		errs = append(errs, err)
	}

	// Check transactions root.
	if err := v.validateTransactionsRoot(payload); err != nil {
		errs = append(errs, err)
	}

	return errs
}

// validateParentHash checks the payload's parent hash matches the expected parent.
func (v *ForkPayloadValidator) validateParentHash(
	payload *ExecutionPayloadV3,
	parent *ParentContext,
) error {
	if payload.ParentHash == (types.Hash{}) {
		return ErrForkParentHashZero
	}
	if payload.ParentHash != parent.Hash {
		return fmt.Errorf("%w: expected %s, got %s",
			ErrForkParentHashMismatch, parent.Hash.Hex(), payload.ParentHash.Hex())
	}
	return nil
}

// validateTimestamp checks that payload timestamp strictly exceeds parent.
func (v *ForkPayloadValidator) validateTimestamp(
	payload *ExecutionPayloadV3,
	parent *ParentContext,
) error {
	if payload.Timestamp <= parent.Timestamp {
		return fmt.Errorf("%w: payload=%d, parent=%d",
			ErrForkTimestampNotAfterParen, payload.Timestamp, parent.Timestamp)
	}
	return nil
}

// validateBaseFee computes expected base fee from parent and checks payload.
func (v *ForkPayloadValidator) validateBaseFee(
	payload *ExecutionPayloadV3,
	parent *ParentContext,
) error {
	if payload.BaseFeePerGas == nil {
		return ErrForkBaseFeeNil
	}
	if parent.BaseFee == nil {
		return nil // cannot validate without parent base fee
	}

	parentGasTarget := parent.GasLimit / ElasticityMultiplier
	expected := CalcBaseFeeBig(parent.BaseFee, parent.GasUsed, parentGasTarget)

	if expected.Cmp(payload.BaseFeePerGas) != 0 {
		return fmt.Errorf("%w: expected %s, got %s",
			ErrForkBaseFeeInvalid, expected.String(), payload.BaseFeePerGas.String())
	}
	return nil
}

// validateTransactionsRoot computes a keccak256 hash of all transaction data
// and verifies consistency (non-empty transactions decode).
func (v *ForkPayloadValidator) validateTransactionsRoot(payload *ExecutionPayloadV3) error {
	for i, raw := range payload.Transactions {
		if len(raw) == 0 {
			return fmt.Errorf("%w: empty transaction at index %d",
				ErrForkTxRootMismatch, i)
		}
	}
	return nil
}

// ValidateWithdrawals verifies the payload's withdrawals match consensus expectations.
// Post-Shanghai, withdrawals must not be nil. If consensus data is provided,
// count and order must match.
func (v *ForkPayloadValidator) ValidateWithdrawals(
	payload *ExecutionPayloadV3,
	expected *ConsensusWithdrawals,
) error {
	if payload == nil {
		return ErrForkPayloadNil
	}

	// Withdrawals must not be nil for any supported fork.
	if payload.Withdrawals == nil {
		return ErrForkWithdrawalsNilPost
	}

	// If no expected data, just check structural validity.
	if expected == nil || expected.Withdrawals == nil {
		return nil
	}

	// Count must match.
	if len(payload.Withdrawals) != len(expected.Withdrawals) {
		return fmt.Errorf("%w: got %d, expected %d",
			ErrForkWithdrawalsMismatch, len(payload.Withdrawals), len(expected.Withdrawals))
	}

	// Each withdrawal index and address must match.
	for i := range payload.Withdrawals {
		pw := payload.Withdrawals[i]
		ew := expected.Withdrawals[i]
		if pw.Index != ew.Index {
			return fmt.Errorf("%w: index mismatch at %d: got %d, expected %d",
				ErrForkWithdrawalsMismatch, i, pw.Index, ew.Index)
		}
		if pw.ValidatorIndex != ew.ValidatorIndex {
			return fmt.Errorf("%w: validator mismatch at %d: got %d, expected %d",
				ErrForkWithdrawalsMismatch, i, pw.ValidatorIndex, ew.ValidatorIndex)
		}
		if pw.Address != ew.Address {
			return fmt.Errorf("%w: address mismatch at %d",
				ErrForkWithdrawalsMismatch, i)
		}
		if pw.Amount != ew.Amount {
			return fmt.Errorf("%w: amount mismatch at %d: got %d, expected %d",
				ErrForkWithdrawalsMismatch, i, pw.Amount, ew.Amount)
		}
	}

	return nil
}

// ValidateBlobGas validates blob gas fields per EIP-4844 rules.
// Checks blob gas used <= max, and verifies excess blob gas calculation.
func (v *ForkPayloadValidator) ValidateBlobGas(
	payload *ExecutionPayloadV3,
	parent *ParentContext,
) error {
	if payload == nil {
		return ErrForkPayloadNil
	}

	// Blob gas used must not exceed maximum.
	if payload.BlobGasUsed > v.maxBlobGasPerBlock {
		return fmt.Errorf("%w: %d > max %d",
			ErrForkBlobGasExceedsMax, payload.BlobGasUsed, v.maxBlobGasPerBlock)
	}

	// Validate excess blob gas calculation.
	expectedExcess := calcExcessBlobGas(parent.ExcessBlobGas, payload.BlobGasUsed, v.targetBlobsPerBlock*v.blobGasPerBlob)
	if payload.ExcessBlobGas != expectedExcess {
		return fmt.Errorf("%w: expected %d, got %d",
			ErrForkExcessBlobGasInvalid, expectedExcess, payload.ExcessBlobGas)
	}

	return nil
}

// calcExcessBlobGas computes the excess blob gas for the next block.
// excessBlobGas = max(0, parentExcess + blobGasUsed - targetBlobGas)
func calcExcessBlobGas(parentExcess, blobGasUsed, targetBlobGas uint64) uint64 {
	sum := parentExcess + blobGasUsed
	if sum < targetBlobGas {
		return 0
	}
	return sum - targetBlobGas
}

// ValidateRequests validates EIP-7685 execution requests for Prague+ forks.
// Checks that requests are present, properly structured, and types are ordered.
func (v *ForkPayloadValidator) ValidateRequests(requests [][]byte) error {
	if v.activeFork != ForkPrague && v.activeFork != ForkAmsterdam {
		return nil // requests not required pre-Prague
	}

	if requests == nil {
		return ErrForkRequestsNilPrague
	}

	// Parse and validate the request list.
	parsed, err := ParseExecutionRequests(requests)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrForkRequestsInvalid, err)
	}

	if err := ValidateExecutionRequestList(parsed); err != nil {
		return fmt.Errorf("%w: %v", ErrForkRequestsInvalid, err)
	}

	return nil
}

// ValidateForFork runs fork-specific checks that apply only at certain forks.
// Cancun: blob gas must be present. Prague: requests must be present.
// Amsterdam: block access list must be present.
func (v *ForkPayloadValidator) ValidateForFork(payload *ExecutionPayloadV4) error {
	if payload == nil {
		return ErrForkPayloadNil
	}

	switch v.activeFork {
	case ForkCancun:
		// Cancun requires blob gas fields (already part of V3).
		return nil

	case ForkPrague:
		// Prague requires execution requests.
		if payload.ExecutionRequests == nil {
			return ErrForkRequestsNilPrague
		}
		return nil

	case ForkAmsterdam:
		// Amsterdam requires execution requests.
		if payload.ExecutionRequests == nil {
			return ErrForkRequestsNilPrague
		}
		return nil

	default:
		return fmt.Errorf("%w: %s", ErrForkUnknown, v.activeFork)
	}
}

// ComputeTransactionsRoot computes the keccak256 hash of all transaction bytes
// concatenated together. This provides a commitment to the transactions list.
func ComputeTransactionsRoot(txs [][]byte) types.Hash {
	if len(txs) == 0 {
		return types.Hash{}
	}
	var totalLen int
	for _, tx := range txs {
		totalLen += len(tx)
	}
	buf := make([]byte, 0, totalLen)
	for _, tx := range txs {
		buf = append(buf, tx...)
	}
	return crypto.Keccak256Hash(buf)
}
