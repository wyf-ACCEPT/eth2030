package p2p

import (
	"bytes"
	"sync"
	"testing"
	"time"
)

func TestMultiplexer_SingleProtocol(t *testing.T) {
	a, b := MsgPipe()
	defer a.Close()
	defer b.Close()

	proto := Protocol{Name: "eth", Version: 68, Length: 11}
	mux := NewMultiplexer(a, []Protocol{proto})

	protos := mux.Protocols()
	if len(protos) != 1 {
		t.Fatalf("Protocols count = %d, want 1", len(protos))
	}
	if protos[0].offset != 0 {
		t.Errorf("offset = %d, want 0", protos[0].offset)
	}

	// Write through the mux, read from the other end.
	payload := []byte("test")
	go mux.WriteMsg(protos[0], Msg{Code: 3, Size: uint32(len(payload)), Payload: payload})

	msg, err := b.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg: %v", err)
	}
	// Wire code should be offset + code = 0 + 3 = 3.
	if msg.Code != 3 {
		t.Errorf("wire code = %d, want 3", msg.Code)
	}
	if !bytes.Equal(msg.Payload, payload) {
		t.Errorf("payload mismatch")
	}
}

func TestMultiplexer_MultipleProtocols(t *testing.T) {
	a, b := MsgPipe()
	defer a.Close()
	defer b.Close()

	proto1 := Protocol{Name: "aaa", Version: 1, Length: 5}
	proto2 := Protocol{Name: "bbb", Version: 1, Length: 3}

	mux := NewMultiplexer(a, []Protocol{proto1, proto2})
	protos := mux.Protocols()

	if len(protos) != 2 {
		t.Fatalf("Protocols count = %d, want 2", len(protos))
	}

	// Verify offsets: sorted by name, so "aaa" at 0, "bbb" at 5.
	if protos[0].proto.Name != "aaa" {
		t.Errorf("first proto = %q, want %q", protos[0].proto.Name, "aaa")
	}
	if protos[0].offset != 0 {
		t.Errorf("aaa offset = %d, want 0", protos[0].offset)
	}
	if protos[1].proto.Name != "bbb" {
		t.Errorf("second proto = %q, want %q", protos[1].proto.Name, "bbb")
	}
	if protos[1].offset != 5 {
		t.Errorf("bbb offset = %d, want 5", protos[1].offset)
	}
}

func TestMultiplexer_Dispatch(t *testing.T) {
	a, b := MsgPipe()
	defer a.Close()
	defer b.Close()

	proto1 := Protocol{Name: "aaa", Version: 1, Length: 5}
	proto2 := Protocol{Name: "bbb", Version: 1, Length: 3}

	mux := NewMultiplexer(b, []Protocol{proto1, proto2})

	// Start the read loop.
	go mux.ReadLoop()
	defer mux.Close()

	protos := mux.Protocols()

	// Send a message with wire code 6 (bbb's code 1 = offset 5 + 1).
	a.WriteMsg(Msg{Code: 6, Size: 2, Payload: []byte("hi")})

	// Read from bbb's ProtoRW.
	select {
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for dispatched message")
	default:
	}

	msg, err := protos[1].ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg from bbb: %v", err)
	}
	// Local code should be 1 (6 - offset 5).
	if msg.Code != 1 {
		t.Errorf("local code = %d, want 1", msg.Code)
	}
	if !bytes.Equal(msg.Payload, []byte("hi")) {
		t.Errorf("payload mismatch")
	}
}

func TestMultiplexer_WriteOffset(t *testing.T) {
	a, b := MsgPipe()
	defer a.Close()
	defer b.Close()

	proto1 := Protocol{Name: "aaa", Version: 1, Length: 5}
	proto2 := Protocol{Name: "bbb", Version: 1, Length: 3}

	mux := NewMultiplexer(a, []Protocol{proto1, proto2})
	protos := mux.Protocols()

	// Write code 2 on proto2 (offset=5), should go out as wire code 7.
	go mux.WriteMsg(protos[1], Msg{Code: 2, Size: 3, Payload: []byte("xyz")})

	msg, err := b.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg: %v", err)
	}
	if msg.Code != 7 {
		t.Errorf("wire code = %d, want 7 (offset 5 + code 2)", msg.Code)
	}
}

func TestMultiplexer_WriteCodeOutOfRange(t *testing.T) {
	a, _ := MsgPipe()
	defer a.Close()

	proto := Protocol{Name: "eth", Version: 68, Length: 5}
	mux := NewMultiplexer(a, []Protocol{proto})
	protos := mux.Protocols()

	// Code 5 exceeds protocol length of 5 (valid: 0-4).
	err := mux.WriteMsg(protos[0], Msg{Code: 5, Payload: nil})
	if err == nil {
		t.Error("expected error for out-of-range code")
	}
}

func TestMultiplexer_Close(t *testing.T) {
	a, _ := MsgPipe()
	defer a.Close()

	proto := Protocol{Name: "eth", Version: 68, Length: 5}
	mux := NewMultiplexer(a, []Protocol{proto})

	mux.Close()

	// Write after close.
	err := mux.WriteMsg(mux.Protocols()[0], Msg{Code: 0, Payload: nil})
	if err != ErrMuxClosed {
		t.Errorf("WriteMsg after close: got %v, want ErrMuxClosed", err)
	}

	// Read after close.
	_, err = mux.Protocols()[0].ReadMsg()
	if err != ErrMuxClosed {
		t.Errorf("ReadMsg after close: got %v, want ErrMuxClosed", err)
	}
}

func TestMultiplexer_ProtocolSorting(t *testing.T) {
	a, _ := MsgPipe()
	defer a.Close()

	// Provide protocols in unsorted order.
	protos := []Protocol{
		{Name: "zzz", Version: 1, Length: 2},
		{Name: "aaa", Version: 2, Length: 3},
		{Name: "aaa", Version: 1, Length: 3},
	}

	mux := NewMultiplexer(a, protos)
	result := mux.Protocols()

	// Should be sorted: aaa/1, aaa/2, zzz/1.
	if result[0].proto.Name != "aaa" || result[0].proto.Version != 1 {
		t.Errorf("proto[0] = %s/%d, want aaa/1", result[0].proto.Name, result[0].proto.Version)
	}
	if result[1].proto.Name != "aaa" || result[1].proto.Version != 2 {
		t.Errorf("proto[1] = %s/%d, want aaa/2", result[1].proto.Name, result[1].proto.Version)
	}
	if result[2].proto.Name != "zzz" || result[2].proto.Version != 1 {
		t.Errorf("proto[2] = %s/%d, want zzz/1", result[2].proto.Name, result[2].proto.Version)
	}

	// Offsets: 0, 3, 6.
	if result[0].offset != 0 {
		t.Errorf("offset[0] = %d, want 0", result[0].offset)
	}
	if result[1].offset != 3 {
		t.Errorf("offset[1] = %d, want 3", result[1].offset)
	}
	if result[2].offset != 6 {
		t.Errorf("offset[2] = %d, want 6", result[2].offset)
	}
}

func TestMultiplexer_FullRoundtrip(t *testing.T) {
	// Test full roundtrip: two muxes connected via pipe.
	a, b := MsgPipe()
	defer a.Close()
	defer b.Close()

	proto1 := Protocol{Name: "alpha", Version: 1, Length: 3}
	proto2 := Protocol{Name: "beta", Version: 1, Length: 2}

	muxA := NewMultiplexer(a, []Protocol{proto1, proto2})
	muxB := NewMultiplexer(b, []Protocol{proto1, proto2})

	// Start read loops.
	go muxA.ReadLoop()
	go muxB.ReadLoop()
	defer muxA.Close()
	defer muxB.Close()

	protosA := muxA.Protocols()
	protosB := muxB.Protocols()

	// A sends on alpha (code 2), B reads on alpha.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		muxA.WriteMsg(protosA[0], Msg{Code: 2, Size: 3, Payload: []byte("hey")})
	}()

	msg, err := protosB[0].ReadMsg()
	if err != nil {
		t.Fatalf("B read alpha: %v", err)
	}
	wg.Wait()
	if msg.Code != 2 {
		t.Errorf("alpha msg code = %d, want 2", msg.Code)
	}

	// B sends on beta (code 1), A reads on beta.
	wg.Add(1)
	go func() {
		defer wg.Done()
		muxB.WriteMsg(protosB[1], Msg{Code: 1, Size: 3, Payload: []byte("sup")})
	}()

	msg, err = protosA[1].ReadMsg()
	if err != nil {
		t.Fatalf("A read beta: %v", err)
	}
	wg.Wait()
	if msg.Code != 1 {
		t.Errorf("beta msg code = %d, want 1", msg.Code)
	}
	if !bytes.Equal(msg.Payload, []byte("sup")) {
		t.Errorf("beta payload = %s, want sup", msg.Payload)
	}
}

func TestMuxTransportAdapter(t *testing.T) {
	a, b := MsgPipe()
	defer a.Close()
	defer b.Close()

	proto := Protocol{Name: "test", Version: 1, Length: 5}
	mux := NewMultiplexer(a, []Protocol{proto})
	go mux.ReadLoop()
	defer mux.Close()

	adapter := &muxTransportAdapter{mux: mux, rw: mux.Protocols()[0]}

	// Write through adapter, read from pipe.
	go adapter.WriteMsg(Msg{Code: 3, Size: 4, Payload: []byte("test")})

	msg, err := b.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg: %v", err)
	}
	if msg.Code != 3 {
		t.Errorf("code = %d, want 3", msg.Code)
	}

	// Write to pipe, read through adapter.
	go b.WriteMsg(Msg{Code: 1, Size: 2, Payload: []byte("ok")})

	msg, err = adapter.ReadMsg()
	if err != nil {
		t.Fatalf("adapter ReadMsg: %v", err)
	}
	if msg.Code != 1 {
		t.Errorf("code = %d, want 1", msg.Code)
	}
}
