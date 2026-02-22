package rollup

import (
	"encoding/binary"
	"errors"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Anchor storage slot constants (similar to EIP-4788 beacon root contract).
const (
	// AnchorRingBufferSize is the number of anchor entries stored.
	// Matches the EIP-4788 history buffer size.
	AnchorRingBufferSize = 8191

	// AnchorSlotBlockHash is the base storage slot for block hashes.
	AnchorSlotBlockHash = 0

	// AnchorSlotStateRoot is the base storage slot for state roots.
	AnchorSlotStateRoot = AnchorRingBufferSize

	// AnchorSlotLatestBlockNumber stores the latest anchored block number.
	AnchorSlotLatestBlockNumber = AnchorRingBufferSize * 2

	// AnchorSlotLatestTimestamp stores the latest anchored timestamp.
	AnchorSlotLatestTimestamp = AnchorRingBufferSize*2 + 1
)

// Errors for anchor operations.
var (
	ErrAnchorDataTooShort = errors.New("anchor: data too short")
	ErrAnchorStaleBlock   = errors.New("anchor: block number not increasing")
)

// AnchorContract manages the anchor predeploy state for a native rollup.
// It provides L1->L2 anchoring by storing the latest L1 block hash and
// state root in a ring buffer, similar to EIP-4788.
type AnchorContract struct {
	// state tracks the current anchor state.
	state AnchorState

	// history stores past anchor entries in a ring buffer.
	history [AnchorRingBufferSize]AnchorEntry
}

// AnchorEntry is a single entry in the anchor ring buffer.
type AnchorEntry struct {
	BlockHash types.Hash
	StateRoot types.Hash
	Timestamp uint64
}

// NewAnchorContract creates a new anchor contract with empty state.
func NewAnchorContract() *AnchorContract {
	return &AnchorContract{}
}

// GetLatestState returns the most recent anchor state.
func (ac *AnchorContract) GetLatestState() AnchorState {
	return ac.state
}

// UpdateState updates the anchor with a new L1 state.
// The block number must be strictly increasing.
func (ac *AnchorContract) UpdateState(newState AnchorState) error {
	if newState.BlockNumber <= ac.state.BlockNumber && ac.state.BlockNumber > 0 {
		return ErrAnchorStaleBlock
	}

	// Store in ring buffer.
	idx := newState.BlockNumber % AnchorRingBufferSize
	ac.history[idx] = AnchorEntry{
		BlockHash: newState.LatestBlockHash,
		StateRoot: newState.LatestStateRoot,
		Timestamp: newState.Timestamp,
	}

	ac.state = newState
	return nil
}

// GetAnchorByNumber retrieves the anchor entry for a given block number
// if it is still in the ring buffer. Returns false if the entry has been
// overwritten or was never stored.
func (ac *AnchorContract) GetAnchorByNumber(blockNumber uint64) (AnchorEntry, bool) {
	if blockNumber == 0 || blockNumber > ac.state.BlockNumber {
		return AnchorEntry{}, false
	}

	// Check if the entry is still within the ring buffer window.
	if ac.state.BlockNumber-blockNumber >= AnchorRingBufferSize {
		return AnchorEntry{}, false
	}

	idx := blockNumber % AnchorRingBufferSize
	entry := ac.history[idx]

	// Verify it's the correct entry (not overwritten).
	if entry.BlockHash == (types.Hash{}) {
		return AnchorEntry{}, false
	}

	return entry, true
}

// ProcessAnchorData decodes and applies anchor data from an EXECUTE call.
// Anchor data format:
//
//	[0:32]   blockHash    (bytes32)
//	[32:64]  stateRoot    (bytes32)
//	[64:72]  blockNumber  (uint64, big-endian)
//	[72:80]  timestamp    (uint64, big-endian)
func (ac *AnchorContract) ProcessAnchorData(data []byte) error {
	if len(data) < 80 {
		return ErrAnchorDataTooShort
	}

	var blockHash, stateRoot types.Hash
	copy(blockHash[:], data[0:32])
	copy(stateRoot[:], data[32:64])
	blockNumber := binary.BigEndian.Uint64(data[64:72])
	timestamp := binary.BigEndian.Uint64(data[72:80])

	return ac.UpdateState(AnchorState{
		LatestBlockHash: blockHash,
		LatestStateRoot: stateRoot,
		BlockNumber:     blockNumber,
		Timestamp:       timestamp,
	})
}

// UpdateAnchorAfterExecute advances the anchor state after a successful EXECUTE
// precompile call. It validates the execution output, constructs the new anchor
// state from the output's post-state root and the provided block metadata, and
// updates the ring buffer. Returns an error if the output indicates failure or
// if the block number does not advance.
func (ac *AnchorContract) UpdateAnchorAfterExecute(output *ExecuteOutput, blockNumber, timestamp uint64) error {
	if output == nil {
		return ErrAnchorDataTooShort
	}
	if !output.Success {
		return ErrSTFailed
	}
	if output.PostStateRoot == (types.Hash{}) {
		return ErrInvalidBlockData
	}

	newState := AnchorState{
		LatestBlockHash: crypto.Keccak256Hash(output.PostStateRoot[:]),
		LatestStateRoot: output.PostStateRoot,
		BlockNumber:     blockNumber,
		Timestamp:       timestamp,
	}
	return ac.UpdateState(newState)
}

// EncodeAnchorData encodes an AnchorState into the wire format expected
// by ProcessAnchorData.
func EncodeAnchorData(state AnchorState) []byte {
	data := make([]byte, 80)
	copy(data[0:32], state.LatestBlockHash[:])
	copy(data[32:64], state.LatestStateRoot[:])
	binary.BigEndian.PutUint64(data[64:72], state.BlockNumber)
	binary.BigEndian.PutUint64(data[72:80], state.Timestamp)
	return data
}
