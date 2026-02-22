package eth

import (
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

func testAddr(id byte) types.Address {
	var a types.Address
	a[0] = 0xAA
	a[19] = id
	return a
}

func validAnnouncement(addr types.Address, nonce uint64) *NonceAnnouncement {
	return &NonceAnnouncement{
		Sender:    addr,
		Nonce:     nonce,
		NextNonce: nonce + 1,
		Signature: []byte{0x01, 0x02, 0x03},
		Timestamp: time.Now(),
	}
}

func TestValidateAnnouncement_Valid(t *testing.T) {
	ann := validAnnouncement(testAddr(1), 10)
	if err := ValidateAnnouncement(ann); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAnnouncement_Nil(t *testing.T) {
	if err := ValidateAnnouncement(nil); err != ErrNonceAnnNil {
		t.Errorf("expected ErrNonceAnnNil, got %v", err)
	}
}

func TestValidateAnnouncement_ZeroAddress(t *testing.T) {
	ann := &NonceAnnouncement{
		Nonce:     5,
		NextNonce: 6,
		Signature: []byte{0x01},
	}
	if err := ValidateAnnouncement(ann); err != ErrNonceAnnZeroAddress {
		t.Errorf("expected ErrNonceAnnZeroAddress, got %v", err)
	}
}

func TestValidateAnnouncement_EmptySignature(t *testing.T) {
	ann := &NonceAnnouncement{
		Sender:    testAddr(1),
		Nonce:     5,
		NextNonce: 6,
	}
	if err := ValidateAnnouncement(ann); err != ErrNonceAnnEmptySignature {
		t.Errorf("expected ErrNonceAnnEmptySignature, got %v", err)
	}
}

func TestValidateAnnouncement_NextBelowCurrent(t *testing.T) {
	ann := &NonceAnnouncement{
		Sender:    testAddr(1),
		Nonce:     10,
		NextNonce: 5,
		Signature: []byte{0x01},
	}
	if err := ValidateAnnouncement(ann); err != ErrNonceAnnNextBelowCurr {
		t.Errorf("expected ErrNonceAnnNextBelowCurr, got %v", err)
	}
}

func TestValidateAnnouncement_GapTooLarge(t *testing.T) {
	ann := &NonceAnnouncement{
		Sender:    testAddr(1),
		Nonce:     0,
		NextNonce: MaxNonceGap + 1,
		Signature: []byte{0x01},
	}
	if err := ValidateAnnouncement(ann); err != ErrNonceAnnGapTooLarge {
		t.Errorf("expected ErrNonceAnnGapTooLarge, got %v", err)
	}
}

func TestValidateAnnouncement_ExactMaxGap(t *testing.T) {
	ann := &NonceAnnouncement{
		Sender:    testAddr(1),
		Nonce:     0,
		NextNonce: MaxNonceGap,
		Signature: []byte{0x01},
	}
	if err := ValidateAnnouncement(ann); err != nil {
		t.Errorf("exact MaxNonceGap should be valid, got %v", err)
	}
}

func TestNonceAnnouncementPool_AddAndGet(t *testing.T) {
	pool := NewNonceAnnouncementPool()
	addr := testAddr(1)

	// Not found before adding.
	if _, ok := pool.GetLatestNonce(addr); ok {
		t.Fatal("should not find nonce before adding")
	}

	ann := validAnnouncement(addr, 42)
	if err := pool.AddAnnouncement(ann); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nonce, ok := pool.GetLatestNonce(addr)
	if !ok {
		t.Fatal("should find nonce after adding")
	}
	if nonce != 42 {
		t.Errorf("expected nonce 42, got %d", nonce)
	}
}

func TestNonceAnnouncementPool_ReplacesHigherNonce(t *testing.T) {
	pool := NewNonceAnnouncementPool()
	addr := testAddr(1)

	pool.AddAnnouncement(validAnnouncement(addr, 10))
	pool.AddAnnouncement(validAnnouncement(addr, 20))

	nonce, _ := pool.GetLatestNonce(addr)
	if nonce != 20 {
		t.Errorf("expected nonce 20, got %d", nonce)
	}
}

func TestNonceAnnouncementPool_IgnoresLowerNonce(t *testing.T) {
	pool := NewNonceAnnouncementPool()
	addr := testAddr(1)

	pool.AddAnnouncement(validAnnouncement(addr, 20))
	pool.AddAnnouncement(validAnnouncement(addr, 10))

	nonce, _ := pool.GetLatestNonce(addr)
	if nonce != 20 {
		t.Errorf("expected nonce 20 (should ignore lower), got %d", nonce)
	}
}

func TestNonceAnnouncementPool_IgnoresEqualNonce(t *testing.T) {
	pool := NewNonceAnnouncementPool()
	addr := testAddr(1)

	pool.AddAnnouncement(validAnnouncement(addr, 15))
	pool.AddAnnouncement(validAnnouncement(addr, 15))

	if pool.Size() != 1 {
		t.Errorf("expected size 1, got %d", pool.Size())
	}
}

func TestNonceAnnouncementPool_MultipleAddresses(t *testing.T) {
	pool := NewNonceAnnouncementPool()

	for i := byte(1); i <= 5; i++ {
		addr := testAddr(i)
		pool.AddAnnouncement(validAnnouncement(addr, uint64(i*10)))
	}

	if pool.Size() != 5 {
		t.Fatalf("expected 5 addresses, got %d", pool.Size())
	}

	for i := byte(1); i <= 5; i++ {
		nonce, ok := pool.GetLatestNonce(testAddr(i))
		if !ok {
			t.Fatalf("addr %d: not found", i)
		}
		if nonce != uint64(i*10) {
			t.Errorf("addr %d: expected nonce %d, got %d", i, i*10, nonce)
		}
	}
}

func TestNonceAnnouncementPool_AddValidation(t *testing.T) {
	pool := NewNonceAnnouncementPool()

	// Nil should fail.
	if err := pool.AddAnnouncement(nil); err != ErrNonceAnnNil {
		t.Errorf("expected ErrNonceAnnNil, got %v", err)
	}

	// Zero address should fail.
	if err := pool.AddAnnouncement(&NonceAnnouncement{
		Nonce:     5,
		NextNonce: 6,
		Signature: []byte{0x01},
	}); err != ErrNonceAnnZeroAddress {
		t.Errorf("expected ErrNonceAnnZeroAddress, got %v", err)
	}
}

func TestNonceAnnouncementPool_PruneStale(t *testing.T) {
	pool := NewNonceAnnouncementPool()
	addr1 := testAddr(1)
	addr2 := testAddr(2)

	// Add two announcements and manually backdate one.
	pool.AddAnnouncement(validAnnouncement(addr1, 10))
	pool.AddAnnouncement(validAnnouncement(addr2, 20))

	// Manually set addr1's entry timestamp to 10 minutes ago.
	pool.mu.Lock()
	pool.entries[addr1].at = time.Now().Add(-10 * time.Minute)
	pool.mu.Unlock()

	pruned := pool.PruneStale(5 * time.Minute)
	if pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", pruned)
	}

	if _, ok := pool.GetLatestNonce(addr1); ok {
		t.Error("addr1 should have been pruned")
	}
	if _, ok := pool.GetLatestNonce(addr2); !ok {
		t.Error("addr2 should still exist")
	}
}

func TestNonceAnnouncementPool_PruneStale_NothingToPrune(t *testing.T) {
	pool := NewNonceAnnouncementPool()
	pool.AddAnnouncement(validAnnouncement(testAddr(1), 10))

	pruned := pool.PruneStale(1 * time.Hour)
	if pruned != 0 {
		t.Errorf("expected 0 pruned, got %d", pruned)
	}
}

func TestBroadcastNonce(t *testing.T) {
	addr := testAddr(1)
	ann := BroadcastNonce(addr, 42)

	if ann.Sender != addr {
		t.Error("sender address mismatch")
	}
	if ann.Nonce != 42 {
		t.Errorf("expected nonce 42, got %d", ann.Nonce)
	}
	if ann.NextNonce != 43 {
		t.Errorf("expected next nonce 43, got %d", ann.NextNonce)
	}
	if len(ann.Signature) != 32 {
		t.Errorf("expected 32-byte signature, got %d bytes", len(ann.Signature))
	}
	if ann.Timestamp.IsZero() {
		t.Error("timestamp should not be zero")
	}
}

func TestBroadcastNonce_CanBeAddedToPool(t *testing.T) {
	pool := NewNonceAnnouncementPool()
	addr := testAddr(1)

	ann := BroadcastNonce(addr, 100)
	if err := pool.AddAnnouncement(ann); err != nil {
		t.Fatalf("adding broadcast announcement failed: %v", err)
	}

	nonce, ok := pool.GetLatestNonce(addr)
	if !ok {
		t.Fatal("should find broadcast nonce in pool")
	}
	if nonce != 100 {
		t.Errorf("expected nonce 100, got %d", nonce)
	}
}

func TestAnnouncementStats_Empty(t *testing.T) {
	pool := NewNonceAnnouncementPool()
	stats := pool.AnnouncementStats()

	if stats.TotalAddresses != 0 {
		t.Errorf("expected 0 addresses, got %d", stats.TotalAddresses)
	}
	if stats.TotalAnnouncements != 0 {
		t.Errorf("expected 0 announcements, got %d", stats.TotalAnnouncements)
	}
	if !stats.OldestAnnouncement.IsZero() {
		t.Error("oldest should be zero for empty pool")
	}
	if !stats.NewestAnnouncement.IsZero() {
		t.Error("newest should be zero for empty pool")
	}
}

func TestAnnouncementStats_WithEntries(t *testing.T) {
	pool := NewNonceAnnouncementPool()

	// Add entries with controlled timing.
	pool.AddAnnouncement(validAnnouncement(testAddr(1), 10))
	time.Sleep(1 * time.Millisecond) // ensure different timestamps
	pool.AddAnnouncement(validAnnouncement(testAddr(2), 20))
	time.Sleep(1 * time.Millisecond)
	pool.AddAnnouncement(validAnnouncement(testAddr(3), 30))

	stats := pool.AnnouncementStats()

	if stats.TotalAddresses != 3 {
		t.Errorf("expected 3 addresses, got %d", stats.TotalAddresses)
	}
	if stats.TotalAnnouncements != 3 {
		t.Errorf("expected 3 announcements, got %d", stats.TotalAnnouncements)
	}
	if stats.OldestAnnouncement.IsZero() {
		t.Error("oldest should not be zero")
	}
	if stats.NewestAnnouncement.IsZero() {
		t.Error("newest should not be zero")
	}
	if !stats.OldestAnnouncement.Before(stats.NewestAnnouncement) {
		t.Error("oldest should be before newest")
	}
}

func TestNonceAnnouncementPool_Size(t *testing.T) {
	pool := NewNonceAnnouncementPool()
	if pool.Size() != 0 {
		t.Fatalf("empty pool should have size 0, got %d", pool.Size())
	}

	pool.AddAnnouncement(validAnnouncement(testAddr(1), 10))
	pool.AddAnnouncement(validAnnouncement(testAddr(2), 20))

	if pool.Size() != 2 {
		t.Errorf("expected size 2, got %d", pool.Size())
	}

	// Adding to same address should not increase size.
	pool.AddAnnouncement(validAnnouncement(testAddr(1), 15))
	if pool.Size() != 2 {
		t.Errorf("expected size 2 after update, got %d", pool.Size())
	}
}
