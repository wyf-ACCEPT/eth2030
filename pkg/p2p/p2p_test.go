package p2p

import (
	"bytes"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// --- FrameTransport tests ---

func TestFrameTransport_ReadWrite(t *testing.T) {
	c1, c2 := net.Pipe()
	t1 := NewFrameTransport(c1)
	t2 := NewFrameTransport(c2)
	defer t1.Close()
	defer t2.Close()

	payload := []byte("hello devp2p")
	sent := Msg{Code: 0x10, Size: uint32(len(payload)), Payload: payload}

	errc := make(chan error, 1)
	go func() {
		errc <- t1.WriteMsg(sent)
	}()

	got, err := t2.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg error: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("WriteMsg error: %v", err)
	}

	if got.Code != sent.Code {
		t.Errorf("code: got %d, want %d", got.Code, sent.Code)
	}
	if got.Size != sent.Size {
		t.Errorf("size: got %d, want %d", got.Size, sent.Size)
	}
	if !bytes.Equal(got.Payload, sent.Payload) {
		t.Errorf("payload: got %x, want %x", got.Payload, sent.Payload)
	}
}

func TestFrameTransport_EmptyPayload(t *testing.T) {
	c1, c2 := net.Pipe()
	t1 := NewFrameTransport(c1)
	t2 := NewFrameTransport(c2)
	defer t1.Close()
	defer t2.Close()

	sent := Msg{Code: 0x01, Size: 0, Payload: nil}

	errc := make(chan error, 1)
	go func() {
		errc <- t1.WriteMsg(sent)
	}()

	got, err := t2.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg error: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("WriteMsg error: %v", err)
	}

	if got.Code != 0x01 {
		t.Errorf("code: got %d, want 1", got.Code)
	}
	if got.Size != 0 {
		t.Errorf("size: got %d, want 0", got.Size)
	}
}

func TestFrameTransport_MultipleMessages(t *testing.T) {
	c1, c2 := net.Pipe()
	t1 := NewFrameTransport(c1)
	t2 := NewFrameTransport(c2)
	defer t1.Close()
	defer t2.Close()

	msgs := []Msg{
		{Code: 0x00, Size: 3, Payload: []byte("aaa")},
		{Code: 0x01, Size: 3, Payload: []byte("bbb")},
		{Code: 0x02, Size: 3, Payload: []byte("ccc")},
	}

	errc := make(chan error, 1)
	go func() {
		for _, m := range msgs {
			if err := t1.WriteMsg(m); err != nil {
				errc <- err
				return
			}
		}
		errc <- nil
	}()

	for i, want := range msgs {
		got, err := t2.ReadMsg()
		if err != nil {
			t.Fatalf("msg %d: ReadMsg error: %v", i, err)
		}
		if got.Code != want.Code {
			t.Errorf("msg %d: code got %d, want %d", i, got.Code, want.Code)
		}
		if !bytes.Equal(got.Payload, want.Payload) {
			t.Errorf("msg %d: payload mismatch", i)
		}
	}

	if err := <-errc; err != nil {
		t.Fatalf("WriteMsg error: %v", err)
	}
}

func TestFrameTransport_ReadClosed(t *testing.T) {
	c1, c2 := net.Pipe()
	t1 := NewFrameTransport(c1)
	t2 := NewFrameTransport(c2)
	t1.Close()

	_, err := t2.ReadMsg()
	if err == nil {
		t.Fatal("expected error reading from closed pipe")
	}
}

// --- ManagedPeerSet tests ---

func TestManagedPeerSet_AddRemove(t *testing.T) {
	ps := NewManagedPeerSet(10)

	p1 := NewPeer("peer1", "1.2.3.4:30303", nil)
	p2 := NewPeer("peer2", "5.6.7.8:30303", nil)

	if err := ps.Add(p1); err != nil {
		t.Fatalf("Add p1: %v", err)
	}
	if err := ps.Add(p2); err != nil {
		t.Fatalf("Add p2: %v", err)
	}
	if ps.Len() != 2 {
		t.Errorf("Len: got %d, want 2", ps.Len())
	}

	// Duplicate add should fail.
	if err := ps.Add(p1); err != ErrPeerAlreadyRegistered {
		t.Errorf("duplicate Add: got %v, want ErrPeerAlreadyRegistered", err)
	}

	// Get should return the peer.
	if got := ps.Get("peer1"); got != p1 {
		t.Errorf("Get peer1: got %v, want %v", got, p1)
	}
	if got := ps.Get("nonexistent"); got != nil {
		t.Errorf("Get nonexistent: got %v, want nil", got)
	}

	// Remove and verify.
	if err := ps.Remove("peer1"); err != nil {
		t.Fatalf("Remove peer1: %v", err)
	}
	if ps.Len() != 1 {
		t.Errorf("Len after remove: got %d, want 1", ps.Len())
	}

	// Remove non-existent should fail.
	if err := ps.Remove("peer1"); err != ErrPeerNotRegistered {
		t.Errorf("Remove non-existent: got %v, want ErrPeerNotRegistered", err)
	}
}

func TestManagedPeerSet_MaxPeers(t *testing.T) {
	ps := NewManagedPeerSet(2)

	p1 := NewPeer("peer1", "1.2.3.4:30303", nil)
	p2 := NewPeer("peer2", "5.6.7.8:30303", nil)
	p3 := NewPeer("peer3", "9.10.11.12:30303", nil)

	if err := ps.Add(p1); err != nil {
		t.Fatalf("Add p1: %v", err)
	}
	if err := ps.Add(p2); err != nil {
		t.Fatalf("Add p2: %v", err)
	}

	// Third peer should be rejected.
	if err := ps.Add(p3); err != ErrMaxPeers {
		t.Errorf("Add p3: got %v, want ErrMaxPeers", err)
	}

	// Remove one, then p3 should succeed.
	ps.Remove("peer1")
	if err := ps.Add(p3); err != nil {
		t.Fatalf("Add p3 after remove: %v", err)
	}
}

func TestManagedPeerSet_Close(t *testing.T) {
	ps := NewManagedPeerSet(10)
	ps.Add(NewPeer("peer1", "1.2.3.4:30303", nil))
	ps.Close()

	if ps.Len() != 0 {
		t.Errorf("Len after close: got %d, want 0", ps.Len())
	}
	if err := ps.Add(NewPeer("peer2", "5.6.7.8:30303", nil)); err != ErrPeerSetClosed {
		t.Errorf("Add after close: got %v, want ErrPeerSetClosed", err)
	}
}

func TestManagedPeerSet_Peers(t *testing.T) {
	ps := NewManagedPeerSet(10)
	ps.Add(NewPeer("a", "1.1.1.1:1", nil))
	ps.Add(NewPeer("b", "2.2.2.2:2", nil))

	peers := ps.Peers()
	if len(peers) != 2 {
		t.Errorf("Peers: got %d, want 2", len(peers))
	}
}

// --- MsgPipe tests ---

func TestMsgPipe(t *testing.T) {
	a, b := MsgPipe()
	defer a.Close()
	defer b.Close()

	payload := []byte("pipe test data")
	sent := Msg{Code: 42, Size: uint32(len(payload)), Payload: payload}

	errc := make(chan error, 1)
	go func() {
		errc <- a.WriteMsg(sent)
	}()

	got, err := b.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg error: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("WriteMsg error: %v", err)
	}
	if got.Code != 42 {
		t.Errorf("code: got %d, want 42", got.Code)
	}
	if !bytes.Equal(got.Payload, payload) {
		t.Errorf("payload mismatch")
	}
}

func TestMsgPipe_Bidirectional(t *testing.T) {
	a, b := MsgPipe()
	defer a.Close()
	defer b.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	// a writes, b reads
	go func() {
		defer wg.Done()
		a.WriteMsg(Msg{Code: 1, Payload: []byte("from-a")})
	}()
	go func() {
		defer wg.Done()
		b.WriteMsg(Msg{Code: 2, Payload: []byte("from-b")})
	}()

	msgFromA, err := b.ReadMsg()
	if err != nil {
		t.Fatalf("b.ReadMsg: %v", err)
	}
	if msgFromA.Code != 1 {
		t.Errorf("got code %d, want 1", msgFromA.Code)
	}

	msgFromB, err := a.ReadMsg()
	if err != nil {
		t.Fatalf("a.ReadMsg: %v", err)
	}
	if msgFromB.Code != 2 {
		t.Errorf("got code %d, want 2", msgFromB.Code)
	}

	wg.Wait()
}

func TestMsgPipe_CloseEndsRead(t *testing.T) {
	a, b := MsgPipe()

	errc := make(chan error, 1)
	go func() {
		_, err := b.ReadMsg()
		errc <- err
	}()

	// Give the goroutine time to block on read.
	time.Sleep(10 * time.Millisecond)
	a.Close()

	err := <-errc
	if err != io.EOF {
		t.Errorf("got %v, want io.EOF", err)
	}
}

// --- Send helper test ---

func TestSend(t *testing.T) {
	a, b := MsgPipe()
	defer a.Close()
	defer b.Close()

	errc := make(chan error, 1)
	go func() {
		errc <- Send(a, 0x05, []byte("send-helper"))
	}()

	msg, err := b.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("Send: %v", err)
	}
	if msg.Code != 0x05 {
		t.Errorf("code: got %d, want 5", msg.Code)
	}
	if string(msg.Payload) != "send-helper" {
		t.Errorf("payload: got %q, want %q", msg.Payload, "send-helper")
	}
}

// --- Server tests ---

func TestServer_StartStop(t *testing.T) {
	srv := NewServer(Config{
		ListenAddr: "127.0.0.1:0",
		MaxPeers:   5,
	})

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	addr := srv.ListenAddr()
	if addr == nil {
		t.Fatal("ListenAddr returned nil")
	}

	// Verify the server can accept connections.
	conn, err := net.DialTimeout("tcp", addr.String(), time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.Close()

	srv.Stop()

	// Double-stop should be safe.
	srv.Stop()
}

func TestServer_PeerConnect(t *testing.T) {
	// Track handshake completions.
	var mu sync.Mutex
	handshakes := 0
	handshakeDone := make(chan struct{}, 2)

	proto := Protocol{
		Name:    "test",
		Version: 1,
		Length:  1,
		Run: func(peer *Peer, tr Transport) error {
			mu.Lock()
			handshakes++
			mu.Unlock()
			handshakeDone <- struct{}{}

			// Exchange a message.
			if err := tr.WriteMsg(Msg{Code: 0x00, Size: 5, Payload: []byte("hello")}); err != nil {
				return err
			}
			msg, err := tr.ReadMsg()
			if err != nil {
				return err
			}
			_ = msg
			return nil
		},
	}

	srv1 := NewServer(Config{
		ListenAddr: "127.0.0.1:0",
		MaxPeers:   5,
		Protocols:  []Protocol{proto},
	})
	srv2 := NewServer(Config{
		ListenAddr: "127.0.0.1:0",
		MaxPeers:   5,
		Protocols:  []Protocol{proto},
	})

	if err := srv1.Start(); err != nil {
		t.Fatalf("srv1 start: %v", err)
	}
	defer srv1.Stop()

	if err := srv2.Start(); err != nil {
		t.Fatalf("srv2 start: %v", err)
	}
	defer srv2.Stop()

	// srv2 connects to srv1.
	if err := srv2.AddPeer(srv1.ListenAddr().String()); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}

	// Wait for both protocol handlers to run (srv1 accepts, srv2 dials).
	timeout := time.After(3 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case <-handshakeDone:
		case <-timeout:
			t.Fatal("timeout waiting for protocol handler")
		}
	}

	mu.Lock()
	if handshakes != 2 {
		t.Errorf("handshakes: got %d, want 2", handshakes)
	}
	mu.Unlock()
}

func TestServer_PeerCount(t *testing.T) {
	ready := make(chan struct{})
	proto := Protocol{
		Name:    "test",
		Version: 1,
		Length:  1,
		Run: func(peer *Peer, tr Transport) error {
			ready <- struct{}{}
			// Block until connection closes.
			tr.ReadMsg()
			return nil
		},
	}

	srv := NewServer(Config{
		ListenAddr: "127.0.0.1:0",
		MaxPeers:   5,
		Protocols:  []Protocol{proto},
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	// Connect a raw client.
	conn, err := net.Dial("tcp", srv.ListenAddr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Wait for the protocol handler to start.
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for protocol handler")
	}

	if n := srv.PeerCount(); n != 1 {
		t.Errorf("PeerCount: got %d, want 1", n)
	}
}
