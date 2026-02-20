package vm

// stack_pool.go implements a memory-efficient pooled stack allocator with
// comprehensive overflow/underflow protection, deep peek/swap/dup operations,
// and stack profiling for EVM execution analysis.

import (
	"errors"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
)

// Pool-level errors.
var (
	ErrPoolExhausted = errors.New("stack pool: no stacks available")
)

// StackPool is a sync.Pool-backed allocator for EVMStack instances.
// It reduces GC pressure by reusing stacks across EVM call frames.
type StackPool struct {
	pool sync.Pool

	// Metrics (accessed atomically).
	allocCount  uint64
	reuseCount  uint64
	returnCount uint64
}

// NewStackPool creates a new StackPool.
func NewStackPool() *StackPool {
	sp := &StackPool{}
	sp.pool = sync.Pool{
		New: func() interface{} {
			atomic.AddUint64(&sp.allocCount, 1)
			return NewEVMStack()
		},
	}
	return sp
}

// Get retrieves a stack from the pool, allocating one if the pool is empty.
// The returned stack is always in a clean (empty) state.
func (sp *StackPool) Get() *EVMStack {
	s := sp.pool.Get().(*EVMStack)
	if s.top > 0 {
		s.Reset()
	}
	atomic.AddUint64(&sp.reuseCount, 1)
	return s
}

// Put returns a stack to the pool for reuse. The stack is reset before
// being placed back into the pool.
func (sp *StackPool) Put(s *EVMStack) {
	if s == nil {
		return
	}
	s.Reset()
	atomic.AddUint64(&sp.returnCount, 1)
	sp.pool.Put(s)
}

// Stats returns pool usage statistics.
func (sp *StackPool) Stats() StackPoolStats {
	return StackPoolStats{
		Allocations: atomic.LoadUint64(&sp.allocCount),
		Reuses:      atomic.LoadUint64(&sp.reuseCount),
		Returns:     atomic.LoadUint64(&sp.returnCount),
	}
}

// StackPoolStats holds pool usage metrics.
type StackPoolStats struct {
	Allocations uint64 // total new allocations
	Reuses      uint64 // total Get() calls (includes allocations)
	Returns     uint64 // total Put() calls
}

// HitRate returns the fraction of Get() calls served from the pool (0.0 to 1.0).
// Returns 0 if no Gets have been made.
func (s StackPoolStats) HitRate() float64 {
	if s.Reuses == 0 {
		return 0
	}
	// Allocations are new stacks (cache misses), reuses are all Gets.
	// Hit rate = (reuses - allocations) / reuses.
	if s.Allocations >= s.Reuses {
		return 0
	}
	return float64(s.Reuses-s.Allocations) / float64(s.Reuses)
}

// StackProfiler tracks per-opcode stack depth statistics during execution.
// It is useful for analyzing stack usage patterns in EVM contracts.
type StackProfiler struct {
	maxDepth    int
	minDepth    int
	totalOps    uint64
	depthSum    uint64
	peakOpcode  OpCode // opcode that produced the max depth
	depthHist   [1025]uint32 // histogram: depthHist[d] = number of steps at depth d

	// Per-opcode depth tracking.
	opcodeMaxDepth [256]int
	opcodeMinDepth [256]int
	opcodeCount    [256]uint64
}

// NewStackProfiler creates a new StackProfiler.
func NewStackProfiler() *StackProfiler {
	p := &StackProfiler{
		minDepth: evmStackLimit + 1,
	}
	for i := range p.opcodeMinDepth {
		p.opcodeMinDepth[i] = evmStackLimit + 1
	}
	return p
}

// RecordStep records a single opcode execution step for profiling.
func (p *StackProfiler) RecordStep(op OpCode, depth int) {
	p.totalOps++
	p.depthSum += uint64(depth)

	if depth > p.maxDepth {
		p.maxDepth = depth
		p.peakOpcode = op
	}
	if depth < p.minDepth {
		p.minDepth = depth
	}

	// Update histogram (clamp to bounds).
	if depth >= 0 && depth <= evmStackLimit {
		p.depthHist[depth]++
	}

	// Per-opcode tracking.
	idx := byte(op)
	p.opcodeCount[idx]++
	if depth > p.opcodeMaxDepth[idx] {
		p.opcodeMaxDepth[idx] = depth
	}
	if depth < p.opcodeMinDepth[idx] {
		p.opcodeMinDepth[idx] = depth
	}
}

// MaxDepth returns the maximum stack depth observed.
func (p *StackProfiler) MaxDepth() int { return p.maxDepth }

// MinDepth returns the minimum stack depth observed.
func (p *StackProfiler) MinDepth() int {
	if p.totalOps == 0 {
		return 0
	}
	return p.minDepth
}

// TotalOps returns the total number of opcode steps recorded.
func (p *StackProfiler) TotalOps() uint64 { return p.totalOps }

// AverageDepth returns the average stack depth across all recorded steps.
func (p *StackProfiler) AverageDepth() float64 {
	if p.totalOps == 0 {
		return 0
	}
	return float64(p.depthSum) / float64(p.totalOps)
}

// PeakOpcode returns the opcode that produced the maximum stack depth.
func (p *StackProfiler) PeakOpcode() OpCode { return p.peakOpcode }

// DepthHistogram returns the depth histogram as a slice.
// Index i holds the number of steps where stack depth was exactly i.
func (p *StackProfiler) DepthHistogram() []uint32 {
	if p.totalOps == 0 {
		return nil
	}
	// Find the highest non-zero bucket.
	maxBucket := -1
	for i := evmStackLimit; i >= 0; i-- {
		if p.depthHist[i] > 0 {
			maxBucket = i
			break
		}
	}
	if maxBucket < 0 {
		return nil
	}
	result := make([]uint32, maxBucket+1)
	copy(result, p.depthHist[:maxBucket+1])
	return result
}

// OpcodeStats returns the profiling stats for a specific opcode.
func (p *StackProfiler) OpcodeStats(op OpCode) OpcodeDepthStats {
	idx := byte(op)
	minD := p.opcodeMinDepth[idx]
	if p.opcodeCount[idx] == 0 {
		minD = 0
	}
	return OpcodeDepthStats{
		Opcode:   op,
		Count:    p.opcodeCount[idx],
		MaxDepth: p.opcodeMaxDepth[idx],
		MinDepth: minD,
	}
}

// OpcodeDepthStats holds per-opcode stack depth statistics.
type OpcodeDepthStats struct {
	Opcode   OpCode
	Count    uint64
	MaxDepth int
	MinDepth int
}

// String returns a formatted summary of the profile.
func (p *StackProfiler) String() string {
	return fmt.Sprintf("StackProfile: ops=%d maxDepth=%d minDepth=%d avgDepth=%.1f peakOp=%s",
		p.totalOps, p.maxDepth, p.MinDepth(), p.AverageDepth(), p.peakOpcode)
}

// PeekAt returns the element at the given absolute index (0 = bottom) from
// the EVMStack without removing it. Returns an error if the index is out of
// bounds.
func (s *EVMStack) PeekAt(index int) (*big.Int, error) {
	if index < 0 || index >= s.top {
		return nil, fmt.Errorf("%w: index %d out of range [0, %d)",
			ErrEVMStackUnderflow, index, s.top)
	}
	return s.data[index], nil
}

// PeekBottom returns the bottom element of the stack.
func (s *EVMStack) PeekBottom() (*big.Int, error) {
	if s.top == 0 {
		return nil, ErrEVMStackUnderflow
	}
	return s.data[0], nil
}

// SwapAt swaps the elements at absolute positions i and j (0 = bottom).
// Returns an error if either index is out of bounds.
func (s *EVMStack) SwapAt(i, j int) error {
	if i < 0 || i >= s.top {
		return fmt.Errorf("%w: index %d out of range [0, %d)",
			ErrEVMSwapOutOfRange, i, s.top)
	}
	if j < 0 || j >= s.top {
		return fmt.Errorf("%w: index %d out of range [0, %d)",
			ErrEVMSwapOutOfRange, j, s.top)
	}
	s.data[i], s.data[j] = s.data[j], s.data[i]
	return nil
}

// DupAt duplicates the element at absolute index (0 = bottom) and pushes
// the copy onto the top of the stack. Returns an error on overflow or
// invalid index.
func (s *EVMStack) DupAt(index int) error {
	if index < 0 || index >= s.top {
		return fmt.Errorf("%w: index %d out of range [0, %d)",
			ErrEVMDupOutOfRange, index, s.top)
	}
	if s.top >= evmStackLimit {
		return ErrEVMStackOverflow
	}
	s.data[s.top] = new(big.Int).Set(s.data[index])
	s.top++
	return nil
}

// PopN pops n elements from the top and returns them (top first).
// Returns an error if the stack has fewer than n elements.
func (s *EVMStack) PopN(n int) ([]*big.Int, error) {
	if n < 0 {
		return nil, fmt.Errorf("PopN: negative count %d", n)
	}
	if s.top < n {
		return nil, fmt.Errorf("%w: need %d elements, have %d",
			ErrEVMStackUnderflow, n, s.top)
	}
	result := make([]*big.Int, n)
	for i := 0; i < n; i++ {
		s.top--
		result[i] = s.data[s.top]
		s.data[s.top] = nil
	}
	return result, nil
}

// PushN pushes multiple values onto the stack. The first element in the
// slice is pushed first (becomes deepest of the group).
func (s *EVMStack) PushN(vals ...*big.Int) error {
	if s.top+len(vals) > evmStackLimit {
		return fmt.Errorf("%w: pushing %d items would exceed limit (current: %d)",
			ErrEVMStackOverflow, len(vals), s.top)
	}
	for _, v := range vals {
		s.data[s.top] = new(big.Int).Set(v)
		s.top++
	}
	return nil
}

// Snapshot returns a copy of the current stack contents as a slice
// (bottom to top). Useful for tracing and debugging.
func (s *EVMStack) Snapshot() []*big.Int {
	result := make([]*big.Int, s.top)
	for i := 0; i < s.top; i++ {
		result[i] = new(big.Int).Set(s.data[i])
	}
	return result
}

// Depth is an alias for Len(), named to match EVM terminology.
func (s *EVMStack) Depth() int {
	return s.top
}
