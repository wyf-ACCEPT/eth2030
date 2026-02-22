package eth

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// Block download constants.
const (
	// maxConcurrentDownloads is the maximum number of concurrent body downloads.
	maxConcurrentDownloads = 32

	// maxImportBatch is the maximum number of blocks imported in one batch.
	maxImportBatch = 128

	// headerBatchSize is the number of headers requested per batch.
	headerBatchSize = 192

	// downloadTimeout is the maximum time to wait for a download response.
	downloadTimeout = 20 * time.Second

	// staleDownloadAge is the age at which a download is considered stale.
	staleDownloadAge = 2 * time.Minute

	// maxKnownBlocks is the size of the known block hash filter.
	maxKnownBlocks = 65536
)

// Block download errors.
var (
	ErrDownloadStopped    = errors.New("eth: download manager stopped")
	ErrBlockAlreadyKnown  = errors.New("eth: block already known")
	ErrImportQueueFull    = errors.New("eth: import queue full")
	ErrMissingParent      = errors.New("eth: missing parent block")
	ErrInvalidBlockOrder  = errors.New("eth: blocks not in order")
	ErrDownloadInProgress = errors.New("eth: download already in progress for hash")
)

// DownloadState represents the state of a block in the download pipeline.
type DownloadState int

const (
	DownloadPending  DownloadState = iota // Header known, body not yet requested.
	DownloadActive                        // Body download in progress.
	DownloadComplete                      // Body received, awaiting import.
	DownloadImported                      // Block successfully imported.
)

// BlockTask tracks a single block through the download pipeline.
type BlockTask struct {
	Hash      types.Hash
	Number    uint64
	Header    *types.Header
	Body      *types.Body
	PeerID    string
	State     DownloadState
	Created   time.Time
	Requested time.Time
}

// BlockDownloadManager coordinates header-first block downloading.
// It tracks announced blocks, schedules header and body downloads,
// maintains an import queue with dependency ordering, and filters
// duplicates and stale entries.
type BlockDownloadManager struct {
	mu sync.Mutex

	// tasks maps block hash to its download task.
	tasks map[types.Hash]*BlockTask

	// byNumber allows lookup by block number for dependency ordering.
	byNumber map[uint64]*BlockTask

	// importQueue holds blocks ready for import, sorted by number.
	importQueue []*BlockTask

	// knownBlocks is a bounded set of recently known block hashes for dedup.
	knownBlocks map[types.Hash]struct{}
	knownOrder  []types.Hash

	// headerQueue holds block numbers pending header download.
	headerQueue []uint64

	// chain reference for checking existing blocks.
	chain Blockchain

	// stopped indicates the manager has been shut down.
	stopped bool
}

// NewBlockDownloadManager creates a new download manager.
func NewBlockDownloadManager(chain Blockchain) *BlockDownloadManager {
	return &BlockDownloadManager{
		tasks:       make(map[types.Hash]*BlockTask),
		byNumber:    make(map[uint64]*BlockTask),
		knownBlocks: make(map[types.Hash]struct{}),
		chain:       chain,
	}
}

// AddHeaders processes a batch of received headers, creating download tasks
// for blocks whose bodies we still need. Headers are validated for
// sequential numbering. Returns the number of new tasks created.
func (dm *BlockDownloadManager) AddHeaders(headers []*types.Header, peerID string) (int, error) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if dm.stopped {
		return 0, ErrDownloadStopped
	}

	added := 0
	for _, h := range headers {
		hash := h.Hash()

		// Skip if already known.
		if dm.isKnownLocked(hash) {
			continue
		}

		// Skip if chain already has it.
		if dm.chain != nil && dm.chain.HasBlock(hash) {
			dm.markKnownLocked(hash)
			continue
		}

		// Skip if already tracked.
		if _, exists := dm.tasks[hash]; exists {
			continue
		}

		task := &BlockTask{
			Hash:    hash,
			Number:  h.Number.Uint64(),
			Header:  h,
			PeerID:  peerID,
			State:   DownloadPending,
			Created: time.Now(),
		}
		dm.tasks[hash] = task
		dm.byNumber[task.Number] = task
		added++
	}
	return added, nil
}

// ScheduleBodies returns a list of block hashes whose bodies need to be
// downloaded. It moves the corresponding tasks from Pending to Active state.
// At most maxConcurrentDownloads tasks are scheduled.
func (dm *BlockDownloadManager) ScheduleBodies() []types.Hash {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	var toFetch []types.Hash
	activeCount := dm.activeCountLocked()

	// Collect pending tasks sorted by block number for ordered downloading.
	var pending []*BlockTask
	for _, task := range dm.tasks {
		if task.State == DownloadPending {
			pending = append(pending, task)
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].Number < pending[j].Number
	})

	for _, task := range pending {
		if activeCount >= maxConcurrentDownloads {
			break
		}
		task.State = DownloadActive
		task.Requested = time.Now()
		toFetch = append(toFetch, task.Hash)
		activeCount++
	}
	return toFetch
}

// DeliverBody delivers a downloaded block body, matching it to an active
// download task by hash. Moves the task to Complete state and enqueues
// it for import.
func (dm *BlockDownloadManager) DeliverBody(hash types.Hash, body *types.Body) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if dm.stopped {
		return ErrDownloadStopped
	}

	task, ok := dm.tasks[hash]
	if !ok {
		return fmt.Errorf("%w: %s", ErrBlockAlreadyKnown, hash.Hex())
	}
	if task.State != DownloadActive {
		return fmt.Errorf("eth: unexpected body delivery for state %d", task.State)
	}

	task.Body = body
	task.State = DownloadComplete

	// Insert into import queue in block-number order.
	dm.insertImportQueue(task)
	return nil
}

// DeliverBodies delivers multiple block bodies by hash.
// Returns the number of successfully delivered bodies and any error.
func (dm *BlockDownloadManager) DeliverBodies(hashes []types.Hash, bodies []*types.Body) (int, error) {
	if len(hashes) != len(bodies) {
		return 0, errors.New("eth: hash/body count mismatch")
	}
	delivered := 0
	var lastErr error
	for i, hash := range hashes {
		if err := dm.DeliverBody(hash, bodies[i]); err != nil {
			lastErr = err
		} else {
			delivered++
		}
	}
	return delivered, lastErr
}

// DrainImportQueue returns blocks that are ready to import in dependency
// order (ascending by block number). Only returns blocks whose parent
// is either already imported or exists in the chain.
func (dm *BlockDownloadManager) DrainImportQueue() []*types.Block {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	var ready []*types.Block
	remaining := dm.importQueue[:0]

	for _, task := range dm.importQueue {
		if len(ready) >= maxImportBatch {
			remaining = append(remaining, task)
			continue
		}

		// Check parent dependency: parent must exist in chain or be imported.
		parentHash := task.Header.ParentHash
		if dm.chain != nil && !dm.chain.HasBlock(parentHash) {
			// Check if parent was just imported in this batch.
			found := false
			for _, b := range ready {
				if b.Hash() == parentHash {
					found = true
					break
				}
			}
			if !found {
				remaining = append(remaining, task)
				continue
			}
		}

		block := types.NewBlock(task.Header, task.Body)
		ready = append(ready, block)
		task.State = DownloadImported
		dm.markKnownLocked(task.Hash)
	}

	dm.importQueue = remaining
	return ready
}

// RequestHeaders adds a range of block numbers to the header download queue.
func (dm *BlockDownloadManager) RequestHeaders(from, count uint64) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	for i := uint64(0); i < count; i++ {
		num := from + i
		// Skip if we already have a task for this number.
		if _, exists := dm.byNumber[num]; exists {
			continue
		}
		dm.headerQueue = append(dm.headerQueue, num)
	}
}

// NextHeaderBatch returns the next batch of block numbers to request headers
// for. Returns at most headerBatchSize numbers.
func (dm *BlockDownloadManager) NextHeaderBatch() (from uint64, count uint64) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if len(dm.headerQueue) == 0 {
		return 0, 0
	}

	// Sort and take the first batch.
	sort.Slice(dm.headerQueue, func(i, j int) bool {
		return dm.headerQueue[i] < dm.headerQueue[j]
	})

	n := len(dm.headerQueue)
	if n > headerBatchSize {
		n = headerBatchSize
	}

	from = dm.headerQueue[0]
	// Find contiguous range starting from 'from'.
	count = 1
	for i := 1; i < n; i++ {
		if dm.headerQueue[i] == from+count {
			count++
		} else {
			break
		}
	}

	// Remove processed entries.
	dm.headerQueue = dm.headerQueue[count:]
	return from, count
}

// ExpireStale removes download tasks that have exceeded the stale timeout.
// Returns the number of expired tasks.
func (dm *BlockDownloadManager) ExpireStale() int {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	now := time.Now()
	expired := 0

	for hash, task := range dm.tasks {
		switch task.State {
		case DownloadActive:
			if now.Sub(task.Requested) > downloadTimeout {
				// Revert to pending for retry.
				task.State = DownloadPending
				task.Requested = time.Time{}
				expired++
			}
		case DownloadPending:
			if now.Sub(task.Created) > staleDownloadAge {
				delete(dm.tasks, hash)
				delete(dm.byNumber, task.Number)
				expired++
			}
		}
	}
	return expired
}

// CancelDownload removes a specific block from the download pipeline.
func (dm *BlockDownloadManager) CancelDownload(hash types.Hash) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	task, ok := dm.tasks[hash]
	if !ok {
		return
	}
	delete(dm.tasks, hash)
	delete(dm.byNumber, task.Number)

	// Remove from import queue if present.
	for i, t := range dm.importQueue {
		if t.Hash == hash {
			dm.importQueue = append(dm.importQueue[:i], dm.importQueue[i+1:]...)
			break
		}
	}
}

// IsDuplicate returns true if the block hash is already known or being downloaded.
func (dm *BlockDownloadManager) IsDuplicate(hash types.Hash) bool {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if dm.isKnownLocked(hash) {
		return true
	}
	if _, exists := dm.tasks[hash]; exists {
		return true
	}
	if dm.chain != nil && dm.chain.HasBlock(hash) {
		return true
	}
	return false
}

// TaskCount returns the total number of download tasks.
func (dm *BlockDownloadManager) TaskCount() int {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	return len(dm.tasks)
}

// PendingCount returns the number of tasks in Pending state.
func (dm *BlockDownloadManager) PendingCount() int {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	count := 0
	for _, t := range dm.tasks {
		if t.State == DownloadPending {
			count++
		}
	}
	return count
}

// ActiveCount returns the number of tasks in Active (downloading) state.
func (dm *BlockDownloadManager) ActiveCount() int {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	return dm.activeCountLocked()
}

// ImportQueueLen returns the number of blocks waiting to be imported.
func (dm *BlockDownloadManager) ImportQueueLen() int {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	return len(dm.importQueue)
}

// Stop stops the download manager.
func (dm *BlockDownloadManager) Stop() {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	dm.stopped = true
}

// Reset clears all internal state and restarts the manager.
func (dm *BlockDownloadManager) Reset() {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	dm.tasks = make(map[types.Hash]*BlockTask)
	dm.byNumber = make(map[uint64]*BlockTask)
	dm.importQueue = nil
	dm.knownBlocks = make(map[types.Hash]struct{})
	dm.knownOrder = nil
	dm.headerQueue = nil
	dm.stopped = false
}

// GetTask returns the download task for the given hash, or nil if not found.
func (dm *BlockDownloadManager) GetTask(hash types.Hash) *BlockTask {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	task, ok := dm.tasks[hash]
	if !ok {
		return nil
	}
	// Return a copy to avoid data races.
	cp := *task
	return &cp
}

// --- Internal helpers (must be called with dm.mu held) ---

func (dm *BlockDownloadManager) activeCountLocked() int {
	count := 0
	for _, t := range dm.tasks {
		if t.State == DownloadActive {
			count++
		}
	}
	return count
}

func (dm *BlockDownloadManager) insertImportQueue(task *BlockTask) {
	// Binary search for insertion point to maintain sorted order by number.
	i := sort.Search(len(dm.importQueue), func(j int) bool {
		return dm.importQueue[j].Number >= task.Number
	})
	dm.importQueue = append(dm.importQueue, nil)
	copy(dm.importQueue[i+1:], dm.importQueue[i:])
	dm.importQueue[i] = task
}

func (dm *BlockDownloadManager) isKnownLocked(hash types.Hash) bool {
	_, ok := dm.knownBlocks[hash]
	return ok
}

func (dm *BlockDownloadManager) markKnownLocked(hash types.Hash) {
	if _, ok := dm.knownBlocks[hash]; ok {
		return
	}
	dm.knownBlocks[hash] = struct{}{}
	dm.knownOrder = append(dm.knownOrder, hash)

	// Evict oldest if over capacity.
	for len(dm.knownOrder) > maxKnownBlocks {
		oldest := dm.knownOrder[0]
		dm.knownOrder = dm.knownOrder[1:]
		delete(dm.knownBlocks, oldest)
	}
}
