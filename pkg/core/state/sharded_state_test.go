package state

import (
	"math/big"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// Helper to create an address with a specific first nibble.
func shardedAddr(nibble byte, tail byte) types.Address {
	var addr types.Address
	addr[0] = (nibble << 4) | tail
	addr[19] = tail
	return addr
}

func TestShardedState_NewShardedStateDB(t *testing.T) {
	s := NewShardedStateDB()
	if s.ShardCount() != 16 {
		t.Errorf("ShardCount() = %d, want 16", s.ShardCount())
	}
}

func TestShardedState_ShardIndex(t *testing.T) {
	tests := []struct {
		nibble byte
		want   int
	}{
		{0x0, 0}, {0x1, 1}, {0x5, 5}, {0xA, 10}, {0xF, 15},
	}
	for _, tt := range tests {
		addr := shardedAddr(tt.nibble, 0x01)
		got := shardIndex(addr)
		if got != tt.want {
			t.Errorf("shardIndex(0x%02x...) = %d, want %d", addr[0], got, tt.want)
		}
	}
}

func TestShardedState_BalanceGetSet(t *testing.T) {
	s := NewShardedStateDB()
	addr := shardedAddr(0x3, 0x42)

	// Default balance is zero.
	bal := s.GetBalance(addr)
	if bal.Sign() != 0 {
		t.Errorf("default balance = %s, want 0", bal)
	}

	// Set and get.
	s.SetBalance(addr, big.NewInt(1_000_000))
	bal = s.GetBalance(addr)
	if bal.Cmp(big.NewInt(1_000_000)) != 0 {
		t.Errorf("balance = %s, want 1000000", bal)
	}
}

func TestShardedState_NonceGetSet(t *testing.T) {
	s := NewShardedStateDB()
	addr := shardedAddr(0x7, 0x01)

	if n := s.GetNonce(addr); n != 0 {
		t.Errorf("default nonce = %d, want 0", n)
	}

	s.SetNonce(addr, 42)
	if n := s.GetNonce(addr); n != 42 {
		t.Errorf("nonce = %d, want 42", n)
	}
}

func TestShardedState_StorageGetSet(t *testing.T) {
	s := NewShardedStateDB()
	addr := shardedAddr(0xA, 0x01)
	key := types.BytesToHash([]byte{0x01})
	val := types.BytesToHash([]byte{0xFF})

	// Default is zero hash.
	got := s.GetState(addr, key)
	if got != (types.Hash{}) {
		t.Errorf("default storage = %s, want zero hash", got.Hex())
	}

	s.SetState(addr, key, val)
	got = s.GetState(addr, key)
	if got != val {
		t.Errorf("storage = %s, want %s", got.Hex(), val.Hex())
	}
}

func TestShardedState_ConcurrentAccess(t *testing.T) {
	s := NewShardedStateDB()
	var wg sync.WaitGroup

	// Concurrent writes to different shards should be safe.
	for i := byte(0); i < 16; i++ {
		wg.Add(1)
		go func(nibble byte) {
			defer wg.Done()
			addr := shardedAddr(nibble, 0x01)
			s.SetBalance(addr, big.NewInt(int64(nibble)*100))
			s.SetNonce(addr, uint64(nibble))
			key := types.BytesToHash([]byte{nibble})
			val := types.BytesToHash([]byte{nibble, nibble})
			s.SetState(addr, key, val)
		}(i)
	}
	wg.Wait()

	// Verify all writes.
	for i := byte(0); i < 16; i++ {
		addr := shardedAddr(i, 0x01)
		bal := s.GetBalance(addr)
		expected := big.NewInt(int64(i) * 100)
		if bal.Cmp(expected) != 0 {
			t.Errorf("shard %d: balance = %s, want %s", i, bal, expected)
		}
		if n := s.GetNonce(addr); n != uint64(i) {
			t.Errorf("shard %d: nonce = %d, want %d", i, n, i)
		}
	}
}

func TestShardedState_ConcurrentSameShard(t *testing.T) {
	s := NewShardedStateDB()
	var wg sync.WaitGroup

	// Multiple goroutines accessing the same shard.
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			addr := shardedAddr(0x5, byte(idx)) // all in shard 5
			s.SetBalance(addr, big.NewInt(int64(idx)))
			s.SetNonce(addr, uint64(idx))
		}(i)
	}
	wg.Wait()

	// Verify a sample.
	addr := shardedAddr(0x5, 50)
	if n := s.GetNonce(addr); n != 50 {
		t.Errorf("nonce = %d, want 50", n)
	}
}

func TestShardedState_Merge(t *testing.T) {
	s1 := NewShardedStateDB()
	s2 := NewShardedStateDB()

	addr1 := shardedAddr(0x1, 0x01)
	addr2 := shardedAddr(0x2, 0x02)

	s1.SetBalance(addr1, big.NewInt(100))
	s1.SetNonce(addr1, 1)

	s2.SetBalance(addr2, big.NewInt(200))
	s2.SetNonce(addr2, 2)
	key := types.BytesToHash([]byte{0x42})
	val := types.BytesToHash([]byte{0xAB})
	s2.SetState(addr2, key, val)

	// Merge s2 into s1.
	s1.Merge(s2)

	// Verify s1 has both addresses.
	if bal := s1.GetBalance(addr1); bal.Cmp(big.NewInt(100)) != 0 {
		t.Errorf("addr1 balance = %s, want 100", bal)
	}
	if bal := s1.GetBalance(addr2); bal.Cmp(big.NewInt(200)) != 0 {
		t.Errorf("addr2 balance = %s, want 200", bal)
	}
	if n := s1.GetNonce(addr2); n != 2 {
		t.Errorf("addr2 nonce = %d, want 2", n)
	}
	if got := s1.GetState(addr2, key); got != val {
		t.Errorf("addr2 storage = %s, want %s", got.Hex(), val.Hex())
	}
}

func TestShardedState_MergeParallel(t *testing.T) {
	s1 := NewShardedStateDB()
	s2 := NewShardedStateDB()

	// Populate s2 with data across multiple shards.
	for i := byte(0); i < 16; i++ {
		addr := shardedAddr(i, 0x01)
		s2.SetBalance(addr, big.NewInt(int64(i)*1000))
	}

	s1.MergeParallel(s2)

	for i := byte(0); i < 16; i++ {
		addr := shardedAddr(i, 0x01)
		expected := big.NewInt(int64(i) * 1000)
		if bal := s1.GetBalance(addr); bal.Cmp(expected) != 0 {
			t.Errorf("shard %d: balance = %s, want %s", i, bal, expected)
		}
	}
}

func TestShardedState_MergeOverwrite(t *testing.T) {
	s1 := NewShardedStateDB()
	s2 := NewShardedStateDB()

	addr := shardedAddr(0x3, 0x01)
	s1.SetBalance(addr, big.NewInt(100))
	s2.SetBalance(addr, big.NewInt(999))

	s1.Merge(s2)

	// s2's value should overwrite s1's.
	if bal := s1.GetBalance(addr); bal.Cmp(big.NewInt(999)) != 0 {
		t.Errorf("balance after merge = %s, want 999", bal)
	}
}

func TestShardedState_TxAccessRecordConflict(t *testing.T) {
	addr := shardedAddr(0x1, 0x01)
	key := types.BytesToHash([]byte{0x42})

	r1 := NewTxAccessRecord(0)
	r1.AddRead(addr, key)

	r2 := NewTxAccessRecord(1)
	r2.AddWrite(addr, key)

	if !r1.ConflictsWith(r2) {
		t.Error("expected conflict: r1 reads what r2 writes")
	}

	// No conflict if they access different keys.
	r3 := NewTxAccessRecord(2)
	key2 := types.BytesToHash([]byte{0x99})
	r3.AddWrite(addr, key2)

	if r1.ConflictsWith(r3) {
		t.Error("unexpected conflict: different keys")
	}
}

func TestShardedState_TxAccessRecordNoConflict(t *testing.T) {
	addr1 := shardedAddr(0x1, 0x01)
	addr2 := shardedAddr(0x2, 0x01)
	key := types.BytesToHash([]byte{0x42})

	r1 := NewTxAccessRecord(0)
	r1.AddRead(addr1, key)

	r2 := NewTxAccessRecord(1)
	r2.AddRead(addr2, key)

	// Two reads should not conflict.
	if r1.ConflictsWith(r2) {
		t.Error("unexpected conflict: both are reads")
	}
}

func TestShardedState_DetectShardedConflicts(t *testing.T) {
	addr := shardedAddr(0x5, 0x01)
	key := types.BytesToHash([]byte{0x10})

	a := NewTxAccessRecord(0)
	a.AddWrite(addr, key)

	b := NewTxAccessRecord(1)
	b.AddWrite(addr, key)

	if !DetectShardedConflicts(a, b) {
		t.Error("expected write-write conflict")
	}
}

func TestShardedState_AddressCount(t *testing.T) {
	s := NewShardedStateDB()

	if c := s.AddressCount(); c != 0 {
		t.Errorf("AddressCount() = %d, want 0", c)
	}

	s.SetBalance(shardedAddr(0x0, 0x01), big.NewInt(1))
	s.SetBalance(shardedAddr(0xF, 0x02), big.NewInt(2))
	s.SetNonce(shardedAddr(0x0, 0x01), 1) // same address, should not double-count

	if c := s.AddressCount(); c != 2 {
		t.Errorf("AddressCount() = %d, want 2", c)
	}
}
