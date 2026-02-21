// replacement_policy.go implements a transaction replacement-by-fee policy
// engine. It enforces EIP-1559 RBF rules (10% bump minimum), handles blob
// transaction replacement (EIP-4844 with 100% blob fee bump), tracks
// replacement chains per account/nonce, and prevents spam via replacement
// count limits.
package txpool

import (
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// RBF policy constants.
const (
	// RBFMinFeeBump is the minimum gas fee bump percentage for standard txs.
	RBFMinFeeBump = 10 // 10%

	// RBFMinTipBump is the minimum tip cap bump percentage for EIP-1559 txs.
	RBFMinTipBump = 10 // 10%

	// RBFBlobFeeBump is the minimum blob fee bump for blob tx replacement.
	// EIP-4844 requires 100% bump for blob fee cap.
	RBFBlobFeeBump = 100 // 100%

	// RBFBlobGasFeeBump is the minimum gas fee bump for blob txs.
	RBFBlobGasFeeBump = 10 // 10% same as regular txs

	// RBFMaxReplacements is the maximum number of times a single
	// (sender, nonce) slot can be replaced, to prevent spam.
	RBFMaxReplacements = 25

	// RBFMaxChainDepth is the maximum replacement chain depth tracked per
	// account (across all nonces).
	RBFMaxChainDepth = 100
)

// RBF policy errors.
var (
	ErrRBFNilTx              = errors.New("rbf: nil transaction")
	ErrRBFNonceMismatch      = errors.New("rbf: replacement nonce mismatch")
	ErrRBFInsufficientFeeBump = errors.New("rbf: fee cap bump below minimum")
	ErrRBFInsufficientTipBump = errors.New("rbf: tip cap bump below minimum")
	ErrRBFInsufficientBlobBump = errors.New("rbf: blob fee cap bump below minimum")
	ErrRBFMaxReplacements    = errors.New("rbf: maximum replacement count exceeded")
	ErrRBFMaxChainDepth      = errors.New("rbf: account replacement chain too deep")
	ErrRBFTypeMismatch       = errors.New("rbf: cannot replace blob tx with non-blob tx")
)

// ReplacementChainEntry records a single replacement event.
type ReplacementChainEntry struct {
	TxHash    types.Hash // Hash of the replacement transaction.
	Nonce     uint64     // Nonce of the transaction slot.
	Timestamp time.Time  // When the replacement occurred.
	FeeBump   int        // Actual fee bump percentage applied.
}

// nonceSlot tracks replacement count and history for a single (sender, nonce).
type nonceSlot struct {
	count   int                    // Number of replacements at this nonce.
	entries []ReplacementChainEntry // Replacement history.
}

// accountChain tracks replacement data for a single sender account.
type accountChain struct {
	slots      map[uint64]*nonceSlot // nonce -> slot.
	totalDepth int                   // Total replacements across all nonces.
}

// RBFPolicyConfig configures the RBF policy engine.
type RBFPolicyConfig struct {
	MinFeeBump      int // Minimum fee cap bump percentage (default 10%).
	MinTipBump      int // Minimum tip cap bump percentage (default 10%).
	BlobFeeBump     int // Minimum blob fee cap bump percentage (default 100%).
	MaxReplacements int // Max replacement count per (sender, nonce).
	MaxChainDepth   int // Max total replacements per account.
}

// DefaultRBFPolicyConfig returns production defaults.
func DefaultRBFPolicyConfig() RBFPolicyConfig {
	return RBFPolicyConfig{
		MinFeeBump:      RBFMinFeeBump,
		MinTipBump:      RBFMinTipBump,
		BlobFeeBump:     RBFBlobFeeBump,
		MaxReplacements: RBFMaxReplacements,
		MaxChainDepth:   RBFMaxChainDepth,
	}
}

// RBFPolicyEngine enforces replace-by-fee rules for the transaction pool.
// It validates fee bumps, tracks replacement chains, and enforces spam
// limits. All methods are safe for concurrent use.
type RBFPolicyEngine struct {
	mu     sync.RWMutex
	config RBFPolicyConfig
	chains map[types.Address]*accountChain // sender -> chain.
	stats  RBFStats
}

// RBFStats holds replacement statistics.
type RBFStats struct {
	TotalAttempts   uint64 // Total replacement attempts.
	TotalAccepted   uint64 // Successful replacements.
	TotalRejected   uint64 // Rejected replacements.
	FeeRejects      uint64 // Rejected due to insufficient fee bump.
	TipRejects      uint64 // Rejected due to insufficient tip bump.
	BlobFeeRejects  uint64 // Rejected due to insufficient blob fee bump.
	SpamRejects     uint64 // Rejected due to max replacements.
	ChainRejects    uint64 // Rejected due to chain depth.
}

// NewRBFPolicyEngine creates an RBF policy engine with the given config.
func NewRBFPolicyEngine(config RBFPolicyConfig) *RBFPolicyEngine {
	if config.MinFeeBump <= 0 {
		config.MinFeeBump = RBFMinFeeBump
	}
	if config.MinTipBump <= 0 {
		config.MinTipBump = RBFMinTipBump
	}
	if config.BlobFeeBump <= 0 {
		config.BlobFeeBump = RBFBlobFeeBump
	}
	if config.MaxReplacements <= 0 {
		config.MaxReplacements = RBFMaxReplacements
	}
	if config.MaxChainDepth <= 0 {
		config.MaxChainDepth = RBFMaxChainDepth
	}
	return &RBFPolicyEngine{
		config: config,
		chains: make(map[types.Address]*accountChain),
	}
}

// ValidateReplacement checks whether newTx can replace existing according
// to the RBF policy. Both must have the same nonce. The sender address is
// used for per-account replacement tracking.
func (e *RBFPolicyEngine) ValidateReplacement(sender types.Address, existing, newTx *types.Transaction) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.stats.TotalAttempts++

	if existing == nil || newTx == nil {
		e.stats.TotalRejected++
		return ErrRBFNilTx
	}
	if existing.Nonce() != newTx.Nonce() {
		e.stats.TotalRejected++
		return ErrRBFNonceMismatch
	}

	nonce := existing.Nonce()

	// Check spam limits.
	if err := e.checkSpamLimitsLocked(sender, nonce); err != nil {
		e.stats.TotalRejected++
		e.stats.SpamRejects++
		return err
	}

	// Blob tx type enforcement: cannot replace a blob tx with a non-blob tx.
	existingIsBlob := existing.Type() == types.BlobTxType
	newIsBlob := newTx.Type() == types.BlobTxType
	if existingIsBlob && !newIsBlob {
		e.stats.TotalRejected++
		return ErrRBFTypeMismatch
	}

	// Check fee cap bump.
	feeBump := computeFeeBump(existing, newTx)
	if feeBump < e.config.MinFeeBump {
		e.stats.TotalRejected++
		e.stats.FeeRejects++
		return ErrRBFInsufficientFeeBump
	}

	// For EIP-1559-style txs, also validate tip cap bump.
	if isDynFeeTx(existing) && isDynFeeTx(newTx) {
		tipBump := computeTipCapBump(existing, newTx)
		if tipBump < e.config.MinTipBump {
			e.stats.TotalRejected++
			e.stats.TipRejects++
			return ErrRBFInsufficientTipBump
		}
	}

	// For blob transactions, validate blob fee cap bump.
	if existingIsBlob && newIsBlob {
		blobBump := computeBlobFeeBump(existing, newTx)
		if blobBump < e.config.BlobFeeBump {
			e.stats.TotalRejected++
			e.stats.BlobFeeRejects++
			return ErrRBFInsufficientBlobBump
		}
	}

	// Record the replacement in the chain tracker.
	e.recordReplacementLocked(sender, nonce, newTx.Hash(), feeBump)
	e.stats.TotalAccepted++
	return nil
}

// checkSpamLimitsLocked checks per-nonce and per-account replacement limits.
// Caller must hold e.mu.
func (e *RBFPolicyEngine) checkSpamLimitsLocked(sender types.Address, nonce uint64) error {
	ac, ok := e.chains[sender]
	if !ok {
		return nil // No prior replacements.
	}

	// Check account-wide chain depth.
	if ac.totalDepth >= e.config.MaxChainDepth {
		e.stats.ChainRejects++
		return ErrRBFMaxChainDepth
	}

	// Check per-nonce replacement count.
	slot, ok := ac.slots[nonce]
	if !ok {
		return nil
	}
	if slot.count >= e.config.MaxReplacements {
		return ErrRBFMaxReplacements
	}
	return nil
}

// recordReplacementLocked records a successful replacement in the chain.
// Caller must hold e.mu.
func (e *RBFPolicyEngine) recordReplacementLocked(sender types.Address, nonce uint64, txHash types.Hash, feeBump int) {
	ac, ok := e.chains[sender]
	if !ok {
		ac = &accountChain{
			slots: make(map[uint64]*nonceSlot),
		}
		e.chains[sender] = ac
	}

	slot, ok := ac.slots[nonce]
	if !ok {
		slot = &nonceSlot{}
		ac.slots[nonce] = slot
	}

	slot.count++
	slot.entries = append(slot.entries, ReplacementChainEntry{
		TxHash:    txHash,
		Nonce:     nonce,
		Timestamp: time.Now(),
		FeeBump:   feeBump,
	})
	ac.totalDepth++
}

// ReplacementCount returns the number of replacements for a specific
// (sender, nonce) slot.
func (e *RBFPolicyEngine) ReplacementCount(sender types.Address, nonce uint64) int {
	e.mu.RLock()
	defer e.mu.RUnlock()

	ac, ok := e.chains[sender]
	if !ok {
		return 0
	}
	slot, ok := ac.slots[nonce]
	if !ok {
		return 0
	}
	return slot.count
}

// AccountReplacementDepth returns the total replacement depth for a sender.
func (e *RBFPolicyEngine) AccountReplacementDepth(sender types.Address) int {
	e.mu.RLock()
	defer e.mu.RUnlock()

	ac, ok := e.chains[sender]
	if !ok {
		return 0
	}
	return ac.totalDepth
}

// ReplacementChain returns the replacement history for a (sender, nonce) slot.
func (e *RBFPolicyEngine) ReplacementChain(sender types.Address, nonce uint64) []ReplacementChainEntry {
	e.mu.RLock()
	defer e.mu.RUnlock()

	ac, ok := e.chains[sender]
	if !ok {
		return nil
	}
	slot, ok := ac.slots[nonce]
	if !ok {
		return nil
	}
	// Return a copy.
	result := make([]ReplacementChainEntry, len(slot.entries))
	copy(result, slot.entries)
	return result
}

// ClearAccount removes all replacement tracking data for a sender.
// Should be called when an account's pending transactions are confirmed.
func (e *RBFPolicyEngine) ClearAccount(sender types.Address) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.chains, sender)
}

// ClearNonce removes replacement tracking for a specific (sender, nonce).
// Should be called when a transaction is confirmed at that nonce.
func (e *RBFPolicyEngine) ClearNonce(sender types.Address, nonce uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	ac, ok := e.chains[sender]
	if !ok {
		return
	}
	slot, ok := ac.slots[nonce]
	if !ok {
		return
	}
	ac.totalDepth -= slot.count
	if ac.totalDepth < 0 {
		ac.totalDepth = 0
	}
	delete(ac.slots, nonce)

	// Clean up empty account chain.
	if len(ac.slots) == 0 {
		delete(e.chains, sender)
	}
}

// Stats returns a snapshot of the replacement statistics.
func (e *RBFPolicyEngine) Stats() RBFStats {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.stats
}

// TrackedAccounts returns the number of accounts with active replacement chains.
func (e *RBFPolicyEngine) TrackedAccounts() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.chains)
}

// MinFeeBumpRequired returns the minimum fee cap a replacement tx needs
// to have, given the existing transaction's fee cap and the configured
// bump percentage. Useful for user-facing fee suggestions.
func (e *RBFPolicyEngine) MinFeeBumpRequired(existing *types.Transaction) *big.Int {
	e.mu.RLock()
	bumpPct := e.config.MinFeeBump
	e.mu.RUnlock()

	if existing == nil {
		return new(big.Int)
	}
	feeCap := existing.GasFeeCap()
	if feeCap == nil {
		feeCap = existing.GasPrice()
	}
	if feeCap == nil {
		return new(big.Int)
	}

	// required = feeCap * (100 + bumpPct) / 100
	mult := big.NewInt(int64(100 + bumpPct))
	result := new(big.Int).Mul(feeCap, mult)
	result.Div(result, big.NewInt(100))
	return result
}

// MinBlobFeeBumpRequired returns the minimum blob fee cap a replacement blob
// tx needs, given the existing blob transaction.
func (e *RBFPolicyEngine) MinBlobFeeBumpRequired(existing *types.Transaction) *big.Int {
	e.mu.RLock()
	blobBumpPct := e.config.BlobFeeBump
	e.mu.RUnlock()

	if existing == nil {
		return new(big.Int)
	}
	blobFeeCap := existing.BlobGasFeeCap()
	if blobFeeCap == nil {
		return new(big.Int)
	}

	mult := big.NewInt(int64(100 + blobBumpPct))
	result := new(big.Int).Mul(blobFeeCap, mult)
	result.Div(result, big.NewInt(100))
	return result
}

// --- internal helpers ---

// computeFeeBump calculates the percentage fee cap bump of newTx over
// existing. For legacy txs uses GasPrice, for dynamic uses GasFeeCap.
func computeFeeBump(existing, newTx *types.Transaction) int {
	oldFee := txFeeCap(existing)
	newFee := txFeeCap(newTx)
	return pctBump(oldFee, newFee)
}

// computeTipCapBump calculates the percentage tip cap bump.
func computeTipCapBump(existing, newTx *types.Transaction) int {
	oldTip := existing.GasTipCap()
	newTip := newTx.GasTipCap()
	if oldTip == nil {
		oldTip = new(big.Int)
	}
	if newTip == nil {
		newTip = new(big.Int)
	}
	return pctBump(oldTip, newTip)
}

// computeBlobFeeBump calculates the percentage blob fee cap bump.
func computeBlobFeeBump(existing, newTx *types.Transaction) int {
	oldBlobFee := existing.BlobGasFeeCap()
	newBlobFee := newTx.BlobGasFeeCap()
	if oldBlobFee == nil {
		oldBlobFee = new(big.Int)
	}
	if newBlobFee == nil {
		newBlobFee = new(big.Int)
	}
	return pctBump(oldBlobFee, newBlobFee)
}

// txFeeCap returns the fee cap for a transaction: GasFeeCap for dynamic
// fee txs, GasPrice for legacy.
func txFeeCap(tx *types.Transaction) *big.Int {
	if tx == nil {
		return new(big.Int)
	}
	fc := tx.GasFeeCap()
	if fc != nil && fc.Sign() > 0 {
		return fc
	}
	gp := tx.GasPrice()
	if gp != nil {
		return gp
	}
	return new(big.Int)
}

// pctBump calculates the percentage increase of newVal over oldVal.
// Returns 0 if oldVal is zero and newVal is not positive, or 100 if
// oldVal is zero and newVal is positive.
func pctBump(oldVal, newVal *big.Int) int {
	if oldVal.Sign() == 0 {
		if newVal.Sign() > 0 {
			return 100
		}
		return 0
	}
	diff := new(big.Int).Sub(newVal, oldVal)
	if diff.Sign() <= 0 {
		return 0
	}
	pct := new(big.Int).Mul(diff, big.NewInt(100))
	pct.Div(pct, oldVal)
	return int(pct.Int64())
}

// isDynFeeTx returns true if the transaction uses EIP-1559-style fees.
func isDynFeeTx(tx *types.Transaction) bool {
	if tx == nil {
		return false
	}
	return tx.Type() == types.DynamicFeeTxType ||
		tx.Type() == types.BlobTxType ||
		tx.Type() == types.SetCodeTxType
}
