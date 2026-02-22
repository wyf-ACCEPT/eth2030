package consensus

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestNewEndgameEngine(t *testing.T) {
	cfg := DefaultEndgameEngineConfig()
	eng := NewEndgameEngine(cfg)
	if eng == nil {
		t.Fatal("NewEndgameEngine returned nil")
	}
	if cfg.FinalityThreshold != 0.667 {
		t.Errorf("FinalityThreshold: got %f, want 0.667", cfg.FinalityThreshold)
	}
	if cfg.MaxSlotHistory != 64 {
		t.Errorf("MaxSlotHistory: got %d, want 64", cfg.MaxSlotHistory)
	}
	if cfg.TargetFinalityMs != 500 {
		t.Errorf("TargetFinalityMs: got %d, want 500", cfg.TargetFinalityMs)
	}
	if cfg.OptimisticThreshold != 0.5 {
		t.Errorf("OptimisticThreshold: got %f, want 0.5", cfg.OptimisticThreshold)
	}
}

func TestEndgameEngine_SetValidatorSet(t *testing.T) {
	eng := NewEndgameEngine(DefaultEndgameEngineConfig())
	eng.SetValidatorSet(map[uint64]uint64{0: 100, 1: 200, 2: 300})

	eng.mu.RLock()
	defer eng.mu.RUnlock()
	if eng.totalWeight != 600 {
		t.Errorf("totalWeight: got %d, want 600", eng.totalWeight)
	}

	// Replace with new set.
	eng.mu.RUnlock()
	eng.SetValidatorSet(map[uint64]uint64{5: 500})
	eng.mu.RLock()
	if eng.totalWeight != 500 {
		t.Errorf("after replace: got %d, want 500", eng.totalWeight)
	}
}

func TestEndgameEngine_SubmitVote_Errors(t *testing.T) {
	eng := NewEndgameEngine(DefaultEndgameEngineConfig())
	eng.SetValidatorSet(map[uint64]uint64{0: 100})

	// Zero weight.
	err := eng.SubmitVote(&EndgameVote{Slot: 1, ValidatorIndex: 0, Weight: 0, Timestamp: 1})
	if err != ErrZeroWeight {
		t.Errorf("expected ErrZeroWeight, got %v", err)
	}

	// Unknown validator.
	err = eng.SubmitVote(&EndgameVote{Slot: 1, ValidatorIndex: 99, BlockHash: types.Hash{0xaa}, Weight: 50, Timestamp: 1})
	if err != ErrUnknownValidator {
		t.Errorf("expected ErrUnknownValidator, got %v", err)
	}

	// Duplicate vote.
	eng.SubmitVote(&EndgameVote{Slot: 1, ValidatorIndex: 0, BlockHash: types.Hash{0xaa}, Weight: 100, Timestamp: 1})
	err = eng.SubmitVote(&EndgameVote{Slot: 1, ValidatorIndex: 0, BlockHash: types.Hash{0xaa}, Weight: 100, Timestamp: 2})
	if err != ErrDuplicateVote {
		t.Errorf("expected ErrDuplicateVote, got %v", err)
	}
}

func TestEndgameEngine_SubmitVote_Basic(t *testing.T) {
	eng := NewEndgameEngine(DefaultEndgameEngineConfig())
	eng.SetValidatorSet(map[uint64]uint64{0: 100, 1: 200})

	eng.SubmitVote(&EndgameVote{Slot: 1, ValidatorIndex: 0, BlockHash: types.Hash{0xaa}, Weight: 100, Timestamp: 1000})
	r := eng.CheckFinality(1)
	if r.IsFinalized {
		t.Error("100/300 should not finalize")
	}
	if r.TotalWeight != 100 {
		t.Errorf("TotalWeight: got %d, want 100", r.TotalWeight)
	}
}

func TestEndgameEngine_Finalization(t *testing.T) {
	cfg := DefaultEndgameEngineConfig()
	cfg.FinalityThreshold = 0.667
	eng := NewEndgameEngine(cfg)
	eng.SetValidatorSet(map[uint64]uint64{0: 100, 1: 100, 2: 100})
	// threshold = uint64(0.667 * 300) = 200
	hash := types.Hash{0xbb}

	eng.SubmitVote(&EndgameVote{Slot: 5, ValidatorIndex: 0, BlockHash: hash, Weight: 100, Timestamp: 100})
	if eng.CheckFinality(5).IsFinalized {
		t.Error("100/300 should not finalize")
	}

	eng.SubmitVote(&EndgameVote{Slot: 5, ValidatorIndex: 1, BlockHash: hash, Weight: 100, Timestamp: 200})
	r := eng.CheckFinality(5)
	if !r.IsFinalized {
		t.Error("200/300 should finalize (200 >= threshold 200)")
	}
	if r.FinalizedHash != hash {
		t.Errorf("FinalizedHash: got %v, want %v", r.FinalizedHash, hash)
	}
	if r.TimeToFinality != 100 {
		t.Errorf("TimeToFinality: got %d, want 100", r.TimeToFinality)
	}
}

func TestEndgameEngine_Finalization_ExactThreshold(t *testing.T) {
	cfg := DefaultEndgameEngineConfig()
	cfg.FinalityThreshold = 0.5
	eng := NewEndgameEngine(cfg)
	eng.SetValidatorSet(map[uint64]uint64{0: 50, 1: 50})

	hash := types.Hash{0xcc}
	eng.SubmitVote(&EndgameVote{Slot: 1, ValidatorIndex: 0, BlockHash: hash, Weight: 50, Timestamp: 10})
	if !eng.CheckFinality(1).IsFinalized {
		t.Error("50/100 with 0.5 threshold should finalize")
	}
}

func TestEndgameEngine_CheckFinality_UnknownSlot(t *testing.T) {
	eng := NewEndgameEngine(DefaultEndgameEngineConfig())
	r := eng.CheckFinality(999)
	if r.IsFinalized {
		t.Error("unknown slot should not be finalized")
	}
	if r.Slot != 999 {
		t.Errorf("Slot: got %d, want 999", r.Slot)
	}
}

func TestEndgameEngine_ProcessSlotEnd(t *testing.T) {
	cfg := DefaultEndgameEngineConfig()
	cfg.FinalityThreshold = 0.5
	eng := NewEndgameEngine(cfg)
	eng.SetValidatorSet(map[uint64]uint64{0: 100, 1: 100})

	hash := types.Hash{0xdd}
	eng.SubmitVote(&EndgameVote{Slot: 3, ValidatorIndex: 0, BlockHash: hash, Weight: 100, Timestamp: 50})

	r := eng.ProcessSlotEnd(3)
	if !r.IsFinalized {
		t.Error("100/200 with 0.5 threshold should finalize on ProcessSlotEnd")
	}
	if r.FinalizedHash != hash {
		t.Errorf("FinalizedHash: got %v, want %v", r.FinalizedHash, hash)
	}

	// No votes slot.
	r2 := eng.ProcessSlotEnd(42)
	if r2.IsFinalized {
		t.Error("slot with no votes should not be finalized")
	}
}

func TestEndgameEngine_OptimisticConfirmation(t *testing.T) {
	cfg := DefaultEndgameEngineConfig()
	cfg.OptimisticThreshold = 0.3
	eng := NewEndgameEngine(cfg)
	eng.SetValidatorSet(map[uint64]uint64{0: 40, 1: 60})

	hash := types.Hash{0xee}
	eng.SubmitVote(&EndgameVote{Slot: 1, ValidatorIndex: 0, BlockHash: hash, Weight: 40, Timestamp: 100})

	or := eng.OptimisticConfirmation(hash)
	if !or.Confirmed {
		t.Error("40/100 should confirm at 0.3 threshold")
	}
	if or.Confidence < 0.39 || or.Confidence > 0.41 {
		t.Errorf("Confidence: got %f, want ~0.4", or.Confidence)
	}

	// Not confirmed case.
	cfg2 := DefaultEndgameEngineConfig()
	cfg2.OptimisticThreshold = 0.5
	eng2 := NewEndgameEngine(cfg2)
	eng2.SetValidatorSet(map[uint64]uint64{0: 10, 1: 90})
	eng2.SubmitVote(&EndgameVote{Slot: 1, ValidatorIndex: 0, BlockHash: hash, Weight: 10, Timestamp: 50})
	or2 := eng2.OptimisticConfirmation(hash)
	if or2.Confirmed {
		t.Error("10/100 should not confirm at 0.5 threshold")
	}

	// No validators.
	eng3 := NewEndgameEngine(DefaultEndgameEngineConfig())
	or3 := eng3.OptimisticConfirmation(types.Hash{0x01})
	if or3.Confirmed || or3.Confidence != 0 {
		t.Error("no validators: should not confirm, confidence should be 0")
	}
}

func TestEndgameEngine_OptimisticConfirmation_Timing(t *testing.T) {
	cfg := DefaultEndgameEngineConfig()
	cfg.OptimisticThreshold = 0.3
	eng := NewEndgameEngine(cfg)
	eng.SetValidatorSet(map[uint64]uint64{0: 50, 1: 50})

	hash := types.Hash{0xab}
	eng.SubmitVote(&EndgameVote{Slot: 1, ValidatorIndex: 0, BlockHash: hash, Weight: 50, Timestamp: 100})
	eng.SubmitVote(&EndgameVote{Slot: 1, ValidatorIndex: 1, BlockHash: hash, Weight: 50, Timestamp: 250})

	if or := eng.OptimisticConfirmation(hash); or.TimeMs != 150 {
		t.Errorf("TimeMs: got %d, want 150", or.TimeMs)
	}
}

func TestEndgameEngine_GetFinalizedChain(t *testing.T) {
	cfg := DefaultEndgameEngineConfig()
	cfg.FinalityThreshold = 0.5
	eng := NewEndgameEngine(cfg)
	eng.SetValidatorSet(map[uint64]uint64{0: 100})

	// Empty chain.
	if len(eng.GetFinalizedChain()) != 0 {
		t.Error("expected empty chain initially")
	}

	hashes := [3]types.Hash{{0x01}, {0x02}, {0x03}}
	for i, h := range hashes {
		eng.SubmitVote(&EndgameVote{Slot: uint64(i + 1), ValidatorIndex: 0, BlockHash: h, Weight: 100, Timestamp: uint64(i * 10)})
	}

	chain := eng.GetFinalizedChain()
	if len(chain) != 3 {
		t.Fatalf("chain length: got %d, want 3", len(chain))
	}
	for i, h := range hashes {
		if chain[i] != h {
			t.Errorf("chain[%d]: got %v, want %v", i, chain[i], h)
		}
	}

	// Verify it's a copy.
	chain[0] = types.Hash{0xff}
	if eng.GetFinalizedChain()[0] == (types.Hash{0xff}) {
		t.Error("GetFinalizedChain should return a copy")
	}
}

func TestEndgameEngine_CompetingBlocks(t *testing.T) {
	cfg := DefaultEndgameEngineConfig()
	cfg.FinalityThreshold = 0.75
	eng := NewEndgameEngine(cfg)
	eng.SetValidatorSet(map[uint64]uint64{0: 100, 1: 100, 2: 100, 3: 100})
	// threshold = 300

	hashA, hashB := types.Hash{0xaa}, types.Hash{0xbb}
	eng.SubmitVote(&EndgameVote{Slot: 1, ValidatorIndex: 0, BlockHash: hashA, Weight: 100, Timestamp: 10})
	eng.SubmitVote(&EndgameVote{Slot: 1, ValidatorIndex: 1, BlockHash: hashA, Weight: 100, Timestamp: 20})
	eng.SubmitVote(&EndgameVote{Slot: 1, ValidatorIndex: 2, BlockHash: hashB, Weight: 100, Timestamp: 30})
	eng.SubmitVote(&EndgameVote{Slot: 1, ValidatorIndex: 3, BlockHash: hashB, Weight: 100, Timestamp: 40})

	if eng.CheckFinality(1).IsFinalized {
		t.Error("200/400 with 0.75 threshold should not finalize")
	}

	// Winner finalization with lower threshold.
	cfg2 := DefaultEndgameEngineConfig()
	cfg2.FinalityThreshold = 0.5
	eng2 := NewEndgameEngine(cfg2)
	eng2.SetValidatorSet(map[uint64]uint64{0: 100, 1: 100, 2: 100})
	eng2.SubmitVote(&EndgameVote{Slot: 1, ValidatorIndex: 0, BlockHash: hashA, Weight: 100, Timestamp: 10})
	eng2.SubmitVote(&EndgameVote{Slot: 1, ValidatorIndex: 1, BlockHash: hashB, Weight: 100, Timestamp: 20})
	eng2.SubmitVote(&EndgameVote{Slot: 1, ValidatorIndex: 2, BlockHash: hashA, Weight: 100, Timestamp: 30})

	r := eng2.CheckFinality(1)
	if !r.IsFinalized || r.FinalizedHash != hashA {
		t.Error("hashA with 200/300 at 0.5 threshold should finalize")
	}
}

func TestEndgameEngine_PruneOldSlots(t *testing.T) {
	cfg := DefaultEndgameEngineConfig()
	cfg.FinalityThreshold = 0.5
	cfg.MaxSlotHistory = 3
	eng := NewEndgameEngine(cfg)
	eng.SetValidatorSet(map[uint64]uint64{0: 100})

	for s := uint64(1); s <= 10; s++ {
		eng.SubmitVote(&EndgameVote{
			Slot: s, ValidatorIndex: 0,
			BlockHash: types.BytesToHash([]byte{byte(s)}),
			Weight:    100, Timestamp: s * 10,
		})
	}

	eng.mu.RLock()
	defer eng.mu.RUnlock()
	for s := uint64(1); s < 7; s++ {
		if _, ok := eng.slots[s]; ok {
			t.Errorf("slot %d should have been pruned", s)
		}
	}
	for s := uint64(7); s <= 10; s++ {
		if _, ok := eng.slots[s]; !ok {
			t.Errorf("slot %d should still exist", s)
		}
	}
}

func TestEndgameEngine_VoteTooOld(t *testing.T) {
	cfg := DefaultEndgameEngineConfig()
	cfg.FinalityThreshold = 0.5
	cfg.MaxSlotHistory = 5
	eng := NewEndgameEngine(cfg)
	eng.SetValidatorSet(map[uint64]uint64{0: 100, 1: 100})

	eng.SubmitVote(&EndgameVote{Slot: 100, ValidatorIndex: 0, BlockHash: types.Hash{0x01}, Weight: 100, Timestamp: 1000})

	err := eng.SubmitVote(&EndgameVote{Slot: 1, ValidatorIndex: 1, BlockHash: types.Hash{0x02}, Weight: 100, Timestamp: 2000})
	if err != ErrInvalidSlot {
		t.Errorf("expected ErrInvalidSlot, got %v", err)
	}
}

func TestEndgameEngine_NoValidatorSet(t *testing.T) {
	cfg := DefaultEndgameEngineConfig()
	cfg.FinalityThreshold = 0.5
	eng := NewEndgameEngine(cfg)

	hash := types.Hash{0xaa}
	eng.SubmitVote(&EndgameVote{Slot: 1, ValidatorIndex: 0, BlockHash: hash, Weight: 60, Timestamp: 10})
	eng.SubmitVote(&EndgameVote{Slot: 1, ValidatorIndex: 1, BlockHash: hash, Weight: 40, Timestamp: 20})

	if !eng.CheckFinality(1).IsFinalized {
		t.Error("should finalize without pre-set validators")
	}
}

func TestEndgameEngine_ThreadSafety(t *testing.T) {
	cfg := DefaultEndgameEngineConfig()
	cfg.FinalityThreshold = 0.5
	eng := NewEndgameEngine(cfg)

	weights := make(map[uint64]uint64)
	for i := uint64(0); i < 100; i++ {
		weights[i] = 10
	}
	eng.SetValidatorSet(weights)

	var wg sync.WaitGroup
	hash := types.Hash{0xcc}

	for i := uint64(0); i < 100; i++ {
		wg.Add(1)
		go func(idx uint64) {
			defer wg.Done()
			eng.SubmitVote(&EndgameVote{Slot: 1, ValidatorIndex: idx, BlockHash: hash, Weight: 10, Timestamp: idx * 10})
		}(i)
	}
	wg.Wait()

	r := eng.CheckFinality(1)
	if !r.IsFinalized {
		t.Error("all validators voted, should be finalized")
	}
	if r.TotalWeight != 1000 {
		t.Errorf("TotalWeight: got %d, want 1000", r.TotalWeight)
	}

	// Concurrent reads should not panic.
	for i := 0; i < 20; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); eng.CheckFinality(1) }()
		go func() { defer wg.Done(); eng.GetFinalizedChain() }()
		go func() { defer wg.Done(); eng.OptimisticConfirmation(hash) }()
	}
	wg.Wait()
}

func TestEndgameEngine_MultipleSlots(t *testing.T) {
	cfg := DefaultEndgameEngineConfig()
	cfg.FinalityThreshold = 0.5
	eng := NewEndgameEngine(cfg)
	eng.SetValidatorSet(map[uint64]uint64{0: 100})

	for s := uint64(1); s <= 5; s++ {
		eng.SubmitVote(&EndgameVote{
			Slot: s, ValidatorIndex: 0,
			BlockHash: types.BytesToHash([]byte{byte(s)}),
			Weight:    100, Timestamp: s * 100,
		})
	}
	for s := uint64(1); s <= 5; s++ {
		if !eng.CheckFinality(s).IsFinalized {
			t.Errorf("slot %d should be finalized", s)
		}
	}
	if len(eng.GetFinalizedChain()) != 5 {
		t.Errorf("chain length: got %d, want 5", len(eng.GetFinalizedChain()))
	}
}

func TestEndgameEngine_CheckFinality_Threshold(t *testing.T) {
	cfg := DefaultEndgameEngineConfig()
	cfg.FinalityThreshold = 0.75
	eng := NewEndgameEngine(cfg)
	eng.SetValidatorSet(map[uint64]uint64{0: 100, 1: 100, 2: 100, 3: 100})

	eng.SubmitVote(&EndgameVote{Slot: 1, ValidatorIndex: 0, BlockHash: types.Hash{0x01}, Weight: 100, Timestamp: 10})
	if r := eng.CheckFinality(1); r.Threshold != 300 {
		t.Errorf("Threshold: got %d, want 300", r.Threshold)
	}
}
