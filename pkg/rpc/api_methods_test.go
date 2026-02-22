package rpc

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// ---------- eth_getBlockReceipts ----------

func TestGetBlockReceipts_WithLogs(t *testing.T) {
	mb := newMockBackend()
	blockHash := mb.headers[42].Hash()

	contractAddr := types.HexToAddress("0xcccc")
	topic := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))

	receipt := &types.Receipt{
		Status:            types.ReceiptStatusSuccessful,
		CumulativeGasUsed: 21000,
		GasUsed:           21000,
		TxHash:            types.HexToHash("0x1111"),
		BlockHash:         blockHash,
		BlockNumber:       big.NewInt(42),
		TransactionIndex:  0,
		Logs: []*types.Log{
			{
				Address:     contractAddr,
				Topics:      []types.Hash{topic},
				Data:        []byte{0x01},
				BlockNumber: 42,
				BlockHash:   blockHash,
			},
		},
	}
	mb.receipts[blockHash] = []*types.Receipt{receipt}

	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getBlockReceipts", "latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	receipts, ok := resp.Result.([]*RPCReceipt)
	if !ok {
		t.Fatalf("result not []*RPCReceipt: %T", resp.Result)
	}
	if len(receipts) != 1 {
		t.Fatalf("want 1 receipt, got %d", len(receipts))
	}
	if len(receipts[0].Logs) != 1 {
		t.Fatalf("want 1 log in receipt, got %d", len(receipts[0].Logs))
	}
}

func TestGetBlockReceipts_EmptyBlock(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_getBlockReceipts", "latest")
	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	receipts, ok := resp.Result.([]*RPCReceipt)
	if !ok {
		t.Fatalf("result not []*RPCReceipt: %T", resp.Result)
	}
	if len(receipts) != 0 {
		t.Fatalf("want 0 receipts for empty block, got %d", len(receipts))
	}
}

// ---------- eth_maxPriorityFeePerGas ----------

func TestMaxPriorityFeePerGas(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_maxPriorityFeePerGas")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	got, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	// 1 Gwei = 1000000000 = 0x3b9aca00
	if got != "0x3b9aca00" {
		t.Fatalf("want 0x3b9aca00, got %v", got)
	}
}

// ---------- eth_feeHistory ----------

func TestFeeHistory(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)

	// Request 1 block of history ending at "latest" (block 42)
	resp := callRPC(t, api, "eth_feeHistory", "0x1", "latest", []float64{25, 75})

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
	// Should have 2 baseFeePerGas entries (blockCount + 1)
	if len(result.BaseFeePerGas) != 2 {
		t.Fatalf("want 2 baseFeePerGas entries, got %d", len(result.BaseFeePerGas))
	}
	// Should have 1 gasUsedRatio entry
	if len(result.GasUsedRatio) != 1 {
		t.Fatalf("want 1 gasUsedRatio entry, got %d", len(result.GasUsedRatio))
	}
	// gasUsedRatio should be 15000000/30000000 = 0.5
	if result.GasUsedRatio[0] != 0.5 {
		t.Fatalf("want gasUsedRatio 0.5, got %v", result.GasUsedRatio[0])
	}
	// Should have 1 reward entry with 2 percentiles
	if len(result.Reward) != 1 {
		t.Fatalf("want 1 reward entry, got %d", len(result.Reward))
	}
	if len(result.Reward[0]) != 2 {
		t.Fatalf("want 2 percentile values, got %d", len(result.Reward[0]))
	}
}

func TestFeeHistory_NoRewardPercentiles(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_feeHistory", "0x1", "latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	result := resp.Result.(*FeeHistoryResult)

	// No reward field when percentiles not requested
	if result.Reward != nil {
		t.Fatalf("want nil rewards, got %v", result.Reward)
	}
}

func TestFeeHistory_InvalidBlockCount(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_feeHistory", "0x0", "latest")

	if resp.Error == nil {
		t.Fatal("expected error for blockCount 0")
	}
}

// ---------- eth_syncing ----------

func TestSyncing(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_syncing")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	// Should return false when fully synced
	got, ok := resp.Result.(bool)
	if !ok {
		t.Fatalf("result not bool: %T", resp.Result)
	}
	if got != false {
		t.Fatalf("want false (synced), got %v", got)
	}
}

// ---------- eth_createAccessList ----------

func TestCreateAccessList(t *testing.T) {
	mb := newMockBackend()
	mb.callGasUsed = 21000
	api := NewEthAPI(mb)

	to := "0x000000000000000000000000000000000000bbbb"
	resp := callRPC(t, api, "eth_createAccessList", map[string]interface{}{
		"from": "0x000000000000000000000000000000000000aaaa",
		"to":   to,
		"data": "0x",
	}, "latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	result, ok := resp.Result.(*AccessListResult)
	if !ok {
		t.Fatalf("result not *AccessListResult: %T", resp.Result)
	}
	if result.GasUsed != "0x5208" { // 21000
		t.Fatalf("want gasUsed 0x5208, got %v", result.GasUsed)
	}
	if len(result.AccessList) != 0 {
		t.Fatalf("want empty access list, got %d entries", len(result.AccessList))
	}
}

func TestCreateAccessList_Error(t *testing.T) {
	mb := newMockBackend()
	mb.callErr = errCallFailed
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_createAccessList", map[string]interface{}{
		"to":   "0x000000000000000000000000000000000000bbbb",
		"data": "0x",
	}, "latest")

	if resp.Error == nil {
		t.Fatal("expected error for failed call")
	}
}

// ---------- WebSocket Subscriptions ----------

func TestSubscription_NewHeads(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)

	// Subscribe to newHeads
	resp := callRPC(t, api, "eth_subscribe", "newHeads")
	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	subID, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	if subID == "" {
		t.Fatal("expected non-empty subscription ID")
	}

	// Get the subscription and verify channel works
	sub := api.subs.GetSubscription(subID)
	if sub == nil {
		t.Fatal("subscription not found")
	}
	if sub.Type != SubNewHeads {
		t.Fatalf("want SubNewHeads, got %d", sub.Type)
	}

	// Notify a new head
	header := &types.Header{
		Number:  big.NewInt(100),
		BaseFee: big.NewInt(1000000000),
	}
	api.subs.NotifyNewHead(header)

	// Read from channel
	select {
	case msg := <-sub.Channel():
		block, ok := msg.(*RPCBlock)
		if !ok {
			t.Fatalf("notification not *RPCBlock: %T", msg)
		}
		if block.Number != "0x64" { // 100
			t.Fatalf("want block number 0x64, got %v", block.Number)
		}
	default:
		t.Fatal("expected notification on channel")
	}

	// Unsubscribe
	unsubResp := callRPC(t, api, "eth_unsubscribe", subID)
	if unsubResp.Error != nil {
		t.Fatalf("error: %v", unsubResp.Error.Message)
	}
	if unsubResp.Result != true {
		t.Fatalf("want true, got %v", unsubResp.Result)
	}

	// Verify subscription was removed
	if api.subs.SubscriptionCount() != 0 {
		t.Fatalf("want 0 subscriptions, got %d", api.subs.SubscriptionCount())
	}
}

func TestSubscription_Logs(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)

	contractAddr := types.HexToAddress("0xcccc")
	transferTopic := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))

	// Subscribe to logs from a specific contract
	resp := callRPC(t, api, "eth_subscribe", "logs", map[string]interface{}{
		"address": []string{encodeAddress(contractAddr)},
	})
	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	subID := resp.Result.(string)

	sub := api.subs.GetSubscription(subID)
	if sub == nil {
		t.Fatal("subscription not found")
	}

	// Notify matching and non-matching logs
	matchingLog := &types.Log{
		Address:     contractAddr,
		Topics:      []types.Hash{transferTopic},
		Data:        []byte{0x01},
		BlockNumber: 42,
	}
	nonMatchingLog := &types.Log{
		Address:     types.HexToAddress("0xdddd"),
		Topics:      []types.Hash{transferTopic},
		Data:        []byte{0x02},
		BlockNumber: 42,
	}
	api.subs.NotifyLogs([]*types.Log{matchingLog, nonMatchingLog})

	// Should only receive the matching log
	select {
	case msg := <-sub.Channel():
		rpcLog, ok := msg.(*RPCLog)
		if !ok {
			t.Fatalf("notification not *RPCLog: %T", msg)
		}
		if rpcLog.Address != encodeAddress(contractAddr) {
			t.Fatalf("want address %v, got %v", encodeAddress(contractAddr), rpcLog.Address)
		}
	default:
		t.Fatal("expected notification on channel for matching log")
	}

	// Non-matching log should not be on the channel
	select {
	case msg := <-sub.Channel():
		t.Fatalf("unexpected notification: %v", msg)
	default:
		// Good, nothing extra.
	}

	// Unsubscribe
	callRPC(t, api, "eth_unsubscribe", subID)
}

func TestSubscription_NewPendingTransactions(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_subscribe", "newPendingTransactions")
	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	subID := resp.Result.(string)
	sub := api.subs.GetSubscription(subID)

	// Notify a pending tx
	txHash := types.HexToHash("0xabcdef")
	api.subs.NotifyPendingTxHash(txHash)

	select {
	case msg := <-sub.Channel():
		hashStr, ok := msg.(string)
		if !ok {
			t.Fatalf("notification not string: %T", msg)
		}
		if hashStr != encodeHash(txHash) {
			t.Fatalf("want %v, got %v", encodeHash(txHash), hashStr)
		}
	default:
		t.Fatal("expected notification on channel")
	}

	callRPC(t, api, "eth_unsubscribe", subID)
}

func TestSubscription_InvalidType(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_subscribe", "invalidType")

	if resp.Error == nil {
		t.Fatal("expected error for invalid subscription type")
	}
}

func TestUnsubscribe_NonExistent(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_unsubscribe", "0xnonexistent")

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	if resp.Result != false {
		t.Fatalf("want false for non-existent subscription, got %v", resp.Result)
	}
}

// ---------- Filter poll-based methods (RPC integration) ----------

func TestFilter_GetFilterChanges(t *testing.T) {
	mb := newTestBackend()
	api := NewEthAPI(mb)

	// Create a block filter
	resp := callRPC(t, api, "eth_newBlockFilter")
	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	filterID := resp.Result.(string)

	// Notify a new block
	newHash := types.HexToHash("0xbeef")
	api.subs.NotifyNewBlock(newHash)

	// Get filter changes
	changes := callRPC(t, api, "eth_getFilterChanges", filterID)
	if changes.Error != nil {
		t.Fatalf("error: %v", changes.Error.Message)
	}
	hashes, ok := changes.Result.([]string)
	if !ok {
		t.Fatalf("result not []string: %T", changes.Result)
	}
	if len(hashes) != 1 {
		t.Fatalf("want 1 hash, got %d", len(hashes))
	}
	if hashes[0] != encodeHash(newHash) {
		t.Fatalf("want %v, got %v", encodeHash(newHash), hashes[0])
	}

	// Second poll: no new blocks
	changes2 := callRPC(t, api, "eth_getFilterChanges", filterID)
	if changes2.Error != nil {
		t.Fatalf("error: %v", changes2.Error.Message)
	}
	hashes2 := changes2.Result.([]string)
	if len(hashes2) != 0 {
		t.Fatalf("want 0 hashes, got %d", len(hashes2))
	}
}

func TestFilter_Uninstall(t *testing.T) {
	mb := newTestBackend()
	api := NewEthAPI(mb)

	// Create log filter
	resp := callRPC(t, api, "eth_newFilter", map[string]interface{}{
		"fromBlock": "0x2a",
		"toBlock":   "0x2a",
	})
	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	filterID := resp.Result.(string)

	// Uninstall
	uninstall := callRPC(t, api, "eth_uninstallFilter", filterID)
	if uninstall.Error != nil {
		t.Fatalf("error: %v", uninstall.Error.Message)
	}
	if uninstall.Result != true {
		t.Fatalf("want true, got %v", uninstall.Result)
	}

	// Verify it's gone
	uninstall2 := callRPC(t, api, "eth_uninstallFilter", filterID)
	if uninstall2.Result != false {
		t.Fatalf("want false for double uninstall, got %v", uninstall2.Result)
	}

	// GetFilterChanges on uninstalled filter should error
	changes := callRPC(t, api, "eth_getFilterChanges", filterID)
	if changes.Error == nil {
		t.Fatal("expected error for uninstalled filter")
	}
}

// ---------- WSNotification formatting ----------

func TestFormatWSNotification(t *testing.T) {
	notif := FormatWSNotification("0xabc123", map[string]string{"test": "value"})
	if notif.JSONRPC != "2.0" {
		t.Fatalf("want jsonrpc 2.0, got %v", notif.JSONRPC)
	}
	if notif.Method != "eth_subscription" {
		t.Fatalf("want method eth_subscription, got %v", notif.Method)
	}

	// Verify the params can be parsed
	var result WSSubscriptionResult
	if err := json.Unmarshal(notif.Params, &result); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if result.Subscription != "0xabc123" {
		t.Fatalf("want subscription 0xabc123, got %v", result.Subscription)
	}
}

// ---------- FormatBlock ----------

func TestFormatBlock_WithTxHashes(t *testing.T) {
	header := &types.Header{
		Number:  big.NewInt(10),
		BaseFee: big.NewInt(1000000000),
	}
	block := types.NewBlock(header, nil)
	result := FormatBlock(block, false)

	_, ok := result.(*RPCBlock)
	if !ok {
		t.Fatalf("expected *RPCBlock for fullTx=false, got %T", result)
	}
}
