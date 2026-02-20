package consensus

// BLS signature operations for the Ethereum consensus layer.
//
// Implements attestation and proposer BLS signature verification,
// domain separation per the Ethereum beacon chain spec, and
// signing root computation (hash tree root + domain).
//
// Domain types from the beacon chain spec:
//   - DOMAIN_BEACON_PROPOSER  = 0x00000000
//   - DOMAIN_BEACON_ATTESTER  = 0x01000000
//   - DOMAIN_SYNC_COMMITTEE   = 0x07000000

import (
	"crypto/sha256"
	"encoding/binary"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// Domain type constants per the beacon chain specification.
var (
	DomainBeaconProposer  = [4]byte{0x00, 0x00, 0x00, 0x00}
	DomainBeaconAttester  = [4]byte{0x01, 0x00, 0x00, 0x00}
	DomainRandao          = [4]byte{0x02, 0x00, 0x00, 0x00}
	DomainDeposit         = [4]byte{0x03, 0x00, 0x00, 0x00}
	DomainVoluntaryExit   = [4]byte{0x04, 0x00, 0x00, 0x00}
	DomainSelectionProof  = [4]byte{0x05, 0x00, 0x00, 0x00}
	DomainAggregateAndProof = [4]byte{0x06, 0x00, 0x00, 0x00}
	DomainSyncCommittee   = [4]byte{0x07, 0x00, 0x00, 0x00}
	DomainSyncCommitteeSelectionProof = [4]byte{0x08, 0x00, 0x00, 0x00}
	DomainContributionAndProof = [4]byte{0x09, 0x00, 0x00, 0x00}
)

// BeaconBlockHeader represents a beacon block header for signature
// verification purposes. Matches the spec's BeaconBlockHeader container.
type BeaconBlockHeader struct {
	Slot          uint64
	ProposerIndex uint64
	ParentRoot    [32]byte
	StateRoot     [32]byte
	BodyRoot      [32]byte
}

// DomainSeparation computes the signing domain per the beacon chain spec.
//
// The domain is a 32-byte value computed as:
//
//	domain = fork_data_root[:4] || domain_type
//
// where fork_data_root = sha256(fork_version || genesis_validators_root).
//
// Per spec: compute_domain(domain_type, fork_version, genesis_validators_root).
func DomainSeparation(domainType [4]byte, forkVersion [4]byte, genesisRoot [32]byte) [32]byte {
	// Compute the fork data root: sha256(fork_version || zero_padding || genesis_root).
	forkDataRoot := computeForkDataRoot(forkVersion, genesisRoot)

	// Domain = domain_type (4 bytes) || fork_data_root[:28]
	var domain [32]byte
	copy(domain[:4], domainType[:])
	copy(domain[4:], forkDataRoot[:28])
	return domain
}

// computeForkDataRoot computes the hash tree root of the ForkData object:
//
//	ForkData { current_version: Version, genesis_validators_root: Root }
//
// SSZ hash tree root = sha256(current_version_padded || genesis_validators_root).
func computeForkDataRoot(forkVersion [4]byte, genesisRoot [32]byte) [32]byte {
	// Pad fork version to 32 bytes (SSZ fixed-size encoding).
	var versionPadded [32]byte
	copy(versionPadded[:4], forkVersion[:])

	var combined [64]byte
	copy(combined[:32], versionPadded[:])
	copy(combined[32:], genesisRoot[:])
	return sha256.Sum256(combined[:])
}

// ComputeSigningRoot computes the signing root for a given object hash
// and domain. Per the spec:
//
//	signing_root = sha256(object_root || domain)
//
// This is what validators actually sign.
func ComputeSigningRoot(objectRoot [32]byte, domain [32]byte) [32]byte {
	var combined [64]byte
	copy(combined[:32], objectRoot[:])
	copy(combined[32:], domain[:])
	return sha256.Sum256(combined[:])
}

// HashBeaconBlockHeader computes the SSZ hash tree root of a beacon
// block header. The header has 5 fields:
//
//	slot(uint64), proposer_index(uint64), parent_root(Bytes32),
//	state_root(Bytes32), body_root(Bytes32)
//
// SSZ merkleization: 5 leaves padded to 8 (next power of 2).
func HashBeaconBlockHeader(header *BeaconBlockHeader) [32]byte {
	if header == nil {
		return [32]byte{}
	}

	// Encode each field as a 32-byte SSZ leaf.
	var leaves [8][32]byte

	// Slot (uint64, little-endian, zero-padded).
	binary.LittleEndian.PutUint64(leaves[0][:8], header.Slot)

	// ProposerIndex (uint64, little-endian).
	binary.LittleEndian.PutUint64(leaves[1][:8], header.ProposerIndex)

	// ParentRoot (already 32 bytes).
	leaves[2] = header.ParentRoot

	// StateRoot.
	leaves[3] = header.StateRoot

	// BodyRoot.
	leaves[4] = header.BodyRoot

	// Leaves 5-7 are zero (padding to next power of 2).

	// Merkleize: 3 levels for 8 leaves.
	// Level 2: 4 nodes.
	h01 := sha256Hash(leaves[0], leaves[1])
	h23 := sha256Hash(leaves[2], leaves[3])
	h45 := sha256Hash(leaves[4], leaves[5])
	h67 := sha256Hash(leaves[6], leaves[7])

	// Level 1: 2 nodes.
	h0123 := sha256Hash(h01, h23)
	h4567 := sha256Hash(h45, h67)

	// Root.
	return sha256Hash(h0123, h4567)
}

// HashAttestationData computes the SSZ hash tree root of AttestationData.
// Fields: slot(uint64), beacon_block_root(Bytes32), source(Checkpoint),
// target(Checkpoint).
//
// Note: per EIP-7549, the committee index is NOT part of the signed data.
func HashAttestationData(data *AttestationData) [32]byte {
	if data == nil {
		return [32]byte{}
	}

	// 4 fields -> pad to 4 leaves (already power of 2).
	var leaves [4][32]byte

	// Slot.
	binary.LittleEndian.PutUint64(leaves[0][:8], uint64(data.Slot))

	// BeaconBlockRoot.
	leaves[1] = data.BeaconBlockRoot

	// Source checkpoint (hash tree root of checkpoint = sha256(epoch || root)).
	leaves[2] = hashCheckpoint(data.Source)

	// Target checkpoint.
	leaves[3] = hashCheckpoint(data.Target)

	// Merkleize: 2 levels for 4 leaves.
	h01 := sha256Hash(leaves[0], leaves[1])
	h23 := sha256Hash(leaves[2], leaves[3])
	return sha256Hash(h01, h23)
}

// hashCheckpoint computes the SSZ hash tree root of a Checkpoint.
// Checkpoint = { epoch: uint64, root: Bytes32 }.
func hashCheckpoint(cp Checkpoint) [32]byte {
	var epochLeaf [32]byte
	binary.LittleEndian.PutUint64(epochLeaf[:8], uint64(cp.Epoch))
	return sha256Hash(epochLeaf, cp.Root)
}

// sha256Hash combines two 32-byte values using SHA-256.
func sha256Hash(a, b [32]byte) [32]byte {
	var combined [64]byte
	copy(combined[:32], a[:])
	copy(combined[32:], b[:])
	return sha256.Sum256(combined[:])
}

// VerifyAttestationSignature verifies a BLS aggregate signature for
// an attestation. The signing root is computed from the attestation data
// and the beacon attester domain.
//
// Parameters:
//   - pubkeys: BLS public keys of the attesting validators
//   - data: the attestation data being signed
//   - signature: the 96-byte BLS aggregate signature
//   - forkVersion: current fork version
//   - genesisRoot: genesis validators root
//
// Returns true if the aggregate signature is valid.
func VerifyAttestationSignature(
	pubkeys [][48]byte,
	data *AttestationData,
	signature [96]byte,
	forkVersion [4]byte,
	genesisRoot [32]byte,
) bool {
	if len(pubkeys) == 0 || data == nil {
		return false
	}

	// Compute the signing domain for attestations.
	domain := DomainSeparation(DomainBeaconAttester, forkVersion, genesisRoot)

	// Compute the attestation data root.
	dataRoot := HashAttestationData(data)

	// Compute the signing root.
	signingRoot := ComputeSigningRoot(dataRoot, domain)

	// Verify the aggregate signature using FastAggregateVerify
	// (all validators sign the same message).
	return crypto.FastAggregateVerify(pubkeys, signingRoot[:], signature)
}

// VerifyProposerSignature verifies a BLS signature from a block proposer.
// The signing root is computed from the block header and the beacon
// proposer domain.
//
// Parameters:
//   - pubkey: the proposer's BLS public key
//   - header: the beacon block header being signed
//   - signature: the 96-byte BLS signature
//   - forkVersion: current fork version
//   - genesisRoot: genesis validators root
//
// Returns true if the signature is valid.
func VerifyProposerSignature(
	pubkey [48]byte,
	header *BeaconBlockHeader,
	signature [96]byte,
	forkVersion [4]byte,
	genesisRoot [32]byte,
) bool {
	if header == nil {
		return false
	}

	// Compute the signing domain for block proposals.
	domain := DomainSeparation(DomainBeaconProposer, forkVersion, genesisRoot)

	// Compute the block header root.
	headerRoot := HashBeaconBlockHeader(header)

	// Compute the signing root.
	signingRoot := ComputeSigningRoot(headerRoot, domain)

	// Verify single signature.
	return crypto.BLSVerify(pubkey, signingRoot[:], signature)
}

// VerifySyncCommitteeBLSSignature verifies a BLS aggregate signature
// from a sync committee. Uses the sync committee domain for signing.
//
// Parameters:
//   - pubkeys: BLS public keys of participating committee members
//   - blockRoot: the beacon block root being attested
//   - signature: the 96-byte BLS aggregate signature
//   - forkVersion: current fork version
//   - genesisRoot: genesis validators root
//
// Returns true if the aggregate signature is valid.
func VerifySyncCommitteeBLSSignature(
	pubkeys [][48]byte,
	blockRoot types.Hash,
	signature [96]byte,
	forkVersion [4]byte,
	genesisRoot [32]byte,
) bool {
	if len(pubkeys) == 0 {
		return false
	}

	// Compute the signing domain for sync committees.
	domain := DomainSeparation(DomainSyncCommittee, forkVersion, genesisRoot)

	// The object root is the block root itself (already a hash tree root).
	var objectRoot [32]byte
	copy(objectRoot[:], blockRoot[:])

	// Compute the signing root.
	signingRoot := ComputeSigningRoot(objectRoot, domain)

	// Verify the aggregate signature.
	return crypto.FastAggregateVerify(pubkeys, signingRoot[:], signature)
}

// SignWithDomain creates a BLS signature over an object root with the
// given domain. Used by validators to sign blocks, attestations, etc.
func SignWithDomain(
	secret []byte,
	objectRoot [32]byte,
	domain [32]byte,
) [96]byte {
	signingRoot := ComputeSigningRoot(objectRoot, domain)
	sk := new(big.Int).SetBytes(secret)
	return crypto.BLSSign(sk, signingRoot[:])
}
