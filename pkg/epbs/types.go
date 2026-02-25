// Package epbs implements Enshrined Proposer-Builder Separation (EIP-7732).
//
// ePBS decouples execution validation from consensus validation by introducing
// in-protocol builders who submit bids and later reveal execution payloads.
// This package defines the EL-side data structures and validation logic.
package epbs

import (
	"math/big"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Constants from EIP-7732.
const (
	// PTC_SIZE is the Payload Timeliness Committee size (2^9 = 512).
	PTC_SIZE = 512

	// MAX_PAYLOAD_ATTESTATIONS is the max payload attestations per beacon block.
	MAX_PAYLOAD_ATTESTATIONS = 4

	// MAX_BLOB_COMMITMENTS_PER_BLOCK is the max blob KZG commitments per block.
	MAX_BLOB_COMMITMENTS_PER_BLOCK = 4096

	// BLSPubkeySize is a 48-byte BLS12-381 public key.
	BLSPubkeySize = 48

	// BLSSignatureSize is a 96-byte BLS12-381 signature.
	BLSSignatureSize = 96
)

// PayloadStatus constants for payload attestation.
const (
	PayloadAbsent   uint8 = 0 // payload not seen
	PayloadPresent  uint8 = 1 // payload was revealed on time
	PayloadWithheld uint8 = 2 // payload was withheld by builder
)

// BuilderIndex is a builder registry index per EIP-7732.
type BuilderIndex uint64

// BLSPubkey is a 48-byte BLS12-381 public key.
type BLSPubkey [BLSPubkeySize]byte

// BLSSignature is a 96-byte BLS12-381 signature.
type BLSSignature [BLSSignatureSize]byte

// BuilderBid is a builder's bid for execution payload construction.
// The builder commits to a payload hash and offers a value to the proposer.
type BuilderBid struct {
	ParentBlockHash        types.Hash    `json:"parentBlockHash"`
	ParentBlockRoot        types.Hash    `json:"parentBlockRoot"`
	BlockHash              types.Hash    `json:"blockHash"`
	PrevRandao             types.Hash    `json:"prevRandao"`
	FeeRecipient           types.Address `json:"feeRecipient"`
	GasLimit               uint64        `json:"gasLimit"`
	BuilderIndex           BuilderIndex  `json:"builderIndex"`
	Slot                   uint64        `json:"slot"`
	Value                  uint64        `json:"value"`            // payment to proposer in Gwei
	ExecutionPayment       uint64        `json:"executionPayment"` // EL payment in Gwei
	BlobKZGCommitments     [][]byte      `json:"blobKzgCommitments"`
	BlobKZGCommitmentsRoot types.Hash    `json:"blobKzgCommitmentsRoot"`
	BuilderPubkey          BLSPubkey     `json:"builderPubkey"` // BLS public key of the builder
}

// SignedBuilderBid wraps a BuilderBid with a BLS signature.
type SignedBuilderBid struct {
	Message   BuilderBid   `json:"message"`
	Signature BLSSignature `json:"signature"`
}

// PayloadEnvelope wraps a revealed execution payload with ePBS metadata.
type PayloadEnvelope struct {
	PayloadRoot        types.Hash   `json:"payloadRoot"`
	BuilderIndex       BuilderIndex `json:"builderIndex"`
	BeaconBlockRoot    types.Hash   `json:"beaconBlockRoot"`
	Slot               uint64       `json:"slot"`
	StateRoot          types.Hash   `json:"stateRoot"`
	BlobKZGCommitments [][]byte     `json:"blobKzgCommitments"`
}

// SignedPayloadEnvelope wraps a PayloadEnvelope with a BLS signature.
type SignedPayloadEnvelope struct {
	Message   PayloadEnvelope `json:"message"`
	Signature BLSSignature    `json:"signature"`
}

// PayloadAttestationData is the data attested to by PTC members.
type PayloadAttestationData struct {
	BeaconBlockRoot types.Hash `json:"beaconBlockRoot"`
	Slot            uint64     `json:"slot"`
	PayloadStatus   uint8      `json:"payloadStatus"`
}

// PayloadAttestation is an aggregated PTC attestation.
type PayloadAttestation struct {
	AggregationBits [PTC_SIZE / 8]byte     `json:"aggregationBits"`
	Data            PayloadAttestationData `json:"data"`
	Signature       BLSSignature           `json:"signature"`
}

// PayloadAttestationMessage is a single PTC member's attestation message.
type PayloadAttestationMessage struct {
	ValidatorIndex uint64                 `json:"validatorIndex"`
	Data           PayloadAttestationData `json:"data"`
	Signature      BLSSignature           `json:"signature"`
}

// BidHash returns a deterministic hash of the bid for signing/verification.
func (bid *BuilderBid) BidHash() types.Hash {
	var data []byte
	data = append(data, bid.ParentBlockHash[:]...)
	data = append(data, bid.ParentBlockRoot[:]...)
	data = append(data, bid.BlockHash[:]...)
	data = append(data, bid.PrevRandao[:]...)
	data = append(data, bid.FeeRecipient[:]...)

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

// IsPayloadStatusValid checks whether a payload status value is valid.
func IsPayloadStatusValid(status uint8) bool {
	return status == PayloadAbsent || status == PayloadPresent || status == PayloadWithheld
}
