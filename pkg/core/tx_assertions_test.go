package core

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestNewAssertionSet(t *testing.T) {
	as := NewAssertionSet()
	if as == nil {
		t.Fatal("NewAssertionSet returned nil")
	}
	if len(as.Assertions()) != 0 {
		t.Fatalf("expected 0 assertions, got %d", len(as.Assertions()))
	}
}

func TestBlockAssertion(t *testing.T) {
	as := NewAssertionSet()
	as.AddBlockAssertion(100, 200)

	ctx := &AssertionContext{
		BlockNumber: 150,
		Timestamp:   1000,
	}
	result := as.Evaluate(ctx)
	if !result.Passed {
		t.Fatalf("expected block assertion to pass, got failure at index %d: %s",
			result.AssertionIndex, result.Reason)
	}

	// Exact lower bound.
	ctx.BlockNumber = 100
	result = as.Evaluate(ctx)
	if !result.Passed {
		t.Fatalf("expected block assertion to pass at lower bound: %s", result.Reason)
	}

	// Exact upper bound.
	ctx.BlockNumber = 200
	result = as.Evaluate(ctx)
	if !result.Passed {
		t.Fatalf("expected block assertion to pass at upper bound: %s", result.Reason)
	}
}

func TestBlockAssertionOutOfRange(t *testing.T) {
	as := NewAssertionSet()
	as.AddBlockAssertion(100, 200)

	// Below range.
	ctx := &AssertionContext{
		BlockNumber: 99,
		Timestamp:   1000,
	}
	result := as.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected block assertion to fail for block below range")
	}
	if result.Reason == "" {
		t.Fatal("expected non-empty failure reason")
	}

	// Above range.
	ctx.BlockNumber = 201
	result = as.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected block assertion to fail for block above range")
	}
}

func TestTimestampAssertion(t *testing.T) {
	as := NewAssertionSet()
	as.AddTimestampAssertion(1000, 2000)

	ctx := &AssertionContext{
		BlockNumber: 100,
		Timestamp:   1500,
	}
	result := as.Evaluate(ctx)
	if !result.Passed {
		t.Fatalf("expected timestamp assertion to pass: %s", result.Reason)
	}

	// Exact lower bound.
	ctx.Timestamp = 1000
	result = as.Evaluate(ctx)
	if !result.Passed {
		t.Fatalf("expected timestamp assertion to pass at lower bound: %s", result.Reason)
	}

	// Exact upper bound.
	ctx.Timestamp = 2000
	result = as.Evaluate(ctx)
	if !result.Passed {
		t.Fatalf("expected timestamp assertion to pass at upper bound: %s", result.Reason)
	}

	// Out of range.
	ctx.Timestamp = 999
	result = as.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected timestamp assertion to fail for timestamp below range")
	}

	ctx.Timestamp = 2001
	result = as.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected timestamp assertion to fail for timestamp above range")
	}
}

func TestBalanceAssertion(t *testing.T) {
	as := NewAssertionSet()
	addr := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	as.AddBalanceAssertion(addr, big.NewInt(1000))

	ctx := &AssertionContext{
		BlockNumber: 100,
		Timestamp:   1000,
		GetBalance: func(a types.Address) *big.Int {
			if a == addr {
				return big.NewInt(5000)
			}
			return new(big.Int)
		},
	}
	result := as.Evaluate(ctx)
	if !result.Passed {
		t.Fatalf("expected balance assertion to pass: %s", result.Reason)
	}

	// Exact match.
	ctx.GetBalance = func(a types.Address) *big.Int {
		if a == addr {
			return big.NewInt(1000)
		}
		return new(big.Int)
	}
	result = as.Evaluate(ctx)
	if !result.Passed {
		t.Fatalf("expected balance assertion to pass on exact match: %s", result.Reason)
	}
}

func TestBalanceAssertionInsufficient(t *testing.T) {
	as := NewAssertionSet()
	addr := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	as.AddBalanceAssertion(addr, big.NewInt(1000))

	ctx := &AssertionContext{
		BlockNumber: 100,
		Timestamp:   1000,
		GetBalance: func(a types.Address) *big.Int {
			if a == addr {
				return big.NewInt(999)
			}
			return new(big.Int)
		},
	}
	result := as.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected balance assertion to fail for insufficient balance")
	}
	if result.AssertionIndex != 0 {
		t.Fatalf("expected failure at index 0, got %d", result.AssertionIndex)
	}

	// Zero balance.
	ctx.GetBalance = func(a types.Address) *big.Int {
		return new(big.Int)
	}
	result = as.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected balance assertion to fail for zero balance")
	}
}

func TestNonceAssertion(t *testing.T) {
	as := NewAssertionSet()
	addr := types.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	as.AddNonceAssertion(addr, 42)

	ctx := &AssertionContext{
		BlockNumber: 100,
		Timestamp:   1000,
		GetNonce: func(a types.Address) uint64 {
			if a == addr {
				return 42
			}
			return 0
		},
	}
	result := as.Evaluate(ctx)
	if !result.Passed {
		t.Fatalf("expected nonce assertion to pass: %s", result.Reason)
	}

	// Wrong nonce.
	ctx.GetNonce = func(a types.Address) uint64 {
		return 41
	}
	result = as.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected nonce assertion to fail for wrong nonce")
	}
}

func TestStorageAssertion(t *testing.T) {
	as := NewAssertionSet()
	addr := types.HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc")
	key := types.HexToHash("0x01")
	value := types.HexToHash("0x42")
	as.AddStorageAssertion(addr, key, value)

	ctx := &AssertionContext{
		BlockNumber: 100,
		Timestamp:   1000,
		GetStorage: func(a types.Address, k types.Hash) types.Hash {
			if a == addr && k == key {
				return value
			}
			return types.Hash{}
		},
	}
	result := as.Evaluate(ctx)
	if !result.Passed {
		t.Fatalf("expected storage assertion to pass: %s", result.Reason)
	}

	// Wrong value.
	ctx.GetStorage = func(a types.Address, k types.Hash) types.Hash {
		return types.HexToHash("0x99")
	}
	result = as.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected storage assertion to fail for wrong value")
	}
}

func TestMultipleAssertions(t *testing.T) {
	as := NewAssertionSet()
	addr := types.HexToAddress("0xdddddddddddddddddddddddddddddddddddddd")
	key := types.HexToHash("0x01")
	value := types.HexToHash("0x42")

	as.AddBlockAssertion(100, 200)
	as.AddTimestampAssertion(1000, 2000)
	as.AddBalanceAssertion(addr, big.NewInt(500))
	as.AddNonceAssertion(addr, 10)
	as.AddStorageAssertion(addr, key, value)

	ctx := &AssertionContext{
		BlockNumber: 150,
		Timestamp:   1500,
		GetBalance: func(a types.Address) *big.Int {
			return big.NewInt(1000)
		},
		GetNonce: func(a types.Address) uint64 {
			return 10
		},
		GetStorage: func(a types.Address, k types.Hash) types.Hash {
			if k == key {
				return value
			}
			return types.Hash{}
		},
	}
	result := as.Evaluate(ctx)
	if !result.Passed {
		t.Fatalf("expected all assertions to pass, failed at index %d: %s",
			result.AssertionIndex, result.Reason)
	}
}

func TestFirstFailure(t *testing.T) {
	as := NewAssertionSet()
	addr := types.HexToAddress("0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")

	as.AddBlockAssertion(100, 200)                 // assertions 0,1
	as.AddBalanceAssertion(addr, big.NewInt(1000)) // assertion 2
	as.AddNonceAssertion(addr, 5)                  // assertion 3

	ctx := &AssertionContext{
		BlockNumber: 150, // passes
		Timestamp:   1000,
		GetBalance: func(a types.Address) *big.Int {
			return big.NewInt(500) // fails (need >= 1000)
		},
		GetNonce: func(a types.Address) uint64 {
			return 99 // also fails (need == 5)
		},
	}
	result := as.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected assertion failure")
	}
	// The balance assertion is index 2 (block adds 2 assertions: indices 0,1).
	if result.AssertionIndex != 2 {
		t.Fatalf("expected first failure at index 2, got %d", result.AssertionIndex)
	}
	if result.Reason == "" {
		t.Fatal("expected non-empty failure reason")
	}
}
