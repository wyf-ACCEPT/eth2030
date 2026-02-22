// EIP-7932 Algorithm Registry: maps algorithm type IDs to post-quantum
// verification functions, gas costs, and metadata. Provides a unified
// dispatch interface for verifying PQ signatures across the Ethereum protocol
// (transactions, attestations, withdrawals) with algorithm agility.
package pqc

import (
	"errors"
	"fmt"
	"sync"
)

// AlgorithmType identifies a post-quantum algorithm in the EIP-7932 registry.
type AlgorithmType uint8

const (
	// MLDSA44 is ML-DSA at security level 2 (NIST PQC round 3).
	MLDSA44 AlgorithmType = 1
	// MLDSA65 is ML-DSA at security level 3 (recommended for Ethereum).
	MLDSA65 AlgorithmType = 2
	// MLDSA87 is ML-DSA at security level 5 (highest security).
	MLDSA87 AlgorithmType = 3
	// ALG_FALCON512 is Falcon-512 (NTRU lattice, NIST level 1).
	ALG_FALCON512 AlgorithmType = 4
	// ALG_SLHDSA is SLH-DSA / SPHINCS+ (stateless hash-based).
	ALG_SLHDSA AlgorithmType = 5
)

// VerifyFunc is a signature verification function.
// Returns true if the signature is valid for the given public key and message.
type VerifyFunc func(pubkey, msg, sig []byte) bool

// AlgorithmDescriptor holds metadata and verification logic for a PQ algorithm.
type AlgorithmDescriptor struct {
	Type       AlgorithmType
	Name       string
	SigSize    int
	PubKeySize int
	GasCost    uint64
	VerifyFn   VerifyFunc
}

// PQ algorithm gas costs per EIP-8051.
const (
	GasCostMLDSA44   uint64 = 3500
	GasCostMLDSA65   uint64 = 4500
	GasCostMLDSA87   uint64 = 5500
	GasCostFalcon512 uint64 = 3000
	GasCostSLHDSA    uint64 = 8000
)

// Errors for algorithm registry operations.
var (
	ErrAlgUnknown       = errors.New("pq_registry: unknown algorithm type")
	ErrAlgAlreadyExists = errors.New("pq_registry: algorithm already registered")
	ErrAlgNilVerifyFn   = errors.New("pq_registry: nil verification function")
	ErrAlgSigMismatch   = errors.New("pq_registry: signature size mismatch")
	ErrAlgPKMismatch    = errors.New("pq_registry: public key size mismatch")
	ErrAlgRecoverFail   = errors.New("pq_registry: public key recovery not supported")
)

// PQAlgorithmRegistry maintains a thread-safe mapping of algorithm types
// to their verification functions and metadata.
type PQAlgorithmRegistry struct {
	mu         sync.RWMutex
	algorithms map[AlgorithmType]*AlgorithmDescriptor
}

// globalRegistry is the default algorithm registry, initialized on package load.
var globalRegistry *PQAlgorithmRegistry

func init() {
	globalRegistry = NewPQAlgorithmRegistry()
	globalRegistry.registerDefaults()
}

// NewPQAlgorithmRegistry creates a new empty algorithm registry.
func NewPQAlgorithmRegistry() *PQAlgorithmRegistry {
	return &PQAlgorithmRegistry{
		algorithms: make(map[AlgorithmType]*AlgorithmDescriptor),
	}
}

// GlobalRegistry returns the global algorithm registry.
func GlobalRegistry() *PQAlgorithmRegistry {
	return globalRegistry
}

// RegisterAlgorithm adds a new algorithm to the registry.
func (r *PQAlgorithmRegistry) RegisterAlgorithm(
	algType AlgorithmType,
	name string,
	sigSize, pubkeySize int,
	gasCost uint64,
	verifyFn VerifyFunc,
) error {
	if verifyFn == nil {
		return ErrAlgNilVerifyFn
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.algorithms[algType]; exists {
		return ErrAlgAlreadyExists
	}

	r.algorithms[algType] = &AlgorithmDescriptor{
		Type:       algType,
		Name:       name,
		SigSize:    sigSize,
		PubKeySize: pubkeySize,
		GasCost:    gasCost,
		VerifyFn:   verifyFn,
	}
	return nil
}

// VerifySignature dispatches signature verification to the correct algorithm.
func (r *PQAlgorithmRegistry) VerifySignature(algType AlgorithmType, pubkey, msg, sig []byte) (bool, error) {
	r.mu.RLock()
	desc, exists := r.algorithms[algType]
	r.mu.RUnlock()

	if !exists {
		return false, fmt.Errorf("%w: %d", ErrAlgUnknown, algType)
	}

	if len(sig) != desc.SigSize {
		return false, fmt.Errorf("%w: got %d, want %d", ErrAlgSigMismatch, len(sig), desc.SigSize)
	}

	if len(pubkey) != desc.PubKeySize {
		return false, fmt.Errorf("%w: got %d, want %d", ErrAlgPKMismatch, len(pubkey), desc.PubKeySize)
	}

	return desc.VerifyFn(pubkey, msg, sig), nil
}

// GasCost returns the gas cost for verifying a signature of the given algorithm.
func (r *PQAlgorithmRegistry) GasCost(algType AlgorithmType) (uint64, error) {
	r.mu.RLock()
	desc, exists := r.algorithms[algType]
	r.mu.RUnlock()

	if !exists {
		return 0, fmt.Errorf("%w: %d", ErrAlgUnknown, algType)
	}
	return desc.GasCost, nil
}

// GetAlgorithm returns the descriptor for a registered algorithm.
func (r *PQAlgorithmRegistry) GetAlgorithm(algType AlgorithmType) (*AlgorithmDescriptor, error) {
	r.mu.RLock()
	desc, exists := r.algorithms[algType]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("%w: %d", ErrAlgUnknown, algType)
	}
	// Return a copy to prevent external mutation.
	cpy := *desc
	return &cpy, nil
}

// SupportedAlgorithms returns a list of all registered algorithm type IDs.
func (r *PQAlgorithmRegistry) SupportedAlgorithms() []AlgorithmType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]AlgorithmType, 0, len(r.algorithms))
	for t := range r.algorithms {
		types = append(types, t)
	}
	return types
}

// Size returns the number of registered algorithms.
func (r *PQAlgorithmRegistry) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.algorithms)
}

// RecoverPublicKey attempts to recover a public key from a signature.
// For PQ schemes this is generally not possible (unlike ECDSA), so this
// returns the algorithm type and a best-effort extraction from the signature
// if the scheme supports it, or an error otherwise.
// Per EIP-7932 SIGRECOVER, lattice-based schemes do not support recovery;
// the public key must be transmitted alongside the signature.
func (r *PQAlgorithmRegistry) RecoverPublicKey(algType AlgorithmType, msg, sig []byte) ([]byte, error) {
	r.mu.RLock()
	_, exists := r.algorithms[algType]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("%w: %d", ErrAlgUnknown, algType)
	}

	// PQ schemes do not support public key recovery. The public key must
	// be explicitly provided in the transaction or attestation envelope.
	return nil, ErrAlgRecoverFail
}

// AlgorithmName returns the human-readable name for an algorithm type.
func (r *PQAlgorithmRegistry) AlgorithmName(algType AlgorithmType) string {
	r.mu.RLock()
	desc, exists := r.algorithms[algType]
	r.mu.RUnlock()

	if !exists {
		return "unknown"
	}
	return desc.Name
}

// IsRegistered returns whether an algorithm type is registered.
func (r *PQAlgorithmRegistry) IsRegistered(algType AlgorithmType) bool {
	r.mu.RLock()
	_, exists := r.algorithms[algType]
	r.mu.RUnlock()
	return exists
}

// TotalGasCost returns the aggregate gas cost for verifying multiple signatures.
func (r *PQAlgorithmRegistry) TotalGasCost(algTypes []AlgorithmType) (uint64, error) {
	var total uint64
	for _, t := range algTypes {
		cost, err := r.GasCost(t)
		if err != nil {
			return 0, err
		}
		total += cost
	}
	return total, nil
}

// registerDefaults populates the registry with all supported PQ algorithms.
func (r *PQAlgorithmRegistry) registerDefaults() {
	mldsa := NewMLDSASigner()

	// ML-DSA-44 (security level 2).
	r.RegisterAlgorithm(MLDSA44, "ML-DSA-44", 2420, 1312, GasCostMLDSA44,
		func(pubkey, msg, sig []byte) bool {
			// Use stub verification for level 2.
			return verifyMLDSAStub(pubkey, msg, sig, 2420, 1312)
		},
	)

	// ML-DSA-65 (security level 3) - uses real lattice verification.
	r.RegisterAlgorithm(MLDSA65, "ML-DSA-65", MLDSASignatureSize, MLDSAPublicKeySize, GasCostMLDSA65,
		func(pubkey, msg, sig []byte) bool {
			return mldsa.Verify(pubkey, msg, sig)
		},
	)

	// ML-DSA-87 (security level 5).
	r.RegisterAlgorithm(MLDSA87, "ML-DSA-87", 4627, 2592, GasCostMLDSA87,
		func(pubkey, msg, sig []byte) bool {
			return verifyMLDSAStub(pubkey, msg, sig, 4627, 2592)
		},
	)

	// Falcon-512.
	falcon := &FalconSigner{}
	r.RegisterAlgorithm(ALG_FALCON512, "Falcon-512", Falcon512SigSize, Falcon512PubKeySize, GasCostFalcon512,
		func(pubkey, msg, sig []byte) bool {
			return falcon.Verify(pubkey, msg, sig)
		},
	)

	// SLH-DSA (SPHINCS+).
	r.RegisterAlgorithm(ALG_SLHDSA, "SLH-DSA-SHA2-128f", SPHINCSSha256SigSize, SPHINCSSha256PubKeySize, GasCostSLHDSA,
		func(pubkey, msg, sig []byte) bool {
			return SPHINCSVerify(SPHINCSPublicKey(pubkey), msg, SPHINCSSignature(sig))
		},
	)
}

// verifyMLDSAStub provides a size-check verification for ML-DSA security
// levels that do not yet have full lattice implementations.
func verifyMLDSAStub(pubkey, msg, sig []byte, sigSize, pkSize int) bool {
	if len(sig) != sigSize || len(pubkey) != pkSize || len(msg) == 0 {
		return false
	}
	// Check that the signature has non-trivial content.
	for _, b := range sig[:32] {
		if b != 0 {
			return true
		}
	}
	return false
}
