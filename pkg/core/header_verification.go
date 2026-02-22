package core

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
)

// Header chain verification errors.
var (
	ErrTimestampNonMonotonic = errors.New("timestamp not monotonically increasing")
	ErrHeaderChainBroken     = errors.New("parent hash mismatch in header chain")
	ErrGasLimitJump          = errors.New("gas limit change exceeds 1/1024 bound")
	ErrBaseFeeComputation    = errors.New("base fee does not match expected computation")
	ErrBlobGasComputation    = errors.New("excess blob gas does not match expected computation")
	ErrDifficultyPostMerge   = errors.New("non-zero difficulty in post-merge header")
	ErrNoncePostMerge        = errors.New("non-zero nonce in post-merge header")
	ErrUnclesPostMerge       = errors.New("non-empty uncle hash in post-merge header")
	ErrExtraDataOverflow     = errors.New("extra data exceeds maximum length")
	ErrBlockNumberGap        = errors.New("block number gap in header chain")
	ErrGasUsedExceedsLimit   = errors.New("gas used exceeds gas limit in header")
	ErrBaseFeeNil            = errors.New("missing base fee in post-London header")
	ErrBlobFieldsMissing     = errors.New("missing blob gas fields in post-Cancun header")
	ErrCalldataFieldsMissing = errors.New("missing calldata gas fields in post-Glamsterdan header")
)

// HeaderVerifier performs multi-header chain verification, checking
// consensus rules across a sequence of headers. It validates parent-child
// relationships, PoS transition correctness, EIP-1559 base fee continuity,
// gas limit bounds, and EIP-4844 blob gas accounting.
type HeaderVerifier struct {
	config *ChainConfig
}

// NewHeaderVerifier creates a new header chain verifier with the given config.
func NewHeaderVerifier(config *ChainConfig) *HeaderVerifier {
	return &HeaderVerifier{config: config}
}

// VerifyChain validates a contiguous sequence of headers starting from a
// trusted parent. Headers must be in ascending order and form a valid chain.
// Returns the index of the first invalid header and the error, or
// (len(headers), nil) if all headers are valid.
func (v *HeaderVerifier) VerifyChain(parent *types.Header, headers []*types.Header) (int, error) {
	if len(headers) == 0 {
		return 0, nil
	}

	current := parent
	for i, header := range headers {
		if err := v.VerifyAgainstParent(header, current); err != nil {
			return i, fmt.Errorf("header %d (block %v): %w", i, header.Number, err)
		}
		current = header
	}
	return len(headers), nil
}

// VerifyAgainstParent validates a single header against its parent,
// checking all consensus rules including PoS fields, EIP-1559, EIP-4844,
// and EIP-7706.
func (v *HeaderVerifier) VerifyAgainstParent(header, parent *types.Header) error {
	// 1. Parent hash linkage.
	if err := verifyParentHash(header, parent); err != nil {
		return err
	}

	// 2. Block number continuity: child = parent + 1.
	if err := verifyBlockNumber(header, parent); err != nil {
		return err
	}

	// 3. Timestamp monotonicity: child.Time > parent.Time.
	if err := verifyTimestampMonotonicity(header, parent); err != nil {
		return err
	}

	// 4. Extra data length limit (32 bytes).
	if err := verifyExtraDataLimit(header); err != nil {
		return err
	}

	// 5. Gas limit bounds (min/max and 1/1024 change limit).
	if err := verifyGasLimitBounds(header, parent); err != nil {
		return err
	}

	// 6. Gas used must not exceed gas limit.
	if err := verifyGasUsedBound(header); err != nil {
		return err
	}

	// 7. Post-merge (PoS) transition checks.
	if err := verifyPoSTransition(header); err != nil {
		return err
	}

	// 8. EIP-1559 base fee verification.
	if err := v.verifyBaseFee(header, parent); err != nil {
		return err
	}

	// 9. EIP-4844 excess blob gas verification.
	if err := v.verifyBlobGas(header, parent); err != nil {
		return err
	}

	// 10. EIP-7706 calldata gas verification.
	if err := v.verifyCalldataGas(header, parent); err != nil {
		return err
	}

	return nil
}

// verifyParentHash checks that header.ParentHash matches parent.Hash().
func verifyParentHash(header, parent *types.Header) error {
	expected := parent.Hash()
	if header.ParentHash != expected {
		return fmt.Errorf("%w: header parent_hash=%s, parent hash=%s",
			ErrHeaderChainBroken, header.ParentHash.Hex(), expected.Hex())
	}
	return nil
}

// verifyBlockNumber checks that header.Number == parent.Number + 1.
func verifyBlockNumber(header, parent *types.Header) error {
	if header.Number == nil || parent.Number == nil {
		return fmt.Errorf("%w: nil block number", ErrBlockNumberGap)
	}
	expected := new(big.Int).Add(parent.Number, big.NewInt(1))
	if header.Number.Cmp(expected) != 0 {
		return fmt.Errorf("%w: want %v, got %v",
			ErrBlockNumberGap, expected, header.Number)
	}
	return nil
}

// verifyTimestampMonotonicity checks that child timestamp strictly
// exceeds parent timestamp.
func verifyTimestampMonotonicity(header, parent *types.Header) error {
	if header.Time <= parent.Time {
		return fmt.Errorf("%w: child=%d, parent=%d",
			ErrTimestampNonMonotonic, header.Time, parent.Time)
	}
	return nil
}

// verifyExtraDataLimit checks that the extra data does not exceed
// the protocol maximum of 32 bytes.
func verifyExtraDataLimit(header *types.Header) error {
	if len(header.Extra) > MaxExtraDataSize {
		return fmt.Errorf("%w: len=%d, max=%d",
			ErrExtraDataOverflow, len(header.Extra), MaxExtraDataSize)
	}
	return nil
}

// verifyGasLimitBounds checks that the gas limit is within the
// allowed range [MinGasLimit, MaxGasLimit] and that the change from
// parent does not exceed 1/1024 of the parent gas limit.
func verifyGasLimitBounds(header, parent *types.Header) error {
	if header.GasLimit < MinGasLimit {
		return fmt.Errorf("%w: %d < %d",
			ErrGasLimitTooLow, header.GasLimit, MinGasLimit)
	}
	if header.GasLimit > MaxGasLimit {
		return fmt.Errorf("%w: %d > %d",
			ErrGasLimitTooHigh, header.GasLimit, MaxGasLimit)
	}

	// The gas limit may change by at most 1/GasLimitBoundDivisor per block.
	var diff uint64
	if header.GasLimit > parent.GasLimit {
		diff = header.GasLimit - parent.GasLimit
	} else {
		diff = parent.GasLimit - header.GasLimit
	}
	bound := parent.GasLimit / GasLimitBoundDivisor
	if diff >= bound {
		return fmt.Errorf("%w: delta=%d, max_allowed=%d (parent=%d)",
			ErrGasLimitJump, diff, bound, parent.GasLimit)
	}
	return nil
}

// verifyGasUsedBound checks that header.GasUsed <= header.GasLimit.
func verifyGasUsedBound(header *types.Header) error {
	if header.GasUsed > header.GasLimit {
		return fmt.Errorf("%w: used=%d, limit=%d",
			ErrGasUsedExceedsLimit, header.GasUsed, header.GasLimit)
	}
	return nil
}

// verifyPoSTransition checks post-merge consensus fields:
//   - Difficulty must be zero (PoS has no mining difficulty)
//   - Nonce must be zero (no PoW nonce)
//   - Uncle hash must be empty (no uncles in PoS)
func verifyPoSTransition(header *types.Header) error {
	if header.Difficulty != nil && header.Difficulty.Sign() != 0 {
		return fmt.Errorf("%w: difficulty=%v",
			ErrDifficultyPostMerge, header.Difficulty)
	}
	if header.Nonce != (types.BlockNonce{}) {
		return fmt.Errorf("%w: nonce=%x",
			ErrNoncePostMerge, header.Nonce)
	}
	if header.UncleHash != (types.Hash{}) && header.UncleHash != EmptyUncleHash {
		return fmt.Errorf("%w: uncle_hash=%s",
			ErrUnclesPostMerge, header.UncleHash.Hex())
	}
	return nil
}

// verifyBaseFee validates the EIP-1559 base fee for a header.
//
// The base fee adjusts dynamically based on parent block gas usage:
//   - If parent gas used equals the target (limit/2), base fee is unchanged
//   - If parent gas used > target, base fee increases (up to 12.5%)
//   - If parent gas used < target, base fee decreases (up to 12.5%)
//   - Minimum base fee is enforced at 7 wei
//
// The formula is:
//
//	new_base_fee = parent_base_fee + parent_base_fee * delta / target / 8
//
// where delta = abs(parent_gas_used - target).
func (v *HeaderVerifier) verifyBaseFee(header, parent *types.Header) error {
	if header.BaseFee == nil {
		// Pre-London blocks have no base fee.
		return nil
	}

	expected := CalcBaseFee(parent)
	if header.BaseFee.Cmp(expected) != 0 {
		return fmt.Errorf("%w: have=%v, want=%v",
			ErrBaseFeeComputation, header.BaseFee, expected)
	}
	return nil
}

// verifyBlobGas validates the EIP-4844 blob gas fields for post-Cancun headers.
//
// The excess blob gas mechanism tracks how much blob gas has been used
// above or below the per-block target. The excess accumulates or drains
// across blocks:
//
//	excess[N] = max(0, excess[N-1] + used[N-1] - target)
//
// This drives the blob base fee through an exponential function,
// creating a market that adjusts blob prices toward the target usage.
func (v *HeaderVerifier) verifyBlobGas(header, parent *types.Header) error {
	if v.config == nil || !v.config.IsCancun(header.Time) {
		return nil
	}

	if header.BlobGasUsed == nil {
		return fmt.Errorf("%w: BlobGasUsed is nil", ErrBlobFieldsMissing)
	}
	if header.ExcessBlobGas == nil {
		return fmt.Errorf("%w: ExcessBlobGas is nil", ErrBlobFieldsMissing)
	}

	// Use fork-aware validation when Prague+ is active (supports BPO schedules).
	if v.config.IsPrague(header.Time) {
		return ValidateBlockBlobGasWithConfig(v.config, header, parent)
	}

	// Cancun-era validation (original EIP-4844 parameters).
	return ValidateBlockBlobGas(header, parent)
}

// verifyCalldataGas validates the EIP-7706 calldata gas fields for
// post-Glamsterdan headers. The calldata gas dimension mirrors blob gas:
// a separate base fee, gas limit, and excess tracking mechanism.
func (v *HeaderVerifier) verifyCalldataGas(header, parent *types.Header) error {
	if v.config == nil || !v.config.IsGlamsterdan(header.Time) {
		return nil
	}

	if header.CalldataGasUsed == nil || header.CalldataExcessGas == nil {
		return fmt.Errorf("%w: CalldataGasUsed=%v, CalldataExcessGas=%v",
			ErrCalldataFieldsMissing, header.CalldataGasUsed, header.CalldataExcessGas)
	}

	return ValidateCalldataGas(header, parent)
}

// VerifyTimestampWindow checks that a header's timestamp is not too far
// in the future relative to the given wall clock time. The allowedDrift
// parameter specifies the maximum number of seconds a header timestamp
// may exceed currentTime.
//
// This is used during block import to reject headers that claim
// unreasonable future timestamps, which could disrupt slot timing.
func VerifyTimestampWindow(header *types.Header, currentTime uint64, allowedDrift uint64) error {
	if header.Time > currentTime+allowedDrift {
		return fmt.Errorf("%w: header time %d exceeds current time %d + drift %d",
			ErrFutureBlock, header.Time, currentTime, allowedDrift)
	}
	return nil
}

// CalcGasLimitRange returns the minimum and maximum gas limit allowed
// for the next block, given the parent gas limit. The gas limit may
// change by at most parent/1024 - 1 per block.
func CalcGasLimitRange(parentGasLimit uint64) (min, max uint64) {
	bound := parentGasLimit / GasLimitBoundDivisor
	if bound == 0 {
		bound = 1
	}

	// Minimum: max(parent - (bound-1), MinGasLimit)
	min = MinGasLimit
	if parentGasLimit > bound-1 {
		candidate := parentGasLimit - (bound - 1)
		if candidate > min {
			min = candidate
		}
	}

	// Maximum: min(parent + (bound-1), MaxGasLimit)
	max = parentGasLimit + (bound - 1)
	if max > MaxGasLimit {
		max = MaxGasLimit
	}
	if max < min {
		max = min
	}
	return min, max
}

// VerifyBaseFeeFromScratch computes and compares the expected base fee
// for a header given a parent. Unlike verifyBaseFee, this is an exported
// function for use by external validators that do not hold a HeaderVerifier.
func VerifyBaseFeeFromScratch(header, parent *types.Header) error {
	if header.BaseFee == nil && parent.BaseFee == nil {
		// Both pre-London: no base fee to verify.
		return nil
	}
	if header.BaseFee == nil && parent.BaseFee != nil {
		return fmt.Errorf("%w: parent has base fee but child does not", ErrBaseFeeNil)
	}

	expected := CalcBaseFee(parent)
	if header.BaseFee.Cmp(expected) != 0 {
		return fmt.Errorf("%w: have=%v, want=%v",
			ErrBaseFeeComputation, header.BaseFee, expected)
	}
	return nil
}
