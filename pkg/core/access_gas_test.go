package core

import (
	"errors"
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func testAddr(b byte) types.Address {
	var a types.Address
	a[19] = b
	return a
}

func testSlot(b byte) types.Hash {
	var h types.Hash
	h[31] = b
	return h
}

func TestAccessGasCounter_BasicRead(t *testing.T) {
	cfg := DefaultAccessGasConfig()
	c := NewAccessGasCounter(cfg)

	addr := testAddr(1)
	slot := testSlot(1)

	// First read should be cold.
	cost, err := c.TrackRead(addr, slot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cost != cfg.ReadGasCost {
		t.Fatalf("expected cold read cost %d, got %d", cfg.ReadGasCost, cost)
	}

	// Second read should be warm.
	cost, err = c.TrackRead(addr, slot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cost != cfg.WarmReadCost {
		t.Fatalf("expected warm read cost %d, got %d", cfg.WarmReadCost, cost)
	}

	if c.ReadGasUsed() != cfg.ReadGasCost+cfg.WarmReadCost {
		t.Fatalf("unexpected total read gas: %d", c.ReadGasUsed())
	}
	if c.WriteGasUsed() != 0 {
		t.Fatalf("write gas should be 0, got %d", c.WriteGasUsed())
	}
}

func TestAccessGasCounter_BasicWrite(t *testing.T) {
	cfg := DefaultAccessGasConfig()
	c := NewAccessGasCounter(cfg)

	addr := testAddr(2)
	slot := testSlot(2)

	// Cold write.
	cost, err := c.TrackWrite(addr, slot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cost != cfg.WriteGasCost {
		t.Fatalf("expected cold write cost %d, got %d", cfg.WriteGasCost, cost)
	}

	// Warm write (same slot).
	cost, err = c.TrackWrite(addr, slot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cost != cfg.WarmWriteCost {
		t.Fatalf("expected warm write cost %d, got %d", cfg.WarmWriteCost, cost)
	}

	if c.WriteGasUsed() != cfg.WriteGasCost+cfg.WarmWriteCost {
		t.Fatalf("unexpected write gas: %d", c.WriteGasUsed())
	}
}

func TestAccessGasCounter_ReadThenWrite(t *testing.T) {
	cfg := DefaultAccessGasConfig()
	c := NewAccessGasCounter(cfg)

	addr := testAddr(3)
	slot := testSlot(3)

	// Cold read warms the slot.
	_, err := c.TrackRead(addr, slot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Write to same slot should now be warm.
	cost, err := c.TrackWrite(addr, slot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cost != cfg.WarmWriteCost {
		t.Fatalf("expected warm write cost after read, got %d", cost)
	}
}

func TestAccessGasCounter_ExceedsLimit(t *testing.T) {
	cfg := AccessGasConfig{
		AccessGasLimit:  5000,
		ComputeGasLimit: 30_000_000,
		ReadGasCost:     2100,
		WriteGasCost:    5000,
		WarmReadCost:    100,
		WarmWriteCost:   100,
	}
	c := NewAccessGasCounter(cfg)

	addr := testAddr(4)
	slot1 := testSlot(1)
	slot2 := testSlot(2)
	slot3 := testSlot(3)

	// First cold read: 2100 gas.
	_, err := c.TrackRead(addr, slot1)
	if err != nil {
		t.Fatalf("first read should succeed: %v", err)
	}

	// Second cold read (different slot): 2100 gas (total = 4200).
	_, err = c.TrackRead(addr, slot2)
	if err != nil {
		t.Fatalf("second read should succeed: %v", err)
	}

	// Third cold read: would need 2100 more (total = 6300 > 5000).
	_, err = c.TrackRead(addr, slot3)
	if err == nil {
		t.Fatal("expected error when exceeding access gas limit")
	}
	if !errors.Is(err, ErrAccessGasExceeded) {
		t.Fatalf("expected ErrAccessGasExceeded, got: %v", err)
	}
}

func TestAccessGasCounter_IsAccessGasExceeded(t *testing.T) {
	cfg := AccessGasConfig{
		AccessGasLimit:  2100,
		ComputeGasLimit: 30_000_000,
		ReadGasCost:     2100,
		WriteGasCost:    5000,
		WarmReadCost:    100,
		WarmWriteCost:   100,
	}
	c := NewAccessGasCounter(cfg)

	if c.IsAccessGasExceeded() {
		t.Fatal("should not be exceeded initially")
	}

	_, _ = c.TrackRead(testAddr(1), testSlot(1))

	// Exactly at limit, not exceeded.
	if c.IsAccessGasExceeded() {
		t.Fatal("should not be exceeded at exactly the limit")
	}
}

func TestAccessGasCounter_Remaining(t *testing.T) {
	cfg := AccessGasConfig{
		AccessGasLimit:  10000,
		ComputeGasLimit: 30_000_000,
		ReadGasCost:     2100,
		WriteGasCost:    5000,
		WarmReadCost:    100,
		WarmWriteCost:   100,
	}
	c := NewAccessGasCounter(cfg)

	if c.Remaining() != 10000 {
		t.Fatalf("expected 10000 remaining, got %d", c.Remaining())
	}

	_, _ = c.TrackRead(testAddr(1), testSlot(1))
	if c.Remaining() != 7900 {
		t.Fatalf("expected 7900 remaining, got %d", c.Remaining())
	}
}

func TestAccessGasCounter_MergeCounters(t *testing.T) {
	cfg := DefaultAccessGasConfig()
	c1 := NewAccessGasCounter(cfg)
	c2 := NewAccessGasCounter(cfg)

	addr1 := testAddr(1)
	slot1 := testSlot(1)
	addr2 := testAddr(2)
	slot2 := testSlot(2)

	_, _ = c1.TrackRead(addr1, slot1)
	_, _ = c2.TrackWrite(addr2, slot2)

	err := c1.MergeCounters(c2)
	if err != nil {
		t.Fatalf("merge error: %v", err)
	}

	if c1.ReadGasUsed() != cfg.ReadGasCost {
		t.Fatalf("expected read gas %d, got %d", cfg.ReadGasCost, c1.ReadGasUsed())
	}
	if c1.WriteGasUsed() != cfg.WriteGasCost {
		t.Fatalf("expected write gas %d, got %d", cfg.WriteGasCost, c1.WriteGasUsed())
	}

	// Warm slots from c2 should be merged.
	if !c1.IsWarm(addr2, slot2) {
		t.Fatal("slot from c2 should be warm after merge")
	}

	// Records should be merged.
	if len(c1.Records()) != 2 {
		t.Fatalf("expected 2 records after merge, got %d", len(c1.Records()))
	}
}

func TestAccessGasCounter_MergeNil(t *testing.T) {
	cfg := DefaultAccessGasConfig()
	c := NewAccessGasCounter(cfg)

	err := c.MergeCounters(nil)
	if !errors.Is(err, ErrNilCounter) {
		t.Fatalf("expected ErrNilCounter, got: %v", err)
	}
}

func TestAccessGasCounter_Reset(t *testing.T) {
	cfg := DefaultAccessGasConfig()
	c := NewAccessGasCounter(cfg)

	_, _ = c.TrackRead(testAddr(1), testSlot(1))
	_, _ = c.TrackWrite(testAddr(2), testSlot(2))

	c.Reset()

	if c.ReadGasUsed() != 0 {
		t.Fatalf("read gas should be 0 after reset, got %d", c.ReadGasUsed())
	}
	if c.WriteGasUsed() != 0 {
		t.Fatalf("write gas should be 0 after reset, got %d", c.WriteGasUsed())
	}
	if c.WarmSlotCount() != 0 {
		t.Fatalf("warm slots should be 0 after reset, got %d", c.WarmSlotCount())
	}
	if len(c.Records()) != 0 {
		t.Fatalf("records should be empty after reset, got %d", len(c.Records()))
	}
	if c.Remaining() != cfg.AccessGasLimit {
		t.Fatalf("remaining should equal limit after reset")
	}
}

func TestAccessGasCounter_WarmSlotTracking(t *testing.T) {
	cfg := DefaultAccessGasConfig()
	c := NewAccessGasCounter(cfg)

	addr := testAddr(5)
	slot := testSlot(5)

	if c.IsWarm(addr, slot) {
		t.Fatal("slot should not be warm before access")
	}

	_, _ = c.TrackRead(addr, slot)

	if !c.IsWarm(addr, slot) {
		t.Fatal("slot should be warm after read")
	}

	if c.WarmSlotCount() != 1 {
		t.Fatalf("expected 1 warm slot, got %d", c.WarmSlotCount())
	}
}

func TestAccessGasCounter_DifferentSlotsSameAddr(t *testing.T) {
	cfg := DefaultAccessGasConfig()
	c := NewAccessGasCounter(cfg)

	addr := testAddr(6)
	slot1 := testSlot(1)
	slot2 := testSlot(2)

	// Both should be cold.
	cost1, _ := c.TrackRead(addr, slot1)
	cost2, _ := c.TrackRead(addr, slot2)

	if cost1 != cfg.ReadGasCost || cost2 != cfg.ReadGasCost {
		t.Fatalf("both reads should be cold: cost1=%d, cost2=%d", cost1, cost2)
	}

	if c.WarmSlotCount() != 2 {
		t.Fatalf("expected 2 warm slots, got %d", c.WarmSlotCount())
	}
}

func TestAccessGasCounter_ConcurrentAccess(t *testing.T) {
	cfg := DefaultAccessGasConfig()
	c := NewAccessGasCounter(cfg)

	var wg sync.WaitGroup
	for i := byte(0); i < 50; i++ {
		wg.Add(1)
		go func(b byte) {
			defer wg.Done()
			_, _ = c.TrackRead(testAddr(b), testSlot(b))
			_, _ = c.TrackWrite(testAddr(b), testSlot(b+128))
		}(i)
	}
	wg.Wait()

	// 50 cold reads + 50 cold writes.
	expectedRead := 50 * cfg.ReadGasCost
	expectedWrite := 50 * cfg.WriteGasCost
	if c.ReadGasUsed() != expectedRead {
		t.Fatalf("expected read gas %d, got %d", expectedRead, c.ReadGasUsed())
	}
	if c.WriteGasUsed() != expectedWrite {
		t.Fatalf("expected write gas %d, got %d", expectedWrite, c.WriteGasUsed())
	}
}

func TestAccessGasCounter_Records(t *testing.T) {
	cfg := DefaultAccessGasConfig()
	c := NewAccessGasCounter(cfg)

	addr := testAddr(7)
	slot := testSlot(7)

	_, _ = c.TrackRead(addr, slot)
	_, _ = c.TrackWrite(addr, slot)

	recs := c.Records()
	if len(recs) != 2 {
		t.Fatalf("expected 2 records, got %d", len(recs))
	}

	if recs[0].IsWrite {
		t.Fatal("first record should be a read")
	}
	if !recs[0].Warm {
		// First access: should not be warm.
	}
	if recs[0].Warm {
		t.Fatal("first access should be cold")
	}

	if !recs[1].IsWrite {
		t.Fatal("second record should be a write")
	}
	if !recs[1].Warm {
		t.Fatal("second access should be warm (slot warmed by read)")
	}
}

func TestAccessGasCounter_Summary(t *testing.T) {
	cfg := DefaultAccessGasConfig()
	c := NewAccessGasCounter(cfg)

	_, _ = c.TrackRead(testAddr(1), testSlot(1))

	s := c.Summary()
	if len(s) == 0 {
		t.Fatal("summary should not be empty")
	}
}

func TestAccessGasCounter_Config(t *testing.T) {
	cfg := DefaultAccessGasConfig()
	c := NewAccessGasCounter(cfg)

	got := c.Config()
	if got.AccessGasLimit != cfg.AccessGasLimit {
		t.Fatalf("config mismatch: %d != %d", got.AccessGasLimit, cfg.AccessGasLimit)
	}
	if got.ReadGasCost != cfg.ReadGasCost {
		t.Fatalf("config mismatch: %d != %d", got.ReadGasCost, cfg.ReadGasCost)
	}
}

func TestAccessGasCounter_TotalAccessGas(t *testing.T) {
	cfg := DefaultAccessGasConfig()
	c := NewAccessGasCounter(cfg)

	_, _ = c.TrackRead(testAddr(1), testSlot(1))
	_, _ = c.TrackWrite(testAddr(2), testSlot(2))

	expected := cfg.ReadGasCost + cfg.WriteGasCost
	if c.TotalAccessGasUsed() != expected {
		t.Fatalf("expected total %d, got %d", expected, c.TotalAccessGasUsed())
	}
}

func TestAccessGasCounter_WriteExceedsLimit(t *testing.T) {
	cfg := AccessGasConfig{
		AccessGasLimit:  4000,
		ComputeGasLimit: 30_000_000,
		ReadGasCost:     2100,
		WriteGasCost:    5000,
		WarmReadCost:    100,
		WarmWriteCost:   100,
	}
	c := NewAccessGasCounter(cfg)

	// Cold write needs 5000, limit is 4000.
	_, err := c.TrackWrite(testAddr(1), testSlot(1))
	if err == nil {
		t.Fatal("expected error when write exceeds limit")
	}
	if !errors.Is(err, ErrAccessGasExceeded) {
		t.Fatalf("expected ErrAccessGasExceeded, got: %v", err)
	}
}

func TestDefaultAccessGasConfig(t *testing.T) {
	cfg := DefaultAccessGasConfig()
	if cfg.AccessGasLimit != DefaultAccessGasLimit {
		t.Fatalf("expected %d, got %d", DefaultAccessGasLimit, cfg.AccessGasLimit)
	}
	if cfg.ComputeGasLimit != DefaultComputeGasLimit {
		t.Fatalf("expected %d, got %d", DefaultComputeGasLimit, cfg.ComputeGasLimit)
	}
	if cfg.ReadGasCost != DefaultReadGas {
		t.Fatalf("expected %d, got %d", DefaultReadGas, cfg.ReadGasCost)
	}
	if cfg.WriteGasCost != DefaultWriteGas {
		t.Fatalf("expected %d, got %d", DefaultWriteGas, cfg.WriteGasCost)
	}
}
