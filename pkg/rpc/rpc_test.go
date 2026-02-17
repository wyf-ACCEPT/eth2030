package rpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

var errCallFailed = errors.New("execution reverted")

// mockBackend implements Backend for testing.
type mockBackend struct {
	chainID      *big.Int
	headers      map[uint64]*types.Header
	statedb      *state.MemoryStateDB
	transactions map[types.Hash]*mockTxInfo
	receipts     map[types.Hash][]*types.Receipt
	logs         map[types.Hash][]*types.Log
	sentTxs      []*types.Transaction
	callResult   []byte
	callGasUsed  uint64
	callErr      error
}

type mockTxInfo struct {
	tx       *types.Transaction
	blockNum uint64
	index    uint64
}

func newMockBackend() *mockBackend {
	sdb := state.NewMemoryStateDB()
	// Create a funded account.
	addr := types.HexToAddress("0xaaaa")
	sdb.AddBalance(addr, big.NewInt(1e18))
	sdb.SetNonce(addr, 5)
	sdb.SetCode(addr, []byte{0x60, 0x00})

	header := &types.Header{
		Number:   big.NewInt(42),
		GasLimit: 30000000,
		GasUsed:  15000000,
		Time:     1700000000,
		BaseFee:  big.NewInt(1000000000),
	}

	return &mockBackend{
		chainID:      big.NewInt(1337),
		headers:      map[uint64]*types.Header{42: header},
		statedb:      sdb,
		transactions: make(map[types.Hash]*mockTxInfo),
		receipts:     make(map[types.Hash][]*types.Receipt),
		logs:         make(map[types.Hash][]*types.Log),
	}
}

func (b *mockBackend) HeaderByNumber(number BlockNumber) *types.Header {
	if number == LatestBlockNumber {
		return b.headers[42]
	}
	return b.headers[uint64(number)]
}

func (b *mockBackend) HeaderByHash(hash types.Hash) *types.Header {
	for _, h := range b.headers {
		if h.Hash() == hash {
			return h
		}
	}
	return nil
}

func (b *mockBackend) BlockByNumber(number BlockNumber) *types.Block { return nil }
func (b *mockBackend) BlockByHash(hash types.Hash) *types.Block      { return nil }

func (b *mockBackend) CurrentHeader() *types.Header {
	return b.headers[42]
}

func (b *mockBackend) ChainID() *big.Int {
	return b.chainID
}

func (b *mockBackend) StateAt(root types.Hash) (state.StateDB, error) {
	return b.statedb, nil
}

func (b *mockBackend) SendTransaction(tx *types.Transaction) error {
	b.sentTxs = append(b.sentTxs, tx)
	return nil
}

func (b *mockBackend) GetTransaction(hash types.Hash) (*types.Transaction, uint64, uint64) {
	if info, ok := b.transactions[hash]; ok {
		return info.tx, info.blockNum, info.index
	}
	return nil, 0, 0
}

func (b *mockBackend) SuggestGasPrice() *big.Int { return big.NewInt(1000000000) }

func (b *mockBackend) GetReceipts(blockHash types.Hash) []*types.Receipt {
	return b.receipts[blockHash]
}

func (b *mockBackend) GetLogs(blockHash types.Hash) []*types.Log {
	return b.logs[blockHash]
}

func (b *mockBackend) GetBlockReceipts(number uint64) []*types.Receipt {
	header := b.headers[number]
	if header == nil {
		return nil
	}
	return b.receipts[header.Hash()]
}

func (b *mockBackend) EVMCall(from types.Address, to *types.Address, data []byte, gas uint64, value *big.Int, blockNumber BlockNumber) ([]byte, uint64, error) {
	return b.callResult, b.callGasUsed, b.callErr
}

func callRPC(t *testing.T, api *EthAPI, method string, params ...interface{}) *Response {
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
	return api.HandleRequest(req)
}

func TestEthChainID(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_chainId")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	if resp.Result != "0x539" { // 1337
		t.Fatalf("want 0x539, got %v", resp.Result)
	}
}

func TestEthBlockNumber(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_blockNumber")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	if resp.Result != "0x2a" { // 42
		t.Fatalf("want 0x2a, got %v", resp.Result)
	}
}

func TestEthGetBalance(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_getBalance", "0x000000000000000000000000000000000000aaaa", "latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	// 1e18 = 0xde0b6b3a7640000
	got, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	if got != "0xde0b6b3a7640000" {
		t.Fatalf("want 0xde0b6b3a7640000, got %v", got)
	}
}

func TestEthGetTransactionCount(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_getTransactionCount", "0x000000000000000000000000000000000000aaaa", "latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	if resp.Result != "0x5" { // nonce 5
		t.Fatalf("want 0x5, got %v", resp.Result)
	}
}

func TestEthGetCode(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_getCode", "0x000000000000000000000000000000000000aaaa", "latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	if resp.Result != "0x6000" {
		t.Fatalf("want 0x6000, got %v", resp.Result)
	}
}

func TestEthGasPrice(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_gasPrice")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	if resp.Result != "0x3b9aca00" { // 1 Gwei
		t.Fatalf("want 0x3b9aca00, got %v", resp.Result)
	}
}

func TestWeb3ClientVersion(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "web3_clientVersion")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	if resp.Result != "eth2028/v0.1.0" {
		t.Fatalf("want eth2028/v0.1.0, got %v", resp.Result)
	}
}

func TestNetVersion(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "net_version")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	if resp.Result != "1337" {
		t.Fatalf("want 1337, got %v", resp.Result)
	}
}

func TestMethodNotFound(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_nonexistent")

	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != ErrCodeMethodNotFound {
		t.Fatalf("want error code %d, got %d", ErrCodeMethodNotFound, resp.Error.Code)
	}
}

func TestGetBlockByNumber(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_getBlockByNumber", "latest", false)

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestGetBlockByNumber_NotFound(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_getBlockByNumber", "0x999", false)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	if resp.Result != nil {
		t.Fatal("expected nil result for non-existent block")
	}
}

func TestServer_HTTPPost(t *testing.T) {
	srv := NewServer(newMockBackend())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody := `{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}`
	resp, err := http.Post(ts.URL, "application/json", bytes.NewBufferString(reqBody))
	if err != nil {
		t.Fatalf("HTTP POST: %v", err)
	}
	defer resp.Body.Close()

	var rpcResp Response
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if rpcResp.Error != nil {
		t.Fatalf("RPC error: %v", rpcResp.Error.Message)
	}
}

func TestServer_MethodNotAllowed(t *testing.T) {
	srv := NewServer(newMockBackend())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("HTTP GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", resp.StatusCode)
	}
}

func TestEthCall(t *testing.T) {
	mb := newMockBackend()
	mb.callResult = []byte{0x00, 0x00, 0x00, 0x01}
	api := NewEthAPI(mb)

	to := "0x000000000000000000000000000000000000bbbb"
	resp := callRPC(t, api, "eth_call", map[string]interface{}{
		"from": "0x000000000000000000000000000000000000aaaa",
		"to":   to,
		"data": "0x12345678",
	}, "latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	got, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	if got != "0x00000001" {
		t.Fatalf("want 0x00000001, got %v", got)
	}
}

func TestEthCall_Error(t *testing.T) {
	mb := newMockBackend()
	mb.callErr = errCallFailed
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_call", map[string]interface{}{
		"to":   "0x000000000000000000000000000000000000bbbb",
		"data": "0x",
	}, "latest")

	if resp.Error == nil {
		t.Fatal("expected error for failed call")
	}
}

func TestEstimateGas(t *testing.T) {
	mb := newMockBackend()
	// EVMCall always succeeds (callErr is nil)
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
	// Should be 21000 (0x5208) since the mock always succeeds
	if got != "0x5208" {
		t.Fatalf("want 0x5208, got %v", got)
	}
}

func TestGetTransactionByHash(t *testing.T) {
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

	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getTransactionByHash", encodeHash(txHash))

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("expected non-nil result")
	}
	rpcTx, ok := resp.Result.(*RPCTransaction)
	if !ok {
		t.Fatalf("result not *RPCTransaction: %T", resp.Result)
	}
	if rpcTx.Nonce != "0x1" {
		t.Fatalf("want nonce 0x1, got %v", rpcTx.Nonce)
	}
}

func TestGetTransactionByHash_NotFound(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_getTransactionByHash",
		"0x0000000000000000000000000000000000000000000000000000000000000000")

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	if resp.Result != nil {
		t.Fatal("expected nil result for non-existent tx")
	}
}

func TestGetTransactionReceipt(t *testing.T) {
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

	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getTransactionReceipt", encodeHash(txHash))

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("expected non-nil result")
	}
	rpcReceipt, ok := resp.Result.(*RPCReceipt)
	if !ok {
		t.Fatalf("result not *RPCReceipt: %T", resp.Result)
	}
	if rpcReceipt.Status != "0x1" {
		t.Fatalf("want status 0x1, got %v", rpcReceipt.Status)
	}
	if rpcReceipt.GasUsed != "0x5208" {
		t.Fatalf("want gasUsed 0x5208, got %v", rpcReceipt.GasUsed)
	}
}

func TestGetLogs(t *testing.T) {
	mb := newMockBackend()
	blockHash := mb.headers[42].Hash()

	topic := types.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")
	contractAddr := types.HexToAddress("0xcccc")

	mb.logs[blockHash] = []*types.Log{
		{
			Address:     contractAddr,
			Topics:      []types.Hash{topic},
			Data:        []byte{0x01},
			BlockNumber: 42,
			BlockHash:   blockHash,
			TxIndex:     0,
			Index:       0,
		},
	}

	api := NewEthAPI(mb)
	from := BlockNumber(42)
	to := BlockNumber(42)
	resp := callRPC(t, api, "eth_getLogs", map[string]interface{}{
		"fromBlock": encodeUint64(uint64(from)),
		"toBlock":   encodeUint64(uint64(to)),
	})

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	logs, ok := resp.Result.([]*RPCLog)
	if !ok {
		t.Fatalf("result not []*RPCLog: %T", resp.Result)
	}
	if len(logs) != 1 {
		t.Fatalf("want 1 log, got %d", len(logs))
	}
}

func TestGetLogs_AddressFilter(t *testing.T) {
	mb := newMockBackend()
	blockHash := mb.headers[42].Hash()

	addr1 := types.HexToAddress("0xcccc")
	addr2 := types.HexToAddress("0xdddd")

	mb.logs[blockHash] = []*types.Log{
		{Address: addr1, BlockNumber: 42, BlockHash: blockHash, Index: 0},
		{Address: addr2, BlockNumber: 42, BlockHash: blockHash, Index: 1},
	}

	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getLogs", map[string]interface{}{
		"fromBlock": "0x2a",
		"toBlock":   "0x2a",
		"address":   []string{encodeAddress(addr1)},
	})

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	logs, ok := resp.Result.([]*RPCLog)
	if !ok {
		t.Fatalf("result not []*RPCLog: %T", resp.Result)
	}
	if len(logs) != 1 {
		t.Fatalf("want 1 log (filtered by address), got %d", len(logs))
	}
}

func TestSendRawTransaction(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)

	// Send some raw bytes (simplified - not a real RLP-encoded tx)
	resp := callRPC(t, api, "eth_sendRawTransaction", "0xdeadbeef")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(mb.sentTxs) != 1 {
		t.Fatalf("expected 1 sent tx, got %d", len(mb.sentTxs))
	}
}

func TestGetBlockReceipts(t *testing.T) {
	mb := newMockBackend()
	blockHash := mb.headers[42].Hash()

	receipt1 := &types.Receipt{
		Status:            types.ReceiptStatusSuccessful,
		CumulativeGasUsed: 21000,
		GasUsed:           21000,
		TxHash:            types.HexToHash("0x1111"),
		BlockHash:         blockHash,
		BlockNumber:       big.NewInt(42),
		TransactionIndex:  0,
		Logs:              []*types.Log{},
	}
	receipt2 := &types.Receipt{
		Status:            types.ReceiptStatusSuccessful,
		CumulativeGasUsed: 42000,
		GasUsed:           21000,
		TxHash:            types.HexToHash("0x2222"),
		BlockHash:         blockHash,
		BlockNumber:       big.NewInt(42),
		TransactionIndex:  1,
		Logs:              []*types.Log{},
	}
	mb.receipts[blockHash] = []*types.Receipt{receipt1, receipt2}

	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getBlockReceipts", "0x2a")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	receipts, ok := resp.Result.([]*RPCReceipt)
	if !ok {
		t.Fatalf("result not []*RPCReceipt: %T", resp.Result)
	}
	if len(receipts) != 2 {
		t.Fatalf("want 2 receipts, got %d", len(receipts))
	}
}

func TestGetBlockReceipts_NotFound(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_getBlockReceipts", "0x999")

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	if resp.Result != nil {
		t.Fatal("expected nil result for non-existent block")
	}
}
