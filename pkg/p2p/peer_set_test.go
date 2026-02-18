package p2p

import (
	"fmt"
	"sync"
	"testing"
)

// --- ManagedPeerSet additional tests ---

func TestManagedPeerSet_AddGet(t *testing.T) {
	ps := NewManagedPeerSet(10)

	p := NewPeer("peer-a", "10.0.0.1:30303", nil)
	if err := ps.Add(p); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got := ps.Get("peer-a")
	if got == nil {
		t.Fatal("Get returned nil for existing peer")
	}
	if got.ID() != "peer-a" {
		t.Errorf("Get returned peer with ID %q, want %q", got.ID(), "peer-a")
	}
}

func TestManagedPeerSet_GetNonexistent(t *testing.T) {
	ps := NewManagedPeerSet(10)

	if got := ps.Get("no-such-peer"); got != nil {
		t.Errorf("Get returned %v for nonexistent peer, want nil", got)
	}
}

func TestManagedPeerSet_RemoveNonexistent(t *testing.T) {
	ps := NewManagedPeerSet(10)

	err := ps.Remove("ghost")
	if err != ErrPeerNotRegistered {
		t.Errorf("Remove nonexistent: got %v, want ErrPeerNotRegistered", err)
	}
}

func TestManagedPeerSet_AddDuplicate(t *testing.T) {
	ps := NewManagedPeerSet(10)
	p := NewPeer("dup", "1.2.3.4:30303", nil)

	if err := ps.Add(p); err != nil {
		t.Fatalf("first Add: %v", err)
	}
	if err := ps.Add(p); err != ErrPeerAlreadyRegistered {
		t.Errorf("duplicate Add: got %v, want ErrPeerAlreadyRegistered", err)
	}
}

func TestManagedPeerSet_MaxPeersEnforcement(t *testing.T) {
	ps := NewManagedPeerSet(3)

	for i := 0; i < 3; i++ {
		p := NewPeer(fmt.Sprintf("p%d", i), fmt.Sprintf("10.0.0.%d:30303", i), nil)
		if err := ps.Add(p); err != nil {
			t.Fatalf("Add p%d: %v", i, err)
		}
	}

	// Fourth peer exceeds capacity.
	overflow := NewPeer("p3", "10.0.0.3:30303", nil)
	if err := ps.Add(overflow); err != ErrMaxPeers {
		t.Errorf("Add beyond max: got %v, want ErrMaxPeers", err)
	}
	if ps.Len() != 3 {
		t.Errorf("Len after rejected add: got %d, want 3", ps.Len())
	}
}

func TestManagedPeerSet_RemoveThenAdd(t *testing.T) {
	ps := NewManagedPeerSet(2)

	p1 := NewPeer("p1", "1.1.1.1:30303", nil)
	p2 := NewPeer("p2", "2.2.2.2:30303", nil)
	p3 := NewPeer("p3", "3.3.3.3:30303", nil)

	ps.Add(p1)
	ps.Add(p2)

	// Remove p1, then p3 should fit.
	if err := ps.Remove("p1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := ps.Add(p3); err != nil {
		t.Errorf("Add after remove: %v", err)
	}
	if ps.Len() != 2 {
		t.Errorf("Len: got %d, want 2", ps.Len())
	}
}

func TestManagedPeerSet_CloseClears(t *testing.T) {
	ps := NewManagedPeerSet(10)
	ps.Add(NewPeer("a", "1.1.1.1:1", nil))
	ps.Add(NewPeer("b", "2.2.2.2:2", nil))

	ps.Close()

	if ps.Len() != 0 {
		t.Errorf("Len after close: got %d, want 0", ps.Len())
	}
}

func TestManagedPeerSet_AddAfterClose(t *testing.T) {
	ps := NewManagedPeerSet(10)
	ps.Close()

	err := ps.Add(NewPeer("x", "1.2.3.4:30303", nil))
	if err != ErrPeerSetClosed {
		t.Errorf("Add after close: got %v, want ErrPeerSetClosed", err)
	}
}

func TestManagedPeerSet_RemoveAfterClose(t *testing.T) {
	ps := NewManagedPeerSet(10)
	ps.Add(NewPeer("a", "1.1.1.1:1", nil))
	ps.Close()

	err := ps.Remove("a")
	if err != ErrPeerSetClosed {
		t.Errorf("Remove after close: got %v, want ErrPeerSetClosed", err)
	}
}

func TestManagedPeerSet_PeersSnapshot(t *testing.T) {
	ps := NewManagedPeerSet(10)
	p1 := NewPeer("a", "1.1.1.1:1", nil)
	p2 := NewPeer("b", "2.2.2.2:2", nil)
	p3 := NewPeer("c", "3.3.3.3:3", nil)

	ps.Add(p1)
	ps.Add(p2)
	ps.Add(p3)

	peers := ps.Peers()
	if len(peers) != 3 {
		t.Fatalf("Peers: got %d, want 3", len(peers))
	}

	// Verify all peers are present by collecting IDs.
	ids := make(map[string]bool)
	for _, p := range peers {
		ids[p.ID()] = true
	}
	for _, want := range []string{"a", "b", "c"} {
		if !ids[want] {
			t.Errorf("Peers missing ID %q", want)
		}
	}
}

func TestManagedPeerSet_PeersEmpty(t *testing.T) {
	ps := NewManagedPeerSet(10)
	peers := ps.Peers()
	if len(peers) != 0 {
		t.Errorf("Peers on empty set: got %d, want 0", len(peers))
	}
}

func TestManagedPeerSet_LenZeroCapacity(t *testing.T) {
	ps := NewManagedPeerSet(0)
	err := ps.Add(NewPeer("a", "1.1.1.1:1", nil))
	if err != ErrMaxPeers {
		t.Errorf("Add to zero-capacity set: got %v, want ErrMaxPeers", err)
	}
	if ps.Len() != 0 {
		t.Errorf("Len: got %d, want 0", ps.Len())
	}
}

func TestManagedPeerSet_Concurrent(t *testing.T) {
	ps := NewManagedPeerSet(200)
	const n = 100
	var wg sync.WaitGroup

	// Concurrent adds.
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			p := NewPeer(fmt.Sprintf("peer%d", i), fmt.Sprintf("10.0.0.%d:30303", i%256), nil)
			ps.Add(p)
		}(i)
	}
	wg.Wait()

	if ps.Len() != n {
		t.Errorf("Len after concurrent adds: got %d, want %d", ps.Len(), n)
	}

	// Concurrent reads and mutations.
	wg.Add(n * 3)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			ps.Get(fmt.Sprintf("peer%d", i))
		}(i)
		go func() {
			defer wg.Done()
			ps.Peers()
		}()
		go func() {
			defer wg.Done()
			ps.Len()
		}()
	}
	wg.Wait()

	// Concurrent removes.
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			ps.Remove(fmt.Sprintf("peer%d", i))
		}(i)
	}
	wg.Wait()

	if ps.Len() != 0 {
		t.Errorf("Len after concurrent removes: got %d, want 0", ps.Len())
	}
}

func TestManagedPeerSet_ConcurrentAddClose(t *testing.T) {
	ps := NewManagedPeerSet(1000)
	var wg sync.WaitGroup

	// Race adds against close.
	wg.Add(51)
	for i := 0; i < 50; i++ {
		go func(i int) {
			defer wg.Done()
			p := NewPeer(fmt.Sprintf("p%d", i), fmt.Sprintf("10.0.0.%d:30303", i%256), nil)
			// Errors are expected; we just check no data race occurs.
			ps.Add(p)
		}(i)
	}
	go func() {
		defer wg.Done()
		ps.Close()
	}()

	wg.Wait()
	// After close, set should be empty and refuse adds.
	if err := ps.Add(NewPeer("late", "9.9.9.9:1", nil)); err != ErrPeerSetClosed {
		t.Errorf("Add after concurrent close: got %v, want ErrPeerSetClosed", err)
	}
}

func TestManagedPeerSet_DoubleClose(t *testing.T) {
	ps := NewManagedPeerSet(10)
	ps.Add(NewPeer("a", "1.1.1.1:1", nil))

	// Double close should not panic.
	ps.Close()
	ps.Close()

	if ps.Len() != 0 {
		t.Errorf("Len after double close: got %d, want 0", ps.Len())
	}
}
