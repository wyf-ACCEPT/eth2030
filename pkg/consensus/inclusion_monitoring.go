package consensus

import (
	"errors"
	"fmt"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// InclusionMonitor tracks FOCIL inclusion list compliance, censorship
// resistance scoring, and violation detection across blocks.

// Inclusion monitoring errors.
var (
	ErrIMSlotNotFound      = errors.New("inclusion-monitor: slot not found")
	ErrIMProposerNotFound  = errors.New("inclusion-monitor: proposer not found")
	ErrIMDuplicateBlock    = errors.New("inclusion-monitor: block already recorded")
	ErrIMInvalidScore      = errors.New("inclusion-monitor: score out of range")
	ErrIMNoData            = errors.New("inclusion-monitor: no data for epoch")
)

// InclusionComplianceScore represents a proposer's compliance score.
type InclusionComplianceScore struct {
	ProposerIndex     uint64
	Score             uint64 // 0-100, higher is better
	TotalListed       int    // txs on inclusion list
	TotalIncluded     int    // listed txs actually included
	TotalMissed       int    // listed txs not included
	ViolationCount    int
}

// InclusionDelayRecord tracks delay between tx submission and inclusion.
type InclusionDelayRecord struct {
	TxHash       types.Hash
	SubmitSlot   uint64
	IncludeSlot  uint64
	DelaySlots   uint64
	ProposerIndex uint64
}

// FOCILViolation represents a detected inclusion list violation.
type FOCILViolation struct {
	ProposerIndex  uint64
	Slot           uint64
	MissedTxHashes []types.Hash
	ListSize       int
	IncludedCount  int
	Severity       uint64 // 0=minor, 1=moderate, 2=severe
}

// CensorshipScore scores a proposer's censorship resistance. Higher is better.
type CensorshipScore struct {
	ProposerIndex uint64
	Score         uint64 // 0-100
	BlocksProposed int
	ListCompliance float64 // 0.0-100.0 percentage of lists honored
	AvgDelaySlots  float64 // average inclusion delay
}

// InclusionEpochReport summarizes inclusion quality for an epoch.
type InclusionEpochReport struct {
	Epoch              uint64
	TotalBlocks        int
	AvgComplianceScore float64
	TotalViolations    int
	WorstProposer      uint64
	WorstScore         uint64
	BestProposer       uint64
	BestScore          uint64
	ProposerScores     map[uint64]*InclusionComplianceScore
}

// slotBlockRecord tracks inclusion data for a single block/slot.
type slotBlockRecord struct {
	slot           uint64
	proposerIndex  uint64
	blockRoot      types.Hash
	listedTxs      map[types.Hash]bool // txs on the inclusion list
	includedTxs    map[types.Hash]bool // txs actually included
}

// InclusionMonitorConfig configures the monitor.
type InclusionMonitorConfig struct {
	// ViolationThreshold: if compliance drops below this, it's a violation.
	ViolationThreshold float64
	// SevereViolationMiss: missed tx count that triggers severe violation.
	SevereViolationMiss int
	// MaxHistory: number of epochs of data to retain.
	MaxHistory int
}

// DefaultInclusionMonitorConfig returns production defaults.
func DefaultInclusionMonitorConfig() InclusionMonitorConfig {
	return InclusionMonitorConfig{
		ViolationThreshold:  50.0,
		SevereViolationMiss: 10,
		MaxHistory:          64,
	}
}

// InclusionMonitor tracks inclusion list compliance across blocks. Thread-safe.
type InclusionMonitor struct {
	mu     sync.RWMutex
	config InclusionMonitorConfig

	// Per-slot block records.
	blocks map[uint64]*slotBlockRecord

	// Per-proposer cumulative scores.
	proposerScores map[uint64]*InclusionComplianceScore

	// Detected violations.
	violations []*FOCILViolation

	// Inclusion delay records.
	delays []*InclusionDelayRecord

	// Per-epoch report cache.
	reports map[uint64]*InclusionEpochReport
}

// NewInclusionMonitor creates a new inclusion monitor.
func NewInclusionMonitor(config InclusionMonitorConfig) *InclusionMonitor {
	if config.MaxHistory <= 0 {
		config.MaxHistory = 64
	}
	if config.ViolationThreshold <= 0 {
		config.ViolationThreshold = 50.0
	}
	if config.SevereViolationMiss <= 0 {
		config.SevereViolationMiss = 10
	}
	return &InclusionMonitor{
		config:         config,
		blocks:         make(map[uint64]*slotBlockRecord),
		proposerScores: make(map[uint64]*InclusionComplianceScore),
		reports:        make(map[uint64]*InclusionEpochReport),
	}
}

// RecordBlock registers a block with its inclusion list and actually included txs.
func (im *InclusionMonitor) RecordBlock(
	slot uint64,
	proposerIndex uint64,
	blockRoot types.Hash,
	listedTxs []types.Hash,
	includedTxs []types.Hash,
) error {
	im.mu.Lock()
	defer im.mu.Unlock()

	if _, ok := im.blocks[slot]; ok {
		return fmt.Errorf("%w: slot %d", ErrIMDuplicateBlock, slot)
	}

	listed := make(map[types.Hash]bool, len(listedTxs))
	for _, tx := range listedTxs {
		listed[tx] = true
	}

	included := make(map[types.Hash]bool, len(includedTxs))
	for _, tx := range includedTxs {
		included[tx] = true
	}

	im.blocks[slot] = &slotBlockRecord{
		slot:          slot,
		proposerIndex: proposerIndex,
		blockRoot:     blockRoot,
		listedTxs:     listed,
		includedTxs:   included,
	}

	// Update proposer compliance score.
	im.updateProposerScore(proposerIndex, listed, included)

	// Check for violations.
	im.checkViolation(slot, proposerIndex, listed, included)

	return nil
}

// RecordInclusionDelay records a tx inclusion delay.
func (im *InclusionMonitor) RecordInclusionDelay(txHash types.Hash, submitSlot, includeSlot uint64, proposer uint64) {
	im.mu.Lock()
	defer im.mu.Unlock()

	delay := uint64(0)
	if includeSlot > submitSlot {
		delay = includeSlot - submitSlot
	}
	im.delays = append(im.delays, &InclusionDelayRecord{
		TxHash:        txHash,
		SubmitSlot:    submitSlot,
		IncludeSlot:   includeSlot,
		DelaySlots:    delay,
		ProposerIndex: proposer,
	})
}

// GetComplianceScore returns the compliance score for a proposer.
func (im *InclusionMonitor) GetComplianceScore(proposerIndex uint64) (*InclusionComplianceScore, error) {
	im.mu.RLock()
	defer im.mu.RUnlock()

	score, ok := im.proposerScores[proposerIndex]
	if !ok {
		return nil, fmt.Errorf("%w: proposer %d", ErrIMProposerNotFound, proposerIndex)
	}
	cp := *score
	return &cp, nil
}

// DetectViolations returns all detected FOCIL violations.
func (im *InclusionMonitor) DetectViolations() []*FOCILViolation {
	im.mu.RLock()
	defer im.mu.RUnlock()

	result := make([]*FOCILViolation, len(im.violations))
	for i, v := range im.violations {
		cp := *v
		missedCopy := make([]types.Hash, len(v.MissedTxHashes))
		copy(missedCopy, v.MissedTxHashes)
		cp.MissedTxHashes = missedCopy
		result[i] = &cp
	}
	return result
}

// ViolationCount returns the total number of violations detected.
func (im *InclusionMonitor) ViolationCount() int {
	im.mu.RLock()
	defer im.mu.RUnlock()
	return len(im.violations)
}

// ComputeCensorshipScore computes censorship resistance for a proposer.
func (im *InclusionMonitor) ComputeCensorshipScore(proposerIndex uint64) (*CensorshipScore, error) {
	im.mu.RLock()
	defer im.mu.RUnlock()

	score, ok := im.proposerScores[proposerIndex]
	if !ok {
		return nil, fmt.Errorf("%w: proposer %d", ErrIMProposerNotFound, proposerIndex)
	}

	compliance := float64(0)
	if score.TotalListed > 0 {
		compliance = float64(score.TotalIncluded) / float64(score.TotalListed) * 100.0
	}

	// Compute average delay for this proposer's txs.
	var totalDelay uint64
	var delayCount int
	for _, d := range im.delays {
		if d.ProposerIndex == proposerIndex {
			totalDelay += d.DelaySlots
			delayCount++
		}
	}
	avgDelay := float64(0)
	if delayCount > 0 {
		avgDelay = float64(totalDelay) / float64(delayCount)
	}

	// Count blocks proposed.
	blocksProposed := 0
	for _, b := range im.blocks {
		if b.proposerIndex == proposerIndex {
			blocksProposed++
		}
	}

	return &CensorshipScore{
		ProposerIndex:  proposerIndex,
		Score:          score.Score,
		BlocksProposed: blocksProposed,
		ListCompliance: compliance,
		AvgDelaySlots:  avgDelay,
	}, nil
}

// GenerateEpochReport generates an inclusion quality report for an epoch.
func (im *InclusionMonitor) GenerateEpochReport(epoch uint64, slotsPerEpoch uint64) (*InclusionEpochReport, error) {
	im.mu.Lock()
	defer im.mu.Unlock()

	startSlot := epoch * slotsPerEpoch
	endSlot := startSlot + slotsPerEpoch

	report := &InclusionEpochReport{
		Epoch:          epoch,
		ProposerScores: make(map[uint64]*InclusionComplianceScore),
		WorstScore:     101, // sentinel
	}

	var totalScore uint64
	for slot := startSlot; slot < endSlot; slot++ {
		block, ok := im.blocks[slot]
		if !ok {
			continue
		}
		report.TotalBlocks++

		// Compute per-block compliance.
		listSize := len(block.listedTxs)
		includedCount := 0
		for tx := range block.listedTxs {
			if block.includedTxs[tx] {
				includedCount++
			}
		}

		blockScore := uint64(100)
		if listSize > 0 {
			blockScore = uint64(includedCount) * 100 / uint64(listSize)
		}
		totalScore += blockScore

		// Track per-proposer.
		ps, ok := report.ProposerScores[block.proposerIndex]
		if !ok {
			ps = &InclusionComplianceScore{ProposerIndex: block.proposerIndex}
			report.ProposerScores[block.proposerIndex] = ps
		}
		ps.TotalListed += listSize
		ps.TotalIncluded += includedCount
		ps.TotalMissed += listSize - includedCount
		if ps.TotalListed > 0 {
			ps.Score = uint64(ps.TotalIncluded) * 100 / uint64(ps.TotalListed)
		} else {
			ps.Score = 100
		}
	}

	if report.TotalBlocks > 0 {
		report.AvgComplianceScore = float64(totalScore) / float64(report.TotalBlocks)
	}

	// Count violations in this epoch.
	for _, v := range im.violations {
		if v.Slot >= startSlot && v.Slot < endSlot {
			report.TotalViolations++
		}
	}

	// Find best and worst proposers.
	for idx, ps := range report.ProposerScores {
		if ps.Score < report.WorstScore {
			report.WorstScore = ps.Score
			report.WorstProposer = idx
		}
		if ps.Score > report.BestScore {
			report.BestScore = ps.Score
			report.BestProposer = idx
		}
	}

	// Reset sentinel if no proposers.
	if len(report.ProposerScores) == 0 {
		report.WorstScore = 0
	}

	im.reports[epoch] = report
	return report, nil
}

// GetEpochReport returns a cached epoch report.
func (im *InclusionMonitor) GetEpochReport(epoch uint64) (*InclusionEpochReport, bool) {
	im.mu.RLock()
	defer im.mu.RUnlock()
	report, ok := im.reports[epoch]
	return report, ok
}

// GetInclusionDelays returns all recorded delays.
func (im *InclusionMonitor) GetInclusionDelays() []*InclusionDelayRecord {
	im.mu.RLock()
	defer im.mu.RUnlock()
	result := make([]*InclusionDelayRecord, len(im.delays))
	for i, d := range im.delays {
		cp := *d
		result[i] = &cp
	}
	return result
}

// --- internal helpers ---

// updateProposerScore updates cumulative compliance for a proposer.
func (im *InclusionMonitor) updateProposerScore(
	proposerIndex uint64,
	listed map[types.Hash]bool,
	included map[types.Hash]bool,
) {
	ps, ok := im.proposerScores[proposerIndex]
	if !ok {
		ps = &InclusionComplianceScore{ProposerIndex: proposerIndex}
		im.proposerScores[proposerIndex] = ps
	}

	listSize := len(listed)
	includedCount := 0
	for tx := range listed {
		if included[tx] {
			includedCount++
		}
	}

	ps.TotalListed += listSize
	ps.TotalIncluded += includedCount
	ps.TotalMissed += listSize - includedCount

	// Recompute score.
	if ps.TotalListed > 0 {
		ps.Score = uint64(ps.TotalIncluded) * 100 / uint64(ps.TotalListed)
	} else {
		ps.Score = 100 // no requirements, perfect score
	}
}

// checkViolation detects FOCIL inclusion list violations.
func (im *InclusionMonitor) checkViolation(
	slot uint64,
	proposerIndex uint64,
	listed map[types.Hash]bool,
	included map[types.Hash]bool,
) {
	if len(listed) == 0 {
		return // no list, no violation
	}

	var missedTxs []types.Hash
	for tx := range listed {
		if !included[tx] {
			missedTxs = append(missedTxs, tx)
		}
	}

	if len(missedTxs) == 0 {
		return // all listed txs included
	}

	compliance := float64(len(listed)-len(missedTxs)) / float64(len(listed)) * 100.0
	if compliance >= im.config.ViolationThreshold {
		return // above threshold, no violation
	}

	severity := uint64(0)
	if len(missedTxs) >= im.config.SevereViolationMiss {
		severity = 2
	} else if len(missedTxs) > 1 {
		severity = 1
	}

	violation := &FOCILViolation{
		ProposerIndex:  proposerIndex,
		Slot:           slot,
		MissedTxHashes: missedTxs,
		ListSize:       len(listed),
		IncludedCount:  len(listed) - len(missedTxs),
		Severity:       severity,
	}

	im.violations = append(im.violations, violation)

	// Update proposer violation count.
	if ps, ok := im.proposerScores[proposerIndex]; ok {
		ps.ViolationCount++
	}
}
