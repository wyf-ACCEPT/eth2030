package p2p

import (
	"math/big"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestPeerManager_AddRemove(t *testing.T) {
	pm := NewPeerManager()

	a, b := MsgPipe()
	defer a.Close()
	defer b.Close()

	p := NewPeer("peer1", "1.2.3.4:30303", nil)

	if err := pm.AddPeer(p, a); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}
	if pm.Len() != 1 {
		t.Errorf("Len = %d, want 1", pm.Len())
	}

	// Duplicate add should fail.
	if err := pm.AddPeer(p, a); err != ErrPeerAlreadyRegistered {
		t.Errorf("duplicate AddPeer: got %v, want ErrPeerAlreadyRegistered", err)
	}

	// Lookup.
	if got := pm.Peer("peer1"); got != p {
		t.Error("Peer(peer1) did not return expected peer")
	}
	if got := pm.Peer("unknown"); got != nil {
		t.Error("Peer(unknown) should return nil")
	}

	// Remove.
	if err := pm.RemovePeer("peer1"); err != nil {
		t.Fatalf("RemovePeer: %v", err)
	}
	if pm.Len() != 0 {
		t.Errorf("Len after remove = %d, want 0", pm.Len())
	}

	// Remove unknown.
	if err := pm.RemovePeer("peer1"); err != ErrPeerNotRegistered {
		t.Errorf("RemovePeer(unknown): got %v, want ErrPeerNotRegistered", err)
	}
}

func TestPeerManager_Peers(t *testing.T) {
	pm := NewPeerManager()

	a1, b1 := MsgPipe()
	defer a1.Close()
	defer b1.Close()
	a2, b2 := MsgPipe()
	defer a2.Close()
	defer b2.Close()

	p1 := NewPeer("peer1", "1.2.3.4:30303", nil)
	p2 := NewPeer("peer2", "5.6.7.8:30303", nil)

	pm.AddPeer(p1, a1)
	pm.AddPeer(p2, a2)

	peers := pm.Peers()
	if len(peers) != 2 {
		t.Errorf("Peers() length = %d, want 2", len(peers))
	}

	ids := map[string]bool{}
	for _, p := range peers {
		ids[p.ID()] = true
	}
	if !ids["peer1"] || !ids["peer2"] {
		t.Errorf("Peers() missing expected peers: %v", ids)
	}
}

func TestPeerManager_BestPeer(t *testing.T) {
	pm := NewPeerManager()

	// Empty manager.
	if best := pm.BestPeer(); best != nil {
		t.Error("BestPeer on empty manager should return nil")
	}

	a1, b1 := MsgPipe()
	defer a1.Close()
	defer b1.Close()
	a2, b2 := MsgPipe()
	defer a2.Close()
	defer b2.Close()
	a3, b3 := MsgPipe()
	defer a3.Close()
	defer b3.Close()

	p1 := NewPeer("peer1", "1.2.3.4:30303", nil)
	p1.SetHead(types.Hash{}, big.NewInt(100))

	p2 := NewPeer("peer2", "5.6.7.8:30303", nil)
	p2.SetHead(types.Hash{}, big.NewInt(500))

	p3 := NewPeer("peer3", "9.10.11.12:30303", nil)
	p3.SetHead(types.Hash{}, big.NewInt(300))

	pm.AddPeer(p1, a1)
	pm.AddPeer(p2, a2)
	pm.AddPeer(p3, a3)

	best := pm.BestPeer()
	if best == nil {
		t.Fatal("BestPeer returned nil")
	}
	if best.ID() != "peer2" {
		t.Errorf("BestPeer().ID() = %q, want %q", best.ID(), "peer2")
	}
}

func TestPeerManager_Close(t *testing.T) {
	pm := NewPeerManager()

	a, b := MsgPipe()
	defer a.Close()
	defer b.Close()

	p := NewPeer("peer1", "1.2.3.4:30303", nil)
	pm.AddPeer(p, a)
	pm.Close()

	if pm.Len() != 0 {
		t.Errorf("Len after close = %d, want 0", pm.Len())
	}
	if err := pm.AddPeer(NewPeer("peer2", "5.6.7.8:30303", nil), a); err != ErrPeerManagerClosed {
		t.Errorf("AddPeer after close: got %v, want ErrPeerManagerClosed", err)
	}
	if err := pm.RemovePeer("peer1"); err != ErrPeerManagerClosed {
		t.Errorf("RemovePeer after close: got %v, want ErrPeerManagerClosed", err)
	}
}

func TestPeerManager_BroadcastBlock(t *testing.T) {
	pm := NewPeerManager()

	// Create two pipe pairs: pm writes to a1/a2, test reads from b1/b2.
	a1, b1 := MsgPipe()
	defer a1.Close()
	defer b1.Close()
	a2, b2 := MsgPipe()
	defer a2.Close()
	defer b2.Close()

	p1 := NewPeer("peer1", "1.2.3.4:30303", nil)
	p2 := NewPeer("peer2", "5.6.7.8:30303", nil)

	pm.AddPeer(p1, a1)
	pm.AddPeer(p2, a2)

	header := &types.Header{
		Number:     big.NewInt(100),
		Difficulty: big.NewInt(0),
		GasLimit:   30000000,
	}
	block := types.NewBlock(header, nil)
	td := big.NewInt(999999)

	// Broadcast to all peers (no exclusions).
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		errs := pm.BroadcastBlock(block, td, nil)
		for _, err := range errs {
			t.Errorf("BroadcastBlock error: %v", err)
		}
	}()

	// Read from both pipes.
	msg1, err := b1.ReadMsg()
	if err != nil {
		t.Fatalf("b1 ReadMsg: %v", err)
	}
	msg2, err := b2.ReadMsg()
	if err != nil {
		t.Fatalf("b2 ReadMsg: %v", err)
	}
	wg.Wait()

	if msg1.Code != NewBlockMsg {
		t.Errorf("msg1.Code = 0x%02x, want 0x%02x", msg1.Code, NewBlockMsg)
	}
	if msg2.Code != NewBlockMsg {
		t.Errorf("msg2.Code = 0x%02x, want 0x%02x", msg2.Code, NewBlockMsg)
	}

	// Verify peer heads were updated.
	if p1.TD().Cmp(td) != 0 {
		t.Errorf("peer1 TD = %v, want %v", p1.TD(), td)
	}
	if p2.TD().Cmp(td) != 0 {
		t.Errorf("peer2 TD = %v, want %v", p2.TD(), td)
	}
}

func TestPeerManager_BroadcastBlockWithExclude(t *testing.T) {
	pm := NewPeerManager()

	a1, b1 := MsgPipe()
	defer a1.Close()
	defer b1.Close()
	a2, b2 := MsgPipe()
	defer a2.Close()
	defer b2.Close()

	p1 := NewPeer("peer1", "1.2.3.4:30303", nil)
	p2 := NewPeer("peer2", "5.6.7.8:30303", nil)

	pm.AddPeer(p1, a1)
	pm.AddPeer(p2, a2)

	header := &types.Header{
		Number:     big.NewInt(50),
		Difficulty: big.NewInt(0),
		GasLimit:   30000000,
	}
	block := types.NewBlock(header, nil)
	td := big.NewInt(12345)

	// Exclude peer1.
	exclude := map[string]bool{"peer1": true}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		pm.BroadcastBlock(block, td, exclude)
	}()

	// Only peer2 should receive the message.
	msg, err := b2.ReadMsg()
	if err != nil {
		t.Fatalf("b2 ReadMsg: %v", err)
	}
	wg.Wait()

	if msg.Code != NewBlockMsg {
		t.Errorf("msg.Code = 0x%02x, want 0x%02x", msg.Code, NewBlockMsg)
	}

	// peer1 should NOT have its head updated.
	if p1.TD().Cmp(big.NewInt(0)) != 0 {
		t.Errorf("excluded peer1 TD = %v, want 0", p1.TD())
	}
	// peer2 SHOULD have its head updated.
	if p2.TD().Cmp(td) != 0 {
		t.Errorf("peer2 TD = %v, want %v", p2.TD(), td)
	}
}

func TestPeerManager_BroadcastTransactions(t *testing.T) {
	pm := NewPeerManager()

	a1, b1 := MsgPipe()
	defer a1.Close()
	defer b1.Close()
	a2, b2 := MsgPipe()
	defer a2.Close()
	defer b2.Close()

	p1 := NewPeer("peer1", "1.2.3.4:30303", nil)
	p2 := NewPeer("peer2", "5.6.7.8:30303", nil)

	pm.AddPeer(p1, a1)
	pm.AddPeer(p2, a2)

	txTypes := []byte{0x02}
	txSizes := []uint32{256}
	txHashes := []types.Hash{types.HexToHash("abcdef")}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		errs := pm.BroadcastTransactions(txTypes, txSizes, txHashes, nil)
		for _, err := range errs {
			t.Errorf("BroadcastTransactions error: %v", err)
		}
	}()

	msg1, err := b1.ReadMsg()
	if err != nil {
		t.Fatalf("b1 ReadMsg: %v", err)
	}
	msg2, err := b2.ReadMsg()
	if err != nil {
		t.Fatalf("b2 ReadMsg: %v", err)
	}
	wg.Wait()

	if msg1.Code != NewPooledTransactionHashesMsg {
		t.Errorf("msg1.Code = 0x%02x, want 0x%02x", msg1.Code, NewPooledTransactionHashesMsg)
	}
	if msg2.Code != NewPooledTransactionHashesMsg {
		t.Errorf("msg2.Code = 0x%02x, want 0x%02x", msg2.Code, NewPooledTransactionHashesMsg)
	}
}

func TestPeerManager_BroadcastBlockHashes(t *testing.T) {
	pm := NewPeerManager()

	a1, b1 := MsgPipe()
	defer a1.Close()
	defer b1.Close()

	p1 := NewPeer("peer1", "1.2.3.4:30303", nil)
	pm.AddPeer(p1, a1)

	hashes := []NewBlockHashesEntry{
		{Hash: types.HexToHash("1111"), Number: 100},
		{Hash: types.HexToHash("2222"), Number: 101},
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		errs := pm.BroadcastBlockHashes(hashes, nil)
		for _, err := range errs {
			t.Errorf("BroadcastBlockHashes error: %v", err)
		}
	}()

	msg, err := b1.ReadMsg()
	if err != nil {
		t.Fatalf("b1 ReadMsg: %v", err)
	}
	wg.Wait()

	if msg.Code != NewBlockHashesMsg {
		t.Errorf("msg.Code = 0x%02x, want 0x%02x", msg.Code, NewBlockHashesMsg)
	}
	if msg.Size == 0 {
		t.Error("expected non-empty payload")
	}
}

func TestPeerManager_Concurrency(t *testing.T) {
	pm := NewPeerManager()
	const n = 50

	// Create pipes for each peer.
	pipes := make([]*MsgPipeEnd, n*2)
	for i := 0; i < n; i++ {
		a, b := MsgPipe()
		pipes[i*2] = a
		pipes[i*2+1] = b
	}
	defer func() {
		for _, p := range pipes {
			p.Close()
		}
	}()

	var wg sync.WaitGroup

	// Concurrent adds.
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			p := NewPeer(
				"peer"+string(rune('A'+i)),
				"10.0.0.1:30303",
				nil,
			)
			p.SetHead(types.Hash{}, big.NewInt(int64(i)))
			pm.AddPeer(p, pipes[i*2])
		}(i)
	}
	wg.Wait()

	if pm.Len() != n {
		t.Errorf("Len = %d, want %d", pm.Len(), n)
	}

	// Concurrent reads.
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			pm.BestPeer()
			pm.Len()
			pm.Peers()
		}()
	}
	wg.Wait()
}
