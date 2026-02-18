package rpc

import (
	"encoding/json"

	"github.com/eth2028/eth2028/core/types"
)

// BlockTraceResult is a single transaction trace within a block trace.
type BlockTraceResult struct {
	TxHash string       `json:"txHash"`
	Result *TraceResult `json:"result"`
}

// debugTraceBlockByNumber implements debug_traceBlockByNumber.
// Returns an array of trace results for each transaction in the block.
func (api *EthAPI) debugTraceBlockByNumber(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing block number parameter")
	}

	var bn BlockNumber
	if err := json.Unmarshal(req.Params[0], &bn); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, "invalid block number: "+err.Error())
	}

	block := api.backend.BlockByNumber(bn)
	if block == nil {
		return errorResponse(req.ID, ErrCodeInternal, "block not found")
	}

	return api.traceBlock(req, block)
}

// debugTraceBlockByHash implements debug_traceBlockByHash.
// Returns an array of trace results for each transaction in the block.
func (api *EthAPI) debugTraceBlockByHash(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing block hash parameter")
	}

	var hashHex string
	if err := json.Unmarshal(req.Params[0], &hashHex); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, "invalid block hash: "+err.Error())
	}

	hash := types.HexToHash(hashHex)
	block := api.backend.BlockByHash(hash)
	if block == nil {
		return errorResponse(req.ID, ErrCodeInternal, "block not found")
	}

	return api.traceBlock(req, block)
}

// traceBlock produces a trace result for each transaction in the block.
// For a full implementation, this would re-execute each transaction with a
// tracing EVM. Currently returns a minimal trace per transaction.
func (api *EthAPI) traceBlock(req *Request, block *types.Block) *Response {
	txs := block.Transactions()
	blockHash := block.Hash()

	// Get receipts for this block to determine per-tx gas usage and status.
	receipts := api.backend.GetReceipts(blockHash)

	results := make([]*BlockTraceResult, len(txs))
	for i, tx := range txs {
		trace := &TraceResult{
			Gas:         tx.Gas(),
			Failed:      false,
			ReturnValue: "",
			StructLogs:  []StructLog{},
		}

		// If receipts are available, use actual gas used and status.
		if i < len(receipts) {
			trace.Gas = receipts[i].GasUsed
			trace.Failed = receipts[i].Status == types.ReceiptStatusFailed
		}

		results[i] = &BlockTraceResult{
			TxHash: encodeHash(tx.Hash()),
			Result: trace,
		}
	}

	return successResponse(req.ID, results)
}
