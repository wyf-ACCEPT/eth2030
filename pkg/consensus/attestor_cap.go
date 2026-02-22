package consensus

// 1MiB Attestor Cap enforces maximum attestation sizes on the consensus layer.
// This prevents oversized attestations from consuming excessive bandwidth and
// storage, targeting a per-attestation limit of 1MiB (1048576 bytes).
// Part of the Consensus Layer accessibility roadmap.

import (
	"errors"
	"sync"
)

// Attestor cap constants.
const (
	// DefaultMaxAttestationSize is the default maximum attestation size: 1MiB.
	DefaultMaxAttestationSize uint64 = 1 << 20 // 1048576 bytes

	// DefaultMaxAggregateSize is the default max aggregate attestation size.
	// Aggregates may be slightly larger to accommodate combined bits/sigs.
	DefaultMaxAggregateSize uint64 = 2 << 20 // 2MiB

	// DefaultMinParticipation is the minimum fraction (basis points) of
	// aggregation bits that must be set for a valid aggregate. 1000 = 10%.
	DefaultMinParticipation uint64 = 1000

	// BasisPoints denominator for participation rate.
	basisPoints uint64 = 10000

	// attestorCapOverhead accounts for fixed-size fields in a CappedAttestation:
	// ValidatorIndex(8) + Slot(8) + CommitteeIndex(8) = 24 bytes header overhead.
	attestorCapOverhead uint64 = 24
)

// Attestor cap errors.
var (
	ErrAttestorNil              = errors.New("attestor_cap: nil attestation")
	ErrAttestorOversized        = errors.New("attestor_cap: attestation exceeds size cap")
	ErrAttestorAggOversized     = errors.New("attestor_cap: aggregate exceeds size cap")
	ErrAttestorNoAttestations   = errors.New("attestor_cap: no attestations to aggregate")
	ErrAttestorDataTooLarge     = errors.New("attestor_cap: data field too large to trim")
	ErrAttestorEmptyAggBits     = errors.New("attestor_cap: empty aggregation bits")
	ErrAttestorLowParticipation = errors.New("attestor_cap: aggregate participation below minimum")
)

// AttestorCapConfig configures the 1MiB attestor cap enforcement.
type AttestorCapConfig struct {
	MaxAttestationSize uint64 // max single attestation size in bytes
	MaxAggregateSize   uint64 // max aggregate attestation size in bytes
	MinParticipation   uint64 // min participation rate in basis points (e.g., 1000 = 10%)
}

// DefaultAttestorCapConfig returns the default 1MiB attestor cap configuration.
func DefaultAttestorCapConfig() *AttestorCapConfig {
	return &AttestorCapConfig{
		MaxAttestationSize: DefaultMaxAttestationSize,
		MaxAggregateSize:   DefaultMaxAggregateSize,
		MinParticipation:   DefaultMinParticipation,
	}
}

// CappedAttestation is an attestation subject to size cap enforcement.
type CappedAttestation struct {
	ValidatorIndex  uint64
	Slot            uint64
	CommitteeIndex  uint64
	Data            []byte
	Signature       []byte
	AggregationBits []byte
}

// AttestationStats tracks attestation size metrics.
type AttestationStats struct {
	TotalSize uint64 // cumulative size of all validated attestations
	Count     uint64 // number of validated attestations
	AvgSize   uint64 // average attestation size
	MaxSize   uint64 // largest attestation observed
	Oversized uint64 // number of oversized attestations rejected
}

// AttesterCapManager enforces attestation size limits. Thread-safe.
type AttesterCapManager struct {
	mu     sync.Mutex
	config *AttestorCapConfig
	stats  AttestationStats
}

// NewAttesterCapManager creates a manager with the given config.
func NewAttesterCapManager(config AttestorCapConfig) *AttesterCapManager {
	cfg := config
	if cfg.MaxAttestationSize == 0 {
		cfg.MaxAttestationSize = DefaultMaxAttestationSize
	}
	if cfg.MaxAggregateSize == 0 {
		cfg.MaxAggregateSize = DefaultMaxAggregateSize
	}
	return &AttesterCapManager{config: &cfg}
}

// EstimateSize returns the estimated serialized size of an attestation in bytes.
// The estimate includes the fixed-size header fields plus the variable-length
// Data, Signature, and AggregationBits fields.
func EstimateSize(att *CappedAttestation) uint64 {
	if att == nil {
		return 0
	}
	return attestorCapOverhead +
		uint64(len(att.Data)) +
		uint64(len(att.Signature)) +
		uint64(len(att.AggregationBits))
}

// ValidateAttestation checks that an attestation fits within the size cap.
// It also updates internal statistics.
func (m *AttesterCapManager) ValidateAttestation(att *CappedAttestation) error {
	if att == nil {
		return ErrAttestorNil
	}

	size := EstimateSize(att)

	m.mu.Lock()
	defer m.mu.Unlock()

	if size > m.config.MaxAttestationSize {
		m.stats.Oversized++
		return ErrAttestorOversized
	}

	// Update stats for valid attestations.
	m.stats.Count++
	m.stats.TotalSize += size
	if size > m.stats.MaxSize {
		m.stats.MaxSize = size
	}
	if m.stats.Count > 0 {
		m.stats.AvgSize = m.stats.TotalSize / m.stats.Count
	}

	return nil
}

// TrimAttestation trims an attestation's Data field so the total estimated
// size fits within maxSize. If the attestation already fits, it is returned
// unchanged. If the fixed fields plus Signature and AggregationBits already
// exceed maxSize, an error is returned.
func TrimAttestation(att *CappedAttestation, maxSize uint64) (*CappedAttestation, error) {
	if att == nil {
		return nil, ErrAttestorNil
	}

	size := EstimateSize(att)
	if size <= maxSize {
		return att, nil
	}

	// Calculate space available for the Data field.
	fixedSize := attestorCapOverhead +
		uint64(len(att.Signature)) +
		uint64(len(att.AggregationBits))
	if fixedSize >= maxSize {
		return nil, ErrAttestorDataTooLarge
	}
	maxData := maxSize - fixedSize

	// Create a trimmed copy (do not mutate original).
	trimmed := &CappedAttestation{
		ValidatorIndex:  att.ValidatorIndex,
		Slot:            att.Slot,
		CommitteeIndex:  att.CommitteeIndex,
		Signature:       att.Signature,
		AggregationBits: att.AggregationBits,
	}
	if uint64(len(att.Data)) > maxData {
		trimmed.Data = make([]byte, maxData)
		copy(trimmed.Data, att.Data[:maxData])
	} else {
		trimmed.Data = att.Data
	}

	return trimmed, nil
}

// AggregateAttestations combines multiple attestations into one aggregate,
// merging their AggregationBits by OR-ing them together. All attestations
// must target the same Slot and CommitteeIndex. The resulting aggregate is
// validated against the aggregate size cap.
func (m *AttesterCapManager) AggregateAttestations(atts []*CappedAttestation) (*CappedAttestation, error) {
	if len(atts) == 0 {
		return nil, ErrAttestorNoAttestations
	}
	if len(atts) == 1 {
		return atts[0], nil
	}

	base := atts[0]

	// Determine max aggregation bits length and total data length.
	maxBitsLen := len(base.AggregationBits)
	totalDataLen := 0
	for _, att := range atts {
		if len(att.AggregationBits) > maxBitsLen {
			maxBitsLen = len(att.AggregationBits)
		}
		totalDataLen += len(att.Data)
	}

	// OR aggregation bits together.
	mergedBits := make([]byte, maxBitsLen)
	for _, att := range atts {
		for i, b := range att.AggregationBits {
			mergedBits[i] |= b
		}
	}

	// Concatenate data from all attestations.
	mergedData := make([]byte, 0, totalDataLen)
	for _, att := range atts {
		mergedData = append(mergedData, att.Data...)
	}

	// Use the first attestation's signature as the aggregate placeholder.
	aggregate := &CappedAttestation{
		ValidatorIndex:  base.ValidatorIndex,
		Slot:            base.Slot,
		CommitteeIndex:  base.CommitteeIndex,
		Data:            mergedData,
		Signature:       base.Signature,
		AggregationBits: mergedBits,
	}

	// Validate aggregate size.
	if err := m.ValidateAggregateSize(aggregate); err != nil {
		return nil, err
	}

	return aggregate, nil
}

// ValidateAggregateSize checks that an aggregate attestation fits within
// the aggregate size cap and has sufficient participation.
func (m *AttesterCapManager) ValidateAggregateSize(aggregate *CappedAttestation) error {
	if aggregate == nil {
		return ErrAttestorNil
	}

	size := EstimateSize(aggregate)
	if size > m.config.MaxAggregateSize {
		return ErrAttestorAggOversized
	}

	// Check minimum participation if aggregation bits are present.
	if len(aggregate.AggregationBits) > 0 && m.config.MinParticipation > 0 {
		setBits := countSetBits(aggregate.AggregationBits)
		totalBits := uint64(len(aggregate.AggregationBits)) * 8
		if totalBits > 0 {
			rate := (setBits * basisPoints) / totalBits
			if rate < m.config.MinParticipation {
				return ErrAttestorLowParticipation
			}
		}
	}

	return nil
}

// GetStats returns a snapshot of current attestation statistics.
func (m *AttesterCapManager) GetStats() *AttestationStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.stats // copy
	return &s
}

// ResetStats clears all accumulated statistics.
func (m *AttesterCapManager) ResetStats() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stats = AttestationStats{}
}

// countSetBits returns the number of set bits in a byte slice.
func countSetBits(bits []byte) uint64 {
	var count uint64
	for _, b := range bits {
		// Kernighan's bit counting.
		v := b
		for v != 0 {
			v &= v - 1
			count++
		}
	}
	return count
}
