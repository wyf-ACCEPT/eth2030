package vm

import (
	"errors"
	"fmt"
)

// Memory manager errors.
var (
	ErrMemoryOutOfBounds  = errors.New("memory: out of bounds access")
	ErrMemoryOverflow     = errors.New("memory: size overflow")
	ErrMemoryExceedsLimit = errors.New("memory: exceeds maximum size limit")
)

// MemoryManagerMaxSize is the maximum allowed memory size (32 MiB).
const MemoryManagerMaxSize = 32 * 1024 * 1024

// MemoryManager tracks EVM memory regions with gas accounting.
// It maintains a byte-addressable memory that grows in 32-byte word increments.
type MemoryManager struct {
	store       []byte
	totalGasUsed uint64
}

// NewMemoryManager creates a new MemoryManager instance.
func NewMemoryManager() *MemoryManager {
	return &MemoryManager{}
}

// Size returns the current memory size in bytes (always a multiple of 32).
func (mm *MemoryManager) Size() uint64 {
	return uint64(len(mm.store))
}

// MemoryExpansionCost computes the gas cost for expanding memory to newSize bytes.
// The formula is: cost = (newWords * newWords) / 512 + 3 * newWords
// where newWords = ceil(newSize / 32).
// Returns 0 if newSize does not exceed current size.
func (mm *MemoryManager) MemoryExpansionCost(newSize uint64) uint64 {
	if newSize <= mm.Size() {
		return 0
	}
	return memoryGasCost(newSize)
}

// memoryGasCost computes the absolute gas cost for a given memory size.
func memoryGasCost(size uint64) uint64 {
	if size == 0 {
		return 0
	}
	words := (size + 31) / 32
	return (words*words)/512 + 3*words
}

// Allocate ensures that memory is at least offset+size bytes and returns
// the incremental gas cost for any required expansion. The memory is always
// expanded to a multiple of 32 bytes.
func (mm *MemoryManager) Allocate(offset, size uint64) (uint64, error) {
	if size == 0 {
		return 0, nil
	}

	// Check for overflow in offset + size.
	newEnd := offset + size
	if newEnd < offset {
		return 0, ErrMemoryOverflow
	}

	// Round up to nearest 32-byte word boundary.
	rounded := roundUpTo32(newEnd)
	if rounded < newEnd {
		// Overflow during rounding.
		return 0, ErrMemoryOverflow
	}

	if rounded > MemoryManagerMaxSize {
		return 0, ErrMemoryExceedsLimit
	}

	currentSize := mm.Size()
	if rounded <= currentSize {
		return 0, nil
	}

	// Calculate incremental gas cost.
	oldCost := memoryGasCost(currentSize)
	newCost := memoryGasCost(rounded)
	gasCost := newCost - oldCost

	// Expand memory.
	mm.store = append(mm.store, make([]byte, rounded-currentSize)...)
	mm.totalGasUsed += gasCost

	return gasCost, nil
}

// Store writes data at the given offset in memory. The memory region
// [offset, offset+len(data)) must have been previously allocated.
func (mm *MemoryManager) Store(offset uint64, data []byte) error {
	if len(data) == 0 {
		return nil
	}

	end := offset + uint64(len(data))
	if end < offset {
		return ErrMemoryOverflow
	}
	if end > mm.Size() {
		return fmt.Errorf("%w: store at offset %d, size %d, memory size %d",
			ErrMemoryOutOfBounds, offset, len(data), mm.Size())
	}

	copy(mm.store[offset:end], data)
	return nil
}

// Load reads size bytes from offset and returns a copy. The region
// [offset, offset+size) must be within the current memory bounds.
func (mm *MemoryManager) Load(offset, size uint64) ([]byte, error) {
	if size == 0 {
		return nil, nil
	}

	end := offset + size
	if end < offset {
		return nil, ErrMemoryOverflow
	}
	if end > mm.Size() {
		return nil, fmt.Errorf("%w: load at offset %d, size %d, memory size %d",
			ErrMemoryOutOfBounds, offset, size, mm.Size())
	}

	// Return a copy so the caller cannot mutate internal memory.
	out := make([]byte, size)
	copy(out, mm.store[offset:end])
	return out, nil
}

// TotalGasUsed returns the cumulative gas spent on memory expansion.
func (mm *MemoryManager) TotalGasUsed() uint64 {
	return mm.totalGasUsed
}

// roundUpTo32 rounds n up to the nearest multiple of 32.
// Returns a value < n on overflow.
func roundUpTo32(n uint64) uint64 {
	if n == 0 {
		return 0
	}
	return ((n + 31) / 32) * 32
}
