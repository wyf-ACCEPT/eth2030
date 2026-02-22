package consensus

import (
	"errors"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Beacon state constants.
const (
	// DefaultRecentSlotCount is the number of recent slots retained for fast
	// access in the RecentState ring buffer.
	DefaultRecentSlotCount = 64

	// HistoricalRootsLimit is the maximum number of historical state roots
	// stored in the beacon state.
	HistoricalRootsLimit = 8192
)

// Beacon state errors.
var (
	ErrNilBeaconBlock      = errors.New("beacon: nil block")
	ErrSlotRegression      = errors.New("beacon: block slot must advance state")
	ErrParentRootMismatch  = errors.New("beacon: parent root does not match state")
	ErrHistoricalNotFound  = errors.New("beacon: historical state not found")
	ErrRecentSlotNotFound  = errors.New("beacon: recent slot not found")
	ErrInvalidSlotCount    = errors.New("beacon: invalid recent slot count")
	ErrValidatorIndexBound = errors.New("beacon: validator index out of bounds")
)

// FullBeaconState extends BeaconState with the complete beacon chain state
// including validators, balances, and recent/historical state tracking.
type FullBeaconState struct {
	mu sync.RWMutex

	// Core consensus fields (embedded from types.go BeaconState).
	Slot                Slot
	Epoch               Epoch
	FinalizedCheckpoint Checkpoint
	JustifiedCheckpoint Checkpoint
	PreviousJustified   Checkpoint
	JustificationBits   JustificationBits

	// State roots.
	LatestBlockRoot types.Hash
	StateRoot       types.Hash

	// Validator registry, indexed by ValidatorIndex.
	Validators []*ValidatorBalance
	Balances   []uint64 // actual balances in Gwei, parallel to Validators

	// pubkeyIndex maps pubkey -> validator index for fast lookup.
	pubkeyIndex map[[48]byte]ValidatorIndex

	// Recent state ring buffer for fast slot lookups.
	recentStates *RecentState

	// Historical state roots for archival access.
	historicalRoots *HistoricalState

	// Configuration.
	config *ConsensusConfig
}

// NewFullBeaconState creates a new beacon state at genesis with the given config.
func NewFullBeaconState(cfg *ConsensusConfig) *FullBeaconState {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &FullBeaconState{
		Validators:      make([]*ValidatorBalance, 0),
		Balances:        make([]uint64, 0),
		pubkeyIndex:     make(map[[48]byte]ValidatorIndex),
		recentStates:    NewRecentState(DefaultRecentSlotCount),
		historicalRoots: NewHistoricalState(HistoricalRootsLimit),
		config:          cfg,
	}
}

// AddValidator appends a validator to the registry and returns its index.
func (s *FullBeaconState) AddValidator(v *ValidatorBalance, balance uint64) ValidatorIndex {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := ValidatorIndex(len(s.Validators))
	s.Validators = append(s.Validators, v)
	s.Balances = append(s.Balances, balance)
	s.pubkeyIndex[v.Pubkey] = idx
	return idx
}

// GetValidatorByIndex returns the validator at the given index.
func (s *FullBeaconState) GetValidatorByIndex(idx ValidatorIndex) (*ValidatorBalance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if int(idx) >= len(s.Validators) {
		return nil, ErrValidatorIndexBound
	}
	return s.Validators[idx], nil
}

// GetValidatorByPubkey returns the validator with the given public key.
func (s *FullBeaconState) GetValidatorByPubkey(pubkey [48]byte) (*ValidatorBalance, ValidatorIndex, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	idx, ok := s.pubkeyIndex[pubkey]
	if !ok {
		return nil, 0, ErrValidatorNotFound
	}
	return s.Validators[idx], idx, nil
}

// ValidatorCount returns the total number of validators in the registry.
func (s *FullBeaconState) ValidatorCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.Validators)
}

// ActiveValidatorCount returns the number of active validators at the
// current epoch.
func (s *FullBeaconState) ActiveValidatorCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, v := range s.Validators {
		if v.IsActive(s.Epoch) {
			count++
		}
	}
	return count
}

// TotalActiveBalance returns the sum of effective balances for all active
// validators at the current epoch.
func (s *FullBeaconState) TotalActiveBalance() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var total uint64
	for _, v := range s.Validators {
		if v.IsActive(s.Epoch) {
			total += v.EffectiveBalance
		}
	}
	return total
}

// EffectiveBalanceUpdate recomputes effective balances for all validators
// based on their actual balances, applying hysteresis.
func (s *FullBeaconState) EffectiveBalanceUpdate() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, v := range s.Validators {
		if i < len(s.Balances) {
			UpdateEffectiveBalance(v, s.Balances[i])
		}
	}
}

// BeaconBlock is a minimal beacon block for state transition.
type BeaconBlock struct {
	Slot       Slot
	ParentRoot types.Hash
	StateRoot  types.Hash
	BodyRoot   types.Hash
}

// BlockRoot computes the hash tree root of a beacon block.
// In production this uses SSZ; here we use Keccak256 for simplicity.
func BlockRoot(block *BeaconBlock) types.Hash {
	data := make([]byte, 0, 8+32*3)
	// Encode slot as 8-byte little-endian.
	s := uint64(block.Slot)
	data = append(data, byte(s), byte(s>>8), byte(s>>16), byte(s>>24),
		byte(s>>32), byte(s>>40), byte(s>>48), byte(s>>56))
	data = append(data, block.ParentRoot[:]...)
	data = append(data, block.StateRoot[:]...)
	data = append(data, block.BodyRoot[:]...)
	return crypto.Keccak256Hash(data)
}

// StateTransition processes a beacon block and advances the state.
// This implements a simplified version of the beacon chain state transition:
//  1. Verify the block slot advances the state.
//  2. Verify the parent root matches the latest block root.
//  3. Update slot/epoch and store the new block root.
//  4. Record state in recent and historical caches.
func (s *FullBeaconState) StateTransition(block *BeaconBlock) error {
	if block == nil {
		return ErrNilBeaconBlock
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Block slot must be strictly greater than current state slot.
	if block.Slot <= s.Slot {
		return ErrSlotRegression
	}

	// Parent root must match the latest block root (skip at genesis).
	if s.Slot > 0 && block.ParentRoot != s.LatestBlockRoot {
		return ErrParentRootMismatch
	}

	// Process slot transitions for any skipped slots.
	newEpoch := SlotToEpoch(block.Slot, s.config.SlotsPerEpoch)

	// Update state.
	s.Slot = block.Slot
	s.Epoch = newEpoch
	blockRoot := BlockRoot(block)
	s.LatestBlockRoot = blockRoot
	s.StateRoot = block.StateRoot

	// Record in recent states.
	s.recentStates.Put(block.Slot, blockRoot)

	// Record in historical state roots.
	s.historicalRoots.Put(blockRoot, block.StateRoot)

	return nil
}

// GetRecentBlockRoot returns the block root for a recent slot.
func (s *FullBeaconState) GetRecentBlockRoot(slot Slot) (types.Hash, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.recentStates.Get(slot)
}

// GetHistoricalStateRoot returns a state root by block root from the archive.
func (s *FullBeaconState) GetHistoricalStateRoot(blockRoot types.Hash) (types.Hash, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.historicalRoots.Get(blockRoot)
}

// ToMinimalState extracts the minimal BeaconState (from types.go) from the
// full beacon state.
func (s *FullBeaconState) ToMinimalState() BeaconState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return BeaconState{
		Slot:                s.Slot,
		Epoch:               s.Epoch,
		FinalizedCheckpoint: s.FinalizedCheckpoint,
		JustifiedCheckpoint: s.JustifiedCheckpoint,
		PreviousJustified:   s.PreviousJustified,
		JustificationBits:   s.JustificationBits,
	}
}

// --- RecentState ---

// RecentState is a ring buffer of the last N slot -> block root mappings
// for fast access to recent chain history.
type RecentState struct {
	mu      sync.RWMutex
	slots   []Slot
	roots   []types.Hash
	size    int
	head    int
	count   int
}

// NewRecentState creates a new ring buffer with capacity for n slots.
func NewRecentState(n int) *RecentState {
	if n <= 0 {
		n = DefaultRecentSlotCount
	}
	return &RecentState{
		slots: make([]Slot, n),
		roots: make([]types.Hash, n),
		size:  n,
	}
}

// Put records a slot -> block root mapping.
func (rs *RecentState) Put(slot Slot, root types.Hash) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	rs.slots[rs.head] = slot
	rs.roots[rs.head] = root
	rs.head = (rs.head + 1) % rs.size
	if rs.count < rs.size {
		rs.count++
	}
}

// Get returns the block root for the given slot.
func (rs *RecentState) Get(slot Slot) (types.Hash, error) {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	for i := 0; i < rs.count; i++ {
		idx := (rs.head - 1 - i + rs.size) % rs.size
		if rs.slots[idx] == slot {
			return rs.roots[idx], nil
		}
	}
	return types.Hash{}, ErrRecentSlotNotFound
}

// Len returns the number of entries currently stored.
func (rs *RecentState) Len() int {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	return rs.count
}

// --- HistoricalState ---

// HistoricalState provides archived state root lookup by block root.
// Entries beyond the limit are evicted in FIFO order.
type HistoricalState struct {
	mu     sync.RWMutex
	index  map[types.Hash]types.Hash
	order  []types.Hash // FIFO order of block roots
	limit  int
}

// NewHistoricalState creates a historical state archive with the given limit.
func NewHistoricalState(limit int) *HistoricalState {
	if limit <= 0 {
		limit = HistoricalRootsLimit
	}
	return &HistoricalState{
		index: make(map[types.Hash]types.Hash, limit),
		order: make([]types.Hash, 0, limit),
		limit: limit,
	}
}

// Put records a block root -> state root mapping.
func (hs *HistoricalState) Put(blockRoot, stateRoot types.Hash) {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	// If already present, update in place.
	if _, ok := hs.index[blockRoot]; ok {
		hs.index[blockRoot] = stateRoot
		return
	}

	// Evict oldest if at capacity.
	if len(hs.order) >= hs.limit {
		oldest := hs.order[0]
		delete(hs.index, oldest)
		hs.order = hs.order[1:]
	}

	hs.index[blockRoot] = stateRoot
	hs.order = append(hs.order, blockRoot)
}

// Get returns the state root for a given block root.
func (hs *HistoricalState) Get(blockRoot types.Hash) (types.Hash, error) {
	hs.mu.RLock()
	defer hs.mu.RUnlock()

	stateRoot, ok := hs.index[blockRoot]
	if !ok {
		return types.Hash{}, ErrHistoricalNotFound
	}
	return stateRoot, nil
}

// Len returns the number of archived entries.
func (hs *HistoricalState) Len() int {
	hs.mu.RLock()
	defer hs.mu.RUnlock()
	return len(hs.order)
}
