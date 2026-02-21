package das

import (
	"testing"
)

func schedTestNodeID(seed byte) [32]byte {
	var id [32]byte
	id[0] = seed
	id[31] = seed ^ 0xFF
	return id
}

func TestSamplingSchedulerStartRoundBasic(t *testing.T) {
	ss := NewSamplingScheduler(DefaultSchedulerConfig(), schedTestNodeID(1))

	round, err := ss.StartRound(100, RegularSampling)
	if err != nil {
		t.Fatalf("StartRound: %v", err)
	}
	if round.Slot != 100 {
		t.Errorf("Slot = %d, want 100", round.Slot)
	}
	if round.Mode != RegularSampling {
		t.Error("Mode should be RegularSampling")
	}
	if len(round.TargetColumns) == 0 {
		t.Error("TargetColumns should not be empty")
	}
	if round.Quota <= 0 {
		t.Errorf("Quota = %d, should be > 0", round.Quota)
	}
	if round.Complete {
		t.Error("round should not be complete initially")
	}
}

func TestSamplingSchedulerStartRoundSlotZero(t *testing.T) {
	ss := NewSamplingScheduler(DefaultSchedulerConfig(), schedTestNodeID(2))
	_, err := ss.StartRound(0, RegularSampling)
	if err != ErrSchedSlotZero {
		t.Fatalf("expected ErrSchedSlotZero, got %v", err)
	}
}

func TestSamplingSchedulerStartRoundIdempotent(t *testing.T) {
	ss := NewSamplingScheduler(DefaultSchedulerConfig(), schedTestNodeID(3))

	r1, _ := ss.StartRound(50, RegularSampling)
	r2, _ := ss.StartRound(50, RegularSampling)
	if r1.Slot != r2.Slot {
		t.Error("idempotent StartRound should return same round")
	}
}

func TestSamplingSchedulerExtendedMode(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	cfg.BaseSamplesPerSlot = 8
	ss := NewSamplingScheduler(cfg, schedTestNodeID(4))

	regular, _ := ss.StartRound(1, RegularSampling)
	ss2 := NewSamplingScheduler(cfg, schedTestNodeID(4))
	extended, _ := ss2.StartRound(1, ExtendedSampling)

	if len(extended.TargetColumns) <= len(regular.TargetColumns) {
		t.Errorf("ExtendedSampling should have more targets: %d vs %d",
			len(extended.TargetColumns), len(regular.TargetColumns))
	}
}

func TestSamplingSchedulerRecordSample(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	cfg.BaseSamplesPerSlot = 4
	cfg.NumberOfColumns = 16
	ss := NewSamplingScheduler(cfg, schedTestNodeID(5))

	round, _ := ss.StartRound(10, RegularSampling)
	target := round.TargetColumns[0]

	err := ss.RecordSample(10, target, true)
	if err != nil {
		t.Fatalf("RecordSample: %v", err)
	}

	r := ss.GetRound(10)
	if !r.SampledColumns[target] {
		t.Error("target should be marked as sampled")
	}
	if !r.SuccessColumns[target] {
		t.Error("target should be marked as success")
	}
}

func TestSamplingSchedulerRecordSampleFailure(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	cfg.BaseSamplesPerSlot = 4
	cfg.NumberOfColumns = 16
	ss := NewSamplingScheduler(cfg, schedTestNodeID(6))

	round, _ := ss.StartRound(20, RegularSampling)
	target := round.TargetColumns[0]

	_ = ss.RecordSample(20, target, false)

	r := ss.GetRound(20)
	if !r.FailedColumns[target] {
		t.Error("target should be marked as failed")
	}
	if r.SuccessColumns[target] {
		t.Error("target should not be marked as success")
	}
}

func TestSamplingSchedulerRecordSampleNoRound(t *testing.T) {
	ss := NewSamplingScheduler(DefaultSchedulerConfig(), schedTestNodeID(7))
	err := ss.RecordSample(999, 0, true)
	if err != ErrSchedNoActiveRound {
		t.Fatalf("expected ErrSchedNoActiveRound, got %v", err)
	}
}

func TestSamplingSchedulerRecordSampleOOB(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	cfg.NumberOfColumns = 16
	ss := NewSamplingScheduler(cfg, schedTestNodeID(8))
	_, _ = ss.StartRound(1, RegularSampling)

	err := ss.RecordSample(1, ColumnIndex(20), true)
	if err == nil {
		t.Fatal("expected error for column OOB")
	}
}

func TestSamplingSchedulerRoundCompletion(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	cfg.BaseSamplesPerSlot = 4
	cfg.NumberOfColumns = 16
	ss := NewSamplingScheduler(cfg, schedTestNodeID(9))

	round, _ := ss.StartRound(30, RegularSampling)

	// Sample all target columns.
	for _, col := range round.TargetColumns {
		_ = ss.RecordSample(30, col, true)
	}

	if !ss.IsRoundComplete(30) {
		t.Error("round should be complete after sampling all targets")
	}
}

func TestSamplingSchedulerCompleteRound(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	cfg.BaseSamplesPerSlot = 4
	ss := NewSamplingScheduler(cfg, schedTestNodeID(10))

	_, _ = ss.StartRound(40, RegularSampling)

	err := ss.CompleteRound(40)
	if err != nil {
		t.Fatalf("CompleteRound: %v", err)
	}
	if !ss.IsRoundComplete(40) {
		t.Error("round should be complete")
	}

	// Completing again should be a no-op.
	err = ss.CompleteRound(40)
	if err != nil {
		t.Fatalf("second CompleteRound: %v", err)
	}
}

func TestSamplingSchedulerCompleteRoundNoRound(t *testing.T) {
	ss := NewSamplingScheduler(DefaultSchedulerConfig(), schedTestNodeID(11))
	err := ss.CompleteRound(999)
	if err != ErrSchedNoActiveRound {
		t.Fatalf("expected ErrSchedNoActiveRound, got %v", err)
	}
}

func TestSamplingSchedulerRemainingQuota(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	cfg.BaseSamplesPerSlot = 4
	cfg.NumberOfColumns = 16
	ss := NewSamplingScheduler(cfg, schedTestNodeID(12))

	round, _ := ss.StartRound(50, RegularSampling)
	initialQuota := ss.RemainingQuota(50)
	if initialQuota <= 0 {
		t.Fatal("initial quota should be > 0")
	}

	_ = ss.RecordSample(50, round.TargetColumns[0], true)
	if ss.RemainingQuota(50) >= initialQuota {
		t.Error("quota should decrease after sample")
	}
}

func TestSamplingSchedulerRoundSuccessRate(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	cfg.BaseSamplesPerSlot = 4
	cfg.NumberOfColumns = 16
	ss := NewSamplingScheduler(cfg, schedTestNodeID(13))

	round, _ := ss.StartRound(60, RegularSampling)

	// Record 2 successes and 2 failures.
	if len(round.TargetColumns) >= 4 {
		_ = ss.RecordSample(60, round.TargetColumns[0], true)
		_ = ss.RecordSample(60, round.TargetColumns[1], true)
		_ = ss.RecordSample(60, round.TargetColumns[2], false)
		_ = ss.RecordSample(60, round.TargetColumns[3], false)

		rate := ss.RoundSuccessRate(60)
		if rate < 0.49 || rate > 0.51 {
			t.Errorf("SuccessRate = %f, want ~0.5", rate)
		}
	}
}

func TestSamplingSchedulerUnsampledColumns(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	cfg.BaseSamplesPerSlot = 4
	cfg.NumberOfColumns = 16
	ss := NewSamplingScheduler(cfg, schedTestNodeID(14))

	round, _ := ss.StartRound(70, RegularSampling)
	unsampled := ss.UnsampledColumns(70)
	if len(unsampled) != len(round.TargetColumns) {
		t.Errorf("initially %d unsampled, want %d", len(unsampled), len(round.TargetColumns))
	}

	_ = ss.RecordSample(70, round.TargetColumns[0], true)
	unsampled = ss.UnsampledColumns(70)
	if len(unsampled) != len(round.TargetColumns)-1 {
		t.Errorf("after 1 sample: %d unsampled, want %d", len(unsampled), len(round.TargetColumns)-1)
	}
}

func TestSamplingSchedulerGetStats(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	cfg.BaseSamplesPerSlot = 4
	cfg.NumberOfColumns = 16
	ss := NewSamplingScheduler(cfg, schedTestNodeID(15))

	r1, _ := ss.StartRound(80, RegularSampling)
	for _, col := range r1.TargetColumns {
		_ = ss.RecordSample(80, col, true)
	}

	stats := ss.GetStats()
	if stats.TotalRounds != 1 {
		t.Errorf("TotalRounds = %d, want 1", stats.TotalRounds)
	}
	if stats.CompletedRounds != 1 {
		t.Errorf("CompletedRounds = %d, want 1", stats.CompletedRounds)
	}
	if stats.TotalSamples == 0 {
		t.Error("TotalSamples should be > 0")
	}
	if stats.SuccessRate != 1.0 {
		t.Errorf("SuccessRate = %f, want 1.0", stats.SuccessRate)
	}
}

func TestSamplingSchedulerAdaptiveRateDecrease(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	cfg.BaseSamplesPerSlot = 4
	cfg.NumberOfColumns = 16
	cfg.HighSuccessThreshold = 0.9
	ss := NewSamplingScheduler(cfg, schedTestNodeID(16))

	// Complete several all-success rounds to trigger rate decrease.
	for slot := uint64(1); slot <= 5; slot++ {
		round, _ := ss.StartRound(slot, RegularSampling)
		for _, col := range round.TargetColumns {
			_ = ss.RecordSample(slot, col, true)
		}
	}

	rate := ss.AdaptiveRate()
	if rate >= 1.0 {
		t.Errorf("AdaptiveRate = %f, should decrease below 1.0 after high success", rate)
	}
}

func TestSamplingSchedulerAdaptiveRateIncrease(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	cfg.BaseSamplesPerSlot = 4
	cfg.NumberOfColumns = 16
	cfg.SuccessRateThreshold = 0.8
	ss := NewSamplingScheduler(cfg, schedTestNodeID(17))

	// Complete several low-success rounds.
	for slot := uint64(1); slot <= 5; slot++ {
		round, _ := ss.StartRound(slot, RegularSampling)
		// Only the first target succeeds, rest fail.
		for i, col := range round.TargetColumns {
			_ = ss.RecordSample(slot, col, i == 0)
		}
	}

	rate := ss.AdaptiveRate()
	if rate <= 1.0 {
		t.Errorf("AdaptiveRate = %f, should increase above 1.0 after low success", rate)
	}
}

func TestSamplingSchedulerSetAdaptiveRate(t *testing.T) {
	ss := NewSamplingScheduler(DefaultSchedulerConfig(), schedTestNodeID(18))

	ss.SetAdaptiveRate(2.5)
	if ss.AdaptiveRate() != 2.5 {
		t.Errorf("AdaptiveRate = %f, want 2.5", ss.AdaptiveRate())
	}

	// Clamp to min.
	ss.SetAdaptiveRate(0.01)
	if ss.AdaptiveRate() < ss.config.AdaptiveMinRate {
		t.Error("rate should be clamped to min")
	}

	// Clamp to max.
	ss.SetAdaptiveRate(100.0)
	if ss.AdaptiveRate() > ss.config.AdaptiveMaxRate {
		t.Error("rate should be clamped to max")
	}
}

func TestSamplingSchedulerIsCustodyColumn(t *testing.T) {
	ss := NewSamplingScheduler(DefaultSchedulerConfig(), schedTestNodeID(19))

	// The node should have some custody columns.
	hasCustody := false
	for col := ColumnIndex(0); col < ColumnIndex(NumberOfColumns); col++ {
		if ss.IsCustodyColumn(col) {
			hasCustody = true
			break
		}
	}
	if !hasCustody {
		t.Error("expected at least one custody column")
	}
}

func TestSamplingSchedulerActiveRoundCount(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	cfg.BaseSamplesPerSlot = 4
	cfg.NumberOfColumns = 16
	ss := NewSamplingScheduler(cfg, schedTestNodeID(20))

	_, _ = ss.StartRound(1, RegularSampling)
	_, _ = ss.StartRound(2, RegularSampling)
	if ss.ActiveRoundCount() != 2 {
		t.Errorf("ActiveRoundCount = %d, want 2", ss.ActiveRoundCount())
	}

	_ = ss.CompleteRound(1)
	if ss.ActiveRoundCount() != 1 {
		t.Errorf("ActiveRoundCount = %d, want 1", ss.ActiveRoundCount())
	}
}

func TestSamplingSchedulerPruneCompleted(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	cfg.BaseSamplesPerSlot = 4
	cfg.NumberOfColumns = 16
	ss := NewSamplingScheduler(cfg, schedTestNodeID(21))

	_, _ = ss.StartRound(1, RegularSampling)
	_, _ = ss.StartRound(2, RegularSampling)
	_ = ss.CompleteRound(1)

	pruned := ss.PruneCompleted()
	if pruned != 1 {
		t.Errorf("pruned = %d, want 1", pruned)
	}

	if ss.GetRound(1) != nil {
		t.Error("round 1 should be pruned")
	}
	if ss.GetRound(2) == nil {
		t.Error("round 2 should still exist")
	}
}

func TestSamplingSchedulerClose(t *testing.T) {
	ss := NewSamplingScheduler(DefaultSchedulerConfig(), schedTestNodeID(22))
	ss.Close()

	_, err := ss.StartRound(1, RegularSampling)
	if err != ErrSchedClosed {
		t.Fatalf("expected ErrSchedClosed, got %v", err)
	}
}

func TestSamplingSchedulerEvictOldRounds(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	cfg.MaxConcurrentSlots = 3
	cfg.BaseSamplesPerSlot = 4
	cfg.NumberOfColumns = 16
	ss := NewSamplingScheduler(cfg, schedTestNodeID(23))

	for i := uint64(1); i <= 5; i++ {
		_, _ = ss.StartRound(i, RegularSampling)
	}

	stats := ss.GetStats()
	if stats.TotalRounds > 3 {
		t.Errorf("TotalRounds = %d, should be <= 3 after eviction", stats.TotalRounds)
	}
}

func TestSamplingSchedulerDeterministicColumns(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	nodeID := schedTestNodeID(24)

	ss1 := NewSamplingScheduler(cfg, nodeID)
	ss2 := NewSamplingScheduler(cfg, nodeID)

	r1, _ := ss1.StartRound(100, RegularSampling)
	r2, _ := ss2.StartRound(100, RegularSampling)

	if len(r1.TargetColumns) != len(r2.TargetColumns) {
		t.Fatal("target column counts should match for same nodeID and slot")
	}
	for i := range r1.TargetColumns {
		if r1.TargetColumns[i] != r2.TargetColumns[i] {
			t.Errorf("TargetColumns[%d] mismatch: %d vs %d", i, r1.TargetColumns[i], r2.TargetColumns[i])
		}
	}
}

func TestSamplingSchedulerDefaultConfig(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	if cfg.BaseSamplesPerSlot != SamplesPerSlot {
		t.Errorf("BaseSamplesPerSlot = %d, want %d", cfg.BaseSamplesPerSlot, SamplesPerSlot)
	}
	if cfg.NumberOfColumns != NumberOfColumns {
		t.Errorf("NumberOfColumns = %d, want %d", cfg.NumberOfColumns, NumberOfColumns)
	}
	if cfg.AdaptiveMinRate <= 0 {
		t.Error("AdaptiveMinRate should be > 0")
	}
	if cfg.AdaptiveMaxRate <= cfg.AdaptiveMinRate {
		t.Error("AdaptiveMaxRate should be > AdaptiveMinRate")
	}
}

func TestSamplingSchedulerRecordAfterComplete(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	cfg.BaseSamplesPerSlot = 4
	cfg.NumberOfColumns = 16
	ss := NewSamplingScheduler(cfg, schedTestNodeID(25))

	_, _ = ss.StartRound(90, RegularSampling)
	_ = ss.CompleteRound(90)

	err := ss.RecordSample(90, 0, true)
	if err != ErrSchedRoundComplete {
		t.Fatalf("expected ErrSchedRoundComplete, got %v", err)
	}
}
