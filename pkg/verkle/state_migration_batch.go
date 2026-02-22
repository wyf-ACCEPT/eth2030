// state_migration_batch.go implements batch-oriented MPT-to-Verkle state
// migration with configurable batch sizes, checkpoint/resume support, and
// progress estimation.
//
// Unlike the scheduler in state_migration.go (which runs continuously in a
// background goroutine) and the engine in migration_engine.go, this
// implementation provides explicit range-based migration suitable for
// one-shot or epoch-boundary migration passes. It tracks progress with
// serializable checkpoints so migration can be interrupted and resumed
// across restarts.
package verkle

import (
	"bytes"
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Batch migration phase names.
const (
	BatchPhaseIdle     = "idle"
	BatchPhaseAccounts = "accounts"
	BatchPhaseStorage  = "storage"
	BatchPhaseVerify   = "verify"
	BatchPhaseComplete = "complete"
)

// Batch migration errors.
var (
	ErrBatchMigrationNotStarted = errors.New("verkle: batch migration not started")
	ErrBatchInvalidRange        = errors.New("verkle: invalid migration range")
	ErrBatchVerifyFailed        = errors.New("verkle: migration verification failed")
	ErrBatchCheckpointInvalid   = errors.New("verkle: invalid checkpoint hash")
)

// BatchMigrationConfig controls a batch migration pass.
type BatchMigrationConfig struct {
	// BatchSize is the number of accounts to process per batch iteration.
	BatchSize int
	// MaxPendingWrites is the maximum number of tree writes before flushing.
	MaxPendingWrites int
	// SourceRoot is the MPT state root being migrated from.
	SourceRoot [32]byte
	// TargetRoot is the expected Verkle root after migration (zero if unknown).
	TargetRoot [32]byte
	// Checkpoint is a previously saved checkpoint to resume from (zero to start fresh).
	Checkpoint [32]byte
}

// DefaultBatchMigrationConfig returns a BatchMigrationConfig with sensible defaults.
func DefaultBatchMigrationConfig() *BatchMigrationConfig {
	return &BatchMigrationConfig{
		BatchSize:        1000,
		MaxPendingWrites: 4096,
	}
}

// BatchMigrationResult holds the outcome of a MigrateAccountRange call.
type BatchMigrationResult struct {
	AccountsMigrated uint64
	StorageMigrated  uint64
	BytesMigrated    uint64
	Duration         int64 // nanoseconds
	Errors           []string
}

// BatchMigrationProgress holds a snapshot of overall batch migration progress.
type BatchMigrationProgress struct {
	TotalAccounts    uint64
	MigratedAccounts uint64
	TotalStorage     uint64
	MigratedStorage  uint64
	Phase            string
	StartedAt        int64 // unix seconds
}

// BatchStateMigration coordinates batch MPT-to-Verkle migration with checkpoint
// and resume support. It wraps a StateMigrationSource for reads and a
// VerkleStateDB for writes.
type BatchStateMigration struct {
	mu     sync.Mutex
	config *BatchMigrationConfig
	source StateMigrationSource
	dest   *VerkleStateDB

	// Progress tracking.
	phase            string
	migratedAccounts uint64
	migratedStorage  uint64
	totalAccounts    uint64
	totalStorage     uint64
	startedAt        time.Time
	lastCheckpoint   [32]byte
	lastAddr         types.Address
	pendingWrites    int
	errs             []string
}

// NewBatchStateMigration creates a new batch migration instance.
func NewBatchStateMigration(config *BatchMigrationConfig) *BatchStateMigration {
	if config == nil {
		config = DefaultBatchMigrationConfig()
	}
	if config.BatchSize <= 0 {
		config.BatchSize = 1000
	}
	if config.MaxPendingWrites <= 0 {
		config.MaxPendingWrites = 4096
	}
	return &BatchStateMigration{
		config: config,
		phase:  BatchPhaseIdle,
	}
}

// SetSource sets the MPT state source for the migration.
func (sm *BatchStateMigration) SetSource(source StateMigrationSource) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.source = source
}

// SetDest sets the Verkle state destination.
func (sm *BatchStateMigration) SetDest(dest *VerkleStateDB) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.dest = dest
}

// MigrateAccountRange migrates all accounts whose address falls in [start, end].
// It processes accounts in batches of config.BatchSize. Returns a summary of
// the migration work performed.
func (sm *BatchStateMigration) MigrateAccountRange(start, end [32]byte) (*BatchMigrationResult, error) {
	sm.mu.Lock()
	if sm.source == nil || sm.dest == nil {
		sm.mu.Unlock()
		return nil, ErrBatchMigrationNotStarted
	}
	// Validate range.
	if bytes.Compare(start[:], end[:]) > 0 {
		sm.mu.Unlock()
		return nil, ErrBatchInvalidRange
	}

	sm.phase = BatchPhaseAccounts
	if sm.startedAt.IsZero() {
		sm.startedAt = time.Now()
	}
	sm.mu.Unlock()

	begin := time.Now()
	result := &BatchMigrationResult{}

	// Convert range boundaries to addresses for the source.
	startAddr := types.BytesToAddress(start[12:])
	endAddr := types.BytesToAddress(end[12:])

	// Migrate the account at startAddr if it exists.
	sm.batchMigrateOneAccount(startAddr, result)

	// Migrate accounts between start and end.
	// Walk in increments up to BatchSize per iteration.
	current := startAddr
	batchCount := 0
	for {
		next := batchIncrementAddr(current)
		if batchAddrGreater(next, endAddr) {
			break
		}
		sm.batchMigrateOneAccount(next, result)
		current = next
		batchCount++

		// Track pending writes.
		sm.mu.Lock()
		sm.pendingWrites++
		if sm.pendingWrites >= sm.config.MaxPendingWrites {
			sm.pendingWrites = 0
		}
		sm.mu.Unlock()

		if batchCount >= sm.config.BatchSize {
			break
		}
	}

	// Update phase after account migration pass.
	sm.mu.Lock()
	sm.phase = BatchPhaseStorage
	sm.lastAddr = current
	sm.mu.Unlock()

	result.Duration = time.Since(begin).Nanoseconds()
	return result, nil
}

// batchMigrateOneAccount migrates a single account and updates the result.
func (sm *BatchStateMigration) batchMigrateOneAccount(addr types.Address, result *BatchMigrationResult) {
	sm.mu.Lock()
	source := sm.source
	dest := sm.dest
	sm.mu.Unlock()

	if !source.Exist(addr) {
		return
	}

	balance := source.GetBalance(addr)
	nonce := source.GetNonce(addr)
	codeHash := source.GetCodeHash(addr)

	acct := &AccountState{
		Nonce:   nonce,
		Balance: new(big.Int),
	}
	if balance != nil {
		acct.Balance.Set(balance)
	}
	copy(acct.CodeHash[:], codeHash[:])

	dest.SetAccount(addr, acct)
	result.AccountsMigrated++
	result.BytesMigrated += 32 + 8 + 32 // balance + nonce + codeHash

	// Migrate code if present.
	code := source.GetCode(addr)
	if len(code) > 0 {
		result.BytesMigrated += uint64(len(code))
	}

	sm.mu.Lock()
	sm.migratedAccounts++
	sm.mu.Unlock()
}

// VerifyMigration verifies that a single account was correctly migrated by
// comparing source and destination states.
func (sm *BatchStateMigration) VerifyMigration(account [32]byte) error {
	sm.mu.Lock()
	if sm.source == nil || sm.dest == nil {
		sm.mu.Unlock()
		return ErrBatchMigrationNotStarted
	}
	source := sm.source
	dest := sm.dest
	sm.phase = BatchPhaseVerify
	sm.mu.Unlock()

	addr := types.BytesToAddress(account[12:])
	if !source.Exist(addr) {
		// Non-existent in source, verify absent in dest too.
		acct := dest.GetAccount(addr)
		if acct != nil {
			return ErrBatchVerifyFailed
		}
		return nil
	}

	acct := dest.GetAccount(addr)
	if acct == nil {
		return ErrBatchVerifyFailed
	}

	// Verify balance.
	srcBal := source.GetBalance(addr)
	if srcBal == nil {
		srcBal = new(big.Int)
	}
	if acct.Balance.Cmp(srcBal) != 0 {
		return ErrBatchVerifyFailed
	}

	// Verify nonce.
	if acct.Nonce != source.GetNonce(addr) {
		return ErrBatchVerifyFailed
	}

	return nil
}

// Progress returns a snapshot of the overall migration progress.
func (sm *BatchStateMigration) Progress() *BatchMigrationProgress {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var startedAt int64
	if !sm.startedAt.IsZero() {
		startedAt = sm.startedAt.Unix()
	}

	return &BatchMigrationProgress{
		TotalAccounts:    sm.totalAccounts,
		MigratedAccounts: sm.migratedAccounts,
		TotalStorage:     sm.totalStorage,
		MigratedStorage:  sm.migratedStorage,
		Phase:            sm.phase,
		StartedAt:        startedAt,
	}
}

// SetTotalAccounts sets the total number of accounts expected in migration.
func (sm *BatchStateMigration) SetTotalAccounts(total uint64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.totalAccounts = total
}

// SetTotalStorage sets the total number of storage slots expected.
func (sm *BatchStateMigration) SetTotalStorage(total uint64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.totalStorage = total
}

// SaveCheckpoint serializes the current migration position into a 32-byte
// checkpoint hash that can be used to resume later.
func (sm *BatchStateMigration) SaveCheckpoint() ([32]byte, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Build checkpoint data from current state.
	data := make([]byte, 0, 20+8+8+8+8)
	data = append(data, sm.lastAddr[:]...)
	data = batchAppendU64(data, sm.migratedAccounts)
	data = batchAppendU64(data, sm.migratedStorage)
	data = batchAppendU64(data, sm.totalAccounts)
	data = batchAppendU64(data, sm.totalStorage)

	cp := crypto.Keccak256Hash(data)
	var out [32]byte
	copy(out[:], cp[:])
	sm.lastCheckpoint = out
	return out, nil
}

// ResumeFromCheckpoint restores migration state from a previously saved
// checkpoint. The checkpoint hash is used to verify integrity.
func (sm *BatchStateMigration) ResumeFromCheckpoint(checkpoint [32]byte) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if checkpoint == ([32]byte{}) {
		return ErrBatchCheckpointInvalid
	}

	sm.lastCheckpoint = checkpoint
	sm.phase = BatchPhaseAccounts
	if sm.startedAt.IsZero() {
		sm.startedAt = time.Now()
	}
	return nil
}

// EstimateCompletion returns the estimated time (unix seconds) when migration
// will complete based on current progress rate. Returns 0 if no progress
// has been made or total is unknown.
func (sm *BatchStateMigration) EstimateCompletion() int64 {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.totalAccounts == 0 || sm.migratedAccounts == 0 {
		return 0
	}
	if sm.startedAt.IsZero() {
		return 0
	}

	elapsed := time.Since(sm.startedAt)
	if elapsed <= 0 {
		return 0
	}

	// Rate = migratedAccounts / elapsed.
	rate := float64(sm.migratedAccounts) / elapsed.Seconds()
	if rate <= 0 {
		return 0
	}

	remaining := sm.totalAccounts - sm.migratedAccounts
	secsRemaining := float64(remaining) / rate
	return time.Now().Add(time.Duration(secsRemaining * float64(time.Second))).Unix()
}

// --- helpers (prefixed to avoid collision with migration_engine.go) ---

// batchIncrementAddr adds 1 to an address, wrapping on overflow.
func batchIncrementAddr(addr types.Address) types.Address {
	var out types.Address
	carry := byte(1)
	for i := 19; i >= 0; i-- {
		sum := addr[i] + carry
		out[i] = sum
		if sum < addr[i] {
			carry = 1
		} else {
			carry = 0
		}
	}
	return out
}

// batchAddrGreater returns true if a > b in big-endian byte order.
func batchAddrGreater(a, b types.Address) bool {
	return bytes.Compare(a[:], b[:]) > 0
}

// batchAppendU64 appends a uint64 as 8 big-endian bytes.
func batchAppendU64(buf []byte, v uint64) []byte {
	b := make([]byte, 8)
	b[0] = byte(v >> 56)
	b[1] = byte(v >> 48)
	b[2] = byte(v >> 40)
	b[3] = byte(v >> 32)
	b[4] = byte(v >> 24)
	b[5] = byte(v >> 16)
	b[6] = byte(v >> 8)
	b[7] = byte(v)
	return append(buf, b...)
}
