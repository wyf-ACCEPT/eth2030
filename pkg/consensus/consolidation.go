package consensus

import (
	"errors"

	"github.com/eth2030/eth2030/core/types"
)

// EIP-7251: Validator consolidation processing.
// Consolidation allows merging two validators' balances into one,
// capped at MaxEffectiveBalance.

var (
	ErrConsolidationSameValidator       = errors.New("source and target must differ")
	ErrConsolidationSourceNotActive     = errors.New("source validator not active")
	ErrConsolidationTargetNotActive     = errors.New("target validator not active")
	ErrConsolidationSourceSlashed       = errors.New("source validator is slashed")
	ErrConsolidationTargetSlashed       = errors.New("target validator is slashed")
	ErrConsolidationCredentialsMismatch = errors.New("withdrawal credentials mismatch")
	ErrConsolidationNotCompounding      = errors.New("target must have compounding credentials")
	ErrConsolidationOverflow            = errors.New("balance overflow during consolidation")
)

// ConsolidationResult holds the outcome of processing a consolidation request.
type ConsolidationResult struct {
	SourcePubkey      [48]byte
	TargetPubkey      [48]byte
	AmountTransferred uint64 // Gwei moved from source to target
}

// ValidateConsolidation checks whether a consolidation request is valid per EIP-7251.
// Both validators must be active, not slashed, and share the same withdrawal credentials.
// The target must have compounding withdrawal credentials (0x02 prefix).
func ValidateConsolidation(
	source *ValidatorBalance,
	target *ValidatorBalance,
	currentEpoch Epoch,
) error {
	// Source and target must differ.
	if source.Pubkey == target.Pubkey {
		return ErrConsolidationSameValidator
	}

	// Both must be active.
	if !source.IsActive(currentEpoch) {
		return ErrConsolidationSourceNotActive
	}
	if !target.IsActive(currentEpoch) {
		return ErrConsolidationTargetNotActive
	}

	// Neither can be slashed.
	if source.Slashed {
		return ErrConsolidationSourceSlashed
	}
	if target.Slashed {
		return ErrConsolidationTargetSlashed
	}

	// Withdrawal credentials must match.
	if source.WithdrawalCredentials != target.WithdrawalCredentials {
		return ErrConsolidationCredentialsMismatch
	}

	// Target must have compounding credentials.
	if !target.HasCompoundingCredentials() {
		return ErrConsolidationNotCompounding
	}

	return nil
}

// ProcessConsolidation merges the source validator's effective balance into
// the target, capped at MaxEffectiveBalance. The source is marked for exit.
// Returns the amount transferred.
//
// The caller must validate the consolidation request first via ValidateConsolidation.
func ProcessConsolidation(
	source *ValidatorBalance,
	target *ValidatorBalance,
	sourceBalance uint64,
	targetBalance uint64,
	currentEpoch Epoch,
) (*ConsolidationResult, uint64, uint64, error) {
	// Amount to transfer is the source's full balance.
	amount := sourceBalance

	// Cap target at MaxEffectiveBalance.
	newTargetBalance := targetBalance + amount
	if newTargetBalance < targetBalance {
		return nil, sourceBalance, targetBalance, ErrConsolidationOverflow
	}

	// Mark source for exit. The source validator will be fully drained.
	source.ExitEpoch = currentEpoch + 1

	// Update effective balances.
	newSourceBalance := uint64(0)
	source.EffectiveBalance = 0

	// Cap target effective balance.
	targetEffective := newTargetBalance
	if targetEffective > MaxEffectiveBalance {
		targetEffective = MaxEffectiveBalance
	}
	target.EffectiveBalance = (targetEffective / EffectiveBalanceIncrement) * EffectiveBalanceIncrement

	return &ConsolidationResult{
		SourcePubkey:      source.Pubkey,
		TargetPubkey:      target.Pubkey,
		AmountTransferred: amount,
	}, newSourceBalance, newTargetBalance, nil
}

// ConsolidationRequestToEIP7685 converts a types.ConsolidationRequest into
// an EIP-7685 Request for inclusion in the block.
func ConsolidationRequestToEIP7685(req *types.ConsolidationRequest) *types.Request {
	return types.NewRequest(types.ConsolidationRequestType, req.Encode())
}

// ConsolidateValidators merges two validators in a UnifiedBeaconState per
// EIP-7251. The source validator's balance is transferred to the target,
// and the source is marked for exit. Both validators must be active, not
// slashed, and have matching compounding withdrawal credentials.
func ConsolidateValidators(state *UnifiedBeaconState, sourceIdx, targetIdx uint64, currentEpoch Epoch) (*ConsolidationResult, error) {
	if state == nil {
		return nil, ErrConsolidationSameValidator // nil state
	}
	if sourceIdx == targetIdx {
		return nil, ErrConsolidationSameValidator
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	if int(sourceIdx) >= len(state.Validators) || int(targetIdx) >= len(state.Validators) {
		return nil, ErrUnifiedIndexOutOfRange
	}

	src := state.Validators[sourceIdx]
	tgt := state.Validators[targetIdx]

	// Both must be active.
	if !src.IsActiveAt(currentEpoch) {
		return nil, ErrConsolidationSourceNotActive
	}
	if !tgt.IsActiveAt(currentEpoch) {
		return nil, ErrConsolidationTargetNotActive
	}
	// Neither can be slashed.
	if src.Slashed {
		return nil, ErrConsolidationSourceSlashed
	}
	if tgt.Slashed {
		return nil, ErrConsolidationTargetSlashed
	}
	// Withdrawal credentials must match.
	if src.WithdrawalCredentials != tgt.WithdrawalCredentials {
		return nil, ErrConsolidationCredentialsMismatch
	}
	// Target must have compounding credentials (0x02 prefix).
	if tgt.WithdrawalCredentials[0] != CompoundingWithdrawalPrefix {
		return nil, ErrConsolidationNotCompounding
	}

	// Transfer balance from source to target.
	amount := src.Balance
	tgt.Balance += amount
	src.Balance = 0
	src.EffectiveBalance = 0
	src.ExitEpoch = currentEpoch + 1

	// Cap target effective balance at MaxEffectiveBalance.
	tgtEff := tgt.Balance
	if tgtEff > MaxEffectiveBalance {
		tgtEff = MaxEffectiveBalance
	}
	tgt.EffectiveBalance = (tgtEff / EffectiveBalanceIncrement) * EffectiveBalanceIncrement

	return &ConsolidationResult{
		SourcePubkey:      src.Pubkey,
		TargetPubkey:      tgt.Pubkey,
		AmountTransferred: amount,
	}, nil
}

// EIP7685ToConsolidationRequest decodes an EIP-7685 Request into a
// types.ConsolidationRequest.
func EIP7685ToConsolidationRequest(req *types.Request) (*types.ConsolidationRequest, error) {
	if req.Type != types.ConsolidationRequestType {
		return nil, errors.New("not a consolidation request")
	}
	return types.DecodeConsolidationRequest(req.Data)
}
