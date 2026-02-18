package p2p

import (
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

// --- ConnTransport tests ---

func TestFrameConnTransport_RemoteAddr(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	ct := NewFrameConnTransport(c1)
	addr := ct.RemoteAddr()
	if addr == "" {
		t.Error("RemoteAddr returned empty string")
	}
}

func TestFrameConnTransport_ReadWrite(t *testing.T) {
	c1, c2 := net.Pipe()
	ct1 := NewFrameConnTransport(c1)
	ct2 := NewFrameConnTransport(c2)
	defer ct1.Close()
	defer ct2.Close()

	payload := []byte("conn transport test")
	sent := Msg{Code: 0x05, Size: uint32(len(payload)), Payload: payload}

	errc := make(chan error, 1)
	go func() {
		errc <- ct1.WriteMsg(sent)
	}()

	got, err := ct2.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("WriteMsg: %v", err)
	}
	if got.Code != sent.Code {
		t.Errorf("code: got %d, want %d", got.Code, sent.Code)
	}
	if string(got.Payload) != string(sent.Payload) {
		t.Errorf("payload mismatch")
	}
}

// --- TCPDialer tests ---

func TestTCPDialer_Dial(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	// Accept in background.
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			accepted <- conn
		}
	}()

	dialer := &TCPDialer{}
	ct, err := dialer.Dial(ln.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer ct.Close()

	// Verify we got a ConnTransport with a valid remote address.
	if ct.RemoteAddr() == "" {
		t.Error("RemoteAddr is empty after Dial")
	}

	// Clean up accepted connection.
	select {
	case conn := <-accepted:
		conn.Close()
	case <-time.After(time.Second):
		t.Error("timeout waiting for accept")
	}
}

func TestTCPDialer_DialFails(t *testing.T) {
	dialer := &TCPDialer{}
	_, err := dialer.Dial("127.0.0.1:1") // Port 1 should be unreachable.
	if err == nil {
		t.Error("Dial to unreachable port should fail")
	}
}

// --- TCPListener tests ---

func TestTCPListener_AcceptAndAddr(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	tcpLn := NewTCPListener(ln)
	defer tcpLn.Close()

	addr := tcpLn.Addr()
	if addr == nil {
		t.Fatal("Addr returned nil")
	}

	// Connect from a raw client.
	go func() {
		conn, err := net.Dial("tcp", addr.String())
		if err == nil {
			conn.Close()
		}
	}()

	ct, err := tcpLn.Accept()
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	ct.Close()
}

func TestTCPListener_CloseStopsAccept(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	tcpLn := NewTCPListener(ln)

	errc := make(chan error, 1)
	go func() {
		_, err := tcpLn.Accept()
		errc <- err
	}()

	time.Sleep(10 * time.Millisecond)
	tcpLn.Close()

	select {
	case err := <-errc:
		if err == nil {
			t.Error("expected error from Accept after Close")
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for Accept to return after Close")
	}
}

// --- Mock transport for testing ---

// mockConnTransport implements ConnTransport for testing without real network I/O.
type mockConnTransport struct {
	*MsgPipeEnd
	addr string
}

func (m *mockConnTransport) RemoteAddr() string { return m.addr }

// mockConnTransportPair creates two connected mockConnTransports.
func mockConnTransportPair() (*mockConnTransport, *mockConnTransport) {
	a, b := MsgPipe()
	return &mockConnTransport{MsgPipeEnd: a, addr: "127.0.0.1:1111"},
		&mockConnTransport{MsgPipeEnd: b, addr: "127.0.0.1:2222"}
}

// mockDialer implements Dialer for testing.
type mockDialer struct {
	mu      sync.Mutex
	pending []chan ConnTransport
}

func newMockDialer() *mockDialer {
	return &mockDialer{}
}

func (d *mockDialer) Dial(addr string) (ConnTransport, error) {
	d.mu.Lock()
	if len(d.pending) == 0 {
		d.mu.Unlock()
		return nil, errors.New("mock dialer: no pending connections")
	}
	ch := d.pending[0]
	d.pending = d.pending[1:]
	d.mu.Unlock()

	ct, ok := <-ch
	if !ok {
		return nil, errors.New("mock dialer: channel closed")
	}
	return ct, nil
}

// prepare adds a pending connection that will be returned on the next Dial call.
func (d *mockDialer) prepare(ct ConnTransport) {
	d.mu.Lock()
	defer d.mu.Unlock()
	ch := make(chan ConnTransport, 1)
	ch <- ct
	d.pending = append(d.pending, ch)
}

// mockListener implements Listener for testing.
type mockListener struct {
	ch     chan ConnTransport
	closed chan struct{}
	once   sync.Once
	addr   net.Addr
}

func newMockListener() *mockListener {
	return &mockListener{
		ch:     make(chan ConnTransport, 8),
		closed: make(chan struct{}),
		addr:   &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 30303},
	}
}

func (l *mockListener) Accept() (ConnTransport, error) {
	select {
	case ct := <-l.ch:
		return ct, nil
	case <-l.closed:
		return nil, errors.New("mock listener: closed")
	}
}

func (l *mockListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *mockListener) Addr() net.Addr { return l.addr }

// inject adds a mock connection to the listener's accept queue.
func (l *mockListener) inject(ct ConnTransport) {
	l.ch <- ct
}

// --- Interface compliance ---

var _ ConnTransport = (*FrameConnTransport)(nil)
var _ ConnTransport = (*mockConnTransport)(nil)
var _ Dialer = (*TCPDialer)(nil)
var _ Dialer = (*mockDialer)(nil)
var _ Listener = (*TCPListener)(nil)
var _ Listener = (*mockListener)(nil)
