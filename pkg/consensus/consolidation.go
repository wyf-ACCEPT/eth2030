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

// EIP7685ToConsolidationRequest decodes an EIP-7685 Request into a
// types.ConsolidationRequest.
func EIP7685ToConsolidationRequest(req *types.Request) (*types.ConsolidationRequest, error) {
	if req.Type != types.ConsolidationRequestType {
		return nil, errors.New("not a consolidation request")
	}
	return types.DecodeConsolidationRequest(req.Data)
}
