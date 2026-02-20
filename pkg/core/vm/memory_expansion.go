package vm

// memory_expansion.go implements advanced EVM memory management: quadratic
// memory cost calculation per the Yellow Paper, memory copy operations,
// memory access bounds checking, and lazy allocation.

import (
	"errors"
	"fmt"
	"math"
)

// Memory expansion errors.
var (
	ErrMemoryAccessOutOfBounds = errors.New("memory: access out of allocated bounds")
	ErrMemorySizeOverflow      = errors.New("memory: requested size causes overflow")
	ErrMemoryGasOverflow       = errors.New("memory: gas cost overflow")
	ErrMemoryCopySrcOverlap    = errors.New("memory: MCOPY source and destination overlap requires safe copy")
	// ErrMemoryLimitExceeded is declared in gigagas.go
)

// LazyMemoryDefaultLimit is the default maximum memory that can be lazily
// allocated (32 MiB), matching the DoS protection in the EVM.
const LazyMemoryDefaultLimit = 32 * 1024 * 1024

// MemoryExpander handles EVM memory expansion with proper gas accounting
// per the Yellow Paper quadratic cost formula. It separates the cost
// calculation from the actual allocation, enabling pre-flight gas checks.
type MemoryExpander struct {
	lastWordCount uint64 // last allocated memory in 32-byte words
	lastGasCost   uint64 // total gas cost paid for memory so far
}

// NewMemoryExpander creates a new MemoryExpander.
func NewMemoryExpander() *MemoryExpander {
	return &MemoryExpander{}
}

// CurrentWords returns the current memory size in 32-byte words.
func (me *MemoryExpander) CurrentWords() uint64 {
	return me.lastWordCount
}

// CurrentBytes returns the current memory size in bytes.
func (me *MemoryExpander) CurrentBytes() uint64 {
	return me.lastWordCount * 32
}

// TotalGasCost returns the cumulative gas paid for memory expansion.
func (me *MemoryExpander) TotalGasCost() uint64 {
	return me.lastGasCost
}

// quadraticCost calculates the Yellow Paper memory cost for a given number
// of 32-byte words: C_mem(a) = G_memory * a + floor(a^2 / 512).
// Returns (cost, true) on success or (0, false) if the computation overflows.
func quadraticCost(words uint64) (uint64, bool) {
	if words == 0 {
		return 0, true
	}
	// Overflow guard: words*words overflows when words > ~4.29 billion.
	// At 181_000 words (5.8 MB) the gas cost already exceeds any block limit.
	if words > math.MaxUint64/words {
		return 0, false
	}
	quadratic := (words * words) / 512
	linear := words * 3 // GasMemory = 3
	total := linear + quadratic
	if total < linear {
		return 0, false // overflow in addition
	}
	return total, true
}

// ExpansionCost computes the incremental gas cost to expand memory so that
// at least newBytes are allocated. Returns (0, nil) if no expansion is needed.
// The memory is always rounded up to a 32-byte word boundary.
func (me *MemoryExpander) ExpansionCost(newBytes uint64) (uint64, error) {
	if newBytes == 0 {
		return 0, nil
	}
	currentBytes := me.lastWordCount * 32
	if newBytes <= currentBytes {
		return 0, nil
	}

	// Round up to 32-byte boundary.
	if newBytes > math.MaxUint64-31 {
		return 0, ErrMemorySizeOverflow
	}
	newWords := (newBytes + 31) / 32

	newCost, ok := quadraticCost(newWords)
	if !ok {
		return 0, ErrMemoryGasOverflow
	}

	// Incremental cost is the difference between the new total cost and
	// the cost already paid.
	if newCost < me.lastGasCost {
		// Should never happen because newWords >= lastWordCount.
		return 0, ErrMemoryGasOverflow
	}
	return newCost - me.lastGasCost, nil
}

// Expand marks memory as expanded to at least newBytes. The caller must
// have already charged the gas returned by ExpansionCost. The actual
// byte slice allocation is handled elsewhere (in Memory or MemoryManager).
func (me *MemoryExpander) Expand(newBytes uint64) error {
	if newBytes == 0 {
		return nil
	}
	if newBytes > math.MaxUint64-31 {
		return ErrMemorySizeOverflow
	}
	newWords := (newBytes + 31) / 32
	if newWords <= me.lastWordCount {
		return nil
	}
	newCost, ok := quadraticCost(newWords)
	if !ok {
		return ErrMemoryGasOverflow
	}
	me.lastWordCount = newWords
	me.lastGasCost = newCost
	return nil
}

// LazyMemory is an EVM memory implementation that defers physical allocation
// until the first write to a region. It tracks which pages have been
// materialized to reduce memory usage for contracts that declare large
// offsets but only access small regions.
type LazyMemory struct {
	store    []byte
	size     uint64 // logical size (always a multiple of 32)
	limit    uint64 // maximum allowed size
	expander *MemoryExpander
}

// NewLazyMemory creates a LazyMemory with the default 32 MiB limit.
func NewLazyMemory() *LazyMemory {
	return &LazyMemory{
		limit:    LazyMemoryDefaultLimit,
		expander: NewMemoryExpander(),
	}
}

// NewLazyMemoryWithLimit creates a LazyMemory with a custom size limit.
func NewLazyMemoryWithLimit(limit uint64) *LazyMemory {
	return &LazyMemory{
		limit:    limit,
		expander: NewMemoryExpander(),
	}
}

// Size returns the current logical memory size in bytes.
func (lm *LazyMemory) Size() uint64 {
	return lm.size
}

// Expander returns the underlying MemoryExpander for gas queries.
func (lm *LazyMemory) Expander() *MemoryExpander {
	return lm.expander
}

// EnsureSize ensures memory is at least newSize bytes. Returns the
// incremental gas cost for expansion. Does not allocate the backing
// store until data is written via Store.
func (lm *LazyMemory) EnsureSize(newSize uint64) (uint64, error) {
	if newSize <= lm.size {
		return 0, nil
	}
	if newSize > lm.limit {
		return 0, fmt.Errorf("%w: %d > %d", ErrMemoryLimitExceeded, newSize, lm.limit)
	}

	gasCost, err := lm.expander.ExpansionCost(newSize)
	if err != nil {
		return 0, err
	}

	if err := lm.expander.Expand(newSize); err != nil {
		return 0, err
	}

	// Round up to 32-byte boundary for logical size.
	if newSize > math.MaxUint64-31 {
		return 0, ErrMemorySizeOverflow
	}
	lm.size = ((newSize + 31) / 32) * 32
	return gasCost, nil
}

// materialize ensures the backing store covers [0, lm.size).
func (lm *LazyMemory) materialize() {
	if uint64(len(lm.store)) < lm.size {
		newStore := make([]byte, lm.size)
		copy(newStore, lm.store)
		lm.store = newStore
	}
}

// Store writes data at offset. The region [offset, offset+len(data))
// must be within the current logical size.
func (lm *LazyMemory) Store(offset uint64, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	end := offset + uint64(len(data))
	if end < offset {
		return ErrMemorySizeOverflow
	}
	if end > lm.size {
		return fmt.Errorf("%w: write at [%d, %d), logical size %d",
			ErrMemoryAccessOutOfBounds, offset, end, lm.size)
	}
	lm.materialize()
	copy(lm.store[offset:end], data)
	return nil
}

// Load reads size bytes from offset and returns a copy.
func (lm *LazyMemory) Load(offset, size uint64) ([]byte, error) {
	if size == 0 {
		return nil, nil
	}
	end := offset + size
	if end < offset {
		return nil, ErrMemorySizeOverflow
	}
	if end > lm.size {
		return nil, fmt.Errorf("%w: read at [%d, %d), logical size %d",
			ErrMemoryAccessOutOfBounds, offset, end, lm.size)
	}
	lm.materialize()
	out := make([]byte, size)
	copy(out, lm.store[offset:end])
	return out, nil
}

// CopySafe performs MCOPY-style memory copy that correctly handles
// overlapping regions by using a temporary buffer when src and dst overlap.
// Both regions must be within the current logical size.
func (lm *LazyMemory) CopySafe(dst, src, size uint64) error {
	if size == 0 {
		return nil
	}

	srcEnd := src + size
	dstEnd := dst + size
	if srcEnd < src || dstEnd < dst {
		return ErrMemorySizeOverflow
	}

	maxEnd := srcEnd
	if dstEnd > maxEnd {
		maxEnd = dstEnd
	}
	if maxEnd > lm.size {
		return fmt.Errorf("%w: copy requires %d bytes, logical size %d",
			ErrMemoryAccessOutOfBounds, maxEnd, lm.size)
	}
	lm.materialize()

	// Go's built-in copy handles overlapping slices correctly (it uses
	// memmove semantics), so no temporary buffer is needed.
	copy(lm.store[dst:dstEnd], lm.store[src:srcEnd])
	return nil
}

// Data returns the full backing slice. May be nil or shorter than Size()
// if no writes have materialized the store.
func (lm *LazyMemory) Data() []byte {
	lm.materialize()
	return lm.store
}

// CalcMemoryExpansionGas is a standalone utility that computes the gas cost
// for expanding memory from currentSize to newSize without mutating any state.
// It returns (gasCost, true) on success or (0, false) on overflow.
func CalcMemoryExpansionGas(currentSize, newSize uint64) (uint64, bool) {
	if newSize <= currentSize {
		return 0, true
	}
	if newSize > math.MaxUint64-31 {
		return 0, false
	}
	newWords := (newSize + 31) / 32
	oldWords := uint64(0)
	if currentSize > 0 {
		oldWords = (currentSize + 31) / 32
	}

	newCost, ok := quadraticCost(newWords)
	if !ok {
		return 0, false
	}
	oldCost, ok := quadraticCost(oldWords)
	if !ok {
		return 0, false
	}
	if newCost < oldCost {
		return 0, false
	}
	return newCost - oldCost, true
}
