// announce_nonce_proto.go implements a nonce announcement protocol for the
// eth2028 client. This is from EL Throughput: "announce nonce".
// Peers broadcast their latest account nonces so that other nodes can
// pre-validate transactions and detect nonce gaps before full propagation.
package eth

import (
	"errors"
	"sync"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// Nonce announcement errors.
var (
	ErrNonceAnnNil            = errors.New("announce_nonce: nil announcement")
	ErrNonceAnnZeroAddress    = errors.New("announce_nonce: zero sender address")
	ErrNonceAnnNextBelowCurr  = errors.New("announce_nonce: next nonce is below current nonce")
	ErrNonceAnnEmptySignature = errors.New("announce_nonce: empty signature")
	ErrNonceAnnGapTooLarge    = errors.New("announce_nonce: nonce gap too large")
)

// MaxNonceGap is the maximum allowed gap between current and next nonce
// in a single announcement.
const MaxNonceGap = 256

// NonceAnnouncement represents a peer's declaration of their current and
// expected next nonce for a given address.
type NonceAnnouncement struct {
	Sender    types.Address
	Nonce     uint64 // current nonce (last confirmed)
	NextNonce uint64 // next expected nonce
	Signature []byte // cryptographic signature over (sender, nonce, nextNonce)
	Timestamp time.Time
}

// announcementEntry is the internal storage for a nonce announcement.
type announcementEntry struct {
	ann *NonceAnnouncement
	at  time.Time
}

// AnnounceStats holds statistics about the announcement pool.
type AnnounceStats struct {
	TotalAddresses    int
	TotalAnnouncements int
	OldestAnnouncement time.Time
	NewestAnnouncement time.Time
}

// NonceAnnouncementPool tracks nonce announcements from peers, indexed by
// sender address. It maintains a single latest announcement per address.
// Thread-safe.
type NonceAnnouncementPool struct {
	mu      sync.RWMutex
	entries map[types.Address]*announcementEntry
}

// NewNonceAnnouncementPool creates a new empty announcement pool.
func NewNonceAnnouncementPool() *NonceAnnouncementPool {
	return &NonceAnnouncementPool{
		entries: make(map[types.Address]*announcementEntry),
	}
}

// ValidateAnnouncement checks that a nonce announcement is well-formed.
func ValidateAnnouncement(ann *NonceAnnouncement) error {
	if ann == nil {
		return ErrNonceAnnNil
	}
	if ann.Sender == (types.Address{}) {
		return ErrNonceAnnZeroAddress
	}
	if len(ann.Signature) == 0 {
		return ErrNonceAnnEmptySignature
	}
	if ann.NextNonce < ann.Nonce {
		return ErrNonceAnnNextBelowCurr
	}
	if ann.NextNonce-ann.Nonce > MaxNonceGap {
		return ErrNonceAnnGapTooLarge
	}
	return nil
}

// AddAnnouncement validates and adds a nonce announcement to the pool.
// If an announcement for the same address already exists, it is replaced
// only if the new announcement has a higher nonce.
func (p *NonceAnnouncementPool) AddAnnouncement(ann *NonceAnnouncement) error {
	if err := ValidateAnnouncement(ann); err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	existing, ok := p.entries[ann.Sender]
	if ok && existing.ann.Nonce >= ann.Nonce {
		// Existing entry has same or higher nonce; ignore this one.
		return nil
	}

	// Copy the announcement to prevent caller mutation.
	entry := &announcementEntry{
		ann: &NonceAnnouncement{
			Sender:    ann.Sender,
			Nonce:     ann.Nonce,
			NextNonce: ann.NextNonce,
			Signature: make([]byte, len(ann.Signature)),
			Timestamp: ann.Timestamp,
		},
		at: time.Now(),
	}
	copy(entry.ann.Signature, ann.Signature)
	if entry.ann.Timestamp.IsZero() {
		entry.ann.Timestamp = entry.at
	}

	p.entries[ann.Sender] = entry
	return nil
}

// GetLatestNonce returns the latest announced nonce for an address.
// Returns (nonce, true) if found, (0, false) otherwise.
func (p *NonceAnnouncementPool) GetLatestNonce(addr types.Address) (uint64, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	entry, ok := p.entries[addr]
	if !ok {
		return 0, false
	}
	return entry.ann.Nonce, true
}

// PruneStale removes announcements older than maxAge.
func (p *NonceAnnouncementPool) PruneStale(maxAge time.Duration) int {
	p.mu.Lock()
	defer p.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	pruned := 0

	for addr, entry := range p.entries {
		if entry.at.Before(cutoff) {
			delete(p.entries, addr)
			pruned++
		}
	}
	return pruned
}

// BroadcastNonce creates a NonceAnnouncement ready for broadcast.
// The signature is a placeholder (32 bytes of 0xff) since real signing
// requires a private key that the pool does not hold.
func BroadcastNonce(addr types.Address, nonce uint64) *NonceAnnouncement {
	return &NonceAnnouncement{
		Sender:    addr,
		Nonce:     nonce,
		NextNonce: nonce + 1,
		Signature: make([]byte, 32), // placeholder signature
		Timestamp: time.Now(),
	}
}

// AnnouncementStats returns aggregate statistics about the pool.
func (p *NonceAnnouncementPool) AnnouncementStats() AnnounceStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := AnnounceStats{
		TotalAddresses:     len(p.entries),
		TotalAnnouncements: len(p.entries),
	}

	for _, entry := range p.entries {
		if stats.OldestAnnouncement.IsZero() || entry.at.Before(stats.OldestAnnouncement) {
			stats.OldestAnnouncement = entry.at
		}
		if stats.NewestAnnouncement.IsZero() || entry.at.After(stats.NewestAnnouncement) {
			stats.NewestAnnouncement = entry.at
		}
	}

	return stats
}

// Size returns the number of addresses tracked in the pool.
func (p *NonceAnnouncementPool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.entries)
}
