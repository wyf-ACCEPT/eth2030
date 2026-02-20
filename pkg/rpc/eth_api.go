// eth_api.go provides a typed, direct-call API surface for core eth_
// namespace JSON-RPC methods. Unlike the JSON-RPC dispatch layer in api.go,
// these methods accept and return Go types directly, making them suitable
// for internal use, testing, and programmatic access.
package rpc

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
)

// EthDirectAPI exposes core eth_ RPC methods with direct Go types instead
// of JSON-RPC request/response wrappers. It delegates to a Backend for
// all chain, state, and transaction pool access.
type EthDirectAPI struct {
	backend Backend
	chainID *big.Int
}

// Errors returned by EthDirectAPI methods.
var (
	ErrNoCurrentBlock   = errors.New("no current block")
	ErrBlockNotFound    = errors.New("block not found")
	ErrStateUnavailable = errors.New("state unavailable")
	ErrTxNotFound       = errors.New("transaction not found")
	ErrReceiptNotFound  = errors.New("receipt not found")
	ErrEmptyTxData      = errors.New("empty transaction data")
	ErrExecutionFailed  = errors.New("execution failed")
)

// NewEthDirectAPI creates a new direct API backed by the given Backend.
func NewEthDirectAPI(backend Backend) *EthDirectAPI {
	return &EthDirectAPI{
		backend: backend,
		chainID: backend.ChainID(),
	}
}

// ChainID returns the chain ID as a hex string.
func (api *EthDirectAPI) ChainID() string {
	return encodeBigInt(api.chainID)
}

// BlockNumber returns the latest block number as a hex string.
func (api *EthDirectAPI) BlockNumber() (string, error) {
	header := api.backend.CurrentHeader()
	if header == nil {
		return "", ErrNoCurrentBlock
	}
	return encodeUint64(header.Number.Uint64()), nil
}

// GetBalance returns the balance of the given address at the given block
// as a hex string.
func (api *EthDirectAPI) GetBalance(address string, block string) (string, error) {
	bn := parseBlockNumber(block)
	header := api.backend.HeaderByNumber(bn)
	if header == nil {
		return "", ErrBlockNotFound
	}

	statedb, err := api.backend.StateAt(header.Root)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrStateUnavailable, err)
	}

	addr := types.HexToAddress(address)
	balance := statedb.GetBalance(addr)
	return encodeBigInt(balance), nil
}

// GetTransactionCount returns the nonce of the given address at the given
// block as a hex string.
func (api *EthDirectAPI) GetTransactionCount(address string, block string) (string, error) {
	bn := parseBlockNumber(block)
	header := api.backend.HeaderByNumber(bn)
	if header == nil {
		return "", ErrBlockNotFound
	}

	statedb, err := api.backend.StateAt(header.Root)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrStateUnavailable, err)
	}

	addr := types.HexToAddress(address)
	nonce := statedb.GetNonce(addr)
	return encodeUint64(nonce), nil
}

// GetBlockByNumber returns a block by number. If fullTxs is true, the
// "transactions" field contains full transaction objects; otherwise it
// contains only transaction hashes.
func (api *EthDirectAPI) GetBlockByNumber(number string, fullTxs bool) (map[string]interface{}, error) {
	bn := parseBlockNumber(number)

	if fullTxs {
		block := api.backend.BlockByNumber(bn)
		if block == nil {
			return nil, nil
		}
		return formatBlockAsMap(block, true), nil
	}

	header := api.backend.HeaderByNumber(bn)
	if header == nil {
		return nil, nil
	}
	return formatHeaderMap(header), nil
}

// GetBlockByHash returns a block by hash. If fullTxs is true, the
// "transactions" field contains full transaction objects.
func (api *EthDirectAPI) GetBlockByHash(hash string, fullTxs bool) (map[string]interface{}, error) {
	h := types.HexToHash(hash)

	if fullTxs {
		block := api.backend.BlockByHash(h)
		if block == nil {
			return nil, nil
		}
		return formatBlockAsMap(block, true), nil
	}

	header := api.backend.HeaderByHash(h)
	if header == nil {
		return nil, nil
	}
	return formatHeaderMap(header), nil
}

// GetTransactionByHash returns transaction details by hash. Returns nil
// if the transaction is not found.
func (api *EthDirectAPI) GetTransactionByHash(hash string) (map[string]interface{}, error) {
	txHash := types.HexToHash(hash)
	tx, blockNum, index := api.backend.GetTransaction(txHash)
	if tx == nil {
		return nil, nil
	}

	result := formatTxAsMap(tx)
	if blockNum > 0 {
		header := api.backend.HeaderByNumber(BlockNumber(blockNum))
		if header != nil {
			bh := header.Hash()
			result["blockHash"] = encodeHash(bh)
		}
		result["blockNumber"] = encodeUint64(blockNum)
	}
	result["transactionIndex"] = encodeUint64(index)
	return result, nil
}

// GetTransactionReceipt returns the receipt for a transaction by hash.
// Returns nil if the transaction or receipt is not found.
func (api *EthDirectAPI) GetTransactionReceipt(hash string) (map[string]interface{}, error) {
	txHash := types.HexToHash(hash)
	tx, blockNum, _ := api.backend.GetTransaction(txHash)
	if tx == nil {
		return nil, nil
	}

	header := api.backend.HeaderByNumber(BlockNumber(blockNum))
	if header == nil {
		return nil, nil
	}

	blockHash := header.Hash()
	receipts := api.backend.GetReceipts(blockHash)

	for _, receipt := range receipts {
		if receipt.TxHash == txHash {
			return formatReceiptAsMap(receipt, tx), nil
		}
	}

	return nil, nil
}

// EstimateGas estimates the gas needed to execute a transaction using
// binary search between a floor and the block gas limit.
func (api *EthDirectAPI) EstimateGas(args map[string]interface{}) (string, error) {
	from, to, gas, value, data := extractCallArgs(args)

	header := api.backend.CurrentHeader()
	if header == nil {
		return "", ErrNoCurrentBlock
	}

	hi := header.GasLimit
	if gas > 0 && gas < hi {
		hi = gas
	}
	lo := uint64(21000) // intrinsic gas floor

	// Check the upper bound works.
	_, _, err := api.backend.EVMCall(from, to, data, hi, value, LatestBlockNumber)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrExecutionFailed, err)
	}

	// Check if the floor itself is sufficient.
	_, _, err = api.backend.EVMCall(from, to, data, lo, value, LatestBlockNumber)
	if err == nil {
		return encodeUint64(lo), nil
	}

	// Binary search for the minimum sufficient gas.
	for lo+1 < hi {
		mid := (lo + hi) / 2
		_, _, err := api.backend.EVMCall(from, to, data, mid, value, LatestBlockNumber)
		if err != nil {
			lo = mid
		} else {
			hi = mid
		}
	}

	return encodeUint64(hi), nil
}

// GasPrice returns the suggested gas price as a hex string.
func (api *EthDirectAPI) GasPrice() (string, error) {
	price := api.backend.SuggestGasPrice()
	if price == nil {
		price = new(big.Int)
	}
	return encodeBigInt(price), nil
}

// SendRawTransaction decodes raw transaction bytes and submits them to the
// transaction pool. Returns the transaction hash.
func (api *EthDirectAPI) SendRawTransaction(data string) (string, error) {
	rawBytes := fromHexBytes(data)
	if len(rawBytes) == 0 {
		return "", ErrEmptyTxData
	}

	// Wrap raw bytes as a legacy transaction. A full implementation would
	// RLP-decode the transaction to determine its type.
	tx := types.NewTransaction(&types.LegacyTx{
		Data: rawBytes,
	})

	if err := api.backend.SendTransaction(tx); err != nil {
		return "", err
	}

	return encodeHash(tx.Hash()), nil
}

// Call executes a read-only call against the EVM without creating a
// transaction. Returns the return data as a hex string.
func (api *EthDirectAPI) Call(args map[string]interface{}, block string) (string, error) {
	from, to, gas, value, data := extractCallArgs(args)
	if gas == 0 {
		gas = 50_000_000 // default gas limit
	}

	bn := parseBlockNumber(block)
	result, _, err := api.backend.EVMCall(from, to, data, gas, value, bn)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrExecutionFailed, err)
	}

	return encodeBytes(result), nil
}

// GetCode returns the code at the given address at the given block.
func (api *EthDirectAPI) GetCode(address, block string) (string, error) {
	bn := parseBlockNumber(block)
	header := api.backend.HeaderByNumber(bn)
	if header == nil {
		return "", ErrBlockNotFound
	}

	statedb, err := api.backend.StateAt(header.Root)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrStateUnavailable, err)
	}

	addr := types.HexToAddress(address)
	code := statedb.GetCode(addr)
	return encodeBytes(code), nil
}

// GetStorageAt returns the storage value at the given address and key
// at the given block.
func (api *EthDirectAPI) GetStorageAt(address, key, block string) (string, error) {
	bn := parseBlockNumber(block)
	header := api.backend.HeaderByNumber(bn)
	if header == nil {
		return "", ErrBlockNotFound
	}

	statedb, err := api.backend.StateAt(header.Root)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrStateUnavailable, err)
	}

	addr := types.HexToAddress(address)
	slot := types.HexToHash(key)
	value := statedb.GetState(addr, slot)
	return encodeHash(value), nil
}

// parseBlockNumber converts a block number string (hex or tag) to BlockNumber.
func parseBlockNumber(s string) BlockNumber {
	switch s {
	case "latest", "":
		return LatestBlockNumber
	case "pending":
		return PendingBlockNumber
	case "earliest":
		return EarliestBlockNumber
	case "safe":
		return SafeBlockNumber
	case "finalized":
		return FinalizedBlockNumber
	default:
		n := parseHexUint64(s)
		return BlockNumber(n)
	}
}

// extractCallArgs parses call arguments from a map representation.
func extractCallArgs(args map[string]interface{}) (from types.Address, to *types.Address, gas uint64, value *big.Int, data []byte) {
	value = new(big.Int)

	if v, ok := args["from"]; ok {
		if s, ok := v.(string); ok {
			from = types.HexToAddress(s)
		}
	}
	if v, ok := args["to"]; ok {
		if s, ok := v.(string); ok {
			addr := types.HexToAddress(s)
			to = &addr
		}
	}
	if v, ok := args["gas"]; ok {
		if s, ok := v.(string); ok {
			gas = parseHexUint64(s)
		}
	}
	if v, ok := args["value"]; ok {
		if s, ok := v.(string); ok {
			value = parseHexBigInt(s)
		}
	}
	if v, ok := args["data"]; ok {
		if s, ok := v.(string); ok {
			data = fromHexBytes(s)
		}
	}
	if v, ok := args["input"]; ok {
		if s, ok := v.(string); ok && len(data) == 0 {
			data = fromHexBytes(s)
		}
	}
	return
}

// formatBlockAsMap converts a block to a map[string]interface{}.
func formatBlockAsMap(block *types.Block, fullTxs bool) map[string]interface{} {
	header := block.Header()
	m := formatHeaderMap(header)

	if fullTxs {
		txs := block.Transactions()
		txList := make([]map[string]interface{}, len(txs))
		blockHash := block.Hash()
		blockNum := block.NumberU64()
		for i, tx := range txs {
			txm := formatTxAsMap(tx)
			txm["blockHash"] = encodeHash(blockHash)
			txm["blockNumber"] = encodeUint64(blockNum)
			txm["transactionIndex"] = encodeUint64(uint64(i))
			txList[i] = txm
		}
		m["transactions"] = txList
	} else {
		txs := block.Transactions()
		hashes := make([]string, len(txs))
		for i, tx := range txs {
			hashes[i] = encodeHash(tx.Hash())
		}
		m["transactions"] = hashes
	}

	return m
}

// formatHeaderMap converts a header to a map[string]interface{}.
func formatHeaderMap(h *types.Header) map[string]interface{} {
	m := map[string]interface{}{
		"number":           encodeUint64(h.Number.Uint64()),
		"hash":             encodeHash(h.Hash()),
		"parentHash":       encodeHash(h.ParentHash),
		"timestamp":        encodeUint64(h.Time),
		"gasLimit":         encodeUint64(h.GasLimit),
		"gasUsed":          encodeUint64(h.GasUsed),
		"miner":            encodeAddress(h.Coinbase),
		"stateRoot":        encodeHash(h.Root),
		"transactionsRoot": encodeHash(h.TxHash),
		"receiptsRoot":     encodeHash(h.ReceiptHash),
	}
	if h.BaseFee != nil {
		m["baseFeePerGas"] = encodeBigInt(h.BaseFee)
	}
	return m
}

// formatTxAsMap converts a transaction to a map[string]interface{}.
func formatTxAsMap(tx *types.Transaction) map[string]interface{} {
	m := map[string]interface{}{
		"hash":     encodeHash(tx.Hash()),
		"nonce":    encodeUint64(tx.Nonce()),
		"value":    encodeBigInt(tx.Value()),
		"gas":      encodeUint64(tx.Gas()),
		"gasPrice": encodeBigInt(tx.GasPrice()),
		"input":    encodeBytes(tx.Data()),
		"type":     encodeUint64(uint64(tx.Type())),
		"v":        "0x0",
		"r":        "0x0",
		"s":        "0x0",
	}

	if sender := tx.Sender(); sender != nil {
		m["from"] = encodeAddress(*sender)
	} else {
		m["from"] = encodeAddress(types.Address{})
	}

	if tx.To() != nil {
		m["to"] = encodeAddress(*tx.To())
	} else {
		m["to"] = nil
	}

	// Fill in dynamic fee fields if applicable.
	if tx.Type() == types.DynamicFeeTxType || tx.Type() == types.BlobTxType || tx.Type() == types.SetCodeTxType {
		if tx.GasTipCap() != nil {
			m["maxPriorityFeePerGas"] = encodeBigInt(tx.GasTipCap())
		}
		if tx.GasFeeCap() != nil {
			m["maxFeePerGas"] = encodeBigInt(tx.GasFeeCap())
		}
	}

	return m
}

// formatReceiptAsMap converts a receipt and its transaction to a map.
func formatReceiptAsMap(receipt *types.Receipt, tx *types.Transaction) map[string]interface{} {
	m := map[string]interface{}{
		"transactionHash":   encodeHash(receipt.TxHash),
		"transactionIndex":  encodeUint64(uint64(receipt.TransactionIndex)),
		"blockHash":         encodeHash(receipt.BlockHash),
		"blockNumber":       encodeBigInt(receipt.BlockNumber),
		"gasUsed":           encodeUint64(receipt.GasUsed),
		"cumulativeGasUsed": encodeUint64(receipt.CumulativeGasUsed),
		"status":            encodeUint64(receipt.Status),
		"type":              encodeUint64(uint64(receipt.Type)),
		"logsBloom":         encodeBloom(receipt.Bloom),
	}

	if receipt.EffectiveGasPrice != nil {
		m["effectiveGasPrice"] = encodeBigInt(receipt.EffectiveGasPrice)
	} else {
		m["effectiveGasPrice"] = "0x0"
	}

	if tx != nil {
		if sender := tx.Sender(); sender != nil {
			m["from"] = encodeAddress(*sender)
		}
		if tx.To() != nil {
			m["to"] = encodeAddress(*tx.To())
		} else {
			m["to"] = nil
		}
	}

	if !receipt.ContractAddress.IsZero() {
		m["contractAddress"] = encodeAddress(receipt.ContractAddress)
	} else {
		m["contractAddress"] = nil
	}

	logs := make([]map[string]interface{}, len(receipt.Logs))
	for i, log := range receipt.Logs {
		logs[i] = formatLogAsMap(log)
	}
	if logs == nil {
		logs = []map[string]interface{}{}
	}
	m["logs"] = logs

	return m
}

// formatLogAsMap converts a log to a map.
func formatLogAsMap(log *types.Log) map[string]interface{} {
	topics := make([]string, len(log.Topics))
	for i, topic := range log.Topics {
		topics[i] = encodeHash(topic)
	}
	return map[string]interface{}{
		"address":          encodeAddress(log.Address),
		"topics":           topics,
		"data":             encodeBytes(log.Data),
		"blockNumber":      encodeUint64(log.BlockNumber),
		"transactionHash":  encodeHash(log.TxHash),
		"transactionIndex": encodeUint64(uint64(log.TxIndex)),
		"blockHash":        encodeHash(log.BlockHash),
		"logIndex":         encodeUint64(uint64(log.Index)),
		"removed":          log.Removed,
	}
}
