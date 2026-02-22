// state_migration.go implements the MPT-to-Verkle state migration scheduler.
//
// The Ethereum state transition from Merkle Patricia Tries (MPT) to Verkle
// trees requires migrating all account and storage data. This scheduler
// coordinates background trie walks, converts MPT leaf data to Verkle
// key/value pairs, tracks progress with resumable checkpoints, and handles
// storage slot migration per account.
//
// The scheduler is designed to run alongside normal block processing,
// throttling migration work to avoid impacting chain head tracking.
package verkle

import (
	"encoding/binary"
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// MigrationSchedulerConfig controls the background migration scheduler.
type MigrationSchedulerConfig struct {
	// AccountsPerTick is the maximum number of accounts to migrate per tick.
	AccountsPerTick int
	// StorageSlotsPerAccount is the max storage slots to migrate per account
	// in a single tick before yielding.
	StorageSlotsPerAccount int
	// TickInterval is the minimum time between migration ticks.
	TickInterval time.Duration
	// MaxPendingAccounts is the backlog limit for accounts waiting to be
	// migrated. If the queue exceeds this, new enqueues are dropped.
	MaxPendingAccounts int
}

// DefaultSchedulerConfig returns a MigrationSchedulerConfig with production
// defaults suitable for mainnet.
func DefaultSchedulerConfig() MigrationSchedulerConfig {
	return MigrationSchedulerConfig{
		AccountsPerTick:        500,
		StorageSlotsPerAccount: 1000,
		TickInterval:           50 * time.Millisecond,
		MaxPendingAccounts:     100000,
	}
}

// StorageIterator provides iteration over storage slots for a given account.
type StorageIterator interface {
	// Next advances to the next storage slot. Returns false when exhausted.
	Next() bool
	// Key returns the current storage slot key.
	Key() types.Hash
	// Value returns the current storage slot value.
	Value() types.Hash
	// Error returns any error encountered during iteration.
	Error() error
}

// MigrationSourceReader extends StateMigrationSource with storage iteration
// and account enumeration for the background scheduler.
type MigrationSourceReader interface {
	StateMigrationSource
	// AccountIterator returns an iterator over all accounts starting at or
	// after the given address. If start is zero, iteration begins from the
	// first account.
	AccountIterator(start types.Address) AccountIterator
	// StorageIterator returns an iterator over all storage slots for the
	// given account.
	StorageIterator(addr types.Address) StorageIterator
}

// AccountIterator provides iteration over accounts in the source MPT.
type AccountIterator interface {
	// Next advances to the next account. Returns false when exhausted.
	Next() bool
	// Address returns the current account address.
	Address() types.Address
	// Error returns any error encountered during iteration.
	Error() error
}

// MigrationCheckpoint records the scheduler's position so migration can be
// resumed after a restart.
type MigrationCheckpoint struct {
	// LastAddress is the last fully migrated account address. Resume from
	// the next address after this one.
	LastAddress types.Address
	// LastStorageKey is the last migrated storage key within the account
	// currently being migrated (for partial account migration).
	LastStorageKey types.Hash
	// Timestamp is when this checkpoint was created.
	Timestamp time.Time
	// AccountsDone is the total number of accounts migrated so far.
	AccountsDone uint64
	// StorageDone is the total number of storage slots migrated so far.
	StorageDone uint64
}

// SchedulerState represents the current state of the migration scheduler.
type SchedulerState int

const (
	// SchedulerIdle means the scheduler has not been started.
	SchedulerIdle SchedulerState = iota
	// SchedulerRunning means the scheduler is actively migrating.
	SchedulerRunning
	// SchedulerPaused means the scheduler has been temporarily paused.
	SchedulerPaused
	// SchedulerDone means the migration has completed.
	SchedulerDone
)

// MigrationScheduler coordinates background MPT-to-Verkle state migration.
// It walks the MPT trie in address order, converts each account and its
// storage into Verkle key/value entries, and writes them to the destination
// Verkle tree. Progress is checkpointed periodically for resumability.
type MigrationScheduler struct {
	mu     sync.Mutex
	config MigrationSchedulerConfig
	source MigrationSourceReader
	dest   *VerkleStateDB
	state  SchedulerState

	// Progress tracking.
	checkpoint   MigrationCheckpoint
	accountsDone uint64
	storageDone  uint64
	errCount     uint64
	lastError    error
	startTime    time.Time
	lastTickTime time.Time

	// Stop signal for the background goroutine.
	stopCh chan struct{}
	doneCh chan struct{}
}

// NewMigrationScheduler creates a new migration scheduler. The scheduler
// does not start automatically; call Start() to begin background migration.
func NewMigrationScheduler(
	config MigrationSchedulerConfig,
	source MigrationSourceReader,
	dest *VerkleStateDB,
) *MigrationScheduler {
	return &MigrationScheduler{
		config: config,
		source: source,
		dest:   dest,
		state:  SchedulerIdle,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

// ResumeFrom restores the scheduler's position from a previously saved
// checkpoint. Must be called before Start().
func (s *MigrationScheduler) ResumeFrom(cp MigrationCheckpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state == SchedulerRunning {
		return errors.New("migration: cannot resume while running")
	}
	s.checkpoint = cp
	s.accountsDone = cp.AccountsDone
	s.storageDone = cp.StorageDone
	return nil
}

// Start begins the background migration process.
func (s *MigrationScheduler) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state == SchedulerRunning {
		return errors.New("migration: scheduler already running")
	}
	s.state = SchedulerRunning
	s.startTime = time.Now()
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})

	go s.run()
	return nil
}

// Stop halts the background migration and waits for the current tick to
// finish. The scheduler can be resumed later with Start().
func (s *MigrationScheduler) Stop() {
	s.mu.Lock()
	if s.state != SchedulerRunning {
		s.mu.Unlock()
		return
	}
	s.state = SchedulerPaused
	close(s.stopCh)
	s.mu.Unlock()

	<-s.doneCh
}

// State returns the current scheduler state.
func (s *MigrationScheduler) State() SchedulerState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Checkpoint returns a copy of the current migration checkpoint.
func (s *MigrationScheduler) Checkpoint() MigrationCheckpoint {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.checkpoint
}

// SchedulerProgress holds a snapshot of the migration scheduler's progress.
type SchedulerProgress struct {
	AccountsMigrated uint64
	StorageMigrated  uint64
	ErrorCount       uint64
	LastError        error
	State            SchedulerState
	Elapsed          time.Duration
}

// Progress returns a snapshot of the current migration progress.
func (s *MigrationScheduler) Progress() SchedulerProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	var elapsed time.Duration
	if !s.startTime.IsZero() {
		elapsed = time.Since(s.startTime)
	}
	return SchedulerProgress{
		AccountsMigrated: s.accountsDone,
		StorageMigrated:  s.storageDone,
		ErrorCount:       s.errCount,
		LastError:        s.lastError,
		State:            s.state,
		Elapsed:          elapsed,
	}
}

// run is the main migration loop executed in a background goroutine.
func (s *MigrationScheduler) run() {
	defer close(s.doneCh)

	ticker := time.NewTicker(s.config.TickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			done := s.tick()
			if done {
				s.mu.Lock()
				s.state = SchedulerDone
				s.mu.Unlock()
				return
			}
		}
	}
}

// tick processes one batch of accounts. Returns true when migration is
// complete (no more accounts).
func (s *MigrationScheduler) tick() bool {
	s.mu.Lock()
	startAddr := s.checkpoint.LastAddress
	limit := s.config.AccountsPerTick
	s.lastTickTime = time.Now()
	s.mu.Unlock()

	iter := s.source.AccountIterator(startAddr)
	count := 0

	for count < limit && iter.Next() {
		addr := iter.Address()

		// Skip if this is the checkpoint address (already migrated).
		if addr == startAddr && s.accountsDone > 0 {
			continue
		}

		if err := s.migrateAccount(addr); err != nil {
			s.mu.Lock()
			s.errCount++
			s.lastError = err
			s.mu.Unlock()
			continue
		}

		s.migrateAccountStorage(addr)

		s.mu.Lock()
		s.accountsDone++
		s.checkpoint.LastAddress = addr
		s.checkpoint.AccountsDone = s.accountsDone
		s.checkpoint.StorageDone = s.storageDone
		s.checkpoint.Timestamp = time.Now()
		s.mu.Unlock()

		count++
	}

	if err := iter.Error(); err != nil {
		s.mu.Lock()
		s.errCount++
		s.lastError = err
		s.mu.Unlock()
	}

	// If we processed fewer accounts than the limit, we have reached the end.
	return count < limit
}

// migrateAccount converts a single MPT account to Verkle tree entries.
// It writes version, balance, nonce, and code hash under the account stem.
func (s *MigrationScheduler) migrateAccount(addr types.Address) error {
	if !s.source.Exist(addr) {
		return nil
	}

	balance := s.source.GetBalance(addr)
	nonce := s.source.GetNonce(addr)
	codeHash := s.source.GetCodeHash(addr)

	acct := &AccountState{
		Nonce:   nonce,
		Balance: new(big.Int),
	}
	if balance != nil {
		acct.Balance.Set(balance)
	}
	copy(acct.CodeHash[:], codeHash[:])

	s.dest.SetAccount(addr, acct)

	// Migrate code chunks if this is a contract account.
	code := s.source.GetCode(addr)
	if len(code) > 0 {
		s.migrateCode(addr, code)
	}

	return nil
}

// migrateCode splits contract code into 31-byte chunks and writes each to
// the Verkle tree under the appropriate code chunk keys.
func (s *MigrationScheduler) migrateCode(addr types.Address, code []byte) {
	const chunkSize = 31 // EIP-6800 code chunk size
	numChunks := (len(code) + chunkSize - 1) / chunkSize

	// Write code size.
	csKey := GetTreeKeyForCodeSize(addr)
	var csVal [ValueSize]byte
	binary.LittleEndian.PutUint64(csVal[:8], uint64(len(code)))
	s.dest.Tree().Put(csKey[:], csVal[:])

	for i := 0; i < numChunks; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > len(code) {
			end = len(code)
		}
		chunk := code[start:end]

		treeKey := GetTreeKeyForCodeChunk(addr, uint64(i))
		var val [ValueSize]byte
		// First byte is the number of leading bytes that are pushdata
		// continuation (set to 0 for simplicity; a full implementation would
		// track PUSH instruction overlap).
		copy(val[1:], chunk)

		s.dest.Tree().Put(treeKey[:], val[:])
	}
}

// migrateAccountStorage migrates all storage slots for an account.
func (s *MigrationScheduler) migrateAccountStorage(addr types.Address) {
	iter := s.source.StorageIterator(addr)
	if iter == nil {
		return
	}

	limit := s.config.StorageSlotsPerAccount
	count := 0

	for count < limit && iter.Next() {
		key := iter.Key()
		value := iter.Value()

		// Skip empty values.
		if value == (types.Hash{}) {
			continue
		}

		s.dest.SetStorage(addr, key, value)

		s.mu.Lock()
		s.storageDone++
		s.mu.Unlock()
		count++
	}

	if err := iter.Error(); err != nil {
		s.mu.Lock()
		s.errCount++
		s.lastError = err
		s.mu.Unlock()
	}
}
