package p2p

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
)

var (
	// ErrTransportClosed is returned when reading/writing on a closed transport.
	ErrTransportClosed = errors.New("p2p: transport closed")

	// ErrFrameTooLarge is returned when a frame exceeds MaxMessageSize.
	ErrFrameTooLarge = errors.New("p2p: frame too large")
)

// Transport is the interface for reading and writing devp2p messages on a connection.
type Transport interface {
	ReadMsg() (Msg, error)
	WriteMsg(msg Msg) error
	Close() error
}

// ConnTransport extends Transport with remote address information.
// Implementations wrap a net.Conn and provide framed message I/O.
type ConnTransport interface {
	Transport
	// RemoteAddr returns the remote network address of the underlying connection.
	RemoteAddr() string
}

// Dialer is the interface for establishing outbound connections to peers.
type Dialer interface {
	// Dial connects to the given address and returns a ConnTransport.
	Dial(addr string) (ConnTransport, error)
}

// Listener is the interface for accepting inbound connections from peers.
type Listener interface {
	// Accept blocks until an inbound connection arrives and returns a ConnTransport.
	Accept() (ConnTransport, error)
	// Close stops the listener.
	Close() error
	// Addr returns the listener's network address.
	Addr() net.Addr
}

// TCPDialer dials TCP connections and wraps them in a FrameTransport.
type TCPDialer struct{}

// Dial connects to addr via TCP and returns a plaintext FrameTransport.
func (d *TCPDialer) Dial(addr string) (ConnTransport, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("p2p: dial error: %w", err)
	}
	return NewFrameConnTransport(conn), nil
}

// TCPListener wraps a net.Listener to accept connections as ConnTransports.
type TCPListener struct {
	ln net.Listener
}

// NewTCPListener creates a TCPListener from a net.Listener.
func NewTCPListener(ln net.Listener) *TCPListener {
	return &TCPListener{ln: ln}
}

// Accept blocks until an inbound TCP connection arrives.
func (l *TCPListener) Accept() (ConnTransport, error) {
	conn, err := l.ln.Accept()
	if err != nil {
		return nil, err
	}
	return NewFrameConnTransport(conn), nil
}

// Close stops the listener.
func (l *TCPListener) Close() error {
	return l.ln.Close()
}

// Addr returns the listener's network address.
func (l *TCPListener) Addr() net.Addr {
	return l.ln.Addr()
}

// FrameConnTransport wraps a FrameTransport with ConnTransport capabilities,
// providing remote address information alongside framed message I/O.
type FrameConnTransport struct {
	*FrameTransport
	remoteAddr string
}

// NewFrameConnTransport wraps a net.Conn as a ConnTransport with plaintext framing.
func NewFrameConnTransport(conn net.Conn) *FrameConnTransport {
	return &FrameConnTransport{
		FrameTransport: NewFrameTransport(conn),
		remoteAddr:     conn.RemoteAddr().String(),
	}
}

// RemoteAddr returns the remote network address.
func (t *FrameConnTransport) RemoteAddr() string {
	return t.remoteAddr
}

// FrameTransport implements Transport using length-prefixed plaintext framing.
// Wire format per message: [4-byte big-endian length][1-byte msg code][payload]
// where length = 1 + len(payload).
type FrameTransport struct {
	conn net.Conn
	rmu  sync.Mutex
	wmu  sync.Mutex
}

// NewFrameTransport wraps a net.Conn as a plaintext frame transport.
func NewFrameTransport(conn net.Conn) *FrameTransport {
	return &FrameTransport{conn: conn}
}

// ReadMsg reads a single framed message from the connection.
func (t *FrameTransport) ReadMsg() (Msg, error) {
	t.rmu.Lock()
	defer t.rmu.Unlock()

	// Read 4-byte length prefix.
	var lenBuf [4]byte
	if _, err := io.ReadFull(t.conn, lenBuf[:]); err != nil {
		return Msg{}, err
	}
	frameLen := binary.BigEndian.Uint32(lenBuf[:])
	if frameLen == 0 {
		return Msg{}, errors.New("p2p: empty frame")
	}
	if frameLen > MaxMessageSize+1 {
		return Msg{}, fmt.Errorf("%w: %d bytes", ErrFrameTooLarge, frameLen)
	}

	// Read the frame body: [1-byte code][payload].
	frame := make([]byte, frameLen)
	if _, err := io.ReadFull(t.conn, frame); err != nil {
		return Msg{}, err
	}

	code := uint64(frame[0])
	payload := frame[1:]

	return Msg{
		Code:    code,
		Size:    uint32(len(payload)),
		Payload: payload,
	}, nil
}

// WriteMsg writes a framed message to the connection.
func (t *FrameTransport) WriteMsg(msg Msg) error {
	t.wmu.Lock()
	defer t.wmu.Unlock()

	frameLen := 1 + len(msg.Payload) // 1 byte code + payload
	if frameLen > MaxMessageSize+1 {
		return fmt.Errorf("%w: %d bytes", ErrFrameTooLarge, frameLen)
	}

	// Write 4-byte length prefix.
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(frameLen))
	if _, err := t.conn.Write(lenBuf[:]); err != nil {
		return err
	}

	// Write code byte.
	if _, err := t.conn.Write([]byte{byte(msg.Code)}); err != nil {
		return err
	}

	// Write payload.
	if len(msg.Payload) > 0 {
		if _, err := t.conn.Write(msg.Payload); err != nil {
			return err
		}
	}
	return nil
}

// Close closes the underlying connection.
func (t *FrameTransport) Close() error {
	return t.conn.Close()
}
