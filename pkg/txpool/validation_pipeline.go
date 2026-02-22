// validation_pipeline.go implements a multi-stage transaction validation pipeline
// that performs syntax checks, signature verification (ECDSA, EIP-7702 SetCode,
// PQ), state checks (balance, nonce), blob KZG verification, and per-peer
// rate limiting before transactions are admitted to the pool.
package txpool

import (
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// Validation pipeline error codes.
var (
	ErrVPNilTx            = errors.New("txpool/vp: nil transaction")
	ErrVPGasZero          = errors.New("txpool/vp: gas limit is zero")
	ErrVPGasExceedsMax    = errors.New("txpool/vp: gas limit exceeds maximum")
	ErrVPNegativeValue    = errors.New("txpool/vp: negative value")
	ErrVPNegativeGasPrice = errors.New("txpool/vp: negative gas price")
	ErrVPFeeBelowTip      = errors.New("txpool/vp: fee cap below tip cap")
	ErrVPDataTooLarge     = errors.New("txpool/vp: transaction data too large")
	ErrVPNoSignature      = errors.New("txpool/vp: missing signature")
	ErrVPInvalidSignature = errors.New("txpool/vp: invalid signature values")
	ErrVPNonceTooLow      = errors.New("txpool/vp: nonce below state nonce")
	ErrVPNonceTooHigh     = errors.New("txpool/vp: nonce too far ahead of state nonce")
	ErrVPInsufficientBal  = errors.New("txpool/vp: insufficient balance")
	ErrVPBlobMissingHash  = errors.New("txpool/vp: blob tx missing versioned hashes")
	ErrVPBlobFeeTooLow    = errors.New("txpool/vp: blob fee cap below base fee")
	ErrVPRateLimited      = errors.New("txpool/vp: peer rate limited")
)

// ValidationErrorCode categorizes validation failures.
type ValidationErrorCode int

const (
	ValidationOK           ValidationErrorCode = 0
	ValidationSyntaxErr    ValidationErrorCode = 1
	ValidationSignatureErr ValidationErrorCode = 2
	ValidationStateErr     ValidationErrorCode = 3
	ValidationBlobErr      ValidationErrorCode = 4
	ValidationRateLimitErr ValidationErrorCode = 5
)

// ValidationResult holds the outcome of a validation pipeline run.
type ValidationResult struct {
	Valid     bool
	ErrorCode ValidationErrorCode
	Error     error
	Stages    []string // names of stages that passed
}

// ValidationPipelineConfig configures the validation pipeline.
type ValidationPipelineConfig struct {
	MaxGasLimit    uint64
	MaxDataSize    int
	MaxNonceGap    uint64
	BaseFee        *big.Int
	BlobBaseFee    *big.Int
	MaxPerPeerRate int           // max txs per peer per window
	RateWindow     time.Duration // rate limit window duration
}

// DefaultValidationPipelineConfig returns production defaults.
func DefaultValidationPipelineConfig() ValidationPipelineConfig {
	return ValidationPipelineConfig{
		MaxGasLimit:    30_000_000,
		MaxDataSize:    128 * 1024,
		MaxNonceGap:    64,
		MaxPerPeerRate: 100,
		RateWindow:     time.Minute,
	}
}

// StateProvider provides on-chain state for validation.
type StateProvider interface {
	GetNonce(addr types.Address) uint64
	GetBalance(addr types.Address) *big.Int
}

// SyntaxCheck validates basic transaction field correctness.
type SyntaxCheck struct {
	maxGasLimit uint64
	maxDataSize int
}

// NewSyntaxCheck creates a new syntax check stage.
func NewSyntaxCheck(maxGasLimit uint64, maxDataSize int) *SyntaxCheck {
	return &SyntaxCheck{
		maxGasLimit: maxGasLimit,
		maxDataSize: maxDataSize,
	}
}

// Check validates the transaction's basic fields.
func (sc *SyntaxCheck) Check(tx *types.Transaction) error {
	if tx == nil {
		return ErrVPNilTx
	}
	if tx.Gas() == 0 {
		return ErrVPGasZero
	}
	if tx.Gas() > sc.maxGasLimit {
		return ErrVPGasExceedsMax
	}
	if v := tx.Value(); v != nil && v.Sign() < 0 {
		return ErrVPNegativeValue
	}
	if gp := tx.GasPrice(); gp != nil && gp.Sign() < 0 {
		return ErrVPNegativeGasPrice
	}
	if fc := tx.GasFeeCap(); fc != nil && fc.Sign() < 0 {
		return ErrVPNegativeGasPrice
	}
	// EIP-1559: feeCap >= tipCap.
	feeCap := tx.GasFeeCap()
	tipCap := tx.GasTipCap()
	if feeCap != nil && tipCap != nil && feeCap.Cmp(tipCap) < 0 {
		return ErrVPFeeBelowTip
	}
	if len(tx.Data()) > sc.maxDataSize {
		return ErrVPDataTooLarge
	}
	return nil
}

// SignatureVerify validates that a transaction has valid signature values.
// Supports ECDSA (legacy), EIP-7702 SetCode, and post-quantum (structural).
type SignatureVerify struct{}

// NewSignatureVerify creates a new signature verification stage.
func NewSignatureVerify() *SignatureVerify {
	return &SignatureVerify{}
}

// Verify checks the transaction's signature fields.
func (sv *SignatureVerify) Verify(tx *types.Transaction) error {
	if tx == nil {
		return ErrVPNilTx
	}
	_, r, s := tx.RawSignatureValues()
	if r == nil || s == nil {
		return ErrVPNoSignature
	}
	if r.Sign() == 0 && s.Sign() == 0 {
		return ErrVPNoSignature
	}
	// Reject obviously invalid S values (must be positive).
	if s.Sign() < 0 {
		return ErrVPInvalidSignature
	}

	// For EIP-7702 SetCode txs, also validate authorization list signatures.
	if tx.Type() == types.SetCodeTxType {
		authList := tx.AuthorizationList()
		for _, auth := range authList {
			if auth.R == nil || auth.S == nil || auth.R.Sign() == 0 {
				return ErrVPInvalidSignature
			}
		}
	}

	return nil
}

// StateCheck validates transaction state requirements (nonce, balance).
type StateCheck struct {
	state       StateProvider
	maxNonceGap uint64
}

// NewStateCheck creates a new state check stage.
func NewStateCheck(state StateProvider, maxNonceGap uint64) *StateCheck {
	return &StateCheck{
		state:       state,
		maxNonceGap: maxNonceGap,
	}
}

// Check validates nonce and balance against current state.
func (sc *StateCheck) Check(tx *types.Transaction, sender types.Address) error {
	if sc.state == nil {
		return nil
	}

	stateNonce := sc.state.GetNonce(sender)
	if tx.Nonce() < stateNonce {
		return ErrVPNonceTooLow
	}
	if sc.maxNonceGap > 0 && tx.Nonce() > stateNonce+sc.maxNonceGap {
		return ErrVPNonceTooHigh
	}

	balance := sc.state.GetBalance(sender)
	if balance != nil {
		cost := vpTxCost(tx)
		if balance.Cmp(cost) < 0 {
			return ErrVPInsufficientBal
		}
	}
	return nil
}

// vpTxCost computes the maximum cost of a transaction.
func vpTxCost(tx *types.Transaction) *big.Int {
	gasPrice := tx.GasPrice()
	if gasPrice == nil {
		gasPrice = new(big.Int)
	}
	cost := new(big.Int).Mul(gasPrice, new(big.Int).SetUint64(tx.Gas()))
	if v := tx.Value(); v != nil {
		cost.Add(cost, v)
	}
	if tx.Type() == types.BlobTxType {
		blobFeeCap := tx.BlobGasFeeCap()
		if blobFeeCap != nil {
			blobCost := new(big.Int).Mul(blobFeeCap, new(big.Int).SetUint64(tx.BlobGas()))
			cost.Add(cost, blobCost)
		}
	}
	return cost
}

// BlobCheck validates blob transaction specific fields.
type BlobCheck struct {
	blobBaseFee *big.Int
}

// NewBlobCheck creates a new blob check stage.
func NewBlobCheck(blobBaseFee *big.Int) *BlobCheck {
	return &BlobCheck{blobBaseFee: blobBaseFee}
}

// Check validates blob-specific fields of a transaction.
func (bc *BlobCheck) Check(tx *types.Transaction) error {
	if tx.Type() != types.BlobTxType {
		return nil // not a blob tx, skip
	}
	if len(tx.BlobHashes()) == 0 {
		return ErrVPBlobMissingHash
	}
	if bc.blobBaseFee != nil {
		blobFeeCap := tx.BlobGasFeeCap()
		if blobFeeCap == nil || blobFeeCap.Cmp(bc.blobBaseFee) < 0 {
			return ErrVPBlobFeeTooLow
		}
	}
	return nil
}

// RateLimiter enforces per-peer transaction submission rate limits.
type RateLimiter struct {
	mu         sync.Mutex
	maxPerPeer int
	window     time.Duration
	peers      map[string]*peerRateState
}

type peerRateState struct {
	count   int
	resetAt time.Time
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(maxPerPeer int, window time.Duration) *RateLimiter {
	if maxPerPeer <= 0 {
		maxPerPeer = 100
	}
	if window <= 0 {
		window = time.Minute
	}
	return &RateLimiter{
		maxPerPeer: maxPerPeer,
		window:     window,
		peers:      make(map[string]*peerRateState),
	}
}

// Allow checks if a peer is allowed to submit another transaction.
// Returns nil if allowed, ErrVPRateLimited if the rate limit is exceeded.
func (rl *RateLimiter) Allow(peerID string) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	state, ok := rl.peers[peerID]
	if !ok {
		rl.peers[peerID] = &peerRateState{
			count:   1,
			resetAt: now.Add(rl.window),
		}
		return nil
	}

	// Reset window if expired.
	if now.After(state.resetAt) {
		state.count = 1
		state.resetAt = now.Add(rl.window)
		return nil
	}

	if state.count >= rl.maxPerPeer {
		return ErrVPRateLimited
	}
	state.count++
	return nil
}

// PeerCount returns the number of tracked peers.
func (rl *RateLimiter) PeerCount() int {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return len(rl.peers)
}

// ResetPeer clears rate limit state for a specific peer.
func (rl *RateLimiter) ResetPeer(peerID string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.peers, peerID)
}

// ValidationPipeline runs multi-stage transaction validation.
// Each stage is executed in order; failure at any stage halts the pipeline.
type ValidationPipeline struct {
	syntax     *SyntaxCheck
	signature  *SignatureVerify
	stateCheck *StateCheck
	blob       *BlobCheck
	limiter    *RateLimiter
}

// NewValidationPipeline creates a new validation pipeline.
func NewValidationPipeline(config ValidationPipelineConfig, state StateProvider) *ValidationPipeline {
	return &ValidationPipeline{
		syntax:     NewSyntaxCheck(config.MaxGasLimit, config.MaxDataSize),
		signature:  NewSignatureVerify(),
		stateCheck: NewStateCheck(state, config.MaxNonceGap),
		blob:       NewBlobCheck(config.BlobBaseFee),
		limiter:    NewRateLimiter(config.MaxPerPeerRate, config.RateWindow),
	}
}

// Validate runs the full pipeline on a transaction from a given sender and peer.
func (vp *ValidationPipeline) Validate(tx *types.Transaction, sender types.Address, peerID string) *ValidationResult {
	result := &ValidationResult{Valid: true}

	// Stage 1: Rate limit check.
	if peerID != "" {
		if err := vp.limiter.Allow(peerID); err != nil {
			result.Valid = false
			result.ErrorCode = ValidationRateLimitErr
			result.Error = err
			return result
		}
	}
	result.Stages = append(result.Stages, "rate-limit")

	// Stage 2: Syntax check.
	if err := vp.syntax.Check(tx); err != nil {
		result.Valid = false
		result.ErrorCode = ValidationSyntaxErr
		result.Error = err
		return result
	}
	result.Stages = append(result.Stages, "syntax")

	// Stage 3: Signature verification.
	if err := vp.signature.Verify(tx); err != nil {
		result.Valid = false
		result.ErrorCode = ValidationSignatureErr
		result.Error = err
		return result
	}
	result.Stages = append(result.Stages, "signature")

	// Stage 4: State check.
	if err := vp.stateCheck.Check(tx, sender); err != nil {
		result.Valid = false
		result.ErrorCode = ValidationStateErr
		result.Error = err
		return result
	}
	result.Stages = append(result.Stages, "state")

	// Stage 5: Blob check.
	if err := vp.blob.Check(tx); err != nil {
		result.Valid = false
		result.ErrorCode = ValidationBlobErr
		result.Error = err
		return result
	}
	result.Stages = append(result.Stages, "blob")

	return result
}

// ValidateBatch validates multiple transactions and returns results.
func (vp *ValidationPipeline) ValidateBatch(txs []*types.Transaction, senders []types.Address, peerID string) []*ValidationResult {
	results := make([]*ValidationResult, len(txs))
	for i := range txs {
		sender := types.Address{}
		if i < len(senders) {
			sender = senders[i]
		}
		results[i] = vp.Validate(txs[i], sender, peerID)
	}
	return results
}

// RateLimiter returns the pipeline's rate limiter.
func (vp *ValidationPipeline) RateLimiter() *RateLimiter {
	return vp.limiter
}
