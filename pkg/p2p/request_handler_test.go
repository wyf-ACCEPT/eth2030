package p2p

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// echoHandler is a MessageHandler that echoes back the data it receives.
type echoHandler struct{}

func (h *echoHandler) Handle(peerID string, data []byte) ([]byte, error) {
	return data, nil
}

// errorHandler always returns an error.
type errorHandler struct {
	err error
}

func (h *errorHandler) Handle(peerID string, data []byte) ([]byte, error) {
	return nil, h.err
}

// slowHandler blocks for a configured duration before responding.
type slowHandler struct {
	delay time.Duration
}

func (h *slowHandler) Handle(peerID string, data []byte) ([]byte, error) {
	time.Sleep(h.delay)
	return data, nil
}

// peerRecordingHandler records the peerID it was called with.
type peerRecordingHandler struct {
	mu      sync.Mutex
	peerIDs []string
}

func (h *peerRecordingHandler) Handle(peerID string, data []byte) ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.peerIDs = append(h.peerIDs, peerID)
	return []byte("ok"), nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRequestHandler_RegisterAndHandle(t *testing.T) {
	rh := NewRequestHandler()

	handler := &echoHandler{}
	if err := rh.RegisterHandler(0x01, handler); err != nil {
		t.Fatalf("RegisterHandler: %v", err)
	}

	data := []byte("hello")
	resp, err := rh.HandleMessage("peer-1", 0x01, data)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if !bytes.Equal(resp, data) {
		t.Errorf("response = %q, want %q", resp, data)
	}
}

func TestRequestHandler_UnregisteredCode(t *testing.T) {
	rh := NewRequestHandler()

	_, err := rh.HandleMessage("peer-1", 0xFF, []byte("test"))
	if err == nil {
		t.Fatal("expected error for unregistered code")
	}
	if !errors.Is(err, ErrNoMessageHandler) {
		t.Errorf("got %v, want ErrNoMessageHandler", err)
	}
}

func TestRequestHandler_NilHandler(t *testing.T) {
	rh := NewRequestHandler()

	err := rh.RegisterHandler(0x01, nil)
	if err == nil {
		t.Fatal("expected error for nil handler")
	}
	if !errors.Is(err, ErrNilMessageHandler) {
		t.Errorf("got %v, want ErrNilMessageHandler", err)
	}
}

func TestRequestHandler_ReplaceHandler(t *testing.T) {
	rh := NewRequestHandler()

	handler1 := MessageHandlerFunc(func(peerID string, data []byte) ([]byte, error) {
		return []byte("handler1"), nil
	})
	handler2 := MessageHandlerFunc(func(peerID string, data []byte) ([]byte, error) {
		return []byte("handler2"), nil
	})

	rh.RegisterHandler(0x01, handler1)
	resp, _ := rh.HandleMessage("peer-1", 0x01, nil)
	if string(resp) != "handler1" {
		t.Errorf("expected handler1, got %q", resp)
	}

	rh.RegisterHandler(0x01, handler2)
	resp, _ = rh.HandleMessage("peer-1", 0x01, nil)
	if string(resp) != "handler2" {
		t.Errorf("expected handler2 after replace, got %q", resp)
	}
}

func TestRequestHandler_HandlerError(t *testing.T) {
	rh := NewRequestHandler()

	testErr := errors.New("test error")
	rh.RegisterHandler(0x01, &errorHandler{err: testErr})

	_, err := rh.HandleMessage("peer-1", 0x01, []byte("data"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, testErr) {
		t.Errorf("got %v, want %v", err, testErr)
	}
}

func TestRequestHandler_Timeout(t *testing.T) {
	rh := NewRequestHandler()
	rh.SetTimeout(50 * time.Millisecond)

	rh.RegisterHandler(0x01, &slowHandler{delay: 500 * time.Millisecond})

	_, err := rh.HandleMessage("peer-1", 0x01, []byte("data"))
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, ErrHandlerTimeout) {
		t.Errorf("got %v, want ErrHandlerTimeout", err)
	}
}

func TestRequestHandler_NoTimeoutWhenFast(t *testing.T) {
	rh := NewRequestHandler()
	rh.SetTimeout(5 * time.Second)

	rh.RegisterHandler(0x01, &echoHandler{})

	data := []byte("fast")
	resp, err := rh.HandleMessage("peer-1", 0x01, data)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if !bytes.Equal(resp, data) {
		t.Errorf("response = %q, want %q", resp, data)
	}
}

func TestRequestHandler_Stats(t *testing.T) {
	rh := NewRequestHandler()

	testErr := errors.New("fail")
	rh.RegisterHandler(0x01, &echoHandler{})
	rh.RegisterHandler(0x02, &errorHandler{err: testErr})

	// Successful message.
	rh.HandleMessage("peer-1", 0x01, []byte("ok"))
	// Handler error.
	rh.HandleMessage("peer-1", 0x02, []byte("err"))
	// Unregistered code error.
	rh.HandleMessage("peer-1", 0xFF, []byte("miss"))

	received, handled, errs := rh.Stats().Snapshot()
	if received != 3 {
		t.Errorf("received = %d, want 3", received)
	}
	if handled != 1 {
		t.Errorf("handled = %d, want 1", handled)
	}
	if errs != 2 {
		t.Errorf("errors = %d, want 2", errs)
	}
}

func TestRequestHandler_UnregisterHandler(t *testing.T) {
	rh := NewRequestHandler()

	rh.RegisterHandler(0x01, &echoHandler{})
	if !rh.HasHandler(0x01) {
		t.Fatal("expected handler to be registered")
	}

	rh.UnregisterHandler(0x01)
	if rh.HasHandler(0x01) {
		t.Fatal("expected handler to be unregistered")
	}

	_, err := rh.HandleMessage("peer-1", 0x01, []byte("test"))
	if !errors.Is(err, ErrNoMessageHandler) {
		t.Errorf("got %v, want ErrNoMessageHandler after unregister", err)
	}
}

func TestRequestHandler_HandlerCount(t *testing.T) {
	rh := NewRequestHandler()

	if rh.HandlerCount() != 0 {
		t.Errorf("initial count = %d, want 0", rh.HandlerCount())
	}

	rh.RegisterHandler(0x01, &echoHandler{})
	rh.RegisterHandler(0x02, &echoHandler{})
	rh.RegisterHandler(0x03, &echoHandler{})

	if rh.HandlerCount() != 3 {
		t.Errorf("count = %d, want 3", rh.HandlerCount())
	}

	rh.UnregisterHandler(0x02)
	if rh.HandlerCount() != 2 {
		t.Errorf("count after unregister = %d, want 2", rh.HandlerCount())
	}
}

func TestRequestHandler_ConcurrentAccess(t *testing.T) {
	rh := NewRequestHandler()

	recorder := &peerRecordingHandler{}
	rh.RegisterHandler(0x01, recorder)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			rh.HandleMessage("peer", 0x01, []byte("data"))
		}(i)
	}
	wg.Wait()

	received, handled, _ := rh.Stats().Snapshot()
	if received != 50 {
		t.Errorf("received = %d, want 50", received)
	}
	if handled != 50 {
		t.Errorf("handled = %d, want 50", handled)
	}

	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.peerIDs) != 50 {
		t.Errorf("recorded peer IDs = %d, want 50", len(recorder.peerIDs))
	}
}

func TestRequestHandler_MessageHandlerFunc(t *testing.T) {
	rh := NewRequestHandler()

	fn := MessageHandlerFunc(func(peerID string, data []byte) ([]byte, error) {
		return append([]byte("echo:"), data...), nil
	})
	rh.RegisterHandler(0x10, fn)

	resp, err := rh.HandleMessage("peer-1", 0x10, []byte("world"))
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if string(resp) != "echo:world" {
		t.Errorf("response = %q, want %q", resp, "echo:world")
	}
}

func TestRequestHandler_DisabledTimeout(t *testing.T) {
	rh := NewRequestHandler()
	// Non-positive timeout disables timeout enforcement.
	rh.SetTimeout(0)

	rh.RegisterHandler(0x01, &echoHandler{})

	resp, err := rh.HandleMessage("peer-1", 0x01, []byte("no-timeout"))
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if string(resp) != "no-timeout" {
		t.Errorf("response = %q, want %q", resp, "no-timeout")
	}
}

func TestRequestHandler_PeerIDPassedThrough(t *testing.T) {
	rh := NewRequestHandler()

	recorder := &peerRecordingHandler{}
	rh.RegisterHandler(0x01, recorder)

	rh.HandleMessage("alpha", 0x01, nil)
	rh.HandleMessage("beta", 0x01, nil)

	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.peerIDs) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(recorder.peerIDs))
	}
	if recorder.peerIDs[0] != "alpha" {
		t.Errorf("first peerID = %q, want %q", recorder.peerIDs[0], "alpha")
	}
	if recorder.peerIDs[1] != "beta" {
		t.Errorf("second peerID = %q, want %q", recorder.peerIDs[1], "beta")
	}
}

func TestRequestHandler_NilResponseIsValid(t *testing.T) {
	rh := NewRequestHandler()

	// A handler that returns nil response and nil error is valid (fire-and-forget).
	rh.RegisterHandler(0x01, MessageHandlerFunc(func(peerID string, data []byte) ([]byte, error) {
		return nil, nil
	}))

	resp, err := rh.HandleMessage("peer-1", 0x01, []byte("data"))
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if resp != nil {
		t.Errorf("expected nil response, got %v", resp)
	}

	_, handled, _ := rh.Stats().Snapshot()
	if handled != 1 {
		t.Errorf("handled = %d, want 1", handled)
	}
}
