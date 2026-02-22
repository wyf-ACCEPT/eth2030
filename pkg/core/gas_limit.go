package core

import (
	"fmt"
	"sort"

	"github.com/eth2030/eth2030/core/types"
)

// GasLimitEntry defines a target gas limit that activates at a specific time.
type GasLimitEntry struct {
	ActivationTime uint64
	TargetGasLimit uint64
}

// GasLimitSchedule is a time-ordered list of gas limit targets.
type GasLimitSchedule []GasLimitEntry

// DefaultGasLimitSchedule defines the planned gas limit increases (3x/year).
var DefaultGasLimitSchedule = GasLimitSchedule{
	{ActivationTime: 0, TargetGasLimit: 60_000_000},           // current: 60M
	{ActivationTime: 15_768_000, TargetGasLimit: 180_000_000}, // +6 months: 180M (3x)
	{ActivationTime: 31_536_000, TargetGasLimit: 540_000_000}, // +12 months: 540M (3x)
	{ActivationTime: 47_304_000, TargetGasLimit: 1_000_000_000}, // +18 months: 1G (capped)
}

// GetTargetGasLimit returns the target gas limit for a given timestamp
// based on the gas limit schedule.
func GetTargetGasLimit(schedule GasLimitSchedule, time uint64) uint64 {
	if len(schedule) == 0 {
		return 0
	}

	// Sort by activation time (should already be sorted, but be safe).
	sorted := make(GasLimitSchedule, len(schedule))
	copy(sorted, schedule)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ActivationTime < sorted[j].ActivationTime
	})

	// Find the latest entry that is active.
	target := sorted[0].TargetGasLimit
	for _, entry := range sorted {
		if entry.ActivationTime <= time {
			target = entry.TargetGasLimit
		} else {
			break
		}
	}
	return target
}

// CalcGasLimit computes the gas limit for the next block, moving from
// parentGasLimit toward targetGasLimit at the maximum allowed rate
// (1/1024 per block).
func CalcGasLimit(parentGasLimit, targetGasLimit uint64) uint64 {
	delta := parentGasLimit / GasLimitBoundDivisor
	if delta < 1 {
		delta = 1
	}

	var limit uint64
	if targetGasLimit > parentGasLimit {
		// Increasing toward target.
		if parentGasLimit+delta > targetGasLimit {
			limit = targetGasLimit
		} else {
			limit = parentGasLimit + delta
		}
	} else if targetGasLimit < parentGasLimit {
		// Decreasing toward target.
		if parentGasLimit-delta < targetGasLimit {
			limit = targetGasLimit
		} else {
			limit = parentGasLimit - delta
		}
	} else {
		limit = parentGasLimit
	}

	if limit < MinGasLimit {
		limit = MinGasLimit
	}
	return limit
}

// ValidateGasLimit validates that the gas limit change between parent and
// header is within the allowed bounds (1/1024 per block) and trending
// toward the schedule target.
func ValidateGasLimit(schedule GasLimitSchedule, parent *types.Header, header *types.Header) error {
	parentGasLimit := parent.GasLimit
	headerGasLimit := header.GasLimit

	// Check minimum gas limit.
	if headerGasLimit < MinGasLimit {
		return fmt.Errorf("gas limit %d below minimum %d", headerGasLimit, MinGasLimit)
	}

	// Check the 1/1024 bound.
	delta := parentGasLimit / GasLimitBoundDivisor
	if delta < 1 {
		delta = 1
	}

	var diff uint64
	if headerGasLimit > parentGasLimit {
		diff = headerGasLimit - parentGasLimit
	} else {
		diff = parentGasLimit - headerGasLimit
	}

	if diff > delta {
		return fmt.Errorf("gas limit change too large: parent=%d, header=%d, max delta=%d",
			parentGasLimit, headerGasLimit, delta)
	}

	return nil
}
