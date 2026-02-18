package p2p

import (
	"sync"
	"testing"
)

func TestParseEnode(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
		wantIP  string
		wantTCP uint16
		wantUDP uint16
		wantID  NodeID
	}{
		{
			name:    "valid enode",
			url:     "enode://abcdef1234567890@127.0.0.1:30303",
			wantIP:  "127.0.0.1",
			wantTCP: 30303,
			wantUDP: 30303,
			wantID:  "abcdef1234567890",
		},
		{
			name:    "with discport",
			url:     "enode://deadbeef@10.0.0.1:30303?discport=30304",
			wantIP:  "10.0.0.1",
			wantTCP: 30303,
			wantUDP: 30304,
			wantID:  "deadbeef",
		},
		{
			name:    "missing prefix",
			url:     "enr://abcdef@127.0.0.1:30303",
			wantErr: true,
		},
		{
			name:    "empty node ID",
			url:     "enode://@127.0.0.1:30303",
			wantErr: true,
		},
		{
			name:    "missing port",
			url:     "enode://abc@127.0.0.1",
			wantErr: true,
		},
		{
			name:    "bad discport",
			url:     "enode://abc@127.0.0.1:30303?discport=notanumber",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := ParseEnode(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if node.ID != tt.wantID {
				t.Errorf("ID = %q, want %q", node.ID, tt.wantID)
			}
			if node.IP.String() != tt.wantIP {
				t.Errorf("IP = %q, want %q", node.IP.String(), tt.wantIP)
			}
			if node.TCP != tt.wantTCP {
				t.Errorf("TCP = %d, want %d", node.TCP, tt.wantTCP)
			}
			if node.UDP != tt.wantUDP {
				t.Errorf("UDP = %d, want %d", node.UDP, tt.wantUDP)
			}
		})
	}
}

func TestNodeAddr(t *testing.T) {
	n := &Node{
		IP:  parseIP("192.168.1.1"),
		TCP: 30303,
	}
	if got := n.Addr(); got != "192.168.1.1:30303" {
		t.Errorf("Addr() = %q, want %q", got, "192.168.1.1:30303")
	}
}

func TestNodeString(t *testing.T) {
	n := &Node{
		ID:  "abcdef",
		IP:  parseIP("10.0.0.1"),
		TCP: 30303,
	}
	s := n.String()
	if s == "" {
		t.Error("String() returned empty")
	}

	// With a name set.
	n.Name = "bootnode1"
	if got := n.String(); got != "bootnode1@10.0.0.1:30303" {
		t.Errorf("String() = %q, want %q", got, "bootnode1@10.0.0.1:30303")
	}
}

func TestNodeTable_AddStatic(t *testing.T) {
	nt := NewNodeTable()

	n := &Node{ID: "node1", IP: parseIP("1.2.3.4"), TCP: 30303}
	if err := nt.AddStatic(n); err != nil {
		t.Fatalf("AddStatic: %v", err)
	}
	if nt.Len() != 1 {
		t.Errorf("Len = %d, want 1", nt.Len())
	}

	// Duplicate should fail.
	if err := nt.AddStatic(n); err != ErrNodeAlreadyKnown {
		t.Errorf("duplicate AddStatic: got %v, want ErrNodeAlreadyKnown", err)
	}

	// Should be static.
	if !nt.IsStatic("node1") {
		t.Error("node1 should be static")
	}
}

func TestNodeTable_AddNode(t *testing.T) {
	nt := NewNodeTable()

	n := &Node{ID: "node1", IP: parseIP("1.2.3.4"), TCP: 30303}
	if err := nt.AddNode(n); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	// Non-static node.
	if nt.IsStatic("node1") {
		t.Error("node1 should not be static")
	}

	// Duplicate.
	if err := nt.AddNode(n); err != ErrNodeAlreadyKnown {
		t.Errorf("duplicate AddNode: got %v, want ErrNodeAlreadyKnown", err)
	}
}

func TestNodeTable_GetRemove(t *testing.T) {
	nt := NewNodeTable()
	n := &Node{ID: "node1", IP: parseIP("1.2.3.4"), TCP: 30303}
	nt.AddStatic(n)

	got := nt.Get("node1")
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.ID != "node1" {
		t.Errorf("Get ID = %q, want %q", got.ID, "node1")
	}

	// Remove.
	nt.Remove("node1")
	if nt.Get("node1") != nil {
		t.Error("Get after Remove should return nil")
	}
	if nt.Len() != 0 {
		t.Errorf("Len after Remove = %d, want 0", nt.Len())
	}
}

func TestNodeTable_StaticNodes(t *testing.T) {
	nt := NewNodeTable()

	s1 := &Node{ID: "static1", IP: parseIP("1.1.1.1"), TCP: 30303}
	s2 := &Node{ID: "static2", IP: parseIP("2.2.2.2"), TCP: 30303}
	d1 := &Node{ID: "dynamic1", IP: parseIP("3.3.3.3"), TCP: 30303}

	nt.AddStatic(s1)
	nt.AddStatic(s2)
	nt.AddNode(d1)

	statics := nt.StaticNodes()
	if len(statics) != 2 {
		t.Errorf("StaticNodes count = %d, want 2", len(statics))
	}

	all := nt.AllNodes()
	if len(all) != 3 {
		t.Errorf("AllNodes count = %d, want 3", len(all))
	}
}

func TestNodeTable_MarkSeenFailed(t *testing.T) {
	nt := NewNodeTable()
	n := &Node{ID: "node1", IP: parseIP("1.2.3.4"), TCP: 30303}
	nt.AddNode(n)

	// Initial fail count is 0.
	if fc := nt.FailCount("node1"); fc != 0 {
		t.Errorf("initial FailCount = %d, want 0", fc)
	}

	// Mark failed twice.
	nt.MarkFailed("node1")
	nt.MarkFailed("node1")
	if fc := nt.FailCount("node1"); fc != 2 {
		t.Errorf("FailCount after 2 failures = %d, want 2", fc)
	}

	// MarkSeen resets.
	nt.MarkSeen("node1")
	if fc := nt.FailCount("node1"); fc != 0 {
		t.Errorf("FailCount after MarkSeen = %d, want 0", fc)
	}

	// FailCount for unknown node.
	if fc := nt.FailCount("unknown"); fc != 0 {
		t.Errorf("FailCount(unknown) = %d, want 0", fc)
	}
}

func TestNodeTable_Evict(t *testing.T) {
	nt := NewNodeTable()

	// Add 3 dynamic nodes, 1 static.
	for i := 0; i < 3; i++ {
		n := &Node{ID: NodeID(string(rune('a' + i))), IP: parseIP("1.2.3.4"), TCP: 30303}
		nt.AddNode(n)
	}
	s := &Node{ID: "static", IP: parseIP("5.5.5.5"), TCP: 30303}
	nt.AddStatic(s)

	// Fail two dynamic nodes past the threshold.
	nt.MarkFailed(NodeID("a"))
	nt.MarkFailed(NodeID("a"))
	nt.MarkFailed(NodeID("a"))
	nt.MarkFailed(NodeID("b"))
	nt.MarkFailed(NodeID("b"))
	nt.MarkFailed(NodeID("b"))

	// Also fail the static node -- it should survive eviction.
	nt.MarkFailed(NodeID("static"))
	nt.MarkFailed(NodeID("static"))
	nt.MarkFailed(NodeID("static"))

	evicted := nt.Evict(3)
	if evicted != 2 {
		t.Errorf("Evict returned %d, want 2", evicted)
	}

	// Static node and 1 unfailed dynamic node should remain.
	if nt.Len() != 2 {
		t.Errorf("Len after eviction = %d, want 2", nt.Len())
	}
	if nt.Get("static") == nil {
		t.Error("static node was evicted")
	}
}

func TestNodeTable_Concurrency(t *testing.T) {
	nt := NewNodeTable()
	var wg sync.WaitGroup

	// Concurrent adds.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := NodeID(string(rune(i + 100)))
			n := &Node{ID: id, IP: parseIP("1.2.3.4"), TCP: uint16(30000 + i)}
			nt.AddNode(n)
		}(i)
	}
	wg.Wait()

	if nt.Len() != 50 {
		t.Errorf("Len = %d, want 50", nt.Len())
	}

	// Concurrent reads and writes.
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			nt.AllNodes()
			nt.StaticNodes()
		}(i)
		go func(i int) {
			defer wg.Done()
			id := NodeID(string(rune(i + 100)))
			nt.MarkFailed(id)
			nt.MarkSeen(id)
		}(i)
	}
	wg.Wait()
}

// parseIP is a test helper that parses an IP string.
func parseIP(s string) []byte {
	// Return as net.IP bytes.
	parts := make([]byte, 4)
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			n++
			continue
		}
		parts[n] = parts[n]*10 + (s[i] - '0')
	}
	return parts
}
