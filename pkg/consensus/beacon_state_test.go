package consensus

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestNewFullBeaconState(t *testing.T) {
	state := NewFullBeaconState(nil)
	if state == nil {
		t.Fatal("NewFullBeaconState returned nil")
	}
	if state.Slot != 0 {
		t.Errorf("initial slot = %d, want 0", state.Slot)
	}
	if state.Epoch != 0 {
		t.Errorf("initial epoch = %d, want 0", state.Epoch)
	}
	if state.ValidatorCount() != 0 {
		t.Errorf("initial validator count = %d, want 0", state.ValidatorCount())
	}
	if state.config == nil {
		t.Error("config should not be nil")
	}
}

func TestNewFullBeaconState_CustomConfig(t *testing.T) {
	cfg := QuickSlotsConfig()
	state := NewFullBeaconState(cfg)
	if state.config.SecondsPerSlot != 6 {
		t.Errorf("seconds per slot = %d, want 6", state.config.SecondsPerSlot)
	}
	if state.config.SlotsPerEpoch != 4 {
		t.Errorf("slots per epoch = %d, want 4", state.config.SlotsPerEpoch)
	}
}

func TestFullBeaconState_AddAndGetValidator(t *testing.T) {
	state := NewFullBeaconState(DefaultConfig())

	pk1 := [48]byte{0x01}
	v1 := &ValidatorBalance{
		Pubkey:           pk1,
		EffectiveBalance: 32 * GweiPerETH,
		ActivationEpoch:  0,
		ExitEpoch:        FarFutureEpoch,
	}

	idx := state.AddValidator(v1, 32*GweiPerETH)
	if idx != 0 {
		t.Errorf("first validator index = %d, want 0", idx)
	}

	pk2 := [48]byte{0x02}
	v2 := &ValidatorBalance{
		Pubkey:           pk2,
		EffectiveBalance: 64 * GweiPerETH,
		ActivationEpoch:  0,
		ExitEpoch:        FarFutureEpoch,
	}
	idx2 := state.AddValidator(v2, 64*GweiPerETH)
	if idx2 != 1 {
		t.Errorf("second validator index = %d, want 1", idx2)
	}

	if state.ValidatorCount() != 2 {
		t.Errorf("validator count = %d, want 2", state.ValidatorCount())
	}
}

func TestFullBeaconState_GetValidatorByIndex(t *testing.T) {
	state := NewFullBeaconState(DefaultConfig())

	pk := [48]byte{0xaa}
	v := &ValidatorBalance{
		Pubkey:           pk,
		EffectiveBalance: 32 * GweiPerETH,
		ActivationEpoch:  0,
		ExitEpoch:        FarFutureEpoch,
	}
	state.AddValidator(v, 32*GweiPerETH)

	got, err := state.GetValidatorByIndex(0)
	if err != nil {
		t.Fatalf("GetValidatorByIndex(0) error: %v", err)
	}
	if got.Pubkey != pk {
		t.Error("pubkey mismatch")
	}

	// Out of bounds.
	_, err = state.GetValidatorByIndex(999)
	if err != ErrValidatorIndexBound {
		t.Errorf("expected ErrValidatorIndexBound, got %v", err)
	}
}

func TestFullBeaconState_GetValidatorByPubkey(t *testing.T) {
	state := NewFullBeaconState(DefaultConfig())

	pk := [48]byte{0xbb}
	v := &ValidatorBalance{
		Pubkey:           pk,
		EffectiveBalance: 32 * GweiPerETH,
		ActivationEpoch:  0,
		ExitEpoch:        FarFutureEpoch,
	}
	state.AddValidator(v, 32*GweiPerETH)

	got, idx, err := state.GetValidatorByPubkey(pk)
	if err != nil {
		t.Fatalf("GetValidatorByPubkey error: %v", err)
	}
	if idx != 0 {
		t.Errorf("index = %d, want 0", idx)
	}
	if got.Pubkey != pk {
		t.Error("pubkey mismatch")
	}

	// Not found.
	_, _, err = state.GetValidatorByPubkey([48]byte{0xff})
	if err != ErrValidatorNotFound {
		t.Errorf("expected ErrValidatorNotFound, got %v", err)
	}
}

func TestFullBeaconState_ActiveValidatorCount(t *testing.T) {
	state := NewFullBeaconState(DefaultConfig())

	// Active validator.
	state.AddValidator(&ValidatorBalance{
		Pubkey:          [48]byte{1},
		ActivationEpoch: 0,
		ExitEpoch:       FarFutureEpoch,
	}, 32*GweiPerETH)

	// Inactive validator (not yet activated).
	state.AddValidator(&ValidatorBalance{
		Pubkey:          [48]byte{2},
		ActivationEpoch: 100,
		ExitEpoch:       FarFutureEpoch,
	}, 32*GweiPerETH)

	// Exited validator.
	state.AddValidator(&ValidatorBalance{
		Pubkey:          [48]byte{3},
		ActivationEpoch: 0,
		ExitEpoch:       0, // already exited
	}, 32*GweiPerETH)

	if count := state.ActiveValidatorCount(); count != 1 {
		t.Errorf("active validator count = %d, want 1", count)
	}
}

func TestFullBeaconState_TotalActiveBalance(t *testing.T) {
	state := NewFullBeaconState(DefaultConfig())

	state.AddValidator(&ValidatorBalance{
		Pubkey:           [48]byte{1},
		EffectiveBalance: 32 * GweiPerETH,
		ActivationEpoch:  0,
		ExitEpoch:        FarFutureEpoch,
	}, 32*GweiPerETH)

	state.AddValidator(&ValidatorBalance{
		Pubkey:           [48]byte{2},
		EffectiveBalance: 64 * GweiPerETH,
		ActivationEpoch:  0,
		ExitEpoch:        FarFutureEpoch,
	}, 64*GweiPerETH)

	// Inactive validator should not count.
	state.AddValidator(&ValidatorBalance{
		Pubkey:           [48]byte{3},
		EffectiveBalance: 100 * GweiPerETH,
		ActivationEpoch:  999,
		ExitEpoch:        FarFutureEpoch,
	}, 100*GweiPerETH)

	expected := uint64(96) * GweiPerETH // 32 + 64
	if got := state.TotalActiveBalance(); got != expected {
		t.Errorf("total active balance = %d, want %d", got, expected)
	}
}

func TestFullBeaconState_EffectiveBalanceUpdate(t *testing.T) {
	state := NewFullBeaconState(DefaultConfig())

	state.AddValidator(&ValidatorBalance{
		Pubkey:           [48]byte{1},
		EffectiveBalance: 32 * GweiPerETH,
		ActivationEpoch:  0,
		ExitEpoch:        FarFutureEpoch,
	}, 50*GweiPerETH) // actual balance is 50 ETH

	state.AddValidator(&ValidatorBalance{
		Pubkey:           [48]byte{2},
		EffectiveBalance: 32 * GweiPerETH,
		ActivationEpoch:  0,
		ExitEpoch:        FarFutureEpoch,
	}, 3000*GweiPerETH) // actual balance exceeds max

	state.EffectiveBalanceUpdate()

	v0, _ := state.GetValidatorByIndex(0)
	if v0.EffectiveBalance != 50*GweiPerETH {
		t.Errorf("v0 effective balance = %d, want %d", v0.EffectiveBalance, 50*GweiPerETH)
	}

	v1, _ := state.GetValidatorByIndex(1)
	if v1.EffectiveBalance != MaxEffectiveBalance {
		t.Errorf("v1 effective balance = %d, want %d (max)", v1.EffectiveBalance, MaxEffectiveBalance)
	}
}

func TestFullBeaconState_ToMinimalState(t *testing.T) {
	state := NewFullBeaconState(DefaultConfig())
	state.Slot = 42
	state.Epoch = 1
	state.FinalizedCheckpoint = Checkpoint{Epoch: 0, Root: types.HexToHash("0xaa")}

	minimal := state.ToMinimalState()
	if minimal.Slot != 42 {
		t.Errorf("minimal slot = %d, want 42", minimal.Slot)
	}
	if minimal.Epoch != 1 {
		t.Errorf("minimal epoch = %d, want 1", minimal.Epoch)
	}
	if minimal.FinalizedCheckpoint.Root != types.HexToHash("0xaa") {
		t.Error("finalized checkpoint root mismatch")
	}
}

// --- RecentState tests ---

func TestRecentState_PutGet(t *testing.T) {
	rs := NewRecentState(8)

	root := types.HexToHash("0x1234")
	rs.Put(10, root)

	got, err := rs.Get(10)
	if err != nil {
		t.Fatalf("Get(10) error: %v", err)
	}
	if got != root {
		t.Errorf("root mismatch: got %s, want %s", got, root)
	}
}

func TestRecentState_NotFound(t *testing.T) {
	rs := NewRecentState(8)

	_, err := rs.Get(999)
	if err != ErrRecentSlotNotFound {
		t.Errorf("expected ErrRecentSlotNotFound, got %v", err)
	}
}

func TestRecentState_RingBufferOverwrite(t *testing.T) {
	rs := NewRecentState(4)

	// Fill buffer.
	for i := Slot(0); i < 4; i++ {
		rs.Put(i, types.HexToHash("0x00"))
	}

	if rs.Len() != 4 {
		t.Errorf("len = %d, want 4", rs.Len())
	}

	// Overwrite oldest.
	rs.Put(100, types.HexToHash("0xff"))

	if rs.Len() != 4 {
		t.Errorf("len after overwrite = %d, want 4", rs.Len())
	}

	// Slot 0 should be gone.
	_, err := rs.Get(0)
	if err != ErrRecentSlotNotFound {
		t.Error("slot 0 should have been overwritten")
	}

	// Slot 100 should be present.
	got, err := rs.Get(100)
	if err != nil {
		t.Fatalf("Get(100) error: %v", err)
	}
	if got != types.HexToHash("0xff") {
		t.Error("slot 100 root mismatch")
	}
}

func TestRecentState_DefaultSize(t *testing.T) {
	rs := NewRecentState(0) // should default
	if rs.size != DefaultRecentSlotCount {
		t.Errorf("default size = %d, want %d", rs.size, DefaultRecentSlotCount)
	}
}

// --- HistoricalState tests ---

func TestHistoricalState_PutGet(t *testing.T) {
	hs := NewHistoricalState(100)

	blockRoot := types.HexToHash("0xaabb")
	stateRoot := types.HexToHash("0xccdd")

	hs.Put(blockRoot, stateRoot)

	got, err := hs.Get(blockRoot)
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if got != stateRoot {
		t.Errorf("state root mismatch: got %s, want %s", got, stateRoot)
	}
}

func TestHistoricalState_NotFound(t *testing.T) {
	hs := NewHistoricalState(10)

	_, err := hs.Get(types.HexToHash("0xffff"))
	if err != ErrHistoricalNotFound {
		t.Errorf("expected ErrHistoricalNotFound, got %v", err)
	}
}

func TestHistoricalState_FIFOEviction(t *testing.T) {
	hs := NewHistoricalState(3)

	// Insert 3 entries.
	for i := byte(0); i < 3; i++ {
		blockRoot := types.Hash{i}
		stateRoot := types.Hash{i + 10}
		hs.Put(blockRoot, stateRoot)
	}

	if hs.Len() != 3 {
		t.Errorf("len = %d, want 3", hs.Len())
	}

	// Insert 4th -- should evict the first.
	hs.Put(types.Hash{99}, types.Hash{109})

	if hs.Len() != 3 {
		t.Errorf("len after eviction = %d, want 3", hs.Len())
	}

	// First entry should be gone.
	_, err := hs.Get(types.Hash{0})
	if err != ErrHistoricalNotFound {
		t.Error("first entry should have been evicted")
	}

	// Last entry should be present.
	got, err := hs.Get(types.Hash{99})
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if got != (types.Hash{109}) {
		t.Error("state root mismatch for new entry")
	}
}

func TestHistoricalState_UpdateInPlace(t *testing.T) {
	hs := NewHistoricalState(10)

	blockRoot := types.HexToHash("0x01")
	hs.Put(blockRoot, types.HexToHash("0xaa"))
	hs.Put(blockRoot, types.HexToHash("0xbb"))

	got, _ := hs.Get(blockRoot)
	if got != types.HexToHash("0xbb") {
		t.Error("update in place should use latest value")
	}

	if hs.Len() != 1 {
		t.Errorf("len = %d, want 1 after update in place", hs.Len())
	}
}

func TestHistoricalState_DefaultLimit(t *testing.T) {
	hs := NewHistoricalState(0)
	if hs.limit != HistoricalRootsLimit {
		t.Errorf("default limit = %d, want %d", hs.limit, HistoricalRootsLimit)
	}
}

// --- StateTransition tests ---

func TestStateTransition_Basic(t *testing.T) {
	state := NewFullBeaconState(DefaultConfig())

	block := &BeaconBlock{
		Slot:       1,
		ParentRoot: types.Hash{}, // genesis has zero parent
		StateRoot:  types.HexToHash("0xabcd"),
		BodyRoot:   types.HexToHash("0xef01"),
	}

	if err := state.StateTransition(block); err != nil {
		t.Fatalf("StateTransition failed: %v", err)
	}

	if state.Slot != 1 {
		t.Errorf("slot = %d, want 1", state.Slot)
	}
	if state.StateRoot != types.HexToHash("0xabcd") {
		t.Error("state root mismatch")
	}
	if state.LatestBlockRoot.IsZero() {
		t.Error("latest block root should be set")
	}
}

func TestStateTransition_SlotAdvance(t *testing.T) {
	cfg := DefaultConfig()
	state := NewFullBeaconState(cfg)

	// Process block at slot 1.
	block1 := &BeaconBlock{
		Slot:      1,
		StateRoot: types.HexToHash("0x01"),
		BodyRoot:  types.HexToHash("0x01"),
	}
	if err := state.StateTransition(block1); err != nil {
		t.Fatalf("block 1 failed: %v", err)
	}

	// Process block at slot 5 (skipping 2, 3, 4).
	block5 := &BeaconBlock{
		Slot:       5,
		ParentRoot: state.LatestBlockRoot,
		StateRoot:  types.HexToHash("0x05"),
		BodyRoot:   types.HexToHash("0x05"),
	}
	if err := state.StateTransition(block5); err != nil {
		t.Fatalf("block 5 failed: %v", err)
	}

	if state.Slot != 5 {
		t.Errorf("slot = %d, want 5", state.Slot)
	}
}

func TestStateTransition_EpochAdvance(t *testing.T) {
	cfg := &ConsensusConfig{
		SecondsPerSlot:    12,
		SlotsPerEpoch:     4, // small epoch for testing
		EpochsForFinality: 2,
	}
	state := NewFullBeaconState(cfg)

	// Block at slot 1 (epoch 0).
	block1 := &BeaconBlock{
		Slot:      1,
		StateRoot: types.HexToHash("0x01"),
		BodyRoot:  types.HexToHash("0x01"),
	}
	state.StateTransition(block1)

	if state.Epoch != 0 {
		t.Errorf("epoch at slot 1 = %d, want 0", state.Epoch)
	}

	// Block at slot 5 (epoch 1).
	block5 := &BeaconBlock{
		Slot:       5,
		ParentRoot: state.LatestBlockRoot,
		StateRoot:  types.HexToHash("0x05"),
		BodyRoot:   types.HexToHash("0x05"),
	}
	state.StateTransition(block5)

	if state.Epoch != 1 {
		t.Errorf("epoch at slot 5 = %d, want 1", state.Epoch)
	}
}

func TestStateTransition_NilBlock(t *testing.T) {
	state := NewFullBeaconState(DefaultConfig())

	if err := state.StateTransition(nil); err != ErrNilBeaconBlock {
		t.Errorf("expected ErrNilBeaconBlock, got %v", err)
	}
}

func TestStateTransition_SlotRegression(t *testing.T) {
	state := NewFullBeaconState(DefaultConfig())

	block := &BeaconBlock{Slot: 10, StateRoot: types.HexToHash("0x01"), BodyRoot: types.HexToHash("0x01")}
	state.StateTransition(block)

	// Block with same slot should fail.
	block2 := &BeaconBlock{Slot: 10, ParentRoot: state.LatestBlockRoot}
	if err := state.StateTransition(block2); err != ErrSlotRegression {
		t.Errorf("expected ErrSlotRegression, got %v", err)
	}

	// Block with earlier slot should fail.
	block3 := &BeaconBlock{Slot: 5, ParentRoot: state.LatestBlockRoot}
	if err := state.StateTransition(block3); err != ErrSlotRegression {
		t.Errorf("expected ErrSlotRegression, got %v", err)
	}
}

func TestStateTransition_ParentRootMismatch(t *testing.T) {
	state := NewFullBeaconState(DefaultConfig())

	block1 := &BeaconBlock{Slot: 1, StateRoot: types.HexToHash("0x01"), BodyRoot: types.HexToHash("0x01")}
	state.StateTransition(block1)

	// Block with wrong parent root should fail.
	block2 := &BeaconBlock{
		Slot:       2,
		ParentRoot: types.HexToHash("0xwrongparent"),
	}
	if err := state.StateTransition(block2); err != ErrParentRootMismatch {
		t.Errorf("expected ErrParentRootMismatch, got %v", err)
	}
}

func TestStateTransition_RecentStateTracking(t *testing.T) {
	state := NewFullBeaconState(DefaultConfig())

	block1 := &BeaconBlock{
		Slot:      1,
		StateRoot: types.HexToHash("0x01"),
		BodyRoot:  types.HexToHash("0x01"),
	}
	state.StateTransition(block1)
	root1 := state.LatestBlockRoot

	// Should be able to look up the recent slot.
	got, err := state.GetRecentBlockRoot(1)
	if err != nil {
		t.Fatalf("GetRecentBlockRoot(1) error: %v", err)
	}
	if got != root1 {
		t.Error("recent block root mismatch")
	}
}

func TestStateTransition_HistoricalStateTracking(t *testing.T) {
	state := NewFullBeaconState(DefaultConfig())

	block := &BeaconBlock{
		Slot:      1,
		StateRoot: types.HexToHash("0xabcd"),
		BodyRoot:  types.HexToHash("0x01"),
	}
	state.StateTransition(block)
	blockRoot := state.LatestBlockRoot

	stateRoot, err := state.GetHistoricalStateRoot(blockRoot)
	if err != nil {
		t.Fatalf("GetHistoricalStateRoot error: %v", err)
	}
	if stateRoot != types.HexToHash("0xabcd") {
		t.Errorf("state root = %s, want 0xabcd", stateRoot)
	}
}

func TestBlockRoot_Deterministic(t *testing.T) {
	block := &BeaconBlock{
		Slot:       42,
		ParentRoot: types.HexToHash("0xaa"),
		StateRoot:  types.HexToHash("0xbb"),
		BodyRoot:   types.HexToHash("0xcc"),
	}

	root1 := BlockRoot(block)
	root2 := BlockRoot(block)
	if root1 != root2 {
		t.Error("BlockRoot should be deterministic")
	}
	if root1.IsZero() {
		t.Error("BlockRoot should not be zero")
	}

	// Different blocks should produce different roots.
	block2 := &BeaconBlock{
		Slot:       43,
		ParentRoot: types.HexToHash("0xaa"),
		StateRoot:  types.HexToHash("0xbb"),
		BodyRoot:   types.HexToHash("0xcc"),
	}
	root3 := BlockRoot(block2)
	if root1 == root3 {
		t.Error("different blocks should produce different roots")
	}
}

// --- JustificationBits integration tests for beacon state ---

func TestBeaconState_JustificationBitsSetAndCheck(t *testing.T) {
	state := NewFullBeaconState(DefaultConfig())

	state.JustificationBits.Set(0)
	if !state.JustificationBits.IsJustified(0) {
		t.Error("bit 0 should be set")
	}
	if state.JustificationBits.IsJustified(1) {
		t.Error("bit 1 should not be set")
	}

	state.JustificationBits.Set(2)
	if !state.JustificationBits.IsJustified(2) {
		t.Error("bit 2 should be set")
	}
}

func TestBeaconState_JustificationBitsShift(t *testing.T) {
	state := NewFullBeaconState(DefaultConfig())
	state.JustificationBits.Set(0) // bit 0 set

	state.JustificationBits.Shift(1) // bit 0 -> bit 1

	if state.JustificationBits.IsJustified(0) {
		t.Error("bit 0 should be clear after shift")
	}
	if !state.JustificationBits.IsJustified(1) {
		t.Error("bit 1 should be set after shift")
	}
}

// --- Integration: StateTransition + Validators ---

func TestStateTransition_WithValidators(t *testing.T) {
	cfg := DefaultConfig()
	state := NewFullBeaconState(cfg)

	// Add some validators.
	for i := 0; i < 5; i++ {
		pk := [48]byte{byte(i)}
		v := &ValidatorBalance{
			Pubkey:           pk,
			EffectiveBalance: 32 * GweiPerETH,
			ActivationEpoch:  0,
			ExitEpoch:        FarFutureEpoch,
		}
		state.AddValidator(v, 32*GweiPerETH)
	}

	// Process a block.
	block := &BeaconBlock{
		Slot:      1,
		StateRoot: types.HexToHash("0x01"),
		BodyRoot:  types.HexToHash("0x01"),
	}
	if err := state.StateTransition(block); err != nil {
		t.Fatalf("StateTransition failed: %v", err)
	}

	// Validators should still be accessible.
	if state.ValidatorCount() != 5 {
		t.Errorf("validator count = %d, want 5", state.ValidatorCount())
	}
	if state.ActiveValidatorCount() != 5 {
		t.Errorf("active validator count = %d, want 5", state.ActiveValidatorCount())
	}

	// Update balances and recompute effective balance.
	state.mu.Lock()
	state.Balances[0] = 100 * GweiPerETH
	state.mu.Unlock()

	state.EffectiveBalanceUpdate()

	v0, _ := state.GetValidatorByIndex(0)
	if v0.EffectiveBalance != 100*GweiPerETH {
		t.Errorf("v0 effective after update = %d, want %d", v0.EffectiveBalance, 100*GweiPerETH)
	}
}
