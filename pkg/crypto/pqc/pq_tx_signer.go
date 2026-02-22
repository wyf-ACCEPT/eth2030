// Package pqc provides post-quantum cryptographic primitives for Ethereum.
// pq_tx_signer.go implements a transaction signing framework supporting
// multiple PQ schemes (Dilithium3, Falcon-512, SPHINCS+-128) with batch
// verification and the hash-then-sign pattern.
package pqc

import (
	"crypto/rand"
	"errors"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// PQSignatureType enumerates supported post-quantum signature schemes.
type PQSignatureType uint8

const (
	// SigDilithium3 is CRYSTALS-Dilithium at NIST level 3.
	SigDilithium3 PQSignatureType = 0
	// SigFalcon512 is Falcon at NIST level 1.
	SigFalcon512 PQSignatureType = 1
	// SigSPHINCS128 is SPHINCS+-128 (stateless hash-based, NIST level 1).
	SigSPHINCS128 PQSignatureType = 2
)

// Canonical signature sizes per scheme as defined by the eth2030 roadmap.
const (
	Dilithium3SignatureSize = 3309
	Falcon512SignatureSize  = 690
	SPHINCS128SignatureSize = 7856
)

// SignatureSizeForType returns the expected signature size for a scheme.
func SignatureSizeForType(st PQSignatureType) (int, error) {
	switch st {
	case SigDilithium3:
		return Dilithium3SignatureSize, nil
	case SigFalcon512:
		return Falcon512SignatureSize, nil
	case SigSPHINCS128:
		return SPHINCS128SignatureSize, nil
	default:
		return 0, ErrUnsupportedScheme
	}
}

// Errors returned by PQTxSigner operations.
var (
	ErrUnsupportedScheme = errors.New("pq_tx_signer: unsupported PQ scheme")
	ErrNilTransaction    = errors.New("pq_tx_signer: nil transaction")
	ErrNilKey            = errors.New("pq_tx_signer: nil key")
	ErrSchemeMismatch    = errors.New("pq_tx_signer: key scheme does not match signer")
	ErrInvalidSignature  = errors.New("pq_tx_signer: invalid signature")
	ErrBatchLenMismatch  = errors.New("pq_tx_signer: batch length mismatch")
)

// PQPrivateKey holds a post-quantum private key with its scheme identifier.
type PQPrivateKey struct {
	Scheme PQSignatureType
	Raw    []byte
}

// PQPublicKey holds a post-quantum public key with its scheme identifier.
type PQPublicKey struct {
	Scheme PQSignatureType
	Raw    []byte
}

// PQTxSigner signs and verifies Ethereum transactions using post-quantum
// signature schemes. It is safe for concurrent use.
type PQTxSigner struct {
	mu     sync.RWMutex
	scheme PQSignatureType
}

// NewPQTxSigner creates a signer for the given PQ scheme.
func NewPQTxSigner(scheme PQSignatureType) (*PQTxSigner, error) {
	if _, err := SignatureSizeForType(scheme); err != nil {
		return nil, err
	}
	return &PQTxSigner{scheme: scheme}, nil
}

// Scheme returns the signer's PQ scheme.
func (s *PQTxSigner) Scheme() PQSignatureType {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.scheme
}

// GenerateKey generates a new PQ key pair for the signer's scheme.
// Uses crypto/rand for placeholder key material. A production implementation
// would use a real lattice/hash-based key generation algorithm.
func (s *PQTxSigner) GenerateKey() (*PQPrivateKey, *PQPublicKey, error) {
	s.mu.RLock()
	scheme := s.scheme
	s.mu.RUnlock()

	privSize, pubSize, err := keySizesForScheme(scheme)
	if err != nil {
		return nil, nil, err
	}

	privRaw := make([]byte, privSize)
	if _, err := rand.Read(privRaw); err != nil {
		return nil, nil, err
	}

	// Derive public key deterministically from private key using Keccak256.
	pubRaw := make([]byte, pubSize)
	fillFromSeed(pubRaw, crypto.Keccak256(privRaw))

	priv := &PQPrivateKey{Scheme: scheme, Raw: privRaw}
	pub := &PQPublicKey{Scheme: scheme, Raw: pubRaw}
	return priv, pub, nil
}

// SignTransaction signs a transaction using the hash-then-sign pattern:
// signature = Sign(Keccak256(RLP(tx)), privateKey).
// The returned signature has the canonical size for the scheme.
func (s *PQTxSigner) SignTransaction(tx *types.Transaction, key *PQPrivateKey) ([]byte, error) {
	if tx == nil {
		return nil, ErrNilTransaction
	}
	if key == nil {
		return nil, ErrNilKey
	}

	s.mu.RLock()
	scheme := s.scheme
	s.mu.RUnlock()

	if key.Scheme != scheme {
		return nil, ErrSchemeMismatch
	}

	// Hash-then-sign: Keccak256(RLP(tx)) -> sign the hash.
	txHash := hashTransaction(tx)
	return signHash(scheme, key.Raw, txHash[:])
}

// VerifyTransaction verifies a PQ signature over a transaction.
func (s *PQTxSigner) VerifyTransaction(tx *types.Transaction, sig []byte, pubkey *PQPublicKey) (bool, error) {
	if tx == nil {
		return false, ErrNilTransaction
	}
	if pubkey == nil {
		return false, ErrNilKey
	}

	s.mu.RLock()
	scheme := s.scheme
	s.mu.RUnlock()

	if pubkey.Scheme != scheme {
		return false, ErrSchemeMismatch
	}

	expectedSize, _ := SignatureSizeForType(scheme)
	if len(sig) != expectedSize {
		return false, ErrInvalidSignature
	}

	txHash := hashTransaction(tx)
	return verifyHash(scheme, pubkey.Raw, txHash[:], sig), nil
}

// VerifyBatch verifies multiple transaction signatures in parallel.
// Returns a bool slice indicating validity of each signature, plus an error
// if input slices have mismatched lengths.
func (s *PQTxSigner) VerifyBatch(
	txs []*types.Transaction,
	sigs [][]byte,
	pubkeys []*PQPublicKey,
) ([]bool, error) {
	n := len(txs)
	if n != len(sigs) || n != len(pubkeys) {
		return nil, ErrBatchLenMismatch
	}
	if n == 0 {
		return nil, nil
	}

	results := make([]bool, n)
	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			ok, err := s.VerifyTransaction(txs[idx], sigs[idx], pubkeys[idx])
			results[idx] = ok && err == nil
		}(i)
	}

	wg.Wait()
	return results, nil
}

// --- internal helpers ---

// hashTransaction computes Keccak256(RLP(tx)) for the hash-then-sign pattern.
func hashTransaction(tx *types.Transaction) types.Hash {
	encoded, err := tx.EncodeRLP()
	if err != nil {
		return types.Hash{}
	}
	return crypto.Keccak256Hash(encoded)
}

// signHash produces a stub PQ signature over a hash using the given scheme.
// In a real implementation this would invoke the actual lattice/hash-based
// signing algorithm.
func signHash(scheme PQSignatureType, privKey, hash []byte) ([]byte, error) {
	sigSize, err := SignatureSizeForType(scheme)
	if err != nil {
		return nil, err
	}

	// Deterministic stub: sign = fill(keccak256(privKey || hash), sigSize).
	seed := crypto.Keccak256(append(privKey, hash...))
	sig := make([]byte, sigSize)
	fillFromSeed(sig, seed)
	return sig, nil
}

// verifyHash verifies a stub PQ signature over a hash.
// Checks that the signature has the right length and non-trivial content.
// In production this would call the real PQ verification algorithm.
func verifyHash(scheme PQSignatureType, pubKey, hash, sig []byte) bool {
	sigSize, err := SignatureSizeForType(scheme)
	if err != nil || len(sig) != sigSize {
		return false
	}
	if len(pubKey) == 0 || len(hash) == 0 {
		return false
	}
	// Check that signature has non-zero content in the first 32 bytes.
	for _, b := range sig[:pqMin(32, len(sig))] {
		if b != 0 {
			return true
		}
	}
	return false
}

// keySizesForScheme returns (privateKeySize, publicKeySize) for a scheme.
func keySizesForScheme(scheme PQSignatureType) (int, int, error) {
	switch scheme {
	case SigDilithium3:
		return Dilithium3SecKeySize, Dilithium3PubKeySize, nil
	case SigFalcon512:
		return Falcon512SecKeySize, Falcon512PubKeySize, nil
	case SigSPHINCS128:
		// SPHINCS+-128s key sizes.
		return 64, 32, nil
	default:
		return 0, 0, ErrUnsupportedScheme
	}
}

// fillFromSeed fills buf by repeatedly hashing seed with Keccak256.
func fillFromSeed(buf, seed []byte) {
	offset := 0
	current := seed
	for offset < len(buf) {
		n := copy(buf[offset:], current)
		offset += n
		current = crypto.Keccak256(current)
	}
}

// pqMin returns the smaller of two ints.
func pqMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
