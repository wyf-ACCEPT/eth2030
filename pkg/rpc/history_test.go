package rpc

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// TestGetBlockByNumber_HistoryPruned verifies that requesting full tx data
// for a pruned block returns the EIP-4444 error.
func TestGetBlockByNumber_HistoryPruned(t *testing.T) {
	mb := newMockBackend()
	mb.historyOldest = 100

	api := NewEthAPI(mb)
	// Block 42 is below the oldest available (100), requesting fullTx=true
	// should return a history pruned error.
	resp := callRPC(t, api, "eth_getBlockByNumber", "0x2a", true)

	if resp.Error == nil {
		t.Fatal("expected EIP-4444 pruned error for old block with fullTx=true")
	}
	if resp.Error.Code != ErrCodeHistoryPruned {
		t.Fatalf("want error code %d, got %d", ErrCodeHistoryPruned, resp.Error.Code)
	}
}

// TestGetBlockByNumber_HeaderOnlyNotPruned verifies that requesting headers
// only (fullTx=false) works even for pruned blocks.
func TestGetBlockByNumber_HeaderOnlyNotPruned(t *testing.T) {
	mb := newMockBackend()
	mb.historyOldest = 100

	api := NewEthAPI(mb)
	// Block 42 is below oldest, but fullTx=false should still return header.
	resp := callRPC(t, api, "eth_getBlockByNumber", "0x2a", false)

	if resp.Error != nil {
		t.Fatalf("headers should be available even for pruned blocks: %v", resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("expected header result for pruned block with fullTx=false")
	}
}

// TestGetBlockByNumber_NoPruning verifies normal behavior when no pruning
// has occurred (historyOldest=0).
func TestGetBlockByNumber_NoPruning(t *testing.T) {
	mb := newMockBackend()
	// historyOldest defaults to 0 (no pruning).

	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getBlockByNumber", "0x2a", true)

	// Should not error - no pruning configured.
	// Block may return nil result since we don't have blocks in mockBackend,
	// but there should be no pruning error.
	if resp.Error != nil && resp.Error.Code == ErrCodeHistoryPruned {
		t.Fatal("should not get pruning error when historyOldest is 0")
	}
}

// TestGetTransactionReceipt_HistoryPruned verifies that receipt requests
// for pruned blocks return the EIP-4444 error.
func TestGetTransactionReceipt_HistoryPruned(t *testing.T) {
	mb := newMockBackend()
	mb.historyOldest = 100

	to := types.HexToAddress("0xbbbb")
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(0),
	})
	sender := types.HexToAddress("0xaaaa")
	tx.SetSender(sender)

	txHash := tx.Hash()
	// Block 42 is below the oldest available (100).
	mb.transactions[txHash] = &mockTxInfo{tx: tx, blockNum: 42, index: 0}

	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getTransactionReceipt", encodeHash(txHash))

	if resp.Error == nil {
		t.Fatal("expected EIP-4444 pruned error for receipt in old block")
	}
	if resp.Error.Code != ErrCodeHistoryPruned {
		t.Fatalf("want error code %d, got %d", ErrCodeHistoryPruned, resp.Error.Code)
	}
}

// TestGetBlockReceipts_HistoryPruned verifies that block receipt requests
// for pruned blocks return the EIP-4444 error.
func TestGetBlockReceipts_HistoryPruned(t *testing.T) {
	mb := newMockBackend()
	mb.historyOldest = 100

	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getBlockReceipts", "0x2a")

	if resp.Error == nil {
		t.Fatal("expected EIP-4444 pruned error for receipts in old block")
	}
	if resp.Error.Code != ErrCodeHistoryPruned {
		t.Fatalf("want error code %d, got %d", ErrCodeHistoryPruned, resp.Error.Code)
	}
}

// TestGetLogs_HistoryPruned verifies that log queries touching pruned blocks
// return the EIP-4444 error.
func TestGetLogs_HistoryPruned(t *testing.T) {
	mb := newMockBackend()
	mb.historyOldest = 100

	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getLogs", map[string]interface{}{
		"fromBlock": "0x10", // block 16, below oldest
		"toBlock":   "0x2a", // block 42
	})

	if resp.Error == nil {
		t.Fatal("expected EIP-4444 pruned error for logs in old block range")
	}
	if resp.Error.Code != ErrCodeHistoryPruned {
		t.Fatalf("want error code %d, got %d", ErrCodeHistoryPruned, resp.Error.Code)
	}
}

// TestGetLogs_NotPruned verifies logs work when the block range is within
// the available history.
func TestGetLogs_NotPruned(t *testing.T) {
	mb := newMockBackend()
	mb.historyOldest = 10

	api := NewEthAPI(mb)
	resp := callRPC(t, api, "eth_getLogs", map[string]interface{}{
		"fromBlock": "0x2a", // block 42, above oldest
		"toBlock":   "0x2a",
	})

	// Should not get a pruning error.
	if resp.Error != nil && resp.Error.Code == ErrCodeHistoryPruned {
		t.Fatal("should not get pruning error when block is within available range")
	}
}
