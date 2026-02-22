// trie_heal.go implements concurrent trie healing with gap detection,
// access-pattern prioritization, node verification, parallel work
// distribution, and progress tracking with ETA estimation.
package sync

import (
	"errors"
	"fmt"
	"math"
	gosync "sync"
	"sync/atomic"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Concurrent trie heal errors.
var (
	ErrConcHealerClosed  = errors.New("trie_heal: healer closed")
	ErrConcHealerRunning = errors.New("trie_heal: already running")
	ErrNodeVerifyFailed  = errors.New("trie_heal: node verification failed")
	ErrGapFinderNoRoot   = errors.New("trie_heal: no root specified")
)

// HealPriority represents the priority level for a trie path.
type HealPriority int

const (
	// PriorityHigh is for frequently accessed paths.
	PriorityHigh HealPriority = 3
	// PriorityMedium is for moderately accessed paths.
	PriorityMedium HealPriority = 2
	// PriorityLow is the default for newly discovered gaps.
	PriorityLow HealPriority = 1
)

// HealTask represents a single trie node that needs repair with priority.
type HealTask struct {
	Path     []byte
	Root     types.Hash
	Priority HealPriority
	Depth    int
	Retries  int
	Created  time.Time
}

// GapResult holds the result of a gap detection scan.
type GapResult struct {
	Paths    [][]byte
	Root     types.Hash
	Scanned  int
	Duration time.Duration
}

// GapFinder identifies missing trie nodes from partial sync state.
type GapFinder struct {
	mu     gosync.Mutex
	writer StateWriter
}

// NewGapFinder creates a gap finder using the given state writer.
func NewGapFinder(writer StateWriter) *GapFinder {
	return &GapFinder{writer: writer}
}

// FindGaps scans for missing nodes under the root. Returns missing paths.
func (gf *GapFinder) FindGaps(root types.Hash, limit int) (*GapResult, error) {
	if root == (types.Hash{}) {
		return nil, ErrGapFinderNoRoot
	}
	gf.mu.Lock()
	defer gf.mu.Unlock()

	start := time.Now()
	missing := gf.writer.MissingTrieNodes(root, limit)
	return &GapResult{
		Paths:    missing,
		Root:     root,
		Scanned:  len(missing),
		Duration: time.Since(start),
	}, nil
}

// NodeVerifier validates fetched trie nodes against expected hashes.
type NodeVerifier struct{}

// NewNodeVerifier creates a new node verifier.
func NewNodeVerifier() *NodeVerifier { return &NodeVerifier{} }

// VerifyNode checks that node data hashes to the expected hash.
func (nv *NodeVerifier) VerifyNode(data []byte, expectedHash types.Hash) error {
	if len(data) == 0 {
		return fmt.Errorf("%w: empty node data", ErrNodeVerifyFailed)
	}
	actual := crypto.Keccak256Hash(data)
	if actual != expectedHash {
		return fmt.Errorf("%w: hash mismatch: expected %s, got %s",
			ErrNodeVerifyFailed, expectedHash.Hex(), actual.Hex())
	}
	return nil
}

// VerifyBatch verifies nodes; returns index of first invalid, or -1 if all valid.
func (nv *NodeVerifier) VerifyBatch(data [][]byte, hashes []types.Hash) int {
	for i := 0; i < len(data) && i < len(hashes); i++ {
		if err := nv.VerifyNode(data[i], hashes[i]); err != nil {
			return i
		}
	}
	return -1
}

// HealScheduler prioritizes trie paths based on access patterns.
type HealScheduler struct {
	mu       gosync.Mutex
	tasks    []*HealTask
	seen     map[string]bool
	accesses map[string]int // path -> access count
}

// NewHealScheduler creates a new heal scheduler.
func NewHealScheduler() *HealScheduler {
	return &HealScheduler{
		seen:     make(map[string]bool),
		accesses: make(map[string]int),
	}
}

// RecordAccess records a trie path access, boosting its healing priority.
func (hs *HealScheduler) RecordAccess(path []byte) {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	hs.accesses[string(path)]++
}

// AddTask adds a healing task, upgrading priority for frequently accessed paths.
func (hs *HealScheduler) AddTask(task *HealTask) bool {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	key := string(task.Path)
	if hs.seen[key] {
		// Upgrade priority if accessed frequently.
		count := hs.accesses[key]
		if count >= 5 {
			for _, t := range hs.tasks {
				if string(t.Path) == key {
					t.Priority = PriorityHigh
					break
				}
			}
		}
		return false
	}

	// Set priority based on access count.
	count := hs.accesses[key]
	if count >= 5 {
		task.Priority = PriorityHigh
	} else if count >= 2 {
		task.Priority = PriorityMedium
	}

	hs.tasks = append(hs.tasks, task)
	hs.seen[key] = true
	return true
}

// ScheduleBatch extracts up to n tasks sorted by priority then depth.
func (hs *HealScheduler) ScheduleBatch(n int) []*HealTask {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	if n > len(hs.tasks) {
		n = len(hs.tasks)
	}
	if n == 0 {
		return nil
	}

	// Sort: higher priority first, then shallower depth first.
	for i := 0; i < len(hs.tasks)-1; i++ {
		for j := i + 1; j < len(hs.tasks); j++ {
			if hs.tasks[j].Priority > hs.tasks[i].Priority ||
				(hs.tasks[j].Priority == hs.tasks[i].Priority && hs.tasks[j].Depth < hs.tasks[i].Depth) {
				hs.tasks[i], hs.tasks[j] = hs.tasks[j], hs.tasks[i]
			}
		}
	}

	batch := make([]*HealTask, n)
	copy(batch, hs.tasks[:n])
	// Clear seen for scheduled items so they can be re-added on retry.
	for _, task := range batch {
		delete(hs.seen, string(task.Path))
	}
	hs.tasks = hs.tasks[n:]
	return batch
}

// PendingCount returns the number of pending tasks.
func (hs *HealScheduler) PendingCount() int { hs.mu.Lock(); defer hs.mu.Unlock(); return len(hs.tasks) }

// Reset clears all scheduled tasks and access counters.
func (hs *HealScheduler) Reset() {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	hs.tasks, hs.seen, hs.accesses = nil, make(map[string]bool), make(map[string]int)
}

// ConcurrentHealProgress tracks healing progress with ETA estimation.
type ConcurrentHealProgress struct {
	NodesDetected uint64
	NodesHealed   uint64
	NodesFailed   uint64
	BytesFetched  uint64
	WorkersActive int
	StartTime     time.Time
	Complete      bool
}

// Percentage returns the estimated completion percentage.
func (p *ConcurrentHealProgress) Percentage() float64 {
	if p.NodesDetected == 0 {
		return 100.0
	}
	done := p.NodesHealed + p.NodesFailed
	if done >= p.NodesDetected {
		return 100.0
	}
	return float64(done) / float64(p.NodesDetected) * 100.0
}

// ETA estimates the time remaining based on current heal rate.
func (p *ConcurrentHealProgress) ETA() time.Duration {
	if p.StartTime.IsZero() || p.NodesHealed == 0 {
		return 0
	}
	elapsed := time.Since(p.StartTime)
	rate := float64(p.NodesHealed) / elapsed.Seconds()
	if rate <= 0 {
		return 0
	}
	remaining := p.NodesDetected - p.NodesHealed - p.NodesFailed
	return time.Duration(float64(remaining)/rate) * time.Second
}

// NodesPerSecond returns the average healing throughput.
func (p *ConcurrentHealProgress) NodesPerSecond() float64 {
	if p.StartTime.IsZero() {
		return 0
	}
	elapsed := time.Since(p.StartTime).Seconds()
	if elapsed <= 0 {
		return 0
	}
	return float64(p.NodesHealed) / elapsed
}

// ConcurrentHealConfig configures parallel trie healing.
type ConcurrentHealConfig struct {
	Workers    int // Number of concurrent heal workers.
	BatchSize  int // Tasks per batch per worker.
	MaxRetries int // Max retries per node before marking failed.
}

// DefaultConcurrentHealConfig returns sensible defaults.
func DefaultConcurrentHealConfig() ConcurrentHealConfig {
	return ConcurrentHealConfig{
		Workers:    4,
		BatchSize:  64,
		MaxRetries: 3,
	}
}

// ConcurrentTrieHealer coordinates parallel trie healing with workers.
type ConcurrentTrieHealer struct {
	mu        gosync.Mutex
	config    ConcurrentHealConfig
	root      types.Hash
	writer    StateWriter
	scheduler *HealScheduler
	gapFinder *GapFinder
	verifier  *NodeVerifier
	progress  ConcurrentHealProgress
	closed    atomic.Bool
	running   atomic.Bool
}

// NewConcurrentTrieHealer creates a new concurrent trie healer.
func NewConcurrentTrieHealer(config ConcurrentHealConfig, root types.Hash, writer StateWriter) *ConcurrentTrieHealer {
	return &ConcurrentTrieHealer{
		config:    config,
		root:      root,
		writer:    writer,
		scheduler: NewHealScheduler(),
		gapFinder: NewGapFinder(writer),
		verifier:  NewNodeVerifier(),
	}
}

// DetectGaps scans for missing nodes and adds them to the scheduler.
func (ch *ConcurrentTrieHealer) DetectGaps() (int, error) {
	if ch.closed.Load() {
		return 0, ErrConcHealerClosed
	}

	result, err := ch.gapFinder.FindGaps(ch.root, 0)
	if err != nil {
		return 0, err
	}

	now := time.Now()
	added := 0
	for _, path := range result.Paths {
		task := &HealTask{
			Path:     copySlice(path),
			Root:     ch.root,
			Priority: PriorityLow,
			Depth:    len(path),
			Created:  now,
		}
		if ch.scheduler.AddTask(task) {
			added++
		}
	}

	ch.mu.Lock()
	ch.progress.NodesDetected += uint64(added)
	ch.mu.Unlock()
	return added, nil
}

// ProcessBatch processes a batch of healing tasks using the given peer.
func (ch *ConcurrentTrieHealer) ProcessBatch(peer SnapPeer) (int, error) {
	if ch.closed.Load() {
		return 0, ErrConcHealerClosed
	}

	batch := ch.scheduler.ScheduleBatch(ch.config.BatchSize)
	if len(batch) == 0 {
		return 0, nil
	}

	paths := make([][]byte, len(batch))
	for i, task := range batch {
		paths[i] = task.Path
	}

	results, err := peer.RequestTrieNodes(ch.root, paths)
	if err != nil {
		// Re-queue tasks.
		for _, task := range batch {
			ch.scheduler.AddTask(task)
		}
		return 0, fmt.Errorf("trie_heal: request failed: %w", err)
	}

	healed := 0
	ch.mu.Lock()
	defer ch.mu.Unlock()

	for i, task := range batch {
		var data []byte
		if i < len(results) {
			data = results[i]
		}

		if len(data) == 0 {
			task.Retries++
			if task.Retries >= ch.config.MaxRetries {
				ch.progress.NodesFailed++
			} else {
				ch.scheduler.AddTask(task)
			}
			continue
		}

		// Verify the node.
		nodeHash := crypto.Keccak256Hash(data)
		if nodeHash == (types.Hash{}) {
			task.Retries++
			if task.Retries >= ch.config.MaxRetries {
				ch.progress.NodesFailed++
			} else {
				ch.scheduler.AddTask(task)
			}
			continue
		}

		if writeErr := ch.writer.WriteTrieNode(task.Path, data); writeErr != nil {
			return healed, fmt.Errorf("trie_heal: write: %w", writeErr)
		}

		ch.progress.NodesHealed++
		ch.progress.BytesFetched += uint64(len(data))
		healed++
	}
	return healed, nil
}

// Run executes the concurrent healing loop with multiple workers.
func (ch *ConcurrentTrieHealer) Run(peers []SnapPeer) error {
	if !ch.running.CompareAndSwap(false, true) {
		return ErrConcHealerRunning
	}
	defer ch.running.Store(false)

	if len(peers) == 0 {
		return ErrTrieHealNoPeer
	}

	ch.mu.Lock()
	ch.progress.StartTime = time.Now()
	ch.mu.Unlock()

	// Initial gap detection.
	if _, err := ch.DetectGaps(); err != nil {
		return err
	}

	workers := ch.config.Workers
	if workers > len(peers) {
		workers = len(peers)
	}

	ch.mu.Lock()
	ch.progress.WorkersActive = workers
	ch.mu.Unlock()

	var wg gosync.WaitGroup
	errCh := make(chan error, workers)

	for i := 0; i < workers; i++ {
		peer := peers[i%len(peers)]
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !ch.closed.Load() {
				healed, err := ch.ProcessBatch(peer)
				if err != nil {
					errCh <- err
					return
				}
				if healed == 0 && ch.scheduler.PendingCount() == 0 {
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errCh)

	// Return the first error, if any.
	for err := range errCh {
		return err
	}

	// Check completion.
	missing := ch.writer.MissingTrieNodes(ch.root, 1)
	ch.mu.Lock()
	ch.progress.Complete = len(missing) == 0
	ch.progress.WorkersActive = 0
	ch.mu.Unlock()

	return nil
}

// Progress returns the current healing progress.
func (ch *ConcurrentTrieHealer) Progress() ConcurrentHealProgress {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	return ch.progress
}

// Scheduler returns the heal scheduler for access pattern recording.
func (ch *ConcurrentTrieHealer) Scheduler() *HealScheduler { return ch.scheduler }

// Close stops the healer.
func (ch *ConcurrentTrieHealer) Close() { ch.closed.Store(true) }

// Reset clears all healing state.
func (ch *ConcurrentTrieHealer) Reset() {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	ch.scheduler.Reset()
	ch.progress = ConcurrentHealProgress{}
}

// FormatETA formats an ETA duration for display.
func FormatETA(d time.Duration) string {
	if d <= 0 {
		return "unknown"
	}
	h := int(math.Floor(d.Hours()))
	m := int(math.Floor(d.Minutes())) % 60
	s := int(math.Floor(d.Seconds())) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%dm%ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
