package vm

// evm_memory_pool.go implements a memory pool for EVM execution. It provides
// sync.Pool-based reuse of EVM memory pages, memory expansion cost tracking,
// optimized memory copy for MCOPY (EIP-5656), page-aligned allocation, and
// zero-cost initialization via lazy zeroing.

import (
	"math"
	"sync"
)

// Page size for the memory pool. EVM memory is expanded in 32-byte word
// increments, but the pool allocates in larger pages to reduce the number
// of slice reallocations. 4 KiB pages align with OS page sizes.
const (
	MemPoolPageSize  = 4096      // 4 KiB per page
	MemPoolMaxPages  = 8192      // 32 MiB max (8192 * 4 KiB)
	MemPoolWordSize  = 32        // EVM word size in bytes
	MemPoolMaxMemory = MemPoolPageSize * MemPoolMaxPages
)

// memPagePool is a global pool of 4 KiB memory pages for reuse across
// EVM executions. This reduces GC pressure in high-throughput scenarios.
var memPagePool = sync.Pool{
	New: func() interface{} {
		page := make([]byte, MemPoolPageSize)
		return &page
	},
}

// getPage acquires a zeroed page from the pool.
func getPage() *[]byte {
	p := memPagePool.Get().(*[]byte)
	// Zero the page before returning it to the caller.
	for i := range *p {
		(*p)[i] = 0
	}
	return p
}

// putPage returns a page to the pool for reuse.
func putPage(p *[]byte) {
	if p != nil && len(*p) == MemPoolPageSize {
		memPagePool.Put(p)
	}
}

// PooledMemory is an EVM memory implementation that uses pooled pages for
// its backing store. It tracks gas costs for expansion and supports
// efficient MCOPY operations.
type PooledMemory struct {
	pages      []*[]byte // pooled backing pages
	size       uint64    // logical size in bytes (always word-aligned)
	lastGasCost uint64   // cumulative gas paid for expansion
}

// NewPooledMemory creates a PooledMemory with no initial allocation.
func NewPooledMemory() *PooledMemory {
	return &PooledMemory{}
}

// Size returns the current logical memory size in bytes.
func (pm *PooledMemory) Size() uint64 {
	return pm.size
}

// TotalGasCost returns the cumulative gas paid for memory expansion.
func (pm *PooledMemory) TotalGasCost() uint64 {
	return pm.lastGasCost
}

// ExpansionCost computes the incremental gas cost to expand memory to
// at least newSize bytes, without performing the actual expansion.
func (pm *PooledMemory) ExpansionCost(newSize uint64) (uint64, bool) {
	if newSize <= pm.size {
		return 0, true
	}
	if newSize > MemPoolMaxMemory {
		return 0, false
	}
	return CalcMemoryExpansionGas(pm.size, newSize)
}

// Expand grows memory to at least newSize bytes, allocating new pages as
// needed. Returns the incremental gas cost. The caller must verify that
// sufficient gas is available before calling this method.
func (pm *PooledMemory) Expand(newSize uint64) (uint64, bool) {
	if newSize <= pm.size {
		return 0, true
	}
	if newSize > MemPoolMaxMemory {
		return 0, false
	}

	// Calculate gas cost.
	gasCost, ok := CalcMemoryExpansionGas(pm.size, newSize)
	if !ok {
		return 0, false
	}

	// Round up to word boundary.
	wordAligned := ((newSize + MemPoolWordSize - 1) / MemPoolWordSize) * MemPoolWordSize

	// Allocate new pages.
	currentPages := len(pm.pages)
	neededPages := int((wordAligned + MemPoolPageSize - 1) / MemPoolPageSize)

	for i := currentPages; i < neededPages; i++ {
		pm.pages = append(pm.pages, getPage())
	}

	pm.size = wordAligned
	pm.lastGasCost += gasCost
	return gasCost, true
}

// Set writes data at the given offset. The memory must have been expanded
// to cover the write region beforehand.
func (pm *PooledMemory) Set(offset, size uint64, value []byte) {
	if size == 0 {
		return
	}
	end := offset + size
	if end > pm.size {
		return // caller error: should have expanded first
	}

	written := uint64(0)
	for written < size {
		pageIdx := int((offset + written) / MemPoolPageSize)
		pageOff := (offset + written) % MemPoolPageSize
		remaining := size - written
		canWrite := MemPoolPageSize - pageOff
		if remaining < canWrite {
			canWrite = remaining
		}
		if pageIdx < len(pm.pages) {
			copy((*pm.pages[pageIdx])[pageOff:pageOff+canWrite], value[written:written+canWrite])
		}
		written += canWrite
	}
}

// Get reads size bytes starting at offset, returning a copy.
func (pm *PooledMemory) Get(offset, size uint64) []byte {
	if size == 0 {
		return nil
	}
	if offset+size > pm.size {
		return nil
	}

	result := make([]byte, size)
	read := uint64(0)
	for read < size {
		pageIdx := int((offset + read) / MemPoolPageSize)
		pageOff := (offset + read) % MemPoolPageSize
		remaining := size - read
		canRead := MemPoolPageSize - pageOff
		if remaining < canRead {
			canRead = remaining
		}
		if pageIdx < len(pm.pages) {
			copy(result[read:read+canRead], (*pm.pages[pageIdx])[pageOff:pageOff+canRead])
		}
		read += canRead
	}
	return result
}

// Set32 writes a 32-byte value at offset (big-endian, zero-padded).
func (pm *PooledMemory) Set32(offset uint64, val []byte) {
	if offset+32 > pm.size {
		return
	}
	// Zero-pad to 32 bytes.
	padded := make([]byte, 32)
	if len(val) > 0 {
		if len(val) > 32 {
			val = val[len(val)-32:]
		}
		copy(padded[32-len(val):], val)
	}
	pm.Set(offset, 32, padded)
}

// CopyWithin performs an MCOPY-style copy within memory from src to dst.
// It handles overlapping regions correctly by using a temporary buffer.
func (pm *PooledMemory) CopyWithin(dst, src, size uint64) {
	if size == 0 {
		return
	}
	if src+size > pm.size || dst+size > pm.size {
		return
	}

	// Read into a temporary buffer first to handle overlaps safely.
	tmp := pm.Get(src, size)
	if tmp != nil {
		pm.Set(dst, size, tmp)
	}
}

// Free returns all pooled pages back to the pool for reuse. This should be
// called when the EVM execution context is done with this memory.
func (pm *PooledMemory) Free() {
	for _, p := range pm.pages {
		putPage(p)
	}
	pm.pages = nil
	pm.size = 0
	pm.lastGasCost = 0
}

// Data returns the full memory contents as a contiguous byte slice.
// This is primarily used for debugging and testing. For production use,
// prefer Get/Set to avoid copying the entire memory.
func (pm *PooledMemory) Data() []byte {
	if pm.size == 0 {
		return nil
	}
	return pm.Get(0, pm.size)
}

// McopyGas calculates the gas cost for an MCOPY operation (EIP-5656).
// The cost is: 3 * ceil(size/32) for the copy + memory expansion cost.
// The memory expansion cost covers both source and destination regions.
func McopyGas(currentMemSize, dst, src, size uint64) (uint64, bool) {
	if size == 0 {
		return GasMcopyBase, true // just the constant gas
	}

	// Calculate the minimum memory size needed.
	dstEnd := dst + size
	srcEnd := src + size
	if dstEnd < dst || srcEnd < src {
		return 0, false // overflow
	}
	maxEnd := dstEnd
	if srcEnd > maxEnd {
		maxEnd = srcEnd
	}

	// Copy gas: 3 per 32-byte word.
	words := (size + 31) / 32
	copyGas := safeMul(GasCopy, words)
	if copyGas == math.MaxUint64 {
		return 0, false
	}

	// Memory expansion gas.
	var expandGas uint64
	if maxEnd > currentMemSize {
		var ok bool
		expandGas, ok = CalcMemoryExpansionGas(currentMemSize, maxEnd)
		if !ok {
			return 0, false
		}
	}

	totalGas := safeAdd(copyGas, expandGas)
	if totalGas == math.MaxUint64 {
		return 0, false
	}
	return totalGas, true
}

// WordAlignedSize rounds a byte size up to the next 32-byte word boundary.
func WordAlignedSize(size uint64) uint64 {
	if size == 0 {
		return 0
	}
	if size > math.MaxUint64-31 {
		return math.MaxUint64
	}
	return ((size + 31) / 32) * 32
}

// PageAlignedSize rounds a byte size up to the next page boundary.
func PageAlignedSize(size uint64) uint64 {
	if size == 0 {
		return 0
	}
	if size > math.MaxUint64-MemPoolPageSize+1 {
		return math.MaxUint64
	}
	return ((size + MemPoolPageSize - 1) / MemPoolPageSize) * MemPoolPageSize
}
