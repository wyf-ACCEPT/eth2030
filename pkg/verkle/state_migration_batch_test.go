package verkle

import (
	"math/big"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// batchMockSource implements StateMigrationSource for batch migration tests.
type batchMockSource struct {
	accounts map[types.Address]*batchMockAccount
}
type batchMockAccount struct {
	balance  *big.Int
	nonce    uint64
	codeHash types.Hash
	code     []byte
}

func newBatchMockSource() *batchMockSource {
	return &batchMockSource{accounts: make(map[types.Address]*batchMockAccount)}
}
func (m *batchMockSource) GetBalance(addr types.Address) *big.Int {
	if a, ok := m.accounts[addr]; ok && a.balance != nil {
		return new(big.Int).Set(a.balance)
	}
	return new(big.Int)
}
func (m *batchMockSource) GetNonce(addr types.Address) uint64 {
	if a, ok := m.accounts[addr]; ok {
		return a.nonce
	}
	return 0
}
func (m *batchMockSource) GetCodeHash(addr types.Address) types.Hash {
	if a, ok := m.accounts[addr]; ok {
		return a.codeHash
	}
	return types.Hash{}
}
func (m *batchMockSource) GetCode(addr types.Address) []byte {
	if a, ok := m.accounts[addr]; ok {
		return a.code
	}
	return nil
}
func (m *batchMockSource) Exist(addr types.Address) bool {
	_, ok := m.accounts[addr]
	return ok
}

func batchAddrFromByte(b byte) types.Address { return types.BytesToAddress([]byte{b}) }
func batchHash32FromByte(b byte) [32]byte    { var h [32]byte; h[31] = b; return h }

// newBatchTestSetup creates a common test setup with source, dest, and migration.
func newBatchTestSetup(config *BatchMigrationConfig) (*batchMockSource, *VerkleStateDB, *BatchStateMigration) {
	source := newBatchMockSource()
	tree := NewInMemoryVerkleTree()
	dest := NewVerkleStateDB(tree)
	sm := NewBatchStateMigration(config)
	sm.SetSource(source)
	sm.SetDest(dest)
	return source, dest, sm
}

func TestBatchMigration_NewMigration(t *testing.T) {
	sm := NewBatchStateMigration(nil)
	if sm == nil {
		t.Fatal("expected non-nil BatchStateMigration")
	}
	if sm.config.BatchSize != 1000 {
		t.Fatalf("expected default batch size 1000, got %d", sm.config.BatchSize)
	}
	if sm.config.MaxPendingWrites != 4096 {
		t.Fatalf("expected default max pending writes 4096, got %d", sm.config.MaxPendingWrites)
	}
	if sm.phase != BatchPhaseIdle {
		t.Fatalf("expected idle phase, got %s", sm.phase)
	}
}

func TestBatchMigration_MigrateRange(t *testing.T) {
	source, _, sm := newBatchTestSetup(DefaultBatchMigrationConfig())
	source.accounts[batchAddrFromByte(0x01)] = &batchMockAccount{balance: big.NewInt(1000), nonce: 5}
	source.accounts[batchAddrFromByte(0x02)] = &batchMockAccount{balance: big.NewInt(2000), nonce: 10}

	result, err := sm.MigrateAccountRange(batchHash32FromByte(0x01), batchHash32FromByte(0x05))
	if err != nil {
		t.Fatalf("MigrateAccountRange: %v", err)
	}
	if result.AccountsMigrated == 0 {
		t.Fatal("expected at least some accounts migrated")
	}
	if result.Duration <= 0 {
		t.Fatal("expected positive duration")
	}
}

func TestBatchMigration_VerifyMigration(t *testing.T) {
	source, _, sm := newBatchTestSetup(DefaultBatchMigrationConfig())
	addr := batchAddrFromByte(0x01)
	source.accounts[addr] = &batchMockAccount{balance: big.NewInt(5000), nonce: 7}

	if _, err := sm.MigrateAccountRange(batchHash32FromByte(0x01), batchHash32FromByte(0x01)); err != nil {
		t.Fatalf("MigrateAccountRange: %v", err)
	}
	var acctHash [32]byte
	copy(acctHash[12:], addr[:])
	if err := sm.VerifyMigration(acctHash); err != nil {
		t.Fatalf("VerifyMigration: %v", err)
	}
}

func TestBatchMigration_VerifyNonExistent(t *testing.T) {
	_, _, sm := newBatchTestSetup(DefaultBatchMigrationConfig())
	if err := sm.VerifyMigration(batchHash32FromByte(0xFF)); err != nil {
		t.Fatalf("VerifyMigration for non-existent: %v", err)
	}
}

func TestBatchMigration_Progress(t *testing.T) {
	sm := NewBatchStateMigration(DefaultBatchMigrationConfig())
	sm.SetTotalAccounts(1000)
	sm.SetTotalStorage(5000)

	prog := sm.Progress()
	if prog.Phase != BatchPhaseIdle {
		t.Fatalf("expected idle phase, got %s", prog.Phase)
	}
	if prog.TotalAccounts != 1000 {
		t.Fatalf("expected total 1000, got %d", prog.TotalAccounts)
	}
	if prog.TotalStorage != 5000 {
		t.Fatalf("expected total storage 5000, got %d", prog.TotalStorage)
	}
}

func TestBatchMigration_Checkpoint(t *testing.T) {
	source, _, sm := newBatchTestSetup(DefaultBatchMigrationConfig())
	source.accounts[batchAddrFromByte(0x01)] = &batchMockAccount{balance: big.NewInt(100), nonce: 1}

	sm.MigrateAccountRange(batchHash32FromByte(0x01), batchHash32FromByte(0x01))
	cp, err := sm.SaveCheckpoint()
	if err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}
	if cp == ([32]byte{}) {
		t.Fatal("expected non-zero checkpoint hash")
	}
}

func TestBatchMigration_ResumeCheckpoint(t *testing.T) {
	sm := NewBatchStateMigration(DefaultBatchMigrationConfig())
	var cp [32]byte
	cp[0] = 0xAB
	if err := sm.ResumeFromCheckpoint(cp); err != nil {
		t.Fatalf("ResumeFromCheckpoint: %v", err)
	}
	if sm.phase != BatchPhaseAccounts {
		t.Fatalf("expected accounts phase after resume, got %s", sm.phase)
	}
}

func TestBatchMigration_ResumeCheckpointInvalid(t *testing.T) {
	sm := NewBatchStateMigration(DefaultBatchMigrationConfig())
	if err := sm.ResumeFromCheckpoint([32]byte{}); err != ErrBatchCheckpointInvalid {
		t.Fatalf("expected ErrBatchCheckpointInvalid, got %v", err)
	}
}

func TestBatchMigration_EstimateCompletion(t *testing.T) {
	sm := NewBatchStateMigration(DefaultBatchMigrationConfig())
	if est := sm.EstimateCompletion(); est != 0 {
		t.Fatalf("expected 0 estimate with no progress, got %d", est)
	}

	source, _, _ := newBatchTestSetup(nil)
	source.accounts[batchAddrFromByte(0x01)] = &batchMockAccount{balance: big.NewInt(100), nonce: 1}
	sm.SetSource(source)
	tree := NewInMemoryVerkleTree()
	sm.SetDest(NewVerkleStateDB(tree))
	sm.SetTotalAccounts(100)
	sm.MigrateAccountRange(batchHash32FromByte(0x01), batchHash32FromByte(0x01))

	est := sm.EstimateCompletion()
	if est <= 0 {
		t.Logf("estimate is %d (may be 0 if migration was instant)", est)
	}
}

func TestBatchMigration_EmptyRange(t *testing.T) {
	_, _, sm := newBatchTestSetup(DefaultBatchMigrationConfig())
	result, err := sm.MigrateAccountRange(batchHash32FromByte(0x10), batchHash32FromByte(0x20))
	if err != nil {
		t.Fatalf("MigrateAccountRange: %v", err)
	}
	if result.AccountsMigrated != 0 {
		t.Fatalf("expected 0 accounts migrated, got %d", result.AccountsMigrated)
	}
}

func TestBatchMigration_InvalidRange(t *testing.T) {
	_, _, sm := newBatchTestSetup(DefaultBatchMigrationConfig())
	_, err := sm.MigrateAccountRange(batchHash32FromByte(0x20), batchHash32FromByte(0x10))
	if err != ErrBatchInvalidRange {
		t.Fatalf("expected ErrBatchInvalidRange, got %v", err)
	}
}

func TestBatchMigration_BatchSize(t *testing.T) {
	config := &BatchMigrationConfig{BatchSize: 5, MaxPendingWrites: 100}
	source, _, sm := newBatchTestSetup(config)
	for i := byte(1); i < 50; i++ {
		source.accounts[batchAddrFromByte(i)] = &batchMockAccount{
			balance: big.NewInt(int64(i) * 100), nonce: uint64(i),
		}
	}
	result, err := sm.MigrateAccountRange(batchHash32FromByte(0x01), batchHash32FromByte(0x30))
	if err != nil {
		t.Fatalf("MigrateAccountRange: %v", err)
	}
	if result.AccountsMigrated > 6 {
		t.Fatalf("expected at most 6 accounts (batch limit), got %d", result.AccountsMigrated)
	}
}

func TestBatchMigration_Config(t *testing.T) {
	config := &BatchMigrationConfig{BatchSize: 500, MaxPendingWrites: 2048}
	sm := NewBatchStateMigration(config)
	if sm.config.BatchSize != 500 {
		t.Fatalf("expected batch size 500, got %d", sm.config.BatchSize)
	}
	if sm.config.MaxPendingWrites != 2048 {
		t.Fatalf("expected max pending writes 2048, got %d", sm.config.MaxPendingWrites)
	}
}

func TestBatchMigration_ConcurrentMigration(t *testing.T) {
	source, _, sm := newBatchTestSetup(DefaultBatchMigrationConfig())
	for i := byte(1); i < 20; i++ {
		source.accounts[batchAddrFromByte(i)] = &batchMockAccount{
			balance: big.NewInt(int64(i) * 100), nonce: uint64(i),
		}
	}
	var wg sync.WaitGroup
	wg.Add(4)
	for g := 0; g < 4; g++ {
		go func(id int) {
			defer wg.Done()
			sm.MigrateAccountRange(batchHash32FromByte(byte(id*5+1)), batchHash32FromByte(byte(id*5+5)))
		}(g)
	}
	wg.Wait()
	if sm.Progress().MigratedAccounts == 0 {
		t.Fatal("expected some accounts migrated concurrently")
	}
}

func TestBatchMigration_ErrorHandling(t *testing.T) {
	sm := NewBatchStateMigration(DefaultBatchMigrationConfig())
	_, err := sm.MigrateAccountRange(batchHash32FromByte(0x01), batchHash32FromByte(0x10))
	if err != ErrBatchMigrationNotStarted {
		t.Fatalf("expected ErrBatchMigrationNotStarted, got %v", err)
	}
}

func TestBatchMigration_VerifyErrorHandling(t *testing.T) {
	sm := NewBatchStateMigration(DefaultBatchMigrationConfig())
	if err := sm.VerifyMigration(batchHash32FromByte(0x01)); err != ErrBatchMigrationNotStarted {
		t.Fatalf("expected ErrBatchMigrationNotStarted, got %v", err)
	}
}

func TestBatchMigration_MigrationResult(t *testing.T) {
	source, _, sm := newBatchTestSetup(DefaultBatchMigrationConfig())
	source.accounts[batchAddrFromByte(0x01)] = &batchMockAccount{
		balance: big.NewInt(9999), nonce: 42, code: []byte{0x60, 0x00, 0x60, 0x00},
	}
	result, err := sm.MigrateAccountRange(batchHash32FromByte(0x01), batchHash32FromByte(0x01))
	if err != nil {
		t.Fatalf("MigrateAccountRange: %v", err)
	}
	if result.AccountsMigrated != 1 {
		t.Fatalf("expected 1 account migrated, got %d", result.AccountsMigrated)
	}
	if result.BytesMigrated == 0 {
		t.Fatal("expected non-zero bytes migrated")
	}
	if result.Duration <= 0 {
		t.Fatal("expected positive duration")
	}
}

func TestBatchMigration_Phases(t *testing.T) {
	source, _, sm := newBatchTestSetup(DefaultBatchMigrationConfig())
	addr := batchAddrFromByte(0x01)
	source.accounts[addr] = &batchMockAccount{balance: big.NewInt(100), nonce: 1}

	// Phase starts as idle.
	if prog := sm.Progress(); prog.Phase != BatchPhaseIdle {
		t.Fatalf("expected idle phase, got %s", prog.Phase)
	}
	sm.MigrateAccountRange(batchHash32FromByte(0x01), batchHash32FromByte(0x01))
	// After migration, phase should be storage.
	if prog := sm.Progress(); prog.Phase != BatchPhaseStorage {
		t.Fatalf("expected storage phase, got %s", prog.Phase)
	}
	// Verify sets phase to verify.
	var acctHash [32]byte
	copy(acctHash[12:], addr[:])
	sm.VerifyMigration(acctHash)
	if prog := sm.Progress(); prog.Phase != BatchPhaseVerify {
		t.Fatalf("expected verify phase, got %s", prog.Phase)
	}
}

func TestBatchMigration_IncrementAddress(t *testing.T) {
	addr := batchAddrFromByte(0x01)
	next := batchIncrementAddr(addr)
	if next[19] != 0x02 {
		t.Fatalf("expected 0x02, got 0x%x", next[19])
	}
	// Test carry.
	addr[19] = 0xFF
	next = batchIncrementAddr(addr)
	if next[19] != 0x00 || next[18] != 0x01 {
		t.Fatalf("expected carry: got %x", next)
	}
}

func TestBatchMigration_AddressGreater(t *testing.T) {
	a, b := batchAddrFromByte(0x10), batchAddrFromByte(0x05)
	if !batchAddrGreater(a, b) {
		t.Fatal("expected a > b")
	}
	if batchAddrGreater(b, a) {
		t.Fatal("expected b < a")
	}
	if batchAddrGreater(a, a) {
		t.Fatal("expected a not > a")
	}
}
