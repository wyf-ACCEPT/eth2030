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
	Hash     string `json:"hash"`
	Nonce    string `json:"nonce"`
	From     string `json:"from"`
	To       string `json:"to"`
	Value    string `json:"value"`
	Gas      string `json:"gas"`
	GasPrice string `json:"gasPrice"`
	Input    string `json:"input"`
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
	return "0x" + fmt.Sprintf("%064x", h)
}

func encodeAddress(a types.Address) string {
	return "0x" + fmt.Sprintf("%040x", a)
}

func encodeBytes(b []byte) string {
	return "0x" + fmt.Sprintf("%x", b)
}
