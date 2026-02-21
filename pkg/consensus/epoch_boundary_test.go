package consensus

import (
	"testing"
)

// Helper to create a UnifiedBeaconState with N validators at a given epoch.
func makeEBState(n int, epoch Epoch, slotsPerEpoch uint64) *UnifiedBeaconState {
	state := NewUnifiedBeaconState(slotsPerEpoch)
	state.CurrentEpoch = epoch
	state.CurrentSlot = uint64(epoch) * slotsPerEpoch

	for i := 0; i < n; i++ {
		var pk [48]byte
		pk[0] = byte(i)
		pk[1] = byte(i >> 8)
		v := &UnifiedValidator{
			Index:                      uint64(i),
			Pubkey:                     pk,
			EffectiveBalance:           32 * GweiPerETH,
			Balance:                    32 * GweiPerETH,
			ActivationEligibilityEpoch: 0,
			ActivationEpoch:            0,
			ExitEpoch:                  FarFutureEpoch,
			WithdrawableEpoch:          FarFutureEpoch,
		}
		state.Validators = append(state.Validators, v)
		state.pubkeyIdx[pk] = uint64(i)
	}
	return state
}

func TestEpochBoundaryBasicProcess(t *testing.T) {
	cfg := DefaultEpochBoundaryConfig()
	ep := NewEpochBoundaryProcessor(cfg)
	state := makeEBState(10, 5, 32)

	atts := make([]*PendingAttestation, 10)
	for i := 0; i < 10; i++ {
		atts[i] = &PendingAttestation{
			ValidatorIndex: ValidatorIndex(i),
			SourceEpoch:    4,
			TargetEpoch:    4,
			HeadCorrect:    true,
		}
	}

	summary, err := ep.ProcessEpoch(state, atts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary.Epoch != 5 {
		t.Fatalf("expected epoch 5, got %d", summary.Epoch)
	}
	if summary.ActiveValidators != 10 {
		t.Fatalf("expected 10 active validators, got %d", summary.ActiveValidators)
	}
}

func TestEpochBoundaryNilState(t *testing.T) {
	ep := NewEpochBoundaryProcessor(DefaultEpochBoundaryConfig())
	_, err := ep.ProcessEpoch(nil, nil)
	if err != ErrEBNilState {
		t.Fatalf("expected ErrEBNilState, got %v", err)
	}
}

func TestEpochBoundaryNoValidators(t *testing.T) {
	ep := NewEpochBoundaryProcessor(DefaultEpochBoundaryConfig())
	state := NewUnifiedBeaconState(32)
	state.CurrentEpoch = 1
	_, err := ep.ProcessEpoch(state, nil)
	if err != ErrEBNoValidators {
		t.Fatalf("expected ErrEBNoValidators, got %v", err)
	}
}

func TestEpochBoundaryGenesisEpoch(t *testing.T) {
	ep := NewEpochBoundaryProcessor(DefaultEpochBoundaryConfig())
	state := makeEBState(5, 0, 32)
	_, err := ep.ProcessEpoch(state, nil)
	if err != ErrEBGenesisEpoch {
		t.Fatalf("expected ErrEBGenesisEpoch, got %v", err)
	}
}

func TestEpochBoundaryAlreadyProcessed(t *testing.T) {
	ep := NewEpochBoundaryProcessor(DefaultEpochBoundaryConfig())
	state := makeEBState(5, 3, 32)

	_, err := ep.ProcessEpoch(state, nil)
	if err != nil {
		t.Fatalf("first process should succeed: %v", err)
	}

	// Re-create state for same epoch (processor remembers).
	state2 := makeEBState(5, 3, 32)
	_, err = ep.ProcessEpoch(state2, nil)
	if err != ErrEBAlreadyProcessed {
		t.Fatalf("expected ErrEBAlreadyProcessed, got %v", err)
	}
}

func TestEpochBoundaryParticipationRates(t *testing.T) {
	cfg := DefaultEpochBoundaryConfig()
	ep := NewEpochBoundaryProcessor(cfg)
	state := makeEBState(10, 5, 32)

	// 7 of 10 validators attested correctly.
	atts := make([]*PendingAttestation, 7)
	for i := 0; i < 7; i++ {
		atts[i] = &PendingAttestation{
			ValidatorIndex: ValidatorIndex(i),
			SourceEpoch:    4,
			TargetEpoch:    4,
			HeadCorrect:    true,
		}
	}

	summary, err := ep.ProcessEpoch(state, atts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary.SourceParticipation != 0.7 {
		t.Fatalf("expected 0.7 source participation, got %f", summary.SourceParticipation)
	}
	if summary.HeadParticipation != 0.7 {
		t.Fatalf("expected 0.7 head participation, got %f", summary.HeadParticipation)
	}
}

func TestEpochBoundaryPartialHeadCorrect(t *testing.T) {
	cfg := DefaultEpochBoundaryConfig()
	ep := NewEpochBoundaryProcessor(cfg)
	state := makeEBState(10, 5, 32)

	atts := []*PendingAttestation{
		{ValidatorIndex: 0, TargetEpoch: 4, HeadCorrect: true},
		{ValidatorIndex: 1, TargetEpoch: 4, HeadCorrect: false},
		{ValidatorIndex: 2, TargetEpoch: 4, HeadCorrect: true},
	}

	summary, err := ep.ProcessEpoch(state, atts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary.HeadParticipation != 0.2 { // 2/10
		t.Fatalf("expected 0.2 head participation, got %f", summary.HeadParticipation)
	}
}

func TestEpochBoundaryActivation(t *testing.T) {
	cfg := DefaultEpochBoundaryConfig()
	ep := NewEpochBoundaryProcessor(cfg)
	state := makeEBState(5, 3, 32)

	// Add a pending validator.
	var pk [48]byte
	pk[0] = 0xFF
	pending := &UnifiedValidator{
		Index:                      5,
		Pubkey:                     pk,
		EffectiveBalance:           32 * GweiPerETH,
		Balance:                    32 * GweiPerETH,
		ActivationEligibilityEpoch: 1,
		ActivationEpoch:            FarFutureEpoch,
		ExitEpoch:                  FarFutureEpoch,
		WithdrawableEpoch:          FarFutureEpoch,
	}
	state.Validators = append(state.Validators, pending)

	summary, err := ep.ProcessEpoch(state, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summary.Activated) != 1 {
		t.Fatalf("expected 1 activation, got %d", len(summary.Activated))
	}
	if state.Validators[5].ActivationEpoch != 4 { // epoch+1
		t.Fatalf("expected activation epoch 4, got %d", state.Validators[5].ActivationEpoch)
	}
}

func TestEpochBoundaryEjection(t *testing.T) {
	cfg := DefaultEpochBoundaryConfig()
	ep := NewEpochBoundaryProcessor(cfg)
	state := makeEBState(5, 3, 32)

	// Set one validator's balance below ejection threshold.
	state.Validators[2].Balance = 15 * GweiPerETH
	state.Validators[2].EffectiveBalance = 15 * GweiPerETH

	summary, err := ep.ProcessEpoch(state, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summary.Ejected) != 1 {
		t.Fatalf("expected 1 ejection, got %d", len(summary.Ejected))
	}
	if state.Validators[2].ExitEpoch == FarFutureEpoch {
		t.Fatal("ejected validator should have a set exit epoch")
	}
}

func TestEpochBoundaryEffectiveBalanceUpdate(t *testing.T) {
	cfg := DefaultEpochBoundaryConfig()
	ep := NewEpochBoundaryProcessor(cfg)
	state := makeEBState(3, 5, 32)

	// Increase actual balance significantly.
	state.Validators[0].Balance = 64 * GweiPerETH
	// Decrease one.
	state.Validators[1].Balance = 16 * GweiPerETH

	summary, err := ep.ProcessEpoch(state, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary.EffBalUpdated < 2 {
		t.Fatalf("expected at least 2 eff balance updates, got %d", summary.EffBalUpdated)
	}
}

func TestEpochBoundaryJustification(t *testing.T) {
	cfg := DefaultEpochBoundaryConfig()
	ep := NewEpochBoundaryProcessor(cfg)
	state := makeEBState(9, 5, 32)

	// All 9 validators attested -- should justify.
	atts := make([]*PendingAttestation, 9)
	for i := 0; i < 9; i++ {
		atts[i] = &PendingAttestation{
			ValidatorIndex: ValidatorIndex(i),
			SourceEpoch:    4,
			TargetEpoch:    4,
			HeadCorrect:    true,
		}
	}

	summary, err := ep.ProcessEpoch(state, atts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !summary.CurrJustified {
		t.Fatal("expected current epoch to be justified")
	}
}

func TestEpochBoundaryNoJustificationLowParticipation(t *testing.T) {
	cfg := DefaultEpochBoundaryConfig()
	ep := NewEpochBoundaryProcessor(cfg)
	state := makeEBState(10, 5, 32)

	// Only 5 of 10 attested -- below 2/3.
	atts := make([]*PendingAttestation, 5)
	for i := 0; i < 5; i++ {
		atts[i] = &PendingAttestation{
			ValidatorIndex: ValidatorIndex(i),
			TargetEpoch:    4,
		}
	}

	summary, err := ep.ProcessEpoch(state, atts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary.CurrJustified {
		t.Fatal("should not justify with only 50% participation")
	}
}

func TestEpochBoundaryIsEpochBoundary(t *testing.T) {
	ep := NewEpochBoundaryProcessor(EpochBoundaryConfig{SlotsPerEpoch: 4})

	if !ep.IsEpochBoundary(3) { // slot 3 is last of epoch 0
		t.Fatal("slot 3 should be boundary for spe=4")
	}
	if ep.IsEpochBoundary(2) {
		t.Fatal("slot 2 should not be boundary")
	}
	if !ep.IsEpochBoundary(7) { // slot 7 is last of epoch 1
		t.Fatal("slot 7 should be boundary")
	}
}

func TestEpochBoundaryHasProcessed(t *testing.T) {
	ep := NewEpochBoundaryProcessor(DefaultEpochBoundaryConfig())
	state := makeEBState(5, 3, 32)

	if ep.HasProcessed(3) {
		t.Fatal("epoch 3 should not be processed yet")
	}
	ep.ProcessEpoch(state, nil)
	if !ep.HasProcessed(3) {
		t.Fatal("epoch 3 should be processed")
	}
}

func TestEpochBoundaryReset(t *testing.T) {
	ep := NewEpochBoundaryProcessor(DefaultEpochBoundaryConfig())
	state := makeEBState(5, 3, 32)
	ep.ProcessEpoch(state, nil)

	ep.Reset()
	if ep.ProcessedCount() != 0 {
		t.Fatal("reset should clear processed epochs")
	}
}

func TestEpochBoundaryComputeParticipationRate(t *testing.T) {
	summary := &EpochSummary{
		ActiveValidators:    10,
		SourceParticipation: 0.8,
		TargetParticipation: 0.7,
		HeadParticipation:   0.6,
	}
	rate := ComputeParticipationRate(summary)
	expected := (0.8 + 0.7 + 0.6) / 3.0
	diff := rate - expected
	if diff < 0 {
		diff = -diff
	}
	if diff > 0.0001 {
		t.Fatalf("expected ~%f, got %f", expected, rate)
	}
}

func TestEpochBoundaryComputeParticipationRateNil(t *testing.T) {
	if ComputeParticipationRate(nil) != 0 {
		t.Fatal("nil summary should return 0")
	}
}

func TestEpochBoundaryExitedValidators(t *testing.T) {
	cfg := DefaultEpochBoundaryConfig()
	ep := NewEpochBoundaryProcessor(cfg)
	state := makeEBState(5, 10, 32)

	// Set one validator as exited but not withdrawable.
	state.Validators[3].ExitEpoch = 8
	state.Validators[3].WithdrawableEpoch = 264

	summary, err := ep.ProcessEpoch(state, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summary.Exited) != 1 {
		t.Fatalf("expected 1 exited, got %d", len(summary.Exited))
	}
	if summary.Exited[0] != 3 {
		t.Fatalf("expected validator 3, got %d", summary.Exited[0])
	}
}

func TestEpochBoundarySlashedNotCounted(t *testing.T) {
	cfg := DefaultEpochBoundaryConfig()
	ep := NewEpochBoundaryProcessor(cfg)
	state := makeEBState(5, 5, 32)

	state.Validators[0].Slashed = true

	atts := []*PendingAttestation{
		{ValidatorIndex: 0, TargetEpoch: 4, HeadCorrect: true},
		{ValidatorIndex: 1, TargetEpoch: 4, HeadCorrect: true},
	}

	summary, err := ep.ProcessEpoch(state, atts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only validator 1 should count (0 is slashed).
	if summary.SourceParticipation != 0.2 { // 1/5
		t.Fatalf("expected 0.2, got %f", summary.SourceParticipation)
	}
}

func TestEpochBoundaryActivationChurnLimit(t *testing.T) {
	cfg := DefaultEpochBoundaryConfig()
	cfg.ActivationChurn = 2
	ep := NewEpochBoundaryProcessor(cfg)
	state := makeEBState(3, 5, 32)

	// Add 5 pending validators.
	for i := 0; i < 5; i++ {
		var pk [48]byte
		pk[0] = byte(100 + i)
		v := &UnifiedValidator{
			Index:                      uint64(3 + i),
			Pubkey:                     pk,
			EffectiveBalance:           32 * GweiPerETH,
			Balance:                    32 * GweiPerETH,
			ActivationEligibilityEpoch: 1,
			ActivationEpoch:            FarFutureEpoch,
			ExitEpoch:                  FarFutureEpoch,
			WithdrawableEpoch:          FarFutureEpoch,
		}
		state.Validators = append(state.Validators, v)
	}

	summary, err := ep.ProcessEpoch(state, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only 2 should be activated due to churn limit.
	if len(summary.Activated) != 2 {
		t.Fatalf("expected 2 activations (churn limit), got %d", len(summary.Activated))
	}
}

func TestEpochBoundaryTotalActiveBalance(t *testing.T) {
	cfg := DefaultEpochBoundaryConfig()
	ep := NewEpochBoundaryProcessor(cfg)
	state := makeEBState(4, 5, 32)

	summary, err := ep.ProcessEpoch(state, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := uint64(4) * 32 * GweiPerETH
	if summary.TotalActiveBalance != expected {
		t.Fatalf("expected %d, got %d", expected, summary.TotalActiveBalance)
	}
}
