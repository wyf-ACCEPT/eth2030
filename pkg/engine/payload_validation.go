// payload_validation.go implements comprehensive execution payload validation
// for the Engine API. It validates all fields of an ExecutionPayloadV3 including
// block hash, timestamp progression, EIP-1559 base fee, gas limits, extra data,
// transactions, blob gas (EIP-4844), withdrawals, and beacon block root.
//
// Constants like MaxExtraDataSize, MinGasLimit, GasLimitBoundDivisor, CalcBaseFee
// are defined in payload_processor.go and reused here.
package engine

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
	"github.com/eth2028/eth2028/rlp"
)

// Payload validation constants (supplement those in payload_processor.go).
const (
	// MaxTransactionsPerPayload is the soft limit for transactions per payload.
	MaxTransactionsPerPayload = 1 << 20 // ~1M

	// MaxTransactionSize is the maximum allowed size for a single encoded transaction.
	MaxTransactionSize = 1 << 24 // 16 MiB

	// MaxWithdrawalsPerPayloadV2 is the max withdrawals in a payload.
	MaxWithdrawalsPerPayloadV2 = 16
)

// Payload validation errors.
var (
	ErrPayloadNil              = errors.New("payload is nil")
	ErrTimestampNotIncreasing  = errors.New("timestamp must be greater than parent")
	ErrTimestampZero           = errors.New("timestamp must not be zero")
	ErrBaseFeeNil              = errors.New("base fee per gas must not be nil")
	ErrBaseFeeNegative         = errors.New("base fee per gas must not be negative")
	ErrBaseFeeZero             = errors.New("base fee per gas must not be zero")
	ErrBaseFeeInvalid          = errors.New("base fee does not match expected value")
	ErrGasLimitTooLow          = errors.New("gas limit below minimum")
	ErrGasLimitChangeTooLarge  = errors.New("gas limit change exceeds 1/1024 bound")
	ErrGasUsedExceedsLimit     = errors.New("gas used exceeds gas limit")
	ErrExtraDataTooLong        = errors.New("extra data exceeds 32 bytes")
	ErrTransactionDecode       = errors.New("failed to decode transaction")
	ErrTransactionEmpty        = errors.New("empty transaction bytes")
	ErrTransactionTooLarge     = errors.New("transaction exceeds maximum size")
	ErrTooManyTransactions     = errors.New("too many transactions in payload")
	ErrBlobGasUsedMissing      = errors.New("blob gas used must be present post-Cancun")
	ErrBlobGasUsedMismatch     = errors.New("blob gas used does not match transaction blobs")
	ErrBlobGasUsedNotAligned   = errors.New("blob gas used not aligned to blob gas per blob")
	ErrBlobGasUsedExceedsMax   = errors.New("blob gas used exceeds maximum")
	ErrWithdrawalsNil          = errors.New("withdrawals list must not be nil post-Shanghai")
	ErrWithdrawalsTooMany      = errors.New("too many withdrawals")
	ErrWithdrawalInvalid       = errors.New("invalid withdrawal entry")
	ErrBeaconRootMissing       = errors.New("parent beacon block root must be present post-Cancun")
	ErrBlockHashMismatch       = errors.New("block hash does not match computed hash")
)

// PayloadValidator validates execution payloads received via the Engine API.
// It checks structural correctness, consensus rules, and field consistency.
type PayloadValidator struct {
	// maxBlobsPerBlock is the configured max blobs per block.
	maxBlobsPerBlock int

	// blobGasPerBlob is the gas consumed per blob.
	blobGasPerBlob uint64
}

// NewPayloadValidator creates a new PayloadValidator with default EIP-4844 params.
func NewPayloadValidator() *PayloadValidator {
	return &PayloadValidator{
		maxBlobsPerBlock: int(types.MaxBlobGasPerBlock / types.BlobTxBlobGasPerBlob),
		blobGasPerBlob:   types.BlobTxBlobGasPerBlob,
	}
}

// ValidatePayloadFull runs all validation checks on the payload, returning all errors found.
// This performs structural checks only; it does not execute transactions.
func (v *PayloadValidator) ValidatePayloadFull(payload *ExecutionPayloadV3) []error {
	if payload == nil {
		return []error{ErrPayloadNil}
	}

	var errs []error

	// Validate extra data length.
	if err := ValidateExtraData(payload.ExtraData); err != nil {
		errs = append(errs, err)
	}

	// Validate gas used does not exceed gas limit.
	if payload.GasUsed > payload.GasLimit {
		errs = append(errs, fmt.Errorf("%w: used %d, limit %d",
			ErrGasUsedExceedsLimit, payload.GasUsed, payload.GasLimit))
	}

	// Validate base fee.
	if payload.BaseFeePerGas == nil {
		errs = append(errs, ErrBaseFeeNil)
	} else if payload.BaseFeePerGas.Sign() < 0 {
		errs = append(errs, ErrBaseFeeNegative)
	} else if payload.BaseFeePerGas.Sign() == 0 {
		errs = append(errs, ErrBaseFeeZero)
	}

	// Validate timestamp is nonzero.
	if payload.Timestamp == 0 {
		errs = append(errs, ErrTimestampZero)
	}

	// Decode and validate transactions.
	txs, err := ValidateTransactions(payload.Transactions)
	if err != nil {
		errs = append(errs, err)
	}

	// Validate blob gas used against transactions.
	blobGasUsed := payload.BlobGasUsed
	if err := ValidateBlobGasUsed(blobGasUsed, txs); err != nil {
		errs = append(errs, err)
	}

	// Validate withdrawals.
	if err := ValidateWithdrawals(payload.Withdrawals); err != nil {
		errs = append(errs, err)
	}

	return errs
}

// ValidateBlockHashComputed recomputes the block hash from header fields and compares
// it against the declared BlockHash in the payload. It constructs an RLP-encoded
// header from the payload fields and hashes it with Keccak-256.
func ValidateBlockHashComputed(payload *ExecutionPayloadV3) error {
	if payload == nil {
		return ErrPayloadNil
	}

	// Reconstruct a header from the payload fields.
	header := headerFromPayloadV3(payload)

	// Encode the header via RLP and hash with Keccak-256.
	encoded, err := rlp.EncodeToBytes(headerToRLPFieldsV3(header))
	if err != nil {
		return fmt.Errorf("encode header for hash: %w", err)
	}

	computed := types.BytesToHash(crypto.Keccak256(encoded))
	if computed != payload.BlockHash {
		return fmt.Errorf("%w: computed %s, declared %s",
			ErrBlockHashMismatch, computed.Hex(), payload.BlockHash.Hex())
	}
	return nil
}

// headerFromPayloadV3 builds a types.Header from an ExecutionPayloadV3.
func headerFromPayloadV3(payload *ExecutionPayloadV3) *types.Header {
	blobGasUsed := payload.BlobGasUsed
	excessBlobGas := payload.ExcessBlobGas
	h := &types.Header{
		ParentHash:    payload.ParentHash,
		UncleHash:     types.EmptyUncleHash,
		Coinbase:      payload.FeeRecipient,
		Root:          payload.StateRoot,
		ReceiptHash:   payload.ReceiptsRoot,
		Bloom:         payload.LogsBloom,
		Difficulty:    new(big.Int),
		Number:        new(big.Int).SetUint64(payload.BlockNumber),
		GasLimit:      payload.GasLimit,
		GasUsed:       payload.GasUsed,
		Time:          payload.Timestamp,
		Extra:         payload.ExtraData,
		BaseFee:       payload.BaseFeePerGas,
		BlobGasUsed:   &blobGasUsed,
		ExcessBlobGas: &excessBlobGas,
	}
	return h
}

// headerRLPFieldsV3 is a serializable struct for header RLP encoding.
// Field order must match the Ethereum consensus spec.
type headerRLPFieldsV3 struct {
	ParentHash    types.Hash
	UncleHash     types.Hash
	Coinbase      types.Address
	Root          types.Hash
	TxHash        types.Hash
	ReceiptHash   types.Hash
	Bloom         types.Bloom
	Difficulty    *big.Int
	Number        *big.Int
	GasLimit      uint64
	GasUsed       uint64
	Time          uint64
	Extra         []byte
	MixDigest     types.Hash
	Nonce         types.BlockNonce
	BaseFee       *big.Int
	BlobGasUsed   uint64
	ExcessBlobGas uint64
}

// headerToRLPFieldsV3 converts a Header to the RLP-encodable struct.
func headerToRLPFieldsV3(h *types.Header) headerRLPFieldsV3 {
	f := headerRLPFieldsV3{
		ParentHash:  h.ParentHash,
		UncleHash:   h.UncleHash,
		Coinbase:    h.Coinbase,
		Root:        h.Root,
		TxHash:      h.TxHash,
		ReceiptHash: h.ReceiptHash,
		Bloom:       h.Bloom,
		Difficulty:  h.Difficulty,
		Number:      h.Number,
		GasLimit:    h.GasLimit,
		GasUsed:     h.GasUsed,
		Time:        h.Time,
		Extra:       h.Extra,
		MixDigest:   h.MixDigest,
		Nonce:       h.Nonce,
		BaseFee:     h.BaseFee,
	}
	if h.BlobGasUsed != nil {
		f.BlobGasUsed = *h.BlobGasUsed
	}
	if h.ExcessBlobGas != nil {
		f.ExcessBlobGas = *h.ExcessBlobGas
	}
	return f
}

// ValidateTimestamp checks that the payload timestamp is strictly greater than
// the parent timestamp. The payload timestamp must be nonzero.
func ValidateTimestamp(parentTimestamp, payloadTimestamp uint64) error {
	if payloadTimestamp == 0 {
		return ErrTimestampZero
	}
	if payloadTimestamp <= parentTimestamp {
		return fmt.Errorf("%w: parent=%d, payload=%d",
			ErrTimestampNotIncreasing, parentTimestamp, payloadTimestamp)
	}
	return nil
}

// ValidateBaseFee validates the EIP-1559 base fee calculation using big.Int.
// Given parent base fee, parent gas used, and parent gas target (gasLimit / elasticity),
// it computes the expected base fee and compares against the current base fee.
func ValidateBaseFee(parent, current *big.Int, parentGasUsed, parentGasTarget uint64) error {
	if parent == nil || current == nil {
		return ErrBaseFeeNil
	}
	if parent.Sign() <= 0 {
		return fmt.Errorf("%w: parent base fee is non-positive", ErrBaseFeeInvalid)
	}
	if current.Sign() <= 0 {
		return ErrBaseFeeZero
	}

	expected := CalcBaseFeeBig(parent, parentGasUsed, parentGasTarget)
	if expected.Cmp(current) != 0 {
		return fmt.Errorf("%w: expected %s, got %s",
			ErrBaseFeeInvalid, expected.String(), current.String())
	}
	return nil
}

// CalcBaseFeeBig computes the EIP-1559 base fee for the next block using big.Int
// arithmetic. This complements CalcBaseFee (in payload_processor.go) which uses uint64.
// If parentGasUsed == parentGasTarget, base fee stays the same.
// If parentGasUsed > parentGasTarget, base fee increases.
// If parentGasUsed < parentGasTarget, base fee decreases.
func CalcBaseFeeBig(parentBaseFee *big.Int, parentGasUsed, parentGasTarget uint64) *big.Int {
	if parentGasTarget == 0 {
		return new(big.Int).Set(parentBaseFee)
	}

	parentGasUsedBig := new(big.Int).SetUint64(parentGasUsed)
	parentGasTargetBig := new(big.Int).SetUint64(parentGasTarget)

	if parentGasUsed == parentGasTarget {
		return new(big.Int).Set(parentBaseFee)
	}

	if parentGasUsed > parentGasTarget {
		// Base fee increases.
		// delta = max(parentBaseFee * (parentGasUsed - parentGasTarget) / parentGasTarget / denominator, 1)
		gasUsedDelta := new(big.Int).Sub(parentGasUsedBig, parentGasTargetBig)
		x := new(big.Int).Mul(parentBaseFee, gasUsedDelta)
		x.Div(x, parentGasTargetBig)
		x.Div(x, new(big.Int).SetUint64(BaseFeeChangeDenominator))

		// Ensure minimum increase of 1.
		if x.Sign() == 0 {
			x.SetUint64(1)
		}
		return new(big.Int).Add(parentBaseFee, x)
	}

	// Base fee decreases.
	// delta = parentBaseFee * (parentGasTarget - parentGasUsed) / parentGasTarget / denominator
	gasUsedDelta := new(big.Int).Sub(parentGasTargetBig, parentGasUsedBig)
	x := new(big.Int).Mul(parentBaseFee, gasUsedDelta)
	x.Div(x, parentGasTargetBig)
	x.Div(x, new(big.Int).SetUint64(BaseFeeChangeDenominator))

	result := new(big.Int).Sub(parentBaseFee, x)
	// Base fee cannot go below 1.
	if result.Sign() <= 0 {
		result.SetUint64(1)
	}
	return result
}

// ValidateGasLimit checks that the payload gas limit is within the allowed
// range of the parent gas limit (plus or minus 1/1024).
func ValidateGasLimit(parentGasLimit, payloadGasLimit uint64) error {
	if payloadGasLimit < MinGasLimit {
		return fmt.Errorf("%w: %d < minimum %d",
			ErrGasLimitTooLow, payloadGasLimit, MinGasLimit)
	}

	// Gas limit can change by at most parentGasLimit / GasLimitBoundDivisor.
	diff := parentGasLimit / GasLimitBoundDivisor
	if diff == 0 {
		diff = 1
	}

	if payloadGasLimit > parentGasLimit+diff {
		return fmt.Errorf("%w: %d > parent %d + %d",
			ErrGasLimitChangeTooLarge, payloadGasLimit, parentGasLimit, diff)
	}
	if payloadGasLimit+diff < parentGasLimit {
		return fmt.Errorf("%w: %d < parent %d - %d",
			ErrGasLimitChangeTooLarge, payloadGasLimit, parentGasLimit, diff)
	}

	return nil
}

// ValidateExtraData checks that the extra data does not exceed 32 bytes.
func ValidateExtraData(extra []byte) error {
	if len(extra) > MaxExtraDataSize {
		return fmt.Errorf("%w: length %d", ErrExtraDataTooLong, len(extra))
	}
	return nil
}

// ValidateTransactions decodes each raw transaction and returns the decoded
// transactions. Returns an error if any transaction fails to decode, is
// empty, or exceeds the maximum size.
func ValidateTransactions(txBytes [][]byte) ([]*types.Transaction, error) {
	if len(txBytes) > MaxTransactionsPerPayload {
		return nil, fmt.Errorf("%w: %d transactions", ErrTooManyTransactions, len(txBytes))
	}

	txs := make([]*types.Transaction, 0, len(txBytes))
	for i, raw := range txBytes {
		if len(raw) == 0 {
			return nil, fmt.Errorf("%w at index %d", ErrTransactionEmpty, i)
		}
		if len(raw) > MaxTransactionSize {
			return nil, fmt.Errorf("%w at index %d: size %d",
				ErrTransactionTooLarge, i, len(raw))
		}

		tx, err := types.DecodeTxRLP(raw)
		if err != nil {
			return nil, fmt.Errorf("%w at index %d: %v",
				ErrTransactionDecode, i, err)
		}
		txs = append(txs, tx)
	}
	return txs, nil
}

// ValidateBlobGasUsed checks that the blob gas used in the payload matches
// the total blob gas from all blob transactions. Per EIP-4844, blob gas used
// must be aligned to BlobTxBlobGasPerBlob and must not exceed the maximum.
func ValidateBlobGasUsed(blobGasUsed uint64, txs []*types.Transaction) error {
	// Count total blob gas from transactions.
	var totalBlobGas uint64
	for _, tx := range txs {
		if tx != nil && tx.Type() == types.BlobTxType {
			totalBlobGas += tx.BlobGas()
		}
	}

	// Blob gas used must be aligned to BlobTxBlobGasPerBlob.
	if blobGasUsed%types.BlobTxBlobGasPerBlob != 0 {
		return fmt.Errorf("%w: %d not divisible by %d",
			ErrBlobGasUsedNotAligned, blobGasUsed, types.BlobTxBlobGasPerBlob)
	}

	// Blob gas used must not exceed maximum.
	if blobGasUsed > types.MaxBlobGasPerBlock {
		return fmt.Errorf("%w: %d > max %d",
			ErrBlobGasUsedExceedsMax, blobGasUsed, types.MaxBlobGasPerBlock)
	}

	// Blob gas used must match the sum from transactions.
	if blobGasUsed != totalBlobGas {
		return fmt.Errorf("%w: declared %d, computed %d from transactions",
			ErrBlobGasUsedMismatch, blobGasUsed, totalBlobGas)
	}

	return nil
}

// ValidateWithdrawals checks the withdrawals list for structural validity.
// Post-Shanghai, withdrawals must not be nil and must not exceed the maximum count.
func ValidateWithdrawals(withdrawals []*Withdrawal) error {
	if withdrawals == nil {
		return ErrWithdrawalsNil
	}
	if len(withdrawals) > MaxWithdrawalsPerPayloadV2 {
		return fmt.Errorf("%w: %d > max %d",
			ErrWithdrawalsTooMany, len(withdrawals), MaxWithdrawalsPerPayloadV2)
	}

	seen := make(map[uint64]bool, len(withdrawals))
	for i, w := range withdrawals {
		if w == nil {
			return fmt.Errorf("%w: nil withdrawal at index %d", ErrWithdrawalInvalid, i)
		}
		if w.Address == (types.Address{}) {
			return fmt.Errorf("%w: zero address at index %d", ErrWithdrawalInvalid, i)
		}
		if seen[w.Index] {
			return fmt.Errorf("%w: duplicate index %d", ErrWithdrawalInvalid, w.Index)
		}
		seen[w.Index] = true
	}
	return nil
}

// ValidateParentBeaconBlockRoot checks that the parent beacon block root is
// present (non-zero). Required post-Cancun per EIP-4788.
func ValidateParentBeaconBlockRoot(root *types.Hash) error {
	if root == nil {
		return ErrBeaconRootMissing
	}
	if *root == (types.Hash{}) {
		return fmt.Errorf("%w: root is zero hash", ErrBeaconRootMissing)
	}
	return nil
}
