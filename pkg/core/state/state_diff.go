// Package state manages Ethereum world state.
//
// state_diff.go implements a state diff generator that computes the
// differences between two state snapshots, tracking balance, nonce, code,
// and storage changes per account.
package state

import (
	"math/big"
	"sort"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// BalanceChange records a balance transition.
type BalanceChange struct {
	From *big.Int
	To   *big.Int
}

// NonceChange records a nonce transition.
type NonceChange struct {
	From uint64
	To   uint64
}

// CodeChange records a code transition.
type CodeChange struct {
	From []byte
	To   []byte
}

// StorageChange records a single storage slot transition.
type StorageChange struct {
	Key  types.Hash
	From types.Hash
	To   types.Hash
}

// AccountDiff aggregates all changes for a single account.
type AccountDiff struct {
	Address        types.Address
	BalanceChange  *BalanceChange
	NonceChange    *NonceChange
	CodeChange     *CodeChange
	StorageChanges []StorageChange
}

// BlockStateDiff represents the full set of state changes in a block.
type BlockStateDiff struct {
	BlockNumber  uint64
	BlockHash    types.Hash
	AccountDiffs []AccountDiff
}

// accountDiffBuilder accumulates changes for a single account.
type accountDiffBuilder struct {
	balanceChange  *BalanceChange
	nonceChange    *NonceChange
	codeChange     *CodeChange
	storageChanges map[types.Hash]StorageChange
}

// StateDiffBuilder accumulates state changes and produces a BlockStateDiff.
type StateDiffBuilder struct {
	mu          sync.Mutex
	blockNumber uint64
	blockHash   types.Hash
	accounts    map[types.Address]*accountDiffBuilder
}

// NewStateDiffBuilder creates a builder for a diff at the given block.
func NewStateDiffBuilder(blockNumber uint64, blockHash types.Hash) *StateDiffBuilder {
	return &StateDiffBuilder{
		blockNumber: blockNumber,
		blockHash:   blockHash,
		accounts:    make(map[types.Address]*accountDiffBuilder),
	}
}

// getOrCreate returns the accountDiffBuilder for addr, creating one if needed.
// Caller must hold s.mu.
func (s *StateDiffBuilder) getOrCreate(addr types.Address) *accountDiffBuilder {
	b, ok := s.accounts[addr]
	if !ok {
		b = &accountDiffBuilder{
			storageChanges: make(map[types.Hash]StorageChange),
		}
		s.accounts[addr] = b
	}
	return b
}

// RecordBalanceChange records a balance change for the given address.
// If from == to the change is still recorded (allows explicit no-ops).
func (s *StateDiffBuilder) RecordBalanceChange(addr types.Address, from, to *big.Int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b := s.getOrCreate(addr)
	b.balanceChange = &BalanceChange{
		From: new(big.Int).Set(from),
		To:   new(big.Int).Set(to),
	}
}

// RecordNonceChange records a nonce change for the given address.
func (s *StateDiffBuilder) RecordNonceChange(addr types.Address, from, to uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b := s.getOrCreate(addr)
	b.nonceChange = &NonceChange{From: from, To: to}
}

// RecordCodeChange records a code change for the given address.
func (s *StateDiffBuilder) RecordCodeChange(addr types.Address, from, to []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b := s.getOrCreate(addr)
	fromCopy := make([]byte, len(from))
	copy(fromCopy, from)
	toCopy := make([]byte, len(to))
	copy(toCopy, to)
	b.codeChange = &CodeChange{From: fromCopy, To: toCopy}
}

// RecordStorageChange records a storage slot change for the given address.
// Multiple calls for the same key overwrite the previous entry.
func (s *StateDiffBuilder) RecordStorageChange(addr types.Address, key, from, to types.Hash) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b := s.getOrCreate(addr)
	b.storageChanges[key] = StorageChange{Key: key, From: from, To: to}
}

// Build constructs the final BlockStateDiff from accumulated changes. The
// result is sorted by address, and storage changes within each account are
// sorted by key.
func (s *StateDiffBuilder) Build() *BlockStateDiff {
	s.mu.Lock()
	defer s.mu.Unlock()

	diff := &BlockStateDiff{
		BlockNumber:  s.blockNumber,
		BlockHash:    s.blockHash,
		AccountDiffs: make([]AccountDiff, 0, len(s.accounts)),
	}

	for addr, b := range s.accounts {
		ad := AccountDiff{
			Address:       addr,
			BalanceChange: b.balanceChange,
			NonceChange:   b.nonceChange,
			CodeChange:    b.codeChange,
		}
		if len(b.storageChanges) > 0 {
			ad.StorageChanges = make([]StorageChange, 0, len(b.storageChanges))
			for _, sc := range b.storageChanges {
				ad.StorageChanges = append(ad.StorageChanges, sc)
			}
			sort.Slice(ad.StorageChanges, func(i, j int) bool {
				return hashLess(ad.StorageChanges[i].Key, ad.StorageChanges[j].Key)
			})
		}
		diff.AccountDiffs = append(diff.AccountDiffs, ad)
	}

	sort.Slice(diff.AccountDiffs, func(i, j int) bool {
		a, b := diff.AccountDiffs[i].Address, diff.AccountDiffs[j].Address
		for k := range a {
			if a[k] < b[k] {
				return true
			}
			if a[k] > b[k] {
				return false
			}
		}
		return false
	})

	return diff
}

// IsEmpty returns true if no changes have been recorded.
func (s *StateDiffBuilder) IsEmpty() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.accounts) == 0
}

// AffectedAddresses returns the sorted list of addresses that have at least
// one recorded change.
func (s *StateDiffBuilder) AffectedAddresses() []types.Address {
	s.mu.Lock()
	defer s.mu.Unlock()

	addrs := make([]types.Address, 0, len(s.accounts))
	for addr := range s.accounts {
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool {
		a, b := addrs[i], addrs[j]
		for k := range a {
			if a[k] < b[k] {
				return true
			}
			if a[k] > b[k] {
				return false
			}
		}
		return false
	})
	return addrs
}

// hashLess returns true if a < b in byte-lexicographic order.
func hashLess(a, b types.Hash) bool {
	for i := range a {
		if a[i] < b[i] {
			return true
		}
		if a[i] > b[i] {
			return false
		}
	}
	return false
}
