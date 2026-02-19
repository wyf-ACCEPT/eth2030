package txpool

import (
	"errors"
	"math/big"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// Validator-specific error codes.
var (
	ErrTxGasTooLow   = errors.New("tx gas price below minimum")
	ErrTxGasTooHigh  = errors.New("tx gas limit exceeds maximum")
	ErrTxDataTooLarge = errors.New("tx data exceeds maximum size")
	ErrTxValueTooHigh = errors.New("tx value exceeds maximum")
	ErrTxNoSignature  = errors.New("tx has no signature")
	ErrTxBadChainID   = errors.New("tx chain ID mismatch")
)

// oneGwei is 1e9 wei, used as the default minimum gas price.
var oneGwei = big.NewInt(1_000_000_000)

// maxEthValue is 100 million ETH in wei, used as the default max tx value.
var maxEthValue = new(big.Int).Mul(
	new(big.Int).Mul(big.NewInt(100_000_000), big.NewInt(1e18)),
	big.NewInt(1),
)

// TxValidationConfig holds configuration for the TxValidator.
type TxValidationConfig struct {
	MinGasPrice *big.Int // minimum gas price (default 1 Gwei)
	MaxGasLimit uint64   // maximum gas limit (default 30_000_000)
	MaxDataSize int      // maximum transaction data size in bytes (default 128*1024)
	MaxValueWei *big.Int // maximum transaction value in wei (default 100M ETH)
	ChainID     uint64   // expected chain ID
}

// DefaultTxValidationConfig returns a TxValidationConfig with sensible defaults.
func DefaultTxValidationConfig() TxValidationConfig {
	return TxValidationConfig{
		MinGasPrice: new(big.Int).Set(oneGwei),
		MaxGasLimit: 30_000_000,
		MaxDataSize: 128 * 1024,
		MaxValueWei: new(big.Int).Set(maxEthValue),
		ChainID:     1,
	}
}

// TxValidationResult holds the result of a transaction validation.
type TxValidationResult struct {
	Valid  bool
	Error  error
	Checks []string // list of passed check names
}

// TxValidator performs comprehensive transaction validation.
// It is safe for concurrent use.
type TxValidator struct {
	mu     sync.RWMutex
	config TxValidationConfig
}

// NewTxValidator creates a new TxValidator with the given config.
// Nil fields in config are populated with defaults.
func NewTxValidator(config TxValidationConfig) *TxValidator {
	if config.MinGasPrice == nil {
		config.MinGasPrice = new(big.Int).Set(oneGwei)
	}
	if config.MaxGasLimit == 0 {
		config.MaxGasLimit = 30_000_000
	}
	if config.MaxDataSize == 0 {
		config.MaxDataSize = 128 * 1024
	}
	if config.MaxValueWei == nil {
		config.MaxValueWei = new(big.Int).Set(maxEthValue)
	}
	return &TxValidator{config: config}
}

// ValidateTx runs all validation checks on a transaction and returns
// the combined result including which checks passed.
func (v *TxValidator) ValidateTx(tx *types.Transaction) *TxValidationResult {
	v.mu.RLock()
	defer v.mu.RUnlock()

	result := &TxValidationResult{Valid: true}

	if err := v.validateBasic(tx); err != nil {
		result.Valid = false
		result.Error = err
		return result
	}
	result.Checks = append(result.Checks, "basic")

	if err := v.validateGas(tx); err != nil {
		result.Valid = false
		result.Error = err
		return result
	}
	result.Checks = append(result.Checks, "gas")

	if err := v.validateSize(tx); err != nil {
		result.Valid = false
		result.Error = err
		return result
	}
	result.Checks = append(result.Checks, "size")

	if v.config.ChainID != 0 {
		if err := v.validateChainID(tx, v.config.ChainID); err != nil {
			result.Valid = false
			result.Error = err
			return result
		}
		result.Checks = append(result.Checks, "chainid")
	}

	if err := v.validateSignature(tx); err != nil {
		result.Valid = false
		result.Error = err
		return result
	}
	result.Checks = append(result.Checks, "signature")

	return result
}

// ValidateBasic checks nonce (non-zero gas), value bounds.
func (v *TxValidator) ValidateBasic(tx *types.Transaction) error {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.validateBasic(tx)
}

// validateBasic is the non-locking implementation of ValidateBasic.
func (v *TxValidator) validateBasic(tx *types.Transaction) error {
	// Gas must be non-zero.
	if tx.Gas() == 0 {
		return ErrTxGasTooLow
	}
	// Value must not exceed maximum.
	if val := tx.Value(); val != nil && val.Sign() > 0 {
		if val.Cmp(v.config.MaxValueWei) > 0 {
			return ErrTxValueTooHigh
		}
	}
	return nil
}

// ValidateGas checks gas price and gas limit bounds.
func (v *TxValidator) ValidateGas(tx *types.Transaction) error {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.validateGas(tx)
}

// validateGas is the non-locking implementation of ValidateGas.
func (v *TxValidator) validateGas(tx *types.Transaction) error {
	// Gas limit must not exceed maximum.
	if tx.Gas() > v.config.MaxGasLimit {
		return ErrTxGasTooHigh
	}
	// Gas price must meet minimum. Check GasFeeCap for EIP-1559 txs,
	// GasPrice for legacy txs.
	effectivePrice := tx.GasFeeCap()
	if effectivePrice == nil {
		effectivePrice = tx.GasPrice()
	}
	if effectivePrice == nil || effectivePrice.Cmp(v.config.MinGasPrice) < 0 {
		return ErrTxGasTooLow
	}
	return nil
}

// ValidateSize checks that the transaction data does not exceed the max size.
func (v *TxValidator) ValidateSize(tx *types.Transaction) error {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.validateSize(tx)
}

// validateSize is the non-locking implementation of ValidateSize.
func (v *TxValidator) validateSize(tx *types.Transaction) error {
	if len(tx.Data()) > v.config.MaxDataSize {
		return ErrTxDataTooLarge
	}
	return nil
}

// ValidateChainID checks that the transaction's chain ID matches the expected one.
func (v *TxValidator) ValidateChainID(tx *types.Transaction, chainID uint64) error {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.validateChainID(tx, chainID)
}

// validateChainID is the non-locking implementation of ValidateChainID.
func (v *TxValidator) validateChainID(tx *types.Transaction, chainID uint64) error {
	txChainID := tx.ChainId()
	if txChainID == nil {
		return ErrTxBadChainID
	}
	// Chain ID of 0 on the tx means pre-EIP-155 legacy tx; skip check.
	if txChainID.Sign() == 0 {
		return nil
	}
	if txChainID.Uint64() != chainID {
		return ErrTxBadChainID
	}
	return nil
}

// ValidateSignature checks that the transaction has valid-looking V, R, S values.
func (v *TxValidator) ValidateSignature(tx *types.Transaction) error {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.validateSignature(tx)
}

// validateSignature is the non-locking implementation of ValidateSignature.
func (v *TxValidator) validateSignature(tx *types.Transaction) error {
	_, r, s := tx.RawSignatureValues()
	if r == nil || s == nil {
		return ErrTxNoSignature
	}
	if r.Sign() == 0 && s.Sign() == 0 {
		return ErrTxNoSignature
	}
	return nil
}

// ValidateBatch validates multiple transactions and returns a result for each.
func (v *TxValidator) ValidateBatch(txs []*types.Transaction) []*TxValidationResult {
	results := make([]*TxValidationResult, len(txs))
	for i, tx := range txs {
		results[i] = v.ValidateTx(tx)
	}
	return results
}
