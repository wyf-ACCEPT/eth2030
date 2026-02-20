package consensus

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// makeAggTestAtt creates an AggregateAttestation with the given slot and bits.
func makeAggTestAtt(slot Slot, bits []byte) *AggregateAttestation {
	sig := [96]byte{0x01, 0x02, 0x03}
	return &AggregateAttestation{
		Data: AttestationData{
			Slot:            slot,
			BeaconBlockRoot: types.Hash{0xAA},
			Source:          Checkpoint{Epoch: 0, Root: types.Hash{0xBB}},
			Target:          Checkpoint{Epoch: 1, Root: types.Hash{0xCC}},
		},
		AggregationBits: bits,
		Signature:       sig,
	}
}

// makeAggPoolTestAtt creates an Attestation suitable for AddAttestation.
func makeAggPoolTestAtt(slot Slot, bits []byte) *Attestation {
	sig := [96]byte{0x01, 0x02, 0x03}
	return &Attestation{
		Data: AttestationData{
			Slot:            slot,
			BeaconBlockRoot: types.Hash{0xAA},
			Source:          Checkpoint{Epoch: 0, Root: types.Hash{0xBB}},
			Target:          Checkpoint{Epoch: 1, Root: types.Hash{0xCC}},
		},
		AggregationBits: bits,
		CommitteeBits:   []byte{0x01},
		Signature:       sig,
	}
}

func TestBitfieldOR(t *testing.T) {
	tests := []struct {
		name string
		a, b []byte
		want []byte
	}{
		{"both_empty", nil, nil, []byte{}},
		{"a_empty", nil, []byte{0xFF}, []byte{0xFF}},
		{"b_empty", []byte{0xFF}, nil, []byte{0xFF}},
		{"same_length", []byte{0x0F}, []byte{0xF0}, []byte{0xFF}},
		{"different_length", []byte{0x01, 0x02}, []byte{0x04}, []byte{0x05, 0x02}},
		{"no_overlap", []byte{0xAA}, []byte{0x55}, []byte{0xFF}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BitfieldOR(tt.a, tt.b)
			if len(got) != len(tt.want) {
				t.Fatalf("length mismatch: got %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("byte %d: got 0x%02X, want 0x%02X", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestBitfieldOverlaps(t *testing.T) {
	tests := []struct {
		name string
		a, b []byte
		want bool
	}{
		{"both_nil", nil, nil, false},
		{"no_overlap", []byte{0xAA}, []byte{0x55}, false},
		{"overlap", []byte{0x0F}, []byte{0x01}, true},
		{"different_length_overlap", []byte{0x01, 0x00}, []byte{0x01}, true},
		{"different_length_no_overlap", []byte{0x00, 0x01}, []byte{0x01, 0x00}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BitfieldOverlaps(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBitfieldAND(t *testing.T) {
	got := BitfieldAND([]byte{0xFF, 0x0F}, []byte{0x0F})
	if len(got) != 1 || got[0] != 0x0F {
		t.Errorf("expected [0x0F], got %v", got)
	}
}

func TestCountBits(t *testing.T) {
	tests := []struct {
		name     string
		bitfield []byte
		want     int
	}{
		{"empty", nil, 0},
		{"all_zeros", []byte{0x00, 0x00}, 0},
		{"all_ones", []byte{0xFF}, 8},
		{"mixed", []byte{0xAA, 0x55}, 8},
		{"single_bit", []byte{0x01}, 1},
		{"multi_byte", []byte{0x01, 0x02, 0x04}, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CountBits(tt.bitfield)
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBitfieldEqual(t *testing.T) {
	if !BitfieldEqual([]byte{0x01, 0x02}, []byte{0x01, 0x02}) {
		t.Error("expected equal")
	}
	if BitfieldEqual([]byte{0x01}, []byte{0x02}) {
		t.Error("expected not equal")
	}
	if BitfieldEqual([]byte{0x01}, []byte{0x01, 0x00}) {
		t.Error("expected not equal (different length)")
	}
}

func TestSetBitGetBit(t *testing.T) {
	var bits []byte
	bits = SetBit(bits, 0)
	bits = SetBit(bits, 7)
	bits = SetBit(bits, 8)
	bits = SetBit(bits, 15)

	if !GetBit(bits, 0) {
		t.Error("bit 0 should be set")
	}
	if !GetBit(bits, 7) {
		t.Error("bit 7 should be set")
	}
	if !GetBit(bits, 8) {
		t.Error("bit 8 should be set")
	}
	if !GetBit(bits, 15) {
		t.Error("bit 15 should be set")
	}
	if GetBit(bits, 1) {
		t.Error("bit 1 should not be set")
	}
	if GetBit(bits, 100) {
		t.Error("bit 100 should not be set (out of range)")
	}
}

func TestTryAggregate(t *testing.T) {
	att1 := makeAggTestAtt(10, []byte{0x0F})
	att2 := makeAggTestAtt(10, []byte{0xF0})

	merged, ok := TryAggregate(att1, att2)
	if !ok {
		t.Fatal("expected successful aggregation")
	}
	if !BitfieldEqual(merged.AggregationBits, []byte{0xFF}) {
		t.Errorf("expected 0xFF bits, got %v", merged.AggregationBits)
	}
	if CountBits(merged.AggregationBits) != 8 {
		t.Errorf("expected 8 bits, got %d", CountBits(merged.AggregationBits))
	}
}

func TestTryAggregateOverlapping(t *testing.T) {
	att1 := makeAggTestAtt(10, []byte{0x0F})
	att2 := makeAggTestAtt(10, []byte{0x03})
	_, ok := TryAggregate(att1, att2)
	if ok {
		t.Error("expected aggregation to fail due to overlapping bits")
	}
}

func TestTryAggregateDataMismatch(t *testing.T) {
	att1 := makeAggTestAtt(10, []byte{0x0F})
	att2 := makeAggTestAtt(11, []byte{0xF0})
	_, ok := TryAggregate(att1, att2)
	if ok {
		t.Error("expected aggregation to fail due to data mismatch")
	}
}

func TestTryAggregateNil(t *testing.T) {
	att1 := makeAggTestAtt(10, []byte{0x0F})
	_, ok := TryAggregate(nil, att1)
	if ok {
		t.Error("expected nil to fail")
	}
	_, ok = TryAggregate(att1, nil)
	if ok {
		t.Error("expected nil to fail")
	}
}

func TestAggregationPoolAddAndGet(t *testing.T) {
	pool := NewAggregationPool(nil)
	att1 := makeAggPoolTestAtt(10, []byte{0x01})
	att2 := makeAggPoolTestAtt(10, []byte{0x02})

	if err := pool.AddAttestation(att1); err != nil {
		t.Fatalf("failed to add att1: %v", err)
	}
	if err := pool.AddAttestation(att2); err != nil {
		t.Fatalf("failed to add att2: %v", err)
	}

	aggs := pool.GetAggregates(10)
	if len(aggs) != 1 {
		t.Fatalf("expected 1 aggregate, got %d", len(aggs))
	}
	if CountBits(aggs[0].AggregationBits) != 2 {
		t.Errorf("expected 2 bits, got %d", CountBits(aggs[0].AggregationBits))
	}
}

func TestAggregationPoolAddNil(t *testing.T) {
	pool := NewAggregationPool(nil)
	if err := pool.AddAttestation(nil); err != ErrAggAttNil {
		t.Errorf("expected ErrAggAttNil, got %v", err)
	}
}

func TestAggregationPoolAddEmptyBits(t *testing.T) {
	pool := NewAggregationPool(nil)
	att := makeAggPoolTestAtt(10, []byte{})
	if err := pool.AddAttestation(att); err != ErrAggAttEmptyBits {
		t.Errorf("expected ErrAggAttEmptyBits, got %v", err)
	}
}

func TestAggregationPoolAddEmptySignature(t *testing.T) {
	pool := NewAggregationPool(nil)
	att := makeAggPoolTestAtt(10, []byte{0x01})
	att.Signature = [96]byte{}
	if err := pool.AddAttestation(att); err != ErrAggAttEmptySig {
		t.Errorf("expected ErrAggAttEmptySig, got %v", err)
	}
}

func TestAggregateAll(t *testing.T) {
	pool := NewAggregationPool(nil)
	for i := 0; i < 4; i++ {
		bits := []byte{1 << uint(i)}
		att := makeAggPoolTestAtt(5, bits)
		if err := pool.AddAttestation(att); err != nil {
			t.Fatalf("failed to add attestation %d: %v", i, err)
		}
	}

	result := pool.AggregateAll(5)
	if len(result) != 1 {
		t.Fatalf("expected 1 aggregate after AggregateAll, got %d", len(result))
	}
	if CountBits(result[0].AggregationBits) != 4 {
		t.Errorf("expected 4 bits, got %d", CountBits(result[0].AggregationBits))
	}
}

func TestAggregateAllMultipleGroups(t *testing.T) {
	pool := NewAggregationPool(nil)
	att1 := makeAggPoolTestAtt(5, []byte{0x01})
	if err := pool.AddAttestation(att1); err != nil {
		t.Fatal(err)
	}

	att2 := &Attestation{
		Data: AttestationData{
			Slot: 5, BeaconBlockRoot: types.Hash{0xDD},
			Source: Checkpoint{Epoch: 0, Root: types.Hash{0xBB}},
			Target: Checkpoint{Epoch: 1, Root: types.Hash{0xCC}},
		},
		AggregationBits: []byte{0x02},
		CommitteeBits:   []byte{0x01},
		Signature:       [96]byte{0x04, 0x05, 0x06},
	}
	if err := pool.AddAttestation(att2); err != nil {
		t.Fatal(err)
	}

	result := pool.AggregateAll(5)
	if len(result) != 2 {
		t.Fatalf("expected 2 aggregates, got %d", len(result))
	}
}

func TestAggregationPoolPruneOld(t *testing.T) {
	pool := NewAggregationPool(nil)
	att1 := makeAggPoolTestAtt(5, []byte{0x01})
	pool.AddAttestation(att1)

	att2 := makeAggPoolTestAtt(90, []byte{0x02})
	att2.Data.Slot = 90
	pool.AddAttestation(att2)

	if pool.TotalCount() != 2 {
		t.Fatalf("expected 2, got %d", pool.TotalCount())
	}

	// Prune with currentSlot=100, maxAge=20. Cutoff=80.
	// Slot 5 < 80 (pruned), slot 90 >= 80 (retained).
	pool.PruneOld(100, 20)

	if pool.SlotCount(5) != 0 {
		t.Error("slot 5 should have been pruned")
	}
	if pool.SlotCount(90) != 1 {
		t.Error("slot 90 should remain")
	}
}

func TestAggregationPoolSlotCapacity(t *testing.T) {
	cfg := &AggregationPoolConfig{MaxPerSlot: 2, MaxAge: 64}
	pool := NewAggregationPool(cfg)

	att1 := makeAggPoolTestAtt(10, []byte{0x01})
	att2 := makeAggPoolTestAtt(10, []byte{0x01})
	att2.Data.BeaconBlockRoot = types.Hash{0xDD}

	pool.AddAttestation(att1)
	pool.AddAttestation(att2)

	att3 := makeAggPoolTestAtt(10, []byte{0x01})
	att3.Data.BeaconBlockRoot = types.Hash{0xEE}
	if err := pool.AddAttestation(att3); err != ErrAggAttSlotFull {
		t.Errorf("expected ErrAggAttSlotFull, got %v", err)
	}
}

func TestHashAggregateAttestation(t *testing.T) {
	att1 := makeAggTestAtt(10, []byte{0x0F})
	att2 := makeAggTestAtt(10, []byte{0x0F})
	att3 := makeAggTestAtt(10, []byte{0xF0})

	h1 := HashAggregateAttestation(att1)
	h2 := HashAggregateAttestation(att2)
	h3 := HashAggregateAttestation(att3)

	if h1 != h2 {
		t.Error("identical attestations should have same hash")
	}
	if h1 == h3 {
		t.Error("different attestations should have different hash")
	}
}

func TestAggregationPoolEmptySlot(t *testing.T) {
	pool := NewAggregationPool(nil)
	if pool.AggregateAll(999) != nil {
		t.Error("expected nil for empty slot")
	}
	if pool.GetAggregates(999) != nil {
		t.Error("expected nil for empty slot")
	}
}
