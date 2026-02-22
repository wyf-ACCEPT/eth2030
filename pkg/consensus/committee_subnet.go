// Subnet-based attestation distribution for 1M attestation scale.
//
// Implements 64 subnets (matching Ethereum spec) where each validator is
// assigned based on committee index. A SubnetRouter routes attestations to
// the correct subnet, and cross-subnet aggregation produces one aggregate
// per slot per subnet. Includes subnet health tracking.
package consensus

import (
	"errors"
	"sync"
	"sync/atomic"
)

// Subnet constants matching the Ethereum consensus spec.
const (
	// DefaultSubnetCount is the number of attestation subnets (per spec).
	DefaultSubnetCount = 64

	// DefaultValidatorsPerSubnet is the default expected validators per subnet.
	DefaultValidatorsPerSubnet = 16384

	// SubnetMaxPendingPerSlot caps pending attestations per slot per subnet.
	SubnetMaxPendingPerSlot = 32768
)

// Subnet errors.
var (
	ErrSubnetNilAtt       = errors.New("committee_subnet: nil attestation")
	ErrSubnetInvalidID    = errors.New("committee_subnet: invalid subnet ID")
	ErrSubnetFull         = errors.New("committee_subnet: subnet pending queue full")
	ErrSubnetNoAtts       = errors.New("committee_subnet: no attestations to aggregate")
	ErrSubnetRouterClosed = errors.New("committee_subnet: router is closed")
)

// SubnetHealth tracks the health metrics of a single subnet.
type SubnetHealth struct {
	SubnetID         uint64
	ActiveValidators int
	PendingCount     int
	AggregatedCount  int
	MessageRate      float64 // attestations per slot (exponential moving average)
}

// SubnetConfig configures the subnet router.
type SubnetConfig struct {
	SubnetCount         int // number of subnets (default 64)
	ValidatorsPerSubnet int // expected validators per subnet
	MaxPendingPerSlot   int // max pending attestations per slot per subnet
}

// DefaultSubnetConfig returns the default subnet configuration.
func DefaultSubnetConfig() *SubnetConfig {
	return &SubnetConfig{
		SubnetCount:         DefaultSubnetCount,
		ValidatorsPerSubnet: DefaultValidatorsPerSubnet,
		MaxPendingPerSlot:   SubnetMaxPendingPerSlot,
	}
}

// Subnet represents a single attestation subnet that collects and
// aggregates attestations from its assigned validators.
type Subnet struct {
	ID uint64
	mu sync.Mutex
	// pending holds attestations indexed by slot.
	pending map[Slot][]*AggregateAttestation
	// aggregates holds the per-slot aggregate output.
	aggregates map[Slot]*AggregateAttestation
	// health tracks subnet metrics.
	health SubnetHealth
	config *SubnetConfig
}

// newSubnet creates a new subnet with the given ID.
func newSubnet(id uint64, cfg *SubnetConfig) *Subnet {
	return &Subnet{
		ID:         id,
		pending:    make(map[Slot][]*AggregateAttestation),
		aggregates: make(map[Slot]*AggregateAttestation),
		health: SubnetHealth{
			SubnetID: id,
		},
		config: cfg,
	}
}

// AddAttestation adds an attestation to this subnet's pending queue.
func (s *Subnet) AddAttestation(att *AggregateAttestation) error {
	if att == nil {
		return ErrSubnetNilAtt
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	slot := att.Data.Slot
	if len(s.pending[slot]) >= s.config.MaxPendingPerSlot {
		return ErrSubnetFull
	}

	s.pending[slot] = append(s.pending[slot], copyAggregateAttestation(att))
	s.health.PendingCount++

	// Update moving average: simple EMA with alpha=0.1.
	s.health.MessageRate = s.health.MessageRate*0.9 + 0.1
	return nil
}

// AggregateSlot produces a single aggregate attestation for the given slot
// by greedily merging non-overlapping attestations. Returns the aggregate
// or an error if no attestations are available.
func (s *Subnet) AggregateSlot(slot Slot) (*AggregateAttestation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	atts := s.pending[slot]
	if len(atts) == 0 {
		return nil, ErrSubnetNoAtts
	}

	// Greedy aggregation: take the first, merge in non-overlapping ones.
	result := copyAggregateAttestation(atts[0])
	for i := 1; i < len(atts); i++ {
		if !BitfieldOverlaps(result.AggregationBits, atts[i].AggregationBits) {
			result.AggregationBits = BitfieldOR(result.AggregationBits, atts[i].AggregationBits)
			result.Signature = aggregateSigPair(result.Signature, atts[i].Signature)
		}
	}

	s.aggregates[slot] = result
	s.health.AggregatedCount++
	return copyAggregateAttestation(result), nil
}

// GetAggregate returns the aggregate for a slot, if available.
func (s *Subnet) GetAggregate(slot Slot) *AggregateAttestation {
	s.mu.Lock()
	defer s.mu.Unlock()

	agg, ok := s.aggregates[slot]
	if !ok {
		return nil
	}
	return copyAggregateAttestation(agg)
}

// PendingCount returns the number of pending attestations for a slot.
func (s *Subnet) PendingCount(slot Slot) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pending[slot])
}

// Health returns the current subnet health metrics.
func (s *Subnet) Health() SubnetHealth {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.health
	h.PendingCount = 0
	for _, atts := range s.pending {
		h.PendingCount += len(atts)
	}
	return h
}

// PruneSlot removes pending and aggregate data for a given slot.
func (s *Subnet) PruneSlot(slot Slot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := len(s.pending[slot])
	delete(s.pending, slot)
	delete(s.aggregates, slot)
	s.health.PendingCount -= removed
	if s.health.PendingCount < 0 {
		s.health.PendingCount = 0
	}
}

// SubnetRouter routes attestations to subnets and orchestrates
// cross-subnet aggregation. Thread-safe.
type SubnetRouter struct {
	subnets []*Subnet
	config  *SubnetConfig
	closed  atomic.Bool
}

// NewSubnetRouter creates a new subnet router with the configured number
// of subnets.
func NewSubnetRouter(cfg *SubnetConfig) *SubnetRouter {
	if cfg == nil {
		cfg = DefaultSubnetConfig()
	}
	if cfg.SubnetCount < 1 {
		cfg.SubnetCount = DefaultSubnetCount
	}
	if cfg.MaxPendingPerSlot < 1 {
		cfg.MaxPendingPerSlot = SubnetMaxPendingPerSlot
	}

	subnets := make([]*Subnet, cfg.SubnetCount)
	for i := 0; i < cfg.SubnetCount; i++ {
		subnets[i] = newSubnet(uint64(i), cfg)
	}

	return &SubnetRouter{
		subnets: subnets,
		config:  cfg,
	}
}

// RouteAttestation routes an attestation to the appropriate subnet based
// on the committee index encoded in the attestation data. The subnet is
// selected as: committeeIndex % subnetCount.
func (r *SubnetRouter) RouteAttestation(att *AggregateAttestation, committeeIndex uint64) error {
	if r.closed.Load() {
		return ErrSubnetRouterClosed
	}
	if att == nil {
		return ErrSubnetNilAtt
	}

	subnetID := committeeIndex % uint64(r.config.SubnetCount)
	return r.subnets[subnetID].AddAttestation(att)
}

// AggregateSubnets produces per-subnet aggregates for a slot.
// Returns one aggregate per subnet that has attestations.
func (r *SubnetRouter) AggregateSubnets(slot Slot) []*AggregateAttestation {
	var results []*AggregateAttestation

	for _, s := range r.subnets {
		agg, err := s.AggregateSlot(slot)
		if err != nil {
			continue
		}
		results = append(results, agg)
	}
	return results
}

// CrossSubnetAggregate performs cross-subnet aggregation by merging all
// subnet aggregates for a slot into a single aggregate. This is the final
// step that produces the block-level aggregate attestation.
func (r *SubnetRouter) CrossSubnetAggregate(slot Slot) (*AggregateAttestation, error) {
	subnetAggs := r.AggregateSubnets(slot)
	if len(subnetAggs) == 0 {
		return nil, ErrSubnetNoAtts
	}

	result := copyAggregateAttestation(subnetAggs[0])
	for i := 1; i < len(subnetAggs); i++ {
		if !IsEqualAttestationData(&result.Data, &subnetAggs[i].Data) {
			continue
		}
		result.AggregationBits = BitfieldOR(result.AggregationBits, subnetAggs[i].AggregationBits)
		result.Signature = aggregateSigPair(result.Signature, subnetAggs[i].Signature)
	}
	return result, nil
}

// GetSubnet returns the subnet for a given ID.
func (r *SubnetRouter) GetSubnet(id uint64) *Subnet {
	if id >= uint64(len(r.subnets)) {
		return nil
	}
	return r.subnets[id]
}

// SubnetCount returns the number of subnets.
func (r *SubnetRouter) SubnetCount() int {
	return len(r.subnets)
}

// AllHealth returns health metrics for all subnets.
func (r *SubnetRouter) AllHealth() []SubnetHealth {
	health := make([]SubnetHealth, len(r.subnets))
	for i, s := range r.subnets {
		health[i] = s.Health()
	}
	return health
}

// PruneSlot removes data for a slot across all subnets.
func (r *SubnetRouter) PruneSlot(slot Slot) {
	for _, s := range r.subnets {
		s.PruneSlot(slot)
	}
}

// Close marks the router as closed.
func (r *SubnetRouter) Close() {
	r.closed.Store(true)
}

// ValidatorSubnet returns the subnet assignment for a validator given
// their committee index. This is a pure function matching the spec:
// subnet = committeeIndex % subnetCount.
func ValidatorSubnet(committeeIndex uint64, subnetCount int) uint64 {
	if subnetCount <= 0 {
		return 0
	}
	return committeeIndex % uint64(subnetCount)
}
