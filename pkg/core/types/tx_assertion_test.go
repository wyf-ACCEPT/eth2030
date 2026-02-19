package types

import (
	"math/big"
	"testing"
)

// mockStateReader implements StateReader for testing.
type mockStateReader struct {
	states   map[Address]map[Hash]Hash
	balances map[Address]*big.Int
}

func newMockStateReader() *mockStateReader {
	return &mockStateReader{
		states:   make(map[Address]map[Hash]Hash),
		balances: make(map[Address]*big.Int),
	}
}

func (m *mockStateReader) GetState(addr Address, key Hash) Hash {
	if slots, ok := m.states[addr]; ok {
		if v, ok := slots[key]; ok {
			return v
		}
	}
	return Hash{}
}

func (m *mockStateReader) GetBalance(addr Address) *big.Int {
	if b, ok := m.balances[addr]; ok {
		return new(big.Int).Set(b)
	}
	return new(big.Int)
}

func (m *mockStateReader) setStorage(addr Address, key Hash, val Hash) {
	if m.states[addr] == nil {
		m.states[addr] = make(map[Hash]Hash)
	}
	m.states[addr][key] = val
}

func (m *mockStateReader) setBalance(addr Address, val *big.Int) {
	m.balances[addr] = new(big.Int).Set(val)
}

func TestAssertStorageEq(t *testing.T) {
	state := newMockStateReader()
	addr := HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	key := HexToHash("0x01")
	val := big.NewInt(42)
	state.setStorage(addr, key, IntToHash(val))

	header := &Header{Number: big.NewInt(100), Time: 1000}

	// Valid assertion.
	assertions := []TransactionAssertion{{
		Type:    AssertStorageEq,
		Address: addr,
		Key:     key,
		Value:   val,
	}}
	if err := ValidateAssertions(assertions, state, header); err != nil {
		t.Fatalf("valid storage assertion failed: %v", err)
	}

	// Wrong value.
	assertions[0].Value = big.NewInt(99)
	if err := ValidateAssertions(assertions, state, header); err == nil {
		t.Fatal("expected error for wrong storage value")
	}

	// Nil value.
	assertions[0].Value = nil
	if err := ValidateAssertions(assertions, state, header); err == nil {
		t.Fatal("expected error for nil storage value")
	}
}

func TestAssertBalanceGTE(t *testing.T) {
	state := newMockStateReader()
	addr := HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	state.setBalance(addr, big.NewInt(1000))

	header := &Header{Number: big.NewInt(100), Time: 1000}

	// Exact match.
	assertions := []TransactionAssertion{{
		Type:    AssertBalanceGTE,
		Address: addr,
		Value:   big.NewInt(1000),
	}}
	if err := ValidateAssertions(assertions, state, header); err != nil {
		t.Fatalf("exact balance assertion failed: %v", err)
	}

	// Lower bound.
	assertions[0].Value = big.NewInt(500)
	if err := ValidateAssertions(assertions, state, header); err != nil {
		t.Fatalf("lower balance assertion failed: %v", err)
	}

	// Too high.
	assertions[0].Value = big.NewInt(1001)
	if err := ValidateAssertions(assertions, state, header); err == nil {
		t.Fatal("expected error for balance too low")
	}

	// Zero balance address.
	assertions[0].Address = HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	assertions[0].Value = big.NewInt(1)
	if err := ValidateAssertions(assertions, state, header); err == nil {
		t.Fatal("expected error for zero balance")
	}

	// Nil value.
	assertions[0].Value = nil
	if err := ValidateAssertions(assertions, state, header); err == nil {
		t.Fatal("expected error for nil balance value")
	}
}

func TestAssertBlockRange(t *testing.T) {
	state := newMockStateReader()
	header := &Header{Number: big.NewInt(100), Time: 1000}

	// Within range.
	assertions := []TransactionAssertion{{
		Type:     AssertBlockRange,
		MinBlock: 50,
		MaxBlock: 150,
	}}
	if err := ValidateAssertions(assertions, state, header); err != nil {
		t.Fatalf("valid block range failed: %v", err)
	}

	// Exact boundaries.
	assertions[0].MinBlock = 100
	assertions[0].MaxBlock = 100
	if err := ValidateAssertions(assertions, state, header); err != nil {
		t.Fatalf("exact block range failed: %v", err)
	}

	// Below range.
	assertions[0].MinBlock = 101
	assertions[0].MaxBlock = 200
	if err := ValidateAssertions(assertions, state, header); err == nil {
		t.Fatal("expected error for block below range")
	}

	// Above range.
	assertions[0].MinBlock = 50
	assertions[0].MaxBlock = 99
	if err := ValidateAssertions(assertions, state, header); err == nil {
		t.Fatal("expected error for block above range")
	}

	// Nil block number.
	header2 := &Header{Time: 1000}
	assertions[0].MinBlock = 0
	assertions[0].MaxBlock = 10
	if err := ValidateAssertions(assertions, state, header2); err != nil {
		t.Fatalf("nil block number (treated as 0) should be in range [0,10]: %v", err)
	}
}

func TestAssertTimestampRange(t *testing.T) {
	state := newMockStateReader()
	header := &Header{Number: big.NewInt(100), Time: 1000}

	// Within range.
	assertions := []TransactionAssertion{{
		Type:    AssertTimestampRange,
		MinTime: 900,
		MaxTime: 1100,
	}}
	if err := ValidateAssertions(assertions, state, header); err != nil {
		t.Fatalf("valid timestamp range failed: %v", err)
	}

	// Exact.
	assertions[0].MinTime = 1000
	assertions[0].MaxTime = 1000
	if err := ValidateAssertions(assertions, state, header); err != nil {
		t.Fatalf("exact timestamp range failed: %v", err)
	}

	// Below.
	assertions[0].MinTime = 1001
	assertions[0].MaxTime = 2000
	if err := ValidateAssertions(assertions, state, header); err == nil {
		t.Fatal("expected error for timestamp below range")
	}

	// Above.
	assertions[0].MinTime = 500
	assertions[0].MaxTime = 999
	if err := ValidateAssertions(assertions, state, header); err == nil {
		t.Fatal("expected error for timestamp above range")
	}
}

func TestMultipleAssertions(t *testing.T) {
	state := newMockStateReader()
	addr := HexToAddress("0x1111111111111111111111111111111111111111")
	state.setBalance(addr, big.NewInt(500))

	header := &Header{Number: big.NewInt(50), Time: 2000}

	assertions := []TransactionAssertion{
		{Type: AssertBlockRange, MinBlock: 40, MaxBlock: 60},
		{Type: AssertTimestampRange, MinTime: 1500, MaxTime: 2500},
		{Type: AssertBalanceGTE, Address: addr, Value: big.NewInt(100)},
	}

	if err := ValidateAssertions(assertions, state, header); err != nil {
		t.Fatalf("all valid assertions failed: %v", err)
	}

	// Fail on second assertion.
	assertions[1].MinTime = 3000
	err := ValidateAssertions(assertions, state, header)
	if err == nil {
		t.Fatal("expected error on failing assertion")
	}
	// Should reference assertion 1.
	if got := err.Error(); len(got) == 0 {
		t.Fatal("error message should not be empty")
	}
}

func TestUnknownAssertionType(t *testing.T) {
	state := newMockStateReader()
	header := &Header{Number: big.NewInt(1), Time: 1}

	assertions := []TransactionAssertion{{
		Type: AssertionType(255),
	}}
	if err := ValidateAssertions(assertions, state, header); err == nil {
		t.Fatal("expected error for unknown assertion type")
	}
}

func TestEncodeDecodeAssertions(t *testing.T) {
	original := []TransactionAssertion{
		{
			Type:    AssertStorageEq,
			Address: HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"),
			Key:     HexToHash("0x01"),
			Value:   big.NewInt(42),
		},
		{
			Type:    AssertBalanceGTE,
			Address: HexToAddress("0xcafecafecafecafecafecafecafecafecafecafe"),
			Value:   big.NewInt(1000000),
		},
		{
			Type:     AssertBlockRange,
			MinBlock: 100,
			MaxBlock: 200,
		},
		{
			Type:    AssertTimestampRange,
			MinTime: 1700000000,
			MaxTime: 1800000000,
		},
	}

	encoded := EncodeAssertions(original)
	decoded, err := DecodeAssertions(encoded)
	if err != nil {
		t.Fatalf("DecodeAssertions: %v", err)
	}

	if len(decoded) != len(original) {
		t.Fatalf("got %d assertions, want %d", len(decoded), len(original))
	}

	for i, got := range decoded {
		want := original[i]
		if got.Type != want.Type {
			t.Errorf("assertion %d: Type got %d, want %d", i, got.Type, want.Type)
		}
		if got.Address != want.Address {
			t.Errorf("assertion %d: Address mismatch", i)
		}
		if got.Key != want.Key {
			t.Errorf("assertion %d: Key mismatch", i)
		}
		if (got.Value == nil) != (want.Value == nil) {
			t.Errorf("assertion %d: Value nil mismatch", i)
		} else if got.Value != nil && got.Value.Cmp(want.Value) != 0 {
			t.Errorf("assertion %d: Value got %s, want %s", i, got.Value, want.Value)
		}
		if got.MinBlock != want.MinBlock {
			t.Errorf("assertion %d: MinBlock got %d, want %d", i, got.MinBlock, want.MinBlock)
		}
		if got.MaxBlock != want.MaxBlock {
			t.Errorf("assertion %d: MaxBlock got %d, want %d", i, got.MaxBlock, want.MaxBlock)
		}
		if got.MinTime != want.MinTime {
			t.Errorf("assertion %d: MinTime got %d, want %d", i, got.MinTime, want.MinTime)
		}
		if got.MaxTime != want.MaxTime {
			t.Errorf("assertion %d: MaxTime got %d, want %d", i, got.MaxTime, want.MaxTime)
		}
	}
}

func TestEncodeDecodeEmptyAssertions(t *testing.T) {
	encoded := EncodeAssertions(nil)
	decoded, err := DecodeAssertions(encoded)
	if err != nil {
		t.Fatalf("DecodeAssertions: %v", err)
	}
	if len(decoded) != 0 {
		t.Fatalf("expected 0 assertions, got %d", len(decoded))
	}
}

func TestDecodeAssertionsErrors(t *testing.T) {
	// Too short.
	_, err := DecodeAssertions([]byte{0x00})
	if err == nil {
		t.Fatal("expected error for short data")
	}

	// Truncated.
	encoded := EncodeAssertions([]TransactionAssertion{{
		Type:  AssertBlockRange,
		Value: big.NewInt(1),
	}})
	_, err = DecodeAssertions(encoded[:len(encoded)-5])
	if err == nil {
		t.Fatal("expected error for truncated data")
	}
}

func TestAssertionWithLargeValue(t *testing.T) {
	// Test with a 256-bit value.
	bigVal := new(big.Int)
	bigVal.SetString("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", 16)

	original := []TransactionAssertion{{
		Type:  AssertStorageEq,
		Value: bigVal,
	}}

	encoded := EncodeAssertions(original)
	decoded, err := DecodeAssertions(encoded)
	if err != nil {
		t.Fatalf("DecodeAssertions: %v", err)
	}
	if decoded[0].Value.Cmp(bigVal) != 0 {
		t.Fatalf("large value mismatch: got %s, want %s", decoded[0].Value.Text(16), bigVal.Text(16))
	}
}
