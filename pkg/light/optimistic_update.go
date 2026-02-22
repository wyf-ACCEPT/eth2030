// optimistic_update.go implements optimistic and finality updates for the
// light client. It tracks the best known finalized and optimistic headers,
// processes incoming updates with sync aggregate verification, and applies
// finality advances. This follows the Altair light client protocol.
package light

import (
	"errors"
	"sync"
)

// Optimistic update errors.
var (
	ErrOptUpdateNil              = errors.New("optimistic_update: nil update")
	ErrOptUpdateNilHeader        = errors.New("optimistic_update: nil attested header")
	ErrOptUpdateNilAggregate     = errors.New("optimistic_update: nil sync aggregate")
	ErrOptUpdateSlotRegression   = errors.New("optimistic_update: slot does not advance")
	ErrOptUpdateNoCommittee      = errors.New("optimistic_update: no sync committee configured")
	ErrOptUpdateSigFailed        = errors.New("optimistic_update: sync aggregate signature failed")
	ErrOptUpdateInsufficientPart = errors.New("optimistic_update: insufficient participation")
	ErrFinUpdateNil              = errors.New("finality_update: nil update")
	ErrFinUpdateNilAttested      = errors.New("finality_update: nil attested header")
	ErrFinUpdateNilFinalized     = errors.New("finality_update: nil finalized header")
	ErrFinUpdateNilAggregate     = errors.New("finality_update: nil sync aggregate")
	ErrFinUpdateFinalityProof    = errors.New("finality_update: finality proof failed")
	ErrFinUpdateSlotRegression   = errors.New("finality_update: finalized slot does not advance")
	ErrFinUpdateInsufficientPart = errors.New("finality_update: insufficient participation")
	ErrStoreNotInitialized       = errors.New("light_client_store: not initialized")
)

// OptimisticUpdate carries an attested header with a sync aggregate signature
// allowing the light client to optimistically track the chain head without
// waiting for finality.
type OptimisticUpdate struct {
	// AttestedHeader is the beacon block header being attested to.
	AttestedHeader *LightHeader

	// SyncAggregate contains the committee's signature over the header.
	SyncAggregate *SyncAggregate

	// SignatureSlot is the slot at which the sync committee signed.
	SignatureSlot uint64
}

// FinalityUpdate carries a finalized header along with the attested header
// and finality branch Merkle proof, allowing the light client to advance
// its finalized state.
type FinalityUpdate struct {
	// AttestedHeader is the beacon block header being attested to.
	AttestedHeader *LightHeader

	// FinalizedHeader is the finalized beacon block header.
	FinalizedHeader *LightHeader

	// FinalityBranch is the Merkle proof linking the finalized checkpoint
	// to the attested header's state root.
	FinalityBranch [][32]byte

	// SyncAggregate contains the committee's signature over the attested header.
	SyncAggregate *SyncAggregate

	// SignatureSlot is the slot at which the sync committee signed.
	SignatureSlot uint64
}

// LightClientStore tracks the light client's current view of the chain,
// including finalized and optimistic headers, the current sync committee,
// and the best valid update seen so far. Thread-safe.
type LightClientStore struct {
	mu sync.RWMutex

	// finalizedHeader is the latest finalized beacon block header.
	finalizedHeader *LightHeader

	// optimisticHeader is the latest optimistically accepted header.
	optimisticHeader *LightHeader

	// currentSyncCommittee is used for signature verification.
	currentSyncCommittee *VerifierSyncCommittee

	// bestValidUpdate stores the best finality update seen, to be applied
	// when sufficient time has passed or a better update arrives.
	bestValidUpdate *FinalityUpdate

	// verifier is used for header and signature verification.
	verifier *HeaderVerifier
}

// NewLightClientStore creates a new light client store initialized with
// the given finalized header and sync committee.
func NewLightClientStore(
	finalizedHeader *LightHeader,
	committee *VerifierSyncCommittee,
) *LightClientStore {
	verifier := NewHeaderVerifier(finalizedHeader, committee, 1024)
	return &LightClientStore{
		finalizedHeader:      finalizedHeader,
		optimisticHeader:     finalizedHeader,
		currentSyncCommittee: committee,
		verifier:             verifier,
	}
}

// ProcessOptimisticUpdate validates and applies an optimistic header update.
// It verifies the sync aggregate signature against the current committee,
// checks participation meets the 2/3 threshold, and updates the optimistic
// header if the update advances the chain.
func (s *LightClientStore) ProcessOptimisticUpdate(update *OptimisticUpdate) error {
	if update == nil {
		return ErrOptUpdateNil
	}
	if update.AttestedHeader == nil {
		return ErrOptUpdateNilHeader
	}
	if update.SyncAggregate == nil {
		return ErrOptUpdateNilAggregate
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentSyncCommittee == nil {
		return ErrOptUpdateNoCommittee
	}

	// Check that the update advances the optimistic slot.
	if s.optimisticHeader != nil && update.AttestedHeader.Slot <= s.optimisticHeader.Slot {
		return ErrOptUpdateSlotRegression
	}

	// Verify the sync aggregate signature.
	domain := [32]byte{0x07} // DOMAIN_SYNC_COMMITTEE
	signingRoot := ComputeSigningRoot(update.AttestedHeader, domain)

	participationCount, err := s.verifier.VerifySyncAggregate(
		update.SyncAggregate,
		signingRoot,
		s.currentSyncCommittee,
	)
	if err != nil {
		return ErrOptUpdateSigFailed
	}

	// Check sufficient participation.
	if err := CheckSufficientParticipation(participationCount, s.currentSyncCommittee.Size()); err != nil {
		return ErrOptUpdateInsufficientPart
	}

	// Update the optimistic header.
	s.optimisticHeader = update.AttestedHeader

	return nil
}

// ProcessFinalityUpdate validates and applies a finality update. It verifies
// the sync aggregate signature, the finality branch Merkle proof, and advances
// the finalized header if the update is better than the current state.
func (s *LightClientStore) ProcessFinalityUpdate(update *FinalityUpdate) error {
	if update == nil {
		return ErrFinUpdateNil
	}
	if update.AttestedHeader == nil {
		return ErrFinUpdateNilAttested
	}
	if update.FinalizedHeader == nil {
		return ErrFinUpdateNilFinalized
	}
	if update.SyncAggregate == nil {
		return ErrFinUpdateNilAggregate
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentSyncCommittee == nil {
		return ErrOptUpdateNoCommittee
	}

	// Verify the sync aggregate signature over the attested header.
	domain := [32]byte{0x07} // DOMAIN_SYNC_COMMITTEE
	signingRoot := ComputeSigningRoot(update.AttestedHeader, domain)

	participationCount, err := s.verifier.VerifySyncAggregate(
		update.SyncAggregate,
		signingRoot,
		s.currentSyncCommittee,
	)
	if err != nil {
		return ErrOptUpdateSigFailed
	}

	// Check sufficient participation.
	if err := CheckSufficientParticipation(participationCount, s.currentSyncCommittee.Size()); err != nil {
		return ErrFinUpdateInsufficientPart
	}

	// Verify the finality branch if provided.
	if len(update.FinalityBranch) > 0 {
		finalizedRoot := update.FinalizedHeader.HashTreeRoot()
		if err := s.verifier.VerifyFinalityProof(
			update.AttestedHeader,
			update.FinalityBranch,
			finalizedRoot,
		); err != nil {
			return ErrFinUpdateFinalityProof
		}
	}

	// Check that the finalized slot advances.
	if s.finalizedHeader != nil && update.FinalizedHeader.Slot <= s.finalizedHeader.Slot {
		return ErrFinUpdateSlotRegression
	}

	// Apply the finality update.
	s.finalizedHeader = update.FinalizedHeader
	s.optimisticHeader = update.AttestedHeader
	s.verifier.SetTrustedHeader(update.FinalizedHeader)

	// Clear the best valid update since we've applied a new one.
	s.bestValidUpdate = nil

	return nil
}

// ShouldApplyUpdate determines whether a finality update is better than the
// current best valid update. A better update has either:
// - Higher finalized slot
// - Same finalized slot but higher participation
func (s *LightClientStore) ShouldApplyUpdate(update *FinalityUpdate) bool {
	if update == nil || update.FinalizedHeader == nil || update.SyncAggregate == nil {
		return false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// If we have no best update, any valid update is better.
	if s.bestValidUpdate == nil {
		return true
	}

	bestFinSlot := s.bestValidUpdate.FinalizedHeader.Slot
	updateFinSlot := update.FinalizedHeader.Slot

	// Higher finalized slot is always better.
	if updateFinSlot > bestFinSlot {
		return true
	}

	// Same slot: compare participation.
	if updateFinSlot == bestFinSlot {
		bestPart := s.bestValidUpdate.SyncAggregate.ParticipationCount()
		updatePart := update.SyncAggregate.ParticipationCount()
		return updatePart > bestPart
	}

	return false
}

// SetBestValidUpdate stores a finality update as the best candidate
// for later application.
func (s *LightClientStore) SetBestValidUpdate(update *FinalityUpdate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bestValidUpdate = update
}

// GetBestValidUpdate returns the best valid update, if any.
func (s *LightClientStore) GetBestValidUpdate() *FinalityUpdate {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bestValidUpdate
}

// GetFinalizedHeader returns the latest finalized header.
func (s *LightClientStore) GetFinalizedHeader() *LightHeader {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.finalizedHeader
}

// GetOptimisticHeader returns the latest optimistic header.
func (s *LightClientStore) GetOptimisticHeader() *LightHeader {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.optimisticHeader
}

// GetCurrentSyncCommittee returns the current sync committee.
func (s *LightClientStore) GetCurrentSyncCommittee() *VerifierSyncCommittee {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentSyncCommittee
}

// SetCurrentSyncCommittee updates the sync committee (e.g., on period rotation).
func (s *LightClientStore) SetCurrentSyncCommittee(committee *VerifierSyncCommittee) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentSyncCommittee = committee
	s.verifier.SetSyncCommittee(committee)
}

// FinalizedSlot returns the slot of the finalized header, or 0 if none.
func (s *LightClientStore) FinalizedSlot() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.finalizedHeader == nil {
		return 0
	}
	return s.finalizedHeader.Slot
}

// OptimisticSlot returns the slot of the optimistic header, or 0 if none.
func (s *LightClientStore) OptimisticSlot() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.optimisticHeader == nil {
		return 0
	}
	return s.optimisticHeader.Slot
}
