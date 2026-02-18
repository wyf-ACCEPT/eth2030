package p2p

import (
	"bytes"
	"net"
	"sync"
	"testing"
	"time"
)

func TestRLPxTransport_Handshake(t *testing.T) {
	c1, c2 := net.Pipe()
	t1 := NewRLPxTransport(c1)
	t2 := NewRLPxTransport(c2)
	defer t1.Close()
	defer t2.Close()

	// Perform handshake concurrently: t1 as initiator, t2 as responder.
	errc := make(chan error, 2)
	go func() { errc <- t1.Handshake(true) }()
	go func() { errc <- t2.Handshake(false) }()

	for i := 0; i < 2; i++ {
		if err := <-errc; err != nil {
			t.Fatalf("handshake %d: %v", i, err)
		}
	}
}

func TestRLPxTransport_ReadWrite(t *testing.T) {
	c1, c2 := net.Pipe()
	t1 := NewRLPxTransport(c1)
	t2 := NewRLPxTransport(c2)
	defer t1.Close()
	defer t2.Close()

	// Handshake.
	errc := make(chan error, 2)
	go func() { errc <- t1.Handshake(true) }()
	go func() { errc <- t2.Handshake(false) }()
	for i := 0; i < 2; i++ {
		if err := <-errc; err != nil {
			t.Fatalf("handshake: %v", err)
		}
	}

	// Write a message on t1, read on t2.
	payload := []byte("encrypted hello")
	sent := Msg{Code: 0x10, Size: uint32(len(payload)), Payload: payload}

	writeErr := make(chan error, 1)
	go func() { writeErr <- t1.WriteMsg(sent) }()

	got, err := t2.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg: %v", err)
	}
	if err := <-writeErr; err != nil {
		t.Fatalf("WriteMsg: %v", err)
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

func TestRLPxTransport_Bidirectional(t *testing.T) {
	c1, c2 := net.Pipe()
	t1 := NewRLPxTransport(c1)
	t2 := NewRLPxTransport(c2)
	defer t1.Close()
	defer t2.Close()

	// Handshake.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); t1.Handshake(true) }()
	go func() { defer wg.Done(); t2.Handshake(false) }()
	wg.Wait()

	// t1 -> t2
	wg.Add(1)
	go func() {
		defer wg.Done()
		t1.WriteMsg(Msg{Code: 1, Size: 3, Payload: []byte("abc")})
	}()

	msg1, err := t2.ReadMsg()
	if err != nil {
		t.Fatalf("t2 ReadMsg: %v", err)
	}
	wg.Wait()
	if msg1.Code != 1 || !bytes.Equal(msg1.Payload, []byte("abc")) {
		t.Errorf("t1->t2: code=%d payload=%s", msg1.Code, msg1.Payload)
	}

	// t2 -> t1
	wg.Add(1)
	go func() {
		defer wg.Done()
		t2.WriteMsg(Msg{Code: 2, Size: 3, Payload: []byte("xyz")})
	}()

	msg2, err := t1.ReadMsg()
	if err != nil {
		t.Fatalf("t1 ReadMsg: %v", err)
	}
	wg.Wait()
	if msg2.Code != 2 || !bytes.Equal(msg2.Payload, []byte("xyz")) {
		t.Errorf("t2->t1: code=%d payload=%s", msg2.Code, msg2.Payload)
	}
}

func TestRLPxTransport_MultipleMessages(t *testing.T) {
	c1, c2 := net.Pipe()
	t1 := NewRLPxTransport(c1)
	t2 := NewRLPxTransport(c2)
	defer t1.Close()
	defer t2.Close()

	// Handshake.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); t1.Handshake(true) }()
	go func() { defer wg.Done(); t2.Handshake(false) }()
	wg.Wait()

	msgs := []Msg{
		{Code: 0, Size: 3, Payload: []byte("aaa")},
		{Code: 1, Size: 3, Payload: []byte("bbb")},
		{Code: 2, Size: 3, Payload: []byte("ccc")},
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
			t.Fatalf("msg %d: ReadMsg: %v", i, err)
		}
		if got.Code != want.Code {
			t.Errorf("msg %d: code got %d, want %d", i, got.Code, want.Code)
		}
		if !bytes.Equal(got.Payload, want.Payload) {
			t.Errorf("msg %d: payload mismatch", i)
		}
	}

	if err := <-errc; err != nil {
		t.Fatalf("WriteMsg: %v", err)
	}
}

func TestRLPxTransport_EmptyPayload(t *testing.T) {
	c1, c2 := net.Pipe()
	t1 := NewRLPxTransport(c1)
	t2 := NewRLPxTransport(c2)
	defer t1.Close()
	defer t2.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); t1.Handshake(true) }()
	go func() { defer wg.Done(); t2.Handshake(false) }()
	wg.Wait()

	sent := Msg{Code: 0x05, Size: 0, Payload: nil}

	errc := make(chan error, 1)
	go func() { errc <- t1.WriteMsg(sent) }()

	got, err := t2.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("WriteMsg: %v", err)
	}

	if got.Code != 0x05 {
		t.Errorf("code: got %d, want 5", got.Code)
	}
	if got.Size != 0 {
		t.Errorf("size: got %d, want 0", got.Size)
	}
}

func TestRLPxTransport_ReadBeforeHandshake(t *testing.T) {
	c1, _ := net.Pipe()
	t1 := NewRLPxTransport(c1)
	defer t1.Close()

	_, err := t1.ReadMsg()
	if err == nil {
		t.Fatal("expected error reading before handshake")
	}
}

func TestRLPxTransport_WriteBeforeHandshake(t *testing.T) {
	c1, _ := net.Pipe()
	t1 := NewRLPxTransport(c1)
	defer t1.Close()

	err := t1.WriteMsg(Msg{Code: 1, Payload: []byte("test")})
	if err == nil {
		t.Fatal("expected error writing before handshake")
	}
}

func TestRLPxTransport_ServerIntegration(t *testing.T) {
	// Test that the server works with RLPx transport enabled.
	ready := make(chan struct{}, 2)

	proto := Protocol{
		Name:    "test",
		Version: 1,
		Length:  1,
		Run: func(peer *Peer, tr Transport) error {
			ready <- struct{}{}
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
		EnableRLPx: true,
	})
	srv2 := NewServer(Config{
		ListenAddr: "127.0.0.1:0",
		MaxPeers:   5,
		Protocols:  []Protocol{proto},
		EnableRLPx: true,
	})

	if err := srv1.Start(); err != nil {
		t.Fatalf("srv1 start: %v", err)
	}
	defer srv1.Stop()

	if err := srv2.Start(); err != nil {
		t.Fatalf("srv2 start: %v", err)
	}
	defer srv2.Stop()

	if err := srv2.AddPeer(srv1.ListenAddr().String()); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}

	timeout := time.After(3 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case <-ready:
		case <-timeout:
			t.Fatal("timeout waiting for protocol handler with RLPx")
		}
	}
}
