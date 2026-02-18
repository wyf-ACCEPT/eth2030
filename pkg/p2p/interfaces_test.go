package p2p

import (
	"errors"
	"testing"
)

func TestPeerHandlerFunc(t *testing.T) {
	called := false
	handler := PeerHandlerFunc(func(peer *Peer, rw MsgReadWriter) error {
		called = true
		if peer.ID() != "test-peer" {
			t.Errorf("peer ID = %q, want %q", peer.ID(), "test-peer")
		}
		return nil
	})

	peer := NewPeer("test-peer", "1.2.3.4:30303", nil)
	a, _ := MsgPipe()
	defer a.Close()

	err := handler.HandlePeer(peer, a)
	if err != nil {
		t.Fatalf("HandlePeer: %v", err)
	}
	if !called {
		t.Error("handler was not called")
	}
}

func TestPeerHandlerFuncError(t *testing.T) {
	errTest := errors.New("test error")
	handler := PeerHandlerFunc(func(peer *Peer, rw MsgReadWriter) error {
		return errTest
	})

	peer := NewPeer("test-peer", "1.2.3.4:30303", nil)
	a, _ := MsgPipe()
	defer a.Close()

	err := handler.HandlePeer(peer, a)
	if err != errTest {
		t.Errorf("HandlePeer error = %v, want %v", err, errTest)
	}
}

// TestPeerInfoInterface verifies that *Peer satisfies PeerInfo.
func TestPeerInfoInterface(t *testing.T) {
	caps := []Cap{{Name: "eth", Version: 68}}
	p := NewPeer("node1", "10.0.0.1:30303", caps)
	p.SetVersion(ETH68)

	// Use through the interface.
	var info PeerInfo = p
	if info.ID() != "node1" {
		t.Errorf("ID = %q, want %q", info.ID(), "node1")
	}
	if info.RemoteAddr() != "10.0.0.1:30303" {
		t.Errorf("RemoteAddr = %q, want %q", info.RemoteAddr(), "10.0.0.1:30303")
	}
	if len(info.Caps()) != 1 {
		t.Errorf("Caps count = %d, want 1", len(info.Caps()))
	}
	if info.Version() != ETH68 {
		t.Errorf("Version = %d, want %d", info.Version(), ETH68)
	}
}

// TestPeerSetReaderInterface verifies that *PeerSet satisfies PeerSetReader.
func TestPeerSetReaderInterface(t *testing.T) {
	ps := NewPeerSet()
	p := NewPeer("p1", "1.2.3.4:30303", nil)
	ps.Register(p)

	var reader PeerSetReader = ps
	if reader.Len() != 1 {
		t.Errorf("Len = %d, want 1", reader.Len())
	}
	if reader.Peer("p1") == nil {
		t.Error("Peer(p1) returned nil")
	}
	if len(reader.Peers()) != 1 {
		t.Errorf("Peers count = %d, want 1", len(reader.Peers()))
	}
}

// TestNodeDiscoveryInterface verifies that *NodeTable satisfies NodeDiscovery.
func TestNodeDiscoveryInterface(t *testing.T) {
	nt := NewNodeTable()
	n := &Node{ID: "n1", IP: parseIP("1.2.3.4"), TCP: 30303}

	var disc NodeDiscovery = nt
	if err := disc.AddNode(n); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if len(disc.AllNodes()) != 1 {
		t.Errorf("AllNodes count = %d, want 1", len(disc.AllNodes()))
	}
	disc.Remove("n1")
	if len(disc.AllNodes()) != 0 {
		t.Errorf("AllNodes after remove = %d, want 0", len(disc.AllNodes()))
	}
}

// TestMsgReadWriterInterface verifies MsgPipeEnd satisfies MsgReadWriter.
func TestMsgReadWriterInterface(t *testing.T) {
	a, b := MsgPipe()
	defer a.Close()
	defer b.Close()

	var rw MsgReadWriter = a

	go func() {
		rw.WriteMsg(Msg{Code: 1, Size: 4, Payload: []byte("test")})
	}()

	msg, err := b.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg: %v", err)
	}
	if msg.Code != 1 {
		t.Errorf("code = %d, want 1", msg.Code)
	}
}
