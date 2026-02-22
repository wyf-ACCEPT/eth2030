// skeleton_chain.go implements a skeleton chain for header-first
// synchronization. The skeleton is a sparse set of anchor headers fetched
// at regular intervals from trusted peers. After the skeleton is built,
// gap-filling downloads contiguous headers between anchors, bodies and
// receipts are fetched in parallel, and a pivot point is chosen for snap
// sync. Download throttling limits the in-flight bytes and tasks to
// protect memory and bandwidth.
package sync

import (
	"errors"
	"fmt"
	gosync "sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// Skeleton chain errors.
var (
	ErrSkeletonEmpty       = errors.New("skeleton: no anchors set")
	ErrSkeletonOverlap     = errors.New("skeleton: anchor overlaps existing range")
	ErrSkeletonGapNotFound = errors.New("skeleton: gap segment not found")
	ErrSkeletonBadLink     = errors.New("skeleton: anchor does not link to filled chain")
	ErrThrottled           = errors.New("skeleton: download throttled")
)

// Default skeleton parameters.
const (
	DefaultSkeletonStride   = 2048  // Blocks between skeleton anchors.
	DefaultMaxInFlight      = 8     // Maximum concurrent in-flight fetch tasks.
	DefaultMaxInFlightBytes = 64 << 20 // 64 MiB max in-flight data.
	DefaultReceiptBatch     = 64    // Receipts per fetch request.
)

// SkeletonConfig configures the skeleton chain builder.
type SkeletonConfig struct {
	Stride          uint64 // Block interval between skeleton anchors.
	MaxInFlight     int    // Maximum concurrent fetch tasks.
	MaxInFlightBytes int64 // Maximum bytes of in-flight data.
	ReceiptBatch    int    // Number of receipts per fetch batch.
}

// DefaultSkeletonConfig returns sensible defaults.
func DefaultSkeletonConfig() SkeletonConfig {
	return SkeletonConfig{
		Stride:           DefaultSkeletonStride,
		MaxInFlight:      DefaultMaxInFlight,
		MaxInFlightBytes: DefaultMaxInFlightBytes,
		ReceiptBatch:     DefaultReceiptBatch,
	}
}

// SkeletonAnchor is a trusted header at a known height that serves as a
// reference point in the skeleton chain. Anchors are fetched first and
// gaps between them are filled in a second pass.
type SkeletonAnchor struct {
	Number     uint64
	Hash       types.Hash
	ParentHash types.Hash
	Timestamp  uint64
}

// GapSegment describes a contiguous range of missing headers between two
// anchors that still needs to be downloaded.
type GapSegment struct {
	Start  uint64 // First missing block number (inclusive).
	End    uint64 // Last missing block number (inclusive).
	Filled bool   // True once all headers in the segment are downloaded.
}

// ReceiptTask represents a pending receipt download request.
type ReceiptTask struct {
	Hashes   []types.Hash // Block hashes to fetch receipts for.
	PeerID   string       // Assigned peer.
	Issued   time.Time    // When the task was dispatched.
	Complete bool         // Whether receipts have been received.
}

// ThrottleState tracks the current download throttling pressure.
type ThrottleState struct {
	InFlightTasks int   // Number of active fetch tasks.
	InFlightBytes int64 // Estimated bytes of in-flight data.
}

// IsThrottled returns true if any throttle limit is reached.
func (ts ThrottleState) IsThrottled(config SkeletonConfig) bool {
	if config.MaxInFlight > 0 && ts.InFlightTasks >= config.MaxInFlight {
		return true
	}
	if config.MaxInFlightBytes > 0 && ts.InFlightBytes >= config.MaxInFlightBytes {
		return true
	}
	return false
}

// SkeletonChain manages the skeleton chain structure used during
// header-first sync. It stores sparse anchors, tracks gap segments,
// handles receipt fetching, and enforces download throttling.
// All methods are safe for concurrent use.
type SkeletonChain struct {
	mu     gosync.RWMutex
	config SkeletonConfig

	// Anchors stored in ascending block number order.
	anchors []SkeletonAnchor

	// Gap segments derived from anchors.
	gaps []GapSegment

	// Filled headers indexed by block number.
	filled map[uint64]*types.Header

	// Receipt download tasks by block hash.
	receiptTasks map[types.Hash]*ReceiptTask

	// Throttle accounting.
	throttle ThrottleState

	// Pivot block number for snap sync (0 if not yet selected).
	pivot uint64
}

// NewSkeletonChain creates a new empty skeleton chain.
func NewSkeletonChain(config SkeletonConfig) *SkeletonChain {
	return &SkeletonChain{
		config:       config,
		filled:       make(map[uint64]*types.Header),
		receiptTasks: make(map[types.Hash]*ReceiptTask),
	}
}

// AddAnchor inserts a trusted anchor header into the skeleton.
// Anchors must be added in ascending block number order.
func (sc *SkeletonChain) AddAnchor(anchor SkeletonAnchor) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Reject if this anchor number overlaps an existing one.
	for _, a := range sc.anchors {
		if a.Number == anchor.Number {
			return ErrSkeletonOverlap
		}
	}

	sc.anchors = append(sc.anchors, anchor)
	// Keep anchors sorted.
	for i := len(sc.anchors) - 1; i > 0 && sc.anchors[i].Number < sc.anchors[i-1].Number; i-- {
		sc.anchors[i], sc.anchors[i-1] = sc.anchors[i-1], sc.anchors[i]
	}

	sc.rebuildGapsLocked()
	return nil
}

// BuildSkeleton constructs anchors from start to end at the configured
// stride using the provided header source. Returns the number of anchors
// successfully fetched.
func (sc *SkeletonChain) BuildSkeleton(start, end uint64, source HeaderSource) (int, error) {
	if start > end {
		return 0, ErrInvalidRange
	}

	stride := sc.config.Stride
	if stride == 0 {
		stride = DefaultSkeletonStride
	}

	count := 0
	for num := start; num <= end; num += stride {
		headers, err := source.FetchHeaders(num, 1)
		if err != nil {
			return count, fmt.Errorf("skeleton: fetch anchor %d: %w", num, err)
		}
		if len(headers) == 0 {
			return count, fmt.Errorf("skeleton: empty response for anchor %d", num)
		}
		h := headers[0]
		anchor := SkeletonAnchor{
			Number:     h.Number.Uint64(),
			Hash:       h.Hash(),
			ParentHash: h.ParentHash,
			Timestamp:  h.Time,
		}
		if err := sc.AddAnchor(anchor); err != nil {
			return count, err
		}
		count++
	}

	// Always include the end block if it was not covered by stride.
	if end > start {
		last := sc.Anchors()
		if len(last) == 0 || last[len(last)-1].Number < end {
			headers, err := source.FetchHeaders(end, 1)
			if err == nil && len(headers) > 0 {
				h := headers[0]
				_ = sc.AddAnchor(SkeletonAnchor{
					Number:     h.Number.Uint64(),
					Hash:       h.Hash(),
					ParentHash: h.ParentHash,
					Timestamp:  h.Time,
				})
				count++
			}
		}
	}

	return count, nil
}

// Anchors returns a copy of all skeleton anchors in ascending order.
func (sc *SkeletonChain) Anchors() []SkeletonAnchor {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	result := make([]SkeletonAnchor, len(sc.anchors))
	copy(result, sc.anchors)
	return result
}

// Gaps returns a copy of all gap segments.
func (sc *SkeletonChain) Gaps() []GapSegment {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	result := make([]GapSegment, len(sc.gaps))
	copy(result, sc.gaps)
	return result
}

// NextGap returns the first unfilled gap segment, or an error if all
// gaps have been filled or no gaps exist.
func (sc *SkeletonChain) NextGap() (GapSegment, error) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	for _, g := range sc.gaps {
		if !g.Filled {
			return g, nil
		}
	}
	return GapSegment{}, ErrSkeletonGapNotFound
}

// FillHeaders stores downloaded headers that fill a gap segment. It
// validates basic parent-hash linkage within the segment.
func (sc *SkeletonChain) FillHeaders(headers []*types.Header) error {
	if len(headers) == 0 {
		return nil
	}

	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Validate sequential parent linkage within the batch.
	for i := 1; i < len(headers); i++ {
		prev := headers[i-1]
		cur := headers[i]
		if cur.ParentHash != prev.Hash() {
			return fmt.Errorf("%w: header %d parent %s does not match %s",
				ErrSkeletonBadLink, cur.Number.Uint64(), cur.ParentHash.Hex(), prev.Hash().Hex())
		}
	}

	// Store headers.
	for _, h := range headers {
		sc.filled[h.Number.Uint64()] = h
	}

	// Update gap segments.
	sc.updateGapsLocked()
	return nil
}

// FilledCount returns the number of filled headers.
func (sc *SkeletonChain) FilledCount() int {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return len(sc.filled)
}

// FilledHeader returns a filled header by block number, or nil if not yet filled.
func (sc *SkeletonChain) FilledHeader(number uint64) *types.Header {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.filled[number]
}

// SelectPivotBlock chooses a pivot block for snap sync. The pivot is set
// to the highest anchor minus a safety margin (64 blocks), ensuring the
// state is available from peers. Returns the pivot block number.
func (sc *SkeletonChain) SelectPivotBlock() (uint64, error) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if len(sc.anchors) == 0 {
		return 0, ErrSkeletonEmpty
	}

	highest := sc.anchors[len(sc.anchors)-1].Number
	const pivotMargin = 64
	if highest <= pivotMargin {
		sc.pivot = 1
	} else {
		sc.pivot = highest - pivotMargin
	}
	return sc.pivot, nil
}

// Pivot returns the selected pivot block number (0 if not yet selected).
func (sc *SkeletonChain) Pivot() uint64 {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.pivot
}

// QueueReceiptTask creates a receipt fetch task for the given block hashes
// and assigns it to the specified peer. Returns ErrThrottled if the
// download throttle limits have been reached.
func (sc *SkeletonChain) QueueReceiptTask(hashes []types.Hash, peerID string, estimatedBytes int64) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if sc.throttle.IsThrottled(sc.config) {
		return ErrThrottled
	}

	for _, h := range hashes {
		sc.receiptTasks[h] = &ReceiptTask{
			Hashes: hashes,
			PeerID: peerID,
			Issued: time.Now(),
		}
	}

	sc.throttle.InFlightTasks++
	sc.throttle.InFlightBytes += estimatedBytes
	return nil
}

// CompleteReceiptTask marks a receipt task as complete and releases the
// throttle accounting.
func (sc *SkeletonChain) CompleteReceiptTask(hash types.Hash, releasedBytes int64) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if task, ok := sc.receiptTasks[hash]; ok {
		task.Complete = true
	}

	sc.throttle.InFlightTasks--
	if sc.throttle.InFlightTasks < 0 {
		sc.throttle.InFlightTasks = 0
	}
	sc.throttle.InFlightBytes -= releasedBytes
	if sc.throttle.InFlightBytes < 0 {
		sc.throttle.InFlightBytes = 0
	}
}

// ThrottleStatus returns the current download throttle state.
func (sc *SkeletonChain) ThrottleStatus() ThrottleState {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.throttle
}

// IsComplete returns true if all gaps have been filled.
func (sc *SkeletonChain) IsComplete() bool {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	for _, g := range sc.gaps {
		if !g.Filled {
			return false
		}
	}
	return len(sc.anchors) > 0
}

// Reset clears all skeleton state.
func (sc *SkeletonChain) Reset() {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.anchors = nil
	sc.gaps = nil
	sc.filled = make(map[uint64]*types.Header)
	sc.receiptTasks = make(map[types.Hash]*ReceiptTask)
	sc.throttle = ThrottleState{}
	sc.pivot = 0
}

// --- internal helpers (caller must hold sc.mu write lock) ---

// rebuildGapsLocked recomputes gap segments from the current anchors.
func (sc *SkeletonChain) rebuildGapsLocked() {
	sc.gaps = nil
	for i := 0; i < len(sc.anchors)-1; i++ {
		lo := sc.anchors[i].Number + 1
		hi := sc.anchors[i+1].Number - 1
		if lo <= hi {
			sc.gaps = append(sc.gaps, GapSegment{
				Start: lo,
				End:   hi,
			})
		}
	}
}

// updateGapsLocked marks gap segments as filled when all their headers
// have been stored.
func (sc *SkeletonChain) updateGapsLocked() {
	for i := range sc.gaps {
		if sc.gaps[i].Filled {
			continue
		}
		filled := true
		for num := sc.gaps[i].Start; num <= sc.gaps[i].End; num++ {
			if _, ok := sc.filled[num]; !ok {
				filled = false
				break
			}
		}
		sc.gaps[i].Filled = filled
	}
}
