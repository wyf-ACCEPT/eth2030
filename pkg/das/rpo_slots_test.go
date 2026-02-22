package das

import (
	"sync"
	"testing"
)

// --- RPOConfig defaults ---

func TestDefaultRPOConfig(t *testing.T) {
	cfg := DefaultRPOConfig()
	if cfg.InitialRPO != 4 {
		t.Fatalf("InitialRPO = %d, want 4", cfg.InitialRPO)
	}
	if cfg.MaxRPO != 64 {
		t.Fatalf("MaxRPO = %d, want 64", cfg.MaxRPO)
	}
	if cfg.MinRPO != 1 {
		t.Fatalf("MinRPO = %d, want 1", cfg.MinRPO)
	}
	if cfg.RPOStepSize != 8 {
		t.Fatalf("RPOStepSize = %d, want 8", cfg.RPOStepSize)
	}
}

// --- NewRPOManager ---

func TestNewRPOManagerDefaults(t *testing.T) {
	rm := NewRPOManager(RPOConfig{})
	if rm.CurrentRPO() < 1 {
		t.Fatalf("CurrentRPO = %d, want >= 1", rm.CurrentRPO())
	}
	if rm.config.MinRPO != 1 {
		t.Fatalf("MinRPO = %d, want 1", rm.config.MinRPO)
	}
	if rm.config.MaxRPO != 64 {
		t.Fatalf("MaxRPO = %d, want 64", rm.config.MaxRPO)
	}
}

func TestNewRPOManagerInitialClamped(t *testing.T) {
	// InitialRPO below MinRPO.
	rm := NewRPOManager(RPOConfig{
		InitialRPO: 0,
		MinRPO:     5,
		MaxRPO:     20,
	})
	if rm.CurrentRPO() != 5 {
		t.Fatalf("CurrentRPO = %d, want 5 (clamped to min)", rm.CurrentRPO())
	}

	// InitialRPO above MaxRPO.
	rm2 := NewRPOManager(RPOConfig{
		InitialRPO: 100,
		MinRPO:     5,
		MaxRPO:     20,
	})
	if rm2.CurrentRPO() != 20 {
		t.Fatalf("CurrentRPO = %d, want 20 (clamped to max)", rm2.CurrentRPO())
	}
}

func TestNewRPOManagerMaxBelowMin(t *testing.T) {
	rm := NewRPOManager(RPOConfig{
		MinRPO: 10,
		MaxRPO: 5, // below min
	})
	if rm.config.MaxRPO < rm.config.MinRPO {
		t.Fatalf("MaxRPO %d < MinRPO %d after normalization",
			rm.config.MaxRPO, rm.config.MinRPO)
	}
}

// --- CurrentRPO ---

func TestCurrentRPO(t *testing.T) {
	rm := NewRPOManager(DefaultRPOConfig())
	if rm.CurrentRPO() != 4 {
		t.Fatalf("CurrentRPO = %d, want 4", rm.CurrentRPO())
	}
}

// --- IncreaseRPO ---

func TestIncreaseRPOBasic(t *testing.T) {
	rm := NewRPOManager(DefaultRPOConfig())

	if err := rm.IncreaseRPO(8); err != nil {
		t.Fatalf("IncreaseRPO(8): %v", err)
	}
	if rm.CurrentRPO() != 8 {
		t.Fatalf("CurrentRPO = %d, want 8", rm.CurrentRPO())
	}
}

func TestIncreaseRPOMultipleSteps(t *testing.T) {
	rm := NewRPOManager(DefaultRPOConfig())

	steps := []uint64{8, 12, 16, 24, 32}
	for _, target := range steps {
		if err := rm.IncreaseRPO(target); err != nil {
			t.Fatalf("IncreaseRPO(%d): %v", target, err)
		}
	}
	if rm.CurrentRPO() != 32 {
		t.Fatalf("CurrentRPO = %d, want 32", rm.CurrentRPO())
	}
}

func TestIncreaseRPORecordsHistory(t *testing.T) {
	rm := NewRPOManager(DefaultRPOConfig())

	rm.IncreaseRPO(8)
	rm.IncreaseRPO(12)

	history := rm.GetHistory()
	if len(history) != 2 {
		t.Fatalf("history len = %d, want 2", len(history))
	}
	if history[0].OldRPO != 4 || history[0].NewRPO != 8 {
		t.Fatalf("history[0]: %d -> %d, want 4 -> 8", history[0].OldRPO, history[0].NewRPO)
	}
	if history[1].OldRPO != 8 || history[1].NewRPO != 12 {
		t.Fatalf("history[1]: %d -> %d, want 8 -> 12", history[1].OldRPO, history[1].NewRPO)
	}
}

func TestIncreaseRPOErrors(t *testing.T) {
	rm := NewRPOManager(DefaultRPOConfig())

	// Not increasing.
	if err := rm.IncreaseRPO(4); err == nil {
		t.Fatal("expected error for non-increasing RPO")
	}
	if err := rm.IncreaseRPO(2); err == nil {
		t.Fatal("expected error for decreasing RPO")
	}

	// Step too large (from 4, step > 8).
	if err := rm.IncreaseRPO(13); err == nil {
		t.Fatal("expected error for step too large")
	}

	// Above max.
	rm2 := NewRPOManager(RPOConfig{
		InitialRPO:  60,
		MinRPO:      1,
		MaxRPO:      64,
		RPOStepSize: 8,
	})
	if err := rm2.IncreaseRPO(65); err == nil {
		t.Fatal("expected error for above max RPO")
	}
}

// --- ValidateRPOTransition ---

func TestValidateRPOTransition(t *testing.T) {
	rm := NewRPOManager(DefaultRPOConfig())

	// Valid transition.
	if err := rm.ValidateRPOTransition(4, 8); err != nil {
		t.Fatalf("ValidateRPOTransition(4, 8): %v", err)
	}

	// Below min.
	rm2 := NewRPOManager(RPOConfig{MinRPO: 5, MaxRPO: 64, RPOStepSize: 8})
	if err := rm2.ValidateRPOTransition(4, 3); err == nil {
		t.Fatal("expected error for target below min")
	}

	// Above max.
	if err := rm.ValidateRPOTransition(60, 100); err == nil {
		t.Fatal("expected error for target above max")
	}

	// Not increasing (equal).
	if err := rm.ValidateRPOTransition(10, 10); err == nil {
		t.Fatal("expected error for equal RPO")
	}

	// Not increasing (decreasing).
	if err := rm.ValidateRPOTransition(10, 5); err == nil {
		t.Fatal("expected error for decreasing RPO")
	}

	// Step too large.
	if err := rm.ValidateRPOTransition(4, 20); err == nil {
		t.Fatal("expected error for step too large")
	}
}

// --- CalculateThroughput ---

func TestCalculateThroughput(t *testing.T) {
	rm := NewRPOManager(DefaultRPOConfig())

	// Basic fields are non-zero.
	est := rm.CalculateThroughput(4)
	if est.BlobsPerSlot == 0 || est.DataRateKBps == 0 ||
		est.SamplesNeeded == 0 || est.ValidationTimeMs == 0 {
		t.Fatal("all fields should be non-zero for RPO=4")
	}

	// Higher RPO should scale up.
	est8 := rm.CalculateThroughput(8)
	if est8.BlobsPerSlot < est.BlobsPerSlot || est8.DataRateKBps < est.DataRateKBps {
		t.Fatal("higher RPO should have higher throughput")
	}

	// BlobsPerSlot capped at MaxBlobCommitmentsPerBlock.
	rm2 := NewRPOManager(RPOConfig{InitialRPO: 1, MaxRPO: 1000, MinRPO: 1, RPOStepSize: 1000})
	if est100 := rm2.CalculateThroughput(100); est100.BlobsPerSlot > uint64(MaxBlobCommitmentsPerBlock) {
		t.Fatalf("BlobsPerSlot %d > max %d", est100.BlobsPerSlot, MaxBlobCommitmentsPerBlock)
	}

	// Zero RPO treated as 1.
	if est0 := rm.CalculateThroughput(0); est0.BlobsPerSlot == 0 {
		t.Fatal("zero RPO should be treated as 1")
	}
}

// --- isqrt ---

func TestIsqrt(t *testing.T) {
	cases := []struct {
		n    uint64
		want uint64
	}{
		{0, 0},
		{1, 1},
		{4, 2},
		{9, 3},
		{10, 3},
		{15, 3},
		{16, 4},
		{100, 10},
		{255, 15},
	}
	for _, tc := range cases {
		got := isqrt(tc.n)
		if got != tc.want {
			t.Errorf("isqrt(%d) = %d, want %d", tc.n, got, tc.want)
		}
	}
}

// --- SetSchedule ---

func TestSetScheduleValid(t *testing.T) {
	rm := NewRPOManager(DefaultRPOConfig())

	schedule := []*RPOSchedule{
		{Epoch: 100, TargetRPO: 8, Description: "first increase"},
		{Epoch: 200, TargetRPO: 16, Description: "second increase"},
		{Epoch: 300, TargetRPO: 32, Description: "third increase"},
	}
	if err := rm.SetSchedule(schedule); err != nil {
		t.Fatalf("SetSchedule: %v", err)
	}
}

func TestSetScheduleErrors(t *testing.T) {
	rm := NewRPOManager(DefaultRPOConfig())

	// Empty schedule.
	if err := rm.SetSchedule(nil); err != ErrRPOScheduleEmpty {
		t.Fatalf("nil: got %v, want ErrRPOScheduleEmpty", err)
	}
	if err := rm.SetSchedule([]*RPOSchedule{}); err != ErrRPOScheduleEmpty {
		t.Fatalf("empty: got %v, want ErrRPOScheduleEmpty", err)
	}

	// Same epoch (non-increasing).
	if err := rm.SetSchedule([]*RPOSchedule{
		{Epoch: 100, TargetRPO: 8}, {Epoch: 100, TargetRPO: 16},
	}); err == nil {
		t.Fatal("expected error for same epoch")
	}

	// Decreasing epoch.
	if err := rm.SetSchedule([]*RPOSchedule{
		{Epoch: 200, TargetRPO: 8}, {Epoch: 100, TargetRPO: 16},
	}); err == nil {
		t.Fatal("expected error for decreasing epochs")
	}

	// Decreasing RPO.
	if err := rm.SetSchedule([]*RPOSchedule{
		{Epoch: 100, TargetRPO: 16}, {Epoch: 200, TargetRPO: 8},
	}); err == nil {
		t.Fatal("expected error for decreasing RPO")
	}

	// Out of bounds.
	rm2 := NewRPOManager(RPOConfig{InitialRPO: 4, MinRPO: 4, MaxRPO: 32, RPOStepSize: 8})
	if err := rm2.SetSchedule([]*RPOSchedule{{Epoch: 100, TargetRPO: 2}}); err == nil {
		t.Fatal("expected error for RPO below min")
	}
	if err := rm2.SetSchedule([]*RPOSchedule{{Epoch: 100, TargetRPO: 64}}); err == nil {
		t.Fatal("expected error for RPO above max")
	}
}

// --- GetScheduledRPO ---

func TestGetScheduledRPO(t *testing.T) {
	rm := NewRPOManager(DefaultRPOConfig())

	// No schedule: returns current RPO.
	if got := rm.GetScheduledRPO(100); got != rm.CurrentRPO() {
		t.Fatalf("no schedule: got %d, want current %d", got, rm.CurrentRPO())
	}

	rm.SetSchedule([]*RPOSchedule{
		{Epoch: 100, TargetRPO: 8},
		{Epoch: 200, TargetRPO: 16},
		{Epoch: 300, TargetRPO: 32},
	})

	tests := []struct {
		epoch uint64
		want  uint64
	}{
		{50, 4},   // before first entry -> current RPO
		{100, 8},  // exact match
		{150, 8},  // between entries
		{200, 16}, // exact match
		{250, 16}, // between entries
		{300, 32}, // exact match
		{500, 32}, // after last entry
	}
	for _, tt := range tests {
		if got := rm.GetScheduledRPO(tt.epoch); got != tt.want {
			t.Errorf("epoch %d: got %d, want %d", tt.epoch, got, tt.want)
		}
	}
}

// --- GetHistory ---

func TestGetHistoryEmpty(t *testing.T) {
	rm := NewRPOManager(DefaultRPOConfig())
	history := rm.GetHistory()
	if len(history) != 0 {
		t.Fatalf("initial history len = %d, want 0", len(history))
	}
}

func TestGetHistoryIsACopy(t *testing.T) {
	rm := NewRPOManager(DefaultRPOConfig())
	rm.IncreaseRPO(8)

	history := rm.GetHistory()
	if len(history) != 1 {
		t.Fatalf("history len = %d, want 1", len(history))
	}

	// Mutate the returned slice; should not affect the manager.
	history[0].NewRPO = 999
	fresh := rm.GetHistory()
	if fresh[0].NewRPO == 999 {
		t.Fatal("GetHistory should return a copy, not a reference")
	}
}

// --- Concurrency ---

func TestRPOManagerConcurrency(t *testing.T) {
	rm := NewRPOManager(RPOConfig{
		InitialRPO:  1,
		MaxRPO:      10000,
		MinRPO:      1,
		RPOStepSize: 10000,
	})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = rm.CurrentRPO()
			_ = rm.CalculateThroughput(uint64(idx + 1))
			_ = rm.GetScheduledRPO(uint64(idx * 10))
			_ = rm.GetHistory()
		}(i)
	}
	wg.Wait()
}

func TestRPOManagerConcurrentIncrease(t *testing.T) {
	// Only one goroutine can successfully increase at a time due to
	// the strictly-increasing constraint, but we should not panic.
	rm := NewRPOManager(RPOConfig{
		InitialRPO:  1,
		MaxRPO:      1000,
		MinRPO:      1,
		RPOStepSize: 1000,
	})

	var wg sync.WaitGroup
	for i := 2; i <= 50; i++ {
		wg.Add(1)
		go func(target uint64) {
			defer wg.Done()
			_ = rm.IncreaseRPO(target)
		}(uint64(i))
	}
	wg.Wait()

	// Final RPO should be somewhere in [2, 50].
	final := rm.CurrentRPO()
	if final < 2 || final > 50 {
		t.Fatalf("final RPO %d out of expected range [2, 50]", final)
	}
}

// --- Integration: schedule + lookup ---

func TestRPOScheduleIntegration(t *testing.T) {
	rm := NewRPOManager(RPOConfig{
		InitialRPO:  4,
		MaxRPO:      64,
		MinRPO:      1,
		RPOStepSize: 16,
	})

	schedule := []*RPOSchedule{
		{Epoch: 10, TargetRPO: 8, Description: "Hogota"},
		{Epoch: 50, TargetRPO: 16, Description: "I+ fork"},
		{Epoch: 100, TargetRPO: 32, Description: "J+ fork"},
		{Epoch: 200, TargetRPO: 64, Description: "K+ fork"},
	}
	if err := rm.SetSchedule(schedule); err != nil {
		t.Fatalf("SetSchedule: %v", err)
	}

	// Walk through epochs.
	tests := []struct {
		epoch   uint64
		wantRPO uint64
	}{
		{0, 4},    // before schedule
		{5, 4},    // before first entry
		{10, 8},   // exact
		{25, 8},   // between
		{50, 16},  // exact
		{75, 16},  // between
		{100, 32}, // exact
		{150, 32}, // between
		{200, 64}, // exact
		{999, 64}, // after all
	}
	for _, tt := range tests {
		got := rm.GetScheduledRPO(tt.epoch)
		if got != tt.wantRPO {
			t.Errorf("epoch %d: got RPO %d, want %d", tt.epoch, got, tt.wantRPO)
		}
	}

	// Verify throughput grows.
	est4 := rm.CalculateThroughput(4)
	est64 := rm.CalculateThroughput(64)
	if est64.DataRateKBps <= est4.DataRateKBps {
		t.Fatalf("RPO 64 should have higher throughput than RPO 4: %d vs %d",
			est64.DataRateKBps, est4.DataRateKBps)
	}
}

// --- SetSchedule immutability ---

func TestSetScheduleImmutability(t *testing.T) {
	rm := NewRPOManager(DefaultRPOConfig())
	schedule := []*RPOSchedule{
		{Epoch: 100, TargetRPO: 8},
	}
	rm.SetSchedule(schedule)

	// Mutate the original slice.
	schedule[0].TargetRPO = 999

	// Internal schedule should be unaffected.
	got := rm.GetScheduledRPO(100)
	if got == 999 {
		t.Fatal("SetSchedule should copy entries to prevent external mutation")
	}
}

func TestBPO3Schedule(t *testing.T) {
	sched := BPO3Schedule()
	if len(sched) != 2 {
		t.Fatalf("BPO3Schedule length = %d, want 2", len(sched))
	}
	if sched[0].TargetRPO != 32 {
		t.Errorf("BPO3[0].TargetRPO = %d, want 32", sched[0].TargetRPO)
	}
	if sched[1].TargetRPO != 48 {
		t.Errorf("BPO3[1].TargetRPO = %d, want 48", sched[1].TargetRPO)
	}
	// Epochs should be increasing.
	if sched[1].Epoch <= sched[0].Epoch {
		t.Error("BPO3 epochs must be increasing")
	}
}

func TestBPO4Schedule(t *testing.T) {
	sched := BPO4Schedule()
	if len(sched) != 2 {
		t.Fatalf("BPO4Schedule length = %d, want 2", len(sched))
	}
	if sched[0].TargetRPO != 48 {
		t.Errorf("BPO4[0].TargetRPO = %d, want 48", sched[0].TargetRPO)
	}
	if sched[1].TargetRPO != 64 {
		t.Errorf("BPO4[1].TargetRPO = %d, want 64", sched[1].TargetRPO)
	}
}

func TestMergeBPOSchedules(t *testing.T) {
	t.Run("merge BPO3 and BPO4", func(t *testing.T) {
		merged, err := MergeBPOSchedules(BPO3Schedule(), BPO4Schedule())
		if err != nil {
			t.Fatalf("MergeBPOSchedules: %v", err)
		}
		if len(merged) != 4 {
			t.Fatalf("merged length = %d, want 4", len(merged))
		}
		// Verify monotonicity.
		for i := 1; i < len(merged); i++ {
			if merged[i].Epoch <= merged[i-1].Epoch {
				t.Errorf("epoch[%d]=%d <= epoch[%d]=%d", i, merged[i].Epoch, i-1, merged[i-1].Epoch)
			}
			if merged[i].TargetRPO < merged[i-1].TargetRPO {
				t.Errorf("RPO[%d]=%d < RPO[%d]=%d", i, merged[i].TargetRPO, i-1, merged[i-1].TargetRPO)
			}
		}
	})

	t.Run("empty merge", func(t *testing.T) {
		_, err := MergeBPOSchedules()
		if err != ErrRPOScheduleEmpty {
			t.Fatalf("expected ErrRPOScheduleEmpty, got %v", err)
		}
	})
}

func TestValidateBlobSchedule(t *testing.T) {
	cfg := DefaultRPOConfig()

	// Valid schedule.
	sched := []*RPOSchedule{
		{Epoch: 10, TargetRPO: 4},
		{Epoch: 20, TargetRPO: 8},
	}
	if err := ValidateBlobSchedule(sched, cfg); err != nil {
		t.Errorf("valid schedule: %v", err)
	}

	// Empty schedule.
	if err := ValidateBlobSchedule(nil, cfg); err == nil {
		t.Error("expected error for empty schedule")
	}

	// Non-increasing epochs.
	bad := []*RPOSchedule{
		{Epoch: 20, TargetRPO: 4},
		{Epoch: 10, TargetRPO: 8},
	}
	if err := ValidateBlobSchedule(bad, cfg); err == nil {
		t.Error("expected error for non-increasing epochs")
	}

	// RPO above max.
	overMax := []*RPOSchedule{
		{Epoch: 10, TargetRPO: 1000},
	}
	if err := ValidateBlobSchedule(overMax, cfg); err == nil {
		t.Error("expected error for RPO above max")
	}
}
