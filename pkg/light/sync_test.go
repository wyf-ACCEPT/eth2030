package light

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func makeValidUpdate(attestedNum, finalizedNum uint64) *LightClientUpdate {
	attested := &types.Header{Number: big.NewInt(int64(attestedNum))}
	finalized := &types.Header{Number: big.NewInt(int64(finalizedNum))}

	// Create supermajority bits (>= 2/3 of 512).
	bits := MakeCommitteeBits(400) // 400 out of 512

	sig := SignUpdate(attested, bits)

	return &LightClientUpdate{
		AttestedHeader:    attested,
		FinalizedHeader:   finalized,
		SyncCommitteeBits: bits,
		Signature:         sig,
	}
}

func TestLightSyncer_ProcessUpdate(t *testing.T) {
	store := NewMemoryLightStore()
	syncer := NewLightSyncer(store)

	if syncer.IsSynced() {
		t.Error("should not be synced initially")
	}

	update := makeValidUpdate(100, 90)
	if err := syncer.ProcessUpdate(update); err != nil {
		t.Fatalf("ProcessUpdate: %v", err)
	}

	if !syncer.IsSynced() {
		t.Error("should be synced after valid update")
	}

	finalized := syncer.GetFinalizedHeader()
	if finalized == nil {
		t.Fatal("finalized header is nil")
	}
	if finalized.Number.Int64() != 90 {
		t.Errorf("finalized number = %d, want 90", finalized.Number.Int64())
	}

	if syncer.State().CurrentSlot != 100 {
		t.Errorf("current slot = %d, want 100", syncer.State().CurrentSlot)
	}
}

func TestLightSyncer_NilUpdate(t *testing.T) {
	syncer := NewLightSyncer(NewMemoryLightStore())
	if err := syncer.ProcessUpdate(nil); err != ErrNoUpdate {
		t.Errorf("expected ErrNoUpdate, got %v", err)
	}
}

func TestLightSyncer_MissingAttestedHeader(t *testing.T) {
	syncer := NewLightSyncer(NewMemoryLightStore())
	update := &LightClientUpdate{
		FinalizedHeader:   makeHeader(10),
		Signature:         []byte{0x01},
		SyncCommitteeBits: MakeCommitteeBits(400),
	}
	if err := syncer.ProcessUpdate(update); err != ErrNoAttestedHeader {
		t.Errorf("expected ErrNoAttestedHeader, got %v", err)
	}
}

func TestLightSyncer_MissingFinalizedHeader(t *testing.T) {
	syncer := NewLightSyncer(NewMemoryLightStore())
	update := &LightClientUpdate{
		AttestedHeader:    makeHeader(10),
		Signature:         []byte{0x01},
		SyncCommitteeBits: MakeCommitteeBits(400),
	}
	if err := syncer.ProcessUpdate(update); err != ErrNoFinalizedHeader {
		t.Errorf("expected ErrNoFinalizedHeader, got %v", err)
	}
}

func TestLightSyncer_MissingSignature(t *testing.T) {
	syncer := NewLightSyncer(NewMemoryLightStore())
	update := &LightClientUpdate{
		AttestedHeader:    makeHeader(10),
		FinalizedHeader:   makeHeader(5),
		SyncCommitteeBits: MakeCommitteeBits(400),
	}
	if err := syncer.ProcessUpdate(update); err != ErrNoSignature {
		t.Errorf("expected ErrNoSignature, got %v", err)
	}
}

func TestLightSyncer_InsufficientSignatures(t *testing.T) {
	syncer := NewLightSyncer(NewMemoryLightStore())
	attested := makeHeader(10)
	bits := MakeCommitteeBits(100) // only 100 out of 512
	sig := SignUpdate(attested, bits)

	update := &LightClientUpdate{
		AttestedHeader:    attested,
		FinalizedHeader:   makeHeader(5),
		SyncCommitteeBits: bits,
		Signature:         sig,
	}
	if err := syncer.ProcessUpdate(update); err != ErrInsufficientSigs {
		t.Errorf("expected ErrInsufficientSigs, got %v", err)
	}
}

func TestLightSyncer_FinalizedExceedsAttested(t *testing.T) {
	syncer := NewLightSyncer(NewMemoryLightStore())
	attested := makeHeader(10)
	bits := MakeCommitteeBits(400)
	sig := SignUpdate(attested, bits)

	update := &LightClientUpdate{
		AttestedHeader:    attested,
		FinalizedHeader:   makeHeader(20), // finalized > attested
		SyncCommitteeBits: bits,
		Signature:         sig,
	}
	if err := syncer.ProcessUpdate(update); err != ErrNotFinalized {
		t.Errorf("expected ErrNotFinalized, got %v", err)
	}
}

func TestLightSyncer_CommitteeRotation(t *testing.T) {
	syncer := NewLightSyncer(NewMemoryLightStore())

	nextCommittee := &SyncCommittee{
		Pubkeys:         make([][]byte, 512),
		AggregatePubkey: []byte{0x01},
		Period:          1,
	}

	update := makeValidUpdate(100, 90)
	update.NextSyncCommittee = nextCommittee

	if err := syncer.ProcessUpdate(update); err != nil {
		t.Fatalf("ProcessUpdate: %v", err)
	}

	if syncer.State().CurrentCommittee != nextCommittee {
		t.Error("committee should have been rotated")
	}
}

func TestLightSyncer_MultipleUpdates(t *testing.T) {
	syncer := NewLightSyncer(NewMemoryLightStore())

	// Process multiple updates.
	for i := uint64(1); i <= 5; i++ {
		update := makeValidUpdate(i*100, i*100-10)
		if err := syncer.ProcessUpdate(update); err != nil {
			t.Fatalf("ProcessUpdate %d: %v", i, err)
		}
	}

	finalized := syncer.GetFinalizedHeader()
	if finalized.Number.Int64() != 490 {
		t.Errorf("finalized = %d, want 490", finalized.Number.Int64())
	}
	if syncer.State().CurrentSlot != 500 {
		t.Errorf("current slot = %d, want 500", syncer.State().CurrentSlot)
	}
}

func TestMakeCommitteeBits(t *testing.T) {
	bits := MakeCommitteeBits(10)
	count := 0
	for _, b := range bits {
		for i := 0; i < 8; i++ {
			if b&(1<<uint(i)) != 0 {
				count++
			}
		}
	}
	if count != 10 {
		t.Errorf("bit count = %d, want 10", count)
	}
}
