// forkchoice_tracker.go provides forkchoice state tracking across Engine API
// V3/V4 updates. It maintains the head/safe/finalized chain, stores recent
// forkchoice updates for debugging, detects conflicting updates from the CL,
// allocates unique payload IDs, and identifies head reorgs with their depth.
//
// This complements forkchoice_state.go (low-level state management) and
// forkchoice_engine.go (orchestration) by providing higher-level analytics
// and debugging facilities for operators monitoring CL-EL interactions.
package engine

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// ForkchoiceTracker errors.
var (
	ErrFCTNilUpdate        = errors.New("fc_tracker: nil forkchoice update")
	ErrFCTZeroHead         = errors.New("fc_tracker: head block hash is zero")
	ErrFCTConflict         = errors.New("fc_tracker: conflicting forkchoice update detected")
	ErrFCTHistoryEmpty     = errors.New("fc_tracker: no forkchoice history")
	ErrFCTPayloadIDExists  = errors.New("fc_tracker: payload ID already allocated")
	ErrFCTPayloadNotFound  = errors.New("fc_tracker: payload ID not found")
	ErrFCTBlockNotFound    = errors.New("fc_tracker: block not found in chain")
)

// FCURecord stores a single forkchoice update for the debug history.
type FCURecord struct {
	// Timestamp is when the update was received.
	Timestamp time.Time

	// State is the forkchoice state from the CL.
	State ForkchoiceStateV1

	// HasAttributes indicates whether payload attributes were attached.
	HasAttributes bool

	// PayloadID is the assigned payload ID (zero if no build started).
	PayloadID PayloadID

	// Result is the status returned for this update.
	Result string
}

// HeadChain tracks the head, safe, and finalized blocks.
type HeadChain struct {
	mu        sync.RWMutex
	head      types.Hash
	safe      types.Hash
	finalized types.Hash
	headNum   uint64
	safeNum   uint64
	finalNum  uint64
}

// NewHeadChain creates an empty head chain tracker.
func NewHeadChain() *HeadChain {
	return &HeadChain{}
}

// Update sets new head/safe/finalized values.
func (hc *HeadChain) Update(head, safe, finalized types.Hash, headNum, safeNum, finalNum uint64) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.head = head
	hc.safe = safe
	hc.finalized = finalized
	hc.headNum = headNum
	hc.safeNum = safeNum
	hc.finalNum = finalNum
}

// Head returns the current head hash and number.
func (hc *HeadChain) Head() (types.Hash, uint64) {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	return hc.head, hc.headNum
}

// Safe returns the current safe hash and number.
func (hc *HeadChain) Safe() (types.Hash, uint64) {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	return hc.safe, hc.safeNum
}

// Finalized returns the current finalized hash and number.
func (hc *HeadChain) Finalized() (types.Hash, uint64) {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	return hc.finalized, hc.finalNum
}

// FCUHistory stores recent forkchoice updates for debugging and analytics.
type FCUHistory struct {
	mu         sync.RWMutex
	records    []FCURecord
	maxRecords int
}

// NewFCUHistory creates a history buffer with the given max size.
func NewFCUHistory(maxRecords int) *FCUHistory {
	if maxRecords <= 0 {
		maxRecords = 256
	}
	return &FCUHistory{
		records:    make([]FCURecord, 0, maxRecords),
		maxRecords: maxRecords,
	}
}

// Add appends a record to the history, evicting the oldest if at capacity.
func (h *FCUHistory) Add(record FCURecord) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.records) >= h.maxRecords {
		h.records = h.records[1:]
	}
	h.records = append(h.records, record)
}

// Len returns the number of records in the history.
func (h *FCUHistory) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.records)
}

// Latest returns the most recent record, or an error if empty.
func (h *FCUHistory) Latest() (FCURecord, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.records) == 0 {
		return FCURecord{}, ErrFCTHistoryEmpty
	}
	return h.records[len(h.records)-1], nil
}

// All returns a copy of all records.
func (h *FCUHistory) All() []FCURecord {
	h.mu.RLock()
	defer h.mu.RUnlock()
	result := make([]FCURecord, len(h.records))
	copy(result, h.records)
	return result
}

// ConflictDetector detects when the CL sends conflicting forkchoice updates
// (e.g., safe hash regresses to a non-ancestor, or finalized hash changes).
type ConflictDetector struct {
	mu            sync.RWMutex
	lastState     *ForkchoiceStateV1
	conflictCount uint64
}

// NewConflictDetector creates a new conflict detector.
func NewConflictDetector() *ConflictDetector {
	return &ConflictDetector{}
}

// Check compares a new update against the previous one and returns a conflict
// description if the finalized hash regressed (changed to a different non-zero value).
func (cd *ConflictDetector) Check(update ForkchoiceStateV1) (bool, string) {
	cd.mu.Lock()
	defer cd.mu.Unlock()

	if cd.lastState == nil {
		cd.lastState = &update
		return false, ""
	}

	prev := cd.lastState

	// Finalized hash regression: it changed to a different non-zero hash.
	if prev.FinalizedBlockHash != (types.Hash{}) &&
		update.FinalizedBlockHash != (types.Hash{}) &&
		update.FinalizedBlockHash != prev.FinalizedBlockHash {
		cd.conflictCount++
		cd.lastState = &update
		return true, fmt.Sprintf("finalized changed: %s -> %s",
			prev.FinalizedBlockHash.Hex(), update.FinalizedBlockHash.Hex())
	}

	cd.lastState = &update
	return false, ""
}

// ConflictCount returns the total number of detected conflicts.
func (cd *ConflictDetector) ConflictCount() uint64 {
	cd.mu.RLock()
	defer cd.mu.RUnlock()
	return cd.conflictCount
}

// PayloadIDAllocator assigns unique payload IDs for payload building.
type PayloadIDAllocator struct {
	mu        sync.Mutex
	allocated map[PayloadID]uint64 // payloadID -> timestamp
	counter   uint64
}

// NewPayloadIDAllocator creates a new allocator.
func NewPayloadIDAllocator() *PayloadIDAllocator {
	return &PayloadIDAllocator{
		allocated: make(map[PayloadID]uint64),
	}
}

// Allocate generates a unique payload ID from the head hash and timestamp.
// Returns the ID and an error if collision is detected.
func (a *PayloadIDAllocator) Allocate(headHash types.Hash, timestamp uint64) (PayloadID, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.counter++
	var id PayloadID
	copy(id[:4], headHash[:4])
	binary.BigEndian.PutUint32(id[4:], uint32(a.counter))

	// Add randomness to prevent deterministic collisions.
	var rb [2]byte
	rand.Read(rb[:])
	id[2] ^= rb[0]
	id[3] ^= rb[1]

	if _, exists := a.allocated[id]; exists {
		return PayloadID{}, ErrFCTPayloadIDExists
	}

	a.allocated[id] = timestamp
	return id, nil
}

// Has returns true if the given payload ID has been allocated.
func (a *PayloadIDAllocator) Has(id PayloadID) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.allocated[id]
	return ok
}

// Count returns the number of allocated payload IDs.
func (a *PayloadIDAllocator) Count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.allocated)
}

// Prune removes payload IDs older than the given timestamp.
func (a *PayloadIDAllocator) Prune(beforeTimestamp uint64) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	pruned := 0
	for id, ts := range a.allocated {
		if ts < beforeTimestamp {
			delete(a.allocated, id)
			pruned++
		}
	}
	return pruned
}

// ReorgTracker identifies head reorgs and tracks their depth.
type ReorgTracker struct {
	mu       sync.RWMutex
	lastHead types.Hash
	lastNum  uint64
	// blocks provides ancestry lookup.
	blocks map[types.Hash]*BlockInfo
	// history of detected reorgs.
	reorgs     []TrackedReorg
	maxHistory int
}

// TrackedReorg records a single reorg detection event.
type TrackedReorg struct {
	OldHead    types.Hash
	NewHead    types.Hash
	OldHeadNum uint64
	NewHeadNum uint64
	Depth      uint64
	Timestamp  time.Time
}

// NewReorgTracker creates a reorg tracker.
func NewReorgTracker(maxHistory int) *ReorgTracker {
	if maxHistory <= 0 {
		maxHistory = 128
	}
	return &ReorgTracker{
		blocks:     make(map[types.Hash]*BlockInfo),
		reorgs:     make([]TrackedReorg, 0),
		maxHistory: maxHistory,
	}
}

// AddBlock registers a block for ancestry lookup.
func (rt *ReorgTracker) AddBlock(info *BlockInfo) {
	if info == nil {
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.blocks[info.Hash] = info
}

// ProcessHead checks for a reorg when the head changes. Returns the reorg
// if detected, or nil if the head is a direct extension.
func (rt *ReorgTracker) ProcessHead(newHead types.Hash, newNum uint64) *TrackedReorg {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	oldHead := rt.lastHead
	oldNum := rt.lastNum
	rt.lastHead = newHead
	rt.lastNum = newNum

	if oldHead == (types.Hash{}) || oldHead == newHead {
		return nil
	}

	// Check if newHead is a descendant of oldHead (no reorg).
	if rt.isAncestorLocked(oldHead, newHead) {
		return nil
	}

	depth := rt.reorgDepthLocked(oldHead, newHead)
	reorg := TrackedReorg{
		OldHead:    oldHead,
		NewHead:    newHead,
		OldHeadNum: oldNum,
		NewHeadNum: newNum,
		Depth:      depth,
		Timestamp:  time.Now(),
	}

	if len(rt.reorgs) >= rt.maxHistory {
		rt.reorgs = rt.reorgs[1:]
	}
	rt.reorgs = append(rt.reorgs, reorg)

	return &reorg
}

// ReorgCount returns the total detected reorgs.
func (rt *ReorgTracker) ReorgCount() int {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return len(rt.reorgs)
}

// Reorgs returns a copy of all tracked reorgs.
func (rt *ReorgTracker) Reorgs() []TrackedReorg {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	result := make([]TrackedReorg, len(rt.reorgs))
	copy(result, rt.reorgs)
	return result
}

// isAncestorLocked checks ancestry. Caller must hold rt.mu.
func (rt *ReorgTracker) isAncestorLocked(ancestor, descendant types.Hash) bool {
	current := descendant
	for i := 0; i < 1024; i++ {
		if current == ancestor {
			return true
		}
		info, ok := rt.blocks[current]
		if !ok {
			return false
		}
		if info.ParentHash == current {
			return false
		}
		current = info.ParentHash
	}
	return false
}

// reorgDepthLocked computes reorg depth. Caller must hold rt.mu.
func (rt *ReorgTracker) reorgDepthLocked(oldHead, newHead types.Hash) uint64 {
	oldAnc := make(map[types.Hash]uint64)
	current := oldHead
	for d := uint64(0); d < 1024; d++ {
		oldAnc[current] = d
		info, ok := rt.blocks[current]
		if !ok || info.ParentHash == current {
			break
		}
		current = info.ParentHash
	}

	current = newHead
	for d := uint64(0); d < 1024; d++ {
		if oldDist, found := oldAnc[current]; found {
			if d > oldDist {
				return d
			}
			return oldDist
		}
		info, ok := rt.blocks[current]
		if !ok || info.ParentHash == current {
			break
		}
		current = info.ParentHash
	}
	return 0
}

// ForkchoiceTracker is the top-level tracker that composes HeadChain,
// FCUHistory, ConflictDetector, PayloadIDAllocator, and ReorgTracker.
type ForkchoiceTracker struct {
	Chain     *HeadChain
	History   *FCUHistory
	Conflicts *ConflictDetector
	Payloads  *PayloadIDAllocator
	Reorgs    *ReorgTracker
}

// NewForkchoiceTracker creates a fully-initialized forkchoice tracker.
func NewForkchoiceTracker(historySize, reorgHistorySize int) *ForkchoiceTracker {
	return &ForkchoiceTracker{
		Chain:     NewHeadChain(),
		History:   NewFCUHistory(historySize),
		Conflicts: NewConflictDetector(),
		Payloads:  NewPayloadIDAllocator(),
		Reorgs:    NewReorgTracker(reorgHistorySize),
	}
}

// ProcessUpdate handles a full forkchoice update: tracks state, detects
// conflicts and reorgs, and records the update in history.
func (ft *ForkchoiceTracker) ProcessUpdate(
	state ForkchoiceStateV1,
	hasAttrs bool,
	headNum, safeNum, finalNum uint64,
) (conflict bool, conflictReason string, reorg *TrackedReorg) {
	// Detect conflicts.
	conflict, conflictReason = ft.Conflicts.Check(state)

	// Update head chain.
	ft.Chain.Update(state.HeadBlockHash, state.SafeBlockHash,
		state.FinalizedBlockHash, headNum, safeNum, finalNum)

	// Detect reorgs.
	reorg = ft.Reorgs.ProcessHead(state.HeadBlockHash, headNum)

	// Record in history.
	record := FCURecord{
		Timestamp:     time.Now(),
		State:         state,
		HasAttributes: hasAttrs,
		Result:        StatusValid,
	}
	ft.History.Add(record)

	return conflict, conflictReason, reorg
}
