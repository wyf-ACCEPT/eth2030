package consensus

import (
	"testing"
)

func TestDefaultAttesterCapConfig(t *testing.T) {
	cfg := DefaultAttesterCapConfig()
	if cfg.MaxAttesterBalance != DefaultAttesterCap {
		t.Errorf("expected %d, got %d", DefaultAttesterCap, cfg.MaxAttesterBalance)
	}
	expectedCap := uint64(128) * GweiPerETH
	if cfg.MaxAttesterBalance != expectedCap {
		t.Errorf("expected 128 ETH = %d Gwei, got %d", expectedCap, cfg.MaxAttesterBalance)
	}
}

func TestIsCapActive(t *testing.T) {
	cfg := &AttesterCapConfig{CapEpoch: 50}

	if IsCapActive(49, cfg) {
		t.Error("cap should not be active before CapEpoch")
	}
	if !IsCapActive(50, cfg) {
		t.Error("cap should be active at CapEpoch")
	}
	if !IsCapActive(100, cfg) {
		t.Error("cap should be active after CapEpoch")
	}
}

func TestCapEffectiveBalance(t *testing.T) {
	cap := uint64(128 * GweiPerETH)

	tests := []struct {
		name    string
		balance uint64
		maxCap  uint64
		want    uint64
	}{
		{"below cap", 32 * GweiPerETH, cap, 32 * GweiPerETH},
		{"at cap", cap, cap, cap},
		{"above cap", 2048 * GweiPerETH, cap, cap},
		{"zero balance", 0, cap, 0},
		{"zero cap", 100, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CapEffectiveBalance(tt.balance, tt.maxCap)
			if got != tt.want {
				t.Errorf("CapEffectiveBalance(%d, %d) = %d, want %d",
					tt.balance, tt.maxCap, got, tt.want)
			}
		})
	}
}

func TestApplyAttesterCap(t *testing.T) {
	vs := NewValidatorSet()
	cap := uint64(128 * GweiPerETH)

	// Add validators with various balances.
	vs.Add(&ValidatorBalance{
		Pubkey:           [48]byte{1},
		EffectiveBalance: 2048 * GweiPerETH, // way above cap
		ActivationEpoch:  0,
		ExitEpoch:        FarFutureEpoch,
	})
	vs.Add(&ValidatorBalance{
		Pubkey:           [48]byte{2},
		EffectiveBalance: 64 * GweiPerETH, // below cap
		ActivationEpoch:  0,
		ExitEpoch:        FarFutureEpoch,
	})
	vs.Add(&ValidatorBalance{
		Pubkey:           [48]byte{3},
		EffectiveBalance: 128 * GweiPerETH, // at cap
		ActivationEpoch:  0,
		ExitEpoch:        FarFutureEpoch,
	})

	config := &AttesterCapConfig{MaxAttesterBalance: cap, CapEpoch: 0}
	ApplyAttesterCap(vs, config, Epoch(5))

	v1, _ := vs.Get([48]byte{1})
	if v1.EffectiveBalance != cap {
		t.Errorf("validator 1: expected %d, got %d", cap, v1.EffectiveBalance)
	}

	v2, _ := vs.Get([48]byte{2})
	if v2.EffectiveBalance != 64*GweiPerETH {
		t.Errorf("validator 2: expected %d, got %d", 64*GweiPerETH, v2.EffectiveBalance)
	}

	v3, _ := vs.Get([48]byte{3})
	if v3.EffectiveBalance != cap {
		t.Errorf("validator 3: expected %d, got %d", cap, v3.EffectiveBalance)
	}
}

func TestApplyAttesterCap_InactiveUnaffected(t *testing.T) {
	vs := NewValidatorSet()
	cap := uint64(128 * GweiPerETH)

	// Inactive validator (exited at epoch 5).
	vs.Add(&ValidatorBalance{
		Pubkey:           [48]byte{1},
		EffectiveBalance: 2048 * GweiPerETH,
		ActivationEpoch:  0,
		ExitEpoch:        5,
	})

	config := &AttesterCapConfig{MaxAttesterBalance: cap, CapEpoch: 0}
	ApplyAttesterCap(vs, config, Epoch(10))

	v, _ := vs.Get([48]byte{1})
	if v.EffectiveBalance != 2048*GweiPerETH {
		t.Errorf("inactive validator should not be capped: got %d", v.EffectiveBalance)
	}
}

func TestApplyAttesterCap_PreActivation(t *testing.T) {
	vs := NewValidatorSet()
	vs.Add(&ValidatorBalance{
		Pubkey:           [48]byte{1},
		EffectiveBalance: 2048 * GweiPerETH,
		ActivationEpoch:  0,
		ExitEpoch:        FarFutureEpoch,
	})

	config := &AttesterCapConfig{MaxAttesterBalance: 128 * GweiPerETH, CapEpoch: 100}
	ApplyAttesterCap(vs, config, Epoch(50)) // before CapEpoch

	v, _ := vs.Get([48]byte{1})
	if v.EffectiveBalance != 2048*GweiPerETH {
		t.Errorf("cap should not apply before CapEpoch: got %d", v.EffectiveBalance)
	}
}

func TestTotalCappedWeight(t *testing.T) {
	vs := NewValidatorSet()
	vs.Add(&ValidatorBalance{
		Pubkey:           [48]byte{1},
		EffectiveBalance: 2048 * GweiPerETH,
		ActivationEpoch:  0,
		ExitEpoch:        FarFutureEpoch,
	})
	vs.Add(&ValidatorBalance{
		Pubkey:           [48]byte{2},
		EffectiveBalance: 64 * GweiPerETH,
		ActivationEpoch:  0,
		ExitEpoch:        FarFutureEpoch,
	})

	cap := uint64(128 * GweiPerETH)
	config := &AttesterCapConfig{MaxAttesterBalance: cap, CapEpoch: 0}

	total := TotalCappedWeight(vs, config, Epoch(1))
	// 2048 ETH capped to 128 ETH + 64 ETH = 192 ETH in Gwei.
	expected := (128 + 64) * GweiPerETH
	if total != expected {
		t.Errorf("expected total %d, got %d", expected, total)
	}
}

func TestTotalCappedWeight_PreCap(t *testing.T) {
	vs := NewValidatorSet()
	vs.Add(&ValidatorBalance{
		Pubkey:           [48]byte{1},
		EffectiveBalance: 2048 * GweiPerETH,
		ActivationEpoch:  0,
		ExitEpoch:        FarFutureEpoch,
	})

	config := &AttesterCapConfig{MaxAttesterBalance: 128 * GweiPerETH, CapEpoch: 100}

	total := TotalCappedWeight(vs, config, Epoch(50))
	if total != 2048*GweiPerETH {
		t.Errorf("before cap epoch, should use full balance: got %d", total)
	}
}
