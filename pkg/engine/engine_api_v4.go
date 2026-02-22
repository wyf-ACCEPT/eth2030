// engine_api_v4.go implements Engine API V4 for Prague/Electra, adding
// Verkle proof parameters to newPayload, execution request processing
// (deposits EIP-6110, withdrawal requests EIP-7002, consolidation
// requests EIP-7251), and an extended getPayload response.
package engine

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Execution request type identifiers per EIP-7685.
const (
	// DepositRequestType identifies deposit requests (EIP-6110).
	DepositRequestType byte = 0x00

	// WithdrawalRequestType identifies withdrawal requests (EIP-7002).
	WithdrawalRequestType byte = 0x01

	// ConsolidationRequestType identifies consolidation requests (EIP-7251).
	ConsolidationRequestType byte = 0x02
)

// Maximum sizes for execution requests per type.
const (
	MaxDepositRequests       = 8192
	MaxWithdrawalRequests    = 16
	MaxConsolidationRequests = 1
	DepositRequestSize       = 192 // pubkey(48) + withdrawal_creds(32) + amount(8) + signature(96) + index(8)
	WithdrawalRequestSize    = 76  // source_address(20) + validator_pubkey(48) + amount(8)
	ConsolidationRequestSize = 116 // source_address(20) + source_pubkey(48) + target_pubkey(48)
)

// EngV4 errors.
var (
	ErrV4NilPayload           = errors.New("engine_v4: nil execution payload")
	ErrV4RequestTypeMismatch  = errors.New("engine_v4: execution request type mismatch")
	ErrV4RequestTooLarge      = errors.New("engine_v4: execution request exceeds maximum size")
	ErrV4MissingRequests      = errors.New("engine_v4: execution requests required for Prague")
	ErrV4InvalidRequestOrder  = errors.New("engine_v4: execution requests not in type-ascending order")
	ErrV4VerkleProofInvalid   = errors.New("engine_v4: invalid verkle proof")
	ErrV4DuplicateRequestType = errors.New("engine_v4: duplicate execution request type")
)

// EngV4 manages Engine API V4 logic for Prague/Electra.
type EngV4 struct {
	backend Backend
}

// NewEngV4 creates a new EngV4 instance.
func NewEngV4(backend Backend) *EngV4 {
	return &EngV4{backend: backend}
}

// DepositRequest represents an EIP-6110 deposit from the execution layer.
type DepositRequest struct {
	Pubkey                [48]byte   `json:"pubkey"`
	WithdrawalCredentials types.Hash `json:"withdrawalCredentials"`
	Amount                uint64     `json:"amount"` // in Gwei
	Signature             [96]byte   `json:"signature"`
	Index                 uint64     `json:"index"`
}

// WithdrawalRequest represents an EIP-7002 withdrawal request triggered
// from the execution layer's withdrawal request contract.
type WithdrawalRequest struct {
	SourceAddress   types.Address `json:"sourceAddress"`
	ValidatorPubkey [48]byte      `json:"validatorPubkey"`
	Amount          uint64        `json:"amount"` // in Gwei, 0 = full exit
}

// ConsolidationRequest represents an EIP-7251 consolidation request that
// merges one validator's stake into another.
type ConsolidationRequest struct {
	SourceAddress types.Address `json:"sourceAddress"`
	SourcePubkey  [48]byte      `json:"sourcePubkey"`
	TargetPubkey  [48]byte      `json:"targetPubkey"`
}

// ExecutionRequestsV4 holds categorized execution requests for Prague.
type ExecutionRequestsV4 struct {
	Deposits       []DepositRequest       `json:"deposits"`
	Withdrawals    []WithdrawalRequest    `json:"withdrawals"`
	Consolidations []ConsolidationRequest `json:"consolidations"`
}

// VerkleProofData contains Verkle proof material for stateless validation.
type VerkleProofData struct {
	// D is the proof polynomial commitment.
	D []byte `json:"d"`

	// DepthExtensionPresent encodes the node type per stem.
	DepthExtensionPresent []byte `json:"depthExtensionPresent"`

	// CommitmentsByPath maps stem paths to their commitments.
	CommitmentsByPath [][]byte `json:"commitmentsByPath"`

	// OtherStems lists stems referenced by the proof that are not in the
	// main key set.
	OtherStems [][]byte `json:"otherStems"`

	// IpaProof is the Inner Product Argument proof.
	IpaProof []byte `json:"ipaProof"`
}

// GetPayloadV4Result extends the V3 result with execution requests and
// Verkle proof data.
type GetPayloadV4Result struct {
	ExecutionPayload  *ExecutionPayloadV3 `json:"executionPayload"`
	BlockValue        *big.Int            `json:"blockValue"`
	BlobsBundle       *BlobsBundleV1      `json:"blobsBundle"`
	ShouldOverride    bool                `json:"shouldOverrideBuilder"`
	ExecutionRequests [][]byte            `json:"executionRequests"`
}

// ValidateExecutionRequests checks that the execution requests byte slices
// are well-formed: each starts with a valid type byte, the list is sorted
// by type in ascending order, and no type appears more than once.
func ValidateExecutionRequests(requests [][]byte) error {
	if requests == nil {
		return ErrV4MissingRequests
	}

	seenTypes := make(map[byte]bool)
	lastType := byte(0)

	for i, req := range requests {
		if len(req) < 1 {
			return fmt.Errorf("%w: empty request at index %d", ErrV4RequestTypeMismatch, i)
		}

		reqType := req[0]

		// Validate known types.
		switch reqType {
		case DepositRequestType:
			if err := validateDepositRequestPayload(req[1:]); err != nil {
				return err
			}
		case WithdrawalRequestType:
			if err := validateWithdrawalRequestPayload(req[1:]); err != nil {
				return err
			}
		case ConsolidationRequestType:
			if err := validateConsolidationRequestPayload(req[1:]); err != nil {
				return err
			}
		default:
			// Unknown request types are accepted but not validated.
		}

		// Check ascending order.
		if i > 0 && reqType < lastType {
			return fmt.Errorf("%w: type 0x%02x at index %d after 0x%02x",
				ErrV4InvalidRequestOrder, reqType, i, lastType)
		}

		// Check for duplicates.
		if seenTypes[reqType] {
			return fmt.Errorf("%w: type 0x%02x at index %d",
				ErrV4DuplicateRequestType, reqType, i)
		}
		seenTypes[reqType] = true
		lastType = reqType
	}

	return nil
}

// validateDepositRequestPayload checks a deposit request payload size.
func validateDepositRequestPayload(payload []byte) error {
	if len(payload) == 0 {
		return nil // empty deposit list is valid
	}
	if len(payload)%DepositRequestSize != 0 {
		return fmt.Errorf("%w: deposit payload length %d not a multiple of %d",
			ErrV4RequestTooLarge, len(payload), DepositRequestSize)
	}
	count := len(payload) / DepositRequestSize
	if count > MaxDepositRequests {
		return fmt.Errorf("%w: %d deposits exceeds maximum %d",
			ErrV4RequestTooLarge, count, MaxDepositRequests)
	}
	return nil
}

// validateWithdrawalRequestPayload checks a withdrawal request payload size.
func validateWithdrawalRequestPayload(payload []byte) error {
	if len(payload) == 0 {
		return nil
	}
	if len(payload)%WithdrawalRequestSize != 0 {
		return fmt.Errorf("%w: withdrawal payload length %d not a multiple of %d",
			ErrV4RequestTooLarge, len(payload), WithdrawalRequestSize)
	}
	count := len(payload) / WithdrawalRequestSize
	if count > MaxWithdrawalRequests {
		return fmt.Errorf("%w: %d withdrawal requests exceeds maximum %d",
			ErrV4RequestTooLarge, count, MaxWithdrawalRequests)
	}
	return nil
}

// validateConsolidationRequestPayload checks a consolidation request payload size.
func validateConsolidationRequestPayload(payload []byte) error {
	if len(payload) == 0 {
		return nil
	}
	if len(payload)%ConsolidationRequestSize != 0 {
		return fmt.Errorf("%w: consolidation payload length %d not a multiple of %d",
			ErrV4RequestTooLarge, len(payload), ConsolidationRequestSize)
	}
	count := len(payload) / ConsolidationRequestSize
	if count > MaxConsolidationRequests {
		return fmt.Errorf("%w: %d consolidation requests exceeds maximum %d",
			ErrV4RequestTooLarge, count, MaxConsolidationRequests)
	}
	return nil
}

// DecodeDepositRequests parses deposit request objects from the raw bytes.
func DecodeDepositRequests(payload []byte) ([]DepositRequest, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	if len(payload)%DepositRequestSize != 0 {
		return nil, fmt.Errorf("deposit payload length %d not a multiple of %d",
			len(payload), DepositRequestSize)
	}

	count := len(payload) / DepositRequestSize
	deposits := make([]DepositRequest, count)

	for i := 0; i < count; i++ {
		offset := i * DepositRequestSize
		chunk := payload[offset : offset+DepositRequestSize]

		copy(deposits[i].Pubkey[:], chunk[0:48])
		copy(deposits[i].WithdrawalCredentials[:], chunk[48:80])
		deposits[i].Amount = binary.LittleEndian.Uint64(chunk[80:88])
		copy(deposits[i].Signature[:], chunk[88:184])
		deposits[i].Index = binary.LittleEndian.Uint64(chunk[184:192])
	}

	return deposits, nil
}

// DecodeWithdrawalRequests parses withdrawal request objects from the raw bytes.
func DecodeWithdrawalRequests(payload []byte) ([]WithdrawalRequest, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	if len(payload)%WithdrawalRequestSize != 0 {
		return nil, fmt.Errorf("withdrawal payload length %d not a multiple of %d",
			len(payload), WithdrawalRequestSize)
	}

	count := len(payload) / WithdrawalRequestSize
	reqs := make([]WithdrawalRequest, count)

	for i := 0; i < count; i++ {
		offset := i * WithdrawalRequestSize
		chunk := payload[offset : offset+WithdrawalRequestSize]

		copy(reqs[i].SourceAddress[:], chunk[0:20])
		copy(reqs[i].ValidatorPubkey[:], chunk[20:68])
		reqs[i].Amount = binary.LittleEndian.Uint64(chunk[68:76])
	}

	return reqs, nil
}

// DecodeConsolidationRequests parses consolidation request objects from raw bytes.
func DecodeConsolidationRequests(payload []byte) ([]ConsolidationRequest, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	if len(payload)%ConsolidationRequestSize != 0 {
		return nil, fmt.Errorf("consolidation payload length %d not a multiple of %d",
			len(payload), ConsolidationRequestSize)
	}

	count := len(payload) / ConsolidationRequestSize
	reqs := make([]ConsolidationRequest, count)

	for i := 0; i < count; i++ {
		offset := i * ConsolidationRequestSize
		chunk := payload[offset : offset+ConsolidationRequestSize]

		copy(reqs[i].SourceAddress[:], chunk[0:20])
		copy(reqs[i].SourcePubkey[:], chunk[20:68])
		copy(reqs[i].TargetPubkey[:], chunk[68:116])
	}

	return reqs, nil
}

// EncodeDepositRequest serializes a deposit request to bytes.
func EncodeDepositRequest(d *DepositRequest) []byte {
	buf := make([]byte, DepositRequestSize)
	copy(buf[0:48], d.Pubkey[:])
	copy(buf[48:80], d.WithdrawalCredentials[:])
	binary.LittleEndian.PutUint64(buf[80:88], d.Amount)
	copy(buf[88:184], d.Signature[:])
	binary.LittleEndian.PutUint64(buf[184:192], d.Index)
	return buf
}

// EncodeWithdrawalRequest serializes a withdrawal request to bytes.
func EncodeWithdrawalRequest(w *WithdrawalRequest) []byte {
	buf := make([]byte, WithdrawalRequestSize)
	copy(buf[0:20], w.SourceAddress[:])
	copy(buf[20:68], w.ValidatorPubkey[:])
	binary.LittleEndian.PutUint64(buf[68:76], w.Amount)
	return buf
}

// EncodeConsolidationRequest serializes a consolidation request to bytes.
func EncodeConsolidationRequest(c *ConsolidationRequest) []byte {
	buf := make([]byte, ConsolidationRequestSize)
	copy(buf[0:20], c.SourceAddress[:])
	copy(buf[20:68], c.SourcePubkey[:])
	copy(buf[68:116], c.TargetPubkey[:])
	return buf
}

// NewPayloadV4WithVerkle validates and processes a Prague payload with
// an optional Verkle proof parameter for stateless validation.
func (v4 *EngV4) NewPayloadV4WithVerkle(
	payload *ExecutionPayloadV3,
	expectedBlobVersionedHashes []types.Hash,
	parentBeaconBlockRoot types.Hash,
	executionRequests [][]byte,
	verkleProof *VerkleProofData,
) (PayloadStatusV1, error) {
	if payload == nil {
		return PayloadStatusV1{}, ErrV4NilPayload
	}

	// Fork check: must be Prague.
	if !v4.backend.IsPrague(payload.Timestamp) {
		return PayloadStatusV1{}, ErrUnsupportedFork
	}

	// EIP-4788: beacon root must be provided.
	if parentBeaconBlockRoot == (types.Hash{}) {
		return PayloadStatusV1{}, ErrMissingBeaconRoot
	}

	// EIP-7685: execution requests validation.
	if err := ValidateExecutionRequests(executionRequests); err != nil {
		errMsg := err.Error()
		return PayloadStatusV1{
			Status:          StatusInvalid,
			ValidationError: &errMsg,
		}, nil
	}

	// EIP-4844: validate blob versioned hashes.
	if err := validateBlobHashes(payload, expectedBlobVersionedHashes); err != nil {
		errMsg := err.Error()
		return PayloadStatusV1{
			Status:          StatusInvalid,
			ValidationError: &errMsg,
		}, nil
	}

	// Verify block hash matches the computed hash.
	computedHash := computePayloadBlockHash(payload)
	if payload.BlockHash != computedHash {
		errMsg := fmt.Sprintf("block hash mismatch: got %s, computed %s",
			payload.BlockHash.Hex(), computedHash.Hex())
		return PayloadStatusV1{
			Status:          StatusInvalidBlockHash,
			ValidationError: &errMsg,
		}, nil
	}

	// Delegate to backend for full execution.
	return v4.backend.ProcessBlockV4(payload, expectedBlobVersionedHashes,
		parentBeaconBlockRoot, executionRequests)
}

// GetPayloadV4 retrieves a previously built Prague payload and returns
// it along with execution requests and block value.
func (v4 *EngV4) GetPayloadV4(payloadID PayloadID) (*GetPayloadV4Result, error) {
	resp, err := v4.backend.GetPayloadV4ByID(payloadID)
	if err != nil {
		return nil, err
	}

	// Validate the fork.
	if resp.ExecutionPayload != nil && !v4.backend.IsPrague(resp.ExecutionPayload.Timestamp) {
		return nil, ErrUnsupportedFork
	}

	return &GetPayloadV4Result{
		ExecutionPayload:  resp.ExecutionPayload,
		BlockValue:        resp.BlockValue,
		BlobsBundle:       resp.BlobsBundle,
		ShouldOverride:    resp.Override,
		ExecutionRequests: resp.ExecutionRequests,
	}, nil
}

// computePayloadBlockHash computes the expected block hash from the payload
// fields. This is a simplified hash computation for validation purposes.
func computePayloadBlockHash(payload *ExecutionPayloadV3) types.Hash {
	// In a real implementation, this would RLP-encode the block header
	// derived from the payload and compute keccak256. Here we hash key
	// fields to provide basic validation.
	var buf []byte

	buf = append(buf, payload.ParentHash[:]...)
	buf = append(buf, payload.StateRoot[:]...)
	buf = append(buf, payload.ReceiptsRoot[:]...)
	buf = append(buf, payload.PrevRandao[:]...)

	var numBuf [8]byte
	binary.BigEndian.PutUint64(numBuf[:], payload.BlockNumber)
	buf = append(buf, numBuf[:]...)

	binary.BigEndian.PutUint64(numBuf[:], payload.Timestamp)
	buf = append(buf, numBuf[:]...)

	binary.BigEndian.PutUint64(numBuf[:], payload.GasLimit)
	buf = append(buf, numBuf[:]...)

	binary.BigEndian.PutUint64(numBuf[:], payload.GasUsed)
	buf = append(buf, numBuf[:]...)

	if payload.BaseFeePerGas != nil {
		buf = append(buf, payload.BaseFeePerGas.Bytes()...)
	}

	buf = append(buf, payload.FeeRecipient[:]...)
	buf = append(buf, payload.ExtraData...)
	buf = append(buf, payload.LogsBloom[:]...)

	for _, txBytes := range payload.Transactions {
		buf = append(buf, txBytes...)
	}

	return crypto.Keccak256Hash(buf)
}

// BuildExecutionRequestsList constructs the ordered execution requests
// byte slice list from individual request sets. Each entry is prefixed
// with its type byte.
func BuildExecutionRequestsList(
	deposits []DepositRequest,
	withdrawals []WithdrawalRequest,
	consolidations []ConsolidationRequest,
) [][]byte {
	var result [][]byte

	// Type 0x00: Deposits.
	if len(deposits) > 0 {
		var payload []byte
		for i := range deposits {
			payload = append(payload, EncodeDepositRequest(&deposits[i])...)
		}
		result = append(result, append([]byte{DepositRequestType}, payload...))
	}

	// Type 0x01: Withdrawal requests.
	if len(withdrawals) > 0 {
		var payload []byte
		for i := range withdrawals {
			payload = append(payload, EncodeWithdrawalRequest(&withdrawals[i])...)
		}
		result = append(result, append([]byte{WithdrawalRequestType}, payload...))
	}

	// Type 0x02: Consolidation requests.
	if len(consolidations) > 0 {
		var payload []byte
		for i := range consolidations {
			payload = append(payload, EncodeConsolidationRequest(&consolidations[i])...)
		}
		result = append(result, append([]byte{ConsolidationRequestType}, payload...))
	}

	return result
}

// ExecutionRequestsHash computes the hash of the execution requests list.
// This is used for inclusion in the block header (EIP-7685).
func ExecutionRequestsHash(requests [][]byte) types.Hash {
	if len(requests) == 0 {
		return types.Hash{}
	}
	var combined []byte
	for _, req := range requests {
		combined = append(combined, req...)
	}
	return crypto.Keccak256Hash(combined)
}

// ClassifyExecutionRequests separates raw request byte slices into typed
// request structures by their type prefix byte.
func ClassifyExecutionRequests(requests [][]byte) (*ExecutionRequestsV4, error) {
	result := &ExecutionRequestsV4{}

	for i, req := range requests {
		if len(req) < 1 {
			return nil, fmt.Errorf("empty request at index %d", i)
		}

		reqType := req[0]
		payload := req[1:]

		switch reqType {
		case DepositRequestType:
			deposits, err := DecodeDepositRequests(payload)
			if err != nil {
				return nil, fmt.Errorf("deposit decode: %w", err)
			}
			result.Deposits = append(result.Deposits, deposits...)

		case WithdrawalRequestType:
			wReqs, err := DecodeWithdrawalRequests(payload)
			if err != nil {
				return nil, fmt.Errorf("withdrawal decode: %w", err)
			}
			result.Withdrawals = append(result.Withdrawals, wReqs...)

		case ConsolidationRequestType:
			cReqs, err := DecodeConsolidationRequests(payload)
			if err != nil {
				return nil, fmt.Errorf("consolidation decode: %w", err)
			}
			result.Consolidations = append(result.Consolidations, cReqs...)

		default:
			// Unknown request types are silently skipped.
		}
	}

	return result, nil
}
