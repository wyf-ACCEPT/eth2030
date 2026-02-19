package consensus

import (
	"testing"
	"time"
)

func TestSlotClock_CurrentSlot(t *testing.T) {
	cfg := DefaultConfig() // 12s slots
	genesis := uint64(1000)
	sc := NewSlotClock(genesis, cfg)

	tests := []struct {
		now  uint64
		want Slot
	}{
		{999, 0},     // before genesis
		{1000, 0},    // at genesis
		{1001, 0},    // 1s in
		{1011, 0},    // 11s in (still slot 0)
		{1012, 1},    // slot 1 starts
		{1023, 1},    // end of slot 1
		{1024, 2},    // slot 2 starts
		{1384, 32},   // epoch 1, slot 32
	}
	for _, tt := range tests {
		got := sc.CurrentSlot(tt.now)
		if got != tt.want {
			t.Errorf("CurrentSlot(%d) = %d, want %d", tt.now, got, tt.want)
		}
	}
}

func TestSlotClock_CurrentEpoch(t *testing.T) {
	cfg := DefaultConfig() // 12s, 32 slots/epoch
	genesis := uint64(0)
	sc := NewSlotClock(genesis, cfg)

	tests := []struct {
		now  uint64
		want Epoch
	}{
		{0, 0},
		{383, 0},     // last second of epoch 0
		{384, 1},     // epoch 1 starts at slot 32 * 12 = 384
		{767, 1},     // last second of epoch 1
		{768, 2},     // epoch 2
	}
	for _, tt := range tests {
		got := sc.CurrentEpoch(tt.now)
		if got != tt.want {
			t.Errorf("CurrentEpoch(%d) = %d, want %d", tt.now, got, tt.want)
		}
	}
}

func TestSlotClock_QuickSlots(t *testing.T) {
	cfg := QuickSlotsConfig() // 6s, 4 slots/epoch
	genesis := uint64(0)
	sc := NewSlotClock(genesis, cfg)

	tests := []struct {
		now  uint64
		want Slot
	}{
		{0, 0},
		{5, 0},
		{6, 1},
		{11, 1},
		{12, 2},
		{23, 3},
		{24, 4}, // epoch 1
	}
	for _, tt := range tests {
		got := sc.CurrentSlot(tt.now)
		if got != tt.want {
			t.Errorf("QuickSlots CurrentSlot(%d) = %d, want %d", tt.now, got, tt.want)
		}
	}

	// Epoch boundary: epoch 0 = slots 0-3, epoch 1 = slots 4-7
	if sc.CurrentEpoch(23) != 0 {
		t.Error("t=23 should be epoch 0")
	}
	if sc.CurrentEpoch(24) != 1 {
		t.Error("t=24 should be epoch 1")
	}
}

func TestSlotClock_SlotStartTime(t *testing.T) {
	cfg := DefaultConfig()
	genesis := uint64(1000)
	sc := NewSlotClock(genesis, cfg)

	if sc.SlotStartTime(0) != 1000 {
		t.Errorf("slot 0 start time should be genesis")
	}
	if sc.SlotStartTime(1) != 1012 {
		t.Errorf("slot 1 start time should be 1012, got %d", sc.SlotStartTime(1))
	}
	if sc.SlotStartTime(32) != 1384 {
		t.Errorf("slot 32 start time should be 1384, got %d", sc.SlotStartTime(32))
	}
}

func TestSlotClock_TimeInSlot(t *testing.T) {
	cfg := DefaultConfig()
	genesis := uint64(0)
	sc := NewSlotClock(genesis, cfg)

	tests := []struct {
		now  uint64
		want uint64
	}{
		{0, 0},
		{1, 1},
		{11, 11},
		{12, 0}, // start of slot 1
		{13, 1},
		{25, 1},
	}
	for _, tt := range tests {
		got := sc.TimeInSlot(tt.now)
		if got != tt.want {
			t.Errorf("TimeInSlot(%d) = %d, want %d", tt.now, got, tt.want)
		}
	}
}

func TestSlotClock_TimeInSlot_BeforeGenesis(t *testing.T) {
	cfg := DefaultConfig()
	sc := NewSlotClock(100, cfg)
	if sc.TimeInSlot(50) != 0 {
		t.Error("TimeInSlot before genesis should return 0")
	}
}

func TestSlotClock_NextSlotIn(t *testing.T) {
	cfg := DefaultConfig() // 12s
	sc := NewSlotClock(0, cfg)

	tests := []struct {
		now  uint64
		want time.Duration
	}{
		{0, 12 * time.Second},  // at slot boundary, next is 12s away
		{1, 11 * time.Second},
		{6, 6 * time.Second},
		{11, 1 * time.Second},
		{12, 12 * time.Second}, // at slot 1 boundary
	}
	for _, tt := range tests {
		got := sc.NextSlotIn(tt.now)
		if got != tt.want {
			t.Errorf("NextSlotIn(%d) = %v, want %v", tt.now, got, tt.want)
		}
	}
}

func TestSlotClock_NextSlotIn_BeforeGenesis(t *testing.T) {
	sc := NewSlotClock(100, DefaultConfig())
	got := sc.NextSlotIn(50)
	if got != 50*time.Second {
		t.Errorf("NextSlotIn before genesis should be 50s, got %v", got)
	}
}

func TestSlotClock_AttestationDeadline(t *testing.T) {
	sc := NewSlotClock(0, DefaultConfig())
	d := sc.AttestationDeadline()
	if d != 4*time.Second {
		t.Errorf("attestation deadline for 12s slot should be 4s, got %v", d)
	}

	qs := NewSlotClock(0, QuickSlotsConfig())
	d = qs.AttestationDeadline()
	if d != 2*time.Second {
		t.Errorf("attestation deadline for 6s slot should be 2s, got %v", d)
	}
}

func TestSlotClock_ProposalDeadline(t *testing.T) {
	sc := NewSlotClock(0, DefaultConfig())
	if sc.ProposalDeadline() != 0 {
		t.Error("proposal deadline should be 0 (slot start)")
	}
}

func TestSlotClock_Accessors(t *testing.T) {
	sc := NewSlotClock(42, DefaultConfig())
	if sc.GenesisTime() != 42 {
		t.Errorf("GenesisTime() = %d, want 42", sc.GenesisTime())
	}
	if sc.SecondsPerSlot() != 12 {
		t.Errorf("SecondsPerSlot() = %d, want 12", sc.SecondsPerSlot())
	}
}

func TestSlotSchedule_Basic(t *testing.T) {
	ss := NewSlotSchedule(0, 12)
	if ss.SlotDurationAtTime(0) != 12 {
		t.Error("base duration should be 12")
	}
	if ss.SlotDurationAtTime(1000) != 12 {
		t.Error("without forks, duration should stay 12")
	}
}

func TestSlotSchedule_ForkTransition(t *testing.T) {
	ss := NewSlotSchedule(0, 12)
	if err := ss.AddFork(100, 6); err != nil {
		t.Fatalf("AddFork failed: %v", err)
	}

	// Before fork.
	if ss.SlotDurationAtTime(0) != 12 {
		t.Error("before fork, duration should be 12")
	}
	if ss.SlotDurationAtTime(99) != 12 {
		t.Error("at t=99, duration should still be 12")
	}

	// At and after fork.
	if ss.SlotDurationAtTime(100) != 6 {
		t.Error("at fork, duration should be 6")
	}
	if ss.SlotDurationAtTime(200) != 6 {
		t.Error("after fork, duration should be 6")
	}
}

func TestSlotSchedule_SlotAtTime(t *testing.T) {
	ss := NewSlotSchedule(0, 12)
	if err := ss.AddFork(120, 6); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		t    uint64
		want Slot
	}{
		{0, 0},      // genesis
		{12, 1},     // slot 1 at 12s
		{24, 2},     // slot 2 at 24s
		{119, 9},    // last full slot before fork: 119/12 = 9
		{120, 10},   // fork at t=120: 120/12 = 10 slots in pre-fork
		{126, 11},   // 10 + (126-120)/6 = 10 + 1 = 11
		{132, 12},   // 10 + (132-120)/6 = 10 + 2 = 12
	}
	for _, tt := range tests {
		got := ss.SlotAtTime(tt.t)
		if got != tt.want {
			t.Errorf("SlotAtTime(%d) = %d, want %d", tt.t, got, tt.want)
		}
	}
}

func TestSlotSchedule_SlotAtTime_BeforeGenesis(t *testing.T) {
	ss := NewSlotSchedule(100, 12)
	if ss.SlotAtTime(50) != 0 {
		t.Error("SlotAtTime before genesis should return 0")
	}
}

func TestSlotSchedule_AddFork_Errors(t *testing.T) {
	ss := NewSlotSchedule(0, 12)
	if err := ss.AddFork(100, 6); err != nil {
		t.Fatal(err)
	}
	// Fork before previous.
	if err := ss.AddFork(50, 3); err == nil {
		t.Error("expected error for fork before previous")
	}
	// Fork at same time.
	if err := ss.AddFork(100, 3); err == nil {
		t.Error("expected error for fork at same time as previous")
	}
	// Zero duration.
	if err := ss.AddFork(200, 0); err == nil {
		t.Error("expected error for zero slot duration")
	}
}

func TestSlotSchedule_MultipleForks(t *testing.T) {
	ss := NewSlotSchedule(0, 12)
	if err := ss.AddFork(120, 6); err != nil {
		t.Fatal(err)
	}
	if err := ss.AddFork(180, 3); err != nil {
		t.Fatal(err)
	}

	// Pre-fork: 12s slots.
	if ss.SlotDurationAtTime(0) != 12 {
		t.Error("at t=0, duration should be 12")
	}
	// First fork: 6s slots.
	if ss.SlotDurationAtTime(120) != 6 {
		t.Error("at t=120, duration should be 6")
	}
	// Second fork: 3s slots.
	if ss.SlotDurationAtTime(180) != 3 {
		t.Error("at t=180, duration should be 3")
	}

	// Slot counting across 3 periods:
	// [0, 120): 120/12 = 10 slots
	// [120, 180): 60/6 = 10 slots
	// [180, 183): 3/3 = 1 slot
	got := ss.SlotAtTime(183)
	if got != 21 {
		t.Errorf("SlotAtTime(183) = %d, want 21", got)
	}
}
