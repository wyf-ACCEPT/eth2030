package state

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestHistoricalAccessor_SetGetAccount(t *testing.T) {
	ha := NewHistoricalAccessor(100)

	addr := types.HexToAddress("0x01")
	balance := big.NewInt(1000)
	code := []byte{0x60, 0x00, 0xfd}

	ha.SetAccount(addr, balance, 5, code)

	if ha.GetBalance(addr).Cmp(balance) != 0 {
		t.Fatalf("balance: want %s, got %s", balance, ha.GetBalance(addr))
	}
	if ha.GetNonce(addr) != 5 {
		t.Fatalf("nonce: want 5, got %d", ha.GetNonce(addr))
	}
	if len(ha.GetCode(addr)) != 3 {
		t.Fatalf("code length: want 3, got %d", len(ha.GetCode(addr)))
	}
	if ha.GetCodeHash(addr) == (types.Hash{}) {
		t.Fatal("code hash should not be zero for account with code")
	}
}

func TestHistoricalAccessor_GetNonExistent(t *testing.T) {
	ha := NewHistoricalAccessor(100)
	addr := types.HexToAddress("0xff")

	if ha.GetBalance(addr).Sign() != 0 {
		t.Fatal("non-existent account should have zero balance")
	}
	if ha.GetNonce(addr) != 0 {
		t.Fatal("non-existent account should have zero nonce")
	}
	if ha.GetCode(addr) != nil {
		t.Fatal("non-existent account should have nil code")
	}
	if ha.GetCodeHash(addr) != (types.Hash{}) {
		t.Fatal("non-existent account should have zero code hash")
	}
}

func TestHistoricalAccessor_Storage(t *testing.T) {
	ha := NewHistoricalAccessor(200)
	addr := types.HexToAddress("0x01")
	ha.SetAccount(addr, big.NewInt(100), 1, nil)

	key := types.HexToHash("0x01")
	value := types.HexToHash("0xdeadbeef")

	ha.SetStorage(addr, key, value)

	got := ha.GetStorageAt(addr, key)
	if got != value {
		t.Fatalf("storage: want %s, got %s", value.Hex(), got.Hex())
	}

	// Non-existent key returns zero.
	missing := ha.GetStorageAt(addr, types.HexToHash("0x99"))
	if missing != (types.Hash{}) {
		t.Fatal("missing storage key should return zero hash")
	}
}

func TestHistoricalAccessor_StorageCreatesAccount(t *testing.T) {
	ha := NewHistoricalAccessor(200)
	addr := types.HexToAddress("0x02")

	// SetStorage on non-existent account should create it.
	key := types.HexToHash("0x01")
	value := types.HexToHash("0x42")

	ha.SetStorage(addr, key, value)

	if !ha.Exist(addr) {
		t.Fatal("SetStorage should create the account")
	}
	if ha.GetStorageAt(addr, key) != value {
		t.Fatal("storage value mismatch after implicit create")
	}
}

func TestHistoricalAccessor_Exist(t *testing.T) {
	ha := NewHistoricalAccessor(100)
	addr := types.HexToAddress("0x01")

	if ha.Exist(addr) {
		t.Fatal("account should not exist before creation")
	}

	ha.SetAccount(addr, big.NewInt(0), 0, nil)

	if !ha.Exist(addr) {
		t.Fatal("account should exist after creation")
	}
}

func TestHistoricalAccessor_BlockNumber(t *testing.T) {
	ha := NewHistoricalAccessor(42)
	if ha.BlockNumber() != 42 {
		t.Fatalf("block number: want 42, got %d", ha.BlockNumber())
	}
}

func TestHistoricalAccessor_AccountCount(t *testing.T) {
	ha := NewHistoricalAccessor(100)
	if ha.AccountCount() != 0 {
		t.Fatal("empty accessor should have 0 accounts")
	}

	ha.SetAccount(types.HexToAddress("0x01"), big.NewInt(100), 0, nil)
	ha.SetAccount(types.HexToAddress("0x02"), big.NewInt(200), 0, nil)
	ha.SetAccount(types.HexToAddress("0x03"), big.NewInt(300), 0, nil)

	if ha.AccountCount() != 3 {
		t.Fatalf("account count: want 3, got %d", ha.AccountCount())
	}
}

func TestHistoricalAccessor_StorageCount(t *testing.T) {
	ha := NewHistoricalAccessor(100)
	addr := types.HexToAddress("0x01")

	// Non-existent account.
	if ha.StorageCount(addr) != 0 {
		t.Fatal("non-existent account should have 0 storage")
	}

	ha.SetAccount(addr, big.NewInt(100), 0, nil)
	ha.SetStorage(addr, types.HexToHash("0x01"), types.HexToHash("0xaa"))
	ha.SetStorage(addr, types.HexToHash("0x02"), types.HexToHash("0xbb"))

	if ha.StorageCount(addr) != 2 {
		t.Fatalf("storage count: want 2, got %d", ha.StorageCount(addr))
	}
}

func TestHistoricalAccessor_CodeHash_NoCode(t *testing.T) {
	ha := NewHistoricalAccessor(100)
	addr := types.HexToAddress("0x01")

	ha.SetAccount(addr, big.NewInt(100), 0, nil)

	codeHash := ha.GetCodeHash(addr)
	if codeHash != types.EmptyCodeHash {
		t.Fatalf("account with no code should return EmptyCodeHash, got %s", codeHash.Hex())
	}
}

func TestHistoricalAccessor_NilBalance(t *testing.T) {
	ha := NewHistoricalAccessor(100)
	addr := types.HexToAddress("0x01")

	// Setting nil balance should default to zero.
	ha.SetAccount(addr, nil, 0, nil)

	bal := ha.GetBalance(addr)
	if bal.Sign() != 0 {
		t.Fatalf("nil balance should be treated as zero, got %s", bal)
	}
}

func TestHistoricalAccessor_BalanceDeepCopy(t *testing.T) {
	ha := NewHistoricalAccessor(100)
	addr := types.HexToAddress("0x01")
	balance := big.NewInt(1000)

	ha.SetAccount(addr, balance, 0, nil)

	// Mutate the original: accessor should be unaffected.
	balance.SetInt64(9999)

	got := ha.GetBalance(addr)
	if got.Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("balance should be deep-copied, got %s", got)
	}
}

func TestHistoricalAccessor_UpdateAccount(t *testing.T) {
	ha := NewHistoricalAccessor(100)
	addr := types.HexToAddress("0x01")

	ha.SetAccount(addr, big.NewInt(100), 1, []byte{0x01})
	ha.SetAccount(addr, big.NewInt(200), 2, []byte{0x02})

	if ha.GetBalance(addr).Cmp(big.NewInt(200)) != 0 {
		t.Fatal("balance should be updated")
	}
	if ha.GetNonce(addr) != 2 {
		t.Fatal("nonce should be updated")
	}
}

// --- StateDiff tests ---

func TestStateDiff_AddBalanceChange(t *testing.T) {
	diff := NewStateDiff()
	addr := types.HexToAddress("0x01")

	diff.AddBalanceChange(addr, big.NewInt(100), big.NewInt(200))

	changes := diff.Changes()
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Field != "balance" {
		t.Fatalf("field: want balance, got %s", changes[0].Field)
	}
	before := changes[0].Before.(*big.Int)
	after := changes[0].After.(*big.Int)
	if before.Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("before: want 100, got %s", before)
	}
	if after.Cmp(big.NewInt(200)) != 0 {
		t.Fatalf("after: want 200, got %s", after)
	}
}

func TestStateDiff_AddNonceChange(t *testing.T) {
	diff := NewStateDiff()
	addr := types.HexToAddress("0x01")

	diff.AddNonceChange(addr, 1, 2)

	changes := diff.Changes()
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Field != "nonce" {
		t.Fatalf("field: want nonce, got %s", changes[0].Field)
	}
	if changes[0].Before.(uint64) != 1 {
		t.Fatal("before nonce mismatch")
	}
	if changes[0].After.(uint64) != 2 {
		t.Fatal("after nonce mismatch")
	}
}

func TestStateDiff_AddStorageChange(t *testing.T) {
	diff := NewStateDiff()
	addr := types.HexToAddress("0x01")
	key := types.HexToHash("0x10")

	diff.AddStorageChange(addr, key, types.Hash{}, types.HexToHash("0xaa"))

	changes := diff.Changes()
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Before.(types.Hash) != (types.Hash{}) {
		t.Fatal("before should be zero hash")
	}
	if changes[0].After.(types.Hash) != types.HexToHash("0xaa") {
		t.Fatal("after should be 0xaa hash")
	}
}

func TestStateDiff_Apply(t *testing.T) {
	ha := NewHistoricalAccessor(100)
	addr := types.HexToAddress("0x01")
	ha.SetAccount(addr, big.NewInt(100), 1, nil)

	diff := NewStateDiff()
	diff.AddBalanceChange(addr, big.NewInt(100), big.NewInt(500))
	diff.AddNonceChange(addr, 1, 5)

	key := types.HexToHash("0x10")
	diff.AddStorageChange(addr, key, types.Hash{}, types.HexToHash("0xbeef"))

	diff.Apply(ha)

	if ha.GetBalance(addr).Cmp(big.NewInt(500)) != 0 {
		t.Fatalf("applied balance: want 500, got %s", ha.GetBalance(addr))
	}
	if ha.GetNonce(addr) != 5 {
		t.Fatalf("applied nonce: want 5, got %d", ha.GetNonce(addr))
	}
	if ha.GetStorageAt(addr, key) != types.HexToHash("0xbeef") {
		t.Fatal("applied storage mismatch")
	}
}

func TestStateDiff_ApplyCreatesAccount(t *testing.T) {
	ha := NewHistoricalAccessor(100)
	addr := types.HexToAddress("0x99")

	diff := NewStateDiff()
	diff.AddBalanceChange(addr, big.NewInt(0), big.NewInt(1000))

	diff.Apply(ha)

	if !ha.Exist(addr) {
		t.Fatal("Apply should create account if it doesn't exist")
	}
	if ha.GetBalance(addr).Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("applied balance: want 1000, got %s", ha.GetBalance(addr))
	}
}

func TestStateDiff_EmptyDiff(t *testing.T) {
	diff := NewStateDiff()
	changes := diff.Changes()
	if len(changes) != 0 {
		t.Fatalf("empty diff should have 0 changes, got %d", len(changes))
	}

	// Apply empty diff should not panic.
	ha := NewHistoricalAccessor(100)
	diff.Apply(ha)
	if ha.AccountCount() != 0 {
		t.Fatal("applying empty diff should not create accounts")
	}
}

func TestStateDiff_MultipleChanges(t *testing.T) {
	diff := NewStateDiff()
	addr1 := types.HexToAddress("0x01")
	addr2 := types.HexToAddress("0x02")

	diff.AddBalanceChange(addr1, big.NewInt(0), big.NewInt(100))
	diff.AddNonceChange(addr1, 0, 1)
	diff.AddBalanceChange(addr2, big.NewInt(0), big.NewInt(200))

	changes := diff.Changes()
	if len(changes) != 3 {
		t.Fatalf("expected 3 changes, got %d", len(changes))
	}

	// Changes should be sorted by address, then field.
	for i := 1; i < len(changes); i++ {
		if changes[i].Address.Hex() < changes[i-1].Address.Hex() {
			t.Fatal("changes not sorted by address")
		}
		if changes[i].Address == changes[i-1].Address && changes[i].Field < changes[i-1].Field {
			t.Fatal("changes for same address not sorted by field")
		}
	}
}

func TestHistoricalAccessor_StorageOnNonExistentAddress(t *testing.T) {
	ha := NewHistoricalAccessor(100)
	addr := types.HexToAddress("0xfe")

	got := ha.GetStorageAt(addr, types.HexToHash("0x01"))
	if got != (types.Hash{}) {
		t.Fatal("storage on non-existent account should be zero")
	}
}

func TestStateAccessor_Interface(t *testing.T) {
	// Verify that HistoricalAccessor satisfies the interface.
	var accessor StateAccessor = NewHistoricalAccessor(100)
	addr := types.HexToAddress("0x01")

	// All methods should work without panicking.
	_ = accessor.GetBalance(addr)
	_ = accessor.GetNonce(addr)
	_ = accessor.GetCode(addr)
	_ = accessor.GetCodeHash(addr)
	_ = accessor.GetStorageAt(addr, types.Hash{})
	_ = accessor.Exist(addr)
}
