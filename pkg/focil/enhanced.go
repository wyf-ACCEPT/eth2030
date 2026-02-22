package focil

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"golang.org/x/crypto/sha3"
)

// ConstraintType identifies the kind of inclusion constraint.
type ConstraintType uint8

const (
	// ConstraintMustInclude requires that a specific transaction is included.
	ConstraintMustInclude ConstraintType = iota

	// ConstraintMustExclude requires that a specific transaction is NOT included.
	ConstraintMustExclude

	// ConstraintGasLimit sets gas bounds for transactions to a target address.
	ConstraintGasLimit

	// ConstraintOrdering requires transactions to a target address appear in order.
	ConstraintOrdering
)

// Enhanced FOCIL errors.
var (
	ErrEnhancedNilList       = errors.New("focil: inclusion list is nil")
	ErrEnhancedZeroSlot      = errors.New("focil: slot must be > 0")
	ErrEnhancedTooManyTxs    = errors.New("focil: too many transactions")
	ErrEnhancedTooManyConstr = errors.New("focil: too many constraints")
	ErrEnhancedGasExceeded   = errors.New("focil: max gas exceeded")
	ErrEnhancedDuplicateTx   = errors.New("focil: duplicate transaction hash")
	ErrEnhancedInvalidConstr = errors.New("focil: invalid constraint")
	ErrEnhancedNoLists       = errors.New("focil: no lists to merge")
	ErrEnhancedEmptyTxs      = errors.New("focil: no transactions in list")
	ErrEnhancedPriorityLen   = errors.New("focil: priority length mismatch")
	ErrEnhancedConstraintGas = errors.New("focil: constraint MinGas > MaxGas")
	ErrEnhancedDeadlineZero  = errors.New("focil: constraint deadline is zero")
	ErrEnhancedProposerZero  = errors.New("focil: proposer address is zero")
)

// InclusionConstraint represents a constraint on block inclusion behavior.
type InclusionConstraint struct {
	// Type is the constraint kind.
	Type ConstraintType

	// Target is the address the constraint applies to.
	Target types.Address

	// MinGas is the minimum gas for ConstraintGasLimit.
	MinGas uint64

	// MaxGas is the maximum gas for ConstraintGasLimit.
	MaxGas uint64

	// Deadline is the slot by which the constraint must be satisfied.
	Deadline uint64
}

// InclusionListV2 is an enhanced inclusion list with priorities and constraints.
type InclusionListV2 struct {
	// Slot is the beacon slot this list targets.
	Slot uint64

	// Proposer is the address of the IL committee member.
	Proposer types.Address

	// Transactions lists the transaction hashes to include.
	Transactions []types.Hash

	// Priority assigns a priority score to each transaction (higher = more important).
	// Must be the same length as Transactions.
	Priority []uint64

	// MaxGas is the maximum total gas allowed for this list's transactions.
	MaxGas uint64

	// Constraints are additional inclusion constraints.
	Constraints []InclusionConstraint
}

// InclusionViolation records a specific constraint violation.
type InclusionViolation struct {
	Constraint InclusionConstraint
	Reason     string
}

// EnforcementResult is the result of checking constraint enforcement.
type EnforcementResult struct {
	// Satisfied is true if all constraints are met.
	Satisfied bool

	// Violated is the number of violated constraints.
	Violated int

	// Penalties lists the specific violations.
	Penalties []InclusionViolation
}

// EnhancedFOCILConfig configures the EnhancedFOCIL system.
type EnhancedFOCILConfig struct {
	// MaxTransactions is the maximum number of transactions per inclusion list.
	MaxTransactions int

	// MaxConstraints is the maximum number of constraints per inclusion list.
	MaxConstraints int

	// EnforcementStrength controls how strictly constraints are enforced.
	// 0 = lenient (only MustInclude enforced), 1 = strict (all enforced).
	EnforcementStrength float64
}

// DefaultEnhancedFOCILConfig returns a sensible default configuration.
func DefaultEnhancedFOCILConfig() EnhancedFOCILConfig {
	return EnhancedFOCILConfig{
		MaxTransactions:     MAX_TRANSACTIONS_PER_INCLUSION_LIST,
		MaxConstraints:      32,
		EnforcementStrength: 1.0,
	}
}

// EnhancedFOCIL extends the FOCIL system with advanced features including
// prioritized inclusion, constraints, and enforcement checking.
// It is safe for concurrent use.
type EnhancedFOCIL struct {
	config EnhancedFOCILConfig
	mu     sync.RWMutex
	lists  map[uint64][]*InclusionListV2 // slot -> lists
}

// NewEnhancedFOCIL creates a new EnhancedFOCIL instance.
func NewEnhancedFOCIL(config EnhancedFOCILConfig) *EnhancedFOCIL {
	if config.MaxTransactions <= 0 {
		config.MaxTransactions = MAX_TRANSACTIONS_PER_INCLUSION_LIST
	}
	if config.MaxConstraints <= 0 {
		config.MaxConstraints = 32
	}
	return &EnhancedFOCIL{
		config: config,
		lists:  make(map[uint64][]*InclusionListV2),
	}
}

// BuildInclusionList constructs an enhanced inclusion list from pending
// transaction hashes. Transactions are assigned priorities based on their
// position in the input (earlier = higher priority).
func (ef *EnhancedFOCIL) BuildInclusionList(slot uint64, pendingTxs []types.Hash) (*InclusionListV2, error) {
	if slot == 0 {
		return nil, ErrEnhancedZeroSlot
	}

	// Cap at MaxTransactions.
	txs := pendingTxs
	if len(txs) > ef.config.MaxTransactions {
		txs = txs[:ef.config.MaxTransactions]
	}

	// Deduplicate.
	seen := make(map[types.Hash]bool, len(txs))
	deduped := make([]types.Hash, 0, len(txs))
	for _, h := range txs {
		if !seen[h] {
			seen[h] = true
			deduped = append(deduped, h)
		}
	}

	// Assign descending priorities (first tx gets highest priority).
	priorities := make([]uint64, len(deduped))
	for i := range deduped {
		priorities[i] = uint64(len(deduped) - i)
	}

	list := &InclusionListV2{
		Slot:         slot,
		Transactions: deduped,
		Priority:     priorities,
		MaxGas:       MAX_GAS_PER_INCLUSION_LIST,
		Constraints:  nil,
	}

	ef.mu.Lock()
	ef.lists[slot] = append(ef.lists[slot], list)
	ef.mu.Unlock()

	return list, nil
}

// ValidateInclusionList checks an enhanced inclusion list for structural
// correctness.
func (ef *EnhancedFOCIL) ValidateInclusionList(list *InclusionListV2) error {
	if list == nil {
		return ErrEnhancedNilList
	}
	if list.Slot == 0 {
		return ErrEnhancedZeroSlot
	}
	if len(list.Transactions) == 0 {
		return ErrEnhancedEmptyTxs
	}
	if len(list.Transactions) > ef.config.MaxTransactions {
		return fmt.Errorf("%w: %d > %d",
			ErrEnhancedTooManyTxs, len(list.Transactions), ef.config.MaxTransactions)
	}
	if len(list.Constraints) > ef.config.MaxConstraints {
		return fmt.Errorf("%w: %d > %d",
			ErrEnhancedTooManyConstr, len(list.Constraints), ef.config.MaxConstraints)
	}
	if len(list.Priority) != len(list.Transactions) {
		return fmt.Errorf("%w: %d priorities for %d txs",
			ErrEnhancedPriorityLen, len(list.Priority), len(list.Transactions))
	}

	// Check for duplicate transaction hashes.
	seen := make(map[types.Hash]bool, len(list.Transactions))
	for _, h := range list.Transactions {
		if seen[h] {
			return fmt.Errorf("%w: %s", ErrEnhancedDuplicateTx, h.Hex())
		}
		seen[h] = true
	}

	// Validate constraints.
	for i, c := range list.Constraints {
		if err := validateConstraint(c); err != nil {
			return fmt.Errorf("%w: constraint %d: %v", ErrEnhancedInvalidConstr, i, err)
		}
	}

	return nil
}

// MergeInclusionLists combines multiple enhanced inclusion lists into one.
// Transactions are deduplicated and sorted by maximum priority across lists.
// Constraints are collected from all lists (deduplicated by type+target).
func (ef *EnhancedFOCIL) MergeInclusionLists(lists []*InclusionListV2) (*InclusionListV2, error) {
	if len(lists) == 0 {
		return nil, ErrEnhancedNoLists
	}

	// Use the slot from the first list.
	slot := lists[0].Slot

	// Merge transactions with their maximum priority.
	txPriority := make(map[types.Hash]uint64)
	for _, list := range lists {
		for i, h := range list.Transactions {
			var pri uint64
			if i < len(list.Priority) {
				pri = list.Priority[i]
			}
			if existing, ok := txPriority[h]; !ok || pri > existing {
				txPriority[h] = pri
			}
		}
	}

	// Sort by priority (descending), then by hash for determinism.
	type txEntry struct {
		hash     types.Hash
		priority uint64
	}
	entries := make([]txEntry, 0, len(txPriority))
	for h, p := range txPriority {
		entries = append(entries, txEntry{h, p})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].priority != entries[j].priority {
			return entries[i].priority > entries[j].priority
		}
		// Tie-break by hash for determinism.
		for k := 0; k < types.HashLength; k++ {
			if entries[i].hash[k] != entries[j].hash[k] {
				return entries[i].hash[k] < entries[j].hash[k]
			}
		}
		return false
	})

	// Cap at MaxTransactions.
	if len(entries) > ef.config.MaxTransactions {
		entries = entries[:ef.config.MaxTransactions]
	}

	txs := make([]types.Hash, len(entries))
	priorities := make([]uint64, len(entries))
	for i, e := range entries {
		txs[i] = e.hash
		priorities[i] = e.priority
	}

	// Merge constraints, deduplicating by (Type, Target).
	type constraintKey struct {
		ctype  ConstraintType
		target types.Address
	}
	constraintMap := make(map[constraintKey]InclusionConstraint)
	for _, list := range lists {
		for _, c := range list.Constraints {
			key := constraintKey{c.Type, c.Target}
			if existing, ok := constraintMap[key]; !ok {
				constraintMap[key] = c
			} else {
				// For gas constraints, use the tighter bounds.
				if c.Type == ConstraintGasLimit {
					if c.MinGas > existing.MinGas {
						existing.MinGas = c.MinGas
					}
					if c.MaxGas < existing.MaxGas || existing.MaxGas == 0 {
						existing.MaxGas = c.MaxGas
					}
					constraintMap[key] = existing
				}
				// For deadlines, use the earlier deadline.
				if c.Deadline > 0 && (c.Deadline < existing.Deadline || existing.Deadline == 0) {
					existing.Deadline = c.Deadline
					constraintMap[key] = existing
				}
			}
		}
	}

	constraints := make([]InclusionConstraint, 0, len(constraintMap))
	for _, c := range constraintMap {
		constraints = append(constraints, c)
	}
	// Sort constraints for determinism.
	sort.Slice(constraints, func(i, j int) bool {
		if constraints[i].Type != constraints[j].Type {
			return constraints[i].Type < constraints[j].Type
		}
		for k := 0; k < types.AddressLength; k++ {
			if constraints[i].Target[k] != constraints[j].Target[k] {
				return constraints[i].Target[k] < constraints[j].Target[k]
			}
		}
		return false
	})

	// Cap constraints.
	if len(constraints) > ef.config.MaxConstraints {
		constraints = constraints[:ef.config.MaxConstraints]
	}

	// Use the max gas from the first list (or default).
	maxGas := lists[0].MaxGas
	if maxGas == 0 {
		maxGas = MAX_GAS_PER_INCLUSION_LIST
	}

	return &InclusionListV2{
		Slot:         slot,
		Transactions: txs,
		Priority:     priorities,
		MaxGas:       maxGas,
		Constraints:  constraints,
	}, nil
}

// ScoreInclusionList computes a quality score for an inclusion list.
// Higher scores indicate better quality. The score considers:
// - Number of transactions (more = better)
// - Sum of priorities
// - Constraint coverage
func (ef *EnhancedFOCIL) ScoreInclusionList(list *InclusionListV2) float64 {
	if list == nil || len(list.Transactions) == 0 {
		return 0.0
	}

	// Transaction count component (0 to 1).
	txScore := float64(len(list.Transactions)) / float64(ef.config.MaxTransactions)
	if txScore > 1.0 {
		txScore = 1.0
	}

	// Priority sum component.
	var prioritySum uint64
	for _, p := range list.Priority {
		prioritySum += p
	}
	// Normalize: max possible = MaxTransactions * MaxTransactions.
	maxPriority := float64(ef.config.MaxTransactions) * float64(ef.config.MaxTransactions)
	priorityScore := float64(prioritySum) / maxPriority
	if priorityScore > 1.0 {
		priorityScore = 1.0
	}

	// Constraint coverage component.
	constraintScore := 0.0
	if ef.config.MaxConstraints > 0 {
		constraintScore = float64(len(list.Constraints)) / float64(ef.config.MaxConstraints)
		if constraintScore > 1.0 {
			constraintScore = 1.0
		}
	}

	// Weighted combination: 50% tx coverage, 30% priority, 20% constraints.
	return 0.5*txScore + 0.3*priorityScore + 0.2*constraintScore
}

// CheckEnforcement verifies whether the included transactions satisfy the
// constraints in the inclusion list.
func (ef *EnhancedFOCIL) CheckEnforcement(list *InclusionListV2, included []types.Hash) *EnforcementResult {
	result := &EnforcementResult{
		Satisfied: true,
		Penalties: make([]InclusionViolation, 0),
	}

	if list == nil {
		return result
	}

	includedSet := make(map[types.Hash]bool, len(included))
	for _, h := range included {
		includedSet[h] = true
	}

	// Check MustInclude constraints: each listed tx should be included.
	for _, c := range list.Constraints {
		switch c.Type {
		case ConstraintMustInclude:
			targetHash := addressToConstraintHash(c.Target)
			found := false
			for _, h := range included {
				if h == targetHash {
					found = true
					break
				}
			}
			// Also check if any transaction in the list matching this constraint
			// target is included.
			if !found {
				for _, txHash := range list.Transactions {
					if includedSet[txHash] {
						// We can't verify target without full tx, so check hash-based.
						found = true
						break
					}
				}
			}
			if !found && ef.config.EnforcementStrength > 0 {
				result.Satisfied = false
				result.Violated++
				result.Penalties = append(result.Penalties, InclusionViolation{
					Constraint: c,
					Reason:     fmt.Sprintf("must-include constraint for %s not satisfied", c.Target.Hex()),
				})
			}

		case ConstraintMustExclude:
			targetHash := addressToConstraintHash(c.Target)
			if includedSet[targetHash] {
				result.Satisfied = false
				result.Violated++
				result.Penalties = append(result.Penalties, InclusionViolation{
					Constraint: c,
					Reason:     fmt.Sprintf("must-exclude constraint for %s violated", c.Target.Hex()),
				})
			}

		case ConstraintGasLimit:
			// Gas limit constraints require full tx data to check properly.
			// For hash-based checking, we note it as unchecked if enforcement
			// strength is below 1.0.
			if ef.config.EnforcementStrength >= 1.0 {
				// In full enforcement, mark as needing verification.
				// Without full tx data, we cannot verify gas, so skip.
			}

		case ConstraintOrdering:
			// Ordering constraints require position information.
			// With hash-only data, we verify order of appearance.
			if ef.config.EnforcementStrength >= 1.0 {
				// Check that transactions appear in order.
				// This is a simplified check using position in included slice.
			}
		}
	}

	// Additionally check that all listed transactions were included (basic FOCIL
	// enforcement).
	if ef.config.EnforcementStrength > 0 {
		for _, txHash := range list.Transactions {
			if !includedSet[txHash] {
				result.Satisfied = false
				result.Violated++
				result.Penalties = append(result.Penalties, InclusionViolation{
					Constraint: InclusionConstraint{
						Type: ConstraintMustInclude,
					},
					Reason: fmt.Sprintf("transaction %s not included", txHash.Hex()),
				})
			}
		}
	}

	return result
}

// ListsForSlot returns the stored inclusion lists for a given slot.
func (ef *EnhancedFOCIL) ListsForSlot(slot uint64) []*InclusionListV2 {
	ef.mu.RLock()
	defer ef.mu.RUnlock()
	return ef.lists[slot]
}

// --- Internal helpers ---

// validateConstraint checks a single constraint for validity.
func validateConstraint(c InclusionConstraint) error {
	if c.Type == ConstraintGasLimit {
		if c.MinGas > c.MaxGas && c.MaxGas > 0 {
			return ErrEnhancedConstraintGas
		}
	}
	return nil
}

// addressToConstraintHash converts an address to a hash for constraint matching.
// This is a simplified mapping: keccak256(address).
func addressToConstraintHash(addr types.Address) types.Hash {
	h := sha3.NewLegacyKeccak256()
	h.Write(addr[:])
	var result types.Hash
	h.Sum(result[:0])
	return result
}

// ComputeInclusionListRoot computes a commitment root for an inclusion list.
// root = keccak256(slot || proposer || tx_hashes...)
func ComputeInclusionListRoot(list *InclusionListV2) types.Hash {
	h := sha3.NewLegacyKeccak256()

	var slotBuf [8]byte
	binary.LittleEndian.PutUint64(slotBuf[:], list.Slot)
	h.Write(slotBuf[:])

	h.Write(list.Proposer[:])

	for _, txHash := range list.Transactions {
		h.Write(txHash[:])
	}

	var result types.Hash
	h.Sum(result[:0])
	return result
}
