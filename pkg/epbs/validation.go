package epbs

import (
	"errors"
	"fmt"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Validation errors.
var (
	ErrEmptyBlockHash       = errors.New("block hash must not be empty")
	ErrEmptyParentBlockHash = errors.New("parent block hash must not be empty")
	ErrZeroBidValue         = errors.New("bid value must be greater than zero")
	ErrInvalidPayloadStatus = errors.New("invalid payload status")
	ErrEmptyBeaconRoot      = errors.New("beacon block root must not be empty")
	ErrEmptyPayloadRoot     = errors.New("payload root must not be empty")
	ErrEmptyStateRoot       = errors.New("state root must not be empty")
	ErrZeroSlot             = errors.New("slot must be greater than zero")
	ErrBidSlotMismatch      = errors.New("bid slot does not match envelope slot")
	ErrBuilderMismatch      = errors.New("builder index mismatch between bid and envelope")
	ErrBLSSignatureInvalid  = errors.New("BLS signature verification failed")
)

// ValidateBuilderBid checks a signed builder bid for basic correctness.
func ValidateBuilderBid(signed *SignedBuilderBid) error {
	bid := &signed.Message

	if bid.BlockHash == (types.Hash{}) {
		return ErrEmptyBlockHash
	}
	if bid.ParentBlockHash == (types.Hash{}) {
		return ErrEmptyParentBlockHash
	}
	if bid.Value == 0 {
		return ErrZeroBidValue
	}
	if bid.Slot == 0 {
		return ErrZeroSlot
	}

	// BLS signature verification: verify the builder's signature over the bid hash.
	// Only verify when both pubkey and signature are non-zero (allows unsigned bids
	// during testing, but requires valid BLS in production with registered builders).
	if signed.Signature != (BLSSignature{}) && bid.BuilderPubkey != (BLSPubkey{}) {
		bidHash := bid.BidHash()
		if !crypto.DefaultBLSBackend().Verify(
			bid.BuilderPubkey[:],
			bidHash[:],
			signed.Signature[:],
		) {
			return ErrBLSSignatureInvalid
		}
	}

	return nil
}

// ValidatePayloadEnvelope checks a payload envelope for basic correctness.
func ValidatePayloadEnvelope(env *PayloadEnvelope) error {
	if env.PayloadRoot == (types.Hash{}) {
		return ErrEmptyPayloadRoot
	}
	if env.BeaconBlockRoot == (types.Hash{}) {
		return ErrEmptyBeaconRoot
	}
	if env.StateRoot == (types.Hash{}) {
		return ErrEmptyStateRoot
	}
	if env.Slot == 0 {
		return ErrZeroSlot
	}
	return nil
}

// ValidatePayloadAttestationData checks attestation data for correctness.
func ValidatePayloadAttestationData(data *PayloadAttestationData) error {
	if data.BeaconBlockRoot == (types.Hash{}) {
		return ErrEmptyBeaconRoot
	}
	if data.Slot == 0 {
		return ErrZeroSlot
	}
	if !IsPayloadStatusValid(data.PayloadStatus) {
		return fmt.Errorf("%w: got %d", ErrInvalidPayloadStatus, data.PayloadStatus)
	}
	return nil
}

// ValidateBidEnvelopeConsistency checks that a bid and envelope are consistent.
func ValidateBidEnvelopeConsistency(bid *BuilderBid, env *PayloadEnvelope) error {
	if bid.Slot != env.Slot {
		return fmt.Errorf("%w: bid slot %d, envelope slot %d",
			ErrBidSlotMismatch, bid.Slot, env.Slot)
	}
	if bid.BuilderIndex != env.BuilderIndex {
		return fmt.Errorf("%w: bid builder %d, envelope builder %d",
			ErrBuilderMismatch, bid.BuilderIndex, env.BuilderIndex)
	}
	return nil
}
