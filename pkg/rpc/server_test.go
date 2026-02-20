package rpc

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestServerHandler_ValidRequest tests the server handles a valid JSON-RPC
// POST request and returns a proper response.
func TestServerHandler_ValidRequest(t *testing.T) {
	srv := NewServer(newMockBackend())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := `{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}`
	resp, err := http.Post(ts.URL, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("HTTP POST failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("want Content-Type application/json, got %s", ct)
	}

	var rpcResp Response
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("RPC error: %s", rpcResp.Error.Message)
	}
	if rpcResp.JSONRPC != "2.0" {
		t.Fatalf("want jsonrpc 2.0, got %s", rpcResp.JSONRPC)
	}
}

// TestServerHandler_MethodNotAllowed tests that GET returns 405.
func TestServerHandler_MethodNotAllowed(t *testing.T) {
	srv := NewServer(newMockBackend())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("HTTP GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", resp.StatusCode)
	}
}

// TestServerHandler_InvalidJSON tests that invalid JSON returns a parse error.
func TestServerHandler_InvalidJSON(t *testing.T) {
	srv := NewServer(newMockBackend())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := `not-json`
	resp, err := http.Post(ts.URL, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("HTTP POST failed: %v", err)
	}
	defer resp.Body.Close()

	var rpcResp Response
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected parse error")
	}
	if rpcResp.Error.Code != ErrCodeParse {
		t.Fatalf("want error code %d, got %d", ErrCodeParse, rpcResp.Error.Code)
	}
}

// TestServerHandler_MethodNotFound tests that an unknown method returns the
// correct error code.
func TestServerHandler_MethodNotFound(t *testing.T) {
	srv := NewServer(newMockBackend())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := `{"jsonrpc":"2.0","method":"eth_unknown","params":[],"id":1}`
	resp, err := http.Post(ts.URL, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("HTTP POST failed: %v", err)
	}
	defer resp.Body.Close()

	var rpcResp Response
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected method not found error")
	}
	if rpcResp.Error.Code != ErrCodeMethodNotFound {
		t.Fatalf("want error code %d, got %d", ErrCodeMethodNotFound, rpcResp.Error.Code)
	}
}

// TestServerHandler_MultipleRequests tests that the server handles multiple
// sequential requests correctly.
func TestServerHandler_MultipleRequests(t *testing.T) {
	srv := NewServer(newMockBackend())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	methods := []string{"eth_chainId", "eth_blockNumber", "eth_gasPrice"}
	for _, method := range methods {
		body := `{"jsonrpc":"2.0","method":"` + method + `","params":[],"id":1}`
		resp, err := http.Post(ts.URL, "application/json", bytes.NewBufferString(body))
		if err != nil {
			t.Fatalf("HTTP POST for %s: %v", method, err)
		}

		var rpcResp Response
		if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
			t.Fatalf("decode response for %s: %v", method, err)
		}
		resp.Body.Close()

		if rpcResp.Error != nil {
			t.Fatalf("RPC error for %s: %s", method, rpcResp.Error.Message)
		}
	}
}

// TestServerHandler_EmptyBody tests the server with an empty request body.
func TestServerHandler_EmptyBody(t *testing.T) {
	srv := NewServer(newMockBackend())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL, "application/json", bytes.NewBufferString(""))
	if err != nil {
		t.Fatalf("HTTP POST failed: %v", err)
	}
	defer resp.Body.Close()

	var rpcResp Response
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// Empty body is not valid JSON, so should return a parse error.
	if rpcResp.Error == nil {
		t.Fatal("expected error for empty body")
	}
}

// TestNewServer creates a server and verifies the handler is non-nil.
func TestNewServer(t *testing.T) {
	srv := NewServer(newMockBackend())
	if srv == nil {
		t.Fatal("NewServer returned nil")
	}
	if srv.Handler() == nil {
		t.Fatal("Handler() returned nil")
	}
}
