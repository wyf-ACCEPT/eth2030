package rollup

import (
	"crypto/sha256"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

func anchorTestGenesisRoot() types.Hash {
	var h types.Hash
	h[0] = 0xaa
	h[31] = 0xbb
	return h
}

func anchorTestNewRoot() types.Hash {
	var h types.Hash
	h[0] = 0xcc
	h[31] = 0xdd
	return h
}

func TestAnchorStateManagerRegister(t *testing.T) {
	mgr := NewAnchorStateManager()
	meta := AnchorMetadata{
		Name:        "TestRollup",
		ChainID:     42,
		GenesisRoot: anchorTestGenesisRoot(),
	}
	if err := mgr.RegisterAnchor(1, meta); err != nil {
		t.Fatalf("RegisterAnchor: %v", err)
	}
	if mgr.AnchorCount() != 1 {
		t.Errorf("expected 1 anchor, got %d", mgr.AnchorCount())
	}
	if mgr.ActiveCount() != 1 {
		t.Errorf("expected 1 active, got %d", mgr.ActiveCount())
	}
}

func TestAnchorStateManagerRegisterDuplicate(t *testing.T) {
	mgr := NewAnchorStateManager()
	meta := AnchorMetadata{Name: "R1", ChainID: 1, GenesisRoot: anchorTestGenesisRoot()}
	if err := mgr.RegisterAnchor(1, meta); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := mgr.RegisterAnchor(1, meta); err != ErrAnchorAlreadyExists {
		t.Errorf("expected ErrAnchorAlreadyExists, got %v", err)
	}
}

func TestAnchorStateManagerRegisterZeroID(t *testing.T) {
	mgr := NewAnchorStateManager()
	err := mgr.RegisterAnchor(0, AnchorMetadata{Name: "bad"})
	if err != ErrAnchorRollupIDZero {
		t.Errorf("expected ErrAnchorRollupIDZero, got %v", err)
	}
}

func TestAnchorStateManagerGetState(t *testing.T) {
	mgr := NewAnchorStateManager()
	genesis := anchorTestGenesisRoot()
	meta := AnchorMetadata{Name: "TestRollup", ChainID: 42, GenesisRoot: genesis}
	if err := mgr.RegisterAnchor(1, meta); err != nil {
		t.Fatalf("register: %v", err)
	}

	state, err := mgr.GetAnchorState(1)
	if err != nil {
		t.Fatalf("GetAnchorState: %v", err)
	}
	if state.StateRoot != genesis {
		t.Errorf("expected genesis root, got %x", state.StateRoot)
	}
	if state.BlockNumber != 0 {
		t.Errorf("expected block 0, got %d", state.BlockNumber)
	}
}

func TestAnchorStateManagerGetNotFound(t *testing.T) {
	mgr := NewAnchorStateManager()
	_, err := mgr.GetAnchorState(999)
	if err != ErrAnchorNotFound {
		t.Errorf("expected ErrAnchorNotFound, got %v", err)
	}
}

func TestAnchorStateManagerUpdateState(t *testing.T) {
	mgr := NewAnchorStateManager()
	genesis := anchorTestGenesisRoot()
	newRoot := anchorTestNewRoot()
	meta := AnchorMetadata{Name: "TestRollup", ChainID: 42, GenesisRoot: genesis}
	if err := mgr.RegisterAnchor(1, meta); err != nil {
		t.Fatalf("register: %v", err)
	}

	proof := MakeValidAnchorProof(genesis, newRoot, 1, 1000)
	if err := mgr.UpdateAnchorState(1, proof); err != nil {
		t.Fatalf("UpdateAnchorState: %v", err)
	}

	state, err := mgr.GetAnchorState(1)
	if err != nil {
		t.Fatalf("GetAnchorState: %v", err)
	}
	if state.StateRoot != newRoot {
		t.Errorf("expected new root, got %x", state.StateRoot)
	}
	if state.BlockNumber != 1 {
		t.Errorf("expected block 1, got %d", state.BlockNumber)
	}
	if state.TotalUpdates != 1 {
		t.Errorf("expected 1 update, got %d", state.TotalUpdates)
	}
}

func TestAnchorStateManagerUpdateNotFound(t *testing.T) {
	mgr := NewAnchorStateManager()
	proof := AnchorExecutionProof{Proof: []byte("data")}
	if err := mgr.UpdateAnchorState(999, proof); err != ErrAnchorNotFound {
		t.Errorf("expected ErrAnchorNotFound, got %v", err)
	}
}

func TestAnchorStateManagerUpdateEmptyProof(t *testing.T) {
	mgr := NewAnchorStateManager()
	if err := mgr.UpdateAnchorState(1, AnchorExecutionProof{}); err != ErrAnchorProofEmpty {
		t.Errorf("expected ErrAnchorProofEmpty, got %v", err)
	}
}

func TestAnchorStateManagerDeactivateActivate(t *testing.T) {
	mgr := NewAnchorStateManager()
	meta := AnchorMetadata{Name: "R", ChainID: 1, GenesisRoot: anchorTestGenesisRoot()}
	mgr.RegisterAnchor(1, meta)

	if err := mgr.DeactivateAnchor(1); err != nil {
		t.Fatalf("DeactivateAnchor: %v", err)
	}
	if mgr.ActiveCount() != 0 {
		t.Errorf("expected 0 active, got %d", mgr.ActiveCount())
	}

	// Updates should fail while inactive.
	proof := AnchorExecutionProof{Proof: []byte("data"), BlockNumber: 1}
	if err := mgr.UpdateAnchorState(1, proof); err != ErrAnchorRollupInactive {
		t.Errorf("expected ErrAnchorRollupInactive, got %v", err)
	}

	if err := mgr.ActivateAnchor(1); err != nil {
		t.Fatalf("ActivateAnchor: %v", err)
	}
	if mgr.ActiveCount() != 1 {
		t.Errorf("expected 1 active, got %d", mgr.ActiveCount())
	}
}

func TestAnchorStateManagerPruneStaleAnchors(t *testing.T) {
	mgr := NewAnchorStateManager()
	meta := AnchorMetadata{Name: "R1", ChainID: 1, GenesisRoot: anchorTestGenesisRoot()}
	mgr.RegisterAnchor(1, meta)
	mgr.RegisterAnchor(2, AnchorMetadata{Name: "R2", ChainID: 2, GenesisRoot: anchorTestGenesisRoot()})

	// Deactivate rollup 1 and set its last update to the past.
	mgr.DeactivateAnchor(1)
	mgr.mu.Lock()
	mgr.anchors[1].LastUpdateTime = time.Now().Add(-2 * time.Hour)
	mgr.mu.Unlock()

	pruned := mgr.PruneStaleAnchors(3600) // 1 hour maxAge
	if pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", pruned)
	}
	if mgr.AnchorCount() != 1 {
		t.Errorf("expected 1 remaining, got %d", mgr.AnchorCount())
	}
}

func TestAnchorStateManagerValidateStateTransition(t *testing.T) {
	mgr := NewAnchorStateManager()

	old := &ManagedAnchorState{RollupID: 1, StateRoot: anchorTestGenesisRoot(), BlockNumber: 0}
	newState := &ManagedAnchorState{RollupID: 1, StateRoot: anchorTestNewRoot(), BlockNumber: 1}

	proof := makeAnchorTransitionProof(old.StateRoot, newState.StateRoot)
	err := mgr.ValidateStateTransition(old, newState, proof)
	if err != nil {
		t.Fatalf("ValidateStateTransition: %v", err)
	}
}

func TestAnchorStateManagerValidateTransitionRegression(t *testing.T) {
	mgr := NewAnchorStateManager()

	old := &ManagedAnchorState{RollupID: 1, BlockNumber: 5}
	newState := &ManagedAnchorState{RollupID: 1, BlockNumber: 3}

	err := mgr.ValidateStateTransition(old, newState, []byte("proof"))
	if err != ErrAnchorStateRegression {
		t.Errorf("expected ErrAnchorStateRegression, got %v", err)
	}
}

func TestAnchorStateManagerRollupIDs(t *testing.T) {
	mgr := NewAnchorStateManager()
	mgr.RegisterAnchor(10, AnchorMetadata{Name: "A", ChainID: 1, GenesisRoot: anchorTestGenesisRoot()})
	mgr.RegisterAnchor(20, AnchorMetadata{Name: "B", ChainID: 2, GenesisRoot: anchorTestGenesisRoot()})

	ids := mgr.RollupIDs()
	if len(ids) != 2 {
		t.Fatalf("expected 2 IDs, got %d", len(ids))
	}
}

func TestAnchorMetadataRetrieval(t *testing.T) {
	mgr := NewAnchorStateManager()
	genesis := anchorTestGenesisRoot()
	mgr.RegisterAnchor(1, AnchorMetadata{Name: "TestR", ChainID: 42, GenesisRoot: genesis})

	meta, err := mgr.GetAnchorMetadata(1)
	if err != nil {
		t.Fatalf("GetAnchorMetadata: %v", err)
	}
	if meta.Name != "TestR" {
		t.Errorf("expected name 'TestR', got %q", meta.Name)
	}
	if meta.ChainID != 42 {
		t.Errorf("expected chainID 42, got %d", meta.ChainID)
	}
	if !meta.Active {
		t.Error("expected active")
	}
}

// makeAnchorTransitionProof finds proof bytes where
// SHA256(old || new || proof)[0] == byte(len(proof)).
func makeAnchorTransitionProof(oldRoot, newRoot types.Hash) []byte {
	for proofLen := 32; proofLen < 256; proofLen++ {
		proof := make([]byte, proofLen)
		copy(proof, []byte("state-transition-proof"))

		h := sha256.New()
		h.Write(oldRoot[:])
		h.Write(newRoot[:])
		h.Write(proof)
		digest := h.Sum(nil)

		if digest[0] == byte(proofLen) {
			return proof
		}
	}
	return []byte("fallback")
}
