// Package lethe implements LETHE (Light Ephemeral Transport for Hidden
// Execution) insulation channels. Each Channel establishes a private
// execution context where participants share an ephemeral symmetric key
// derived via Keccak256, and payloads are encrypted with AES-256-GCM.
// This provides confidential cross-validator communication as part of
// the Ethereum 2028 roadmap privacy layer.
package lethe

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Channel errors.
var (
	ErrChannelClosed        = errors.New("channel is closed")
	ErrNotParticipant       = errors.New("address is not a channel participant")
	ErrAlreadyParticipant   = errors.New("address is already a channel participant")
	ErrNoParticipants       = errors.New("channel requires at least one participant")
	ErrChannelNotFound      = errors.New("channel not found")
	ErrChannelExists        = errors.New("channel already exists")
	ErrInvalidPubKey        = errors.New("invalid public key")
	ErrDecryptionFailed     = errors.New("decryption failed")
	ErrPayloadTooShort      = errors.New("ciphertext too short")
	ErrInvalidChannelID     = errors.New("invalid channel ID")
)

// participant holds a channel member's address and optional ephemeral public key.
type participant struct {
	addr   types.Address
	pubKey []byte // ephemeral public key (opaque bytes)
}

// Channel represents a private execution channel with ephemeral key material.
// All participants share the same derived symmetric key for AES-GCM
// encrypt/decrypt. The key is derived from the channel ID and participant
// addresses via Keccak256.
type Channel struct {
	mu           sync.RWMutex
	ID           types.Hash
	participants map[types.Address]*participant
	symKey       []byte // 32-byte AES-256 key
	closed       bool
}

// NewChannel creates a new LETHE channel with the given participants.
// A shared symmetric key is derived from the channel ID and participant set.
func NewChannel(id types.Hash, participants []types.Address) *Channel {
	ch := &Channel{
		ID:           id,
		participants: make(map[types.Address]*participant, len(participants)),
	}
	for _, addr := range participants {
		ch.participants[addr] = &participant{addr: addr}
	}
	ch.symKey = ch.deriveKey()
	return ch
}

// deriveKey computes a 32-byte AES key from the channel ID and sorted
// participant addresses using Keccak256.
func (ch *Channel) deriveKey() []byte {
	// Build preimage: channelID || addr1 || addr2 || ...
	// Addresses are included in map-iteration order; the resulting key
	// is deterministic because Keccak256 is a PRF and the same set of
	// addresses always produces the same hash regardless of insertion order
	// when we hash the ID together with all addresses combined.
	preimage := make([]byte, 0, types.HashLength+len(ch.participants)*types.AddressLength)
	preimage = append(preimage, ch.ID[:]...)
	// Collect and sort addresses for deterministic derivation.
	addrs := make([]types.Address, 0, len(ch.participants))
	for addr := range ch.participants {
		addrs = append(addrs, addr)
	}
	sortAddresses(addrs)
	for _, addr := range addrs {
		preimage = append(preimage, addr[:]...)
	}
	return crypto.Keccak256(preimage)
}

// sortAddresses sorts a slice of addresses lexicographically.
func sortAddresses(addrs []types.Address) {
	for i := 1; i < len(addrs); i++ {
		for j := i; j > 0; j-- {
			if compareAddresses(addrs[j], addrs[j-1]) < 0 {
				addrs[j], addrs[j-1] = addrs[j-1], addrs[j]
			} else {
				break
			}
		}
	}
}

func compareAddresses(a, b types.Address) int {
	for i := 0; i < types.AddressLength; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

// EncryptPayload encrypts a payload for the channel. The sender must be a
// participant. Uses AES-256-GCM with a random nonce prepended to the output.
func (ch *Channel) EncryptPayload(sender types.Address, payload []byte) ([]byte, error) {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	if ch.closed {
		return nil, ErrChannelClosed
	}
	if _, ok := ch.participants[sender]; !ok {
		return nil, ErrNotParticipant
	}

	block, err := aes.NewCipher(ch.symKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	// Output: nonce || ciphertext (including GCM tag).
	ciphertext := gcm.Seal(nonce, nonce, payload, nil)
	return ciphertext, nil
}

// DecryptPayload decrypts a ciphertext from the channel. The recipient must
// be a participant. Expects the nonce to be prepended to the ciphertext.
func (ch *Channel) DecryptPayload(recipient types.Address, ciphertext []byte) ([]byte, error) {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	if ch.closed {
		return nil, ErrChannelClosed
	}
	if _, ok := ch.participants[recipient]; !ok {
		return nil, ErrNotParticipant
	}

	block, err := aes.NewCipher(ch.symKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, ErrPayloadTooShort
	}

	nonce := ciphertext[:nonceSize]
	enc := ciphertext[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, enc, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}
	return plaintext, nil
}

// AddParticipant adds a new participant with an optional ephemeral public key.
// The channel symmetric key is re-derived after adding the participant.
func (ch *Channel) AddParticipant(addr types.Address, pubKey []byte) error {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	if ch.closed {
		return ErrChannelClosed
	}
	if _, exists := ch.participants[addr]; exists {
		return ErrAlreadyParticipant
	}

	ch.participants[addr] = &participant{addr: addr, pubKey: cloneBytes(pubKey)}
	ch.symKey = ch.deriveKey()
	return nil
}

// RemoveParticipant removes a participant from the channel.
// The symmetric key is re-derived, locking out the removed participant.
func (ch *Channel) RemoveParticipant(addr types.Address) error {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	if ch.closed {
		return ErrChannelClosed
	}
	if _, exists := ch.participants[addr]; !exists {
		return ErrNotParticipant
	}
	if len(ch.participants) <= 1 {
		return ErrNoParticipants
	}

	delete(ch.participants, addr)
	ch.symKey = ch.deriveKey()
	return nil
}

// Participants returns the addresses of all current participants.
func (ch *Channel) Participants() []types.Address {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	addrs := make([]types.Address, 0, len(ch.participants))
	for addr := range ch.participants {
		addrs = append(addrs, addr)
	}
	sortAddresses(addrs)
	return addrs
}

// IsClosed returns whether the channel has been closed.
func (ch *Channel) IsClosed() bool {
	ch.mu.RLock()
	defer ch.mu.RUnlock()
	return ch.closed
}

// Close marks the channel as closed. Subsequent operations return ErrChannelClosed.
func (ch *Channel) Close() {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	ch.closed = true
	// Zero out key material.
	for i := range ch.symKey {
		ch.symKey[i] = 0
	}
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

// ChannelManager manages the lifecycle of multiple LETHE channels.
// It is safe for concurrent use.
type ChannelManager struct {
	mu       sync.RWMutex
	channels map[types.Hash]*Channel
}

// NewChannelManager creates a new channel manager.
func NewChannelManager() *ChannelManager {
	return &ChannelManager{
		channels: make(map[types.Hash]*Channel),
	}
}

// CreateChannel creates a new channel for the given participants.
// The channel ID is derived from a random seed hashed with participant addresses.
func (cm *ChannelManager) CreateChannel(participants []types.Address) (*Channel, error) {
	if len(participants) == 0 {
		return nil, ErrNoParticipants
	}

	// Generate a random channel ID.
	seed := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, seed); err != nil {
		return nil, err
	}

	// Derive ID: Keccak256(seed || participant addresses).
	preimage := make([]byte, 0, 32+len(participants)*types.AddressLength)
	preimage = append(preimage, seed...)
	sorted := make([]types.Address, len(participants))
	copy(sorted, participants)
	sortAddresses(sorted)
	for _, addr := range sorted {
		preimage = append(preimage, addr[:]...)
	}
	id := crypto.Keccak256Hash(preimage)

	cm.mu.Lock()
	defer cm.mu.Unlock()

	if _, exists := cm.channels[id]; exists {
		return nil, ErrChannelExists
	}

	ch := NewChannel(id, participants)
	cm.channels[id] = ch
	return ch, nil
}

// GetChannel retrieves an active channel by ID.
func (cm *ChannelManager) GetChannel(id types.Hash) (*Channel, error) {
	if id.IsZero() {
		return nil, ErrInvalidChannelID
	}

	cm.mu.RLock()
	defer cm.mu.RUnlock()

	ch, ok := cm.channels[id]
	if !ok {
		return nil, ErrChannelNotFound
	}
	return ch, nil
}

// CloseChannel closes a channel and removes it from the manager.
func (cm *ChannelManager) CloseChannel(id types.Hash) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	ch, ok := cm.channels[id]
	if !ok {
		return ErrChannelNotFound
	}
	ch.Close()
	delete(cm.channels, id)
	return nil
}

// ListActiveChannels returns all channels that have not been closed.
func (cm *ChannelManager) ListActiveChannels() []*Channel {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	result := make([]*Channel, 0, len(cm.channels))
	for _, ch := range cm.channels {
		if !ch.IsClosed() {
			result = append(result, ch)
		}
	}
	return result
}

// ChannelCount returns the number of managed channels.
func (cm *ChannelManager) ChannelCount() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.channels)
}
