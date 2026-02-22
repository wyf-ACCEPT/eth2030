package das

import (
	"errors"
	"fmt"
	"sort"
)

// Variable blob configuration constants.
const (
	// DefaultBlobSize is the standard blob size in bytes (128 KiB).
	DefaultBlobSize = FieldElementsPerBlob * BytesPerFieldElement // 131072

	// MinBlobSizeBytes is the minimum allowed variable blob size (1 KiB).
	MinBlobSizeBytes = 1024

	// MaxBlobSizeBytes is the maximum allowed variable blob size (1 MiB).
	MaxBlobSizeBytes = 1 << 20
)

var (
	ErrInvalidBlobConfig   = errors.New("das: invalid blob config")
	ErrBlobCountOutOfRange = errors.New("das: blob count out of range")
	ErrBlobSizeOutOfRange  = errors.New("das: blob size out of range")
	ErrNoScheduleEntries   = errors.New("das: no blob schedule entries")
	ErrScheduleNotSorted   = errors.New("das: blob schedule entries not sorted by timestamp")
)

// BlobConfig defines the blob parameters for a given fork.
type BlobConfig struct {
	// MinBlobsPerBlock is the minimum number of blobs allowed per block.
	MinBlobsPerBlock uint64
	// MaxBlobsPerBlock is the maximum number of blobs allowed per block.
	MaxBlobsPerBlock uint64
	// TargetBlobsPerBlock is the target for the blob base fee adjustment.
	TargetBlobsPerBlock uint64
	// BlobSize is the maximum size of each blob in bytes.
	BlobSize uint64
}

// Validate checks that the BlobConfig has consistent parameters.
func (c *BlobConfig) Validate() error {
	if c.MaxBlobsPerBlock == 0 {
		return fmt.Errorf("%w: max blobs must be > 0", ErrInvalidBlobConfig)
	}
	if c.MinBlobsPerBlock > c.MaxBlobsPerBlock {
		return fmt.Errorf("%w: min %d > max %d", ErrInvalidBlobConfig, c.MinBlobsPerBlock, c.MaxBlobsPerBlock)
	}
	if c.TargetBlobsPerBlock < c.MinBlobsPerBlock || c.TargetBlobsPerBlock > c.MaxBlobsPerBlock {
		return fmt.Errorf("%w: target %d not in [%d, %d]", ErrInvalidBlobConfig, c.TargetBlobsPerBlock, c.MinBlobsPerBlock, c.MaxBlobsPerBlock)
	}
	if c.BlobSize == 0 {
		return fmt.Errorf("%w: blob size must be > 0", ErrInvalidBlobConfig)
	}
	if c.BlobSize < MinBlobSizeBytes || c.BlobSize > MaxBlobSizeBytes {
		return fmt.Errorf("%w: blob size %d not in [%d, %d]", ErrInvalidBlobConfig, c.BlobSize, MinBlobSizeBytes, MaxBlobSizeBytes)
	}
	return nil
}

// BlobScheduleEntry maps a fork activation timestamp to its blob configuration.
type BlobScheduleEntry struct {
	// Timestamp is the fork activation time (seconds since Unix epoch).
	Timestamp uint64
	// Config is the blob parameters active from this timestamp.
	Config BlobConfig
}

// BlobSchedule is an ordered list of blob schedule entries, sorted by timestamp.
type BlobSchedule []BlobScheduleEntry

// DefaultBlobSchedule defines the progressive blob parameter increases.
// Cancun (EIP-4844): 6 blobs, Pectra (EIP-7691): 9 blobs, future forks scale further.
var DefaultBlobSchedule = BlobSchedule{
	{
		Timestamp: 0, // Genesis / Cancun
		Config: BlobConfig{
			MinBlobsPerBlock:    0,
			MaxBlobsPerBlock:    6,
			TargetBlobsPerBlock: 3,
			BlobSize:            DefaultBlobSize,
		},
	},
	{
		Timestamp: 1710338135, // Pectra (EIP-7691 increase)
		Config: BlobConfig{
			MinBlobsPerBlock:    0,
			MaxBlobsPerBlock:    9,
			TargetBlobsPerBlock: 6,
			BlobSize:            DefaultBlobSize,
		},
	},
	{
		Timestamp: 1750000000, // Future Fulu-era increase
		Config: BlobConfig{
			MinBlobsPerBlock:    0,
			MaxBlobsPerBlock:    16,
			TargetBlobsPerBlock: 8,
			BlobSize:            DefaultBlobSize,
		},
	},
	{
		Timestamp: 1800000000, // Future further increase
		Config: BlobConfig{
			MinBlobsPerBlock:    0,
			MaxBlobsPerBlock:    32,
			TargetBlobsPerBlock: 16,
			BlobSize:            DefaultBlobSize,
		},
	},
}

// Validate checks that the schedule is properly ordered and all configs are valid.
func (s BlobSchedule) Validate() error {
	if len(s) == 0 {
		return ErrNoScheduleEntries
	}
	for i, entry := range s {
		if err := entry.Config.Validate(); err != nil {
			return fmt.Errorf("entry %d (timestamp %d): %w", i, entry.Timestamp, err)
		}
		if i > 0 && entry.Timestamp <= s[i-1].Timestamp {
			return fmt.Errorf("%w: entry %d timestamp %d <= entry %d timestamp %d",
				ErrScheduleNotSorted, i, entry.Timestamp, i-1, s[i-1].Timestamp)
		}
	}
	return nil
}

// GetBlobConfigAtTime returns the BlobConfig active at the given timestamp.
// It performs a binary search on the schedule to find the latest entry
// whose timestamp is <= the given time.
func GetBlobConfigAtTime(schedule BlobSchedule, timestamp uint64) BlobConfig {
	if len(schedule) == 0 {
		// Fallback to Cancun defaults.
		return BlobConfig{
			MinBlobsPerBlock:    0,
			MaxBlobsPerBlock:    6,
			TargetBlobsPerBlock: 3,
			BlobSize:            DefaultBlobSize,
		}
	}

	// Binary search: find the last entry with Timestamp <= timestamp.
	idx := sort.Search(len(schedule), func(i int) bool {
		return schedule[i].Timestamp > timestamp
	})
	if idx == 0 {
		// timestamp is before the first entry; use the first entry.
		return schedule[0].Config
	}
	return schedule[idx-1].Config
}

// ValidateBlobCount checks that the given blob count is within the dynamic
// range for the specified timestamp.
func ValidateBlobCount(schedule BlobSchedule, timestamp uint64, blobCount uint64) error {
	config := GetBlobConfigAtTime(schedule, timestamp)
	if blobCount < config.MinBlobsPerBlock {
		return fmt.Errorf("%w: have %d, min %d", ErrBlobCountOutOfRange, blobCount, config.MinBlobsPerBlock)
	}
	if blobCount > config.MaxBlobsPerBlock {
		return fmt.Errorf("%w: have %d, max %d", ErrBlobCountOutOfRange, blobCount, config.MaxBlobsPerBlock)
	}
	return nil
}

// ValidateVariableBlobSize checks that a blob's data size is valid for the
// given timestamp's configuration.
func ValidateVariableBlobSize(schedule BlobSchedule, timestamp uint64, blobDataSize uint64) error {
	config := GetBlobConfigAtTime(schedule, timestamp)
	if blobDataSize == 0 {
		return fmt.Errorf("%w: blob size must be > 0", ErrBlobSizeOutOfRange)
	}
	if blobDataSize > config.BlobSize {
		return fmt.Errorf("%w: blob size %d exceeds max %d", ErrBlobSizeOutOfRange, blobDataSize, config.BlobSize)
	}
	return nil
}
