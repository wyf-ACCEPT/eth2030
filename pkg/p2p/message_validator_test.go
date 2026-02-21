package p2p

import (
	"sync"
	"testing"
	"time"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

func validEnvelope() *GossipMsgEnvelope {
	payload := []byte("test payload data")
	return &GossipMsgEnvelope{
		Topic:     "blocks",
		Payload:   payload,
		SenderID:  types.HexToHash("0xaaaa"),
		MessageID: crypto.Keccak256Hash(payload),
		Timestamp: uint64(time.Now().Unix()),
	}
}

func newTestMsgValidator() *MsgValidator {
	return NewMsgValidator(DefaultMsgValidatorConfig())
}

func TestMsgValidator_ValidMessage(t *testing.T) {
	mv := newTestMsgValidator()
	env := validEnvelope()
	res := mv.Validate(env)
	if !res.Valid {
		t.Errorf("expected valid, got reason: %v", res.Reason)
	}
}

func TestMsgValidator_NilMessage(t *testing.T) {
	mv := newTestMsgValidator()
	res := mv.Validate(nil)
	if res.Valid {
		t.Error("expected invalid for nil message")
	}
	if res.Reason != ErrMsgValNilMessage {
		t.Errorf("reason = %v, want ErrMsgValNilMessage", res.Reason)
	}
}

func TestMsgValidator_EmptyPayload(t *testing.T) {
	mv := newTestMsgValidator()
	env := validEnvelope()
	env.Payload = nil
	res := mv.Validate(env)
	if res.Valid {
		t.Error("expected invalid for empty payload")
	}
	if res.Reason != ErrMsgValEmptyPayload {
		t.Errorf("reason = %v, want ErrMsgValEmptyPayload", res.Reason)
	}
}

func TestMsgValidator_TooLarge(t *testing.T) {
	mv := newTestMsgValidator()
	env := validEnvelope()
	env.Payload = make([]byte, 2<<20) // 2 MiB > 1 MiB limit
	res := mv.Validate(env)
	if res.Valid {
		t.Error("expected invalid for oversized message")
	}
	if res.Reason != ErrMsgValTooLarge {
		t.Errorf("reason = %v, want ErrMsgValTooLarge", res.Reason)
	}
}

func TestMsgValidator_ZeroSender(t *testing.T) {
	mv := newTestMsgValidator()
	env := validEnvelope()
	env.SenderID = types.Hash{}
	res := mv.Validate(env)
	if res.Valid {
		t.Error("expected invalid for zero sender")
	}
	if res.Reason != ErrMsgValZeroSender {
		t.Errorf("reason = %v, want ErrMsgValZeroSender", res.Reason)
	}
}

func TestMsgValidator_ZeroMessageID(t *testing.T) {
	mv := newTestMsgValidator()
	env := validEnvelope()
	env.MessageID = types.Hash{}
	res := mv.Validate(env)
	if res.Valid {
		t.Error("expected invalid for zero message ID")
	}
	if res.Reason != ErrMsgValZeroMsgID {
		t.Errorf("reason = %v, want ErrMsgValZeroMsgID", res.Reason)
	}
}

func TestMsgValidator_StaleMessage(t *testing.T) {
	mv := newTestMsgValidator()
	env := validEnvelope()
	env.Timestamp = uint64(time.Now().Add(-10 * time.Minute).Unix())
	res := mv.Validate(env)
	if res.Valid {
		t.Error("expected invalid for stale message")
	}
	if res.Reason != ErrMsgValStale {
		t.Errorf("reason = %v, want ErrMsgValStale", res.Reason)
	}
}

func TestMsgValidator_ZeroTimestamp(t *testing.T) {
	mv := newTestMsgValidator()
	env := validEnvelope()
	env.Timestamp = 0
	res := mv.Validate(env)
	if res.Valid {
		t.Error("expected invalid for zero timestamp")
	}
	if res.Reason != ErrMsgValStale {
		t.Errorf("reason = %v, want ErrMsgValStale", res.Reason)
	}
}

func TestMsgValidator_FutureMessage(t *testing.T) {
	mv := newTestMsgValidator()
	env := validEnvelope()
	env.Timestamp = uint64(time.Now().Add(2 * time.Minute).Unix())
	res := mv.Validate(env)
	if res.Valid {
		t.Error("expected invalid for future message")
	}
	if res.Reason != ErrMsgValFuture {
		t.Errorf("reason = %v, want ErrMsgValFuture", res.Reason)
	}
}

func TestMsgValidator_Duplicate(t *testing.T) {
	mv := newTestMsgValidator()
	env := validEnvelope()
	res1 := mv.Validate(env)
	if !res1.Valid {
		t.Fatalf("first validation failed: %v", res1.Reason)
	}

	// Second validation of same message should be duplicate.
	res2 := mv.Validate(env)
	if res2.Valid {
		t.Error("expected duplicate detection on second validation")
	}
	if res2.Reason != ErrMsgValDuplicate {
		t.Errorf("reason = %v, want ErrMsgValDuplicate", res2.Reason)
	}
}

func TestMsgValidator_RateLimit(t *testing.T) {
	cfg := DefaultMsgValidatorConfig()
	cfg.RateLimitPerPeer = 3
	cfg.RatePeriod = time.Minute
	mv := NewMsgValidator(cfg)

	sender := types.HexToHash("0xbbbb")
	for i := 0; i < 3; i++ {
		env := validEnvelope()
		env.SenderID = sender
		env.MessageID = crypto.Keccak256Hash([]byte{byte(i), byte(i >> 8)})
		res := mv.Validate(env)
		if !res.Valid {
			t.Fatalf("message %d should be valid: %v", i, res.Reason)
		}
	}

	// 4th message should be rate limited.
	env := validEnvelope()
	env.SenderID = sender
	env.MessageID = crypto.Keccak256Hash([]byte{0xff, 0xff})
	res := mv.Validate(env)
	if res.Valid {
		t.Error("expected rate limit rejection")
	}
	if res.Reason != ErrMsgValRateLimited {
		t.Errorf("reason = %v, want ErrMsgValRateLimited", res.Reason)
	}
}

func TestMsgValidator_TopicFiltering(t *testing.T) {
	cfg := DefaultMsgValidatorConfig()
	cfg.AllowedTopics = []string{"blocks", "attestations"}
	mv := NewMsgValidator(cfg)

	// Allowed topic.
	env := validEnvelope()
	env.Topic = "blocks"
	res := mv.Validate(env)
	if !res.Valid {
		t.Errorf("blocks topic should be allowed: %v", res.Reason)
	}

	// Disallowed topic.
	env2 := validEnvelope()
	env2.Topic = "unknown_topic"
	env2.MessageID = crypto.Keccak256Hash([]byte("unique2"))
	res2 := mv.Validate(env2)
	if res2.Valid {
		t.Error("unknown_topic should be rejected")
	}
	if res2.Reason != ErrMsgValUnknownTopic {
		t.Errorf("reason = %v, want ErrMsgValUnknownTopic", res2.Reason)
	}
}

func TestMsgValidator_InvalidSignature(t *testing.T) {
	mv := newTestMsgValidator()
	env := validEnvelope()
	env.Signature = []byte("too short")
	res := mv.Validate(env)
	if res.Valid {
		t.Error("expected invalid for bad signature length")
	}
	if res.Reason != ErrMsgValInvalidSig {
		t.Errorf("reason = %v, want ErrMsgValInvalidSig", res.Reason)
	}
}

func TestMsgValidator_NoSignatureIsOK(t *testing.T) {
	mv := newTestMsgValidator()
	env := validEnvelope()
	env.Signature = nil
	res := mv.Validate(env)
	if !res.Valid {
		t.Errorf("no signature should be OK: %v", res.Reason)
	}
}

func TestMsgValidator_Stats(t *testing.T) {
	mv := newTestMsgValidator()

	env := validEnvelope()
	mv.Validate(env) // accepted
	mv.Validate(env) // duplicate

	stats := mv.Stats()
	if stats.TotalValidated != 2 {
		t.Errorf("TotalValidated = %d, want 2", stats.TotalValidated)
	}
	if stats.TotalAccepted != 1 {
		t.Errorf("TotalAccepted = %d, want 1", stats.TotalAccepted)
	}
	if stats.TotalRejected != 1 {
		t.Errorf("TotalRejected = %d, want 1", stats.TotalRejected)
	}
	if stats.DuplicateCount != 1 {
		t.Errorf("DuplicateCount = %d, want 1", stats.DuplicateCount)
	}
}

func TestMsgValidator_ResetBloom(t *testing.T) {
	mv := newTestMsgValidator()
	env := validEnvelope()
	mv.Validate(env)

	// After reset, same message should pass again.
	mv.ResetBloom()
	res := mv.Validate(env)
	if !res.Valid {
		t.Errorf("expected valid after bloom reset: %v", res.Reason)
	}
}

func TestMsgValidator_ResetRates(t *testing.T) {
	cfg := DefaultMsgValidatorConfig()
	cfg.RateLimitPerPeer = 1
	mv := NewMsgValidator(cfg)

	env := validEnvelope()
	mv.Validate(env)

	// Reset rates and bloom so we can re-send.
	mv.ResetRates()
	mv.ResetBloom()

	res := mv.Validate(env)
	if !res.Valid {
		t.Errorf("expected valid after rate reset: %v", res.Reason)
	}
}

func TestMsgValidator_AddRemoveAllowedTopic(t *testing.T) {
	cfg := DefaultMsgValidatorConfig()
	cfg.AllowedTopics = []string{"blocks"}
	mv := NewMsgValidator(cfg)

	mv.AddAllowedTopic("txs")
	topics := mv.AllowedTopics()
	found := false
	for _, tt := range topics {
		if tt == "txs" {
			found = true
		}
	}
	if !found {
		t.Error("txs topic not found after AddAllowedTopic")
	}

	mv.RemoveAllowedTopic("txs")
	topics = mv.AllowedTopics()
	for _, tt := range topics {
		if tt == "txs" {
			t.Error("txs topic still present after RemoveAllowedTopic")
		}
	}
}

func TestMsgValidator_BloomBitCount(t *testing.T) {
	mv := newTestMsgValidator()
	if mv.BloomBitCount() != 0 {
		t.Errorf("initial bloom bits = %d, want 0", mv.BloomBitCount())
	}
	env := validEnvelope()
	mv.Validate(env)
	if mv.BloomBitCount() == 0 {
		t.Error("bloom bits should be > 0 after adding a message")
	}
}

func TestMsgValidator_PeerRateCount(t *testing.T) {
	mv := newTestMsgValidator()
	sender := types.HexToHash("0xcccc")
	if mv.PeerRateCount(sender) != 0 {
		t.Errorf("initial rate count = %d, want 0", mv.PeerRateCount(sender))
	}
	env := validEnvelope()
	env.SenderID = sender
	mv.Validate(env)
	if mv.PeerRateCount(sender) != 1 {
		t.Errorf("rate count = %d, want 1", mv.PeerRateCount(sender))
	}
}

func TestMsgValidator_ConcurrentAccess(t *testing.T) {
	mv := newTestMsgValidator()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			env := &GossipMsgEnvelope{
				Topic:     "blocks",
				Payload:   []byte{byte(n)},
				SenderID:  types.HexToHash("0xdddd"),
				MessageID: crypto.Keccak256Hash([]byte{byte(n), byte(n >> 8)}),
				Timestamp: uint64(time.Now().Unix()),
			}
			mv.Validate(env)
		}(i)
	}
	wg.Wait()
	stats := mv.Stats()
	if stats.TotalValidated != 50 {
		t.Errorf("TotalValidated = %d, want 50", stats.TotalValidated)
	}
}

func TestMsgValidator_BloomFilterFalsePositiveRate(t *testing.T) {
	bf := newBloomFilter(65536, 8)
	// Insert 100 items.
	for i := 0; i < 100; i++ {
		h := crypto.Keccak256Hash([]byte{byte(i), byte(i >> 8), byte(i >> 16)})
		bf.add(h)
	}
	// Check that all inserted items are found.
	for i := 0; i < 100; i++ {
		h := crypto.Keccak256Hash([]byte{byte(i), byte(i >> 8), byte(i >> 16)})
		if !bf.contains(h) {
			t.Errorf("bloom filter missed inserted item %d", i)
		}
	}
	// Check false positive rate on 1000 unseen items.
	fp := 0
	for i := 1000; i < 2000; i++ {
		h := crypto.Keccak256Hash([]byte{byte(i), byte(i >> 8), byte(i >> 16)})
		if bf.contains(h) {
			fp++
		}
	}
	// With 65536 bits and 100 insertions, FP rate should be very low.
	if fp > 50 { // Allow generous margin.
		t.Errorf("bloom filter false positive rate too high: %d/1000", fp)
	}
}

func TestMsgValidator_BloomReset(t *testing.T) {
	bf := newBloomFilter(1024, 4)
	h := crypto.Keccak256Hash([]byte("test"))
	bf.add(h)
	if !bf.contains(h) {
		t.Error("bloom should contain inserted item")
	}
	bf.reset()
	if bf.count() != 0 {
		t.Errorf("bloom bits after reset = %d, want 0", bf.count())
	}
}

func TestMsgValidator_StaleStats(t *testing.T) {
	mv := newTestMsgValidator()
	env := validEnvelope()
	env.Timestamp = uint64(time.Now().Add(-10 * time.Minute).Unix())
	mv.Validate(env)
	stats := mv.Stats()
	if stats.StaleCount != 1 {
		t.Errorf("StaleCount = %d, want 1", stats.StaleCount)
	}
}

func TestMsgValidator_RateLimitedStats(t *testing.T) {
	cfg := DefaultMsgValidatorConfig()
	cfg.RateLimitPerPeer = 1
	mv := NewMsgValidator(cfg)

	env1 := validEnvelope()
	mv.Validate(env1)
	env2 := validEnvelope()
	env2.MessageID = crypto.Keccak256Hash([]byte("unique"))
	mv.Validate(env2)

	stats := mv.Stats()
	if stats.RateLimited != 1 {
		t.Errorf("RateLimited = %d, want 1", stats.RateLimited)
	}
}
