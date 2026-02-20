package verkle

import (
	"math/big"
	"testing"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// overlayTestSource creates and populates a mockStateSource for overlay tests.
func overlayTestSource() *mockStateSource {
	return newMockStateSource()
}

func TestMPTToVerkleConverterConvertAccount(t *testing.T) {
	source := overlayTestSource()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	source.accounts[addr] = &mockAccount{
		balance:  big.NewInt(1000),
		nonce:    5,
		codeHash: types.HexToHash("0xabcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"),
	}

	tree := NewInMemoryVerkleTree()
	dest := NewVerkleStateDB(tree)
	converter := NewMPTToVerkleConverter(source, dest)

	err := converter.ConvertAccount(addr)
	if err != nil {
		t.Fatalf("ConvertAccount failed: %v", err)
	}

	acct := dest.GetAccount(addr)
	if acct == nil {
		t.Fatal("account not found in Verkle tree after conversion")
	}
	if acct.Nonce != 5 {
		t.Fatalf("nonce: got %d, want 5", acct.Nonce)
	}
	if acct.Balance.Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("balance: got %s, want 1000", acct.Balance.String())
	}
}

func TestMPTToVerkleConverterConvertAccountNotFound(t *testing.T) {
	source := overlayTestSource()
	addr := types.HexToAddress("0x2222222222222222222222222222222222222222")

	tree := NewInMemoryVerkleTree()
	dest := NewVerkleStateDB(tree)
	converter := NewMPTToVerkleConverter(source, dest)

	err := converter.ConvertAccount(addr)
	if err != ErrAccountNotFound {
		t.Fatalf("expected ErrAccountNotFound, got %v", err)
	}
}

func TestMPTToVerkleConverterConvertStorageSlot(t *testing.T) {
	source := overlayTestSource()
	tree := NewInMemoryVerkleTree()
	dest := NewVerkleStateDB(tree)
	converter := NewMPTToVerkleConverter(source, dest)

	addr := types.HexToAddress("0x3333333333333333333333333333333333333333")
	key := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")
	val := types.HexToHash("0x00000000000000000000000000000000000000000000000000000000000000ff")

	converter.ConvertStorageSlot(addr, key, val)

	p := converter.Progress()
	if p.StorageSlotsConverted != 1 {
		t.Fatalf("storage slots converted: got %d, want 1", p.StorageSlotsConverted)
	}
}

func TestMPTToVerkleConverterConvertCodeChunks(t *testing.T) {
	source := overlayTestSource()
	tree := NewInMemoryVerkleTree()
	dest := NewVerkleStateDB(tree)
	converter := NewMPTToVerkleConverter(source, dest)

	addr := types.HexToAddress("0x4444444444444444444444444444444444444444")
	code := make([]byte, 93) // 3 chunks of 31 bytes
	for i := range code {
		code[i] = byte(i % 256)
	}

	numChunks := converter.ConvertCodeChunks(addr, code)
	if numChunks != 3 {
		t.Fatalf("code chunks: got %d, want 3", numChunks)
	}

	p := converter.Progress()
	if p.CodeChunksConverted != 3 {
		t.Fatalf("code chunks converted: got %d, want 3", p.CodeChunksConverted)
	}
}

func TestMPTToVerkleConverterEmptyCode(t *testing.T) {
	source := overlayTestSource()
	tree := NewInMemoryVerkleTree()
	dest := NewVerkleStateDB(tree)
	converter := NewMPTToVerkleConverter(source, dest)

	addr := types.HexToAddress("0x5555555555555555555555555555555555555555")
	numChunks := converter.ConvertCodeChunks(addr, nil)
	if numChunks != 0 {
		t.Fatalf("empty code chunks: got %d, want 0", numChunks)
	}
}

func TestMPTToVerkleConverterProgress(t *testing.T) {
	source := overlayTestSource()
	addr1 := types.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := types.HexToAddress("0x2222222222222222222222222222222222222222")
	source.accounts[addr1] = &mockAccount{balance: big.NewInt(100), nonce: 1}
	source.accounts[addr2] = &mockAccount{balance: big.NewInt(200), nonce: 2}

	tree := NewInMemoryVerkleTree()
	dest := NewVerkleStateDB(tree)
	converter := NewMPTToVerkleConverter(source, dest)

	converter.ConvertAccount(addr1)
	converter.ConvertAccount(addr2)

	p := converter.Progress()
	if p.AccountsConverted != 2 {
		t.Fatalf("accounts converted: got %d, want 2", p.AccountsConverted)
	}
	if p.Complete {
		t.Fatal("should not be marked complete yet")
	}

	converter.MarkComplete()
	p = converter.Progress()
	if !p.Complete {
		t.Fatal("should be marked complete")
	}
}

func TestEpochBasedMigrationScheduleAndProcess(t *testing.T) {
	source := overlayTestSource()
	addr1 := types.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := types.HexToAddress("0x2222222222222222222222222222222222222222")
	addr3 := types.HexToAddress("0x3333333333333333333333333333333333333333")
	source.accounts[addr1] = &mockAccount{balance: big.NewInt(100), nonce: 1}
	source.accounts[addr2] = &mockAccount{balance: big.NewInt(200), nonce: 2}
	source.accounts[addr3] = &mockAccount{balance: big.NewInt(300), nonce: 3}

	tree := NewInMemoryVerkleTree()
	dest := NewVerkleStateDB(tree)
	converter := NewMPTToVerkleConverter(source, dest)
	epoch := NewEpochBasedMigration(converter, 10)

	epoch.ScheduleAccounts(0, []types.Address{addr1, addr2})
	epoch.ScheduleAccounts(1, []types.Address{addr3})

	if epoch.TotalEpochs() != 2 {
		t.Fatalf("total epochs: got %d, want 2", epoch.TotalEpochs())
	}

	n, err := epoch.ProcessEpoch(0)
	if err != nil {
		t.Fatalf("ProcessEpoch(0) failed: %v", err)
	}
	if n != 2 {
		t.Fatalf("epoch 0 migrated: got %d, want 2", n)
	}

	n, err = epoch.ProcessEpoch(1)
	if err != nil {
		t.Fatalf("ProcessEpoch(1) failed: %v", err)
	}
	if n != 1 {
		t.Fatalf("epoch 1 migrated: got %d, want 1", n)
	}

	if epoch.CurrentEpoch() != 1 {
		t.Fatalf("current epoch: got %d, want 1", epoch.CurrentEpoch())
	}
}

func TestEpochBasedMigrationNonExistentAccounts(t *testing.T) {
	source := overlayTestSource()
	tree := NewInMemoryVerkleTree()
	dest := NewVerkleStateDB(tree)
	converter := NewMPTToVerkleConverter(source, dest)
	epoch := NewEpochBasedMigration(converter, 10)

	addr := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	epoch.ScheduleAccounts(0, []types.Address{addr})

	n, err := epoch.ProcessEpoch(0)
	if err != nil {
		t.Fatalf("ProcessEpoch should not error for missing accounts: %v", err)
	}
	if n != 0 {
		t.Fatalf("should have migrated 0 accounts, got %d", n)
	}
}

func TestOverlayDBReadsMigratedFromVerkle(t *testing.T) {
	source := overlayTestSource()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	source.accounts[addr] = &mockAccount{balance: big.NewInt(500), nonce: 10}

	tree := NewInMemoryVerkleTree()
	vdb := NewVerkleStateDB(tree)

	// Write different values to Verkle.
	vdb.SetAccount(addr, &AccountState{
		Nonce:   42,
		Balance: big.NewInt(9999),
	})

	overlay := NewOverlayDB(vdb, source)
	overlay.MarkMigrated(addr)

	// Should read from Verkle, not MPT.
	bal := overlay.GetBalance(addr)
	if bal.Cmp(big.NewInt(9999)) != 0 {
		t.Fatalf("overlay balance: got %s, want 9999", bal.String())
	}

	nonce := overlay.GetNonce(addr)
	if nonce != 42 {
		t.Fatalf("overlay nonce: got %d, want 42", nonce)
	}
}

func TestOverlayDBFallsBackToMPT(t *testing.T) {
	source := overlayTestSource()
	addr := types.HexToAddress("0x2222222222222222222222222222222222222222")
	source.accounts[addr] = &mockAccount{balance: big.NewInt(777), nonce: 3}

	tree := NewInMemoryVerkleTree()
	vdb := NewVerkleStateDB(tree)
	overlay := NewOverlayDB(vdb, source)

	// Not migrated: should fall back to MPT.
	bal := overlay.GetBalance(addr)
	if bal.Cmp(big.NewInt(777)) != 0 {
		t.Fatalf("overlay fallback balance: got %s, want 777", bal.String())
	}

	nonce := overlay.GetNonce(addr)
	if nonce != 3 {
		t.Fatalf("overlay fallback nonce: got %d, want 3", nonce)
	}
}

func TestOverlayDBExist(t *testing.T) {
	source := overlayTestSource()
	addr := types.HexToAddress("0x3333333333333333333333333333333333333333")
	source.accounts[addr] = &mockAccount{balance: big.NewInt(1), nonce: 0}

	tree := NewInMemoryVerkleTree()
	vdb := NewVerkleStateDB(tree)
	overlay := NewOverlayDB(vdb, source)

	// Not migrated: should check MPT.
	if !overlay.Exist(addr) {
		t.Fatal("account should exist in MPT")
	}

	// Non-existent address.
	noAddr := types.HexToAddress("0xffffffffffffffffffffffffffffffffffffffff")
	if overlay.Exist(noAddr) {
		t.Fatal("non-existent address should not exist")
	}
}

func TestOverlayDBMigratedCount(t *testing.T) {
	source := overlayTestSource()
	tree := NewInMemoryVerkleTree()
	vdb := NewVerkleStateDB(tree)
	overlay := NewOverlayDB(vdb, source)

	if overlay.MigratedCount() != 0 {
		t.Fatalf("initial migrated count: got %d, want 0", overlay.MigratedCount())
	}

	overlay.MarkMigrated(types.HexToAddress("0x0000000000000000000000000000000000000001"))
	overlay.MarkMigrated(types.HexToAddress("0x0000000000000000000000000000000000000002"))

	if overlay.MigratedCount() != 2 {
		t.Fatalf("migrated count: got %d, want 2", overlay.MigratedCount())
	}
}

func TestDeadlineTrackerNotExpired(t *testing.T) {
	deadline := time.Now().Add(1 * time.Hour)
	dt := NewDeadlineTracker(deadline, 1000, 0)

	if dt.IsExpired() {
		t.Fatal("should not be expired before deadline")
	}

	rem := dt.BlocksRemaining()
	if rem != 1000 {
		t.Fatalf("blocks remaining: got %d, want 1000", rem)
	}
}

func TestDeadlineTrackerExpired(t *testing.T) {
	deadline := time.Now().Add(-1 * time.Second)
	dt := NewDeadlineTracker(deadline, 1000, 0)

	if !dt.IsExpired() {
		t.Fatal("should be expired after deadline")
	}

	tr := dt.TimeRemaining()
	if tr != 0 {
		t.Fatalf("time remaining should be 0 when expired, got %v", tr)
	}
}

func TestDeadlineTrackerProgress(t *testing.T) {
	deadline := time.Now().Add(1 * time.Hour)
	dt := NewDeadlineTracker(deadline, 1000, 0)

	dt.UpdateProgress(500)

	pct := dt.ProgressPercent()
	if pct != 50.0 {
		t.Fatalf("progress percent: got %f, want 50.0", pct)
	}

	rem := dt.BlocksRemaining()
	if rem != 500 {
		t.Fatalf("blocks remaining: got %d, want 500", rem)
	}
}

func TestDeadlineTrackerProgressComplete(t *testing.T) {
	deadline := time.Now().Add(1 * time.Hour)
	dt := NewDeadlineTracker(deadline, 1000, 0)

	dt.UpdateProgress(1000)

	pct := dt.ProgressPercent()
	if pct != 100.0 {
		t.Fatalf("progress percent: got %f, want 100.0", pct)
	}

	rem := dt.BlocksRemaining()
	if rem != 0 {
		t.Fatalf("blocks remaining at fork: got %d, want 0", rem)
	}
}

func TestDeadlineTrackerZeroRange(t *testing.T) {
	deadline := time.Now().Add(1 * time.Hour)
	dt := NewDeadlineTracker(deadline, 100, 100)

	pct := dt.ProgressPercent()
	if pct != 100.0 {
		t.Fatalf("zero range progress: got %f, want 100.0", pct)
	}
}
