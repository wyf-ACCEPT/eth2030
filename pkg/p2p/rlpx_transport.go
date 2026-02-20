package p2p

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"
	"math/big"
	"net"
	"sync"

	ethcrypto "github.com/eth2028/eth2028/crypto"
)

// ECIES handshake constants.
const (
	// eciesOverhead is the overhead of an ECIES-encrypted message:
	// 65 bytes uncompressed public key + 16 bytes IV + 32 bytes HMAC.
	eciesOverhead = 65 + 16 + 32

	// authMsgLen is the length of the auth message (nonce + ephemeral pubkey).
	authMsgLen = 32 + 65

	// authRespLen is the length of the auth response (nonce + ephemeral pubkey).
	authRespLen = 32 + 65

	// frameHeaderLen is the size of an encrypted frame header (16 bytes + 16 MAC).
	frameHeaderLen = 32

	// frameMACLen is the truncated MAC size.
	frameMACLen = 16

	// maxFramePayload limits frame payloads to 16 MiB.
	maxFramePayload = 16 * 1024 * 1024
)

var (
	// ErrECIESDecrypt is returned when ECIES decryption fails.
	ErrECIESDecrypt = errors.New("p2p: ecies decryption failed")

	// ErrInvalidPubKey is returned when a public key is malformed.
	ErrInvalidPubKey = errors.New("p2p: invalid public key")

	// ErrFrameMACMismatch is returned when frame MAC verification fails.
	ErrFrameMACMismatch = errors.New("p2p: frame mac mismatch")
)

// RLPxHandshake holds the state for an ECIES-based RLPx handshake.
// The handshake derives shared secrets from ECDH key agreement between
// ephemeral secp256k1 keys, matching the devp2p RLPx spec.
type RLPxHandshake struct {
	// Local ephemeral key used for this handshake.
	ephemeralKey *ecdsa.PrivateKey

	// Remote ephemeral public key, set after receiving the auth/ack message.
	remoteEphPub *ecdsa.PublicKey

	// Nonces exchanged during the handshake.
	localNonce  [32]byte
	remoteNonce [32]byte

	// Whether this side initiated the connection.
	initiator bool

	// Derived shared secret after handshake.
	sharedSecret []byte
}

// NewRLPxHandshake creates a new handshake state. It generates an ephemeral
// key and random nonce.
func NewRLPxHandshake(initiator bool) (*RLPxHandshake, error) {
	key, err := ethcrypto.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("p2p: generate ephemeral key: %w", err)
	}
	h := &RLPxHandshake{
		ephemeralKey: key,
		initiator:    initiator,
	}
	if _, err := rand.Read(h.localNonce[:]); err != nil {
		return nil, fmt.Errorf("p2p: generate nonce: %w", err)
	}
	return h, nil
}

// LocalPublicKey returns the local ephemeral public key as uncompressed bytes (65 bytes).
func (h *RLPxHandshake) LocalPublicKey() []byte {
	return elliptic.Marshal(h.ephemeralKey.PublicKey.Curve, h.ephemeralKey.PublicKey.X, h.ephemeralKey.PublicKey.Y)
}

// SetRemotePublicKey parses and sets the remote ephemeral public key from
// uncompressed bytes.
func (h *RLPxHandshake) SetRemotePublicKey(pub []byte) error {
	if len(pub) != 65 || pub[0] != 0x04 {
		return ErrInvalidPubKey
	}
	curve := ethcrypto.S256()
	x, y := elliptic.Unmarshal(curve, pub)
	if x == nil {
		return ErrInvalidPubKey
	}
	h.remoteEphPub = &ecdsa.PublicKey{Curve: curve, X: x, Y: y}
	return nil
}

// DeriveSecrets performs ECDH between the local ephemeral key and the remote
// ephemeral public key, then derives symmetric keys. Returns the shared
// secret used for key derivation.
func (h *RLPxHandshake) DeriveSecrets() ([]byte, error) {
	if h.remoteEphPub == nil {
		return nil, errors.New("p2p: remote public key not set")
	}
	// ECDH: multiply remote public key by our private scalar.
	sx, _ := h.remoteEphPub.Curve.ScalarMult(
		h.remoteEphPub.X, h.remoteEphPub.Y,
		h.ephemeralKey.D.Bytes(),
	)
	// Derive shared secret: hash(ecdh || nonce_init || nonce_resp).
	var material []byte
	material = append(material, sx.Bytes()...)
	if h.initiator {
		material = append(material, h.localNonce[:]...)
		material = append(material, h.remoteNonce[:]...)
	} else {
		material = append(material, h.remoteNonce[:]...)
		material = append(material, h.localNonce[:]...)
	}
	shared := sha256.Sum256(material)
	h.sharedSecret = shared[:]
	return h.sharedSecret, nil
}

// EncryptFrame encrypts a plaintext frame using AES-CTR and appends an
// HMAC-SHA256 tag (truncated to 16 bytes). The key is derived from the
// provided 32-byte secret.
func EncryptFrame(plaintext, secret []byte) ([]byte, error) {
	if len(secret) < 32 {
		return nil, errors.New("p2p: secret too short")
	}
	aesKey := secret[:16]
	macKey := secret[16:]

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, fmt.Errorf("p2p: aes cipher: %w", err)
	}

	// Generate random IV.
	iv := make([]byte, aes.BlockSize)
	if _, err := rand.Read(iv); err != nil {
		return nil, fmt.Errorf("p2p: iv generation: %w", err)
	}

	ciphertext := make([]byte, len(plaintext))
	stream := cipher.NewCTR(block, iv)
	stream.XORKeyStream(ciphertext, plaintext)

	// HMAC-SHA256 over iv + ciphertext, truncated to 16 bytes.
	mac := hmac.New(sha256.New, macKey)
	mac.Write(iv)
	mac.Write(ciphertext)
	tag := mac.Sum(nil)[:frameMACLen]

	// Result: iv + ciphertext + tag.
	result := make([]byte, 0, len(iv)+len(ciphertext)+len(tag))
	result = append(result, iv...)
	result = append(result, ciphertext...)
	result = append(result, tag...)
	return result, nil
}

// DecryptFrame decrypts a frame produced by EncryptFrame. It verifies the
// HMAC tag before decrypting.
func DecryptFrame(data, secret []byte) ([]byte, error) {
	if len(secret) < 32 {
		return nil, errors.New("p2p: secret too short")
	}
	minLen := aes.BlockSize + frameMACLen // iv + tag, at minimum
	if len(data) < minLen {
		return nil, errors.New("p2p: encrypted frame too short")
	}
	aesKey := secret[:16]
	macKey := secret[16:]

	iv := data[:aes.BlockSize]
	tag := data[len(data)-frameMACLen:]
	ciphertext := data[aes.BlockSize : len(data)-frameMACLen]

	// Verify MAC.
	mac := hmac.New(sha256.New, macKey)
	mac.Write(iv)
	mac.Write(ciphertext)
	expected := mac.Sum(nil)[:frameMACLen]
	if !hmac.Equal(tag, expected) {
		return nil, ErrFrameMACMismatch
	}

	// Decrypt.
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, fmt.Errorf("p2p: aes cipher: %w", err)
	}
	plaintext := make([]byte, len(ciphertext))
	stream := cipher.NewCTR(block, iv)
	stream.XORKeyStream(plaintext, ciphertext)
	return plaintext, nil
}

// eciesTransport wraps an RLPxTransport with ECIES-based handshake secrets.
// After the ECIES handshake completes, it provides the same frame-level
// encryption as the base RLPxTransport but keyed from ECDH-derived material.
type eciesTransport struct {
	conn net.Conn

	encStream cipher.Stream
	decStream cipher.Stream
	egressMAC hash.Hash
	ingrMAC   hash.Hash

	rmu sync.Mutex
	wmu sync.Mutex

	handshook bool
}

// Handshake performs an ECIES-based RLPx handshake over the connection.
// Both sides exchange ephemeral public keys and nonces, perform ECDH,
// and derive symmetric encryption keys.
func (t *eciesTransport) Handshake(initiator bool) error {
	hs, err := NewRLPxHandshake(initiator)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBadHandshake, err)
	}

	localPub := hs.LocalPublicKey()
	// Auth message: [32-byte nonce][65-byte pubkey].
	authMsg := make([]byte, 32+65)
	copy(authMsg[:32], hs.localNonce[:])
	copy(authMsg[32:], localPub)

	var remoteAuth [32 + 65]byte

	if initiator {
		if _, err := t.conn.Write(authMsg); err != nil {
			return fmt.Errorf("%w: send auth: %v", ErrBadHandshake, err)
		}
		if _, err := io.ReadFull(t.conn, remoteAuth[:]); err != nil {
			return fmt.Errorf("%w: recv ack: %v", ErrBadHandshake, err)
		}
	} else {
		if _, err := io.ReadFull(t.conn, remoteAuth[:]); err != nil {
			return fmt.Errorf("%w: recv auth: %v", ErrBadHandshake, err)
		}
		if _, err := t.conn.Write(authMsg); err != nil {
			return fmt.Errorf("%w: send ack: %v", ErrBadHandshake, err)
		}
	}

	// Parse remote nonce and pubkey.
	copy(hs.remoteNonce[:], remoteAuth[:32])
	if err := hs.SetRemotePublicKey(remoteAuth[32:]); err != nil {
		return fmt.Errorf("%w: remote key: %v", ErrBadHandshake, err)
	}

	// Derive shared secrets.
	shared, err := hs.DeriveSecrets()
	if err != nil {
		return fmt.Errorf("%w: derive: %v", ErrBadHandshake, err)
	}

	return t.setupStreams(shared, initiator)
}

// setupStreams derives directional keys from shared material and initializes
// AES-CTR streams and HMAC-SHA256 MACs.
func (t *eciesTransport) setupStreams(shared []byte, initiator bool) error {
	encKey := deriveKey(shared, []byte("ecies-enc"))
	decKey := deriveKey(shared, []byte("ecies-dec"))
	eMACKey := deriveKey(shared, []byte("ecies-egress-mac"))
	iMACKey := deriveKey(shared, []byte("ecies-ingress-mac"))

	if !initiator {
		encKey, decKey = decKey, encKey
		eMACKey, iMACKey = iMACKey, eMACKey
	}

	encBlock, err := aes.NewCipher(encKey[:16])
	if err != nil {
		return err
	}
	t.encStream = cipher.NewCTR(encBlock, encKey[16:])

	decBlock, err := aes.NewCipher(decKey[:16])
	if err != nil {
		return err
	}
	t.decStream = cipher.NewCTR(decBlock, decKey[16:])

	t.egressMAC = hmac.New(sha256.New, eMACKey)
	t.ingrMAC = hmac.New(sha256.New, iMACKey)
	t.handshook = true
	return nil
}

// ReadFrame reads and decrypts a single frame from the connection.
// Frame format: [4-byte encrypted length][16 MAC][encrypted payload][16 MAC]
func (t *eciesTransport) ReadFrame() ([]byte, error) {
	t.rmu.Lock()
	defer t.rmu.Unlock()

	if !t.handshook {
		return nil, errors.New("p2p: ecies not handshook")
	}

	// Read encrypted 4-byte header.
	var encHdr [4]byte
	if _, err := io.ReadFull(t.conn, encHdr[:]); err != nil {
		return nil, err
	}
	// Read header MAC.
	var hdrMAC [frameMACLen]byte
	if _, err := io.ReadFull(t.conn, hdrMAC[:]); err != nil {
		return nil, err
	}
	// Verify header MAC.
	t.ingrMAC.Reset()
	t.ingrMAC.Write(encHdr[:])
	if !hmac.Equal(hdrMAC[:], t.ingrMAC.Sum(nil)[:frameMACLen]) {
		return nil, ErrFrameMACMismatch
	}
	// Decrypt header.
	var hdr [4]byte
	t.decStream.XORKeyStream(hdr[:], encHdr[:])
	frameLen := binary.BigEndian.Uint32(hdr[:])
	if frameLen > maxFramePayload {
		return nil, fmt.Errorf("%w: %d", ErrFrameTooLarge, frameLen)
	}

	// Read encrypted body.
	encBody := make([]byte, frameLen)
	if _, err := io.ReadFull(t.conn, encBody); err != nil {
		return nil, err
	}
	// Read body MAC.
	var bodyMAC [frameMACLen]byte
	if _, err := io.ReadFull(t.conn, bodyMAC[:]); err != nil {
		return nil, err
	}
	// Verify body MAC.
	t.ingrMAC.Reset()
	t.ingrMAC.Write(encBody)
	if !hmac.Equal(bodyMAC[:], t.ingrMAC.Sum(nil)[:frameMACLen]) {
		return nil, ErrFrameMACMismatch
	}
	// Decrypt body.
	body := make([]byte, frameLen)
	t.decStream.XORKeyStream(body, encBody)
	return body, nil
}

// WriteFrame encrypts and writes a frame to the connection.
func (t *eciesTransport) WriteFrame(data []byte) error {
	t.wmu.Lock()
	defer t.wmu.Unlock()

	if !t.handshook {
		return errors.New("p2p: ecies not handshook")
	}
	if len(data) > maxFramePayload {
		return fmt.Errorf("%w: %d", ErrFrameTooLarge, len(data))
	}

	// Encrypt header.
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(data)))
	var encHdr [4]byte
	t.encStream.XORKeyStream(encHdr[:], hdr[:])
	// Header MAC.
	t.egressMAC.Reset()
	t.egressMAC.Write(encHdr[:])
	hdrMAC := t.egressMAC.Sum(nil)[:frameMACLen]

	// Encrypt body.
	encBody := make([]byte, len(data))
	t.encStream.XORKeyStream(encBody, data)
	// Body MAC.
	t.egressMAC.Reset()
	t.egressMAC.Write(encBody)
	bodyMAC := t.egressMAC.Sum(nil)[:frameMACLen]

	// Write: encHdr + hdrMAC + encBody + bodyMAC.
	buf := make([]byte, 0, 4+frameMACLen+len(data)+frameMACLen)
	buf = append(buf, encHdr[:]...)
	buf = append(buf, hdrMAC...)
	buf = append(buf, encBody...)
	buf = append(buf, bodyMAC...)
	_, err := t.conn.Write(buf)
	return err
}

// Close closes the underlying connection.
func (t *eciesTransport) Close() error {
	return t.conn.Close()
}

// newECIESTransport wraps a net.Conn with ECIES-based RLPx framing.
func newECIESTransport(conn net.Conn) *eciesTransport {
	return &eciesTransport{conn: conn}
}

// ecdhShared computes the ECDH shared secret between a private key and a
// public key on secp256k1. Exported for testing.
func ecdhShared(prv *ecdsa.PrivateKey, pub *ecdsa.PublicKey) *big.Int {
	sx, _ := pub.Curve.ScalarMult(pub.X, pub.Y, prv.D.Bytes())
	return sx
}
