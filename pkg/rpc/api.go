package rpc

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// defaultPriorityFee is 1 Gwei, the suggested default max priority fee.
var defaultPriorityFee = big.NewInt(1_000_000_000)

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
	case "eth_maxPriorityFeePerGas":
		return api.maxPriorityFeePerGas(req)
	case "eth_feeHistory":
		return api.feeHistory(req)
	case "eth_syncing":
		return api.syncing(req)
	case "eth_createAccessList":
		return api.createAccessList(req)
	case "eth_subscribe":
		return api.ethSubscribe(req)
	case "eth_unsubscribe":
		return api.ethUnsubscribe(req)
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
	case "eth_getProof":
		return api.getProof(req)
	case "eth_getHeaderByNumber":
		return api.getHeaderByNumber(req)
	case "eth_getHeaderByHash":
		return api.getHeaderByHash(req)
	case "eth_getTransactionByBlockHashAndIndex":
		return api.getTransactionByBlockHashAndIndex(req)
	case "eth_getTransactionByBlockNumberAndIndex":
		return api.getTransactionByBlockNumberAndIndex(req)
	case "eth_getBlockTransactionCountByHash":
		return api.getBlockTransactionCountByHash(req)
	case "eth_getBlockTransactionCountByNumber":
		return api.getBlockTransactionCountByNumber(req)
	case "eth_accounts":
		return api.accounts(req)
	case "eth_coinbase":
		return api.coinbase(req)
	case "eth_mining":
		return api.mining(req)
	case "eth_hashrate":
		return api.hashrate(req)
	case "eth_protocolVersion":
		return api.protocolVersion(req)
	case "eth_getUncleCountByBlockHash":
		return api.getUncleCountByBlockHash(req)
	case "eth_getUncleCountByBlockNumber":
		return api.getUncleCountByBlockNumber(req)
	case "eth_getUncleByBlockHashAndIndex":
		return api.getUncleByBlockHashAndIndex(req)
	case "eth_getUncleByBlockNumberAndIndex":
		return api.getUncleByBlockNumberAndIndex(req)
	case "eth_blobBaseFee":
		return api.getBlobBaseFee(req)
	case "debug_traceTransaction":
		return api.debugTraceTransaction(req)
	case "debug_traceCall":
		return api.debugTraceCall(req)
	case "debug_traceBlockByNumber":
		return api.debugTraceBlockByNumber(req)
	case "debug_traceBlockByHash":
		return api.debugTraceBlockByHash(req)
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

// historyPruned returns true if the given block number's body/receipt data
// has been pruned per EIP-4444.
func (api *EthAPI) historyPruned(blockNum uint64) bool {
	oldest := api.backend.HistoryOldestBlock()
	return oldest > 0 && blockNum < oldest
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

	// Parse the optional fullTx boolean (second param).
	fullTx := false
	if len(req.Params) > 1 {
		_ = json.Unmarshal(req.Params[1], &fullTx)
	}

	header := api.backend.HeaderByNumber(bn)
	if header == nil {
		return successResponse(req.ID, nil)
	}

	if fullTx {
		// EIP-4444: check if block body has been pruned.
		if api.historyPruned(header.Number.Uint64()) {
			return errorResponse(req.ID, ErrCodeHistoryPruned,
				"historical block body pruned (EIP-4444)")
		}
		block := api.backend.BlockByNumber(bn)
		if block != nil {
			return successResponse(req.ID, FormatBlock(block, true))
		}
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

	// Parse the optional fullTx boolean (second param).
	fullTx := false
	if len(req.Params) > 1 {
		_ = json.Unmarshal(req.Params[1], &fullTx)
	}

	hash := types.HexToHash(hashHex)
	header := api.backend.HeaderByHash(hash)
	if header == nil {
		return successResponse(req.ID, nil)
	}

	if fullTx {
		// EIP-4444: check if block body has been pruned.
		if api.historyPruned(header.Number.Uint64()) {
			return errorResponse(req.ID, ErrCodeHistoryPruned,
				"historical block body pruned (EIP-4444)")
		}
		block := api.backend.BlockByHash(hash)
		if block != nil {
			return successResponse(req.ID, FormatBlock(block, true))
		}
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
	return successResponse(req.ID, "ETH2030/v0.1.0")
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

// maxPriorityFeePerGas returns the suggested priority fee (1 Gwei default).
func (api *EthAPI) maxPriorityFeePerGas(req *Request) *Response {
	return successResponse(req.ID, encodeBigInt(defaultPriorityFee))
}

// FeeHistoryResult is the response for eth_feeHistory.
type FeeHistoryResult struct {
	OldestBlock   string     `json:"oldestBlock"`
	BaseFeePerGas []string   `json:"baseFeePerGas"`
	GasUsedRatio  []float64  `json:"gasUsedRatio"`
	Reward        [][]string `json:"reward,omitempty"`
}

// feeHistory returns base fee and gas usage history over a range of blocks.
func (api *EthAPI) feeHistory(req *Request) *Response {
	if len(req.Params) < 2 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing blockCount or newestBlock")
	}

	// Parse block count (hex or decimal)
	var blockCountHex string
	if err := json.Unmarshal(req.Params[0], &blockCountHex); err != nil {
		// Try as integer
		var blockCount int
		if err2 := json.Unmarshal(req.Params[0], &blockCount); err2 != nil {
			return errorResponse(req.ID, ErrCodeInvalidParams, "invalid blockCount: "+err.Error())
		}
		blockCountHex = fmt.Sprintf("0x%x", blockCount)
	}
	blockCount := parseHexUint64(blockCountHex)
	if blockCount == 0 || blockCount > 1024 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "blockCount must be 1..1024")
	}

	var newestBN BlockNumber
	if err := json.Unmarshal(req.Params[1], &newestBN); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, "invalid newestBlock: "+err.Error())
	}

	// Parse optional reward percentiles
	var rewardPercentiles []float64
	if len(req.Params) > 2 {
		if err := json.Unmarshal(req.Params[2], &rewardPercentiles); err != nil {
			return errorResponse(req.ID, ErrCodeInvalidParams, "invalid rewardPercentiles: "+err.Error())
		}
	}

	// Resolve newest block
	newestHeader := api.backend.HeaderByNumber(newestBN)
	if newestHeader == nil {
		return errorResponse(req.ID, ErrCodeInternal, "block not found")
	}
	newestNum := newestHeader.Number.Uint64()

	// Calculate block range
	oldest := uint64(0)
	if newestNum+1 >= blockCount {
		oldest = newestNum + 1 - blockCount
	}

	result := &FeeHistoryResult{
		OldestBlock: encodeUint64(oldest),
	}

	// Collect baseFeePerGas and gasUsedRatio for each block in range,
	// plus the baseFee of the next block (blockCount + 1 entries total).
	for i := oldest; i <= newestNum+1; i++ {
		header := api.backend.HeaderByNumber(BlockNumber(i))
		if header != nil && header.BaseFee != nil {
			result.BaseFeePerGas = append(result.BaseFeePerGas, encodeBigInt(header.BaseFee))
		} else {
			result.BaseFeePerGas = append(result.BaseFeePerGas, "0x0")
		}

		// gasUsedRatio only for blocks in the range (not the extra entry).
		if i <= newestNum {
			if header != nil && header.GasLimit > 0 {
				ratio := float64(header.GasUsed) / float64(header.GasLimit)
				result.GasUsedRatio = append(result.GasUsedRatio, ratio)
			} else {
				result.GasUsedRatio = append(result.GasUsedRatio, 0)
			}
		}
	}

	// If reward percentiles are requested, return default priority fee for each.
	if len(rewardPercentiles) > 0 {
		for i := oldest; i <= newestNum; i++ {
			rewards := make([]string, len(rewardPercentiles))
			for j := range rewardPercentiles {
				rewards[j] = encodeBigInt(defaultPriorityFee)
			}
			result.Reward = append(result.Reward, rewards)
		}
	}

	return successResponse(req.ID, result)
}

// SyncStatus is the response for eth_syncing when the node is syncing.
type SyncStatus struct {
	StartingBlock string `json:"startingBlock"`
	CurrentBlock  string `json:"currentBlock"`
	HighestBlock  string `json:"highestBlock"`
}

// syncing returns the sync status. Returns false when fully synced.
func (api *EthAPI) syncing(req *Request) *Response {
	// For now, we report as fully synced.
	return successResponse(req.ID, false)
}

// AccessListResult is the response for eth_createAccessList.
type AccessListResult struct {
	AccessList []AccessListEntry `json:"accessList"`
	GasUsed    string            `json:"gasUsed"`
}

// AccessListEntry is a single entry in an access list result.
type AccessListEntry struct {
	Address     string   `json:"address"`
	StorageKeys []string `json:"storageKeys"`
}

// createAccessList simulates a tx and returns an access list.
func (api *EthAPI) createAccessList(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing call arguments")
	}

	var args CallArgs
	if err := json.Unmarshal(req.Params[0], &args); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}

	bn := LatestBlockNumber
	if len(req.Params) > 1 {
		if err := json.Unmarshal(req.Params[1], &bn); err != nil {
			return errorResponse(req.ID, ErrCodeInvalidParams, "invalid block number: "+err.Error())
		}
	}

	from, to, gas, value, data := parseCallArgs(&args)

	// Execute the call to determine gas usage.
	_, gasUsed, err := api.backend.EVMCall(from, to, data, gas, value, bn)
	if err != nil {
		return errorResponse(req.ID, ErrCodeInternal, "execution error: "+err.Error())
	}

	// A full implementation would trace storage accesses during execution.
	// For now, return an empty access list with the gas used.
	result := &AccessListResult{
		AccessList: []AccessListEntry{},
		GasUsed:    encodeUint64(gasUsed),
	}

	return successResponse(req.ID, result)
}

// ethSubscribe creates a new subscription (WebSocket-oriented).
func (api *EthAPI) ethSubscribe(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing subscription type")
	}

	var subType string
	if err := json.Unmarshal(req.Params[0], &subType); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}

	switch subType {
	case "newHeads":
		id := api.subs.Subscribe(SubNewHeads, FilterQuery{})
		return successResponse(req.ID, id)
	case "logs":
		var query FilterQuery
		if len(req.Params) > 1 {
			var criteria FilterCriteria
			if err := json.Unmarshal(req.Params[1], &criteria); err != nil {
				return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
			}
			query = criteriaToQuery(criteria, api.backend)
		}
		id := api.subs.Subscribe(SubLogs, query)
		return successResponse(req.ID, id)
	case "newPendingTransactions":
		id := api.subs.Subscribe(SubPendingTx, FilterQuery{})
		return successResponse(req.ID, id)
	default:
		return errorResponse(req.ID, ErrCodeInvalidParams, fmt.Sprintf("unsupported subscription type: %q", subType))
	}
}

// ethUnsubscribe removes a subscription by ID.
func (api *EthAPI) ethUnsubscribe(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing subscription ID")
	}

	var subID string
	if err := json.Unmarshal(req.Params[0], &subID); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}

	ok := api.subs.Unsubscribe(subID)
	return successResponse(req.ID, ok)
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
