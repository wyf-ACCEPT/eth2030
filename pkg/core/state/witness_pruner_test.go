package state

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// makeTestWitnessData creates an ExecutionWitnessData with the specified
// number of stems and suffixes per stem.
func makeTestWitnessData(numStems int, suffixesPerStem int) *ExecutionWitnessData {
	w := &ExecutionWitnessData{
		ParentRoot: types.Hash{},
	}
	for i := 0; i < numStems; i++ {
		var stem [31]byte
		stem[0] = byte(i)
		diff := WitnessStemDiff{
			Stem: stem,
		}
		for j := 0; j < suffixesPerStem; j++ {
			diff.Suffixes = append(diff.Suffixes, WitnessSuffixDiff{
				Suffix: byte(j),
			})
		}
		w.State = append(w.State, diff)
	}
	return w
}

func TestWitnessPrunerNew(t *testing.T) {
	p := NewWitnessPruner(nil)
	if p == nil {
		t.Fatal("expected non-nil pruner")
	}
	if p.RetentionWindow() != defaultRetentionWindow {
		t.Fatalf("expected default retention window %d, got %d",
			defaultRetentionWindow, p.RetentionWindow())
	}
	if p.EntryCount() != 0 {
		t.Fatalf("expected 0 entries, got %d", p.EntryCount())
	}
}

func TestWitnessPrunerCustomConfig(t *testing.T) {
	cfg := &WitnessPrunerConfig{
		RetentionWindow: 100,
		BloomSize:       4096,
		BloomHashes:     3,
	}
	p := NewWitnessPruner(cfg)
	if p.RetentionWindow() != 100 {
		t.Fatalf("expected retention window 100, got %d", p.RetentionWindow())
	}
}

func TestWitnessPrunerMarkReachable(t *testing.T) {
	p := NewWitnessPruner(&WitnessPrunerConfig{
		RetentionWindow: 10,
		BloomSize:       4096,
		BloomHashes:     3,
	})

	w := makeTestWitnessData(3, 2)
	p.MarkReachable(100, w)

	if p.EntryCount() != 6 { // 3 stems * 2 suffixes
		t.Fatalf("expected 6 entries, got %d", p.EntryCount())
	}
	if p.BloomFilterCount() != 1 {
		t.Fatalf("expected 1 bloom filter, got %d", p.BloomFilterCount())
	}
}

func TestWitnessPrunerMarkReachableNilWitness(t *testing.T) {
	p := NewWitnessPruner(nil)
	// Should not panic on nil witness.
	p.MarkReachable(100, nil)
	if p.EntryCount() != 0 {
		t.Fatalf("expected 0 entries after nil witness, got %d", p.EntryCount())
	}
}

func TestWitnessPrunerReachabilityCheckFound(t *testing.T) {
	p := NewWitnessPruner(&WitnessPrunerConfig{
		RetentionWindow: 10,
		BloomSize:       8192,
		BloomHashes:     5,
	})

	addr := types.Address{0x01}
	slot := types.Hash{0x02}
	p.MarkReachableAccount(100, addr, slot)

	if !p.ReachabilityCheck(addr, slot) {
		t.Fatal("expected entry to be reachable after marking")
	}
}

func TestWitnessPrunerReachabilityCheckNotFound(t *testing.T) {
	p := NewWitnessPruner(&WitnessPrunerConfig{
		RetentionWindow: 10,
		BloomSize:       8192,
		BloomHashes:     5,
	})

	// Mark one address.
	addr1 := types.Address{0x01}
	slot1 := types.Hash{0x02}
	p.MarkReachableAccount(100, addr1, slot1)

	// Check a different address.
	addr2 := types.Address{0xFF}
	slot2 := types.Hash{0xFE}
	if p.ReachabilityCheck(addr2, slot2) {
		t.Fatal("expected unknown entry to not be reachable")
	}
}

func TestWitnessPrunerPruneDistinctEntries(t *testing.T) {
	p := NewWitnessPruner(&WitnessPrunerConfig{
		RetentionWindow: 10,
		BloomSize:       4096,
		BloomHashes:     3,
	})

	// Mark entry A at block 50.
	addrA := types.Address{0xAA}
	slotA := types.Hash{0xAA}
	p.MarkReachableAccount(50, addrA, slotA)

	// Mark entry B at block 100.
	addrB := types.Address{0xBB}
	slotB := types.Hash{0xBB}
	p.MarkReachableAccount(100, addrB, slotB)

	if p.EntryCount() != 2 {
		t.Fatalf("expected 2 entries, got %d", p.EntryCount())
	}

	// Prune at block 100, retention=10, cutoff=90. Entry A (block 50) pruned.
	result := p.Prune(100)

	if result.PrunedCount != 1 {
		t.Fatalf("expected 1 pruned, got %d", result.PrunedCount)
	}
	if result.RetainedCount != 1 {
		t.Fatalf("expected 1 retained, got %d", result.RetainedCount)
	}
	if result.BytesFreed == 0 {
		t.Fatal("expected non-zero bytes freed")
	}

	// Entry B should still be reachable.
	if !p.ReachabilityCheck(addrB, slotB) {
		t.Fatal("expected entry B to still be reachable")
	}

	// Entry A should no longer be reachable.
	if p.ReachabilityCheck(addrA, slotA) {
		t.Fatal("expected entry A to be pruned")
	}
}

func TestWitnessPrunerRetentionWindowEdge(t *testing.T) {
	p := NewWitnessPruner(&WitnessPrunerConfig{
		RetentionWindow: 10,
		BloomSize:       4096,
		BloomHashes:     3,
	})

	addr := types.Address{0x01}
	slot := types.Hash{0x01}

	// Mark at block 90.
	p.MarkReachableAccount(90, addr, slot)

	// Prune at block 100. Cutoff = 90. Entry at block 90 is NOT < 90, so kept.
	result := p.Prune(100)
	if result.PrunedCount != 0 {
		t.Fatalf("expected 0 pruned at exact boundary, got %d", result.PrunedCount)
	}

	// Prune at block 101. Cutoff = 91. Entry at block 90 IS < 91, so pruned.
	result = p.Prune(101)
	if result.PrunedCount != 1 {
		t.Fatalf("expected 1 pruned past boundary, got %d", result.PrunedCount)
	}
}

func TestWitnessPrunerRetentionWindowZeroBlock(t *testing.T) {
	p := NewWitnessPruner(&WitnessPrunerConfig{
		RetentionWindow: 100,
		BloomSize:       4096,
		BloomHashes:     3,
	})

	addr := types.Address{0x01}
	slot := types.Hash{0x01}

	// Mark at block 0.
	p.MarkReachableAccount(0, addr, slot)

	// Prune at block 50. Cutoff = max(0, 50-100) = 0. Entry at block 0 not < 0.
	result := p.Prune(50)
	if result.PrunedCount != 0 {
		t.Fatalf("expected 0 pruned when within underflow range, got %d", result.PrunedCount)
	}
}

func TestWitnessPrunerMultipleWitnessesAcrossBlocks(t *testing.T) {
	p := NewWitnessPruner(&WitnessPrunerConfig{
		RetentionWindow: 5,
		BloomSize:       4096,
		BloomHashes:     3,
	})

	// Mark different entries across blocks 1-10.
	for block := uint64(1); block <= 10; block++ {
		addr := types.Address{byte(block)}
		slot := types.Hash{byte(block)}
		p.MarkReachableAccount(block, addr, slot)
	}

	if p.EntryCount() != 10 {
		t.Fatalf("expected 10 entries, got %d", p.EntryCount())
	}
	if p.BloomFilterCount() != 10 {
		t.Fatalf("expected 10 bloom filters, got %d", p.BloomFilterCount())
	}

	// Prune at block 10 with retention=5, cutoff=5.
	// Entries at blocks 1-4 should be pruned (lastBlock < 5).
	result := p.Prune(10)

	if result.PrunedCount != 4 {
		t.Fatalf("expected 4 pruned, got %d", result.PrunedCount)
	}
	if result.RetainedCount != 6 {
		t.Fatalf("expected 6 retained, got %d", result.RetainedCount)
	}
}

func TestWitnessPrunerBloomFilterFPR(t *testing.T) {
	p := NewWitnessPruner(&WitnessPrunerConfig{
		RetentionWindow: 100,
		BloomSize:       1 << 16, // 64K bits
		BloomHashes:     7,
	})

	// Insert a moderate number of items.
	for i := 0; i < 100; i++ {
		addr := types.Address{byte(i), byte(i >> 8)}
		slot := types.Hash{byte(i), byte(i >> 8)}
		p.MarkReachableAccount(1, addr, slot)
	}

	fpr := p.EstimatedFPR(1)
	if fpr < 0 || fpr > 1 {
		t.Fatalf("FPR out of range: %f", fpr)
	}
	// With 64K bits, 7 hash functions, and 100 items, FPR should be very low.
	if fpr > 0.01 {
		t.Fatalf("expected FPR < 0.01, got %f", fpr)
	}
}

func TestWitnessPrunerBloomFilterNoBlock(t *testing.T) {
	p := NewWitnessPruner(nil)
	fpr := p.EstimatedFPR(999)
	if fpr != 0 {
		t.Fatalf("expected 0 FPR for non-existent block, got %f", fpr)
	}
}

func TestWitnessPrunerConcurrentAccess(t *testing.T) {
	p := NewWitnessPruner(&WitnessPrunerConfig{
		RetentionWindow: 50, BloomSize: 4096, BloomHashes: 3,
	})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func(id int) {
			defer wg.Done()
			p.MarkReachableAccount(uint64(id+100), types.Address{byte(id)}, types.Hash{byte(id)})
		}(i)
		go func(id int) {
			defer wg.Done()
			p.ReachabilityCheck(types.Address{byte(id)}, types.Hash{byte(id)})
		}(i)
	}
	wg.Add(1)
	go func() { defer wg.Done(); p.Prune(200) }()
	wg.Wait()
	_ = p.PruneStats()
}

func TestWitnessPrunerStatsAccuracy(t *testing.T) {
	p := NewWitnessPruner(&WitnessPrunerConfig{
		RetentionWindow: 5,
		BloomSize:       4096,
		BloomHashes:     3,
	})

	// Add 10 entries at different blocks.
	for i := uint64(1); i <= 10; i++ {
		addr := types.Address{byte(i)}
		slot := types.Hash{byte(i)}
		p.MarkReachableAccount(i, addr, slot)
	}

	// Prune at block 10, cutoff = 5. Entries 1-4 pruned.
	p.Prune(10)

	stats := p.PruneStats()
	if stats.PrunedCount != 4 {
		t.Fatalf("expected cumulative pruned=4, got %d", stats.PrunedCount)
	}
	if stats.RetainedCount != 6 {
		t.Fatalf("expected retained=6, got %d", stats.RetainedCount)
	}
	if stats.BytesFreed == 0 {
		t.Fatal("expected non-zero cumulative bytes freed")
	}
}

func TestWitnessPrunerEmptyWitness(t *testing.T) {
	p := NewWitnessPruner(nil)

	// Empty witness (no stems).
	w := &ExecutionWitnessData{
		ParentRoot: types.Hash{0x01},
	}
	p.MarkReachable(100, w)

	if p.EntryCount() != 0 {
		t.Fatalf("expected 0 entries from empty witness, got %d", p.EntryCount())
	}
}

func TestWitnessPrunerReset(t *testing.T) {
	p := NewWitnessPruner(&WitnessPrunerConfig{
		RetentionWindow: 10,
		BloomSize:       4096,
		BloomHashes:     3,
	})

	// Populate some entries.
	for i := uint64(1); i <= 5; i++ {
		addr := types.Address{byte(i)}
		slot := types.Hash{byte(i)}
		p.MarkReachableAccount(i, addr, slot)
	}

	if p.EntryCount() == 0 {
		t.Fatal("expected non-zero entries before reset")
	}

	p.Reset()

	if p.EntryCount() != 0 {
		t.Fatalf("expected 0 entries after reset, got %d", p.EntryCount())
	}
	if p.BloomFilterCount() != 0 {
		t.Fatalf("expected 0 bloom filters after reset, got %d", p.BloomFilterCount())
	}
	stats := p.PruneStats()
	if stats.PrunedCount != 0 || stats.BytesFreed != 0 {
		t.Fatal("expected zeroed stats after reset")
	}
}

func TestWitnessPrunerUpdateExistingEntry(t *testing.T) {
	p := NewWitnessPruner(&WitnessPrunerConfig{
		RetentionWindow: 10,
		BloomSize:       4096,
		BloomHashes:     3,
	})

	addr := types.Address{0xAA}
	slot := types.Hash{0xBB}

	// Mark at block 50.
	p.MarkReachableAccount(50, addr, slot)
	if p.EntryCount() != 1 {
		t.Fatalf("expected 1 entry, got %d", p.EntryCount())
	}

	// Mark the same entry again at block 100 (should update, not duplicate).
	p.MarkReachableAccount(100, addr, slot)
	if p.EntryCount() != 1 {
		t.Fatalf("expected still 1 entry after update, got %d", p.EntryCount())
	}

	// Prune at block 100, cutoff = 90. Entry was updated to block 100, so kept.
	result := p.Prune(100)
	if result.PrunedCount != 0 {
		t.Fatalf("expected 0 pruned after updating entry, got %d", result.PrunedCount)
	}
}

func TestWitnessPrunerPruneEmptyState(t *testing.T) {
	p := NewWitnessPruner(nil)

	result := p.Prune(100)
	if result.PrunedCount != 0 {
		t.Fatalf("expected 0 pruned on empty state, got %d", result.PrunedCount)
	}
	if result.RetainedCount != 0 {
		t.Fatalf("expected 0 retained on empty state, got %d", result.RetainedCount)
	}
}

func TestWitnessPrunerBloomFilterCleanup(t *testing.T) {
	p := NewWitnessPruner(&WitnessPrunerConfig{
		RetentionWindow: 5,
		BloomSize:       4096,
		BloomHashes:     3,
	})

	// Create bloom filters at blocks 1-10.
	for i := uint64(1); i <= 10; i++ {
		addr := types.Address{byte(i)}
		slot := types.Hash{byte(i)}
		p.MarkReachableAccount(i, addr, slot)
	}
	if p.BloomFilterCount() != 10 {
		t.Fatalf("expected 10 blooms, got %d", p.BloomFilterCount())
	}

	// Prune at block 10, cutoff = 5. Bloom filters for blocks 1-4 removed.
	p.Prune(10)
	if p.BloomFilterCount() != 6 {
		t.Fatalf("expected 6 blooms after pruning, got %d", p.BloomFilterCount())
	}
}

func TestWitnessPrunerMarkReachableWitnessVerify(t *testing.T) {
	p := NewWitnessPruner(&WitnessPrunerConfig{
		RetentionWindow: 100, BloomSize: 8192, BloomHashes: 5,
	})
	var stem [31]byte
	stem[0] = 0xDE
	w := &ExecutionWitnessData{
		ParentRoot: types.Hash{0x01},
		State: []WitnessStemDiff{{
			Stem:     stem,
			Suffixes: []WitnessSuffixDiff{{Suffix: 0x01}, {Suffix: 0x02}},
		}},
	}
	p.MarkReachable(50, w)
	if p.EntryCount() != 2 {
		t.Fatalf("expected 2 entries from witness, got %d", p.EntryCount())
	}
	var addr types.Address
	copy(addr[:], stem[:types.AddressLength])
	for _, suffix := range []byte{0x01, 0x02} {
		slot := types.Hash{}
		copy(slot[:], stem[:])
		slot[types.HashLength-1] = suffix
		if !p.ReachabilityCheck(addr, slot) {
			t.Fatalf("expected suffix 0x%02x to be reachable", suffix)
		}
	}
}

func TestWitnessPrunerMakeKeyDeterministic(t *testing.T) {
	addr := types.Address{0x01, 0x02, 0x03}
	slot := types.Hash{0x04, 0x05, 0x06}

	key1 := makeKey(addr, slot)
	key2 := makeKey(addr, slot)

	if key1 != key2 {
		t.Fatal("makeKey should be deterministic")
	}

	// Different address should produce different key.
	addr2 := types.Address{0xFF}
	key3 := makeKey(addr2, slot)
	if key1 == key3 {
		t.Fatal("different addresses should produce different keys")
	}
}
