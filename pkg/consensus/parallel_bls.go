// Parallel BLS signature aggregation for 1M attestations per slot.
//
// Splits attestation batches across multiple worker goroutines, each
// aggregating its subset of attestations (XOR bitfields, combine sigs).
// Worker results are merged via tree-reduction (pairs merge up).
// Supports incremental aggregation and bloom-filter deduplication.
//
// Design follows the blst parallel aggregation pattern with atomic
// counters for work-stealing and configurable worker pools.
package consensus

import (
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/eth2028/eth2028/crypto"
)

// Parallel BLS aggregation constants.
const (
	// DefaultParallelWorkers is the default worker goroutine count.
	DefaultParallelWorkers = 16

	// DefaultBatchSize is the default attestations per worker batch.
	DefaultBatchSize = 4096

	// BloomFilterSize is the bloom filter size in bytes for deduplication.
	BloomFilterSize = 1 << 16 // 64 KB

	// BloomHashCount is the number of hash functions for the bloom filter.
	BloomHashCount = 4
)

// Parallel BLS errors.
var (
	ErrParallelBLSNoAtts      = errors.New("parallel_bls: no attestations to aggregate")
	ErrParallelBLSDataMismatch = errors.New("parallel_bls: attestation data mismatch")
	ErrParallelBLSDuplicate   = errors.New("parallel_bls: duplicate attestation detected")
)

// ParallelAggResult is the result of a parallel aggregation operation.
type ParallelAggResult struct {
	Aggregate       *AggregateAttestation
	ProcessedCount  int
	DuplicateCount  int
	MergeDepth      int
	WorkerBatches   int
}

// ParallelAggregatorConfig configures the parallel BLS aggregator.
type ParallelAggregatorConfig struct {
	Workers   int // number of worker goroutines
	BatchSize int // attestations per batch
}

// DefaultParallelAggregatorConfig returns default config.
func DefaultParallelAggregatorConfig() *ParallelAggregatorConfig {
	return &ParallelAggregatorConfig{
		Workers:   DefaultParallelWorkers,
		BatchSize: DefaultBatchSize,
	}
}

// ParallelAggregator performs parallel BLS signature aggregation across
// multiple goroutines, supporting 1M attestations per slot.
type ParallelAggregator struct {
	config  *ParallelAggregatorConfig
	bloom   []byte // bloom filter for deduplication
	bloomMu sync.Mutex

	// Metrics tracked via atomics.
	totalAggregated atomic.Int64
	totalDuplicates atomic.Int64
	totalMerges     atomic.Int64
}

// NewParallelAggregator creates a new parallel aggregator.
func NewParallelAggregator(cfg *ParallelAggregatorConfig) *ParallelAggregator {
	if cfg == nil {
		cfg = DefaultParallelAggregatorConfig()
	}
	if cfg.Workers < 1 {
		cfg.Workers = 1
	}
	if cfg.BatchSize < 1 {
		cfg.BatchSize = DefaultBatchSize
	}
	return &ParallelAggregator{
		config: cfg,
		bloom:  make([]byte, BloomFilterSize),
	}
}

// Aggregate performs parallel aggregation of attestations sharing the
// same AttestationData. Splits input across workers, each aggregates
// its batch, then merges via tree-reduction.
func (pa *ParallelAggregator) Aggregate(atts []*AggregateAttestation) (*ParallelAggResult, error) {
	if len(atts) == 0 {
		return nil, ErrParallelBLSNoAtts
	}
	if len(atts) == 1 {
		return &ParallelAggResult{
			Aggregate:      copyAggregateAttestation(atts[0]),
			ProcessedCount: 1,
			MergeDepth:     0,
			WorkerBatches:  1,
		}, nil
	}

	// Verify all attestations share the same data.
	for i := 1; i < len(atts); i++ {
		if !IsEqualAttestationData(&atts[0].Data, &atts[i].Data) {
			return nil, ErrParallelBLSDataMismatch
		}
	}

	// Determine batch count and size.
	workers := pa.config.Workers
	if workers > len(atts) {
		workers = len(atts)
	}
	batchSize := (len(atts) + workers - 1) / workers

	// Phase 1: parallel batch aggregation.
	results := make([]*AggregateAttestation, workers)
	dupCounts := make([]int, workers)
	var wg sync.WaitGroup
	var workIdx atomic.Int64

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				idx := int(workIdx.Add(1)) - 1
				if idx >= workers {
					return
				}
				start := idx * batchSize
				end := start + batchSize
				if end > len(atts) {
					end = len(atts)
				}
				if start >= end {
					return
				}
				agg, dups := pa.aggregateBatch(atts[start:end])
				results[idx] = agg
				dupCounts[idx] = dups
			}
		}()
	}
	wg.Wait()

	// Compact non-nil results.
	var partials []*AggregateAttestation
	totalDups := 0
	for i, r := range results {
		if r != nil {
			partials = append(partials, r)
		}
		totalDups += dupCounts[i]
	}

	if len(partials) == 0 {
		return nil, ErrParallelBLSNoAtts
	}

	// Phase 2: tree-reduction merge.
	merged, depth := pa.treeReduce(partials)

	pa.totalAggregated.Add(int64(len(atts)))
	pa.totalDuplicates.Add(int64(totalDups))
	pa.totalMerges.Add(int64(depth))

	return &ParallelAggResult{
		Aggregate:      merged,
		ProcessedCount: len(atts),
		DuplicateCount: totalDups,
		MergeDepth:     depth,
		WorkerBatches:  workers,
	}, nil
}

// aggregateBatch aggregates a batch of attestations sequentially.
// Returns the aggregate and count of duplicates detected.
func (pa *ParallelAggregator) aggregateBatch(batch []*AggregateAttestation) (*AggregateAttestation, int) {
	if len(batch) == 0 {
		return nil, 0
	}

	dups := 0
	result := copyAggregateAttestation(batch[0])

	for i := 1; i < len(batch); i++ {
		att := batch[i]
		// Check for overlapping bits (skip duplicates).
		if BitfieldOverlaps(result.AggregationBits, att.AggregationBits) {
			dups++
			continue
		}
		// Merge: OR bitfields and aggregate signatures.
		result.AggregationBits = BitfieldOR(result.AggregationBits, att.AggregationBits)
		result.Signature = aggregateSigPair(result.Signature, att.Signature)
	}

	return result, dups
}

// treeReduce merges partial aggregates using tree-reduction (pairs merge up).
// Returns the final aggregate and the merge depth.
func (pa *ParallelAggregator) treeReduce(partials []*AggregateAttestation) (*AggregateAttestation, int) {
	if len(partials) == 1 {
		return partials[0], 0
	}

	depth := 0
	current := partials

	for len(current) > 1 {
		depth++
		next := make([]*AggregateAttestation, 0, (len(current)+1)/2)

		for i := 0; i < len(current); i += 2 {
			if i+1 < len(current) {
				merged := mergeAggregatesParallel(current[i], current[i+1])
				next = append(next, merged)
			} else {
				next = append(next, current[i])
			}
		}
		current = next
	}

	return current[0], depth
}

// mergeAggregatesParallel merges two aggregates (bitfield OR + sig aggregation).
func mergeAggregatesParallel(a, b *AggregateAttestation) *AggregateAttestation {
	bits := BitfieldOR(a.AggregationBits, b.AggregationBits)
	sig := aggregateSigPair(a.Signature, b.Signature)
	return &AggregateAttestation{
		Data:            a.Data,
		AggregationBits: bits,
		Signature:       sig,
	}
}

// aggregateSigPair combines two 96-byte signatures by XOR (placeholder for
// real BLS aggregation which would use point addition on G2).
func aggregateSigPair(a, b [96]byte) [96]byte {
	var out [96]byte
	for i := 0; i < 96; i++ {
		out[i] = a[i] ^ b[i]
	}
	return out
}

// IncrementalAggregate adds new attestations to an existing aggregate.
func (pa *ParallelAggregator) IncrementalAggregate(
	existing *AggregateAttestation,
	newAtts []*AggregateAttestation,
) (*ParallelAggResult, error) {
	if existing == nil {
		return pa.Aggregate(newAtts)
	}
	if len(newAtts) == 0 {
		return &ParallelAggResult{
			Aggregate:      copyAggregateAttestation(existing),
			ProcessedCount: 0,
		}, nil
	}

	// Filter out attestations overlapping with existing.
	var compatible []*AggregateAttestation
	dups := 0
	for _, att := range newAtts {
		if !IsEqualAttestationData(&existing.Data, &att.Data) {
			continue
		}
		if BitfieldOverlaps(existing.AggregationBits, att.AggregationBits) {
			dups++
			continue
		}
		compatible = append(compatible, att)
	}

	if len(compatible) == 0 {
		return &ParallelAggResult{
			Aggregate:      copyAggregateAttestation(existing),
			ProcessedCount: 0,
			DuplicateCount: dups,
		}, nil
	}

	// Aggregate compatible attestations.
	all := make([]*AggregateAttestation, 0, len(compatible)+1)
	all = append(all, existing)
	all = append(all, compatible...)
	return pa.Aggregate(all)
}

// CheckDuplicate checks if an attestation is a duplicate using the bloom filter.
// Returns true if the attestation is likely a duplicate.
func (pa *ParallelAggregator) CheckDuplicate(att *AggregateAttestation) bool {
	if att == nil {
		return false
	}
	fp := bloomFingerprint(att)

	pa.bloomMu.Lock()
	defer pa.bloomMu.Unlock()

	for i := 0; i < BloomHashCount; i++ {
		idx := bloomIndex(fp, i)
		if pa.bloom[idx/8]&(1<<(idx%8)) == 0 {
			return false
		}
	}
	return true
}

// MarkSeen marks an attestation as seen in the bloom filter.
func (pa *ParallelAggregator) MarkSeen(att *AggregateAttestation) {
	if att == nil {
		return
	}
	fp := bloomFingerprint(att)

	pa.bloomMu.Lock()
	defer pa.bloomMu.Unlock()

	for i := 0; i < BloomHashCount; i++ {
		idx := bloomIndex(fp, i)
		pa.bloom[idx/8] |= 1 << (idx % 8)
	}
}

// ResetBloom clears the bloom filter.
func (pa *ParallelAggregator) ResetBloom() {
	pa.bloomMu.Lock()
	defer pa.bloomMu.Unlock()
	pa.bloom = make([]byte, BloomFilterSize)
}

// Metrics returns aggregation metrics.
func (pa *ParallelAggregator) Metrics() (aggregated, duplicates, merges int64) {
	return pa.totalAggregated.Load(), pa.totalDuplicates.Load(), pa.totalMerges.Load()
}

// bloomFingerprint computes a fingerprint for bloom filter insertion/lookup.
func bloomFingerprint(att *AggregateAttestation) []byte {
	var buf []byte
	s := uint64(att.Data.Slot)
	var slotBytes [8]byte
	binary.LittleEndian.PutUint64(slotBytes[:], s)
	buf = append(buf, slotBytes[:]...)
	buf = append(buf, att.Data.BeaconBlockRoot[:]...)
	buf = append(buf, att.AggregationBits...)
	buf = append(buf, att.Signature[:]...)
	return crypto.Keccak256(buf)
}

// bloomIndex computes the i-th bloom filter index from a fingerprint.
func bloomIndex(fp []byte, i int) uint {
	offset := (i * 4) % len(fp)
	if offset+4 > len(fp) {
		offset = 0
	}
	val := binary.LittleEndian.Uint32(fp[offset : offset+4])
	return uint(val) % (BloomFilterSize * 8)
}
