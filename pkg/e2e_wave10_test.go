package e2e_test

import (
	"bytes"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eth2028/eth2028/core"
	"github.com/eth2028/eth2028/core/rawdb"
	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/rpc"
	ssync "github.com/eth2028/eth2028/sync"
)

// --- Blockchain Integration Tests ---

func makeGenesisBlock() (*types.Block, *state.MemoryStateDB) {
	header := &types.Header{
		Number:     big.NewInt(0),
		GasLimit:   30000000,
		GasUsed:    0,
		Time:       0,
		Difficulty: new(big.Int),
		BaseFee:    big.NewInt(1000000000),
		UncleHash:  core.EmptyUncleHash,
	}
	statedb := state.NewMemoryStateDB()
	// Fund a sender for transactions.
	sender := types.HexToAddress("0x1111")
	statedb.AddBalance(sender, new(big.Int).Mul(big.NewInt(100), new(big.Int).SetUint64(1e18)))
	return types.NewBlock(header, nil), statedb
}

func buildBlock(t *testing.T, parent *types.Block, statedb *state.MemoryStateDB, txs []*types.Transaction) *types.Block {
	t.Helper()
	parentHeader := parent.Header()
	header := &types.Header{
		ParentHash: parent.Hash(),
		Number:     new(big.Int).Add(parentHeader.Number, big.NewInt(1)),
		GasLimit:   parentHeader.GasLimit,
		Time:       parentHeader.Time + 12,
		Difficulty: new(big.Int),
		BaseFee:    core.CalcBaseFee(parentHeader),
		UncleHash:  core.EmptyUncleHash,
	}

	// Process transactions.
	gp := new(core.GasPool).AddGas(header.GasLimit)
	var (
		receipts []*types.Receipt
		gasUsed  uint64
	)
	for _, tx := range txs {
		receipt, used, err := core.ApplyTransaction(core.TestConfig, statedb, header, tx, gp)
		if err != nil {
			t.Fatalf("apply tx: %v", err)
		}
		receipts = append(receipts, receipt)
		gasUsed += used
	}
	_ = receipts
	header.GasUsed = gasUsed

	return types.NewBlock(header, &types.Body{Transactions: txs})
}

func TestBlockchainE2E_FullLifecycle(t *testing.T) {
	genesis, genesisState := makeGenesisBlock()
	db := rawdb.NewMemoryDB()
	bc, err := core.NewBlockchain(core.TestConfig, genesis, genesisState, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}

	if bc.ChainLength() != 1 {
		t.Fatalf("initial chain length: want 1, got %d", bc.ChainLength())
	}

	// Build and insert 3 empty blocks.
	parent := genesis
	sdb := genesisState.Copy()
	for i := 0; i < 3; i++ {
		block := buildBlock(t, parent, sdb, nil)
		if err := bc.InsertBlock(block); err != nil {
			t.Fatalf("InsertBlock %d: %v", i+1, err)
		}
		parent = block
	}

	if bc.ChainLength() != 4 {
		t.Fatalf("chain length: want 4, got %d", bc.ChainLength())
	}

	// Current block should be block 3.
	current := bc.CurrentBlock()
	if current.NumberU64() != 3 {
		t.Fatalf("current block: want 3, got %d", current.NumberU64())
	}
}

func TestBlockchainE2E_WithTransactions(t *testing.T) {
	genesis, genesisState := makeGenesisBlock()
	db := rawdb.NewMemoryDB()
	bc, err := core.NewBlockchain(core.TestConfig, genesis, genesisState, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}

	// Build block with empty transactions (value transfer test is more
	// complex due to tx signing, so we test the chain mechanics here).
	sdb := genesisState.Copy()
	block1 := buildBlock(t, genesis, sdb, nil)
	if err := bc.InsertBlock(block1); err != nil {
		t.Fatalf("InsertBlock: %v", err)
	}

	// Verify chain state: sender should still have their funded balance.
	sender := types.HexToAddress("0x1111")
	st := bc.State()
	senderBal := st.GetBalance(sender)
	hundredETH := new(big.Int).Mul(big.NewInt(100), new(big.Int).SetUint64(1e18))
	if senderBal.Cmp(hundredETH) != 0 {
		t.Fatalf("sender balance: want %v, got %v", hundredETH, senderBal)
	}
}

func TestBlockchainE2E_GetHashFn(t *testing.T) {
	genesis, genesisState := makeGenesisBlock()
	db := rawdb.NewMemoryDB()
	bc, err := core.NewBlockchain(core.TestConfig, genesis, genesisState, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}

	// Insert a few blocks.
	parent := genesis
	sdb := genesisState.Copy()
	var blocks []*types.Block
	for i := 0; i < 5; i++ {
		block := buildBlock(t, parent, sdb, nil)
		bc.InsertBlock(block)
		blocks = append(blocks, block)
		parent = block
	}

	// GetHashFn should return hashes for previous blocks.
	getHash := bc.GetHashFn()

	// Genesis hash.
	h0 := getHash(0)
	if h0 != genesis.Hash() {
		t.Fatal("GetHashFn(0) != genesis hash")
	}

	// Block 3 hash.
	h3 := getHash(3)
	if h3 != blocks[2].Hash() {
		t.Fatal("GetHashFn(3) != block 3 hash")
	}

	// Non-existent block should return zero hash.
	h99 := getHash(99)
	if h99 != (types.Hash{}) {
		t.Fatal("GetHashFn(99) should return zero hash")
	}
}

func TestBlockchainE2E_SetHead(t *testing.T) {
	genesis, genesisState := makeGenesisBlock()
	db := rawdb.NewMemoryDB()
	bc, err := core.NewBlockchain(core.TestConfig, genesis, genesisState, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}

	parent := genesis
	sdb := genesisState.Copy()
	for i := 0; i < 5; i++ {
		block := buildBlock(t, parent, sdb, nil)
		bc.InsertBlock(block)
		parent = block
	}

	if bc.CurrentBlock().NumberU64() != 5 {
		t.Fatalf("should be at block 5")
	}

	if err := bc.SetHead(3); err != nil {
		t.Fatalf("SetHead: %v", err)
	}

	if bc.CurrentBlock().NumberU64() != 3 {
		t.Fatalf("after SetHead(3): want 3, got %d", bc.CurrentBlock().NumberU64())
	}

	// Block 4 should not be in canonical chain.
	b4 := bc.GetBlockByNumber(4)
	if b4 != nil {
		t.Fatal("block 4 should not exist after SetHead(3)")
	}
}

func TestBlockchainE2E_InvalidBlock(t *testing.T) {
	genesis, genesisState := makeGenesisBlock()
	db := rawdb.NewMemoryDB()
	bc, err := core.NewBlockchain(core.TestConfig, genesis, genesisState, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}

	// Try to insert a block with wrong parent.
	badBlock := types.NewBlock(&types.Header{
		ParentHash: types.Hash{0xff},
		Number:     big.NewInt(1),
		GasLimit:   30000000,
		Time:       12,
		Difficulty: new(big.Int),
		BaseFee:    big.NewInt(1000000000),
	}, nil)

	err = bc.InsertBlock(badBlock)
	if err == nil {
		t.Fatal("expected error for invalid block")
	}
}

// --- JSON-RPC Integration Tests ---

type blockchainBackend struct {
	bc      *core.Blockchain
	chainID *big.Int
}

func (b *blockchainBackend) HeaderByNumber(number rpc.BlockNumber) *types.Header {
	if number == rpc.LatestBlockNumber {
		return b.bc.CurrentBlock().Header()
	}
	block := b.bc.GetBlockByNumber(uint64(number))
	if block == nil {
		return nil
	}
	return block.Header()
}

func (b *blockchainBackend) HeaderByHash(hash types.Hash) *types.Header {
	block := b.bc.GetBlock(hash)
	if block == nil {
		return nil
	}
	return block.Header()
}

func (b *blockchainBackend) BlockByNumber(number rpc.BlockNumber) *types.Block {
	if number == rpc.LatestBlockNumber {
		return b.bc.CurrentBlock()
	}
	return b.bc.GetBlockByNumber(uint64(number))
}

func (b *blockchainBackend) BlockByHash(hash types.Hash) *types.Block {
	return b.bc.GetBlock(hash)
}

func (b *blockchainBackend) CurrentHeader() *types.Header {
	return b.bc.CurrentBlock().Header()
}

func (b *blockchainBackend) ChainID() *big.Int { return b.chainID }

func (b *blockchainBackend) StateAt(root types.Hash) (state.StateDB, error) {
	return b.bc.State(), nil
}

func (b *blockchainBackend) SendTransaction(tx *types.Transaction) error { return nil }
func (b *blockchainBackend) GetTransaction(hash types.Hash) (*types.Transaction, uint64, uint64) {
	return nil, 0, 0
}
func (b *blockchainBackend) SuggestGasPrice() *big.Int { return big.NewInt(1000000000) }
func (b *blockchainBackend) GetReceipts(blockHash types.Hash) []*types.Receipt { return nil }
func (b *blockchainBackend) GetLogs(blockHash types.Hash) []*types.Log     { return nil }
func (b *blockchainBackend) GetBlockReceipts(number uint64) []*types.Receipt { return nil }
func (b *blockchainBackend) EVMCall(from types.Address, to *types.Address, data []byte, gas uint64, value *big.Int, blockNumber rpc.BlockNumber) ([]byte, uint64, error) {
	return nil, 0, nil
}

func TestRPCE2E_BlockchainQueries(t *testing.T) {
	genesis, genesisState := makeGenesisBlock()
	db := rawdb.NewMemoryDB()
	bc, err := core.NewBlockchain(core.TestConfig, genesis, genesisState, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}

	// Insert 3 blocks.
	parent := genesis
	sdb := genesisState.Copy()
	for i := 0; i < 3; i++ {
		block := buildBlock(t, parent, sdb, nil)
		bc.InsertBlock(block)
		parent = block
	}

	backend := &blockchainBackend{bc: bc, chainID: big.NewInt(1337)}
	srv := rpc.NewServer(backend)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Query block number.
	resp := httpRPC(t, ts.URL, "eth_blockNumber", nil)
	if resp.Error != nil {
		t.Fatalf("eth_blockNumber error: %v", resp.Error.Message)
	}
	if resp.Result != "0x3" { // block 3
		t.Fatalf("eth_blockNumber: want 0x3, got %v", resp.Result)
	}

	// Query chain ID.
	resp = httpRPC(t, ts.URL, "eth_chainId", nil)
	if resp.Error != nil {
		t.Fatalf("eth_chainId error: %v", resp.Error.Message)
	}
	if resp.Result != "0x539" { // 1337
		t.Fatalf("eth_chainId: want 0x539, got %v", resp.Result)
	}

	// Query balance.
	resp = httpRPC(t, ts.URL, "eth_getBalance", []interface{}{
		"0x0000000000000000000000000000000000001111",
		"latest",
	})
	if resp.Error != nil {
		t.Fatalf("eth_getBalance error: %v", resp.Error.Message)
	}
	// Should have ~100 ETH (funded in genesis).
	result, ok := resp.Result.(string)
	if !ok || result == "0x0" {
		t.Fatalf("eth_getBalance: expected non-zero, got %v", resp.Result)
	}
}

func TestRPCE2E_ClientVersion(t *testing.T) {
	genesis, genesisState := makeGenesisBlock()
	db := rawdb.NewMemoryDB()
	bc, _ := core.NewBlockchain(core.TestConfig, genesis, genesisState, db)

	backend := &blockchainBackend{bc: bc, chainID: big.NewInt(1337)}
	srv := rpc.NewServer(backend)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp := httpRPC(t, ts.URL, "web3_clientVersion", nil)
	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	if resp.Result != "eth2028/v0.1.0" {
		t.Fatalf("want eth2028/v0.1.0, got %v", resp.Result)
	}
}

func httpRPC(t *testing.T, url, method string, params interface{}) rpc.Response {
	t.Helper()
	body := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"id":      1,
	}
	if params != nil {
		body["params"] = params
	} else {
		body["params"] = []interface{}{}
	}
	b, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	var rpcResp rpc.Response
	json.NewDecoder(resp.Body).Decode(&rpcResp)
	return rpcResp
}

// --- rawdb Round-Trip Tests ---

func TestRawDB_BlockRoundTrip(t *testing.T) {
	db := rawdb.NewMemoryDB()
	hash := [32]byte{0x01, 0x02, 0x03}
	num := uint64(42)

	headerData := []byte("header-rlp")
	bodyData := []byte("body-rlp")
	receiptData := []byte("receipt-rlp")

	// Write all block data.
	rawdb.WriteHeader(db, num, hash, headerData)
	rawdb.WriteBody(db, num, hash, bodyData)
	rawdb.WriteReceipts(db, num, hash, receiptData)
	rawdb.WriteCanonicalHash(db, num, hash)

	// Read back.
	gotHeader, err := rawdb.ReadHeader(db, num, hash)
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	if !bytes.Equal(gotHeader, headerData) {
		t.Fatal("header mismatch")
	}

	gotBody, err := rawdb.ReadBody(db, num, hash)
	if err != nil {
		t.Fatalf("ReadBody: %v", err)
	}
	if !bytes.Equal(gotBody, bodyData) {
		t.Fatal("body mismatch")
	}

	gotReceipt, err := rawdb.ReadReceipts(db, num, hash)
	if err != nil {
		t.Fatalf("ReadReceipts: %v", err)
	}
	if !bytes.Equal(gotReceipt, receiptData) {
		t.Fatal("receipt mismatch")
	}

	gotCanon, err := rawdb.ReadCanonicalHash(db, num)
	if err != nil {
		t.Fatalf("ReadCanonicalHash: %v", err)
	}
	if gotCanon != hash {
		t.Fatal("canonical hash mismatch")
	}
}

func TestRawDB_TxLookup(t *testing.T) {
	db := rawdb.NewMemoryDB()
	txHash := [32]byte{0xaa}
	blockNum := uint64(100)

	rawdb.WriteTxLookup(db, txHash, blockNum)
	got, err := rawdb.ReadTxLookup(db, txHash)
	if err != nil {
		t.Fatalf("ReadTxLookup: %v", err)
	}
	if got != blockNum {
		t.Fatalf("want %d, got %d", blockNum, got)
	}
}

// --- Sync Tests ---

func TestSyncE2E_FullSync(t *testing.T) {
	var insertedHeaders int
	var insertedBlocks int

	s := ssync.NewSyncer(ssync.DefaultConfig())
	s.SetCallbacks(
		func(headers []ssync.HeaderData) (int, error) {
			insertedHeaders += len(headers)
			return len(headers), nil
		},
		func(blocks []ssync.BlockData) (int, error) {
			insertedBlocks += len(blocks)
			return len(blocks), nil
		},
		func() uint64 { return 0 },
		func(hash [32]byte) bool { return false },
	)

	if err := s.Start(10); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Simulate header download.
	headers := make([]ssync.HeaderData, 10)
	for i := range headers {
		headers[i] = ssync.HeaderData{Number: uint64(i + 1)}
	}
	s.ProcessHeaders(headers)

	// Simulate block download.
	blocks := make([]ssync.BlockData, 10)
	for i := range blocks {
		blocks[i] = ssync.BlockData{Number: uint64(i + 1)}
	}
	s.ProcessBlocks(blocks)

	if insertedHeaders != 10 {
		t.Fatalf("inserted headers: want 10, got %d", insertedHeaders)
	}
	if insertedBlocks != 10 {
		t.Fatalf("inserted blocks: want 10, got %d", insertedBlocks)
	}

	// Should be done (reached target).
	if s.State() != ssync.StateDone {
		t.Fatalf("state: want done, got %d", s.State())
	}
}

func TestSyncE2E_CancelDuringSync(t *testing.T) {
	s := ssync.NewSyncer(nil)
	s.SetCallbacks(
		func(headers []ssync.HeaderData) (int, error) {
			return len(headers), nil
		},
		nil,
		func() uint64 { return 0 },
		nil,
	)
	s.Start(1000)

	// Process some headers.
	s.ProcessHeaders([]ssync.HeaderData{{Number: 1}, {Number: 2}})

	// Cancel mid-sync.
	s.Cancel()

	// Further processing should fail.
	_, err := s.ProcessHeaders([]ssync.HeaderData{{Number: 3}})
	if err != ssync.ErrCancelled {
		t.Fatalf("want ErrCancelled, got %v", err)
	}
}
