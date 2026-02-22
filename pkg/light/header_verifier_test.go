package light

import (
	"testing"

	"github.com/eth2030/eth2030/crypto"
)

func makeLightHeader(slot uint64, parentRoot [32]byte) *LightHeader {
	return &LightHeader{
		Slot:          slot,
		ProposerIndex: 1,
		ParentRoot:    parentRoot,
		StateRoot:     [32]byte{0x01},
		BodyRoot:      [32]byte{0x02},
	}
}

func TestLightHeaderHashTreeRoot(t *testing.T) {
	h := &LightHeader{Slot: 100, ProposerIndex: 5}
	root := h.HashTreeRoot()
	if root == ([32]byte{}) {
		t.Fatal("hash tree root should not be zero")
	}
	// Deterministic: same header produces same root.
	root2 := h.HashTreeRoot()
	if root != root2 {
		t.Fatal("hash tree root should be deterministic")
	}
}

func TestLightHeaderHashTreeRootNil(t *testing.T) {
	var h *LightHeader
	root := h.HashTreeRoot()
	if root != ([32]byte{}) {
		t.Fatal("nil header should produce zero root")
	}
}

func TestSyncAggregateParticipationCount(t *testing.T) {
	bits := MakeVerifierCommitteeBits(64, 48)
	sa := &SyncAggregate{SyncCommitteeBits: bits}
	if sa.ParticipationCount() != 48 {
		t.Fatalf("expected 48 participants, got %d", sa.ParticipationCount())
	}
}

func TestSyncAggregateParticipationCountNil(t *testing.T) {
	var sa *SyncAggregate
	if sa.ParticipationCount() != 0 {
		t.Fatal("nil aggregate should have 0 participation")
	}
}

func TestVerifierSyncCommitteeSize(t *testing.T) {
	c := MakeTestVerifierCommittee(64)
	if c.Size() != 64 {
		t.Fatalf("expected 64, got %d", c.Size())
	}

	var nilC *VerifierSyncCommittee
	if nilC.Size() != 0 {
		t.Fatal("nil committee should have size 0")
	}
}

func TestVerifyHeaderChainValid(t *testing.T) {
	genesis := &LightHeader{Slot: 1, ProposerIndex: 0}
	genesisRoot := genesis.HashTreeRoot()

	h1 := &LightHeader{Slot: 2, ParentRoot: genesisRoot}
	h1Root := h1.HashTreeRoot()

	h2 := &LightHeader{Slot: 3, ParentRoot: h1Root}

	hv := NewHeaderVerifier(genesis, nil, 100)
	if err := hv.VerifyHeaderChain([]*LightHeader{h1, h2}); err != nil {
		t.Fatalf("valid chain should verify: %v", err)
	}
}

func TestVerifyHeaderChainEmpty(t *testing.T) {
	hv := NewHeaderVerifier(nil, nil, 100)
	if err := hv.VerifyHeaderChain(nil); err != ErrVerifierEmptyChain {
		t.Fatalf("expected ErrVerifierEmptyChain, got %v", err)
	}
}

func TestVerifyHeaderChainParentMismatch(t *testing.T) {
	genesis := &LightHeader{Slot: 1}

	h1 := &LightHeader{Slot: 2, ParentRoot: [32]byte{0xff}} // wrong parent
	hv := NewHeaderVerifier(genesis, nil, 100)
	if err := hv.VerifyHeaderChain([]*LightHeader{h1}); err != ErrVerifierParentMismatch {
		t.Fatalf("expected ErrVerifierParentMismatch, got %v", err)
	}
}

func TestVerifyHeaderChainSlotNotIncreasing(t *testing.T) {
	genesis := &LightHeader{Slot: 10}
	genesisRoot := genesis.HashTreeRoot()

	h1 := &LightHeader{Slot: 5, ParentRoot: genesisRoot} // slot goes backwards
	hv := NewHeaderVerifier(genesis, nil, 100)
	if err := hv.VerifyHeaderChain([]*LightHeader{h1}); err != ErrVerifierSlotNotIncreasing {
		t.Fatalf("expected ErrVerifierSlotNotIncreasing, got %v", err)
	}
}

func TestVerifyHeaderChainDepthExceeded(t *testing.T) {
	hv := NewHeaderVerifier(nil, nil, 2)
	headers := make([]*LightHeader, 3)
	for i := range headers {
		headers[i] = &LightHeader{Slot: uint64(i + 1)}
	}
	if err := hv.VerifyHeaderChain(headers); err != ErrVerifierDepthExceeded {
		t.Fatalf("expected ErrVerifierDepthExceeded, got %v", err)
	}
}

func TestVerifyHeaderChainNoTrustedHeader(t *testing.T) {
	h1 := &LightHeader{Slot: 1}
	h1Root := h1.HashTreeRoot()
	h2 := &LightHeader{Slot: 2, ParentRoot: h1Root}

	hv := NewHeaderVerifier(nil, nil, 100)
	if err := hv.VerifyHeaderChain([]*LightHeader{h1, h2}); err != nil {
		t.Fatalf("chain without trusted header should succeed: %v", err)
	}
}

func TestVerifyHeaderChainInternalParentMismatch(t *testing.T) {
	h1 := &LightHeader{Slot: 1}
	h2 := &LightHeader{Slot: 2, ParentRoot: [32]byte{0xab}} // wrong

	hv := NewHeaderVerifier(nil, nil, 100)
	if err := hv.VerifyHeaderChain([]*LightHeader{h1, h2}); err != ErrVerifierParentMismatch {
		t.Fatalf("expected ErrVerifierParentMismatch, got %v", err)
	}
}

func TestVerifyFinalityProof(t *testing.T) {
	finalizedRoot := [32]byte{0x01, 0x02, 0x03}
	depth := FinalityBranchDepth
	branch := BuildFinalityBranch([32]byte{}, finalizedRoot, depth)

	// Compute the state root that this branch produces.
	stateRoot := ComputeFinalityStateRoot(finalizedRoot, branch)

	header := &LightHeader{Slot: 100, StateRoot: stateRoot}
	hv := NewHeaderVerifier(nil, nil, 100)

	if err := hv.VerifyFinalityProof(header, branch, finalizedRoot); err != nil {
		t.Fatalf("valid finality proof should verify: %v", err)
	}
}

func TestVerifyFinalityProofNilHeader(t *testing.T) {
	hv := NewHeaderVerifier(nil, nil, 100)
	if err := hv.VerifyFinalityProof(nil, nil, [32]byte{}); err != ErrVerifierNilHeader {
		t.Fatalf("expected ErrVerifierNilHeader, got %v", err)
	}
}

func TestVerifyFinalityProofEmptyBranch(t *testing.T) {
	hv := NewHeaderVerifier(nil, nil, 100)
	h := &LightHeader{Slot: 1}
	if err := hv.VerifyFinalityProof(h, nil, [32]byte{}); err != ErrVerifierNilFinalityProof {
		t.Fatalf("expected ErrVerifierNilFinalityProof, got %v", err)
	}
}

func TestVerifyFinalityProofMismatch(t *testing.T) {
	hv := NewHeaderVerifier(nil, nil, 100)
	h := &LightHeader{Slot: 1, StateRoot: [32]byte{0xff}}
	branch := [][32]byte{{0x01}, {0x02}}
	if err := hv.VerifyFinalityProof(h, branch, [32]byte{0xaa}); err != ErrVerifierFinalityMismatch {
		t.Fatalf("expected ErrVerifierFinalityMismatch, got %v", err)
	}
}

func TestVerifySyncAggregate(t *testing.T) {
	committee := MakeTestVerifierCommittee(64)
	bits := MakeVerifierCommitteeBits(64, 48) // 75% participation
	signingRoot := [32]byte{0xaa, 0xbb}
	sig := SignSyncAggregate(signingRoot, bits, committee)

	aggregate := &SyncAggregate{
		SyncCommitteeBits: bits,
		Signature:         sig,
	}

	hv := NewHeaderVerifier(nil, committee, 100)
	count, err := hv.VerifySyncAggregate(aggregate, signingRoot, committee)
	if err != nil {
		t.Fatalf("valid sync aggregate should verify: %v", err)
	}
	if count != 48 {
		t.Fatalf("expected 48 participants, got %d", count)
	}
}

func TestVerifySyncAggregateNilAggregate(t *testing.T) {
	hv := NewHeaderVerifier(nil, nil, 100)
	_, err := hv.VerifySyncAggregate(nil, [32]byte{}, nil)
	if err != ErrVerifierNilAggregate {
		t.Fatalf("expected ErrVerifierNilAggregate, got %v", err)
	}
}

func TestVerifySyncAggregateNilCommittee(t *testing.T) {
	hv := NewHeaderVerifier(nil, nil, 100)
	agg := &SyncAggregate{SyncCommitteeBits: []byte{0xff}}
	_, err := hv.VerifySyncAggregate(agg, [32]byte{}, nil)
	if err != ErrVerifierNilCommittee {
		t.Fatalf("expected ErrVerifierNilCommittee, got %v", err)
	}
}

func TestVerifySyncAggregateNoParticipation(t *testing.T) {
	committee := MakeTestVerifierCommittee(64)
	bits := MakeVerifierCommitteeBits(64, 0) // no participation
	agg := &SyncAggregate{SyncCommitteeBits: bits}

	hv := NewHeaderVerifier(nil, committee, 100)
	_, err := hv.VerifySyncAggregate(agg, [32]byte{}, committee)
	if err != ErrVerifierInsufficientPart {
		t.Fatalf("expected ErrVerifierInsufficientPart, got %v", err)
	}
}

func TestVerifySyncAggregateBadSignature(t *testing.T) {
	committee := MakeTestVerifierCommittee(64)
	bits := MakeVerifierCommitteeBits(64, 48)
	agg := &SyncAggregate{
		SyncCommitteeBits: bits,
		Signature:         [96]byte{0xff}, // bad signature
	}

	hv := NewHeaderVerifier(nil, committee, 100)
	_, err := hv.VerifySyncAggregate(agg, [32]byte{0xaa}, committee)
	if err != ErrVerifierSignatureFailed {
		t.Fatalf("expected ErrVerifierSignatureFailed, got %v", err)
	}
}

func TestComputeSigningRoot(t *testing.T) {
	h := &LightHeader{Slot: 42, ProposerIndex: 7}
	domain := [32]byte{0x01, 0x02}

	root1 := ComputeSigningRoot(h, domain)
	if root1 == ([32]byte{}) {
		t.Fatal("signing root should not be zero")
	}

	// Deterministic.
	root2 := ComputeSigningRoot(h, domain)
	if root1 != root2 {
		t.Fatal("signing root should be deterministic")
	}

	// Nil header.
	root3 := ComputeSigningRoot(nil, domain)
	if root3 != ([32]byte{}) {
		t.Fatal("nil header signing root should be zero")
	}
}

func TestCheckSufficientParticipation(t *testing.T) {
	// 48/64 = 75% >= 66.7%
	if err := CheckSufficientParticipation(48, 64); err != nil {
		t.Fatalf("75%% should be sufficient: %v", err)
	}

	// 42/64 = 65.6% < 66.7%
	if err := CheckSufficientParticipation(42, 64); err != ErrVerifierInsufficientPart {
		t.Fatalf("expected ErrVerifierInsufficientPart, got %v", err)
	}

	// 43/64 = 67.2% >= 66.7%
	if err := CheckSufficientParticipation(43, 64); err != nil {
		t.Fatalf("67.2%% should be sufficient: %v", err)
	}

	// Empty committee.
	if err := CheckSufficientParticipation(0, 0); err != ErrVerifierCommitteeEmpty {
		t.Fatalf("expected ErrVerifierCommitteeEmpty, got %v", err)
	}
}

func TestSetTrustedHeaderAndCommittee(t *testing.T) {
	hv := NewHeaderVerifier(nil, nil, 100)

	h := &LightHeader{Slot: 5}
	hv.SetTrustedHeader(h)
	if hv.TrustedHeader() != h {
		t.Fatal("trusted header not set")
	}

	c := MakeTestVerifierCommittee(16)
	hv.SetSyncCommittee(c)
	if hv.SyncCommittee() != c {
		t.Fatal("sync committee not set")
	}
}

func TestComputeVerifierCommitteeRoot(t *testing.T) {
	c := MakeTestVerifierCommittee(8)
	root := computeVerifierCommitteeRoot(c)
	if root == ([32]byte{}) {
		t.Fatal("committee root should not be zero")
	}

	// Nil committee.
	root2 := computeVerifierCommitteeRoot(nil)
	if root2 != ([32]byte{}) {
		t.Fatal("nil committee root should be zero")
	}
}

func TestComputeFinalityStateRoot(t *testing.T) {
	finalizedRoot := [32]byte{0xaa}
	branch := [][32]byte{{0x01}, {0x02}, {0x03}}
	root := ComputeFinalityStateRoot(finalizedRoot, branch)
	if root == ([32]byte{}) {
		t.Fatal("finality state root should not be zero")
	}

	// Deterministic.
	root2 := ComputeFinalityStateRoot(finalizedRoot, branch)
	if root != root2 {
		t.Fatal("finality state root should be deterministic")
	}
}

func TestMakeVerifierCommitteeBits(t *testing.T) {
	bits := MakeVerifierCommitteeBits(16, 10)
	count := 0
	for i := 0; i < 16; i++ {
		if bits[i/8]&(1<<(uint(i)%8)) != 0 {
			count++
		}
	}
	if count != 10 {
		t.Fatalf("expected 10 bits set, got %d", count)
	}
}

// Verify the Keccak256 import compiles correctly.
func TestCryptoImport(t *testing.T) {
	h := crypto.Keccak256([]byte("test"))
	if len(h) != 32 {
		t.Fatal("expected 32-byte hash")
	}
}
