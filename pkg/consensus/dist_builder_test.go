package consensus

import (
	"math/big"
	"testing"
	"time"
)

func TestDefaultDistBuilderConfig(t *testing.T) {
	cfg := DefaultDistBuilderConfig()
	if cfg.MaxBuilders != 16 {
		t.Errorf("MaxBuilders = %d, want 16", cfg.MaxBuilders)
	}
	if cfg.BidTimeout != 4*time.Second {
		t.Errorf("BidTimeout = %v, want 4s", cfg.BidTimeout)
	}
	if cfg.Strategy != MergeByPriority {
		t.Errorf("Strategy = %d, want MergeByPriority", cfg.Strategy)
	}
	if cfg.GasLimit != 30_000_000 {
		t.Errorf("GasLimit = %d, want 30000000", cfg.GasLimit)
	}
	if cfg.MaxFragmentsPerSlot != 64 {
		t.Errorf("MaxFragmentsPerSlot = %d, want 64", cfg.MaxFragmentsPerSlot)
	}
}

func TestNewDistBlockBuilder_NilConfig(t *testing.T) {
	db := NewDistBlockBuilder(nil)
	if db.Config().MaxBuilders != 16 {
		t.Error("nil config should use defaults")
	}
}

func makeBid(id string, slot Slot, value int64) *ConsensusBuilderBid {
	return &ConsensusBuilderBid{
		BuilderID: id, Slot: slot, Value: big.NewInt(value),
		BlockRoot: fcHash(byte(value)), Timestamp: time.Now(),
	}
}

func makeFrag(id string, txCount int, gas uint64, prio int) *BlockFragment {
	txs := make([][]byte, txCount)
	for i := range txs {
		txs[i] = []byte{byte(i)}
	}
	return &BlockFragment{BuilderID: id, TxList: txs, GasUsed: gas, Priority: prio}
}

func TestSubmitBid_Errors(t *testing.T) {
	db := NewDistBlockBuilder(nil)

	if err := db.SubmitBid(nil); err != ErrDBNilBid {
		t.Errorf("nil bid: got %v, want ErrDBNilBid", err)
	}
	if err := db.SubmitBid(&ConsensusBuilderBid{BuilderID: "b", Slot: 0, Value: big.NewInt(1)}); err != ErrDBSlotZero {
		t.Errorf("slot zero: got %v, want ErrDBSlotZero", err)
	}
	if err := db.SubmitBid(&ConsensusBuilderBid{BuilderID: "b", Slot: 1, Value: big.NewInt(0)}); err != ErrDBZeroBidValue {
		t.Errorf("zero value: got %v, want ErrDBZeroBidValue", err)
	}
	if err := db.SubmitBid(&ConsensusBuilderBid{BuilderID: "b", Slot: 1, Value: nil}); err != ErrDBZeroBidValue {
		t.Errorf("nil value: got %v, want ErrDBZeroBidValue", err)
	}
	if err := db.SubmitBid(&ConsensusBuilderBid{BuilderID: "b", Slot: 1, Value: big.NewInt(-5)}); err != ErrDBZeroBidValue {
		t.Errorf("negative value: got %v, want ErrDBZeroBidValue", err)
	}
}

func TestSubmitBid_Basic(t *testing.T) {
	db := NewDistBlockBuilder(nil)
	if err := db.SubmitBid(makeBid("b1", 1, 100)); err != nil {
		t.Fatalf("SubmitBid: %v", err)
	}
	if db.BidCount(1) != 1 {
		t.Errorf("BidCount = %d, want 1", db.BidCount(1))
	}
}

func TestSubmitBid_MaxBuilders(t *testing.T) {
	cfg := DefaultDistBuilderConfig()
	cfg.MaxBuilders = 2
	db := NewDistBlockBuilder(cfg)

	db.SubmitBid(makeBid("A", 1, 100))
	db.SubmitBid(makeBid("B", 1, 100))

	// Third builder rejected.
	if err := db.SubmitBid(makeBid("C", 1, 200)); err != ErrDBMaxBuilders {
		t.Errorf("got %v, want ErrDBMaxBuilders", err)
	}
	// Existing builder can still bid.
	if err := db.SubmitBid(makeBid("A", 1, 300)); err != nil {
		t.Errorf("existing builder bid: %v", err)
	}
}

func TestSubmitFragment_Errors(t *testing.T) {
	db := NewDistBlockBuilder(nil)

	if err := db.SubmitFragment(1, nil); err != ErrDBNilFragment {
		t.Errorf("nil: got %v", err)
	}
	if err := db.SubmitFragment(0, makeFrag("b", 1, 21000, 1)); err != ErrDBSlotZero {
		t.Errorf("slot zero: got %v", err)
	}
	if err := db.SubmitFragment(1, &BlockFragment{BuilderID: "b", TxList: [][]byte{}, GasUsed: 21000}); err != ErrDBEmptyFragment {
		t.Errorf("empty tx: got %v", err)
	}
}

func TestSubmitFragment_Basic(t *testing.T) {
	db := NewDistBlockBuilder(nil)
	if err := db.SubmitFragment(1, makeFrag("b1", 2, 42000, 10)); err != nil {
		t.Fatalf("SubmitFragment: %v", err)
	}
	if db.FragmentCount(1) != 1 {
		t.Errorf("FragmentCount = %d, want 1", db.FragmentCount(1))
	}
}

func TestSubmitFragment_MaxFragments(t *testing.T) {
	cfg := DefaultDistBuilderConfig()
	cfg.MaxFragmentsPerSlot = 2
	db := NewDistBlockBuilder(cfg)

	db.SubmitFragment(1, makeFrag("b", 1, 21000, 1))
	db.SubmitFragment(1, makeFrag("b", 1, 21000, 2))

	if err := db.SubmitFragment(1, makeFrag("b", 1, 21000, 3)); err != ErrDBMaxBuilders {
		t.Errorf("got %v, want max fragments error", err)
	}
}

func TestMergeBids_PicksHighestValue(t *testing.T) {
	db := NewDistBlockBuilder(nil)
	db.SubmitBid(makeBid("a", 5, 100))
	db.SubmitBid(makeBid("b", 5, 500))
	db.SubmitBid(makeBid("c", 5, 300))

	winner, err := db.MergeBids(5)
	if err != nil {
		t.Fatalf("MergeBids: %v", err)
	}
	if winner.BuilderID != "b" {
		t.Errorf("winner = %s, want b", winner.BuilderID)
	}
	if winner.Value.Cmp(big.NewInt(500)) != 0 {
		t.Errorf("value = %s, want 500", winner.Value)
	}
}

func TestMergeBids_NoBids(t *testing.T) {
	db := NewDistBlockBuilder(nil)
	_, err := db.MergeBids(1)
	if err != ErrDBNoBids {
		t.Errorf("got %v, want ErrDBNoBids", err)
	}
}

func TestMergeBids_SingleBid(t *testing.T) {
	db := NewDistBlockBuilder(nil)
	db.SubmitBid(makeBid("solo", 3, 42))

	winner, err := db.MergeBids(3)
	if err != nil {
		t.Fatalf("MergeBids: %v", err)
	}
	if winner.BuilderID != "solo" {
		t.Errorf("winner = %s, want solo", winner.BuilderID)
	}
}

func TestMergeFragments_ByPriority(t *testing.T) {
	cfg := DefaultDistBuilderConfig()
	cfg.GasLimit = 100_000
	db := NewDistBlockBuilder(cfg)

	db.SubmitFragment(7, makeFrag("a", 1, 30_000, 1))
	db.SubmitFragment(7, makeFrag("b", 2, 40_000, 10))
	db.SubmitFragment(7, makeFrag("c", 1, 20_000, 5))

	merged, err := db.MergeFragments(7)
	if err != nil {
		t.Fatalf("MergeFragments: %v", err)
	}
	if len(merged.Fragments) != 3 {
		t.Fatalf("fragments = %d, want 3", len(merged.Fragments))
	}
	if merged.Fragments[0].BuilderID != "b" {
		t.Errorf("first = %s, want b (priority 10)", merged.Fragments[0].BuilderID)
	}
	if merged.TotalGas != 90_000 {
		t.Errorf("TotalGas = %d, want 90000", merged.TotalGas)
	}
	if merged.TotalTxs != 4 {
		t.Errorf("TotalTxs = %d, want 4", merged.TotalTxs)
	}
}

func TestMergeFragments_GasLimitEnforced(t *testing.T) {
	cfg := DefaultDistBuilderConfig()
	cfg.GasLimit = 50_000
	db := NewDistBlockBuilder(cfg)

	db.SubmitFragment(8, makeFrag("a", 1, 30_000, 10))
	db.SubmitFragment(8, makeFrag("b", 1, 30_000, 5))
	db.SubmitFragment(8, makeFrag("c", 1, 15_000, 1))

	merged, err := db.MergeFragments(8)
	if err != nil {
		t.Fatalf("MergeFragments: %v", err)
	}
	// a(30k) + c(15k) = 45k <= 50k; b(30k) skipped as 30k+30k > 50k.
	if merged.TotalGas > 50_000 {
		t.Errorf("TotalGas = %d, exceeds 50000", merged.TotalGas)
	}
	if len(merged.Fragments) != 2 {
		t.Fatalf("fragments = %d, want 2", len(merged.Fragments))
	}
	if merged.Fragments[0].BuilderID != "a" || merged.Fragments[1].BuilderID != "c" {
		t.Errorf("order: %s, %s; want a, c", merged.Fragments[0].BuilderID, merged.Fragments[1].BuilderID)
	}
}

func TestMergeFragments_ByGasPrice(t *testing.T) {
	cfg := DefaultDistBuilderConfig()
	cfg.Strategy = MergeByGasPrice
	cfg.GasLimit = 100_000
	db := NewDistBlockBuilder(cfg)

	db.SubmitFragment(9, makeFrag("low", 1, 50_000, 10)) // 1tx/50k = low efficiency
	db.SubmitFragment(9, makeFrag("high", 5, 30_000, 1)) // 5tx/30k = high efficiency

	merged, err := db.MergeFragments(9)
	if err != nil {
		t.Fatalf("MergeFragments: %v", err)
	}
	if len(merged.Fragments) != 2 {
		t.Fatalf("fragments = %d, want 2", len(merged.Fragments))
	}
	if merged.Fragments[0].BuilderID != "high" {
		t.Errorf("first = %s, want high", merged.Fragments[0].BuilderID)
	}
}

func TestMergeFragments_NoFragments(t *testing.T) {
	db := NewDistBlockBuilder(nil)
	_, err := db.MergeFragments(1)
	if err != ErrDBNoFragments {
		t.Errorf("got %v, want ErrDBNoFragments", err)
	}
}

func TestMergeFragments_AllExceedGas(t *testing.T) {
	cfg := DefaultDistBuilderConfig()
	cfg.GasLimit = 1000
	db := NewDistBlockBuilder(cfg)

	db.SubmitFragment(1, makeFrag("big", 1, 2000, 10))

	_, err := db.MergeFragments(1)
	if err != ErrDBNoFragments {
		t.Errorf("got %v, want ErrDBNoFragments", err)
	}
}

func TestGetWinningBid(t *testing.T) {
	db := NewDistBlockBuilder(nil)

	if db.GetWinningBid(1) != nil {
		t.Error("expected nil for no bids")
	}

	db.SubmitBid(makeBid("a", 1, 50))
	db.SubmitBid(makeBid("b", 1, 200))

	winner := db.GetWinningBid(1)
	if winner == nil || winner.BuilderID != "b" {
		t.Errorf("winner = %v, want b", winner)
	}
}

func TestBidAndFragmentCount_Untracked(t *testing.T) {
	db := NewDistBlockBuilder(nil)
	if db.BidCount(999) != 0 {
		t.Error("untracked bid count should be 0")
	}
	if db.FragmentCount(999) != 0 {
		t.Error("untracked fragment count should be 0")
	}
}

func TestPruneSlot(t *testing.T) {
	db := NewDistBlockBuilder(nil)
	db.SubmitBid(makeBid("a", 5, 100))
	db.SubmitFragment(5, makeFrag("a", 1, 21000, 1))

	db.PruneSlot(5)
	if db.BidCount(5) != 0 || db.FragmentCount(5) != 0 {
		t.Error("slot 5 should be pruned")
	}
}

func TestPruneBefore(t *testing.T) {
	db := NewDistBlockBuilder(nil)
	for i := Slot(1); i <= 5; i++ {
		db.SubmitBid(makeBid("a", i, 100))
	}

	pruned := db.PruneBefore(4)
	if pruned != 3 {
		t.Errorf("pruned = %d, want 3", pruned)
	}
	if db.BidCount(1) != 0 || db.BidCount(2) != 0 || db.BidCount(3) != 0 {
		t.Error("slots 1-3 should be pruned")
	}
	if db.BidCount(4) != 1 || db.BidCount(5) != 1 {
		t.Error("slots 4-5 should remain")
	}
}

func TestMergeFragments_BuilderIDs(t *testing.T) {
	cfg := DefaultDistBuilderConfig()
	cfg.GasLimit = 1_000_000
	db := NewDistBlockBuilder(cfg)

	db.SubmitFragment(10, makeFrag("alice", 1, 21000, 3))
	db.SubmitFragment(10, makeFrag("alice", 1, 21000, 2))
	db.SubmitFragment(10, makeFrag("bob", 1, 21000, 1))

	merged, err := db.MergeFragments(10)
	if err != nil {
		t.Fatalf("MergeFragments: %v", err)
	}
	if len(merged.BuilderIDs) != 2 {
		t.Errorf("BuilderIDs = %d, want 2", len(merged.BuilderIDs))
	}
	if merged.BuilderIDs[0] != "alice" {
		t.Errorf("first builder = %s, want alice", merged.BuilderIDs[0])
	}
}

func TestMergeFragments_GasPriceZeroGas(t *testing.T) {
	cfg := DefaultDistBuilderConfig()
	cfg.Strategy = MergeByGasPrice
	cfg.GasLimit = 1_000_000
	db := NewDistBlockBuilder(cfg)

	db.SubmitFragment(1, makeFrag("zero", 1, 0, 5))
	db.SubmitFragment(1, makeFrag("normal", 1, 21000, 1))

	merged, err := db.MergeFragments(1)
	if err != nil {
		t.Fatalf("MergeFragments: %v", err)
	}
	if len(merged.Fragments) != 2 {
		t.Fatalf("fragments = %d, want 2", len(merged.Fragments))
	}
	// Non-zero gas should come first in gas-price sort.
	if merged.Fragments[0].BuilderID != "normal" {
		t.Errorf("first = %s, want normal", merged.Fragments[0].BuilderID)
	}
}

func TestMergedBlock_Slot(t *testing.T) {
	db := NewDistBlockBuilder(nil)
	db.SubmitFragment(42, makeFrag("x", 1, 21000, 1))

	merged, err := db.MergeFragments(42)
	if err != nil {
		t.Fatalf("MergeFragments: %v", err)
	}
	if merged.Slot != 42 {
		t.Errorf("Slot = %d, want 42", merged.Slot)
	}
}

func TestMultipleSlotsIndependent(t *testing.T) {
	db := NewDistBlockBuilder(nil)
	db.SubmitBid(makeBid("a", 1, 100))
	db.SubmitBid(makeBid("b", 2, 200))

	w1 := db.GetWinningBid(1)
	w2 := db.GetWinningBid(2)
	if w1.Value.Cmp(big.NewInt(100)) != 0 {
		t.Errorf("slot 1 value = %s, want 100", w1.Value)
	}
	if w2.Value.Cmp(big.NewInt(200)) != 0 {
		t.Errorf("slot 2 value = %s, want 200", w2.Value)
	}
}

func TestValidateBuilderFragment(t *testing.T) {
	// Valid.
	frag := &BlockFragment{BuilderID: "b1", TxList: [][]byte{{0x01}}, GasUsed: 100}
	if err := ValidateBuilderFragment(frag, 30_000_000); err != nil {
		t.Errorf("valid fragment: %v", err)
	}

	// Nil.
	if err := ValidateBuilderFragment(nil, 30_000_000); err == nil {
		t.Error("expected error for nil fragment")
	}

	// Empty builder ID.
	badID := &BlockFragment{BuilderID: "", TxList: [][]byte{{0x01}}, GasUsed: 100}
	if err := ValidateBuilderFragment(badID, 30_000_000); err == nil {
		t.Error("expected error for empty builder ID")
	}

	// Gas exceeds limit.
	gasOver := &BlockFragment{BuilderID: "b1", TxList: [][]byte{{0x01}}, GasUsed: 50_000_000}
	if err := ValidateBuilderFragment(gasOver, 30_000_000); err == nil {
		t.Error("expected error for gas exceeding limit")
	}
}

func TestValidateDistBuilderConfig(t *testing.T) {
	cfg := DefaultDistBuilderConfig()
	if err := ValidateDistBuilderConfig(cfg); err != nil {
		t.Errorf("valid config: %v", err)
	}

	if err := ValidateDistBuilderConfig(nil); err == nil {
		t.Error("expected error for nil config")
	}
}
