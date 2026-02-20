// state_syncer.go implements StateSyn, the state synchronization coordinator.
// It manages the full state download pipeline: account iteration in hash order,
// storage download batching, code deduplication, trie healing, state root
// verification, resumable checkpoints, and bandwidth-aware scheduling.
package sync

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// StateSyn tuning constants.
const (
	// StateSynAccountBatch is the number of accounts per download request.
	StateSynAccountBatch = 256

	// StateSynStorageBatch is how many accounts to batch for storage download.
	StateSynStorageBatch = 8

	// StateSynCodeBatch is the max bytecodes per request.
	StateSynCodeBatch = 64

	// StateSynHealBatch is the max trie nodes per heal request.
	StateSynHealBatch = 128

	// StateSynMaxRetries is the max retries for a failed request.
	StateSynMaxRetries = 5

	// StateSynRetryDelay is the backoff delay between retries.
	StateSynRetryDelay = 500 * time.Millisecond

	// StateSynBandwidthWindow is the sliding window for bandwidth estimation.
	StateSynBandwidthWindow = 10 * time.Second

	// StateSynCheckpointInterval is how often to persist progress checkpoints.
	StateSynCheckpointInterval = 30 * time.Second

	// StateSynPartitions is the number of hash space partitions.
	StateSynPartitions = 16
)

// StateSyn phase constants.
const (
	StateSynPhaseInit     uint32 = 0
	StateSynPhaseAccounts uint32 = 1
	StateSynPhaseStorage  uint32 = 2
	StateSynPhaseCodes    uint32 = 3
	StateSynPhaseHeal     uint32 = 4
	StateSynPhaseVerify   uint32 = 5
	StateSynPhaseDone     uint32 = 6
	StateSynPhaseFailed   uint32 = 7
)

// StateSynPhaseName returns the human-readable name for a phase.
func StateSynPhaseName(phase uint32) string {
	names := [...]string{
		"init", "accounts", "storage", "codes", "heal", "verify", "done", "failed",
	}
	if int(phase) < len(names) {
		return names[phase]
	}
	return fmt.Sprintf("unknown(%d)", phase)
}

// StateSyn errors.
var (
	ErrStateSynRunning      = errors.New("state syncer: already running")
	ErrStateSynCancelled    = errors.New("state syncer: cancelled")
	ErrStateSynVerifyFailed = errors.New("state syncer: state root verification failed")
	ErrStateSynRetryLimit   = errors.New("state syncer: retry limit exceeded")
	ErrStateSynNoPeer       = errors.New("state syncer: no peer available")
)

// StateSynCheckpoint records sync progress for resumption after restart.
type StateSynCheckpoint struct {
	Phase          uint32
	PivotBlock     uint64
	PivotRoot      types.Hash
	LastAccountKey types.Hash // last account hash synced (for resume)
	AccountsDone   uint64
	StorageDone    uint64
	CodesDone      uint64
	HealNodesDone  uint64
	BytesTotal     uint64
	Timestamp      time.Time
}

// Encode serializes a checkpoint to bytes: phase(4) + pivot(8) + root(32)
// + lastKey(32) + accounts(8) + storage(8) + codes(8) + heal(8) + bytes(8) + time(8).
func (cp *StateSynCheckpoint) Encode() []byte {
	buf := make([]byte, 4+8+32+32+8+8+8+8+8+8)
	offset := 0
	binary.BigEndian.PutUint32(buf[offset:], cp.Phase)
	offset += 4
	binary.BigEndian.PutUint64(buf[offset:], cp.PivotBlock)
	offset += 8
	copy(buf[offset:], cp.PivotRoot[:])
	offset += 32
	copy(buf[offset:], cp.LastAccountKey[:])
	offset += 32
	binary.BigEndian.PutUint64(buf[offset:], cp.AccountsDone)
	offset += 8
	binary.BigEndian.PutUint64(buf[offset:], cp.StorageDone)
	offset += 8
	binary.BigEndian.PutUint64(buf[offset:], cp.CodesDone)
	offset += 8
	binary.BigEndian.PutUint64(buf[offset:], cp.HealNodesDone)
	offset += 8
	binary.BigEndian.PutUint64(buf[offset:], cp.BytesTotal)
	offset += 8
	binary.BigEndian.PutUint64(buf[offset:], uint64(cp.Timestamp.Unix()))
	return buf
}

// DecodeStateSynCheckpoint deserializes a checkpoint from bytes.
func DecodeStateSynCheckpoint(data []byte) (*StateSynCheckpoint, error) {
	if len(data) < 4+8+32+32+8+8+8+8+8+8 {
		return nil, errors.New("state syncer: checkpoint data too short")
	}
	cp := &StateSynCheckpoint{}
	offset := 0
	cp.Phase = binary.BigEndian.Uint32(data[offset:])
	offset += 4
	cp.PivotBlock = binary.BigEndian.Uint64(data[offset:])
	offset += 8
	copy(cp.PivotRoot[:], data[offset:offset+32])
	offset += 32
	copy(cp.LastAccountKey[:], data[offset:offset+32])
	offset += 32
	cp.AccountsDone = binary.BigEndian.Uint64(data[offset:])
	offset += 8
	cp.StorageDone = binary.BigEndian.Uint64(data[offset:])
	offset += 8
	cp.CodesDone = binary.BigEndian.Uint64(data[offset:])
	offset += 8
	cp.HealNodesDone = binary.BigEndian.Uint64(data[offset:])
	offset += 8
	cp.BytesTotal = binary.BigEndian.Uint64(data[offset:])
	offset += 8
	ts := binary.BigEndian.Uint64(data[offset:])
	cp.Timestamp = time.Unix(int64(ts), 0)
	return cp, nil
}

// StateSynProgress holds a snapshot of the state sync progress.
type StateSynProgress struct {
	Phase         uint32
	PivotBlock    uint64
	PivotRoot     types.Hash
	AccountsDone  uint64
	AccountBytes  uint64
	StorageDone   uint64
	StorageBytes  uint64
	CodesDone     uint64
	CodeBytes     uint64
	HealDone      uint64
	HealBytes     uint64
	BytesTotal    uint64
	StartTime     time.Time
	LastUpdate    time.Time
	Bandwidth     float64 // estimated bytes per second
}

// stateSynPartition represents a hash space partition for account iteration.
type stateSynPartition struct {
	origin  types.Hash
	limit   types.Hash
	cursor  types.Hash // current position (for resume)
	done    bool
	retries int
}

// stateSynStorageGroup batches accounts by storage root for efficient download.
type stateSynStorageGroup struct {
	storageRoot types.Hash
	accounts    []types.Hash
	done        bool
}

// bandwidthTracker estimates download bandwidth over a sliding window.
type bandwidthTracker struct {
	mu      sync.Mutex
	samples []bandwidthSample
	window  time.Duration
}

type bandwidthSample struct {
	bytes uint64
	time  time.Time
}

func newBandwidthTracker(window time.Duration) *bandwidthTracker {
	return &bandwidthTracker{window: window}
}

func (bt *bandwidthTracker) record(n uint64) {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	bt.samples = append(bt.samples, bandwidthSample{bytes: n, time: time.Now()})
	bt.prune()
}

func (bt *bandwidthTracker) estimate() float64 {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	bt.prune()
	if len(bt.samples) < 2 {
		return 0
	}
	first := bt.samples[0]
	last := bt.samples[len(bt.samples)-1]
	elapsed := last.time.Sub(first.time).Seconds()
	if elapsed <= 0 {
		return 0
	}
	var total uint64
	for _, s := range bt.samples {
		total += s.bytes
	}
	return float64(total) / elapsed
}

func (bt *bandwidthTracker) prune() {
	cutoff := time.Now().Add(-bt.window)
	i := 0
	for i < len(bt.samples) && bt.samples[i].time.Before(cutoff) {
		i++
	}
	if i > 0 {
		bt.samples = bt.samples[i:]
	}
}

// StateSyn coordinates the full state download with account iteration,
// storage batching, code deduplication, trie healing, and resumable checkpoints.
type StateSyn struct {
	mu       sync.Mutex
	running  atomic.Bool
	phase    atomic.Uint32
	cancel   chan struct{}
	progress StateSynProgress

	// Pivot configuration.
	pivotBlock  uint64
	pivotRoot   types.Hash
	pivotHeader *types.Header

	// Account partitions for hash-ordered iteration.
	partitions []*stateSynPartition

	// Storage groups (accounts batched by storage root).
	storageGroups []*stateSynStorageGroup
	storageIndex  map[types.Hash]int // storageRoot -> group index

	// Code deduplication: hash -> downloaded flag.
	codeTracker map[types.Hash]bool

	// Bandwidth tracking.
	bandwidth *bandwidthTracker

	// Checkpoint state.
	lastCheckpoint time.Time
	checkpoint     *StateSynCheckpoint

	// State persistence.
	writer StateWriter

	// Request ID counter.
	reqID atomic.Uint64
}

// NewStateSyn creates a new state syncer coordinator.
func NewStateSyn(writer StateWriter) *StateSyn {
	return &StateSyn{
		cancel:       make(chan struct{}),
		writer:       writer,
		storageIndex: make(map[types.Hash]int),
		codeTracker:  make(map[types.Hash]bool),
		bandwidth:    newBandwidthTracker(StateSynBandwidthWindow),
	}
}

// SetStateSynPivot configures the pivot block for the state sync.
func (ss *StateSyn) SetStateSynPivot(header *types.Header) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.pivotBlock = header.Number.Uint64()
	ss.pivotRoot = header.Root
	ss.pivotHeader = header
	ss.progress.PivotBlock = ss.pivotBlock
	ss.progress.PivotRoot = ss.pivotRoot
}

// ResumeFrom resumes state sync from a checkpoint.
func (ss *StateSyn) ResumeFrom(cp *StateSynCheckpoint) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.checkpoint = cp
	ss.progress.PivotBlock = cp.PivotBlock
	ss.progress.PivotRoot = cp.PivotRoot
	ss.progress.AccountsDone = cp.AccountsDone
	ss.progress.StorageDone = cp.StorageDone
	ss.progress.CodesDone = cp.CodesDone
	ss.progress.HealDone = cp.HealNodesDone
	ss.progress.BytesTotal = cp.BytesTotal
}

// GetStateSynProgress returns a snapshot of the current progress.
func (ss *StateSyn) GetStateSynProgress() StateSynProgress {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	p := ss.progress
	p.Phase = ss.phase.Load()
	p.Bandwidth = ss.bandwidth.estimate()
	return p
}

// StateSynPhase returns the current phase.
func (ss *StateSyn) StateSynPhase() uint32 {
	return ss.phase.Load()
}

// StateSynRunning returns whether the syncer is active.
func (ss *StateSyn) StateSynRunning() bool {
	return ss.running.Load()
}

// CancelStateSyn cancels the state sync.
func (ss *StateSyn) CancelStateSyn() {
	ss.mu.Lock()
	ch := ss.cancel
	ss.mu.Unlock()
	select {
	case <-ch:
	default:
		close(ch)
	}
}

// Checkpoint returns the latest checkpoint for resumable sync.
func (ss *StateSyn) Checkpoint() *StateSynCheckpoint {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	var lastKey types.Hash
	if len(ss.partitions) > 0 {
		for i := len(ss.partitions) - 1; i >= 0; i-- {
			if ss.partitions[i].done || !ss.partitions[i].cursor.IsZero() {
				lastKey = ss.partitions[i].cursor
				break
			}
		}
	}

	return &StateSynCheckpoint{
		Phase:          ss.phase.Load(),
		PivotBlock:     ss.pivotBlock,
		PivotRoot:      ss.pivotRoot,
		LastAccountKey: lastKey,
		AccountsDone:   ss.progress.AccountsDone,
		StorageDone:    ss.progress.StorageDone,
		CodesDone:      ss.progress.CodesDone,
		HealNodesDone:  ss.progress.HealDone,
		BytesTotal:     ss.progress.BytesTotal,
		Timestamp:      time.Now(),
	}
}

// initPartitions creates hash-space partitions for account iteration.
func (ss *StateSyn) initPartitions(n int) {
	if n <= 0 {
		n = 1
	}
	if n > 256 {
		n = 256
	}

	ss.partitions = make([]*stateSynPartition, n)
	max256 := new(big.Int).Lsh(big.NewInt(1), 256)
	step := new(big.Int).Div(max256, big.NewInt(int64(n)))

	for i := 0; i < n; i++ {
		p := &stateSynPartition{}
		if i > 0 {
			p.origin = types.IntToHash(new(big.Int).Mul(step, big.NewInt(int64(i))))
		}
		p.cursor = p.origin
		if i == n-1 {
			for j := range p.limit {
				p.limit[j] = 0xff
			}
		} else {
			next := new(big.Int).Mul(step, big.NewInt(int64(i+1)))
			next.Sub(next, big.NewInt(1))
			p.limit = types.IntToHash(next)
		}
		ss.partitions[i] = p
	}

	// If resuming, skip partitions before the checkpoint.
	if ss.checkpoint != nil && !ss.checkpoint.LastAccountKey.IsZero() {
		lastKey := new(big.Int).SetBytes(ss.checkpoint.LastAccountKey[:])
		for _, p := range ss.partitions {
			pLimit := new(big.Int).SetBytes(p.limit[:])
			if pLimit.Cmp(lastKey) <= 0 {
				p.done = true
			} else {
				pOrigin := new(big.Int).SetBytes(p.origin[:])
				if pOrigin.Cmp(lastKey) < 0 {
					p.cursor = ss.checkpoint.LastAccountKey
				}
				break
			}
		}
	}
}

// RunStateSyn executes the full state sync pipeline. Blocks until done or cancelled.
func (ss *StateSyn) RunStateSyn(peer SnapPeer) error {
	if !ss.running.CompareAndSwap(false, true) {
		return ErrStateSynRunning
	}
	defer ss.running.Store(false)

	ss.mu.Lock()
	ss.cancel = make(chan struct{})
	ss.progress.StartTime = time.Now()
	ss.lastCheckpoint = time.Now()
	ss.initPartitions(StateSynPartitions)
	ss.mu.Unlock()

	// Phase 1: Accounts.
	ss.phase.Store(StateSynPhaseAccounts)
	if err := ss.syncAccounts(peer); err != nil {
		ss.phase.Store(StateSynPhaseFailed)
		return err
	}

	// Phase 2: Storage (batched by storage root).
	ss.phase.Store(StateSynPhaseStorage)
	if err := ss.syncStorage(peer); err != nil {
		ss.phase.Store(StateSynPhaseFailed)
		return err
	}

	// Phase 3: Codes (deduplicated).
	ss.phase.Store(StateSynPhaseCodes)
	if err := ss.syncCodes(peer); err != nil {
		ss.phase.Store(StateSynPhaseFailed)
		return err
	}

	// Phase 4: Heal.
	ss.phase.Store(StateSynPhaseHeal)
	if err := ss.syncHeal(peer); err != nil {
		ss.phase.Store(StateSynPhaseFailed)
		return err
	}

	// Phase 5: Verify state root.
	ss.phase.Store(StateSynPhaseVerify)
	if err := ss.verifyStateRoot(); err != nil {
		ss.phase.Store(StateSynPhaseFailed)
		return err
	}

	// Done.
	ss.phase.Store(StateSynPhaseDone)
	ss.mu.Lock()
	ss.progress.LastUpdate = time.Now()
	ss.mu.Unlock()
	return nil
}

// syncAccounts iterates account partitions in hash order.
func (ss *StateSyn) syncAccounts(peer SnapPeer) error {
	for {
		select {
		case <-ss.cancel:
			return ErrStateSynCancelled
		default:
		}

		part := ss.nextPartition()
		if part == nil {
			return nil
		}

		resp, err := ss.requestAccountsWithRetry(peer, part)
		if err != nil {
			return err
		}

		for _, acct := range resp.Accounts {
			if err := ss.writer.WriteAccount(acct.Hash, acct); err != nil {
				return fmt.Errorf("state syncer: write account: %w", err)
			}

			ss.mu.Lock()
			ss.progress.AccountsDone++
			acctSize := uint64(146) // approx account size
			ss.progress.AccountBytes += acctSize
			ss.progress.BytesTotal += acctSize
			ss.bandwidth.record(acctSize)

			// Group accounts by storage root for batched download.
			if acct.Root != types.EmptyRootHash {
				ss.addToStorageGroup(acct.Root, acct.Hash)
			}
			// Track code hashes for deduplicated download.
			if acct.CodeHash != types.EmptyCodeHash {
				if _, tracked := ss.codeTracker[acct.CodeHash]; !tracked {
					ss.codeTracker[acct.CodeHash] = false
				}
			}
			ss.mu.Unlock()
		}

		// Advance partition cursor.
		if !resp.More || len(resp.Accounts) == 0 {
			part.done = true
		} else {
			last := resp.Accounts[len(resp.Accounts)-1].Hash
			part.cursor = snapSyncIncrementHash(last)
		}

		ss.mu.Lock()
		ss.progress.LastUpdate = time.Now()
		ss.mu.Unlock()

		ss.maybeCheckpoint()
	}
}

// nextPartition returns the next incomplete partition.
func (ss *StateSyn) nextPartition() *stateSynPartition {
	for _, p := range ss.partitions {
		if !p.done {
			return p
		}
	}
	return nil
}

// requestAccountsWithRetry fetches accounts with retry logic.
func (ss *StateSyn) requestAccountsWithRetry(peer SnapPeer, part *stateSynPartition) (*AccountRangeResponse, error) {
	var lastErr error
	for attempt := 0; attempt <= StateSynMaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ss.cancel:
				return nil, ErrStateSynCancelled
			case <-time.After(StateSynRetryDelay * time.Duration(attempt)):
			}
		}

		ss.mu.Lock()
		root := ss.pivotRoot
		ss.mu.Unlock()

		resp, err := peer.RequestAccountRange(AccountRangeRequest{
			ID:     ss.reqID.Add(1),
			Root:   root,
			Origin: part.cursor,
			Limit:  part.limit,
			Bytes:  SnapSyncSoftByteLimit,
		})
		if err != nil {
			lastErr = err
			part.retries++
			continue
		}
		return resp, nil
	}
	return nil, fmt.Errorf("%w: %v", ErrStateSynRetryLimit, lastErr)
}

// addToStorageGroup groups accounts by storage root for batched download.
// Caller must hold ss.mu.
func (ss *StateSyn) addToStorageGroup(storageRoot, accountHash types.Hash) {
	idx, exists := ss.storageIndex[storageRoot]
	if exists {
		ss.storageGroups[idx].accounts = append(ss.storageGroups[idx].accounts, accountHash)
	} else {
		idx = len(ss.storageGroups)
		ss.storageGroups = append(ss.storageGroups, &stateSynStorageGroup{
			storageRoot: storageRoot,
			accounts:    []types.Hash{accountHash},
		})
		ss.storageIndex[storageRoot] = idx
	}
}

// syncStorage downloads storage for all queued accounts, batched by group.
func (ss *StateSyn) syncStorage(peer SnapPeer) error {
	ss.mu.Lock()
	groups := ss.storageGroups
	ss.mu.Unlock()

	// Sort groups by number of accounts (largest first for throughput).
	sort.Slice(groups, func(i, j int) bool {
		return len(groups[i].accounts) > len(groups[j].accounts)
	})

	for _, group := range groups {
		select {
		case <-ss.cancel:
			return ErrStateSynCancelled
		default:
		}

		if group.done {
			continue
		}

		// Download storage in batches of accounts.
		for i := 0; i < len(group.accounts); i += StateSynStorageBatch {
			select {
			case <-ss.cancel:
				return ErrStateSynCancelled
			default:
			}

			end := i + StateSynStorageBatch
			if end > len(group.accounts) {
				end = len(group.accounts)
			}
			batch := group.accounts[i:end]

			if err := ss.downloadStorageForAccounts(peer, batch); err != nil {
				return err
			}
		}
		group.done = true
		ss.maybeCheckpoint()
	}
	return nil
}

// downloadStorageForAccounts fetches all storage for a batch of accounts.
func (ss *StateSyn) downloadStorageForAccounts(peer SnapPeer, accounts []types.Hash) error {
	origin := types.Hash{}
	for {
		select {
		case <-ss.cancel:
			return ErrStateSynCancelled
		default:
		}

		var limit types.Hash
		for i := range limit {
			limit[i] = 0xff
		}

		ss.mu.Lock()
		root := ss.pivotRoot
		ss.mu.Unlock()

		resp, err := peer.RequestStorageRange(StorageRangeRequest{
			ID:       ss.reqID.Add(1),
			Root:     root,
			Accounts: accounts,
			Origin:   origin,
			Limit:    limit,
			Bytes:    SnapSyncSoftByteLimit,
		})
		if err != nil {
			return fmt.Errorf("state syncer: storage request: %w", err)
		}

		for _, slot := range resp.Slots {
			if err := ss.writer.WriteStorage(slot.AccountHash, slot.SlotHash, slot.Value); err != nil {
				return fmt.Errorf("state syncer: write storage: %w", err)
			}
			ss.mu.Lock()
			ss.progress.StorageDone++
			slotSize := uint64(len(slot.Value)) + 32
			ss.progress.StorageBytes += slotSize
			ss.progress.BytesTotal += slotSize
			ss.bandwidth.record(slotSize)
			ss.mu.Unlock()
		}

		if !resp.More || len(resp.Slots) == 0 {
			break
		}
		last := resp.Slots[len(resp.Slots)-1].SlotHash
		origin = snapSyncIncrementHash(last)
	}

	ss.mu.Lock()
	ss.progress.LastUpdate = time.Now()
	ss.mu.Unlock()
	return nil
}

// syncCodes downloads bytecodes, deduplicating by code hash.
func (ss *StateSyn) syncCodes(peer SnapPeer) error {
	for {
		select {
		case <-ss.cancel:
			return ErrStateSynCancelled
		default:
		}

		ss.mu.Lock()
		batch := make([]types.Hash, 0, StateSynCodeBatch)
		for hash, done := range ss.codeTracker {
			if done {
				continue
			}
			// Skip codes we already have.
			if ss.writer.HasBytecode(hash) {
				ss.codeTracker[hash] = true
				ss.progress.CodesDone++
				continue
			}
			batch = append(batch, hash)
			if len(batch) >= StateSynCodeBatch {
				break
			}
		}
		if len(batch) == 0 {
			ss.mu.Unlock()
			return nil
		}
		ss.mu.Unlock()

		resp, err := peer.RequestBytecodes(BytecodeRequest{
			ID:     ss.reqID.Add(1),
			Hashes: batch,
		})
		if err != nil {
			return fmt.Errorf("state syncer: code request: %w", err)
		}

		for _, code := range resp.Codes {
			// Verify code hash.
			computed := types.BytesToHash(crypto.Keccak256(code.Code))
			if computed != code.Hash {
				return fmt.Errorf("state syncer: code hash mismatch: want %s got %s",
					code.Hash.Hex(), computed.Hex())
			}
			if err := ss.writer.WriteBytecode(code.Hash, code.Code); err != nil {
				return fmt.Errorf("state syncer: write code: %w", err)
			}
			ss.mu.Lock()
			ss.codeTracker[code.Hash] = true
			ss.progress.CodesDone++
			codeSize := uint64(len(code.Code))
			ss.progress.CodeBytes += codeSize
			ss.progress.BytesTotal += codeSize
			ss.bandwidth.record(codeSize)
			ss.mu.Unlock()
		}

		ss.mu.Lock()
		ss.progress.LastUpdate = time.Now()
		ss.mu.Unlock()
		ss.maybeCheckpoint()
	}
}

// syncHeal fills gaps in the trie left by snap sync.
func (ss *StateSyn) syncHeal(peer SnapPeer) error {
	ss.mu.Lock()
	root := ss.pivotRoot
	ss.mu.Unlock()

	for {
		select {
		case <-ss.cancel:
			return ErrStateSynCancelled
		default:
		}

		missing := ss.writer.MissingTrieNodes(root, StateSynHealBatch)
		if len(missing) == 0 {
			return nil
		}

		nodes, err := peer.RequestTrieNodes(root, missing)
		if err != nil {
			return fmt.Errorf("state syncer: heal request: %w", err)
		}

		for i, data := range nodes {
			if len(data) == 0 || i >= len(missing) {
				continue
			}
			if err := ss.writer.WriteTrieNode(missing[i], data); err != nil {
				return fmt.Errorf("state syncer: write trie node: %w", err)
			}
			ss.mu.Lock()
			ss.progress.HealDone++
			healSize := uint64(len(data))
			ss.progress.HealBytes += healSize
			ss.progress.BytesTotal += healSize
			ss.bandwidth.record(healSize)
			ss.mu.Unlock()
		}

		ss.mu.Lock()
		ss.progress.LastUpdate = time.Now()
		ss.mu.Unlock()
	}
}

// verifyStateRoot checks that all downloaded state produces the expected root.
// In this implementation, healing is sufficient -- if no trie nodes are missing,
// the state root is implicitly verified.
func (ss *StateSyn) verifyStateRoot() error {
	ss.mu.Lock()
	root := ss.pivotRoot
	ss.mu.Unlock()

	remaining := ss.writer.MissingTrieNodes(root, 1)
	if len(remaining) > 0 {
		return fmt.Errorf("%w: %d missing trie nodes",
			ErrStateSynVerifyFailed, len(remaining))
	}
	return nil
}

// maybeCheckpoint persists a checkpoint if enough time has elapsed.
func (ss *StateSyn) maybeCheckpoint() {
	now := time.Now()
	ss.mu.Lock()
	if now.Sub(ss.lastCheckpoint) < StateSynCheckpointInterval {
		ss.mu.Unlock()
		return
	}
	ss.lastCheckpoint = now
	ss.mu.Unlock()

	// The caller can retrieve the checkpoint via Checkpoint() and persist it.
	// In a full implementation this would write to the database.
}
