package enode

import (
	"net"
	"testing"
)

func TestNewNode(t *testing.T) {
	id := HexID("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	n := NewNode(id, net.ParseIP("10.0.0.1"), 30303, 30301)

	if n.ID != id {
		t.Fatal("ID mismatch")
	}
	if !n.IP.Equal(net.ParseIP("10.0.0.1")) {
		t.Fatalf("IP = %v", n.IP)
	}
	if n.TCP != 30303 {
		t.Fatalf("TCP = %d, want 30303", n.TCP)
	}
	if n.UDP != 30301 {
		t.Fatalf("UDP = %d, want 30301", n.UDP)
	}
}

func TestNodeString(t *testing.T) {
	id := HexID("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	n := NewNode(id, net.ParseIP("10.0.0.1"), 30303, 30303)

	s := n.String()
	if s == "" {
		t.Fatal("String() returned empty")
	}
	// When UDP == TCP, no ?discport= suffix.
	if got := n.String(); got != "enode://"+id.String()+"@10.0.0.1:30303" {
		t.Fatalf("String() = %q", got)
	}

	// When UDP != TCP, should include discport.
	n.UDP = 30301
	s = n.String()
	if got := "enode://" + id.String() + "@10.0.0.1:30303?discport=30301"; s != got {
		t.Fatalf("String() = %q, want %q", s, got)
	}
}

func TestParseNode(t *testing.T) {
	hexID := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	url := "enode://" + hexID + "@192.168.1.1:30303"

	n, err := ParseNode(url)
	if err != nil {
		t.Fatal(err)
	}
	if n.ID.String() != hexID {
		t.Fatalf("ID = %s, want %s", n.ID.String(), hexID)
	}
	if !n.IP.Equal(net.ParseIP("192.168.1.1")) {
		t.Fatalf("IP = %v", n.IP)
	}
	if n.TCP != 30303 {
		t.Fatalf("TCP = %d", n.TCP)
	}
	if n.UDP != 30303 {
		t.Fatalf("UDP = %d, want 30303 (same as TCP)", n.UDP)
	}
}

func TestParseNodeWithDiscport(t *testing.T) {
	hexID := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	url := "enode://" + hexID + "@10.0.0.1:30303?discport=30301"

	n, err := ParseNode(url)
	if err != nil {
		t.Fatal(err)
	}
	if n.TCP != 30303 {
		t.Fatalf("TCP = %d", n.TCP)
	}
	if n.UDP != 30301 {
		t.Fatalf("UDP = %d, want 30301", n.UDP)
	}
}

func TestParseNodeInvalidPrefix(t *testing.T) {
	_, err := ParseNode("http://foobar@10.0.0.1:30303")
	if err == nil {
		t.Fatal("expected error for invalid prefix")
	}
}

func TestParseNodeInvalidHex(t *testing.T) {
	_, err := ParseNode("enode://invalidhex@10.0.0.1:30303")
	if err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

func TestParseNodeMissingAt(t *testing.T) {
	_, err := ParseNode("enode://abcdef0123456789abcdef0123456789abcdef0123456789abcdef01234567")
	if err == nil {
		t.Fatal("expected error for missing @")
	}
}

func TestNodeAddr(t *testing.T) {
	id := NodeID{}
	n := NewNode(id, net.ParseIP("10.0.0.1"), 30303, 30301)

	udp := n.Addr()
	if udp.Port != 30301 {
		t.Fatalf("UDP port = %d", udp.Port)
	}
	if !udp.IP.Equal(net.ParseIP("10.0.0.1")) {
		t.Fatalf("UDP IP = %v", udp.IP)
	}

	tcp := n.TCPAddr()
	if tcp.Port != 30303 {
		t.Fatalf("TCP port = %d", tcp.Port)
	}
}

func TestParseID(t *testing.T) {
	hex := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	id, err := ParseID(hex)
	if err != nil {
		t.Fatal(err)
	}
	if id.String() != hex {
		t.Fatalf("ParseID roundtrip: got %s, want %s", id.String(), hex)
	}

	// With 0x prefix.
	id2, err := ParseID("0x" + hex)
	if err != nil {
		t.Fatal(err)
	}
	if id != id2 {
		t.Fatal("0x-prefixed and non-prefixed should match")
	}
}

func TestParseIDInvalid(t *testing.T) {
	_, err := ParseID("too-short")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDistance(t *testing.T) {
	a := NodeID{}
	b := NodeID{}

	// Same ID -> distance 0.
	if d := Distance(a, b); d != 0 {
		t.Fatalf("Distance(a, a) = %d, want 0", d)
	}

	// One bit difference in last byte -> distance 1.
	b[31] = 1
	if d := Distance(a, b); d != 1 {
		t.Fatalf("Distance(0, ...001) = %d, want 1", d)
	}

	// High bit difference -> distance 256.
	b = NodeID{}
	b[0] = 0x80
	if d := Distance(a, b); d != 256 {
		t.Fatalf("Distance(0, 0x80...) = %d, want 256", d)
	}
}

func TestDistCmp(t *testing.T) {
	target := NodeID{}
	a := NodeID{}
	b := NodeID{}

	// Both equal -> 0.
	if c := DistCmp(target, a, b); c != 0 {
		t.Fatalf("DistCmp(0, 0, 0) = %d, want 0", c)
	}

	// a closer than b.
	a[31] = 1
	b[31] = 2
	if c := DistCmp(target, a, b); c != -1 {
		t.Fatalf("DistCmp: a=1 closer than b=2, got %d, want -1", c)
	}

	// b closer than a.
	if c := DistCmp(target, b, a); c != 1 {
		t.Fatalf("DistCmp: b=2 farther than a=1, got %d, want 1", c)
	}
}

func TestNodeIDIsZero(t *testing.T) {
	var id NodeID
	if !id.IsZero() {
		t.Fatal("zero ID should be zero")
	}
	id[0] = 1
	if id.IsZero() {
		t.Fatal("non-zero ID should not be zero")
	}
}

func TestHexIDPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic from HexID with invalid input")
		}
	}()
	HexID("not-a-valid-hex-id")
}
