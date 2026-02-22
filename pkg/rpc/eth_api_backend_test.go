package rpc

import (
	"errors"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// TestEthAPIBackend_GetBlockByNumber_HeaderOnly tests header-only retrieval.
func TestEthAPIBackend_GetBlockByNumber_HeaderOnly(t *testing.T) {
	mb := newMockBackend()
	ab := NewEthAPIBackend(mb)

	block, header, err := ab.GetBlockByNumber(LatestBlockNumber, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if block != nil {
		t.Fatal("expected nil block for header-only mode")
	}
	if header == nil {
		t.Fatal("expected non-nil header")
	}
	if header.Number.Uint64() != 42 {
		t.Fatalf("want block 42, got %d", header.Number.Uint64())
	}
}

// TestEthAPIBackend_GetBlockByNumber_FullTx tests full block retrieval.
func TestEthAPIBackend_GetBlockByNumber_FullTx(t *testing.T) {
	mb := newMockBackend()
	// Create a block with transactions.
	to := types.HexToAddress("0xbbbb")
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1e9),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(1000),
	})
	block := types.NewBlock(mb.headers[42], &types.Body{Transactions: []*types.Transaction{tx}})
	mb.blocks[42] = block

	ab := NewEthAPIBackend(mb)
	gotBlock, header, err := ab.GetBlockByNumber(LatestBlockNumber, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBlock == nil {
		t.Fatal("expected non-nil block for full tx mode")
	}
	if header == nil {
		t.Fatal("expected non-nil header")
	}
	if len(gotBlock.Transactions()) != 1 {
		t.Fatalf("want 1 tx, got %d", len(gotBlock.Transactions()))
	}
}

// TestEthAPIBackend_GetBlockByNumber_NotFound tests missing block.
func TestEthAPIBackend_GetBlockByNumber_NotFound(t *testing.T) {
	mb := newMockBackend()
	ab := NewEthAPIBackend(mb)

	block, header, err := ab.GetBlockByNumber(BlockNumber(999), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if block != nil || header != nil {
		t.Fatal("expected nil for non-existent block")
	}
}

// TestEthAPIBackend_GetTransactionByHash_Found tests tx lookup.
func TestEthAPIBackend_GetTransactionByHash_Found(t *testing.T) {
	mb := newMockBackend()
	to := types.HexToAddress("0xbbbb")
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(1e9),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(1000),
	})
	txHash := tx.Hash()
	mb.transactions[txHash] = &mockTxInfo{tx: tx, blockNum: 42, index: 0}

	ab := NewEthAPIBackend(mb)
	result, err := ab.GetTransactionByHash(txHash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Tx.Hash() != txHash {
		t.Fatalf("want tx hash %s, got %s", txHash.Hex(), result.Tx.Hash().Hex())
	}
	if result.BlockNumber != 42 {
		t.Fatalf("want block 42, got %d", result.BlockNumber)
	}
}

// TestEthAPIBackend_GetTransactionByHash_NotFound tests missing tx.
func TestEthAPIBackend_GetTransactionByHash_NotFound(t *testing.T) {
	mb := newMockBackend()
	ab := NewEthAPIBackend(mb)

	result, err := ab.GetTransactionByHash(types.Hash{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatal("expected nil for non-existent tx")
	}
}

// TestEthAPIBackend_GetTransactionByHash_WithReceipt tests receipt inclusion.
func TestEthAPIBackend_GetTransactionByHash_WithReceipt(t *testing.T) {
	mb := newMockBackend()
	to := types.HexToAddress("0xbbbb")
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    2,
		GasPrice: big.NewInt(1e9),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(500),
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

	ab := NewEthAPIBackend(mb)
	result, err := ab.GetTransactionByHash(txHash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Receipt == nil {
		t.Fatal("expected non-nil receipt")
	}
	if result.Receipt.GasUsed != 21000 {
		t.Fatalf("want gasUsed 21000, got %d", result.Receipt.GasUsed)
	}
}

// TestEthAPIBackend_GetBalance tests balance retrieval.
func TestEthAPIBackend_GetBalance(t *testing.T) {
	mb := newMockBackend()
	ab := NewEthAPIBackend(mb)

	addr := types.HexToAddress("0xaaaa")
	balance, err := ab.GetBalance(addr, LatestBlockNumber)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := big.NewInt(1e18)
	if balance.Cmp(expected) != 0 {
		t.Fatalf("want balance %s, got %s", expected, balance)
	}
}

// TestEthAPIBackend_GetBalance_BlockNotFound tests balance at missing block.
func TestEthAPIBackend_GetBalance_BlockNotFound(t *testing.T) {
	mb := newMockBackend()
	ab := NewEthAPIBackend(mb)

	addr := types.HexToAddress("0xaaaa")
	_, err := ab.GetBalance(addr, BlockNumber(999))
	if !errors.Is(err, ErrAPIBackendNoBlock) {
		t.Fatalf("want ErrAPIBackendNoBlock, got %v", err)
	}
}

// TestEthAPIBackend_GetCode tests bytecode retrieval.
func TestEthAPIBackend_GetCode(t *testing.T) {
	mb := newMockBackend()
	ab := NewEthAPIBackend(mb)

	addr := types.HexToAddress("0xaaaa")
	code, err := ab.GetCode(addr, LatestBlockNumber)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(code) != 2 || code[0] != 0x60 || code[1] != 0x00 {
		t.Fatalf("want code [0x60, 0x00], got %x", code)
	}
}

// TestEthAPIBackend_GetCode_BlockNotFound tests code at missing block.
func TestEthAPIBackend_GetCode_BlockNotFound(t *testing.T) {
	mb := newMockBackend()
	ab := NewEthAPIBackend(mb)

	addr := types.HexToAddress("0xaaaa")
	_, err := ab.GetCode(addr, BlockNumber(999))
	if !errors.Is(err, ErrAPIBackendNoBlock) {
		t.Fatalf("want ErrAPIBackendNoBlock, got %v", err)
	}
}

// TestEthAPIBackend_GetStorageAt tests storage slot retrieval.
func TestEthAPIBackend_GetStorageAt(t *testing.T) {
	mb := newMockBackend()
	ab := NewEthAPIBackend(mb)

	addr := types.HexToAddress("0xaaaa")
	key := types.Hash{}
	value, err := ab.GetStorageAt(addr, key, LatestBlockNumber)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Default storage value is zero hash.
	if value != (types.Hash{}) {
		t.Fatalf("want zero hash, got %s", value.Hex())
	}
}

// TestEthAPIBackend_GetStorageAt_BlockNotFound tests storage at missing block.
func TestEthAPIBackend_GetStorageAt_BlockNotFound(t *testing.T) {
	mb := newMockBackend()
	ab := NewEthAPIBackend(mb)

	addr := types.HexToAddress("0xaaaa")
	_, err := ab.GetStorageAt(addr, types.Hash{}, BlockNumber(999))
	if !errors.Is(err, ErrAPIBackendNoBlock) {
		t.Fatalf("want ErrAPIBackendNoBlock, got %v", err)
	}
}

// TestEthAPIBackend_GetLogs tests log filtering.
func TestEthAPIBackend_GetLogs(t *testing.T) {
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
		{
			Address:     types.HexToAddress("0xdddd"),
			Topics:      []types.Hash{topic},
			Data:        []byte{0x02},
			BlockNumber: 42,
			BlockHash:   blockHash,
			TxIndex:     1,
			Index:       1,
		},
	}

	ab := NewEthAPIBackend(mb)

	// Filter by address.
	logs, err := ab.GetLogs(LogFilterParams{
		FromBlock: 42,
		ToBlock:   42,
		Addresses: []types.Address{contractAddr},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("want 1 log, got %d", len(logs))
	}
	if logs[0].Address != contractAddr {
		t.Fatalf("want address %s, got %s", contractAddr.Hex(), logs[0].Address.Hex())
	}
}

// TestEthAPIBackend_GetLogs_Empty tests log filtering with no matches.
func TestEthAPIBackend_GetLogs_Empty(t *testing.T) {
	mb := newMockBackend()
	ab := NewEthAPIBackend(mb)

	logs, err := ab.GetLogs(LogFilterParams{
		FromBlock: 42,
		ToBlock:   42,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(logs) != 0 {
		t.Fatalf("want 0 logs, got %d", len(logs))
	}
}

// TestEthAPIBackend_GetLogs_InvalidRange tests invalid block range.
func TestEthAPIBackend_GetLogs_InvalidRange(t *testing.T) {
	mb := newMockBackend()
	ab := NewEthAPIBackend(mb)

	_, err := ab.GetLogs(LogFilterParams{
		FromBlock: 100,
		ToBlock:   50,
	})
	if !errors.Is(err, ErrAPIBackendInvalidArg) {
		t.Fatalf("want ErrAPIBackendInvalidArg, got %v", err)
	}
}

// TestEthAPIBackend_EstimateGas tests gas estimation success.
func TestEthAPIBackend_EstimateGas(t *testing.T) {
	mb := newMockBackend()
	// EVMCall always succeeds with nil error.
	ab := NewEthAPIBackend(mb)

	gas, err := ab.EstimateGas(GasEstimateArgs{
		From: types.HexToAddress("0xaaaa"),
		To:   addrPtr(types.HexToAddress("0xbbbb")),
	}, LatestBlockNumber)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return the floor (21000) since mock always succeeds.
	if gas != 21000 {
		t.Fatalf("want gas 21000, got %d", gas)
	}
}

// TestEthAPIBackend_EstimateGas_BlockNotFound tests estimation at missing block.
func TestEthAPIBackend_EstimateGas_BlockNotFound(t *testing.T) {
	mb := newMockBackend()
	ab := NewEthAPIBackend(mb)

	_, err := ab.EstimateGas(GasEstimateArgs{}, BlockNumber(999))
	if !errors.Is(err, ErrAPIBackendNoBlock) {
		t.Fatalf("want ErrAPIBackendNoBlock, got %v", err)
	}
}

// TestEthAPIBackend_EstimateGas_FailAtUpperBound tests estimation when exec fails.
func TestEthAPIBackend_EstimateGas_FailAtUpperBound(t *testing.T) {
	mb := newMockBackend()
	mb.callErr = errors.New("out of gas")
	ab := NewEthAPIBackend(mb)

	_, err := ab.EstimateGas(GasEstimateArgs{
		From: types.HexToAddress("0xaaaa"),
	}, LatestBlockNumber)
	if !errors.Is(err, ErrAPIBackendEstimate) {
		t.Fatalf("want ErrAPIBackendEstimate, got %v", err)
	}
}

// TestEthAPIBackend_PendingTransactions tests pending tx query.
func TestEthAPIBackend_PendingTransactions(t *testing.T) {
	mb := newMockBackend()
	ab := NewEthAPIBackend(mb)

	txs, err := ab.PendingTransactions()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if txs == nil {
		t.Fatal("expected non-nil result")
	}
	if len(txs) != 0 {
		t.Fatalf("want 0 pending txs, got %d", len(txs))
	}
}

// TestEthAPIBackend_SuggestGasPrice tests gas price suggestion.
func TestEthAPIBackend_SuggestGasPrice(t *testing.T) {
	mb := newMockBackend()
	ab := NewEthAPIBackend(mb)

	price := ab.SuggestGasPrice()
	expected := big.NewInt(1e9)
	if price.Cmp(expected) != 0 {
		t.Fatalf("want %s, got %s", expected, price)
	}
}

// TestEthAPIBackend_GetNonce tests nonce retrieval.
func TestEthAPIBackend_GetNonce(t *testing.T) {
	mb := newMockBackend()
	ab := NewEthAPIBackend(mb)

	addr := types.HexToAddress("0xaaaa")
	nonce, err := ab.GetNonce(addr, LatestBlockNumber)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nonce != 5 {
		t.Fatalf("want nonce 5, got %d", nonce)
	}
}

// TestEthAPIBackend_CurrentBlock tests current block number.
func TestEthAPIBackend_CurrentBlock(t *testing.T) {
	mb := newMockBackend()
	ab := NewEthAPIBackend(mb)

	num, err := ab.CurrentBlock()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if num != 42 {
		t.Fatalf("want block 42, got %d", num)
	}
}

// TestEthAPIBackend_GetBlockReceipts tests block receipt retrieval.
func TestEthAPIBackend_GetBlockReceipts(t *testing.T) {
	mb := newMockBackend()
	blockHash := mb.headers[42].Hash()
	mb.receipts[blockHash] = []*types.Receipt{
		{
			Status:  types.ReceiptStatusSuccessful,
			GasUsed: 21000,
			TxHash:  types.HexToHash("0x1111"),
			Logs:    []*types.Log{},
		},
	}

	ab := NewEthAPIBackend(mb)
	receipts, err := ab.GetBlockReceipts(42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(receipts) != 1 {
		t.Fatalf("want 1 receipt, got %d", len(receipts))
	}
}

// TestEthAPIBackend_ChainID tests chain ID retrieval.
func TestEthAPIBackend_ChainID(t *testing.T) {
	mb := newMockBackend()
	ab := NewEthAPIBackend(mb)

	chainID := ab.ChainID()
	if chainID.Int64() != 1337 {
		t.Fatalf("want chain ID 1337, got %d", chainID.Int64())
	}
}

// TestEthAPIBackend_GetLogs_TopicFilter tests log filtering by topic.
func TestEthAPIBackend_GetLogs_TopicFilter(t *testing.T) {
	mb := newMockBackend()
	blockHash := mb.headers[42].Hash()
	topic1 := types.HexToHash("0xaabb")
	topic2 := types.HexToHash("0xccdd")
	contractAddr := types.HexToAddress("0xcccc")
	mb.logs[blockHash] = []*types.Log{
		{
			Address:     contractAddr,
			Topics:      []types.Hash{topic1},
			BlockNumber: 42,
			BlockHash:   blockHash,
			Index:       0,
		},
		{
			Address:     contractAddr,
			Topics:      []types.Hash{topic2},
			BlockNumber: 42,
			BlockHash:   blockHash,
			Index:       1,
		},
	}

	ab := NewEthAPIBackend(mb)
	logs, err := ab.GetLogs(LogFilterParams{
		FromBlock: 42,
		ToBlock:   42,
		Topics:    [][]types.Hash{{topic1}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("want 1 log, got %d", len(logs))
	}
}

// addrPtr returns a pointer to the given address.
func addrPtr(a types.Address) *types.Address {
	return &a
}
