package rpc

import (
	"errors"
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// TestEthDirectAPI_ChainID verifies ChainID returns hex chain ID.
func TestEthDirectAPI_ChainID(t *testing.T) {
	mb := newMockBackend()
	api := NewEthDirectAPI(mb)

	got := api.ChainID()
	if got != "0x539" { // 1337
		t.Fatalf("ChainID = %q, want 0x539", got)
	}
}

// TestEthDirectAPI_BlockNumber verifies BlockNumber returns the latest block.
func TestEthDirectAPI_BlockNumber(t *testing.T) {
	mb := newMockBackend()
	api := NewEthDirectAPI(mb)

	got, err := api.BlockNumber()
	if err != nil {
		t.Fatalf("BlockNumber error: %v", err)
	}
	if got != "0x2a" { // 42
		t.Fatalf("BlockNumber = %q, want 0x2a", got)
	}
}

// TestEthDirectAPI_BlockNumber_NoHeader tests error when no current header.
func TestEthDirectAPI_BlockNumber_NoHeader(t *testing.T) {
	mb := newMockBackend()
	delete(mb.headers, 42) // remove the only header
	api := NewEthDirectAPI(mb)

	_, err := api.BlockNumber()
	if !errors.Is(err, ErrNoCurrentBlock) {
		t.Fatalf("got %v, want ErrNoCurrentBlock", err)
	}
}

// TestEthDirectAPI_GetBalance verifies balance retrieval.
func TestEthDirectAPI_GetBalance(t *testing.T) {
	mb := newMockBackend()
	api := NewEthDirectAPI(mb)

	balance, err := api.GetBalance("0x000000000000000000000000000000000000aaaa", "latest")
	if err != nil {
		t.Fatalf("GetBalance error: %v", err)
	}
	// 1e18 = 0xde0b6b3a7640000
	if balance != "0xde0b6b3a7640000" {
		t.Fatalf("GetBalance = %q, want 0xde0b6b3a7640000", balance)
	}
}

// TestEthDirectAPI_GetBalance_BlockNotFound tests error on missing block.
func TestEthDirectAPI_GetBalance_BlockNotFound(t *testing.T) {
	mb := newMockBackend()
	api := NewEthDirectAPI(mb)

	_, err := api.GetBalance("0xaaaa", "0x999")
	if !errors.Is(err, ErrBlockNotFound) {
		t.Fatalf("got %v, want ErrBlockNotFound", err)
	}
}

// TestEthDirectAPI_GetTransactionCount verifies nonce retrieval.
func TestEthDirectAPI_GetTransactionCount(t *testing.T) {
	mb := newMockBackend()
	api := NewEthDirectAPI(mb)

	nonce, err := api.GetTransactionCount("0x000000000000000000000000000000000000aaaa", "latest")
	if err != nil {
		t.Fatalf("GetTransactionCount error: %v", err)
	}
	if nonce != "0x5" {
		t.Fatalf("GetTransactionCount = %q, want 0x5", nonce)
	}
}

// TestEthDirectAPI_GetBlockByNumber_Header tests header-only block retrieval.
func TestEthDirectAPI_GetBlockByNumber_Header(t *testing.T) {
	mb := newMockBackend()
	api := NewEthDirectAPI(mb)

	result, err := api.GetBlockByNumber("latest", false)
	if err != nil {
		t.Fatalf("GetBlockByNumber error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result["number"] != "0x2a" {
		t.Fatalf("number = %v, want 0x2a", result["number"])
	}
}

// TestEthDirectAPI_GetBlockByNumber_NotFound tests nil return for unknown block.
func TestEthDirectAPI_GetBlockByNumber_NotFound(t *testing.T) {
	mb := newMockBackend()
	api := NewEthDirectAPI(mb)

	result, err := api.GetBlockByNumber("0x999", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatal("expected nil for unknown block")
	}
}

// TestEthDirectAPI_GetBlockByNumber_FullTxs tests full block retrieval.
func TestEthDirectAPI_GetBlockByNumber_FullTxs(t *testing.T) {
	mb := newMockBackend()

	// Create a block with a transaction.
	header := mb.headers[42]
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(1e9),
		Gas:      21000,
		Value:    big.NewInt(1000),
	})
	block := types.NewBlock(header, &types.Body{
		Transactions: []*types.Transaction{tx},
	})
	mb.blocks[42] = block

	api := NewEthDirectAPI(mb)
	result, err := api.GetBlockByNumber("latest", true)
	if err != nil {
		t.Fatalf("GetBlockByNumber error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	txList, ok := result["transactions"].([]map[string]interface{})
	if !ok {
		t.Fatalf("transactions type = %T, want []map[string]interface{}", result["transactions"])
	}
	if len(txList) != 1 {
		t.Fatalf("transactions len = %d, want 1", len(txList))
	}
}

// TestEthDirectAPI_GetBlockByHash tests block retrieval by hash.
func TestEthDirectAPI_GetBlockByHash(t *testing.T) {
	mb := newMockBackend()
	api := NewEthDirectAPI(mb)

	header := mb.headers[42]
	hash := header.Hash()

	result, err := api.GetBlockByHash(encodeHash(hash), false)
	if err != nil {
		t.Fatalf("GetBlockByHash error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result["number"] != "0x2a" {
		t.Fatalf("number = %v, want 0x2a", result["number"])
	}
}

// TestEthDirectAPI_GetBlockByHash_NotFound tests nil return for unknown hash.
func TestEthDirectAPI_GetBlockByHash_NotFound(t *testing.T) {
	mb := newMockBackend()
	api := NewEthDirectAPI(mb)

	result, err := api.GetBlockByHash("0x0000000000000000000000000000000000000000000000000000000000000000", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatal("expected nil for unknown hash")
	}
}

// TestEthDirectAPI_GetTransactionByHash verifies tx lookup.
func TestEthDirectAPI_GetTransactionByHash(t *testing.T) {
	mb := newMockBackend()
	to := types.HexToAddress("0xbbbb")
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    7,
		GasPrice: big.NewInt(2e9),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(500),
	})
	sender := types.HexToAddress("0xaaaa")
	tx.SetSender(sender)

	txHash := tx.Hash()
	mb.transactions[txHash] = &mockTxInfo{tx: tx, blockNum: 42, index: 3}

	api := NewEthDirectAPI(mb)
	result, err := api.GetTransactionByHash(encodeHash(txHash))
	if err != nil {
		t.Fatalf("GetTransactionByHash error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result["nonce"] != "0x7" {
		t.Fatalf("nonce = %v, want 0x7", result["nonce"])
	}
	if result["transactionIndex"] != "0x3" {
		t.Fatalf("transactionIndex = %v, want 0x3", result["transactionIndex"])
	}
}

// TestEthDirectAPI_GetTransactionByHash_NotFound tests nil for unknown tx.
func TestEthDirectAPI_GetTransactionByHash_NotFound(t *testing.T) {
	mb := newMockBackend()
	api := NewEthDirectAPI(mb)

	result, err := api.GetTransactionByHash("0x0000000000000000000000000000000000000000000000000000000000001111")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatal("expected nil for unknown tx")
	}
}

// TestEthDirectAPI_GetTransactionReceipt verifies receipt lookup.
func TestEthDirectAPI_GetTransactionReceipt(t *testing.T) {
	mb := newMockBackend()
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(1e9),
		Gas:      21000,
	})
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

	api := NewEthDirectAPI(mb)
	result, err := api.GetTransactionReceipt(encodeHash(txHash))
	if err != nil {
		t.Fatalf("GetTransactionReceipt error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result["status"] != "0x1" {
		t.Fatalf("status = %v, want 0x1", result["status"])
	}
	if result["gasUsed"] != "0x5208" {
		t.Fatalf("gasUsed = %v, want 0x5208", result["gasUsed"])
	}
}

// TestEthDirectAPI_GetTransactionReceipt_NotFound tests nil for unknown receipt.
func TestEthDirectAPI_GetTransactionReceipt_NotFound(t *testing.T) {
	mb := newMockBackend()
	api := NewEthDirectAPI(mb)

	result, err := api.GetTransactionReceipt("0x0000000000000000000000000000000000000000000000000000000000001111")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatal("expected nil for unknown receipt")
	}
}

// TestEthDirectAPI_GasPrice verifies gas price suggestion.
func TestEthDirectAPI_GasPrice(t *testing.T) {
	mb := newMockBackend()
	api := NewEthDirectAPI(mb)

	price, err := api.GasPrice()
	if err != nil {
		t.Fatalf("GasPrice error: %v", err)
	}
	if price != "0x3b9aca00" { // 1 Gwei
		t.Fatalf("GasPrice = %q, want 0x3b9aca00", price)
	}
}

// TestEthDirectAPI_EstimateGas verifies gas estimation binary search.
func TestEthDirectAPI_EstimateGas(t *testing.T) {
	mb := newMockBackend()
	// Mock always succeeds, so estimation should find minimum (21000).
	api := NewEthDirectAPI(mb)

	result, err := api.EstimateGas(map[string]interface{}{
		"from": "0x000000000000000000000000000000000000aaaa",
		"to":   "0x000000000000000000000000000000000000bbbb",
	})
	if err != nil {
		t.Fatalf("EstimateGas error: %v", err)
	}
	if result != "0x5208" { // 21000
		t.Fatalf("EstimateGas = %q, want 0x5208", result)
	}
}

// TestEthDirectAPI_EstimateGas_Error tests estimation error.
func TestEthDirectAPI_EstimateGas_Error(t *testing.T) {
	mb := newMockBackend()
	mb.callErr = errCallFailed
	api := NewEthDirectAPI(mb)

	_, err := api.EstimateGas(map[string]interface{}{
		"to": "0x000000000000000000000000000000000000bbbb",
	})
	if !errors.Is(err, ErrExecutionFailed) {
		t.Fatalf("got %v, want ErrExecutionFailed", err)
	}
}

// TestEthDirectAPI_SendRawTransaction verifies tx submission.
func TestEthDirectAPI_SendRawTransaction(t *testing.T) {
	mb := newMockBackend()
	api := NewEthDirectAPI(mb)

	hash, err := api.SendRawTransaction("0xdeadbeef")
	if err != nil {
		t.Fatalf("SendRawTransaction error: %v", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
	if len(mb.sentTxs) != 1 {
		t.Fatalf("expected 1 sent tx, got %d", len(mb.sentTxs))
	}
}

// TestEthDirectAPI_SendRawTransaction_Empty tests empty data error.
func TestEthDirectAPI_SendRawTransaction_Empty(t *testing.T) {
	mb := newMockBackend()
	api := NewEthDirectAPI(mb)

	_, err := api.SendRawTransaction("0x")
	if !errors.Is(err, ErrEmptyTxData) {
		t.Fatalf("got %v, want ErrEmptyTxData", err)
	}
}

// TestEthDirectAPI_Call verifies read-only EVM call.
func TestEthDirectAPI_Call(t *testing.T) {
	mb := newMockBackend()
	mb.callResult = []byte{0xab, 0xcd}
	api := NewEthDirectAPI(mb)

	result, err := api.Call(map[string]interface{}{
		"to":   "0x000000000000000000000000000000000000bbbb",
		"data": "0x12345678",
	}, "latest")
	if err != nil {
		t.Fatalf("Call error: %v", err)
	}
	if result != "0xabcd" {
		t.Fatalf("Call = %q, want 0xabcd", result)
	}
}

// TestEthDirectAPI_Call_Error tests call execution error.
func TestEthDirectAPI_Call_Error(t *testing.T) {
	mb := newMockBackend()
	mb.callErr = errCallFailed
	api := NewEthDirectAPI(mb)

	_, err := api.Call(map[string]interface{}{
		"to": "0xbbbb",
	}, "latest")
	if !errors.Is(err, ErrExecutionFailed) {
		t.Fatalf("got %v, want ErrExecutionFailed", err)
	}
}

// TestEthDirectAPI_GetCode verifies code retrieval.
func TestEthDirectAPI_GetCode(t *testing.T) {
	mb := newMockBackend()
	api := NewEthDirectAPI(mb)

	code, err := api.GetCode("0x000000000000000000000000000000000000aaaa", "latest")
	if err != nil {
		t.Fatalf("GetCode error: %v", err)
	}
	if code != "0x6000" {
		t.Fatalf("GetCode = %q, want 0x6000", code)
	}
}

// TestEthDirectAPI_GetCode_Empty tests code for address with no code.
func TestEthDirectAPI_GetCode_Empty(t *testing.T) {
	mb := newMockBackend()
	api := NewEthDirectAPI(mb)

	code, err := api.GetCode("0x000000000000000000000000000000000000ffff", "latest")
	if err != nil {
		t.Fatalf("GetCode error: %v", err)
	}
	if code != "0x" {
		t.Fatalf("GetCode empty = %q, want 0x", code)
	}
}

// TestEthDirectAPI_GetStorageAt verifies storage retrieval.
func TestEthDirectAPI_GetStorageAt(t *testing.T) {
	mb := newMockBackend()
	api := NewEthDirectAPI(mb)

	// Default storage is zero.
	value, err := api.GetStorageAt(
		"0x000000000000000000000000000000000000aaaa",
		"0x0000000000000000000000000000000000000000000000000000000000000001",
		"latest",
	)
	if err != nil {
		t.Fatalf("GetStorageAt error: %v", err)
	}
	// Should be zero hash.
	expected := "0x0000000000000000000000000000000000000000000000000000000000000000"
	if value != expected {
		t.Fatalf("GetStorageAt = %q, want %q", value, expected)
	}
}

// TestParseBlockNumber tests various block number string parsing.
func TestParseBlockNumber(t *testing.T) {
	tests := []struct {
		input string
		want  BlockNumber
	}{
		{"latest", LatestBlockNumber},
		{"", LatestBlockNumber},
		{"pending", PendingBlockNumber},
		{"earliest", EarliestBlockNumber},
		{"safe", SafeBlockNumber},
		{"finalized", FinalizedBlockNumber},
		{"0x2a", BlockNumber(42)},
		{"0x0", BlockNumber(0)},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseBlockNumber(tt.input)
			if got != tt.want {
				t.Errorf("parseBlockNumber(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestExtractCallArgs tests extraction of call arguments from maps.
func TestExtractCallArgs(t *testing.T) {
	args := map[string]interface{}{
		"from":  "0x000000000000000000000000000000000000aaaa",
		"to":    "0x000000000000000000000000000000000000bbbb",
		"gas":   "0x5208",
		"value": "0x3e8",
		"data":  "0xdeadbeef",
	}

	from, to, gas, value, data := extractCallArgs(args)

	if from != types.HexToAddress("0xaaaa") {
		t.Errorf("from = %v, want 0xaaaa", from)
	}
	if to == nil {
		t.Fatal("to is nil")
	}
	if *to != types.HexToAddress("0xbbbb") {
		t.Errorf("to = %v, want 0xbbbb", *to)
	}
	if gas != 21000 {
		t.Errorf("gas = %d, want 21000", gas)
	}
	if value.Int64() != 1000 {
		t.Errorf("value = %d, want 1000", value.Int64())
	}
	if len(data) != 4 {
		t.Errorf("data len = %d, want 4", len(data))
	}
}

// TestExtractCallArgs_InputField tests the "input" field is used when "data" is absent.
func TestExtractCallArgs_InputField(t *testing.T) {
	args := map[string]interface{}{
		"input": "0xabcdef",
	}

	_, _, _, _, data := extractCallArgs(args)
	if len(data) != 3 {
		t.Errorf("data len = %d, want 3", len(data))
	}
}

// TestFormatHeaderMap tests header-to-map conversion.
func TestFormatHeaderMap(t *testing.T) {
	header := &types.Header{
		Number:   big.NewInt(100),
		GasLimit: 30000000,
		GasUsed:  15000000,
		Time:     1700000000,
		BaseFee:  big.NewInt(1e9),
	}

	m := formatHeaderMap(header)
	if m["number"] != "0x64" {
		t.Errorf("number = %v, want 0x64", m["number"])
	}
	if m["gasLimit"] != "0x1c9c380" {
		t.Errorf("gasLimit = %v, want 0x1c9c380", m["gasLimit"])
	}
	if m["baseFeePerGas"] != "0x3b9aca00" {
		t.Errorf("baseFeePerGas = %v, want 0x3b9aca00", m["baseFeePerGas"])
	}
}

// TestFormatTxAsMap tests transaction-to-map conversion.
func TestFormatTxAsMap(t *testing.T) {
	to := types.HexToAddress("0xbbbb")
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    5,
		GasPrice: big.NewInt(1e9),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(1000),
	})

	m := formatTxAsMap(tx)
	if m["nonce"] != "0x5" {
		t.Errorf("nonce = %v, want 0x5", m["nonce"])
	}
	if m["gas"] != "0x5208" {
		t.Errorf("gas = %v, want 0x5208", m["gas"])
	}
	if m["to"] == nil {
		t.Error("to should not be nil")
	}
}

// TestFormatTxAsMap_DynamicFee tests EIP-1559 tx fields in map.
func TestFormatTxAsMap_DynamicFee(t *testing.T) {
	tx := types.NewTransaction(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     3,
		GasTipCap: big.NewInt(2e9),
		GasFeeCap: big.NewInt(10e9),
		Gas:       21000,
		Value:     big.NewInt(0),
	})

	m := formatTxAsMap(tx)
	if _, ok := m["maxPriorityFeePerGas"]; !ok {
		t.Error("missing maxPriorityFeePerGas for dynamic fee tx")
	}
	if _, ok := m["maxFeePerGas"]; !ok {
		t.Error("missing maxFeePerGas for dynamic fee tx")
	}
}
