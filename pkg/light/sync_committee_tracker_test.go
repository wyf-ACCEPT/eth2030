package light

import (
	"sync"
	"testing"
)

func makeTestCommitteeMembers(n int, offset uint64) []*CommitteeMember {
	members := make([]*CommitteeMember, n)
	for i := 0; i < n; i++ {
		var pk [48]byte
		pk[0] = byte(offset)
		pk[1] = byte(i)
		members[i] = &CommitteeMember{
			ValidatorIndex: offset + uint64(i),
			Pubkey:         pk,
			Weight:         32_000_000_000, // 32 ETH in Gwei
		}
	}
	return members
}

func makeSCTrackerBits(n, total int) []byte {
	bits := make([]byte, (total+7)/8)
	for i := 0; i < n && i < total; i++ {
		bits[i/8] |= 1 << (uint(i) % 8)
	}
	return bits
}

func TestSCTrackerNew(t *testing.T) {
	tracker := NewSyncCommitteeTracker(4)
	if tracker == nil {
		t.Fatal("expected non-nil tracker")
	}
	if tracker.maxPeriods != 4 {
		t.Errorf("maxPeriods = %d, want 4", tracker.maxPeriods)
	}
	if tracker.PeriodCount() != 0 {
		t.Errorf("period count = %d, want 0", tracker.PeriodCount())
	}
}

func TestSCTrackerNewDefaultMaxPeriods(t *testing.T) {
	tracker := NewSyncCommitteeTracker(0)
	if tracker.maxPeriods != 8 {
		t.Errorf("maxPeriods = %d, want 8 (default)", tracker.maxPeriods)
	}
}

func TestSCTrackerRegisterCommittee(t *testing.T) {
	tracker := NewSyncCommitteeTracker(8)
	members := makeTestCommitteeMembers(512, 0)

	if err := tracker.RegisterCommittee(10, members); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tracker.PeriodCount() != 1 {
		t.Errorf("period count = %d, want 1", tracker.PeriodCount())
	}
	if tracker.CurrentPeriod() != 10 {
		t.Errorf("current period = %d, want 10", tracker.CurrentPeriod())
	}
}

func TestSCTrackerRegisterCommitteeNilMembers(t *testing.T) {
	tracker := NewSyncCommitteeTracker(8)
	if err := tracker.RegisterCommittee(0, nil); err != ErrSCTrackerNilMembers {
		t.Errorf("expected ErrSCTrackerNilMembers, got %v", err)
	}
}

func TestSCTrackerRegisterCommitteeEmpty(t *testing.T) {
	tracker := NewSyncCommitteeTracker(8)
	if err := tracker.RegisterCommittee(0, []*CommitteeMember{}); err != ErrSCTrackerEmptyCommittee {
		t.Errorf("expected ErrSCTrackerEmptyCommittee, got %v", err)
	}
}

func TestSCTrackerGetCommittee(t *testing.T) {
	tracker := NewSyncCommitteeTracker(8)
	members := makeTestCommitteeMembers(16, 100)
	tracker.RegisterCommittee(5, members)

	got, err := tracker.GetCommittee(5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 16 {
		t.Errorf("committee size = %d, want 16", len(got))
	}
	// Verify it's a copy, not a reference.
	got[0].Weight = 999
	original, _ := tracker.GetCommittee(5)
	if original[0].Weight == 999 {
		t.Error("GetCommittee should return a copy, not a reference")
	}
}

func TestSCTrackerGetCommitteePeriodNotFound(t *testing.T) {
	tracker := NewSyncCommitteeTracker(8)
	_, err := tracker.GetCommittee(999)
	if err != ErrSCTrackerPeriodNotFound {
		t.Errorf("expected ErrSCTrackerPeriodNotFound, got %v", err)
	}
}

func TestSCTrackerCurrentPeriod(t *testing.T) {
	tracker := NewSyncCommitteeTracker(8)
	if tracker.CurrentPeriod() != 0 {
		t.Errorf("initial current period = %d, want 0", tracker.CurrentPeriod())
	}

	tracker.RegisterCommittee(5, makeTestCommitteeMembers(16, 0))
	if tracker.CurrentPeriod() != 5 {
		t.Errorf("current period = %d, want 5", tracker.CurrentPeriod())
	}

	tracker.RegisterCommittee(10, makeTestCommitteeMembers(16, 100))
	if tracker.CurrentPeriod() != 10 {
		t.Errorf("current period = %d, want 10", tracker.CurrentPeriod())
	}

	// Registering a lower period should not decrease current.
	tracker.RegisterCommittee(3, makeTestCommitteeMembers(16, 200))
	if tracker.CurrentPeriod() != 10 {
		t.Errorf("current period after lower register = %d, want 10", tracker.CurrentPeriod())
	}
}

func TestSCTrackerValidateUpdate(t *testing.T) {
	tracker := NewSyncCommitteeTracker(8)
	committeeSize := 512
	members := makeTestCommitteeMembers(committeeSize, 0)
	period := uint64(0)
	tracker.RegisterCommittee(period, members)

	// Attested slot 100 => period 0 (100/8192 = 0).
	bits := makeSCTrackerBits(400, committeeSize) // 400/512 > 2/3
	update := &SyncUpdate{
		AttestedSlot:      100,
		FinalizedSlot:     50,
		SyncCommitteeBits: bits,
		Signature:         [96]byte{},
	}

	if err := tracker.ValidateUpdate(update); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSCTrackerValidateUpdateNil(t *testing.T) {
	tracker := NewSyncCommitteeTracker(8)
	if err := tracker.ValidateUpdate(nil); err != ErrSCTrackerInvalidUpdate {
		t.Errorf("expected ErrSCTrackerInvalidUpdate, got %v", err)
	}
}

func TestSCTrackerValidateUpdateFinalityExceedsAttested(t *testing.T) {
	tracker := NewSyncCommitteeTracker(8)
	tracker.RegisterCommittee(0, makeTestCommitteeMembers(512, 0))

	update := &SyncUpdate{
		AttestedSlot:      50,
		FinalizedSlot:     100, // finalized > attested is invalid
		SyncCommitteeBits: makeSCTrackerBits(400, 512),
	}
	if err := tracker.ValidateUpdate(update); err != ErrSCTrackerNoFinality {
		t.Errorf("expected ErrSCTrackerNoFinality, got %v", err)
	}
}

func TestSCTrackerValidateUpdateInsufficientSigs(t *testing.T) {
	tracker := NewSyncCommitteeTracker(8)
	committeeSize := 512
	tracker.RegisterCommittee(0, makeTestCommitteeMembers(committeeSize, 0))

	// Only 100 of 512 signed => 100*3=300 < 512*2=1024.
	bits := makeSCTrackerBits(100, committeeSize)
	update := &SyncUpdate{
		AttestedSlot:      100,
		FinalizedSlot:     50,
		SyncCommitteeBits: bits,
	}
	if err := tracker.ValidateUpdate(update); err != ErrSCTrackerInsufficientSig {
		t.Errorf("expected ErrSCTrackerInsufficientSig, got %v", err)
	}
}

func TestSCTrackerValidateUpdatePeriodNotFound(t *testing.T) {
	tracker := NewSyncCommitteeTracker(8)
	// No period registered.
	update := &SyncUpdate{
		AttestedSlot:      100,
		FinalizedSlot:     50,
		SyncCommitteeBits: makeSCTrackerBits(400, 512),
	}
	if err := tracker.ValidateUpdate(update); err != ErrSCTrackerPeriodNotFound {
		t.Errorf("expected ErrSCTrackerPeriodNotFound, got %v", err)
	}
}

func TestSCTrackerValidateUpdateEmptyNextCommittee(t *testing.T) {
	tracker := NewSyncCommitteeTracker(8)
	tracker.RegisterCommittee(0, makeTestCommitteeMembers(512, 0))

	update := &SyncUpdate{
		AttestedSlot:      100,
		FinalizedSlot:     50,
		SyncCommitteeBits: makeSCTrackerBits(400, 512),
		NextCommittee:     []*CommitteeMember{}, // non-nil but empty
	}
	if err := tracker.ValidateUpdate(update); err != ErrSCTrackerEmptyCommittee {
		t.Errorf("expected ErrSCTrackerEmptyCommittee, got %v", err)
	}
}

func TestSCTrackerParticipationRate(t *testing.T) {
	tracker := NewSyncCommitteeTracker(8)
	committeeSize := 100
	members := makeTestCommitteeMembers(committeeSize, 0)
	tracker.RegisterCommittee(0, members)

	// Record 75 of 100 participating.
	bits := makeSCTrackerBits(75, committeeSize)
	tracker.UpdateParticipation(0, bits)

	rate := tracker.ParticipationRate(0)
	expected := 75.0 / 100.0
	if rate != expected {
		t.Errorf("participation rate = %f, want %f", rate, expected)
	}
}

func TestSCTrackerParticipationRateUnknownPeriod(t *testing.T) {
	tracker := NewSyncCommitteeTracker(8)
	rate := tracker.ParticipationRate(999)
	if rate != 0.0 {
		t.Errorf("expected 0.0 for unknown period, got %f", rate)
	}
}

func TestSCTrackerUpdateParticipationOR(t *testing.T) {
	tracker := NewSyncCommitteeTracker(8)
	committeeSize := 16
	tracker.RegisterCommittee(0, makeTestCommitteeMembers(committeeSize, 0))

	// First batch: members 0-7.
	bits1 := makeSCTrackerBits(8, committeeSize)
	tracker.UpdateParticipation(0, bits1)

	// Second batch: members 4-11 (overlapping with first).
	bits2 := make([]byte, (committeeSize+7)/8)
	for i := 4; i < 12; i++ {
		bits2[i/8] |= 1 << (uint(i) % 8)
	}
	tracker.UpdateParticipation(0, bits2)

	// Should have members 0-11 participating (12 total).
	rate := tracker.ParticipationRate(0)
	expected := 12.0 / 16.0
	if rate != expected {
		t.Errorf("participation rate after OR = %f, want %f", rate, expected)
	}
}

func TestSCTrackerUpdateParticipationUnknownPeriod(t *testing.T) {
	tracker := NewSyncCommitteeTracker(8)
	// Should not panic for unknown period.
	tracker.UpdateParticipation(999, makeSCTrackerBits(10, 100))
}

func TestSCTrackerPruneBefore(t *testing.T) {
	tracker := NewSyncCommitteeTracker(100)
	for i := uint64(0); i < 10; i++ {
		tracker.RegisterCommittee(i, makeTestCommitteeMembers(16, i*100))
	}
	if tracker.PeriodCount() != 10 {
		t.Fatalf("period count = %d, want 10", tracker.PeriodCount())
	}

	pruned := tracker.PruneBefore(5)
	if pruned != 5 {
		t.Errorf("pruned = %d, want 5", pruned)
	}
	if tracker.PeriodCount() != 5 {
		t.Errorf("period count after prune = %d, want 5", tracker.PeriodCount())
	}

	// Periods 0-4 should be gone.
	for i := uint64(0); i < 5; i++ {
		if _, err := tracker.GetCommittee(i); err != ErrSCTrackerPeriodNotFound {
			t.Errorf("period %d should be pruned", i)
		}
	}
	// Periods 5-9 should remain.
	for i := uint64(5); i < 10; i++ {
		if _, err := tracker.GetCommittee(i); err != nil {
			t.Errorf("period %d should exist: %v", i, err)
		}
	}
}

func TestSCTrackerStats(t *testing.T) {
	tracker := NewSyncCommitteeTracker(8)

	// Register two periods with different sizes.
	tracker.RegisterCommittee(0, makeTestCommitteeMembers(100, 0))
	tracker.RegisterCommittee(1, makeTestCommitteeMembers(200, 1000))

	// Record participation for period 0: 80 of 100.
	tracker.UpdateParticipation(0, makeSCTrackerBits(80, 100))

	// Record participation for period 1: 150 of 200.
	tracker.UpdateParticipation(1, makeSCTrackerBits(150, 200))

	stats := tracker.Stats()
	if stats.Periods != 2 {
		t.Errorf("Periods = %d, want 2", stats.Periods)
	}
	if stats.TotalMembers != 300 {
		t.Errorf("TotalMembers = %d, want 300", stats.TotalMembers)
	}

	// Average participation: (80/100 + 150/200) / 2 = (0.8 + 0.75) / 2 = 0.775.
	expectedAvg := (0.8 + 0.75) / 2.0
	if stats.AvgParticipation < expectedAvg-0.001 || stats.AvgParticipation > expectedAvg+0.001 {
		t.Errorf("AvgParticipation = %f, want ~%f", stats.AvgParticipation, expectedAvg)
	}
}

func TestSCTrackerStatsEmpty(t *testing.T) {
	tracker := NewSyncCommitteeTracker(8)
	stats := tracker.Stats()
	if stats.Periods != 0 {
		t.Errorf("Periods = %d, want 0", stats.Periods)
	}
	if stats.TotalMembers != 0 {
		t.Errorf("TotalMembers = %d, want 0", stats.TotalMembers)
	}
	if stats.AvgParticipation != 0.0 {
		t.Errorf("AvgParticipation = %f, want 0.0", stats.AvgParticipation)
	}
}

func TestSCTrackerDuplicateRegister(t *testing.T) {
	tracker := NewSyncCommitteeTracker(8)
	members1 := makeTestCommitteeMembers(16, 0)
	members2 := makeTestCommitteeMembers(32, 100)

	tracker.RegisterCommittee(5, members1)
	if got, _ := tracker.GetCommittee(5); len(got) != 16 {
		t.Fatalf("initial committee size = %d, want 16", len(got))
	}

	// Overwrite with larger committee.
	tracker.RegisterCommittee(5, members2)
	got, _ := tracker.GetCommittee(5)
	if len(got) != 32 {
		t.Errorf("overwritten committee size = %d, want 32", len(got))
	}
}

func TestSCTrackerEvictionOnCapacity(t *testing.T) {
	tracker := NewSyncCommitteeTracker(3) // keep only 3 periods

	for i := uint64(0); i < 5; i++ {
		tracker.RegisterCommittee(i, makeTestCommitteeMembers(16, i*100))
	}

	if tracker.PeriodCount() != 3 {
		t.Errorf("period count = %d, want 3", tracker.PeriodCount())
	}

	// Oldest periods (0, 1) should be evicted.
	for i := uint64(0); i < 2; i++ {
		if _, err := tracker.GetCommittee(i); err != ErrSCTrackerPeriodNotFound {
			t.Errorf("period %d should be evicted", i)
		}
	}
	// Periods 2, 3, 4 should remain.
	for i := uint64(2); i < 5; i++ {
		if _, err := tracker.GetCommittee(i); err != nil {
			t.Errorf("period %d should exist: %v", i, err)
		}
	}
}

func TestSCTrackerConcurrentAccess(t *testing.T) {
	tracker := NewSyncCommitteeTracker(16)
	var wg sync.WaitGroup

	// Concurrent writers.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			period := uint64(idx)
			members := makeTestCommitteeMembers(16, period*100)
			tracker.RegisterCommittee(period, members)
			tracker.UpdateParticipation(period, makeSCTrackerBits(10, 16))
		}(i)
	}

	// Concurrent readers.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tracker.GetCommittee(uint64(idx))
			tracker.CurrentPeriod()
			tracker.ParticipationRate(uint64(idx))
			tracker.Stats()
			tracker.PeriodCount()
		}(i)
	}

	wg.Wait()
}

func TestSCTrackerMultiPeriod(t *testing.T) {
	tracker := NewSyncCommitteeTracker(100)

	// Register 10 periods with increasing committee sizes.
	for i := uint64(0); i < 10; i++ {
		size := int(16 + i*8) // 16, 24, 32, ..., 88
		tracker.RegisterCommittee(i, makeTestCommitteeMembers(size, i*1000))
	}

	if tracker.PeriodCount() != 10 {
		t.Fatalf("period count = %d, want 10", tracker.PeriodCount())
	}
	if tracker.CurrentPeriod() != 9 {
		t.Errorf("current period = %d, want 9", tracker.CurrentPeriod())
	}

	// Verify each period has the right size.
	for i := uint64(0); i < 10; i++ {
		members, err := tracker.GetCommittee(i)
		if err != nil {
			t.Errorf("period %d: unexpected error: %v", i, err)
			continue
		}
		expectedSize := int(16 + i*8)
		if len(members) != expectedSize {
			t.Errorf("period %d: size = %d, want %d", i, len(members), expectedSize)
		}
	}

	stats := tracker.Stats()
	// Total: 16+24+32+40+48+56+64+72+80+88 = 520
	if stats.TotalMembers != 520 {
		t.Errorf("TotalMembers = %d, want 520", stats.TotalMembers)
	}
}

func TestSCTrackerLargePeriod(t *testing.T) {
	tracker := NewSyncCommitteeTracker(8)
	largePeriod := uint64(1_000_000)
	members := makeTestCommitteeMembers(512, 0)

	if err := tracker.RegisterCommittee(largePeriod, members); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tracker.CurrentPeriod() != largePeriod {
		t.Errorf("current period = %d, want %d", tracker.CurrentPeriod(), largePeriod)
	}

	got, err := tracker.GetCommittee(largePeriod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 512 {
		t.Errorf("committee size = %d, want 512", len(got))
	}
}

func TestSCTrackerPruneBeforeEmpty(t *testing.T) {
	tracker := NewSyncCommitteeTracker(8)
	pruned := tracker.PruneBefore(100)
	if pruned != 0 {
		t.Errorf("pruned = %d, want 0", pruned)
	}
}
