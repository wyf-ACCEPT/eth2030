package consensus

import (
	"testing"
)

// makeRewardInput creates a RewardInputV2 with n active validators.
func makeRewardInput(n int, currentEpoch, finalizedEpoch Epoch) *RewardInputV2 {
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

	return &RewardInputV2{
		Validators:        validators,
		Balances:          balances,
		InactivityScores:  inactScores,
		CurrentEpoch:      currentEpoch,
		FinalizedEpoch:    finalizedEpoch,
		SlotsPerEpoch:     32,
		SyncCommitteeSize: 512,
		PrevSource:        make(map[ValidatorIndex]bool),
		PrevTarget:        make(map[ValidatorIndex]bool),
		PrevHead:          make(map[ValidatorIndex]bool),
	}
}

func TestRewardCalculatorV2NilInput(t *testing.T) {
	rc := NewRewardCalculatorV2()
	_, err := rc.ComputeRewardsV2(nil)
	if err != ErrRCV2NilInput {
		t.Fatalf("expected ErrRCV2NilInput, got %v", err)
	}
}

func TestRewardCalculatorV2NoValidators(t *testing.T) {
	rc := NewRewardCalculatorV2()
	input := &RewardInputV2{
		Validators: nil,
		Balances:   nil,
	}
	_, err := rc.ComputeRewardsV2(input)
	if err != ErrRCV2NoValidators {
		t.Fatalf("expected ErrRCV2NoValidators, got %v", err)
	}
}

func TestRewardCalculatorV2LengthMismatch(t *testing.T) {
	rc := NewRewardCalculatorV2()
	input := &RewardInputV2{
		Validators: []*ValidatorV2{{}},
		Balances:   []uint64{1, 2},
	}
	_, err := rc.ComputeRewardsV2(input)
	if err != ErrRCV2LenMismatch {
		t.Fatalf("expected ErrRCV2LenMismatch, got %v", err)
	}
}

func TestRewardCalculatorV2FullParticipation(t *testing.T) {
	rc := NewRewardCalculatorV2()
	input := makeRewardInput(8, 5, 3)

	// Full participation.
	for i := 0; i < 8; i++ {
		idx := ValidatorIndex(i)
		input.PrevSource[idx] = true
		input.PrevTarget[idx] = true
		input.PrevHead[idx] = true
	}

	output, err := rc.ComputeRewardsV2(input)
	if err != nil {
		t.Fatalf("ComputeRewardsV2: %v", err)
	}

	if output.InLeakMode {
		t.Error("should not be in leak mode")
	}

	// All validators should receive positive rewards.
	for i, bd := range output.Breakdowns {
		if bd.NetDelta <= 0 {
			t.Errorf("validator %d NetDelta=%d, want positive", i, bd.NetDelta)
		}
		if bd.SourceDelta <= 0 {
			t.Errorf("validator %d SourceDelta=%d, want positive", i, bd.SourceDelta)
		}
		if bd.TargetDelta <= 0 {
			t.Errorf("validator %d TargetDelta=%d, want positive", i, bd.TargetDelta)
		}
		if bd.HeadDelta <= 0 {
			t.Errorf("validator %d HeadDelta=%d, want positive", i, bd.HeadDelta)
		}
	}

	if output.TotalRewards <= 0 {
		t.Errorf("TotalRewards=%d, want positive", output.TotalRewards)
	}
	if output.TotalPenalties != 0 {
		t.Errorf("TotalPenalties=%d, want 0", output.TotalPenalties)
	}
}

func TestRewardCalculatorV2NoParticipation(t *testing.T) {
	rc := NewRewardCalculatorV2()
	input := makeRewardInput(4, 5, 3)

	output, err := rc.ComputeRewardsV2(input)
	if err != nil {
		t.Fatalf("ComputeRewardsV2: %v", err)
	}

	// No participation => source/target penalties, zero head.
	for i, bd := range output.Breakdowns {
		if bd.SourceDelta >= 0 {
			t.Errorf("validator %d SourceDelta=%d, want negative", i, bd.SourceDelta)
		}
		if bd.TargetDelta >= 0 {
			t.Errorf("validator %d TargetDelta=%d, want negative", i, bd.TargetDelta)
		}
		if bd.HeadDelta != 0 {
			t.Errorf("validator %d HeadDelta=%d, want 0", i, bd.HeadDelta)
		}
		if bd.NetDelta >= 0 {
			t.Errorf("validator %d NetDelta=%d, want negative", i, bd.NetDelta)
		}
	}
}

func TestRewardCalculatorV2InactivityLeak(t *testing.T) {
	rc := NewRewardCalculatorV2()
	input := makeRewardInput(4, 20, 5) // finality delay = 19-5 = 14 > 4

	// Set high inactivity scores.
	for i := range input.InactivityScores {
		input.InactivityScores[i] = 200
	}

	// Validator 0 attested, rest did not.
	input.PrevSource[0] = true
	input.PrevTarget[0] = true
	input.PrevHead[0] = true

	output, err := rc.ComputeRewardsV2(input)
	if err != nil {
		t.Fatalf("ComputeRewardsV2: %v", err)
	}

	if !output.InLeakMode {
		t.Fatal("expected InLeakMode=true")
	}
	if output.FinalityDelay <= RCV2MinEpochsToInactivityPen {
		t.Errorf("FinalityDelay=%d, want >%d", output.FinalityDelay, RCV2MinEpochsToInactivityPen)
	}

	// Validator 0 should have positive net reward.
	if output.Breakdowns[0].NetDelta <= 0 {
		t.Errorf("attesting validator NetDelta=%d, want positive", output.Breakdowns[0].NetDelta)
	}

	// Others should have large negative rewards including inactivity.
	for i := 1; i < 4; i++ {
		bd := output.Breakdowns[i]
		if bd.InactivityDelta >= 0 {
			t.Errorf("validator %d InactivityDelta=%d, want negative", i, bd.InactivityDelta)
		}
		if bd.NetDelta >= 0 {
			t.Errorf("validator %d NetDelta=%d, want negative", i, bd.NetDelta)
		}
	}
}

func TestRewardCalculatorV2SyncRewards(t *testing.T) {
	rc := NewRewardCalculatorV2()
	input := makeRewardInput(4, 5, 4)

	// Full attestation participation.
	for i := 0; i < 4; i++ {
		idx := ValidatorIndex(i)
		input.PrevSource[idx] = true
		input.PrevTarget[idx] = true
		input.PrevHead[idx] = true
	}

	// Validator 0 participated in 32 sync slots.
	input.SyncParticipants = map[ValidatorIndex]uint64{
		0: 32,
	}

	output, err := rc.ComputeRewardsV2(input)
	if err != nil {
		t.Fatalf("ComputeRewardsV2: %v", err)
	}

	// Validator 0 should have sync reward > 0.
	if output.Breakdowns[0].SyncDelta <= 0 {
		t.Errorf("sync participant SyncDelta=%d, want positive", output.Breakdowns[0].SyncDelta)
	}

	// Validator 1 (not in sync committee) should have SyncDelta == 0.
	if output.Breakdowns[1].SyncDelta != 0 {
		t.Errorf("non-sync validator SyncDelta=%d, want 0", output.Breakdowns[1].SyncDelta)
	}
}

func TestRewardCalculatorV2ProposerReward(t *testing.T) {
	rc := NewRewardCalculatorV2()
	input := makeRewardInput(4, 5, 4)

	// Only validator 0 attested.
	input.PrevSource[0] = true
	input.PrevTarget[0] = true

	output, err := rc.ComputeRewardsV2(input)
	if err != nil {
		t.Fatalf("ComputeRewardsV2: %v", err)
	}

	// Validator 0 should get proposer reward.
	if output.Breakdowns[0].ProposerDelta <= 0 {
		t.Errorf("attesting validator ProposerDelta=%d, want positive",
			output.Breakdowns[0].ProposerDelta)
	}

	// Validator 1 (did not attest) should get zero proposer reward.
	if output.Breakdowns[1].ProposerDelta != 0 {
		t.Errorf("non-attesting validator ProposerDelta=%d, want 0",
			output.Breakdowns[1].ProposerDelta)
	}
}

func TestComputeBaseRewardV2(t *testing.T) {
	rc := NewRewardCalculatorV2()

	// 32 ETH effective balance, total active = 128 ETH (4 validators).
	effBal := uint64(32 * GweiPerETH)
	totalActive := uint64(128 * GweiPerETH)
	br := rc.ComputeBaseRewardV2(effBal, totalActive)

	if br == 0 {
		t.Fatal("base reward should be non-zero")
	}

	// base_reward = 32e9 * 64 / sqrt(128e9)
	// sqrt(128e9) ~ 357771
	// br ~ 32e9 * 64 / 357771 ~ 5722
	if br < 1000 || br > 10000000 {
		t.Errorf("base reward %d out of expected range", br)
	}
}

func TestComputeSyncReward(t *testing.T) {
	rc := NewRewardCalculatorV2()
	reward := rc.ComputeSyncReward(128*GweiPerETH, 32, 512)
	if reward == 0 {
		t.Fatal("sync reward should be non-zero")
	}
}

func TestComputeProposerReward(t *testing.T) {
	rc := NewRewardCalculatorV2()
	br := uint64(10000)
	pr := rc.ComputeProposerReward(br)
	if pr == 0 {
		t.Fatal("proposer reward should be non-zero")
	}
	// proposer_reward = br * 8 / (64 * 56) = br * 8 / 3584
	expected := br * 8 / (64 * 56)
	if pr != expected {
		t.Errorf("proposer reward = %d, want %d", pr, expected)
	}
}

func TestComputeInactivityPenalty(t *testing.T) {
	rc := NewRewardCalculatorV2()
	penalty := rc.ComputeInactivityPenalty(32*GweiPerETH, 100)
	if penalty == 0 {
		t.Fatal("inactivity penalty should be non-zero")
	}
	expected := uint64(32*GweiPerETH) * 100 / RCV2InactivityPenaltyQuotient
	if penalty != expected {
		t.Errorf("penalty=%d, want %d", penalty, expected)
	}
}

func TestComputeSlashingPenalty(t *testing.T) {
	rc := NewRewardCalculatorV2()
	penalty := rc.ComputeSlashingPenalty(
		32*GweiPerETH,  // effective balance
		64*GweiPerETH,  // total slashed
		256*GweiPerETH, // total active
		3,              // Bellatrix multiplier
	)
	if penalty == 0 {
		t.Fatal("slashing penalty should be non-zero")
	}
}

func TestRewardCalculatorV2InactiveValidator(t *testing.T) {
	rc := NewRewardCalculatorV2()
	input := makeRewardInput(4, 5, 4)

	// Mark validator 3 as not active in the previous epoch.
	input.Validators[3].ActivationEpoch = 100

	for i := 0; i < 4; i++ {
		idx := ValidatorIndex(i)
		input.PrevSource[idx] = true
		input.PrevTarget[idx] = true
		input.PrevHead[idx] = true
	}

	output, err := rc.ComputeRewardsV2(input)
	if err != nil {
		t.Fatalf("ComputeRewardsV2: %v", err)
	}

	// Inactive validator should get zero rewards.
	if output.Breakdowns[3].NetDelta != 0 {
		t.Errorf("inactive validator NetDelta=%d, want 0", output.Breakdowns[3].NetDelta)
	}
}

func TestRewardCalculatorV2SlashedValidatorNoReward(t *testing.T) {
	rc := NewRewardCalculatorV2()
	input := makeRewardInput(4, 5, 4)

	// Mark validator 2 as slashed.
	input.Validators[2].Slashed = true

	// Set participation for all (but slashed check should prevent rewards).
	for i := 0; i < 4; i++ {
		idx := ValidatorIndex(i)
		input.PrevSource[idx] = true
		input.PrevTarget[idx] = true
		input.PrevHead[idx] = true
	}

	output, err := rc.ComputeRewardsV2(input)
	if err != nil {
		t.Fatalf("ComputeRewardsV2: %v", err)
	}

	// Slashed validator should get penalties, not rewards, for source/target.
	bd := output.Breakdowns[2]
	if bd.SourceDelta >= 0 {
		t.Errorf("slashed validator SourceDelta=%d, want negative", bd.SourceDelta)
	}
	if bd.TargetDelta >= 0 {
		t.Errorf("slashed validator TargetDelta=%d, want negative", bd.TargetDelta)
	}
}
