package p2p

import (
	"encoding/binary"
	"math/big"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// makeSignedSetCodeMsg creates a SetCodeMessage with a real secp256k1 signature.
func makeSignedSetCodeMsg(nonce uint64) (*SetCodeMessage, error) {
	key, err := crypto.GenerateKey()
	if err != nil {
		return nil, err
	}
	authority := crypto.PubkeyToAddress(key.PublicKey)
	target := types.HexToAddress("0x2222222222222222222222222222222222222222")
	chainID := big.NewInt(1)

	msg := &SetCodeMessage{
		Authority: authority,
		ChainID:   chainID,
		Nonce:     nonce,
		Target:    target,
		Timestamp: time.Now(),
	}

	// Compute message hash using the same logic as computeSetCodeMessageHash.
	msgHash := computeSetCodeMessageHash(msg)

	sig, err := crypto.Sign(msgHash[:], key)
	if err != nil {
		return nil, err
	}

	msg.R = new(big.Int).SetBytes(sig[0:32])
	msg.S = new(big.Int).SetBytes(sig[32:64])
	msg.V = new(big.Int).SetUint64(uint64(sig[64]))

	return msg, nil
}

// makeSetCodeMsg creates a basic SetCodeMessage with fake (but well-formed) R/S values.
// This is used for tests that don't need real signature verification.
func makeSetCodeMsg(authority types.Address, nonce uint64) *SetCodeMessage {
	return &SetCodeMessage{
		Authority: authority,
		ChainID:   big.NewInt(1),
		Nonce:     nonce,
		Target:    types.HexToAddress("0x2222222222222222222222222222222222222222"),
		V:         big.NewInt(0),
		R:         big.NewInt(123456789),
		S:         big.NewInt(987654321),
		Timestamp: time.Now(),
	}
}

func TestSetCodeBroadcasterNew(t *testing.T) {
	b := NewSetCodeBroadcaster(big.NewInt(1))
	if b == nil {
		t.Fatal("expected non-nil broadcaster")
	}
	if b.PendingCount() != 0 {
		t.Errorf("pending: got %d, want 0", b.PendingCount())
	}
}

func TestSetCodeBroadcasterTopicName(t *testing.T) {
	b := NewSetCodeBroadcaster(big.NewInt(1))
	expected := "setcode_auth/1"
	if b.TopicName() != expected {
		t.Errorf("topic: got %q, want %q", b.TopicName(), expected)
	}

	b2 := NewSetCodeBroadcaster(nil)
	if b2.TopicName() != "setcode_auth/0" {
		t.Errorf("nil chain topic: got %q", b2.TopicName())
	}
}

func TestSetCodeBroadcasterSubmitSigned(t *testing.T) {
	b := NewSetCodeBroadcaster(big.NewInt(1))

	msg, err := makeSignedSetCodeMsg(1)
	if err != nil {
		t.Fatalf("makeSignedSetCodeMsg: %v", err)
	}

	if err := b.Submit(msg); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if b.PendingCount() != 1 {
		t.Errorf("pending: got %d, want 1", b.PendingCount())
	}
}

func TestSetCodeBroadcasterSubmitNil(t *testing.T) {
	b := NewSetCodeBroadcaster(big.NewInt(1))
	if err := b.Submit(nil); err != ErrSetCodeNilMessage {
		t.Errorf("expected ErrSetCodeNilMessage, got %v", err)
	}
}

func TestSetCodeBroadcasterSubmitEmptyAuthority(t *testing.T) {
	b := NewSetCodeBroadcaster(big.NewInt(1))
	msg := makeSetCodeMsg(types.Address{}, 1)
	if err := b.Submit(msg); err != ErrSetCodeEmptyAuthority {
		t.Errorf("expected ErrSetCodeEmptyAuthority, got %v", err)
	}
}

func TestSetCodeBroadcasterSubmitInvalidChainID(t *testing.T) {
	b := NewSetCodeBroadcaster(big.NewInt(1))
	auth := types.HexToAddress("0x1111111111111111111111111111111111111111")
	msg := makeSetCodeMsg(auth, 1)
	msg.ChainID = big.NewInt(-1)
	if err := b.Submit(msg); err != ErrSetCodeInvalidChainID {
		t.Errorf("expected ErrSetCodeInvalidChainID, got %v", err)
	}
}

func TestSetCodeBroadcasterSubmitWrongChainID(t *testing.T) {
	b := NewSetCodeBroadcaster(big.NewInt(1))

	msg, err := makeSignedSetCodeMsg(1)
	if err != nil {
		t.Fatalf("makeSignedSetCodeMsg: %v", err)
	}
	msg.ChainID = big.NewInt(999) // different from broadcaster's chain ID

	if err := b.Submit(msg); err != ErrSetCodeInvalidChainID {
		t.Errorf("expected ErrSetCodeInvalidChainID for wrong chain ID, got %v", err)
	}
}

func TestSetCodeBroadcasterSubmitInvalidSig(t *testing.T) {
	b := NewSetCodeBroadcaster(big.NewInt(1))
	auth := types.HexToAddress("0x1111111111111111111111111111111111111111")
	msg := makeSetCodeMsg(auth, 1)
	msg.R = big.NewInt(0) // zero R is invalid
	if err := b.Submit(msg); err != ErrSetCodeInvalidSig {
		t.Errorf("expected ErrSetCodeInvalidSig, got %v", err)
	}
}

func TestSetCodeBroadcasterDeduplication(t *testing.T) {
	b := NewSetCodeBroadcaster(big.NewInt(1))

	msg1, err := makeSignedSetCodeMsg(1)
	if err != nil {
		t.Fatalf("makeSignedSetCodeMsg: %v", err)
	}

	if err := b.Submit(msg1); err != nil {
		t.Fatalf("Submit first: %v", err)
	}

	// Same authority + nonce = duplicate.
	msg2 := *msg1
	if err := b.Submit(&msg2); err != ErrSetCodeDuplicate {
		t.Errorf("expected ErrSetCodeDuplicate, got %v", err)
	}
}

func TestSetCodeBroadcasterDifferentNonces(t *testing.T) {
	b := NewSetCodeBroadcaster(big.NewInt(1))

	msg1, err := makeSignedSetCodeMsg(1)
	if err != nil {
		t.Fatalf("makeSignedSetCodeMsg(1): %v", err)
	}
	if err := b.Submit(msg1); err != nil {
		t.Fatalf("Submit nonce 1: %v", err)
	}

	msg2, err := makeSignedSetCodeMsg(2)
	if err != nil {
		t.Fatalf("makeSignedSetCodeMsg(2): %v", err)
	}
	if err := b.Submit(msg2); err != nil {
		t.Fatalf("Submit nonce 2: %v", err)
	}

	if b.PendingCount() != 2 {
		t.Errorf("pending: got %d, want 2", b.PendingCount())
	}
}

func TestSetCodeBroadcasterDrainPending(t *testing.T) {
	b := NewSetCodeBroadcaster(big.NewInt(1))

	msg1, _ := makeSignedSetCodeMsg(1)
	msg2, _ := makeSignedSetCodeMsg(2)
	b.Submit(msg1)
	b.Submit(msg2)

	msgs := b.DrainPending()
	if len(msgs) != 2 {
		t.Errorf("drained: got %d, want 2", len(msgs))
	}
	if b.PendingCount() != 0 {
		t.Errorf("after drain: got %d, want 0", b.PendingCount())
	}
}

func TestSetCodeBroadcasterStop(t *testing.T) {
	b := NewSetCodeBroadcaster(big.NewInt(1))
	b.Stop()

	msg, _ := makeSignedSetCodeMsg(1)
	if err := b.Submit(msg); err != ErrSetCodeBroadcasterStop {
		t.Errorf("expected ErrSetCodeBroadcasterStop, got %v", err)
	}
}

func TestSetCodeBroadcasterResetEpoch(t *testing.T) {
	b := NewSetCodeBroadcaster(big.NewInt(1))

	msg, _ := makeSignedSetCodeMsg(1)
	b.Submit(msg)
	b.ResetEpoch()

	// After reset, same key should be submittable again.
	if err := b.Submit(msg); err != nil {
		t.Errorf("after reset, submit should succeed: %v", err)
	}
}

func TestSetCodeBroadcasterHandler(t *testing.T) {
	b := NewSetCodeBroadcaster(big.NewInt(1))
	var received *SetCodeMessage

	handler := SetCodeGossipHandlerFunc(func(msg *SetCodeMessage) error {
		received = msg
		return nil
	})
	b.AddHandler(handler)

	msg, _ := makeSignedSetCodeMsg(1)
	b.Submit(msg)

	if received == nil {
		t.Fatal("handler should have received the message")
	}
	if received.Authority != msg.Authority {
		t.Error("handler received wrong authority")
	}
}

func TestSetCodeValidateAuthWithRealSignature(t *testing.T) {
	msg, err := makeSignedSetCodeMsg(42)
	if err != nil {
		t.Fatalf("makeSignedSetCodeMsg: %v", err)
	}
	if !ValidateSetCodeAuth(msg) {
		t.Error("properly signed message should pass validation")
	}
}

func TestSetCodeValidateAuthFakeSignature(t *testing.T) {
	auth := types.HexToAddress("0x9999999999999999999999999999999999999999")
	msg := makeSetCodeMsg(auth, 1)
	// Fake R/S will not recover to the claimed authority.
	if ValidateSetCodeAuth(msg) {
		t.Error("fake signature should not pass validation with ecrecover")
	}
}

func TestSetCodeValidateAuthNil(t *testing.T) {
	if ValidateSetCodeAuth(nil) {
		t.Error("nil message should not pass validation")
	}
}

func TestSetCodeValidateAuthEmptyAuthority(t *testing.T) {
	msg := makeSetCodeMsg(types.Address{}, 1)
	if ValidateSetCodeAuth(msg) {
		t.Error("empty authority should not pass validation")
	}
}

func TestSetCodeValidateAuthNilSig(t *testing.T) {
	auth := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	msg := makeSetCodeMsg(auth, 1)
	msg.R = nil
	if ValidateSetCodeAuth(msg) {
		t.Error("nil R should not pass validation")
	}
}

func TestSetCodeValidateAuthBadV(t *testing.T) {
	auth := types.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	msg := makeSetCodeMsg(auth, 1)
	msg.V = big.NewInt(2) // must be 0 or 1
	if ValidateSetCodeAuth(msg) {
		t.Error("V=2 should not pass validation")
	}
}

func TestSetCodeValidateAuthWithChainID(t *testing.T) {
	msg, err := makeSignedSetCodeMsg(1)
	if err != nil {
		t.Fatalf("makeSignedSetCodeMsg: %v", err)
	}

	// Should pass with matching chain ID.
	if !ValidateSetCodeAuthWithChainID(msg, big.NewInt(1)) {
		t.Error("valid message with matching chain ID should pass")
	}

	// Should fail with mismatched chain ID.
	if ValidateSetCodeAuthWithChainID(msg, big.NewInt(999)) {
		t.Error("message with wrong chain ID should not pass")
	}

	// Nil localChainID should fail.
	if ValidateSetCodeAuthWithChainID(msg, nil) {
		t.Error("nil local chain ID should not pass")
	}
}

func TestSetCodeMessageHash(t *testing.T) {
	auth := types.HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc")
	msg1 := makeSetCodeMsg(auth, 1)
	msg2 := makeSetCodeMsg(auth, 2)

	h1 := msg1.Hash()
	h2 := msg2.Hash()

	if h1 == h2 {
		t.Error("different nonces should produce different hashes")
	}
	if h1 == (types.Hash{}) {
		t.Error("hash should not be zero")
	}
}

func TestSetCodeMessageDeduplicationKey(t *testing.T) {
	auth := types.HexToAddress("0xdddddddddddddddddddddddddddddddddddddd")
	msg := makeSetCodeMsg(auth, 42)
	key := msg.DeduplicationKey()
	if key == (types.Hash{}) {
		t.Error("dedup key should not be zero")
	}

	// Same authority + nonce -> same key.
	msg2 := makeSetCodeMsg(auth, 42)
	if msg.DeduplicationKey() != msg2.DeduplicationKey() {
		t.Error("same authority+nonce should produce same dedup key")
	}
}

func TestSetCodeBroadcasterSetRateLimit(t *testing.T) {
	b := NewSetCodeBroadcaster(big.NewInt(1))
	b.SetRateLimit(5)

	// Use the same key for all messages so they share the same authority.
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	authority := crypto.PubkeyToAddress(key.PublicKey)
	target := types.HexToAddress("0x2222222222222222222222222222222222222222")

	for i := uint64(0); i < 5; i++ {
		msg := &SetCodeMessage{
			Authority: authority,
			ChainID:   big.NewInt(1),
			Nonce:     i,
			Target:    target,
			Timestamp: time.Now(),
		}
		msgHash := computeSetCodeMessageHash(msg)
		sig, err := crypto.Sign(msgHash[:], key)
		if err != nil {
			t.Fatalf("Sign(%d): %v", i, err)
		}
		msg.R = new(big.Int).SetBytes(sig[0:32])
		msg.S = new(big.Int).SetBytes(sig[32:64])
		msg.V = new(big.Int).SetUint64(uint64(sig[64]))

		if err := b.Submit(msg); err != nil {
			t.Fatalf("Submit[%d]: %v", i, err)
		}
	}

	// 6th with same authority should be rate limited.
	msg := &SetCodeMessage{
		Authority: authority,
		ChainID:   big.NewInt(1),
		Nonce:     5,
		Target:    target,
		Timestamp: time.Now(),
	}
	msgHash := computeSetCodeMessageHash(msg)
	sig, err := crypto.Sign(msgHash[:], key)
	if err != nil {
		t.Fatalf("Sign(5): %v", err)
	}
	msg.R = new(big.Int).SetBytes(sig[0:32])
	msg.S = new(big.Int).SetBytes(sig[32:64])
	msg.V = new(big.Int).SetUint64(uint64(sig[64]))

	if err := b.Submit(msg); err != ErrSetCodeRateLimited {
		t.Errorf("expected ErrSetCodeRateLimited, got %v", err)
	}
}

// computeSetCodeMessageHashForTest is a test helper that replicates the hash computation.
func computeSetCodeMessageHashForTest(msg *SetCodeMessage) types.Hash {
	var buf []byte
	buf = append(buf, msg.Authority[:]...)
	if msg.ChainID != nil {
		chainIDBytes := msg.ChainID.Bytes()
		padded := make([]byte, 32)
		copy(padded[32-len(chainIDBytes):], chainIDBytes)
		buf = append(buf, padded...)
	}
	var nonceBuf [8]byte
	binary.BigEndian.PutUint64(nonceBuf[:], msg.Nonce)
	buf = append(buf, nonceBuf[:]...)
	buf = append(buf, msg.Target[:]...)
	return crypto.Keccak256Hash(buf)
}

func TestComputeSetCodeMessageHash(t *testing.T) {
	auth := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	msg := makeSetCodeMsg(auth, 1)

	h1 := computeSetCodeMessageHash(msg)
	h2 := computeSetCodeMessageHashForTest(msg)

	if h1 != h2 {
		t.Error("message hashes should match")
	}
	if h1 == (types.Hash{}) {
		t.Error("message hash should not be zero")
	}
}
