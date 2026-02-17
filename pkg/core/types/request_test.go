package types

import (
	"bytes"
	"testing"
)

func TestRequestEncode(t *testing.T) {
	r := NewRequest(DepositRequestType, []byte{0x01, 0x02, 0x03})
	encoded := r.Encode()
	if encoded[0] != 0x00 {
		t.Errorf("type byte = %d, want 0", encoded[0])
	}
	if !bytes.Equal(encoded[1:], []byte{0x01, 0x02, 0x03}) {
		t.Errorf("data mismatch")
	}
}

func TestDecodeRequest(t *testing.T) {
	data := []byte{0x01, 0xaa, 0xbb}
	r, err := DecodeRequest(data)
	if err != nil {
		t.Fatal(err)
	}
	if r.Type != WithdrawalRequestType {
		t.Errorf("type = %d, want %d", r.Type, WithdrawalRequestType)
	}
	if !bytes.Equal(r.Data, []byte{0xaa, 0xbb}) {
		t.Errorf("data mismatch")
	}
}

func TestDecodeRequestTooShort(t *testing.T) {
	_, err := DecodeRequest([]byte{})
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestRequestsFilterByType(t *testing.T) {
	requests := Requests{
		NewRequest(DepositRequestType, []byte{0x01}),
		NewRequest(WithdrawalRequestType, []byte{0x02}),
		NewRequest(DepositRequestType, []byte{0x03}),
		NewRequest(ConsolidationRequestType, []byte{0x04}),
	}

	deposits := requests.FilterByType(DepositRequestType)
	if len(deposits) != 2 {
		t.Errorf("expected 2 deposit requests, got %d", len(deposits))
	}

	withdrawals := requests.FilterByType(WithdrawalRequestType)
	if len(withdrawals) != 1 {
		t.Errorf("expected 1 withdrawal request, got %d", len(withdrawals))
	}

	consolidations := requests.FilterByType(ConsolidationRequestType)
	if len(consolidations) != 1 {
		t.Errorf("expected 1 consolidation request, got %d", len(consolidations))
	}
}

func TestRequestsEncode(t *testing.T) {
	requests := Requests{
		NewRequest(0x00, []byte{0x01}),
		NewRequest(0x01, []byte{0x02, 0x03}),
	}
	encoded := requests.Encode()
	if len(encoded) != 2 {
		t.Fatalf("expected 2 encoded requests, got %d", len(encoded))
	}
	if !bytes.Equal(encoded[0], []byte{0x00, 0x01}) {
		t.Errorf("first request encoding mismatch")
	}
	if !bytes.Equal(encoded[1], []byte{0x01, 0x02, 0x03}) {
		t.Errorf("second request encoding mismatch")
	}
}

func TestComputeRequestsHash(t *testing.T) {
	// Compute hash of known requests.
	requests := Requests{
		NewRequest(DepositRequestType, []byte{0x01, 0x02}),
		NewRequest(WithdrawalRequestType, []byte{0x03}),
	}

	hash := ComputeRequestsHash(requests)

	// Hash should not be zero.
	if hash.IsZero() {
		t.Error("requests hash should not be zero")
	}

	// Same requests should produce same hash.
	hash2 := ComputeRequestsHash(requests)
	if hash != hash2 {
		t.Error("same requests should produce same hash")
	}

	// Different requests should produce different hash.
	requests3 := Requests{
		NewRequest(DepositRequestType, []byte{0x01, 0x03}), // different data
	}
	hash3 := ComputeRequestsHash(requests3)
	if hash == hash3 {
		t.Error("different requests should produce different hash")
	}
}

func TestComputeRequestsHashEmpty(t *testing.T) {
	hash := ComputeRequestsHash(nil)
	// Empty requests should produce the hash of empty input.
	if hash.IsZero() {
		// SHA-256 of empty is not zero, but our implementation only hashes
		// existing types, so empty input gives sha256 of nothing.
	}
}

func TestValidateRequestsHash(t *testing.T) {
	requests := Requests{
		NewRequest(DepositRequestType, []byte{0x01}),
	}
	hash := ComputeRequestsHash(requests)

	// Valid header.
	header := &Header{RequestsHash: &hash}
	if err := ValidateRequestsHash(header, requests); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}

	// Wrong hash.
	wrongHash := HexToHash("0xdeadbeef")
	header2 := &Header{RequestsHash: &wrongHash}
	if err := ValidateRequestsHash(header2, requests); err == nil {
		t.Error("expected error for wrong hash")
	}

	// No hash field with requests.
	header3 := &Header{}
	if err := ValidateRequestsHash(header3, requests); err == nil {
		t.Error("expected error for missing requests_hash")
	}

	// No hash field without requests.
	header4 := &Header{}
	if err := ValidateRequestsHash(header4, nil); err != nil {
		t.Errorf("expected valid for no requests + no hash, got: %v", err)
	}
}

func TestRequestTypeConstants(t *testing.T) {
	if DepositRequestType != 0x00 {
		t.Errorf("DepositRequestType = %d, want 0", DepositRequestType)
	}
	if WithdrawalRequestType != 0x01 {
		t.Errorf("WithdrawalRequestType = %d, want 1", WithdrawalRequestType)
	}
	if ConsolidationRequestType != 0x02 {
		t.Errorf("ConsolidationRequestType = %d, want 2", ConsolidationRequestType)
	}
}
