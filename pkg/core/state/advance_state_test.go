package state

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestNewSlotAdvancer(t *testing.T) {
	sa := NewSlotAdvancer(AdvanceConfig{})
	if sa == nil {
		t.Fatal("NewSlotAdvancer returned nil")
	}
	cfg := sa.Config()
	if cfg.LookaheadSlots != DefaultAdvanceConfig().LookaheadSlots {
		t.Errorf("LookaheadSlots: want %d, got %d",
			DefaultAdvanceConfig().LookaheadSlots, cfg.LookaheadSlots)
	}
	if cfg.MaxSpecBranches != DefaultAdvanceConfig().MaxSpecBranches {
		t.Errorf("MaxSpecBranches: want %d, got %d",
			DefaultAdvanceConfig().MaxSpecBranches, cfg.MaxSpecBranches)
	}
	if cfg.PruneInterval != DefaultAdvanceConfig().PruneInterval {
		t.Errorf("PruneInterval: want %d, got %d",
			DefaultAdvanceConfig().PruneInterval, cfg.PruneInterval)
	}
}

func TestNewSlotAdvancerCustomConfig(t *testing.T) {
	cfg := AdvanceConfig{
		LookaheadSlots:  4,
		MaxSpecBranches: 10,
		PruneInterval:   8,
	}
	sa := NewSlotAdvancer(cfg)
	got := sa.Config()
	if got.LookaheadSlots != 4 {
		t.Errorf("LookaheadSlots: want 4, got %d", got.LookaheadSlots)
	}
	if got.MaxSpecBranches != 10 {
		t.Errorf("MaxSpecBranches: want 10, got %d", got.MaxSpecBranches)
	}
	if got.PruneInterval != 8 {
		t.Errorf("PruneInterval: want 8, got %d", got.PruneInterval)
	}
}

func TestAdvanceBasic(t *testing.T) {
	sa := NewSlotAdvancer(DefaultAdvanceConfig())
	sa.SetHeadSlot(100)

	parent := types.HexToHash("0xaaaa")
	branch, err := sa.Advance(parent, 105)
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if branch == nil {
		t.Fatal("expected non-nil branch")
	}
	if branch.ParentHash != parent {
		t.Error("parent hash mismatch")
	}
	if branch.Slot != 105 {
		t.Errorf("slot: want 105, got %d", branch.Slot)
	}
	if branch.Status != BranchPending {
		t.Errorf("status: want pending, got %s", branch.Status)
	}
	if branch.PredictedRoot.IsZero() {
		t.Error("predicted root should not be zero")
	}
	if branch.ID == "" {
		t.Error("branch ID should not be empty")
	}
}

func TestAdvanceTooFar(t *testing.T) {
	sa := NewSlotAdvancer(AdvanceConfig{LookaheadSlots: 4})
	sa.SetHeadSlot(100)

	_, err := sa.Advance(types.HexToHash("0xaa"), 105)
	if err != ErrAdvanceTooFar {
		t.Errorf("expected ErrAdvanceTooFar, got %v", err)
	}
}

func TestAdvanceAtLookaheadBoundary(t *testing.T) {
	sa := NewSlotAdvancer(AdvanceConfig{LookaheadSlots: 4})
	sa.SetHeadSlot(100)

	// Slot 104 = headSlot + 4, should succeed.
	_, err := sa.Advance(types.HexToHash("0xaa"), 104)
	if err != nil {
		t.Fatalf("Advance at boundary should succeed: %v", err)
	}
}

func TestAdvanceMaxBranches(t *testing.T) {
	sa := NewSlotAdvancer(AdvanceConfig{MaxSpecBranches: 2, LookaheadSlots: 100})
	sa.SetHeadSlot(0)

	_, err := sa.Advance(types.HexToHash("0x01"), 1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = sa.Advance(types.HexToHash("0x02"), 2)
	if err != nil {
		t.Fatal(err)
	}

	// Third should fail.
	_, err = sa.Advance(types.HexToHash("0x03"), 3)
	if err != ErrMaxBranches {
		t.Errorf("expected ErrMaxBranches, got %v", err)
	}
}

func TestConfirmBranch(t *testing.T) {
	sa := NewSlotAdvancer(DefaultAdvanceConfig())
	sa.SetHeadSlot(100)

	branch, _ := sa.Advance(types.HexToHash("0xaa"), 102)
	err := sa.Confirm(branch.ID)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	got := sa.GetBranch(branch.ID)
	if got.Status != BranchConfirmed {
		t.Errorf("status: want confirmed, got %s", got.Status)
	}
}

func TestConfirmBranchNotFound(t *testing.T) {
	sa := NewSlotAdvancer(DefaultAdvanceConfig())

	err := sa.Confirm("nonexistent")
	if err != ErrBranchNotFound {
		t.Errorf("expected ErrBranchNotFound, got %v", err)
	}
}

func TestConfirmBranchAlreadyConfirmed(t *testing.T) {
	sa := NewSlotAdvancer(DefaultAdvanceConfig())
	sa.SetHeadSlot(100)

	branch, _ := sa.Advance(types.HexToHash("0xaa"), 102)
	sa.Confirm(branch.ID)

	err := sa.Confirm(branch.ID)
	if err != ErrBranchAlreadyConfirmed {
		t.Errorf("expected ErrBranchAlreadyConfirmed, got %v", err)
	}
}

func TestGetBranch(t *testing.T) {
	sa := NewSlotAdvancer(DefaultAdvanceConfig())
	sa.SetHeadSlot(100)

	branch, _ := sa.Advance(types.HexToHash("0xaa"), 102)
	got := sa.GetBranch(branch.ID)
	if got == nil {
		t.Fatal("expected to find branch")
	}
	if got.ID != branch.ID {
		t.Error("branch ID mismatch")
	}
}

func TestGetBranchNotFound(t *testing.T) {
	sa := NewSlotAdvancer(DefaultAdvanceConfig())
	if sa.GetBranch("nonexistent") != nil {
		t.Error("expected nil for unknown branch ID")
	}
}

func TestActiveBranches(t *testing.T) {
	sa := NewSlotAdvancer(DefaultAdvanceConfig())
	sa.SetHeadSlot(100)

	if sa.ActiveBranches() != 0 {
		t.Errorf("initial active: want 0, got %d", sa.ActiveBranches())
	}

	sa.Advance(types.HexToHash("0x01"), 101)
	sa.Advance(types.HexToHash("0x02"), 102)

	if sa.ActiveBranches() != 2 {
		t.Errorf("active: want 2, got %d", sa.ActiveBranches())
	}

	// Confirming a branch should reduce active count.
	branch, _ := sa.Advance(types.HexToHash("0x03"), 103)
	sa.Confirm(branch.ID)

	if sa.ActiveBranches() != 2 {
		t.Errorf("active after confirm: want 2, got %d", sa.ActiveBranches())
	}
}

func TestConfirmedBranches(t *testing.T) {
	sa := NewSlotAdvancer(DefaultAdvanceConfig())
	sa.SetHeadSlot(100)

	if sa.ConfirmedBranches() != 0 {
		t.Errorf("initial confirmed: want 0, got %d", sa.ConfirmedBranches())
	}

	b1, _ := sa.Advance(types.HexToHash("0x01"), 101)
	b2, _ := sa.Advance(types.HexToHash("0x02"), 102)

	sa.Confirm(b1.ID)
	if sa.ConfirmedBranches() != 1 {
		t.Errorf("confirmed: want 1, got %d", sa.ConfirmedBranches())
	}

	sa.Confirm(b2.ID)
	if sa.ConfirmedBranches() != 2 {
		t.Errorf("confirmed: want 2, got %d", sa.ConfirmedBranches())
	}
}

func TestPrune(t *testing.T) {
	sa := NewSlotAdvancer(AdvanceConfig{
		LookaheadSlots:  100,
		MaxSpecBranches: 100,
		PruneInterval:   10,
	})
	sa.SetHeadSlot(5)

	// Create branches at low slots.
	sa.Advance(types.HexToHash("0x01"), 1)
	sa.Advance(types.HexToHash("0x02"), 2)
	sa.Advance(types.HexToHash("0x03"), 50) // future slot, should survive

	// Move head forward so slots 1 and 2 are old enough to prune.
	sa.SetHeadSlot(20)
	pruned := sa.Prune()

	if pruned != 2 {
		t.Errorf("pruned: want 2, got %d", pruned)
	}
	if sa.ActiveBranches() != 1 {
		t.Errorf("active after prune: want 1, got %d", sa.ActiveBranches())
	}
}

func TestPruneKeepsConfirmed(t *testing.T) {
	sa := NewSlotAdvancer(AdvanceConfig{
		LookaheadSlots:  100,
		MaxSpecBranches: 100,
		PruneInterval:   5,
	})
	sa.SetHeadSlot(0)

	branch, _ := sa.Advance(types.HexToHash("0x01"), 1)
	sa.Confirm(branch.ID)

	// Move head far forward.
	sa.SetHeadSlot(100)
	pruned := sa.Prune()

	// Confirmed branches should not be pruned.
	if pruned != 0 {
		t.Errorf("pruned: want 0 (confirmed kept), got %d", pruned)
	}
	if sa.ConfirmedBranches() != 1 {
		t.Errorf("confirmed: want 1, got %d", sa.ConfirmedBranches())
	}
}

func TestPruneNothingToPrune(t *testing.T) {
	sa := NewSlotAdvancer(DefaultAdvanceConfig())
	sa.SetHeadSlot(0)

	pruned := sa.Prune()
	if pruned != 0 {
		t.Errorf("pruned: want 0, got %d", pruned)
	}
}

func TestSetHeadSlot(t *testing.T) {
	sa := NewSlotAdvancer(DefaultAdvanceConfig())

	sa.SetHeadSlot(42)
	if sa.HeadSlot() != 42 {
		t.Errorf("head slot: want 42, got %d", sa.HeadSlot())
	}

	sa.SetHeadSlot(100)
	if sa.HeadSlot() != 100 {
		t.Errorf("head slot: want 100, got %d", sa.HeadSlot())
	}
}

func TestBranchStatusString(t *testing.T) {
	tests := []struct {
		status BranchStatus
		want   string
	}{
		{BranchPending, "pending"},
		{BranchConfirmed, "confirmed"},
		{BranchPruned, "pruned"},
		{BranchStatus(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.status.String(); got != tt.want {
			t.Errorf("BranchStatus(%d).String(): want %q, got %q", tt.status, tt.want, got)
		}
	}
}

func TestDeterministicBranchID(t *testing.T) {
	parent := types.HexToHash("0xabcd")
	slot := uint64(42)

	b1ID := deriveBranchID(parent, slot)
	b2ID := deriveBranchID(parent, slot)

	if b1ID != b2ID {
		t.Errorf("branch IDs should be deterministic: %s != %s", b1ID, b2ID)
	}

	// Different inputs should produce different IDs.
	b3ID := deriveBranchID(parent, slot+1)
	if b1ID == b3ID {
		t.Error("different slots should produce different branch IDs")
	}
}

func TestDeterministicPredictedRoot(t *testing.T) {
	parent := types.HexToHash("0xabcd")
	slot := uint64(42)

	r1 := derivePredictedRoot(parent, slot)
	r2 := derivePredictedRoot(parent, slot)
	if r1 != r2 {
		t.Error("predicted roots should be deterministic")
	}

	r3 := derivePredictedRoot(parent, slot+1)
	if r1 == r3 {
		t.Error("different slots should produce different predicted roots")
	}
}

func TestMaxBranchesDoesNotCountConfirmed(t *testing.T) {
	sa := NewSlotAdvancer(AdvanceConfig{MaxSpecBranches: 2, LookaheadSlots: 100})
	sa.SetHeadSlot(0)

	b1, _ := sa.Advance(types.HexToHash("0x01"), 1)
	sa.Advance(types.HexToHash("0x02"), 2)

	// Confirm b1 to free up a slot.
	sa.Confirm(b1.ID)

	// Should succeed since only 1 pending branch remains.
	_, err := sa.Advance(types.HexToHash("0x03"), 3)
	if err != nil {
		t.Fatalf("expected success after confirming a branch: %v", err)
	}
}

func TestConcurrentAdvance(t *testing.T) {
	sa := NewSlotAdvancer(AdvanceConfig{
		LookaheadSlots:  1000,
		MaxSpecBranches: 1000,
		PruneInterval:   100,
	})
	sa.SetHeadSlot(0)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			parent := types.HexToHash("0x" + string(rune('A'+i%26)))
			sa.Advance(parent, uint64(i+1))
		}(i)
	}
	wg.Wait()

	if sa.TotalBranches() == 0 {
		t.Error("expected branches after concurrent advance")
	}
}

func TestConcurrentConfirmAndPrune(t *testing.T) {
	sa := NewSlotAdvancer(AdvanceConfig{
		LookaheadSlots:  1000,
		MaxSpecBranches: 1000,
		PruneInterval:   5,
	})
	sa.SetHeadSlot(0)

	// Create a bunch of branches.
	var branches []*SpeculativeBranch
	for i := 0; i < 20; i++ {
		b, err := sa.Advance(types.HexToHash("0x"+string(rune('a'+i%26))), uint64(i+1))
		if err != nil {
			t.Fatalf("Advance %d: %v", i, err)
		}
		branches = append(branches, b)
	}

	var wg sync.WaitGroup

	// Concurrently confirm some.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sa.Confirm(branches[i].ID)
		}(i)
	}

	// Concurrently prune.
	wg.Add(1)
	go func() {
		defer wg.Done()
		sa.SetHeadSlot(100)
		sa.Prune()
	}()

	wg.Wait()

	// No panics = success. Verify state is consistent.
	total := sa.TotalBranches()
	active := sa.ActiveBranches()
	confirmed := sa.ConfirmedBranches()
	if active+confirmed > total {
		t.Errorf("inconsistent: active(%d) + confirmed(%d) > total(%d)", active, confirmed, total)
	}
}

func TestTotalBranches(t *testing.T) {
	sa := NewSlotAdvancer(DefaultAdvanceConfig())
	sa.SetHeadSlot(100)

	if sa.TotalBranches() != 0 {
		t.Errorf("initial total: want 0, got %d", sa.TotalBranches())
	}

	b1, _ := sa.Advance(types.HexToHash("0x01"), 101)
	sa.Advance(types.HexToHash("0x02"), 102)
	sa.Confirm(b1.ID)

	// Total should include both pending and confirmed.
	if sa.TotalBranches() != 2 {
		t.Errorf("total: want 2, got %d", sa.TotalBranches())
	}
}
