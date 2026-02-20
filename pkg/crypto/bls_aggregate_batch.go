// Batch BLS12-381 signature aggregation and verification for high-throughput
// consensus validation.
//
// Extends the base BLS aggregation with:
//   - BLSBatchAggregator: manages pending verification jobs and batch processing
//   - Weighted aggregation for committee-based signatures
//   - Incremental aggregation for streaming scenarios
//   - Threshold signature assembly from partial signatures
//   - Parallel-safe batch submission via channels
//
// This is used by the beacon chain to efficiently process large numbers of
// attestations and sync committee signatures per slot.
package crypto

import (
	"errors"
	"math/big"
	"sync"
)

// Batch aggregation errors.
var (
	ErrBatchEmpty           = errors.New("bls_batch: no entries to verify")
	ErrBatchInvalidPubkey   = errors.New("bls_batch: invalid public key in batch")
	ErrBatchInvalidSig      = errors.New("bls_batch: invalid signature in batch")
	ErrBatchThresholdNotMet = errors.New("bls_batch: threshold not met")
	ErrBatchAlreadyAdded    = errors.New("bls_batch: duplicate entry")
	ErrBatchClosed          = errors.New("bls_batch: aggregator is closed")
	ErrBatchWeightZero      = errors.New("bls_batch: weight must be positive")
)

// BatchVerifyEntry is a single pending verification job with metadata.
type BatchVerifyEntry struct {
	PubKey    [BLSPubkeySize]byte
	Message   []byte
	Signature [BLSSignatureSize]byte
	Tag       string // optional caller-provided tag for tracking
}

// BLSBatchAggregator collects verification requests and processes them
// in batches for improved throughput. Thread-safe via mutex.
type BLSBatchAggregator struct {
	mu      sync.Mutex
	entries []BatchVerifyEntry
	closed  bool
	tags    map[string]bool
}

// NewBLSBatchAggregator creates a new batch aggregator.
func NewBLSBatchAggregator() *BLSBatchAggregator {
	return &BLSBatchAggregator{
		tags: make(map[string]bool),
	}
}

// Submit adds a verification entry to the batch. If tag is non-empty and
// already present, returns ErrBatchAlreadyAdded to prevent duplicates.
func (ba *BLSBatchAggregator) Submit(entry BatchVerifyEntry) error {
	ba.mu.Lock()
	defer ba.mu.Unlock()

	if ba.closed {
		return ErrBatchClosed
	}
	if entry.Tag != "" {
		if ba.tags[entry.Tag] {
			return ErrBatchAlreadyAdded
		}
		ba.tags[entry.Tag] = true
	}
	ba.entries = append(ba.entries, entry)
	return nil
}

// Pending returns the number of entries waiting for verification.
func (ba *BLSBatchAggregator) Pending() int {
	ba.mu.Lock()
	defer ba.mu.Unlock()
	return len(ba.entries)
}

// VerifyBatch processes all pending entries using random linear combination.
// Returns true only if every signature in the batch is valid.
// Drains the pending queue on success or failure.
func (ba *BLSBatchAggregator) VerifyBatch() (bool, error) {
	ba.mu.Lock()
	entries := make([]BatchVerifyEntry, len(ba.entries))
	copy(entries, ba.entries)
	ba.entries = ba.entries[:0]
	ba.tags = make(map[string]bool)
	ba.mu.Unlock()

	if len(entries) == 0 {
		return false, ErrBatchEmpty
	}

	// Build a BLSSignatureSet for batch verification.
	ss := NewBLSSignatureSet()
	for _, e := range entries {
		ss.Add(e.PubKey, e.Message, e.Signature)
	}
	return ss.Verify(), nil
}

// Close prevents further submissions to this aggregator.
func (ba *BLSBatchAggregator) Close() {
	ba.mu.Lock()
	defer ba.mu.Unlock()
	ba.closed = true
}

// IsClosed returns whether the aggregator has been closed.
func (ba *BLSBatchAggregator) IsClosed() bool {
	ba.mu.Lock()
	defer ba.mu.Unlock()
	return ba.closed
}

// --- Weighted Aggregation ---

// WeightedPubkey pairs a public key with a weight for committee-based
// aggregation where validators have different effective balances.
type WeightedPubkey struct {
	PubKey [BLSPubkeySize]byte
	Weight uint64
}

// AggregateWeightedPubkeys aggregates public keys with scalar weights.
// Each pubkey is multiplied by its weight before summation:
//   result = sum(weight_i * pk_i)
// This is used when validators have different effective balances.
func AggregateWeightedPubkeys(entries []WeightedPubkey) ([BLSPubkeySize]byte, error) {
	if len(entries) == 0 {
		return [BLSPubkeySize]byte{}, ErrBatchEmpty
	}

	agg := BlsG1Infinity()
	for _, e := range entries {
		if e.Weight == 0 {
			return [BLSPubkeySize]byte{}, ErrBatchWeightZero
		}
		p := DeserializeG1(e.PubKey)
		if p == nil {
			return [BLSPubkeySize]byte{}, ErrBatchInvalidPubkey
		}
		w := new(big.Int).SetUint64(e.Weight)
		scaled := blsG1ScalarMul(p, w)
		agg = blsG1Add(agg, scaled)
	}
	return SerializeG1(agg), nil
}

// --- Incremental Aggregation ---

// IncrementalAggregator builds an aggregate signature incrementally as
// individual signatures arrive. Useful for streaming scenarios where
// signatures trickle in over time (e.g., attestation aggregation).
type IncrementalAggregator struct {
	mu       sync.Mutex
	aggSig   *BlsG2Point
	aggPK    *BlsG1Point
	count    int
	pubkeys  map[[BLSPubkeySize]byte]bool
}

// NewIncrementalAggregator creates an empty incremental aggregator.
func NewIncrementalAggregator() *IncrementalAggregator {
	return &IncrementalAggregator{
		aggSig:  BlsG2Infinity(),
		aggPK:   BlsG1Infinity(),
		pubkeys: make(map[[BLSPubkeySize]byte]bool),
	}
}

// Add incorporates a new signature and its public key into the running
// aggregate. Rejects duplicate pubkeys.
func (ia *IncrementalAggregator) Add(
	pubkey [BLSPubkeySize]byte,
	sig [BLSSignatureSize]byte,
) error {
	pk := DeserializeG1(pubkey)
	if pk == nil || pk.blsG1IsInfinity() {
		return ErrBatchInvalidPubkey
	}
	s := DeserializeG2(sig)
	if s == nil || s.blsG2IsInfinity() {
		return ErrBatchInvalidSig
	}

	ia.mu.Lock()
	defer ia.mu.Unlock()

	if ia.pubkeys[pubkey] {
		return ErrBatchAlreadyAdded
	}

	ia.aggSig = blsG2Add(ia.aggSig, s)
	ia.aggPK = blsG1Add(ia.aggPK, pk)
	ia.pubkeys[pubkey] = true
	ia.count++
	return nil
}

// Count returns the number of signatures aggregated so far.
func (ia *IncrementalAggregator) Count() int {
	ia.mu.Lock()
	defer ia.mu.Unlock()
	return ia.count
}

// AggregateSignature returns the current aggregate signature.
func (ia *IncrementalAggregator) AggregateSignature() [BLSSignatureSize]byte {
	ia.mu.Lock()
	defer ia.mu.Unlock()
	return SerializeG2(ia.aggSig)
}

// AggregatePubkey returns the current aggregate public key.
func (ia *IncrementalAggregator) AggregatePubkey() [BLSPubkeySize]byte {
	ia.mu.Lock()
	defer ia.mu.Unlock()
	return SerializeG1(ia.aggPK)
}

// --- Threshold Signature Assembly ---

// ThresholdAssembler collects partial BLS signatures and assembles the
// threshold aggregate once enough shares have been collected.
type ThresholdAssembler struct {
	mu        sync.Mutex
	threshold int
	partials  [][BLSSignatureSize]byte
	signers   map[int]bool // track signer indices to prevent duplicates
}

// NewThresholdAssembler creates a new threshold assembler. The threshold
// specifies the minimum number of partial signatures needed.
func NewThresholdAssembler(threshold int) *ThresholdAssembler {
	return &ThresholdAssembler{
		threshold: threshold,
		signers:   make(map[int]bool),
	}
}

// AddPartial adds a partial signature from a given signer index.
func (ta *ThresholdAssembler) AddPartial(signerIdx int, sig [BLSSignatureSize]byte) error {
	ta.mu.Lock()
	defer ta.mu.Unlock()

	if ta.signers[signerIdx] {
		return ErrBatchAlreadyAdded
	}
	ta.signers[signerIdx] = true
	ta.partials = append(ta.partials, sig)
	return nil
}

// PartialCount returns how many partial signatures have been collected.
func (ta *ThresholdAssembler) PartialCount() int {
	ta.mu.Lock()
	defer ta.mu.Unlock()
	return len(ta.partials)
}

// IsComplete returns whether the threshold has been met.
func (ta *ThresholdAssembler) IsComplete() bool {
	ta.mu.Lock()
	defer ta.mu.Unlock()
	return len(ta.partials) >= ta.threshold
}

// Assemble combines the collected partial signatures into an aggregate.
// Returns ErrBatchThresholdNotMet if fewer than threshold partials exist.
func (ta *ThresholdAssembler) Assemble() ([BLSSignatureSize]byte, error) {
	ta.mu.Lock()
	defer ta.mu.Unlock()

	if len(ta.partials) < ta.threshold {
		return [BLSSignatureSize]byte{}, ErrBatchThresholdNotMet
	}

	return AggregateSignatures(ta.partials), nil
}
