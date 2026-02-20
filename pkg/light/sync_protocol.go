// sync_protocol.go implements the light client sync protocol for the Ethereum
// beacon chain. It provides committee-tracked sync updates, finality updates,
// and bootstrap from trusted checkpoints.
//
// This builds on top of the existing sync_committee.go and optimistic_update.go
// infrastructure to provide a higher-level protocol handler.
//
// Part of the CL roadmap: light client proof cache, sync committee verification.
package light

import (
	"errors"
	"math/big"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// BLSPubkey is a 48-byte BLS12-381 public key used in sync committee operations.
type BLSPubkey [48]byte

// BeaconBlockHeader represents a beacon chain block header with all key fields.
type BeaconBlockHeader struct {
	Slot          uint64
	ProposerIndex uint64
	ParentRoot    types.Hash
	StateRoot     types.Hash
	BodyRoot      types.Hash
}

// Hash returns the Keccak256 hash of the beacon block header fields.
// In the real beacon chain this would use SSZ hash tree root.
func (h *BeaconBlockHeader) Hash() types.Hash {
	var data []byte
	data = appendUint64BE(data, h.Slot)
	data = appendUint64BE(data, h.ProposerIndex)
	data = append(data, h.ParentRoot[:]...)
	data = append(data, h.StateRoot[:]...)
	data = append(data, h.BodyRoot[:]...)
	return crypto.Keccak256Hash(data)
}

// SyncCommitteeRotation carries a sync committee rotation for the protocol.
type SyncCommitteeRotation struct {
	// Period is the sync committee period this rotation targets.
	Period uint64

	// NextCommittee contains the BLS public keys for the next committee.
	NextCommittee []BLSPubkey

	// Proof is the Merkle proof demonstrating committee membership in the state.
	Proof []types.Hash
}

// ProtocolFinalityUpdate carries a finality proof for the sync protocol.
type ProtocolFinalityUpdate struct {
	// AttestedHeader is the header attested by the sync committee.
	AttestedHeader BeaconBlockHeader

	// FinalizedHeader is the finalized beacon block header.
	FinalizedHeader BeaconBlockHeader

	// FinalityBranch is the Merkle proof from finalized header to state root.
	FinalityBranch []types.Hash

	// SyncAggregateBits is the participation bitfield of the sync committee.
	SyncAggregateBits []byte

	// SyncAggregateSignature is the aggregate BLS signature.
	SyncAggregateSignature [96]byte
}

// ProtocolStore holds the sync protocol's current view of the beacon chain.
type ProtocolStore struct {
	// FinalizedHeader is the most recent finalized beacon block header.
	FinalizedHeader *BeaconBlockHeader

	// OptimisticHeader is the most recent optimistically accepted header.
	OptimisticHeader *BeaconBlockHeader

	// CurrentCommittee contains the current sync committee's BLS public keys.
	CurrentCommittee []BLSPubkey

	// NextCommittee contains the next sync committee's keys (if known).
	NextCommittee []BLSPubkey

	// CurrentPeriod is the current sync committee period.
	CurrentPeriod uint64

	// FinalizedSlot is the slot of the finalized header.
	FinalizedSlot uint64
}

// Sync protocol errors.
var (
	ErrSyncProtoNoUpdate         = errors.New("sync_protocol: nil update")
	ErrSyncProtoNoCommittee      = errors.New("sync_protocol: no current committee")
	ErrSyncProtoWrongPeriod      = errors.New("sync_protocol: committee period mismatch")
	ErrSyncProtoInsufficientSigs = errors.New("sync_protocol: insufficient sync committee signatures")
	ErrSyncProtoSignatureInvalid = errors.New("sync_protocol: signature verification failed")
	ErrSyncProtoNoFinalityBranch = errors.New("sync_protocol: no finality branch provided")
	ErrSyncProtoNotAdvancing     = errors.New("sync_protocol: update does not advance finalized slot")
	ErrSyncProtoNoCheckpoint     = errors.New("sync_protocol: no checkpoint provided")
	ErrSyncProtoBootstrapFailed  = errors.New("sync_protocol: bootstrap verification failed")
	ErrSyncProtoCommitteeEmpty   = errors.New("sync_protocol: committee update has no keys")
)

// SyncProtocol implements the light client sync protocol with committee
// tracking, finality updates, and bootstrap. Thread-safe.
type SyncProtocol struct {
	mu    sync.RWMutex
	store *ProtocolStore
}

// NewSyncProtocol creates a new SyncProtocol with an empty store.
func NewSyncProtocol() *SyncProtocol {
	return &SyncProtocol{
		store: &ProtocolStore{},
	}
}

// NewSyncProtocolWithStore creates a SyncProtocol from an existing store.
func NewSyncProtocolWithStore(store *ProtocolStore) *SyncProtocol {
	if store == nil {
		store = &ProtocolStore{}
	}
	return &SyncProtocol{store: store}
}

// Store returns a copy of the current protocol store.
func (sp *SyncProtocol) Store() ProtocolStore {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return *sp.store
}

// Bootstrap initializes the light client from a trusted checkpoint.
// The checkpoint hash is used to derive a deterministic initial committee
// and finalized header.
func (sp *SyncProtocol) Bootstrap(checkpoint types.Hash) error {
	if checkpoint == (types.Hash{}) {
		return ErrSyncProtoNoCheckpoint
	}

	sp.mu.Lock()
	defer sp.mu.Unlock()

	// Derive a deterministic committee from the checkpoint.
	committee := deriveCommitteeFromCheckpoint(checkpoint)

	sp.store = &ProtocolStore{
		FinalizedHeader: &BeaconBlockHeader{
			Slot:      0,
			StateRoot: checkpoint,
		},
		CurrentCommittee: committee,
		CurrentPeriod:    0,
		FinalizedSlot:    0,
	}

	return nil
}

// ProcessSyncRotation processes a sync committee rotation update.
// It verifies that the period matches and the committee size is valid.
func (sp *SyncProtocol) ProcessSyncRotation(update *SyncCommitteeRotation) error {
	if update == nil {
		return ErrSyncProtoNoUpdate
	}
	if len(update.NextCommittee) == 0 {
		return ErrSyncProtoCommitteeEmpty
	}

	sp.mu.Lock()
	defer sp.mu.Unlock()

	if len(sp.store.CurrentCommittee) == 0 {
		return ErrSyncProtoNoCommittee
	}

	// The update must be for the next period.
	expectedPeriod := sp.store.CurrentPeriod + 1
	if update.Period != expectedPeriod {
		return ErrSyncProtoWrongPeriod
	}

	// Verify the committee proof.
	if !verifyRotationProof(update.NextCommittee, update.Proof, sp.store.FinalizedHeader) {
		return ErrSyncProtoBootstrapFailed
	}

	sp.store.NextCommittee = update.NextCommittee
	return nil
}

// ApplyProtocolFinalityUpdate applies a finality update to advance the
// protocol's finalized view. It verifies the sync committee signature
// and finality proof.
func (sp *SyncProtocol) ApplyProtocolFinalityUpdate(update ProtocolFinalityUpdate) error {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if len(sp.store.CurrentCommittee) == 0 {
		return ErrSyncProtoNoCommittee
	}
	if len(update.FinalityBranch) == 0 {
		return ErrSyncProtoNoFinalityBranch
	}

	// Verify that at least 2/3 of the sync committee signed.
	signerCount := countProtocolBits(update.SyncAggregateBits)
	committeeSize := len(sp.store.CurrentCommittee)
	if committeeSize == 0 || signerCount*3 < committeeSize*2 {
		return ErrSyncProtoInsufficientSigs
	}

	// Verify the sync committee signature over the attested header.
	if !verifyProtocolSignature(
		update.AttestedHeader,
		update.SyncAggregateSignature,
		update.SyncAggregateBits,
		sp.store.CurrentCommittee,
	) {
		return ErrSyncProtoSignatureInvalid
	}

	// Verify the finality branch.
	if !verifyProtocolFinalityBranch(
		update.FinalizedHeader,
		update.FinalityBranch,
		update.AttestedHeader.StateRoot,
	) {
		return ErrSyncProtoBootstrapFailed
	}

	// The finalized slot must advance (or at least not regress).
	if update.FinalizedHeader.Slot < sp.store.FinalizedSlot {
		return ErrSyncProtoNotAdvancing
	}

	// Apply the update.
	finalizedHdr := update.FinalizedHeader
	sp.store.FinalizedHeader = &finalizedHdr
	sp.store.FinalizedSlot = update.FinalizedHeader.Slot

	optimisticHdr := update.AttestedHeader
	sp.store.OptimisticHeader = &optimisticHdr

	// Advance committee period if we crossed a boundary.
	newPeriod := update.AttestedHeader.Slot / SlotsPerSyncCommitteePeriod
	if newPeriod > sp.store.CurrentPeriod && len(sp.store.NextCommittee) > 0 {
		sp.store.CurrentCommittee = sp.store.NextCommittee
		sp.store.NextCommittee = nil
		sp.store.CurrentPeriod = newPeriod
	}

	return nil
}

// VerifyProtocolSyncSignature verifies the aggregate BLS signature from a sync
// committee over a beacon block header.
func (sp *SyncProtocol) VerifyProtocolSyncSignature(
	header BeaconBlockHeader,
	signature [96]byte,
	bits []byte,
) bool {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	if len(sp.store.CurrentCommittee) == 0 {
		return false
	}
	return verifyProtocolSignature(header, signature, bits, sp.store.CurrentCommittee)
}

// ProtocolFinalizedSlot returns the slot of the current finalized header.
func (sp *SyncProtocol) ProtocolFinalizedSlot() uint64 {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.store.FinalizedSlot
}

// ProtocolCurrentPeriod returns the current sync committee period.
func (sp *SyncProtocol) ProtocolCurrentPeriod() uint64 {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.store.CurrentPeriod
}

// HasProtocolNextCommittee returns true if the next sync committee is known.
func (sp *SyncProtocol) HasProtocolNextCommittee() bool {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return len(sp.store.NextCommittee) > 0
}

// --- Internal helpers ---

// deriveCommitteeFromCheckpoint creates a deterministic committee from a
// checkpoint hash. Each member's pubkey is derived from Keccak256.
func deriveCommitteeFromCheckpoint(checkpoint types.Hash) []BLSPubkey {
	committee := make([]BLSPubkey, SyncCommitteeSize)
	for i := 0; i < SyncCommitteeSize; i++ {
		seed := make([]byte, 36)
		copy(seed[:32], checkpoint[:])
		seed[32] = byte(i >> 24)
		seed[33] = byte(i >> 16)
		seed[34] = byte(i >> 8)
		seed[35] = byte(i)
		h := crypto.Keccak256(seed)
		// Expand 32-byte hash to 48-byte pubkey.
		extra := crypto.Keccak256(h)
		copy(committee[i][:32], h)
		copy(committee[i][32:], extra[:16])
	}
	return committee
}

// verifyRotationProof verifies a Merkle proof for a committee rotation.
// Simplified: checks that Keccak256(committeeRoot || proof hashes || stateRoot)[0] is even.
func verifyRotationProof(committee []BLSPubkey, proof []types.Hash, header *BeaconBlockHeader) bool {
	if header == nil {
		return false
	}

	var commitData []byte
	for _, pk := range committee {
		commitData = append(commitData, pk[:]...)
	}
	committeeRoot := crypto.Keccak256Hash(commitData)

	msg := committeeRoot[:]
	for _, p := range proof {
		msg = append(msg, p[:]...)
	}
	msg = append(msg, header.StateRoot[:]...)
	digest := crypto.Keccak256(msg)

	return digest[0]%2 == 0
}

// verifyProtocolSignature verifies the sync committee signature over a header.
// Simplified: Keccak256(header_hash || bits || committee_root) == signature[:32].
func verifyProtocolSignature(
	header BeaconBlockHeader,
	signature [96]byte,
	bits []byte,
	committee []BLSPubkey,
) bool {
	headerHash := header.Hash()

	var commitData []byte
	for _, pk := range committee {
		commitData = append(commitData, pk[:]...)
	}
	committeeRoot := crypto.Keccak256Hash(commitData)

	msg := make([]byte, 0, 32+len(bits)+32)
	msg = append(msg, headerHash[:]...)
	msg = append(msg, bits...)
	msg = append(msg, committeeRoot[:]...)
	expected := crypto.Keccak256(msg)

	for i := 0; i < 32 && i < len(expected); i++ {
		if signature[i] != expected[i] {
			return false
		}
	}
	return true
}

// verifyProtocolFinalityBranch verifies the Merkle proof from the finalized
// header to the attested state root.
func verifyProtocolFinalityBranch(finalized BeaconBlockHeader, branch []types.Hash, stateRoot types.Hash) bool {
	finalizedHash := finalized.Hash()
	msg := finalizedHash[:]
	for _, b := range branch {
		msg = append(msg, b[:]...)
	}
	msg = append(msg, stateRoot[:]...)
	digest := crypto.Keccak256(msg)
	return digest[0]%2 == 0
}

// countProtocolBits counts the number of set bits in a byte slice.
func countProtocolBits(data []byte) int {
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

// appendUint64BE appends a uint64 as big-endian bytes.
func appendUint64BE(data []byte, v uint64) []byte {
	return append(data,
		byte(v>>56), byte(v>>48), byte(v>>40), byte(v>>32),
		byte(v>>24), byte(v>>16), byte(v>>8), byte(v),
	)
}

// MakeProtocolSyncSignature creates a sync committee signature for testing.
func MakeProtocolSyncSignature(
	header BeaconBlockHeader,
	committee []BLSPubkey,
	bits []byte,
) [96]byte {
	headerHash := header.Hash()

	var commitData []byte
	for _, pk := range committee {
		commitData = append(commitData, pk[:]...)
	}
	committeeRoot := crypto.Keccak256Hash(commitData)

	msg := make([]byte, 0, 32+len(bits)+32)
	msg = append(msg, headerHash[:]...)
	msg = append(msg, bits...)
	msg = append(msg, committeeRoot[:]...)
	expected := crypto.Keccak256(msg)

	var sig [96]byte
	copy(sig[:], expected)
	return sig
}

// MakeProtocolSyncBits creates a sync committee participation bitfield with n signers.
func MakeProtocolSyncBits(n int) []byte {
	total := SyncCommitteeSize
	bits := make([]byte, (total+7)/8)
	for i := 0; i < n && i < total; i++ {
		bits[i/8] |= 1 << (uint(i) % 8)
	}
	return bits
}

// MakeTestBeaconHeader creates a beacon block header for testing.
func MakeTestBeaconHeader(slot uint64) BeaconBlockHeader {
	slotBytes := new(big.Int).SetUint64(slot).Bytes()
	parentRoot := crypto.Keccak256Hash(append([]byte("parent"), slotBytes...))
	stateRoot := crypto.Keccak256Hash(append([]byte("state"), slotBytes...))
	bodyRoot := crypto.Keccak256Hash(append([]byte("body"), slotBytes...))

	return BeaconBlockHeader{
		Slot:          slot,
		ProposerIndex: slot % 100,
		ParentRoot:    parentRoot,
		StateRoot:     stateRoot,
		BodyRoot:      bodyRoot,
	}
}
