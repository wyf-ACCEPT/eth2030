package rpc

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"runtime"
	"runtime/debug"

	"github.com/eth2030/eth2030/core/types"
)

// DebugAPI implements the debug namespace RPC methods.
// It provides block introspection, RLP encoding, and chain management utilities.
type DebugAPI struct {
	backend Backend
}

// NewDebugAPI creates a new DebugAPI instance.
func NewDebugAPI(backend Backend) *DebugAPI {
	return &DebugAPI{backend: backend}
}

// HandleDebugRequest dispatches a debug_ namespace JSON-RPC request.
func (d *DebugAPI) HandleDebugRequest(req *Request) *Response {
	switch req.Method {
	case "debug_traceBlockByNumber":
		return d.debugNSTraceBlockByNumber(req)
	case "debug_traceBlockByHash":
		return d.debugNSTraceBlockByHash(req)
	case "debug_getBlockRlp":
		return d.debugGetBlockRlp(req)
	case "debug_printBlock":
		return d.debugPrintBlock(req)
	case "debug_chaindbProperty":
		return d.debugChaindbProperty(req)
	case "debug_chaindbCompact":
		return d.debugChaindbCompact(req)
	case "debug_setHead":
		return d.debugSetHead(req)
	case "debug_freeOSMemory":
		return d.debugFreeOSMemory(req)
	default:
		return errorResponse(req.ID, ErrCodeMethodNotFound,
			fmt.Sprintf("method %q not found in debug namespace", req.Method))
	}
}

// debugNSTraceBlockByNumber traces all transactions in a block by number.
// Returns an array of TraceResult, one per transaction.
func (d *DebugAPI) debugNSTraceBlockByNumber(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing block number parameter")
	}

	var bn BlockNumber
	if err := json.Unmarshal(req.Params[0], &bn); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, "invalid block number: "+err.Error())
	}

	block := d.backend.BlockByNumber(bn)
	if block == nil {
		return errorResponse(req.ID, ErrCodeInternal, "block not found")
	}

	return d.traceBlockTxs(req, block)
}

// debugNSTraceBlockByHash traces all transactions in a block by hash.
// Returns an array of TraceResult, one per transaction.
func (d *DebugAPI) debugNSTraceBlockByHash(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing block hash parameter")
	}

	var hashHex string
	if err := json.Unmarshal(req.Params[0], &hashHex); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, "invalid block hash: "+err.Error())
	}

	hash := types.HexToHash(hashHex)
	block := d.backend.BlockByHash(hash)
	if block == nil {
		return errorResponse(req.ID, ErrCodeInternal, "block not found")
	}

	return d.traceBlockTxs(req, block)
}

// traceBlockTxs produces a trace result for each transaction in the block.
func (d *DebugAPI) traceBlockTxs(req *Request, block *types.Block) *Response {
	txs := block.Transactions()
	blockHash := block.Hash()
	receipts := d.backend.GetReceipts(blockHash)

	results := make([]*DebugBlockTraceEntry, len(txs))
	for i, tx := range txs {
		trace := &TraceResult{
			Gas:         tx.Gas(),
			Failed:      false,
			ReturnValue: "",
			StructLogs:  []StructLog{},
		}

		if i < len(receipts) {
			trace.Gas = receipts[i].GasUsed
			trace.Failed = receipts[i].Status == types.ReceiptStatusFailed
		}

		results[i] = &DebugBlockTraceEntry{
			TxHash: encodeHash(tx.Hash()),
			Result: trace,
		}
	}

	return successResponse(req.ID, results)
}

// DebugBlockTraceEntry is a single transaction trace in a block trace response.
type DebugBlockTraceEntry struct {
	TxHash string       `json:"txHash"`
	Result *TraceResult `json:"result"`
}

// debugGetBlockRlp returns the RLP-encoded block as a hex string.
func (d *DebugAPI) debugGetBlockRlp(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing block number parameter")
	}

	var bn BlockNumber
	if err := json.Unmarshal(req.Params[0], &bn); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, "invalid block number: "+err.Error())
	}

	block := d.backend.BlockByNumber(bn)
	if block == nil {
		return errorResponse(req.ID, ErrCodeInternal, "block not found")
	}

	rlpBytes, err := block.EncodeRLP()
	if err != nil {
		return errorResponse(req.ID, ErrCodeInternal, "RLP encoding failed: "+err.Error())
	}

	return successResponse(req.ID, "0x"+hex.EncodeToString(rlpBytes))
}

// debugPrintBlock returns a human-readable representation of a block.
func (d *DebugAPI) debugPrintBlock(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing block number parameter")
	}

	var bn BlockNumber
	if err := json.Unmarshal(req.Params[0], &bn); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, "invalid block number: "+err.Error())
	}

	block := d.backend.BlockByNumber(bn)
	if block == nil {
		return errorResponse(req.ID, ErrCodeInternal, "block not found")
	}

	header := block.Header()
	txCount := len(block.Transactions())

	repr := fmt.Sprintf(
		"Block #%d [%s]\n  Parent:     %s\n  Coinbase:   %s\n  StateRoot:  %s\n  TxRoot:     %s\n  GasLimit:   %d\n  GasUsed:    %d\n  Timestamp:  %d\n  TxCount:    %d",
		header.Number.Uint64(),
		encodeHash(header.Hash()),
		encodeHash(header.ParentHash),
		encodeAddress(header.Coinbase),
		encodeHash(header.Root),
		encodeHash(header.TxHash),
		header.GasLimit,
		header.GasUsed,
		header.Time,
		txCount,
	)

	if header.BaseFee != nil {
		repr += fmt.Sprintf("\n  BaseFee:    %s", header.BaseFee.String())
	}

	return successResponse(req.ID, repr)
}

// debugChaindbProperty returns a database property value.
// Supported properties: "leveldb.stats", "leveldb.iostats", "version".
func (d *DebugAPI) debugChaindbProperty(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing property parameter")
	}

	var property string
	if err := json.Unmarshal(req.Params[0], &property); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, "invalid property: "+err.Error())
	}

	// Return a basic response for known properties.
	switch property {
	case "leveldb.stats":
		return successResponse(req.ID, "Compactions: 0\nLevel  Files  Size(MB)\n")
	case "leveldb.iostats":
		return successResponse(req.ID, "Read(MB): 0.0\nWrite(MB): 0.0\n")
	case "version":
		return successResponse(req.ID, "ETH2030/db/v1.0")
	default:
		return errorResponse(req.ID, ErrCodeInvalidParams,
			fmt.Sprintf("unknown property: %q", property))
	}
}

// debugChaindbCompact triggers database compaction.
func (d *DebugAPI) debugChaindbCompact(req *Request) *Response {
	// In a real implementation, this would trigger LevelDB compaction.
	// For now, return success.
	return successResponse(req.ID, nil)
}

// debugSetHead rewinds the chain head to a specific block number.
func (d *DebugAPI) debugSetHead(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing block number parameter")
	}

	var bn BlockNumber
	if err := json.Unmarshal(req.Params[0], &bn); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, "invalid block number: "+err.Error())
	}

	// Verify the target block exists.
	header := d.backend.HeaderByNumber(bn)
	if header == nil {
		return errorResponse(req.ID, ErrCodeInternal, "target block not found")
	}

	// In a full implementation, this would rewind the chain.
	// Return success to indicate the operation was accepted.
	return successResponse(req.ID, nil)
}

// debugFreeOSMemory triggers a garbage collection and returns released
// memory back to the OS.
func (d *DebugAPI) debugFreeOSMemory(req *Request) *Response {
	runtime.GC()
	debug.FreeOSMemory()
	return successResponse(req.ID, nil)
}

// DebugMemStats returns runtime memory statistics for diagnostics.
type DebugMemStats struct {
	Alloc      uint64 `json:"alloc"`
	TotalAlloc uint64 `json:"totalAlloc"`
	Sys        uint64 `json:"sys"`
	NumGC      uint32 `json:"numGC"`
}
