package focil

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// --- Helper functions ---

func makeLegacyTx(nonce uint64, gas uint64, gasPrice int64) *types.Transaction {
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

func encodeTx(t *testing.T, tx *types.Transaction) []byte {
	t.Helper()
	data, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP: %v", err)
	}
	return data
}

// --- Type Tests ---

func TestConstants(t *testing.T) {
	if MAX_TRANSACTIONS_PER_INCLUSION_LIST != 16 {
		t.Errorf("MAX_TRANSACTIONS_PER_INCLUSION_LIST = %d, want 16",
			MAX_TRANSACTIONS_PER_INCLUSION_LIST)
	}
	if MAX_GAS_PER_INCLUSION_LIST != 1<<21 {
		t.Errorf("MAX_GAS_PER_INCLUSION_LIST = %d, want %d",
			MAX_GAS_PER_INCLUSION_LIST, 1<<21)
	}
	if MAX_BYTES_PER_INCLUSION_LIST != 8192 {
		t.Errorf("MAX_BYTES_PER_INCLUSION_LIST = %d, want 8192",
			MAX_BYTES_PER_INCLUSION_LIST)
	}
	if IL_COMMITTEE_SIZE != 16 {
		t.Errorf("IL_COMMITTEE_SIZE = %d, want 16", IL_COMMITTEE_SIZE)
	}
}

func TestInclusionListJSON(t *testing.T) {
	il := InclusionList{
		Slot:          42,
		ProposerIndex: 7,
		Entries: []InclusionListEntry{
			{Transaction: []byte{0x01, 0x02}, Index: 0},
			{Transaction: []byte{0x03, 0x04}, Index: 1},
		},
	}

	data, err := json.Marshal(il)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded InclusionList
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded.Slot != 42 {
		t.Errorf("Slot = %d, want 42", decoded.Slot)
	}
	if decoded.ProposerIndex != 7 {
		t.Errorf("ProposerIndex = %d, want 7", decoded.ProposerIndex)
	}
	if len(decoded.Entries) != 2 {
		t.Errorf("entries = %d, want 2", len(decoded.Entries))
	}
}

func TestTotalBytes(t *testing.T) {
	il := &InclusionList{
		Entries: []InclusionListEntry{
			{Transaction: make([]byte, 100)},
			{Transaction: make([]byte, 200)},
			{Transaction: make([]byte, 50)},
		},
	}
	if got := il.TotalBytes(); got != 350 {
		t.Errorf("TotalBytes = %d, want 350", got)
	}
}

func TestTotalGas(t *testing.T) {
	tx1 := makeLegacyTx(0, 21000, 1)
	tx2 := makeLegacyTx(1, 42000, 1)

	tx1Bytes, _ := tx1.EncodeRLP()
	tx2Bytes, _ := tx2.EncodeRLP()

	il := &InclusionList{
		Entries: []InclusionListEntry{
			{Transaction: tx1Bytes},
			{Transaction: tx2Bytes},
		},
	}

	got := il.TotalGas()
	if got != 63000 {
		t.Errorf("TotalGas = %d, want 63000", got)
	}
}

// --- Validation Tests ---

func TestValidateInclusionListValid(t *testing.T) {
	tx := makeLegacyTx(0, 21000, 1)
	txBytes := encodeTx(t, tx)

	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: txBytes, Index: 0},
		},
	}

	if err := ValidateInclusionList(il); err != nil {
		t.Errorf("valid IL: %v", err)
	}
}

func TestValidateInclusionListZeroSlot(t *testing.T) {
	tx := makeLegacyTx(0, 21000, 1)
	txBytes := encodeTx(t, tx)

	il := &InclusionList{
		Slot:    0,
		Entries: []InclusionListEntry{{Transaction: txBytes}},
	}

	if err := ValidateInclusionList(il); err != ErrZeroSlot {
		t.Errorf("zero slot: got %v, want ErrZeroSlot", err)
	}
}

func TestValidateInclusionListEmpty(t *testing.T) {
	il := &InclusionList{
		Slot:    100,
		Entries: []InclusionListEntry{},
	}

	if err := ValidateInclusionList(il); err != ErrEmptyList {
		t.Errorf("empty list: got %v, want ErrEmptyList", err)
	}
}

func TestValidateInclusionListTooMany(t *testing.T) {
	entries := make([]InclusionListEntry, MAX_TRANSACTIONS_PER_INCLUSION_LIST+1)
	tx := makeLegacyTx(0, 100, 1)
	txBytes := encodeTx(t, tx)

	for i := range entries {
		entries[i] = InclusionListEntry{Transaction: txBytes, Index: uint64(i)}
	}

	il := &InclusionList{
		Slot:    100,
		Entries: entries,
	}

	if err := ValidateInclusionList(il); err == nil {
		t.Error("expected error for too many transactions")
	}
}

func TestValidateInclusionListEmptyEntry(t *testing.T) {
	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: []byte{}, Index: 0},
		},
	}

	if err := ValidateInclusionList(il); err == nil {
		t.Error("expected error for empty entry")
	}
}

func TestValidateInclusionListInvalidTxEncoding(t *testing.T) {
	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: []byte{0xff, 0xfe, 0xfd}, Index: 0},
		},
	}

	if err := ValidateInclusionList(il); err == nil {
		t.Error("expected error for invalid tx encoding")
	}
}

// --- Compliance Tests ---

func TestCheckInclusionComplianceAllSatisfied(t *testing.T) {
	tx1 := makeLegacyTx(0, 21000, 1)
	tx2 := makeLegacyTx(1, 21000, 2)

	tx1Bytes := encodeTx(t, tx1)
	tx2Bytes := encodeTx(t, tx2)

	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: tx1Bytes, Index: 0},
			{Transaction: tx2Bytes, Index: 1},
		},
	}

	header := &types.Header{Number: big.NewInt(1), GasLimit: 1_000_000}
	body := &types.Body{Transactions: []*types.Transaction{tx1, tx2}}
	block := types.NewBlock(header, body)

	satisfied, unsatisfied := CheckInclusionCompliance(block, []*InclusionList{il})
	if !satisfied {
		t.Error("expected all ILs satisfied")
	}
	if len(unsatisfied) != 0 {
		t.Errorf("unsatisfied = %v, want empty", unsatisfied)
	}
}

func TestCheckInclusionComplianceMissing(t *testing.T) {
	tx1 := makeLegacyTx(0, 21000, 1)
	tx2 := makeLegacyTx(1, 21000, 2) // not in block

	tx1Bytes := encodeTx(t, tx1)
	tx2Bytes := encodeTx(t, tx2)

	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: tx1Bytes, Index: 0},
			{Transaction: tx2Bytes, Index: 1},
		},
	}

	header := &types.Header{Number: big.NewInt(1), GasLimit: 1_000_000}
	body := &types.Body{Transactions: []*types.Transaction{tx1}} // only tx1
	block := types.NewBlock(header, body)

	satisfied, unsatisfied := CheckInclusionCompliance(block, []*InclusionList{il})
	if satisfied {
		t.Error("expected unsatisfied (tx2 missing)")
	}
	if len(unsatisfied) != 1 || unsatisfied[0] != 0 {
		t.Errorf("unsatisfied = %v, want [0]", unsatisfied)
	}
}

func TestCheckInclusionComplianceNoILs(t *testing.T) {
	header := &types.Header{Number: big.NewInt(1), GasLimit: 1_000_000}
	block := types.NewBlock(header, nil)

	satisfied, unsatisfied := CheckInclusionCompliance(block, nil)
	if !satisfied {
		t.Error("expected satisfied with no ILs")
	}
	if len(unsatisfied) != 0 {
		t.Errorf("unsatisfied = %v, want empty", unsatisfied)
	}
}

func TestCheckInclusionComplianceMultipleILs(t *testing.T) {
	tx1 := makeLegacyTx(0, 21000, 1)
	tx2 := makeLegacyTx(1, 21000, 2)
	tx3 := makeLegacyTx(2, 21000, 3)

	tx1Bytes := encodeTx(t, tx1)
	tx2Bytes := encodeTx(t, tx2)
	tx3Bytes := encodeTx(t, tx3)

	il1 := &InclusionList{
		Slot:    100,
		Entries: []InclusionListEntry{{Transaction: tx1Bytes}},
	}
	il2 := &InclusionList{
		Slot:    100,
		Entries: []InclusionListEntry{{Transaction: tx2Bytes}, {Transaction: tx3Bytes}},
	}

	// Block only has tx1 and tx2 (missing tx3 from il2).
	header := &types.Header{Number: big.NewInt(1), GasLimit: 1_000_000}
	body := &types.Body{Transactions: []*types.Transaction{tx1, tx2}}
	block := types.NewBlock(header, body)

	satisfied, unsatisfied := CheckInclusionCompliance(block, []*InclusionList{il1, il2})
	if satisfied {
		t.Error("expected not all satisfied")
	}
	if len(unsatisfied) != 1 || unsatisfied[0] != 1 {
		t.Errorf("unsatisfied = %v, want [1]", unsatisfied)
	}
}

// --- Builder Tests ---

func TestBuildInclusionList(t *testing.T) {
	txs := make([]*types.Transaction, 5)
	for i := 0; i < 5; i++ {
		txs[i] = makeLegacyTx(uint64(i), 21000, int64(i+1)*1000)
	}

	il := BuildInclusionList(txs, 42)

	if il.Slot != 42 {
		t.Errorf("Slot = %d, want 42", il.Slot)
	}
	if len(il.Entries) == 0 {
		t.Fatal("expected non-empty inclusion list")
	}
	if len(il.Entries) > MAX_TRANSACTIONS_PER_INCLUSION_LIST {
		t.Errorf("too many entries: %d", len(il.Entries))
	}

	// Verify indices are sequential.
	for i, entry := range il.Entries {
		if entry.Index != uint64(i) {
			t.Errorf("entry %d: index = %d, want %d", i, entry.Index, i)
		}
	}
}

func TestBuildInclusionListEmpty(t *testing.T) {
	il := BuildInclusionList(nil, 42)
	if il.Slot != 42 {
		t.Errorf("Slot = %d, want 42", il.Slot)
	}
	if len(il.Entries) != 0 {
		t.Errorf("entries = %d, want 0", len(il.Entries))
	}
}

func TestBuildInclusionListGasLimit(t *testing.T) {
	// Create transactions that would exceed the gas limit.
	txs := make([]*types.Transaction, 3)
	for i := 0; i < 3; i++ {
		txs[i] = makeLegacyTx(uint64(i), MAX_GAS_PER_INCLUSION_LIST/2+1, 1000)
	}

	il := BuildInclusionList(txs, 100)

	// Only 1 tx should fit (the first one fills > half the gas budget).
	if len(il.Entries) > 1 {
		t.Errorf("entries = %d, want <= 1 (gas limited)", len(il.Entries))
	}
}

func TestBuildInclusionListFromRaw(t *testing.T) {
	tx1 := makeLegacyTx(0, 21000, 1)
	tx2 := makeLegacyTx(1, 21000, 2)

	raw1, _ := tx1.EncodeRLP()
	raw2, _ := tx2.EncodeRLP()

	il := BuildInclusionListFromRaw([][]byte{raw1, raw2}, 50)

	if il.Slot != 50 {
		t.Errorf("Slot = %d, want 50", il.Slot)
	}
	if len(il.Entries) != 2 {
		t.Errorf("entries = %d, want 2", len(il.Entries))
	}
}

func TestBuildInclusionListFromRawSkipsInvalid(t *testing.T) {
	tx := makeLegacyTx(0, 21000, 1)
	raw, _ := tx.EncodeRLP()

	il := BuildInclusionListFromRaw([][]byte{
		raw,
		{0xff, 0xfe}, // invalid encoding
		raw,
	}, 50)

	// The invalid entry should be skipped.
	if len(il.Entries) != 2 {
		t.Errorf("entries = %d, want 2 (invalid entry skipped)", len(il.Entries))
	}
}

func TestBuildInclusionListPriorityOrder(t *testing.T) {
	// Create transactions with different gas prices.
	txLow := makeLegacyTx(0, 21000, 100)
	txHigh := makeLegacyTx(1, 21000, 10000)
	txMid := makeLegacyTx(2, 21000, 5000)

	il := BuildInclusionList([]*types.Transaction{txLow, txHigh, txMid}, 42)

	if len(il.Entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(il.Entries))
	}

	// The highest gas price tx should come first.
	firstTx, err := types.DecodeTxRLP(il.Entries[0].Transaction)
	if err != nil {
		t.Fatalf("decode first entry: %v", err)
	}
	if firstTx.GasPrice().Int64() != 10000 {
		t.Errorf("first entry gas price = %d, want 10000", firstTx.GasPrice().Int64())
	}
}
