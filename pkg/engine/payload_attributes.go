// payload_attributes.go implements payload attribute management for the
// Engine API. It provides validation, processing, and payload ID derivation
// for the attributes that the consensus layer passes to the execution layer
// when requesting a new payload build via forkchoiceUpdated.
package engine

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Payload attribute validation errors.
var (
	ErrAttrNil                = errors.New("payload_attributes: nil attributes")
	ErrAttrTimestampZero      = errors.New("payload_attributes: timestamp must not be zero")
	ErrAttrTimestampRegress   = errors.New("payload_attributes: timestamp must be after parent")
	ErrAttrTimestampTooFar    = errors.New("payload_attributes: timestamp too far in the future")
	ErrAttrBeaconRootMissing  = errors.New("payload_attributes: parent beacon block root is zero")
	ErrAttrFeeRecipientZero   = errors.New("payload_attributes: suggested fee recipient is zero address")
	ErrAttrWithdrawalNilEntry = errors.New("payload_attributes: withdrawal entry is nil")
	ErrAttrWithdrawalBadIndex = errors.New("payload_attributes: withdrawal indices not monotonically increasing")
	ErrAttrPrevRandaoZero     = errors.New("payload_attributes: prevRandao is zero (unexpected)")
	ErrAttrSlotZero           = errors.New("payload_attributes: slot number is zero in V4 attributes")
)

// MaxAllowedTimeDrift is the maximum number of seconds a payload timestamp
// may be ahead of the current wall clock. Mirrors the CL clock drift
// tolerance used in block validation.
const MaxAllowedTimeDrift = 12

// ValidateAttributesV3 performs full validation of V3 payload attributes
// (Cancun/Deneb). It checks timestamp progression, the parent beacon block
// root, and withdrawal consistency.
func ValidateAttributesV3(attrs *PayloadAttributesV3, parentTimestamp uint64, nowUnix uint64) error {
	if attrs == nil {
		return ErrAttrNil
	}
	if err := validateTimestamp(attrs.Timestamp, parentTimestamp, nowUnix); err != nil {
		return err
	}
	// V3 requires a parent beacon block root.
	if attrs.ParentBeaconBlockRoot.IsZero() {
		return ErrAttrBeaconRootMissing
	}
	if err := validateWithdrawals(attrs.Withdrawals); err != nil {
		return err
	}
	return nil
}

// ValidateAttributesV4 performs full validation of V4 payload attributes
// (Amsterdam/FOCIL). It includes all V3 checks and additionally validates
// the slot number.
func ValidateAttributesV4(attrs *PayloadAttributesV4, parentTimestamp uint64, nowUnix uint64) error {
	if attrs == nil {
		return ErrAttrNil
	}
	if err := ValidateAttributesV3(&attrs.PayloadAttributesV3, parentTimestamp, nowUnix); err != nil {
		return err
	}
	if attrs.SlotNumber == 0 {
		return ErrAttrSlotZero
	}
	return nil
}

// ProcessWithdrawals validates and deduplicates a withdrawal list. It
// returns a cleaned list suitable for block construction. Nil entries
// are rejected. The returned list preserves ordering.
func ProcessWithdrawals(withdrawals []*Withdrawal) ([]*Withdrawal, uint64, error) {
	if len(withdrawals) == 0 {
		return nil, 0, nil
	}

	result := make([]*Withdrawal, 0, len(withdrawals))
	var totalGwei uint64

	for i, w := range withdrawals {
		if w == nil {
			return nil, 0, fmt.Errorf("%w: index %d", ErrAttrWithdrawalNilEntry, i)
		}
		result = append(result, &Withdrawal{
			Index:          w.Index,
			ValidatorIndex: w.ValidatorIndex,
			Address:        w.Address,
			Amount:         w.Amount,
		})
		totalGwei += w.Amount
	}

	return result, totalGwei, nil
}

// ResolveFeeRecipient returns the fee recipient to use for block building.
// If the suggested recipient is the zero address and a fallback is provided,
// the fallback is used instead. This allows node operators to configure a
// default coinbase while still respecting CL suggestions.
func ResolveFeeRecipient(suggested, fallback types.Address) types.Address {
	if suggested.IsZero() && !fallback.IsZero() {
		return fallback
	}
	return suggested
}

// ValidatePrevRandao checks that the prevRandao value from the CL is
// plausible. In post-merge Ethereum, prevRandao carries the RANDAO reveal
// and should not normally be zero (though technically possible).
// This returns a non-fatal warning error when zero is detected.
func ValidatePrevRandao(prevRandao types.Hash) error {
	if prevRandao.IsZero() {
		return ErrAttrPrevRandaoZero
	}
	return nil
}

// ValidateBeaconRoot checks that the parent beacon block root is present.
// Required since Cancun (EIP-4788).
func ValidateBeaconRoot(root types.Hash) error {
	if root.IsZero() {
		return ErrAttrBeaconRootMissing
	}
	return nil
}

// DerivePayloadID computes a deterministic 8-byte PayloadID from the
// payload attributes following the execution-apis spec convention:
//   PayloadID = first 8 bytes of keccak256(
//     parentHash || timestamp || prevRandao || suggestedFeeRecipient || [withdrawals] || parentBeaconBlockRoot
//   )
// This ensures that two identical forkchoiceUpdated calls with the same
// attributes produce the same payload ID, enabling idempotent builds.
func DerivePayloadID(
	parentHash types.Hash,
	timestamp uint64,
	prevRandao types.Hash,
	feeRecipient types.Address,
	withdrawals []*Withdrawal,
	beaconRoot types.Hash,
) PayloadID {
	// Pre-allocate: 32 (parent) + 8 (ts) + 32 (randao) + 20 (fee) + N*48 (withdrawals) + 32 (beacon)
	size := 32 + 8 + 32 + 20 + len(withdrawals)*48 + 32
	buf := make([]byte, 0, size)

	buf = append(buf, parentHash[:]...)

	var tsBuf [8]byte
	binary.BigEndian.PutUint64(tsBuf[:], timestamp)
	buf = append(buf, tsBuf[:]...)

	buf = append(buf, prevRandao[:]...)
	buf = append(buf, feeRecipient[:]...)

	// Encode each withdrawal as: index(8) + validatorIndex(8) + address(20) + amount(8) = 44 bytes
	// Padded to 48 for alignment.
	for _, w := range withdrawals {
		var wBuf [48]byte
		binary.BigEndian.PutUint64(wBuf[0:8], w.Index)
		binary.BigEndian.PutUint64(wBuf[8:16], w.ValidatorIndex)
		copy(wBuf[16:36], w.Address[:])
		binary.BigEndian.PutUint64(wBuf[36:44], w.Amount)
		buf = append(buf, wBuf[:]...)
	}

	buf = append(buf, beaconRoot[:]...)

	hash := crypto.Keccak256Hash(buf)
	var id PayloadID
	copy(id[:], hash[:8])
	return id
}

// DerivePayloadIDV3 is a convenience wrapper that derives the payload ID
// from V3 attributes and a parent block hash.
func DerivePayloadIDV3(parentHash types.Hash, attrs *PayloadAttributesV3) PayloadID {
	if attrs == nil {
		return PayloadID{}
	}
	return DerivePayloadID(
		parentHash,
		attrs.Timestamp,
		attrs.PrevRandao,
		attrs.SuggestedFeeRecipient,
		attrs.Withdrawals,
		attrs.ParentBeaconBlockRoot,
	)
}

// DerivePayloadIDV4 derives the payload ID from V4 attributes and a parent
// block hash. The slot number is mixed into the hash to differentiate
// otherwise identical attributes at different slots.
func DerivePayloadIDV4(parentHash types.Hash, attrs *PayloadAttributesV4) PayloadID {
	if attrs == nil {
		return PayloadID{}
	}
	// Start with the base V3 derivation material.
	size := 32 + 8 + 32 + 20 + len(attrs.Withdrawals)*48 + 32 + 8
	buf := make([]byte, 0, size)

	buf = append(buf, parentHash[:]...)

	var tsBuf [8]byte
	binary.BigEndian.PutUint64(tsBuf[:], attrs.Timestamp)
	buf = append(buf, tsBuf[:]...)

	buf = append(buf, attrs.PrevRandao[:]...)
	buf = append(buf, attrs.SuggestedFeeRecipient[:]...)

	for _, w := range attrs.Withdrawals {
		var wBuf [48]byte
		binary.BigEndian.PutUint64(wBuf[0:8], w.Index)
		binary.BigEndian.PutUint64(wBuf[8:16], w.ValidatorIndex)
		copy(wBuf[16:36], w.Address[:])
		binary.BigEndian.PutUint64(wBuf[36:44], w.Amount)
		buf = append(buf, wBuf[:]...)
	}

	buf = append(buf, attrs.ParentBeaconBlockRoot[:]...)

	// Mix in the slot number for V4 differentiation.
	var slotBuf [8]byte
	binary.BigEndian.PutUint64(slotBuf[:], attrs.SlotNumber)
	buf = append(buf, slotBuf[:]...)

	hash := crypto.Keccak256Hash(buf)
	var id PayloadID
	copy(id[:], hash[:8])
	return id
}

// --- internal helpers ---

// validateTimestamp checks that the payload timestamp is after the parent
// and not too far in the future.
func validateTimestamp(timestamp, parentTimestamp, nowUnix uint64) error {
	if timestamp == 0 {
		return ErrAttrTimestampZero
	}
	if parentTimestamp > 0 && timestamp <= parentTimestamp {
		return fmt.Errorf("%w: %d <= parent %d", ErrAttrTimestampRegress, timestamp, parentTimestamp)
	}
	if nowUnix > 0 && timestamp > nowUnix+MaxAllowedTimeDrift {
		return fmt.Errorf("%w: %d > now(%d)+%d", ErrAttrTimestampTooFar, timestamp, nowUnix, MaxAllowedTimeDrift)
	}
	return nil
}

// validateWithdrawals checks that all withdrawal entries are non-nil and
// that their indices are monotonically increasing.
func validateWithdrawals(withdrawals []*Withdrawal) error {
	if len(withdrawals) == 0 {
		return nil
	}
	for i, w := range withdrawals {
		if w == nil {
			return fmt.Errorf("%w: index %d", ErrAttrWithdrawalNilEntry, i)
		}
		if i > 0 && w.Index <= withdrawals[i-1].Index {
			return fmt.Errorf("%w: withdrawal[%d].Index=%d <= withdrawal[%d].Index=%d",
				ErrAttrWithdrawalBadIndex, i, w.Index, i-1, withdrawals[i-1].Index)
		}
	}
	return nil
}
