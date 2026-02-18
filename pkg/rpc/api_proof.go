package rpc

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
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

	var storageKeys []string
	if err := json.Unmarshal(req.Params[1], &storageKeys); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, "invalid storageKeys: "+err.Error())
	}

	var bn BlockNumber
	if err := json.Unmarshal(req.Params[2], &bn); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, "invalid block number: "+err.Error())
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

	// Get account data.
	balance := statedb.GetBalance(addr)
	nonce := statedb.GetNonce(addr)
	codeHash := statedb.GetCodeHash(addr)

	// If account doesn't exist, return empty code hash.
	if codeHash == (types.Hash{}) {
		codeHash = types.EmptyCodeHash
	}

	// Compute storage hash from the account's storage trie root.
	// For a full implementation, this requires the actual storage trie root.
	// Use EmptyRootHash if the account has no storage.
	storageHash := types.EmptyRootHash

	// Build account proof (Merkle proof for the account in the state trie).
	// The key in the state trie is keccak256(address).
	accountKey := crypto.Keccak256(addr[:])
	_ = accountKey

	// Build the account RLP for the proof.
	// Account = RLP(nonce, balance, storageRoot, codeHash)
	acctRLP := encodeAccountRLP(nonce, balance, storageHash, codeHash)
	_ = acctRLP

	// For now, return an empty proof path. A full implementation would
	// walk the MPT and collect the proof nodes.
	accountProofHex := []string{}

	// Build storage proofs.
	storageProofs := make([]StorageProof, len(storageKeys))
	for i, keyHex := range storageKeys {
		key := types.HexToHash(keyHex)
		val := statedb.GetState(addr, key)

		// Convert to big.Int for proper hex encoding (strip leading zeros).
		valInt := new(big.Int).SetBytes(val[:])

		storageProofs[i] = StorageProof{
			Key:   keyHex,
			Value: encodeBigInt(valInt),
			Proof: []string{}, // Empty proof for now.
		}
	}

	result := &AccountProof{
		Address:      encodeAddress(addr),
		AccountProof: accountProofHex,
		Balance:      encodeBigInt(balance),
		CodeHash:     encodeHash(codeHash),
		Nonce:        encodeUint64(nonce),
		StorageHash:  encodeHash(storageHash),
		StorageProof: storageProofs,
	}

	return successResponse(req.ID, result)
}

// encodeAccountRLP is a placeholder for encoding an account as RLP.
// A full implementation would use rlp.EncodeToBytes with the account struct.
func encodeAccountRLP(nonce uint64, balance *big.Int, storageRoot, codeHash types.Hash) []byte {
	// Simplified: use the account struct from core/types.
	// In production, this would encode [nonce, balance, storageRoot, codeHash].
	_ = nonce
	_ = balance
	_ = storageRoot
	_ = codeHash
	return nil
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
// Returns a detailed execution trace for a given transaction hash.
func (api *EthAPI) debugTraceTransaction(req *Request) *Response {
	if len(req.Params) < 1 {
		return errorResponse(req.ID, ErrCodeInvalidParams, "missing transaction hash")
	}

	var txHashHex string
	if err := json.Unmarshal(req.Params[0], &txHashHex); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, "invalid tx hash: "+err.Error())
	}

	txHash := types.HexToHash(txHashHex)

	// Look up the transaction.
	tx, blockNum, txIndex := api.backend.GetTransaction(txHash)
	if tx == nil {
		return errorResponse(req.ID, ErrCodeInternal, "transaction not found")
	}

	_ = blockNum
	_ = txIndex

	// For a full implementation, we would:
	// 1. Get the block containing the transaction.
	// 2. Re-execute all transactions before this one to build up state.
	// 3. Execute this transaction with a tracing EVM that logs each step.
	// 4. Return the trace.

	// For now, return a minimal trace result.
	result := &TraceResult{
		Gas:         tx.Gas(),
		Failed:      false,
		ReturnValue: "",
		StructLogs:  []StructLog{},
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
	if int(index) >= len(txs) {
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
	if int(index) >= len(txs) {
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
