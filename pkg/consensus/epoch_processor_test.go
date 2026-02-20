package consensus

import (
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// makeTestState builds a minimal EpochProcessorState with n active validators
// each having the given effective balance and actual balance.
func makeTestState(n int, effBal, actBal uint64, slot, slotsPerEpoch uint64) *EpochProcessorState {
	state := &EpochProcessorState{
		Slot:          slot,
		SlotsPerEpoch: slotsPerEpoch,
		Validators:    make([]*ValidatorV2, n),
		Balances:      make([]uint64, n),
		HistoricalRoots:            make([]types.Hash, 0),
		PreviousEpochParticipation: NewEpochParticipation(),
		CurrentEpochParticipation:  NewEpochParticipation(),
	}

	for i := 0; i < n; i++ {
		state.Validators[i] = &ValidatorV2{
			EffectiveBalance:           effBal,
			ActivationEpoch:            0,
			ExitEpoch:                  FarFutureEpoch,
			WithdrawableEpoch:          FarFutureEpoch,
			ActivationEligibilityEpoch: 0,
		}
		state.Validators[i].Pubkey[0] = byte(i)
		state.Balances[i] = actBal
	}

	return state
}

func TestEpochProcessor_NilState(t *testing.T) {
	ep := NewEpochProcessor()
	err := ep.ProcessEpochTransition(nil)
	if err != ErrEPNilState {
		t.Fatalf("expected ErrEPNilState, got %v", err)
	}
}

func TestEpochProcessor_NoValidators(t *testing.T) {
	ep := NewEpochProcessor()
	state := &EpochProcessorState{
		Validators: make([]*ValidatorV2, 0),
		Balances:   make([]uint64, 0),
	}
	err := ep.ProcessEpochTransition(state)
	if err != ErrEPNoValidators {
		t.Fatalf("expected ErrEPNoValidators, got %v", err)
	}
}

func TestEpochProcessor_BalanceMismatch(t *testing.T) {
	ep := NewEpochProcessor()
	state := &EpochProcessorState{
		Validators: make([]*ValidatorV2, 2),
		Balances:   make([]uint64, 1),
	}
	state.Validators[0] = &ValidatorV2{}
	state.Validators[1] = &ValidatorV2{}
	err := ep.ProcessEpochTransition(state)
	if err != ErrEPBalanceMismatch {
		t.Fatalf("expected ErrEPBalanceMismatch, got %v", err)
	}
}

func TestEpochProcessor_BasicTransition(t *testing.T) {
	// 4 validators, 32 ETH each, at epoch 2 (slot 64 with 32 slots/epoch).
	state := makeTestState(4, 32*GweiPerETH, 32*GweiPerETH, 64, 32)

	// Mark all validators as attesting to source, target, head for the
	// previous epoch.
	for i := 0; i < 4; i++ {
		state.PreviousEpochParticipation.SourceAttested[uint64(i)] = true
		state.PreviousEpochParticipation.TargetAttested[uint64(i)] = true
		state.PreviousEpochParticipation.HeadAttested[uint64(i)] = true
	}

	ep := NewEpochProcessor()
	err := ep.ProcessEpochTransition(state)
	if err != nil {
		t.Fatalf("ProcessEpochTransition: %v", err)
	}

	// Verify that balances increased (rewards applied).
	for i, bal := range state.Balances {
		if bal <= 32*GweiPerETH {
			t.Errorf("validator %d balance should increase, got %d", i, bal)
		}
	}
}

func TestEpochProcessor_PenaltiesForNonAttesters(t *testing.T) {
	state := makeTestState(4, 32*GweiPerETH, 32*GweiPerETH, 64, 32)

	// Only validator 0 attests.
	state.PreviousEpochParticipation.SourceAttested[0] = true
	state.PreviousEpochParticipation.TargetAttested[0] = true
	state.PreviousEpochParticipation.HeadAttested[0] = true

	initialBal := state.Balances[1]

	ep := NewEpochProcessor()
	if err := ep.ProcessEpochTransition(state); err != nil {
		t.Fatal(err)
	}

	// Non-attesters should lose balance.
	if state.Balances[1] >= initialBal {
		t.Errorf("non-attesting validator should lose balance: before=%d, after=%d",
			initialBal, state.Balances[1])
	}
}

func TestEpochProcessor_JustificationAndFinalization(t *testing.T) {
	// Create state at epoch 4, with 32 slots per epoch.
	state := makeTestState(10, 32*GweiPerETH, 32*GweiPerETH, 128, 32)

	// Set up block roots so they can be looked up.
	for i := range state.BlockRoots {
		state.BlockRoots[i] = hashFromByte(byte(i % 256))
	}

	// All validators attested to previous and current targets.
	for i := 0; i < 10; i++ {
		state.PreviousEpochParticipation.SourceAttested[uint64(i)] = true
		state.PreviousEpochParticipation.TargetAttested[uint64(i)] = true
		state.CurrentEpochParticipation.SourceAttested[uint64(i)] = true
		state.CurrentEpochParticipation.TargetAttested[uint64(i)] = true
	}

	// Set initial justified/finalized.
	state.CurrentJustifiedCheckpoint = Checkpoint{Epoch: 2, Root: hashFromByte(64)}
	state.PreviousJustifiedCheckpoint = Checkpoint{Epoch: 1, Root: hashFromByte(32)}
	state.JustificationBits = [4]bool{true, true, false, false}

	ep := NewEpochProcessor()
	if err := ep.ProcessEpochTransition(state); err != nil {
		t.Fatal(err)
	}

	// Current epoch (4) should be justified since supermajority attested.
	if state.CurrentJustifiedCheckpoint.Epoch < 3 {
		t.Errorf("expected justified epoch >= 3, got %d",
			state.CurrentJustifiedCheckpoint.Epoch)
	}
}

func TestEpochProcessor_Finalization_FourthRule(t *testing.T) {
	// Test the 1st/2nd epochs justified -> finalize 1st rule.
	// Current epoch = 3, bits[0]=true bits[1]=true, oldCurrJustified.Epoch+1 == 3.
	state := makeTestState(10, 32*GweiPerETH, 32*GweiPerETH, 96, 32)

	for i := range state.BlockRoots {
		state.BlockRoots[i] = hashFromByte(byte(i % 256))
	}

	// All validators attest.
	for i := 0; i < 10; i++ {
		state.PreviousEpochParticipation.SourceAttested[uint64(i)] = true
		state.PreviousEpochParticipation.TargetAttested[uint64(i)] = true
		state.CurrentEpochParticipation.SourceAttested[uint64(i)] = true
		state.CurrentEpochParticipation.TargetAttested[uint64(i)] = true
	}

	// Setup: current justified at epoch 2, bits show both recent justified.
	state.CurrentJustifiedCheckpoint = Checkpoint{Epoch: 2, Root: hashFromByte(64)}
	state.PreviousJustifiedCheckpoint = Checkpoint{Epoch: 1, Root: hashFromByte(32)}
	state.JustificationBits = [4]bool{true, true, false, false}

	ep := NewEpochProcessor()
	if err := ep.ProcessEpochTransition(state); err != nil {
		t.Fatal(err)
	}

	// After processing epoch 3 with bits[0]=true, bits[1]=true and
	// oldCurrJustified.Epoch(2)+1==3, we expect finalization.
	if state.FinalizedCheckpoint.Epoch < 2 {
		t.Errorf("expected finalized epoch >= 2, got %d",
			state.FinalizedCheckpoint.Epoch)
	}
}

func TestEpochProcessor_RegistryUpdates_Activation(t *testing.T) {
	state := makeTestState(4, 32*GweiPerETH, 32*GweiPerETH, 64, 32)

	// Add a pending validator (not yet eligible for activation).
	pending := &ValidatorV2{
		EffectiveBalance:           32 * GweiPerETH,
		ActivationEligibilityEpoch: FarFutureEpoch,
		ActivationEpoch:            FarFutureEpoch,
		ExitEpoch:                  FarFutureEpoch,
		WithdrawableEpoch:          FarFutureEpoch,
	}
	state.Validators = append(state.Validators, pending)
	state.Balances = append(state.Balances, 32*GweiPerETH)

	// All existing validators attest.
	for i := 0; i < 4; i++ {
		state.PreviousEpochParticipation.SourceAttested[uint64(i)] = true
		state.PreviousEpochParticipation.TargetAttested[uint64(i)] = true
		state.PreviousEpochParticipation.HeadAttested[uint64(i)] = true
	}

	ep := NewEpochProcessor()
	if err := ep.ProcessEpochTransition(state); err != nil {
		t.Fatal(err)
	}

	// Pending validator should become activation-eligible.
	if state.Validators[4].ActivationEligibilityEpoch == FarFutureEpoch {
		t.Error("pending validator should have activation eligibility set")
	}
}

func TestEpochProcessor_RegistryUpdates_Ejection(t *testing.T) {
	state := makeTestState(4, 32*GweiPerETH, 32*GweiPerETH, 64, 32)

	// Set one validator's effective balance below ejection threshold.
	state.Validators[2].EffectiveBalance = 15_000_000_000

	for i := 0; i < 4; i++ {
		state.PreviousEpochParticipation.SourceAttested[uint64(i)] = true
		state.PreviousEpochParticipation.TargetAttested[uint64(i)] = true
	}

	ep := NewEpochProcessor()
	if err := ep.ProcessEpochTransition(state); err != nil {
		t.Fatal(err)
	}

	// Validator 2 should be ejected (exit epoch set).
	if state.Validators[2].ExitEpoch == FarFutureEpoch {
		t.Error("low-balance validator should be ejected")
	}
}

func TestEpochProcessor_Slashings(t *testing.T) {
	state := makeTestState(4, 32*GweiPerETH, 32*GweiPerETH, 64, 32)

	// Slash validator 1.
	state.Validators[1].Slashed = true
	currentEpoch := state.CurrentEpoch()
	state.Validators[1].WithdrawableEpoch = currentEpoch + Epoch(epEpochsPerSlashingsVector/2)

	// Record some slashings.
	state.Slashings[0] = 32 * GweiPerETH

	for i := 0; i < 4; i++ {
		state.PreviousEpochParticipation.SourceAttested[uint64(i)] = true
		state.PreviousEpochParticipation.TargetAttested[uint64(i)] = true
	}

	initialBal := state.Balances[1]

	ep := NewEpochProcessor()
	if err := ep.ProcessEpochTransition(state); err != nil {
		t.Fatal(err)
	}

	// Slashed validator should have reduced balance.
	if state.Balances[1] >= initialBal {
		t.Errorf("slashed validator balance should decrease: before=%d, after=%d",
			initialBal, state.Balances[1])
	}
}

func TestEpochProcessor_EffectiveBalanceUpdate(t *testing.T) {
	state := makeTestState(2, 32*GweiPerETH, 32*GweiPerETH, 64, 32)

	// Give validator 0 a much higher actual balance.
	state.Balances[0] = 40 * GweiPerETH

	for i := 0; i < 2; i++ {
		state.PreviousEpochParticipation.SourceAttested[uint64(i)] = true
		state.PreviousEpochParticipation.TargetAttested[uint64(i)] = true
	}

	ep := NewEpochProcessor()
	if err := ep.ProcessEpochTransition(state); err != nil {
		t.Fatal(err)
	}

	// Effective balance should increase due to hysteresis.
	if state.Validators[0].EffectiveBalance <= 32*GweiPerETH {
		t.Errorf("effective balance should increase for validator with 40 ETH actual, got %d",
			state.Validators[0].EffectiveBalance)
	}
}

func TestEpochProcessor_EffectiveBalanceUpdate_Downward(t *testing.T) {
	state := makeTestState(2, 32*GweiPerETH, 32*GweiPerETH, 64, 32)

	// Give validator 0 a much lower actual balance.
	state.Balances[0] = 28 * GweiPerETH

	for i := 0; i < 2; i++ {
		state.PreviousEpochParticipation.SourceAttested[uint64(i)] = true
		state.PreviousEpochParticipation.TargetAttested[uint64(i)] = true
	}

	ep := NewEpochProcessor()
	if err := ep.ProcessEpochTransition(state); err != nil {
		t.Fatal(err)
	}

	// Effective balance should decrease.
	if state.Validators[0].EffectiveBalance >= 32*GweiPerETH {
		t.Errorf("effective balance should decrease for validator with 28 ETH actual, got %d",
			state.Validators[0].EffectiveBalance)
	}
}

func TestEpochProcessor_SlashingsReset(t *testing.T) {
	state := makeTestState(2, 32*GweiPerETH, 32*GweiPerETH, 64, 32)
	// Fill a slashings entry.
	nextEpoch := state.CurrentEpoch() + 1
	idx := uint64(nextEpoch) % epEpochsPerSlashingsVector
	state.Slashings[idx] = 1_000_000

	for i := 0; i < 2; i++ {
		state.PreviousEpochParticipation.SourceAttested[uint64(i)] = true
		state.PreviousEpochParticipation.TargetAttested[uint64(i)] = true
	}

	ep := NewEpochProcessor()
	if err := ep.ProcessEpochTransition(state); err != nil {
		t.Fatal(err)
	}

	if state.Slashings[idx] != 0 {
		t.Errorf("slashings[%d] should be reset to 0, got %d", idx, state.Slashings[idx])
	}
}

func TestEpochProcessor_RandaoMixesRotation(t *testing.T) {
	state := makeTestState(2, 32*GweiPerETH, 32*GweiPerETH, 64, 32)

	// Set current epoch's RANDAO mix.
	currentEpoch := state.CurrentEpoch()
	state.RandaoMixes[uint64(currentEpoch)%epEpochsPerHistoricalVector] = hashFromByte(42)

	for i := 0; i < 2; i++ {
		state.PreviousEpochParticipation.SourceAttested[uint64(i)] = true
		state.PreviousEpochParticipation.TargetAttested[uint64(i)] = true
	}

	ep := NewEpochProcessor()
	if err := ep.ProcessEpochTransition(state); err != nil {
		t.Fatal(err)
	}

	nextEpoch := currentEpoch + 1
	nextMix := state.RandaoMixes[uint64(nextEpoch)%epEpochsPerHistoricalVector]
	expected := hashFromByte(42)
	if nextMix != expected {
		t.Errorf("next epoch RANDAO mix should be copied from current, got %v", nextMix)
	}
}

func TestEpochProcessor_ParticipationRotation(t *testing.T) {
	state := makeTestState(2, 32*GweiPerETH, 32*GweiPerETH, 64, 32)

	state.CurrentEpochParticipation.SourceAttested[0] = true
	state.PreviousEpochParticipation.SourceAttested[0] = true
	state.PreviousEpochParticipation.TargetAttested[0] = true
	state.PreviousEpochParticipation.SourceAttested[1] = true
	state.PreviousEpochParticipation.TargetAttested[1] = true

	ep := NewEpochProcessor()
	if err := ep.ProcessEpochTransition(state); err != nil {
		t.Fatal(err)
	}

	// Previous should now have the old current's data.
	if !state.PreviousEpochParticipation.SourceAttested[0] {
		t.Error("rotated previous should have old current data")
	}

	// Current should be fresh.
	if len(state.CurrentEpochParticipation.SourceAttested) != 0 {
		t.Error("current epoch participation should be empty after rotation")
	}
}

func TestEpochProcessor_EarlyEpoch_NoJustification(t *testing.T) {
	// At epoch 0, justification should be skipped.
	state := makeTestState(4, 32*GweiPerETH, 32*GweiPerETH, 0, 32)

	ep := NewEpochProcessor()
	if err := ep.ProcessEpochTransition(state); err != nil {
		t.Fatal(err)
	}

	// No justification should occur at epoch 0.
	if state.CurrentJustifiedCheckpoint.Epoch != 0 {
		t.Errorf("no justification expected at epoch 0")
	}
}

func TestEpochProcessor_ThreadSafety(t *testing.T) {
	ep := NewEpochProcessor()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := makeTestState(4, 32*GweiPerETH, 32*GweiPerETH, 64, 32)
			for j := 0; j < 4; j++ {
				s.PreviousEpochParticipation.SourceAttested[uint64(j)] = true
				s.PreviousEpochParticipation.TargetAttested[uint64(j)] = true
			}
			_ = ep.ProcessEpochTransition(s)
		}()
	}
	wg.Wait()
}

func TestEpochProcessor_InactivityPenalty(t *testing.T) {
	// Create a state where finality delay > MIN_EPOCHS_TO_INACTIVITY_PENALTY.
	// Set finalized at epoch 0, current at epoch 10 -> delay = 9.
	state := makeTestState(4, 32*GweiPerETH, 32*GweiPerETH, 320, 32)
	state.FinalizedCheckpoint = Checkpoint{Epoch: 0, Root: types.Hash{}}

	// Validator 0 attests, validators 1-3 don't.
	state.PreviousEpochParticipation.SourceAttested[0] = true
	state.PreviousEpochParticipation.TargetAttested[0] = true
	state.PreviousEpochParticipation.HeadAttested[0] = true

	initialBal := state.Balances[1]

	ep := NewEpochProcessor()
	if err := ep.ProcessEpochTransition(state); err != nil {
		t.Fatal(err)
	}

	// Non-attesters should have inactivity penalty on top of base penalty.
	if state.Balances[1] >= initialBal {
		t.Errorf("expected inactivity penalty for non-attesting validator: before=%d, after=%d",
			initialBal, state.Balances[1])
	}

	// The penalty should be more severe than a normal non-attestation penalty.
	// With finality delay of 9 and effective balance of 32 ETH, the inactivity
	// penalty = 32e9 * 9 / 67108864 ~= 4291 Gwei, on top of base penalties.
	normalPenalty := uint64(3) * (32 * GweiPerETH * 64 / ep.intSqrt(4*32*GweiPerETH) / 4)
	if initialBal-state.Balances[1] <= normalPenalty {
		t.Errorf("inactivity penalty should exceed normal penalty")
	}
}

func TestEpochProcessor_IntSqrt(t *testing.T) {
	ep := NewEpochProcessor()
	tests := []struct {
		input uint64
		want  uint64
	}{
		{0, 0},
		{1, 1},
		{4, 2},
		{9, 3},
		{10, 3},
		{100, 10},
		{1_000_000, 1000},
		{128_000_000_000, 357770}, // sqrt(128 * 10^9)
	}
	for _, tc := range tests {
		got := ep.intSqrt(tc.input)
		if got != tc.want {
			t.Errorf("intSqrt(%d) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestEpochParticipation_New(t *testing.T) {
	p := NewEpochParticipation()
	if p.SourceAttested == nil || p.TargetAttested == nil || p.HeadAttested == nil {
		t.Fatal("NewEpochParticipation maps should not be nil")
	}
}

func TestEpochProcessorState_Epochs(t *testing.T) {
	s := &EpochProcessorState{Slot: 100, SlotsPerEpoch: 32}
	if s.CurrentEpoch() != 3 {
		t.Errorf("expected epoch 3, got %d", s.CurrentEpoch())
	}
	if s.PreviousEpoch() != 2 {
		t.Errorf("expected previous epoch 2, got %d", s.PreviousEpoch())
	}
	if s2 := (&EpochProcessorState{Slot: 0, SlotsPerEpoch: 32}); s2.PreviousEpoch() != 0 {
		t.Errorf("expected previous epoch 0 at genesis, got %d", s2.PreviousEpoch())
	}
}
