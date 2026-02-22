// PQ blob commitment signing connects the lattice-based blob commitments from
// pq_blobs.go with the post-quantum signature schemes from crypto/pqc.
// Provides signing and verification of blob commitments for the PeerDAS
// post-quantum security layer (L+ roadmap).
//
// Supported signers: Falcon-512, SPHINCS+-SHA256, ML-DSA-65.
// Includes batch verification for efficient block-level validation.
package das

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/crypto/pqc"
)

// PQ blob signer algorithm identifiers.
const (
	PQBlobAlgFalcon  uint8 = 1
	PQBlobAlgSPHINCS uint8 = 2
	PQBlobAlgMLDSA   uint8 = 3
)

// PQ blob signer errors.
var (
	ErrPQBlobSignNilSigner    = errors.New("pq_blob_sign: nil signer")
	ErrPQBlobSignNilKey       = errors.New("pq_blob_sign: nil key pair")
	ErrPQBlobSignEmptyCommit  = errors.New("pq_blob_sign: empty commitment")
	ErrPQBlobSignBadSig       = errors.New("pq_blob_sign: invalid signature")
	ErrPQBlobSignBadPK        = errors.New("pq_blob_sign: invalid public key")
	ErrPQBlobSignUnknownAlg   = errors.New("pq_blob_sign: unknown algorithm")
	ErrPQBlobSignBatchEmpty   = errors.New("pq_blob_sign: empty batch")
	ErrPQBlobSignVerifyFailed = errors.New("pq_blob_sign: verification failed")
)

// PQBlobSignature holds a post-quantum signature over a blob commitment.
type PQBlobSignature struct {
	// Algorithm identifies the PQ scheme used (PQBlobAlgFalcon, etc.).
	Algorithm uint8

	// PublicKey is the signer's PQ public key.
	PublicKey []byte

	// Signature is the PQ signature bytes.
	Signature []byte

	// Commitment is the hash of the blob commitment being signed.
	Commitment [32]byte
}

// PQBlobSigner wraps a PQ signer for blob commitment signing operations.
// It manages key pairs and provides sign/verify operations specific to
// the PeerDAS blob commitment workflow.
type PQBlobSigner struct {
	mu      sync.RWMutex
	signer  pqc.PQSigner
	keyPair *pqc.PQKeyPair
	algID   uint8
}

// NewPQBlobSigner creates a new blob signer with the specified algorithm.
// Supported algorithms: PQBlobAlgFalcon, PQBlobAlgSPHINCS, PQBlobAlgMLDSA.
func NewPQBlobSigner(algID uint8) (*PQBlobSigner, error) {
	var signer pqc.PQSigner
	switch algID {
	case PQBlobAlgFalcon:
		signer = &pqc.FalconSigner{}
	case PQBlobAlgMLDSA:
		signer = &pqc.DilithiumSigner{}
	default:
		return nil, fmt.Errorf("%w: %d", ErrPQBlobSignUnknownAlg, algID)
	}

	kp, err := signer.GenerateKey()
	if err != nil {
		return nil, err
	}

	return &PQBlobSigner{
		signer:  signer,
		keyPair: kp,
		algID:   algID,
	}, nil
}

// NewPQBlobSignerWithKey creates a signer with a pre-existing key pair.
func NewPQBlobSignerWithKey(algID uint8, kp *pqc.PQKeyPair) (*PQBlobSigner, error) {
	if kp == nil {
		return nil, ErrPQBlobSignNilKey
	}

	var signer pqc.PQSigner
	switch algID {
	case PQBlobAlgFalcon:
		signer = &pqc.FalconSigner{}
	case PQBlobAlgMLDSA:
		signer = &pqc.DilithiumSigner{}
	default:
		return nil, fmt.Errorf("%w: %d", ErrPQBlobSignUnknownAlg, algID)
	}

	return &PQBlobSigner{
		signer:  signer,
		keyPair: kp,
		algID:   algID,
	}, nil
}

// SignBlobCommitment produces a PQ signature over a blob commitment digest.
// The signing message is: domain_tag || commitment_digest.
func SignBlobCommitment(commitment [PQCommitmentSize]byte, signer *PQBlobSigner) (PQBlobSignature, error) {
	if signer == nil {
		return PQBlobSignature{}, ErrPQBlobSignNilSigner
	}

	signer.mu.RLock()
	kp := signer.keyPair
	pqSigner := signer.signer
	algID := signer.algID
	signer.mu.RUnlock()

	if kp == nil {
		return PQBlobSignature{}, ErrPQBlobSignNilKey
	}

	// Construct the signing message: domain tag + commitment digest.
	msg := pqBlobSigningMessage(commitment[:])

	sig, err := pqSigner.Sign(kp.SecretKey, msg)
	if err != nil {
		return PQBlobSignature{}, fmt.Errorf("pq_blob_sign: %w", err)
	}

	// Compute a 32-byte summary of the commitment for the signature struct.
	commitHash := crypto.Keccak256Hash(commitment[:])

	result := PQBlobSignature{
		Algorithm:  algID,
		PublicKey:  make([]byte, len(kp.PublicKey)),
		Signature:  sig,
	}
	copy(result.PublicKey, kp.PublicKey)
	copy(result.Commitment[:], commitHash[:])
	return result, nil
}

// VerifyBlobSignature checks that a PQ blob signature is valid.
func VerifyBlobSignature(commitment [PQCommitmentSize]byte, sig PQBlobSignature) bool {
	if len(sig.PublicKey) == 0 || len(sig.Signature) == 0 {
		return false
	}

	// Reconstruct the signing message.
	msg := pqBlobSigningMessage(commitment[:])

	// Verify the commitment hash matches.
	commitHash := crypto.Keccak256Hash(commitment[:])
	if commitHash != sig.Commitment {
		return false
	}

	// Dispatch verification to the correct algorithm.
	var signer pqc.PQSigner
	switch sig.Algorithm {
	case PQBlobAlgFalcon:
		signer = &pqc.FalconSigner{}
	case PQBlobAlgMLDSA:
		signer = &pqc.DilithiumSigner{}
	default:
		return false
	}

	return signer.Verify(sig.PublicKey, msg, sig.Signature)
}

// BatchVerifyBlobSignatures verifies multiple blob signatures in parallel.
// Returns the number of valid signatures and an error if the batch is empty.
func BatchVerifyBlobSignatures(commitments [][PQCommitmentSize]byte, sigs []PQBlobSignature) (int, error) {
	if len(sigs) == 0 {
		return 0, ErrPQBlobSignBatchEmpty
	}
	if len(commitments) != len(sigs) {
		return 0, fmt.Errorf("pq_blob_sign: commitments length %d != sigs length %d",
			len(commitments), len(sigs))
	}

	n := len(sigs)
	if n <= 4 {
		// Sequential verification for small batches.
		valid := 0
		for i := 0; i < n; i++ {
			if VerifyBlobSignature(commitments[i], sigs[i]) {
				valid++
			}
		}
		return valid, nil
	}

	// Parallel verification for larger batches.
	return pqBlobBatchVerifyParallel(commitments, sigs)
}

// pqBlobBatchVerifyParallel performs parallel batch verification using goroutines.
func pqBlobBatchVerifyParallel(commitments [][PQCommitmentSize]byte, sigs []PQBlobSignature) (int, error) {
	n := len(sigs)
	workers := 4
	if n < workers {
		workers = n
	}

	type result struct {
		index int
		valid bool
	}

	results := make([]bool, n)
	var wg sync.WaitGroup
	chunkSz := (n + workers - 1) / workers

	for w := 0; w < workers; w++ {
		start := w * chunkSz
		end := start + chunkSz
		if end > n {
			end = n
		}
		if start >= n {
			break
		}

		wg.Add(1)
		go func(s, e int) {
			defer wg.Done()
			for i := s; i < e; i++ {
				results[i] = VerifyBlobSignature(commitments[i], sigs[i])
			}
		}(start, end)
	}

	wg.Wait()

	valid := 0
	for _, v := range results {
		if v {
			valid++
		}
	}
	return valid, nil
}

// pqBlobSigningMessage constructs the message to be signed for a blob commitment.
// Format: "pq-blob-commitment-v1" || commitment_bytes.
func pqBlobSigningMessage(commitment []byte) []byte {
	domain := []byte("pq-blob-commitment-v1")
	msg := make([]byte, len(domain)+len(commitment))
	copy(msg, domain)
	copy(msg[len(domain):], commitment)
	return msg
}

// PQBlobSignatureSize returns the expected signature size for the given algorithm.
func PQBlobSignatureSize(algID uint8) int {
	switch algID {
	case PQBlobAlgFalcon:
		return pqc.Falcon512SigSize
	case PQBlobAlgSPHINCS:
		return pqc.SPHINCSSha256SigSize
	case PQBlobAlgMLDSA:
		return pqc.Dilithium3SigSize
	default:
		return 0
	}
}

// PQBlobPublicKeySize returns the expected public key size for the given algorithm.
func PQBlobPublicKeySize(algID uint8) int {
	switch algID {
	case PQBlobAlgFalcon:
		return pqc.Falcon512PubKeySize
	case PQBlobAlgSPHINCS:
		return pqc.SPHINCSSha256PubKeySize
	case PQBlobAlgMLDSA:
		return pqc.Dilithium3PubKeySize
	default:
		return 0
	}
}

// EncodePQBlobSignature serializes a PQBlobSignature to bytes.
// Format: algorithm(1) || pk_len(4) || pk || sig_len(4) || sig || commitment(32).
func EncodePQBlobSignature(sig PQBlobSignature) []byte {
	// 1 + 4 + pk + 4 + sig + 32
	size := 1 + 4 + len(sig.PublicKey) + 4 + len(sig.Signature) + 32
	buf := make([]byte, size)
	offset := 0

	buf[offset] = sig.Algorithm
	offset++

	binary.BigEndian.PutUint32(buf[offset:], uint32(len(sig.PublicKey)))
	offset += 4
	copy(buf[offset:], sig.PublicKey)
	offset += len(sig.PublicKey)

	binary.BigEndian.PutUint32(buf[offset:], uint32(len(sig.Signature)))
	offset += 4
	copy(buf[offset:], sig.Signature)
	offset += len(sig.Signature)

	copy(buf[offset:], sig.Commitment[:])
	return buf
}

// DecodePQBlobSignature deserializes a PQBlobSignature from bytes.
func DecodePQBlobSignature(data []byte) (PQBlobSignature, error) {
	if len(data) < 1+4+4+32 {
		return PQBlobSignature{}, ErrPQBlobSignBadSig
	}

	var sig PQBlobSignature
	offset := 0

	sig.Algorithm = data[offset]
	offset++

	pkLen := int(binary.BigEndian.Uint32(data[offset:]))
	offset += 4
	if offset+pkLen > len(data) {
		return PQBlobSignature{}, ErrPQBlobSignBadSig
	}
	sig.PublicKey = make([]byte, pkLen)
	copy(sig.PublicKey, data[offset:offset+pkLen])
	offset += pkLen

	if offset+4 > len(data) {
		return PQBlobSignature{}, ErrPQBlobSignBadSig
	}
	sigLen := int(binary.BigEndian.Uint32(data[offset:]))
	offset += 4
	if offset+sigLen > len(data) {
		return PQBlobSignature{}, ErrPQBlobSignBadSig
	}
	sig.Signature = make([]byte, sigLen)
	copy(sig.Signature, data[offset:offset+sigLen])
	offset += sigLen

	if offset+32 > len(data) {
		return PQBlobSignature{}, ErrPQBlobSignBadSig
	}
	copy(sig.Commitment[:], data[offset:offset+32])
	return sig, nil
}

// PQBlobSignerAlgorithmName returns the human-readable name for an algorithm ID.
func PQBlobSignerAlgorithmName(algID uint8) string {
	switch algID {
	case PQBlobAlgFalcon:
		return "Falcon-512"
	case PQBlobAlgSPHINCS:
		return "SPHINCS+-SHA256"
	case PQBlobAlgMLDSA:
		return "ML-DSA-65"
	default:
		return "unknown"
	}
}
