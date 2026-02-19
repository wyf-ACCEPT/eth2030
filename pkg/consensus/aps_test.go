package consensus

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestDefaultAPSConfig(t *testing.T) {
	cfg := DefaultAPSConfig()
	if cfg.AttesterWeight != 0.9 {
		t.Errorf("expected AttesterWeight=0.9, got %f", cfg.AttesterWeight)
	}
	if cfg.ProposerWeight != 0.1 {
		t.Errorf("expected ProposerWeight=0.1, got %f", cfg.ProposerWeight)
	}
}

func TestAPSConfig_IsAPSActive(t *testing.T) {
	cfg := &APSConfig{SeparationEpoch: 10}
	if cfg.IsAPSActive(9) {
		t.Error("APS should not be active before SeparationEpoch")
	}
	if !cfg.IsAPSActive(10) {
		t.Error("APS should be active at SeparationEpoch")
	}
	if !cfg.IsAPSActive(11) {
		t.Error("APS should be active after SeparationEpoch")
	}
}

func TestShuffleValidators(t *testing.T) {
	vals := []uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	seed := types.Hash{1, 2, 3}

	shuffled := ShuffleValidators(vals, seed)

	if len(shuffled) != len(vals) {
		t.Fatalf("expected %d validators, got %d", len(vals), len(shuffled))
	}

	// All original elements should still be present.
	seen := make(map[uint64]bool)
	for _, v := range shuffled {
		seen[v] = true
	}
	for _, v := range vals {
		if !seen[v] {
			t.Errorf("validator %d missing from shuffled output", v)
		}
	}

	// The shuffle should actually change the order (statistically very unlikely to be identical).
	same := true
	for i := range vals {
		if vals[i] != shuffled[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("shuffle should change the order for non-trivial inputs")
	}
}

func TestShuffleValidators_Deterministic(t *testing.T) {
	vals := []uint64{0, 1, 2, 3, 4}
	seed := types.Hash{42}

	s1 := ShuffleValidators(vals, seed)
	s2 := ShuffleValidators(vals, seed)

	for i := range s1 {
		if s1[i] != s2[i] {
			t.Fatal("same seed should produce same shuffle")
		}
	}
}

func TestShuffleValidators_DifferentSeeds(t *testing.T) {
	vals := []uint64{0, 1, 2, 3, 4, 5, 6, 7}
	seed1 := types.Hash{1}
	seed2 := types.Hash{2}

	s1 := ShuffleValidators(vals, seed1)
	s2 := ShuffleValidators(vals, seed2)

	same := true
	for i := range s1 {
		if s1[i] != s2[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("different seeds should produce different shuffles")
	}
}

func TestShuffleValidators_Empty(t *testing.T) {
	result := ShuffleValidators(nil, types.Hash{})
	if result != nil {
		t.Error("nil input should return nil")
	}
}

func TestShuffleValidators_Single(t *testing.T) {
	result := ShuffleValidators([]uint64{42}, types.Hash{1})
	if len(result) != 1 || result[0] != 42 {
		t.Error("single element should remain unchanged")
	}
}

func TestShuffleValidators_DoesNotMutateInput(t *testing.T) {
	vals := []uint64{0, 1, 2, 3}
	original := make([]uint64, len(vals))
	copy(original, vals)

	ShuffleValidators(vals, types.Hash{99})

	for i := range vals {
		if vals[i] != original[i] {
			t.Fatal("ShuffleValidators should not mutate the input slice")
		}
	}
}

func TestSplitDuties(t *testing.T) {
	vals := make([]uint64, 100)
	for i := range vals {
		vals[i] = uint64(i)
	}

	cfg := DefaultAPSConfig() // 90% attesters, 10% proposers
	attesters, proposers := SplitDuties(vals, cfg)

	if len(attesters)+len(proposers) != len(vals) {
		t.Fatalf("split should cover all validators: %d + %d != %d",
			len(attesters), len(proposers), len(vals))
	}

	if len(proposers) < 1 {
		t.Error("should have at least 1 proposer")
	}

	// With 100 validators and 10% proposer weight, expect 10 proposers.
	if len(proposers) != 10 {
		t.Errorf("expected 10 proposers, got %d", len(proposers))
	}
}

func TestSplitDuties_Empty(t *testing.T) {
	a, p := SplitDuties(nil, DefaultAPSConfig())
	if a != nil || p != nil {
		t.Error("empty input should return nil slices")
	}
}

func TestSplitDuties_MinOneProposer(t *testing.T) {
	// Even with a tiny set, at least 1 proposer.
	vals := []uint64{0, 1, 2}
	cfg := &APSConfig{ProposerWeight: 0.01}
	_, proposers := SplitDuties(vals, cfg)
	if len(proposers) < 1 {
		t.Error("should always have at least 1 proposer")
	}
}

func TestComputeAttesterDuties(t *testing.T) {
	vals := make([]uint64, 20)
	for i := range vals {
		vals[i] = uint64(i)
	}

	apsCfg := DefaultAPSConfig()
	apsCfg.SeparationEpoch = 0

	consensusCfg := QuickSlotsConfig() // 4 slots per epoch
	randao := types.Hash{7, 8, 9}

	duties := ComputeAttesterDuties(Epoch(5), vals, randao, apsCfg, consensusCfg)

	if len(duties) == 0 {
		t.Fatal("expected attester duties")
	}

	// With APS active, attesters should be ~90% of 20 = 18.
	if len(duties) != 18 {
		t.Errorf("expected 18 attester duties, got %d", len(duties))
	}

	// Check slot range: epoch 5 with 4 slots/epoch = slots 20-23.
	for _, d := range duties {
		if d.Slot < 20 || d.Slot > 23 {
			t.Errorf("duty slot %d outside epoch 5 range [20, 23]", d.Slot)
		}
	}
}

func TestComputeProposerDuties(t *testing.T) {
	vals := make([]uint64, 20)
	for i := range vals {
		vals[i] = uint64(i)
	}

	apsCfg := DefaultAPSConfig()
	apsCfg.SeparationEpoch = 0

	consensusCfg := QuickSlotsConfig()
	randao := types.Hash{1, 2, 3}

	duties := ComputeProposerDuties(Epoch(3), vals, randao, apsCfg, consensusCfg)

	// Should have exactly SlotsPerEpoch duties.
	if len(duties) != int(consensusCfg.SlotsPerEpoch) {
		t.Fatalf("expected %d proposer duties, got %d", consensusCfg.SlotsPerEpoch, len(duties))
	}

	// Check slot range: epoch 3 with 4 slots/epoch = slots 12-15.
	for i, d := range duties {
		expectedSlot := Slot(12 + uint64(i))
		if d.Slot != expectedSlot {
			t.Errorf("duty[%d] slot=%d, expected %d", i, d.Slot, expectedSlot)
		}
	}
}

func TestComputeAttesterDuties_PreAPS(t *testing.T) {
	// Before APS activation, all validators are attesters.
	vals := make([]uint64, 10)
	for i := range vals {
		vals[i] = uint64(i)
	}

	apsCfg := &APSConfig{SeparationEpoch: 100, AttesterWeight: 0.9, ProposerWeight: 0.1}
	consensusCfg := DefaultConfig()
	randao := types.Hash{5}

	duties := ComputeAttesterDuties(Epoch(5), vals, randao, apsCfg, consensusCfg)
	if len(duties) != 10 {
		t.Errorf("pre-APS: all validators should attest, got %d duties", len(duties))
	}
}

func TestComputeAttesterDuties_Empty(t *testing.T) {
	duties := ComputeAttesterDuties(Epoch(0), nil, types.Hash{}, DefaultAPSConfig(), DefaultConfig())
	if duties != nil {
		t.Error("empty validator set should return nil")
	}
}

func TestComputeProposerDuties_Empty(t *testing.T) {
	duties := ComputeProposerDuties(Epoch(0), nil, types.Hash{}, DefaultAPSConfig(), DefaultConfig())
	if duties != nil {
		t.Error("empty validator set should return nil")
	}
}
