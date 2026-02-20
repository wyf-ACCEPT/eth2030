package focil

import (
	"math/big"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// ceAddr creates a deterministic address for testing.
func ceAddr(b byte) types.Address {
	var a types.Address
	a[0] = b
	return a
}

// ceMakeTx creates a legacy transaction for testing.
func ceMakeTx(nonce uint64, gas uint64) *types.Transaction {
	to := types.HexToAddress("0xdeaddeaddeaddeaddeaddeaddeaddeaddeaddead")
	return types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(1000),
		Gas:      gas,
		To:       &to,
		Value:    big.NewInt(0),
		V:        big.NewInt(27),
		R:        big.NewInt(int64(nonce + 1)),
		S:        big.NewInt(int64(nonce + 1)),
	})
}

// ceEncodeTx encodes a transaction to RLP bytes.
func ceEncodeTx(t *testing.T, tx *types.Transaction) []byte {
	t.Helper()
	data, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP: %v", err)
	}
	return data
}

// ceMakeIL creates an inclusion list with the given transactions.
func ceMakeIL(slot uint64, txs []*types.Transaction, t *testing.T) *InclusionList {
	t.Helper()
	entries := make([]InclusionListEntry, len(txs))
	for i, tx := range txs {
		entries[i] = InclusionListEntry{
			Transaction: ceEncodeTx(t, tx),
			Index:       uint64(i),
		}
	}
	return &InclusionList{
		Slot:    slot,
		Entries: entries,
	}
}

// ceMakeBlock creates a block at the given number with the given transactions.
func ceMakeBlock(number uint64, txs []*types.Transaction) *types.Block {
	header := &types.Header{
		Number:   big.NewInt(int64(number)),
		GasLimit: 30_000_000,
	}
	body := &types.Body{Transactions: txs}
	return types.NewBlock(header, body)
}

// --- NewComplianceEngine ---

func TestComplianceEngineNew(t *testing.T) {
	ce := NewComplianceEngine(DefaultComplianceEngineConfig())
	if ce == nil {
		t.Fatal("NewComplianceEngine returned nil")
	}
	if ce.EvaluationCount() != 0 {
		t.Errorf("initial evaluation count = %d, want 0", ce.EvaluationCount())
	}
	if ce.BuilderCount() != 0 {
		t.Errorf("initial builder count = %d, want 0", ce.BuilderCount())
	}
}

func TestComplianceEngineDefaultConfig(t *testing.T) {
	cfg := DefaultComplianceEngineConfig()
	if cfg.ComplianceThreshold != 0.75 {
		t.Errorf("default threshold = %f, want 0.75", cfg.ComplianceThreshold)
	}
	if cfg.BasePenalty != 5.0 {
		t.Errorf("default base penalty = %f, want 5.0", cfg.BasePenalty)
	}
	if cfg.MaxScore != 100.0 {
		t.Errorf("default max score = %f, want 100.0", cfg.MaxScore)
	}
}

// --- AddInclusionList ---

func TestComplianceAddInclusionList(t *testing.T) {
	ce := NewComplianceEngine(DefaultComplianceEngineConfig())
	tx := ceMakeTx(0, 21000)
	il := ceMakeIL(100, []*types.Transaction{tx}, t)

	if err := ce.AddInclusionList(il); err != nil {
		t.Fatalf("AddInclusionList: %v", err)
	}
	if ce.InclusionListCount(100) != 1 {
		t.Errorf("IL count = %d, want 1", ce.InclusionListCount(100))
	}
}

func TestComplianceAddInclusionListNil(t *testing.T) {
	ce := NewComplianceEngine(DefaultComplianceEngineConfig())
	if err := ce.AddInclusionList(nil); err == nil {
		t.Error("expected error for nil inclusion list")
	}
}

func TestComplianceAddInclusionListZeroSlot(t *testing.T) {
	ce := NewComplianceEngine(DefaultComplianceEngineConfig())
	il := &InclusionList{Slot: 0}
	if err := ce.AddInclusionList(il); err != ErrComplianceZeroSlot {
		t.Errorf("got %v, want ErrComplianceZeroSlot", err)
	}
}

// --- EvaluateBlock ---

func TestComplianceEvaluateBlockFullCompliance(t *testing.T) {
	ce := NewComplianceEngine(DefaultComplianceEngineConfig())
	builder := ceAddr(0x01)

	tx1 := ceMakeTx(0, 21000)
	tx2 := ceMakeTx(1, 21000)
	il := ceMakeIL(100, []*types.Transaction{tx1, tx2}, t)
	ce.AddInclusionList(il)

	block := ceMakeBlock(100, []*types.Transaction{tx1, tx2})

	result, err := ce.EvaluateBlock(block, builder)
	if err != nil {
		t.Fatalf("EvaluateBlock: %v", err)
	}
	if !result.Compliant {
		t.Error("expected compliant block")
	}
	if result.ComplianceRate != 1.0 {
		t.Errorf("compliance rate = %f, want 1.0", result.ComplianceRate)
	}
	if result.TotalRequired != 2 {
		t.Errorf("total required = %d, want 2", result.TotalRequired)
	}
	if result.TotalIncluded != 2 {
		t.Errorf("total included = %d, want 2", result.TotalIncluded)
	}
	if len(result.MissingTxs) != 0 {
		t.Errorf("missing txs = %d, want 0", len(result.MissingTxs))
	}
}

func TestComplianceEvaluateBlockPartialCompliance(t *testing.T) {
	cfg := DefaultComplianceEngineConfig()
	cfg.ComplianceThreshold = 0.75
	ce := NewComplianceEngine(cfg)
	builder := ceAddr(0x01)

	tx1 := ceMakeTx(0, 21000)
	tx2 := ceMakeTx(1, 21000)
	tx3 := ceMakeTx(2, 21000)
	tx4 := ceMakeTx(3, 21000)
	il := ceMakeIL(100, []*types.Transaction{tx1, tx2, tx3, tx4}, t)
	ce.AddInclusionList(il)

	// Include 3 out of 4 (75% = threshold).
	block := ceMakeBlock(100, []*types.Transaction{tx1, tx2, tx3})

	result, err := ce.EvaluateBlock(block, builder)
	if err != nil {
		t.Fatalf("EvaluateBlock: %v", err)
	}
	if !result.Compliant {
		t.Error("expected compliant at exactly the threshold (75%)")
	}
	if result.TotalIncluded != 3 {
		t.Errorf("included = %d, want 3", result.TotalIncluded)
	}
	if len(result.MissingTxs) != 1 {
		t.Errorf("missing = %d, want 1", len(result.MissingTxs))
	}
}

func TestComplianceEvaluateBlockNonCompliant(t *testing.T) {
	cfg := DefaultComplianceEngineConfig()
	cfg.ComplianceThreshold = 0.75
	ce := NewComplianceEngine(cfg)
	builder := ceAddr(0x01)

	tx1 := ceMakeTx(0, 21000)
	tx2 := ceMakeTx(1, 21000)
	tx3 := ceMakeTx(2, 21000)
	tx4 := ceMakeTx(3, 21000)
	il := ceMakeIL(100, []*types.Transaction{tx1, tx2, tx3, tx4}, t)
	ce.AddInclusionList(il)

	// Include only 2 out of 4 (50% < 75% threshold).
	block := ceMakeBlock(100, []*types.Transaction{tx1, tx2})

	result, err := ce.EvaluateBlock(block, builder)
	if err != nil {
		t.Fatalf("EvaluateBlock: %v", err)
	}
	if result.Compliant {
		t.Error("expected non-compliant block (50% < 75%)")
	}
	if result.ComplianceRate != 0.5 {
		t.Errorf("compliance rate = %f, want 0.5", result.ComplianceRate)
	}
}

func TestComplianceEvaluateBlockNoLists(t *testing.T) {
	ce := NewComplianceEngine(DefaultComplianceEngineConfig())
	builder := ceAddr(0x01)

	block := ceMakeBlock(100, nil)
	result, err := ce.EvaluateBlock(block, builder)
	if err != nil {
		t.Fatalf("EvaluateBlock: %v", err)
	}
	if !result.Compliant {
		t.Error("block with no ILs should be vacuously compliant")
	}
	if result.ComplianceRate != 1.0 {
		t.Errorf("rate = %f, want 1.0", result.ComplianceRate)
	}
}

func TestComplianceEvaluateBlockNilBlock(t *testing.T) {
	ce := NewComplianceEngine(DefaultComplianceEngineConfig())
	_, err := ce.EvaluateBlock(nil, ceAddr(0x01))
	if err != ErrComplianceNilBlock {
		t.Errorf("got %v, want ErrComplianceNilBlock", err)
	}
}

func TestComplianceEvaluateBlockZeroSlot(t *testing.T) {
	ce := NewComplianceEngine(DefaultComplianceEngineConfig())
	block := ceMakeBlock(0, nil)
	_, err := ce.EvaluateBlock(block, ceAddr(0x01))
	if err != ErrComplianceZeroSlot {
		t.Errorf("got %v, want ErrComplianceZeroSlot", err)
	}
}

func TestComplianceEvaluateBlockDuplicate(t *testing.T) {
	ce := NewComplianceEngine(DefaultComplianceEngineConfig())
	builder := ceAddr(0x01)
	block := ceMakeBlock(100, nil)

	ce.EvaluateBlock(block, builder)
	_, err := ce.EvaluateBlock(block, builder)
	if err == nil {
		t.Error("expected error for duplicate evaluation")
	}
}

// --- GetBuilderScore ---

func TestComplianceGetBuilderScoreAfterCompliant(t *testing.T) {
	ce := NewComplianceEngine(DefaultComplianceEngineConfig())
	builder := ceAddr(0x01)

	block := ceMakeBlock(100, nil) // no ILs = vacuously compliant
	ce.EvaluateBlock(block, builder)

	score, err := ce.GetBuilderScore(builder)
	if err != nil {
		t.Fatalf("GetBuilderScore: %v", err)
	}
	// Initial 100 + recovery 2.0 = 102 -> clamped to 100.
	if score.Score != 100.0 {
		t.Errorf("score = %f, want 100.0 (clamped)", score.Score)
	}
	if score.TotalEvaluations != 1 {
		t.Errorf("evaluations = %d, want 1", score.TotalEvaluations)
	}
	if score.CompliantCount != 1 {
		t.Errorf("compliant count = %d, want 1", score.CompliantCount)
	}
}

func TestComplianceGetBuilderScoreAfterViolation(t *testing.T) {
	cfg := DefaultComplianceEngineConfig()
	cfg.ComplianceThreshold = 1.0 // require all txs
	cfg.BasePenalty = 10.0
	ce := NewComplianceEngine(cfg)
	builder := ceAddr(0x01)

	tx := ceMakeTx(0, 21000)
	il := ceMakeIL(100, []*types.Transaction{tx}, t)
	ce.AddInclusionList(il)

	// Block missing the required tx.
	block := ceMakeBlock(100, nil)
	ce.EvaluateBlock(block, builder)

	score, err := ce.GetBuilderScore(builder)
	if err != nil {
		t.Fatalf("GetBuilderScore: %v", err)
	}
	if score.ViolationCount != 1 {
		t.Errorf("violations = %d, want 1", score.ViolationCount)
	}
	if score.Score >= 100.0 {
		t.Errorf("score = %f, should be < 100 after violation", score.Score)
	}
}

func TestComplianceGetBuilderScoreUnknown(t *testing.T) {
	ce := NewComplianceEngine(DefaultComplianceEngineConfig())
	_, err := ce.GetBuilderScore(ceAddr(0x99))
	if err == nil {
		t.Error("expected error for unknown builder")
	}
}

// --- ApplyPenalty ---

func TestComplianceApplyPenalty(t *testing.T) {
	ce := NewComplianceEngine(DefaultComplianceEngineConfig())
	builder := ceAddr(0x01)
	ce.RegisterBuilder(builder)

	err := ce.ApplyPenalty(builder, 30.0)
	if err != nil {
		t.Fatalf("ApplyPenalty: %v", err)
	}

	score, _ := ce.GetBuilderScore(builder)
	if score.Score != 70.0 {
		t.Errorf("score = %f, want 70.0", score.Score)
	}
	if score.TotalPenalties != 30.0 {
		t.Errorf("total penalties = %f, want 30.0", score.TotalPenalties)
	}
}

func TestComplianceApplyPenaltyClampZero(t *testing.T) {
	ce := NewComplianceEngine(DefaultComplianceEngineConfig())
	builder := ceAddr(0x01)
	ce.RegisterBuilder(builder)

	ce.ApplyPenalty(builder, 200.0)
	score, _ := ce.GetBuilderScore(builder)
	if score.Score != 0 {
		t.Errorf("score = %f, want 0 (clamped)", score.Score)
	}
}

func TestComplianceApplyPenaltyUnknown(t *testing.T) {
	ce := NewComplianceEngine(DefaultComplianceEngineConfig())
	err := ce.ApplyPenalty(ceAddr(0x99), 10.0)
	if err == nil {
		t.Error("expected error for unknown builder")
	}
}

func TestComplianceApplyPenaltyZeroAmount(t *testing.T) {
	ce := NewComplianceEngine(DefaultComplianceEngineConfig())
	ce.RegisterBuilder(ceAddr(0x01))
	// Zero penalty should be a no-op.
	err := ce.ApplyPenalty(ceAddr(0x01), 0)
	if err != nil {
		t.Errorf("zero penalty should be no-op, got %v", err)
	}
}

// --- Escalating penalties ---

func TestComplianceEscalatingPenalties(t *testing.T) {
	cfg := DefaultComplianceEngineConfig()
	cfg.ComplianceThreshold = 1.0
	cfg.BasePenalty = 10.0
	cfg.EscalationFactor = 2.0
	cfg.InitialScore = 100.0
	ce := NewComplianceEngine(cfg)
	builder := ceAddr(0x01)

	// Create violations at different slots.
	for slot := uint64(1); slot <= 3; slot++ {
		tx := ceMakeTx(slot*10, 21000)
		il := ceMakeIL(slot, []*types.Transaction{tx}, t)
		ce.AddInclusionList(il)
		block := ceMakeBlock(slot, nil) // empty block = no compliance
		ce.EvaluateBlock(block, builder)
	}

	score, _ := ce.GetBuilderScore(builder)
	// 3 consecutive violations with escalation factor 2:
	// penalty 1: 10, penalty 2: 20, penalty 3: 40 = total 70
	if score.ConsecutiveViolations != 3 {
		t.Errorf("consecutive violations = %d, want 3", score.ConsecutiveViolations)
	}
	if score.TotalPenalties != 70.0 {
		t.Errorf("total penalties = %f, want 70.0", score.TotalPenalties)
	}
}

// --- RegisterBuilder ---

func TestComplianceRegisterBuilder(t *testing.T) {
	ce := NewComplianceEngine(DefaultComplianceEngineConfig())
	ce.RegisterBuilder(ceAddr(0x01))
	if ce.BuilderCount() != 1 {
		t.Errorf("BuilderCount = %d, want 1", ce.BuilderCount())
	}
	// Registering again should be a no-op.
	ce.RegisterBuilder(ceAddr(0x01))
	if ce.BuilderCount() != 1 {
		t.Errorf("BuilderCount after re-register = %d, want 1", ce.BuilderCount())
	}
}

// --- GetBlockCompliance ---

func TestComplianceGetBlockCompliance(t *testing.T) {
	ce := NewComplianceEngine(DefaultComplianceEngineConfig())
	block := ceMakeBlock(100, nil)
	ce.EvaluateBlock(block, ceAddr(0x01))

	result, err := ce.GetBlockCompliance(100)
	if err != nil {
		t.Fatalf("GetBlockCompliance: %v", err)
	}
	if !result.Compliant {
		t.Error("expected compliant")
	}
}

func TestComplianceGetBlockComplianceNotFound(t *testing.T) {
	ce := NewComplianceEngine(DefaultComplianceEngineConfig())
	_, err := ce.GetBlockCompliance(999)
	if err == nil {
		t.Error("expected error for unknown slot")
	}
}

// --- PruneBefore ---

func TestCompliancePruneBefore(t *testing.T) {
	ce := NewComplianceEngine(DefaultComplianceEngineConfig())
	for slot := uint64(1); slot <= 10; slot++ {
		block := ceMakeBlock(slot, nil)
		ce.EvaluateBlock(block, ceAddr(0x01))
	}
	if ce.EvaluationCount() != 10 {
		t.Fatalf("expected 10 evaluations, got %d", ce.EvaluationCount())
	}

	pruned := ce.PruneBefore(6)
	if pruned != 5 {
		t.Errorf("pruned = %d, want 5", pruned)
	}
	if ce.EvaluationCount() != 5 {
		t.Errorf("remaining evaluations = %d, want 5", ce.EvaluationCount())
	}
}

// --- Multiple ILs per slot ---

func TestComplianceMultipleILsPerSlot(t *testing.T) {
	ce := NewComplianceEngine(DefaultComplianceEngineConfig())
	builder := ceAddr(0x01)

	tx1 := ceMakeTx(0, 21000)
	tx2 := ceMakeTx(1, 21000)
	tx3 := ceMakeTx(2, 21000)

	il1 := ceMakeIL(100, []*types.Transaction{tx1, tx2}, t)
	il2 := ceMakeIL(100, []*types.Transaction{tx2, tx3}, t)
	ce.AddInclusionList(il1)
	ce.AddInclusionList(il2)

	if ce.InclusionListCount(100) != 2 {
		t.Errorf("IL count = %d, want 2", ce.InclusionListCount(100))
	}

	// Block includes all 3 unique txs.
	block := ceMakeBlock(100, []*types.Transaction{tx1, tx2, tx3})
	result, err := ce.EvaluateBlock(block, builder)
	if err != nil {
		t.Fatalf("EvaluateBlock: %v", err)
	}
	if !result.Compliant {
		t.Error("expected compliant with all txs included")
	}
	// tx2 is in both ILs but should count only once.
	if result.TotalRequired != 3 {
		t.Errorf("total required = %d, want 3 (deduplicated)", result.TotalRequired)
	}
}

// --- Concurrent access ---

func TestComplianceConcurrentAccess(t *testing.T) {
	ce := NewComplianceEngine(DefaultComplianceEngineConfig())

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			builder := ceAddr(byte(slot % 5))
			block := ceMakeBlock(uint64(slot+1), nil)
			ce.EvaluateBlock(block, builder)
		}(i)
	}
	wg.Wait()

	if ce.EvaluationCount() != 20 {
		t.Errorf("evaluation count = %d, want 20", ce.EvaluationCount())
	}
}
