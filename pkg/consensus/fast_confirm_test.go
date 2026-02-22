package consensus

import (
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// fcHash creates a deterministic Hash from a byte value for testing.
func fcHash(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	return h
}

func TestDefaultFastConfirmConfig(t *testing.T) {
	cfg := DefaultFastConfirmConfig()
	if cfg.QuorumThreshold != 0.67 {
		t.Errorf("QuorumThreshold = %f, want 0.67", cfg.QuorumThreshold)
	}
	if cfg.MinAttesters != 64 {
		t.Errorf("MinAttesters = %d, want 64", cfg.MinAttesters)
	}
	if cfg.ConfirmTimeout != 4*time.Second {
		t.Errorf("ConfirmTimeout = %v, want 4s", cfg.ConfirmTimeout)
	}
	if cfg.MaxTrackedSlots != 64 {
		t.Errorf("MaxTrackedSlots = %d, want 64", cfg.MaxTrackedSlots)
	}
	if cfg.TotalValidators != 1024 {
		t.Errorf("TotalValidators = %d, want 1024", cfg.TotalValidators)
	}
}

func TestNewFastConfirmTracker_NilConfig(t *testing.T) {
	tracker := NewFastConfirmTracker(nil)
	cfg := tracker.Config()
	if cfg.QuorumThreshold != 0.67 {
		t.Error("nil config should use defaults")
	}
}

func TestAddAttestation_Basic(t *testing.T) {
	cfg := &FastConfirmConfig{
		QuorumThreshold: 0.5,
		MinAttesters:    2,
		ConfirmTimeout:  10 * time.Second,
		MaxTrackedSlots: 64,
		TotalValidators: 10,
	}
	tracker := NewFastConfirmTracker(cfg)

	root := fcHash(0xAA)
	if err := tracker.AddAttestation(1, root, 0); err != nil {
		t.Fatalf("AddAttestation: %v", err)
	}
	if tracker.AttestationCount(1) != 1 {
		t.Errorf("AttestationCount = %d, want 1", tracker.AttestationCount(1))
	}
	if tracker.IsConfirmed(1, root) {
		t.Error("should not be confirmed with 1/10 attestations")
	}
}

func TestAddAttestation_SlotZero(t *testing.T) {
	tracker := NewFastConfirmTracker(nil)
	err := tracker.AddAttestation(0, fcHash(0x01), 0)
	if err != ErrFCSlotZero {
		t.Errorf("expected ErrFCSlotZero, got %v", err)
	}
}

func TestAddAttestation_EmptyRoot(t *testing.T) {
	tracker := NewFastConfirmTracker(nil)
	err := tracker.AddAttestation(1, types.Hash{}, 0)
	if err != ErrFCBlockRootEmpty {
		t.Errorf("expected ErrFCBlockRootEmpty, got %v", err)
	}
}

func TestAddAttestation_DuplicateAttester(t *testing.T) {
	cfg := &FastConfirmConfig{
		QuorumThreshold: 0.5,
		MinAttesters:    1,
		ConfirmTimeout:  10 * time.Second,
		MaxTrackedSlots: 64,
		TotalValidators: 10,
	}
	tracker := NewFastConfirmTracker(cfg)
	root := fcHash(0xBB)

	if err := tracker.AddAttestation(1, root, 5); err != nil {
		t.Fatalf("first attestation: %v", err)
	}
	err := tracker.AddAttestation(1, root, 5)
	if err != ErrFCDuplicateAttester {
		t.Errorf("expected ErrFCDuplicateAttester, got %v", err)
	}
	// Count should still be 1.
	if tracker.AttestationCount(1) != 1 {
		t.Errorf("AttestationCount = %d, want 1", tracker.AttestationCount(1))
	}
}

func TestAddAttestation_QuorumReached(t *testing.T) {
	cfg := &FastConfirmConfig{
		QuorumThreshold: 0.5,
		MinAttesters:    2,
		ConfirmTimeout:  10 * time.Second,
		MaxTrackedSlots: 64,
		TotalValidators: 4,
	}
	tracker := NewFastConfirmTracker(cfg)
	root := fcHash(0xCC)

	// Attester 0: 1/4 = 0.25, below quorum.
	tracker.AddAttestation(1, root, 0)
	if tracker.IsConfirmed(1, root) {
		t.Error("should not be confirmed at 1/4")
	}

	// Attester 1: 2/4 = 0.50, meets quorum (>= 0.5) and min attesters.
	tracker.AddAttestation(1, root, 1)
	if !tracker.IsConfirmed(1, root) {
		t.Error("should be confirmed at 2/4 with 0.5 threshold")
	}

	// Additional attestation should still work.
	if err := tracker.AddAttestation(1, root, 2); err != nil {
		t.Fatalf("third attestation: %v", err)
	}
	if tracker.AttestationCount(1) != 3 {
		t.Errorf("AttestationCount = %d, want 3", tracker.AttestationCount(1))
	}
}

func TestAddAttestation_MinAttestersNotMet(t *testing.T) {
	cfg := &FastConfirmConfig{
		QuorumThreshold: 0.1,  // very low threshold
		MinAttesters:    5,    // but high minimum
		ConfirmTimeout:  10 * time.Second,
		MaxTrackedSlots: 64,
		TotalValidators: 10,
	}
	tracker := NewFastConfirmTracker(cfg)
	root := fcHash(0xDD)

	// Add 4 attesters: ratio = 0.4 > 0.1 threshold, but min attesters = 5.
	for i := 0; i < 4; i++ {
		tracker.AddAttestation(1, root, ValidatorIndex(i))
	}
	if tracker.IsConfirmed(1, root) {
		t.Error("should not be confirmed: min attesters not met")
	}

	// Add 5th attester: now meets both quorum and min.
	tracker.AddAttestation(1, root, 4)
	if !tracker.IsConfirmed(1, root) {
		t.Error("should be confirmed: 5 attesters, ratio 0.5 >= 0.1")
	}
}

func TestGetConfirmation_NotFound(t *testing.T) {
	tracker := NewFastConfirmTracker(nil)
	_, err := tracker.GetConfirmation(999)
	if err != ErrFCNotFound {
		t.Errorf("expected ErrFCNotFound, got %v", err)
	}
}

func TestGetConfirmation_Exists(t *testing.T) {
	cfg := &FastConfirmConfig{
		QuorumThreshold: 0.5,
		MinAttesters:    1,
		ConfirmTimeout:  10 * time.Second,
		MaxTrackedSlots: 64,
		TotalValidators: 2,
	}
	tracker := NewFastConfirmTracker(cfg)
	root := fcHash(0xEE)

	tracker.AddAttestation(5, root, 0)
	tracker.AddAttestation(5, root, 1)

	fc, err := tracker.GetConfirmation(5)
	if err != nil {
		t.Fatalf("GetConfirmation: %v", err)
	}
	if fc.Slot != 5 {
		t.Errorf("Slot = %d, want 5", fc.Slot)
	}
	if fc.BlockRoot != root {
		t.Error("BlockRoot mismatch")
	}
	if fc.AttestationCount != 2 {
		t.Errorf("AttestationCount = %d, want 2", fc.AttestationCount)
	}
	if !fc.Confirmed {
		t.Error("expected confirmed")
	}
	if fc.Timestamp.IsZero() {
		t.Error("confirmed timestamp should not be zero")
	}
}

func TestIsConfirmed_WrongRoot(t *testing.T) {
	cfg := &FastConfirmConfig{
		QuorumThreshold: 0.5,
		MinAttesters:    1,
		ConfirmTimeout:  10 * time.Second,
		MaxTrackedSlots: 64,
		TotalValidators: 2,
	}
	tracker := NewFastConfirmTracker(cfg)
	root := fcHash(0xAA)
	wrongRoot := fcHash(0xBB)

	tracker.AddAttestation(1, root, 0)
	tracker.AddAttestation(1, root, 1)

	if tracker.IsConfirmed(1, wrongRoot) {
		t.Error("should not be confirmed with wrong root")
	}
	if !tracker.IsConfirmed(1, root) {
		t.Error("should be confirmed with correct root")
	}
}

func TestIsConfirmed_UntrackedSlot(t *testing.T) {
	tracker := NewFastConfirmTracker(nil)
	if tracker.IsConfirmed(999, fcHash(0x01)) {
		t.Error("untracked slot should not be confirmed")
	}
}

func TestTrackedSlots(t *testing.T) {
	cfg := &FastConfirmConfig{
		QuorumThreshold: 0.5,
		MinAttesters:    1,
		ConfirmTimeout:  10 * time.Second,
		MaxTrackedSlots: 64,
		TotalValidators: 10,
	}
	tracker := NewFastConfirmTracker(cfg)

	if tracker.TrackedSlots() != 0 {
		t.Errorf("TrackedSlots = %d, want 0", tracker.TrackedSlots())
	}

	tracker.AddAttestation(1, fcHash(0x01), 0)
	tracker.AddAttestation(2, fcHash(0x02), 0)
	tracker.AddAttestation(3, fcHash(0x03), 0)

	if tracker.TrackedSlots() != 3 {
		t.Errorf("TrackedSlots = %d, want 3", tracker.TrackedSlots())
	}
}

func TestAttestationCount_Untracked(t *testing.T) {
	tracker := NewFastConfirmTracker(nil)
	if tracker.AttestationCount(999) != 0 {
		t.Error("untracked slot should have 0 attestations")
	}
}

func TestPruneExpired(t *testing.T) {
	cfg := &FastConfirmConfig{
		QuorumThreshold: 0.5,
		MinAttesters:    1,
		ConfirmTimeout:  100 * time.Millisecond,
		MaxTrackedSlots: 64,
		TotalValidators: 10,
	}
	tracker := NewFastConfirmTracker(cfg)

	tracker.AddAttestation(1, fcHash(0x01), 0)
	tracker.AddAttestation(2, fcHash(0x02), 0)

	if tracker.TrackedSlots() != 2 {
		t.Fatalf("TrackedSlots = %d, want 2", tracker.TrackedSlots())
	}

	// Prune with a future time beyond the timeout.
	future := time.Now().Add(200 * time.Millisecond)
	pruned := tracker.PruneExpired(future)
	if pruned != 2 {
		t.Errorf("pruned = %d, want 2", pruned)
	}
	if tracker.TrackedSlots() != 0 {
		t.Errorf("TrackedSlots after prune = %d, want 0", tracker.TrackedSlots())
	}
}

func TestPruneExpired_SelectivePrune(t *testing.T) {
	cfg := &FastConfirmConfig{
		QuorumThreshold: 0.5,
		MinAttesters:    1,
		ConfirmTimeout:  1 * time.Hour, // very long timeout
		MaxTrackedSlots: 64,
		TotalValidators: 10,
	}
	tracker := NewFastConfirmTracker(cfg)

	tracker.AddAttestation(1, fcHash(0x01), 0)
	tracker.AddAttestation(2, fcHash(0x02), 0)

	// Prune now: nothing should be expired.
	pruned := tracker.PruneExpired(time.Now())
	if pruned != 0 {
		t.Errorf("pruned = %d, want 0 (nothing expired)", pruned)
	}
	if tracker.TrackedSlots() != 2 {
		t.Errorf("TrackedSlots = %d, want 2", tracker.TrackedSlots())
	}
}

func TestMaxTrackedSlots_Pruning(t *testing.T) {
	cfg := &FastConfirmConfig{
		QuorumThreshold: 0.5,
		MinAttesters:    1,
		ConfirmTimeout:  10 * time.Second,
		MaxTrackedSlots: 3,
		TotalValidators: 10,
	}
	tracker := NewFastConfirmTracker(cfg)

	// Add 5 slots; max is 3, so oldest 2 should be pruned.
	for i := Slot(1); i <= 5; i++ {
		tracker.AddAttestation(i, fcHash(byte(i)), 0)
	}

	if tracker.TrackedSlots() != 3 {
		t.Errorf("TrackedSlots = %d, want 3", tracker.TrackedSlots())
	}

	// Slots 1 and 2 should be pruned.
	if tracker.AttestationCount(1) != 0 {
		t.Error("slot 1 should have been pruned")
	}
	if tracker.AttestationCount(2) != 0 {
		t.Error("slot 2 should have been pruned")
	}
	// Slots 3, 4, 5 should remain.
	if tracker.AttestationCount(3) != 1 {
		t.Error("slot 3 should still exist")
	}
	if tracker.AttestationCount(4) != 1 {
		t.Error("slot 4 should still exist")
	}
	if tracker.AttestationCount(5) != 1 {
		t.Error("slot 5 should still exist")
	}
}

func TestMultipleSlots_Independent(t *testing.T) {
	cfg := &FastConfirmConfig{
		QuorumThreshold: 0.5,
		MinAttesters:    1,
		ConfirmTimeout:  10 * time.Second,
		MaxTrackedSlots: 64,
		TotalValidators: 4,
	}
	tracker := NewFastConfirmTracker(cfg)

	rootA := fcHash(0xAA)
	rootB := fcHash(0xBB)

	// Slot 10: 2/4 confirmed.
	tracker.AddAttestation(10, rootA, 0)
	tracker.AddAttestation(10, rootA, 1)

	// Slot 20: 1/4 not confirmed.
	tracker.AddAttestation(20, rootB, 0)

	if !tracker.IsConfirmed(10, rootA) {
		t.Error("slot 10 should be confirmed")
	}
	if tracker.IsConfirmed(20, rootB) {
		t.Error("slot 20 should not be confirmed")
	}
}

func TestQuorum_ZeroTotalValidators(t *testing.T) {
	cfg := &FastConfirmConfig{
		QuorumThreshold: 0.5,
		MinAttesters:    1,
		ConfirmTimeout:  10 * time.Second,
		MaxTrackedSlots: 64,
		TotalValidators: 0, // edge case
	}
	tracker := NewFastConfirmTracker(cfg)

	tracker.AddAttestation(1, fcHash(0x01), 0)
	if tracker.IsConfirmed(1, fcHash(0x01)) {
		t.Error("should not be confirmed with 0 total validators")
	}
}

func TestGetConfirmation_NotConfirmed(t *testing.T) {
	cfg := &FastConfirmConfig{
		QuorumThreshold: 0.9,
		MinAttesters:    1,
		ConfirmTimeout:  10 * time.Second,
		MaxTrackedSlots: 64,
		TotalValidators: 100,
	}
	tracker := NewFastConfirmTracker(cfg)
	root := fcHash(0xFF)

	tracker.AddAttestation(7, root, 0)

	fc, err := tracker.GetConfirmation(7)
	if err != nil {
		t.Fatalf("GetConfirmation: %v", err)
	}
	if fc.Confirmed {
		t.Error("should not be confirmed")
	}
	if !fc.Timestamp.IsZero() {
		t.Error("timestamp should be zero for unconfirmed")
	}
	if fc.AttestationCount != 1 {
		t.Errorf("AttestationCount = %d, want 1", fc.AttestationCount)
	}
}
