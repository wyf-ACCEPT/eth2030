package consensus

import (
	"errors"
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func testRoot(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	return h
}

func testWeights(n int, stakeEach uint64) map[uint64]uint64 {
	w := make(map[uint64]uint64, n)
	for i := 0; i < n; i++ {
		w[uint64(i)] = stakeEach
	}
	return w
}

// --- NewSSFEngine ---

func TestNewSSFEngine_Default(t *testing.T) {
	cfg := DefaultSSFEngineConfig()
	e := NewSSFEngine(cfg)
	if e == nil {
		t.Fatal("NewSSFEngine returned nil for default config")
	}
}

func TestNewSSFEngine_InvalidConfig(t *testing.T) {
	cfg := SSFEngineConfig{
		FinalityThresholdNum: 0,
		FinalityThresholdDen: 3,
		TotalStake:           100,
	}
	e := NewSSFEngine(cfg)
	if e != nil {
		t.Error("expected nil for zero threshold numerator")
	}

	cfg2 := SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 0,
		TotalStake:           100,
	}
	e2 := NewSSFEngine(cfg2)
	if e2 != nil {
		t.Error("expected nil for zero threshold denominator")
	}
}

func TestNewSSFEngine_DefaultMaxSlotHistory(t *testing.T) {
	cfg := SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		MaxSlotHistory:       -1,
		TotalStake:           300,
	}
	e := NewSSFEngine(cfg)
	if e == nil {
		t.Fatal("NewSSFEngine returned nil")
	}
	if e.config.MaxSlotHistory != DefaultMaxSlotHistory {
		t.Errorf("MaxSlotHistory = %d, want %d", e.config.MaxSlotHistory, DefaultMaxSlotHistory)
	}
}

// --- SetValidatorWeights ---

func TestSetValidatorWeights(t *testing.T) {
	e := NewSSFEngine(SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           300,
		MaxSlotHistory:       10,
	})

	weights := map[uint64]uint64{0: 100, 1: 100, 2: 100}
	e.SetValidatorWeights(weights)

	// Modify original map - engine should not be affected.
	weights[3] = 999

	att := &SSFAttestation{Slot: 1, ValidatorIndex: 3, TargetRoot: testRoot(0xAA)}
	err := e.ProcessAttestation(att)
	if !errors.Is(err, ErrSSFEngineUnknownValidator) {
		t.Errorf("expected ErrSSFEngineUnknownValidator, got %v", err)
	}
}

// --- ProcessAttestation ---

func TestProcessAttestation_Basic(t *testing.T) {
	e := NewSSFEngine(SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           300,
		MaxSlotHistory:       10,
	})
	e.SetValidatorWeights(testWeights(3, 100))

	att := &SSFAttestation{
		Slot:           1,
		ValidatorIndex: 0,
		SourceEpoch:    0,
		TargetRoot:     testRoot(0xAB),
	}
	if err := e.ProcessAttestation(att); err != nil {
		t.Fatalf("ProcessAttestation failed: %v", err)
	}

	status := e.GetVotingStatus(1)
	if status.TotalVotes != 1 {
		t.Errorf("TotalVotes = %d, want 1", status.TotalVotes)
	}
	if status.TotalStake != 100 {
		t.Errorf("TotalStake = %d, want 100", status.TotalStake)
	}
}

func TestProcessAttestation_Nil(t *testing.T) {
	e := NewSSFEngine(DefaultSSFEngineConfig())
	err := e.ProcessAttestation(nil)
	if !errors.Is(err, ErrSSFEngineNilAttestation) {
		t.Errorf("expected ErrSSFEngineNilAttestation, got %v", err)
	}
}

func TestProcessAttestation_DuplicateVote(t *testing.T) {
	e := NewSSFEngine(SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           300,
		MaxSlotHistory:       10,
	})
	e.SetValidatorWeights(testWeights(3, 100))

	att := &SSFAttestation{Slot: 1, ValidatorIndex: 0, TargetRoot: testRoot(0x01)}
	e.ProcessAttestation(att)

	err := e.ProcessAttestation(att)
	if !errors.Is(err, ErrSSFEngineDuplicateVote) {
		t.Errorf("expected ErrSSFEngineDuplicateVote, got %v", err)
	}

	// Different root, same validator, same slot -> still duplicate.
	att2 := &SSFAttestation{Slot: 1, ValidatorIndex: 0, TargetRoot: testRoot(0x02)}
	err = e.ProcessAttestation(att2)
	if !errors.Is(err, ErrSSFEngineDuplicateVote) {
		t.Errorf("expected ErrSSFEngineDuplicateVote for equivocation, got %v", err)
	}
}

func TestProcessAttestation_UnknownValidator(t *testing.T) {
	e := NewSSFEngine(SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           300,
		MaxSlotHistory:       10,
	})
	e.SetValidatorWeights(testWeights(3, 100))

	att := &SSFAttestation{Slot: 1, ValidatorIndex: 99, TargetRoot: testRoot(0x01)}
	err := e.ProcessAttestation(att)
	if !errors.Is(err, ErrSSFEngineUnknownValidator) {
		t.Errorf("expected ErrSSFEngineUnknownValidator, got %v", err)
	}
}

func TestProcessAttestation_FinalizedSlot(t *testing.T) {
	e := NewSSFEngine(SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           300,
		MaxSlotHistory:       10,
	})
	e.SetValidatorWeights(testWeights(3, 100))

	e.Finalize(5, testRoot(0xCC))

	att := &SSFAttestation{Slot: 5, ValidatorIndex: 0, TargetRoot: testRoot(0xCC)}
	err := e.ProcessAttestation(att)
	if !errors.Is(err, ErrSSFEngineSlotFinalized) {
		t.Errorf("expected ErrSSFEngineSlotFinalized, got %v", err)
	}
}

// --- CheckFinality ---

func TestCheckFinality_NotMet(t *testing.T) {
	e := NewSSFEngine(SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           300,
		MaxSlotHistory:       10,
	})
	e.SetValidatorWeights(testWeights(3, 100))

	// One vote = 100/300 = 33%, below 2/3.
	e.ProcessAttestation(&SSFAttestation{Slot: 1, ValidatorIndex: 0, TargetRoot: testRoot(0xAA)})

	result, err := e.CheckFinality(1)
	if err != nil {
		t.Fatalf("CheckFinality error: %v", err)
	}
	if result.IsFinalized {
		t.Error("should not be finalized with 33% stake")
	}
	if result.VoteCount != 1 {
		t.Errorf("VoteCount = %d, want 1", result.VoteCount)
	}
	if result.StakeWeight != 100 {
		t.Errorf("StakeWeight = %d, want 100", result.StakeWeight)
	}
}

func TestCheckFinality_Met(t *testing.T) {
	e := NewSSFEngine(SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           300,
		MaxSlotHistory:       10,
	})
	e.SetValidatorWeights(testWeights(3, 100))

	root := testRoot(0xBB)
	e.ProcessAttestation(&SSFAttestation{Slot: 1, ValidatorIndex: 0, TargetRoot: root})
	e.ProcessAttestation(&SSFAttestation{Slot: 1, ValidatorIndex: 1, TargetRoot: root})

	result, err := e.CheckFinality(1)
	if err != nil {
		t.Fatalf("CheckFinality error: %v", err)
	}
	if !result.IsFinalized {
		t.Error("should be finalized with 200/300 (66.7%) stake")
	}
	if result.VoteCount != 2 {
		t.Errorf("VoteCount = %d, want 2", result.VoteCount)
	}
}

func TestCheckFinality_ExactThreshold(t *testing.T) {
	e := NewSSFEngine(SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           3,
		MaxSlotHistory:       10,
	})
	e.SetValidatorWeights(map[uint64]uint64{0: 2})

	e.ProcessAttestation(&SSFAttestation{Slot: 1, ValidatorIndex: 0, TargetRoot: testRoot(0x01)})

	result, _ := e.CheckFinality(1)
	if !result.IsFinalized {
		t.Error("exactly 2/3 stake should meet threshold")
	}
}

func TestCheckFinality_BelowExact(t *testing.T) {
	e := NewSSFEngine(SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           3,
		MaxSlotHistory:       10,
	})
	e.SetValidatorWeights(map[uint64]uint64{0: 1})

	e.ProcessAttestation(&SSFAttestation{Slot: 1, ValidatorIndex: 0, TargetRoot: testRoot(0x01)})

	result, _ := e.CheckFinality(1)
	if result.IsFinalized {
		t.Error("1/3 stake should not meet 2/3 threshold")
	}
}

func TestCheckFinality_ZeroStake(t *testing.T) {
	e := NewSSFEngine(SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           0,
		MaxSlotHistory:       10,
	})

	_, err := e.CheckFinality(1)
	if !errors.Is(err, ErrSSFEngineZeroStake) {
		t.Errorf("expected ErrSSFEngineZeroStake, got %v", err)
	}
}

func TestCheckFinality_EmptySlot(t *testing.T) {
	e := NewSSFEngine(SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           300,
		MaxSlotHistory:       10,
	})

	result, err := e.CheckFinality(999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsFinalized {
		t.Error("empty slot should not be finalized")
	}
	if result.VoteCount != 0 {
		t.Errorf("VoteCount = %d, want 0", result.VoteCount)
	}
}

func TestCheckFinality_MultipleRoots(t *testing.T) {
	e := NewSSFEngine(SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           300,
		MaxSlotHistory:       10,
	})
	e.SetValidatorWeights(testWeights(3, 100))

	rootA := testRoot(0xA0)
	rootB := testRoot(0xB0)

	// Validators split between two roots.
	e.ProcessAttestation(&SSFAttestation{Slot: 1, ValidatorIndex: 0, TargetRoot: rootA})
	e.ProcessAttestation(&SSFAttestation{Slot: 1, ValidatorIndex: 1, TargetRoot: rootB})
	e.ProcessAttestation(&SSFAttestation{Slot: 1, ValidatorIndex: 2, TargetRoot: rootA})

	// rootA: 200/300 -> meets 2/3.
	result, _ := e.CheckFinality(1)
	if !result.IsFinalized {
		t.Error("should finalize because rootA has 200/300")
	}
}

func TestCheckFinality_SplitNoFinality(t *testing.T) {
	e := NewSSFEngine(SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           300,
		MaxSlotHistory:       10,
	})
	e.SetValidatorWeights(testWeights(3, 100))

	// Each validator votes for a different root.
	e.ProcessAttestation(&SSFAttestation{Slot: 1, ValidatorIndex: 0, TargetRoot: testRoot(0xA0)})
	e.ProcessAttestation(&SSFAttestation{Slot: 1, ValidatorIndex: 1, TargetRoot: testRoot(0xB0)})
	e.ProcessAttestation(&SSFAttestation{Slot: 1, ValidatorIndex: 2, TargetRoot: testRoot(0xC0)})

	result, _ := e.CheckFinality(1)
	if result.IsFinalized {
		t.Error("should not finalize with 3-way split (100/300 each)")
	}
}

func TestCheckFinality_FinalizedHistory(t *testing.T) {
	e := NewSSFEngine(SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           300,
		MaxSlotHistory:       10,
	})
	e.SetValidatorWeights(testWeights(3, 100))

	root := testRoot(0xDD)
	e.ProcessAttestation(&SSFAttestation{Slot: 5, ValidatorIndex: 0, TargetRoot: root})
	e.ProcessAttestation(&SSFAttestation{Slot: 5, ValidatorIndex: 1, TargetRoot: root})
	e.Finalize(5, root)

	result, err := e.CheckFinality(5)
	if err != nil {
		t.Fatalf("CheckFinality error: %v", err)
	}
	if !result.IsFinalized {
		t.Error("finalized slot should report as finalized")
	}
}

// --- GetVotingStatus ---

func TestGetVotingStatus_Active(t *testing.T) {
	e := NewSSFEngine(SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           300,
		MaxSlotHistory:       10,
	})
	e.SetValidatorWeights(testWeights(3, 100))

	e.ProcessAttestation(&SSFAttestation{Slot: 1, ValidatorIndex: 0, TargetRoot: testRoot(0x01)})
	e.ProcessAttestation(&SSFAttestation{Slot: 1, ValidatorIndex: 1, TargetRoot: testRoot(0x01)})

	status := e.GetVotingStatus(1)
	if status.TotalVotes != 2 {
		t.Errorf("TotalVotes = %d, want 2", status.TotalVotes)
	}
	if status.TotalStake != 200 {
		t.Errorf("TotalStake = %d, want 200", status.TotalStake)
	}
	if status.RequiredStake != 200 {
		t.Errorf("RequiredStake = %d, want 200", status.RequiredStake)
	}
	// 200/300 = 66.7%.
	if status.Participation < 66.0 || status.Participation > 67.0 {
		t.Errorf("Participation = %f, expected ~66.7", status.Participation)
	}
}

func TestGetVotingStatus_Empty(t *testing.T) {
	e := NewSSFEngine(SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           300,
		MaxSlotHistory:       10,
	})

	status := e.GetVotingStatus(999)
	if status.TotalVotes != 0 {
		t.Errorf("TotalVotes = %d, want 0", status.TotalVotes)
	}
	if status.Participation != 0 {
		t.Errorf("Participation = %f, want 0", status.Participation)
	}
}

func TestGetVotingStatus_Finalized(t *testing.T) {
	e := NewSSFEngine(SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           300,
		MaxSlotHistory:       10,
	})
	e.SetValidatorWeights(testWeights(3, 100))

	root := testRoot(0xEE)
	e.ProcessAttestation(&SSFAttestation{Slot: 5, ValidatorIndex: 0, TargetRoot: root})
	e.ProcessAttestation(&SSFAttestation{Slot: 5, ValidatorIndex: 1, TargetRoot: root})
	e.Finalize(5, root)

	status := e.GetVotingStatus(5)
	if status.TotalVotes != 2 {
		t.Errorf("TotalVotes = %d, want 2", status.TotalVotes)
	}
	if status.TotalStake != 200 {
		t.Errorf("TotalStake = %d, want 200", status.TotalStake)
	}
}

// --- Finalize and History ---

func TestFinalize_MovesToHistory(t *testing.T) {
	e := NewSSFEngine(SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           300,
		MaxSlotHistory:       10,
	})
	e.SetValidatorWeights(testWeights(3, 100))

	root := testRoot(0xAA)
	e.ProcessAttestation(&SSFAttestation{Slot: 1, ValidatorIndex: 0, TargetRoot: root})
	e.Finalize(1, root)

	if !e.IsFinalized(1) {
		t.Error("slot 1 should be finalized")
	}

	history := e.SlotHistory()
	if len(history) != 1 || history[0] != 1 {
		t.Errorf("SlotHistory = %v, want [1]", history)
	}
}

func TestFinalize_HistoryEviction(t *testing.T) {
	maxHistory := 5
	e := NewSSFEngine(SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           300,
		MaxSlotHistory:       maxHistory,
	})

	// Finalize more slots than max history.
	for i := uint64(1); i <= 10; i++ {
		e.Finalize(i, testRoot(byte(i)))
	}

	history := e.SlotHistory()
	if len(history) != maxHistory {
		t.Fatalf("history length = %d, want %d", len(history), maxHistory)
	}

	// Oldest should be evicted: only slots 6-10 remain.
	for i, slot := range history {
		expected := uint64(i + 6)
		if slot != expected {
			t.Errorf("history[%d] = %d, want %d", i, slot, expected)
		}
	}

	// Evicted slots should not be found.
	for i := uint64(1); i <= 5; i++ {
		if e.IsFinalized(i) {
			t.Errorf("slot %d should be evicted from history", i)
		}
	}

	// Remaining slots should be found.
	for i := uint64(6); i <= 10; i++ {
		if !e.IsFinalized(i) {
			t.Errorf("slot %d should still be in history", i)
		}
	}
}

func TestFinalize_NoPreexistingRecord(t *testing.T) {
	e := NewSSFEngine(SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           300,
		MaxSlotHistory:       10,
	})

	// Finalize a slot that has no attestations.
	e.Finalize(42, testRoot(0xFF))

	if !e.IsFinalized(42) {
		t.Error("slot 42 should be finalized even without attestations")
	}
}

// --- Concurrent access ---

func TestSSFEngine_ConcurrentAttestations(t *testing.T) {
	e := NewSSFEngine(SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           100_000,
		MaxSlotHistory:       10,
	})

	numValidators := 500
	weights := make(map[uint64]uint64, numValidators)
	for i := 0; i < numValidators; i++ {
		weights[uint64(i)] = 10
	}
	e.SetValidatorWeights(weights)

	root := testRoot(0xFF)
	var wg sync.WaitGroup
	errCh := make(chan error, numValidators)

	for i := 0; i < numValidators; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			att := &SSFAttestation{
				Slot:           100,
				ValidatorIndex: uint64(idx),
				TargetRoot:     root,
			}
			if err := e.ProcessAttestation(att); err != nil {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("unexpected concurrent error: %v", err)
	}

	status := e.GetVotingStatus(100)
	if status.TotalVotes != numValidators {
		t.Errorf("TotalVotes = %d, want %d", status.TotalVotes, numValidators)
	}
}

func TestSSFEngine_ConcurrentCheckFinality(t *testing.T) {
	e := NewSSFEngine(SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           300,
		MaxSlotHistory:       10,
	})
	e.SetValidatorWeights(testWeights(3, 100))

	root := testRoot(0xDD)
	e.ProcessAttestation(&SSFAttestation{Slot: 1, ValidatorIndex: 0, TargetRoot: root})
	e.ProcessAttestation(&SSFAttestation{Slot: 1, ValidatorIndex: 1, TargetRoot: root})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := e.CheckFinality(1)
			if err != nil {
				t.Errorf("concurrent CheckFinality error: %v", err)
				return
			}
			if !result.IsFinalized {
				t.Error("concurrent CheckFinality: should be finalized")
			}
		}()
	}
	wg.Wait()
}

// --- Threshold and FinalityResult fields ---

func TestFinalityResult_Threshold(t *testing.T) {
	e := NewSSFEngine(SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           300,
		MaxSlotHistory:       10,
	})

	result, _ := e.CheckFinality(1)
	// ceil(300 * 2 / 3) = 200.
	if result.Threshold != 200 {
		t.Errorf("Threshold = %d, want 200", result.Threshold)
	}
}

func TestFinalityResult_ThresholdRoundsUp(t *testing.T) {
	e := NewSSFEngine(SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           301,
		MaxSlotHistory:       10,
	})

	result, _ := e.CheckFinality(1)
	// ceil(301 * 2 / 3) = ceil(602/3) = ceil(200.67) = 201.
	if result.Threshold != 201 {
		t.Errorf("Threshold = %d, want 201", result.Threshold)
	}
}

// --- Multiple slots ---

func TestSSFEngine_MultipleSlots(t *testing.T) {
	e := NewSSFEngine(SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           300,
		MaxSlotHistory:       10,
	})
	e.SetValidatorWeights(testWeights(3, 100))

	// Slot 1: 1 vote (not finalized).
	e.ProcessAttestation(&SSFAttestation{Slot: 1, ValidatorIndex: 0, TargetRoot: testRoot(0x01)})

	// Slot 2: 2 votes (finalized).
	e.ProcessAttestation(&SSFAttestation{Slot: 2, ValidatorIndex: 0, TargetRoot: testRoot(0x02)})
	e.ProcessAttestation(&SSFAttestation{Slot: 2, ValidatorIndex: 1, TargetRoot: testRoot(0x02)})

	r1, _ := e.CheckFinality(1)
	r2, _ := e.CheckFinality(2)

	if r1.IsFinalized {
		t.Error("slot 1 should not be finalized")
	}
	if !r2.IsFinalized {
		t.Error("slot 2 should be finalized")
	}
}

// --- IsFinalized ---

func TestIsFinalized_NotFinalized(t *testing.T) {
	e := NewSSFEngine(SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           300,
		MaxSlotHistory:       10,
	})

	if e.IsFinalized(1) {
		t.Error("slot 1 should not be finalized initially")
	}
}

// --- VotingStatus participation calculation ---

func TestVotingStatus_FullParticipation(t *testing.T) {
	e := NewSSFEngine(SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           300,
		MaxSlotHistory:       10,
	})
	e.SetValidatorWeights(testWeights(3, 100))

	root := testRoot(0x01)
	e.ProcessAttestation(&SSFAttestation{Slot: 1, ValidatorIndex: 0, TargetRoot: root})
	e.ProcessAttestation(&SSFAttestation{Slot: 1, ValidatorIndex: 1, TargetRoot: root})
	e.ProcessAttestation(&SSFAttestation{Slot: 1, ValidatorIndex: 2, TargetRoot: root})

	status := e.GetVotingStatus(1)
	if status.Participation != 100.0 {
		t.Errorf("Participation = %f, want 100.0", status.Participation)
	}
}

func TestVotingStatus_ZeroTotalStake(t *testing.T) {
	e := NewSSFEngine(SSFEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           0,
		MaxSlotHistory:       10,
	})

	status := e.GetVotingStatus(1)
	if status.Participation != 0 {
		t.Errorf("Participation = %f, want 0 when total stake is 0", status.Participation)
	}
}
