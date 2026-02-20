package p2p

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMethodIDString(t *testing.T) {
	tests := []struct {
		method MethodID
		want   string
	}{
		{StatusV1, "/eth2/beacon_chain/req/status/1/ssz_snappy"},
		{GoodbyeV1, "/eth2/beacon_chain/req/goodbye/1/ssz_snappy"},
		{BeaconBlocksByRangeV2, "/eth2/beacon_chain/req/beacon_blocks_by_range/2/ssz_snappy"},
		{BeaconBlocksByRootV2, "/eth2/beacon_chain/req/beacon_blocks_by_root/2/ssz_snappy"},
		{BlobSidecarsByRangeV1, "/eth2/beacon_chain/req/blob_sidecars_by_range/1/ssz_snappy"},
		{BlobSidecarsByRootV1, "/eth2/beacon_chain/req/blob_sidecars_by_root/1/ssz_snappy"},
	}
	for _, tt := range tests {
		if got := tt.method.String(); got != tt.want {
			t.Errorf("MethodID(%d).String() = %q, want %q", tt.method, got, tt.want)
		}
	}
}

func TestMethodIDStringUnknown(t *testing.T) {
	unknown := MethodID(999)
	got := unknown.String()
	if got == "" {
		t.Fatal("expected non-empty string for unknown method")
	}
}

func TestResponseCodeString(t *testing.T) {
	tests := []struct {
		code ResponseCode
		want string
	}{
		{RespSuccess, "Success"},
		{RespInvalidRequest, "InvalidRequest"},
		{RespServerError, "ServerError"},
		{RespResourceUnavailable, "ResourceUnavailable"},
		{ResponseCode(99), "ResponseCode(99)"},
	}
	for _, tt := range tests {
		if got := tt.code.String(); got != tt.want {
			t.Errorf("ResponseCode(%d).String() = %q, want %q", tt.code, got, tt.want)
		}
	}
}

func TestDefaultProtocolConfig(t *testing.T) {
	cfg := DefaultProtocolConfig()
	if cfg.DefaultTimeout != 10*time.Second {
		t.Errorf("DefaultTimeout = %v, want 10s", cfg.DefaultTimeout)
	}
	if cfg.RateLimitWindow != 10*time.Second {
		t.Errorf("RateLimitWindow = %v, want 10s", cfg.RateLimitWindow)
	}
	if cfg.RateLimitMaxRequests != 20 {
		t.Errorf("RateLimitMaxRequests = %d, want 20", cfg.RateLimitMaxRequests)
	}

	// Check method-specific timeouts.
	if t1, ok := cfg.MethodTimeouts[StatusV1]; !ok || t1 != 5*time.Second {
		t.Errorf("StatusV1 timeout = %v, want 5s", t1)
	}
	if t2, ok := cfg.MethodTimeouts[BeaconBlocksByRangeV2]; !ok || t2 != 30*time.Second {
		t.Errorf("BeaconBlocksByRangeV2 timeout = %v, want 30s", t2)
	}
}

func TestReqRespProtocolSendRequest(t *testing.T) {
	p := NewReqRespProtocol(DefaultProtocolConfig())
	defer p.Close()

	// Set up mock send function.
	p.SetSendFunc(func(peer string, method MethodID, payload []byte) (*ProtocolResponse, error) {
		return &ProtocolResponse{
			Code:    RespSuccess,
			Payload: []byte("response-data"),
		}, nil
	})

	resp, err := p.SendRequest("peer1", StatusV1, []byte("status-req"))
	if err != nil {
		t.Fatalf("SendRequest: %v", err)
	}
	if resp.Code != RespSuccess {
		t.Errorf("response code = %v, want Success", resp.Code)
	}
	if string(resp.Payload) != "response-data" {
		t.Errorf("payload = %q, want %q", resp.Payload, "response-data")
	}
}

func TestReqRespProtocolSendRequestNoHandler(t *testing.T) {
	p := NewReqRespProtocol(DefaultProtocolConfig())
	defer p.Close()

	_, err := p.SendRequest("peer1", StatusV1, nil)
	if err != ErrProtocolNoHandler {
		t.Fatalf("expected ErrProtocolNoHandler, got %v", err)
	}
}

func TestReqRespProtocolSendRequestClosed(t *testing.T) {
	p := NewReqRespProtocol(DefaultProtocolConfig())
	p.SetSendFunc(func(peer string, method MethodID, payload []byte) (*ProtocolResponse, error) {
		return &ProtocolResponse{Code: RespSuccess}, nil
	})
	p.Close()

	_, err := p.SendRequest("peer1", StatusV1, nil)
	if err != ErrProtocolClosed {
		t.Fatalf("expected ErrProtocolClosed, got %v", err)
	}
}

func TestReqRespProtocolTimeout(t *testing.T) {
	cfg := DefaultProtocolConfig()
	cfg.MethodTimeouts[StatusV1] = 50 * time.Millisecond
	p := NewReqRespProtocol(cfg)
	defer p.Close()

	// Send function that blocks longer than the timeout.
	p.SetSendFunc(func(peer string, method MethodID, payload []byte) (*ProtocolResponse, error) {
		time.Sleep(200 * time.Millisecond)
		return &ProtocolResponse{Code: RespSuccess}, nil
	})

	_, err := p.SendRequest("peer1", StatusV1, nil)
	if err != ErrProtocolTimeout {
		t.Fatalf("expected ErrProtocolTimeout, got %v", err)
	}
}

func TestReqRespProtocolRateLimiting(t *testing.T) {
	cfg := DefaultProtocolConfig()
	cfg.RateLimitWindow = time.Second
	cfg.RateLimitMaxRequests = 3
	p := NewReqRespProtocol(cfg)
	defer p.Close()

	p.SetSendFunc(func(peer string, method MethodID, payload []byte) (*ProtocolResponse, error) {
		return &ProtocolResponse{Code: RespSuccess}, nil
	})

	// Send 3 requests (should all succeed).
	for i := 0; i < 3; i++ {
		_, err := p.SendRequest("peer1", StatusV1, nil)
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}

	// The 4th should be rate limited.
	_, err := p.SendRequest("peer1", StatusV1, nil)
	if err != ErrProtocolRateLimited {
		t.Fatalf("expected ErrProtocolRateLimited, got %v", err)
	}

	// A different peer should not be rate limited.
	_, err = p.SendRequest("peer2", StatusV1, nil)
	if err != nil {
		t.Fatalf("peer2 should not be rate limited: %v", err)
	}

	// A different method for the same peer should not be rate limited.
	_, err = p.SendRequest("peer1", GoodbyeV1, nil)
	if err != nil {
		t.Fatalf("different method should not be rate limited: %v", err)
	}
}

func TestReqRespProtocolConcurrencyLimit(t *testing.T) {
	cfg := DefaultProtocolConfig()
	cfg.RateLimitMaxRequests = 100 // High limit so rate limiting doesn't interfere.
	p := NewReqRespProtocol(cfg)
	defer p.Close()

	// Use a channel to block send functions so we can test concurrency.
	block := make(chan struct{})
	p.SetSendFunc(func(peer string, method MethodID, payload []byte) (*ProtocolResponse, error) {
		<-block
		return &ProtocolResponse{Code: RespSuccess}, nil
	})

	// Start MaxConcurrentRequestsPerProtocol requests that block.
	var wg sync.WaitGroup
	for i := 0; i < MaxConcurrentRequestsPerProtocol; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.SendRequest("peer1", StatusV1, nil)
		}()
	}

	// Give goroutines time to start.
	time.Sleep(50 * time.Millisecond)

	// The next request should fail with concurrency error.
	_, err := p.SendRequest("peer1", StatusV1, nil)
	if err != ErrProtocolConcurrency {
		t.Fatalf("expected ErrProtocolConcurrency, got %v", err)
	}

	// Different peer should work.
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(block)
	}()

	wg.Wait()
}

func TestReqRespProtocolHandleRequest(t *testing.T) {
	p := NewReqRespProtocol(DefaultProtocolConfig())
	defer p.Close()

	p.HandleRequest(StatusV1, func(peer string, payload []byte) *ProtocolResponse {
		return &ProtocolResponse{
			Code:    RespSuccess,
			Payload: []byte("status-response"),
		}
	})

	if !p.HasHandler(StatusV1) {
		t.Fatal("expected StatusV1 handler to be registered")
	}

	resp := p.ProcessIncomingRequest("peer1", StatusV1, []byte("status-req"))
	if resp.Code != RespSuccess {
		t.Errorf("code = %v, want Success", resp.Code)
	}
	if string(resp.Payload) != "status-response" {
		t.Errorf("payload = %q, want %q", resp.Payload, "status-response")
	}
}

func TestReqRespProtocolHandleRequestNoHandler(t *testing.T) {
	p := NewReqRespProtocol(DefaultProtocolConfig())
	defer p.Close()

	resp := p.ProcessIncomingRequest("peer1", StatusV1, nil)
	if resp.Code != RespInvalidRequest {
		t.Errorf("code = %v, want InvalidRequest", resp.Code)
	}
}

func TestReqRespProtocolHandleRequestClosed(t *testing.T) {
	p := NewReqRespProtocol(DefaultProtocolConfig())
	p.HandleRequest(StatusV1, func(peer string, payload []byte) *ProtocolResponse {
		return &ProtocolResponse{Code: RespSuccess}
	})
	p.Close()

	resp := p.ProcessIncomingRequest("peer1", StatusV1, nil)
	if resp.Code != RespServerError {
		t.Errorf("code = %v, want ServerError after close", resp.Code)
	}
}

func TestReqRespProtocolStreamingRequest(t *testing.T) {
	p := NewReqRespProtocol(DefaultProtocolConfig())
	defer p.Close()

	p.SetStreamSendFunc(func(peer string, method MethodID, payload []byte) (*StreamedResponse, error) {
		return &StreamedResponse{
			Chunks: []ResponseChunk{
				{Code: RespSuccess, Payload: []byte("block1")},
				{Code: RespSuccess, Payload: []byte("block2")},
				{Code: RespSuccess, Payload: []byte("block3")},
			},
		}, nil
	})

	resp, err := p.SendStreamingRequest("peer1", BeaconBlocksByRangeV2, []byte("range-req"))
	if err != nil {
		t.Fatalf("SendStreamingRequest: %v", err)
	}
	if len(resp.Chunks) != 3 {
		t.Fatalf("chunks = %d, want 3", len(resp.Chunks))
	}
	if string(resp.Chunks[0].Payload) != "block1" {
		t.Errorf("chunk[0] = %q, want %q", resp.Chunks[0].Payload, "block1")
	}
	if string(resp.Chunks[2].Payload) != "block3" {
		t.Errorf("chunk[2] = %q, want %q", resp.Chunks[2].Payload, "block3")
	}
}

func TestReqRespProtocolStreamingRequestNoHandler(t *testing.T) {
	p := NewReqRespProtocol(DefaultProtocolConfig())
	defer p.Close()

	_, err := p.SendStreamingRequest("peer1", BeaconBlocksByRangeV2, nil)
	if err != ErrProtocolNoHandler {
		t.Fatalf("expected ErrProtocolNoHandler, got %v", err)
	}
}

func TestReqRespProtocolStreamingRequestClosed(t *testing.T) {
	p := NewReqRespProtocol(DefaultProtocolConfig())
	p.SetStreamSendFunc(func(peer string, method MethodID, payload []byte) (*StreamedResponse, error) {
		return &StreamedResponse{}, nil
	})
	p.Close()

	_, err := p.SendStreamingRequest("peer1", BeaconBlocksByRangeV2, nil)
	if err != ErrProtocolClosed {
		t.Fatalf("expected ErrProtocolClosed, got %v", err)
	}
}

func TestReqRespProtocolProcessStreamingRequest(t *testing.T) {
	p := NewReqRespProtocol(DefaultProtocolConfig())
	defer p.Close()

	p.HandleStreamingRequest(BeaconBlocksByRangeV2, func(peer string, payload []byte, chunks chan<- ResponseChunk) {
		for i := 0; i < 5; i++ {
			chunks <- ResponseChunk{
				Code:    RespSuccess,
				Payload: []byte{byte(i)},
			}
		}
	})

	if !p.HasHandler(BeaconBlocksByRangeV2) {
		t.Fatal("expected streaming handler to be registered")
	}

	resp := p.ProcessIncomingStreamingRequest("peer1", BeaconBlocksByRangeV2, nil)
	if len(resp.Chunks) != 5 {
		t.Fatalf("chunks = %d, want 5", len(resp.Chunks))
	}
	for i, chunk := range resp.Chunks {
		if chunk.Code != RespSuccess {
			t.Errorf("chunk[%d] code = %v, want Success", i, chunk.Code)
		}
		if len(chunk.Payload) != 1 || chunk.Payload[0] != byte(i) {
			t.Errorf("chunk[%d] payload mismatch", i)
		}
	}
}

func TestReqRespProtocolProcessStreamingNoHandler(t *testing.T) {
	p := NewReqRespProtocol(DefaultProtocolConfig())
	defer p.Close()

	resp := p.ProcessIncomingStreamingRequest("peer1", BeaconBlocksByRangeV2, nil)
	if len(resp.Chunks) != 1 {
		t.Fatalf("expected 1 error chunk, got %d", len(resp.Chunks))
	}
	if resp.Chunks[0].Code != RespInvalidRequest {
		t.Errorf("code = %v, want InvalidRequest", resp.Chunks[0].Code)
	}
}

func TestReqRespProtocolPendingCount(t *testing.T) {
	p := NewReqRespProtocol(DefaultProtocolConfig())
	defer p.Close()

	if c := p.PendingRequestCount("peer1", StatusV1); c != 0 {
		t.Errorf("initial pending = %d, want 0", c)
	}

	block := make(chan struct{})
	p.SetSendFunc(func(peer string, method MethodID, payload []byte) (*ProtocolResponse, error) {
		<-block
		return &ProtocolResponse{Code: RespSuccess}, nil
	})

	go func() {
		p.SendRequest("peer1", StatusV1, nil)
	}()

	time.Sleep(50 * time.Millisecond)

	if c := p.PendingRequestCount("peer1", StatusV1); c != 1 {
		t.Errorf("pending = %d, want 1", c)
	}

	close(block)
	time.Sleep(50 * time.Millisecond)

	if c := p.PendingRequestCount("peer1", StatusV1); c != 0 {
		t.Errorf("after completion pending = %d, want 0", c)
	}
}

func TestReqRespProtocolConcurrentSendRequests(t *testing.T) {
	cfg := DefaultProtocolConfig()
	cfg.RateLimitMaxRequests = 1000
	p := NewReqRespProtocol(cfg)
	defer p.Close()

	var callCount atomic.Int64
	p.SetSendFunc(func(peer string, method MethodID, payload []byte) (*ProtocolResponse, error) {
		callCount.Add(1)
		return &ProtocolResponse{Code: RespSuccess}, nil
	})

	var wg sync.WaitGroup
	// Use many different peers to avoid concurrency limit.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			peer := "peer" + string(rune('A'+n%26))
			p.SendRequest(peer, StatusV1, nil)
		}(i)
	}

	wg.Wait()

	if c := callCount.Load(); c == 0 {
		t.Fatal("expected some successful requests")
	}
}

func TestReqRespProtocolStatusMessage(t *testing.T) {
	// Verify StatusMessage struct fields match spec.
	msg := StatusMessage{
		ForkDigest:     [4]byte{0xab, 0xcd, 0x12, 0x34},
		FinalizedRoot:  [32]byte{0x01},
		FinalizedEpoch: 100,
		HeadRoot:       [32]byte{0x02},
		HeadSlot:       3200,
	}

	if msg.ForkDigest != [4]byte{0xab, 0xcd, 0x12, 0x34} {
		t.Error("ForkDigest mismatch")
	}
	if msg.FinalizedEpoch != 100 {
		t.Errorf("FinalizedEpoch = %d, want 100", msg.FinalizedEpoch)
	}
	if msg.HeadSlot != 3200 {
		t.Errorf("HeadSlot = %d, want 3200", msg.HeadSlot)
	}
}

func TestReqRespProtocolGoodbyeReason(t *testing.T) {
	if GoodbyeClientShutdown != 1 {
		t.Errorf("GoodbyeClientShutdown = %d, want 1", GoodbyeClientShutdown)
	}
	if GoodbyeIrrelevantNetwork != 2 {
		t.Errorf("GoodbyeIrrelevantNetwork = %d, want 2", GoodbyeIrrelevantNetwork)
	}
	if GoodbyeFaultError != 3 {
		t.Errorf("GoodbyeFaultError = %d, want 3", GoodbyeFaultError)
	}
}

func TestReqRespProtocolHasHandler(t *testing.T) {
	p := NewReqRespProtocol(DefaultProtocolConfig())
	defer p.Close()

	if p.HasHandler(StatusV1) {
		t.Fatal("expected no handler initially")
	}

	p.HandleRequest(StatusV1, func(peer string, payload []byte) *ProtocolResponse {
		return &ProtocolResponse{Code: RespSuccess}
	})

	if !p.HasHandler(StatusV1) {
		t.Fatal("expected handler after registration")
	}

	// Streaming handler should also be detected.
	p.HandleStreamingRequest(BeaconBlocksByRangeV2, func(peer string, payload []byte, chunks chan<- ResponseChunk) {})
	if !p.HasHandler(BeaconBlocksByRangeV2) {
		t.Fatal("expected streaming handler to be detected")
	}
}

func TestReqRespProtocolRateLimitExpiry(t *testing.T) {
	cfg := DefaultProtocolConfig()
	cfg.RateLimitWindow = 50 * time.Millisecond
	cfg.RateLimitMaxRequests = 2
	p := NewReqRespProtocol(cfg)
	defer p.Close()

	p.SetSendFunc(func(peer string, method MethodID, payload []byte) (*ProtocolResponse, error) {
		return &ProtocolResponse{Code: RespSuccess}, nil
	})

	// Use up the rate limit.
	p.SendRequest("peer1", StatusV1, nil)
	p.SendRequest("peer1", StatusV1, nil)

	_, err := p.SendRequest("peer1", StatusV1, nil)
	if err != ErrProtocolRateLimited {
		t.Fatalf("expected ErrProtocolRateLimited, got %v", err)
	}

	// Wait for the window to expire.
	time.Sleep(60 * time.Millisecond)

	_, err = p.SendRequest("peer1", StatusV1, nil)
	if err != nil {
		t.Fatalf("expected request to succeed after window expiry: %v", err)
	}
}

func TestReqRespProtocolStreamingTimeout(t *testing.T) {
	cfg := DefaultProtocolConfig()
	cfg.MethodTimeouts[BeaconBlocksByRangeV2] = 50 * time.Millisecond
	p := NewReqRespProtocol(cfg)
	defer p.Close()

	p.SetStreamSendFunc(func(peer string, method MethodID, payload []byte) (*StreamedResponse, error) {
		time.Sleep(200 * time.Millisecond)
		return &StreamedResponse{}, nil
	})

	_, err := p.SendStreamingRequest("peer1", BeaconBlocksByRangeV2, nil)
	if err != ErrProtocolTimeout {
		t.Fatalf("expected ErrProtocolTimeout, got %v", err)
	}
}

func TestReqRespProtocolConfig(t *testing.T) {
	cfg := DefaultProtocolConfig()
	cfg.DefaultTimeout = 42 * time.Second
	p := NewReqRespProtocol(cfg)
	defer p.Close()

	got := p.Config()
	if got.DefaultTimeout != 42*time.Second {
		t.Errorf("Config().DefaultTimeout = %v, want 42s", got.DefaultTimeout)
	}
}
