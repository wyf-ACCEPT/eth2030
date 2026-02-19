package consensus

import (
	"testing"
)

func TestConstants(t *testing.T) {
	if MinActivationBalance != 32_000_000_000 {
		t.Errorf("MinActivationBalance = %d, want 32_000_000_000", MinActivationBalance)
	}
	if MaxEffectiveBalance != 2_048_000_000_000 {
		t.Errorf("MaxEffectiveBalance = %d, want 2_048_000_000_000", MaxEffectiveBalance)
	}
	if MaxEffectiveBalance/GweiPerETH != 2048 {
		t.Errorf("MaxEffectiveBalance in ETH = %d, want 2048", MaxEffectiveBalance/GweiPerETH)
	}
}

func TestValidatorBalance_IsActive(t *testing.T) {
	v := &ValidatorBalance{
		ActivationEpoch: 10,
		ExitEpoch:       20,
	}

	tests := []struct {
		epoch  Epoch
		active bool
	}{
		{0, false},
		{9, false},
		{10, true},
		{15, true},
		{19, true},
		{20, false},
		{100, false},
	}

	for _, tt := range tests {
		if got := v.IsActive(tt.epoch); got != tt.active {
			t.Errorf("IsActive(%d) = %v, want %v", tt.epoch, got, tt.active)
		}
	}
}

func TestValidatorBalance_IsEligibleForActivation(t *testing.T) {
	tests := []struct {
		name     string
		v        ValidatorBalance
		eligible bool
	}{
		{
			name: "eligible - sufficient balance, not activated, not slashed",
			v: ValidatorBalance{
				ActivationEpoch:  FarFutureEpoch,
				EffectiveBalance: MinActivationBalance,
				Slashed:          false,
			},
			eligible: true,
		},
		{
			name: "not eligible - already activated",
			v: ValidatorBalance{
				ActivationEpoch:  10,
				EffectiveBalance: MinActivationBalance,
				Slashed:          false,
			},
			eligible: false,
		},
		{
			name: "not eligible - slashed",
			v: ValidatorBalance{
				ActivationEpoch:  FarFutureEpoch,
				EffectiveBalance: MinActivationBalance,
				Slashed:          true,
			},
			eligible: false,
		},
		{
			name: "not eligible - insufficient balance",
			v: ValidatorBalance{
				ActivationEpoch:  FarFutureEpoch,
				EffectiveBalance: MinActivationBalance - 1,
				Slashed:          false,
			},
			eligible: false,
		},
		{
			name: "eligible - large balance (EIP-7251)",
			v: ValidatorBalance{
				ActivationEpoch:  FarFutureEpoch,
				EffectiveBalance: 1000 * GweiPerETH,
				Slashed:          false,
			},
			eligible: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.v.IsEligibleForActivation(); got != tt.eligible {
				t.Errorf("IsEligibleForActivation() = %v, want %v", got, tt.eligible)
			}
		})
	}
}

func TestValidatorBalance_HasCompoundingCredentials(t *testing.T) {
	tests := []struct {
		name   string
		creds  [32]byte
		expect bool
	}{
		{"0x02 prefix", func() [32]byte { var b [32]byte; b[0] = 0x02; return b }(), true},
		{"0x01 prefix", func() [32]byte { var b [32]byte; b[0] = 0x01; return b }(), false},
		{"0x00 prefix", func() [32]byte { var b [32]byte; return b }(), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &ValidatorBalance{WithdrawalCredentials: tt.creds}
			if got := v.HasCompoundingCredentials(); got != tt.expect {
				t.Errorf("HasCompoundingCredentials() = %v, want %v", got, tt.expect)
			}
		})
	}
}

func TestValidatorSet_AddGetRemove(t *testing.T) {
	vs := NewValidatorSet()

	pubkey1 := [48]byte{1}
	pubkey2 := [48]byte{2}

	v1 := &ValidatorBalance{
		Pubkey:           pubkey1,
		EffectiveBalance: 32 * GweiPerETH,
		ActivationEpoch:  0,
		ExitEpoch:        FarFutureEpoch,
	}
	v2 := &ValidatorBalance{
		Pubkey:           pubkey2,
		EffectiveBalance: 64 * GweiPerETH,
		ActivationEpoch:  0,
		ExitEpoch:        FarFutureEpoch,
	}

	// Add validators.
	if err := vs.Add(v1); err != nil {
		t.Fatalf("Add(v1) unexpected error: %v", err)
	}
	if err := vs.Add(v2); err != nil {
		t.Fatalf("Add(v2) unexpected error: %v", err)
	}

	// Duplicate add should fail.
	if err := vs.Add(v1); err != ErrValidatorAlreadyAdded {
		t.Errorf("Add duplicate: got %v, want ErrValidatorAlreadyAdded", err)
	}

	// Get.
	got, err := vs.Get(pubkey1)
	if err != nil {
		t.Fatalf("Get(pubkey1) error: %v", err)
	}
	if got.EffectiveBalance != 32*GweiPerETH {
		t.Errorf("v1 effective balance = %d, want %d", got.EffectiveBalance, 32*GweiPerETH)
	}

	// Get nonexistent.
	_, err = vs.Get([48]byte{99})
	if err != ErrValidatorNotFound {
		t.Errorf("Get nonexistent: got %v, want ErrValidatorNotFound", err)
	}

	// Len.
	if vs.Len() != 2 {
		t.Errorf("Len() = %d, want 2", vs.Len())
	}

	// ActiveCount.
	if c := vs.ActiveCount(0); c != 2 {
		t.Errorf("ActiveCount(0) = %d, want 2", c)
	}

	// Remove.
	if err := vs.Remove(pubkey1); err != nil {
		t.Fatalf("Remove(pubkey1) error: %v", err)
	}
	if vs.Len() != 1 {
		t.Errorf("Len() after remove = %d, want 1", vs.Len())
	}

	// Remove nonexistent.
	if err := vs.Remove(pubkey1); err != ErrValidatorNotFound {
		t.Errorf("Remove nonexistent: got %v, want ErrValidatorNotFound", err)
	}
}

func TestComputeEffectiveBalance(t *testing.T) {
	tests := []struct {
		name            string
		balance         uint64
		currentEff      uint64
		expectedEff     uint64
	}{
		{
			name:        "exact 32 ETH",
			balance:     32 * GweiPerETH,
			currentEff:  32 * GweiPerETH,
			expectedEff: 32 * GweiPerETH,
		},
		{
			name:        "balance drops below hysteresis",
			balance:     31 * GweiPerETH,
			currentEff:  32 * GweiPerETH,
			expectedEff: 31 * GweiPerETH,
		},
		{
			name:        "balance within hysteresis (no change)",
			balance:     32*GweiPerETH - 100_000_000, // 31.9 ETH
			currentEff:  32 * GweiPerETH,
			expectedEff: 32 * GweiPerETH,
		},
		{
			name:        "balance increases past hysteresis",
			balance:     34 * GweiPerETH,
			currentEff:  32 * GweiPerETH,
			expectedEff: 34 * GweiPerETH,
		},
		{
			name:        "large balance capped at MaxEffectiveBalance",
			balance:     3000 * GweiPerETH,
			currentEff:  32 * GweiPerETH,
			expectedEff: MaxEffectiveBalance,
		},
		{
			name:        "balance at max",
			balance:     MaxEffectiveBalance,
			currentEff:  MaxEffectiveBalance,
			expectedEff: MaxEffectiveBalance,
		},
		{
			name:        "zero balance",
			balance:     0,
			currentEff:  32 * GweiPerETH,
			expectedEff: 0,
		},
		{
			name:        "initial activation at 32 ETH",
			balance:     32 * GweiPerETH,
			currentEff:  0,
			expectedEff: 32 * GweiPerETH,
		},
		{
			name:        "initial activation at 100 ETH (EIP-7251)",
			balance:     100 * GweiPerETH,
			currentEff:  0,
			expectedEff: 100 * GweiPerETH,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeEffectiveBalance(tt.balance, tt.currentEff)
			if got != tt.expectedEff {
				t.Errorf("ComputeEffectiveBalance(%d, %d) = %d, want %d",
					tt.balance, tt.currentEff, got, tt.expectedEff)
			}
		})
	}
}

func TestUpdateEffectiveBalance(t *testing.T) {
	v := &ValidatorBalance{
		EffectiveBalance: 0,
	}

	// From zero, a 64 ETH balance should snap to 64 ETH effective.
	UpdateEffectiveBalance(v, 64*GweiPerETH)
	if v.EffectiveBalance != 64*GweiPerETH {
		t.Errorf("after update with 64 ETH: effective = %d, want %d",
			v.EffectiveBalance, 64*GweiPerETH)
	}

	// With 2100 ETH balance, should cap at MaxEffectiveBalance.
	UpdateEffectiveBalance(v, 2100*GweiPerETH)
	if v.EffectiveBalance != MaxEffectiveBalance {
		t.Errorf("after update with 2100 ETH: effective = %d, want %d",
			v.EffectiveBalance, MaxEffectiveBalance)
	}
}
