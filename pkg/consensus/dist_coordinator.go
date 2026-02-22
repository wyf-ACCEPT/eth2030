// Distributed block building coordinator for the CL.
//
// The DistCoordinator manages the full lifecycle of distributed block
// building: builder registration, coordination rounds, fragment collection,
// conflict detection, scoring, and assembly. It builds on top of the
// fragment merging primitives in dist_builder.go with network-level
// coordination semantics (round management, deadlines, builder reputation).
package consensus

import (
	"errors"
	"math/big"
	"sort"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// Coordinator errors.
var (
	ErrCoordNoActiveRound    = errors.New("coordinator: no active round")
	ErrCoordRoundActive      = errors.New("coordinator: round already active")
	ErrCoordRoundFinalized   = errors.New("coordinator: round already finalized")
	ErrCoordBuilderNotFound  = errors.New("coordinator: builder not registered")
	ErrCoordBuilderExists    = errors.New("coordinator: builder already registered")
	ErrCoordMaxBuilders      = errors.New("coordinator: max builders reached")
	ErrCoordFragmentLimit    = errors.New("coordinator: builder fragment limit exceeded")
	ErrCoordGasConflict      = errors.New("coordinator: fragment gas exceeds round limit")
	ErrCoordDeadlinePassed   = errors.New("coordinator: submission deadline passed")
	ErrCoordNilFragment      = errors.New("coordinator: nil fragment")
	ErrCoordEmptyFragment    = errors.New("coordinator: fragment has no transactions")
	ErrCoordNoFragments      = errors.New("coordinator: no fragments collected")
	ErrCoordInvalidSlot      = errors.New("coordinator: invalid slot")
	ErrCoordSlotMismatch     = errors.New("coordinator: fragment slot does not match round")
	ErrCoordInvalidPubkey    = errors.New("coordinator: invalid builder pubkey")
	ErrCoordInsufficientStake = errors.New("coordinator: insufficient builder stake")
)

// BuilderCapability describes what a builder can contribute.
type BuilderCapability uint8

const (
	CapTransactions BuilderCapability = 1 << iota
	CapBlobs
	CapBundles
)

// BuilderRegistration holds a registered builder's metadata.
type BuilderRegistration struct {
	ID            string
	Pubkey        [48]byte // BLS public key
	Stake         *big.Int // staked ETH in wei
	Capabilities  BuilderCapability
	MaxFragments  int // max fragments this builder may submit per round
	RegisteredAt  time.Time
	Reputation    float64 // 0.0-1.0, higher is better
	TotalRevenue  *big.Int
	RoundsJoined  uint64
}

// CoordinationRound tracks state for a single slot's coordination.
type CoordinationRound struct {
	Slot       Slot
	Deadline   time.Time
	Builders   map[string]*BuilderRegistration // registered builders in this round
	Fragments  []*ScoredFragment               // collected fragments
	Assembled  *AssembledBlock                  // result after assembly
	Finalized  bool
	StartedAt  time.Time
	fragCounts map[string]int // builderID -> fragments submitted
}

// ScoredFragment extends BlockFragment with scoring metadata.
type ScoredFragment struct {
	Fragment    *BlockFragment
	BuilderID   string
	GasRevenue  *big.Int // estimated revenue from this fragment
	Score       float64  // combined score (revenue + reputation)
	SubmittedAt time.Time
}

// AssembledBlock is the result of assembling scored fragments.
type AssembledBlock struct {
	Slot        Slot
	Fragments   []*ScoredFragment
	TotalGas    uint64
	TotalTxs    int
	TotalScore  float64
	BuilderIDs  []string
	AssembledAt time.Time
	BlockHash   types.Hash
}

// CoordinatorConfig configures the distributed coordinator.
type CoordinatorConfig struct {
	MaxBuilders      int
	MaxFragments     int // total fragments per round
	GasLimit         uint64
	RoundTimeout     time.Duration
	MinStake         *big.Int // minimum builder stake
	DefaultReputation float64
}

// DefaultCoordinatorConfig returns sensible defaults.
func DefaultCoordinatorConfig() *CoordinatorConfig {
	return &CoordinatorConfig{
		MaxBuilders:       32,
		MaxFragments:      128,
		GasLimit:          30_000_000,
		RoundTimeout:      4 * time.Second,
		MinStake:          new(big.Int).Mul(big.NewInt(32), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)), // 32 ETH
		DefaultReputation: 0.5,
	}
}

// DistCoordinator manages builder registration, round lifecycle,
// fragment collection, scoring, and block assembly. Thread-safe.
type DistCoordinator struct {
	mu       sync.RWMutex
	config   *CoordinatorConfig
	builders map[string]*BuilderRegistration // global registry
	round    *CoordinationRound              // current active round (nil if none)
	history  []*AssembledBlock               // past assembled blocks
	maxHist  int
}

// NewDistCoordinator creates a new coordinator with the given config.
func NewDistCoordinator(cfg *CoordinatorConfig) *DistCoordinator {
	if cfg == nil {
		cfg = DefaultCoordinatorConfig()
	}
	return &DistCoordinator{
		config:   cfg,
		builders: make(map[string]*BuilderRegistration),
		maxHist:  64,
	}
}

// RegisterBuilder adds a builder to the global registry.
func (dc *DistCoordinator) RegisterBuilder(reg *BuilderRegistration) error {
	if reg == nil || reg.ID == "" {
		return ErrCoordInvalidPubkey
	}
	// Validate pubkey is not all zeros.
	emptyKey := [48]byte{}
	if reg.Pubkey == emptyKey {
		return ErrCoordInvalidPubkey
	}
	if reg.Stake == nil || (dc.config.MinStake != nil && reg.Stake.Cmp(dc.config.MinStake) < 0) {
		return ErrCoordInsufficientStake
	}

	dc.mu.Lock()
	defer dc.mu.Unlock()

	if _, exists := dc.builders[reg.ID]; exists {
		return ErrCoordBuilderExists
	}
	if len(dc.builders) >= dc.config.MaxBuilders {
		return ErrCoordMaxBuilders
	}

	if reg.MaxFragments <= 0 {
		reg.MaxFragments = 4
	}
	if reg.Reputation <= 0 {
		reg.Reputation = dc.config.DefaultReputation
	}
	if reg.TotalRevenue == nil {
		reg.TotalRevenue = new(big.Int)
	}
	reg.RegisteredAt = time.Now()

	dc.builders[reg.ID] = reg
	return nil
}

// UnregisterBuilder removes a builder from the registry.
func (dc *DistCoordinator) UnregisterBuilder(builderID string) error {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	if _, exists := dc.builders[builderID]; !exists {
		return ErrCoordBuilderNotFound
	}
	delete(dc.builders, builderID)
	return nil
}

// BuilderCount returns the number of registered builders.
func (dc *DistCoordinator) BuilderCount() int {
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	return len(dc.builders)
}

// GetBuilder returns a copy of the builder registration, or nil.
func (dc *DistCoordinator) GetBuilder(builderID string) *BuilderRegistration {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	reg, exists := dc.builders[builderID]
	if !exists {
		return nil
	}
	cpy := *reg
	cpy.TotalRevenue = new(big.Int).Set(reg.TotalRevenue)
	cpy.Stake = new(big.Int).Set(reg.Stake)
	return &cpy
}

// StartRound begins a new coordination round for the given slot.
func (dc *DistCoordinator) StartRound(slot Slot) error {
	if slot == 0 {
		return ErrCoordInvalidSlot
	}

	dc.mu.Lock()
	defer dc.mu.Unlock()

	if dc.round != nil && !dc.round.Finalized {
		return ErrCoordRoundActive
	}

	now := time.Now()
	dc.round = &CoordinationRound{
		Slot:       slot,
		Deadline:   now.Add(dc.config.RoundTimeout),
		Builders:   make(map[string]*BuilderRegistration),
		Fragments:  nil,
		StartedAt:  now,
		fragCounts: make(map[string]int),
	}
	return nil
}

// HasActiveRound returns true if there is an active, non-finalized round.
func (dc *DistCoordinator) HasActiveRound() bool {
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	return dc.round != nil && !dc.round.Finalized
}

// CurrentSlot returns the slot of the active round, or 0 if none.
func (dc *DistCoordinator) CurrentSlot() Slot {
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	if dc.round == nil {
		return 0
	}
	return dc.round.Slot
}

// SubmitFragment adds a fragment from a registered builder to the current round.
func (dc *DistCoordinator) SubmitFragment(builderID string, frag *BlockFragment) error {
	if frag == nil {
		return ErrCoordNilFragment
	}
	if len(frag.TxList) == 0 {
		return ErrCoordEmptyFragment
	}

	dc.mu.Lock()
	defer dc.mu.Unlock()

	if dc.round == nil || dc.round.Finalized {
		return ErrCoordNoActiveRound
	}

	// Check builder is registered globally.
	reg, exists := dc.builders[builderID]
	if !exists {
		return ErrCoordBuilderNotFound
	}

	// Check deadline (allow override for testing via zero deadline).
	if !dc.round.Deadline.IsZero() && time.Now().After(dc.round.Deadline) {
		return ErrCoordDeadlinePassed
	}

	// Check per-builder fragment limit.
	count := dc.round.fragCounts[builderID]
	if count >= reg.MaxFragments {
		return ErrCoordFragmentLimit
	}

	// Check total fragment limit.
	if len(dc.round.Fragments) >= dc.config.MaxFragments {
		return ErrCoordFragmentLimit
	}

	// Check gas conflict: would this fragment push total beyond limit?
	var currentGas uint64
	for _, sf := range dc.round.Fragments {
		currentGas += sf.Fragment.GasUsed
	}
	if currentGas+frag.GasUsed > dc.config.GasLimit {
		return ErrCoordGasConflict
	}

	// Calculate score.
	revenue := estimateRevenue(frag)
	score := computeScore(revenue, reg.Reputation)

	scored := &ScoredFragment{
		Fragment:    frag,
		BuilderID:   builderID,
		GasRevenue:  revenue,
		Score:       score,
		SubmittedAt: time.Now(),
	}

	dc.round.Fragments = append(dc.round.Fragments, scored)
	dc.round.fragCounts[builderID] = count + 1
	dc.round.Builders[builderID] = reg

	return nil
}

// FragmentCount returns the number of fragments in the current round.
func (dc *DistCoordinator) FragmentCount() int {
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	if dc.round == nil {
		return 0
	}
	return len(dc.round.Fragments)
}

// AssembleBlock ranks and assembles fragments from the current round.
// Fragments are sorted by score descending and packed greedily.
func (dc *DistCoordinator) AssembleBlock() (*AssembledBlock, error) {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	if dc.round == nil {
		return nil, ErrCoordNoActiveRound
	}
	if dc.round.Finalized {
		return nil, ErrCoordRoundFinalized
	}
	if len(dc.round.Fragments) == 0 {
		return nil, ErrCoordNoFragments
	}

	// Sort fragments by score descending.
	sorted := make([]*ScoredFragment, len(dc.round.Fragments))
	copy(sorted, dc.round.Fragments)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Score > sorted[j].Score
	})

	// Greedy packing within gas limit.
	assembled := &AssembledBlock{
		Slot:        dc.round.Slot,
		AssembledAt: time.Now(),
	}
	gasLeft := dc.config.GasLimit
	builderSet := make(map[string]struct{})

	for _, sf := range sorted {
		if sf.Fragment.GasUsed > gasLeft {
			continue
		}
		assembled.Fragments = append(assembled.Fragments, sf)
		assembled.TotalGas += sf.Fragment.GasUsed
		assembled.TotalTxs += len(sf.Fragment.TxList)
		assembled.TotalScore += sf.Score
		gasLeft -= sf.Fragment.GasUsed

		if _, seen := builderSet[sf.BuilderID]; !seen {
			builderSet[sf.BuilderID] = struct{}{}
			assembled.BuilderIDs = append(assembled.BuilderIDs, sf.BuilderID)
		}
	}

	if len(assembled.Fragments) == 0 {
		return nil, ErrCoordNoFragments
	}

	// Compute a deterministic block hash from slot + gas + tx count.
	assembled.BlockHash = computeAssemblyHash(assembled)
	dc.round.Assembled = assembled

	return assembled, nil
}

// FinalizeRound marks the current round as finalized and archives the result.
// After finalization, no more fragments can be submitted.
func (dc *DistCoordinator) FinalizeRound() (*AssembledBlock, error) {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	if dc.round == nil {
		return nil, ErrCoordNoActiveRound
	}
	if dc.round.Finalized {
		return nil, ErrCoordRoundFinalized
	}

	// If not yet assembled, assemble now from available fragments.
	if dc.round.Assembled == nil {
		if len(dc.round.Fragments) == 0 {
			return nil, ErrCoordNoFragments
		}
		// Inline assembly (same logic as AssembleBlock but under write lock).
		sorted := make([]*ScoredFragment, len(dc.round.Fragments))
		copy(sorted, dc.round.Fragments)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Score > sorted[j].Score
		})

		assembled := &AssembledBlock{
			Slot:        dc.round.Slot,
			AssembledAt: time.Now(),
		}
		gasLeft := dc.config.GasLimit
		builderSet := make(map[string]struct{})

		for _, sf := range sorted {
			if sf.Fragment.GasUsed > gasLeft {
				continue
			}
			assembled.Fragments = append(assembled.Fragments, sf)
			assembled.TotalGas += sf.Fragment.GasUsed
			assembled.TotalTxs += len(sf.Fragment.TxList)
			assembled.TotalScore += sf.Score
			gasLeft -= sf.Fragment.GasUsed
			if _, seen := builderSet[sf.BuilderID]; !seen {
				builderSet[sf.BuilderID] = struct{}{}
				assembled.BuilderIDs = append(assembled.BuilderIDs, sf.BuilderID)
			}
		}

		if len(assembled.Fragments) == 0 {
			return nil, ErrCoordNoFragments
		}
		assembled.BlockHash = computeAssemblyHash(assembled)
		dc.round.Assembled = assembled
	}

	dc.round.Finalized = true

	// Update builder reputation and revenue.
	for _, sf := range dc.round.Assembled.Fragments {
		if reg, ok := dc.builders[sf.BuilderID]; ok {
			reg.RoundsJoined++
			reg.TotalRevenue.Add(reg.TotalRevenue, sf.GasRevenue)
			// Slightly boost reputation for successful participation, capped at 1.0.
			reg.Reputation += 0.01
			if reg.Reputation > 1.0 {
				reg.Reputation = 1.0
			}
		}
	}

	// Archive result.
	result := dc.round.Assembled
	dc.history = append(dc.history, result)
	if len(dc.history) > dc.maxHist {
		dc.history = dc.history[len(dc.history)-dc.maxHist:]
	}

	return result, nil
}

// History returns the last n assembled blocks from history.
func (dc *DistCoordinator) History(n int) []*AssembledBlock {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	if n <= 0 || len(dc.history) == 0 {
		return nil
	}
	if n > len(dc.history) {
		n = len(dc.history)
	}
	result := make([]*AssembledBlock, n)
	copy(result, dc.history[len(dc.history)-n:])
	return result
}

// IsDeadlinePassed returns true if the current round's deadline has passed.
func (dc *DistCoordinator) IsDeadlinePassed() bool {
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	if dc.round == nil {
		return false
	}
	return !dc.round.Deadline.IsZero() && time.Now().After(dc.round.Deadline)
}

// estimateRevenue estimates gas revenue from a fragment.
// Uses gasUsed * priority as a proxy (in a real system, this would use
// actual fee data from transactions).
func estimateRevenue(frag *BlockFragment) *big.Int {
	gas := new(big.Int).SetUint64(frag.GasUsed)
	prio := big.NewInt(int64(frag.Priority))
	if prio.Sign() <= 0 {
		prio = big.NewInt(1)
	}
	return gas.Mul(gas, prio)
}

// computeScore combines gas revenue and builder reputation into a score.
// Score = revenue_normalized * 0.7 + reputation * 0.3
func computeScore(revenue *big.Int, reputation float64) float64 {
	// Use revenue in Gwei for reasonable float values.
	revFloat := float64(revenue.Int64()) / 1e9
	if revFloat < 0 {
		revFloat = 0
	}
	return revFloat*0.7 + reputation*0.3
}

// computeAssemblyHash produces a deterministic hash for an assembled block.
func computeAssemblyHash(ab *AssembledBlock) types.Hash {
	var h types.Hash
	// Encode slot + gas + tx count into hash bytes for uniqueness.
	slotBytes := new(big.Int).SetUint64(uint64(ab.Slot)).Bytes()
	gasBytes := new(big.Int).SetUint64(ab.TotalGas).Bytes()
	copy(h[:], slotBytes)
	off := len(slotBytes)
	if off+len(gasBytes) <= len(h) {
		copy(h[off:], gasBytes)
	}
	h[31] = byte(ab.TotalTxs & 0xFF)
	return h
}
