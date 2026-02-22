// riscv_memory.go implements sparse page-based memory for the RISC-V CPU
// emulator. Memory is organized in 4KB pages allocated on demand, supporting
// a 32-bit address space. A memory-mapped I/O region at 0xF0000000+ allows
// guest programs to communicate with the host environment.
//
// Part of the K+ roadmap for canonical RISC-V guest execution.
package zkvm

import (
	"encoding/binary"
	"errors"
)

// Memory constants.
const (
	// RVPageSize is 4KB per page.
	RVPageSize = 4096

	// RVPageShift is log2(RVPageSize).
	RVPageShift = 12

	// RVMMIOBase is the start of the memory-mapped I/O region.
	RVMMIOBase uint32 = 0xF0000000

	// RVMaxPages limits total page allocations to bound memory usage.
	RVMaxPages = 16384 // 64 MiB total
)

// Memory errors.
var (
	ErrRVMemPageLimit  = errors.New("riscv: page allocation limit exceeded")
	ErrRVMemUnaligned  = errors.New("riscv: unaligned memory access")
	ErrRVMemMMIOWrite  = errors.New("riscv: write to read-only MMIO region")
	ErrRVMemSegOverlap = errors.New("riscv: segment load would overflow")
	ErrRVMemSegEmpty   = errors.New("riscv: empty segment data")
)

// RVMemory implements sparse page-based memory for RV32IM.
type RVMemory struct {
	pages     map[uint32][]byte // pageIndex -> 4KB page
	mmioRead  func(addr uint32) uint32
	mmioWrite func(addr uint32, val uint32)
	pageCount int
	maxPages  int
}

// NewRVMemory creates a new sparse memory instance.
func NewRVMemory() *RVMemory {
	return &RVMemory{
		pages:    make(map[uint32][]byte),
		maxPages: RVMaxPages,
	}
}

// SetMMIO registers MMIO read/write handlers for the 0xF0000000+ region.
func (m *RVMemory) SetMMIO(read func(uint32) uint32, write func(uint32, uint32)) {
	m.mmioRead = read
	m.mmioWrite = write
}

// isMMIO returns true if addr falls in the MMIO region.
func isMMIO(addr uint32) bool {
	return addr >= RVMMIOBase
}

// getPage returns the page for addr, allocating on demand.
func (m *RVMemory) getPage(addr uint32) ([]byte, error) {
	pageIdx := addr >> RVPageShift
	if page, ok := m.pages[pageIdx]; ok {
		return page, nil
	}
	if m.pageCount >= m.maxPages {
		return nil, ErrRVMemPageLimit
	}
	page := make([]byte, RVPageSize)
	m.pages[pageIdx] = page
	m.pageCount++
	return page, nil
}

// pageOffset returns the offset within a page for the given address.
func pageOffset(addr uint32) uint32 {
	return addr & (RVPageSize - 1)
}

// ReadByteAt reads a single byte from memory at the given address.
func (m *RVMemory) ReadByteAt(addr uint32) (byte, error) {
	if isMMIO(addr) && m.mmioRead != nil {
		return byte(m.mmioRead(addr)), nil
	}
	page, err := m.getPage(addr)
	if err != nil {
		return 0, err
	}
	return page[pageOffset(addr)], nil
}

// WriteByteAt writes a single byte to memory at the given address.
func (m *RVMemory) WriteByteAt(addr uint32, val byte) error {
	if isMMIO(addr) {
		if m.mmioWrite != nil {
			m.mmioWrite(addr, uint32(val))
			return nil
		}
		return nil
	}
	page, err := m.getPage(addr)
	if err != nil {
		return err
	}
	page[pageOffset(addr)] = val
	return nil
}

// ReadHalfword reads a 16-bit little-endian value from memory.
func (m *RVMemory) ReadHalfword(addr uint32) (uint16, error) {
	if isMMIO(addr) && m.mmioRead != nil {
		return uint16(m.mmioRead(addr)), nil
	}
	b0, err := m.ReadByteAt(addr)
	if err != nil {
		return 0, err
	}
	b1, err := m.ReadByteAt(addr + 1)
	if err != nil {
		return 0, err
	}
	return uint16(b0) | uint16(b1)<<8, nil
}

// WriteHalfword writes a 16-bit little-endian value to memory.
func (m *RVMemory) WriteHalfword(addr uint32, val uint16) error {
	if isMMIO(addr) {
		if m.mmioWrite != nil {
			m.mmioWrite(addr, uint32(val))
			return nil
		}
		return nil
	}
	if err := m.WriteByteAt(addr, byte(val)); err != nil {
		return err
	}
	return m.WriteByteAt(addr+1, byte(val>>8))
}

// ReadWord reads a 32-bit little-endian value from memory.
func (m *RVMemory) ReadWord(addr uint32) (uint32, error) {
	if isMMIO(addr) && m.mmioRead != nil {
		return m.mmioRead(addr), nil
	}
	// Optimization: if the word falls within a single page, read directly.
	off := pageOffset(addr)
	if off <= RVPageSize-4 {
		page, err := m.getPage(addr)
		if err != nil {
			return 0, err
		}
		return binary.LittleEndian.Uint32(page[off:]), nil
	}
	// Cross-page read: byte-by-byte.
	var buf [4]byte
	for i := uint32(0); i < 4; i++ {
		b, err := m.ReadByteAt(addr + i)
		if err != nil {
			return 0, err
		}
		buf[i] = b
	}
	return binary.LittleEndian.Uint32(buf[:]), nil
}

// WriteWord writes a 32-bit little-endian value to memory.
func (m *RVMemory) WriteWord(addr uint32, val uint32) error {
	if isMMIO(addr) {
		if m.mmioWrite != nil {
			m.mmioWrite(addr, val)
			return nil
		}
		return nil
	}
	// Optimization: if the word falls within a single page, write directly.
	off := pageOffset(addr)
	if off <= RVPageSize-4 {
		page, err := m.getPage(addr)
		if err != nil {
			return err
		}
		binary.LittleEndian.PutUint32(page[off:], val)
		return nil
	}
	// Cross-page write: byte-by-byte.
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], val)
	for i := uint32(0); i < 4; i++ {
		if err := m.WriteByteAt(addr+i, buf[i]); err != nil {
			return err
		}
	}
	return nil
}

// LoadSegment writes a contiguous byte slice into memory at the given base
// address, simulating loading of ELF program segments.
func (m *RVMemory) LoadSegment(base uint32, data []byte) error {
	if len(data) == 0 {
		return ErrRVMemSegEmpty
	}
	// Check for uint32 overflow.
	end := uint64(base) + uint64(len(data))
	if end > 0x100000000 {
		return ErrRVMemSegOverlap
	}
	for i, b := range data {
		if err := m.WriteByteAt(base+uint32(i), b); err != nil {
			return err
		}
	}
	return nil
}

// PageCount returns the number of allocated pages.
func (m *RVMemory) PageCount() int {
	return m.pageCount
}

// Reset clears all allocated pages and resets the memory to empty.
func (m *RVMemory) Reset() {
	m.pages = make(map[uint32][]byte)
	m.pageCount = 0
}
