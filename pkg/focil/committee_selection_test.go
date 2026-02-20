package focil

import (
	"testing"
)

func makeSelectionValidators(n int) []uint64 {
	vals := make([]uint64, n)
	for i := range vals {
		vals[i] = uint64(i + 1)
	}
	return vals
}

func TestCommitteeSelectionBasic(t *testing.T) {
	vals := makeSelectionValidators(100)
	cs := NewCommitteeSelector(DefaultCommitteeSelectionConfig(), vals)

	committee, err := cs.SelectCommittee(1)
	if err != nil {
		t.Fatalf("SelectCommittee: %v", err)
	}
	if len(committee) != IL_COMMITTEE_SIZE {
		t.Errorf("committee size = %d, want %d", len(committee), IL_COMMITTEE_SIZE)
	}

	// All members should be valid validator indices.
	valSet := make(map[uint64]bool)
	for _, v := range vals {
		valSet[v] = true
	}
	for _, m := range committee {
		if !valSet[m] {
			t.Errorf("committee member %d not in validator set", m)
		}
	}

	// Committee should be sorted.
	for i := 1; i < len(committee); i++ {
		if committee[i] <= committee[i-1] {
			t.Errorf("committee not sorted: [%d]=%d <= [%d]=%d",
				i, committee[i], i-1, committee[i-1])
		}
	}
}

func TestCommitteeSelectionDeterministic(t *testing.T) {
	vals := makeSelectionValidators(50)
	cs := NewCommitteeSelector(DefaultCommitteeSelectionConfig(), vals)

	c1, _ := cs.SelectCommittee(42)
	c2, _ := cs.SelectCommittee(42)

	if len(c1) != len(c2) {
		t.Fatalf("committees have different sizes: %d vs %d", len(c1), len(c2))
	}
	for i := range c1 {
		if c1[i] != c2[i] {
			t.Errorf("committees differ at index %d: %d vs %d", i, c1[i], c2[i])
		}
	}
}

func TestCommitteeSelectionSlotZero(t *testing.T) {
	vals := makeSelectionValidators(50)
	cs := NewCommitteeSelector(DefaultCommitteeSelectionConfig(), vals)

	_, err := cs.SelectCommittee(0)
	if err == nil {
		t.Fatal("expected error for slot 0")
	}
}

func TestCommitteeSelectionNoValidators(t *testing.T) {
	cs := NewCommitteeSelector(DefaultCommitteeSelectionConfig(), nil)

	_, err := cs.SelectCommittee(1)
	if err == nil {
		t.Fatal("expected error for no validators")
	}
}

func TestCommitteeSelectionWithEpochSeed(t *testing.T) {
	vals := makeSelectionValidators(100)
	cs := NewCommitteeSelector(DefaultCommitteeSelectionConfig(), vals)

	// Set a specific seed for epoch 0.
	var seed [32]byte
	copy(seed[:], []byte("test-randao-seed-epoch-zero-1234"))
	if err := cs.SetEpochSeed(0, seed); err != nil {
		t.Fatalf("SetEpochSeed: %v", err)
	}

	c1, _ := cs.SelectCommittee(1)

	// Different epoch seed should produce different committee.
	var seed2 [32]byte
	copy(seed2[:], []byte("different-randao-seed-for-test!!"))
	cs2 := NewCommitteeSelector(DefaultCommitteeSelectionConfig(), vals)
	_ = cs2.SetEpochSeed(0, seed2)
	c2, _ := cs2.SelectCommittee(1)

	// While it's theoretically possible for them to match, with 100 validators
	// and a 16-member committee, the probability is astronomically low.
	same := true
	for i := range c1 {
		if c1[i] != c2[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("different seeds should produce different committees")
	}
}

func TestCommitteeSelectionZeroSeed(t *testing.T) {
	vals := makeSelectionValidators(50)
	cs := NewCommitteeSelector(DefaultCommitteeSelectionConfig(), vals)

	err := cs.SetEpochSeed(0, [32]byte{})
	if err == nil {
		t.Fatal("expected error for zero seed")
	}
}

func TestCommitteeSelectionSmallValidatorSet(t *testing.T) {
	// Fewer validators than committee size.
	vals := makeSelectionValidators(5)
	cs := NewCommitteeSelector(DefaultCommitteeSelectionConfig(), vals)

	committee, err := cs.SelectCommittee(1)
	if err != nil {
		t.Fatalf("SelectCommittee: %v", err)
	}
	// Should select all 5 validators.
	if len(committee) != 5 {
		t.Errorf("committee size = %d, want 5", len(committee))
	}
}

func TestCommitteeSelectionDifferentSlots(t *testing.T) {
	vals := makeSelectionValidators(100)
	cs := NewCommitteeSelector(DefaultCommitteeSelectionConfig(), vals)

	c1, _ := cs.SelectCommittee(1)
	c2, _ := cs.SelectCommittee(2)

	// Different slots should (almost certainly) produce different committees.
	same := true
	for i := range c1 {
		if c1[i] != c2[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("different slots should produce different committees")
	}
}

func TestCommitteeSelectionFallback(t *testing.T) {
	vals := makeSelectionValidators(50)
	cs := NewCommitteeSelector(DefaultCommitteeSelectionConfig(), vals)

	committee, _ := cs.SelectCommittee(1)
	fallback, err := cs.SelectFallback(1)
	if err != nil {
		t.Fatalf("SelectFallback: %v", err)
	}

	if len(fallback) != cs.config.FallbackSize {
		t.Errorf("fallback size = %d, want %d", len(fallback), cs.config.FallbackSize)
	}

	// Fallback should not overlap with primary committee.
	committeeSet := make(map[uint64]bool)
	for _, v := range committee {
		committeeSet[v] = true
	}
	for _, v := range fallback {
		if committeeSet[v] {
			t.Errorf("fallback member %d is also in primary committee", v)
		}
	}
}

func TestCommitteeSelectionFallbackSmallSet(t *testing.T) {
	// Only enough validators for the committee, no fallback possible.
	vals := makeSelectionValidators(IL_COMMITTEE_SIZE)
	cs := NewCommitteeSelector(DefaultCommitteeSelectionConfig(), vals)

	_, err := cs.SelectFallback(1)
	if err == nil {
		t.Fatal("expected error when no fallback validators available")
	}
}

func TestCommitteeSelectionFallbackSlotZero(t *testing.T) {
	vals := makeSelectionValidators(50)
	cs := NewCommitteeSelector(DefaultCommitteeSelectionConfig(), vals)

	_, err := cs.SelectFallback(0)
	if err == nil {
		t.Fatal("expected error for slot 0 fallback")
	}
}

func TestCommitteeSelectionLookAhead(t *testing.T) {
	vals := makeSelectionValidators(100)
	cs := NewCommitteeSelector(DefaultCommitteeSelectionConfig(), vals)

	lookAhead, err := cs.ComputeLookAhead(1, 5)
	if err != nil {
		t.Fatalf("ComputeLookAhead: %v", err)
	}
	if len(lookAhead) != 5 {
		t.Errorf("look-ahead size = %d, want 5", len(lookAhead))
	}

	// Each slot should have a committee.
	for slot := uint64(1); slot <= 5; slot++ {
		c, ok := lookAhead[slot]
		if !ok {
			t.Errorf("missing committee for slot %d", slot)
			continue
		}
		if len(c) != IL_COMMITTEE_SIZE {
			t.Errorf("slot %d: committee size = %d, want %d", slot, len(c), IL_COMMITTEE_SIZE)
		}
	}
}

func TestCommitteeSelectionLookAheadErrors(t *testing.T) {
	vals := makeSelectionValidators(50)
	cs := NewCommitteeSelector(DefaultCommitteeSelectionConfig(), vals)

	_, err := cs.ComputeLookAhead(0, 5)
	if err == nil {
		t.Fatal("expected error for slot 0")
	}

	_, err = cs.ComputeLookAhead(1, 0)
	if err == nil {
		t.Fatal("expected error for count 0")
	}
}

func TestCommitteeSelectionLookAheadCap(t *testing.T) {
	vals := makeSelectionValidators(50)
	config := DefaultCommitteeSelectionConfig()
	config.MaxLookAhead = 3
	cs := NewCommitteeSelector(config, vals)

	lookAhead, err := cs.ComputeLookAhead(1, 100)
	if err != nil {
		t.Fatalf("ComputeLookAhead: %v", err)
	}
	// Should be capped at MaxLookAhead.
	if len(lookAhead) != 3 {
		t.Errorf("look-ahead size = %d, want 3 (capped)", len(lookAhead))
	}
}

func TestCommitteeSelectionProofGenAndVerify(t *testing.T) {
	vals := makeSelectionValidators(100)
	cs := NewCommitteeSelector(DefaultCommitteeSelectionConfig(), vals)

	var seed [32]byte
	copy(seed[:], []byte("test-seed-for-proof-generation!"))
	_ = cs.SetEpochSeed(0, seed)

	committee, _ := cs.SelectCommittee(1)
	if len(committee) == 0 {
		t.Fatal("committee is empty")
	}

	// Generate proof for first committee member.
	proof, err := cs.GenerateSelectionProof(committee[0], 1)
	if err != nil {
		t.Fatalf("GenerateSelectionProof: %v", err)
	}
	if proof.ValidatorIndex != committee[0] {
		t.Errorf("proof.ValidatorIndex = %d, want %d", proof.ValidatorIndex, committee[0])
	}
	if proof.Slot != 1 {
		t.Errorf("proof.Slot = %d, want 1", proof.Slot)
	}
	if proof.CommitteePosition != 0 {
		t.Errorf("proof.CommitteePosition = %d, want 0", proof.CommitteePosition)
	}
	if proof.Commitment.IsZero() {
		t.Error("proof commitment should not be zero")
	}

	// Verify the proof.
	if !VerifySelectionProof(proof, seed) {
		t.Error("valid proof should verify")
	}

	// Tamper with the proof.
	proof.CommitteePosition = 99
	if VerifySelectionProof(proof, seed) {
		t.Error("tampered proof should not verify")
	}

	// Nil proof.
	if VerifySelectionProof(nil, seed) {
		t.Error("nil proof should not verify")
	}
}

func TestCommitteeSelectionProofNonMember(t *testing.T) {
	vals := makeSelectionValidators(100)
	cs := NewCommitteeSelector(DefaultCommitteeSelectionConfig(), vals)

	committee, _ := cs.SelectCommittee(1)
	committeeSet := make(map[uint64]bool)
	for _, v := range committee {
		committeeSet[v] = true
	}

	// Find a validator NOT in the committee.
	var nonMember uint64
	for _, v := range vals {
		if !committeeSet[v] {
			nonMember = v
			break
		}
	}
	if nonMember == 0 {
		t.Skip("all validators in committee, cannot test non-member")
	}

	_, err := cs.GenerateSelectionProof(nonMember, 1)
	if err == nil {
		t.Fatal("expected error for non-committee member proof")
	}
}

func TestCommitteeSelectionRotationRecords(t *testing.T) {
	vals := makeSelectionValidators(50)
	cs := NewCommitteeSelector(DefaultCommitteeSelectionConfig(), vals)

	records := cs.GetRotationRecords(1, 10)
	if len(records) == 0 {
		t.Fatal("expected rotation records for 10 slots")
	}

	// Check that records are sorted by validator index.
	for i := 1; i < len(records); i++ {
		if records[i].ValidatorIndex <= records[i-1].ValidatorIndex {
			t.Errorf("records not sorted: [%d]=%d <= [%d]=%d",
				i, records[i].ValidatorIndex, i-1, records[i-1].ValidatorIndex)
		}
	}

	// Total assignments across all records should be 10 slots * committee_size.
	totalAssignments := 0
	for _, r := range records {
		totalAssignments += r.AssignmentCount
	}
	expectedAssignments := 10 * IL_COMMITTEE_SIZE
	if totalAssignments != expectedAssignments {
		t.Errorf("total assignments = %d, want %d", totalAssignments, expectedAssignments)
	}
}

func TestCommitteeSelectionPruneCache(t *testing.T) {
	vals := makeSelectionValidators(50)
	cs := NewCommitteeSelector(DefaultCommitteeSelectionConfig(), vals)

	for i := uint64(1); i <= 10; i++ {
		_, _ = cs.SelectCommittee(i)
	}
	if cs.CachedSlots() != 10 {
		t.Errorf("cached slots = %d, want 10", cs.CachedSlots())
	}

	pruned := cs.PruneCacheBefore(6)
	if pruned != 5 {
		t.Errorf("pruned %d, want 5", pruned)
	}
	if cs.CachedSlots() != 5 {
		t.Errorf("cached slots after prune = %d, want 5", cs.CachedSlots())
	}
}

func TestCommitteeSelectionValidatorCount(t *testing.T) {
	vals := makeSelectionValidators(42)
	cs := NewCommitteeSelector(DefaultCommitteeSelectionConfig(), vals)
	if cs.ValidatorCount() != 42 {
		t.Errorf("ValidatorCount = %d, want 42", cs.ValidatorCount())
	}
}
