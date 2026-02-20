package consensus

import (
	"testing"
)

// makeETState creates a test EpochTransitionState with n active validators.
func makeETState(n int, slotsPerEpoch, currentSlot uint64) *EpochTransitionState {
	validators := make([]*ValidatorV2, n)
	balances := make([]uint64, n)
	inactScores := make([]uint64, n)
	for i := 0; i < n; i++ {
		validators[i] = &ValidatorV2{
			EffectiveBalance:           32 * GweiPerETH,
			ActivationEligibilityEpoch: 0,
			ActivationEpoch:            0,
			ExitEpoch:                  FarFutureEpoch,
			WithdrawableEpoch:          FarFutureEpoch,
		}
		balances[i] = 32 * GweiPerETH
	}

	// BlockRoots for justification.
	blockRoots := make([]Checkpoint, slotsPerEpoch*4)
	for i := range blockRoots {
		blockRoots[i] = Checkpoint{Epoch: Epoch(i)}
	}

	return &EpochTransitionState{
		CurrentSlot:      currentSlot,
		SlotsPerEpoch:    slotsPerEpoch,
		Validators:       validators,
		Balances:         balances,
		Slashings:        make([]uint64, EpochsPerSlashingsVector),
		InactivityScores: inactScores,
		JustBits:         [4]bool{},
		PrevJustCP:       Checkpoint{},
		CurrJustCP:       Checkpoint{},
		FinalizedCP:      Checkpoint{},
		BlockRoots:       blockRoots,
		Participation:    NewEpochTransitionParticipation(),
	}
}

func TestEpochTransitionNilState(t *testing.T) {
	et := NewEpochTransition()
	_, err := et.ProcessEpoch(nil)
	if err != ErrETNilState {
		t.Fatalf("expected ErrETNilState, got %v", err)
	}
}

func TestEpochTransitionNoValidators(t *testing.T) {
	et := NewEpochTransition()
	state := &EpochTransitionState{
		CurrentSlot:   64,
		SlotsPerEpoch: 32,
		Validators:    nil,
		Balances:      nil,
	}
	_, err := et.ProcessEpoch(state)
	if err != ErrETNoValidators {
		t.Fatalf("expected ErrETNoValidators, got %v", err)
	}
}

func TestEpochTransitionMismatch(t *testing.T) {
	et := NewEpochTransition()
	state := &EpochTransitionState{
		CurrentSlot:   64,
		SlotsPerEpoch: 32,
		Validators: []*ValidatorV2{{
			EffectiveBalance: 32 * GweiPerETH,
			ActivationEpoch:  0,
			ExitEpoch:        FarFutureEpoch,
		}},
		Balances: []uint64{1, 2},
	}
	_, err := et.ProcessEpoch(state)
	if err != ErrETMismatch {
		t.Fatalf("expected ErrETMismatch, got %v", err)
	}
}

func TestEpochTransitionEpochZero(t *testing.T) {
	et := NewEpochTransition()
	state := makeETState(4, 32, 16) // slot 16 = epoch 0
	_, err := et.ProcessEpoch(state)
	if err != ErrETEarlyEpoch {
		t.Fatalf("expected ErrETEarlyEpoch, got %v", err)
	}
}

func TestEpochTransitionRewardsWithFullParticipation(t *testing.T) {
	et := NewEpochTransition()
	state := makeETState(8, 32, 64) // slot 64 = epoch 2

	// Set all validators as previous-epoch attesters.
	for i := 0; i < 8; i++ {
		idx := ValidatorIndex(i)
		state.Participation.PrevSource[idx] = true
		state.Participation.PrevTarget[idx] = true
		state.Participation.PrevHead[idx] = true
	}

	// Set justified checkpoints so we don't finalize (keep delay low).
	state.CurrJustCP = Checkpoint{Epoch: 1}
	state.FinalizedCP = Checkpoint{Epoch: 0}

	result, err := et.ProcessEpoch(state)
	if err != nil {
		t.Fatalf("ProcessEpoch failed: %v", err)
	}

	// All rewards should be positive for full participation.
	for i, r := range result.Rewards {
		if r <= 0 {
			t.Errorf("validator %d reward = %d, expected positive", i, r)
		}
	}
}

func TestEpochTransitionPenaltiesForNonParticipation(t *testing.T) {
	et := NewEpochTransition()
	state := makeETState(8, 32, 64)

	// No one attested -- all participation maps empty.
	state.CurrJustCP = Checkpoint{Epoch: 1}
	state.FinalizedCP = Checkpoint{Epoch: 0}

	result, err := et.ProcessEpoch(state)
	if err != nil {
		t.Fatalf("ProcessEpoch failed: %v", err)
	}

	// All validators should receive net negative rewards (penalties).
	for i, r := range result.Rewards {
		if r >= 0 {
			t.Errorf("validator %d reward = %d, expected negative", i, r)
		}
	}
}

func TestEpochTransitionInactivityLeak(t *testing.T) {
	et := NewEpochTransition()
	state := makeETState(4, 32, 64) // epoch 2

	// Finalized at epoch 0, current epoch 2 => previous epoch 1.
	// finality_delay = 1 - 0 = 1. Not in leak.
	// Let's move to epoch 8 with finalized still at 0 to trigger leak.
	state.CurrentSlot = 256 // epoch 8
	state.FinalizedCP = Checkpoint{Epoch: 0}

	// Set high inactivity scores for non-attesters.
	for i := range state.InactivityScores {
		state.InactivityScores[i] = 100
	}

	// Validator 0 attested, others did not.
	state.Participation.PrevSource[0] = true
	state.Participation.PrevTarget[0] = true
	state.Participation.PrevHead[0] = true

	result, err := et.ProcessEpoch(state)
	if err != nil {
		t.Fatalf("ProcessEpoch failed: %v", err)
	}

	if !result.InLeakMode {
		t.Fatal("expected InLeakMode=true")
	}
	if result.FinalityDelay <= ETMinEpochsToInactivityPenalty {
		t.Errorf("FinalityDelay=%d, want >%d", result.FinalityDelay, ETMinEpochsToInactivityPenalty)
	}

	// Validator 0 (attested) should have a positive reward.
	if result.Rewards[0] <= 0 {
		t.Errorf("attesting validator reward=%d, expected positive", result.Rewards[0])
	}
	// Non-attesting validators should have negative rewards (penalties).
	for i := 1; i < 4; i++ {
		if result.Rewards[i] >= 0 {
			t.Errorf("non-attesting validator %d reward=%d, expected negative", i, result.Rewards[i])
		}
	}
}

func TestEpochTransitionInactivityScoreUpdates(t *testing.T) {
	et := NewEpochTransition()
	state := makeETState(4, 32, 256) // epoch 8

	// Long finality delay => leak.
	state.FinalizedCP = Checkpoint{Epoch: 0}

	// Validator 0 attested to target, others did not.
	state.Participation.PrevTarget[0] = true
	state.InactivityScores = []uint64{10, 10, 10, 10}

	_, err := et.ProcessEpoch(state)
	if err != nil {
		t.Fatalf("ProcessEpoch: %v", err)
	}

	// Validator 0 (target attested) should have decreased score.
	if state.InactivityScores[0] >= 10 {
		t.Errorf("attesting validator inactivity score=%d, want <10", state.InactivityScores[0])
	}
	// Others should have increased scores.
	for i := 1; i < 4; i++ {
		if state.InactivityScores[i] <= 10 {
			t.Errorf("non-attesting validator %d inactivity score=%d, want >10",
				i, state.InactivityScores[i])
		}
	}
}

func TestEpochTransitionRegistryUpdates(t *testing.T) {
	et := NewEpochTransition()
	state := makeETState(4, 32, 64) // epoch 2

	// Add a pending validator eligible for activation.
	pending := &ValidatorV2{
		EffectiveBalance:           32 * GweiPerETH,
		ActivationEligibilityEpoch: 0, // eligible before finalized epoch
		ActivationEpoch:            FarFutureEpoch,
		ExitEpoch:                  FarFutureEpoch,
		WithdrawableEpoch:          FarFutureEpoch,
	}
	state.Validators = append(state.Validators, pending)
	state.Balances = append(state.Balances, 32*GweiPerETH)
	state.InactivityScores = append(state.InactivityScores, 0)

	// Set full participation to avoid errors.
	for i := 0; i < len(state.Validators); i++ {
		idx := ValidatorIndex(i)
		state.Participation.PrevSource[idx] = true
		state.Participation.PrevTarget[idx] = true
		state.Participation.PrevHead[idx] = true
	}

	// Finalized at epoch 1, so the pending validator (eligible at 0) qualifies.
	state.FinalizedCP = Checkpoint{Epoch: 1}
	state.CurrJustCP = Checkpoint{Epoch: 1}

	result, err := et.ProcessEpoch(state)
	if err != nil {
		t.Fatalf("ProcessEpoch: %v", err)
	}

	if len(result.Activated) == 0 {
		t.Fatal("expected at least one activated validator")
	}
}

func TestEpochTransitionEjection(t *testing.T) {
	et := NewEpochTransition()
	state := makeETState(4, 32, 64)

	// Set validator 3 to low effective balance (should be ejected).
	state.Validators[3].EffectiveBalance = EjectionBalance
	state.Balances[3] = EjectionBalance

	for i := 0; i < 4; i++ {
		idx := ValidatorIndex(i)
		state.Participation.PrevSource[idx] = true
		state.Participation.PrevTarget[idx] = true
		state.Participation.PrevHead[idx] = true
	}
	state.FinalizedCP = Checkpoint{Epoch: 1}
	state.CurrJustCP = Checkpoint{Epoch: 1}

	result, err := et.ProcessEpoch(state)
	if err != nil {
		t.Fatalf("ProcessEpoch: %v", err)
	}

	if len(result.Ejected) == 0 {
		t.Fatal("expected ejected validators")
	}
	found := false
	for _, idx := range result.Ejected {
		if idx == 3 {
			found = true
		}
	}
	if !found {
		t.Fatal("expected validator 3 to be ejected")
	}
	if state.Validators[3].ExitEpoch == FarFutureEpoch {
		t.Fatal("ejected validator should have exit epoch set")
	}
}

func TestEpochTransitionEffectiveBalanceUpdate(t *testing.T) {
	et := NewEpochTransition()
	state := makeETState(2, 32, 64)

	// Set balance well above effective balance to trigger update.
	state.Balances[0] = 40 * GweiPerETH
	state.Validators[0].EffectiveBalance = 32 * GweiPerETH

	for i := 0; i < 2; i++ {
		idx := ValidatorIndex(i)
		state.Participation.PrevSource[idx] = true
		state.Participation.PrevTarget[idx] = true
		state.Participation.PrevHead[idx] = true
	}
	state.FinalizedCP = Checkpoint{Epoch: 1}

	_, err := et.ProcessEpoch(state)
	if err != nil {
		t.Fatalf("ProcessEpoch: %v", err)
	}

	// Effective balance should have been updated upward.
	if state.Validators[0].EffectiveBalance <= 32*GweiPerETH {
		t.Errorf("effective balance not updated: %d", state.Validators[0].EffectiveBalance)
	}
}

func TestEpochTransitionSlashingsPenalty(t *testing.T) {
	et := NewEpochTransition()
	state := makeETState(4, 32, 64)

	// Slash validator 0 and set it up for slashings processing.
	state.Validators[0].Slashed = true
	epoch2 := Epoch(2) // current epoch
	halfVector := Epoch(EpochsPerSlashingsVector / 2)
	state.Validators[0].WithdrawableEpoch = epoch2 + halfVector
	state.Validators[0].ExitEpoch = epoch2

	// Record some slashed amount.
	state.Slashings[0] = 32 * GweiPerETH

	for i := 0; i < 4; i++ {
		idx := ValidatorIndex(i)
		state.Participation.PrevSource[idx] = true
		state.Participation.PrevTarget[idx] = true
		state.Participation.PrevHead[idx] = true
	}
	state.FinalizedCP = Checkpoint{Epoch: 1}

	prevBalance := state.Balances[0]
	result, err := et.ProcessEpoch(state)
	if err != nil {
		t.Fatalf("ProcessEpoch: %v", err)
	}

	if len(result.Slashed) == 0 {
		t.Fatal("expected slashed validators in result")
	}
	if state.Balances[0] >= prevBalance {
		t.Errorf("slashed validator balance not decreased: %d >= %d",
			state.Balances[0], prevBalance)
	}
}

func TestEpochTransitionJustificationFinalization(t *testing.T) {
	et := NewEpochTransition()
	state := makeETState(4, 32, 128) // epoch 4

	// Set up for finalization: consecutive justified epochs.
	state.JustBits = [4]bool{false, true, true, true}
	state.PrevJustCP = Checkpoint{Epoch: 1}
	state.CurrJustCP = Checkpoint{Epoch: 3}
	state.FinalizedCP = Checkpoint{Epoch: 0}

	// Full participation on target for current epoch.
	for i := 0; i < 4; i++ {
		idx := ValidatorIndex(i)
		state.Participation.CurrTarget[idx] = true
		state.Participation.PrevTarget[idx] = true
		state.Participation.PrevSource[idx] = true
		state.Participation.PrevHead[idx] = true
	}

	result, err := et.ProcessEpoch(state)
	if err != nil {
		t.Fatalf("ProcessEpoch: %v", err)
	}

	// Should have finalized some checkpoint.
	if result.NewFinalizedCP.Epoch == 0 {
		t.Error("expected finalization to advance from epoch 0")
	}
}

func TestEpochTransitionParticipationRotation(t *testing.T) {
	state := makeETState(2, 32, 64)
	state.Participation.CurrSource[0] = true
	state.Participation.CurrTarget[0] = true

	et := NewEpochTransition()

	for i := 0; i < 2; i++ {
		idx := ValidatorIndex(i)
		state.Participation.PrevSource[idx] = true
		state.Participation.PrevTarget[idx] = true
		state.Participation.PrevHead[idx] = true
	}
	state.FinalizedCP = Checkpoint{Epoch: 1}

	_, err := et.ProcessEpoch(state)
	if err != nil {
		t.Fatalf("ProcessEpoch: %v", err)
	}

	// After rotation, current should be empty, previous should have old current.
	if len(state.Participation.CurrSource) != 0 {
		t.Error("current participation should be empty after rotation")
	}
	if !state.Participation.PrevSource[0] {
		t.Error("previous participation should contain rotated current data")
	}
}

func TestEpochTransitionCurrentEpochPreviousEpoch(t *testing.T) {
	s := &EpochTransitionState{CurrentSlot: 96, SlotsPerEpoch: 32}
	if s.CurrentEpoch() != 3 {
		t.Errorf("CurrentEpoch()=%d, want 3", s.CurrentEpoch())
	}
	if s.PreviousEpoch() != 2 {
		t.Errorf("PreviousEpoch()=%d, want 2", s.PreviousEpoch())
	}

	s2 := &EpochTransitionState{CurrentSlot: 16, SlotsPerEpoch: 32}
	if s2.PreviousEpoch() != 0 {
		t.Errorf("PreviousEpoch()=%d, want 0 (clamped)", s2.PreviousEpoch())
	}
}
