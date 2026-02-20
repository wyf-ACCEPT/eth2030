package p2p

import (
	"testing"
	"time"
)

func TestRequestManager_SendAndDeliverResponse(t *testing.T) {
	cfg := DefaultRequestManagerConfig()
	rm := NewRequestManager(cfg)
	defer rm.Close()

	id, respCh, err := rm.SendRequest("peer1", GetBlockHeadersMsg, []byte("test"))
	if err != nil {
		t.Fatalf("SendRequest failed: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero request ID")
	}
	if rm.PendingCount() != 1 {
		t.Fatalf("expected 1 pending, got %d", rm.PendingCount())
	}

	// Deliver response.
	err = rm.DeliverResponse(id, []byte("response-data"))
	if err != nil {
		t.Fatalf("DeliverResponse failed: %v", err)
	}

	// Read from channel.
	select {
	case data := <-respCh:
		if string(data) != "response-data" {
			t.Fatalf("expected 'response-data', got '%s'", string(data))
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for response")
	}

	if rm.PendingCount() != 0 {
		t.Fatalf("expected 0 pending after delivery, got %d", rm.PendingCount())
	}
}

func TestRequestManager_DeliverResponseNotFound(t *testing.T) {
	cfg := DefaultRequestManagerConfig()
	rm := NewRequestManager(cfg)
	defer rm.Close()

	err := rm.DeliverResponse(999, []byte("data"))
	if err != ErrReqMgrNotFound {
		t.Fatalf("expected ErrReqMgrNotFound, got %v", err)
	}
}

func TestRequestManager_CancelRequest(t *testing.T) {
	cfg := DefaultRequestManagerConfig()
	rm := NewRequestManager(cfg)
	defer rm.Close()

	id, respCh, err := rm.SendRequest("peer1", 0x03, nil)
	if err != nil {
		t.Fatalf("SendRequest failed: %v", err)
	}

	rm.CancelRequest(id)

	// Channel should be closed.
	select {
	case _, ok := <-respCh:
		if ok {
			t.Fatal("expected channel to be closed after cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for channel close")
	}

	if rm.PendingCount() != 0 {
		t.Fatalf("expected 0 pending after cancel, got %d", rm.PendingCount())
	}
}

func TestRequestManager_ExpireTimeouts(t *testing.T) {
	cfg := DefaultRequestManagerConfig()
	cfg.DefaultTimeout = 1 * time.Millisecond
	rm := NewRequestManager(cfg)
	defer rm.Close()

	_, respCh, err := rm.SendRequest("peer1", 0x03, nil)
	if err != nil {
		t.Fatalf("SendRequest failed: %v", err)
	}

	// Wait for timeout to pass.
	time.Sleep(10 * time.Millisecond)

	expired := rm.ExpireTimeouts()
	if expired != 1 {
		t.Fatalf("expected 1 expired, got %d", expired)
	}

	// Channel should be closed.
	select {
	case _, ok := <-respCh:
		if ok {
			t.Fatal("expected channel to be closed after expiry")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for channel close")
	}
}

func TestRequestManager_MaxPendingLimit(t *testing.T) {
	cfg := DefaultRequestManagerConfig()
	cfg.MaxPending = 2
	rm := NewRequestManager(cfg)
	defer rm.Close()

	_, _, err := rm.SendRequest("p1", 0x01, nil)
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	_, _, err = rm.SendRequest("p2", 0x01, nil)
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	_, _, err = rm.SendRequest("p3", 0x01, nil)
	if err != ErrReqMgrMaxPending {
		t.Fatalf("expected ErrReqMgrMaxPending, got %v", err)
	}
}

func TestRequestManager_RetryRequest(t *testing.T) {
	cfg := DefaultRequestManagerConfig()
	cfg.MaxRetries = 2
	cfg.BaseBackoff = 100 * time.Millisecond
	rm := NewRequestManager(cfg)
	defer rm.Close()

	id, _, err := rm.SendRequest("peer1", 0x03, []byte("data"))
	if err != nil {
		t.Fatalf("SendRequest failed: %v", err)
	}

	// First retry should succeed.
	backoff, err := rm.RetryRequest(id)
	if err != nil {
		t.Fatalf("first retry failed: %v", err)
	}
	if backoff < 100*time.Millisecond {
		t.Fatalf("expected backoff >= 100ms, got %v", backoff)
	}

	// Second retry should succeed.
	backoff2, err := rm.RetryRequest(id)
	if err != nil {
		t.Fatalf("second retry failed: %v", err)
	}
	// Backoff should increase (exponential).
	if backoff2 <= backoff {
		t.Fatalf("expected increasing backoff, got %v then %v", backoff, backoff2)
	}

	// Third retry should fail (MaxRetries = 2, already retried 2 times).
	_, err = rm.RetryRequest(id)
	if err != ErrReqMgrMaxRetries {
		t.Fatalf("expected ErrReqMgrMaxRetries, got %v", err)
	}
}

func TestRequestManager_RetryNotFound(t *testing.T) {
	cfg := DefaultRequestManagerConfig()
	rm := NewRequestManager(cfg)
	defer rm.Close()

	_, err := rm.RetryRequest(999)
	if err != ErrReqMgrNotFound {
		t.Fatalf("expected ErrReqMgrNotFound, got %v", err)
	}
}

func TestRequestManager_BackoffDuration(t *testing.T) {
	cfg := DefaultRequestManagerConfig()
	cfg.BaseBackoff = 100 * time.Millisecond
	cfg.MaxBackoff = 5 * time.Second
	rm := NewRequestManager(cfg)
	defer rm.Close()

	// Attempt 1: 100ms * 2^0 = 100ms
	b1 := rm.BackoffDuration(1)
	if b1 != 100*time.Millisecond {
		t.Fatalf("expected 100ms for attempt 1, got %v", b1)
	}

	// Attempt 2: 100ms * 2^1 = 200ms
	b2 := rm.BackoffDuration(2)
	if b2 != 200*time.Millisecond {
		t.Fatalf("expected 200ms for attempt 2, got %v", b2)
	}

	// Attempt 3: 100ms * 2^2 = 400ms
	b3 := rm.BackoffDuration(3)
	if b3 != 400*time.Millisecond {
		t.Fatalf("expected 400ms for attempt 3, got %v", b3)
	}

	// Large attempt: should be capped at MaxBackoff.
	bLarge := rm.BackoffDuration(100)
	if bLarge != 5*time.Second {
		t.Fatalf("expected backoff capped at 5s, got %v", bLarge)
	}
}

func TestRequestManager_PendingForPeer(t *testing.T) {
	cfg := DefaultRequestManagerConfig()
	rm := NewRequestManager(cfg)
	defer rm.Close()

	rm.SendRequest("peer1", 0x01, nil)
	rm.SendRequest("peer1", 0x02, nil)
	rm.SendRequest("peer2", 0x01, nil)

	if rm.PendingForPeer("peer1") != 2 {
		t.Fatalf("expected 2 pending for peer1, got %d", rm.PendingForPeer("peer1"))
	}
	if rm.PendingForPeer("peer2") != 1 {
		t.Fatalf("expected 1 pending for peer2, got %d", rm.PendingForPeer("peer2"))
	}
	if rm.PendingForPeer("peer3") != 0 {
		t.Fatalf("expected 0 pending for peer3, got %d", rm.PendingForPeer("peer3"))
	}
}

func TestRequestManager_GetRequest(t *testing.T) {
	cfg := DefaultRequestManagerConfig()
	rm := NewRequestManager(cfg)
	defer rm.Close()

	id, _, _ := rm.SendRequest("peer1", 0x03, []byte("payload"))

	req := rm.GetRequest(id)
	if req == nil {
		t.Fatal("expected non-nil request")
	}
	if req.PeerID != "peer1" {
		t.Fatalf("expected peer1, got %s", req.PeerID)
	}
	if req.MsgCode != 0x03 {
		t.Fatalf("expected msg code 0x03, got 0x%x", req.MsgCode)
	}
	if string(req.Data) != "payload" {
		t.Fatalf("expected 'payload', got '%s'", string(req.Data))
	}
	// RespCh should be nil in the copy.
	if req.RespCh != nil {
		t.Fatal("expected nil RespCh in copy")
	}
}

func TestRequestManager_GetRequestNotFound(t *testing.T) {
	cfg := DefaultRequestManagerConfig()
	rm := NewRequestManager(cfg)
	defer rm.Close()

	req := rm.GetRequest(999)
	if req != nil {
		t.Fatal("expected nil for non-existent request")
	}
}

func TestRequestManager_ClosePreventsNewRequests(t *testing.T) {
	cfg := DefaultRequestManagerConfig()
	rm := NewRequestManager(cfg)

	rm.Close()

	_, _, err := rm.SendRequest("peer1", 0x01, nil)
	if err != ErrReqMgrClosed {
		t.Fatalf("expected ErrReqMgrClosed, got %v", err)
	}
}

func TestRequestManager_CloseCleansPending(t *testing.T) {
	cfg := DefaultRequestManagerConfig()
	rm := NewRequestManager(cfg)

	_, respCh, _ := rm.SendRequest("peer1", 0x01, nil)
	rm.Close()

	// Channel should be closed.
	select {
	case _, ok := <-respCh:
		if ok {
			t.Fatal("expected channel to be closed after Close")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for channel close")
	}
}
