package rpc

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// EthAPI implements the eth_, net_, and web3_ namespace JSON-RPC methods.
type EthAPI struct {
	backend Backend
	subs    *SubscriptionManager
}

// NewEthAPI creates a new API service with subscription support.
func NewEthAPI(backend Backend) *EthAPI {
	return &EthAPI{
		backend: backend,
		subs:    NewSubscriptionManager(backend),
	}
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
	case "eth_newFilter":
		return api.newFilter(req)
	case "eth_newBlockFilter":
		return api.newBlockFilter(req)
	case "eth_newPendingTransactionFilter":
		return api.newPendingTransactionFilter(req)
	case "eth_getFilterChanges":
		return api.getFilterChanges(req)
	case "eth_getFilterLogs":
		return api.getFilterLogs(req)
	case "eth_uninstallFilter":
		return api.uninstallFilter(req)
	case "web3_clientVersion":
		return api.clientVersion(req)
	case "web3_sha3":
		return api.web3Sha3(req)
	case "net_version":
		return api.netVersion(req)
	case "net_listening":
		return api.netListening(req)
	case "net_peerCount":
		return api.netPeerCount(req)
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

func (api *EthAPI) netListening(req *Request) *Response {
	return successResponse(req.ID, true)
}

func (api *EthAPI) netPeerCount(req *Request) *Response {
	return successResponse(req.ID, encodeUint64(0))
}

func (api *EthAPI) web3Sha3(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing data parameter")
	}

	var dataHex string
	if err := json.Unmarshal(req.Params[0], &dataHex); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}

	data := fromHexBytes(dataHex)
	hash := crypto.Keccak256Hash(data)
	return successResponse(req.ID, encodeHash(hash))
}

// newFilter creates a log filter and returns its filter ID.
func (api *EthAPI) newFilter(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing filter criteria")
	}

	var criteria FilterCriteria
	if err := json.Unmarshal(req.Params[0], &criteria); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}

	query := criteriaToQuery(criteria, api.backend)
	id := api.subs.NewLogFilter(query)
	return successResponse(req.ID, id)
}

// newBlockFilter creates a block filter and returns its filter ID.
func (api *EthAPI) newBlockFilter(req *Request) *Response {
	id := api.subs.NewBlockFilter()
	return successResponse(req.ID, id)
}

// newPendingTransactionFilter creates a pending tx filter.
func (api *EthAPI) newPendingTransactionFilter(req *Request) *Response {
	id := api.subs.NewPendingTxFilter()
	return successResponse(req.ID, id)
}

// getFilterChanges returns new results since the last poll.
func (api *EthAPI) getFilterChanges(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing filter ID")
	}

	var filterID string
	if err := json.Unmarshal(req.Params[0], &filterID); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}

	result, ok := api.subs.GetFilterChanges(filterID)
	if !ok {
		return errorResponse(req.ID, ErrCodeInvalidParams, "filter not found")
	}

	// Format the result depending on filter type.
	switch v := result.(type) {
	case []*types.Log:
		rpcLogs := make([]*RPCLog, len(v))
		for i, log := range v {
			rpcLogs[i] = FormatLog(log)
		}
		return successResponse(req.ID, rpcLogs)
	case []types.Hash:
		hashes := make([]string, len(v))
		for i, h := range v {
			hashes[i] = encodeHash(h)
		}
		return successResponse(req.ID, hashes)
	default:
		return successResponse(req.ID, result)
	}
}

// getFilterLogs returns all logs matching an installed log filter.
func (api *EthAPI) getFilterLogs(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing filter ID")
	}

	var filterID string
	if err := json.Unmarshal(req.Params[0], &filterID); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}

	logs, ok := api.subs.GetFilterLogs(filterID)
	if !ok {
		return errorResponse(req.ID, ErrCodeInvalidParams, "filter not found")
	}

	rpcLogs := make([]*RPCLog, len(logs))
	for i, log := range logs {
		rpcLogs[i] = FormatLog(log)
	}
	return successResponse(req.ID, rpcLogs)
}

// uninstallFilter removes a filter by ID.
func (api *EthAPI) uninstallFilter(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing filter ID")
	}

	var filterID string
	if err := json.Unmarshal(req.Params[0], &filterID); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}

	ok := api.subs.Uninstall(filterID)
	return successResponse(req.ID, ok)
}

// criteriaToQuery converts a JSON-RPC FilterCriteria to an internal FilterQuery.
func criteriaToQuery(c FilterCriteria, backend Backend) FilterQuery {
	var q FilterQuery

	if c.FromBlock != nil {
		var from uint64
		if *c.FromBlock == LatestBlockNumber {
			header := backend.CurrentHeader()
			if header != nil {
				from = header.Number.Uint64()
			}
		} else {
			from = uint64(*c.FromBlock)
		}
		q.FromBlock = &from
	}

	if c.ToBlock != nil {
		var to uint64
		if *c.ToBlock == LatestBlockNumber {
			header := backend.CurrentHeader()
			if header != nil {
				to = header.Number.Uint64()
			}
		} else {
			to = uint64(*c.ToBlock)
		}
		q.ToBlock = &to
	}

	for _, addrHex := range c.Addresses {
		q.Addresses = append(q.Addresses, types.HexToAddress(addrHex))
	}

	for _, topicList := range c.Topics {
		var hashes []types.Hash
		for _, topicHex := range topicList {
			hashes = append(hashes, types.HexToHash(topicHex))
		}
		q.Topics = append(q.Topics, hashes)
	}

	return q
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
