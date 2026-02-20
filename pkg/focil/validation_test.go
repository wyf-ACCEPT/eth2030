package focil

import (
	"errors"
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func mkTx(nonce uint64, gas uint64, gasPrice int64) *types.Transaction {
	to := types.HexToAddress("0xdeaddeaddeaddeaddeaddeaddeaddeaddeaddead")
	return types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(gasPrice),
		Gas:      gas,
		To:       &to,
		Value:    big.NewInt(0),
		V:        big.NewInt(27),
		R:        big.NewInt(1),
		S:        big.NewInt(1),
	})
}

func encTx(t *testing.T, tx *types.Transaction) []byte {
	t.Helper()
	data, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP: %v", err)
	}
	return data
}

func validIL(t *testing.T) *InclusionList {
	t.Helper()
	tx := mkTx(0, 21000, 1)
	txBytes := encTx(t, tx)
	return &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: txBytes, Index: 0},
		},
	}
}

// --- ValidateInclusionList tests ---

func TestValidateILValid(t *testing.T) {
	il := validIL(t)
	if err := ValidateInclusionList(il); err != nil {
		t.Errorf("valid IL: %v", err)
	}
}

func TestValidateILZeroSlot(t *testing.T) {
	il := validIL(t)
	il.Slot = 0
	err := ValidateInclusionList(il)
	if !errors.Is(err, ErrZeroSlot) {
		t.Errorf("expected ErrZeroSlot, got %v", err)
	}
}

func TestValidateILEmpty(t *testing.T) {
	il := &InclusionList{
		Slot:    100,
		Entries: []InclusionListEntry{},
	}
	err := ValidateInclusionList(il)
	if !errors.Is(err, ErrEmptyList) {
		t.Errorf("expected ErrEmptyList, got %v", err)
	}
}

func TestValidateInclusionListTooManyTransactions(t *testing.T) {
	tx := mkTx(0, 100, 1)
	txBytes := encTx(t, tx)

	entries := make([]InclusionListEntry, MAX_TRANSACTIONS_PER_INCLUSION_LIST+1)
	for i := range entries {
		entries[i] = InclusionListEntry{Transaction: txBytes, Index: uint64(i)}
	}
	il := &InclusionList{Slot: 100, Entries: entries}

	err := ValidateInclusionList(il)
	if !errors.Is(err, ErrTooManyTransactions) {
		t.Errorf("expected ErrTooManyTransactions, got %v", err)
	}
}

func TestValidateInclusionListEmptyTransaction(t *testing.T) {
	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: []byte{}, Index: 0},
		},
	}
	err := ValidateInclusionList(il)
	if !errors.Is(err, ErrInvalidTransaction) {
		t.Errorf("expected ErrInvalidTransaction, got %v", err)
	}
}

func TestValidateILInvalidTxEncoding(t *testing.T) {
	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: []byte{0xff, 0xfe, 0xfd}, Index: 0},
		},
	}
	err := ValidateInclusionList(il)
	if !errors.Is(err, ErrInvalidTransaction) {
		t.Errorf("expected ErrInvalidTransaction, got %v", err)
	}
}

func TestValidateInclusionListExactMaxTransactions(t *testing.T) {
	// Exactly at the limit should be valid.
	tx := mkTx(0, 100, 1)
	txBytes := encTx(t, tx)

	entries := make([]InclusionListEntry, MAX_TRANSACTIONS_PER_INCLUSION_LIST)
	for i := range entries {
		entries[i] = InclusionListEntry{Transaction: txBytes, Index: uint64(i)}
	}
	il := &InclusionList{Slot: 100, Entries: entries}

	if err := ValidateInclusionList(il); err != nil {
		t.Errorf("exact max should be valid: %v", err)
	}
}

func TestValidateInclusionListByteLimitExceeded(t *testing.T) {
	// Create a transaction with large data to exceed byte limit.
	to := types.HexToAddress("0xdeaddeaddeaddeaddeaddeaddeaddeaddeaddead")
	bigData := make([]byte, MAX_BYTES_PER_INCLUSION_LIST) // way too large
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      100000,
		To:       &to,
		Value:    big.NewInt(0),
		Data:     bigData,
		V:        big.NewInt(27),
		R:        big.NewInt(1),
		S:        big.NewInt(1),
	})
	txBytes, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP: %v", err)
	}

	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: txBytes, Index: 0},
		},
	}

	err = ValidateInclusionList(il)
	if !errors.Is(err, ErrTooManyBytes) {
		t.Errorf("expected ErrTooManyBytes, got %v", err)
	}
}

// --- CheckInclusionCompliance tests ---

func TestCheckComplianceNoILs(t *testing.T) {
	header := &types.Header{Number: big.NewInt(1), GasLimit: 1_000_000}
	block := types.NewBlock(header, nil)

	satisfied, unsatisfied := CheckInclusionCompliance(block, nil)
	if !satisfied {
		t.Error("no ILs should be satisfied")
	}
	if len(unsatisfied) != 0 {
		t.Error("unsatisfied should be empty")
	}
}

func TestCheckInclusionComplianceEmptyBlock(t *testing.T) {
	tx := mkTx(0, 21000, 1)
	txBytes := encTx(t, tx)

	il := &InclusionList{
		Slot:    100,
		Entries: []InclusionListEntry{{Transaction: txBytes}},
	}

	header := &types.Header{Number: big.NewInt(1), GasLimit: 1_000_000}
	block := types.NewBlock(header, nil)

	satisfied, unsatisfied := CheckInclusionCompliance(block, []*InclusionList{il})
	if satisfied {
		t.Error("empty block should not satisfy IL with transactions")
	}
	if len(unsatisfied) != 1 || unsatisfied[0] != 0 {
		t.Errorf("unsatisfied = %v, want [0]", unsatisfied)
	}
}

func TestCheckInclusionComplianceAllPresent(t *testing.T) {
	tx1 := mkTx(0, 21000, 1)
	tx2 := mkTx(1, 21000, 2)

	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: encTx(t, tx1)},
			{Transaction: encTx(t, tx2)},
		},
	}

	header := &types.Header{Number: big.NewInt(1), GasLimit: 1_000_000}
	body := &types.Body{Transactions: []*types.Transaction{tx1, tx2}}
	block := types.NewBlock(header, body)

	satisfied, unsatisfied := CheckInclusionCompliance(block, []*InclusionList{il})
	if !satisfied {
		t.Error("all transactions present, should be satisfied")
	}
	if len(unsatisfied) != 0 {
		t.Errorf("unsatisfied = %v, want empty", unsatisfied)
	}
}

func TestCheckInclusionCompliancePartialMissing(t *testing.T) {
	tx1 := mkTx(0, 21000, 1)
	tx2 := mkTx(1, 21000, 2)

	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: encTx(t, tx1)},
			{Transaction: encTx(t, tx2)},
		},
	}

	// Block only has tx1.
	header := &types.Header{Number: big.NewInt(1), GasLimit: 1_000_000}
	body := &types.Body{Transactions: []*types.Transaction{tx1}}
	block := types.NewBlock(header, body)

	satisfied, unsatisfied := CheckInclusionCompliance(block, []*InclusionList{il})
	if satisfied {
		t.Error("missing tx2, should not be satisfied")
	}
	if len(unsatisfied) != 1 {
		t.Errorf("unsatisfied = %v, want 1 entry", unsatisfied)
	}
}

func TestCheckComplianceMultipleILs(t *testing.T) {
	tx1 := mkTx(0, 21000, 1)
	tx2 := mkTx(1, 21000, 2)
	tx3 := mkTx(2, 21000, 3)

	il0 := &InclusionList{
		Slot:    100,
		Entries: []InclusionListEntry{{Transaction: encTx(t, tx1)}},
	}
	il1 := &InclusionList{
		Slot:    100,
		Entries: []InclusionListEntry{{Transaction: encTx(t, tx3)}},
	}

	// Block has tx1 and tx2 but not tx3.
	header := &types.Header{Number: big.NewInt(1), GasLimit: 1_000_000}
	body := &types.Body{Transactions: []*types.Transaction{tx1, tx2}}
	block := types.NewBlock(header, body)

	satisfied, unsatisfied := CheckInclusionCompliance(block, []*InclusionList{il0, il1})
	if satisfied {
		t.Error("il1 is unsatisfied")
	}
	if len(unsatisfied) != 1 || unsatisfied[0] != 1 {
		t.Errorf("unsatisfied = %v, want [1]", unsatisfied)
	}
}

func TestCheckInclusionComplianceInvalidILEntry(t *testing.T) {
	// Invalid IL entry should be skipped (ignored per spec).
	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: []byte{0xff, 0xfe}}, // invalid encoding
		},
	}

	header := &types.Header{Number: big.NewInt(1), GasLimit: 1_000_000}
	block := types.NewBlock(header, nil)

	// Invalid entries are skipped, so the IL is trivially satisfied.
	satisfied, unsatisfied := CheckInclusionCompliance(block, []*InclusionList{il})
	if !satisfied {
		t.Error("IL with only invalid entries should be satisfied")
	}
	if len(unsatisfied) != 0 {
		t.Errorf("unsatisfied = %v, want empty", unsatisfied)
	}
}
