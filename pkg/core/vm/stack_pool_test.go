package vm

import (
	"math/big"
	"sync"
	"testing"
)

// TestStackPool_GetPut verifies basic pool get/put cycle.
func TestStackPool_GetPut(t *testing.T) {
	pool := NewStackPool()

	s1 := pool.Get()
	if s1 == nil {
		t.Fatal("Get returned nil")
	}
	if s1.Len() != 0 {
		t.Errorf("Get returned stack with %d items, want 0", s1.Len())
	}

	// Use the stack.
	s1.Push(big.NewInt(42))
	s1.Push(big.NewInt(99))

	// Return to pool.
	pool.Put(s1)

	// Get should return a clean stack (may or may not be the same pointer).
	s2 := pool.Get()
	if s2.Len() != 0 {
		t.Errorf("Get after Put returned stack with %d items, want 0", s2.Len())
	}
	pool.Put(s2)
}

// TestStackPool_Stats verifies pool statistics tracking.
func TestStackPool_Stats(t *testing.T) {
	pool := NewStackPool()

	// First get triggers an allocation.
	s1 := pool.Get()
	stats := pool.Stats()
	if stats.Allocations != 1 {
		t.Errorf("allocations = %d, want 1", stats.Allocations)
	}
	if stats.Reuses != 1 {
		t.Errorf("reuses = %d, want 1", stats.Reuses)
	}

	// Return and re-get: second get should reuse.
	pool.Put(s1)
	_ = pool.Get()
	stats = pool.Stats()
	if stats.Returns != 1 {
		t.Errorf("returns = %d, want 1", stats.Returns)
	}
	if stats.Reuses != 2 {
		t.Errorf("reuses = %d, want 2", stats.Reuses)
	}
}

// TestStackPool_Concurrent verifies thread safety.
func TestStackPool_Concurrent(t *testing.T) {
	pool := NewStackPool()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			s := pool.Get()
			s.Push(big.NewInt(int64(v)))
			s.Push(big.NewInt(int64(v + 1)))
			_, _ = s.Pop()
			pool.Put(s)
		}(i)
	}
	wg.Wait()

	stats := pool.Stats()
	if stats.Reuses != 100 {
		t.Errorf("reuses = %d, want 100", stats.Reuses)
	}
	if stats.Returns != 100 {
		t.Errorf("returns = %d, want 100", stats.Returns)
	}
}

// TestStackPool_PutNil does not panic.
func TestStackPool_PutNil(t *testing.T) {
	pool := NewStackPool()
	pool.Put(nil) // should not panic
}

// TestStackPoolStats_HitRate verifies hit rate calculation.
func TestStackPoolStats_HitRate(t *testing.T) {
	s := StackPoolStats{Allocations: 0, Reuses: 0}
	if s.HitRate() != 0 {
		t.Errorf("empty stats HitRate = %f, want 0", s.HitRate())
	}

	s = StackPoolStats{Allocations: 10, Reuses: 100}
	expected := 0.9
	if hr := s.HitRate(); hr != expected {
		t.Errorf("HitRate = %f, want %f", hr, expected)
	}

	// All misses.
	s = StackPoolStats{Allocations: 50, Reuses: 50}
	if hr := s.HitRate(); hr != 0 {
		t.Errorf("all-miss HitRate = %f, want 0", hr)
	}
}

// TestStackProfiler_Basic verifies profiler tracks depth correctly.
func TestStackProfiler_Basic(t *testing.T) {
	p := NewStackProfiler()

	// Simulate a sequence of opcodes at varying depths.
	p.RecordStep(PUSH1, 0)
	p.RecordStep(PUSH1, 1)
	p.RecordStep(ADD, 2)
	p.RecordStep(POP, 1)

	if p.MaxDepth() != 2 {
		t.Errorf("MaxDepth = %d, want 2", p.MaxDepth())
	}
	if p.MinDepth() != 0 {
		t.Errorf("MinDepth = %d, want 0", p.MinDepth())
	}
	if p.TotalOps() != 4 {
		t.Errorf("TotalOps = %d, want 4", p.TotalOps())
	}
	// Average: (0+1+2+1)/4 = 1.0
	if avg := p.AverageDepth(); avg != 1.0 {
		t.Errorf("AverageDepth = %f, want 1.0", avg)
	}
	if p.PeakOpcode() != ADD {
		t.Errorf("PeakOpcode = %s, want ADD", p.PeakOpcode())
	}
}

// TestStackProfiler_Histogram verifies depth histogram.
func TestStackProfiler_Histogram(t *testing.T) {
	p := NewStackProfiler()

	p.RecordStep(PUSH1, 0)
	p.RecordStep(PUSH1, 0)
	p.RecordStep(ADD, 1)
	p.RecordStep(ADD, 2)
	p.RecordStep(ADD, 2)

	hist := p.DepthHistogram()
	if len(hist) < 3 {
		t.Fatalf("histogram too short: %d", len(hist))
	}
	if hist[0] != 2 {
		t.Errorf("hist[0] = %d, want 2", hist[0])
	}
	if hist[1] != 1 {
		t.Errorf("hist[1] = %d, want 1", hist[1])
	}
	if hist[2] != 2 {
		t.Errorf("hist[2] = %d, want 2", hist[2])
	}
}

// TestStackProfiler_OpcodeStats verifies per-opcode stats.
func TestStackProfiler_OpcodeStats(t *testing.T) {
	p := NewStackProfiler()

	p.RecordStep(ADD, 3)
	p.RecordStep(ADD, 5)
	p.RecordStep(ADD, 1)
	p.RecordStep(MUL, 10)

	addStats := p.OpcodeStats(ADD)
	if addStats.Count != 3 {
		t.Errorf("ADD count = %d, want 3", addStats.Count)
	}
	if addStats.MaxDepth != 5 {
		t.Errorf("ADD maxDepth = %d, want 5", addStats.MaxDepth)
	}
	if addStats.MinDepth != 1 {
		t.Errorf("ADD minDepth = %d, want 1", addStats.MinDepth)
	}

	mulStats := p.OpcodeStats(MUL)
	if mulStats.Count != 1 {
		t.Errorf("MUL count = %d, want 1", mulStats.Count)
	}
	if mulStats.MaxDepth != 10 {
		t.Errorf("MUL maxDepth = %d, want 10", mulStats.MaxDepth)
	}
}

// TestStackProfiler_Empty verifies the profiler handles no data.
func TestStackProfiler_Empty(t *testing.T) {
	p := NewStackProfiler()
	if p.MaxDepth() != 0 {
		t.Errorf("MaxDepth on empty = %d, want 0", p.MaxDepth())
	}
	if p.MinDepth() != 0 {
		t.Errorf("MinDepth on empty = %d, want 0", p.MinDepth())
	}
	if p.AverageDepth() != 0 {
		t.Errorf("AverageDepth on empty = %f, want 0", p.AverageDepth())
	}
	hist := p.DepthHistogram()
	if len(hist) != 0 {
		t.Errorf("histogram on empty has %d entries, want 0", len(hist))
	}
}

// TestStackProfiler_String verifies the string representation.
func TestStackProfiler_String(t *testing.T) {
	p := NewStackProfiler()
	p.RecordStep(PUSH1, 5)
	s := p.String()
	if s == "" {
		t.Error("String() returned empty")
	}
}

// TestEVMStack_PeekAt verifies PeekAt with absolute indexing.
func TestEVMStack_PeekAt(t *testing.T) {
	s := NewEVMStack()
	s.Push(big.NewInt(10))
	s.Push(big.NewInt(20))
	s.Push(big.NewInt(30))

	// Bottom element (index 0).
	val, err := s.PeekAt(0)
	if err != nil {
		t.Fatalf("PeekAt(0): %v", err)
	}
	if val.Int64() != 10 {
		t.Errorf("PeekAt(0) = %d, want 10", val.Int64())
	}

	// Top element (index 2).
	val, err = s.PeekAt(2)
	if err != nil {
		t.Fatalf("PeekAt(2): %v", err)
	}
	if val.Int64() != 30 {
		t.Errorf("PeekAt(2) = %d, want 30", val.Int64())
	}

	// Out of bounds.
	_, err = s.PeekAt(3)
	if err == nil {
		t.Error("PeekAt(3) should fail on 3-element stack")
	}
	_, err = s.PeekAt(-1)
	if err == nil {
		t.Error("PeekAt(-1) should fail")
	}
}

// TestEVMStack_PeekBottom verifies PeekBottom.
func TestEVMStack_PeekBottom(t *testing.T) {
	s := NewEVMStack()

	_, err := s.PeekBottom()
	if err == nil {
		t.Error("PeekBottom on empty should fail")
	}

	s.Push(big.NewInt(100))
	s.Push(big.NewInt(200))

	val, err := s.PeekBottom()
	if err != nil {
		t.Fatalf("PeekBottom: %v", err)
	}
	if val.Int64() != 100 {
		t.Errorf("PeekBottom = %d, want 100", val.Int64())
	}
}

// TestEVMStack_SwapAt verifies SwapAt with absolute indexing.
func TestEVMStack_SwapAt(t *testing.T) {
	s := NewEVMStack()
	s.Push(big.NewInt(10))
	s.Push(big.NewInt(20))
	s.Push(big.NewInt(30))

	// Swap bottom (0) with top (2).
	if err := s.SwapAt(0, 2); err != nil {
		t.Fatalf("SwapAt(0, 2): %v", err)
	}

	bottom, _ := s.PeekAt(0)
	top, _ := s.Peek()
	if bottom.Int64() != 30 {
		t.Errorf("after swap, bottom = %d, want 30", bottom.Int64())
	}
	if top.Int64() != 10 {
		t.Errorf("after swap, top = %d, want 10", top.Int64())
	}

	// Out of bounds.
	if err := s.SwapAt(0, 5); err == nil {
		t.Error("SwapAt(0, 5) should fail on 3-element stack")
	}
	if err := s.SwapAt(-1, 0); err == nil {
		t.Error("SwapAt(-1, 0) should fail")
	}
}

// TestEVMStack_DupAt verifies DupAt with absolute indexing.
func TestEVMStack_DupAt(t *testing.T) {
	s := NewEVMStack()
	s.Push(big.NewInt(10))
	s.Push(big.NewInt(20))
	s.Push(big.NewInt(30))

	// Duplicate bottom element.
	if err := s.DupAt(0); err != nil {
		t.Fatalf("DupAt(0): %v", err)
	}
	if s.Len() != 4 {
		t.Fatalf("Len = %d, want 4", s.Len())
	}
	top, _ := s.Peek()
	if top.Int64() != 10 {
		t.Errorf("after DupAt(0), top = %d, want 10", top.Int64())
	}

	// DupAt creates independent copy.
	top.SetInt64(999)
	orig, _ := s.PeekAt(0)
	if orig.Int64() != 10 {
		t.Error("DupAt should create independent copy")
	}

	// Out of bounds.
	if err := s.DupAt(10); err == nil {
		t.Error("DupAt(10) should fail on 4-element stack")
	}
}

// TestEVMStack_DupAt_Overflow verifies DupAt overflow protection.
func TestEVMStack_DupAt_Overflow(t *testing.T) {
	s := NewEVMStack()
	for i := 0; i < 1024; i++ {
		s.Push(big.NewInt(int64(i)))
	}
	if err := s.DupAt(0); err == nil {
		t.Error("DupAt on full stack should fail")
	}
}

// TestEVMStack_PopN verifies PopN.
func TestEVMStack_PopN(t *testing.T) {
	s := NewEVMStack()
	s.Push(big.NewInt(1))
	s.Push(big.NewInt(2))
	s.Push(big.NewInt(3))

	vals, err := s.PopN(2)
	if err != nil {
		t.Fatalf("PopN(2): %v", err)
	}
	if len(vals) != 2 {
		t.Fatalf("PopN returned %d values, want 2", len(vals))
	}
	// Top first: 3, then 2.
	if vals[0].Int64() != 3 {
		t.Errorf("vals[0] = %d, want 3", vals[0].Int64())
	}
	if vals[1].Int64() != 2 {
		t.Errorf("vals[1] = %d, want 2", vals[1].Int64())
	}
	if s.Len() != 1 {
		t.Errorf("Len = %d, want 1", s.Len())
	}

	// PopN more than available.
	_, err = s.PopN(5)
	if err == nil {
		t.Error("PopN(5) should fail on 1-element stack")
	}

	// PopN(0) is valid.
	vals, err = s.PopN(0)
	if err != nil {
		t.Fatalf("PopN(0): %v", err)
	}
	if len(vals) != 0 {
		t.Errorf("PopN(0) returned %d values, want 0", len(vals))
	}
}

// TestEVMStack_PushN verifies PushN.
func TestEVMStack_PushN(t *testing.T) {
	s := NewEVMStack()
	err := s.PushN(big.NewInt(1), big.NewInt(2), big.NewInt(3))
	if err != nil {
		t.Fatalf("PushN: %v", err)
	}
	if s.Len() != 3 {
		t.Fatalf("Len = %d, want 3", s.Len())
	}
	// First pushed = bottom.
	bottom, _ := s.PeekAt(0)
	if bottom.Int64() != 1 {
		t.Errorf("bottom = %d, want 1", bottom.Int64())
	}
	top, _ := s.Peek()
	if top.Int64() != 3 {
		t.Errorf("top = %d, want 3", top.Int64())
	}
}

// TestEVMStack_PushN_Overflow verifies PushN overflow protection.
func TestEVMStack_PushN_Overflow(t *testing.T) {
	s := NewEVMStack()
	// Fill to 1023.
	for i := 0; i < 1023; i++ {
		s.Push(big.NewInt(int64(i)))
	}
	// Pushing 2 would exceed limit.
	err := s.PushN(big.NewInt(1), big.NewInt(2))
	if err == nil {
		t.Error("PushN should fail when exceeding limit")
	}
	if s.Len() != 1023 {
		t.Errorf("Len = %d, want 1023 (stack unchanged after failed PushN)", s.Len())
	}
}

// TestEVMStack_Snapshot verifies Snapshot creates independent copies.
func TestEVMStack_Snapshot(t *testing.T) {
	s := NewEVMStack()
	s.Push(big.NewInt(10))
	s.Push(big.NewInt(20))

	snap := s.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("snapshot len = %d, want 2", len(snap))
	}
	if snap[0].Int64() != 10 || snap[1].Int64() != 20 {
		t.Errorf("snapshot = [%d, %d], want [10, 20]", snap[0].Int64(), snap[1].Int64())
	}

	// Modifying snapshot should not affect stack.
	snap[0].SetInt64(999)
	val, _ := s.PeekAt(0)
	if val.Int64() != 10 {
		t.Error("Snapshot should create independent copies")
	}
}

// TestEVMStack_Depth verifies the Depth alias.
func TestEVMStack_Depth(t *testing.T) {
	s := NewEVMStack()
	if s.Depth() != 0 {
		t.Errorf("Depth on empty = %d, want 0", s.Depth())
	}
	s.Push(big.NewInt(1))
	s.Push(big.NewInt(2))
	if s.Depth() != 2 {
		t.Errorf("Depth = %d, want 2", s.Depth())
	}
}
