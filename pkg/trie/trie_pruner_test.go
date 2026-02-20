package trie

import (
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func hashFromByte(b byte) types.Hash {
	var h types.Hash
	h[types.HashLength-1] = b
	return h
}

func TestNewTriePrunerDefaults(t *testing.T) {
	p := NewTriePruner(PrunerConfig{})
	if p == nil {
		t.Fatal("expected non-nil pruner")
	}
	if p.config.BatchSize <= 0 {
		t.Fatal("expected positive default batch size")
	}
	stats := p.Stats()
	if stats.LiveNodes != 0 || stats.DeadNodes != 0 || stats.PrunedTotal != 0 {
		t.Fatalf("expected zero stats on fresh pruner, got %+v", stats)
	}
	if stats.LastPruneTime != 0 {
		t.Fatalf("expected zero last prune time, got %d", stats.LastPruneTime)
	}
}

func TestMarkLive(t *testing.T) {
	p := NewTriePruner(PrunerConfig{})
	h := hashFromByte(0x01)

	p.MarkLive(h)
	if !p.IsLive(h) {
		t.Fatal("expected node to be live")
	}
	stats := p.Stats()
	if stats.LiveNodes != 1 {
		t.Fatalf("expected 1 live node, got %d", stats.LiveNodes)
	}
	if stats.DeadNodes != 0 {
		t.Fatalf("expected 0 dead nodes, got %d", stats.DeadNodes)
	}
}

func TestMarkDead(t *testing.T) {
	p := NewTriePruner(PrunerConfig{})
	h := hashFromByte(0x02)

	p.MarkDead(h)
	if p.IsLive(h) {
		t.Fatal("expected node not to be live")
	}
	stats := p.Stats()
	if stats.DeadNodes != 1 {
		t.Fatalf("expected 1 dead node, got %d", stats.DeadNodes)
	}
}

func TestMarkLiveRemovesFromDead(t *testing.T) {
	p := NewTriePruner(PrunerConfig{})
	h := hashFromByte(0x03)

	// Mark dead first, then mark live -- should be removed from dead.
	p.MarkDead(h)
	if p.Stats().DeadNodes != 1 {
		t.Fatal("expected 1 dead node")
	}
	p.MarkLive(h)
	if !p.IsLive(h) {
		t.Fatal("expected node to be live after re-marking")
	}
	if p.Stats().DeadNodes != 0 {
		t.Fatal("expected 0 dead nodes after marking live")
	}
}

func TestMarkDeadRemovesFromLive(t *testing.T) {
	p := NewTriePruner(PrunerConfig{})
	h := hashFromByte(0x04)

	p.MarkLive(h)
	p.MarkDead(h)
	if p.IsLive(h) {
		t.Fatal("expected node not to be live after marking dead")
	}
	if p.Stats().LiveNodes != 0 {
		t.Fatalf("expected 0 live nodes, got %d", p.Stats().LiveNodes)
	}
}

func TestPrunableNodes(t *testing.T) {
	p := NewTriePruner(PrunerConfig{})
	h1 := hashFromByte(0x10)
	h2 := hashFromByte(0x20)
	h3 := hashFromByte(0x30)

	p.MarkDead(h1)
	p.MarkDead(h2)
	p.MarkDead(h3)

	prunable := p.PrunableNodes()
	if len(prunable) != 3 {
		t.Fatalf("expected 3 prunable nodes, got %d", len(prunable))
	}
	// Verify sorted order.
	for i := 1; i < len(prunable); i++ {
		if comparHashes(prunable[i-1], prunable[i]) >= 0 {
			t.Fatal("prunable nodes not in sorted order")
		}
	}
}

func TestPrunableNodesEmpty(t *testing.T) {
	p := NewTriePruner(PrunerConfig{})
	prunable := p.PrunableNodes()
	if len(prunable) != 0 {
		t.Fatalf("expected 0 prunable nodes, got %d", len(prunable))
	}
}

func TestPrune(t *testing.T) {
	p := NewTriePruner(PrunerConfig{BatchSize: 100})

	for i := byte(0); i < 5; i++ {
		p.MarkDead(hashFromByte(i))
	}
	if p.Stats().DeadNodes != 5 {
		t.Fatalf("expected 5 dead nodes, got %d", p.Stats().DeadNodes)
	}

	pruned, err := p.Prune()
	if err != nil {
		t.Fatal(err)
	}
	if pruned != 5 {
		t.Fatalf("expected 5 pruned, got %d", pruned)
	}
	if p.Stats().DeadNodes != 0 {
		t.Fatalf("expected 0 dead nodes after prune, got %d", p.Stats().DeadNodes)
	}
	if p.Stats().PrunedTotal != 5 {
		t.Fatalf("expected PrunedTotal=5, got %d", p.Stats().PrunedTotal)
	}
	if p.Stats().LastPruneTime == 0 {
		t.Fatal("expected non-zero LastPruneTime")
	}
}

func TestPruneBatchLimit(t *testing.T) {
	p := NewTriePruner(PrunerConfig{BatchSize: 3})

	for i := byte(0); i < 10; i++ {
		p.MarkDead(hashFromByte(i))
	}

	pruned, err := p.Prune()
	if err != nil {
		t.Fatal(err)
	}
	if pruned != 3 {
		t.Fatalf("expected 3 pruned (batch limit), got %d", pruned)
	}
	if p.Stats().DeadNodes != 7 {
		t.Fatalf("expected 7 remaining dead nodes, got %d", p.Stats().DeadNodes)
	}
}

func TestPruneEmpty(t *testing.T) {
	p := NewTriePruner(PrunerConfig{})
	pruned, err := p.Prune()
	if err != nil {
		t.Fatal(err)
	}
	if pruned != 0 {
		t.Fatalf("expected 0 pruned from empty set, got %d", pruned)
	}
	// LastPruneTime should remain 0 since nothing was pruned.
	if p.Stats().LastPruneTime != 0 {
		t.Fatal("expected LastPruneTime to stay 0 when nothing pruned")
	}
}

func TestPruneAccumulatesTotal(t *testing.T) {
	p := NewTriePruner(PrunerConfig{BatchSize: 2})

	for i := byte(0); i < 6; i++ {
		p.MarkDead(hashFromByte(i))
	}

	// Prune in 3 batches.
	total := 0
	for i := 0; i < 3; i++ {
		n, err := p.Prune()
		if err != nil {
			t.Fatal(err)
		}
		total += n
	}
	if total != 6 {
		t.Fatalf("expected 6 total pruned, got %d", total)
	}
	if p.Stats().PrunedTotal != 6 {
		t.Fatalf("expected PrunedTotal=6, got %d", p.Stats().PrunedTotal)
	}
}

func TestSetRetainBlock(t *testing.T) {
	p := NewTriePruner(PrunerConfig{KeepBlocks: 128})

	if p.RetainBlock() != 0 {
		t.Fatalf("expected initial retain block 0, got %d", p.RetainBlock())
	}

	p.SetRetainBlock(1000)
	if p.RetainBlock() != 1000 {
		t.Fatalf("expected retain block 1000, got %d", p.RetainBlock())
	}
}

func TestReset(t *testing.T) {
	p := NewTriePruner(PrunerConfig{BatchSize: 100})

	p.MarkLive(hashFromByte(0x01))
	p.MarkDead(hashFromByte(0x02))
	p.SetRetainBlock(500)

	// Prune so PrunedTotal is non-zero.
	p.Prune()

	p.Reset()

	stats := p.Stats()
	if stats.LiveNodes != 0 {
		t.Fatalf("expected 0 live nodes after reset, got %d", stats.LiveNodes)
	}
	if stats.DeadNodes != 0 {
		t.Fatalf("expected 0 dead nodes after reset, got %d", stats.DeadNodes)
	}
	if p.RetainBlock() != 0 {
		t.Fatalf("expected 0 retain block after reset, got %d", p.RetainBlock())
	}
	// PrunedTotal is preserved across resets (it's an atomic counter).
	if stats.PrunedTotal != 1 {
		t.Fatalf("expected PrunedTotal preserved as 1 after reset, got %d", stats.PrunedTotal)
	}
}

func TestBloomFilter(t *testing.T) {
	b := newPrunerBloom(1 << 20)
	key1 := []byte("node-hash-1")
	key2 := []byte("node-hash-2")

	b.add(key1)
	if !b.contains(key1) {
		t.Fatal("bloom should contain key1")
	}
	b.add(key2)
	if !b.contains(key2) {
		t.Fatal("bloom should contain key2")
	}
}

func TestBloomNoFalseNegatives(t *testing.T) {
	b := newPrunerBloom(1 << 20)
	keys := make([][]byte, 500)
	for i := range keys {
		keys[i] = []byte{byte(i >> 8), byte(i), 0xDE, 0xAD}
		b.add(keys[i])
	}
	for _, key := range keys {
		if !b.contains(key) {
			t.Fatalf("bloom false negative for key %x", key)
		}
	}
}

func TestConcurrentAccess(t *testing.T) {
	p := NewTriePruner(PrunerConfig{BatchSize: 50})

	var wg sync.WaitGroup
	// Concurrent writers.
	for g := byte(0); g < 4; g++ {
		wg.Add(1)
		go func(offset byte) {
			defer wg.Done()
			for i := byte(0); i < 50; i++ {
				h := hashFromByte(offset*50 + i)
				if i%2 == 0 {
					p.MarkLive(h)
				} else {
					p.MarkDead(h)
				}
			}
		}(g)
	}
	// Concurrent readers.
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_ = p.IsLive(hashFromByte(byte(i)))
				_ = p.PrunableNodes()
				_ = p.Stats()
			}
		}()
	}
	wg.Wait()

	// Verify consistency: every hash is in exactly one of live or dead.
	stats := p.Stats()
	if stats.LiveNodes+stats.DeadNodes != 200 {
		t.Fatalf("expected 200 total nodes, got live=%d dead=%d",
			stats.LiveNodes, stats.DeadNodes)
	}
}

func TestComparHashes(t *testing.T) {
	a := hashFromByte(0x01)
	b := hashFromByte(0x02)
	c := hashFromByte(0x01)

	if comparHashes(a, b) >= 0 {
		t.Fatal("expected a < b")
	}
	if comparHashes(b, a) <= 0 {
		t.Fatal("expected b > a")
	}
	if comparHashes(a, c) != 0 {
		t.Fatal("expected a == c")
	}
}

func TestConfigKeepBlocks(t *testing.T) {
	p := NewTriePruner(PrunerConfig{KeepBlocks: 256})
	if p.config.KeepBlocks != 256 {
		t.Fatalf("expected KeepBlocks=256, got %d", p.config.KeepBlocks)
	}
}

func TestConfigMaxPendingNodes(t *testing.T) {
	p := NewTriePruner(PrunerConfig{MaxPendingNodes: 10000})
	if p.config.MaxPendingNodes != 10000 {
		t.Fatalf("expected MaxPendingNodes=10000, got %d", p.config.MaxPendingNodes)
	}
}
