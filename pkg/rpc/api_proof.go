package rpc

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/rlp"
)

// AccountProof is the response for eth_getProof.
type AccountProof struct {
	Address      string         `json:"address"`
	AccountProof []string       `json:"accountProof"`
	Balance      string         `json:"balance"`
	CodeHash     string         `json:"codeHash"`
	Nonce        string         `json:"nonce"`
	StorageHash  string         `json:"storageHash"`
	StorageProof []StorageProof `json:"storageProof"`
}

// StorageProof is a single storage slot proof within eth_getProof.
type StorageProof struct {
	Key   string   `json:"key"`
	Value string   `json:"value"`
	Proof []string `json:"proof"`
}

// getProof implements eth_getProof (EIP-1186).
// Returns the account and storage values along with Merkle proofs.
func (api *EthAPI) getProof(req *Request) *Response {
	if len(req.Params) < 3 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing address, storageKeys, or block number")
	}

	var addrHex string
	if err := json.Unmarshal(req.Params[0], &addrHex); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, "invalid address: "+err.Error())
	}

	var storageKeysHex []string
	if err := json.Unmarshal(req.Params[1], &storageKeysHex); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, "invalid storageKeys: "+err.Error())
	}

	var bn BlockNumber
	if err := json.Unmarshal(req.Params[2], &bn); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, "invalid block number: "+err.Error())
	}

	addr := types.HexToAddress(addrHex)

	// Convert storage key hex strings to types.Hash.
	storageKeys := make([]types.Hash, len(storageKeysHex))
	for i, keyHex := range storageKeysHex {
		storageKeys[i] = types.HexToHash(keyHex)
	}

	// Generate real MPT proofs via the backend.
	proof, err := api.backend.GetProof(addr, storageKeys, bn)
	if err != nil {
		return errorResponse(req.ID, ErrCodeInternal, err.Error())
	}

	// Convert trie.StorageProof to rpc.StorageProof with hex encoding.
	rpcStorageProofs := make([]StorageProof, len(proof.StorageProof))
	for i, sp := range proof.StorageProof {
		rpcStorageProofs[i] = StorageProof{
			Key:   storageKeysHex[i],
			Value: encodeBigInt(sp.Value),
			Proof: encodeProofNodes(sp.Proof),
		}
	}

	result := &AccountProof{
		Address:      encodeAddress(proof.Address),
		AccountProof: encodeProofNodes(proof.AccountProof),
		Balance:      encodeBigInt(proof.Balance),
		CodeHash:     encodeHash(proof.CodeHash),
		Nonce:        encodeUint64(proof.Nonce),
		StorageHash:  encodeHash(proof.StorageHash),
		StorageProof: rpcStorageProofs,
	}

	return successResponse(req.ID, result)
}

// rlpAccount is the RLP-serializable account struct matching the Yellow Paper
// definition: [nonce, balance, storageRoot, codeHash].
type rlpAccountForProof struct {
	Nonce    uint64
	Balance  *big.Int
	Root     []byte
	CodeHash []byte
}

// encodeAccountRLP encodes an account as RLP per the Yellow Paper:
// RLP([nonce, balance, storageRoot, codeHash]).
func encodeAccountRLP(nonce uint64, balance *big.Int, storageRoot, codeHash types.Hash) []byte {
	acc := rlpAccountForProof{
		Nonce:    nonce,
		Balance:  balance,
		Root:     storageRoot[:],
		CodeHash: codeHash[:],
	}
	encoded, err := rlp.EncodeToBytes(acc)
	if err != nil {
		return nil
	}
	return encoded
}

// encodeProofNodes converts raw proof node bytes to 0x-prefixed hex strings.
func encodeProofNodes(nodes [][]byte) []string {
	result := make([]string, len(nodes))
	for i, node := range nodes {
		result[i] = "0x" + hex.EncodeToString(node)
	}
	return result
}

// StructLog is a single step in an EVM execution trace.
type StructLog struct {
	PC      uint64            `json:"pc"`
	Op      string            `json:"op"`
	Gas     uint64            `json:"gas"`
	GasCost uint64            `json:"gasCost"`
	Depth   int               `json:"depth"`
	Stack   []string          `json:"stack"`
	Memory  []string          `json:"memory,omitempty"`
	Storage map[string]string `json:"storage,omitempty"`
}

// TraceResult is the response for debug_traceTransaction.
type TraceResult struct {
	Gas         uint64      `json:"gas"`
	Failed      bool        `json:"failed"`
	ReturnValue string      `json:"returnValue"`
	StructLogs  []StructLog `json:"structLogs"`
}

// debugTraceTransaction implements debug_traceTransaction.
// Re-executes the transaction with a tracing EVM and returns a detailed
// step-by-step execution trace including opcode, gas, stack per step.
func (api *EthAPI) debugTraceTransaction(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing transaction hash")
	}

	var txHashHex string
	if err := json.Unmarshal(req.Params[0], &txHashHex); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, "invalid tx hash: "+err.Error())
	}

	txHash := types.HexToHash(txHashHex)

	// Delegate tracing to the backend, which has access to the blockchain
	// and state processor needed to re-execute the transaction.
	tracer, err := api.backend.TraceTransaction(txHash)
	if err != nil {
		return errorResponse(req.ID, ErrCodeInternal, err.Error())
	}

	// Convert StructLogTracer entries to the RPC response format.
	structLogs := make([]StructLog, len(tracer.Logs))
	for i, entry := range tracer.Logs {
		stackHex := make([]string, len(entry.Stack))
		for j, val := range entry.Stack {
			stackHex[j] = "0x" + val.Text(16)
		}
		structLogs[i] = StructLog{
			PC:      entry.Pc,
			Op:      entry.Op.String(),
			Gas:     entry.Gas,
			GasCost: entry.GasCost,
			Depth:   entry.Depth,
			Stack:   stackHex,
		}
	}

	failed := tracer.Error() != nil
	retVal := ""
	if out := tracer.Output(); len(out) > 0 {
		retVal = encodeBytes(out)
	}

	result := &TraceResult{
		Gas:         tracer.GasUsed(),
		Failed:      failed,
		ReturnValue: retVal,
		StructLogs:  structLogs,
	}

	return successResponse(req.ID, result)
}

// getAccountRange implements debug_getAccountRange (for snap sync debugging).
func (api *EthAPI) getAccountRange(req *Request) *Response {
	// This method is used for debugging snap sync and is not critical.
	return errorResponse(req.ID, ErrCodeMethodNotFound, "debug_getAccountRange not yet implemented")
}

// getHeaderByNumber implements eth_getHeaderByNumber.
func (api *EthAPI) getHeaderByNumber(req *Request) *Response {
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
	return successResponse(req.ID, FormatHeader(header))
}

// getHeaderByHash implements eth_getHeaderByHash.
func (api *EthAPI) getHeaderByHash(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing block hash")
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

// getTransactionByBlockHashAndIndex implements eth_getTransactionByBlockHashAndIndex.
func (api *EthAPI) getTransactionByBlockHashAndIndex(req *Request) *Response {
	if len(req.Params) < 2 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing block hash or index")
	}

	var hashHex, indexHex string
	if err := json.Unmarshal(req.Params[0], &hashHex); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}
	if err := json.Unmarshal(req.Params[1], &indexHex); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}

	blockHash := types.HexToHash(hashHex)
	index := parseHexUint64(indexHex)

	block := api.backend.BlockByHash(blockHash)
	if block == nil {
		return successResponse(req.ID, nil)
	}

	txs := block.Transactions()
	if index >= uint64(len(txs)) {
		return successResponse(req.ID, nil)
	}

	blockNum := block.NumberU64()
	bh := block.Hash()
	return successResponse(req.ID, FormatTransaction(txs[index], &bh, &blockNum, &index))
}

// getTransactionByBlockNumberAndIndex implements eth_getTransactionByBlockNumberAndIndex.
func (api *EthAPI) getTransactionByBlockNumberAndIndex(req *Request) *Response {
	if len(req.Params) < 2 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing block number or index")
	}

	var bn BlockNumber
	if err := json.Unmarshal(req.Params[0], &bn); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}

	var indexHex string
	if err := json.Unmarshal(req.Params[1], &indexHex); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}
	index := parseHexUint64(indexHex)

	block := api.backend.BlockByNumber(bn)
	if block == nil {
		return successResponse(req.ID, nil)
	}

	txs := block.Transactions()
	if index >= uint64(len(txs)) {
		return successResponse(req.ID, nil)
	}

	blockNum := block.NumberU64()
	bh := block.Hash()
	return successResponse(req.ID, FormatTransaction(txs[index], &bh, &blockNum, &index))
}

// getBlockTransactionCountByHash implements eth_getBlockTransactionCountByHash.
func (api *EthAPI) getBlockTransactionCountByHash(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing block hash")
	}

	var hashHex string
	if err := json.Unmarshal(req.Params[0], &hashHex); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}

	block := api.backend.BlockByHash(types.HexToHash(hashHex))
	if block == nil {
		return successResponse(req.ID, nil)
	}

	return successResponse(req.ID, encodeUint64(uint64(len(block.Transactions()))))
}

// getBlockTransactionCountByNumber implements eth_getBlockTransactionCountByNumber.
func (api *EthAPI) getBlockTransactionCountByNumber(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing block number")
	}

	var bn BlockNumber
	if err := json.Unmarshal(req.Params[0], &bn); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}

	block := api.backend.BlockByNumber(bn)
	if block == nil {
		return successResponse(req.ID, nil)
	}

	return successResponse(req.ID, encodeUint64(uint64(len(block.Transactions()))))
}

// accounts implements eth_accounts (returns empty list for non-wallet nodes).
func (api *EthAPI) accounts(req *Request) *Response {
	return successResponse(req.ID, []string{})
}

// coinbase implements eth_coinbase.
func (api *EthAPI) coinbase(req *Request) *Response {
	header := api.backend.CurrentHeader()
	if header == nil {
		return errorResponse(req.ID, ErrCodeInternal, "no current block")
	}
	return successResponse(req.ID, encodeAddress(header.Coinbase))
}

// mining implements eth_mining (always false for PoS).
func (api *EthAPI) mining(req *Request) *Response {
	return successResponse(req.ID, false)
}

// hashrate implements eth_hashrate (always 0 for PoS).
func (api *EthAPI) hashrate(req *Request) *Response {
	return successResponse(req.ID, "0x0")
}

// protocolVersion implements eth_protocolVersion.
func (api *EthAPI) protocolVersion(req *Request) *Response {
	return successResponse(req.ID, fmt.Sprintf("0x%x", 68)) // ETH/68
}

// getUncleCountByBlockHash implements eth_getUncleCountByBlockHash.
// Post-merge: always 0.
func (api *EthAPI) getUncleCountByBlockHash(req *Request) *Response {
	return successResponse(req.ID, "0x0")
}

// getUncleCountByBlockNumber implements eth_getUncleCountByBlockNumber.
// Post-merge: always 0.
func (api *EthAPI) getUncleCountByBlockNumber(req *Request) *Response {
	return successResponse(req.ID, "0x0")
}

// getUncleByBlockHashAndIndex implements eth_getUncleByBlockHashAndIndex.
// Post-merge: always returns null (no uncles in PoS).
func (api *EthAPI) getUncleByBlockHashAndIndex(req *Request) *Response {
	return successResponse(req.ID, nil)
}

// getUncleByBlockNumberAndIndex implements eth_getUncleByBlockNumberAndIndex.
// Post-merge: always returns null (no uncles in PoS).
func (api *EthAPI) getUncleByBlockNumberAndIndex(req *Request) *Response {
	return successResponse(req.ID, nil)
}

// getBlobBaseFee implements eth_blobBaseFee (EIP-7516).
func (api *EthAPI) getBlobBaseFee(req *Request) *Response {
	header := api.backend.CurrentHeader()
	if header == nil {
		return errorResponse(req.ID, ErrCodeInternal, "no current block")
	}
	if header.ExcessBlobGas != nil {
		return successResponse(req.ID, encodeBigInt(new(big.Int).SetUint64(*header.ExcessBlobGas)))
	}
	return successResponse(req.ID, "0x0")
}
