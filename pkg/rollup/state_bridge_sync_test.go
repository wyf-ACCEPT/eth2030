package rollup

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestSyncEngineCreateCheckpoint(t *testing.T) {
	se := NewSyncEngine(DefaultSyncEngineConfig())

	l1Root := types.HexToHash("0xaa")
	l2Root := types.HexToHash("0xbb")

	cp, err := se.CreateCheckpoint(100, 1000, l1Root, l2Root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cp.L1Block != 100 {
		t.Errorf("L1Block = %d, want 100", cp.L1Block)
	}
	if cp.L2Block != 1000 {
		t.Errorf("L2Block = %d, want 1000", cp.L2Block)
	}
	if cp.L1StateRoot != l1Root {
		t.Errorf("L1StateRoot mismatch")
	}
	if cp.L2StateRoot != l2Root {
		t.Errorf("L2StateRoot mismatch")
	}
	if cp.Commitment.IsZero() {
		t.Error("Commitment should not be zero")
	}
	if cp.Finalized {
		t.Error("checkpoint should not be finalized initially")
	}
}

func TestSyncEngineCheckpointBlockRegression(t *testing.T) {
	se := NewSyncEngine(DefaultSyncEngineConfig())

	l1Root := types.HexToHash("0xaa")
	l2Root := types.HexToHash("0xbb")

	_, err := se.CreateCheckpoint(100, 1000, l1Root, l2Root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Try to create a checkpoint with a regressed L2 block.
	_, err = se.CreateCheckpoint(101, 500, l1Root, l2Root)
	if err == nil {
		t.Fatal("expected error for L2 block regression, got nil")
	}
}

func TestSyncEngineL1Regression(t *testing.T) {
	se := NewSyncEngine(DefaultSyncEngineConfig())

	l1Root := types.HexToHash("0xaa")
	l2Root := types.HexToHash("0xbb")

	_, err := se.CreateCheckpoint(200, 1000, l1Root, l2Root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// L1 regression should fail.
	_, err = se.CreateCheckpoint(100, 2000, l1Root, l2Root)
	if err == nil {
		t.Fatal("expected error for L1 regression, got nil")
	}
}

func TestSyncEngineFinalizeCheckpoints(t *testing.T) {
	config := SyncEngineConfig{
		FinalizationDepth: 10,
		MaxJournalEntries: 100,
	}
	se := NewSyncEngine(config)

	l1Root := types.HexToHash("0xaa")
	l2Root := types.HexToHash("0xbb")

	_, err := se.CreateCheckpoint(100, 1000, l1Root, l2Root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Not enough depth yet.
	count := se.FinalizeCheckpoints(105)
	if count != 0 {
		t.Errorf("finalized %d, expected 0", count)
	}

	// Now sufficient depth.
	count = se.FinalizeCheckpoints(110)
	if count != 1 {
		t.Errorf("finalized %d, expected 1", count)
	}

	// Already finalized, should not re-count.
	count = se.FinalizeCheckpoints(120)
	if count != 0 {
		t.Errorf("finalized %d, expected 0 (already finalized)", count)
	}
}

func TestSyncEngineGetCheckpoint(t *testing.T) {
	se := NewSyncEngine(DefaultSyncEngineConfig())

	// Non-existent should return nil.
	cp := se.GetCheckpoint(999)
	if cp != nil {
		t.Fatal("expected nil for non-existent checkpoint")
	}

	l1Root := types.HexToHash("0xaa")
	l2Root := types.HexToHash("0xbb")

	_, err := se.CreateCheckpoint(100, 1000, l1Root, l2Root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cp = se.GetCheckpoint(1000)
	if cp == nil {
		t.Fatal("expected checkpoint, got nil")
	}
	if cp.L2Block != 1000 {
		t.Errorf("L2Block = %d, want 1000", cp.L2Block)
	}
}

func TestSyncEngineLatestFinalizedCheckpoint(t *testing.T) {
	config := SyncEngineConfig{
		FinalizationDepth: 5,
		MaxJournalEntries: 100,
	}
	se := NewSyncEngine(config)

	// No checkpoints at all.
	if se.LatestFinalizedCheckpoint() != nil {
		t.Fatal("expected nil when no checkpoints exist")
	}

	l1Root := types.HexToHash("0xaa")
	l2Root := types.HexToHash("0xbb")

	_, _ = se.CreateCheckpoint(10, 100, l1Root, l2Root)
	_, _ = se.CreateCheckpoint(20, 200, types.HexToHash("0xcc"), types.HexToHash("0xdd"))

	// Finalize only the first.
	se.FinalizeCheckpoints(15)

	latest := se.LatestFinalizedCheckpoint()
	if latest == nil {
		t.Fatal("expected finalized checkpoint")
	}
	if latest.L2Block != 100 {
		t.Errorf("L2Block = %d, want 100", latest.L2Block)
	}

	// Finalize both.
	se.FinalizeCheckpoints(25)
	latest = se.LatestFinalizedCheckpoint()
	if latest.L2Block != 200 {
		t.Errorf("L2Block = %d, want 200", latest.L2Block)
	}
}

func TestVerifySyncCheckpoint(t *testing.T) {
	l1Root := types.HexToHash("0xaa")
	l2Root := types.HexToHash("0xbb")

	commitment := computeSyncCommitment(100, 1000, l1Root, l2Root)

	cp := &SyncCheckpoint{
		L1Block:     100,
		L2Block:     1000,
		L1StateRoot: l1Root,
		L2StateRoot: l2Root,
		Commitment:  commitment,
	}

	valid, err := VerifySyncCheckpoint(cp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !valid {
		t.Error("checkpoint should be valid")
	}

	// Tamper with the commitment.
	cp.Commitment = types.HexToHash("0xff")
	valid, err = VerifySyncCheckpoint(cp)
	if err == nil {
		t.Error("expected error for tampered commitment")
	}
	if valid {
		t.Error("tampered checkpoint should not be valid")
	}

	// Nil checkpoint.
	_, err = VerifySyncCheckpoint(nil)
	if err == nil {
		t.Error("expected error for nil checkpoint")
	}
}

func TestSyncEngineDetectDivergence(t *testing.T) {
	se := NewSyncEngine(DefaultSyncEngineConfig())

	l1Root := types.HexToHash("0xaa")
	l2Root := types.HexToHash("0xbb")

	_, _ = se.CreateCheckpoint(100, 1000, l1Root, l2Root)

	// Same root should not diverge.
	if se.DetectDivergence(1000, l2Root) {
		t.Error("same root should not indicate divergence")
	}

	// Different root should diverge.
	if !se.DetectDivergence(1000, types.HexToHash("0xcc")) {
		t.Error("different root should indicate divergence")
	}

	// Non-existent block should not diverge.
	if se.DetectDivergence(9999, types.HexToHash("0xcc")) {
		t.Error("non-existent block should not indicate divergence")
	}
}

func TestSyncEngineCheckpointAndFinalizedCount(t *testing.T) {
	config := SyncEngineConfig{
		FinalizationDepth: 5,
		MaxJournalEntries: 100,
	}
	se := NewSyncEngine(config)

	if se.CheckpointCount() != 0 {
		t.Errorf("expected 0 checkpoints, got %d", se.CheckpointCount())
	}
	if se.FinalizedCount() != 0 {
		t.Errorf("expected 0 finalized, got %d", se.FinalizedCount())
	}

	_, _ = se.CreateCheckpoint(10, 100, types.HexToHash("0x01"), types.HexToHash("0x02"))
	_, _ = se.CreateCheckpoint(20, 200, types.HexToHash("0x03"), types.HexToHash("0x04"))

	if se.CheckpointCount() != 2 {
		t.Errorf("expected 2 checkpoints, got %d", se.CheckpointCount())
	}

	se.FinalizeCheckpoints(15)
	if se.FinalizedCount() != 1 {
		t.Errorf("expected 1 finalized, got %d", se.FinalizedCount())
	}
}

func TestSyncEngineJournal(t *testing.T) {
	se := NewSyncEngine(DefaultSyncEngineConfig())

	_, _ = se.CreateCheckpoint(100, 1000, types.HexToHash("0xaa"), types.HexToHash("0xbb"))
	_, _ = se.CreateCheckpoint(200, 2000, types.HexToHash("0xcc"), types.HexToHash("0xdd"))

	entries := se.JournalEntries()
	if len(entries) != 2 {
		t.Fatalf("expected 2 journal entries, got %d", len(entries))
	}
	if entries[0].EventType != SyncEventCheckpoint {
		t.Errorf("entry 0: expected checkpoint event, got %s", entries[0].EventType)
	}
	if entries[0].Sequence != 0 || entries[1].Sequence != 1 {
		t.Error("journal sequence numbers should be monotonic")
	}
}

func TestSyncEngineJournalPruning(t *testing.T) {
	config := SyncEngineConfig{
		FinalizationDepth: 5,
		MaxJournalEntries: 3,
	}
	se := NewSyncEngine(config)

	for i := uint64(0); i < 5; i++ {
		_, _ = se.CreateCheckpoint(i*10, (i+1)*100, types.HexToHash("0xaa"), types.HexToHash("0xbb"))
	}

	if se.JournalLength() > config.MaxJournalEntries {
		t.Errorf("journal length %d exceeds max %d", se.JournalLength(), config.MaxJournalEntries)
	}
}

func TestSyncEnginePruneBefore(t *testing.T) {
	se := NewSyncEngine(DefaultSyncEngineConfig())

	_, _ = se.CreateCheckpoint(10, 100, types.HexToHash("0x01"), types.HexToHash("0x02"))
	_, _ = se.CreateCheckpoint(20, 200, types.HexToHash("0x03"), types.HexToHash("0x04"))
	_, _ = se.CreateCheckpoint(30, 300, types.HexToHash("0x05"), types.HexToHash("0x06"))

	pruned := se.PruneBefore(200)
	if pruned != 1 {
		t.Errorf("pruned %d, expected 1", pruned)
	}
	if se.CheckpointCount() != 2 {
		t.Errorf("expected 2 checkpoints after prune, got %d", se.CheckpointCount())
	}
}

func TestSyncEventTypeString(t *testing.T) {
	cases := []struct {
		e    SyncEventType
		want string
	}{
		{SyncEventCheckpoint, "checkpoint"},
		{SyncEventFinalization, "finalization"},
		{SyncEventDivergence, "divergence"},
		{SyncEventReconciliation, "reconciliation"},
		{SyncEventType(99), "unknown"},
	}
	for _, c := range cases {
		if got := c.e.String(); got != c.want {
			t.Errorf("%d.String() = %q, want %q", c.e, got, c.want)
		}
	}
}

func TestSyncEngineMultipleCheckpointsFinalization(t *testing.T) {
	config := SyncEngineConfig{
		FinalizationDepth: 10,
		MaxJournalEntries: 100,
	}
	se := NewSyncEngine(config)

	// Create 5 checkpoints across different L1 blocks.
	for i := uint64(1); i <= 5; i++ {
		_, err := se.CreateCheckpoint(i*10, i*100, types.HexToHash("0xaa"), types.HexToHash("0xbb"))
		if err != nil {
			t.Fatalf("checkpoint %d: %v", i, err)
		}
	}

	// Finalize at L1 block 25 should finalize the first checkpoint (L1=10).
	count := se.FinalizeCheckpoints(20)
	if count != 1 {
		t.Errorf("finalized %d, expected 1", count)
	}

	// Finalize at L1 block 55 should finalize all remaining.
	count = se.FinalizeCheckpoints(60)
	if count != 4 {
		t.Errorf("finalized %d, expected 4", count)
	}
}
