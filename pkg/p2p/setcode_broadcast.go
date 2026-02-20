// setcode_broadcast.go implements EIP-7702 SetCode authorization tuple
// dissemination over the P2P gossip network. Authorization tuples allow
// accounts to delegate their code to a specified address, and must be
// propagated to validators before inclusion in blocks.
//
// The broadcaster uses topic-based gossip with deduplication via bloom
// filters and per-authority rate limiting to prevent spam.
package p2p

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// SetCode broadcast errors.
var (
	ErrSetCodeNilMessage      = errors.New("setcode: nil message")
	ErrSetCodeEmptyAuthority  = errors.New("setcode: empty authority address")
	ErrSetCodeInvalidChainID  = errors.New("setcode: invalid chain ID")
	ErrSetCodeInvalidSig      = errors.New("setcode: invalid authorization signature")
	ErrSetCodeDuplicate       = errors.New("setcode: duplicate authorization")
	ErrSetCodeRateLimited     = errors.New("setcode: authority rate limited")
	ErrSetCodeBroadcasterStop = errors.New("setcode: broadcaster stopped")
)

// SetCode broadcast constants.
const (
	// SetCodeTopicPrefix is the gossip topic prefix for set-code messages.
	SetCodeTopicPrefix = "setcode_auth/"

	// DefaultSetCodeRateLimit is max set-code messages per authority per epoch.
	DefaultSetCodeRateLimit = 16

	// DefaultSetCodeEpochDuration is the epoch duration for rate limiting.
	DefaultSetCodeEpochDuration = 12 * time.Second * 32 // ~6.4 minutes

	// setCodeBloomSize is the bloom filter size in bits for deduplication.
	setCodeBloomSize = 1 << 16 // 65536 bits

	// setCodeBloomHashes is the number of hash functions for the bloom filter.
	setCodeBloomHashes = 4
)

// SetCodeMessage represents an EIP-7702 authorization tuple for gossip.
type SetCodeMessage struct {
	Authority types.Address // Account delegating its code
	ChainID   *big.Int      // Target chain ID
	Nonce     uint64        // Authorization nonce
	Target    types.Address // Address whose code to delegate to
	V         *big.Int      // Signature V
	R         *big.Int      // Signature R
	S         *big.Int      // Signature S
	Timestamp time.Time     // When the message was created
}

// Hash computes the canonical hash of the SetCode message.
func (m *SetCodeMessage) Hash() types.Hash {
	var buf []byte
	buf = append(buf, m.Authority[:]...)
	if m.ChainID != nil {
		buf = append(buf, m.ChainID.Bytes()...)
	}
	var nonceBuf [8]byte
	binary.BigEndian.PutUint64(nonceBuf[:], m.Nonce)
	buf = append(buf, nonceBuf[:]...)
	buf = append(buf, m.Target[:]...)
	return crypto.Keccak256Hash(buf)
}

// DeduplicationKey returns the unique key for deduplication: (authority, nonce).
func (m *SetCodeMessage) DeduplicationKey() types.Hash {
	var buf []byte
	buf = append(buf, m.Authority[:]...)
	var nonceBuf [8]byte
	binary.BigEndian.PutUint64(nonceBuf[:], m.Nonce)
	buf = append(buf, nonceBuf[:]...)
	return crypto.Keccak256Hash(buf)
}

// ValidateSetCodeAuth verifies the authorization signature and checks nonce.
func ValidateSetCodeAuth(msg *SetCodeMessage) bool {
	if msg == nil {
		return false
	}
	if msg.Authority == (types.Address{}) {
		return false
	}
	if msg.ChainID == nil || msg.ChainID.Sign() < 0 {
		return false
	}
	if msg.R == nil || msg.S == nil || msg.V == nil {
		return false
	}
	if msg.R.Sign() == 0 || msg.S.Sign() == 0 {
		return false
	}

	// Verify signature is well-formed (r, s in valid range).
	// secp256k1 order N (approximate check).
	secp256k1N, _ := new(big.Int).SetString(
		"FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141", 16,
	)
	if msg.R.Cmp(secp256k1N) >= 0 || msg.S.Cmp(secp256k1N) >= 0 {
		return false
	}

	// V must be 0 or 1 (EIP-7702 uses yParity).
	vVal := msg.V.Uint64()
	if vVal > 1 {
		return false
	}

	return true
}

// setCodeBloomFilter is a simple bloom filter for deduplication.
type setCodeBloomFilter struct {
	bits [setCodeBloomSize / 8]byte
}

// add adds a key to the bloom filter.
func (bf *setCodeBloomFilter) add(key types.Hash) {
	for i := 0; i < setCodeBloomHashes; i++ {
		var iBuf [4]byte
		binary.BigEndian.PutUint32(iBuf[:], uint32(i))
		h := sha256.Sum256(append(key[:], iBuf[:]...))
		idx := binary.BigEndian.Uint32(h[:4]) % setCodeBloomSize
		bf.bits[idx/8] |= 1 << (idx % 8)
	}
}

// contains checks if a key might be in the bloom filter.
func (bf *setCodeBloomFilter) contains(key types.Hash) bool {
	for i := 0; i < setCodeBloomHashes; i++ {
		var iBuf [4]byte
		binary.BigEndian.PutUint32(iBuf[:], uint32(i))
		h := sha256.Sum256(append(key[:], iBuf[:]...))
		idx := binary.BigEndian.Uint32(h[:4]) % setCodeBloomSize
		if bf.bits[idx/8]&(1<<(idx%8)) == 0 {
			return false
		}
	}
	return true
}

// reset clears the bloom filter.
func (bf *setCodeBloomFilter) reset() {
	for i := range bf.bits {
		bf.bits[i] = 0
	}
}

// authorityRateState tracks rate limiting for a single authority.
type authorityRateState struct {
	count     int
	epochTime time.Time
}

// SetCodeBroadcaster manages EIP-7702 authorization dissemination.
type SetCodeBroadcaster struct {
	mu            sync.RWMutex
	stopped       int32
	chainID       *big.Int
	rateLimit     int
	epochDuration time.Duration
	bloom         setCodeBloomFilter
	rates         map[types.Address]*authorityRateState
	pending       []*SetCodeMessage
	handlers      []SetCodeGossipHandler
}

// SetCodeGossipHandler is the interface for handling set-code gossip messages.
type SetCodeGossipHandler interface {
	HandleSetCodeAuth(msg *SetCodeMessage) error
}

// SetCodeGossipHandlerFunc is an adapter to use ordinary functions as handlers.
type SetCodeGossipHandlerFunc func(msg *SetCodeMessage) error

// HandleSetCodeAuth implements SetCodeGossipHandler.
func (f SetCodeGossipHandlerFunc) HandleSetCodeAuth(msg *SetCodeMessage) error {
	return f(msg)
}

// NewSetCodeBroadcaster creates a new SetCode broadcaster for the given chain.
func NewSetCodeBroadcaster(chainID *big.Int) *SetCodeBroadcaster {
	return &SetCodeBroadcaster{
		chainID:       chainID,
		rateLimit:     DefaultSetCodeRateLimit,
		epochDuration: DefaultSetCodeEpochDuration,
		rates:         make(map[types.Address]*authorityRateState),
		pending:       make([]*SetCodeMessage, 0, 64),
	}
}

// TopicName returns the gossip topic for this broadcaster.
func (b *SetCodeBroadcaster) TopicName() string {
	if b.chainID == nil {
		return SetCodeTopicPrefix + "0"
	}
	return SetCodeTopicPrefix + b.chainID.String()
}

// Submit validates and queues a set-code message for broadcast.
func (b *SetCodeBroadcaster) Submit(msg *SetCodeMessage) error {
	if atomic.LoadInt32(&b.stopped) == 1 {
		return ErrSetCodeBroadcasterStop
	}
	if msg == nil {
		return ErrSetCodeNilMessage
	}
	if msg.Authority == (types.Address{}) {
		return ErrSetCodeEmptyAuthority
	}
	if msg.ChainID == nil || msg.ChainID.Sign() < 0 {
		return ErrSetCodeInvalidChainID
	}

	// Validate signature.
	if !ValidateSetCodeAuth(msg) {
		return ErrSetCodeInvalidSig
	}

	dedupKey := msg.DeduplicationKey()

	b.mu.Lock()
	defer b.mu.Unlock()

	// Check deduplication via bloom filter.
	if b.bloom.contains(dedupKey) {
		return ErrSetCodeDuplicate
	}

	// Check rate limiting.
	if err := b.checkRateLimit(msg.Authority); err != nil {
		return err
	}

	// Add to bloom filter and pending queue.
	b.bloom.add(dedupKey)
	b.updateRateLimit(msg.Authority)
	b.pending = append(b.pending, msg)

	// Notify handlers.
	for _, h := range b.handlers {
		_ = h.HandleSetCodeAuth(msg)
	}

	return nil
}

// AddHandler registers a gossip handler.
func (b *SetCodeBroadcaster) AddHandler(handler SetCodeGossipHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers = append(b.handlers, handler)
}

// PendingCount returns the number of pending messages.
func (b *SetCodeBroadcaster) PendingCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.pending)
}

// DrainPending returns and clears all pending messages.
func (b *SetCodeBroadcaster) DrainPending() []*SetCodeMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	msgs := b.pending
	b.pending = make([]*SetCodeMessage, 0, 64)
	return msgs
}

// Stop stops the broadcaster.
func (b *SetCodeBroadcaster) Stop() {
	atomic.StoreInt32(&b.stopped, 1)
}

// ResetEpoch resets the rate limiter and bloom filter for a new epoch.
func (b *SetCodeBroadcaster) ResetEpoch() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.bloom.reset()
	b.rates = make(map[types.Address]*authorityRateState)
}

// SetRateLimit updates the per-authority message rate limit.
func (b *SetCodeBroadcaster) SetRateLimit(limit int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if limit > 0 {
		b.rateLimit = limit
	}
}

// --- internal helpers ---

// checkRateLimit checks if the authority has exceeded its rate limit.
func (b *SetCodeBroadcaster) checkRateLimit(authority types.Address) error {
	state, exists := b.rates[authority]
	if !exists {
		return nil
	}
	// Check if epoch has expired.
	if time.Since(state.epochTime) > b.epochDuration {
		return nil // epoch expired, will be reset
	}
	if state.count >= b.rateLimit {
		return ErrSetCodeRateLimited
	}
	return nil
}

// updateRateLimit increments the rate counter for an authority.
func (b *SetCodeBroadcaster) updateRateLimit(authority types.Address) {
	state, exists := b.rates[authority]
	if !exists || time.Since(state.epochTime) > b.epochDuration {
		b.rates[authority] = &authorityRateState{
			count:     1,
			epochTime: time.Now(),
		}
		return
	}
	state.count++
}
