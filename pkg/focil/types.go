// Package focil implements Fork-Choice enforced Inclusion Lists (EIP-7805).
//
// FOCIL ensures censorship resistance by requiring block builders to include
// transactions specified by a committee of validators. Each committee member
// constructs an inclusion list from pending mempool transactions, and the
// block builder must satisfy these constraints or attesters will not vote
// for the block.
package focil

import (
	"github.com/eth2030/eth2030/core/types"
)

// Constants from EIP-7805.
const (
	// MAX_TRANSACTIONS_PER_INCLUSION_LIST is the max entries in one IL (2^4 = 16).
	MAX_TRANSACTIONS_PER_INCLUSION_LIST = 16

	// MAX_GAS_PER_INCLUSION_LIST is the max total gas per IL (2^21 = 2097152).
	MAX_GAS_PER_INCLUSION_LIST = 1 << 21

	// MAX_BYTES_PER_INCLUSION_LIST is the max byte size of IL transactions (8 KiB).
	MAX_BYTES_PER_INCLUSION_LIST = 8192

	// IL_COMMITTEE_SIZE is the number of validators in the IL committee.
	IL_COMMITTEE_SIZE = 16
)

// InclusionListEntry represents a single transaction entry in an inclusion list.
type InclusionListEntry struct {
	Transaction []byte `json:"transaction"` // RLP-encoded transaction
	Index       uint64 `json:"index"`       // position in the list
}

// InclusionList represents a set of transactions that MUST be included in
// a subsequent block. Created by an IL committee member for a given slot.
type InclusionList struct {
	Slot          uint64               `json:"slot"`
	ProposerIndex uint64               `json:"proposerIndex"` // IL committee member index
	CommitteeRoot types.Hash           `json:"committeeRoot"` // root of IL committee
	Entries       []InclusionListEntry `json:"entries"`
}

// SignedInclusionList wraps an InclusionList with a BLS signature.
type SignedInclusionList struct {
	Message   InclusionList `json:"message"`
	Signature [96]byte      `json:"signature"`
}

// TotalGas returns the total gas of all transactions in the inclusion list.
func (il *InclusionList) TotalGas() uint64 {
	var total uint64
	for _, entry := range il.Entries {
		tx, err := types.DecodeTxRLP(entry.Transaction)
		if err != nil {
			continue
		}
		total += tx.Gas()
	}
	return total
}

// TotalBytes returns the total byte size of all transactions in the IL.
func (il *InclusionList) TotalBytes() int {
	total := 0
	for _, entry := range il.Entries {
		total += len(entry.Transaction)
	}
	return total
}

// TransactionHashes returns the keccak256 hashes of all transactions in the IL.
func (il *InclusionList) TransactionHashes() []types.Hash {
	var hashes []types.Hash
	for _, entry := range il.Entries {
		tx, err := types.DecodeTxRLP(entry.Transaction)
		if err != nil {
			continue
		}
		hashes = append(hashes, tx.Hash())
	}
	return hashes
}
