package rpc

import (
	"encoding/json"
	"errors"
	"testing"
)

// callAdminDispatch is a test helper for AdminDispatchAPI.
func callAdminDispatch(t *testing.T, a *AdminDispatchAPI, method string, params ...interface{}) *Response {
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
	return a.HandleAdminRequest(req)
}

// --- admin_addPeer tests ---

func TestAdminDispatch_AddPeer(t *testing.T) {
	mb := newMockAdminBackend()
	api := NewAdminDispatchAPI(mb)

	resp := callAdminDispatch(t, api, "admin_addPeer", "enode://abc@127.0.0.1:30303")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	if resp.Result != true {
		t.Fatalf("expected true, got %v", resp.Result)
	}
}

func TestAdminDispatch_AddPeer_Error(t *testing.T) {
	mb := newMockAdminBackend()
	mb.addPeerErr = errors.New("dial failed")
	api := NewAdminDispatchAPI(mb)

	resp := callAdminDispatch(t, api, "admin_addPeer", "enode://abc@127.0.0.1:30303")
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

func TestAdminDispatch_AddPeer_MissingParam(t *testing.T) {
	api := NewAdminDispatchAPI(newMockAdminBackend())
	resp := callAdminDispatch(t, api, "admin_addPeer")
	if resp.Error == nil {
		t.Fatal("expected error for missing param")
	}
	if resp.Error.Code != ErrCodeInvalidParams {
		t.Fatalf("want error code %d, got %d", ErrCodeInvalidParams, resp.Error.Code)
	}
}

func TestAdminDispatch_AddPeer_EmptyURL(t *testing.T) {
	api := NewAdminDispatchAPI(newMockAdminBackend())
	resp := callAdminDispatch(t, api, "admin_addPeer", "")
	if resp.Error == nil {
		t.Fatal("expected error for empty URL")
	}
}

// --- admin_removePeer tests ---

func TestAdminDispatch_RemovePeer(t *testing.T) {
	api := NewAdminDispatchAPI(newMockAdminBackend())
	resp := callAdminDispatch(t, api, "admin_removePeer", "enode://abc@127.0.0.1:30303")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	if resp.Result != true {
		t.Fatalf("expected true, got %v", resp.Result)
	}
}

func TestAdminDispatch_RemovePeer_Error(t *testing.T) {
	mb := newMockAdminBackend()
	mb.removePeerErr = errors.New("peer not found")
	api := NewAdminDispatchAPI(mb)

	resp := callAdminDispatch(t, api, "admin_removePeer", "enode://abc@127.0.0.1:30303")
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

func TestAdminDispatch_RemovePeer_MissingParam(t *testing.T) {
	api := NewAdminDispatchAPI(newMockAdminBackend())
	resp := callAdminDispatch(t, api, "admin_removePeer")
	if resp.Error == nil {
		t.Fatal("expected error for missing param")
	}
	if resp.Error.Code != ErrCodeInvalidParams {
		t.Fatalf("want error code %d, got %d", ErrCodeInvalidParams, resp.Error.Code)
	}
}

// --- admin_peers tests ---

func TestAdminDispatch_Peers(t *testing.T) {
	api := NewAdminDispatchAPI(newMockAdminBackend())
	resp := callAdminDispatch(t, api, "admin_peers")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	peers, ok := resp.Result.([]PeerInfoData)
	if !ok {
		t.Fatalf("result not []PeerInfoData: %T", resp.Result)
	}
	if len(peers) != 2 {
		t.Fatalf("want 2 peers, got %d", len(peers))
	}
	if peers[0].ID != "peer1" {
		t.Fatalf("want peer1, got %s", peers[0].ID)
	}
}

func TestAdminDispatch_Peers_NilBackend(t *testing.T) {
	api := NewAdminDispatchAPI(nil)
	resp := callAdminDispatch(t, api, "admin_peers")
	if resp.Error == nil {
		t.Fatal("expected error for nil backend")
	}
}

// --- admin_nodeInfo tests ---

func TestAdminDispatch_NodeInfo(t *testing.T) {
	api := NewAdminDispatchAPI(newMockAdminBackend())
	resp := callAdminDispatch(t, api, "admin_nodeInfo")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	info, ok := resp.Result.(*NodeInfoData)
	if !ok {
		t.Fatalf("result not *NodeInfoData: %T", resp.Result)
	}
	if info.Name != "eth2028/v0.1.0/linux-amd64/go1.22" {
		t.Fatalf("want eth2028 name, got %s", info.Name)
	}
	if info.Enode != "enode://abc123@127.0.0.1:30303" {
		t.Fatalf("want enode string, got %s", info.Enode)
	}
}

func TestAdminDispatch_NodeInfo_NilBackend(t *testing.T) {
	api := NewAdminDispatchAPI(nil)
	resp := callAdminDispatch(t, api, "admin_nodeInfo")
	if resp.Error == nil {
		t.Fatal("expected error for nil backend")
	}
}

// --- admin_datadir tests ---

func TestAdminDispatch_Datadir(t *testing.T) {
	api := NewAdminDispatchAPI(newMockAdminBackend())
	resp := callAdminDispatch(t, api, "admin_datadir")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	dir, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	if dir != "/tmp/eth2028-data" {
		t.Fatalf("want /tmp/eth2028-data, got %s", dir)
	}
}

func TestAdminDispatch_Datadir_NilBackend(t *testing.T) {
	api := NewAdminDispatchAPI(nil)
	resp := callAdminDispatch(t, api, "admin_datadir")
	if resp.Error == nil {
		t.Fatal("expected error for nil backend")
	}
}

// --- admin_startRPC tests ---

func TestAdminDispatch_StartRPC(t *testing.T) {
	api := NewAdminDispatchAPI(newMockAdminBackend())
	resp := callAdminDispatch(t, api, "admin_startRPC", "127.0.0.1", 8545)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	if resp.Result != true {
		t.Fatalf("expected true, got %v", resp.Result)
	}
}

func TestAdminDispatch_StartRPC_MissingParams(t *testing.T) {
	api := NewAdminDispatchAPI(newMockAdminBackend())
	resp := callAdminDispatch(t, api, "admin_startRPC", "127.0.0.1")
	if resp.Error == nil {
		t.Fatal("expected error for missing port")
	}
	if resp.Error.Code != ErrCodeInvalidParams {
		t.Fatalf("want error code %d, got %d", ErrCodeInvalidParams, resp.Error.Code)
	}
}

func TestAdminDispatch_StartRPC_InvalidPort(t *testing.T) {
	api := NewAdminDispatchAPI(newMockAdminBackend())
	resp := callAdminDispatch(t, api, "admin_startRPC", "127.0.0.1", 0)
	if resp.Error == nil {
		t.Fatal("expected error for invalid port")
	}
}

// --- admin_stopRPC tests ---

func TestAdminDispatch_StopRPC(t *testing.T) {
	api := NewAdminDispatchAPI(newMockAdminBackend())
	resp := callAdminDispatch(t, api, "admin_stopRPC")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	if resp.Result != true {
		t.Fatalf("expected true, got %v", resp.Result)
	}
}

// --- admin_chainId tests ---

func TestAdminDispatch_ChainId(t *testing.T) {
	api := NewAdminDispatchAPI(newMockAdminBackend())
	resp := callAdminDispatch(t, api, "admin_chainId")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	id, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	if id != "0x539" { // 1337
		t.Fatalf("want 0x539, got %s", id)
	}
}

// --- Unknown method ---

func TestAdminDispatch_UnknownMethod(t *testing.T) {
	api := NewAdminDispatchAPI(newMockAdminBackend())
	resp := callAdminDispatch(t, api, "admin_nonexistent")
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != ErrCodeMethodNotFound {
		t.Fatalf("want error code %d, got %d", ErrCodeMethodNotFound, resp.Error.Code)
	}
}

// --- Constructor tests ---

func TestNewAdminDispatchAPI(t *testing.T) {
	mb := newMockAdminBackend()
	api := NewAdminDispatchAPI(mb)
	if api == nil {
		t.Fatal("expected non-nil API")
	}
	if api.inner == nil {
		t.Fatal("expected non-nil inner AdminAPI")
	}
}

func TestNewAdminDispatchAPI_NilBackend(t *testing.T) {
	api := NewAdminDispatchAPI(nil)
	if api == nil {
		t.Fatal("expected non-nil API even with nil backend")
	}
}
