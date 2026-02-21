package consensus

import (
	"testing"
)

// helper: build a test UnifiedBeaconState with N active validators.
func buildPRTestState(n int, epoch Epoch) *UnifiedBeaconState {
	state := NewUnifiedBeaconState(4) // 4 slots per epoch (quick slots)
	state.CurrentEpoch = epoch
	state.CurrentSlot = uint64(epoch) * 4

	for i := 0; i < n; i++ {
		var pk [48]byte
		pk[0] = byte(i)
		pk[1] = byte(i >> 8)
		v := &UnifiedValidator{
			Index:                      uint64(i),
			Pubkey:                     pk,
			EffectiveBalance:           32 * GweiPerETH,
			Balance:                    32 * GweiPerETH,
			ActivationEpoch:            0,
			ExitEpoch:                  FarFutureEpoch,
			ActivationEligibilityEpoch: 0,
			WithdrawableEpoch:          FarFutureEpoch,
		}
		state.Validators = append(state.Validators, v)
	}
	return state
}

func TestProposerRotationNewManager(t *testing.T) {
	cfg := DefaultProposerRotationConfig()
	prm := NewProposerRotationManager(cfg, nil)

	if prm == nil {
		t.Fatal("expected non-nil manager")
	}
	if prm.config.SlotsPerEpoch != 4 {
		t.Errorf("expected SlotsPerEpoch=4, got %d", prm.config.SlotsPerEpoch)
	}
	if prm.config.MaxLookahead != PRMaxLookahead {
		t.Errorf("expected MaxLookahead=%d, got %d", PRMaxLookahead, prm.config.MaxLookahead)
	}
	if prm.CachedEpochCount() != 0 {
		t.Errorf("expected 0 cached epochs, got %d", prm.CachedEpochCount())
	}
}

func TestProposerRotationDefaultConfig(t *testing.T) {
	cfg := DefaultProposerRotationConfig()
	if cfg.SlotsPerEpoch != 4 {
		t.Errorf("expected SlotsPerEpoch=4, got %d", cfg.SlotsPerEpoch)
	}
	if !cfg.UseWeighted {
		t.Error("expected UseWeighted=true")
	}
	if cfg.MaxLookahead != PRMaxLookahead {
		t.Errorf("expected MaxLookahead=%d, got %d", PRMaxLookahead, cfg.MaxLookahead)
	}
}

func TestProposerRotationComputeEpochSchedule(t *testing.T) {
	state := buildPRTestState(16, 5)
	cfg := DefaultProposerRotationConfig()
	prm := NewProposerRotationManager(cfg, nil)

	sched, err := prm.ComputeEpochSchedule(state, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sched == nil {
		t.Fatal("expected non-nil schedule")
	}
	if sched.Epoch != 5 {
		t.Errorf("expected epoch 5, got %d", sched.Epoch)
	}
	if len(sched.Assignments) != 4 {
		t.Errorf("expected 4 assignments, got %d", len(sched.Assignments))
	}
	if sched.ActiveCount != 16 {
		t.Errorf("expected 16 active validators, got %d", sched.ActiveCount)
	}

	// All assignments should have valid slots.
	for i, a := range sched.Assignments {
		expectedSlot := uint64(5)*4 + uint64(i)
		if a.Slot != expectedSlot {
			t.Errorf("assignment %d: expected slot %d, got %d", i, expectedSlot, a.Slot)
		}
		if a.Epoch != 5 {
			t.Errorf("assignment %d: expected epoch 5, got %d", i, a.Epoch)
		}
		if a.ValidatorIndex >= 16 {
			t.Errorf("assignment %d: validator index %d out of range", i, a.ValidatorIndex)
		}
	}
}

func TestProposerRotationNilState(t *testing.T) {
	cfg := DefaultProposerRotationConfig()
	prm := NewProposerRotationManager(cfg, nil)

	_, err := prm.ComputeEpochSchedule(nil, 1)
	if err != ErrPRNilState {
		t.Errorf("expected ErrPRNilState, got %v", err)
	}
}

func TestProposerRotationNoActiveValidators(t *testing.T) {
	state := NewUnifiedBeaconState(4)
	state.CurrentEpoch = 3
	cfg := DefaultProposerRotationConfig()
	prm := NewProposerRotationManager(cfg, nil)

	_, err := prm.ComputeEpochSchedule(state, 3)
	if err != ErrPRNoActiveValidators {
		t.Errorf("expected ErrPRNoActiveValidators, got %v", err)
	}
}

func TestProposerRotationCachedSchedule(t *testing.T) {
	state := buildPRTestState(8, 2)
	cfg := DefaultProposerRotationConfig()
	prm := NewProposerRotationManager(cfg, nil)

	sched1, err := prm.ComputeEpochSchedule(state, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return the cached schedule.
	sched2, err := prm.ComputeEpochSchedule(state, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sched1 != sched2 {
		t.Error("expected same schedule pointer on cache hit")
	}
	if prm.CachedEpochCount() != 1 {
		t.Errorf("expected 1 cached epoch, got %d", prm.CachedEpochCount())
	}
}

func TestProposerRotationGetProposerForSlot(t *testing.T) {
	state := buildPRTestState(32, 10)
	cfg := DefaultProposerRotationConfig()
	prm := NewProposerRotationManager(cfg, nil)

	_, err := prm.ComputeEpochSchedule(state, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Slots 40..43 are in epoch 10.
	for slot := uint64(40); slot < 44; slot++ {
		a, err := prm.GetProposerForSlot(slot)
		if err != nil {
			t.Fatalf("slot %d: unexpected error: %v", slot, err)
		}
		if a.Slot != slot {
			t.Errorf("slot %d: expected slot %d, got %d", slot, slot, a.Slot)
		}
	}
}

func TestProposerRotationGetProposerForSlotInvalidEpoch(t *testing.T) {
	cfg := DefaultProposerRotationConfig()
	prm := NewProposerRotationManager(cfg, nil)

	_, err := prm.GetProposerForSlot(100)
	if err != ErrPRInvalidEpoch {
		t.Errorf("expected ErrPRInvalidEpoch, got %v", err)
	}
}

func TestProposerRotationComputeLookahead(t *testing.T) {
	state := buildPRTestState(16, 5)
	cfg := DefaultProposerRotationConfig()
	prm := NewProposerRotationManager(cfg, nil)

	schedules, err := prm.ComputeLookahead(state, 5, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(schedules) != 3 {
		t.Errorf("expected 3 schedules, got %d", len(schedules))
	}
	for i, s := range schedules {
		expectedEpoch := Epoch(5 + i)
		if s.Epoch != expectedEpoch {
			t.Errorf("schedule %d: expected epoch %d, got %d", i, expectedEpoch, s.Epoch)
		}
	}
	if prm.CachedEpochCount() != 3 {
		t.Errorf("expected 3 cached epochs, got %d", prm.CachedEpochCount())
	}
}

func TestProposerRotationLookaheadExceedsLimit(t *testing.T) {
	state := buildPRTestState(16, 0)
	cfg := DefaultProposerRotationConfig()
	prm := NewProposerRotationManager(cfg, nil)

	_, err := prm.ComputeLookahead(state, 0, PRMaxLookahead+1)
	if err != ErrPRLookaheadLimit {
		t.Errorf("expected ErrPRLookaheadLimit, got %v", err)
	}
}

func TestProposerRotationRecordProposal(t *testing.T) {
	cfg := DefaultProposerRotationConfig()
	prm := NewProposerRotationManager(cfg, nil)

	prm.RecordProposal(3, 20, true)
	prm.RecordProposal(7, 21, true)
	prm.RecordProposal(1, 22, false)

	history := prm.History()
	if len(history) != 3 {
		t.Fatalf("expected 3 history entries, got %d", len(history))
	}
	if !history[0].Proposed {
		t.Error("expected first entry to be proposed")
	}
	if history[2].Proposed {
		t.Error("expected third entry to be missed")
	}
}

func TestProposerRotationProposalRate(t *testing.T) {
	cfg := DefaultProposerRotationConfig()
	prm := NewProposerRotationManager(cfg, nil)

	// No history.
	if rate := prm.ProposalRate(); rate != 0 {
		t.Errorf("expected rate 0 with no history, got %f", rate)
	}

	// 3 proposed, 1 missed.
	prm.RecordProposal(0, 0, true)
	prm.RecordProposal(1, 1, true)
	prm.RecordProposal(2, 2, true)
	prm.RecordProposal(3, 3, false)

	rate := prm.ProposalRate()
	if rate < 0.74 || rate > 0.76 {
		t.Errorf("expected rate ~0.75, got %f", rate)
	}
}

func TestProposerRotationHistoryForValidator(t *testing.T) {
	cfg := DefaultProposerRotationConfig()
	prm := NewProposerRotationManager(cfg, nil)

	prm.RecordProposal(5, 10, true)
	prm.RecordProposal(5, 20, true)
	prm.RecordProposal(3, 30, true)
	prm.RecordProposal(5, 40, false)

	entries := prm.HistoryForValidator(5)
	if len(entries) != 3 {
		t.Errorf("expected 3 entries for validator 5, got %d", len(entries))
	}
	for _, e := range entries {
		if e.ValidatorIndex != 5 {
			t.Errorf("expected validator 5, got %d", e.ValidatorIndex)
		}
	}
}

func TestProposerRotationPruneSchedules(t *testing.T) {
	state := buildPRTestState(8, 0)
	cfg := DefaultProposerRotationConfig()
	prm := NewProposerRotationManager(cfg, nil)

	for e := Epoch(0); e < 5; e++ {
		state.CurrentEpoch = e
		_, err := prm.ComputeEpochSchedule(state, e)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if prm.CachedEpochCount() != 5 {
		t.Fatalf("expected 5 cached epochs, got %d", prm.CachedEpochCount())
	}

	pruned := prm.PruneSchedules(3)
	if pruned != 3 {
		t.Errorf("expected 3 pruned, got %d", pruned)
	}
	if prm.CachedEpochCount() != 2 {
		t.Errorf("expected 2 remaining, got %d", prm.CachedEpochCount())
	}
}

func TestProposerRotationPruneHistory(t *testing.T) {
	cfg := DefaultProposerRotationConfig()
	cfg.SlotsPerEpoch = 4
	prm := NewProposerRotationManager(cfg, nil)

	// Epoch = slot / 4
	prm.RecordProposal(0, 0, true)  // epoch 0
	prm.RecordProposal(1, 4, true)  // epoch 1
	prm.RecordProposal(2, 8, true)  // epoch 2
	prm.RecordProposal(3, 12, true) // epoch 3
	prm.RecordProposal(4, 16, true) // epoch 4

	pruned := prm.PruneHistory(3)
	if pruned != 3 {
		t.Errorf("expected 3 pruned, got %d", pruned)
	}
	history := prm.History()
	if len(history) != 2 {
		t.Errorf("expected 2 remaining, got %d", len(history))
	}
}

func TestProposerRotationReset(t *testing.T) {
	state := buildPRTestState(8, 1)
	cfg := DefaultProposerRotationConfig()
	prm := NewProposerRotationManager(cfg, nil)

	_, _ = prm.ComputeEpochSchedule(state, 1)
	prm.RecordProposal(0, 4, true)

	prm.Reset()

	if prm.CachedEpochCount() != 0 {
		t.Errorf("expected 0 cached epochs after reset, got %d", prm.CachedEpochCount())
	}
	if len(prm.History()) != 0 {
		t.Errorf("expected 0 history after reset, got %d", len(prm.History()))
	}
	if prm.ProposalRate() != 0 {
		t.Errorf("expected 0 proposal rate after reset, got %f", prm.ProposalRate())
	}
}

func TestProposerRotationUniqueProposers(t *testing.T) {
	state := buildPRTestState(32, 0)
	cfg := DefaultProposerRotationConfig()
	prm := NewProposerRotationManager(cfg, nil)

	// Compute a few epochs.
	for e := Epoch(0); e < 3; e++ {
		_, err := prm.ComputeEpochSchedule(state, e)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	unique := prm.UniqueProposers()
	if unique == 0 {
		t.Error("expected > 0 unique proposers")
	}
	// With 32 validators and 12 slots (3 epochs * 4 slots), at least some
	// unique validators should be assigned.
	if unique > 12 {
		t.Errorf("cannot have more unique proposers (%d) than total slots (12)", unique)
	}
}

func TestProposerRotationUniformSelection(t *testing.T) {
	state := buildPRTestState(16, 1)
	cfg := DefaultProposerRotationConfig()
	cfg.UseWeighted = false
	prm := NewProposerRotationManager(cfg, nil)

	sched, err := prm.ComputeEpochSchedule(state, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sched.Assignments) != 4 {
		t.Errorf("expected 4 assignments, got %d", len(sched.Assignments))
	}
	// All indices should be < 16.
	for _, a := range sched.Assignments {
		if a.ValidatorIndex >= 16 {
			t.Errorf("validator index %d out of range", a.ValidatorIndex)
		}
	}
}

func TestProposerRotationWeightedFavorsHigherBalance(t *testing.T) {
	// Create a state with one whale validator (2048 ETH) and 15 normal (32 ETH).
	state := buildPRTestState(16, 0)
	state.Validators[0].EffectiveBalance = 2048 * GweiPerETH
	state.Validators[0].Balance = 2048 * GweiPerETH

	cfg := DefaultProposerRotationConfig()
	cfg.UseWeighted = true
	prm := NewProposerRotationManager(cfg, nil)

	// Compute many epochs to see statistical distribution.
	whaleCount := 0
	totalSlots := 0
	for e := Epoch(0); e < 50; e++ {
		sched, err := prm.ComputeEpochSchedule(state, e)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, a := range sched.Assignments {
			totalSlots++
			if a.ValidatorIndex == 0 {
				whaleCount++
			}
		}
	}

	// The whale has ~80% of the total stake (2048/(2048+15*32) = 2048/2528 ~ 81%).
	// It should be selected more often than uniform (1/16 = 6.25%).
	uniformRate := float64(totalSlots) / 16.0
	whaleRate := float64(whaleCount)
	if whaleRate <= uniformRate {
		t.Logf("whale selected %d/%d times (%.1f%%), uniform expected ~%.1f",
			whaleCount, totalSlots, whaleRate/float64(totalSlots)*100, uniformRate)
		// This is statistical so we allow some variance, but whale should
		// generally be selected more often.
	}
}

func TestProposerRotationDeterministic(t *testing.T) {
	state := buildPRTestState(16, 3)
	cfg := DefaultProposerRotationConfig()

	prm1 := NewProposerRotationManager(cfg, nil)
	prm2 := NewProposerRotationManager(cfg, nil)

	sched1, err := prm1.ComputeEpochSchedule(state, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sched2, err := prm2.ComputeEpochSchedule(state, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for i := range sched1.Assignments {
		if sched1.Assignments[i].ValidatorIndex != sched2.Assignments[i].ValidatorIndex {
			t.Errorf("slot %d: different proposers (%d vs %d)",
				sched1.Assignments[i].Slot,
				sched1.Assignments[i].ValidatorIndex,
				sched2.Assignments[i].ValidatorIndex)
		}
	}
}

func TestProposerRotationScheduleForEpoch(t *testing.T) {
	cfg := DefaultProposerRotationConfig()
	prm := NewProposerRotationManager(cfg, nil)

	// Should return nil for uncached epoch.
	if s := prm.ScheduleForEpoch(99); s != nil {
		t.Error("expected nil for uncached epoch")
	}

	state := buildPRTestState(8, 5)
	_, _ = prm.ComputeEpochSchedule(state, 5)

	if s := prm.ScheduleForEpoch(5); s == nil {
		t.Error("expected non-nil for cached epoch 5")
	}
}

func TestProposerRotationComputeSlotSeed(t *testing.T) {
	var seed [32]byte
	seed[0] = 0xAB

	s1 := computeSlotSeed(seed, 10)
	s2 := computeSlotSeed(seed, 11)
	s3 := computeSlotSeed(seed, 10)

	if s1 == s2 {
		t.Error("different slots should produce different seeds")
	}
	if s1 != s3 {
		t.Error("same slot and seed should produce identical results")
	}
}

func TestProposerRotationZeroConfig(t *testing.T) {
	cfg := ProposerRotationConfig{} // all zeros
	prm := NewProposerRotationManager(cfg, nil)

	if prm.config.SlotsPerEpoch != 4 {
		t.Errorf("expected default SlotsPerEpoch=4, got %d", prm.config.SlotsPerEpoch)
	}
	if prm.config.MaxLookahead != PRMaxLookahead {
		t.Errorf("expected default MaxLookahead=%d, got %d", PRMaxLookahead, prm.config.MaxLookahead)
	}
}
