package vm

import (
	"sync"
	"testing"
)

func TestResourceType_String(t *testing.T) {
	tests := []struct {
		rt   ResourceType
		want string
	}{
		{Compute, "Compute"},
		{StorageRead, "StorageRead"},
		{StorageWrite, "StorageWrite"},
		{Bandwidth, "Bandwidth"},
		{StateGrowth, "StateGrowth"},
		{ResourceType(99), "Unknown"},
	}
	for _, tt := range tests {
		if got := tt.rt.String(); got != tt.want {
			t.Errorf("ResourceType(%d).String() = %q, want %q", tt.rt, got, tt.want)
		}
	}
}

func TestDefaultGasSchedulerConfig(t *testing.T) {
	cfg := DefaultGasSchedulerConfig()
	if cfg.BlockGasLimit != 30_000_000 {
		t.Fatalf("expected block gas limit 30M, got %d", cfg.BlockGasLimit)
	}
	// Compute should have full block limit.
	if cfg.Resources[Compute].Limit != 30_000_000 {
		t.Fatalf("expected compute limit 30M, got %d", cfg.Resources[Compute].Limit)
	}
	// StorageWrite should be 12.5% of block.
	if cfg.Resources[StorageWrite].Limit != 30_000_000/8 {
		t.Fatalf("expected storage write limit %d, got %d", 30_000_000/8, cfg.Resources[StorageWrite].Limit)
	}
	// State growth multiplier should be 10.
	if cfg.Resources[StateGrowth].BaseFeeMultiplier != 10 {
		t.Fatalf("expected state growth multiplier 10, got %d", cfg.Resources[StateGrowth].BaseFeeMultiplier)
	}
}

func TestGasScheduler_AccountGas_Basic(t *testing.T) {
	gs := NewGasScheduler(DefaultGasSchedulerConfig())

	// Account some compute gas.
	if err := gs.AccountGas(Compute, 1000); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := gs.UsedGas(Compute); got != 1000 {
		t.Fatalf("expected 1000 used, got %d", got)
	}
	remaining := gs.RemainingGas(Compute)
	if remaining != 30_000_000-1000 {
		t.Fatalf("expected %d remaining, got %d", 30_000_000-1000, remaining)
	}
}

func TestGasScheduler_AccountGas_Exhausted(t *testing.T) {
	cfg := DefaultGasSchedulerConfig()
	cfg.Resources[Compute] = ResourceConfig{Limit: 100, TargetUsage: 50, BaseFeeMultiplier: 1, MinGasPrice: 1}
	gs := NewGasScheduler(cfg)

	if err := gs.AccountGas(Compute, 80); err != nil {
		t.Fatal(err)
	}

	// Should fail: 80 + 30 > 100.
	err := gs.AccountGas(Compute, 30)
	if err == nil {
		t.Fatal("expected error when exceeding gas limit")
	}
}

func TestGasScheduler_AccountGas_InvalidResource(t *testing.T) {
	gs := NewGasScheduler(DefaultGasSchedulerConfig())
	err := gs.AccountGas(ResourceType(99), 100)
	if err == nil {
		t.Fatal("expected error for invalid resource type")
	}
}

func TestGasScheduler_MultiDimensional(t *testing.T) {
	gs := NewGasScheduler(DefaultGasSchedulerConfig())

	// Use gas in different dimensions independently.
	if err := gs.AccountGas(Compute, 5000); err != nil {
		t.Fatal(err)
	}
	if err := gs.AccountGas(StorageRead, 2000); err != nil {
		t.Fatal(err)
	}
	if err := gs.AccountGas(StorageWrite, 1000); err != nil {
		t.Fatal(err)
	}
	if err := gs.AccountGas(Bandwidth, 3000); err != nil {
		t.Fatal(err)
	}
	if err := gs.AccountGas(StateGrowth, 500); err != nil {
		t.Fatal(err)
	}

	total := gs.TotalUsed()
	expected := uint64(5000 + 2000 + 1000 + 3000 + 500)
	if total != expected {
		t.Fatalf("expected total %d, got %d", expected, total)
	}
}

func TestGasScheduler_RemainingGas_InvalidResource(t *testing.T) {
	gs := NewGasScheduler(DefaultGasSchedulerConfig())
	if got := gs.RemainingGas(ResourceType(99)); got != 0 {
		t.Fatalf("expected 0 for invalid resource, got %d", got)
	}
}

func TestGasScheduler_GasPrice_AtTarget(t *testing.T) {
	cfg := DefaultGasSchedulerConfig()
	cfg.Resources[Compute] = ResourceConfig{
		Limit:             100_000,
		TargetUsage:       50_000,
		BaseFeeMultiplier: 1,
		MinGasPrice:       1,
	}
	gs := NewGasScheduler(cfg)

	// Use exactly the target amount.
	_ = gs.AccountGas(Compute, 50_000)

	price := gs.GasPrice(Compute, 100)
	// At target: ratio = 1000, factor = 500 + 500 = 1000, price = 100*1*1000/1000 = 100.
	if price != 100 {
		t.Fatalf("expected price 100 at target, got %d", price)
	}
}

func TestGasScheduler_GasPrice_UnderTarget(t *testing.T) {
	cfg := DefaultGasSchedulerConfig()
	cfg.Resources[Compute] = ResourceConfig{
		Limit:             100_000,
		TargetUsage:       50_000,
		BaseFeeMultiplier: 1,
		MinGasPrice:       1,
	}
	gs := NewGasScheduler(cfg)

	// Use nothing: price should be discounted.
	price := gs.GasPrice(Compute, 100)
	// At zero: ratio = 0, factor = 500, price = 100*500/1000 = 50.
	if price != 50 {
		t.Fatalf("expected discounted price 50, got %d", price)
	}
}

func TestGasScheduler_GasPrice_OverTarget(t *testing.T) {
	cfg := DefaultGasSchedulerConfig()
	cfg.Resources[Compute] = ResourceConfig{
		Limit:             100_000,
		TargetUsage:       50_000,
		BaseFeeMultiplier: 1,
		MinGasPrice:       1,
	}
	gs := NewGasScheduler(cfg)

	// Use 75_000 (50% over target).
	_ = gs.AccountGas(Compute, 75_000)

	price := gs.GasPrice(Compute, 100)
	// excess = (75000-50000)*1000/50000 = 500
	// factor = 1000+500 = 1500
	// price = 100*1500/1000 = 150
	if price != 150 {
		t.Fatalf("expected premium price 150, got %d", price)
	}
}

func TestGasScheduler_GasPrice_WithMultiplier(t *testing.T) {
	cfg := DefaultGasSchedulerConfig()
	cfg.Resources[StorageWrite] = ResourceConfig{
		Limit:             10_000,
		TargetUsage:       5_000,
		BaseFeeMultiplier: 5,
		MinGasPrice:       1,
	}
	gs := NewGasScheduler(cfg)

	// Use exactly target.
	_ = gs.AccountGas(StorageWrite, 5_000)

	price := gs.GasPrice(StorageWrite, 100)
	// At target with 5x multiplier: 100*5*1000/1000 = 500.
	if price != 500 {
		t.Fatalf("expected price 500, got %d", price)
	}
}

func TestGasScheduler_GasPrice_MinPrice(t *testing.T) {
	cfg := DefaultGasSchedulerConfig()
	cfg.Resources[Compute] = ResourceConfig{
		Limit:             100_000,
		TargetUsage:       50_000,
		BaseFeeMultiplier: 0, // zero multiplier
		MinGasPrice:       42,
	}
	gs := NewGasScheduler(cfg)

	price := gs.GasPrice(Compute, 0)
	// baseFee=0, multiplier=0 -> basePrice falls to MinGasPrice=42.
	// With zero usage: factor=500, price=42*500/1000=21. But min is 42.
	if price != 42 {
		t.Fatalf("expected min price 42, got %d", price)
	}
}

func TestGasScheduler_GasPrice_ZeroTarget(t *testing.T) {
	cfg := DefaultGasSchedulerConfig()
	cfg.Resources[Compute] = ResourceConfig{
		Limit:             100_000,
		TargetUsage:       0, // zero target
		BaseFeeMultiplier: 1,
		MinGasPrice:       1,
	}
	gs := NewGasScheduler(cfg)

	price := gs.GasPrice(Compute, 100)
	if price != 100 {
		t.Fatalf("expected base price 100 with zero target, got %d", price)
	}
}

func TestGasScheduler_GasPrice_InvalidResource(t *testing.T) {
	gs := NewGasScheduler(DefaultGasSchedulerConfig())
	price := gs.GasPrice(ResourceType(99), 100)
	if price != 0 {
		t.Fatalf("expected 0 for invalid resource, got %d", price)
	}
}

func TestGasScheduler_Utilization(t *testing.T) {
	cfg := DefaultGasSchedulerConfig()
	cfg.Resources[Compute] = ResourceConfig{Limit: 1000, TargetUsage: 500, BaseFeeMultiplier: 1, MinGasPrice: 1}
	gs := NewGasScheduler(cfg)

	if got := gs.Utilization(Compute); got != 0 {
		t.Fatalf("expected 0%% utilization, got %d%%", got)
	}

	_ = gs.AccountGas(Compute, 500)
	if got := gs.Utilization(Compute); got != 50 {
		t.Fatalf("expected 50%% utilization, got %d%%", got)
	}

	_ = gs.AccountGas(Compute, 500)
	if got := gs.Utilization(Compute); got != 100 {
		t.Fatalf("expected 100%% utilization, got %d%%", got)
	}
}

func TestGasScheduler_Utilization_InvalidResource(t *testing.T) {
	gs := NewGasScheduler(DefaultGasSchedulerConfig())
	if got := gs.Utilization(ResourceType(99)); got != 0 {
		t.Fatalf("expected 0 for invalid resource, got %d", got)
	}
}

func TestGasScheduler_IsExhausted(t *testing.T) {
	cfg := DefaultGasSchedulerConfig()
	cfg.Resources[Compute] = ResourceConfig{Limit: 100, TargetUsage: 50, BaseFeeMultiplier: 1, MinGasPrice: 1}
	gs := NewGasScheduler(cfg)

	if gs.IsExhausted() {
		t.Fatal("should not be exhausted initially")
	}

	_ = gs.AccountGas(Compute, 100)
	if !gs.IsExhausted() {
		t.Fatal("should be exhausted after using full limit")
	}
}

func TestGasScheduler_SnapshotRestore(t *testing.T) {
	gs := NewGasScheduler(DefaultGasSchedulerConfig())

	_ = gs.AccountGas(Compute, 1000)
	_ = gs.AccountGas(StorageRead, 500)

	snap := gs.Snapshot()

	// Use more gas.
	_ = gs.AccountGas(Compute, 2000)
	_ = gs.AccountGas(StorageWrite, 300)

	if gs.UsedGas(Compute) != 3000 {
		t.Fatalf("expected 3000, got %d", gs.UsedGas(Compute))
	}

	// Restore snapshot.
	gs.RestoreSnapshot(snap)
	if gs.UsedGas(Compute) != 1000 {
		t.Fatalf("expected 1000 after restore, got %d", gs.UsedGas(Compute))
	}
	if gs.UsedGas(StorageRead) != 500 {
		t.Fatalf("expected 500 after restore, got %d", gs.UsedGas(StorageRead))
	}
	if gs.UsedGas(StorageWrite) != 0 {
		t.Fatalf("expected 0 after restore, got %d", gs.UsedGas(StorageWrite))
	}
}

func TestGasScheduler_ResetForBlock(t *testing.T) {
	gs := NewGasScheduler(DefaultGasSchedulerConfig())
	_ = gs.AccountGas(Compute, 5000)
	_ = gs.AccountGas(StorageRead, 1000)

	gs.ResetForBlock(0) // keep same block gas limit

	if gs.UsedGas(Compute) != 0 {
		t.Fatalf("expected 0 after reset, got %d", gs.UsedGas(Compute))
	}
	if gs.UsedGas(StorageRead) != 0 {
		t.Fatalf("expected 0 after reset, got %d", gs.UsedGas(StorageRead))
	}
}

func TestGasScheduler_Concurrent(t *testing.T) {
	cfg := DefaultGasSchedulerConfig()
	cfg.Resources[Compute] = ResourceConfig{Limit: 1_000_000, TargetUsage: 500_000, BaseFeeMultiplier: 1, MinGasPrice: 1}
	gs := NewGasScheduler(cfg)

	var wg sync.WaitGroup
	const goroutines = 10
	const perGoroutine = 100

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				_ = gs.AccountGas(Compute, 1)
			}
		}()
	}
	wg.Wait()

	if gs.UsedGas(Compute) != goroutines*perGoroutine {
		t.Fatalf("expected %d, got %d", goroutines*perGoroutine, gs.UsedGas(Compute))
	}
}

func TestGasScheduler_AllDimensionsIndependent(t *testing.T) {
	cfg := DefaultGasSchedulerConfig()
	// Set small limits for easy testing.
	for i := 0; i < resourceTypeCount; i++ {
		cfg.Resources[i] = ResourceConfig{Limit: 100, TargetUsage: 50, BaseFeeMultiplier: 1, MinGasPrice: 1}
	}
	gs := NewGasScheduler(cfg)

	// Exhaust compute.
	if err := gs.AccountGas(Compute, 100); err != nil {
		t.Fatal(err)
	}
	// Other dimensions should still be available.
	if err := gs.AccountGas(StorageRead, 50); err != nil {
		t.Fatalf("storage read should be independent: %v", err)
	}
	if err := gs.AccountGas(StorageWrite, 50); err != nil {
		t.Fatalf("storage write should be independent: %v", err)
	}
	if err := gs.AccountGas(Bandwidth, 50); err != nil {
		t.Fatalf("bandwidth should be independent: %v", err)
	}
	if err := gs.AccountGas(StateGrowth, 50); err != nil {
		t.Fatalf("state growth should be independent: %v", err)
	}
}

func TestGasScheduler_GasPrice_ExcessCapped(t *testing.T) {
	cfg := DefaultGasSchedulerConfig()
	cfg.Resources[Compute] = ResourceConfig{
		Limit:             100_000,
		TargetUsage:       10_000, // very low target
		BaseFeeMultiplier: 1,
		MinGasPrice:       1,
	}
	gs := NewGasScheduler(cfg)

	// Use 90_000 (9x target, but excess cap is 2000/1000 = 2x premium).
	_ = gs.AccountGas(Compute, 90_000)

	price := gs.GasPrice(Compute, 100)
	// excess = (90000-10000)*1000/10000 = 8000, capped to 2000
	// factor = 1000+2000 = 3000
	// price = 100*3000/1000 = 300 (3x)
	if price != 300 {
		t.Fatalf("expected capped price 300, got %d", price)
	}
}
