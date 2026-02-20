package witness

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestStateWitnessBuilderRecordAccount(t *testing.T) {
	root := types.HexToHash("0xaa")
	b := NewStateWitnessBuilder(100, root)

	addr := types.HexToAddress("0x01")
	balance := big.NewInt(1000)
	codeHash := types.HexToHash("0xcc")

	err := b.RecordAccount(addr, true, 5, balance, codeHash)
	if err != nil {
		t.Fatalf("RecordAccount: %v", err)
	}

	if b.AccountCount() != 1 {
		t.Fatalf("expected 1 account, got %d", b.AccountCount())
	}

	// Second call for same address is a no-op on values.
	err = b.RecordAccount(addr, false, 99, big.NewInt(9999), types.Hash{})
	if err != nil {
		t.Fatalf("RecordAccount second: %v", err)
	}
	if b.AccountCount() != 1 {
		t.Fatalf("expected 1 account after dup, got %d", b.AccountCount())
	}
}

func TestStateWitnessBuilderRecordStorage(t *testing.T) {
	b := NewStateWitnessBuilder(1, types.Hash{0x01})

	addr := types.HexToAddress("0x02")
	key1 := types.HexToHash("0x10")
	val1 := types.HexToHash("0x20")
	key2 := types.HexToHash("0x30")
	val2 := types.HexToHash("0x40")

	b.RecordStorage(addr, key1, val1)
	b.RecordStorage(addr, key2, val2)
	// Duplicate: should not increase count.
	b.RecordStorage(addr, key1, types.HexToHash("0xff"))

	if b.SlotCount() != 2 {
		t.Fatalf("expected 2 slots, got %d", b.SlotCount())
	}
}

func TestStateWitnessBuilderRecordCode(t *testing.T) {
	b := NewStateWitnessBuilder(1, types.Hash{0x01})

	codeHash := types.HexToHash("0xab")
	code := []byte{0x60, 0x00, 0x60, 0x00, 0xf3}

	b.RecordCode(codeHash, code)
	// Duplicate: no-op.
	b.RecordCode(codeHash, []byte{0xff})

	if b.CodeCount() != 1 {
		t.Fatalf("expected 1 code entry, got %d", b.CodeCount())
	}

	// Empty code hash: should be ignored.
	b.RecordCode(types.EmptyCodeHash, []byte{0x01})
	if b.CodeCount() != 1 {
		t.Fatalf("expected 1 code entry after empty hash, got %d", b.CodeCount())
	}

	// Zero hash: should be ignored.
	b.RecordCode(types.Hash{}, []byte{0x01})
	if b.CodeCount() != 1 {
		t.Fatalf("expected 1 code entry after zero hash, got %d", b.CodeCount())
	}
}

func TestStateWitnessBuilderFinalize(t *testing.T) {
	root := types.HexToHash("0xbb")
	b := NewStateWitnessBuilder(42, root)

	addr := types.HexToAddress("0x01")
	b.RecordAccount(addr, true, 10, big.NewInt(500), types.HexToHash("0xcc"))
	b.RecordStorage(addr, types.HexToHash("0x11"), types.HexToHash("0x22"))

	codeHash := types.HexToHash("0xcc")
	b.RecordCode(codeHash, []byte{0x60, 0x00})

	sw, err := b.Finalize()
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if sw.BlockNumber != 42 {
		t.Fatalf("expected block 42, got %d", sw.BlockNumber)
	}
	if sw.StateRoot != root {
		t.Fatalf("state root mismatch")
	}
	if len(sw.Accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(sw.Accounts))
	}
	acc := sw.Accounts[addr]
	if acc == nil {
		t.Fatal("account not found in witness")
	}
	if acc.Nonce != 10 {
		t.Fatalf("expected nonce 10, got %d", acc.Nonce)
	}
	if acc.Balance.Cmp(big.NewInt(500)) != 0 {
		t.Fatalf("expected balance 500, got %s", acc.Balance)
	}
	if acc.CodeHash != codeHash {
		t.Fatalf("code hash mismatch")
	}
	if !acc.Exists {
		t.Fatal("expected account to exist")
	}
	if len(acc.Storage) != 1 {
		t.Fatalf("expected 1 storage slot, got %d", len(acc.Storage))
	}

	if len(sw.Codes) != 1 {
		t.Fatalf("expected 1 code entry, got %d", len(sw.Codes))
	}
	if sw.AccessedSlots != 1 {
		t.Fatalf("expected 1 accessed slot, got %d", sw.AccessedSlots)
	}
	if sw.WitnessHash.IsZero() {
		t.Fatal("witness hash should not be zero")
	}
}

func TestStateWitnessBuilderFinalizeEmpty(t *testing.T) {
	b := NewStateWitnessBuilder(1, types.Hash{0x01})
	_, err := b.Finalize()
	if err != ErrStateWitnessEmpty {
		t.Fatalf("expected ErrStateWitnessEmpty, got %v", err)
	}
}

func TestStateWitnessBuilderFinalizedTwice(t *testing.T) {
	b := NewStateWitnessBuilder(1, types.Hash{0x01})
	addr := types.HexToAddress("0x01")
	b.RecordAccount(addr, true, 0, big.NewInt(0), types.Hash{})
	b.Finalize()

	_, err := b.Finalize()
	if err != ErrStateWitnessFinalized {
		t.Fatalf("expected ErrStateWitnessFinalized, got %v", err)
	}
}

func TestStateWitnessBuilderRecordAfterFinalize(t *testing.T) {
	b := NewStateWitnessBuilder(1, types.Hash{0x01})
	addr := types.HexToAddress("0x01")
	b.RecordAccount(addr, true, 0, big.NewInt(0), types.Hash{})
	b.Finalize()

	err := b.RecordAccount(types.HexToAddress("0x02"), true, 0, big.NewInt(0), types.Hash{})
	if err != ErrStateWitnessFinalized {
		t.Fatalf("expected ErrStateWitnessFinalized for RecordAccount, got %v", err)
	}
	err = b.RecordStorage(addr, types.Hash{}, types.Hash{})
	if err != ErrStateWitnessFinalized {
		t.Fatalf("expected ErrStateWitnessFinalized for RecordStorage, got %v", err)
	}
	err = b.RecordCode(types.HexToHash("0xab"), []byte{0x01})
	if err != ErrStateWitnessFinalized {
		t.Fatalf("expected ErrStateWitnessFinalized for RecordCode, got %v", err)
	}
}

func TestStateWitnessBuilderIsFinalized(t *testing.T) {
	b := NewStateWitnessBuilder(1, types.Hash{0x01})
	if b.IsFinalized() {
		t.Fatal("should not be finalized initially")
	}
	b.RecordAccount(types.HexToAddress("0x01"), true, 0, big.NewInt(0), types.Hash{})
	b.Finalize()
	if !b.IsFinalized() {
		t.Fatal("should be finalized after Finalize()")
	}
}

func TestVerifyStateWitnessHash(t *testing.T) {
	b := NewStateWitnessBuilder(50, types.HexToHash("0xdd"))
	addr1 := types.HexToAddress("0x01")
	addr2 := types.HexToAddress("0x02")

	b.RecordAccount(addr1, true, 1, big.NewInt(100), types.HexToHash("0xee"))
	b.RecordAccount(addr2, false, 0, big.NewInt(0), types.Hash{})
	b.RecordStorage(addr1, types.HexToHash("0x11"), types.HexToHash("0x22"))

	sw, err := b.Finalize()
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if !VerifyStateWitnessHash(sw) {
		t.Fatal("VerifyStateWitnessHash returned false for valid witness")
	}

	// Tamper with witness.
	sw.Accounts[addr1].Nonce = 999
	if VerifyStateWitnessHash(sw) {
		t.Fatal("VerifyStateWitnessHash returned true for tampered witness")
	}
}

func TestVerifyStateWitnessHashNil(t *testing.T) {
	if VerifyStateWitnessHash(nil) {
		t.Fatal("expected false for nil witness")
	}
}

func TestStateWitnessSize(t *testing.T) {
	b := NewStateWitnessBuilder(1, types.Hash{0x01})
	addr := types.HexToAddress("0x01")
	b.RecordAccount(addr, true, 1, big.NewInt(100), types.HexToHash("0xcc"))
	b.RecordStorage(addr, types.HexToHash("0x11"), types.HexToHash("0x22"))
	b.RecordCode(types.HexToHash("0xcc"), []byte{0x60, 0x00, 0x60, 0x00, 0xf3})

	sw, _ := b.Finalize()
	size := StateWitnessSize(sw)
	if size <= 0 {
		t.Fatalf("expected positive size, got %d", size)
	}

	if StateWitnessSize(nil) != 0 {
		t.Fatal("expected 0 for nil witness")
	}
}

func TestStateWitnessBuilderEstimateSize(t *testing.T) {
	b := NewStateWitnessBuilder(1, types.Hash{0x01})
	initial := b.EstimateSize()
	if initial < 40 { // at least base overhead
		t.Fatalf("expected at least 40 bytes, got %d", initial)
	}

	addr := types.HexToAddress("0x01")
	b.RecordAccount(addr, true, 0, big.NewInt(0), types.Hash{})
	after := b.EstimateSize()
	if after <= initial {
		t.Fatalf("expected size to increase after adding account")
	}
}

func TestStateWitnessBuilderAccessLog(t *testing.T) {
	b := NewStateWitnessBuilder(1, types.Hash{0x01})
	addr := types.HexToAddress("0x01")

	b.RecordAccount(addr, true, 0, big.NewInt(0), types.Hash{})
	b.RecordStorage(addr, types.HexToHash("0x10"), types.HexToHash("0x20"))
	b.RecordStorage(addr, types.HexToHash("0x10"), types.HexToHash("0x20")) // dup

	if b.AccessLogLen() != 3 { // 1 account + 2 storage (dup still logged)
		t.Fatalf("expected 3 access log entries, got %d", b.AccessLogLen())
	}
}

func TestStateWitnessBuilderMultipleAccounts(t *testing.T) {
	b := NewStateWitnessBuilder(10, types.HexToHash("0xaa"))

	for i := 0; i < 5; i++ {
		addr := types.BytesToAddress([]byte{byte(i + 1)})
		b.RecordAccount(addr, true, uint64(i), big.NewInt(int64(i*100)), types.Hash{})
		b.RecordStorage(addr, types.HexToHash("0x01"), types.HexToHash("0x02"))
	}

	if b.AccountCount() != 5 {
		t.Fatalf("expected 5 accounts, got %d", b.AccountCount())
	}
	if b.SlotCount() != 5 {
		t.Fatalf("expected 5 slots, got %d", b.SlotCount())
	}

	sw, err := b.Finalize()
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if len(sw.Accounts) != 5 {
		t.Fatalf("expected 5 accounts in witness, got %d", len(sw.Accounts))
	}
	if sw.AccessedSlots != 5 {
		t.Fatalf("expected 5 accessed slots, got %d", sw.AccessedSlots)
	}
	if !VerifyStateWitnessHash(sw) {
		t.Fatal("witness hash verification failed")
	}
}

func TestStateWitnessBuilderDeepCopy(t *testing.T) {
	// Verify that the finalized witness is a deep copy.
	b := NewStateWitnessBuilder(1, types.Hash{0x01})
	addr := types.HexToAddress("0x01")
	balance := big.NewInt(500)
	b.RecordAccount(addr, true, 0, balance, types.Hash{})

	sw, _ := b.Finalize()

	// Mutate the original balance.
	balance.SetInt64(9999)
	if sw.Accounts[addr].Balance.Cmp(big.NewInt(500)) != 0 {
		t.Fatal("witness balance should not change after external mutation")
	}
}

func TestStateWitnessHashDeterministic(t *testing.T) {
	// Build the same witness twice; hashes should match.
	build := func() *StateWitness {
		b := NewStateWitnessBuilder(42, types.HexToHash("0xff"))
		b.RecordAccount(types.HexToAddress("0x01"), true, 1, big.NewInt(100), types.HexToHash("0xcc"))
		b.RecordAccount(types.HexToAddress("0x02"), true, 2, big.NewInt(200), types.HexToHash("0xdd"))
		b.RecordStorage(types.HexToAddress("0x01"), types.HexToHash("0xa1"), types.HexToHash("0xb1"))
		b.RecordStorage(types.HexToAddress("0x02"), types.HexToHash("0xa2"), types.HexToHash("0xb2"))
		sw, _ := b.Finalize()
		return sw
	}

	sw1 := build()
	sw2 := build()
	if sw1.WitnessHash != sw2.WitnessHash {
		t.Fatalf("witness hashes should be deterministic:\n  %x\n  %x",
			sw1.WitnessHash, sw2.WitnessHash)
	}
}

func TestStateWitnessBuilderNilBalance(t *testing.T) {
	b := NewStateWitnessBuilder(1, types.Hash{0x01})
	addr := types.HexToAddress("0x01")
	// nil balance should not panic.
	b.RecordAccount(addr, true, 0, nil, types.Hash{})
	sw, err := b.Finalize()
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if sw.Accounts[addr].Balance == nil {
		t.Fatal("balance should be non-nil (zero) after finalize")
	}
}
