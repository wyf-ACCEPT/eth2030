// Package rpc provides JSON-RPC 2.0 types and the standard Ethereum
// JSON-RPC API (eth_ namespace) for the eth2028 execution client.
package rpc

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"

	"github.com/eth2028/eth2028/core/types"
)

// BlockNumber represents a block number parameter in JSON-RPC.
type BlockNumber int64

const (
	LatestBlockNumber   BlockNumber = -1
	PendingBlockNumber  BlockNumber = -2
	EarliestBlockNumber BlockNumber = 0
)

// UnmarshalJSON implements json.Unmarshaler for block number.
func (bn *BlockNumber) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		// Try as integer.
		var n int64
		if err := json.Unmarshal(data, &n); err != nil {
			return fmt.Errorf("invalid block number: %s", string(data))
		}
		*bn = BlockNumber(n)
		return nil
	}
	switch s {
	case "latest":
		*bn = LatestBlockNumber
	case "pending":
		*bn = PendingBlockNumber
	case "earliest":
		*bn = EarliestBlockNumber
	default:
		// Parse hex string.
		n, err := strconv.ParseInt(s, 0, 64)
		if err != nil {
			return fmt.Errorf("invalid block number: %s", s)
		}
		*bn = BlockNumber(n)
	}
	return nil
}

// Request is a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string            `json:"jsonrpc"`
	Method  string            `json:"method"`
	Params  []json.RawMessage `json:"params"`
	ID      json.RawMessage   `json:"id"`
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
	ID      json.RawMessage `json:"id"`
}

// RPCError is a JSON-RPC 2.0 error.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error codes.
const (
	ErrCodeParse          = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternal       = -32603
)

// RPCBlock is the JSON representation of a block.
type RPCBlock struct {
	Number       string         `json:"number"`
	Hash         string         `json:"hash"`
	ParentHash   string         `json:"parentHash"`
	Timestamp    string         `json:"timestamp"`
	GasLimit     string         `json:"gasLimit"`
	GasUsed      string         `json:"gasUsed"`
	Miner        string         `json:"miner"`
	BaseFeePerGas *string       `json:"baseFeePerGas,omitempty"`
	StateRoot    string         `json:"stateRoot"`
	TxRoot       string         `json:"transactionsRoot"`
	ReceiptsRoot string         `json:"receiptsRoot"`
	Transactions []string       `json:"transactions"` // tx hashes
}

// RPCTransaction is the JSON representation of a transaction.
type RPCTransaction struct {
	Hash             string  `json:"hash"`
	Nonce            string  `json:"nonce"`
	BlockHash        *string `json:"blockHash"`
	BlockNumber      *string `json:"blockNumber"`
	TransactionIndex *string `json:"transactionIndex"`
	From             string  `json:"from"`
	To               *string `json:"to"`
	Value            string  `json:"value"`
	Gas              string  `json:"gas"`
	GasPrice         string  `json:"gasPrice"`
	Input            string  `json:"input"`
	Type             string  `json:"type"`
	V                string  `json:"v"`
	R                string  `json:"r"`
	S                string  `json:"s"`
}

// RPCReceipt is the JSON representation of a transaction receipt.
type RPCReceipt struct {
	TransactionHash   string   `json:"transactionHash"`
	TransactionIndex  string   `json:"transactionIndex"`
	BlockHash         string   `json:"blockHash"`
	BlockNumber       string   `json:"blockNumber"`
	From              string   `json:"from"`
	To                *string  `json:"to"`
	GasUsed           string   `json:"gasUsed"`
	CumulativeGasUsed string   `json:"cumulativeGasUsed"`
	ContractAddress   *string  `json:"contractAddress"`
	Logs              []*RPCLog `json:"logs"`
	Status            string   `json:"status"`
	LogsBloom         string   `json:"logsBloom"`
}

// RPCLog is the JSON representation of a contract log event.
type RPCLog struct {
	Address          string   `json:"address"`
	Topics           []string `json:"topics"`
	Data             string   `json:"data"`
	BlockNumber      string   `json:"blockNumber"`
	TransactionHash  string   `json:"transactionHash"`
	TransactionIndex string   `json:"transactionIndex"`
	BlockHash        string   `json:"blockHash"`
	LogIndex         string   `json:"logIndex"`
	Removed          bool     `json:"removed"`
}

// CallArgs represents the arguments for eth_call and eth_estimateGas.
type CallArgs struct {
	From     *string `json:"from"`
	To       *string `json:"to"`
	Gas      *string `json:"gas"`
	GasPrice *string `json:"gasPrice"`
	Value    *string `json:"value"`
	Data     *string `json:"data"`
	Input    *string `json:"input"`
}

// GetData returns the call input data, preferring "input" over "data".
func (args *CallArgs) GetData() []byte {
	if args.Input != nil {
		return fromHexBytes(*args.Input)
	}
	if args.Data != nil {
		return fromHexBytes(*args.Data)
	}
	return nil
}

// FilterCriteria contains parameters for log filtering.
type FilterCriteria struct {
	FromBlock *BlockNumber `json:"fromBlock"`
	ToBlock   *BlockNumber `json:"toBlock"`
	Addresses []string     `json:"address"`
	Topics    [][]string   `json:"topics"`
}

// RPCBlockWithTxs is the JSON representation of a block with full transaction objects.
type RPCBlockWithTxs struct {
	Number        string             `json:"number"`
	Hash          string             `json:"hash"`
	ParentHash    string             `json:"parentHash"`
	Timestamp     string             `json:"timestamp"`
	GasLimit      string             `json:"gasLimit"`
	GasUsed       string             `json:"gasUsed"`
	Miner         string             `json:"miner"`
	BaseFeePerGas *string            `json:"baseFeePerGas,omitempty"`
	StateRoot     string             `json:"stateRoot"`
	TxRoot        string             `json:"transactionsRoot"`
	ReceiptsRoot  string             `json:"receiptsRoot"`
	Transactions  []*RPCTransaction  `json:"transactions"`
}

// FormatBlock converts a block to its JSON-RPC representation.
// If fullTx is true, returns full transaction objects; otherwise returns tx hashes.
func FormatBlock(block *types.Block, fullTx bool) interface{} {
	header := block.Header()
	if !fullTx {
		return FormatHeader(header)
	}

	result := &RPCBlockWithTxs{
		Number:       encodeUint64(header.Number.Uint64()),
		Hash:         encodeHash(header.Hash()),
		ParentHash:   encodeHash(header.ParentHash),
		Timestamp:    encodeUint64(header.Time),
		GasLimit:     encodeUint64(header.GasLimit),
		GasUsed:      encodeUint64(header.GasUsed),
		Miner:        encodeAddress(header.Coinbase),
		StateRoot:    encodeHash(header.Root),
		TxRoot:       encodeHash(header.TxHash),
		ReceiptsRoot: encodeHash(header.ReceiptHash),
	}
	if header.BaseFee != nil {
		s := encodeBigInt(header.BaseFee)
		result.BaseFeePerGas = &s
	}

	txs := block.Transactions()
	result.Transactions = make([]*RPCTransaction, len(txs))
	blockHash := block.Hash()
	blockNum := block.NumberU64()
	for i, tx := range txs {
		idx := uint64(i)
		result.Transactions[i] = FormatTransaction(tx, &blockHash, &blockNum, &idx)
	}

	return result
}

// FormatHeader converts a header to JSON-RPC representation.
func FormatHeader(h *types.Header) *RPCBlock {
	block := &RPCBlock{
		Number:       encodeUint64(h.Number.Uint64()),
		Hash:         encodeHash(h.Hash()),
		ParentHash:   encodeHash(h.ParentHash),
		Timestamp:    encodeUint64(h.Time),
		GasLimit:     encodeUint64(h.GasLimit),
		GasUsed:      encodeUint64(h.GasUsed),
		Miner:        encodeAddress(h.Coinbase),
		StateRoot:    encodeHash(h.Root),
		TxRoot:       encodeHash(h.TxHash),
		ReceiptsRoot: encodeHash(h.ReceiptHash),
	}
	if h.BaseFee != nil {
		s := encodeBigInt(h.BaseFee)
		block.BaseFeePerGas = &s
	}
	return block
}

func encodeUint64(n uint64) string {
	return "0x" + strconv.FormatUint(n, 16)
}

func encodeBigInt(n *big.Int) string {
	if n == nil {
		return "0x0"
	}
	return "0x" + n.Text(16)
}

func encodeHash(h types.Hash) string {
	return "0x" + fmt.Sprintf("%064x", h[:])
}

func encodeAddress(a types.Address) string {
	return "0x" + fmt.Sprintf("%040x", a[:])
}

func encodeBytes(b []byte) string {
	if len(b) == 0 {
		return "0x"
	}
	return "0x" + fmt.Sprintf("%x", b)
}

func encodeBloom(b types.Bloom) string {
	return fmt.Sprintf("0x%0512x", b[:])
}

// fromHexBytes decodes a hex string (with optional 0x prefix) into bytes.
func fromHexBytes(s string) []byte {
	if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		s = s[2:]
	}
	if len(s) == 0 {
		return nil
	}
	if len(s)%2 == 1 {
		s = "0" + s
	}
	b := make([]byte, len(s)/2)
	for i := 0; i < len(b); i++ {
		b[i] = unhex(s[2*i])<<4 | unhex(s[2*i+1])
	}
	return b
}

func unhex(c byte) byte {
	switch {
	case '0' <= c && c <= '9':
		return c - '0'
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10
	}
	return 0
}

func parseHexUint64(s string) uint64 {
	if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		s = s[2:]
	}
	n, _ := strconv.ParseUint(s, 16, 64)
	return n
}

func parseHexBigInt(s string) *big.Int {
	if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		s = s[2:]
	}
	n := new(big.Int)
	n.SetString(s, 16)
	return n
}

// FormatTransaction converts a transaction to its JSON-RPC representation.
func FormatTransaction(tx *types.Transaction, blockHash *types.Hash, blockNumber *uint64, index *uint64) *RPCTransaction {
	rpcTx := &RPCTransaction{
		Hash:     encodeHash(tx.Hash()),
		Nonce:    encodeUint64(tx.Nonce()),
		Value:    encodeBigInt(tx.Value()),
		Gas:      encodeUint64(tx.Gas()),
		GasPrice: encodeBigInt(tx.GasPrice()),
		Input:    encodeBytes(tx.Data()),
		Type:     encodeUint64(uint64(tx.Type())),
	}

	if sender := tx.Sender(); sender != nil {
		rpcTx.From = encodeAddress(*sender)
	}

	if tx.To() != nil {
		to := encodeAddress(*tx.To())
		rpcTx.To = &to
	}

	if blockHash != nil {
		bh := encodeHash(*blockHash)
		rpcTx.BlockHash = &bh
	}
	if blockNumber != nil {
		bn := encodeUint64(*blockNumber)
		rpcTx.BlockNumber = &bn
	}
	if index != nil {
		idx := encodeUint64(*index)
		rpcTx.TransactionIndex = &idx
	}

	// V, R, S - use "0x0" as default if not available
	rpcTx.V = "0x0"
	rpcTx.R = "0x0"
	rpcTx.S = "0x0"

	return rpcTx
}

// FormatReceipt converts a receipt to its JSON-RPC representation.
func FormatReceipt(receipt *types.Receipt, tx *types.Transaction) *RPCReceipt {
	rpcReceipt := &RPCReceipt{
		TransactionHash:   encodeHash(receipt.TxHash),
		TransactionIndex:  encodeUint64(uint64(receipt.TransactionIndex)),
		BlockHash:         encodeHash(receipt.BlockHash),
		BlockNumber:       encodeBigInt(receipt.BlockNumber),
		GasUsed:           encodeUint64(receipt.GasUsed),
		CumulativeGasUsed: encodeUint64(receipt.CumulativeGasUsed),
		Status:            encodeUint64(receipt.Status),
		LogsBloom:         encodeBloom(receipt.Bloom),
	}

	// From
	if tx != nil {
		if sender := tx.Sender(); sender != nil {
			rpcReceipt.From = encodeAddress(*sender)
		}
		if tx.To() != nil {
			to := encodeAddress(*tx.To())
			rpcReceipt.To = &to
		}
	}

	// Contract address (only if contract creation)
	if !receipt.ContractAddress.IsZero() {
		ca := encodeAddress(receipt.ContractAddress)
		rpcReceipt.ContractAddress = &ca
	}

	// Logs
	rpcReceipt.Logs = make([]*RPCLog, len(receipt.Logs))
	for i, log := range receipt.Logs {
		rpcReceipt.Logs[i] = FormatLog(log)
	}
	if rpcReceipt.Logs == nil {
		rpcReceipt.Logs = []*RPCLog{}
	}

	return rpcReceipt
}

// FormatLog converts a log to its JSON-RPC representation.
func FormatLog(log *types.Log) *RPCLog {
	topics := make([]string, len(log.Topics))
	for i, topic := range log.Topics {
		topics[i] = encodeHash(topic)
	}
	return &RPCLog{
		Address:          encodeAddress(log.Address),
		Topics:           topics,
		Data:             encodeBytes(log.Data),
		BlockNumber:      encodeUint64(log.BlockNumber),
		TransactionHash:  encodeHash(log.TxHash),
		TransactionIndex: encodeUint64(uint64(log.TxIndex)),
		BlockHash:        encodeHash(log.BlockHash),
		LogIndex:         encodeUint64(uint64(log.Index)),
		Removed:          log.Removed,
	}
}
