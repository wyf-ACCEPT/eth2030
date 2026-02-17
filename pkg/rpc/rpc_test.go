package rpc

import (
	"bytes"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

// mockBackend implements Backend for testing.
type mockBackend struct {
	chainID  *big.Int
	headers  map[uint64]*types.Header
	statedb  *state.MemoryStateDB
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
		chainID: big.NewInt(1337),
		headers: map[uint64]*types.Header{42: header},
		statedb: sdb,
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

func (b *mockBackend) SendTransaction(tx *types.Transaction) error                      { return nil }
func (b *mockBackend) GetTransaction(hash types.Hash) (*types.Transaction, uint64, uint64) { return nil, 0, 0 }
func (b *mockBackend) SuggestGasPrice() *big.Int                                        { return big.NewInt(1000000000) }

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
