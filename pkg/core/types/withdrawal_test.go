package types

import (
	"testing"
)

func TestWithdrawalHash(t *testing.T) {
	w := &Withdrawal{
		Index:          0,
		ValidatorIndex: 100,
		Address:        HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"),
		Amount:         32_000_000_000, // 32 ETH in Gwei
	}

	h1 := WithdrawalHash(w)
	h2 := WithdrawalHash(w)

	if h1.IsZero() {
		t.Fatal("hash should not be zero")
	}
	if h1 != h2 {
		t.Fatal("hash should be deterministic")
	}

	// Different amount should produce different hash.
	w2 := &Withdrawal{
		Index:          0,
		ValidatorIndex: 100,
		Address:        HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"),
		Amount:         1_000_000_000,
	}
	h3 := WithdrawalHash(w2)
	if h1 == h3 {
		t.Fatal("different amounts should produce different hashes")
	}
}

func TestWithdrawalsRoot(t *testing.T) {
	// Empty list should return EmptyRootHash.
	root := WithdrawalsRoot(nil)
	if root != EmptyRootHash {
		t.Fatalf("empty withdrawals root = %s, want %s", root.Hex(), EmptyRootHash.Hex())
	}

	root2 := WithdrawalsRoot([]*Withdrawal{})
	if root2 != EmptyRootHash {
		t.Fatalf("empty slice withdrawals root = %s, want %s", root2.Hex(), EmptyRootHash.Hex())
	}

	// Non-empty list.
	withdrawals := []*Withdrawal{
		{Index: 0, ValidatorIndex: 1, Address: HexToAddress("0xaaaa"), Amount: 1000},
		{Index: 1, ValidatorIndex: 2, Address: HexToAddress("0xbbbb"), Amount: 2000},
	}
	root3 := WithdrawalsRoot(withdrawals)
	if root3.IsZero() {
		t.Fatal("non-empty withdrawals root should not be zero")
	}

	// Same list should produce same root.
	root4 := WithdrawalsRoot(withdrawals)
	if root3 != root4 {
		t.Fatal("withdrawals root should be deterministic")
	}

	// Different list should produce different root.
	different := []*Withdrawal{
		{Index: 0, ValidatorIndex: 1, Address: HexToAddress("0xaaaa"), Amount: 9999},
	}
	root5 := WithdrawalsRoot(different)
	if root3 == root5 {
		t.Fatal("different withdrawals should produce different root")
	}
}

func TestEncodeDecodeWithdrawal(t *testing.T) {
	original := &Withdrawal{
		Index:          42,
		ValidatorIndex: 1000,
		Address:        HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"),
		Amount:         32_000_000_000,
	}

	encoded := EncodeWithdrawal(original)
	if len(encoded) == 0 {
		t.Fatal("encoded withdrawal should not be empty")
	}

	decoded, err := DecodeWithdrawal(encoded)
	if err != nil {
		t.Fatalf("DecodeWithdrawal failed: %v", err)
	}

	if decoded.Index != original.Index {
		t.Fatalf("Index = %d, want %d", decoded.Index, original.Index)
	}
	if decoded.ValidatorIndex != original.ValidatorIndex {
		t.Fatalf("ValidatorIndex = %d, want %d", decoded.ValidatorIndex, original.ValidatorIndex)
	}
	if decoded.Address != original.Address {
		t.Fatalf("Address = %s, want %s", decoded.Address.Hex(), original.Address.Hex())
	}
	if decoded.Amount != original.Amount {
		t.Fatalf("Amount = %d, want %d", decoded.Amount, original.Amount)
	}
}

func TestDecodeWithdrawalErrors(t *testing.T) {
	// Empty data.
	if _, err := DecodeWithdrawal(nil); err == nil {
		t.Fatal("expected error for nil data")
	}

	// Invalid RLP.
	if _, err := DecodeWithdrawal([]byte{0xff, 0xff}); err == nil {
		t.Fatal("expected error for invalid RLP")
	}
}

func TestEncodeDecodeWithdrawalRoundtripHash(t *testing.T) {
	w := &Withdrawal{
		Index:          7,
		ValidatorIndex: 500,
		Address:        HexToAddress("0x1234567890abcdef1234567890abcdef12345678"),
		Amount:         64_000_000_000,
	}

	hashBefore := WithdrawalHash(w)
	encoded := EncodeWithdrawal(w)
	decoded, err := DecodeWithdrawal(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	hashAfter := WithdrawalHash(decoded)

	if hashBefore != hashAfter {
		t.Fatalf("hash changed after encode/decode: %s vs %s", hashBefore.Hex(), hashAfter.Hex())
	}
}

func TestValidateWithdrawal(t *testing.T) {
	// Valid withdrawal.
	valid := &Withdrawal{
		Index:          0,
		ValidatorIndex: 100,
		Address:        HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"),
		Amount:         1000,
	}
	if err := ValidateWithdrawal(valid); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Nil withdrawal.
	if err := ValidateWithdrawal(nil); err == nil {
		t.Fatal("expected error for nil withdrawal")
	}

	// Zero address.
	zeroAddr := &Withdrawal{
		Index:          0,
		ValidatorIndex: 100,
		Address:        Address{},
		Amount:         1000,
	}
	if err := ValidateWithdrawal(zeroAddr); err == nil {
		t.Fatal("expected error for zero address")
	}

	// Zero amount is valid (partial withdrawal of zero is allowed per spec).
	zeroAmount := &Withdrawal{
		Index:          0,
		ValidatorIndex: 100,
		Address:        HexToAddress("0xaaaa"),
		Amount:         0,
	}
	if err := ValidateWithdrawal(zeroAmount); err != nil {
		t.Fatalf("zero amount should be valid, got: %v", err)
	}
}

func TestProcessWithdrawals(t *testing.T) {
	addr1 := HexToAddress("0xaaaa")
	addr2 := HexToAddress("0xbbbb")

	withdrawals := []*Withdrawal{
		{Index: 0, ValidatorIndex: 1, Address: addr1, Amount: 1000},
		{Index: 1, ValidatorIndex: 2, Address: addr2, Amount: 2000},
		{Index: 2, ValidatorIndex: 3, Address: addr1, Amount: 3000},
	}

	credits, err := ProcessWithdrawals(withdrawals)
	if err != nil {
		t.Fatalf("ProcessWithdrawals failed: %v", err)
	}

	if credits[addr1] != 4000 {
		t.Fatalf("addr1 credit = %d, want 4000", credits[addr1])
	}
	if credits[addr2] != 2000 {
		t.Fatalf("addr2 credit = %d, want 2000", credits[addr2])
	}
}

func TestProcessWithdrawalsEmpty(t *testing.T) {
	credits, err := ProcessWithdrawals(nil)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(credits) != 0 {
		t.Fatal("expected empty credit map for nil withdrawals")
	}

	credits2, err := ProcessWithdrawals([]*Withdrawal{})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(credits2) != 0 {
		t.Fatal("expected empty credit map for empty withdrawals")
	}
}

func TestProcessWithdrawalsTooMany(t *testing.T) {
	withdrawals := make([]*Withdrawal, MaxWithdrawalsPerPayload+1)
	for i := range withdrawals {
		withdrawals[i] = &Withdrawal{
			Index:          uint64(i),
			ValidatorIndex: uint64(i),
			Address:        HexToAddress("0xaaaa"),
			Amount:         1000,
		}
	}
	if _, err := ProcessWithdrawals(withdrawals); err == nil {
		t.Fatal("expected error for too many withdrawals")
	}
}

func TestProcessWithdrawalsExactMax(t *testing.T) {
	withdrawals := make([]*Withdrawal, MaxWithdrawalsPerPayload)
	for i := range withdrawals {
		withdrawals[i] = &Withdrawal{
			Index:          uint64(i),
			ValidatorIndex: uint64(i),
			Address:        HexToAddress("0xaaaa"),
			Amount:         100,
		}
	}
	credits, err := ProcessWithdrawals(withdrawals)
	if err != nil {
		t.Fatalf("expected no error for exactly max withdrawals, got: %v", err)
	}
	if credits[HexToAddress("0xaaaa")] != 100*MaxWithdrawalsPerPayload {
		t.Fatal("credit sum mismatch")
	}
}

func TestProcessWithdrawalsDuplicateIndex(t *testing.T) {
	withdrawals := []*Withdrawal{
		{Index: 0, ValidatorIndex: 1, Address: HexToAddress("0xaaaa"), Amount: 1000},
		{Index: 0, ValidatorIndex: 2, Address: HexToAddress("0xbbbb"), Amount: 2000},
	}
	if _, err := ProcessWithdrawals(withdrawals); err == nil {
		t.Fatal("expected error for duplicate withdrawal index")
	}
}

func TestProcessWithdrawalsInvalidWithdrawal(t *testing.T) {
	withdrawals := []*Withdrawal{
		{Index: 0, ValidatorIndex: 1, Address: Address{}, Amount: 1000},
	}
	if _, err := ProcessWithdrawals(withdrawals); err == nil {
		t.Fatal("expected error for zero address withdrawal")
	}
}

func TestFilterByValidator(t *testing.T) {
	withdrawals := []*Withdrawal{
		{Index: 0, ValidatorIndex: 1, Address: HexToAddress("0xaaaa"), Amount: 1000},
		{Index: 1, ValidatorIndex: 2, Address: HexToAddress("0xbbbb"), Amount: 2000},
		{Index: 2, ValidatorIndex: 1, Address: HexToAddress("0xaaaa"), Amount: 3000},
		{Index: 3, ValidatorIndex: 3, Address: HexToAddress("0xcccc"), Amount: 4000},
	}

	// Filter for validator 1.
	filtered := FilterByValidator(withdrawals, 1)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 withdrawals for validator 1, got %d", len(filtered))
	}
	if filtered[0].Index != 0 || filtered[1].Index != 2 {
		t.Fatal("wrong withdrawals returned for validator 1")
	}

	// Filter for validator 2.
	filtered2 := FilterByValidator(withdrawals, 2)
	if len(filtered2) != 1 {
		t.Fatalf("expected 1 withdrawal for validator 2, got %d", len(filtered2))
	}

	// Filter for non-existent validator.
	filtered3 := FilterByValidator(withdrawals, 99)
	if len(filtered3) != 0 {
		t.Fatalf("expected 0 withdrawals for validator 99, got %d", len(filtered3))
	}

	// Empty list.
	filtered4 := FilterByValidator(nil, 1)
	if len(filtered4) != 0 {
		t.Fatal("expected 0 for nil list")
	}
}

func TestTotalWithdrawalAmount(t *testing.T) {
	withdrawals := []*Withdrawal{
		{Index: 0, ValidatorIndex: 1, Address: HexToAddress("0xaaaa"), Amount: 1000},
		{Index: 1, ValidatorIndex: 2, Address: HexToAddress("0xbbbb"), Amount: 2000},
		{Index: 2, ValidatorIndex: 3, Address: HexToAddress("0xcccc"), Amount: 3000},
	}

	total := TotalWithdrawalAmount(withdrawals)
	if total != 6000 {
		t.Fatalf("TotalWithdrawalAmount = %d, want 6000", total)
	}

	// Empty.
	if TotalWithdrawalAmount(nil) != 0 {
		t.Fatal("expected 0 for nil list")
	}
	if TotalWithdrawalAmount([]*Withdrawal{}) != 0 {
		t.Fatal("expected 0 for empty list")
	}

	// Single.
	single := []*Withdrawal{{Amount: 42}}
	if TotalWithdrawalAmount(single) != 42 {
		t.Fatal("expected 42 for single element")
	}
}

func TestMaxWithdrawalsPerPayloadConst(t *testing.T) {
	if MaxWithdrawalsPerPayload != 16 {
		t.Fatalf("MaxWithdrawalsPerPayload = %d, want 16", MaxWithdrawalsPerPayload)
	}
}

func TestWithdrawalHashDifferentFields(t *testing.T) {
	base := &Withdrawal{
		Index:          1,
		ValidatorIndex: 100,
		Address:        HexToAddress("0xaaaa"),
		Amount:         1000,
	}

	// Different index.
	w2 := &Withdrawal{
		Index:          2,
		ValidatorIndex: 100,
		Address:        HexToAddress("0xaaaa"),
		Amount:         1000,
	}
	if WithdrawalHash(base) == WithdrawalHash(w2) {
		t.Fatal("different Index should produce different hash")
	}

	// Different validator.
	w3 := &Withdrawal{
		Index:          1,
		ValidatorIndex: 200,
		Address:        HexToAddress("0xaaaa"),
		Amount:         1000,
	}
	if WithdrawalHash(base) == WithdrawalHash(w3) {
		t.Fatal("different ValidatorIndex should produce different hash")
	}

	// Different address.
	w4 := &Withdrawal{
		Index:          1,
		ValidatorIndex: 100,
		Address:        HexToAddress("0xbbbb"),
		Amount:         1000,
	}
	if WithdrawalHash(base) == WithdrawalHash(w4) {
		t.Fatal("different Address should produce different hash")
	}
}
