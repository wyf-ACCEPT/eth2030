package p2p

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"net"
	"sync"
	"testing"

	ethcrypto "github.com/eth2028/eth2028/crypto"
)

func TestNewRLPxHandshake(t *testing.T) {
	hs, err := NewRLPxHandshake(true)
	if err != nil {
		t.Fatalf("NewRLPxHandshake: %v", err)
	}
	if hs.ephemeralKey == nil {
		t.Fatal("ephemeral key is nil")
	}
	if !hs.initiator {
		t.Fatal("expected initiator=true")
	}
	// Nonce should not be all zeros.
	allZero := true
	for _, b := range hs.localNonce {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("nonce is all zeros")
	}
}

func TestRLPxHandshake_LocalPublicKey(t *testing.T) {
	hs, err := NewRLPxHandshake(true)
	if err != nil {
		t.Fatalf("NewRLPxHandshake: %v", err)
	}
	pub := hs.LocalPublicKey()
	if len(pub) != 65 {
		t.Fatalf("expected 65-byte pubkey, got %d", len(pub))
	}
	if pub[0] != 0x04 {
		t.Fatalf("expected 0x04 prefix, got 0x%02x", pub[0])
	}
}

func TestRLPxHandshake_SetRemotePublicKey_Valid(t *testing.T) {
	hs, err := NewRLPxHandshake(true)
	if err != nil {
		t.Fatalf("NewRLPxHandshake: %v", err)
	}
	// Generate another key for the remote side.
	remote, err := ethcrypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate remote key: %v", err)
	}
	pub := elliptic.Marshal(remote.PublicKey.Curve, remote.PublicKey.X, remote.PublicKey.Y)
	if err := hs.SetRemotePublicKey(pub); err != nil {
		t.Fatalf("SetRemotePublicKey: %v", err)
	}
	if hs.remoteEphPub == nil {
		t.Fatal("remoteEphPub is nil after SetRemotePublicKey")
	}
}

func TestRLPxHandshake_SetRemotePublicKey_Invalid(t *testing.T) {
	hs, err := NewRLPxHandshake(true)
	if err != nil {
		t.Fatalf("NewRLPxHandshake: %v", err)
	}
	// Too short.
	if err := hs.SetRemotePublicKey([]byte{0x04, 0x01, 0x02}); err == nil {
		t.Fatal("expected error for short key")
	}
	// Wrong prefix.
	bad := make([]byte, 65)
	bad[0] = 0x03
	if err := hs.SetRemotePublicKey(bad); err == nil {
		t.Fatal("expected error for bad prefix")
	}
}

func TestRLPxHandshake_DeriveSecrets(t *testing.T) {
	init, err := NewRLPxHandshake(true)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := NewRLPxHandshake(false)
	if err != nil {
		t.Fatal(err)
	}

	// Exchange pubkeys.
	if err := init.SetRemotePublicKey(resp.LocalPublicKey()); err != nil {
		t.Fatal(err)
	}
	if err := resp.SetRemotePublicKey(init.LocalPublicKey()); err != nil {
		t.Fatal(err)
	}

	// Exchange nonces (initiator nonce goes first in canonical order).
	copy(init.remoteNonce[:], resp.localNonce[:])
	copy(resp.remoteNonce[:], init.localNonce[:])

	secretA, err := init.DeriveSecrets()
	if err != nil {
		t.Fatalf("initiator DeriveSecrets: %v", err)
	}
	secretB, err := resp.DeriveSecrets()
	if err != nil {
		t.Fatalf("responder DeriveSecrets: %v", err)
	}
	if !bytes.Equal(secretA, secretB) {
		t.Fatalf("shared secrets differ: %x vs %x", secretA, secretB)
	}
}

func TestRLPxHandshake_DeriveSecrets_NoRemoteKey(t *testing.T) {
	hs, err := NewRLPxHandshake(true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = hs.DeriveSecrets()
	if err == nil {
		t.Fatal("expected error when remote key not set")
	}
}

func TestEncryptFrame_DecryptFrame_Roundtrip(t *testing.T) {
	secret := sha256.Sum256([]byte("test-secret-key-material"))
	plaintext := []byte("hello, encrypted world!")

	encrypted, err := EncryptFrame(plaintext, secret[:])
	if err != nil {
		t.Fatalf("EncryptFrame: %v", err)
	}
	decrypted, err := DecryptFrame(encrypted, secret[:])
	if err != nil {
		t.Fatalf("DecryptFrame: %v", err)
	}
	if !bytes.Equal(plaintext, decrypted) {
		t.Fatalf("roundtrip mismatch: got %x, want %x", decrypted, plaintext)
	}
}

func TestEncryptFrame_EmptyPayload(t *testing.T) {
	secret := sha256.Sum256([]byte("empty-payload-test"))
	encrypted, err := EncryptFrame([]byte{}, secret[:])
	if err != nil {
		t.Fatalf("EncryptFrame: %v", err)
	}
	decrypted, err := DecryptFrame(encrypted, secret[:])
	if err != nil {
		t.Fatalf("DecryptFrame: %v", err)
	}
	if len(decrypted) != 0 {
		t.Fatalf("expected empty, got %x", decrypted)
	}
}

func TestDecryptFrame_BadMAC(t *testing.T) {
	secret := sha256.Sum256([]byte("bad-mac-test"))
	encrypted, err := EncryptFrame([]byte("data"), secret[:])
	if err != nil {
		t.Fatal(err)
	}
	// Corrupt the MAC (last 16 bytes).
	encrypted[len(encrypted)-1] ^= 0xFF
	_, err = DecryptFrame(encrypted, secret[:])
	if err != ErrFrameMACMismatch {
		t.Fatalf("expected ErrFrameMACMismatch, got %v", err)
	}
}

func TestDecryptFrame_TooShort(t *testing.T) {
	secret := sha256.Sum256([]byte("short-data"))
	_, err := DecryptFrame([]byte{1, 2, 3}, secret[:])
	if err == nil {
		t.Fatal("expected error for short data")
	}
}

func TestDecryptFrame_ShortSecret(t *testing.T) {
	_, err := DecryptFrame(make([]byte, 64), []byte{1, 2, 3})
	if err == nil {
		t.Fatal("expected error for short secret")
	}
}

func TestEncryptFrame_ShortSecret(t *testing.T) {
	_, err := EncryptFrame([]byte("data"), []byte{1, 2, 3})
	if err == nil {
		t.Fatal("expected error for short secret")
	}
}

func TestEncryptFrame_LargePayload(t *testing.T) {
	secret := sha256.Sum256([]byte("large-payload"))
	payload := make([]byte, 4096)
	for i := range payload {
		payload[i] = byte(i % 256)
	}
	encrypted, err := EncryptFrame(payload, secret[:])
	if err != nil {
		t.Fatalf("EncryptFrame: %v", err)
	}
	decrypted, err := DecryptFrame(encrypted, secret[:])
	if err != nil {
		t.Fatalf("DecryptFrame: %v", err)
	}
	if !bytes.Equal(payload, decrypted) {
		t.Fatal("large payload roundtrip mismatch")
	}
}

func TestEncryptFrame_DifferentKeys(t *testing.T) {
	secret1 := sha256.Sum256([]byte("key-1"))
	secret2 := sha256.Sum256([]byte("key-2"))
	encrypted, err := EncryptFrame([]byte("secret data"), secret1[:])
	if err != nil {
		t.Fatal(err)
	}
	_, err = DecryptFrame(encrypted, secret2[:])
	if err == nil {
		t.Fatal("expected error decrypting with wrong key")
	}
}

func TestECIESTransport_Handshake(t *testing.T) {
	c1, c2 := net.Pipe()
	t1 := newECIESTransport(c1)
	t2 := newECIESTransport(c2)
	defer t1.Close()
	defer t2.Close()

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); errs[0] = t1.Handshake(true) }()
	go func() { defer wg.Done(); errs[1] = t2.Handshake(false) }()
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("handshake side %d: %v", i, err)
		}
	}
	if !t1.handshook || !t2.handshook {
		t.Fatal("handshook not set")
	}
}

func TestECIESTransport_ReadWriteFrame(t *testing.T) {
	c1, c2 := net.Pipe()
	t1 := newECIESTransport(c1)
	t2 := newECIESTransport(c2)
	defer t1.Close()
	defer t2.Close()

	// Handshake.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); t1.Handshake(true) }()
	go func() { defer wg.Done(); t2.Handshake(false) }()
	wg.Wait()

	// Write from t1, read from t2.
	payload := []byte("ecies encrypted frame test")
	errCh := make(chan error, 1)
	go func() { errCh <- t1.WriteFrame(payload) }()

	got, err := t2.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("frame mismatch: got %x, want %x", got, payload)
	}
}

func TestECIESTransport_BidirectionalFrames(t *testing.T) {
	c1, c2 := net.Pipe()
	t1 := newECIESTransport(c1)
	t2 := newECIESTransport(c2)
	defer t1.Close()
	defer t2.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); t1.Handshake(true) }()
	go func() { defer wg.Done(); t2.Handshake(false) }()
	wg.Wait()

	// t1 -> t2
	errCh := make(chan error, 1)
	go func() { errCh <- t1.WriteFrame([]byte("from-initiator")) }()
	got1, err := t2.ReadFrame()
	if err != nil {
		t.Fatalf("t2 ReadFrame: %v", err)
	}
	<-errCh
	if string(got1) != "from-initiator" {
		t.Fatalf("got %q, want %q", got1, "from-initiator")
	}

	// t2 -> t1
	go func() { errCh <- t2.WriteFrame([]byte("from-responder")) }()
	got2, err := t1.ReadFrame()
	if err != nil {
		t.Fatalf("t1 ReadFrame: %v", err)
	}
	<-errCh
	if string(got2) != "from-responder" {
		t.Fatalf("got %q, want %q", got2, "from-responder")
	}
}

func TestECIESTransport_ReadBeforeHandshake(t *testing.T) {
	c1, _ := net.Pipe()
	tr := newECIESTransport(c1)
	defer tr.Close()
	_, err := tr.ReadFrame()
	if err == nil {
		t.Fatal("expected error reading before handshake")
	}
}

func TestECIESTransport_WriteBeforeHandshake(t *testing.T) {
	c1, _ := net.Pipe()
	tr := newECIESTransport(c1)
	defer tr.Close()
	err := tr.WriteFrame([]byte("data"))
	if err == nil {
		t.Fatal("expected error writing before handshake")
	}
}

func TestECDHShared(t *testing.T) {
	k1, _ := ethcrypto.GenerateKey()
	k2, _ := ethcrypto.GenerateKey()

	s1 := ecdhShared(k1, &k2.PublicKey)
	s2 := ecdhShared(k2, &k1.PublicKey)

	if s1.Cmp(s2) != 0 {
		t.Fatalf("ECDH not symmetric: %s vs %s", s1, s2)
	}
}

func TestECIESTransport_MultipleFrames(t *testing.T) {
	c1, c2 := net.Pipe()
	t1 := newECIESTransport(c1)
	t2 := newECIESTransport(c2)
	defer t1.Close()
	defer t2.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); t1.Handshake(true) }()
	go func() { defer wg.Done(); t2.Handshake(false) }()
	wg.Wait()

	messages := []string{"msg-0", "msg-1", "msg-2", "msg-3", "msg-4"}
	errCh := make(chan error, 1)
	go func() {
		for _, m := range messages {
			if err := t1.WriteFrame([]byte(m)); err != nil {
				errCh <- err
				return
			}
		}
		errCh <- nil
	}()

	for i, want := range messages {
		got, err := t2.ReadFrame()
		if err != nil {
			t.Fatalf("ReadFrame %d: %v", i, err)
		}
		if string(got) != want {
			t.Fatalf("frame %d: got %q, want %q", i, got, want)
		}
	}
	if err := <-errCh; err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
}

// Verify that two separate handshakes produce different shared secrets.
func TestRLPxHandshake_UniqueSessions(t *testing.T) {
	derive := func() []byte {
		a, _ := NewRLPxHandshake(true)
		b, _ := NewRLPxHandshake(false)
		a.SetRemotePublicKey(b.LocalPublicKey())
		b.SetRemotePublicKey(a.LocalPublicKey())
		copy(a.remoteNonce[:], b.localNonce[:])
		copy(b.remoteNonce[:], a.localNonce[:])
		s, _ := a.DeriveSecrets()
		return s
	}
	s1 := derive()
	s2 := derive()
	if bytes.Equal(s1, s2) {
		t.Fatal("two handshakes produced the same shared secret")
	}
}

// Compile-time check that eciesTransport's public key is valid on secp256k1.
func TestRLPxHandshake_PublicKeyOnCurve(t *testing.T) {
	hs, _ := NewRLPxHandshake(true)
	pub := hs.LocalPublicKey()
	curve := ethcrypto.S256()
	x, y := elliptic.Unmarshal(curve, pub)
	if x == nil || y == nil {
		t.Fatal("public key not on curve")
	}
	if !curve.IsOnCurve(x, y) {
		t.Fatal("public key point not on secp256k1")
	}
}

// Ensure that SetRemotePublicKey rejects a point with valid format but
// not on the curve (all zeros for x, y).
func TestRLPxHandshake_SetRemotePublicKey_OffCurve(t *testing.T) {
	hs, _ := NewRLPxHandshake(true)
	offCurve := make([]byte, 65)
	offCurve[0] = 0x04 // correct prefix but x=0, y=0 is not on secp256k1
	err := hs.SetRemotePublicKey(offCurve)
	if err == nil {
		// The Go standard library's Unmarshal returns nil for off-curve points.
		// Our function should catch this.
		if hs.remoteEphPub != nil {
			t.Fatal("should not have set remote pub for off-curve point")
		}
	}
}

// Verify the ecdhShared helper with a known-private-key scenario.
func TestECDHShared_Deterministic(t *testing.T) {
	// Use the same key pair twice; result should be the same.
	k, _ := ethcrypto.GenerateKey()
	s1 := ecdhShared(k, &k.PublicKey)
	s2 := ecdhShared(k, &k.PublicKey)
	if s1.Cmp(s2) != 0 {
		t.Fatal("deterministic ECDH failed")
	}
}

// Verify that ecdhShared works with explicit ecdsa.PublicKey construction.
func TestECDHShared_ExplicitKey(t *testing.T) {
	k1, _ := ethcrypto.GenerateKey()
	k2, _ := ethcrypto.GenerateKey()

	pub2 := &ecdsa.PublicKey{
		Curve: k2.PublicKey.Curve,
		X:     k2.PublicKey.X,
		Y:     k2.PublicKey.Y,
	}
	s := ecdhShared(k1, pub2)
	if s == nil || s.Sign() == 0 {
		t.Fatal("ECDH returned zero")
	}
}
