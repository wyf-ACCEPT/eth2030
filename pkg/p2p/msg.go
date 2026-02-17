package p2p

import (
	"errors"
	"io"
	"sync"
)

// Msg represents a low-level devp2p frame message used by the transport layer.
// Unlike the higher-level Message type (which carries RLP payloads), Msg is the
// raw frame exchanged over the wire.
type Msg struct {
	Code    uint64 // Message code.
	Size    uint32 // Payload size in bytes.
	Payload []byte // Raw payload bytes.
}

// Send writes a message with the given code and payload to a Transport.
func Send(t Transport, code uint64, data []byte) error {
	return t.WriteMsg(Msg{
		Code:    code,
		Size:    uint32(len(data)),
		Payload: data,
	})
}

// MsgPipe creates a pair of connected Transports for testing.
// Messages sent on one end are received on the other.
type msgPipe struct {
	mu     sync.Mutex
	closed bool
	in     chan Msg
	out    chan Msg
	done   chan struct{}
}

// MsgPipe creates two connected transports. A message written to one is
// readable from the other, and vice versa. Close either end to shut down both.
func MsgPipe() (*MsgPipeEnd, *MsgPipeEnd) {
	ch1 := make(chan Msg, 16)
	ch2 := make(chan Msg, 16)
	done := make(chan struct{})
	once := new(sync.Once)

	a := &MsgPipeEnd{
		send:      ch1,
		recv:      ch2,
		done:      done,
		closeOnce: once,
	}
	b := &MsgPipeEnd{
		send:      ch2,
		recv:      ch1,
		done:      done,
		closeOnce: once,
	}
	return a, b
}

// MsgPipeEnd is one end of a MsgPipe.
type MsgPipeEnd struct {
	send      chan Msg
	recv      chan Msg
	done      chan struct{}
	closeOnce *sync.Once
}

func (p *MsgPipeEnd) ReadMsg() (Msg, error) {
	select {
	case msg, ok := <-p.recv:
		if !ok {
			return Msg{}, io.EOF
		}
		return msg, nil
	case <-p.done:
		return Msg{}, io.EOF
	}
}

func (p *MsgPipeEnd) WriteMsg(msg Msg) error {
	select {
	case p.send <- msg:
		return nil
	case <-p.done:
		return errors.New("p2p: pipe closed")
	}
}

func (p *MsgPipeEnd) Close() error {
	p.closeOnce.Do(func() {
		close(p.done)
	})
	return nil
}
