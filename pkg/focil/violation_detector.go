// violation_detector.go implements detection and reporting of FOCIL inclusion
// list violations for the eth2028 Ethereum client.
//
// Per EIP-7805, block builders must include transactions from the merged
// inclusion list or risk attestation penalties. The ViolationDetector:
//
//   - Compares block contents against the union of active inclusion lists
//   - Detects missing transactions that should have been included
//   - Identifies conflicting ILs from the same committee member
//   - Tracks committee members who failed to submit their IL
//   - Computes penalty amounts based on violation severity
//   - Aggregates violations into per-epoch reports
//
// Violation detection is a prerequisite for the slashing conditions that
// enforce FOCIL's censorship-resistance guarantees.
package focil

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// ViolationType identifies the kind of FOCIL violation.
type ViolationType uint8

const (
	// MissingTransaction means a transaction from the merged IL was not
	// included in the block and was valid at execution time.
	MissingTransaction ViolationType = iota

	// DelayedSubmission means a committee member submitted their IL after
	// the deadline.
	DelayedSubmission

	// ConflictingIL means a committee member submitted two different ILs
	// for the same slot.
	ConflictingIL

	// CommitteeAbsent means a committee member did not submit any IL for
	// their assigned slot.
	CommitteeAbsent
)

// String returns a human-readable name for ViolationType.
func (v ViolationType) String() string {
	switch v {
	case MissingTransaction:
		return "missing_transaction"
	case DelayedSubmission:
		return "delayed_submission"
	case ConflictingIL:
		return "conflicting_il"
	case CommitteeAbsent:
		return "committee_absent"
	default:
		return "unknown"
	}
}

// Violation represents a single detected FOCIL protocol violation.
type Violation struct {
	// Type classifies the violation.
	Type ViolationType

	// Slot is the beacon slot where the violation occurred.
	Slot uint64

	// ValidatorIndex is the responsible validator (0 for block-level violations).
	ValidatorIndex uint64

	// Evidence contains violation-specific proof data (e.g., missing tx hash).
	Evidence types.Hash

	// Timestamp is when the violation was detected (Unix seconds).
	Timestamp uint64

	// Description provides a human-readable summary.
	Description string
}

// ViolationReport aggregates violations for a reporting period (typically
// one epoch).
type ViolationReport struct {
	// StartSlot is the first slot in the reporting period.
	StartSlot uint64

	// EndSlot is the last slot (inclusive) in the reporting period.
	EndSlot uint64

	// Violations lists all detected violations.
	Violations []Violation

	// TotalPenaltyGwei is the total computed penalty across all violations.
	TotalPenaltyGwei uint64

	// ByType breaks down violation counts by type.
	ByType map[ViolationType]int

	// ByValidator breaks down violation counts by validator index.
	ByValidator map[uint64]int
}

// ViolationDetectorConfig configures the violation detector.
type ViolationDetectorConfig struct {
	// MissingTxPenaltyGwei is the base penalty per missing transaction.
	MissingTxPenaltyGwei uint64

	// DelayedSubmissionPenaltyGwei is the penalty for a late IL submission.
	DelayedSubmissionPenaltyGwei uint64

	// ConflictingILPenaltyGwei is the penalty for submitting conflicting ILs.
	ConflictingILPenaltyGwei uint64

	// AbsentPenaltyGwei is the penalty for not submitting an IL at all.
	AbsentPenaltyGwei uint64

	// SubmissionDeadlineSeconds is the max time after slot start for a
	// valid IL submission (used to detect delayed submissions).
	SubmissionDeadlineSeconds uint64
}

// DefaultViolationDetectorConfig returns production defaults.
func DefaultViolationDetectorConfig() ViolationDetectorConfig {
	return ViolationDetectorConfig{
		MissingTxPenaltyGwei:         1_000_000,   // 0.001 ETH per missing tx
		DelayedSubmissionPenaltyGwei: 500_000,     // 0.0005 ETH
		ConflictingILPenaltyGwei:     10_000_000,  // 0.01 ETH (severe)
		AbsentPenaltyGwei:            2_000_000,   // 0.002 ETH
		SubmissionDeadlineSeconds:    4,           // 4 seconds into slot
	}
}

// Violation detector errors.
var (
	ErrDetectorNilBlock  = errors.New("violation-detector: nil block")
	ErrDetectorNoILs     = errors.New("violation-detector: no inclusion lists provided")
	ErrDetectorNilIL     = errors.New("violation-detector: nil inclusion list")
)

// ViolationDetector identifies FOCIL protocol violations by comparing block
// contents against inclusion lists and tracking committee behavior.
// All public methods are safe for concurrent use.
type ViolationDetector struct {
	mu     sync.RWMutex
	config ViolationDetectorConfig

	// violations stores all detected violations keyed by slot.
	violations map[uint64][]Violation
}

// NewViolationDetector creates a new ViolationDetector.
func NewViolationDetector(config ViolationDetectorConfig) *ViolationDetector {
	if config.MissingTxPenaltyGwei == 0 {
		config.MissingTxPenaltyGwei = 1_000_000
	}
	if config.AbsentPenaltyGwei == 0 {
		config.AbsentPenaltyGwei = 2_000_000
	}
	if config.ConflictingILPenaltyGwei == 0 {
		config.ConflictingILPenaltyGwei = 10_000_000
	}
	if config.SubmissionDeadlineSeconds == 0 {
		config.SubmissionDeadlineSeconds = 4
	}
	return &ViolationDetector{
		config:     config,
		violations: make(map[uint64][]Violation),
	}
}

// CheckBlockCompliance compares a block's transactions against the union of
// all inclusion lists for the block's slot. Returns violations for any IL
// transactions missing from the block.
func (vd *ViolationDetector) CheckBlockCompliance(block *types.Block, inclusionLists []*InclusionList) []Violation {
	if block == nil {
		return nil
	}
	if len(inclusionLists) == 0 {
		return nil
	}

	slot := block.NumberU64()
	now := uint64(time.Now().Unix())

	// Build set of block transaction hashes.
	blockTxHashes := make(map[types.Hash]bool, len(block.Transactions()))
	for _, tx := range block.Transactions() {
		blockTxHashes[tx.Hash()] = true
	}

	// Collect unique required hashes from all ILs.
	required := make(map[types.Hash]bool)
	for _, il := range inclusionLists {
		if il == nil {
			continue
		}
		for _, entry := range il.Entries {
			tx, err := types.DecodeTxRLP(entry.Transaction)
			if err != nil {
				continue // skip invalid entries per spec
			}
			required[tx.Hash()] = true
		}
	}

	var violations []Violation

	// Detect missing transactions.
	missing := DetectMissingTransactions(blockTxHashes, required)
	for _, h := range missing {
		violations = append(violations, Violation{
			Type:        MissingTransaction,
			Slot:        slot,
			Evidence:    h,
			Timestamp:   now,
			Description: fmt.Sprintf("IL tx %s not included in block at slot %d", h.Hex(), slot),
		})
	}

	// Store violations.
	if len(violations) > 0 {
		vd.mu.Lock()
		vd.violations[slot] = append(vd.violations[slot], violations...)
		vd.mu.Unlock()
	}

	return violations
}

// DetectMissingTransactions compares block transaction hashes against IL
// transaction hashes and returns hashes that are in the IL but not in the
// block. Both inputs are hash sets for O(1) lookup.
func DetectMissingTransactions(blockTxHashes map[types.Hash]bool, ilTxHashes map[types.Hash]bool) []types.Hash {
	var missing []types.Hash
	for h := range ilTxHashes {
		if !blockTxHashes[h] {
			missing = append(missing, h)
		}
	}
	// Sort for deterministic output.
	sort.Slice(missing, func(i, j int) bool {
		for k := 0; k < types.HashLength; k++ {
			if missing[i][k] != missing[j][k] {
				return missing[i][k] < missing[j][k]
			}
		}
		return false
	})
	return missing
}

// DetectMissingFromSlices is a convenience wrapper that accepts hash slices
// instead of maps. Returns IL tx hashes not found in the block.
func DetectMissingFromSlices(blockTxHashes, ilTxHashes []types.Hash) []types.Hash {
	blockSet := make(map[types.Hash]bool, len(blockTxHashes))
	for _, h := range blockTxHashes {
		blockSet[h] = true
	}
	ilSet := make(map[types.Hash]bool, len(ilTxHashes))
	for _, h := range ilTxHashes {
		ilSet[h] = true
	}
	return DetectMissingTransactions(blockSet, ilSet)
}

// DetectConflicting checks whether two inclusion lists from the same
// committee member conflict. Two ILs conflict if they target the same slot
// but have different transaction sets (different IL roots).
func DetectConflicting(il1, il2 *InclusionList) bool {
	if il1 == nil || il2 == nil {
		return false
	}
	// Must be from the same proposer and same slot to conflict.
	if il1.ProposerIndex != il2.ProposerIndex {
		return false
	}
	if il1.Slot != il2.Slot {
		return false
	}

	// Compare transaction contents.
	hashes1 := ilEntryHashes(il1)
	hashes2 := ilEntryHashes(il2)

	if len(hashes1) != len(hashes2) {
		return true
	}

	// Build sorted sets for comparison.
	set1 := sortHashes(hashes1)
	set2 := sortHashes(hashes2)

	for i := range set1 {
		if set1[i] != set2[i] {
			return true
		}
	}
	return false
}

// ilEntryHashes extracts transaction hashes from an IL's entries.
func ilEntryHashes(il *InclusionList) []types.Hash {
	var hashes []types.Hash
	for _, entry := range il.Entries {
		tx, err := types.DecodeTxRLP(entry.Transaction)
		if err != nil {
			continue
		}
		hashes = append(hashes, tx.Hash())
	}
	return hashes
}

// sortHashes returns a sorted copy of the hash slice.
func sortHashes(hashes []types.Hash) []types.Hash {
	sorted := make([]types.Hash, len(hashes))
	copy(sorted, hashes)
	sort.Slice(sorted, func(i, j int) bool {
		for k := 0; k < types.HashLength; k++ {
			if sorted[i][k] != sorted[j][k] {
				return sorted[i][k] < sorted[j][k]
			}
		}
		return false
	})
	return sorted
}

// RecordAbsentMember records that a committee member did not submit an IL
// for their assigned slot.
func (vd *ViolationDetector) RecordAbsentMember(validatorIndex uint64, slot uint64) {
	now := uint64(time.Now().Unix())
	v := Violation{
		Type:           CommitteeAbsent,
		Slot:           slot,
		ValidatorIndex: validatorIndex,
		Timestamp:      now,
		Description: fmt.Sprintf("validator %d did not submit IL for slot %d",
			validatorIndex, slot),
	}

	vd.mu.Lock()
	vd.violations[slot] = append(vd.violations[slot], v)
	vd.mu.Unlock()
}

// RecordConflictingIL records that a committee member submitted conflicting
// ILs for a slot.
func (vd *ViolationDetector) RecordConflictingIL(validatorIndex uint64, slot uint64, evidence types.Hash) {
	now := uint64(time.Now().Unix())
	v := Violation{
		Type:           ConflictingIL,
		Slot:           slot,
		ValidatorIndex: validatorIndex,
		Evidence:       evidence,
		Timestamp:      now,
		Description: fmt.Sprintf("validator %d submitted conflicting ILs for slot %d",
			validatorIndex, slot),
	}

	vd.mu.Lock()
	vd.violations[slot] = append(vd.violations[slot], v)
	vd.mu.Unlock()
}

// RecordDelayedSubmission records that a committee member submitted their
// IL late.
func (vd *ViolationDetector) RecordDelayedSubmission(validatorIndex uint64, slot uint64) {
	now := uint64(time.Now().Unix())
	v := Violation{
		Type:           DelayedSubmission,
		Slot:           slot,
		ValidatorIndex: validatorIndex,
		Timestamp:      now,
		Description: fmt.Sprintf("validator %d submitted IL late for slot %d",
			validatorIndex, slot),
	}

	vd.mu.Lock()
	vd.violations[slot] = append(vd.violations[slot], v)
	vd.mu.Unlock()
}

// ComputeViolationPenalty computes the penalty in Gwei for a given violation
// based on its type and the detector's configuration.
func (vd *ViolationDetector) ComputeViolationPenalty(v Violation) uint64 {
	vd.mu.RLock()
	cfg := vd.config
	vd.mu.RUnlock()

	switch v.Type {
	case MissingTransaction:
		return cfg.MissingTxPenaltyGwei
	case DelayedSubmission:
		return cfg.DelayedSubmissionPenaltyGwei
	case ConflictingIL:
		return cfg.ConflictingILPenaltyGwei
	case CommitteeAbsent:
		return cfg.AbsentPenaltyGwei
	default:
		return 0
	}
}

// GetViolations returns all violations recorded for a given slot.
func (vd *ViolationDetector) GetViolations(slot uint64) []Violation {
	vd.mu.RLock()
	defer vd.mu.RUnlock()

	vs := vd.violations[slot]
	if len(vs) == 0 {
		return nil
	}
	result := make([]Violation, len(vs))
	copy(result, vs)
	return result
}

// GenerateReport creates a ViolationReport for a range of slots (inclusive).
// This aggregates all violations, computes total penalties, and breaks down
// counts by type and validator.
func (vd *ViolationDetector) GenerateReport(startSlot, endSlot uint64) *ViolationReport {
	vd.mu.RLock()
	defer vd.mu.RUnlock()

	report := &ViolationReport{
		StartSlot:   startSlot,
		EndSlot:     endSlot,
		ByType:      make(map[ViolationType]int),
		ByValidator: make(map[uint64]int),
	}

	for slot := startSlot; slot <= endSlot; slot++ {
		vs, ok := vd.violations[slot]
		if !ok {
			continue
		}
		for _, v := range vs {
			report.Violations = append(report.Violations, v)
			report.TotalPenaltyGwei += vd.computePenaltyLocked(v)
			report.ByType[v.Type]++
			if v.ValidatorIndex > 0 || v.Type == CommitteeAbsent {
				report.ByValidator[v.ValidatorIndex]++
			}
		}
	}

	return report
}

// computePenaltyLocked computes penalty without acquiring the lock.
// Caller must hold at least a read lock.
func (vd *ViolationDetector) computePenaltyLocked(v Violation) uint64 {
	switch v.Type {
	case MissingTransaction:
		return vd.config.MissingTxPenaltyGwei
	case DelayedSubmission:
		return vd.config.DelayedSubmissionPenaltyGwei
	case ConflictingIL:
		return vd.config.ConflictingILPenaltyGwei
	case CommitteeAbsent:
		return vd.config.AbsentPenaltyGwei
	default:
		return 0
	}
}

// PruneBefore removes violation records for slots before the given slot.
// Returns the number of slots pruned.
func (vd *ViolationDetector) PruneBefore(slot uint64) int {
	vd.mu.Lock()
	defer vd.mu.Unlock()

	pruned := 0
	for s := range vd.violations {
		if s < slot {
			delete(vd.violations, s)
			pruned++
		}
	}
	return pruned
}

// ViolationCount returns the total number of violations recorded for a slot.
func (vd *ViolationDetector) ViolationCount(slot uint64) int {
	vd.mu.RLock()
	defer vd.mu.RUnlock()
	return len(vd.violations[slot])
}
