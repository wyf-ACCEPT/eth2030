package light

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

func syncProtoTestCheckpoint() types.Hash {
	var h types.Hash
	h[0] = 0x01
	h[31] = 0xff
	return h
}

func TestSyncProtocolBootstrap(t *testing.T) {
	sp := NewSyncProtocol()

	if err := sp.Bootstrap(syncProtoTestCheckpoint()); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	store := sp.Store()
	if store.FinalizedHeader == nil {
		t.Fatal("FinalizedHeader should not be nil after bootstrap")
	}
	if store.FinalizedHeader.StateRoot != syncProtoTestCheckpoint() {
		t.Error("FinalizedHeader.StateRoot should match checkpoint")
	}
	if len(store.CurrentCommittee) != SyncCommitteeSize {
		t.Errorf("expected %d committee members, got %d", SyncCommitteeSize, len(store.CurrentCommittee))
	}
	if sp.ProtocolCurrentPeriod() != 0 {
		t.Errorf("expected period 0, got %d", sp.ProtocolCurrentPeriod())
	}
}

func TestSyncProtocolBootstrapEmptyCheckpoint(t *testing.T) {
	sp := NewSyncProtocol()
	if err := sp.Bootstrap(types.Hash{}); err != ErrSyncProtoNoCheckpoint {
		t.Errorf("expected ErrSyncProtoNoCheckpoint, got %v", err)
	}
}

func TestSyncProtocolApplyFinalityUpdate(t *testing.T) {
	sp := NewSyncProtocol()
	if err := sp.Bootstrap(syncProtoTestCheckpoint()); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	store := sp.Store()
	attestedHdr := MakeTestBeaconHeader(100)
	finalizedHdr := MakeTestBeaconHeader(90)
	bits := MakeProtocolSyncBits(400) // > 2/3 of 512

	sig := MakeProtocolSyncSignature(attestedHdr, store.CurrentCommittee, bits)

	// Find a finality branch that passes verification.
	branch := makeSyncProtoFinalityBranch(finalizedHdr, attestedHdr.StateRoot)

	update := ProtocolFinalityUpdate{
		AttestedHeader:         attestedHdr,
		FinalizedHeader:        finalizedHdr,
		FinalityBranch:         branch,
		SyncAggregateBits:      bits,
		SyncAggregateSignature: sig,
	}

	if err := sp.ApplyProtocolFinalityUpdate(update); err != nil {
		t.Fatalf("ApplyProtocolFinalityUpdate: %v", err)
	}

	if sp.ProtocolFinalizedSlot() != 90 {
		t.Errorf("expected finalized slot 90, got %d", sp.ProtocolFinalizedSlot())
	}

	updatedStore := sp.Store()
	if updatedStore.OptimisticHeader == nil {
		t.Error("OptimisticHeader should be set")
	}
}

func TestSyncProtocolApplyFinalityUpdateInsufficientSigs(t *testing.T) {
	sp := NewSyncProtocol()
	sp.Bootstrap(syncProtoTestCheckpoint())

	store := sp.Store()
	attestedHdr := MakeTestBeaconHeader(100)
	finalizedHdr := MakeTestBeaconHeader(90)
	bits := MakeProtocolSyncBits(100) // < 2/3 of 512

	sig := MakeProtocolSyncSignature(attestedHdr, store.CurrentCommittee, bits)
	branch := []types.Hash{{0x01}}

	update := ProtocolFinalityUpdate{
		AttestedHeader:         attestedHdr,
		FinalizedHeader:        finalizedHdr,
		FinalityBranch:         branch,
		SyncAggregateBits:      bits,
		SyncAggregateSignature: sig,
	}

	if err := sp.ApplyProtocolFinalityUpdate(update); err != ErrSyncProtoInsufficientSigs {
		t.Errorf("expected ErrSyncProtoInsufficientSigs, got %v", err)
	}
}

func TestSyncProtocolApplyFinalityUpdateNoCommittee(t *testing.T) {
	sp := NewSyncProtocol()
	update := ProtocolFinalityUpdate{FinalityBranch: []types.Hash{{0x01}}}

	if err := sp.ApplyProtocolFinalityUpdate(update); err != ErrSyncProtoNoCommittee {
		t.Errorf("expected ErrSyncProtoNoCommittee, got %v", err)
	}
}

func TestSyncProtocolApplyFinalityUpdateNoBranch(t *testing.T) {
	sp := NewSyncProtocol()
	sp.Bootstrap(syncProtoTestCheckpoint())

	update := ProtocolFinalityUpdate{}
	if err := sp.ApplyProtocolFinalityUpdate(update); err != ErrSyncProtoNoFinalityBranch {
		t.Errorf("expected ErrSyncProtoNoFinalityBranch, got %v", err)
	}
}

func TestSyncProtocolProcessSyncRotation(t *testing.T) {
	sp := NewSyncProtocol()
	sp.Bootstrap(syncProtoTestCheckpoint())

	// Create a next committee rotation for period 1.
	nextCommittee := make([]BLSPubkey, SyncCommitteeSize)
	for i := range nextCommittee {
		nextCommittee[i][0] = byte(i)
		nextCommittee[i][47] = 0xff
	}

	proof := makeSyncProtoCommitteeProof(nextCommittee, sp.Store().FinalizedHeader)

	update := &SyncCommitteeRotation{
		Period:        1,
		NextCommittee: nextCommittee,
		Proof:         proof,
	}

	if err := sp.ProcessSyncRotation(update); err != nil {
		t.Fatalf("ProcessSyncRotation: %v", err)
	}

	if !sp.HasProtocolNextCommittee() {
		t.Error("expected next committee to be set")
	}
}

func TestSyncProtocolProcessSyncRotationNil(t *testing.T) {
	sp := NewSyncProtocol()
	sp.Bootstrap(syncProtoTestCheckpoint())

	if err := sp.ProcessSyncRotation(nil); err != ErrSyncProtoNoUpdate {
		t.Errorf("expected ErrSyncProtoNoUpdate, got %v", err)
	}
}

func TestSyncProtocolProcessSyncRotationWrongPeriod(t *testing.T) {
	sp := NewSyncProtocol()
	sp.Bootstrap(syncProtoTestCheckpoint())

	update := &SyncCommitteeRotation{
		Period:        5, // wrong, should be 1
		NextCommittee: make([]BLSPubkey, SyncCommitteeSize),
	}

	if err := sp.ProcessSyncRotation(update); err != ErrSyncProtoWrongPeriod {
		t.Errorf("expected ErrSyncProtoWrongPeriod, got %v", err)
	}
}

func TestSyncProtocolProcessSyncRotationEmptyCommittee(t *testing.T) {
	sp := NewSyncProtocol()
	sp.Bootstrap(syncProtoTestCheckpoint())

	update := &SyncCommitteeRotation{Period: 1}
	if err := sp.ProcessSyncRotation(update); err != ErrSyncProtoCommitteeEmpty {
		t.Errorf("expected ErrSyncProtoCommitteeEmpty, got %v", err)
	}
}

func TestSyncProtocolVerifyProtocolSyncSignature(t *testing.T) {
	sp := NewSyncProtocol()
	sp.Bootstrap(syncProtoTestCheckpoint())

	store := sp.Store()
	header := MakeTestBeaconHeader(50)
	bits := MakeProtocolSyncBits(400)
	sig := MakeProtocolSyncSignature(header, store.CurrentCommittee, bits)

	if !sp.VerifyProtocolSyncSignature(header, sig, bits) {
		t.Error("expected signature to verify")
	}
}

func TestSyncProtocolVerifyProtocolSyncSignatureInvalid(t *testing.T) {
	sp := NewSyncProtocol()
	sp.Bootstrap(syncProtoTestCheckpoint())

	header := MakeTestBeaconHeader(50)
	bits := MakeProtocolSyncBits(400)
	var badSig [96]byte
	badSig[0] = 0xff

	if sp.VerifyProtocolSyncSignature(header, badSig, bits) {
		t.Error("expected invalid signature to fail")
	}
}

func TestSyncProtocolNoCommitteeVerify(t *testing.T) {
	sp := NewSyncProtocol()
	header := MakeTestBeaconHeader(50)
	var sig [96]byte

	if sp.VerifyProtocolSyncSignature(header, sig, nil) {
		t.Error("expected false with no committee")
	}
}

func TestBeaconBlockHeaderHash(t *testing.T) {
	h1 := MakeTestBeaconHeader(10)
	h2 := MakeTestBeaconHeader(10)
	h3 := MakeTestBeaconHeader(20)

	hash1 := h1.Hash()
	hash2 := h2.Hash()
	hash3 := h3.Hash()

	if hash1 != hash2 {
		t.Error("same headers should produce same hash")
	}
	if hash1 == hash3 {
		t.Error("different headers should produce different hash")
	}
}

func TestBLSPubkeyType(t *testing.T) {
	var pk BLSPubkey
	if len(pk) != 48 {
		t.Errorf("BLSPubkey should be 48 bytes, got %d", len(pk))
	}
}

func TestCountProtocolBits(t *testing.T) {
	tests := []struct {
		data     []byte
		expected int
	}{
		{nil, 0},
		{[]byte{0xff}, 8},
		{[]byte{0x00}, 0},
		{[]byte{0x01, 0x01}, 2},
		{[]byte{0xff, 0xff}, 16},
	}

	for _, tt := range tests {
		got := countProtocolBits(tt.data)
		if got != tt.expected {
			t.Errorf("countProtocolBits(%v): expected %d, got %d", tt.data, tt.expected, got)
		}
	}
}

// makeSyncProtoFinalityBranch constructs a finality branch that will pass verification.
func makeSyncProtoFinalityBranch(finalized BeaconBlockHeader, stateRoot types.Hash) []types.Hash {
	for nonce := byte(0); ; nonce++ {
		branch := []types.Hash{{nonce}}
		finalizedHash := finalized.Hash()
		msg := finalizedHash[:]
		for _, b := range branch {
			msg = append(msg, b[:]...)
		}
		msg = append(msg, stateRoot[:]...)
		digest := crypto.Keccak256(msg)
		if digest[0]%2 == 0 {
			return branch
		}
	}
}

// makeSyncProtoCommitteeProof constructs a proof that will pass verifyRotationProof.
func makeSyncProtoCommitteeProof(committee []BLSPubkey, header *BeaconBlockHeader) []types.Hash {
	if header == nil {
		return nil
	}
	var commitData []byte
	for _, pk := range committee {
		commitData = append(commitData, pk[:]...)
	}
	committeeRoot := crypto.Keccak256Hash(commitData)

	for nonce := byte(0); ; nonce++ {
		proof := []types.Hash{{nonce}}
		msg := committeeRoot[:]
		for _, p := range proof {
			msg = append(msg, p[:]...)
		}
		msg = append(msg, header.StateRoot[:]...)
		digest := crypto.Keccak256(msg)
		if digest[0]%2 == 0 {
			return proof
		}
	}
}
