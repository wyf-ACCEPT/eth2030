package snapshot

import (
	"testing"

	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/types"
)

// --- Helper to build a tree with N diff layers ---

func buildTreeWithLayers(n int) (*Tree, []types.Hash) {
	db := rawdb.NewMemoryDB()
	diskRoot := makeHash(0x01)
	tree := NewTree(db, diskRoot)

	roots := make([]types.Hash, n)
	parent := diskRoot
	for i := 0; i < n; i++ {
		root := makeHash(byte(i + 2))
		acctHash := makeHash(byte(i + 0x80))
		accounts := map[types.Hash][]byte{acctHash: {byte(i)}}
		tree.Update(root, parent, accounts, nil)
		roots[i] = root
		parent = root
	}
	return tree, roots
}

func TestCompactionSchedulerNew(t *testing.T) {
	cs := NewCompactionScheduler(nil)
	if cs == nil {
		t.Fatal("expected non-nil scheduler")
	}
	cfg := cs.Config()
	if cfg.MaxLayers != defaultMaxLayers {
		t.Fatalf("expected default max layers %d, got %d", defaultMaxLayers, cfg.MaxLayers)
	}
	if cfg.MaxMemory != defaultMaxMemory {
		t.Fatalf("expected default max memory %d, got %d", defaultMaxMemory, cfg.MaxMemory)
	}
}

func TestCompactionSchedulerCustomConfig(t *testing.T) {
	cfg := &CompactionConfig{MaxLayers: 10, MaxMemory: 1024}
	cs := NewCompactionScheduler(cfg)
	c := cs.Config()
	if c.MaxLayers != 10 {
		t.Fatalf("expected max layers 10, got %d", c.MaxLayers)
	}
	if c.MaxMemory != 1024 {
		t.Fatalf("expected max memory 1024, got %d", c.MaxMemory)
	}
}

func TestCompactionSchedulerShouldCompactBelowThreshold(t *testing.T) {
	cs := NewCompactionScheduler(&CompactionConfig{
		MaxLayers: 10,
		MaxMemory: 1 << 30, // 1 GB, won't trigger
	})
	tree, _ := buildTreeWithLayers(5)

	if cs.ShouldCompact(tree) {
		t.Fatal("should not compact when below layer threshold")
	}
}

func TestCompactionSchedulerShouldCompactAboveThreshold(t *testing.T) {
	cs := NewCompactionScheduler(&CompactionConfig{
		MaxLayers: 3,
		MaxMemory: 1 << 30,
	})
	tree, _ := buildTreeWithLayers(5)

	if !cs.ShouldCompact(tree) {
		t.Fatal("should compact when above layer threshold")
	}
}

func TestCompactionSchedulerShouldCompactMemoryThreshold(t *testing.T) {
	cs := NewCompactionScheduler(&CompactionConfig{
		MaxLayers: 1000, // Won't trigger on count.
		MaxMemory: 1,    // 1 byte, will trigger immediately.
	})

	db := rawdb.NewMemoryDB()
	diskRoot := makeHash(0x01)
	tree := NewTree(db, diskRoot)

	blockRoot := makeHash(0x02)
	acctHash := makeHash(0xAA)
	acctData := make([]byte, 100)
	tree.Update(blockRoot, diskRoot, map[types.Hash][]byte{acctHash: acctData}, nil)

	if !cs.ShouldCompact(tree) {
		t.Fatal("should compact when above memory threshold")
	}
}

func TestCompactionSchedulerShouldCompactNilTree(t *testing.T) {
	cs := NewCompactionScheduler(nil)
	if cs.ShouldCompact(nil) {
		t.Fatal("should not compact nil tree")
	}
}

func TestCompactionSchedulerShouldCompactEmptyTree(t *testing.T) {
	cs := NewCompactionScheduler(&CompactionConfig{MaxLayers: 1, MaxMemory: 1})
	db := rawdb.NewMemoryDB()
	tree := NewTree(db, makeHash(0x01))

	// Only disk layer, no diff layers.
	if cs.ShouldCompact(tree) {
		t.Fatal("should not compact tree with only disk layer")
	}
}

func TestCompactionSchedulerPlanCompactionNilTree(t *testing.T) {
	cs := NewCompactionScheduler(nil)
	plan := cs.PlanCompaction(nil)
	if plan.LayerCount != 0 {
		t.Fatalf("expected empty plan for nil tree, got %d layers", plan.LayerCount)
	}
}

func TestCompactionSchedulerPlanCompactionBelowThreshold(t *testing.T) {
	cs := NewCompactionScheduler(&CompactionConfig{
		MaxLayers: 100,
		MaxMemory: 1 << 30,
	})
	tree, _ := buildTreeWithLayers(3)

	plan := cs.PlanCompaction(tree)
	if plan.LayerCount != 0 {
		t.Fatalf("expected empty plan below threshold, got %d layers", plan.LayerCount)
	}
}

func TestCompactionSchedulerPlanCompactionAboveThreshold(t *testing.T) {
	cs := NewCompactionScheduler(&CompactionConfig{
		MaxLayers: 3,
		MaxMemory: 1 << 30,
	})
	tree, _ := buildTreeWithLayers(6)

	plan := cs.PlanCompaction(tree)
	if plan.LayerCount == 0 {
		t.Fatal("expected non-empty plan above threshold")
	}
	if len(plan.SourceLayers) != plan.LayerCount {
		t.Fatalf("source layers count %d != plan layer count %d",
			len(plan.SourceLayers), plan.LayerCount)
	}
	if plan.TargetRoot.IsZero() {
		t.Fatal("expected non-zero target root")
	}
}

func TestCompactionSchedulerPlanCompactionEstimatedSavings(t *testing.T) {
	cs := NewCompactionScheduler(&CompactionConfig{
		MaxLayers: 2,
		MaxMemory: 1 << 30,
	})
	tree, _ := buildTreeWithLayers(5)

	plan := cs.PlanCompaction(tree)
	if plan.LayerCount < 2 {
		t.Fatalf("expected at least 2 layers in plan, got %d", plan.LayerCount)
	}
	if plan.EstimatedSavings == 0 {
		t.Fatal("expected non-zero estimated savings for multi-layer merge")
	}
	if plan.TotalMemory == 0 {
		t.Fatal("expected non-zero total memory in plan")
	}
}

func TestEstimateMemoryNilLayer(t *testing.T) {
	mem := EstimateMemory(nil)
	if mem != 0 {
		t.Fatalf("expected 0 memory for nil layer, got %d", mem)
	}
}

func TestEstimateMemoryEmptyLayer(t *testing.T) {
	dl := newDiffLayer(nil, types.Hash{}, make(map[types.Hash][]byte), nil)
	mem := EstimateMemory(dl)
	// Should be at least 0 (no data).
	if mem < 0 {
		t.Fatal("memory estimate should be non-negative")
	}
}

func TestEstimateMemoryWithData(t *testing.T) {
	acctHash := makeHash(0xAA)
	slotHash := makeHash(0xBB)
	acctData := make([]byte, 100)
	slotData := make([]byte, 32)

	accounts := map[types.Hash][]byte{acctHash: acctData}
	storage := map[types.Hash]map[types.Hash][]byte{
		acctHash: {slotHash: slotData},
	}
	dl := newDiffLayer(nil, makeHash(0x01), accounts, storage)

	mem := EstimateMemory(dl)
	if mem == 0 {
		t.Fatal("expected non-zero memory for layer with data")
	}
	// Memory should be at least the raw data size.
	rawSize := uint64(types.HashLength + len(acctData) + types.HashLength + len(slotData))
	if mem < rawSize {
		t.Fatalf("estimated memory %d less than raw data size %d", mem, rawSize)
	}
}

func TestMergeRangeEmpty(t *testing.T) {
	cs := NewCompactionScheduler(nil)
	_, err := cs.MergeRange(nil)
	if err != ErrInvalidMergeRange {
		t.Fatalf("expected ErrInvalidMergeRange, got %v", err)
	}

	_, err = cs.MergeRange([]*diffLayer{})
	if err != ErrInvalidMergeRange {
		t.Fatalf("expected ErrInvalidMergeRange for empty slice, got %v", err)
	}
}

func TestMergeRangeSingleLayer(t *testing.T) {
	cs := NewCompactionScheduler(nil)

	acctHash := makeHash(0xAA)
	accounts := map[types.Hash][]byte{acctHash: {1, 2, 3}}
	dl := newDiffLayer(nil, makeHash(0x01), accounts, nil)

	merged, err := cs.MergeRange([]*diffLayer{dl})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if merged != dl {
		t.Fatal("single layer merge should return the same layer")
	}
}

func TestMergeRangeAccountsMerge(t *testing.T) {
	cs := NewCompactionScheduler(nil)

	// Older layer has account A.
	hashA := makeHash(0xAA)
	accts1 := map[types.Hash][]byte{hashA: {1}}
	layer1 := newDiffLayer(nil, makeHash(0x01), accts1, nil)

	// Newer layer has account B.
	hashB := makeHash(0xBB)
	accts2 := map[types.Hash][]byte{hashB: {2}}
	layer2 := newDiffLayer(layer1, makeHash(0x02), accts2, nil)

	// Merge: newer first, then older.
	merged, err := cs.MergeRange([]*diffLayer{layer2, layer1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Merged layer should have both accounts.
	merged.lock.RLock()
	defer merged.lock.RUnlock()

	if _, ok := merged.accountData[hashA]; !ok {
		t.Fatal("expected account A in merged layer")
	}
	if _, ok := merged.accountData[hashB]; !ok {
		t.Fatal("expected account B in merged layer")
	}
}

func TestMergeRangeNewerOverwritesOlder(t *testing.T) {
	cs := NewCompactionScheduler(nil)

	hash := makeHash(0xAA)

	// Older layer: value = {1}.
	accts1 := map[types.Hash][]byte{hash: {1}}
	layer1 := newDiffLayer(nil, makeHash(0x01), accts1, nil)

	// Newer layer: value = {2}.
	accts2 := map[types.Hash][]byte{hash: {2}}
	layer2 := newDiffLayer(layer1, makeHash(0x02), accts2, nil)

	merged, err := cs.MergeRange([]*diffLayer{layer2, layer1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	merged.lock.RLock()
	defer merged.lock.RUnlock()

	data := merged.accountData[hash]
	if len(data) != 1 || data[0] != 2 {
		t.Fatalf("expected newer value {2}, got %v", data)
	}
}

func TestMergeRangeStorageMerge(t *testing.T) {
	cs := NewCompactionScheduler(nil)

	acctHash := makeHash(0xAA)
	slot1 := makeHash(0x10)
	slot2 := makeHash(0x20)

	// Older: slot1 = {1}.
	stor1 := map[types.Hash]map[types.Hash][]byte{
		acctHash: {slot1: {1}},
	}
	layer1 := newDiffLayer(nil, makeHash(0x01), nil, stor1)

	// Newer: slot2 = {2}.
	stor2 := map[types.Hash]map[types.Hash][]byte{
		acctHash: {slot2: {2}},
	}
	layer2 := newDiffLayer(layer1, makeHash(0x02), nil, stor2)

	merged, err := cs.MergeRange([]*diffLayer{layer2, layer1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	merged.lock.RLock()
	defer merged.lock.RUnlock()

	slots := merged.storageData[acctHash]
	if slots == nil {
		t.Fatal("expected storage for account in merged layer")
	}
	if _, ok := slots[slot1]; !ok {
		t.Fatal("expected slot1 in merged storage")
	}
	if _, ok := slots[slot2]; !ok {
		t.Fatal("expected slot2 in merged storage")
	}
}

func TestMergeRangeDeletedAccounts(t *testing.T) {
	cs := NewCompactionScheduler(nil)

	hash := makeHash(0xAA)

	// Older: account exists.
	accts1 := map[types.Hash][]byte{hash: {1, 2, 3}}
	layer1 := newDiffLayer(nil, makeHash(0x01), accts1, nil)

	// Newer: account deleted (nil).
	accts2 := map[types.Hash][]byte{hash: nil}
	layer2 := newDiffLayer(layer1, makeHash(0x02), accts2, nil)

	merged, err := cs.MergeRange([]*diffLayer{layer2, layer1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	merged.lock.RLock()
	defer merged.lock.RUnlock()

	data, exists := merged.accountData[hash]
	if !exists {
		t.Fatal("expected account hash key to exist (as deletion marker)")
	}
	if data != nil {
		t.Fatalf("expected nil data for deleted account, got %v", data)
	}
}

func TestCompactionMetricsTracking(t *testing.T) {
	cs := NewCompactionScheduler(nil)
	m := cs.Metrics()
	if m.CompactionsPerformed != 0 || m.LayersMerged != 0 || m.BytesSaved != 0 {
		t.Fatal("expected zero initial metrics")
	}
	layer1 := newDiffLayer(nil, makeHash(0x01),
		map[types.Hash][]byte{makeHash(0xAA): make([]byte, 100)}, nil)
	layer2 := newDiffLayer(layer1, makeHash(0x02),
		map[types.Hash][]byte{makeHash(0xBB): make([]byte, 100)}, nil)
	if _, err := cs.MergeRange([]*diffLayer{layer2, layer1}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m = cs.Metrics()
	if m.CompactionsPerformed != 1 {
		t.Fatalf("expected 1 compaction, got %d", m.CompactionsPerformed)
	}
	if m.LayersMerged != 2 {
		t.Fatalf("expected 2 layers merged, got %d", m.LayersMerged)
	}
}

func TestCompactionMetricsCumulative(t *testing.T) {
	cs := NewCompactionScheduler(nil)

	// Perform two merges.
	for i := 0; i < 2; i++ {
		h := makeHash(byte(i + 0x10))
		layer1 := newDiffLayer(nil, makeHash(byte(i*2+1)),
			map[types.Hash][]byte{h: {byte(i)}}, nil)
		layer2 := newDiffLayer(layer1, makeHash(byte(i*2+2)),
			map[types.Hash][]byte{h: {byte(i + 1)}}, nil)
		cs.MergeRange([]*diffLayer{layer2, layer1})
	}

	m := cs.Metrics()
	if m.CompactionsPerformed != 2 {
		t.Fatalf("expected 2 cumulative compactions, got %d", m.CompactionsPerformed)
	}
	if m.LayersMerged != 4 {
		t.Fatalf("expected 4 cumulative layers merged, got %d", m.LayersMerged)
	}
}

func TestCompactionSchedulerPlanSingleLayer(t *testing.T) {
	cs := NewCompactionScheduler(&CompactionConfig{
		MaxLayers: 1,
		MaxMemory: 1 << 30,
	})
	tree, _ := buildTreeWithLayers(1)

	plan := cs.PlanCompaction(tree)
	// A single diff layer cannot be merged by itself.
	if plan.LayerCount > 1 {
		t.Fatalf("expected plan with <= 1 layers for single diff, got %d", plan.LayerCount)
	}
}

func TestCompactionSchedulerCollectDiffLayersChain(t *testing.T) {
	cs := NewCompactionScheduler(nil)
	tree, _ := buildTreeWithLayers(5)
	layers := cs.collectDiffLayers(tree)
	if len(layers) != 5 {
		t.Fatalf("expected 5 diff layers, got %d", len(layers))
	}
	// Verify all roots are distinct.
	for i := 1; i < len(layers); i++ {
		if layers[i-1].root == layers[i].root {
			t.Fatal("duplicate layer roots in chain")
		}
	}
}

func TestMergeRangeThreeLayersMerge(t *testing.T) {
	cs := NewCompactionScheduler(nil)

	hashA := makeHash(0xAA)
	hashB := makeHash(0xBB)
	hashC := makeHash(0xCC)

	layer1 := newDiffLayer(nil, makeHash(0x01),
		map[types.Hash][]byte{hashA: {1}}, nil)
	layer2 := newDiffLayer(layer1, makeHash(0x02),
		map[types.Hash][]byte{hashB: {2}}, nil)
	layer3 := newDiffLayer(layer2, makeHash(0x03),
		map[types.Hash][]byte{hashC: {3}}, nil)

	merged, err := cs.MergeRange([]*diffLayer{layer3, layer2, layer1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	merged.lock.RLock()
	defer merged.lock.RUnlock()

	if len(merged.accountData) != 3 {
		t.Fatalf("expected 3 accounts in merged layer, got %d", len(merged.accountData))
	}
	// Root should be from the newest layer.
	if merged.root != makeHash(0x03) {
		t.Fatalf("expected merged root 0x03, got %v", merged.root)
	}
}

func TestMergeRangePreservesParent(t *testing.T) {
	cs := NewCompactionScheduler(nil)

	// Create a disk-like base layer.
	baseDL := newDiffLayer(nil, makeHash(0x00),
		map[types.Hash][]byte{makeHash(0x01): {0}}, nil)

	layer1 := newDiffLayer(baseDL, makeHash(0x01),
		map[types.Hash][]byte{makeHash(0xAA): {1}}, nil)
	layer2 := newDiffLayer(layer1, makeHash(0x02),
		map[types.Hash][]byte{makeHash(0xBB): {2}}, nil)

	merged, err := cs.MergeRange([]*diffLayer{layer2, layer1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The merged layer's parent should be baseDL (oldest layer's parent).
	if merged.Parent() != baseDL {
		t.Fatal("merged layer's parent should be the oldest layer's parent")
	}
}
