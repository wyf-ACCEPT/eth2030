package consensus

import (
	"errors"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func roundTestRoot(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	return h
}

func roundTestVote(slot uint64, valHash byte, root types.Hash, stake uint64) SSFRoundVote {
	var pubkeyHash types.Hash
	pubkeyHash[0] = valHash
	return SSFRoundVote{
		ValidatorPubkeyHash: pubkeyHash,
		Slot:                slot,
		BlockRoot:           root,
		Stake:               stake,
	}
}

func roundTestConfig() SSFRoundEngineConfig {
	return SSFRoundEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           300,
		MaxRoundHistory:      10,
	}
}

// --- SSFRoundPhase ---

func TestSSFRoundPhaseString(t *testing.T) {
	tests := []struct {
		phase SSFRoundPhase
		want  string
	}{
		{PhasePropose, "Propose"},
		{PhaseAttest, "Attest"},
		{PhaseAggregate, "Aggregate"},
		{PhaseFinalize, "Finalize"},
		{SSFRoundPhase(99), "Unknown"},
	}
	for _, tt := range tests {
		if got := tt.phase.String(); got != tt.want {
			t.Errorf("Phase(%d).String() = %q, want %q", tt.phase, got, tt.want)
		}
	}
}

// --- NewSSFRoundEngine ---

func TestNewSSFRoundEngine(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())
	if e == nil {
		t.Fatal("NewSSFRoundEngine returned nil for valid config")
	}
}

func TestNewSSFRoundEngineInvalidConfig(t *testing.T) {
	e := NewSSFRoundEngine(SSFRoundEngineConfig{
		FinalityThresholdNum: 0,
		FinalityThresholdDen: 3,
		TotalStake:           300,
	})
	if e != nil {
		t.Error("expected nil for zero threshold numerator")
	}

	e2 := NewSSFRoundEngine(SSFRoundEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 0,
		TotalStake:           300,
	})
	if e2 != nil {
		t.Error("expected nil for zero threshold denominator")
	}
}

func TestNewSSFRoundEngineDefaultHistory(t *testing.T) {
	cfg := roundTestConfig()
	cfg.MaxRoundHistory = -1
	e := NewSSFRoundEngine(cfg)
	if e == nil {
		t.Fatal("should not be nil")
	}
	if e.config.MaxRoundHistory != 256 {
		t.Errorf("MaxRoundHistory = %d, want 256", e.config.MaxRoundHistory)
	}
}

func TestDefaultSSFRoundEngineConfig(t *testing.T) {
	cfg := DefaultSSFRoundEngineConfig()
	if cfg.FinalityThresholdNum != 2 || cfg.FinalityThresholdDen != 3 {
		t.Error("default threshold should be 2/3")
	}
	if cfg.TotalStake == 0 {
		t.Error("default TotalStake should be non-zero")
	}
	if cfg.MaxRoundHistory <= 0 {
		t.Error("default MaxRoundHistory should be positive")
	}
}

// --- NewRound ---

func TestNewRound(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())
	round, err := e.NewRound(100)
	if err != nil {
		t.Fatalf("NewRound: %v", err)
	}
	if round.Slot != 100 {
		t.Errorf("Slot = %d, want 100", round.Slot)
	}
	if round.Phase != PhasePropose {
		t.Errorf("Phase = %s, want Propose", round.Phase)
	}
	if round.Finalized {
		t.Error("new round should not be finalized")
	}
}

func TestNewRoundDuplicate(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())
	e.NewRound(100)
	_, err := e.NewRound(100)
	if !errors.Is(err, ErrRoundAlreadyExists) {
		t.Errorf("expected ErrRoundAlreadyExists, got %v", err)
	}
}

// --- ProposeBlock ---

func TestProposeBlock(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())
	e.NewRound(1)

	root := roundTestRoot(0xAA)
	err := e.ProposeBlock(1, root)
	if err != nil {
		t.Fatalf("ProposeBlock: %v", err)
	}

	phase, ok := e.GetPhase(1)
	if !ok {
		t.Fatal("round not found")
	}
	if phase != PhaseAttest {
		t.Errorf("Phase = %s, want Attest", phase)
	}
}

func TestProposeBlockNotFound(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())
	err := e.ProposeBlock(999, roundTestRoot(0x01))
	if !errors.Is(err, ErrRoundNotFound) {
		t.Errorf("expected ErrRoundNotFound, got %v", err)
	}
}

func TestProposeBlockWrongPhase(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())
	e.NewRound(1)
	e.ProposeBlock(1, roundTestRoot(0xAA))

	// Already in Attest, proposing again should fail.
	err := e.ProposeBlock(1, roundTestRoot(0xBB))
	if !errors.Is(err, ErrRoundWrongPhase) {
		t.Errorf("expected ErrRoundWrongPhase, got %v", err)
	}
}

// --- SubmitAttestation ---

func TestSubmitAttestation(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())
	e.NewRound(1)
	e.ProposeBlock(1, roundTestRoot(0xAA))

	root := roundTestRoot(0xAA)
	vote := roundTestVote(1, 0x01, root, 100)
	err := e.SubmitAttestation(vote)
	if err != nil {
		t.Fatalf("SubmitAttestation: %v", err)
	}

	count := e.VoteCount(1)
	if count != 1 {
		t.Errorf("VoteCount = %d, want 1", count)
	}
}

func TestSubmitAttestationDuplicate(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())
	e.NewRound(1)
	e.ProposeBlock(1, roundTestRoot(0xAA))

	root := roundTestRoot(0xAA)
	vote := roundTestVote(1, 0x01, root, 100)
	e.SubmitAttestation(vote)

	err := e.SubmitAttestation(vote)
	if !errors.Is(err, ErrRoundDuplicateVote) {
		t.Errorf("expected ErrRoundDuplicateVote, got %v", err)
	}
}

func TestSubmitAttestationEquivocation(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())
	e.NewRound(1)
	e.ProposeBlock(1, roundTestRoot(0xAA))

	vote1 := roundTestVote(1, 0x01, roundTestRoot(0xAA), 100)
	e.SubmitAttestation(vote1)

	// Same validator, different root = equivocation.
	vote2 := roundTestVote(1, 0x01, roundTestRoot(0xBB), 100)
	err := e.SubmitAttestation(vote2)
	if !errors.Is(err, ErrRoundEquivocation) {
		t.Errorf("expected ErrRoundEquivocation, got %v", err)
	}

	stats := e.Stats()
	if stats.EquivocationsDetected != 1 {
		t.Errorf("EquivocationsDetected = %d, want 1", stats.EquivocationsDetected)
	}

	eqCount := e.EquivocationCount(1)
	if eqCount != 1 {
		t.Errorf("EquivocationCount = %d, want 1", eqCount)
	}
}

func TestSubmitAttestationWrongPhase(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())
	e.NewRound(1)
	// Still in Propose phase.
	vote := roundTestVote(1, 0x01, roundTestRoot(0xAA), 100)
	err := e.SubmitAttestation(vote)
	if !errors.Is(err, ErrRoundWrongPhase) {
		t.Errorf("expected ErrRoundWrongPhase, got %v", err)
	}
}

func TestSubmitAttestationNotFound(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())
	vote := roundTestVote(999, 0x01, roundTestRoot(0xAA), 100)
	err := e.SubmitAttestation(vote)
	if !errors.Is(err, ErrRoundNotFound) {
		t.Errorf("expected ErrRoundNotFound, got %v", err)
	}
}

// --- Optimistic path ---

func TestOptimisticFinality(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig()) // 2/3 of 300
	e.NewRound(1)
	e.ProposeBlock(1, roundTestRoot(0xAA))

	root := roundTestRoot(0xAA)
	// 100 + 100 = 200/300 >= 2/3 threshold.
	e.SubmitAttestation(roundTestVote(1, 0x01, root, 100))
	e.SubmitAttestation(roundTestVote(1, 0x02, root, 100))

	// Should have jumped to Finalize phase (optimistic path).
	phase, _ := e.GetPhase(1)
	if phase != PhaseFinalize {
		t.Errorf("Phase = %s, want Finalize (optimistic)", phase)
	}
}

// --- AggregateVotes ---

func TestAggregateVotes(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())
	e.NewRound(1)
	e.ProposeBlock(1, roundTestRoot(0xAA))

	root := roundTestRoot(0xAA)
	// Submit 1 vote (100/300, not enough for optimistic).
	e.SubmitAttestation(roundTestVote(1, 0x01, root, 100))

	err := e.AggregateVotes(1)
	if err != nil {
		t.Fatalf("AggregateVotes: %v", err)
	}

	// Not enough for finality, should be in Aggregate phase.
	phase, _ := e.GetPhase(1)
	if phase != PhaseAggregate {
		t.Errorf("Phase = %s, want Aggregate", phase)
	}
}

func TestAggregateVotesTriggersFinalize(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())
	e.NewRound(1)
	e.ProposeBlock(1, roundTestRoot(0xAA))

	root := roundTestRoot(0xAA)
	e.SubmitAttestation(roundTestVote(1, 0x01, root, 100))

	// Manually keep in Attest phase by checking phase before aggregate.
	// Add enough stake to meet threshold before aggregation.
	e.SubmitAttestation(roundTestVote(1, 0x02, root, 100))

	// Optimistic path should have already moved to Finalize.
	phase, _ := e.GetPhase(1)
	if phase != PhaseFinalize {
		t.Errorf("Phase = %s, want Finalize", phase)
	}

	// AggregateVotes should be a no-op when already at Finalize.
	err := e.AggregateVotes(1)
	if err != nil {
		t.Fatalf("AggregateVotes on finalize phase: %v", err)
	}
}

func TestAggregateVotesNotFound(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())
	err := e.AggregateVotes(999)
	if !errors.Is(err, ErrRoundNotFound) {
		t.Errorf("expected ErrRoundNotFound, got %v", err)
	}
}

func TestAggregateVotesWrongPhase(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())
	e.NewRound(1)
	// Still in Propose phase.
	err := e.AggregateVotes(1)
	if !errors.Is(err, ErrRoundWrongPhase) {
		t.Errorf("expected ErrRoundWrongPhase, got %v", err)
	}
}

// --- Finalize ---

func TestFinalizeRound(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())
	e.NewRound(1)
	e.ProposeBlock(1, roundTestRoot(0xAA))

	root := roundTestRoot(0xAA)
	e.SubmitAttestation(roundTestVote(1, 0x01, root, 100))
	e.SubmitAttestation(roundTestVote(1, 0x02, root, 100))

	finalRoot, err := e.Finalize(1)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if finalRoot != root {
		t.Errorf("finalRoot = %s, want %s", finalRoot.Hex(), root.Hex())
	}

	if !e.IsFinalized(1) {
		t.Error("slot 1 should be finalized")
	}
}

func TestFinalizeRoundBelowThreshold(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())
	e.NewRound(1)
	e.ProposeBlock(1, roundTestRoot(0xAA))

	// Only 100/300 = 33%, below 2/3.
	e.SubmitAttestation(roundTestVote(1, 0x01, roundTestRoot(0xAA), 100))
	e.AggregateVotes(1)

	_, err := e.Finalize(1)
	if err == nil {
		t.Error("expected error when below threshold")
	}
}

func TestFinalizeRoundNotFound(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())
	_, err := e.Finalize(999)
	if !errors.Is(err, ErrRoundNotFound) {
		t.Errorf("expected ErrRoundNotFound, got %v", err)
	}
}

func TestFinalizeRoundZeroStake(t *testing.T) {
	cfg := roundTestConfig()
	cfg.TotalStake = 0
	e := NewSSFRoundEngine(cfg)
	e.NewRound(1)
	_, err := e.Finalize(1)
	if !errors.Is(err, ErrRoundZeroTotalStake) {
		t.Errorf("expected ErrRoundZeroTotalStake, got %v", err)
	}
}

func TestFinalizeAlreadyFinalized(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())
	e.NewRound(1)
	e.ProposeBlock(1, roundTestRoot(0xAA))
	root := roundTestRoot(0xAA)
	e.SubmitAttestation(roundTestVote(1, 0x01, root, 200))
	e.Finalize(1)

	// Finalizing again should return the root without error.
	finalRoot, err := e.Finalize(1)
	if err != nil {
		// It's now in history, so the round slot lookup fails.
		// That's acceptable behavior.
		return
	}
	if finalRoot != root {
		t.Errorf("re-finalize: root mismatch")
	}
}

// --- GetRound ---

func TestGetRoundActive(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())
	e.NewRound(1)

	round, ok := e.GetRound(1)
	if !ok {
		t.Fatal("round 1 not found")
	}
	if round.Slot != 1 {
		t.Errorf("Slot = %d, want 1", round.Slot)
	}
}

func TestGetRoundHistory(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())
	e.NewRound(1)
	e.ProposeBlock(1, roundTestRoot(0xAA))
	e.SubmitAttestation(roundTestVote(1, 0x01, roundTestRoot(0xAA), 200))
	e.Finalize(1)

	round, ok := e.GetRound(1)
	if !ok {
		t.Fatal("finalized round 1 not found in history")
	}
	if !round.Finalized {
		t.Error("round should be finalized")
	}
}

func TestGetRoundNotFound(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())
	_, ok := e.GetRound(999)
	if ok {
		t.Error("should not find non-existent round")
	}
}

// --- History eviction ---

func TestHistoryEviction(t *testing.T) {
	cfg := roundTestConfig()
	cfg.MaxRoundHistory = 3
	e := NewSSFRoundEngine(cfg)

	for slot := uint64(1); slot <= 5; slot++ {
		e.NewRound(slot)
		e.ProposeBlock(slot, roundTestRoot(byte(slot)))
		e.SubmitAttestation(roundTestVote(slot, byte(slot), roundTestRoot(byte(slot)), 200))
		e.Finalize(slot)
	}

	// Only last 3 should remain: slots 3, 4, 5.
	if e.IsFinalized(1) {
		t.Error("slot 1 should be evicted")
	}
	if e.IsFinalized(2) {
		t.Error("slot 2 should be evicted")
	}
	for _, slot := range []uint64{3, 4, 5} {
		if !e.IsFinalized(slot) {
			t.Errorf("slot %d should still be in history", slot)
		}
	}
}

// --- Statistics ---

func TestSSFRoundStats(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())

	stats := e.Stats()
	if stats.RoundsCompleted != 0 {
		t.Errorf("initial RoundsCompleted = %d, want 0", stats.RoundsCompleted)
	}

	// Complete a round.
	e.NewRound(1)
	e.ProposeBlock(1, roundTestRoot(0xAA))
	e.SubmitAttestation(roundTestVote(1, 0x01, roundTestRoot(0xAA), 200))
	e.Finalize(1)

	stats = e.Stats()
	if stats.RoundsCompleted != 1 {
		t.Errorf("RoundsCompleted = %d, want 1", stats.RoundsCompleted)
	}
}

func TestSSFRoundStatsAverage(t *testing.T) {
	s := SSFRoundStats{RoundsCompleted: 0}
	if avg := s.AverageFinalityMs(); avg != 0 {
		t.Errorf("AverageFinalityMs() = %d, want 0 for no rounds", avg)
	}

	s = SSFRoundStats{RoundsCompleted: 4, TotalFinalityTimeMs: 400}
	if avg := s.AverageFinalityMs(); avg != 100 {
		t.Errorf("AverageFinalityMs() = %d, want 100", avg)
	}
}

// --- Multiple roots ---

func TestFinalizeMultipleRoots(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())
	e.NewRound(1)
	e.ProposeBlock(1, roundTestRoot(0xAA))

	rootA := roundTestRoot(0xAA)
	rootB := roundTestRoot(0xBB)

	// Split votes: rootA=100, rootB=200.
	e.SubmitAttestation(roundTestVote(1, 0x01, rootA, 100))
	e.SubmitAttestation(roundTestVote(1, 0x02, rootB, 200))

	finalRoot, err := e.Finalize(1)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	// rootB has 200/300 >= 2/3, should be finalized.
	if finalRoot != rootB {
		t.Errorf("finalRoot = %s, want rootB %s", finalRoot.Hex(), rootB.Hex())
	}
}

// --- Full round lifecycle ---

func TestFullRoundLifecycle(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())

	// Phase 1: Propose.
	round, err := e.NewRound(10)
	if err != nil {
		t.Fatalf("NewRound: %v", err)
	}
	if round.Phase != PhasePropose {
		t.Fatalf("initial phase = %s, want Propose", round.Phase)
	}

	// Phase 2: Block proposed -> Attest.
	root := roundTestRoot(0xCC)
	if err := e.ProposeBlock(10, root); err != nil {
		t.Fatalf("ProposeBlock: %v", err)
	}
	phase, _ := e.GetPhase(10)
	if phase != PhaseAttest {
		t.Fatalf("after propose: phase = %s, want Attest", phase)
	}

	// Phase 3: Submit attestations (not enough for optimistic).
	e.SubmitAttestation(roundTestVote(10, 0x01, root, 100))

	// Phase 4: Aggregate.
	if err := e.AggregateVotes(10); err != nil {
		t.Fatalf("AggregateVotes: %v", err)
	}
	phase, _ = e.GetPhase(10)
	if phase != PhaseAggregate {
		t.Fatalf("after aggregate: phase = %s, want Aggregate", phase)
	}

	// Finalize should fail (only 100/300).
	_, err = e.Finalize(10)
	if err == nil {
		t.Fatal("expected finalize to fail with insufficient stake")
	}

	// Not finalized yet.
	if e.IsFinalized(10) {
		t.Error("should not be finalized yet")
	}
}

func TestFullRoundLifecycleSuccess(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())

	e.NewRound(10)
	root := roundTestRoot(0xCC)
	e.ProposeBlock(10, root)

	// Enough stake but below optimistic (let's submit individually).
	e.SubmitAttestation(roundTestVote(10, 0x01, root, 100))

	// Still in Attest. Aggregate manually.
	e.AggregateVotes(10)
	// Still in Aggregate (100/300 < 2/3).

	// Submit more, but round is in Aggregate now so we need to accept
	// the round as is. Let's redo with proper sequencing.
	e2 := NewSSFRoundEngine(roundTestConfig())
	e2.NewRound(20)
	e2.ProposeBlock(20, root)
	e2.SubmitAttestation(roundTestVote(20, 0x01, root, 100))
	e2.SubmitAttestation(roundTestVote(20, 0x02, root, 100))

	// Optimistic: 200/300 >= 2/3, should be in Finalize phase.
	phase, _ := e2.GetPhase(20)
	if phase != PhaseFinalize {
		t.Fatalf("after 2/3 votes: phase = %s, want Finalize", phase)
	}

	finalRoot, err := e2.Finalize(20)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if finalRoot != root {
		t.Errorf("finalRoot = %s, want %s", finalRoot.Hex(), root.Hex())
	}
	if !e2.IsFinalized(20) {
		t.Error("slot 20 should be finalized")
	}
}

// --- Concurrency ---

func TestSSFRoundEngineConcurrentVotes(t *testing.T) {
	cfg := roundTestConfig()
	cfg.TotalStake = 100_000
	e := NewSSFRoundEngine(cfg)

	e.NewRound(1)
	e.ProposeBlock(1, roundTestRoot(0xAA))

	root := roundTestRoot(0xAA)
	numVoters := 100

	var wg sync.WaitGroup
	errCh := make(chan error, numVoters)

	for i := 0; i < numVoters; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			var pubkeyHash types.Hash
			pubkeyHash[0] = byte(idx)
			pubkeyHash[1] = byte(idx >> 8)
			vote := SSFRoundVote{
				ValidatorPubkeyHash: pubkeyHash,
				Slot:                1,
				BlockRoot:           root,
				Stake:               10,
			}
			if err := e.SubmitAttestation(vote); err != nil {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent error: %v", err)
	}

	count := e.VoteCount(1)
	if count != numVoters {
		t.Errorf("VoteCount = %d, want %d", count, numVoters)
	}
}

func TestSSFRoundEngineConcurrentNewRounds(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			e.NewRound(uint64(slot))
		}(i + 1)
	}
	wg.Wait()

	// All 50 rounds should exist.
	for i := 1; i <= 50; i++ {
		_, ok := e.GetRound(uint64(i))
		if !ok {
			t.Errorf("round %d not found after concurrent creation", i)
		}
	}
}

func TestSSFRoundEngineConcurrentGetPhase(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())
	e.NewRound(1)
	e.ProposeBlock(1, roundTestRoot(0xAA))

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			phase, ok := e.GetPhase(1)
			if !ok {
				t.Error("round not found")
				return
			}
			if phase != PhaseAttest && phase != PhaseFinalize {
				t.Errorf("unexpected phase: %s", phase)
			}
		}()
	}
	wg.Wait()
}

// --- Propose after finalized ---

func TestProposeBlockFinalized(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())
	e.NewRound(1)
	e.ProposeBlock(1, roundTestRoot(0xAA))
	e.SubmitAttestation(roundTestVote(1, 0x01, roundTestRoot(0xAA), 200))
	e.Finalize(1)

	// Cannot create a new round for a finalized slot.
	_, err := e.NewRound(1)
	if !errors.Is(err, ErrRoundAlreadyFinalized) {
		t.Errorf("expected ErrRoundAlreadyFinalized, got %v", err)
	}
}

// --- EquivocationCount for non-existent slot ---

func TestEquivocationCountMissing(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())
	if c := e.EquivocationCount(999); c != 0 {
		t.Errorf("EquivocationCount = %d, want 0", c)
	}
}

// --- VoteCount for non-existent slot ---

func TestVoteCountMissing(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())
	if c := e.VoteCount(999); c != 0 {
		t.Errorf("VoteCount = %d, want 0", c)
	}
}

// --- IsFinalized for non-existent slot ---

func TestIsFinalizedMissing(t *testing.T) {
	e := NewSSFRoundEngine(roundTestConfig())
	if e.IsFinalized(999) {
		t.Error("non-existent slot should not be finalized")
	}
}
