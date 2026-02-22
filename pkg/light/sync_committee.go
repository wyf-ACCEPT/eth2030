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
// sync committee. In the beacon chain, this would use BLS12-381 aggregate
// signature verification. Here we verify a Keccak256 binding commitment:
//
//	expected = Keccak256(signing_root || committee_bits || committee_root)
//
// The signing_root is typically the hash tree root of the attested header.
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

	// Verify the signature as Keccak256(signing_root || bits || committee_root).
	msg := make([]byte, 0, 32+len(committeeBits)+32)
	msg = append(msg, signingRoot[:]...)
	msg = append(msg, committeeBits...)
	msg = append(msg, committeeRoot[:]...)
	expected := crypto.Keccak256(msg)

	if len(signature) != len(expected) {
		return ErrInvalidSignature
	}
	for i := range expected {
		if signature[i] != expected[i] {
			return ErrInvalidSignature
		}
	}
	return nil
}

// SignSyncCommittee creates a placeholder sync committee signature for testing.
// In production, this would be a BLS aggregate signature.
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
	return crypto.Keccak256(msg)
}

// NextSyncCommittee derives a deterministic next sync committee from the
// current committee. In the real beacon chain, committee selection uses
// the RANDAO mix and validator shuffle. Here we rotate pubkeys and re-hash
// to simulate the transition.
func NextSyncCommittee(current *SyncCommittee) (*SyncCommittee, error) {
	if current == nil {
		return nil, ErrNilCommittee
	}
	if len(current.Pubkeys) != SyncCommitteeSize {
		return nil, ErrCommitteeWrongSize
	}

	nextPubkeys := make([][]byte, SyncCommitteeSize)
	for i, pk := range current.Pubkeys {
		// Derive next key: Keccak256(current_key || period+1).
		seed := make([]byte, len(pk)+8)
		copy(seed, pk)
		nextPeriod := current.Period + 1
		seed[len(pk)] = byte(nextPeriod >> 56)
		seed[len(pk)+1] = byte(nextPeriod >> 48)
		seed[len(pk)+2] = byte(nextPeriod >> 40)
		seed[len(pk)+3] = byte(nextPeriod >> 32)
		seed[len(pk)+4] = byte(nextPeriod >> 24)
		seed[len(pk)+5] = byte(nextPeriod >> 16)
		seed[len(pk)+6] = byte(nextPeriod >> 8)
		seed[len(pk)+7] = byte(nextPeriod)
		nextPubkeys[i] = crypto.Keccak256(seed)
	}

	// Compute aggregate pubkey for the next committee.
	aggPK := crypto.Keccak256(nextPubkeys[0])
	for i := 1; i < len(nextPubkeys); i++ {
		combined := append(aggPK, nextPubkeys[i]...)
		aggPK = crypto.Keccak256(combined)
	}

	return &SyncCommittee{
		Pubkeys:         nextPubkeys,
		AggregatePubkey: aggPK,
		Period:          current.Period + 1,
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

// MakeTestSyncCommittee creates a sync committee with deterministic pubkeys
// for testing purposes.
func MakeTestSyncCommittee(period uint64) *SyncCommittee {
	pubkeys := make([][]byte, SyncCommitteeSize)
	for i := 0; i < SyncCommitteeSize; i++ {
		// Derive pubkey from period and index.
		seed := new(big.Int).SetUint64(period*uint64(SyncCommitteeSize) + uint64(i))
		pubkeys[i] = crypto.Keccak256(seed.Bytes())
	}

	// Compute aggregate pubkey.
	aggPK := crypto.Keccak256(pubkeys[0])
	for i := 1; i < len(pubkeys); i++ {
		combined := append(aggPK, pubkeys[i]...)
		aggPK = crypto.Keccak256(combined)
	}

	return &SyncCommittee{
		Pubkeys:         pubkeys,
		AggregatePubkey: aggPK,
		Period:          period,
	}
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
