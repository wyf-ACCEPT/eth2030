package core

import (
	"errors"
	"math/big"
	"sort"

	"github.com/eth2030/eth2030/core/types"
)

var (
	ErrEmptyBundle        = errors.New("mev: bundle has no transactions")
	ErrBundleTooLarge     = errors.New("mev: bundle exceeds maximum transaction count")
	ErrSandwichDetected   = errors.New("mev: sandwich attack pattern detected")
	ErrFrontrunDetected   = errors.New("mev: frontrunning pattern detected")
	ErrInvalidFairOrder   = errors.New("mev: transaction violates fair ordering rules")
)

// MaxBundleSize is the maximum number of transactions in a Flashbots bundle.
const MaxBundleSize = 32

// FlashbotsBundle represents a bundle of transactions for atomic inclusion.
// All transactions in the bundle are included together or not at all.
// The bundle specifies a target block and optional revert protection.
type FlashbotsBundle struct {
	Transactions []*types.Transaction
	BlockNumber  uint64         // target block for inclusion
	MinTimestamp uint64         // earliest valid timestamp (0 = no constraint)
	MaxTimestamp uint64         // latest valid timestamp (0 = no constraint)
	RevertingTxHashes []types.Hash // txs allowed to revert without failing bundle
}

// Validate checks that a bundle is well-formed.
func (b *FlashbotsBundle) Validate() error {
	if len(b.Transactions) == 0 {
		return ErrEmptyBundle
	}
	if len(b.Transactions) > MaxBundleSize {
		return ErrBundleTooLarge
	}
	if b.MinTimestamp > 0 && b.MaxTimestamp > 0 && b.MinTimestamp > b.MaxTimestamp {
		return errors.New("mev: min timestamp exceeds max timestamp")
	}
	return nil
}

// IsValidAtTime checks if the bundle is valid for the given block timestamp.
func (b *FlashbotsBundle) IsValidAtTime(timestamp uint64) bool {
	if b.MinTimestamp > 0 && timestamp < b.MinTimestamp {
		return false
	}
	if b.MaxTimestamp > 0 && timestamp > b.MaxTimestamp {
		return false
	}
	return true
}

// TotalGas returns the total gas used by all transactions in the bundle.
func (b *FlashbotsBundle) TotalGas() uint64 {
	var total uint64
	for _, tx := range b.Transactions {
		total += tx.Gas()
	}
	return total
}

// IsRevertAllowed checks if a transaction hash is in the revert-allowed list.
func (b *FlashbotsBundle) IsRevertAllowed(txHash types.Hash) bool {
	for _, h := range b.RevertingTxHashes {
		if h == txHash {
			return true
		}
	}
	return false
}

// MEVProtectionConfig configures MEV resistance features for the client.
type MEVProtectionConfig struct {
	// EnableSandwichDetection activates sandwich attack detection.
	EnableSandwichDetection bool
	// EnableFrontrunDetection activates frontrunning detection.
	EnableFrontrunDetection bool
	// EnableFairOrdering enforces fair ordering rules.
	EnableFairOrdering bool
	// MaxGasPriceRatio is the maximum allowed ratio between gas prices
	// of adjacent transactions to the same target. A ratio above this
	// threshold triggers frontrun detection. Default: 10.
	MaxGasPriceRatio uint64
	// SandwichProfitThreshold is the minimum profit (in wei) for a
	// suspected sandwich to be flagged. Default: 0 (flag all).
	SandwichProfitThreshold *big.Int
	// FairOrderMaxDelay is the maximum number of positions a transaction
	// can be delayed from its arrival-time position. Default: 5.
	FairOrderMaxDelay int
}

// DefaultMEVProtectionConfig returns a configuration with sane defaults.
func DefaultMEVProtectionConfig() *MEVProtectionConfig {
	return &MEVProtectionConfig{
		EnableSandwichDetection: true,
		EnableFrontrunDetection: true,
		EnableFairOrdering:      true,
		MaxGasPriceRatio:        10,
		SandwichProfitThreshold: big.NewInt(0),
		FairOrderMaxDelay:       5,
	}
}

// SandwichCandidate describes a detected sandwich attack pattern.
type SandwichCandidate struct {
	FrontTx  *types.Transaction // the frontrunning transaction
	VictimTx *types.Transaction // the victim transaction
	BackTx   *types.Transaction // the backrunning transaction
	Attacker types.Address      // suspected attacker address
}

// FrontrunCandidate describes a detected frontrunning pattern.
type FrontrunCandidate struct {
	Frontrunner *types.Transaction // the suspected frontrunning tx
	Victim      *types.Transaction // the victim transaction
	GasRatio    uint64             // ratio of frontrunner to victim gas price
}

// BackrunOpportunity describes a benign backrunning opportunity.
// Backrunning is generally considered non-harmful as it doesn't disadvantage
// the original transaction -- it merely captures leftover arbitrage.
type BackrunOpportunity struct {
	TriggerTx     *types.Transaction // transaction that creates the opportunity
	BackrunTx     *types.Transaction // the potential backrun
	TargetAddress types.Address      // the contract being targeted
}

// DetectSandwich performs heuristic sandwich attack detection on a list of
// transactions. A sandwich pattern is: txA from attacker -> txB from victim
// -> txC from attacker, where txA and txC target the same contract.
//
// The heuristic looks for:
// 1. Two transactions from the same sender bracketing a third
// 2. All three targeting the same contract address
// 3. The attacker's first tx has a higher gas price than the victim
func DetectSandwich(txs []*types.Transaction) []SandwichCandidate {
	if len(txs) < 3 {
		return nil
	}

	var candidates []SandwichCandidate

	// Build a map of sender -> tx indices for quick lookup.
	type txInfo struct {
		tx     *types.Transaction
		index  int
		sender types.Address
	}

	infos := make([]txInfo, len(txs))
	for i, tx := range txs {
		var sender types.Address
		if s := tx.Sender(); s != nil {
			sender = *s
		}
		infos[i] = txInfo{tx: tx, index: i, sender: sender}
	}

	// For each pair of txs from the same sender, check if there's a victim between them.
	for i := 0; i < len(infos); i++ {
		for j := i + 2; j < len(infos); j++ {
			// Same sender for front and back.
			if infos[i].sender.IsZero() || infos[i].sender != infos[j].sender {
				continue
			}

			front := infos[i].tx
			back := infos[j].tx

			// Both must target the same contract.
			if front.To() == nil || back.To() == nil {
				continue
			}
			if *front.To() != *back.To() {
				continue
			}

			// Check for victim txs in between that also target the same contract.
			for k := i + 1; k < j; k++ {
				victim := infos[k].tx
				if victim.To() == nil || *victim.To() != *front.To() {
					continue
				}

				// Victim must be from a different sender.
				if infos[k].sender == infos[i].sender {
					continue
				}

				// Front tx should have higher or equal gas price to get ordered first.
				frontPrice := txGasPrice(front)
				victimPrice := txGasPrice(victim)
				if frontPrice.Cmp(victimPrice) >= 0 {
					candidates = append(candidates, SandwichCandidate{
						FrontTx:  front,
						VictimTx: victim,
						BackTx:   back,
						Attacker: infos[i].sender,
					})
				}
			}
		}
	}

	return candidates
}

// DetectFrontrun detects potential frontrunning patterns in a transaction list.
// A frontrun is suspected when:
// 1. Two transactions target the same contract
// 2. They are adjacent (or nearly adjacent) in the ordering
// 3. The first has a significantly higher gas price (ratio > maxRatio)
// 4. They are from different senders
func DetectFrontrun(txs []*types.Transaction, maxRatio uint64) []FrontrunCandidate {
	if len(txs) < 2 || maxRatio == 0 {
		return nil
	}

	var candidates []FrontrunCandidate

	for i := 0; i < len(txs)-1; i++ {
		for j := i + 1; j < len(txs) && j <= i+3; j++ {
			txA := txs[i]
			txB := txs[j]

			// Both must target the same contract.
			if txA.To() == nil || txB.To() == nil {
				continue
			}
			if *txA.To() != *txB.To() {
				continue
			}

			// Must be from different senders.
			senderA := txA.Sender()
			senderB := txB.Sender()
			if senderA != nil && senderB != nil && *senderA == *senderB {
				continue
			}

			priceA := txGasPrice(txA)
			priceB := txGasPrice(txB)

			// Avoid division by zero.
			if priceB.Sign() == 0 {
				continue
			}

			ratio := new(big.Int).Div(priceA, priceB)
			if ratio.Uint64() >= maxRatio {
				candidates = append(candidates, FrontrunCandidate{
					Frontrunner: txA,
					Victim:      txB,
					GasRatio:    ratio.Uint64(),
				})
			}
		}
	}

	return candidates
}

// FairOrderingEntry represents a transaction with its arrival time for fair ordering.
type FairOrderingEntry struct {
	Transaction *types.Transaction
	ArrivalTime uint64 // unix timestamp of when the tx was first seen
}

// FairOrdering enforces fair ordering rules. It sorts transactions primarily
// by arrival time and checks that no transaction is delayed more than maxDelay
// positions from its arrival-time position. Returns the fairly ordered entries
// and any violations found.
func FairOrdering(entries []FairOrderingEntry, maxDelay int) ([]FairOrderingEntry, []error) {
	if len(entries) == 0 {
		return nil, nil
	}

	// Sort by arrival time (fair order).
	sorted := make([]FairOrderingEntry, len(entries))
	copy(sorted, entries)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].ArrivalTime < sorted[j].ArrivalTime
	})

	// Check if any transaction in the original ordering is delayed too far.
	var violations []error
	for origPos, entry := range entries {
		// Find this entry's position in the fair order.
		fairPos := -1
		for i, s := range sorted {
			if s.Transaction == entry.Transaction {
				fairPos = i
				break
			}
		}
		if fairPos < 0 {
			continue
		}

		delay := origPos - fairPos
		if delay < 0 {
			delay = -delay
		}
		if delay > maxDelay {
			violations = append(violations, ErrInvalidFairOrder)
		}
	}

	return sorted, violations
}

// IdentifyBackrunOpportunities identifies benign backrun opportunities.
// A backrun is when a transaction follows another to the same contract,
// from a different sender, with a lower or equal gas price. Unlike
// frontrunning, backruns don't harm the original transaction.
func IdentifyBackrunOpportunities(txs []*types.Transaction) []BackrunOpportunity {
	if len(txs) < 2 {
		return nil
	}

	var opportunities []BackrunOpportunity

	for i := 0; i < len(txs)-1; i++ {
		for j := i + 1; j < len(txs) && j <= i+2; j++ {
			trigger := txs[i]
			backrun := txs[j]

			// Both must target the same contract.
			if trigger.To() == nil || backrun.To() == nil {
				continue
			}
			if *trigger.To() != *backrun.To() {
				continue
			}

			// Must be from different senders.
			senderT := trigger.Sender()
			senderB := backrun.Sender()
			if senderT != nil && senderB != nil && *senderT == *senderB {
				continue
			}

			// Backrun should have lower or equal gas price.
			priceT := txGasPrice(trigger)
			priceB := txGasPrice(backrun)
			if priceB.Cmp(priceT) <= 0 {
				opportunities = append(opportunities, BackrunOpportunity{
					TriggerTx:     trigger,
					BackrunTx:     backrun,
					TargetAddress: *trigger.To(),
				})
			}
		}
	}

	return opportunities
}

// txGasPrice returns the effective gas price of a transaction for comparison.
func txGasPrice(tx *types.Transaction) *big.Int {
	if tx == nil {
		return big.NewInt(0)
	}
	// Use gas tip cap for EIP-1559 txs, gas price for legacy.
	tip := tx.GasTipCap()
	if tip != nil && tip.Sign() > 0 {
		return tip
	}
	price := tx.GasPrice()
	if price != nil {
		return price
	}
	return big.NewInt(0)
}
