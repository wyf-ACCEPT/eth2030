package light

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestMemoryLightStore_StoreAndGet(t *testing.T) {
	store := NewMemoryLightStore()

	h := &types.Header{Number: big.NewInt(100)}
	if err := store.StoreHeader(h); err != nil {
		t.Fatalf("StoreHeader: %v", err)
	}

	hash := h.Hash()
	got := store.GetHeader(hash)
	if got == nil {
		t.Fatal("GetHeader returned nil")
	}
	if got.Number.Int64() != 100 {
		t.Errorf("number = %d, want 100", got.Number.Int64())
	}
}

func TestMemoryLightStore_GetByNumber(t *testing.T) {
	store := NewMemoryLightStore()

	h := &types.Header{Number: big.NewInt(50)}
	store.StoreHeader(h)

	got := store.GetByNumber(50)
	if got == nil {
		t.Fatal("GetByNumber returned nil")
	}
	if got.Number.Int64() != 50 {
		t.Errorf("number = %d, want 50", got.Number.Int64())
	}

	// Non-existent.
	if store.GetByNumber(999) != nil {
		t.Error("expected nil for non-existent block number")
	}
}

func TestMemoryLightStore_GetLatest(t *testing.T) {
	store := NewMemoryLightStore()

	// Initially empty.
	if store.GetLatest() != nil {
		t.Error("expected nil for empty store")
	}

	// Store headers out of order.
	h1 := &types.Header{Number: big.NewInt(10)}
	h2 := &types.Header{Number: big.NewInt(20)}
	h3 := &types.Header{Number: big.NewInt(5)}
	store.StoreHeader(h1)
	store.StoreHeader(h2)
	store.StoreHeader(h3)

	latest := store.GetLatest()
	if latest == nil {
		t.Fatal("GetLatest returned nil")
	}
	if latest.Number.Int64() != 20 {
		t.Errorf("latest = %d, want 20", latest.Number.Int64())
	}
}

func TestMemoryLightStore_Count(t *testing.T) {
	store := NewMemoryLightStore()
	if store.Count() != 0 {
		t.Error("count should be 0 for empty store")
	}

	store.StoreHeader(&types.Header{Number: big.NewInt(1)})
	store.StoreHeader(&types.Header{Number: big.NewInt(2)})
	if store.Count() != 2 {
		t.Errorf("count = %d, want 2", store.Count())
	}
}

func TestMemoryLightStore_NilHeader(t *testing.T) {
	store := NewMemoryLightStore()

	// Storing nil should not panic.
	if err := store.StoreHeader(nil); err != nil {
		t.Fatalf("StoreHeader(nil): %v", err)
	}
	if store.Count() != 0 {
		t.Error("nil header should not be stored")
	}
}

func TestMemoryLightStore_NilNumber(t *testing.T) {
	store := NewMemoryLightStore()

	h := &types.Header{Number: nil}
	if err := store.StoreHeader(h); err != nil {
		t.Fatalf("StoreHeader(nil number): %v", err)
	}
	if store.Count() != 0 {
		t.Error("header with nil number should not be stored")
	}
}

func TestMemoryLightStore_GetNonExistentHash(t *testing.T) {
	store := NewMemoryLightStore()
	if store.GetHeader(types.Hash{}) != nil {
		t.Error("expected nil for non-existent hash")
	}
}
