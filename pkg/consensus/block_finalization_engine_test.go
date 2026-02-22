package consensus

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestBlockFinalizationEngineNew(t *testing.T) {
	// Default config.
	e := NewBlockFinalizationEngine(nil)
	if e == nil {
		t.Fatal("expected non-nil engine")
	}
	if e.config.TargetLatencyMs != 500 {
		t.Errorf("expected default target latency 500, got %d", e.config.TargetLatencyMs)
	}
	if e.config.MinParticipation != 0.667 {
		t.Errorf("expected default min participation 0.667, got %f", e.config.MinParticipation)
	}
	if e.ValidatorCount() != 0 {
		t.Errorf("expected 0 validators, got %d", e.ValidatorCount())
	}

	// Custom config.
	cfg := &FinalizationConfig{
		TargetLatencyMs:     200,
		VotingWindowMs:      150,
		AggregationWindowMs: 50,
		MinParticipation:    0.75,
		MaxValidators:       1000,
	}
	e2 := NewBlockFinalizationEngine(cfg)
	if e2.config.TargetLatencyMs != 200 {
		t.Errorf("expected target latency 200, got %d", e2.config.TargetLatencyMs)
	}
	if e2.config.MinParticipation != 0.75 {
		t.Errorf("expected min participation 0.75, got %f", e2.config.MinParticipation)
	}
}

func TestBlockFinalizationEngineProposeBlock(t *testing.T) {
	e := NewBlockFinalizationEngine(nil)

	block := &FinalizationBlock{
		Slot:           1,
		ProposerIndex:  0,
		StateRoot:      types.Hash{0x01},
		ParentRoot:     types.Hash{0x02},
		Body:           []byte("test body"),
		ExecutionValid: true,
	}

	if err := e.ProposeBlock(block); err != nil {
		t.Fatalf("ProposeBlock failed: %v", err)
	}

	// Nil block should error.
	if err := e.ProposeBlock(nil); err != ErrBFENilBlock {
		t.Errorf("expected ErrBFENilBlock, got %v", err)
	}

	// Non-execution-valid block should error.
	badBlock := &FinalizationBlock{
		Slot:           2,
		ExecutionValid: false,
	}
	if err := e.ProposeBlock(badBlock); err != ErrBFEBlockNotExecutionValid {
		t.Errorf("expected ErrBFEBlockNotExecutionValid, got %v", err)
	}
}

func TestBlockFinalizationEngineReceiveVote(t *testing.T) {
	e := NewBlockFinalizationEngine(nil)
	e.RegisterValidator(0, []byte{0xAA}, 100)
	e.RegisterValidator(1, []byte{0xBB}, 100)

	block := &FinalizationBlock{
		Slot:           1,
		ProposerIndex:  0,
		StateRoot:      types.Hash{0x01},
		ExecutionValid: true,
	}
	if err := e.ProposeBlock(block); err != nil {
		t.Fatalf("ProposeBlock failed: %v", err)
	}

	root := computeBlockRoot(block)
	vote := &BlockFinalityVote{
		Slot:           1,
		BlockRoot:      root,
		ValidatorIndex: 0,
		Signature:      []byte{0x01, 0x02},
		Timestamp:      1000,
	}

	if err := e.ReceiveVote(vote); err != nil {
		t.Fatalf("ReceiveVote failed: %v", err)
	}

	// Nil vote should error.
	if err := e.ReceiveVote(nil); err != ErrBFENilVote {
		t.Errorf("expected ErrBFENilVote, got %v", err)
	}

	// Vote for wrong slot should error.
	wrongSlot := &BlockFinalityVote{
		Slot:           99,
		ValidatorIndex: 0,
	}
	if err := e.ReceiveVote(wrongSlot); err != ErrBFEWrongSlot {
		t.Errorf("expected ErrBFEWrongSlot, got %v", err)
	}
}

func TestBlockFinalizationEngineReachFinality(t *testing.T) {
	cfg := &FinalizationConfig{
		TargetLatencyMs:  500,
		MinParticipation: 0.667,
		MaxValidators:    100,
	}
	e := NewBlockFinalizationEngine(cfg)

	// Register 3 validators with equal stake.
	for i := uint64(0); i < 3; i++ {
		e.RegisterValidator(i, []byte{byte(i)}, 100)
	}

	block := &FinalizationBlock{
		Slot:           1,
		ProposerIndex:  0,
		StateRoot:      types.Hash{0x01},
		ExecutionValid: true,
	}
	if err := e.ProposeBlock(block); err != nil {
		t.Fatalf("ProposeBlock: %v", err)
	}

	root := computeBlockRoot(block)

	// Two votes = 200/300 = 66.7% -- should reach threshold.
	for i := uint64(0); i < 2; i++ {
		vote := &BlockFinalityVote{
			Slot:           1,
			BlockRoot:      root,
			ValidatorIndex: i,
			Signature:      []byte{byte(i)},
			Timestamp:      1000 + int64(i),
		}
		if err := e.ReceiveVote(vote); err != nil {
			t.Fatalf("ReceiveVote(%d): %v", i, err)
		}
	}

	// With threshold 0.667 and total 300, need 200.1 -> rounded to 200.
	// Two validators contribute 200 of stake.
	if !e.IsSlotFinalized(1) {
		t.Error("expected slot 1 to be finalized")
	}
	if e.LatestFinalizedSlot() != 1 {
		t.Errorf("expected latest finalized slot 1, got %d", e.LatestFinalizedSlot())
	}

	proof, err := e.CheckFinality(1)
	if err != nil {
		t.Fatalf("CheckFinality: %v", err)
	}
	if proof == nil {
		t.Fatal("expected non-nil proof")
	}
	if proof.BlockRoot != root {
		t.Errorf("expected proof root %v, got %v", root, proof.BlockRoot)
	}
}

func TestBlockFinalizationEngineInsufficientVotes(t *testing.T) {
	cfg := &FinalizationConfig{
		TargetLatencyMs:  500,
		MinParticipation: 0.667,
		MaxValidators:    100,
	}
	e := NewBlockFinalizationEngine(cfg)

	// Register 10 validators.
	for i := uint64(0); i < 10; i++ {
		e.RegisterValidator(i, []byte{byte(i)}, 100)
	}

	block := &FinalizationBlock{
		Slot:           1,
		ProposerIndex:  0,
		StateRoot:      types.Hash{0x01},
		ExecutionValid: true,
	}
	e.ProposeBlock(block)

	root := computeBlockRoot(block)

	// Only 3 out of 10 validators vote (300/1000 = 30%).
	for i := uint64(0); i < 3; i++ {
		vote := &BlockFinalityVote{
			Slot:           1,
			BlockRoot:      root,
			ValidatorIndex: i,
			Signature:      []byte{byte(i)},
			Timestamp:      1000,
		}
		e.ReceiveVote(vote)
	}

	if e.IsSlotFinalized(1) {
		t.Error("slot should not be finalized with insufficient votes")
	}

	proof, err := e.CheckFinality(1)
	if err != nil {
		t.Fatalf("CheckFinality: %v", err)
	}
	if proof != nil {
		t.Error("expected nil proof for non-finalized slot")
	}
}

func TestBlockFinalizationEngineDuplicateVote(t *testing.T) {
	e := NewBlockFinalizationEngine(nil)
	// Register multiple validators so a single vote doesn't finalize.
	e.RegisterValidator(0, []byte{0xAA}, 100)
	e.RegisterValidator(1, []byte{0xBB}, 100)
	e.RegisterValidator(2, []byte{0xCC}, 100)
	e.RegisterValidator(3, []byte{0xDD}, 100)

	block := &FinalizationBlock{
		Slot:           1,
		ExecutionValid: true,
	}
	e.ProposeBlock(block)

	root := computeBlockRoot(block)
	vote := &BlockFinalityVote{
		Slot:           1,
		BlockRoot:      root,
		ValidatorIndex: 0,
		Timestamp:      1000,
	}

	if err := e.ReceiveVote(vote); err != nil {
		t.Fatalf("first vote: %v", err)
	}

	err := e.ReceiveVote(vote)
	if err != ErrBFEDuplicateVote {
		t.Errorf("expected ErrBFEDuplicateVote, got %v", err)
	}
}

func TestBlockFinalizationEngineWrongSlot(t *testing.T) {
	e := NewBlockFinalizationEngine(nil)
	e.RegisterValidator(0, []byte{0xAA}, 100)

	vote := &BlockFinalityVote{
		Slot:           99, // no block at this slot
		ValidatorIndex: 0,
		Timestamp:      1000,
	}

	err := e.ReceiveVote(vote)
	if err != ErrBFEWrongSlot {
		t.Errorf("expected ErrBFEWrongSlot, got %v", err)
	}
}

func TestBlockFinalizationEngineLatencyTracking(t *testing.T) {
	e := NewBlockFinalizationEngine(nil)
	e.RegisterValidator(0, []byte{0xAA}, 100)

	block := &FinalizationBlock{
		Slot:           1,
		ExecutionValid: true,
	}
	e.ProposeBlock(block)

	root := computeBlockRoot(block)
	vote := &BlockFinalityVote{
		Slot:           1,
		BlockRoot:      root,
		ValidatorIndex: 0,
		Timestamp:      1000,
	}
	e.ReceiveVote(vote)

	// Single validator with all the stake: should finalize immediately.
	metrics := e.LatencyMetrics()
	if metrics.FinalizedBlocks != 1 {
		t.Errorf("expected 1 finalized block, got %d", metrics.FinalizedBlocks)
	}
	// The latency should be >= 0.
	if metrics.AvgLatencyMs < 0 {
		t.Errorf("expected non-negative latency, got %f", metrics.AvgLatencyMs)
	}

	// Test missed slot tracking.
	e.RecordMissedSlot()
	e.RecordMissedSlot()
	metrics = e.LatencyMetrics()
	if metrics.MissedSlots != 2 {
		t.Errorf("expected 2 missed slots, got %d", metrics.MissedSlots)
	}
}

func TestBlockFinalizationEngineMultipleSlots(t *testing.T) {
	e := NewBlockFinalizationEngine(nil)

	for i := uint64(0); i < 5; i++ {
		e.RegisterValidator(i, []byte{byte(i)}, 100)
	}

	// Propose and finalize 3 consecutive slots.
	for slot := uint64(1); slot <= 3; slot++ {
		block := &FinalizationBlock{
			Slot:           slot,
			ProposerIndex:  slot - 1,
			StateRoot:      types.Hash{byte(slot)},
			ExecutionValid: true,
		}
		e.ProposeBlock(block)

		root := computeBlockRoot(block)

		// 4 out of 5 validators vote (80% > 66.7%).
		for i := uint64(0); i < 4; i++ {
			vote := &BlockFinalityVote{
				Slot:           slot,
				BlockRoot:      root,
				ValidatorIndex: i,
				Timestamp:      int64(slot * 1000),
			}
			e.ReceiveVote(vote)
		}
	}

	for slot := uint64(1); slot <= 3; slot++ {
		if !e.IsSlotFinalized(slot) {
			t.Errorf("expected slot %d to be finalized", slot)
		}
	}

	if e.LatestFinalizedSlot() != 3 {
		t.Errorf("expected latest finalized slot 3, got %d", e.LatestFinalizedSlot())
	}

	metrics := e.LatencyMetrics()
	if metrics.FinalizedBlocks != 3 {
		t.Errorf("expected 3 finalized blocks, got %d", metrics.FinalizedBlocks)
	}
}

func TestBlockFinalizationEngineValidatorRegistration(t *testing.T) {
	e := NewBlockFinalizationEngine(nil)

	e.RegisterValidator(0, []byte{0xAA}, 100)
	e.RegisterValidator(1, []byte{0xBB}, 200)
	e.RegisterValidator(2, []byte{0xCC}, 300)

	if e.ValidatorCount() != 3 {
		t.Errorf("expected 3 validators, got %d", e.ValidatorCount())
	}
	if e.TotalStake() != 600 {
		t.Errorf("expected total stake 600, got %d", e.TotalStake())
	}

	// Update validator 0's stake.
	e.RegisterValidator(0, []byte{0xAA}, 150)
	if e.ValidatorCount() != 3 {
		t.Errorf("expected 3 validators after update, got %d", e.ValidatorCount())
	}
	if e.TotalStake() != 650 {
		t.Errorf("expected total stake 650 after update, got %d", e.TotalStake())
	}

	// Active validator set.
	active := e.ActiveValidatorSet(1)
	if len(active) != 3 {
		t.Errorf("expected 3 active validators, got %d", len(active))
	}
}

func TestBlockFinalizationEngineFinalityProof(t *testing.T) {
	e := NewBlockFinalizationEngine(nil)
	e.RegisterValidator(0, []byte{0xAA}, 100)
	e.RegisterValidator(1, []byte{0xBB}, 100)

	block := &FinalizationBlock{
		Slot:           5,
		ProposerIndex:  0,
		StateRoot:      types.Hash{0x05},
		ExecutionValid: true,
	}
	e.ProposeBlock(block)

	root := computeBlockRoot(block)

	// Both validators vote.
	for i := uint64(0); i < 2; i++ {
		vote := &BlockFinalityVote{
			Slot:           5,
			BlockRoot:      root,
			ValidatorIndex: i,
			Signature:      []byte{byte(i + 1)},
			Timestamp:      5000 + int64(i),
		}
		e.ReceiveVote(vote)
	}

	proof, err := e.CheckFinality(5)
	if err != nil {
		t.Fatalf("CheckFinality: %v", err)
	}
	if proof == nil {
		t.Fatal("expected non-nil proof")
	}
	if proof.Slot != 5 {
		t.Errorf("expected proof slot 5, got %d", proof.Slot)
	}
	if proof.TotalStake != 200 {
		t.Errorf("expected total stake 200, got %d", proof.TotalStake)
	}
	if len(proof.AggregateSignature) == 0 {
		t.Error("expected non-empty aggregate signature")
	}
	if proof.FinalizedAt == 0 {
		t.Error("expected non-zero finalized timestamp")
	}
}

func TestBlockFinalizationEngineParticipantBitfield(t *testing.T) {
	e := NewBlockFinalizationEngine(nil)

	// Register validators 0, 1, 2.
	for i := uint64(0); i < 3; i++ {
		e.RegisterValidator(i, []byte{byte(i)}, 100)
	}

	block := &FinalizationBlock{
		Slot:           1,
		ExecutionValid: true,
	}
	e.ProposeBlock(block)

	root := computeBlockRoot(block)

	// Only validators 0 and 2 vote.
	for _, idx := range []uint64{0, 2} {
		vote := &BlockFinalityVote{
			Slot:           1,
			BlockRoot:      root,
			ValidatorIndex: idx,
			Signature:      []byte{byte(idx)},
			Timestamp:      1000,
		}
		e.ReceiveVote(vote)
	}

	proof, err := e.CheckFinality(1)
	if err != nil {
		t.Fatalf("CheckFinality: %v", err)
	}
	if proof == nil {
		t.Fatal("expected non-nil proof")
	}

	if len(proof.ParticipantBitfield) == 0 {
		t.Fatal("expected non-empty bitfield")
	}

	// Validator 0 should be set (bit 0).
	if proof.ParticipantBitfield[0]&(1<<0) == 0 {
		t.Error("expected validator 0 in bitfield")
	}
	// Validator 2 should be set (bit 2).
	if proof.ParticipantBitfield[0]&(1<<2) == 0 {
		t.Error("expected validator 2 in bitfield")
	}
}

func TestBlockFinalizationEngineStakeWeighting(t *testing.T) {
	cfg := &FinalizationConfig{
		TargetLatencyMs:  500,
		MinParticipation: 0.667,
		MaxValidators:    100,
	}
	e := NewBlockFinalizationEngine(cfg)

	// Validator 0 has 700 stake, validators 1-3 have 100 each. Total 1000.
	e.RegisterValidator(0, []byte{0x00}, 700)
	e.RegisterValidator(1, []byte{0x01}, 100)
	e.RegisterValidator(2, []byte{0x02}, 100)
	e.RegisterValidator(3, []byte{0x03}, 100)

	block := &FinalizationBlock{
		Slot:           1,
		ExecutionValid: true,
	}
	e.ProposeBlock(block)

	root := computeBlockRoot(block)

	// Only validator 0 votes: 700/1000 = 70% > 66.7%.
	vote := &BlockFinalityVote{
		Slot:           1,
		BlockRoot:      root,
		ValidatorIndex: 0,
		Timestamp:      1000,
	}
	if err := e.ReceiveVote(vote); err != nil {
		t.Fatalf("ReceiveVote: %v", err)
	}

	if !e.IsSlotFinalized(1) {
		t.Error("expected finality with 70% stake from single large validator")
	}
}

func TestBlockFinalizationEngineMissedSlot(t *testing.T) {
	e := NewBlockFinalizationEngine(nil)

	e.RecordMissedSlot()
	e.RecordMissedSlot()
	e.RecordMissedSlot()

	metrics := e.LatencyMetrics()
	if metrics.MissedSlots != 3 {
		t.Errorf("expected 3 missed slots, got %d", metrics.MissedSlots)
	}
	if metrics.FinalizedBlocks != 0 {
		t.Errorf("expected 0 finalized blocks, got %d", metrics.FinalizedBlocks)
	}
}

func TestBlockFinalizationEngineConfig(t *testing.T) {
	cfg := DefaultFinalizationConfig()
	if cfg.TargetLatencyMs != 500 {
		t.Errorf("expected default target latency 500, got %d", cfg.TargetLatencyMs)
	}
	if cfg.VotingWindowMs != 400 {
		t.Errorf("expected default voting window 400, got %d", cfg.VotingWindowMs)
	}
	if cfg.AggregationWindowMs != 100 {
		t.Errorf("expected default aggregation window 100, got %d", cfg.AggregationWindowMs)
	}
	if cfg.MinParticipation != 0.667 {
		t.Errorf("expected default min participation 0.667, got %f", cfg.MinParticipation)
	}
	if cfg.MaxValidators != 131072 {
		t.Errorf("expected default max validators 131072, got %d", cfg.MaxValidators)
	}
}

func TestBlockFinalizationEngineUnknownValidator(t *testing.T) {
	e := NewBlockFinalizationEngine(nil)
	e.RegisterValidator(0, []byte{0xAA}, 100)

	block := &FinalizationBlock{
		Slot:           1,
		ExecutionValid: true,
	}
	e.ProposeBlock(block)

	vote := &BlockFinalityVote{
		Slot:           1,
		ValidatorIndex: 99, // not registered
		Timestamp:      1000,
	}

	err := e.ReceiveVote(vote)
	if err != ErrBFEUnknownValidator {
		t.Errorf("expected ErrBFEUnknownValidator, got %v", err)
	}
}

func TestBlockFinalizationEnginePruneSlots(t *testing.T) {
	e := NewBlockFinalizationEngine(nil)
	e.RegisterValidator(0, []byte{0xAA}, 100)

	// Propose and finalize slots 1-5.
	for slot := uint64(1); slot <= 5; slot++ {
		block := &FinalizationBlock{
			Slot:           slot,
			StateRoot:      types.Hash{byte(slot)},
			ExecutionValid: true,
		}
		e.ProposeBlock(block)

		root := computeBlockRoot(block)
		vote := &BlockFinalityVote{
			Slot:           slot,
			BlockRoot:      root,
			ValidatorIndex: 0,
			Timestamp:      int64(slot * 1000),
		}
		e.ReceiveVote(vote)
	}

	// All should be finalized.
	for slot := uint64(1); slot <= 5; slot++ {
		if !e.IsSlotFinalized(slot) {
			t.Errorf("expected slot %d to be finalized before prune", slot)
		}
	}

	// Prune keeping only 2 slots of history.
	e.PruneSlots(2)

	// Slots 1, 2 should be pruned (cutoff = 5 - 2 = 3).
	if e.IsSlotFinalized(1) {
		t.Error("expected slot 1 to be pruned")
	}
	if e.IsSlotFinalized(2) {
		t.Error("expected slot 2 to be pruned")
	}

	// Slots 3, 4, 5 should remain.
	for slot := uint64(3); slot <= 5; slot++ {
		if !e.IsSlotFinalized(slot) {
			t.Errorf("expected slot %d to remain after prune", slot)
		}
	}
}

func TestBlockFinalizationEngineConcurrentAccess(t *testing.T) {
	e := NewBlockFinalizationEngine(nil)

	// Register 10 validators.
	for i := uint64(0); i < 10; i++ {
		e.RegisterValidator(i, []byte{byte(i)}, 100)
	}

	block := &FinalizationBlock{
		Slot:           1,
		ExecutionValid: true,
	}
	e.ProposeBlock(block)

	root := computeBlockRoot(block)

	var wg sync.WaitGroup
	for i := uint64(0); i < 10; i++ {
		wg.Add(1)
		go func(idx uint64) {
			defer wg.Done()
			vote := &BlockFinalityVote{
				Slot:           1,
				BlockRoot:      root,
				ValidatorIndex: idx,
				Timestamp:      1000,
			}
			e.ReceiveVote(vote) // may return ErrBFESlotAlreadyFinalized, that's OK
		}(i)
	}
	wg.Wait()

	if !e.IsSlotFinalized(1) {
		t.Error("expected slot 1 to be finalized after concurrent votes")
	}
}
