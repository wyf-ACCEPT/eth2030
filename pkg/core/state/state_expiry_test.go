package state

import (
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestDefaultStateExpiryConfig(t *testing.T) {
	cfg := DefaultStateExpiryConfig()
	if cfg.ExpiryPeriod != 256 {
		t.Errorf("ExpiryPeriod = %d, want 256", cfg.ExpiryPeriod)
	}
	if cfg.MaxWitnessSize != 128*1024 {
		t.Errorf("MaxWitnessSize = %d, want %d", cfg.MaxWitnessSize, 128*1024)
	}
	if cfg.RevivalGasCost != 50000 {
		t.Errorf("RevivalGasCost = %d, want 50000", cfg.RevivalGasCost)
	}
}

func TestNewStateExpiryManager(t *testing.T) {
	m := NewStateExpiryManager(DefaultStateExpiryConfig())
	if m == nil {
		t.Fatal("NewStateExpiryManager returned nil")
	}
	stats := m.GetExpiryStats()
	if stats.TotalAccounts != 0 {
		t.Errorf("TotalAccounts = %d, want 0", stats.TotalAccounts)
	}
}

func TestStateExpiryTouchAccount(t *testing.T) {
	m := NewStateExpiryManager(DefaultStateExpiryConfig())
	addr := types.BytesToAddress([]byte{0xAA})

	m.TouchAccount(addr, 10)
	rec := m.GetRecord(addr)
	if rec == nil {
		t.Fatal("nil after TouchAccount")
	}
	if rec.LastAccessEpoch != 10 {
		t.Errorf("LastAccessEpoch = %d, want 10", rec.LastAccessEpoch)
	}
	if rec.Expired {
		t.Error("should not be expired")
	}

	// Update to higher epoch.
	m.TouchAccount(addr, 20)
	if m.GetRecord(addr).LastAccessEpoch != 20 {
		t.Error("epoch should update to 20")
	}

	// Older epoch ignored.
	m.TouchAccount(addr, 5)
	if m.GetRecord(addr).LastAccessEpoch != 20 {
		t.Error("older epoch should be ignored")
	}
}

func TestStateExpiryTouchAccount_ExpiredIgnored(t *testing.T) {
	cfg := DefaultStateExpiryConfig()
	cfg.ExpiryPeriod = 10
	m := NewStateExpiryManager(cfg)
	addr := types.BytesToAddress([]byte{0xDD})

	m.TouchAccount(addr, 1)
	m.ExpireStaleState(100)
	m.TouchAccount(addr, 200)

	rec := m.GetRecord(addr)
	if !rec.Expired {
		t.Error("should still be expired")
	}
	if rec.LastAccessEpoch != 1 {
		t.Errorf("epoch should be 1, got %d", rec.LastAccessEpoch)
	}
}

func TestStateExpiryTouchStorage(t *testing.T) {
	m := NewStateExpiryManager(DefaultStateExpiryConfig())
	addr := types.BytesToAddress([]byte{0x01})
	key1 := types.BytesToHash([]byte{0x42})
	key2 := types.BytesToHash([]byte{0x43})

	m.TouchStorage(addr, key1, 15)
	m.TouchStorage(addr, key2, 20)

	rec := m.GetRecord(addr)
	if rec.LastAccessEpoch != 20 {
		t.Errorf("LastAccessEpoch = %d, want 20", rec.LastAccessEpoch)
	}
	if len(rec.StorageKeys) != 2 {
		t.Fatalf("StorageKeys len = %d, want 2", len(rec.StorageKeys))
	}
	if rec.StorageKeys[key1] != 15 {
		t.Errorf("key1 epoch = %d, want 15", rec.StorageKeys[key1])
	}

	// Older storage epoch ignored.
	m.TouchStorage(addr, key1, 5)
	if m.GetRecord(addr).StorageKeys[key1] != 15 {
		t.Error("older storage key epoch should be ignored")
	}
}

func TestStateExpiryTouchStorage_ExpiredIgnored(t *testing.T) {
	cfg := DefaultStateExpiryConfig()
	cfg.ExpiryPeriod = 10
	m := NewStateExpiryManager(cfg)
	addr := types.BytesToAddress([]byte{0x04})
	key := types.BytesToHash([]byte{0x44})

	m.TouchStorage(addr, key, 1)
	m.ExpireStaleState(100)
	m.TouchStorage(addr, key, 200)

	if m.GetRecord(addr).StorageKeys[key] != 1 {
		t.Error("storage key epoch should not change for expired account")
	}
}

func TestStateExpiryExpireStaleState(t *testing.T) {
	cfg := DefaultStateExpiryConfig()
	cfg.ExpiryPeriod = 50
	m := NewStateExpiryManager(cfg)

	addr1 := types.BytesToAddress([]byte{0x01})
	addr2 := types.BytesToAddress([]byte{0x02})
	addr3 := types.BytesToAddress([]byte{0x03})

	m.TouchAccount(addr1, 10) // 10+50=60 < 100 -> expire
	m.TouchAccount(addr2, 40) // 40+50=90 < 100 -> expire
	m.TouchAccount(addr3, 80) // 80+50=130 > 100 -> active

	expired := m.ExpireStaleState(100)
	if len(expired) != 2 {
		t.Fatalf("expired count = %d, want 2", len(expired))
	}
	if !m.IsExpired(addr1) || !m.IsExpired(addr2) {
		t.Error("addr1 and addr2 should be expired")
	}
	if m.IsExpired(addr3) {
		t.Error("addr3 should not be expired")
	}

	// Already expired accounts are not re-expired.
	again := m.ExpireStaleState(200)
	expiredInSecondPass := 0
	for _, a := range again {
		if a == addr1 || a == addr2 {
			t.Error("already expired account should not appear again")
		}
		expiredInSecondPass++
	}
	if expiredInSecondPass != 1 { // addr3 now expired
		t.Errorf("second pass expired = %d, want 1", expiredInSecondPass)
	}

	stats := m.GetExpiryStats()
	if stats.LastExpiredEpoch != 200 {
		t.Errorf("LastExpiredEpoch = %d, want 200", stats.LastExpiredEpoch)
	}
}

func TestStateExpiryExpireStaleState_Boundary(t *testing.T) {
	cfg := DefaultStateExpiryConfig()
	cfg.ExpiryPeriod = 100
	m := NewStateExpiryManager(cfg)
	addr := types.BytesToAddress([]byte{0x01})
	m.TouchAccount(addr, 0)

	// At exactly boundary (0+100=100), uses >, so not expired.
	if len(m.ExpireStaleState(100)) != 0 {
		t.Error("should not expire at exact boundary")
	}
	// One past boundary.
	if len(m.ExpireStaleState(101)) != 1 {
		t.Error("should expire one past boundary")
	}
}

func TestStateExpiryExpireStaleState_NoExpirableDoesNotUpdateEpoch(t *testing.T) {
	cfg := DefaultStateExpiryConfig()
	cfg.ExpiryPeriod = 1000
	m := NewStateExpiryManager(cfg)
	m.TouchAccount(types.BytesToAddress([]byte{0x01}), 500)

	m.ExpireStaleState(600)
	if m.GetExpiryStats().LastExpiredEpoch != 0 {
		t.Error("LastExpiredEpoch should not update when nothing expires")
	}
}

func TestStateExpiryIsExpired(t *testing.T) {
	m := NewStateExpiryManager(DefaultStateExpiryConfig())
	if m.IsExpired(types.BytesToAddress([]byte{0xFF})) {
		t.Error("unknown address should not be expired")
	}
	addr := types.BytesToAddress([]byte{0x01})
	m.TouchAccount(addr, 100)
	if m.IsExpired(addr) {
		t.Error("recently touched should not be expired")
	}
}

func TestStateExpiryReviveAccount(t *testing.T) {
	cfg := DefaultStateExpiryConfig()
	cfg.ExpiryPeriod = 10
	m := NewStateExpiryManager(cfg)
	addr := types.BytesToAddress([]byte{0x01})
	m.TouchAccount(addr, 1)
	m.ExpireStaleState(100)

	// Success case.
	if err := m.ReviveAccount(addr, []byte{0xDE, 0xAD}); err != nil {
		t.Fatalf("ReviveAccount: %v", err)
	}
	if m.IsExpired(addr) {
		t.Error("should not be expired after revival")
	}

	// Not tracked.
	if err := m.ReviveAccount(types.BytesToAddress([]byte{0x99}), []byte{0x01}); err != errStateExpiryNotFound {
		t.Errorf("err = %v, want errStateExpiryNotFound", err)
	}

	// Not expired.
	m.TouchAccount(types.BytesToAddress([]byte{0x02}), 100)
	if err := m.ReviveAccount(types.BytesToAddress([]byte{0x02}), []byte{0x01}); err != errStateExpiryNotExpired {
		t.Errorf("err = %v, want errStateExpiryNotExpired", err)
	}
}

func TestStateExpiryReviveAccount_ProofValidation(t *testing.T) {
	cfg := DefaultStateExpiryConfig()
	cfg.ExpiryPeriod = 10
	cfg.MaxWitnessSize = 8
	m := NewStateExpiryManager(cfg)
	addr := types.BytesToAddress([]byte{0x01})
	m.TouchAccount(addr, 1)
	m.ExpireStaleState(100)

	// Empty proof.
	if err := m.ReviveAccount(addr, nil); err != errStateExpiryBadProof {
		t.Errorf("nil proof: err = %v, want errStateExpiryBadProof", err)
	}
	if err := m.ReviveAccount(addr, []byte{}); err != errStateExpiryBadProof {
		t.Errorf("empty proof: err = %v, want errStateExpiryBadProof", err)
	}

	// Too large.
	if err := m.ReviveAccount(addr, make([]byte, 9)); err != errStateExpiryProofSize {
		t.Errorf("oversized proof: err = %v, want errStateExpiryProofSize", err)
	}

	// Exact limit succeeds.
	exact := make([]byte, 8)
	exact[0] = 1
	if err := m.ReviveAccount(addr, exact); err != nil {
		t.Fatalf("exact size proof failed: %v", err)
	}
}

func TestStateExpiryReviveAccount_StorageKeysPreserved(t *testing.T) {
	cfg := DefaultStateExpiryConfig()
	cfg.ExpiryPeriod = 10
	m := NewStateExpiryManager(cfg)
	addr := types.BytesToAddress([]byte{0x01})
	m.TouchStorage(addr, types.BytesToHash([]byte{0x11}), 5)
	m.TouchStorage(addr, types.BytesToHash([]byte{0x22}), 5)
	m.ExpireStaleState(100)

	_ = m.ReviveAccount(addr, []byte{0x01})
	if len(m.GetRecord(addr).StorageKeys) != 2 {
		t.Error("storage keys should be preserved after revival")
	}
}

func TestStateExpiryGetExpiryStats(t *testing.T) {
	cfg := DefaultStateExpiryConfig()
	cfg.ExpiryPeriod = 10
	m := NewStateExpiryManager(cfg)

	for i := 0; i < 5; i++ {
		m.TouchAccount(types.BytesToAddress([]byte{byte(i)}), 1)
	}
	for i := 5; i < 8; i++ {
		m.TouchAccount(types.BytesToAddress([]byte{byte(i)}), 90)
	}
	m.ExpireStaleState(50)

	stats := m.GetExpiryStats()
	if stats.TotalAccounts != 8 {
		t.Errorf("TotalAccounts = %d, want 8", stats.TotalAccounts)
	}
	if stats.ExpiredCount != 5 {
		t.Errorf("ExpiredCount = %d, want 5", stats.ExpiredCount)
	}
	if stats.ActiveCount != 3 {
		t.Errorf("ActiveCount = %d, want 3", stats.ActiveCount)
	}
	if stats.LastExpiredEpoch != 50 {
		t.Errorf("LastExpiredEpoch = %d, want 50", stats.LastExpiredEpoch)
	}
}

func TestStateExpiryGetRecord_DeepCopy(t *testing.T) {
	m := NewStateExpiryManager(DefaultStateExpiryConfig())
	addr := types.BytesToAddress([]byte{0x01})
	m.TouchStorage(addr, types.BytesToHash([]byte{0x42}), 10)

	rec := m.GetRecord(addr)
	rec.LastAccessEpoch = 9999
	rec.StorageKeys[types.BytesToHash([]byte{0xFF})] = 1234

	orig := m.GetRecord(addr)
	if orig.LastAccessEpoch != 10 {
		t.Error("original should be unaffected by copy mutation")
	}
	if len(orig.StorageKeys) != 1 {
		t.Error("original storage keys should be unaffected")
	}
}

func TestStateExpiryGetRecord_Nil(t *testing.T) {
	m := NewStateExpiryManager(DefaultStateExpiryConfig())
	if m.GetRecord(types.BytesToAddress([]byte{0xFF})) != nil {
		t.Error("should return nil for untracked address")
	}
}

func TestStateExpiryConfig_Method(t *testing.T) {
	cfg := StateExpiryConfig{ExpiryPeriod: 500, MaxWitnessSize: 1024, RevivalGasCost: 75000}
	m := NewStateExpiryManager(cfg)
	if m.Config() != cfg {
		t.Error("Config() mismatch")
	}
}

func TestStateExpiryConcurrent(t *testing.T) {
	cfg := DefaultStateExpiryConfig()
	cfg.ExpiryPeriod = 50
	m := NewStateExpiryManager(cfg)

	var wg sync.WaitGroup
	n := 100
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			addr := types.BytesToAddress([]byte{byte(idx)})
			m.TouchAccount(addr, uint64(idx))
			m.TouchStorage(addr, types.BytesToHash([]byte{byte(idx)}), uint64(idx))
		}(i)
	}
	wg.Wait()

	if m.GetExpiryStats().TotalAccounts != n {
		t.Errorf("TotalAccounts = %d, want %d", m.GetExpiryStats().TotalAccounts, n)
	}

	// Concurrent expire and reads.
	wg.Add(3)
	go func() { defer wg.Done(); m.ExpireStaleState(500) }()
	go func() { defer wg.Done(); m.GetExpiryStats() }()
	go func() { defer wg.Done(); m.IsExpired(types.BytesToAddress([]byte{0x01})) }()
	wg.Wait()
}

func TestStateExpiryConcurrentRevive(t *testing.T) {
	cfg := DefaultStateExpiryConfig()
	cfg.ExpiryPeriod = 10
	m := NewStateExpiryManager(cfg)
	addr := types.BytesToAddress([]byte{0x01})
	m.TouchAccount(addr, 1)
	m.ExpireStaleState(100)

	var wg sync.WaitGroup
	results := make(chan error, 10)
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func() {
			defer wg.Done()
			results <- m.ReviveAccount(addr, []byte{0x01})
		}()
	}
	wg.Wait()
	close(results)

	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Errorf("concurrent revive successes = %d, want 1", successes)
	}
}
