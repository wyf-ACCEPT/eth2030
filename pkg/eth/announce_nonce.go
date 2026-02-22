// Package eth implements EIP-8077: announce transactions with nonce.
//
// EIP-8077 extends the eth protocol's NewPooledTransactionHashes message to
// include each transaction's source address and nonce alongside the existing
// hash, type, and size fields. This enables receivers to make smarter fetch
// decisions, fill nonce gaps, and filter stale announcements.
package eth

import (
	"fmt"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/rlp"
)

// Protocol version for EIP-8077 announce nonce extension.
const ETH72 = 72 // EIP-8077: Announce Nonce

// MaxAnnouncements is the maximum number of transaction announcements in a
// single NewPooledTransactionHashesWithNonce message.
const MaxAnnouncements = 4096

// nonceTrackerExpiry is how long announced nonces are tracked before expiry.
const nonceTrackerExpiry = 5 * time.Minute

// AnnounceNonceMsg is a transaction announcement carrying source address and
// nonce per EIP-8077. It extends the eth/68 NewPooledTransactionHashes format.
//
// Wire format (RLP):
//
//	[txtypes: B, [txsize₁, txsize₂, ...], [txhash₁, txhash₂, ...],
//	 [txsource₁, txsource₂, ...], [txnonce₁, txnonce₂, ...]]
type AnnounceNonceMsg struct {
	Types   []byte          // Transaction type for each entry.
	Sizes   []uint32        // Encoded size of each transaction.
	Hashes  []types.Hash    // Transaction hash for each entry.
	Sources []types.Address // Sender address for each entry (EIP-8077).
	Nonces  []uint64        // Sender nonce for each entry (EIP-8077).
}

// Validate checks that all slices have equal length and the count does not
// exceed MaxAnnouncements.
func (m *AnnounceNonceMsg) Validate() error {
	n := len(m.Hashes)
	if n > MaxAnnouncements {
		return ErrTooManyAnnouncements
	}
	if len(m.Types) != n || len(m.Sizes) != n || len(m.Sources) != n || len(m.Nonces) != n {
		return ErrAnnounceLengthMismatch
	}
	return nil
}

// EncodeAnnounceNonce encodes an AnnounceNonceMsg to RLP bytes.
// Wire format: list of [types_bytes, sizes_list, hashes_list, sources_list, nonces_list].
func EncodeAnnounceNonce(msg *AnnounceNonceMsg) ([]byte, error) {
	if err := msg.Validate(); err != nil {
		return nil, err
	}

	// Encode each field.
	typesEnc, err := rlp.EncodeToBytes(msg.Types)
	if err != nil {
		return nil, err
	}

	sizesEnc, err := rlp.EncodeToBytes(msg.Sizes)
	if err != nil {
		return nil, err
	}

	hashesEnc, err := rlp.EncodeToBytes(msg.Hashes)
	if err != nil {
		return nil, err
	}

	sourcesEnc, err := rlp.EncodeToBytes(msg.Sources)
	if err != nil {
		return nil, err
	}

	noncesEnc, err := rlp.EncodeToBytes(msg.Nonces)
	if err != nil {
		return nil, err
	}

	var payload []byte
	payload = append(payload, typesEnc...)
	payload = append(payload, sizesEnc...)
	payload = append(payload, hashesEnc...)
	payload = append(payload, sourcesEnc...)
	payload = append(payload, noncesEnc...)

	return rlp.WrapList(payload), nil
}

// DecodeAnnounceNonce decodes RLP bytes into an AnnounceNonceMsg.
func DecodeAnnounceNonce(data []byte) (*AnnounceNonceMsg, error) {
	s := rlp.NewStreamFromBytes(data)
	_, err := s.List()
	if err != nil {
		return nil, err
	}

	// 1. Types (byte string).
	txTypes, err := s.Bytes()
	if err != nil {
		return nil, err
	}

	// 2. Sizes (list of uint).
	sizes, err := decodeUint32List(s)
	if err != nil {
		return nil, err
	}

	// 3. Hashes (list of B_32).
	hashes, err := decodeHashList(s)
	if err != nil {
		return nil, err
	}

	// 4. Sources (list of B_20).
	sources, err := decodeAddressList(s)
	if err != nil {
		return nil, err
	}

	// 5. Nonces (list of uint).
	nonces, err := decodeUint64List(s)
	if err != nil {
		return nil, err
	}

	if err := s.ListEnd(); err != nil {
		return nil, err
	}

	msg := &AnnounceNonceMsg{
		Types:   txTypes,
		Sizes:   sizes,
		Hashes:  hashes,
		Sources: sources,
		Nonces:  nonces,
	}
	if err := msg.Validate(); err != nil {
		return nil, err
	}
	return msg, nil
}

// RLP list decode helpers.

func decodeUint32List(s *rlp.Stream) ([]uint32, error) {
	_, err := s.List()
	if err != nil {
		return nil, err
	}
	var result []uint32
	for !s.AtListEnd() {
		v, err := s.Uint64()
		if err != nil {
			return nil, err
		}
		result = append(result, uint32(v))
	}
	return result, s.ListEnd()
}

func decodeHashList(s *rlp.Stream) ([]types.Hash, error) {
	_, err := s.List()
	if err != nil {
		return nil, err
	}
	var result []types.Hash
	for !s.AtListEnd() {
		b, err := s.Bytes()
		if err != nil {
			return nil, err
		}
		if len(b) != 32 {
			return nil, ErrInvalidHashLength
		}
		var h types.Hash
		copy(h[:], b)
		result = append(result, h)
	}
	return result, s.ListEnd()
}

func decodeAddressList(s *rlp.Stream) ([]types.Address, error) {
	_, err := s.List()
	if err != nil {
		return nil, err
	}
	var result []types.Address
	for !s.AtListEnd() {
		b, err := s.Bytes()
		if err != nil {
			return nil, err
		}
		if len(b) != 20 {
			return nil, ErrInvalidAddressLength
		}
		var addr types.Address
		copy(addr[:], b)
		result = append(result, addr)
	}
	return result, s.ListEnd()
}

func decodeUint64List(s *rlp.Stream) ([]uint64, error) {
	_, err := s.List()
	if err != nil {
		return nil, err
	}
	var result []uint64
	for !s.AtListEnd() {
		v, err := s.Uint64()
		if err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, s.ListEnd()
}

// --- Nonce Tracker ---

// nonceEntry tracks an announced sender+nonce pair with a timestamp.
type nonceEntry struct {
	hash types.Hash
	at   time.Time
}

// NonceTracker tracks announced transaction nonces per sender address
// to enable deduplication and intelligent fetch scheduling.
type NonceTracker struct {
	mu sync.RWMutex
	// known maps sender address -> nonce -> latest announcement.
	known map[types.Address]map[uint64]nonceEntry
}

// NewNonceTracker creates a new NonceTracker.
func NewNonceTracker() *NonceTracker {
	return &NonceTracker{
		known: make(map[types.Address]map[uint64]nonceEntry),
	}
}

// Announce records a transaction announcement. Returns true if this is a new
// announcement (not previously known for this sender+nonce), false if duplicate.
func (nt *NonceTracker) Announce(sender types.Address, nonce uint64, txHash types.Hash) bool {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	nonces, ok := nt.known[sender]
	if !ok {
		nonces = make(map[uint64]nonceEntry)
		nt.known[sender] = nonces
	}

	if existing, ok := nonces[nonce]; ok {
		// If same hash, this is a duplicate announcement.
		if existing.hash == txHash {
			return false
		}
		// Different hash for same sender+nonce means RBF replacement.
		// Update to track the latest version.
	}

	nonces[nonce] = nonceEntry{hash: txHash, at: time.Now()}
	return true
}

// IsKnown returns true if a transaction with the given sender+nonce
// has already been announced.
func (nt *NonceTracker) IsKnown(sender types.Address, nonce uint64) bool {
	nt.mu.RLock()
	defer nt.mu.RUnlock()

	nonces, ok := nt.known[sender]
	if !ok {
		return false
	}
	_, exists := nonces[nonce]
	return exists
}

// ExpireOld removes entries older than the nonceTrackerExpiry duration.
func (nt *NonceTracker) ExpireOld() int {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	cutoff := time.Now().Add(-nonceTrackerExpiry)
	expired := 0

	for sender, nonces := range nt.known {
		for nonce, entry := range nonces {
			if entry.at.Before(cutoff) {
				delete(nonces, nonce)
				expired++
			}
		}
		if len(nonces) == 0 {
			delete(nt.known, sender)
		}
	}
	return expired
}

// GetPending returns the set of announced but not-yet-fetched transactions
// for a given sender, as a map of nonce -> tx hash.
func (nt *NonceTracker) GetPending(sender types.Address) map[uint64]types.Hash {
	nt.mu.RLock()
	defer nt.mu.RUnlock()

	nonces, ok := nt.known[sender]
	if !ok {
		return nil
	}

	result := make(map[uint64]types.Hash, len(nonces))
	for nonce, entry := range nonces {
		result[nonce] = entry.hash
	}
	return result
}

// Remove deletes a specific sender+nonce entry (e.g., after fetching/including).
func (nt *NonceTracker) Remove(sender types.Address, nonce uint64) {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	nonces, ok := nt.known[sender]
	if !ok {
		return
	}
	delete(nonces, nonce)
	if len(nonces) == 0 {
		delete(nt.known, sender)
	}
}

// Len returns the total number of tracked announcements.
func (nt *NonceTracker) Len() int {
	nt.mu.RLock()
	defer nt.mu.RUnlock()

	total := 0
	for _, nonces := range nt.known {
		total += len(nonces)
	}
	return total
}

// ValidateAnnouncedNonce checks whether the nonce announced for a given block
// hash is consistent with the tracker. It looks up the announced nonce for the
// sender and verifies it matches the expected nonce. Returns nil if valid.
func ValidateAnnouncedNonce(tracker *NonceTracker, sender types.Address, expectedNonce uint64, txHash types.Hash) error {
	if tracker == nil {
		return ErrNonceTrackerNil
	}
	pending := tracker.GetPending(sender)
	if pending == nil {
		return ErrNonceNotTracked
	}
	storedHash, ok := pending[expectedNonce]
	if !ok {
		return fmt.Errorf("%w: sender %x nonce %d", ErrNonceNotTracked, sender, expectedNonce)
	}
	if storedHash != txHash {
		return fmt.Errorf("%w: expected tx %x, got %x", ErrNonceHashMismatch, storedHash, txHash)
	}
	return nil
}

// ProcessAnnounceMsg processes an AnnounceNonceMsg and records all entries
// into the given NonceTracker. Returns the number of new announcements recorded.
func ProcessAnnounceMsg(tracker *NonceTracker, msg *AnnounceNonceMsg) (int, error) {
	if tracker == nil {
		return 0, ErrNonceTrackerNil
	}
	if err := msg.Validate(); err != nil {
		return 0, err
	}
	newCount := 0
	for i := range msg.Hashes {
		if tracker.Announce(msg.Sources[i], msg.Nonces[i], msg.Hashes[i]) {
			newCount++
		}
	}
	return newCount, nil
}

// Errors for announce nonce.
var (
	ErrTooManyAnnouncements   = fmt.Errorf("eth: too many announcements (max %d)", MaxAnnouncements)
	ErrAnnounceLengthMismatch = fmt.Errorf("eth: announce field length mismatch")
	ErrInvalidHashLength      = fmt.Errorf("eth: invalid hash length in announcement")
	ErrInvalidAddressLength   = fmt.Errorf("eth: invalid address length in announcement")
	ErrNonceTrackerNil        = fmt.Errorf("eth: nonce tracker is nil")
	ErrNonceNotTracked        = fmt.Errorf("eth: nonce not tracked for sender")
	ErrNonceHashMismatch      = fmt.Errorf("eth: nonce hash mismatch")
)
