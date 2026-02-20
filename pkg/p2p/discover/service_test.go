package discover

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestNewDiscoveryService(t *testing.T) {
	ds := NewDiscoveryService(DiscoveryServiceConfig{})
	if ds.config.MaxNodes != 256 {
		t.Fatalf("default MaxNodes = %d, want 256", ds.config.MaxNodes)
	}
	if ds.config.RefreshInterval != 30 {
		t.Fatalf("default RefreshInterval = %d, want 30", ds.config.RefreshInterval)
	}
	if ds.config.BootnodeTimeout != 60 {
		t.Fatalf("default BootnodeTimeout = %d, want 60", ds.config.BootnodeTimeout)
	}
	if ds.ActiveNodes() != 0 {
		t.Fatalf("ActiveNodes() = %d, want 0", ds.ActiveNodes())
	}
}

func TestNewDiscoveryServiceCustomConfig(t *testing.T) {
	cfg := DiscoveryServiceConfig{
		MaxNodes:        10,
		RefreshInterval: 5,
		BootnodeTimeout: 120,
		EnableDNS:       true,
	}
	ds := NewDiscoveryService(cfg)
	if ds.config.MaxNodes != 10 {
		t.Fatalf("MaxNodes = %d, want 10", ds.config.MaxNodes)
	}
	if !ds.config.EnableDNS {
		t.Fatal("EnableDNS should be true")
	}
}

func TestAddBootnode(t *testing.T) {
	ds := NewDiscoveryService(DiscoveryServiceConfig{MaxNodes: 100})
	err := ds.AddBootnode("node-1", "10.0.0.1", 30303)
	if err != nil {
		t.Fatalf("AddBootnode: %v", err)
	}
	if ds.ActiveNodes() != 1 {
		t.Fatalf("ActiveNodes() = %d, want 1", ds.ActiveNodes())
	}
}

func TestAddBootnodeEmptyID(t *testing.T) {
	ds := NewDiscoveryService(DiscoveryServiceConfig{})
	err := ds.AddBootnode("", "10.0.0.1", 30303)
	if err != ErrEmptyID {
		t.Fatalf("expected ErrEmptyID, got %v", err)
	}
}

func TestAddBootnodeInvalidPort(t *testing.T) {
	ds := NewDiscoveryService(DiscoveryServiceConfig{})
	err := ds.AddBootnode("node-1", "10.0.0.1", 0)
	if err != ErrInvalidPort {
		t.Fatalf("expected ErrInvalidPort, got %v", err)
	}
}

func TestAddBootnodeDuplicate(t *testing.T) {
	ds := NewDiscoveryService(DiscoveryServiceConfig{MaxNodes: 100})
	_ = ds.AddBootnode("node-1", "10.0.0.1", 30303)
	err := ds.AddBootnode("node-1", "10.0.0.1", 30303)
	if err != ErrNodeExists {
		t.Fatalf("expected ErrNodeExists, got %v", err)
	}
}

func TestAddBootnodeTableFull(t *testing.T) {
	ds := NewDiscoveryService(DiscoveryServiceConfig{MaxNodes: 2})
	_ = ds.AddBootnode("a", "10.0.0.1", 30303)
	_ = ds.AddBootnode("b", "10.0.0.2", 30303)
	err := ds.AddBootnode("c", "10.0.0.3", 30303)
	if err != ErrTableFull {
		t.Fatalf("expected ErrTableFull, got %v", err)
	}
}

func TestServiceRemoveNode(t *testing.T) {
	ds := NewDiscoveryService(DiscoveryServiceConfig{MaxNodes: 100})
	_ = ds.AddBootnode("node-1", "10.0.0.1", 30303)
	ds.RemoveNode("node-1")
	if ds.ActiveNodes() != 0 {
		t.Fatalf("ActiveNodes() = %d after remove, want 0", ds.ActiveNodes())
	}
}

func TestServiceRemoveNodeNonexistent(t *testing.T) {
	ds := NewDiscoveryService(DiscoveryServiceConfig{})
	// Should not panic.
	ds.RemoveNode("nonexistent")
}

func TestGetNode(t *testing.T) {
	ds := NewDiscoveryService(DiscoveryServiceConfig{MaxNodes: 100})
	_ = ds.AddBootnode("node-1", "10.0.0.1", 30303)
	n := ds.GetNode("node-1")
	if n == nil {
		t.Fatal("GetNode returned nil")
	}
	if n.ID != "node-1" {
		t.Fatalf("ID = %q, want %q", n.ID, "node-1")
	}
	if n.IP != "10.0.0.1" {
		t.Fatalf("IP = %q, want %q", n.IP, "10.0.0.1")
	}
	if n.Port != 30303 {
		t.Fatalf("Port = %d, want 30303", n.Port)
	}
}

func TestGetNodeNotFound(t *testing.T) {
	ds := NewDiscoveryService(DiscoveryServiceConfig{})
	n := ds.GetNode("missing")
	if n != nil {
		t.Fatal("expected nil for missing node")
	}
}

func TestGetNodeReturnsCopy(t *testing.T) {
	ds := NewDiscoveryService(DiscoveryServiceConfig{MaxNodes: 100})
	_ = ds.AddNode(DiscoveredNode{
		ID:        "node-1",
		IP:        "10.0.0.1",
		Port:      30303,
		Protocols: []string{"eth/68"},
		LastSeen:  time.Now().Unix(),
	})
	n := ds.GetNode("node-1")
	n.IP = "mutated"
	original := ds.GetNode("node-1")
	if original.IP == "mutated" {
		t.Fatal("GetNode did not return a copy")
	}
}

func TestNearestNodes(t *testing.T) {
	ds := NewDiscoveryService(DiscoveryServiceConfig{MaxNodes: 100})
	for i := 0; i < 10; i++ {
		_ = ds.AddBootnode(fmt.Sprintf("node-%d", i), "10.0.0.1", 30303)
	}

	nearest := ds.NearestNodes("node-5", 3)
	if len(nearest) != 3 {
		t.Fatalf("NearestNodes returned %d, want 3", len(nearest))
	}

	// The closest node should be node-5 itself (distance 0).
	if nearest[0].ID != "node-5" {
		t.Fatalf("nearest[0].ID = %q, want %q", nearest[0].ID, "node-5")
	}
	if nearest[0].Distance != 0 {
		t.Fatalf("nearest[0].Distance = %d, want 0", nearest[0].Distance)
	}
}

func TestNearestNodesCountExceedsTable(t *testing.T) {
	ds := NewDiscoveryService(DiscoveryServiceConfig{MaxNodes: 100})
	_ = ds.AddBootnode("a", "10.0.0.1", 30303)
	_ = ds.AddBootnode("b", "10.0.0.2", 30303)

	nearest := ds.NearestNodes("a", 100)
	if len(nearest) != 2 {
		t.Fatalf("NearestNodes returned %d, want 2", len(nearest))
	}
}

func TestNearestNodesEmpty(t *testing.T) {
	ds := NewDiscoveryService(DiscoveryServiceConfig{})
	result := ds.NearestNodes("target", 5)
	if result != nil {
		t.Fatalf("expected nil from empty table, got %d entries", len(result))
	}
}

func TestNearestNodesZeroCount(t *testing.T) {
	ds := NewDiscoveryService(DiscoveryServiceConfig{MaxNodes: 100})
	_ = ds.AddBootnode("a", "10.0.0.1", 30303)
	result := ds.NearestNodes("a", 0)
	if result != nil {
		t.Fatal("expected nil for count=0")
	}
}

func TestNearestNodesSorted(t *testing.T) {
	ds := NewDiscoveryService(DiscoveryServiceConfig{MaxNodes: 100})
	_ = ds.AddBootnode("aaa", "10.0.0.1", 30303)
	_ = ds.AddBootnode("bbb", "10.0.0.2", 30303)
	_ = ds.AddBootnode("zzz", "10.0.0.3", 30303)

	nearest := ds.NearestNodes("aaa", 3)
	for i := 1; i < len(nearest); i++ {
		if nearest[i].Distance < nearest[i-1].Distance {
			t.Fatal("NearestNodes results not sorted by ascending distance")
		}
	}
}

func TestRefreshNodes(t *testing.T) {
	ds := NewDiscoveryService(DiscoveryServiceConfig{
		MaxNodes:        100,
		BootnodeTimeout: 1,
	})
	// Add a node with LastSeen in the past.
	ds.mu.Lock()
	ds.nodes["stale"] = &DiscoveredNode{
		ID:       "stale",
		IP:       "10.0.0.1",
		Port:     30303,
		LastSeen: time.Now().Unix() - 10, // well past 1-second timeout
	}
	ds.mu.Unlock()

	// Add a fresh node.
	_ = ds.AddBootnode("fresh", "10.0.0.2", 30303)

	ds.RefreshNodes()

	if ds.ActiveNodes() != 1 {
		t.Fatalf("ActiveNodes() = %d after refresh, want 1", ds.ActiveNodes())
	}
	if ds.GetNode("stale") != nil {
		t.Fatal("stale node should have been removed")
	}
	if ds.GetNode("fresh") == nil {
		t.Fatal("fresh node should still exist")
	}
}

func TestSetProtocolFilter(t *testing.T) {
	ds := NewDiscoveryService(DiscoveryServiceConfig{})
	ds.SetProtocolFilter([]string{"eth/68", "snap/1"})

	ds.mu.RLock()
	if len(ds.protocolFilter) != 2 {
		t.Fatalf("protocolFilter len = %d, want 2", len(ds.protocolFilter))
	}
	ds.mu.RUnlock()

	// Clear filter.
	ds.SetProtocolFilter(nil)
	ds.mu.RLock()
	if len(ds.protocolFilter) != 0 {
		t.Fatalf("protocolFilter len = %d after clear, want 0", len(ds.protocolFilter))
	}
	ds.mu.RUnlock()
}

func TestNodesByProtocol(t *testing.T) {
	ds := NewDiscoveryService(DiscoveryServiceConfig{MaxNodes: 100})
	_ = ds.AddNode(DiscoveredNode{
		ID: "eth-node", IP: "10.0.0.1", Port: 30303,
		Protocols: []string{"eth/68", "snap/1"}, LastSeen: time.Now().Unix(),
	})
	_ = ds.AddNode(DiscoveredNode{
		ID: "snap-node", IP: "10.0.0.2", Port: 30303,
		Protocols: []string{"snap/1"}, LastSeen: time.Now().Unix(),
	})
	_ = ds.AddNode(DiscoveredNode{
		ID: "plain-node", IP: "10.0.0.3", Port: 30303,
		Protocols: []string{"les/4"}, LastSeen: time.Now().Unix(),
	})

	ethNodes := ds.NodesByProtocol("eth/68")
	if len(ethNodes) != 1 {
		t.Fatalf("NodesByProtocol(eth/68) = %d, want 1", len(ethNodes))
	}
	if ethNodes[0].ID != "eth-node" {
		t.Fatalf("unexpected node ID %q", ethNodes[0].ID)
	}

	snapNodes := ds.NodesByProtocol("snap/1")
	if len(snapNodes) != 2 {
		t.Fatalf("NodesByProtocol(snap/1) = %d, want 2", len(snapNodes))
	}

	noNodes := ds.NodesByProtocol("nonexistent")
	if len(noNodes) != 0 {
		t.Fatalf("NodesByProtocol(nonexistent) = %d, want 0", len(noNodes))
	}
}

func TestServiceAddNode(t *testing.T) {
	ds := NewDiscoveryService(DiscoveryServiceConfig{MaxNodes: 100})
	err := ds.AddNode(DiscoveredNode{
		ID: "n1", IP: "10.0.0.1", Port: 30303,
		Protocols: []string{"eth/68"}, LastSeen: 1000,
	})
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if ds.ActiveNodes() != 1 {
		t.Fatalf("ActiveNodes() = %d, want 1", ds.ActiveNodes())
	}
}

func TestAddNodeUpdate(t *testing.T) {
	ds := NewDiscoveryService(DiscoveryServiceConfig{MaxNodes: 100})
	_ = ds.AddNode(DiscoveredNode{
		ID: "n1", IP: "10.0.0.1", Port: 30303,
		Protocols: []string{"eth/68"}, LastSeen: 1000,
	})
	// Update with new LastSeen and protocols.
	_ = ds.AddNode(DiscoveredNode{
		ID: "n1", IP: "10.0.0.1", Port: 30303,
		Protocols: []string{"eth/68", "snap/1"}, LastSeen: 2000,
	})
	n := ds.GetNode("n1")
	if n.LastSeen != 2000 {
		t.Fatalf("LastSeen = %d, want 2000", n.LastSeen)
	}
	if len(n.Protocols) != 2 {
		t.Fatalf("Protocols len = %d, want 2", len(n.Protocols))
	}
	if ds.ActiveNodes() != 1 {
		t.Fatalf("ActiveNodes() = %d after update, want 1", ds.ActiveNodes())
	}
}

func TestAddNodeEmptyID(t *testing.T) {
	ds := NewDiscoveryService(DiscoveryServiceConfig{})
	err := ds.AddNode(DiscoveredNode{ID: "", IP: "10.0.0.1", Port: 30303})
	if err != ErrEmptyID {
		t.Fatalf("expected ErrEmptyID, got %v", err)
	}
}

func TestAddNodeTableFull(t *testing.T) {
	ds := NewDiscoveryService(DiscoveryServiceConfig{MaxNodes: 1})
	_ = ds.AddNode(DiscoveredNode{
		ID: "a", IP: "10.0.0.1", Port: 30303, LastSeen: time.Now().Unix(),
	})
	err := ds.AddNode(DiscoveredNode{
		ID: "b", IP: "10.0.0.2", Port: 30303, LastSeen: time.Now().Unix(),
	})
	if err != ErrTableFull {
		t.Fatalf("expected ErrTableFull, got %v", err)
	}
}

func TestConcurrentAccess(t *testing.T) {
	ds := NewDiscoveryService(DiscoveryServiceConfig{MaxNodes: 1000})

	const goroutines = 50
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 3)

	// Concurrent adds.
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				id := fmt.Sprintf("node-%d-%d", g, i)
				_ = ds.AddBootnode(id, "10.0.0.1", 30303)
			}
		}(g)
	}

	// Concurrent reads.
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				ds.GetNode(fmt.Sprintf("node-%d-%d", g, i))
				ds.ActiveNodes()
			}
		}(g)
	}

	// Concurrent nearest lookups.
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				ds.NearestNodes("target", 5)
			}
		}()
	}

	wg.Wait()

	if ds.ActiveNodes() == 0 {
		t.Fatal("expected some nodes after concurrent adds")
	}
}

func TestXorStringDistance(t *testing.T) {
	// Same string -> distance 0.
	if d := xorStringDistance("abc", "abc"); d != 0 {
		t.Fatalf("distance(abc, abc) = %d, want 0", d)
	}
	// Different strings -> distance > 0.
	if d := xorStringDistance("aaa", "bbb"); d <= 0 {
		t.Fatalf("distance(aaa, bbb) = %d, want > 0", d)
	}
	// Symmetry.
	d1 := xorStringDistance("abc", "xyz")
	d2 := xorStringDistance("xyz", "abc")
	if d1 != d2 {
		t.Fatalf("distance not symmetric: %d != %d", d1, d2)
	}
}
