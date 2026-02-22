package consensus

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// makeSubnetAtt creates a test aggregate attestation for subnet tests.
func makeSubnetAtt(slot uint64, bitPos int) *AggregateAttestation {
	bits := make([]byte, (bitPos/8)+1)
	bits[bitPos/8] |= 1 << (bitPos % 8)

	var sig [96]byte
	sig[0] = byte(bitPos)
	sig[1] = byte(slot)

	return &AggregateAttestation{
		Data: AttestationData{
			Slot:            Slot(slot),
			BeaconBlockRoot: types.Hash{0xaa},
			Source:          Checkpoint{Epoch: 1, Root: types.Hash{0xbb}},
			Target:          Checkpoint{Epoch: 2, Root: types.Hash{0xcc}},
		},
		AggregationBits: bits,
		Signature:       sig,
	}
}

func TestCommitteeSubnet_NewSubnetRouter(t *testing.T) {
	router := NewSubnetRouter(nil)
	if router == nil {
		t.Fatal("expected non-nil router")
	}
	if router.SubnetCount() != DefaultSubnetCount {
		t.Errorf("expected %d subnets, got %d", DefaultSubnetCount, router.SubnetCount())
	}
}

func TestCommitteeSubnet_CustomConfig(t *testing.T) {
	cfg := &SubnetConfig{SubnetCount: 16, MaxPendingPerSlot: 100}
	router := NewSubnetRouter(cfg)
	if router.SubnetCount() != 16 {
		t.Errorf("expected 16 subnets, got %d", router.SubnetCount())
	}
}

func TestCommitteeSubnet_RouteAttestation(t *testing.T) {
	router := NewSubnetRouter(&SubnetConfig{
		SubnetCount:       8,
		MaxPendingPerSlot: 100,
	})

	att := makeSubnetAtt(1, 0)

	// Route to committee index 3 -> subnet 3.
	err := router.RouteAttestation(att, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := router.GetSubnet(3)
	if s == nil {
		t.Fatal("expected subnet 3 to exist")
	}
	if s.PendingCount(Slot(1)) != 1 {
		t.Errorf("expected 1 pending, got %d", s.PendingCount(Slot(1)))
	}
}

func TestCommitteeSubnet_RouteNilAttestation(t *testing.T) {
	router := NewSubnetRouter(nil)
	err := router.RouteAttestation(nil, 0)
	if err != ErrSubnetNilAtt {
		t.Errorf("expected ErrSubnetNilAtt, got %v", err)
	}
}

func TestCommitteeSubnet_RouteToClosedRouter(t *testing.T) {
	router := NewSubnetRouter(nil)
	router.Close()

	att := makeSubnetAtt(1, 0)
	err := router.RouteAttestation(att, 0)
	if err != ErrSubnetRouterClosed {
		t.Errorf("expected ErrSubnetRouterClosed, got %v", err)
	}
}

func TestCommitteeSubnet_SubnetWraparound(t *testing.T) {
	router := NewSubnetRouter(&SubnetConfig{
		SubnetCount:       8,
		MaxPendingPerSlot: 100,
	})

	att := makeSubnetAtt(1, 0)
	// Committee 10 wraps to subnet 2 (10 % 8 = 2).
	err := router.RouteAttestation(att, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := router.GetSubnet(2)
	if s.PendingCount(Slot(1)) != 1 {
		t.Errorf("expected 1 pending at subnet 2, got %d", s.PendingCount(Slot(1)))
	}
}

func TestCommitteeSubnet_SubnetFull(t *testing.T) {
	router := NewSubnetRouter(&SubnetConfig{
		SubnetCount:       4,
		MaxPendingPerSlot: 2,
	})

	att1 := makeSubnetAtt(1, 0)
	att2 := makeSubnetAtt(1, 1)
	att3 := makeSubnetAtt(1, 2)

	_ = router.RouteAttestation(att1, 0)
	_ = router.RouteAttestation(att2, 0)
	err := router.RouteAttestation(att3, 0)
	if err != ErrSubnetFull {
		t.Errorf("expected ErrSubnetFull, got %v", err)
	}
}

func TestCommitteeSubnet_AggregateSlot(t *testing.T) {
	router := NewSubnetRouter(&SubnetConfig{
		SubnetCount:       4,
		MaxPendingPerSlot: 100,
	})

	// Add 4 attestations to subnet 0.
	for i := 0; i < 4; i++ {
		att := makeSubnetAtt(1, i)
		err := router.RouteAttestation(att, 0) // all to subnet 0
		if err != nil {
			t.Fatalf("unexpected error at %d: %v", i, err)
		}
	}

	s := router.GetSubnet(0)
	agg, err := s.AggregateSlot(Slot(1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	bits := CountBits(agg.AggregationBits)
	if bits < 2 {
		t.Errorf("expected at least 2 bits after aggregation, got %d", bits)
	}
}

func TestCommitteeSubnet_AggregateEmptySlot(t *testing.T) {
	s := newSubnet(0, DefaultSubnetConfig())
	_, err := s.AggregateSlot(Slot(99))
	if err != ErrSubnetNoAtts {
		t.Errorf("expected ErrSubnetNoAtts, got %v", err)
	}
}

func TestCommitteeSubnet_CrossSubnetAggregate(t *testing.T) {
	router := NewSubnetRouter(&SubnetConfig{
		SubnetCount:       4,
		MaxPendingPerSlot: 100,
	})

	// Route attestations across different subnets.
	for i := 0; i < 4; i++ {
		att := makeSubnetAtt(1, i)
		err := router.RouteAttestation(att, uint64(i))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	agg, err := router.CrossSubnetAggregate(Slot(1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	bits := CountBits(agg.AggregationBits)
	if bits < 2 {
		t.Errorf("expected at least 2 bits after cross-subnet aggregation, got %d", bits)
	}
}

func TestCommitteeSubnet_CrossSubnetNoAtts(t *testing.T) {
	router := NewSubnetRouter(nil)
	_, err := router.CrossSubnetAggregate(Slot(99))
	if err != ErrSubnetNoAtts {
		t.Errorf("expected ErrSubnetNoAtts, got %v", err)
	}
}

func TestCommitteeSubnet_AggregateSubnets(t *testing.T) {
	router := NewSubnetRouter(&SubnetConfig{
		SubnetCount:       4,
		MaxPendingPerSlot: 100,
	})

	// Route to 2 subnets.
	_ = router.RouteAttestation(makeSubnetAtt(1, 0), 0)
	_ = router.RouteAttestation(makeSubnetAtt(1, 1), 2)

	aggs := router.AggregateSubnets(Slot(1))
	if len(aggs) != 2 {
		t.Errorf("expected 2 subnet aggregates, got %d", len(aggs))
	}
}

func TestCommitteeSubnet_PruneSlot(t *testing.T) {
	router := NewSubnetRouter(&SubnetConfig{
		SubnetCount:       4,
		MaxPendingPerSlot: 100,
	})

	_ = router.RouteAttestation(makeSubnetAtt(1, 0), 0)
	_ = router.RouteAttestation(makeSubnetAtt(1, 1), 1)

	router.PruneSlot(Slot(1))

	s := router.GetSubnet(0)
	if s.PendingCount(Slot(1)) != 0 {
		t.Errorf("expected 0 pending after prune, got %d", s.PendingCount(Slot(1)))
	}
}

func TestCommitteeSubnet_Health(t *testing.T) {
	router := NewSubnetRouter(&SubnetConfig{
		SubnetCount:       4,
		MaxPendingPerSlot: 100,
	})

	_ = router.RouteAttestation(makeSubnetAtt(1, 0), 0)
	_ = router.RouteAttestation(makeSubnetAtt(1, 1), 0)

	health := router.AllHealth()
	if len(health) != 4 {
		t.Errorf("expected 4 health entries, got %d", len(health))
	}

	// Subnet 0 should have 2 pending.
	if health[0].PendingCount != 2 {
		t.Errorf("expected 2 pending for subnet 0, got %d", health[0].PendingCount)
	}
}

func TestCommitteeSubnet_ValidatorSubnet(t *testing.T) {
	tests := []struct {
		committee uint64
		subnets   int
		expected  uint64
	}{
		{0, 64, 0},
		{63, 64, 63},
		{64, 64, 0},
		{65, 64, 1},
		{0, 0, 0},
	}

	for _, tt := range tests {
		got := ValidatorSubnet(tt.committee, tt.subnets)
		if got != tt.expected {
			t.Errorf("ValidatorSubnet(%d, %d) = %d, want %d",
				tt.committee, tt.subnets, got, tt.expected)
		}
	}
}

func TestCommitteeSubnet_GetSubnetInvalid(t *testing.T) {
	router := NewSubnetRouter(&SubnetConfig{
		SubnetCount:       4,
		MaxPendingPerSlot: 100,
	})

	s := router.GetSubnet(10)
	if s != nil {
		t.Error("expected nil for invalid subnet ID")
	}
}

func TestCommitteeSubnet_GetAggregate(t *testing.T) {
	s := newSubnet(0, DefaultSubnetConfig())
	att := makeSubnetAtt(1, 0)
	_ = s.AddAttestation(att)
	_, _ = s.AggregateSlot(Slot(1))

	agg := s.GetAggregate(Slot(1))
	if agg == nil {
		t.Error("expected non-nil aggregate")
	}

	// No aggregate for unprocessed slot.
	noAgg := s.GetAggregate(Slot(99))
	if noAgg != nil {
		t.Error("expected nil for unprocessed slot")
	}
}
