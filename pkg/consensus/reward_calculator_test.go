package consensus

import (
	"sync"
	"testing"
)

// makeRewardState creates a test reward state with n active validators each
// holding the given effective balance.
func makeRewardState(n int, effBal uint64, finalizedEpoch, currentEpoch Epoch) *RewardState {
	validators := make([]*RewardValidator, n)
	balances := make([]uint64, n)
	for i := 0; i < n; i++ {
		validators[i] = &RewardValidator{
			EffectiveBalance: effBal,
			Active:           true,
		}
		balances[i] = effBal
	}
	return &RewardState{
		Validators:        validators,
		Balances:          balances,
		CurrentEpoch:      currentEpoch,
		FinalizedEpoch:    finalizedEpoch,
		SlotsPerEpoch:     32,
		SyncCommitteeSize: 512,
	}
}

// fullParticipation returns participation with all validators attesting to all components.
func fullParticipation(n int) *Participation {
	p := NewParticipation()
	for i := 0; i < n; i++ {
		p.Source[i] = true
		p.Target[i] = true
		p.Head[i] = true
	}
	return p
}

func TestRewardCalculatorNilState(t *testing.T) {
	rc := NewRewardCalculator(nil)
	_, err := rc.ComputeRewards(nil, NewParticipation())
	if err != ErrRCNilState {
		t.Fatalf("expected ErrRCNilState, got %v", err)
	}
}

func TestRewardCalculatorNilParticipation(t *testing.T) {
	rc := NewRewardCalculator(nil)
	state := makeRewardState(10, 32*GweiPerETH, 0, 1)
	_, err := rc.ComputeRewards(state, nil)
	if err != ErrRCNilParticipation {
		t.Fatalf("expected ErrRCNilParticipation, got %v", err)
	}
}

func TestRewardCalculatorNoValidators(t *testing.T) {
	rc := NewRewardCalculator(nil)
	state := &RewardState{
		Validators: []*RewardValidator{},
		Balances:   []uint64{},
	}
	_, err := rc.ComputeRewards(state, NewParticipation())
	if err != ErrRCNoValidators {
		t.Fatalf("expected ErrRCNoValidators, got %v", err)
	}
}

func TestRewardCalculatorLengthMismatch(t *testing.T) {
	rc := NewRewardCalculator(nil)
	state := &RewardState{
		Validators: []*RewardValidator{{Active: true, EffectiveBalance: 32 * GweiPerETH}},
		Balances:   []uint64{},
	}
	_, err := rc.ComputeRewards(state, NewParticipation())
	if err != ErrRCLengthMismatch {
		t.Fatalf("expected ErrRCLengthMismatch, got %v", err)
	}
}

func TestRewardCalculatorPhase0FullParticipation(t *testing.T) {
	n := 100
	effBal := uint64(32 * GweiPerETH)
	state := makeRewardState(n, effBal, 0, 1)
	part := fullParticipation(n)

	rc := NewRewardCalculator(DefaultRewardConfig())
	summary, err := rc.ComputeRewards(state, part)
	if err != nil {
		t.Fatalf("ComputeRewards failed: %v", err)
	}

	if len(summary.Validators) != n {
		t.Fatalf("expected %d validators, got %d", n, len(summary.Validators))
	}

	// All validators should have positive rewards with full participation.
	for i, vr := range summary.Validators {
		if vr.NetReward <= 0 {
			t.Fatalf("validator %d has non-positive reward: %d", i, vr.NetReward)
		}
		if vr.SourceReward <= 0 {
			t.Fatalf("validator %d has non-positive source reward: %d", i, vr.SourceReward)
		}
		if vr.TargetReward <= 0 {
			t.Fatalf("validator %d has non-positive target reward: %d", i, vr.TargetReward)
		}
		if vr.HeadReward <= 0 {
			t.Fatalf("validator %d has non-positive head reward: %d", i, vr.HeadReward)
		}
		if vr.InactivityPen != 0 {
			t.Fatalf("validator %d has inactivity penalty with full participation: %d", i, vr.InactivityPen)
		}
	}

	if summary.TotalRewards <= 0 {
		t.Fatal("expected positive total rewards")
	}
	if summary.TotalPenalties != 0 {
		t.Fatalf("expected zero penalties with full participation, got %d", summary.TotalPenalties)
	}
	if summary.InLeakMode {
		t.Fatal("should not be in leak mode")
	}
}

func TestRewardCalculatorPhase0NoParticipation(t *testing.T) {
	n := 10
	effBal := uint64(32 * GweiPerETH)
	state := makeRewardState(n, effBal, 0, 1)
	part := NewParticipation() // no participation

	rc := NewRewardCalculator(DefaultRewardConfig())
	summary, err := rc.ComputeRewards(state, part)
	if err != nil {
		t.Fatalf("ComputeRewards failed: %v", err)
	}

	// All validators should have negative rewards with zero participation.
	for i, vr := range summary.Validators {
		if vr.NetReward >= 0 {
			t.Fatalf("validator %d should have negative reward: %d", i, vr.NetReward)
		}
		if vr.SourceReward >= 0 {
			t.Fatalf("validator %d should have negative source: %d", i, vr.SourceReward)
		}
	}

	if summary.TotalPenalties <= 0 {
		t.Fatal("expected positive penalties with zero participation")
	}
}

func TestRewardCalculatorPhase0InactivityLeak(t *testing.T) {
	n := 10
	effBal := uint64(32 * GweiPerETH)
	// finality delay > 4 triggers inactivity leak
	state := makeRewardState(n, effBal, 0, 10)
	part := NewParticipation() // no participation

	rc := NewRewardCalculator(DefaultRewardConfig())
	summary, err := rc.ComputeRewards(state, part)
	if err != nil {
		t.Fatalf("ComputeRewards failed: %v", err)
	}

	if !summary.InLeakMode {
		t.Fatal("expected leak mode with high finality delay")
	}
	if summary.FinalityDelay != 10 {
		t.Fatalf("expected finality delay 10, got %d", summary.FinalityDelay)
	}

	// Non-attesters should have inactivity penalties.
	for i, vr := range summary.Validators {
		if vr.InactivityPen >= 0 {
			t.Fatalf("validator %d should have inactivity penalty: %d", i, vr.InactivityPen)
		}
	}
}

func TestRewardCalculatorPhase0InactivityLeak_AttersExempt(t *testing.T) {
	n := 10
	effBal := uint64(32 * GweiPerETH)
	state := makeRewardState(n, effBal, 0, 10)

	// Only validator 0 attests to target.
	part := NewParticipation()
	part.Source[0] = true
	part.Target[0] = true
	part.Head[0] = true

	rc := NewRewardCalculator(DefaultRewardConfig())
	summary, err := rc.ComputeRewards(state, part)
	if err != nil {
		t.Fatalf("ComputeRewards failed: %v", err)
	}

	// Validator 0 should have no inactivity penalty.
	if summary.Validators[0].InactivityPen != 0 {
		t.Fatalf("attesting validator should have no inactivity penalty, got %d",
			summary.Validators[0].InactivityPen)
	}

	// Non-attesting validators should have penalties.
	for i := 1; i < n; i++ {
		if summary.Validators[i].InactivityPen >= 0 {
			t.Fatalf("non-attesting validator %d should have inactivity penalty", i)
		}
	}
}

func TestRewardCalculatorPhase0ProposerReward(t *testing.T) {
	n := 10
	effBal := uint64(32 * GweiPerETH)
	state := makeRewardState(n, effBal, 0, 1)
	part := fullParticipation(n)

	rc := NewRewardCalculator(DefaultRewardConfig())
	summary, err := rc.ComputeRewards(state, part)
	if err != nil {
		t.Fatalf("ComputeRewards failed: %v", err)
	}

	// All attesters should get an inclusion reward.
	for i, vr := range summary.Validators {
		if vr.InclusionReward <= 0 {
			t.Fatalf("validator %d should have positive inclusion reward, got %d", i, vr.InclusionReward)
		}
	}
}

func TestRewardCalculatorPhase0SlashedValidator(t *testing.T) {
	n := 5
	effBal := uint64(32 * GweiPerETH)
	state := makeRewardState(n, effBal, 0, 1)
	state.Validators[2].Slashed = true

	// Even though validator 2 is in participation, being slashed negates it.
	part := fullParticipation(n)

	rc := NewRewardCalculator(DefaultRewardConfig())
	summary, err := rc.ComputeRewards(state, part)
	if err != nil {
		t.Fatalf("ComputeRewards failed: %v", err)
	}

	// Slashed validator should have negative rewards.
	if summary.Validators[2].SourceReward >= 0 {
		t.Fatalf("slashed validator should have negative source reward: %d",
			summary.Validators[2].SourceReward)
	}
}

func TestRewardCalculatorPhase0InactiveValidator(t *testing.T) {
	n := 5
	effBal := uint64(32 * GweiPerETH)
	state := makeRewardState(n, effBal, 0, 1)
	state.Validators[3].Active = false

	part := fullParticipation(n)

	rc := NewRewardCalculator(DefaultRewardConfig())
	summary, err := rc.ComputeRewards(state, part)
	if err != nil {
		t.Fatalf("ComputeRewards failed: %v", err)
	}

	// Inactive validator should have zero rewards.
	vr := summary.Validators[3]
	if vr.NetReward != 0 {
		t.Fatalf("inactive validator should have zero net reward, got %d", vr.NetReward)
	}
}

func TestRewardCalculatorAltairFullParticipation(t *testing.T) {
	n := 100
	effBal := uint64(32 * GweiPerETH)
	state := makeRewardState(n, effBal, 0, 1)
	part := fullParticipation(n)

	rc := NewRewardCalculator(AltairRewardConfig())
	summary, err := rc.ComputeRewards(state, part)
	if err != nil {
		t.Fatalf("ComputeRewards failed: %v", err)
	}

	for i, vr := range summary.Validators {
		if vr.NetReward <= 0 {
			t.Fatalf("Altair validator %d has non-positive reward: %d", i, vr.NetReward)
		}
		if vr.SourceReward <= 0 {
			t.Fatalf("Altair validator %d has non-positive source: %d", i, vr.SourceReward)
		}
		if vr.TargetReward <= 0 {
			t.Fatalf("Altair validator %d has non-positive target: %d", i, vr.TargetReward)
		}
		if vr.SyncReward <= 0 {
			t.Fatalf("Altair validator %d has non-positive sync reward: %d", i, vr.SyncReward)
		}
	}

	if summary.TotalRewards <= 0 {
		t.Fatal("expected positive total Altair rewards")
	}
}

func TestRewardCalculatorAltairNoParticipation(t *testing.T) {
	n := 10
	effBal := uint64(32 * GweiPerETH)
	state := makeRewardState(n, effBal, 0, 1)
	part := NewParticipation()

	rc := NewRewardCalculator(AltairRewardConfig())
	summary, err := rc.ComputeRewards(state, part)
	if err != nil {
		t.Fatalf("ComputeRewards failed: %v", err)
	}

	for i, vr := range summary.Validators {
		if vr.SourceReward >= 0 {
			t.Fatalf("Altair validator %d should have negative source: %d", i, vr.SourceReward)
		}
		if vr.TargetReward >= 0 {
			t.Fatalf("Altair validator %d should have negative target: %d", i, vr.TargetReward)
		}
		// In Altair, non-attestation to head gives 0 (not negative).
		if vr.HeadReward != 0 {
			t.Fatalf("Altair validator %d should have zero head reward: %d", i, vr.HeadReward)
		}
	}
}

func TestRewardCalculatorAltairInactivityLeak(t *testing.T) {
	n := 10
	effBal := uint64(32 * GweiPerETH)
	state := makeRewardState(n, effBal, 0, 10)
	part := NewParticipation()

	rc := NewRewardCalculator(AltairRewardConfig())
	summary, err := rc.ComputeRewards(state, part)
	if err != nil {
		t.Fatalf("ComputeRewards failed: %v", err)
	}

	if !summary.InLeakMode {
		t.Fatal("expected leak mode in Altair")
	}

	for i, vr := range summary.Validators {
		if vr.InactivityPen >= 0 {
			t.Fatalf("Altair validator %d should have inactivity penalty: %d", i, vr.InactivityPen)
		}
	}
}

func TestRewardCalculatorAltairSyncRewardComputation(t *testing.T) {
	n := 10
	effBal := uint64(32 * GweiPerETH)
	state := makeRewardState(n, effBal, 0, 1)
	part := fullParticipation(n)

	rc := NewRewardCalculator(AltairRewardConfig())
	summary, err := rc.ComputeRewards(state, part)
	if err != nil {
		t.Fatalf("ComputeRewards failed: %v", err)
	}

	totalActive := uint64(n) * effBal
	expectedSync := int64(totalActive / 512 / 32) // total / sync_committee_size / slots_per_epoch

	for i, vr := range summary.Validators {
		if vr.SyncReward != expectedSync {
			t.Fatalf("validator %d expected sync reward %d, got %d", i, expectedSync, vr.SyncReward)
		}
	}
}

func TestRewardCalculatorBaseReward(t *testing.T) {
	rc := NewRewardCalculator(DefaultRewardConfig())
	effBal := uint64(32 * GweiPerETH)
	totalActive := effBal * 100
	sqrtTotal := intSqrtReward(totalActive)

	br := rc.baseReward(effBal, sqrtTotal)
	if br == 0 {
		t.Fatal("base reward should be non-zero")
	}

	// base_reward = eff_bal * 64 / sqrt(total) / 4
	expected := effBal * 64 / sqrtTotal / 4
	if br != expected {
		t.Fatalf("expected base reward %d, got %d", expected, br)
	}
}

func TestRewardCalculatorBaseRewardAltair(t *testing.T) {
	rc := NewRewardCalculator(AltairRewardConfig())
	effBal := uint64(32 * GweiPerETH)
	totalActive := effBal * 100
	sqrtTotal := intSqrtReward(totalActive)

	br := rc.baseRewardAltair(effBal, sqrtTotal)
	if br == 0 {
		t.Fatal("Altair base reward should be non-zero")
	}

	// Altair: base_reward = eff_bal * 64 / sqrt(total)
	expected := effBal * 64 / sqrtTotal
	if br != expected {
		t.Fatalf("expected Altair base reward %d, got %d", expected, br)
	}
}

func TestIntSqrtReward(t *testing.T) {
	tests := []struct {
		input    uint64
		expected uint64
	}{
		{0, 0},
		{1, 1},
		{4, 2},
		{9, 3},
		{10, 3},
		{100, 10},
		{10000, 100},
		{1_000_000, 1000},
	}
	for _, tt := range tests {
		got := intSqrtReward(tt.input)
		if got != tt.expected {
			t.Errorf("intSqrtReward(%d) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

func TestRewardCalculatorThreadSafety(t *testing.T) {
	rc := NewRewardCalculator(DefaultRewardConfig())
	n := 50
	state := makeRewardState(n, 32*GweiPerETH, 0, 1)
	part := fullParticipation(n)

	var wg sync.WaitGroup
	errs := make(chan error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := rc.ComputeRewards(state, part)
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent ComputeRewards failed: %v", err)
	}
}

func TestRewardCalculatorZeroBalance(t *testing.T) {
	rc := NewRewardCalculator(nil)
	state := &RewardState{
		Validators: []*RewardValidator{
			{EffectiveBalance: 0, Active: false},
		},
		Balances: []uint64{0},
	}
	_, err := rc.ComputeRewards(state, NewParticipation())
	if err != ErrRCZeroBalance {
		t.Fatalf("expected ErrRCZeroBalance, got %v", err)
	}
}

func TestRewardCalculatorMixedParticipation(t *testing.T) {
	n := 10
	effBal := uint64(32 * GweiPerETH)
	state := makeRewardState(n, effBal, 0, 1)

	// Only odd validators participate.
	part := NewParticipation()
	for i := 0; i < n; i++ {
		if i%2 == 1 {
			part.Source[i] = true
			part.Target[i] = true
			part.Head[i] = true
		}
	}

	rc := NewRewardCalculator(DefaultRewardConfig())
	summary, err := rc.ComputeRewards(state, part)
	if err != nil {
		t.Fatalf("ComputeRewards failed: %v", err)
	}

	// Odd validators should have positive rewards.
	for i := 1; i < n; i += 2 {
		if summary.Validators[i].NetReward <= 0 {
			t.Fatalf("participating validator %d should have positive reward: %d",
				i, summary.Validators[i].NetReward)
		}
	}

	// Even validators should have negative rewards.
	for i := 0; i < n; i += 2 {
		if summary.Validators[i].NetReward >= 0 {
			t.Fatalf("non-participating validator %d should have negative reward: %d",
				i, summary.Validators[i].NetReward)
		}
	}
}

func TestRewardCalculatorDifferentBalances(t *testing.T) {
	state := &RewardState{
		Validators: []*RewardValidator{
			{EffectiveBalance: 32 * GweiPerETH, Active: true},
			{EffectiveBalance: 64 * GweiPerETH, Active: true},
		},
		Balances:       []uint64{32 * GweiPerETH, 64 * GweiPerETH},
		CurrentEpoch:   1,
		FinalizedEpoch: 0,
		SlotsPerEpoch:  32,
	}
	part := fullParticipation(2)

	rc := NewRewardCalculator(DefaultRewardConfig())
	summary, err := rc.ComputeRewards(state, part)
	if err != nil {
		t.Fatalf("ComputeRewards failed: %v", err)
	}

	// Validator with higher effective balance should earn more.
	if summary.Validators[1].NetReward <= summary.Validators[0].NetReward {
		t.Fatalf("higher-balance validator should earn more: v0=%d, v1=%d",
			summary.Validators[0].NetReward, summary.Validators[1].NetReward)
	}
}

func TestRewardCalculatorFinalityDelayComputation(t *testing.T) {
	state := makeRewardState(5, 32*GweiPerETH, 3, 8)
	part := fullParticipation(5)

	rc := NewRewardCalculator(DefaultRewardConfig())
	summary, err := rc.ComputeRewards(state, part)
	if err != nil {
		t.Fatalf("ComputeRewards failed: %v", err)
	}

	if summary.FinalityDelay != 5 {
		t.Fatalf("expected finality delay 5, got %d", summary.FinalityDelay)
	}
	if !summary.InLeakMode {
		t.Fatal("expected leak mode with finality delay 5")
	}
}
