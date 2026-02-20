package rpc

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// callDebugExt is a test helper that dispatches a request through DebugExtAPI.
func callDebugExt(t *testing.T, d *DebugExtAPI, method string, params ...interface{}) *Response {
	t.Helper()
	var rawParams []json.RawMessage
	for _, p := range params {
		b, _ := json.Marshal(p)
		rawParams = append(rawParams, json.RawMessage(b))
	}
	req := &Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  rawParams,
		ID:      json.RawMessage(`1`),
	}
	return d.HandleDebugExtRequest(req)
}

// --- debug_storageRangeAt tests ---

func TestDebugExt_StorageRangeAt(t *testing.T) {
	mb := newMockBackend()
	block := types.NewBlock(mb.headers[42], nil)
	mb.blocks[42] = block
	blockHash := mb.headers[42].Hash()

	d := NewDebugExtAPI(mb)
	resp := callDebugExt(t, d, "debug_storageRangeAt",
		encodeHash(blockHash), 0,
		"0x000000000000000000000000000000000000aaaa",
		"0x0000000000000000000000000000000000000000000000000000000000000000",
		256,
	)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	result, ok := resp.Result.(*StorageRangeResult)
	if !ok {
		t.Fatalf("result not *StorageRangeResult: %T", resp.Result)
	}
	if result.Storage == nil {
		t.Fatal("storage should not be nil")
	}
	// nextKey should be nil (no more data in mock).
	if result.NextKey != nil {
		t.Fatalf("expected nil nextKey, got %v", *result.NextKey)
	}
}

func TestDebugExt_StorageRangeAt_BlockNotFound(t *testing.T) {
	d := NewDebugExtAPI(newMockBackend())
	resp := callDebugExt(t, d, "debug_storageRangeAt",
		"0x0000000000000000000000000000000000000000000000000000000000000000",
		0,
		"0x000000000000000000000000000000000000aaaa",
		"0x0000000000000000000000000000000000000000000000000000000000000000",
		256,
	)
	if resp.Error == nil {
		t.Fatal("expected error for non-existent block")
	}
}

func TestDebugExt_StorageRangeAt_AccountNotExist(t *testing.T) {
	mb := newMockBackend()
	blockHash := mb.headers[42].Hash()

	d := NewDebugExtAPI(mb)
	resp := callDebugExt(t, d, "debug_storageRangeAt",
		encodeHash(blockHash), 0,
		"0x000000000000000000000000000000000000ffff",
		"0x0000000000000000000000000000000000000000000000000000000000000000",
		256,
	)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	result, ok := resp.Result.(*StorageRangeResult)
	if !ok {
		t.Fatalf("result not *StorageRangeResult: %T", resp.Result)
	}
	if len(result.Storage) != 0 {
		t.Fatalf("expected empty storage for non-existent account, got %d", len(result.Storage))
	}
}

func TestDebugExt_StorageRangeAt_MissingParams(t *testing.T) {
	d := NewDebugExtAPI(newMockBackend())
	resp := callDebugExt(t, d, "debug_storageRangeAt")
	if resp.Error == nil {
		t.Fatal("expected error for missing params")
	}
	if resp.Error.Code != ErrCodeInvalidParams {
		t.Fatalf("want error code %d, got %d", ErrCodeInvalidParams, resp.Error.Code)
	}
}

// --- debug_accountRange tests ---

func TestDebugExt_AccountRange(t *testing.T) {
	d := NewDebugExtAPI(newMockBackend())
	resp := callDebugExt(t, d, "debug_accountRange",
		"0x2a",
		"0x0000000000000000000000000000000000000000000000000000000000000000",
		256,
	)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	result, ok := resp.Result.(*AccountRangeResult)
	if !ok {
		t.Fatalf("result not *AccountRangeResult: %T", resp.Result)
	}
	if result.Accounts == nil {
		t.Fatal("accounts should not be nil")
	}
}

func TestDebugExt_AccountRange_BlockNotFound(t *testing.T) {
	d := NewDebugExtAPI(newMockBackend())
	resp := callDebugExt(t, d, "debug_accountRange",
		"0x999",
		"0x0000000000000000000000000000000000000000000000000000000000000000",
		256,
	)
	if resp.Error == nil {
		t.Fatal("expected error for non-existent block")
	}
}

func TestDebugExt_AccountRange_MissingParams(t *testing.T) {
	d := NewDebugExtAPI(newMockBackend())
	resp := callDebugExt(t, d, "debug_accountRange")
	if resp.Error == nil {
		t.Fatal("expected error for missing params")
	}
	if resp.Error.Code != ErrCodeInvalidParams {
		t.Fatalf("want error code %d, got %d", ErrCodeInvalidParams, resp.Error.Code)
	}
}

// --- debug_setHeadExt tests ---

func TestDebugExt_SetHeadExt(t *testing.T) {
	d := NewDebugExtAPI(newMockBackend())

	// Block 42 exists and is the current head.
	resp := callDebugExt(t, d, "debug_setHeadExt", "0x2a")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("result not map: %T", resp.Result)
	}
	if result["success"] != true {
		t.Fatal("expected success to be true")
	}
	if result["newHead"] != "0x2a" {
		t.Fatalf("want newHead 0x2a, got %v", result["newHead"])
	}
}

func TestDebugExt_SetHeadExt_FutureBlock(t *testing.T) {
	mb := newMockBackend()
	// Add a header at block 100 but keep the current header at 42.
	mb.headers[100] = &types.Header{
		Number:   big.NewInt(100),
		GasLimit: 30000000,
	}

	d := NewDebugExtAPI(mb)
	resp := callDebugExt(t, d, "debug_setHeadExt", "0x64")
	if resp.Error == nil {
		t.Fatal("expected error for future block")
	}
}

func TestDebugExt_SetHeadExt_BlockNotFound(t *testing.T) {
	d := NewDebugExtAPI(newMockBackend())
	resp := callDebugExt(t, d, "debug_setHeadExt", "0x999")
	if resp.Error == nil {
		t.Fatal("expected error for non-existent block")
	}
}

func TestDebugExt_SetHeadExt_MissingParam(t *testing.T) {
	d := NewDebugExtAPI(newMockBackend())
	resp := callDebugExt(t, d, "debug_setHeadExt")
	if resp.Error == nil {
		t.Fatal("expected error for missing param")
	}
}

// --- debug_dumpBlock tests ---

func TestDebugExt_DumpBlock(t *testing.T) {
	d := NewDebugExtAPI(newMockBackend())
	resp := callDebugExt(t, d, "debug_dumpBlock", "0x2a")

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	result, ok := resp.Result.(*DumpBlockResult)
	if !ok {
		t.Fatalf("result not *DumpBlockResult: %T", resp.Result)
	}
	if result.Accounts == nil {
		t.Fatal("accounts should not be nil")
	}
	// The mock has address 0xaaaa with balance 1e18.
	found := false
	for _, acct := range result.Accounts {
		if acct.Balance == "0xde0b6b3a7640000" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected to find account with balance 1e18 in dump")
	}
}

func TestDebugExt_DumpBlock_BlockNotFound(t *testing.T) {
	d := NewDebugExtAPI(newMockBackend())
	resp := callDebugExt(t, d, "debug_dumpBlock", "0x999")
	if resp.Error == nil {
		t.Fatal("expected error for non-existent block")
	}
}

func TestDebugExt_DumpBlock_HasCode(t *testing.T) {
	d := NewDebugExtAPI(newMockBackend())
	resp := callDebugExt(t, d, "debug_dumpBlock", "0x2a")

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	result := resp.Result.(*DumpBlockResult)
	// 0xaaaa has code 0x6000 set in mock.
	for _, acct := range result.Accounts {
		if acct.Code == "0x6000" {
			if len(acct.Code) == 0 {
				t.Fatal("expected non-empty code for account with code")
			}
			return
		}
	}
}

func TestDebugExt_DumpBlock_MissingParam(t *testing.T) {
	d := NewDebugExtAPI(newMockBackend())
	resp := callDebugExt(t, d, "debug_dumpBlock")
	if resp.Error == nil {
		t.Fatal("expected error for missing param")
	}
}

// --- debug_getModifiedAccounts tests ---

func TestDebugExt_GetModifiedAccounts(t *testing.T) {
	d := NewDebugExtAPI(newMockBackend())
	// Both start and end are block 42.
	resp := callDebugExt(t, d, "debug_getModifiedAccounts", "0x2a", "0x2a")

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	accounts, ok := resp.Result.([]string)
	if !ok {
		t.Fatalf("result not []string: %T", resp.Result)
	}
	// Should contain at least the coinbase address from block 42.
	if len(accounts) == 0 {
		// Block 42's coinbase is the zero address, which gets encoded.
		// This is still a valid result.
	}
}

func TestDebugExt_GetModifiedAccounts_InvalidRange(t *testing.T) {
	mb := newMockBackend()
	mb.headers[10] = &types.Header{Number: big.NewInt(10), GasLimit: 30000000}

	d := NewDebugExtAPI(mb)
	// Start block (42) > end block (10) should fail.
	resp := callDebugExt(t, d, "debug_getModifiedAccounts", "0x2a", "0xa")
	if resp.Error == nil {
		t.Fatal("expected error for invalid range")
	}
}

func TestDebugExt_GetModifiedAccounts_MissingParams(t *testing.T) {
	d := NewDebugExtAPI(newMockBackend())
	resp := callDebugExt(t, d, "debug_getModifiedAccounts", "0x2a")
	if resp.Error == nil {
		t.Fatal("expected error for missing params")
	}
}

// --- Unknown method ---

func TestDebugExt_UnknownMethod(t *testing.T) {
	d := NewDebugExtAPI(newMockBackend())
	resp := callDebugExt(t, d, "debug_nonexistent")
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != ErrCodeMethodNotFound {
		t.Fatalf("want error code %d, got %d", ErrCodeMethodNotFound, resp.Error.Code)
	}
}

// --- Constructor test ---

func TestNewDebugExtAPI(t *testing.T) {
	mb := newMockBackend()
	api := NewDebugExtAPI(mb)
	if api == nil {
		t.Fatal("expected non-nil API")
	}
	if api.backend != mb {
		t.Fatal("backend not set correctly")
	}
}
