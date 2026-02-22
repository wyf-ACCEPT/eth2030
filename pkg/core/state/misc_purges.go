// misc_purges.go implements state purge operations for removing obsolete state.
// Part of the I+ era roadmap (EL Sustainability): periodically removing empty
// accounts, self-destructed contracts, and expired storage slots reduces state
// bloat and improves trie performance.
package state

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// Purge target flags for PurgeConfig.
const (
	PurgeTargetEmptyAccounts  uint32 = 1 << iota // accounts with zero balance, nonce, empty code
	PurgeTargetSelfDestructed                    // self-destructed contracts
	PurgeTargetExpiredStorage                    // storage slots past cutoff block
	PurgeTargetAll            = PurgeTargetEmptyAccounts | PurgeTargetSelfDestructed | PurgeTargetExpiredStorage
)

// Purge errors.
var (
	ErrPurgeNilState   = errors.New("purge: nil state database")
	ErrPurgeNoTargets  = errors.New("purge: no purge targets selected")
	ErrPurgeDryRun     = errors.New("purge: dry run does not modify state")
	ErrPurgeCutoffZero = errors.New("purge: cutoff block cannot be zero")
)

// PurgeConfig controls which state elements are purged and how.
type PurgeConfig struct {
	// Targets is a bitmask of purge targets to apply.
	Targets uint32

	// DryRun if true performs estimation without modifying state.
	DryRun bool

	// BatchSize controls how many accounts are processed per batch.
	// Zero means no limit (process all at once).
	BatchSize int

	// PreserveAddresses is a set of addresses that should never be purged
	// (e.g., system contracts, precompiles).
	PreserveAddresses map[types.Address]bool
}

// DefaultPurgeConfig returns a config that purges all targets.
func DefaultPurgeConfig() PurgeConfig {
	return PurgeConfig{
		Targets:           PurgeTargetAll,
		DryRun:            false,
		BatchSize:         0,
		PreserveAddresses: make(map[types.Address]bool),
	}
}

// HasTarget returns true if the config includes the specified target.
func (pc PurgeConfig) HasTarget(target uint32) bool {
	return pc.Targets&target != 0
}

// PurgeStats holds before/after metrics for a purge operation.
type PurgeStats struct {
	// Counts of purged items.
	EmptyAccountsPurged  int
	SelfDestructedPurged int
	ExpiredSlotsPurged   int

	// Before/after state metrics.
	AccountsBefore     int
	AccountsAfter      int
	StorageSlotsBefore int
	StorageSlotsAfter  int

	// Root hashes.
	RootBefore types.Hash
	RootAfter  types.Hash

	// Timing.
	Duration time.Duration

	// Whether this was a dry run.
	DryRun bool
}

// TotalPurged returns the total number of items purged.
func (ps PurgeStats) TotalPurged() int {
	return ps.EmptyAccountsPurged + ps.SelfDestructedPurged + ps.ExpiredSlotsPurged
}

// Summary returns a human-readable summary of the purge operation.
func (ps PurgeStats) Summary() string {
	mode := "applied"
	if ps.DryRun {
		mode = "dry-run"
	}
	return fmt.Sprintf("purge(%s): empty=%d selfdestructed=%d expired_slots=%d "+
		"accounts %d->%d slots %d->%d duration=%v",
		mode,
		ps.EmptyAccountsPurged, ps.SelfDestructedPurged, ps.ExpiredSlotsPurged,
		ps.AccountsBefore, ps.AccountsAfter,
		ps.StorageSlotsBefore, ps.StorageSlotsAfter,
		ps.Duration)
}

// StatePurger performs purge operations on a MemoryStateDB.
type StatePurger struct {
	mu     sync.Mutex
	config PurgeConfig
}

// NewStatePurger creates a new purger with the given config.
func NewStatePurger(config PurgeConfig) *StatePurger {
	return &StatePurger{config: config}
}

// PurgeEmptyAccounts removes accounts with zero balance, zero nonce, and empty
// code hash. Returns the count of purged accounts, the new state root, and
// any error.
func (sp *StatePurger) PurgeEmptyAccounts(db *MemoryStateDB) (int, types.Hash, error) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if db == nil {
		return 0, types.Hash{}, ErrPurgeNilState
	}

	if sp.config.DryRun {
		count := sp.countEmptyAccounts(db)
		root := db.GetRoot()
		return count, root, ErrPurgeDryRun
	}

	count := 0
	toDelete := sp.findEmptyAccounts(db)

	for _, addr := range toDelete {
		delete(db.stateObjects, addr)
		count++
	}

	newRoot := db.GetRoot()
	return count, newRoot, nil
}

// PurgeSelfDestructed removes accounts that have been marked as self-destructed.
// Returns the count, new root, and any error.
func (sp *StatePurger) PurgeSelfDestructed(db *MemoryStateDB) (int, types.Hash, error) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if db == nil {
		return 0, types.Hash{}, ErrPurgeNilState
	}

	if sp.config.DryRun {
		count := sp.countSelfDestructed(db)
		root := db.GetRoot()
		return count, root, ErrPurgeDryRun
	}

	count := 0
	toDelete := sp.findSelfDestructed(db)

	for _, addr := range toDelete {
		delete(db.stateObjects, addr)
		count++
	}

	newRoot := db.GetRoot()
	return count, newRoot, nil
}

// PurgeExpiredStorage removes storage slots that belong to accounts whose
// last access block is before the cutoff block. This simulates slot-level
// state expiry. Returns the number of slots purged and any error.
func (sp *StatePurger) PurgeExpiredStorage(db *MemoryStateDB, cutoffBlock uint64) (int, error) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if db == nil {
		return 0, ErrPurgeNilState
	}
	if cutoffBlock == 0 {
		return 0, ErrPurgeCutoffZero
	}

	if sp.config.DryRun {
		count := sp.countExpiredSlots(db, cutoffBlock)
		return count, ErrPurgeDryRun
	}

	// For in-memory state, we approximate "expired storage" by clearing
	// storage from accounts with nonce below the cutoff block. This is a
	// simplified model; production would use per-slot access timestamps.
	slotsPurged := 0
	for addr, obj := range db.stateObjects {
		if sp.config.PreserveAddresses[addr] {
			continue
		}
		if obj.account.Nonce < cutoffBlock {
			purgedCount := len(obj.dirtyStorage) + len(obj.committedStorage)
			obj.dirtyStorage = make(map[types.Hash]types.Hash)
			obj.committedStorage = make(map[types.Hash]types.Hash)
			obj.account.Root = types.EmptyRootHash
			slotsPurged += purgedCount
		}
	}

	return slotsPurged, nil
}

// DryRunPurge estimates what a full purge would remove without modifying state.
// Returns PurgeStats with estimation counts.
func (sp *StatePurger) DryRunPurge(db *MemoryStateDB, cutoffBlock uint64) (PurgeStats, error) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if db == nil {
		return PurgeStats{}, ErrPurgeNilState
	}

	start := time.Now()

	stats := PurgeStats{
		DryRun:     true,
		RootBefore: db.GetRoot(),
	}

	// Count accounts and slots before.
	stats.AccountsBefore = len(db.stateObjects)
	for _, obj := range db.stateObjects {
		stats.StorageSlotsBefore += len(obj.committedStorage) + len(obj.dirtyStorage)
	}

	if sp.config.HasTarget(PurgeTargetEmptyAccounts) {
		stats.EmptyAccountsPurged = sp.countEmptyAccounts(db)
	}
	if sp.config.HasTarget(PurgeTargetSelfDestructed) {
		stats.SelfDestructedPurged = sp.countSelfDestructed(db)
	}
	if sp.config.HasTarget(PurgeTargetExpiredStorage) && cutoffBlock > 0 {
		stats.ExpiredSlotsPurged = sp.countExpiredSlots(db, cutoffBlock)
	}

	stats.AccountsAfter = stats.AccountsBefore - stats.EmptyAccountsPurged - stats.SelfDestructedPurged
	if stats.AccountsAfter < 0 {
		stats.AccountsAfter = 0
	}
	stats.StorageSlotsAfter = stats.StorageSlotsBefore - stats.ExpiredSlotsPurged
	if stats.StorageSlotsAfter < 0 {
		stats.StorageSlotsAfter = 0
	}

	stats.RootAfter = stats.RootBefore // dry run: root unchanged
	stats.Duration = time.Since(start)

	return stats, nil
}

// FullPurge runs all configured purge targets and returns comprehensive stats.
func (sp *StatePurger) FullPurge(db *MemoryStateDB, cutoffBlock uint64) (PurgeStats, error) {
	if db == nil {
		return PurgeStats{}, ErrPurgeNilState
	}

	if sp.config.Targets == 0 {
		return PurgeStats{}, ErrPurgeNoTargets
	}

	start := time.Now()

	stats := PurgeStats{
		DryRun:     sp.config.DryRun,
		RootBefore: db.GetRoot(),
	}

	// Count before.
	stats.AccountsBefore = len(db.stateObjects)
	for _, obj := range db.stateObjects {
		stats.StorageSlotsBefore += len(obj.committedStorage) + len(obj.dirtyStorage)
	}

	if sp.config.DryRun {
		dryStats, err := sp.DryRunPurge(db, cutoffBlock)
		if err != nil {
			return dryStats, err
		}
		dryStats.Duration = time.Since(start)
		return dryStats, nil
	}

	if sp.config.HasTarget(PurgeTargetEmptyAccounts) {
		count, _, err := sp.PurgeEmptyAccounts(db)
		if err != nil && err != ErrPurgeDryRun {
			return stats, fmt.Errorf("purge empty accounts: %w", err)
		}
		stats.EmptyAccountsPurged = count
	}

	if sp.config.HasTarget(PurgeTargetSelfDestructed) {
		count, _, err := sp.PurgeSelfDestructed(db)
		if err != nil && err != ErrPurgeDryRun {
			return stats, fmt.Errorf("purge self-destructed: %w", err)
		}
		stats.SelfDestructedPurged = count
	}

	if sp.config.HasTarget(PurgeTargetExpiredStorage) && cutoffBlock > 0 {
		count, err := sp.PurgeExpiredStorage(db, cutoffBlock)
		if err != nil && err != ErrPurgeDryRun {
			return stats, fmt.Errorf("purge expired storage: %w", err)
		}
		stats.ExpiredSlotsPurged = count
	}

	// Count after.
	stats.AccountsAfter = len(db.stateObjects)
	for _, obj := range db.stateObjects {
		stats.StorageSlotsAfter += len(obj.committedStorage) + len(obj.dirtyStorage)
	}

	stats.RootAfter = db.GetRoot()
	stats.Duration = time.Since(start)

	return stats, nil
}

// --- internal helpers ---

// findEmptyAccounts returns addresses of empty accounts eligible for purging.
func (sp *StatePurger) findEmptyAccounts(db *MemoryStateDB) []types.Address {
	var addrs []types.Address
	processed := 0
	for addr, obj := range db.stateObjects {
		if sp.config.PreserveAddresses[addr] {
			continue
		}
		if isPurgeableEmpty(obj) {
			addrs = append(addrs, addr)
		}
		processed++
		if sp.config.BatchSize > 0 && processed >= sp.config.BatchSize {
			break
		}
	}
	return addrs
}

// findSelfDestructed returns addresses of self-destructed accounts.
func (sp *StatePurger) findSelfDestructed(db *MemoryStateDB) []types.Address {
	var addrs []types.Address
	processed := 0
	for addr, obj := range db.stateObjects {
		if sp.config.PreserveAddresses[addr] {
			continue
		}
		if obj.selfDestructed {
			addrs = append(addrs, addr)
		}
		processed++
		if sp.config.BatchSize > 0 && processed >= sp.config.BatchSize {
			break
		}
	}
	return addrs
}

// countEmptyAccounts returns the count of empty accounts.
func (sp *StatePurger) countEmptyAccounts(db *MemoryStateDB) int {
	count := 0
	for addr, obj := range db.stateObjects {
		if sp.config.PreserveAddresses[addr] {
			continue
		}
		if isPurgeableEmpty(obj) {
			count++
		}
	}
	return count
}

// countSelfDestructed returns the count of self-destructed accounts.
func (sp *StatePurger) countSelfDestructed(db *MemoryStateDB) int {
	count := 0
	for addr, obj := range db.stateObjects {
		if sp.config.PreserveAddresses[addr] {
			continue
		}
		if obj.selfDestructed {
			count++
		}
	}
	return count
}

// countExpiredSlots returns an estimate of storage slots to purge.
func (sp *StatePurger) countExpiredSlots(db *MemoryStateDB, cutoffBlock uint64) int {
	count := 0
	for addr, obj := range db.stateObjects {
		if sp.config.PreserveAddresses[addr] {
			continue
		}
		if obj.account.Nonce < cutoffBlock {
			count += len(obj.dirtyStorage) + len(obj.committedStorage)
		}
	}
	return count
}

// isPurgeableEmpty checks if the account is empty per EIP-161:
// zero nonce, zero balance, empty code hash, and not self-destructed.
// Self-destructed accounts are handled separately by PurgeSelfDestructed.
func isPurgeableEmpty(obj *stateObject) bool {
	if obj == nil {
		return true
	}
	if obj.selfDestructed {
		return false
	}
	return obj.account.Nonce == 0 &&
		(obj.account.Balance == nil || obj.account.Balance.Sign() == 0) &&
		(len(obj.account.CodeHash) == 0 || types.BytesToHash(obj.account.CodeHash) == types.EmptyCodeHash) &&
		len(obj.code) == 0
}

// SetConfig updates the purger's config. Useful for switching between
// dry-run and apply modes.
func (sp *StatePurger) SetConfig(config PurgeConfig) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	sp.config = config
}

// Config returns the current purge config.
func (sp *StatePurger) Config() PurgeConfig {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return sp.config
}

// EstimateGasSavings estimates the gas savings from purging, based on the
// reduction in trie nodes that need to be accessed. Returns an approximate
// gas savings value.
func EstimateGasSavings(stats PurgeStats) uint64 {
	// Each purged account saves ~2100 gas per access (cold SLOAD cost equivalent).
	// Each purged slot saves ~2100 gas per access.
	accountSavings := uint64(stats.EmptyAccountsPurged+stats.SelfDestructedPurged) * 2100
	slotSavings := uint64(stats.ExpiredSlotsPurged) * 2100
	return accountSavings + slotSavings
}

// PreserveSystemContracts adds common system contract addresses to the preserve
// list. These addresses should never be purged.
func PreserveSystemContracts(config *PurgeConfig) {
	if config.PreserveAddresses == nil {
		config.PreserveAddresses = make(map[types.Address]bool)
	}
	// Beacon deposit contract (mainnet).
	config.PreserveAddresses[types.HexToAddress("0x00000000219ab540356cBB839Cbe05303d7705Fa")] = true
	// System contracts at address 0x0...0 through 0x0...9 (precompiles).
	for i := byte(1); i <= 9; i++ {
		var addr types.Address
		addr[19] = i
		config.PreserveAddresses[addr] = true
	}
}
