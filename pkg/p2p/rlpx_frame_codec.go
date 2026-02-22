package p2p

import (
	"bytes"
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
	"time"
)

const (
	snappyMaxDecompressed = 24 * 1024 * 1024 // 24 MiB max decompressed size
	codecHeaderSize       = 16               // encrypted frame header size
	codecMACSize          = 16               // truncated HMAC-SHA256 tag size
	keepaliveInterval     = 15 * time.Second
	keepaliveTimeout      = 20 * time.Second
	maxCodecFrameSize     = 16 * 1024 * 1024 // 16 MiB max frame payload
)

var (
	ErrSnappyDecompressTooLarge = errors.New("p2p: snappy decompressed data too large")
	ErrCodecClosed              = errors.New("p2p: frame codec closed")
	ErrPongTimeout              = errors.New("p2p: pong timeout")
	ErrUnknownCapability        = errors.New("p2p: unknown capability for message code")
)

// FrameCodec implements the RLPx frame codec with AES-256-CTR encryption,
// snappy compression, capability offset multiplexing, and ping/pong keepalive.
type FrameCodec struct {
	conn      net.Conn
	encStream cipher.Stream
	decStream cipher.Stream
	egressMAC hash.Hash
	ingrMAC   hash.Hash

	snappyEnabled bool
	capOffsets    []capOffset

	lastPong      time.Time
	keepaliveDone chan struct{}
	keepaliveOnce sync.Once

	rmu, wmu, mu sync.Mutex
	closed       bool
}

// capOffset maps a capability to its message code offset and length.
type capOffset struct {
	Name    string
	Version uint
	Offset  uint64
	Length  uint64
}

// FrameCodecConfig holds the configuration for a FrameCodec.
type FrameCodecConfig struct {
	AESKey       []byte // 32-byte AES-256 key for CTR mode
	MACKey       []byte // 32-byte key for HMAC-SHA256
	Initiator    bool
	EnableSnappy bool
	Caps         []Cap
}

// NewFrameCodec creates a new RLPx frame codec. Keys must be 32+ bytes.
func NewFrameCodec(conn net.Conn, cfg FrameCodecConfig) (*FrameCodec, error) {
	if len(cfg.AESKey) < 32 {
		return nil, errors.New("p2p: AES key must be at least 32 bytes")
	}
	if len(cfg.MACKey) < 32 {
		return nil, errors.New("p2p: MAC key must be at least 32 bytes")
	}

	encKey := deriveCodecKey(cfg.AESKey, []byte("frame-enc"))
	decKey := deriveCodecKey(cfg.AESKey, []byte("frame-dec"))
	eMACKey := deriveCodecKey(cfg.MACKey, []byte("frame-egress-mac"))
	iMACKey := deriveCodecKey(cfg.MACKey, []byte("frame-ingress-mac"))

	if !cfg.Initiator {
		encKey, decKey = decKey, encKey
		eMACKey, iMACKey = iMACKey, eMACKey
	}

	encBlock, err := aes.NewCipher(encKey[:32])
	if err != nil {
		return nil, fmt.Errorf("p2p: enc cipher: %w", err)
	}
	decBlock, err := aes.NewCipher(decKey[:32])
	if err != nil {
		return nil, fmt.Errorf("p2p: dec cipher: %w", err)
	}

	encIV := sha256Hash(encKey)[:aes.BlockSize]
	decIV := sha256Hash(decKey)[:aes.BlockSize]

	fc := &FrameCodec{
		conn:          conn,
		encStream:     cipher.NewCTR(encBlock, encIV),
		decStream:     cipher.NewCTR(decBlock, decIV),
		egressMAC:     hmac.New(sha256.New, eMACKey),
		ingrMAC:       hmac.New(sha256.New, iMACKey),
		snappyEnabled: cfg.EnableSnappy,
		lastPong:      time.Now(),
		keepaliveDone: make(chan struct{}),
	}

	fc.capOffsets = computeCapOffsets(cfg.Caps)
	return fc, nil
}

// computeCapOffsets assigns message code offsets after the base protocol (0x00-0x0F).
func computeCapOffsets(caps []Cap) []capOffset {
	const baseProtoLen = 16 // base protocol: codes 0x00-0x0F
	offsets := make([]capOffset, 0, len(caps))
	offset := uint64(baseProtoLen)
	for _, c := range caps {
		length := uint64(17) // default codes per capability
		if c.Name == "eth" {
			length = 21 // eth/68 uses codes 0x00-0x14
		} else if c.Name == "snap" {
			length = 8 // snap protocol uses codes 0x00-0x07
		}
		offsets = append(offsets, capOffset{
			Name:    c.Name,
			Version: c.Version,
			Offset:  offset,
			Length:  length,
		})
		offset += length
	}
	return offsets
}

// CapOffset returns the message code offset for the given capability name.
// Returns 0, false if the capability is not found.
func (fc *FrameCodec) CapOffset(name string) (uint64, bool) {
	for _, co := range fc.capOffsets {
		if co.Name == name {
			return co.Offset, true
		}
	}
	return 0, false
}

// WriteMsg encrypts and writes a framed message.
func (fc *FrameCodec) WriteMsg(msg Msg) error {
	fc.mu.Lock()
	if fc.closed {
		fc.mu.Unlock()
		return ErrCodecClosed
	}
	fc.mu.Unlock()

	fc.wmu.Lock()
	defer fc.wmu.Unlock()

	body := make([]byte, 1+len(msg.Payload))
	body[0] = byte(msg.Code)
	copy(body[1:], msg.Payload)

	if fc.snappyEnabled {
		body = snappyEncode(body)
	}

	if len(body) > maxCodecFrameSize {
		return fmt.Errorf("%w: %d", ErrFrameTooLarge, len(body))
	}

	padded := padTo16(body)
	var header [codecHeaderSize]byte
	putUint24(header[:3], uint32(len(padded)))

	var encHeader [codecHeaderSize]byte
	fc.encStream.XORKeyStream(encHeader[:], header[:])

	fc.egressMAC.Reset()
	fc.egressMAC.Write(encHeader[:])
	headerMAC := fc.egressMAC.Sum(nil)[:codecMACSize]

	encBody := make([]byte, len(padded))
	fc.encStream.XORKeyStream(encBody, padded)

	fc.egressMAC.Reset()
	fc.egressMAC.Write(encBody)
	bodyMAC := fc.egressMAC.Sum(nil)[:codecMACSize]
	var buf bytes.Buffer
	buf.Write(encHeader[:])
	buf.Write(headerMAC)
	buf.Write(encBody)
	buf.Write(bodyMAC)

	_, err := fc.conn.Write(buf.Bytes())
	return err
}

// ReadMsg reads and decrypts a framed message.
func (fc *FrameCodec) ReadMsg() (Msg, error) {
	fc.mu.Lock()
	if fc.closed {
		fc.mu.Unlock()
		return Msg{}, ErrCodecClosed
	}
	fc.mu.Unlock()

	fc.rmu.Lock()
	defer fc.rmu.Unlock()

	var encHeader [codecHeaderSize]byte
	if _, err := io.ReadFull(fc.conn, encHeader[:]); err != nil {
		return Msg{}, err
	}

	var headerMAC [codecMACSize]byte
	if _, err := io.ReadFull(fc.conn, headerMAC[:]); err != nil {
		return Msg{}, err
	}

	fc.ingrMAC.Reset()
	fc.ingrMAC.Write(encHeader[:])
	expectedHMAC := fc.ingrMAC.Sum(nil)[:codecMACSize]
	if !hmac.Equal(headerMAC[:], expectedHMAC) {
		return Msg{}, ErrBadMAC
	}

	var header [codecHeaderSize]byte
	fc.decStream.XORKeyStream(header[:], encHeader[:])
	frameSize := getUint24(header[:3])

	if frameSize > maxCodecFrameSize {
		return Msg{}, fmt.Errorf("%w: %d", ErrFrameTooLarge, frameSize)
	}

	encBody := make([]byte, frameSize)
	if _, err := io.ReadFull(fc.conn, encBody); err != nil {
		return Msg{}, err
	}

	var bodyMAC [codecMACSize]byte
	if _, err := io.ReadFull(fc.conn, bodyMAC[:]); err != nil {
		return Msg{}, err
	}

	fc.ingrMAC.Reset()
	fc.ingrMAC.Write(encBody)
	expectedBodyMAC := fc.ingrMAC.Sum(nil)[:codecMACSize]
	if !hmac.Equal(bodyMAC[:], expectedBodyMAC) {
		return Msg{}, ErrBadMAC
	}

	body := make([]byte, frameSize)
	fc.decStream.XORKeyStream(body, encBody)

	body = unpadFrom16(body)
	if fc.snappyEnabled && len(body) > 0 {
		var err error
		body, err = snappyDecode(body, snappyMaxDecompressed)
		if err != nil {
			return Msg{}, err
		}
	}

	if len(body) == 0 {
		return Msg{}, errors.New("p2p: empty codec frame")
	}

	code := uint64(body[0])
	payload := body[1:]

	return Msg{
		Code:    code,
		Size:    uint32(len(payload)),
		Payload: payload,
	}, nil
}

func (fc *FrameCodec) SendPing() error { return fc.WriteMsg(Msg{Code: PingMsg, Size: 0}) }
func (fc *FrameCodec) SendPong() error { return fc.WriteMsg(Msg{Code: PongMsg, Size: 0}) }

// SendDisconnect sends a disconnect message and closes the codec.
func (fc *FrameCodec) SendDisconnect(reason DisconnectReason) error {
	err := fc.WriteMsg(Msg{
		Code:    DisconnectMsg,
		Size:    1,
		Payload: []byte{byte(reason)},
	})
	fc.Close()
	return err
}

// StartKeepalive starts the background ping/pong keepalive loop.
func (fc *FrameCodec) StartKeepalive() { go fc.keepaliveLoop() }
func (fc *FrameCodec) keepaliveLoop() {
	ticker := time.NewTicker(keepaliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			fc.mu.Lock()
			elapsed := time.Since(fc.lastPong)
			fc.mu.Unlock()

			if elapsed > keepaliveTimeout {
				fc.SendDisconnect(DiscNetworkError)
				return
			}
			// Ignore error; if write fails, the read loop will catch it.
			_ = fc.SendPing()

		case <-fc.keepaliveDone:
			return
		}
	}
}

func (fc *FrameCodec) HandlePong() { fc.mu.Lock(); fc.lastPong = time.Now(); fc.mu.Unlock() }

func (fc *FrameCodec) LastPong() time.Time { fc.mu.Lock(); defer fc.mu.Unlock(); return fc.lastPong }

// Close closes the frame codec.
func (fc *FrameCodec) Close() error {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if fc.closed {
		return nil
	}
	fc.closed = true
	fc.keepaliveOnce.Do(func() { close(fc.keepaliveDone) })
	return fc.conn.Close()
}

func (fc *FrameCodec) IsClosed() bool { fc.mu.Lock(); defer fc.mu.Unlock(); return fc.closed }

// --- Helper functions ---
func deriveCodecKey(material, tag []byte) []byte {
	h := sha256.New()
	h.Write(tag)
	h.Write(material)
	return h.Sum(nil) // 32 bytes
}

func sha256Hash(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}

func putUint24(b []byte, v uint32) {
	b[0] = byte(v >> 16)
	b[1] = byte(v >> 8)
	b[2] = byte(v)
}

func getUint24(b []byte) uint32 {
	return uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2])
}

func padTo16(data []byte) []byte {
	padLen := (16 - len(data)%16) % 16
	if padLen == 0 {
		return data
	}
	padded := make([]byte, len(data)+padLen)
	copy(padded, data)
	return padded
}

// unpadFrom16 removes trailing zero bytes added as padding.
func unpadFrom16(data []byte) []byte {
	end := len(data)
	for end > 1 && data[end-1] == 0 {
		end--
	}
	return data[:end]
}

// --- Snappy compression (simplified varint-length + identity encoding) ---
func snappyEncode(src []byte) []byte {
	lenBuf := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(lenBuf, uint64(len(src)))
	out := make([]byte, 0, n+len(src))
	out = append(out, lenBuf[:n]...)
	out = append(out, src...)
	return out
}

func snappyDecode(src []byte, maxSize int) ([]byte, error) {
	if len(src) == 0 {
		return nil, nil
	}
	decodedLen, n := binary.Uvarint(src)
	if n <= 0 {
		return nil, errors.New("p2p: invalid snappy length")
	}
	if int(decodedLen) > maxSize {
		return nil, ErrSnappyDecompressTooLarge
	}
	data := src[n:]
	if uint64(len(data)) != decodedLen {
		return nil, fmt.Errorf("p2p: snappy length mismatch: header=%d actual=%d", decodedLen, len(data))
	}
	out := make([]byte, decodedLen)
	copy(out, data)
	return out, nil
}

// DeriveFrameKeys derives 32-byte AES and MAC keys from handshake secrets.
func DeriveFrameKeys(sharedSecret, initiatorNonce, responderNonce []byte) (aesKey, macKey []byte) {
	nonceHash := sha256.Sum256(append(initiatorNonce, responderNonce...))
	h := sha256.New()
	h.Write(sharedSecret)
	h.Write(nonceHash[:])
	aesKey = h.Sum(nil)
	h.Reset()
	h.Write(sharedSecret)
	h.Write(aesKey)
	macKey = h.Sum(nil)
	return
}

// GenerateNonce generates a random 32-byte nonce.
func GenerateNonce() ([32]byte, error) {
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nonce, fmt.Errorf("p2p: nonce generation: %w", err)
	}
	return nonce, nil
}
