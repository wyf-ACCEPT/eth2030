package consensus

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestDefaultEndgameConfig(t *testing.T) {
	cfg := DefaultEndgameConfig()
	if cfg.TargetFinalityDelay != 1 {
		t.Errorf("expected TargetFinalityDelay=1, got %d", cfg.TargetFinalityDelay)
	}
	if cfg.MaxFinalityDelay != 4 {
		t.Errorf("expected MaxFinalityDelay=4, got %d", cfg.MaxFinalityDelay)
	}
	if cfg.SubSlotCount != 3 {
		t.Errorf("expected SubSlotCount=3, got %d", cfg.SubSlotCount)
	}
}

func TestEndgameFinalityTracker_ProcessSlot_Finalize(t *testing.T) {
	consensusCfg := QuickSlotsConfig()
	endgameCfg := DefaultEndgameConfig()
	tracker := NewEndgameFinalityTracker(consensusCfg, endgameCfg)

	root := types.Hash{0xaa}
	totalWeight := uint64(300)
	slot := Slot(10)

	// Provide less than 2/3: 199/300 < 66.7%.
	attestations := []SubSlotAttestation{
		{ValidatorIndex: 1, Slot: slot, SubSlotIndex: 0, Weight: 100, BlockRoot: root},
		{ValidatorIndex: 2, Slot: slot, SubSlotIndex: 1, Weight: 50, BlockRoot: root},
		{ValidatorIndex: 3, Slot: slot, SubSlotIndex: 2, Weight: 49, BlockRoot: root},
	}

	finalized := tracker.ProcessSlot(slot, totalWeight, attestations)
	if finalized {
		t.Fatal("199 weight < 2/3 of 300 should not finalize")
	}

	// Add more weight to reach supermajority: 199 + 1 = 200, 200*3=600 >= 300*2=600.
	more := []SubSlotAttestation{
		{ValidatorIndex: 4, Slot: slot, SubSlotIndex: 0, Weight: 1, BlockRoot: root},
	}

	finalized = tracker.ProcessSlot(slot, totalWeight, more)
	// Total attesting: 200. 200*3 >= 300*2 => finalized (exactly 2/3).
	if !finalized {
		t.Fatal("200/300 should finalize (exactly 2/3)")
	}

	if !tracker.SlotFinalized(slot) {
		t.Error("slot should be marked finalized")
	}
}

func TestEndgameFinalityTracker_ProcessSlot_AlreadyFinalized(t *testing.T) {
	tracker := NewEndgameFinalityTracker(QuickSlotsConfig(), DefaultEndgameConfig())

	root := types.Hash{0xbb}
	slot := Slot(5)
	totalWeight := uint64(100)

	// Provide full weight to finalize immediately.
	atts := []SubSlotAttestation{
		{ValidatorIndex: 1, Slot: slot, SubSlotIndex: 0, Weight: 100, BlockRoot: root},
	}
	tracker.ProcessSlot(slot, totalWeight, atts)

	// Processing again should return true immediately.
	if !tracker.ProcessSlot(slot, totalWeight, nil) {
		t.Fatal("already-finalized slot should return true")
	}
}

func TestEndgameFinalityTracker_FinalityScore(t *testing.T) {
	tracker := NewEndgameFinalityTracker(QuickSlotsConfig(), DefaultEndgameConfig())

	slot := Slot(1)

	// No state yet.
	if tracker.FinalityScore(slot) != 0.0 {
		t.Error("unknown slot should have score 0")
	}

	totalWeight := uint64(300)
	atts := []SubSlotAttestation{
		{ValidatorIndex: 1, Slot: slot, SubSlotIndex: 0, Weight: 100, BlockRoot: types.Hash{1}},
	}
	tracker.ProcessSlot(slot, totalWeight, atts)

	score := tracker.FinalityScore(slot)
	// 100 / (300*2/3) = 100/200 = 0.5
	if score < 0.49 || score > 0.51 {
		t.Errorf("expected score ~0.5, got %f", score)
	}

	// Add more weight to hit 100%.
	more := []SubSlotAttestation{
		{ValidatorIndex: 2, Slot: slot, SubSlotIndex: 1, Weight: 100, BlockRoot: types.Hash{1}},
	}
	tracker.ProcessSlot(slot, totalWeight, more)

	score = tracker.FinalityScore(slot)
	// 200/200 = 1.0
	if score < 0.99 {
		t.Errorf("expected score ~1.0, got %f", score)
	}
}

func TestEndgameFinalityTracker_FinalityScore_OverOne(t *testing.T) {
	tracker := NewEndgameFinalityTracker(QuickSlotsConfig(), DefaultEndgameConfig())

	slot := Slot(2)
	totalWeight := uint64(100)

	// Weight exceeds 2/3 threshold.
	atts := []SubSlotAttestation{
		{ValidatorIndex: 1, Slot: slot, SubSlotIndex: 0, Weight: 100, BlockRoot: types.Hash{1}},
	}
	tracker.ProcessSlot(slot, totalWeight, atts)

	score := tracker.FinalityScore(slot)
	if score > 1.0 {
		t.Errorf("score should be capped at 1.0, got %f", score)
	}
}

func TestEndgameFinalityTracker_IsSubSlotFinalized(t *testing.T) {
	cfg := DefaultEndgameConfig()
	tracker := NewEndgameFinalityTracker(QuickSlotsConfig(), cfg)

	slot := Slot(7)
	totalWeight := uint64(300)

	// Sub-slot threshold = (300*2)/(3*3) = 200/3 ~= 66
	// Provide enough weight for sub-slot 0 only.
	atts := []SubSlotAttestation{
		{ValidatorIndex: 1, Slot: slot, SubSlotIndex: 0, Weight: 70, BlockRoot: types.Hash{1}},
		{ValidatorIndex: 2, Slot: slot, SubSlotIndex: 1, Weight: 30, BlockRoot: types.Hash{1}},
	}
	tracker.ProcessSlot(slot, totalWeight, atts)

	if !tracker.IsSubSlotFinalized(slot, 0) {
		t.Error("sub-slot 0 should be finalized (70 >= 66)")
	}
	if tracker.IsSubSlotFinalized(slot, 1) {
		t.Error("sub-slot 1 should not be finalized (30 < 66)")
	}
	if tracker.IsSubSlotFinalized(slot, 2) {
		t.Error("sub-slot 2 should not be finalized (0 < 66)")
	}
}

func TestEndgameFinalityTracker_IsSubSlotFinalized_UnknownSlot(t *testing.T) {
	tracker := NewEndgameFinalityTracker(QuickSlotsConfig(), DefaultEndgameConfig())
	if tracker.IsSubSlotFinalized(Slot(999), 0) {
		t.Error("unknown slot should not be finalized")
	}
}

func TestEndgameFinalityTracker_IsSubSlotFinalized_InvalidIndex(t *testing.T) {
	tracker := NewEndgameFinalityTracker(QuickSlotsConfig(), DefaultEndgameConfig())
	tracker.ProcessSlot(Slot(1), 100, []SubSlotAttestation{
		{ValidatorIndex: 1, Slot: 1, SubSlotIndex: 0, Weight: 100, BlockRoot: types.Hash{1}},
	})
	if tracker.IsSubSlotFinalized(Slot(1), 99) {
		t.Error("invalid sub-slot index should return false")
	}
}

func TestEndgameFinalityTracker_SubSlotVoting(t *testing.T) {
	tracker := NewEndgameFinalityTracker(QuickSlotsConfig(), DefaultEndgameConfig())

	slot := Slot(3)
	totalWeight := uint64(90)

	// Feed sub-slot 0.
	atts0 := []SubSlotAttestation{
		{ValidatorIndex: 1, Slot: slot, SubSlotIndex: 0, Weight: 30, BlockRoot: types.Hash{1}},
	}
	tracker.SubSlotVoting(slot, 0, totalWeight, atts0)

	// Feed sub-slot 1.
	atts1 := []SubSlotAttestation{
		{ValidatorIndex: 2, Slot: slot, SubSlotIndex: 1, Weight: 30, BlockRoot: types.Hash{1}},
	}
	tracker.SubSlotVoting(slot, 1, totalWeight, atts1)

	// Feed sub-slot 2 to finalize.
	atts2 := []SubSlotAttestation{
		{ValidatorIndex: 3, Slot: slot, SubSlotIndex: 2, Weight: 30, BlockRoot: types.Hash{1}},
	}
	finalized := tracker.SubSlotVoting(slot, 2, totalWeight, atts2)
	// Total: 90/90 >= 2/3 => finalized.
	if !finalized {
		t.Fatal("slot should be finalized after all sub-slots vote")
	}
}

func TestEndgameFinalityTracker_SubSlotVoting_FiltersMismatch(t *testing.T) {
	tracker := NewEndgameFinalityTracker(QuickSlotsConfig(), DefaultEndgameConfig())

	slot := Slot(4)
	// Include attestations for wrong slot and wrong sub-slot.
	atts := []SubSlotAttestation{
		{ValidatorIndex: 1, Slot: slot, SubSlotIndex: 0, Weight: 50, BlockRoot: types.Hash{1}},
		{ValidatorIndex: 2, Slot: slot + 1, SubSlotIndex: 0, Weight: 50, BlockRoot: types.Hash{1}}, // wrong slot
		{ValidatorIndex: 3, Slot: slot, SubSlotIndex: 1, Weight: 50, BlockRoot: types.Hash{1}},     // wrong sub-slot
	}

	tracker.SubSlotVoting(slot, 0, 100, atts)

	// Only the first attestation should be counted.
	score := tracker.FinalityScore(slot)
	// 50 / (100*2/3) = 50/66 ~= 0.75
	if score < 0.74 || score > 0.76 {
		t.Errorf("expected score ~0.75, got %f", score)
	}
}

func TestEndgameFinalityTracker_PruneSlots(t *testing.T) {
	tracker := NewEndgameFinalityTracker(QuickSlotsConfig(), DefaultEndgameConfig())

	// Create state for slots 1-5.
	for s := Slot(1); s <= 5; s++ {
		tracker.ProcessSlot(s, 100, []SubSlotAttestation{
			{ValidatorIndex: 1, Slot: s, SubSlotIndex: 0, Weight: 100, BlockRoot: types.Hash{1}},
		})
	}

	// Prune slots before 4.
	tracker.PruneSlots(Slot(4))

	if tracker.FinalityScore(Slot(1)) != 0.0 {
		t.Error("slot 1 should be pruned")
	}
	if tracker.FinalityScore(Slot(3)) != 0.0 {
		t.Error("slot 3 should be pruned")
	}
	if tracker.FinalityScore(Slot(4)) == 0.0 {
		t.Error("slot 4 should still exist")
	}
	if tracker.FinalityScore(Slot(5)) == 0.0 {
		t.Error("slot 5 should still exist")
	}
}

func TestEndgameFinalityTracker_FinalityScore_ZeroWeight(t *testing.T) {
	tracker := NewEndgameFinalityTracker(QuickSlotsConfig(), DefaultEndgameConfig())

	// Zero total weight.
	tracker.ProcessSlot(Slot(1), 0, nil)
	if tracker.FinalityScore(Slot(1)) != 0.0 {
		t.Error("zero total weight should give score 0")
	}
}

func TestEndgameFinalityTracker_SkipWrongSlotAttestations(t *testing.T) {
	tracker := NewEndgameFinalityTracker(QuickSlotsConfig(), DefaultEndgameConfig())

	slot := Slot(10)
	atts := []SubSlotAttestation{
		{ValidatorIndex: 1, Slot: slot + 1, SubSlotIndex: 0, Weight: 100, BlockRoot: types.Hash{1}}, // wrong slot
	}

	tracker.ProcessSlot(slot, 100, atts)
	if tracker.FinalityScore(slot) != 0.0 {
		t.Error("wrong-slot attestations should not count")
	}
}

func TestValidateEndgameVote(t *testing.T) {
	cfg := DefaultEndgameConfig()

	// Valid vote.
	att := &SubSlotAttestation{
		Slot: 1, SubSlotIndex: 0,
		BlockRoot: types.Hash{0x01}, Weight: 10,
	}
	if err := ValidateEndgameVote(att, cfg); err != nil {
		t.Errorf("valid vote: %v", err)
	}

	// Nil vote.
	if err := ValidateEndgameVote(nil, cfg); err == nil {
		t.Error("expected error for nil vote")
	}

	// Zero weight.
	zw := &SubSlotAttestation{Slot: 1, BlockRoot: types.Hash{0x01}, Weight: 0}
	if err := ValidateEndgameVote(zw, cfg); err == nil {
		t.Error("expected error for zero weight")
	}

	// Sub-slot index out of range.
	oor := &SubSlotAttestation{
		Slot: 1, SubSlotIndex: cfg.SubSlotCount + 1,
		BlockRoot: types.Hash{0x01}, Weight: 10,
	}
	if err := ValidateEndgameVote(oor, cfg); err == nil {
		t.Error("expected error for sub-slot out of range")
	}
}

func TestValidateEndgameConfig(t *testing.T) {
	cfg := DefaultEndgameConfig()
	if err := ValidateEndgameConfig(cfg); err != nil {
		t.Errorf("valid config: %v", err)
	}

	if err := ValidateEndgameConfig(nil); err == nil {
		t.Error("expected error for nil config")
	}

	bad := *cfg
	bad.SubSlotCount = 0
	if err := ValidateEndgameConfig(&bad); err == nil {
		t.Error("expected error for zero sub-slot count")
	}
}
