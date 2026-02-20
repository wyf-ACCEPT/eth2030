// tx_replacement.go implements transaction replacement rules including
// price bump validation, EIP-1559-aware gas price comparison, and
// pending transaction promotion for the transaction pool.
package txpool

import (
	"errors"
	"math/big"
	"sort"

	"github.com/eth2028/eth2028/core/types"
)

// Default replacement policy constants.
const (
	DefaultMinPriceBump = 10   // 10% minimum price bump for replacements
	DefaultMaxPoolSize  = 4096 // maximum number of transactions in the pool
	DefaultAccountSlots = 16   // maximum pending transactions per account
)

// Replacement-specific errors.
var (
	ErrNilTransaction      = errors.New("nil transaction")
	ErrNonceMismatch       = errors.New("replacement nonce mismatch")
	ErrInsufficientBump    = errors.New("replacement price bump insufficient")
	ErrInsufficientTipBump = errors.New("replacement tip bump insufficient")
	ErrPoolCapacity        = errors.New("pool at maximum capacity")
	ErrAccountFull         = errors.New("account slot limit exceeded")
)

// ReplacementPolicy defines the rules for transaction replacement in the pool.
// It governs the minimum price bump needed to replace an existing transaction,
// the maximum pool size, and the per-account pending slot limit.
type ReplacementPolicy struct {
	// MinPriceBump is the minimum percentage increase in gas price required
	// for a new transaction to replace an existing one with the same nonce.
	// Default: 10 (i.e. 10%).
	MinPriceBump int

	// MaxPoolSize is the maximum number of transactions the pool will hold.
	MaxPoolSize int

	// AccountSlots is the maximum number of pending transactions allowed
	// per sender account.
	AccountSlots int
}

// DefaultReplacementPolicy returns a ReplacementPolicy with sensible defaults.
func DefaultReplacementPolicy() *ReplacementPolicy {
	return &ReplacementPolicy{
		MinPriceBump: DefaultMinPriceBump,
		MaxPoolSize:  DefaultMaxPoolSize,
		AccountSlots: DefaultAccountSlots,
	}
}

// NewReplacementPolicy creates a ReplacementPolicy with the given parameters.
// Zero or negative values fall back to defaults.
func NewReplacementPolicy(priceBump, poolSize, accountSlots int) *ReplacementPolicy {
	p := &ReplacementPolicy{
		MinPriceBump: priceBump,
		MaxPoolSize:  poolSize,
		AccountSlots: accountSlots,
	}
	if p.MinPriceBump <= 0 {
		p.MinPriceBump = DefaultMinPriceBump
	}
	if p.MaxPoolSize <= 0 {
		p.MaxPoolSize = DefaultMaxPoolSize
	}
	if p.AccountSlots <= 0 {
		p.AccountSlots = DefaultAccountSlots
	}
	return p
}

// CanReplace checks whether newTx can replace existing in the pool according
// to the replacement policy. Both transactions must have the same nonce.
// Returns true if replacement is allowed, or an error explaining why not.
func (p *ReplacementPolicy) CanReplace(existing, newTx *types.Transaction) (bool, error) {
	if existing == nil || newTx == nil {
		return false, ErrNilTransaction
	}
	if existing.Nonce() != newTx.Nonce() {
		return false, ErrNonceMismatch
	}

	// Compute the required price bump.
	bump := ComputePriceBump(existing, newTx)
	if bump < p.MinPriceBump {
		return false, ErrInsufficientBump
	}

	// For EIP-1559-style transactions, also validate the tip cap bump.
	if isDynFee(existing) && isDynFee(newTx) {
		tipBump := computeTipBump(existing, newTx)
		if tipBump < p.MinPriceBump {
			return false, ErrInsufficientTipBump
		}
	}

	return true, nil
}

// ComputePriceBump calculates the percentage price increase of newTx over
// existing based on their gas prices (GasPrice for legacy, GasFeeCap for
// EIP-1559). Returns the bump as an integer percentage (e.g. 10 for 10%).
// If existing has zero gas price, any positive new price yields 100%.
func ComputePriceBump(existing, newTx *types.Transaction) int {
	if existing == nil || newTx == nil {
		return 0
	}

	oldPrice := effectivePrice(existing)
	newPrice := effectivePrice(newTx)

	if oldPrice.Sign() == 0 {
		if newPrice.Sign() > 0 {
			return 100
		}
		return 0
	}

	// bump% = ((newPrice - oldPrice) * 100) / oldPrice
	diff := new(big.Int).Sub(newPrice, oldPrice)
	if diff.Sign() <= 0 {
		return 0
	}

	pct := new(big.Int).Mul(diff, big.NewInt(100))
	pct.Div(pct, oldPrice)
	return int(pct.Int64())
}

// computeTipBump calculates the percentage tip cap increase of newTx over
// existing. Used for EIP-1559-style transactions.
func computeTipBump(existing, newTx *types.Transaction) int {
	oldTip := existing.GasTipCap()
	newTip := newTx.GasTipCap()
	if oldTip == nil {
		oldTip = new(big.Int)
	}
	if newTip == nil {
		newTip = new(big.Int)
	}

	if oldTip.Sign() == 0 {
		if newTip.Sign() > 0 {
			return 100
		}
		return 0
	}

	diff := new(big.Int).Sub(newTip, oldTip)
	if diff.Sign() <= 0 {
		return 0
	}

	pct := new(big.Int).Mul(diff, big.NewInt(100))
	pct.Div(pct, oldTip)
	return int(pct.Int64())
}

// CompareEffectiveGasPrice compares two transactions by their effective gas
// price given a base fee. Returns -1 if a < b, 0 if equal, +1 if a > b.
// For legacy and access list transactions, the effective price is GasPrice.
// For EIP-1559 transactions: min(GasFeeCap, baseFee + GasTipCap).
func CompareEffectiveGasPrice(a, b *types.Transaction, baseFee *big.Int) int {
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return -1
	}
	if b == nil {
		return 1
	}

	priceA := EffectiveGasPriceCapped(a, baseFee)
	priceB := EffectiveGasPriceCapped(b, baseFee)
	return priceA.Cmp(priceB)
}

// EffectiveGasPriceCapped computes the effective gas price for a transaction
// considering the current baseFee. For legacy/access-list transactions,
// returns GasPrice. For EIP-1559: min(GasFeeCap, baseFee + GasTipCap).
// If baseFee is nil, returns GasFeeCap for dynamic transactions.
func EffectiveGasPriceCapped(tx *types.Transaction, baseFee *big.Int) *big.Int {
	if tx == nil {
		return new(big.Int)
	}
	return EffectiveGasPrice(tx, baseFee)
}

// SortByPrice sorts a slice of transactions by effective gas price in
// descending order (highest price first). The baseFee is used for
// EIP-1559-aware price calculation. Returns a new sorted slice; the
// original is not modified.
func SortByPrice(txs []*types.Transaction, baseFee *big.Int) []*types.Transaction {
	if len(txs) == 0 {
		return nil
	}

	sorted := make([]*types.Transaction, len(txs))
	copy(sorted, txs)

	sort.SliceStable(sorted, func(i, j int) bool {
		pi := EffectiveGasPriceCapped(sorted[i], baseFee)
		pj := EffectiveGasPriceCapped(sorted[j], baseFee)
		return pi.Cmp(pj) > 0
	})

	return sorted
}

// EffectiveTip computes the miner tip for a transaction given the current
// baseFee. For legacy transactions, tip = GasPrice - baseFee (clamped to 0).
// For EIP-1559 transactions: min(GasTipCap, GasFeeCap - baseFee).
// If baseFee is nil, returns GasTipCap (or GasPrice for legacy).
func EffectiveTip(tx *types.Transaction, baseFee *big.Int) *big.Int {
	if tx == nil {
		return new(big.Int)
	}

	// No base fee means the full tip cap is the effective tip.
	if baseFee == nil {
		tip := tx.GasTipCap()
		if tip == nil {
			return new(big.Int)
		}
		return new(big.Int).Set(tip)
	}

	feeCap := tx.GasFeeCap()
	tipCap := tx.GasTipCap()
	if feeCap == nil {
		feeCap = tx.GasPrice()
	}
	if tipCap == nil {
		tipCap = tx.GasPrice()
	}
	if feeCap == nil {
		return new(big.Int)
	}
	if tipCap == nil {
		tipCap = new(big.Int)
	}

	// effectiveTip = min(tipCap, feeCap - baseFee)
	availableTip := new(big.Int).Sub(feeCap, baseFee)
	if availableTip.Sign() < 0 {
		return new(big.Int)
	}

	if tipCap.Cmp(availableTip) < 0 {
		return new(big.Int).Set(tipCap)
	}
	return availableTip
}

// AccountPending represents the pending state of a single account in the
// transaction pool, tracking the expected next nonce and the pending
// transaction list.
type AccountPending struct {
	// Nonce is the current state nonce for this account (i.e. the nonce
	// of the next expected transaction).
	Nonce uint64

	// Transactions is the list of pending transactions from this account,
	// sorted by nonce ascending.
	Transactions []*types.Transaction
}

// Len returns the number of pending transactions for this account.
func (ap *AccountPending) Len() int {
	if ap == nil {
		return 0
	}
	return len(ap.Transactions)
}

// Executable returns the transactions that are immediately executable
// starting from the account's current state nonce. A transaction is
// executable if it has a nonce that forms a contiguous sequence starting
// at ap.Nonce.
func (ap *AccountPending) Executable() []*types.Transaction {
	if ap == nil || len(ap.Transactions) == 0 {
		return nil
	}

	var result []*types.Transaction
	expected := ap.Nonce

	for _, tx := range ap.Transactions {
		if tx.Nonce() != expected {
			break
		}
		result = append(result, tx)
		expected++
	}

	return result
}

// GetPromotable collects all promotable (immediately executable) transactions
// from the pending map, sorted by effective gas price (descending). A
// transaction is promotable if its nonce matches the account's expected next
// nonce in a contiguous sequence. The baseFee is used for EIP-1559-aware
// price ordering.
func GetPromotable(pending map[[20]byte]*AccountPending, baseFee *big.Int) []*types.Transaction {
	if len(pending) == 0 {
		return nil
	}

	var promotable []*types.Transaction

	for _, acct := range pending {
		if acct == nil || len(acct.Transactions) == 0 {
			continue
		}

		// Collect executable transactions starting from the state nonce.
		executable := acct.Executable()
		promotable = append(promotable, executable...)
	}

	if len(promotable) == 0 {
		return nil
	}

	// Sort by effective gas price descending (highest first for block building).
	return SortByPrice(promotable, baseFee)
}

// isDynFee returns true if the transaction uses EIP-1559-style fee mechanics
// (DynamicFeeTx, BlobTx, or SetCodeTx).
func isDynFee(tx *types.Transaction) bool {
	if tx == nil {
		return false
	}
	return tx.Type() == types.DynamicFeeTxType ||
		tx.Type() == types.BlobTxType ||
		tx.Type() == types.SetCodeTxType
}

// effectivePrice returns the primary gas price field for comparison.
// For legacy/access-list: GasPrice. For EIP-1559-style: GasFeeCap.
func effectivePrice(tx *types.Transaction) *big.Int {
	if tx == nil {
		return new(big.Int)
	}
	if isDynFee(tx) {
		fc := tx.GasFeeCap()
		if fc == nil {
			return new(big.Int)
		}
		return new(big.Int).Set(fc)
	}
	gp := tx.GasPrice()
	if gp == nil {
		return new(big.Int)
	}
	return new(big.Int).Set(gp)
}

// FilterByMinTip filters transactions, keeping only those with an effective
// tip at or above minTip. Returns a new slice.
func FilterByMinTip(txs []*types.Transaction, baseFee, minTip *big.Int) []*types.Transaction {
	if len(txs) == 0 || minTip == nil {
		return txs
	}

	var result []*types.Transaction
	for _, tx := range txs {
		tip := EffectiveTip(tx, baseFee)
		if tip.Cmp(minTip) >= 0 {
			result = append(result, tx)
		}
	}
	return result
}

// GroupByNonce groups transactions by nonce and returns a map.
// This is useful for identifying replacement candidates.
func GroupByNonce(txs []*types.Transaction) map[uint64][]*types.Transaction {
	groups := make(map[uint64][]*types.Transaction)
	for _, tx := range txs {
		if tx == nil {
			continue
		}
		groups[tx.Nonce()] = append(groups[tx.Nonce()], tx)
	}
	return groups
}

// BestByNonce selects the highest-priced transaction for each nonce from
// a list. The baseFee is used for EIP-1559-aware comparison. Returns a
// deduplicated list sorted by nonce ascending.
func BestByNonce(txs []*types.Transaction, baseFee *big.Int) []*types.Transaction {
	groups := GroupByNonce(txs)
	if len(groups) == 0 {
		return nil
	}

	result := make([]*types.Transaction, 0, len(groups))
	for _, group := range groups {
		best := group[0]
		for _, tx := range group[1:] {
			if CompareEffectiveGasPrice(tx, best, baseFee) > 0 {
				best = tx
			}
		}
		result = append(result, best)
	}

	// Sort by nonce ascending.
	sort.Slice(result, func(i, j int) bool {
		return result[i].Nonce() < result[j].Nonce()
	})

	return result
}
