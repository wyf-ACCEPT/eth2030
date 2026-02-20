// compliance_engine.go implements a FOCIL compliance enforcement engine that
// tracks inclusion list compliance per block, maintains per-builder compliance
// scores, and applies penalties for non-compliance.
//
// The engine evaluates each block against the active inclusion lists for its
// slot, computing an inclusion rate. Builders that consistently fail to
// include mandated transactions receive score deductions and may trigger
// protocol-level penalties.
package focil

import (
	"errors"
	"fmt"
	"math"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// Compliance engine errors.
var (
	ErrComplianceNilBlock      = errors.New("compliance: nil block")
	ErrComplianceNoLists       = errors.New("compliance: no inclusion lists for slot")
	ErrComplianceUnknownBuilder = errors.New("compliance: unknown builder")
	ErrComplianceZeroSlot      = errors.New("compliance: slot must be > 0")
	ErrComplianceDuplicateEval = errors.New("compliance: block already evaluated for slot")
)

// BlockCompliance records the compliance evaluation result for a single block.
type BlockCompliance struct {
	// Slot is the slot the block was produced for.
	Slot uint64

	// BlockHash is the hash of the evaluated block.
	BlockHash types.Hash

	// BuilderAddr identifies the block builder.
	BuilderAddr types.Address

	// TotalRequired is the total number of unique transactions required
	// across all inclusion lists for this slot.
	TotalRequired int

	// TotalIncluded is how many required transactions were actually included.
	TotalIncluded int

	// MissingTxs lists transaction hashes that were required but not included.
	MissingTxs []types.Hash

	// ComplianceRate is TotalIncluded / TotalRequired (0.0 to 1.0).
	ComplianceRate float64

	// Compliant is true if the compliance rate meets the threshold.
	Compliant bool
}

// ComplianceScore tracks a builder's cumulative compliance reputation.
type ComplianceScore struct {
	// BuilderAddr is the builder's address.
	BuilderAddr types.Address

	// Score is the current compliance score (0 to 100).
	Score float64

	// TotalEvaluations is how many blocks have been evaluated.
	TotalEvaluations uint64

	// CompliantCount is how many blocks met the compliance threshold.
	CompliantCount uint64

	// ViolationCount is how many blocks failed compliance.
	ViolationCount uint64

	// ConsecutiveViolations tracks the current streak of violations.
	ConsecutiveViolations uint64

	// TotalPenalties is the cumulative penalty points applied.
	TotalPenalties float64
}

// ComplianceEngineConfig configures the compliance engine.
type ComplianceEngineConfig struct {
	// ComplianceThreshold is the minimum inclusion rate to be considered
	// compliant (0.0 to 1.0). Default: 0.75.
	ComplianceThreshold float64

	// BasePenalty is the base penalty points for a single violation.
	BasePenalty float64

	// EscalationFactor multiplies the penalty for consecutive violations.
	// penalty = BasePenalty * EscalationFactor^(consecutiveViolations - 1)
	EscalationFactor float64

	// RecoveryRate is the score points recovered per compliant block.
	RecoveryRate float64

	// MaxScore is the maximum compliance score. Default: 100.
	MaxScore float64

	// InitialScore is the score assigned to new builders. Default: 100.
	InitialScore float64
}

// DefaultComplianceEngineConfig returns production defaults.
func DefaultComplianceEngineConfig() ComplianceEngineConfig {
	return ComplianceEngineConfig{
		ComplianceThreshold: 0.75,
		BasePenalty:         5.0,
		EscalationFactor:    1.5,
		RecoveryRate:        2.0,
		MaxScore:            100.0,
		InitialScore:        100.0,
	}
}

// ComplianceEngine is the FOCIL compliance enforcement engine. It evaluates
// blocks against inclusion lists, scores builders, and applies penalties.
//
// All public methods are safe for concurrent use.
type ComplianceEngine struct {
	mu sync.RWMutex

	config ComplianceEngineConfig

	// Per-builder compliance scores.
	scores map[types.Address]*ComplianceScore

	// Per-slot block compliance records.
	evaluations map[uint64]*BlockCompliance

	// Inclusion lists tracked per slot.
	inclusionLists map[uint64][]*InclusionList
}

// NewComplianceEngine creates a new compliance engine.
func NewComplianceEngine(cfg ComplianceEngineConfig) *ComplianceEngine {
	if cfg.ComplianceThreshold <= 0 || cfg.ComplianceThreshold > 1 {
		cfg.ComplianceThreshold = 0.75
	}
	if cfg.BasePenalty <= 0 {
		cfg.BasePenalty = 5.0
	}
	if cfg.EscalationFactor <= 0 {
		cfg.EscalationFactor = 1.5
	}
	if cfg.RecoveryRate <= 0 {
		cfg.RecoveryRate = 2.0
	}
	if cfg.MaxScore <= 0 {
		cfg.MaxScore = 100.0
	}
	if cfg.InitialScore <= 0 {
		cfg.InitialScore = 100.0
	}
	return &ComplianceEngine{
		config:         cfg,
		scores:         make(map[types.Address]*ComplianceScore),
		evaluations:    make(map[uint64]*BlockCompliance),
		inclusionLists: make(map[uint64][]*InclusionList),
	}
}

// AddInclusionList registers an inclusion list for a slot. The engine
// accumulates all lists for a slot, then evaluates compliance against the
// union of required transactions.
func (ce *ComplianceEngine) AddInclusionList(il *InclusionList) error {
	if il == nil {
		return ErrComplianceNilBlock
	}
	if il.Slot == 0 {
		return ErrComplianceZeroSlot
	}

	ce.mu.Lock()
	defer ce.mu.Unlock()
	ce.inclusionLists[il.Slot] = append(ce.inclusionLists[il.Slot], il)
	return nil
}

// EvaluateBlock evaluates a block's compliance with the inclusion lists
// for its slot. It computes the inclusion rate, records the result, and
// updates the builder's score.
func (ce *ComplianceEngine) EvaluateBlock(block *types.Block, builderAddr types.Address) (*BlockCompliance, error) {
	if block == nil {
		return nil, ErrComplianceNilBlock
	}

	slot := block.NumberU64()
	if slot == 0 {
		return nil, ErrComplianceZeroSlot
	}

	ce.mu.Lock()
	defer ce.mu.Unlock()

	// Check for duplicate evaluation.
	if _, exists := ce.evaluations[slot]; exists {
		return nil, fmt.Errorf("%w: slot %d", ErrComplianceDuplicateEval, slot)
	}

	ils, ok := ce.inclusionLists[slot]
	if !ok || len(ils) == 0 {
		// No inclusion lists for this slot: vacuously compliant.
		result := &BlockCompliance{
			Slot:           slot,
			BlockHash:      block.Hash(),
			BuilderAddr:    builderAddr,
			ComplianceRate: 1.0,
			Compliant:      true,
		}
		ce.evaluations[slot] = result
		ce.updateScore(builderAddr, result)
		return result, nil
	}

	// Build set of block transaction hashes.
	blockTxs := make(map[types.Hash]bool)
	for _, tx := range block.Transactions() {
		blockTxs[tx.Hash()] = true
	}

	// Collect unique required transaction hashes from all ILs.
	requiredTxs := make(map[types.Hash]bool)
	for _, il := range ils {
		for _, entry := range il.Entries {
			tx, err := types.DecodeTxRLP(entry.Transaction)
			if err != nil {
				continue // skip invalid entries per spec
			}
			requiredTxs[tx.Hash()] = true
		}
	}

	totalRequired := len(requiredTxs)
	included := 0
	var missing []types.Hash

	for txHash := range requiredTxs {
		if blockTxs[txHash] {
			included++
		} else {
			missing = append(missing, txHash)
		}
	}

	rate := 1.0
	if totalRequired > 0 {
		rate = float64(included) / float64(totalRequired)
	}
	compliant := rate >= ce.config.ComplianceThreshold

	result := &BlockCompliance{
		Slot:           slot,
		BlockHash:      block.Hash(),
		BuilderAddr:    builderAddr,
		TotalRequired:  totalRequired,
		TotalIncluded:  included,
		MissingTxs:     missing,
		ComplianceRate: rate,
		Compliant:      compliant,
	}

	ce.evaluations[slot] = result
	ce.updateScore(builderAddr, result)

	return result, nil
}

// updateScore updates the builder's compliance score based on the evaluation
// result. Must be called with the write lock held.
func (ce *ComplianceEngine) updateScore(addr types.Address, result *BlockCompliance) {
	score, exists := ce.scores[addr]
	if !exists {
		score = &ComplianceScore{
			BuilderAddr: addr,
			Score:       ce.config.InitialScore,
		}
		ce.scores[addr] = score
	}

	score.TotalEvaluations++

	if result.Compliant {
		score.CompliantCount++
		score.ConsecutiveViolations = 0
		score.Score += ce.config.RecoveryRate
	} else {
		score.ViolationCount++
		score.ConsecutiveViolations++
		penalty := ce.computePenalty(score.ConsecutiveViolations)
		score.Score -= penalty
		score.TotalPenalties += penalty
	}

	// Clamp score.
	if score.Score > ce.config.MaxScore {
		score.Score = ce.config.MaxScore
	}
	if score.Score < 0 {
		score.Score = 0
	}
}

// computePenalty calculates the penalty for a violation based on the
// current consecutive violation count.
func (ce *ComplianceEngine) computePenalty(consecutive uint64) float64 {
	if consecutive == 0 {
		return 0
	}
	return ce.config.BasePenalty * math.Pow(ce.config.EscalationFactor, float64(consecutive-1))
}

// GetBuilderScore returns the current compliance score for a builder.
func (ce *ComplianceEngine) GetBuilderScore(addr types.Address) (*ComplianceScore, error) {
	ce.mu.RLock()
	defer ce.mu.RUnlock()

	score, ok := ce.scores[addr]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrComplianceUnknownBuilder, addr.Hex())
	}
	cp := *score
	return &cp, nil
}

// ApplyPenalty manually applies an additional penalty to a builder's score.
// This can be used for protocol-level sanctions beyond automatic scoring.
func (ce *ComplianceEngine) ApplyPenalty(addr types.Address, amount float64) error {
	if amount <= 0 {
		return nil
	}

	ce.mu.Lock()
	defer ce.mu.Unlock()

	score, ok := ce.scores[addr]
	if !ok {
		return fmt.Errorf("%w: %s", ErrComplianceUnknownBuilder, addr.Hex())
	}

	score.Score -= amount
	score.TotalPenalties += amount
	if score.Score < 0 {
		score.Score = 0
	}
	return nil
}

// GetBlockCompliance returns the compliance evaluation for a given slot.
func (ce *ComplianceEngine) GetBlockCompliance(slot uint64) (*BlockCompliance, error) {
	ce.mu.RLock()
	defer ce.mu.RUnlock()

	eval, ok := ce.evaluations[slot]
	if !ok {
		return nil, fmt.Errorf("compliance: no evaluation for slot %d", slot)
	}
	cp := *eval
	return &cp, nil
}

// EvaluationCount returns the number of blocks that have been evaluated.
func (ce *ComplianceEngine) EvaluationCount() int {
	ce.mu.RLock()
	defer ce.mu.RUnlock()
	return len(ce.evaluations)
}

// BuilderCount returns the number of builders with tracked scores.
func (ce *ComplianceEngine) BuilderCount() int {
	ce.mu.RLock()
	defer ce.mu.RUnlock()
	return len(ce.scores)
}

// RegisterBuilder initializes a builder's compliance score without requiring
// a block evaluation.
func (ce *ComplianceEngine) RegisterBuilder(addr types.Address) {
	ce.mu.Lock()
	defer ce.mu.Unlock()

	if _, exists := ce.scores[addr]; !exists {
		ce.scores[addr] = &ComplianceScore{
			BuilderAddr: addr,
			Score:       ce.config.InitialScore,
		}
	}
}

// PruneBefore removes evaluation records and inclusion lists for slots
// before the given slot. Returns the number of evaluations removed.
func (ce *ComplianceEngine) PruneBefore(slot uint64) int {
	ce.mu.Lock()
	defer ce.mu.Unlock()

	pruned := 0
	for s := range ce.evaluations {
		if s < slot {
			delete(ce.evaluations, s)
			pruned++
		}
	}
	for s := range ce.inclusionLists {
		if s < slot {
			delete(ce.inclusionLists, s)
		}
	}
	return pruned
}

// InclusionListCount returns the number of inclusion lists tracked for
// a given slot.
func (ce *ComplianceEngine) InclusionListCount(slot uint64) int {
	ce.mu.RLock()
	defer ce.mu.RUnlock()
	return len(ce.inclusionLists[slot])
}
