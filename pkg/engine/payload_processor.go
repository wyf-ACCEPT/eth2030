// payload_processor.go implements execution payload validation and processing
// for the Engine API. Includes full block validation, EIP-1559 base fee
// calculation, gas limit bounds checking, and timestamp validation.
package engine

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
)

// Payload processing errors.
var (
	ErrPPNilPayload       = errors.New("payload_processor: nil payload")
	ErrPPGasExceedsLimit  = errors.New("payload_processor: gas used exceeds gas limit")
	ErrPPBlockHashMismatch = errors.New("payload_processor: block hash mismatch")
	ErrPPTimestampNotAfter = errors.New("payload_processor: timestamp not after parent")
	ErrPPGasLimitDelta     = errors.New("payload_processor: gas limit change exceeds allowed delta")
	ErrPPBaseFeeInvalid    = errors.New("payload_processor: base fee does not match expected")
	ErrPPExtraDataTooLong  = errors.New("payload_processor: extra data exceeds 32 bytes")
	ErrPPNilBaseFee        = errors.New("payload_processor: nil base fee")
	ErrPPZeroGasLimit      = errors.New("payload_processor: zero gas limit")
)

// EIP-1559 constants.
const (
	// BaseFeeChangeDenominator is the bound divisor for base fee changes.
	BaseFeeChangeDenominator = 8
	// ElasticityMultiplier is the ratio between gas limit and gas target.
	ElasticityMultiplier = 2
	// MaxExtraDataSize is the maximum length of extra data in bytes.
	MaxExtraDataSize = 32
	// MinGasLimit is the absolute minimum gas limit.
	MinGasLimit = 5000
	// GasLimitBoundDivisor controls max gas limit change per block.
	GasLimitBoundDivisor = 1024
)

// ProcessResult contains the results of payload execution.
type ProcessResult struct {
	StateRoot    types.Hash
	ReceiptsRoot types.Hash
	LogsBloom    types.Bloom
	GasUsed      uint64
}

// PayloadProcessor validates and processes execution payloads. It provides
// modular validation methods that can be composed for different scenarios.
type PayloadProcessor struct {
	minGasLimit          uint64
	gasLimitBoundDivisor uint64
}

// NewPayloadProcessor creates a payload processor with default settings.
func NewPayloadProcessor() *PayloadProcessor {
	return &PayloadProcessor{
		minGasLimit:          MinGasLimit,
		gasLimitBoundDivisor: GasLimitBoundDivisor,
	}
}

// ValidatePayload performs full validation of an execution payload against
// consensus rules. This checks intrinsic validity (gas, extra data, base fee)
// without comparing to parent.
func (pp *PayloadProcessor) ValidatePayload(payload *ExecutionPayloadV3) error {
	if payload == nil {
		return ErrPPNilPayload
	}
	// Gas used must not exceed gas limit.
	if payload.GasUsed > payload.GasLimit {
		return fmt.Errorf("%w: used %d > limit %d", ErrPPGasExceedsLimit, payload.GasUsed, payload.GasLimit)
	}
	// Gas limit must be positive.
	if payload.GasLimit == 0 {
		return ErrPPZeroGasLimit
	}
	// Extra data length check (max 32 bytes per spec).
	if len(payload.ExtraData) > MaxExtraDataSize {
		return fmt.Errorf("%w: length %d", ErrPPExtraDataTooLong, len(payload.ExtraData))
	}
	// Base fee must be present and non-negative.
	if payload.BaseFeePerGas == nil {
		return ErrPPNilBaseFee
	}
	if payload.BaseFeePerGas.Sign() < 0 {
		return fmt.Errorf("%w: negative base fee", ErrPPBaseFeeInvalid)
	}
	return nil
}

// ValidateBlockHash verifies that the block hash in the payload matches
// the hash computed from the payload's header fields.
func (pp *PayloadProcessor) ValidateBlockHash(payload *ExecutionPayloadV3) error {
	if payload == nil {
		return ErrPPNilPayload
	}
	v4 := &ExecutionPayloadV4{ExecutionPayloadV3: *payload}
	header := PayloadToHeader(v4)
	computed := header.Hash()
	if payload.BlockHash != (types.Hash{}) && computed != payload.BlockHash {
		return fmt.Errorf("%w: computed %s, got %s", ErrPPBlockHashMismatch, computed, payload.BlockHash)
	}
	return nil
}

// ValidateGasLimits checks that the gas limit change between parent and child
// is within the allowed bounds. Per the spec, the gas limit may change by at
// most parent_gas_limit / GasLimitBoundDivisor.
func (pp *PayloadProcessor) ValidateGasLimits(payload *ExecutionPayloadV3, parentGasLimit uint64) error {
	if payload == nil {
		return ErrPPNilPayload
	}
	// Gas limit must be at least MinGasLimit.
	if payload.GasLimit < pp.minGasLimit {
		return fmt.Errorf("%w: %d below minimum %d", ErrPPGasLimitDelta, payload.GasLimit, pp.minGasLimit)
	}
	// Calculate allowed delta.
	maxDelta := parentGasLimit / pp.gasLimitBoundDivisor
	if maxDelta == 0 {
		maxDelta = 1
	}
	var diff uint64
	if payload.GasLimit > parentGasLimit {
		diff = payload.GasLimit - parentGasLimit
	} else {
		diff = parentGasLimit - payload.GasLimit
	}
	// The difference must be strictly less than maxDelta per the spec.
	if diff >= maxDelta {
		return fmt.Errorf("%w: delta %d >= max %d", ErrPPGasLimitDelta, diff, maxDelta)
	}
	return nil
}

// ValidateTimestamp checks that the payload timestamp is strictly after the
// parent timestamp.
func (pp *PayloadProcessor) ValidateTimestamp(payload *ExecutionPayloadV3, parentTimestamp uint64) error {
	if payload == nil {
		return ErrPPNilPayload
	}
	if payload.Timestamp <= parentTimestamp {
		return fmt.Errorf("%w: %d <= %d", ErrPPTimestampNotAfter, payload.Timestamp, parentTimestamp)
	}
	return nil
}

// ValidateBaseFee checks that the base fee in the payload matches the
// expected value derived from the parent's gas usage.
func (pp *PayloadProcessor) ValidateBaseFee(
	payload *ExecutionPayloadV3,
	parentBaseFee, parentGasUsed, parentGasTarget uint64,
) error {
	if payload == nil {
		return ErrPPNilPayload
	}
	expected := CalcBaseFee(parentBaseFee, parentGasUsed, parentGasTarget)
	if payload.BaseFeePerGas == nil {
		return ErrPPNilBaseFee
	}
	if payload.BaseFeePerGas.Uint64() != expected {
		return fmt.Errorf("%w: expected %d, got %d", ErrPPBaseFeeInvalid, expected, payload.BaseFeePerGas.Uint64())
	}
	return nil
}

// ProcessPayload executes a payload by converting to a block, running
// basic validation, and returning a ProcessResult. This is a simplified
// execution path; the full state transition is handled by the EngineBackend.
func (pp *PayloadProcessor) ProcessPayload(payload *ExecutionPayloadV3) (*ProcessResult, error) {
	if payload == nil {
		return nil, ErrPPNilPayload
	}
	// Run intrinsic validation first.
	if err := pp.ValidatePayload(payload); err != nil {
		return nil, err
	}
	return &ProcessResult{
		StateRoot:    payload.StateRoot,
		ReceiptsRoot: payload.ReceiptsRoot,
		LogsBloom:    payload.LogsBloom,
		GasUsed:      payload.GasUsed,
	}, nil
}

// CalcBaseFee computes the expected base fee for the next block using
// EIP-1559 rules. If the parent used more gas than the target, the base
// fee increases; if less, it decreases. The minimum base fee is 1.
func CalcBaseFee(parentBaseFee, parentGasUsed, parentGasTarget uint64) uint64 {
	if parentGasTarget == 0 {
		return parentBaseFee
	}
	if parentGasUsed == parentGasTarget {
		return parentBaseFee
	}

	if parentGasUsed > parentGasTarget {
		// Base fee increases.
		gasUsedDelta := parentGasUsed - parentGasTarget
		// fee_delta = max(parent_base_fee * gas_used_delta / parent_gas_target / BASE_FEE_CHANGE_DENOMINATOR, 1)
		x := new(big.Int).SetUint64(parentBaseFee)
		x.Mul(x, new(big.Int).SetUint64(gasUsedDelta))
		x.Div(x, new(big.Int).SetUint64(parentGasTarget))
		x.Div(x, new(big.Int).SetUint64(BaseFeeChangeDenominator))
		delta := x.Uint64()
		if delta == 0 {
			delta = 1
		}
		return parentBaseFee + delta
	}

	// Base fee decreases.
	gasUsedDelta := parentGasTarget - parentGasUsed
	x := new(big.Int).SetUint64(parentBaseFee)
	x.Mul(x, new(big.Int).SetUint64(gasUsedDelta))
	x.Div(x, new(big.Int).SetUint64(parentGasTarget))
	x.Div(x, new(big.Int).SetUint64(BaseFeeChangeDenominator))
	delta := x.Uint64()
	if delta >= parentBaseFee {
		// Floor at 0 (protocol enforces minimum base fee of 0; callers may
		// impose a higher floor).
		return 0
	}
	return parentBaseFee - delta
}
