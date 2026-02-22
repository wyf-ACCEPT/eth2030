package consensus

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func imTestHash(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	return h
}

func imTxHashes(n int) []types.Hash {
	txs := make([]types.Hash, n)
	for i := range txs {
		txs[i] = imTestHash(byte(i + 1))
	}
	return txs
}

// --- RecordBlock ---

func TestIM_RecordBlock_Basic(t *testing.T) {
	im := NewInclusionMonitor(DefaultInclusionMonitorConfig())

	listed := imTxHashes(5)
	included := listed[:5] // all included
	err := im.RecordBlock(1, 10, imTestHash(0xAA), listed, included)
	if err != nil {
		t.Fatalf("RecordBlock failed: %v", err)
	}
}

func TestIM_RecordBlock_Duplicate(t *testing.T) {
	im := NewInclusionMonitor(DefaultInclusionMonitorConfig())
	im.RecordBlock(1, 10, imTestHash(0xAA), nil, nil)
	err := im.RecordBlock(1, 10, imTestHash(0xBB), nil, nil)
	if err == nil {
		t.Error("expected error for duplicate block at same slot")
	}
}

// --- GetComplianceScore ---

func TestIM_ComplianceScore_Perfect(t *testing.T) {
	im := NewInclusionMonitor(DefaultInclusionMonitorConfig())

	listed := imTxHashes(10)
	im.RecordBlock(1, 5, imTestHash(0xAA), listed, listed)

	score, err := im.GetComplianceScore(5)
	if err != nil {
		t.Fatalf("GetComplianceScore failed: %v", err)
	}
	if score.Score != 100 {
		t.Errorf("Score = %d, want 100", score.Score)
	}
	if score.TotalListed != 10 {
		t.Errorf("TotalListed = %d, want 10", score.TotalListed)
	}
	if score.TotalIncluded != 10 {
		t.Errorf("TotalIncluded = %d, want 10", score.TotalIncluded)
	}
	if score.TotalMissed != 0 {
		t.Errorf("TotalMissed = %d, want 0", score.TotalMissed)
	}
}

func TestIM_ComplianceScore_Partial(t *testing.T) {
	im := NewInclusionMonitor(DefaultInclusionMonitorConfig())

	listed := imTxHashes(10)
	included := listed[:7] // 7 out of 10
	im.RecordBlock(1, 5, imTestHash(0xAA), listed, included)

	score, err := im.GetComplianceScore(5)
	if err != nil {
		t.Fatalf("GetComplianceScore failed: %v", err)
	}
	if score.Score != 70 {
		t.Errorf("Score = %d, want 70", score.Score)
	}
	if score.TotalMissed != 3 {
		t.Errorf("TotalMissed = %d, want 3", score.TotalMissed)
	}
}

func TestIM_ComplianceScore_NotFound(t *testing.T) {
	im := NewInclusionMonitor(DefaultInclusionMonitorConfig())
	_, err := im.GetComplianceScore(99)
	if err == nil {
		t.Error("expected error for unknown proposer")
	}
}

func TestIM_ComplianceScore_EmptyList(t *testing.T) {
	im := NewInclusionMonitor(DefaultInclusionMonitorConfig())
	im.RecordBlock(1, 5, imTestHash(0xAA), nil, nil)

	score, err := im.GetComplianceScore(5)
	if err != nil {
		t.Fatalf("GetComplianceScore failed: %v", err)
	}
	// No listed txs = perfect score.
	if score.Score != 100 {
		t.Errorf("Score = %d, want 100 (no requirements)", score.Score)
	}
}

// --- Violation Detection ---

func TestIM_ViolationDetected(t *testing.T) {
	cfg := DefaultInclusionMonitorConfig()
	cfg.ViolationThreshold = 50.0 // 50% compliance needed
	im := NewInclusionMonitor(cfg)

	listed := imTxHashes(10)
	included := listed[:2] // only 2/10 = 20% -> violation
	im.RecordBlock(1, 5, imTestHash(0xAA), listed, included)

	violations := im.DetectViolations()
	if len(violations) != 1 {
		t.Fatalf("violations count = %d, want 1", len(violations))
	}
	if violations[0].ProposerIndex != 5 {
		t.Errorf("ProposerIndex = %d, want 5", violations[0].ProposerIndex)
	}
	if violations[0].Slot != 1 {
		t.Errorf("Slot = %d, want 1", violations[0].Slot)
	}
	if len(violations[0].MissedTxHashes) != 8 {
		t.Errorf("MissedTxHashes = %d, want 8", len(violations[0].MissedTxHashes))
	}
}

func TestIM_ViolationNotTriggered_AboveThreshold(t *testing.T) {
	cfg := DefaultInclusionMonitorConfig()
	cfg.ViolationThreshold = 50.0
	im := NewInclusionMonitor(cfg)

	listed := imTxHashes(10)
	included := listed[:6] // 60% > 50% threshold
	im.RecordBlock(1, 5, imTestHash(0xAA), listed, included)

	if im.ViolationCount() != 0 {
		t.Errorf("ViolationCount = %d, want 0 (above threshold)", im.ViolationCount())
	}
}

func TestIM_ViolationSeverity_Minor(t *testing.T) {
	cfg := DefaultInclusionMonitorConfig()
	cfg.ViolationThreshold = 80.0
	cfg.SevereViolationMiss = 5
	im := NewInclusionMonitor(cfg)

	listed := imTxHashes(10)
	included := listed[:9] // 1 miss, but 90% > 80%. Need lower compliance.
	// Actually 1 miss = 90% which is above 80. Let's adjust.
	included = listed[:7] // 3 miss = 70% < 80%
	im.RecordBlock(1, 5, imTestHash(0xAA), listed, included)

	violations := im.DetectViolations()
	if len(violations) != 1 {
		t.Fatalf("violations count = %d, want 1", len(violations))
	}
	// 3 misses, severity = 1 (moderate).
	if violations[0].Severity != 1 {
		t.Errorf("Severity = %d, want 1 (moderate)", violations[0].Severity)
	}
}

func TestIM_ViolationSeverity_Severe(t *testing.T) {
	cfg := DefaultInclusionMonitorConfig()
	cfg.ViolationThreshold = 50.0
	cfg.SevereViolationMiss = 5
	im := NewInclusionMonitor(cfg)

	listed := imTxHashes(20)
	included := listed[:2] // 18 misses >= 5 severe threshold
	im.RecordBlock(1, 5, imTestHash(0xAA), listed, included)

	violations := im.DetectViolations()
	if len(violations) != 1 {
		t.Fatalf("violations count = %d, want 1", len(violations))
	}
	if violations[0].Severity != 2 {
		t.Errorf("Severity = %d, want 2 (severe)", violations[0].Severity)
	}
}

func TestIM_ViolationCount(t *testing.T) {
	cfg := DefaultInclusionMonitorConfig()
	cfg.ViolationThreshold = 80.0
	im := NewInclusionMonitor(cfg)

	// 3 blocks, 2 violating.
	listed := imTxHashes(10)
	im.RecordBlock(1, 5, imTestHash(0x01), listed, listed[:2]) // 20% < 80% -> violation
	im.RecordBlock(2, 6, imTestHash(0x02), listed, listed[:9]) // 90% >= 80% -> ok
	im.RecordBlock(3, 7, imTestHash(0x03), listed, listed[:3]) // 30% < 80% -> violation

	if im.ViolationCount() != 2 {
		t.Errorf("ViolationCount = %d, want 2", im.ViolationCount())
	}
}

func TestIM_ViolationUpdatesProposerCount(t *testing.T) {
	cfg := DefaultInclusionMonitorConfig()
	cfg.ViolationThreshold = 50.0
	im := NewInclusionMonitor(cfg)

	listed := imTxHashes(10)
	im.RecordBlock(1, 5, imTestHash(0x01), listed, listed[:1]) // violation

	score, _ := im.GetComplianceScore(5)
	if score.ViolationCount != 1 {
		t.Errorf("ViolationCount = %d, want 1", score.ViolationCount)
	}
}

// --- InclusionDelay ---

func TestIM_RecordInclusionDelay(t *testing.T) {
	im := NewInclusionMonitor(DefaultInclusionMonitorConfig())
	im.RecordInclusionDelay(imTestHash(0x01), 10, 12, 5)

	delays := im.GetInclusionDelays()
	if len(delays) != 1 {
		t.Fatalf("delays count = %d, want 1", len(delays))
	}
	if delays[0].DelaySlots != 2 {
		t.Errorf("DelaySlots = %d, want 2", delays[0].DelaySlots)
	}
}

func TestIM_InclusionDelay_ZeroDelay(t *testing.T) {
	im := NewInclusionMonitor(DefaultInclusionMonitorConfig())
	im.RecordInclusionDelay(imTestHash(0x01), 10, 10, 5)

	delays := im.GetInclusionDelays()
	if delays[0].DelaySlots != 0 {
		t.Errorf("DelaySlots = %d, want 0 (same slot)", delays[0].DelaySlots)
	}
}

// --- CensorshipScore ---

func TestIM_CensorshipScore(t *testing.T) {
	im := NewInclusionMonitor(DefaultInclusionMonitorConfig())

	listed := imTxHashes(10)
	im.RecordBlock(1, 5, imTestHash(0xAA), listed, listed[:8])
	im.RecordInclusionDelay(imTestHash(0x01), 1, 3, 5)
	im.RecordInclusionDelay(imTestHash(0x02), 1, 5, 5)

	cs, err := im.ComputeCensorshipScore(5)
	if err != nil {
		t.Fatalf("ComputeCensorshipScore failed: %v", err)
	}
	if cs.Score != 80 {
		t.Errorf("Score = %d, want 80", cs.Score)
	}
	if cs.BlocksProposed != 1 {
		t.Errorf("BlocksProposed = %d, want 1", cs.BlocksProposed)
	}
	if cs.ListCompliance != 80.0 {
		t.Errorf("ListCompliance = %f, want 80.0", cs.ListCompliance)
	}
	// Avg delay: (2 + 4) / 2 = 3.
	if cs.AvgDelaySlots != 3.0 {
		t.Errorf("AvgDelaySlots = %f, want 3.0", cs.AvgDelaySlots)
	}
}

func TestIM_CensorshipScore_NotFound(t *testing.T) {
	im := NewInclusionMonitor(DefaultInclusionMonitorConfig())
	_, err := im.ComputeCensorshipScore(99)
	if err == nil {
		t.Error("expected error for unknown proposer")
	}
}

// --- EpochReport ---

func TestIM_GenerateEpochReport(t *testing.T) {
	im := NewInclusionMonitor(DefaultInclusionMonitorConfig())

	slotsPerEpoch := uint64(4)
	epoch := uint64(0)

	// Slots 0-3 in epoch 0.
	listed := imTxHashes(10)
	im.RecordBlock(0, 1, imTestHash(0x01), listed, listed)      // 100%
	im.RecordBlock(1, 2, imTestHash(0x02), listed, listed[:5])  // 50%
	im.RecordBlock(2, 3, imTestHash(0x03), listed, listed[:8])  // 80%
	im.RecordBlock(3, 1, imTestHash(0x04), listed, listed[:10]) // 100%

	report, err := im.GenerateEpochReport(epoch, slotsPerEpoch)
	if err != nil {
		t.Fatalf("GenerateEpochReport failed: %v", err)
	}
	if report.TotalBlocks != 4 {
		t.Errorf("TotalBlocks = %d, want 4", report.TotalBlocks)
	}
	if report.Epoch != 0 {
		t.Errorf("Epoch = %d, want 0", report.Epoch)
	}
	// Avg: (100 + 50 + 80 + 100) / 4 = 82.5
	if report.AvgComplianceScore < 82.0 || report.AvgComplianceScore > 83.0 {
		t.Errorf("AvgComplianceScore = %f, want ~82.5", report.AvgComplianceScore)
	}
}

func TestIM_GenerateEpochReport_Empty(t *testing.T) {
	im := NewInclusionMonitor(DefaultInclusionMonitorConfig())
	report, err := im.GenerateEpochReport(5, 32)
	if err != nil {
		t.Fatalf("GenerateEpochReport failed: %v", err)
	}
	if report.TotalBlocks != 0 {
		t.Errorf("TotalBlocks = %d, want 0", report.TotalBlocks)
	}
}

func TestIM_GenerateEpochReport_BestWorst(t *testing.T) {
	cfg := DefaultInclusionMonitorConfig()
	cfg.ViolationThreshold = 10.0 // low threshold so no violations interfere
	im := NewInclusionMonitor(cfg)

	slotsPerEpoch := uint64(2)
	listed := imTxHashes(10)

	im.RecordBlock(0, 10, imTestHash(0x01), listed, listed)     // 100%
	im.RecordBlock(1, 20, imTestHash(0x02), listed, listed[:3]) // 30%

	report, _ := im.GenerateEpochReport(0, slotsPerEpoch)

	if report.BestProposer != 10 {
		t.Errorf("BestProposer = %d, want 10", report.BestProposer)
	}
	if report.BestScore != 100 {
		t.Errorf("BestScore = %d, want 100", report.BestScore)
	}
	if report.WorstProposer != 20 {
		t.Errorf("WorstProposer = %d, want 20", report.WorstProposer)
	}
	if report.WorstScore != 30 {
		t.Errorf("WorstScore = %d, want 30", report.WorstScore)
	}
}

func TestIM_GetEpochReport_Cached(t *testing.T) {
	im := NewInclusionMonitor(DefaultInclusionMonitorConfig())
	im.GenerateEpochReport(5, 32)

	report, ok := im.GetEpochReport(5)
	if !ok {
		t.Fatal("epoch report not cached")
	}
	if report.Epoch != 5 {
		t.Errorf("Epoch = %d, want 5", report.Epoch)
	}
}

func TestIM_GetEpochReport_NotCached(t *testing.T) {
	im := NewInclusionMonitor(DefaultInclusionMonitorConfig())
	_, ok := im.GetEpochReport(99)
	if ok {
		t.Error("expected not cached")
	}
}

func TestIM_GenerateEpochReport_ViolationCount(t *testing.T) {
	cfg := DefaultInclusionMonitorConfig()
	cfg.ViolationThreshold = 50.0
	im := NewInclusionMonitor(cfg)

	slotsPerEpoch := uint64(4)
	listed := imTxHashes(10)

	im.RecordBlock(0, 1, imTestHash(0x01), listed, listed[:1]) // violation (10%)
	im.RecordBlock(1, 2, imTestHash(0x02), listed, listed)     // ok
	im.RecordBlock(2, 3, imTestHash(0x03), listed, listed[:2]) // violation (20%)
	im.RecordBlock(3, 4, imTestHash(0x04), listed, listed)     // ok

	report, _ := im.GenerateEpochReport(0, slotsPerEpoch)
	if report.TotalViolations != 2 {
		t.Errorf("TotalViolations = %d, want 2", report.TotalViolations)
	}
}
