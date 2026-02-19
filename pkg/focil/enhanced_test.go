package focil

import (
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// --- Helpers ---

func makeHashes(n int) []types.Hash {
	hashes := make([]types.Hash, n)
	for i := range hashes {
		hashes[i] = types.Hash{byte(i + 1)}
	}
	return hashes
}

func makeConstraints(n int) []InclusionConstraint {
	cs := make([]InclusionConstraint, n)
	for i := range cs {
		cs[i] = InclusionConstraint{
			Type:   ConstraintMustInclude,
			Target: types.Address{byte(i + 1)},
		}
	}
	return cs
}

// --- ConstraintType tests ---

func TestConstraintTypeValues(t *testing.T) {
	if ConstraintMustInclude != 0 {
		t.Error("ConstraintMustInclude should be 0")
	}
	if ConstraintMustExclude != 1 {
		t.Error("ConstraintMustExclude should be 1")
	}
	if ConstraintGasLimit != 2 {
		t.Error("ConstraintGasLimit should be 2")
	}
	if ConstraintOrdering != 3 {
		t.Error("ConstraintOrdering should be 3")
	}
}

// --- EnhancedFOCILConfig tests ---

func TestDefaultEnhancedFOCILConfig(t *testing.T) {
	cfg := DefaultEnhancedFOCILConfig()
	if cfg.MaxTransactions != MAX_TRANSACTIONS_PER_INCLUSION_LIST {
		t.Errorf("MaxTransactions = %d, want %d",
			cfg.MaxTransactions, MAX_TRANSACTIONS_PER_INCLUSION_LIST)
	}
	if cfg.MaxConstraints <= 0 {
		t.Error("MaxConstraints should be positive")
	}
	if cfg.EnforcementStrength != 1.0 {
		t.Errorf("EnforcementStrength = %f, want 1.0", cfg.EnforcementStrength)
	}
}

// --- NewEnhancedFOCIL tests ---

func TestNewEnhancedFOCIL(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	if ef == nil {
		t.Fatal("NewEnhancedFOCIL returned nil")
	}
}

func TestNewEnhancedFOCILDefaultsZeroConfig(t *testing.T) {
	ef := NewEnhancedFOCIL(EnhancedFOCILConfig{})
	if ef.config.MaxTransactions <= 0 {
		t.Error("MaxTransactions should default to positive value")
	}
	if ef.config.MaxConstraints <= 0 {
		t.Error("MaxConstraints should default to positive value")
	}
}

// --- BuildInclusionList tests ---

func TestBuildInclusionListV2(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	hashes := makeHashes(5)

	list, err := ef.BuildInclusionList(100, hashes)
	if err != nil {
		t.Fatalf("BuildInclusionList: %v", err)
	}
	if list.Slot != 100 {
		t.Errorf("Slot = %d, want 100", list.Slot)
	}
	if len(list.Transactions) != 5 {
		t.Errorf("Transactions = %d, want 5", len(list.Transactions))
	}
	if len(list.Priority) != 5 {
		t.Errorf("Priority = %d, want 5", len(list.Priority))
	}
	// First tx should have highest priority.
	if list.Priority[0] <= list.Priority[4] {
		t.Error("first tx should have higher priority than last")
	}
}

func TestBuildInclusionListV2ZeroSlot(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	_, err := ef.BuildInclusionList(0, makeHashes(1))
	if err != ErrEnhancedZeroSlot {
		t.Errorf("err = %v, want ErrEnhancedZeroSlot", err)
	}
}

func TestBuildInclusionListV2Capped(t *testing.T) {
	ef := NewEnhancedFOCIL(EnhancedFOCILConfig{
		MaxTransactions: 3,
		MaxConstraints:  10,
	})
	hashes := makeHashes(10)

	list, err := ef.BuildInclusionList(1, hashes)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Transactions) != 3 {
		t.Errorf("Transactions = %d, want 3 (capped)", len(list.Transactions))
	}
}

func TestBuildInclusionListV2Deduplicates(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	h := types.Hash{0x01}
	hashes := []types.Hash{h, h, h}

	list, err := ef.BuildInclusionList(1, hashes)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Transactions) != 1 {
		t.Errorf("Transactions = %d, want 1 (deduplicated)", len(list.Transactions))
	}
}

func TestBuildInclusionListV2Empty(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	list, err := ef.BuildInclusionList(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Transactions) != 0 {
		t.Errorf("Transactions = %d, want 0", len(list.Transactions))
	}
}

func TestBuildInclusionListV2StoresInSlot(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	ef.BuildInclusionList(42, makeHashes(2))
	ef.BuildInclusionList(42, makeHashes(3))

	lists := ef.ListsForSlot(42)
	if len(lists) != 2 {
		t.Errorf("lists for slot 42 = %d, want 2", len(lists))
	}
}

// --- ValidateInclusionList tests ---

func TestValidateInclusionListV2Valid(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	list := &InclusionListV2{
		Slot:         100,
		Proposer:     types.Address{0x01},
		Transactions: makeHashes(3),
		Priority:     []uint64{3, 2, 1},
		MaxGas:       100000,
		Constraints: []InclusionConstraint{
			{Type: ConstraintMustInclude, Target: types.Address{0x01}},
		},
	}

	if err := ef.ValidateInclusionList(list); err != nil {
		t.Errorf("valid list: %v", err)
	}
}

func TestValidateInclusionListV2Nil(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	if err := ef.ValidateInclusionList(nil); err != ErrEnhancedNilList {
		t.Errorf("err = %v, want ErrEnhancedNilList", err)
	}
}

func TestValidateInclusionListV2ZeroSlot(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	list := &InclusionListV2{
		Slot:         0,
		Transactions: makeHashes(1),
		Priority:     []uint64{1},
	}
	if err := ef.ValidateInclusionList(list); err != ErrEnhancedZeroSlot {
		t.Errorf("err = %v, want ErrEnhancedZeroSlot", err)
	}
}

func TestValidateInclusionListV2EmptyTxs(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	list := &InclusionListV2{
		Slot:         1,
		Transactions: nil,
		Priority:     nil,
	}
	if err := ef.ValidateInclusionList(list); err != ErrEnhancedEmptyTxs {
		t.Errorf("err = %v, want ErrEnhancedEmptyTxs", err)
	}
}

func TestValidateInclusionListV2TooManyTxs(t *testing.T) {
	ef := NewEnhancedFOCIL(EnhancedFOCILConfig{
		MaxTransactions: 3,
		MaxConstraints:  10,
	})
	hashes := makeHashes(5)
	list := &InclusionListV2{
		Slot:         1,
		Transactions: hashes,
		Priority:     make([]uint64, 5),
	}
	err := ef.ValidateInclusionList(list)
	if err == nil {
		t.Error("expected error for too many txs")
	}
}

func TestValidateInclusionListV2TooManyConstraints(t *testing.T) {
	ef := NewEnhancedFOCIL(EnhancedFOCILConfig{
		MaxTransactions: 16,
		MaxConstraints:  2,
	})
	list := &InclusionListV2{
		Slot:         1,
		Transactions: makeHashes(1),
		Priority:     []uint64{1},
		Constraints:  makeConstraints(5),
	}
	err := ef.ValidateInclusionList(list)
	if err == nil {
		t.Error("expected error for too many constraints")
	}
}

func TestValidateInclusionListV2PriorityLenMismatch(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	list := &InclusionListV2{
		Slot:         1,
		Transactions: makeHashes(3),
		Priority:     []uint64{1, 2}, // mismatch
	}
	err := ef.ValidateInclusionList(list)
	if err == nil {
		t.Error("expected error for priority length mismatch")
	}
}

func TestValidateInclusionListV2DuplicateTx(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	h := types.Hash{0x01}
	list := &InclusionListV2{
		Slot:         1,
		Transactions: []types.Hash{h, h},
		Priority:     []uint64{2, 1},
	}
	err := ef.ValidateInclusionList(list)
	if err == nil {
		t.Error("expected error for duplicate transaction")
	}
}

func TestValidateInclusionListV2InvalidGasConstraint(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	list := &InclusionListV2{
		Slot:         1,
		Transactions: makeHashes(1),
		Priority:     []uint64{1},
		Constraints: []InclusionConstraint{
			{Type: ConstraintGasLimit, MinGas: 100, MaxGas: 50}, // invalid
		},
	}
	err := ef.ValidateInclusionList(list)
	if err == nil {
		t.Error("expected error for MinGas > MaxGas")
	}
}

// --- MergeInclusionLists tests ---

func TestMergeInclusionListsEmpty(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	_, err := ef.MergeInclusionLists(nil)
	if err != ErrEnhancedNoLists {
		t.Errorf("err = %v, want ErrEnhancedNoLists", err)
	}
}

func TestMergeInclusionListsSingle(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	list := &InclusionListV2{
		Slot:         10,
		Transactions: makeHashes(3),
		Priority:     []uint64{3, 2, 1},
		MaxGas:       100000,
	}

	merged, err := ef.MergeInclusionLists([]*InclusionListV2{list})
	if err != nil {
		t.Fatal(err)
	}
	if merged.Slot != 10 {
		t.Errorf("Slot = %d, want 10", merged.Slot)
	}
	if len(merged.Transactions) != 3 {
		t.Errorf("Transactions = %d, want 3", len(merged.Transactions))
	}
}

func TestMergeInclusionListsDeduplicates(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	h1 := types.Hash{0x01}
	h2 := types.Hash{0x02}
	h3 := types.Hash{0x03}

	list1 := &InclusionListV2{
		Slot:         10,
		Transactions: []types.Hash{h1, h2},
		Priority:     []uint64{5, 3},
		MaxGas:       100000,
	}
	list2 := &InclusionListV2{
		Slot:         10,
		Transactions: []types.Hash{h2, h3},
		Priority:     []uint64{10, 1},
		MaxGas:       100000,
	}

	merged, err := ef.MergeInclusionLists([]*InclusionListV2{list1, list2})
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Transactions) != 3 {
		t.Errorf("Transactions = %d, want 3 (deduplicated)", len(merged.Transactions))
	}

	// h2 should have priority 10 (max of 3 and 10).
	for i, h := range merged.Transactions {
		if h == h2 && merged.Priority[i] != 10 {
			t.Errorf("h2 priority = %d, want 10 (max)", merged.Priority[i])
		}
	}
}

func TestMergeInclusionListsCapped(t *testing.T) {
	ef := NewEnhancedFOCIL(EnhancedFOCILConfig{
		MaxTransactions: 3,
		MaxConstraints:  10,
	})

	list1 := &InclusionListV2{
		Slot:         1,
		Transactions: makeHashes(5),
		Priority:     []uint64{5, 4, 3, 2, 1},
		MaxGas:       100000,
	}

	merged, err := ef.MergeInclusionLists([]*InclusionListV2{list1})
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Transactions) != 3 {
		t.Errorf("Transactions = %d, want 3 (capped)", len(merged.Transactions))
	}
}

func TestMergeInclusionListsMergesConstraints(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	addr1 := types.Address{0x01}
	addr2 := types.Address{0x02}

	list1 := &InclusionListV2{
		Slot:         1,
		Transactions: makeHashes(1),
		Priority:     []uint64{1},
		MaxGas:       100000,
		Constraints: []InclusionConstraint{
			{Type: ConstraintMustInclude, Target: addr1},
		},
	}
	list2 := &InclusionListV2{
		Slot:         1,
		Transactions: makeHashes(1),
		Priority:     []uint64{1},
		MaxGas:       100000,
		Constraints: []InclusionConstraint{
			{Type: ConstraintMustInclude, Target: addr1}, // duplicate
			{Type: ConstraintMustExclude, Target: addr2},
		},
	}

	merged, err := ef.MergeInclusionLists([]*InclusionListV2{list1, list2})
	if err != nil {
		t.Fatal(err)
	}
	// Should have 2 unique constraints (not 3).
	if len(merged.Constraints) != 2 {
		t.Errorf("Constraints = %d, want 2 (deduplicated)", len(merged.Constraints))
	}
}

func TestMergeInclusionListsPriorityOrder(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	h1 := types.Hash{0x01}
	h2 := types.Hash{0x02}

	list := &InclusionListV2{
		Slot:         1,
		Transactions: []types.Hash{h1, h2},
		Priority:     []uint64{1, 100},
		MaxGas:       100000,
	}

	merged, err := ef.MergeInclusionLists([]*InclusionListV2{list})
	if err != nil {
		t.Fatal(err)
	}

	// h2 (priority 100) should come before h1 (priority 1).
	if merged.Transactions[0] != h2 {
		t.Error("highest priority tx should be first after merge")
	}
}

// --- ScoreInclusionList tests ---

func TestScoreInclusionListNil(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	score := ef.ScoreInclusionList(nil)
	if score != 0.0 {
		t.Errorf("score(nil) = %f, want 0.0", score)
	}
}

func TestScoreInclusionListEmpty(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	list := &InclusionListV2{Transactions: nil}
	score := ef.ScoreInclusionList(list)
	if score != 0.0 {
		t.Errorf("score(empty) = %f, want 0.0", score)
	}
}

func TestScoreInclusionListNonZero(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	list := &InclusionListV2{
		Slot:         1,
		Transactions: makeHashes(5),
		Priority:     []uint64{5, 4, 3, 2, 1},
		Constraints:  makeConstraints(2),
	}

	score := ef.ScoreInclusionList(list)
	if score <= 0.0 {
		t.Errorf("score = %f, should be > 0", score)
	}
	if score > 1.0 {
		t.Errorf("score = %f, should be <= 1.0", score)
	}
}

func TestScoreInclusionListMoreTxsHigherScore(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	small := &InclusionListV2{
		Transactions: makeHashes(2),
		Priority:     []uint64{2, 1},
	}
	large := &InclusionListV2{
		Transactions: makeHashes(10),
		Priority:     []uint64{10, 9, 8, 7, 6, 5, 4, 3, 2, 1},
	}

	scoreSmall := ef.ScoreInclusionList(small)
	scoreLarge := ef.ScoreInclusionList(large)

	if scoreLarge <= scoreSmall {
		t.Errorf("more txs should score higher: small=%f, large=%f", scoreSmall, scoreLarge)
	}
}

func TestScoreInclusionListConstraintsAddValue(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	noConstraints := &InclusionListV2{
		Transactions: makeHashes(5),
		Priority:     []uint64{5, 4, 3, 2, 1},
	}
	withConstraints := &InclusionListV2{
		Transactions: makeHashes(5),
		Priority:     []uint64{5, 4, 3, 2, 1},
		Constraints:  makeConstraints(10),
	}

	s1 := ef.ScoreInclusionList(noConstraints)
	s2 := ef.ScoreInclusionList(withConstraints)

	if s2 <= s1 {
		t.Errorf("constraints should increase score: without=%f, with=%f", s1, s2)
	}
}

// --- CheckEnforcement tests ---

func TestCheckEnforcementAllIncluded(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	hashes := makeHashes(3)
	list := &InclusionListV2{
		Slot:         1,
		Transactions: hashes,
		Priority:     []uint64{3, 2, 1},
	}

	result := ef.CheckEnforcement(list, hashes)
	if !result.Satisfied {
		t.Error("all txs included, should be satisfied")
	}
	if result.Violated != 0 {
		t.Errorf("Violated = %d, want 0", result.Violated)
	}
}

func TestCheckEnforcementMissing(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	hashes := makeHashes(3)
	list := &InclusionListV2{
		Slot:         1,
		Transactions: hashes,
		Priority:     []uint64{3, 2, 1},
	}

	// Only include 2 of 3.
	result := ef.CheckEnforcement(list, hashes[:2])
	if result.Satisfied {
		t.Error("missing tx, should not be satisfied")
	}
	if result.Violated == 0 {
		t.Error("should have violations")
	}
	if len(result.Penalties) == 0 {
		t.Error("should have penalty entries")
	}
}

func TestCheckEnforcementMustExcludeViolated(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	excludeAddr := types.Address{0xAA}
	excludeHash := addressToConstraintHash(excludeAddr)

	list := &InclusionListV2{
		Slot:         1,
		Transactions: []types.Hash{{0x01}},
		Priority:     []uint64{1},
		Constraints: []InclusionConstraint{
			{Type: ConstraintMustExclude, Target: excludeAddr},
		},
	}

	// Include both the listed tx and the excluded hash.
	included := []types.Hash{{0x01}, excludeHash}
	result := ef.CheckEnforcement(list, included)
	if result.Satisfied {
		t.Error("must-exclude violated, should not be satisfied")
	}
}

func TestCheckEnforcementNilList(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	result := ef.CheckEnforcement(nil, makeHashes(3))
	if !result.Satisfied {
		t.Error("nil list should be satisfied")
	}
}

func TestCheckEnforcementLenient(t *testing.T) {
	ef := NewEnhancedFOCIL(EnhancedFOCILConfig{
		MaxTransactions:     16,
		MaxConstraints:      32,
		EnforcementStrength: 0, // lenient
	})

	list := &InclusionListV2{
		Slot:         1,
		Transactions: makeHashes(3),
		Priority:     []uint64{3, 2, 1},
	}

	// Even with missing txs, lenient enforcement should pass.
	result := ef.CheckEnforcement(list, nil)
	if !result.Satisfied {
		t.Error("lenient enforcement should be satisfied even with missing txs")
	}
}

// --- ComputeInclusionListRoot tests ---

func TestComputeInclusionListRoot(t *testing.T) {
	list := &InclusionListV2{
		Slot:         42,
		Proposer:     types.Address{0x01},
		Transactions: makeHashes(3),
	}

	root := ComputeInclusionListRoot(list)
	if root.IsZero() {
		t.Error("root should not be zero")
	}

	// Should be deterministic.
	root2 := ComputeInclusionListRoot(list)
	if root != root2 {
		t.Error("root should be deterministic")
	}

	// Different slot should produce different root.
	list.Slot = 43
	root3 := ComputeInclusionListRoot(list)
	if root3 == root {
		t.Error("different slot should produce different root")
	}
}

func TestComputeInclusionListRootDifferentTxs(t *testing.T) {
	list1 := &InclusionListV2{
		Slot:         1,
		Proposer:     types.Address{0x01},
		Transactions: []types.Hash{{0x01}, {0x02}},
	}
	list2 := &InclusionListV2{
		Slot:         1,
		Proposer:     types.Address{0x01},
		Transactions: []types.Hash{{0x03}, {0x04}},
	}

	root1 := ComputeInclusionListRoot(list1)
	root2 := ComputeInclusionListRoot(list2)
	if root1 == root2 {
		t.Error("different txs should produce different roots")
	}
}

// --- Concurrency tests ---

func TestEnhancedFOCILConcurrentBuild(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			hashes := makeHashes(3)
			_, err := ef.BuildInclusionList(uint64(idx+1), hashes)
			if err != nil {
				t.Errorf("goroutine %d: BuildInclusionList: %v", idx, err)
			}
		}(i)
	}

	wg.Wait()
}

func TestEnhancedFOCILConcurrentValidate(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	var wg sync.WaitGroup

	list := &InclusionListV2{
		Slot:         1,
		Transactions: makeHashes(3),
		Priority:     []uint64{3, 2, 1},
	}

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ef.ValidateInclusionList(list)
		}()
	}

	wg.Wait()
}

func TestEnhancedFOCILConcurrentScore(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	var wg sync.WaitGroup

	list := &InclusionListV2{
		Slot:         1,
		Transactions: makeHashes(5),
		Priority:     []uint64{5, 4, 3, 2, 1},
		Constraints:  makeConstraints(2),
	}

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			score := ef.ScoreInclusionList(list)
			if score <= 0 {
				t.Errorf("score = %f, should be > 0", score)
			}
		}()
	}

	wg.Wait()
}

func TestEnhancedFOCILConcurrentBuildAndRead(t *testing.T) {
	ef := NewEnhancedFOCIL(DefaultEnhancedFOCILConfig())
	var wg sync.WaitGroup

	// Writers.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ef.BuildInclusionList(42, makeHashes(2))
		}(i)
	}

	// Readers.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = ef.ListsForSlot(42)
		}()
	}

	wg.Wait()
}

// --- validateConstraint tests ---

func TestValidateConstraintGasOK(t *testing.T) {
	c := InclusionConstraint{
		Type:   ConstraintGasLimit,
		MinGas: 100,
		MaxGas: 200,
	}
	if err := validateConstraint(c); err != nil {
		t.Errorf("valid gas constraint: %v", err)
	}
}

func TestValidateConstraintGasInvalid(t *testing.T) {
	c := InclusionConstraint{
		Type:   ConstraintGasLimit,
		MinGas: 200,
		MaxGas: 100,
	}
	if err := validateConstraint(c); err == nil {
		t.Error("expected error for MinGas > MaxGas")
	}
}

func TestValidateConstraintGasZeroMax(t *testing.T) {
	// MaxGas = 0 means unlimited, so MinGas > 0 is fine.
	c := InclusionConstraint{
		Type:   ConstraintGasLimit,
		MinGas: 100,
		MaxGas: 0,
	}
	if err := validateConstraint(c); err != nil {
		t.Errorf("gas constraint with zero max: %v", err)
	}
}

func TestValidateConstraintNonGas(t *testing.T) {
	c := InclusionConstraint{
		Type:   ConstraintMustInclude,
		Target: types.Address{0x01},
	}
	if err := validateConstraint(c); err != nil {
		t.Errorf("must-include constraint: %v", err)
	}
}

// --- addressToConstraintHash tests ---

func TestAddressToConstraintHash(t *testing.T) {
	addr := types.Address{0x01}
	h := addressToConstraintHash(addr)
	if h.IsZero() {
		t.Error("hash should not be zero")
	}

	// Deterministic.
	h2 := addressToConstraintHash(addr)
	if h != h2 {
		t.Error("hash should be deterministic")
	}

	// Different address produces different hash.
	h3 := addressToConstraintHash(types.Address{0x02})
	if h == h3 {
		t.Error("different addresses should produce different hashes")
	}
}
