package p2p

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

var (
	// ErrProtocolNotFound is returned when a message code does not match any protocol.
	ErrProtocolNotFound = errors.New("p2p: protocol not found for message code")

	// ErrMuxClosed is returned when the multiplexer has been shut down.
	ErrMuxClosed = errors.New("p2p: multiplexer closed")
)

// ProtoRW is a read-write interface scoped to a single sub-protocol's message
// code range. It offsets message codes so each protocol sees codes starting at 0.
type ProtoRW struct {
	proto  Protocol
	offset uint64 // Code offset for this protocol in the multiplexed stream.
	in     chan Msg
	closed chan struct{}
}

// ReadMsg reads the next message destined for this protocol. The returned
// message's Code is relative to the protocol (i.e., offset has been subtracted).
func (rw *ProtoRW) ReadMsg() (Msg, error) {
	select {
	case msg, ok := <-rw.in:
		if !ok {
			return Msg{}, ErrMuxClosed
		}
		return msg, nil
	case <-rw.closed:
		return Msg{}, ErrMuxClosed
	}
}

// Multiplexer manages multiple sub-protocols over a single transport connection.
// Each protocol is assigned a contiguous range of message codes. Incoming messages
// are dispatched to the correct protocol based on their code.
type Multiplexer struct {
	transport Transport
	protos    []*ProtoRW
	totalLen  uint64 // Total message code space across all protocols.

	mu     sync.Mutex
	closed bool
	done   chan struct{}
	wmu    sync.Mutex // Serializes writes to the transport.
}

// ProtoMatch describes a protocol and its assigned offset in the multiplexed
// message code space.
type ProtoMatch struct {
	Proto  Protocol
	Offset uint64
}

// NewMultiplexer creates a multiplexer for the given protocols over a transport.
// Protocols are sorted by (Name, Version) and assigned contiguous code ranges.
func NewMultiplexer(tr Transport, protocols []Protocol) *Multiplexer {
	// Sort protocols by name, then version (ascending).
	sorted := make([]Protocol, len(protocols))
	copy(sorted, protocols)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Name != sorted[j].Name {
			return sorted[i].Name < sorted[j].Name
		}
		return sorted[i].Version < sorted[j].Version
	})

	mux := &Multiplexer{
		transport: tr,
		done:      make(chan struct{}),
	}

	var offset uint64
	for _, p := range sorted {
		rw := &ProtoRW{
			proto:  p,
			offset: offset,
			in:     make(chan Msg, 16),
			closed: mux.done,
		}
		mux.protos = append(mux.protos, rw)
		offset += p.Length
	}
	mux.totalLen = offset

	return mux
}

// Protocols returns the ProtoRW handles for each registered protocol,
// in the order they were assigned offsets.
func (mux *Multiplexer) Protocols() []*ProtoRW {
	return mux.protos
}

// WriteMsg sends a message for the given protocol. The code is offset by the
// protocol's base before writing to the transport.
func (mux *Multiplexer) WriteMsg(rw *ProtoRW, msg Msg) error {
	mux.mu.Lock()
	if mux.closed {
		mux.mu.Unlock()
		return ErrMuxClosed
	}
	mux.mu.Unlock()

	if msg.Code >= rw.proto.Length {
		return fmt.Errorf("p2p: message code %d exceeds protocol length %d", msg.Code, rw.proto.Length)
	}

	// Offset the code for the wire.
	wireMsg := Msg{
		Code:    msg.Code + rw.offset,
		Size:    msg.Size,
		Payload: msg.Payload,
	}

	mux.wmu.Lock()
	defer mux.wmu.Unlock()
	return mux.transport.WriteMsg(wireMsg)
}

// ReadLoop reads messages from the transport and dispatches them to the
// appropriate protocol's channel. It blocks until the transport returns an error
// or Close is called. Returns the error that caused the loop to exit.
func (mux *Multiplexer) ReadLoop() error {
	for {
		msg, err := mux.transport.ReadMsg()
		if err != nil {
			mux.Close()
			return err
		}

		rw := mux.findProto(msg.Code)
		if rw == nil {
			// Unknown code; skip it. A stricter implementation would disconnect.
			continue
		}

		// Subtract offset so the protocol sees code-relative values.
		localMsg := Msg{
			Code:    msg.Code - rw.offset,
			Size:    msg.Size,
			Payload: msg.Payload,
		}

		select {
		case rw.in <- localMsg:
		case <-mux.done:
			return ErrMuxClosed
		}
	}
}

// Close shuts down the multiplexer and unblocks all protocol readers.
func (mux *Multiplexer) Close() {
	mux.mu.Lock()
	defer mux.mu.Unlock()
	if !mux.closed {
		mux.closed = true
		close(mux.done)
	}
}

// findProto returns the ProtoRW that owns the given wire message code.
func (mux *Multiplexer) findProto(code uint64) *ProtoRW {
	for _, rw := range mux.protos {
		if code >= rw.offset && code < rw.offset+rw.proto.Length {
			return rw
		}
	}
	return nil
}
