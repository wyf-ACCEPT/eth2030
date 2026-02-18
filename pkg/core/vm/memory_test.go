package vm

import (
	"bytes"
	"math"
	"math/big"
	"testing"
)

func TestMemoryResize(t *testing.T) {
	mem := NewMemory()
	if mem.Len() != 0 {
		t.Fatalf("initial Len() = %d, want 0", mem.Len())
	}

	mem.Resize(64)
	if mem.Len() != 64 {
		t.Fatalf("after Resize(64), Len() = %d, want 64", mem.Len())
	}

	// Resize to smaller should not shrink
	mem.Resize(32)
	if mem.Len() != 64 {
		t.Fatalf("after Resize(32), Len() = %d, want 64", mem.Len())
	}
}

func TestMemorySetGet(t *testing.T) {
	mem := NewMemory()
	mem.Resize(64)

	data := []byte{0xde, 0xad, 0xbe, 0xef}
	mem.Set(10, uint64(len(data)), data)

	got := mem.Get(10, int64(len(data)))
	if !bytes.Equal(got, data) {
		t.Errorf("Get() = %x, want %x", got, data)
	}
}

func TestMemorySet32(t *testing.T) {
	mem := NewMemory()
	mem.Resize(64)

	val := big.NewInt(0xff)
	mem.Set32(0, val)

	got := mem.Get(0, 32)
	// Should be 31 zero bytes followed by 0xff
	expected := make([]byte, 32)
	expected[31] = 0xff
	if !bytes.Equal(got, expected) {
		t.Errorf("Set32 result = %x, want %x", got, expected)
	}
}

func TestMemoryGetPtr(t *testing.T) {
	mem := NewMemory()
	mem.Resize(32)

	data := []byte{1, 2, 3, 4}
	mem.Set(0, 4, data)

	ptr := mem.GetPtr(0, 4)
	if !bytes.Equal(ptr, data) {
		t.Errorf("GetPtr() = %x, want %x", ptr, data)
	}

	// Modifying ptr should modify memory
	ptr[0] = 0xff
	if mem.Data()[0] != 0xff {
		t.Error("GetPtr should return a direct reference")
	}
}

func TestMemoryGetZeroSize(t *testing.T) {
	mem := NewMemory()
	mem.Resize(32)

	if got := mem.Get(0, 0); got != nil {
		t.Errorf("Get(0, 0) = %v, want nil", got)
	}
	if got := mem.GetPtr(0, 0); got != nil {
		t.Errorf("GetPtr(0, 0) = %v, want nil", got)
	}
}

func TestMemoryData(t *testing.T) {
	mem := NewMemory()
	mem.Resize(32)

	d := mem.Data()
	if len(d) != 32 {
		t.Errorf("Data() len = %d, want 32", len(d))
	}
}

// --- MemoryCost tests ---

func TestMemoryCostNoExpansion(t *testing.T) {
	// newSize <= currentSize should return 0.
	cost, ok := MemoryCost(64, 32)
	if !ok {
		t.Fatal("MemoryCost(64, 32) returned ok=false")
	}
	if cost != 0 {
		t.Errorf("MemoryCost(64, 32) = %d, want 0", cost)
	}

	// Same size.
	cost, ok = MemoryCost(64, 64)
	if !ok {
		t.Fatal("MemoryCost(64, 64) returned ok=false")
	}
	if cost != 0 {
		t.Errorf("MemoryCost(64, 64) = %d, want 0", cost)
	}
}

func TestMemoryCostFromZero(t *testing.T) {
	tests := []struct {
		newSize uint64
		want    uint64
	}{
		// 1 word: 1*3 + 1/512 = 3
		{32, 3},
		// 2 words: 2*3 + 4/512 = 6
		{64, 6},
		// 32 words: 32*3 + 1024/512 = 96 + 2 = 98
		{1024, 98},
		// 1024 words: 1024*3 + 1048576/512 = 3072 + 2048 = 5120
		{32768, 5120},
	}
	for _, tt := range tests {
		cost, ok := MemoryCost(0, tt.newSize)
		if !ok {
			t.Fatalf("MemoryCost(0, %d) returned ok=false", tt.newSize)
		}
		if cost != tt.want {
			t.Errorf("MemoryCost(0, %d) = %d, want %d", tt.newSize, cost, tt.want)
		}
	}
}

func TestMemoryCostDelta(t *testing.T) {
	// Expanding from 32 to 64 bytes (1 word to 2 words).
	// cost(2 words) - cost(1 word) = 6 - 3 = 3
	cost, ok := MemoryCost(32, 64)
	if !ok {
		t.Fatal("MemoryCost(32, 64) returned ok=false")
	}
	if cost != 3 {
		t.Errorf("MemoryCost(32, 64) = %d, want 3", cost)
	}

	// Expanding from 64 to 1024 bytes (2 words to 32 words).
	// cost(32 words) - cost(2 words) = 98 - 6 = 92
	cost, ok = MemoryCost(64, 1024)
	if !ok {
		t.Fatal("MemoryCost(64, 1024) returned ok=false")
	}
	if cost != 92 {
		t.Errorf("MemoryCost(64, 1024) = %d, want 92", cost)
	}
}

func TestMemoryCostQuadraticGrowth(t *testing.T) {
	// Verify quadratic growth: expanding to 2x memory costs more than 2x gas.
	smallCost, ok := MemoryCost(0, 1024)
	if !ok {
		t.Fatal("MemoryCost(0, 1024) returned ok=false")
	}
	largeCost, ok := MemoryCost(0, 32768)
	if !ok {
		t.Fatal("MemoryCost(0, 32768) returned ok=false")
	}
	// 32768 is 32x larger than 1024, but cost should be much more than 32x
	// because of the quadratic term.
	ratio := float64(largeCost) / float64(smallCost)
	if ratio <= 32.0 {
		t.Errorf("large/small cost ratio = %f, expected > 32 (quadratic growth)", ratio)
	}
}

func TestMemoryCostOverflow(t *testing.T) {
	// Near-max uint64 should fail.
	_, ok := MemoryCost(0, math.MaxUint64)
	if ok {
		t.Error("MemoryCost(0, MaxUint64) should return ok=false")
	}

	// Just above MaxMemorySize should fail.
	_, ok = MemoryCost(0, MaxMemorySize+1)
	if ok {
		t.Error("MemoryCost(0, MaxMemorySize+1) should return ok=false")
	}
}

func TestMemoryCostAtMaxMemorySize(t *testing.T) {
	// MaxMemorySize should succeed.
	cost, ok := MemoryCost(0, MaxMemorySize)
	if !ok {
		t.Fatal("MemoryCost(0, MaxMemorySize) should succeed")
	}
	if cost == 0 {
		t.Error("MemoryCost(0, MaxMemorySize) should be > 0")
	}
}

func TestMemoryCostNonWordAligned(t *testing.T) {
	// 33 bytes = 2 words (rounded up). Same cost as 64 bytes.
	cost33, ok := MemoryCost(0, 33)
	if !ok {
		t.Fatal("MemoryCost(0, 33) returned ok=false")
	}
	cost64, ok := MemoryCost(0, 64)
	if !ok {
		t.Fatal("MemoryCost(0, 64) returned ok=false")
	}
	if cost33 != cost64 {
		t.Errorf("MemoryCost(0, 33) = %d, MemoryCost(0, 64) = %d, want equal (both 2 words)", cost33, cost64)
	}
}
