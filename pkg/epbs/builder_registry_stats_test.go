package epbs

import (
	"sync"
	"testing"
	"time"
)

// testBuilderAddr creates a unique [20]byte address for builder registry tests.
func testBuilderAddr(b byte) [20]byte {
	var a [20]byte
	a[19] = b
	return a
}

// testBuilderPubkey creates a unique [48]byte pubkey for builder registry tests.
func testBuilderPubkey(b byte) [48]byte {
	var pk [48]byte
	pk[0] = b
	return pk
}

// testBuilderInfo creates a BuilderInfo suitable for registry tests.
func testBuilderInfo(seed byte, active bool) *BuilderInfo {
	return &BuilderInfo{
		Address:      testBuilderAddr(seed),
		Pubkey:       testBuilderPubkey(seed),
		FeeRecipient: testBuilderAddr(seed + 100),
		GasLimit:     30_000_000,
		RegisteredAt: time.Now().Unix(),
		Active:       active,
		Stake:        32_000_000_000, // 32 ETH in Gwei
	}
}

// testBidRecord creates a BuilderBidRecord for testing.
func testBidRecord(slot, value uint64, won bool) *BuilderBidRecord {
	return &BuilderBidRecord{
		Slot:      slot,
		Value:     value,
		GasLimit:  30_000_000,
		Timestamp: time.Now().Unix(),
		Won:       won,
	}
}

func TestBuilderRegistryNew(t *testing.T) {
	r := NewBuilderRegistry(100)
	if r == nil {
		t.Fatal("NewBuilderRegistry returned nil")
	}
	if r.maxBuilders != 100 {
		t.Errorf("maxBuilders = %d, want 100", r.maxBuilders)
	}
	if r.BuilderCount() != 0 {
		t.Errorf("initial BuilderCount = %d, want 0", r.BuilderCount())
	}
}

func TestBuilderRegistryNewDefault(t *testing.T) {
	r := NewBuilderRegistry(0)
	if r.maxBuilders != 1024 {
		t.Errorf("default maxBuilders = %d, want 1024", r.maxBuilders)
	}
}

func TestBuilderRegistryRegister(t *testing.T) {
	r := NewBuilderRegistry(100)
	info := testBuilderInfo(1, true)
	if err := r.RegisterBuilder(info); err != nil {
		t.Fatalf("RegisterBuilder: %v", err)
	}
	if r.BuilderCount() != 1 {
		t.Errorf("BuilderCount = %d, want 1", r.BuilderCount())
	}
}

func TestBuilderRegistryRegisterNil(t *testing.T) {
	r := NewBuilderRegistry(100)
	if err := r.RegisterBuilder(nil); err != ErrRegistryNilInfo {
		t.Errorf("expected ErrRegistryNilInfo, got %v", err)
	}
}

func TestBuilderRegistryDuplicateRegister(t *testing.T) {
	r := NewBuilderRegistry(100)
	info := testBuilderInfo(1, true)
	r.RegisterBuilder(info)

	err := r.RegisterBuilder(info)
	if err != ErrRegistryDuplicate {
		t.Errorf("expected ErrRegistryDuplicate, got %v", err)
	}
}

func TestBuilderRegistryMaxBuilders(t *testing.T) {
	r := NewBuilderRegistry(3)
	for i := byte(1); i <= 3; i++ {
		if err := r.RegisterBuilder(testBuilderInfo(i, true)); err != nil {
			t.Fatalf("RegisterBuilder(%d): %v", i, err)
		}
	}

	err := r.RegisterBuilder(testBuilderInfo(4, true))
	if err != ErrRegistryFull {
		t.Errorf("expected ErrRegistryFull, got %v", err)
	}
}

func TestBuilderRegistryDeregister(t *testing.T) {
	r := NewBuilderRegistry(100)
	info := testBuilderInfo(1, true)
	r.RegisterBuilder(info)

	if err := r.DeregisterBuilder(info.Address); err != nil {
		t.Fatalf("DeregisterBuilder: %v", err)
	}
	if r.BuilderCount() != 0 {
		t.Errorf("BuilderCount after deregister = %d, want 0", r.BuilderCount())
	}
}

func TestBuilderRegistryDeregisterNotFound(t *testing.T) {
	r := NewBuilderRegistry(100)
	err := r.DeregisterBuilder(testBuilderAddr(99))
	if err != ErrRegistryNotFound {
		t.Errorf("expected ErrRegistryNotFound, got %v", err)
	}
}

func TestBuilderRegistryGetBuilder(t *testing.T) {
	r := NewBuilderRegistry(100)
	info := testBuilderInfo(1, true)
	r.RegisterBuilder(info)

	got, ok := r.GetBuilder(info.Address)
	if !ok {
		t.Fatal("GetBuilder should return true for registered builder")
	}
	if got.Address != info.Address {
		t.Errorf("Address mismatch")
	}
	if got.GasLimit != info.GasLimit {
		t.Errorf("GasLimit = %d, want %d", got.GasLimit, info.GasLimit)
	}
	if got.Stake != info.Stake {
		t.Errorf("Stake = %d, want %d", got.Stake, info.Stake)
	}
}

func TestBuilderRegistryGetBuilderNotFound(t *testing.T) {
	r := NewBuilderRegistry(100)
	_, ok := r.GetBuilder(testBuilderAddr(99))
	if ok {
		t.Error("GetBuilder should return false for unregistered builder")
	}
}

func TestBuilderRegistryGetBuilderDefensiveCopy(t *testing.T) {
	r := NewBuilderRegistry(100)
	info := testBuilderInfo(1, true)
	r.RegisterBuilder(info)

	got, _ := r.GetBuilder(info.Address)
	got.GasLimit = 999

	got2, _ := r.GetBuilder(info.Address)
	if got2.GasLimit == 999 {
		t.Error("GetBuilder should return a defensive copy")
	}
}

func TestBuilderRegistryActiveBuilders(t *testing.T) {
	r := NewBuilderRegistry(100)
	r.RegisterBuilder(testBuilderInfo(1, true))
	r.RegisterBuilder(testBuilderInfo(2, false))
	r.RegisterBuilder(testBuilderInfo(3, true))
	r.RegisterBuilder(testBuilderInfo(4, false))
	r.RegisterBuilder(testBuilderInfo(5, true))

	active := r.ActiveBuilders()
	if len(active) != 3 {
		t.Errorf("ActiveBuilders count = %d, want 3", len(active))
	}
}

func TestBuilderRegistryRecordBid(t *testing.T) {
	r := NewBuilderRegistry(100)
	addr := testBuilderAddr(1)
	r.RegisterBuilder(testBuilderInfo(1, true))

	bid := testBidRecord(100, 5000, false)
	if err := r.RecordBid(addr, bid); err != nil {
		t.Fatalf("RecordBid: %v", err)
	}
}

func TestBuilderRegistryRecordBidUnknown(t *testing.T) {
	r := NewBuilderRegistry(100)
	bid := testBidRecord(100, 5000, false)
	err := r.RecordBid(testBuilderAddr(99), bid)
	if err != ErrRegistryNotFound {
		t.Errorf("expected ErrRegistryNotFound, got %v", err)
	}
}

func TestBuilderRegistryRecordBidNil(t *testing.T) {
	r := NewBuilderRegistry(100)
	addr := testBuilderAddr(1)
	r.RegisterBuilder(testBuilderInfo(1, true))
	err := r.RecordBid(addr, nil)
	if err != ErrRegistryNilBidRecord {
		t.Errorf("expected ErrRegistryNilBidRecord, got %v", err)
	}
}

func TestBuilderRegistryStats(t *testing.T) {
	r := NewBuilderRegistry(100)
	addr := testBuilderAddr(1)
	r.RegisterBuilder(testBuilderInfo(1, true))

	r.RecordBid(addr, testBidRecord(100, 1000, true))
	r.RecordBid(addr, testBidRecord(101, 2000, false))
	r.RecordBid(addr, testBidRecord(102, 3000, true))

	stats, err := r.GetBuilderStats(addr)
	if err != nil {
		t.Fatalf("GetBuilderStats: %v", err)
	}
	if stats.TotalBids != 3 {
		t.Errorf("TotalBids = %d, want 3", stats.TotalBids)
	}
	if stats.WonBids != 2 {
		t.Errorf("WonBids = %d, want 2", stats.WonBids)
	}
	if stats.TotalValue != 6000 {
		t.Errorf("TotalValue = %d, want 6000", stats.TotalValue)
	}
	if stats.AvgBidValue != 2000 {
		t.Errorf("AvgBidValue = %d, want 2000", stats.AvgBidValue)
	}
}

func TestBuilderRegistryStatsUnknown(t *testing.T) {
	r := NewBuilderRegistry(100)
	_, err := r.GetBuilderStats(testBuilderAddr(99))
	if err != ErrRegistryNotFound {
		t.Errorf("expected ErrRegistryNotFound, got %v", err)
	}
}

func TestBuilderRegistryWinRate(t *testing.T) {
	r := NewBuilderRegistry(100)
	addr := testBuilderAddr(1)
	r.RegisterBuilder(testBuilderInfo(1, true))

	// 3 out of 4 bids won => 75% win rate.
	r.RecordBid(addr, testBidRecord(100, 1000, true))
	r.RecordBid(addr, testBidRecord(101, 1000, true))
	r.RecordBid(addr, testBidRecord(102, 1000, false))
	r.RecordBid(addr, testBidRecord(103, 1000, true))

	stats, _ := r.GetBuilderStats(addr)
	if stats.WinRate != 0.75 {
		t.Errorf("WinRate = %f, want 0.75", stats.WinRate)
	}
}

func TestBuilderRegistryTopBuilders(t *testing.T) {
	r := NewBuilderRegistry(100)

	// Builder 1: 100% win rate.
	addr1 := testBuilderAddr(1)
	r.RegisterBuilder(testBuilderInfo(1, true))
	r.RecordBid(addr1, testBidRecord(100, 5000, true))
	r.RecordBid(addr1, testBidRecord(101, 5000, true))

	// Builder 2: 50% win rate.
	addr2 := testBuilderAddr(2)
	r.RegisterBuilder(testBuilderInfo(2, true))
	r.RecordBid(addr2, testBidRecord(100, 3000, true))
	r.RecordBid(addr2, testBidRecord(101, 3000, false))

	// Builder 3: 0% win rate.
	addr3 := testBuilderAddr(3)
	r.RegisterBuilder(testBuilderInfo(3, true))
	r.RecordBid(addr3, testBidRecord(100, 1000, false))

	// Builder 4: no bids (should be excluded).
	r.RegisterBuilder(testBuilderInfo(4, true))

	top := r.TopBuilders(10)
	if len(top) != 3 {
		t.Fatalf("TopBuilders count = %d, want 3", len(top))
	}

	// Should be ordered: builder 1 (100%), builder 2 (50%), builder 3 (0%).
	if top[0].Address != addr1 {
		t.Error("top[0] should be builder 1 with 100% win rate")
	}
	if top[1].Address != addr2 {
		t.Error("top[1] should be builder 2 with 50% win rate")
	}
	if top[2].Address != addr3 {
		t.Error("top[2] should be builder 3 with 0% win rate")
	}
}

func TestBuilderRegistryTopBuildersLimitedN(t *testing.T) {
	r := NewBuilderRegistry(100)
	for i := byte(1); i <= 5; i++ {
		addr := testBuilderAddr(i)
		r.RegisterBuilder(testBuilderInfo(i, true))
		r.RecordBid(addr, testBidRecord(100, uint64(i)*1000, true))
	}

	top := r.TopBuilders(2)
	if len(top) != 2 {
		t.Errorf("TopBuilders(2) count = %d, want 2", len(top))
	}
}

func TestBuilderRegistryPruneInactive(t *testing.T) {
	r := NewBuilderRegistry(100)

	// Builder 1: registered long ago, no bids.
	info1 := testBuilderInfo(1, true)
	info1.RegisteredAt = 1000
	r.RegisterBuilder(info1)

	// Builder 2: registered long ago, but has a recent bid.
	info2 := testBuilderInfo(2, true)
	info2.RegisteredAt = 1000
	r.RegisterBuilder(info2)
	r.RecordBid(info2.Address, &BuilderBidRecord{
		Slot: 100, Value: 5000, Timestamp: 9000, Won: false,
	})

	// Builder 3: recently registered.
	info3 := testBuilderInfo(3, true)
	info3.RegisteredAt = 8000
	r.RegisterBuilder(info3)

	// Prune anything with latest activity before 5000.
	pruned := r.PruneInactive(5000)
	if pruned != 1 {
		t.Errorf("pruned = %d, want 1", pruned)
	}
	if r.BuilderCount() != 2 {
		t.Errorf("BuilderCount after prune = %d, want 2", r.BuilderCount())
	}

	// Builder 1 should be pruned.
	_, ok := r.GetBuilder(info1.Address)
	if ok {
		t.Error("builder 1 should have been pruned")
	}

	// Builder 2 and 3 should remain.
	_, ok = r.GetBuilder(info2.Address)
	if !ok {
		t.Error("builder 2 should remain (recent bid)")
	}
	_, ok = r.GetBuilder(info3.Address)
	if !ok {
		t.Error("builder 3 should remain (recent registration)")
	}
}

func TestBuilderRegistryConcurrentAccess(t *testing.T) {
	r := NewBuilderRegistry(200)

	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	// Concurrent registrations.
	for i := byte(0); i < 50; i++ {
		wg.Add(1)
		go func(seed byte) {
			defer wg.Done()
			if err := r.RegisterBuilder(testBuilderInfo(seed, true)); err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()

	// Concurrent bid recording.
	for i := byte(0); i < 50; i++ {
		wg.Add(1)
		go func(seed byte) {
			defer wg.Done()
			addr := testBuilderAddr(seed)
			bid := testBidRecord(uint64(seed)*10, uint64(seed)*1000, seed%2 == 0)
			if err := r.RecordBid(addr, bid); err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent error: %v", err)
	}

	if r.BuilderCount() != 50 {
		t.Errorf("BuilderCount = %d, want 50", r.BuilderCount())
	}
}

func TestBuilderRegistryBuilderInfoFields(t *testing.T) {
	r := NewBuilderRegistry(100)
	info := &BuilderInfo{
		Address:      testBuilderAddr(42),
		Pubkey:       testBuilderPubkey(42),
		FeeRecipient: testBuilderAddr(142),
		GasLimit:     25_000_000,
		RegisteredAt: 1234567890,
		Active:       true,
		Stake:        64_000_000_000,
	}
	r.RegisterBuilder(info)

	got, ok := r.GetBuilder(info.Address)
	if !ok {
		t.Fatal("builder not found")
	}
	if got.Pubkey != info.Pubkey {
		t.Error("Pubkey mismatch")
	}
	if got.FeeRecipient != info.FeeRecipient {
		t.Error("FeeRecipient mismatch")
	}
	if got.GasLimit != 25_000_000 {
		t.Errorf("GasLimit = %d, want 25000000", got.GasLimit)
	}
	if got.RegisteredAt != 1234567890 {
		t.Errorf("RegisteredAt = %d, want 1234567890", got.RegisteredAt)
	}
	if !got.Active {
		t.Error("Active should be true")
	}
	if got.Stake != 64_000_000_000 {
		t.Errorf("Stake = %d, want 64000000000", got.Stake)
	}
}

func TestBuilderRegistryStatsNoBids(t *testing.T) {
	r := NewBuilderRegistry(100)
	addr := testBuilderAddr(1)
	r.RegisterBuilder(testBuilderInfo(1, true))

	stats, err := r.GetBuilderStats(addr)
	if err != nil {
		t.Fatalf("GetBuilderStats: %v", err)
	}
	if stats.TotalBids != 0 {
		t.Errorf("TotalBids = %d, want 0", stats.TotalBids)
	}
	if stats.WinRate != 0.0 {
		t.Errorf("WinRate = %f, want 0.0", stats.WinRate)
	}
}
