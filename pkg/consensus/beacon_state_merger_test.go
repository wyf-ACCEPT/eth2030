package consensus

import (
	"testing"
)

func makeTestUnifiedState(slot uint64, epoch Epoch, numValidators int) *UnifiedBeaconState {
	state := NewUnifiedBeaconState(32)
	state.CurrentSlot = slot
	state.CurrentEpoch = epoch
	state.GenesisTime = 1000

	for i := 0; i < numValidators; i++ {
		var pk [48]byte
		pk[0] = byte(i)
		pk[1] = byte(i >> 8)
		state.AddValidator(&UnifiedValidator{
			Pubkey:                     pk,
			EffectiveBalance:           32 * GweiPerETH,
			Balance:                    32 * GweiPerETH,
			ActivationEpoch:            0,
			ExitEpoch:                  FarFutureEpoch,
			ActivationEligibilityEpoch: 0,
			WithdrawableEpoch:          FarFutureEpoch,
		})
	}
	return state
}

func TestStateMergerCreate(t *testing.T) {
	m := NewStateMerger(MergePreferModern)
	if m == nil {
		t.Fatal("expected non-nil merger")
	}
	if m.policy != MergePreferModern {
		t.Errorf("expected MergePreferModern, got %v", m.policy)
	}
}

func TestStateMergerDetectVersion(t *testing.T) {
	m := NewStateMerger(MergePreferModern)

	// Nil state.
	v := m.DetectVersion(nil)
	if v != SpecVersionUnknown {
		t.Errorf("expected Unknown for nil, got %v", v)
	}

	// Empty state (no validators).
	empty := NewUnifiedBeaconState(32)
	v = m.DetectVersion(empty)
	if v != SpecVersionUnknown {
		t.Errorf("expected Unknown for empty state, got %v", v)
	}

	// V1-like: has validators but no fork/genesis.
	v1like := makeTestUnifiedState(10, 0, 5)
	v1like.GenesisTime = 0
	v1like.ForkCurrentVersion = [4]byte{}
	v = m.DetectVersion(v1like)
	if v != SpecVersionV1 {
		t.Errorf("expected V1, got %v", v)
	}

	// Unified: has genesis, fork, and validators.
	unified := makeTestUnifiedState(100, 3, 5)
	unified.ForkCurrentVersion = [4]byte{0x01, 0x00, 0x00, 0x00}
	unified.ForkEpoch = 1
	v = m.DetectVersion(unified)
	if v != SpecVersionUnified {
		t.Errorf("expected Unified, got %v", v)
	}
}

func TestStateMergerMergeState(t *testing.T) {
	m := NewStateMerger(MergePreferModern)

	left := makeTestUnifiedState(100, 3, 3)
	right := makeTestUnifiedState(200, 6, 3)

	merged, err := m.MergeState(left, right)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}

	// Should prefer the higher slot.
	if merged.CurrentSlot != 200 {
		t.Errorf("expected slot 200, got %d", merged.CurrentSlot)
	}
	if merged.CurrentEpoch != 6 {
		t.Errorf("expected epoch 6, got %d", merged.CurrentEpoch)
	}

	// Both states have 3 validators with same pubkeys, so merged should have 3.
	if len(merged.Validators) != 3 {
		t.Errorf("expected 3 validators, got %d", len(merged.Validators))
	}
}

func TestStateMergerMergeStateNilInputs(t *testing.T) {
	m := NewStateMerger(MergePreferModern)

	_, err := m.MergeState(nil, nil)
	if err != ErrMergerNilState {
		t.Errorf("expected ErrMergerNilState, got %v", err)
	}

	state := makeTestUnifiedState(1, 0, 1)
	_, err = m.MergeState(state, nil)
	if err != ErrMergerNilState {
		t.Errorf("expected ErrMergerNilState, got %v", err)
	}
}

func TestStateMergerMergeDisjointValidators(t *testing.T) {
	m := NewStateMerger(MergePreferModern)

	left := NewUnifiedBeaconState(32)
	left.CurrentSlot = 10
	right := NewUnifiedBeaconState(32)
	right.CurrentSlot = 20

	// Add different validators to each.
	var pk1, pk2 [48]byte
	pk1[0] = 0xAA
	pk2[0] = 0xBB
	left.AddValidator(&UnifiedValidator{
		Pubkey:           pk1,
		EffectiveBalance: 32 * GweiPerETH,
		Balance:          32 * GweiPerETH,
		ActivationEpoch:  0,
		ExitEpoch:        FarFutureEpoch,
	})
	right.AddValidator(&UnifiedValidator{
		Pubkey:           pk2,
		EffectiveBalance: 32 * GweiPerETH,
		Balance:          32 * GweiPerETH,
		ActivationEpoch:  0,
		ExitEpoch:        FarFutureEpoch,
	})

	merged, err := m.MergeState(left, right)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}
	if len(merged.Validators) != 2 {
		t.Errorf("expected 2 validators, got %d", len(merged.Validators))
	}
}

func TestStateMergerStrictVersionConflict(t *testing.T) {
	m := NewStateMerger(MergeStrictVersion)

	left := NewUnifiedBeaconState(32)
	left.CurrentSlot = 10
	right := NewUnifiedBeaconState(32)
	right.CurrentSlot = 10

	// Same pubkey, different effective balances.
	var pk [48]byte
	pk[0] = 0xCC
	left.AddValidator(&UnifiedValidator{
		Pubkey:           pk,
		EffectiveBalance: 32 * GweiPerETH,
		Balance:          32 * GweiPerETH,
		ActivationEpoch:  0,
		ExitEpoch:        FarFutureEpoch,
	})
	right.AddValidator(&UnifiedValidator{
		Pubkey:           pk,
		EffectiveBalance: 64 * GweiPerETH, // conflict
		Balance:          64 * GweiPerETH,
		ActivationEpoch:  0,
		ExitEpoch:        FarFutureEpoch,
	})

	_, err := m.MergeState(left, right)
	if err == nil {
		t.Error("expected conflict error in strict mode")
	}
}

func TestStateMergerMigrateToModern(t *testing.T) {
	m := NewStateMerger(MergePreferModern)

	state := NewUnifiedBeaconState(0) // zero SlotsPerEpoch
	state.CurrentSlot = 100
	state.AddValidator(&UnifiedValidator{
		Pubkey:  [48]byte{0x01},
		Balance: 32 * GweiPerETH,
		// Zero ExitEpoch and WithdrawableEpoch (legacy).
	})

	err := m.MigrateToModern(state)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	if state.SlotsPerEpoch != 32 {
		t.Errorf("expected SlotsPerEpoch=32, got %d", state.SlotsPerEpoch)
	}
	if state.CurrentEpoch != Epoch(100/32) {
		t.Errorf("expected epoch %d, got %d", 100/32, state.CurrentEpoch)
	}
	if state.Validators[0].ExitEpoch != FarFutureEpoch {
		t.Errorf("expected FarFutureEpoch for ExitEpoch, got %d", state.Validators[0].ExitEpoch)
	}
}

func TestStateMergerMigrateToModernNil(t *testing.T) {
	m := NewStateMerger(MergePreferModern)
	err := m.MigrateToModern(nil)
	if err != ErrMergerNilState {
		t.Errorf("expected ErrMergerNilState, got %v", err)
	}
}

func TestMergeLog(t *testing.T) {
	m := NewStateMerger(MergePreferModern)
	left := makeTestUnifiedState(10, 0, 1)
	right := makeTestUnifiedState(20, 0, 1)

	_, err := m.MergeState(left, right)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}

	log := m.Log()
	if log.EntryCount() == 0 {
		t.Error("expected non-empty merge log")
	}
	if log.ConflictCount() != 0 {
		t.Errorf("expected 0 conflicts, got %d", log.ConflictCount())
	}
}

func TestMergePolicyString(t *testing.T) {
	tests := []struct {
		p    MergePolicy
		want string
	}{
		{MergePreferModern, "PreferModern"},
		{MergePreferLegacy, "PreferLegacy"},
		{MergeStrictVersion, "StrictVersion"},
		{MergePolicy(99), "Unknown"},
	}
	for _, tt := range tests {
		if got := tt.p.String(); got != tt.want {
			t.Errorf("MergePolicy(%d).String() = %q, want %q", tt.p, got, tt.want)
		}
	}
}

func TestBeaconSpecVersionString(t *testing.T) {
	tests := []struct {
		v    BeaconSpecVersion
		want string
	}{
		{SpecVersionV1, "V1"},
		{SpecVersionV2, "V2"},
		{SpecVersionModern, "Modern"},
		{SpecVersionUnified, "Unified"},
		{SpecVersionUnknown, "Unknown"},
	}
	for _, tt := range tests {
		if got := tt.v.String(); got != tt.want {
			t.Errorf("BeaconSpecVersion(%d).String() = %q, want %q", tt.v, got, tt.want)
		}
	}
}
