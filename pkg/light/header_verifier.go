// header_verifier.go implements beacon chain header verification for the light
// client, including header chain validation, finality proof checking, and sync
// aggregate verification. This is part of the Consensus Layer roadmap for
// fast confirmation and single-slot finality.
package light

import (
	"encoding/binary"
	"errors"

	"github.com/eth2030/eth2030/crypto"
)

// Header verifier errors.
var (
	ErrVerifierNilHeader         = errors.New("header_verifier: nil header")
	ErrVerifierEmptyChain        = errors.New("header_verifier: empty header chain")
	ErrVerifierParentMismatch    = errors.New("header_verifier: parent root mismatch")
	ErrVerifierSlotNotIncreasing = errors.New("header_verifier: slot not increasing")
	ErrVerifierNilFinalityProof  = errors.New("header_verifier: nil finality branch")
	ErrVerifierFinalityMismatch  = errors.New("header_verifier: finality proof verification failed")
	ErrVerifierNilAggregate      = errors.New("header_verifier: nil sync aggregate")
	ErrVerifierNilCommittee      = errors.New("header_verifier: nil sync committee")
	ErrVerifierSignatureFailed   = errors.New("header_verifier: sync aggregate signature verification failed")
	ErrVerifierInsufficientPart  = errors.New("header_verifier: insufficient participation (need >= 2/3)")
	ErrVerifierCommitteeEmpty    = errors.New("header_verifier: committee has no pubkeys")
	ErrVerifierDepthExceeded     = errors.New("header_verifier: chain exceeds max verification depth")
)

// FinalityBranchDepth is the depth of the finality branch Merkle proof
// in the beacon state. Per the Altair spec this is 6 levels.
const FinalityBranchDepth = 6

// FinalityBranchIndex is the generalized index of the finalized_checkpoint
// in the beacon state tree. For Altair this is index 105.
const FinalityBranchIndex = 105

// LightHeader represents a beacon chain block header for light client use.
// It contains the minimal fields needed for header chain verification.
type LightHeader struct {
	// Slot is the beacon chain slot number.
	Slot uint64

	// ProposerIndex identifies which validator proposed this block.
	ProposerIndex uint64

	// ParentRoot is the hash tree root of the parent beacon block.
	ParentRoot [32]byte

	// StateRoot is the hash tree root of the beacon state after this block.
	StateRoot [32]byte

	// BodyRoot is the hash tree root of the beacon block body.
	BodyRoot [32]byte
}

// HashTreeRoot computes the hash tree root of the light header by hashing
// the concatenation of its fields. In a full SSZ implementation this would
// use proper Merkleization; here we use Keccak256 as a stand-in.
func (h *LightHeader) HashTreeRoot() [32]byte {
	if h == nil {
		return [32]byte{}
	}
	var slotBuf [8]byte
	binary.LittleEndian.PutUint64(slotBuf[:], h.Slot)

	var proposerBuf [8]byte
	binary.LittleEndian.PutUint64(proposerBuf[:], h.ProposerIndex)

	data := make([]byte, 0, 8+8+32+32+32)
	data = append(data, slotBuf[:]...)
	data = append(data, proposerBuf[:]...)
	data = append(data, h.ParentRoot[:]...)
	data = append(data, h.StateRoot[:]...)
	data = append(data, h.BodyRoot[:]...)

	hash := crypto.Keccak256(data)
	var result [32]byte
	copy(result[:], hash)
	return result
}

// SyncAggregate contains the sync committee's aggregate signature over a
// beacon block root. The SyncCommitteeBits bitfield indicates which committee
// members participated in signing.
type SyncAggregate struct {
	// SyncCommitteeBits is a bitfield where each bit indicates whether
	// the corresponding sync committee member signed.
	SyncCommitteeBits []byte

	// Signature is the BLS aggregate signature over the signing root.
	// In production this is a 96-byte BLS signature; here we use
	// Keccak256-based commitments for deterministic testing.
	Signature [96]byte
}

// ParticipationCount returns the number of set bits in the committee bitfield.
func (sa *SyncAggregate) ParticipationCount() int {
	if sa == nil {
		return 0
	}
	count := 0
	for _, b := range sa.SyncCommitteeBits {
		for i := 0; i < 8; i++ {
			if b&(1<<uint(i)) != 0 {
				count++
			}
		}
	}
	return count
}

// VerifierSyncCommittee holds the public keys for a sync committee period.
// It wraps the committee data needed for signature verification.
type VerifierSyncCommittee struct {
	// Pubkeys holds the 48-byte BLS public keys for each committee member.
	Pubkeys [][48]byte

	// AggregatePubkey is the aggregate of all committee member public keys.
	AggregatePubkey [48]byte
}

// Size returns the number of members in the committee.
func (c *VerifierSyncCommittee) Size() int {
	if c == nil {
		return 0
	}
	return len(c.Pubkeys)
}

// HeaderVerifier verifies beacon chain headers for light client operation.
// It maintains a trusted header and sync committee state and validates
// incoming header chains, finality proofs, and sync aggregates.
type HeaderVerifier struct {
	// trustedHeader is the most recently verified header.
	trustedHeader *LightHeader

	// syncCommittee is the current sync committee for signature verification.
	syncCommittee *VerifierSyncCommittee

	// verificationDepth limits the maximum chain length that can be verified
	// in a single call to VerifyHeaderChain.
	verificationDepth int
}

// NewHeaderVerifier creates a new HeaderVerifier with the given trusted header,
// sync committee, and maximum verification depth.
func NewHeaderVerifier(
	trusted *LightHeader,
	committee *VerifierSyncCommittee,
	depth int,
) *HeaderVerifier {
	if depth <= 0 {
		depth = 1024
	}
	return &HeaderVerifier{
		trustedHeader:     trusted,
		syncCommittee:     committee,
		verificationDepth: depth,
	}
}

// TrustedHeader returns the current trusted header.
func (hv *HeaderVerifier) TrustedHeader() *LightHeader {
	return hv.trustedHeader
}

// SetTrustedHeader updates the trusted header after successful verification.
func (hv *HeaderVerifier) SetTrustedHeader(header *LightHeader) {
	hv.trustedHeader = header
}

// SyncCommittee returns the current sync committee.
func (hv *HeaderVerifier) SyncCommittee() *VerifierSyncCommittee {
	return hv.syncCommittee
}

// SetSyncCommittee updates the sync committee after a period rotation.
func (hv *HeaderVerifier) SetSyncCommittee(committee *VerifierSyncCommittee) {
	hv.syncCommittee = committee
}

// VerifyHeaderChain verifies that a sequence of headers forms a valid chain
// with correct parent linkage and increasing slot numbers. Each header's
// ParentRoot must equal the HashTreeRoot of the preceding header. The first
// header must link to the current trusted header.
func (hv *HeaderVerifier) VerifyHeaderChain(headers []*LightHeader) error {
	if len(headers) == 0 {
		return ErrVerifierEmptyChain
	}
	if len(headers) > hv.verificationDepth {
		return ErrVerifierDepthExceeded
	}

	// Verify the first header links to the trusted header.
	if hv.trustedHeader != nil {
		trustedRoot := hv.trustedHeader.HashTreeRoot()
		if headers[0].ParentRoot != trustedRoot {
			return ErrVerifierParentMismatch
		}
		if headers[0].Slot <= hv.trustedHeader.Slot {
			return ErrVerifierSlotNotIncreasing
		}
	}

	// Verify each subsequent header links to its predecessor.
	for i := 1; i < len(headers); i++ {
		if headers[i] == nil {
			return ErrVerifierNilHeader
		}
		prevRoot := headers[i-1].HashTreeRoot()
		if headers[i].ParentRoot != prevRoot {
			return ErrVerifierParentMismatch
		}
		if headers[i].Slot <= headers[i-1].Slot {
			return ErrVerifierSlotNotIncreasing
		}
	}

	return nil
}

// VerifyFinalityProof verifies that a finalized header is included in a beacon
// state by checking its finality branch Merkle proof against the attested
// header's state root. The finalityBranch contains sibling hashes from the
// finalized checkpoint leaf up to the state root.
func (hv *HeaderVerifier) VerifyFinalityProof(
	header *LightHeader,
	finalityBranch [][32]byte,
	finalizedRoot [32]byte,
) error {
	if header == nil {
		return ErrVerifierNilHeader
	}
	if len(finalityBranch) == 0 {
		return ErrVerifierNilFinalityProof
	}

	// Compute the leaf hash (the finalized checkpoint root).
	current := finalizedRoot

	// Walk up the Merkle tree using the branch siblings.
	gIndex := uint64(FinalityBranchIndex)
	for _, sibling := range finalityBranch {
		if gIndex%2 == 0 {
			// Current node is a left child.
			combined := append(current[:], sibling[:]...)
			hash := crypto.Keccak256(combined)
			copy(current[:], hash)
		} else {
			// Current node is a right child.
			combined := append(sibling[:], current[:]...)
			hash := crypto.Keccak256(combined)
			copy(current[:], hash)
		}
		gIndex /= 2
	}

	// The resulting root should match the header's state root.
	if current != header.StateRoot {
		return ErrVerifierFinalityMismatch
	}

	return nil
}

// VerifySyncAggregate verifies the sync committee's aggregate signature over
// a signing root. It extracts participating committee members from the bitfield,
// verifies the aggregate signature, and returns the participation count.
//
// In a production implementation this would use BLS FastAggregateVerify.
// Here we use a Keccak256 commitment scheme:
//
//	expected = Keccak256(signingRoot || committeeBits || committeeRoot)
//
// Returns the number of participating committee members and any error.
func (hv *HeaderVerifier) VerifySyncAggregate(
	aggregate *SyncAggregate,
	signingRoot [32]byte,
	committee *VerifierSyncCommittee,
) (int, error) {
	if aggregate == nil {
		return 0, ErrVerifierNilAggregate
	}
	if committee == nil || len(committee.Pubkeys) == 0 {
		return 0, ErrVerifierNilCommittee
	}

	// Count participating members.
	participationCount := aggregate.ParticipationCount()
	if participationCount == 0 {
		return 0, ErrVerifierInsufficientPart
	}

	// Compute committee root for binding.
	committeeRoot := computeVerifierCommitteeRoot(committee)

	// Verify the signature commitment.
	msg := make([]byte, 0, 32+len(aggregate.SyncCommitteeBits)+32)
	msg = append(msg, signingRoot[:]...)
	msg = append(msg, aggregate.SyncCommitteeBits...)
	msg = append(msg, committeeRoot[:]...)
	expected := crypto.Keccak256(msg)

	// Compare against the first 32 bytes of the signature.
	for i := 0; i < 32 && i < len(expected); i++ {
		if aggregate.Signature[i] != expected[i] {
			return 0, ErrVerifierSignatureFailed
		}
	}

	return participationCount, nil
}

// ComputeSigningRoot computes the signing root for a beacon block header
// by combining its hash tree root with a domain value. This is the message
// that sync committee members sign.
//
// signing_root = Keccak256(header_root || domain)
func ComputeSigningRoot(header *LightHeader, domain [32]byte) [32]byte {
	if header == nil {
		return [32]byte{}
	}
	headerRoot := header.HashTreeRoot()
	hash := crypto.Keccak256(headerRoot[:], domain[:])
	var result [32]byte
	copy(result[:], hash)
	return result
}

// CheckSufficientParticipation verifies that the participation count meets
// the 2/3 supermajority threshold required by the beacon chain spec.
// Returns an error if participation is insufficient.
func CheckSufficientParticipation(participationCount, committeeSize int) error {
	if committeeSize == 0 {
		return ErrVerifierCommitteeEmpty
	}
	// participationCount * 3 >= committeeSize * 2
	if participationCount*3 < committeeSize*2 {
		return ErrVerifierInsufficientPart
	}
	return nil
}

// SignSyncAggregate creates a sync aggregate signature for testing.
// It produces a Keccak256 commitment over the signing root, committee bits,
// and committee root.
func SignSyncAggregate(
	signingRoot [32]byte,
	committeeBits []byte,
	committee *VerifierSyncCommittee,
) [96]byte {
	committeeRoot := computeVerifierCommitteeRoot(committee)
	msg := make([]byte, 0, 32+len(committeeBits)+32)
	msg = append(msg, signingRoot[:]...)
	msg = append(msg, committeeBits...)
	msg = append(msg, committeeRoot[:]...)
	hash := crypto.Keccak256(msg)

	var sig [96]byte
	copy(sig[:], hash)
	return sig
}

// MakeVerifierCommitteeBits creates a participation bitfield with the first
// n members marked as participating.
func MakeVerifierCommitteeBits(committeeSize, participants int) []byte {
	bits := make([]byte, (committeeSize+7)/8)
	for i := 0; i < participants && i < committeeSize; i++ {
		bits[i/8] |= 1 << (uint(i) % 8)
	}
	return bits
}

// MakeTestVerifierCommittee creates a test VerifierSyncCommittee with
// deterministic public keys derived from the given size.
func MakeTestVerifierCommittee(size int) *VerifierSyncCommittee {
	pubkeys := make([][48]byte, size)
	for i := 0; i < size; i++ {
		seed := make([]byte, 8)
		binary.LittleEndian.PutUint64(seed, uint64(i))
		hash := crypto.Keccak256(seed)
		copy(pubkeys[i][:], hash)
		// Fill remaining bytes with second hash.
		hash2 := crypto.Keccak256(hash)
		copy(pubkeys[i][32:], hash2[:16])
	}

	// Compute aggregate pubkey.
	var aggData []byte
	for _, pk := range pubkeys {
		aggData = append(aggData, pk[:]...)
	}
	aggHash := crypto.Keccak256(aggData)
	var agg [48]byte
	copy(agg[:], aggHash)
	aggHash2 := crypto.Keccak256(aggHash)
	copy(agg[32:], aggHash2[:16])

	return &VerifierSyncCommittee{
		Pubkeys:         pubkeys,
		AggregatePubkey: agg,
	}
}

// BuildFinalityBranch constructs a finality branch Merkle proof for testing.
// Since hash inversion is impossible, the caller should use
// ComputeFinalityStateRoot to derive the matching state root from the
// returned branch, then set that as the header's StateRoot.
func BuildFinalityBranch(stateRoot [32]byte, finalizedRoot [32]byte, depth int) [][32]byte {
	if depth <= 0 {
		depth = FinalityBranchDepth
	}
	siblings := make([][32]byte, depth)
	for i := 0; i < depth; i++ {
		seed := make([]byte, 0, 72)
		seed = append(seed, stateRoot[:]...)
		seed = append(seed, finalizedRoot[:]...)
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], uint64(i))
		seed = append(seed, buf[:]...)
		hash := crypto.Keccak256(seed)
		copy(siblings[i][:], hash)
	}
	return siblings
}

// ComputeFinalityStateRoot computes the state root that results from verifying
// a finality branch. This is useful for constructing matching test data.
func ComputeFinalityStateRoot(finalizedRoot [32]byte, finalityBranch [][32]byte) [32]byte {
	current := finalizedRoot
	gIndex := uint64(FinalityBranchIndex)
	for _, sibling := range finalityBranch {
		if gIndex%2 == 0 {
			combined := append(current[:], sibling[:]...)
			hash := crypto.Keccak256(combined)
			copy(current[:], hash)
		} else {
			combined := append(sibling[:], current[:]...)
			hash := crypto.Keccak256(combined)
			copy(current[:], hash)
		}
		gIndex /= 2
	}
	return current
}

// computeVerifierCommitteeRoot computes a Keccak256 commitment over all
// public keys in the committee.
func computeVerifierCommitteeRoot(committee *VerifierSyncCommittee) [32]byte {
	if committee == nil || len(committee.Pubkeys) == 0 {
		return [32]byte{}
	}
	var data []byte
	for _, pk := range committee.Pubkeys {
		data = append(data, pk[:]...)
	}
	hash := crypto.Keccak256(data)
	var result [32]byte
	copy(result[:], hash)
	return result
}
