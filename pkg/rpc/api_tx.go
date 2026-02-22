package rpc

import (
	"encoding/json"

	"github.com/eth2030/eth2030/core/types"
)

// getTransactionByHash returns transaction info by hash.
func (api *EthAPI) getTransactionByHash(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing transaction hash")
	}

	var hashHex string
	if err := json.Unmarshal(req.Params[0], &hashHex); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}

	hash := types.HexToHash(hashHex)
	tx, blockNum, index := api.backend.GetTransaction(hash)
	if tx == nil {
		return successResponse(req.ID, nil)
	}

	var blockHash *types.Hash
	if blockNum > 0 {
		header := api.backend.HeaderByNumber(BlockNumber(blockNum))
		if header != nil {
			h := header.Hash()
			blockHash = &h
		}
	}

	return successResponse(req.ID, FormatTransaction(tx, blockHash, &blockNum, &index))
}

// getTransactionReceipt returns a receipt for a transaction hash.
func (api *EthAPI) getTransactionReceipt(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing transaction hash")
	}

	var hashHex string
	if err := json.Unmarshal(req.Params[0], &hashHex); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}

	txHash := types.HexToHash(hashHex)
	tx, blockNum, _ := api.backend.GetTransaction(txHash)
	if tx == nil {
		return successResponse(req.ID, nil)
	}

	// EIP-4444: check if receipt has been pruned.
	if api.historyPruned(blockNum) {
		return errorResponse(req.ID, ErrCodeHistoryPruned,
			"historical receipt pruned (EIP-4444)")
	}

	// Get the block header for block hash
	header := api.backend.HeaderByNumber(BlockNumber(blockNum))
	if header == nil {
		return successResponse(req.ID, nil)
	}

	blockHash := header.Hash()
	receipts := api.backend.GetReceipts(blockHash)

	// Find the receipt matching our tx hash
	for _, receipt := range receipts {
		if receipt.TxHash == txHash {
			return successResponse(req.ID, FormatReceipt(receipt, tx))
		}
	}

	return successResponse(req.ID, nil)
}

// sendRawTransaction decodes an RLP-encoded transaction and submits it.
func (api *EthAPI) sendRawTransaction(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing raw transaction data")
	}

	var dataHex string
	if err := json.Unmarshal(req.Params[0], &dataHex); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}

	rawBytes := fromHexBytes(dataHex)
	if len(rawBytes) == 0 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "empty transaction data")
	}

	// For now, create a minimal legacy transaction from the raw bytes.
	// A full implementation would RLP-decode the transaction.
	tx := types.NewTransaction(&types.LegacyTx{
		Data: rawBytes,
	})

	if err := api.backend.SendTransaction(tx); err != nil {
		return errorResponse(req.ID, ErrCodeInternal, err.Error())
	}

	return successResponse(req.ID, encodeHash(tx.Hash()))
}
