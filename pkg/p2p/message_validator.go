// message_validator.go implements a message validation pipeline for the gossip
// protocol. It validates incoming messages against protocol rules, checks
// freshness, verifies signatures, deduplicates using a bloom filter, and
// applies per-peer rate limiting.
package p2p

import (
	"errors"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Message validation errors.
var (
	ErrMsgValNilMessage    = errors.New("msgval: nil message")
	ErrMsgValEmptyPayload  = errors.New("msgval: empty payload")
	ErrMsgValTooLarge      = errors.New("msgval: message exceeds max size")
	ErrMsgValStale         = errors.New("msgval: message is stale")
	ErrMsgValFuture        = errors.New("msgval: message timestamp in the future")
	ErrMsgValDuplicate     = errors.New("msgval: duplicate message")
	ErrMsgValRateLimited   = errors.New("msgval: peer rate limit exceeded")
	ErrMsgValInvalidSig    = errors.New("msgval: invalid signature")
	ErrMsgValZeroSender    = errors.New("msgval: zero sender")
	ErrMsgValZeroMsgID     = errors.New("msgval: zero message ID")
	ErrMsgValUnknownTopic  = errors.New("msgval: unknown topic")
)

// MsgValidatorConfig configures the MsgValidator.
type MsgValidatorConfig struct {
	MaxMessageSize   uint64        // Maximum allowed message size in bytes.
	MaxStaleAge      time.Duration // Maximum age of a message before it is stale.
	MaxFutureDrift   time.Duration // Maximum timestamp drift into the future.
	BloomFilterSize  uint32        // Bloom filter bit count for seen messages.
	BloomHashCount   uint32        // Number of hash functions for bloom filter.
	RateLimitPerPeer int           // Max messages per peer per rate period.
	RatePeriod       time.Duration // Rate limiting window duration.
	AllowedTopics    []string      // Allowed topic names. If empty, all topics accepted.
}

// DefaultMsgValidatorConfig returns sensible defaults for message validation.
func DefaultMsgValidatorConfig() MsgValidatorConfig {
	return MsgValidatorConfig{
		MaxMessageSize:   1 << 20, // 1 MiB
		MaxStaleAge:      5 * time.Minute,
		MaxFutureDrift:   30 * time.Second,
		BloomFilterSize:  65536, // 64K bits
		BloomHashCount:   8,
		RateLimitPerPeer: 100,
		RatePeriod:       10 * time.Second,
	}
}

// bloomFilter is a simple bloom filter for message deduplication.
type bloomFilter struct {
	bits     []uint64
	size     uint32
	hashN    uint32
}

func newBloomFilter(size, hashCount uint32) *bloomFilter {
	if size == 0 {
		size = 65536
	}
	if hashCount == 0 {
		hashCount = 8
	}
	words := (size + 63) / 64
	return &bloomFilter{
		bits:  make([]uint64, words),
		size:  size,
		hashN: hashCount,
	}
}

// add inserts a hash into the bloom filter.
func (bf *bloomFilter) add(h types.Hash) {
	for i := uint32(0); i < bf.hashN; i++ {
		idx := bf.index(h, i)
		bf.bits[idx/64] |= 1 << (idx % 64)
	}
}

// contains returns true if the hash may be in the filter (probabilistic).
func (bf *bloomFilter) contains(h types.Hash) bool {
	for i := uint32(0); i < bf.hashN; i++ {
		idx := bf.index(h, i)
		if bf.bits[idx/64]&(1<<(idx%64)) == 0 {
			return false
		}
	}
	return true
}

// reset clears the bloom filter.
func (bf *bloomFilter) reset() {
	for i := range bf.bits {
		bf.bits[i] = 0
	}
}

// index computes the bit index for a given hash and hash function index.
func (bf *bloomFilter) index(h types.Hash, fnIdx uint32) uint32 {
	// Use different byte ranges from the 32-byte hash for each function.
	offset := (fnIdx * 4) % 32
	val := uint32(h[offset]) | uint32(h[(offset+1)%32])<<8 |
		uint32(h[(offset+2)%32])<<16 | uint32(h[(offset+3)%32])<<24
	return val % bf.size
}

// count returns the approximate number of bits set.
func (bf *bloomFilter) count() int {
	n := 0
	for _, word := range bf.bits {
		for word != 0 {
			n++
			word &= word - 1
		}
	}
	return n
}

// peerRateState tracks rate limiting state for a single peer.
type peerRateState struct {
	count     int
	windowStart time.Time
}

// GossipMsgEnvelope wraps a gossip message with metadata for validation.
type GossipMsgEnvelope struct {
	Topic     string
	Payload   []byte
	SenderID  types.Hash
	MessageID types.Hash
	Timestamp uint64     // Unix seconds.
	Signature []byte     // Optional signature over the payload.
}

// ValidationResult holds the outcome of message validation.
type ValidationResult struct {
	Valid   bool
	Reason  error
}

// MsgValidator validates incoming gossip messages. It enforces size limits,
// freshness, deduplication, per-peer rate limits, and optional signature
// verification. All methods are safe for concurrent use.
type MsgValidator struct {
	mu       sync.RWMutex
	config   MsgValidatorConfig
	bloom    *bloomFilter
	rates    map[types.Hash]*peerRateState // sender -> rate state
	topics   map[string]bool               // allowed topics (empty = all allowed)
	stats    MsgValidatorStats
}

// MsgValidatorStats tracks validation statistics.
type MsgValidatorStats struct {
	TotalValidated uint64
	TotalAccepted  uint64
	TotalRejected  uint64
	DuplicateCount uint64
	StaleCount     uint64
	RateLimited    uint64
	InvalidSigs    uint64
}

// NewMsgValidator creates a new MsgValidator.
func NewMsgValidator(config MsgValidatorConfig) *MsgValidator {
	topics := make(map[string]bool)
	for _, t := range config.AllowedTopics {
		topics[t] = true
	}
	return &MsgValidator{
		config: config,
		bloom:  newBloomFilter(config.BloomFilterSize, config.BloomHashCount),
		rates:  make(map[types.Hash]*peerRateState),
		topics: topics,
	}
}

// Validate runs the full validation pipeline on a message envelope.
func (mv *MsgValidator) Validate(env *GossipMsgEnvelope) ValidationResult {
	mv.mu.Lock()
	defer mv.mu.Unlock()

	mv.stats.TotalValidated++

	if err := mv.checkBasicLocked(env); err != nil {
		mv.stats.TotalRejected++
		return ValidationResult{Valid: false, Reason: err}
	}

	if err := mv.checkFreshnessLocked(env); err != nil {
		mv.stats.TotalRejected++
		if errors.Is(err, ErrMsgValStale) {
			mv.stats.StaleCount++
		}
		return ValidationResult{Valid: false, Reason: err}
	}

	if err := mv.checkDuplicateLocked(env); err != nil {
		mv.stats.TotalRejected++
		mv.stats.DuplicateCount++
		return ValidationResult{Valid: false, Reason: err}
	}

	if err := mv.checkRateLimitLocked(env); err != nil {
		mv.stats.TotalRejected++
		mv.stats.RateLimited++
		return ValidationResult{Valid: false, Reason: err}
	}

	if err := mv.checkTopicLocked(env); err != nil {
		mv.stats.TotalRejected++
		return ValidationResult{Valid: false, Reason: err}
	}

	if err := mv.checkSignatureLocked(env); err != nil {
		mv.stats.TotalRejected++
		mv.stats.InvalidSigs++
		return ValidationResult{Valid: false, Reason: err}
	}

	// Mark as seen.
	mv.bloom.add(env.MessageID)
	mv.stats.TotalAccepted++
	return ValidationResult{Valid: true}
}

// checkBasicLocked validates basic message structure.
func (mv *MsgValidator) checkBasicLocked(env *GossipMsgEnvelope) error {
	if env == nil {
		return ErrMsgValNilMessage
	}
	if len(env.Payload) == 0 {
		return ErrMsgValEmptyPayload
	}
	if mv.config.MaxMessageSize > 0 && uint64(len(env.Payload)) > mv.config.MaxMessageSize {
		return ErrMsgValTooLarge
	}
	if env.SenderID.IsZero() {
		return ErrMsgValZeroSender
	}
	if env.MessageID.IsZero() {
		return ErrMsgValZeroMsgID
	}
	return nil
}

// checkFreshnessLocked validates message timestamp is within bounds.
func (mv *MsgValidator) checkFreshnessLocked(env *GossipMsgEnvelope) error {
	if env.Timestamp == 0 {
		return ErrMsgValStale
	}
	msgTime := time.Unix(int64(env.Timestamp), 0)
	now := time.Now()

	age := now.Sub(msgTime)
	if age > mv.config.MaxStaleAge {
		return ErrMsgValStale
	}
	if msgTime.After(now.Add(mv.config.MaxFutureDrift)) {
		return ErrMsgValFuture
	}
	return nil
}

// checkDuplicateLocked checks the bloom filter for duplicates.
func (mv *MsgValidator) checkDuplicateLocked(env *GossipMsgEnvelope) error {
	if mv.bloom.contains(env.MessageID) {
		return ErrMsgValDuplicate
	}
	return nil
}

// checkRateLimitLocked applies per-peer rate limiting.
func (mv *MsgValidator) checkRateLimitLocked(env *GossipMsgEnvelope) error {
	if mv.config.RateLimitPerPeer <= 0 {
		return nil
	}
	now := time.Now()
	rs, ok := mv.rates[env.SenderID]
	if !ok {
		rs = &peerRateState{windowStart: now}
		mv.rates[env.SenderID] = rs
	}

	// Reset window if expired.
	if now.Sub(rs.windowStart) >= mv.config.RatePeriod {
		rs.count = 0
		rs.windowStart = now
	}

	rs.count++
	if rs.count > mv.config.RateLimitPerPeer {
		return ErrMsgValRateLimited
	}
	return nil
}

// checkTopicLocked verifies the topic is allowed.
func (mv *MsgValidator) checkTopicLocked(env *GossipMsgEnvelope) error {
	if len(mv.topics) == 0 {
		return nil // All topics allowed.
	}
	if !mv.topics[env.Topic] {
		return ErrMsgValUnknownTopic
	}
	return nil
}

// checkSignatureLocked verifies the message signature if present.
func (mv *MsgValidator) checkSignatureLocked(env *GossipMsgEnvelope) error {
	if len(env.Signature) == 0 {
		return nil // No signature to verify; skip.
	}
	// Compute hash of the payload for signature verification.
	payloadHash := crypto.Keccak256Hash(env.Payload)
	if len(env.Signature) != 65 {
		return ErrMsgValInvalidSig
	}
	_, err := crypto.SigToPub(payloadHash[:], env.Signature)
	if err != nil {
		return ErrMsgValInvalidSig
	}
	return nil
}

// Stats returns a snapshot of the validation statistics.
func (mv *MsgValidator) Stats() MsgValidatorStats {
	mv.mu.RLock()
	defer mv.mu.RUnlock()
	return mv.stats
}

// ResetBloom clears the bloom filter for seen messages. This should be
// called periodically to prevent false positive buildup.
func (mv *MsgValidator) ResetBloom() {
	mv.mu.Lock()
	defer mv.mu.Unlock()
	mv.bloom.reset()
}

// ResetRates clears all per-peer rate limiting state.
func (mv *MsgValidator) ResetRates() {
	mv.mu.Lock()
	defer mv.mu.Unlock()
	mv.rates = make(map[types.Hash]*peerRateState)
}

// AddAllowedTopic adds a topic to the allowed set.
func (mv *MsgValidator) AddAllowedTopic(topic string) {
	mv.mu.Lock()
	defer mv.mu.Unlock()
	mv.topics[topic] = true
}

// RemoveAllowedTopic removes a topic from the allowed set.
func (mv *MsgValidator) RemoveAllowedTopic(topic string) {
	mv.mu.Lock()
	defer mv.mu.Unlock()
	delete(mv.topics, topic)
}

// AllowedTopics returns the list of currently allowed topics.
func (mv *MsgValidator) AllowedTopics() []string {
	mv.mu.RLock()
	defer mv.mu.RUnlock()
	result := make([]string, 0, len(mv.topics))
	for t := range mv.topics {
		result = append(result, t)
	}
	return result
}

// BloomBitCount returns the number of bits currently set in the bloom filter,
// useful for monitoring false positive pressure.
func (mv *MsgValidator) BloomBitCount() int {
	mv.mu.RLock()
	defer mv.mu.RUnlock()
	return mv.bloom.count()
}

// PeerRateCount returns the current message count in the rate window
// for a peer. Returns 0 if the peer has no rate state.
func (mv *MsgValidator) PeerRateCount(senderID types.Hash) int {
	mv.mu.RLock()
	defer mv.mu.RUnlock()
	rs, ok := mv.rates[senderID]
	if !ok {
		return 0
	}
	return rs.count
}
