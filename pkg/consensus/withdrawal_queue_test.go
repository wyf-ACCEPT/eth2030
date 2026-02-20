package consensus

import (
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func testAddr(b byte) types.Address {
	var addr types.Address
	addr[19] = b
	return addr
}

func TestNewWithdrawalQueue(t *testing.T) {
	cfg := DefaultWithdrawalQueueConfig()
	wq := NewWithdrawalQueue(cfg)
	if wq == nil {
		t.Fatal("NewWithdrawalQueue returned nil")
	}
	if wq.PendingCount() != 0 {
		t.Fatalf("expected 0 pending, got %d", wq.PendingCount())
	}
	stats := wq.Stats()
	if stats.Pending != 0 || stats.Processed != 0 || stats.TotalAmount != 0 {
		t.Fatalf("unexpected initial stats: %+v", stats)
	}
}

func TestEnqueueBasic(t *testing.T) {
	cfg := DefaultWithdrawalQueueConfig()
	wq := NewWithdrawalQueue(cfg)

	req := WithdrawalRequest{
		ValidatorIndex: 1,
		Amount:         32_000_000_000,
		TargetAddress:  testAddr(0x01),
		RequestSlot:    100,
		Priority:       5,
	}

	if err := wq.Enqueue(req); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	if wq.PendingCount() != 1 {
		t.Fatalf("expected 1 pending, got %d", wq.PendingCount())
	}
}

func TestEnqueueValidation(t *testing.T) {
	cfg := DefaultWithdrawalQueueConfig()
	wq := NewWithdrawalQueue(cfg)

	// Zero amount.
	err := wq.Enqueue(WithdrawalRequest{
		ValidatorIndex: 1,
		Amount:         0,
		TargetAddress:  testAddr(0x01),
	})
	if err != ErrWithdrawalZeroAmount {
		t.Fatalf("expected ErrWithdrawalZeroAmount, got %v", err)
	}

	// Zero address.
	err = wq.Enqueue(WithdrawalRequest{
		ValidatorIndex: 1,
		Amount:         1000,
		TargetAddress:  types.Address{},
	})
	if err != ErrWithdrawalZeroAddress {
		t.Fatalf("expected ErrWithdrawalZeroAddress, got %v", err)
	}
}

func TestEnqueueDuplicate(t *testing.T) {
	cfg := DefaultWithdrawalQueueConfig()
	wq := NewWithdrawalQueue(cfg)

	req := WithdrawalRequest{
		ValidatorIndex: 1,
		Amount:         1000,
		TargetAddress:  testAddr(0x01),
		RequestSlot:    100,
	}
	if err := wq.Enqueue(req); err != nil {
		t.Fatalf("first Enqueue failed: %v", err)
	}

	// Duplicate should fail.
	err := wq.Enqueue(req)
	if err != ErrWithdrawalAlreadyQueued {
		t.Fatalf("expected ErrWithdrawalAlreadyQueued, got %v", err)
	}
}

func TestEnqueueAlreadyProcessed(t *testing.T) {
	cfg := DefaultWithdrawalQueueConfig()
	cfg.MinWithdrawalDelay = 0
	cfg.MaxWithdrawalsPerSlot = 16
	cfg.ChurnLimit = 16
	wq := NewWithdrawalQueue(cfg)

	req := WithdrawalRequest{
		ValidatorIndex: 1,
		Amount:         1000,
		TargetAddress:  testAddr(0x01),
		RequestSlot:    0,
	}
	if err := wq.Enqueue(req); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Process it.
	wq.ProcessSlot(0)

	// Attempting to enqueue same validator again should fail.
	req2 := WithdrawalRequest{
		ValidatorIndex: 1,
		Amount:         2000,
		TargetAddress:  testAddr(0x01),
		RequestSlot:    1,
	}
	err := wq.Enqueue(req2)
	if err != ErrWithdrawalProcessed {
		t.Fatalf("expected ErrWithdrawalProcessed, got %v", err)
	}
}

func TestEnqueueQueueFull(t *testing.T) {
	cfg := WithdrawalQueueConfig{
		MaxQueueSize:          2,
		MaxWithdrawalsPerSlot: 16,
		MinWithdrawalDelay:    0,
		ChurnLimit:            16,
	}
	wq := NewWithdrawalQueue(cfg)

	for i := uint64(0); i < 2; i++ {
		err := wq.Enqueue(WithdrawalRequest{
			ValidatorIndex: i,
			Amount:         1000,
			TargetAddress:  testAddr(byte(i + 1)),
			RequestSlot:    0,
		})
		if err != nil {
			t.Fatalf("Enqueue %d failed: %v", i, err)
		}
	}

	// Third should fail.
	err := wq.Enqueue(WithdrawalRequest{
		ValidatorIndex: 99,
		Amount:         1000,
		TargetAddress:  testAddr(0x99),
		RequestSlot:    0,
	})
	if err != ErrWithdrawalQueueFull {
		t.Fatalf("expected ErrWithdrawalQueueFull, got %v", err)
	}
}

func TestProcessSlotBasic(t *testing.T) {
	cfg := WithdrawalQueueConfig{
		MaxQueueSize:          100,
		MaxWithdrawalsPerSlot: 16,
		MinWithdrawalDelay:    10,
		ChurnLimit:            16,
	}
	wq := NewWithdrawalQueue(cfg)

	// Enqueue at slot 0.
	if err := wq.Enqueue(WithdrawalRequest{
		ValidatorIndex: 1,
		Amount:         32_000_000_000,
		TargetAddress:  testAddr(0x01),
		RequestSlot:    0,
	}); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Processing at slot 5 should return nothing (delay not met).
	result := wq.ProcessSlot(5)
	if len(result) != 0 {
		t.Fatalf("expected 0 processed, got %d", len(result))
	}
	if wq.PendingCount() != 1 {
		t.Fatalf("expected 1 pending, got %d", wq.PendingCount())
	}

	// Processing at slot 10 should process the withdrawal.
	result = wq.ProcessSlot(10)
	if len(result) != 1 {
		t.Fatalf("expected 1 processed, got %d", len(result))
	}
	if result[0].ValidatorIndex != 1 {
		t.Fatalf("expected validator 1, got %d", result[0].ValidatorIndex)
	}
	if wq.PendingCount() != 0 {
		t.Fatalf("expected 0 pending, got %d", wq.PendingCount())
	}
}

func TestProcessSlotPriorityOrdering(t *testing.T) {
	cfg := WithdrawalQueueConfig{
		MaxQueueSize:          100,
		MaxWithdrawalsPerSlot: 2,
		MinWithdrawalDelay:    0,
		ChurnLimit:            10,
	}
	wq := NewWithdrawalQueue(cfg)

	// Enqueue in order: low priority first, high priority second.
	wq.Enqueue(WithdrawalRequest{
		ValidatorIndex: 1,
		Amount:         1000,
		TargetAddress:  testAddr(0x01),
		RequestSlot:    0,
		Priority:       1,
	})
	wq.Enqueue(WithdrawalRequest{
		ValidatorIndex: 2,
		Amount:         2000,
		TargetAddress:  testAddr(0x02),
		RequestSlot:    0,
		Priority:       10,
	})
	wq.Enqueue(WithdrawalRequest{
		ValidatorIndex: 3,
		Amount:         3000,
		TargetAddress:  testAddr(0x03),
		RequestSlot:    0,
		Priority:       5,
	})

	// Should process top 2 by priority: validator 2 (pri=10), validator 3 (pri=5).
	result := wq.ProcessSlot(0)
	if len(result) != 2 {
		t.Fatalf("expected 2 processed, got %d", len(result))
	}
	if result[0].ValidatorIndex != 2 {
		t.Fatalf("expected validator 2 first (priority 10), got %d", result[0].ValidatorIndex)
	}
	if result[1].ValidatorIndex != 3 {
		t.Fatalf("expected validator 3 second (priority 5), got %d", result[1].ValidatorIndex)
	}
	if wq.PendingCount() != 1 {
		t.Fatalf("expected 1 remaining, got %d", wq.PendingCount())
	}
}

func TestProcessSlotChurnLimit(t *testing.T) {
	cfg := WithdrawalQueueConfig{
		MaxQueueSize:          100,
		MaxWithdrawalsPerSlot: 16,
		MinWithdrawalDelay:    0,
		ChurnLimit:            2,
	}
	wq := NewWithdrawalQueue(cfg)

	// Enqueue 5 requests.
	for i := uint64(0); i < 5; i++ {
		wq.Enqueue(WithdrawalRequest{
			ValidatorIndex: i,
			Amount:         1000,
			TargetAddress:  testAddr(byte(i + 1)),
			RequestSlot:    0,
		})
	}

	// Churn limit of 2 means only 2 processed per slot.
	result := wq.ProcessSlot(0)
	if len(result) != 2 {
		t.Fatalf("expected 2 processed (churn limit), got %d", len(result))
	}
	if wq.PendingCount() != 3 {
		t.Fatalf("expected 3 remaining, got %d", wq.PendingCount())
	}
}

func TestIsProcessed(t *testing.T) {
	cfg := WithdrawalQueueConfig{
		MaxQueueSize:          100,
		MaxWithdrawalsPerSlot: 16,
		MinWithdrawalDelay:    0,
		ChurnLimit:            16,
	}
	wq := NewWithdrawalQueue(cfg)

	wq.Enqueue(WithdrawalRequest{
		ValidatorIndex: 42,
		Amount:         1000,
		TargetAddress:  testAddr(0x42),
		RequestSlot:    0,
	})

	if wq.IsProcessed(42) {
		t.Fatal("validator should not be processed yet")
	}

	wq.ProcessSlot(0)

	if !wq.IsProcessed(42) {
		t.Fatal("validator should be processed")
	}
	if wq.IsProcessed(99) {
		t.Fatal("unknown validator should not be processed")
	}
}

func TestGetPosition(t *testing.T) {
	cfg := WithdrawalQueueConfig{
		MaxQueueSize:          100,
		MaxWithdrawalsPerSlot: 16,
		MinWithdrawalDelay:    0,
		ChurnLimit:            16,
	}
	wq := NewWithdrawalQueue(cfg)

	// Enqueue 3 with same priority and same slot -- ordered by validator index.
	for i := uint64(10); i < 13; i++ {
		wq.Enqueue(WithdrawalRequest{
			ValidatorIndex: i,
			Amount:         1000,
			TargetAddress:  testAddr(byte(i)),
			RequestSlot:    0,
			Priority:       5,
		})
	}

	if pos := wq.GetPosition(10); pos != 0 {
		t.Fatalf("expected position 0 for validator 10, got %d", pos)
	}
	if pos := wq.GetPosition(11); pos != 1 {
		t.Fatalf("expected position 1 for validator 11, got %d", pos)
	}
	if pos := wq.GetPosition(12); pos != 2 {
		t.Fatalf("expected position 2 for validator 12, got %d", pos)
	}
	if pos := wq.GetPosition(999); pos != -1 {
		t.Fatalf("expected -1 for unknown validator, got %d", pos)
	}
}

func TestCancelWithdrawal(t *testing.T) {
	cfg := DefaultWithdrawalQueueConfig()
	wq := NewWithdrawalQueue(cfg)

	wq.Enqueue(WithdrawalRequest{
		ValidatorIndex: 1,
		Amount:         1000,
		TargetAddress:  testAddr(0x01),
		RequestSlot:    0,
	})
	wq.Enqueue(WithdrawalRequest{
		ValidatorIndex: 2,
		Amount:         2000,
		TargetAddress:  testAddr(0x02),
		RequestSlot:    0,
	})

	if wq.PendingCount() != 2 {
		t.Fatalf("expected 2 pending, got %d", wq.PendingCount())
	}

	// Cancel validator 1.
	if !wq.CancelWithdrawal(1) {
		t.Fatal("expected CancelWithdrawal to return true")
	}
	if wq.PendingCount() != 1 {
		t.Fatalf("expected 1 pending after cancel, got %d", wq.PendingCount())
	}

	// Cancel again should return false.
	if wq.CancelWithdrawal(1) {
		t.Fatal("expected CancelWithdrawal to return false for already cancelled")
	}

	// Cancel unknown should return false.
	if wq.CancelWithdrawal(999) {
		t.Fatal("expected CancelWithdrawal to return false for unknown")
	}

	// After cancellation, validator can re-enqueue.
	err := wq.Enqueue(WithdrawalRequest{
		ValidatorIndex: 1,
		Amount:         500,
		TargetAddress:  testAddr(0x01),
		RequestSlot:    1,
	})
	if err != nil {
		t.Fatalf("re-enqueue after cancel failed: %v", err)
	}
}

func TestStats(t *testing.T) {
	cfg := WithdrawalQueueConfig{
		MaxQueueSize:          100,
		MaxWithdrawalsPerSlot: 1,
		MinWithdrawalDelay:    0,
		ChurnLimit:            10,
	}
	wq := NewWithdrawalQueue(cfg)

	wq.Enqueue(WithdrawalRequest{
		ValidatorIndex: 1,
		Amount:         1000,
		TargetAddress:  testAddr(0x01),
		RequestSlot:    0,
	})
	wq.Enqueue(WithdrawalRequest{
		ValidatorIndex: 2,
		Amount:         2000,
		TargetAddress:  testAddr(0x02),
		RequestSlot:    0,
	})

	stats := wq.Stats()
	if stats.Pending != 2 {
		t.Fatalf("expected 2 pending, got %d", stats.Pending)
	}
	if stats.Processed != 0 {
		t.Fatalf("expected 0 processed, got %d", stats.Processed)
	}
	if stats.TotalAmount != 3000 {
		t.Fatalf("expected total 3000, got %d", stats.TotalAmount)
	}

	// Process one.
	wq.ProcessSlot(0)

	stats = wq.Stats()
	if stats.Pending != 1 {
		t.Fatalf("expected 1 pending after process, got %d", stats.Pending)
	}
	if stats.Processed != 1 {
		t.Fatalf("expected 1 processed, got %d", stats.Processed)
	}
}

func TestProcessSlotSameSlotOrdering(t *testing.T) {
	// When priority and slot are equal, lower validator index comes first.
	cfg := WithdrawalQueueConfig{
		MaxQueueSize:          100,
		MaxWithdrawalsPerSlot: 10,
		MinWithdrawalDelay:    0,
		ChurnLimit:            10,
	}
	wq := NewWithdrawalQueue(cfg)

	// Enqueue in reverse order.
	for i := uint64(5); i > 0; i-- {
		wq.Enqueue(WithdrawalRequest{
			ValidatorIndex: i,
			Amount:         1000,
			TargetAddress:  testAddr(byte(i)),
			RequestSlot:    0,
			Priority:       3,
		})
	}

	result := wq.ProcessSlot(0)
	if len(result) != 5 {
		t.Fatalf("expected 5, got %d", len(result))
	}
	for i, r := range result {
		expected := uint64(i + 1)
		if r.ValidatorIndex != expected {
			t.Fatalf("position %d: expected validator %d, got %d", i, expected, r.ValidatorIndex)
		}
	}
}

func TestConcurrentEnqueueAndProcess(t *testing.T) {
	cfg := WithdrawalQueueConfig{
		MaxQueueSize:          10000,
		MaxWithdrawalsPerSlot: 100,
		MinWithdrawalDelay:    0,
		ChurnLimit:            100,
	}
	wq := NewWithdrawalQueue(cfg)

	var wg sync.WaitGroup
	// Enqueue from multiple goroutines.
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				idx := uint64(base*50 + i)
				wq.Enqueue(WithdrawalRequest{
					ValidatorIndex: idx,
					Amount:         1000,
					TargetAddress:  testAddr(byte(idx%255 + 1)),
					RequestSlot:    0,
				})
			}
		}(g)
	}

	// Process concurrently too.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for slot := uint64(0); slot < 10; slot++ {
			wq.ProcessSlot(slot)
		}
	}()

	wg.Wait()

	stats := wq.Stats()
	total := stats.Pending + stats.Processed
	if total != 500 {
		t.Fatalf("expected 500 total (pending+processed), got %d", total)
	}
}

func TestProcessSlotEmptyQueue(t *testing.T) {
	cfg := DefaultWithdrawalQueueConfig()
	wq := NewWithdrawalQueue(cfg)

	result := wq.ProcessSlot(100)
	if len(result) != 0 {
		t.Fatalf("expected 0 from empty queue, got %d", len(result))
	}
}

func TestDefaultWithdrawalQueueConfig(t *testing.T) {
	cfg := DefaultWithdrawalQueueConfig()
	if cfg.MaxQueueSize != 65536 {
		t.Fatalf("unexpected MaxQueueSize: %d", cfg.MaxQueueSize)
	}
	if cfg.MaxWithdrawalsPerSlot != 16 {
		t.Fatalf("unexpected MaxWithdrawalsPerSlot: %d", cfg.MaxWithdrawalsPerSlot)
	}
	if cfg.MinWithdrawalDelay != 256 {
		t.Fatalf("unexpected MinWithdrawalDelay: %d", cfg.MinWithdrawalDelay)
	}
	if cfg.ChurnLimit != 8 {
		t.Fatalf("unexpected ChurnLimit: %d", cfg.ChurnLimit)
	}
}
