package state

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// makeTestProof creates a ValidityProof for testing with a deterministic
// state root derived from the block number.
func makeTestProof(blockNum uint64) *ValidityProof {
	var root types.Hash
	root[0] = byte(blockNum >> 8)
	root[1] = byte(blockNum)
	root[31] = 0xAA // ensure non-zero
	return &ValidityProof{
		StateRoot:    root,
		BlockNumber:  blockNum,
		ProofData:    []byte{0x01, 0x02, 0x03, byte(blockNum)},
		VerifierType: VerifierTypeSNARK,
	}
}

func TestValidityOnlyState_AddAndCount(t *testing.T) {
	vs := NewValidityOnlyState()

	if vs.ProofCount() != 0 {
		t.Fatalf("expected 0 proofs, got %d", vs.ProofCount())
	}

	p1 := makeTestProof(1)
	if err := vs.AddValidityProof(p1); err != nil {
		t.Fatalf("AddValidityProof(1): %v", err)
	}
	if vs.ProofCount() != 1 {
		t.Fatalf("expected 1 proof, got %d", vs.ProofCount())
	}

	p2 := makeTestProof(2)
	if err := vs.AddValidityProof(p2); err != nil {
		t.Fatalf("AddValidityProof(2): %v", err)
	}
	if vs.ProofCount() != 2 {
		t.Fatalf("expected 2 proofs, got %d", vs.ProofCount())
	}
}

func TestValidityOnlyState_AddErrors(t *testing.T) {
	vs := NewValidityOnlyState()

	// nil proof
	if err := vs.AddValidityProof(nil); err != ErrValidityNilProof {
		t.Errorf("nil proof: got %v, want %v", err, ErrValidityNilProof)
	}

	// empty root
	if err := vs.AddValidityProof(&ValidityProof{
		ProofData: []byte{1},
	}); err != ErrValidityEmptyRoot {
		t.Errorf("empty root: got %v, want %v", err, ErrValidityEmptyRoot)
	}

	// empty proof data
	var nonZero types.Hash
	nonZero[0] = 0xFF
	if err := vs.AddValidityProof(&ValidityProof{
		StateRoot: nonZero,
	}); err != ErrValidityEmptyProofData {
		t.Errorf("empty data: got %v, want %v", err, ErrValidityEmptyProofData)
	}

	// duplicate block number
	p := makeTestProof(10)
	if err := vs.AddValidityProof(p); err != nil {
		t.Fatalf("first add: %v", err)
	}
	p2 := makeTestProof(10)
	p2.StateRoot[2] = 0xBB // different root, same block
	if err := vs.AddValidityProof(p2); err != ErrValidityDuplicateProof {
		t.Errorf("duplicate block: got %v, want %v", err, ErrValidityDuplicateProof)
	}
}

func TestValidityOnlyState_IsValid(t *testing.T) {
	vs := NewValidityOnlyState()

	p1 := makeTestProof(1)
	vs.AddValidityProof(p1)

	if !vs.IsValid(p1.StateRoot) {
		t.Error("expected root to be valid after adding proof")
	}

	var unknownRoot types.Hash
	unknownRoot[0] = 0xFF
	unknownRoot[1] = 0xFF
	if vs.IsValid(unknownRoot) {
		t.Error("expected unknown root to be invalid")
	}

	// Zero root should never be valid (never added).
	if vs.IsValid(types.Hash{}) {
		t.Error("expected zero root to be invalid")
	}
}

func TestValidityOnlyState_GetLatestValidRoot(t *testing.T) {
	vs := NewValidityOnlyState()

	// No proofs: zero hash.
	if root := vs.GetLatestValidRoot(); !root.IsZero() {
		t.Errorf("expected zero root, got %v", root)
	}

	p1 := makeTestProof(1)
	vs.AddValidityProof(p1)
	if root := vs.GetLatestValidRoot(); root != p1.StateRoot {
		t.Errorf("latest root = %v, want %v", root, p1.StateRoot)
	}

	p2 := makeTestProof(2)
	vs.AddValidityProof(p2)
	if root := vs.GetLatestValidRoot(); root != p2.StateRoot {
		t.Errorf("latest root = %v, want %v", root, p2.StateRoot)
	}
}

func TestValidityOnlyState_VerifyStateTransition(t *testing.T) {
	vs := NewValidityOnlyState()

	var prevRoot, newRoot types.Hash
	prevRoot[0] = 0x01
	newRoot[0] = 0x02

	proof := &ValidityProof{
		StateRoot:    newRoot,
		BlockNumber:  1,
		ProofData:    []byte{0xDE, 0xAD, 0xBE, 0xEF},
		VerifierType: VerifierTypeSNARK,
	}

	// Valid transition.
	if !vs.VerifyStateTransition(prevRoot, newRoot, proof) {
		t.Error("expected valid transition")
	}

	// nil proof.
	if vs.VerifyStateTransition(prevRoot, newRoot, nil) {
		t.Error("expected invalid for nil proof")
	}

	// Root mismatch.
	var wrongRoot types.Hash
	wrongRoot[0] = 0x99
	if vs.VerifyStateTransition(prevRoot, wrongRoot, proof) {
		t.Error("expected invalid for root mismatch")
	}

	// Zero prevRoot.
	if vs.VerifyStateTransition(types.Hash{}, newRoot, proof) {
		t.Error("expected invalid for zero prevRoot")
	}

	// Zero newRoot.
	if vs.VerifyStateTransition(prevRoot, types.Hash{}, proof) {
		t.Error("expected invalid for zero newRoot")
	}

	// Empty proof data.
	emptyProof := &ValidityProof{StateRoot: newRoot}
	if vs.VerifyStateTransition(prevRoot, newRoot, emptyProof) {
		t.Error("expected invalid for empty proof data")
	}
}

func TestValidityOnlyState_PruneOldProofs(t *testing.T) {
	vs := NewValidityOnlyState()

	// Add 10 proofs.
	for i := uint64(1); i <= 10; i++ {
		vs.AddValidityProof(makeTestProof(i))
	}
	if vs.ProofCount() != 10 {
		t.Fatalf("expected 10 proofs, got %d", vs.ProofCount())
	}

	// Keep last 3.
	vs.PruneOldProofs(3)
	if vs.ProofCount() != 3 {
		t.Fatalf("after prune(3): expected 3 proofs, got %d", vs.ProofCount())
	}

	// Proofs for blocks 8, 9, 10 should remain.
	for _, bn := range []uint64{8, 9, 10} {
		p := makeTestProof(bn)
		if !vs.IsValid(p.StateRoot) {
			t.Errorf("expected block %d root to be valid after prune", bn)
		}
	}

	// Proofs for blocks 1-7 should be gone.
	for _, bn := range []uint64{1, 2, 3, 4, 5, 6, 7} {
		p := makeTestProof(bn)
		if vs.IsValid(p.StateRoot) {
			t.Errorf("expected block %d root to be invalid after prune", bn)
		}
	}

	// Latest root should be block 10.
	p10 := makeTestProof(10)
	if root := vs.GetLatestValidRoot(); root != p10.StateRoot {
		t.Errorf("latest root after prune: got %v, want %v", root, p10.StateRoot)
	}
}

func TestValidityOnlyState_PruneEdgeCases(t *testing.T) {
	vs := NewValidityOnlyState()

	// Prune on empty state: no panic.
	vs.PruneOldProofs(5)
	if vs.ProofCount() != 0 {
		t.Fatal("expected 0 after prune on empty")
	}

	// Prune with keepLastN <= 0: no-op.
	vs.AddValidityProof(makeTestProof(1))
	vs.PruneOldProofs(0)
	if vs.ProofCount() != 1 {
		t.Fatal("expected 1 after prune(0)")
	}
	vs.PruneOldProofs(-1)
	if vs.ProofCount() != 1 {
		t.Fatal("expected 1 after prune(-1)")
	}

	// Prune with keepLastN > count: no-op.
	vs.PruneOldProofs(100)
	if vs.ProofCount() != 1 {
		t.Fatal("expected 1 after prune(100)")
	}
}

func TestValidityOnlyState_GetProofByBlock(t *testing.T) {
	vs := NewValidityOnlyState()

	p5 := makeTestProof(5)
	vs.AddValidityProof(p5)

	got := vs.GetProofByBlock(5)
	if got == nil {
		t.Fatal("expected proof for block 5")
	}
	if got.StateRoot != p5.StateRoot {
		t.Errorf("proof root = %v, want %v", got.StateRoot, p5.StateRoot)
	}
	if got.BlockNumber != 5 {
		t.Errorf("proof block = %d, want 5", got.BlockNumber)
	}

	// Verify we get a copy, not the original.
	got.ProofData[0] = 0xFF
	orig := vs.GetProofByBlock(5)
	if orig.ProofData[0] == 0xFF {
		t.Error("GetProofByBlock should return a copy")
	}

	// Non-existent block.
	if vs.GetProofByBlock(999) != nil {
		t.Error("expected nil for non-existent block")
	}
}

func TestValidityOnlyState_StateRootDigest(t *testing.T) {
	vs := NewValidityOnlyState()

	// Empty digest.
	if !vs.StateRootDigest().IsZero() {
		t.Error("expected zero digest for empty state")
	}

	vs.AddValidityProof(makeTestProof(1))
	d1 := vs.StateRootDigest()
	if d1.IsZero() {
		t.Error("expected non-zero digest after adding proof")
	}

	vs.AddValidityProof(makeTestProof(2))
	d2 := vs.StateRootDigest()
	if d2.IsZero() {
		t.Error("expected non-zero digest after adding second proof")
	}
	if d1 == d2 {
		t.Error("digest should change after adding another proof")
	}
}

func TestValidityOnlyState_VerifierTypes(t *testing.T) {
	vs := NewValidityOnlyState()

	var root types.Hash
	root[0] = 0xAA

	for _, vt := range []VerifierType{VerifierTypeSNARK, VerifierTypeSTARK, VerifierTypePlonk} {
		p := &ValidityProof{
			StateRoot:    root,
			BlockNumber:  uint64(vt) + 100,
			ProofData:    []byte{0x01, byte(vt)},
			VerifierType: vt,
		}
		if err := vs.AddValidityProof(p); err != nil {
			t.Errorf("failed to add proof with verifier type %d: %v", vt, err)
		}

		got := vs.GetProofByBlock(uint64(vt) + 100)
		if got == nil {
			t.Fatalf("expected proof for block %d", uint64(vt)+100)
		}
		if got.VerifierType != vt {
			t.Errorf("verifier type = %d, want %d", got.VerifierType, vt)
		}
	}
}

func TestValidityOnlyState_DeepCopyOnAdd(t *testing.T) {
	vs := NewValidityOnlyState()

	p := makeTestProof(1)
	vs.AddValidityProof(p)

	// Mutate the original proof data.
	p.ProofData[0] = 0xFF

	// The stored proof should not be affected.
	stored := vs.GetProofByBlock(1)
	if stored.ProofData[0] == 0xFF {
		t.Error("AddValidityProof should deep-copy proof data")
	}
}
