package p2p

import (
	"sync"
	"testing"
	"time"
)

func TestNewMessageRouter(t *testing.T) {
	r := NewMessageRouter(RouterConfig{})
	if r == nil {
		t.Fatal("NewMessageRouter returned nil")
	}
	if r.HandlerCount() != 0 {
		t.Fatalf("expected 0 handlers, got %d", r.HandlerCount())
	}
	r.Close()
}

func TestMessageRouter_RegisterHandler(t *testing.T) {
	r := NewMessageRouter(RouterConfig{})
	defer r.Close()

	handler := func(peerID string, msg Msg) error { return nil }
	if err := r.RegisterHandler(0x01, handler); err != nil {
		t.Fatalf("RegisterHandler: %v", err)
	}
	if !r.HasHandler(0x01) {
		t.Fatal("handler should be registered")
	}
	if r.HandlerCount() != 1 {
		t.Fatalf("expected 1 handler, got %d", r.HandlerCount())
	}
}

func TestMessageRouter_RegisterHandler_Duplicate(t *testing.T) {
	r := NewMessageRouter(RouterConfig{})
	defer r.Close()

	handler := func(peerID string, msg Msg) error { return nil }
	r.RegisterHandler(0x01, handler)

	err := r.RegisterHandler(0x01, handler)
	if err == nil {
		t.Fatal("expected error for duplicate handler")
	}
}

func TestMessageRouter_RegisterHandler_Nil(t *testing.T) {
	r := NewMessageRouter(RouterConfig{})
	defer r.Close()

	err := r.RegisterHandler(0x01, nil)
	if err == nil {
		t.Fatal("expected error for nil handler")
	}
}

func TestMessageRouter_SetHandler(t *testing.T) {
	r := NewMessageRouter(RouterConfig{})
	defer r.Close()

	handler := func(peerID string, msg Msg) error { return nil }
	r.SetHandler(0x01, handler)
	if !r.HasHandler(0x01) {
		t.Fatal("handler should be registered")
	}

	// Replace.
	r.SetHandler(0x01, func(peerID string, msg Msg) error { return nil })
	if !r.HasHandler(0x01) {
		t.Fatal("handler should still be registered after replace")
	}

	// Remove.
	r.SetHandler(0x01, nil)
	if r.HasHandler(0x01) {
		t.Fatal("handler should be unregistered after nil set")
	}
}

func TestMessageRouter_UnregisterHandler(t *testing.T) {
	r := NewMessageRouter(RouterConfig{})
	defer r.Close()

	handler := func(peerID string, msg Msg) error { return nil }
	r.RegisterHandler(0x01, handler)
	r.UnregisterHandler(0x01)
	if r.HasHandler(0x01) {
		t.Fatal("handler should be unregistered")
	}
}

func TestMessageRouter_Dispatch(t *testing.T) {
	r := NewMessageRouter(RouterConfig{RateLimit: 1000, RateBurst: 100})
	defer r.Close()

	var received Msg
	var receivedPeer string

	r.SetHandler(0x01, func(peerID string, msg Msg) error {
		received = msg
		receivedPeer = peerID
		return nil
	})
	r.TrackPeer("peer1")

	msg := Msg{Code: 0x01, Size: 5, Payload: []byte("hello")}
	if err := r.Dispatch("peer1", msg); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if receivedPeer != "peer1" {
		t.Fatalf("peer: got %q, want %q", receivedPeer, "peer1")
	}
	if received.Code != 0x01 {
		t.Fatalf("code: got %d, want 1", received.Code)
	}
	if string(received.Payload) != "hello" {
		t.Fatalf("payload: got %q, want %q", received.Payload, "hello")
	}
}

func TestMessageRouter_Dispatch_NoHandler(t *testing.T) {
	r := NewMessageRouter(RouterConfig{RateLimit: 1000, RateBurst: 100})
	defer r.Close()

	r.TrackPeer("peer1")
	err := r.Dispatch("peer1", Msg{Code: 0xFF})
	if err == nil {
		t.Fatal("expected error for unregistered code")
	}
}

func TestMessageRouter_Dispatch_Closed(t *testing.T) {
	r := NewMessageRouter(RouterConfig{})
	r.Close()

	err := r.Dispatch("peer1", Msg{Code: 0x01})
	if err != ErrRouterClosed {
		t.Fatalf("expected ErrRouterClosed, got %v", err)
	}
}

func TestMessageRouter_RateLimit(t *testing.T) {
	// Very low rate limit to trigger limiting.
	r := NewMessageRouter(RouterConfig{RateLimit: 1, RateBurst: 1})
	defer r.Close()

	r.SetHandler(0x01, func(peerID string, msg Msg) error { return nil })
	r.TrackPeer("peer1")

	// First message should succeed (uses the burst token).
	if err := r.Dispatch("peer1", Msg{Code: 0x01}); err != nil {
		t.Fatalf("first dispatch: %v", err)
	}

	// Second message should be rate limited (no tokens left).
	err := r.Dispatch("peer1", Msg{Code: 0x01})
	if err != ErrRateLimited {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}

	_, dropped, rateLimited, _ := r.Stats()
	if rateLimited < 1 {
		t.Fatalf("expected at least 1 rate limited, got %d", rateLimited)
	}
	if dropped < 1 {
		t.Fatalf("expected at least 1 dropped, got %d", dropped)
	}
}

func TestMessageRouter_TrackUntrackPeer(t *testing.T) {
	r := NewMessageRouter(RouterConfig{})
	defer r.Close()

	r.TrackPeer("peer1")
	r.TrackPeer("peer1") // double track should not panic

	r.UntrackPeer("peer1")
	r.UntrackPeer("peer1") // double untrack should not panic
}

func TestMessageRouter_PriorityQueue_Enqueue(t *testing.T) {
	r := NewMessageRouter(RouterConfig{OutboundMax: 100})
	defer r.Close()

	err := r.Enqueue(Msg{Code: 0x01, Payload: []byte("low")}, "peer1", PriorityLow)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	err = r.Enqueue(Msg{Code: 0x02, Payload: []byte("high")}, "peer1", PriorityHigh)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	err = r.Enqueue(Msg{Code: 0x03, Payload: []byte("normal")}, "peer1", PriorityNormal)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if r.QueueLen() != 3 {
		t.Fatalf("queue length: got %d, want 3", r.QueueLen())
	}
}

func TestMessageRouter_PriorityQueue_Order(t *testing.T) {
	r := NewMessageRouter(RouterConfig{OutboundMax: 100})
	defer r.Close()

	// Enqueue in reverse priority order.
	r.Enqueue(Msg{Code: 0x01, Payload: []byte("low")}, "p1", PriorityLow)
	time.Sleep(time.Millisecond) // ensure different timestamps
	r.Enqueue(Msg{Code: 0x02, Payload: []byte("high")}, "p1", PriorityHigh)
	time.Sleep(time.Millisecond)
	r.Enqueue(Msg{Code: 0x03, Payload: []byte("normal")}, "p1", PriorityNormal)

	// Dequeue should return highest priority first.
	m1 := r.DequeueNonBlocking()
	if m1 == nil || m1.Priority != PriorityHigh {
		t.Fatalf("first dequeue should be high priority, got %v", m1)
	}

	m2 := r.DequeueNonBlocking()
	if m2 == nil || m2.Priority != PriorityNormal {
		t.Fatalf("second dequeue should be normal priority, got %v", m2)
	}

	m3 := r.DequeueNonBlocking()
	if m3 == nil || m3.Priority != PriorityLow {
		t.Fatalf("third dequeue should be low priority, got %v", m3)
	}

	m4 := r.DequeueNonBlocking()
	if m4 != nil {
		t.Fatal("queue should be empty")
	}
}

func TestMessageRouter_PriorityQueue_FIFO(t *testing.T) {
	r := NewMessageRouter(RouterConfig{OutboundMax: 100})
	defer r.Close()

	// Same priority: FIFO.
	r.Enqueue(Msg{Code: 0x01, Payload: []byte("first")}, "p1", PriorityNormal)
	time.Sleep(time.Millisecond)
	r.Enqueue(Msg{Code: 0x02, Payload: []byte("second")}, "p1", PriorityNormal)
	time.Sleep(time.Millisecond)
	r.Enqueue(Msg{Code: 0x03, Payload: []byte("third")}, "p1", PriorityNormal)

	m1 := r.DequeueNonBlocking()
	if string(m1.Msg.Payload) != "first" {
		t.Fatalf("expected first, got %q", m1.Msg.Payload)
	}
	m2 := r.DequeueNonBlocking()
	if string(m2.Msg.Payload) != "second" {
		t.Fatalf("expected second, got %q", m2.Msg.Payload)
	}
	m3 := r.DequeueNonBlocking()
	if string(m3.Msg.Payload) != "third" {
		t.Fatalf("expected third, got %q", m3.Msg.Payload)
	}
}

func TestMessageRouter_PriorityQueue_Full(t *testing.T) {
	r := NewMessageRouter(RouterConfig{OutboundMax: 2})
	defer r.Close()

	r.Enqueue(Msg{Code: 0x01}, "p1", PriorityNormal)
	r.Enqueue(Msg{Code: 0x02}, "p1", PriorityNormal)

	err := r.Enqueue(Msg{Code: 0x03}, "p1", PriorityNormal)
	if err != ErrQueueFull {
		t.Fatalf("expected ErrQueueFull, got %v", err)
	}
}

func TestMessageRouter_Enqueue_Closed(t *testing.T) {
	r := NewMessageRouter(RouterConfig{})
	r.Close()

	err := r.Enqueue(Msg{Code: 0x01}, "p1", PriorityNormal)
	if err != ErrRouterClosed {
		t.Fatalf("expected ErrRouterClosed, got %v", err)
	}
}

func TestMessageRouter_RequestResponse(t *testing.T) {
	r := NewMessageRouter(RouterConfig{RateLimit: 1000, RateBurst: 100})
	defer r.Close()

	// Use a MsgPipe as transport.
	a, b := MsgPipe()
	defer a.Close()
	defer b.Close()

	r.TrackPeer("peer1")

	// Goroutine simulates the remote side: reads request from b, sends response back on b.
	go func() {
		msg, err := b.ReadMsg()
		if err != nil {
			return
		}
		// Echo back as response with code 0x10.
		b.WriteMsg(Msg{
			Code:    0x10,
			Size:    msg.Size,
			Payload: msg.Payload, // includes request ID prefix
		})
	}()

	// Goroutine reads the response from transport a and delivers it to the router.
	go func() {
		msg, err := a.ReadMsg()
		if err != nil {
			return
		}
		r.Dispatch("peer1", msg)
	}()

	// Register a handler for 0x10 so Dispatch does not error with "no handler".
	r.SetHandler(0x10, func(peerID string, msg Msg) error {
		return nil
	})

	resp, err := r.SendRequest(a, 0x0F, 0x10, []byte("request-data"), "peer1")
	if err != nil {
		t.Fatalf("SendRequest: %v", err)
	}

	if string(resp.Payload) != "request-data" {
		t.Fatalf("response payload: got %q, want %q", resp.Payload, "request-data")
	}
}

func TestMessageRouter_PendingRequests(t *testing.T) {
	r := NewMessageRouter(RouterConfig{})
	defer r.Close()

	if r.PendingRequests() != 0 {
		t.Fatalf("expected 0 pending, got %d", r.PendingRequests())
	}
}

func TestMessageRouter_ExpireRequests(t *testing.T) {
	r := NewMessageRouter(RouterConfig{})
	defer r.Close()

	// Manually add a pending request.
	r.reqMu.Lock()
	r.pending[1] = &routerPendingReq{
		id:      1,
		code:    0x10,
		peerID:  "peer1",
		created: time.Now().Add(-time.Minute),
		respCh:  make(chan Msg, 1),
	}
	r.reqMu.Unlock()

	expired := r.ExpireRequests(30 * time.Second)
	if expired != 1 {
		t.Fatalf("expected 1 expired, got %d", expired)
	}
	if r.PendingRequests() != 0 {
		t.Fatalf("expected 0 pending after expire, got %d", r.PendingRequests())
	}
}

func TestMessageRouter_Stats(t *testing.T) {
	r := NewMessageRouter(RouterConfig{RateLimit: 1000, RateBurst: 100})
	defer r.Close()

	r.SetHandler(0x01, func(peerID string, msg Msg) error { return nil })
	r.TrackPeer("peer1")

	r.Dispatch("peer1", Msg{Code: 0x01})
	r.Dispatch("peer1", Msg{Code: 0x01})

	dispatched, _, _, _ := r.Stats()
	if dispatched != 2 {
		t.Fatalf("expected 2 dispatched, got %d", dispatched)
	}
}

func TestMessageRouter_DeliverResponse(t *testing.T) {
	r := NewMessageRouter(RouterConfig{})
	defer r.Close()

	// Register a pending request.
	respCh := make(chan Msg, 1)
	r.reqMu.Lock()
	r.pending[42] = &routerPendingReq{
		id:      42,
		code:    0x10,
		peerID:  "peer1",
		created: time.Now(),
		respCh:  respCh,
	}
	r.reqMu.Unlock()

	// Build a response message with request ID in first 8 bytes.
	payload := make([]byte, 8+5)
	putUint64BE(payload[:8], 42)
	copy(payload[8:], []byte("reply"))

	delivered := r.deliverResponse(Msg{Code: 0x10, Payload: payload})
	if !delivered {
		t.Fatal("response should have been delivered")
	}

	select {
	case resp := <-respCh:
		if string(resp.Payload) != "reply" {
			t.Fatalf("response payload: got %q, want %q", resp.Payload, "reply")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for response")
	}
}

func TestMessageRouter_DeliverResponse_NoMatch(t *testing.T) {
	r := NewMessageRouter(RouterConfig{})
	defer r.Close()

	// No pending requests.
	payload := make([]byte, 8)
	putUint64BE(payload[:8], 999)
	delivered := r.deliverResponse(Msg{Code: 0x10, Payload: payload})
	if delivered {
		t.Fatal("should not deliver when no pending request matches")
	}
}

func TestMessageRouter_ConcurrentDispatch(t *testing.T) {
	r := NewMessageRouter(RouterConfig{RateLimit: 10000, RateBurst: 1000})
	defer r.Close()

	var count int64
	var mu sync.Mutex

	r.SetHandler(0x01, func(peerID string, msg Msg) error {
		mu.Lock()
		count++
		mu.Unlock()
		return nil
	})

	const numPeers = 5
	const msgsPerPeer = 20

	for i := 0; i < numPeers; i++ {
		peerID := "peer" + string(rune('0'+i))
		r.TrackPeer(peerID)
	}

	var wg sync.WaitGroup
	for i := 0; i < numPeers; i++ {
		peerID := "peer" + string(rune('0'+i))
		wg.Add(1)
		go func(pid string) {
			defer wg.Done()
			for j := 0; j < msgsPerPeer; j++ {
				r.Dispatch(pid, Msg{Code: 0x01, Payload: []byte("data")})
			}
		}(peerID)
	}
	wg.Wait()

	mu.Lock()
	total := count
	mu.Unlock()

	if total != numPeers*msgsPerPeer {
		t.Fatalf("expected %d handled, got %d", numPeers*msgsPerPeer, total)
	}
}

func TestRateLimiter(t *testing.T) {
	rl := newRateLimiter(10, 5)

	// Should allow burst.
	for i := 0; i < 5; i++ {
		if !rl.allow() {
			t.Fatalf("should allow message %d in burst", i)
		}
	}
	// Should deny after burst exhausted.
	if rl.allow() {
		t.Fatal("should deny after burst exhausted")
	}

	// After waiting, tokens should refill.
	time.Sleep(200 * time.Millisecond) // 10/sec * 0.2s = 2 tokens
	if !rl.allow() {
		t.Fatal("should allow after refill")
	}
}

func TestPutGetUint64BE(t *testing.T) {
	tests := []uint64{0, 1, 255, 65535, 0xFFFFFFFF, 0xFFFFFFFFFFFFFFFF}
	for _, v := range tests {
		buf := make([]byte, 8)
		putUint64BE(buf, v)
		got := getUint64BE(buf)
		if got != v {
			t.Fatalf("uint64 roundtrip: got %d, want %d", got, v)
		}
	}
}
