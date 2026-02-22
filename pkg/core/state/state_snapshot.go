// state_snapshot.go implements a state snapshot system that provides fast
// account and storage reads, snapshot diff layers for journaling changes,
// snapshot generation from trie data, pruning/flattening of old layers,
// and recovery from incomplete snapshots.
package state

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Snapshot system errors.
var (
	ErrSnapLayerNotFound  = errors.New("state_snapshot: layer not found")
	ErrSnapLayerStale     = errors.New("state_snapshot: layer is stale")
	ErrSnapIncomplete     = errors.New("state_snapshot: incomplete generation")
	ErrSnapCycle          = errors.New("state_snapshot: update would form cycle")
	ErrSnapReadOnly       = errors.New("state_snapshot: read-only snapshot")
)

// SnapshotAccount is a cached account stored in snapshot layers.
type SnapshotAccount struct {
	Nonce    uint64
	Balance  [32]byte // big-endian encoded balance
	Root     types.Hash
	CodeHash types.Hash
}

// IsEmpty returns true if the account has no nonce, zero balance, empty root,
// and empty code hash.
func (sa *SnapshotAccount) IsEmpty() bool {
	return sa.Nonce == 0 &&
		sa.Balance == [32]byte{} &&
		(sa.Root == types.Hash{} || sa.Root == types.EmptyRootHash) &&
		(sa.CodeHash == types.Hash{} || sa.CodeHash == types.EmptyCodeHash)
}

// SnapshotLayer is the interface implemented by all snapshot layers.
type SnapshotLayer interface {
	// Root returns the state root this layer represents.
	Root() types.Hash
	// AccountData retrieves the account at the given hash.
	// Returns nil, nil if the account does not exist in this layer.
	AccountData(accountHash types.Hash) (*SnapshotAccount, error)
	// StorageData retrieves storage at the given hashes.
	StorageData(accountHash, storageHash types.Hash) ([]byte, error)
	// Stale returns whether this layer has been invalidated.
	Stale() bool
}

// SnapshotDiffLayer journals state changes on top of a parent layer.
type SnapshotDiffLayer struct {
	mu     sync.RWMutex
	parent SnapshotLayer
	root   types.Hash
	stale  atomic.Bool

	// Account diffs: hash -> account (nil means deleted).
	accounts map[types.Hash]*SnapshotAccount
	// Storage diffs: account hash -> (slot hash -> value).
	storage map[types.Hash]map[types.Hash][]byte
	// Memory usage estimate.
	memory uint64
}

// NewSnapshotDiffLayer creates a new diff layer on top of a parent.
func NewSnapshotDiffLayer(
	parent SnapshotLayer,
	root types.Hash,
	accounts map[types.Hash]*SnapshotAccount,
	storage map[types.Hash]map[types.Hash][]byte,
) *SnapshotDiffLayer {
	dl := &SnapshotDiffLayer{
		parent:   parent,
		root:     root,
		accounts: accounts,
		storage:  storage,
	}
	// Estimate memory usage.
	dl.memory = uint64(len(accounts)) * 128
	for _, slots := range storage {
		dl.memory += uint64(len(slots)) * 96
	}
	return dl
}

// Root returns the state root of this diff layer.
func (dl *SnapshotDiffLayer) Root() types.Hash {
	return dl.root
}

// Stale returns whether this layer has been invalidated.
func (dl *SnapshotDiffLayer) Stale() bool {
	return dl.stale.Load()
}

// MarkStale marks this layer as stale (invalidated).
func (dl *SnapshotDiffLayer) MarkStale() {
	dl.stale.Store(true)
}

// AccountData retrieves an account, checking locally first then parent.
func (dl *SnapshotDiffLayer) AccountData(accountHash types.Hash) (*SnapshotAccount, error) {
	dl.mu.RLock()
	if dl.stale.Load() {
		dl.mu.RUnlock()
		return nil, ErrSnapLayerStale
	}
	if acc, ok := dl.accounts[accountHash]; ok {
		dl.mu.RUnlock()
		return acc, nil // nil means deleted
	}
	parent := dl.parent
	dl.mu.RUnlock()

	if parent != nil {
		return parent.AccountData(accountHash)
	}
	return nil, nil
}

// StorageData retrieves a storage slot, checking locally first then parent.
func (dl *SnapshotDiffLayer) StorageData(accountHash, storageHash types.Hash) ([]byte, error) {
	dl.mu.RLock()
	if dl.stale.Load() {
		dl.mu.RUnlock()
		return nil, ErrSnapLayerStale
	}
	if slots, ok := dl.storage[accountHash]; ok {
		if val, ok := slots[storageHash]; ok {
			dl.mu.RUnlock()
			return val, nil
		}
	}
	parent := dl.parent
	dl.mu.RUnlock()

	if parent != nil {
		return parent.StorageData(accountHash, storageHash)
	}
	return nil, nil
}

// Parent returns the parent layer.
func (dl *SnapshotDiffLayer) Parent() SnapshotLayer {
	dl.mu.RLock()
	defer dl.mu.RUnlock()
	return dl.parent
}

// Memory returns the estimated memory usage in bytes.
func (dl *SnapshotDiffLayer) Memory() uint64 {
	return dl.memory
}

// AccountCount returns the number of accounts tracked in this diff layer.
func (dl *SnapshotDiffLayer) AccountCount() int {
	dl.mu.RLock()
	defer dl.mu.RUnlock()
	return len(dl.accounts)
}

// StorageCount returns the total number of storage slots across all accounts.
func (dl *SnapshotDiffLayer) StorageCount() int {
	dl.mu.RLock()
	defer dl.mu.RUnlock()
	total := 0
	for _, slots := range dl.storage {
		total += len(slots)
	}
	return total
}

// SnapshotBaseLayer provides the base (disk-equivalent) layer for the snapshot
// tree. In a full implementation this would be backed by a KV store; here it
// uses an in-memory map for simplicity and testability.
type SnapshotBaseLayer struct {
	mu       sync.RWMutex
	root     types.Hash
	accounts map[types.Hash]*SnapshotAccount
	storage  map[types.Hash]map[types.Hash][]byte
	stale    bool
}

// NewSnapshotBaseLayer creates a base layer with the given root hash.
func NewSnapshotBaseLayer(root types.Hash) *SnapshotBaseLayer {
	return &SnapshotBaseLayer{
		root:     root,
		accounts: make(map[types.Hash]*SnapshotAccount),
		storage:  make(map[types.Hash]map[types.Hash][]byte),
	}
}

// Root returns the state root of the base layer.
func (bl *SnapshotBaseLayer) Root() types.Hash {
	return bl.root
}

// Stale returns whether this base layer has been invalidated.
func (bl *SnapshotBaseLayer) Stale() bool {
	bl.mu.RLock()
	defer bl.mu.RUnlock()
	return bl.stale
}

// MarkStale marks this base layer as stale.
func (bl *SnapshotBaseLayer) MarkStale() {
	bl.mu.Lock()
	defer bl.mu.Unlock()
	bl.stale = true
}

// AccountData retrieves an account from the base layer.
func (bl *SnapshotBaseLayer) AccountData(accountHash types.Hash) (*SnapshotAccount, error) {
	bl.mu.RLock()
	defer bl.mu.RUnlock()
	if bl.stale {
		return nil, ErrSnapLayerStale
	}
	acc := bl.accounts[accountHash]
	return acc, nil
}

// StorageData retrieves a storage slot from the base layer.
func (bl *SnapshotBaseLayer) StorageData(accountHash, storageHash types.Hash) ([]byte, error) {
	bl.mu.RLock()
	defer bl.mu.RUnlock()
	if bl.stale {
		return nil, ErrSnapLayerStale
	}
	if slots, ok := bl.storage[accountHash]; ok {
		return slots[storageHash], nil
	}
	return nil, nil
}

// SetAccount stores an account in the base layer.
func (bl *SnapshotBaseLayer) SetAccount(accountHash types.Hash, acc *SnapshotAccount) {
	bl.mu.Lock()
	defer bl.mu.Unlock()
	if acc == nil {
		delete(bl.accounts, accountHash)
	} else {
		bl.accounts[accountHash] = acc
	}
}

// SetStorage stores a storage slot in the base layer.
func (bl *SnapshotBaseLayer) SetStorage(accountHash, storageHash types.Hash, value []byte) {
	bl.mu.Lock()
	defer bl.mu.Unlock()
	if bl.storage[accountHash] == nil {
		bl.storage[accountHash] = make(map[types.Hash][]byte)
	}
	if len(value) == 0 {
		delete(bl.storage[accountHash], storageHash)
	} else {
		cp := make([]byte, len(value))
		copy(cp, value)
		bl.storage[accountHash][storageHash] = cp
	}
}

// SnapshotTree manages layered snapshots for fast state access.
type SnapshotTree struct {
	mu     sync.RWMutex
	layers map[types.Hash]SnapshotLayer
}

// NewSnapshotTree creates a snapshot tree with a base layer at the given root.
func NewSnapshotTree(root types.Hash) *SnapshotTree {
	base := NewSnapshotBaseLayer(root)
	return &SnapshotTree{
		layers: map[types.Hash]SnapshotLayer{root: base},
	}
}

// Snapshot retrieves the snapshot layer for the given state root.
func (st *SnapshotTree) Snapshot(root types.Hash) SnapshotLayer {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.layers[root]
}

// Update adds a new diff layer on top of the given parent root.
func (st *SnapshotTree) Update(
	blockRoot, parentRoot types.Hash,
	accounts map[types.Hash]*SnapshotAccount,
	storage map[types.Hash]map[types.Hash][]byte,
) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	if blockRoot == parentRoot {
		return ErrSnapCycle
	}
	parent, ok := st.layers[parentRoot]
	if !ok {
		return ErrSnapLayerNotFound
	}
	diff := NewSnapshotDiffLayer(parent, blockRoot, accounts, storage)
	st.layers[blockRoot] = diff
	return nil
}

// Flatten merges the bottommost diff layers into the base layer, keeping
// at most `keepLayers` diff layers above the base. Returns the number of
// layers flattened.
func (st *SnapshotTree) Flatten(root types.Hash, keepLayers int) (int, error) {
	st.mu.Lock()
	defer st.mu.Unlock()

	snap, ok := st.layers[root]
	if !ok {
		return 0, ErrSnapLayerNotFound
	}

	// Build the chain from root down to base.
	chain := buildChain(snap)
	if len(chain) <= 1 {
		return 0, nil // nothing to flatten
	}

	// chain[0] = topmost, chain[len-1] = base layer.
	diffCount := len(chain) - 1
	if diffCount <= keepLayers {
		return 0, nil
	}

	flattened := 0
	toFlatten := diffCount - keepLayers

	for i := 0; i < toFlatten; i++ {
		// Re-build chain each iteration since structure changes.
		chain = buildChain(st.layers[root])
		if len(chain) < 2 {
			break
		}
		bottomDiffIdx := len(chain) - 2
		bottomDiff, ok := chain[bottomDiffIdx].(*SnapshotDiffLayer)
		if !ok {
			break
		}
		base, ok := chain[len(chain)-1].(*SnapshotBaseLayer)
		if !ok {
			break
		}

		// Merge diff into base.
		mergeIntoBase(base, bottomDiff)
		base.root = bottomDiff.root

		// Update references: any layer pointing to bottomDiff now points to base.
		for _, layer := range st.layers {
			if dl, ok := layer.(*SnapshotDiffLayer); ok {
				dl.mu.Lock()
				if dl.parent == bottomDiff {
					dl.parent = base
				}
				dl.mu.Unlock()
			}
		}

		// Remove old entries.
		bottomDiff.MarkStale()
		delete(st.layers, bottomDiff.root)
		st.layers[base.root] = base

		flattened++
	}
	return flattened, nil
}

// Prune removes stale layers from the tree that are no longer reachable.
func (st *SnapshotTree) Prune() int {
	st.mu.Lock()
	defer st.mu.Unlock()

	pruned := 0
	for root, layer := range st.layers {
		if layer.Stale() {
			delete(st.layers, root)
			pruned++
		}
	}
	return pruned
}

// Size returns the number of layers in the tree.
func (st *SnapshotTree) Size() int {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return len(st.layers)
}

// GenerateFromTrie generates a base layer snapshot from account data.
// The accounts map provides accountHash -> SnapshotAccount, and storageData
// provides accountHash -> (slotHash -> value).
func GenerateFromTrie(
	root types.Hash,
	accounts map[types.Hash]*SnapshotAccount,
	storageData map[types.Hash]map[types.Hash][]byte,
) *SnapshotBaseLayer {
	base := NewSnapshotBaseLayer(root)
	for hash, acc := range accounts {
		base.accounts[hash] = acc
	}
	for accHash, slots := range storageData {
		base.storage[accHash] = make(map[types.Hash][]byte)
		for slotHash, val := range slots {
			cp := make([]byte, len(val))
			copy(cp, val)
			base.storage[accHash][slotHash] = cp
		}
	}
	return base
}

// RecoverSnapshot attempts to recover from an incomplete snapshot by verifying
// each account hash against the provided list. Accounts not in the valid set
// are removed. Returns the number of removed accounts.
func RecoverSnapshot(base *SnapshotBaseLayer, validAccounts map[types.Hash]struct{}) int {
	base.mu.Lock()
	defer base.mu.Unlock()

	removed := 0
	for hash := range base.accounts {
		if _, ok := validAccounts[hash]; !ok {
			delete(base.accounts, hash)
			delete(base.storage, hash)
			removed++
		}
	}
	return removed
}

// HashAddress computes the keccak256 hash of an address for snapshot lookups.
func HashAddress(addr types.Address) types.Hash {
	return crypto.Keccak256Hash(addr[:])
}

// HashStorageKey computes the keccak256 hash of a storage key for lookups.
func HashStorageKey(key types.Hash) types.Hash {
	return crypto.Keccak256Hash(key[:])
}

// buildChain walks the layer ancestry from top to base.
func buildChain(snap SnapshotLayer) []SnapshotLayer {
	var chain []SnapshotLayer
	for current := snap; current != nil; {
		chain = append(chain, current)
		switch c := current.(type) {
		case *SnapshotDiffLayer:
			current = c.Parent()
		default:
			current = nil
		}
	}
	return chain
}

// mergeIntoBase merges a diff layer's data into the base layer.
func mergeIntoBase(base *SnapshotBaseLayer, diff *SnapshotDiffLayer) {
	diff.mu.RLock()
	defer diff.mu.RUnlock()

	for hash, acc := range diff.accounts {
		if acc == nil {
			delete(base.accounts, hash)
			delete(base.storage, hash)
		} else {
			base.accounts[hash] = acc
		}
	}
	for accHash, slots := range diff.storage {
		if base.storage[accHash] == nil {
			base.storage[accHash] = make(map[types.Hash][]byte)
		}
		for slotHash, val := range slots {
			if len(val) == 0 {
				delete(base.storage[accHash], slotHash)
			} else {
				cp := make([]byte, len(val))
				copy(cp, val)
				base.storage[accHash][slotHash] = cp
			}
		}
	}
}
