package p2p

import (
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

// --- HelloPacket encode/decode ---

func TestHelloPacket_EncodeDecode(t *testing.T) {
	original := &HelloPacket{
		Version:    5,
		Name:       "ETH2030/v0.1.0",
		Caps:       []Cap{{Name: "eth", Version: 68}, {Name: "snap", Version: 1}},
		ListenPort: 30303,
		ID:         "abcdef0123456789",
	}

	encoded := EncodeHello(original)
	decoded, err := DecodeHello(encoded)
	if err != nil {
		t.Fatalf("DecodeHello: %v", err)
	}

	if decoded.Version != original.Version {
		t.Errorf("Version: got %d, want %d", decoded.Version, original.Version)
	}
	if decoded.Name != original.Name {
		t.Errorf("Name: got %q, want %q", decoded.Name, original.Name)
	}
	if len(decoded.Caps) != len(original.Caps) {
		t.Fatalf("Caps length: got %d, want %d", len(decoded.Caps), len(original.Caps))
	}
	for i, c := range decoded.Caps {
		if c.Name != original.Caps[i].Name || c.Version != original.Caps[i].Version {
			t.Errorf("Cap[%d]: got %+v, want %+v", i, c, original.Caps[i])
		}
	}
	if decoded.ListenPort != original.ListenPort {
		t.Errorf("ListenPort: got %d, want %d", decoded.ListenPort, original.ListenPort)
	}
	if decoded.ID != original.ID {
		t.Errorf("ID: got %q, want %q", decoded.ID, original.ID)
	}
}

func TestHelloPacket_EmptyCaps(t *testing.T) {
	original := &HelloPacket{
		Version: 5,
		Name:    "test",
		Caps:    nil,
		ID:      "node1",
	}

	encoded := EncodeHello(original)
	decoded, err := DecodeHello(encoded)
	if err != nil {
		t.Fatalf("DecodeHello: %v", err)
	}
	if len(decoded.Caps) != 0 {
		t.Errorf("Caps: got %d caps, want 0", len(decoded.Caps))
	}
}

func TestDecodeHello_TooShort(t *testing.T) {
	_, err := DecodeHello([]byte{0x01, 0x02})
	if err == nil {
		t.Error("expected error for too-short packet")
	}
}

func TestDecodeHello_Truncated(t *testing.T) {
	original := &HelloPacket{
		Version: 5,
		Name:    "test-client",
		Caps:    []Cap{{Name: "eth", Version: 68}},
		ID:      "nodeXYZ",
	}
	encoded := EncodeHello(original)

	// Truncate at various points.
	for i := 10; i < len(encoded)-1; i++ {
		_, err := DecodeHello(encoded[:i])
		if err == nil {
			t.Errorf("expected error for truncated packet at byte %d", i)
		}
	}
}

// --- DisconnectReason ---

func TestDisconnectReason_String(t *testing.T) {
	tests := []struct {
		reason DisconnectReason
		want   string
	}{
		{DiscRequested, "requested"},
		{DiscNetworkError, "network error"},
		{DiscProtocolError, "protocol error"},
		{DiscUselessPeer, "useless peer"},
		{DiscTooManyPeers, "too many peers"},
		{DiscAlreadyConnected, "already connected"},
		{DiscSubprotocolError, "sub-protocol error"},
		{DisconnectReason(0xFF), "unknown(255)"},
	}
	for _, tt := range tests {
		got := tt.reason.String()
		if got != tt.want {
			t.Errorf("DisconnectReason(%d).String() = %q, want %q", tt.reason, got, tt.want)
		}
	}
}

// --- PerformHandshake ---

func TestPerformHandshake_Success(t *testing.T) {
	c1, c2 := net.Pipe()
	t1 := NewFrameTransport(c1)
	t2 := NewFrameTransport(c2)
	defer t1.Close()
	defer t2.Close()

	localA := &HelloPacket{
		Version: 5,
		Name:    "client-a",
		Caps:    []Cap{{Name: "eth", Version: 68}},
		ID:      "node-a",
	}
	localB := &HelloPacket{
		Version: 5,
		Name:    "client-b",
		Caps:    []Cap{{Name: "eth", Version: 68}, {Name: "snap", Version: 1}},
		ID:      "node-b",
	}

	type result struct {
		hello *HelloPacket
		err   error
	}
	resA := make(chan result, 1)
	resB := make(chan result, 1)

	go func() {
		h, err := PerformHandshake(t1, localA)
		resA <- result{h, err}
	}()
	go func() {
		h, err := PerformHandshake(t2, localB)
		resB <- result{h, err}
	}()

	select {
	case r := <-resA:
		if r.err != nil {
			t.Fatalf("handshake A: %v", r.err)
		}
		if r.hello.Name != "client-b" {
			t.Errorf("A received name: got %q, want %q", r.hello.Name, "client-b")
		}
		if r.hello.ID != "node-b" {
			t.Errorf("A received ID: got %q, want %q", r.hello.ID, "node-b")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for handshake A")
	}

	select {
	case r := <-resB:
		if r.err != nil {
			t.Fatalf("handshake B: %v", r.err)
		}
		if r.hello.Name != "client-a" {
			t.Errorf("B received name: got %q, want %q", r.hello.Name, "client-a")
		}
		if r.hello.ID != "node-a" {
			t.Errorf("B received ID: got %q, want %q", r.hello.ID, "node-a")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for handshake B")
	}
}

func TestPerformHandshake_VersionMismatch(t *testing.T) {
	c1, c2 := net.Pipe()
	t1 := NewFrameTransport(c1)
	t2 := NewFrameTransport(c2)
	defer t1.Close()
	defer t2.Close()

	localA := &HelloPacket{
		Version: 5,
		Name:    "modern-client",
		Caps:    []Cap{{Name: "eth", Version: 68}},
		ID:      "node-a",
	}
	localB := &HelloPacket{
		Version: 3, // Too old.
		Name:    "old-client",
		Caps:    []Cap{{Name: "eth", Version: 68}},
		ID:      "node-b",
	}

	type result struct {
		hello *HelloPacket
		err   error
	}
	resA := make(chan result, 1)
	resB := make(chan result, 1)

	go func() {
		h, err := PerformHandshake(t1, localA)
		resA <- result{h, err}
	}()
	go func() {
		h, err := PerformHandshake(t2, localB)
		resB <- result{h, err}
	}()

	// A should reject B's old version.
	select {
	case r := <-resA:
		if r.err == nil {
			t.Fatal("handshake A should have failed with version mismatch")
		}
		if !errors.Is(r.err, ErrIncompatibleVersion) {
			t.Errorf("handshake A error: got %v, want ErrIncompatibleVersion", r.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for handshake A")
	}
}

func TestPerformHandshake_NoMatchingCaps(t *testing.T) {
	c1, c2 := net.Pipe()
	t1 := NewFrameTransport(c1)
	t2 := NewFrameTransport(c2)
	defer t1.Close()
	defer t2.Close()

	localA := &HelloPacket{
		Version: 5,
		Name:    "client-a",
		Caps:    []Cap{{Name: "eth", Version: 68}},
		ID:      "node-a",
	}
	localB := &HelloPacket{
		Version: 5,
		Name:    "client-b",
		Caps:    []Cap{{Name: "snap", Version: 1}}, // No "eth" capability.
		ID:      "node-b",
	}

	type result struct {
		hello *HelloPacket
		err   error
	}
	resA := make(chan result, 1)
	resB := make(chan result, 1)

	go func() {
		h, err := PerformHandshake(t1, localA)
		resA <- result{h, err}
	}()
	go func() {
		h, err := PerformHandshake(t2, localB)
		resB <- result{h, err}
	}()

	// At least one side should fail with ErrNoMatchingCaps.
	timeout := time.After(2 * time.Second)
	gotNoMatch := false
	for i := 0; i < 2; i++ {
		select {
		case r := <-resA:
			if errors.Is(r.err, ErrNoMatchingCaps) {
				gotNoMatch = true
			}
		case r := <-resB:
			if errors.Is(r.err, ErrNoMatchingCaps) {
				gotNoMatch = true
			}
		case <-timeout:
			t.Fatal("timeout waiting for handshake")
		}
	}
	if !gotNoMatch {
		t.Error("expected at least one side to fail with ErrNoMatchingCaps")
	}
}

func TestPerformHandshake_ConnectionClosed(t *testing.T) {
	c1, c2 := net.Pipe()
	t1 := NewFrameTransport(c1)
	t2 := NewFrameTransport(c2)

	localA := &HelloPacket{
		Version: 5,
		Name:    "client-a",
		Caps:    []Cap{{Name: "eth", Version: 68}},
		ID:      "node-a",
	}

	// Close one side immediately.
	t2.Close()

	_, err := PerformHandshake(t1, localA)
	if err == nil {
		t.Error("expected error when remote connection is closed")
	}
}

// --- MatchingCaps ---

func TestMatchingCaps(t *testing.T) {
	local := []Cap{{Name: "eth", Version: 68}, {Name: "snap", Version: 1}, {Name: "les", Version: 4}}
	remote := []Cap{{Name: "eth", Version: 68}, {Name: "wit", Version: 1}, {Name: "les", Version: 4}}

	matched := MatchingCaps(local, remote)
	if len(matched) != 2 {
		t.Fatalf("MatchingCaps: got %d, want 2", len(matched))
	}

	names := make(map[string]bool)
	for _, c := range matched {
		names[c.Name] = true
	}
	if !names["eth"] || !names["les"] {
		t.Errorf("MatchingCaps: got %v, want eth and les", matched)
	}
}

func TestMatchingCaps_None(t *testing.T) {
	local := []Cap{{Name: "eth", Version: 68}}
	remote := []Cap{{Name: "snap", Version: 1}}

	matched := MatchingCaps(local, remote)
	if len(matched) != 0 {
		t.Errorf("MatchingCaps: got %d, want 0", len(matched))
	}
}

func TestMatchingCaps_VersionMismatch(t *testing.T) {
	local := []Cap{{Name: "eth", Version: 68}}
	remote := []Cap{{Name: "eth", Version: 67}}

	matched := MatchingCaps(local, remote)
	if len(matched) != 0 {
		t.Errorf("MatchingCaps: got %d, want 0 (version mismatch)", len(matched))
	}
}

// --- Disconnect message handling ---

func TestPerformHandshake_DisconnectReceived(t *testing.T) {
	c1, c2 := net.Pipe()
	t1 := NewFrameTransport(c1)
	t2 := NewFrameTransport(c2)
	defer t1.Close()
	defer t2.Close()

	localA := &HelloPacket{
		Version: 5,
		Name:    "client-a",
		Caps:    []Cap{{Name: "eth", Version: 68}},
		ID:      "node-a",
	}

	// On side B, send a disconnect instead of a hello.
	go func() {
		// Read A's hello first (consume it).
		t2.ReadMsg()
		// Send disconnect.
		t2.WriteMsg(Msg{
			Code:    DisconnectMsg,
			Size:    1,
			Payload: []byte{byte(DiscTooManyPeers)},
		})
	}()

	_, err := PerformHandshake(t1, localA)
	if err == nil {
		t.Fatal("expected error when receiving disconnect")
	}
	if !strings.Contains(err.Error(), "too many peers") {
		t.Errorf("error should mention disconnect reason, got: %v", err)
	}
}
