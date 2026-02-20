package consensus

import (
	"testing"
)

func TestUnifiedBeaconStateNew(t *testing.T) {
	s := NewUnifiedBeaconState(32)
	if s == nil {
		t.Fatal("state should not be nil")
	}
	if s.SlotsPerEpoch != 32 {
		t.Errorf("expected 32 slots per epoch, got %d", s.SlotsPerEpoch)
	}
	if s.ValidatorCountU() != 0 {
		t.Errorf("expected 0 validators, got %d", s.ValidatorCountU())
	}
}

func TestUnifiedBeaconStateNewDefaultSlots(t *testing.T) {
	s := NewUnifiedBeaconState(0)
	if s.SlotsPerEpoch != 32 {
		t.Errorf("expected default 32, got %d", s.SlotsPerEpoch)
	}
}

func TestUnifiedBeaconStateAddValidator(t *testing.T) {
	s := NewUnifiedBeaconState(32)
	v := &UnifiedValidator{
		Pubkey:           [48]byte{1, 2, 3},
		EffectiveBalance: 32 * GweiPerETH,
		Balance:          32 * GweiPerETH,
		ActivationEpoch:  0,
		ExitEpoch:        FarFutureEpoch,
		WithdrawableEpoch: FarFutureEpoch,
		ActivationEligibilityEpoch: FarFutureEpoch,
	}
	idx := s.AddValidator(v)
	if idx != 0 {
		t.Errorf("expected index 0, got %d", idx)
	}
	if s.ValidatorCountU() != 1 {
		t.Errorf("expected 1 validator, got %d", s.ValidatorCountU())
	}

	got, err := s.GetValidator(0)
	if err != nil {
		t.Fatalf("get validator error: %v", err)
	}
	if got.Pubkey != v.Pubkey {
		t.Error("pubkey mismatch")
	}
}

func TestUnifiedBeaconStateGetValidatorOutOfRange(t *testing.T) {
	s := NewUnifiedBeaconState(32)
	_, err := s.GetValidator(999)
	if err != ErrUnifiedIndexOutOfRange {
		t.Fatalf("expected ErrUnifiedIndexOutOfRange, got %v", err)
	}
}

func TestUnifiedBeaconStateValidatorSet(t *testing.T) {
	s := NewUnifiedBeaconState(4)
	for i := 0; i < 5; i++ {
		s.AddValidator(&UnifiedValidator{
			Pubkey:          [48]byte{byte(i)},
			Balance:         32 * GweiPerETH,
			ActivationEpoch: 0,
			ExitEpoch:       FarFutureEpoch,
		})
	}
	set := s.ValidatorSet()
	if len(set) != 5 {
		t.Fatalf("expected 5 validators, got %d", len(set))
	}
}

func TestUnifiedBeaconStateActiveValidatorIndices(t *testing.T) {
	s := NewUnifiedBeaconState(32)
	// Active validator.
	s.AddValidator(&UnifiedValidator{
		ActivationEpoch: 0,
		ExitEpoch:       FarFutureEpoch,
	})
	// Inactive validator (activation in future).
	s.AddValidator(&UnifiedValidator{
		ActivationEpoch: 100,
		ExitEpoch:       FarFutureEpoch,
	})

	indices := s.ActiveValidatorIndices(0)
	if len(indices) != 1 {
		t.Fatalf("expected 1 active validator, got %d", len(indices))
	}
	if indices[0] != 0 {
		t.Errorf("expected index 0, got %d", indices[0])
	}
}

func TestUnifiedBeaconStateTotalActiveBalance(t *testing.T) {
	s := NewUnifiedBeaconState(32)
	s.AddValidator(&UnifiedValidator{
		EffectiveBalance: 32 * GweiPerETH,
		ActivationEpoch:  0,
		ExitEpoch:        FarFutureEpoch,
	})
	s.AddValidator(&UnifiedValidator{
		EffectiveBalance: 64 * GweiPerETH,
		ActivationEpoch:  0,
		ExitEpoch:        FarFutureEpoch,
	})

	total := s.TotalActiveBalance(0)
	expected := 96 * GweiPerETH
	if total != expected {
		t.Errorf("expected %d, got %d", expected, total)
	}
}

func TestUnifiedBeaconStateApplySlot(t *testing.T) {
	s := NewUnifiedBeaconState(32)
	if err := s.ApplySlot(1); err != nil {
		t.Fatalf("ApplySlot(1) failed: %v", err)
	}
	if s.CurrentSlot != 1 {
		t.Errorf("expected slot 1, got %d", s.CurrentSlot)
	}

	// Regression should fail.
	if err := s.ApplySlot(0); err != ErrUnifiedSlotRegression {
		t.Fatalf("expected slot regression error, got %v", err)
	}

	// Same slot should fail.
	if err := s.ApplySlot(1); err != ErrUnifiedSlotRegression {
		t.Fatalf("expected slot regression error for same slot, got %v", err)
	}
}

func TestUnifiedBeaconStateApplyAttestation(t *testing.T) {
	s := NewUnifiedBeaconState(32)
	s.CurrentEpoch = 5

	root := [32]byte{0xAA}
	s.ApplyAttestation(4, 5, root)

	if !s.JustificationBitsU[0] {
		t.Error("bit 0 should be set for current epoch attestation")
	}
	if s.CurrentJustified.Epoch != 5 {
		t.Errorf("expected justified epoch 5, got %d", s.CurrentJustified.Epoch)
	}
}

func TestUnifiedBeaconStateApplyDeposit(t *testing.T) {
	s := NewUnifiedBeaconState(32)
	pk := [48]byte{0xDE}
	wd := [32]byte{0xAB}
	idx := s.ApplyDeposit(pk, wd, 32*GweiPerETH)

	if idx != 0 {
		t.Errorf("expected index 0, got %d", idx)
	}
	v, _ := s.GetValidator(0)
	if v.Pubkey != pk {
		t.Error("pubkey mismatch in deposited validator")
	}
	if v.ActivationEpoch != FarFutureEpoch {
		t.Error("deposited validator should have far future activation")
	}
}

func TestUnifiedBeaconStateApplyWithdrawal(t *testing.T) {
	s := NewUnifiedBeaconState(32)
	s.AddValidator(&UnifiedValidator{Balance: 100 * GweiPerETH})

	if err := s.ApplyWithdrawal(0, 10*GweiPerETH); err != nil {
		t.Fatalf("withdrawal failed: %v", err)
	}
	v, _ := s.GetValidator(0)
	if v.Balance != 90*GweiPerETH {
		t.Errorf("expected 90 ETH balance, got %d", v.Balance)
	}

	// Withdraw more than balance.
	if err := s.ApplyWithdrawal(0, 999*GweiPerETH); err != nil {
		t.Fatalf("over-withdrawal failed: %v", err)
	}
	v, _ = s.GetValidator(0)
	if v.Balance != 0 {
		t.Errorf("expected 0 balance, got %d", v.Balance)
	}

	// Out of range.
	if err := s.ApplyWithdrawal(999, 1); err != ErrUnifiedIndexOutOfRange {
		t.Fatalf("expected index error, got %v", err)
	}
}

func TestUnifiedBeaconStateFinalizationStatus(t *testing.T) {
	s := NewUnifiedBeaconState(32)

	// Initially not finalized.
	epoch, _, isFin := s.FinalizationStatus()
	if isFin {
		t.Error("should not be finalized initially")
	}
	if epoch != 0 {
		t.Errorf("expected epoch 0, got %d", epoch)
	}

	// Set finalized.
	root := [32]byte{0xFF}
	s.SetFinalized(10, root)
	epoch, gotRoot, isFin := s.FinalizationStatus()
	if !isFin {
		t.Error("should be finalized after SetFinalized")
	}
	if epoch != 10 {
		t.Errorf("expected epoch 10, got %d", epoch)
	}
	if gotRoot != root {
		t.Error("root mismatch")
	}
}

func TestUnifiedBeaconStateStateRoot(t *testing.T) {
	s := NewUnifiedBeaconState(32)
	s.AddValidator(&UnifiedValidator{
		Pubkey:  [48]byte{1},
		Balance: 32 * GweiPerETH,
	})

	root1 := s.StateRoot()
	root2 := s.StateRoot()
	if root1 != root2 {
		t.Error("state root should be deterministic")
	}
	if root1.IsZero() {
		t.Error("state root should not be zero")
	}
}

func TestUnifiedBeaconStateCopy(t *testing.T) {
	s := NewUnifiedBeaconState(32)
	s.AddValidator(&UnifiedValidator{
		Pubkey:          [48]byte{0xAA},
		Balance:         50 * GweiPerETH,
		ActivationEpoch: 0,
		ExitEpoch:       FarFutureEpoch,
	})
	s.CurrentSlot = 100
	s.CurrentEpoch = 3

	cp := s.Copy()
	if cp.CurrentSlot != 100 {
		t.Error("copy slot mismatch")
	}
	if cp.ValidatorCountU() != 1 {
		t.Error("copy validator count mismatch")
	}

	// Mutate original, copy should be unaffected.
	s.Validators[0].Balance = 0
	if cp.Validators[0].Balance == 0 {
		t.Error("copy should be independent of original")
	}
}

func TestUnifiedBeaconStateMigrateFromV1(t *testing.T) {
	cfg := DefaultConfig()
	old := NewFullBeaconState(cfg)
	old.AddValidator(&ValidatorBalance{
		Pubkey:           [48]byte{0x01},
		EffectiveBalance: 32 * GweiPerETH,
		ActivationEpoch:  0,
		ExitEpoch:        FarFutureEpoch,
	}, 32*GweiPerETH)
	old.Slot = 64
	old.Epoch = 2

	u, err := MigrateFromV1(old)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	if u.CurrentSlot != 64 {
		t.Errorf("expected slot 64, got %d", u.CurrentSlot)
	}
	if u.ValidatorCountU() != 1 {
		t.Errorf("expected 1 validator, got %d", u.ValidatorCountU())
	}
}

func TestUnifiedBeaconStateMigrateFromV1Nil(t *testing.T) {
	_, err := MigrateFromV1(nil)
	if err != ErrUnifiedNilState {
		t.Fatalf("expected nil state error, got %v", err)
	}
}

func TestUnifiedBeaconStateMigrateFromV2(t *testing.T) {
	old := NewBeaconStateV2(32)
	old.GenesisTime = 1000
	old.Slot = 128
	old.AddValidatorV2(&ValidatorV2{
		Pubkey:                     [48]byte{0x02},
		EffectiveBalance:           32 * GweiPerETH,
		ActivationEpoch:            0,
		ExitEpoch:                  FarFutureEpoch,
		WithdrawableEpoch:          FarFutureEpoch,
		ActivationEligibilityEpoch: 0,
	}, 32*GweiPerETH)

	u, err := MigrateFromV2(old)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	if u.GenesisTime != 1000 {
		t.Errorf("expected genesis time 1000, got %d", u.GenesisTime)
	}
	if u.CurrentSlot != 128 {
		t.Errorf("expected slot 128, got %d", u.CurrentSlot)
	}
	if u.ValidatorCountU() != 1 {
		t.Errorf("expected 1 validator, got %d", u.ValidatorCountU())
	}
}

func TestUnifiedBeaconStateMigrateFromV2Nil(t *testing.T) {
	_, err := MigrateFromV2(nil)
	if err != ErrUnifiedNilState {
		t.Fatalf("expected nil state error, got %v", err)
	}
}

func TestUnifiedBeaconStateMigrateFromModern(t *testing.T) {
	cfg := *DefaultModernBeaconConfig()
	old := NewModernBeaconState(cfg)
	old.SetValidator(0, &ModernValidator{
		Index:           0,
		Balance:         32 * GweiPerETH,
		ActivationEpoch: 0,
		ExitEpoch:       ^uint64(0),
	})
	old.slot = 256
	old.epoch = 8

	u, err := MigrateFromModern(old)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	if u.CurrentSlot != 256 {
		t.Errorf("expected slot 256, got %d", u.CurrentSlot)
	}
	if u.ValidatorCountU() != 1 {
		t.Errorf("expected 1 validator, got %d", u.ValidatorCountU())
	}
}

func TestUnifiedBeaconStateMigrateFromModernNil(t *testing.T) {
	_, err := MigrateFromModern(nil)
	if err != ErrUnifiedNilState {
		t.Fatalf("expected nil state error, got %v", err)
	}
}

func TestUnifiedBeaconStateToMinimalBeaconState(t *testing.T) {
	s := NewUnifiedBeaconState(32)
	s.CurrentSlot = 100
	s.CurrentEpoch = 3
	s.FinalizedCheckpointU = UnifiedCheckpoint{Epoch: 2, Root: [32]byte{0xAB}}

	minimal := s.ToMinimalBeaconState()
	if minimal.Slot != 100 {
		t.Errorf("expected slot 100, got %d", minimal.Slot)
	}
	if minimal.Epoch != 3 {
		t.Errorf("expected epoch 3, got %d", minimal.Epoch)
	}
	if minimal.FinalizedCheckpoint.Epoch != 2 {
		t.Errorf("expected finalized epoch 2, got %d", minimal.FinalizedCheckpoint.Epoch)
	}
}

func TestUnifiedValidatorIsActiveAt(t *testing.T) {
	v := &UnifiedValidator{ActivationEpoch: 5, ExitEpoch: 100}
	if v.IsActiveAt(4) {
		t.Error("should not be active before activation")
	}
	if !v.IsActiveAt(5) {
		t.Error("should be active at activation epoch")
	}
	if !v.IsActiveAt(50) {
		t.Error("should be active mid-range")
	}
	if v.IsActiveAt(100) {
		t.Error("should not be active at exit epoch")
	}
}

func TestUnifiedValidatorIsSlashableAt(t *testing.T) {
	v := &UnifiedValidator{
		ActivationEpoch: 5,
		WithdrawableEpoch: 200,
		Slashed:         false,
	}
	if !v.IsSlashableAt(10) {
		t.Error("should be slashable")
	}

	v.Slashed = true
	if v.IsSlashableAt(10) {
		t.Error("slashed validator should not be slashable")
	}
}
