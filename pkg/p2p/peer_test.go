package p2p

import (
	"fmt"
	"math/big"
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestNewPeer(t *testing.T) {
	caps := []Cap{{Name: "eth", Version: 68}}
	p := NewPeer("peer1", "192.168.1.1:30303", caps)

	if p.ID() != "peer1" {
		t.Errorf("ID() = %q, want %q", p.ID(), "peer1")
	}
	if p.RemoteAddr() != "192.168.1.1:30303" {
		t.Errorf("RemoteAddr() = %q, want %q", p.RemoteAddr(), "192.168.1.1:30303")
	}
	gotCaps := p.Caps()
	if len(gotCaps) != 1 {
		t.Fatalf("len(Caps()) = %d, want 1", len(gotCaps))
	}
	if gotCaps[0].Name != "eth" || gotCaps[0].Version != 68 {
		t.Errorf("Caps()[0] = %+v, want {eth 68}", gotCaps[0])
	}
	if p.TD().Sign() != 0 {
		t.Errorf("initial TD = %v, want 0", p.TD())
	}
	if !p.Head().IsZero() {
		t.Errorf("initial Head is not zero")
	}
}

func TestPeerCapsIsolation(t *testing.T) {
	caps := []Cap{{Name: "eth", Version: 68}}
	p := NewPeer("peer1", "127.0.0.1:30303", caps)

	// Mutating the original caps slice should not affect the peer.
	caps[0].Name = "modified"
	gotCaps := p.Caps()
	if gotCaps[0].Name != "eth" {
		t.Error("peer caps were mutated by external modification")
	}

	// Mutating the returned caps should not affect the peer.
	gotCaps[0].Name = "hacked"
	if p.Caps()[0].Name != "eth" {
		t.Error("peer caps were mutated via returned slice")
	}
}

func TestPeerSetHead(t *testing.T) {
	p := NewPeer("peer1", "127.0.0.1:30303", nil)
	head := types.HexToHash("abcdef")
	td := big.NewInt(50000)

	p.SetHead(head, td)

	if p.Head() != head {
		t.Errorf("Head() = %v, want %v", p.Head(), head)
	}
	if p.TD().Cmp(td) != 0 {
		t.Errorf("TD() = %v, want %v", p.TD(), td)
	}

	// Mutating the original td should not affect the peer.
	td.SetInt64(0)
	if p.TD().Cmp(big.NewInt(50000)) != 0 {
		t.Error("peer TD was mutated by external big.Int modification")
	}
}

func TestPeerSetHeadNilTD(t *testing.T) {
	p := NewPeer("peer1", "127.0.0.1:30303", nil)
	head := types.HexToHash("abcdef")

	// Setting nil TD should keep the previous TD.
	p.SetHead(head, big.NewInt(100))
	p.SetHead(head, nil)

	if p.TD().Cmp(big.NewInt(100)) != 0 {
		t.Errorf("TD() = %v, want 100 (nil TD should preserve previous)", p.TD())
	}
}

func TestPeerSetVersion(t *testing.T) {
	p := NewPeer("peer1", "127.0.0.1:30303", nil)
	p.SetVersion(ETH68)
	if p.Version() != ETH68 {
		t.Errorf("Version() = %d, want %d", p.Version(), ETH68)
	}
}

func TestPeerTDIsolation(t *testing.T) {
	p := NewPeer("peer1", "127.0.0.1:30303", nil)
	p.SetHead(types.Hash{}, big.NewInt(42))

	// Returned TD should be a copy.
	td := p.TD()
	td.SetInt64(9999)
	if p.TD().Cmp(big.NewInt(42)) != 0 {
		t.Error("peer TD was mutated via returned big.Int")
	}
}

func TestPeerSetRegisterUnregister(t *testing.T) {
	ps := NewPeerSet()
	p1 := NewPeer("peer1", "1.2.3.4:30303", nil)
	p2 := NewPeer("peer2", "5.6.7.8:30303", nil)

	if ps.Len() != 0 {
		t.Errorf("Len() = %d, want 0", ps.Len())
	}

	// Register.
	if err := ps.Register(p1); err != nil {
		t.Fatalf("Register(p1) error: %v", err)
	}
	if err := ps.Register(p2); err != nil {
		t.Fatalf("Register(p2) error: %v", err)
	}
	if ps.Len() != 2 {
		t.Errorf("Len() = %d, want 2", ps.Len())
	}

	// Duplicate registration.
	if err := ps.Register(p1); err != ErrPeerAlreadyRegistered {
		t.Errorf("duplicate Register error = %v, want ErrPeerAlreadyRegistered", err)
	}

	// Lookup.
	if got := ps.Peer("peer1"); got != p1 {
		t.Error("Peer(peer1) did not return p1")
	}
	if got := ps.Peer("unknown"); got != nil {
		t.Error("Peer(unknown) should return nil")
	}

	// Unregister.
	if err := ps.Unregister("peer1"); err != nil {
		t.Fatalf("Unregister(peer1) error: %v", err)
	}
	if ps.Len() != 1 {
		t.Errorf("Len() = %d, want 1", ps.Len())
	}
	if got := ps.Peer("peer1"); got != nil {
		t.Error("Peer(peer1) should return nil after unregister")
	}

	// Unregister unknown.
	if err := ps.Unregister("nonexistent"); err != ErrPeerNotRegistered {
		t.Errorf("Unregister(nonexistent) error = %v, want ErrPeerNotRegistered", err)
	}
}

func TestPeerSetBestPeer(t *testing.T) {
	ps := NewPeerSet()

	// Empty set returns nil.
	if best := ps.BestPeer(); best != nil {
		t.Error("BestPeer() on empty set should return nil")
	}

	p1 := NewPeer("peer1", "1.2.3.4:30303", nil)
	p1.SetHead(types.Hash{}, big.NewInt(100))

	p2 := NewPeer("peer2", "5.6.7.8:30303", nil)
	p2.SetHead(types.Hash{}, big.NewInt(200))

	p3 := NewPeer("peer3", "9.10.11.12:30303", nil)
	p3.SetHead(types.Hash{}, big.NewInt(150))

	ps.Register(p1)
	ps.Register(p2)
	ps.Register(p3)

	best := ps.BestPeer()
	if best == nil {
		t.Fatal("BestPeer() returned nil")
	}
	if best.ID() != "peer2" {
		t.Errorf("BestPeer().ID() = %q, want %q", best.ID(), "peer2")
	}
}

func TestPeerSetPeers(t *testing.T) {
	ps := NewPeerSet()
	p1 := NewPeer("peer1", "1.2.3.4:30303", nil)
	p2 := NewPeer("peer2", "5.6.7.8:30303", nil)

	ps.Register(p1)
	ps.Register(p2)

	peers := ps.Peers()
	if len(peers) != 2 {
		t.Errorf("len(Peers()) = %d, want 2", len(peers))
	}

	// Verify both peers are present.
	ids := make(map[string]bool)
	for _, p := range peers {
		ids[p.ID()] = true
	}
	if !ids["peer1"] || !ids["peer2"] {
		t.Errorf("Peers() missing expected peers, got IDs: %v", ids)
	}
}

func TestPeerSetConcurrency(t *testing.T) {
	ps := NewPeerSet()
	const n = 100

	var wg sync.WaitGroup

	// Concurrent registrations.
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			p := NewPeer(
				fmt.Sprintf("peer%d", i),
				fmt.Sprintf("10.0.0.%d:30303", i%256),
				nil,
			)
			p.SetHead(types.Hash{}, big.NewInt(int64(i)))
			ps.Register(p)
		}(i)
	}
	wg.Wait()

	if ps.Len() != n {
		t.Errorf("Len() = %d, want %d after concurrent registrations", ps.Len(), n)
	}

	// Concurrent reads.
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			ps.BestPeer()
			ps.Len()
			ps.Peers()
		}()
	}
	wg.Wait()

	// Concurrent unregistrations.
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			ps.Unregister(fmt.Sprintf("peer%d", i))
		}(i)
	}
	wg.Wait()

	if ps.Len() != 0 {
		t.Errorf("Len() = %d, want 0 after concurrent unregistrations", ps.Len())
	}
}
