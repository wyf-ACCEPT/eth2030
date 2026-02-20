package sync

import (
	"errors"
	"fmt"
	gosync "sync"
	"testing"
)

func TestDefaultBlobSyncConfig(t *testing.T) {
	cfg := DefaultBlobSyncConfig()
	if cfg.MaxPendingBlobs != 128 {
		t.Errorf("MaxPendingBlobs = %d, want 128", cfg.MaxPendingBlobs)
	}
	if cfg.BlobTimeout != 30 {
		t.Errorf("BlobTimeout = %d, want 30", cfg.BlobTimeout)
	}
	if cfg.RetryLimit != 3 {
		t.Errorf("RetryLimit = %d, want 3", cfg.RetryLimit)
	}
	if cfg.PeerTimeout != 15 {
		t.Errorf("PeerTimeout = %d, want 15", cfg.PeerTimeout)
	}
}

func TestNewBlobSyncManager(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())
	if mgr == nil {
		t.Fatal("NewBlobSyncManager returned nil")
	}
	if len(mgr.GetPendingSlots()) != 0 {
		t.Error("new manager should have no pending slots")
	}
}

func TestNewBlobSyncManager_ZeroConfig(t *testing.T) {
	mgr := NewBlobSyncManager(BlobSyncConfig{})
	if mgr.config.MaxPendingBlobs != 128 {
		t.Errorf("MaxPendingBlobs = %d, want 128", mgr.config.MaxPendingBlobs)
	}
	if mgr.config.BlobTimeout != 30 {
		t.Errorf("BlobTimeout = %d, want 30", mgr.config.BlobTimeout)
	}
	if mgr.config.RetryLimit != 3 {
		t.Errorf("RetryLimit = %d, want 3", mgr.config.RetryLimit)
	}
	if mgr.config.PeerTimeout != 15 {
		t.Errorf("PeerTimeout = %d, want 15", mgr.config.PeerTimeout)
	}
}

func TestBlobSync_RequestBlobs(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())

	err := mgr.RequestBlobs(100, []uint64{0, 1, 2})
	if err != nil {
		t.Fatalf("RequestBlobs: %v", err)
	}

	pending := mgr.GetPendingSlots()
	if len(pending) != 1 || pending[0] != 100 {
		t.Errorf("pending slots = %v, want [100]", pending)
	}
}

func TestBlobSync_RequestBlobs_NoIndices(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())

	err := mgr.RequestBlobs(100, nil)
	if !errors.Is(err, ErrBlobSyncNoIndices) {
		t.Fatalf("expected ErrBlobSyncNoIndices, got: %v", err)
	}

	err = mgr.RequestBlobs(100, []uint64{})
	if !errors.Is(err, ErrBlobSyncNoIndices) {
		t.Fatalf("expected ErrBlobSyncNoIndices for empty slice, got: %v", err)
	}
}

func TestBlobSync_RequestBlobs_AlreadyComplete(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())

	_ = mgr.RequestBlobs(100, []uint64{0})
	mgr.MarkSlotComplete(100)

	err := mgr.RequestBlobs(100, []uint64{1})
	if !errors.Is(err, ErrBlobSyncSlotAlreadyComplete) {
		t.Fatalf("expected ErrBlobSyncSlotAlreadyComplete, got: %v", err)
	}
}

func TestBlobSync_RequestBlobs_AdditionalIndices(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())

	_ = mgr.RequestBlobs(100, []uint64{0, 1})
	_ = mgr.RequestBlobs(100, []uint64{2, 3})

	// Process all four blobs to verify they are all accepted.
	for i := uint64(0); i < 4; i++ {
		blob := []byte{byte(i), 0xAA, 0xBB}
		err := mgr.ProcessBlobResponse(100, i, blob)
		if err != nil {
			t.Fatalf("ProcessBlobResponse index %d: %v", i, err)
		}
	}
}

func TestBlobSync_ProcessBlobResponse_NormalFlow(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())
	_ = mgr.RequestBlobs(10, []uint64{0, 1, 2})

	blobs := [][]byte{
		{0x01, 0x02, 0x03},
		{0x04, 0x05, 0x06},
		{0x07, 0x08, 0x09},
	}

	for i, blob := range blobs {
		err := mgr.ProcessBlobResponse(10, uint64(i), blob)
		if err != nil {
			t.Fatalf("ProcessBlobResponse index %d: %v", i, err)
		}
	}

	if mgr.SlotBlobCount(10) != 3 {
		t.Errorf("SlotBlobCount = %d, want 3", mgr.SlotBlobCount(10))
	}
}

func TestBlobSync_ProcessBlobResponse_EmptyBlob(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())
	_ = mgr.RequestBlobs(10, []uint64{0})

	err := mgr.ProcessBlobResponse(10, 0, nil)
	if !errors.Is(err, ErrBlobSyncEmptyBlob) {
		t.Fatalf("expected ErrBlobSyncEmptyBlob, got: %v", err)
	}

	err = mgr.ProcessBlobResponse(10, 0, []byte{})
	if !errors.Is(err, ErrBlobSyncEmptyBlob) {
		t.Fatalf("expected ErrBlobSyncEmptyBlob for empty slice, got: %v", err)
	}
}

func TestBlobSync_ProcessBlobResponse_SlotNotFound(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())

	err := mgr.ProcessBlobResponse(999, 0, []byte{0x01})
	if !errors.Is(err, ErrBlobSyncSlotNotFound) {
		t.Fatalf("expected ErrBlobSyncSlotNotFound, got: %v", err)
	}
}

func TestBlobSync_ProcessBlobResponse_InvalidIndex(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())
	_ = mgr.RequestBlobs(10, []uint64{0, 1})

	err := mgr.ProcessBlobResponse(10, 5, []byte{0x01})
	if !errors.Is(err, ErrBlobSyncInvalidIndex) {
		t.Fatalf("expected ErrBlobSyncInvalidIndex, got: %v", err)
	}
}

func TestBlobSync_ProcessBlobResponse_Duplicate(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())
	_ = mgr.RequestBlobs(10, []uint64{0})

	_ = mgr.ProcessBlobResponse(10, 0, []byte{0x01, 0x02})

	err := mgr.ProcessBlobResponse(10, 0, []byte{0x03, 0x04})
	if !errors.Is(err, ErrBlobSyncDuplicateBlob) {
		t.Fatalf("expected ErrBlobSyncDuplicateBlob, got: %v", err)
	}
}

func TestBlobSync_ProcessBlobResponse_CompletedSlot(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())
	_ = mgr.RequestBlobs(10, []uint64{0, 1})
	_ = mgr.ProcessBlobResponse(10, 0, []byte{0x01})
	mgr.MarkSlotComplete(10)

	err := mgr.ProcessBlobResponse(10, 1, []byte{0x02})
	if !errors.Is(err, ErrBlobSyncSlotAlreadyComplete) {
		t.Fatalf("expected ErrBlobSyncSlotAlreadyComplete, got: %v", err)
	}
}

func TestBlobSync_VerifyBlobConsistency_AllPresent(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())
	_ = mgr.RequestBlobs(10, []uint64{0, 1, 2})

	_ = mgr.ProcessBlobResponse(10, 0, []byte{0x01, 0x02})
	_ = mgr.ProcessBlobResponse(10, 1, []byte{0x03, 0x04})
	_ = mgr.ProcessBlobResponse(10, 2, []byte{0x05, 0x06})

	ok, err := mgr.VerifyBlobConsistency(10)
	if err != nil {
		t.Fatalf("VerifyBlobConsistency: %v", err)
	}
	if !ok {
		t.Error("expected consistency check to pass")
	}
	if !mgr.IsSlotVerified(10) {
		t.Error("slot should be marked as verified")
	}
}

func TestBlobSync_VerifyBlobConsistency_MissingBlobs(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())
	_ = mgr.RequestBlobs(10, []uint64{0, 1, 2})

	_ = mgr.ProcessBlobResponse(10, 0, []byte{0x01, 0x02})
	// indices 1 and 2 are missing

	ok, err := mgr.VerifyBlobConsistency(10)
	if err != nil {
		t.Fatalf("VerifyBlobConsistency: %v", err)
	}
	if ok {
		t.Error("expected consistency check to fail with missing blobs")
	}
}

func TestBlobSync_VerifyBlobConsistency_SlotNotFound(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())

	_, err := mgr.VerifyBlobConsistency(999)
	if !errors.Is(err, ErrBlobSyncSlotNotFound) {
		t.Fatalf("expected ErrBlobSyncSlotNotFound, got: %v", err)
	}
}

func TestBlobSync_VerifyBlobConsistency_DuplicateContent(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())
	_ = mgr.RequestBlobs(10, []uint64{0, 1})

	// Both blobs have identical content.
	sameData := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	_ = mgr.ProcessBlobResponse(10, 0, sameData)
	_ = mgr.ProcessBlobResponse(10, 1, sameData)

	ok, err := mgr.VerifyBlobConsistency(10)
	if !errors.Is(err, ErrBlobSyncInconsistent) {
		t.Fatalf("expected ErrBlobSyncInconsistent, got ok=%v err=%v", ok, err)
	}
	if ok {
		t.Error("expected consistency check to fail with duplicate content")
	}
}

func TestBlobSync_GetPendingSlots_MultipleSlots(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())
	_ = mgr.RequestBlobs(300, []uint64{0})
	_ = mgr.RequestBlobs(100, []uint64{0})
	_ = mgr.RequestBlobs(200, []uint64{0})

	pending := mgr.GetPendingSlots()
	if len(pending) != 3 {
		t.Fatalf("expected 3 pending slots, got %d", len(pending))
	}
	// Should be sorted.
	if pending[0] != 100 || pending[1] != 200 || pending[2] != 300 {
		t.Errorf("pending slots = %v, want [100 200 300]", pending)
	}
}

func TestBlobSync_GetPendingSlots_ExcludesComplete(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())
	_ = mgr.RequestBlobs(100, []uint64{0})
	_ = mgr.RequestBlobs(200, []uint64{0})
	mgr.MarkSlotComplete(100)

	pending := mgr.GetPendingSlots()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending slot, got %d", len(pending))
	}
	if pending[0] != 200 {
		t.Errorf("pending slot = %d, want 200", pending[0])
	}
}

func TestBlobSync_GetVerifiedBlobs(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())
	_ = mgr.RequestBlobs(10, []uint64{0, 1, 2})

	blob0 := []byte{0x01, 0x02, 0x03}
	blob1 := []byte{0x04, 0x05, 0x06}
	blob2 := []byte{0x07, 0x08, 0x09}

	_ = mgr.ProcessBlobResponse(10, 0, blob0)
	_ = mgr.ProcessBlobResponse(10, 1, blob1)
	_ = mgr.ProcessBlobResponse(10, 2, blob2)

	// Before verification, GetVerifiedBlobs returns nil.
	if blobs := mgr.GetVerifiedBlobs(10); blobs != nil {
		t.Error("expected nil before verification")
	}

	ok, err := mgr.VerifyBlobConsistency(10)
	if err != nil || !ok {
		t.Fatalf("VerifyBlobConsistency: ok=%v err=%v", ok, err)
	}

	blobs := mgr.GetVerifiedBlobs(10)
	if len(blobs) != 3 {
		t.Fatalf("expected 3 verified blobs, got %d", len(blobs))
	}

	// Verify blob order and content.
	for i, expected := range [][]byte{blob0, blob1, blob2} {
		if len(blobs[i]) != len(expected) {
			t.Errorf("blob[%d] length = %d, want %d", i, len(blobs[i]), len(expected))
			continue
		}
		for j := range expected {
			if blobs[i][j] != expected[j] {
				t.Errorf("blob[%d][%d] = %x, want %x", i, j, blobs[i][j], expected[j])
			}
		}
	}
}

func TestBlobSync_GetVerifiedBlobs_NotFound(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())
	if blobs := mgr.GetVerifiedBlobs(999); blobs != nil {
		t.Error("expected nil for unknown slot")
	}
}

func TestBlobSync_MarkSlotComplete(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())
	_ = mgr.RequestBlobs(10, []uint64{0})
	_ = mgr.ProcessBlobResponse(10, 0, []byte{0x01})

	if mgr.IsSlotComplete(10) {
		t.Error("slot should not be complete before marking")
	}

	mgr.MarkSlotComplete(10)

	if !mgr.IsSlotComplete(10) {
		t.Error("slot should be complete after marking")
	}

	// Verify it's no longer pending.
	pending := mgr.GetPendingSlots()
	for _, s := range pending {
		if s == 10 {
			t.Error("completed slot should not appear in pending")
		}
	}
}

func TestBlobSync_MarkSlotComplete_Unknown(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())
	// Should not panic for unknown slot.
	mgr.MarkSlotComplete(999)
	if mgr.IsSlotComplete(999) {
		t.Error("unknown slot should not report as complete")
	}
}

func TestBlobSync_PeerStats(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())
	_ = mgr.RequestBlobs(10, []uint64{0, 1, 2})

	_ = mgr.ProcessBlobResponseFromPeer(10, 0, []byte{0x01}, "peer-A")
	_ = mgr.ProcessBlobResponseFromPeer(10, 1, []byte{0x02}, "peer-A")
	_ = mgr.ProcessBlobResponseFromPeer(10, 2, []byte{0x03}, "peer-B")

	stats := mgr.PeerStats()
	if stats["peer-A"] != 2 {
		t.Errorf("peer-A downloads = %d, want 2", stats["peer-A"])
	}
	if stats["peer-B"] != 1 {
		t.Errorf("peer-B downloads = %d, want 1", stats["peer-B"])
	}
}

func TestBlobSync_PeerStats_Empty(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())
	stats := mgr.PeerStats()
	if len(stats) != 0 {
		t.Errorf("expected empty peer stats, got %d entries", len(stats))
	}
}

func TestBlobSync_PeerStats_NoPeerID(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())
	_ = mgr.RequestBlobs(10, []uint64{0})
	_ = mgr.ProcessBlobResponse(10, 0, []byte{0x01})

	stats := mgr.PeerStats()
	if len(stats) != 0 {
		t.Errorf("expected empty peer stats without peerID, got %d", len(stats))
	}
}

func TestBlobSync_IsSlotVerified_NotVerified(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())
	if mgr.IsSlotVerified(10) {
		t.Error("unknown slot should not be verified")
	}

	_ = mgr.RequestBlobs(10, []uint64{0})
	if mgr.IsSlotVerified(10) {
		t.Error("slot with pending requests should not be verified")
	}
}

func TestBlobSync_SlotBlobCount(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())
	if mgr.SlotBlobCount(10) != 0 {
		t.Error("unknown slot should have 0 blobs")
	}

	_ = mgr.RequestBlobs(10, []uint64{0, 1, 2})
	if mgr.SlotBlobCount(10) != 0 {
		t.Error("slot with no responses should have 0 blobs")
	}

	_ = mgr.ProcessBlobResponse(10, 0, []byte{0x01})
	if mgr.SlotBlobCount(10) != 1 {
		t.Errorf("expected 1 blob, got %d", mgr.SlotBlobCount(10))
	}
}

func TestBlobSync_FullWorkflow(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())

	// Request blobs for two slots.
	_ = mgr.RequestBlobs(100, []uint64{0, 1, 2, 3})
	_ = mgr.RequestBlobs(200, []uint64{0, 1})

	// Process blobs for slot 100.
	for i := uint64(0); i < 4; i++ {
		blob := make([]byte, 32)
		blob[0] = byte(i)
		blob[1] = 0xAA
		_ = mgr.ProcessBlobResponseFromPeer(100, i, blob, fmt.Sprintf("peer-%d", i%2))
	}

	// Process blobs for slot 200.
	_ = mgr.ProcessBlobResponseFromPeer(200, 0, []byte{0xBB, 0x01}, "peer-0")
	_ = mgr.ProcessBlobResponseFromPeer(200, 1, []byte{0xCC, 0x02}, "peer-1")

	// Verify slot 100.
	ok, err := mgr.VerifyBlobConsistency(100)
	if err != nil || !ok {
		t.Fatalf("slot 100 verify: ok=%v err=%v", ok, err)
	}

	// Verify slot 200.
	ok, err = mgr.VerifyBlobConsistency(200)
	if err != nil || !ok {
		t.Fatalf("slot 200 verify: ok=%v err=%v", ok, err)
	}

	// Get verified blobs.
	blobs100 := mgr.GetVerifiedBlobs(100)
	if len(blobs100) != 4 {
		t.Errorf("slot 100: got %d verified blobs, want 4", len(blobs100))
	}

	blobs200 := mgr.GetVerifiedBlobs(200)
	if len(blobs200) != 2 {
		t.Errorf("slot 200: got %d verified blobs, want 2", len(blobs200))
	}

	// Mark slots complete.
	mgr.MarkSlotComplete(100)
	mgr.MarkSlotComplete(200)

	pending := mgr.GetPendingSlots()
	if len(pending) != 0 {
		t.Errorf("expected no pending slots after completion, got %v", pending)
	}

	// Check peer stats.
	stats := mgr.PeerStats()
	if stats["peer-0"] != 3 { // 2 from slot 100 + 1 from slot 200
		t.Errorf("peer-0 = %d, want 3", stats["peer-0"])
	}
	if stats["peer-1"] != 3 { // 2 from slot 100 + 1 from slot 200
		t.Errorf("peer-1 = %d, want 3", stats["peer-1"])
	}
}

func TestBlobSync_GetVerifiedBlobs_DataIsolation(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())
	_ = mgr.RequestBlobs(10, []uint64{0})

	original := []byte{0x01, 0x02, 0x03}
	_ = mgr.ProcessBlobResponse(10, 0, original)
	_, _ = mgr.VerifyBlobConsistency(10)

	blobs := mgr.GetVerifiedBlobs(10)

	// Mutate the returned slice -- original data should be unaffected.
	blobs[0][0] = 0xFF

	blobs2 := mgr.GetVerifiedBlobs(10)
	if blobs2[0][0] != 0x01 {
		t.Error("returned blob data should be a copy, not a reference")
	}
}

func TestBlobSync_ConcurrentAccess(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())

	// Set up multiple slots.
	for slot := uint64(0); slot < 10; slot++ {
		_ = mgr.RequestBlobs(slot, []uint64{0, 1, 2})
	}

	var wg gosync.WaitGroup

	// Concurrent writers.
	for slot := uint64(0); slot < 10; slot++ {
		for idx := uint64(0); idx < 3; idx++ {
			wg.Add(1)
			go func(s, i uint64) {
				defer wg.Done()
				blob := []byte{byte(s), byte(i), 0xAB}
				_ = mgr.ProcessBlobResponseFromPeer(s, i, blob, fmt.Sprintf("p%d", s))
			}(slot, idx)
		}
	}

	// Concurrent readers.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = mgr.GetPendingSlots()
			_ = mgr.PeerStats()
			for s := uint64(0); s < 10; s++ {
				_ = mgr.SlotBlobCount(s)
				_ = mgr.IsSlotComplete(s)
				_ = mgr.IsSlotVerified(s)
				_ = mgr.GetVerifiedBlobs(s)
			}
		}()
	}

	wg.Wait()

	// Verify and complete all slots after concurrent operations finish.
	for slot := uint64(0); slot < 10; slot++ {
		ok, err := mgr.VerifyBlobConsistency(slot)
		if err != nil {
			t.Errorf("slot %d verify error: %v", slot, err)
		}
		if !ok {
			t.Errorf("slot %d verification failed", slot)
		}
		mgr.MarkSlotComplete(slot)
	}

	pending := mgr.GetPendingSlots()
	if len(pending) != 0 {
		t.Errorf("expected 0 pending after completing all, got %d", len(pending))
	}
}

func TestBlobSync_ProcessBlobResponse_InputNotAliased(t *testing.T) {
	mgr := NewBlobSyncManager(DefaultBlobSyncConfig())
	_ = mgr.RequestBlobs(10, []uint64{0})

	input := []byte{0x01, 0x02, 0x03}
	_ = mgr.ProcessBlobResponse(10, 0, input)

	// Mutate the input after storing.
	input[0] = 0xFF

	_, _ = mgr.VerifyBlobConsistency(10)
	blobs := mgr.GetVerifiedBlobs(10)
	if blobs[0][0] != 0x01 {
		t.Error("stored blob should not be aliased to input slice")
	}
}
