package rpc

import (
	"errors"
	"testing"
)

// mockAdminBackend implements AdminBackend for testing.
type mockAdminBackend struct {
	nodeInfo     NodeInfoData
	peers        []PeerInfoData
	addPeerErr   error
	removePeerErr error
	chainID      uint64
	dataDir      string
}

func newMockAdminBackend() *mockAdminBackend {
	return &mockAdminBackend{
		nodeInfo: NodeInfoData{
			Name:       "eth2030/v0.1.0/linux-amd64/go1.22",
			ID:         "abc123def456",
			Enode:      "enode://abc123@127.0.0.1:30303",
			ListenAddr: ":30303",
			Protocols: map[string]interface{}{
				"eth": map[string]interface{}{
					"version": 68,
				},
			},
		},
		peers: []PeerInfoData{
			{
				ID:         "peer1",
				Name:       "geth/v1.13.0",
				RemoteAddr: "192.168.1.1:30303",
				Caps:       []string{"eth/68"},
				Static:     false,
				Trusted:    false,
			},
			{
				ID:         "peer2",
				Name:       "erigon/v2.0.0",
				RemoteAddr: "192.168.1.2:30303",
				Caps:       []string{"eth/67", "eth/68"},
				Static:     true,
				Trusted:    true,
			},
		},
		chainID: 1337,
		dataDir: "/tmp/eth2030-data",
	}
}

func (b *mockAdminBackend) NodeInfo() NodeInfoData     { return b.nodeInfo }
func (b *mockAdminBackend) Peers() []PeerInfoData      { return b.peers }
func (b *mockAdminBackend) AddPeer(url string) error    { return b.addPeerErr }
func (b *mockAdminBackend) RemovePeer(url string) error { return b.removePeerErr }
func (b *mockAdminBackend) ChainID() uint64             { return b.chainID }
func (b *mockAdminBackend) DataDir() string             { return b.dataDir }

// --- AdminNodeInfo tests ---

func TestAdminNodeInfo(t *testing.T) {
	mb := newMockAdminBackend()
	api := NewAdminAPI(mb)

	info, err := api.AdminNodeInfo()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Name != "eth2030/v0.1.0/linux-amd64/go1.22" {
		t.Fatalf("want name eth2030/v0.1.0/linux-amd64/go1.22, got %s", info.Name)
	}
	if info.ID != "abc123def456" {
		t.Fatalf("want ID abc123def456, got %s", info.ID)
	}
	if info.Enode != "enode://abc123@127.0.0.1:30303" {
		t.Fatalf("want enode enode://abc123@127.0.0.1:30303, got %s", info.Enode)
	}
	if info.ListenAddr != ":30303" {
		t.Fatalf("want listenAddr :30303, got %s", info.ListenAddr)
	}
	if info.Protocols == nil {
		t.Fatal("expected non-nil protocols")
	}
	if _, ok := info.Protocols["eth"]; !ok {
		t.Fatal("expected eth protocol entry")
	}
}

func TestAdminNodeInfo_NilBackend(t *testing.T) {
	api := NewAdminAPI(nil)
	_, err := api.AdminNodeInfo()
	if err == nil {
		t.Fatal("expected error for nil backend")
	}
}

// --- AdminPeers tests ---

func TestAdminPeers(t *testing.T) {
	mb := newMockAdminBackend()
	api := NewAdminAPI(mb)

	peers, err := api.AdminPeers()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(peers) != 2 {
		t.Fatalf("want 2 peers, got %d", len(peers))
	}
	if peers[0].ID != "peer1" {
		t.Fatalf("want peer1, got %s", peers[0].ID)
	}
	if peers[1].Static != true {
		t.Fatal("expected peer2 to be static")
	}
	if peers[1].Trusted != true {
		t.Fatal("expected peer2 to be trusted")
	}
	if len(peers[1].Caps) != 2 {
		t.Fatalf("want 2 caps for peer2, got %d", len(peers[1].Caps))
	}
}

func TestAdminPeers_Empty(t *testing.T) {
	mb := newMockAdminBackend()
	mb.peers = nil
	api := NewAdminAPI(mb)

	peers, err := api.AdminPeers()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("want 0 peers, got %d", len(peers))
	}
}

func TestAdminPeers_NilBackend(t *testing.T) {
	api := NewAdminAPI(nil)
	_, err := api.AdminPeers()
	if err == nil {
		t.Fatal("expected error for nil backend")
	}
}

// --- AdminAddPeer tests ---

func TestAdminAddPeer(t *testing.T) {
	mb := newMockAdminBackend()
	api := NewAdminAPI(mb)

	ok, err := api.AdminAddPeer("enode://abc@127.0.0.1:30303")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected true")
	}
}

func TestAdminAddPeer_Error(t *testing.T) {
	mb := newMockAdminBackend()
	mb.addPeerErr = errors.New("dial failed")
	api := NewAdminAPI(mb)

	ok, err := api.AdminAddPeer("enode://abc@127.0.0.1:30303")
	if err == nil {
		t.Fatal("expected error")
	}
	if ok {
		t.Fatal("expected false on error")
	}
	if err.Error() != "dial failed" {
		t.Fatalf("want 'dial failed', got %v", err)
	}
}

func TestAdminAddPeer_EmptyURL(t *testing.T) {
	mb := newMockAdminBackend()
	api := NewAdminAPI(mb)

	ok, err := api.AdminAddPeer("")
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
	if ok {
		t.Fatal("expected false for empty URL")
	}
}

func TestAdminAddPeer_NilBackend(t *testing.T) {
	api := NewAdminAPI(nil)
	_, err := api.AdminAddPeer("enode://abc@127.0.0.1:30303")
	if err == nil {
		t.Fatal("expected error for nil backend")
	}
}

// --- AdminRemovePeer tests ---

func TestAdminRemovePeer(t *testing.T) {
	mb := newMockAdminBackend()
	api := NewAdminAPI(mb)

	ok, err := api.AdminRemovePeer("enode://abc@127.0.0.1:30303")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected true")
	}
}

func TestAdminRemovePeer_Error(t *testing.T) {
	mb := newMockAdminBackend()
	mb.removePeerErr = errors.New("peer not found")
	api := NewAdminAPI(mb)

	ok, err := api.AdminRemovePeer("enode://abc@127.0.0.1:30303")
	if err == nil {
		t.Fatal("expected error")
	}
	if ok {
		t.Fatal("expected false on error")
	}
	if err.Error() != "peer not found" {
		t.Fatalf("want 'peer not found', got %v", err)
	}
}

func TestAdminRemovePeer_EmptyURL(t *testing.T) {
	mb := newMockAdminBackend()
	api := NewAdminAPI(mb)

	ok, err := api.AdminRemovePeer("")
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
	if ok {
		t.Fatal("expected false for empty URL")
	}
}

func TestAdminRemovePeer_NilBackend(t *testing.T) {
	api := NewAdminAPI(nil)
	_, err := api.AdminRemovePeer("enode://abc@127.0.0.1:30303")
	if err == nil {
		t.Fatal("expected error for nil backend")
	}
}

// --- AdminDataDir tests ---

func TestAdminDataDir(t *testing.T) {
	mb := newMockAdminBackend()
	api := NewAdminAPI(mb)

	dir, err := api.AdminDataDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != "/tmp/eth2030-data" {
		t.Fatalf("want /tmp/eth2030-data, got %s", dir)
	}
}

func TestAdminDataDir_NilBackend(t *testing.T) {
	api := NewAdminAPI(nil)
	_, err := api.AdminDataDir()
	if err == nil {
		t.Fatal("expected error for nil backend")
	}
}

// --- AdminStartRPC tests ---

func TestAdminStartRPC(t *testing.T) {
	mb := newMockAdminBackend()
	api := NewAdminAPI(mb)

	ok, err := api.AdminStartRPC("127.0.0.1", 8545)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected true")
	}
}

func TestAdminStartRPC_EmptyHost(t *testing.T) {
	mb := newMockAdminBackend()
	api := NewAdminAPI(mb)

	ok, err := api.AdminStartRPC("", 8545)
	if err == nil {
		t.Fatal("expected error for empty host")
	}
	if ok {
		t.Fatal("expected false for empty host")
	}
}

func TestAdminStartRPC_InvalidPort(t *testing.T) {
	mb := newMockAdminBackend()
	api := NewAdminAPI(mb)

	ok, err := api.AdminStartRPC("127.0.0.1", 0)
	if err == nil {
		t.Fatal("expected error for port 0")
	}
	if ok {
		t.Fatal("expected false for invalid port")
	}

	ok, err = api.AdminStartRPC("127.0.0.1", 70000)
	if err == nil {
		t.Fatal("expected error for port 70000")
	}
	if ok {
		t.Fatal("expected false for invalid port")
	}
}

func TestAdminStartRPC_NilBackend(t *testing.T) {
	api := NewAdminAPI(nil)
	_, err := api.AdminStartRPC("127.0.0.1", 8545)
	if err == nil {
		t.Fatal("expected error for nil backend")
	}
}

// --- AdminStopRPC tests ---

func TestAdminStopRPC(t *testing.T) {
	mb := newMockAdminBackend()
	api := NewAdminAPI(mb)

	ok, err := api.AdminStopRPC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected true")
	}
}

func TestAdminStopRPC_NilBackend(t *testing.T) {
	api := NewAdminAPI(nil)
	_, err := api.AdminStopRPC()
	if err == nil {
		t.Fatal("expected error for nil backend")
	}
}

// --- AdminChainID tests ---

func TestAdminChainID(t *testing.T) {
	mb := newMockAdminBackend()
	api := NewAdminAPI(mb)

	id, err := api.AdminChainID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "0x539" { // 1337
		t.Fatalf("want 0x539, got %s", id)
	}
}

func TestAdminChainID_Mainnet(t *testing.T) {
	mb := newMockAdminBackend()
	mb.chainID = 1
	api := NewAdminAPI(mb)

	id, err := api.AdminChainID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "0x1" {
		t.Fatalf("want 0x1, got %s", id)
	}
}

func TestAdminChainID_NilBackend(t *testing.T) {
	api := NewAdminAPI(nil)
	_, err := api.AdminChainID()
	if err == nil {
		t.Fatal("expected error for nil backend")
	}
}

// --- Comprehensive nil backend coverage ---

func TestNewAdminAPI(t *testing.T) {
	mb := newMockAdminBackend()
	api := NewAdminAPI(mb)
	if api == nil {
		t.Fatal("expected non-nil API")
	}
	if api.backend != mb {
		t.Fatal("backend not set correctly")
	}
}

func TestNewAdminAPI_NilBackend(t *testing.T) {
	api := NewAdminAPI(nil)
	if api == nil {
		t.Fatal("expected non-nil API even with nil backend")
	}
}
