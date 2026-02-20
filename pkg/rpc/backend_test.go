package rpc

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// TestBackendInterface_ChainID verifies the mock backend implements ChainID.
func TestBackendInterface_ChainID(t *testing.T) {
	mb := newMockBackend()
	var b Backend = mb
	id := b.ChainID()
	if id == nil {
		t.Fatal("ChainID returned nil")
	}
	if id.Cmp(big.NewInt(1337)) != 0 {
		t.Fatalf("want 1337, got %s", id.String())
	}
}

// TestBackendInterface_CurrentHeader verifies CurrentHeader returns the expected header.
func TestBackendInterface_CurrentHeader(t *testing.T) {
	mb := newMockBackend()
	var b Backend = mb
	header := b.CurrentHeader()
	if header == nil {
		t.Fatal("CurrentHeader returned nil")
	}
	if header.Number.Uint64() != 42 {
		t.Fatalf("want block 42, got %d", header.Number.Uint64())
	}
}

// TestBackendInterface_HeaderByNumber tests header lookup by number.
func TestBackendInterface_HeaderByNumber(t *testing.T) {
	mb := newMockBackend()
	var b Backend = mb

	// Known block.
	h := b.HeaderByNumber(42)
	if h == nil {
		t.Fatal("HeaderByNumber(42) returned nil")
	}

	// Latest should resolve to block 42.
	h = b.HeaderByNumber(LatestBlockNumber)
	if h == nil {
		t.Fatal("HeaderByNumber(latest) returned nil")
	}
	if h.Number.Uint64() != 42 {
		t.Fatalf("latest: want 42, got %d", h.Number.Uint64())
	}

	// Unknown block.
	h = b.HeaderByNumber(999)
	if h != nil {
		t.Fatal("HeaderByNumber(999) should return nil")
	}
}

// TestBackendInterface_HeaderByHash tests header lookup by hash.
func TestBackendInterface_HeaderByHash(t *testing.T) {
	mb := newMockBackend()
	var b Backend = mb

	header := b.HeaderByNumber(42)
	if header == nil {
		t.Fatal("need header 42")
	}
	hash := header.Hash()

	found := b.HeaderByHash(hash)
	if found == nil {
		t.Fatal("HeaderByHash returned nil for known hash")
	}
	if found.Number.Uint64() != 42 {
		t.Fatalf("want block 42, got %d", found.Number.Uint64())
	}

	// Unknown hash.
	missing := b.HeaderByHash(types.Hash{})
	if missing != nil {
		t.Fatal("HeaderByHash should return nil for unknown hash")
	}
}

// TestBackendInterface_StateAt tests state retrieval.
func TestBackendInterface_StateAt(t *testing.T) {
	mb := newMockBackend()
	var b Backend = mb

	st, err := b.StateAt(types.Hash{})
	if err != nil {
		t.Fatalf("StateAt error: %v", err)
	}
	if st == nil {
		t.Fatal("StateAt returned nil state")
	}

	// Verify the known account exists.
	addr := types.HexToAddress("0xaaaa")
	balance := st.GetBalance(addr)
	if balance == nil || balance.Cmp(big.NewInt(1e18)) != 0 {
		t.Fatalf("want balance 1e18, got %v", balance)
	}
}

// TestBackendInterface_SendTransaction tests tx submission.
func TestBackendInterface_SendTransaction(t *testing.T) {
	mb := newMockBackend()
	var b Backend = mb

	tx := types.NewTransaction(&types.LegacyTx{
		Nonce: 1,
		Gas:   21000,
	})
	err := b.SendTransaction(tx)
	if err != nil {
		t.Fatalf("SendTransaction error: %v", err)
	}
	if len(mb.sentTxs) != 1 {
		t.Fatalf("want 1 sent tx, got %d", len(mb.sentTxs))
	}
}

// TestBackendInterface_GetTransaction tests tx lookup.
func TestBackendInterface_GetTransaction(t *testing.T) {
	mb := newMockBackend()
	var b Backend = mb

	tx := types.NewTransaction(&types.LegacyTx{Nonce: 10})
	hash := tx.Hash()
	mb.transactions[hash] = &mockTxInfo{tx: tx, blockNum: 42, index: 0}

	found, blockNum, index := b.GetTransaction(hash)
	if found == nil {
		t.Fatal("GetTransaction returned nil")
	}
	if blockNum != 42 {
		t.Fatalf("want blockNum 42, got %d", blockNum)
	}
	if index != 0 {
		t.Fatalf("want index 0, got %d", index)
	}

	// Unknown hash.
	missing, _, _ := b.GetTransaction(types.Hash{})
	if missing != nil {
		t.Fatal("GetTransaction should return nil for unknown hash")
	}
}

// TestBackendInterface_SuggestGasPrice tests gas price suggestion.
func TestBackendInterface_SuggestGasPrice(t *testing.T) {
	mb := newMockBackend()
	var b Backend = mb

	price := b.SuggestGasPrice()
	if price == nil {
		t.Fatal("SuggestGasPrice returned nil")
	}
	if price.Cmp(big.NewInt(1_000_000_000)) != 0 {
		t.Fatalf("want 1 Gwei, got %s", price.String())
	}
}

// TestBackendInterface_GetReceipts tests receipt retrieval.
func TestBackendInterface_GetReceipts(t *testing.T) {
	mb := newMockBackend()
	var b Backend = mb

	blockHash := mb.headers[42].Hash()
	receipt := &types.Receipt{
		Status:  types.ReceiptStatusSuccessful,
		TxHash:  types.HexToHash("0x1111"),
		GasUsed: 21000,
		Logs:    []*types.Log{},
	}
	mb.receipts[blockHash] = []*types.Receipt{receipt}

	receipts := b.GetReceipts(blockHash)
	if len(receipts) != 1 {
		t.Fatalf("want 1 receipt, got %d", len(receipts))
	}
	if receipts[0].GasUsed != 21000 {
		t.Fatalf("want gas used 21000, got %d", receipts[0].GasUsed)
	}
}

// TestBackendInterface_GetLogs tests log retrieval.
func TestBackendInterface_GetLogs(t *testing.T) {
	mb := newMockBackend()
	var b Backend = mb

	blockHash := mb.headers[42].Hash()
	logEntry := &types.Log{
		Address:     types.HexToAddress("0xcccc"),
		BlockNumber: 42,
		BlockHash:   blockHash,
	}
	mb.logs[blockHash] = []*types.Log{logEntry}

	logs := b.GetLogs(blockHash)
	if len(logs) != 1 {
		t.Fatalf("want 1 log, got %d", len(logs))
	}
}

// TestBackendInterface_EVMCall tests EVM call execution.
func TestBackendInterface_EVMCall(t *testing.T) {
	mb := newMockBackend()
	mb.callResult = []byte{0x01, 0x02}
	mb.callGasUsed = 5000
	var b Backend = mb

	result, gasUsed, err := b.EVMCall(
		types.Address{},
		nil,
		nil,
		100000,
		nil,
		LatestBlockNumber,
	)
	if err != nil {
		t.Fatalf("EVMCall error: %v", err)
	}
	if len(result) != 2 || result[0] != 0x01 {
		t.Fatalf("unexpected result: %x", result)
	}
	if gasUsed != 5000 {
		t.Fatalf("want gasUsed 5000, got %d", gasUsed)
	}
}

// TestBackendInterface_HistoryOldestBlock tests history pruning threshold.
func TestBackendInterface_HistoryOldestBlock(t *testing.T) {
	mb := newMockBackend()
	var b Backend = mb

	// Default is 0 (no pruning).
	oldest := b.HistoryOldestBlock()
	if oldest != 0 {
		t.Fatalf("want 0, got %d", oldest)
	}

	// Set pruning threshold.
	mb.historyOldest = 100
	oldest = b.HistoryOldestBlock()
	if oldest != 100 {
		t.Fatalf("want 100, got %d", oldest)
	}
}
