package p2p

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"
)

// makeCodecPair creates a pair of FrameCodecs connected via net.Pipe
// with matching keys.
func makeCodecPair(t *testing.T, enableSnappy bool) (*FrameCodec, *FrameCodec) {
	t.Helper()

	c1, c2 := net.Pipe()
	aesKey := sha256Hash([]byte("test-aes-key-material-32bytes!!"))
	macKey := sha256Hash([]byte("test-mac-key-material-32bytes!!"))
	caps := []Cap{{Name: "eth", Version: 68}}

	fc1, err := NewFrameCodec(c1, FrameCodecConfig{
		AESKey:       aesKey,
		MACKey:       macKey,
		Initiator:    true,
		EnableSnappy: enableSnappy,
		Caps:         caps,
	})
	if err != nil {
		t.Fatalf("NewFrameCodec initiator: %v", err)
	}

	fc2, err := NewFrameCodec(c2, FrameCodecConfig{
		AESKey:       aesKey,
		MACKey:       macKey,
		Initiator:    false,
		EnableSnappy: enableSnappy,
		Caps:         caps,
	})
	if err != nil {
		t.Fatalf("NewFrameCodec responder: %v", err)
	}

	t.Cleanup(func() {
		fc1.Close()
		fc2.Close()
	})
	return fc1, fc2
}

func TestFrameCodec_NewFrameCodec_ShortKeys(t *testing.T) {
	c1, _ := net.Pipe()
	defer c1.Close()

	_, err := NewFrameCodec(c1, FrameCodecConfig{
		AESKey: []byte("short"),
		MACKey: []byte("also-short"),
	})
	if err == nil {
		t.Fatal("expected error for short AES key")
	}

	aesKey := sha256Hash([]byte("ok-aes-key"))
	_, err = NewFrameCodec(c1, FrameCodecConfig{
		AESKey: aesKey,
		MACKey: []byte("short"),
	})
	if err == nil {
		t.Fatal("expected error for short MAC key")
	}
}

func TestFrameCodec_WriteReadMsg(t *testing.T) {
	fc1, fc2 := makeCodecPair(t, false)

	payload := []byte("hello frame codec")
	errCh := make(chan error, 1)
	go func() {
		errCh <- fc1.WriteMsg(Msg{
			Code:    0x01,
			Size:    uint32(len(payload)),
			Payload: payload,
		})
	}()

	msg, err := fc2.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("WriteMsg: %v", err)
	}
	if msg.Code != 0x01 {
		t.Fatalf("code: got %d, want 1", msg.Code)
	}
	if !bytes.Equal(msg.Payload, payload) {
		t.Fatalf("payload mismatch: got %x, want %x", msg.Payload, payload)
	}
}

func TestFrameCodec_Bidirectional(t *testing.T) {
	fc1, fc2 := makeCodecPair(t, false)

	// fc1 -> fc2
	errCh := make(chan error, 1)
	go func() {
		errCh <- fc1.WriteMsg(Msg{Code: 0x02, Payload: []byte("from-init")})
	}()
	msg, err := fc2.ReadMsg()
	if err != nil {
		t.Fatalf("fc2 ReadMsg: %v", err)
	}
	<-errCh
	if string(msg.Payload) != "from-init" {
		t.Fatalf("got %q, want %q", msg.Payload, "from-init")
	}

	// fc2 -> fc1
	go func() {
		errCh <- fc2.WriteMsg(Msg{Code: 0x03, Payload: []byte("from-resp")})
	}()
	msg, err = fc1.ReadMsg()
	if err != nil {
		t.Fatalf("fc1 ReadMsg: %v", err)
	}
	<-errCh
	if string(msg.Payload) != "from-resp" {
		t.Fatalf("got %q, want %q", msg.Payload, "from-resp")
	}
}

func TestFrameCodec_WithSnappy(t *testing.T) {
	fc1, fc2 := makeCodecPair(t, true)

	// Send a larger payload that benefits from compression.
	payload := bytes.Repeat([]byte("ABCDEFGH"), 128) // 1024 bytes, highly compressible
	errCh := make(chan error, 1)
	go func() {
		errCh <- fc1.WriteMsg(Msg{Code: 0x05, Payload: payload})
	}()

	msg, err := fc2.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("WriteMsg: %v", err)
	}
	if !bytes.Equal(msg.Payload, payload) {
		t.Fatalf("snappy roundtrip mismatch: got len %d, want len %d", len(msg.Payload), len(payload))
	}
}

func TestFrameCodec_MultipleMessages(t *testing.T) {
	fc1, fc2 := makeCodecPair(t, false)

	messages := []string{"msg-0", "msg-1", "msg-2", "msg-3"}
	errCh := make(chan error, 1)
	go func() {
		for _, m := range messages {
			if err := fc1.WriteMsg(Msg{Code: 0x01, Payload: []byte(m)}); err != nil {
				errCh <- err
				return
			}
		}
		errCh <- nil
	}()

	for i, want := range messages {
		msg, err := fc2.ReadMsg()
		if err != nil {
			t.Fatalf("ReadMsg %d: %v", i, err)
		}
		if string(msg.Payload) != want {
			t.Fatalf("message %d: got %q, want %q", i, msg.Payload, want)
		}
	}
	if err := <-errCh; err != nil {
		t.Fatalf("WriteMsg: %v", err)
	}
}

func TestFrameCodec_EmptyPayload(t *testing.T) {
	fc1, fc2 := makeCodecPair(t, false)

	errCh := make(chan error, 1)
	go func() {
		errCh <- fc1.WriteMsg(Msg{Code: PingMsg})
	}()

	msg, err := fc2.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("WriteMsg: %v", err)
	}
	if msg.Code != PingMsg {
		t.Fatalf("code: got 0x%02x, want 0x%02x", msg.Code, PingMsg)
	}
}

func TestFrameCodec_PingPong(t *testing.T) {
	fc1, fc2 := makeCodecPair(t, false)

	errCh := make(chan error, 1)
	go func() {
		errCh <- fc1.SendPing()
	}()

	msg, err := fc2.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("SendPing: %v", err)
	}
	if msg.Code != PingMsg {
		t.Fatalf("expected ping, got 0x%02x", msg.Code)
	}

	// Send pong back.
	go func() {
		errCh <- fc2.SendPong()
	}()
	msg, err = fc1.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg pong: %v", err)
	}
	<-errCh
	if msg.Code != PongMsg {
		t.Fatalf("expected pong, got 0x%02x", msg.Code)
	}
}

func TestFrameCodec_HandlePong(t *testing.T) {
	fc1, _ := makeCodecPair(t, false)

	before := fc1.LastPong()
	time.Sleep(10 * time.Millisecond)
	fc1.HandlePong()
	after := fc1.LastPong()

	if !after.After(before) {
		t.Fatal("HandlePong should update lastPong time")
	}
}

func TestFrameCodec_SendDisconnect(t *testing.T) {
	fc1, fc2 := makeCodecPair(t, false)

	errCh := make(chan error, 1)
	go func() {
		errCh <- fc1.SendDisconnect(DiscTooManyPeers)
	}()

	msg, err := fc2.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg: %v", err)
	}
	<-errCh

	if msg.Code != DisconnectMsg {
		t.Fatalf("expected disconnect, got 0x%02x", msg.Code)
	}
	if len(msg.Payload) != 1 || DisconnectReason(msg.Payload[0]) != DiscTooManyPeers {
		t.Fatalf("unexpected disconnect reason: %v", msg.Payload)
	}

	// fc1 should be closed after disconnect.
	if !fc1.IsClosed() {
		t.Fatal("codec should be closed after SendDisconnect")
	}
}

func TestFrameCodec_Close(t *testing.T) {
	fc1, _ := makeCodecPair(t, false)

	if fc1.IsClosed() {
		t.Fatal("should not be closed initially")
	}
	fc1.Close()
	if !fc1.IsClosed() {
		t.Fatal("should be closed after Close()")
	}

	// Double close should not panic.
	fc1.Close()
}

func TestFrameCodec_WriteAfterClose(t *testing.T) {
	fc1, _ := makeCodecPair(t, false)
	fc1.Close()

	err := fc1.WriteMsg(Msg{Code: 0x01, Payload: []byte("data")})
	if err != ErrCodecClosed {
		t.Fatalf("expected ErrCodecClosed, got %v", err)
	}
}

func TestFrameCodec_ReadAfterClose(t *testing.T) {
	fc1, _ := makeCodecPair(t, false)
	fc1.Close()

	_, err := fc1.ReadMsg()
	if err != ErrCodecClosed {
		t.Fatalf("expected ErrCodecClosed, got %v", err)
	}
}

func TestFrameCodec_CapOffset(t *testing.T) {
	c1, _ := net.Pipe()
	defer c1.Close()

	aesKey := sha256Hash([]byte("cap-test-aes"))
	macKey := sha256Hash([]byte("cap-test-mac"))

	fc, err := NewFrameCodec(c1, FrameCodecConfig{
		AESKey: aesKey,
		MACKey: macKey,
		Caps:   []Cap{{Name: "eth", Version: 68}, {Name: "snap", Version: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}

	ethOff, ok := fc.CapOffset("eth")
	if !ok {
		t.Fatal("eth capability not found")
	}
	if ethOff != 16 { // base protocol takes 16
		t.Fatalf("eth offset: got %d, want 16", ethOff)
	}

	snapOff, ok := fc.CapOffset("snap")
	if !ok {
		t.Fatal("snap capability not found")
	}
	if snapOff != 16+21 { // eth takes 21 codes
		t.Fatalf("snap offset: got %d, want %d", snapOff, 16+21)
	}

	_, ok = fc.CapOffset("unknown")
	if ok {
		t.Fatal("should not find unknown capability")
	}
}

func TestSnappyEncodeDecode(t *testing.T) {
	data := []byte("hello snappy compression test")
	encoded := snappyEncode(data)

	decoded, err := snappyDecode(encoded, snappyMaxDecompressed)
	if err != nil {
		t.Fatalf("snappyDecode: %v", err)
	}
	if !bytes.Equal(decoded, data) {
		t.Fatalf("roundtrip mismatch: got %x, want %x", decoded, data)
	}
}

func TestSnappyDecode_TooLarge(t *testing.T) {
	// Fake a snappy-encoded block claiming to be very large.
	data := snappyEncode(make([]byte, 100))
	// Tamper with the length prefix to claim a huge size.
	data[0] = 0xFF
	data[1] = 0xFF
	data[2] = 0xFF
	data[3] = 0x0F

	_, err := snappyDecode(data, 1024)
	if err == nil {
		t.Fatal("expected error for oversized snappy data")
	}
}

func TestSnappyEncodeDecode_Empty(t *testing.T) {
	encoded := snappyEncode([]byte{})
	decoded, err := snappyDecode(encoded, snappyMaxDecompressed)
	if err != nil {
		t.Fatalf("snappyDecode: %v", err)
	}
	if len(decoded) != 0 {
		t.Fatalf("expected empty, got %d bytes", len(decoded))
	}
}

func TestPadTo16(t *testing.T) {
	tests := []struct {
		inLen  int
		outLen int
	}{
		{0, 0},
		{1, 16},
		{15, 16},
		{16, 16},
		{17, 32},
		{32, 32},
		{33, 48},
	}
	for _, tt := range tests {
		data := make([]byte, tt.inLen)
		padded := padTo16(data)
		if len(padded) != tt.outLen {
			t.Errorf("padTo16(%d): got %d, want %d", tt.inLen, len(padded), tt.outLen)
		}
	}
}

func TestPutGetUint24(t *testing.T) {
	tests := []uint32{0, 1, 255, 256, 65535, 65536, 0xFFFFFF}
	for _, v := range tests {
		buf := make([]byte, 3)
		putUint24(buf, v)
		got := getUint24(buf)
		if got != v {
			t.Fatalf("uint24 roundtrip: got %d, want %d", got, v)
		}
	}
}

func TestDeriveFrameKeys(t *testing.T) {
	shared := sha256Hash([]byte("shared-secret"))
	initNonce := sha256Hash([]byte("init-nonce"))
	respNonce := sha256Hash([]byte("resp-nonce"))

	aesKey, macKey := DeriveFrameKeys(shared, initNonce, respNonce)
	if len(aesKey) != 32 {
		t.Fatalf("aes key length: got %d, want 32", len(aesKey))
	}
	if len(macKey) != 32 {
		t.Fatalf("mac key length: got %d, want 32", len(macKey))
	}

	// Same inputs should produce same outputs.
	aesKey2, macKey2 := DeriveFrameKeys(shared, initNonce, respNonce)
	if !bytes.Equal(aesKey, aesKey2) || !bytes.Equal(macKey, macKey2) {
		t.Fatal("DeriveFrameKeys not deterministic")
	}

	// Different inputs should produce different outputs.
	aesKey3, _ := DeriveFrameKeys(sha256Hash([]byte("other")), initNonce, respNonce)
	if bytes.Equal(aesKey, aesKey3) {
		t.Fatal("different shared secrets should produce different keys")
	}
}

func TestGenerateNonce(t *testing.T) {
	n1, err := GenerateNonce()
	if err != nil {
		t.Fatal(err)
	}
	n2, err := GenerateNonce()
	if err != nil {
		t.Fatal(err)
	}
	if n1 == n2 {
		t.Fatal("two nonces should not be equal")
	}
	// Check not all zeros.
	allZero := true
	for _, b := range n1 {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("nonce should not be all zeros")
	}
}

func TestComputeCapOffsets(t *testing.T) {
	caps := []Cap{
		{Name: "eth", Version: 68},
		{Name: "snap", Version: 1},
	}
	offsets := computeCapOffsets(caps)
	if len(offsets) != 2 {
		t.Fatalf("expected 2 offsets, got %d", len(offsets))
	}
	if offsets[0].Name != "eth" || offsets[0].Offset != 16 || offsets[0].Length != 21 {
		t.Fatalf("eth offset: %+v", offsets[0])
	}
	if offsets[1].Name != "snap" || offsets[1].Offset != 37 || offsets[1].Length != 8 {
		t.Fatalf("snap offset: %+v", offsets[1])
	}
}

func TestFrameCodec_ConcurrentReadWrite(t *testing.T) {
	fc1, fc2 := makeCodecPair(t, false)

	const numMessages = 20
	var wg sync.WaitGroup

	// Writer goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numMessages; i++ {
			payload := []byte(fmt.Sprintf("msg-%d", i))
			fc1.WriteMsg(Msg{Code: 0x01, Payload: payload})
		}
	}()

	// Reader goroutine.
	received := make([]string, 0, numMessages)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numMessages; i++ {
			msg, err := fc2.ReadMsg()
			if err != nil {
				return
			}
			received = append(received, string(msg.Payload))
		}
	}()

	wg.Wait()
	if len(received) != numMessages {
		t.Fatalf("expected %d messages, got %d", numMessages, len(received))
	}
}

// Ensure imports are used.
var _ = fmt.Sprintf
var _ = sha256.Sum256
