package light

// BLS-based sync committee signature verifier for light client security.
//
// Replaces the simplified Keccak256-based sync committee verification
// with real BLS12-381 aggregate signature verification using the
// FastAggregateVerify scheme.
//
// In the beacon chain, sync committee members sign the block root with
// their BLS keys. The aggregate signature is verified against the
// participating members' public keys.

import (
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/crypto"
)

// BLS verifier errors.
var (
	ErrBLSInvalidPubkey    = errors.New("light: invalid BLS public key")
	ErrBLSInvalidSignature = errors.New("light: invalid BLS signature")
	ErrBLSVerifyFailed     = errors.New("light: BLS aggregate signature verification failed")
	ErrBLSNoParticipants   = errors.New("light: no participating sync committee members")
	ErrBLSQuorumNotMet     = errors.New("light: sync committee quorum not met (need 2/3)")
)

// MinQuorumNumerator and MinQuorumDenominator define the minimum
// participation threshold: at least 2/3 of the committee must sign.
const (
	MinQuorumNumerator   = 2
	MinQuorumDenominator = 3
)

// SyncCommitteeBLSVerifier verifies sync committee signatures using
// real BLS12-381 aggregate signature verification.
type SyncCommitteeBLSVerifier struct {
	// committeeSize is the expected number of members in the sync committee.
	committeeSize int

	// participationRate stores the last verified participation rate.
	participationRate float64

	// totalVerified counts the total number of successfully verified updates.
	totalVerified uint64

	// totalFailed counts the total number of failed verifications.
	totalFailed uint64
}

// NewSyncCommitteeBLSVerifier creates a new BLS-based sync committee verifier.
func NewSyncCommitteeBLSVerifier() *SyncCommitteeBLSVerifier {
	return &SyncCommitteeBLSVerifier{
		committeeSize: SyncCommitteeSize,
	}
}

// NewSyncCommitteeBLSVerifierWithSize creates a verifier with a custom
// committee size (useful for testing with smaller committees).
func NewSyncCommitteeBLSVerifierWithSize(size int) *SyncCommitteeBLSVerifier {
	return &SyncCommitteeBLSVerifier{
		committeeSize: size,
	}
}

// VerifySyncCommitteeSignature verifies a BLS aggregate signature from
// the sync committee.
//
// Parameters:
//   - committee: the 48-byte BLS public keys of all committee members
//   - participationBits: bitfield indicating which members signed
//   - msg: the signing root (typically the beacon block root)
//   - sig: the 96-byte BLS aggregate signature
//
// The function checks:
//  1. Committee size matches expected size
//  2. Participation meets the 2/3 quorum threshold
//  3. BLS FastAggregateVerify succeeds for participating members
func (v *SyncCommitteeBLSVerifier) VerifySyncCommitteeSignature(
	committee [][48]byte,
	participationBits []byte,
	msg []byte,
	sig [96]byte,
) bool {
	// Validate committee size.
	if len(committee) != v.committeeSize {
		v.totalFailed++
		return false
	}

	// Extract participating public keys.
	participants := extractParticipants(committee, participationBits)
	if len(participants) == 0 {
		v.totalFailed++
		return false
	}

	// Check quorum: need at least 2/3 of committee to sign.
	if !meetsQuorum(len(participants), v.committeeSize) {
		v.totalFailed++
		return false
	}

	// Update participation rate.
	v.participationRate = float64(len(participants)) / float64(v.committeeSize)

	// Verify the BLS aggregate signature.
	if !crypto.FastAggregateVerify(participants, msg, sig) {
		v.totalFailed++
		return false
	}

	v.totalVerified++
	return true
}

// ParticipationRate returns the participation rate from the last
// verified signature (0.0 to 1.0).
func (v *SyncCommitteeBLSVerifier) ParticipationRate() float64 {
	return v.participationRate
}

// TotalVerified returns the total number of successfully verified updates.
func (v *SyncCommitteeBLSVerifier) TotalVerified() uint64 {
	return v.totalVerified
}

// TotalFailed returns the total number of failed verification attempts.
func (v *SyncCommitteeBLSVerifier) TotalFailed() uint64 {
	return v.totalFailed
}

// CommitteeSize returns the expected committee size.
func (v *SyncCommitteeBLSVerifier) CommitteeSize() int {
	return v.committeeSize
}

// extractParticipants extracts the public keys of committee members
// whose corresponding participation bit is set.
func extractParticipants(committee [][48]byte, participationBits []byte) [][48]byte {
	var participants [][48]byte
	for i, pk := range committee {
		byteIdx := i / 8
		bitIdx := uint(i % 8)
		if byteIdx < len(participationBits) && participationBits[byteIdx]&(1<<bitIdx) != 0 {
			participants = append(participants, pk)
		}
	}
	return participants
}

// meetsQuorum checks if the participation count meets the 2/3 threshold.
func meetsQuorum(participants, total int) bool {
	if total == 0 {
		return false
	}
	// participants * denominator >= total * numerator
	// avoids floating point: participants * 3 >= total * 2
	return participants*MinQuorumDenominator >= total*MinQuorumNumerator
}

// CountParticipants counts the number of set bits in the participation
// bitfield, representing the number of committee members that signed.
func CountParticipants(participationBits []byte, committeeSize int) int {
	count := 0
	for i := 0; i < committeeSize; i++ {
		byteIdx := i / 8
		bitIdx := uint(i % 8)
		if byteIdx < len(participationBits) && participationBits[byteIdx]&(1<<bitIdx) != 0 {
			count++
		}
	}
	return count
}

// MakeParticipationBits creates a participation bitfield with the first
// n members marked as participating.
func MakeParticipationBits(committeeSize, participants int) []byte {
	bits := make([]byte, (committeeSize+7)/8)
	for i := 0; i < participants && i < committeeSize; i++ {
		bits[i/8] |= 1 << (uint(i) % 8)
	}
	return bits
}

// MakeBLSTestCommittee creates a test sync committee with deterministic
// BLS key pairs. Returns the public keys and corresponding secret keys.
func MakeBLSTestCommittee(size int) ([][48]byte, []*[32]byte) {
	pubkeys := make([][48]byte, size)
	secrets := make([]*[32]byte, size)
	for i := 0; i < size; i++ {
		secret := make([]byte, 32)
		// Deterministic secret: validator index + 1 (avoid zero).
		secret[31] = byte(i + 1)
		if i >= 255 {
			secret[30] = byte((i + 1) >> 8)
		}
		var secretArr [32]byte
		copy(secretArr[:], secret)
		secrets[i] = &secretArr

		sk := new(big.Int).SetBytes(secret)
		pubkeys[i] = crypto.BLSPubkeyFromSecret(sk)
	}
	return pubkeys, secrets
}

// SignSyncCommitteeBLS creates a BLS aggregate signature for a sync
// committee. The participating members (indicated by participationBits)
// each sign the message, and their signatures are aggregated.
func SignSyncCommitteeBLS(
	secrets []*[32]byte,
	participationBits []byte,
	msg []byte,
) [96]byte {
	var sigs [][96]byte
	for i, secret := range secrets {
		byteIdx := i / 8
		bitIdx := uint(i % 8)
		if byteIdx < len(participationBits) && participationBits[byteIdx]&(1<<bitIdx) != 0 {
			sk := new(big.Int).SetBytes(secret[:])
			sig := crypto.BLSSign(sk, msg)
			sigs = append(sigs, sig)
		}
	}
	if len(sigs) == 0 {
		return [96]byte{}
	}
	return crypto.AggregateSignatures(sigs)
}
