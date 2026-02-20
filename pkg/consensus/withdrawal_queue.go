// Package consensus implements Ethereum consensus-layer primitives.
// This file implements a beacon chain withdrawal queue that manages
// validator withdrawal requests with priority ordering and rate limiting.

package consensus

import (
	"errors"
	"sort"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

var (
	ErrWithdrawalQueueFull     = errors.New("withdrawal queue: queue is full")
	ErrWithdrawalAlreadyQueued = errors.New("withdrawal queue: validator already has pending withdrawal")
	ErrWithdrawalProcessed     = errors.New("withdrawal queue: validator already withdrawn")
	ErrWithdrawalZeroAmount    = errors.New("withdrawal queue: amount must be > 0")
	ErrWithdrawalZeroAddress   = errors.New("withdrawal queue: target address must not be zero")
	ErrWithdrawalNotFound      = errors.New("withdrawal queue: withdrawal not found")
)

// WithdrawalQueueConfig holds configuration for the withdrawal queue.
type WithdrawalQueueConfig struct {
	// MaxQueueSize is the maximum number of pending withdrawal requests.
	MaxQueueSize int

	// MaxWithdrawalsPerSlot is the maximum number of withdrawals processed per slot.
	MaxWithdrawalsPerSlot int

	// MinWithdrawalDelay is the minimum number of slots a withdrawal must wait
	// before it can be processed.
	MinWithdrawalDelay uint64

	// ChurnLimit is the maximum number of validators that can exit per epoch.
	ChurnLimit int
}

// DefaultWithdrawalQueueConfig returns a sensible default configuration.
func DefaultWithdrawalQueueConfig() WithdrawalQueueConfig {
	return WithdrawalQueueConfig{
		MaxQueueSize:          65536,
		MaxWithdrawalsPerSlot: 16,
		MinWithdrawalDelay:    256,
		ChurnLimit:            8,
	}
}

// WithdrawalRequest represents a validator's request to withdraw funds.
type WithdrawalRequest struct {
	// ValidatorIndex identifies which validator is withdrawing.
	ValidatorIndex uint64

	// Amount is the withdrawal amount in Gwei.
	Amount uint64

	// TargetAddress is the execution-layer address to send funds to.
	TargetAddress types.Address

	// RequestSlot is the slot at which the withdrawal was requested.
	RequestSlot uint64

	// Priority determines processing order (higher = processed first).
	Priority uint8
}

// QueueStats contains statistics about the withdrawal queue.
type QueueStats struct {
	// Pending is the number of pending withdrawal requests.
	Pending int

	// Processed is the number of completed withdrawals.
	Processed int

	// TotalAmount is the total Gwei amount of all pending withdrawals.
	TotalAmount uint64
}

// WithdrawalQueue manages an ordered queue of validator withdrawal requests.
// It enforces rate limits, churn limits, and minimum withdrawal delays.
// All methods are thread-safe.
type WithdrawalQueue struct {
	mu        sync.Mutex
	config    WithdrawalQueueConfig
	queue     []*WithdrawalRequest
	processed map[uint64]bool // validatorIndex -> processed
	queued    map[uint64]bool // validatorIndex -> currently in queue
	lastSlot  uint64
}

// NewWithdrawalQueue creates a new withdrawal queue with the given config.
func NewWithdrawalQueue(config WithdrawalQueueConfig) *WithdrawalQueue {
	return &WithdrawalQueue{
		config:    config,
		queue:     make([]*WithdrawalRequest, 0),
		processed: make(map[uint64]bool),
		queued:    make(map[uint64]bool),
	}
}

// Enqueue adds a withdrawal request to the queue. Returns an error if the
// queue is full, the validator already has a pending or processed withdrawal,
// the amount is zero, or the target address is zero.
func (wq *WithdrawalQueue) Enqueue(request WithdrawalRequest) error {
	if request.Amount == 0 {
		return ErrWithdrawalZeroAmount
	}
	if request.TargetAddress.IsZero() {
		return ErrWithdrawalZeroAddress
	}

	wq.mu.Lock()
	defer wq.mu.Unlock()

	if wq.processed[request.ValidatorIndex] {
		return ErrWithdrawalProcessed
	}
	if wq.queued[request.ValidatorIndex] {
		return ErrWithdrawalAlreadyQueued
	}
	if len(wq.queue) >= wq.config.MaxQueueSize {
		return ErrWithdrawalQueueFull
	}

	// Copy the request and insert into the queue.
	r := request
	wq.queue = append(wq.queue, &r)
	wq.queued[request.ValidatorIndex] = true

	// Sort: higher priority first, then earlier request slot, then lower index.
	sort.SliceStable(wq.queue, func(i, j int) bool {
		if wq.queue[i].Priority != wq.queue[j].Priority {
			return wq.queue[i].Priority > wq.queue[j].Priority
		}
		if wq.queue[i].RequestSlot != wq.queue[j].RequestSlot {
			return wq.queue[i].RequestSlot < wq.queue[j].RequestSlot
		}
		return wq.queue[i].ValidatorIndex < wq.queue[j].ValidatorIndex
	})

	return nil
}

// ProcessSlot processes withdrawals for the given slot. It returns up to
// MaxWithdrawalsPerSlot withdrawals that have met the MinWithdrawalDelay.
// The churn limit is enforced per slot. Processed validators are recorded.
func (wq *WithdrawalQueue) ProcessSlot(slot uint64) []WithdrawalRequest {
	wq.mu.Lock()
	defer wq.mu.Unlock()

	wq.lastSlot = slot

	var result []WithdrawalRequest
	var remaining []*WithdrawalRequest
	processed := 0

	for _, req := range wq.queue {
		// Stop once we hit the per-slot limit or the churn limit.
		if processed >= wq.config.MaxWithdrawalsPerSlot || processed >= wq.config.ChurnLimit {
			remaining = append(remaining, req)
			continue
		}

		// Check the minimum withdrawal delay.
		if slot < req.RequestSlot+wq.config.MinWithdrawalDelay {
			remaining = append(remaining, req)
			continue
		}

		// Process this withdrawal.
		result = append(result, *req)
		wq.processed[req.ValidatorIndex] = true
		delete(wq.queued, req.ValidatorIndex)
		processed++
	}

	wq.queue = remaining
	return result
}

// PendingCount returns the number of pending withdrawal requests.
func (wq *WithdrawalQueue) PendingCount() int {
	wq.mu.Lock()
	defer wq.mu.Unlock()
	return len(wq.queue)
}

// IsProcessed returns true if the validator has already been processed.
func (wq *WithdrawalQueue) IsProcessed(validatorIndex uint64) bool {
	wq.mu.Lock()
	defer wq.mu.Unlock()
	return wq.processed[validatorIndex]
}

// GetPosition returns the queue position of the validator's withdrawal request.
// Returns -1 if the validator does not have a pending withdrawal.
// Position 0 is the front of the queue (highest priority).
func (wq *WithdrawalQueue) GetPosition(validatorIndex uint64) int {
	wq.mu.Lock()
	defer wq.mu.Unlock()

	for i, req := range wq.queue {
		if req.ValidatorIndex == validatorIndex {
			return i
		}
	}
	return -1
}

// CancelWithdrawal removes a pending withdrawal request for the given
// validator. Returns true if the withdrawal was found and removed.
func (wq *WithdrawalQueue) CancelWithdrawal(validatorIndex uint64) bool {
	wq.mu.Lock()
	defer wq.mu.Unlock()

	if !wq.queued[validatorIndex] {
		return false
	}

	for i, req := range wq.queue {
		if req.ValidatorIndex == validatorIndex {
			wq.queue = append(wq.queue[:i], wq.queue[i+1:]...)
			delete(wq.queued, validatorIndex)
			return true
		}
	}
	return false
}

// Stats returns current statistics about the withdrawal queue.
func (wq *WithdrawalQueue) Stats() QueueStats {
	wq.mu.Lock()
	defer wq.mu.Unlock()

	var totalAmount uint64
	for _, req := range wq.queue {
		totalAmount += req.Amount
	}

	return QueueStats{
		Pending:     len(wq.queue),
		Processed:   len(wq.processed),
		TotalAmount: totalAmount,
	}
}
