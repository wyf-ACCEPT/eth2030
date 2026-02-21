package consensus

import (
	"testing"
	"time"
)

func TestPhaseTimerNew(t *testing.T) {
	pt := NewPhaseTimer(nil)
	if pt == nil {
		t.Fatal("expected non-nil PhaseTimer")
	}
	cfg := pt.Config()
	if cfg.SlotDurationMs != 6000 {
		t.Errorf("SlotDurationMs = %d, want 6000", cfg.SlotDurationMs)
	}
	if cfg.SlotsPerEpoch != 4 {
		t.Errorf("SlotsPerEpoch = %d, want 4", cfg.SlotsPerEpoch)
	}
}

func TestPhaseTimerCurrentSlot(t *testing.T) {
	genesis := time.Now().Add(-30 * time.Second)
	pt := NewPhaseTimer(&PhaseTimerConfig{
		SlotDurationMs:     6000,
		ProposalPhaseMs:    2000,
		AttestationPhaseMs: 2000,
		AggregationPhaseMs: 2000,
		GenesisTime:        genesis.Unix(),
		SlotsPerEpoch:      4,
	})

	slot := pt.CurrentSlot()
	// 30s / 6s = 5 slots, allow for timing jitter.
	if slot < 4 || slot > 6 {
		t.Errorf("CurrentSlot = %d, expected ~5", slot)
	}
}

func TestPhaseTimerCurrentPhase(t *testing.T) {
	// Set genesis to a known point where we can predict the phase.
	now := time.Now()
	// Genesis 1 second ago => we're 1000ms into slot 0 => proposal phase (0-2000ms).
	genesis := now.Add(-1 * time.Second)
	pt := NewPhaseTimer(&PhaseTimerConfig{
		SlotDurationMs:     6000,
		ProposalPhaseMs:    2000,
		AttestationPhaseMs: 2000,
		AggregationPhaseMs: 2000,
		GenesisTime:        genesis.Unix(),
		SlotsPerEpoch:      4,
	})

	// Use timeFunc for determinism.
	pt.timeFunc = func() time.Time { return genesis.Add(500 * time.Millisecond) }
	if pt.CurrentPhase() != PhaseProposal {
		t.Errorf("phase at 500ms = %v, want proposal", pt.CurrentPhase())
	}

	pt.timeFunc = func() time.Time { return genesis.Add(2500 * time.Millisecond) }
	if pt.CurrentPhase() != PhaseAttestation {
		t.Errorf("phase at 2500ms = %v, want attestation", pt.CurrentPhase())
	}

	pt.timeFunc = func() time.Time { return genesis.Add(4500 * time.Millisecond) }
	if pt.CurrentPhase() != PhaseAggregation {
		t.Errorf("phase at 4500ms = %v, want aggregation", pt.CurrentPhase())
	}
}

func TestPhaseTimerTimeToNextSlot(t *testing.T) {
	genesis := time.Now()
	pt := NewPhaseTimer(&PhaseTimerConfig{
		SlotDurationMs:     6000,
		ProposalPhaseMs:    2000,
		AttestationPhaseMs: 2000,
		AggregationPhaseMs: 2000,
		GenesisTime:        genesis.Unix(),
		SlotsPerEpoch:      4,
	})

	// Set time to 1 second after genesis (5 seconds remain in slot 0).
	pt.timeFunc = func() time.Time { return genesis.Add(1 * time.Second) }

	ttns := pt.TimeToNextSlot()
	// We expect roughly 5 seconds, but genesis time resolution is seconds so
	// there can be up to 1s of rounding.
	if ttns < 4*time.Second || ttns > 6*time.Second {
		t.Errorf("TimeToNextSlot = %v, expected ~5s", ttns)
	}
}

func TestPhaseTimerTimeToNextPhase(t *testing.T) {
	genesis := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	pt := NewPhaseTimer(&PhaseTimerConfig{
		SlotDurationMs:     6000,
		ProposalPhaseMs:    2000,
		AttestationPhaseMs: 2000,
		AggregationPhaseMs: 2000,
		GenesisTime:        genesis.Unix(),
		SlotsPerEpoch:      4,
	})

	// At 500ms into proposal phase, next phase starts at 2000ms.
	pt.timeFunc = func() time.Time { return genesis.Add(500 * time.Millisecond) }
	ttnp := pt.TimeToNextPhase()
	if ttnp < 1400*time.Millisecond || ttnp > 1600*time.Millisecond {
		t.Errorf("TimeToNextPhase at proposal = %v, expected ~1500ms", ttnp)
	}

	// At 3000ms into attestation phase, next phase at 4000ms.
	pt.timeFunc = func() time.Time { return genesis.Add(3000 * time.Millisecond) }
	ttnp = pt.TimeToNextPhase()
	if ttnp < 900*time.Millisecond || ttnp > 1100*time.Millisecond {
		t.Errorf("TimeToNextPhase at attestation = %v, expected ~1000ms", ttnp)
	}

	// At 5000ms into aggregation phase, next boundary is next slot at 6000ms.
	pt.timeFunc = func() time.Time { return genesis.Add(5000 * time.Millisecond) }
	ttnp = pt.TimeToNextPhase()
	if ttnp < 900*time.Millisecond || ttnp > 1100*time.Millisecond {
		t.Errorf("TimeToNextPhase at aggregation = %v, expected ~1000ms", ttnp)
	}
}

func TestPhaseTimerSlotStartTime(t *testing.T) {
	genesis := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	pt := NewPhaseTimer(&PhaseTimerConfig{
		SlotDurationMs:     6000,
		ProposalPhaseMs:    2000,
		AttestationPhaseMs: 2000,
		AggregationPhaseMs: 2000,
		GenesisTime:        genesis.Unix(),
		SlotsPerEpoch:      4,
	})

	tests := []struct {
		slot uint64
		want time.Time
	}{
		{0, genesis},
		{1, genesis.Add(6 * time.Second)},
		{2, genesis.Add(12 * time.Second)},
		{10, genesis.Add(60 * time.Second)},
	}
	for _, tt := range tests {
		got := pt.SlotStartTime(tt.slot)
		if !got.Equal(tt.want) {
			t.Errorf("SlotStartTime(%d) = %v, want %v", tt.slot, got, tt.want)
		}
	}
}

func TestPhaseTimerPhaseStartTime(t *testing.T) {
	genesis := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	pt := NewPhaseTimer(&PhaseTimerConfig{
		SlotDurationMs:     6000,
		ProposalPhaseMs:    2000,
		AttestationPhaseMs: 2000,
		AggregationPhaseMs: 2000,
		GenesisTime:        genesis.Unix(),
		SlotsPerEpoch:      4,
	})

	slotStart := genesis.Add(6 * time.Second) // slot 1

	tests := []struct {
		phase SlotPhase
		want  time.Time
	}{
		{PhaseProposal, slotStart},
		{PhaseAttestation, slotStart.Add(2 * time.Second)},
		{PhaseAggregation, slotStart.Add(4 * time.Second)},
	}
	for _, tt := range tests {
		got := pt.PhaseStartTime(1, tt.phase)
		if !got.Equal(tt.want) {
			t.Errorf("PhaseStartTime(1, %v) = %v, want %v", tt.phase, got, tt.want)
		}
	}
}

func TestPhaseTimerIsInSlot(t *testing.T) {
	genesis := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	pt := NewPhaseTimer(&PhaseTimerConfig{
		SlotDurationMs:     6000,
		ProposalPhaseMs:    2000,
		AttestationPhaseMs: 2000,
		AggregationPhaseMs: 2000,
		GenesisTime:        genesis.Unix(),
		SlotsPerEpoch:      4,
	})

	// Set time to 7 seconds after genesis (slot 1).
	pt.timeFunc = func() time.Time { return genesis.Add(7 * time.Second) }

	if !pt.IsInSlot(1) {
		t.Error("expected IsInSlot(1) = true at 7s")
	}
	if pt.IsInSlot(0) {
		t.Error("expected IsInSlot(0) = false at 7s")
	}
	if pt.IsInSlot(2) {
		t.Error("expected IsInSlot(2) = false at 7s")
	}
}

func TestPhaseTimerEpochForSlot(t *testing.T) {
	pt := NewPhaseTimer(&PhaseTimerConfig{
		SlotDurationMs:     6000,
		ProposalPhaseMs:    2000,
		AttestationPhaseMs: 2000,
		AggregationPhaseMs: 2000,
		GenesisTime:        0,
		SlotsPerEpoch:      4,
	})

	tests := []struct {
		slot uint64
		want uint64
	}{
		{0, 0},
		{1, 0},
		{3, 0},
		{4, 1},
		{7, 1},
		{8, 2},
		{100, 25},
	}
	for _, tt := range tests {
		got := pt.EpochForSlot(tt.slot)
		if got != tt.want {
			t.Errorf("EpochForSlot(%d) = %d, want %d", tt.slot, got, tt.want)
		}
	}
}

func TestPhaseTimerIsEpochBoundary(t *testing.T) {
	pt := NewPhaseTimer(&PhaseTimerConfig{
		SlotDurationMs:     6000,
		ProposalPhaseMs:    2000,
		AttestationPhaseMs: 2000,
		AggregationPhaseMs: 2000,
		GenesisTime:        0,
		SlotsPerEpoch:      4,
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
		{12, true},
	}
	for _, tt := range tests {
		got := pt.IsEpochBoundary(tt.slot)
		if got != tt.want {
			t.Errorf("IsEpochBoundary(%d) = %v, want %v", tt.slot, got, tt.want)
		}
	}
}

func TestPhaseTimerDefaultConfig(t *testing.T) {
	cfg := DefaultPhaseTimerConfig()
	if cfg.SlotDurationMs != 6000 {
		t.Errorf("SlotDurationMs = %d, want 6000", cfg.SlotDurationMs)
	}
	if cfg.ProposalPhaseMs != 2000 {
		t.Errorf("ProposalPhaseMs = %d, want 2000", cfg.ProposalPhaseMs)
	}
	if cfg.AttestationPhaseMs != 2000 {
		t.Errorf("AttestationPhaseMs = %d, want 2000", cfg.AttestationPhaseMs)
	}
	if cfg.AggregationPhaseMs != 2000 {
		t.Errorf("AggregationPhaseMs = %d, want 2000", cfg.AggregationPhaseMs)
	}
	if cfg.SlotsPerEpoch != 4 {
		t.Errorf("SlotsPerEpoch = %d, want 4", cfg.SlotsPerEpoch)
	}
}

func TestPhaseTimerCustomDuration(t *testing.T) {
	pt := NewPhaseTimer(&PhaseTimerConfig{
		SlotDurationMs:     12000,
		ProposalPhaseMs:    4000,
		AttestationPhaseMs: 4000,
		AggregationPhaseMs: 4000,
		GenesisTime:        0,
		SlotsPerEpoch:      32,
	})

	cfg := pt.Config()
	if cfg.SlotDurationMs != 12000 {
		t.Errorf("SlotDurationMs = %d, want 12000", cfg.SlotDurationMs)
	}

	if pt.SlotDuration() != 12*time.Second {
		t.Errorf("SlotDuration = %v, want 12s", pt.SlotDuration())
	}

	// Phase durations.
	if pt.PhaseDuration(PhaseProposal) != 4*time.Second {
		t.Errorf("ProposalDuration = %v, want 4s", pt.PhaseDuration(PhaseProposal))
	}
	if pt.PhaseDuration(PhaseAttestation) != 4*time.Second {
		t.Errorf("AttestationDuration = %v, want 4s", pt.PhaseDuration(PhaseAttestation))
	}
	if pt.PhaseDuration(PhaseAggregation) != 4*time.Second {
		t.Errorf("AggregationDuration = %v, want 4s", pt.PhaseDuration(PhaseAggregation))
	}
}

func TestPhaseTimerPhaseString(t *testing.T) {
	tests := []struct {
		phase SlotPhase
		want  string
	}{
		{PhaseProposal, "proposal"},
		{PhaseAttestation, "attestation"},
		{PhaseAggregation, "aggregation"},
		{SlotPhase(99), "unknown"},
	}
	for _, tt := range tests {
		got := tt.phase.String()
		if got != tt.want {
			t.Errorf("SlotPhase(%d).String() = %q, want %q", tt.phase, got, tt.want)
		}
	}
}

func TestPhaseTimerMultipleSlots(t *testing.T) {
	genesis := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	pt := NewPhaseTimer(&PhaseTimerConfig{
		SlotDurationMs: 6000, ProposalPhaseMs: 2000,
		AttestationPhaseMs: 2000, AggregationPhaseMs: 2000,
		GenesisTime: genesis.Unix(), SlotsPerEpoch: 4,
	})
	for slot := uint64(0); slot < 20; slot++ {
		want := genesis.Add(time.Duration(slot*6) * time.Second)
		if got := pt.SlotStartTime(slot); !got.Equal(want) {
			t.Errorf("slot %d: start = %v, want %v", slot, got, want)
		}
		p0 := pt.PhaseStartTime(slot, PhaseProposal)
		p1 := pt.PhaseStartTime(slot, PhaseAttestation)
		p2 := pt.PhaseStartTime(slot, PhaseAggregation)
		if !p0.Before(p1) || !p1.Before(p2) {
			t.Errorf("slot %d: phase ordering violated", slot)
		}
	}
}

func TestPhaseTimerGenesisTime(t *testing.T) {
	genesis := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	pt := NewPhaseTimer(&PhaseTimerConfig{
		SlotDurationMs:     6000,
		ProposalPhaseMs:    2000,
		AttestationPhaseMs: 2000,
		AggregationPhaseMs: 2000,
		GenesisTime:        genesis.Unix(),
		SlotsPerEpoch:      4,
	})

	got := pt.GenesisTime()
	if got.Unix() != genesis.Unix() {
		t.Errorf("GenesisTime = %v, want %v", got, genesis)
	}

	// Before genesis => slot 0.
	pt.timeFunc = func() time.Time { return genesis.Add(-10 * time.Second) }
	if pt.CurrentSlot() != 0 {
		t.Errorf("slot before genesis = %d, want 0", pt.CurrentSlot())
	}
	if pt.CurrentPhase() != PhaseProposal {
		t.Errorf("phase before genesis = %v, want proposal", pt.CurrentPhase())
	}
}

func TestPhaseTimerSubscribe(t *testing.T) {
	pt := NewPhaseTimer(nil)
	ch := pt.Subscribe()
	if ch == nil {
		t.Fatal("Subscribe returned nil channel")
	}

	// Send a notification manually.
	evt := SlotEvent{Slot: 42, Phase: PhaseAttestation, Timestamp: time.Now()}
	pt.notify(evt)

	select {
	case got := <-ch:
		if got.Slot != 42 {
			t.Errorf("event slot = %d, want 42", got.Slot)
		}
		if got.Phase != PhaseAttestation {
			t.Errorf("event phase = %v, want attestation", got.Phase)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timed out waiting for event")
	}

	// Unsubscribe.
	pt.Unsubscribe(ch)

	// After unsubscribe, channel should be closed.
	_, ok := <-ch
	if ok {
		t.Error("expected channel to be closed after unsubscribe")
	}
}

func TestPhaseTimerProgressInSlot(t *testing.T) {
	genesis := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	pt := NewPhaseTimer(&PhaseTimerConfig{
		SlotDurationMs:     6000,
		ProposalPhaseMs:    2000,
		AttestationPhaseMs: 2000,
		AggregationPhaseMs: 2000,
		GenesisTime:        genesis.Unix(),
		SlotsPerEpoch:      4,
	})

	// At start of slot.
	pt.timeFunc = func() time.Time { return genesis }
	p := pt.ProgressInSlot()
	if p != 0 {
		t.Errorf("progress at slot start = %f, want 0", p)
	}

	// Halfway through slot (3 seconds).
	pt.timeFunc = func() time.Time { return genesis.Add(3 * time.Second) }
	p = pt.ProgressInSlot()
	if p < 0.49 || p > 0.51 {
		t.Errorf("progress at 3s = %f, want ~0.5", p)
	}
}

func TestPhaseTimerBeforeGenesis(t *testing.T) {
	future := time.Now().Add(1 * time.Hour)
	pt := NewPhaseTimer(&PhaseTimerConfig{
		SlotDurationMs:     6000,
		ProposalPhaseMs:    2000,
		AttestationPhaseMs: 2000,
		AggregationPhaseMs: 2000,
		GenesisTime:        future.Unix(),
		SlotsPerEpoch:      4,
	})

	if pt.CurrentSlot() != 0 {
		t.Errorf("slot before genesis = %d, want 0", pt.CurrentSlot())
	}
	if pt.CurrentPhase() != PhaseProposal {
		t.Errorf("phase before genesis = %v, want proposal", pt.CurrentPhase())
	}
}

func TestPhaseTimerUnequalPhases(t *testing.T) {
	pt := NewPhaseTimer(&PhaseTimerConfig{
		SlotDurationMs: 9000, ProposalPhaseMs: 1000,
		AttestationPhaseMs: 3000, AggregationPhaseMs: 5000,
		SlotsPerEpoch: 4,
	})
	if pt.Config().SlotDurationMs != 9000 {
		t.Errorf("SlotDurationMs = %d, want 9000", pt.Config().SlotDurationMs)
	}
	genesis := time.Unix(0, 0)
	pt.timeFunc = func() time.Time { return genesis.Add(500 * time.Millisecond) }
	if pt.CurrentPhase() != PhaseProposal {
		t.Errorf("phase at 500ms = %v, want proposal", pt.CurrentPhase())
	}
	pt.timeFunc = func() time.Time { return genesis.Add(2000 * time.Millisecond) }
	if pt.CurrentPhase() != PhaseAttestation {
		t.Errorf("phase at 2000ms = %v, want attestation", pt.CurrentPhase())
	}
	pt.timeFunc = func() time.Time { return genesis.Add(5000 * time.Millisecond) }
	if pt.CurrentPhase() != PhaseAggregation {
		t.Errorf("phase at 5000ms = %v, want aggregation", pt.CurrentPhase())
	}
}

func TestPhaseTimerZeroPhases(t *testing.T) {
	pt := NewPhaseTimer(&PhaseTimerConfig{SlotDurationMs: 6000, SlotsPerEpoch: 4})
	cfg := pt.Config()
	if cfg.ProposalPhaseMs != 2000 {
		t.Errorf("auto ProposalPhaseMs = %d, want 2000", cfg.ProposalPhaseMs)
	}
	if cfg.AttestationPhaseMs != 2000 {
		t.Errorf("auto AttestationPhaseMs = %d, want 2000", cfg.AttestationPhaseMs)
	}
	if cfg.AggregationPhaseMs != 2000 {
		t.Errorf("auto AggregationPhaseMs = %d, want 2000", cfg.AggregationPhaseMs)
	}
}
