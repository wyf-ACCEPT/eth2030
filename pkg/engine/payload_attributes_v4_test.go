package engine

import (
	"errors"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func nonZeroHash() types.Hash {
	var h types.Hash
	h[0] = 0xAA
	h[31] = 0xBB
	return h
}

func validPragueAttrs() *PayloadAttributesV4Prague {
	return &PayloadAttributesV4Prague{
		PayloadAttributesV3: PayloadAttributesV3{
			PayloadAttributesV2: PayloadAttributesV2{
				PayloadAttributesV1: PayloadAttributesV1{
					Timestamp:             100,
					PrevRandao:            nonZeroHash(),
					SuggestedFeeRecipient: types.Address{0x01},
				},
				Withdrawals: []*Withdrawal{},
			},
			ParentBeaconBlockRoot: nonZeroHash(),
		},
		TargetBlobCount: DefaultTargetBlobCount,
		MaxBlobCount:    DefaultMaxBlobCount,
	}
}

func TestValidatePraguePayloadAttributes_Valid(t *testing.T) {
	attrs := validPragueAttrs()
	if err := ValidatePraguePayloadAttributes(attrs, 50); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidatePraguePayloadAttributes_Nil(t *testing.T) {
	err := ValidatePraguePayloadAttributes(nil, 0)
	if !errors.Is(err, ErrPragueAttrNil) {
		t.Fatalf("expected ErrPragueAttrNil, got %v", err)
	}
}

func TestValidatePraguePayloadAttributes_TimestampZero(t *testing.T) {
	attrs := validPragueAttrs()
	attrs.Timestamp = 0
	err := ValidatePraguePayloadAttributes(attrs, 0)
	if !errors.Is(err, ErrPragueAttrTimestampZero) {
		t.Fatalf("expected ErrPragueAttrTimestampZero, got %v", err)
	}
}

func TestValidatePraguePayloadAttributes_TimestampNotAfterParent(t *testing.T) {
	attrs := validPragueAttrs()
	attrs.Timestamp = 50
	err := ValidatePraguePayloadAttributes(attrs, 50)
	if !errors.Is(err, ErrPragueAttrTimestampOld) {
		t.Fatalf("expected ErrPragueAttrTimestampOld, got %v", err)
	}
}

func TestValidatePraguePayloadAttributes_BeaconRootZero(t *testing.T) {
	attrs := validPragueAttrs()
	attrs.ParentBeaconBlockRoot = types.Hash{}
	err := ValidatePraguePayloadAttributes(attrs, 50)
	if !errors.Is(err, ErrPragueAttrBeaconRoot) {
		t.Fatalf("expected ErrPragueAttrBeaconRoot, got %v", err)
	}
}

func TestValidatePraguePayloadAttributes_MaxBlobCountZero(t *testing.T) {
	attrs := validPragueAttrs()
	attrs.MaxBlobCount = 0
	err := ValidatePraguePayloadAttributes(attrs, 50)
	if !errors.Is(err, ErrPragueAttrMaxZero) {
		t.Fatalf("expected ErrPragueAttrMaxZero, got %v", err)
	}
}

func TestValidatePraguePayloadAttributes_MaxBlobCountTooLarge(t *testing.T) {
	attrs := validPragueAttrs()
	attrs.MaxBlobCount = AbsoluteMaxBlobCount + 1
	err := ValidatePraguePayloadAttributes(attrs, 50)
	if !errors.Is(err, ErrPragueAttrMaxTooLarge) {
		t.Fatalf("expected ErrPragueAttrMaxTooLarge, got %v", err)
	}
}

func TestValidatePraguePayloadAttributes_TargetBlobCountZero(t *testing.T) {
	attrs := validPragueAttrs()
	attrs.TargetBlobCount = 0
	err := ValidatePraguePayloadAttributes(attrs, 50)
	if !errors.Is(err, ErrPragueAttrTargetZero) {
		t.Fatalf("expected ErrPragueAttrTargetZero, got %v", err)
	}
}

func TestValidatePraguePayloadAttributes_TargetExceedsMax(t *testing.T) {
	attrs := validPragueAttrs()
	attrs.TargetBlobCount = 10
	attrs.MaxBlobCount = 6
	err := ValidatePraguePayloadAttributes(attrs, 50)
	if !errors.Is(err, ErrPragueAttrTargetExceedsMax) {
		t.Fatalf("expected ErrPragueAttrTargetExceedsMax, got %v", err)
	}
}

func TestValidatePraguePayloadAttributes_TargetEqualsMax(t *testing.T) {
	attrs := validPragueAttrs()
	attrs.TargetBlobCount = 6
	attrs.MaxBlobCount = 6
	if err := ValidatePraguePayloadAttributes(attrs, 50); err != nil {
		t.Fatalf("target==max should be valid: %v", err)
	}
}

func TestValidatePraguePayloadAttributes_MaxAtAbsoluteLimit(t *testing.T) {
	attrs := validPragueAttrs()
	attrs.MaxBlobCount = AbsoluteMaxBlobCount
	attrs.TargetBlobCount = AbsoluteMaxBlobCount
	if err := ValidatePraguePayloadAttributes(attrs, 50); err != nil {
		t.Fatalf("max at absolute limit should be valid: %v", err)
	}
}

func TestValidatePraguePayloadAttributes_BadWithdrawals(t *testing.T) {
	attrs := validPragueAttrs()
	attrs.Withdrawals = []*Withdrawal{
		{Index: 1, ValidatorIndex: 10, Address: types.Address{0x01}, Amount: 100},
		{Index: 1, ValidatorIndex: 20, Address: types.Address{0x02}, Amount: 200}, // dup index
	}
	err := ValidatePraguePayloadAttributes(attrs, 50)
	if err == nil {
		t.Fatal("expected error for bad withdrawals")
	}
}

func TestComputeBlobGasTarget(t *testing.T) {
	target := ComputeBlobGasTarget(3)
	expected := uint64(3) * types.BlobTxBlobGasPerBlob
	if target != expected {
		t.Errorf("expected %d, got %d", expected, target)
	}
}

func TestComputeMaxBlobGas(t *testing.T) {
	maxGas := ComputeMaxBlobGas(6)
	expected := uint64(6) * types.BlobTxBlobGasPerBlob
	if maxGas != expected {
		t.Errorf("expected %d, got %d", expected, maxGas)
	}
}

func TestNewDefaultPragueAttributes(t *testing.T) {
	attrs := NewDefaultPragueAttributes()
	if attrs.TargetBlobCount != DefaultTargetBlobCount {
		t.Errorf("expected target %d, got %d", DefaultTargetBlobCount, attrs.TargetBlobCount)
	}
	if attrs.MaxBlobCount != DefaultMaxBlobCount {
		t.Errorf("expected max %d, got %d", DefaultMaxBlobCount, attrs.MaxBlobCount)
	}
}

func TestBlobCountsEqual(t *testing.T) {
	a := validPragueAttrs()
	b := validPragueAttrs()

	if !BlobCountsEqual(a, b) {
		t.Error("identical blob counts should be equal")
	}

	b.TargetBlobCount = 5
	if BlobCountsEqual(a, b) {
		t.Error("different target counts should not be equal")
	}

	if BlobCountsEqual(nil, b) {
		t.Error("nil and non-nil should not be equal")
	}
	if !BlobCountsEqual(nil, nil) {
		t.Error("nil and nil should be equal")
	}
}

func TestDerivePayloadIDPrague(t *testing.T) {
	parent := nonZeroHash()
	attrs := validPragueAttrs()
	id := DerivePayloadIDPrague(parent, attrs)

	// Non-zero ID.
	if id == (PayloadID{}) {
		t.Error("expected non-zero payload ID")
	}

	// Same input should produce same ID.
	id2 := DerivePayloadIDPrague(parent, attrs)
	if id != id2 {
		t.Error("payload ID not deterministic")
	}

	// Different blob counts should produce different ID.
	attrs2 := validPragueAttrs()
	attrs2.TargetBlobCount = 5
	id3 := DerivePayloadIDPrague(parent, attrs2)
	if id == id3 {
		t.Error("different blob counts should produce different IDs")
	}

	// Nil attributes should produce zero ID.
	idNil := DerivePayloadIDPrague(parent, nil)
	if idNil != (PayloadID{}) {
		t.Error("nil attrs should produce zero ID")
	}
}

func TestDerivePayloadIDPrague_MaxBlobCountVariation(t *testing.T) {
	parent := nonZeroHash()

	attrs1 := validPragueAttrs()
	attrs1.MaxBlobCount = 6

	attrs2 := validPragueAttrs()
	attrs2.MaxBlobCount = 8

	id1 := DerivePayloadIDPrague(parent, attrs1)
	id2 := DerivePayloadIDPrague(parent, attrs2)

	if id1 == id2 {
		t.Error("different max blob counts should produce different IDs")
	}
}

func TestValidatePraguePayloadAttributes_ParentTimestampZero(t *testing.T) {
	// When parentTimestamp is 0, timestamp validation should pass.
	attrs := validPragueAttrs()
	attrs.Timestamp = 1
	if err := ValidatePraguePayloadAttributes(attrs, 0); err != nil {
		t.Fatalf("unexpected error with zero parent timestamp: %v", err)
	}
}
