package focil

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// --- ViolationDetector creation ---

func TestNewViolationDetector(t *testing.T) {
	vd := NewViolationDetector(DefaultViolationDetectorConfig())
	if vd == nil {
		t.Fatal("NewViolationDetector returned nil")
	}
}

func TestNewViolationDetectorDefaults(t *testing.T) {
	// Zero config should be filled with sensible defaults.
	vd := NewViolationDetector(ViolationDetectorConfig{})
	if vd.config.MissingTxPenaltyGwei == 0 {
		t.Error("MissingTxPenaltyGwei should have a default")
	}
	if vd.config.ConflictingILPenaltyGwei == 0 {
		t.Error("ConflictingILPenaltyGwei should have a default")
	}
	if vd.config.AbsentPenaltyGwei == 0 {
		t.Error("AbsentPenaltyGwei should have a default")
	}
}

// --- ViolationType String ---

func TestViolationTypeString(t *testing.T) {
	tests := []struct {
		v    ViolationType
		want string
	}{
		{MissingTransaction, "missing_transaction"},
		{DelayedSubmission, "delayed_submission"},
		{ConflictingIL, "conflicting_il"},
		{CommitteeAbsent, "committee_absent"},
		{ViolationType(255), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.v.String(); got != tt.want {
			t.Errorf("ViolationType(%d).String() = %q, want %q", tt.v, got, tt.want)
		}
	}
}

// --- CheckBlockCompliance ---

func TestCheckBlockComplianceAllIncluded(t *testing.T) {
	vd := NewViolationDetector(DefaultViolationDetectorConfig())

	tx1 := mkTx(0, 21000, 1)
	tx2 := mkTx(1, 21000, 2)
	tx1Bytes := encTx(t, tx1)
	tx2Bytes := encTx(t, tx2)

	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: tx1Bytes},
			{Transaction: tx2Bytes},
		},
	}

	header := &types.Header{Number: big.NewInt(100), GasLimit: 1_000_000}
	body := &types.Body{Transactions: []*types.Transaction{tx1, tx2}}
	block := types.NewBlock(header, body)

	violations := vd.CheckBlockCompliance(block, []*InclusionList{il})
	if len(violations) != 0 {
		t.Errorf("violations = %d, want 0", len(violations))
	}
}

func TestCheckBlockComplianceMissingTx(t *testing.T) {
	vd := NewViolationDetector(DefaultViolationDetectorConfig())

	tx1 := mkTx(0, 21000, 1)
	tx2 := mkTx(1, 21000, 2) // in IL but not in block
	tx1Bytes := encTx(t, tx1)
	tx2Bytes := encTx(t, tx2)

	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: tx1Bytes},
			{Transaction: tx2Bytes},
		},
	}

	header := &types.Header{Number: big.NewInt(100), GasLimit: 1_000_000}
	body := &types.Body{Transactions: []*types.Transaction{tx1}} // only tx1
	block := types.NewBlock(header, body)

	violations := vd.CheckBlockCompliance(block, []*InclusionList{il})
	if len(violations) != 1 {
		t.Fatalf("violations = %d, want 1", len(violations))
	}
	if violations[0].Type != MissingTransaction {
		t.Errorf("type = %s, want missing_transaction", violations[0].Type)
	}
	if violations[0].Slot != 100 {
		t.Errorf("slot = %d, want 100", violations[0].Slot)
	}
}

func TestCheckBlockComplianceNilBlock(t *testing.T) {
	vd := NewViolationDetector(DefaultViolationDetectorConfig())
	il := &InclusionList{Slot: 100}
	violations := vd.CheckBlockCompliance(nil, []*InclusionList{il})
	if violations != nil {
		t.Error("nil block should return nil violations")
	}
}

func TestCheckBlockComplianceNoILs(t *testing.T) {
	vd := NewViolationDetector(DefaultViolationDetectorConfig())
	header := &types.Header{Number: big.NewInt(1), GasLimit: 1_000_000}
	block := types.NewBlock(header, nil)
	violations := vd.CheckBlockCompliance(block, nil)
	if violations != nil {
		t.Error("no ILs should return nil violations")
	}
}

func TestCheckBlockComplianceMultipleILs(t *testing.T) {
	vd := NewViolationDetector(DefaultViolationDetectorConfig())

	tx1 := mkTx(0, 21000, 1)
	tx2 := mkTx(1, 21000, 2)
	tx3 := mkTx(2, 21000, 3) // missing from block

	il1 := &InclusionList{
		Slot:    50,
		Entries: []InclusionListEntry{{Transaction: encTx(t, tx1)}},
	}
	il2 := &InclusionList{
		Slot: 50,
		Entries: []InclusionListEntry{
			{Transaction: encTx(t, tx2)},
			{Transaction: encTx(t, tx3)},
		},
	}

	header := &types.Header{Number: big.NewInt(50), GasLimit: 1_000_000}
	body := &types.Body{Transactions: []*types.Transaction{tx1, tx2}}
	block := types.NewBlock(header, body)

	violations := vd.CheckBlockCompliance(block, []*InclusionList{il1, il2})
	if len(violations) != 1 {
		t.Fatalf("violations = %d, want 1 (tx3 missing)", len(violations))
	}
}

func TestCheckBlockComplianceStoresViolations(t *testing.T) {
	vd := NewViolationDetector(DefaultViolationDetectorConfig())

	tx := mkTx(0, 21000, 1) // in IL but not in block
	il := &InclusionList{
		Slot:    200,
		Entries: []InclusionListEntry{{Transaction: encTx(t, tx)}},
	}

	header := &types.Header{Number: big.NewInt(200), GasLimit: 1_000_000}
	block := types.NewBlock(header, nil)
	vd.CheckBlockCompliance(block, []*InclusionList{il})

	if vd.ViolationCount(200) != 1 {
		t.Errorf("stored violations = %d, want 1", vd.ViolationCount(200))
	}
}

// --- DetectMissingTransactions ---

func TestDetectMissingTransactionsNone(t *testing.T) {
	block := map[types.Hash]bool{
		types.HexToHash("0x01"): true,
		types.HexToHash("0x02"): true,
	}
	il := map[types.Hash]bool{
		types.HexToHash("0x01"): true,
		types.HexToHash("0x02"): true,
	}
	missing := DetectMissingTransactions(block, il)
	if len(missing) != 0 {
		t.Errorf("missing = %d, want 0", len(missing))
	}
}

func TestDetectMissingTransactionsSome(t *testing.T) {
	block := map[types.Hash]bool{
		types.HexToHash("0x01"): true,
	}
	il := map[types.Hash]bool{
		types.HexToHash("0x01"): true,
		types.HexToHash("0x02"): true,
		types.HexToHash("0x03"): true,
	}
	missing := DetectMissingTransactions(block, il)
	if len(missing) != 2 {
		t.Errorf("missing = %d, want 2", len(missing))
	}
}

func TestDetectMissingTransactionsEmpty(t *testing.T) {
	missing := DetectMissingTransactions(nil, nil)
	if len(missing) != 0 {
		t.Errorf("empty inputs: missing = %d, want 0", len(missing))
	}
}

func TestDetectMissingFromSlices(t *testing.T) {
	blockHashes := []types.Hash{types.HexToHash("0x01")}
	ilHashes := []types.Hash{types.HexToHash("0x01"), types.HexToHash("0x02")}

	missing := DetectMissingFromSlices(blockHashes, ilHashes)
	if len(missing) != 1 {
		t.Errorf("missing = %d, want 1", len(missing))
	}
}

// --- DetectConflicting ---

func TestDetectConflictingTrue(t *testing.T) {
	tx1 := mkTx(0, 21000, 1)
	tx2 := mkTx(1, 21000, 2)

	il1 := &InclusionList{
		Slot:          100,
		ProposerIndex: 42,
		Entries:       []InclusionListEntry{{Transaction: encTx(t, tx1)}},
	}
	il2 := &InclusionList{
		Slot:          100,
		ProposerIndex: 42,
		Entries:       []InclusionListEntry{{Transaction: encTx(t, tx2)}},
	}

	if !DetectConflicting(il1, il2) {
		t.Error("different tx sets from same proposer/slot should conflict")
	}
}

func TestDetectConflictingFalseSameContent(t *testing.T) {
	tx := mkTx(0, 21000, 1)
	txBytes := encTx(t, tx)

	il1 := &InclusionList{
		Slot:          100,
		ProposerIndex: 42,
		Entries:       []InclusionListEntry{{Transaction: txBytes}},
	}
	il2 := &InclusionList{
		Slot:          100,
		ProposerIndex: 42,
		Entries:       []InclusionListEntry{{Transaction: txBytes}},
	}

	if DetectConflicting(il1, il2) {
		t.Error("identical content from same proposer should not conflict")
	}
}

func TestDetectConflictingDifferentProposer(t *testing.T) {
	tx1 := mkTx(0, 21000, 1)
	tx2 := mkTx(1, 21000, 2)

	il1 := &InclusionList{
		Slot:          100,
		ProposerIndex: 1,
		Entries:       []InclusionListEntry{{Transaction: encTx(t, tx1)}},
	}
	il2 := &InclusionList{
		Slot:          100,
		ProposerIndex: 2, // different proposer
		Entries:       []InclusionListEntry{{Transaction: encTx(t, tx2)}},
	}

	if DetectConflicting(il1, il2) {
		t.Error("different proposers cannot conflict")
	}
}

func TestDetectConflictingDifferentSlot(t *testing.T) {
	tx1 := mkTx(0, 21000, 1)
	tx2 := mkTx(1, 21000, 2)

	il1 := &InclusionList{
		Slot:          100,
		ProposerIndex: 42,
		Entries:       []InclusionListEntry{{Transaction: encTx(t, tx1)}},
	}
	il2 := &InclusionList{
		Slot:          101, // different slot
		ProposerIndex: 42,
		Entries:       []InclusionListEntry{{Transaction: encTx(t, tx2)}},
	}

	if DetectConflicting(il1, il2) {
		t.Error("different slots cannot conflict")
	}
}

func TestDetectConflictingNil(t *testing.T) {
	if DetectConflicting(nil, nil) {
		t.Error("nil ILs should not conflict")
	}
	il := &InclusionList{Slot: 1, ProposerIndex: 1}
	if DetectConflicting(il, nil) {
		t.Error("nil il2 should not conflict")
	}
	if DetectConflicting(nil, il) {
		t.Error("nil il1 should not conflict")
	}
}

func TestDetectConflictingDifferentLengths(t *testing.T) {
	tx1 := mkTx(0, 21000, 1)
	tx2 := mkTx(1, 21000, 2)

	il1 := &InclusionList{
		Slot:          100,
		ProposerIndex: 42,
		Entries:       []InclusionListEntry{{Transaction: encTx(t, tx1)}},
	}
	il2 := &InclusionList{
		Slot:          100,
		ProposerIndex: 42,
		Entries: []InclusionListEntry{
			{Transaction: encTx(t, tx1)},
			{Transaction: encTx(t, tx2)},
		},
	}

	if !DetectConflicting(il1, il2) {
		t.Error("different length tx sets should conflict")
	}
}

// --- ComputeViolationPenalty ---

func TestComputeViolationPenalty(t *testing.T) {
	cfg := DefaultViolationDetectorConfig()
	vd := NewViolationDetector(cfg)

	tests := []struct {
		vtype ViolationType
		want  uint64
	}{
		{MissingTransaction, cfg.MissingTxPenaltyGwei},
		{DelayedSubmission, cfg.DelayedSubmissionPenaltyGwei},
		{ConflictingIL, cfg.ConflictingILPenaltyGwei},
		{CommitteeAbsent, cfg.AbsentPenaltyGwei},
		{ViolationType(255), 0}, // unknown type
	}
	for _, tt := range tests {
		v := Violation{Type: tt.vtype}
		got := vd.ComputeViolationPenalty(v)
		if got != tt.want {
			t.Errorf("penalty for %s = %d, want %d", tt.vtype, got, tt.want)
		}
	}
}

// --- RecordAbsentMember ---

func TestRecordAbsentMember(t *testing.T) {
	vd := NewViolationDetector(DefaultViolationDetectorConfig())
	vd.RecordAbsentMember(42, 300)

	vs := vd.GetViolations(300)
	if len(vs) != 1 {
		t.Fatalf("violations = %d, want 1", len(vs))
	}
	if vs[0].Type != CommitteeAbsent {
		t.Errorf("type = %s, want committee_absent", vs[0].Type)
	}
	if vs[0].ValidatorIndex != 42 {
		t.Errorf("validator = %d, want 42", vs[0].ValidatorIndex)
	}
}

// --- RecordConflictingIL ---

func TestRecordConflictingIL(t *testing.T) {
	vd := NewViolationDetector(DefaultViolationDetectorConfig())
	evidence := types.HexToHash("0xdead")
	vd.RecordConflictingIL(99, 400, evidence)

	vs := vd.GetViolations(400)
	if len(vs) != 1 {
		t.Fatalf("violations = %d, want 1", len(vs))
	}
	if vs[0].Type != ConflictingIL {
		t.Errorf("type = %s, want conflicting_il", vs[0].Type)
	}
	if vs[0].Evidence != evidence {
		t.Errorf("evidence mismatch")
	}
}

// --- RecordDelayedSubmission ---

func TestRecordDelayedSubmission(t *testing.T) {
	vd := NewViolationDetector(DefaultViolationDetectorConfig())
	vd.RecordDelayedSubmission(7, 500)

	vs := vd.GetViolations(500)
	if len(vs) != 1 {
		t.Fatalf("violations = %d, want 1", len(vs))
	}
	if vs[0].Type != DelayedSubmission {
		t.Errorf("type = %s, want delayed_submission", vs[0].Type)
	}
}

// --- GenerateReport ---

func TestGenerateReport(t *testing.T) {
	vd := NewViolationDetector(DefaultViolationDetectorConfig())

	// Record various violations.
	vd.RecordAbsentMember(1, 100)
	vd.RecordAbsentMember(2, 101)
	vd.RecordConflictingIL(3, 102, types.Hash{})
	vd.RecordDelayedSubmission(4, 103)

	report := vd.GenerateReport(100, 103)

	if len(report.Violations) != 4 {
		t.Errorf("violations = %d, want 4", len(report.Violations))
	}
	if report.StartSlot != 100 {
		t.Errorf("StartSlot = %d, want 100", report.StartSlot)
	}
	if report.EndSlot != 103 {
		t.Errorf("EndSlot = %d, want 103", report.EndSlot)
	}
	if report.ByType[CommitteeAbsent] != 2 {
		t.Errorf("absent count = %d, want 2", report.ByType[CommitteeAbsent])
	}
	if report.ByType[ConflictingIL] != 1 {
		t.Errorf("conflicting count = %d, want 1", report.ByType[ConflictingIL])
	}
	if report.ByType[DelayedSubmission] != 1 {
		t.Errorf("delayed count = %d, want 1", report.ByType[DelayedSubmission])
	}
	if report.TotalPenaltyGwei == 0 {
		t.Error("TotalPenaltyGwei should be > 0")
	}
}

func TestGenerateReportEmpty(t *testing.T) {
	vd := NewViolationDetector(DefaultViolationDetectorConfig())
	report := vd.GenerateReport(1, 100)
	if len(report.Violations) != 0 {
		t.Errorf("violations = %d, want 0", len(report.Violations))
	}
	if report.TotalPenaltyGwei != 0 {
		t.Errorf("TotalPenaltyGwei = %d, want 0", report.TotalPenaltyGwei)
	}
}

// --- GetViolations ---

func TestGetViolationsEmpty(t *testing.T) {
	vd := NewViolationDetector(DefaultViolationDetectorConfig())
	vs := vd.GetViolations(999)
	if vs != nil {
		t.Errorf("empty slot: violations = %v, want nil", vs)
	}
}

// --- PruneBefore ---

func TestViolationPruneBefore(t *testing.T) {
	vd := NewViolationDetector(DefaultViolationDetectorConfig())
	vd.RecordAbsentMember(1, 10)
	vd.RecordAbsentMember(2, 20)
	vd.RecordAbsentMember(3, 30)

	pruned := vd.PruneBefore(25)
	if pruned != 2 {
		t.Errorf("pruned = %d, want 2", pruned)
	}
	if vd.ViolationCount(10) != 0 {
		t.Error("slot 10 should be pruned")
	}
	if vd.ViolationCount(20) != 0 {
		t.Error("slot 20 should be pruned")
	}
	if vd.ViolationCount(30) != 1 {
		t.Error("slot 30 should remain")
	}
}

// --- ViolationCount ---

func TestViolationCount(t *testing.T) {
	vd := NewViolationDetector(DefaultViolationDetectorConfig())
	if vd.ViolationCount(1) != 0 {
		t.Error("empty slot should have 0 violations")
	}
	vd.RecordAbsentMember(1, 1)
	vd.RecordAbsentMember(2, 1)
	if vd.ViolationCount(1) != 2 {
		t.Errorf("count = %d, want 2", vd.ViolationCount(1))
	}
}
