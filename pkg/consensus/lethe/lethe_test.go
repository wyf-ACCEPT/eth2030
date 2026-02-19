package lethe

import (
	"bytes"
	"errors"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

func addr(b byte) types.Address {
	var a types.Address
	a[types.AddressLength-1] = b
	return a
}

func channelID(b byte) types.Hash {
	var h types.Hash
	h[types.HashLength-1] = b
	return h
}

func TestNewChannel(t *testing.T) {
	participants := []types.Address{addr(1), addr(2), addr(3)}
	ch := NewChannel(channelID(1), participants)

	if ch == nil {
		t.Fatal("channel should not be nil")
	}
	if ch.ID != channelID(1) {
		t.Errorf("unexpected channel ID")
	}
	addrs := ch.Participants()
	if len(addrs) != 3 {
		t.Errorf("expected 3 participants, got %d", len(addrs))
	}
	if ch.IsClosed() {
		t.Error("new channel should not be closed")
	}
}

func TestEncryptDecrypt(t *testing.T) {
	alice := addr(1)
	bob := addr(2)
	ch := NewChannel(channelID(1), []types.Address{alice, bob})

	plaintext := []byte("secret execution payload for the LETHE channel")

	ciphertext, err := ch.EncryptPayload(alice, plaintext)
	if err != nil {
		t.Fatalf("EncryptPayload: %v", err)
	}

	if bytes.Equal(ciphertext, plaintext) {
		t.Error("ciphertext should differ from plaintext")
	}

	// Bob decrypts.
	decrypted, err := ch.DecryptPayload(bob, ciphertext)
	if err != nil {
		t.Fatalf("DecryptPayload: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("decrypted text does not match: got %q, want %q", decrypted, plaintext)
	}

	// Alice can also decrypt (shared key).
	decrypted2, err := ch.DecryptPayload(alice, ciphertext)
	if err != nil {
		t.Fatalf("DecryptPayload by sender: %v", err)
	}
	if !bytes.Equal(decrypted2, plaintext) {
		t.Error("sender should be able to decrypt own message")
	}
}

func TestEncryptDecrypt_EmptyPayload(t *testing.T) {
	alice := addr(1)
	ch := NewChannel(channelID(1), []types.Address{alice})

	ciphertext, err := ch.EncryptPayload(alice, []byte{})
	if err != nil {
		t.Fatalf("encrypt empty: %v", err)
	}

	decrypted, err := ch.DecryptPayload(alice, ciphertext)
	if err != nil {
		t.Fatalf("decrypt empty: %v", err)
	}
	if len(decrypted) != 0 {
		t.Errorf("expected empty decrypted payload, got %d bytes", len(decrypted))
	}
}

func TestEncrypt_NonParticipant(t *testing.T) {
	alice := addr(1)
	eve := addr(99)
	ch := NewChannel(channelID(1), []types.Address{alice})

	_, err := ch.EncryptPayload(eve, []byte("hello"))
	if err != ErrNotParticipant {
		t.Errorf("expected ErrNotParticipant, got %v", err)
	}
}

func TestDecrypt_NonParticipant(t *testing.T) {
	alice := addr(1)
	eve := addr(99)
	ch := NewChannel(channelID(1), []types.Address{alice})

	ciphertext, err := ch.EncryptPayload(alice, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = ch.DecryptPayload(eve, ciphertext)
	if err != ErrNotParticipant {
		t.Errorf("expected ErrNotParticipant, got %v", err)
	}
}

func TestDecrypt_TamperedCiphertext(t *testing.T) {
	alice := addr(1)
	ch := NewChannel(channelID(1), []types.Address{alice})

	ciphertext, err := ch.EncryptPayload(alice, []byte("important data"))
	if err != nil {
		t.Fatal(err)
	}

	// Tamper with the ciphertext (flip a bit in the middle).
	tampered := make([]byte, len(ciphertext))
	copy(tampered, ciphertext)
	tampered[len(tampered)/2] ^= 0xFF

	_, err = ch.DecryptPayload(alice, tampered)
	if err != ErrDecryptionFailed {
		t.Errorf("expected ErrDecryptionFailed for tampered data, got %v", err)
	}
}

func TestDecrypt_TooShort(t *testing.T) {
	alice := addr(1)
	ch := NewChannel(channelID(1), []types.Address{alice})

	_, err := ch.DecryptPayload(alice, []byte{0x01, 0x02})
	if err != ErrPayloadTooShort {
		t.Errorf("expected ErrPayloadTooShort, got %v", err)
	}
}

func TestEncrypt_ClosedChannel(t *testing.T) {
	alice := addr(1)
	ch := NewChannel(channelID(1), []types.Address{alice})
	ch.Close()

	_, err := ch.EncryptPayload(alice, []byte("hello"))
	if err != ErrChannelClosed {
		t.Errorf("expected ErrChannelClosed, got %v", err)
	}
}

func TestDecrypt_ClosedChannel(t *testing.T) {
	alice := addr(1)
	ch := NewChannel(channelID(1), []types.Address{alice})
	ch.Close()

	_, err := ch.DecryptPayload(alice, []byte("doesn't matter"))
	if err != ErrChannelClosed {
		t.Errorf("expected ErrChannelClosed, got %v", err)
	}
}

func TestAddParticipant(t *testing.T) {
	alice := addr(1)
	bob := addr(2)
	ch := NewChannel(channelID(1), []types.Address{alice})

	pubKey := []byte("bob-ephemeral-key-32-bytes-long!")
	if err := ch.AddParticipant(bob, pubKey); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}

	addrs := ch.Participants()
	if len(addrs) != 2 {
		t.Errorf("expected 2 participants, got %d", len(addrs))
	}

	// New participant should be able to decrypt messages.
	ciphertext, err := ch.EncryptPayload(alice, []byte("hello bob"))
	if err != nil {
		t.Fatal(err)
	}
	dec, err := ch.DecryptPayload(bob, ciphertext)
	if err != nil {
		t.Fatalf("new participant decrypt: %v", err)
	}
	if string(dec) != "hello bob" {
		t.Errorf("unexpected decrypted text: %q", dec)
	}
}

func TestAddParticipant_Duplicate(t *testing.T) {
	alice := addr(1)
	ch := NewChannel(channelID(1), []types.Address{alice})

	err := ch.AddParticipant(alice, nil)
	if err != ErrAlreadyParticipant {
		t.Errorf("expected ErrAlreadyParticipant, got %v", err)
	}
}

func TestAddParticipant_ClosedChannel(t *testing.T) {
	alice := addr(1)
	ch := NewChannel(channelID(1), []types.Address{alice})
	ch.Close()

	err := ch.AddParticipant(addr(2), nil)
	if err != ErrChannelClosed {
		t.Errorf("expected ErrChannelClosed, got %v", err)
	}
}

func TestRemoveParticipant(t *testing.T) {
	alice := addr(1)
	bob := addr(2)
	ch := NewChannel(channelID(1), []types.Address{alice, bob})

	// Encrypt before removal.
	ciphertext, err := ch.EncryptPayload(alice, []byte("pre-removal"))
	if err != nil {
		t.Fatal(err)
	}

	// Remove bob.
	if err := ch.RemoveParticipant(bob); err != nil {
		t.Fatalf("RemoveParticipant: %v", err)
	}

	addrs := ch.Participants()
	if len(addrs) != 1 {
		t.Errorf("expected 1 participant after removal, got %d", len(addrs))
	}

	// Old ciphertext should NOT decrypt with the new key (key re-derivation).
	_, err = ch.DecryptPayload(alice, ciphertext)
	if err != ErrDecryptionFailed {
		t.Errorf("expected ErrDecryptionFailed after key change, got %v", err)
	}
}

func TestRemoveParticipant_NotMember(t *testing.T) {
	alice := addr(1)
	ch := NewChannel(channelID(1), []types.Address{alice})

	err := ch.RemoveParticipant(addr(99))
	if err != ErrNotParticipant {
		t.Errorf("expected ErrNotParticipant, got %v", err)
	}
}

func TestRemoveParticipant_LastMember(t *testing.T) {
	alice := addr(1)
	ch := NewChannel(channelID(1), []types.Address{alice})

	err := ch.RemoveParticipant(alice)
	if err != ErrNoParticipants {
		t.Errorf("expected ErrNoParticipants, got %v", err)
	}
}

func TestRemoveParticipant_Closed(t *testing.T) {
	alice := addr(1)
	bob := addr(2)
	ch := NewChannel(channelID(1), []types.Address{alice, bob})
	ch.Close()

	err := ch.RemoveParticipant(bob)
	if err != ErrChannelClosed {
		t.Errorf("expected ErrChannelClosed, got %v", err)
	}
}

func TestKeyDerivation_Deterministic(t *testing.T) {
	// Same ID + same participants should produce the same key.
	id := channelID(42)
	addrs := []types.Address{addr(1), addr(2), addr(3)}

	ch1 := NewChannel(id, addrs)
	ch2 := NewChannel(id, addrs)

	if !bytes.Equal(ch1.symKey, ch2.symKey) {
		t.Error("same channel parameters should produce same key")
	}

	// Different order should still produce same key.
	reversed := []types.Address{addr(3), addr(1), addr(2)}
	ch3 := NewChannel(id, reversed)
	if !bytes.Equal(ch1.symKey, ch3.symKey) {
		t.Error("participant order should not affect key derivation")
	}
}

func TestKeyDerivation_DifferentIDs(t *testing.T) {
	addrs := []types.Address{addr(1), addr(2)}
	ch1 := NewChannel(channelID(1), addrs)
	ch2 := NewChannel(channelID(2), addrs)

	if bytes.Equal(ch1.symKey, ch2.symKey) {
		t.Error("different channel IDs should produce different keys")
	}
}

func TestKeyDerivation_DifferentParticipants(t *testing.T) {
	id := channelID(1)
	ch1 := NewChannel(id, []types.Address{addr(1), addr(2)})
	ch2 := NewChannel(id, []types.Address{addr(1), addr(3)})

	if bytes.Equal(ch1.symKey, ch2.symKey) {
		t.Error("different participants should produce different keys")
	}
}

func TestKeyDerivation_UsesKeccak256(t *testing.T) {
	id := channelID(1)
	addrs := []types.Address{addr(1), addr(2)}

	ch := NewChannel(id, addrs)

	// Manually compute expected key.
	sorted := []types.Address{addr(1), addr(2)}
	sortAddresses(sorted)
	preimage := make([]byte, 0)
	preimage = append(preimage, id[:]...)
	for _, a := range sorted {
		preimage = append(preimage, a[:]...)
	}
	expected := crypto.Keccak256(preimage)

	if !bytes.Equal(ch.symKey, expected) {
		t.Error("key should match manual Keccak256 derivation")
	}
}

// --- ChannelManager tests ---

func TestChannelManager_CreateAndGet(t *testing.T) {
	cm := NewChannelManager()

	ch, err := cm.CreateChannel([]types.Address{addr(1), addr(2)})
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if ch == nil {
		t.Fatal("created channel should not be nil")
	}

	// Retrieve by ID.
	got, err := cm.GetChannel(ch.ID)
	if err != nil {
		t.Fatalf("GetChannel: %v", err)
	}
	if got.ID != ch.ID {
		t.Error("retrieved channel ID mismatch")
	}
}

func TestChannelManager_CreateNoParticipants(t *testing.T) {
	cm := NewChannelManager()
	_, err := cm.CreateChannel(nil)
	if err != ErrNoParticipants {
		t.Errorf("expected ErrNoParticipants, got %v", err)
	}

	_, err = cm.CreateChannel([]types.Address{})
	if err != ErrNoParticipants {
		t.Errorf("expected ErrNoParticipants for empty slice, got %v", err)
	}
}

func TestChannelManager_GetNotFound(t *testing.T) {
	cm := NewChannelManager()
	_, err := cm.GetChannel(channelID(99))
	if err != ErrChannelNotFound {
		t.Errorf("expected ErrChannelNotFound, got %v", err)
	}
}

func TestChannelManager_GetZeroID(t *testing.T) {
	cm := NewChannelManager()
	_, err := cm.GetChannel(types.Hash{})
	if err != ErrInvalidChannelID {
		t.Errorf("expected ErrInvalidChannelID for zero hash, got %v", err)
	}
}

func TestChannelManager_CloseChannel(t *testing.T) {
	cm := NewChannelManager()

	ch, err := cm.CreateChannel([]types.Address{addr(1)})
	if err != nil {
		t.Fatal(err)
	}

	if err := cm.CloseChannel(ch.ID); err != nil {
		t.Fatalf("CloseChannel: %v", err)
	}

	if cm.ChannelCount() != 0 {
		t.Errorf("expected 0 channels after close, got %d", cm.ChannelCount())
	}

	// Verify channel is actually closed.
	if !ch.IsClosed() {
		t.Error("channel should be closed")
	}

	// Double close should fail.
	if err := cm.CloseChannel(ch.ID); err != ErrChannelNotFound {
		t.Errorf("expected ErrChannelNotFound on double close, got %v", err)
	}
}

func TestChannelManager_ListActiveChannels(t *testing.T) {
	cm := NewChannelManager()

	ch1, _ := cm.CreateChannel([]types.Address{addr(1)})
	ch2, _ := cm.CreateChannel([]types.Address{addr(2)})
	_, _ = cm.CreateChannel([]types.Address{addr(3)})

	active := cm.ListActiveChannels()
	if len(active) != 3 {
		t.Errorf("expected 3 active channels, got %d", len(active))
	}

	// Close one.
	cm.CloseChannel(ch1.ID)
	active = cm.ListActiveChannels()
	if len(active) != 2 {
		t.Errorf("expected 2 active channels after close, got %d", len(active))
	}

	// Close another.
	cm.CloseChannel(ch2.ID)
	active = cm.ListActiveChannels()
	if len(active) != 1 {
		t.Errorf("expected 1 active channel, got %d", len(active))
	}
}

func TestChannelManager_ChannelCount(t *testing.T) {
	cm := NewChannelManager()

	if cm.ChannelCount() != 0 {
		t.Errorf("expected 0, got %d", cm.ChannelCount())
	}

	cm.CreateChannel([]types.Address{addr(1)})
	cm.CreateChannel([]types.Address{addr(2)})

	if cm.ChannelCount() != 2 {
		t.Errorf("expected 2, got %d", cm.ChannelCount())
	}
}

func TestChannelManager_EndToEnd(t *testing.T) {
	cm := NewChannelManager()
	alice := addr(1)
	bob := addr(2)

	ch, err := cm.CreateChannel([]types.Address{alice, bob})
	if err != nil {
		t.Fatal(err)
	}

	// Alice encrypts.
	msg := []byte("cross-validator confidential execution result")
	ciphertext, err := ch.EncryptPayload(alice, msg)
	if err != nil {
		t.Fatal(err)
	}

	// Bob decrypts through the manager's channel.
	retrieved, _ := cm.GetChannel(ch.ID)
	decrypted, err := retrieved.DecryptPayload(bob, ciphertext)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(decrypted, msg) {
		t.Errorf("end-to-end failed: got %q want %q", decrypted, msg)
	}
}

func TestChannelManager_ConcurrentAccess(t *testing.T) {
	cm := NewChannelManager()
	var wg sync.WaitGroup
	errs := make(chan error, 100)

	// Concurrent channel creation.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := cm.CreateChannel([]types.Address{addr(byte(idx))})
			if err != nil {
				errs <- err
			}
		}(i)
	}

	// Concurrent reads.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cm.ListActiveChannels()
			cm.ChannelCount()
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent error: %v", err)
	}

	if cm.ChannelCount() != 20 {
		t.Errorf("expected 20 channels, got %d", cm.ChannelCount())
	}
}

func TestChannel_ConcurrentEncryptDecrypt(t *testing.T) {
	alice := addr(1)
	bob := addr(2)
	ch := NewChannel(channelID(1), []types.Address{alice, bob})

	var wg sync.WaitGroup
	errs := make(chan error, 100)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			msg := []byte("concurrent message")
			ct, err := ch.EncryptPayload(alice, msg)
			if err != nil {
				errs <- err
				return
			}
			dec, err := ch.DecryptPayload(bob, ct)
			if err != nil {
				errs <- err
				return
			}
			if !bytes.Equal(dec, msg) {
				errs <- errors.New("decrypted mismatch in concurrent test")
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent encrypt/decrypt error: %v", err)
	}
}

func TestEncryptProducesUniqueNonces(t *testing.T) {
	alice := addr(1)
	ch := NewChannel(channelID(1), []types.Address{alice})

	msg := []byte("same message")
	ct1, err := ch.EncryptPayload(alice, msg)
	if err != nil {
		t.Fatal(err)
	}
	ct2, err := ch.EncryptPayload(alice, msg)
	if err != nil {
		t.Fatal(err)
	}

	// Two encryptions of the same plaintext should differ due to random nonce.
	if bytes.Equal(ct1, ct2) {
		t.Error("two encryptions of same plaintext should produce different ciphertext")
	}
}

func TestClose_ZerosKeyMaterial(t *testing.T) {
	alice := addr(1)
	ch := NewChannel(channelID(1), []types.Address{alice})

	// Key should be non-zero before close.
	allZero := true
	for _, b := range ch.symKey {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("key should not be all zeros before close")
	}

	ch.Close()

	// Key should be zeroed after close.
	for i, b := range ch.symKey {
		if b != 0 {
			t.Errorf("key byte %d not zeroed after close: %x", i, b)
		}
	}
}

func TestSortAddresses(t *testing.T) {
	a1 := addr(3)
	a2 := addr(1)
	a3 := addr(2)

	addrs := []types.Address{a1, a2, a3}
	sortAddresses(addrs)

	if addrs[0] != a2 || addrs[1] != a3 || addrs[2] != a1 {
		t.Errorf("unexpected sort order: %v", addrs)
	}
}

// Ensure errors import is used.
var _ = errors.New
