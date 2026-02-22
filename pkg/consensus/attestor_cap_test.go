package consensus

import (
	"sync"
	"testing"
)

func TestDefaultAttestorCapConfig(t *testing.T) {
	cfg := DefaultAttestorCapConfig()
	if cfg.MaxAttestationSize != 1<<20 {
		t.Errorf("MaxAttestationSize = %d, want %d", cfg.MaxAttestationSize, 1<<20)
	}
	if cfg.MaxAggregateSize != 2<<20 {
		t.Errorf("MaxAggregateSize = %d, want %d", cfg.MaxAggregateSize, 2<<20)
	}
	if cfg.MinParticipation != 1000 {
		t.Errorf("MinParticipation = %d, want 1000", cfg.MinParticipation)
	}
}

func TestNewAttesterCapManager_Defaults(t *testing.T) {
	m := NewAttesterCapManager(AttestorCapConfig{})
	if m.config.MaxAttestationSize != DefaultMaxAttestationSize {
		t.Errorf("default MaxAttestationSize = %d, want %d", m.config.MaxAttestationSize, DefaultMaxAttestationSize)
	}
	if m.config.MaxAggregateSize != DefaultMaxAggregateSize {
		t.Errorf("default MaxAggregateSize = %d, want %d", m.config.MaxAggregateSize, DefaultMaxAggregateSize)
	}
}

func TestNewAttesterCapManager_CustomConfig(t *testing.T) {
	m := NewAttesterCapManager(AttestorCapConfig{
		MaxAttestationSize: 512,
		MaxAggregateSize:   1024,
		MinParticipation:   500,
	})
	if m.config.MaxAttestationSize != 512 {
		t.Errorf("MaxAttestationSize = %d, want 512", m.config.MaxAttestationSize)
	}
	if m.config.MaxAggregateSize != 1024 {
		t.Errorf("MaxAggregateSize = %d, want 1024", m.config.MaxAggregateSize)
	}
}

func TestEstimateSize_Nil(t *testing.T) {
	if got := EstimateSize(nil); got != 0 {
		t.Errorf("EstimateSize(nil) = %d, want 0", got)
	}
}

func TestEstimateSize_Empty(t *testing.T) {
	att := &CappedAttestation{}
	// Only the 24-byte fixed overhead.
	if got := EstimateSize(att); got != attestorCapOverhead {
		t.Errorf("EstimateSize(empty) = %d, want %d", got, attestorCapOverhead)
	}
}

func TestEstimateSize_WithData(t *testing.T) {
	att := &CappedAttestation{
		Data:            make([]byte, 100),
		Signature:       make([]byte, 96),
		AggregationBits: make([]byte, 64),
	}
	expected := attestorCapOverhead + 100 + 96 + 64
	if got := EstimateSize(att); got != expected {
		t.Errorf("EstimateSize = %d, want %d", got, expected)
	}
}

func TestValidateAttestation_Nil(t *testing.T) {
	m := NewAttesterCapManager(*DefaultAttestorCapConfig())
	if err := m.ValidateAttestation(nil); err != ErrAttestorNil {
		t.Errorf("expected ErrAttestorNil, got %v", err)
	}
}

func TestValidateAttestation_Valid(t *testing.T) {
	m := NewAttesterCapManager(*DefaultAttestorCapConfig())
	att := &CappedAttestation{
		ValidatorIndex:  1,
		Slot:            10,
		CommitteeIndex:  0,
		Data:            make([]byte, 256),
		Signature:       make([]byte, 96),
		AggregationBits: []byte{0xff},
	}
	if err := m.ValidateAttestation(att); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateAttestation_Oversized(t *testing.T) {
	m := NewAttesterCapManager(AttestorCapConfig{
		MaxAttestationSize: 100,
		MaxAggregateSize:   200,
	})
	att := &CappedAttestation{
		Data:      make([]byte, 200), // exceeds 100 byte cap
		Signature: make([]byte, 96),
	}
	if err := m.ValidateAttestation(att); err != ErrAttestorOversized {
		t.Errorf("expected ErrAttestorOversized, got %v", err)
	}
}

func TestValidateAttestation_ExactCap(t *testing.T) {
	cap := attestorCapOverhead + 100
	m := NewAttesterCapManager(AttestorCapConfig{
		MaxAttestationSize: cap,
		MaxAggregateSize:   cap * 2,
	})
	att := &CappedAttestation{
		Data: make([]byte, 100),
	}
	if err := m.ValidateAttestation(att); err != nil {
		t.Errorf("attestation at exact cap should be valid, got: %v", err)
	}
}

func TestValidateAttestation_StatsTracking(t *testing.T) {
	m := NewAttesterCapManager(AttestorCapConfig{
		MaxAttestationSize: 1000,
		MaxAggregateSize:   2000,
	})

	// Validate two valid attestations.
	att1 := &CappedAttestation{Data: make([]byte, 100)}
	att2 := &CappedAttestation{Data: make([]byte, 200)}
	m.ValidateAttestation(att1)
	m.ValidateAttestation(att2)

	// Validate one oversized.
	att3 := &CappedAttestation{Data: make([]byte, 1500)}
	m.ValidateAttestation(att3)

	stats := m.GetStats()
	if stats.Count != 2 {
		t.Errorf("Count = %d, want 2", stats.Count)
	}
	if stats.Oversized != 1 {
		t.Errorf("Oversized = %d, want 1", stats.Oversized)
	}

	size1 := EstimateSize(att1)
	size2 := EstimateSize(att2)
	expectedTotal := size1 + size2
	if stats.TotalSize != expectedTotal {
		t.Errorf("TotalSize = %d, want %d", stats.TotalSize, expectedTotal)
	}
	if stats.AvgSize != expectedTotal/2 {
		t.Errorf("AvgSize = %d, want %d", stats.AvgSize, expectedTotal/2)
	}
	if stats.MaxSize != size2 {
		t.Errorf("MaxSize = %d, want %d", stats.MaxSize, size2)
	}
}

func TestResetStats(t *testing.T) {
	m := NewAttesterCapManager(*DefaultAttestorCapConfig())
	m.ValidateAttestation(&CappedAttestation{Data: make([]byte, 50)})

	m.ResetStats()
	stats := m.GetStats()
	if stats.Count != 0 || stats.TotalSize != 0 || stats.MaxSize != 0 || stats.Oversized != 0 {
		t.Errorf("stats not reset: %+v", stats)
	}
}

func TestTrimAttestation_Nil(t *testing.T) {
	_, err := TrimAttestation(nil, 100)
	if err != ErrAttestorNil {
		t.Errorf("expected ErrAttestorNil, got %v", err)
	}
}

func TestTrimAttestation_AlreadyFits(t *testing.T) {
	att := &CappedAttestation{Data: make([]byte, 10)}
	trimmed, err := TrimAttestation(att, 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if trimmed != att {
		t.Error("attestation already fits, should return same pointer")
	}
}

func TestTrimAttestation_TrimsData(t *testing.T) {
	data := make([]byte, 500)
	for i := range data {
		data[i] = byte(i % 256)
	}
	att := &CappedAttestation{
		ValidatorIndex:  42,
		Slot:            99,
		Data:            data,
		Signature:       make([]byte, 96),
		AggregationBits: make([]byte, 8),
	}

	// Set max so only 100 bytes of data fits.
	maxSize := attestorCapOverhead + 96 + 8 + 100
	trimmed, err := TrimAttestation(att, maxSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uint64(len(trimmed.Data)) != 100 {
		t.Errorf("trimmed data len = %d, want 100", len(trimmed.Data))
	}
	if EstimateSize(trimmed) > maxSize {
		t.Errorf("trimmed size %d exceeds max %d", EstimateSize(trimmed), maxSize)
	}
	// Verify data content preserved.
	for i := 0; i < 100; i++ {
		if trimmed.Data[i] != byte(i%256) {
			t.Fatalf("data[%d] = %d, want %d", i, trimmed.Data[i], byte(i%256))
		}
	}
	// Verify original not mutated.
	if len(att.Data) != 500 {
		t.Error("original data was mutated")
	}
}

func TestTrimAttestation_FixedExceedsCap(t *testing.T) {
	att := &CappedAttestation{
		Signature:       make([]byte, 96),
		AggregationBits: make([]byte, 64),
	}
	// Max size smaller than fixed overhead + sig + bits.
	_, err := TrimAttestation(att, 10)
	if err != ErrAttestorDataTooLarge {
		t.Errorf("expected ErrAttestorDataTooLarge, got %v", err)
	}
}

func TestCappedAggregateAttestations_Empty(t *testing.T) {
	m := NewAttesterCapManager(*DefaultAttestorCapConfig())
	_, err := m.AggregateAttestations(nil)
	if err != ErrAttestorNoAttestations {
		t.Errorf("expected ErrAttestorNoAttestations, got %v", err)
	}
}

func TestCappedAggregateAttestations_Single(t *testing.T) {
	m := NewAttesterCapManager(*DefaultAttestorCapConfig())
	att := &CappedAttestation{
		Slot:            1,
		Data:            []byte{1, 2, 3},
		AggregationBits: []byte{0x01},
	}
	agg, err := m.AggregateAttestations([]*CappedAttestation{att})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agg != att {
		t.Error("single attestation should return same pointer")
	}
}

func TestCappedAggregateAttestations_MultipleValid(t *testing.T) {
	m := NewAttesterCapManager(*DefaultAttestorCapConfig())

	att1 := &CappedAttestation{
		Slot:            1,
		CommitteeIndex:  0,
		Data:            []byte{1, 2},
		Signature:       make([]byte, 96),
		AggregationBits: []byte{0x01}, // bit 0
	}
	att2 := &CappedAttestation{
		Slot:            1,
		CommitteeIndex:  0,
		Data:            []byte{3, 4},
		Signature:       make([]byte, 96),
		AggregationBits: []byte{0x02}, // bit 1
	}

	agg, err := m.AggregateAttestations([]*CappedAttestation{att1, att2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Merged bits should be 0x01 | 0x02 = 0x03.
	if len(agg.AggregationBits) != 1 || agg.AggregationBits[0] != 0x03 {
		t.Errorf("merged bits = %v, want [0x03]", agg.AggregationBits)
	}

	// Merged data should be concatenation.
	if len(agg.Data) != 4 {
		t.Errorf("merged data len = %d, want 4", len(agg.Data))
	}
	expected := []byte{1, 2, 3, 4}
	for i, b := range agg.Data {
		if b != expected[i] {
			t.Errorf("data[%d] = %d, want %d", i, b, expected[i])
		}
	}
}

func TestCappedAggregateAttestations_OversizedResult(t *testing.T) {
	m := NewAttesterCapManager(AttestorCapConfig{
		MaxAttestationSize: 200,
		MaxAggregateSize:   100, // very small aggregate cap
		MinParticipation:   0,
	})

	att1 := &CappedAttestation{
		Data:            make([]byte, 50),
		AggregationBits: []byte{0xff},
	}
	att2 := &CappedAttestation{
		Data:            make([]byte, 50),
		AggregationBits: []byte{0xff},
	}

	_, err := m.AggregateAttestations([]*CappedAttestation{att1, att2})
	if err != ErrAttestorAggOversized {
		t.Errorf("expected ErrAttestorAggOversized, got %v", err)
	}
}

func TestValidateAggregateSize_Nil(t *testing.T) {
	m := NewAttesterCapManager(*DefaultAttestorCapConfig())
	if err := m.ValidateAggregateSize(nil); err != ErrAttestorNil {
		t.Errorf("expected ErrAttestorNil, got %v", err)
	}
}

func TestValidateAggregateSize_Valid(t *testing.T) {
	m := NewAttesterCapManager(*DefaultAttestorCapConfig())
	agg := &CappedAttestation{
		Data:            make([]byte, 100),
		AggregationBits: []byte{0xff, 0xff}, // 16 bits, all set = 100% participation
	}
	if err := m.ValidateAggregateSize(agg); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateAggregateSize_LowParticipation(t *testing.T) {
	m := NewAttesterCapManager(AttestorCapConfig{
		MaxAttestationSize: DefaultMaxAttestationSize,
		MaxAggregateSize:   DefaultMaxAggregateSize,
		MinParticipation:   5000, // 50% required
	})
	// Only 1 bit set out of 64 = 1.5% participation.
	agg := &CappedAttestation{
		Data:            make([]byte, 10),
		AggregationBits: []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	}
	if err := m.ValidateAggregateSize(agg); err != ErrAttestorLowParticipation {
		t.Errorf("expected ErrAttestorLowParticipation, got %v", err)
	}
}

func TestValidateAggregateSize_ZeroMinParticipation(t *testing.T) {
	m := NewAttesterCapManager(AttestorCapConfig{
		MaxAttestationSize: DefaultMaxAttestationSize,
		MaxAggregateSize:   DefaultMaxAggregateSize,
		MinParticipation:   0, // no minimum
	})
	agg := &CappedAttestation{
		Data:            make([]byte, 10),
		AggregationBits: []byte{0x00}, // zero participation
	}
	if err := m.ValidateAggregateSize(agg); err != nil {
		t.Errorf("with MinParticipation=0, should accept: %v", err)
	}
}

func TestCountSetBits(t *testing.T) {
	tests := []struct {
		name string
		bits []byte
		want uint64
	}{
		{"empty", nil, 0},
		{"zero", []byte{0x00}, 0},
		{"one", []byte{0x01}, 1},
		{"all set byte", []byte{0xff}, 8},
		{"mixed", []byte{0xaa}, 4}, // 10101010
		{"multi byte", []byte{0xff, 0x01}, 9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countSetBits(tt.bits); got != tt.want {
				t.Errorf("countSetBits(%v) = %d, want %d", tt.bits, got, tt.want)
			}
		})
	}
}

func TestAttesterCapManager_Concurrent(t *testing.T) {
	m := NewAttesterCapManager(*DefaultAttestorCapConfig())
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			att := &CappedAttestation{
				ValidatorIndex:  uint64(idx),
				Data:            make([]byte, 100),
				AggregationBits: []byte{0xff},
			}
			m.ValidateAttestation(att)
		}(i)
	}

	wg.Wait()

	stats := m.GetStats()
	if stats.Count != 100 {
		t.Errorf("concurrent Count = %d, want 100", stats.Count)
	}
}

func TestCappedAggregateAttestations_DifferentBitLengths(t *testing.T) {
	m := NewAttesterCapManager(*DefaultAttestorCapConfig())

	att1 := &CappedAttestation{
		Slot:            1,
		Data:            []byte{1},
		Signature:       make([]byte, 96),
		AggregationBits: []byte{0x01},
	}
	att2 := &CappedAttestation{
		Slot:            1,
		Data:            []byte{2},
		Signature:       make([]byte, 96),
		AggregationBits: []byte{0x00, 0x01}, // longer bits
	}

	agg, err := m.AggregateAttestations([]*CappedAttestation{att1, att2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(agg.AggregationBits) != 2 {
		t.Errorf("merged bits len = %d, want 2", len(agg.AggregationBits))
	}
	if agg.AggregationBits[0] != 0x01 || agg.AggregationBits[1] != 0x01 {
		t.Errorf("merged bits = %v, want [0x01, 0x01]", agg.AggregationBits)
	}
}

func TestEstimateSize_1MiB(t *testing.T) {
	// Verify a ~1MiB attestation is exactly at the cap.
	dataSize := DefaultMaxAttestationSize - attestorCapOverhead
	att := &CappedAttestation{
		Data: make([]byte, dataSize),
	}
	if got := EstimateSize(att); got != DefaultMaxAttestationSize {
		t.Errorf("1MiB attestation size = %d, want %d", got, DefaultMaxAttestationSize)
	}
}

func TestGetStats_Snapshot(t *testing.T) {
	m := NewAttesterCapManager(*DefaultAttestorCapConfig())
	m.ValidateAttestation(&CappedAttestation{Data: make([]byte, 100)})

	stats := m.GetStats()
	// Mutating returned stats should not affect manager state.
	stats.Count = 999

	stats2 := m.GetStats()
	if stats2.Count != 1 {
		t.Errorf("GetStats should return a copy, got Count=%d", stats2.Count)
	}
}
