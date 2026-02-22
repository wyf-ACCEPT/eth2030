package epbs

import (
	"errors"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func makeValidBid() *SignedBuilderBid {
	return &SignedBuilderBid{
		Message: BuilderBid{
			ParentBlockHash: types.HexToHash("0xaaaa000000000000000000000000000000000000000000000000000000000000"),
			BlockHash:       types.HexToHash("0xbbbb000000000000000000000000000000000000000000000000000000000000"),
			Slot:            42,
			Value:           1000,
			GasLimit:        30_000_000,
			BuilderIndex:    1,
			FeeRecipient:    types.HexToAddress("0xdead"),
		},
	}
}

// --- ValidateBuilderBid tests ---

func TestValidateBuilderBidAllValid(t *testing.T) {
	if err := ValidateBuilderBid(makeValidBid()); err != nil {
		t.Errorf("valid bid: %v", err)
	}
}

func TestValidateBuilderBidMissingBlockHash(t *testing.T) {
	bid := makeValidBid()
	bid.Message.BlockHash = types.Hash{}
	err := ValidateBuilderBid(bid)
	if !errors.Is(err, ErrEmptyBlockHash) {
		t.Errorf("expected ErrEmptyBlockHash, got %v", err)
	}
}

func TestValidateBuilderBidEmptyParentBlockHash(t *testing.T) {
	bid := makeValidBid()
	bid.Message.ParentBlockHash = types.Hash{}
	err := ValidateBuilderBid(bid)
	if !errors.Is(err, ErrEmptyParentBlockHash) {
		t.Errorf("expected ErrEmptyParentBlockHash, got %v", err)
	}
}

func TestValidateBuilderBidNoValue(t *testing.T) {
	bid := makeValidBid()
	bid.Message.Value = 0
	err := ValidateBuilderBid(bid)
	if !errors.Is(err, ErrZeroBidValue) {
		t.Errorf("expected ErrZeroBidValue, got %v", err)
	}
}

func TestValidateBuilderBidMissingSlot(t *testing.T) {
	bid := makeValidBid()
	bid.Message.Slot = 0
	err := ValidateBuilderBid(bid)
	if !errors.Is(err, ErrZeroSlot) {
		t.Errorf("expected ErrZeroSlot, got %v", err)
	}
}

func TestValidateBuilderBidMinimalValid(t *testing.T) {
	// Minimal valid bid: non-zero block hash, parent hash, slot, value.
	bid := &SignedBuilderBid{
		Message: BuilderBid{
			ParentBlockHash: types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001"),
			BlockHash:       types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000002"),
			Slot:            1,
			Value:           1,
		},
	}
	if err := ValidateBuilderBid(bid); err != nil {
		t.Errorf("minimal valid bid: %v", err)
	}
}

// --- ValidatePayloadEnvelope tests ---

func TestValidatePayloadEnvelopeAllValid(t *testing.T) {
	env := &PayloadEnvelope{
		PayloadRoot:     types.HexToHash("0xaaaa"),
		BeaconBlockRoot: types.HexToHash("0xbbbb"),
		StateRoot:       types.HexToHash("0xcccc"),
		Slot:            10,
		BuilderIndex:    1,
	}
	if err := ValidatePayloadEnvelope(env); err != nil {
		t.Errorf("valid envelope: %v", err)
	}
}

func TestValidatePayloadEnvelopeMissingPayloadRoot(t *testing.T) {
	env := &PayloadEnvelope{
		PayloadRoot:     types.Hash{},
		BeaconBlockRoot: types.HexToHash("0xbbbb"),
		StateRoot:       types.HexToHash("0xcccc"),
		Slot:            10,
	}
	if err := ValidatePayloadEnvelope(env); err != ErrEmptyPayloadRoot {
		t.Errorf("expected ErrEmptyPayloadRoot, got %v", err)
	}
}

func TestValidatePayloadEnvelopeMissingBeaconRoot(t *testing.T) {
	env := &PayloadEnvelope{
		PayloadRoot:     types.HexToHash("0xaaaa"),
		BeaconBlockRoot: types.Hash{},
		StateRoot:       types.HexToHash("0xcccc"),
		Slot:            10,
	}
	if err := ValidatePayloadEnvelope(env); err != ErrEmptyBeaconRoot {
		t.Errorf("expected ErrEmptyBeaconRoot, got %v", err)
	}
}

func TestValidatePayloadEnvelopeMissingStateRoot(t *testing.T) {
	env := &PayloadEnvelope{
		PayloadRoot:     types.HexToHash("0xaaaa"),
		BeaconBlockRoot: types.HexToHash("0xbbbb"),
		StateRoot:       types.Hash{},
		Slot:            10,
	}
	if err := ValidatePayloadEnvelope(env); err != ErrEmptyStateRoot {
		t.Errorf("expected ErrEmptyStateRoot, got %v", err)
	}
}

func TestValidatePayloadEnvelopeZeroSlot(t *testing.T) {
	env := &PayloadEnvelope{
		PayloadRoot:     types.HexToHash("0xaaaa"),
		BeaconBlockRoot: types.HexToHash("0xbbbb"),
		StateRoot:       types.HexToHash("0xcccc"),
		Slot:            0,
	}
	if err := ValidatePayloadEnvelope(env); err != ErrZeroSlot {
		t.Errorf("expected ErrZeroSlot, got %v", err)
	}
}

// --- ValidatePayloadAttestationData tests ---

func TestValidatePayloadAttestationDataValid(t *testing.T) {
	statuses := []uint8{PayloadAbsent, PayloadPresent, PayloadWithheld}
	for _, s := range statuses {
		data := &PayloadAttestationData{
			BeaconBlockRoot: types.HexToHash("0x1234"),
			Slot:            10,
			PayloadStatus:   s,
		}
		if err := ValidatePayloadAttestationData(data); err != nil {
			t.Errorf("status %d: %v", s, err)
		}
	}
}

func TestValidatePayloadAttestationDataInvalidStatus(t *testing.T) {
	for _, s := range []uint8{3, 10, 255} {
		data := &PayloadAttestationData{
			BeaconBlockRoot: types.HexToHash("0x1234"),
			Slot:            10,
			PayloadStatus:   s,
		}
		err := ValidatePayloadAttestationData(data)
		if err == nil {
			t.Errorf("status %d: expected error", s)
		}
		if !errors.Is(err, ErrInvalidPayloadStatus) {
			t.Errorf("status %d: expected ErrInvalidPayloadStatus, got %v", s, err)
		}
	}
}

func TestValidatePayloadAttestationDataEmptyBeaconRoot(t *testing.T) {
	data := &PayloadAttestationData{
		BeaconBlockRoot: types.Hash{},
		Slot:            10,
		PayloadStatus:   PayloadPresent,
	}
	if err := ValidatePayloadAttestationData(data); err != ErrEmptyBeaconRoot {
		t.Errorf("expected ErrEmptyBeaconRoot, got %v", err)
	}
}

func TestValidatePayloadAttestationDataZeroSlot(t *testing.T) {
	data := &PayloadAttestationData{
		BeaconBlockRoot: types.HexToHash("0x1234"),
		Slot:            0,
		PayloadStatus:   PayloadPresent,
	}
	if err := ValidatePayloadAttestationData(data); err != ErrZeroSlot {
		t.Errorf("expected ErrZeroSlot, got %v", err)
	}
}

// --- ValidateBidEnvelopeConsistency tests ---

func TestValidateBidEnvelopeConsistencyValid(t *testing.T) {
	bid := &BuilderBid{Slot: 100, BuilderIndex: 5}
	env := &PayloadEnvelope{Slot: 100, BuilderIndex: 5}
	if err := ValidateBidEnvelopeConsistency(bid, env); err != nil {
		t.Errorf("consistent: %v", err)
	}
}

func TestValidateBidEnvelopeConsistencySlotMismatch(t *testing.T) {
	bid := &BuilderBid{Slot: 100, BuilderIndex: 5}
	env := &PayloadEnvelope{Slot: 200, BuilderIndex: 5}
	err := ValidateBidEnvelopeConsistency(bid, env)
	if !errors.Is(err, ErrBidSlotMismatch) {
		t.Errorf("expected ErrBidSlotMismatch, got %v", err)
	}
}

func TestValidateBidEnvelopeConsistencyBuilderMismatch(t *testing.T) {
	bid := &BuilderBid{Slot: 100, BuilderIndex: 5}
	env := &PayloadEnvelope{Slot: 100, BuilderIndex: 9}
	err := ValidateBidEnvelopeConsistency(bid, env)
	if !errors.Is(err, ErrBuilderMismatch) {
		t.Errorf("expected ErrBuilderMismatch, got %v", err)
	}
}

func TestValidateBidEnvelopeConsistencyBothMismatch(t *testing.T) {
	bid := &BuilderBid{Slot: 100, BuilderIndex: 5}
	env := &PayloadEnvelope{Slot: 200, BuilderIndex: 9}
	// Slot is checked first, so slot mismatch error should be returned.
	err := ValidateBidEnvelopeConsistency(bid, env)
	if !errors.Is(err, ErrBidSlotMismatch) {
		t.Errorf("expected ErrBidSlotMismatch (checked first), got %v", err)
	}
}
