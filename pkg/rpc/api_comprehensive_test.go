package rpc

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// ---------- eth_blockNumber ----------

func TestEthBlockNumber_ReturnsCorrectBlock(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_blockNumber")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	got, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	// Block 42 = 0x2a
	if got != "0x2a" {
		t.Fatalf("want 0x2a, got %v", got)
	}
}

// ---------- eth_getBlockByNumber ----------

func TestEthGetBlockByNumberLatest(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getBlockByNumber", "latest", false)

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("expected non-nil result for latest block")
	}
	block, ok := resp.Result.(*RPCBlock)
	if !ok {
		t.Fatalf("result not *RPCBlock: %T", resp.Result)
	}
	if block.Number != "0x2a" {
		t.Fatalf("want block number 0x2a, got %v", block.Number)
	}
}

func TestEthGetBlockByNumberGenesis(t *testing.T) {
	mb := newMockBackend()
	// Add genesis header at block 0.
	genesisHeader := &types.Header{
		Number:   big.NewInt(0),
		GasLimit: 30000000,
		GasUsed:  0,
		Time:     1690000000,
		BaseFee:  big.NewInt(1000000000),
	}
	mb.headers[0] = genesisHeader

	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getBlockByNumber", "0x0", false)

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("expected non-nil result for genesis block")
	}
	block, ok := resp.Result.(*RPCBlock)
	if !ok {
		t.Fatalf("result not *RPCBlock: %T", resp.Result)
	}
	if block.Number != "0x0" {
		t.Fatalf("want block number 0x0, got %v", block.Number)
	}
}

func TestEthGetBlockByNumber_NonExistent(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getBlockByNumber", "0xffffff", false)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	if resp.Result != nil {
		t.Fatal("expected nil result for non-existent block")
	}
}

// ---------- eth_getBlockByHash ----------

func TestEthGetBlockByHash_Valid(t *testing.T) {
	mb := newMockBackend()
	blockHash := mb.headers[42].Hash()

	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getBlockByHash", encodeHash(blockHash), false)

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("expected non-nil result for valid block hash")
	}
	block, ok := resp.Result.(*RPCBlock)
	if !ok {
		t.Fatalf("result not *RPCBlock: %T", resp.Result)
	}
	if block.Number != "0x2a" {
		t.Fatalf("want block number 0x2a, got %v", block.Number)
	}
}

func TestEthGetBlockByHash_Invalid(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getBlockByHash",
		"0x0000000000000000000000000000000000000000000000000000000000000000", false)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	if resp.Result != nil {
		t.Fatal("expected nil result for invalid block hash")
	}
}

// ---------- eth_getTransactionCount ----------

func TestEthGetTransactionCount_Existing(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_getTransactionCount",
		"0x000000000000000000000000000000000000aaaa", "latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	got, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	// nonce is 5
	if got != "0x5" {
		t.Fatalf("want 0x5, got %v", got)
	}
}

func TestEthGetTransactionCount_NonExisting(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_getTransactionCount",
		"0x0000000000000000000000000000000000009999", "latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	got, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	// Non-existing account should have nonce 0.
	if got != "0x0" {
		t.Fatalf("want 0x0, got %v", got)
	}
}

// ---------- eth_getBalance ----------

func TestEthGetBalance_FundedAccount(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_getBalance",
		"0x000000000000000000000000000000000000aaaa", "latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	got, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	// 1e18 = 0xde0b6b3a7640000
	if got != "0xde0b6b3a7640000" {
		t.Fatalf("want 0xde0b6b3a7640000, got %v", got)
	}
}

func TestEthGetBalance_ZeroBalance(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_getBalance",
		"0x0000000000000000000000000000000000009999", "latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	got, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	if got != "0x0" {
		t.Fatalf("want 0x0, got %v", got)
	}
}

// ---------- eth_getCode ----------

func TestEthGetCode_Contract(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_getCode",
		"0x000000000000000000000000000000000000aaaa", "latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	got, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	// Account 0xaaaa has code 0x6000 (PUSH1 0x00).
	if got != "0x6000" {
		t.Fatalf("want 0x6000, got %v", got)
	}
}

func TestEthGetCode_EOA(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_getCode",
		"0x0000000000000000000000000000000000009999", "latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	got, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	// EOA (non-existent or no code) should return "0x".
	if got != "0x" {
		t.Fatalf("want 0x (empty code), got %v", got)
	}
}

// ---------- eth_chainId ----------

func TestEthChainId_Correct(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_chainId")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	got, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	// Chain ID 1337 = 0x539
	if got != "0x539" {
		t.Fatalf("want 0x539, got %v", got)
	}
}

// ---------- eth_gasPrice ----------

func TestEthGasPrice_NonZero(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_gasPrice")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	got, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	// 1 Gwei = 0x3b9aca00
	if got == "0x0" {
		t.Fatal("gas price should be non-zero")
	}
	if got != "0x3b9aca00" {
		t.Fatalf("want 0x3b9aca00, got %v", got)
	}
}

// ---------- net_version ----------

func TestNetVersion_ReturnsVersionString(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)
	resp := callRPC(t, api, "net_version")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	got, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	if got != "1337" {
		t.Fatalf("want 1337, got %v", got)
	}
}

// ---------- net_listening ----------

func TestNetListening_ReturnsTrue(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)
	resp := callRPC(t, api, "net_listening")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	got, ok := resp.Result.(bool)
	if !ok {
		t.Fatalf("result not bool: %T", resp.Result)
	}
	if !got {
		t.Fatal("expected net_listening to return true")
	}
}

// ---------- net_peerCount ----------

func TestNetPeerCount_ReturnsCount(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)
	resp := callRPC(t, api, "net_peerCount")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	got, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	// Currently returns 0x0, but should be a valid hex string.
	if got != "0x0" {
		t.Fatalf("want 0x0, got %v", got)
	}
}

// ---------- web3_clientVersion ----------

func TestWeb3ClientVersion_ReturnsVersion(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)
	resp := callRPC(t, api, "web3_clientVersion")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	got, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	if got != "eth2030/v0.1.0" {
		t.Fatalf("want eth2030/v0.1.0, got %v", got)
	}
}

// ---------- web3_sha3 ----------

func TestWeb3Sha3_Keccak256(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)

	// Keccak256 of empty data "0x".
	resp := callRPC(t, api, "web3_sha3", "0x")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	got, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}

	// Compute expected hash.
	expected := crypto.Keccak256Hash(nil)
	expectedHex := encodeHash(expected)
	if got != expectedHex {
		t.Fatalf("want %v, got %v", expectedHex, got)
	}
}

func TestWeb3Sha3_WithData(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)

	// Keccak256 of "0x68656c6c6f" ("hello")
	resp := callRPC(t, api, "web3_sha3", "0x68656c6c6f")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	got, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}

	// Compute expected hash of "hello".
	expected := crypto.Keccak256Hash([]byte("hello"))
	expectedHex := encodeHash(expected)
	if got != expectedHex {
		t.Fatalf("want %v, got %v", expectedHex, got)
	}
}

func TestWeb3Sha3_MissingParams(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)
	resp := callRPC(t, api, "web3_sha3")

	if resp.Error == nil {
		t.Fatal("expected error for missing params")
	}
	if resp.Error.Code != ErrCodeInvalidParams {
		t.Fatalf("want error code %d, got %d", ErrCodeInvalidParams, resp.Error.Code)
	}
}

// ---------- eth_getStorageAt ----------

func TestEthGetStorageAt_ValidSlot(t *testing.T) {
	mb := newMockBackend()
	addr := types.HexToAddress("0xaaaa")
	slot := types.HexToHash("0x01")
	val := types.HexToHash("0x42")
	mb.statedb.SetState(addr, slot, val)

	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getStorageAt",
		"0x000000000000000000000000000000000000aaaa",
		"0x0000000000000000000000000000000000000000000000000000000000000001",
		"latest")

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

func TestEthGetStorageAt_EmptySlot(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_getStorageAt",
		"0x000000000000000000000000000000000000aaaa",
		"0x0000000000000000000000000000000000000000000000000000000000000099",
		"latest")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	got, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	// Empty slot should return zero hash.
	expected := encodeHash(types.Hash{})
	if got != expected {
		t.Fatalf("want %v, got %v", expected, got)
	}
}

// ---------- eth_getTransactionByHash ----------

func TestEthGetTransactionByHash_Found(t *testing.T) {
	mb := newMockBackend()
	to := types.HexToAddress("0xbbbb")
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    10,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(5000),
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
	if rpcTx.Nonce != "0xa" { // nonce 10
		t.Fatalf("want nonce 0xa, got %v", rpcTx.Nonce)
	}
}

func TestEthGetTransactionByHash_NotFound(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getTransactionByHash",
		"0x0000000000000000000000000000000000000000000000000000000000000000")

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	if resp.Result != nil {
		t.Fatal("expected nil result for non-existent tx")
	}
}

// ---------- eth_getTransactionReceipt ----------

func TestEthGetTransactionReceipt_Valid(t *testing.T) {
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

// ---------- eth_call ----------

func TestEthCall_Success(t *testing.T) {
	mb := newMockBackend()
	mb.callResult = []byte{0x00, 0x00, 0x00, 0x01}
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_call", map[string]interface{}{
		"from": "0x000000000000000000000000000000000000aaaa",
		"to":   "0x000000000000000000000000000000000000bbbb",
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

// ---------- eth_estimateGas ----------

func TestEthEstimateGas_SimpleTransfer(t *testing.T) {
	mb := newMockBackend()
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
	// Simple transfer = 21000 (0x5208).
	if got != "0x5208" {
		t.Fatalf("want 0x5208, got %v", got)
	}
}

// ---------- eth_getLogs ----------

func TestEthGetLogs_ByBlockRange(t *testing.T) {
	mb := newMockBackend()
	blockHash := mb.headers[42].Hash()
	contractAddr := types.HexToAddress("0xcccc")
	topic := types.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")

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
	resp := callRPC(t, api, "eth_getLogs", map[string]interface{}{
		"fromBlock": "0x2a",
		"toBlock":   "0x2a",
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

// ---------- Missing parameter edge cases ----------

func TestEthGetBalance_MissingParams(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getBalance", "0x000000000000000000000000000000000000aaaa")

	if resp.Error == nil {
		t.Fatal("expected error for missing block number param")
	}
	if resp.Error.Code != ErrCodeInvalidParams {
		t.Fatalf("want error code %d, got %d", ErrCodeInvalidParams, resp.Error.Code)
	}
}

func TestEthGetBlockByNumber_MissingParams(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getBlockByNumber")

	if resp.Error == nil {
		t.Fatal("expected error for missing params")
	}
	if resp.Error.Code != ErrCodeInvalidParams {
		t.Fatalf("want error code %d, got %d", ErrCodeInvalidParams, resp.Error.Code)
	}
}

func TestEthGetBlockByHash_MissingParams(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getBlockByHash")

	if resp.Error == nil {
		t.Fatal("expected error for missing params")
	}
	if resp.Error.Code != ErrCodeInvalidParams {
		t.Fatalf("want error code %d, got %d", ErrCodeInvalidParams, resp.Error.Code)
	}
}

// ---------- JSON serialization round-trip ----------

func TestEthBlockNumber_JSONRoundTrip(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_blockNumber")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}

	// Marshal the full response to JSON and back.
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded Response
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if decoded.Error != nil {
		t.Fatalf("decoded response has error: %v", decoded.Error.Message)
	}
}

// ---------- eth_protocolVersion ----------

func TestEthProtocolVersion(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_protocolVersion")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	// Should return some value without error.
	if resp.Result == nil {
		t.Fatal("expected non-nil result for protocolVersion")
	}
}

// ---------- eth_accounts ----------

func TestEthAccounts(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_accounts")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
}

// ---------- eth_mining ----------

func TestEthMining(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_mining")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
}

// ---------- eth_hashrate ----------

func TestEthHashrate(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_hashrate")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
}

// ---------- eth_getUncleCountByBlockHash ----------

func TestEthGetUncleCountByBlockHash(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getUncleCountByBlockHash",
		"0x0000000000000000000000000000000000000000000000000000000000001234")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
}

// ---------- eth_getUncleCountByBlockNumber ----------

func TestEthGetUncleCountByBlockNumber(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getUncleCountByBlockNumber", "0x2a")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
}

// ---------- EIP-4444: history pruning ----------

func TestEthGetBlockByNumber_HistoryPruned(t *testing.T) {
	mb := newMockBackend()
	mb.historyOldest = 40 // blocks before 40 are pruned

	// Add a header at block 30 (pruned range).
	mb.headers[30] = &types.Header{
		Number:   big.NewInt(30),
		GasLimit: 30000000,
		GasUsed:  0,
		Time:     1690000000,
		BaseFee:  big.NewInt(1000000000),
	}

	api := NewEthAPI(mb)

	// Request with fullTx=true should fail for pruned block.
	resp := callRPC(t, api, "eth_getBlockByNumber", "0x1e", true)

	if resp.Error == nil {
		t.Fatal("expected error for pruned block with fullTx=true")
	}
	if resp.Error.Code != ErrCodeHistoryPruned {
		t.Fatalf("want error code %d, got %d", ErrCodeHistoryPruned, resp.Error.Code)
	}
}
