package consensus

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestCreateAttestation(t *testing.T) {
	source := Checkpoint{Epoch: 10, Root: types.HexToHash("0xaa")}
	target := Checkpoint{Epoch: 11, Root: types.HexToHash("0xbb")}
	blockRoot := types.HexToHash("0xcc")

	att := CreateAttestation(100, 5, blockRoot, source, target)
	if att == nil {
		t.Fatal("expected non-nil attestation")
	}

	if att.Data.Slot != 100 {
		t.Errorf("wrong slot: got %d, want 100", att.Data.Slot)
	}
	if att.Data.BeaconBlockRoot != blockRoot {
		t.Error("wrong beacon block root")
	}
	if att.Data.Source != source {
		t.Error("wrong source checkpoint")
	}
	if att.Data.Target != target {
		t.Error("wrong target checkpoint")
	}

	// Committee index 5 should be set in committee bits.
	indices := GetCommitteeIndices(att.CommitteeBits)
	if len(indices) != 1 || indices[0] != 5 {
		t.Errorf("wrong committee indices: got %v, want [5]", indices)
	}
}

func TestGetCommitteeIndices(t *testing.T) {
	tests := []struct {
		name     string
		bits     []byte
		expected []uint64
	}{
		{
			name:     "empty",
			bits:     []byte{},
			expected: nil,
		},
		{
			name:     "single bit 0",
			bits:     []byte{0x01},
			expected: []uint64{0},
		},
		{
			name:     "bit 7",
			bits:     []byte{0x80},
			expected: []uint64{7},
		},
		{
			name:     "multiple bits",
			bits:     []byte{0x05}, // bits 0 and 2
			expected: []uint64{0, 2},
		},
		{
			name:     "second byte",
			bits:     []byte{0x00, 0x01}, // bit 8
			expected: []uint64{8},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetCommitteeIndices(tt.bits)
			if len(got) != len(tt.expected) {
				t.Fatalf("got %v, want %v", got, tt.expected)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("index %d: got %d, want %d", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestIsEqualAttestationData(t *testing.T) {
	a := &AttestationData{
		Slot:            100,
		BeaconBlockRoot: types.HexToHash("0xaa"),
		Source:          Checkpoint{Epoch: 10, Root: types.HexToHash("0xbb")},
		Target:          Checkpoint{Epoch: 11, Root: types.HexToHash("0xcc")},
	}

	// Equal.
	b := &AttestationData{
		Slot:            100,
		BeaconBlockRoot: types.HexToHash("0xaa"),
		Source:          Checkpoint{Epoch: 10, Root: types.HexToHash("0xbb")},
		Target:          Checkpoint{Epoch: 11, Root: types.HexToHash("0xcc")},
	}
	if !IsEqualAttestationData(a, b) {
		t.Error("equal data should return true")
	}

	// Different slot.
	c := &AttestationData{
		Slot:            101,
		BeaconBlockRoot: types.HexToHash("0xaa"),
		Source:          Checkpoint{Epoch: 10, Root: types.HexToHash("0xbb")},
		Target:          Checkpoint{Epoch: 11, Root: types.HexToHash("0xcc")},
	}
	if IsEqualAttestationData(a, c) {
		t.Error("different slot should return false")
	}

	// Nil handling.
	if !IsEqualAttestationData(nil, nil) {
		t.Error("nil==nil should return true")
	}
	if IsEqualAttestationData(a, nil) {
		t.Error("non-nil vs nil should return false")
	}
}

func TestValidateAttestation(t *testing.T) {
	state := &BeaconState{Slot: 200}

	att := CreateAttestation(100, 3, types.HexToHash("0xaa"),
		Checkpoint{Epoch: 10, Root: types.HexToHash("0xbb")},
		Checkpoint{Epoch: 11, Root: types.HexToHash("0xcc")},
	)
	// Set non-zero signature.
	att.Signature[0] = 0xFF

	if err := ValidateAttestation(att, state); err != nil {
		t.Fatalf("valid attestation rejected: %v", err)
	}
}

func TestValidateAttestation_EmptySig(t *testing.T) {
	state := &BeaconState{Slot: 200}
	att := CreateAttestation(100, 0, types.HexToHash("0xaa"),
		Checkpoint{Epoch: 10, Root: types.HexToHash("0xbb")},
		Checkpoint{Epoch: 11, Root: types.HexToHash("0xcc")},
	)
	// Signature is all zeros.
	if err := ValidateAttestation(att, state); err != ErrAttestationEmptySig {
		t.Fatalf("expected ErrAttestationEmptySig, got %v", err)
	}
}

func TestValidateAttestation_FutureSlot(t *testing.T) {
	state := &BeaconState{Slot: 50}
	att := CreateAttestation(100, 0, types.HexToHash("0xaa"),
		Checkpoint{Epoch: 10, Root: types.HexToHash("0xbb")},
		Checkpoint{Epoch: 11, Root: types.HexToHash("0xcc")},
	)
	att.Signature[0] = 0xFF
	if err := ValidateAttestation(att, state); err != ErrAttestationFutureSlot {
		t.Fatalf("expected ErrAttestationFutureSlot, got %v", err)
	}
}

func TestValidateAttestation_SourceAfterTarget(t *testing.T) {
	state := &BeaconState{Slot: 200}
	att := CreateAttestation(100, 0, types.HexToHash("0xaa"),
		Checkpoint{Epoch: 15, Root: types.HexToHash("0xbb")}, // source > target
		Checkpoint{Epoch: 11, Root: types.HexToHash("0xcc")},
	)
	att.Signature[0] = 0xFF
	if err := ValidateAttestation(att, state); err != ErrAttestationSourceAfterTarget {
		t.Fatalf("expected ErrAttestationSourceAfterTarget, got %v", err)
	}
}

func TestAggregateAttestations(t *testing.T) {
	source := Checkpoint{Epoch: 10, Root: types.HexToHash("0xbb")}
	target := Checkpoint{Epoch: 11, Root: types.HexToHash("0xcc")}
	blockRoot := types.HexToHash("0xaa")

	att1 := CreateAttestation(100, 0, blockRoot, source, target)
	att1.AggregationBits = []byte{0x01} // validator 0
	att1.Signature[0] = 0xFF

	att2 := CreateAttestation(100, 1, blockRoot, source, target)
	att2.AggregationBits = []byte{0x02} // validator 1
	att2.Signature[0] = 0xFF

	agg, err := AggregateAttestations([]*Attestation{att1, att2})
	if err != nil {
		t.Fatalf("aggregation failed: %v", err)
	}

	// Aggregation bits should be OR'd.
	if len(agg.AggregationBits) < 1 || agg.AggregationBits[0] != 0x03 {
		t.Errorf("wrong aggregation bits: got %v", agg.AggregationBits)
	}

	// Committee bits should contain both committees.
	indices := GetCommitteeIndices(agg.CommitteeBits)
	if len(indices) != 2 {
		t.Fatalf("expected 2 committee indices, got %d: %v", len(indices), indices)
	}
}

func TestAggregateAttestations_DataMismatch(t *testing.T) {
	source := Checkpoint{Epoch: 10, Root: types.HexToHash("0xbb")}
	target := Checkpoint{Epoch: 11, Root: types.HexToHash("0xcc")}

	att1 := CreateAttestation(100, 0, types.HexToHash("0xaa"), source, target)
	att2 := CreateAttestation(101, 1, types.HexToHash("0xdd"), source, target) // different slot

	_, err := AggregateAttestations([]*Attestation{att1, att2})
	if err != ErrAttestationDataMismatch {
		t.Fatalf("expected ErrAttestationDataMismatch, got %v", err)
	}
}

func TestAggregateAttestations_Overlapping(t *testing.T) {
	source := Checkpoint{Epoch: 10, Root: types.HexToHash("0xbb")}
	target := Checkpoint{Epoch: 11, Root: types.HexToHash("0xcc")}
	blockRoot := types.HexToHash("0xaa")

	att1 := CreateAttestation(100, 0, blockRoot, source, target)
	att1.AggregationBits = []byte{0x01}

	att2 := CreateAttestation(100, 1, blockRoot, source, target)
	att2.AggregationBits = []byte{0x01} // overlapping bit

	_, err := AggregateAttestations([]*Attestation{att1, att2})
	if err != ErrAttestationOverlapping {
		t.Fatalf("expected ErrAttestationOverlapping, got %v", err)
	}
}

func TestAggregateAttestations_Single(t *testing.T) {
	att := CreateAttestation(100, 0, types.HexToHash("0xaa"),
		Checkpoint{Epoch: 10, Root: types.HexToHash("0xbb")},
		Checkpoint{Epoch: 11, Root: types.HexToHash("0xcc")},
	)

	result, err := AggregateAttestations([]*Attestation{att})
	if err != nil {
		t.Fatalf("single aggregation failed: %v", err)
	}
	if result != att {
		t.Error("single attestation should return the same object")
	}
}
