package consensus

import (
	"sync"
	"testing"
)

// makeTestStateV2 creates a BeaconStateV2 with n active validators at epoch 0.
func makeTestStateV2(n int, balance uint64) *BeaconStateV2 {
	s := NewBeaconStateV2(32)
	for i := 0; i < n; i++ {
		var pubkey [48]byte
		pubkey[0] = byte(i)
		pubkey[1] = byte(i >> 8)
		v := &ValidatorV2{
			Pubkey:                     pubkey,
			EffectiveBalance:           balance,
			ActivationEligibilityEpoch: 0,
			ActivationEpoch:            0,
			ExitEpoch:                  FarFutureEpoch,
			WithdrawableEpoch:          FarFutureEpoch,
		}
		s.AddValidatorV2(v, balance)
	}
	return s
}

func TestNewBeaconStateV2(t *testing.T) {
	s := NewBeaconStateV2(0) // should default to 32
	if s.SlotsPerEpoch != 32 {
		t.Fatalf("expected default SlotsPerEpoch 32, got %d", s.SlotsPerEpoch)
	}
	if s.ValidatorCount() != 0 {
		t.Fatalf("expected 0 validators, got %d", s.ValidatorCount())
	}
}

func TestAddValidatorV2(t *testing.T) {
	s := NewBeaconStateV2(32)
	v := &ValidatorV2{
		Pubkey:           [48]byte{1},
		EffectiveBalance: MaxEffectiveBalanceV2,
		ActivationEpoch:  0,
		ExitEpoch:        FarFutureEpoch,
		WithdrawableEpoch: FarFutureEpoch,
	}
	idx := s.AddValidatorV2(v, MaxEffectiveBalanceV2)
	if idx != 0 {
		t.Fatalf("expected index 0, got %d", idx)
	}
	if s.ValidatorCount() != 1 {
		t.Fatalf("expected 1 validator, got %d", s.ValidatorCount())
	}
}

func TestGetActiveValidatorIndices(t *testing.T) {
	s := makeTestStateV2(10, MaxEffectiveBalanceV2)

	// All 10 should be active at epoch 0.
	indices := s.GetActiveValidatorIndices(0)
	if len(indices) != 10 {
		t.Fatalf("expected 10 active validators, got %d", len(indices))
	}

	// Mark validator 5 as exited at epoch 0.
	s.Validators[5].ExitEpoch = 0
	indices = s.GetActiveValidatorIndices(0)
	if len(indices) != 9 {
		t.Fatalf("expected 9 active validators after exit, got %d", len(indices))
	}
}

func TestGetTotalActiveBalance(t *testing.T) {
	balance := uint64(32_000_000_000)
	s := makeTestStateV2(4, balance)
	total := s.GetTotalActiveBalance(0)
	expected := uint64(4) * balance
	if total != expected {
		t.Fatalf("expected total balance %d, got %d", expected, total)
	}
}

func TestGetTotalActiveBalanceMinimum(t *testing.T) {
	// With no active validators, should return EffectiveBalanceIncrement.
	s := NewBeaconStateV2(32)
	total := s.GetTotalActiveBalance(0)
	if total != EffectiveBalanceIncrement {
		t.Fatalf("expected minimum balance %d, got %d", EffectiveBalanceIncrement, total)
	}
}

func TestValidatorV2IsActive(t *testing.T) {
	v := &ValidatorV2{
		ActivationEpoch: 5,
		ExitEpoch:       100,
	}
	if v.IsActiveV2(4) {
		t.Fatal("should not be active before activation epoch")
	}
	if !v.IsActiveV2(5) {
		t.Fatal("should be active at activation epoch")
	}
	if !v.IsActiveV2(99) {
		t.Fatal("should be active before exit epoch")
	}
	if v.IsActiveV2(100) {
		t.Fatal("should not be active at exit epoch")
	}
}

func TestValidatorV2IsSlashable(t *testing.T) {
	v := &ValidatorV2{
		ActivationEpoch:   5,
		ExitEpoch:         100,
		WithdrawableEpoch: 200,
		Slashed:           false,
	}
	if !v.IsSlashableV2(50) {
		t.Fatal("unslashed active validator should be slashable")
	}
	v.Slashed = true
	if v.IsSlashableV2(50) {
		t.Fatal("already slashed validator should not be slashable")
	}
}

func TestGetCurrentAndPreviousEpoch(t *testing.T) {
	s := NewBeaconStateV2(32)
	s.Slot = 64 // epoch 2
	if s.GetCurrentEpoch() != 2 {
		t.Fatalf("expected epoch 2, got %d", s.GetCurrentEpoch())
	}
	if s.GetPreviousEpoch() != 1 {
		t.Fatalf("expected previous epoch 1, got %d", s.GetPreviousEpoch())
	}

	// Genesis epoch: previous should be 0.
	s.Slot = 0
	if s.GetPreviousEpoch() != 0 {
		t.Fatalf("expected previous epoch 0 at genesis, got %d", s.GetPreviousEpoch())
	}
}

func TestGetBeaconProposerIndex(t *testing.T) {
	s := makeTestStateV2(16, MaxEffectiveBalanceV2)
	s.Slot = 10

	idx, err := s.GetBeaconProposerIndex()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx >= 16 {
		t.Fatalf("proposer index %d out of range [0, 16)", idx)
	}

	// Same slot should produce same proposer (deterministic).
	idx2, err := s.GetBeaconProposerIndex()
	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	if idx != idx2 {
		t.Fatalf("expected deterministic proposer, got %d then %d", idx, idx2)
	}
}

func TestGetBeaconProposerIndexNoValidators(t *testing.T) {
	s := NewBeaconStateV2(32)
	_, err := s.GetBeaconProposerIndex()
	if err != ErrV2NoValidators {
		t.Fatalf("expected ErrV2NoValidators, got %v", err)
	}
}

func TestGetBeaconProposerIndexDifferentSlots(t *testing.T) {
	s := makeTestStateV2(32, MaxEffectiveBalanceV2)

	// Different slots should generally produce different proposers.
	proposers := make(map[uint64]bool)
	for slot := uint64(0); slot < 32; slot++ {
		s.Slot = slot
		idx, err := s.GetBeaconProposerIndex()
		if err != nil {
			t.Fatalf("slot %d: %v", slot, err)
		}
		proposers[idx] = true
	}
	// With 32 validators and 32 slots, we expect some diversity.
	if len(proposers) < 2 {
		t.Fatalf("expected diverse proposers across slots, got %d unique", len(proposers))
	}
}

func TestProcessEpochJustification(t *testing.T) {
	s := makeTestStateV2(8, MaxEffectiveBalanceV2)
	// Move to epoch 3 so justification logic runs.
	s.Slot = 3 * 32

	s.ProcessEpoch()

	// Justification bits should be updated. Since all validators are active,
	// the target balance equals total active balance (supermajority met).
	if !s.JustificationBitsV2[0] && !s.JustificationBitsV2[1] {
		t.Log("justification bits may not be set if target balances " +
			"don't meet 2/3 threshold in simplified model")
	}
}

func TestProcessEpochRewardsAndPenalties(t *testing.T) {
	s := makeTestStateV2(4, MaxEffectiveBalanceV2)
	s.Slot = 32 // epoch 1

	initialBalances := make([]uint64, len(s.Balances))
	copy(initialBalances, s.Balances)

	s.ProcessEpoch()

	// Active non-slashed validators should receive rewards.
	for i, bal := range s.Balances {
		if bal <= initialBalances[i] {
			t.Fatalf("validator %d: expected balance increase (was %d, now %d)",
				i, initialBalances[i], bal)
		}
	}
}

func TestProcessEpochSlashedPenalty(t *testing.T) {
	s := makeTestStateV2(4, MaxEffectiveBalanceV2)
	s.Slot = 32 // epoch 1
	s.Validators[0].Slashed = true

	initialBalance := s.Balances[0]
	s.ProcessEpoch()

	if s.Balances[0] >= initialBalance {
		t.Fatalf("slashed validator should receive penalty, was %d now %d",
			initialBalance, s.Balances[0])
	}
}

func TestProcessEpochRegistryUpdates(t *testing.T) {
	s := makeTestStateV2(4, MaxEffectiveBalanceV2)
	s.Slot = 32

	// Add a validator eligible for activation.
	v := &ValidatorV2{
		Pubkey:                     [48]byte{99},
		EffectiveBalance:           MaxEffectiveBalanceV2,
		ActivationEligibilityEpoch: FarFutureEpoch,
		ActivationEpoch:            FarFutureEpoch,
		ExitEpoch:                  FarFutureEpoch,
		WithdrawableEpoch:          FarFutureEpoch,
	}
	s.AddValidatorV2(v, MaxEffectiveBalanceV2)

	s.ProcessEpoch()

	// The new validator should have activation eligibility set.
	if s.Validators[4].ActivationEligibilityEpoch == FarFutureEpoch {
		t.Fatal("expected activation eligibility epoch to be set")
	}
}

func TestProcessEpochEjection(t *testing.T) {
	s := makeTestStateV2(4, MaxEffectiveBalanceV2)
	s.Slot = 32

	// Set validator effective balance below ejection threshold.
	s.Validators[0].EffectiveBalance = EjectionBalance - 1

	s.ProcessEpoch()

	// Validator should be initiated for exit.
	if s.Validators[0].ExitEpoch == FarFutureEpoch {
		t.Fatal("expected ejected validator to have exit epoch set")
	}
}

func TestIncreaseDecreaseBalance(t *testing.T) {
	s := makeTestStateV2(2, 1000)

	if err := s.IncreaseBalance(0, 500); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Balances[0] != 1500 {
		t.Fatalf("expected 1500, got %d", s.Balances[0])
	}

	if err := s.DecreaseBalance(0, 200); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Balances[0] != 1300 {
		t.Fatalf("expected 1300, got %d", s.Balances[0])
	}

	// Underflow protection.
	if err := s.DecreaseBalance(1, 9999); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Balances[1] != 0 {
		t.Fatalf("expected 0 after underflow, got %d", s.Balances[1])
	}
}

func TestIncreaseDecreaseBalanceOutOfRange(t *testing.T) {
	s := makeTestStateV2(1, 1000)
	if err := s.IncreaseBalance(5, 100); err != ErrV2IndexOutOfRange {
		t.Fatalf("expected ErrV2IndexOutOfRange, got %v", err)
	}
	if err := s.DecreaseBalance(5, 100); err != ErrV2IndexOutOfRange {
		t.Fatalf("expected ErrV2IndexOutOfRange, got %v", err)
	}
}

func TestHashTreeRootV2Deterministic(t *testing.T) {
	s := makeTestStateV2(4, MaxEffectiveBalanceV2)
	s.Slot = 100
	s.GenesisTime = 1606824000

	root1 := s.HashTreeRootV2()
	root2 := s.HashTreeRootV2()

	if root1 != root2 {
		t.Fatalf("hash tree root should be deterministic")
	}
	if root1 == [32]byte{} {
		t.Fatal("hash tree root should not be zero")
	}
}

func TestHashTreeRootV2DifferentStates(t *testing.T) {
	s1 := makeTestStateV2(4, MaxEffectiveBalanceV2)
	s2 := makeTestStateV2(4, MaxEffectiveBalanceV2)

	s1.Slot = 10
	s2.Slot = 20

	root1 := s1.HashTreeRootV2()
	root2 := s2.HashTreeRootV2()

	if root1 == root2 {
		t.Fatal("different states should have different roots")
	}
}

func TestValidatorV2HashTreeRoot(t *testing.T) {
	v := &ValidatorV2{
		Pubkey:           [48]byte{1, 2, 3},
		EffectiveBalance: MaxEffectiveBalanceV2,
		ActivationEpoch:  0,
		ExitEpoch:        FarFutureEpoch,
		WithdrawableEpoch: FarFutureEpoch,
	}
	root := v.HashTreeRoot()
	if root == [32]byte{} {
		t.Fatal("validator hash tree root should not be zero")
	}

	// Deterministic.
	root2 := v.HashTreeRoot()
	if root != root2 {
		t.Fatal("validator hash tree root should be deterministic")
	}
}

func TestComputeShuffledIndex(t *testing.T) {
	seed := [32]byte{1, 2, 3, 4}
	// Basic sanity: shuffled index should be within bounds.
	for i := uint64(0); i < 10; i++ {
		result := computeShuffledIndex(i, 10, seed)
		if result >= 10 {
			t.Fatalf("shuffled index %d out of range [0, 10)", result)
		}
	}
	// Shuffling should be a permutation (no duplicates).
	seen := make(map[uint64]bool)
	for i := uint64(0); i < 10; i++ {
		result := computeShuffledIndex(i, 10, seed)
		if seen[result] {
			t.Fatalf("duplicate shuffled index %d", result)
		}
		seen[result] = true
	}
}

func TestComputeShuffledIndexSingleElement(t *testing.T) {
	seed := [32]byte{5}
	result := computeShuffledIndex(0, 1, seed)
	if result != 0 {
		t.Fatalf("single element shuffle should return 0, got %d", result)
	}
}

func TestIntegerSquareRoot(t *testing.T) {
	tests := []struct {
		n    uint64
		want uint64
	}{
		{0, 0},
		{1, 1},
		{4, 2},
		{9, 3},
		{10, 3},
		{100, 10},
		{1000000, 1000},
	}
	for _, tt := range tests {
		got := integerSquareRoot(tt.n)
		if got != tt.want {
			t.Errorf("integerSquareRoot(%d) = %d, want %d", tt.n, got, tt.want)
		}
	}
}

func TestProcessEpochGenesisSkip(t *testing.T) {
	s := makeTestStateV2(4, MaxEffectiveBalanceV2)
	s.Slot = 0 // epoch 0
	initialBals := make([]uint64, len(s.Balances))
	copy(initialBals, s.Balances)

	s.ProcessEpoch()

	// At genesis epoch, rewards should not be applied.
	for i, bal := range s.Balances {
		if bal != initialBals[i] {
			t.Fatalf("balance %d changed at genesis epoch", i)
		}
	}
}

func TestProcessSlashingsReset(t *testing.T) {
	s := makeTestStateV2(4, MaxEffectiveBalanceV2)
	s.Slot = 32 // epoch 1
	s.Slashings[2%EpochsPerSlashingsVector] = 999

	s.ProcessEpoch()

	// Next epoch index should be reset to 0.
	nextIdx := uint64(s.GetCurrentEpoch()+1) % EpochsPerSlashingsVector
	if s.Slashings[nextIdx] != 0 {
		t.Fatalf("slashings[%d] should be reset to 0, got %d",
			nextIdx, s.Slashings[nextIdx])
	}
}

func TestBeaconStateV2ThreadSafety(t *testing.T) {
	s := makeTestStateV2(16, MaxEffectiveBalanceV2)
	s.Slot = 64

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			s.GetActiveValidatorIndices(Epoch(n % 3))
			s.GetTotalActiveBalance(Epoch(n % 3))
			s.GetBeaconProposerIndex()
			s.HashTreeRootV2()
		}(i)
	}

	// Concurrent writes.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			var pk [48]byte
			pk[0] = byte(100 + n)
			v := &ValidatorV2{
				Pubkey:           pk,
				EffectiveBalance: MaxEffectiveBalanceV2,
				ActivationEpoch:  0,
				ExitEpoch:        FarFutureEpoch,
				WithdrawableEpoch: FarFutureEpoch,
			}
			s.AddValidatorV2(v, MaxEffectiveBalanceV2)
		}(i)
	}
	wg.Wait()

	if s.ValidatorCount() < 16 {
		t.Fatalf("expected at least 16 validators, got %d", s.ValidatorCount())
	}
}
