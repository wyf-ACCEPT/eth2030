package p2p

import (
	"math/big"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

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

func TestSetCodeBroadcasterSubmit(t *testing.T) {
	b := NewSetCodeBroadcaster(big.NewInt(1))
	auth := types.HexToAddress("0x1111111111111111111111111111111111111111")
	msg := makeSetCodeMsg(auth, 1)

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
	auth := types.HexToAddress("0x3333333333333333333333333333333333333333")

	msg1 := makeSetCodeMsg(auth, 1)
	msg2 := makeSetCodeMsg(auth, 1) // same authority + nonce

	if err := b.Submit(msg1); err != nil {
		t.Fatalf("Submit first: %v", err)
	}
	if err := b.Submit(msg2); err != ErrSetCodeDuplicate {
		t.Errorf("expected ErrSetCodeDuplicate, got %v", err)
	}
}

func TestSetCodeBroadcasterDifferentNonces(t *testing.T) {
	b := NewSetCodeBroadcaster(big.NewInt(1))
	auth := types.HexToAddress("0x4444444444444444444444444444444444444444")

	if err := b.Submit(makeSetCodeMsg(auth, 1)); err != nil {
		t.Fatalf("Submit nonce 1: %v", err)
	}
	if err := b.Submit(makeSetCodeMsg(auth, 2)); err != nil {
		t.Fatalf("Submit nonce 2: %v", err)
	}
	if b.PendingCount() != 2 {
		t.Errorf("pending: got %d, want 2", b.PendingCount())
	}
}

func TestSetCodeBroadcasterDrainPending(t *testing.T) {
	b := NewSetCodeBroadcaster(big.NewInt(1))
	auth := types.HexToAddress("0x5555555555555555555555555555555555555555")

	b.Submit(makeSetCodeMsg(auth, 1))
	b.Submit(makeSetCodeMsg(auth, 2))

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

	auth := types.HexToAddress("0x6666666666666666666666666666666666666666")
	if err := b.Submit(makeSetCodeMsg(auth, 1)); err != ErrSetCodeBroadcasterStop {
		t.Errorf("expected ErrSetCodeBroadcasterStop, got %v", err)
	}
}

func TestSetCodeBroadcasterResetEpoch(t *testing.T) {
	b := NewSetCodeBroadcaster(big.NewInt(1))
	auth := types.HexToAddress("0x7777777777777777777777777777777777777777")

	b.Submit(makeSetCodeMsg(auth, 1))
	b.ResetEpoch()

	// After reset, same key should be submittable again.
	if err := b.Submit(makeSetCodeMsg(auth, 1)); err != nil {
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

	auth := types.HexToAddress("0x8888888888888888888888888888888888888888")
	msg := makeSetCodeMsg(auth, 1)
	b.Submit(msg)

	if received == nil {
		t.Fatal("handler should have received the message")
	}
	if received.Authority != auth {
		t.Error("handler received wrong authority")
	}
}

func TestSetCodeValidateAuth(t *testing.T) {
	auth := types.HexToAddress("0x9999999999999999999999999999999999999999")
	msg := makeSetCodeMsg(auth, 1)
	if !ValidateSetCodeAuth(msg) {
		t.Error("valid message should pass validation")
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

	auth := types.HexToAddress("0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")
	for i := uint64(0); i < 5; i++ {
		if err := b.Submit(makeSetCodeMsg(auth, i)); err != nil {
			t.Fatalf("Submit[%d]: %v", i, err)
		}
	}
	// 6th should be rate limited.
	if err := b.Submit(makeSetCodeMsg(auth, 5)); err != ErrSetCodeRateLimited {
		t.Errorf("expected ErrSetCodeRateLimited, got %v", err)
	}
}
