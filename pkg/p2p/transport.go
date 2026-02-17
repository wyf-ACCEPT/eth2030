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
