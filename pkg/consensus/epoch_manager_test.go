package consensus

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func defaultEpochConfig() EpochManagerConfig {
	return EpochManagerConfig{
		SlotsPerEpoch:    4,
		CommitteeSize:    8,
		MaxHistoryEpochs: 10,
	}
}

func testValidators(n int) []string {
	vals := make([]string, n)
	for i := 0; i < n; i++ {
		vals[i] = fmt.Sprintf("val-%d", i)
	}
	return vals
}

func TestEpochManagerStartEpoch(t *testing.T) {
	em := NewEpochManager(defaultEpochConfig())
	vals := testValidators(4)

	if err := em.StartEpoch(0, vals); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cur := em.CurrentEpoch()
	if cur == nil {
		t.Fatal("expected current epoch, got nil")
	}
	if cur.Number != 0 {
		t.Fatalf("expected epoch 0, got %d", cur.Number)
	}
	if cur.StartSlot != 0 || cur.EndSlot != 3 {
		t.Fatalf("expected slots 0-3, got %d-%d", cur.StartSlot, cur.EndSlot)
	}
}

func TestEpochManagerStartEpochDuplicate(t *testing.T) {
	em := NewEpochManager(defaultEpochConfig())
	vals := testValidators(4)

	_ = em.StartEpoch(0, vals)
	err := em.StartEpoch(0, vals)

	if !errors.Is(err, ErrEpochAlreadyStarted) {
		t.Fatalf("expected ErrEpochAlreadyStarted, got %v", err)
	}
}

func TestEpochManagerStartEpochNoValidators(t *testing.T) {
	em := NewEpochManager(defaultEpochConfig())
	err := em.StartEpoch(0, nil)

	if !errors.Is(err, ErrEpochNoValidators) {
		t.Fatalf("expected ErrEpochNoValidators, got %v", err)
	}
}

func TestEpochManagerStartEpochEmptyValidators(t *testing.T) {
	em := NewEpochManager(defaultEpochConfig())
	err := em.StartEpoch(0, []string{})

	if !errors.Is(err, ErrEpochNoValidators) {
		t.Fatalf("expected ErrEpochNoValidators, got %v", err)
	}
}

func TestEpochManagerCurrentEpochNil(t *testing.T) {
	em := NewEpochManager(defaultEpochConfig())
	if em.CurrentEpoch() != nil {
		t.Fatal("expected nil current epoch before any starts")
	}
}

func TestEpochManagerEpochForSlot(t *testing.T) {
	em := NewEpochManager(defaultEpochConfig())

	tests := []struct {
		slot  uint64
		epoch uint64
	}{
		{0, 0},
		{1, 0},
		{3, 0},
		{4, 1},
		{7, 1},
		{8, 2},
		{100, 25},
	}

	for _, tc := range tests {
		got := em.EpochForSlot(tc.slot)
		if got != tc.epoch {
			t.Errorf("EpochForSlot(%d) = %d, want %d", tc.slot, got, tc.epoch)
		}
	}
}

func TestEpochManagerFinalizeEpoch(t *testing.T) {
	em := NewEpochManager(defaultEpochConfig())
	_ = em.StartEpoch(0, testValidators(4))

	var stateRoot types.Hash
	stateRoot[0] = 0xAB

	if err := em.FinalizeEpoch(0, stateRoot); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cur := em.CurrentEpoch()
	if !cur.Finalized {
		t.Fatal("epoch should be finalized")
	}
	if cur.StateRoot != stateRoot {
		t.Fatal("state root mismatch")
	}
}

func TestEpochManagerFinalizeEpochNotFound(t *testing.T) {
	em := NewEpochManager(defaultEpochConfig())
	err := em.FinalizeEpoch(99, types.Hash{})

	if !errors.Is(err, ErrEpochNotFound) {
		t.Fatalf("expected ErrEpochNotFound, got %v", err)
	}
}

func TestEpochManagerFinalizeEpochAlready(t *testing.T) {
	em := NewEpochManager(defaultEpochConfig())
	_ = em.StartEpoch(0, testValidators(4))
	_ = em.FinalizeEpoch(0, types.Hash{})

	err := em.FinalizeEpoch(0, types.Hash{})
	if !errors.Is(err, ErrEpochAlreadyFinalized) {
		t.Fatalf("expected ErrEpochAlreadyFinalized, got %v", err)
	}
}

func TestEpochManagerGetCommittee(t *testing.T) {
	em := NewEpochManager(defaultEpochConfig())
	vals := testValidators(4)
	_ = em.StartEpoch(0, vals)

	committee, err := em.GetCommittee(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(committee) != 4 {
		t.Fatalf("expected 4 committee members, got %d", len(committee))
	}
	for i, v := range committee {
		if v != vals[i] {
			t.Errorf("committee[%d] = %s, want %s", i, v, vals[i])
		}
	}
}

func TestEpochManagerGetCommitteeNotFound(t *testing.T) {
	em := NewEpochManager(defaultEpochConfig())
	_, err := em.GetCommittee(99)

	if !errors.Is(err, ErrEpochNotFound) {
		t.Fatalf("expected ErrEpochNotFound, got %v", err)
	}
}

func TestEpochManagerGetAssignment(t *testing.T) {
	em := NewEpochManager(defaultEpochConfig())
	vals := testValidators(4)
	_ = em.StartEpoch(0, vals)

	// Slot 0 -> position 0 -> val-0
	a, err := em.GetAssignment(0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.ValidatorID != "val-0" {
		t.Fatalf("expected val-0, got %s", a.ValidatorID)
	}
	if !a.IsProposer {
		t.Fatal("expected proposer flag to be true")
	}

	// Slot 2 -> position 2 -> val-2
	a, err = em.GetAssignment(0, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.ValidatorID != "val-2" {
		t.Fatalf("expected val-2, got %s", a.ValidatorID)
	}
}

func TestEpochManagerGetAssignmentNotFound(t *testing.T) {
	em := NewEpochManager(defaultEpochConfig())
	_, err := em.GetAssignment(99, 0)

	if !errors.Is(err, ErrEpochNotFound) {
		t.Fatalf("expected ErrEpochNotFound, got %v", err)
	}
}

func TestEpochManagerIsEpochBoundary(t *testing.T) {
	em := NewEpochManager(defaultEpochConfig())

	tests := []struct {
		slot     uint64
		boundary bool
	}{
		{0, false},
		{1, false},
		{2, false},
		{3, true}, // last slot of epoch 0
		{4, false},
		{7, true},  // last slot of epoch 1
		{11, true}, // last slot of epoch 2
	}

	for _, tc := range tests {
		got := em.IsEpochBoundary(tc.slot)
		if got != tc.boundary {
			t.Errorf("IsEpochBoundary(%d) = %v, want %v", tc.slot, got, tc.boundary)
		}
	}
}

func TestEpochManagerHistory(t *testing.T) {
	em := NewEpochManager(defaultEpochConfig())

	for i := uint64(0); i < 5; i++ {
		_ = em.StartEpoch(i, testValidators(4))
	}

	h := em.History(3)
	if len(h) != 3 {
		t.Fatalf("expected 3 epochs in history, got %d", len(h))
	}

	// Should be epochs 2, 3, 4 (last 3, oldest first).
	if h[0].Number != 2 {
		t.Fatalf("expected epoch 2, got %d", h[0].Number)
	}
	if h[2].Number != 4 {
		t.Fatalf("expected epoch 4, got %d", h[2].Number)
	}
}

func TestEpochManagerHistoryAll(t *testing.T) {
	em := NewEpochManager(defaultEpochConfig())
	_ = em.StartEpoch(0, testValidators(4))

	h := em.History(100)
	if len(h) != 1 {
		t.Fatalf("expected 1 epoch, got %d", len(h))
	}
}

func TestEpochManagerHistoryEmpty(t *testing.T) {
	em := NewEpochManager(defaultEpochConfig())

	h := em.History(5)
	if h != nil {
		t.Fatalf("expected nil history, got %v", h)
	}
}

func TestEpochManagerHistoryZero(t *testing.T) {
	em := NewEpochManager(defaultEpochConfig())
	_ = em.StartEpoch(0, testValidators(4))

	h := em.History(0)
	if h != nil {
		t.Fatalf("expected nil for n=0, got %v", h)
	}
}

func TestEpochManagerSlotInEpoch(t *testing.T) {
	em := NewEpochManager(defaultEpochConfig())

	tests := []struct {
		slot uint64
		pos  uint64
	}{
		{0, 0},
		{1, 1},
		{3, 3},
		{4, 0},
		{5, 1},
		{7, 3},
	}

	for _, tc := range tests {
		got := em.SlotInEpoch(tc.slot)
		if got != tc.pos {
			t.Errorf("SlotInEpoch(%d) = %d, want %d", tc.slot, got, tc.pos)
		}
	}
}

func TestEpochManagerPruneHistory(t *testing.T) {
	cfg := defaultEpochConfig()
	cfg.MaxHistoryEpochs = 3
	em := NewEpochManager(cfg)

	for i := uint64(0); i < 5; i++ {
		_ = em.StartEpoch(i, testValidators(4))
	}

	// Should only have epochs 2, 3, 4 (max 3 in history).
	h := em.History(10)
	if len(h) != 3 {
		t.Fatalf("expected 3 epochs after pruning, got %d", len(h))
	}
	if h[0].Number != 2 {
		t.Fatalf("expected oldest epoch 2, got %d", h[0].Number)
	}

	// Epoch 0 should no longer be accessible.
	_, err := em.GetCommittee(0)
	if !errors.Is(err, ErrEpochNotFound) {
		t.Fatalf("expected pruned epoch 0 not found, got %v", err)
	}
}

func TestEpochManagerConcurrentStartAndRead(t *testing.T) {
	em := NewEpochManager(EpochManagerConfig{
		SlotsPerEpoch:    4,
		CommitteeSize:    8,
		MaxHistoryEpochs: 100,
	})

	var wg sync.WaitGroup

	// Concurrently start epochs.
	for i := uint64(0); i < 20; i++ {
		wg.Add(1)
		go func(epoch uint64) {
			defer wg.Done()
			_ = em.StartEpoch(epoch, testValidators(4))
		}(i)
	}

	// Concurrently read.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = em.CurrentEpoch()
			em.History(5)
			em.EpochForSlot(10)
		}()
	}

	wg.Wait()
}

func TestEpochManagerGetAssignmentWrapsAround(t *testing.T) {
	em := NewEpochManager(EpochManagerConfig{
		SlotsPerEpoch:    8,
		CommitteeSize:    8,
		MaxHistoryEpochs: 10,
	})

	// Committee of 3 validators, epoch with 8 slots.
	vals := []string{"alice", "bob", "charlie"}
	_ = em.StartEpoch(0, vals)

	// Slot 3 -> position 3 % 3 = 0 -> alice
	a, _ := em.GetAssignment(0, 3)
	if a.ValidatorID != "alice" {
		t.Fatalf("expected alice, got %s", a.ValidatorID)
	}

	// Slot 4 -> position 4 % 3 = 1 -> bob
	a, _ = em.GetAssignment(0, 4)
	if a.ValidatorID != "bob" {
		t.Fatalf("expected bob, got %s", a.ValidatorID)
	}

	// Slot 5 -> position 5 % 3 = 2 -> charlie
	a, _ = em.GetAssignment(0, 5)
	if a.ValidatorID != "charlie" {
		t.Fatalf("expected charlie, got %s", a.ValidatorID)
	}
}

func TestEpochManagerCommitteeIsCopy(t *testing.T) {
	em := NewEpochManager(defaultEpochConfig())
	vals := testValidators(4)
	_ = em.StartEpoch(0, vals)

	// Mutate the returned committee and verify the original is unchanged.
	committee, _ := em.GetCommittee(0)
	committee[0] = "mutated"

	original, _ := em.GetCommittee(0)
	if original[0] == "mutated" {
		t.Fatal("GetCommittee returned a reference instead of a copy")
	}
}

func TestEpochManagerEpochForSlotZeroConfig(t *testing.T) {
	em := NewEpochManager(EpochManagerConfig{
		SlotsPerEpoch:    0,
		CommitteeSize:    8,
		MaxHistoryEpochs: 10,
	})

	got := em.EpochForSlot(100)
	if got != 0 {
		t.Fatalf("expected 0 for zero SlotsPerEpoch, got %d", got)
	}
}
