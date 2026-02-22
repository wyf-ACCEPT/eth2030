package consensus

// Modernized Beacon State provides an improved beacon state management
// layer with cleaner validator lifecycle, committee computation, and
// justification/finalization tracking. Part of the Consensus Layer
// accessibility roadmap.

import (
	"encoding/binary"
	"errors"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Modern beacon errors.
var (
	ErrModernSlotRegression    = errors.New("modern_beacon: slot must advance")
	ErrModernEpochRegression   = errors.New("modern_beacon: epoch must advance")
	ErrModernValidatorNotFound = errors.New("modern_beacon: validator not found")
	ErrModernValidatorExists   = errors.New("modern_beacon: validator index already set")
	ErrModernMaxValidators     = errors.New("modern_beacon: max validators reached")
	ErrModernInvalidEpoch      = errors.New("modern_beacon: invalid epoch")
	ErrModernNoValidators      = errors.New("modern_beacon: no active validators for committees")
)

// ModernBeaconConfig holds configuration for the modernized beacon state.
type ModernBeaconConfig struct {
	SlotsPerEpoch       uint64 // slots per epoch (default 32)
	MaxValidators       uint64 // max validators in registry (default 1<<20)
	MaxEffectiveBalance uint64 // max effective balance in Gwei (default 2048 ETH)
	EjectionBalance     uint64 // balance below which validator is ejected (default 16 ETH)
}

// DefaultModernBeaconConfig returns sensible defaults for the modern beacon.
func DefaultModernBeaconConfig() *ModernBeaconConfig {
	return &ModernBeaconConfig{
		SlotsPerEpoch:       32,
		MaxValidators:       1 << 20, // ~1M validators
		MaxEffectiveBalance: 2048 * GweiPerETH,
		EjectionBalance:     16 * GweiPerETH,
	}
}

// ModernValidator holds validator state for the modernized beacon.
type ModernValidator struct {
	Index                 uint64
	Balance               uint64
	EffectiveBalance      uint64
	Slashed               bool
	ActivationEpoch       uint64
	ExitEpoch             uint64
	WithdrawalCredentials []byte
}

// isActive returns true if the validator is active at the given epoch.
func (v *ModernValidator) isActive(epoch uint64) bool {
	return v.ActivationEpoch <= epoch && epoch < v.ExitEpoch
}

// ModernBeaconState is the modernized beacon state with improved state
// management, committee computation, and finality tracking. Thread-safe.
type ModernBeaconState struct {
	mu sync.RWMutex

	// Current slot and epoch.
	slot       uint64
	epoch      uint64
	lastEpoch  uint64

	// Validator registry, indexed by validator index.
	validators map[uint64]*ModernValidator
	nextIndex  uint64

	// Justification and finalization.
	justifiedCheckpoint Checkpoint
	finalizedCheckpoint Checkpoint

	// State root cache: epoch -> root for deterministic computation.
	stateRoots map[uint64]types.Hash

	config *ModernBeaconConfig
}

// NewModernBeaconState creates a new modernized beacon state at genesis.
func NewModernBeaconState(config ModernBeaconConfig) *ModernBeaconState {
	cfg := config
	if cfg.SlotsPerEpoch == 0 {
		cfg.SlotsPerEpoch = 32
	}
	if cfg.MaxValidators == 0 {
		cfg.MaxValidators = 1 << 20
	}
	if cfg.MaxEffectiveBalance == 0 {
		cfg.MaxEffectiveBalance = 2048 * GweiPerETH
	}
	if cfg.EjectionBalance == 0 {
		cfg.EjectionBalance = 16 * GweiPerETH
	}
	return &ModernBeaconState{
		validators: make(map[uint64]*ModernValidator),
		stateRoots: make(map[uint64]types.Hash),
		config:     &cfg,
	}
}

// ProcessSlot advances the state to the given slot. The slot must be
// strictly greater than the current slot. If the slot crosses an epoch
// boundary, it records the epoch transition but does not run full epoch
// processing (call ProcessEpoch for that).
func (s *ModernBeaconState) ProcessSlot(slot uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if slot <= s.slot && s.slot > 0 {
		return ErrModernSlotRegression
	}

	s.slot = slot
	s.epoch = slot / s.config.SlotsPerEpoch
	return nil
}

// ProcessEpoch processes the epoch boundary for the given epoch. It
// updates effective balances, ejects low-balance validators, and records
// the epoch in the state root cache.
func (s *ModernBeaconState) ProcessEpoch(epoch uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if epoch < s.lastEpoch && s.lastEpoch > 0 {
		return ErrModernEpochRegression
	}

	// Update effective balances and eject underfunded validators.
	for _, v := range s.validators {
		if !v.isActive(epoch) {
			continue
		}
		// Cap effective balance.
		eb := v.Balance
		if eb > s.config.MaxEffectiveBalance {
			eb = s.config.MaxEffectiveBalance
		}
		v.EffectiveBalance = eb

		// Eject validators below ejection balance.
		if v.Balance < s.config.EjectionBalance && !v.Slashed {
			v.ExitEpoch = epoch + 1
		}
	}

	// Compute and store state root for this epoch.
	root := s.computeStateRootLocked()
	s.stateRoots[epoch] = root
	s.lastEpoch = epoch

	return nil
}

// GetValidator returns a copy of the validator at the given index.
func (s *ModernBeaconState) GetValidator(index uint64) (*ModernValidator, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	v, ok := s.validators[index]
	if !ok {
		return nil, ErrModernValidatorNotFound
	}
	// Return a copy to avoid races.
	cp := *v
	if v.WithdrawalCredentials != nil {
		cp.WithdrawalCredentials = make([]byte, len(v.WithdrawalCredentials))
		copy(cp.WithdrawalCredentials, v.WithdrawalCredentials)
	}
	return &cp, nil
}

// SetValidator sets or updates the validator at the given index.
func (s *ModernBeaconState) SetValidator(index uint64, val *ModernValidator) error {
	if val == nil {
		return ErrModernValidatorNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if uint64(len(s.validators)) >= s.config.MaxValidators {
		if _, exists := s.validators[index]; !exists {
			return ErrModernMaxValidators
		}
	}

	// Store a copy.
	cp := *val
	cp.Index = index
	if val.WithdrawalCredentials != nil {
		cp.WithdrawalCredentials = make([]byte, len(val.WithdrawalCredentials))
		copy(cp.WithdrawalCredentials, val.WithdrawalCredentials)
	}
	s.validators[index] = &cp

	if index >= s.nextIndex {
		s.nextIndex = index + 1
	}

	return nil
}

// GetActiveValidators returns all validators active at the given epoch.
func (s *ModernBeaconState) GetActiveValidators(epoch uint64) []*ModernValidator {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var active []*ModernValidator
	for _, v := range s.validators {
		if v.isActive(epoch) {
			cp := *v
			active = append(active, &cp)
		}
	}
	// Sort by index for deterministic ordering.
	sort.Slice(active, func(i, j int) bool {
		return active[i].Index < active[j].Index
	})
	return active
}

// CalculateCommittees computes committees for the given epoch by
// distributing active validators evenly across the slots in the epoch.
// Returns a slice of committees, one per slot, each containing validator indices.
func (s *ModernBeaconState) CalculateCommittees(epoch uint64) ([][]uint64, error) {
	active := s.GetActiveValidators(epoch)
	if len(active) == 0 {
		return nil, ErrModernNoValidators
	}

	s.mu.RLock()
	slotsPerEpoch := s.config.SlotsPerEpoch
	s.mu.RUnlock()

	// Shuffle deterministically using epoch as seed.
	indices := make([]uint64, len(active))
	for i, v := range active {
		indices[i] = v.Index
	}
	shuffleIndices(indices, epoch)

	// Distribute across slots.
	committees := make([][]uint64, slotsPerEpoch)
	for i, idx := range indices {
		slotIdx := uint64(i) % slotsPerEpoch
		committees[slotIdx] = append(committees[slotIdx], idx)
	}

	return committees, nil
}

// shuffleIndices performs a deterministic Fisher-Yates shuffle using the
// epoch as a seed. This is a simplified version; production uses swap-or-not.
func shuffleIndices(indices []uint64, epoch uint64) {
	n := len(indices)
	if n <= 1 {
		return
	}
	// Use epoch-derived seed for deterministic shuffling.
	seed := epoch
	for i := n - 1; i > 0; i-- {
		// Simple deterministic index from seed.
		seed = seed*6364136223846793005 + 1442695040888963407
		j := int(seed>>33) % (i + 1)
		if j < 0 {
			j = -j
		}
		indices[i], indices[j] = indices[j], indices[i]
	}
}

// StateRoot computes the state root by hashing all validator states and
// the current slot/epoch information.
func (s *ModernBeaconState) StateRoot() types.Hash {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.computeStateRootLocked()
}

// computeStateRootLocked computes the state root. Caller must hold at
// least a read lock.
func (s *ModernBeaconState) computeStateRootLocked() types.Hash {
	buf := make([]byte, 0, 256)

	// Encode slot and epoch.
	var tmp [8]byte
	binary.LittleEndian.PutUint64(tmp[:], s.slot)
	buf = append(buf, tmp[:]...)
	binary.LittleEndian.PutUint64(tmp[:], s.epoch)
	buf = append(buf, tmp[:]...)

	// Encode validator count.
	binary.LittleEndian.PutUint64(tmp[:], uint64(len(s.validators)))
	buf = append(buf, tmp[:]...)

	// Encode each validator sorted by index for determinism.
	indices := make([]uint64, 0, len(s.validators))
	for idx := range s.validators {
		indices = append(indices, idx)
	}
	sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })

	for _, idx := range indices {
		v := s.validators[idx]
		binary.LittleEndian.PutUint64(tmp[:], v.Index)
		buf = append(buf, tmp[:]...)
		binary.LittleEndian.PutUint64(tmp[:], v.Balance)
		buf = append(buf, tmp[:]...)
		binary.LittleEndian.PutUint64(tmp[:], v.EffectiveBalance)
		buf = append(buf, tmp[:]...)
		if v.Slashed {
			buf = append(buf, 1)
		} else {
			buf = append(buf, 0)
		}
		binary.LittleEndian.PutUint64(tmp[:], v.ActivationEpoch)
		buf = append(buf, tmp[:]...)
		binary.LittleEndian.PutUint64(tmp[:], v.ExitEpoch)
		buf = append(buf, tmp[:]...)
	}

	// Encode checkpoints.
	binary.LittleEndian.PutUint64(tmp[:], uint64(s.justifiedCheckpoint.Epoch))
	buf = append(buf, tmp[:]...)
	buf = append(buf, s.justifiedCheckpoint.Root[:]...)
	binary.LittleEndian.PutUint64(tmp[:], uint64(s.finalizedCheckpoint.Epoch))
	buf = append(buf, tmp[:]...)
	buf = append(buf, s.finalizedCheckpoint.Root[:]...)

	return crypto.Keccak256Hash(buf)
}

// CopyState returns a deep copy of the beacon state.
func (s *ModernBeaconState) CopyState() *ModernBeaconState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cp := &ModernBeaconState{
		slot:                s.slot,
		epoch:               s.epoch,
		lastEpoch:           s.lastEpoch,
		nextIndex:           s.nextIndex,
		justifiedCheckpoint: s.justifiedCheckpoint,
		finalizedCheckpoint: s.finalizedCheckpoint,
		validators:          make(map[uint64]*ModernValidator, len(s.validators)),
		stateRoots:          make(map[uint64]types.Hash, len(s.stateRoots)),
		config:              s.config, // shared config is immutable
	}

	for idx, v := range s.validators {
		vc := *v
		if v.WithdrawalCredentials != nil {
			vc.WithdrawalCredentials = make([]byte, len(v.WithdrawalCredentials))
			copy(vc.WithdrawalCredentials, v.WithdrawalCredentials)
		}
		cp.validators[idx] = &vc
	}

	for k, v := range s.stateRoots {
		cp.stateRoots[k] = v
	}

	return cp
}

// UpdateJustification sets the justified checkpoint for the given epoch.
func (s *ModernBeaconState) UpdateJustification(epoch uint64, root types.Hash) error {
	if epoch == 0 && root.IsZero() {
		return ErrModernInvalidEpoch
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.justifiedCheckpoint = Checkpoint{
		Epoch: Epoch(epoch),
		Root:  root,
	}
	return nil
}

// GetJustifiedCheckpoint returns the current justified checkpoint.
func (s *ModernBeaconState) GetJustifiedCheckpoint() *Checkpoint {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := s.justifiedCheckpoint
	return &cp
}

// GetFinalizedCheckpoint returns the current finalized checkpoint.
func (s *ModernBeaconState) GetFinalizedCheckpoint() *Checkpoint {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := s.finalizedCheckpoint
	return &cp
}

// Finalize sets the finalized checkpoint. The finalized epoch should
// not exceed the justified epoch.
func (s *ModernBeaconState) Finalize(epoch uint64, root types.Hash) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finalizedCheckpoint = Checkpoint{
		Epoch: Epoch(epoch),
		Root:  root,
	}
}
