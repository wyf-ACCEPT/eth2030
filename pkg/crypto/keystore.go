package crypto

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// KeystoreConfig holds configuration for the keystore.
type KeystoreConfig struct {
	ScryptN int // CPU/memory cost parameter (default: 262144)
	ScryptR int // block size parameter (default: 8)
	ScryptP int // parallelization parameter (default: 1)
	KeyDir  string
}

// DefaultKeystoreConfig returns a KeystoreConfig with standard defaults.
func DefaultKeystoreConfig() KeystoreConfig {
	return KeystoreConfig{
		ScryptN: 262144,
		ScryptR: 8,
		ScryptP: 1,
		KeyDir:  "keystore",
	}
}

// EncryptedKey holds the encrypted key material and associated metadata.
type EncryptedKey struct {
	Address    types.Address
	ID         string // UUID v4
	Version    int    // always 3
	CipherText []byte
	IV         []byte
	Salt       []byte
	MAC        []byte
}

// Keystore manages encrypted private keys (thread-safe).
type Keystore struct {
	mu     sync.RWMutex
	config KeystoreConfig
	keys   map[types.Address]*EncryptedKey
}

// NewKeystore creates a new Keystore with the given configuration.
// Zero-valued config fields are replaced with defaults.
func NewKeystore(config KeystoreConfig) *Keystore {
	if config.ScryptN == 0 {
		config.ScryptN = 262144
	}
	if config.ScryptR == 0 {
		config.ScryptR = 8
	}
	if config.ScryptP == 0 {
		config.ScryptP = 1
	}
	if config.KeyDir == "" {
		config.KeyDir = "keystore"
	}
	return &Keystore{
		config: config,
		keys:   make(map[types.Address]*EncryptedKey),
	}
}

// StoreKey encrypts a private key with the given passphrase and stores it.
// The privateKey must be a 32-byte secp256k1 private key.
func (ks *Keystore) StoreKey(privateKey []byte, passphrase string) (*EncryptedKey, error) {
	if len(privateKey) != 32 {
		return nil, errors.New("keystore: private key must be 32 bytes")
	}

	addr := DeriveAddress(privateKey)

	// Generate random salt (32 bytes) and IV (16 bytes).
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("keystore: failed to generate salt: %w", err)
	}
	iv := make([]byte, 16)
	if _, err := rand.Read(iv); err != nil {
		return nil, fmt.Errorf("keystore: failed to generate IV: %w", err)
	}

	// Generate UUID v4.
	uuid, err := generateUUIDv4()
	if err != nil {
		return nil, fmt.Errorf("keystore: failed to generate UUID: %w", err)
	}

	// Derive encryption key from passphrase.
	derivedKey := deriveKey([]byte(passphrase), salt, ks.config.ScryptN)

	// Encrypt using AES-128-CTR (simplified: XOR with key stream).
	cipherText := ctrEncrypt(privateKey, derivedKey[:16], iv)

	// Compute MAC: Keccak256(derivedKey[16:32] + cipherText).
	mac := Keccak256(derivedKey[16:32], cipherText)

	ek := &EncryptedKey{
		Address:    addr,
		ID:         uuid,
		Version:    3,
		CipherText: cipherText,
		IV:         iv,
		Salt:       salt,
		MAC:        mac,
	}

	ks.mu.Lock()
	ks.keys[addr] = ek
	ks.mu.Unlock()

	return ek, nil
}

// LoadKey decrypts and returns the private key for the given address.
func (ks *Keystore) LoadKey(address types.Address, passphrase string) ([]byte, error) {
	ks.mu.RLock()
	ek, ok := ks.keys[address]
	ks.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("keystore: key not found for address %s", address.Hex())
	}

	// Re-derive the key.
	derivedKey := deriveKey([]byte(passphrase), ek.Salt, ks.config.ScryptN)

	// Verify MAC.
	expectedMAC := Keccak256(derivedKey[16:32], ek.CipherText)
	if !keystoreBytesEqual(expectedMAC, ek.MAC) {
		return nil, errors.New("keystore: wrong passphrase (MAC mismatch)")
	}

	// Decrypt.
	privateKey := ctrEncrypt(ek.CipherText, derivedKey[:16], ek.IV)
	return privateKey, nil
}

// HasKey returns true if a key exists for the given address.
func (ks *Keystore) HasKey(address types.Address) bool {
	ks.mu.RLock()
	_, ok := ks.keys[address]
	ks.mu.RUnlock()
	return ok
}

// ListAddresses returns all addresses stored in the keystore.
func (ks *Keystore) ListAddresses() []types.Address {
	ks.mu.RLock()
	defer ks.mu.RUnlock()

	addrs := make([]types.Address, 0, len(ks.keys))
	for addr := range ks.keys {
		addrs = append(addrs, addr)
	}
	return addrs
}

// DeleteKey removes the key for the given address.
func (ks *Keystore) DeleteKey(address types.Address) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	if _, ok := ks.keys[address]; !ok {
		return fmt.Errorf("keystore: key not found for address %s", address.Hex())
	}
	delete(ks.keys, address)
	return nil
}

// ChangePassphrase re-encrypts the key under a new passphrase.
func (ks *Keystore) ChangePassphrase(address types.Address, oldPass, newPass string) error {
	// Decrypt with old passphrase.
	privateKey, err := ks.LoadKey(address, oldPass)
	if err != nil {
		return err
	}

	// Remove old entry.
	ks.mu.Lock()
	delete(ks.keys, address)
	ks.mu.Unlock()

	// Re-store with new passphrase.
	_, err = ks.StoreKey(privateKey, newPass)
	return err
}

// DeriveAddress computes the Ethereum address from a 32-byte private key.
// Address = Keccak256(uncompressedPubKey[1:])[12:]
func DeriveAddress(privateKey []byte) types.Address {
	if len(privateKey) != 32 {
		return types.Address{}
	}

	// Reconstruct the ECDSA private key to get the public key.
	curve := S256()
	k := new(big.Int).SetBytes(privateKey)
	x, y := curve.ScalarBaseMult(k.Bytes())

	pub := &ecdsa.PublicKey{Curve: curve, X: x, Y: y}
	return PubkeyToAddress(*pub)
}

// deriveKey performs simplified scrypt-like key derivation:
// iteratively hashing Keccak256(passphrase + salt) for n rounds.
// Returns a 32-byte derived key.
func deriveKey(passphrase, salt []byte, n int) []byte {
	// Use a reduced iteration count based on scryptN to keep it fast.
	// Real scrypt would use memory-hard iterations; we simplify for
	// the purpose of this implementation.
	iterations := n / 1024
	if iterations < 1 {
		iterations = 1
	}
	if iterations > 4096 {
		iterations = 4096
	}

	key := Keccak256(passphrase, salt)
	for i := 1; i < iterations; i++ {
		key = Keccak256(key, salt)
	}
	return key
}

// ctrEncrypt performs AES-128-CTR-like encryption using XOR with a key stream
// derived from Keccak256(key + iv + counter) for each 32-byte block.
func ctrEncrypt(data, key, iv []byte) []byte {
	result := make([]byte, len(data))
	counter := make([]byte, 8)

	for offset := 0; offset < len(data); offset += 32 {
		// Generate key stream block: Keccak256(key + iv + counter).
		binary.BigEndian.PutUint64(counter, uint64(offset/32))
		stream := Keccak256(key, iv, counter)

		end := offset + 32
		if end > len(data) {
			end = len(data)
		}
		for i := offset; i < end; i++ {
			result[i] = data[i] ^ stream[i-offset]
		}
	}
	return result
}

// generateUUIDv4 generates a random UUID v4 string.
func generateUUIDv4() (string, error) {
	var uuid [16]byte
	if _, err := rand.Read(uuid[:]); err != nil {
		return "", err
	}
	// Set version (4) and variant (RFC 4122).
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	uuid[8] = (uuid[8] & 0x3f) | 0x80

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16]), nil
}

// keystoreBytesEqual compares two byte slices in constant-ish time.
func keystoreBytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
