package sync

import (
	"fmt"
	"sync"
	"testing"
)

func makeSidecarV2(slot, index uint64) *BlobSidecarV2 {
	sc := &BlobSidecarV2{
		Index:         index,
		Slot:          slot,
		ProposerIndex: 1,
	}
	// Non-zero KZG commitment and proof.
	sc.KZGCommitment[0] = byte(index + 1)
	sc.KZGCommitment[1] = byte(slot)
	sc.KZGProof[0] = byte(index + 2)
	sc.KZGProof[1] = byte(slot + 1)
	// Put some data in blob.
	sc.Blob[0] = byte(index)
	sc.Blob[1] = byte(slot)
	sc.SignedBlockHeaderRoot[0] = byte(slot)
	return sc
}

func TestNewBlobSyncProtocol(t *testing.T) {
	p := NewBlobSyncProtocol(DefaultBlobSyncProtocolConfig())
	if p == nil {
		t.Fatal("expected non-nil protocol")
	}
	if p.PeerCount() != 0 {
		t.Fatalf("expected 0 peers, got %d", p.PeerCount())
	}
	if p.SlotCount() != 0 {
		t.Fatalf("expected 0 slots, got %d", p.SlotCount())
	}
}

func TestNewBlobSyncProtocolDefaults(t *testing.T) {
	p := NewBlobSyncProtocol(BlobSyncProtocolConfig{})
	if p.config.MaxBlobsPerBlock != MaxBlobsPerBlockV2 {
		t.Fatalf("expected default max blobs %d, got %d",
			MaxBlobsPerBlockV2, p.config.MaxBlobsPerBlock)
	}
}

func TestRegisterAndRemovePeer(t *testing.T) {
	p := NewBlobSyncProtocol(DefaultBlobSyncProtocolConfig())
	p.RegisterPeer("peer1")
	p.RegisterPeer("peer2")
	if p.PeerCount() != 2 {
		t.Fatalf("expected 2 peers, got %d", p.PeerCount())
	}

	p.RemovePeer("peer1")
	if p.PeerCount() != 1 {
		t.Fatalf("expected 1 peer after removal, got %d", p.PeerCount())
	}
}

func TestRegisterPeerIdempotent(t *testing.T) {
	p := NewBlobSyncProtocol(DefaultBlobSyncProtocolConfig())
	p.RegisterPeer("peer1")
	p.RegisterPeer("peer1") // should not panic or duplicate
	if p.PeerCount() != 1 {
		t.Fatalf("expected 1 peer, got %d", p.PeerCount())
	}
}

func TestValidateSidecar(t *testing.T) {
	p := NewBlobSyncProtocol(DefaultBlobSyncProtocolConfig())

	// Nil sidecar.
	if err := p.ValidateSidecar(nil); err != ErrBlobProtoNilSidecar {
		t.Fatalf("expected nil sidecar error, got %v", err)
	}

	// Index out of range.
	sc := makeSidecarV2(1, MaxBlobsPerBlockV2)
	if err := p.ValidateSidecar(sc); err == nil {
		t.Fatal("expected index out of range error")
	}

	// Zero commitment.
	sc = makeSidecarV2(1, 0)
	sc.KZGCommitment = [48]byte{}
	if err := p.ValidateSidecar(sc); err != ErrBlobProtoZeroCommitment {
		t.Fatalf("expected zero commitment error, got %v", err)
	}

	// Zero proof.
	sc = makeSidecarV2(1, 0)
	sc.KZGProof = [48]byte{}
	if err := p.ValidateSidecar(sc); err != ErrBlobProtoZeroProof {
		t.Fatalf("expected zero proof error, got %v", err)
	}

	// Valid sidecar.
	sc = makeSidecarV2(1, 0)
	if err := p.ValidateSidecar(sc); err != nil {
		t.Fatalf("expected valid sidecar, got error: %v", err)
	}
}

func TestStoreSidecar(t *testing.T) {
	p := NewBlobSyncProtocol(DefaultBlobSyncProtocolConfig())

	sc := makeSidecarV2(100, 0)
	if err := p.StoreSidecar(sc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sidecars := p.GetSidecarsForSlot(100)
	if len(sidecars) != 1 {
		t.Fatalf("expected 1 sidecar, got %d", len(sidecars))
	}
	if sidecars[0].Index != 0 {
		t.Fatalf("expected index 0, got %d", sidecars[0].Index)
	}
}

func TestStoreSidecarNil(t *testing.T) {
	p := NewBlobSyncProtocol(DefaultBlobSyncProtocolConfig())
	if err := p.StoreSidecar(nil); err != ErrBlobProtoNilSidecar {
		t.Fatalf("expected nil sidecar error, got %v", err)
	}
}

func TestRequestBlobSidecars(t *testing.T) {
	p := NewBlobSyncProtocol(DefaultBlobSyncProtocolConfig())
	p.RegisterPeer("peer1")

	// Store some sidecars.
	for i := uint64(0); i < 3; i++ {
		p.StoreSidecar(makeSidecarV2(50, i))
	}

	// Request specific indices.
	result, err := p.RequestBlobSidecars(50, []uint64{0, 2}, "peer1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 sidecars, got %d", len(result))
	}
}

func TestRequestBlobSidecarsUnknownPeer(t *testing.T) {
	p := NewBlobSyncProtocol(DefaultBlobSyncProtocolConfig())
	_, err := p.RequestBlobSidecars(50, []uint64{0}, "unknown")
	if err != ErrBlobProtoNoPeers {
		t.Fatalf("expected no peers error, got %v", err)
	}
}

func TestRequestBlobSidecarsInvalidIndex(t *testing.T) {
	p := NewBlobSyncProtocol(DefaultBlobSyncProtocolConfig())
	p.RegisterPeer("peer1")
	_, err := p.RequestBlobSidecars(50, []uint64{MaxBlobsPerBlockV2}, "peer1")
	if err == nil {
		t.Fatal("expected index out of range error")
	}
}

func TestRequestBlobRange(t *testing.T) {
	p := NewBlobSyncProtocol(DefaultBlobSyncProtocolConfig())

	// Store sidecars across multiple slots.
	for slot := uint64(10); slot <= 15; slot++ {
		for i := uint64(0); i < 3; i++ {
			p.StoreSidecar(makeSidecarV2(slot, i))
		}
	}

	result, err := p.RequestBlobRange(10, 15)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 18 { // 6 slots * 3 sidecars
		t.Fatalf("expected 18 sidecars, got %d", len(result))
	}
}

func TestRequestBlobRangeInvalid(t *testing.T) {
	p := NewBlobSyncProtocol(DefaultBlobSyncProtocolConfig())

	_, err := p.RequestBlobRange(100, 50)
	if err != ErrBlobProtoInvalidRange {
		t.Fatalf("expected invalid range error, got %v", err)
	}
}

func TestRequestBlobRangeTooLarge(t *testing.T) {
	p := NewBlobSyncProtocol(DefaultBlobSyncProtocolConfig())

	_, err := p.RequestBlobRange(0, MaxRequestBlocksDeneb+1)
	if err == nil {
		t.Fatal("expected range too large error")
	}
}

func TestProcessSidecarResponse(t *testing.T) {
	p := NewBlobSyncProtocol(DefaultBlobSyncProtocolConfig())
	p.RegisterPeer("peer1")

	resp := &BlobSidecarResponse{
		Sidecars: []*BlobSidecarV2{
			makeSidecarV2(200, 0),
			makeSidecarV2(200, 1),
		},
		PeerID: "peer1",
		Slot:   200,
	}

	accepted, err := p.ProcessSidecarResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if accepted != 2 {
		t.Fatalf("expected 2 accepted, got %d", accepted)
	}

	// Peer score should increase.
	score, ok := p.GetPeerScore("peer1")
	if !ok {
		t.Fatal("peer not found")
	}
	if score <= 0 {
		t.Fatalf("expected positive score, got %d", score)
	}
}

func TestProcessSidecarResponseSlotMismatch(t *testing.T) {
	p := NewBlobSyncProtocol(DefaultBlobSyncProtocolConfig())
	p.RegisterPeer("peer1")

	// Sidecar slot doesn't match response slot.
	resp := &BlobSidecarResponse{
		Sidecars: []*BlobSidecarV2{
			makeSidecarV2(999, 0), // slot mismatch
		},
		PeerID: "peer1",
		Slot:   200,
	}

	accepted, err := p.ProcessSidecarResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if accepted != 0 {
		t.Fatalf("expected 0 accepted due to mismatch, got %d", accepted)
	}

	// Peer score should decrease.
	score, _ := p.GetPeerScore("peer1")
	if score >= 0 {
		t.Fatalf("expected negative score after mismatch, got %d", score)
	}
}

func TestProcessSidecarResponseEmpty(t *testing.T) {
	p := NewBlobSyncProtocol(DefaultBlobSyncProtocolConfig())
	p.RegisterPeer("peer1")

	resp := &BlobSidecarResponse{
		Sidecars: nil,
		PeerID:   "peer1",
		Slot:     100,
	}

	accepted, err := p.ProcessSidecarResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if accepted != 0 {
		t.Fatalf("expected 0 accepted, got %d", accepted)
	}

	score, _ := p.GetPeerScore("peer1")
	if score >= 0 {
		t.Fatalf("expected negative score for empty response, got %d", score)
	}
}

func TestIsSlotFullyValidated(t *testing.T) {
	p := NewBlobSyncProtocol(DefaultBlobSyncProtocolConfig())

	// Not validated yet.
	if p.IsSlotFullyValidated(100, 3) {
		t.Fatal("slot should not be validated before storing sidecars")
	}

	// Store 3 sidecars.
	for i := uint64(0); i < 3; i++ {
		p.StoreSidecar(makeSidecarV2(100, i))
	}

	if !p.IsSlotFullyValidated(100, 3) {
		t.Fatal("slot should be fully validated with 3 sidecars")
	}
	if p.IsSlotFullyValidated(100, 4) {
		t.Fatal("slot should not be fully validated if expecting 4")
	}
}

func TestPeerScoreBounds(t *testing.T) {
	p := NewBlobSyncProtocol(DefaultBlobSyncProtocolConfig())
	p.RegisterPeer("peer1")

	// Drive score to minimum.
	for i := 0; i < 50; i++ {
		resp := &BlobSidecarResponse{
			Sidecars: []*BlobSidecarV2{makeSidecarV2(999, 0)},
			PeerID:   "peer1",
			Slot:     uint64(i), // mismatched slot
		}
		p.ProcessSidecarResponse(resp)
	}

	score, _ := p.GetPeerScore("peer1")
	if score < defaultMinScore {
		t.Fatalf("score %d should not go below %d", score, defaultMinScore)
	}
}

func TestPeerBanned(t *testing.T) {
	p := NewBlobSyncProtocol(DefaultBlobSyncProtocolConfig())
	p.RegisterPeer("peer1")

	// Set score to minimum.
	p.mu.Lock()
	p.peers["peer1"].score = defaultMinScore
	p.mu.Unlock()

	_, err := p.RequestBlobSidecars(50, []uint64{0}, "peer1")
	if err != ErrBlobProtoPeerBanned {
		t.Fatalf("expected peer banned error, got %v", err)
	}
}

func TestRateLimiting(t *testing.T) {
	config := DefaultBlobSyncProtocolConfig()
	config.MaxRequestsPerWindow = 3
	p := NewBlobSyncProtocol(config)
	p.RegisterPeer("peer1")

	// Make requests up to the limit.
	for i := 0; i < 3; i++ {
		_, err := p.RequestBlobSidecars(uint64(i), nil, "peer1")
		if err != nil {
			t.Fatalf("request %d: unexpected error: %v", i, err)
		}
	}

	// Next request should be rate limited.
	_, err := p.RequestBlobSidecars(99, nil, "peer1")
	if err != ErrBlobProtoRateLimited {
		t.Fatalf("expected rate limited error, got %v", err)
	}
}

func TestGetPeerStats(t *testing.T) {
	p := NewBlobSyncProtocol(DefaultBlobSyncProtocolConfig())
	p.RegisterPeer("peer1")

	// Process a good response.
	resp := &BlobSidecarResponse{
		Sidecars: []*BlobSidecarV2{makeSidecarV2(100, 0)},
		PeerID:   "peer1",
		Slot:     100,
	}
	p.ProcessSidecarResponse(resp)

	good, bad, ok := p.GetPeerStats("peer1")
	if !ok {
		t.Fatal("peer not found")
	}
	if good == 0 {
		t.Fatal("expected nonzero good count")
	}
	if bad != 0 {
		t.Fatalf("expected 0 bad count, got %d", bad)
	}
}

func TestGetPeerStatsUnknownPeer(t *testing.T) {
	p := NewBlobSyncProtocol(DefaultBlobSyncProtocolConfig())
	_, _, ok := p.GetPeerStats("nobody")
	if ok {
		t.Fatal("expected false for unknown peer")
	}
}

func TestResetSlot(t *testing.T) {
	p := NewBlobSyncProtocol(DefaultBlobSyncProtocolConfig())
	p.StoreSidecar(makeSidecarV2(300, 0))
	p.StoreSidecar(makeSidecarV2(300, 1))

	if p.SlotCount() != 1 {
		t.Fatalf("expected 1 slot, got %d", p.SlotCount())
	}

	p.ResetSlot(300)

	if p.SlotCount() != 0 {
		t.Fatalf("expected 0 slots after reset, got %d", p.SlotCount())
	}
	if len(p.GetSidecarsForSlot(300)) != 0 {
		t.Fatal("expected no sidecars after reset")
	}
}

func TestRequestBlobSidecarsAllIndices(t *testing.T) {
	p := NewBlobSyncProtocol(DefaultBlobSyncProtocolConfig())
	p.RegisterPeer("peer1")

	// Store all blob indices for a slot.
	for i := uint64(0); i < MaxBlobsPerBlockV2; i++ {
		p.StoreSidecar(makeSidecarV2(500, i))
	}

	// Request with empty indices should return all.
	result, err := p.RequestBlobSidecars(500, nil, "peer1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != int(MaxBlobsPerBlockV2) {
		t.Fatalf("expected %d sidecars, got %d", MaxBlobsPerBlockV2, len(result))
	}
}

func TestBlobSyncProtocolThreadSafety(t *testing.T) {
	p := NewBlobSyncProtocol(DefaultBlobSyncProtocolConfig())
	for i := 0; i < 5; i++ {
		p.RegisterPeer(fmt.Sprintf("peer%d", i))
	}

	var wg sync.WaitGroup

	// Concurrent stores.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			slot := uint64(n / 3)
			index := uint64(n % 3)
			p.StoreSidecar(makeSidecarV2(slot, index))
		}(i)
	}

	// Concurrent reads.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			p.GetSidecarsForSlot(uint64(n))
			p.IsSlotFullyValidated(uint64(n), 3)
			p.PeerCount()
			p.SlotCount()
		}(i)
	}

	// Concurrent requests.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			peerID := fmt.Sprintf("peer%d", n)
			p.RequestBlobSidecars(uint64(n), []uint64{0}, peerID)
		}(i)
	}

	wg.Wait()
}

// Ensure fmt is used (referenced in TestBlobSyncProtocolThreadSafety).
var _ = fmt.Sprintf
