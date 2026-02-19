// Package rollup implements EIP-8079 native rollups, exposing the Ethereum
// state transition function as an EXECUTE precompile for rollup verification.
package rollup

import (
	"math/big"

	"github.com/eth2028/eth2028/core/types"
)

// ExecutePrecompileAddress is the address of the EXECUTE precompile.
// Uses 0x0101 (in the extended precompile range, after P256VERIFY at 0x0100).
var ExecutePrecompileAddress = types.BytesToAddress([]byte{0x01, 0x01})

// AnchorAddress is the predeploy address for the anchor contract that
// stores L1->L2 anchoring data within native rollup state.
var AnchorAddress = types.BytesToAddress([]byte{0x01, 0x02})

// RollupConfig defines the configuration for a native rollup chain.
type RollupConfig struct {
	// ChainID uniquely identifies this rollup chain.
	ChainID *big.Int

	// AnchorAddress is the address of the anchor predeploy on the rollup.
	AnchorAddress types.Address

	// GenesisStateRoot is the state root of the rollup genesis block.
	GenesisStateRoot types.Hash

	// GasLimit is the block gas limit for the rollup.
	GasLimit uint64

	// BaseFee is the base fee configuration for the rollup.
	BaseFee *big.Int

	// AllowBlobTx controls whether blob-carrying transactions are permitted.
	// Per EIP-8079, blob transactions are disabled by default.
	AllowBlobTx bool
}

// ExecuteInput is the input data for the EXECUTE precompile.
// It encodes the rollup chain configuration, pre-state, block data, and
// optional witness for stateless execution.
type ExecuteInput struct {
	// ChainID identifies the rollup chain being executed.
	ChainID uint64

	// PreStateRoot is the state root before block execution.
	PreStateRoot types.Hash

	// BlockData is the RLP-encoded block to execute.
	BlockData []byte

	// Witness is the optional execution witness for stateless validation.
	Witness []byte

	// AnchorData is the data to inject via the anchor system transaction
	// (e.g., L1 state root, message root, or rolling hash).
	AnchorData []byte
}

// ExecuteOutput is the result of the EXECUTE precompile.
type ExecuteOutput struct {
	// PostStateRoot is the state root after block execution.
	PostStateRoot types.Hash

	// ReceiptsRoot is the root of the receipts trie after execution.
	ReceiptsRoot types.Hash

	// GasUsed is the total gas consumed by the block.
	GasUsed uint64

	// BurnedFees is the total base fees burned during execution (EIP-8079 header extension).
	BurnedFees uint64

	// Success indicates whether the state transition completed without error.
	Success bool
}

// AnchorState represents the state stored in the anchor predeploy contract.
// It tracks the latest verified L1 state for the rollup.
type AnchorState struct {
	// LatestBlockHash is the hash of the most recently anchored L1 block.
	LatestBlockHash types.Hash

	// LatestStateRoot is the state root of the most recently anchored L1 block.
	LatestStateRoot types.Hash

	// BlockNumber is the L1 block number of the latest anchor.
	BlockNumber uint64

	// Timestamp is the L1 block timestamp of the latest anchor.
	Timestamp uint64
}

// ProofData carries a rollup state transition proof for on-chain verification.
type ProofData struct {
	// RollupID identifies which rollup this proof is for.
	RollupID uint64

	// Proof is the encoded validity proof (ZK or re-execution).
	Proof []byte

	// PublicInputs are the public inputs to the proof circuit.
	PublicInputs []byte

	// PreStateRoot is the claimed pre-state root.
	PreStateRoot types.Hash

	// PostStateRoot is the claimed post-state root.
	PostStateRoot types.Hash
}
