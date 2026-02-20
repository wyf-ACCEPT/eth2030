package rpc

import (
	"encoding/json"
	"errors"
	"testing"
)

// mockNetBackend implements NetBackend for testing.
type mockNetBackend struct {
	networkID  uint64
	listening  bool
	peerCount  int
	maxPeers   int
}

func newMockNetBackend() *mockNetBackend {
	return &mockNetBackend{
		networkID: 1337,
		listening: true,
		peerCount: 25,
		maxPeers:  50,
	}
}

func (b *mockNetBackend) NetworkID() uint64  { return b.networkID }
func (b *mockNetBackend) IsListening() bool  { return b.listening }
func (b *mockNetBackend) PeerCount() int     { return b.peerCount }
func (b *mockNetBackend) MaxPeers() int      { return b.maxPeers }

// callNet is a test helper for NetAPI dispatch.
func callNet(t *testing.T, n *NetAPI, method string, params ...interface{}) *Response {
	t.Helper()
	var rawParams []json.RawMessage
	for _, p := range params {
		b, _ := json.Marshal(p)
		rawParams = append(rawParams, json.RawMessage(b))
	}
	req := &Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  rawParams,
		ID:      json.RawMessage(`1`),
	}
	return n.HandleNetRequest(req)
}

// --- net_version tests ---

func TestNetAPI_Version_Dispatch(t *testing.T) {
	api := NewNetAPI(newMockNetBackend())
	resp := callNet(t, api, "net_version")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	version, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	if version != "1337" {
		t.Fatalf("want 1337, got %s", version)
	}
}

func TestNetAPI_Version_Mainnet(t *testing.T) {
	mb := newMockNetBackend()
	mb.networkID = 1
	api := NewNetAPI(mb)

	resp := callNet(t, api, "net_version")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	if resp.Result != "1" {
		t.Fatalf("want 1, got %v", resp.Result)
	}
}

func TestNetAPI_Version_NilBackend(t *testing.T) {
	api := NewNetAPI(nil)
	resp := callNet(t, api, "net_version")
	if resp.Error == nil {
		t.Fatal("expected error for nil backend")
	}
}

func TestNetAPI_Version_Direct(t *testing.T) {
	api := NewNetAPI(newMockNetBackend())
	v, err := api.Version()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != "1337" {
		t.Fatalf("want 1337, got %s", v)
	}
}

func TestNetAPI_Version_Direct_NilBackend(t *testing.T) {
	api := NewNetAPI(nil)
	_, err := api.Version()
	if !errors.Is(err, ErrNetBackendNil) {
		t.Fatalf("want ErrNetBackendNil, got %v", err)
	}
}

// --- net_listening tests ---

func TestNetAPI_Listening_Dispatch(t *testing.T) {
	api := NewNetAPI(newMockNetBackend())
	resp := callNet(t, api, "net_listening")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	if resp.Result != true {
		t.Fatalf("expected true, got %v", resp.Result)
	}
}

func TestNetAPI_Listening_False(t *testing.T) {
	mb := newMockNetBackend()
	mb.listening = false
	api := NewNetAPI(mb)

	resp := callNet(t, api, "net_listening")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	if resp.Result != false {
		t.Fatalf("expected false, got %v", resp.Result)
	}
}

func TestNetAPI_Listening_NilBackend(t *testing.T) {
	api := NewNetAPI(nil)
	resp := callNet(t, api, "net_listening")
	if resp.Error == nil {
		t.Fatal("expected error for nil backend")
	}
}

func TestNetAPI_Listening_Direct(t *testing.T) {
	api := NewNetAPI(newMockNetBackend())
	listening, err := api.Listening()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !listening {
		t.Fatal("expected true")
	}
}

func TestNetAPI_Listening_Direct_NilBackend(t *testing.T) {
	api := NewNetAPI(nil)
	_, err := api.Listening()
	if !errors.Is(err, ErrNetBackendNil) {
		t.Fatalf("want ErrNetBackendNil, got %v", err)
	}
}

// --- net_peerCount tests ---

func TestNetAPI_PeerCount_Dispatch(t *testing.T) {
	api := NewNetAPI(newMockNetBackend())
	resp := callNet(t, api, "net_peerCount")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	count, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	if count != "0x19" { // 25
		t.Fatalf("want 0x19, got %s", count)
	}
}

func TestNetAPI_PeerCount_Zero(t *testing.T) {
	mb := newMockNetBackend()
	mb.peerCount = 0
	api := NewNetAPI(mb)

	resp := callNet(t, api, "net_peerCount")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	if resp.Result != "0x0" {
		t.Fatalf("want 0x0, got %v", resp.Result)
	}
}

func TestNetAPI_PeerCount_NilBackend(t *testing.T) {
	api := NewNetAPI(nil)
	resp := callNet(t, api, "net_peerCount")
	if resp.Error == nil {
		t.Fatal("expected error for nil backend")
	}
}

func TestNetAPI_PeerCount_Direct(t *testing.T) {
	api := NewNetAPI(newMockNetBackend())
	count, err := api.PeerCount()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 25 {
		t.Fatalf("want 25, got %d", count)
	}
}

func TestNetAPI_PeerCount_Direct_NilBackend(t *testing.T) {
	api := NewNetAPI(nil)
	_, err := api.PeerCount()
	if !errors.Is(err, ErrNetBackendNil) {
		t.Fatalf("want ErrNetBackendNil, got %v", err)
	}
}

// --- net_maxPeers tests ---

func TestNetAPI_MaxPeers_Dispatch(t *testing.T) {
	api := NewNetAPI(newMockNetBackend())
	resp := callNet(t, api, "net_maxPeers")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	max, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	if max != "0x32" { // 50
		t.Fatalf("want 0x32, got %s", max)
	}
}

func TestNetAPI_MaxPeers_NilBackend(t *testing.T) {
	api := NewNetAPI(nil)
	resp := callNet(t, api, "net_maxPeers")
	if resp.Error == nil {
		t.Fatal("expected error for nil backend")
	}
}

// --- Unknown method ---

func TestNetAPI_UnknownMethod(t *testing.T) {
	api := NewNetAPI(newMockNetBackend())
	resp := callNet(t, api, "net_nonexistent")
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != ErrCodeMethodNotFound {
		t.Fatalf("want error code %d, got %d", ErrCodeMethodNotFound, resp.Error.Code)
	}
}

// --- Constructor tests ---

func TestNewNetAPI(t *testing.T) {
	mb := newMockNetBackend()
	api := NewNetAPI(mb)
	if api == nil {
		t.Fatal("expected non-nil API")
	}
	if api.backend != mb {
		t.Fatal("backend not set correctly")
	}
}

func TestNewNetAPI_NilBackend(t *testing.T) {
	api := NewNetAPI(nil)
	if api == nil {
		t.Fatal("expected non-nil API even with nil backend")
	}
}
