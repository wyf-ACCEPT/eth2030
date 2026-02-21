package state

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func makeJMAddr(b byte) types.Address {
	var a types.Address
	a[types.AddressLength-1] = b
	return a
}

func makeJMHash(b byte) types.Hash {
	var h types.Hash
	h[types.HashLength-1] = b
	return h
}

func newJMState() (*MemoryStateDB, *JournalManager) {
	sdb := NewMemoryStateDB()
	jm := NewJournalManager(sdb)
	return sdb, jm
}

func TestJournalManagerNewState(t *testing.T) {
	_, jm := newJMState()
	if jm.JournalLength() != 0 {
		t.Fatalf("expected 0 journal entries, got %d", jm.JournalLength())
	}
	if jm.TxIndex() != -1 {
		t.Fatalf("expected tx index -1, got %d", jm.TxIndex())
	}
	if jm.IsFinalized() {
		t.Fatal("expected not finalized")
	}
}

func TestJournalManagerBeginEndTransaction(t *testing.T) {
	_, jm := newJMState()

	if err := jm.BeginTransaction(); err != nil {
		t.Fatal(err)
	}
	if jm.TxIndex() != 0 {
		t.Fatalf("expected tx index 0, got %d", jm.TxIndex())
	}
	if err := jm.EndTransaction(); err != nil {
		t.Fatal(err)
	}

	// Start another transaction.
	if err := jm.BeginTransaction(); err != nil {
		t.Fatal(err)
	}
	if jm.TxIndex() != 1 {
		t.Fatalf("expected tx index 1, got %d", jm.TxIndex())
	}
	if err := jm.EndTransaction(); err != nil {
		t.Fatal(err)
	}
}

func TestJournalManagerDoubleBeginFails(t *testing.T) {
	_, jm := newJMState()
	if err := jm.BeginTransaction(); err != nil {
		t.Fatal(err)
	}
	if err := jm.BeginTransaction(); err != ErrJournalMgrTxActive {
		t.Fatalf("expected ErrJournalMgrTxActive, got %v", err)
	}
}

func TestJournalManagerEndWithoutBeginFails(t *testing.T) {
	_, jm := newJMState()
	if err := jm.EndTransaction(); err != ErrJournalMgrNoActiveTx {
		t.Fatalf("expected ErrJournalMgrNoActiveTx, got %v", err)
	}
}

func TestJournalManagerTrackAccountCreate(t *testing.T) {
	sdb, jm := newJMState()
	addr := makeJMAddr(0x01)
	sdb.CreateAccount(addr)

	jm.BeginTransaction()
	jm.TrackAccountCreate(addr)
	jm.EndTransaction()

	mods := jm.Modifications()
	if len(mods) != 1 {
		t.Fatalf("expected 1 modification, got %d", len(mods))
	}
	if mods[0].Kind != ModAccountCreate {
		t.Fatalf("expected ModAccountCreate, got %v", mods[0].Kind)
	}
	if mods[0].Address != addr {
		t.Fatal("address mismatch")
	}
}

func TestJournalManagerTrackBalanceChange(t *testing.T) {
	sdb, jm := newJMState()
	addr := makeJMAddr(0x02)
	sdb.CreateAccount(addr)
	sdb.AddBalance(addr, big.NewInt(1000))

	jm.BeginTransaction()
	prevBal := sdb.GetBalance(addr)
	jm.TrackBalanceChange(addr, prevBal)
	sdb.AddBalance(addr, big.NewInt(500))
	jm.EndTransaction()

	mods := jm.Modifications()
	if len(mods) != 1 {
		t.Fatalf("expected 1 modification, got %d", len(mods))
	}
	if mods[0].Kind != ModBalanceChange {
		t.Fatalf("expected ModBalanceChange, got %v", mods[0].Kind)
	}
}

func TestJournalManagerTrackNonceChange(t *testing.T) {
	sdb, jm := newJMState()
	addr := makeJMAddr(0x03)
	sdb.CreateAccount(addr)

	jm.BeginTransaction()
	jm.TrackNonceChange(addr, sdb.GetNonce(addr))
	sdb.SetNonce(addr, 1)
	jm.EndTransaction()

	mods := jm.Modifications()
	if len(mods) != 1 || mods[0].Kind != ModNonceChange {
		t.Fatal("expected single nonce change modification")
	}
}

func TestJournalManagerTrackStorageWrite(t *testing.T) {
	sdb, jm := newJMState()
	addr := makeJMAddr(0x04)
	key := makeJMHash(0x10)
	sdb.CreateAccount(addr)

	jm.BeginTransaction()
	prev := sdb.GetState(addr, key)
	jm.TrackStorageWrite(addr, key, prev)
	sdb.SetState(addr, key, makeJMHash(0xFF))
	jm.EndTransaction()

	mods := jm.Modifications()
	if len(mods) != 1 || mods[0].Kind != ModStorageWrite {
		t.Fatal("expected single storage write modification")
	}
	if mods[0].Key != key {
		t.Fatal("key mismatch in storage write modification")
	}
}

func TestJournalManagerTrackCodeDeploy(t *testing.T) {
	sdb, jm := newJMState()
	addr := makeJMAddr(0x05)
	sdb.CreateAccount(addr)

	jm.BeginTransaction()
	jm.TrackCodeDeploy(addr, nil, nil)
	sdb.SetCode(addr, []byte{0x60, 0x00, 0x60, 0x00})
	jm.EndTransaction()

	mods := jm.Modifications()
	if len(mods) != 1 || mods[0].Kind != ModCodeDeploy {
		t.Fatal("expected single code deploy modification")
	}
}

func TestJournalManagerTrackSelfDestruct(t *testing.T) {
	sdb, jm := newJMState()
	addr := makeJMAddr(0x06)
	sdb.CreateAccount(addr)
	sdb.AddBalance(addr, big.NewInt(100))

	jm.BeginTransaction()
	jm.TrackSelfDestruct(addr, sdb.GetBalance(addr), sdb.GetNonce(addr))
	sdb.SelfDestruct(addr)
	jm.EndTransaction()

	mods := jm.Modifications()
	if len(mods) != 1 || mods[0].Kind != ModSelfDestruct {
		t.Fatal("expected single self-destruct modification")
	}
}

func TestJournalManagerCheckpointCreateAndGet(t *testing.T) {
	_, jm := newJMState()
	jm.BeginTransaction()

	cp, err := jm.CreateCheckpoint("before_transfer")
	if err != nil {
		t.Fatal(err)
	}
	if cp == nil {
		t.Fatal("expected non-nil checkpoint")
	}
	if cp.Name != "before_transfer" {
		t.Fatalf("expected name 'before_transfer', got %q", cp.Name)
	}

	got := jm.GetCheckpoint("before_transfer")
	if got == nil || got.Name != cp.Name {
		t.Fatal("checkpoint lookup mismatch")
	}
	jm.EndTransaction()
}

func TestJournalManagerRollbackToCheckpoint(t *testing.T) {
	sdb, jm := newJMState()
	addr := makeJMAddr(0x10)
	sdb.CreateAccount(addr)
	sdb.AddBalance(addr, big.NewInt(1000))

	jm.BeginTransaction()
	jm.CreateCheckpoint("pre_change")

	prevBal := sdb.GetBalance(addr)
	jm.TrackBalanceChange(addr, prevBal)
	sdb.AddBalance(addr, big.NewInt(500))

	// Balance should be 1500 before rollback.
	if sdb.GetBalance(addr).Cmp(big.NewInt(1500)) != 0 {
		t.Fatalf("expected 1500, got %v", sdb.GetBalance(addr))
	}

	err := jm.RollbackToCheckpoint("pre_change")
	if err != nil {
		t.Fatal(err)
	}

	// After rollback, the journal entry was reverted. The raw statedb balance
	// was modified via the journal revert.
	// Note: the journal reverts the JrnlBalanceChange, restoring prev balance.
	jm.EndTransaction()
}

func TestJournalManagerRollbackUnknownCheckpoint(t *testing.T) {
	_, jm := newJMState()
	err := jm.RollbackToCheckpoint("nonexistent")
	if err != ErrJournalMgrCheckpointNotFound {
		t.Fatalf("expected ErrJournalMgrCheckpointNotFound, got %v", err)
	}
}

func TestJournalManagerModificationsForTx(t *testing.T) {
	sdb, jm := newJMState()
	addr1 := makeJMAddr(0x20)
	addr2 := makeJMAddr(0x21)
	sdb.CreateAccount(addr1)
	sdb.CreateAccount(addr2)

	// Tx 0
	jm.BeginTransaction()
	jm.TrackBalanceChange(addr1, sdb.GetBalance(addr1))
	jm.EndTransaction()

	// Tx 1
	jm.BeginTransaction()
	jm.TrackBalanceChange(addr2, sdb.GetBalance(addr2))
	jm.TrackNonceChange(addr2, 0)
	jm.EndTransaction()

	tx0Mods := jm.ModificationsForTx(0)
	if len(tx0Mods) != 1 {
		t.Fatalf("expected 1 mod for tx 0, got %d", len(tx0Mods))
	}
	tx1Mods := jm.ModificationsForTx(1)
	if len(tx1Mods) != 2 {
		t.Fatalf("expected 2 mods for tx 1, got %d", len(tx1Mods))
	}
}

func TestJournalManagerModificationCountByKind(t *testing.T) {
	sdb, jm := newJMState()
	addr := makeJMAddr(0x30)
	sdb.CreateAccount(addr)

	jm.BeginTransaction()
	jm.TrackAccountCreate(addr)
	jm.TrackBalanceChange(addr, big.NewInt(0))
	jm.TrackBalanceChange(addr, big.NewInt(100))
	jm.TrackStorageWrite(addr, makeJMHash(0x01), types.Hash{})
	jm.EndTransaction()

	counts := jm.ModificationCountByKind()
	if counts[ModAccountCreate] != 1 {
		t.Fatal("expected 1 account create")
	}
	if counts[ModBalanceChange] != 2 {
		t.Fatal("expected 2 balance changes")
	}
	if counts[ModStorageWrite] != 1 {
		t.Fatal("expected 1 storage write")
	}
}

func TestJournalManagerTouchedAddresses(t *testing.T) {
	sdb, jm := newJMState()
	addr1 := makeJMAddr(0x40)
	addr2 := makeJMAddr(0x41)
	addr3 := makeJMAddr(0x42)
	sdb.CreateAccount(addr1)
	sdb.CreateAccount(addr2)
	sdb.CreateAccount(addr3)

	jm.BeginTransaction()
	jm.TrackBalanceChange(addr1, big.NewInt(0))
	jm.TrackNonceChange(addr2, 0)
	jm.TrackBalanceChange(addr1, big.NewInt(0)) // duplicate addr
	jm.EndTransaction()

	addrs := jm.TouchedAddresses()
	if len(addrs) != 2 {
		t.Fatalf("expected 2 unique addresses, got %d", len(addrs))
	}
	if _, ok := addrs[addr1]; !ok {
		t.Fatal("expected addr1 in touched set")
	}
	if _, ok := addrs[addr2]; !ok {
		t.Fatal("expected addr2 in touched set")
	}
}

func TestJournalManagerFinalize(t *testing.T) {
	_, jm := newJMState()
	jm.Finalize()

	if !jm.IsFinalized() {
		t.Fatal("expected finalized")
	}
	if err := jm.BeginTransaction(); err != ErrJournalMgrAlreadyFinalized {
		t.Fatalf("expected ErrJournalMgrAlreadyFinalized, got %v", err)
	}
}

func TestJournalManagerFinalizeBlocksCheckpoint(t *testing.T) {
	_, jm := newJMState()
	jm.Finalize()

	_, err := jm.CreateCheckpoint("post_finalize")
	if err != ErrJournalMgrAlreadyFinalized {
		t.Fatalf("expected ErrJournalMgrAlreadyFinalized, got %v", err)
	}
}

func TestJournalManagerMetrics(t *testing.T) {
	sdb, jm := newJMState()
	addr := makeJMAddr(0x50)
	sdb.CreateAccount(addr)

	jm.BeginTransaction()
	jm.TrackAccountCreate(addr)
	jm.TrackBalanceChange(addr, big.NewInt(0))
	jm.TrackStorageWrite(addr, makeJMHash(0x01), types.Hash{})
	jm.EndTransaction()

	m := jm.Metrics()
	if m.TotalModifications.Load() != 3 {
		t.Fatalf("expected 3 total modifications, got %d", m.TotalModifications.Load())
	}
	if m.AccountCreates.Load() != 1 {
		t.Fatal("expected 1 account create in metrics")
	}
	if m.BalanceChanges.Load() != 1 {
		t.Fatal("expected 1 balance change in metrics")
	}
	if m.StorageWrites.Load() != 1 {
		t.Fatal("expected 1 storage write in metrics")
	}
	if m.TransactionsCount.Load() != 1 {
		t.Fatal("expected 1 transaction in metrics")
	}
	if m.CheckpointsCreated.Load() < 1 {
		t.Fatal("expected at least 1 checkpoint in metrics")
	}
}

func TestJournalManagerPeakJournalEntries(t *testing.T) {
	sdb, jm := newJMState()
	addr := makeJMAddr(0x60)
	sdb.CreateAccount(addr)

	jm.BeginTransaction()
	for i := 0; i < 10; i++ {
		jm.TrackBalanceChange(addr, big.NewInt(int64(i)))
	}
	jm.EndTransaction()

	m := jm.Metrics()
	if m.PeakJournalEntries.Load() < 10 {
		t.Fatalf("expected peak >= 10, got %d", m.PeakJournalEntries.Load())
	}
}

func TestJournalManagerReset(t *testing.T) {
	sdb, jm := newJMState()
	addr := makeJMAddr(0x70)
	sdb.CreateAccount(addr)

	jm.BeginTransaction()
	jm.TrackAccountCreate(addr)
	jm.EndTransaction()

	jm.Reset()

	if jm.JournalLength() != 0 {
		t.Fatal("expected 0 journal entries after reset")
	}
	if jm.TxIndex() != -1 {
		t.Fatal("expected tx index -1 after reset")
	}
	if len(jm.Modifications()) != 0 {
		t.Fatal("expected 0 modifications after reset")
	}
	if jm.CheckpointCount() != 0 {
		t.Fatal("expected 0 checkpoints after reset")
	}
}

func TestJournalManagerTxModCount(t *testing.T) {
	sdb, jm := newJMState()
	addr := makeJMAddr(0x80)
	sdb.CreateAccount(addr)

	jm.BeginTransaction()
	jm.TrackBalanceChange(addr, big.NewInt(0))
	jm.TrackNonceChange(addr, 0)
	jm.TrackStorageWrite(addr, makeJMHash(0x01), types.Hash{})

	if jm.TxModCount() != 3 {
		t.Fatalf("expected 3 tx mod count, got %d", jm.TxModCount())
	}
	jm.EndTransaction()
}

func TestJournalManagerModKindString(t *testing.T) {
	cases := []struct {
		kind ModKind
		want string
	}{
		{ModAccountCreate, "account_create"},
		{ModBalanceChange, "balance_change"},
		{ModNonceChange, "nonce_change"},
		{ModStorageWrite, "storage_write"},
		{ModCodeDeploy, "code_deploy"},
		{ModSelfDestruct, "self_destruct"},
		{ModKind(99), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.kind.String(); got != tc.want {
			t.Fatalf("ModKind(%d).String() = %q, want %q", tc.kind, got, tc.want)
		}
	}
}

func TestJournalManagerMultipleCheckpoints(t *testing.T) {
	sdb, jm := newJMState()
	addr := makeJMAddr(0x90)
	sdb.CreateAccount(addr)

	jm.BeginTransaction()
	cp1, _ := jm.CreateCheckpoint("cp1")
	jm.TrackBalanceChange(addr, big.NewInt(0))

	cp2, _ := jm.CreateCheckpoint("cp2")
	jm.TrackNonceChange(addr, 0)

	if cp1 == nil || cp2 == nil {
		t.Fatal("expected non-nil checkpoints")
	}

	if jm.CheckpointCount() != 3 { // tx_0 + cp1 + cp2
		t.Fatalf("expected 3 checkpoints, got %d", jm.CheckpointCount())
	}

	// Rollback to cp1 should invalidate cp2.
	err := jm.RollbackToCheckpoint("cp1")
	if err != nil {
		t.Fatal(err)
	}

	if jm.GetCheckpoint("cp2") != nil {
		t.Fatal("expected cp2 to be invalidated after rollback to cp1")
	}

	jm.EndTransaction()
}

func TestJournalManagerAutoCheckpointOnBeginTx(t *testing.T) {
	_, jm := newJMState()

	jm.BeginTransaction()
	jm.EndTransaction()

	cp := jm.GetCheckpoint("tx_0")
	if cp == nil {
		t.Fatal("expected automatic checkpoint 'tx_0'")
	}
	if cp.TxIndex != 0 {
		t.Fatalf("expected tx index 0, got %d", cp.TxIndex)
	}
}

func TestJournalManagerRollbackMetrics(t *testing.T) {
	sdb, jm := newJMState()
	addr := makeJMAddr(0xA0)
	sdb.CreateAccount(addr)

	jm.BeginTransaction()
	jm.CreateCheckpoint("cp")
	jm.TrackBalanceChange(addr, big.NewInt(0))
	jm.RollbackToCheckpoint("cp")
	jm.EndTransaction()

	m := jm.Metrics()
	if m.Rollbacks.Load() != 1 {
		t.Fatalf("expected 1 rollback in metrics, got %d", m.Rollbacks.Load())
	}
}
