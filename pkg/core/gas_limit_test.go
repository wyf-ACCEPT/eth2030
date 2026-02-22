package core

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestGetTargetGasLimit_Default(t *testing.T) {
	tests := []struct {
		time   uint64
		target uint64
	}{
		{0, 60_000_000},
		{1, 60_000_000},
		{15_767_999, 60_000_000},
		{15_768_000, 180_000_000},
		{31_535_999, 180_000_000},
		{31_536_000, 540_000_000},
		{47_303_999, 540_000_000},
		{47_304_000, 1_000_000_000},
		{100_000_000, 1_000_000_000},
	}

	for _, tt := range tests {
		got := GetTargetGasLimit(DefaultGasLimitSchedule, tt.time)
		if got != tt.target {
			t.Errorf("time=%d: target = %d, want %d", tt.time, got, tt.target)
		}
	}
}

func TestGetTargetGasLimit_EmptySchedule(t *testing.T) {
	got := GetTargetGasLimit(nil, 100)
	if got != 0 {
		t.Errorf("empty schedule: got %d, want 0", got)
	}
}

func TestGetTargetGasLimit_SingleEntry(t *testing.T) {
	schedule := GasLimitSchedule{
		{ActivationTime: 50, TargetGasLimit: 100_000_000},
	}

	// Before activation, returns the entry (since it's the first).
	got := GetTargetGasLimit(schedule, 0)
	if got != 100_000_000 {
		t.Errorf("before activation: got %d, want 100000000", got)
	}

	got = GetTargetGasLimit(schedule, 50)
	if got != 100_000_000 {
		t.Errorf("at activation: got %d, want 100000000", got)
	}
}

func TestGetTargetGasLimit_UnsortedSchedule(t *testing.T) {
	// GetTargetGasLimit should sort internally.
	schedule := GasLimitSchedule{
		{ActivationTime: 200, TargetGasLimit: 300_000_000},
		{ActivationTime: 100, TargetGasLimit: 200_000_000},
		{ActivationTime: 0, TargetGasLimit: 100_000_000},
	}

	if got := GetTargetGasLimit(schedule, 50); got != 100_000_000 {
		t.Errorf("time=50: got %d, want 100000000", got)
	}
	if got := GetTargetGasLimit(schedule, 150); got != 200_000_000 {
		t.Errorf("time=150: got %d, want 200000000", got)
	}
	if got := GetTargetGasLimit(schedule, 250); got != 300_000_000 {
		t.Errorf("time=250: got %d, want 300000000", got)
	}
}

func TestCalcGasLimit_Increasing(t *testing.T) {
	// Gas limit should move toward target by at most 1/1024.
	parent := uint64(60_000_000)
	target := uint64(180_000_000)
	maxDelta := parent / GasLimitBoundDivisor

	result := CalcGasLimit(parent, target)
	if result != parent+maxDelta {
		t.Errorf("increasing: got %d, want %d", result, parent+maxDelta)
	}
}

func TestCalcGasLimit_Decreasing(t *testing.T) {
	parent := uint64(180_000_000)
	target := uint64(60_000_000)
	maxDelta := parent / GasLimitBoundDivisor

	result := CalcGasLimit(parent, target)
	if result != parent-maxDelta {
		t.Errorf("decreasing: got %d, want %d", result, parent-maxDelta)
	}
}

func TestCalcGasLimit_AtTarget(t *testing.T) {
	parent := uint64(60_000_000)
	target := uint64(60_000_000)

	result := CalcGasLimit(parent, target)
	if result != parent {
		t.Errorf("at target: got %d, want %d", result, parent)
	}
}

func TestCalcGasLimit_CloseToTarget(t *testing.T) {
	// When close enough to target, snap to it.
	target := uint64(60_000_000)
	parent := target - 10 // very close
	maxDelta := parent / GasLimitBoundDivisor

	// Since diff to target (10) < maxDelta, should snap to target.
	if 10 < maxDelta {
		result := CalcGasLimit(parent, target)
		if result != target {
			t.Errorf("close to target: got %d, want %d", result, target)
		}
	}
}

func TestCalcGasLimit_MinGasLimit(t *testing.T) {
	// Should never go below MinGasLimit.
	result := CalcGasLimit(MinGasLimit, 1)
	if result < MinGasLimit {
		t.Errorf("below minimum: got %d, want >= %d", result, MinGasLimit)
	}
}

func TestCalcGasLimit_Convergence(t *testing.T) {
	// Simulate convergence from 60M to 180M over many blocks.
	current := uint64(60_000_000)
	target := uint64(180_000_000)
	blocks := 0

	for current < target {
		current = CalcGasLimit(current, target)
		blocks++
		if blocks > 100000 {
			t.Fatalf("did not converge within 100000 blocks, current=%d", current)
		}
	}

	if current != target {
		t.Errorf("converged to %d, want %d", current, target)
	}
	// The convergence should be reasonable. At 1/1024 per step, doubling
	// takes ~709 blocks (ln(2) * 1024). 60M -> 180M is ln(3) * 1024 ~ 1124 steps.
	if blocks < 500 || blocks > 2000 {
		t.Errorf("convergence took %d blocks, expected ~1124", blocks)
	}
}

func TestValidateGasLimit_Valid(t *testing.T) {
	parent := &types.Header{
		GasLimit: 60_000_000,
		Number:   big.NewInt(100),
	}
	delta := parent.GasLimit / GasLimitBoundDivisor
	child := &types.Header{
		GasLimit: parent.GasLimit + delta,
		Number:   big.NewInt(101),
	}

	err := ValidateGasLimit(DefaultGasLimitSchedule, parent, child)
	if err != nil {
		t.Fatalf("valid gas limit change rejected: %v", err)
	}
}

func TestValidateGasLimit_TooLarge(t *testing.T) {
	parent := &types.Header{
		GasLimit: 60_000_000,
		Number:   big.NewInt(100),
	}
	delta := parent.GasLimit / GasLimitBoundDivisor
	child := &types.Header{
		GasLimit: parent.GasLimit + delta + 1,
		Number:   big.NewInt(101),
	}

	err := ValidateGasLimit(DefaultGasLimitSchedule, parent, child)
	if err == nil {
		t.Fatal("expected error for too-large gas limit change")
	}
}

func TestValidateGasLimit_Decrease(t *testing.T) {
	parent := &types.Header{
		GasLimit: 180_000_000,
		Number:   big.NewInt(100),
	}
	delta := parent.GasLimit / GasLimitBoundDivisor
	child := &types.Header{
		GasLimit: parent.GasLimit - delta,
		Number:   big.NewInt(101),
	}

	err := ValidateGasLimit(DefaultGasLimitSchedule, parent, child)
	if err != nil {
		t.Fatalf("valid decrease rejected: %v", err)
	}
}

func TestValidateGasLimit_BelowMinimum(t *testing.T) {
	parent := &types.Header{
		GasLimit: MinGasLimit,
		Number:   big.NewInt(100),
	}
	child := &types.Header{
		GasLimit: MinGasLimit - 1,
		Number:   big.NewInt(101),
	}

	err := ValidateGasLimit(DefaultGasLimitSchedule, parent, child)
	if err == nil {
		t.Fatal("expected error for gas limit below minimum")
	}
}

func TestValidateGasLimit_NoChange(t *testing.T) {
	parent := &types.Header{
		GasLimit: 60_000_000,
		Number:   big.NewInt(100),
	}
	child := &types.Header{
		GasLimit: 60_000_000,
		Number:   big.NewInt(101),
	}

	err := ValidateGasLimit(DefaultGasLimitSchedule, parent, child)
	if err != nil {
		t.Fatalf("no-change gas limit rejected: %v", err)
	}
}

func TestCalcGasLimit_SmallValues(t *testing.T) {
	// Test with very small gas limits (but above MinGasLimit) to ensure delta >= 1.
	result := CalcGasLimit(MinGasLimit, MinGasLimit+100)
	// delta = 5000/1024 = 4
	expectedDelta := MinGasLimit / GasLimitBoundDivisor
	if result != MinGasLimit+expectedDelta {
		t.Errorf("small increasing: got %d, want %d", result, MinGasLimit+expectedDelta)
	}

	result = CalcGasLimit(MinGasLimit+100, MinGasLimit)
	expectedDelta = (MinGasLimit + 100) / GasLimitBoundDivisor
	if result != MinGasLimit+100-expectedDelta {
		t.Errorf("small decreasing: got %d, want %d", result, MinGasLimit+100-expectedDelta)
	}
}

func TestValidateGasLimitSchedule_Default(t *testing.T) {
	if err := ValidateGasLimitSchedule(DefaultGasLimitSchedule); err != nil {
		t.Fatalf("default schedule should be valid: %v", err)
	}
}

func TestValidateGasLimitSchedule_Empty(t *testing.T) {
	err := ValidateGasLimitSchedule(nil)
	if err != ErrGasLimitScheduleEmpty {
		t.Fatalf("expected ErrGasLimitScheduleEmpty, got %v", err)
	}
}

func TestValidateGasLimitSchedule_NotMonotone(t *testing.T) {
	schedule := GasLimitSchedule{
		{ActivationTime: 100, TargetGasLimit: 60_000_000},
		{ActivationTime: 50, TargetGasLimit: 120_000_000},
	}
	err := ValidateGasLimitSchedule(schedule)
	if err == nil {
		t.Fatal("expected error for non-monotone activation times")
	}
}

func TestValidateGasLimitSchedule_DuplicateTime(t *testing.T) {
	schedule := GasLimitSchedule{
		{ActivationTime: 100, TargetGasLimit: 60_000_000},
		{ActivationTime: 100, TargetGasLimit: 120_000_000},
	}
	err := ValidateGasLimitSchedule(schedule)
	if err == nil {
		t.Fatal("expected error for duplicate activation times")
	}
}

func TestValidateGasLimitSchedule_JumpTooLarge(t *testing.T) {
	schedule := GasLimitSchedule{
		{ActivationTime: 0, TargetGasLimit: 60_000_000},
		{ActivationTime: 100, TargetGasLimit: 60_000_001 * 3}, // > 3x
	}
	err := ValidateGasLimitSchedule(schedule)
	if err == nil {
		t.Fatal("expected error for > 3x jump")
	}
}

func TestValidateGasLimitSchedule_ExactlyThreeX(t *testing.T) {
	schedule := GasLimitSchedule{
		{ActivationTime: 0, TargetGasLimit: 60_000_000},
		{ActivationTime: 100, TargetGasLimit: 180_000_000}, // exactly 3x
	}
	if err := ValidateGasLimitSchedule(schedule); err != nil {
		t.Fatalf("3x jump should be allowed: %v", err)
	}
}

func TestValidateGasLimitSchedule_ExceedsGigagas(t *testing.T) {
	schedule := GasLimitSchedule{
		{ActivationTime: 0, TargetGasLimit: MaxGigagas + 1},
	}
	err := ValidateGasLimitSchedule(schedule)
	if err == nil {
		t.Fatal("expected error for exceeding gigagas ceiling")
	}
}

func TestValidateGasLimitSchedule_SingleEntry(t *testing.T) {
	schedule := GasLimitSchedule{
		{ActivationTime: 0, TargetGasLimit: 60_000_000},
	}
	if err := ValidateGasLimitSchedule(schedule); err != nil {
		t.Fatalf("single entry should be valid: %v", err)
	}
}
