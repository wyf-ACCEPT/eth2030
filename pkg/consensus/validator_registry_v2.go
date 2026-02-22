// validator_registry_v2.go implements a comprehensive validator lifecycle
// manager with activation/exit queue processing, voluntary exit handling,
// balance tracking (effective vs actual), withdrawal credential management,
// and churn limit calculation.
//
// Extends the existing validator_lifecycle.go and validator_set.go by
// providing an integrated registry that tracks validators through their
// full lifecycle with proper exit queue ordering and withdrawal processing.
package consensus

import (
	"errors"
	"sort"
	"sync"
)

// Validator registry v2 constants.
const (
	// VRMinValidatorWithdrawDelay is epochs between exit and withdrawability.
	VRMinValidatorWithdrawDelay Epoch = 256

	// VRMinSlashingPenaltyQuotient is the initial slash penalty divisor.
	VRMinSlashingPenaltyQuotient uint64 = 128

	// VRMaxVoluntaryExitEpoch is the maximum future epoch for exit requests.
	VRMaxVoluntaryExitEpoch uint64 = 18446744073709551615

	// VRShardCommitteePeriod is the minimum epochs active before exit is
	// allowed (256 epochs ~ 27 hours).
	VRShardCommitteePeriod Epoch = 256

	// VRWithdrawalCredentialETH1 is the 0x01 ETH1 withdrawal credential prefix.
	VRWithdrawalCredentialETH1 byte = 0x01

	// VRWithdrawalCredentialCompounding is the 0x02 compounding prefix.
	VRWithdrawalCredentialCompounding byte = 0x02
)

// Validator registry v2 errors.
var (
	ErrVRNotFound        = errors.New("validator_registry_v2: validator not found")
	ErrVRDuplicatePubkey = errors.New("validator_registry_v2: duplicate pubkey")
	ErrVRNotActive       = errors.New("validator_registry_v2: validator not active")
	ErrVRAlreadyExiting  = errors.New("validator_registry_v2: already exiting")
	ErrVRAlreadySlashed  = errors.New("validator_registry_v2: already slashed")
	ErrVRInsufficientBal = errors.New("validator_registry_v2: insufficient balance")
	ErrVRTooEarlyExit    = errors.New("validator_registry_v2: has not been active long enough")
	ErrVRIndexOutOfRange = errors.New("validator_registry_v2: index out of range")
	ErrVRRegistryFull    = errors.New("validator_registry_v2: registry full")
)

// WithdrawalCredentialType identifies the credential type.
type WithdrawalCredentialType byte

const (
	CredentialBLS         WithdrawalCredentialType = 0x00
	CredentialETH1        WithdrawalCredentialType = 0x01
	CredentialCompounding WithdrawalCredentialType = 0x02
)

// ValidatorRecordV2 holds the complete validator state for the registry.
type ValidatorRecordV2 struct {
	Index                 ValidatorIndex
	Pubkey                [48]byte
	WithdrawalCredentials [32]byte
	EffectiveBalance      uint64 // in Gwei
	Balance               uint64 // actual balance in Gwei
	Slashed               bool

	ActivationEligibility Epoch
	ActivationEpoch       Epoch
	ExitEpoch             Epoch
	WithdrawableEpoch     Epoch
}

// IsActive returns true if the validator is active at the given epoch.
func (v *ValidatorRecordV2) IsActive(epoch Epoch) bool {
	return v.ActivationEpoch <= epoch && epoch < v.ExitEpoch
}

// IsSlashable returns true if the validator can be slashed at the given epoch.
func (v *ValidatorRecordV2) IsSlashable(epoch Epoch) bool {
	return !v.Slashed && v.ActivationEpoch <= epoch && epoch < v.WithdrawableEpoch
}

// IsPending returns true if the validator has not yet been activated.
func (v *ValidatorRecordV2) IsPending() bool {
	return v.ActivationEpoch == FarFutureEpoch
}

// IsExited returns true if the validator has exited.
func (v *ValidatorRecordV2) IsExited(epoch Epoch) bool {
	return v.ExitEpoch != FarFutureEpoch && epoch >= v.ExitEpoch
}

// IsWithdrawable returns true if the validator is withdrawable.
func (v *ValidatorRecordV2) IsWithdrawable(epoch Epoch) bool {
	return v.WithdrawableEpoch != FarFutureEpoch && epoch >= v.WithdrawableEpoch
}

// CredentialType returns the withdrawal credential type prefix.
func (v *ValidatorRecordV2) CredentialType() WithdrawalCredentialType {
	if len(v.WithdrawalCredentials) == 0 {
		return CredentialBLS
	}
	return WithdrawalCredentialType(v.WithdrawalCredentials[0])
}

// HasETH1Credentials returns true if the validator has 0x01 ETH1 credentials.
func (v *ValidatorRecordV2) HasETH1Credentials() bool {
	return v.CredentialType() == CredentialETH1
}

// HasCompoundingCreds returns true if the validator uses 0x02 compounding.
func (v *ValidatorRecordV2) HasCompoundingCreds() bool {
	return v.CredentialType() == CredentialCompounding
}

// ValidatorRegistryV2Config configures the v2 registry.
type ValidatorRegistryV2Config struct {
	MaxValidators    int
	SlotsPerEpoch    uint64
	ChurnQuotient    uint64
	MinPerEpochChurn uint64
	MaxSeedLookahead uint64
	MinWithdrawDelay Epoch
	ShardCommPeriod  Epoch
}

// DefaultValidatorRegistryV2Config returns mainnet defaults.
func DefaultValidatorRegistryV2Config() ValidatorRegistryV2Config {
	return ValidatorRegistryV2Config{
		MaxValidators:    1 << 22,
		SlotsPerEpoch:    32,
		ChurnQuotient:    ChurnLimitQuotient,
		MinPerEpochChurn: MinPerEpochChurnLimit,
		MaxSeedLookahead: MaxSeedLookahead,
		MinWithdrawDelay: VRMinValidatorWithdrawDelay,
		ShardCommPeriod:  VRShardCommitteePeriod,
	}
}

// ValidatorRegistryV2 manages the full validator lifecycle. Thread-safe.
type ValidatorRegistryV2 struct {
	mu         sync.RWMutex
	config     ValidatorRegistryV2Config
	validators []*ValidatorRecordV2
	byPubkey   map[[48]byte]ValidatorIndex
}

// NewValidatorRegistryV2 creates a new empty v2 registry.
func NewValidatorRegistryV2(cfg ValidatorRegistryV2Config) *ValidatorRegistryV2 {
	return &ValidatorRegistryV2{
		config:     cfg,
		validators: make([]*ValidatorRecordV2, 0),
		byPubkey:   make(map[[48]byte]ValidatorIndex),
	}
}

// RegisterValidator adds a new validator to the registry. Returns the index.
func (r *ValidatorRegistryV2) RegisterValidator(
	pubkey [48]byte,
	withdrawalCreds [32]byte,
	effectiveBalance, balance uint64,
) (ValidatorIndex, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.validators) >= r.config.MaxValidators {
		return 0, ErrVRRegistryFull
	}
	if _, exists := r.byPubkey[pubkey]; exists {
		return 0, ErrVRDuplicatePubkey
	}

	idx := ValidatorIndex(len(r.validators))
	rec := &ValidatorRecordV2{
		Index:                 idx,
		Pubkey:                pubkey,
		WithdrawalCredentials: withdrawalCreds,
		EffectiveBalance:      effectiveBalance,
		Balance:               balance,
		ActivationEligibility: FarFutureEpoch,
		ActivationEpoch:       FarFutureEpoch,
		ExitEpoch:             FarFutureEpoch,
		WithdrawableEpoch:     FarFutureEpoch,
	}
	r.validators = append(r.validators, rec)
	r.byPubkey[pubkey] = idx
	return idx, nil
}

// GetValidator returns the validator record at the given index.
func (r *ValidatorRegistryV2) GetValidator(idx ValidatorIndex) (*ValidatorRecordV2, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if int(idx) >= len(r.validators) {
		return nil, ErrVRIndexOutOfRange
	}
	cp := *r.validators[idx]
	return &cp, nil
}

// GetByPubkey looks up a validator by public key.
func (r *ValidatorRegistryV2) GetByPubkey(pubkey [48]byte) (*ValidatorRecordV2, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	idx, ok := r.byPubkey[pubkey]
	if !ok {
		return nil, ErrVRNotFound
	}
	cp := *r.validators[idx]
	return &cp, nil
}

// Size returns the total number of registered validators.
func (r *ValidatorRegistryV2) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.validators)
}

// ActiveIndices returns sorted indices of active validators at the given epoch.
func (r *ValidatorRegistryV2) ActiveIndices(epoch Epoch) []ValidatorIndex {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []ValidatorIndex
	for _, v := range r.validators {
		if v.IsActive(epoch) {
			out = append(out, v.Index)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// ActiveCount returns the number of active validators at the given epoch.
func (r *ValidatorRegistryV2) ActiveCount(epoch Epoch) int {
	return len(r.ActiveIndices(epoch))
}

// TotalEffectiveBalance returns the sum of effective balances for active
// validators at the given epoch. Returns at least EffectiveBalanceIncrement.
func (r *ValidatorRegistryV2) TotalEffectiveBalance(epoch Epoch) uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var total uint64
	for _, v := range r.validators {
		if v.IsActive(epoch) {
			total += v.EffectiveBalance
		}
	}
	if total < EffectiveBalanceIncrement {
		return EffectiveBalanceIncrement
	}
	return total
}

// ComputeChurn returns the churn limit for the given epoch.
func (r *ValidatorRegistryV2) ComputeChurn(epoch Epoch) uint64 {
	active := uint64(r.ActiveCount(epoch))
	churn := active / r.config.ChurnQuotient
	if churn < r.config.MinPerEpochChurn {
		return r.config.MinPerEpochChurn
	}
	return churn
}

// MarkEligibleForActivation sets the activation eligibility epoch for a
// validator. Must be called when the validator's effective balance reaches
// the minimum activation balance.
func (r *ValidatorRegistryV2) MarkEligibleForActivation(idx ValidatorIndex, currentEpoch Epoch) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if int(idx) >= len(r.validators) {
		return ErrVRIndexOutOfRange
	}
	v := r.validators[idx]
	if v.ActivationEligibility != FarFutureEpoch {
		return nil // already eligible
	}
	if v.EffectiveBalance < MinActivationBalance {
		return ErrVRInsufficientBal
	}
	v.ActivationEligibility = currentEpoch + 1
	return nil
}

// ProcessActivationQueueV2 activates eligible validators in order of
// eligibility epoch (then index), up to the churn limit. Returns activated.
func (r *ValidatorRegistryV2) ProcessActivationQueueV2(
	currentEpoch Epoch, finalizedEpoch Epoch,
) []ValidatorIndex {
	r.mu.Lock()
	defer r.mu.Unlock()

	churn := r.churnLocked(currentEpoch)

	type cand struct {
		idx   ValidatorIndex
		eligE Epoch
	}
	var candidates []cand
	for _, v := range r.validators {
		if v.ActivationEligibility != FarFutureEpoch &&
			v.ActivationEpoch == FarFutureEpoch &&
			v.ActivationEligibility <= finalizedEpoch &&
			!v.Slashed &&
			v.EffectiveBalance >= MinActivationBalance {
			candidates = append(candidates, cand{v.Index, v.ActivationEligibility})
		}
	}

	// Sort by eligibility epoch, then by index for fair ordering.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].eligE != candidates[j].eligE {
			return candidates[i].eligE < candidates[j].eligE
		}
		return candidates[i].idx < candidates[j].idx
	})

	activationEpoch := Epoch(uint64(currentEpoch) + 1 + r.config.MaxSeedLookahead)
	limit := int(churn)
	if limit > len(candidates) {
		limit = len(candidates)
	}

	activated := make([]ValidatorIndex, 0, limit)
	for i := 0; i < limit; i++ {
		v := r.validators[candidates[i].idx]
		v.ActivationEpoch = activationEpoch
		activated = append(activated, v.Index)
	}
	return activated
}

// InitiateVoluntaryExit processes a voluntary exit request. Validates that
// the validator is active, has been active for the shard committee period,
// and is not already exiting.
func (r *ValidatorRegistryV2) InitiateVoluntaryExit(
	idx ValidatorIndex, currentEpoch Epoch,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if int(idx) >= len(r.validators) {
		return ErrVRIndexOutOfRange
	}
	v := r.validators[idx]

	if !v.IsActive(currentEpoch) {
		return ErrVRNotActive
	}
	if v.ExitEpoch != FarFutureEpoch {
		return ErrVRAlreadyExiting
	}
	// Check shard committee period: must have been active for at least
	// SHARD_COMMITTEE_PERIOD epochs.
	if currentEpoch < v.ActivationEpoch+r.config.ShardCommPeriod {
		return ErrVRTooEarlyExit
	}

	r.initiateExitLocked(v, currentEpoch)
	return nil
}

// ProcessSlashingV2 slashes a validator: marks slashed, initiates exit,
// applies initial penalty. Returns the penalty amount.
func (r *ValidatorRegistryV2) ProcessSlashingV2(
	idx ValidatorIndex, currentEpoch Epoch,
) (uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if int(idx) >= len(r.validators) {
		return 0, ErrVRIndexOutOfRange
	}
	v := r.validators[idx]

	if v.Slashed {
		return 0, ErrVRAlreadySlashed
	}
	if !v.IsSlashable(currentEpoch) {
		return 0, ErrVRNotActive
	}

	v.Slashed = true

	// Initiate exit if not already exiting.
	if v.ExitEpoch == FarFutureEpoch {
		r.initiateExitLocked(v, currentEpoch)
	}

	// Set withdrawable epoch to max(existing, epoch + EPOCHS_PER_SLASHINGS_VECTOR).
	slashWithdrawable := Epoch(uint64(currentEpoch) + EpochsPerSlashingsVector)
	if slashWithdrawable > v.WithdrawableEpoch {
		v.WithdrawableEpoch = slashWithdrawable
	}

	// Initial penalty: effective_balance / MIN_SLASHING_PENALTY_QUOTIENT.
	penalty := v.EffectiveBalance / VRMinSlashingPenaltyQuotient
	if penalty > v.Balance {
		v.Balance = 0
	} else {
		v.Balance -= penalty
	}
	return penalty, nil
}

// UpdateEffectiveBalancesV2 updates effective balances for all validators
// using hysteresis.
func (r *ValidatorRegistryV2) UpdateEffectiveBalancesV2() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, v := range r.validators {
		v.EffectiveBalance = ComputeEffectiveBalance(v.Balance, v.EffectiveBalance)
	}
}

// UpdateBalance adjusts a validator's actual balance. Returns new balance.
func (r *ValidatorRegistryV2) UpdateBalance(idx ValidatorIndex, delta int64) (uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if int(idx) >= len(r.validators) {
		return 0, ErrVRIndexOutOfRange
	}
	v := r.validators[idx]
	if delta >= 0 {
		v.Balance += uint64(delta)
	} else {
		d := uint64(-delta)
		if d > v.Balance {
			v.Balance = 0
		} else {
			v.Balance -= d
		}
	}
	return v.Balance, nil
}

// UpdateWithdrawalCredentials updates a validator's withdrawal credentials.
// Per EIP-7251, this allows upgrading from BLS (0x00) to ETH1 (0x01)
// or compounding (0x02).
func (r *ValidatorRegistryV2) UpdateWithdrawalCredentials(
	idx ValidatorIndex, newCreds [32]byte,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if int(idx) >= len(r.validators) {
		return ErrVRIndexOutOfRange
	}
	r.validators[idx].WithdrawalCredentials = newCreds
	return nil
}

// ProcessEjections ejects active validators whose effective balance has
// dropped to or below EJECTION_BALANCE. Returns ejected indices.
func (r *ValidatorRegistryV2) ProcessEjections(currentEpoch Epoch) []ValidatorIndex {
	r.mu.Lock()
	defer r.mu.Unlock()

	var ejected []ValidatorIndex
	for _, v := range r.validators {
		if v.IsActive(currentEpoch) && v.EffectiveBalance <= EjectionBalance &&
			v.ExitEpoch == FarFutureEpoch {
			r.initiateExitLocked(v, currentEpoch)
			ejected = append(ejected, v.Index)
		}
	}
	return ejected
}

// Stats returns aggregate statistics about the validator set.
func (r *ValidatorRegistryV2) Stats(epoch Epoch) ValidatorRegistryV2Stats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var stats ValidatorRegistryV2Stats
	for _, v := range r.validators {
		stats.Total++
		if v.IsActive(epoch) {
			stats.Active++
			stats.TotalActiveBalance += v.EffectiveBalance
		} else if v.IsPending() {
			stats.Pending++
		} else if v.IsWithdrawable(epoch) {
			stats.Withdrawable++
		} else if v.IsExited(epoch) {
			stats.Exited++
		}
		if v.Slashed {
			stats.Slashed++
		}
	}
	return stats
}

// ValidatorRegistryV2Stats holds aggregate validator set statistics.
type ValidatorRegistryV2Stats struct {
	Total              int
	Active             int
	Pending            int
	Exited             int
	Withdrawable       int
	Slashed            int
	TotalActiveBalance uint64
}

// initiateExitLocked begins the exit process, computing exit epoch from the
// exit queue. Must be called with r.mu held.
func (r *ValidatorRegistryV2) initiateExitLocked(v *ValidatorRecordV2, currentEpoch Epoch) {
	exitQueueEpoch := Epoch(uint64(currentEpoch) + 1 + r.config.MaxSeedLookahead)
	exitQueueChurn := uint64(0)

	for _, ev := range r.validators {
		if ev.ExitEpoch != FarFutureEpoch {
			if ev.ExitEpoch > exitQueueEpoch {
				exitQueueEpoch = ev.ExitEpoch
				exitQueueChurn = 1
			} else if ev.ExitEpoch == exitQueueEpoch {
				exitQueueChurn++
			}
		}
	}

	churn := r.churnLocked(currentEpoch)
	if exitQueueChurn >= churn {
		exitQueueEpoch++
	}

	v.ExitEpoch = exitQueueEpoch
	v.WithdrawableEpoch = Epoch(uint64(exitQueueEpoch) + uint64(r.config.MinWithdrawDelay))
}

// churnLocked computes the churn limit. Must be called with r.mu held.
func (r *ValidatorRegistryV2) churnLocked(epoch Epoch) uint64 {
	var active uint64
	for _, v := range r.validators {
		if v.IsActive(epoch) {
			active++
		}
	}
	churn := active / r.config.ChurnQuotient
	if churn < r.config.MinPerEpochChurn {
		return r.config.MinPerEpochChurn
	}
	return churn
}
