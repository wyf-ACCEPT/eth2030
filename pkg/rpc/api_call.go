package rpc

import (
	"encoding/json"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
)

// ethCall executes a read-only EVM call without creating a transaction.
func (api *EthAPI) ethCall(req *Request) *Response {
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

	result, _, err := api.backend.EVMCall(from, to, data, gas, value, bn)
	if err != nil {
		return errorResponse(req.ID, ErrCodeInternal, "execution error: "+err.Error())
	}

	return successResponse(req.ID, encodeBytes(result))
}

// estimateGas estimates the gas needed to execute a transaction.
// Uses binary search between the intrinsic gas floor and the block gas limit.
func (api *EthAPI) estimateGas(req *Request) *Response {
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

	from, to, _, value, data := parseCallArgs(&args)

	// Get block gas limit as upper bound
	header := api.backend.HeaderByNumber(bn)
	if header == nil {
		return errorResponse(req.ID, ErrCodeInternal, "block not found")
	}

	hi := header.GasLimit
	// Intrinsic gas as lower bound (21000 base)
	lo := uint64(21000)

	// If user specified gas, use it as upper bound
	if args.Gas != nil {
		userGas := parseHexUint64(*args.Gas)
		if userGas > 0 && userGas < hi {
			hi = userGas
		}
	}

	// Check that the upper bound works
	_, _, err := api.backend.EVMCall(from, to, data, hi, value, bn)
	if err != nil {
		return errorResponse(req.ID, ErrCodeInternal, "execution error: "+err.Error())
	}

	// Check if the lower bound itself works.
	_, _, errLo := api.backend.EVMCall(from, to, data, lo, value, bn)
	if errLo == nil {
		return successResponse(req.ID, encodeUint64(lo))
	}

	// Binary search for minimum gas needed
	for lo+1 < hi {
		mid := (lo + hi) / 2
		_, _, err := api.backend.EVMCall(from, to, data, mid, value, bn)
		if err != nil {
			lo = mid
		} else {
			hi = mid
		}
	}

	return successResponse(req.ID, encodeUint64(hi))
}

// getLogs returns logs matching the given filter criteria.
func (api *EthAPI) getLogs(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing filter criteria")
	}

	var criteria FilterCriteria
	if err := json.Unmarshal(req.Params[0], &criteria); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}

	// Determine block range
	fromBlock := uint64(0)
	toBlock := uint64(0)

	current := api.backend.CurrentHeader()
	if current == nil {
		return errorResponse(req.ID, ErrCodeInternal, "no current block")
	}
	currentNum := current.Number.Uint64()

	if criteria.FromBlock != nil {
		if *criteria.FromBlock == LatestBlockNumber {
			fromBlock = currentNum
		} else {
			fromBlock = uint64(*criteria.FromBlock)
		}
	}
	if criteria.ToBlock != nil {
		if *criteria.ToBlock == LatestBlockNumber {
			toBlock = currentNum
		} else {
			toBlock = uint64(*criteria.ToBlock)
		}
	} else {
		toBlock = currentNum
	}

	// Collect matching logs
	var result []*RPCLog

	// Parse address filter
	addrFilter := make(map[types.Address]bool)
	for _, addrHex := range criteria.Addresses {
		addrFilter[types.HexToAddress(addrHex)] = true
	}

	// Parse topic filters
	topicFilter := make([][]types.Hash, len(criteria.Topics))
	for i, topicList := range criteria.Topics {
		for _, topicHex := range topicList {
			topicFilter[i] = append(topicFilter[i], types.HexToHash(topicHex))
		}
	}

	// EIP-4444: check if the requested range includes pruned blocks.
	if api.historyPruned(fromBlock) {
		return errorResponse(req.ID, ErrCodeHistoryPruned,
			"historical logs pruned (EIP-4444)")
	}

	for blockNum := fromBlock; blockNum <= toBlock; blockNum++ {
		header := api.backend.HeaderByNumber(BlockNumber(blockNum))
		if header == nil {
			continue
		}
		blockHash := header.Hash()
		logs := api.backend.GetLogs(blockHash)
		for _, log := range logs {
			if matchLog(log, addrFilter, topicFilter) {
				result = append(result, FormatLog(log))
			}
		}
	}

	if result == nil {
		result = []*RPCLog{}
	}
	return successResponse(req.ID, result)
}

// getBlockReceipts returns all receipts for a given block number.
func (api *EthAPI) getBlockReceipts(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing block number")
	}

	var bn BlockNumber
	if err := json.Unmarshal(req.Params[0], &bn); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}

	header := api.backend.HeaderByNumber(bn)
	if header == nil {
		return successResponse(req.ID, nil)
	}

	blockNum := header.Number.Uint64()

	// EIP-4444: check if receipts have been pruned.
	if api.historyPruned(blockNum) {
		return errorResponse(req.ID, ErrCodeHistoryPruned,
			"historical receipts pruned (EIP-4444)")
	}

	receipts := api.backend.GetBlockReceipts(blockNum)
	if receipts == nil {
		return successResponse(req.ID, []*RPCReceipt{})
	}

	result := make([]*RPCReceipt, len(receipts))
	for i, receipt := range receipts {
		result[i] = FormatReceipt(receipt, nil)
	}

	return successResponse(req.ID, result)
}

// parseCallArgs extracts EVM call parameters from CallArgs.
func parseCallArgs(args *CallArgs) (from types.Address, to *types.Address, gas uint64, value *big.Int, data []byte) {
	if args.From != nil {
		from = types.HexToAddress(*args.From)
	}
	if args.To != nil {
		addr := types.HexToAddress(*args.To)
		to = &addr
	}
	gas = 50_000_000 // default gas limit
	if args.Gas != nil {
		gas = parseHexUint64(*args.Gas)
	}
	value = new(big.Int)
	if args.Value != nil {
		value = parseHexBigInt(*args.Value)
	}
	data = args.GetData()
	return
}

// matchLog checks whether a log matches the filter criteria.
func matchLog(log *types.Log, addrFilter map[types.Address]bool, topicFilter [][]types.Hash) bool {
	// Check address filter
	if len(addrFilter) > 0 && !addrFilter[log.Address] {
		return false
	}

	// Check topic filters
	for i, topics := range topicFilter {
		if len(topics) == 0 {
			continue // wildcard position
		}
		if i >= len(log.Topics) {
			return false
		}
		matched := false
		for _, topic := range topics {
			if log.Topics[i] == topic {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	return true
}
