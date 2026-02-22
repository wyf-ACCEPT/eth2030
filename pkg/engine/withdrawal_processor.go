// withdrawal_processor.go implements EIP-4895 withdrawal processing for the
// execution layer. This applies beacon chain validator withdrawals to EL state
// by crediting the specified amounts to withdrawal addresses.
//
// EIP-4895 (Shanghai) introduced a system-level operation where the consensus
// layer sends a list of withdrawals to the execution layer. Each withdrawal
// credits a specified amount (in Gwei) to a validator's withdrawal address.
// These operations are not transactions -- they bypass the EVM and directly
// modify account balances.
//
// The Withdrawal type and basic validation (ValidateWithdrawals, ProcessWithdrawals)
// are in types.go and payload_attributes.go respectively. This file adds the
// state-modifying withdrawal processor and related utilities.
package engine

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

// Withdrawal processing constants.
const (
	// GweiToWei is the conversion factor from Gwei to Wei (10^9).
	GweiToWei = 1_000_000_000

	// MaxWithdrawalsPerBlock is the maximum withdrawals per block (EIP-4895).
	MaxWithdrawalsPerBlock = 16

	// MaxWithdrawalAmountGwei is the maximum withdrawal amount in Gwei.
	// This is 2^64-1 (max uint64), the protocol does not impose a lower bound.
	MaxWithdrawalAmountGwei = ^uint64(0)
)

// Withdrawal processing errors.
var (
	ErrWPNilState             = errors.New("withdrawal_processor: nil state database")
	ErrWPNilWithdrawal        = errors.New("withdrawal_processor: nil withdrawal entry")
	ErrWPZeroAddress          = errors.New("withdrawal_processor: zero withdrawal address")
	ErrWPZeroAmount           = errors.New("withdrawal_processor: zero withdrawal amount")
	ErrWPTooManyWithdrawals   = errors.New("withdrawal_processor: too many withdrawals")
	ErrWPIndexNotMonotonic    = errors.New("withdrawal_processor: withdrawal indices not monotonically increasing")
	ErrWPDuplicateIndex       = errors.New("withdrawal_processor: duplicate withdrawal index")
	ErrWPOverflow             = errors.New("withdrawal_processor: withdrawal amount overflows Wei conversion")
)

// WithdrawalResult holds the result of processing a set of withdrawals.
type WithdrawalResult struct {
	// ProcessedCount is the number of withdrawals successfully processed.
	ProcessedCount int

	// TotalAmountGwei is the total amount withdrawn in Gwei.
	TotalAmountGwei uint64

	// TotalAmountWei is the total amount withdrawn in Wei.
	TotalAmountWei *big.Int

	// AffectedAddresses is the set of addresses that received withdrawals.
	AffectedAddresses map[types.Address]bool
}

// ApplyWithdrawalsToState processes a list of withdrawals by crediting each
// withdrawal amount (converted from Gwei to Wei) to the corresponding
// address in the state database. This implements the core EIP-4895 logic.
//
// Per the spec, withdrawals:
//   - Are NOT transactions (no gas, no nonce, no sender).
//   - Create accounts if they do not exist.
//   - Are applied after transaction execution but before block finalization.
//   - Cannot fail individually (each credit is unconditional).
func ApplyWithdrawalsToState(
	statedb state.StateDB,
	withdrawals []*Withdrawal,
) (*WithdrawalResult, error) {
	if statedb == nil {
		return nil, ErrWPNilState
	}

	result := &WithdrawalResult{
		TotalAmountWei:    new(big.Int),
		AffectedAddresses: make(map[types.Address]bool),
	}

	if len(withdrawals) == 0 {
		return result, nil
	}

	for i, w := range withdrawals {
		if w == nil {
			return nil, fmt.Errorf("%w at index %d", ErrWPNilWithdrawal, i)
		}

		// Convert Gwei to Wei: amount_wei = amount_gwei * 10^9.
		amountWei := GweiToWeiBig(w.Amount)

		// Credit the withdrawal address. Per EIP-4895, this creates
		// the account if it does not exist.
		statedb.AddBalance(w.Address, amountWei)

		result.ProcessedCount++
		result.TotalAmountGwei += w.Amount
		result.TotalAmountWei.Add(result.TotalAmountWei, amountWei)
		result.AffectedAddresses[w.Address] = true
	}

	return result, nil
}

// ValidateWithdrawalOrdering checks that a withdrawal list satisfies structural
// requirements:
//   - No nil entries.
//   - No zero addresses.
//   - Indices are monotonically increasing (no duplicates).
//   - List does not exceed MaxWithdrawalsPerBlock.
func ValidateWithdrawalOrdering(withdrawals []*Withdrawal) error {
	if len(withdrawals) > MaxWithdrawalsPerBlock {
		return fmt.Errorf("%w: got %d, max %d",
			ErrWPTooManyWithdrawals, len(withdrawals), MaxWithdrawalsPerBlock)
	}

	for i, w := range withdrawals {
		if w == nil {
			return fmt.Errorf("%w at index %d", ErrWPNilWithdrawal, i)
		}
		if w.Address == (types.Address{}) {
			return fmt.Errorf("%w at index %d", ErrWPZeroAddress, i)
		}
		if i > 0 {
			if w.Index <= withdrawals[i-1].Index {
				if w.Index == withdrawals[i-1].Index {
					return fmt.Errorf("%w: index %d at position %d",
						ErrWPDuplicateIndex, w.Index, i)
				}
				return fmt.Errorf("%w: index %d at position %d <= index %d at position %d",
					ErrWPIndexNotMonotonic, w.Index, i, withdrawals[i-1].Index, i-1)
			}
		}
	}
	return nil
}

// ValidateWithdrawalAmounts checks that all withdrawal amounts are non-zero.
// Zero-amount withdrawals are considered invalid as they waste block space.
func ValidateWithdrawalAmounts(withdrawals []*Withdrawal) error {
	for i, w := range withdrawals {
		if w == nil {
			return fmt.Errorf("%w at index %d", ErrWPNilWithdrawal, i)
		}
		if w.Amount == 0 {
			return fmt.Errorf("%w at index %d (validator %d)",
				ErrWPZeroAmount, i, w.ValidatorIndex)
		}
	}
	return nil
}

// GweiToWeiBig converts a Gwei amount to Wei as a *big.Int.
// amount_wei = amount_gwei * 10^9
func GweiToWeiBig(gwei uint64) *big.Int {
	return new(big.Int).Mul(
		new(big.Int).SetUint64(gwei),
		new(big.Int).SetUint64(GweiToWei),
	)
}

// WeiToGwei converts a Wei amount to Gwei. Returns the Gwei value and any
// remainder (wei mod 10^9). If the remainder is non-zero, the conversion
// is lossy.
func WeiToGwei(wei *big.Int) (uint64, uint64) {
	if wei == nil || wei.Sign() <= 0 {
		return 0, 0
	}
	gweiDiv := new(big.Int).SetUint64(GweiToWei)
	gwei := new(big.Int).Div(wei, gweiDiv)
	remainder := new(big.Int).Mod(wei, gweiDiv)
	return gwei.Uint64(), remainder.Uint64()
}

// SumWithdrawalAmounts returns the total Gwei amount across all withdrawals.
func SumWithdrawalAmounts(withdrawals []*Withdrawal) uint64 {
	var total uint64
	for _, w := range withdrawals {
		if w != nil {
			total += w.Amount
		}
	}
	return total
}

// GroupWithdrawalsByAddress groups withdrawals by their target address.
// Returns a map from address to the list of withdrawals for that address.
func GroupWithdrawalsByAddress(withdrawals []*Withdrawal) map[types.Address][]*Withdrawal {
	groups := make(map[types.Address][]*Withdrawal)
	for _, w := range withdrawals {
		if w != nil {
			groups[w.Address] = append(groups[w.Address], w)
		}
	}
	return groups
}

// GroupWithdrawalsByValidator groups withdrawals by validator index.
func GroupWithdrawalsByValidator(withdrawals []*Withdrawal) map[uint64][]*Withdrawal {
	groups := make(map[uint64][]*Withdrawal)
	for _, w := range withdrawals {
		if w != nil {
			groups[w.ValidatorIndex] = append(groups[w.ValidatorIndex], w)
		}
	}
	return groups
}

// NextWithdrawalIndex returns the next expected withdrawal index given the
// current list. If the list is empty, returns startIndex.
func NextWithdrawalIndex(withdrawals []*Withdrawal, startIndex uint64) uint64 {
	if len(withdrawals) == 0 {
		return startIndex
	}
	last := withdrawals[len(withdrawals)-1]
	if last == nil {
		return startIndex
	}
	return last.Index + 1
}

// EngineToTypesWithdrawals converts engine Withdrawal entries to core types.
// This is a convenience wrapper around the existing WithdrawalsToCore in
// conversion.go, but accepts a slice (not pointer slice) and returns a
// newly allocated slice.
func EngineToTypesWithdrawals(ws []*Withdrawal) []*types.Withdrawal {
	return WithdrawalsToCore(ws)
}
