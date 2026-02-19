package das

import (
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

func makeCommitment(data []byte) types.Hash {
	return crypto.Keccak256Hash(data)
}

func TestAnnounceBlob(t *testing.T) {
	fc := NewForwardCaster(DefaultForwardCastConfig())
	fc.SetCurrentSlot(100)

	commitment := makeCommitment([]byte("blob data"))
	if err := fc.AnnounceBlob(110, 0, commitment); err != nil {
		t.Fatalf("AnnounceBlob: %v", err)
	}
	anns := fc.GetAnnouncements(110)
	if len(anns) != 1 {
		t.Fatalf("expected 1 announcement, got %d", len(anns))
	}
	if anns[0].Slot != 110 || anns[0].BlobIndex != 0 {
		t.Errorf("Slot=%d BlobIndex=%d, want 110/0", anns[0].Slot, anns[0].BlobIndex)
	}
	if anns[0].Commitment != commitment {
		t.Error("Commitment mismatch")
	}
	if anns[0].Expiry != 110+16 {
		t.Errorf("Expiry = %d, want %d", anns[0].Expiry, 110+16)
	}
}

func TestAnnounceBlobSlotValidation(t *testing.T) {
	fc := NewForwardCaster(DefaultForwardCastConfig())
	fc.SetCurrentSlot(100)
	c := makeCommitment([]byte("data"))

	// Past slot.
	if err := fc.AnnounceBlob(50, 0, c); err == nil {
		t.Fatal("expected error for past slot")
	}
	// Current slot.
	if err := fc.AnnounceBlob(100, 0, c); err == nil {
		t.Fatal("expected error for current slot")
	}
	// Too far ahead (MaxLeadSlots=64, max is 164).
	if err := fc.AnnounceBlob(165, 0, c); err == nil {
		t.Fatal("expected error for slot too far ahead")
	}
	// Boundary slot should be fine.
	if err := fc.AnnounceBlob(164, 0, c); err != nil {
		t.Fatalf("slot 164 should be valid: %v", err)
	}
}

func TestAnnounceBlobSlotFull(t *testing.T) {
	cfg := ForwardCastConfig{
		MaxLeadSlots: 64, MaxAnnouncementsPerSlot: 2,
		ExpirySlots: 16, MaxBlobDataSize: 131072,
	}
	fc := NewForwardCaster(cfg)
	fc.SetCurrentSlot(100)

	fc.AnnounceBlob(110, 0, makeCommitment([]byte("b1")))
	fc.AnnounceBlob(110, 1, makeCommitment([]byte("b2")))
	if err := fc.AnnounceBlob(110, 2, makeCommitment([]byte("b3"))); err == nil {
		t.Fatal("expected error for full slot")
	}
}

func TestAnnounceBlobZeroCommitment(t *testing.T) {
	fc := NewForwardCaster(DefaultForwardCastConfig())
	fc.SetCurrentSlot(100)
	if err := fc.AnnounceBlob(110, 0, types.Hash{}); err != ErrInvalidCommitment {
		t.Fatalf("expected ErrInvalidCommitment, got %v", err)
	}
}

func TestAnnounceBlobDedup(t *testing.T) {
	fc := NewForwardCaster(DefaultForwardCastConfig())
	fc.SetCurrentSlot(100)

	c1 := makeCommitment([]byte("first"))
	c2 := makeCommitment([]byte("second"))
	fc.AnnounceBlob(110, 0, c1)
	fc.AnnounceBlob(110, 0, c2) // same (slot, blobIndex)

	anns := fc.GetAnnouncements(110)
	if len(anns) != 1 {
		t.Fatalf("expected 1 after dedup, got %d", len(anns))
	}
	if anns[0].Commitment != c2 {
		t.Error("commitment should be updated after dedup")
	}
}

func TestGetAnnouncementsEmptyAndSorted(t *testing.T) {
	fc := NewForwardCaster(DefaultForwardCastConfig())
	if fc.GetAnnouncements(999) != nil {
		t.Fatal("expected nil for empty slot")
	}

	fc.SetCurrentSlot(100)
	// Add in reverse order.
	for i := uint64(5); i > 0; i-- {
		fc.AnnounceBlob(110, i-1, makeCommitment([]byte{byte(i)}))
	}
	anns := fc.GetAnnouncements(110)
	if len(anns) != 5 {
		t.Fatalf("expected 5, got %d", len(anns))
	}
	for i, ann := range anns {
		if ann.BlobIndex != uint64(i) {
			t.Errorf("index %d: BlobIndex=%d, want %d", i, ann.BlobIndex, i)
		}
	}
}

func TestValidateAnnouncement(t *testing.T) {
	fc := NewForwardCaster(DefaultForwardCastConfig())
	fc.SetCurrentSlot(100)

	c := makeCommitment([]byte("valid"))
	fc.AnnounceBlob(110, 0, c)
	anns := fc.GetAnnouncements(110)
	if err := fc.ValidateAnnouncement(anns[0]); err != nil {
		t.Fatalf("ValidateAnnouncement: %v", err)
	}
	// Nil.
	if err := fc.ValidateAnnouncement(nil); err != ErrAnnouncementNotFound {
		t.Fatalf("expected ErrAnnouncementNotFound, got %v", err)
	}
	// Zero commitment.
	if err := fc.ValidateAnnouncement(&ForwardCastAnnouncement{Slot: 110}); err != ErrInvalidCommitment {
		t.Fatalf("expected ErrInvalidCommitment, got %v", err)
	}
	// Past slot.
	if err := fc.ValidateAnnouncement(&ForwardCastAnnouncement{Slot: 50, Commitment: c, Expiry: 200}); err == nil {
		t.Fatal("expected error for past slot")
	}
	// Expired.
	if err := fc.ValidateAnnouncement(&ForwardCastAnnouncement{Slot: 110, Commitment: c, Expiry: 99}); err != ErrAnnouncementExpired {
		t.Fatalf("expected ErrAnnouncementExpired, got %v", err)
	}
}

func TestFulfillAnnouncement(t *testing.T) {
	fc := NewForwardCaster(DefaultForwardCastConfig())
	fc.SetCurrentSlot(100)

	blobData := []byte("the actual blob data for fulfillment")
	commitment := makeCommitment(blobData)
	fc.AnnounceBlob(110, 0, commitment)

	anns := fc.GetAnnouncements(110)
	if err := fc.FulfillAnnouncement(anns[0], blobData); err != nil {
		t.Fatalf("FulfillAnnouncement: %v", err)
	}
	status := fc.CheckFulfillment(110)
	if status.Total != 1 || status.Fulfilled != 1 || status.Missing != 0 {
		t.Errorf("status: total=%d fulfilled=%d missing=%d", status.Total, status.Fulfilled, status.Missing)
	}
}

func TestFulfillAnnouncementErrors(t *testing.T) {
	fc := NewForwardCaster(DefaultForwardCastConfig())
	fc.SetCurrentSlot(100)

	// Nil announcement.
	if err := fc.FulfillAnnouncement(nil, []byte("data")); err != ErrAnnouncementNotFound {
		t.Fatalf("expected ErrAnnouncementNotFound, got %v", err)
	}

	// Already fulfilled.
	blobData := []byte("fulfill twice")
	fc.AnnounceBlob(110, 0, makeCommitment(blobData))
	anns := fc.GetAnnouncements(110)
	fc.FulfillAnnouncement(anns[0], blobData)
	if err := fc.FulfillAnnouncement(anns[0], blobData); err != ErrAnnouncementFulfilled {
		t.Fatalf("expected ErrAnnouncementFulfilled, got %v", err)
	}

	// Expired.
	fc2 := NewForwardCaster(DefaultForwardCastConfig())
	fc2.SetCurrentSlot(100)
	expData := []byte("expired")
	fc2.AnnounceBlob(110, 0, makeCommitment(expData))
	expAnns := fc2.GetAnnouncements(110)
	fc2.SetCurrentSlot(130) // past expiry 126
	if err := fc2.FulfillAnnouncement(expAnns[0], expData); err != ErrAnnouncementExpired {
		t.Fatalf("expected ErrAnnouncementExpired, got %v", err)
	}

	// Commitment mismatch.
	fc3 := NewForwardCaster(DefaultForwardCastConfig())
	fc3.SetCurrentSlot(100)
	fc3.AnnounceBlob(110, 0, makeCommitment([]byte("expected")))
	mmAnns := fc3.GetAnnouncements(110)
	if err := fc3.FulfillAnnouncement(mmAnns[0], []byte("wrong")); err != ErrBlobCommitmentMismatch {
		t.Fatalf("expected ErrBlobCommitmentMismatch, got %v", err)
	}
}

func TestFulfillAnnouncementDataTooLarge(t *testing.T) {
	cfg := ForwardCastConfig{
		MaxLeadSlots: 64, MaxAnnouncementsPerSlot: 32,
		ExpirySlots: 16, MaxBlobDataSize: 100,
	}
	fc := NewForwardCaster(cfg)
	fc.SetCurrentSlot(100)

	big := make([]byte, 200)
	fc.AnnounceBlob(110, 0, makeCommitment(big))
	anns := fc.GetAnnouncements(110)
	if err := fc.FulfillAnnouncement(anns[0], big); err == nil {
		t.Fatal("expected error for oversized blob data")
	}
}

func TestCheckFulfillmentMixed(t *testing.T) {
	fc := NewForwardCaster(DefaultForwardCastConfig())
	fc.SetCurrentSlot(100)

	d0, d1, d2 := []byte("blob zero"), []byte("blob one"), []byte("blob two")
	fc.AnnounceBlob(110, 0, makeCommitment(d0))
	fc.AnnounceBlob(110, 1, makeCommitment(d1))
	fc.AnnounceBlob(110, 2, makeCommitment(d2))

	// Fulfill only blobs 0 and 2.
	for _, ann := range fc.GetAnnouncements(110) {
		if ann.BlobIndex == 0 {
			fc.FulfillAnnouncement(ann, d0)
		} else if ann.BlobIndex == 2 {
			fc.FulfillAnnouncement(ann, d2)
		}
	}

	status := fc.CheckFulfillment(110)
	if status.Total != 3 || status.Fulfilled != 2 || status.Missing != 1 {
		t.Errorf("status: %+v", status)
	}
	if len(status.MissingBlobs) != 1 || status.MissingBlobs[0] != 1 {
		t.Errorf("MissingBlobs = %v, want [1]", status.MissingBlobs)
	}
}

func TestCheckFulfillmentEmptySlot(t *testing.T) {
	fc := NewForwardCaster(DefaultForwardCastConfig())
	s := fc.CheckFulfillment(999)
	if s.Total != 0 || s.Fulfilled != 0 || s.Missing != 0 {
		t.Errorf("empty slot should be all-zero: %+v", s)
	}
}

func TestPruneExpired(t *testing.T) {
	fc := NewForwardCaster(DefaultForwardCastConfig())
	fc.SetCurrentSlot(100)

	fc.AnnounceBlob(110, 0, makeCommitment([]byte("soon")))  // expiry 126
	fc.AnnounceBlob(150, 0, makeCommitment([]byte("later"))) // expiry 166

	if fc.GetPendingCount() != 2 {
		t.Fatalf("pending = %d, want 2", fc.GetPendingCount())
	}

	fc.SetCurrentSlot(127)
	fc.PruneExpired()

	if len(fc.GetAnnouncements(110)) != 0 {
		t.Error("slot 110 should be pruned")
	}
	if len(fc.GetAnnouncements(150)) != 1 {
		t.Error("slot 150 should remain")
	}
	if fc.GetPendingCount() != 1 {
		t.Errorf("pending = %d, want 1", fc.GetPendingCount())
	}

	// Prune everything.
	fc.SetCurrentSlot(200)
	fc.PruneExpired()
	if fc.GetPendingCount() != 0 {
		t.Errorf("pending = %d, want 0", fc.GetPendingCount())
	}
}

func TestGetPendingCount(t *testing.T) {
	fc := NewForwardCaster(DefaultForwardCastConfig())
	fc.SetCurrentSlot(100)

	if fc.GetPendingCount() != 0 {
		t.Fatal("pending should be 0 initially")
	}

	fc.AnnounceBlob(110, 0, makeCommitment([]byte("p1")))
	fc.AnnounceBlob(110, 1, makeCommitment([]byte("p2")))
	fc.AnnounceBlob(120, 0, makeCommitment([]byte("p3")))
	if fc.GetPendingCount() != 3 {
		t.Fatalf("pending = %d, want 3", fc.GetPendingCount())
	}

	// Fulfill one.
	for _, ann := range fc.GetAnnouncements(110) {
		if ann.BlobIndex == 0 {
			fc.FulfillAnnouncement(ann, []byte("p1"))
		}
	}
	if fc.GetPendingCount() != 2 {
		t.Fatalf("pending = %d, want 2", fc.GetPendingCount())
	}
}

func TestAnnounceBlobFrom(t *testing.T) {
	fc := NewForwardCaster(DefaultForwardCastConfig())
	fc.SetCurrentSlot(100)

	announcer := types.Address{0xAA, 0xBB}
	fc.AnnounceBlobFrom(110, 0, makeCommitment([]byte("with announcer")), announcer)

	anns := fc.GetAnnouncements(110)
	if len(anns) != 1 || anns[0].Announcer != announcer {
		t.Errorf("Announcer mismatch: %v", anns)
	}
}

func TestForwardCasterConcurrency(t *testing.T) {
	fc := NewForwardCaster(DefaultForwardCastConfig())
	fc.SetCurrentSlot(100)
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			fc.AnnounceBlob(uint64(110+n%5), uint64(n), makeCommitment([]byte{byte(n)}))
		}(i)
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			fc.GetAnnouncements(uint64(110 + n%5))
			fc.GetPendingCount()
			fc.CheckFulfillment(uint64(110 + n%5))
		}(i)
	}
	wg.Wait()
}

func TestNewForwardCasterDefaults(t *testing.T) {
	fc := NewForwardCaster(ForwardCastConfig{})
	if fc.config.MaxLeadSlots != 64 {
		t.Errorf("MaxLeadSlots = %d, want 64", fc.config.MaxLeadSlots)
	}
	if fc.config.MaxAnnouncementsPerSlot != 32 {
		t.Errorf("MaxAnnouncementsPerSlot = %d, want 32", fc.config.MaxAnnouncementsPerSlot)
	}
	if fc.config.ExpirySlots != 16 {
		t.Errorf("ExpirySlots = %d, want 16", fc.config.ExpirySlots)
	}
	if fc.config.MaxBlobDataSize != DefaultBlobSize {
		t.Errorf("MaxBlobDataSize = %d, want %d", fc.config.MaxBlobDataSize, DefaultBlobSize)
	}
}

func TestMultipleSlotFulfillment(t *testing.T) {
	fc := NewForwardCaster(DefaultForwardCastConfig())
	fc.SetCurrentSlot(100)

	d1, d2 := []byte("slot 110 blob"), []byte("slot 120 blob")
	fc.AnnounceBlob(110, 0, makeCommitment(d1))
	fc.AnnounceBlob(120, 0, makeCommitment(d2))

	fc.FulfillAnnouncement(fc.GetAnnouncements(110)[0], d1)
	fc.FulfillAnnouncement(fc.GetAnnouncements(120)[0], d2)

	s1 := fc.CheckFulfillment(110)
	s2 := fc.CheckFulfillment(120)
	if s1.Fulfilled != 1 || s2.Fulfilled != 1 {
		t.Errorf("expected both fulfilled: s1=%d s2=%d", s1.Fulfilled, s2.Fulfilled)
	}
	if fc.GetPendingCount() != 0 {
		t.Errorf("pending = %d, want 0", fc.GetPendingCount())
	}
}
