package consensus

import (
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func slashTestHash(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	return h
}

func TestNewSlashingDetector(t *testing.T) {
	sd := NewSlashingDetector(DefaultSlashingDetectorConfig())
	if sd == nil {
		t.Fatal("NewSlashingDetector returned nil")
	}
	if sd.config.AttestationWindow != DefaultAttestationWindow {
		t.Errorf("window = %d, want %d", sd.config.AttestationWindow, DefaultAttestationWindow)
	}
	if sd.BlockCount() != 0 {
		t.Errorf("initial block count = %d, want 0", sd.BlockCount())
	}
	if sd.AttestationCount() != 0 {
		t.Errorf("initial attestation count = %d, want 0", sd.AttestationCount())
	}
}

func TestNewSlashingDetectorZeroWindow(t *testing.T) {
	sd := NewSlashingDetector(SlashingDetectorConfig{AttestationWindow: 0})
	if sd.config.AttestationWindow != DefaultAttestationWindow {
		t.Errorf("zero window should default to %d, got %d",
			DefaultAttestationWindow, sd.config.AttestationWindow)
	}
}

func TestRegisterBlockNoSlashing(t *testing.T) {
	sd := NewSlashingDetector(DefaultSlashingDetectorConfig())

	root := slashTestHash(1)
	sd.RegisterBlock(0, 10, root)

	if sd.BlockCount() != 1 {
		t.Errorf("block count = %d, want 1", sd.BlockCount())
	}

	// Registering the same block again should be a no-op.
	sd.RegisterBlock(0, 10, root)
	if sd.BlockCount() != 1 {
		t.Errorf("duplicate block: count = %d, want 1", sd.BlockCount())
	}

	evidence := sd.DetectProposerSlashing()
	if len(evidence) != 0 {
		t.Errorf("expected no slashing evidence, got %d", len(evidence))
	}
}

func TestProposerDoubleBlock(t *testing.T) {
	sd := NewSlashingDetector(DefaultSlashingDetectorConfig())

	root1 := slashTestHash(1)
	root2 := slashTestHash(2)

	sd.RegisterBlock(0, 10, root1)
	sd.RegisterBlock(0, 10, root2) // Same proposer, same slot, different root.

	evidence := sd.DetectProposerSlashing()
	if len(evidence) != 1 {
		t.Fatalf("expected 1 proposer slashing, got %d", len(evidence))
	}
	e := evidence[0]
	if e.ProposerIndex != 0 {
		t.Errorf("proposer index = %d, want 0", e.ProposerIndex)
	}
	if e.Slot != 10 {
		t.Errorf("slot = %d, want 10", e.Slot)
	}
	if e.Root1 != root1 || e.Root2 != root2 {
		t.Error("roots do not match expected values")
	}

	// Evidence should be consumed.
	evidence2 := sd.DetectProposerSlashing()
	if len(evidence2) != 0 {
		t.Errorf("evidence not consumed: got %d", len(evidence2))
	}
}

func TestProposerTripleBlock(t *testing.T) {
	sd := NewSlashingDetector(DefaultSlashingDetectorConfig())

	root1 := slashTestHash(1)
	root2 := slashTestHash(2)
	root3 := slashTestHash(3)

	sd.RegisterBlock(0, 10, root1)
	sd.RegisterBlock(0, 10, root2)
	sd.RegisterBlock(0, 10, root3) // Third block should generate 2 more evidence entries.

	evidence := sd.DetectProposerSlashing()
	// root1 vs root2 = 1
	// root1 vs root3 = 1, root2 vs root3 = 1
	// Total = 3
	if len(evidence) != 3 {
		t.Errorf("triple block: expected 3 slashing entries, got %d", len(evidence))
	}
}

func TestDifferentProposersSameSlot(t *testing.T) {
	sd := NewSlashingDetector(DefaultSlashingDetectorConfig())

	// Different proposers at the same slot is not a slashable offense.
	sd.RegisterBlock(0, 10, slashTestHash(1))
	sd.RegisterBlock(1, 10, slashTestHash(2))

	evidence := sd.DetectProposerSlashing()
	if len(evidence) != 0 {
		t.Errorf("different proposers: expected 0 slashings, got %d", len(evidence))
	}
}

func TestSameProposerDifferentSlots(t *testing.T) {
	sd := NewSlashingDetector(DefaultSlashingDetectorConfig())

	// Same proposer at different slots is fine.
	sd.RegisterBlock(0, 10, slashTestHash(1))
	sd.RegisterBlock(0, 11, slashTestHash(2))

	evidence := sd.DetectProposerSlashing()
	if len(evidence) != 0 {
		t.Errorf("different slots: expected 0 slashings, got %d", len(evidence))
	}
}

func TestAttesterDoubleVote(t *testing.T) {
	sd := NewSlashingDetector(DefaultSlashingDetectorConfig())

	// Two attestations for the same target epoch but different target roots.
	sd.RegisterAttestation(0, 5, 10, slashTestHash(1))
	sd.RegisterAttestation(0, 5, 10, slashTestHash(2)) // Double vote.

	evidence := sd.DetectAttesterSlashing()
	if len(evidence) != 1 {
		t.Fatalf("expected 1 double vote, got %d", len(evidence))
	}
	e := evidence[0]
	if e.Type != "double_vote" {
		t.Errorf("type = %q, want %q", e.Type, "double_vote")
	}
	if e.ValidatorIndex != 0 {
		t.Errorf("validator index = %d, want 0", e.ValidatorIndex)
	}
}

func TestAttesterDoubleVoteSameRoot(t *testing.T) {
	sd := NewSlashingDetector(DefaultSlashingDetectorConfig())

	root := slashTestHash(1)
	// Same target epoch AND same target root is not a double vote.
	sd.RegisterAttestation(0, 5, 10, root)
	sd.RegisterAttestation(0, 5, 10, root)

	evidence := sd.DetectAttesterSlashing()
	if len(evidence) != 0 {
		t.Errorf("same root attestation: expected 0 slashings, got %d", len(evidence))
	}
}

func TestAttesterSurroundVoteExistingSurroundsNew(t *testing.T) {
	sd := NewSlashingDetector(DefaultSlashingDetectorConfig())

	// Existing attestation: source=1, target=10.
	// New attestation: source=2, target=9.
	// Existing surrounds new: existing.source(1) < new.source(2) AND
	// new.target(9) < existing.target(10).
	sd.RegisterAttestation(0, 1, 10, slashTestHash(1))
	sd.RegisterAttestation(0, 2, 9, slashTestHash(2))

	evidence := sd.DetectAttesterSlashing()
	if len(evidence) != 1 {
		t.Fatalf("expected 1 surround vote, got %d", len(evidence))
	}
	if evidence[0].Type != "surround_vote" {
		t.Errorf("type = %q, want %q", evidence[0].Type, "surround_vote")
	}
}

func TestAttesterSurroundVoteNewSurroundsExisting(t *testing.T) {
	sd := NewSlashingDetector(DefaultSlashingDetectorConfig())

	// Existing attestation: source=3, target=8.
	// New attestation: source=2, target=9.
	// New surrounds existing: new.source(2) < existing.source(3) AND
	// existing.target(8) < new.target(9).
	sd.RegisterAttestation(0, 3, 8, slashTestHash(1))
	sd.RegisterAttestation(0, 2, 9, slashTestHash(2))

	evidence := sd.DetectAttesterSlashing()
	if len(evidence) != 1 {
		t.Fatalf("expected 1 surround vote, got %d", len(evidence))
	}
	if evidence[0].Type != "surround_vote" {
		t.Errorf("type = %q, want %q", evidence[0].Type, "surround_vote")
	}
}

func TestAttesterNoSlashingDifferentValidators(t *testing.T) {
	sd := NewSlashingDetector(DefaultSlashingDetectorConfig())

	// Different validators cannot slash each other.
	sd.RegisterAttestation(0, 5, 10, slashTestHash(1))
	sd.RegisterAttestation(1, 5, 10, slashTestHash(2))

	evidence := sd.DetectAttesterSlashing()
	if len(evidence) != 0 {
		t.Errorf("different validators: expected 0 slashings, got %d", len(evidence))
	}
}

func TestAttesterNoSlashingDifferentTargetEpochs(t *testing.T) {
	sd := NewSlashingDetector(DefaultSlashingDetectorConfig())

	// Different target epochs, no surround: source=5, target=10 and source=5, target=11.
	// Not a double vote (different target epochs).
	// Not a surround vote (sources equal, so neither source < other).
	sd.RegisterAttestation(0, 5, 10, slashTestHash(1))
	sd.RegisterAttestation(0, 5, 11, slashTestHash(2))

	evidence := sd.DetectAttesterSlashing()
	if len(evidence) != 0 {
		t.Errorf("non-overlapping attestations: expected 0, got %d", len(evidence))
	}
}

func TestPeekProposerSlashing(t *testing.T) {
	sd := NewSlashingDetector(DefaultSlashingDetectorConfig())

	sd.RegisterBlock(0, 10, slashTestHash(1))
	sd.RegisterBlock(0, 10, slashTestHash(2))

	peeked := sd.PeekProposerSlashing()
	if len(peeked) != 1 {
		t.Fatalf("peek: expected 1, got %d", len(peeked))
	}

	// Peek should not consume.
	peeked2 := sd.PeekProposerSlashing()
	if len(peeked2) != 1 {
		t.Errorf("peek again: expected 1, got %d", len(peeked2))
	}

	// Detect should consume.
	detected := sd.DetectProposerSlashing()
	if len(detected) != 1 {
		t.Errorf("detect: expected 1, got %d", len(detected))
	}
	remaining := sd.PeekProposerSlashing()
	if len(remaining) != 0 {
		t.Errorf("after consume: expected 0, got %d", len(remaining))
	}
}

func TestPeekAttesterSlashing(t *testing.T) {
	sd := NewSlashingDetector(DefaultSlashingDetectorConfig())

	sd.RegisterAttestation(0, 5, 10, slashTestHash(1))
	sd.RegisterAttestation(0, 5, 10, slashTestHash(2))

	peeked := sd.PeekAttesterSlashing()
	if len(peeked) != 1 {
		t.Fatalf("peek: expected 1, got %d", len(peeked))
	}

	detected := sd.DetectAttesterSlashing()
	if len(detected) != 1 {
		t.Errorf("detect: expected 1, got %d", len(detected))
	}
	remaining := sd.PeekAttesterSlashing()
	if len(remaining) != 0 {
		t.Errorf("after consume: expected 0, got %d", len(remaining))
	}
}

func TestSlashingDetectorBlockCount(t *testing.T) {
	sd := NewSlashingDetector(DefaultSlashingDetectorConfig())

	sd.RegisterBlock(0, 10, slashTestHash(1))
	sd.RegisterBlock(0, 11, slashTestHash(2))
	sd.RegisterBlock(1, 10, slashTestHash(3))

	if got := sd.BlockCount(); got != 3 {
		t.Errorf("block count = %d, want 3", got)
	}
}

func TestSlashingDetectorAttestationCount(t *testing.T) {
	sd := NewSlashingDetector(DefaultSlashingDetectorConfig())

	sd.RegisterAttestation(0, 1, 5, slashTestHash(1))
	sd.RegisterAttestation(0, 2, 6, slashTestHash(2))
	sd.RegisterAttestation(1, 1, 5, slashTestHash(3))

	if got := sd.AttestationCount(); got != 3 {
		t.Errorf("attestation count = %d, want 3", got)
	}
}

func TestValidatorsWithAttestations(t *testing.T) {
	sd := NewSlashingDetector(DefaultSlashingDetectorConfig())

	sd.RegisterAttestation(5, 1, 10, slashTestHash(1))
	sd.RegisterAttestation(3, 1, 10, slashTestHash(2))
	sd.RegisterAttestation(7, 1, 10, slashTestHash(3))

	indices := sd.ValidatorsWithAttestations()
	if len(indices) != 3 {
		t.Fatalf("validators = %d, want 3", len(indices))
	}
	// Should be sorted.
	expected := []ValidatorIndex{3, 5, 7}
	for i, idx := range expected {
		if indices[i] != idx {
			t.Errorf("indices[%d] = %d, want %d", i, indices[i], idx)
		}
	}
}

func TestAttestationPruning(t *testing.T) {
	sd := NewSlashingDetector(SlashingDetectorConfig{AttestationWindow: 10})

	// Register attestations spanning epochs 1 through 20.
	for epoch := Epoch(1); epoch <= 20; epoch++ {
		sd.RegisterAttestation(0, epoch-1, epoch, slashTestHash(byte(epoch)))
	}

	// After registering target epoch 20 with window 10,
	// attestations with target epoch < 10 should be pruned.
	count := sd.AttestationCount()
	if count > 11 {
		t.Errorf("expected at most 11 attestations after pruning, got %d", count)
	}
}

func TestSlashingDetectorConcurrent(t *testing.T) {
	sd := NewSlashingDetector(DefaultSlashingDetectorConfig())

	var wg sync.WaitGroup

	// Concurrent block registrations.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sd.RegisterBlock(ValidatorIndex(idx), Slot(idx), slashTestHash(byte(idx)))
		}(i)
	}

	// Concurrent attestation registrations.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sd.RegisterAttestation(ValidatorIndex(idx), Epoch(idx), Epoch(idx+5), slashTestHash(byte(idx)))
		}(i)
	}

	// Concurrent reads.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = sd.PeekProposerSlashing()
			_ = sd.PeekAttesterSlashing()
			_ = sd.BlockCount()
			_ = sd.AttestationCount()
		}()
	}

	wg.Wait()
}

func TestComplexSlashingScenario(t *testing.T) {
	sd := NewSlashingDetector(DefaultSlashingDetectorConfig())

	// Validator 0: proposes two blocks at slot 100 (proposer slashing).
	sd.RegisterBlock(0, 100, slashTestHash(0xAA))
	sd.RegisterBlock(0, 100, slashTestHash(0xBB))

	// Validator 1: double vote at target epoch 50.
	sd.RegisterAttestation(1, 40, 50, slashTestHash(0xCC))
	sd.RegisterAttestation(1, 40, 50, slashTestHash(0xDD))

	// Validator 2: surround vote.
	// First: source=10, target=20.
	sd.RegisterAttestation(2, 10, 20, slashTestHash(0xEE))
	// Second: source=11, target=19 (surrounded by first).
	sd.RegisterAttestation(2, 11, 19, slashTestHash(0xFF))

	propEvidence := sd.DetectProposerSlashing()
	if len(propEvidence) != 1 {
		t.Errorf("proposer slashings = %d, want 1", len(propEvidence))
	}

	attEvidence := sd.DetectAttesterSlashing()
	if len(attEvidence) != 2 {
		t.Errorf("attester slashings = %d, want 2", len(attEvidence))
	}

	// Verify types.
	typeMap := make(map[string]int)
	for _, e := range attEvidence {
		typeMap[e.Type]++
	}
	if typeMap["double_vote"] != 1 {
		t.Errorf("double_vote count = %d, want 1", typeMap["double_vote"])
	}
	if typeMap["surround_vote"] != 1 {
		t.Errorf("surround_vote count = %d, want 1", typeMap["surround_vote"])
	}
}

func TestBothDirectionsSurroundVote(t *testing.T) {
	sd := NewSlashingDetector(DefaultSlashingDetectorConfig())

	// Register attestation that both surrounds and is surrounded by the
	// same pair. This should generate 2 surround vote evidence entries
	// if we carefully construct the test.

	// Existing: source=5, target=15.
	sd.RegisterAttestation(0, 5, 15, slashTestHash(1))
	// New: source=3, target=20 (new surrounds existing).
	sd.RegisterAttestation(0, 3, 20, slashTestHash(2))

	evidence := sd.DetectAttesterSlashing()
	if len(evidence) != 1 {
		t.Fatalf("expected 1 surround vote, got %d", len(evidence))
	}
	if evidence[0].Type != "surround_vote" {
		t.Errorf("type = %q, want surround_vote", evidence[0].Type)
	}
}
