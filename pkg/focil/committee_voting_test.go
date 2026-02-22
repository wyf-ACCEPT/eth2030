package focil

import (
	"errors"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// --- CommitteeVoting creation ---

func TestNewCommitteeVoting(t *testing.T) {
	cv := NewCommitteeVoting(DefaultCommitteeVotingConfig(), 1000)
	if cv == nil {
		t.Fatal("NewCommitteeVoting returned nil")
	}
}

func TestNewCommitteeVotingDefaults(t *testing.T) {
	// Zero config fields should be replaced with defaults.
	cv := NewCommitteeVoting(CommitteeVotingConfig{}, 100)
	if cv.config.CommitteeSize != IL_COMMITTEE_SIZE {
		t.Errorf("CommitteeSize = %d, want %d", cv.config.CommitteeSize, IL_COMMITTEE_SIZE)
	}
	if cv.config.QuorumNumerator != QuorumNumerator {
		t.Errorf("QuorumNumerator = %d, want %d", cv.config.QuorumNumerator, QuorumNumerator)
	}
	if cv.config.SlotsPerEpoch != SlotsPerEpoch {
		t.Errorf("SlotsPerEpoch = %d, want %d", cv.config.SlotsPerEpoch, SlotsPerEpoch)
	}
}

// --- ComputeILCommittee ---

func TestComputeILCommittee(t *testing.T) {
	cv := NewCommitteeVoting(DefaultCommitteeVotingConfig(), 1000)
	seed := [32]byte{0x01, 0x02, 0x03}

	members, err := cv.ComputeILCommittee(1, 1, seed, 1000)
	if err != nil {
		t.Fatalf("ComputeILCommittee: %v", err)
	}
	if len(members) != IL_COMMITTEE_SIZE {
		t.Errorf("committee size = %d, want %d", len(members), IL_COMMITTEE_SIZE)
	}

	// Verify sorted order.
	for i := 1; i < len(members); i++ {
		if members[i] <= members[i-1] {
			t.Errorf("committee not sorted: members[%d]=%d <= members[%d]=%d",
				i, members[i], i-1, members[i-1])
		}
	}

	// Verify all members are within range.
	for _, m := range members {
		if m >= 1000 {
			t.Errorf("member %d out of range [0, 1000)", m)
		}
	}
}

func TestComputeILCommitteeDeterministic(t *testing.T) {
	cv := NewCommitteeVoting(DefaultCommitteeVotingConfig(), 1000)
	seed := [32]byte{0xaa, 0xbb, 0xcc}

	// Same inputs should produce identical committees.
	// Use separate instances to avoid cache effects.
	cv2 := NewCommitteeVoting(DefaultCommitteeVotingConfig(), 1000)

	m1, err1 := cv.ComputeILCommittee(5, 100, seed, 1000)
	m2, err2 := cv2.ComputeILCommittee(5, 100, seed, 1000)

	if err1 != nil || err2 != nil {
		t.Fatalf("errors: %v, %v", err1, err2)
	}
	if len(m1) != len(m2) {
		t.Fatalf("lengths differ: %d vs %d", len(m1), len(m2))
	}
	for i := range m1 {
		if m1[i] != m2[i] {
			t.Errorf("member[%d]: %d != %d", i, m1[i], m2[i])
		}
	}
}

func TestComputeILCommitteeDifferentSeeds(t *testing.T) {
	cv1 := NewCommitteeVoting(DefaultCommitteeVotingConfig(), 1000)
	cv2 := NewCommitteeVoting(DefaultCommitteeVotingConfig(), 1000)

	seed1 := [32]byte{0x01}
	seed2 := [32]byte{0x02}

	m1, _ := cv1.ComputeILCommittee(1, 1, seed1, 1000)
	m2, _ := cv2.ComputeILCommittee(1, 1, seed2, 1000)

	// Different seeds should (almost certainly) produce different committees.
	same := true
	for i := range m1 {
		if m1[i] != m2[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("different seeds produced identical committees")
	}
}

func TestComputeILCommitteeSmallValidatorSet(t *testing.T) {
	// Fewer validators than committee size.
	cv := NewCommitteeVoting(DefaultCommitteeVotingConfig(), 5)
	seed := [32]byte{0x01}

	members, err := cv.ComputeILCommittee(1, 1, seed, 5)
	if err != nil {
		t.Fatalf("ComputeILCommittee: %v", err)
	}
	if len(members) != 5 {
		t.Errorf("committee size = %d, want 5 (capped at validator count)", len(members))
	}
}

func TestComputeILCommitteeZeroSlot(t *testing.T) {
	cv := NewCommitteeVoting(DefaultCommitteeVotingConfig(), 100)
	_, err := cv.ComputeILCommittee(1, 0, [32]byte{}, 100)
	if !errors.Is(err, ErrVotingSlotZero) {
		t.Errorf("slot 0: got %v, want ErrVotingSlotZero", err)
	}
}

func TestComputeILCommitteeZeroEpoch(t *testing.T) {
	cv := NewCommitteeVoting(DefaultCommitteeVotingConfig(), 100)
	_, err := cv.ComputeILCommittee(0, 1, [32]byte{}, 100)
	if !errors.Is(err, ErrVotingEpochZero) {
		t.Errorf("epoch 0: got %v, want ErrVotingEpochZero", err)
	}
}

func TestComputeILCommitteeNoValidators(t *testing.T) {
	cv := NewCommitteeVoting(DefaultCommitteeVotingConfig(), 0)
	_, err := cv.ComputeILCommittee(1, 1, [32]byte{}, 0)
	if !errors.Is(err, ErrVotingNoValidators) {
		t.Errorf("no validators: got %v, want ErrVotingNoValidators", err)
	}
}

func TestComputeILCommitteeCaching(t *testing.T) {
	cv := NewCommitteeVoting(DefaultCommitteeVotingConfig(), 100)
	seed := [32]byte{0x11}

	m1, _ := cv.ComputeILCommittee(1, 5, seed, 100)
	m2, _ := cv.ComputeILCommittee(1, 5, seed, 100) // should hit cache

	if len(m1) != len(m2) {
		t.Fatalf("cached result differs in length: %d vs %d", len(m1), len(m2))
	}
	for i := range m1 {
		if m1[i] != m2[i] {
			t.Errorf("cached member[%d] differs: %d vs %d", i, m1[i], m2[i])
		}
	}
}

// --- IsILCommitteeMember ---

func TestIsILCommitteeMember(t *testing.T) {
	cv := NewCommitteeVoting(DefaultCommitteeVotingConfig(), 1000)
	seed := [32]byte{0x42}

	members, _ := cv.ComputeILCommittee(1, 10, seed, 1000)

	// Each member should be found.
	for _, m := range members {
		if !cv.IsILCommitteeMember(m, 10) {
			t.Errorf("member %d should be in committee for slot 10", m)
		}
	}

	// A slot with no cached committee should return false.
	if cv.IsILCommitteeMember(0, 999) {
		t.Error("uncached slot should return false")
	}
}

// --- TrackSubmission ---

func TestTrackSubmission(t *testing.T) {
	cv := NewCommitteeVoting(DefaultCommitteeVotingConfig(), 1000)
	seed := [32]byte{0x55}
	members, _ := cv.ComputeILCommittee(1, 20, seed, 1000)

	ilHash := types.HexToHash("0xabcd")
	err := cv.TrackSubmission(members[0], 20, ilHash)
	if err != nil {
		t.Fatalf("TrackSubmission: %v", err)
	}

	// Duplicate should fail.
	err = cv.TrackSubmission(members[0], 20, ilHash)
	if !errors.Is(err, ErrVotingDuplicateSubmit) {
		t.Errorf("duplicate: got %v, want ErrVotingDuplicateSubmit", err)
	}
}

func TestTrackSubmissionNonMember(t *testing.T) {
	cv := NewCommitteeVoting(DefaultCommitteeVotingConfig(), 1000)
	seed := [32]byte{0x55}
	members, _ := cv.ComputeILCommittee(1, 20, seed, 1000)

	// Find an index not in the committee.
	memberSet := make(map[uint64]bool)
	for _, m := range members {
		memberSet[m] = true
	}
	var nonMember uint64
	for i := uint64(0); i < 1000; i++ {
		if !memberSet[i] {
			nonMember = i
			break
		}
	}

	err := cv.TrackSubmission(nonMember, 20, types.Hash{})
	if !errors.Is(err, ErrVotingNotMember) {
		t.Errorf("non-member: got %v, want ErrVotingNotMember", err)
	}
}

func TestTrackSubmissionSlotZero(t *testing.T) {
	cv := NewCommitteeVoting(DefaultCommitteeVotingConfig(), 100)
	err := cv.TrackSubmission(0, 0, types.Hash{})
	if !errors.Is(err, ErrVotingSlotZero) {
		t.Errorf("slot 0: got %v, want ErrVotingSlotZero", err)
	}
}

// --- GetMissingSubmitters ---

func TestGetMissingSubmitters(t *testing.T) {
	cv := NewCommitteeVoting(DefaultCommitteeVotingConfig(), 1000)
	seed := [32]byte{0x77}
	members, _ := cv.ComputeILCommittee(1, 30, seed, 1000)

	// Submit from first member only.
	cv.TrackSubmission(members[0], 30, types.Hash{0x01})

	missing := cv.GetMissingSubmitters(30)
	if len(missing) != len(members)-1 {
		t.Errorf("missing = %d, want %d", len(missing), len(members)-1)
	}

	// The first member should not be in missing.
	for _, m := range missing {
		if m == members[0] {
			t.Error("submitted member should not be in missing list")
		}
	}
}

func TestGetMissingSubmittersUncachedSlot(t *testing.T) {
	cv := NewCommitteeVoting(DefaultCommitteeVotingConfig(), 100)
	missing := cv.GetMissingSubmitters(999)
	if missing != nil {
		t.Errorf("uncached slot: missing = %v, want nil", missing)
	}
}

// --- CommitteeQuorum ---

func TestCommitteeQuorum(t *testing.T) {
	cfg := DefaultCommitteeVotingConfig()
	cfg.CommitteeSize = 3 // small committee for easier testing
	cv := NewCommitteeVoting(cfg, 100)
	seed := [32]byte{0x88}
	members, _ := cv.ComputeILCommittee(1, 40, seed, 100)

	// 0 of 3 submitted: quorum not reached (need ceil(2/3 * 3) = 2).
	if cv.CommitteeQuorum(40) {
		t.Error("0/3 should not reach quorum")
	}

	// 1 of 3 submitted.
	cv.TrackSubmission(members[0], 40, types.Hash{0x01})
	if cv.CommitteeQuorum(40) {
		t.Error("1/3 should not reach quorum")
	}

	// 2 of 3 submitted: quorum reached.
	cv.TrackSubmission(members[1], 40, types.Hash{0x02})
	if !cv.CommitteeQuorum(40) {
		t.Error("2/3 should reach quorum")
	}
}

func TestCommitteeQuorumUncached(t *testing.T) {
	cv := NewCommitteeVoting(DefaultCommitteeVotingConfig(), 100)
	if cv.CommitteeQuorum(999) {
		t.Error("uncached slot should not have quorum")
	}
}

// --- QuorumDetail ---

func TestQuorumDetail(t *testing.T) {
	cfg := DefaultCommitteeVotingConfig()
	cfg.CommitteeSize = 6
	cv := NewCommitteeVoting(cfg, 100)
	seed := [32]byte{0x99}
	members, _ := cv.ComputeILCommittee(1, 50, seed, 100)

	size, recv, thresh, reached := cv.QuorumDetail(50)
	if size != 6 {
		t.Errorf("size = %d, want 6", size)
	}
	if recv != 0 {
		t.Errorf("received = %d, want 0", recv)
	}
	// ceil(2/3 * 6) = 4
	if thresh != 4 {
		t.Errorf("threshold = %d, want 4", thresh)
	}
	if reached {
		t.Error("should not be reached with 0 submissions")
	}

	// Submit from 4 members.
	for i := 0; i < 4; i++ {
		cv.TrackSubmission(members[i], 50, types.Hash{byte(i)})
	}

	size, recv, thresh, reached = cv.QuorumDetail(50)
	if recv != 4 {
		t.Errorf("received = %d, want 4", recv)
	}
	if !reached {
		t.Error("should be reached with 4/6 submissions")
	}
}

// --- GetSubmissions ---

func TestGetSubmissions(t *testing.T) {
	cv := NewCommitteeVoting(DefaultCommitteeVotingConfig(), 1000)
	seed := [32]byte{0xaa}
	members, _ := cv.ComputeILCommittee(1, 60, seed, 1000)

	h1 := types.HexToHash("0x1111")
	h2 := types.HexToHash("0x2222")
	cv.TrackSubmission(members[0], 60, h1)
	cv.TrackSubmission(members[1], 60, h2)

	subs := cv.GetSubmissions(60)
	if len(subs) != 2 {
		t.Fatalf("submissions = %d, want 2", len(subs))
	}

	// Sorted by validator index.
	if subs[0].ValidatorIndex > subs[1].ValidatorIndex {
		t.Error("submissions not sorted by validator index")
	}
}

func TestGetSubmissionsEmpty(t *testing.T) {
	cv := NewCommitteeVoting(DefaultCommitteeVotingConfig(), 100)
	subs := cv.GetSubmissions(999)
	if subs != nil {
		t.Errorf("empty slot: submissions = %v, want nil", subs)
	}
}

// --- SlotToEpoch ---

func TestSlotToEpoch(t *testing.T) {
	cv := NewCommitteeVoting(DefaultCommitteeVotingConfig(), 100)

	tests := []struct {
		slot  uint64
		epoch uint64
	}{
		{0, 0},
		{1, 0},
		{31, 0},
		{32, 1},
		{33, 1},
		{63, 1},
		{64, 2},
		{320, 10},
	}
	for _, tt := range tests {
		if got := cv.SlotToEpoch(tt.slot); got != tt.epoch {
			t.Errorf("SlotToEpoch(%d) = %d, want %d", tt.slot, got, tt.epoch)
		}
	}
}

// --- PruneBefore ---

func TestVotingPruneBefore(t *testing.T) {
	cv := NewCommitteeVoting(DefaultCommitteeVotingConfig(), 100)
	seed := [32]byte{0xbb}

	// Compute committees for several slots.
	for slot := uint64(1); slot <= 5; slot++ {
		cv.ComputeILCommittee(1, slot, seed, 100)
	}

	pruned := cv.PruneBefore(4)
	if pruned != 3 {
		t.Errorf("pruned = %d, want 3", pruned)
	}

	// Slots 1-3 should be gone, 4-5 remain.
	if cv.IsILCommitteeMember(0, 1) {
		t.Error("slot 1 should be pruned")
	}

	cv.mu.RLock()
	_, has4 := cv.committeeCache[4]
	_, has5 := cv.committeeCache[5]
	cv.mu.RUnlock()

	if !has4 {
		t.Error("slot 4 should still be cached")
	}
	if !has5 {
		t.Error("slot 5 should still be cached")
	}
}
