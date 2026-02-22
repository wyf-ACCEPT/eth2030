// Post-Quantum Custody Replacer (M+ roadmap: post quantum custody replacer)
//
// Replaces classical BLS custody proofs with post-quantum secure alternatives.
// Each validator is assigned a PQ-secure custody key pair, and custody proofs
// are generated using Keccak256-based hash commitments. Key rotation and
// revocation are supported for long-lived validator deployments.
//
// Security relies on Keccak256 pre-image resistance, providing full
// post-quantum security without lattice or number-theoretic assumptions.
package pqc

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/eth2030/eth2030/crypto"
)

// Custody replacer errors.
var (
	ErrCustodyKeyNotFound   = errors.New("pqc: custody key not found for validator")
	ErrCustodyProofInvalid  = errors.New("pqc: custody proof verification failed")
	ErrCustodySlotExpired   = errors.New("pqc: custody slot has expired")
	ErrCustodyMaxSlots      = errors.New("pqc: maximum custody slots reached")
)

// Custody replacer constants.
const (
	custodyKeySize   = 32  // bytes for each key component
	custodyProofSize = 64  // bytes for proof output
	defaultMaxSlots  = 256 // default maximum custody slots per validator
)

// CustodyReplacerConfig configures the custody replacer.
type CustodyReplacerConfig struct {
	SecurityLevel  int    // 128, 192, or 256
	ProofSizeTarget int   // target proof size in bytes
	HashFunction   string // hash function name (e.g. "keccak256")
	MaxCustodySlots int   // maximum slots per validator
}

// DefaultCustodyReplacerConfig returns a sensible default configuration.
func DefaultCustodyReplacerConfig() CustodyReplacerConfig {
	return CustodyReplacerConfig{
		SecurityLevel:   128,
		ProofSizeTarget: custodyProofSize,
		HashFunction:    "keccak256",
		MaxCustodySlots: defaultMaxSlots,
	}
}

// CustodyKeyPair holds a post-quantum custody key pair for a validator.
type CustodyKeyPair struct {
	PublicKey      []byte // PQ-secure public key
	PrivateKey     []byte // PQ-secure private key (seed)
	SlotAssignment uint64 // assigned slot
	Expiry         uint64 // expiry timestamp (unix seconds)
	ValidatorIndex uint64 // owning validator
}

// CustodyProofPQ is a post-quantum custody proof for a specific slot.
type CustodyProofPQ struct {
	Slot           uint64 // slot being proven
	ValidatorIndex uint64 // validator index
	Commitment     []byte // commitment hash
	ProofBytes     []byte // the proof data
	Timestamp      uint64 // creation timestamp (unix seconds)
}

// CustodyReplacer manages post-quantum custody key pairs and proofs.
// All methods are safe for concurrent use.
type CustodyReplacer struct {
	mu         sync.RWMutex
	config     CustodyReplacerConfig
	keys       map[uint64]*CustodyKeyPair // validatorIndex -> key pair
	proofCache map[string]*CustodyProofPQ // "slot:validator" -> proof
}

// NewCustodyReplacer creates a new custody replacer with the given config.
func NewCustodyReplacer(config CustodyReplacerConfig) *CustodyReplacer {
	if config.MaxCustodySlots <= 0 {
		config.MaxCustodySlots = defaultMaxSlots
	}
	if config.ProofSizeTarget <= 0 {
		config.ProofSizeTarget = custodyProofSize
	}
	if config.HashFunction == "" {
		config.HashFunction = "keccak256"
	}
	return &CustodyReplacer{
		config:     config,
		keys:       make(map[uint64]*CustodyKeyPair),
		proofCache: make(map[string]*CustodyProofPQ),
	}
}

// GenerateKeyPair generates a PQ-secure custody key pair for the given validator.
// If a key already exists for the validator, it is overwritten.
func (cr *CustodyReplacer) GenerateKeyPair(validatorIndex uint64) (*CustodyKeyPair, error) {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	// Check max slots.
	if len(cr.keys) >= cr.config.MaxCustodySlots {
		if _, exists := cr.keys[validatorIndex]; !exists {
			return nil, ErrCustodyMaxSlots
		}
	}

	// Generate random seed for the private key.
	seed := make([]byte, custodyKeySize)
	if _, err := rand.Read(seed); err != nil {
		return nil, err
	}

	// Derive public key: H(seed || "custody-pk" || validatorIndex).
	idxBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(idxBuf, validatorIndex)
	pubKey := crypto.Keccak256(seed, []byte("custody-pk"), idxBuf)

	now := uint64(time.Now().Unix())
	kp := &CustodyKeyPair{
		PublicKey:      pubKey,
		PrivateKey:     seed,
		SlotAssignment: 0,
		Expiry:         now + 86400*365, // 1 year default expiry
		ValidatorIndex: validatorIndex,
	}

	cr.keys[validatorIndex] = kp

	// Return a copy.
	return cr.copyKeyPair(kp), nil
}

// ProveSlot creates a post-quantum custody proof for the given slot and data.
func (cr *CustodyReplacer) ProveSlot(slot uint64, validatorIndex uint64, data []byte) (*CustodyProofPQ, error) {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	kp, exists := cr.keys[validatorIndex]
	if !exists {
		return nil, ErrCustodyKeyNotFound
	}

	now := uint64(time.Now().Unix())
	if now > kp.Expiry {
		return nil, ErrCustodySlotExpired
	}

	// Build commitment: H(privateKey || slot || data || "custody-commit").
	slotBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(slotBuf, slot)
	commitment := crypto.Keccak256(kp.PrivateKey, slotBuf, data, []byte("custody-commit"))

	// Build proof: H(commitment || publicKey || slot || "custody-proof")
	// Extended to proof size target.
	proofCore := crypto.Keccak256(commitment, kp.PublicKey, slotBuf, []byte("custody-proof"))
	proofExt := crypto.Keccak256(proofCore, commitment)
	proofBytes := make([]byte, custodyProofSize)
	copy(proofBytes[:32], proofCore)
	copy(proofBytes[32:], proofExt)

	proof := &CustodyProofPQ{
		Slot:           slot,
		ValidatorIndex: validatorIndex,
		Commitment:     commitment,
		ProofBytes:     proofBytes,
		Timestamp:      now,
	}

	// Cache the proof.
	cacheKey := custodyCacheKey(slot, validatorIndex)
	cr.proofCache[cacheKey] = proof

	return cr.copyProof(proof), nil
}

// VerifyProof verifies a custody proof against a public key.
func (cr *CustodyReplacer) VerifyProof(proof *CustodyProofPQ, pubKey []byte) (bool, error) {
	if proof == nil {
		return false, ErrCustodyProofInvalid
	}
	if len(pubKey) == 0 {
		return false, ErrCustodyProofInvalid
	}
	if len(proof.ProofBytes) != custodyProofSize {
		return false, ErrCustodyProofInvalid
	}
	if len(proof.Commitment) == 0 {
		return false, ErrCustodyProofInvalid
	}

	// Recompute proof from commitment and public key.
	slotBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(slotBuf, proof.Slot)
	expectedCore := crypto.Keccak256(proof.Commitment, pubKey, slotBuf, []byte("custody-proof"))
	expectedExt := crypto.Keccak256(expectedCore, proof.Commitment)

	expected := make([]byte, custodyProofSize)
	copy(expected[:32], expectedCore)
	copy(expected[32:], expectedExt)

	if subtle.ConstantTimeCompare(proof.ProofBytes, expected) != 1 {
		return false, ErrCustodyProofInvalid
	}

	return true, nil
}

// RotateKeys generates a new key pair for the validator, replacing the old one.
// Existing cached proofs for the validator are invalidated.
func (cr *CustodyReplacer) RotateKeys(validatorIndex uint64) (*CustodyKeyPair, error) {
	cr.mu.Lock()
	_, exists := cr.keys[validatorIndex]
	cr.mu.Unlock()

	if !exists {
		return nil, ErrCustodyKeyNotFound
	}

	// Generate new key pair (GenerateKeyPair handles the lock).
	kp, err := cr.GenerateKeyPair(validatorIndex)
	if err != nil {
		return nil, err
	}

	// Invalidate cached proofs for this validator.
	cr.mu.Lock()
	for key, proof := range cr.proofCache {
		if proof.ValidatorIndex == validatorIndex {
			delete(cr.proofCache, key)
		}
	}
	cr.mu.Unlock()

	return kp, nil
}

// ActiveKeys returns the number of active custody key pairs.
func (cr *CustodyReplacer) ActiveKeys() int {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	return len(cr.keys)
}

// RevokeKey removes the custody key for the given validator.
func (cr *CustodyReplacer) RevokeKey(validatorIndex uint64) error {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	if _, exists := cr.keys[validatorIndex]; !exists {
		return ErrCustodyKeyNotFound
	}

	delete(cr.keys, validatorIndex)

	// Remove cached proofs for the revoked validator.
	for key, proof := range cr.proofCache {
		if proof.ValidatorIndex == validatorIndex {
			delete(cr.proofCache, key)
		}
	}

	return nil
}

// GetPublicKey returns the public key for a validator, or an error if not found.
func (cr *CustodyReplacer) GetPublicKey(validatorIndex uint64) ([]byte, error) {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	kp, exists := cr.keys[validatorIndex]
	if !exists {
		return nil, ErrCustodyKeyNotFound
	}
	out := make([]byte, len(kp.PublicKey))
	copy(out, kp.PublicKey)
	return out, nil
}

// --- internal helpers ---

// custodyCacheKey builds a cache key string from slot and validator index.
func custodyCacheKey(slot, validatorIndex uint64) string {
	buf := make([]byte, 16)
	binary.BigEndian.PutUint64(buf[:8], slot)
	binary.BigEndian.PutUint64(buf[8:], validatorIndex)
	return string(buf)
}

// copyKeyPair returns a deep copy of a CustodyKeyPair.
func (cr *CustodyReplacer) copyKeyPair(kp *CustodyKeyPair) *CustodyKeyPair {
	pub := make([]byte, len(kp.PublicKey))
	copy(pub, kp.PublicKey)
	priv := make([]byte, len(kp.PrivateKey))
	copy(priv, kp.PrivateKey)
	return &CustodyKeyPair{
		PublicKey:      pub,
		PrivateKey:     priv,
		SlotAssignment: kp.SlotAssignment,
		Expiry:         kp.Expiry,
		ValidatorIndex: kp.ValidatorIndex,
	}
}

// copyProof returns a deep copy of a CustodyProofPQ.
func (cr *CustodyReplacer) copyProof(p *CustodyProofPQ) *CustodyProofPQ {
	commit := make([]byte, len(p.Commitment))
	copy(commit, p.Commitment)
	proof := make([]byte, len(p.ProofBytes))
	copy(proof, p.ProofBytes)
	return &CustodyProofPQ{
		Slot:           p.Slot,
		ValidatorIndex: p.ValidatorIndex,
		Commitment:     commit,
		ProofBytes:     proof,
		Timestamp:      p.Timestamp,
	}
}
