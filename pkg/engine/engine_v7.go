package engine

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// Engine API V7 extends the Engine API for the 2030 roadmap (K+ era).
// Adds support for DA layer configuration, proof requirements (mandatory
// 3-of-5 proofs), and shielded transactions (encrypted mempool).

// EngineV7Backend defines the backend interface for Engine API V7.
// Implementations must be safe for concurrent use.
type EngineV7Backend interface {
	// NewPayloadV7 validates and executes a 2028-era payload with blob
	// commitments, proof submissions, and shielded transaction results.
	NewPayloadV7(payload *ExecutionPayloadV7) (*PayloadStatusV1, error)

	// ForkchoiceUpdatedV7 processes a forkchoice update with V7 attributes
	// including DA layer config, proof requirements, and shielded txs.
	ForkchoiceUpdatedV7(state *ForkchoiceStateV1, attrs *PayloadAttributesV7) (*ForkchoiceUpdatedResult, error)

	// GetPayloadV7 retrieves a previously built V7 payload by ID.
	GetPayloadV7(id PayloadID) (*ExecutionPayloadV7, error)
}

// DALayerConfig configures the data availability layer for the 2030 roadmap.
// Controls peerDAS parameters and blob reconstruction settings.
type DALayerConfig struct {
	// SampleCount is the number of DA samples required per slot.
	SampleCount uint64 `json:"sampleCount"`
	// ColumnCount is the number of columns in the DAS matrix.
	ColumnCount uint64 `json:"columnCount"`
	// RecoveryThreshold is the minimum fraction (in basis points) of
	// samples needed for local blob reconstruction.
	RecoveryThreshold uint64 `json:"recoveryThreshold"`
}

// ProofRequirements specifies the mandatory proof parameters per the K+ era.
// Requires 3-of-5 proof submissions to be valid.
type ProofRequirements struct {
	// MinProofs is the minimum number of valid proofs required (default 3).
	MinProofs uint64 `json:"minProofs"`
	// TotalProofs is the total number of proof slots (default 5).
	TotalProofs uint64 `json:"totalProofs"`
	// AllowedTypes lists the accepted proof type identifiers.
	AllowedTypes []string `json:"allowedTypes"`
}

// Validate checks that proof requirements are internally consistent.
func (pr *ProofRequirements) Validate() error {
	if pr.TotalProofs == 0 {
		return errors.New("engine: totalProofs must be > 0")
	}
	if pr.MinProofs == 0 {
		return errors.New("engine: minProofs must be > 0")
	}
	if pr.MinProofs > pr.TotalProofs {
		return fmt.Errorf("engine: minProofs (%d) > totalProofs (%d)", pr.MinProofs, pr.TotalProofs)
	}
	return nil
}

// PayloadAttributesV7 extends V3 attributes with 2030 roadmap features.
type PayloadAttributesV7 struct {
	PayloadAttributesV3

	// DALayerConfig configures peerDAS and blob reconstruction for this payload.
	DALayerConfig *DALayerConfig `json:"daLayerConfig,omitempty"`

	// ProofRequirements specifies mandatory proof parameters (3-of-5).
	ProofRequirements *ProofRequirements `json:"proofRequirements,omitempty"`

	// ShieldedTxs contains encrypted transaction payloads for the
	// encrypted mempool (post-quantum shielded compute).
	ShieldedTxs [][]byte `json:"shieldedTxs,omitempty"`
}

// ExecutionPayloadV7 extends V3 with 2030 roadmap fields.
type ExecutionPayloadV7 struct {
	ExecutionPayloadV3

	// BlobCommitments are KZG commitments for the blobs in this payload.
	BlobCommitments []types.Hash `json:"blobCommitments"`

	// ProofSubmissions contains the mandatory proof data (3-of-5).
	// Each entry is an opaque proof blob validated by the proof verifier.
	ProofSubmissions [][]byte `json:"proofSubmissions"`

	// ShieldedResults are the state-root hashes of shielded transaction
	// execution results, enabling verification without revealing contents.
	ShieldedResults []types.Hash `json:"shieldedResults"`
}

// GetPayloadV7Response is the response for engine_getPayloadV7.
type GetPayloadV7Response struct {
	ExecutionPayload *ExecutionPayloadV7 `json:"executionPayload"`
	BlockValue       []byte              `json:"blockValue"`
	BlobsBundle      *BlobsBundleV1      `json:"blobsBundle"`
	Override         bool                `json:"shouldOverrideBuilder"`
}

// EngineV7 provides the Engine API V7 methods.
// Thread-safe: all state is protected by a mutex.
type EngineV7 struct {
	mu      sync.Mutex
	backend EngineV7Backend
}

// NewEngineV7 creates a new Engine API V7 handler.
func NewEngineV7(backend EngineV7Backend) *EngineV7 {
	return &EngineV7{
		backend: backend,
	}
}

// HandleNewPayloadV7 validates and executes a 2028-era execution payload.
// Validates blob commitments, proof submissions (3-of-5), and shielded results.
func (e *EngineV7) HandleNewPayloadV7(payload *ExecutionPayloadV7) (*PayloadStatusV1, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if payload == nil {
		return nil, ErrInvalidParams
	}

	// Validate blob commitments are present when blob gas is used.
	if payload.BlobGasUsed > 0 && len(payload.BlobCommitments) == 0 {
		errMsg := "blob gas used but no blob commitments provided"
		return &PayloadStatusV1{
			Status:          StatusInvalid,
			ValidationError: &errMsg,
		}, nil
	}

	// Validate proof submissions format (non-nil, but can be empty if no
	// proof requirements are active for this payload).
	if payload.ProofSubmissions == nil {
		return nil, ErrInvalidParams
	}

	// Validate no empty proof entries.
	for i, proof := range payload.ProofSubmissions {
		if len(proof) == 0 {
			errMsg := fmt.Sprintf("empty proof submission at index %d", i)
			return &PayloadStatusV1{
				Status:          StatusInvalid,
				ValidationError: &errMsg,
			}, nil
		}
	}

	return e.backend.NewPayloadV7(payload)
}

// HandleForkchoiceUpdatedV7 processes a forkchoice state update with V7
// payload attributes. Validates the forkchoice state and optional attributes
// including DA layer config, proof requirements, and shielded transactions.
func (e *EngineV7) HandleForkchoiceUpdatedV7(
	state *ForkchoiceStateV1,
	attrs *PayloadAttributesV7,
) (*ForkchoiceUpdatedResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if state == nil {
		return nil, ErrInvalidForkchoiceState
	}

	// Validate forkchoice state: head must be non-zero.
	if state.HeadBlockHash == (types.Hash{}) {
		return nil, ErrInvalidForkchoiceState
	}

	// Validate attributes if provided.
	if attrs != nil {
		if attrs.Timestamp == 0 {
			return nil, ErrInvalidPayloadAttributes
		}

		// Validate proof requirements if specified.
		if attrs.ProofRequirements != nil {
			if err := attrs.ProofRequirements.Validate(); err != nil {
				return nil, ErrInvalidPayloadAttributes
			}
		}

		// Validate DA layer config if specified.
		if attrs.DALayerConfig != nil {
			if attrs.DALayerConfig.SampleCount == 0 {
				return nil, ErrInvalidPayloadAttributes
			}
			if attrs.DALayerConfig.ColumnCount == 0 {
				return nil, ErrInvalidPayloadAttributes
			}
		}
	}

	return e.backend.ForkchoiceUpdatedV7(state, attrs)
}

// HandleGetPayloadV7 retrieves a previously built V7 payload by its ID.
func (e *EngineV7) HandleGetPayloadV7(payloadID PayloadID) (*ExecutionPayloadV7, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if payloadID == (PayloadID{}) {
		return nil, ErrUnknownPayload
	}

	return e.backend.GetPayloadV7(payloadID)
}

// generateV7PayloadID creates a V7 payload ID from parent hash and timestamp.
func generateV7PayloadID(parentHash types.Hash, timestamp uint64) PayloadID {
	var id PayloadID
	// Mix parent hash bytes with timestamp for uniqueness.
	binary.BigEndian.PutUint64(id[:], timestamp)
	// XOR in all 32 bytes of the parent hash, folded into 8 bytes.
	for i := 0; i < types.HashLength; i++ {
		id[i%8] ^= parentHash[i]
	}
	return id
}
