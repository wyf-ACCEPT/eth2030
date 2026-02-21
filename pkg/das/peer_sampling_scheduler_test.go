package das

import (
	"sync"
	"testing"
	"time"
)

// --- helpers ---

func testPeers() []SamplingPeerInfo {
	return []SamplingPeerInfo{
		{
			ID:             "peer-A",
			Latency:        10 * time.Millisecond,
			CustodyColumns: map[uint64]bool{0: true, 1: true, 2: true, 3: true},
			SuccessRate:    0.95,
		},
		{
			ID:             "peer-B",
			Latency:        50 * time.Millisecond,
			CustodyColumns: map[uint64]bool{4: true, 5: true, 6: true, 7: true},
			SuccessRate:    0.80,
		},
		{
			ID:             "peer-C",
			Latency:        100 * time.Millisecond,
			CustodyColumns: map[uint64]bool{0: true, 4: true, 8: true, 12: true},
			SuccessRate:    0.70,
		},
		{
			ID:             "peer-D",
			Latency:        5 * time.Millisecond,
			CustodyColumns: map[uint64]bool{10: true, 11: true, 12: true, 13: true},
			SuccessRate:    0.99,
		},
	}
}

func testColumns(n int) []uint64 {
	cols := make([]uint64, n)
	for i := range cols {
		cols[i] = uint64(i)
	}
	return cols
}

// --- tests ---

func TestPeerSamplingScheduleCreation(t *testing.T) {
	ps := NewPeerSamplingScheduler(DefaultPeerSamplingConfig())
	peers := testPeers()
	columns := testColumns(8)

	plan, err := ps.ScheduleSampling(100, columns, peers)
	if err != nil {
		t.Fatalf("ScheduleSampling: %v", err)
	}
	if plan.Slot != 100 {
		t.Errorf("Slot = %d, want 100", plan.Slot)
	}
	if len(plan.Assignments) != 8 {
		t.Errorf("Assignments count = %d, want 8", len(plan.Assignments))
	}
	if plan.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

func TestPeerSamplingScheduleVariousColumnCounts(t *testing.T) {
	ps := NewPeerSamplingScheduler(DefaultPeerSamplingConfig())
	peers := testPeers()

	for _, n := range []int{1, 4, 16, 32} {
		columns := testColumns(n)
		plan, err := ps.ScheduleSampling(uint64(n), columns, peers)
		if err != nil {
			t.Fatalf("ScheduleSampling(%d columns): %v", n, err)
		}
		if len(plan.Assignments) != n {
			t.Errorf("columns=%d: got %d assignments, want %d", n, len(plan.Assignments), n)
		}
	}
}

func TestPeerSamplingAssignPeerByLatency(t *testing.T) {
	ps := NewPeerSamplingScheduler(DefaultPeerSamplingConfig())

	// Both peers custody column 0, peer-fast has lower latency.
	peers := []SamplingPeerInfo{
		{ID: "peer-slow", Latency: 200 * time.Millisecond, CustodyColumns: map[uint64]bool{0: true}, SuccessRate: 0.9},
		{ID: "peer-fast", Latency: 5 * time.Millisecond, CustodyColumns: map[uint64]bool{0: true}, SuccessRate: 0.9},
	}

	best := ps.AssignPeer(0, peers)
	if best.ID != "peer-fast" {
		t.Errorf("expected peer-fast (lower latency), got %s", best.ID)
	}
}

func TestPeerSamplingAssignPeerByCustody(t *testing.T) {
	ps := NewPeerSamplingScheduler(DefaultPeerSamplingConfig())

	// peer-custody has column 5 in custody, peer-no does not.
	peers := []SamplingPeerInfo{
		{ID: "peer-no", Latency: 1 * time.Millisecond, CustodyColumns: map[uint64]bool{0: true}, SuccessRate: 0.99},
		{ID: "peer-custody", Latency: 50 * time.Millisecond, CustodyColumns: map[uint64]bool{5: true}, SuccessRate: 0.80},
	}

	best := ps.AssignPeer(5, peers)
	if best.ID != "peer-custody" {
		t.Errorf("expected peer-custody (has column in custody), got %s", best.ID)
	}
}

func TestPeerSamplingAssignPeerBySuccessRate(t *testing.T) {
	ps := NewPeerSamplingScheduler(DefaultPeerSamplingConfig())

	// Neither peer custodies column 99. peer-reliable has higher success rate.
	peers := []SamplingPeerInfo{
		{ID: "peer-unreliable", Latency: 10 * time.Millisecond, CustodyColumns: map[uint64]bool{}, SuccessRate: 0.1},
		{ID: "peer-reliable", Latency: 10 * time.Millisecond, CustodyColumns: map[uint64]bool{}, SuccessRate: 0.99},
	}

	best := ps.AssignPeer(99, peers)
	if best.ID != "peer-reliable" {
		t.Errorf("expected peer-reliable (higher success rate), got %s", best.ID)
	}
}

func TestPeerSamplingTrackResultSuccess(t *testing.T) {
	ps := NewPeerSamplingScheduler(DefaultPeerSamplingConfig())
	peers := testPeers()
	columns := []uint64{0, 1}

	_, err := ps.ScheduleSampling(50, columns, peers)
	if err != nil {
		t.Fatalf("ScheduleSampling: %v", err)
	}

	err = ps.TrackResult(50, 0, "peer-A", true, 8*time.Millisecond)
	if err != nil {
		t.Fatalf("TrackResult success: %v", err)
	}

	rate := ps.GetPeerSuccessRate("peer-A")
	if rate != 1.0 {
		t.Errorf("success rate = %f, want 1.0", rate)
	}

	avg := ps.GetPeerAvgLatency("peer-A")
	if avg != 8*time.Millisecond {
		t.Errorf("avg latency = %v, want 8ms", avg)
	}
}

func TestPeerSamplingTrackResultFailure(t *testing.T) {
	ps := NewPeerSamplingScheduler(DefaultPeerSamplingConfig())
	peers := testPeers()
	columns := []uint64{0}

	_, err := ps.ScheduleSampling(60, columns, peers)
	if err != nil {
		t.Fatalf("ScheduleSampling: %v", err)
	}

	err = ps.TrackResult(60, 0, "peer-A", false, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("TrackResult failure: %v", err)
	}

	rate := ps.GetPeerSuccessRate("peer-A")
	if rate != 0.0 {
		t.Errorf("success rate = %f, want 0.0", rate)
	}
}

func TestPeerSamplingSlotStatusPending(t *testing.T) {
	ps := NewPeerSamplingScheduler(DefaultPeerSamplingConfig())
	peers := testPeers()
	columns := []uint64{0, 1, 2, 3}

	_, err := ps.ScheduleSampling(70, columns, peers)
	if err != nil {
		t.Fatalf("ScheduleSampling: %v", err)
	}

	status, err := ps.GetSlotStatus(70)
	if err != nil {
		t.Fatalf("GetSlotStatus: %v", err)
	}
	if status.Pending != 4 {
		t.Errorf("Pending = %d, want 4", status.Pending)
	}
	if status.Verdict != VerdictPending {
		t.Errorf("Verdict = %v, want pending", status.Verdict)
	}
}

func TestPeerSamplingSlotStatusAvailable(t *testing.T) {
	ps := NewPeerSamplingScheduler(DefaultPeerSamplingConfig())
	peers := testPeers()
	columns := []uint64{0, 1, 2, 3}

	_, err := ps.ScheduleSampling(80, columns, peers)
	if err != nil {
		t.Fatalf("ScheduleSampling: %v", err)
	}

	// Mark all columns as successful.
	for _, col := range columns {
		_ = ps.TrackResult(80, col, "peer-A", true, 5*time.Millisecond)
	}

	status, err := ps.GetSlotStatus(80)
	if err != nil {
		t.Fatalf("GetSlotStatus: %v", err)
	}
	if status.Completed != 4 {
		t.Errorf("Completed = %d, want 4", status.Completed)
	}
	if status.Verdict != VerdictAvailable {
		t.Errorf("Verdict = %v, want available", status.Verdict)
	}
}

func TestPeerSamplingSlotStatusUnavailable(t *testing.T) {
	cfg := DefaultPeerSamplingConfig()
	cfg.MaxRetries = 1 // fail on first failure
	cfg.FailureThreshold = 0.3
	ps := NewPeerSamplingScheduler(cfg)
	peers := testPeers()
	columns := []uint64{0, 1, 2, 3}

	_, err := ps.ScheduleSampling(90, columns, peers)
	if err != nil {
		t.Fatalf("ScheduleSampling: %v", err)
	}

	// Fail 3 out of 4 columns (75% > 30% threshold).
	_ = ps.TrackResult(90, 0, "peer-A", false, 100*time.Millisecond)
	_ = ps.TrackResult(90, 1, "peer-A", false, 100*time.Millisecond)
	_ = ps.TrackResult(90, 2, "peer-A", false, 100*time.Millisecond)
	_ = ps.TrackResult(90, 3, "peer-A", true, 5*time.Millisecond)

	status, err := ps.GetSlotStatus(90)
	if err != nil {
		t.Fatalf("GetSlotStatus: %v", err)
	}
	if status.Failed != 3 {
		t.Errorf("Failed = %d, want 3", status.Failed)
	}
	if status.Verdict != VerdictUnavailable {
		t.Errorf("Verdict = %v, want unavailable", status.Verdict)
	}
}

func TestPeerSamplingRetryLogic(t *testing.T) {
	cfg := DefaultPeerSamplingConfig()
	cfg.MaxRetries = 3
	ps := NewPeerSamplingScheduler(cfg)
	peers := testPeers()
	columns := []uint64{0}

	_, err := ps.ScheduleSampling(100, columns, peers)
	if err != nil {
		t.Fatalf("ScheduleSampling: %v", err)
	}

	// First failure (retries=1, not yet exhausted).
	_ = ps.TrackResult(100, 0, "peer-A", false, 50*time.Millisecond)

	retries := ps.RetryFailed(100, peers)
	if len(retries) != 1 {
		t.Fatalf("expected 1 retry assignment, got %d", len(retries))
	}

	// The retry should assign a different peer.
	if retries[0].Peer == "peer-A" {
		t.Error("retry should select a different peer than the one that failed")
	}
}

func TestPeerSamplingRetryExhaustion(t *testing.T) {
	cfg := DefaultPeerSamplingConfig()
	cfg.MaxRetries = 2
	ps := NewPeerSamplingScheduler(cfg)
	peers := []SamplingPeerInfo{
		{ID: "peer-only", Latency: 10 * time.Millisecond, CustodyColumns: map[uint64]bool{0: true}, SuccessRate: 0.5},
	}
	columns := []uint64{0}

	_, err := ps.ScheduleSampling(110, columns, peers)
	if err != nil {
		t.Fatalf("ScheduleSampling: %v", err)
	}

	// Fail twice to exhaust retries.
	_ = ps.TrackResult(110, 0, "peer-only", false, 50*time.Millisecond)
	_ = ps.TrackResult(110, 0, "peer-only", false, 50*time.Millisecond)

	status, err := ps.GetSlotStatus(110)
	if err != nil {
		t.Fatalf("GetSlotStatus: %v", err)
	}
	if status.Failed != 1 {
		t.Errorf("Failed = %d, want 1 (retries exhausted)", status.Failed)
	}
}

func TestPeerSamplingTimeoutDeadlines(t *testing.T) {
	cfg := DefaultPeerSamplingConfig()
	cfg.SampleTimeout = 500 * time.Millisecond
	ps := NewPeerSamplingScheduler(cfg)
	peers := testPeers()
	columns := []uint64{0, 1}

	plan, err := ps.ScheduleSampling(120, columns, peers)
	if err != nil {
		t.Fatalf("ScheduleSampling: %v", err)
	}

	for _, a := range plan.Assignments {
		if a.Deadline.IsZero() {
			t.Error("deadline should not be zero")
		}
		if time.Until(a.Deadline) < 0 {
			t.Error("deadline should be in the future")
		}
		// Deadline should be roughly within the configured timeout.
		if time.Until(a.Deadline) > cfg.SampleTimeout+100*time.Millisecond {
			t.Error("deadline is too far in the future")
		}
	}
}

func TestPeerSamplingEmptyPeerList(t *testing.T) {
	ps := NewPeerSamplingScheduler(DefaultPeerSamplingConfig())
	columns := []uint64{0, 1}

	_, err := ps.ScheduleSampling(130, columns, nil)
	if err != ErrPeerSchedNoPeers {
		t.Fatalf("expected ErrPeerSchedNoPeers, got %v", err)
	}

	_, err = ps.ScheduleSampling(130, columns, []SamplingPeerInfo{})
	if err != ErrPeerSchedNoPeers {
		t.Fatalf("expected ErrPeerSchedNoPeers for empty slice, got %v", err)
	}
}

func TestPeerSamplingEmptyColumns(t *testing.T) {
	ps := NewPeerSamplingScheduler(DefaultPeerSamplingConfig())
	peers := testPeers()

	_, err := ps.ScheduleSampling(140, nil, peers)
	if err != ErrPeerSchedNoColumns {
		t.Fatalf("expected ErrPeerSchedNoColumns, got %v", err)
	}

	_, err = ps.ScheduleSampling(140, []uint64{}, peers)
	if err != ErrPeerSchedNoColumns {
		t.Fatalf("expected ErrPeerSchedNoColumns for empty slice, got %v", err)
	}
}

func TestPeerSamplingClose(t *testing.T) {
	ps := NewPeerSamplingScheduler(DefaultPeerSamplingConfig())
	ps.Close()

	_, err := ps.ScheduleSampling(150, []uint64{0}, testPeers())
	if err != ErrPeerSchedClosed {
		t.Fatalf("expected ErrPeerSchedClosed, got %v", err)
	}

	err = ps.TrackResult(150, 0, "p", true, time.Millisecond)
	if err != ErrPeerSchedClosed {
		t.Fatalf("expected ErrPeerSchedClosed on TrackResult, got %v", err)
	}
}

func TestPeerSamplingSlotStatusUnknown(t *testing.T) {
	ps := NewPeerSamplingScheduler(DefaultPeerSamplingConfig())

	_, err := ps.GetSlotStatus(999)
	if err != ErrPeerSchedSlotUnknown {
		t.Fatalf("expected ErrPeerSchedSlotUnknown, got %v", err)
	}
}

func TestPeerSamplingPurgeSlot(t *testing.T) {
	ps := NewPeerSamplingScheduler(DefaultPeerSamplingConfig())
	peers := testPeers()

	_, _ = ps.ScheduleSampling(200, []uint64{0}, peers)
	if ps.SlotCount() != 1 {
		t.Fatalf("SlotCount = %d, want 1", ps.SlotCount())
	}

	ps.PurgeSlot(200)
	if ps.SlotCount() != 0 {
		t.Errorf("SlotCount = %d after purge, want 0", ps.SlotCount())
	}
}

func TestPeerSamplingConcurrentScheduling(t *testing.T) {
	ps := NewPeerSamplingScheduler(DefaultPeerSamplingConfig())
	peers := testPeers()

	var wg sync.WaitGroup
	errs := make(chan error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(slot uint64) {
			defer wg.Done()
			cols := []uint64{0, 1, 2, 3}
			_, err := ps.ScheduleSampling(slot, cols, peers)
			if err != nil {
				errs <- err
			}
			for _, col := range cols {
				_ = ps.TrackResult(slot, col, "peer-A", true, 5*time.Millisecond)
			}
			_, _ = ps.GetSlotStatus(slot)
		}(uint64(1000 + i))
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent error: %v", err)
	}

	if ps.SlotCount() != 20 {
		t.Errorf("SlotCount = %d, want 20", ps.SlotCount())
	}
}

func TestPeerDAVerdictString(t *testing.T) {
	cases := []struct {
		v    DAVerdict
		want string
	}{
		{VerdictPending, "pending"},
		{VerdictAvailable, "available"},
		{VerdictUnavailable, "unavailable"},
		{DAVerdict(99), "unknown"},
	}
	for _, c := range cases {
		if got := c.v.String(); got != c.want {
			t.Errorf("DAVerdict(%d).String() = %q, want %q", c.v, got, c.want)
		}
	}
}

func TestPeerSamplingDefaultConfig(t *testing.T) {
	cfg := DefaultPeerSamplingConfig()
	if cfg.SamplesPerSlot != SamplesPerSlot {
		t.Errorf("SamplesPerSlot = %d, want %d", cfg.SamplesPerSlot, SamplesPerSlot)
	}
	if cfg.MaxRetries <= 0 {
		t.Error("MaxRetries should be > 0")
	}
	if cfg.SampleTimeout <= 0 {
		t.Error("SampleTimeout should be > 0")
	}
	if cfg.FailureThreshold <= 0 || cfg.FailureThreshold > 1.0 {
		t.Error("FailureThreshold should be in (0, 1]")
	}
}

func TestPeerSamplingTrackResultUnknownSlot(t *testing.T) {
	ps := NewPeerSamplingScheduler(DefaultPeerSamplingConfig())
	err := ps.TrackResult(999, 0, "peer-A", true, time.Millisecond)
	if err != ErrPeerSchedSlotUnknown {
		t.Fatalf("expected ErrPeerSchedSlotUnknown, got %v", err)
	}
}

func TestPeerSamplingMultipleSlotsIndependent(t *testing.T) {
	ps := NewPeerSamplingScheduler(DefaultPeerSamplingConfig())
	peers := testPeers()

	_, _ = ps.ScheduleSampling(300, []uint64{0, 1}, peers)
	_, _ = ps.ScheduleSampling(301, []uint64{2, 3}, peers)

	// Complete slot 300 only.
	_ = ps.TrackResult(300, 0, "peer-A", true, 5*time.Millisecond)
	_ = ps.TrackResult(300, 1, "peer-A", true, 5*time.Millisecond)

	s300, _ := ps.GetSlotStatus(300)
	s301, _ := ps.GetSlotStatus(301)

	if s300.Verdict != VerdictAvailable {
		t.Errorf("slot 300 verdict = %v, want available", s300.Verdict)
	}
	if s301.Verdict != VerdictPending {
		t.Errorf("slot 301 verdict = %v, want pending", s301.Verdict)
	}
}

func TestPeerSamplingAssignPeerEmptyList(t *testing.T) {
	ps := NewPeerSamplingScheduler(DefaultPeerSamplingConfig())
	result := ps.AssignPeer(0, nil)
	if result.ID != "" {
		t.Errorf("expected empty peer for nil list, got %s", result.ID)
	}
}
