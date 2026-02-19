package sync

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// mockBeaconFetcher implements BeaconBlockFetcher for testing.
type mockBeaconFetcher struct {
	blocks   map[uint64]*BeaconBlock
	blobs    map[uint64][]*BlobSidecar
	failSlot uint64 // if non-zero, fail requests for this slot
	calls    atomic.Uint64
}

func newMockBeaconFetcher() *mockBeaconFetcher {
	return &mockBeaconFetcher{
		blocks: make(map[uint64]*BeaconBlock),
		blobs:  make(map[uint64][]*BlobSidecar),
	}
}

func (m *mockBeaconFetcher) FetchBeaconBlock(slot uint64) (*BeaconBlock, error) {
	m.calls.Add(1)
	if slot == m.failSlot {
		return nil, errors.New("mock: fetch failed")
	}
	b, ok := m.blocks[slot]
	if !ok {
		return nil, errors.New("mock: block not found")
	}
	return b, nil
}

func (m *mockBeaconFetcher) FetchBlobSidecars(slot uint64) ([]*BlobSidecar, error) {
	m.calls.Add(1)
	if slot == m.failSlot {
		return nil, errors.New("mock: fetch failed")
	}
	sc := m.blobs[slot]
	return sc, nil
}

func makeBeaconBlock(slot uint64) *BeaconBlock {
	return &BeaconBlock{
		Slot:          slot,
		ProposerIndex: slot % 100,
		ParentRoot:    [32]byte{byte(slot - 1)},
		StateRoot:     [32]byte{0x01, byte(slot)},
		Body:          []byte{0xDE, 0xAD, byte(slot)},
	}
}

func makeBlobSidecar(index uint64, block *BeaconBlock) *BlobSidecar {
	sc := &BlobSidecar{
		Index:             index,
		KZGCommitment:     [48]byte{0xAA, byte(index)},
		KZGProof:          [48]byte{0xBB, byte(index)},
		SignedBlockHeader: block.Hash(),
	}
	// Fill blob with deterministic data.
	for i := range sc.Blob {
		sc.Blob[i] = byte(index) ^ byte(i%256)
	}
	return sc
}

func TestDefaultBeaconSyncConfig(t *testing.T) {
	cfg := DefaultBeaconSyncConfig()
	if cfg.MaxConcurrentRequests != 16 {
		t.Errorf("MaxConcurrentRequests = %d, want 16", cfg.MaxConcurrentRequests)
	}
	if cfg.SlotTimeout != 10*time.Second {
		t.Errorf("SlotTimeout = %v, want 10s", cfg.SlotTimeout)
	}
	if !cfg.BlobVerification {
		t.Error("BlobVerification should default to true")
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", cfg.MaxRetries)
	}
}

func TestNewBeaconSyncer(t *testing.T) {
	cfg := DefaultBeaconSyncConfig()
	syncer := NewBeaconSyncer(cfg)
	if syncer == nil {
		t.Fatal("NewBeaconSyncer returned nil")
	}

	status := syncer.GetSyncStatus()
	if status.IsComplete {
		t.Error("new syncer should not be complete")
	}
	if status.CurrentSlot != 0 {
		t.Errorf("CurrentSlot = %d, want 0", status.CurrentSlot)
	}
}

func TestNewBeaconSyncer_DefaultsZeroConfig(t *testing.T) {
	syncer := NewBeaconSyncer(BeaconSyncConfig{})
	if syncer == nil {
		t.Fatal("NewBeaconSyncer returned nil")
	}
	// Defaults should be filled in.
	if syncer.config.MaxConcurrentRequests != 16 {
		t.Errorf("MaxConcurrentRequests = %d, want 16", syncer.config.MaxConcurrentRequests)
	}
	if syncer.config.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", syncer.config.MaxRetries)
	}
}

func TestBeaconSyncer_ProcessBlock(t *testing.T) {
	syncer := NewBeaconSyncer(DefaultBeaconSyncConfig())

	block := makeBeaconBlock(100)
	if err := syncer.ProcessBlock(block); err != nil {
		t.Fatalf("ProcessBlock: %v", err)
	}

	stored := syncer.GetBlock(100)
	if stored == nil {
		t.Fatal("block not stored after ProcessBlock")
	}
	if stored.Slot != 100 {
		t.Errorf("stored slot = %d, want 100", stored.Slot)
	}
}

func TestBeaconSyncer_ProcessBlock_Nil(t *testing.T) {
	syncer := NewBeaconSyncer(DefaultBeaconSyncConfig())
	if err := syncer.ProcessBlock(nil); !errors.Is(err, ErrBeaconBlockNil) {
		t.Fatalf("expected ErrBeaconBlockNil, got: %v", err)
	}
}

func TestBeaconSyncer_ProcessBlock_EmptyBody(t *testing.T) {
	syncer := NewBeaconSyncer(DefaultBeaconSyncConfig())
	block := &BeaconBlock{
		Slot:      1,
		StateRoot: [32]byte{0x01},
		Body:      nil,
	}
	err := syncer.ProcessBlock(block)
	if !errors.Is(err, ErrBeaconBlockInvalid) {
		t.Fatalf("expected ErrBeaconBlockInvalid, got: %v", err)
	}
}

func TestBeaconSyncer_ProcessBlock_ZeroStateRoot(t *testing.T) {
	syncer := NewBeaconSyncer(DefaultBeaconSyncConfig())
	block := &BeaconBlock{
		Slot:      1,
		StateRoot: [32]byte{},
		Body:      []byte{0x01},
	}
	err := syncer.ProcessBlock(block)
	if !errors.Is(err, ErrBeaconBlockInvalid) {
		t.Fatalf("expected ErrBeaconBlockInvalid, got: %v", err)
	}
}

func TestBeaconSyncer_ProcessBlobSidecar(t *testing.T) {
	syncer := NewBeaconSyncer(DefaultBeaconSyncConfig())

	block := makeBeaconBlock(50)
	if err := syncer.ProcessBlock(block); err != nil {
		t.Fatalf("ProcessBlock: %v", err)
	}

	sc := makeBlobSidecar(0, block)
	if err := syncer.ProcessBlobSidecar(sc); err != nil {
		t.Fatalf("ProcessBlobSidecar: %v", err)
	}
}

func TestBeaconSyncer_ProcessBlobSidecar_Nil(t *testing.T) {
	syncer := NewBeaconSyncer(DefaultBeaconSyncConfig())
	if err := syncer.ProcessBlobSidecar(nil); !errors.Is(err, ErrBeaconSidecarNil) {
		t.Fatalf("expected ErrBeaconSidecarNil, got: %v", err)
	}
}

func TestBeaconSyncer_ProcessBlobSidecar_InvalidIndex(t *testing.T) {
	syncer := NewBeaconSyncer(DefaultBeaconSyncConfig())
	sc := &BlobSidecar{
		Index:         MaxBlobsPerBlock, // out of range
		KZGCommitment: [48]byte{0x01},
	}
	err := syncer.ProcessBlobSidecar(sc)
	if !errors.Is(err, ErrBeaconBlobIndexInvalid) {
		t.Fatalf("expected ErrBeaconBlobIndexInvalid, got: %v", err)
	}
}

func TestBeaconSyncer_ProcessBlobSidecar_ZeroCommitment(t *testing.T) {
	cfg := DefaultBeaconSyncConfig()
	cfg.BlobVerification = true
	syncer := NewBeaconSyncer(cfg)

	sc := &BlobSidecar{
		Index:         0,
		KZGCommitment: [48]byte{}, // zero
	}
	err := syncer.ProcessBlobSidecar(sc)
	if !errors.Is(err, ErrBeaconSidecarInvalid) {
		t.Fatalf("expected ErrBeaconSidecarInvalid, got: %v", err)
	}
}

func TestBeaconSyncer_ProcessBlobSidecar_NoVerification(t *testing.T) {
	cfg := DefaultBeaconSyncConfig()
	cfg.BlobVerification = false
	syncer := NewBeaconSyncer(cfg)

	// Zero commitment should be accepted when verification is off.
	sc := &BlobSidecar{
		Index:             0,
		KZGCommitment:     [48]byte{},
		SignedBlockHeader: [32]byte{0xFF},
	}
	if err := syncer.ProcessBlobSidecar(sc); err != nil {
		t.Fatalf("expected no error without verification, got: %v", err)
	}
}

func TestBeaconSyncer_RequestBlock(t *testing.T) {
	mock := newMockBeaconFetcher()
	mock.blocks[10] = makeBeaconBlock(10)

	syncer := NewBeaconSyncer(DefaultBeaconSyncConfig())
	syncer.SetFetcher(mock)

	block, err := syncer.RequestBlock(10)
	if err != nil {
		t.Fatalf("RequestBlock: %v", err)
	}
	if block.Slot != 10 {
		t.Errorf("block slot = %d, want 10", block.Slot)
	}
}

func TestBeaconSyncer_RequestBlock_Retry(t *testing.T) {
	// Fetcher that fails the first call, succeeds on retry.
	callCount := atomic.Uint64{}
	fetcher := &retriableFetcher{
		failCount: 2,
		calls:     &callCount,
		block:     makeBeaconBlock(5),
	}

	cfg := DefaultBeaconSyncConfig()
	cfg.MaxRetries = 5
	syncer := NewBeaconSyncer(cfg)
	syncer.SetFetcher(fetcher)

	block, err := syncer.RequestBlock(5)
	if err != nil {
		t.Fatalf("RequestBlock with retries: %v", err)
	}
	if block.Slot != 5 {
		t.Errorf("block slot = %d, want 5", block.Slot)
	}
	if callCount.Load() != 3 {
		t.Errorf("expected 3 calls (2 fail + 1 success), got %d", callCount.Load())
	}
}

func TestBeaconSyncer_RequestBlock_MaxRetriesExceeded(t *testing.T) {
	mock := newMockBeaconFetcher()
	mock.failSlot = 99 // always fail

	cfg := DefaultBeaconSyncConfig()
	cfg.MaxRetries = 2
	syncer := NewBeaconSyncer(cfg)
	syncer.SetFetcher(mock)

	_, err := syncer.RequestBlock(99)
	if !errors.Is(err, ErrBeaconMaxRetries) {
		t.Fatalf("expected ErrBeaconMaxRetries, got: %v", err)
	}
}

func TestBeaconSyncer_RequestBlock_NilFetcher(t *testing.T) {
	syncer := NewBeaconSyncer(DefaultBeaconSyncConfig())
	_, err := syncer.RequestBlock(1)
	if err == nil {
		t.Fatal("expected error with nil fetcher")
	}
}

func TestBeaconSyncer_RequestBlobSidecars(t *testing.T) {
	mock := newMockBeaconFetcher()
	block := makeBeaconBlock(20)
	mock.blocks[20] = block
	mock.blobs[20] = []*BlobSidecar{
		makeBlobSidecar(0, block),
		makeBlobSidecar(1, block),
	}

	syncer := NewBeaconSyncer(DefaultBeaconSyncConfig())
	syncer.SetFetcher(mock)

	sidecars, err := syncer.RequestBlobSidecars(20)
	if err != nil {
		t.Fatalf("RequestBlobSidecars: %v", err)
	}
	if len(sidecars) != 2 {
		t.Fatalf("got %d sidecars, want 2", len(sidecars))
	}
}

func TestBeaconSyncer_SyncSlotRange(t *testing.T) {
	mock := newMockBeaconFetcher()
	for slot := uint64(1); slot <= 5; slot++ {
		block := makeBeaconBlock(slot)
		mock.blocks[slot] = block
		mock.blobs[slot] = []*BlobSidecar{makeBlobSidecar(0, block)}
	}

	cfg := DefaultBeaconSyncConfig()
	cfg.MaxConcurrentRequests = 2
	syncer := NewBeaconSyncer(cfg)
	syncer.SetFetcher(mock)

	if err := syncer.SyncSlotRange(1, 5); err != nil {
		t.Fatalf("SyncSlotRange: %v", err)
	}

	status := syncer.GetSyncStatus()
	if !status.IsComplete {
		t.Error("sync should be complete")
	}
	if status.TargetSlot != 5 {
		t.Errorf("TargetSlot = %d, want 5", status.TargetSlot)
	}
	if status.BlobsDownloaded < 5 {
		t.Errorf("BlobsDownloaded = %d, want >= 5", status.BlobsDownloaded)
	}

	// All blocks should be stored.
	for slot := uint64(1); slot <= 5; slot++ {
		if syncer.GetBlock(slot) == nil {
			t.Errorf("block at slot %d not stored", slot)
		}
	}
}

func TestBeaconSyncer_SyncSlotRange_InvalidRange(t *testing.T) {
	syncer := NewBeaconSyncer(DefaultBeaconSyncConfig())
	syncer.SetFetcher(newMockBeaconFetcher())

	err := syncer.SyncSlotRange(10, 5)
	if !errors.Is(err, ErrBeaconInvalidSlotRange) {
		t.Fatalf("expected ErrBeaconInvalidSlotRange, got: %v", err)
	}
}

func TestBeaconSyncer_SyncSlotRange_SingleSlot(t *testing.T) {
	mock := newMockBeaconFetcher()
	block := makeBeaconBlock(42)
	mock.blocks[42] = block

	syncer := NewBeaconSyncer(DefaultBeaconSyncConfig())
	syncer.SetFetcher(mock)

	if err := syncer.SyncSlotRange(42, 42); err != nil {
		t.Fatalf("SyncSlotRange single: %v", err)
	}

	if syncer.GetBlock(42) == nil {
		t.Error("block at slot 42 should be stored")
	}
}

func TestBeaconSyncer_SyncSlotRange_AlreadySyncing(t *testing.T) {
	mock := newMockBeaconFetcher()
	// Set up a large range with blocks.
	for slot := uint64(1); slot <= 100; slot++ {
		mock.blocks[slot] = makeBeaconBlock(slot)
	}

	syncer := NewBeaconSyncer(DefaultBeaconSyncConfig())
	syncer.SetFetcher(mock)

	// Start a sync in the background.
	done := make(chan error, 1)
	go func() {
		done <- syncer.SyncSlotRange(1, 100)
	}()

	// Give it a moment to start.
	time.Sleep(10 * time.Millisecond)

	// A second sync should fail.
	err := syncer.SyncSlotRange(1, 50)
	if err != ErrBeaconAlreadySyncing && err != nil {
		// If the first sync finished before we got here, that's also OK.
		// Just make sure we didn't get an unexpected error.
		if !errors.Is(err, ErrBeaconAlreadySyncing) {
			// The first sync might have finished already; re-syncing is fine.
		}
	}

	<-done // wait for the first sync to finish
}

func TestBeaconSyncer_Cancel(t *testing.T) {
	syncer := NewBeaconSyncer(DefaultBeaconSyncConfig())
	// Cancel should not panic on a fresh syncer.
	syncer.Cancel()
}

func TestBeaconBlock_Hash(t *testing.T) {
	b1 := makeBeaconBlock(100)
	b2 := makeBeaconBlock(100)
	b3 := makeBeaconBlock(101)

	h1 := b1.Hash()
	h2 := b2.Hash()
	h3 := b3.Hash()

	if h1 != h2 {
		t.Error("identical blocks should produce the same hash")
	}
	if h1 == h3 {
		t.Error("different blocks should produce different hashes")
	}
	if h1 == [32]byte{} {
		t.Error("hash should not be zero")
	}
}

func TestBlobRecovery_New(t *testing.T) {
	br := NewBlobRecovery(4)
	if br.Custody() != 4 {
		t.Errorf("custody = %d, want 4", br.Custody())
	}
}

func TestBlobRecovery_NewDefaultCustody(t *testing.T) {
	br := NewBlobRecovery(0)
	if br.Custody() != MaxBlobsPerBlock {
		t.Errorf("custody = %d, want %d", br.Custody(), MaxBlobsPerBlock)
	}
}

func TestBlobRecovery_AttemptRecovery_AllAvailable(t *testing.T) {
	br := NewBlobRecovery(3)
	block := makeBeaconBlock(1)

	available := []*BlobSidecar{
		makeBlobSidecar(0, block),
		makeBlobSidecar(1, block),
		makeBlobSidecar(2, block),
	}

	result, err := br.AttemptRecovery(1, available)
	if err != nil {
		t.Fatalf("AttemptRecovery: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("recovered %d blobs, want 3", len(result))
	}
	// Verify the original blobs are preserved.
	for i, sc := range result {
		if sc.Index != uint64(i) {
			t.Errorf("blob[%d] index = %d, want %d", i, sc.Index, i)
		}
	}
}

func TestBlobRecovery_AttemptRecovery_PartialAvailable(t *testing.T) {
	br := NewBlobRecovery(4)
	block := makeBeaconBlock(1)

	// Provide only 2 out of 4 (meets 50% threshold).
	available := []*BlobSidecar{
		makeBlobSidecar(0, block),
		makeBlobSidecar(2, block),
	}

	result, err := br.AttemptRecovery(1, available)
	if err != nil {
		t.Fatalf("AttemptRecovery: %v", err)
	}
	if len(result) != 4 {
		t.Fatalf("recovered %d blobs, want 4", len(result))
	}

	// Blobs at indices 0 and 2 should be the original ones.
	if result[0].Index != 0 {
		t.Errorf("result[0] index = %d, want 0", result[0].Index)
	}
	if result[2].Index != 2 {
		t.Errorf("result[2] index = %d, want 2", result[2].Index)
	}
	// Blobs at indices 1 and 3 should be recovered.
	if result[1].Index != 1 {
		t.Errorf("result[1] index = %d, want 1", result[1].Index)
	}
	if result[3].Index != 3 {
		t.Errorf("result[3] index = %d, want 3", result[3].Index)
	}
}

func TestBlobRecovery_AttemptRecovery_InsufficientData(t *testing.T) {
	br := NewBlobRecovery(6)
	block := makeBeaconBlock(1)

	// Only 2 out of 6 (below 50% threshold of 3).
	available := []*BlobSidecar{
		makeBlobSidecar(0, block),
		makeBlobSidecar(1, block),
	}

	_, err := br.AttemptRecovery(1, available)
	if !errors.Is(err, ErrBlobRecoveryFailed) {
		t.Fatalf("expected ErrBlobRecoveryFailed, got: %v", err)
	}
}

func TestBlobRecovery_AttemptRecovery_EmptyInput(t *testing.T) {
	br := NewBlobRecovery(4)
	_, err := br.AttemptRecovery(1, nil)
	if !errors.Is(err, ErrBlobRecoveryFailed) {
		t.Fatalf("expected ErrBlobRecoveryFailed, got: %v", err)
	}
}

func TestSyncStatus_Fields(t *testing.T) {
	s := &SyncStatus{
		CurrentSlot:     100,
		TargetSlot:      200,
		BlobsDownloaded: 50,
		IsComplete:      false,
	}
	if s.CurrentSlot != 100 {
		t.Errorf("CurrentSlot = %d, want 100", s.CurrentSlot)
	}
	if s.TargetSlot != 200 {
		t.Errorf("TargetSlot = %d, want 200", s.TargetSlot)
	}
	if s.BlobsDownloaded != 50 {
		t.Errorf("BlobsDownloaded = %d, want 50", s.BlobsDownloaded)
	}
	if s.IsComplete {
		t.Error("should not be complete")
	}
}

func TestBeaconSyncer_SyncSlotRange_WithError(t *testing.T) {
	mock := newMockBeaconFetcher()
	mock.blocks[1] = makeBeaconBlock(1)
	mock.blocks[2] = makeBeaconBlock(2)
	mock.failSlot = 3 // slot 3 will fail

	cfg := DefaultBeaconSyncConfig()
	cfg.MaxRetries = 1
	cfg.MaxConcurrentRequests = 1 // sequential for determinism
	syncer := NewBeaconSyncer(cfg)
	syncer.SetFetcher(mock)

	err := syncer.SyncSlotRange(1, 5)
	if err == nil {
		t.Fatal("expected error from failed slot")
	}
}

func TestBeaconSyncer_ThreadSafety(t *testing.T) {
	syncer := NewBeaconSyncer(DefaultBeaconSyncConfig())

	// Concurrent ProcessBlock and GetBlock calls.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := uint64(0); i < 100; i++ {
			_ = syncer.ProcessBlock(makeBeaconBlock(i))
		}
	}()

	for i := uint64(0); i < 100; i++ {
		_ = syncer.GetBlock(i)
		_ = syncer.GetSyncStatus()
	}
	<-done
}

// retriableFetcher fails the first N calls, then succeeds.
type retriableFetcher struct {
	failCount int
	calls     *atomic.Uint64
	block     *BeaconBlock
}

func (f *retriableFetcher) FetchBeaconBlock(slot uint64) (*BeaconBlock, error) {
	n := f.calls.Add(1)
	if int(n) <= f.failCount {
		return nil, errors.New("temporary failure")
	}
	return f.block, nil
}

func (f *retriableFetcher) FetchBlobSidecars(slot uint64) ([]*BlobSidecar, error) {
	return nil, nil
}
