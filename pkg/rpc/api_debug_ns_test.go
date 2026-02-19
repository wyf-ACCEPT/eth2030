package rpc

import (
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// callDebug is a test helper that dispatches a request through the DebugAPI.
func callDebug(t *testing.T, d *DebugAPI, method string, params ...interface{}) *Response {
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
	return d.HandleDebugRequest(req)
}

// ---------- debug_traceBlockByNumber ----------

func TestDebugNS_TraceBlockByNumber(t *testing.T) {
	mb := newMockBackend()

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

	block := types.NewBlock(mb.headers[42], &types.Body{
		Transactions: []*types.Transaction{tx1},
	})
	mb.blocks[42] = block

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
	}

	d := NewDebugAPI(mb)
	resp := callDebug(t, d, "debug_traceBlockByNumber", "0x2a")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	results, ok := resp.Result.([]*DebugBlockTraceEntry)
	if !ok {
		t.Fatalf("result not []*DebugBlockTraceEntry: %T", resp.Result)
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
}

func TestDebugNS_TraceBlockByNumber_NotFound(t *testing.T) {
	d := NewDebugAPI(newMockBackend())
	resp := callDebug(t, d, "debug_traceBlockByNumber", "0x999")
	if resp.Error == nil {
		t.Fatal("expected error for non-existent block")
	}
}

// ---------- debug_traceBlockByHash ----------

func TestDebugNS_TraceBlockByHash(t *testing.T) {
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

	d := NewDebugAPI(mb)
	resp := callDebug(t, d, "debug_traceBlockByHash", encodeHash(blockHash))

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	results, ok := resp.Result.([]*DebugBlockTraceEntry)
	if !ok {
		t.Fatalf("result not []*DebugBlockTraceEntry: %T", resp.Result)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 trace result, got %d", len(results))
	}
	if results[0].Result.Gas != 21000 {
		t.Fatalf("want gas 21000, got %d", results[0].Result.Gas)
	}
}

func TestDebugNS_TraceBlockByHash_NotFound(t *testing.T) {
	d := NewDebugAPI(newMockBackend())
	resp := callDebug(t, d, "debug_traceBlockByHash",
		"0x0000000000000000000000000000000000000000000000000000000000000000")
	if resp.Error == nil {
		t.Fatal("expected error for non-existent block")
	}
}

// ---------- debug_getBlockRlp ----------

func TestDebugNS_GetBlockRlp(t *testing.T) {
	mb := newMockBackend()
	block := types.NewBlock(mb.headers[42], nil)
	mb.blocks[42] = block

	d := NewDebugAPI(mb)
	resp := callDebug(t, d, "debug_getBlockRlp", "0x2a")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	rlpHex, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	if !strings.HasPrefix(rlpHex, "0x") {
		t.Fatalf("expected hex prefix, got %q", rlpHex[:10])
	}
	if len(rlpHex) <= 2 {
		t.Fatal("expected non-empty RLP data")
	}
}

func TestDebugNS_GetBlockRlp_NotFound(t *testing.T) {
	d := NewDebugAPI(newMockBackend())
	resp := callDebug(t, d, "debug_getBlockRlp", "0x999")
	if resp.Error == nil {
		t.Fatal("expected error for non-existent block")
	}
}

// ---------- debug_printBlock ----------

func TestDebugNS_PrintBlock(t *testing.T) {
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

	d := NewDebugAPI(mb)
	resp := callDebug(t, d, "debug_printBlock", "0x2a")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	repr, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}

	// Verify the output contains expected fields.
	if !strings.Contains(repr, "Block #42") {
		t.Fatalf("expected 'Block #42' in output, got: %s", repr)
	}
	if !strings.Contains(repr, "GasLimit:   30000000") {
		t.Fatalf("expected gas limit in output, got: %s", repr)
	}
	if !strings.Contains(repr, "TxCount:    1") {
		t.Fatalf("expected TxCount 1 in output, got: %s", repr)
	}
	if !strings.Contains(repr, "BaseFee:") {
		t.Fatalf("expected BaseFee in output, got: %s", repr)
	}
}

func TestDebugNS_PrintBlock_NotFound(t *testing.T) {
	d := NewDebugAPI(newMockBackend())
	resp := callDebug(t, d, "debug_printBlock", "0x999")
	if resp.Error == nil {
		t.Fatal("expected error for non-existent block")
	}
}

// ---------- debug_chaindbProperty ----------

func TestDebugNS_ChaindbProperty(t *testing.T) {
	d := NewDebugAPI(newMockBackend())

	tests := []struct {
		property string
		contains string
	}{
		{"leveldb.stats", "Compactions"},
		{"leveldb.iostats", "Read(MB)"},
		{"version", "eth2028"},
	}

	for _, tt := range tests {
		resp := callDebug(t, d, "debug_chaindbProperty", tt.property)
		if resp.Error != nil {
			t.Fatalf("error for property %q: %v", tt.property, resp.Error.Message)
		}
		result, ok := resp.Result.(string)
		if !ok {
			t.Fatalf("result not string for %q: %T", tt.property, resp.Result)
		}
		if !strings.Contains(result, tt.contains) {
			t.Fatalf("property %q result = %q, want to contain %q", tt.property, result, tt.contains)
		}
	}
}

func TestDebugNS_ChaindbProperty_Unknown(t *testing.T) {
	d := NewDebugAPI(newMockBackend())
	resp := callDebug(t, d, "debug_chaindbProperty", "nonexistent")
	if resp.Error == nil {
		t.Fatal("expected error for unknown property")
	}
	if resp.Error.Code != ErrCodeInvalidParams {
		t.Fatalf("want error code %d, got %d", ErrCodeInvalidParams, resp.Error.Code)
	}
}

// ---------- debug_chaindbCompact ----------

func TestDebugNS_ChaindbCompact(t *testing.T) {
	d := NewDebugAPI(newMockBackend())
	resp := callDebug(t, d, "debug_chaindbCompact")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
}

// ---------- debug_setHead ----------

func TestDebugNS_SetHead(t *testing.T) {
	d := NewDebugAPI(newMockBackend())

	// Block 42 exists in the mock backend.
	resp := callDebug(t, d, "debug_setHead", "0x2a")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
}

func TestDebugNS_SetHead_NotFound(t *testing.T) {
	d := NewDebugAPI(newMockBackend())
	resp := callDebug(t, d, "debug_setHead", "0x999")
	if resp.Error == nil {
		t.Fatal("expected error for non-existent target block")
	}
}

// ---------- debug_freeOSMemory ----------

func TestDebugNS_FreeOSMemory(t *testing.T) {
	d := NewDebugAPI(newMockBackend())
	resp := callDebug(t, d, "debug_freeOSMemory")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
}

// ---------- Unknown method ----------

func TestDebugNS_UnknownMethod(t *testing.T) {
	d := NewDebugAPI(newMockBackend())
	resp := callDebug(t, d, "debug_nonexistent")
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != ErrCodeMethodNotFound {
		t.Fatalf("want error code %d, got %d", ErrCodeMethodNotFound, resp.Error.Code)
	}
}

// ---------- Missing parameter edge cases ----------

func TestDebugNS_TraceBlockByNumber_MissingParam(t *testing.T) {
	d := NewDebugAPI(newMockBackend())
	resp := callDebug(t, d, "debug_traceBlockByNumber")
	if resp.Error == nil {
		t.Fatal("expected error for missing parameter")
	}
	if resp.Error.Code != ErrCodeInvalidParams {
		t.Fatalf("want error code %d, got %d", ErrCodeInvalidParams, resp.Error.Code)
	}
}

func TestDebugNS_GetBlockRlp_MissingParam(t *testing.T) {
	d := NewDebugAPI(newMockBackend())
	resp := callDebug(t, d, "debug_getBlockRlp")
	if resp.Error == nil {
		t.Fatal("expected error for missing parameter")
	}
	if resp.Error.Code != ErrCodeInvalidParams {
		t.Fatalf("want error code %d, got %d", ErrCodeInvalidParams, resp.Error.Code)
	}
}
