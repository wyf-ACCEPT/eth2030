// block_processor.go implements a block processing pipeline that accepts
// downloaded blocks, orders them by number, validates parent hash linkage,
// uncle headers, receipt roots, and state roots after execution, then
// imports them into the chain. Import metrics are tracked throughout.
package sync

import (
	"errors"
	"fmt"
	gosync "sync"
	"sync/atomic"
	"time"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/metrics"
)

// Block processor errors.
var (
	ErrProcessorClosed    = errors.New("block processor: closed")
	ErrDuplicateBlock     = errors.New("block processor: duplicate block in queue")
	ErrQueueFull          = errors.New("block processor: queue is full")
	ErrMissingParent      = errors.New("block processor: missing parent block")
	ErrBadUncleCount      = errors.New("block processor: too many uncles")
	ErrDuplicateUncle     = errors.New("block processor: duplicate uncle")
	ErrUncleIsAncestor    = errors.New("block processor: uncle is a direct ancestor")
	ErrBadReceiptRoot     = errors.New("block processor: receipt root mismatch")
	ErrStateRootMismatch  = errors.New("block processor: state root mismatch")
	ErrBadUncleParentHash = errors.New("block processor: uncle parent hash unknown")
)

// Maximum number of uncles allowed per block (Ethereum mainnet).
const maxUncles = 2

// Maximum uncle depth (how far back an uncle's parent can be).
const maxUncleDepth = 7

// BlockProcessorConfig configures the block processing pipeline.
type BlockProcessorConfig struct {
	MaxQueueSize   int  // Maximum blocks queued before processing.
	BatchSize      int  // Blocks per processing batch.
	VerifyReceipts bool // Whether to verify receipt roots.
	VerifyState    bool // Whether to verify state roots after execution.
	VerifyUncles   bool // Whether to validate uncle headers.
}

// DefaultBlockProcessorConfig returns sensible defaults.
func DefaultBlockProcessorConfig() BlockProcessorConfig {
	return BlockProcessorConfig{
		MaxQueueSize:   4096,
		BatchSize:      64,
		VerifyReceipts: true,
		VerifyState:    true,
		VerifyUncles:   true,
	}
}

// BlockProcessorMetrics tracks import statistics.
type BlockProcessorMetrics struct {
	BlocksQueued    *metrics.Counter
	BlocksProcessed *metrics.Counter
	BlocksFailed    *metrics.Counter
	UnclesVerified  *metrics.Counter
	ReceiptsChecked *metrics.Counter
	StateChecked    *metrics.Counter
	ImportTime      *metrics.Histogram
	QueueSize       *metrics.Gauge
}

// NewBlockProcessorMetrics creates a new metrics set.
func NewBlockProcessorMetrics() *BlockProcessorMetrics {
	return &BlockProcessorMetrics{
		BlocksQueued:    metrics.NewCounter("sync.block_processor.blocks_queued"),
		BlocksProcessed: metrics.NewCounter("sync.block_processor.blocks_processed"),
		BlocksFailed:    metrics.NewCounter("sync.block_processor.blocks_failed"),
		UnclesVerified:  metrics.NewCounter("sync.block_processor.uncles_verified"),
		ReceiptsChecked: metrics.NewCounter("sync.block_processor.receipts_checked"),
		StateChecked:    metrics.NewCounter("sync.block_processor.state_checked"),
		ImportTime:      metrics.NewHistogram("sync.block_processor.import_ms"),
		QueueSize:       metrics.NewGauge("sync.block_processor.queue_size"),
	}
}

// ReceiptHasher computes the receipt root from a set of receipts.
type ReceiptHasher interface {
	ComputeReceiptRoot(receipts []*types.Receipt) types.Hash
}

// StateExecutor executes a block against a state and returns the resulting
// state root. This is called after basic validation to verify the state root.
type StateExecutor interface {
	ExecuteBlock(block *types.Block) (stateRoot types.Hash, receipts []*types.Receipt, err error)
}

// AncestorLookup retrieves headers from the local chain for uncle validation.
type AncestorLookup interface {
	GetHeader(hash types.Hash) *types.Header
	GetBlock(hash types.Hash) *types.Block
}

// queuedBlock wraps a block with its arrival time for ordering.
type queuedBlock struct {
	block    *types.Block
	receipts []*types.Receipt // optional pre-downloaded receipts
	arrived  time.Time
}

// BlockProcessor manages the block validation and import pipeline.
type BlockProcessor struct {
	config  BlockProcessorConfig
	metrics *BlockProcessorMetrics

	mu    gosync.Mutex
	queue map[uint64]*queuedBlock // blocks indexed by number
	seen  map[types.Hash]bool     // tracks already-queued block hashes

	inserter  BlockInserter
	executor  StateExecutor
	ancestors AncestorLookup
	hasher    ReceiptHasher

	nextExpected uint64       // next block number we expect to process
	closed       atomic.Bool
}

// NewBlockProcessor creates a new block processor with the given config.
func NewBlockProcessor(config BlockProcessorConfig, inserter BlockInserter) *BlockProcessor {
	if config.MaxQueueSize <= 0 {
		config.MaxQueueSize = 4096
	}
	if config.BatchSize <= 0 {
		config.BatchSize = 64
	}
	return &BlockProcessor{
		config:  config,
		metrics: NewBlockProcessorMetrics(),
		queue:   make(map[uint64]*queuedBlock),
		seen:    make(map[types.Hash]bool),
		inserter: inserter,
	}
}

// SetExecutor sets the state executor for state root verification.
func (bp *BlockProcessor) SetExecutor(exec StateExecutor) {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	bp.executor = exec
}

// SetAncestorLookup sets the ancestor lookup for uncle validation.
func (bp *BlockProcessor) SetAncestorLookup(al AncestorLookup) {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	bp.ancestors = al
}

// SetReceiptHasher sets the receipt hasher for receipt root verification.
func (bp *BlockProcessor) SetReceiptHasher(rh ReceiptHasher) {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	bp.hasher = rh
}

// SetNextExpected sets the next expected block number. This should be
// called once before enqueuing blocks, typically set to current head + 1.
func (bp *BlockProcessor) SetNextExpected(num uint64) {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	bp.nextExpected = num
}

// Enqueue adds a block to the processing queue. Returns an error if the
// queue is full or the block is a duplicate.
func (bp *BlockProcessor) Enqueue(block *types.Block, receipts []*types.Receipt) error {
	if bp.closed.Load() {
		return ErrProcessorClosed
	}

	bp.mu.Lock()
	defer bp.mu.Unlock()

	hash := block.Hash()
	if bp.seen[hash] {
		return ErrDuplicateBlock
	}
	if len(bp.queue) >= bp.config.MaxQueueSize {
		return ErrQueueFull
	}

	num := block.NumberU64()
	bp.queue[num] = &queuedBlock{
		block:    block,
		receipts: receipts,
		arrived:  time.Now(),
	}
	bp.seen[hash] = true
	bp.metrics.BlocksQueued.Inc()
	bp.metrics.QueueSize.Set(int64(len(bp.queue)))
	return nil
}

// ProcessReady processes all contiguous blocks starting from nextExpected.
// Returns the number of blocks successfully imported.
func (bp *BlockProcessor) ProcessReady() (int, error) {
	if bp.closed.Load() {
		return 0, ErrProcessorClosed
	}

	bp.mu.Lock()
	// Collect a contiguous batch starting from nextExpected.
	var batch []*queuedBlock
	num := bp.nextExpected
	for i := 0; i < bp.config.BatchSize; i++ {
		qb, ok := bp.queue[num]
		if !ok {
			break
		}
		batch = append(batch, qb)
		num++
	}
	bp.mu.Unlock()

	if len(batch) == 0 {
		return 0, nil
	}

	imported := 0
	for _, qb := range batch {
		start := time.Now()

		if err := bp.validateBlock(qb); err != nil {
			bp.metrics.BlocksFailed.Inc()
			return imported, fmt.Errorf("block %d: %w", qb.block.NumberU64(), err)
		}

		// Insert into chain via the configured inserter.
		if _, err := bp.inserter.InsertChain([]*types.Block{qb.block}); err != nil {
			bp.metrics.BlocksFailed.Inc()
			return imported, fmt.Errorf("insert block %d: %w", qb.block.NumberU64(), err)
		}

		elapsed := time.Since(start)
		bp.metrics.ImportTime.Observe(float64(elapsed.Milliseconds()))
		bp.metrics.BlocksProcessed.Inc()
		imported++

		// Advance expected and clean up queue.
		bp.mu.Lock()
		blockNum := qb.block.NumberU64()
		delete(bp.queue, blockNum)
		bp.nextExpected = blockNum + 1
		bp.metrics.QueueSize.Set(int64(len(bp.queue)))
		bp.mu.Unlock()
	}

	return imported, nil
}

// validateBlock runs all configured validation checks on a queued block.
func (bp *BlockProcessor) validateBlock(qb *queuedBlock) error {
	block := qb.block

	// 1. Parent hash verification: the block's parent hash must match
	//    the hash of the previous block in the chain.
	if err := bp.verifyParentHash(block); err != nil {
		return err
	}

	// 2. Uncle validation.
	if bp.config.VerifyUncles && len(block.Uncles()) > 0 {
		if err := bp.verifyUncles(block); err != nil {
			return err
		}
	}

	// 3. State execution and state root verification.
	if bp.config.VerifyState && bp.executor != nil {
		stateRoot, receipts, err := bp.executor.ExecuteBlock(block)
		if err != nil {
			return fmt.Errorf("execute: %w", err)
		}
		if stateRoot != block.Root() {
			return fmt.Errorf("%w: got %s, want %s",
				ErrStateRootMismatch, stateRoot.Hex(), block.Root().Hex())
		}
		bp.metrics.StateChecked.Inc()

		// If we got receipts from execution, verify receipt root too.
		if bp.config.VerifyReceipts && bp.hasher != nil && receipts != nil {
			receiptRoot := bp.hasher.ComputeReceiptRoot(receipts)
			if receiptRoot != block.ReceiptHash() {
				return fmt.Errorf("%w: got %s, want %s",
					ErrBadReceiptRoot, receiptRoot.Hex(), block.ReceiptHash().Hex())
			}
			bp.metrics.ReceiptsChecked.Inc()
		}
	} else if bp.config.VerifyReceipts && bp.hasher != nil && qb.receipts != nil {
		// Verify pre-downloaded receipts against the block's receipt hash.
		receiptRoot := bp.hasher.ComputeReceiptRoot(qb.receipts)
		if receiptRoot != block.ReceiptHash() {
			return fmt.Errorf("%w: got %s, want %s",
				ErrBadReceiptRoot, receiptRoot.Hex(), block.ReceiptHash().Hex())
		}
		bp.metrics.ReceiptsChecked.Inc()
	}

	return nil
}

// verifyParentHash checks that the block's parent hash matches the current
// chain head or a previously processed block.
func (bp *BlockProcessor) verifyParentHash(block *types.Block) error {
	if bp.inserter == nil {
		return nil
	}
	current := bp.inserter.CurrentBlock()
	if current == nil {
		return nil
	}
	// For the first block in a batch, its parent should be the current head.
	// For subsequent blocks, the parent is the previous block in the batch.
	if block.ParentHash() != current.Hash() {
		// Check if the parent is in our queue (sequential batch).
		bp.mu.Lock()
		parentNum := block.NumberU64() - 1
		qb, ok := bp.queue[parentNum]
		bp.mu.Unlock()
		if ok && block.ParentHash() == qb.block.Hash() {
			return nil
		}
		return fmt.Errorf("%w: block %d parent %s, head %s",
			ErrMissingParent, block.NumberU64(),
			block.ParentHash().Hex(), current.Hash().Hex())
	}
	return nil
}

// verifyUncles validates uncle headers in a block. Checks:
// - Uncle count does not exceed maxUncles (2).
// - No duplicate uncles within the block.
// - Uncle is not a direct ancestor of the block.
// - Uncle's parent exists in the recent chain (within maxUncleDepth).
func (bp *BlockProcessor) verifyUncles(block *types.Block) error {
	uncles := block.Uncles()
	if len(uncles) > maxUncles {
		return fmt.Errorf("%w: got %d, max %d",
			ErrBadUncleCount, len(uncles), maxUncles)
	}

	// Check for duplicate uncles within this block.
	seen := make(map[types.Hash]bool, len(uncles))
	for _, uncle := range uncles {
		h := uncle.Hash()
		if seen[h] {
			return fmt.Errorf("%w: %s", ErrDuplicateUncle, h.Hex())
		}
		seen[h] = true
	}

	// Validate each uncle against the ancestor chain if available.
	if bp.ancestors == nil {
		bp.metrics.UnclesVerified.Add(int64(len(uncles)))
		return nil
	}

	// Build ancestor set for the current block going back maxUncleDepth.
	ancestors := make(map[types.Hash]bool)
	parent := bp.ancestors.GetHeader(block.ParentHash())
	for i := 0; i < maxUncleDepth && parent != nil; i++ {
		ancestors[parent.Hash()] = true
		parent = bp.ancestors.GetHeader(parent.ParentHash)
	}

	for _, uncle := range uncles {
		uncleHash := uncle.Hash()

		// Uncle must not be a direct ancestor.
		if ancestors[uncleHash] {
			return fmt.Errorf("%w: %s", ErrUncleIsAncestor, uncleHash.Hex())
		}

		// Uncle's parent must exist in the recent ancestor chain.
		if !ancestors[uncle.ParentHash] {
			return fmt.Errorf("%w: uncle %s parent %s",
				ErrBadUncleParentHash, uncleHash.Hex(), uncle.ParentHash.Hex())
		}
	}

	bp.metrics.UnclesVerified.Add(int64(len(uncles)))
	return nil
}

// QueueSize returns the current number of blocks in the queue.
func (bp *BlockProcessor) QueueSize() int {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	return len(bp.queue)
}

// NextExpected returns the next block number the processor expects.
func (bp *BlockProcessor) NextExpected() uint64 {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	return bp.nextExpected
}

// Metrics returns the processor's metrics.
func (bp *BlockProcessor) Metrics() *BlockProcessorMetrics {
	return bp.metrics
}

// Close shuts down the block processor and clears the queue.
func (bp *BlockProcessor) Close() {
	bp.closed.Store(true)
	bp.mu.Lock()
	defer bp.mu.Unlock()
	bp.queue = make(map[uint64]*queuedBlock)
	bp.seen = make(map[types.Hash]bool)
	bp.metrics.QueueSize.Set(0)
}
