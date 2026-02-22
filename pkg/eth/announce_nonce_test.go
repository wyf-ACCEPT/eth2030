package eth

import (
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

func TestAnnounceNonceMsg_Validate(t *testing.T) {
	// Valid message with matching lengths.
	msg := &AnnounceNonceMsg{
		Types:   []byte{0x02, 0x03},
		Sizes:   []uint32{100, 200},
		Hashes:  []types.Hash{{1}, {2}},
		Sources: []types.Address{{0xaa}, {0xbb}},
		Nonces:  []uint64{0, 1},
	}
	if err := msg.Validate(); err != nil {
		t.Fatalf("valid message rejected: %v", err)
	}

	// Mismatched lengths.
	bad := &AnnounceNonceMsg{
		Types:   []byte{0x02},
		Sizes:   []uint32{100, 200},
		Hashes:  []types.Hash{{1}},
		Sources: []types.Address{{0xaa}},
		Nonces:  []uint64{0},
	}
	if err := bad.Validate(); err != ErrAnnounceLengthMismatch {
		t.Fatalf("expected ErrAnnounceLengthMismatch, got %v", err)
	}

	// Empty message is valid.
	empty := &AnnounceNonceMsg{}
	if err := empty.Validate(); err != nil {
		t.Fatalf("empty message rejected: %v", err)
	}
}

func TestAnnounceNonceMsg_Validate_TooMany(t *testing.T) {
	n := MaxAnnouncements + 1
	msg := &AnnounceNonceMsg{
		Types:   make([]byte, n),
		Sizes:   make([]uint32, n),
		Hashes:  make([]types.Hash, n),
		Sources: make([]types.Address, n),
		Nonces:  make([]uint64, n),
	}
	if err := msg.Validate(); err != ErrTooManyAnnouncements {
		t.Fatalf("expected ErrTooManyAnnouncements, got %v", err)
	}
}

func TestEncodeDecodeAnnounceNonce(t *testing.T) {
	original := &AnnounceNonceMsg{
		Types:   []byte{0x02, 0x03},
		Sizes:   []uint32{150, 131072},
		Hashes:  []types.Hash{{1, 2, 3}, {4, 5, 6}},
		Sources: []types.Address{{0xaa, 0xbb}, {0xcc, 0xdd}},
		Nonces:  []uint64{42, 100},
	}

	encoded, err := EncodeAnnounceNonce(original)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	if len(encoded) == 0 {
		t.Fatal("encoded result is empty")
	}

	decoded, err := DecodeAnnounceNonce(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	// Verify all fields match.
	if len(decoded.Types) != len(original.Types) {
		t.Fatalf("types length mismatch: got %d, want %d", len(decoded.Types), len(original.Types))
	}
	for i := range original.Types {
		if decoded.Types[i] != original.Types[i] {
			t.Errorf("types[%d]: got %d, want %d", i, decoded.Types[i], original.Types[i])
		}
	}

	if len(decoded.Sizes) != len(original.Sizes) {
		t.Fatalf("sizes length mismatch: got %d, want %d", len(decoded.Sizes), len(original.Sizes))
	}
	for i := range original.Sizes {
		if decoded.Sizes[i] != original.Sizes[i] {
			t.Errorf("sizes[%d]: got %d, want %d", i, decoded.Sizes[i], original.Sizes[i])
		}
	}

	if len(decoded.Hashes) != len(original.Hashes) {
		t.Fatalf("hashes length mismatch: got %d, want %d", len(decoded.Hashes), len(original.Hashes))
	}
	for i := range original.Hashes {
		if decoded.Hashes[i] != original.Hashes[i] {
			t.Errorf("hashes[%d]: got %s, want %s", i, decoded.Hashes[i].Hex(), original.Hashes[i].Hex())
		}
	}

	if len(decoded.Sources) != len(original.Sources) {
		t.Fatalf("sources length mismatch: got %d, want %d", len(decoded.Sources), len(original.Sources))
	}
	for i := range original.Sources {
		if decoded.Sources[i] != original.Sources[i] {
			t.Errorf("sources[%d] mismatch", i)
		}
	}

	if len(decoded.Nonces) != len(original.Nonces) {
		t.Fatalf("nonces length mismatch: got %d, want %d", len(decoded.Nonces), len(original.Nonces))
	}
	for i := range original.Nonces {
		if decoded.Nonces[i] != original.Nonces[i] {
			t.Errorf("nonces[%d]: got %d, want %d", i, decoded.Nonces[i], original.Nonces[i])
		}
	}
}

func TestEncodeDecodeAnnounceNonce_Empty(t *testing.T) {
	original := &AnnounceNonceMsg{
		Types:   []byte{},
		Sizes:   []uint32{},
		Hashes:  []types.Hash{},
		Sources: []types.Address{},
		Nonces:  []uint64{},
	}

	encoded, err := EncodeAnnounceNonce(original)
	if err != nil {
		t.Fatalf("encode empty failed: %v", err)
	}

	decoded, err := DecodeAnnounceNonce(encoded)
	if err != nil {
		t.Fatalf("decode empty failed: %v", err)
	}

	if len(decoded.Hashes) != 0 {
		t.Fatalf("expected empty hashes, got %d", len(decoded.Hashes))
	}
}

func TestEncodeDecodeAnnounceNonce_SingleEntry(t *testing.T) {
	hash := types.Hash{0xde, 0xad, 0xbe, 0xef}
	addr := types.Address{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a,
		0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14}

	original := &AnnounceNonceMsg{
		Types:   []byte{0x02},
		Sizes:   []uint32{256},
		Hashes:  []types.Hash{hash},
		Sources: []types.Address{addr},
		Nonces:  []uint64{7},
	}

	encoded, err := EncodeAnnounceNonce(original)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	decoded, err := DecodeAnnounceNonce(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if decoded.Hashes[0] != hash {
		t.Errorf("hash mismatch: got %s, want %s", decoded.Hashes[0].Hex(), hash.Hex())
	}
	if decoded.Sources[0] != addr {
		t.Errorf("source address mismatch")
	}
	if decoded.Nonces[0] != 7 {
		t.Errorf("nonce mismatch: got %d, want 7", decoded.Nonces[0])
	}
}

func TestEncodeAnnounceNonce_InvalidMessage(t *testing.T) {
	// Length mismatch should fail encoding.
	bad := &AnnounceNonceMsg{
		Types:   []byte{0x02},
		Sizes:   []uint32{100},
		Hashes:  []types.Hash{{1}, {2}}, // 2 hashes but 1 type
		Sources: []types.Address{{0xaa}},
		Nonces:  []uint64{0},
	}
	_, err := EncodeAnnounceNonce(bad)
	if err != ErrAnnounceLengthMismatch {
		t.Fatalf("expected ErrAnnounceLengthMismatch, got %v", err)
	}
}

func TestDecodeAnnounceNonce_InvalidData(t *testing.T) {
	// Random garbage should fail to decode.
	_, err := DecodeAnnounceNonce([]byte{0xff, 0x00, 0x01})
	if err == nil {
		t.Fatal("expected error decoding garbage data")
	}
}

// --- NonceTracker tests ---

func TestNonceTracker_Announce(t *testing.T) {
	nt := NewNonceTracker()
	sender := types.Address{0x01}
	hash1 := types.Hash{0xaa}
	hash2 := types.Hash{0xbb}

	// First announcement should be new.
	if !nt.Announce(sender, 0, hash1) {
		t.Fatal("first announce should return true")
	}

	// Duplicate announcement should return false.
	if nt.Announce(sender, 0, hash1) {
		t.Fatal("duplicate announce should return false")
	}

	// RBF: different hash for same sender+nonce should return true.
	if !nt.Announce(sender, 0, hash2) {
		t.Fatal("RBF announce should return true")
	}

	// New nonce for same sender should return true.
	if !nt.Announce(sender, 1, hash1) {
		t.Fatal("new nonce announce should return true")
	}

	if nt.Len() != 2 {
		t.Fatalf("expected 2 entries, got %d", nt.Len())
	}
}

func TestNonceTracker_IsKnown(t *testing.T) {
	nt := NewNonceTracker()
	sender := types.Address{0x01}
	hash := types.Hash{0xaa}

	if nt.IsKnown(sender, 0) {
		t.Fatal("should not be known before announcement")
	}

	nt.Announce(sender, 0, hash)

	if !nt.IsKnown(sender, 0) {
		t.Fatal("should be known after announcement")
	}

	if nt.IsKnown(sender, 1) {
		t.Fatal("unknown nonce should not be known")
	}

	other := types.Address{0x02}
	if nt.IsKnown(other, 0) {
		t.Fatal("unknown sender should not be known")
	}
}

func TestNonceTracker_GetPending(t *testing.T) {
	nt := NewNonceTracker()
	sender := types.Address{0x01}
	hash0 := types.Hash{0xaa}
	hash1 := types.Hash{0xbb}

	nt.Announce(sender, 0, hash0)
	nt.Announce(sender, 1, hash1)

	pending := nt.GetPending(sender)
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(pending))
	}
	if pending[0] != hash0 {
		t.Errorf("pending[0]: got %s, want %s", pending[0].Hex(), hash0.Hex())
	}
	if pending[1] != hash1 {
		t.Errorf("pending[1]: got %s, want %s", pending[1].Hex(), hash1.Hex())
	}

	// Unknown sender returns nil.
	unknown := types.Address{0xff}
	if nt.GetPending(unknown) != nil {
		t.Fatal("expected nil for unknown sender")
	}
}

func TestNonceTracker_Remove(t *testing.T) {
	nt := NewNonceTracker()
	sender := types.Address{0x01}
	hash := types.Hash{0xaa}

	nt.Announce(sender, 0, hash)
	nt.Announce(sender, 1, hash)

	nt.Remove(sender, 0)

	if nt.IsKnown(sender, 0) {
		t.Fatal("removed entry should not be known")
	}
	if !nt.IsKnown(sender, 1) {
		t.Fatal("non-removed entry should still be known")
	}
	if nt.Len() != 1 {
		t.Fatalf("expected 1 entry, got %d", nt.Len())
	}

	// Remove last entry for sender should clean up sender map.
	nt.Remove(sender, 1)
	if nt.Len() != 0 {
		t.Fatalf("expected 0 entries, got %d", nt.Len())
	}

	// Remove on unknown sender/nonce should not panic.
	nt.Remove(types.Address{0xff}, 99)
}

func TestNonceTracker_ExpireOld(t *testing.T) {
	nt := NewNonceTracker()
	sender := types.Address{0x01}
	hash := types.Hash{0xaa}

	// Directly inject an old entry by manipulating the tracker.
	nt.mu.Lock()
	nonces := make(map[uint64]nonceEntry)
	nonces[0] = nonceEntry{hash: hash, at: time.Now().Add(-10 * time.Minute)}
	nonces[1] = nonceEntry{hash: hash, at: time.Now()} // recent
	nt.known[sender] = nonces
	nt.mu.Unlock()

	expired := nt.ExpireOld()
	if expired != 1 {
		t.Fatalf("expected 1 expired, got %d", expired)
	}
	if nt.IsKnown(sender, 0) {
		t.Fatal("old entry should have been expired")
	}
	if !nt.IsKnown(sender, 1) {
		t.Fatal("recent entry should still be known")
	}
}

func TestNonceTracker_Len(t *testing.T) {
	nt := NewNonceTracker()
	if nt.Len() != 0 {
		t.Fatalf("empty tracker should have length 0, got %d", nt.Len())
	}

	s1 := types.Address{0x01}
	s2 := types.Address{0x02}

	nt.Announce(s1, 0, types.Hash{0x01})
	nt.Announce(s1, 1, types.Hash{0x02})
	nt.Announce(s2, 0, types.Hash{0x03})

	if nt.Len() != 3 {
		t.Fatalf("expected 3 entries, got %d", nt.Len())
	}
}

func TestNonceTracker_MultipleSenders(t *testing.T) {
	nt := NewNonceTracker()

	senders := make([]types.Address, 5)
	for i := range senders {
		senders[i] = types.Address{byte(i + 1)}
	}

	// Each sender announces 3 nonces.
	for _, s := range senders {
		for n := uint64(0); n < 3; n++ {
			h := types.Hash{s[0], byte(n)}
			nt.Announce(s, n, h)
		}
	}

	if nt.Len() != 15 {
		t.Fatalf("expected 15 entries, got %d", nt.Len())
	}

	// Verify each sender's pending.
	for _, s := range senders {
		pending := nt.GetPending(s)
		if len(pending) != 3 {
			t.Fatalf("sender %x: expected 3 pending, got %d", s[0], len(pending))
		}
	}
}

func TestEncodeDecodeAnnounceNonce_LargeNonce(t *testing.T) {
	// Test with large nonce values near uint64 max.
	original := &AnnounceNonceMsg{
		Types:   []byte{0x02},
		Sizes:   []uint32{500},
		Hashes:  []types.Hash{{0xff}},
		Sources: []types.Address{{0x01}},
		Nonces:  []uint64{^uint64(0)}, // max uint64
	}

	encoded, err := EncodeAnnounceNonce(original)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	decoded, err := DecodeAnnounceNonce(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if decoded.Nonces[0] != ^uint64(0) {
		t.Errorf("nonce mismatch: got %d, want %d", decoded.Nonces[0], ^uint64(0))
	}
}

func TestNonceTracker_RBFTracking(t *testing.T) {
	nt := NewNonceTracker()
	sender := types.Address{0x01}

	// Announce initial tx.
	h1 := types.Hash{0x01}
	nt.Announce(sender, 5, h1)

	// RBF: announce replacement with higher fee (different hash).
	h2 := types.Hash{0x02}
	if !nt.Announce(sender, 5, h2) {
		t.Fatal("RBF should return true for new hash")
	}

	// The pending entry should now point to h2.
	pending := nt.GetPending(sender)
	if pending[5] != h2 {
		t.Errorf("after RBF, expected hash %s, got %s", h2.Hex(), pending[5].Hex())
	}

	// Total count should still be 1 (same sender+nonce slot).
	if nt.Len() != 1 {
		t.Fatalf("expected 1 entry after RBF, got %d", nt.Len())
	}
}
