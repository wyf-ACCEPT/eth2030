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

func TestCodeHashAssertion(t *testing.T) {
	as := NewAssertionSet()
	addr := types.HexToAddress("0xaabbccddaabbccddaabbccddaabbccddaabbccdd")
	expectedHash := types.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	as.AddCodeHashAssertion(addr, expectedHash)

	ctx := &AssertionContext{
		BlockNumber: 100,
		Timestamp:   1000,
		GetCodeHash: func(a types.Address) types.Hash {
			if a == addr {
				return expectedHash
			}
			return types.Hash{}
		},
	}
	result := as.Evaluate(ctx)
	if !result.Passed {
		t.Fatalf("expected code hash assertion to pass: %s", result.Reason)
	}

	// Wrong code hash.
	ctx.GetCodeHash = func(a types.Address) types.Hash {
		return types.HexToHash("0x9999999999999999999999999999999999999999999999999999999999999999")
	}
	result = as.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected code hash assertion to fail for wrong hash")
	}
}

func TestCodeHashAssertionNilGetter(t *testing.T) {
	as := NewAssertionSet()
	addr := types.HexToAddress("0xaabbccddaabbccddaabbccddaabbccddaabbccdd")
	as.AddCodeHashAssertion(addr, types.Hash{})

	ctx := &AssertionContext{
		BlockNumber: 100,
		Timestamp:   1000,
		// No GetCodeHash set.
	}
	result := as.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected failure when code hash getter is nil")
	}
}

func TestAllComparators(t *testing.T) {
	// Test all comparators via block assertions.
	tests := []struct {
		name   string
		cmp    uint8
		actual uint64
		expect uint64
		passes bool
	}{
		{"Eq pass", CmpEq, 100, 100, true},
		{"Eq fail", CmpEq, 100, 200, false},
		{"Gt pass", CmpGt, 200, 100, true},
		{"Gt fail", CmpGt, 100, 100, false},
		{"Lt pass", CmpLt, 50, 100, true},
		{"Lt fail", CmpLt, 100, 100, false},
		{"Gte pass eq", CmpGte, 100, 100, true},
		{"Gte pass gt", CmpGte, 200, 100, true},
		{"Gte fail", CmpGte, 50, 100, false},
		{"Lte pass eq", CmpLte, 100, 100, true},
		{"Lte pass lt", CmpLte, 50, 100, true},
		{"Lte fail", CmpLte, 200, 100, false},
		{"Neq pass", CmpNeq, 100, 200, true},
		{"Neq fail", CmpNeq, 100, 100, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			as := NewAssertionSet()
			as.AddAssertion(TxAssertion{
				Type:          AssertBlock,
				ExpectedValue: uint64ToHash(tt.expect),
				Comparator:    tt.cmp,
			})

			ctx := &AssertionContext{
				BlockNumber: tt.actual,
			}
			result := as.Evaluate(ctx)
			if result.Passed != tt.passes {
				t.Errorf("Passed=%v, want %v (reason=%s)", result.Passed, tt.passes, result.Reason)
			}
		})
	}
}

func TestStorageAssertionNeq(t *testing.T) {
	as := NewAssertionSet()
	addr := types.HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc")
	key := types.HexToHash("0x01")
	value := types.HexToHash("0x42")

	as.AddAssertion(TxAssertion{
		Type:          AssertStorage,
		Address:       addr,
		Key:           key,
		ExpectedValue: value,
		Comparator:    CmpNeq,
	})

	ctx := &AssertionContext{
		BlockNumber: 100,
		Timestamp:   1000,
		GetStorage: func(a types.Address, k types.Hash) types.Hash {
			return types.HexToHash("0x99") // different from 0x42
		},
	}
	result := as.Evaluate(ctx)
	if !result.Passed {
		t.Fatalf("expected Neq storage assertion to pass: %s", result.Reason)
	}

	// Same value should fail Neq.
	ctx.GetStorage = func(a types.Address, k types.Hash) types.Hash {
		return value
	}
	result = as.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected Neq storage assertion to fail when values are equal")
	}
}

func TestFailedAssertionsField(t *testing.T) {
	as := NewAssertionSet()
	addr := types.HexToAddress("0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")

	as.AddBlockAssertion(100, 200)                 // block 50 fails both >= 100 and <= 200 checks
	as.AddBalanceAssertion(addr, big.NewInt(1000)) // fails
	as.AddNonceAssertion(addr, 5)                  // fails

	ctx := &AssertionContext{
		BlockNumber: 50, // fails block >= 100
		Timestamp:   1000,
		GetBalance: func(a types.Address) *big.Int {
			return big.NewInt(0) // fails >= 1000
		},
		GetNonce: func(a types.Address) uint64 {
			return 99 // fails == 5
		},
	}
	result := as.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected assertion failure")
	}
	// Should have at least 3 failures (block min, balance, nonce).
	if len(result.FailedAssertions) < 3 {
		t.Fatalf("expected at least 3 failed assertions, got %d", len(result.FailedAssertions))
	}
	// First failure should be at index 0 (block >= 100).
	if result.AssertionIndex != 0 {
		t.Fatalf("expected first failure at index 0, got %d", result.AssertionIndex)
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
