package engine

import (
	"bytes"
	"testing"
)

// makeDepositData creates valid deposit request data of the given item count.
func makeDepositData(count int) []byte {
	data := make([]byte, count*DepositRequestDataLen)
	for i := range data {
		data[i] = byte(i % 256)
	}
	return data
}

// makeWithdrawalReqData creates valid withdrawal request data of the given item count.
func makeWithdrawalReqData(count int) []byte {
	data := make([]byte, count*WithdrawalRequestDataLen)
	for i := range data {
		data[i] = byte((i + 1) % 256)
	}
	return data
}

// makeConsolidationData creates valid consolidation request data of the given item count.
func makeConsolidationData(count int) []byte {
	data := make([]byte, count*ConsolidationRequestDataLen)
	for i := range data {
		data[i] = byte((i + 2) % 256)
	}
	return data
}

func TestParseExecutionRequests_Valid(t *testing.T) {
	deposit := append([]byte{DepositRequestType}, makeDepositData(1)...)
	withdrawal := append([]byte{WithdrawalRequestType}, makeWithdrawalReqData(1)...)
	consolidation := append([]byte{ConsolidationRequestType}, makeConsolidationData(1)...)

	raw := [][]byte{deposit, withdrawal, consolidation}
	reqs, err := ParseExecutionRequests(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reqs) != 3 {
		t.Fatalf("expected 3 requests, got %d", len(reqs))
	}
	if reqs[0].Type != DepositRequestType {
		t.Errorf("expected deposit type, got 0x%02x", reqs[0].Type)
	}
	if reqs[1].Type != WithdrawalRequestType {
		t.Errorf("expected withdrawal type, got 0x%02x", reqs[1].Type)
	}
	if reqs[2].Type != ConsolidationRequestType {
		t.Errorf("expected consolidation type, got 0x%02x", reqs[2].Type)
	}
	if len(reqs[0].Data) != DepositRequestDataLen {
		t.Errorf("deposit data length: got %d, want %d", len(reqs[0].Data), DepositRequestDataLen)
	}
}

func TestParseExecutionRequests_NilInput(t *testing.T) {
	_, err := ParseExecutionRequests(nil)
	if err != ErrExecReqNil {
		t.Fatalf("expected ErrExecReqNil, got %v", err)
	}
}

func TestParseExecutionRequests_EmptyEntry(t *testing.T) {
	raw := [][]byte{{}}
	_, err := ParseExecutionRequests(raw)
	if err == nil {
		t.Fatal("expected error for empty entry")
	}
}

func TestParseExecutionRequests_TooShort(t *testing.T) {
	raw := [][]byte{{0x00}} // only type byte, no data
	_, err := ParseExecutionRequests(raw)
	if err == nil {
		t.Fatal("expected error for entry with only type byte")
	}
}

func TestParseExecutionRequests_EmptyList(t *testing.T) {
	reqs, err := ParseExecutionRequests([][]byte{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reqs) != 0 {
		t.Fatalf("expected 0 requests, got %d", len(reqs))
	}
}

func TestValidateExecutionRequestList_Valid(t *testing.T) {
	reqs := []*ExecutionRequest{
		{Type: DepositRequestType, Data: makeDepositData(1)},
		{Type: WithdrawalRequestType, Data: makeWithdrawalReqData(1)},
		{Type: ConsolidationRequestType, Data: makeConsolidationData(1)},
	}
	if err := ValidateExecutionRequestList(reqs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateExecutionRequestList_EmptyList(t *testing.T) {
	if err := ValidateExecutionRequestList([]*ExecutionRequest{}); err != nil {
		t.Fatalf("unexpected error for empty list: %v", err)
	}
}

func TestValidateExecutionRequestList_Nil(t *testing.T) {
	if err := ValidateExecutionRequestList(nil); err != ErrExecReqNil {
		t.Fatalf("expected ErrExecReqNil, got %v", err)
	}
}

func TestValidateExecutionRequestList_NotAscending(t *testing.T) {
	reqs := []*ExecutionRequest{
		{Type: WithdrawalRequestType, Data: makeWithdrawalReqData(1)},
		{Type: DepositRequestType, Data: makeDepositData(1)},
	}
	err := ValidateExecutionRequestList(reqs)
	if err == nil {
		t.Fatal("expected error for non-ascending types")
	}
}

func TestValidateExecutionRequestList_DuplicateType(t *testing.T) {
	reqs := []*ExecutionRequest{
		{Type: DepositRequestType, Data: makeDepositData(1)},
		{Type: DepositRequestType, Data: makeDepositData(1)},
	}
	err := ValidateExecutionRequestList(reqs)
	if err == nil {
		t.Fatal("expected error for duplicate types")
	}
}

func TestValidateExecutionRequestList_UnknownType(t *testing.T) {
	reqs := []*ExecutionRequest{
		{Type: 0xFF, Data: []byte{1, 2, 3}},
	}
	err := ValidateExecutionRequestList(reqs)
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestValidateExecutionRequestList_InvalidDataLen(t *testing.T) {
	// Deposit with wrong data length (not a multiple of 192).
	reqs := []*ExecutionRequest{
		{Type: DepositRequestType, Data: []byte{1, 2, 3}},
	}
	err := ValidateExecutionRequestList(reqs)
	if err == nil {
		t.Fatal("expected error for invalid data length")
	}
}

func TestValidateExecutionRequestList_EmptyData(t *testing.T) {
	reqs := []*ExecutionRequest{
		{Type: DepositRequestType, Data: []byte{}},
	}
	err := ValidateExecutionRequestList(reqs)
	if err == nil {
		t.Fatal("expected error for empty data")
	}
}

func TestValidateExecutionRequestList_TooMany(t *testing.T) {
	// 17 items exceeds MaxExecutionRequestsPerType=16.
	reqs := []*ExecutionRequest{
		{Type: DepositRequestType, Data: makeDepositData(17)},
	}
	err := ValidateExecutionRequestList(reqs)
	if err == nil {
		t.Fatal("expected error for too many items")
	}
}

func TestValidateExecutionRequestList_MultipleItems(t *testing.T) {
	// 3 deposit items should be valid.
	reqs := []*ExecutionRequest{
		{Type: DepositRequestType, Data: makeDepositData(3)},
	}
	if err := ValidateExecutionRequestList(reqs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateExecutionRequestList_NilEntry(t *testing.T) {
	reqs := []*ExecutionRequest{nil}
	err := ValidateExecutionRequestList(reqs)
	if err == nil {
		t.Fatal("expected error for nil entry")
	}
}

func TestExecutionRequest_Encode(t *testing.T) {
	data := makeDepositData(1)
	req := &ExecutionRequest{Type: DepositRequestType, Data: data}
	encoded := req.Encode()

	if encoded[0] != DepositRequestType {
		t.Errorf("expected type 0x%02x, got 0x%02x", DepositRequestType, encoded[0])
	}
	if !bytes.Equal(encoded[1:], data) {
		t.Error("encoded data does not match original")
	}
}

func TestExecutionRequestsHash_Empty(t *testing.T) {
	h := ExecutionRequestsHash(nil)
	var zero [32]byte
	if h != zero {
		t.Errorf("expected zero hash for nil requests, got %x", h)
	}
}

func TestExecutionRequestsHash_NonEmpty(t *testing.T) {
	raw := [][]byte{
		append([]byte{DepositRequestType}, makeDepositData(1)...),
		append([]byte{WithdrawalRequestType}, makeWithdrawalReqData(1)...),
	}
	h := ExecutionRequestsHash(raw)
	var zero [32]byte
	if h == zero {
		t.Error("expected non-zero hash for non-empty requests")
	}

	// Same input should produce same hash.
	h2 := ExecutionRequestsHash(raw)
	if h != h2 {
		t.Error("hash is not deterministic")
	}
}

func TestSplitRequestsByType(t *testing.T) {
	reqs := []*ExecutionRequest{
		{Type: DepositRequestType, Data: makeDepositData(1)},
		{Type: WithdrawalRequestType, Data: makeWithdrawalReqData(1)},
		{Type: ConsolidationRequestType, Data: makeConsolidationData(1)},
	}

	groups := SplitRequestsByType(reqs)
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}
	if len(groups[DepositRequestType]) != 1 {
		t.Errorf("expected 1 deposit request, got %d", len(groups[DepositRequestType]))
	}
	if len(groups[WithdrawalRequestType]) != 1 {
		t.Errorf("expected 1 withdrawal request, got %d", len(groups[WithdrawalRequestType]))
	}
}

func TestSplitRequestsByType_WithNil(t *testing.T) {
	reqs := []*ExecutionRequest{
		{Type: DepositRequestType, Data: makeDepositData(1)},
		nil,
		{Type: WithdrawalRequestType, Data: makeWithdrawalReqData(1)},
	}
	groups := SplitRequestsByType(reqs)
	if len(groups[DepositRequestType]) != 1 {
		t.Error("nil entry should be skipped")
	}
}

func TestCountDepositRequests(t *testing.T) {
	if got := CountDepositRequests(makeDepositData(3)); got != 3 {
		t.Errorf("expected 3, got %d", got)
	}
	if got := CountDepositRequests(nil); got != 0 {
		t.Errorf("expected 0 for nil, got %d", got)
	}
}

func TestCountWithdrawalRequests(t *testing.T) {
	if got := CountWithdrawalRequests(makeWithdrawalReqData(5)); got != 5 {
		t.Errorf("expected 5, got %d", got)
	}
}

func TestCountConsolidationRequests(t *testing.T) {
	if got := CountConsolidationRequests(makeConsolidationData(2)); got != 2 {
		t.Errorf("expected 2, got %d", got)
	}
}

func TestExecutionRequestConstants(t *testing.T) {
	// Verify type bytes.
	if DepositRequestType != 0x00 {
		t.Errorf("DepositRequestType: got 0x%02x, want 0x00", DepositRequestType)
	}
	if WithdrawalRequestType != 0x01 {
		t.Errorf("WithdrawalRequestType: got 0x%02x, want 0x01", WithdrawalRequestType)
	}
	if ConsolidationRequestType != 0x02 {
		t.Errorf("ConsolidationRequestType: got 0x%02x, want 0x02", ConsolidationRequestType)
	}

	// Verify data lengths per spec.
	if DepositRequestDataLen != 192 {
		t.Errorf("DepositRequestDataLen: got %d, want 192", DepositRequestDataLen)
	}
	if WithdrawalRequestDataLen != 76 {
		t.Errorf("WithdrawalRequestDataLen: got %d, want 76", WithdrawalRequestDataLen)
	}
	if ConsolidationRequestDataLen != 116 {
		t.Errorf("ConsolidationRequestDataLen: got %d, want 116", ConsolidationRequestDataLen)
	}
}

func TestParseAndRoundtrip(t *testing.T) {
	// Create raw requests, parse them, encode back, verify match.
	origData := makeDepositData(2)
	raw := [][]byte{append([]byte{DepositRequestType}, origData...)}

	reqs, err := ParseExecutionRequests(raw)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	encoded := reqs[0].Encode()
	if !bytes.Equal(encoded, raw[0]) {
		t.Error("roundtrip failed: encoded does not match original raw bytes")
	}
}
