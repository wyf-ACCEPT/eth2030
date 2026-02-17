package vm

import (
	"bytes"
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
