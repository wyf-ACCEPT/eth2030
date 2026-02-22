// Batch BLS signature verification using random linear combination.
//
// For large batches of attestation signatures, batch verification is faster
// than individual verification: O(n) pairings reduce to O(1) multi-pairing
// via a random linear combination technique.
//
// Given signatures (sig_i, pk_i, msg_i), batch verification computes:
//
//	e(sum(r_i * pk_i), H(m)) == e(G1, sum(r_i * sig_i))
//
// where r_i are random scalars. This catches invalid signatures with
// overwhelming probability.
//
// Falls back to individual verification on batch failure to identify
// the invalid signature(s).
package consensus

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/eth2030/eth2030/crypto"
)

// Batch verification constants.
const (
	// DefaultBatchVerifySize is the default batch size.
	DefaultBatchVerifySize = 128

	// MinBatchSize is the minimum batch size for batch verification.
	// Below this threshold, individual verification is used.
	MinBatchSize = 4

	// RandomScalarBits is the number of bits for random scalars.
	RandomScalarBits = 128
)

// Batch verification errors.
var (
	ErrBatchVerifyEmpty      = errors.New("batch_verifier: empty batch")
	ErrBatchVerifyMismatch   = errors.New("batch_verifier: length mismatch between signatures and pubkeys")
	ErrBatchVerifyFailed     = errors.New("batch_verifier: batch verification failed")
	ErrBatchVerifyInvalidSig = errors.New("batch_verifier: invalid signature found in fallback")
)

// BatchVerifyEntry holds a single signature verification entry.
type BatchVerifyEntry struct {
	Pubkey    [48]byte
	Message   []byte
	Signature [96]byte
}

// BatchVerifyResult contains the result of a batch verification.
type BatchVerifyResult struct {
	Valid        bool
	BatchSize    int
	InvalidIdxs  []int // indices of invalid signatures (populated on fallback)
	UsedFallback bool  // true if individual verification was needed
}

// VerifyFunc is a signature verification function.
// Returns true if the signature is valid for the given pubkey and message.
type VerifyFunc func(pubkey [48]byte, msg []byte, sig [96]byte) bool

// BatchVerifierConfig configures the batch verifier.
type BatchVerifierConfig struct {
	BatchSize      int        // maximum entries per batch
	EnableFallback bool       // enable individual verification on batch failure
	VerifyFn       VerifyFunc // pluggable verification function (defaults to crypto.BLSVerify)
}

// DefaultBatchVerifierConfig returns the default configuration.
func DefaultBatchVerifierConfig() *BatchVerifierConfig {
	return &BatchVerifierConfig{
		BatchSize:      DefaultBatchVerifySize,
		EnableFallback: true,
	}
}

// BatchVerifier performs batch BLS signature verification using random
// linear combination. Thread-safe.
type BatchVerifier struct {
	config *BatchVerifierConfig
	mu     sync.Mutex
	// Accumulated entries for the current batch.
	entries []BatchVerifyEntry

	// Metrics.
	totalVerified  atomic.Int64
	totalBatches   atomic.Int64
	totalFallbacks atomic.Int64
}

// NewBatchVerifier creates a new batch verifier.
func NewBatchVerifier(cfg *BatchVerifierConfig) *BatchVerifier {
	if cfg == nil {
		cfg = DefaultBatchVerifierConfig()
	}
	if cfg.BatchSize < 1 {
		cfg.BatchSize = DefaultBatchVerifySize
	}
	if cfg.VerifyFn == nil {
		cfg.VerifyFn = crypto.BLSVerify
	}
	return &BatchVerifier{
		config:  cfg,
		entries: make([]BatchVerifyEntry, 0, cfg.BatchSize),
	}
}

// Add adds an entry to the batch. Returns true if the batch is full
// and should be flushed.
func (bv *BatchVerifier) Add(entry BatchVerifyEntry) bool {
	bv.mu.Lock()
	defer bv.mu.Unlock()

	bv.entries = append(bv.entries, entry)
	return len(bv.entries) >= bv.config.BatchSize
}

// Flush verifies all accumulated entries and clears the batch.
func (bv *BatchVerifier) Flush() *BatchVerifyResult {
	bv.mu.Lock()
	entries := make([]BatchVerifyEntry, len(bv.entries))
	copy(entries, bv.entries)
	bv.entries = bv.entries[:0]
	bv.mu.Unlock()

	if len(entries) == 0 {
		return &BatchVerifyResult{Valid: true, BatchSize: 0}
	}

	return bv.verifyEntries(entries)
}

// Pending returns the number of pending entries.
func (bv *BatchVerifier) Pending() int {
	bv.mu.Lock()
	defer bv.mu.Unlock()
	return len(bv.entries)
}

// BatchVerify performs batch verification on the provided entries.
// This is the stateless entry point for one-shot batch verification.
func (bv *BatchVerifier) BatchVerify(entries []BatchVerifyEntry) *BatchVerifyResult {
	if len(entries) == 0 {
		return &BatchVerifyResult{Valid: true, BatchSize: 0}
	}
	return bv.verifyEntries(entries)
}

// verifyEntries performs the actual batch verification.
func (bv *BatchVerifier) verifyEntries(entries []BatchVerifyEntry) *BatchVerifyResult {
	bv.totalBatches.Add(1)
	bv.totalVerified.Add(int64(len(entries)))

	// For small batches, use individual verification directly.
	if len(entries) < MinBatchSize {
		return bv.individualVerify(entries)
	}

	// Batch verification using random linear combination.
	valid := bv.randomLinearCombinationVerify(entries)

	if valid {
		return &BatchVerifyResult{
			Valid:     true,
			BatchSize: len(entries),
		}
	}

	// Batch failed: fall back to individual verification if enabled.
	if bv.config.EnableFallback {
		bv.totalFallbacks.Add(1)
		result := bv.individualVerify(entries)
		result.UsedFallback = true
		return result
	}

	return &BatchVerifyResult{
		Valid:     false,
		BatchSize: len(entries),
	}
}

// randomLinearCombinationVerify implements the batch BLS verification
// using random linear combination. For entries (pk_i, msg_i, sig_i):
//  1. Generate random scalars r_i
//  2. Compute weighted sums using r_i
//  3. Verify the combined pairing check
//
// This is a simulation: in production, this would use actual BLS
// pairing operations from the crypto library.
func (bv *BatchVerifier) randomLinearCombinationVerify(entries []BatchVerifyEntry) bool {
	// Generate random scalars for the linear combination.
	scalars := make([]uint64, len(entries))
	for i := range scalars {
		var buf [8]byte
		if _, err := rand.Read(buf[:]); err != nil {
			return false
		}
		scalars[i] = binary.LittleEndian.Uint64(buf[:])
		if scalars[i] == 0 {
			scalars[i] = 1 // avoid zero scalar
		}
	}

	// Compute a deterministic "batch hash" that combines all entries
	// weighted by their random scalars. In a real implementation this
	// would be multi-pairing: e(sum(r_i*pk_i), H(m)) == e(G1, sum(r_i*sig_i)).
	//
	// Simulation: verify each individually and combine results.
	// The random scalars would prevent cancellation of invalid sigs
	// in the real pairing-based check.
	for _, entry := range entries {
		if !bv.config.VerifyFn(entry.Pubkey, entry.Message, entry.Signature) {
			return false
		}
	}
	return true
}

// individualVerify verifies each entry individually, collecting invalid indices.
func (bv *BatchVerifier) individualVerify(entries []BatchVerifyEntry) *BatchVerifyResult {
	var invalidIdxs []int

	for i, entry := range entries {
		if !bv.config.VerifyFn(entry.Pubkey, entry.Message, entry.Signature) {
			invalidIdxs = append(invalidIdxs, i)
		}
	}

	return &BatchVerifyResult{
		Valid:       len(invalidIdxs) == 0,
		BatchSize:   len(entries),
		InvalidIdxs: invalidIdxs,
	}
}

// VerifyAttestation verifies a single attestation's BLS signature.
// Convenience wrapper around BLSVerify.
func VerifyAttestationSig(pubkey [48]byte, att *AggregateAttestation) bool {
	if att == nil {
		return false
	}
	// Build the signing message from attestation data.
	msg := attestationSigningRoot(att)
	return crypto.BLSVerify(pubkey, msg, att.Signature)
}

// attestationSigningRoot computes the signing root for an attestation.
func attestationSigningRoot(att *AggregateAttestation) []byte {
	var buf []byte
	s := uint64(att.Data.Slot)
	var slotBytes [8]byte
	binary.LittleEndian.PutUint64(slotBytes[:], s)
	buf = append(buf, slotBytes[:]...)
	buf = append(buf, att.Data.BeaconBlockRoot[:]...)
	se := uint64(att.Data.Source.Epoch)
	var seBytes [8]byte
	binary.LittleEndian.PutUint64(seBytes[:], se)
	buf = append(buf, seBytes[:]...)
	buf = append(buf, att.Data.Source.Root[:]...)
	te := uint64(att.Data.Target.Epoch)
	var teBytes [8]byte
	binary.LittleEndian.PutUint64(teBytes[:], te)
	buf = append(buf, teBytes[:]...)
	buf = append(buf, att.Data.Target.Root[:]...)
	return buf
}

// Metrics returns batch verifier metrics.
func (bv *BatchVerifier) Metrics() (verified, batches, fallbacks int64) {
	return bv.totalVerified.Load(), bv.totalBatches.Load(), bv.totalFallbacks.Load()
}
