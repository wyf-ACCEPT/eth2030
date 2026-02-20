package vm

// stack_depth.go implements call stack depth tracking for the EVM.
// StackDepthTracker enforces the EVM's 1024 call depth limit, providing
// Enter/Leave operations for call frames and depth queries. It is used
// alongside the interpreter to prevent stack-depth-based attacks.

import "errors"

// Stack depth constants.
const (
	// DefaultMaxCallStackDepth is the standard Ethereum call depth limit.
	DefaultMaxCallStackDepth = 1024
)

// Errors for stack depth tracking.
var (
	ErrCallStackDepthExceeded = errors.New("stack depth: max call depth exceeded")
	ErrCallStackUnderflow     = errors.New("stack depth: cannot leave at depth 0")
)

// StackDepthTracker tracks the current EVM call stack depth and enforces
// the maximum depth limit (1024 by default). Each CALL, STATICCALL,
// DELEGATECALL, CALLCODE, CREATE, or CREATE2 increments the depth.
type StackDepthTracker struct {
	current  int
	maxDepth int
	peak     int // highest depth reached (for diagnostics)
}

// NewStackDepthTracker creates a tracker with the standard 1024 depth limit.
func NewStackDepthTracker() *StackDepthTracker {
	return &StackDepthTracker{
		maxDepth: DefaultMaxCallStackDepth,
	}
}

// NewStackDepthTrackerWithLimit creates a tracker with a custom depth limit.
func NewStackDepthTrackerWithLimit(maxDepth int) *StackDepthTracker {
	if maxDepth <= 0 {
		maxDepth = DefaultMaxCallStackDepth
	}
	return &StackDepthTracker{
		maxDepth: maxDepth,
	}
}

// Enter increments the call depth by one, simulating entry into a new call
// frame. Returns ErrCallStackDepthExceeded if the depth would exceed the
// maximum. The check happens before incrementing: if current == maxDepth,
// no more calls are possible.
func (sd *StackDepthTracker) Enter() error {
	if sd.current >= sd.maxDepth {
		return ErrCallStackDepthExceeded
	}
	sd.current++
	if sd.current > sd.peak {
		sd.peak = sd.current
	}
	return nil
}

// Leave decrements the call depth by one, simulating return from a call
// frame. Returns ErrCallStackUnderflow if the depth is already zero.
func (sd *StackDepthTracker) Leave() error {
	if sd.current <= 0 {
		return ErrCallStackUnderflow
	}
	sd.current--
	return nil
}

// CurrentDepth returns the current call stack depth. A depth of 0 means
// the EVM is at the top-level execution context.
func (sd *StackDepthTracker) CurrentDepth() int {
	return sd.current
}

// HasSpace returns true if another call frame can be entered without
// exceeding the maximum depth. Equivalent to CurrentDepth() < MaxDepth().
func (sd *StackDepthTracker) HasSpace() bool {
	return sd.current < sd.maxDepth
}

// MaxDepth returns the configured maximum call depth.
func (sd *StackDepthTracker) MaxDepth() int {
	return sd.maxDepth
}

// PeakDepth returns the highest depth reached since the tracker was created
// or last reset. Useful for diagnostics and gas estimation.
func (sd *StackDepthTracker) PeakDepth() int {
	return sd.peak
}

// Reset resets the tracker to depth zero and clears the peak. This is
// useful when reusing a tracker across multiple transactions.
func (sd *StackDepthTracker) Reset() {
	sd.current = 0
	sd.peak = 0
}

// IsAtLimit returns true if the current depth equals the maximum depth,
// meaning no further calls are possible.
func (sd *StackDepthTracker) IsAtLimit() bool {
	return sd.current >= sd.maxDepth
}

// RemainingDepth returns the number of additional call frames that can
// be entered before hitting the depth limit.
func (sd *StackDepthTracker) RemainingDepth() int {
	remaining := sd.maxDepth - sd.current
	if remaining < 0 {
		return 0
	}
	return remaining
}
