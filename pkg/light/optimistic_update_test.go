package light

import (
	"testing"
)

// makeTestStore creates a LightClientStore with a committee and genesis header
// for testing.
func makeTestStore(committeeSize int) (*LightClientStore, *VerifierSyncCommittee) {
	committee := MakeTestVerifierCommittee(committeeSize)
	genesis := &LightHeader{Slot: 1, ProposerIndex: 0}
	store := NewLightClientStore(genesis, committee)
	return store, committee
}

// makeOptimisticUpdate creates a valid optimistic update for testing.
func makeOptimisticUpdate(
	slot uint64,
	committee *VerifierSyncCommittee,
	committeeSize, participants int,
) *OptimisticUpdate {
	header := &LightHeader{Slot: slot, ProposerIndex: 1}
	domain := [32]byte{0x07}
	signingRoot := ComputeSigningRoot(header, domain)
	bits := MakeVerifierCommitteeBits(committeeSize, participants)
	sig := SignSyncAggregate(signingRoot, bits, committee)

	return &OptimisticUpdate{
		AttestedHeader: header,
		SyncAggregate: &SyncAggregate{
			SyncCommitteeBits: bits,
			Signature:         sig,
		},
		SignatureSlot: slot,
	}
}

func TestProcessOptimisticUpdateValid(t *testing.T) {
	store, committee := makeTestStore(64)
	update := makeOptimisticUpdate(10, committee, 64, 48)

	if err := store.ProcessOptimisticUpdate(update); err != nil {
		t.Fatalf("valid optimistic update should succeed: %v", err)
	}

	h := store.GetOptimisticHeader()
	if h == nil || h.Slot != 10 {
		t.Fatalf("optimistic header should be at slot 10, got %v", h)
	}
}

func TestProcessOptimisticUpdateNil(t *testing.T) {
	store, _ := makeTestStore(64)
	if err := store.ProcessOptimisticUpdate(nil); err != ErrOptUpdateNil {
		t.Fatalf("expected ErrOptUpdateNil, got %v", err)
	}
}

func TestProcessOptimisticUpdateNilHeader(t *testing.T) {
	store, _ := makeTestStore(64)
	update := &OptimisticUpdate{
		SyncAggregate: &SyncAggregate{},
	}
	if err := store.ProcessOptimisticUpdate(update); err != ErrOptUpdateNilHeader {
		t.Fatalf("expected ErrOptUpdateNilHeader, got %v", err)
	}
}

func TestProcessOptimisticUpdateSlotRegression(t *testing.T) {
	store, committee := makeTestStore(64)

	// First update at slot 10.
	update1 := makeOptimisticUpdate(10, committee, 64, 48)
	if err := store.ProcessOptimisticUpdate(update1); err != nil {
		t.Fatalf("first update should succeed: %v", err)
	}

	// Second update at slot 5 should fail.
	update2 := makeOptimisticUpdate(5, committee, 64, 48)
	if err := store.ProcessOptimisticUpdate(update2); err != ErrOptUpdateSlotRegression {
		t.Fatalf("expected ErrOptUpdateSlotRegression, got %v", err)
	}
}

func TestProcessOptimisticUpdateBadSignature(t *testing.T) {
	store, _ := makeTestStore(64)
	header := &LightHeader{Slot: 10}
	bits := MakeVerifierCommitteeBits(64, 48)
	update := &OptimisticUpdate{
		AttestedHeader: header,
		SyncAggregate: &SyncAggregate{
			SyncCommitteeBits: bits,
			Signature:         [96]byte{0xff}, // bad
		},
	}
	if err := store.ProcessOptimisticUpdate(update); err != ErrOptUpdateSigFailed {
		t.Fatalf("expected ErrOptUpdateSigFailed, got %v", err)
	}
}

func TestProcessOptimisticUpdateNoCommittee(t *testing.T) {
	genesis := &LightHeader{Slot: 1}
	store := NewLightClientStore(genesis, nil)
	update := &OptimisticUpdate{
		AttestedHeader: &LightHeader{Slot: 5},
		SyncAggregate:  &SyncAggregate{SyncCommitteeBits: []byte{0xff}},
	}
	if err := store.ProcessOptimisticUpdate(update); err != ErrOptUpdateNoCommittee {
		t.Fatalf("expected ErrOptUpdateNoCommittee, got %v", err)
	}
}

func TestProcessFinalityUpdateValid(t *testing.T) {
	store, committee := makeTestStore(64)

	finalizedHeader := &LightHeader{Slot: 8, ProposerIndex: 2}
	attestedHeader := &LightHeader{Slot: 10, ProposerIndex: 1}

	domain := [32]byte{0x07}
	signingRoot := ComputeSigningRoot(attestedHeader, domain)
	bits := MakeVerifierCommitteeBits(64, 48)
	sig := SignSyncAggregate(signingRoot, bits, committee)

	update := &FinalityUpdate{
		AttestedHeader:  attestedHeader,
		FinalizedHeader: finalizedHeader,
		SyncAggregate: &SyncAggregate{
			SyncCommitteeBits: bits,
			Signature:         sig,
		},
	}

	if err := store.ProcessFinalityUpdate(update); err != nil {
		t.Fatalf("valid finality update should succeed: %v", err)
	}

	h := store.GetFinalizedHeader()
	if h == nil || h.Slot != 8 {
		t.Fatalf("finalized header should be at slot 8, got %v", h)
	}
	if store.FinalizedSlot() != 8 {
		t.Fatalf("finalized slot should be 8")
	}
}

func TestProcessFinalityUpdateNil(t *testing.T) {
	store, _ := makeTestStore(64)
	if err := store.ProcessFinalityUpdate(nil); err != ErrFinUpdateNil {
		t.Fatalf("expected ErrFinUpdateNil, got %v", err)
	}
}

func TestProcessFinalityUpdateNilAttested(t *testing.T) {
	store, _ := makeTestStore(64)
	update := &FinalityUpdate{
		FinalizedHeader: &LightHeader{Slot: 5},
		SyncAggregate:   &SyncAggregate{},
	}
	if err := store.ProcessFinalityUpdate(update); err != ErrFinUpdateNilAttested {
		t.Fatalf("expected ErrFinUpdateNilAttested, got %v", err)
	}
}

func TestProcessFinalityUpdateSlotRegression(t *testing.T) {
	store, committee := makeTestStore(64)

	// First finality update at slot 8.
	domain := [32]byte{0x07}
	h1 := &LightHeader{Slot: 10}
	sr1 := ComputeSigningRoot(h1, domain)
	bits1 := MakeVerifierCommitteeBits(64, 48)
	sig1 := SignSyncAggregate(sr1, bits1, committee)
	u1 := &FinalityUpdate{
		AttestedHeader:  h1,
		FinalizedHeader: &LightHeader{Slot: 8},
		SyncAggregate:   &SyncAggregate{SyncCommitteeBits: bits1, Signature: sig1},
	}
	if err := store.ProcessFinalityUpdate(u1); err != nil {
		t.Fatalf("first update should succeed: %v", err)
	}

	// Second update with finalized slot 5 should fail.
	h2 := &LightHeader{Slot: 12}
	sr2 := ComputeSigningRoot(h2, domain)
	bits2 := MakeVerifierCommitteeBits(64, 48)
	sig2 := SignSyncAggregate(sr2, bits2, committee)
	u2 := &FinalityUpdate{
		AttestedHeader:  h2,
		FinalizedHeader: &LightHeader{Slot: 5},
		SyncAggregate:   &SyncAggregate{SyncCommitteeBits: bits2, Signature: sig2},
	}
	if err := store.ProcessFinalityUpdate(u2); err != ErrFinUpdateSlotRegression {
		t.Fatalf("expected ErrFinUpdateSlotRegression, got %v", err)
	}
}

func TestShouldApplyUpdate(t *testing.T) {
	store, _ := makeTestStore(64)

	// No best update: any update is better.
	update := &FinalityUpdate{
		FinalizedHeader: &LightHeader{Slot: 10},
		SyncAggregate:   &SyncAggregate{SyncCommitteeBits: MakeVerifierCommitteeBits(64, 48)},
	}
	if !store.ShouldApplyUpdate(update) {
		t.Fatal("should apply first update")
	}

	// Set best update.
	store.SetBestValidUpdate(update)

	// Same slot, lower participation: should not apply.
	update2 := &FinalityUpdate{
		FinalizedHeader: &LightHeader{Slot: 10},
		SyncAggregate:   &SyncAggregate{SyncCommitteeBits: MakeVerifierCommitteeBits(64, 40)},
	}
	if store.ShouldApplyUpdate(update2) {
		t.Fatal("should not apply update with lower participation")
	}

	// Higher slot: should apply.
	update3 := &FinalityUpdate{
		FinalizedHeader: &LightHeader{Slot: 15},
		SyncAggregate:   &SyncAggregate{SyncCommitteeBits: MakeVerifierCommitteeBits(64, 48)},
	}
	if !store.ShouldApplyUpdate(update3) {
		t.Fatal("should apply update with higher slot")
	}

	// Nil update: should not apply.
	if store.ShouldApplyUpdate(nil) {
		t.Fatal("should not apply nil update")
	}
}

func TestGetBestValidUpdate(t *testing.T) {
	store, _ := makeTestStore(64)
	if store.GetBestValidUpdate() != nil {
		t.Fatal("initial best update should be nil")
	}

	update := &FinalityUpdate{FinalizedHeader: &LightHeader{Slot: 5}}
	store.SetBestValidUpdate(update)
	if store.GetBestValidUpdate() != update {
		t.Fatal("best update should match")
	}
}

func TestStoreSlotAccessors(t *testing.T) {
	store, _ := makeTestStore(64)
	if store.FinalizedSlot() != 1 {
		t.Fatalf("expected finalized slot 1, got %d", store.FinalizedSlot())
	}
	if store.OptimisticSlot() != 1 {
		t.Fatalf("expected optimistic slot 1, got %d", store.OptimisticSlot())
	}
}

func TestSetCurrentSyncCommittee(t *testing.T) {
	store, _ := makeTestStore(64)
	newCommittee := MakeTestVerifierCommittee(128)
	store.SetCurrentSyncCommittee(newCommittee)
	if store.GetCurrentSyncCommittee().Size() != 128 {
		t.Fatalf("expected committee size 128, got %d", store.GetCurrentSyncCommittee().Size())
	}
}
