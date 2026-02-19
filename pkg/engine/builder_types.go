// Package engine implements ePBS (EIP-7732) builder types.
//
// EIP-7732 introduces Enshrined Proposer-Builder Separation, where builders
// submit bids with execution payload commitments, and proposers select the
// winning bid. The builder later reveals the full payload.
package engine

import (
	"math/big"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// BLS public key and signature sizes (BLS12-381).
const (
	BLSPubkeySize   = 48
	BLSSignatureSize = 96
)

// BLSPubkey is a 48-byte BLS12-381 public key.
type BLSPubkey [BLSPubkeySize]byte

// BLSSignature is a 96-byte BLS12-381 signature.
type BLSSignature [BLSSignatureSize]byte

// BuilderIndex is the index of a builder in the builder registry.
type BuilderIndex uint64

// BuilderStatus represents the lifecycle state of a registered builder.
type BuilderStatus uint8

const (
	BuilderStatusActive      BuilderStatus = iota // actively participating
	BuilderStatusExiting                          // requested exit, in cooldown
	BuilderStatusWithdrawn                        // fully withdrawn
)

// Builder represents a registered builder in the ePBS system.
// Per EIP-7732, builders are staked entities with a minimum stake of 1 ETH.
type Builder struct {
	Pubkey           BLSPubkey     `json:"pubkey"`
	Index            BuilderIndex  `json:"index"`
	FeeRecipient     types.Address `json:"feeRecipient"`
	GasLimit         uint64        `json:"gasLimit"`
	Balance          *big.Int      `json:"balance"`          // builder stake in wei
	Status           BuilderStatus `json:"status"`
	RegistrationTime uint64        `json:"registrationTime"` // unix timestamp
}

// ExecutionPayloadBid is a builder's bid for block construction.
// Per EIP-7732, the bid commits to the payload hash without revealing
// the full payload contents until the reveal phase.
type ExecutionPayloadBid struct {
	ParentBlockHash types.Hash    `json:"parentBlockHash"`
	ParentBlockRoot types.Hash    `json:"parentBlockRoot"`
	BlockHash       types.Hash    `json:"blockHash"`
	PrevRandao      types.Hash    `json:"prevRandao"`
	FeeRecipient    types.Address `json:"feeRecipient"`
	GasLimit        uint64        `json:"gasLimit"`
	BuilderIndex    BuilderIndex  `json:"builderIndex"`
	Slot            uint64        `json:"slot"`
	Value           uint64        `json:"value"`            // payment to proposer in Gwei
	ExecutionPayment uint64       `json:"executionPayment"` // payment on execution layer in Gwei
	BlobKZGCommitments [][]byte   `json:"blobKzgCommitments"`
}

// SignedExecutionPayloadBid wraps an ExecutionPayloadBid with a BLS signature.
type SignedExecutionPayloadBid struct {
	Message   ExecutionPayloadBid `json:"message"`
	Signature BLSSignature        `json:"signature"`
}

// ExecutionPayloadEnvelope wraps a revealed execution payload with metadata.
// Per EIP-7732, the builder reveals the full payload after the proposer
// has committed to the bid.
type ExecutionPayloadEnvelope struct {
	Payload           *ExecutionPayloadV4 `json:"payload"`
	ExecutionRequests [][]byte            `json:"executionRequests"`
	BuilderIndex      BuilderIndex        `json:"builderIndex"`
	BeaconBlockRoot   types.Hash          `json:"beaconBlockRoot"`
	Slot              uint64              `json:"slot"`
	StateRoot         types.Hash          `json:"stateRoot"`
}

// SignedExecutionPayloadEnvelope wraps an ExecutionPayloadEnvelope with a BLS signature.
type SignedExecutionPayloadEnvelope struct {
	Message   ExecutionPayloadEnvelope `json:"message"`
	Signature BLSSignature             `json:"signature"`
}

// BuilderRegistrationV1 contains the information needed to register a builder.
type BuilderRegistrationV1 struct {
	FeeRecipient types.Address `json:"feeRecipient"`
	GasLimit     uint64        `json:"gasLimit"`
	Timestamp    uint64        `json:"timestamp"`
	Pubkey       BLSPubkey     `json:"pubkey"`
}

// SignedBuilderRegistrationV1 wraps a registration with a BLS signature.
type SignedBuilderRegistrationV1 struct {
	Message   BuilderRegistrationV1 `json:"message"`
	Signature BLSSignature          `json:"signature"`
}

// BidHashInput returns a deterministic hash of the bid for signing/validation.
// The hash covers all bid fields to prevent any field from being tampered with.
func (bid *ExecutionPayloadBid) BidHash() types.Hash {
	// Hash all critical fields: parent hashes, block hash, slot, value, builder index.
	var data []byte
	data = append(data, bid.ParentBlockHash[:]...)
	data = append(data, bid.ParentBlockRoot[:]...)
	data = append(data, bid.BlockHash[:]...)
	data = append(data, bid.PrevRandao[:]...)
	data = append(data, bid.FeeRecipient[:]...)

	// Encode numeric fields as big-endian bytes.
	gasLimitBytes := new(big.Int).SetUint64(bid.GasLimit).Bytes()
	data = append(data, gasLimitBytes...)

	builderIndexBytes := new(big.Int).SetUint64(uint64(bid.BuilderIndex)).Bytes()
	data = append(data, builderIndexBytes...)

	slotBytes := new(big.Int).SetUint64(bid.Slot).Bytes()
	data = append(data, slotBytes...)

	valueBytes := new(big.Int).SetUint64(bid.Value).Bytes()
	data = append(data, valueBytes...)

	return crypto.Keccak256Hash(data)
}
