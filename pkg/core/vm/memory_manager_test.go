package vm

import (
	"bytes"
	"errors"
	"math"
	"testing"
)

func TestMemoryManager_NewEmpty(t *testing.T) {
	mm := NewMemoryManager()
	if mm.Size() != 0 {
		t.Fatalf("new MemoryManager Size() = %d, want 0", mm.Size())
	}
	if mm.TotalGasUsed() != 0 {
		t.Fatalf("new MemoryManager TotalGasUsed() = %d, want 0", mm.TotalGasUsed())
	}
}

func TestMemoryManager_AllocateBasic(t *testing.T) {
	mm := NewMemoryManager()

	// Allocate 1 byte at offset 0 -- should expand to 32 bytes (1 word).
	// Gas cost: (1*1)/512 + 3*1 = 0 + 3 = 3
	gas, err := mm.Allocate(0, 1)
	if err != nil {
		t.Fatalf("Allocate(0, 1) error: %v", err)
	}
	if gas != 3 {
		t.Fatalf("Allocate(0, 1) gas = %d, want 3", gas)
	}
	if mm.Size() != 32 {
		t.Fatalf("after Allocate(0,1) Size() = %d, want 32", mm.Size())
	}
}

func TestMemoryManager_AllocateMultipleWords(t *testing.T) {
	mm := NewMemoryManager()

	// Allocate 64 bytes at offset 0 -- 2 words.
	// Gas cost: (2*2)/512 + 3*2 = 0 + 6 = 6
	gas, err := mm.Allocate(0, 64)
	if err != nil {
		t.Fatalf("Allocate(0, 64) error: %v", err)
	}
	if gas != 6 {
		t.Fatalf("Allocate(0, 64) gas = %d, want 6", gas)
	}
	if mm.Size() != 64 {
		t.Fatalf("after Allocate(0,64) Size() = %d, want 64", mm.Size())
	}
}

func TestMemoryManager_AllocateNoExpansion(t *testing.T) {
	mm := NewMemoryManager()

	// First allocation.
	_, err := mm.Allocate(0, 64)
	if err != nil {
		t.Fatalf("first Allocate error: %v", err)
	}

	// Second allocation within existing bounds.
	gas, err := mm.Allocate(0, 32)
	if err != nil {
		t.Fatalf("second Allocate error: %v", err)
	}
	if gas != 0 {
		t.Fatalf("Allocate within bounds gas = %d, want 0", gas)
	}
}

func TestMemoryManager_AllocateIncremental(t *testing.T) {
	mm := NewMemoryManager()

	// Allocate 32 bytes (1 word). Cost: 3
	gas1, err := mm.Allocate(0, 32)
	if err != nil {
		t.Fatalf("first Allocate error: %v", err)
	}
	if gas1 != 3 {
		t.Fatalf("first gas = %d, want 3", gas1)
	}

	// Expand to 64 bytes (2 words). Incremental cost: 6 - 3 = 3
	gas2, err := mm.Allocate(0, 64)
	if err != nil {
		t.Fatalf("second Allocate error: %v", err)
	}
	if gas2 != 3 {
		t.Fatalf("incremental gas = %d, want 3", gas2)
	}
	if mm.Size() != 64 {
		t.Fatalf("Size() = %d, want 64", mm.Size())
	}

	// Total gas should be cumulative.
	if mm.TotalGasUsed() != 6 {
		t.Fatalf("TotalGasUsed() = %d, want 6", mm.TotalGasUsed())
	}
}

func TestMemoryManager_AllocateRoundsUp(t *testing.T) {
	mm := NewMemoryManager()

	// Allocate 33 bytes at offset 0 -- should round up to 64 bytes (2 words).
	gas, err := mm.Allocate(0, 33)
	if err != nil {
		t.Fatalf("Allocate(0, 33) error: %v", err)
	}
	// 2 words cost: (4)/512 + 6 = 6
	if gas != 6 {
		t.Fatalf("Allocate(0, 33) gas = %d, want 6", gas)
	}
	if mm.Size() != 64 {
		t.Fatalf("Size() = %d, want 64", mm.Size())
	}
}

func TestMemoryManager_AllocateWithOffset(t *testing.T) {
	mm := NewMemoryManager()

	// Allocate 32 bytes starting at offset 32 -- needs 64 bytes total.
	gas, err := mm.Allocate(32, 32)
	if err != nil {
		t.Fatalf("Allocate(32, 32) error: %v", err)
	}
	if gas != 6 {
		t.Fatalf("Allocate(32, 32) gas = %d, want 6", gas)
	}
	if mm.Size() != 64 {
		t.Fatalf("Size() = %d, want 64", mm.Size())
	}
}

func TestMemoryManager_AllocateZeroSize(t *testing.T) {
	mm := NewMemoryManager()

	gas, err := mm.Allocate(100, 0)
	if err != nil {
		t.Fatalf("Allocate(100, 0) error: %v", err)
	}
	if gas != 0 {
		t.Fatalf("Allocate(100, 0) gas = %d, want 0", gas)
	}
	if mm.Size() != 0 {
		t.Fatalf("Size() = %d, want 0", mm.Size())
	}
}

func TestMemoryManager_StoreAndLoad(t *testing.T) {
	mm := NewMemoryManager()
	_, err := mm.Allocate(0, 64)
	if err != nil {
		t.Fatalf("Allocate error: %v", err)
	}

	data := []byte{0xde, 0xad, 0xbe, 0xef}
	if err := mm.Store(10, data); err != nil {
		t.Fatalf("Store error: %v", err)
	}

	got, err := mm.Load(10, 4)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("Load() = %x, want %x", got, data)
	}
}

func TestMemoryManager_LoadCopySemantics(t *testing.T) {
	mm := NewMemoryManager()
	_, err := mm.Allocate(0, 32)
	if err != nil {
		t.Fatalf("Allocate error: %v", err)
	}

	data := []byte{0x01, 0x02, 0x03, 0x04}
	if err := mm.Store(0, data); err != nil {
		t.Fatalf("Store error: %v", err)
	}

	// Load returns a copy, so modifying it should not affect internal memory.
	loaded, err := mm.Load(0, 4)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	loaded[0] = 0xff

	// Verify internal memory is unchanged.
	original, err := mm.Load(0, 4)
	if err != nil {
		t.Fatalf("second Load error: %v", err)
	}
	if original[0] != 0x01 {
		t.Fatalf("Load returned mutable reference; internal memory changed to %x", original)
	}
}

func TestMemoryManager_StoreOutOfBounds(t *testing.T) {
	mm := NewMemoryManager()
	_, err := mm.Allocate(0, 32)
	if err != nil {
		t.Fatalf("Allocate error: %v", err)
	}

	// Try to store beyond allocated memory.
	err = mm.Store(30, []byte{0x01, 0x02, 0x03, 0x04})
	if err == nil {
		t.Fatal("expected error for out-of-bounds store")
	}
	if !errors.Is(err, ErrMemoryOutOfBounds) {
		t.Fatalf("expected ErrMemoryOutOfBounds, got: %v", err)
	}
}

func TestMemoryManager_LoadOutOfBounds(t *testing.T) {
	mm := NewMemoryManager()
	_, err := mm.Allocate(0, 32)
	if err != nil {
		t.Fatalf("Allocate error: %v", err)
	}

	_, err = mm.Load(28, 8)
	if err == nil {
		t.Fatal("expected error for out-of-bounds load")
	}
	if !errors.Is(err, ErrMemoryOutOfBounds) {
		t.Fatalf("expected ErrMemoryOutOfBounds, got: %v", err)
	}
}

func TestMemoryManager_LoadZeroSize(t *testing.T) {
	mm := NewMemoryManager()

	got, err := mm.Load(0, 0)
	if err != nil {
		t.Fatalf("Load(0, 0) error: %v", err)
	}
	if got != nil {
		t.Fatalf("Load(0, 0) = %v, want nil", got)
	}
}

func TestMemoryManager_StoreEmptyData(t *testing.T) {
	mm := NewMemoryManager()

	// Storing empty data at any offset should succeed without allocation.
	err := mm.Store(0, nil)
	if err != nil {
		t.Fatalf("Store(0, nil) error: %v", err)
	}
	err = mm.Store(0, []byte{})
	if err != nil {
		t.Fatalf("Store(0, []) error: %v", err)
	}
}

func TestMemoryManager_AllocateOverflow(t *testing.T) {
	mm := NewMemoryManager()

	// offset + size overflows uint64.
	_, err := mm.Allocate(math.MaxUint64, 1)
	if err == nil {
		t.Fatal("expected overflow error")
	}
	if !errors.Is(err, ErrMemoryOverflow) {
		t.Fatalf("expected ErrMemoryOverflow, got: %v", err)
	}
}

func TestMemoryManager_AllocateExceedsLimit(t *testing.T) {
	mm := NewMemoryManager()

	_, err := mm.Allocate(0, MemoryManagerMaxSize+1)
	if err == nil {
		t.Fatal("expected exceeds limit error")
	}
	if !errors.Is(err, ErrMemoryExceedsLimit) {
		t.Fatalf("expected ErrMemoryExceedsLimit, got: %v", err)
	}
}

func TestMemoryManager_MemoryExpansionCost(t *testing.T) {
	mm := NewMemoryManager()

	// Before any allocation, cost to expand to 32 bytes (1 word):
	// (1*1)/512 + 3*1 = 3
	cost := mm.MemoryExpansionCost(32)
	if cost != 3 {
		t.Fatalf("MemoryExpansionCost(32) = %d, want 3", cost)
	}

	// Cost to expand to 64 bytes (2 words): (4)/512 + 6 = 6
	cost = mm.MemoryExpansionCost(64)
	if cost != 6 {
		t.Fatalf("MemoryExpansionCost(64) = %d, want 6", cost)
	}

	// After allocating 32 bytes, cost to expand to 32 should be 0.
	mm.Allocate(0, 32)
	cost = mm.MemoryExpansionCost(32)
	if cost != 0 {
		t.Fatalf("MemoryExpansionCost(32) after 32-byte alloc = %d, want 0", cost)
	}

	// Cost to expand to smaller is also 0.
	cost = mm.MemoryExpansionCost(16)
	if cost != 0 {
		t.Fatalf("MemoryExpansionCost(16) = %d, want 0", cost)
	}
}

func TestMemoryManager_QuadraticGrowth(t *testing.T) {
	mm := NewMemoryManager()

	// Expanding to 1024 bytes: 32 words.
	// cost = (32*32)/512 + 3*32 = 2 + 96 = 98
	gas1, err := mm.Allocate(0, 1024)
	if err != nil {
		t.Fatalf("Allocate error: %v", err)
	}
	if gas1 != 98 {
		t.Fatalf("1024 bytes gas = %d, want 98", gas1)
	}

	// Expanding to 32768 bytes: 1024 words.
	// new cost = (1024*1024)/512 + 3*1024 = 2048 + 3072 = 5120
	// incremental = 5120 - 98 = 5022
	gas2, err := mm.Allocate(0, 32768)
	if err != nil {
		t.Fatalf("Allocate error: %v", err)
	}
	if gas2 != 5022 {
		t.Fatalf("32768 bytes incremental gas = %d, want 5022", gas2)
	}
}

func TestMemoryManager_SizeAlwaysMultipleOf32(t *testing.T) {
	mm := NewMemoryManager()

	sizes := []uint64{1, 15, 31, 32, 33, 63, 64, 65, 100}
	for _, s := range sizes {
		mm2 := NewMemoryManager()
		_, err := mm2.Allocate(0, s)
		if err != nil {
			t.Fatalf("Allocate(0, %d) error: %v", s, err)
		}
		if mm2.Size()%32 != 0 {
			t.Fatalf("after Allocate(0, %d), Size() = %d, not a multiple of 32", s, mm2.Size())
		}
	}

	// Verify the original is still empty.
	if mm.Size() != 0 {
		t.Fatalf("original mm.Size() = %d, want 0", mm.Size())
	}
}

func TestMemoryManager_LargeStoreLoad(t *testing.T) {
	mm := NewMemoryManager()

	// Allocate 1 KiB.
	_, err := mm.Allocate(0, 1024)
	if err != nil {
		t.Fatalf("Allocate error: %v", err)
	}

	// Write a pattern across the full region.
	data := make([]byte, 1024)
	for i := range data {
		data[i] = byte(i % 256)
	}
	if err := mm.Store(0, data); err != nil {
		t.Fatalf("Store error: %v", err)
	}

	// Read it back.
	got, err := mm.Load(0, 1024)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("Load() data mismatch for large region")
	}

	// Read a sub-slice.
	slice, err := mm.Load(100, 50)
	if err != nil {
		t.Fatalf("Load sub-slice error: %v", err)
	}
	if !bytes.Equal(slice, data[100:150]) {
		t.Fatal("Load() sub-slice mismatch")
	}
}
