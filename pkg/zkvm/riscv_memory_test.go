package zkvm

import (
	"testing"
)

func TestRVMem_ReadWriteByte(t *testing.T) {
	mem := NewRVMemory()

	if err := mem.WriteByteAt(0x100, 0xAB); err != nil {
		t.Fatalf("WriteByte: %v", err)
	}
	val, err := mem.ReadByteAt(0x100)
	if err != nil {
		t.Fatalf("ReadByte: %v", err)
	}
	if val != 0xAB {
		t.Errorf("ReadByte: got 0x%02x, want 0xAB", val)
	}
}

func TestRVMem_ReadWriteHalfword(t *testing.T) {
	mem := NewRVMemory()

	if err := mem.WriteHalfword(0x200, 0xBEEF); err != nil {
		t.Fatalf("WriteHalfword: %v", err)
	}
	val, err := mem.ReadHalfword(0x200)
	if err != nil {
		t.Fatalf("ReadHalfword: %v", err)
	}
	if val != 0xBEEF {
		t.Errorf("ReadHalfword: got 0x%04x, want 0xBEEF", val)
	}
}

func TestRVMem_ReadWriteWord(t *testing.T) {
	mem := NewRVMemory()

	if err := mem.WriteWord(0x400, 0xDEADBEEF); err != nil {
		t.Fatalf("WriteWord: %v", err)
	}
	val, err := mem.ReadWord(0x400)
	if err != nil {
		t.Fatalf("ReadWord: %v", err)
	}
	if val != 0xDEADBEEF {
		t.Errorf("ReadWord: got 0x%08x, want 0xDEADBEEF", val)
	}
}

func TestRVMem_SparsePages(t *testing.T) {
	mem := NewRVMemory()

	// Write to widely separated addresses.
	addrs := []uint32{0x0000, 0x10000, 0x20000, 0x100000}
	for i, addr := range addrs {
		if err := mem.WriteByteAt(addr, byte(i+1)); err != nil {
			t.Fatalf("WriteByte at 0x%x: %v", addr, err)
		}
	}

	if mem.PageCount() != len(addrs) {
		t.Errorf("PageCount: got %d, want %d", mem.PageCount(), len(addrs))
	}

	for i, addr := range addrs {
		val, err := mem.ReadByteAt(addr)
		if err != nil {
			t.Fatalf("ReadByte at 0x%x: %v", addr, err)
		}
		if val != byte(i+1) {
			t.Errorf("ReadByte at 0x%x: got %d, want %d", addr, val, i+1)
		}
	}
}

func TestRVMem_UntouchedPageReadsZero(t *testing.T) {
	mem := NewRVMemory()

	// Untouched memory should read as zero.
	val, err := mem.ReadWord(0x5000)
	if err != nil {
		t.Fatalf("ReadWord: %v", err)
	}
	if val != 0 {
		t.Errorf("untouched memory: got 0x%x, want 0", val)
	}
}

func TestRVMem_CrossPageWord(t *testing.T) {
	mem := NewRVMemory()

	// Write a word that spans two pages.
	crossAddr := uint32(RVPageSize - 2)
	if err := mem.WriteWord(crossAddr, 0x12345678); err != nil {
		t.Fatalf("WriteWord cross-page: %v", err)
	}
	val, err := mem.ReadWord(crossAddr)
	if err != nil {
		t.Fatalf("ReadWord cross-page: %v", err)
	}
	if val != 0x12345678 {
		t.Errorf("cross-page word: got 0x%08x, want 0x12345678", val)
	}
	if mem.PageCount() != 2 {
		t.Errorf("cross-page should allocate 2 pages, got %d", mem.PageCount())
	}
}

func TestRVMem_LoadSegment(t *testing.T) {
	mem := NewRVMemory()

	data := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	if err := mem.LoadSegment(0x8000, data); err != nil {
		t.Fatalf("LoadSegment: %v", err)
	}

	for i, expected := range data {
		val, err := mem.ReadByteAt(0x8000 + uint32(i))
		if err != nil {
			t.Fatalf("ReadByte after segment load: %v", err)
		}
		if val != expected {
			t.Errorf("byte %d: got 0x%02x, want 0x%02x", i, val, expected)
		}
	}
}

func TestRVMem_LoadSegmentEmpty(t *testing.T) {
	mem := NewRVMemory()
	err := mem.LoadSegment(0, nil)
	if err != ErrRVMemSegEmpty {
		t.Errorf("expected ErrRVMemSegEmpty, got %v", err)
	}
}

func TestRVMem_LoadSegmentOverflow(t *testing.T) {
	mem := NewRVMemory()
	data := make([]byte, 512)
	// Base address 0xFFFFFF00 + 512 bytes = 0x100000100, overflows 32-bit.
	err := mem.LoadSegment(0xFFFFFF00, data)
	if err != ErrRVMemSegOverlap {
		t.Errorf("expected ErrRVMemSegOverlap, got %v", err)
	}
}

func TestRVMem_PageLimit(t *testing.T) {
	mem := NewRVMemory()
	mem.maxPages = 2

	// Each write to a different page should eventually hit the limit.
	if err := mem.WriteByteAt(0x0000, 1); err != nil {
		t.Fatalf("first page: %v", err)
	}
	if err := mem.WriteByteAt(0x1000, 2); err != nil {
		t.Fatalf("second page: %v", err)
	}
	err := mem.WriteByteAt(0x2000, 3)
	if err != ErrRVMemPageLimit {
		t.Errorf("expected ErrRVMemPageLimit, got %v", err)
	}
}

func TestRVMem_Reset(t *testing.T) {
	mem := NewRVMemory()
	if err := mem.WriteByteAt(0x100, 0xFF); err != nil {
		t.Fatalf("WriteByte: %v", err)
	}
	if mem.PageCount() != 1 {
		t.Fatalf("PageCount before reset: %d", mem.PageCount())
	}

	mem.Reset()

	if mem.PageCount() != 0 {
		t.Errorf("PageCount after reset: %d, want 0", mem.PageCount())
	}
	// Reading after reset should give zero (allocates a new page).
	val, err := mem.ReadByteAt(0x100)
	if err != nil {
		t.Fatalf("ReadByte after reset: %v", err)
	}
	if val != 0 {
		t.Errorf("ReadByte after reset: got 0x%02x, want 0", val)
	}
}

func TestRVMem_MMIO(t *testing.T) {
	mem := NewRVMemory()

	var lastWriteAddr uint32
	var lastWriteVal uint32

	mem.SetMMIO(
		func(addr uint32) uint32 {
			if addr == RVMMIOBase {
				return 0x42
			}
			return 0
		},
		func(addr uint32, val uint32) {
			lastWriteAddr = addr
			lastWriteVal = val
		},
	)

	// Read from MMIO.
	b, err := mem.ReadByteAt(RVMMIOBase)
	if err != nil {
		t.Fatalf("ReadByte MMIO: %v", err)
	}
	if b != 0x42 {
		t.Errorf("MMIO read: got 0x%02x, want 0x42", b)
	}

	// Write to MMIO.
	if err := mem.WriteWord(RVMMIOBase+4, 0xABCD); err != nil {
		t.Fatalf("WriteWord MMIO: %v", err)
	}
	if lastWriteAddr != RVMMIOBase+4 {
		t.Errorf("MMIO write addr: got 0x%08x, want 0x%08x", lastWriteAddr, RVMMIOBase+4)
	}
	if lastWriteVal != 0xABCD {
		t.Errorf("MMIO write val: got 0x%08x, want 0xABCD", lastWriteVal)
	}
}

func TestRVMem_LittleEndian(t *testing.T) {
	mem := NewRVMemory()

	if err := mem.WriteWord(0x100, 0x04030201); err != nil {
		t.Fatalf("WriteWord: %v", err)
	}

	// Verify byte order: LE means byte 0 is LSB.
	b0, _ := mem.ReadByteAt(0x100)
	b1, _ := mem.ReadByteAt(0x101)
	b2, _ := mem.ReadByteAt(0x102)
	b3, _ := mem.ReadByteAt(0x103)

	if b0 != 0x01 || b1 != 0x02 || b2 != 0x03 || b3 != 0x04 {
		t.Errorf("LE byte order: got [%02x %02x %02x %02x], want [01 02 03 04]",
			b0, b1, b2, b3)
	}

	// Verify halfword read.
	hw, _ := mem.ReadHalfword(0x100)
	if hw != 0x0201 {
		t.Errorf("LE halfword: got 0x%04x, want 0x0201", hw)
	}
}
