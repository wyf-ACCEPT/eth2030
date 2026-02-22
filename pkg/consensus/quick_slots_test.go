package consensus

import (
	"testing"
	"time"
)

func TestQuickSlotConfig(t *testing.T) {
	cfg := DefaultQuickSlotConfig()

	if cfg.SlotDuration != 6*time.Second {
		t.Errorf("SlotDuration = %v, want 6s", cfg.SlotDuration)
	}
	if cfg.SlotsPerEpoch != 4 {
		t.Errorf("SlotsPerEpoch = %d, want 4", cfg.SlotsPerEpoch)
	}
	if cfg.EpochDuration() != 24*time.Second {
		t.Errorf("EpochDuration = %v, want 24s", cfg.EpochDuration())
	}
}

func TestCurrentSlot(t *testing.T) {
	cfg := DefaultQuickSlotConfig()
	genesis := time.Now().Add(-30 * time.Second) // 30 seconds ago
	sched := NewQuickSlotScheduler(cfg, genesis)

	// 30s / 6s = slot 5
	slot := sched.CurrentSlot()
	if slot != 5 {
		t.Errorf("CurrentSlot = %d, want 5 (30s elapsed, 6s slots)", slot)
	}
}

func TestCurrentSlotBeforeGenesis(t *testing.T) {
	cfg := DefaultQuickSlotConfig()
	genesis := time.Now().Add(10 * time.Second) // 10 seconds in the future
	sched := NewQuickSlotScheduler(cfg, genesis)

	slot := sched.CurrentSlot()
	if slot != 0 {
		t.Errorf("CurrentSlot before genesis = %d, want 0", slot)
	}
}

func TestSlotAt(t *testing.T) {
	cfg := DefaultQuickSlotConfig()
	genesis := time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC)
	sched := NewQuickSlotScheduler(cfg, genesis)

	tests := []struct {
		name   string
		offset time.Duration
		want   uint64
	}{
		{"at genesis", 0, 0},
		{"1s after genesis", 1 * time.Second, 0},
		{"5s after genesis", 5 * time.Second, 0},
		{"6s after genesis (slot 1)", 6 * time.Second, 1},
		{"11s after genesis", 11 * time.Second, 1},
		{"12s after genesis (slot 2)", 12 * time.Second, 2},
		{"23s after genesis", 23 * time.Second, 3},
		{"24s after genesis (slot 4, epoch 1)", 24 * time.Second, 4},
		{"48s after genesis (slot 8, epoch 2)", 48 * time.Second, 8},
		{"before genesis", -1 * time.Second, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sched.SlotAt(genesis.Add(tt.offset))
			if got != tt.want {
				t.Errorf("SlotAt(genesis+%v) = %d, want %d", tt.offset, got, tt.want)
			}
		})
	}
}

func TestQuickSlotToEpoch(t *testing.T) {
	cfg := DefaultQuickSlotConfig() // 4 slots per epoch
	genesis := time.Now()
	sched := NewQuickSlotScheduler(cfg, genesis)

	tests := []struct {
		slot uint64
		want uint64
	}{
		{0, 0},
		{1, 0},
		{2, 0},
		{3, 0},
		{4, 1}, // epoch 1 starts at slot 4
		{5, 1},
		{7, 1},
		{8, 2}, // epoch 2 starts at slot 8
		{15, 3},
		{16, 4},
		{100, 25},
	}

	for _, tt := range tests {
		got := sched.SlotToEpoch(tt.slot)
		if got != tt.want {
			t.Errorf("SlotToEpoch(%d) = %d, want %d", tt.slot, got, tt.want)
		}
	}
}

func TestQuickEpochStartSlot(t *testing.T) {
	cfg := DefaultQuickSlotConfig() // 4 slots per epoch
	genesis := time.Now()
	sched := NewQuickSlotScheduler(cfg, genesis)

	tests := []struct {
		epoch uint64
		want  uint64
	}{
		{0, 0},
		{1, 4},
		{2, 8},
		{3, 12},
		{10, 40},
		{25, 100},
	}

	for _, tt := range tests {
		got := sched.EpochStartSlot(tt.epoch)
		if got != tt.want {
			t.Errorf("EpochStartSlot(%d) = %d, want %d", tt.epoch, got, tt.want)
		}
	}
}

func TestIsFirstSlotOfEpoch(t *testing.T) {
	cfg := DefaultQuickSlotConfig() // 4 slots per epoch
	genesis := time.Now()
	sched := NewQuickSlotScheduler(cfg, genesis)

	tests := []struct {
		slot uint64
		want bool
	}{
		{0, true}, // epoch 0, slot 0
		{1, false},
		{2, false},
		{3, false},
		{4, true}, // epoch 1, slot 0
		{5, false},
		{8, true},  // epoch 2, slot 0
		{12, true}, // epoch 3, slot 0
		{13, false},
		{100, true}, // epoch 25, slot 0
		{101, false},
	}

	for _, tt := range tests {
		got := sched.IsFirstSlotOfEpoch(tt.slot)
		if got != tt.want {
			t.Errorf("IsFirstSlotOfEpoch(%d) = %v, want %v", tt.slot, got, tt.want)
		}
	}
}

func TestNextSlotTime(t *testing.T) {
	cfg := DefaultQuickSlotConfig()
	// Set genesis to a known time 10 seconds ago.
	genesis := time.Now().Add(-10 * time.Second)
	sched := NewQuickSlotScheduler(cfg, genesis)

	nextSlot := sched.NextSlotTime()
	now := time.Now()

	// Next slot should be in the future.
	if !nextSlot.After(now) {
		t.Errorf("NextSlotTime = %v should be after now %v", nextSlot, now)
	}

	// Next slot should be at most one slot duration away.
	maxWait := cfg.SlotDuration
	diff := nextSlot.Sub(now)
	if diff > maxWait {
		t.Errorf("NextSlotTime is %v away, should be at most %v", diff, maxWait)
	}
}

func TestNextSlotTimeBeforeGenesis(t *testing.T) {
	cfg := DefaultQuickSlotConfig()
	genesis := time.Now().Add(10 * time.Second) // 10 seconds in the future
	sched := NewQuickSlotScheduler(cfg, genesis)

	nextSlot := sched.NextSlotTime()
	// Before genesis, next slot time should be genesis.
	if !nextSlot.Equal(genesis) {
		t.Errorf("NextSlotTime before genesis = %v, want genesis %v", nextSlot, genesis)
	}
}

func TestGetDuties(t *testing.T) {
	cfg := DefaultQuickSlotConfig() // 4 slots per epoch
	genesis := time.Now()
	sched := NewQuickSlotScheduler(cfg, genesis)

	// With 16 validators and 4 slots per epoch, each slot gets 4 validators.
	duties := sched.GetDuties(0, 16)
	if duties.ProposerIndex != 0 {
		t.Errorf("slot 0: ProposerIndex = %d, want 0", duties.ProposerIndex)
	}
	if len(duties.CommitteeIndices) != 4 {
		t.Errorf("slot 0: committee size = %d, want 4", len(duties.CommitteeIndices))
	}
	// Slot 0 should get validators [0, 1, 2, 3].
	for i, idx := range duties.CommitteeIndices {
		if idx != uint64(i) {
			t.Errorf("slot 0: committee[%d] = %d, want %d", i, idx, i)
		}
	}

	// Slot 1 should get validators [4, 5, 6, 7].
	duties = sched.GetDuties(1, 16)
	if duties.ProposerIndex != 1 {
		t.Errorf("slot 1: ProposerIndex = %d, want 1", duties.ProposerIndex)
	}
	if len(duties.CommitteeIndices) != 4 {
		t.Errorf("slot 1: committee size = %d, want 4", len(duties.CommitteeIndices))
	}
	for i, idx := range duties.CommitteeIndices {
		expected := uint64(4 + i)
		if idx != expected {
			t.Errorf("slot 1: committee[%d] = %d, want %d", i, idx, expected)
		}
	}

	// Slot 3 (last in epoch) should get validators [12, 13, 14, 15].
	duties = sched.GetDuties(3, 16)
	if duties.ProposerIndex != 3 {
		t.Errorf("slot 3: ProposerIndex = %d, want 3", duties.ProposerIndex)
	}
	for i, idx := range duties.CommitteeIndices {
		expected := uint64(12 + i)
		if idx != expected {
			t.Errorf("slot 3: committee[%d] = %d, want %d", i, idx, expected)
		}
	}
}

func TestGetDutiesZeroValidators(t *testing.T) {
	cfg := DefaultQuickSlotConfig()
	genesis := time.Now()
	sched := NewQuickSlotScheduler(cfg, genesis)

	duties := sched.GetDuties(0, 0)
	if duties.ProposerIndex != 0 {
		t.Errorf("zero validators: ProposerIndex = %d, want 0", duties.ProposerIndex)
	}
	if len(duties.CommitteeIndices) != 0 {
		t.Errorf("zero validators: committee size = %d, want 0", len(duties.CommitteeIndices))
	}
}

func TestGetDutiesUnevenSplit(t *testing.T) {
	cfg := DefaultQuickSlotConfig() // 4 slots per epoch
	genesis := time.Now()
	sched := NewQuickSlotScheduler(cfg, genesis)

	// 10 validators / 4 slots = 2 per slot with 2 remainder.
	// Slots 0 and 1 get 3 validators, slots 2 and 3 get 2.
	duties0 := sched.GetDuties(0, 10)
	duties1 := sched.GetDuties(1, 10)
	duties2 := sched.GetDuties(2, 10)
	duties3 := sched.GetDuties(3, 10)

	if len(duties0.CommitteeIndices) != 3 {
		t.Errorf("slot 0: committee size = %d, want 3", len(duties0.CommitteeIndices))
	}
	if len(duties1.CommitteeIndices) != 3 {
		t.Errorf("slot 1: committee size = %d, want 3", len(duties1.CommitteeIndices))
	}
	if len(duties2.CommitteeIndices) != 2 {
		t.Errorf("slot 2: committee size = %d, want 2", len(duties2.CommitteeIndices))
	}
	if len(duties3.CommitteeIndices) != 2 {
		t.Errorf("slot 3: committee size = %d, want 2", len(duties3.CommitteeIndices))
	}

	// Total committee members should equal total validators.
	total := len(duties0.CommitteeIndices) + len(duties1.CommitteeIndices) +
		len(duties2.CommitteeIndices) + len(duties3.CommitteeIndices)
	if total != 10 {
		t.Errorf("total committee members = %d, want 10", total)
	}

	// All validators should be assigned exactly once.
	assigned := make(map[uint64]bool)
	for _, d := range []*ValidatorDuties{duties0, duties1, duties2, duties3} {
		for _, idx := range d.CommitteeIndices {
			if assigned[idx] {
				t.Errorf("validator %d assigned to multiple slots", idx)
			}
			assigned[idx] = true
		}
	}
	if len(assigned) != 10 {
		t.Errorf("unique validators assigned = %d, want 10", len(assigned))
	}
}

func TestGetDutiesProposerWraps(t *testing.T) {
	cfg := DefaultQuickSlotConfig()
	genesis := time.Now()
	sched := NewQuickSlotScheduler(cfg, genesis)

	// With 3 validators, slot 3 should wrap: 3 % 3 = 0.
	duties := sched.GetDuties(3, 3)
	if duties.ProposerIndex != 0 {
		t.Errorf("slot 3 with 3 validators: ProposerIndex = %d, want 0", duties.ProposerIndex)
	}

	// Slot 5 with 3 validators: 5 % 3 = 2.
	duties = sched.GetDuties(5, 3)
	if duties.ProposerIndex != 2 {
		t.Errorf("slot 5 with 3 validators: ProposerIndex = %d, want 2", duties.ProposerIndex)
	}
}

func TestSlotStartTime(t *testing.T) {
	cfg := DefaultQuickSlotConfig() // 6s slots
	genesis := time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC)
	sched := NewQuickSlotScheduler(cfg, genesis)

	tests := []struct {
		slot uint64
		want time.Time
	}{
		{0, genesis},
		{1, genesis.Add(6 * time.Second)},
		{2, genesis.Add(12 * time.Second)},
		{4, genesis.Add(24 * time.Second)}, // epoch 1 start
		{10, genesis.Add(60 * time.Second)},
		{100, genesis.Add(600 * time.Second)},
	}

	for _, tt := range tests {
		got := sched.SlotStartTime(tt.slot)
		if !got.Equal(tt.want) {
			t.Errorf("SlotStartTime(%d) = %v, want %v", tt.slot, got, tt.want)
		}
	}
}

func TestSchedulerAccessors(t *testing.T) {
	cfg := DefaultQuickSlotConfig()
	genesis := time.Date(2028, 6, 15, 12, 0, 0, 0, time.UTC)
	sched := NewQuickSlotScheduler(cfg, genesis)

	if !sched.GenesisTime().Equal(genesis) {
		t.Errorf("GenesisTime = %v, want %v", sched.GenesisTime(), genesis)
	}
	if sched.Config() != cfg {
		t.Error("Config() should return the same config pointer")
	}
}

func TestSchedulerNilConfig(t *testing.T) {
	genesis := time.Now()
	sched := NewQuickSlotScheduler(nil, genesis)

	if sched.config == nil {
		t.Fatal("NewQuickSlotScheduler(nil, ...) should use default config")
	}
	if sched.config.SlotDuration != 6*time.Second {
		t.Errorf("default SlotDuration = %v, want 6s", sched.config.SlotDuration)
	}
}

func TestValidateConfig(t *testing.T) {
	// Valid config.
	cfg := DefaultQuickSlotConfig()
	if err := ValidateConfig(cfg); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	// Nil config.
	if err := ValidateConfig(nil); err == nil {
		t.Fatal("nil config should fail validation")
	}

	// Zero slot duration.
	bad := &QuickSlotConfig{SlotDuration: 0, SlotsPerEpoch: 4}
	if err := ValidateConfig(bad); err != ErrQuickSlotDurationZero {
		t.Fatalf("expected ErrQuickSlotDurationZero, got %v", err)
	}

	// Zero slots per epoch.
	bad2 := &QuickSlotConfig{SlotDuration: 6 * time.Second, SlotsPerEpoch: 0}
	if err := ValidateConfig(bad2); err != ErrQuickSlotSlotsPerEpoch {
		t.Fatalf("expected ErrQuickSlotSlotsPerEpoch, got %v", err)
	}
}

func TestNewQuickSlotSchedulerZeroDuration(t *testing.T) {
	cfg := &QuickSlotConfig{SlotDuration: 0, SlotsPerEpoch: 4}
	sched := NewQuickSlotScheduler(cfg, time.Now())
	if sched.config.SlotDuration != 6*time.Second {
		t.Errorf("zero SlotDuration should default to 6s, got %v", sched.config.SlotDuration)
	}
}

func TestNewQuickSlotSchedulerZeroSlotsPerEpoch(t *testing.T) {
	cfg := &QuickSlotConfig{SlotDuration: 6 * time.Second, SlotsPerEpoch: 0}
	sched := NewQuickSlotScheduler(cfg, time.Now())
	if sched.config.SlotsPerEpoch != 4 {
		t.Errorf("zero SlotsPerEpoch should default to 4, got %d", sched.config.SlotsPerEpoch)
	}
}

func TestIsSlotInEpoch(t *testing.T) {
	cfg := DefaultQuickSlotConfig() // 4 slots per epoch
	genesis := time.Now()
	sched := NewQuickSlotScheduler(cfg, genesis)

	tests := []struct {
		slot  uint64
		epoch uint64
		want  bool
	}{
		{0, 0, true},
		{1, 0, true},
		{3, 0, true},
		{4, 0, false},
		{4, 1, true},
		{7, 1, true},
		{8, 2, true},
		{8, 1, false},
		{100, 25, true},
		{100, 24, false},
	}

	for _, tt := range tests {
		got := sched.IsSlotInEpoch(tt.slot, tt.epoch)
		if got != tt.want {
			t.Errorf("IsSlotInEpoch(%d, %d) = %v, want %v", tt.slot, tt.epoch, got, tt.want)
		}
	}
}

func TestValidateSlotTransition(t *testing.T) {
	cfg := DefaultQuickSlotConfig()
	genesis := time.Now()
	sched := NewQuickSlotScheduler(cfg, genesis)

	// Forward transition.
	if err := sched.ValidateSlotTransition(5, 6); err != nil {
		t.Fatalf("forward transition rejected: %v", err)
	}

	// Same slot (no change).
	if err := sched.ValidateSlotTransition(5, 5); err != nil {
		t.Fatalf("same slot rejected: %v", err)
	}

	// Backward transition.
	err := sched.ValidateSlotTransition(10, 5)
	if err == nil {
		t.Fatal("backward transition should fail")
	}

	// Zero to zero.
	if err := sched.ValidateSlotTransition(0, 0); err != nil {
		t.Fatalf("0->0 rejected: %v", err)
	}
}

func TestEpochRoundTrip(t *testing.T) {
	cfg := DefaultQuickSlotConfig()
	genesis := time.Now()
	sched := NewQuickSlotScheduler(cfg, genesis)

	// For each epoch 0..9, the start slot's epoch should equal the epoch.
	for epoch := uint64(0); epoch < 10; epoch++ {
		startSlot := sched.EpochStartSlot(epoch)
		gotEpoch := sched.SlotToEpoch(startSlot)
		if gotEpoch != epoch {
			t.Errorf("SlotToEpoch(EpochStartSlot(%d)) = %d, want %d", epoch, gotEpoch, epoch)
		}
		// Last slot of the epoch should also map to the same epoch.
		lastSlot := startSlot + cfg.SlotsPerEpoch - 1
		gotEpoch = sched.SlotToEpoch(lastSlot)
		if gotEpoch != epoch {
			t.Errorf("SlotToEpoch(last slot of epoch %d) = %d, want %d", epoch, gotEpoch, epoch)
		}
	}
}
