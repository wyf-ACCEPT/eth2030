package core

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
	"golang.org/x/crypto/sha3"
)

// Block validation errors.
var (
	ErrUnknownParent      = errors.New("unknown parent")
	ErrFutureBlock        = errors.New("block in the future")
	ErrInvalidNumber      = errors.New("invalid block number")
	ErrInvalidGasLimit    = errors.New("invalid gas limit")
	ErrInvalidGasUsed     = errors.New("gas used exceeds gas limit")
	ErrInvalidTimestamp    = errors.New("timestamp not greater than parent")
	ErrExtraDataTooLong   = errors.New("extra data too long")
	ErrInvalidBaseFee     = errors.New("invalid base fee")
	ErrInvalidDifficulty  = errors.New("invalid difficulty for post-merge block")
	ErrInvalidUncleHash   = errors.New("invalid uncle hash for post-merge block")
	ErrInvalidNonce       = errors.New("invalid nonce for post-merge block")
	ErrInvalidRequestHash       = errors.New("invalid requests hash")
	ErrInvalidBlockAccessList   = errors.New("invalid block access list hash")
	ErrMissingBlockAccessList   = errors.New("missing block access list hash")
)

const (
	// MaxExtraDataSize is the maximum allowed extra data in a block header.
	MaxExtraDataSize = 32

	// GasLimitBoundDivisor is the divisor for max gas limit change per block.
	GasLimitBoundDivisor uint64 = 1024

	// MinGasLimit is the minimum gas limit.
	MinGasLimit uint64 = 5000

	// MaxGasLimit is the maximum gas limit (2^63 - 1).
	MaxGasLimit uint64 = 1<<63 - 1

	// ElasticityMultiplier is the EIP-1559 elasticity multiplier.
	ElasticityMultiplier uint64 = 2

	// BaseFeeChangeDenominator is the EIP-1559 base fee change denominator.
	BaseFeeChangeDenominator uint64 = 8
)

// EmptyUncleHash is the keccak256 of RLP([]) — the hash of an empty uncle list.
// RLP of an empty list is 0xc0; keccak256(0xc0) = 1dcc4de8...
var EmptyUncleHash = func() types.Hash {
	d := sha3.NewLegacyKeccak256()
	d.Write([]byte{0xc0}) // RLP empty list
	var h types.Hash
	copy(h[:], d.Sum(nil))
	return h
}()

// BlockValidator validates block headers against consensus rules.
type BlockValidator struct {
	config *ChainConfig
}

// NewBlockValidator creates a new block validator.
func NewBlockValidator(config *ChainConfig) *BlockValidator {
	return &BlockValidator{config: config}
}

// ValidateHeader checks whether a header conforms to the consensus rules.
// The parent header must be provided for validation.
func (v *BlockValidator) ValidateHeader(header, parent *types.Header) error {
	// Verify parent hash matches.
	if header.ParentHash != parent.Hash() {
		return fmt.Errorf("%w: want %v, got %v", ErrUnknownParent, parent.Hash(), header.ParentHash)
	}

	// Verify extra data length.
	if len(header.Extra) > MaxExtraDataSize {
		return fmt.Errorf("%w: %d > %d", ErrExtraDataTooLong, len(header.Extra), MaxExtraDataSize)
	}

	// Verify timestamp is strictly greater than parent.
	if header.Time <= parent.Time {
		return fmt.Errorf("%w: child %d <= parent %d", ErrInvalidTimestamp, header.Time, parent.Time)
	}

	// Verify block number = parent number + 1.
	expected := new(big.Int).Add(parent.Number, big.NewInt(1))
	if header.Number.Cmp(expected) != 0 {
		return fmt.Errorf("%w: want %v, got %v", ErrInvalidNumber, expected, header.Number)
	}

	// Verify gas limit bounds (change per block limited to 1/1024).
	if err := verifyGasLimit(parent.GasLimit, header.GasLimit); err != nil {
		return err
	}

	// Verify gas used <= gas limit.
	if header.GasUsed > header.GasLimit {
		return fmt.Errorf("%w: %d > %d", ErrInvalidGasUsed, header.GasUsed, header.GasLimit)
	}

	// Post-merge (PoS) checks: difficulty = 0, nonce = 0, no uncles.
	if err := verifyPostMerge(header); err != nil {
		return err
	}

	// EIP-1559: verify base fee.
	if header.BaseFee != nil {
		expectedBaseFee := CalcBaseFee(parent)
		if header.BaseFee.Cmp(expectedBaseFee) != 0 {
			return fmt.Errorf("%w: want %v, got %v", ErrInvalidBaseFee, expectedBaseFee, header.BaseFee)
		}
	}

	// EIP-4844: verify blob gas fields for Cancun+ blocks.
	if v.config != nil && v.config.IsCancun(header.Time) {
		if err := ValidateBlockBlobGas(header, parent); err != nil {
			return err
		}
	}

	return nil
}

// ValidateBody checks the block body (transactions, uncles, withdrawals) against the header.
func (v *BlockValidator) ValidateBody(block *types.Block) error {
	header := block.Header()

	// Post-merge: no uncles allowed.
	if len(block.Uncles()) > 0 {
		return ErrInvalidUncleHash
	}

	// EIP-4844: validate blob gas used matches the sum of blob gas from transactions.
	if v.config != nil && v.config.IsCancun(header.Time) {
		var totalBlobGas uint64
		for _, tx := range block.Transactions() {
			totalBlobGas += CountBlobGas(tx)
		}
		if header.BlobGasUsed != nil && *header.BlobGasUsed != totalBlobGas {
			return fmt.Errorf("blob gas used mismatch: header %d, computed %d", *header.BlobGasUsed, totalBlobGas)
		}
	}

	// Validate withdrawals for post-Shanghai blocks.
	if v.config != nil && v.config.IsShanghai(header.Time) {
		if block.Withdrawals() == nil {
			return errors.New("post-Shanghai block missing withdrawals")
		}
	}

	return nil
}

// ValidateRequests verifies the requests_hash field in a post-Prague block
// header matches the computed hash from the provided requests. This should
// be called after ProcessRequests has collected all execution layer requests.
//
// Per EIP-7685:
//   - Post-Prague blocks MUST include a requests_hash in the header.
//   - The hash must match SHA-256 commitment over all typed requests.
//   - Pre-Prague blocks must NOT have a requests_hash.
func (v *BlockValidator) ValidateRequests(header *types.Header, requests types.Requests) error {
	isPrague := v.config != nil && v.config.IsPrague(header.Time)

	if !isPrague {
		// Pre-Prague: requests_hash must not be present.
		if header.RequestsHash != nil {
			return fmt.Errorf("%w: pre-Prague block has requests_hash", ErrInvalidRequestHash)
		}
		return nil
	}

	// Post-Prague: requests_hash must be present.
	if header.RequestsHash == nil {
		return fmt.Errorf("%w: post-Prague block missing requests_hash", ErrInvalidRequestHash)
	}

	return types.ValidateRequestsHash(header, requests)
}

// ValidateBlockAccessList verifies the Block Access List (BAL) hash in the
// header. Per EIP-7928:
//   - Post-Amsterdam blocks MUST include a BlockAccessListHash.
//   - The hash must match the computed hash from re-executing the block.
//   - Pre-Amsterdam blocks must NOT have a BlockAccessListHash.
func (v *BlockValidator) ValidateBlockAccessList(header *types.Header, computedBALHash *types.Hash) error {
	isAmsterdam := v.config != nil && v.config.IsAmsterdam(header.Time)

	if !isAmsterdam {
		// Pre-Amsterdam: BlockAccessListHash must not be present.
		if header.BlockAccessListHash != nil {
			return fmt.Errorf("%w: pre-Amsterdam block has BlockAccessListHash", ErrInvalidBlockAccessList)
		}
		return nil
	}

	// Post-Amsterdam: BlockAccessListHash must be present.
	if header.BlockAccessListHash == nil {
		return fmt.Errorf("%w: post-Amsterdam block missing BlockAccessListHash", ErrMissingBlockAccessList)
	}

	// If no computed hash is provided (e.g. empty block), use empty BAL hash.
	if computedBALHash == nil {
		return fmt.Errorf("%w: no computed BAL hash available for comparison", ErrInvalidBlockAccessList)
	}

	// Verify the hash matches.
	if *header.BlockAccessListHash != *computedBALHash {
		return fmt.Errorf("%w: header=%s computed=%s", ErrInvalidBlockAccessList,
			header.BlockAccessListHash.Hex(), computedBALHash.Hex())
	}

	return nil
}

// verifyGasLimit checks that the gas limit change is within bounds.
func verifyGasLimit(parentGasLimit, headerGasLimit uint64) error {
	if headerGasLimit < MinGasLimit {
		return fmt.Errorf("%w: %d < minimum %d", ErrInvalidGasLimit, headerGasLimit, MinGasLimit)
	}
	if headerGasLimit > MaxGasLimit {
		return fmt.Errorf("%w: %d > maximum %d", ErrInvalidGasLimit, headerGasLimit, MaxGasLimit)
	}

	// Gas limit can change by at most 1/1024 per block.
	diff := headerGasLimit
	if headerGasLimit < parentGasLimit {
		diff = parentGasLimit - headerGasLimit
	} else {
		diff = headerGasLimit - parentGasLimit
	}
	limit := parentGasLimit / GasLimitBoundDivisor
	if diff >= limit {
		return fmt.Errorf("%w: change %d exceeds limit %d", ErrInvalidGasLimit, diff, limit)
	}
	return nil
}

// verifyPostMerge checks that post-merge consensus fields are correct.
func verifyPostMerge(header *types.Header) error {
	// Difficulty must be 0 post-merge.
	if header.Difficulty != nil && header.Difficulty.Sign() != 0 {
		return fmt.Errorf("%w: got %v", ErrInvalidDifficulty, header.Difficulty)
	}

	// Nonce must be 0 post-merge.
	if header.Nonce != (types.BlockNonce{}) {
		return fmt.Errorf("%w: got %v", ErrInvalidNonce, header.Nonce)
	}

	// Uncle hash must be empty post-merge.
	if header.UncleHash != (types.Hash{}) && header.UncleHash != EmptyUncleHash {
		return fmt.Errorf("%w: got %v", ErrInvalidUncleHash, header.UncleHash)
	}

	return nil
}
