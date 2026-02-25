package light

import (
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Sync committee constants matching the Ethereum beacon chain specification.
const (
	// SlotsPerSyncCommitteePeriod is the number of slots in one sync committee
	// period: EPOCHS_PER_SYNC_COMMITTEE_PERIOD * SLOTS_PER_EPOCH = 256 * 32.
	SlotsPerSyncCommitteePeriod = 8192

	// EpochsPerSyncCommitteePeriod is the number of epochs per committee period.
	EpochsPerSyncCommitteePeriod = 256

	// MinSyncCommitteeParticipants is the minimum number of participants
	// required for a valid sync committee signature (1 in spec, but we
	// enforce supermajority in practice).
	MinSyncCommitteeParticipants = 1
)

// Sync committee errors.
var (
	ErrNilCommittee              = errors.New("light: nil sync committee")
	ErrCommitteeWrongSize        = errors.New("light: sync committee must have 512 pubkeys")
	ErrNilUpdate                 = errors.New("light: nil light client update")
	ErrInvalidSignature          = errors.New("light: invalid sync committee signature")
	ErrInsufficientParticipation = errors.New("light: insufficient sync committee participation")
	ErrNilBootstrap              = errors.New("light: nil bootstrap data")
	ErrBootstrapMismatch         = errors.New("light: bootstrap header root mismatch")
	ErrUpdateNotNewer            = errors.New("light: update does not advance finalized state")
)

// SyncCommitteePeriod computes the sync committee period index for a given slot.
// The committee serving slot S is period = S / SlotsPerSyncCommitteePeriod.
func SyncCommitteePeriod(slot uint64) uint64 {
	return slot / SlotsPerSyncCommitteePeriod
}

// SyncCommitteePeriodStartSlot returns the first slot of the given period.
func SyncCommitteePeriodStartSlot(period uint64) uint64 {
	return period * SlotsPerSyncCommitteePeriod
}

// ComputeCommitteeRoot computes a commitment to the sync committee's pubkeys
// by hashing the concatenation of all public keys.
func ComputeCommitteeRoot(pubkeys [][]byte) types.Hash {
	var data []byte
	for _, pk := range pubkeys {
		data = append(data, pk...)
	}
	return crypto.Keccak256Hash(data)
}

// VerifySyncCommitteeSignature verifies the aggregate BLS signature from a
// sync committee using BLS12-381 aggregate signature verification.
// The signing message is: signing_root || committee_bits || committee_root.
// Returns nil on success, or an error describing the failure.
func VerifySyncCommitteeSignature(
	committee *SyncCommittee,
	signingRoot types.Hash,
	committeeBits []byte,
	signature []byte,
) error {
	if committee == nil {
		return ErrNilCommittee
	}
	if len(committee.Pubkeys) != SyncCommitteeSize {
		return ErrCommitteeWrongSize
	}

	// Count participating validators.
	participantCount := countBits(committeeBits)
	if participantCount < MinSyncCommitteeParticipants {
		return ErrInsufficientParticipation
	}

	// Check supermajority (2/3 of committee).
	if participantCount*3 < SyncCommitteeSize*2 {
		return ErrInsufficientParticipation
	}

	// Compute the committee root for binding.
	committeeRoot := ComputeCommitteeRoot(committee.Pubkeys)

	// Build the signing message.
	msg := make([]byte, 0, 32+len(committeeBits)+32)
	msg = append(msg, signingRoot[:]...)
	msg = append(msg, committeeBits...)
	msg = append(msg, committeeRoot[:]...)

	// Collect participating pubkeys based on committee bits.
	var participantPubkeys [][]byte
	for i := 0; i < SyncCommitteeSize; i++ {
		if i/8 < len(committeeBits) && committeeBits[i/8]&(1<<(uint(i)%8)) != 0 {
			participantPubkeys = append(participantPubkeys, committee.Pubkeys[i])
		}
	}

	if len(participantPubkeys) == 0 {
		return ErrInsufficientParticipation
	}

	// Verify using BLS FastAggregateVerify.
	if len(signature) != crypto.BLSSignatureSize {
		return ErrInvalidSignature
	}
	if !crypto.DefaultBLSBackend().FastAggregateVerify(participantPubkeys, msg, signature) {
		return ErrInvalidSignature
	}
	return nil
}

// SignSyncCommittee creates a BLS aggregate sync committee signature.
// Each participating committee member signs the message with their
// secret key (derived from committee.SecretKeys if available).
func SignSyncCommittee(
	committee *SyncCommittee,
	signingRoot types.Hash,
	committeeBits []byte,
) []byte {
	committeeRoot := ComputeCommitteeRoot(committee.Pubkeys)
	msg := make([]byte, 0, 32+len(committeeBits)+32)
	msg = append(msg, signingRoot[:]...)
	msg = append(msg, committeeBits...)
	msg = append(msg, committeeRoot[:]...)

	// Collect individual signatures from participating members.
	var sigs [][crypto.BLSSignatureSize]byte
	for i := 0; i < SyncCommitteeSize; i++ {
		if i/8 < len(committeeBits) && committeeBits[i/8]&(1<<(uint(i)%8)) != 0 {
			if i < len(committee.SecretKeys) && committee.SecretKeys[i] != nil {
				sig := crypto.BLSSign(committee.SecretKeys[i], msg)
				sigs = append(sigs, sig)
			}
		}
	}

	// Aggregate the individual signatures.
	aggSig := crypto.AggregateSignatures(sigs)
	return aggSig[:]
}

// NextSyncCommittee derives a deterministic next sync committee from the
// current committee. Uses real BLS keypairs derived from the next period.
func NextSyncCommittee(current *SyncCommittee) (*SyncCommittee, error) {
	if current == nil {
		return nil, ErrNilCommittee
	}
	if len(current.Pubkeys) != SyncCommitteeSize {
		return nil, ErrCommitteeWrongSize
	}

	nextPeriod := current.Period + 1
	nextPubkeys := make([][]byte, SyncCommitteeSize)
	nextSecretKeys := make([]*big.Int, SyncCommitteeSize)
	for i := 0; i < SyncCommitteeSize; i++ {
		// Derive secret key from next period and index.
		sk := new(big.Int).SetUint64(nextPeriod*uint64(SyncCommitteeSize) + uint64(i) + 1)
		nextSecretKeys[i] = sk
		pk := crypto.BLSPubkeyFromSecret(sk)
		nextPubkeys[i] = pk[:]
	}

	// Compute aggregate pubkey using BLS aggregation.
	aggPK := crypto.AggregatePublicKeys(bls48SlicesTo48Arrays(nextPubkeys))
	aggPKSlice := aggPK[:]

	return &SyncCommittee{
		Pubkeys:         nextPubkeys,
		AggregatePubkey: aggPKSlice,
		Period:          nextPeriod,
		SecretKeys:      nextSecretKeys,
	}, nil
}

// SyncCommitteeUpdate processes a sync committee rotation. It validates
// the next committee and returns the updated committee state.
func SyncCommitteeUpdate(current *SyncCommittee, next *SyncCommittee) (*SyncCommittee, error) {
	if current == nil || next == nil {
		return nil, ErrNilCommittee
	}
	if len(next.Pubkeys) != SyncCommitteeSize {
		return nil, ErrCommitteeWrongSize
	}
	if next.Period != current.Period+1 {
		return nil, errors.New("light: next committee period must be current+1")
	}
	return next, nil
}

// LightClientBootstrap contains the data needed to initialize a light client
// from a trusted finalized checkpoint.
type LightClientBootstrap struct {
	Header           *types.Header
	CurrentCommittee *SyncCommittee
	CommitteeRoot    types.Hash
}

// LightClientIncrementalUpdate carries data for advancing the light client
// from one finalized header to the next.
type LightClientIncrementalUpdate struct {
	AttestedHeader    *types.Header
	FinalizedHeader   *types.Header
	SyncCommitteeBits []byte
	Signature         []byte
	FinalityBranch    []types.Hash // Merkle proof of finality
	NextSyncCommittee *SyncCommittee
}

// ProcessBootstrap initializes a LightClientState from a bootstrap packet.
// The trusted root is used to validate the bootstrap header's state root.
func ProcessBootstrap(bootstrap *LightClientBootstrap, trustedRoot types.Hash) (*LightClientState, error) {
	if bootstrap == nil {
		return nil, ErrNilBootstrap
	}
	if bootstrap.Header == nil {
		return nil, ErrNoFinalizedHdr
	}
	if bootstrap.CurrentCommittee == nil {
		return nil, ErrNilCommittee
	}
	if len(bootstrap.CurrentCommittee.Pubkeys) != SyncCommitteeSize {
		return nil, ErrCommitteeWrongSize
	}

	// Verify the committee root matches.
	computedRoot := ComputeCommitteeRoot(bootstrap.CurrentCommittee.Pubkeys)
	if computedRoot != bootstrap.CommitteeRoot {
		return nil, ErrBootstrapMismatch
	}

	// Verify the trusted root matches the header state root.
	if !trustedRoot.IsZero() && bootstrap.Header.Root != trustedRoot {
		return nil, ErrBootstrapMismatch
	}

	var slot uint64
	if bootstrap.Header.Number != nil {
		slot = bootstrap.Header.Number.Uint64()
	}

	return &LightClientState{
		CurrentSlot:      slot,
		FinalizedHeader:  bootstrap.Header,
		CurrentCommittee: bootstrap.CurrentCommittee,
	}, nil
}

// ProcessIncrementalUpdate validates and applies an incremental light client
// update, advancing the finalized header and potentially rotating the sync
// committee.
func ProcessIncrementalUpdate(
	state *LightClientState,
	update *LightClientIncrementalUpdate,
) error {
	if update == nil {
		return ErrNilUpdate
	}
	if update.AttestedHeader == nil || update.FinalizedHeader == nil {
		return ErrNoFinalizedHdr
	}
	if state.CurrentCommittee == nil {
		return ErrNilCommittee
	}

	// Verify finalized header does not exceed attested header.
	if update.FinalizedHeader.Number != nil && update.AttestedHeader.Number != nil {
		if update.FinalizedHeader.Number.Cmp(update.AttestedHeader.Number) > 0 {
			return ErrNotFinalized
		}
	}

	// The finalized header must advance state (or at least not regress).
	if state.FinalizedHeader != nil && update.FinalizedHeader.Number != nil {
		if state.FinalizedHeader.Number != nil {
			if update.FinalizedHeader.Number.Cmp(state.FinalizedHeader.Number) < 0 {
				return ErrUpdateNotNewer
			}
		}
	}

	// Verify the sync committee signature.
	signingRoot := update.AttestedHeader.Hash()
	if err := VerifySyncCommitteeSignature(
		state.CurrentCommittee,
		signingRoot,
		update.SyncCommitteeBits,
		update.Signature,
	); err != nil {
		return err
	}

	// Update state.
	state.FinalizedHeader = update.FinalizedHeader
	if update.AttestedHeader.Number != nil {
		state.CurrentSlot = update.AttestedHeader.Number.Uint64()
	}

	// Rotate committee if a next committee is provided.
	if update.NextSyncCommittee != nil {
		rotated, err := SyncCommitteeUpdate(state.CurrentCommittee, update.NextSyncCommittee)
		if err != nil {
			return err
		}
		state.CurrentCommittee = rotated
	}

	return nil
}

// MakeTestSyncCommittee creates a sync committee with real BLS keypairs
// for testing purposes. Secret keys are deterministic based on period and index.
func MakeTestSyncCommittee(period uint64) *SyncCommittee {
	pubkeys := make([][]byte, SyncCommitteeSize)
	secretKeys := make([]*big.Int, SyncCommitteeSize)
	for i := 0; i < SyncCommitteeSize; i++ {
		// Derive a deterministic secret key from period and index.
		// Use i+1 to avoid zero secret key.
		sk := new(big.Int).SetUint64(period*uint64(SyncCommitteeSize) + uint64(i) + 1)
		secretKeys[i] = sk
		pk := crypto.BLSPubkeyFromSecret(sk)
		pubkeys[i] = pk[:]
	}

	// Compute aggregate pubkey using BLS aggregation.
	aggPK := crypto.AggregatePublicKeys(bls48SlicesTo48Arrays(pubkeys))
	aggPKSlice := aggPK[:]

	return &SyncCommittee{
		Pubkeys:         pubkeys,
		AggregatePubkey: aggPKSlice,
		Period:          period,
		SecretKeys:      secretKeys,
	}
}

// bls48SlicesTo48Arrays converts [][]byte to [][48]byte for BLS operations.
func bls48SlicesTo48Arrays(pubkeys [][]byte) [][48]byte {
	result := make([][48]byte, len(pubkeys))
	for i, pk := range pubkeys {
		if len(pk) >= 48 {
			copy(result[i][:], pk[:48])
		}
	}
	return result
}

// countBits returns the number of set bits in a byte slice.
func countBits(data []byte) int {
	count := 0
	for _, b := range data {
		for i := 0; i < 8; i++ {
			if b&(1<<uint(i)) != 0 {
				count++
			}
		}
	}
	return count
}
