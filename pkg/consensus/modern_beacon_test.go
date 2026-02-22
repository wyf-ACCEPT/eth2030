package consensus

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestDefaultModernBeaconConfig(t *testing.T) {
	cfg := DefaultModernBeaconConfig()
	if cfg.SlotsPerEpoch != 32 {
		t.Errorf("SlotsPerEpoch = %d, want 32", cfg.SlotsPerEpoch)
	}
	if cfg.MaxValidators != 1<<20 {
		t.Errorf("MaxValidators = %d, want %d", cfg.MaxValidators, 1<<20)
	}
	if cfg.MaxEffectiveBalance != 2048*GweiPerETH {
		t.Errorf("MaxEffectiveBalance = %d, want %d", cfg.MaxEffectiveBalance, 2048*GweiPerETH)
	}
	if cfg.EjectionBalance != 16*GweiPerETH {
		t.Errorf("EjectionBalance = %d, want %d", cfg.EjectionBalance, 16*GweiPerETH)
	}
}

func TestNewModernBeaconState(t *testing.T) {
	s := NewModernBeaconState(ModernBeaconConfig{})
	if s == nil {
		t.Fatal("NewModernBeaconState returned nil")
	}
	if s.config.SlotsPerEpoch != 32 {
		t.Errorf("default SlotsPerEpoch = %d, want 32", s.config.SlotsPerEpoch)
	}
	// Custom config.
	s2 := NewModernBeaconState(ModernBeaconConfig{SlotsPerEpoch: 4, MaxValidators: 100})
	if s2.config.SlotsPerEpoch != 4 || s2.config.MaxValidators != 100 {
		t.Error("custom config not applied")
	}
}

func TestProcessSlot(t *testing.T) {
	s := NewModernBeaconState(*DefaultModernBeaconConfig())
	if err := s.ProcessSlot(1); err != nil {
		t.Fatalf("ProcessSlot(1) error: %v", err)
	}
	s.mu.RLock()
	if s.slot != 1 || s.epoch != 0 {
		t.Errorf("slot=%d epoch=%d, want 1/0", s.slot, s.epoch)
	}
	s.mu.RUnlock()

	// Epoch boundary.
	s2 := NewModernBeaconState(ModernBeaconConfig{SlotsPerEpoch: 4})
	s2.ProcessSlot(4)
	s2.mu.RLock()
	if s2.epoch != 1 {
		t.Errorf("epoch at slot 4 = %d, want 1", s2.epoch)
	}
	s2.mu.RUnlock()

	// Regression should fail.
	if err := s.ProcessSlot(0); err != ErrModernSlotRegression {
		t.Errorf("expected ErrModernSlotRegression, got %v", err)
	}

	// Genesis slot 0 should work on fresh state.
	s3 := NewModernBeaconState(*DefaultModernBeaconConfig())
	if err := s3.ProcessSlot(0); err != nil {
		t.Errorf("slot 0 at genesis should succeed: %v", err)
	}
}

func TestProcessEpoch(t *testing.T) {
	s := NewModernBeaconState(*DefaultModernBeaconConfig())
	s.SetValidator(0, &ModernValidator{
		Balance: 32 * GweiPerETH, ActivationEpoch: 0, ExitEpoch: ^uint64(0),
	})
	if err := s.ProcessEpoch(0); err != nil {
		t.Fatalf("ProcessEpoch(0) error: %v", err)
	}
	v, _ := s.GetValidator(0)
	if v.EffectiveBalance != 32*GweiPerETH {
		t.Errorf("effective balance = %d, want %d", v.EffectiveBalance, 32*GweiPerETH)
	}

	// State root should be stored.
	s.mu.RLock()
	root, ok := s.stateRoots[0]
	s.mu.RUnlock()
	if !ok || root.IsZero() {
		t.Error("state root not stored or zero for epoch 0")
	}

	// Regression.
	s.ProcessEpoch(5)
	if err := s.ProcessEpoch(3); err != ErrModernEpochRegression {
		t.Errorf("expected ErrModernEpochRegression, got %v", err)
	}
}

func TestProcessEpoch_CapsAndEjects(t *testing.T) {
	// Cap effective balance.
	s := NewModernBeaconState(ModernBeaconConfig{MaxEffectiveBalance: 64 * GweiPerETH})
	s.SetValidator(0, &ModernValidator{
		Balance: 200 * GweiPerETH, ActivationEpoch: 0, ExitEpoch: ^uint64(0),
	})
	s.ProcessEpoch(0)
	v, _ := s.GetValidator(0)
	if v.EffectiveBalance != 64*GweiPerETH {
		t.Errorf("effective balance = %d, want %d (capped)", v.EffectiveBalance, 64*GweiPerETH)
	}

	// Eject low balance.
	s2 := NewModernBeaconState(ModernBeaconConfig{EjectionBalance: 16 * GweiPerETH})
	s2.SetValidator(0, &ModernValidator{
		Balance: 10 * GweiPerETH, ActivationEpoch: 0, ExitEpoch: ^uint64(0),
	})
	s2.ProcessEpoch(5)
	v2, _ := s2.GetValidator(0)
	if v2.ExitEpoch != 6 {
		t.Errorf("ExitEpoch = %d, want 6", v2.ExitEpoch)
	}

	// Slashed validators not ejected for low balance.
	s3 := NewModernBeaconState(ModernBeaconConfig{EjectionBalance: 16 * GweiPerETH})
	s3.SetValidator(0, &ModernValidator{
		Balance: 10 * GweiPerETH, Slashed: true, ActivationEpoch: 0, ExitEpoch: ^uint64(0),
	})
	s3.ProcessEpoch(5)
	v3, _ := s3.GetValidator(0)
	if v3.ExitEpoch != ^uint64(0) {
		t.Error("slashed validator should not be ejected")
	}

	// Inactive validators not affected.
	s4 := NewModernBeaconState(*DefaultModernBeaconConfig())
	s4.SetValidator(0, &ModernValidator{
		Balance: 10 * GweiPerETH, ActivationEpoch: 100, ExitEpoch: ^uint64(0),
	})
	s4.ProcessEpoch(5)
	v4, _ := s4.GetValidator(0)
	if v4.EffectiveBalance != 0 || v4.ExitEpoch != ^uint64(0) {
		t.Error("inactive validator should not be affected")
	}
}

func TestGetSetValidator(t *testing.T) {
	s := NewModernBeaconState(*DefaultModernBeaconConfig())

	// Not found.
	if _, err := s.GetValidator(999); err != ErrModernValidatorNotFound {
		t.Errorf("expected ErrModernValidatorNotFound, got %v", err)
	}

	// Set nil.
	if err := s.SetValidator(0, nil); err != ErrModernValidatorNotFound {
		t.Errorf("expected error for nil, got %v", err)
	}

	// Set and get.
	s.SetValidator(42, &ModernValidator{Balance: 100, WithdrawalCredentials: []byte{0x01}})
	v, err := s.GetValidator(42)
	if err != nil {
		t.Fatalf("GetValidator error: %v", err)
	}
	if v.Index != 42 || v.Balance != 100 {
		t.Errorf("Index=%d Balance=%d, want 42/100", v.Index, v.Balance)
	}

	// Returns a deep copy.
	v.Balance = 0
	v.WithdrawalCredentials[0] = 0xff
	v2, _ := s.GetValidator(42)
	if v2.Balance != 100 || v2.WithdrawalCredentials[0] != 0x01 {
		t.Error("GetValidator should return a deep copy")
	}

	// Max validators enforcement.
	s2 := NewModernBeaconState(ModernBeaconConfig{MaxValidators: 2})
	s2.SetValidator(0, &ModernValidator{})
	s2.SetValidator(1, &ModernValidator{})
	if err := s2.SetValidator(2, &ModernValidator{}); err != ErrModernMaxValidators {
		t.Errorf("expected ErrModernMaxValidators, got %v", err)
	}
	// Updating existing should still work.
	if err := s2.SetValidator(0, &ModernValidator{Balance: 999}); err != nil {
		t.Errorf("update existing should succeed: %v", err)
	}
}

func TestGetActiveValidators(t *testing.T) {
	s := NewModernBeaconState(*DefaultModernBeaconConfig())
	s.SetValidator(5, &ModernValidator{ActivationEpoch: 0, ExitEpoch: ^uint64(0)})
	s.SetValidator(1, &ModernValidator{ActivationEpoch: 0, ExitEpoch: ^uint64(0)})
	s.SetValidator(3, &ModernValidator{ActivationEpoch: 0, ExitEpoch: ^uint64(0)})
	s.SetValidator(9, &ModernValidator{ActivationEpoch: 100, ExitEpoch: ^uint64(0)}) // not active
	s.SetValidator(7, &ModernValidator{ActivationEpoch: 0, ExitEpoch: 0})            // exited

	active := s.GetActiveValidators(0)
	if len(active) != 3 {
		t.Fatalf("active count = %d, want 3", len(active))
	}
	// Sorted by index.
	if active[0].Index != 1 || active[1].Index != 3 || active[2].Index != 5 {
		t.Errorf("not sorted: [%d, %d, %d]", active[0].Index, active[1].Index, active[2].Index)
	}
}

func TestCalculateCommittees(t *testing.T) {
	s := NewModernBeaconState(ModernBeaconConfig{SlotsPerEpoch: 4})

	// No validators.
	if _, err := s.CalculateCommittees(0); err != ErrModernNoValidators {
		t.Errorf("expected ErrModernNoValidators, got %v", err)
	}

	for i := uint64(0); i < 8; i++ {
		s.SetValidator(i, &ModernValidator{ActivationEpoch: 0, ExitEpoch: ^uint64(0)})
	}

	committees, err := s.CalculateCommittees(0)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(committees) != 4 {
		t.Fatalf("count = %d, want 4", len(committees))
	}

	// All 8 validators appear exactly once.
	seen := make(map[uint64]bool)
	for _, comm := range committees {
		for _, idx := range comm {
			if seen[idx] {
				t.Errorf("validator %d duplicated", idx)
			}
			seen[idx] = true
		}
	}
	if len(seen) != 8 {
		t.Errorf("total = %d, want 8", len(seen))
	}

	// Deterministic: same epoch -> same result.
	c1, _ := s.CalculateCommittees(5)
	c2, _ := s.CalculateCommittees(5)
	for i := range c1 {
		for j := range c1[i] {
			if c1[i][j] != c2[i][j] {
				t.Fatal("committees not deterministic")
			}
		}
	}

	// Different epochs differ.
	c3, _ := s.CalculateCommittees(1)
	allSame := true
	for i := range c1 {
		if len(c1[i]) != len(c3[i]) {
			allSame = false
			break
		}
		for j := range c1[i] {
			if c1[i][j] != c3[i][j] {
				allSame = false
				break
			}
		}
	}
	if allSame {
		t.Error("different epochs should produce different committees")
	}
}

func TestStateRoot(t *testing.T) {
	s := NewModernBeaconState(*DefaultModernBeaconConfig())
	s.SetValidator(0, &ModernValidator{Balance: 32 * GweiPerETH})

	r1 := s.StateRoot()
	if r1.IsZero() {
		t.Error("StateRoot should not be zero")
	}
	if r1 != s.StateRoot() {
		t.Error("StateRoot should be deterministic")
	}

	// Different state -> different root.
	s2 := NewModernBeaconState(*DefaultModernBeaconConfig())
	s2.SetValidator(0, &ModernValidator{Balance: 64 * GweiPerETH})
	if s.StateRoot() == s2.StateRoot() {
		t.Error("different states should produce different roots")
	}

	// Checkpoints affect root.
	s3 := s.CopyState()
	s3.UpdateJustification(1, types.HexToHash("0x01"))
	if s.StateRoot() == s3.StateRoot() {
		t.Error("root should change with justified checkpoint")
	}
}

func TestCopyState(t *testing.T) {
	s := NewModernBeaconState(ModernBeaconConfig{SlotsPerEpoch: 4})
	s.ProcessSlot(5)
	s.SetValidator(0, &ModernValidator{
		Balance: 32 * GweiPerETH, ActivationEpoch: 0, ExitEpoch: ^uint64(0),
		WithdrawalCredentials: []byte{0x01, 0x02},
	})
	s.UpdateJustification(1, types.HexToHash("0xaa"))
	s.Finalize(0, types.HexToHash("0xbb"))
	s.ProcessEpoch(0)

	cp := s.CopyState()

	// Same state root initially.
	if s.StateRoot() != cp.StateRoot() {
		t.Error("copy should have same state root")
	}

	// Mutate copy, original unaffected.
	cp.SetValidator(0, &ModernValidator{Balance: 999})
	v, _ := s.GetValidator(0)
	if v.Balance != 32*GweiPerETH {
		t.Error("modifying copy affected original")
	}

	// State roots diverge.
	if s.StateRoot() == cp.StateRoot() {
		t.Error("mutated copy should differ")
	}
}

func TestJustificationAndFinalization(t *testing.T) {
	s := NewModernBeaconState(*DefaultModernBeaconConfig())

	// Defaults are zero.
	jcp := s.GetJustifiedCheckpoint()
	fcp := s.GetFinalizedCheckpoint()
	if uint64(jcp.Epoch) != 0 || !jcp.Root.IsZero() {
		t.Error("default justified should be zero")
	}
	if uint64(fcp.Epoch) != 0 || !fcp.Root.IsZero() {
		t.Error("default finalized should be zero")
	}

	// Invalid: epoch 0 with zero root.
	if err := s.UpdateJustification(0, types.Hash{}); err != ErrModernInvalidEpoch {
		t.Errorf("expected ErrModernInvalidEpoch, got %v", err)
	}

	// Valid: epoch 0 with non-zero root.
	if err := s.UpdateJustification(0, types.HexToHash("0x01")); err != nil {
		t.Errorf("epoch 0 with root should succeed: %v", err)
	}

	// Update and read.
	s.UpdateJustification(5, types.HexToHash("0xabcd"))
	jcp = s.GetJustifiedCheckpoint()
	if uint64(jcp.Epoch) != 5 || jcp.Root != types.HexToHash("0xabcd") {
		t.Error("justified checkpoint not updated")
	}

	s.Finalize(3, types.HexToHash("0xdead"))
	fcp = s.GetFinalizedCheckpoint()
	if uint64(fcp.Epoch) != 3 || fcp.Root != types.HexToHash("0xdead") {
		t.Error("finalized checkpoint not updated")
	}

	// Returns copies.
	jcp.Epoch = 999
	if uint64(s.GetJustifiedCheckpoint().Epoch) != 5 {
		t.Error("GetJustifiedCheckpoint should return a copy")
	}
}

func TestModernBeaconState_Concurrent(t *testing.T) {
	s := NewModernBeaconState(ModernBeaconConfig{SlotsPerEpoch: 4})
	var wg sync.WaitGroup
	for i := uint64(0); i < 50; i++ {
		wg.Add(2)
		go func(idx uint64) {
			defer wg.Done()
			s.SetValidator(idx, &ModernValidator{
				Balance: 32 * GweiPerETH, ActivationEpoch: 0, ExitEpoch: ^uint64(0),
			})
		}(i)
		go func(idx uint64) {
			defer wg.Done()
			s.GetValidator(idx)
			s.GetActiveValidators(0)
			s.StateRoot()
		}(i)
	}
	wg.Wait()

	if active := s.GetActiveValidators(0); len(active) != 50 {
		t.Errorf("active after concurrent ops = %d, want 50", len(active))
	}
}

func TestModernValidator_IsActive(t *testing.T) {
	tests := []struct {
		name  string
		v     ModernValidator
		epoch uint64
		want  bool
	}{
		{"active", ModernValidator{ActivationEpoch: 0, ExitEpoch: 100}, 50, true},
		{"at activation", ModernValidator{ActivationEpoch: 5, ExitEpoch: 100}, 5, true},
		{"before activation", ModernValidator{ActivationEpoch: 5, ExitEpoch: 100}, 4, false},
		{"at exit", ModernValidator{ActivationEpoch: 0, ExitEpoch: 10}, 10, false},
		{"after exit", ModernValidator{ActivationEpoch: 0, ExitEpoch: 10}, 11, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.v.isActive(tt.epoch); got != tt.want {
				t.Errorf("isActive(%d) = %v, want %v", tt.epoch, got, tt.want)
			}
		})
	}
}

func TestShuffleIndices(t *testing.T) {
	a := []uint64{0, 1, 2, 3, 4, 5, 6, 7}
	b := []uint64{0, 1, 2, 3, 4, 5, 6, 7}
	shuffleIndices(a, 42)
	shuffleIndices(b, 42)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("not deterministic at %d: %d vs %d", i, a[i], b[i])
		}
	}

	// Different seeds differ.
	c := []uint64{0, 1, 2, 3, 4, 5, 6, 7}
	shuffleIndices(c, 0)
	allSame := true
	for i := range a {
		if a[i] != c[i] {
			allSame = false
			break
		}
	}
	if allSame {
		t.Error("different seeds should produce different shuffles")
	}
}
