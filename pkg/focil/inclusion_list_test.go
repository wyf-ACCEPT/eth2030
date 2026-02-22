package focil

import (
	"errors"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// ilTx creates a transaction for inclusion list tests with the given parameters.
func ilTx(nonce uint64, gas uint64, gasPrice int64) *types.Transaction {
	to := types.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")
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

// ilEncode RLP-encodes a transaction for inclusion list tests.
func ilEncode(t *testing.T, tx *types.Transaction) []byte {
	t.Helper()
	data, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP: %v", err)
	}
	return data
}

// --- Inclusion list construction ---

func TestBuildInclusionListOrdering(t *testing.T) {
	// BuildInclusionList should sort by gas price descending.
	txLow := ilTx(0, 21000, 100)
	txHigh := ilTx(1, 21000, 10000)
	txMid := ilTx(2, 21000, 5000)

	il := BuildInclusionList([]*types.Transaction{txLow, txHigh, txMid}, 1)

	if len(il.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(il.Entries))
	}

	// First entry should be the highest gas price tx.
	firstTx, err := types.DecodeTxRLP(il.Entries[0].Transaction)
	if err != nil {
		t.Fatalf("decode first entry: %v", err)
	}
	if firstTx.GasPrice().Int64() != 10000 {
		t.Errorf("first entry gas price = %d, want 10000", firstTx.GasPrice().Int64())
	}

	// Last entry should be the lowest gas price tx.
	lastTx, err := types.DecodeTxRLP(il.Entries[2].Transaction)
	if err != nil {
		t.Fatalf("decode last entry: %v", err)
	}
	if lastTx.GasPrice().Int64() != 100 {
		t.Errorf("last entry gas price = %d, want 100", lastTx.GasPrice().Int64())
	}
}

func TestBuildInclusionListEnforcesMaxTx(t *testing.T) {
	txs := make([]*types.Transaction, MAX_TRANSACTIONS_PER_INCLUSION_LIST+10)
	for i := range txs {
		txs[i] = ilTx(uint64(i), 100, 1)
	}

	il := BuildInclusionList(txs, 42)
	if len(il.Entries) > MAX_TRANSACTIONS_PER_INCLUSION_LIST {
		t.Errorf("entries = %d, exceeds max %d",
			len(il.Entries), MAX_TRANSACTIONS_PER_INCLUSION_LIST)
	}
}

func TestBuildInclusionListEnforcesGasLimit(t *testing.T) {
	// Each tx uses just over half the gas limit.
	gasPerTx := uint64(MAX_GAS_PER_INCLUSION_LIST/2 + 1)
	txs := []*types.Transaction{
		ilTx(0, gasPerTx, 1000),
		ilTx(1, gasPerTx, 900),
		ilTx(2, gasPerTx, 800),
	}

	il := BuildInclusionList(txs, 10)

	// At most 1 tx should fit within the gas limit.
	if len(il.Entries) > 1 {
		t.Errorf("gas limit not enforced: %d entries (expected <= 1)", len(il.Entries))
	}
}

func TestBuildInclusionListAssignsSequentialIndices(t *testing.T) {
	txs := make([]*types.Transaction, 5)
	for i := range txs {
		txs[i] = ilTx(uint64(i), 21000, int64(i+1)*100)
	}

	il := BuildInclusionList(txs, 99)

	for i, entry := range il.Entries {
		if entry.Index != uint64(i) {
			t.Errorf("entry %d: index = %d, want %d", i, entry.Index, i)
		}
	}
}

// --- BuildInclusionListFromRaw ---

func TestBuildFromRawSkipsInvalidEncodings(t *testing.T) {
	validRaw := ilEncode(t, ilTx(0, 21000, 1))

	il := BuildInclusionListFromRaw([][]byte{
		validRaw,
		{0xff, 0xfe, 0xfd}, // invalid RLP
		validRaw,
	}, 1)

	if len(il.Entries) != 2 {
		t.Errorf("entries = %d, want 2 (invalid skipped)", len(il.Entries))
	}
}

func TestBuildFromRawEnforcesGasLimit(t *testing.T) {
	tx := ilTx(0, MAX_GAS_PER_INCLUSION_LIST+1, 1)
	raw := ilEncode(t, tx)

	il := BuildInclusionListFromRaw([][]byte{raw}, 1)

	if len(il.Entries) != 0 {
		t.Errorf("entries = %d, want 0 (gas limit exceeded)", len(il.Entries))
	}
}

// --- List merging (via CheckInclusionCompliance with multiple ILs) ---

func TestComplianceMergedListsDeduplication(t *testing.T) {
	// Two ILs referencing the same transaction should only count it once.
	tx := ilTx(0, 21000, 1)
	txBytes := ilEncode(t, tx)

	il1 := &InclusionList{
		Slot:    100,
		Entries: []InclusionListEntry{{Transaction: txBytes}},
	}
	il2 := &InclusionList{
		Slot:    100,
		Entries: []InclusionListEntry{{Transaction: txBytes}},
	}

	header := &types.Header{Number: big.NewInt(1), GasLimit: 1_000_000}
	body := &types.Body{Transactions: []*types.Transaction{tx}}
	block := types.NewBlock(header, body)

	satisfied, unsatisfied := CheckInclusionCompliance(block, []*InclusionList{il1, il2})
	if !satisfied {
		t.Error("block with the tx should satisfy both ILs pointing to the same tx")
	}
	if len(unsatisfied) != 0 {
		t.Errorf("unsatisfied = %v, want empty", unsatisfied)
	}
}

func TestComplianceMixedSatisfaction(t *testing.T) {
	tx1 := ilTx(0, 21000, 1)
	tx2 := ilTx(1, 21000, 2)
	tx3 := ilTx(2, 21000, 3)

	il0 := &InclusionList{
		Slot:    100,
		Entries: []InclusionListEntry{{Transaction: ilEncode(t, tx1)}},
	}
	il1 := &InclusionList{
		Slot:    100,
		Entries: []InclusionListEntry{{Transaction: ilEncode(t, tx2)}},
	}
	il2 := &InclusionList{
		Slot:    100,
		Entries: []InclusionListEntry{{Transaction: ilEncode(t, tx3)}},
	}

	// Block only has tx1 and tx3.
	header := &types.Header{Number: big.NewInt(1), GasLimit: 1_000_000}
	body := &types.Body{Transactions: []*types.Transaction{tx1, tx3}}
	block := types.NewBlock(header, body)

	satisfied, unsatisfied := CheckInclusionCompliance(block, []*InclusionList{il0, il1, il2})
	if satisfied {
		t.Error("tx2 is missing, should not be satisfied")
	}
	// Only il1 (index 1) should be unsatisfied.
	if len(unsatisfied) != 1 || unsatisfied[0] != 1 {
		t.Errorf("unsatisfied = %v, want [1]", unsatisfied)
	}
}

// --- Compliance verification ---

func TestComplianceFullyCompliantBlock(t *testing.T) {
	tx1 := ilTx(0, 21000, 100)
	tx2 := ilTx(1, 21000, 200)

	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: ilEncode(t, tx1)},
			{Transaction: ilEncode(t, tx2)},
		},
	}

	header := &types.Header{Number: big.NewInt(1), GasLimit: 1_000_000}
	body := &types.Body{Transactions: []*types.Transaction{tx1, tx2}}
	block := types.NewBlock(header, body)

	satisfied, unsatisfied := CheckInclusionCompliance(block, []*InclusionList{il})
	if !satisfied {
		t.Error("all txs present, should be satisfied")
	}
	if len(unsatisfied) != 0 {
		t.Errorf("unsatisfied = %v, want empty", unsatisfied)
	}
}

func TestComplianceEmptyBlockWithNonEmptyIL(t *testing.T) {
	tx := ilTx(0, 21000, 1)

	il := &InclusionList{
		Slot:    100,
		Entries: []InclusionListEntry{{Transaction: ilEncode(t, tx)}},
	}

	header := &types.Header{Number: big.NewInt(1), GasLimit: 1_000_000}
	block := types.NewBlock(header, nil) // no transactions

	satisfied, unsatisfied := CheckInclusionCompliance(block, []*InclusionList{il})
	if satisfied {
		t.Error("empty block should not satisfy IL with transactions")
	}
	if len(unsatisfied) != 1 || unsatisfied[0] != 0 {
		t.Errorf("unsatisfied = %v, want [0]", unsatisfied)
	}
}

// --- Compliance engine: deadline and scoring ---

func TestComplianceEngineEvaluateBlockNoLists(t *testing.T) {
	ce := NewComplianceEngine(DefaultComplianceEngineConfig())
	builder := types.HexToAddress("0xbuilderbuilderbuilderbuilderbuilderbuilderbui")

	header := &types.Header{Number: big.NewInt(5), GasLimit: 1_000_000}
	block := types.NewBlock(header, nil)

	result, err := ce.EvaluateBlock(block, builder)
	if err != nil {
		t.Fatalf("EvaluateBlock: %v", err)
	}
	// No ILs means vacuously compliant.
	if !result.Compliant {
		t.Error("no ILs should be vacuously compliant")
	}
	if result.ComplianceRate != 1.0 {
		t.Errorf("compliance rate = %f, want 1.0", result.ComplianceRate)
	}
}

func TestComplianceEngineDuplicateEvaluation(t *testing.T) {
	ce := NewComplianceEngine(DefaultComplianceEngineConfig())
	builder := types.HexToAddress("0xbuilderbuilderbuilderbuilderbuilderbuilderbui")

	header := &types.Header{Number: big.NewInt(10), GasLimit: 1_000_000}
	block := types.NewBlock(header, nil)

	_, err := ce.EvaluateBlock(block, builder)
	if err != nil {
		t.Fatalf("first EvaluateBlock: %v", err)
	}

	_, err = ce.EvaluateBlock(block, builder)
	if !errors.Is(err, ErrComplianceDuplicateEval) {
		t.Errorf("expected ErrComplianceDuplicateEval, got %v", err)
	}
}

func TestComplianceEngineEvaluateWithPartialInclusion(t *testing.T) {
	ce := NewComplianceEngine(DefaultComplianceEngineConfig())
	builder := types.HexToAddress("0xbuilderbuilderbuilderbuilderbuilderbuilderbui")

	tx1 := ilTx(0, 21000, 1)
	tx2 := ilTx(1, 21000, 2)

	il := &InclusionList{
		Slot: 5,
		Entries: []InclusionListEntry{
			{Transaction: ilEncode(t, tx1)},
			{Transaction: ilEncode(t, tx2)},
		},
	}
	ce.AddInclusionList(il)

	// Block only includes tx1.
	header := &types.Header{Number: big.NewInt(5), GasLimit: 1_000_000}
	body := &types.Body{Transactions: []*types.Transaction{tx1}}
	block := types.NewBlock(header, body)

	result, err := ce.EvaluateBlock(block, builder)
	if err != nil {
		t.Fatalf("EvaluateBlock: %v", err)
	}

	if result.TotalRequired != 2 {
		t.Errorf("TotalRequired = %d, want 2", result.TotalRequired)
	}
	if result.TotalIncluded != 1 {
		t.Errorf("TotalIncluded = %d, want 1", result.TotalIncluded)
	}
	if result.ComplianceRate != 0.5 {
		t.Errorf("ComplianceRate = %f, want 0.5", result.ComplianceRate)
	}
	// Default threshold is 0.75, so 50% is not compliant.
	if result.Compliant {
		t.Error("50% inclusion should not be compliant (threshold 0.75)")
	}
	if len(result.MissingTxs) != 1 {
		t.Errorf("MissingTxs count = %d, want 1", len(result.MissingTxs))
	}
}

func TestComplianceEngineScorePenaltyOnViolation(t *testing.T) {
	cfg := DefaultComplianceEngineConfig()
	cfg.BasePenalty = 10.0
	cfg.EscalationFactor = 1.0
	ce := NewComplianceEngine(cfg)
	builder := types.HexToAddress("0xbuilderbuilderbuilderbuilderbuilderbuilderbui")
	ce.RegisterBuilder(builder)

	tx := ilTx(0, 21000, 1)
	il := &InclusionList{
		Slot:    7,
		Entries: []InclusionListEntry{{Transaction: ilEncode(t, tx)}},
	}
	ce.AddInclusionList(il)

	// Block without the required transaction.
	header := &types.Header{Number: big.NewInt(7), GasLimit: 1_000_000}
	block := types.NewBlock(header, nil)
	ce.EvaluateBlock(block, builder)

	score, _ := ce.GetBuilderScore(builder)
	if score.Score >= 100.0 {
		t.Errorf("score should be penalized, got %f", score.Score)
	}
	if score.ViolationCount != 1 {
		t.Errorf("ViolationCount = %d, want 1", score.ViolationCount)
	}
}

// --- Validation table-driven tests ---

func TestValidateInclusionListTable(t *testing.T) {
	validTxBytes := ilEncode(t, ilTx(0, 21000, 1))

	tests := []struct {
		name    string
		il      *InclusionList
		wantErr error
	}{
		{
			name: "valid",
			il: &InclusionList{
				Slot:    100,
				Entries: []InclusionListEntry{{Transaction: validTxBytes, Index: 0}},
			},
			wantErr: nil,
		},
		{
			name: "zero slot",
			il: &InclusionList{
				Slot:    0,
				Entries: []InclusionListEntry{{Transaction: validTxBytes, Index: 0}},
			},
			wantErr: ErrZeroSlot,
		},
		{
			name: "empty entries",
			il: &InclusionList{
				Slot:    100,
				Entries: []InclusionListEntry{},
			},
			wantErr: ErrEmptyList,
		},
		{
			name: "empty transaction bytes",
			il: &InclusionList{
				Slot:    100,
				Entries: []InclusionListEntry{{Transaction: []byte{}, Index: 0}},
			},
			wantErr: ErrInvalidTransaction,
		},
		{
			name: "invalid transaction encoding",
			il: &InclusionList{
				Slot:    100,
				Entries: []InclusionListEntry{{Transaction: []byte{0xfe, 0xed}, Index: 0}},
			},
			wantErr: ErrInvalidTransaction,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateInclusionList(tc.il)
			if tc.wantErr == nil {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			} else {
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("expected %v, got %v", tc.wantErr, err)
				}
			}
		})
	}
}

// --- TotalGas / TotalBytes / TransactionHashes ---

func TestInclusionListTotalGas(t *testing.T) {
	tx1 := ilTx(0, 21000, 1)
	tx2 := ilTx(1, 42000, 2)

	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: ilEncode(t, tx1)},
			{Transaction: ilEncode(t, tx2)},
		},
	}

	gas := il.TotalGas()
	// TotalGas decodes each entry and sums the Gas fields.
	if gas != 63000 {
		t.Errorf("TotalGas = %d, want 63000", gas)
	}
}

func TestInclusionListTotalBytes(t *testing.T) {
	tx := ilTx(0, 21000, 1)
	txBytes := ilEncode(t, tx)

	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: txBytes},
			{Transaction: txBytes},
		},
	}

	totalBytes := il.TotalBytes()
	expectedBytes := len(txBytes) * 2
	if totalBytes != expectedBytes {
		t.Errorf("TotalBytes = %d, want %d", totalBytes, expectedBytes)
	}
}

func TestInclusionListTransactionHashes(t *testing.T) {
	tx1 := ilTx(0, 21000, 1)
	tx2 := ilTx(1, 21000, 2)

	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: ilEncode(t, tx1)},
			{Transaction: ilEncode(t, tx2)},
		},
	}

	hashes := il.TransactionHashes()
	if len(hashes) != 2 {
		t.Fatalf("TransactionHashes count = %d, want 2", len(hashes))
	}

	// Hashes should match the original transaction hashes.
	if hashes[0] != tx1.Hash() {
		t.Errorf("hash[0] mismatch: got %s, want %s", hashes[0].Hex(), tx1.Hash().Hex())
	}
	if hashes[1] != tx2.Hash() {
		t.Errorf("hash[1] mismatch: got %s, want %s", hashes[1].Hex(), tx2.Hash().Hex())
	}
}
