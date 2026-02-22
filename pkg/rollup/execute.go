package rollup

import (
	"encoding/binary"
	"errors"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/rlp"
)

// Gas constants for the EXECUTE precompile.
const (
	// ExecuteBaseGas is the flat gas cost for calling the EXECUTE precompile.
	ExecuteBaseGas uint64 = 100_000

	// ExecutePerByteGas is the per-byte gas cost for the block data input.
	ExecutePerByteGas uint64 = 16

	// MaxBlockDataSize limits the size of block data the precompile accepts (1 MiB).
	MaxBlockDataSize = 1 << 20
)

// Errors returned by the EXECUTE precompile.
var (
	ErrInputTooShort     = errors.New("execute: input too short")
	ErrBlockDataTooLarge = errors.New("execute: block data too large")
	ErrBlobTxNotAllowed  = errors.New("execute: blob transactions not supported")
	ErrInvalidBlockData  = errors.New("execute: invalid block data")
	ErrSTFailed          = errors.New("execute: state transition failed")
	ErrInvalidChainID    = errors.New("execute: invalid chain ID")
	ErrAnchorFailed      = errors.New("execute: anchor processing failed")
)

// ExecutePrecompile implements the EIP-8079 EXECUTE precompile.
// It verifies a state transition by re-executing a rollup block.
type ExecutePrecompile struct{}

// RequiredGas returns the gas required to run the EXECUTE precompile.
// Gas = ExecuteBaseGas + len(blockData) * ExecutePerByteGas
func (c *ExecutePrecompile) RequiredGas(input []byte) uint64 {
	if len(input) < minInputLen {
		return ExecuteBaseGas
	}
	blockDataLen := binary.BigEndian.Uint32(input[40:44])
	return ExecuteBaseGas + uint64(blockDataLen)*ExecutePerByteGas
}

// minInputLen is the minimum input size:
// chainID(8) + preStateRoot(32) + blockDataLen(4) + witnessLen(4) + anchorLen(4) = 52
const minInputLen = 52

// Run executes the EXECUTE precompile with the given input.
//
// Input format (ABI-packed, not ABI-encoded):
//
//	[0:8]    chainID          (uint64, big-endian)
//	[8:40]   preStateRoot     (bytes32)
//	[40:44]  blockDataLen     (uint32, big-endian)
//	[44:48]  witnessLen       (uint32, big-endian)
//	[48:52]  anchorDataLen    (uint32, big-endian)
//	[52:52+blockDataLen]        blockData
//	[52+blockDataLen:...]       witness
//	[52+blockDataLen+witnessLen:...]  anchorData
//
// Output format:
//
//	[0:32]   postStateRoot    (bytes32)
//	[32:64]  receiptsRoot     (bytes32)
//	[64:72]  gasUsed          (uint64, big-endian)
//	[72:80]  burnedFees       (uint64, big-endian)
//	[80]     success          (1 byte, 0 or 1)
func (c *ExecutePrecompile) Run(input []byte) ([]byte, error) {
	// Parse the input.
	execInput, err := decodeExecuteInput(input)
	if err != nil {
		return nil, err
	}

	// Validate: block data must not exceed maximum size.
	if len(execInput.BlockData) > MaxBlockDataSize {
		return nil, ErrBlockDataTooLarge
	}

	// Execute the state transition.
	output, err := executeSTF(execInput)
	if err != nil {
		// Return a failure output rather than an error for STF failures,
		// so the precompile call doesn't revert the outer transaction.
		output = &ExecuteOutput{Success: false}
	}

	return encodeExecuteOutput(output), nil
}

// decodeExecuteInput parses the raw input bytes into an ExecuteInput.
func decodeExecuteInput(input []byte) (*ExecuteInput, error) {
	if len(input) < minInputLen {
		return nil, ErrInputTooShort
	}

	chainID := binary.BigEndian.Uint64(input[0:8])
	var preStateRoot types.Hash
	copy(preStateRoot[:], input[8:40])

	blockDataLen := binary.BigEndian.Uint32(input[40:44])
	witnessLen := binary.BigEndian.Uint32(input[44:48])
	anchorDataLen := binary.BigEndian.Uint32(input[48:52])

	totalLen := minInputLen + int(blockDataLen) + int(witnessLen) + int(anchorDataLen)
	if len(input) < totalLen {
		return nil, ErrInputTooShort
	}

	offset := 52
	blockData := make([]byte, blockDataLen)
	copy(blockData, input[offset:offset+int(blockDataLen)])
	offset += int(blockDataLen)

	witness := make([]byte, witnessLen)
	copy(witness, input[offset:offset+int(witnessLen)])
	offset += int(witnessLen)

	anchorData := make([]byte, anchorDataLen)
	copy(anchorData, input[offset:offset+int(anchorDataLen)])

	return &ExecuteInput{
		ChainID:      chainID,
		PreStateRoot: preStateRoot,
		BlockData:    blockData,
		Witness:      witness,
		AnchorData:   anchorData,
	}, nil
}

// encodeExecuteOutput serializes an ExecuteOutput into the precompile return format.
func encodeExecuteOutput(output *ExecuteOutput) []byte {
	result := make([]byte, 81)
	copy(result[0:32], output.PostStateRoot[:])
	copy(result[32:64], output.ReceiptsRoot[:])
	binary.BigEndian.PutUint64(result[64:72], output.GasUsed)
	binary.BigEndian.PutUint64(result[72:80], output.BurnedFees)
	if output.Success {
		result[80] = 1
	}
	return result
}

// executeSTF runs the state transition function on the rollup block.
// This is a simplified implementation that validates the block structure
// and checks for disallowed transaction types per EIP-8079.
func executeSTF(input *ExecuteInput) (*ExecuteOutput, error) {
	if input.ChainID == 0 {
		return nil, ErrInvalidChainID
	}

	// Decode the block from RLP.
	var blockHeader blockHeaderRLP
	if err := rlp.DecodeBytes(input.BlockData, &blockHeader); err != nil {
		// Try as a raw block with transactions.
		return executeFromRawBlock(input)
	}

	// Compute the post-state root as hash of (preStateRoot || blockData).
	// In a full implementation this would run the actual EVM state transition.
	postState := computeSimulatedStateRoot(input.PreStateRoot, input.BlockData)

	return &ExecuteOutput{
		PostStateRoot: postState,
		ReceiptsRoot:  computeSimulatedReceiptsRoot(input.BlockData),
		GasUsed:       blockHeader.GasUsed,
		BurnedFees:    0,
		Success:       true,
	}, nil
}

// blockHeaderRLP is a minimal header structure for RLP decoding validation.
type blockHeaderRLP struct {
	ParentHash  types.Hash
	Coinbase    types.Address
	StateRoot   types.Hash
	TxHash      types.Hash
	ReceiptHash types.Hash
	Number      uint64
	GasLimit    uint64
	GasUsed     uint64
	Time        uint64
	Extra       []byte
}

// executeFromRawBlock handles block data that doesn't decode as a simple header.
// It validates the data is non-empty and produces a deterministic output.
func executeFromRawBlock(input *ExecuteInput) (*ExecuteOutput, error) {
	if len(input.BlockData) == 0 {
		return nil, ErrInvalidBlockData
	}

	// Validate: scan for blob transaction type bytes.
	// Per EIP-8079, blob transactions are not supported in native rollup execution.
	if err := checkNoBlobTransactions(input.BlockData); err != nil {
		return nil, err
	}

	postState := computeSimulatedStateRoot(input.PreStateRoot, input.BlockData)

	return &ExecuteOutput{
		PostStateRoot: postState,
		ReceiptsRoot:  computeSimulatedReceiptsRoot(input.BlockData),
		GasUsed:       21000, // minimum single-tx gas
		BurnedFees:    0,
		Success:       true,
	}, nil
}

// checkNoBlobTransactions inspects block data for blob transaction type markers.
// Per EIP-8079: "Blob-carrying transactions are not supported."
func checkNoBlobTransactions(blockData []byte) error {
	// Attempt to decode as an RLP list of transactions.
	var txList [][]byte
	if err := rlp.DecodeBytes(blockData, &txList); err == nil {
		for _, txBytes := range txList {
			if len(txBytes) > 0 && txBytes[0] == types.BlobTxType {
				return ErrBlobTxNotAllowed
			}
		}
	}
	return nil
}

// computeSimulatedStateRoot produces a deterministic post-state root from
// the pre-state root and block data. In a full implementation, this would
// be replaced by actual EVM state transition execution.
func computeSimulatedStateRoot(preStateRoot types.Hash, blockData []byte) types.Hash {
	h := crypto.Keccak256(preStateRoot[:], blockData)
	var result types.Hash
	copy(result[:], h)
	return result
}

// ValidateRollupExecution checks that an ExecuteInput is well-formed:
//   - ChainID must be non-zero
//   - PreStateRoot must be non-zero
//   - BlockData must be non-empty and within MaxBlockDataSize
func ValidateRollupExecution(input *ExecuteInput) error {
	if input == nil {
		return ErrInputTooShort
	}
	if input.ChainID == 0 {
		return ErrInvalidChainID
	}
	if input.PreStateRoot == (types.Hash{}) {
		return errors.New("execute: zero pre-state root")
	}
	if len(input.BlockData) == 0 {
		return ErrInvalidBlockData
	}
	if len(input.BlockData) > MaxBlockDataSize {
		return ErrBlockDataTooLarge
	}
	return nil
}

// computeSimulatedReceiptsRoot produces a deterministic receipts root from
// block data. In a full implementation, this would be the actual trie root
// of transaction receipts.
func computeSimulatedReceiptsRoot(blockData []byte) types.Hash {
	h := crypto.Keccak256(blockData)
	var result types.Hash
	copy(result[:], h)
	return result
}
