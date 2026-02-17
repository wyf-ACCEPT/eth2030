// Package bal implements Block Access Lists (EIP-7928) for recording
// state accesses during block execution and enabling parallel transaction scheduling.
package bal

import (
	"math/big"

	"github.com/eth2028/eth2028/core/types"
)

// BlockAccessList contains all state access entries for a block.
type BlockAccessList struct {
	Entries []AccessEntry
}

// AccessEntry records all state accesses for a single execution phase.
// AccessIndex 0 means pre-execution, 1..n maps to transaction indices,
// and n+1 means post-execution.
type AccessEntry struct {
	Address        types.Address
	AccessIndex    uint64
	StorageReads   []StorageAccess
	StorageChanges []StorageChange
	BalanceChange  *BalanceChange
	NonceChange    *NonceChange
	CodeChange     *CodeChange
}

// StorageAccess records a storage slot read.
type StorageAccess struct {
	Slot  types.Hash
	Value types.Hash
}

// StorageChange records a storage slot modification.
type StorageChange struct {
	Slot     types.Hash
	OldValue types.Hash
	NewValue types.Hash
}

// BalanceChange records a balance modification.
type BalanceChange struct {
	OldValue *big.Int
	NewValue *big.Int
}

// NonceChange records a nonce modification.
type NonceChange struct {
	OldValue uint64
	NewValue uint64
}

// CodeChange records a code modification.
type CodeChange struct {
	OldCode []byte
	NewCode []byte
}

// NewBlockAccessList creates a new empty BlockAccessList.
func NewBlockAccessList() *BlockAccessList {
	return &BlockAccessList{}
}

// AddEntry appends an access entry to the list.
func (bal *BlockAccessList) AddEntry(e AccessEntry) {
	bal.Entries = append(bal.Entries, e)
}

// Len returns the number of entries in the list.
func (bal *BlockAccessList) Len() int {
	return len(bal.Entries)
}
