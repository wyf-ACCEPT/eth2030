package rpc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewWSHandler(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	h := NewWSHandler(api, 10)
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	if h.maxConns != 10 {
		t.Fatalf("want maxConns 10, got %d", h.maxConns)
	}
	if h.ConnectionCount() != 0 {
		t.Fatalf("want 0 connections, got %d", h.ConnectionCount())
	}
}

func TestNewWSHandler_DefaultMaxConns(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	h := NewWSHandler(api, 0)
	if h.maxConns != 100 {
		t.Fatalf("want default maxConns 100, got %d", h.maxConns)
	}
}

func TestWSConn_HandleMessage_SingleRequest(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	h := NewWSHandler(api, 10)
	conn := h.newWSConn()

	msg := `{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}`
	result, err := conn.HandleMessage([]byte(msg))
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}

	var resp Response
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
}

func TestWSConn_HandleMessage_BatchRequest(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	h := NewWSHandler(api, 10)
	conn := h.newWSConn()

	msg := `[
		{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1},
		{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":2}
	]`
	result, err := conn.HandleMessage([]byte(msg))
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}

	var responses []BatchResponse
	if err := json.Unmarshal(result, &responses); err != nil {
		t.Fatalf("unmarshal batch: %v", err)
	}
	if len(responses) != 2 {
		t.Fatalf("want 2 responses, got %d", len(responses))
	}
}

func TestWSConn_HandleMessage_InvalidJSON(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	h := NewWSHandler(api, 10)
	conn := h.newWSConn()

	result, err := conn.HandleMessage([]byte("{invalid json"))
	if err != nil {
		t.Fatalf("expected no Go error, got: %v", err)
	}

	var resp Response
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected JSON-RPC error")
	}
	if resp.Error.Code != ErrCodeParse {
		t.Fatalf("want error code %d, got %d", ErrCodeParse, resp.Error.Code)
	}
}

func TestWSConn_HandleMessage_ClosedConnection(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	h := NewWSHandler(api, 10)
	conn := h.newWSConn()
	conn.Close()

	_, err := conn.HandleMessage([]byte(`{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}`))
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestWSConn_HandleMessage_Subscribe(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	h := NewWSHandler(api, 10)
	conn := h.newWSConn()

	msg := `{"jsonrpc":"2.0","method":"eth_subscribe","params":["newHeads"],"id":1}`
	result, err := conn.HandleMessage([]byte(msg))
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}

	var resp Response
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	if conn.SubscriptionCount() != 1 {
		t.Fatalf("want 1 subscription, got %d", conn.SubscriptionCount())
	}
}

func TestWSConn_HandleMessage_Unsubscribe(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	h := NewWSHandler(api, 10)
	conn := h.newWSConn()

	// Subscribe first.
	subMsg := `{"jsonrpc":"2.0","method":"eth_subscribe","params":["newHeads"],"id":1}`
	result, _ := conn.HandleMessage([]byte(subMsg))

	var subResp Response
	json.Unmarshal(result, &subResp)
	subID, _ := subResp.Result.(string)

	// Unsubscribe.
	unsubMsg := `{"jsonrpc":"2.0","method":"eth_unsubscribe","params":["` + subID + `"],"id":2}`
	result2, err := conn.HandleMessage([]byte(unsubMsg))
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}

	var unsubResp Response
	json.Unmarshal(result2, &unsubResp)
	if unsubResp.Error != nil {
		t.Fatalf("unexpected error: %v", unsubResp.Error.Message)
	}
	if conn.SubscriptionCount() != 0 {
		t.Fatalf("want 0 subscriptions after unsub, got %d", conn.SubscriptionCount())
	}
}

func TestWSConn_MaxSubscriptions(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	h := NewWSHandler(api, 10)
	conn := h.newWSConn()

	// Subscribe up to the limit.
	for i := 0; i < WSMaxSubscriptionsPerConn; i++ {
		msg := fmt.Sprintf(`{"jsonrpc":"2.0","method":"eth_subscribe","params":["newHeads"],"id":%d}`, i+1)
		conn.HandleMessage([]byte(msg))
	}
	if conn.SubscriptionCount() != WSMaxSubscriptionsPerConn {
		t.Fatalf("want %d subscriptions, got %d", WSMaxSubscriptionsPerConn, conn.SubscriptionCount())
	}

	// Next subscription should fail.
	msg := `{"jsonrpc":"2.0","method":"eth_subscribe","params":["newHeads"],"id":999}`
	result, _ := conn.HandleMessage([]byte(msg))
	var resp Response
	json.Unmarshal(result, &resp)
	if resp.Error == nil {
		t.Fatal("expected error when max subscriptions reached")
	}
}

func TestRateBucket_Allow(t *testing.T) {
	rb := newRateBucket(5, time.Second)
	for i := 0; i < 5; i++ {
		if !rb.Allow() {
			t.Fatalf("request %d should be allowed", i)
		}
	}
	// 6th should be denied.
	if rb.Allow() {
		t.Fatal("request should be rate limited")
	}
}

func TestRateBucket_Refill(t *testing.T) {
	rb := newRateBucket(2, 10*time.Millisecond)
	rb.Allow()
	rb.Allow()
	if rb.Allow() {
		t.Fatal("should be rate limited")
	}

	// Wait for refill.
	time.Sleep(15 * time.Millisecond)
	if !rb.Allow() {
		t.Fatal("should be allowed after refill")
	}
}

func TestRateBucket_Remaining(t *testing.T) {
	rb := newRateBucket(5, time.Second)
	if rb.Remaining() != 5 {
		t.Fatalf("want 5 remaining, got %d", rb.Remaining())
	}
	rb.Allow()
	if rb.Remaining() != 4 {
		t.Fatalf("want 4 remaining, got %d", rb.Remaining())
	}
}

func TestWSConn_RateLimit(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	h := NewWSHandler(api, 10)
	conn := h.newWSConn()
	// Override rate limiter to a small window.
	conn.rateLimiter = newRateBucket(2, time.Second)

	msg := `{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}`
	// First two should succeed.
	conn.HandleMessage([]byte(msg))
	conn.HandleMessage([]byte(msg))

	// Third should be rate limited.
	result, err := conn.HandleMessage([]byte(msg))
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	var resp Response
	json.Unmarshal(result, &resp)
	if resp.Error == nil {
		t.Fatal("expected rate limit error")
	}
	if resp.Error.Code != -32005 {
		t.Fatalf("want error code -32005, got %d", resp.Error.Code)
	}
}

func TestWSConn_Close(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	h := NewWSHandler(api, 10)
	conn := h.newWSConn()

	if conn.IsClosed() {
		t.Fatal("connection should not be closed initially")
	}
	conn.Close()
	if !conn.IsClosed() {
		t.Fatal("connection should be closed")
	}
	// Closing again should not panic.
	conn.Close()
}

func TestWSConn_ID(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	h := NewWSHandler(api, 10)
	conn1 := h.newWSConn()
	conn2 := h.newWSConn()

	if conn1.ID() == conn2.ID() {
		t.Fatal("connections should have different IDs")
	}
}

func TestWSConn_Info(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	h := NewWSHandler(api, 10)
	conn := h.newWSConn()

	info := conn.Info()
	if info.ID != conn.ID() {
		t.Fatalf("ID mismatch: %d vs %d", info.ID, conn.ID())
	}
	if info.Subscriptions != 0 {
		t.Fatalf("want 0 subscriptions, got %d", info.Subscriptions)
	}
	if info.ConnectedSince == 0 {
		t.Fatal("ConnectedSince should be set")
	}
}

func TestWSHandler_AddRemoveConnection(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	h := NewWSHandler(api, 2)

	conn1 := h.newWSConn()
	conn2 := h.newWSConn()
	conn3 := h.newWSConn()

	if err := h.addConnection(conn1); err != nil {
		t.Fatalf("add conn1: %v", err)
	}
	if err := h.addConnection(conn2); err != nil {
		t.Fatalf("add conn2: %v", err)
	}
	if h.ConnectionCount() != 2 {
		t.Fatalf("want 2, got %d", h.ConnectionCount())
	}

	// Third should fail (max 2).
	if err := h.addConnection(conn3); err == nil {
		t.Fatal("expected error for max connections")
	}

	// Remove one, then add should succeed.
	h.removeConnection(conn1)
	if h.ConnectionCount() != 1 {
		t.Fatalf("want 1, got %d", h.ConnectionCount())
	}
	if err := h.addConnection(conn3); err != nil {
		t.Fatalf("add conn3 after removal: %v", err)
	}
}

func TestWSHandler_RemoveConnection_CleansSubscriptions(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)
	h := NewWSHandler(api, 10)
	conn := h.newWSConn()
	h.addConnection(conn)

	// Create a subscription.
	subMsg := `{"jsonrpc":"2.0","method":"eth_subscribe","params":["newHeads"],"id":1}`
	conn.HandleMessage([]byte(subMsg))
	if conn.SubscriptionCount() != 1 {
		t.Fatal("expected 1 subscription")
	}

	// Remove connection should clean up subscriptions.
	h.removeConnection(conn)
	if h.ConnectionCount() != 0 {
		t.Fatal("expected 0 connections")
	}
}

func TestWSHandler_ServeHTTP_MissingUpgrade(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	h := NewWSHandler(api, 10)

	req := httptest.NewRequest("GET", "/ws", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rr.Code)
	}
}

func TestWSHandler_ServeHTTP_ValidUpgrade(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	h := NewWSHandler(api, 10)

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Upgrade", "websocket")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusSwitchingProtocols {
		t.Fatalf("want 101, got %d", rr.Code)
	}
	if h.ConnectionCount() != 1 {
		t.Fatalf("want 1 connection, got %d", h.ConnectionCount())
	}
}

func TestWSHandler_ServeHTTP_MaxConnections(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	h := NewWSHandler(api, 1)

	// First connection.
	req1 := httptest.NewRequest("GET", "/ws", nil)
	req1.Header.Set("Upgrade", "websocket")
	rr1 := httptest.NewRecorder()
	h.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusSwitchingProtocols {
		t.Fatalf("first: want 101, got %d", rr1.Code)
	}

	// Second connection should be rejected.
	req2 := httptest.NewRequest("GET", "/ws", nil)
	req2.Header.Set("Upgrade", "websocket")
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusServiceUnavailable {
		t.Fatalf("second: want 503, got %d", rr2.Code)
	}
}

func TestPingMessage(t *testing.T) {
	msg := PingMessage()
	if len(msg) < 5 {
		t.Fatal("ping message too short")
	}
	if string(msg[:5]) != "ping-" {
		t.Fatalf("expected ping- prefix, got %s", msg[:5])
	}
}

func TestValidatePong(t *testing.T) {
	if !ValidatePong([]byte("ping-12345")) {
		t.Fatal("expected valid pong")
	}
	if ValidatePong([]byte("pong")) {
		t.Fatal("expected invalid for pong")
	}
	if ValidatePong([]byte("")) {
		t.Fatal("expected invalid for empty")
	}
}

func TestFormatWSError(t *testing.T) {
	data := FormatWSError(json.RawMessage(`1`), ErrCodeInternal, "internal error")
	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != ErrCodeInternal {
		t.Fatalf("want code %d, got %d", ErrCodeInternal, resp.Error.Code)
	}
}

func TestWSConstants(t *testing.T) {
	if WSMaxMessageSize != 1<<20 {
		t.Fatalf("want WSMaxMessageSize 1MB, got %d", WSMaxMessageSize)
	}
	if WSPingInterval != 30*time.Second {
		t.Fatalf("want WSPingInterval 30s, got %v", WSPingInterval)
	}
	if WSRateLimit != 100 {
		t.Fatalf("want WSRateLimit 100, got %d", WSRateLimit)
	}
	if WSMaxSubscriptionsPerConn != 32 {
		t.Fatalf("want WSMaxSubscriptionsPerConn 32, got %d", WSMaxSubscriptionsPerConn)
	}
}
