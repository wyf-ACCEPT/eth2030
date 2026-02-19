package zkvm

import (
	"errors"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// Guest execution errors.
var (
	ErrNilGuestContext = errors.New("zkvm: nil guest context")
	ErrEmptyBlockData  = errors.New("zkvm: empty block data")
	ErrGuestPanicked   = errors.New("zkvm: guest execution panicked")
)

// GuestContext provides a restricted execution environment for the zkVM guest.
// It holds the pre-state and witness data needed for stateless block execution,
// mirroring the geth keeper/main.go pattern.
type GuestContext struct {
	// stateRoot is the pre-execution state root.
	stateRoot types.Hash

	// witness is the serialized execution witness.
	witness []byte

	// chainID identifies the chain being executed.
	chainID uint64

	// executed tracks whether the context has been used.
	executed bool
}

// NewGuestContext creates a new guest execution context.
func NewGuestContext(stateRoot types.Hash, witness []byte) *GuestContext {
	return &GuestContext{
		stateRoot: stateRoot,
		witness:   witness,
	}
}

// NewGuestContextWithChain creates a guest context with a specific chain ID.
func NewGuestContextWithChain(stateRoot types.Hash, witness []byte, chainID uint64) *GuestContext {
	return &GuestContext{
		stateRoot: stateRoot,
		witness:   witness,
		chainID:   chainID,
	}
}

// StateRoot returns the pre-execution state root.
func (ctx *GuestContext) StateRoot() types.Hash {
	return ctx.stateRoot
}

// Witness returns the execution witness data.
func (ctx *GuestContext) Witness() []byte {
	return ctx.witness
}

// ChainID returns the chain identifier.
func (ctx *GuestContext) ChainID() uint64 {
	return ctx.chainID
}

// IsExecuted returns whether this context has already been used.
func (ctx *GuestContext) IsExecuted() bool {
	return ctx.executed
}

// ExecuteBlock runs the state transition for a block within the guest context.
// It returns the post-execution state root.
//
// This mirrors the keeper pattern from go-ethereum:
//  1. Decode the block from RLP
//  2. Execute the block statelessly using the witness
//  3. Return the computed state root
//
// In a full implementation, this would invoke core.ExecuteStateless.
// The current implementation computes a deterministic hash for testing.
func ExecuteBlock(ctx *GuestContext, blockData []byte) (types.Hash, error) {
	if ctx == nil {
		return types.Hash{}, ErrNilGuestContext
	}
	if len(blockData) == 0 {
		return types.Hash{}, ErrEmptyBlockData
	}
	if ctx.executed {
		return types.Hash{}, errors.New("zkvm: context already executed")
	}

	ctx.executed = true

	// Compute post-state root: H(stateRoot || witness || blockData).
	// In production, this is replaced by actual EVM state transition.
	h := crypto.Keccak256(ctx.stateRoot[:], ctx.witness, blockData)
	var postStateRoot types.Hash
	copy(postStateRoot[:], h)

	return postStateRoot, nil
}

// ExecuteBlockFull runs a full block execution and returns a complete result.
func ExecuteBlockFull(ctx *GuestContext, blockData []byte) (*ExecutionResult, error) {
	postState, err := ExecuteBlock(ctx, blockData)
	if err != nil {
		return &ExecutionResult{
			PreStateRoot: ctx.stateRoot,
			Success:      false,
		}, err
	}

	// Compute receipts root from block data.
	receiptsHash := crypto.Keccak256(blockData, postState[:])
	var receiptsRoot types.Hash
	copy(receiptsRoot[:], receiptsHash)

	return &ExecutionResult{
		PreStateRoot:  ctx.stateRoot,
		PostStateRoot: postState,
		ReceiptsRoot:  receiptsRoot,
		GasUsed:       21000, // placeholder
		Success:       true,
	}, nil
}
