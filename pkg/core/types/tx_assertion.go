package types

import (
	"errors"
	"fmt"
	"math/big"
)

// AssertionType defines the type of a transaction assertion.
type AssertionType uint8

const (
	// AssertStorageEq asserts a storage slot equals a specific value.
	AssertStorageEq AssertionType = 0
	// AssertBalanceGTE asserts an account balance is >= a value.
	AssertBalanceGTE AssertionType = 1
	// AssertBlockRange asserts the block number is within a range.
	AssertBlockRange AssertionType = 2
	// AssertTimestampRange asserts the block timestamp is within a range.
	AssertTimestampRange AssertionType = 3
)

// TransactionAssertion represents a precondition that must hold for a transaction to execute.
type TransactionAssertion struct {
	Type    AssertionType
	Address Address  // target address (for storage/balance assertions)
	Key     Hash     // storage slot key (for AssertStorageEq)
	Value   *big.Int // expected value or minimum balance

	MinBlock uint64 // inclusive minimum block number (for AssertBlockRange)
	MaxBlock uint64 // inclusive maximum block number (for AssertBlockRange)
	MinTime  uint64 // inclusive minimum timestamp (for AssertTimestampRange)
	MaxTime  uint64 // inclusive maximum timestamp (for AssertTimestampRange)
}

// StateReader provides read access to state needed for assertion validation.
type StateReader interface {
	GetState(addr Address, key Hash) Hash
	GetBalance(addr Address) *big.Int
}

// ValidateAssertions checks each assertion against current state and block header.
// Returns the first failing assertion's error, or nil if all pass.
func ValidateAssertions(assertions []TransactionAssertion, state StateReader, header *Header) error {
	for i, a := range assertions {
		if err := validateAssertion(a, state, header); err != nil {
			return fmt.Errorf("assertion %d: %w", i, err)
		}
	}
	return nil
}

func validateAssertion(a TransactionAssertion, state StateReader, header *Header) error {
	switch a.Type {
	case AssertStorageEq:
		if a.Value == nil {
			return errors.New("storage assertion: nil value")
		}
		got := state.GetState(a.Address, a.Key)
		expected := IntToHash(a.Value)
		if got != expected {
			return fmt.Errorf("storage %s slot %s: got %s, want %s",
				a.Address.Hex(), a.Key.Hex(), got.Hex(), expected.Hex())
		}

	case AssertBalanceGTE:
		if a.Value == nil {
			return errors.New("balance assertion: nil value")
		}
		bal := state.GetBalance(a.Address)
		if bal == nil {
			bal = new(big.Int)
		}
		if bal.Cmp(a.Value) < 0 {
			return fmt.Errorf("balance of %s: got %s, need >= %s",
				a.Address.Hex(), bal.String(), a.Value.String())
		}

	case AssertBlockRange:
		blockNum := uint64(0)
		if header.Number != nil {
			blockNum = header.Number.Uint64()
		}
		if blockNum < a.MinBlock || blockNum > a.MaxBlock {
			return fmt.Errorf("block number %d not in range [%d, %d]",
				blockNum, a.MinBlock, a.MaxBlock)
		}

	case AssertTimestampRange:
		if header.Time < a.MinTime || header.Time > a.MaxTime {
			return fmt.Errorf("timestamp %d not in range [%d, %d]",
				header.Time, a.MinTime, a.MaxTime)
		}

	default:
		return fmt.Errorf("unknown assertion type %d", a.Type)
	}
	return nil
}

// EncodeAssertions serializes a slice of TransactionAssertions to bytes.
// Format per assertion: Type(1) | Address(20) | Key(32) | ValueLen(2) | Value(N) | MinBlock(8) | MaxBlock(8) | MinTime(8) | MaxTime(8)
// Preceded by a 2-byte count.
func EncodeAssertions(assertions []TransactionAssertion) []byte {
	// 2 bytes for count.
	buf := make([]byte, 2)
	buf[0] = byte(len(assertions) >> 8)
	buf[1] = byte(len(assertions))

	for _, a := range assertions {
		entry := make([]byte, 1+AddressLength+HashLength)
		entry[0] = byte(a.Type)
		copy(entry[1:1+AddressLength], a.Address[:])
		copy(entry[1+AddressLength:1+AddressLength+HashLength], a.Key[:])

		// Encode Value as variable-length big-endian.
		var valBytes []byte
		if a.Value != nil {
			valBytes = a.Value.Bytes()
		}
		valLen := make([]byte, 2)
		valLen[0] = byte(len(valBytes) >> 8)
		valLen[1] = byte(len(valBytes))
		entry = append(entry, valLen...)
		entry = append(entry, valBytes...)

		// Encode block range and timestamp range as fixed 8-byte big-endian.
		uint64Buf := make([]byte, 8)
		putUint64BE(uint64Buf, a.MinBlock)
		entry = append(entry, uint64Buf...)
		putUint64BE(uint64Buf, a.MaxBlock)
		entry = append(entry, uint64Buf...)
		putUint64BE(uint64Buf, a.MinTime)
		entry = append(entry, uint64Buf...)
		putUint64BE(uint64Buf, a.MaxTime)
		entry = append(entry, uint64Buf...)

		buf = append(buf, entry...)
	}
	return buf
}

// DecodeAssertions deserializes a slice of TransactionAssertions from bytes.
func DecodeAssertions(data []byte) ([]TransactionAssertion, error) {
	if len(data) < 2 {
		return nil, errors.New("assertion data too short")
	}
	count := int(data[0])<<8 | int(data[1])
	off := 2

	assertions := make([]TransactionAssertion, count)
	for i := range count {
		// Fixed header: Type(1) + Address(20) + Key(32) = 53 bytes.
		if off+53 > len(data) {
			return nil, fmt.Errorf("assertion %d: data truncated at header", i)
		}
		a := &assertions[i]
		a.Type = AssertionType(data[off])
		off++
		copy(a.Address[:], data[off:off+AddressLength])
		off += AddressLength
		copy(a.Key[:], data[off:off+HashLength])
		off += HashLength

		// Value length + value.
		if off+2 > len(data) {
			return nil, fmt.Errorf("assertion %d: data truncated at value length", i)
		}
		valLen := int(data[off])<<8 | int(data[off+1])
		off += 2
		if off+valLen > len(data) {
			return nil, fmt.Errorf("assertion %d: data truncated at value", i)
		}
		if valLen > 0 {
			a.Value = new(big.Int).SetBytes(data[off : off+valLen])
		}
		off += valLen

		// Block range and timestamp range: 4 * 8 = 32 bytes.
		if off+32 > len(data) {
			return nil, fmt.Errorf("assertion %d: data truncated at ranges", i)
		}
		a.MinBlock = readUint64BE(data[off:])
		off += 8
		a.MaxBlock = readUint64BE(data[off:])
		off += 8
		a.MinTime = readUint64BE(data[off:])
		off += 8
		a.MaxTime = readUint64BE(data[off:])
		off += 8
	}
	return assertions, nil
}

func putUint64BE(buf []byte, v uint64) {
	buf[0] = byte(v >> 56)
	buf[1] = byte(v >> 48)
	buf[2] = byte(v >> 40)
	buf[3] = byte(v >> 32)
	buf[4] = byte(v >> 24)
	buf[5] = byte(v >> 16)
	buf[6] = byte(v >> 8)
	buf[7] = byte(v)
}

func readUint64BE(b []byte) uint64 {
	return uint64(b[0])<<56 | uint64(b[1])<<48 | uint64(b[2])<<40 | uint64(b[3])<<32 |
		uint64(b[4])<<24 | uint64(b[5])<<16 | uint64(b[6])<<8 | uint64(b[7])
}
