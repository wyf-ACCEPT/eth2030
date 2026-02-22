// Package consensus implements Ethereum consensus-layer primitives.
// This file implements the validator lifecycle manager, tracking validators
// through pending -> active -> exiting -> exited -> withdrawable states
// per the beacon chain spec (phase0).

package consensus

import (
	"errors"
	"sort"
	"sync"
)

// Validator lifecycle constants (additional to those in beacon_state_v2.go).
const (
	// MinValidatorWithdrawabilityDelay is the minimum number of epochs
	// between a validator's exit and when it becomes withdrawable.
	MinValidatorWithdrawabilityDelay uint64 = 256

	// MinSlashingPenaltyQuotient determines the initial slashing penalty.
	MinSlashingPenaltyQuotient uint64 = 128
)

// ValidatorState represents the lifecycle state of a beacon chain validator.
type ValidatorState uint8

const (
	// StatePending: deposited but not yet eligible for activation.
	StatePending ValidatorState = iota
	// StateActive: participating in consensus duties.
	StateActive
	// StateExiting: initiated exit, waiting for exit epoch.
	StateExiting
	// StateExited: exit epoch reached, no longer attesting.
	StateExited
	// StateWithdrawable: withdrawable epoch reached, funds can be withdrawn.
	StateWithdrawable
	// StateSlashed: validator has been slashed (can overlap with exiting/exited).
	StateSlashed
)

// String returns the human-readable name of a ValidatorState.
func (s ValidatorState) String() string {
	switch s {
	case StatePending:
		return "pending"
	case StateActive:
		return "active"
	case StateExiting:
		return "exiting"
	case StateExited:
		return "exited"
	case StateWithdrawable:
		return "withdrawable"
	case StateSlashed:
		return "slashed"
	default:
		return "unknown"
	}
}

// Validator lifecycle errors.
var (
	ErrLifecycleValidatorNotFound   = errors.New("lifecycle: validator not found")
	ErrLifecycleAlreadyActive       = errors.New("lifecycle: validator already active")
	ErrLifecycleAlreadyExiting      = errors.New("lifecycle: validator already exiting or exited")
	ErrLifecycleNotActive           = errors.New("lifecycle: validator is not active")
	ErrLifecycleAlreadySlashed      = errors.New("lifecycle: validator already slashed")
	ErrLifecycleInsufficientBalance = errors.New("lifecycle: insufficient effective balance for activation")
)

// LifecycleValidator tracks a single validator through its lifecycle.
type LifecycleValidator struct {
	Index                   ValidatorIndex
	ActivationEligibleEpoch Epoch  // when eligible for the activation queue
	ActivationEpoch         Epoch  // when actually activated
	ExitEpoch               Epoch  // when exit takes effect
	WithdrawableEpoch       Epoch  // when funds become withdrawable
	EffectiveBalance        uint64 // in Gwei
	Balance                 uint64 // actual balance in Gwei
	Slashed                 bool
}

// State returns the current lifecycle state at the given epoch.
func (v *LifecycleValidator) State(epoch Epoch) ValidatorState {
	if v.Slashed && v.ExitEpoch != FarFutureEpoch {
		if epoch >= v.WithdrawableEpoch {
			return StateWithdrawable
		}
		return StateSlashed
	}
	if v.ActivationEpoch == FarFutureEpoch {
		return StatePending
	}
	if epoch < v.ActivationEpoch {
		return StatePending
	}
	if v.ExitEpoch == FarFutureEpoch {
		return StateActive
	}
	if epoch < v.ExitEpoch {
		return StateExiting
	}
	if epoch < v.WithdrawableEpoch {
		return StateExited
	}
	return StateWithdrawable
}

// IsActive returns true if the validator is active at the given epoch.
func (v *LifecycleValidator) IsActive(epoch Epoch) bool {
	return v.ActivationEpoch <= epoch && epoch < v.ExitEpoch
}

// IsSlashable returns true if the validator can be slashed at the given epoch.
// Per spec: not already slashed AND activation_epoch <= epoch < withdrawable_epoch.
func (v *LifecycleValidator) IsSlashable(epoch Epoch) bool {
	return !v.Slashed &&
		v.ActivationEpoch <= epoch &&
		epoch < v.WithdrawableEpoch
}

// ValidatorLifecycleStats holds aggregate statistics about the validator set.
type ValidatorLifecycleStats struct {
	PendingCount       int
	ActiveCount        int
	ExitingCount       int
	ExitedCount        int
	WithdrawableCount  int
	SlashedCount       int
	TotalActiveBalance uint64
}

// ValidatorLifecycle manages the full validator lifecycle for a beacon chain.
type ValidatorLifecycle struct {
	mu         sync.RWMutex
	validators map[ValidatorIndex]*LifecycleValidator
}

// NewValidatorLifecycle creates a new empty lifecycle manager.
func NewValidatorLifecycle() *ValidatorLifecycle {
	return &ValidatorLifecycle{
		validators: make(map[ValidatorIndex]*LifecycleValidator),
	}
}

// AddValidator registers a new pending validator with the given index and
// effective balance. The validator starts in StatePending.
func (vl *ValidatorLifecycle) AddValidator(index ValidatorIndex, effectiveBalance, balance uint64) {
	vl.mu.Lock()
	defer vl.mu.Unlock()
	vl.validators[index] = &LifecycleValidator{
		Index:                   index,
		ActivationEligibleEpoch: FarFutureEpoch,
		ActivationEpoch:         FarFutureEpoch,
		ExitEpoch:               FarFutureEpoch,
		WithdrawableEpoch:       FarFutureEpoch,
		EffectiveBalance:        effectiveBalance,
		Balance:                 balance,
	}
}

// GetValidator returns the lifecycle validator for the given index.
func (vl *ValidatorLifecycle) GetValidator(index ValidatorIndex) (*LifecycleValidator, error) {
	vl.mu.RLock()
	defer vl.mu.RUnlock()
	v, ok := vl.validators[index]
	if !ok {
		return nil, ErrLifecycleValidatorNotFound
	}
	return v, nil
}

// computeActivationExitEpoch returns the epoch at which activations and exits
// initiated in the given epoch take effect: epoch + 1 + MAX_SEED_LOOKAHEAD.
func computeActivationExitEpoch(epoch Epoch) Epoch {
	return Epoch(uint64(epoch) + 1 + MaxSeedLookahead)
}

// getChurnLimit returns the validator churn limit for the given number of
// active validators: max(MIN_PER_EPOCH_CHURN_LIMIT, activeCount / CHURN_LIMIT_QUOTIENT).
func getChurnLimit(activeCount int) uint64 {
	churn := uint64(activeCount) / ChurnLimitQuotient
	if churn < MinPerEpochChurnLimit {
		return MinPerEpochChurnLimit
	}
	return churn
}

// InitiateActivation marks a validator as eligible for activation and sets
// its activation_eligibility_epoch. The actual activation is deferred until
// ProcessActivationQueue is called for the appropriate epoch.
func (vl *ValidatorLifecycle) InitiateActivation(index ValidatorIndex, epoch Epoch) error {
	vl.mu.Lock()
	defer vl.mu.Unlock()

	v, ok := vl.validators[index]
	if !ok {
		return ErrLifecycleValidatorNotFound
	}
	if v.ActivationEpoch != FarFutureEpoch {
		return ErrLifecycleAlreadyActive
	}
	if v.EffectiveBalance < MinActivationBalance {
		return ErrLifecycleInsufficientBalance
	}
	// Set eligibility epoch (per spec: current_epoch + 1).
	v.ActivationEligibleEpoch = Epoch(uint64(epoch) + 1)
	return nil
}

// ProcessActivationQueue processes the activation queue for the given epoch,
// activating validators in order of activation_eligibility_epoch (then index)
// up to the churn limit. Returns the indices of validators that were activated.
func (vl *ValidatorLifecycle) ProcessActivationQueue(epoch Epoch) []ValidatorIndex {
	vl.mu.Lock()
	defer vl.mu.Unlock()

	// Count active validators for churn limit computation.
	activeCount := 0
	for _, v := range vl.validators {
		if v.IsActive(epoch) {
			activeCount++
		}
	}
	churn := getChurnLimit(activeCount)

	// Collect eligible validators: eligibility epoch set, not yet activated,
	// eligibility epoch <= epoch, not slashed.
	type candidate struct {
		index ValidatorIndex
		eligE Epoch
	}
	var candidates []candidate
	for _, v := range vl.validators {
		if v.ActivationEligibleEpoch != FarFutureEpoch &&
			v.ActivationEpoch == FarFutureEpoch &&
			v.ActivationEligibleEpoch <= epoch &&
			!v.Slashed &&
			v.EffectiveBalance >= MinActivationBalance {
			candidates = append(candidates, candidate{v.Index, v.ActivationEligibleEpoch})
		}
	}

	// Sort by eligibility epoch, then by index (tie-break).
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].eligE != candidates[j].eligE {
			return candidates[i].eligE < candidates[j].eligE
		}
		return candidates[i].index < candidates[j].index
	})

	// Activate up to churn limit.
	activationEpoch := computeActivationExitEpoch(epoch)
	limit := int(churn)
	if limit > len(candidates) {
		limit = len(candidates)
	}

	activated := make([]ValidatorIndex, 0, limit)
	for i := 0; i < limit; i++ {
		v := vl.validators[candidates[i].index]
		v.ActivationEpoch = activationEpoch
		activated = append(activated, v.Index)
	}
	return activated
}

// InitiateExit begins the exit process for a validator. Per the spec,
// the exit epoch is computed from the current exit queue to rate-limit exits.
func (vl *ValidatorLifecycle) InitiateExit(index ValidatorIndex, epoch Epoch) error {
	vl.mu.Lock()
	defer vl.mu.Unlock()

	v, ok := vl.validators[index]
	if !ok {
		return ErrLifecycleValidatorNotFound
	}
	// Already initiated exit.
	if v.ExitEpoch != FarFutureEpoch {
		return ErrLifecycleAlreadyExiting
	}
	if !v.IsActive(epoch) {
		return ErrLifecycleNotActive
	}

	// Compute exit queue epoch: max of all scheduled exit epochs and
	// compute_activation_exit_epoch(current_epoch).
	exitQueueEpoch := computeActivationExitEpoch(epoch)
	exitQueueChurn := 0
	for _, ev := range vl.validators {
		if ev.ExitEpoch != FarFutureEpoch {
			if ev.ExitEpoch > exitQueueEpoch {
				exitQueueEpoch = ev.ExitEpoch
				exitQueueChurn = 1
			} else if ev.ExitEpoch == exitQueueEpoch {
				exitQueueChurn++
			}
		}
	}

	// Count active validators for churn limit.
	activeCount := 0
	for _, av := range vl.validators {
		if av.IsActive(epoch) {
			activeCount++
		}
	}
	if uint64(exitQueueChurn) >= getChurnLimit(activeCount) {
		exitQueueEpoch = Epoch(uint64(exitQueueEpoch) + 1)
	}

	v.ExitEpoch = exitQueueEpoch
	v.WithdrawableEpoch = Epoch(uint64(v.ExitEpoch) + MinValidatorWithdrawabilityDelay)
	return nil
}

// ProcessSlashing slashes a validator: marks it slashed, initiates exit,
// and applies the initial penalty. Returns the penalty amount in Gwei.
func (vl *ValidatorLifecycle) ProcessSlashing(index ValidatorIndex, epoch Epoch) (uint64, error) {
	vl.mu.Lock()
	defer vl.mu.Unlock()

	v, ok := vl.validators[index]
	if !ok {
		return 0, ErrLifecycleValidatorNotFound
	}
	if v.Slashed {
		return 0, ErrLifecycleAlreadySlashed
	}
	if !v.IsSlashable(epoch) {
		return 0, ErrLifecycleNotActive
	}

	// Mark slashed.
	v.Slashed = true

	// Initiate exit if not already exiting. We do this inline (not via
	// InitiateExit) because we already hold the lock.
	if v.ExitEpoch == FarFutureEpoch {
		exitQueueEpoch := computeActivationExitEpoch(epoch)
		exitQueueChurn := 0
		for _, ev := range vl.validators {
			if ev.ExitEpoch != FarFutureEpoch {
				if ev.ExitEpoch > exitQueueEpoch {
					exitQueueEpoch = ev.ExitEpoch
					exitQueueChurn = 1
				} else if ev.ExitEpoch == exitQueueEpoch {
					exitQueueChurn++
				}
			}
		}
		activeCount := 0
		for _, av := range vl.validators {
			if av.IsActive(epoch) {
				activeCount++
			}
		}
		if uint64(exitQueueChurn) >= getChurnLimit(activeCount) {
			exitQueueEpoch = Epoch(uint64(exitQueueEpoch) + 1)
		}
		v.ExitEpoch = exitQueueEpoch
		v.WithdrawableEpoch = Epoch(uint64(v.ExitEpoch) + MinValidatorWithdrawabilityDelay)
	}

	// Set withdrawable epoch to max(existing, epoch + EPOCHS_PER_SLASHINGS_VECTOR).
	slashWithdrawable := Epoch(uint64(epoch) + EpochsPerSlashingsVector)
	if slashWithdrawable > v.WithdrawableEpoch {
		v.WithdrawableEpoch = slashWithdrawable
	}

	// Initial penalty: effective_balance / MIN_SLASHING_PENALTY_QUOTIENT.
	penalty := v.EffectiveBalance / MinSlashingPenaltyQuotient
	if penalty > v.Balance {
		v.Balance = 0
	} else {
		v.Balance -= penalty
	}
	return penalty, nil
}

// UpdateEffectiveBalances updates effective balances for all validators
// using hysteresis to prevent oscillation.
func (vl *ValidatorLifecycle) UpdateEffectiveBalances() {
	vl.mu.Lock()
	defer vl.mu.Unlock()
	for _, v := range vl.validators {
		v.EffectiveBalance = ComputeEffectiveBalance(v.Balance, v.EffectiveBalance)
	}
}

// ProcessEjections force-exits any active validator whose effective balance
// has dropped to or below EJECTION_BALANCE. Returns ejected indices.
func (vl *ValidatorLifecycle) ProcessEjections(epoch Epoch) []ValidatorIndex {
	vl.mu.Lock()
	defer vl.mu.Unlock()

	var ejected []ValidatorIndex
	for _, v := range vl.validators {
		if v.IsActive(epoch) && v.EffectiveBalance <= EjectionBalance {
			// Inline exit initiation.
			if v.ExitEpoch == FarFutureEpoch {
				exitQueueEpoch := computeActivationExitEpoch(epoch)
				for _, ev := range vl.validators {
					if ev.ExitEpoch != FarFutureEpoch && ev.ExitEpoch > exitQueueEpoch {
						exitQueueEpoch = ev.ExitEpoch
					}
				}
				v.ExitEpoch = exitQueueEpoch
				v.WithdrawableEpoch = Epoch(uint64(v.ExitEpoch) + MinValidatorWithdrawabilityDelay)
				ejected = append(ejected, v.Index)
			}
		}
	}
	return ejected
}

// Stats returns aggregate statistics about the validator set at the given epoch.
func (vl *ValidatorLifecycle) Stats(epoch Epoch) ValidatorLifecycleStats {
	vl.mu.RLock()
	defer vl.mu.RUnlock()

	var stats ValidatorLifecycleStats
	for _, v := range vl.validators {
		state := v.State(epoch)
		switch state {
		case StatePending:
			stats.PendingCount++
		case StateActive:
			stats.ActiveCount++
			stats.TotalActiveBalance += v.EffectiveBalance
		case StateExiting:
			stats.ExitingCount++
			stats.TotalActiveBalance += v.EffectiveBalance
		case StateExited:
			stats.ExitedCount++
		case StateWithdrawable:
			stats.WithdrawableCount++
		case StateSlashed:
			stats.SlashedCount++
		}
	}
	return stats
}

// ValidatorCount returns the total number of tracked validators.
func (vl *ValidatorLifecycle) ValidatorCount() int {
	vl.mu.RLock()
	defer vl.mu.RUnlock()
	return len(vl.validators)
}

// ActiveIndices returns the indices of all active validators at the given epoch.
func (vl *ValidatorLifecycle) ActiveIndices(epoch Epoch) []ValidatorIndex {
	vl.mu.RLock()
	defer vl.mu.RUnlock()

	var indices []ValidatorIndex
	for _, v := range vl.validators {
		if v.IsActive(epoch) {
			indices = append(indices, v.Index)
		}
	}
	sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })
	return indices
}
