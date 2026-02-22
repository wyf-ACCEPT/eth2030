package consensus

import (
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
)

// Header validation errors.
var (
	ErrInvalidParentHash   = errors.New("header: parent hash mismatch")
	ErrInvalidNumber       = errors.New("header: block number is not parent+1")
	ErrInvalidTimestamp    = errors.New("header: timestamp not after parent")
	ErrInvalidGasLimit     = errors.New("header: gas limit change exceeds bounds")
	ErrGasUsedExceedsLimit = errors.New("header: gas used exceeds gas limit")
	ErrExtraDataTooLong    = errors.New("header: extra data exceeds 32 bytes")
	ErrNilHeader           = errors.New("header: header is nil")
	ErrNilParent           = errors.New("header: parent header is nil")
)

// MaxExtraDataBytes is the maximum allowed length for the Extra field.
const MaxExtraDataBytes = 32

// GasLimitBoundDivisor is the bound divisor for gas limit adjustments.
// The gas limit can change by at most parent_gas_limit / 1024 per block.
const GasLimitBoundDivisor uint64 = 1024

// HeaderValidator validates block headers against consensus rules.
type HeaderValidator struct{}

// NewHeaderValidator creates a new HeaderValidator.
func NewHeaderValidator() *HeaderValidator {
	return &HeaderValidator{}
}

// ValidateHeader performs full validation of a block header against its parent.
// It checks: parent hash linkage, block number continuity, timestamp ordering,
// gas limit bounds, gas usage, and extra data length.
func (hv *HeaderValidator) ValidateHeader(header *types.Header, parent *types.Header) error {
	if header == nil {
		return ErrNilHeader
	}
	if parent == nil {
		return ErrNilParent
	}

	// Parent hash must match the parent's hash.
	parentHash := parent.Hash()
	if header.ParentHash != parentHash {
		return ErrInvalidParentHash
	}

	// Block number must be parent + 1.
	if header.Number == nil || parent.Number == nil {
		return ErrInvalidNumber
	}
	expected := new(big.Int).Add(parent.Number, big.NewInt(1))
	if header.Number.Cmp(expected) != 0 {
		return ErrInvalidNumber
	}

	// Timestamp must be strictly after parent.
	if !ValidateTimestamp(parent.Time, header.Time) {
		return ErrInvalidTimestamp
	}

	// Gas limit must be within bounds.
	if !ValidateGasLimit(parent.GasLimit, header.GasLimit) {
		return ErrInvalidGasLimit
	}

	// Gas used must not exceed gas limit.
	if header.GasUsed > header.GasLimit {
		return ErrGasUsedExceedsLimit
	}

	// Extra data must not exceed 32 bytes.
	if len(header.Extra) > MaxExtraDataBytes {
		return ErrExtraDataTooLong
	}

	return nil
}

// ValidateGasLimit checks whether the gas limit change from parent to child
// is within the allowed bounds. The gas limit can change by at most
// parent_gas_limit / GasLimitBoundDivisor per block, and must be at least 1.
func ValidateGasLimit(parentLimit, headerLimit uint64) bool {
	if headerLimit == 0 {
		return false
	}
	bound := parentLimit / GasLimitBoundDivisor
	if bound == 0 {
		bound = 1
	}

	// The difference must be strictly less than the bound.
	var diff uint64
	if headerLimit > parentLimit {
		diff = headerLimit - parentLimit
	} else {
		diff = parentLimit - headerLimit
	}
	return diff < bound
}

// ValidateTimestamp checks that the header timestamp is strictly after
// the parent timestamp.
func ValidateTimestamp(parentTime, headerTime uint64) bool {
	return headerTime > parentTime
}

// CalcDifficulty returns the difficulty for a block. Post-merge (PoS),
// difficulty is always zero.
func CalcDifficulty(parentDifficulty *big.Int, parentTimestamp, currentTimestamp uint64) *big.Int {
	// Under Proof-of-Stake, difficulty is always 0.
	return new(big.Int)
}
