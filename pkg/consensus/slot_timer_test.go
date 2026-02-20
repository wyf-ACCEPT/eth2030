package consensus

import (
	"testing"
	"time"
)

func TestNewSlotTimer(t *testing.T) {
	cfg := DefaultSlotTimerConfig()
	st := NewSlotTimer(cfg)

	if st.GenesisTimeValue() != 0 {
		t.Errorf("genesis time = %d, want 0", st.GenesisTimeValue())
	}
}

func TestNewSlotTimer_PanicZeroSecondsPerSlot(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for zero SecondsPerSlot")
		}
	}()
	NewSlotTimer(SlotTimerConfig{SecondsPerSlot: 0, SlotsPerEpoch: 32})
}

func TestNewSlotTimer_PanicZeroSlotsPerEpoch(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for zero SlotsPerEpoch")
		}
	}()
	NewSlotTimer(SlotTimerConfig{SecondsPerSlot: 12, SlotsPerEpoch: 0})
}

func TestSlotTimer_SlotAt(t *testing.T) {
	st := NewSlotTimer(SlotTimerConfig{
		GenesisTime:    1000,
		SecondsPerSlot: 12,
		SlotsPerEpoch:  32,
	})

	tests := []struct {
		now  uint64
		want uint64
	}{
		{999, 0},     // before genesis
		{1000, 0},    // at genesis
		{1001, 0},    // 1s into slot 0
		{1011, 0},    // 11s into slot 0
		{1012, 1},    // slot 1 starts
		{1023, 1},    // end of slot 1
		{1024, 2},    // slot 2 starts
		{1384, 32},   // slot 32 = epoch 1 start
		{2000, 83},   // (2000-1000)/12 = 83
	}
	for _, tt := range tests {
		got := st.slotAt(tt.now)
		if got != tt.want {
			t.Errorf("slotAt(%d) = %d, want %d", tt.now, got, tt.want)
		}
	}
}

func TestSlotTimer_EpochAt(t *testing.T) {
	st := NewSlotTimer(SlotTimerConfig{
		GenesisTime:    0,
		SecondsPerSlot: 12,
		SlotsPerEpoch:  32,
	})

	tests := []struct {
		now  uint64
		want uint64
	}{
		{0, 0},
		{383, 0},   // last second of epoch 0 (slot 31)
		{384, 1},   // epoch 1 starts at slot 32 = 32*12 = 384
		{767, 1},   // last second of epoch 1
		{768, 2},   // epoch 2
	}
	for _, tt := range tests {
		got := st.epochAt(tt.now)
		if got != tt.want {
			t.Errorf("epochAt(%d) = %d, want %d", tt.now, got, tt.want)
		}
	}
}

func TestSlotTimer_SlotStartTime(t *testing.T) {
	st := NewSlotTimer(SlotTimerConfig{
		GenesisTime:    1000,
		SecondsPerSlot: 12,
		SlotsPerEpoch:  32,
	})

	tests := []struct {
		slot uint64
		want uint64
	}{
		{0, 1000},
		{1, 1012},
		{32, 1384},
		{100, 2200},
	}
	for _, tt := range tests {
		got := st.SlotStartTime(tt.slot)
		if got != tt.want {
			t.Errorf("SlotStartTime(%d) = %d, want %d", tt.slot, got, tt.want)
		}
	}
}

func TestSlotTimer_EpochStartSlot(t *testing.T) {
	st := NewSlotTimer(SlotTimerConfig{
		GenesisTime:    0,
		SecondsPerSlot: 12,
		SlotsPerEpoch:  32,
	})

	tests := []struct {
		epoch uint64
		want  uint64
	}{
		{0, 0},
		{1, 32},
		{2, 64},
		{10, 320},
	}
	for _, tt := range tests {
		got := st.EpochStartSlot(tt.epoch)
		if got != tt.want {
			t.Errorf("EpochStartSlot(%d) = %d, want %d", tt.epoch, got, tt.want)
		}
	}
}

func TestSlotTimer_SlotToEpoch(t *testing.T) {
	st := NewSlotTimer(SlotTimerConfig{
		GenesisTime:    0,
		SecondsPerSlot: 12,
		SlotsPerEpoch:  32,
	})

	tests := []struct {
		slot uint64
		want uint64
	}{
		{0, 0},
		{1, 0},
		{31, 0},
		{32, 1},
		{63, 1},
		{64, 2},
		{100, 3},
	}
	for _, tt := range tests {
		got := st.SlotToEpoch(tt.slot)
		if got != tt.want {
			t.Errorf("SlotToEpoch(%d) = %d, want %d", tt.slot, got, tt.want)
		}
	}
}

func TestSlotTimer_IsFirstSlotOfEpoch(t *testing.T) {
	st := NewSlotTimer(SlotTimerConfig{
		GenesisTime:    0,
		SecondsPerSlot: 12,
		SlotsPerEpoch:  32,
	})

	tests := []struct {
		slot uint64
		want bool
	}{
		{0, true},
		{1, false},
		{31, false},
		{32, true},
		{33, false},
		{64, true},
		{96, true},
	}
	for _, tt := range tests {
		got := st.IsFirstSlotOfEpoch(tt.slot)
		if got != tt.want {
			t.Errorf("IsFirstSlotOfEpoch(%d) = %v, want %v", tt.slot, got, tt.want)
		}
	}
}

func TestSlotTimer_IsFirstSlotOfEpoch_QuickSlots(t *testing.T) {
	st := NewSlotTimer(SlotTimerConfig{
		GenesisTime:    0,
		SecondsPerSlot: 6,
		SlotsPerEpoch:  4,
	})

	tests := []struct {
		slot uint64
		want bool
	}{
		{0, true},
		{1, false},
		{3, false},
		{4, true},
		{8, true},
		{9, false},
	}
	for _, tt := range tests {
		got := st.IsFirstSlotOfEpoch(tt.slot)
		if got != tt.want {
			t.Errorf("QuickSlots IsFirstSlotOfEpoch(%d) = %v, want %v", tt.slot, got, tt.want)
		}
	}
}

func TestSlotTimer_EpochProgressAt(t *testing.T) {
	st := NewSlotTimer(SlotTimerConfig{
		GenesisTime:    0,
		SecondsPerSlot: 12,
		SlotsPerEpoch:  32,
	})

	tests := []struct {
		now  uint64
		want float64
	}{
		{0, 0.0},              // slot 0, first of epoch => 0/32
		{12, 1.0 / 32.0},     // slot 1 => 1/32
		{24, 2.0 / 32.0},     // slot 2 => 2/32
		{372, 31.0 / 32.0},   // slot 31 => 31/32
		{384, 0.0},            // slot 32 => epoch 1, slot 0 => 0/32
		{396, 1.0 / 32.0},    // slot 33 => epoch 1, slot 1 => 1/32
	}
	for _, tt := range tests {
		got := st.epochProgressAt(tt.now)
		if got != tt.want {
			t.Errorf("epochProgressAt(%d) = %f, want %f", tt.now, got, tt.want)
		}
	}
}

func TestSlotTimer_EpochProgressAt_QuickSlots(t *testing.T) {
	st := NewSlotTimer(SlotTimerConfig{
		GenesisTime:    0,
		SecondsPerSlot: 6,
		SlotsPerEpoch:  4,
	})

	tests := []struct {
		now  uint64
		want float64
	}{
		{0, 0.0},            // slot 0 => 0/4
		{6, 0.25},           // slot 1 => 1/4
		{12, 0.5},           // slot 2 => 2/4
		{18, 0.75},          // slot 3 => 3/4
		{24, 0.0},           // slot 4 => epoch 1, slot 0 => 0/4
	}
	for _, tt := range tests {
		got := st.epochProgressAt(tt.now)
		if got != tt.want {
			t.Errorf("epochProgressAt(%d) = %f, want %f", tt.now, got, tt.want)
		}
	}
}

func TestSlotTimer_CurrentSlot_LiveClock(t *testing.T) {
	// Use genesis far in the past so CurrentSlot is deterministic.
	genesis := uint64(time.Now().Unix()) - 120 // 120 seconds ago
	st := NewSlotTimer(SlotTimerConfig{
		GenesisTime:    genesis,
		SecondsPerSlot: 12,
		SlotsPerEpoch:  32,
	})

	slot := st.CurrentSlot()
	// 120s / 12s = 10 slots, but wall clock may shift slightly.
	if slot < 9 || slot > 11 {
		t.Errorf("CurrentSlot() = %d, expected ~10", slot)
	}
}

func TestSlotTimer_CurrentEpoch_LiveClock(t *testing.T) {
	genesis := uint64(time.Now().Unix()) - 384 // 384s = 1 epoch ago (32*12)
	st := NewSlotTimer(SlotTimerConfig{
		GenesisTime:    genesis,
		SecondsPerSlot: 12,
		SlotsPerEpoch:  32,
	})

	epoch := st.CurrentEpoch()
	if epoch < 0 || epoch > 2 {
		t.Errorf("CurrentEpoch() = %d, expected ~1", epoch)
	}
}

func TestSlotTimer_TimeUntilSlot(t *testing.T) {
	now := uint64(time.Now().Unix())
	st := NewSlotTimer(SlotTimerConfig{
		GenesisTime:    now,
		SecondsPerSlot: 12,
		SlotsPerEpoch:  32,
	})

	// Slot 10 starts at genesis + 120s, which is ~120s from now.
	delta := st.TimeUntilSlot(10)
	if delta < 119 || delta > 121 {
		t.Errorf("TimeUntilSlot(10) = %d, expected ~120", delta)
	}

	// Slot 0 started at genesis (now), so should be ~0 or slightly negative.
	delta0 := st.TimeUntilSlot(0)
	if delta0 > 1 || delta0 < -1 {
		t.Errorf("TimeUntilSlot(0) = %d, expected ~0", delta0)
	}
}

func TestSlotTimer_TimeUntilSlot_Past(t *testing.T) {
	// Genesis was 1000 seconds ago.
	genesis := uint64(time.Now().Unix()) - 1000
	st := NewSlotTimer(SlotTimerConfig{
		GenesisTime:    genesis,
		SecondsPerSlot: 12,
		SlotsPerEpoch:  32,
	})

	// Slot 0 started 1000 seconds ago => TimeUntilSlot should be ~-1000.
	delta := st.TimeUntilSlot(0)
	if delta > -999 || delta < -1001 {
		t.Errorf("TimeUntilSlot(0) = %d, expected ~-1000", delta)
	}
}

func TestSlotTimer_SlotsSinceGenesis(t *testing.T) {
	genesis := uint64(time.Now().Unix()) - 60
	st := NewSlotTimer(SlotTimerConfig{
		GenesisTime:    genesis,
		SecondsPerSlot: 12,
		SlotsPerEpoch:  32,
	})

	slots := st.SlotsSinceGenesis()
	// 60s / 12s = 5 slots.
	if slots < 4 || slots > 6 {
		t.Errorf("SlotsSinceGenesis() = %d, expected ~5", slots)
	}
}

func TestSlotTimer_WithGenesisTime(t *testing.T) {
	st := NewSlotTimer(SlotTimerConfig{
		GenesisTime:    1000,
		SecondsPerSlot: 12,
		SlotsPerEpoch:  32,
	})

	st2 := st.WithGenesisTime(2000)

	// Original unchanged.
	if st.GenesisTimeValue() != 1000 {
		t.Errorf("original genesis = %d, want 1000", st.GenesisTimeValue())
	}
	// New copy has different genesis.
	if st2.GenesisTimeValue() != 2000 {
		t.Errorf("copy genesis = %d, want 2000", st2.GenesisTimeValue())
	}

	// Same slot duration: slot 1 at genesis + 12.
	if st.SlotStartTime(1) != 1012 {
		t.Errorf("original slot 1 start = %d, want 1012", st.SlotStartTime(1))
	}
	if st2.SlotStartTime(1) != 2012 {
		t.Errorf("copy slot 1 start = %d, want 2012", st2.SlotStartTime(1))
	}

	// slotAt should differ.
	if st.slotAt(1024) != 2 {
		t.Errorf("original slotAt(1024) = %d, want 2", st.slotAt(1024))
	}
	if st2.slotAt(2024) != 2 {
		t.Errorf("copy slotAt(2024) = %d, want 2", st2.slotAt(2024))
	}
}

func TestSlotTimer_BeforeGenesis(t *testing.T) {
	st := NewSlotTimer(SlotTimerConfig{
		GenesisTime:    uint64(time.Now().Unix()) + 3600, // 1 hour in the future
		SecondsPerSlot: 12,
		SlotsPerEpoch:  32,
	})

	if st.CurrentSlot() != 0 {
		t.Errorf("CurrentSlot before genesis = %d, want 0", st.CurrentSlot())
	}
	if st.CurrentEpoch() != 0 {
		t.Errorf("CurrentEpoch before genesis = %d, want 0", st.CurrentEpoch())
	}
	if st.SlotsSinceGenesis() != 0 {
		t.Errorf("SlotsSinceGenesis before genesis = %d, want 0", st.SlotsSinceGenesis())
	}
	if st.EpochProgress() != 0.0 {
		t.Errorf("EpochProgress before genesis = %f, want 0.0", st.EpochProgress())
	}
}

func TestSlotTimer_SlotAt_BeforeGenesis(t *testing.T) {
	st := NewSlotTimer(SlotTimerConfig{
		GenesisTime:    1000,
		SecondsPerSlot: 12,
		SlotsPerEpoch:  32,
	})

	if st.slotAt(500) != 0 {
		t.Errorf("slotAt before genesis = %d, want 0", st.slotAt(500))
	}
}

func TestSlotTimer_ConsistentSlotEpochMapping(t *testing.T) {
	st := NewSlotTimer(SlotTimerConfig{
		GenesisTime:    0,
		SecondsPerSlot: 6,
		SlotsPerEpoch:  4,
	})

	// Verify that SlotToEpoch and EpochStartSlot are consistent.
	for slot := uint64(0); slot < 100; slot++ {
		epoch := st.SlotToEpoch(slot)
		startSlot := st.EpochStartSlot(epoch)
		if slot < startSlot || slot >= startSlot+st.slotsPerEpoch {
			t.Errorf("slot %d -> epoch %d -> startSlot %d: inconsistent", slot, epoch, startSlot)
		}
	}
}

func TestSlotTimer_SlotStartTimeConsistency(t *testing.T) {
	st := NewSlotTimer(SlotTimerConfig{
		GenesisTime:    5000,
		SecondsPerSlot: 12,
		SlotsPerEpoch:  32,
	})

	// Slot start times should be monotonically increasing.
	for slot := uint64(0); slot < 100; slot++ {
		t1 := st.SlotStartTime(slot)
		t2 := st.SlotStartTime(slot + 1)
		if t2 != t1+12 {
			t.Errorf("SlotStartTime(%d)=%d, SlotStartTime(%d)=%d, diff=%d, want 12",
				slot, t1, slot+1, t2, t2-t1)
		}
	}
}

func TestSlotTimer_EpochStartSlot_RoundTrip(t *testing.T) {
	st := NewSlotTimer(SlotTimerConfig{
		GenesisTime:    0,
		SecondsPerSlot: 12,
		SlotsPerEpoch:  32,
	})

	for epoch := uint64(0); epoch < 50; epoch++ {
		startSlot := st.EpochStartSlot(epoch)
		gotEpoch := st.SlotToEpoch(startSlot)
		if gotEpoch != epoch {
			t.Errorf("epoch %d -> startSlot %d -> epoch %d", epoch, startSlot, gotEpoch)
		}
		// First slot of epoch should pass IsFirstSlotOfEpoch.
		if !st.IsFirstSlotOfEpoch(startSlot) {
			t.Errorf("EpochStartSlot(%d) = %d, but IsFirstSlotOfEpoch returned false", epoch, startSlot)
		}
	}
}

func TestSlotTimer_DefaultSlotTimerConfig(t *testing.T) {
	cfg := DefaultSlotTimerConfig()
	if cfg.SecondsPerSlot != 12 {
		t.Errorf("default SecondsPerSlot = %d, want 12", cfg.SecondsPerSlot)
	}
	if cfg.SlotsPerEpoch != 32 {
		t.Errorf("default SlotsPerEpoch = %d, want 32", cfg.SlotsPerEpoch)
	}
	if cfg.GenesisTime != 0 {
		t.Errorf("default GenesisTime = %d, want 0", cfg.GenesisTime)
	}
}
