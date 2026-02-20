// ecies.go implements the Elliptic Curve Integrated Encryption Scheme (ECIES)
// on secp256k1 for P2P handshake message encryption. It provides ECDH key
// agreement, SHA-256-based KDF, AES-128-CTR encryption, and HMAC-SHA-256
// message authentication.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"
)

// ECIES constants.
const (
	// eciesOverhead is the byte overhead added by ECIES encryption:
	// 65 bytes (uncompressed ephemeral public key) + 16 bytes (AES IV) + 32 bytes (HMAC).
	eciesOverhead = 65 + 16 + 32

	// eciesKeyLen is the length of derived encryption and MAC keys (16 bytes each).
	eciesKeyLen = 16

	// eciesIVLen is the AES-128-CTR IV length.
	eciesIVLen = 16

	// eciesMACLen is the HMAC-SHA-256 output length.
	eciesMACLen = 32
)

var (
	// ErrInvalidPublicKey is returned when the provided public key is invalid.
	ErrInvalidPublicKey = errors.New("ecies: invalid public key")

	// ErrECIESCiphertext is returned when the ciphertext is malformed.
	ErrECIESCiphertext = errors.New("ecies: invalid ciphertext")

	// ErrMACMismatch is returned when HMAC verification fails.
	ErrMACMismatch = errors.New("ecies: MAC verification failed")

	// ErrKeyAgreement is returned when ECDH key agreement fails.
	ErrKeyAgreement = errors.New("ecies: key agreement failed")
)

// ECIESEncrypt encrypts a plaintext message for the given recipient public key
// using the ECIES scheme:
//  1. Generate an ephemeral secp256k1 key pair.
//  2. Perform ECDH to derive a shared secret.
//  3. Derive encryption (AES-128) and MAC (HMAC-SHA-256) keys via KDF.
//  4. Encrypt plaintext with AES-128-CTR.
//  5. Compute HMAC-SHA-256 over (IV || ciphertext).
//
// The output format is: [ephemeral_pubkey(65) || iv(16) || ciphertext || mac(32)].
func ECIESEncrypt(pub *ecdsa.PublicKey, plaintext []byte) ([]byte, error) {
	if pub == nil || pub.X == nil || pub.Y == nil {
		return nil, ErrInvalidPublicKey
	}
	curve := S256().(*secp256k1Curve)
	if !curve.IsOnCurve(pub.X, pub.Y) {
		return nil, ErrInvalidPublicKey
	}

	// Step 1: Generate ephemeral key pair.
	ephKey, err := GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("ecies: generate ephemeral key: %w", err)
	}

	// Step 2: ECDH key agreement.
	shared, err := ecdhAgreement(ephKey, pub)
	if err != nil {
		return nil, err
	}

	// Step 3: Derive enc + mac keys via KDF.
	encKey, macKey := eciesKDF(shared)

	// Step 4: Generate random IV and encrypt with AES-128-CTR.
	iv := make([]byte, eciesIVLen)
	if _, err := rand.Read(iv); err != nil {
		return nil, fmt.Errorf("ecies: generate IV: %w", err)
	}

	ciphertext, err := aesCTR(encKey, iv, plaintext)
	if err != nil {
		return nil, fmt.Errorf("ecies: encrypt: %w", err)
	}

	// Step 5: Compute HMAC-SHA-256 over (IV || ciphertext).
	mac := computeHMAC(macKey, iv, ciphertext)

	// Assemble output: ephemeral_pubkey || iv || ciphertext || mac.
	ephPub := FromECDSAPub(&ephKey.PublicKey)
	out := make([]byte, 0, len(ephPub)+eciesIVLen+len(ciphertext)+eciesMACLen)
	out = append(out, ephPub...)
	out = append(out, iv...)
	out = append(out, ciphertext...)
	out = append(out, mac...)
	return out, nil
}

// ECIESDecrypt decrypts an ECIES-encrypted message using the recipient's
// private key. The input format is: [ephemeral_pubkey(65) || iv(16) || ciphertext || mac(32)].
func ECIESDecrypt(prv *ecdsa.PrivateKey, data []byte) ([]byte, error) {
	if prv == nil {
		return nil, errors.New("ecies: nil private key")
	}

	// Minimum size: 65 (pubkey) + 16 (iv) + 0 (ciphertext) + 32 (mac).
	minSize := 65 + eciesIVLen + eciesMACLen
	if len(data) < minSize {
		return nil, ErrECIESCiphertext
	}

	// Parse the ephemeral public key.
	ephPubBytes := data[:65]
	if ephPubBytes[0] != 0x04 {
		return nil, ErrInvalidPublicKey
	}
	ephPub := unmarshalPubkey(ephPubBytes)
	if ephPub == nil {
		return nil, ErrInvalidPublicKey
	}

	// Validate the ephemeral public key is on the curve.
	curve := S256().(*secp256k1Curve)
	if !curve.IsOnCurve(ephPub.X, ephPub.Y) {
		return nil, ErrInvalidPublicKey
	}

	// Parse IV, ciphertext, and MAC.
	iv := data[65 : 65+eciesIVLen]
	macStart := len(data) - eciesMACLen
	ciphertext := data[65+eciesIVLen : macStart]
	msgMAC := data[macStart:]

	// Step 1: ECDH key agreement.
	shared, err := ecdhAgreement(prv, ephPub)
	if err != nil {
		return nil, err
	}

	// Step 2: Derive keys.
	encKey, macKey := eciesKDF(shared)

	// Step 3: Verify HMAC.
	expectedMAC := computeHMAC(macKey, iv, ciphertext)
	if subtle.ConstantTimeCompare(msgMAC, expectedMAC) != 1 {
		return nil, ErrMACMismatch
	}

	// Step 4: Decrypt.
	plaintext, err := aesCTR(encKey, iv, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("ecies: decrypt: %w", err)
	}
	return plaintext, nil
}

// ecdhAgreement performs ECDH key agreement on secp256k1.
// Returns the x-coordinate of the shared point as a 32-byte big-endian value.
func ecdhAgreement(prv *ecdsa.PrivateKey, pub *ecdsa.PublicKey) ([]byte, error) {
	curve := S256().(*secp256k1Curve)

	// Scalar multiplication: shared = prv.D * pub
	sx, sy := curve.ScalarMult(pub.X, pub.Y, prv.D.Bytes())

	// Check for point at infinity.
	if sx.Sign() == 0 && sy.Sign() == 0 {
		return nil, ErrKeyAgreement
	}

	// Return the x-coordinate as a 32-byte big-endian value.
	shared := make([]byte, 32)
	sxBytes := sx.Bytes()
	copy(shared[32-len(sxBytes):], sxBytes)
	return shared, nil
}

// eciesKDF derives a 32-byte key from the shared secret using a SHA-256
// based NIST SP 800-56 Concatenation KDF with a single iteration.
// Returns (encKey[16], macKey[16]).
func eciesKDF(sharedSecret []byte) (encKey, macKey []byte) {
	// KDF: key = SHA-256(counter || sharedSecret)
	// counter = 0x00000001 (big-endian uint32)
	h := sha256.New()
	h.Write([]byte{0x00, 0x00, 0x00, 0x01})
	h.Write(sharedSecret)
	derived := h.Sum(nil) // 32 bytes

	return derived[:eciesKeyLen], derived[eciesKeyLen:]
}

// aesCTR encrypts or decrypts data using AES-128-CTR.
// Since CTR mode is symmetric, the same function handles both operations.
func aesCTR(key, iv, data []byte) ([]byte, error) {
	if len(key) != eciesKeyLen {
		return nil, fmt.Errorf("ecies: invalid key length: %d", len(key))
	}
	if len(iv) != eciesIVLen {
		return nil, fmt.Errorf("ecies: invalid IV length: %d", len(iv))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	stream := cipher.NewCTR(block, iv)
	out := make([]byte, len(data))
	stream.XORKeyStream(out, data)
	return out, nil
}

// computeHMAC computes HMAC-SHA-256 over the concatenation of IV and ciphertext.
func computeHMAC(macKey, iv, ciphertext []byte) []byte {
	h := hmac.New(sha256.New, macKey)
	h.Write(iv)
	h.Write(ciphertext)
	return h.Sum(nil)
}

// unmarshalPubkey parses a 65-byte uncompressed secp256k1 public key.
func unmarshalPubkey(data []byte) *ecdsa.PublicKey {
	if len(data) != 65 || data[0] != 0x04 {
		return nil
	}
	x := new(big.Int).SetBytes(data[1:33])
	y := new(big.Int).SetBytes(data[33:65])
	return &ecdsa.PublicKey{Curve: S256(), X: x, Y: y}
}

// GenerateSharedSecret performs ECDH between two parties and returns
// the shared secret. This is a convenience wrapper around ecdhAgreement
// for use in P2P key exchange.
func GenerateSharedSecret(prv *ecdsa.PrivateKey, pub *ecdsa.PublicKey) ([]byte, error) {
	if prv == nil {
		return nil, errors.New("ecies: nil private key")
	}
	if pub == nil || pub.X == nil || pub.Y == nil {
		return nil, ErrInvalidPublicKey
	}
	curve := S256().(*secp256k1Curve)
	if !curve.IsOnCurve(pub.X, pub.Y) {
		return nil, ErrInvalidPublicKey
	}
	return ecdhAgreement(prv, pub)
}

// DeriveSessionKeys derives symmetric encryption keys from ECDH shared secret
// and nonces, suitable for establishing a bidirectional encrypted session.
// Returns (initiatorEncKey, responderEncKey, initiatorMAC, responderMAC).
func DeriveSessionKeys(sharedSecret, initiatorNonce, responderNonce []byte) (
	iEncKey, rEncKey, iMAC, rMAC []byte,
) {
	// Derive master key from shared secret + nonces.
	h := sha256.New()
	h.Write(sharedSecret)
	h.Write(initiatorNonce)
	h.Write(responderNonce)
	master := h.Sum(nil)

	// Derive 4 sub-keys using tagged hashing.
	iEncKey = taggedHash(master, []byte("initiator-enc"))
	rEncKey = taggedHash(master, []byte("responder-enc"))
	iMAC = taggedHash(master, []byte("initiator-mac"))
	rMAC = taggedHash(master, []byte("responder-mac"))
	return
}

// taggedHash returns SHA-256(tag || data), truncated to 16 bytes for use
// as an AES-128 key or HMAC key prefix.
func taggedHash(data, tag []byte) []byte {
	h := sha256.New()
	h.Write(tag)
	h.Write(data)
	full := h.Sum(nil)
	return full[:16]
}
