package sync

import (
	"testing"
)

func TestSyncer_StartStop(t *testing.T) {
	s := NewSyncer(DefaultConfig())

	// Should start in idle state.
	if s.State() != StateIdle {
		t.Fatalf("initial state: want idle, got %d", s.State())
	}

	// Start sync to block 100.
	s.SetCallbacks(nil, nil, func() uint64 { return 0 }, nil)
	if err := s.Start(100); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if !s.IsSyncing() {
		t.Fatal("should be syncing after Start")
	}

	prog := s.GetProgress()
	if prog.HighestBlock != 100 {
		t.Fatalf("highest block: want 100, got %d", prog.HighestBlock)
	}

	// Cancel.
	s.Cancel()
	if s.IsSyncing() {
		t.Fatal("should not be syncing after Cancel")
	}
}

func TestSyncer_DoubleStart(t *testing.T) {
	s := NewSyncer(nil)
	s.SetCallbacks(nil, nil, func() uint64 { return 0 }, nil)

	if err := s.Start(100); err != nil {
		t.Fatalf("first Start: %v", err)
	}

	if err := s.Start(200); err != ErrAlreadySyncing {
		t.Fatalf("second Start: want ErrAlreadySyncing, got %v", err)
	}
}

func TestSyncer_ProcessHeaders(t *testing.T) {
	var insertedCount int
	s := NewSyncer(nil)
	s.SetCallbacks(
		func(headers []HeaderData) (int, error) {
			insertedCount = len(headers)
			return len(headers), nil
		},
		nil,
		func() uint64 { return 0 },
		nil,
	)
	s.Start(100)

	headers := []HeaderData{
		{Number: 1, Hash: [32]byte{0x01}},
		{Number: 2, Hash: [32]byte{0x02}},
		{Number: 3, Hash: [32]byte{0x03}},
	}

	n, err := s.ProcessHeaders(headers)
	if err != nil {
		t.Fatalf("ProcessHeaders: %v", err)
	}
	if n != 3 {
		t.Fatalf("processed: want 3, got %d", n)
	}
	if insertedCount != 3 {
		t.Fatalf("insertedCount: want 3, got %d", insertedCount)
	}

	prog := s.GetProgress()
	if prog.PulledHeaders != 3 {
		t.Fatalf("pulled headers: want 3, got %d", prog.PulledHeaders)
	}
	if prog.CurrentBlock != 3 {
		t.Fatalf("current block: want 3, got %d", prog.CurrentBlock)
	}
}

func TestSyncer_ProcessBlocks_Completion(t *testing.T) {
	s := NewSyncer(nil)
	s.SetCallbacks(
		nil,
		func(blocks []BlockData) (int, error) {
			return len(blocks), nil
		},
		func() uint64 { return 0 },
		nil,
	)
	s.Start(3) // target = block 3

	blocks := []BlockData{
		{Number: 1},
		{Number: 2},
		{Number: 3},
	}

	n, err := s.ProcessBlocks(blocks)
	if err != nil {
		t.Fatalf("ProcessBlocks: %v", err)
	}
	if n != 3 {
		t.Fatalf("processed: want 3, got %d", n)
	}

	// Should be marked done since we reached target.
	if s.State() != StateDone {
		t.Fatalf("state: want done, got %d", s.State())
	}
}

func TestSyncer_CancelledProcess(t *testing.T) {
	s := NewSyncer(nil)
	s.SetCallbacks(
		func(headers []HeaderData) (int, error) {
			return len(headers), nil
		},
		nil,
		func() uint64 { return 0 },
		nil,
	)
	s.Start(100)
	s.Cancel()

	_, err := s.ProcessHeaders([]HeaderData{{Number: 1}})
	if err != ErrCancelled {
		t.Fatalf("want ErrCancelled, got %v", err)
	}
}

func TestHeaderFetcher_RequestDeliver(t *testing.T) {
	f := NewHeaderFetcher(192)

	peer := PeerID("peer1")
	if err := f.Request(peer, 1, 10); err != nil {
		t.Fatalf("Request: %v", err)
	}

	if f.PendingCount() != 1 {
		t.Fatalf("pending: want 1, got %d", f.PendingCount())
	}

	// Duplicate request should fail.
	if err := f.Request(peer, 11, 10); err == nil {
		t.Fatal("duplicate request should fail")
	}

	// Deliver response.
	headers := []HeaderData{
		{Number: 1, Hash: [32]byte{0x01}},
		{Number: 2, Hash: [32]byte{0x02}},
	}
	if err := f.Deliver(peer, headers); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	if f.PendingCount() != 0 {
		t.Fatalf("pending after deliver: want 0, got %d", f.PendingCount())
	}

	// Check result channel.
	select {
	case resp := <-f.Results():
		if resp.PeerID != peer {
			t.Fatalf("response peer: want %s, got %s", peer, resp.PeerID)
		}
		if len(resp.Headers) != 2 {
			t.Fatalf("response headers: want 2, got %d", len(resp.Headers))
		}
	default:
		t.Fatal("expected result in channel")
	}
}

func TestHeaderFetcher_DeliverUnknown(t *testing.T) {
	f := NewHeaderFetcher(192)
	err := f.Deliver(PeerID("unknown"), nil)
	if err == nil {
		t.Fatal("deliver to unknown peer should fail")
	}
}

func TestHeaderFetcher_MaxBatch(t *testing.T) {
	f := NewHeaderFetcher(10)

	// Request more than max batch.
	peer := PeerID("peer1")
	if err := f.Request(peer, 1, 100); err != nil {
		t.Fatalf("Request: %v", err)
	}

	// Pending count should still be 1 (request was capped).
	if !f.HasPending(peer) {
		t.Fatal("should have pending request")
	}
}

func TestBodyFetcher_RequestDeliver(t *testing.T) {
	f := NewBodyFetcher(128)

	peer := PeerID("peer1")
	f.Request(peer, 1, 5)

	bodies := []BlockData{
		{Number: 1, Hash: [32]byte{0x01}},
	}
	if err := f.Deliver(peer, bodies); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	if f.PendingCount() != 0 {
		t.Fatalf("pending: want 0, got %d", f.PendingCount())
	}

	select {
	case resp := <-f.Results():
		if len(resp.Bodies) != 1 {
			t.Fatalf("bodies: want 1, got %d", len(resp.Bodies))
		}
	default:
		t.Fatal("expected result")
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Mode != ModeFull {
		t.Fatalf("default mode: want full, got %s", cfg.Mode)
	}
	if cfg.BatchSize != 192 {
		t.Fatalf("batch size: want 192, got %d", cfg.BatchSize)
	}
}
