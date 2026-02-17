package rpc

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
)

// EthAPI implements the eth_ namespace JSON-RPC methods.
type EthAPI struct {
	backend Backend
}

// NewEthAPI creates a new eth_ namespace API service.
func NewEthAPI(backend Backend) *EthAPI {
	return &EthAPI{backend: backend}
}

// HandleRequest dispatches a JSON-RPC request to the appropriate method.
func (api *EthAPI) HandleRequest(req *Request) *Response {
	switch req.Method {
	case "eth_chainId":
		return api.chainID(req)
	case "eth_blockNumber":
		return api.blockNumber(req)
	case "eth_getBlockByNumber":
		return api.getBlockByNumber(req)
	case "eth_getBlockByHash":
		return api.getBlockByHash(req)
	case "eth_getBalance":
		return api.getBalance(req)
	case "eth_getTransactionCount":
		return api.getTransactionCount(req)
	case "eth_getCode":
		return api.getCode(req)
	case "eth_getStorageAt":
		return api.getStorageAt(req)
	case "eth_gasPrice":
		return api.gasPrice(req)
	case "eth_getTransactionByHash":
		return api.getTransactionByHash(req)
	case "eth_getTransactionReceipt":
		return api.getTransactionReceipt(req)
	case "eth_call":
		return api.ethCall(req)
	case "eth_estimateGas":
		return api.estimateGas(req)
	case "eth_sendRawTransaction":
		return api.sendRawTransaction(req)
	case "eth_getLogs":
		return api.getLogs(req)
	case "eth_getBlockReceipts":
		return api.getBlockReceipts(req)
	case "web3_clientVersion":
		return api.clientVersion(req)
	case "net_version":
		return api.netVersion(req)
	default:
		return errorResponse(req.ID, ErrCodeMethodNotFound, fmt.Sprintf("method %q not found", req.Method))
	}
}

func (api *EthAPI) chainID(req *Request) *Response {
	id := api.backend.ChainID()
	return successResponse(req.ID, encodeBigInt(id))
}

func (api *EthAPI) blockNumber(req *Request) *Response {
	header := api.backend.CurrentHeader()
	if header == nil {
		return errorResponse(req.ID, ErrCodeInternal, "no current header")
	}
	return successResponse(req.ID, encodeUint64(header.Number.Uint64()))
}

func (api *EthAPI) getBlockByNumber(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing block number parameter")
	}

	var bn BlockNumber
	if err := json.Unmarshal(req.Params[0], &bn); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}

	header := api.backend.HeaderByNumber(bn)
	if header == nil {
		return successResponse(req.ID, nil)
	}
	return successResponse(req.ID, FormatHeader(header))
}

func (api *EthAPI) getBlockByHash(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing block hash parameter")
	}

	var hashHex string
	if err := json.Unmarshal(req.Params[0], &hashHex); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}

	hash := types.HexToHash(hashHex)
	header := api.backend.HeaderByHash(hash)
	if header == nil {
		return successResponse(req.ID, nil)
	}
	return successResponse(req.ID, FormatHeader(header))
}

func (api *EthAPI) getBalance(req *Request) *Response {
	if len(req.Params) < 2 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing address or block number")
	}

	var addrHex string
	if err := json.Unmarshal(req.Params[0], &addrHex); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}

	var bn BlockNumber
	if err := json.Unmarshal(req.Params[1], &bn); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}

	header := api.backend.HeaderByNumber(bn)
	if header == nil {
		return errorResponse(req.ID, ErrCodeInternal, "block not found")
	}

	statedb, err := api.backend.StateAt(header.Root)
	if err != nil {
		return errorResponse(req.ID, ErrCodeInternal, err.Error())
	}

	addr := types.HexToAddress(addrHex)
	balance := statedb.GetBalance(addr)
	return successResponse(req.ID, encodeBigInt(balance))
}

func (api *EthAPI) getTransactionCount(req *Request) *Response {
	if len(req.Params) < 2 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing address or block number")
	}

	var addrHex string
	if err := json.Unmarshal(req.Params[0], &addrHex); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}

	var bn BlockNumber
	if err := json.Unmarshal(req.Params[1], &bn); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}

	header := api.backend.HeaderByNumber(bn)
	if header == nil {
		return errorResponse(req.ID, ErrCodeInternal, "block not found")
	}

	statedb, err := api.backend.StateAt(header.Root)
	if err != nil {
		return errorResponse(req.ID, ErrCodeInternal, err.Error())
	}

	addr := types.HexToAddress(addrHex)
	nonce := statedb.GetNonce(addr)
	return successResponse(req.ID, encodeUint64(nonce))
}

func (api *EthAPI) getCode(req *Request) *Response {
	if len(req.Params) < 2 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing address or block number")
	}

	var addrHex string
	if err := json.Unmarshal(req.Params[0], &addrHex); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}

	var bn BlockNumber
	if err := json.Unmarshal(req.Params[1], &bn); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}

	header := api.backend.HeaderByNumber(bn)
	if header == nil {
		return errorResponse(req.ID, ErrCodeInternal, "block not found")
	}

	statedb, err := api.backend.StateAt(header.Root)
	if err != nil {
		return errorResponse(req.ID, ErrCodeInternal, err.Error())
	}

	addr := types.HexToAddress(addrHex)
	code := statedb.GetCode(addr)
	return successResponse(req.ID, encodeBytes(code))
}

func (api *EthAPI) getStorageAt(req *Request) *Response {
	if len(req.Params) < 3 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing address, slot, or block number")
	}

	var addrHex, slotHex string
	if err := json.Unmarshal(req.Params[0], &addrHex); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}
	if err := json.Unmarshal(req.Params[1], &slotHex); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}

	var bn BlockNumber
	if err := json.Unmarshal(req.Params[2], &bn); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}

	header := api.backend.HeaderByNumber(bn)
	if header == nil {
		return errorResponse(req.ID, ErrCodeInternal, "block not found")
	}

	statedb, err := api.backend.StateAt(header.Root)
	if err != nil {
		return errorResponse(req.ID, ErrCodeInternal, err.Error())
	}

	addr := types.HexToAddress(addrHex)
	slot := types.HexToHash(slotHex)
	value := statedb.GetState(addr, slot)
	return successResponse(req.ID, encodeHash(value))
}

func (api *EthAPI) gasPrice(req *Request) *Response {
	price := api.backend.SuggestGasPrice()
	if price == nil {
		price = new(big.Int)
	}
	return successResponse(req.ID, encodeBigInt(price))
}

func (api *EthAPI) clientVersion(req *Request) *Response {
	return successResponse(req.ID, "eth2028/v0.1.0")
}

func (api *EthAPI) netVersion(req *Request) *Response {
	id := api.backend.ChainID()
	return successResponse(req.ID, id.String())
}

func successResponse(id json.RawMessage, result interface{}) *Response {
	return &Response{
		JSONRPC: "2.0",
		Result:  result,
		ID:      id,
	}
}

func errorResponse(id json.RawMessage, code int, message string) *Response {
	return &Response{
		JSONRPC: "2.0",
		Error:   &RPCError{Code: code, Message: message},
		ID:      id,
	}
}
