package encrypted

import (
	"errors"
	"math/big"
	"sort"
	"sync"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

var (
	ErrRevealWindowClosed = errors.New("encrypted: reveal window is closed")
	ErrRevealWindowOpen   = errors.New("encrypted: reveal window still open, cannot finalize")
	ErrNoShares           = errors.New("encrypted: no decryption shares received")
	ErrAlreadyFinalized   = errors.New("encrypted: round already finalized")
	ErrRoundNotFound      = errors.New("encrypted: decryption round not found")
)

// OrderingPolicy defines how transactions are ordered after decryption.
type OrderingPolicy interface {
	// Order sorts the given entries according to the policy.
	Order(entries []OrderableEntry) []OrderableEntry
	// Name returns the policy name for logging/config.
	Name() string
}

// OrderableEntry pairs a commit entry with its revealed transaction for ordering.
type OrderableEntry struct {
	Commit      *CommitEntry
	Transaction *types.Transaction
}

// TimeBasedOrdering orders transactions by commit timestamp (first-come-first-served).
// This is the fairest ordering: earliest commit gets highest priority regardless of
// gas price, removing the incentive for MEV bots to outbid honest users.
type TimeBasedOrdering struct{}

func (o *TimeBasedOrdering) Name() string { return "time-based" }

func (o *TimeBasedOrdering) Order(entries []OrderableEntry) []OrderableEntry {
	sorted := make([]OrderableEntry, len(entries))
	copy(sorted, entries)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Commit.Commit.Timestamp < sorted[j].Commit.Commit.Timestamp
	})
	return sorted
}

// FeeBasedOrdering orders transactions by effective gas price (highest first)
// after reveal. This maximizes block builder revenue but offers no MEV protection.
type FeeBasedOrdering struct{}

func (o *FeeBasedOrdering) Name() string { return "fee-based" }

func (o *FeeBasedOrdering) Order(entries []OrderableEntry) []OrderableEntry {
	sorted := make([]OrderableEntry, len(entries))
	copy(sorted, entries)
	sort.SliceStable(sorted, func(i, j int) bool {
		pi := effectiveGasPrice(sorted[i].Transaction)
		pj := effectiveGasPrice(sorted[j].Transaction)
		return pi.Cmp(pj) > 0
	})
	return sorted
}

// effectiveGasPrice returns the effective gas price for ordering.
// For EIP-1559 transactions, this returns GasTipCap (priority fee).
// For legacy transactions, this returns GasPrice.
func effectiveGasPrice(tx *types.Transaction) *big.Int {
	if tx == nil {
		return big.NewInt(0)
	}
	tip := tx.GasTipCap()
	if tip != nil {
		return tip
	}
	price := tx.GasPrice()
	if price != nil {
		return price
	}
	return big.NewInt(0)
}

// HybridOrdering combines time priority with fee priority using configurable weights.
// Each entry gets a score: score = (1 - FeeWeight) * timeScore + FeeWeight * feeScore.
// - FeeWeight = 0.0: pure time-based (first-come-first-served)
// - FeeWeight = 1.0: pure fee-based (highest bidder wins)
// - FeeWeight = 0.3: 70% weight to commit order, 30% to fee (recommended)
type HybridOrdering struct {
	FeeWeight float64 // 0.0 to 1.0: how much weight to give to fees
}

func (o *HybridOrdering) Name() string { return "hybrid" }

func (o *HybridOrdering) Order(entries []OrderableEntry) []OrderableEntry {
	if len(entries) == 0 {
		return entries
	}

	sorted := make([]OrderableEntry, len(entries))
	copy(sorted, entries)

	// Clamp fee weight to [0, 1].
	w := o.FeeWeight
	if w < 0 {
		w = 0
	}
	if w > 1 {
		w = 1
	}

	// Find min/max timestamp and fee for normalization.
	minTs, maxTs := sorted[0].Commit.Commit.Timestamp, sorted[0].Commit.Commit.Timestamp
	minFee := effectiveGasPrice(sorted[0].Transaction)
	maxFee := new(big.Int).Set(minFee)

	for _, e := range sorted[1:] {
		ts := e.Commit.Commit.Timestamp
		if ts < minTs {
			minTs = ts
		}
		if ts > maxTs {
			maxTs = ts
		}
		fee := effectiveGasPrice(e.Transaction)
		if fee.Cmp(minFee) < 0 {
			minFee = new(big.Int).Set(fee)
		}
		if fee.Cmp(maxFee) > 0 {
			maxFee = new(big.Int).Set(fee)
		}
	}

	tsRange := float64(maxTs - minTs)
	feeRange := new(big.Int).Sub(maxFee, minFee)
	feeRangeF := float64(feeRange.Int64()) // safe for reasonable gas prices

	// Compute scores.
	type scored struct {
		entry OrderableEntry
		score float64
	}
	scores := make([]scored, len(sorted))
	for i, e := range sorted {
		// Time score: earlier = higher score (1.0 for earliest, 0.0 for latest).
		var timeScore float64
		if tsRange > 0 {
			timeScore = 1.0 - float64(e.Commit.Commit.Timestamp-minTs)/tsRange
		} else {
			timeScore = 1.0
		}

		// Fee score: higher fee = higher score (1.0 for highest, 0.0 for lowest).
		var feeScore float64
		if feeRangeF > 0 {
			fee := effectiveGasPrice(e.Transaction)
			feeScore = float64(new(big.Int).Sub(fee, minFee).Int64()) / feeRangeF
		} else {
			feeScore = 1.0
		}

		scores[i] = scored{
			entry: e,
			score: (1-w)*timeScore + w*feeScore,
		}
	}

	sort.SliceStable(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	result := make([]OrderableEntry, len(scores))
	for i, s := range scores {
		result[i] = s.entry
	}
	return result
}

// RevealWindow defines the time window during which decryption shares
// must be submitted before a round can be finalized.
type RevealWindow struct {
	Start    time.Time
	Duration time.Duration
}

// IsOpen returns true if the reveal window is currently accepting shares.
func (rw *RevealWindow) IsOpen(now time.Time) bool {
	return !now.Before(rw.Start) && now.Before(rw.Start.Add(rw.Duration))
}

// IsClosed returns true if the reveal window has passed.
func (rw *RevealWindow) IsClosed(now time.Time) bool {
	return !now.Before(rw.Start.Add(rw.Duration))
}

// DecryptionRound tracks a single threshold decryption round.
type DecryptionRound struct {
	mu              sync.Mutex
	ID              uint64
	Window          *RevealWindow
	Threshold       int // minimum shares needed
	TotalParties    int
	Shares          map[int][]byte // partyIndex -> decryption share bytes
	Finalized       bool
	FinalizedResult []byte // decrypted data (set after finalization)
}

// DecryptionCoordinator manages threshold decryption rounds for the encrypted mempool.
// It collects decryption shares from committee members and finalizes rounds
// when enough shares are collected and the reveal window closes.
type DecryptionCoordinator struct {
	mu     sync.RWMutex
	rounds map[uint64]*DecryptionRound
	nextID uint64
}

// NewDecryptionCoordinator creates a new decryption coordinator.
func NewDecryptionCoordinator() *DecryptionCoordinator {
	return &DecryptionCoordinator{
		rounds: make(map[uint64]*DecryptionRound),
	}
}

// StartRound begins a new decryption round with the given parameters.
func (dc *DecryptionCoordinator) StartRound(threshold, totalParties int, windowDuration time.Duration) uint64 {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	id := dc.nextID
	dc.nextID++

	dc.rounds[id] = &DecryptionRound{
		ID:           id,
		Threshold:    threshold,
		TotalParties: totalParties,
		Window: &RevealWindow{
			Start:    time.Now(),
			Duration: windowDuration,
		},
		Shares: make(map[int][]byte),
	}

	return id
}

// SubmitShare adds a decryption share to the specified round.
func (dc *DecryptionCoordinator) SubmitShare(roundID uint64, partyIndex int, share []byte) error {
	dc.mu.RLock()
	round, ok := dc.rounds[roundID]
	dc.mu.RUnlock()

	if !ok {
		return ErrRoundNotFound
	}

	round.mu.Lock()
	defer round.mu.Unlock()

	if round.Finalized {
		return ErrAlreadyFinalized
	}

	if round.Window.IsClosed(time.Now()) {
		return ErrRevealWindowClosed
	}

	round.Shares[partyIndex] = share
	return nil
}

// ShareCount returns the number of shares collected for a round.
func (dc *DecryptionCoordinator) ShareCount(roundID uint64) (int, error) {
	dc.mu.RLock()
	round, ok := dc.rounds[roundID]
	dc.mu.RUnlock()

	if !ok {
		return 0, ErrRoundNotFound
	}

	round.mu.Lock()
	defer round.mu.Unlock()

	return len(round.Shares), nil
}

// HasThreshold returns true if enough shares have been collected.
func (dc *DecryptionCoordinator) HasThreshold(roundID uint64) (bool, error) {
	dc.mu.RLock()
	round, ok := dc.rounds[roundID]
	dc.mu.RUnlock()

	if !ok {
		return false, ErrRoundNotFound
	}

	round.mu.Lock()
	defer round.mu.Unlock()

	return len(round.Shares) >= round.Threshold, nil
}

// FinalizeRound marks a round as finalized with the decrypted result.
// The caller is responsible for performing the actual threshold decryption
// using the collected shares.
func (dc *DecryptionCoordinator) FinalizeRound(roundID uint64, result []byte) error {
	dc.mu.RLock()
	round, ok := dc.rounds[roundID]
	dc.mu.RUnlock()

	if !ok {
		return ErrRoundNotFound
	}

	round.mu.Lock()
	defer round.mu.Unlock()

	if round.Finalized {
		return ErrAlreadyFinalized
	}

	if len(round.Shares) < round.Threshold {
		return ErrNoShares
	}

	round.Finalized = true
	round.FinalizedResult = result
	return nil
}

// GetRoundResult returns the finalized result for a round.
func (dc *DecryptionCoordinator) GetRoundResult(roundID uint64) ([]byte, error) {
	dc.mu.RLock()
	round, ok := dc.rounds[roundID]
	dc.mu.RUnlock()

	if !ok {
		return nil, ErrRoundNotFound
	}

	round.mu.Lock()
	defer round.mu.Unlock()

	if !round.Finalized {
		return nil, ErrRevealWindowOpen
	}

	return round.FinalizedResult, nil
}

// GetRoundShares returns the collected shares for a round.
func (dc *DecryptionCoordinator) GetRoundShares(roundID uint64) (map[int][]byte, error) {
	dc.mu.RLock()
	round, ok := dc.rounds[roundID]
	dc.mu.RUnlock()

	if !ok {
		return nil, ErrRoundNotFound
	}

	round.mu.Lock()
	defer round.mu.Unlock()

	// Return a copy to avoid data races.
	result := make(map[int][]byte, len(round.Shares))
	for k, v := range round.Shares {
		cpy := make([]byte, len(v))
		copy(cpy, v)
		result[k] = cpy
	}
	return result, nil
}
