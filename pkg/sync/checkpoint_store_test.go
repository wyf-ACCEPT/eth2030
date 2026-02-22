package sync

import (
	"sync"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

func makeTrustedCheckpoint(epoch, blockNum uint64, source string) TrustedCheckpoint {
	return TrustedCheckpoint{
		Epoch:       epoch,
		BlockNumber: blockNum,
		BlockHash:   types.BytesToHash([]byte{byte(epoch), byte(blockNum), 0xaa, 0xbb}),
		StateRoot:   types.BytesToHash([]byte{byte(epoch), byte(blockNum), 0xcc, 0xdd}),
		Source:      source,
	}
}

func TestTrustedCheckpoint_Validate(t *testing.T) {
	cp := makeTrustedCheckpoint(10, 320, "embedded")
	if err := cp.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Zero block hash.
	bad := TrustedCheckpoint{BlockNumber: 100, StateRoot: types.HexToHash("0xab")}
	if err := bad.Validate(); err == nil {
		t.Fatal("expected error for zero block hash")
	}
	// Zero state root.
	bad2 := TrustedCheckpoint{BlockNumber: 100, BlockHash: types.HexToHash("0xab")}
	if err := bad2.Validate(); err == nil {
		t.Fatal("expected error for zero state root")
	}
	// Zero block number.
	bad3 := TrustedCheckpoint{BlockHash: types.HexToHash("0xab"), StateRoot: types.HexToHash("0xcd")}
	if err := bad3.Validate(); err == nil {
		t.Fatal("expected error for zero block number")
	}
	// Inconsistent epoch.
	bad4 := TrustedCheckpoint{Epoch: 100, BlockNumber: 10, BlockHash: types.HexToHash("0xab"), StateRoot: types.HexToHash("0xcd")}
	if err := bad4.Validate(); err == nil {
		t.Fatal("expected error for inconsistent epoch/block")
	}
	// Zero epoch is valid.
	ok := TrustedCheckpoint{Epoch: 0, BlockNumber: 100, BlockHash: types.HexToHash("0xab"), StateRoot: types.HexToHash("0xcd")}
	if err := ok.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTrustedCheckpoint_CheckpointID(t *testing.T) {
	cp1 := makeTrustedCheckpoint(10, 320, "embedded")
	cp2 := makeTrustedCheckpoint(10, 320, "api")
	cp3 := makeTrustedCheckpoint(11, 352, "embedded")
	if cp1.CheckpointID() != cp2.CheckpointID() {
		t.Error("same data should produce same ID regardless of source")
	}
	if cp1.CheckpointID() == cp3.CheckpointID() {
		t.Error("different checkpoints should produce different IDs")
	}
	if cp1.CheckpointID().IsZero() {
		t.Error("checkpoint ID should not be zero")
	}
}

func TestCheckpointStore_RegisterAndGet(t *testing.T) {
	store := NewCheckpointStore(DefaultCheckpointStoreConfig())
	cp := makeTrustedCheckpoint(10, 320, "embedded")
	if err := store.RegisterCheckpoint(cp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.CheckpointCount() != 1 {
		t.Fatalf("expected 1 checkpoint, got %d", store.CheckpointCount())
	}
	got, err := store.GetCheckpoint(cp.CheckpointID())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.BlockNumber != 320 || got.Epoch != 10 {
		t.Errorf("mismatch: epoch=%d block=%d", got.Epoch, got.BlockNumber)
	}
}

func TestCheckpointStore_RegisterDuplicate(t *testing.T) {
	store := NewCheckpointStore(DefaultCheckpointStoreConfig())
	cp := makeTrustedCheckpoint(10, 320, "embedded")
	store.RegisterCheckpoint(cp)
	if err := store.RegisterCheckpoint(cp); err != ErrStoreCheckpointExists {
		t.Fatalf("expected ErrStoreCheckpointExists, got %v", err)
	}
}

func TestCheckpointStore_RegisterInvalid(t *testing.T) {
	store := NewCheckpointStore(DefaultCheckpointStoreConfig())
	cp := TrustedCheckpoint{BlockNumber: 0, BlockHash: types.HexToHash("0xab"), StateRoot: types.HexToHash("0xcd")}
	if err := store.RegisterCheckpoint(cp); err == nil {
		t.Fatal("expected error for invalid checkpoint")
	}
}

func TestCheckpointStore_GetLatestCheckpoint(t *testing.T) {
	store := NewCheckpointStore(DefaultCheckpointStoreConfig())
	if store.GetLatestCheckpoint() != nil {
		t.Fatal("expected nil for empty store")
	}
	store.RegisterCheckpoint(makeTrustedCheckpoint(10, 320, "embedded"))
	store.RegisterCheckpoint(makeTrustedCheckpoint(20, 640, "api"))
	latest := store.GetLatestCheckpoint()
	if latest == nil || latest.BlockNumber != 640 {
		t.Fatalf("expected latest block 640, got %v", latest)
	}
}

func TestCheckpointStore_GetHighestCheckpoint(t *testing.T) {
	store := NewCheckpointStore(DefaultCheckpointStoreConfig())
	if store.GetHighestCheckpoint() != nil {
		t.Fatal("expected nil for empty store")
	}
	store.RegisterCheckpoint(makeTrustedCheckpoint(20, 640, "api"))
	store.RegisterCheckpoint(makeTrustedCheckpoint(10, 320, "embedded"))
	store.RegisterCheckpoint(makeTrustedCheckpoint(30, 960, "manual"))
	highest := store.GetHighestCheckpoint()
	if highest == nil || highest.BlockNumber != 960 {
		t.Fatalf("expected highest 960, got %v", highest)
	}
}

func TestCheckpointStore_ListCheckpoints(t *testing.T) {
	store := NewCheckpointStore(DefaultCheckpointStoreConfig())
	for i := uint64(1); i <= 5; i++ {
		store.RegisterCheckpoint(makeTrustedCheckpoint(i, i*32, "embedded"))
	}
	list := store.ListCheckpoints()
	if len(list) != 5 {
		t.Fatalf("expected 5, got %d", len(list))
	}
	for i, cp := range list {
		if cp.BlockNumber != uint64(i+1)*32 {
			t.Errorf("index %d: expected block %d, got %d", i, (i+1)*32, cp.BlockNumber)
		}
	}
}

func TestCheckpointStore_CheckpointsBySource(t *testing.T) {
	store := NewCheckpointStore(DefaultCheckpointStoreConfig())
	store.RegisterCheckpoint(makeTrustedCheckpoint(1, 32, "embedded"))
	store.RegisterCheckpoint(makeTrustedCheckpoint(2, 64, "api"))
	store.RegisterCheckpoint(makeTrustedCheckpoint(3, 96, "embedded"))
	if len(store.CheckpointsBySource("embedded")) != 2 {
		t.Fatal("expected 2 embedded checkpoints")
	}
	if len(store.CheckpointsBySource("api")) != 1 {
		t.Fatal("expected 1 api checkpoint")
	}
	if len(store.CheckpointsBySource("unknown")) != 0 {
		t.Fatal("expected 0 unknown checkpoints")
	}
}

func TestCheckpointStore_Eviction(t *testing.T) {
	config := DefaultCheckpointStoreConfig()
	config.MaxCheckpoints = 3
	store := NewCheckpointStore(config)
	for i := uint64(1); i <= 5; i++ {
		store.RegisterCheckpoint(makeTrustedCheckpoint(i, i*32, "embedded"))
	}
	if store.CheckpointCount() != 3 {
		t.Fatalf("expected 3 after eviction, got %d", store.CheckpointCount())
	}
	_, err := store.GetCheckpoint(makeTrustedCheckpoint(1, 32, "embedded").CheckpointID())
	if err != ErrStoreCheckpointUnknown {
		t.Fatal("expected evicted checkpoint to be unknown")
	}
}

func TestCheckpointStore_RemoveCheckpoint(t *testing.T) {
	store := NewCheckpointStore(DefaultCheckpointStoreConfig())
	cp := makeTrustedCheckpoint(10, 320, "embedded")
	store.RegisterCheckpoint(cp)
	id := cp.CheckpointID()
	if err := store.RemoveCheckpoint(id); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.CheckpointCount() != 0 {
		t.Fatalf("expected 0, got %d", store.CheckpointCount())
	}
	if err := store.RemoveCheckpoint(id); err != ErrStoreCheckpointUnknown {
		t.Fatalf("expected ErrStoreCheckpointUnknown, got %v", err)
	}
}

func TestCheckpointStore_StartSync(t *testing.T) {
	store := NewCheckpointStore(DefaultCheckpointStoreConfig())
	cp := makeTrustedCheckpoint(10, 320, "embedded")
	if err := store.StartSync(cp, 10000); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.State() != StateCheckpointDownloadingHeaders {
		t.Fatalf("expected downloading_headers, got %s", store.State())
	}
	p := store.Progress()
	if p.CurrentBlock != 320 || p.HighestBlock != 10000 || p.StartedAt.IsZero() {
		t.Errorf("unexpected progress: %+v", p)
	}
}

func TestCheckpointStore_StartSync_AlreadyActive(t *testing.T) {
	store := NewCheckpointStore(DefaultCheckpointStoreConfig())
	cp := makeTrustedCheckpoint(10, 320, "embedded")
	store.StartSync(cp, 10000)
	if err := store.StartSync(cp, 20000); err != ErrStoreSyncActive {
		t.Fatalf("expected ErrStoreSyncActive, got %v", err)
	}
}

func TestCheckpointStore_StartSync_TargetBelowCheckpoint(t *testing.T) {
	store := NewCheckpointStore(DefaultCheckpointStoreConfig())
	cp := makeTrustedCheckpoint(10, 320, "embedded")
	store.StartSync(cp, 320)
	if store.State() != StateCheckpointComplete {
		t.Fatalf("expected complete, got %s", store.State())
	}
	if store.Progress().Percentage != 100.0 {
		t.Errorf("expected 100%%, got %.2f%%", store.Progress().Percentage)
	}
}

func TestCheckpointStore_UpdateProgress(t *testing.T) {
	store := NewCheckpointStore(DefaultCheckpointStoreConfig())
	cp := makeTrustedCheckpoint(10, 320, "embedded")
	store.StartSync(cp, 1320)
	store.UpdateProgress(820, 500, 0, 0)
	p := store.Progress()
	if p.CurrentBlock != 820 || p.HeadersDown != 500 {
		t.Errorf("unexpected: current=%d headers=%d", p.CurrentBlock, p.HeadersDown)
	}
	if p.Percentage < 49.9 || p.Percentage > 50.1 {
		t.Errorf("expected ~50%%, got %.2f%%", p.Percentage)
	}
	if p.ETA <= 0 {
		t.Error("expected positive ETA")
	}
	store.UpdateProgress(1320, 1000, 1000, 1000)
	if store.Progress().Percentage != 100.0 || store.State() != StateCheckpointComplete {
		t.Fatal("expected 100% complete")
	}
}

func TestCheckpointStore_TransitionState(t *testing.T) {
	store := NewCheckpointStore(DefaultCheckpointStoreConfig())
	if err := store.TransitionState(StateCheckpointDownloadingBodies); err == nil {
		t.Fatal("expected error when idle")
	}
	cp := makeTrustedCheckpoint(10, 320, "embedded")
	store.StartSync(cp, 10000)
	if err := store.TransitionState(StateCheckpointDownloadingBodies); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.State() != StateCheckpointDownloadingBodies {
		t.Fatalf("expected downloading_bodies, got %s", store.State())
	}
}

func TestCheckpointStore_Reset(t *testing.T) {
	store := NewCheckpointStore(DefaultCheckpointStoreConfig())
	cp := makeTrustedCheckpoint(10, 320, "embedded")
	store.StartSync(cp, 10000)
	store.CreateRangeRequest(321, 500, "peer1")
	store.Reset()
	if store.State() != StateCheckpointIdle {
		t.Fatalf("expected idle, got %s", store.State())
	}
	if store.ActiveCheckpoint() != nil || store.PendingRangeRequests() != 0 {
		t.Fatal("expected clean state after reset")
	}
}

func TestCheckpointStore_RangeRequests(t *testing.T) {
	store := NewCheckpointStore(DefaultCheckpointStoreConfig())
	id, err := store.CreateRangeRequest(100, 200, "peer1")
	if err != nil || id == 0 {
		t.Fatalf("unexpected: id=%d err=%v", id, err)
	}
	if store.PendingRangeRequests() != 1 {
		t.Fatalf("expected 1 pending, got %d", store.PendingRangeRequests())
	}
	req, _ := store.GetRangeRequest(id)
	if req.From != 100 || req.To != 200 || req.Count() != 101 {
		t.Errorf("range mismatch: %+v", req)
	}
	store.CompleteRangeRequest(id, 101)
	if store.PendingRangeRequests() != 0 || store.CompletedRangeRequests() != 1 {
		t.Fatal("unexpected range counts")
	}
}

func TestCheckpointStore_RangeRequestOverlap(t *testing.T) {
	store := NewCheckpointStore(DefaultCheckpointStoreConfig())
	store.CreateRangeRequest(100, 200, "peer1")
	if _, err := store.CreateRangeRequest(150, 250, "peer2"); err != ErrStoreRangeOverlap {
		t.Fatalf("expected overlap error, got %v", err)
	}
	if _, err := store.CreateRangeRequest(201, 300, "peer2"); err != nil {
		t.Fatalf("non-overlapping should succeed: %v", err)
	}
}

func TestCheckpointStore_RangeRequestLimit(t *testing.T) {
	config := DefaultCheckpointStoreConfig()
	config.MaxPendingRanges = 2
	store := NewCheckpointStore(config)
	store.CreateRangeRequest(100, 200, "peer1")
	store.CreateRangeRequest(201, 300, "peer2")
	if _, err := store.CreateRangeRequest(301, 400, "peer3"); err != ErrStoreTooManyPending {
		t.Fatalf("expected too many pending, got %v", err)
	}
}

func TestCheckpointStore_RangeRequestInvalid(t *testing.T) {
	store := NewCheckpointStore(DefaultCheckpointStoreConfig())
	if _, err := store.CreateRangeRequest(0, 100, "p"); err == nil {
		t.Fatal("expected error for from=0")
	}
	if _, err := store.CreateRangeRequest(200, 100, "p"); err == nil {
		t.Fatal("expected error for to < from")
	}
}

func TestCheckpointStore_VerifyCheckpoint(t *testing.T) {
	store := NewCheckpointStore(DefaultCheckpointStoreConfig())
	cp := makeTrustedCheckpoint(10, 320, "api")
	if err := store.VerifyCheckpoint(cp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.CheckpointCount() != 1 {
		t.Fatalf("expected 1, got %d", store.CheckpointCount())
	}
	bad := TrustedCheckpoint{BlockNumber: 0}
	if err := store.VerifyCheckpoint(bad); err == nil {
		t.Fatal("expected error for invalid checkpoint")
	}
}

func TestSyncState_String(t *testing.T) {
	tests := []struct {
		state SyncState
		want  string
	}{
		{StateCheckpointIdle, "idle"},
		{StateCheckpointDownloadingHeaders, "downloading_headers"},
		{StateCheckpointComplete, "complete"},
		{SyncState(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("SyncState(%d) = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestCheckpointStore_ActiveCheckpoint(t *testing.T) {
	store := NewCheckpointStore(DefaultCheckpointStoreConfig())
	if store.ActiveCheckpoint() != nil {
		t.Fatal("expected nil initially")
	}
	cp := makeTrustedCheckpoint(10, 320, "embedded")
	store.StartSync(cp, 10000)
	active := store.ActiveCheckpoint()
	if active == nil || active.BlockNumber != 320 {
		t.Fatalf("unexpected active: %v", active)
	}
}

func TestCheckpointStore_ConcurrentAccess(t *testing.T) {
	store := NewCheckpointStore(DefaultCheckpointStoreConfig())
	cp := makeTrustedCheckpoint(10, 320, "embedded")
	store.StartSync(cp, 10000)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(3)
		bn := uint64(320 + i*10)
		go func(b uint64) { defer wg.Done(); store.UpdateProgress(b, b-320, 0, 0) }(bn)
		go func() { defer wg.Done(); _ = store.Progress() }()
		go func() { defer wg.Done(); _ = store.State() }()
	}
	wg.Wait()
}

func TestCheckpointStore_AddedAtTimestamp(t *testing.T) {
	store := NewCheckpointStore(DefaultCheckpointStoreConfig())
	before := time.Now()
	cp := makeTrustedCheckpoint(10, 320, "embedded")
	store.RegisterCheckpoint(cp)
	after := time.Now()
	got, _ := store.GetCheckpoint(cp.CheckpointID())
	if got.AddedAt.Before(before) || got.AddedAt.After(after) {
		t.Errorf("AddedAt %v not in [%v, %v]", got.AddedAt, before, after)
	}
}

func TestHeaderRangeRequest_Count(t *testing.T) {
	if (HeaderRangeRequest{From: 100, To: 200}).Count() != 101 {
		t.Error("expected 101")
	}
	if (HeaderRangeRequest{From: 100, To: 100}).Count() != 1 {
		t.Error("expected 1")
	}
}
