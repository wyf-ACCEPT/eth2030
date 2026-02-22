package rpc

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestNewTxPoolAPI(t *testing.T) {
	api := NewTxPoolAPI(newMockBackend())
	if api == nil {
		t.Fatal("expected non-nil TxPoolAPI")
	}
	if api.PendingCount() != 0 {
		t.Fatalf("want 0 pending, got %d", api.PendingCount())
	}
	if api.QueuedCount() != 0 {
		t.Fatalf("want 0 queued, got %d", api.QueuedCount())
	}
}

func TestComputeTxHash(t *testing.T) {
	data := []byte{0xde, 0xad, 0xbe, 0xef}
	hash := computeTxHash(data)
	if hash.IsZero() {
		t.Fatal("expected non-zero hash")
	}
	// Same data should produce the same hash.
	hash2 := computeTxHash(data)
	if hash != hash2 {
		t.Fatal("expected deterministic hash")
	}
	// Different data should produce a different hash.
	hash3 := computeTxHash([]byte{0x01, 0x02})
	if hash == hash3 {
		t.Fatal("expected different hash for different data")
	}
}

func TestDecodeTxType_Legacy(t *testing.T) {
	// RLP-encoded data starts with 0xc0+ range.
	data := []byte{0xc0, 0x01, 0x02}
	txType := decodeTxType(data)
	if txType != types.LegacyTxType {
		t.Fatalf("want LegacyTxType, got %d", txType)
	}
}

func TestDecodeTxType_Typed(t *testing.T) {
	// Type 0x02 (EIP-1559).
	data := []byte{0x02, 0xc0, 0x01}
	txType := decodeTxType(data)
	if txType != types.DynamicFeeTxType {
		t.Fatalf("want DynamicFeeTxType (0x02), got %d", txType)
	}
}

func TestDecodeTxType_BlobTx(t *testing.T) {
	data := []byte{0x03, 0xc0}
	txType := decodeTxType(data)
	if txType != types.BlobTxType {
		t.Fatalf("want BlobTxType (0x03), got %d", txType)
	}
}

func TestDecodeTxType_Empty(t *testing.T) {
	txType := decodeTxType(nil)
	if txType != types.LegacyTxType {
		t.Fatalf("want LegacyTxType for nil, got %d", txType)
	}
}

func TestDecodeRawTransaction_Valid(t *testing.T) {
	tx, raw, err := decodeRawTransaction("0xdeadbeef")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx == nil {
		t.Fatal("expected non-nil tx")
	}
	if len(raw) != 4 {
		t.Fatalf("want 4 raw bytes, got %d", len(raw))
	}
}

func TestDecodeRawTransaction_Empty(t *testing.T) {
	_, _, err := decodeRawTransaction("0x")
	if err == nil {
		t.Fatal("expected error for empty data")
	}
}

func TestDecodeRawTransaction_EmptyString(t *testing.T) {
	_, _, err := decodeRawTransaction("")
	if err == nil {
		t.Fatal("expected error for empty string")
	}
}

func TestTxPoolAPI_SendRawTransaction(t *testing.T) {
	mb := newMockBackend()
	api := NewTxPoolAPI(mb)
	ethAPI := NewEthAPI(mb)

	// Build a fake request.
	params := []json.RawMessage{json.RawMessage(`"0xdeadbeef"`)}
	req := &Request{JSONRPC: "2.0", Method: "eth_sendRawTransaction", Params: params, ID: json.RawMessage(`1`)}
	resp := api.SendRawTransaction(req)

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("expected non-nil result")
	}
	// Verify the transaction was submitted.
	if len(mb.sentTxs) != 1 {
		t.Fatalf("want 1 sent tx, got %d", len(mb.sentTxs))
	}
	// Verify it was tracked in pending.
	if api.PendingCount() != 1 {
		t.Fatalf("want 1 pending, got %d", api.PendingCount())
	}

	_ = ethAPI // just to ensure both APIs can coexist
}

func TestTxPoolAPI_SendRawTransaction_MissingParams(t *testing.T) {
	api := NewTxPoolAPI(newMockBackend())
	req := &Request{JSONRPC: "2.0", Method: "eth_sendRawTransaction", Params: nil, ID: json.RawMessage(`1`)}
	resp := api.SendRawTransaction(req)
	if resp.Error == nil {
		t.Fatal("expected error for missing params")
	}
}

func TestTxPoolAPI_SendRawTransaction_EmptyData(t *testing.T) {
	api := NewTxPoolAPI(newMockBackend())
	params := []json.RawMessage{json.RawMessage(`"0x"`)}
	req := &Request{JSONRPC: "2.0", Method: "eth_sendRawTransaction", Params: params, ID: json.RawMessage(`1`)}
	resp := api.SendRawTransaction(req)
	if resp.Error == nil {
		t.Fatal("expected error for empty data")
	}
}

func TestTxPoolAPI_GetTransactionByHash_FromChain(t *testing.T) {
	mb := newMockBackend()
	to := types.HexToAddress("0xbbbb")
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(1000),
	})
	sender := types.HexToAddress("0xaaaa")
	tx.SetSender(sender)

	txHash := tx.Hash()
	mb.transactions[txHash] = &mockTxInfo{tx: tx, blockNum: 42, index: 0}

	api := NewTxPoolAPI(mb)
	params := []json.RawMessage{json.RawMessage(`"` + encodeHash(txHash) + `"`)}
	req := &Request{JSONRPC: "2.0", Method: "eth_getTransactionByHash", Params: params, ID: json.RawMessage(`1`)}
	resp := api.GetTransactionByHash(req)

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestTxPoolAPI_GetTransactionByHash_FromPool(t *testing.T) {
	mb := newMockBackend()
	api := NewTxPoolAPI(mb)

	to := types.HexToAddress("0xbbbb")
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce: 1,
		Gas:   21000,
		To:    &to,
	})
	sender := types.HexToAddress("0xaaaa")
	api.AddPendingTransaction(sender, tx)

	txHash := tx.Hash()
	params := []json.RawMessage{json.RawMessage(`"` + encodeHash(txHash) + `"`)}
	req := &Request{JSONRPC: "2.0", Method: "eth_getTransactionByHash", Params: params, ID: json.RawMessage(`1`)}
	resp := api.GetTransactionByHash(req)

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("expected non-nil result for pool tx")
	}
}

func TestTxPoolAPI_GetTransactionByHash_NotFound(t *testing.T) {
	api := NewTxPoolAPI(newMockBackend())
	params := []json.RawMessage{json.RawMessage(`"0x0000000000000000000000000000000000000000000000000000000000000000"`)}
	req := &Request{JSONRPC: "2.0", Method: "eth_getTransactionByHash", Params: params, ID: json.RawMessage(`1`)}
	resp := api.GetTransactionByHash(req)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	if resp.Result != nil {
		t.Fatal("expected nil for non-existent tx")
	}
}

func TestTxPoolAPI_GetTransactionReceipt(t *testing.T) {
	mb := newMockBackend()
	to := types.HexToAddress("0xbbbb")
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(1000),
	})
	sender := types.HexToAddress("0xaaaa")
	tx.SetSender(sender)

	txHash := tx.Hash()
	mb.transactions[txHash] = &mockTxInfo{tx: tx, blockNum: 42, index: 0}

	blockHash := mb.headers[42].Hash()
	receipt := &types.Receipt{
		Status:            types.ReceiptStatusSuccessful,
		CumulativeGasUsed: 21000,
		GasUsed:           21000,
		TxHash:            txHash,
		BlockHash:         blockHash,
		BlockNumber:       big.NewInt(42),
		TransactionIndex:  0,
		Logs:              []*types.Log{},
	}
	mb.receipts[blockHash] = []*types.Receipt{receipt}

	api := NewTxPoolAPI(mb)
	params := []json.RawMessage{json.RawMessage(`"` + encodeHash(txHash) + `"`)}
	req := &Request{JSONRPC: "2.0", Method: "eth_getTransactionReceipt", Params: params, ID: json.RawMessage(`1`)}
	resp := api.GetTransactionReceipt(req)

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
}

func TestTxPoolAPI_GetTransactionReceipt_NotFound(t *testing.T) {
	api := NewTxPoolAPI(newMockBackend())
	params := []json.RawMessage{json.RawMessage(`"0x0000000000000000000000000000000000000000000000000000000000000000"`)}
	req := &Request{JSONRPC: "2.0", Method: "eth_getTransactionReceipt", Params: params, ID: json.RawMessage(`1`)}
	resp := api.GetTransactionReceipt(req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	if resp.Result != nil {
		t.Fatal("expected nil for non-existent receipt")
	}
}

func TestTxPoolAPI_Status_Empty(t *testing.T) {
	api := NewTxPoolAPI(newMockBackend())
	req := &Request{JSONRPC: "2.0", Method: "txpool_status", ID: json.RawMessage(`1`)}
	resp := api.Status(req)

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	result, ok := resp.Result.(*TxPoolStatusResult)
	if !ok {
		t.Fatalf("result not *TxPoolStatusResult: %T", resp.Result)
	}
	if result.Pending != "0x0" {
		t.Fatalf("want pending 0x0, got %s", result.Pending)
	}
	if result.Queued != "0x0" {
		t.Fatalf("want queued 0x0, got %s", result.Queued)
	}
}

func TestTxPoolAPI_Status_WithTxs(t *testing.T) {
	api := NewTxPoolAPI(newMockBackend())
	sender := types.HexToAddress("0xaaaa")

	// Add 3 pending and 2 queued.
	for i := 0; i < 3; i++ {
		tx := types.NewTransaction(&types.LegacyTx{Nonce: uint64(i), Gas: 21000})
		api.AddPendingTransaction(sender, tx)
	}
	for i := 0; i < 2; i++ {
		tx := types.NewTransaction(&types.LegacyTx{Nonce: uint64(100 + i), Gas: 21000})
		api.AddQueuedTransaction(sender, tx)
	}

	req := &Request{JSONRPC: "2.0", Method: "txpool_status", ID: json.RawMessage(`1`)}
	resp := api.Status(req)
	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	result := resp.Result.(*TxPoolStatusResult)
	if result.Pending != "0x3" {
		t.Fatalf("want pending 0x3, got %s", result.Pending)
	}
	if result.Queued != "0x2" {
		t.Fatalf("want queued 0x2, got %s", result.Queued)
	}
}

func TestTxPoolAPI_Content_Empty(t *testing.T) {
	api := NewTxPoolAPI(newMockBackend())
	req := &Request{JSONRPC: "2.0", Method: "txpool_content", ID: json.RawMessage(`1`)}
	resp := api.Content(req)

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	result, ok := resp.Result.(*TxPoolContentResult)
	if !ok {
		t.Fatalf("result not *TxPoolContentResult: %T", resp.Result)
	}
	if len(result.Pending) != 0 {
		t.Fatalf("want 0 pending senders, got %d", len(result.Pending))
	}
	if len(result.Queued) != 0 {
		t.Fatalf("want 0 queued senders, got %d", len(result.Queued))
	}
}

func TestTxPoolAPI_Content_WithTxs(t *testing.T) {
	api := NewTxPoolAPI(newMockBackend())
	sender := types.HexToAddress("0xaaaa")

	tx1 := types.NewTransaction(&types.LegacyTx{Nonce: 1, Gas: 21000})
	tx2 := types.NewTransaction(&types.LegacyTx{Nonce: 2, Gas: 21000})
	api.AddPendingTransaction(sender, tx1)
	api.AddPendingTransaction(sender, tx2)

	tx3 := types.NewTransaction(&types.LegacyTx{Nonce: 100, Gas: 21000})
	api.AddQueuedTransaction(sender, tx3)

	req := &Request{JSONRPC: "2.0", Method: "txpool_content", ID: json.RawMessage(`1`)}
	resp := api.Content(req)
	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	result := resp.Result.(*TxPoolContentResult)
	if len(result.Pending) != 1 {
		t.Fatalf("want 1 pending sender, got %d", len(result.Pending))
	}
	if len(result.Queued) != 1 {
		t.Fatalf("want 1 queued sender, got %d", len(result.Queued))
	}
}

func TestFormatTxPoolMap(t *testing.T) {
	txMap := make(map[types.Address][]*types.Transaction)
	sender := types.HexToAddress("0xaaaa")
	tx := types.NewTransaction(&types.LegacyTx{Nonce: 5, Gas: 21000})
	txMap[sender] = []*types.Transaction{tx}

	result := formatTxPoolMap(txMap)
	if len(result) != 1 {
		t.Fatalf("want 1 entry, got %d", len(result))
	}
	// Check that some entry exists.
	found := false
	for _, nonceMap := range result {
		if len(nonceMap) > 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("expected non-empty nonce map")
	}
}

func TestEffectiveGasPrice_Legacy(t *testing.T) {
	tx := types.NewTransaction(&types.LegacyTx{GasPrice: big.NewInt(5000)})
	price := EffectiveGasPrice(tx, big.NewInt(1000))
	if price.Int64() != 5000 {
		t.Fatalf("want 5000, got %d", price.Int64())
	}
}

func TestEffectiveGasPrice_EIP1559(t *testing.T) {
	tx := types.NewTransaction(&types.DynamicFeeTx{
		GasTipCap: big.NewInt(2000),
		GasFeeCap: big.NewInt(10000),
	})
	baseFee := big.NewInt(3000)
	price := EffectiveGasPrice(tx, baseFee)
	// effective = min(2000 + 3000, 10000) = 5000
	if price.Int64() != 5000 {
		t.Fatalf("want 5000, got %d", price.Int64())
	}
}

func TestEffectiveGasPrice_EIP1559_Capped(t *testing.T) {
	tx := types.NewTransaction(&types.DynamicFeeTx{
		GasTipCap: big.NewInt(8000),
		GasFeeCap: big.NewInt(10000),
	})
	baseFee := big.NewInt(5000)
	price := EffectiveGasPrice(tx, baseFee)
	// effective = min(8000 + 5000, 10000) = 10000
	if price.Int64() != 10000 {
		t.Fatalf("want 10000, got %d", price.Int64())
	}
}

func TestEffectiveGasPrice_NilBaseFee(t *testing.T) {
	tx := types.NewTransaction(&types.LegacyTx{GasPrice: big.NewInt(5000)})
	price := EffectiveGasPrice(tx, nil)
	if price.Int64() != 5000 {
		t.Fatalf("want 5000, got %d", price.Int64())
	}
}

func TestValidateTransaction_ZeroGas(t *testing.T) {
	api := NewTxPoolAPI(newMockBackend())
	tx := types.NewTransaction(&types.LegacyTx{Gas: 0})
	err := api.validateTransaction(tx)
	if err == nil {
		t.Fatal("expected error for zero gas")
	}
}

func TestValidateTransaction_NegativeValue(t *testing.T) {
	api := NewTxPoolAPI(newMockBackend())
	tx := types.NewTransaction(&types.LegacyTx{Gas: 21000, Value: big.NewInt(-1)})
	err := api.validateTransaction(tx)
	if err == nil {
		t.Fatal("expected error for negative value")
	}
}

func TestValidateTransaction_ValidTx(t *testing.T) {
	api := NewTxPoolAPI(newMockBackend())
	tx := types.NewTransaction(&types.LegacyTx{
		Gas:      21000,
		GasPrice: big.NewInt(1000),
		Value:    big.NewInt(100),
	})
	err := api.validateTransaction(tx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTxPoolAPI_AddPendingAndQueued(t *testing.T) {
	api := NewTxPoolAPI(newMockBackend())
	sender := types.HexToAddress("0xaaaa")

	tx1 := types.NewTransaction(&types.LegacyTx{Nonce: 0, Gas: 21000})
	tx2 := types.NewTransaction(&types.LegacyTx{Nonce: 1, Gas: 21000})

	api.AddPendingTransaction(sender, tx1)
	api.AddQueuedTransaction(sender, tx2)

	if api.PendingCount() != 1 {
		t.Fatalf("want 1 pending, got %d", api.PendingCount())
	}
	if api.QueuedCount() != 1 {
		t.Fatalf("want 1 queued, got %d", api.QueuedCount())
	}
}
