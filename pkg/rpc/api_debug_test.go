package rpc

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// ---------- debug_traceBlockByNumber ----------

func TestDebugTraceBlockByNumber(t *testing.T) {
	mb := newMockBackend()

	// Create two transactions in block 42.
	to := types.HexToAddress("0xbbbb")
	tx1 := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(100),
	})
	sender := types.HexToAddress("0xaaaa")
	tx1.SetSender(sender)

	tx2 := types.NewTransaction(&types.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(1000000000),
		Gas:      42000,
		To:       &to,
		Value:    big.NewInt(200),
	})
	tx2.SetSender(sender)

	// Build the block with these transactions.
	block := types.NewBlock(mb.headers[42], &types.Body{
		Transactions: []*types.Transaction{tx1, tx2},
	})
	mb.blocks[42] = block

	// Add receipts.
	blockHash := block.Hash()
	mb.receipts[blockHash] = []*types.Receipt{
		{
			Status:            types.ReceiptStatusSuccessful,
			GasUsed:           21000,
			CumulativeGasUsed: 21000,
			TxHash:            tx1.Hash(),
			BlockHash:         blockHash,
			BlockNumber:       big.NewInt(42),
			TransactionIndex:  0,
			Logs:              []*types.Log{},
		},
		{
			Status:            types.ReceiptStatusFailed,
			GasUsed:           15000,
			CumulativeGasUsed: 36000,
			TxHash:            tx2.Hash(),
			BlockHash:         blockHash,
			BlockNumber:       big.NewInt(42),
			TransactionIndex:  1,
			Logs:              []*types.Log{},
		},
	}

	api := NewEthAPI(mb)
	resp := callRPC(t, api, "debug_traceBlockByNumber", "0x2a")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}

	results, ok := resp.Result.([]*BlockTraceResult)
	if !ok {
		t.Fatalf("result not []*BlockTraceResult: %T", resp.Result)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 trace results, got %d", len(results))
	}

	// First tx should succeed with 21000 gas.
	if results[0].Result.Gas != 21000 {
		t.Fatalf("want gas 21000 for tx1, got %d", results[0].Result.Gas)
	}
	if results[0].Result.Failed {
		t.Fatal("tx1 should not be marked as failed")
	}
	if results[0].TxHash != encodeHash(tx1.Hash()) {
		t.Fatalf("want tx1 hash %v, got %v", encodeHash(tx1.Hash()), results[0].TxHash)
	}

	// Second tx should fail with 15000 gas.
	if results[1].Result.Gas != 15000 {
		t.Fatalf("want gas 15000 for tx2, got %d", results[1].Result.Gas)
	}
	if !results[1].Result.Failed {
		t.Fatal("tx2 should be marked as failed")
	}
}

func TestDebugTraceBlockByNumber_Latest(t *testing.T) {
	mb := newMockBackend()

	to := types.HexToAddress("0xbbbb")
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(100),
	})

	block := types.NewBlock(mb.headers[42], &types.Body{
		Transactions: []*types.Transaction{tx},
	})
	mb.blocks[42] = block

	blockHash := block.Hash()
	mb.receipts[blockHash] = []*types.Receipt{
		{
			Status:  types.ReceiptStatusSuccessful,
			GasUsed: 21000,
			TxHash:  tx.Hash(),
			Logs:    []*types.Log{},
		},
	}

	api := NewEthAPI(mb)
	resp := callRPC(t, api, "debug_traceBlockByNumber", "latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	results := resp.Result.([]*BlockTraceResult)
	if len(results) != 1 {
		t.Fatalf("want 1 trace result, got %d", len(results))
	}
}

func TestDebugTraceBlockByNumber_EmptyBlock(t *testing.T) {
	mb := newMockBackend()

	block := types.NewBlock(mb.headers[42], nil)
	mb.blocks[42] = block

	api := NewEthAPI(mb)
	resp := callRPC(t, api, "debug_traceBlockByNumber", "0x2a")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	results := resp.Result.([]*BlockTraceResult)
	if len(results) != 0 {
		t.Fatalf("want 0 trace results for empty block, got %d", len(results))
	}
}

func TestDebugTraceBlockByNumber_NotFound(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "debug_traceBlockByNumber", "0x999")
	if resp.Error == nil {
		t.Fatal("expected error for non-existent block")
	}
}

func TestDebugTraceBlockByNumber_MissingParam(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "debug_traceBlockByNumber")
	if resp.Error == nil {
		t.Fatal("expected error for missing parameter")
	}
	if resp.Error.Code != ErrCodeInvalidParams {
		t.Fatalf("want error code %d, got %d", ErrCodeInvalidParams, resp.Error.Code)
	}
}

// ---------- debug_traceBlockByHash ----------

func TestDebugTraceBlockByHash(t *testing.T) {
	mb := newMockBackend()

	to := types.HexToAddress("0xbbbb")
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(100),
	})
	sender := types.HexToAddress("0xaaaa")
	tx.SetSender(sender)

	block := types.NewBlock(mb.headers[42], &types.Body{
		Transactions: []*types.Transaction{tx},
	})
	mb.blocks[42] = block

	blockHash := block.Hash()
	mb.receipts[blockHash] = []*types.Receipt{
		{
			Status:            types.ReceiptStatusSuccessful,
			GasUsed:           21000,
			CumulativeGasUsed: 21000,
			TxHash:            tx.Hash(),
			BlockHash:         blockHash,
			BlockNumber:       big.NewInt(42),
			TransactionIndex:  0,
			Logs:              []*types.Log{},
		},
	}

	api := NewEthAPI(mb)
	resp := callRPC(t, api, "debug_traceBlockByHash", encodeHash(blockHash))

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}

	results, ok := resp.Result.([]*BlockTraceResult)
	if !ok {
		t.Fatalf("result not []*BlockTraceResult: %T", resp.Result)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 trace result, got %d", len(results))
	}
	if results[0].Result.Gas != 21000 {
		t.Fatalf("want gas 21000, got %d", results[0].Result.Gas)
	}
	if results[0].Result.Failed {
		t.Fatal("tx should not be marked as failed")
	}
	if results[0].TxHash != encodeHash(tx.Hash()) {
		t.Fatalf("want tx hash %v, got %v", encodeHash(tx.Hash()), results[0].TxHash)
	}
}

func TestDebugTraceBlockByHash_NotFound(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "debug_traceBlockByHash",
		"0x0000000000000000000000000000000000000000000000000000000000000000")
	if resp.Error == nil {
		t.Fatal("expected error for non-existent block")
	}
}

func TestDebugTraceBlockByHash_MissingParam(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "debug_traceBlockByHash")
	if resp.Error == nil {
		t.Fatal("expected error for missing parameter")
	}
	if resp.Error.Code != ErrCodeInvalidParams {
		t.Fatalf("want error code %d, got %d", ErrCodeInvalidParams, resp.Error.Code)
	}
}

// ---------- debug_traceBlockByNumber with no receipts ----------

func TestDebugTraceBlockByNumber_NoReceipts(t *testing.T) {
	mb := newMockBackend()

	to := types.HexToAddress("0xbbbb")
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(100),
	})

	block := types.NewBlock(mb.headers[42], &types.Body{
		Transactions: []*types.Transaction{tx},
	})
	mb.blocks[42] = block
	// No receipts stored -- should fallback to tx.Gas().

	api := NewEthAPI(mb)
	resp := callRPC(t, api, "debug_traceBlockByNumber", "0x2a")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	results := resp.Result.([]*BlockTraceResult)
	if len(results) != 1 {
		t.Fatalf("want 1 trace result, got %d", len(results))
	}
	// Without receipts, falls back to tx gas limit.
	if results[0].Result.Gas != 21000 {
		t.Fatalf("want gas 21000, got %d", results[0].Result.Gas)
	}
}

// ---------- eth_getBlockReceipts enhanced fields ----------

func TestGetBlockReceipts_EffectiveGasPrice(t *testing.T) {
	mb := newMockBackend()
	blockHash := mb.headers[42].Hash()

	receipt := &types.Receipt{
		Status:            types.ReceiptStatusSuccessful,
		Type:              types.DynamicFeeTxType,
		CumulativeGasUsed: 21000,
		GasUsed:           21000,
		EffectiveGasPrice: big.NewInt(2000000000), // 2 Gwei
		TxHash:            types.HexToHash("0x1111"),
		BlockHash:         blockHash,
		BlockNumber:       big.NewInt(42),
		TransactionIndex:  0,
		Logs:              []*types.Log{},
	}
	mb.receipts[blockHash] = []*types.Receipt{receipt}

	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getBlockReceipts", "0x2a")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}

	receipts := resp.Result.([]*RPCReceipt)
	if len(receipts) != 1 {
		t.Fatalf("want 1 receipt, got %d", len(receipts))
	}

	r := receipts[0]
	// Check type field.
	if r.Type != "0x2" { // DynamicFeeTxType = 0x02
		t.Fatalf("want type 0x2, got %v", r.Type)
	}

	// Check effectiveGasPrice.
	if r.EffectiveGasPrice != "0x77359400" { // 2000000000 = 0x77359400
		t.Fatalf("want effectiveGasPrice 0x77359400, got %v", r.EffectiveGasPrice)
	}

	// Check status.
	if r.Status != "0x1" {
		t.Fatalf("want status 0x1, got %v", r.Status)
	}
}

func TestGetBlockReceipts_EIP4844Fields(t *testing.T) {
	mb := newMockBackend()
	blockHash := mb.headers[42].Hash()

	receipt := &types.Receipt{
		Status:            types.ReceiptStatusSuccessful,
		Type:              types.BlobTxType,
		CumulativeGasUsed: 21000,
		GasUsed:           21000,
		EffectiveGasPrice: big.NewInt(1000000000),
		BlobGasUsed:       131072, // 1 blob = 2^17
		BlobGasPrice:      big.NewInt(1000),
		TxHash:            types.HexToHash("0x3333"),
		BlockHash:         blockHash,
		BlockNumber:       big.NewInt(42),
		TransactionIndex:  0,
		Logs:              []*types.Log{},
	}
	mb.receipts[blockHash] = []*types.Receipt{receipt}

	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getBlockReceipts", "0x2a")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}

	receipts := resp.Result.([]*RPCReceipt)
	if len(receipts) != 1 {
		t.Fatalf("want 1 receipt, got %d", len(receipts))
	}

	r := receipts[0]

	// Check type field for blob tx.
	if r.Type != "0x3" { // BlobTxType = 0x03
		t.Fatalf("want type 0x3, got %v", r.Type)
	}

	// Check blobGasUsed.
	if r.BlobGasUsed == nil {
		t.Fatal("expected blobGasUsed to be set")
	}
	if *r.BlobGasUsed != "0x20000" { // 131072 = 0x20000
		t.Fatalf("want blobGasUsed 0x20000, got %v", *r.BlobGasUsed)
	}

	// Check blobGasPrice.
	if r.BlobGasPrice == nil {
		t.Fatal("expected blobGasPrice to be set")
	}
	if *r.BlobGasPrice != "0x3e8" { // 1000 = 0x3e8
		t.Fatalf("want blobGasPrice 0x3e8, got %v", *r.BlobGasPrice)
	}
}

func TestGetBlockReceipts_NoBlobFields_LegacyTx(t *testing.T) {
	mb := newMockBackend()
	blockHash := mb.headers[42].Hash()

	receipt := &types.Receipt{
		Status:            types.ReceiptStatusSuccessful,
		Type:              types.LegacyTxType,
		CumulativeGasUsed: 21000,
		GasUsed:           21000,
		EffectiveGasPrice: big.NewInt(1000000000),
		TxHash:            types.HexToHash("0x4444"),
		BlockHash:         blockHash,
		BlockNumber:       big.NewInt(42),
		TransactionIndex:  0,
		Logs:              []*types.Log{},
	}
	mb.receipts[blockHash] = []*types.Receipt{receipt}

	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getBlockReceipts", "0x2a")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}

	receipts := resp.Result.([]*RPCReceipt)
	r := receipts[0]

	// Legacy tx should NOT have blob fields.
	if r.BlobGasUsed != nil {
		t.Fatalf("want nil blobGasUsed for legacy tx, got %v", *r.BlobGasUsed)
	}
	if r.BlobGasPrice != nil {
		t.Fatalf("want nil blobGasPrice for legacy tx, got %v", *r.BlobGasPrice)
	}

	// Type should be 0x0.
	if r.Type != "0x0" {
		t.Fatalf("want type 0x0, got %v", r.Type)
	}
}

func TestGetBlockReceipts_LogIndexing(t *testing.T) {
	mb := newMockBackend()
	blockHash := mb.headers[42].Hash()

	contractAddr := types.HexToAddress("0xcccc")
	topic := types.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")

	receipt1 := &types.Receipt{
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
				TxIndex:     0,
				Index:       0,
			},
			{
				Address:     contractAddr,
				Topics:      []types.Hash{topic},
				Data:        []byte{0x02},
				BlockNumber: 42,
				BlockHash:   blockHash,
				TxIndex:     0,
				Index:       1,
			},
		},
	}
	receipt2 := &types.Receipt{
		Status:            types.ReceiptStatusSuccessful,
		CumulativeGasUsed: 42000,
		GasUsed:           21000,
		TxHash:            types.HexToHash("0x2222"),
		BlockHash:         blockHash,
		BlockNumber:       big.NewInt(42),
		TransactionIndex:  1,
		Logs: []*types.Log{
			{
				Address:     contractAddr,
				Topics:      []types.Hash{topic},
				Data:        []byte{0x03},
				BlockNumber: 42,
				BlockHash:   blockHash,
				TxIndex:     1,
				Index:       2,
			},
		},
	}
	mb.receipts[blockHash] = []*types.Receipt{receipt1, receipt2}

	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getBlockReceipts", "0x2a")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}

	receipts := resp.Result.([]*RPCReceipt)
	if len(receipts) != 2 {
		t.Fatalf("want 2 receipts, got %d", len(receipts))
	}

	// Check log indexing across receipts.
	if len(receipts[0].Logs) != 2 {
		t.Fatalf("want 2 logs in receipt 0, got %d", len(receipts[0].Logs))
	}
	if receipts[0].Logs[0].LogIndex != "0x0" {
		t.Fatalf("want logIndex 0x0 for first log, got %v", receipts[0].Logs[0].LogIndex)
	}
	if receipts[0].Logs[1].LogIndex != "0x1" {
		t.Fatalf("want logIndex 0x1 for second log, got %v", receipts[0].Logs[1].LogIndex)
	}

	if len(receipts[1].Logs) != 1 {
		t.Fatalf("want 1 log in receipt 1, got %d", len(receipts[1].Logs))
	}
	if receipts[1].Logs[0].LogIndex != "0x2" {
		t.Fatalf("want logIndex 0x2 for third log, got %v", receipts[1].Logs[0].LogIndex)
	}

	// Verify transaction index is properly set on logs.
	if receipts[0].Logs[0].TransactionIndex != "0x0" {
		t.Fatalf("want txIndex 0x0 on log, got %v", receipts[0].Logs[0].TransactionIndex)
	}
	if receipts[1].Logs[0].TransactionIndex != "0x1" {
		t.Fatalf("want txIndex 0x1 on log, got %v", receipts[1].Logs[0].TransactionIndex)
	}
}

// ---------- JSON serialization round-trip ----------

func TestBlockTraceResult_JSON(t *testing.T) {
	result := &BlockTraceResult{
		TxHash: "0x1234",
		Result: &TraceResult{
			Gas:         21000,
			Failed:      false,
			ReturnValue: "",
			StructLogs:  []StructLog{},
		},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded BlockTraceResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.TxHash != "0x1234" {
		t.Fatalf("want txHash 0x1234, got %v", decoded.TxHash)
	}
	if decoded.Result.Gas != 21000 {
		t.Fatalf("want gas 21000, got %d", decoded.Result.Gas)
	}
}

func TestRPCReceipt_JSON_WithBlobFields(t *testing.T) {
	bgu := "0x20000"
	bgp := "0x3e8"
	receipt := &RPCReceipt{
		TransactionHash:   "0x1111",
		TransactionIndex:  "0x0",
		BlockHash:         "0x2222",
		BlockNumber:       "0x2a",
		GasUsed:           "0x5208",
		CumulativeGasUsed: "0x5208",
		Status:            "0x1",
		LogsBloom:         "0x00",
		Type:              "0x3",
		EffectiveGasPrice: "0x3b9aca00",
		BlobGasUsed:       &bgu,
		BlobGasPrice:      &bgp,
		Logs:              []*RPCLog{},
	}

	data, err := json.Marshal(receipt)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	// Verify blob fields are present.
	if decoded["blobGasUsed"] != "0x20000" {
		t.Fatalf("want blobGasUsed 0x20000, got %v", decoded["blobGasUsed"])
	}
	if decoded["blobGasPrice"] != "0x3e8" {
		t.Fatalf("want blobGasPrice 0x3e8, got %v", decoded["blobGasPrice"])
	}
	// Verify type and effectiveGasPrice are present.
	if decoded["type"] != "0x3" {
		t.Fatalf("want type 0x3, got %v", decoded["type"])
	}
	if decoded["effectiveGasPrice"] != "0x3b9aca00" {
		t.Fatalf("want effectiveGasPrice 0x3b9aca00, got %v", decoded["effectiveGasPrice"])
	}
}

func TestRPCReceipt_JSON_NoBlobFields(t *testing.T) {
	receipt := &RPCReceipt{
		TransactionHash:   "0x1111",
		TransactionIndex:  "0x0",
		BlockHash:         "0x2222",
		BlockNumber:       "0x2a",
		GasUsed:           "0x5208",
		CumulativeGasUsed: "0x5208",
		Status:            "0x1",
		LogsBloom:         "0x00",
		Type:              "0x0",
		EffectiveGasPrice: "0x3b9aca00",
		Logs:              []*RPCLog{},
	}

	data, err := json.Marshal(receipt)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	// Blob fields should be omitted for non-blob tx.
	if _, exists := decoded["blobGasUsed"]; exists {
		t.Fatal("blobGasUsed should be omitted for non-blob tx")
	}
	if _, exists := decoded["blobGasPrice"]; exists {
		t.Fatal("blobGasPrice should be omitted for non-blob tx")
	}
}

// ---------- Dispatcher routing ----------

func TestDispatcher_DebugTraceBlockByNumber(t *testing.T) {
	mb := newMockBackend()
	block := types.NewBlock(mb.headers[42], nil)
	mb.blocks[42] = block

	api := NewEthAPI(mb)
	resp := callRPC(t, api, "debug_traceBlockByNumber", "0x2a")
	if resp.Error != nil {
		t.Fatalf("expected debug_traceBlockByNumber to be routed, got error: %v", resp.Error.Message)
	}
}

func TestDispatcher_DebugTraceBlockByHash(t *testing.T) {
	mb := newMockBackend()
	block := types.NewBlock(mb.headers[42], nil)
	mb.blocks[42] = block

	api := NewEthAPI(mb)
	blockHash := block.Hash()
	resp := callRPC(t, api, "debug_traceBlockByHash", encodeHash(blockHash))
	if resp.Error != nil {
		t.Fatalf("expected debug_traceBlockByHash to be routed, got error: %v", resp.Error.Message)
	}
}
