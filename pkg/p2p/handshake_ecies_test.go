package p2p

import (
	"bytes"
	"crypto/ecdsa"
	"net"
	"sync"
	"testing"
	"time"

	ethcrypto "github.com/eth2028/eth2028/crypto"
)

func generateTestKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ethcrypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return key
}

func TestECIESHandshake_NewHandshake(t *testing.T) {
	key := generateTestKey(t)
	hs, err := NewECIESHandshake(key, nil, true)
	if err != nil {
		t.Fatalf("NewECIESHandshake: %v", err)
	}
	if hs.staticKey == nil {
		t.Fatal("static key should not be nil")
	}
	if hs.ephemeralKey == nil {
		t.Fatal("ephemeral key should not be nil")
	}
	if !hs.initiator {
		t.Fatal("should be initiator")
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

func TestECIESHandshake_NilStaticKey(t *testing.T) {
	_, err := NewECIESHandshake(nil, nil, true)
	if err == nil {
		t.Fatal("expected error for nil static key")
	}
}

func TestECIESHandshake_AuthAckRoundtrip(t *testing.T) {
	keyA := generateTestKey(t)
	keyB := generateTestKey(t)

	// Initiator creates auth.
	hsA, err := NewECIESHandshake(keyA, &keyB.PublicKey, true)
	if err != nil {
		t.Fatal(err)
	}

	authMsg, err := hsA.MakeAuthMsg()
	if err != nil {
		t.Fatalf("MakeAuthMsg: %v", err)
	}
	if len(authMsg) == 0 {
		t.Fatal("auth message is empty")
	}

	// Responder processes auth.
	hsB, err := NewECIESHandshake(keyB, nil, false)
	if err != nil {
		t.Fatal(err)
	}

	if err := hsB.HandleAuthMsg(authMsg); err != nil {
		t.Fatalf("HandleAuthMsg: %v", err)
	}

	// Responder should now know the initiator's nonce and keys.
	if hsB.remoteEphPub == nil {
		t.Fatal("remote ephemeral key not set after HandleAuthMsg")
	}
	if hsB.remoteStaticPub == nil {
		t.Fatal("remote static key not set after HandleAuthMsg")
	}
	if !bytes.Equal(hsB.remoteNonce[:], hsA.localNonce[:]) {
		t.Fatal("remote nonce does not match initiator's local nonce")
	}

	// Responder creates ack.
	ackMsg, err := hsB.MakeAckMsg()
	if err != nil {
		t.Fatalf("MakeAckMsg: %v", err)
	}

	// Initiator processes ack.
	if err := hsA.HandleAckMsg(ackMsg); err != nil {
		t.Fatalf("HandleAckMsg: %v", err)
	}

	// Initiator should now know the responder's nonce and ephemeral key.
	if hsA.remoteEphPub == nil {
		t.Fatal("remote ephemeral key not set after HandleAckMsg")
	}
	if !bytes.Equal(hsA.remoteNonce[:], hsB.localNonce[:]) {
		t.Fatal("remote nonce does not match responder's local nonce")
	}
}

func TestECIESHandshake_DeriveSecrets(t *testing.T) {
	keyA := generateTestKey(t)
	keyB := generateTestKey(t)

	// Full auth/ack exchange.
	hsA, _ := NewECIESHandshake(keyA, &keyB.PublicKey, true)
	authMsg, _ := hsA.MakeAuthMsg()

	hsB, _ := NewECIESHandshake(keyB, nil, false)
	hsB.HandleAuthMsg(authMsg)

	ackMsg, _ := hsB.MakeAckMsg()
	hsA.HandleAckMsg(ackMsg)

	// Derive secrets on both sides.
	if err := hsA.DeriveSecrets(); err != nil {
		t.Fatalf("initiator DeriveSecrets: %v", err)
	}
	if err := hsB.DeriveSecrets(); err != nil {
		t.Fatalf("responder DeriveSecrets: %v", err)
	}

	// Both sides should derive the same AES and MAC keys.
	if !bytes.Equal(hsA.AESSecret(), hsB.AESSecret()) {
		t.Fatal("AES secrets differ")
	}
	if !bytes.Equal(hsA.MACSecret(), hsB.MACSecret()) {
		t.Fatal("MAC secrets differ")
	}

	// Keys should be 32 bytes each.
	if len(hsA.AESSecret()) != 32 {
		t.Fatalf("AES key length: %d", len(hsA.AESSecret()))
	}
	if len(hsA.MACSecret()) != 32 {
		t.Fatalf("MAC key length: %d", len(hsA.MACSecret()))
	}
}

func TestECIESHandshake_DeriveSecrets_NoRemoteKey(t *testing.T) {
	key := generateTestKey(t)
	hs, _ := NewECIESHandshake(key, nil, true)
	err := hs.DeriveSecrets()
	if err == nil {
		t.Fatal("expected error when remote ephemeral key not set")
	}
}

func TestECIESHandshake_UniqueSecrets(t *testing.T) {
	derive := func() []byte {
		keyA := generateTestKey(t)
		keyB := generateTestKey(t)

		hsA, _ := NewECIESHandshake(keyA, &keyB.PublicKey, true)
		auth, _ := hsA.MakeAuthMsg()

		hsB, _ := NewECIESHandshake(keyB, nil, false)
		hsB.HandleAuthMsg(auth)

		ack, _ := hsB.MakeAckMsg()
		hsA.HandleAckMsg(ack)

		hsA.DeriveSecrets()
		return hsA.AESSecret()
	}

	s1 := derive()
	s2 := derive()
	if bytes.Equal(s1, s2) {
		t.Fatal("two handshakes produced the same AES secret")
	}
}

func TestNegotiateCaps(t *testing.T) {
	local := []Cap{
		{Name: "eth", Version: 68},
		{Name: "eth", Version: 67},
		{Name: "snap", Version: 1},
	}
	remote := []Cap{
		{Name: "eth", Version: 67},
		{Name: "eth", Version: 68},
		{Name: "wit", Version: 1},
	}

	matched := NegotiateCaps(local, remote)
	if len(matched) != 1 {
		t.Fatalf("expected 1 matched cap, got %d", len(matched))
	}
	if matched[0].Name != "eth" || matched[0].Version != 68 {
		t.Fatalf("expected eth/68, got %s/%d", matched[0].Name, matched[0].Version)
	}
}

func TestNegotiateCaps_NoMatch(t *testing.T) {
	local := []Cap{{Name: "eth", Version: 68}}
	remote := []Cap{{Name: "snap", Version: 1}}

	matched := NegotiateCaps(local, remote)
	if len(matched) != 0 {
		t.Fatalf("expected no matches, got %d", len(matched))
	}
}

func TestNegotiateCaps_VersionDown(t *testing.T) {
	local := []Cap{{Name: "eth", Version: 68}}
	remote := []Cap{{Name: "eth", Version: 67}}

	matched := NegotiateCaps(local, remote)
	if len(matched) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matched))
	}
	if matched[0].Version != 67 {
		t.Fatalf("expected version 67, got %d", matched[0].Version)
	}
}

func TestNegotiateCaps_Multiple(t *testing.T) {
	local := []Cap{
		{Name: "eth", Version: 68},
		{Name: "snap", Version: 1},
		{Name: "les", Version: 4},
	}
	remote := []Cap{
		{Name: "eth", Version: 68},
		{Name: "snap", Version: 2},
		{Name: "les", Version: 3},
	}

	matched := NegotiateCaps(local, remote)
	if len(matched) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(matched))
	}
	// Should be sorted by name.
	if matched[0].Name != "eth" || matched[1].Name != "les" || matched[2].Name != "snap" {
		t.Fatalf("unexpected order: %v", matched)
	}
	// les should be negotiated down to 3.
	if matched[1].Version != 3 {
		t.Fatalf("les version: got %d, want 3", matched[1].Version)
	}
	// snap should be negotiated down to 1.
	if matched[2].Version != 1 {
		t.Fatalf("snap version: got %d, want 1", matched[2].Version)
	}
}

func TestDoECIESHandshake(t *testing.T) {
	keyA := generateTestKey(t)
	keyB := generateTestKey(t)
	caps := []Cap{{Name: "eth", Version: 68}}

	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	var fc1, fc2 *FrameCodec
	var err1, err2 error
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		fc1, err1 = DoECIESHandshake(c1, keyA, &keyB.PublicKey, true, caps)
	}()
	go func() {
		defer wg.Done()
		fc2, err2 = DoECIESHandshake(c2, keyB, nil, false, caps)
	}()
	wg.Wait()

	if err1 != nil {
		t.Fatalf("initiator handshake: %v", err1)
	}
	if err2 != nil {
		t.Fatalf("responder handshake: %v", err2)
	}

	// Test that the codecs can exchange messages.
	errCh := make(chan error, 1)
	go func() {
		errCh <- fc1.WriteMsg(Msg{Code: 0x01, Payload: []byte("hello ecies")})
	}()

	msg, err := fc2.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("WriteMsg: %v", err)
	}
	if string(msg.Payload) != "hello ecies" {
		t.Fatalf("payload: got %q, want %q", msg.Payload, "hello ecies")
	}

	fc1.Close()
	fc2.Close()
}

func TestVerifyRemoteIdentity(t *testing.T) {
	key := generateTestKey(t)
	otherKey := generateTestKey(t)

	// Match.
	if err := VerifyRemoteIdentity(&key.PublicKey, &key.PublicKey); err != nil {
		t.Fatalf("matching keys should verify: %v", err)
	}

	// Mismatch.
	if err := VerifyRemoteIdentity(&key.PublicKey, &otherKey.PublicKey); err == nil {
		t.Fatal("mismatching keys should not verify")
	}

	// Nil expected = accept any.
	if err := VerifyRemoteIdentity(&key.PublicKey, nil); err != nil {
		t.Fatalf("nil expected should accept: %v", err)
	}

	// Nil got.
	if err := VerifyRemoteIdentity(nil, &key.PublicKey); err == nil {
		t.Fatal("nil got should fail")
	}
}

func TestStaticPubKey(t *testing.T) {
	key := generateTestKey(t)
	pub := StaticPubKey(&key.PublicKey)
	if len(pub) != 65 {
		t.Fatalf("expected 65 bytes, got %d", len(pub))
	}
	if pub[0] != 0x04 {
		t.Fatalf("expected 0x04 prefix, got 0x%02x", pub[0])
	}
}

func TestMarshalParsePublicKey(t *testing.T) {
	key := generateTestKey(t)
	data := marshalPublicKey(&key.PublicKey)
	parsed := parsePublicKey(data)
	if parsed == nil {
		t.Fatal("parsePublicKey returned nil")
	}
	if parsed.X.Cmp(key.PublicKey.X) != 0 || parsed.Y.Cmp(key.PublicKey.Y) != 0 {
		t.Fatal("parsed key does not match original")
	}
}

func TestParsePublicKey_Invalid(t *testing.T) {
	// Too short.
	if parsePublicKey([]byte{0x04, 0x01}) != nil {
		t.Fatal("should return nil for short input")
	}
	// Wrong prefix.
	bad := make([]byte, 65)
	bad[0] = 0x03
	if parsePublicKey(bad) != nil {
		t.Fatal("should return nil for wrong prefix")
	}
}

func TestWriteReadSizedMsg(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	data := []byte("sized message test data")
	errCh := make(chan error, 1)

	go func() {
		errCh <- writeSizedMsg(c1, data)
	}()

	got, err := readSizedMsg(c2)
	if err != nil {
		t.Fatalf("readSizedMsg: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("writeSizedMsg: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("data mismatch: got %x, want %x", got, data)
	}
}

func TestFullHandshake_Success(t *testing.T) {
	keyA := generateTestKey(t)
	keyB := generateTestKey(t)

	helloA := &HelloPacket{
		Version: 5,
		Name:    "client-a",
		Caps:    []Cap{{Name: "eth", Version: 68}},
		ID:      "node-a",
	}
	helloB := &HelloPacket{
		Version: 5,
		Name:    "client-b",
		Caps:    []Cap{{Name: "eth", Version: 68}, {Name: "snap", Version: 1}},
		ID:      "node-b",
	}

	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	type result struct {
		codec *FrameCodec
		hello *HelloPacket
		caps  []Cap
		err   error
	}

	resA := make(chan result, 1)
	resB := make(chan result, 1)

	go func() {
		fc, h, caps, err := FullHandshake(c1, keyA, &keyB.PublicKey, true, helloA)
		resA <- result{fc, h, caps, err}
	}()
	go func() {
		fc, h, caps, err := FullHandshake(c2, keyB, nil, false, helloB)
		resB <- result{fc, h, caps, err}
	}()

	select {
	case r := <-resA:
		if r.err != nil {
			t.Fatalf("initiator: %v", r.err)
		}
		if r.hello.Name != "client-b" {
			t.Fatalf("got name %q, want client-b", r.hello.Name)
		}
		if len(r.caps) != 1 || r.caps[0].Name != "eth" {
			t.Fatalf("unexpected caps: %v", r.caps)
		}
		r.codec.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for initiator")
	}

	select {
	case r := <-resB:
		if r.err != nil {
			t.Fatalf("responder: %v", r.err)
		}
		if r.hello.Name != "client-a" {
			t.Fatalf("got name %q, want client-a", r.hello.Name)
		}
		r.codec.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for responder")
	}
}
