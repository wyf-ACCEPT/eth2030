package vm

import (
	"testing"
)

func TestNewPooledMemory(t *testing.T) {
	pm := NewPooledMemory()
	if pm.Size() != 0 {
		t.Fatalf("initial size: got %d, want 0", pm.Size())
	}
	if pm.TotalGasCost() != 0 {
		t.Fatalf("initial gas cost: got %d, want 0", pm.TotalGasCost())
	}
}

func TestPooledMemoryExpand(t *testing.T) {
	pm := NewPooledMemory()

	// Expand to 32 bytes (1 word).
	gas, ok := pm.Expand(32)
	if !ok {
		t.Fatal("expansion failed")
	}
	if gas == 0 {
		t.Fatal("expected non-zero gas cost")
	}
	if pm.Size() != 32 {
		t.Fatalf("size after expansion: got %d, want 32", pm.Size())
	}

	// Expand to same size: no-op.
	gas2, ok := pm.Expand(32)
	if !ok {
		t.Fatal("no-op expansion failed")
	}
	if gas2 != 0 {
		t.Fatalf("expected 0 gas for no expansion, got %d", gas2)
	}
}

func TestPooledMemoryExpandMultiplePages(t *testing.T) {
	pm := NewPooledMemory()

	// Expand to 8192 bytes (2 pages).
	_, ok := pm.Expand(8192)
	if !ok {
		t.Fatal("expansion to 2 pages failed")
	}
	if pm.Size() != 8192 {
		t.Fatalf("size: got %d, want 8192", pm.Size())
	}
}

func TestPooledMemorySetGet(t *testing.T) {
	pm := NewPooledMemory()
	pm.Expand(128)

	data := []byte{0xde, 0xad, 0xbe, 0xef}
	pm.Set(0, 4, data)

	got := pm.Get(0, 4)
	if len(got) != 4 {
		t.Fatalf("got length %d, want 4", len(got))
	}
	for i := 0; i < 4; i++ {
		if got[i] != data[i] {
			t.Fatalf("byte %d: got %x, want %x", i, got[i], data[i])
		}
	}
}

func TestPooledMemorySetGetCrossPage(t *testing.T) {
	pm := NewPooledMemory()
	// Expand enough for 2 pages.
	pm.Expand(MemPoolPageSize * 2)

	// Write data across page boundary.
	offset := uint64(MemPoolPageSize - 2) // starts 2 bytes before page end
	data := []byte{0x01, 0x02, 0x03, 0x04}
	pm.Set(offset, 4, data)

	got := pm.Get(offset, 4)
	for i := 0; i < 4; i++ {
		if got[i] != data[i] {
			t.Fatalf("cross-page byte %d: got %x, want %x", i, got[i], data[i])
		}
	}
}

func TestPooledMemorySet32(t *testing.T) {
	pm := NewPooledMemory()
	pm.Expand(64)

	val := []byte{0x01, 0x02, 0x03}
	pm.Set32(0, val)

	got := pm.Get(0, 32)
	// Should be zero-padded: 29 zeros + 0x01 0x02 0x03
	for i := 0; i < 29; i++ {
		if got[i] != 0 {
			t.Fatalf("byte %d should be 0, got %x", i, got[i])
		}
	}
	if got[29] != 0x01 || got[30] != 0x02 || got[31] != 0x03 {
		t.Fatalf("value bytes mismatch: got %x", got[29:32])
	}
}

func TestPooledMemoryCopyWithin(t *testing.T) {
	pm := NewPooledMemory()
	pm.Expand(128)

	data := []byte{0x11, 0x22, 0x33, 0x44}
	pm.Set(0, 4, data)

	// Copy from offset 0 to offset 32.
	pm.CopyWithin(32, 0, 4)

	got := pm.Get(32, 4)
	for i := 0; i < 4; i++ {
		if got[i] != data[i] {
			t.Fatalf("byte %d: got %x, want %x", i, got[i], data[i])
		}
	}
}

func TestPooledMemoryCopyWithinOverlap(t *testing.T) {
	pm := NewPooledMemory()
	pm.Expand(128)

	// Write sequential bytes.
	data := make([]byte, 8)
	for i := 0; i < 8; i++ {
		data[i] = byte(i + 1)
	}
	pm.Set(0, 8, data)

	// Overlapping copy: src=0, dst=2, size=6. After copy,
	// bytes at offset 2-7 should be 1,2,3,4,5,6.
	pm.CopyWithin(2, 0, 6)

	got := pm.Get(2, 6)
	for i := 0; i < 6; i++ {
		if got[i] != byte(i+1) {
			t.Fatalf("overlap byte %d: got %x, want %x", i, got[i], byte(i+1))
		}
	}
}

func TestPooledMemoryData(t *testing.T) {
	pm := NewPooledMemory()
	pm.Expand(64)

	pm.Set(0, 4, []byte{0xAA, 0xBB, 0xCC, 0xDD})
	all := pm.Data()
	if uint64(len(all)) != pm.Size() {
		t.Fatalf("Data length: got %d, want %d", len(all), pm.Size())
	}
	if all[0] != 0xAA || all[3] != 0xDD {
		t.Fatal("Data content mismatch")
	}
}

func TestPooledMemoryFree(t *testing.T) {
	pm := NewPooledMemory()
	pm.Expand(MemPoolPageSize * 3)
	pm.Set(0, 4, []byte{1, 2, 3, 4})

	pm.Free()

	if pm.Size() != 0 {
		t.Fatalf("size after free: got %d, want 0", pm.Size())
	}
	if pm.TotalGasCost() != 0 {
		t.Fatalf("gas cost after free: got %d, want 0", pm.TotalGasCost())
	}
}

func TestPooledMemoryExpansionCost(t *testing.T) {
	pm := NewPooledMemory()

	// No expansion needed.
	gas, ok := pm.ExpansionCost(0)
	if !ok || gas != 0 {
		t.Fatalf("expected (0, true), got (%d, %v)", gas, ok)
	}

	// First expansion.
	gas, ok = pm.ExpansionCost(32)
	if !ok {
		t.Fatal("expected ok")
	}
	if gas == 0 {
		t.Fatal("expected non-zero gas cost")
	}
}

func TestPooledMemoryExpansionCostExceedsLimit(t *testing.T) {
	pm := NewPooledMemory()
	_, ok := pm.ExpansionCost(MemPoolMaxMemory + 1)
	if ok {
		t.Fatal("expected failure for size exceeding limit")
	}
}

func TestPooledMemoryZeroInit(t *testing.T) {
	pm := NewPooledMemory()
	pm.Expand(64)

	// All bytes should be zero initially.
	data := pm.Get(0, 64)
	for i, b := range data {
		if b != 0 {
			t.Fatalf("byte %d not zero: %x", i, b)
		}
	}
}

func TestPooledMemoryGetZeroSize(t *testing.T) {
	pm := NewPooledMemory()
	got := pm.Get(0, 0)
	if got != nil {
		t.Fatal("expected nil for zero-size Get")
	}
}

func TestPooledMemoryGetOutOfBounds(t *testing.T) {
	pm := NewPooledMemory()
	pm.Expand(32)

	got := pm.Get(0, 64) // exceeds size
	if got != nil {
		t.Fatal("expected nil for out-of-bounds Get")
	}
}

func TestPooledMemorySetZeroSize(t *testing.T) {
	pm := NewPooledMemory()
	pm.Expand(32)
	// Should be a no-op.
	pm.Set(0, 0, nil)
}

func TestMcopyGas(t *testing.T) {
	// Zero size: just base gas.
	gas, ok := McopyGas(0, 0, 0, 0)
	if !ok {
		t.Fatal("expected ok for zero size")
	}
	if gas != GasMcopyBase {
		t.Fatalf("expected GasMcopyBase(%d), got %d", GasMcopyBase, gas)
	}

	// Non-zero copy within existing memory.
	gas, ok = McopyGas(128, 0, 32, 32)
	if !ok {
		t.Fatal("expected ok")
	}
	if gas == 0 {
		t.Fatal("expected non-zero gas")
	}

	// Copy requiring memory expansion.
	gas, ok = McopyGas(0, 0, 0, 64)
	if !ok {
		t.Fatal("expected ok")
	}
	if gas == 0 {
		t.Fatal("expected non-zero gas with expansion")
	}
}

func TestMcopyGasOverflow(t *testing.T) {
	// Overflow in dst + size.
	_, ok := McopyGas(0, ^uint64(0), 0, 1)
	if ok {
		t.Fatal("expected overflow failure")
	}
}

func TestWordAlignedSize(t *testing.T) {
	tests := []struct {
		in, want uint64
	}{
		{0, 0},
		{1, 32},
		{31, 32},
		{32, 32},
		{33, 64},
		{64, 64},
	}
	for _, tt := range tests {
		got := WordAlignedSize(tt.in)
		if got != tt.want {
			t.Errorf("WordAlignedSize(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestPageAlignedSize(t *testing.T) {
	tests := []struct {
		in, want uint64
	}{
		{0, 0},
		{1, MemPoolPageSize},
		{MemPoolPageSize - 1, MemPoolPageSize},
		{MemPoolPageSize, MemPoolPageSize},
		{MemPoolPageSize + 1, 2 * MemPoolPageSize},
	}
	for _, tt := range tests {
		got := PageAlignedSize(tt.in)
		if got != tt.want {
			t.Errorf("PageAlignedSize(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestGetPageReturnsZeroed(t *testing.T) {
	p := getPage()
	defer putPage(p)

	for i, b := range *p {
		if b != 0 {
			t.Fatalf("page byte %d not zero: %x", i, b)
		}
	}
}

func TestPooledMemoryExpandWordAligned(t *testing.T) {
	pm := NewPooledMemory()

	// Expand to 33 bytes: should be word-aligned to 64.
	_, ok := pm.Expand(33)
	if !ok {
		t.Fatal("expansion failed")
	}
	if pm.Size() != 64 {
		t.Fatalf("size should be word-aligned: got %d, want 64", pm.Size())
	}
}
