package consensus

import (
	"errors"
	"fmt"
	"sync"
)

// ConsolidationManager handles queued validator consolidation requests per
// EIP-7251, with rate limiting, balance transfer, and slashing protection.

// Consolidation manager errors.
var (
	ErrCMQueueFull          = errors.New("consolidation-mgr: queue is full")
	ErrCMRequestExists      = errors.New("consolidation-mgr: request already queued")
	ErrCMSourceEqualsTarget = errors.New("consolidation-mgr: source equals target")
	ErrCMSourceInactive     = errors.New("consolidation-mgr: source validator not active")
	ErrCMTargetInactive     = errors.New("consolidation-mgr: target validator not active")
	ErrCMSourceSlashed      = errors.New("consolidation-mgr: source validator is slashed")
	ErrCMTargetSlashed      = errors.New("consolidation-mgr: target validator is slashed")
	ErrCMCredsMismatch      = errors.New("consolidation-mgr: withdrawal credentials mismatch")
	ErrCMNotCompounding     = errors.New("consolidation-mgr: target must have compounding credentials")
	ErrCMRateLimited        = errors.New("consolidation-mgr: rate limit exceeded for this epoch")
	ErrCMBalanceOverflow    = errors.New("consolidation-mgr: balance overflow")
	ErrCMEmptyQueue         = errors.New("consolidation-mgr: queue is empty")
)

// CMMaxEffectiveBalance is 2048 ETH per EIP-7251.
const CMMaxEffectiveBalance = 2048 * GweiPerETH

// ConsolidationReq represents a queued consolidation request.
type ConsolidationReq struct {
	SourceIndex  uint64
	TargetIndex  uint64
	SourcePubkey [48]byte
	TargetPubkey [48]byte
	RequestEpoch Epoch
	Processed    bool
}

// BalanceTransferResult records the outcome of an atomic balance transfer.
type BalanceTransferResult struct {
	SourceIndex       uint64
	TargetIndex       uint64
	AmountTransferred uint64
	NewSourceBalance  uint64
	NewTargetBalance  uint64
	TargetEffBal      uint64
	SourceExitEpoch   Epoch
}

// ConsolidationManagerConfig configures the consolidation manager.
type ConsolidationManagerConfig struct {
	// MaxQueueSize limits pending consolidation requests.
	MaxQueueSize int
	// MaxPerEpoch is the rate limit on consolidations processed per epoch.
	MaxPerEpoch int
	// MaxEffBalance is the cap on effective balance (2048 ETH default).
	MaxEffBalance uint64
}

// DefaultConsolidationManagerConfig returns production defaults.
func DefaultConsolidationManagerConfig() ConsolidationManagerConfig {
	return ConsolidationManagerConfig{
		MaxQueueSize:  256,
		MaxPerEpoch:   16,
		MaxEffBalance: CMMaxEffectiveBalance,
	}
}

// ConsolidationManager manages the consolidation request queue and processing.
// Thread-safe.
type ConsolidationManager struct {
	mu     sync.RWMutex
	config ConsolidationManagerConfig

	// Pending consolidation requests in FIFO order.
	queue []*ConsolidationReq

	// Dedup: source+target -> true.
	queued map[[2]uint64]bool

	// Per-epoch processed count for rate limiting.
	epochProcessed map[Epoch]int

	// Completed consolidation results.
	results []*BalanceTransferResult
}

// NewConsolidationManager creates a new manager with the given config.
func NewConsolidationManager(config ConsolidationManagerConfig) *ConsolidationManager {
	if config.MaxQueueSize <= 0 {
		config.MaxQueueSize = 256
	}
	if config.MaxPerEpoch <= 0 {
		config.MaxPerEpoch = 16
	}
	if config.MaxEffBalance == 0 {
		config.MaxEffBalance = CMMaxEffectiveBalance
	}
	return &ConsolidationManager{
		config:         config,
		queued:         make(map[[2]uint64]bool),
		epochProcessed: make(map[Epoch]int),
	}
}

// EnqueueRequest adds a consolidation request to the pending queue.
// Validates source/target validators before queuing.
func (cm *ConsolidationManager) EnqueueRequest(
	req *ConsolidationReq,
	source *ValidatorBalance,
	target *ValidatorBalance,
	currentEpoch Epoch,
) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if req.SourceIndex == req.TargetIndex {
		return ErrCMSourceEqualsTarget
	}

	// Validate source.
	if !source.IsActive(currentEpoch) {
		return ErrCMSourceInactive
	}
	if source.Slashed {
		return ErrCMSourceSlashed
	}

	// Validate target.
	if !target.IsActive(currentEpoch) {
		return ErrCMTargetInactive
	}
	if target.Slashed {
		return ErrCMTargetSlashed
	}

	// Check withdrawal credentials match.
	if source.WithdrawalCredentials != target.WithdrawalCredentials {
		return ErrCMCredsMismatch
	}

	// Target must have compounding credentials.
	if !target.HasCompoundingCredentials() {
		return ErrCMNotCompounding
	}

	// Check queue capacity.
	if len(cm.queue) >= cm.config.MaxQueueSize {
		return ErrCMQueueFull
	}

	// Check for duplicate.
	key := [2]uint64{req.SourceIndex, req.TargetIndex}
	if cm.queued[key] {
		return ErrCMRequestExists
	}

	reqCopy := *req
	reqCopy.RequestEpoch = currentEpoch
	cm.queue = append(cm.queue, &reqCopy)
	cm.queued[key] = true

	return nil
}

// ProcessNextConsolidation dequeues and processes the next pending request.
// Returns the balance transfer result or an error if rate limited or queue empty.
func (cm *ConsolidationManager) ProcessNextConsolidation(
	getValidator func(uint64) *ValidatorBalance,
	getBalance func(uint64) uint64,
	currentEpoch Epoch,
) (*BalanceTransferResult, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Check rate limit.
	if cm.epochProcessed[currentEpoch] >= cm.config.MaxPerEpoch {
		return nil, ErrCMRateLimited
	}

	// Find next unprocessed request.
	var req *ConsolidationReq
	var idx int
	for i, r := range cm.queue {
		if !r.Processed {
			req = r
			idx = i
			break
		}
	}
	if req == nil {
		return nil, ErrCMEmptyQueue
	}

	source := getValidator(req.SourceIndex)
	target := getValidator(req.TargetIndex)
	if source == nil || target == nil {
		// Mark as processed (invalid) and skip.
		cm.queue[idx].Processed = true
		return nil, fmt.Errorf("consolidation-mgr: validator not found")
	}

	// Validate slashing protection: neither should be slashed.
	if source.Slashed {
		cm.queue[idx].Processed = true
		return nil, ErrCMSourceSlashed
	}
	if target.Slashed {
		cm.queue[idx].Processed = true
		return nil, ErrCMTargetSlashed
	}

	// Perform atomic balance transfer.
	sourceBalance := getBalance(req.SourceIndex)
	targetBalance := getBalance(req.TargetIndex)

	newTargetBalance := targetBalance + sourceBalance
	if newTargetBalance < targetBalance {
		cm.queue[idx].Processed = true
		return nil, ErrCMBalanceOverflow
	}

	// Cap effective balance at max.
	targetEff := newTargetBalance
	if targetEff > cm.config.MaxEffBalance {
		targetEff = cm.config.MaxEffBalance
	}
	targetEff = (targetEff / EffectiveBalanceIncrement) * EffectiveBalanceIncrement
	target.EffectiveBalance = targetEff

	// Mark source for exit.
	source.ExitEpoch = currentEpoch + 1
	source.EffectiveBalance = 0

	result := &BalanceTransferResult{
		SourceIndex:       req.SourceIndex,
		TargetIndex:       req.TargetIndex,
		AmountTransferred: sourceBalance,
		NewSourceBalance:  0,
		NewTargetBalance:  newTargetBalance,
		TargetEffBal:      targetEff,
		SourceExitEpoch:   currentEpoch + 1,
	}

	cm.queue[idx].Processed = true
	cm.epochProcessed[currentEpoch]++
	cm.results = append(cm.results, result)

	return result, nil
}

// QueueLen returns the number of pending (unprocessed) requests.
func (cm *ConsolidationManager) QueueLen() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	count := 0
	for _, r := range cm.queue {
		if !r.Processed {
			count++
		}
	}
	return count
}

// TotalQueued returns the total queue size (processed and unprocessed).
func (cm *ConsolidationManager) TotalQueued() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.queue)
}

// ProcessedInEpoch returns how many consolidations were processed in the epoch.
func (cm *ConsolidationManager) ProcessedInEpoch(epoch Epoch) int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.epochProcessed[epoch]
}

// Results returns a copy of all completed consolidation results.
func (cm *ConsolidationManager) Results() []*BalanceTransferResult {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	result := make([]*BalanceTransferResult, len(cm.results))
	for i, r := range cm.results {
		cp := *r
		result[i] = &cp
	}
	return result
}

// PruneProcessed removes processed requests from the queue.
func (cm *ConsolidationManager) PruneProcessed() int {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	var remaining []*ConsolidationReq
	pruned := 0
	for _, r := range cm.queue {
		if r.Processed {
			key := [2]uint64{r.SourceIndex, r.TargetIndex}
			delete(cm.queued, key)
			pruned++
		} else {
			remaining = append(remaining, r)
		}
	}
	cm.queue = remaining
	return pruned
}
