package rpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// RPC conformance test suite.
// Validates JSON-RPC 2.0 method behavior against the execution-apis specification.

// --- Helpers ---

// newConformanceBackend returns a mockBackend configured for conformance tests
// with two blocks (0 genesis, 42 current) and a funded account.
func newConformanceBackend() *mockBackend {
	mb := newMockBackend()

	// Add genesis block header (block 0).
	genesis := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 30000000,
		GasUsed:  0,
		Time:     1600000000,
		BaseFee:  big.NewInt(1000000000),
	}
	mb.headers[0] = genesis

	return mb
}

// conformanceCall issues a JSON-RPC call and returns the raw Response.
func conformanceCall(t *testing.T, api *EthAPI, method string, params ...interface{}) *Response {
	t.Helper()
	return callRPC(t, api, method, params...)
}

// requireSuccess asserts no error in the response.
func requireSuccess(t *testing.T, resp *Response) {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("unexpected error: code=%d, msg=%s", resp.Error.Code, resp.Error.Message)
	}
}

// requireError asserts an error with the given code.
func requireError(t *testing.T, resp *Response, code int) {
	t.Helper()
	if resp.Error == nil {
		t.Fatal("expected error response, got success")
	}
	if resp.Error.Code != code {
		t.Fatalf("expected error code %d, got %d (msg: %s)", code, resp.Error.Code, resp.Error.Message)
	}
}

// mustString asserts the result is a string and returns it.
func mustString(t *testing.T, resp *Response) string {
	t.Helper()
	s, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("expected string result, got %T: %v", resp.Result, resp.Result)
	}
	return s
}

// --- JSON-RPC 2.0 Envelope Format Tests ---

func TestConformance_Envelope_JSONRPC20(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())
	resp := conformanceCall(t, api, "eth_chainId")

	if resp.JSONRPC != "2.0" {
		t.Fatalf("expected jsonrpc 2.0, got %q", resp.JSONRPC)
	}
}

func TestConformance_Envelope_IDPreserved(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	// Test with integer ID.
	req := &Request{
		JSONRPC: "2.0",
		Method:  "eth_chainId",
		Params:  nil,
		ID:      json.RawMessage(`42`),
	}
	resp := api.HandleRequest(req)
	if string(resp.ID) != "42" {
		t.Fatalf("expected ID 42, got %s", string(resp.ID))
	}

	// Test with string ID.
	req.ID = json.RawMessage(`"abc"`)
	resp = api.HandleRequest(req)
	if string(resp.ID) != `"abc"` {
		t.Fatalf("expected ID \"abc\", got %s", string(resp.ID))
	}
}

func TestConformance_Envelope_ResultOrError(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	// Success: result present, error absent.
	resp := conformanceCall(t, api, "eth_chainId")
	if resp.Result == nil {
		t.Fatal("expected result in success response")
	}
	if resp.Error != nil {
		t.Fatal("expected no error in success response")
	}

	// Error: error present, result absent.
	resp = conformanceCall(t, api, "eth_nonexistent")
	if resp.Error == nil {
		t.Fatal("expected error in error response")
	}
}

// --- eth_chainId ---

func TestConformance_ChainId(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())
	resp := conformanceCall(t, api, "eth_chainId")
	requireSuccess(t, resp)

	chainID := mustString(t, resp)
	// chainID should be hex-encoded: 1337 = 0x539
	if chainID != "0x539" {
		t.Fatalf("expected chain ID 0x539, got %s", chainID)
	}
}

// --- eth_blockNumber ---

func TestConformance_BlockNumber(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())
	resp := conformanceCall(t, api, "eth_blockNumber")
	requireSuccess(t, resp)

	blockNum := mustString(t, resp)
	// Current block is 42 = 0x2a
	if blockNum != "0x2a" {
		t.Fatalf("expected block number 0x2a, got %s", blockNum)
	}
}

// --- eth_getBalance ---

func TestConformance_GetBalance(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	// Account 0xaaaa has 1e18 wei.
	resp := conformanceCall(t, api, "eth_getBalance",
		"0x000000000000000000000000000000000000aaaa", "latest")
	requireSuccess(t, resp)

	balance := mustString(t, resp)
	if balance != "0xde0b6b3a7640000" {
		t.Fatalf("expected balance 0xde0b6b3a7640000, got %s", balance)
	}
}

func TestConformance_GetBalance_ZeroAddress(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	// Zero address should have zero balance.
	resp := conformanceCall(t, api, "eth_getBalance",
		"0x0000000000000000000000000000000000000000", "latest")
	requireSuccess(t, resp)

	balance := mustString(t, resp)
	if balance != "0x0" {
		t.Fatalf("expected zero balance, got %s", balance)
	}
}

func TestConformance_GetBalance_MissingParams(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	resp := conformanceCall(t, api, "eth_getBalance", "0x000000000000000000000000000000000000aaaa")
	requireError(t, resp, ErrCodeInvalidParams)
}

// --- eth_getTransactionCount ---

func TestConformance_GetTransactionCount(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	resp := conformanceCall(t, api, "eth_getTransactionCount",
		"0x000000000000000000000000000000000000aaaa", "latest")
	requireSuccess(t, resp)

	nonce := mustString(t, resp)
	if nonce != "0x5" {
		t.Fatalf("expected nonce 0x5, got %s", nonce)
	}
}

func TestConformance_GetTransactionCount_ZeroAddress(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	resp := conformanceCall(t, api, "eth_getTransactionCount",
		"0x0000000000000000000000000000000000000000", "latest")
	requireSuccess(t, resp)

	nonce := mustString(t, resp)
	if nonce != "0x0" {
		t.Fatalf("expected nonce 0x0, got %s", nonce)
	}
}

// --- eth_getBlockByHash ---

func TestConformance_GetBlockByHash(t *testing.T) {
	mb := newConformanceBackend()
	api := NewEthAPI(mb)

	header := mb.headers[42]
	hash := header.Hash()
	hashHex := encodeHash(hash)

	// Without full transactions.
	resp := conformanceCall(t, api, "eth_getBlockByHash", hashHex, false)
	requireSuccess(t, resp)
	if resp.Result == nil {
		t.Fatal("expected non-nil block result")
	}

	block, ok := resp.Result.(*RPCBlock)
	if !ok {
		t.Fatalf("expected *RPCBlock, got %T", resp.Result)
	}
	if block.Number != "0x2a" {
		t.Fatalf("expected block number 0x2a, got %s", block.Number)
	}
}

func TestConformance_GetBlockByHash_NotFound(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	resp := conformanceCall(t, api, "eth_getBlockByHash",
		"0x0000000000000000000000000000000000000000000000000000000000000000", false)
	requireSuccess(t, resp)
	if resp.Result != nil {
		t.Fatal("expected nil result for non-existent block")
	}
}

// --- eth_getBlockByNumber ---

func TestConformance_GetBlockByNumber_Latest(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	resp := conformanceCall(t, api, "eth_getBlockByNumber", "latest", false)
	requireSuccess(t, resp)
	if resp.Result == nil {
		t.Fatal("expected non-nil block result")
	}
}

func TestConformance_GetBlockByNumber_Earliest(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	resp := conformanceCall(t, api, "eth_getBlockByNumber", "earliest", false)
	requireSuccess(t, resp)
	if resp.Result == nil {
		t.Fatal("expected non-nil block result for genesis")
	}
}

func TestConformance_GetBlockByNumber_Hex(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	resp := conformanceCall(t, api, "eth_getBlockByNumber", "0x2a", false)
	requireSuccess(t, resp)
	if resp.Result == nil {
		t.Fatal("expected non-nil result for block 42")
	}
}

func TestConformance_GetBlockByNumber_NotFound(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	resp := conformanceCall(t, api, "eth_getBlockByNumber", "0x9999", false)
	requireSuccess(t, resp)
	if resp.Result != nil {
		t.Fatal("expected nil result for non-existent block")
	}
}

func TestConformance_GetBlockByNumber_FullTx(t *testing.T) {
	mb := newConformanceBackend()

	// Create a block with transactions.
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

	header := mb.headers[42]
	block := types.NewBlock(header, &types.Body{Transactions: []*types.Transaction{tx}})
	mb.blocks[42] = block

	api := NewEthAPI(mb)
	resp := conformanceCall(t, api, "eth_getBlockByNumber", "latest", true)
	requireSuccess(t, resp)

	fullBlock, ok := resp.Result.(*RPCBlockWithTxs)
	if !ok {
		t.Fatalf("expected *RPCBlockWithTxs, got %T", resp.Result)
	}
	if len(fullBlock.Transactions) != 1 {
		t.Fatalf("expected 1 transaction, got %d", len(fullBlock.Transactions))
	}
}

// --- eth_getTransactionByHash ---

func TestConformance_GetTransactionByHash(t *testing.T) {
	mb := newConformanceBackend()

	to := types.HexToAddress("0xbbbb")
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    7,
		GasPrice: big.NewInt(2000000000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(500),
	})
	sender := types.HexToAddress("0xaaaa")
	tx.SetSender(sender)

	txHash := tx.Hash()
	mb.transactions[txHash] = &mockTxInfo{tx: tx, blockNum: 42, index: 0}

	api := NewEthAPI(mb)
	resp := conformanceCall(t, api, "eth_getTransactionByHash", encodeHash(txHash))
	requireSuccess(t, resp)

	rpcTx, ok := resp.Result.(*RPCTransaction)
	if !ok {
		t.Fatalf("expected *RPCTransaction, got %T", resp.Result)
	}
	if rpcTx.Nonce != "0x7" {
		t.Fatalf("expected nonce 0x7, got %s", rpcTx.Nonce)
	}
	if rpcTx.Hash != encodeHash(txHash) {
		t.Fatalf("hash mismatch: got %s", rpcTx.Hash)
	}
}

func TestConformance_GetTransactionByHash_NotFound(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	resp := conformanceCall(t, api, "eth_getTransactionByHash",
		"0x0000000000000000000000000000000000000000000000000000000000001234")
	requireSuccess(t, resp)
	if resp.Result != nil {
		t.Fatal("expected nil result for missing transaction")
	}
}

// --- eth_getTransactionReceipt ---

func TestConformance_GetTransactionReceipt(t *testing.T) {
	mb := newConformanceBackend()

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

	api := NewEthAPI(mb)
	resp := conformanceCall(t, api, "eth_getTransactionReceipt", encodeHash(txHash))
	requireSuccess(t, resp)

	rpcReceipt, ok := resp.Result.(*RPCReceipt)
	if !ok {
		t.Fatalf("expected *RPCReceipt, got %T", resp.Result)
	}
	if rpcReceipt.Status != "0x1" {
		t.Fatalf("expected status 0x1, got %s", rpcReceipt.Status)
	}
	if rpcReceipt.TransactionHash != encodeHash(txHash) {
		t.Fatalf("tx hash mismatch")
	}
	if rpcReceipt.GasUsed != "0x5208" {
		t.Fatalf("expected gasUsed 0x5208, got %s", rpcReceipt.GasUsed)
	}
}

func TestConformance_GetTransactionReceipt_NotFound(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	resp := conformanceCall(t, api, "eth_getTransactionReceipt",
		"0x0000000000000000000000000000000000000000000000000000000000000000")
	requireSuccess(t, resp)
	if resp.Result != nil {
		t.Fatal("expected nil result for missing receipt")
	}
}

// --- eth_call ---

func TestConformance_EthCall_Success(t *testing.T) {
	mb := newConformanceBackend()
	mb.callResult = []byte{0x00, 0x00, 0x00, 0x2a} // returns 42
	api := NewEthAPI(mb)

	resp := conformanceCall(t, api, "eth_call", map[string]interface{}{
		"to":   "0x000000000000000000000000000000000000bbbb",
		"data": "0x12345678",
	}, "latest")
	requireSuccess(t, resp)

	result := mustString(t, resp)
	if result != "0x0000002a" {
		t.Fatalf("expected 0x0000002a, got %s", result)
	}
}

func TestConformance_EthCall_Error(t *testing.T) {
	mb := newConformanceBackend()
	mb.callErr = errors.New("execution reverted")
	api := NewEthAPI(mb)

	resp := conformanceCall(t, api, "eth_call", map[string]interface{}{
		"to":   "0x000000000000000000000000000000000000bbbb",
		"data": "0x",
	}, "latest")
	requireError(t, resp, ErrCodeInternal)
}

func TestConformance_EthCall_EmptyData(t *testing.T) {
	mb := newConformanceBackend()
	mb.callResult = []byte{}
	api := NewEthAPI(mb)

	resp := conformanceCall(t, api, "eth_call", map[string]interface{}{
		"to": "0x000000000000000000000000000000000000bbbb",
	}, "latest")
	requireSuccess(t, resp)

	result := mustString(t, resp)
	if result != "0x" {
		t.Fatalf("expected 0x for empty result, got %s", result)
	}
}

// --- eth_estimateGas ---

func TestConformance_EstimateGas(t *testing.T) {
	mb := newConformanceBackend()
	// Mock always succeeds, so estimateGas binary search should return 21000.
	api := NewEthAPI(mb)

	resp := conformanceCall(t, api, "eth_estimateGas", map[string]interface{}{
		"from": "0x000000000000000000000000000000000000aaaa",
		"to":   "0x000000000000000000000000000000000000bbbb",
	}, "latest")
	requireSuccess(t, resp)

	gas := mustString(t, resp)
	if gas != "0x5208" { // 21000
		t.Fatalf("expected 0x5208, got %s", gas)
	}
}

func TestConformance_EstimateGas_MissingParams(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	resp := conformanceCall(t, api, "eth_estimateGas")
	requireError(t, resp, ErrCodeInvalidParams)
}

// --- eth_gasPrice ---

func TestConformance_GasPrice(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	resp := conformanceCall(t, api, "eth_gasPrice")
	requireSuccess(t, resp)

	price := mustString(t, resp)
	if price != "0x3b9aca00" { // 1 Gwei
		t.Fatalf("expected 0x3b9aca00, got %s", price)
	}
}

// --- eth_getLogs ---

func TestConformance_GetLogs_Empty(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	resp := conformanceCall(t, api, "eth_getLogs", map[string]interface{}{
		"fromBlock": "0x2a",
		"toBlock":   "0x2a",
	})
	requireSuccess(t, resp)

	logs, ok := resp.Result.([]*RPCLog)
	if !ok {
		t.Fatalf("expected []*RPCLog, got %T", resp.Result)
	}
	if len(logs) != 0 {
		t.Fatalf("expected 0 logs, got %d", len(logs))
	}
}

func TestConformance_GetLogs_WithLogs(t *testing.T) {
	mb := newConformanceBackend()
	blockHash := mb.headers[42].Hash()

	topic := types.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")
	addr := types.HexToAddress("0xcccc")

	mb.logs[blockHash] = []*types.Log{
		{Address: addr, Topics: []types.Hash{topic}, Data: []byte{0x01},
			BlockNumber: 42, BlockHash: blockHash, TxIndex: 0, Index: 0},
		{Address: addr, Topics: []types.Hash{topic}, Data: []byte{0x02},
			BlockNumber: 42, BlockHash: blockHash, TxIndex: 1, Index: 1},
	}

	api := NewEthAPI(mb)
	resp := conformanceCall(t, api, "eth_getLogs", map[string]interface{}{
		"fromBlock": "0x2a",
		"toBlock":   "0x2a",
	})
	requireSuccess(t, resp)

	logs, ok := resp.Result.([]*RPCLog)
	if !ok {
		t.Fatalf("expected []*RPCLog, got %T", resp.Result)
	}
	if len(logs) != 2 {
		t.Fatalf("expected 2 logs, got %d", len(logs))
	}
}

func TestConformance_GetLogs_AddressFilter(t *testing.T) {
	mb := newConformanceBackend()
	blockHash := mb.headers[42].Hash()

	addr1 := types.HexToAddress("0xcccc")
	addr2 := types.HexToAddress("0xdddd")

	mb.logs[blockHash] = []*types.Log{
		{Address: addr1, BlockNumber: 42, BlockHash: blockHash, Index: 0},
		{Address: addr2, BlockNumber: 42, BlockHash: blockHash, Index: 1},
	}

	api := NewEthAPI(mb)
	resp := conformanceCall(t, api, "eth_getLogs", map[string]interface{}{
		"fromBlock": "0x2a",
		"toBlock":   "0x2a",
		"address":   []string{encodeAddress(addr2)},
	})
	requireSuccess(t, resp)

	logs, ok := resp.Result.([]*RPCLog)
	if !ok {
		t.Fatalf("expected []*RPCLog, got %T", resp.Result)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log filtered by address, got %d", len(logs))
	}
}

func TestConformance_GetLogs_TopicFilter(t *testing.T) {
	mb := newConformanceBackend()
	blockHash := mb.headers[42].Hash()

	topic1 := types.HexToHash("0xaaa1")
	topic2 := types.HexToHash("0xaaa2")
	addr := types.HexToAddress("0xcccc")

	mb.logs[blockHash] = []*types.Log{
		{Address: addr, Topics: []types.Hash{topic1}, BlockNumber: 42, BlockHash: blockHash, Index: 0},
		{Address: addr, Topics: []types.Hash{topic2}, BlockNumber: 42, BlockHash: blockHash, Index: 1},
	}

	api := NewEthAPI(mb)
	resp := conformanceCall(t, api, "eth_getLogs", map[string]interface{}{
		"fromBlock": "0x2a",
		"toBlock":   "0x2a",
		"topics":    [][]string{{encodeHash(topic1)}},
	})
	requireSuccess(t, resp)

	logs, ok := resp.Result.([]*RPCLog)
	if !ok {
		t.Fatalf("expected []*RPCLog, got %T", resp.Result)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log filtered by topic, got %d", len(logs))
	}
}

func TestConformance_GetLogs_MissingParams(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	resp := conformanceCall(t, api, "eth_getLogs")
	requireError(t, resp, ErrCodeInvalidParams)
}

// --- eth_sendRawTransaction ---

func TestConformance_SendRawTransaction_Valid(t *testing.T) {
	mb := newConformanceBackend()
	api := NewEthAPI(mb)

	resp := conformanceCall(t, api, "eth_sendRawTransaction", "0xdeadbeefcafe")
	requireSuccess(t, resp)

	txHash := mustString(t, resp)
	if !strings.HasPrefix(txHash, "0x") {
		t.Fatalf("expected hex-encoded hash, got %s", txHash)
	}
	if len(mb.sentTxs) != 1 {
		t.Fatalf("expected 1 sent tx, got %d", len(mb.sentTxs))
	}
}

func TestConformance_SendRawTransaction_EmptyData(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	resp := conformanceCall(t, api, "eth_sendRawTransaction", "0x")
	requireError(t, resp, ErrCodeInvalidParams)
}

func TestConformance_SendRawTransaction_MissingParams(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	resp := conformanceCall(t, api, "eth_sendRawTransaction")
	requireError(t, resp, ErrCodeInvalidParams)
}

func TestConformance_SendRawTransaction_BackendError(t *testing.T) {
	mb := newConformanceBackend()
	mb2 := &sendErrBackend{mockBackend: mb, sendErr: errors.New("nonce too low")}

	api := NewEthAPI(mb2)
	resp := conformanceCall(t, api, "eth_sendRawTransaction", "0xdeadbeef")
	requireError(t, resp, ErrCodeInternal)
}

// sendErrBackend wraps mockBackend to return an error on SendTransaction.
type sendErrBackend struct {
	*mockBackend
	sendErr error
}

func (b *sendErrBackend) SendTransaction(tx *types.Transaction) error {
	return b.sendErr
}

// --- Error Response Tests ---

func TestConformance_Error_InvalidParams(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	// eth_getBalance without enough params.
	resp := conformanceCall(t, api, "eth_getBalance")
	requireError(t, resp, ErrCodeInvalidParams)
}

func TestConformance_Error_MethodNotFound(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	resp := conformanceCall(t, api, "eth_doesNotExist")
	requireError(t, resp, ErrCodeMethodNotFound)
	if !strings.Contains(resp.Error.Message, "not found") {
		t.Fatalf("expected 'not found' in message, got %q", resp.Error.Message)
	}
}

func TestConformance_Error_InternalError(t *testing.T) {
	mb := newConformanceBackend()
	mb.callErr = errors.New("internal failure")
	api := NewEthAPI(mb)

	resp := conformanceCall(t, api, "eth_call", map[string]interface{}{
		"to": "0x000000000000000000000000000000000000bbbb",
	}, "latest")
	requireError(t, resp, ErrCodeInternal)
}

// --- Batch Requests (HTTP-level) ---

func TestConformance_BatchRequest(t *testing.T) {
	srv := NewServer(newConformanceBackend())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	batch := `[
		{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1},
		{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":2},
		{"jsonrpc":"2.0","method":"eth_gasPrice","params":[],"id":3}
	]`

	resp, err := http.Post(ts.URL, "application/json", bytes.NewBufferString(batch))
	if err != nil {
		t.Fatalf("HTTP POST: %v", err)
	}
	defer resp.Body.Close()

	// The server handles single requests; batch processing is optional.
	// Per JSON-RPC 2.0 spec, a server that does not support batches should
	// return a single error response. Verify the server doesn't crash.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// --- Edge Cases ---

func TestConformance_EdgeCase_MaxUint256Balance(t *testing.T) {
	mb := newConformanceBackend()

	// Set a huge balance (max uint256 - simulated with a large number).
	maxAddr := types.HexToAddress("0xffff")
	maxBalance := new(big.Int)
	maxBalance.SetString("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", 16)
	mb.statedb.AddBalance(maxAddr, maxBalance)

	api := NewEthAPI(mb)
	resp := conformanceCall(t, api, "eth_getBalance",
		"0x000000000000000000000000000000000000ffff", "latest")
	requireSuccess(t, resp)

	balance := mustString(t, resp)
	if !strings.HasPrefix(balance, "0x") {
		t.Fatalf("expected hex-encoded balance, got %s", balance)
	}
	// Verify the balance is properly encoded (non-zero, starts with 0xff...).
	if !strings.HasPrefix(balance, "0xffffff") {
		t.Fatalf("expected large balance, got %s", balance)
	}
}

func TestConformance_EdgeCase_GenesisBlock(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	resp := conformanceCall(t, api, "eth_getBlockByNumber", "0x0", false)
	requireSuccess(t, resp)
	if resp.Result == nil {
		t.Fatal("expected genesis block, got nil")
	}

	block, ok := resp.Result.(*RPCBlock)
	if !ok {
		t.Fatalf("expected *RPCBlock, got %T", resp.Result)
	}
	if block.Number != "0x0" {
		t.Fatalf("expected block 0, got %s", block.Number)
	}
}

func TestConformance_EdgeCase_BlockNumberTags(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	tags := []string{"latest", "safe", "finalized"}
	for _, tag := range tags {
		t.Run(tag, func(t *testing.T) {
			resp := conformanceCall(t, api, "eth_getBlockByNumber", tag, false)
			requireSuccess(t, resp)
			if resp.Result == nil {
				t.Fatalf("expected block for tag %q", tag)
			}
		})
	}
}

func TestConformance_EdgeCase_PendingBlockNumber(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	// "pending" may return nil if not supported; just verify no crash.
	resp := conformanceCall(t, api, "eth_getBlockByNumber", "pending", false)
	// No assertion on result; just verify the call doesn't panic.
	_ = resp
}

// --- web3_ and net_ Methods ---

func TestConformance_Web3ClientVersion(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	resp := conformanceCall(t, api, "web3_clientVersion")
	requireSuccess(t, resp)

	version := mustString(t, resp)
	if !strings.HasPrefix(version, "ETH2030") {
		t.Fatalf("expected ETH2030 prefix, got %s", version)
	}
}

func TestConformance_NetVersion(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	resp := conformanceCall(t, api, "net_version")
	requireSuccess(t, resp)

	version := mustString(t, resp)
	if version != "1337" {
		t.Fatalf("expected 1337, got %s", version)
	}
}

func TestConformance_NetListening(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	resp := conformanceCall(t, api, "net_listening")
	requireSuccess(t, resp)
	if resp.Result != true {
		t.Fatalf("expected true, got %v", resp.Result)
	}
}

// --- eth_syncing ---

func TestConformance_Syncing(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	resp := conformanceCall(t, api, "eth_syncing")
	requireSuccess(t, resp)
	// When fully synced, returns false.
	if resp.Result != false {
		t.Fatalf("expected false (fully synced), got %v", resp.Result)
	}
}

// --- HTTP Server Tests ---

func TestConformance_HTTP_InvalidJSON(t *testing.T) {
	srv := NewServer(newConformanceBackend())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL, "application/json", bytes.NewBufferString(`{invalid json`))
	if err != nil {
		t.Fatalf("HTTP POST: %v", err)
	}
	defer resp.Body.Close()

	var rpcResp Response
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected parse error")
	}
	if rpcResp.Error.Code != ErrCodeParse {
		t.Fatalf("expected parse error code %d, got %d", ErrCodeParse, rpcResp.Error.Code)
	}
}

func TestConformance_HTTP_MethodNotAllowed(t *testing.T) {
	srv := NewServer(newConformanceBackend())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("HTTP GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
}

func TestConformance_HTTP_ValidRequest(t *testing.T) {
	srv := NewServer(newConformanceBackend())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody := `{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":99}`
	resp, err := http.Post(ts.URL, "application/json", bytes.NewBufferString(reqBody))
	if err != nil {
		t.Fatalf("HTTP POST: %v", err)
	}
	defer resp.Body.Close()

	var rpcResp Response
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rpcResp.JSONRPC != "2.0" {
		t.Fatalf("expected jsonrpc 2.0, got %q", rpcResp.JSONRPC)
	}
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: %v", rpcResp.Error.Message)
	}
	// ID should be preserved.
	if string(rpcResp.ID) != "99" {
		t.Fatalf("expected ID 99, got %s", string(rpcResp.ID))
	}
}

// --- Additional Method Tests ---

func TestConformance_GetCode(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	resp := conformanceCall(t, api, "eth_getCode",
		"0x000000000000000000000000000000000000aaaa", "latest")
	requireSuccess(t, resp)

	code := mustString(t, resp)
	if code != "0x6000" {
		t.Fatalf("expected 0x6000, got %s", code)
	}
}

func TestConformance_GetStorageAt(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	resp := conformanceCall(t, api, "eth_getStorageAt",
		"0x000000000000000000000000000000000000aaaa",
		"0x0000000000000000000000000000000000000000000000000000000000000000",
		"latest")
	requireSuccess(t, resp)

	value := mustString(t, resp)
	// Should return a 32-byte zero hash for unset storage.
	if len(value) != 66 { // 0x + 64 hex chars
		t.Fatalf("expected 66-char hex string, got %d chars: %s", len(value), value)
	}
}

func TestConformance_MaxPriorityFeePerGas(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	resp := conformanceCall(t, api, "eth_maxPriorityFeePerGas")
	requireSuccess(t, resp)

	fee := mustString(t, resp)
	if fee != "0x3b9aca00" { // 1 Gwei
		t.Fatalf("expected 0x3b9aca00, got %s", fee)
	}
}

func TestConformance_Web3Sha3(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	// keccak256("") = 0xc5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470
	resp := conformanceCall(t, api, "web3_sha3", "0x")
	requireSuccess(t, resp)

	hash := mustString(t, resp)
	expected := "0xc5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470"
	if hash != expected {
		t.Fatalf("expected keccak256(''), got %s", hash)
	}
}

func TestConformance_Accounts(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	resp := conformanceCall(t, api, "eth_accounts")
	requireSuccess(t, resp)
}

func TestConformance_ProtocolVersion(t *testing.T) {
	api := NewEthAPI(newConformanceBackend())

	resp := conformanceCall(t, api, "eth_protocolVersion")
	requireSuccess(t, resp)
}
