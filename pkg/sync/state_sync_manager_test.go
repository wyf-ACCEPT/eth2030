package sync

import (
	"sync"
	"testing"
)

func TestSSMNewManager(t *testing.T) {
	m := NewStateSyncManager(nil)
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	cfg := m.Config()
	if cfg.MaxConcurrent != DefaultSSMMaxConcurrent {
		t.Fatalf("expected MaxConcurrent %d, got %d", DefaultSSMMaxConcurrent, cfg.MaxConcurrent)
	}
	if cfg.BatchSize != DefaultSSMBatchSize {
		t.Fatalf("expected BatchSize %d, got %d", DefaultSSMBatchSize, cfg.BatchSize)
	}
	if cfg.RetryAttempts != DefaultSSMRetryAttempts {
		t.Fatalf("expected RetryAttempts %d, got %d", DefaultSSMRetryAttempts, cfg.RetryAttempts)
	}
}

func TestSSMNewManagerCustomConfig(t *testing.T) {
	cfg := &StateSyncConfig{
		MaxConcurrent: 4,
		BatchSize:     128,
		RetryAttempts: 5,
		TargetRoot:    [32]byte{0xaa},
	}
	m := NewStateSyncManager(cfg)
	got := m.Config()
	if got.MaxConcurrent != 4 {
		t.Fatalf("expected MaxConcurrent 4, got %d", got.MaxConcurrent)
	}
	if got.BatchSize != 128 {
		t.Fatalf("expected BatchSize 128, got %d", got.BatchSize)
	}
	if got.RetryAttempts != 5 {
		t.Fatalf("expected RetryAttempts 5, got %d", got.RetryAttempts)
	}
}

func TestSSMRequestRange(t *testing.T) {
	m := NewStateSyncManager(&StateSyncConfig{
		MaxConcurrent: 4,
		BatchSize:     10,
		RetryAttempts: 3,
		TargetRoot:    [32]byte{0x01},
	})
	m.StartSync([32]byte{0x01})

	start := [32]byte{0x00}
	end := [32]byte{0x0f}
	resp, err := m.RequestStateRange(start, end)
	if err != nil {
		t.Fatalf("RequestStateRange failed: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Accounts) == 0 {
		t.Fatal("expected accounts in response")
	}
	// First account should have the start key.
	if resp.Accounts[0].Hash != start {
		t.Fatalf("first account hash %x, expected %x", resp.Accounts[0].Hash, start)
	}
}

func TestSSMRequestRangePaused(t *testing.T) {
	m := NewStateSyncManager(nil)
	m.StartSync([32]byte{0x01})
	m.PauseSync()

	_, err := m.RequestStateRange([32]byte{}, [32]byte{0xff})
	if err != ErrSSMPaused {
		t.Fatalf("expected ErrSSMPaused, got %v", err)
	}
}

func TestSSMValidateRange(t *testing.T) {
	m := NewStateSyncManager(&StateSyncConfig{
		MaxConcurrent: 4,
		BatchSize:     5,
		RetryAttempts: 3,
		TargetRoot:    [32]byte{0x01},
	})
	m.StartSync([32]byte{0x01})

	start := [32]byte{0x00}
	end := [32]byte{0x10}
	resp, err := m.RequestStateRange(start, end)
	if err != nil {
		t.Fatalf("RequestStateRange failed: %v", err)
	}

	err = m.ValidateStateRange(resp)
	if err != nil {
		t.Fatalf("ValidateStateRange failed: %v", err)
	}
}

func TestSSMValidateRangeNil(t *testing.T) {
	m := NewStateSyncManager(nil)
	err := m.ValidateStateRange(nil)
	if err != ErrSSMEmptyRange {
		t.Fatalf("expected ErrSSMEmptyRange, got %v", err)
	}
}

func TestSSMValidateRangeInvalidProof(t *testing.T) {
	m := NewStateSyncManager(nil)
	resp := &StateRangeResponse{
		Accounts: []*StateAccount{{Hash: [32]byte{0x01}}},
		Proofs:   [][]byte{{}}, // empty proof node
	}
	err := m.ValidateStateRange(resp)
	if err == nil {
		t.Fatal("expected error for empty proof node")
	}
}

func TestSSMValidateRangeUnsortedAccounts(t *testing.T) {
	m := NewStateSyncManager(nil)
	resp := &StateRangeResponse{
		Accounts: []*StateAccount{
			{Hash: [32]byte{0x05}},
			{Hash: [32]byte{0x02}}, // out of order
		},
		Proofs: [][]byte{
			makeProofNode([32]byte{0x02}, [32]byte{0x01}),
		},
	}
	err := m.ValidateStateRange(resp)
	if err == nil {
		t.Fatal("expected error for unsorted accounts")
	}
}

func TestSSMProgress(t *testing.T) {
	m := NewStateSyncManager(&StateSyncConfig{
		MaxConcurrent: 4,
		BatchSize:     5,
		RetryAttempts: 3,
		TargetRoot:    [32]byte{0x01},
	})
	m.StartSync([32]byte{0x01})

	start := [32]byte{0x00}
	end := [32]byte{0x10}
	m.RequestStateRange(start, end)

	p := m.Progress()
	if p.AccountsSynced == 0 {
		t.Fatal("expected AccountsSynced > 0 after request")
	}
	if p.BytesDownloaded == 0 {
		t.Fatal("expected BytesDownloaded > 0 after request")
	}
	if p.StartedAt == 0 {
		t.Fatal("expected non-zero StartedAt")
	}
	if p.CurrentPhase != "accounts" {
		t.Fatalf("expected phase 'accounts', got %q", p.CurrentPhase)
	}
}

func TestSSMStartSync(t *testing.T) {
	m := NewStateSyncManager(nil)
	root := [32]byte{0xab, 0xcd}

	err := m.StartSync(root)
	if err != nil {
		t.Fatalf("StartSync failed: %v", err)
	}
	if !m.IsSyncing() {
		t.Fatal("expected IsSyncing() == true")
	}

	// Starting again should fail.
	err = m.StartSync(root)
	if err != ErrSSMAlreadySyncing {
		t.Fatalf("expected ErrSSMAlreadySyncing, got %v", err)
	}
}

func TestSSMPauseResume(t *testing.T) {
	m := NewStateSyncManager(nil)
	m.StartSync([32]byte{0x01})

	m.PauseSync()
	if !m.IsPaused() {
		t.Fatal("expected IsPaused() == true")
	}
	p := m.Progress()
	if p.CurrentPhase != "paused" {
		t.Fatalf("expected phase 'paused', got %q", p.CurrentPhase)
	}

	m.ResumeSync()
	if m.IsPaused() {
		t.Fatal("expected IsPaused() == false after resume")
	}
	p = m.Progress()
	if p.CurrentPhase != "accounts" {
		t.Fatalf("expected phase 'accounts' after resume, got %q", p.CurrentPhase)
	}
}

func TestSSMIsSyncing(t *testing.T) {
	m := NewStateSyncManager(nil)
	if m.IsSyncing() {
		t.Fatal("expected not syncing initially")
	}
	m.StartSync([32]byte{0x01})
	if !m.IsSyncing() {
		t.Fatal("expected syncing after start")
	}
	m.StopSync()
	if m.IsSyncing() {
		t.Fatal("expected not syncing after stop")
	}
}

func TestSSMBatchSize(t *testing.T) {
	cfg := &StateSyncConfig{
		MaxConcurrent: 4,
		BatchSize:     3, // small batch
		RetryAttempts: 3,
		TargetRoot:    [32]byte{0x01},
	}
	m := NewStateSyncManager(cfg)
	m.StartSync([32]byte{0x01})

	start := [32]byte{0x00}
	end := [32]byte{0xff}
	resp, err := m.RequestStateRange(start, end)
	if err != nil {
		t.Fatalf("RequestStateRange failed: %v", err)
	}
	if len(resp.Accounts) > 3 {
		t.Fatalf("expected at most 3 accounts (batch size), got %d", len(resp.Accounts))
	}
}

func TestSSMMaxConcurrent(t *testing.T) {
	cfg := &StateSyncConfig{
		MaxConcurrent: 2,
		BatchSize:     5,
		RetryAttempts: 3,
	}
	m := NewStateSyncManager(cfg)
	got := m.Config()
	if got.MaxConcurrent != 2 {
		t.Fatalf("expected MaxConcurrent 2, got %d", got.MaxConcurrent)
	}
}

func TestSSMEmptyRange(t *testing.T) {
	m := NewStateSyncManager(&StateSyncConfig{
		MaxConcurrent: 4,
		BatchSize:     5,
		RetryAttempts: 3,
	})
	m.StartSync([32]byte{0x01})

	// Request where start == end; should get exactly one account.
	key := [32]byte{0x05}
	resp, err := m.RequestStateRange(key, key)
	if err != nil {
		t.Fatalf("RequestStateRange failed: %v", err)
	}
	if len(resp.Accounts) != 1 {
		t.Fatalf("expected 1 account for equal start/end, got %d", len(resp.Accounts))
	}
	if resp.Continue {
		t.Fatal("expected Continue == false for single-key range")
	}
}

func TestSSMLargeRange(t *testing.T) {
	cfg := &StateSyncConfig{
		MaxConcurrent: 4,
		BatchSize:     100,
		RetryAttempts: 3,
		TargetRoot:    [32]byte{0x01},
	}
	m := NewStateSyncManager(cfg)
	m.StartSync([32]byte{0x01})

	start := [32]byte{0x00}
	var end [32]byte
	for i := range end {
		end[i] = 0xff
	}
	resp, err := m.RequestStateRange(start, end)
	if err != nil {
		t.Fatalf("RequestStateRange failed: %v", err)
	}
	if len(resp.Accounts) == 0 {
		t.Fatal("expected accounts for large range")
	}
	if len(resp.Accounts) > 100 {
		t.Fatalf("expected at most 100 accounts, got %d", len(resp.Accounts))
	}
	if !resp.Continue {
		t.Fatal("expected Continue == true for large range")
	}
}

func TestSSMRetryAttempts(t *testing.T) {
	cfg := &StateSyncConfig{
		MaxConcurrent: 4,
		BatchSize:     5,
		RetryAttempts: 7,
	}
	m := NewStateSyncManager(cfg)
	got := m.Config()
	if got.RetryAttempts != 7 {
		t.Fatalf("expected RetryAttempts 7, got %d", got.RetryAttempts)
	}
}

func TestSSMConfig(t *testing.T) {
	cfg := &StateSyncConfig{
		MaxConcurrent: 16,
		BatchSize:     512,
		RetryAttempts: 10,
		TargetRoot:    [32]byte{0xde, 0xad},
	}
	m := NewStateSyncManager(cfg)
	got := m.Config()

	if got.MaxConcurrent != 16 {
		t.Fatalf("expected MaxConcurrent 16, got %d", got.MaxConcurrent)
	}
	if got.BatchSize != 512 {
		t.Fatalf("expected BatchSize 512, got %d", got.BatchSize)
	}
	if got.RetryAttempts != 10 {
		t.Fatalf("expected RetryAttempts 10, got %d", got.RetryAttempts)
	}
	if got.TargetRoot != cfg.TargetRoot {
		t.Fatalf("expected TargetRoot %x, got %x", cfg.TargetRoot, got.TargetRoot)
	}
}

func TestSSMConcurrentRequests(t *testing.T) {
	m := NewStateSyncManager(&StateSyncConfig{
		MaxConcurrent: 8,
		BatchSize:     5,
		RetryAttempts: 3,
		TargetRoot:    [32]byte{0x01},
	})
	m.StartSync([32]byte{0x01})

	var wg sync.WaitGroup
	errs := make(chan error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			start := [32]byte{byte(n)}
			end := [32]byte{byte(n + 10)}
			_, err := m.RequestStateRange(start, end)
			if err != nil {
				errs <- err
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent request failed: %v", err)
	}

	p := m.Progress()
	if p.AccountsSynced == 0 {
		t.Fatal("expected some accounts synced from concurrent requests")
	}
	if m.TotalRequests() != 20 {
		t.Fatalf("expected 20 total requests, got %d", m.TotalRequests())
	}
}

func TestSSMStopSync(t *testing.T) {
	m := NewStateSyncManager(nil)
	m.StartSync([32]byte{0x01})
	m.StopSync()

	if m.IsSyncing() {
		t.Fatal("expected not syncing after stop")
	}
	p := m.Progress()
	if p.CurrentPhase != "stopped" {
		t.Fatalf("expected phase 'stopped', got %q", p.CurrentPhase)
	}
}

func TestSSMPendingRequests(t *testing.T) {
	m := NewStateSyncManager(nil)
	if m.PendingRequests() != 0 {
		t.Fatalf("expected 0 pending requests, got %d", m.PendingRequests())
	}
}

func TestSSMKeyLessThan(t *testing.T) {
	a := [32]byte{0x01}
	b := [32]byte{0x02}
	if !keyLessThan(a, b) {
		t.Fatal("expected a < b")
	}
	if keyLessThan(b, a) {
		t.Fatal("expected NOT b < a")
	}
	if keyLessThan(a, a) {
		t.Fatal("expected NOT a < a")
	}
}

func TestSSMIncrementKey(t *testing.T) {
	k := [32]byte{0x00}
	next := incrementKey(k)
	if next[31] != 1 {
		t.Fatalf("expected last byte 1, got %d", next[31])
	}

	// Overflow last byte.
	k[31] = 0xff
	next = incrementKey(k)
	if next[31] != 0 || next[30] != 1 {
		t.Fatalf("expected carry, got %x", next)
	}
}

func TestSSMValidateEmptyResponse(t *testing.T) {
	m := NewStateSyncManager(nil)
	resp := &StateRangeResponse{
		Accounts: nil,
		Proofs:   nil,
	}
	err := m.ValidateStateRange(resp)
	if err != nil {
		t.Fatalf("expected nil error for empty valid response, got %v", err)
	}
}

func TestSSMConfigDefaults(t *testing.T) {
	// Test that negative values are corrected.
	cfg := &StateSyncConfig{
		MaxConcurrent: -1,
		BatchSize:     -1,
		RetryAttempts: -1,
	}
	m := NewStateSyncManager(cfg)
	got := m.Config()
	if got.MaxConcurrent != DefaultSSMMaxConcurrent {
		t.Fatalf("expected default MaxConcurrent, got %d", got.MaxConcurrent)
	}
	if got.BatchSize != DefaultSSMBatchSize {
		t.Fatalf("expected default BatchSize, got %d", got.BatchSize)
	}
	if got.RetryAttempts != DefaultSSMRetryAttempts {
		t.Fatalf("expected default RetryAttempts, got %d", got.RetryAttempts)
	}
}
