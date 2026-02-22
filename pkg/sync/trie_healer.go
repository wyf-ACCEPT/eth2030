// trie_healer.go implements an advanced snap sync state healing subsystem.
// It builds on the base StateHealer with a priority queue that processes
// shallowest trie nodes first, per-account storage trie healing, progress
// checkpointing for resume after restart, and robust completion detection.
package sync

import (
	"errors"
	"fmt"
	gosync "sync"
	"sync/atomic"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Trie healer configuration constants.
const (
	DefaultTrieHealBatch      = 256
	DefaultTrieHealMaxRetries = 3
	DefaultCheckpointInterval = 1000 // nodes between checkpoints
)

// Trie healer errors.
var (
	ErrTrieHealerClosed    = errors.New("trie healer: closed")
	ErrTrieHealerRunning   = errors.New("trie healer: already running")
	ErrTrieHealNoPeer      = errors.New("trie healer: no peer available")
	ErrTrieHealCheckpoint  = errors.New("trie healer: checkpoint save failed")
	ErrTrieHealInvalidNode = errors.New("trie healer: invalid node data")
)

// TrieHealNode represents a trie node to be healed, with depth for priority.
type TrieHealNode struct {
	Path        []byte     // Nibble-encoded trie path.
	AccountHash types.Hash // Zero for state trie, account hash for storage tries.
	Root        types.Hash // Trie root this node belongs to.
	Depth       int        // Path depth (shorter = higher priority).
	Retries     int
	CreatedAt   time.Time
}

func (n *TrieHealNode) isStorageTrie() bool { return n.AccountHash != (types.Hash{}) }

// TrieHealCheckpoint stores healing state for resume after restart.
type TrieHealCheckpoint struct {
	StateRoot       types.Hash
	NodesHealed, NodesFailed, BytesDownloaded uint64
	PendingPaths    [][]byte
	AccountRoots    []types.Hash
	Timestamp       time.Time
}

// TrieHealProgress tracks overall healing progress.
type TrieHealProgress struct {
	StateNodesDetected, StateNodesHealed, StateNodesFailed       uint64
	StorageNodesDetected, StorageNodesHealed, StorageNodesFailed uint64
	BytesDownloaded                                              uint64
	AccountsHealed, AccountsTotal                                int
	CheckpointsWritten                                           int
	StartTime                                                    time.Time
	Complete                                                     bool
}

func (p *TrieHealProgress) TotalHealed() uint64   { return p.StateNodesHealed + p.StorageNodesHealed }
func (p *TrieHealProgress) TotalDetected() uint64  { return p.StateNodesDetected + p.StorageNodesDetected }

// Percentage returns an estimated healing completion percentage.
func (p *TrieHealProgress) Percentage() float64 {
	total := p.TotalDetected()
	if total == 0 {
		return 100.0
	}
	healed := p.TotalHealed() + uint64(p.StateNodesFailed) + uint64(p.StorageNodesFailed)
	if healed >= total {
		return 100.0
	}
	return float64(healed) / float64(total) * 100.0
}

// TrieHealConfig configures the TrieHealer.
type TrieHealConfig struct {
	BatchSize          int
	MaxRetries         int
	CheckpointInterval int
}

// DefaultTrieHealConfig returns sensible defaults.
func DefaultTrieHealConfig() TrieHealConfig {
	return TrieHealConfig{
		BatchSize:          DefaultTrieHealBatch,
		MaxRetries:         DefaultTrieHealMaxRetries,
		CheckpointInterval: DefaultCheckpointInterval,
	}
}

// trieHealPriorityQueue implements a min-heap on node depth (shallowest first).
type trieHealPriorityQueue []*TrieHealNode

func (pq trieHealPriorityQueue) Len() int            { return len(pq) }
func (pq trieHealPriorityQueue) Less(i, j int) bool   { return pq[i].Depth < pq[j].Depth }
func (pq trieHealPriorityQueue) Swap(i, j int)        { pq[i], pq[j] = pq[j], pq[i] }

func (pq *trieHealPriorityQueue) Push(node *TrieHealNode) {
	*pq = append(*pq, node)
	pq.up(len(*pq) - 1)
}

func (pq *trieHealPriorityQueue) Pop() *TrieHealNode {
	old := *pq
	n := len(old)
	if n == 0 {
		return nil
	}
	node := old[0]
	old[0] = old[n-1]
	old[n-1] = nil
	*pq = old[:n-1]
	if len(*pq) > 0 {
		pq.down(0)
	}
	return node
}

func (pq trieHealPriorityQueue) up(j int) {
	for {
		i := (j - 1) / 2
		if i == j || !pq.Less(j, i) {
			break
		}
		pq.Swap(i, j)
		j = i
	}
}

func (pq trieHealPriorityQueue) down(i int) {
	n := len(pq)
	for {
		j1 := 2*i + 1
		if j1 >= n {
			break
		}
		j := j1
		if j2 := j1 + 1; j2 < n && pq.Less(j2, j1) {
			j = j2
		}
		if !pq.Less(j, i) {
			break
		}
		pq.Swap(i, j)
		i = j
	}
}

// TrieHealer coordinates trie healing with a priority queue, per-account
// storage healing, checkpoint/resume, and completion detection.
type TrieHealer struct {
	mu       gosync.Mutex
	config   TrieHealConfig
	root     types.Hash
	writer   StateWriter
	closed   atomic.Bool
	running  atomic.Bool
	progress TrieHealProgress

	queue            trieHealPriorityQueue
	seen             map[string]bool
	failed           map[string]*TrieHealNode
	storageRoots     map[types.Hash]types.Hash // account hash -> storage root
	lastCheckpoint   TrieHealCheckpoint
	nodesSinceCheckpt uint64
	saveCheckpoint   func(TrieHealCheckpoint) error
}

// NewTrieHealer creates a new trie healer for the given state root.
func NewTrieHealer(config TrieHealConfig, root types.Hash, writer StateWriter) *TrieHealer {
	if config.BatchSize <= 0 {
		config.BatchSize = DefaultTrieHealBatch
	}
	if config.MaxRetries <= 0 {
		config.MaxRetries = DefaultTrieHealMaxRetries
	}
	if config.CheckpointInterval <= 0 {
		config.CheckpointInterval = DefaultCheckpointInterval
	}
	return &TrieHealer{
		config:       config,
		root:         root,
		writer:       writer,
		seen:         make(map[string]bool),
		failed:       make(map[string]*TrieHealNode),
		storageRoots: make(map[types.Hash]types.Hash),
	}
}

// SetCheckpointCallback registers a function to persist healing checkpoints.
func (th *TrieHealer) SetCheckpointCallback(fn func(TrieHealCheckpoint) error) {
	th.mu.Lock()
	defer th.mu.Unlock()
	th.saveCheckpoint = fn
}

// ResumeFromCheckpoint restores healing state from a prior checkpoint.
func (th *TrieHealer) ResumeFromCheckpoint(cp TrieHealCheckpoint) {
	th.mu.Lock()
	defer th.mu.Unlock()
	th.progress.StateNodesHealed = cp.NodesHealed
	th.progress.StateNodesFailed = cp.NodesFailed
	th.progress.BytesDownloaded = cp.BytesDownloaded
	th.lastCheckpoint = cp
	now := time.Now()
	for _, path := range cp.PendingPaths {
		key := string(path)
		if !th.seen[key] {
			th.queue.Push(&TrieHealNode{Path: copySlice(path), Root: th.root, Depth: len(path), CreatedAt: now})
			th.seen[key] = true
		}
	}
	for _, acctRoot := range cp.AccountRoots {
		th.storageRoots[acctRoot] = acctRoot
	}
}

// AddStorageTrie registers an account's storage trie for healing.
func (th *TrieHealer) AddStorageTrie(accountHash, storageRoot types.Hash) {
	th.mu.Lock()
	defer th.mu.Unlock()
	if storageRoot == types.EmptyRootHash || storageRoot == (types.Hash{}) {
		return
	}
	th.storageRoots[accountHash] = storageRoot
	th.progress.AccountsTotal = len(th.storageRoots)
}

// DetectStateGaps scans for missing nodes in the main state trie.
func (th *TrieHealer) DetectStateGaps() int {
	if th.closed.Load() {
		return 0
	}
	missing := th.writer.MissingTrieNodes(th.root, 0)
	th.mu.Lock()
	defer th.mu.Unlock()
	added := 0
	now := time.Now()
	for _, path := range missing {
		key := string(path)
		if th.seen[key] {
			continue
		}
		th.queue.Push(&TrieHealNode{Path: copySlice(path), Root: th.root, Depth: len(path), CreatedAt: now})
		th.seen[key] = true
		added++
	}
	th.progress.StateNodesDetected += uint64(added)
	return added
}

// DetectStorageGaps scans for missing nodes in registered storage tries.
func (th *TrieHealer) DetectStorageGaps() int {
	if th.closed.Load() {
		return 0
	}
	th.mu.Lock()
	roots := make(map[types.Hash]types.Hash, len(th.storageRoots))
	for k, v := range th.storageRoots {
		roots[k] = v
	}
	th.mu.Unlock()

	totalAdded := 0
	now := time.Now()
	for accountHash, storageRoot := range roots {
		missing := th.writer.MissingTrieNodes(storageRoot, 0)
		th.mu.Lock()
		for _, path := range missing {
			key := string(accountHash[:]) + string(path)
			if th.seen[key] {
				continue
			}
			th.queue.Push(&TrieHealNode{
				Path: copySlice(path), AccountHash: accountHash,
				Root: storageRoot, Depth: len(path), CreatedAt: now,
			})
			th.seen[key] = true
			totalAdded++
		}
		th.progress.StorageNodesDetected += uint64(len(missing))
		th.mu.Unlock()
	}
	return totalAdded
}

// ScheduleBatch extracts the next batch from the priority queue.
func (th *TrieHealer) ScheduleBatch() []*TrieHealNode {
	th.mu.Lock()
	defer th.mu.Unlock()
	batchSize := th.config.BatchSize
	if batchSize > th.queue.Len() {
		batchSize = th.queue.Len()
	}
	batch := make([]*TrieHealNode, 0, batchSize)
	for i := 0; i < batchSize; i++ {
		if node := th.queue.Pop(); node != nil {
			batch = append(batch, node)
		}
	}
	return batch
}

// retryOrFail re-queues a node or marks it failed. Caller must hold th.mu.
func (th *TrieHealer) retryOrFail(node *TrieHealNode) {
	node.Retries++
	if node.Retries >= th.config.MaxRetries {
		th.failed[th.nodeKey(node)] = node
		if node.isStorageTrie() {
			th.progress.StorageNodesFailed++
		} else {
			th.progress.StateNodesFailed++
		}
	} else {
		th.queue.Push(node)
	}
}

// ProcessResults processes healing results for a batch of nodes.
func (th *TrieHealer) ProcessResults(batch []*TrieHealNode, results [][]byte) error {
	if th.closed.Load() {
		return ErrTrieHealerClosed
	}
	th.mu.Lock()
	defer th.mu.Unlock()

	for i, node := range batch {
		var data []byte
		if i < len(results) {
			data = results[i]
		}
		if len(data) == 0 || crypto.Keccak256Hash(data) == (types.Hash{}) {
			th.retryOrFail(node)
			continue
		}
		if err := th.writer.WriteTrieNode(node.Path, data); err != nil {
			return fmt.Errorf("trie healer: write node: %w", err)
		}
		if node.isStorageTrie() {
			th.progress.StorageNodesHealed++
		} else {
			th.progress.StateNodesHealed++
		}
		th.progress.BytesDownloaded += uint64(len(data))
		th.nodesSinceCheckpt++
	}
	if th.nodesSinceCheckpt >= uint64(th.config.CheckpointInterval) {
		th.writeCheckpointLocked()
	}
	return nil
}

// CheckCompletion verifies all tries are fully healed.
func (th *TrieHealer) CheckCompletion() bool {
	if th.closed.Load() {
		return false
	}
	th.mu.Lock()
	if th.queue.Len() > 0 {
		th.mu.Unlock()
		return false
	}
	roots := make(map[types.Hash]types.Hash, len(th.storageRoots))
	for k, v := range th.storageRoots {
		roots[k] = v
	}
	th.mu.Unlock()

	if len(th.writer.MissingTrieNodes(th.root, 1)) > 0 {
		return false
	}
	for _, sr := range roots {
		if len(th.writer.MissingTrieNodes(sr, 1)) > 0 {
			return false
		}
	}
	th.mu.Lock()
	th.progress.Complete = true
	th.progress.AccountsHealed = len(th.storageRoots)
	th.mu.Unlock()
	return true
}

// Run executes the full healing loop: state trie first, then storage tries.
func (th *TrieHealer) Run(peer SnapPeer) error {
	if !th.running.CompareAndSwap(false, true) {
		return ErrTrieHealerRunning
	}
	defer th.running.Store(false)
	th.mu.Lock()
	th.progress.StartTime = time.Now()
	th.mu.Unlock()

	for !th.closed.Load() {
		stateGaps := th.DetectStateGaps()
		storageGaps := th.DetectStorageGaps()
		th.mu.Lock()
		queueLen := th.queue.Len()
		th.mu.Unlock()

		if stateGaps == 0 && storageGaps == 0 && queueLen == 0 {
			if th.CheckCompletion() {
				return nil
			}
			th.mu.Lock()
			th.progress.Complete = true
			th.mu.Unlock()
			return nil
		}
		for {
			if th.closed.Load() {
				return ErrTrieHealerClosed
			}
			batch := th.ScheduleBatch()
			if len(batch) == 0 {
				break
			}
			paths := make([][]byte, len(batch))
			for i, n := range batch {
				paths[i] = n.Path
			}
			results, err := peer.RequestTrieNodes(th.root, paths)
			if err != nil {
				th.mu.Lock()
				for _, n := range batch {
					th.queue.Push(n)
				}
				th.mu.Unlock()
				return fmt.Errorf("trie healer: request failed: %w", err)
			}
			if err := th.ProcessResults(batch, results); err != nil {
				return err
			}
		}
	}
	return ErrTrieHealerClosed
}

func (th *TrieHealer) Progress() TrieHealProgress { th.mu.Lock(); defer th.mu.Unlock(); return th.progress }
func (th *TrieHealer) QueueLen() int              { th.mu.Lock(); defer th.mu.Unlock(); return th.queue.Len() }
func (th *TrieHealer) FailedCount() int            { th.mu.Lock(); defer th.mu.Unlock(); return len(th.failed) }
func (th *TrieHealer) Close()                      { th.closed.Store(true) }

// Reset clears all healing state.
func (th *TrieHealer) Reset() {
	th.mu.Lock()
	defer th.mu.Unlock()
	th.queue = nil
	th.seen = make(map[string]bool)
	th.failed = make(map[string]*TrieHealNode)
	th.storageRoots = make(map[types.Hash]types.Hash)
	th.progress = TrieHealProgress{}
	th.nodesSinceCheckpt = 0
}

func (th *TrieHealer) nodeKey(node *TrieHealNode) string {
	if node.isStorageTrie() {
		return string(node.AccountHash[:]) + string(node.Path)
	}
	return string(node.Path)
}

// writeCheckpointLocked persists a healing checkpoint. Caller must hold th.mu.
func (th *TrieHealer) writeCheckpointLocked() {
	if th.saveCheckpoint == nil {
		th.nodesSinceCheckpt = 0
		return
	}
	paths := make([][]byte, 0, th.queue.Len())
	for _, node := range th.queue {
		paths = append(paths, copySlice(node.Path))
	}
	acctRoots := make([]types.Hash, 0, len(th.storageRoots))
	for _, root := range th.storageRoots {
		acctRoots = append(acctRoots, root)
	}
	cp := TrieHealCheckpoint{
		StateRoot:       th.root,
		NodesHealed:     th.progress.StateNodesHealed + th.progress.StorageNodesHealed,
		NodesFailed:     th.progress.StateNodesFailed + th.progress.StorageNodesFailed,
		BytesDownloaded: th.progress.BytesDownloaded,
		PendingPaths:    paths,
		AccountRoots:    acctRoots,
		Timestamp:       time.Now(),
	}
	if err := th.saveCheckpoint(cp); err == nil {
		th.lastCheckpoint = cp
		th.progress.CheckpointsWritten++
	}
	th.nodesSinceCheckpt = 0
}
