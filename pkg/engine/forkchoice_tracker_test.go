package engine

import (
	"fmt"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// --- HeadChain tests ---

func TestFCTHeadChainUpdate(t *testing.T) {
	hc := NewHeadChain()
	head := types.HexToHash("0xaa")
	safe := types.HexToHash("0xbb")
	fin := types.HexToHash("0xcc")
	hc.Update(head, safe, fin, 100, 90, 80)

	gotHead, gotNum := hc.Head()
	if gotHead != head || gotNum != 100 {
		t.Fatalf("head mismatch: got %s #%d", gotHead.Hex(), gotNum)
	}

	gotSafe, gotSafeNum := hc.Safe()
	if gotSafe != safe || gotSafeNum != 90 {
		t.Fatalf("safe mismatch: got %s #%d", gotSafe.Hex(), gotSafeNum)
	}

	gotFin, gotFinNum := hc.Finalized()
	if gotFin != fin || gotFinNum != 80 {
		t.Fatalf("finalized mismatch: got %s #%d", gotFin.Hex(), gotFinNum)
	}
}

func TestFCTHeadChainEmpty(t *testing.T) {
	hc := NewHeadChain()
	head, num := hc.Head()
	if head != (types.Hash{}) || num != 0 {
		t.Fatal("expected empty head chain")
	}
}

// --- FCUHistory tests ---

func TestFCTHistoryAddAndLatest(t *testing.T) {
	h := NewFCUHistory(10)
	if h.Len() != 0 {
		t.Fatal("expected empty history")
	}

	_, err := h.Latest()
	if err == nil {
		t.Fatal("expected error on empty history")
	}

	rec := FCURecord{
		State:  ForkchoiceStateV1{HeadBlockHash: types.HexToHash("0xaa")},
		Result: StatusValid,
	}
	h.Add(rec)

	if h.Len() != 1 {
		t.Fatalf("expected 1 record, got %d", h.Len())
	}

	latest, err := h.Latest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if latest.State.HeadBlockHash != types.HexToHash("0xaa") {
		t.Fatal("latest record mismatch")
	}
}

func TestFCTHistoryEviction(t *testing.T) {
	h := NewFCUHistory(3)
	for i := 0; i < 5; i++ {
		h.Add(FCURecord{
			State:  ForkchoiceStateV1{HeadBlockHash: types.HexToHash("0xaa")},
			Result: StatusValid,
		})
	}
	if h.Len() != 3 {
		t.Fatalf("expected 3 records after eviction, got %d", h.Len())
	}
}

func TestFCTHistoryAll(t *testing.T) {
	h := NewFCUHistory(10)
	h.Add(FCURecord{Result: "a"})
	h.Add(FCURecord{Result: "b"})
	all := h.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 records, got %d", len(all))
	}
}

// --- ConflictDetector tests ---

func TestFCTConflictDetectorNoConflict(t *testing.T) {
	cd := NewConflictDetector()
	state := ForkchoiceStateV1{
		HeadBlockHash:      types.HexToHash("0xaa"),
		FinalizedBlockHash: types.HexToHash("0xbb"),
	}
	conflict, _ := cd.Check(state)
	if conflict {
		t.Fatal("unexpected conflict on first update")
	}

	// Same finalized, different head -- no conflict.
	state2 := ForkchoiceStateV1{
		HeadBlockHash:      types.HexToHash("0xcc"),
		FinalizedBlockHash: types.HexToHash("0xbb"),
	}
	conflict, _ = cd.Check(state2)
	if conflict {
		t.Fatal("unexpected conflict with same finalized hash")
	}
}

func TestFCTConflictDetectorFinalizedRegression(t *testing.T) {
	cd := NewConflictDetector()
	state1 := ForkchoiceStateV1{
		HeadBlockHash:      types.HexToHash("0xaa"),
		FinalizedBlockHash: types.HexToHash("0xbb"),
	}
	cd.Check(state1)

	state2 := ForkchoiceStateV1{
		HeadBlockHash:      types.HexToHash("0xaa"),
		FinalizedBlockHash: types.HexToHash("0xcc"), // finalized changed
	}
	conflict, reason := cd.Check(state2)
	if !conflict {
		t.Fatal("expected conflict for finalized regression")
	}
	if reason == "" {
		t.Fatal("expected non-empty reason")
	}
	if cd.ConflictCount() != 1 {
		t.Fatalf("expected 1 conflict, got %d", cd.ConflictCount())
	}
}

// --- PayloadIDAllocator tests ---

func TestFCTPayloadIDAllocate(t *testing.T) {
	a := NewPayloadIDAllocator()
	id, err := a.Allocate(types.HexToHash("0xaa"), 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == (PayloadID{}) {
		t.Fatal("expected non-zero payload ID")
	}
	if !a.Has(id) {
		t.Fatal("expected allocated ID to be present")
	}
	if a.Count() != 1 {
		t.Fatalf("expected 1 allocation, got %d", a.Count())
	}
}

func TestFCTPayloadIDAllocateMultiple(t *testing.T) {
	a := NewPayloadIDAllocator()
	ids := make(map[PayloadID]bool)
	for i := 0; i < 20; i++ {
		id, err := a.Allocate(types.HexToHash("0xaa"), uint64(1000+i))
		if err != nil {
			t.Fatalf("unexpected error on allocation %d: %v", i, err)
		}
		ids[id] = true
	}
	if len(ids) != 20 {
		t.Fatalf("expected 20 unique IDs, got %d", len(ids))
	}
}

func TestFCTPayloadIDPrune(t *testing.T) {
	a := NewPayloadIDAllocator()
	a.Allocate(types.HexToHash("0xaa"), 100)
	a.Allocate(types.HexToHash("0xbb"), 200)
	a.Allocate(types.HexToHash("0xcc"), 300)

	pruned := a.Prune(250)
	if pruned != 2 {
		t.Fatalf("expected 2 pruned, got %d", pruned)
	}
	if a.Count() != 1 {
		t.Fatalf("expected 1 remaining, got %d", a.Count())
	}
}

// --- ReorgTracker tests ---

func TestFCTReorgTrackerNoReorg(t *testing.T) {
	rt := NewReorgTracker(10)
	genesis := &BlockInfo{Hash: types.HexToHash("0x01"), Number: 0}
	block1 := &BlockInfo{Hash: types.HexToHash("0x02"), ParentHash: types.HexToHash("0x01"), Number: 1}
	rt.AddBlock(genesis)
	rt.AddBlock(block1)

	// First head set, no reorg.
	reorg := rt.ProcessHead(genesis.Hash, 0)
	if reorg != nil {
		t.Fatal("unexpected reorg on initial set")
	}

	// Direct extension, no reorg.
	reorg = rt.ProcessHead(block1.Hash, 1)
	if reorg != nil {
		t.Fatal("unexpected reorg on chain extension")
	}
}

func TestFCTReorgTrackerDetectsReorg(t *testing.T) {
	rt := NewReorgTracker(10)
	genesis := &BlockInfo{Hash: types.HexToHash("0x01"), Number: 0}
	blockA := &BlockInfo{Hash: types.HexToHash("0x0a"), ParentHash: types.HexToHash("0x01"), Number: 1}
	blockB := &BlockInfo{Hash: types.HexToHash("0x0b"), ParentHash: types.HexToHash("0x01"), Number: 1}
	rt.AddBlock(genesis)
	rt.AddBlock(blockA)
	rt.AddBlock(blockB)

	rt.ProcessHead(blockA.Hash, 1)
	reorg := rt.ProcessHead(blockB.Hash, 1)
	if reorg == nil {
		t.Fatal("expected reorg when switching to sibling block")
	}
	if reorg.OldHead != blockA.Hash || reorg.NewHead != blockB.Hash {
		t.Fatal("reorg heads mismatch")
	}
	if rt.ReorgCount() != 1 {
		t.Fatalf("expected 1 reorg, got %d", rt.ReorgCount())
	}
}

func TestFCTReorgTrackerDeepReorg(t *testing.T) {
	rt := NewReorgTracker(10)
	genesis := &BlockInfo{Hash: types.HexToHash("0x01"), Number: 0}
	rt.AddBlock(genesis)

	// Build chain A: 0x01 -> 0x0a -> 0x0b
	blockA1 := &BlockInfo{Hash: types.HexToHash("0x0a"), ParentHash: genesis.Hash, Number: 1}
	blockA2 := &BlockInfo{Hash: types.HexToHash("0x0b"), ParentHash: blockA1.Hash, Number: 2}
	rt.AddBlock(blockA1)
	rt.AddBlock(blockA2)

	// Build chain B: 0x01 -> 0x0c -> 0x0d
	blockB1 := &BlockInfo{Hash: types.HexToHash("0x0c"), ParentHash: genesis.Hash, Number: 1}
	blockB2 := &BlockInfo{Hash: types.HexToHash("0x0d"), ParentHash: blockB1.Hash, Number: 2}
	rt.AddBlock(blockB1)
	rt.AddBlock(blockB2)

	rt.ProcessHead(blockA2.Hash, 2)
	reorg := rt.ProcessHead(blockB2.Hash, 2)
	if reorg == nil {
		t.Fatal("expected deep reorg")
	}
	if reorg.Depth != 2 {
		t.Fatalf("expected depth 2, got %d", reorg.Depth)
	}
}

func TestFCTReorgTrackerHistory(t *testing.T) {
	rt := NewReorgTracker(2) // max 2
	genesis := &BlockInfo{Hash: types.HexToHash("0x01"), Number: 0}
	rt.AddBlock(genesis)

	// Create 3 forks to cause 3 reorgs.
	for i := byte(0); i < 3; i++ {
		forkA := &BlockInfo{Hash: types.HexToHash(fmt.Sprintf("0x1%d", i)), ParentHash: genesis.Hash, Number: 1}
		forkB := &BlockInfo{Hash: types.HexToHash(fmt.Sprintf("0x2%d", i)), ParentHash: genesis.Hash, Number: 1}
		rt.AddBlock(forkA)
		rt.AddBlock(forkB)
		rt.ProcessHead(forkA.Hash, 1)
		rt.ProcessHead(forkB.Hash, 1)
	}

	reorgs := rt.Reorgs()
	if len(reorgs) != 2 {
		t.Fatalf("expected 2 reorgs (capped), got %d", len(reorgs))
	}
}

// --- ForkchoiceTracker integration tests ---

func TestFCTTrackerProcessUpdate(t *testing.T) {
	ft := NewForkchoiceTracker(100, 100)

	genesis := &BlockInfo{Hash: types.HexToHash("0x01"), Number: 0}
	block1 := &BlockInfo{Hash: types.HexToHash("0x02"), ParentHash: genesis.Hash, Number: 1}
	ft.Reorgs.AddBlock(genesis)
	ft.Reorgs.AddBlock(block1)

	state := ForkchoiceStateV1{
		HeadBlockHash:      genesis.Hash,
		SafeBlockHash:      genesis.Hash,
		FinalizedBlockHash: genesis.Hash,
	}
	conflict, _, reorg := ft.ProcessUpdate(state, false, 0, 0, 0)
	if conflict {
		t.Fatal("unexpected conflict on first update")
	}
	if reorg != nil {
		t.Fatal("unexpected reorg on first update")
	}

	// Extend chain, no conflict.
	state2 := ForkchoiceStateV1{
		HeadBlockHash:      block1.Hash,
		SafeBlockHash:      genesis.Hash,
		FinalizedBlockHash: genesis.Hash,
	}
	conflict, _, reorg = ft.ProcessUpdate(state2, true, 1, 0, 0)
	if conflict {
		t.Fatal("unexpected conflict on extension")
	}

	// Check history.
	if ft.History.Len() != 2 {
		t.Fatalf("expected 2 history entries, got %d", ft.History.Len())
	}

	// Check chain state.
	head, num := ft.Chain.Head()
	if head != block1.Hash || num != 1 {
		t.Fatal("chain head mismatch")
	}
}

func TestFCTTrackerConflictDetection(t *testing.T) {
	ft := NewForkchoiceTracker(100, 100)

	state1 := ForkchoiceStateV1{
		HeadBlockHash:      types.HexToHash("0xaa"),
		FinalizedBlockHash: types.HexToHash("0xbb"),
	}
	ft.ProcessUpdate(state1, false, 100, 0, 0)

	state2 := ForkchoiceStateV1{
		HeadBlockHash:      types.HexToHash("0xaa"),
		FinalizedBlockHash: types.HexToHash("0xcc"), // different finalized
	}
	conflict, reason, _ := ft.ProcessUpdate(state2, false, 100, 0, 0)
	if !conflict {
		t.Fatal("expected conflict")
	}
	if reason == "" {
		t.Fatal("expected conflict reason")
	}
}
