package state

import (
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestExpiryDefaultConfig(t *testing.T) {
	cfg := DefaultExpiryConfig()
	if cfg.ExpiryPeriod != 1_000_000 {
		t.Errorf("ExpiryPeriod = %d, want 1000000", cfg.ExpiryPeriod)
	}
	if cfg.RevivalGasCost != 25000 {
		t.Errorf("RevivalGasCost = %d, want 25000", cfg.RevivalGasCost)
	}
	if cfg.MaxExpiredPerBlock != 100 {
		t.Errorf("MaxExpiredPerBlock = %d, want 100", cfg.MaxExpiredPerBlock)
	}
}

func TestTouchAccount(t *testing.T) {
	m := NewExpiryManager(DefaultExpiryConfig())
	addr := types.BytesToAddress([]byte{0x01})

	m.TouchAccount(addr, 100)

	info := m.GetExpiry(addr)
	if info == nil {
		t.Fatal("GetExpiry returned nil after TouchAccount")
	}
	if info.LastAccessed != 100 {
		t.Errorf("LastAccessed = %d, want 100", info.LastAccessed)
	}
	if info.Expired {
		t.Error("account should not be expired after touch")
	}
}

func TestCheckExpiryNotExpired(t *testing.T) {
	m := NewExpiryManager(DefaultExpiryConfig())
	addr := types.BytesToAddress([]byte{0x01})

	m.TouchAccount(addr, 100)

	// Check at block 100 + 999_999 = 1_000_099 (still within period).
	if m.CheckExpiry(addr, 1_000_099) {
		t.Error("account should not be expired within ExpiryPeriod")
	}

	// Check at block exactly at boundary: LastAccessed + ExpiryPeriod = 1_000_100.
	// CheckExpiry uses > (strictly greater), so exact boundary is not expired.
	if m.CheckExpiry(addr, 1_000_100) {
		t.Error("account should not be expired at exact boundary")
	}
}

func TestCheckExpiryExpired(t *testing.T) {
	m := NewExpiryManager(DefaultExpiryConfig())
	addr := types.BytesToAddress([]byte{0x01})

	m.TouchAccount(addr, 100)

	// Check at block 100 + 1_000_000 + 1 = 1_000_101 (past expiry).
	if !m.CheckExpiry(addr, 1_000_101) {
		t.Error("account should be expired past ExpiryPeriod")
	}
}

func TestExpireAccounts(t *testing.T) {
	cfg := DefaultExpiryConfig()
	cfg.ExpiryPeriod = 100 // short period for testing
	m := NewExpiryManager(cfg)

	addr1 := types.BytesToAddress([]byte{0x01})
	addr2 := types.BytesToAddress([]byte{0x02})
	addr3 := types.BytesToAddress([]byte{0x03})

	m.TouchAccount(addr1, 10)
	m.TouchAccount(addr2, 50)
	m.TouchAccount(addr3, 200)

	// At block 200, addr1 (10+100=110 < 200) and addr2 (50+100=150 < 200) should expire.
	// addr3 (200+100=300 > 200) should not.
	expired := m.ExpireAccounts(200)

	if len(expired) != 2 {
		t.Fatalf("expired count = %d, want 2", len(expired))
	}

	// Verify addr1 and addr2 are expired.
	info1 := m.GetExpiry(addr1)
	info2 := m.GetExpiry(addr2)
	info3 := m.GetExpiry(addr3)

	if !info1.Expired {
		t.Error("addr1 should be expired")
	}
	if !info2.Expired {
		t.Error("addr2 should be expired")
	}
	if info3.Expired {
		t.Error("addr3 should not be expired")
	}
}

func TestReviveAccount(t *testing.T) {
	cfg := DefaultExpiryConfig()
	cfg.ExpiryPeriod = 100
	m := NewExpiryManager(cfg)

	addr := types.BytesToAddress([]byte{0x01})
	m.TouchAccount(addr, 10)

	// Expire the account.
	m.ExpireAccounts(200)
	if !m.CheckExpiry(addr, 200) {
		t.Fatal("account should be expired before revival")
	}

	// Revive with a proof.
	proof := []byte{0x01, 0x02, 0x03}
	err := m.ReviveAccount(addr, proof, 300)
	if err != nil {
		t.Fatalf("ReviveAccount: %v", err)
	}

	info := m.GetExpiry(addr)
	if info.Expired {
		t.Error("account should not be expired after revival")
	}
	if info.LastAccessed != 300 {
		t.Errorf("LastAccessed = %d, want 300", info.LastAccessed)
	}
}

func TestReviveAccountNotExpired(t *testing.T) {
	m := NewExpiryManager(DefaultExpiryConfig())
	addr := types.BytesToAddress([]byte{0x01})

	m.TouchAccount(addr, 100)

	// Attempt to revive a non-expired account.
	err := m.ReviveAccount(addr, []byte{0x01}, 200)
	if err == nil {
		t.Fatal("ReviveAccount should error for non-expired account")
	}
	if err != errAccountNotExpired {
		t.Errorf("error = %v, want errAccountNotExpired", err)
	}
}

func TestExpiredCount(t *testing.T) {
	cfg := DefaultExpiryConfig()
	cfg.ExpiryPeriod = 100
	m := NewExpiryManager(cfg)

	addr1 := types.BytesToAddress([]byte{0x01})
	addr2 := types.BytesToAddress([]byte{0x02})
	addr3 := types.BytesToAddress([]byte{0x03})

	m.TouchAccount(addr1, 10)
	m.TouchAccount(addr2, 10)
	m.TouchAccount(addr3, 200)

	if m.ExpiredCount() != 0 {
		t.Errorf("initial ExpiredCount = %d, want 0", m.ExpiredCount())
	}

	m.ExpireAccounts(200)

	if m.ExpiredCount() != 2 {
		t.Errorf("ExpiredCount = %d, want 2", m.ExpiredCount())
	}
}

func TestActiveCount(t *testing.T) {
	cfg := DefaultExpiryConfig()
	cfg.ExpiryPeriod = 100
	m := NewExpiryManager(cfg)

	addr1 := types.BytesToAddress([]byte{0x01})
	addr2 := types.BytesToAddress([]byte{0x02})
	addr3 := types.BytesToAddress([]byte{0x03})

	m.TouchAccount(addr1, 10)
	m.TouchAccount(addr2, 10)
	m.TouchAccount(addr3, 200)

	if m.ActiveCount() != 3 {
		t.Errorf("initial ActiveCount = %d, want 3", m.ActiveCount())
	}

	m.ExpireAccounts(200)

	if m.ActiveCount() != 1 {
		t.Errorf("ActiveCount after expiry = %d, want 1", m.ActiveCount())
	}
}

func TestConcurrentTouch(t *testing.T) {
	m := NewExpiryManager(DefaultExpiryConfig())

	var wg sync.WaitGroup
	n := 100
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			addr := types.BytesToAddress([]byte{byte(idx)})
			m.TouchAccount(addr, uint64(idx))
		}(i)
	}
	wg.Wait()

	// All 100 accounts should be tracked (some may share addresses if byte
	// wraps, but byte(0..99) are all distinct).
	active := m.ActiveCount()
	if active != n {
		t.Errorf("ActiveCount = %d, want %d", active, n)
	}
	if m.ExpiredCount() != 0 {
		t.Errorf("ExpiredCount = %d, want 0", m.ExpiredCount())
	}
}
