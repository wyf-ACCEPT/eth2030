package discover

import (
	"net"
	"testing"

	"github.com/eth2028/eth2028/crypto"
	"github.com/eth2028/eth2028/p2p/enode"
	"github.com/eth2028/eth2028/p2p/enr"
	"github.com/eth2028/eth2028/rlp"
)

func makeLocalNode(t *testing.T) (*enode.Node, *net.UDPConn) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	rec := &enr.Record{Seq: 1}
	rec.Set(enr.KeyIP, []byte{127, 0, 0, 1})
	rec.Set(enr.KeyUDP, []byte{0x76, 0x5f})
	if err := enr.SignENR(rec, key); err != nil {
		t.Fatal(err)
	}
	id := rec.NodeID()
	node := &enode.Node{
		ID:     enode.NodeID(id),
		IP:     net.ParseIP("127.0.0.1"),
		TCP:    30303,
		UDP:    30303,
		Record: rec,
		Pubkey: crypto.CompressPubkey(&key.PublicKey),
	}

	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return node, conn.(*net.UDPConn)
}

func TestV5ProtocolStartStop(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	node, conn := makeLocalNode(t)
	defer conn.Close()

	p := NewV5Protocol(conn, key, node)
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	p.Stop()
}

func TestV5SendPing(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	node, conn := makeLocalNode(t)
	defer conn.Close()

	p := NewV5Protocol(conn, key, node)

	target := makeNode(1)
	target.IP = net.ParseIP("127.0.0.1")
	target.UDP = 30304

	// SendPing should not error (even if nothing receives it).
	err = p.SendPing(target)
	if err != nil {
		t.Fatalf("SendPing: %v", err)
	}
}

func TestV5SendFindNode(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	node, conn := makeLocalNode(t)
	defer conn.Close()

	p := NewV5Protocol(conn, key, node)

	target := makeNode(1)
	target.IP = net.ParseIP("127.0.0.1")
	target.UDP = 30304

	err = p.SendFindNode(target, []uint{1, 2, 3})
	if err != nil {
		t.Fatalf("SendFindNode: %v", err)
	}
}

func TestV5SendAfterStop(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	node, conn := makeLocalNode(t)
	defer conn.Close()

	p := NewV5Protocol(conn, key, node)
	p.Stop()

	err = p.SendPing(makeNode(1))
	if err != ErrClosed {
		t.Fatalf("SendPing after stop: got %v, want ErrClosed", err)
	}

	err = p.SendFindNode(makeNode(1), []uint{1})
	if err != ErrClosed {
		t.Fatalf("SendFindNode after stop: got %v, want ErrClosed", err)
	}
}

func TestPingPongEncoding(t *testing.T) {
	ping := Ping{ReqID: []byte{1, 2, 3}, ENRSeq: 42}
	data, err := rlp.EncodeToBytes(ping)
	if err != nil {
		t.Fatal(err)
	}

	var decoded Ping
	if err := rlp.DecodeBytes(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ENRSeq != 42 {
		t.Fatalf("ENRSeq = %d, want 42", decoded.ENRSeq)
	}
	if len(decoded.ReqID) != 3 {
		t.Fatalf("ReqID len = %d, want 3", len(decoded.ReqID))
	}
}

func TestFindNodeEncoding(t *testing.T) {
	req := FindNode{
		ReqID:     []byte{4, 5, 6},
		Distances: []uint{1, 128, 256},
	}
	data, err := rlp.EncodeToBytes(req)
	if err != nil {
		t.Fatal(err)
	}

	var decoded FindNode
	if err := rlp.DecodeBytes(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Distances) != 3 {
		t.Fatalf("Distances len = %d, want 3", len(decoded.Distances))
	}
}

func TestNodesEncoding(t *testing.T) {
	nodes := Nodes{
		ReqID: []byte{7, 8, 9},
		Total: 1,
		ENRs:  [][]byte{{0xc0}, {0xc1, 0x80}},
	}
	data, err := rlp.EncodeToBytes(nodes)
	if err != nil {
		t.Fatal(err)
	}

	var decoded Nodes
	if err := rlp.DecodeBytes(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Total != 1 {
		t.Fatalf("Total = %d, want 1", decoded.Total)
	}
	if len(decoded.ENRs) != 2 {
		t.Fatalf("ENRs len = %d, want 2", len(decoded.ENRs))
	}
}

func TestEncodeDecodeMessage(t *testing.T) {
	ping := Ping{ReqID: []byte{1}, ENRSeq: 10}
	packet, err := EncodeMessage(MsgPing, ping)
	if err != nil {
		t.Fatal(err)
	}

	msgType, payload, err := DecodeMessage(packet)
	if err != nil {
		t.Fatal(err)
	}
	if msgType != MsgPing {
		t.Fatalf("msgType = 0x%02x, want 0x%02x", msgType, MsgPing)
	}
	if len(payload) == 0 {
		t.Fatal("empty payload")
	}
}

func TestDecodeMessageTooShort(t *testing.T) {
	_, _, err := DecodeMessage([]byte{0x01})
	if err != ErrInvalidMessage {
		t.Fatalf("expected ErrInvalidMessage, got %v", err)
	}
}

func TestV5HandlePacketPing(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	node, conn := makeLocalNode(t)
	defer conn.Close()

	p := NewV5Protocol(conn, key, node)

	// Craft a ping packet.
	ping := Ping{ReqID: []byte{1, 2, 3}, ENRSeq: 5}
	data, err := rlp.EncodeToBytes(ping)
	if err != nil {
		t.Fatal(err)
	}
	packet := append([]byte{MsgPing}, data...)

	// HandlePacket should not panic.
	from := net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 30303}
	p.HandlePacket(from, packet)
}

func TestV5GetSession(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	node, conn := makeLocalNode(t)
	defer conn.Close()

	p := NewV5Protocol(conn, key, node)

	id := makeNodeID(42)
	_, ok := p.GetSession(id)
	if ok {
		t.Fatal("expected no session for unknown node")
	}

	// Manually add a session.
	p.mu.Lock()
	p.sessions[id] = &Session{NodeID: id, Established: true}
	p.mu.Unlock()

	s, ok := p.GetSession(id)
	if !ok {
		t.Fatal("expected session after manual add")
	}
	if !s.Established {
		t.Fatal("session should be established")
	}
}

func TestV5Table(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	node, conn := makeLocalNode(t)
	defer conn.Close()

	p := NewV5Protocol(conn, key, node)
	tab := p.Table()
	if tab == nil {
		t.Fatal("Table() returned nil")
	}
	if tab.Self() != node.ID {
		t.Fatal("table self ID mismatch")
	}
}
