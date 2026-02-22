package p2p

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"sync"

	ethcrypto "github.com/eth2030/eth2030/crypto"
)

const (
	authMsgSize           = 32 + 65 + 65 + 1 // nonce + ephemeral + static + version
	ackMsgSize            = 32 + 65 + 1       // nonce + ephemeral + version
	eciesHandshakeVersion = 5
)
var (
	ErrECIESAuthFailed = errors.New("p2p: ecies auth message verification failed")
	ErrECIESAckFailed  = errors.New("p2p: ecies ack message verification failed")
	ErrECIESVersion    = errors.New("p2p: ecies version mismatch")
)

// ECIESHandshake implements the full RLPx ECIES handshake protocol:
// ECIES-encrypted auth/ack, ECDH key agreement, frame cipher key derivation.
type ECIESHandshake struct {
	staticKey       *ecdsa.PrivateKey
	ephemeralKey    *ecdsa.PrivateKey
	remoteStaticPub *ecdsa.PublicKey
	remoteEphPub    *ecdsa.PublicKey
	localNonce      [32]byte
	remoteNonce     [32]byte
	initiator       bool
	aesSecret       []byte
	macSecret       []byte
}

// NewECIESHandshake creates a new ECIES handshake state.
// staticKey is the node's long-lived identity key.
// remoteStaticPub may be nil for the responder side (learned during handshake).
func NewECIESHandshake(staticKey *ecdsa.PrivateKey, remoteStaticPub *ecdsa.PublicKey, initiator bool) (*ECIESHandshake, error) {
	if staticKey == nil {
		return nil, errors.New("p2p: nil static key")
	}
	ephKey, err := ethcrypto.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("p2p: generate ephemeral key: %w", err)
	}

	h := &ECIESHandshake{
		staticKey:       staticKey,
		ephemeralKey:    ephKey,
		remoteStaticPub: remoteStaticPub,
		initiator:       initiator,
	}
	if _, err := rand.Read(h.localNonce[:]); err != nil {
		return nil, fmt.Errorf("p2p: generate nonce: %w", err)
	}
	return h, nil
}

// MakeAuthMsg builds the auth message sent by the initiator.
// Plaintext format: [32 nonce][65 ephemeral pubkey][65 static pubkey][1 version]
// The message is encrypted with the remote static public key using ECIES.
func (h *ECIESHandshake) MakeAuthMsg() ([]byte, error) {
	if h.remoteStaticPub == nil {
		return nil, errors.New("p2p: remote static key required for auth")
	}

	// Build plaintext: nonce + ephemeral pubkey + static pubkey + version.
	plain := make([]byte, authMsgSize)
	copy(plain[:32], h.localNonce[:])
	ephPub := marshalPublicKey(&h.ephemeralKey.PublicKey)
	copy(plain[32:97], ephPub)
	staticPub := marshalPublicKey(&h.staticKey.PublicKey)
	copy(plain[97:162], staticPub)
	plain[162] = eciesHandshakeVersion

	// Encrypt with ECIES using remote static public key.
	encrypted, err := ethcrypto.ECIESEncrypt(h.remoteStaticPub, plain)
	if err != nil {
		return nil, fmt.Errorf("p2p: ecies encrypt auth: %w", err)
	}
	return encrypted, nil
}

// HandleAuthMsg processes a received auth message on the responder side.
// It decrypts with the local static key and extracts the remote's nonce,
// ephemeral key, and static key.
func (h *ECIESHandshake) HandleAuthMsg(data []byte) error {
	plain, err := ethcrypto.ECIESDecrypt(h.staticKey, data)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrECIESAuthFailed, err)
	}
	if len(plain) < authMsgSize {
		return fmt.Errorf("%w: message too short: %d", ErrECIESAuthFailed, len(plain))
	}

	// Parse nonce.
	copy(h.remoteNonce[:], plain[:32])

	// Parse remote ephemeral public key.
	remoteEphPub := parsePublicKey(plain[32:97])
	if remoteEphPub == nil {
		return fmt.Errorf("%w: invalid ephemeral key", ErrECIESAuthFailed)
	}
	h.remoteEphPub = remoteEphPub

	// Parse remote static public key.
	remoteStaticPub := parsePublicKey(plain[97:162])
	if remoteStaticPub == nil {
		return fmt.Errorf("%w: invalid static key", ErrECIESAuthFailed)
	}
	h.remoteStaticPub = remoteStaticPub

	// Verify version.
	version := plain[162]
	if version < eciesHandshakeVersion {
		return fmt.Errorf("%w: remote=%d, local=%d", ErrECIESVersion, version, eciesHandshakeVersion)
	}
	return nil
}

// MakeAckMsg builds the ack message sent by the responder.
// Plaintext format: [32 nonce][65 ephemeral pubkey][1 version]
func (h *ECIESHandshake) MakeAckMsg() ([]byte, error) {
	if h.remoteStaticPub == nil {
		return nil, errors.New("p2p: remote static key required for ack")
	}

	plain := make([]byte, ackMsgSize)
	copy(plain[:32], h.localNonce[:])
	ephPub := marshalPublicKey(&h.ephemeralKey.PublicKey)
	copy(plain[32:97], ephPub)
	plain[97] = eciesHandshakeVersion

	encrypted, err := ethcrypto.ECIESEncrypt(h.remoteStaticPub, plain)
	if err != nil {
		return nil, fmt.Errorf("p2p: ecies encrypt ack: %w", err)
	}
	return encrypted, nil
}

// HandleAckMsg processes a received ack message on the initiator side.
func (h *ECIESHandshake) HandleAckMsg(data []byte) error {
	plain, err := ethcrypto.ECIESDecrypt(h.staticKey, data)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrECIESAckFailed, err)
	}
	if len(plain) < ackMsgSize {
		return fmt.Errorf("%w: message too short: %d", ErrECIESAckFailed, len(plain))
	}

	// Parse nonce.
	copy(h.remoteNonce[:], plain[:32])

	// Parse remote ephemeral public key.
	remoteEphPub := parsePublicKey(plain[32:97])
	if remoteEphPub == nil {
		return fmt.Errorf("%w: invalid ephemeral key", ErrECIESAckFailed)
	}
	h.remoteEphPub = remoteEphPub

	// Verify version.
	version := plain[97]
	if version < eciesHandshakeVersion {
		return fmt.Errorf("%w: remote=%d, local=%d", ErrECIESVersion, version, eciesHandshakeVersion)
	}
	return nil
}

// DeriveSecrets computes the shared secret from ECDH between the local
// and remote ephemeral keys, then derives the AES and MAC keys.
func (h *ECIESHandshake) DeriveSecrets() error {
	if h.remoteEphPub == nil {
		return errors.New("p2p: remote ephemeral key not set")
	}

	// ECDH: shared = ephemeral_priv * remote_ephemeral_pub
	sx, _ := h.remoteEphPub.Curve.ScalarMult(
		h.remoteEphPub.X, h.remoteEphPub.Y,
		h.ephemeralKey.D.Bytes(),
	)

	// Build key material: ecdh_shared || initiator_nonce || responder_nonce
	shared := make([]byte, 32)
	sxBytes := sx.Bytes()
	copy(shared[32-len(sxBytes):], sxBytes)

	// Determine nonce order (initiator first).
	var initNonce, respNonce []byte
	if h.initiator {
		initNonce = h.localNonce[:]
		respNonce = h.remoteNonce[:]
	} else {
		initNonce = h.remoteNonce[:]
		respNonce = h.localNonce[:]
	}

	h.aesSecret, h.macSecret = DeriveFrameKeys(shared, initNonce, respNonce)
	return nil
}

// AESSecret returns the derived AES key (32 bytes). Must be called after DeriveSecrets.
func (h *ECIESHandshake) AESSecret() []byte { return h.aesSecret }

// MACSecret returns the derived MAC key (32 bytes). Must be called after DeriveSecrets.
func (h *ECIESHandshake) MACSecret() []byte { return h.macSecret }

// RemoteStaticPub returns the remote peer's static public key.
func (h *ECIESHandshake) RemoteStaticPub() *ecdsa.PublicKey { return h.remoteStaticPub }

// LocalNonce returns the local nonce.
func (h *ECIESHandshake) LocalNonce() [32]byte { return h.localNonce }

// RemoteNonce returns the remote nonce.
func (h *ECIESHandshake) RemoteNonce() [32]byte { return h.remoteNonce }

// --- Full handshake over a connection ---

// DoECIESHandshake performs the complete ECIES handshake over a net.Conn.
// For the initiator: sends auth, receives ack.
// For the responder: receives auth, sends ack.
// On success, it returns the FrameCodec configured with derived keys.
func DoECIESHandshake(conn net.Conn, staticKey *ecdsa.PrivateKey, remoteStaticPub *ecdsa.PublicKey, initiator bool, caps []Cap) (*FrameCodec, error) {
	hs, err := NewECIESHandshake(staticKey, remoteStaticPub, initiator)
	if err != nil {
		return nil, err
	}

	if initiator {
		// Send auth message.
		authMsg, err := hs.MakeAuthMsg()
		if err != nil {
			return nil, err
		}
		if err := writeSizedMsg(conn, authMsg); err != nil {
			return nil, fmt.Errorf("p2p: write auth: %w", err)
		}

		// Read ack message.
		ackData, err := readSizedMsg(conn)
		if err != nil {
			return nil, fmt.Errorf("p2p: read ack: %w", err)
		}
		if err := hs.HandleAckMsg(ackData); err != nil {
			return nil, err
		}
	} else {
		// Read auth message.
		authData, err := readSizedMsg(conn)
		if err != nil {
			return nil, fmt.Errorf("p2p: read auth: %w", err)
		}
		if err := hs.HandleAuthMsg(authData); err != nil {
			return nil, err
		}

		// Send ack message.
		ackMsg, err := hs.MakeAckMsg()
		if err != nil {
			return nil, err
		}
		if err := writeSizedMsg(conn, ackMsg); err != nil {
			return nil, fmt.Errorf("p2p: write ack: %w", err)
		}
	}

	// Derive shared secrets.
	if err := hs.DeriveSecrets(); err != nil {
		return nil, err
	}

	// Build the frame codec.
	return NewFrameCodec(conn, FrameCodecConfig{
		AESKey:       hs.aesSecret,
		MACKey:       hs.macSecret,
		Initiator:    initiator,
		EnableSnappy: true,
		Caps:         caps,
	})
}

// --- Capability negotiation ---

// NegotiateCaps performs full capability matching between local and remote
// capability lists. It returns the matched capabilities sorted by name,
// with the highest mutually supported version for each protocol name.
func NegotiateCaps(local, remote []Cap) []Cap {
	localMax := make(map[string]uint)
	for _, c := range local {
		if v, ok := localMax[c.Name]; !ok || c.Version > v {
			localMax[c.Name] = c.Version
		}
	}

	remoteMax := make(map[string]uint)
	for _, c := range remote {
		if v, ok := remoteMax[c.Name]; !ok || c.Version > v {
			remoteMax[c.Name] = c.Version
		}
	}

	var matched []Cap
	for name, lv := range localMax {
		if rv, ok := remoteMax[name]; ok {
			v := lv
			if rv < v {
				v = rv
			}
			matched = append(matched, Cap{Name: name, Version: v})
		}
	}

	sort.Slice(matched, func(i, j int) bool {
		if matched[i].Name != matched[j].Name {
			return matched[i].Name < matched[j].Name
		}
		return matched[i].Version < matched[j].Version
	})
	return matched
}

// FullHandshake performs both the ECIES transport handshake and the devp2p
// hello handshake in sequence. It returns the negotiated capabilities,
// the FrameCodec for message I/O, and the remote HelloPacket.
func FullHandshake(conn net.Conn, staticKey *ecdsa.PrivateKey, remoteStaticPub *ecdsa.PublicKey, initiator bool, localHello *HelloPacket) (*FrameCodec, *HelloPacket, []Cap, error) {
	// Step 1: ECIES transport handshake.
	codec, err := DoECIESHandshake(conn, staticKey, remoteStaticPub, initiator, localHello.Caps)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("p2p: ecies handshake: %w", err)
	}

	// Step 2: devp2p hello handshake over the encrypted transport.
	type result struct {
		hello *HelloPacket
		err   error
	}
	recvCh := make(chan result, 1)
	sendCh := make(chan error, 1)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		payload := EncodeHello(localHello)
		sendCh <- codec.WriteMsg(Msg{
			Code:    HelloMsg,
			Size:    uint32(len(payload)),
			Payload: payload,
		})
	}()

	go func() {
		defer wg.Done()
		msg, err := codec.ReadMsg()
		if err != nil {
			recvCh <- result{nil, err}
			return
		}
		if msg.Code != HelloMsg {
			recvCh <- result{nil, fmt.Errorf("p2p: expected hello, got 0x%02x", msg.Code)}
			return
		}
		hello, err := DecodeHello(msg.Payload)
		recvCh <- result{hello, err}
	}()

	if err := <-sendCh; err != nil {
		codec.Close()
		return nil, nil, nil, fmt.Errorf("p2p: send hello: %w", err)
	}

	res := <-recvCh
	wg.Wait()

	if res.err != nil {
		codec.Close()
		return nil, nil, nil, fmt.Errorf("p2p: recv hello: %w", res.err)
	}

	// Step 3: Validate version.
	if res.hello.Version < baseProtocolVersion {
		codec.SendDisconnect(DiscProtocolError)
		return nil, nil, nil, fmt.Errorf("%w: remote=%d, local=%d",
			ErrIncompatibleVersion, res.hello.Version, baseProtocolVersion)
	}

	// Step 4: Negotiate capabilities.
	matched := NegotiateCaps(localHello.Caps, res.hello.Caps)
	if len(matched) == 0 {
		codec.SendDisconnect(DiscUselessPeer)
		return nil, nil, nil, ErrNoMatchingCaps
	}

	return codec, res.hello, matched, nil
}

// --- Wire helpers ---

// writeSizedMsg writes a 2-byte length prefix followed by the message data.
func writeSizedMsg(conn net.Conn, data []byte) error {
	var lenBuf [2]byte
	lenBuf[0] = byte(len(data) >> 8)
	lenBuf[1] = byte(len(data))
	if _, err := conn.Write(lenBuf[:]); err != nil {
		return err
	}
	_, err := conn.Write(data)
	return err
}

// readSizedMsg reads a 2-byte length prefix and then the message data.
func readSizedMsg(conn net.Conn) ([]byte, error) {
	var lenBuf [2]byte
	if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
		return nil, err
	}
	size := int(lenBuf[0])<<8 | int(lenBuf[1])
	if size == 0 {
		return nil, errors.New("p2p: zero-length sized message")
	}
	if size > 65535 {
		return nil, errors.New("p2p: sized message too large")
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(conn, data); err != nil {
		return nil, err
	}
	return data, nil
}

// marshalPublicKey returns the 65-byte uncompressed encoding of a secp256k1 public key.
func marshalPublicKey(pub *ecdsa.PublicKey) []byte {
	return elliptic.Marshal(pub.Curve, pub.X, pub.Y)
}

// parsePublicKey parses a 65-byte uncompressed secp256k1 public key.
func parsePublicKey(data []byte) *ecdsa.PublicKey {
	if len(data) != 65 || data[0] != 0x04 {
		return nil
	}
	curve := ethcrypto.S256()
	x, y := elliptic.Unmarshal(curve, data)
	if x == nil {
		return nil
	}
	return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}
}

// StaticPubKey returns the 65-byte uncompressed encoding of the given
// ECDSA public key. Useful for logging and comparison.
func StaticPubKey(key *ecdsa.PublicKey) []byte {
	return marshalPublicKey(key)
}

// VerifyRemoteIdentity checks that the remote static public key received
// during the ECIES handshake matches the expected key. Returns nil if they
// match, or an error describing the mismatch.
func VerifyRemoteIdentity(got, expected *ecdsa.PublicKey) error {
	if expected == nil {
		return nil // no expectation; accept any key
	}
	if got == nil {
		return errors.New("p2p: no remote static key received")
	}
	gotBytes := marshalPublicKey(got)
	expectedBytes := marshalPublicKey(expected)
	h1 := sha256.Sum256(gotBytes)
	h2 := sha256.Sum256(expectedBytes)
	if h1 != h2 {
		return errors.New("p2p: remote identity mismatch")
	}
	return nil
}
