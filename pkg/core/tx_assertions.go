// Package core implements the core Ethereum execution logic.
package core

import (
	"fmt"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
)

// AssertionType defines the kind of precondition a transaction assertion checks.
type AssertionType uint8

const (
	// AssertBlock checks that block number is within range.
	AssertBlock AssertionType = iota
	// AssertTimestamp checks that block timestamp is within range.
	AssertTimestamp
	// AssertStorage checks a storage slot value at an address.
	AssertStorage
	// AssertBalance checks an account balance.
	AssertBalance
	// AssertNonce checks an account nonce.
	AssertNonce
	// AssertCodeHash checks an account code hash.
	AssertCodeHash
)

// Comparator constants for assertion value comparison.
const (
	CmpEq  uint8 = 0 // equal
	CmpGt  uint8 = 1 // greater than
	CmpLt  uint8 = 2 // less than
	CmpGte uint8 = 3 // greater than or equal
	CmpLte uint8 = 4 // less than or equal
	CmpNeq uint8 = 5 // not equal
)

// TxAssertion represents a single precondition for transaction execution.
type TxAssertion struct {
	Type          AssertionType
	Address       types.Address
	Key           types.Hash // storage key (for AssertStorage)
	ExpectedValue types.Hash
	Comparator    uint8
}

// AssertionResult holds the outcome of evaluating an assertion set.
type AssertionResult struct {
	Passed           bool
	AssertionIndex   int
	Reason           string
	FailedAssertions []string
}

// AssertionContext provides the state needed to evaluate assertions.
type AssertionContext struct {
	BlockNumber uint64
	Timestamp   uint64
	GetBalance  func(types.Address) *big.Int
	GetNonce    func(types.Address) uint64
	GetStorage  func(types.Address, types.Hash) types.Hash
	GetCodeHash func(types.Address) types.Hash
}

// AssertionSet holds multiple assertions that must all pass for a transaction.
type AssertionSet struct {
	assertions []TxAssertion
	// Block range fields used by AddBlockAssertion.
	blockMin *uint64
	blockMax *uint64
	// Timestamp range fields used by AddTimestampAssertion.
	timeMin *uint64
	timeMax *uint64
}

// NewAssertionSet creates an empty assertion set.
func NewAssertionSet() *AssertionSet {
	return &AssertionSet{}
}

// AddBlockAssertion adds an assertion that the block number is within [minBlock, maxBlock].
func (as *AssertionSet) AddBlockAssertion(minBlock, maxBlock uint64) {
	as.blockMin = &minBlock
	as.blockMax = &maxBlock
	// Encode as two assertions: block >= minBlock AND block <= maxBlock.
	as.assertions = append(as.assertions, TxAssertion{
		Type:          AssertBlock,
		ExpectedValue: uint64ToHash(minBlock),
		Comparator:    CmpGte,
	})
	as.assertions = append(as.assertions, TxAssertion{
		Type:          AssertBlock,
		ExpectedValue: uint64ToHash(maxBlock),
		Comparator:    CmpLte,
	})
}

// AddTimestampAssertion adds an assertion that the timestamp is within [minTime, maxTime].
func (as *AssertionSet) AddTimestampAssertion(minTime, maxTime uint64) {
	as.timeMin = &minTime
	as.timeMax = &maxTime
	as.assertions = append(as.assertions, TxAssertion{
		Type:          AssertTimestamp,
		ExpectedValue: uint64ToHash(minTime),
		Comparator:    CmpGte,
	})
	as.assertions = append(as.assertions, TxAssertion{
		Type:          AssertTimestamp,
		ExpectedValue: uint64ToHash(maxTime),
		Comparator:    CmpLte,
	})
}

// AddBalanceAssertion adds an assertion that the account balance >= minBalance.
func (as *AssertionSet) AddBalanceAssertion(addr types.Address, minBalance *big.Int) {
	as.assertions = append(as.assertions, TxAssertion{
		Type:          AssertBalance,
		Address:       addr,
		ExpectedValue: types.IntToHash(minBalance),
		Comparator:    CmpGte,
	})
}

// AddNonceAssertion adds an assertion that the account nonce equals expectedNonce.
func (as *AssertionSet) AddNonceAssertion(addr types.Address, expectedNonce uint64) {
	as.assertions = append(as.assertions, TxAssertion{
		Type:          AssertNonce,
		Address:       addr,
		ExpectedValue: uint64ToHash(expectedNonce),
		Comparator:    CmpEq,
	})
}

// AddStorageAssertion adds an assertion that storage slot key at addr equals value.
func (as *AssertionSet) AddStorageAssertion(addr types.Address, key, value types.Hash) {
	as.assertions = append(as.assertions, TxAssertion{
		Type:          AssertStorage,
		Address:       addr,
		Key:           key,
		ExpectedValue: value,
		Comparator:    CmpEq,
	})
}

// AddCodeHashAssertion adds an assertion that the account code hash equals expectedHash.
func (as *AssertionSet) AddCodeHashAssertion(addr types.Address, expectedHash types.Hash) {
	as.assertions = append(as.assertions, TxAssertion{
		Type:          AssertCodeHash,
		Address:       addr,
		ExpectedValue: expectedHash,
		Comparator:    CmpEq,
	})
}

// AddAssertion adds a raw assertion with the given type, address, key, value, and comparator.
func (as *AssertionSet) AddAssertion(a TxAssertion) {
	as.assertions = append(as.assertions, a)
}

// Assertions returns the underlying assertion slice.
func (as *AssertionSet) Assertions() []TxAssertion {
	return as.assertions
}

// Evaluate checks all assertions against the provided context.
// Returns a result indicating whether all passed, or the first failure.
// The FailedAssertions field collects reasons for all failed assertions.
func (as *AssertionSet) Evaluate(ctx *AssertionContext) *AssertionResult {
	result := &AssertionResult{Passed: true}
	for i, a := range as.assertions {
		if reason := evaluateAssertion(a, ctx); reason != "" {
			if result.Passed {
				// Record first failure index and reason for backward compatibility.
				result.Passed = false
				result.AssertionIndex = i
				result.Reason = reason
			}
			result.FailedAssertions = append(result.FailedAssertions, reason)
		}
	}
	return result
}

// evaluateAssertion checks a single assertion. Returns empty string on pass,
// or a failure reason.
func evaluateAssertion(a TxAssertion, ctx *AssertionContext) string {
	switch a.Type {
	case AssertBlock:
		return compareUint64(ctx.BlockNumber, hashToUint64(a.ExpectedValue), a.Comparator, "block number")

	case AssertTimestamp:
		return compareUint64(ctx.Timestamp, hashToUint64(a.ExpectedValue), a.Comparator, "timestamp")

	case AssertBalance:
		if ctx.GetBalance == nil {
			return "balance getter not provided"
		}
		bal := ctx.GetBalance(a.Address)
		if bal == nil {
			bal = new(big.Int)
		}
		expected := new(big.Int).SetBytes(a.ExpectedValue[:])
		return compareBigInt(bal, expected, a.Comparator, fmt.Sprintf("balance of %s", a.Address.Hex()))

	case AssertNonce:
		if ctx.GetNonce == nil {
			return "nonce getter not provided"
		}
		nonce := ctx.GetNonce(a.Address)
		expected := hashToUint64(a.ExpectedValue)
		return compareUint64(nonce, expected, a.Comparator, fmt.Sprintf("nonce of %s", a.Address.Hex()))

	case AssertStorage:
		if ctx.GetStorage == nil {
			return "storage getter not provided"
		}
		got := ctx.GetStorage(a.Address, a.Key)
		if a.Comparator == CmpEq && got != a.ExpectedValue {
			return fmt.Sprintf("storage %s slot %s: got %s, want %s",
				a.Address.Hex(), a.Key.Hex(), got.Hex(), a.ExpectedValue.Hex())
		}
		if a.Comparator == CmpNeq && got == a.ExpectedValue {
			return fmt.Sprintf("storage %s slot %s: got %s, should differ",
				a.Address.Hex(), a.Key.Hex(), got.Hex())
		}
		return ""

	case AssertCodeHash:
		if ctx.GetCodeHash == nil {
			return "code hash getter not provided"
		}
		got := ctx.GetCodeHash(a.Address)
		if a.Comparator == CmpEq && got != a.ExpectedValue {
			return fmt.Sprintf("code hash of %s: got %s, want %s",
				a.Address.Hex(), got.Hex(), a.ExpectedValue.Hex())
		}
		if a.Comparator == CmpNeq && got == a.ExpectedValue {
			return fmt.Sprintf("code hash of %s: got %s, should differ",
				a.Address.Hex(), got.Hex())
		}
		return ""

	default:
		return fmt.Sprintf("unknown assertion type %d", a.Type)
	}
}

// compareUint64 compares actual vs expected using the given comparator.
func compareUint64(actual, expected uint64, cmp uint8, label string) string {
	switch cmp {
	case CmpEq:
		if actual != expected {
			return fmt.Sprintf("%s: got %d, want == %d", label, actual, expected)
		}
	case CmpGt:
		if actual <= expected {
			return fmt.Sprintf("%s: got %d, want > %d", label, actual, expected)
		}
	case CmpLt:
		if actual >= expected {
			return fmt.Sprintf("%s: got %d, want < %d", label, actual, expected)
		}
	case CmpGte:
		if actual < expected {
			return fmt.Sprintf("%s: got %d, want >= %d", label, actual, expected)
		}
	case CmpLte:
		if actual > expected {
			return fmt.Sprintf("%s: got %d, want <= %d", label, actual, expected)
		}
	case CmpNeq:
		if actual == expected {
			return fmt.Sprintf("%s: got %d, want != %d", label, actual, expected)
		}
	}
	return ""
}

// compareBigInt compares actual vs expected big.Int using the given comparator.
func compareBigInt(actual, expected *big.Int, cmp uint8, label string) string {
	c := actual.Cmp(expected)
	switch cmp {
	case CmpEq:
		if c != 0 {
			return fmt.Sprintf("%s: got %s, want == %s", label, actual, expected)
		}
	case CmpGt:
		if c <= 0 {
			return fmt.Sprintf("%s: got %s, want > %s", label, actual, expected)
		}
	case CmpLt:
		if c >= 0 {
			return fmt.Sprintf("%s: got %s, want < %s", label, actual, expected)
		}
	case CmpGte:
		if c < 0 {
			return fmt.Sprintf("%s: got %s, want >= %s", label, actual, expected)
		}
	case CmpLte:
		if c > 0 {
			return fmt.Sprintf("%s: got %s, want <= %s", label, actual, expected)
		}
	case CmpNeq:
		if c == 0 {
			return fmt.Sprintf("%s: got %s, want != %s", label, actual, expected)
		}
	}
	return ""
}

// hashToUint64 reads a uint64 from the last 8 bytes of a Hash.
func hashToUint64(h types.Hash) uint64 {
	return uint64(h[24])<<56 | uint64(h[25])<<48 | uint64(h[26])<<40 | uint64(h[27])<<32 |
		uint64(h[28])<<24 | uint64(h[29])<<16 | uint64(h[30])<<8 | uint64(h[31])
}
