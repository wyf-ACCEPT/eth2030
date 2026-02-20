// state_migration_overlay.go implements the MPT-to-Verkle overlay database,
// epoch-based migration, deadline tracking, and conversion progress for the
// transition from MPT tries to Verkle trees.
package verkle

import (
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// Migration errors.
var (
	ErrMigrationNotStarted  = errors.New("verkle: migration not started")
	ErrMigrationDeadline    = errors.New("verkle: migration deadline exceeded")
	ErrOverlayReadFailed    = errors.New("verkle: overlay read failed")
	ErrAccountNotFound      = errors.New("verkle: account not found in either tree")
)

// MPTToVerkleConverter reads from an MPT-based source and writes converted
// data into a Verkle tree. It tracks conversion progress including accounts,
// storage slots, and code chunks converted.
type MPTToVerkleConverter struct {
	mu       sync.Mutex
	source   StateMigrationSource
	dest     *VerkleStateDB
	progress ConverterProgress
}

// ConverterProgress tracks the conversion progress from MPT to Verkle.
type ConverterProgress struct {
	AccountsConverted    uint64
	StorageSlotsConverted uint64
	CodeChunksConverted  uint64
	StartTime            time.Time
	LastUpdateTime       time.Time
	Complete             bool
}

// NewMPTToVerkleConverter creates a new converter that reads from the MPT
// source and writes to the Verkle destination.
func NewMPTToVerkleConverter(source StateMigrationSource, dest *VerkleStateDB) *MPTToVerkleConverter {
	return &MPTToVerkleConverter{
		source: source,
		dest:   dest,
		progress: ConverterProgress{
			StartTime: time.Now(),
		},
	}
}

// ConvertAccount migrates a single account from MPT to Verkle. It reads
// balance, nonce, and code hash from the source and writes them to the
// Verkle tree.
func (c *MPTToVerkleConverter) ConvertAccount(addr types.Address) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.source.Exist(addr) {
		return ErrAccountNotFound
	}

	acct := &AccountState{
		Nonce:   c.source.GetNonce(addr),
		Balance: new(big.Int),
	}
	bal := c.source.GetBalance(addr)
	if bal != nil {
		acct.Balance.Set(bal)
	}
	codeHash := c.source.GetCodeHash(addr)
	copy(acct.CodeHash[:], codeHash[:])

	c.dest.SetAccount(addr, acct)
	c.progress.AccountsConverted++
	c.progress.LastUpdateTime = time.Now()
	return nil
}

// ConvertStorageSlot migrates a single storage slot for an account.
func (c *MPTToVerkleConverter) ConvertStorageSlot(addr types.Address, key, value types.Hash) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.dest.SetStorage(addr, key, value)
	c.progress.StorageSlotsConverted++
	c.progress.LastUpdateTime = time.Now()
}

// ConvertCodeChunks splits code into 31-byte chunks and writes them to the
// Verkle tree. Returns the number of chunks written.
func (c *MPTToVerkleConverter) ConvertCodeChunks(addr types.Address, code []byte) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(code) == 0 {
		return 0
	}

	const chunkSize = 31
	numChunks := (len(code) + chunkSize - 1) / chunkSize

	for i := 0; i < numChunks; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > len(code) {
			end = len(code)
		}
		treeKey := GetTreeKeyForCodeChunk(addr, uint64(i))
		var val [ValueSize]byte
		copy(val[1:], code[start:end])
		c.dest.Tree().Put(treeKey[:], val[:])
		c.progress.CodeChunksConverted++
	}

	c.progress.LastUpdateTime = time.Now()
	return numChunks
}

// Progress returns the current migration progress.
func (c *MPTToVerkleConverter) Progress() ConverterProgress {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.progress
}

// MarkComplete marks the migration as complete.
func (c *MPTToVerkleConverter) MarkComplete() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.progress.Complete = true
	c.progress.LastUpdateTime = time.Now()
}

// EpochBasedMigration migrates N accounts per epoch to spread the cost
// of migration across multiple blocks.
type EpochBasedMigration struct {
	mu               sync.Mutex
	converter        *MPTToVerkleConverter
	accountsPerEpoch int
	currentEpoch     uint64
	totalEpochs      uint64
	epochAccounts    map[uint64][]types.Address
}

// NewEpochBasedMigration creates a new epoch-based migration scheduler.
func NewEpochBasedMigration(converter *MPTToVerkleConverter, accountsPerEpoch int) *EpochBasedMigration {
	return &EpochBasedMigration{
		converter:        converter,
		accountsPerEpoch: accountsPerEpoch,
		epochAccounts:    make(map[uint64][]types.Address),
	}
}

// ScheduleAccounts assigns accounts to specific epochs for migration.
func (e *EpochBasedMigration) ScheduleAccounts(epoch uint64, accounts []types.Address) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.epochAccounts[epoch] = append(e.epochAccounts[epoch], accounts...)
	if epoch >= e.totalEpochs {
		e.totalEpochs = epoch + 1
	}
}

// ProcessEpoch migrates all accounts scheduled for the given epoch.
// Returns the number of accounts successfully migrated.
func (e *EpochBasedMigration) ProcessEpoch(epoch uint64) (int, error) {
	e.mu.Lock()
	accounts := e.epochAccounts[epoch]
	e.currentEpoch = epoch
	e.mu.Unlock()

	migrated := 0
	for _, addr := range accounts {
		if err := e.converter.ConvertAccount(addr); err != nil {
			if errors.Is(err, ErrAccountNotFound) {
				continue
			}
			return migrated, err
		}
		migrated++
	}
	return migrated, nil
}

// CurrentEpoch returns the most recently processed epoch.
func (e *EpochBasedMigration) CurrentEpoch() uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.currentEpoch
}

// TotalEpochs returns the total number of scheduled epochs.
func (e *EpochBasedMigration) TotalEpochs() uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.totalEpochs
}

// AccountsPerEpoch returns the target accounts per epoch.
func (e *EpochBasedMigration) AccountsPerEpoch() int {
	return e.accountsPerEpoch
}

// OverlayDB provides a transparent overlay that reads from the Verkle tree
// first and falls back to the MPT source during the transition period.
type OverlayDB struct {
	mu       sync.RWMutex
	verkle   *VerkleStateDB
	mpt      StateMigrationSource
	migrated map[types.Address]bool
}

// NewOverlayDB creates a new overlay database that reads from Verkle first
// and falls back to MPT.
func NewOverlayDB(verkle *VerkleStateDB, mpt StateMigrationSource) *OverlayDB {
	return &OverlayDB{
		verkle:   verkle,
		mpt:      mpt,
		migrated: make(map[types.Address]bool),
	}
}

// MarkMigrated marks an address as having been migrated to the Verkle tree.
func (o *OverlayDB) MarkMigrated(addr types.Address) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.migrated[addr] = true
}

// IsMigrated returns whether an address has been migrated.
func (o *OverlayDB) IsMigrated(addr types.Address) bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.migrated[addr]
}

// GetBalance returns the balance, preferring Verkle for migrated accounts.
func (o *OverlayDB) GetBalance(addr types.Address) *big.Int {
	o.mu.RLock()
	defer o.mu.RUnlock()

	if o.migrated[addr] {
		acct := o.verkle.GetAccount(addr)
		if acct != nil {
			return acct.Balance
		}
	}
	return o.mpt.GetBalance(addr)
}

// GetNonce returns the nonce, preferring Verkle for migrated accounts.
func (o *OverlayDB) GetNonce(addr types.Address) uint64 {
	o.mu.RLock()
	defer o.mu.RUnlock()

	if o.migrated[addr] {
		acct := o.verkle.GetAccount(addr)
		if acct != nil {
			return acct.Nonce
		}
	}
	return o.mpt.GetNonce(addr)
}

// GetCodeHash returns the code hash, preferring Verkle for migrated accounts.
func (o *OverlayDB) GetCodeHash(addr types.Address) types.Hash {
	o.mu.RLock()
	defer o.mu.RUnlock()

	if o.migrated[addr] {
		acct := o.verkle.GetAccount(addr)
		if acct != nil {
			return acct.CodeHash
		}
	}
	return o.mpt.GetCodeHash(addr)
}

// GetStorage returns a storage value, preferring Verkle for migrated accounts.
func (o *OverlayDB) GetStorage(addr types.Address, key types.Hash) types.Hash {
	o.mu.RLock()
	defer o.mu.RUnlock()

	if o.migrated[addr] {
		return o.verkle.GetStorage(addr, key)
	}
	// Fall back to MPT; not available through the basic interface, return empty.
	return types.Hash{}
}

// Exist returns whether an account exists in either tree.
func (o *OverlayDB) Exist(addr types.Address) bool {
	o.mu.RLock()
	defer o.mu.RUnlock()

	if o.migrated[addr] {
		return o.verkle.GetAccount(addr) != nil
	}
	return o.mpt.Exist(addr)
}

// MigratedCount returns the number of migrated addresses.
func (o *OverlayDB) MigratedCount() int {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return len(o.migrated)
}

// DeadlineTracker ensures the migration finishes before a fork deadline.
type DeadlineTracker struct {
	mu         sync.Mutex
	deadline   time.Time
	forkBlock  uint64
	startBlock uint64
	current    uint64
}

// NewDeadlineTracker creates a tracker for the migration deadline.
func NewDeadlineTracker(deadline time.Time, forkBlock, startBlock uint64) *DeadlineTracker {
	return &DeadlineTracker{
		deadline:   deadline,
		forkBlock:  forkBlock,
		startBlock: startBlock,
		current:    startBlock,
	}
}

// UpdateProgress updates the current block number.
func (d *DeadlineTracker) UpdateProgress(blockNum uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.current = blockNum
}

// IsExpired returns true if the deadline has passed.
func (d *DeadlineTracker) IsExpired() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return time.Now().After(d.deadline)
}

// BlocksRemaining returns the number of blocks remaining before the fork.
func (d *DeadlineTracker) BlocksRemaining() uint64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.current >= d.forkBlock {
		return 0
	}
	return d.forkBlock - d.current
}

// ProgressPercent returns the migration progress as a percentage of blocks.
func (d *DeadlineTracker) ProgressPercent() float64 {
	d.mu.Lock()
	defer d.mu.Unlock()

	totalBlocks := d.forkBlock - d.startBlock
	if totalBlocks == 0 {
		return 100.0
	}
	elapsed := d.current - d.startBlock
	if elapsed >= totalBlocks {
		return 100.0
	}
	return float64(elapsed) / float64(totalBlocks) * 100.0
}

// TimeRemaining returns the duration until the deadline.
func (d *DeadlineTracker) TimeRemaining() time.Duration {
	d.mu.Lock()
	defer d.mu.Unlock()
	rem := time.Until(d.deadline)
	if rem < 0 {
		return 0
	}
	return rem
}
