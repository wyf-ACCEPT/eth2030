package das

import (
	"errors"
	"testing"
)

func TestBlobConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  BlobConfig
		wantErr bool
	}{
		{
			name: "valid cancun config",
			config: BlobConfig{
				MinBlobsPerBlock:    0,
				MaxBlobsPerBlock:    6,
				TargetBlobsPerBlock: 3,
				BlobSize:            DefaultBlobSize,
			},
		},
		{
			name: "valid pectra config",
			config: BlobConfig{
				MinBlobsPerBlock:    0,
				MaxBlobsPerBlock:    9,
				TargetBlobsPerBlock: 6,
				BlobSize:            DefaultBlobSize,
			},
		},
		{
			name: "zero max blobs",
			config: BlobConfig{
				MaxBlobsPerBlock:    0,
				TargetBlobsPerBlock: 0,
				BlobSize:            DefaultBlobSize,
			},
			wantErr: true,
		},
		{
			name: "min > max",
			config: BlobConfig{
				MinBlobsPerBlock:    10,
				MaxBlobsPerBlock:    6,
				TargetBlobsPerBlock: 3,
				BlobSize:            DefaultBlobSize,
			},
			wantErr: true,
		},
		{
			name: "target below min",
			config: BlobConfig{
				MinBlobsPerBlock:    2,
				MaxBlobsPerBlock:    6,
				TargetBlobsPerBlock: 1,
				BlobSize:            DefaultBlobSize,
			},
			wantErr: true,
		},
		{
			name: "target above max",
			config: BlobConfig{
				MinBlobsPerBlock:    0,
				MaxBlobsPerBlock:    6,
				TargetBlobsPerBlock: 7,
				BlobSize:            DefaultBlobSize,
			},
			wantErr: true,
		},
		{
			name: "zero blob size",
			config: BlobConfig{
				MinBlobsPerBlock:    0,
				MaxBlobsPerBlock:    6,
				TargetBlobsPerBlock: 3,
				BlobSize:            0,
			},
			wantErr: true,
		},
		{
			name: "blob size too small",
			config: BlobConfig{
				MinBlobsPerBlock:    0,
				MaxBlobsPerBlock:    6,
				TargetBlobsPerBlock: 3,
				BlobSize:            512,
			},
			wantErr: true,
		},
		{
			name: "blob size too large",
			config: BlobConfig{
				MinBlobsPerBlock:    0,
				MaxBlobsPerBlock:    6,
				TargetBlobsPerBlock: 3,
				BlobSize:            MaxBlobSizeBytes + 1,
			},
			wantErr: true,
		},
		{
			name: "min blob size boundary",
			config: BlobConfig{
				MinBlobsPerBlock:    0,
				MaxBlobsPerBlock:    1,
				TargetBlobsPerBlock: 0,
				BlobSize:            MinBlobSizeBytes,
			},
		},
		{
			name: "max blob size boundary",
			config: BlobConfig{
				MinBlobsPerBlock:    0,
				MaxBlobsPerBlock:    1,
				TargetBlobsPerBlock: 0,
				BlobSize:            MaxBlobSizeBytes,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBlobScheduleValidate(t *testing.T) {
	// Valid default schedule.
	if err := DefaultBlobSchedule.Validate(); err != nil {
		t.Fatalf("DefaultBlobSchedule.Validate() = %v", err)
	}

	// Empty schedule.
	empty := BlobSchedule{}
	if err := empty.Validate(); err != ErrNoScheduleEntries {
		t.Fatalf("expected ErrNoScheduleEntries, got %v", err)
	}

	// Unsorted schedule.
	unsorted := BlobSchedule{
		{Timestamp: 100, Config: BlobConfig{MaxBlobsPerBlock: 6, TargetBlobsPerBlock: 3, BlobSize: DefaultBlobSize}},
		{Timestamp: 50, Config: BlobConfig{MaxBlobsPerBlock: 9, TargetBlobsPerBlock: 6, BlobSize: DefaultBlobSize}},
	}
	if err := unsorted.Validate(); !errors.Is(err, ErrScheduleNotSorted) {
		t.Fatalf("expected ErrScheduleNotSorted, got %v", err)
	}

	// Duplicate timestamps.
	dup := BlobSchedule{
		{Timestamp: 100, Config: BlobConfig{MaxBlobsPerBlock: 6, TargetBlobsPerBlock: 3, BlobSize: DefaultBlobSize}},
		{Timestamp: 100, Config: BlobConfig{MaxBlobsPerBlock: 9, TargetBlobsPerBlock: 6, BlobSize: DefaultBlobSize}},
	}
	if err := dup.Validate(); !errors.Is(err, ErrScheduleNotSorted) {
		t.Fatalf("expected ErrScheduleNotSorted for duplicates, got %v", err)
	}

	// Invalid config in entry.
	badConfig := BlobSchedule{
		{Timestamp: 0, Config: BlobConfig{MaxBlobsPerBlock: 0, BlobSize: DefaultBlobSize}},
	}
	if err := badConfig.Validate(); !errors.Is(err, ErrInvalidBlobConfig) {
		t.Fatalf("expected ErrInvalidBlobConfig, got %v", err)
	}
}

func TestGetBlobConfigAtTime(t *testing.T) {
	schedule := BlobSchedule{
		{Timestamp: 100, Config: BlobConfig{MaxBlobsPerBlock: 6, TargetBlobsPerBlock: 3, BlobSize: DefaultBlobSize}},
		{Timestamp: 200, Config: BlobConfig{MaxBlobsPerBlock: 9, TargetBlobsPerBlock: 6, BlobSize: DefaultBlobSize}},
		{Timestamp: 300, Config: BlobConfig{MaxBlobsPerBlock: 16, TargetBlobsPerBlock: 8, BlobSize: DefaultBlobSize}},
	}

	tests := []struct {
		timestamp uint64
		wantMax   uint64
	}{
		{0, 6},     // Before first entry: use first entry.
		{50, 6},    // Before first entry.
		{100, 6},   // Exactly at first entry.
		{150, 6},   // Between first and second.
		{200, 9},   // Exactly at second entry.
		{250, 9},   // Between second and third.
		{300, 16},  // Exactly at third entry.
		{1000, 16}, // After all entries.
	}

	for _, tt := range tests {
		config := GetBlobConfigAtTime(schedule, tt.timestamp)
		if config.MaxBlobsPerBlock != tt.wantMax {
			t.Errorf("GetBlobConfigAtTime(%d).MaxBlobsPerBlock = %d, want %d",
				tt.timestamp, config.MaxBlobsPerBlock, tt.wantMax)
		}
	}

	// Empty schedule should return Cancun defaults.
	config := GetBlobConfigAtTime(nil, 999)
	if config.MaxBlobsPerBlock != 6 {
		t.Errorf("empty schedule: MaxBlobsPerBlock = %d, want 6", config.MaxBlobsPerBlock)
	}
}

func TestValidateBlobCount(t *testing.T) {
	schedule := BlobSchedule{
		{Timestamp: 0, Config: BlobConfig{MinBlobsPerBlock: 0, MaxBlobsPerBlock: 6, TargetBlobsPerBlock: 3, BlobSize: DefaultBlobSize}},
		{Timestamp: 200, Config: BlobConfig{MinBlobsPerBlock: 1, MaxBlobsPerBlock: 9, TargetBlobsPerBlock: 6, BlobSize: DefaultBlobSize}},
	}

	// Valid at timestamp 0.
	if err := ValidateBlobCount(schedule, 0, 0); err != nil {
		t.Errorf("count 0 at t=0 should be valid: %v", err)
	}
	if err := ValidateBlobCount(schedule, 0, 6); err != nil {
		t.Errorf("count 6 at t=0 should be valid: %v", err)
	}

	// Too many at timestamp 0.
	if err := ValidateBlobCount(schedule, 0, 7); !errors.Is(err, ErrBlobCountOutOfRange) {
		t.Errorf("count 7 at t=0: expected ErrBlobCountOutOfRange, got %v", err)
	}

	// Valid at timestamp 200.
	if err := ValidateBlobCount(schedule, 200, 1); err != nil {
		t.Errorf("count 1 at t=200 should be valid: %v", err)
	}
	if err := ValidateBlobCount(schedule, 200, 9); err != nil {
		t.Errorf("count 9 at t=200 should be valid: %v", err)
	}

	// Below min at timestamp 200.
	if err := ValidateBlobCount(schedule, 200, 0); !errors.Is(err, ErrBlobCountOutOfRange) {
		t.Errorf("count 0 at t=200: expected ErrBlobCountOutOfRange, got %v", err)
	}

	// Above max at timestamp 200.
	if err := ValidateBlobCount(schedule, 200, 10); !errors.Is(err, ErrBlobCountOutOfRange) {
		t.Errorf("count 10 at t=200: expected ErrBlobCountOutOfRange, got %v", err)
	}
}

func TestValidateVariableBlobSize(t *testing.T) {
	schedule := BlobSchedule{
		{Timestamp: 0, Config: BlobConfig{
			MinBlobsPerBlock: 0, MaxBlobsPerBlock: 6, TargetBlobsPerBlock: 3,
			BlobSize: DefaultBlobSize,
		}},
	}

	// Valid sizes.
	if err := ValidateVariableBlobSize(schedule, 0, 1); err != nil {
		t.Errorf("size 1 should be valid: %v", err)
	}
	if err := ValidateVariableBlobSize(schedule, 0, DefaultBlobSize); err != nil {
		t.Errorf("size %d should be valid: %v", DefaultBlobSize, err)
	}

	// Zero size.
	if err := ValidateVariableBlobSize(schedule, 0, 0); !errors.Is(err, ErrBlobSizeOutOfRange) {
		t.Errorf("size 0: expected ErrBlobSizeOutOfRange, got %v", err)
	}

	// Exceeds max.
	if err := ValidateVariableBlobSize(schedule, 0, DefaultBlobSize+1); !errors.Is(err, ErrBlobSizeOutOfRange) {
		t.Errorf("size %d: expected ErrBlobSizeOutOfRange, got %v", DefaultBlobSize+1, err)
	}
}

func TestDefaultBlobScheduleConstants(t *testing.T) {
	if DefaultBlobSize != 131072 {
		t.Errorf("DefaultBlobSize = %d, want 131072", DefaultBlobSize)
	}
	if MinBlobSizeBytes != 1024 {
		t.Errorf("MinBlobSizeBytes = %d, want 1024", MinBlobSizeBytes)
	}
	if MaxBlobSizeBytes != 1<<20 {
		t.Errorf("MaxBlobSizeBytes = %d, want %d", MaxBlobSizeBytes, 1<<20)
	}
}

func TestDefaultBlobScheduleProgression(t *testing.T) {
	// Verify the default schedule has increasing max blobs.
	prevMax := uint64(0)
	for i, entry := range DefaultBlobSchedule {
		if entry.Config.MaxBlobsPerBlock <= prevMax && i > 0 {
			t.Errorf("entry %d: MaxBlobsPerBlock %d not greater than previous %d",
				i, entry.Config.MaxBlobsPerBlock, prevMax)
		}
		prevMax = entry.Config.MaxBlobsPerBlock
	}
}
