package sync

import (
	"errors"
	"testing"
)

// --- HeaderFetcher creation tests ---

func TestFetcher_NewHeaderFetcher(t *testing.T) {
	f := NewHeaderFetcher(100)
	if f == nil {
		t.Fatal("NewHeaderFetcher returned nil")
	}
	if f.PendingCount() != 0 {
		t.Fatalf("new fetcher pending: want 0, got %d", f.PendingCount())
	}
	if f.maxBatch != 100 {
		t.Fatalf("maxBatch: want 100, got %d", f.maxBatch)
	}
}

func TestFetcher_NewBodyFetcher(t *testing.T) {
	f := NewBodyFetcher(50)
	if f == nil {
		t.Fatal("NewBodyFetcher returned nil")
	}
	if f.PendingCount() != 0 {
		t.Fatalf("new fetcher pending: want 0, got %d", f.PendingCount())
	}
	if f.maxBatch != 50 {
		t.Fatalf("maxBatch: want 50, got %d", f.maxBatch)
	}
}

// --- HeaderFetcher request/deliver cycle ---

func TestFetcher_HeaderRequestDeliverCycle(t *testing.T) {
	f := NewHeaderFetcher(128)
	p := PeerID("alpha")

	if err := f.Request(p, 10, 5); err != nil {
		t.Fatalf("Request: %v", err)
	}
	if !f.HasPending(p) {
		t.Fatal("should have pending request for alpha")
	}
	if f.PendingCount() != 1 {
		t.Fatalf("pending: want 1, got %d", f.PendingCount())
	}

	headers := []HeaderData{
		{Number: 10, Hash: [32]byte{0x0a}},
		{Number: 11, Hash: [32]byte{0x0b}},
	}
	if err := f.Deliver(p, headers); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	if f.HasPending(p) {
		t.Fatal("should not have pending after delivery")
	}

	select {
	case resp := <-f.Results():
		if resp.PeerID != p {
			t.Fatalf("peer: want %s, got %s", p, resp.PeerID)
		}
		if len(resp.Headers) != 2 {
			t.Fatalf("headers: want 2, got %d", len(resp.Headers))
		}
		if resp.Err != nil {
			t.Fatalf("unexpected error: %v", resp.Err)
		}
	default:
		t.Fatal("expected result on channel")
	}
}

// --- Duplicate request ---

func TestFetcher_HeaderDuplicateRequest(t *testing.T) {
	f := NewHeaderFetcher(128)
	p := PeerID("beta")

	if err := f.Request(p, 1, 10); err != nil {
		t.Fatalf("first request: %v", err)
	}
	if err := f.Request(p, 20, 10); err == nil {
		t.Fatal("duplicate request should return error")
	}
	if f.PendingCount() != 1 {
		t.Fatalf("pending: want 1, got %d", f.PendingCount())
	}
}

// --- Batch cap ---

func TestFetcher_HeaderBatchCap(t *testing.T) {
	f := NewHeaderFetcher(10)
	p := PeerID("gamma")

	// Request 100 but should be capped at 10.
	if err := f.Request(p, 1, 100); err != nil {
		t.Fatalf("Request: %v", err)
	}
	if !f.HasPending(p) {
		t.Fatal("should have pending request")
	}
}

// --- Deliver to unknown peer ---

func TestFetcher_HeaderDeliverUnknown(t *testing.T) {
	f := NewHeaderFetcher(128)
	err := f.Deliver(PeerID("unknown"), nil)
	if err == nil {
		t.Fatal("deliver to unknown peer should fail")
	}
}

// --- Error delivery ---

func TestFetcher_HeaderDeliverError(t *testing.T) {
	f := NewHeaderFetcher(128)
	p := PeerID("delta")

	if err := f.Request(p, 1, 5); err != nil {
		t.Fatalf("Request: %v", err)
	}

	testErr := errors.New("fetch failed")
	f.DeliverError(p, testErr)

	if f.HasPending(p) {
		t.Fatal("should not have pending after error delivery")
	}

	select {
	case resp := <-f.Results():
		if resp.PeerID != p {
			t.Fatalf("peer: want %s, got %s", p, resp.PeerID)
		}
		if resp.Err == nil {
			t.Fatal("expected error in response")
		}
		if resp.Err.Error() != "fetch failed" {
			t.Fatalf("error: want 'fetch failed', got %q", resp.Err.Error())
		}
	default:
		t.Fatal("expected result on channel")
	}
}

// --- Multiple peers ---

func TestFetcher_HeaderMultiplePeers(t *testing.T) {
	f := NewHeaderFetcher(128)
	p1, p2, p3 := PeerID("p1"), PeerID("p2"), PeerID("p3")

	f.Request(p1, 1, 10)
	f.Request(p2, 11, 10)
	f.Request(p3, 21, 10)

	if f.PendingCount() != 3 {
		t.Fatalf("pending: want 3, got %d", f.PendingCount())
	}

	// Deliver in reverse order.
	f.Deliver(p3, []HeaderData{{Number: 21}})
	f.Deliver(p1, []HeaderData{{Number: 1}})
	f.Deliver(p2, []HeaderData{{Number: 11}})

	if f.PendingCount() != 0 {
		t.Fatalf("pending after all deliveries: want 0, got %d", f.PendingCount())
	}

	// Should have 3 results.
	for i := 0; i < 3; i++ {
		select {
		case <-f.Results():
		default:
			t.Fatalf("expected result %d on channel", i+1)
		}
	}
}

// --- BodyFetcher duplicate and delivery ---

func TestFetcher_BodyDuplicateRequest(t *testing.T) {
	f := NewBodyFetcher(64)
	p := PeerID("epsilon")

	if err := f.Request(p, 1, 5); err != nil {
		t.Fatalf("first request: %v", err)
	}
	if err := f.Request(p, 10, 5); err == nil {
		t.Fatal("duplicate body request should fail")
	}
}

func TestFetcher_BodyDeliverUnknown(t *testing.T) {
	f := NewBodyFetcher(64)
	err := f.Deliver(PeerID("unknown"), nil)
	if err == nil {
		t.Fatal("deliver to unknown body peer should fail")
	}
}

func TestFetcher_BodyRequestDeliverCycle(t *testing.T) {
	f := NewBodyFetcher(64)
	p := PeerID("zeta")

	f.Request(p, 1, 3)
	bodies := []BlockData{
		{Number: 1, Hash: [32]byte{0x01}},
		{Number: 2, Hash: [32]byte{0x02}},
	}
	if err := f.Deliver(p, bodies); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	select {
	case resp := <-f.Results():
		if resp.PeerID != p {
			t.Fatalf("peer: want %s, got %s", p, resp.PeerID)
		}
		if len(resp.Bodies) != 2 {
			t.Fatalf("bodies: want 2, got %d", len(resp.Bodies))
		}
	default:
		t.Fatal("expected result")
	}
}

// --- FetchRequest struct ---

func TestFetcher_FetchRequestStruct(t *testing.T) {
	r := FetchRequest{
		PeerID: "test-peer",
		From:   100,
		Count:  50,
	}
	if r.PeerID != "test-peer" {
		t.Fatalf("PeerID: want test-peer, got %s", r.PeerID)
	}
	if r.From != 100 {
		t.Fatalf("From: want 100, got %d", r.From)
	}
	if r.Count != 50 {
		t.Fatalf("Count: want 50, got %d", r.Count)
	}
}

// --- HasPending for non-existent peer ---

func TestFetcher_HasPendingFalse(t *testing.T) {
	f := NewHeaderFetcher(64)
	if f.HasPending(PeerID("ghost")) {
		t.Fatal("HasPending should be false for non-existent peer")
	}
}
