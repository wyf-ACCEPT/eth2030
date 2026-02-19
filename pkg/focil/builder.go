package focil

import (
	"sort"

	"github.com/eth2028/eth2028/core/types"
)

// BuildInclusionList constructs an inclusion list from pending transactions.
// It selects transactions sorted by gas price (highest first), up to the
// limits defined by MAX_TRANSACTIONS_PER_INCLUSION_LIST, MAX_GAS_PER_INCLUSION_LIST,
// and MAX_BYTES_PER_INCLUSION_LIST.
//
// Per EIP-7805, the IL building strategy is left to implementers; this
// implementation uses a simple priority-fee ordering.
func BuildInclusionList(pending []*types.Transaction, slot uint64) *InclusionList {
	il := &InclusionList{
		Slot:    slot,
		Entries: make([]InclusionListEntry, 0),
	}

	if len(pending) == 0 {
		return il
	}

	// Sort by gas price descending (prioritize higher-paying transactions).
	sorted := make([]*types.Transaction, len(pending))
	copy(sorted, pending)
	sort.Slice(sorted, func(i, j int) bool {
		pi := sorted[i].GasPrice()
		pj := sorted[j].GasPrice()
		if pi == nil || pj == nil {
			return pi != nil
		}
		return pi.Cmp(pj) > 0
	})

	var totalGas uint64
	totalBytes := 0
	index := uint64(0)

	for _, tx := range sorted {
		if int(index) >= MAX_TRANSACTIONS_PER_INCLUSION_LIST {
			break
		}

		txBytes, err := tx.EncodeRLP()
		if err != nil {
			continue
		}

		// Check gas limit.
		if totalGas+tx.Gas() > MAX_GAS_PER_INCLUSION_LIST {
			continue
		}

		// Check byte limit.
		if totalBytes+len(txBytes) > MAX_BYTES_PER_INCLUSION_LIST {
			continue
		}

		il.Entries = append(il.Entries, InclusionListEntry{
			Transaction: txBytes,
			Index:       index,
		})

		totalGas += tx.Gas()
		totalBytes += len(txBytes)
		index++
	}

	return il
}

// BuildInclusionListFromRaw constructs an inclusion list from raw RLP-encoded
// transaction bytes. This is used when receiving transactions from the CL
// that are already encoded.
func BuildInclusionListFromRaw(rawTxs [][]byte, slot uint64) *InclusionList {
	il := &InclusionList{
		Slot:    slot,
		Entries: make([]InclusionListEntry, 0),
	}

	var totalGas uint64
	totalBytes := 0
	index := uint64(0)

	for _, raw := range rawTxs {
		if int(index) >= MAX_TRANSACTIONS_PER_INCLUSION_LIST {
			break
		}

		tx, err := types.DecodeTxRLP(raw)
		if err != nil {
			continue
		}

		if totalGas+tx.Gas() > MAX_GAS_PER_INCLUSION_LIST {
			continue
		}

		if totalBytes+len(raw) > MAX_BYTES_PER_INCLUSION_LIST {
			continue
		}

		il.Entries = append(il.Entries, InclusionListEntry{
			Transaction: raw,
			Index:       index,
		})

		totalGas += tx.Gas()
		totalBytes += len(raw)
		index++
	}

	return il
}
