package consensus

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// helper to make a basic indexed attestation.
func makeIndexedAtt(slot, committee uint64, root types.Hash, srcEpoch, tgtEpoch uint64) *IndexedAttestation {
	return &IndexedAttestation{
		Slot:            slot,
		BeaconBlockRoot: root,
		Source:          &Checkpoint7549{Epoch: srcEpoch, Root: root},
		Target:          &Checkpoint7549{Epoch: tgtEpoch, Root: root},
		AggregationBits: []byte{0x01},
		CommitteeIndex:  committee,
	}
}

func TestConvertAttestation_Nil(t *testing.T) {
	if result := ConvertAttestation(nil); result != nil {
		t.Fatal("expected nil for nil legacy attestation")
	}
}

func TestConvertAttestation_Basic(t *testing.T) {
	legacy := &LegacyAttestation{
		Slot:            100,
		CommitteeIndex:  5,
		Data:            []byte{0xab, 0xcd},
		AggregationBits: []byte{0xff, 0x01},
		Signature:       []byte{0x01, 0x02},
	}
	result := ConvertAttestation(legacy)
	if result == nil {
		t.Fatal("expected non-nil indexed attestation")
	}
	if result.Slot != 100 {
		t.Errorf("slot: got %d, want 100", result.Slot)
	}
	if result.CommitteeIndex != 5 {
		t.Errorf("committee index: got %d, want 5", result.CommitteeIndex)
	}
	if len(result.AggregationBits) != 2 || result.AggregationBits[0] != 0xff {
		t.Errorf("aggregation bits mismatch: got %v", result.AggregationBits)
	}
	// Verify deep copy: modifying legacy bits should not affect result.
	legacy.AggregationBits[0] = 0x00
	if result.AggregationBits[0] != 0xff {
		t.Error("aggregation bits were not deep copied")
	}
}

func TestValidateIndexedAttestation_Nil(t *testing.T) {
	if err := ValidateIndexedAttestation(nil, 100); err != ErrIndexedAttNil {
		t.Errorf("expected ErrIndexedAttNil, got %v", err)
	}
}

func TestValidateIndexedAttestation_EmptyBits(t *testing.T) {
	att := &IndexedAttestation{
		Source:          &Checkpoint7549{Epoch: 1},
		Target:          &Checkpoint7549{Epoch: 2},
		AggregationBits: []byte{},
	}
	if err := ValidateIndexedAttestation(att, 100); err != ErrIndexedAttEmptyBits {
		t.Errorf("expected ErrIndexedAttEmptyBits, got %v", err)
	}
}

func TestValidateIndexedAttestation_NilSource(t *testing.T) {
	att := &IndexedAttestation{
		AggregationBits: []byte{0x01},
		Target:          &Checkpoint7549{Epoch: 2},
	}
	if err := ValidateIndexedAttestation(att, 100); err != ErrIndexedAttSourceNil {
		t.Errorf("expected ErrIndexedAttSourceNil, got %v", err)
	}
}

func TestValidateIndexedAttestation_NilTarget(t *testing.T) {
	att := &IndexedAttestation{
		AggregationBits: []byte{0x01},
		Source:          &Checkpoint7549{Epoch: 1},
	}
	if err := ValidateIndexedAttestation(att, 100); err != ErrIndexedAttTargetNil {
		t.Errorf("expected ErrIndexedAttTargetNil, got %v", err)
	}
}

func TestValidateIndexedAttestation_SourceAfterTarget(t *testing.T) {
	att := &IndexedAttestation{
		AggregationBits: []byte{0x01},
		Source:          &Checkpoint7549{Epoch: 10},
		Target:          &Checkpoint7549{Epoch: 5},
	}
	if err := ValidateIndexedAttestation(att, 100); err != ErrIndexedAttSourceAfter {
		t.Errorf("expected ErrIndexedAttSourceAfter, got %v", err)
	}
}

func TestValidateIndexedAttestation_CommitteeOutOfRange(t *testing.T) {
	att := &IndexedAttestation{
		AggregationBits: []byte{0x01},
		Source:          &Checkpoint7549{Epoch: 1},
		Target:          &Checkpoint7549{Epoch: 2},
		CommitteeIndex:  MaxCommitteesPerSlot, // exactly at max, out of range
	}
	if err := ValidateIndexedAttestation(att, 100); err != ErrIndexedAttCommitteeRange {
		t.Errorf("expected ErrIndexedAttCommitteeRange, got %v", err)
	}
}

func TestValidateIndexedAttestation_ValidatorBitsExceedCount(t *testing.T) {
	// 2 bytes = 16 bits, but only 4 validators. Set bit 5.
	att := &IndexedAttestation{
		AggregationBits: []byte{0x20, 0x00}, // bit 5 set
		Source:          &Checkpoint7549{Epoch: 1},
		Target:          &Checkpoint7549{Epoch: 2},
		CommitteeIndex:  0,
	}
	err := ValidateIndexedAttestation(att, 4)
	if err == nil {
		t.Fatal("expected error for bits exceeding validator count")
	}
}

func TestValidateIndexedAttestation_Valid(t *testing.T) {
	att := &IndexedAttestation{
		AggregationBits: []byte{0x07}, // bits 0,1,2
		Source:          &Checkpoint7549{Epoch: 1},
		Target:          &Checkpoint7549{Epoch: 2},
		CommitteeIndex:  3,
	}
	if err := ValidateIndexedAttestation(att, 8); err != nil {
		t.Errorf("expected valid attestation, got error: %v", err)
	}
}

func TestValidateIndexedAttestation_ZeroValidatorCount(t *testing.T) {
	att := &IndexedAttestation{
		AggregationBits: []byte{0xff},
		Source:          &Checkpoint7549{Epoch: 1},
		Target:          &Checkpoint7549{Epoch: 2},
		CommitteeIndex:  0,
	}
	// validatorCount=0 skips the bits range check.
	if err := ValidateIndexedAttestation(att, 0); err != nil {
		t.Errorf("expected no error with zero validator count, got: %v", err)
	}
}

func TestIsAggregatable(t *testing.T) {
	root := types.HexToHash("0xaa")
	a := makeIndexedAtt(10, 0, root, 1, 2)
	b := makeIndexedAtt(10, 1, root, 1, 2)

	if !IsAggregatable(a, b) {
		t.Error("expected attestations with same data to be aggregatable")
	}
}

func TestIsAggregatable_DifferentSlots(t *testing.T) {
	root := types.HexToHash("0xaa")
	a := makeIndexedAtt(10, 0, root, 1, 2)
	b := makeIndexedAtt(11, 1, root, 1, 2)

	if IsAggregatable(a, b) {
		t.Error("expected attestations with different slots to NOT be aggregatable")
	}
}

func TestIsAggregatable_DifferentRoots(t *testing.T) {
	a := makeIndexedAtt(10, 0, types.HexToHash("0xaa"), 1, 2)
	b := makeIndexedAtt(10, 1, types.HexToHash("0xbb"), 1, 2)

	if IsAggregatable(a, b) {
		t.Error("expected attestations with different roots to NOT be aggregatable")
	}
}

func TestIsAggregatable_DifferentSourceEpoch(t *testing.T) {
	root := types.HexToHash("0xaa")
	a := makeIndexedAtt(10, 0, root, 1, 2)
	b := makeIndexedAtt(10, 1, root, 0, 2)

	if IsAggregatable(a, b) {
		t.Error("expected attestations with different source epochs to NOT be aggregatable")
	}
}

func TestIsAggregatable_Nil(t *testing.T) {
	a := makeIndexedAtt(10, 0, types.Hash{}, 1, 2)
	if IsAggregatable(nil, a) {
		t.Error("expected nil to NOT be aggregatable")
	}
	if IsAggregatable(a, nil) {
		t.Error("expected nil to NOT be aggregatable")
	}
}

func TestAggregateIndexedAttestations_Empty(t *testing.T) {
	_, err := AggregateIndexedAttestations(nil)
	if err == nil {
		t.Fatal("expected error for empty attestation slice")
	}
}

func TestAggregateIndexedAttestations_Single(t *testing.T) {
	att := makeIndexedAtt(10, 0, types.Hash{}, 1, 2)
	result, err := AggregateIndexedAttestations([]*IndexedAttestation{att})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != att {
		t.Error("single attestation should return itself")
	}
}

func TestAggregateIndexedAttestations_CrossCommittee(t *testing.T) {
	root := types.HexToHash("0xaa")
	a := &IndexedAttestation{
		Slot:            10,
		BeaconBlockRoot: root,
		Source:          &Checkpoint7549{Epoch: 1, Root: root},
		Target:          &Checkpoint7549{Epoch: 2, Root: root},
		AggregationBits: []byte{0x0f}, // bits 0-3
		CommitteeIndex:  0,
	}
	b := &IndexedAttestation{
		Slot:            10,
		BeaconBlockRoot: root,
		Source:          &Checkpoint7549{Epoch: 1, Root: root},
		Target:          &Checkpoint7549{Epoch: 2, Root: root},
		AggregationBits: []byte{0xf0}, // bits 4-7
		CommitteeIndex:  1,
	}

	result, err := AggregateIndexedAttestations([]*IndexedAttestation{a, b})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.AggregationBits) != 1 || result.AggregationBits[0] != 0xff {
		t.Errorf("expected merged bits 0xff, got %v", result.AggregationBits)
	}
	if result.Slot != 10 {
		t.Errorf("expected slot 10, got %d", result.Slot)
	}
}

func TestAggregateIndexedAttestations_DifferentLengths(t *testing.T) {
	root := types.HexToHash("0xaa")
	a := &IndexedAttestation{
		Slot:            10,
		BeaconBlockRoot: root,
		Source:          &Checkpoint7549{Epoch: 1, Root: root},
		Target:          &Checkpoint7549{Epoch: 2, Root: root},
		AggregationBits: []byte{0x01},
		CommitteeIndex:  0,
	}
	b := &IndexedAttestation{
		Slot:            10,
		BeaconBlockRoot: root,
		Source:          &Checkpoint7549{Epoch: 1, Root: root},
		Target:          &Checkpoint7549{Epoch: 2, Root: root},
		AggregationBits: []byte{0x00, 0x01},
		CommitteeIndex:  1,
	}

	result, err := AggregateIndexedAttestations([]*IndexedAttestation{a, b})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.AggregationBits) != 2 {
		t.Errorf("expected 2 bytes of aggregation bits, got %d", len(result.AggregationBits))
	}
	if result.AggregationBits[0] != 0x01 || result.AggregationBits[1] != 0x01 {
		t.Errorf("unexpected merged bits: %v", result.AggregationBits)
	}
}

func TestAggregateIndexedAttestations_Incompatible(t *testing.T) {
	a := makeIndexedAtt(10, 0, types.HexToHash("0xaa"), 1, 2)
	b := makeIndexedAtt(11, 1, types.HexToHash("0xaa"), 1, 2) // different slot

	_, err := AggregateIndexedAttestations([]*IndexedAttestation{a, b})
	if err != ErrIndexedAttNotAggregatable {
		t.Errorf("expected ErrIndexedAttNotAggregatable, got %v", err)
	}
}

func TestAttestationPool7549_AddAndGetBest(t *testing.T) {
	pool := NewAttestationPool7549()
	root := types.HexToHash("0xaa")

	att1 := makeIndexedAtt(10, 0, root, 1, 2)
	att2 := makeIndexedAtt(10, 1, root, 1, 2)
	att3 := makeIndexedAtt(11, 0, root, 1, 2) // different slot

	if err := pool.Add(att1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := pool.Add(att2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := pool.Add(att3); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	best := pool.GetBest(10)
	if len(best) != 2 {
		t.Errorf("expected 2 attestations for slot 10, got %d", len(best))
	}

	best11 := pool.GetBest(11)
	if len(best11) != 1 {
		t.Errorf("expected 1 attestation for slot 11, got %d", len(best11))
	}

	bestEmpty := pool.GetBest(99)
	if len(bestEmpty) != 0 {
		t.Errorf("expected 0 attestations for slot 99, got %d", len(bestEmpty))
	}
}

func TestAttestationPool7549_AddNil(t *testing.T) {
	pool := NewAttestationPool7549()
	if err := pool.Add(nil); err != ErrIndexedAttNil {
		t.Errorf("expected ErrIndexedAttNil, got %v", err)
	}
}

func TestAttestationPool7549_Duplicate(t *testing.T) {
	pool := NewAttestationPool7549()
	root := types.HexToHash("0xaa")

	att := makeIndexedAtt(10, 0, root, 1, 2)
	if err := pool.Add(att); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Same slot + committee = duplicate.
	att2 := makeIndexedAtt(10, 0, root, 1, 2)
	if err := pool.Add(att2); err != ErrIndexedAttPoolDuplicate {
		t.Errorf("expected ErrIndexedAttPoolDuplicate, got %v", err)
	}
}

func TestAttestationPool7549_AggregateAll(t *testing.T) {
	pool := NewAttestationPool7549()
	root := types.HexToHash("0xaa")

	// Three attestations from the same slot with same data but different committees.
	for i := uint64(0); i < 3; i++ {
		att := &IndexedAttestation{
			Slot:            10,
			BeaconBlockRoot: root,
			Source:          &Checkpoint7549{Epoch: 1, Root: root},
			Target:          &Checkpoint7549{Epoch: 2, Root: root},
			AggregationBits: []byte{1 << i},
			CommitteeIndex:  i,
		}
		if err := pool.Add(att); err != nil {
			t.Fatalf("unexpected error adding att %d: %v", i, err)
		}
	}

	// One attestation from a different slot.
	other := makeIndexedAtt(11, 0, types.HexToHash("0xbb"), 1, 2)
	if err := pool.Add(other); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	aggregated := pool.AggregateAll()
	if len(aggregated) != 2 {
		t.Fatalf("expected 2 aggregated groups, got %d", len(aggregated))
	}

	// Find the slot=10 aggregation and verify bits are merged.
	for _, agg := range aggregated {
		if agg.Slot == 10 {
			if len(agg.AggregationBits) < 1 {
				t.Fatal("expected at least 1 byte of aggregation bits")
			}
			if agg.AggregationBits[0] != 0x07 { // bits 0,1,2
				t.Errorf("expected merged bits 0x07, got 0x%02x", agg.AggregationBits[0])
			}
		}
	}
}

func TestAttestationPool7549_ConcurrentAdd(t *testing.T) {
	pool := NewAttestationPool7549()
	root := types.HexToHash("0xcc")

	var wg sync.WaitGroup
	for i := uint64(0); i < 20; i++ {
		wg.Add(1)
		go func(idx uint64) {
			defer wg.Done()
			att := makeIndexedAtt(idx/4, idx%4, root, 1, 2)
			pool.Add(att) // ignore errors from duplicates
		}(i)
	}
	wg.Wait()

	// Verify pool is consistent.
	total := 0
	for slot := uint64(0); slot < 5; slot++ {
		total += len(pool.GetBest(slot))
	}
	if total == 0 {
		t.Error("expected some attestations in pool after concurrent adds")
	}
	if total > 20 {
		t.Errorf("expected at most 20 attestations, got %d", total)
	}
}

func TestDataKey_Deterministic(t *testing.T) {
	root := types.HexToHash("0xaa")
	a := makeIndexedAtt(10, 0, root, 1, 2)
	b := makeIndexedAtt(10, 5, root, 1, 2) // different committee, same data

	keyA := dataKey(a)
	keyB := dataKey(b)
	if keyA != keyB {
		t.Error("data keys should match for same slot/root/source/target")
	}

	c := makeIndexedAtt(10, 0, types.HexToHash("0xbb"), 1, 2) // different root
	keyC := dataKey(c)
	if keyA == keyC {
		t.Error("data keys should differ for different beacon block roots")
	}
}
