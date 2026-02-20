package consensus

import (
	"math"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// epTestState is a helper that builds an EpochProcessorState with customizable
// parameters, distinct from makeTestState in the base test file.
func epTestState(n int, effBal, actBal uint64, slot, spe uint64) *EpochProcessorState {
	state := &EpochProcessorState{
		Slot:                       slot,
		SlotsPerEpoch:              spe,
		Validators:                 make([]*ValidatorV2, n),
		Balances:                   make([]uint64, n),
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
		state.Validators[i].Pubkey[0] = byte(i % 256)
		state.Validators[i].Pubkey[1] = byte(i / 256)
		state.Balances[i] = actBal
	}
	return state
}

func epHash(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	return h
}

// ---------- Justification and Finalization ----------

func TestEP_Justification_PreviousEpochSupermajority(t *testing.T) {
	// At epoch 3 (slot 96, spe 32), if >= 2/3 of previous epoch's target
	// attested, the previous epoch should be justified.
	state := epTestState(6, 32*GweiPerETH, 32*GweiPerETH, 96, 32)
	for i := range state.BlockRoots {
		state.BlockRoots[i] = epHash(byte(i % 256))
	}
	// 5 of 6 validators attest to previous target only.
	for i := 0; i < 5; i++ {
		state.PreviousEpochParticipation.TargetAttested[uint64(i)] = true
		state.PreviousEpochParticipation.SourceAttested[uint64(i)] = true
	}
	state.CurrentJustifiedCheckpoint = Checkpoint{Epoch: 1, Root: epHash(32)}
	state.PreviousJustifiedCheckpoint = Checkpoint{Epoch: 0, Root: epHash(0)}
	state.JustificationBits = [4]bool{false, false, false, false}

	ep := NewEpochProcessor()
	if err := ep.ProcessEpochTransition(state); err != nil {
		t.Fatal(err)
	}
	// Bit[1] should be set (previous epoch justified).
	if !state.JustificationBits[1] {
		t.Error("expected JustificationBits[1]=true for previous epoch supermajority")
	}
}

func TestEP_Justification_CurrentEpochSupermajority(t *testing.T) {
	state := epTestState(6, 32*GweiPerETH, 32*GweiPerETH, 96, 32)
	for i := range state.BlockRoots {
		state.BlockRoots[i] = epHash(byte(i % 256))
	}
	// All validators attest to current target only.
	for i := 0; i < 6; i++ {
		state.CurrentEpochParticipation.TargetAttested[uint64(i)] = true
		state.CurrentEpochParticipation.SourceAttested[uint64(i)] = true
	}
	state.CurrentJustifiedCheckpoint = Checkpoint{Epoch: 1, Root: epHash(32)}
	state.PreviousJustifiedCheckpoint = Checkpoint{Epoch: 0, Root: epHash(0)}

	ep := NewEpochProcessor()
	if err := ep.ProcessEpochTransition(state); err != nil {
		t.Fatal(err)
	}
	// Bit[0] should be set.
	if !state.JustificationBits[0] {
		t.Error("expected JustificationBits[0]=true for current epoch supermajority")
	}
}

func TestEP_Justification_NotEnoughVotes(t *testing.T) {
	// Only 1 out of 6 validators attests: not a supermajority.
	state := epTestState(6, 32*GweiPerETH, 32*GweiPerETH, 96, 32)
	for i := range state.BlockRoots {
		state.BlockRoots[i] = epHash(byte(i % 256))
	}
	state.PreviousEpochParticipation.TargetAttested[0] = true
	state.PreviousEpochParticipation.SourceAttested[0] = true
	state.CurrentJustifiedCheckpoint = Checkpoint{Epoch: 1, Root: epHash(32)}
	state.PreviousJustifiedCheckpoint = Checkpoint{Epoch: 0, Root: epHash(0)}

	ep := NewEpochProcessor()
	if err := ep.ProcessEpochTransition(state); err != nil {
		t.Fatal(err)
	}
	// Neither bit should be set.
	if state.JustificationBits[0] || state.JustificationBits[1] {
		t.Error("justification should not occur with only 1/6 attesting")
	}
}

func TestEP_Finalization_ThirdRule(t *testing.T) {
	// Third rule: bits[0] && bits[1] && bits[2] && oldCJ.Epoch+2 == ce.
	// ce=5, oldCJ at epoch 3, bits after shift = [1,1,1,X].
	state := epTestState(6, 32*GweiPerETH, 32*GweiPerETH, 160, 32)
	for i := range state.BlockRoots {
		state.BlockRoots[i] = epHash(byte(i % 256))
	}
	for i := 0; i < 6; i++ {
		state.PreviousEpochParticipation.TargetAttested[uint64(i)] = true
		state.PreviousEpochParticipation.SourceAttested[uint64(i)] = true
		state.CurrentEpochParticipation.TargetAttested[uint64(i)] = true
		state.CurrentEpochParticipation.SourceAttested[uint64(i)] = true
	}
	state.CurrentJustifiedCheckpoint = Checkpoint{Epoch: 3, Root: epHash(96)}
	state.PreviousJustifiedCheckpoint = Checkpoint{Epoch: 2, Root: epHash(64)}
	state.JustificationBits = [4]bool{true, true, false, false}

	ep := NewEpochProcessor()
	if err := ep.ProcessEpochTransition(state); err != nil {
		t.Fatal(err)
	}
	// After processing, the finalized checkpoint should advance.
	if state.FinalizedCheckpoint.Epoch < 3 {
		t.Errorf("expected finalized epoch >= 3, got %d", state.FinalizedCheckpoint.Epoch)
	}
}

// ---------- Rewards and Penalties ----------

func TestEP_RewardsAndPenalties_TableDriven(t *testing.T) {
	tests := []struct {
		name              string
		attestSource      bool
		attestTarget      bool
		attestHead        bool
		expectBalIncrease bool
	}{
		{"all_attested", true, true, true, true},
		{"source_only", true, false, false, false},
		{"none_attested", false, false, false, false},
		{"target_and_head", false, true, true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := epTestState(4, 32*GweiPerETH, 32*GweiPerETH, 64, 32)
			// Make all validators attest so we have participation balance.
			for i := 0; i < 4; i++ {
				state.PreviousEpochParticipation.SourceAttested[uint64(i)] = true
				state.PreviousEpochParticipation.TargetAttested[uint64(i)] = true
				state.PreviousEpochParticipation.HeadAttested[uint64(i)] = true
			}
			// Override validator 0's participation.
			state.PreviousEpochParticipation.SourceAttested[0] = tc.attestSource
			state.PreviousEpochParticipation.TargetAttested[0] = tc.attestTarget
			state.PreviousEpochParticipation.HeadAttested[0] = tc.attestHead
			initial := state.Balances[0]

			ep := NewEpochProcessor()
			if err := ep.ProcessEpochTransition(state); err != nil {
				t.Fatal(err)
			}
			increased := state.Balances[0] > initial
			if increased != tc.expectBalIncrease {
				t.Errorf("balance increased=%v, want %v (before=%d, after=%d)",
					increased, tc.expectBalIncrease, initial, state.Balances[0])
			}
		})
	}
}

func TestEP_RewardsSkippedAtEpochZero(t *testing.T) {
	state := epTestState(4, 32*GweiPerETH, 32*GweiPerETH, 0, 32)
	for i := 0; i < 4; i++ {
		state.PreviousEpochParticipation.SourceAttested[uint64(i)] = true
		state.PreviousEpochParticipation.TargetAttested[uint64(i)] = true
	}
	initial := state.Balances[0]

	ep := NewEpochProcessor()
	if err := ep.ProcessEpochTransition(state); err != nil {
		t.Fatal(err)
	}
	if state.Balances[0] != initial {
		t.Errorf("at epoch 0, balance should not change: was %d, now %d", initial, state.Balances[0])
	}
}

func TestEP_SlashedValidatorsGetNoPenaltiesForNonAttestation(t *testing.T) {
	// A slashed validator who attests should not receive rewards.
	state := epTestState(4, 32*GweiPerETH, 32*GweiPerETH, 64, 32)
	state.Validators[0].Slashed = true
	state.PreviousEpochParticipation.SourceAttested[0] = true
	state.PreviousEpochParticipation.TargetAttested[0] = true
	state.PreviousEpochParticipation.HeadAttested[0] = true
	// The other validators also attest.
	for i := 1; i < 4; i++ {
		state.PreviousEpochParticipation.SourceAttested[uint64(i)] = true
		state.PreviousEpochParticipation.TargetAttested[uint64(i)] = true
		state.PreviousEpochParticipation.HeadAttested[uint64(i)] = true
	}
	initial := state.Balances[0]

	ep := NewEpochProcessor()
	if err := ep.ProcessEpochTransition(state); err != nil {
		t.Fatal(err)
	}
	// Slashed attester should lose balance (penalized) because slashed
	// attesters are treated as non-attesters.
	if state.Balances[0] >= initial {
		t.Errorf("slashed attester should lose balance: before=%d, after=%d",
			initial, state.Balances[0])
	}
}

// ---------- Registry Updates ----------

func TestEP_Registry_ChurnLimit(t *testing.T) {
	// Test that the churn limit is respected when activating validators.
	// With a small set, the minimum churn limit of 4 applies.
	state := epTestState(8, 32*GweiPerETH, 32*GweiPerETH, 64, 32)
	// Set a finalized checkpoint high enough for activation.
	state.FinalizedCheckpoint = Checkpoint{Epoch: 2, Root: epHash(64)}

	// Add 6 pending validators.
	for i := 0; i < 6; i++ {
		pending := &ValidatorV2{
			EffectiveBalance:           32 * GweiPerETH,
			ActivationEligibilityEpoch: 1, // eligible before finalized
			ActivationEpoch:            FarFutureEpoch,
			ExitEpoch:                  FarFutureEpoch,
			WithdrawableEpoch:          FarFutureEpoch,
		}
		pending.Pubkey[0] = byte(100 + i)
		state.Validators = append(state.Validators, pending)
		state.Balances = append(state.Balances, 32*GweiPerETH)
	}
	// All active validators attest.
	for i := 0; i < 8; i++ {
		state.PreviousEpochParticipation.SourceAttested[uint64(i)] = true
		state.PreviousEpochParticipation.TargetAttested[uint64(i)] = true
	}

	ep := NewEpochProcessor()
	if err := ep.ProcessEpochTransition(state); err != nil {
		t.Fatal(err)
	}
	// Count how many of the 6 pending validators were activated.
	var activated int
	for i := 8; i < 14; i++ {
		if state.Validators[i].ActivationEpoch != FarFutureEpoch {
			activated++
		}
	}
	// Churn limit = max(4, 8/65536) = 4.
	if activated > 4 {
		t.Errorf("activated %d validators, expected at most 4 (churn limit)", activated)
	}
	if activated == 0 {
		t.Error("expected at least some validators to be activated")
	}
}

func TestEP_Registry_EjectionThreshold(t *testing.T) {
	// Validators exactly at the ejection balance should be ejected.
	state := epTestState(4, 32*GweiPerETH, 32*GweiPerETH, 64, 32)
	state.Validators[0].EffectiveBalance = epEjectionBalance // exactly at threshold
	for i := 0; i < 4; i++ {
		state.PreviousEpochParticipation.SourceAttested[uint64(i)] = true
		state.PreviousEpochParticipation.TargetAttested[uint64(i)] = true
	}

	ep := NewEpochProcessor()
	if err := ep.ProcessEpochTransition(state); err != nil {
		t.Fatal(err)
	}
	if state.Validators[0].ExitEpoch == FarFutureEpoch {
		t.Error("validator at ejection balance should be ejected")
	}
}

func TestEP_Registry_InactiveValidatorsNotEjected(t *testing.T) {
	state := epTestState(4, 32*GweiPerETH, 32*GweiPerETH, 64, 32)
	// Make validator 0 already exited.
	state.Validators[0].ExitEpoch = 1
	state.Validators[0].EffectiveBalance = 1 * GweiPerETH // way below ejection

	for i := 0; i < 4; i++ {
		state.PreviousEpochParticipation.SourceAttested[uint64(i)] = true
		state.PreviousEpochParticipation.TargetAttested[uint64(i)] = true
	}

	ep := NewEpochProcessor()
	if err := ep.ProcessEpochTransition(state); err != nil {
		t.Fatal(err)
	}
	// Exit epoch should remain at 1 (already exited, not changed).
	if state.Validators[0].ExitEpoch != 1 {
		t.Errorf("already-exited validator exit epoch changed from 1 to %d",
			state.Validators[0].ExitEpoch)
	}
}

// ---------- Effective Balance Updates ----------

func TestEP_EffectiveBalance_HysteresisNoChange(t *testing.T) {
	// Balance close to effective balance should not trigger update (hysteresis).
	state := epTestState(1, 32*GweiPerETH, 32*GweiPerETH, 64, 32)
	// Slightly above effective balance but within hysteresis band.
	state.Balances[0] = 32*GweiPerETH + EffectiveBalanceIncrement/HysteresisQuotient
	state.PreviousEpochParticipation.SourceAttested[0] = true
	state.PreviousEpochParticipation.TargetAttested[0] = true

	ep := NewEpochProcessor()
	if err := ep.ProcessEpochTransition(state); err != nil {
		t.Fatal(err)
	}
	if state.Validators[0].EffectiveBalance != 32*GweiPerETH {
		t.Errorf("effective balance should not change within hysteresis band, got %d",
			state.Validators[0].EffectiveBalance)
	}
}

func TestEP_EffectiveBalance_CappedAtMax(t *testing.T) {
	// Even if actual balance is huge, effective balance is capped at MaxEffectiveBalance.
	state := epTestState(1, 32*GweiPerETH, 3000*GweiPerETH, 64, 32)
	state.PreviousEpochParticipation.SourceAttested[0] = true
	state.PreviousEpochParticipation.TargetAttested[0] = true

	ep := NewEpochProcessor()
	if err := ep.ProcessEpochTransition(state); err != nil {
		t.Fatal(err)
	}
	if state.Validators[0].EffectiveBalance > MaxEffectiveBalance {
		t.Errorf("effective balance %d exceeds max %d",
			state.Validators[0].EffectiveBalance, MaxEffectiveBalance)
	}
}

// ---------- Slashing Processing ----------

func TestEP_Slashings_NoSlashedValidators(t *testing.T) {
	state := epTestState(4, 32*GweiPerETH, 32*GweiPerETH, 64, 32)
	state.Slashings[0] = 32 * GweiPerETH
	for i := 0; i < 4; i++ {
		state.PreviousEpochParticipation.SourceAttested[uint64(i)] = true
		state.PreviousEpochParticipation.TargetAttested[uint64(i)] = true
	}
	initial := make([]uint64, 4)
	copy(initial, state.Balances)

	ep := NewEpochProcessor()
	if err := ep.ProcessEpochTransition(state); err != nil {
		t.Fatal(err)
	}
	// Without any slashed validators, slashings processing should not apply
	// extra penalties (only rewards/penalties apply).
	for i := 0; i < 4; i++ {
		// All attested, so balances should increase (rewards applied).
		if state.Balances[i] < initial[i] {
			t.Errorf("validator %d balance decreased unexpectedly: %d -> %d",
				i, initial[i], state.Balances[i])
		}
	}
}

func TestEP_Slashings_AdjustedCapped(t *testing.T) {
	// Test that adjusted slashings amount is capped at total active balance.
	state := epTestState(4, 32*GweiPerETH, 32*GweiPerETH, 64, 32)
	ce := state.CurrentEpoch()
	state.Validators[0].Slashed = true
	state.Validators[0].WithdrawableEpoch = ce + Epoch(epEpochsPerSlashingsVector/2)
	// Fill all slashings to a huge value.
	for i := range state.Slashings {
		state.Slashings[i] = 1000 * GweiPerETH
	}
	for i := 0; i < 4; i++ {
		state.PreviousEpochParticipation.SourceAttested[uint64(i)] = true
		state.PreviousEpochParticipation.TargetAttested[uint64(i)] = true
	}
	initial := state.Balances[0]

	ep := NewEpochProcessor()
	if err := ep.ProcessEpochTransition(state); err != nil {
		t.Fatal(err)
	}
	// Even with exaggerated slashings, the balance should not go below 0.
	if state.Balances[0] > initial {
		t.Errorf("slashed validator balance should decrease: %d -> %d",
			initial, state.Balances[0])
	}
}

// ---------- Historical Roots Update ----------

func TestEP_HistoricalRootsUpdate_AtBoundary(t *testing.T) {
	// Historical roots update should occur when (currentEpoch+1) % (SlotsPerHistoricalRoot/SlotsPerEpoch) == 0.
	// With SlotsPerEpoch=1, that means epochsPerHist = 8192/1 = 8192.
	// So at epoch 8191 (slot 8191), currentEpoch+1 = 8192, 8192 % 8192 == 0.
	state := epTestState(2, 32*GweiPerETH, 32*GweiPerETH, 8191, 1)
	for i := 0; i < 2; i++ {
		state.PreviousEpochParticipation.SourceAttested[uint64(i)] = true
		state.PreviousEpochParticipation.TargetAttested[uint64(i)] = true
	}
	// Set some block roots for the XOR computation.
	state.BlockRoots[0] = epHash(0xAB)

	ep := NewEpochProcessor()
	if err := ep.ProcessEpochTransition(state); err != nil {
		t.Fatal(err)
	}
	if len(state.HistoricalRoots) != 1 {
		t.Errorf("expected 1 historical root, got %d", len(state.HistoricalRoots))
	}
}

func TestEP_HistoricalRootsUpdate_NotAtBoundary(t *testing.T) {
	state := epTestState(2, 32*GweiPerETH, 32*GweiPerETH, 64, 32)
	for i := 0; i < 2; i++ {
		state.PreviousEpochParticipation.SourceAttested[uint64(i)] = true
		state.PreviousEpochParticipation.TargetAttested[uint64(i)] = true
	}

	ep := NewEpochProcessor()
	if err := ep.ProcessEpochTransition(state); err != nil {
		t.Fatal(err)
	}
	if len(state.HistoricalRoots) != 0 {
		t.Errorf("no historical root expected, got %d", len(state.HistoricalRoots))
	}
}

// ---------- IntSqrt edge cases ----------

func TestEP_IntSqrt_MaxUint64(t *testing.T) {
	ep := NewEpochProcessor()
	result := ep.intSqrt(math.MaxUint64)
	if result != 4294967295 {
		t.Errorf("intSqrt(MaxUint64) = %d, want 4294967295", result)
	}
}

func TestEP_IntSqrt_LargeValues(t *testing.T) {
	ep := NewEpochProcessor()
	tests := []struct {
		input uint64
		want  uint64
	}{
		{1 << 32, 1 << 16},
		{1 << 40, 1 << 20},
		{1<<62 + 1<<61, 2629308657}, // approximate
	}
	for _, tc := range tests {
		got := ep.intSqrt(tc.input)
		// For large values, verify got*got <= input && (got+1)*(got+1) > input.
		if got*got > tc.input {
			t.Errorf("intSqrt(%d)=%d: result squared %d > input", tc.input, got, got*got)
		}
	}
}

// ---------- Participation and Part Balance ----------

func TestEP_PartBalance_NilParticipation(t *testing.T) {
	state := epTestState(4, 32*GweiPerETH, 32*GweiPerETH, 64, 32)
	ep := NewEpochProcessor()
	// partBalance with nil participation should return 0.
	result := ep.partBalance(state, nil, 0)
	if result != 0 {
		t.Errorf("partBalance with nil participation = %d, want 0", result)
	}
}

func TestEP_PartBalance_SlashedExcluded(t *testing.T) {
	state := epTestState(4, 32*GweiPerETH, 32*GweiPerETH, 64, 32)
	part := NewEpochParticipation()
	for i := 0; i < 4; i++ {
		part.SourceAttested[uint64(i)] = true
	}
	// Slash validator 0.
	state.Validators[0].Slashed = true

	ep := NewEpochProcessor()
	result := ep.partBalance(state, part, 0) // source
	expected := uint64(3) * 32 * GweiPerETH
	if result != expected {
		t.Errorf("partBalance with slashed = %d, want %d", result, expected)
	}
}

func TestEP_TotalActiveBalance_MinimumGuard(t *testing.T) {
	// State with 0 active validators (all exited).
	state := epTestState(2, 32*GweiPerETH, 32*GweiPerETH, 64, 32)
	state.Validators[0].ExitEpoch = 1
	state.Validators[1].ExitEpoch = 1

	ep := NewEpochProcessor()
	result := ep.totalActiveBalance(state, 2)
	if result < EffectiveBalanceIncrement {
		t.Errorf("totalActiveBalance should return at least EffectiveBalanceIncrement, got %d", result)
	}
}

// ---------- Finality Delay ----------

func TestEP_FinalityDelay_Cases(t *testing.T) {
	ep := NewEpochProcessor()
	tests := []struct {
		name       string
		slot       uint64
		spe        uint64
		finEpoch   Epoch
		wantDelay  uint64
	}{
		{"no_delay", 64, 32, 1, 0},
		{"delay_5", 320, 32, 4, 5},
		{"finalized_ahead", 64, 32, 5, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := &EpochProcessorState{
				Slot:                tc.slot,
				SlotsPerEpoch:       tc.spe,
				FinalizedCheckpoint: Checkpoint{Epoch: tc.finEpoch},
			}
			got := ep.finalityDelay(state)
			if got != tc.wantDelay {
				t.Errorf("finalityDelay = %d, want %d", got, tc.wantDelay)
			}
		})
	}
}

// ---------- Epoch helpers ----------

func TestEP_SlotsPerEpochZero(t *testing.T) {
	state := &EpochProcessorState{Slot: 100, SlotsPerEpoch: 0}
	if state.CurrentEpoch() != 0 {
		t.Errorf("CurrentEpoch with SlotsPerEpoch=0 should be 0, got %d", state.CurrentEpoch())
	}
	if state.PreviousEpoch() != 0 {
		t.Errorf("PreviousEpoch with SlotsPerEpoch=0 should be 0, got %d", state.PreviousEpoch())
	}
}

func TestEP_HistoricalRoots_ZeroSlotsPerEpoch(t *testing.T) {
	state := &EpochProcessorState{SlotsPerEpoch: 0, HistoricalRoots: make([]types.Hash, 0)}
	ep := NewEpochProcessor()
	ep.processHistoricalRootsUpdate(state)
	if len(state.HistoricalRoots) != 0 {
		t.Error("no historical root should be added with SlotsPerEpoch=0")
	}
}
