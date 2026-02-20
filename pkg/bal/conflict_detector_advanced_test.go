package bal

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// advTestBAL builds a BAL with known conflict structure:
// tx0 writes addr1/slot1, tx1 reads addr1/slot1 (conflict 0-1),
// tx2 writes addr2/slot2 (no conflict with 0 or 1),
// tx3 writes addr1/slot1 (conflict 3-0, 3-1).
func advTestBAL() *BlockAccessList {
	bal := NewBlockAccessList()
	addr1 := types.HexToAddress("0xaa")
	addr2 := types.HexToAddress("0xbb")
	slot1 := types.HexToHash("0x01")
	slot2 := types.HexToHash("0x02")

	// tx0: writes addr1/slot1
	bal.AddEntry(AccessEntry{
		Address:     addr1,
		AccessIndex: 1,
		StorageChanges: []StorageChange{
			{Slot: slot1, OldValue: types.HexToHash("0x00"), NewValue: types.HexToHash("0x10")},
		},
	})
	// tx1: reads addr1/slot1
	bal.AddEntry(AccessEntry{
		Address:     addr1,
		AccessIndex: 2,
		StorageReads: []StorageAccess{
			{Slot: slot1, Value: types.HexToHash("0x10")},
		},
	})
	// tx2: writes addr2/slot2 (independent)
	bal.AddEntry(AccessEntry{
		Address:     addr2,
		AccessIndex: 3,
		StorageChanges: []StorageChange{
			{Slot: slot2, OldValue: types.HexToHash("0x00"), NewValue: types.HexToHash("0x20")},
		},
	})
	// tx3: writes addr1/slot1 (conflicts with tx0, tx1)
	bal.AddEntry(AccessEntry{
		Address:     addr1,
		AccessIndex: 4,
		StorageChanges: []StorageChange{
			{Slot: slot1, OldValue: types.HexToHash("0x10"), NewValue: types.HexToHash("0x30")},
		},
	})
	return bal
}

func TestAdvancedAnalyzerAnalyzeConflicts(t *testing.T) {
	detector := NewBALConflictDetector(StrategySerialize)
	analyzer := NewAdvancedConflictAnalyzer(detector)

	conflicts, matrix := analyzer.AnalyzeConflicts(advTestBAL())
	if len(conflicts) == 0 {
		t.Fatal("expected conflicts")
	}
	if matrix == nil {
		t.Fatal("expected conflict matrix")
	}
	if matrix.Size() != 4 {
		t.Fatalf("matrix size = %d, want 4", matrix.Size())
	}
	// tx0 and tx1 conflict (write-read on slot1).
	if !matrix.Get(0, 1) {
		t.Error("expected conflict between tx0 and tx1")
	}
	// tx2 is independent.
	if matrix.Get(0, 2) {
		t.Error("tx0 and tx2 should not conflict")
	}
	if matrix.Get(1, 2) {
		t.Error("tx1 and tx2 should not conflict")
	}
}

func TestAdvancedAnalyzerAnalyzeConflictsNil(t *testing.T) {
	detector := NewBALConflictDetector(StrategySerialize)
	analyzer := NewAdvancedConflictAnalyzer(detector)
	conflicts, matrix := analyzer.AnalyzeConflicts(nil)
	if conflicts != nil {
		t.Error("nil BAL should produce nil conflicts")
	}
	if matrix != nil {
		t.Error("nil BAL should produce nil matrix")
	}
}

func TestAdvancedAnalyzerComputeClusters(t *testing.T) {
	detector := NewBALConflictDetector(StrategySerialize)
	analyzer := NewAdvancedConflictAnalyzer(detector)

	clusters := analyzer.ComputeClusters(advTestBAL())
	if len(clusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d", len(clusters))
	}

	// One cluster should contain tx0,tx1,tx3 and the other tx2.
	sizes := map[int]int{}
	for _, cl := range clusters {
		sizes[len(cl.TxIndices)]++
	}
	if sizes[3] != 1 || sizes[1] != 1 {
		t.Errorf("expected cluster sizes {3:1, 1:1}, got %v", sizes)
	}
}

func TestAdvancedAnalyzerComputeClustersNil(t *testing.T) {
	detector := NewBALConflictDetector(StrategySerialize)
	analyzer := NewAdvancedConflictAnalyzer(detector)
	clusters := analyzer.ComputeClusters(nil)
	if clusters != nil {
		t.Error("nil BAL should produce nil clusters")
	}
}

func TestAdvancedAnalyzerComputeClustersNoConflicts(t *testing.T) {
	detector := NewBALConflictDetector(StrategySerialize)
	analyzer := NewAdvancedConflictAnalyzer(detector)

	bal := NewBlockAccessList()
	bal.AddEntry(AccessEntry{
		Address:     types.HexToAddress("0xaa"),
		AccessIndex: 1,
		StorageReads: []StorageAccess{
			{Slot: types.HexToHash("0x01"), Value: types.HexToHash("0xff")},
		},
	})
	bal.AddEntry(AccessEntry{
		Address:     types.HexToAddress("0xbb"),
		AccessIndex: 2,
		StorageReads: []StorageAccess{
			{Slot: types.HexToHash("0x02"), Value: types.HexToHash("0xee")},
		},
	})

	clusters := analyzer.ComputeClusters(bal)
	// No conflicts, so each tx is its own cluster.
	if len(clusters) != 2 {
		t.Fatalf("expected 2 single-tx clusters, got %d", len(clusters))
	}
}

func TestAdvancedAnalyzerSuggestReorder(t *testing.T) {
	detector := NewBALConflictDetector(StrategySerialize)
	analyzer := NewAdvancedConflictAnalyzer(detector)

	suggestion := analyzer.SuggestReorder(advTestBAL())
	if suggestion == nil {
		t.Fatal("expected reorder suggestion")
	}
	if len(suggestion.OriginalOrder) != 4 {
		t.Fatalf("original order length = %d, want 4", len(suggestion.OriginalOrder))
	}
	if len(suggestion.SuggestedOrder) != 4 {
		t.Fatalf("suggested order length = %d, want 4", len(suggestion.SuggestedOrder))
	}
	if suggestion.WavesBefore <= 0 {
		t.Errorf("waves before = %d, should be > 0", suggestion.WavesBefore)
	}
	// All tx indices should be present in suggested order.
	seen := make(map[int]bool)
	for _, idx := range suggestion.SuggestedOrder {
		seen[idx] = true
	}
	for i := 0; i < 4; i++ {
		if !seen[i] {
			t.Errorf("tx %d missing from suggested order", i)
		}
	}
}

func TestAdvancedAnalyzerSuggestReorderNil(t *testing.T) {
	detector := NewBALConflictDetector(StrategySerialize)
	analyzer := NewAdvancedConflictAnalyzer(detector)
	if analyzer.SuggestReorder(nil) != nil {
		t.Error("nil BAL should produce nil suggestion")
	}
}

func TestAdvancedAnalyzerScoreParallelism(t *testing.T) {
	detector := NewBALConflictDetector(StrategySerialize)
	analyzer := NewAdvancedConflictAnalyzer(detector)

	score := analyzer.ScoreParallelism(advTestBAL())
	if score.TotalTx != 4 {
		t.Errorf("TotalTx = %d, want 4", score.TotalTx)
	}
	if score.ClusterCount != 2 {
		t.Errorf("ClusterCount = %d, want 2", score.ClusterCount)
	}
	if score.MaxClusterSize != 3 {
		t.Errorf("MaxClusterSize = %d, want 3", score.MaxClusterSize)
	}
	if score.WaveCount <= 0 {
		t.Errorf("WaveCount = %d, should be > 0", score.WaveCount)
	}
	// Score should be between 0 and 1.
	if score.Score < 0 || score.Score > 1.0 {
		t.Errorf("Score = %f, should be in [0, 1]", score.Score)
	}
	// With 4 txs and some conflicts, score should be < 1.
	if score.Score >= 1.0 {
		t.Errorf("Score = %f, expected < 1.0 with conflicts", score.Score)
	}
	// Total pairs should be 6 (4 choose 2).
	if score.IndependentPairs+score.ConflictingPairs != 6 {
		t.Errorf("independent(%d) + conflicting(%d) != 6",
			score.IndependentPairs, score.ConflictingPairs)
	}
}

func TestAdvancedAnalyzerScoreParallelismNoConflicts(t *testing.T) {
	detector := NewBALConflictDetector(StrategySerialize)
	analyzer := NewAdvancedConflictAnalyzer(detector)

	bal := NewBlockAccessList()
	for i := uint64(1); i <= 3; i++ {
		bal.AddEntry(AccessEntry{
			Address:     types.BytesToAddress([]byte{byte(i)}),
			AccessIndex: i,
			StorageReads: []StorageAccess{
				{Slot: types.BytesToHash([]byte{byte(i)}), Value: types.HexToHash("0x01")},
			},
		})
	}

	score := analyzer.ScoreParallelism(bal)
	if score.Score != 1.0 {
		t.Errorf("Score = %f, want 1.0 for conflict-free block", score.Score)
	}
	if score.ConflictingPairs != 0 {
		t.Errorf("ConflictingPairs = %d, want 0", score.ConflictingPairs)
	}
}

func TestAdvancedAnalyzerScoreParallelismEmpty(t *testing.T) {
	detector := NewBALConflictDetector(StrategySerialize)
	analyzer := NewAdvancedConflictAnalyzer(detector)
	score := analyzer.ScoreParallelism(nil)
	if score.TotalTx != 0 {
		t.Errorf("TotalTx = %d, want 0", score.TotalTx)
	}
}

func TestAdvancedAnalyzerScoreParallelismSingleTx(t *testing.T) {
	detector := NewBALConflictDetector(StrategySerialize)
	analyzer := NewAdvancedConflictAnalyzer(detector)

	bal := NewBlockAccessList()
	bal.AddEntry(AccessEntry{
		Address:     types.HexToAddress("0xaa"),
		AccessIndex: 1,
		StorageReads: []StorageAccess{
			{Slot: types.HexToHash("0x01"), Value: types.HexToHash("0xff")},
		},
	})

	score := analyzer.ScoreParallelism(bal)
	if score.TotalTx != 1 {
		t.Errorf("TotalTx = %d, want 1", score.TotalTx)
	}
	if score.Score != 1.0 {
		t.Errorf("Score = %f, want 1.0 for single tx", score.Score)
	}
}

func TestAdvancedAnalyzerConflictsByAddress(t *testing.T) {
	detector := NewBALConflictDetector(StrategySerialize)
	analyzer := NewAdvancedConflictAnalyzer(detector)

	byAddr := analyzer.ConflictsByAddress(advTestBAL())
	if byAddr == nil {
		t.Fatal("expected non-nil result")
	}
	addr1 := types.HexToAddress("0xaa")
	if _, ok := byAddr[addr1]; !ok {
		t.Error("expected conflicts for addr 0xaa")
	}
	addr2 := types.HexToAddress("0xbb")
	if _, ok := byAddr[addr2]; ok {
		t.Error("no conflicts expected for addr 0xbb")
	}
}

func TestAdvancedAnalyzerConflictsByAddressNil(t *testing.T) {
	detector := NewBALConflictDetector(StrategySerialize)
	analyzer := NewAdvancedConflictAnalyzer(detector)
	if analyzer.ConflictsByAddress(nil) != nil {
		t.Error("nil BAL should return nil")
	}
}

func TestAdvancedAnalyzerHotSpots(t *testing.T) {
	detector := NewBALConflictDetector(StrategySerialize)
	analyzer := NewAdvancedConflictAnalyzer(detector)

	spots := analyzer.HotSpots(advTestBAL(), 0)
	if len(spots) == 0 {
		t.Fatal("expected hot spots")
	}
	// The hottest address should be addr1 (0xaa) since all conflicts are on it.
	addr1 := types.HexToAddress("0xaa")
	if spots[0].Address != addr1 {
		t.Errorf("hottest address = %v, want %v", spots[0].Address, addr1)
	}
	if spots[0].Count <= 0 {
		t.Errorf("hot spot count = %d, should be > 0", spots[0].Count)
	}
}

func TestAdvancedAnalyzerHotSpotsWithLimit(t *testing.T) {
	detector := NewBALConflictDetector(StrategySerialize)
	analyzer := NewAdvancedConflictAnalyzer(detector)

	spots := analyzer.HotSpots(advTestBAL(), 1)
	if len(spots) != 1 {
		t.Fatalf("expected 1 hot spot with limit=1, got %d", len(spots))
	}
}

func TestAdvancedAnalyzerHotSpotsNil(t *testing.T) {
	detector := NewBALConflictDetector(StrategySerialize)
	analyzer := NewAdvancedConflictAnalyzer(detector)
	if analyzer.HotSpots(nil, 0) != nil {
		t.Error("nil BAL should return nil hot spots")
	}
}
