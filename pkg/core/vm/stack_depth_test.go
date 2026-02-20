package vm

import (
	"testing"
)

// TestStackDepthTrackerNewDefault verifies the default tracker starts at depth 0.
func TestStackDepthTrackerNewDefault(t *testing.T) {
	sd := NewStackDepthTracker()

	if sd.CurrentDepth() != 0 {
		t.Fatalf("initial depth: got %d, want 0", sd.CurrentDepth())
	}
	if sd.MaxDepth() != DefaultMaxCallStackDepth {
		t.Fatalf("max depth: got %d, want %d", sd.MaxDepth(), DefaultMaxCallStackDepth)
	}
	if sd.PeakDepth() != 0 {
		t.Fatalf("peak depth: got %d, want 0", sd.PeakDepth())
	}
	if !sd.HasSpace() {
		t.Fatal("expected HasSpace to be true at depth 0")
	}
}

// TestStackDepthTrackerEnterLeave tests basic enter/leave operations.
func TestStackDepthTrackerEnterLeave(t *testing.T) {
	sd := NewStackDepthTracker()

	// Enter 3 levels.
	for i := 0; i < 3; i++ {
		if err := sd.Enter(); err != nil {
			t.Fatalf("Enter at depth %d: %v", i, err)
		}
	}
	if sd.CurrentDepth() != 3 {
		t.Fatalf("depth after 3 enters: got %d, want 3", sd.CurrentDepth())
	}

	// Leave 2 levels.
	for i := 0; i < 2; i++ {
		if err := sd.Leave(); err != nil {
			t.Fatalf("Leave at depth %d: %v", sd.CurrentDepth(), err)
		}
	}
	if sd.CurrentDepth() != 1 {
		t.Fatalf("depth after 2 leaves: got %d, want 1", sd.CurrentDepth())
	}
}

// TestStackDepthTrackerMaxDepthEnforcement verifies the depth limit.
func TestStackDepthTrackerMaxDepthEnforcement(t *testing.T) {
	sd := NewStackDepthTrackerWithLimit(5)

	// Fill up to the limit.
	for i := 0; i < 5; i++ {
		if err := sd.Enter(); err != nil {
			t.Fatalf("Enter at depth %d: %v", i, err)
		}
	}
	if sd.CurrentDepth() != 5 {
		t.Fatalf("depth at limit: got %d, want 5", sd.CurrentDepth())
	}
	if sd.HasSpace() {
		t.Fatal("expected HasSpace to be false at max depth")
	}
	if !sd.IsAtLimit() {
		t.Fatal("expected IsAtLimit to be true")
	}

	// One more Enter should fail.
	err := sd.Enter()
	if err != ErrCallStackDepthExceeded {
		t.Fatalf("expected ErrCallStackDepthExceeded, got: %v", err)
	}

	// Depth should not have changed.
	if sd.CurrentDepth() != 5 {
		t.Fatalf("depth after failed enter: got %d, want 5", sd.CurrentDepth())
	}
}

// TestStackDepthTrackerLeaveUnderflow verifies underflow protection.
func TestStackDepthTrackerLeaveUnderflow(t *testing.T) {
	sd := NewStackDepthTracker()

	err := sd.Leave()
	if err != ErrCallStackUnderflow {
		t.Fatalf("expected ErrCallStackUnderflow, got: %v", err)
	}

	// Depth should still be 0.
	if sd.CurrentDepth() != 0 {
		t.Fatalf("depth after underflow leave: got %d, want 0", sd.CurrentDepth())
	}
}

// TestStackDepthTrackerPeakDepth verifies peak tracking.
func TestStackDepthTrackerPeakDepth(t *testing.T) {
	sd := NewStackDepthTracker()

	// Enter 10 levels.
	for i := 0; i < 10; i++ {
		if err := sd.Enter(); err != nil {
			t.Fatal(err)
		}
	}
	// Leave back to 3.
	for i := 0; i < 7; i++ {
		if err := sd.Leave(); err != nil {
			t.Fatal(err)
		}
	}

	if sd.PeakDepth() != 10 {
		t.Fatalf("peak depth: got %d, want 10", sd.PeakDepth())
	}
	if sd.CurrentDepth() != 3 {
		t.Fatalf("current depth: got %d, want 3", sd.CurrentDepth())
	}
}

// TestStackDepthTrackerReset verifies the reset operation.
func TestStackDepthTrackerReset(t *testing.T) {
	sd := NewStackDepthTracker()

	for i := 0; i < 5; i++ {
		_ = sd.Enter()
	}
	sd.Reset()

	if sd.CurrentDepth() != 0 {
		t.Fatalf("depth after reset: got %d, want 0", sd.CurrentDepth())
	}
	if sd.PeakDepth() != 0 {
		t.Fatalf("peak after reset: got %d, want 0", sd.PeakDepth())
	}
}

// TestStackDepthTrackerRemainingDepth verifies remaining depth calculation.
func TestStackDepthTrackerRemainingDepth(t *testing.T) {
	sd := NewStackDepthTrackerWithLimit(100)

	if sd.RemainingDepth() != 100 {
		t.Fatalf("remaining at 0: got %d, want 100", sd.RemainingDepth())
	}

	for i := 0; i < 30; i++ {
		_ = sd.Enter()
	}
	if sd.RemainingDepth() != 70 {
		t.Fatalf("remaining at 30: got %d, want 70", sd.RemainingDepth())
	}
}

// TestStackDepthTrackerFull1024 verifies the full 1024 depth limit.
func TestStackDepthTrackerFull1024(t *testing.T) {
	sd := NewStackDepthTracker()

	// Enter all 1024 levels.
	for i := 0; i < 1024; i++ {
		if err := sd.Enter(); err != nil {
			t.Fatalf("Enter at depth %d: %v", i, err)
		}
	}

	if sd.CurrentDepth() != 1024 {
		t.Fatalf("depth at full: got %d, want 1024", sd.CurrentDepth())
	}
	if sd.HasSpace() {
		t.Fatal("expected no space at depth 1024")
	}

	// Attempt 1025th entry should fail.
	err := sd.Enter()
	if err != ErrCallStackDepthExceeded {
		t.Fatalf("expected ErrCallStackDepthExceeded at 1024, got: %v", err)
	}

	// Leave all the way back.
	for i := 0; i < 1024; i++ {
		if err := sd.Leave(); err != nil {
			t.Fatalf("Leave at depth %d: %v", sd.CurrentDepth(), err)
		}
	}
	if sd.CurrentDepth() != 0 {
		t.Fatalf("depth after all leaves: got %d, want 0", sd.CurrentDepth())
	}
}

// TestStackDepthTrackerCustomLimit verifies custom limit with edge cases.
func TestStackDepthTrackerCustomLimit(t *testing.T) {
	// Zero limit should default to 1024.
	sd := NewStackDepthTrackerWithLimit(0)
	if sd.MaxDepth() != DefaultMaxCallStackDepth {
		t.Fatalf("zero limit: got %d, want %d", sd.MaxDepth(), DefaultMaxCallStackDepth)
	}

	// Negative limit should default to 1024.
	sd = NewStackDepthTrackerWithLimit(-5)
	if sd.MaxDepth() != DefaultMaxCallStackDepth {
		t.Fatalf("negative limit: got %d, want %d", sd.MaxDepth(), DefaultMaxCallStackDepth)
	}

	// Limit of 1 allows exactly one entry.
	sd = NewStackDepthTrackerWithLimit(1)
	if err := sd.Enter(); err != nil {
		t.Fatalf("Enter at limit 1: %v", err)
	}
	if err := sd.Enter(); err != ErrCallStackDepthExceeded {
		t.Fatalf("expected depth exceeded at limit 1, got: %v", err)
	}
}

// TestStackDepthTrackerHasSpaceBeforeAndAfter tests HasSpace at boundary.
func TestStackDepthTrackerHasSpaceBeforeAndAfter(t *testing.T) {
	sd := NewStackDepthTrackerWithLimit(2)

	if !sd.HasSpace() {
		t.Fatal("should have space at depth 0")
	}

	_ = sd.Enter() // depth 1
	if !sd.HasSpace() {
		t.Fatal("should have space at depth 1 (limit 2)")
	}

	_ = sd.Enter() // depth 2
	if sd.HasSpace() {
		t.Fatal("should not have space at depth 2 (limit 2)")
	}

	_ = sd.Leave() // back to 1
	if !sd.HasSpace() {
		t.Fatal("should have space after leaving to depth 1")
	}
}
