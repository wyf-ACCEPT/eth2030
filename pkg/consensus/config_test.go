package consensus

import "testing"

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.SecondsPerSlot != 12 {
		t.Errorf("expected 12s slots, got %d", cfg.SecondsPerSlot)
	}
	if cfg.SlotsPerEpoch != 32 {
		t.Errorf("expected 32 slots/epoch, got %d", cfg.SlotsPerEpoch)
	}
	if cfg.EpochsForFinality != 2 {
		t.Errorf("expected 2-epoch finality, got %d", cfg.EpochsForFinality)
	}
	if cfg.IsSingleEpochFinality() {
		t.Error("default config should not be single-epoch finality")
	}
}

func TestQuickSlotsConfig(t *testing.T) {
	cfg := QuickSlotsConfig()
	if cfg.SecondsPerSlot != 6 {
		t.Errorf("expected 6s slots, got %d", cfg.SecondsPerSlot)
	}
	if cfg.SlotsPerEpoch != 4 {
		t.Errorf("expected 4 slots/epoch, got %d", cfg.SlotsPerEpoch)
	}
	if cfg.EpochsForFinality != 1 {
		t.Errorf("expected 1-epoch finality, got %d", cfg.EpochsForFinality)
	}
	if !cfg.IsSingleEpochFinality() {
		t.Error("quick slots config should be single-epoch finality")
	}
}

func TestEpochDuration(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.EpochDuration() != 12*32 {
		t.Errorf("expected epoch duration 384, got %d", cfg.EpochDuration())
	}
	qs := QuickSlotsConfig()
	if qs.EpochDuration() != 6*4 {
		t.Errorf("expected epoch duration 24, got %d", qs.EpochDuration())
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *ConsensusConfig
		wantErr bool
	}{
		{"default", DefaultConfig(), false},
		{"quick", QuickSlotsConfig(), false},
		{"zero slot", &ConsensusConfig{SecondsPerSlot: 0, SlotsPerEpoch: 32, EpochsForFinality: 2}, true},
		{"zero epoch", &ConsensusConfig{SecondsPerSlot: 12, SlotsPerEpoch: 0, EpochsForFinality: 2}, true},
		{"zero finality", &ConsensusConfig{SecondsPerSlot: 12, SlotsPerEpoch: 32, EpochsForFinality: 0}, true},
		{"custom valid", &ConsensusConfig{SecondsPerSlot: 3, SlotsPerEpoch: 8, EpochsForFinality: 1}, false},
	}
	for _, tt := range tests {
		err := tt.cfg.Validate()
		if (err != nil) != tt.wantErr {
			t.Errorf("%s: Validate() error = %v, wantErr %v", tt.name, err, tt.wantErr)
		}
	}
}
