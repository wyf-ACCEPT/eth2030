// deposit.go implements EIP-6110 deposit types and deposit root calculation.
// The DepositRequest struct is defined in request.go; this file provides
// deposit contract log parsing, deposit tree Merkleization, and validation.
//
// EIP-6110 moves validator deposit processing in-protocol by reading
// deposit events from the deposit contract's transaction logs instead of
// relying on the consensus layer's deposit vote mechanism.
package types

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/eth2030/eth2030/ssz"
)

// Deposit contract event constants.
// The deposit contract emits a DepositEvent with the following signature:
//
//	event DepositEvent(bytes pubkey, bytes withdrawal_credentials, bytes amount,
//	                   bytes signature, bytes index)
//
// The event topic0 is keccak256("DepositEvent(bytes,bytes,bytes,bytes,bytes)").
var (
	// DepositEventTopic is the topic0 of the DepositEvent log emitted by the
	// beacon chain deposit contract.
	DepositEventTopic = keccak256Hash([]byte("DepositEvent(bytes,bytes,bytes,bytes,bytes)"))
)

// Deposit tree constants per consensus specs.
const (
	// DepositContractTreeDepth is the depth of the deposit Merkle tree.
	DepositContractTreeDepth = 32

	// MaxDepositsPerPayload is the maximum number of deposit requests
	// per execution payload (EIP-6110).
	MaxDepositsPerPayload = 8192

	// DepositRequestSSZSize is the fixed SSZ size of a DepositRequest.
	// pubkey(48) + withdrawal_credentials(32) + amount(8) + signature(96) + index(8) = 192
	DepositRequestSSZSize = 192
)

// Deposit-specific errors.
var (
	ErrDepositLogInvalid    = errors.New("deposit: invalid log format")
	ErrDepositLogTopic      = errors.New("deposit: wrong event topic")
	ErrDepositDataTooShort  = errors.New("deposit: log data too short")
	ErrDepositSSZSize       = errors.New("deposit: invalid SSZ data size")
	ErrTooManyDeposits      = errors.New("deposit: too many deposits in payload")
)

// ParseDepositLog extracts a DepositRequest from a deposit contract log.
// The log must have the correct topic0 and be emitted by the deposit contract.
//
// The deposit contract encodes the log data as ABI-encoded dynamic bytes:
//
//	offset_pubkey(32) | offset_credentials(32) | offset_amount(32) |
//	offset_signature(32) | offset_index(32) |
//	len_pubkey(32) | pubkey(48) | padding(16) |
//	len_credentials(32) | credentials(32) |
//	len_amount(32) | amount(8) | padding(24) |
//	len_signature(32) | signature(96) |
//	len_index(32) | index(8) | padding(24)
//
// Total data length: at least 576 bytes.
func ParseDepositLog(log *Log) (*DepositRequest, error) {
	if log == nil {
		return nil, ErrDepositLogInvalid
	}

	// Verify the log source and topic.
	if log.Address != DepositContractAddress {
		return nil, fmt.Errorf("%w: wrong address %s", ErrDepositLogInvalid, log.Address.Hex())
	}
	if len(log.Topics) < 1 || log.Topics[0] != DepositEventTopic {
		return nil, ErrDepositLogTopic
	}

	data := log.Data
	// Minimum length: 5 offsets (160) + 5 lengths (160) + actual data (48+32+8+96+8=192)
	// plus ABI padding = 576 bytes.
	const minDataLen = 576
	if len(data) < minDataLen {
		return nil, fmt.Errorf("%w: got %d bytes, need >= %d",
			ErrDepositDataTooShort, len(data), minDataLen)
	}

	d := &DepositRequest{}

	// Read ABI-encoded offsets (each is a 32-byte big-endian uint256).
	pubkeyOffset := readABIOffset(data, 0)
	credentialsOffset := readABIOffset(data, 32)
	amountOffset := readABIOffset(data, 64)
	signatureOffset := readABIOffset(data, 96)
	indexOffset := readABIOffset(data, 128)

	// Read pubkey: offset -> length(32) -> data(48).
	if pubkeyOffset+32+48 > len(data) {
		return nil, ErrDepositDataTooShort
	}
	copy(d.Pubkey[:], data[pubkeyOffset+32:pubkeyOffset+32+48])

	// Read withdrawal credentials: offset -> length(32) -> data(32).
	if credentialsOffset+32+32 > len(data) {
		return nil, ErrDepositDataTooShort
	}
	copy(d.WithdrawalCredentials[:], data[credentialsOffset+32:credentialsOffset+32+32])

	// Read amount: offset -> length(32) -> data(8), little-endian.
	if amountOffset+32+8 > len(data) {
		return nil, ErrDepositDataTooShort
	}
	d.Amount = binary.LittleEndian.Uint64(data[amountOffset+32 : amountOffset+32+8])

	// Read signature: offset -> length(32) -> data(96).
	if signatureOffset+32+96 > len(data) {
		return nil, ErrDepositDataTooShort
	}
	copy(d.Signature[:], data[signatureOffset+32:signatureOffset+32+96])

	// Read index: offset -> length(32) -> data(8), little-endian.
	if indexOffset+32+8 > len(data) {
		return nil, ErrDepositDataTooShort
	}
	d.Index = binary.LittleEndian.Uint64(data[indexOffset+32 : indexOffset+32+8])

	return d, nil
}

// readABIOffset reads a 32-byte big-endian offset value from ABI-encoded data.
// In practice, Ethereum ABI offsets fit in a uint32.
func readABIOffset(data []byte, pos int) int {
	// The offset is in the last 4 bytes of the 32-byte word (big-endian uint256).
	return int(binary.BigEndian.Uint32(data[pos+28 : pos+32]))
}

// DepositRequestHashTreeRoot computes the SSZ hash tree root of a single
// DepositRequest, treating it as a container with 5 fields:
//
//	pubkey: Vector[byte, 48]
//	withdrawal_credentials: Bytes32
//	amount: uint64
//	signature: Vector[byte, 96]
//	index: uint64
func DepositRequestHashTreeRoot(d *DepositRequest) [32]byte {
	fieldRoots := [5][32]byte{
		ssz.HashTreeRootBytes48(d.Pubkey),
		ssz.HashTreeRootBytes32(d.WithdrawalCredentials),
		ssz.HashTreeRootUint64(d.Amount),
		ssz.HashTreeRootBytes96(d.Signature),
		ssz.HashTreeRootUint64(d.Index),
	}
	return ssz.HashTreeRootContainer(fieldRoots[:])
}

// DepositRequestsHashTreeRoot computes the SSZ hash tree root for a list
// of deposit requests, treated as List[DepositRequest, MAX_DEPOSITS_PER_PAYLOAD].
func DepositRequestsHashTreeRoot(deposits []*DepositRequest) [32]byte {
	roots := make([][32]byte, len(deposits))
	for i, d := range deposits {
		roots[i] = DepositRequestHashTreeRoot(d)
	}
	return ssz.HashTreeRootList(roots, MaxDepositsPerPayload)
}

// MarshalDepositRequestSSZ serializes a DepositRequest to its SSZ encoding.
// Layout (fixed-size container, 192 bytes):
//
//	pubkey(48) || withdrawal_credentials(32) || amount(8) || signature(96) || index(8)
func MarshalDepositRequestSSZ(d *DepositRequest) []byte {
	return d.Encode()
}

// UnmarshalDepositRequestSSZ deserializes a DepositRequest from SSZ bytes.
func UnmarshalDepositRequestSSZ(data []byte) (*DepositRequest, error) {
	return DecodeDepositRequest(data)
}

// ValidateDepositRequest validates a deposit request's fields.
func ValidateDepositRequest(d *DepositRequest) error {
	if d == nil {
		return errors.New("deposit: nil request")
	}
	// Pubkey must not be all zeros.
	var zeroPubkey [48]byte
	if d.Pubkey == zeroPubkey {
		return errors.New("deposit: zero pubkey")
	}
	// Amount must be at least 1 Gwei (spec minimum is 1 ETH = 1e9 Gwei,
	// but we only check for zero here; higher-level validation enforces min).
	if d.Amount == 0 {
		return errors.New("deposit: zero amount")
	}
	return nil
}

// FilterDepositLogs scans a list of logs and extracts all valid deposit
// events from the deposit contract. Logs from other contracts or with
// incorrect topics are silently skipped.
func FilterDepositLogs(logs []*Log) []*DepositRequest {
	var deposits []*DepositRequest
	for _, log := range logs {
		if log.Address != DepositContractAddress {
			continue
		}
		if len(log.Topics) < 1 || log.Topics[0] != DepositEventTopic {
			continue
		}
		d, err := ParseDepositLog(log)
		if err != nil {
			continue
		}
		deposits = append(deposits, d)
	}
	return deposits
}

// ValidateDepositRequests validates a list of deposit requests for inclusion
// in an execution payload.
func ValidateDepositRequests(deposits []*DepositRequest) error {
	if len(deposits) > MaxDepositsPerPayload {
		return fmt.Errorf("%w: got %d, max %d",
			ErrTooManyDeposits, len(deposits), MaxDepositsPerPayload)
	}
	for i, d := range deposits {
		if err := ValidateDepositRequest(d); err != nil {
			return fmt.Errorf("deposit %d: %w", i, err)
		}
	}
	return nil
}
