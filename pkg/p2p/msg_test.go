package p2p

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"
)

// --- Msg struct tests ---

func TestMsg_Fields(t *testing.T) {
	payload := []byte("test payload")
	msg := Msg{
		Code:    0x10,
		Size:    uint32(len(payload)),
		Payload: payload,
	}
	if msg.Code != 0x10 {
		t.Errorf("Code: got %d, want 0x10", msg.Code)
	}
	if msg.Size != uint32(len(payload)) {
		t.Errorf("Size: got %d, want %d", msg.Size, len(payload))
	}
	if !bytes.Equal(msg.Payload, payload) {
		t.Errorf("Payload: got %x, want %x", msg.Payload, payload)
	}
}

func TestMsg_EmptyPayload(t *testing.T) {
	msg := Msg{Code: 0x00, Size: 0, Payload: nil}
	if msg.Code != 0x00 {
		t.Errorf("Code: got %d, want 0", msg.Code)
	}
	if msg.Size != 0 {
		t.Errorf("Size: got %d, want 0", msg.Size)
	}
	if msg.Payload != nil {
		t.Errorf("Payload: got %v, want nil", msg.Payload)
	}
}

func TestMsg_ZeroValue(t *testing.T) {
	var msg Msg
	if msg.Code != 0 {
		t.Errorf("zero Code: got %d, want 0", msg.Code)
	}
	if msg.Size != 0 {
		t.Errorf("zero Size: got %d, want 0", msg.Size)
	}
	if msg.Payload != nil {
		t.Errorf("zero Payload: got %v, want nil", msg.Payload)
	}
}

func TestMsg_LargePayload(t *testing.T) {
	payload := make([]byte, 4096)
	for i := range payload {
		payload[i] = byte(i % 256)
	}
	msg := Msg{
		Code:    0xff,
		Size:    uint32(len(payload)),
		Payload: payload,
	}
	if msg.Size != 4096 {
		t.Errorf("Size: got %d, want 4096", msg.Size)
	}
	if !bytes.Equal(msg.Payload, payload) {
		t.Error("Payload content mismatch for large payload")
	}
}

// --- Send helper tests ---

func TestSend_SetsFields(t *testing.T) {
	a, b := MsgPipe()
	defer a.Close()
	defer b.Close()

	payload := []byte("hello")
	errc := make(chan error, 1)
	go func() {
		errc <- Send(a, 0x07, payload)
	}()

	msg, err := b.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("Send: %v", err)
	}
	if msg.Code != 0x07 {
		t.Errorf("Code: got %d, want 7", msg.Code)
	}
	if msg.Size != uint32(len(payload)) {
		t.Errorf("Size: got %d, want %d", msg.Size, len(payload))
	}
	if !bytes.Equal(msg.Payload, payload) {
		t.Errorf("Payload: got %x, want %x", msg.Payload, payload)
	}
}

func TestSend_NilPayload(t *testing.T) {
	a, b := MsgPipe()
	defer a.Close()
	defer b.Close()

	errc := make(chan error, 1)
	go func() {
		errc <- Send(a, 0x01, nil)
	}()

	msg, err := b.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("Send: %v", err)
	}
	if msg.Code != 0x01 {
		t.Errorf("Code: got %d, want 1", msg.Code)
	}
	if msg.Size != 0 {
		t.Errorf("Size: got %d, want 0", msg.Size)
	}
}

func TestSend_EmptyPayload(t *testing.T) {
	a, b := MsgPipe()
	defer a.Close()
	defer b.Close()

	errc := make(chan error, 1)
	go func() {
		errc <- Send(a, 0x02, []byte{})
	}()

	msg, err := b.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("Send: %v", err)
	}
	if msg.Size != 0 {
		t.Errorf("Size: got %d, want 0", msg.Size)
	}
}

func TestSend_OnClosedPipe(t *testing.T) {
	a, b := MsgPipe()
	a.Close()

	// After close, writing should eventually fail. Fill the buffered channel
	// first so that the select in WriteMsg is forced to pick the done case.
	var lastErr error
	for i := 0; i < 20; i++ {
		lastErr = Send(b, 0x01, []byte("data"))
		if lastErr != nil {
			break
		}
	}
	if lastErr == nil {
		t.Error("Send on closed pipe should eventually return error")
	}
}

// --- MsgPipe tests ---

func TestMsgPipe_BasicSendReceive(t *testing.T) {
	a, b := MsgPipe()
	defer a.Close()
	defer b.Close()

	payload := []byte("pipe-test")
	sent := Msg{Code: 99, Size: uint32(len(payload)), Payload: payload}

	errc := make(chan error, 1)
	go func() {
		errc <- a.WriteMsg(sent)
	}()

	got, err := b.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("WriteMsg: %v", err)
	}
	if got.Code != 99 {
		t.Errorf("Code: got %d, want 99", got.Code)
	}
	if !bytes.Equal(got.Payload, payload) {
		t.Error("Payload mismatch")
	}
}

func TestMsgPipe_BidirectionalPayloads(t *testing.T) {
	a, b := MsgPipe()
	defer a.Close()
	defer b.Close()

	var wg sync.WaitGroup
	wg.Add(2)

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
		t.Errorf("from-a code: got %d, want 1", msgFromA.Code)
	}
	if string(msgFromA.Payload) != "from-a" {
		t.Errorf("from-a payload: got %q, want %q", msgFromA.Payload, "from-a")
	}

	msgFromB, err := a.ReadMsg()
	if err != nil {
		t.Fatalf("a.ReadMsg: %v", err)
	}
	if msgFromB.Code != 2 {
		t.Errorf("from-b code: got %d, want 2", msgFromB.Code)
	}
	if string(msgFromB.Payload) != "from-b" {
		t.Errorf("from-b payload: got %q, want %q", msgFromB.Payload, "from-b")
	}

	wg.Wait()
}

func TestMsgPipe_MultipleMessages(t *testing.T) {
	a, b := MsgPipe()
	defer a.Close()
	defer b.Close()

	msgs := []Msg{
		{Code: 0, Size: 1, Payload: []byte{0x00}},
		{Code: 1, Size: 1, Payload: []byte{0x01}},
		{Code: 2, Size: 1, Payload: []byte{0x02}},
		{Code: 3, Size: 1, Payload: []byte{0x03}},
		{Code: 4, Size: 1, Payload: []byte{0x04}},
	}

	errc := make(chan error, 1)
	go func() {
		for _, m := range msgs {
			if err := a.WriteMsg(m); err != nil {
				errc <- err
				return
			}
		}
		errc <- nil
	}()

	for i, want := range msgs {
		got, err := b.ReadMsg()
		if err != nil {
			t.Fatalf("msg %d: ReadMsg: %v", i, err)
		}
		if got.Code != want.Code {
			t.Errorf("msg %d: Code got %d, want %d", i, got.Code, want.Code)
		}
		if !bytes.Equal(got.Payload, want.Payload) {
			t.Errorf("msg %d: Payload mismatch", i)
		}
	}

	if err := <-errc; err != nil {
		t.Fatalf("WriteMsg: %v", err)
	}
}

func TestMsgPipe_CloseTerminatesBlockedRead(t *testing.T) {
	a, b := MsgPipe()

	errc := make(chan error, 1)
	go func() {
		_, err := b.ReadMsg()
		errc <- err
	}()

	// Give goroutine time to block.
	time.Sleep(10 * time.Millisecond)
	a.Close()

	err := <-errc
	if err != io.EOF {
		t.Errorf("ReadMsg after close: got %v, want io.EOF", err)
	}
}

func TestMsgPipe_CloseEndsWrite(t *testing.T) {
	a, b := MsgPipe()
	a.Close()

	// The send channel is buffered, so writes may succeed until the buffer
	// fills. Once full, the select must pick the done case and return error.
	var lastErr error
	for i := 0; i < 20; i++ {
		lastErr = b.WriteMsg(Msg{Code: 0, Payload: []byte("data")})
		if lastErr != nil {
			break
		}
	}
	if lastErr == nil {
		t.Error("WriteMsg on closed pipe should eventually return error")
	}
}

func TestMsgPipe_ReadAfterClose(t *testing.T) {
	a, b := MsgPipe()
	a.Close()

	_, err := b.ReadMsg()
	if err != io.EOF {
		t.Errorf("ReadMsg on closed pipe: got %v, want io.EOF", err)
	}
}

func TestMsgPipe_DoubleClose(t *testing.T) {
	a, b := MsgPipe()

	// Double-close on one end should not panic.
	if err := a.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}

	// Other end should also see the pipe as closed.
	_, err := b.ReadMsg()
	if err != io.EOF {
		t.Errorf("ReadMsg after double close: got %v, want io.EOF", err)
	}
}

func TestMsgPipe_CloseBothEnds(t *testing.T) {
	a, b := MsgPipe()

	a.Close()
	b.Close()

	// Both should report errors.
	_, err := a.ReadMsg()
	if err != io.EOF {
		t.Errorf("a.ReadMsg: got %v, want io.EOF", err)
	}
	_, err = b.ReadMsg()
	if err != io.EOF {
		t.Errorf("b.ReadMsg: got %v, want io.EOF", err)
	}
}

func TestMsgPipe_EmptyPayload(t *testing.T) {
	a, b := MsgPipe()
	defer a.Close()
	defer b.Close()

	errc := make(chan error, 1)
	go func() {
		errc <- a.WriteMsg(Msg{Code: 0x42, Size: 0, Payload: nil})
	}()

	got, err := b.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("WriteMsg: %v", err)
	}
	if got.Code != 0x42 {
		t.Errorf("Code: got %d, want 0x42", got.Code)
	}
}

func TestMsgPipe_ConcurrentWriteRead(t *testing.T) {
	a, b := MsgPipe()
	defer a.Close()
	defer b.Close()

	const n = 50
	var wg sync.WaitGroup

	// Writer goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			a.WriteMsg(Msg{Code: uint64(i), Payload: []byte{byte(i)}})
		}
	}()

	// Reader goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			msg, err := b.ReadMsg()
			if err != nil {
				t.Errorf("ReadMsg %d: %v", i, err)
				return
			}
			if msg.Code != uint64(i) {
				t.Errorf("msg %d: Code got %d, want %d", i, msg.Code, i)
			}
		}
	}()

	wg.Wait()
}

func TestMsgPipe_LargePayload(t *testing.T) {
	a, b := MsgPipe()
	defer a.Close()
	defer b.Close()

	// 1 KiB payload.
	payload := make([]byte, 1024)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	errc := make(chan error, 1)
	go func() {
		errc <- a.WriteMsg(Msg{Code: 0x10, Size: uint32(len(payload)), Payload: payload})
	}()

	got, err := b.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("WriteMsg: %v", err)
	}
	if !bytes.Equal(got.Payload, payload) {
		t.Error("large payload content mismatch")
	}
}

// --- MsgPipeEnd as Transport interface ---

func TestMsgPipeEnd_ImplementsTransport(t *testing.T) {
	a, b := MsgPipe()
	defer a.Close()
	defer b.Close()

	// Verify MsgPipeEnd satisfies Transport at compile time.
	var _ Transport = a
	var _ Transport = b
}

func TestMsgPipeEnd_UsedAsTransport(t *testing.T) {
	a, b := MsgPipe()
	defer a.Close()
	defer b.Close()

	// Use the Transport interface to write and read.
	var ta Transport = a
	var tb Transport = b

	payload := []byte("transport-test")
	errc := make(chan error, 1)
	go func() {
		errc <- ta.WriteMsg(Msg{Code: 0x33, Size: uint32(len(payload)), Payload: payload})
	}()

	msg, err := tb.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg via Transport: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("WriteMsg via Transport: %v", err)
	}
	if msg.Code != 0x33 {
		t.Errorf("Code: got %d, want 0x33", msg.Code)
	}
	if !bytes.Equal(msg.Payload, payload) {
		t.Error("Payload mismatch via Transport interface")
	}
}
