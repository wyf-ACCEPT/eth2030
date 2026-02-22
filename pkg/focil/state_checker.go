// state_checker.go implements state-aware validation for FOCIL inclusion lists.
//
// While structural validation (types.go, validation.go) checks IL format,
// state-aware validation verifies that each transaction in an IL is actually
// executable against the current EVM state: correct nonce, sufficient balance,
// and available block gas. This is critical because ILs that contain stale or
// invalid transactions waste block space and reduce censorship-resistance
// effectiveness.
//
// Per EIP-7805, transactions in an IL that are invalid at execution time are
// considered "satisfied" (the builder is not penalized for omitting them).
// The StateChecker identifies these invalid transactions so the compliance
// engine can correctly evaluate builder behavior.
package focil

import (
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// State checker errors.
var (
	ErrStateNilTx          = errors.New("state-checker: nil transaction")
	ErrStateNilReader      = errors.New("state-checker: nil state reader")
	ErrStateNonceMismatch  = errors.New("state-checker: nonce mismatch")
	ErrStateInsufficientBal = errors.New("state-checker: insufficient balance")
	ErrStateGasExhausted   = errors.New("state-checker: block gas exhausted")
	ErrStateSenderUnknown  = errors.New("state-checker: cannot recover sender")
)

// NonceFunc returns the current nonce for an address.
type NonceFunc func(addr types.Address) uint64

// BalanceFunc returns the current balance for an address.
type BalanceFunc func(addr types.Address) *big.Int

// StateReader provides account state lookups needed for IL validation.
// Implementations can wrap an in-memory StateDB, a trie-backed store,
// or an RPC client.
type StateReader interface {
	// GetNonce returns the current nonce for the given address.
	GetNonce(addr types.Address) uint64

	// GetBalance returns the current balance (in wei) for the given address.
	GetBalance(addr types.Address) *big.Int
}

// funcStateReader adapts NonceFunc/BalanceFunc into a StateReader.
type funcStateReader struct {
	nonceFn   NonceFunc
	balanceFn BalanceFunc
}

func (f *funcStateReader) GetNonce(addr types.Address) uint64 {
	if f.nonceFn == nil {
		return 0
	}
	return f.nonceFn(addr)
}

func (f *funcStateReader) GetBalance(addr types.Address) *big.Int {
	if f.balanceFn == nil {
		return new(big.Int)
	}
	return f.balanceFn(addr)
}

// TxValidity describes the state-validation result for a single transaction.
type TxValidity uint8

const (
	// TxValid means the transaction is executable against current state.
	TxValid TxValidity = iota

	// TxInvalidNonce means the transaction nonce doesn't match the sender's
	// current nonce in state.
	TxInvalidNonce

	// TxInsufficientBalance means the sender cannot cover gas*gasPrice + value.
	TxInsufficientBalance

	// TxGasExhausted means the block has insufficient remaining gas.
	TxGasExhausted

	// TxDecodeFailed means the transaction RLP could not be decoded.
	TxDecodeFailed

	// TxNoSender means the sender address could not be determined.
	TxNoSender
)

// String returns a human-readable name for TxValidity.
func (v TxValidity) String() string {
	switch v {
	case TxValid:
		return "valid"
	case TxInvalidNonce:
		return "invalid_nonce"
	case TxInsufficientBalance:
		return "insufficient_balance"
	case TxGasExhausted:
		return "gas_exhausted"
	case TxDecodeFailed:
		return "decode_failed"
	case TxNoSender:
		return "no_sender"
	default:
		return "unknown"
	}
}

// TxValidationResult holds the validation outcome for a single transaction.
type TxValidationResult struct {
	// TxHash is the transaction hash.
	TxHash types.Hash

	// Status is the validation outcome.
	Status TxValidity

	// Error is set when Status != TxValid.
	Error error
}

// BatchValidationResult holds the results of validating an entire IL.
type BatchValidationResult struct {
	// Valid contains transactions that passed all state checks.
	Valid []*types.Transaction

	// Invalid contains transactions that failed state checks.
	Invalid []TxValidationResult

	// TotalChecked is the number of transactions processed.
	TotalChecked int

	// ValidCount is the number of valid transactions.
	ValidCount int

	// InvalidCount is the number of invalid transactions.
	InvalidCount int
}

// StateCheckerConfig configures the StateChecker.
type StateCheckerConfig struct {
	// SkipNonceCheck disables nonce validation (useful for pending pool checks).
	SkipNonceCheck bool

	// SkipBalanceCheck disables balance validation.
	SkipBalanceCheck bool

	// AllowFutureNonce allows nonces ahead of current state (queued txs).
	AllowFutureNonce bool
}

// DefaultStateCheckerConfig returns production defaults.
func DefaultStateCheckerConfig() StateCheckerConfig {
	return StateCheckerConfig{
		SkipNonceCheck:   false,
		SkipBalanceCheck: false,
		AllowFutureNonce: false,
	}
}

// StateChecker validates inclusion list transactions against EVM state.
// All public methods are safe for concurrent use.
type StateChecker struct {
	mu     sync.RWMutex
	config StateCheckerConfig
}

// NewStateChecker creates a new StateChecker with the given config.
func NewStateChecker(config StateCheckerConfig) *StateChecker {
	return &StateChecker{config: config}
}

// NewStateCheckerFromFuncs creates a StateReader from function callbacks.
// This is a convenience constructor for tests and simple integrations.
func NewStateReaderFromFuncs(nonceFn NonceFunc, balanceFn BalanceFunc) StateReader {
	return &funcStateReader{
		nonceFn:   nonceFn,
		balanceFn: balanceFn,
	}
}

// CheckTransactionValidity verifies that a transaction's nonce matches the
// sender's current nonce in state. Returns nil if valid or nonce checking
// is disabled.
func (sc *StateChecker) CheckTransactionValidity(tx *types.Transaction, reader StateReader) error {
	if tx == nil {
		return ErrStateNilTx
	}
	if reader == nil {
		return ErrStateNilReader
	}

	sc.mu.RLock()
	cfg := sc.config
	sc.mu.RUnlock()

	if cfg.SkipNonceCheck {
		return nil
	}

	sender := tx.Sender()
	if sender == nil {
		return ErrStateSenderUnknown
	}

	stateNonce := reader.GetNonce(*sender)
	txNonce := tx.Nonce()

	if cfg.AllowFutureNonce {
		// Only reject if tx nonce is behind state.
		if txNonce < stateNonce {
			return fmt.Errorf("%w: tx nonce %d < state nonce %d for %s",
				ErrStateNonceMismatch, txNonce, stateNonce, sender.Hex())
		}
		return nil
	}

	if txNonce != stateNonce {
		return fmt.Errorf("%w: tx nonce %d, state nonce %d for %s",
			ErrStateNonceMismatch, txNonce, stateNonce, sender.Hex())
	}
	return nil
}

// CheckSenderBalance verifies that the sender has sufficient balance to cover
// gasLimit * gasPrice + value. Returns nil if valid or balance checking
// is disabled.
func (sc *StateChecker) CheckSenderBalance(tx *types.Transaction, reader StateReader) error {
	if tx == nil {
		return ErrStateNilTx
	}
	if reader == nil {
		return ErrStateNilReader
	}

	sc.mu.RLock()
	cfg := sc.config
	sc.mu.RUnlock()

	if cfg.SkipBalanceCheck {
		return nil
	}

	sender := tx.Sender()
	if sender == nil {
		return ErrStateSenderUnknown
	}

	balance := reader.GetBalance(*sender)
	if balance == nil {
		balance = new(big.Int)
	}

	// cost = gasLimit * gasPrice + value
	cost := txCost(tx)
	if balance.Cmp(cost) < 0 {
		return fmt.Errorf("%w: need %s, have %s for %s",
			ErrStateInsufficientBal, cost.String(), balance.String(), sender.Hex())
	}
	return nil
}

// txCost computes gasLimit * gasPrice + value for a transaction.
func txCost(tx *types.Transaction) *big.Int {
	gasPrice := tx.GasPrice()
	if gasPrice == nil {
		gasPrice = new(big.Int)
	}
	cost := new(big.Int).Mul(gasPrice, new(big.Int).SetUint64(tx.Gas()))
	value := tx.Value()
	if value != nil {
		cost.Add(cost, value)
	}
	return cost
}

// CheckGasAvailability verifies that enough gas remains in the block to
// execute the transaction. Returns nil if sufficient gas is available.
func (sc *StateChecker) CheckGasAvailability(tx *types.Transaction, blockGasLimit, usedGas uint64) error {
	if tx == nil {
		return ErrStateNilTx
	}
	remaining := uint64(0)
	if blockGasLimit > usedGas {
		remaining = blockGasLimit - usedGas
	}
	if tx.Gas() > remaining {
		return fmt.Errorf("%w: tx needs %d gas, block has %d remaining",
			ErrStateGasExhausted, tx.Gas(), remaining)
	}
	return nil
}

// BatchValidate validates all transactions in an inclusion list against state.
// It decodes each entry, checks nonce, balance, and returns categorized results.
func (sc *StateChecker) BatchValidate(il *InclusionList, reader StateReader) *BatchValidationResult {
	result := &BatchValidationResult{}

	if il == nil || len(il.Entries) == 0 {
		return result
	}
	if reader == nil {
		// Mark all as invalid if no reader.
		for _, entry := range il.Entries {
			result.TotalChecked++
			result.InvalidCount++
			tx, err := types.DecodeTxRLP(entry.Transaction)
			h := types.Hash{}
			if err == nil {
				h = tx.Hash()
			}
			result.Invalid = append(result.Invalid, TxValidationResult{
				TxHash: h,
				Status: TxNoSender,
				Error:  ErrStateNilReader,
			})
		}
		return result
	}

	for _, entry := range il.Entries {
		result.TotalChecked++

		tx, err := types.DecodeTxRLP(entry.Transaction)
		if err != nil {
			result.InvalidCount++
			result.Invalid = append(result.Invalid, TxValidationResult{
				Status: TxDecodeFailed,
				Error:  err,
			})
			continue
		}

		// Check nonce. If sender is unknown, we cannot check nonce or balance,
		// so treat the tx as valid (sender recovery requires ECDSA, which is
		// outside the scope of state checking).
		if err := sc.CheckTransactionValidity(tx, reader); err != nil {
			if errors.Is(err, ErrStateSenderUnknown) {
				// No sender cached: skip nonce/balance checks.
				result.ValidCount++
				result.Valid = append(result.Valid, tx)
				continue
			}
			result.InvalidCount++
			result.Invalid = append(result.Invalid, TxValidationResult{
				TxHash: tx.Hash(),
				Status: TxInvalidNonce,
				Error:  err,
			})
			continue
		}

		// Check balance.
		if err := sc.CheckSenderBalance(tx, reader); err != nil {
			if errors.Is(err, ErrStateSenderUnknown) {
				// No sender cached: skip balance check.
				result.ValidCount++
				result.Valid = append(result.Valid, tx)
				continue
			}
			result.InvalidCount++
			result.Invalid = append(result.Invalid, TxValidationResult{
				TxHash: tx.Hash(),
				Status: TxInsufficientBalance,
				Error:  err,
			})
			continue
		}

		result.ValidCount++
		result.Valid = append(result.Valid, tx)
	}

	return result
}

// DeduplicateIL removes duplicate transactions from an inclusion list,
// keeping the first occurrence of each unique transaction hash. Returns a
// new InclusionList with duplicates removed.
func DeduplicateIL(il *InclusionList) *InclusionList {
	if il == nil {
		return nil
	}

	deduped := &InclusionList{
		Slot:          il.Slot,
		ProposerIndex: il.ProposerIndex,
		CommitteeRoot: il.CommitteeRoot,
		Entries:       make([]InclusionListEntry, 0, len(il.Entries)),
	}

	seen := make(map[types.Hash]bool, len(il.Entries))
	index := uint64(0)

	for _, entry := range il.Entries {
		tx, err := types.DecodeTxRLP(entry.Transaction)
		if err != nil {
			// Keep invalid entries as-is (they will be caught by validation).
			deduped.Entries = append(deduped.Entries, InclusionListEntry{
				Transaction: entry.Transaction,
				Index:       index,
			})
			index++
			continue
		}

		h := tx.Hash()
		if seen[h] {
			continue // duplicate, skip
		}
		seen[h] = true

		deduped.Entries = append(deduped.Entries, InclusionListEntry{
			Transaction: entry.Transaction,
			Index:       index,
		})
		index++
	}

	return deduped
}

// DeduplicateHashes removes duplicate hashes from a slice, preserving order.
func DeduplicateHashes(hashes []types.Hash) []types.Hash {
	if len(hashes) == 0 {
		return nil
	}
	seen := make(map[types.Hash]bool, len(hashes))
	result := make([]types.Hash, 0, len(hashes))
	for _, h := range hashes {
		if !seen[h] {
			seen[h] = true
			result = append(result, h)
		}
	}
	return result
}
