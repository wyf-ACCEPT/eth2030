package focil

import (
	"errors"
	"fmt"

	"github.com/eth2028/eth2028/core/types"
)

// Validation errors.
var (
	ErrTooManyTransactions = errors.New("inclusion list exceeds max transactions")
	ErrTooMuchGas          = errors.New("inclusion list exceeds max gas")
	ErrTooManyBytes        = errors.New("inclusion list exceeds max bytes")
	ErrZeroSlot            = errors.New("inclusion list slot must be > 0")
	ErrEmptyList           = errors.New("inclusion list is empty")
	ErrInvalidTransaction  = errors.New("inclusion list contains invalid transaction")
)

// ValidateInclusionList checks an inclusion list for structural correctness.
// Per EIP-7805, this validates size bounds but does not perform EL-side
// nonce/balance checks (those are deferred to attestation time).
func ValidateInclusionList(il *InclusionList) error {
	if il.Slot == 0 {
		return ErrZeroSlot
	}
	if len(il.Entries) == 0 {
		return ErrEmptyList
	}
	if len(il.Entries) > MAX_TRANSACTIONS_PER_INCLUSION_LIST {
		return fmt.Errorf("%w: got %d, max %d",
			ErrTooManyTransactions, len(il.Entries), MAX_TRANSACTIONS_PER_INCLUSION_LIST)
	}

	totalBytes := 0
	var totalGas uint64

	for i, entry := range il.Entries {
		totalBytes += len(entry.Transaction)
		if len(entry.Transaction) == 0 {
			return fmt.Errorf("%w: entry %d is empty", ErrInvalidTransaction, i)
		}

		tx, err := types.DecodeTxRLP(entry.Transaction)
		if err != nil {
			return fmt.Errorf("%w: entry %d: %v", ErrInvalidTransaction, i, err)
		}
		totalGas += tx.Gas()
	}

	if totalBytes > MAX_BYTES_PER_INCLUSION_LIST {
		return fmt.Errorf("%w: got %d bytes, max %d",
			ErrTooManyBytes, totalBytes, MAX_BYTES_PER_INCLUSION_LIST)
	}
	if totalGas > MAX_GAS_PER_INCLUSION_LIST {
		return fmt.Errorf("%w: got %d gas, max %d",
			ErrTooMuchGas, totalGas, MAX_GAS_PER_INCLUSION_LIST)
	}

	return nil
}

// CheckInclusionCompliance verifies that a block satisfies the given inclusion
// lists. For each IL, it checks that every transaction in the IL is present
// in the block. Returns true if all ILs are satisfied, and the indices of
// any unsatisfied ILs.
//
// Per EIP-7805, a transaction is considered satisfied if it is present in the
// block OR if it would be invalid when appended to the block (nonce/balance
// check). This simplified version only checks presence.
func CheckInclusionCompliance(block *types.Block, ils []*InclusionList) (bool, []int) {
	if len(ils) == 0 {
		return true, nil
	}

	// Build a set of transaction hashes in the block.
	blockTxHashes := make(map[types.Hash]bool)
	for _, tx := range block.Transactions() {
		blockTxHashes[tx.Hash()] = true
	}

	var unsatisfied []int

	for i, il := range ils {
		satisfied := true
		for _, entry := range il.Entries {
			tx, err := types.DecodeTxRLP(entry.Transaction)
			if err != nil {
				// Invalid IL transaction: skip (per spec, ILs can contain
				// invalid transactions and they are ignored at compliance time).
				continue
			}
			if !blockTxHashes[tx.Hash()] {
				satisfied = false
				break
			}
		}
		if !satisfied {
			unsatisfied = append(unsatisfied, i)
		}
	}

	allSatisfied := len(unsatisfied) == 0
	return allSatisfied, unsatisfied
}
