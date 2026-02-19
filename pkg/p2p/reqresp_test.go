package p2p

import (
	"bytes"
	"testing"
)

func TestEncodeDecodeRequest(t *testing.T) {
	codec := NewReqRespCodec(DefaultReqRespConfig())
	payload := []byte("hello world")

	req, data, err := codec.EncodeRequest("eth_getBlock", payload)
	if err != nil {
		t.Fatalf("EncodeRequest: %v", err)
	}
	if req.Method != "eth_getBlock" {
		t.Fatalf("method: got %q, want %q", req.Method, "eth_getBlock")
	}
	if !bytes.Equal(req.Payload, payload) {
		t.Fatal("payload mismatch")
	}

	decoded, err := codec.DecodeRequest(data)
	if err != nil {
		t.Fatalf("DecodeRequest: %v", err)
	}
	if decoded.ID != req.ID {
		t.Fatalf("ID: got %d, want %d", decoded.ID, req.ID)
	}
	if decoded.Method != req.Method {
		t.Fatalf("Method: got %q, want %q", decoded.Method, req.Method)
	}
	if !bytes.Equal(decoded.Payload, payload) {
		t.Fatal("decoded payload mismatch")
	}
}

func TestEncodeDecodeResponse(t *testing.T) {
	codec := NewReqRespCodec(DefaultReqRespConfig())
	payload := []byte("response data")

	respData, err := codec.EncodeResponse(42, "eth_getBlock", payload, "")
	if err != nil {
		t.Fatalf("EncodeResponse: %v", err)
	}

	decoded, err := codec.DecodeResponse(respData)
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	if decoded.ID != 42 {
		t.Fatalf("ID: got %d, want 42", decoded.ID)
	}
	if decoded.Method != "eth_getBlock" {
		t.Fatalf("Method: got %q, want %q", decoded.Method, "eth_getBlock")
	}
	if !bytes.Equal(decoded.Payload, payload) {
		t.Fatal("payload mismatch")
	}
	if decoded.Error != "" {
		t.Fatalf("expected empty error, got %q", decoded.Error)
	}
}

func TestEncodeResponseError(t *testing.T) {
	codec := NewReqRespCodec(DefaultReqRespConfig())

	respData, err := codec.EncodeResponse(99, "eth_getBlock", nil, "block not found")
	if err != nil {
		t.Fatalf("EncodeResponse: %v", err)
	}

	decoded, err := codec.DecodeResponse(respData)
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	if decoded.ID != 99 {
		t.Fatalf("ID: got %d, want 99", decoded.ID)
	}
	if decoded.Error != "block not found" {
		t.Fatalf("Error: got %q, want %q", decoded.Error, "block not found")
	}
	if len(decoded.Payload) != 0 {
		t.Fatalf("expected empty payload, got %d bytes", len(decoded.Payload))
	}
}

func TestRequestAutoID(t *testing.T) {
	codec := NewReqRespCodec(DefaultReqRespConfig())

	req1, _, err := codec.EncodeRequest("method1", nil)
	if err != nil {
		t.Fatalf("EncodeRequest 1: %v", err)
	}

	req2, _, err := codec.EncodeRequest("method2", nil)
	if err != nil {
		t.Fatalf("EncodeRequest 2: %v", err)
	}

	req3, _, err := codec.EncodeRequest("method3", nil)
	if err != nil {
		t.Fatalf("EncodeRequest 3: %v", err)
	}

	if req2.ID <= req1.ID {
		t.Fatalf("expected req2.ID > req1.ID, got %d <= %d", req2.ID, req1.ID)
	}
	if req3.ID <= req2.ID {
		t.Fatalf("expected req3.ID > req2.ID, got %d <= %d", req3.ID, req2.ID)
	}
}

func TestMaxRequestSize(t *testing.T) {
	cfg := DefaultReqRespConfig()
	cfg.MaxRequestSize = 100
	codec := NewReqRespCodec(cfg)

	// Within limit.
	_, _, err := codec.EncodeRequest("test", make([]byte, 100))
	if err != nil {
		t.Fatalf("expected request within limit to succeed: %v", err)
	}

	// Over limit.
	_, _, err = codec.EncodeRequest("test", make([]byte, 101))
	if err == nil {
		t.Fatal("expected error for oversized request")
	}
	if err != ErrRequestTooLarge {
		t.Fatalf("expected ErrRequestTooLarge, got %v", err)
	}
}

func TestPendingRequests(t *testing.T) {
	codec := NewReqRespCodec(DefaultReqRespConfig())

	if codec.PendingRequests() != 0 {
		t.Fatalf("expected 0 pending, got %d", codec.PendingRequests())
	}

	// Send 3 requests.
	req1, _, _ := codec.EncodeRequest("m1", nil)
	req2, _, _ := codec.EncodeRequest("m2", nil)
	_, _, _ = codec.EncodeRequest("m3", nil)

	if codec.PendingRequests() != 3 {
		t.Fatalf("expected 3 pending, got %d", codec.PendingRequests())
	}

	// Respond to one via EncodeResponse (removes from pending).
	_, err := codec.EncodeResponse(req1.ID, "m1", nil, "")
	if err != nil {
		t.Fatalf("EncodeResponse: %v", err)
	}
	if codec.PendingRequests() != 2 {
		t.Fatalf("expected 2 pending after response, got %d", codec.PendingRequests())
	}

	// Decode a response also removes from pending.
	respData, _ := codec.EncodeResponse(req2.ID, "m2", nil, "")
	_, err = codec.DecodeResponse(respData)
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	if codec.PendingRequests() != 1 {
		t.Fatalf("expected 1 pending after decode, got %d", codec.PendingRequests())
	}
}
