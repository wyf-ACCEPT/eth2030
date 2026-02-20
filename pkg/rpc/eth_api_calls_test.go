package rpc

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestParseCallRequest_Basic(t *testing.T) {
	params := []json.RawMessage{
		json.RawMessage(`{"from":"0xaaaa","to":"0xbbbb","gas":"0x5208","value":"0x0","data":"0x1234"}`),
		json.RawMessage(`"latest"`),
	}
	cr, rpcErr := parseCallRequest(params)
	if rpcErr != nil {
		t.Fatalf("unexpected error: %s", rpcErr.Message)
	}
	if cr.Args.From == nil || *cr.Args.From != "0xaaaa" {
		t.Fatal("expected from address 0xaaaa")
	}
	if cr.Args.To == nil || *cr.Args.To != "0xbbbb" {
		t.Fatal("expected to address 0xbbbb")
	}
	if cr.BlockNum != LatestBlockNumber {
		t.Fatalf("expected latest, got %d", cr.BlockNum)
	}
}

func TestParseCallRequest_MissingParams(t *testing.T) {
	_, rpcErr := parseCallRequest(nil)
	if rpcErr == nil {
		t.Fatal("expected error for missing params")
	}
}

func TestParseCallRequest_WithOverrides(t *testing.T) {
	params := []json.RawMessage{
		json.RawMessage(`{"to":"0xbbbb"}`),
		json.RawMessage(`"latest"`),
		json.RawMessage(`{"0xaaaa":{"balance":"0xde0b6b3a7640000"}}`),
	}
	cr, rpcErr := parseCallRequest(params)
	if rpcErr != nil {
		t.Fatalf("unexpected error: %s", rpcErr.Message)
	}
	if len(cr.Overrides) != 1 {
		t.Fatalf("expected 1 override, got %d", len(cr.Overrides))
	}
	ov, ok := cr.Overrides["0xaaaa"]
	if !ok {
		t.Fatal("expected override for 0xaaaa")
	}
	if ov.Balance == nil || *ov.Balance != "0xde0b6b3a7640000" {
		t.Fatal("expected balance override")
	}
}

func TestParseCallRequest_InvalidBlockNumber(t *testing.T) {
	params := []json.RawMessage{
		json.RawMessage(`{"to":"0xbbbb"}`),
		json.RawMessage(`"not_a_block"`),
	}
	_, rpcErr := parseCallRequest(params)
	if rpcErr == nil {
		t.Fatal("expected error for invalid block number")
	}
}

func TestDecodeRevertReason_ValidError(t *testing.T) {
	// Build ABI-encoded Error(string) with "Insufficient balance"
	selector := []byte{0x08, 0xc3, 0x79, 0xa2}
	// offset = 32
	offset := make([]byte, 32)
	offset[31] = 32
	// length of "Insufficient balance" = 20
	length := make([]byte, 32)
	length[31] = 20
	// "Insufficient balance" padded to 32 bytes
	msg := []byte("Insufficient balance")
	paddedMsg := make([]byte, 32)
	copy(paddedMsg, msg)

	data := append(selector, offset...)
	data = append(data, length...)
	data = append(data, paddedMsg...)

	reason := decodeRevertReason(data)
	if reason != "Insufficient balance" {
		t.Fatalf("want 'Insufficient balance', got %q", reason)
	}
}

func TestDecodeRevertReason_TooShort(t *testing.T) {
	reason := decodeRevertReason([]byte{0x08, 0xc3, 0x79, 0xa2})
	if reason != "" {
		t.Fatalf("expected empty reason for short data, got %q", reason)
	}
}

func TestDecodeRevertReason_WrongSelector(t *testing.T) {
	data := make([]byte, 100)
	data[0] = 0xff
	reason := decodeRevertReason(data)
	if reason != "" {
		t.Fatalf("expected empty reason for wrong selector, got %q", reason)
	}
}

func TestDecodeRevertReason_Empty(t *testing.T) {
	reason := decodeRevertReason(nil)
	if reason != "" {
		t.Fatalf("expected empty reason for nil data, got %q", reason)
	}
}

func TestRevertError_WithReason(t *testing.T) {
	err := &RevertError{Reason: "out of funds"}
	msg := err.Error()
	if msg != "execution reverted: out of funds" {
		t.Fatalf("unexpected error message: %s", msg)
	}
}

func TestRevertError_WithoutReason(t *testing.T) {
	err := &RevertError{}
	msg := err.Error()
	if msg != "execution reverted" {
		t.Fatalf("unexpected error message: %s", msg)
	}
}

func TestEthCallFull_Success(t *testing.T) {
	mb := newMockBackend()
	mb.callResult = []byte{0xde, 0xad}
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_call", map[string]interface{}{
		"from": "0x000000000000000000000000000000000000aaaa",
		"to":   "0x000000000000000000000000000000000000bbbb",
		"data": "0x12345678",
	}, "latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	got, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	if got != "0xdead" {
		t.Fatalf("want 0xdead, got %v", got)
	}
}

func TestEstimateGasBinarySearch_QuickSuccess(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)
	// Mock always succeeds, so lo (21000) should work.
	from := types.HexToAddress("0xaaaa")
	gas, err := api.estimateGasBinarySearch(from, nil, nil, new(big.Int), 21000, 30000000, LatestBlockNumber)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gas != 21000 {
		t.Fatalf("want 21000, got %d", gas)
	}
}

func TestEstimateGasFull_BasicTransfer(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_estimateGas", map[string]interface{}{
		"from": "0x000000000000000000000000000000000000aaaa",
		"to":   "0x000000000000000000000000000000000000bbbb",
	}, "latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	got, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	// Should be 21000 (0x5208) since the mock always succeeds.
	if got != "0x5208" {
		t.Fatalf("want 0x5208, got %v", got)
	}
}

func TestEstimateGasFull_WithData(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_estimateGas", map[string]interface{}{
		"from": "0x000000000000000000000000000000000000aaaa",
		"to":   "0x000000000000000000000000000000000000bbbb",
		"data": "0xff00ff00",
	}, "latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	// With calldata, the floor is higher than 21000; but since mock
	// always succeeds, the floor gas should pass.
	if resp.Result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestEstimateGasFull_BlockNotFound(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_estimateGas", map[string]interface{}{
		"to": "0x000000000000000000000000000000000000bbbb",
	}, "0x999")

	if resp.Error == nil {
		t.Fatal("expected error for missing block")
	}
}

func TestResolveBlockNumber_Latest(t *testing.T) {
	mb := newMockBackend()
	header := resolveBlockNumber(mb, LatestBlockNumber)
	if header == nil {
		t.Fatal("expected non-nil header for latest")
	}
	if header.Number.Uint64() != 42 {
		t.Fatalf("want block 42, got %d", header.Number.Uint64())
	}
}

func TestResolveBlockNumber_NotFound(t *testing.T) {
	mb := newMockBackend()
	header := resolveBlockNumber(mb, BlockNumber(9999))
	if header != nil {
		t.Fatal("expected nil header for non-existent block")
	}
}

func TestBlockNumberOrHashParam_ByNumber(t *testing.T) {
	mb := newMockBackend()
	bn := LatestBlockNumber
	param := BlockNumberOrHashParam{BlockNumber: &bn}
	header := param.ResolveHeader(mb)
	if header == nil {
		t.Fatal("expected non-nil header")
	}
	if header.Number.Uint64() != 42 {
		t.Fatalf("want block 42, got %d", header.Number.Uint64())
	}
}

func TestBlockNumberOrHashParam_ByHash(t *testing.T) {
	mb := newMockBackend()
	header42 := mb.headers[42]
	hashStr := encodeHash(header42.Hash())
	param := BlockNumberOrHashParam{BlockHash: &hashStr}
	header := param.ResolveHeader(mb)
	if header == nil {
		t.Fatal("expected non-nil header")
	}
	if header.Number.Uint64() != 42 {
		t.Fatalf("want block 42, got %d", header.Number.Uint64())
	}
}

func TestBlockNumberOrHashParam_Default(t *testing.T) {
	mb := newMockBackend()
	param := BlockNumberOrHashParam{}
	header := param.ResolveHeader(mb)
	if header == nil {
		t.Fatal("expected current header as default")
	}
}

func TestStateOverride_ParsesCorrectly(t *testing.T) {
	raw := `{
		"0xaaaa": {
			"balance": "0x100",
			"nonce": "0x5",
			"code": "0x6000",
			"stateDiff": {"0x01": "0x02"}
		}
	}`
	var overrides StateOverride
	if err := json.Unmarshal([]byte(raw), &overrides); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ov, ok := overrides["0xaaaa"]
	if !ok {
		t.Fatal("expected 0xaaaa override")
	}
	if ov.Balance == nil || *ov.Balance != "0x100" {
		t.Fatal("expected balance 0x100")
	}
	if ov.Nonce == nil || *ov.Nonce != "0x5" {
		t.Fatal("expected nonce 0x5")
	}
	if ov.Code == nil || *ov.Code != "0x6000" {
		t.Fatal("expected code 0x6000")
	}
	if val, ok := ov.StateDiff["0x01"]; !ok || val != "0x02" {
		t.Fatal("expected stateDiff entry")
	}
}

func TestIntrinsicGasFloor(t *testing.T) {
	if intrinsicGasFloor != 21000 {
		t.Fatalf("want 21000, got %d", intrinsicGasFloor)
	}
}

func TestMaxGasEstimateIterations(t *testing.T) {
	if maxGasEstimateIterations != 64 {
		t.Fatalf("want 64, got %d", maxGasEstimateIterations)
	}
}
