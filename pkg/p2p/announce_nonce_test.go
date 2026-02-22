package p2p

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

func makeHash(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	return h
}

func TestNonceAnnouncer_AnnounceAndValidate(t *testing.T) {
	na := NewNonceAnnouncer()

	peerID := "peer1"
	hash := makeHash(1)
	nonce := uint64(42)

	err := na.AnnounceNonce(peerID, hash, nonce)
	if err != nil {
		t.Fatalf("announce error: %v", err)
	}

	if !na.ValidateNonce(peerID, hash, nonce) {
		t.Fatal("valid nonce should pass validation")
	}

	// Wrong nonce should fail.
	if na.ValidateNonce(peerID, hash, 999) {
		t.Fatal("wrong nonce should fail validation")
	}

	// Unknown peer should fail.
	if na.ValidateNonce("unknown", hash, nonce) {
		t.Fatal("unknown peer should fail validation")
	}

	// Unknown hash should fail.
	if na.ValidateNonce(peerID, makeHash(99), nonce) {
		t.Fatal("unknown hash should fail validation")
	}
}

func TestNonceAnnouncer_EmptyPeerID(t *testing.T) {
	na := NewNonceAnnouncer()

	err := na.AnnounceNonce("", makeHash(1), 1)
	if !errors.Is(err, ErrNonceEmpty) {
		t.Fatalf("expected ErrNonceEmpty, got: %v", err)
	}
}

func TestNonceAnnouncer_ZeroBlockHash(t *testing.T) {
	na := NewNonceAnnouncer()

	err := na.AnnounceNonce("peer1", types.Hash{}, 1)
	if !errors.Is(err, ErrNonceZeroHash) {
		t.Fatalf("expected ErrNonceZeroHash, got: %v", err)
	}
}

func TestNonceAnnouncer_DuplicateAnnouncement(t *testing.T) {
	na := NewNonceAnnouncer()

	peerID := "peer1"
	hash := makeHash(1)

	err := na.AnnounceNonce(peerID, hash, 42)
	if err != nil {
		t.Fatalf("first announce error: %v", err)
	}

	// Same peer, same hash: duplicate (updates in place).
	err = na.AnnounceNonce(peerID, hash, 42)
	if !errors.Is(err, ErrNonceDuplicate) {
		t.Fatalf("expected ErrNonceDuplicate, got: %v", err)
	}
}

func TestNonceAnnouncer_MultiplePeers(t *testing.T) {
	na := NewNonceAnnouncer()

	hash := makeHash(1)

	_ = na.AnnounceNonce("peer1", hash, 10)
	_ = na.AnnounceNonce("peer2", hash, 20)

	if !na.ValidateNonce("peer1", hash, 10) {
		t.Fatal("peer1 nonce should be valid")
	}
	if !na.ValidateNonce("peer2", hash, 20) {
		t.Fatal("peer2 nonce should be valid")
	}

	if na.PeerCount() != 2 {
		t.Fatalf("expected 2 peers, got %d", na.PeerCount())
	}
}

func TestNonceAnnouncer_MultipleHashes(t *testing.T) {
	na := NewNonceAnnouncer()

	peerID := "peer1"
	hash1 := makeHash(1)
	hash2 := makeHash(2)

	_ = na.AnnounceNonce(peerID, hash1, 100)
	_ = na.AnnounceNonce(peerID, hash2, 200)

	if !na.ValidateNonce(peerID, hash1, 100) {
		t.Fatal("hash1 nonce should be valid")
	}
	if !na.ValidateNonce(peerID, hash2, 200) {
		t.Fatal("hash2 nonce should be valid")
	}
}

func TestNonceAnnouncer_GetPeerNonces(t *testing.T) {
	na := NewNonceAnnouncer()

	peerID := "peer1"
	_ = na.AnnounceNonce(peerID, makeHash(1), 10)
	_ = na.AnnounceNonce(peerID, makeHash(2), 20)
	_ = na.AnnounceNonce(peerID, makeHash(3), 30)

	records := na.GetPeerNonces(peerID)
	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}

	// Unknown peer returns nil.
	records = na.GetPeerNonces("unknown")
	if records != nil {
		t.Fatal("unknown peer should return nil")
	}
}

func TestNonceAnnouncer_PruneStale(t *testing.T) {
	// Use a short TTL for testing.
	na := NewNonceAnnouncerWithConfig(100, 50*time.Millisecond, 100)

	peerID := "peer1"
	_ = na.AnnounceNonce(peerID, makeHash(1), 10)
	_ = na.AnnounceNonce(peerID, makeHash(2), 20)

	// Wait for entries to become stale.
	time.Sleep(100 * time.Millisecond)

	pruned := na.PruneStale(50 * time.Millisecond)
	if pruned != 2 {
		t.Fatalf("expected 2 pruned, got %d", pruned)
	}

	if na.RecordCount() != 0 {
		t.Fatalf("expected 0 records after prune, got %d", na.RecordCount())
	}

	// Empty peer caches should be removed.
	if na.PeerCount() != 0 {
		t.Fatalf("expected 0 peers after full prune, got %d", na.PeerCount())
	}
}

func TestNonceAnnouncer_TTLExpiry(t *testing.T) {
	na := NewNonceAnnouncerWithConfig(100, 50*time.Millisecond, 100)

	peerID := "peer1"
	hash := makeHash(1)
	_ = na.AnnounceNonce(peerID, hash, 42)

	// Should validate immediately.
	if !na.ValidateNonce(peerID, hash, 42) {
		t.Fatal("fresh nonce should validate")
	}

	// Wait for TTL to expire.
	time.Sleep(100 * time.Millisecond)

	// Should no longer validate (expired).
	if na.ValidateNonce(peerID, hash, 42) {
		t.Fatal("expired nonce should not validate")
	}
}

func TestNonceAnnouncer_LRUEviction(t *testing.T) {
	na := NewNonceAnnouncerWithConfig(3, 5*time.Minute, 100)

	peerID := "peer1"

	// Fill the cache to capacity.
	_ = na.AnnounceNonce(peerID, makeHash(1), 10)
	_ = na.AnnounceNonce(peerID, makeHash(2), 20)
	_ = na.AnnounceNonce(peerID, makeHash(3), 30)

	// Add one more, should evict the oldest (hash 1).
	_ = na.AnnounceNonce(peerID, makeHash(4), 40)

	// Hash 1 should be evicted.
	if na.HasNonce(peerID, makeHash(1)) {
		t.Fatal("hash 1 should have been evicted")
	}

	// Hash 4 should exist.
	if !na.HasNonce(peerID, makeHash(4)) {
		t.Fatal("hash 4 should exist")
	}
}

func TestNonceAnnouncer_MaxPeers(t *testing.T) {
	na := NewNonceAnnouncerWithConfig(100, 5*time.Minute, 2)

	_ = na.AnnounceNonce("peer1", makeHash(1), 1)
	_ = na.AnnounceNonce("peer2", makeHash(2), 2)

	err := na.AnnounceNonce("peer3", makeHash(3), 3)
	if !errors.Is(err, ErrNonceTooMany) {
		t.Fatalf("expected ErrNonceTooMany, got: %v", err)
	}
}

func TestNonceAnnouncer_RemovePeer(t *testing.T) {
	na := NewNonceAnnouncer()

	_ = na.AnnounceNonce("peer1", makeHash(1), 10)
	_ = na.AnnounceNonce("peer2", makeHash(2), 20)

	na.RemovePeer("peer1")

	if na.PeerCount() != 1 {
		t.Fatalf("expected 1 peer after removal, got %d", na.PeerCount())
	}
	if na.HasNonce("peer1", makeHash(1)) {
		t.Fatal("removed peer's nonces should not be found")
	}
}

func TestNonceAnnouncer_RecordCount(t *testing.T) {
	na := NewNonceAnnouncer()

	_ = na.AnnounceNonce("peer1", makeHash(1), 10)
	_ = na.AnnounceNonce("peer1", makeHash(2), 20)
	_ = na.AnnounceNonce("peer2", makeHash(3), 30)

	if na.RecordCount() != 3 {
		t.Fatalf("expected 3 total records, got %d", na.RecordCount())
	}
}

func TestNonceAnnouncer_HasNonce(t *testing.T) {
	na := NewNonceAnnouncer()

	peerID := "peer1"
	hash := makeHash(1)

	if na.HasNonce(peerID, hash) {
		t.Fatal("should not have nonce before announcement")
	}

	_ = na.AnnounceNonce(peerID, hash, 42)

	if !na.HasNonce(peerID, hash) {
		t.Fatal("should have nonce after announcement")
	}
}

func TestNonceAnnouncer_ConcurrentAccess(t *testing.T) {
	na := NewNonceAnnouncer()

	var wg sync.WaitGroup

	// Concurrent announcements from different peers.
	// Start from 1 to avoid zero hash (which is rejected).
	for i := byte(1); i <= 50; i++ {
		wg.Add(1)
		go func(b byte) {
			defer wg.Done()
			peerID := string([]byte{'p', b})
			_ = na.AnnounceNonce(peerID, makeHash(b), uint64(b))
		}(i)
	}
	wg.Wait()

	if na.PeerCount() != 50 {
		t.Fatalf("expected 50 peers, got %d", na.PeerCount())
	}

	// Concurrent validations.
	for i := byte(1); i <= 50; i++ {
		wg.Add(1)
		go func(b byte) {
			defer wg.Done()
			peerID := string([]byte{'p', b})
			na.ValidateNonce(peerID, makeHash(b), uint64(b))
		}(i)
	}
	wg.Wait()
}

func TestNonceAnnouncer_DefaultConfig(t *testing.T) {
	na := NewNonceAnnouncer()
	if na.cacheSize != DefaultNonceCacheSize {
		t.Fatalf("expected cache size %d, got %d", DefaultNonceCacheSize, na.cacheSize)
	}
	if na.ttl != DefaultNonceTTL {
		t.Fatalf("expected TTL %v, got %v", DefaultNonceTTL, na.ttl)
	}
	if na.maxPeers != DefaultMaxPeers {
		t.Fatalf("expected max peers %d, got %d", DefaultMaxPeers, na.maxPeers)
	}
}

func TestNonceCache_LRUOrder(t *testing.T) {
	cache := newNonceCache(3, 5*time.Minute)

	// Insert 3 entries.
	cache.put(NonceRecord{PeerID: "p", BlockHash: makeHash(1), Nonce: 1, Timestamp: time.Now()})
	cache.put(NonceRecord{PeerID: "p", BlockHash: makeHash(2), Nonce: 2, Timestamp: time.Now()})
	cache.put(NonceRecord{PeerID: "p", BlockHash: makeHash(3), Nonce: 3, Timestamp: time.Now()})

	// Access hash 1 to move it to front.
	cache.get(makeHash(1))

	// Insert hash 4: should evict hash 2 (least recently used).
	cache.put(NonceRecord{PeerID: "p", BlockHash: makeHash(4), Nonce: 4, Timestamp: time.Now()})

	if cache.get(makeHash(2)) != nil {
		t.Fatal("hash 2 should have been evicted (LRU)")
	}
	if cache.get(makeHash(1)) == nil {
		t.Fatal("hash 1 should still be present (recently accessed)")
	}
}

func TestNonceCache_PruneStale(t *testing.T) {
	cache := newNonceCache(100, 5*time.Minute)

	old := time.Now().Add(-10 * time.Minute)
	recent := time.Now()

	cache.put(NonceRecord{PeerID: "p", BlockHash: makeHash(1), Nonce: 1, Timestamp: old})
	cache.put(NonceRecord{PeerID: "p", BlockHash: makeHash(2), Nonce: 2, Timestamp: recent})

	removed := cache.pruneStale(5 * time.Minute)
	if removed != 1 {
		t.Fatalf("expected 1 pruned, got %d", removed)
	}

	if cache.len() != 1 {
		t.Fatalf("expected 1 remaining, got %d", cache.len())
	}
}

func TestNonceAnnouncerWithConfig_Defaults(t *testing.T) {
	// Zero/negative values should use defaults.
	na := NewNonceAnnouncerWithConfig(0, -1, 0)

	if na.cacheSize != DefaultNonceCacheSize {
		t.Fatalf("expected default cache size, got %d", na.cacheSize)
	}
	if na.ttl != DefaultNonceTTL {
		t.Fatalf("expected default TTL, got %v", na.ttl)
	}
	if na.maxPeers != DefaultMaxPeers {
		t.Fatalf("expected default max peers, got %d", na.maxPeers)
	}
}
