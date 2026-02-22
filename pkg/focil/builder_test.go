package focil

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func newLegacyTx(nonce uint64, gas uint64, gasPrice int64) *types.Transaction {
	to := types.HexToAddress("0xdeaddeaddeaddeaddeaddeaddeaddeaddeaddead")
	return types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(gasPrice),
		Gas:      gas,
		To:       &to,
		Value:    big.NewInt(0),
		Data:     nil,
		V:        big.NewInt(27),
		R:        big.NewInt(1),
		S:        big.NewInt(1),
	})
}

func encodeTransaction(t *testing.T, tx *types.Transaction) []byte {
	t.Helper()
	data, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP: %v", err)
	}
	return data
}

// --- BuildInclusionList tests ---

func TestBuildInclusionListSlot(t *testing.T) {
	il := BuildInclusionList(nil, 77)
	if il.Slot != 77 {
		t.Errorf("Slot = %d, want 77", il.Slot)
	}
}

func TestBuildInclusionListNoPending(t *testing.T) {
	il := BuildInclusionList(nil, 1)
	if len(il.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(il.Entries))
	}
}

func TestBuildInclusionListSingleTx(t *testing.T) {
	tx := newLegacyTx(0, 21000, 1000)
	il := BuildInclusionList([]*types.Transaction{tx}, 10)
	if len(il.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(il.Entries))
	}
	if il.Entries[0].Index != 0 {
		t.Errorf("entry index = %d, want 0", il.Entries[0].Index)
	}
}

func TestBuildInclusionListMaxTransactions(t *testing.T) {
	// Create more transactions than the max.
	txs := make([]*types.Transaction, MAX_TRANSACTIONS_PER_INCLUSION_LIST+5)
	for i := range txs {
		txs[i] = newLegacyTx(uint64(i), 100, 1) // low gas to avoid gas/byte limits
	}

	il := BuildInclusionList(txs, 1)

	if len(il.Entries) > MAX_TRANSACTIONS_PER_INCLUSION_LIST {
		t.Errorf("entries = %d, exceeds max %d",
			len(il.Entries), MAX_TRANSACTIONS_PER_INCLUSION_LIST)
	}
}

func TestBuildInclusionListGasLimitEnforced(t *testing.T) {
	// Create transactions whose total gas exceeds the limit.
	txs := make([]*types.Transaction, 3)
	for i := range txs {
		txs[i] = newLegacyTx(uint64(i), MAX_GAS_PER_INCLUSION_LIST/2+1, 1000)
	}

	il := BuildInclusionList(txs, 1)

	// At most 1 tx should fit.
	if len(il.Entries) > 1 {
		t.Errorf("gas limit not enforced: %d entries (expected <= 1)", len(il.Entries))
	}
}

func TestBuildInclusionListSortsByGasPrice(t *testing.T) {
	txLow := newLegacyTx(0, 21000, 100)
	txHigh := newLegacyTx(1, 21000, 10000)
	txMid := newLegacyTx(2, 21000, 5000)

	il := BuildInclusionList([]*types.Transaction{txLow, txHigh, txMid}, 1)

	if len(il.Entries) < 3 {
		t.Fatalf("expected 3 entries, got %d", len(il.Entries))
	}

	// First entry should be the highest gas price.
	firstTx, err := types.DecodeTxRLP(il.Entries[0].Transaction)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if firstTx.GasPrice().Int64() != 10000 {
		t.Errorf("first entry gas price = %d, want 10000", firstTx.GasPrice().Int64())
	}
}

func TestBuildInclusionListSequentialIndices(t *testing.T) {
	txs := make([]*types.Transaction, 5)
	for i := range txs {
		txs[i] = newLegacyTx(uint64(i), 21000, int64(i+1)*100)
	}

	il := BuildInclusionList(txs, 99)

	for i, entry := range il.Entries {
		if entry.Index != uint64(i) {
			t.Errorf("entry %d: index = %d, want %d", i, entry.Index, i)
		}
	}
}

// --- BuildInclusionListFromRaw tests ---

func TestBuildInclusionListFromRawValid(t *testing.T) {
	tx1 := newLegacyTx(0, 21000, 1)
	tx2 := newLegacyTx(1, 21000, 2)
	raw1 := encodeTransaction(t, tx1)
	raw2 := encodeTransaction(t, tx2)

	il := BuildInclusionListFromRaw([][]byte{raw1, raw2}, 42)

	if il.Slot != 42 {
		t.Errorf("Slot = %d, want 42", il.Slot)
	}
	if len(il.Entries) != 2 {
		t.Errorf("entries = %d, want 2", len(il.Entries))
	}
}

func TestBuildInclusionListFromRawEmpty(t *testing.T) {
	il := BuildInclusionListFromRaw(nil, 1)
	if len(il.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(il.Entries))
	}
}

func TestBuildInclusionListFromRawSkipsBadEncoding(t *testing.T) {
	tx := newLegacyTx(0, 21000, 1)
	raw := encodeTransaction(t, tx)

	il := BuildInclusionListFromRaw([][]byte{
		raw,
		{0xff, 0xfe, 0xfd}, // invalid encoding
		raw,
	}, 1)

	if len(il.Entries) != 2 {
		t.Errorf("entries = %d, want 2 (invalid skipped)", len(il.Entries))
	}
}

func TestBuildInclusionListFromRawGasLimit(t *testing.T) {
	tx := newLegacyTx(0, MAX_GAS_PER_INCLUSION_LIST+1, 1)
	raw := encodeTransaction(t, tx)

	il := BuildInclusionListFromRaw([][]byte{raw}, 1)

	// The single tx exceeds gas limit, so it should be skipped.
	if len(il.Entries) != 0 {
		t.Errorf("entries = %d, want 0 (gas limit exceeded)", len(il.Entries))
	}
}

func TestBuildInclusionListFromRawMaxTransactions(t *testing.T) {
	tx := newLegacyTx(0, 100, 1)
	raw := encodeTransaction(t, tx)

	raws := make([][]byte, MAX_TRANSACTIONS_PER_INCLUSION_LIST+5)
	for i := range raws {
		raws[i] = raw
	}

	il := BuildInclusionListFromRaw(raws, 1)

	if len(il.Entries) > MAX_TRANSACTIONS_PER_INCLUSION_LIST {
		t.Errorf("entries = %d, exceeds max %d",
			len(il.Entries), MAX_TRANSACTIONS_PER_INCLUSION_LIST)
	}
}
