package consensus

import (
	"testing"
)

// makeRotationTestValidators creates a slice of validator indices for testing.
func makeRotationTestValidators(count int) []ValidatorIndex {
	validators := make([]ValidatorIndex, count)
	for i := 0; i < count; i++ {
		validators[i] = ValidatorIndex(i)
	}
	return validators
}

func TestComputeNextSyncCommittee(t *testing.T) {
	mgr := NewSyncCommitteeRotationManager(nil)
	validators := makeRotationTestValidators(1000)
	seed := [32]byte{0x01, 0x02, 0x03, 0x04}

	committee := mgr.ComputeNextSyncCommittee(validators, seed, 512)
	if committee == nil {
		t.Fatal("expected non-nil committee")
	}
	if len(committee.MemberIndices) != 512 {
		t.Fatalf("expected 512 members, got %d", len(committee.MemberIndices))
	}
	if len(committee.Pubkeys) != 512 {
		t.Fatalf("expected 512 pubkeys, got %d", len(committee.Pubkeys))
	}
	emptyPk := [48]byte{}
	if committee.AggregatePubkey == emptyPk {
		t.Error("aggregate pubkey should not be empty")
	}
}

func TestComputeNextSyncCommitteeEmpty(t *testing.T) {
	mgr := NewSyncCommitteeRotationManager(nil)
	committee := mgr.ComputeNextSyncCommittee(nil, [32]byte{}, 512)
	if committee != nil {
		t.Error("expected nil committee for empty validators")
	}
}

func TestComputeNextSyncCommitteeDeterministic(t *testing.T) {
	mgr := NewSyncCommitteeRotationManager(nil)
	validators := makeRotationTestValidators(500)
	seed := [32]byte{0xAA, 0xBB}

	c1 := mgr.ComputeNextSyncCommittee(validators, seed, 64)
	c2 := mgr.ComputeNextSyncCommittee(validators, seed, 64)

	for i := range c1.MemberIndices {
		if c1.MemberIndices[i] != c2.MemberIndices[i] {
			t.Fatalf("non-deterministic at position %d", i)
		}
	}
}

func TestInitializeCommitteesRotation(t *testing.T) {
	mgr := NewSyncCommitteeRotationManager(nil)
	validators := makeRotationTestValidators(1000)
	seed := [32]byte{0x01}

	if err := mgr.InitializeCommittees(validators, seed, 5); err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}

	current := mgr.GetCurrentCommittee()
	if current == nil {
		t.Fatal("current committee should not be nil")
	}
	if current.Period != 5 {
		t.Errorf("expected period 5, got %d", current.Period)
	}

	next := mgr.GetNextCommittee()
	if next == nil {
		t.Fatal("next committee should not be nil")
	}
	if next.Period != 6 {
		t.Errorf("expected period 6, got %d", next.Period)
	}
}

func TestInitializeCommitteesNoValidatorsRotation(t *testing.T) {
	mgr := NewSyncCommitteeRotationManager(nil)
	err := mgr.InitializeCommittees(nil, [32]byte{}, 0)
	if err != ErrSCRNoValidators {
		t.Errorf("expected ErrSCRNoValidators, got %v", err)
	}
}

func TestShouldRotateRotation(t *testing.T) {
	mgr := NewSyncCommitteeRotationManager(nil)
	tests := []struct {
		epoch  Epoch
		expect bool
	}{
		{0, false},
		{1, false},
		{255, false},
		{256, true},
		{512, true},
		{257, false},
	}
	for _, tt := range tests {
		got := mgr.ShouldRotate(tt.epoch, 0)
		if got != tt.expect {
			t.Errorf("ShouldRotate(%d): got %v, want %v", tt.epoch, got, tt.expect)
		}
	}
}

func TestRotateCommitteeRotation(t *testing.T) {
	mgr := NewSyncCommitteeRotationManager(nil)
	validators := makeRotationTestValidators(1000)
	seed := [32]byte{0x01}

	mgr.InitializeCommittees(validators, seed, 0)

	if err := mgr.RotateCommittee(256, validators, seed); err != nil {
		t.Fatalf("rotation failed: %v", err)
	}

	newCurrent := mgr.GetCurrentCommittee()
	if newCurrent == nil {
		t.Fatal("current committee should not be nil after rotation")
	}
	if newCurrent.Period != 1 {
		t.Errorf("expected period 1, got %d", newCurrent.Period)
	}

	newNext := mgr.GetNextCommittee()
	if newNext == nil {
		t.Fatal("next committee should not be nil after rotation")
	}
	if newNext.Period != 2 {
		t.Errorf("expected next period 2, got %d", newNext.Period)
	}
}

func TestIsSyncCommitteeMemberRotation(t *testing.T) {
	mgr := NewSyncCommitteeRotationManager(nil)
	validators := makeRotationTestValidators(1000)
	seed := [32]byte{0x01}
	mgr.InitializeCommittees(validators, seed, 0)

	current := mgr.GetCurrentCommittee()
	firstMember := current.MemberIndices[0]

	if !mgr.IsSyncCommitteeMember(firstMember) {
		t.Error("first member should be recognized as committee member")
	}
	if mgr.IsSyncCommitteeMember(ValidatorIndex(99999)) {
		t.Error("99999 should not be a committee member")
	}
}

func TestGetMemberPosition(t *testing.T) {
	mgr := NewSyncCommitteeRotationManager(nil)
	validators := makeRotationTestValidators(1000)
	seed := [32]byte{0x01}
	mgr.InitializeCommittees(validators, seed, 0)

	current := mgr.GetCurrentCommittee()
	pos := mgr.GetMemberPosition(current.MemberIndices[0])
	if pos < 0 {
		t.Error("expected non-negative position for member")
	}
	if mgr.GetMemberPosition(ValidatorIndex(99999)) != -1 {
		t.Error("expected -1 for non-member")
	}
}

func TestProcessSyncCommitteeMessageRotation(t *testing.T) {
	mgr := NewSyncCommitteeRotationManager(nil)
	validators := makeRotationTestValidators(1000)
	seed := [32]byte{0x01}
	mgr.InitializeCommittees(validators, seed, 0)

	current := mgr.GetCurrentCommittee()
	member := current.MemberIndices[0]
	sig := [96]byte{0x01}

	if err := mgr.ProcessSyncCommitteeMessage(10, member, sig); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mgr.ProcessSyncCommitteeMessage(10, member, sig); err != ErrSCRDuplicateMessage {
		t.Errorf("expected ErrSCRDuplicateMessage, got %v", err)
	}
	if err := mgr.ProcessSyncCommitteeMessage(10, ValidatorIndex(99999), sig); err != ErrSCRNotMember {
		t.Errorf("expected ErrSCRNotMember, got %v", err)
	}
}

func TestProcessSyncCommitteeMessageNotInitialized(t *testing.T) {
	mgr := NewSyncCommitteeRotationManager(nil)
	err := mgr.ProcessSyncCommitteeMessage(1, ValidatorIndex(0), [96]byte{})
	if err != ErrSCRNotInitialized {
		t.Errorf("expected ErrSCRNotInitialized, got %v", err)
	}
}

func TestGetSlotParticipationRotation(t *testing.T) {
	mgr := NewSyncCommitteeRotationManager(nil)
	validators := makeRotationTestValidators(1000)
	seed := [32]byte{0x01}
	mgr.InitializeCommittees(validators, seed, 0)

	current := mgr.GetCurrentCommittee()
	sig := [96]byte{0x01}

	for i := 0; i < 5 && i < len(current.MemberIndices); i++ {
		mgr.ProcessSyncCommitteeMessage(10, current.MemberIndices[i], sig)
	}
	if mgr.GetSlotParticipation(10) != 5 {
		t.Errorf("expected 5 participants, got %d", mgr.GetSlotParticipation(10))
	}
	if mgr.GetSlotParticipation(11) != 0 {
		t.Error("expected 0 for empty slot")
	}
}

func TestAddContributionRotation(t *testing.T) {
	mgr := NewSyncCommitteeRotationManager(nil)
	contrib := &SyncCommitteeContribution{
		Slot: 10, SubcommitteeIndex: 0,
		AggregationBits: []byte{0x0F}, Signature: [96]byte{0x01},
	}
	if err := mgr.AddContribution(contrib); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := mgr.GetContribution(10, 0)
	if got == nil {
		t.Fatal("expected contribution")
	}
	if !BitfieldEqual(got.AggregationBits, []byte{0x0F}) {
		t.Error("bits mismatch")
	}
}

func TestAddContributionMergeRotation(t *testing.T) {
	mgr := NewSyncCommitteeRotationManager(nil)
	c1 := &SyncCommitteeContribution{Slot: 10, SubcommitteeIndex: 0,
		AggregationBits: []byte{0x0F}, Signature: [96]byte{0x01}}
	c2 := &SyncCommitteeContribution{Slot: 10, SubcommitteeIndex: 0,
		AggregationBits: []byte{0xF0}, Signature: [96]byte{0x02}}

	mgr.AddContribution(c1)
	if err := mgr.AddContribution(c2); err != nil {
		t.Fatalf("merge failed: %v", err)
	}
	got := mgr.GetContribution(10, 0)
	if !BitfieldEqual(got.AggregationBits, []byte{0xFF}) {
		t.Errorf("expected 0xFF, got %v", got.AggregationBits)
	}
}

func TestAddContributionOverlapRotation(t *testing.T) {
	mgr := NewSyncCommitteeRotationManager(nil)
	c1 := &SyncCommitteeContribution{Slot: 10, SubcommitteeIndex: 0,
		AggregationBits: []byte{0x0F}, Signature: [96]byte{0x01}}
	c2 := &SyncCommitteeContribution{Slot: 10, SubcommitteeIndex: 0,
		AggregationBits: []byte{0x03}, Signature: [96]byte{0x02}}

	mgr.AddContribution(c1)
	if err := mgr.AddContribution(c2); err != ErrSCROverlappingBits {
		t.Errorf("expected ErrSCROverlappingBits, got %v", err)
	}
}

func TestAddContributionInvalidSubcommittee(t *testing.T) {
	mgr := NewSyncCommitteeRotationManager(nil)
	contrib := &SyncCommitteeContribution{Slot: 10, SubcommitteeIndex: SubcommitteeCount}
	if err := mgr.AddContribution(contrib); err != ErrSCRInvalidSubcommIdx {
		t.Errorf("expected ErrSCRInvalidSubcommIdx, got %v", err)
	}
}

func TestPruneOldMessagesRotation(t *testing.T) {
	mgr := NewSyncCommitteeRotationManager(nil)
	validators := makeRotationTestValidators(1000)
	seed := [32]byte{0x01}
	mgr.InitializeCommittees(validators, seed, 0)

	current := mgr.GetCurrentCommittee()
	sig := [96]byte{0x01}

	// Message at slot 5 (will be pruned).
	mgr.ProcessSyncCommitteeMessage(5, current.MemberIndices[0], sig)
	// Message at slot 90 (will be retained, cutoff is 80).
	if len(current.MemberIndices) > 1 {
		mgr.ProcessSyncCommitteeMessage(90, current.MemberIndices[1], sig)
	}
	// Contribution at slot 5 (will be pruned).
	mgr.AddContribution(&SyncCommitteeContribution{
		Slot: 5, SubcommitteeIndex: 0, AggregationBits: []byte{0x01},
	})

	// Prune with currentSlot=100, maxAge=20. Cutoff = 80.
	mgr.PruneOldMessages(100, 20)

	if mgr.GetSlotParticipation(5) != 0 {
		t.Error("slot 5 should be pruned")
	}
	if mgr.GetSlotParticipation(90) != 1 {
		t.Error("slot 90 should remain")
	}
	if mgr.GetContribution(5, 0) != nil {
		t.Error("slot 5 contribution should be pruned")
	}
}

func TestCurrentPeriodRotation(t *testing.T) {
	mgr := NewSyncCommitteeRotationManager(nil)
	if mgr.CurrentPeriod() != 0 {
		t.Error("expected 0 before init")
	}
	validators := makeRotationTestValidators(1000)
	mgr.InitializeCommittees(validators, [32]byte{0x01}, 42)
	if mgr.CurrentPeriod() != 42 {
		t.Errorf("expected 42, got %d", mgr.CurrentPeriod())
	}
}
