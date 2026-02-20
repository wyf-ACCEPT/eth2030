package rpc

import (
	"encoding/json"
	"testing"
)

// TestAPIDispatch_KnownMethods tests that HandleRequest correctly dispatches
// to the right method for a variety of known JSON-RPC methods.
func TestAPIDispatch_KnownMethods(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)

	tests := []struct {
		method  string
		wantErr bool
		errCode int
	}{
		{"eth_chainId", false, 0},
		{"eth_blockNumber", false, 0},
		{"eth_gasPrice", false, 0},
		{"eth_syncing", false, 0},
		{"eth_maxPriorityFeePerGas", false, 0},
		{"net_version", false, 0},
		{"net_listening", false, 0},
		{"net_peerCount", false, 0},
		{"web3_clientVersion", false, 0},
		{"eth_accounts", false, 0},
		{"eth_mining", false, 0},
		{"eth_hashrate", false, 0},
		{"eth_protocolVersion", false, 0},
		{"eth_blobBaseFee", false, 0},
		{"unknown_method", true, ErrCodeMethodNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			resp := callRPC(t, api, tt.method)
			if tt.wantErr {
				if resp.Error == nil {
					t.Fatalf("expected error for %s, got nil", tt.method)
				}
				if resp.Error.Code != tt.errCode {
					t.Fatalf("want error code %d, got %d", tt.errCode, resp.Error.Code)
				}
			} else {
				if resp.Error != nil {
					t.Fatalf("unexpected error for %s: %s", tt.method, resp.Error.Message)
				}
			}
		})
	}
}

// TestAPIDispatch_MissingParams tests that methods requiring params
// return ErrCodeInvalidParams when params are missing.
func TestAPIDispatch_MissingParams(t *testing.T) {
	api := NewEthAPI(newMockBackend())

	methods := []string{
		"eth_getBlockByNumber",
		"eth_getBlockByHash",
		"eth_getBalance",
		"eth_getTransactionCount",
		"eth_getCode",
		"eth_getStorageAt",
		"eth_getTransactionByHash",
		"eth_getTransactionReceipt",
		"eth_sendRawTransaction",
		"eth_call",
		"eth_estimateGas",
		"eth_getLogs",
		"eth_getBlockReceipts",
		"eth_subscribe",
		"eth_unsubscribe",
		"eth_newFilter",
		"eth_getFilterChanges",
		"eth_getFilterLogs",
		"eth_uninstallFilter",
		"web3_sha3",
		"eth_getProof",
	}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			// Call with no params.
			req := &Request{
				JSONRPC: "2.0",
				Method:  method,
				Params:  nil,
				ID:      json.RawMessage(`1`),
			}
			resp := api.HandleRequest(req)
			if resp.Error == nil {
				t.Fatalf("expected error for %s with no params, got nil", method)
			}
			if resp.Error.Code != ErrCodeInvalidParams {
				t.Fatalf("want error code %d, got %d for %s", ErrCodeInvalidParams, resp.Error.Code, method)
			}
		})
	}
}

// TestAPIResponse_JSONRPCField verifies the response always has
// jsonrpc set to "2.0".
func TestAPIResponse_JSONRPCField(t *testing.T) {
	api := NewEthAPI(newMockBackend())

	resp := callRPC(t, api, "eth_chainId")
	if resp.JSONRPC != "2.0" {
		t.Fatalf("want jsonrpc 2.0, got %s", resp.JSONRPC)
	}

	resp = callRPC(t, api, "nonexistent")
	if resp.JSONRPC != "2.0" {
		t.Fatalf("want jsonrpc 2.0 for error, got %s", resp.JSONRPC)
	}
}

// TestAPIResponse_IDPropagation verifies the request ID is propagated to
// the response.
func TestAPIResponse_IDPropagation(t *testing.T) {
	api := NewEthAPI(newMockBackend())

	ids := []string{`1`, `"abc"`, `null`, `42`}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			req := &Request{
				JSONRPC: "2.0",
				Method:  "eth_chainId",
				Params:  nil,
				ID:      json.RawMessage(id),
			}
			resp := api.HandleRequest(req)
			if string(resp.ID) != id {
				t.Fatalf("want ID %s, got %s", id, string(resp.ID))
			}
		})
	}
}

// TestAPI_Web3Sha3 tests the web3_sha3 method.
func TestAPI_Web3Sha3(t *testing.T) {
	api := NewEthAPI(newMockBackend())

	// Keccak256 of empty bytes.
	resp := callRPC(t, api, "web3_sha3", "0x")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	got, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	// The result should be a 0x-prefixed 64 hex char keccak hash.
	if len(got) != 66 {
		t.Fatalf("expected 66 char hex hash, got length %d: %s", len(got), got)
	}
}

// TestAPI_EthSyncing tests the eth_syncing method returns false when synced.
func TestAPI_EthSyncing(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_syncing")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	if resp.Result != false {
		t.Fatalf("expected false, got %v", resp.Result)
	}
}

// TestAPI_EthMaxPriorityFeePerGas tests the eth_maxPriorityFeePerGas method.
func TestAPI_EthMaxPriorityFeePerGas(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_maxPriorityFeePerGas")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	// Default is 1 Gwei = 0x3b9aca00
	if resp.Result != "0x3b9aca00" {
		t.Fatalf("want 0x3b9aca00, got %v", resp.Result)
	}
}

// TestAPI_EthFeeHistory tests the eth_feeHistory method.
func TestAPI_EthFeeHistory(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_feeHistory", "0x1", "latest")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	fh, ok := resp.Result.(*FeeHistoryResult)
	if !ok {
		t.Fatalf("result not *FeeHistoryResult: %T", resp.Result)
	}
	if fh.OldestBlock == "" {
		t.Fatal("oldestBlock should not be empty")
	}
	if len(fh.BaseFeePerGas) == 0 {
		t.Fatal("baseFeePerGas should not be empty")
	}
	if len(fh.GasUsedRatio) == 0 {
		t.Fatal("gasUsedRatio should not be empty")
	}
}

// TestAPI_EthFeeHistory_WithRewardPercentiles tests fee history with reward percentiles.
func TestAPI_EthFeeHistory_WithRewardPercentiles(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_feeHistory", "0x1", "latest", []float64{25.0, 75.0})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	fh, ok := resp.Result.(*FeeHistoryResult)
	if !ok {
		t.Fatalf("result not *FeeHistoryResult: %T", resp.Result)
	}
	if len(fh.Reward) == 0 {
		t.Fatal("reward should not be empty when percentiles given")
	}
	for i, r := range fh.Reward {
		if len(r) != 2 {
			t.Fatalf("reward[%d] has %d entries, want 2", i, len(r))
		}
	}
}

// TestAPI_EthFeeHistory_InvalidBlockCount tests fee history with invalid block count.
func TestAPI_EthFeeHistory_InvalidBlockCount(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	// blockCount of 0 should fail.
	resp := callRPC(t, api, "eth_feeHistory", "0x0", "latest")
	if resp.Error == nil {
		t.Fatal("expected error for blockCount 0")
	}
}

// TestAPI_Subscribe_NewHeads tests subscribing to newHeads notifications.
func TestAPI_Subscribe_NewHeads(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_subscribe", "newHeads")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	subID, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	if subID == "" {
		t.Fatal("subscription ID should not be empty")
	}
}

// TestAPI_Subscribe_Logs tests subscribing to logs notifications.
func TestAPI_Subscribe_Logs(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_subscribe", "logs")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	_, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
}

// TestAPI_Subscribe_UnsupportedType tests subscription with unsupported type.
func TestAPI_Subscribe_UnsupportedType(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_subscribe", "badType")
	if resp.Error == nil {
		t.Fatal("expected error for unsupported subscription type")
	}
}

// TestAPI_Unsubscribe tests unsubscribing.
func TestAPI_Unsubscribe(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_subscribe", "newHeads")
	if resp.Error != nil {
		t.Fatalf("subscribe error: %s", resp.Error.Message)
	}
	subID := resp.Result.(string)

	resp = callRPC(t, api, "eth_unsubscribe", subID)
	if resp.Error != nil {
		t.Fatalf("unsubscribe error: %s", resp.Error.Message)
	}
	if resp.Result != true {
		t.Fatalf("expected true, got %v", resp.Result)
	}

	resp = callRPC(t, api, "eth_unsubscribe", subID)
	if resp.Error != nil {
		t.Fatalf("unsubscribe error: %s", resp.Error.Message)
	}
	if resp.Result != false {
		t.Fatalf("expected false for double unsubscribe, got %v", resp.Result)
	}
}

// TestAPI_NewBlockFilter_RPC tests creating and polling a block filter via RPC.
func TestAPI_NewBlockFilter_RPC(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_newBlockFilter")
	if resp.Error != nil {
		t.Fatalf("error: %s", resp.Error.Message)
	}
	filterID, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	if filterID == "" {
		t.Fatal("filter ID should not be empty")
	}

	resp = callRPC(t, api, "eth_getFilterChanges", filterID)
	if resp.Error != nil {
		t.Fatalf("error: %s", resp.Error.Message)
	}

	resp = callRPC(t, api, "eth_uninstallFilter", filterID)
	if resp.Error != nil {
		t.Fatalf("error: %s", resp.Error.Message)
	}
	if resp.Result != true {
		t.Fatalf("expected true, got %v", resp.Result)
	}
}

// TestAPI_GetFilterChanges_NotFound tests getFilterChanges with non-existent filter ID.
func TestAPI_GetFilterChanges_NotFound(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_getFilterChanges", "0xdeadbeef")
	if resp.Error == nil {
		t.Fatal("expected error for non-existent filter")
	}
	if resp.Error.Code != ErrCodeInvalidParams {
		t.Fatalf("want error code %d, got %d", ErrCodeInvalidParams, resp.Error.Code)
	}
}

// TestAPI_CreateAccessList tests the eth_createAccessList method.
func TestAPI_CreateAccessList(t *testing.T) {
	mb := newMockBackend()
	mb.callGasUsed = 21000
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_createAccessList", map[string]interface{}{
		"from": "0x000000000000000000000000000000000000aaaa",
		"to":   "0x000000000000000000000000000000000000bbbb",
	})
	if resp.Error != nil {
		t.Fatalf("error: %s", resp.Error.Message)
	}
	result, ok := resp.Result.(*AccessListResult)
	if !ok {
		t.Fatalf("result not *AccessListResult: %T", resp.Result)
	}
	if len(result.AccessList) != 0 {
		t.Fatalf("expected empty access list, got %d entries", len(result.AccessList))
	}
	if result.GasUsed != "0x5208" {
		t.Fatalf("want gas 0x5208, got %s", result.GasUsed)
	}
}

// TestAPI_GetStorageAt tests the eth_getStorageAt method.
func TestAPI_GetStorageAt(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_getStorageAt",
		"0x000000000000000000000000000000000000aaaa",
		"0x0000000000000000000000000000000000000000000000000000000000000000",
		"latest",
	)
	if resp.Error != nil {
		t.Fatalf("error: %s", resp.Error.Message)
	}
	got, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	// Default storage is zero.
	if len(got) != 66 { // 0x + 64 hex chars
		t.Fatalf("expected 66-char hash, got %s", got)
	}
}

// TestAPI_HistoryPruned verifies EIP-4444 pruning checks.
func TestAPI_HistoryPruned(t *testing.T) {
	mb := newMockBackend()
	mb.historyOldest = 100 // blocks before 100 are pruned
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_getLogs", map[string]interface{}{
		"fromBlock": "0x1",
		"toBlock":   "0x5",
	})
	if resp.Error == nil {
		t.Fatal("expected error for pruned logs")
	}
	if resp.Error.Code != ErrCodeHistoryPruned {
		t.Fatalf("want error code %d, got %d", ErrCodeHistoryPruned, resp.Error.Code)
	}
}
