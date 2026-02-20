package focil

import (
	"errors"
	"testing"
)

// makeValidators creates a sorted slice of validator indices [0..n).
func makeValidators(n int) []uint64 {
	vals := make([]uint64, n)
	for i := range vals {
		vals[i] = uint64(i)
	}
	return vals
}

func TestNewCommitteeTracker(t *testing.T) {
	vals := makeValidators(100)
	ct := NewCommitteeTracker(DefaultCommitteeTrackerConfig(), vals)
	if ct == nil {
		t.Fatal("NewCommitteeTracker returned nil")
	}
}

func TestNewCommitteeTracker_DefaultsApplied(t *testing.T) {
	vals := makeValidators(100)
	ct := NewCommitteeTracker(CommitteeTrackerConfig{}, vals)
	if ct.config.CommitteeSize != IL_COMMITTEE_SIZE {
		t.Errorf("CommitteeSize = %d, want %d", ct.config.CommitteeSize, IL_COMMITTEE_SIZE)
	}
	if ct.config.CacheSlots != 64 {
		t.Errorf("CacheSlots = %d, want 64", ct.config.CacheSlots)
	}
}

func TestGetCommittee_SlotZero(t *testing.T) {
	ct := NewCommitteeTracker(DefaultCommitteeTrackerConfig(), makeValidators(100))
	_, err := ct.GetCommittee(0)
	if err != ErrTrackerSlotZero {
		t.Errorf("expected ErrTrackerSlotZero, got %v", err)
	}
}

func TestGetCommittee_NoValidators(t *testing.T) {
	ct := NewCommitteeTracker(DefaultCommitteeTrackerConfig(), nil)
	_, err := ct.GetCommittee(1)
	if err != ErrTrackerNoValidators {
		t.Errorf("expected ErrTrackerNoValidators, got %v", err)
	}
}

func TestGetCommittee_Deterministic(t *testing.T) {
	vals := makeValidators(200)
	ct := NewCommitteeTracker(DefaultCommitteeTrackerConfig(), vals)

	c1, err := ct.GetCommittee(42)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := ct.GetCommittee(42)
	if err != nil {
		t.Fatal(err)
	}

	if len(c1.Members) != len(c2.Members) {
		t.Fatalf("committee sizes differ: %d vs %d", len(c1.Members), len(c2.Members))
	}
	for i := range c1.Members {
		if c1.Members[i] != c2.Members[i] {
			t.Errorf("member[%d]: %d != %d", i, c1.Members[i], c2.Members[i])
		}
	}
	if c1.Root != c2.Root {
		t.Error("committee roots differ")
	}
}

func TestGetCommittee_Size(t *testing.T) {
	vals := makeValidators(200)
	ct := NewCommitteeTracker(DefaultCommitteeTrackerConfig(), vals)

	c, err := ct.GetCommittee(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Members) != IL_COMMITTEE_SIZE {
		t.Errorf("committee size = %d, want %d", len(c.Members), IL_COMMITTEE_SIZE)
	}
}

func TestGetCommittee_SizeCapped(t *testing.T) {
	// When fewer validators than committee size, all are selected.
	vals := makeValidators(5)
	ct := NewCommitteeTracker(DefaultCommitteeTrackerConfig(), vals)

	c, err := ct.GetCommittee(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Members) != 5 {
		t.Errorf("committee size = %d, want 5 (capped to validator count)", len(c.Members))
	}
}

func TestGetCommittee_Sorted(t *testing.T) {
	vals := makeValidators(200)
	ct := NewCommitteeTracker(DefaultCommitteeTrackerConfig(), vals)

	c, err := ct.GetCommittee(10)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(c.Members); i++ {
		if c.Members[i] <= c.Members[i-1] {
			t.Fatalf("members not sorted at index %d: %d <= %d",
				i, c.Members[i], c.Members[i-1])
		}
	}
}

func TestGetCommittee_DifferentSlots(t *testing.T) {
	vals := makeValidators(200)
	ct := NewCommitteeTracker(DefaultCommitteeTrackerConfig(), vals)

	c1, _ := ct.GetCommittee(1)
	c2, _ := ct.GetCommittee(2)

	same := true
	if len(c1.Members) == len(c2.Members) {
		for i := range c1.Members {
			if c1.Members[i] != c2.Members[i] {
				same = false
				break
			}
		}
	} else {
		same = false
	}
	if same {
		t.Error("different slots produced identical committees")
	}
}

func TestIsCommitteeMember(t *testing.T) {
	vals := makeValidators(200)
	ct := NewCommitteeTracker(DefaultCommitteeTrackerConfig(), vals)

	c, _ := ct.GetCommittee(5)
	// All listed members should be recognized.
	for _, idx := range c.Members {
		if !ct.IsCommitteeMember(idx, 5) {
			t.Errorf("IsCommitteeMember(%d, 5) = false, want true", idx)
		}
	}
	// A validator not in the committee.
	nonMember := findNonMember(c.Members, 200)
	if ct.IsCommitteeMember(nonMember, 5) {
		t.Errorf("IsCommitteeMember(%d, 5) = true, want false", nonMember)
	}
}

func TestGetDuty_Success(t *testing.T) {
	vals := makeValidators(200)
	ct := NewCommitteeTracker(DefaultCommitteeTrackerConfig(), vals)

	c, _ := ct.GetCommittee(3)
	duty, err := ct.GetDuty(c.Members[0], 3)
	if err != nil {
		t.Fatalf("GetDuty: %v", err)
	}
	if duty.Slot != 3 {
		t.Errorf("duty slot = %d, want 3", duty.Slot)
	}
	if duty.ValidatorIndex != c.Members[0] {
		t.Errorf("duty validator = %d, want %d", duty.ValidatorIndex, c.Members[0])
	}
	if duty.CommitteePosition != 0 {
		t.Errorf("duty position = %d, want 0", duty.CommitteePosition)
	}
}

func TestGetDuty_NotMember(t *testing.T) {
	vals := makeValidators(200)
	ct := NewCommitteeTracker(DefaultCommitteeTrackerConfig(), vals)

	c, _ := ct.GetCommittee(3)
	nonMember := findNonMember(c.Members, 200)
	_, err := ct.GetDuty(nonMember, 3)
	if !errors.Is(err, ErrTrackerNotCommittee) {
		t.Errorf("expected ErrTrackerNotCommittee, got %v", err)
	}
}

func TestRecordList_Success(t *testing.T) {
	vals := makeValidators(200)
	ct := NewCommitteeTracker(DefaultCommitteeTrackerConfig(), vals)

	c, _ := ct.GetCommittee(1)
	err := ct.RecordList(c.Members[0], 1)
	if err != nil {
		t.Fatalf("RecordList: %v", err)
	}
}

func TestRecordList_SlotZero(t *testing.T) {
	vals := makeValidators(200)
	ct := NewCommitteeTracker(DefaultCommitteeTrackerConfig(), vals)

	err := ct.RecordList(0, 0)
	if err != ErrTrackerSlotZero {
		t.Errorf("expected ErrTrackerSlotZero, got %v", err)
	}
}

func TestRecordList_NotMember(t *testing.T) {
	vals := makeValidators(200)
	ct := NewCommitteeTracker(DefaultCommitteeTrackerConfig(), vals)

	c, _ := ct.GetCommittee(1)
	nonMember := findNonMember(c.Members, 200)
	err := ct.RecordList(nonMember, 1)
	if !errors.Is(err, ErrTrackerNotCommittee) {
		t.Errorf("expected ErrTrackerNotCommittee, got %v", err)
	}
}

func TestRecordList_Duplicate(t *testing.T) {
	vals := makeValidators(200)
	ct := NewCommitteeTracker(DefaultCommitteeTrackerConfig(), vals)

	c, _ := ct.GetCommittee(1)
	ct.RecordList(c.Members[0], 1)
	err := ct.RecordList(c.Members[0], 1)
	if !errors.Is(err, ErrTrackerDuplicateList) {
		t.Errorf("expected ErrTrackerDuplicateList, got %v", err)
	}
}

func TestGetQuorumStatus_NoLists(t *testing.T) {
	vals := makeValidators(200)
	ct := NewCommitteeTracker(DefaultCommitteeTrackerConfig(), vals)

	status, err := ct.GetQuorumStatus(1)
	if err != nil {
		t.Fatal(err)
	}
	if status.ListsReceived != 0 {
		t.Errorf("lists received = %d, want 0", status.ListsReceived)
	}
	if status.QuorumReached {
		t.Error("quorum should not be reached with no lists")
	}
	if status.CommitteeSize != IL_COMMITTEE_SIZE {
		t.Errorf("committee size = %d, want %d", status.CommitteeSize, IL_COMMITTEE_SIZE)
	}
}

func TestQuorumThreshold(t *testing.T) {
	tests := []struct {
		size      int
		threshold int
	}{
		{16, 11}, // ceil(16*2/3) = ceil(10.67) = 11
		{3, 2},   // ceil(3*2/3) = ceil(2) = 2
		{1, 1},   // ceil(1*2/3) = ceil(0.67) = 1
		{0, 0},
	}
	for _, tt := range tests {
		got := quorumThreshold(tt.size)
		if got != tt.threshold {
			t.Errorf("quorumThreshold(%d) = %d, want %d", tt.size, got, tt.threshold)
		}
	}
}

func TestCheckQuorum_NotReached(t *testing.T) {
	vals := makeValidators(200)
	ct := NewCommitteeTracker(DefaultCommitteeTrackerConfig(), vals)

	err := ct.CheckQuorum(1)
	if !errors.Is(err, ErrTrackerQuorumNotReached) {
		t.Errorf("expected ErrTrackerQuorumNotReached, got %v", err)
	}
}

func TestCheckQuorum_Reached(t *testing.T) {
	vals := makeValidators(200)
	ct := NewCommitteeTracker(DefaultCommitteeTrackerConfig(), vals)

	c, _ := ct.GetCommittee(1)
	threshold := quorumThreshold(len(c.Members))

	// Submit enough lists to reach quorum.
	for i := 0; i < threshold; i++ {
		if err := ct.RecordList(c.Members[i], 1); err != nil {
			t.Fatalf("RecordList(%d): %v", c.Members[i], err)
		}
	}

	err := ct.CheckQuorum(1)
	if err != nil {
		t.Errorf("CheckQuorum should pass: %v", err)
	}

	status, _ := ct.GetQuorumStatus(1)
	if !status.QuorumReached {
		t.Error("QuorumReached should be true")
	}
	if status.ListsReceived != threshold {
		t.Errorf("lists received = %d, want %d", status.ListsReceived, threshold)
	}
}

func TestGetQuorumStatus_SubmittedBy(t *testing.T) {
	vals := makeValidators(200)
	ct := NewCommitteeTracker(DefaultCommitteeTrackerConfig(), vals)

	c, _ := ct.GetCommittee(1)
	ct.RecordList(c.Members[0], 1)
	ct.RecordList(c.Members[1], 1)

	status, _ := ct.GetQuorumStatus(1)
	if len(status.SubmittedBy) != 2 {
		t.Fatalf("submitted by = %d, want 2", len(status.SubmittedBy))
	}
	// SubmittedBy should be sorted.
	if status.SubmittedBy[0] > status.SubmittedBy[1] {
		t.Error("SubmittedBy should be sorted")
	}
}

func TestPruneBefore(t *testing.T) {
	vals := makeValidators(200)
	ct := NewCommitteeTracker(DefaultCommitteeTrackerConfig(), vals)

	// Cache committees and record lists for slots 1-10.
	for slot := uint64(1); slot <= 10; slot++ {
		ct.GetCommittee(slot)
		c, _ := ct.GetCommittee(slot)
		ct.RecordList(c.Members[0], slot)
	}

	pruned := ct.PruneBefore(6)
	if pruned != 5 {
		t.Errorf("pruned = %d, want 5", pruned)
	}

	// Slot 6 committee should still be cached.
	c, err := ct.GetCommittee(6)
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("committee for slot 6 should still exist")
	}
}

func TestCommitteeRoot_Deterministic(t *testing.T) {
	vals := makeValidators(200)
	ct := NewCommitteeTracker(DefaultCommitteeTrackerConfig(), vals)

	c1, _ := ct.GetCommittee(7)
	root1 := c1.Root

	// Create a second tracker with same validators.
	ct2 := NewCommitteeTracker(DefaultCommitteeTrackerConfig(), vals)
	c2, _ := ct2.GetCommittee(7)
	root2 := c2.Root

	if root1 != root2 {
		t.Errorf("committee roots differ for same inputs")
	}
}

func TestCommitteeRotation(t *testing.T) {
	vals := makeValidators(200)
	ct := NewCommitteeTracker(DefaultCommitteeTrackerConfig(), vals)

	// Verify that across many slots, different validators are selected.
	allMembers := make(map[uint64]bool)
	for slot := uint64(1); slot <= 50; slot++ {
		c, err := ct.GetCommittee(slot)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range c.Members {
			allMembers[m] = true
		}
	}
	// With 200 validators and 50 slots of 16 members each,
	// we expect many distinct validators to appear.
	if len(allMembers) < 50 {
		t.Errorf("expected many distinct validators across 50 slots, got %d", len(allMembers))
	}
}

// findNonMember returns a validator index in [0, totalValidators) that is not
// in the given members list.
func findNonMember(members []uint64, totalValidators int) uint64 {
	set := make(map[uint64]bool, len(members))
	for _, m := range members {
		set[m] = true
	}
	for i := uint64(0); i < uint64(totalValidators); i++ {
		if !set[i] {
			return i
		}
	}
	return uint64(totalValidators) // fallback; should not happen
}
