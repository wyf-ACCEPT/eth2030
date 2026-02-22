// finality_bls_adapter.go wires real BLS12-381 cryptographic operations into
// the SSF round engine and endgame finality engine, replacing the placeholder
// XOR aggregation with proper BLS signatures, aggregate verification, and
// finality proofs.
package consensus

import (
	"encoding/binary"
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Domain separator for SSF vote signing. This prevents cross-domain replay
// by mixing a unique 4-byte prefix into the vote signing digest.
const DomainSSFVote uint32 = 0x0E000000

// Finality BLS adapter errors.
var (
	ErrBLSAdapterNilVote           = errors.New("finality_bls: nil vote")
	ErrBLSAdapterNoVotes           = errors.New("finality_bls: no votes to aggregate")
	ErrBLSAdapterInvalidSig        = errors.New("finality_bls: invalid signature")
	ErrBLSAdapterNilProof          = errors.New("finality_bls: nil finality proof")
	ErrBLSAdapterNilRound          = errors.New("finality_bls: nil round")
	ErrBLSAdapterRoundNotFinalized = errors.New("finality_bls: round not finalized")
	ErrBLSAdapterEmptyValidatorSet = errors.New("finality_bls: empty validator set")
	ErrBLSAdapterMismatchPubkeys   = errors.New("finality_bls: pubkey count mismatch")
)

// FinalityProof is a compact proof of finality for an SSF round. It contains
// the aggregate BLS signature over the vote digests, a bitfield indicating
// which validators participated, and the associated epoch/slot/root data.
type FinalityProof struct {
	Epoch              uint64
	Slot               uint64
	BlockRoot          types.Hash
	StateRoot          types.Hash
	AggregateSignature [96]byte
	ParticipantBitfield []byte
	ParticipantCount   uint64
	TotalStake         uint64
}

// FinalityBLSAdapter connects the SSF round engine to real BLS12-381 crypto.
// It provides signing, verification, aggregation, and finality proof
// generation using the crypto/bls_aggregate operations.
type FinalityBLSAdapter struct {
	domainBytes [4]byte
}

// NewFinalityBLSAdapter creates a new adapter with the standard SSF vote
// domain separator.
func NewFinalityBLSAdapter() *FinalityBLSAdapter {
	var d [4]byte
	binary.LittleEndian.PutUint32(d[:], DomainSSFVote)
	return &FinalityBLSAdapter{domainBytes: d}
}

// VoteDigest computes the signing digest for an SSF vote. The digest
// includes the domain separator, slot, and block root to ensure
// domain separation and uniqueness.
func (a *FinalityBLSAdapter) VoteDigest(vote *SSFRoundVote) []byte {
	if vote == nil {
		return nil
	}
	// Domain (4) + Slot (8) + BlockRoot (32) = 44 bytes.
	buf := make([]byte, 44)
	copy(buf[0:4], a.domainBytes[:])
	binary.LittleEndian.PutUint64(buf[4:12], vote.Slot)
	copy(buf[12:44], vote.BlockRoot[:])
	return buf
}

// SignVote produces a real BLS12-381 signature over the vote digest using
// the provided secret key scalar. Returns the 96-byte compressed G2 signature.
func (a *FinalityBLSAdapter) SignVote(secretKey *big.Int, vote *SSFRoundVote) [96]byte {
	if vote == nil || secretKey == nil {
		return [96]byte{}
	}
	digest := a.VoteDigest(vote)
	return crypto.BLSSign(secretKey, digest)
}

// VerifyVote verifies a BLS signature on an SSF vote using the validator's
// compressed public key.
func (a *FinalityBLSAdapter) VerifyVote(pubkey [48]byte, vote *SSFRoundVote, sig [96]byte) bool {
	if vote == nil {
		return false
	}
	digest := a.VoteDigest(vote)
	return crypto.BLSVerify(pubkey, digest, sig)
}

// AggregateVoteSignatures aggregates multiple vote signatures into a single
// aggregate BLS signature and produces a participant bitfield.
// The bitfield is indexed by the position in the input slice.
func (a *FinalityBLSAdapter) AggregateVoteSignatures(votes []SSFRoundVote) ([96]byte, []byte, error) {
	if len(votes) == 0 {
		return [96]byte{}, nil, ErrBLSAdapterNoVotes
	}

	sigs := make([][96]byte, len(votes))
	for i := range votes {
		sigs[i] = votes[i].Signature
	}

	aggSig := crypto.AggregateSignatures(sigs)

	// Build bitfield: 1 bit per participant, packed into bytes.
	bitfieldLen := (len(votes) + 7) / 8
	bitfield := make([]byte, bitfieldLen)
	for i := range votes {
		bitfield[i/8] |= 1 << uint(i%8)
	}

	return aggSig, bitfield, nil
}

// VerifyAggregateVotes verifies an aggregate BLS signature against a set
// of public keys and their corresponding votes. All votes must have signed
// the same message (same slot + block root) for fast aggregate verification.
func (a *FinalityBLSAdapter) VerifyAggregateVotes(
	pubkeys [][48]byte,
	votes []SSFRoundVote,
	aggSig [96]byte,
) bool {
	if len(pubkeys) == 0 || len(pubkeys) != len(votes) {
		return false
	}

	// Check if all votes target the same slot and block root (fast aggregate path).
	sameMsgPath := true
	for i := 1; i < len(votes); i++ {
		if votes[i].Slot != votes[0].Slot || votes[i].BlockRoot != votes[0].BlockRoot {
			sameMsgPath = false
			break
		}
	}

	if sameMsgPath {
		// All signed the same message: use FastAggregateVerify.
		digest := a.VoteDigest(&votes[0])
		return crypto.FastAggregateVerify(pubkeys, digest, aggSig)
	}

	// Different messages: use general aggregate verify.
	msgs := make([][]byte, len(votes))
	for i := range votes {
		msgs[i] = a.VoteDigest(&votes[i])
	}
	return crypto.VerifyAggregate(pubkeys, msgs, aggSig)
}

// GenerateFinalityProof produces a FinalityProof from a completed SSF round.
// The round must be finalized. The proof contains the aggregate signature
// and participant bitfield for external verification.
func (a *FinalityBLSAdapter) GenerateFinalityProof(
	round *SSFRound,
	epoch uint64,
	stateRoot types.Hash,
) (*FinalityProof, error) {
	if round == nil {
		return nil, ErrBLSAdapterNilRound
	}
	if !round.Finalized {
		return nil, ErrBLSAdapterRoundNotFinalized
	}

	// Collect all votes for the winning block root.
	var votes []SSFRoundVote
	for _, v := range round.Votes {
		if v.BlockRoot == round.BlockRoot {
			votes = append(votes, *v)
		}
	}

	if len(votes) == 0 {
		return nil, ErrBLSAdapterNoVotes
	}

	aggSig, bitfield, err := a.AggregateVoteSignatures(votes)
	if err != nil {
		return nil, err
	}

	var totalStake uint64
	for _, v := range votes {
		totalStake += v.Stake
	}

	return &FinalityProof{
		Epoch:               epoch,
		Slot:                round.Slot,
		BlockRoot:           round.BlockRoot,
		StateRoot:           stateRoot,
		AggregateSignature:  aggSig,
		ParticipantBitfield: bitfield,
		ParticipantCount:    uint64(len(votes)),
		TotalStake:          totalStake,
	}, nil
}

// VerifyFinalityProof verifies that a finality proof is valid given the
// validator set. It checks that the aggregate signature verifies against
// the participating validators' public keys.
func (a *FinalityBLSAdapter) VerifyFinalityProof(
	proof *FinalityProof,
	validatorPubkeys [][48]byte,
) bool {
	if proof == nil || len(validatorPubkeys) == 0 {
		return false
	}
	if proof.ParticipantCount == 0 {
		return false
	}

	// Extract participating pubkeys from bitfield.
	var participantPKs [][48]byte
	for i := 0; i < len(validatorPubkeys); i++ {
		byteIdx := i / 8
		bitIdx := uint(i % 8)
		if byteIdx < len(proof.ParticipantBitfield) &&
			proof.ParticipantBitfield[byteIdx]&(1<<bitIdx) != 0 {
			participantPKs = append(participantPKs, validatorPubkeys[i])
		}
	}

	if len(participantPKs) == 0 {
		return false
	}

	// Build the vote digest that all participants signed.
	vote := &SSFRoundVote{
		Slot:      proof.Slot,
		BlockRoot: proof.BlockRoot,
	}
	digest := a.VoteDigest(vote)

	// Verify aggregate signature.
	return crypto.FastAggregateVerify(participantPKs, digest, proof.AggregateSignature)
}

// SignAndAttachVote is a convenience function that signs a vote with the
// given secret key and returns a copy of the vote with the signature
// field populated.
func (a *FinalityBLSAdapter) SignAndAttachVote(secretKey *big.Int, vote SSFRoundVote) SSFRoundVote {
	vote.Signature = a.SignVote(secretKey, &vote)
	return vote
}

// ComputeAggregatePublicKey aggregates a list of BLS public keys into a
// single aggregate key, suitable for verifying a fast aggregate signature.
func ComputeAggregatePublicKey(pubkeys [][48]byte) [48]byte {
	return crypto.AggregatePublicKeys(pubkeys)
}

// bitfieldCount returns the number of set bits in a participant bitfield.
func bitfieldCount(bitfield []byte) uint64 {
	var count uint64
	for _, b := range bitfield {
		for b != 0 {
			count++
			b &= b - 1
		}
	}
	return count
}

// ProofMeetsThreshold checks whether a finality proof has sufficient
// participation stake relative to totalStake and the given numerator/
// denominator threshold (e.g. 2/3).
func ProofMeetsThreshold(proof *FinalityProof, totalStake, threshNum, threshDen uint64) bool {
	if proof == nil || totalStake == 0 || threshDen == 0 {
		return false
	}
	// proof.TotalStake * den >= totalStake * num
	return proof.TotalStake*threshDen >= totalStake*threshNum
}

// GenerateVoteKeyPair is a helper that derives a BLS public key from a
// secret scalar. Useful for test setup.
func GenerateVoteKeyPair(secret *big.Int) ([48]byte, *big.Int) {
	pk := crypto.BLSPubkeyFromSecret(secret)
	return pk, secret
}
