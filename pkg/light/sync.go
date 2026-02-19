package light

import (
	"errors"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

var (
	ErrNoUpdate          = errors.New("light: nil update")
	ErrNoAttestedHeader  = errors.New("light: update missing attested header")
	ErrNoFinalizedHeader = errors.New("light: update missing finalized header")
	ErrNoSignature       = errors.New("light: update missing signature")
	ErrInsufficientSigs  = errors.New("light: insufficient sync committee signatures")
	ErrNotFinalized      = errors.New("light: finalized header number exceeds attested")
)

// SyncCommitteeSize is the number of validators in a sync committee.
const SyncCommitteeSize = 512

// LightSyncer processes light client updates and maintains the
// finalized chain view.
type LightSyncer struct {
	state *LightClientState
	store LightStore
}

// NewLightSyncer creates a new LightSyncer with the given store.
func NewLightSyncer(store LightStore) *LightSyncer {
	return &LightSyncer{
		state: &LightClientState{},
		store: store,
	}
}

// ProcessUpdate validates and applies a light client update. It verifies
// the sync committee signature meets the supermajority threshold and
// updates the finalized header.
func (ls *LightSyncer) ProcessUpdate(update *LightClientUpdate) error {
	if update == nil {
		return ErrNoUpdate
	}
	if update.AttestedHeader == nil {
		return ErrNoAttestedHeader
	}
	if update.FinalizedHeader == nil {
		return ErrNoFinalizedHeader
	}
	if len(update.Signature) == 0 {
		return ErrNoSignature
	}

	// Verify the finalized header number does not exceed the attested header.
	if update.FinalizedHeader.Number != nil && update.AttestedHeader.Number != nil {
		if update.FinalizedHeader.Number.Cmp(update.AttestedHeader.Number) > 0 {
			return ErrNotFinalized
		}
	}

	// Check that at least 2/3 of the committee signed.
	committeeSize := SyncCommitteeSize
	if ls.state.CurrentCommittee != nil && len(ls.state.CurrentCommittee.Pubkeys) > 0 {
		committeeSize = len(ls.state.CurrentCommittee.Pubkeys)
	}
	if !update.SupermajoritySigned(committeeSize) {
		return ErrInsufficientSigs
	}

	// Verify the signature against the attested header.
	// In production this would verify BLS aggregate signature;
	// here we verify a Keccak256 binding.
	if !ls.verifySignature(update) {
		return ErrNoSignature
	}

	// Update finalized state.
	ls.state.FinalizedHeader = update.FinalizedHeader
	if update.AttestedHeader.Number != nil {
		ls.state.CurrentSlot = update.AttestedHeader.Number.Uint64()
	}

	// Rotate sync committee if provided.
	if update.NextSyncCommittee != nil {
		ls.state.CurrentCommittee = update.NextSyncCommittee
	}

	// Store headers.
	ls.store.StoreHeader(update.FinalizedHeader)
	ls.store.StoreHeader(update.AttestedHeader)

	return nil
}

// GetFinalizedHeader returns the most recent finalized header.
func (ls *LightSyncer) GetFinalizedHeader() *types.Header {
	return ls.state.FinalizedHeader
}

// IsSynced returns true if the light client has a finalized header.
func (ls *LightSyncer) IsSynced() bool {
	return ls.state.FinalizedHeader != nil
}

// State returns the current light client state.
func (ls *LightSyncer) State() *LightClientState {
	return ls.state
}

// verifySignature checks the sync committee signature over the attested
// header. Placeholder: checks Keccak256(header_hash || committee_bits)
// against the signature bytes.
func (ls *LightSyncer) verifySignature(update *LightClientUpdate) bool {
	headerHash := update.AttestedHeader.Hash()
	msg := append(headerHash[:], update.SyncCommitteeBits...)
	expected := crypto.Keccak256(msg)

	if len(update.Signature) != len(expected) {
		return false
	}
	for i := range expected {
		if update.Signature[i] != expected[i] {
			return false
		}
	}
	return true
}

// SignUpdate creates a placeholder sync committee signature for an update.
// Used in tests to create valid updates.
func SignUpdate(header *types.Header, committeeBits []byte) []byte {
	headerHash := header.Hash()
	msg := append(headerHash[:], committeeBits...)
	return crypto.Keccak256(msg)
}

// MakeCommitteeBits creates a sync committee participation bitfield with
// the given number of signers (from bit 0 upward).
func MakeCommitteeBits(signers int) []byte {
	bits := make([]byte, (SyncCommitteeSize+7)/8)
	for i := 0; i < signers && i < SyncCommitteeSize; i++ {
		bits[i/8] |= 1 << (uint(i) % 8)
	}
	return bits
}

// makeHeader is a test helper that creates a header with the given number.
func makeHeader(num uint64) *types.Header {
	return &types.Header{
		Number: new(big.Int).SetUint64(num),
	}
}
