package p2p

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"
	"net"
	"sync"
)

// RLPx frame constants.
const (
	// rlpxHeaderSize is the size of the encrypted frame header (16 bytes data + 16 MAC).
	rlpxHeaderSize = 32

	// rlpxMACSize is the size of a frame MAC (HMAC-SHA256 truncated to 16 bytes).
	rlpxMACSize = 16

	// rlpxMaxFrameSize limits each frame's payload to 16 MiB.
	rlpxMaxFrameSize = 16 * 1024 * 1024
)

var (
	// ErrBadHandshake is returned when RLPx handshake negotiation fails.
	ErrBadHandshake = errors.New("p2p: rlpx handshake failed")

	// ErrBadMAC is returned when frame MAC verification fails.
	ErrBadMAC = errors.New("p2p: frame MAC mismatch")
)

// RLPxTransport implements Transport with AES-CTR encryption and HMAC-SHA256
// message authentication, matching the RLPx protocol framing. The handshake
// derives shared secrets from an exchanged nonce (simplified from the full
// ECIES-based handshake).
type RLPxTransport struct {
	conn net.Conn

	// Encryption state, set after handshake.
	encStream cipher.Stream
	decStream cipher.Stream
	egressMAC hash.Hash
	ingrMAC   hash.Hash

	rmu sync.Mutex
	wmu sync.Mutex

	handshook bool
}

// NewRLPxTransport wraps a net.Conn with RLPx-style framing.
// Call Handshake() before reading or writing messages.
func NewRLPxTransport(conn net.Conn) *RLPxTransport {
	return &RLPxTransport{conn: conn}
}

// Handshake performs a simplified key exchange. Both sides generate a random
// nonce, exchange them, and derive symmetric keys by hashing the concatenated
// nonces. The initiator sends first.
//
// In a production implementation this would use ECIES with secp256k1 keys.
func (t *RLPxTransport) Handshake(initiator bool) error {
	// Generate local nonce.
	var localNonce [32]byte
	if _, err := rand.Read(localNonce[:]); err != nil {
		return fmt.Errorf("%w: nonce generation: %v", ErrBadHandshake, err)
	}

	var remoteNonce [32]byte

	if initiator {
		// Send our nonce, then read theirs.
		if _, err := t.conn.Write(localNonce[:]); err != nil {
			return fmt.Errorf("%w: send nonce: %v", ErrBadHandshake, err)
		}
		if _, err := io.ReadFull(t.conn, remoteNonce[:]); err != nil {
			return fmt.Errorf("%w: recv nonce: %v", ErrBadHandshake, err)
		}
	} else {
		// Read their nonce, then send ours.
		if _, err := io.ReadFull(t.conn, remoteNonce[:]); err != nil {
			return fmt.Errorf("%w: recv nonce: %v", ErrBadHandshake, err)
		}
		if _, err := t.conn.Write(localNonce[:]); err != nil {
			return fmt.Errorf("%w: send nonce: %v", ErrBadHandshake, err)
		}
	}

	// Derive keys from shared material. In production this uses ECDH.
	// We concatenate nonces in a canonical order (initiator first) to ensure
	// both sides derive the same keys.
	var material []byte
	if initiator {
		material = append(localNonce[:], remoteNonce[:]...)
	} else {
		material = append(remoteNonce[:], localNonce[:]...)
	}

	// Derive 4 sub-keys by hashing with different prefixes.
	encKey := deriveKey(material, []byte("enc"))
	decKey := deriveKey(material, []byte("dec"))
	egressMACKey := deriveKey(material, []byte("egress-mac"))
	ingrMACKey := deriveKey(material, []byte("ingress-mac"))

	// For the responder, swap enc/dec and egress/ingress MAC directions.
	if !initiator {
		encKey, decKey = decKey, encKey
		egressMACKey, ingrMACKey = ingrMACKey, egressMACKey
	}

	// Set up AES-CTR streams. Use first 16 bytes of each key as AES-128 key,
	// last 16 as IV.
	encBlock, err := aes.NewCipher(encKey[:16])
	if err != nil {
		return fmt.Errorf("%w: enc cipher: %v", ErrBadHandshake, err)
	}
	t.encStream = cipher.NewCTR(encBlock, encKey[16:])

	decBlock, err := aes.NewCipher(decKey[:16])
	if err != nil {
		return fmt.Errorf("%w: dec cipher: %v", ErrBadHandshake, err)
	}
	t.decStream = cipher.NewCTR(decBlock, decKey[16:])

	t.egressMAC = hmac.New(sha256.New, egressMACKey)
	t.ingrMAC = hmac.New(sha256.New, ingrMACKey)
	t.handshook = true

	return nil
}

// ReadMsg reads and decrypts a single RLPx frame.
// Frame format: [4-byte encrypted length][MAC(16)][encrypted payload][MAC(16)]
func (t *RLPxTransport) ReadMsg() (Msg, error) {
	t.rmu.Lock()
	defer t.rmu.Unlock()

	if !t.handshook {
		return Msg{}, errors.New("p2p: rlpx not handshook")
	}

	// Read encrypted header (4-byte frame length).
	var encHeader [4]byte
	if _, err := io.ReadFull(t.conn, encHeader[:]); err != nil {
		return Msg{}, err
	}

	// Read header MAC.
	var headerMAC [rlpxMACSize]byte
	if _, err := io.ReadFull(t.conn, headerMAC[:]); err != nil {
		return Msg{}, err
	}

	// Verify header MAC.
	t.ingrMAC.Reset()
	t.ingrMAC.Write(encHeader[:])
	expectedMAC := t.ingrMAC.Sum(nil)[:rlpxMACSize]
	if !hmac.Equal(headerMAC[:], expectedMAC) {
		return Msg{}, ErrBadMAC
	}

	// Decrypt header to get frame length.
	var header [4]byte
	t.decStream.XORKeyStream(header[:], encHeader[:])
	frameLen := binary.BigEndian.Uint32(header[:])

	if frameLen > rlpxMaxFrameSize+1 {
		return Msg{}, fmt.Errorf("%w: frame too large: %d", ErrFrameTooLarge, frameLen)
	}

	// Read encrypted frame body + body MAC.
	encBody := make([]byte, frameLen)
	if _, err := io.ReadFull(t.conn, encBody); err != nil {
		return Msg{}, err
	}

	var bodyMAC [rlpxMACSize]byte
	if _, err := io.ReadFull(t.conn, bodyMAC[:]); err != nil {
		return Msg{}, err
	}

	// Verify body MAC.
	t.ingrMAC.Reset()
	t.ingrMAC.Write(encBody)
	expectedBodyMAC := t.ingrMAC.Sum(nil)[:rlpxMACSize]
	if !hmac.Equal(bodyMAC[:], expectedBodyMAC) {
		return Msg{}, ErrBadMAC
	}

	// Decrypt body.
	body := make([]byte, frameLen)
	t.decStream.XORKeyStream(body, encBody)

	if len(body) == 0 {
		return Msg{}, errors.New("p2p: empty rlpx frame")
	}

	code := uint64(body[0])
	payload := body[1:]

	return Msg{
		Code:    code,
		Size:    uint32(len(payload)),
		Payload: payload,
	}, nil
}

// WriteMsg encrypts and writes a single RLPx frame.
func (t *RLPxTransport) WriteMsg(msg Msg) error {
	t.wmu.Lock()
	defer t.wmu.Unlock()

	if !t.handshook {
		return errors.New("p2p: rlpx not handshook")
	}

	// Build plaintext frame: [code byte][payload].
	frameLen := 1 + len(msg.Payload)
	if frameLen > rlpxMaxFrameSize+1 {
		return fmt.Errorf("%w: frame too large: %d", ErrFrameTooLarge, frameLen)
	}

	// Encrypt header (4-byte length).
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(frameLen))
	var encHeader [4]byte
	t.encStream.XORKeyStream(encHeader[:], header[:])

	// Compute header MAC.
	t.egressMAC.Reset()
	t.egressMAC.Write(encHeader[:])
	headerMAC := t.egressMAC.Sum(nil)[:rlpxMACSize]

	// Encrypt body.
	body := make([]byte, frameLen)
	body[0] = byte(msg.Code)
	copy(body[1:], msg.Payload)
	encBody := make([]byte, frameLen)
	t.encStream.XORKeyStream(encBody, body)

	// Compute body MAC.
	t.egressMAC.Reset()
	t.egressMAC.Write(encBody)
	bodyMAC := t.egressMAC.Sum(nil)[:rlpxMACSize]

	// Write everything: encHeader + headerMAC + encBody + bodyMAC.
	buf := make([]byte, 0, 4+rlpxMACSize+frameLen+rlpxMACSize)
	buf = append(buf, encHeader[:]...)
	buf = append(buf, headerMAC...)
	buf = append(buf, encBody...)
	buf = append(buf, bodyMAC...)

	_, err := t.conn.Write(buf)
	return err
}

// Close closes the underlying connection.
func (t *RLPxTransport) Close() error {
	return t.conn.Close()
}

// deriveKey hashes material with a tag to produce a 32-byte key.
func deriveKey(material, tag []byte) []byte {
	h := sha256.New()
	h.Write(tag)
	h.Write(material)
	return h.Sum(nil)
}
