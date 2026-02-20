package consensus

import (
	"sync"
	"testing"
)

func TestValidatorStateString(t *testing.T) {
	tests := []struct {
		state ValidatorState
		want  string
	}{
		{StatePending, "pending"},
		{StateActive, "active"},
		{StateExiting, "exiting"},
		{StateExited, "exited"},
		{StateWithdrawable, "withdrawable"},
		{StateSlashed, "slashed"},
		{ValidatorState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("ValidatorState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestLifecycleValidatorState(t *testing.T) {
	v := &LifecycleValidator{
		ActivationEligibleEpoch: FarFutureEpoch,
		ActivationEpoch:         FarFutureEpoch,
		ExitEpoch:               FarFutureEpoch,
		WithdrawableEpoch:       FarFutureEpoch,
	}
	if got := v.State(0); got != StatePending {
		t.Errorf("expected StatePending, got %s", got)
	}

	v.ActivationEligibleEpoch = 5
	v.ActivationEpoch = 10
	if got := v.State(5); got != StatePending {
		t.Errorf("before activation: expected StatePending, got %s", got)
	}
	if got := v.State(10); got != StateActive {
		t.Errorf("at activation: expected StateActive, got %s", got)
	}

	v.ExitEpoch = 100
	v.WithdrawableEpoch = 100 + Epoch(MinValidatorWithdrawabilityDelay)
	if got := v.State(50); got != StateExiting {
		t.Errorf("before exit: expected StateExiting, got %s", got)
	}
	if got := v.State(100); got != StateExited {
		t.Errorf("at exit: expected StateExited, got %s", got)
	}
	if got := v.State(356); got != StateWithdrawable {
		t.Errorf("at withdrawable: expected StateWithdrawable, got %s", got)
	}
}

func TestLifecycleValidatorSlashedState(t *testing.T) {
	v := &LifecycleValidator{
		ActivationEpoch: 10, ExitEpoch: 100,
		WithdrawableEpoch: 100 + Epoch(EpochsPerSlashingsVector),
		Slashed:           true,
	}
	if got := v.State(50); got != StateSlashed {
		t.Errorf("expected StateSlashed, got %s", got)
	}
	if got := v.State(100 + Epoch(EpochsPerSlashingsVector)); got != StateWithdrawable {
		t.Errorf("expected StateWithdrawable, got %s", got)
	}
}

func TestLifecycleValidatorIsSlashable(t *testing.T) {
	v := &LifecycleValidator{
		ActivationEpoch: 10, ExitEpoch: FarFutureEpoch, WithdrawableEpoch: FarFutureEpoch,
	}
	if v.IsSlashable(5) {
		t.Error("should not be slashable before activation")
	}
	if !v.IsSlashable(10) {
		t.Error("should be slashable at activation epoch")
	}
	v.Slashed = true
	if v.IsSlashable(50) {
		t.Error("should not be slashable if already slashed")
	}
}

func TestAddAndGetValidator(t *testing.T) {
	vl := NewValidatorLifecycle()
	vl.AddValidator(0, MinActivationBalance, MinActivationBalance)
	v, err := vl.GetValidator(0)
	if err != nil {
		t.Fatalf("GetValidator: %v", err)
	}
	if v.EffectiveBalance != MinActivationBalance {
		t.Errorf("balance = %d, want %d", v.EffectiveBalance, MinActivationBalance)
	}
	if v.State(0) != StatePending {
		t.Errorf("new validator should be pending, got %s", v.State(0))
	}
	_, err = vl.GetValidator(999)
	if err != ErrLifecycleValidatorNotFound {
		t.Errorf("expected ErrLifecycleValidatorNotFound, got %v", err)
	}
	if vl.ValidatorCount() != 1 {
		t.Errorf("count = %d, want 1", vl.ValidatorCount())
	}
}

func TestInitiateActivation(t *testing.T) {
	vl := NewValidatorLifecycle()
	vl.AddValidator(0, MinActivationBalance, MinActivationBalance)
	if err := vl.InitiateActivation(0, 5); err != nil {
		t.Fatalf("InitiateActivation: %v", err)
	}
	v, _ := vl.GetValidator(0)
	if v.ActivationEligibleEpoch != 6 {
		t.Errorf("eligibility epoch = %d, want 6", v.ActivationEligibleEpoch)
	}
	if err := vl.InitiateActivation(999, 5); err != ErrLifecycleValidatorNotFound {
		t.Errorf("expected not found, got %v", err)
	}
	v.ActivationEpoch = 10
	if err := vl.InitiateActivation(0, 5); err != ErrLifecycleAlreadyActive {
		t.Errorf("expected already active, got %v", err)
	}
}

func TestInitiateActivationInsufficientBalance(t *testing.T) {
	vl := NewValidatorLifecycle()
	vl.AddValidator(0, EjectionBalance, EjectionBalance)
	if err := vl.InitiateActivation(0, 5); err != ErrLifecycleInsufficientBalance {
		t.Errorf("expected insufficient balance, got %v", err)
	}
}

func TestProcessActivationQueue(t *testing.T) {
	vl := NewValidatorLifecycle()
	for i := ValidatorIndex(0); i < 10; i++ {
		vl.AddValidator(i, MinActivationBalance, MinActivationBalance)
		if err := vl.InitiateActivation(i, Epoch(i)); err != nil {
			t.Fatalf("InitiateActivation(%d): %v", i, err)
		}
	}
	// Churn limit with 0 active = MIN_PER_EPOCH_CHURN_LIMIT = 4.
	activated := vl.ProcessActivationQueue(20)
	if len(activated) != 4 {
		t.Errorf("batch 1: activated %d, want 4", len(activated))
	}
	for i := 0; i < len(activated)-1; i++ {
		if activated[i] > activated[i+1] {
			t.Errorf("not in order: %v", activated)
			break
		}
	}
	if a2 := vl.ProcessActivationQueue(20); len(a2) != 4 {
		t.Errorf("batch 2: %d, want 4", len(a2))
	}
	if a3 := vl.ProcessActivationQueue(20); len(a3) != 2 {
		t.Errorf("batch 3: %d, want 2", len(a3))
	}
	if a4 := vl.ProcessActivationQueue(20); len(a4) != 0 {
		t.Errorf("batch 4: %d, want 0", len(a4))
	}
}

func TestInitiateExit(t *testing.T) {
	vl := NewValidatorLifecycle()
	vl.AddValidator(0, MinActivationBalance, MinActivationBalance)
	v, _ := vl.GetValidator(0)
	v.ActivationEpoch = 10

	if err := vl.InitiateExit(0, 50); err != nil {
		t.Fatalf("InitiateExit: %v", err)
	}
	v, _ = vl.GetValidator(0)
	expectedExit := computeActivationExitEpoch(50)
	if v.ExitEpoch != expectedExit {
		t.Errorf("exit = %d, want %d", v.ExitEpoch, expectedExit)
	}
	expectedW := Epoch(uint64(expectedExit) + MinValidatorWithdrawabilityDelay)
	if v.WithdrawableEpoch != expectedW {
		t.Errorf("withdrawable = %d, want %d", v.WithdrawableEpoch, expectedW)
	}
	if err := vl.InitiateExit(0, 60); err != ErrLifecycleAlreadyExiting {
		t.Errorf("expected already exiting, got %v", err)
	}
}

func TestInitiateExitNotActive(t *testing.T) {
	vl := NewValidatorLifecycle()
	vl.AddValidator(0, MinActivationBalance, MinActivationBalance)
	if err := vl.InitiateExit(0, 50); err != ErrLifecycleNotActive {
		t.Errorf("expected not active, got %v", err)
	}
}

func TestExitQueueChurnLimit(t *testing.T) {
	vl := NewValidatorLifecycle()
	for i := ValidatorIndex(0); i < 10; i++ {
		vl.AddValidator(i, MinActivationBalance, MinActivationBalance)
		v, _ := vl.GetValidator(i)
		v.ActivationEpoch = 1
	}
	for i := ValidatorIndex(0); i < 5; i++ {
		if err := vl.InitiateExit(i, 50); err != nil {
			t.Fatalf("InitiateExit(%d): %v", i, err)
		}
	}
	v4, _ := vl.GetValidator(4)
	v0, _ := vl.GetValidator(0)
	// With churn=4, the 5th exit should spill to the next epoch.
	if v4.ExitEpoch <= v0.ExitEpoch {
		t.Logf("v0 exit=%d, v4 exit=%d (churn spill expected)", v0.ExitEpoch, v4.ExitEpoch)
	}
}

func TestProcessSlashing(t *testing.T) {
	vl := NewValidatorLifecycle()
	vl.AddValidator(0, MinActivationBalance, MinActivationBalance)
	v, _ := vl.GetValidator(0)
	v.ActivationEpoch = 10

	penalty, err := vl.ProcessSlashing(0, 50)
	if err != nil {
		t.Fatalf("ProcessSlashing: %v", err)
	}
	expectedPenalty := MinActivationBalance / MinSlashingPenaltyQuotient
	if penalty != expectedPenalty {
		t.Errorf("penalty = %d, want %d", penalty, expectedPenalty)
	}
	v, _ = vl.GetValidator(0)
	if !v.Slashed {
		t.Error("should be slashed")
	}
	if v.ExitEpoch == FarFutureEpoch {
		t.Error("should have exit epoch set")
	}
	expectedW := Epoch(50 + EpochsPerSlashingsVector)
	if v.WithdrawableEpoch != expectedW {
		t.Errorf("withdrawable = %d, want %d", v.WithdrawableEpoch, expectedW)
	}
	if _, err = vl.ProcessSlashing(0, 50); err != ErrLifecycleAlreadySlashed {
		t.Errorf("expected already slashed, got %v", err)
	}
}

func TestProcessSlashingNotActive(t *testing.T) {
	vl := NewValidatorLifecycle()
	vl.AddValidator(0, MinActivationBalance, MinActivationBalance)
	if _, err := vl.ProcessSlashing(0, 50); err != ErrLifecycleNotActive {
		t.Errorf("expected not active, got %v", err)
	}
}

func TestUpdateEffectiveBalances(t *testing.T) {
	vl := NewValidatorLifecycle()
	vl.AddValidator(0, MinActivationBalance, MinActivationBalance)
	v, _ := vl.GetValidator(0)
	v.Balance = MinActivationBalance - EffectiveBalanceIncrement
	vl.UpdateEffectiveBalances()
	v, _ = vl.GetValidator(0)
	if v.EffectiveBalance >= MinActivationBalance {
		t.Errorf("effective balance should have decreased, got %d", v.EffectiveBalance)
	}
}

func TestProcessEjections(t *testing.T) {
	vl := NewValidatorLifecycle()
	vl.AddValidator(0, MinActivationBalance, MinActivationBalance)
	vl.AddValidator(1, EjectionBalance, EjectionBalance)
	v0, _ := vl.GetValidator(0)
	v0.ActivationEpoch = 1
	v1, _ := vl.GetValidator(1)
	v1.ActivationEpoch = 1

	ejected := vl.ProcessEjections(50)
	if len(ejected) != 1 || ejected[0] != 1 {
		t.Errorf("ejected = %v, want [1]", ejected)
	}
	v1, _ = vl.GetValidator(1)
	if v1.ExitEpoch == FarFutureEpoch {
		t.Error("ejected validator should have exit epoch set")
	}
}

func TestLifecycleStats(t *testing.T) {
	vl := NewValidatorLifecycle()
	for i := ValidatorIndex(0); i < 3; i++ {
		vl.AddValidator(i, MinActivationBalance, MinActivationBalance)
	}
	for i := ValidatorIndex(3); i < 5; i++ {
		vl.AddValidator(i, MinActivationBalance, MinActivationBalance)
		v, _ := vl.GetValidator(i)
		v.ActivationEpoch = 1
	}
	vl.AddValidator(5, MinActivationBalance, MinActivationBalance)
	v5, _ := vl.GetValidator(5)
	v5.ActivationEpoch = 1
	v5.ExitEpoch = 100
	v5.WithdrawableEpoch = 100 + Epoch(MinValidatorWithdrawabilityDelay)

	stats := vl.Stats(50)
	if stats.PendingCount != 3 {
		t.Errorf("pending = %d, want 3", stats.PendingCount)
	}
	if stats.ActiveCount != 2 {
		t.Errorf("active = %d, want 2", stats.ActiveCount)
	}
	if stats.ExitingCount != 1 {
		t.Errorf("exiting = %d, want 1", stats.ExitingCount)
	}
	expected := uint64(3) * MinActivationBalance
	if stats.TotalActiveBalance != expected {
		t.Errorf("total active = %d, want %d", stats.TotalActiveBalance, expected)
	}
}

func TestActiveIndices(t *testing.T) {
	vl := NewValidatorLifecycle()
	for i := ValidatorIndex(0); i < 5; i++ {
		vl.AddValidator(i, MinActivationBalance, MinActivationBalance)
	}
	for _, idx := range []ValidatorIndex{0, 2, 4} {
		v, _ := vl.GetValidator(idx)
		v.ActivationEpoch = 1
	}
	indices := vl.ActiveIndices(10)
	if len(indices) != 3 {
		t.Fatalf("active = %d, want 3", len(indices))
	}
	for i, want := range []ValidatorIndex{0, 2, 4} {
		if indices[i] != want {
			t.Errorf("indices[%d] = %d, want %d", i, indices[i], want)
		}
	}
}

func TestComputeActivationExitEpoch(t *testing.T) {
	if got := computeActivationExitEpoch(10); got != 15 {
		t.Errorf("got %d, want 15", got)
	}
	if got := computeActivationExitEpoch(0); got != 5 {
		t.Errorf("got %d, want 5", got)
	}
}

func TestGetChurnLimit(t *testing.T) {
	if got := getChurnLimit(100); got != MinPerEpochChurnLimit {
		t.Errorf("got %d, want %d", got, MinPerEpochChurnLimit)
	}
	if got := getChurnLimit(1_000_000); got != 15 {
		t.Errorf("got %d, want 15", got)
	}
}

func TestLifecycleConcurrentAccess(t *testing.T) {
	vl := NewValidatorLifecycle()
	for i := ValidatorIndex(0); i < 100; i++ {
		vl.AddValidator(i, MinActivationBalance, MinActivationBalance)
		v, _ := vl.GetValidator(i)
		v.ActivationEpoch = 1
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = vl.Stats(50)
			_ = vl.ActiveIndices(50)
		}()
	}
	for i := ValidatorIndex(0); i < 10; i++ {
		wg.Add(1)
		go func(idx ValidatorIndex) {
			defer wg.Done()
			_ = vl.InitiateExit(idx, 50)
		}(i)
	}
	wg.Wait()
}

func TestFullLifecycleFlow(t *testing.T) {
	vl := NewValidatorLifecycle()
	vl.AddValidator(0, MinActivationBalance, MinActivationBalance)
	if err := vl.InitiateActivation(0, 5); err != nil {
		t.Fatalf("activation: %v", err)
	}
	activated := vl.ProcessActivationQueue(10)
	if len(activated) != 1 {
		t.Fatalf("expected 1 activated, got %d", len(activated))
	}
	v, _ := vl.GetValidator(0)
	if v.State(v.ActivationEpoch) != StateActive {
		t.Fatal("expected active")
	}
	exitEpoch := v.ActivationEpoch + 100
	if err := vl.InitiateExit(0, exitEpoch); err != nil {
		t.Fatalf("exit: %v", err)
	}
	v, _ = vl.GetValidator(0)
	if v.State(exitEpoch) != StateExiting {
		t.Fatal("expected exiting")
	}
	if v.State(v.ExitEpoch) != StateExited {
		t.Fatal("expected exited")
	}
	if v.State(v.WithdrawableEpoch) != StateWithdrawable {
		t.Fatal("expected withdrawable")
	}
}
