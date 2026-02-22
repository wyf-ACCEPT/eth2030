package rpc

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// ---------- BlockNumber: safe and finalized tags ----------

func TestBlockNumber_UnmarshalSafe(t *testing.T) {
	var bn BlockNumber
	if err := json.Unmarshal([]byte(`"safe"`), &bn); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if bn != SafeBlockNumber {
		t.Fatalf("want SafeBlockNumber (%d), got %d", SafeBlockNumber, bn)
	}
}

func TestBlockNumber_UnmarshalFinalized(t *testing.T) {
	var bn BlockNumber
	if err := json.Unmarshal([]byte(`"finalized"`), &bn); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if bn != FinalizedBlockNumber {
		t.Fatalf("want FinalizedBlockNumber (%d), got %d", FinalizedBlockNumber, bn)
	}
}

func TestGetBalance_SafeBlock(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_getBalance",
		"0x000000000000000000000000000000000000aaaa", "safe")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	got, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	if got != "0xde0b6b3a7640000" {
		t.Fatalf("want 0xde0b6b3a7640000, got %v", got)
	}
}

func TestGetBalance_FinalizedBlock(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_getBalance",
		"0x000000000000000000000000000000000000aaaa", "finalized")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	got, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	if got != "0xde0b6b3a7640000" {
		t.Fatalf("want 0xde0b6b3a7640000, got %v", got)
	}
}

func TestGetBlockByNumber_Safe(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_getBlockByNumber", "safe", false)

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("expected non-nil result for safe block")
	}
	block, ok := resp.Result.(*RPCBlock)
	if !ok {
		t.Fatalf("result not *RPCBlock: %T", resp.Result)
	}
	if block.Number != "0x2a" { // 42
		t.Fatalf("want block number 0x2a, got %v", block.Number)
	}
}

func TestGetBlockByNumber_Finalized(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_getBlockByNumber", "finalized", false)

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("expected non-nil result for finalized block")
	}
	block, ok := resp.Result.(*RPCBlock)
	if !ok {
		t.Fatalf("result not *RPCBlock: %T", resp.Result)
	}
	if block.Number != "0x2a" { // 42
		t.Fatalf("want block number 0x2a, got %v", block.Number)
	}
}

func TestGetStorageAt_Safe(t *testing.T) {
	mb := newMockBackend()
	// Set a storage value.
	addr := types.HexToAddress("0xaaaa")
	slot := types.HexToHash("0x01")
	val := types.HexToHash("0x42")
	mb.statedb.SetState(addr, slot, val)

	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getStorageAt",
		"0x000000000000000000000000000000000000aaaa",
		"0x0000000000000000000000000000000000000000000000000000000000000001",
		"safe")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	got, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	expected := encodeHash(val)
	if got != expected {
		t.Fatalf("want %v, got %v", expected, got)
	}
}

func TestGetCode_Finalized(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_getCode",
		"0x000000000000000000000000000000000000aaaa", "finalized")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	if resp.Result != "0x6000" {
		t.Fatalf("want 0x6000, got %v", resp.Result)
	}
}

func TestGetTransactionCount_Safe(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_getTransactionCount",
		"0x000000000000000000000000000000000000aaaa", "safe")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	if resp.Result != "0x5" { // nonce 5
		t.Fatalf("want 0x5, got %v", resp.Result)
	}
}

// ---------- debug_traceCall ----------

func TestDebugTraceCall(t *testing.T) {
	mb := newMockBackend()
	mb.callResult = []byte{0xab, 0xcd}
	mb.callGasUsed = 21000
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "debug_traceCall", map[string]interface{}{
		"from": "0x000000000000000000000000000000000000aaaa",
		"to":   "0x000000000000000000000000000000000000bbbb",
		"data": "0x12345678",
	}, "latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	result, ok := resp.Result.(*TraceResult)
	if !ok {
		t.Fatalf("result not *TraceResult: %T", resp.Result)
	}
	if result.Gas != 21000 {
		t.Fatalf("want gas 21000, got %d", result.Gas)
	}
	if result.Failed {
		t.Fatal("call should not be marked as failed")
	}
	if result.ReturnValue != "0xabcd" {
		t.Fatalf("want returnValue 0xabcd, got %v", result.ReturnValue)
	}
	if result.StructLogs == nil {
		t.Fatal("structLogs should not be nil")
	}
}

func TestDebugTraceCall_DefaultBlockNumber(t *testing.T) {
	mb := newMockBackend()
	mb.callResult = []byte{0x01}
	mb.callGasUsed = 100
	api := NewEthAPI(mb)

	// Call without specifying block number (should default to latest).
	resp := callRPC(t, api, "debug_traceCall", map[string]interface{}{
		"to": "0x000000000000000000000000000000000000bbbb",
	})

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	result := resp.Result.(*TraceResult)
	if result.Gas != 100 {
		t.Fatalf("want gas 100, got %d", result.Gas)
	}
}

func TestDebugTraceCall_Error(t *testing.T) {
	mb := newMockBackend()
	mb.callErr = errCallFailed
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "debug_traceCall", map[string]interface{}{
		"to":   "0x000000000000000000000000000000000000bbbb",
		"data": "0x",
	}, "latest")

	if resp.Error == nil {
		t.Fatal("expected error for failed call")
	}
}

func TestDebugTraceCall_MissingParams(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "debug_traceCall")

	if resp.Error == nil {
		t.Fatal("expected error for missing parameters")
	}
	if resp.Error.Code != ErrCodeInvalidParams {
		t.Fatalf("want error code %d, got %d", ErrCodeInvalidParams, resp.Error.Code)
	}
}

func TestDebugTraceCall_WithValue(t *testing.T) {
	mb := newMockBackend()
	mb.callGasUsed = 21000
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "debug_traceCall", map[string]interface{}{
		"from":  "0x000000000000000000000000000000000000aaaa",
		"to":    "0x000000000000000000000000000000000000bbbb",
		"value": "0xde0b6b3a7640000",
		"gas":   "0x5208",
	}, "latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	result := resp.Result.(*TraceResult)
	if result.Gas != 21000 {
		t.Fatalf("want gas 21000, got %d", result.Gas)
	}
}

// ---------- eth_getUncleByBlockHashAndIndex ----------

func TestGetUncleByBlockHashAndIndex(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_getUncleByBlockHashAndIndex",
		"0x0000000000000000000000000000000000000000000000000000000000001234",
		"0x0")

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	// Post-merge: always returns null.
	if resp.Result != nil {
		t.Fatalf("want nil (no uncles post-merge), got %v", resp.Result)
	}
}

// ---------- eth_getUncleByBlockNumberAndIndex ----------

func TestGetUncleByBlockNumberAndIndex(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_getUncleByBlockNumberAndIndex",
		"0x2a", "0x0")

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	// Post-merge: always returns null.
	if resp.Result != nil {
		t.Fatalf("want nil (no uncles post-merge), got %v", resp.Result)
	}
}

// ---------- eth_feeHistory with safe/finalized ----------

func TestFeeHistory_SafeBlock(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_feeHistory", "0x1", "safe")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	result, ok := resp.Result.(*FeeHistoryResult)
	if !ok {
		t.Fatalf("result not *FeeHistoryResult: %T", resp.Result)
	}
	if result.OldestBlock != "0x2a" {
		t.Fatalf("want oldestBlock 0x2a, got %v", result.OldestBlock)
	}
}

func TestFeeHistory_FinalizedBlock(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_feeHistory", "0x1", "finalized")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	result, ok := resp.Result.(*FeeHistoryResult)
	if !ok {
		t.Fatalf("result not *FeeHistoryResult: %T", resp.Result)
	}
	if result.OldestBlock != "0x2a" {
		t.Fatalf("want oldestBlock 0x2a, got %v", result.OldestBlock)
	}
}

// ---------- eth_getProof with safe/finalized ----------

func TestGetProof_SafeBlock(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_getProof",
		"0x000000000000000000000000000000000000aaaa",
		[]string{},
		"safe")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	result, ok := resp.Result.(*AccountProof)
	if !ok {
		t.Fatalf("result not *AccountProof: %T", resp.Result)
	}
	// Should contain balance of 1e18.
	if result.Balance != "0xde0b6b3a7640000" {
		t.Fatalf("want balance 0xde0b6b3a7640000, got %v", result.Balance)
	}
	if result.Nonce != "0x5" {
		t.Fatalf("want nonce 0x5, got %v", result.Nonce)
	}
}

// ---------- eth_createAccessList with safe/finalized ----------

func TestCreateAccessList_SafeBlock(t *testing.T) {
	mb := newMockBackend()
	mb.callGasUsed = 21000
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_createAccessList", map[string]interface{}{
		"from": "0x000000000000000000000000000000000000aaaa",
		"to":   "0x000000000000000000000000000000000000bbbb",
	}, "safe")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	result, ok := resp.Result.(*AccessListResult)
	if !ok {
		t.Fatalf("result not *AccessListResult: %T", resp.Result)
	}
	if result.GasUsed != "0x5208" {
		t.Fatalf("want gasUsed 0x5208, got %v", result.GasUsed)
	}
}

// ---------- debug_traceCall with safe/finalized ----------

func TestDebugTraceCall_SafeBlock(t *testing.T) {
	mb := newMockBackend()
	mb.callGasUsed = 21000
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "debug_traceCall", map[string]interface{}{
		"to": "0x000000000000000000000000000000000000bbbb",
	}, "safe")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	result := resp.Result.(*TraceResult)
	if result.Gas != 21000 {
		t.Fatalf("want gas 21000, got %d", result.Gas)
	}
}

// ---------- dispatch routing for new methods ----------

func TestDispatcher_DebugTraceCall(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)
	resp := callRPC(t, api, "debug_traceCall", map[string]interface{}{
		"to": "0x000000000000000000000000000000000000bbbb",
	})
	if resp.Error != nil {
		t.Fatalf("expected debug_traceCall to be routed, got error: %v", resp.Error.Message)
	}
}

func TestDispatcher_GetUncleByBlockHashAndIndex(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_getUncleByBlockHashAndIndex",
		"0x0000000000000000000000000000000000000000000000000000000000000000",
		"0x0")
	if resp.Error != nil {
		t.Fatalf("expected eth_getUncleByBlockHashAndIndex to be routed, got error: %v", resp.Error.Message)
	}
}

func TestDispatcher_GetUncleByBlockNumberAndIndex(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_getUncleByBlockNumberAndIndex",
		"latest", "0x0")
	if resp.Error != nil {
		t.Fatalf("expected eth_getUncleByBlockNumberAndIndex to be routed, got error: %v", resp.Error.Message)
	}
}

// ---------- BlockNumber edge cases ----------

func TestBlockNumber_UnmarshalLatest(t *testing.T) {
	var bn BlockNumber
	if err := json.Unmarshal([]byte(`"latest"`), &bn); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if bn != LatestBlockNumber {
		t.Fatalf("want LatestBlockNumber (%d), got %d", LatestBlockNumber, bn)
	}
}

func TestBlockNumber_UnmarshalEarliest(t *testing.T) {
	var bn BlockNumber
	if err := json.Unmarshal([]byte(`"earliest"`), &bn); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if bn != EarliestBlockNumber {
		t.Fatalf("want EarliestBlockNumber (%d), got %d", EarliestBlockNumber, bn)
	}
}

func TestBlockNumber_UnmarshalPending(t *testing.T) {
	var bn BlockNumber
	if err := json.Unmarshal([]byte(`"pending"`), &bn); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if bn != PendingBlockNumber {
		t.Fatalf("want PendingBlockNumber (%d), got %d", PendingBlockNumber, bn)
	}
}

func TestBlockNumber_UnmarshalHex(t *testing.T) {
	var bn BlockNumber
	if err := json.Unmarshal([]byte(`"0x2a"`), &bn); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if bn != 42 {
		t.Fatalf("want 42, got %d", bn)
	}
}

func TestBlockNumber_UnmarshalInvalid(t *testing.T) {
	var bn BlockNumber
	err := json.Unmarshal([]byte(`"not_a_number"`), &bn)
	if err == nil {
		t.Fatal("expected error for invalid block number")
	}
}

// ---------- Combined receipt test: getTransactionReceipt with logs ----------

func TestGetTransactionReceipt_WithLogs(t *testing.T) {
	mb := newMockBackend()
	to := types.HexToAddress("0xbbbb")
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(1000000000),
		Gas:      42000,
		To:       &to,
		Value:    big.NewInt(0),
	})
	sender := types.HexToAddress("0xaaaa")
	tx.SetSender(sender)

	txHash := tx.Hash()
	mb.transactions[txHash] = &mockTxInfo{tx: tx, blockNum: 42, index: 0}

	blockHash := mb.headers[42].Hash()
	contractAddr := types.HexToAddress("0xcccc")
	topic := types.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")

	receipt := &types.Receipt{
		Status:            types.ReceiptStatusSuccessful,
		CumulativeGasUsed: 42000,
		GasUsed:           42000,
		TxHash:            txHash,
		BlockHash:         blockHash,
		BlockNumber:       big.NewInt(42),
		TransactionIndex:  0,
		Logs: []*types.Log{
			{
				Address:     contractAddr,
				Topics:      []types.Hash{topic},
				Data:        []byte{0xab, 0xcd},
				BlockNumber: 42,
				BlockHash:   blockHash,
				TxHash:      txHash,
				TxIndex:     0,
				Index:       0,
			},
			{
				Address:     contractAddr,
				Topics:      []types.Hash{topic},
				Data:        []byte{0xef},
				BlockNumber: 42,
				BlockHash:   blockHash,
				TxHash:      txHash,
				TxIndex:     0,
				Index:       1,
			},
		},
	}
	mb.receipts[blockHash] = []*types.Receipt{receipt}

	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getTransactionReceipt", encodeHash(txHash))

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	rpcReceipt, ok := resp.Result.(*RPCReceipt)
	if !ok {
		t.Fatalf("result not *RPCReceipt: %T", resp.Result)
	}
	if rpcReceipt.Status != "0x1" {
		t.Fatalf("want status 0x1, got %v", rpcReceipt.Status)
	}
	if len(rpcReceipt.Logs) != 2 {
		t.Fatalf("want 2 logs, got %d", len(rpcReceipt.Logs))
	}
	if rpcReceipt.Logs[0].LogIndex != "0x0" {
		t.Fatalf("want logIndex 0x0, got %v", rpcReceipt.Logs[0].LogIndex)
	}
	if rpcReceipt.Logs[1].LogIndex != "0x1" {
		t.Fatalf("want logIndex 0x1, got %v", rpcReceipt.Logs[1].LogIndex)
	}
	if rpcReceipt.From != encodeAddress(sender) {
		t.Fatalf("want from %v, got %v", encodeAddress(sender), rpcReceipt.From)
	}
	toStr := encodeAddress(to)
	if rpcReceipt.To == nil || *rpcReceipt.To != toStr {
		t.Fatalf("want to %v, got %v", toStr, rpcReceipt.To)
	}
}

// ---------- debug_traceCall JSON serialization ----------

func TestDebugTraceCall_JSONRoundTrip(t *testing.T) {
	mb := newMockBackend()
	mb.callResult = []byte{0x01, 0x02, 0x03}
	mb.callGasUsed = 50000
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "debug_traceCall", map[string]interface{}{
		"to":   "0x000000000000000000000000000000000000bbbb",
		"data": "0x12345678",
	}, "latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}

	// Marshal to JSON and back.
	data, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded TraceResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.Gas != 50000 {
		t.Fatalf("want gas 50000, got %d", decoded.Gas)
	}
	if decoded.Failed {
		t.Fatal("want failed=false")
	}
}
