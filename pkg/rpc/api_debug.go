package rpc

import (
	"encoding/json"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
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

// debugTraceCall implements debug_traceCall.
// Simulates a call and returns an execution trace without creating a transaction.
func (api *EthAPI) debugTraceCall(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing call arguments")
	}

	var args CallArgs
	if err := json.Unmarshal(req.Params[0], &args); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, "invalid call arguments: "+err.Error())
	}

	bn := LatestBlockNumber
	if len(req.Params) > 1 {
		if err := json.Unmarshal(req.Params[1], &bn); err != nil {
			return errorResponse(req.ID, ErrCodeInvalidParams, "invalid block number: "+err.Error())
		}
	}

	// Parse optional tracer config (third param); accepted but not fully used yet.
	// A full implementation would support "callTracer", "prestateTracer", etc.

	from := types.Address{}
	if args.From != nil {
		from = types.HexToAddress(*args.From)
	}
	var to *types.Address
	if args.To != nil {
		addr := types.HexToAddress(*args.To)
		to = &addr
	}
	gas := uint64(50_000_000)
	if args.Gas != nil {
		gas = parseHexUint64(*args.Gas)
	}
	value := new(big.Int)
	if args.Value != nil {
		value = parseHexBigInt(*args.Value)
	}
	data := args.GetData()

	result, gasUsed, err := api.backend.EVMCall(from, to, data, gas, value, bn)
	if err != nil {
		return errorResponse(req.ID, ErrCodeInternal, "execution error: "+err.Error())
	}

	// For a full implementation, we would capture step-by-step EVM logs.
	// Currently returns a minimal trace with the gas used and return value.
	trace := &TraceResult{
		Gas:         gasUsed,
		Failed:      false,
		ReturnValue: encodeBytes(result),
		StructLogs:  []StructLog{},
	}

	return successResponse(req.ID, trace)
}
