// Forward-Cast Blobs: pre-announcing future blob availability for the Data Layer
// roadmap track (blob streaming -> short-dated blob futures -> forward-cast blobs).
// Nodes announce blobs they intend to publish in upcoming slots, allowing peers
// to prepare data availability sampling and custody in advance.
package das

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// Forward-cast errors.
var (
	ErrAnnouncementSlotPast    = errors.New("das: announcement slot is in the past")
	ErrAnnouncementSlotTooFar  = errors.New("das: announcement slot exceeds max lead slots")
	ErrAnnouncementSlotFull    = errors.New("das: slot has reached max announcements")
	ErrAnnouncementExpired     = errors.New("das: announcement has expired")
	ErrAnnouncementNotFound    = errors.New("das: announcement not found")
	ErrAnnouncementFulfilled   = errors.New("das: announcement already fulfilled")
	ErrBlobDataTooLarge        = errors.New("das: blob data exceeds max size")
	ErrBlobCommitmentMismatch  = errors.New("das: blob data does not match commitment")
	ErrInvalidCommitment       = errors.New("das: commitment must not be zero")
)

// ForwardCastConfig configures the forward-cast blob subsystem.
type ForwardCastConfig struct {
	// MaxLeadSlots is the maximum number of slots ahead an announcement can target.
	MaxLeadSlots uint64

	// MaxAnnouncementsPerSlot is the cap on announcements per target slot.
	MaxAnnouncementsPerSlot uint64

	// ExpirySlots is how many slots after the target slot an announcement expires.
	ExpirySlots uint64

	// MaxBlobDataSize is the maximum size of blob data for fulfillment.
	MaxBlobDataSize uint64
}

// DefaultForwardCastConfig returns sensible defaults.
func DefaultForwardCastConfig() ForwardCastConfig {
	return ForwardCastConfig{
		MaxLeadSlots:            64,
		MaxAnnouncementsPerSlot: 32,
		ExpirySlots:             16,
		MaxBlobDataSize:         DefaultBlobSize, // 131072
	}
}

// ForwardCastAnnouncement is a pre-announcement of a blob for a future slot.
type ForwardCastAnnouncement struct {
	// Slot is the target slot this blob is announced for.
	Slot uint64

	// BlobIndex is the index of the blob within the slot.
	BlobIndex uint64

	// Commitment is the hash commitment to the blob data (Keccak-256).
	Commitment types.Hash

	// Expiry is the slot after which this announcement is no longer valid.
	Expiry uint64

	// Announcer is the address of the node that made the announcement.
	Announcer types.Address

	// fulfilled tracks whether the blob data has been provided.
	fulfilled bool

	// blobData stores the fulfilled blob payload.
	blobData []byte
}

// FulfillmentStatus reports how many announcements for a slot have been fulfilled.
type FulfillmentStatus struct {
	// Total is the number of announcements for the slot.
	Total uint64

	// Fulfilled is the number of announcements with provided blob data.
	Fulfilled uint64

	// Missing is the number of unfulfilled announcements.
	Missing uint64

	// MissingBlobs lists the blob indices that have not been fulfilled.
	MissingBlobs []uint64
}

// ForwardCaster manages forward-cast blob announcements.
// All methods are safe for concurrent use.
type ForwardCaster struct {
	mu          sync.Mutex
	config      ForwardCastConfig
	currentSlot uint64

	// bySlot maps target slot -> list of announcements.
	bySlot map[uint64][]*ForwardCastAnnouncement

	// byKey maps (slot, blobIndex) -> announcement for fast lookup.
	byKey map[annKey]*ForwardCastAnnouncement
}

// annKey is an internal key for deduplicating announcements.
type annKey struct {
	slot      uint64
	blobIndex uint64
}

// NewForwardCaster creates a new ForwardCaster with the given configuration.
func NewForwardCaster(config ForwardCastConfig) *ForwardCaster {
	if config.MaxLeadSlots == 0 {
		config.MaxLeadSlots = 64
	}
	if config.MaxAnnouncementsPerSlot == 0 {
		config.MaxAnnouncementsPerSlot = 32
	}
	if config.ExpirySlots == 0 {
		config.ExpirySlots = 16
	}
	if config.MaxBlobDataSize == 0 {
		config.MaxBlobDataSize = DefaultBlobSize
	}
	return &ForwardCaster{
		config: config,
		bySlot: make(map[uint64][]*ForwardCastAnnouncement),
		byKey:  make(map[annKey]*ForwardCastAnnouncement),
	}
}

// SetCurrentSlot updates the caster's view of the current slot.
func (fc *ForwardCaster) SetCurrentSlot(slot uint64) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.currentSlot = slot
}

// AnnounceBlob pre-announces a blob for a future slot.
func (fc *ForwardCaster) AnnounceBlob(slot uint64, blobIndex uint64, commitment types.Hash) error {
	return fc.AnnounceBlobFrom(slot, blobIndex, commitment, types.Address{})
}

// AnnounceBlobFrom pre-announces a blob from a specific announcer address.
func (fc *ForwardCaster) AnnounceBlobFrom(slot uint64, blobIndex uint64, commitment types.Hash, announcer types.Address) error {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	if commitment == (types.Hash{}) {
		return ErrInvalidCommitment
	}
	if slot <= fc.currentSlot {
		return fmt.Errorf("%w: slot %d <= current %d", ErrAnnouncementSlotPast, slot, fc.currentSlot)
	}
	if slot > fc.currentSlot+fc.config.MaxLeadSlots {
		return fmt.Errorf("%w: slot %d > current+max %d",
			ErrAnnouncementSlotTooFar, slot, fc.currentSlot+fc.config.MaxLeadSlots)
	}

	existing := fc.bySlot[slot]
	if uint64(len(existing)) >= fc.config.MaxAnnouncementsPerSlot {
		return fmt.Errorf("%w: slot %d has %d announcements",
			ErrAnnouncementSlotFull, slot, len(existing))
	}

	// Deduplicate by (slot, blobIndex): overwrite if same key.
	key := annKey{slot: slot, blobIndex: blobIndex}
	if old, ok := fc.byKey[key]; ok {
		// Update in place.
		old.Commitment = commitment
		old.Announcer = announcer
		old.Expiry = slot + fc.config.ExpirySlots
		old.fulfilled = false
		old.blobData = nil
		return nil
	}

	ann := &ForwardCastAnnouncement{
		Slot:       slot,
		BlobIndex:  blobIndex,
		Commitment: commitment,
		Expiry:     slot + fc.config.ExpirySlots,
		Announcer:  announcer,
	}

	fc.bySlot[slot] = append(fc.bySlot[slot], ann)
	fc.byKey[key] = ann
	return nil
}

// GetAnnouncements returns all announcements targeting the given slot.
// Results are sorted by blob index.
func (fc *ForwardCaster) GetAnnouncements(slot uint64) []*ForwardCastAnnouncement {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	anns := fc.bySlot[slot]
	if len(anns) == 0 {
		return nil
	}

	// Return a sorted copy.
	result := make([]*ForwardCastAnnouncement, len(anns))
	copy(result, anns)
	sort.Slice(result, func(i, j int) bool {
		return result[i].BlobIndex < result[j].BlobIndex
	})
	return result
}

// ValidateAnnouncement checks that an announcement is well-formed relative to
// the current slot.
func (fc *ForwardCaster) ValidateAnnouncement(ann *ForwardCastAnnouncement) error {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	if ann == nil {
		return ErrAnnouncementNotFound
	}
	if ann.Commitment == (types.Hash{}) {
		return ErrInvalidCommitment
	}
	if ann.Slot <= fc.currentSlot {
		return fmt.Errorf("%w: slot %d <= current %d",
			ErrAnnouncementSlotPast, ann.Slot, fc.currentSlot)
	}
	if ann.Expiry <= fc.currentSlot {
		return ErrAnnouncementExpired
	}
	return nil
}

// FulfillAnnouncement provides the actual blob data for a previously announced blob.
// The data is verified against the commitment (Keccak-256 hash).
func (fc *ForwardCaster) FulfillAnnouncement(ann *ForwardCastAnnouncement, blobData []byte) error {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	if ann == nil {
		return ErrAnnouncementNotFound
	}
	if ann.fulfilled {
		return ErrAnnouncementFulfilled
	}
	if ann.Expiry <= fc.currentSlot {
		return ErrAnnouncementExpired
	}
	if uint64(len(blobData)) > fc.config.MaxBlobDataSize {
		return fmt.Errorf("%w: size %d > max %d",
			ErrBlobDataTooLarge, len(blobData), fc.config.MaxBlobDataSize)
	}

	// Verify the blob data matches the commitment.
	dataHash := crypto.Keccak256Hash(blobData)
	if dataHash != ann.Commitment {
		return ErrBlobCommitmentMismatch
	}

	ann.fulfilled = true
	ann.blobData = make([]byte, len(blobData))
	copy(ann.blobData, blobData)
	return nil
}

// CheckFulfillment reports the fulfillment status of announcements for a slot.
func (fc *ForwardCaster) CheckFulfillment(slot uint64) *FulfillmentStatus {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	anns := fc.bySlot[slot]
	status := &FulfillmentStatus{
		Total: uint64(len(anns)),
	}

	for _, ann := range anns {
		if ann.fulfilled {
			status.Fulfilled++
		} else {
			status.Missing++
			status.MissingBlobs = append(status.MissingBlobs, ann.BlobIndex)
		}
	}

	// Sort missing blob indices for determinism.
	sort.Slice(status.MissingBlobs, func(i, j int) bool {
		return status.MissingBlobs[i] < status.MissingBlobs[j]
	})

	return status
}

// PruneExpired removes all announcements whose expiry slot is at or before
// the current slot.
func (fc *ForwardCaster) PruneExpired() {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	for slot, anns := range fc.bySlot {
		var kept []*ForwardCastAnnouncement
		for _, ann := range anns {
			if ann.Expiry <= fc.currentSlot {
				delete(fc.byKey, annKey{slot: ann.Slot, blobIndex: ann.BlobIndex})
			} else {
				kept = append(kept, ann)
			}
		}
		if len(kept) == 0 {
			delete(fc.bySlot, slot)
		} else {
			fc.bySlot[slot] = kept
		}
	}
}

// GetPendingCount returns the number of unfulfilled, non-expired announcements.
func (fc *ForwardCaster) GetPendingCount() int {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	count := 0
	for _, anns := range fc.bySlot {
		for _, ann := range anns {
			if !ann.fulfilled && ann.Expiry > fc.currentSlot {
				count++
			}
		}
	}
	return count
}
