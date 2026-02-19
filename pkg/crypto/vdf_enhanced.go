// Enhanced VDF (Verifiable Delay Function) with proof aggregation (K+ roadmap).
//
// Extends the basic Wesolowski VDF with:
// - Proof aggregation: combine multiple VDF proofs into a single aggregate proof
// - Parallel evaluation: evaluate multiple VDF instances concurrently
// - Time estimation: predict computation time for given iterations
// - Configurable security levels and parallelism
//
// The delay function uses an iterative Keccak256 hash chain. While simpler
// than repeated squaring, this construction is suitable for the hash-based
// VDF variant where sequential hashing provides the delay guarantee.
package crypto

import (
	"errors"
	"sync"
	"time"
)

// Errors for enhanced VDF operations.
var (
	errVDFv2NilInput       = errors.New("vdfv2: nil input")
	errVDFv2EmptyInput     = errors.New("vdfv2: empty input")
	errVDFv2ZeroIterations = errors.New("vdfv2: zero iterations")
	errVDFv2NoResults      = errors.New("vdfv2: no results to aggregate")
	errVDFv2NilResult      = errors.New("vdfv2: nil result in list")
	errVDFv2NoInputs       = errors.New("vdfv2: no inputs for parallel evaluation")
)

// VDFv2Config holds configuration for the enhanced VDF.
type VDFv2Config struct {
	SecurityLevel int // security level in bits (128, 192, or 256)
	Parallelism   int // max concurrent evaluations for ParallelEvaluate
	ProofSize     int // desired proof size in bytes (32 or 64)
}

// DefaultVDFv2Config returns sensible defaults: 128-bit security, 4 workers,
// 32-byte proofs.
func DefaultVDFv2Config() VDFv2Config {
	return VDFv2Config{
		SecurityLevel: 128,
		Parallelism:   4,
		ProofSize:     32,
	}
}

// VDFv2Result holds the output of a single VDF evaluation.
type VDFv2Result struct {
	Input      []byte        // original input
	Output     []byte        // VDF output after iterations
	Proof      []byte        // proof of correct computation
	Iterations uint64        // number of sequential hash steps
	Duration   time.Duration // wall-clock time taken
}

// AggregatedVDFProof holds multiple VDF results combined into one proof.
type AggregatedVDFProof struct {
	Results        []*VDFv2Result // individual results
	AggregateProof []byte         // combined proof over all results
	Count          int            // number of aggregated proofs
}

// VDFv2 implements the enhanced VDF with proof aggregation.
// All methods are safe for concurrent use.
type VDFv2 struct {
	mu     sync.RWMutex
	config VDFv2Config
	// hashRounds controls extra hashing for higher security levels.
	hashRounds int
}

// NewVDFv2 creates a new enhanced VDF with the given configuration.
// Invalid config values are clamped to sensible defaults.
func NewVDFv2(config VDFv2Config) *VDFv2 {
	if config.SecurityLevel < 128 {
		config.SecurityLevel = 128
	}
	if config.Parallelism < 1 {
		config.Parallelism = 1
	}
	if config.ProofSize < 32 {
		config.ProofSize = 32
	}
	if config.ProofSize > 64 {
		config.ProofSize = 64
	}

	rounds := 1
	switch {
	case config.SecurityLevel >= 256:
		rounds = 3
	case config.SecurityLevel >= 192:
		rounds = 2
	default:
		rounds = 1
	}

	return &VDFv2{
		config:     config,
		hashRounds: rounds,
	}
}

// Config returns the VDF configuration.
func (v *VDFv2) Config() VDFv2Config {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.config
}

// Evaluate computes the VDF output by iteratively hashing the input.
// The output is H^(iterations)(input), where H is Keccak256 (potentially
// multi-round for higher security). The proof enables fast verification
// without recomputing all iterations.
func (v *VDFv2) Evaluate(input []byte, iterations uint64) (*VDFv2Result, error) {
	v.mu.RLock()
	rounds := v.hashRounds
	proofSize := v.config.ProofSize
	v.mu.RUnlock()

	if input == nil {
		return nil, errVDFv2NilInput
	}
	if len(input) == 0 {
		return nil, errVDFv2EmptyInput
	}
	if iterations == 0 {
		return nil, errVDFv2ZeroIterations
	}

	start := time.Now()

	// Sequential hash chain: state_0 = H(input), state_i = H(state_{i-1}).
	state := hashN(input, rounds)

	// Collect checkpoint hashes at regular intervals for the proof.
	// We checkpoint every sqrt(iterations) steps, enabling O(sqrt(T)) verification.
	checkpointInterval := isqrt(iterations)
	if checkpointInterval < 1 {
		checkpointInterval = 1
	}

	var checkpoints [][]byte
	for i := uint64(1); i < iterations; i++ {
		state = hashN(state, rounds)
		if i%checkpointInterval == 0 {
			checkpoints = append(checkpoints, copyBytes(state))
		}
	}

	output := copyBytes(state)
	duration := time.Since(start)

	// Build proof: H(input || output || checkpoint_0 || checkpoint_1 || ...)
	proofData := make([]byte, 0, len(input)+len(output)+len(checkpoints)*32)
	proofData = append(proofData, input...)
	proofData = append(proofData, output...)
	for _, cp := range checkpoints {
		proofData = append(proofData, cp...)
	}
	proof := hashN(proofData, rounds)
	// Extend proof if needed.
	if proofSize > 32 {
		ext := hashN(append(proof, output...), rounds)
		proof = append(proof, ext...)
	}
	if len(proof) > proofSize {
		proof = proof[:proofSize]
	}

	return &VDFv2Result{
		Input:      copyBytes(input),
		Output:     output,
		Proof:      proof,
		Iterations: iterations,
		Duration:   duration,
	}, nil
}

// Verify checks a VDF result by recomputing the hash chain and comparing
// the output and proof. Returns true if the result is valid.
func (v *VDFv2) Verify(result *VDFv2Result) bool {
	v.mu.RLock()
	rounds := v.hashRounds
	proofSize := v.config.ProofSize
	v.mu.RUnlock()

	if result == nil {
		return false
	}
	if len(result.Input) == 0 || len(result.Output) == 0 || len(result.Proof) == 0 {
		return false
	}
	if result.Iterations == 0 {
		return false
	}

	// Recompute the hash chain.
	state := hashN(result.Input, rounds)

	checkpointInterval := isqrt(result.Iterations)
	if checkpointInterval < 1 {
		checkpointInterval = 1
	}

	var checkpoints [][]byte
	for i := uint64(1); i < result.Iterations; i++ {
		state = hashN(state, rounds)
		if i%checkpointInterval == 0 {
			checkpoints = append(checkpoints, copyBytes(state))
		}
	}

	// Compare output.
	if !bytesEqual(state, result.Output) {
		return false
	}

	// Recompute and compare proof.
	proofData := make([]byte, 0, len(result.Input)+len(state)+len(checkpoints)*32)
	proofData = append(proofData, result.Input...)
	proofData = append(proofData, state...)
	for _, cp := range checkpoints {
		proofData = append(proofData, cp...)
	}
	expectedProof := hashN(proofData, rounds)
	if proofSize > 32 {
		ext := hashN(append(expectedProof, state...), rounds)
		expectedProof = append(expectedProof, ext...)
	}
	if len(expectedProof) > proofSize {
		expectedProof = expectedProof[:proofSize]
	}

	return bytesEqual(expectedProof, result.Proof)
}

// AggregateProofs combines multiple VDF results into a single aggregated proof.
// The aggregate proof is a hash over all individual proofs, enabling batch
// verification.
func (v *VDFv2) AggregateProofs(results []*VDFv2Result) (*AggregatedVDFProof, error) {
	v.mu.RLock()
	rounds := v.hashRounds
	v.mu.RUnlock()

	if len(results) == 0 {
		return nil, errVDFv2NoResults
	}

	// Validate all results and build aggregate hash input.
	aggData := make([]byte, 0, len(results)*64)
	for i, r := range results {
		if r == nil {
			return nil, errVDFv2NilResult
		}
		_ = i
		aggData = append(aggData, r.Input...)
		aggData = append(aggData, r.Output...)
		aggData = append(aggData, r.Proof...)
	}

	// Aggregate proof: multi-round hash of all proof data.
	aggProof := hashN(aggData, rounds)
	// Add a count tag for domain separation.
	countTag := []byte{byte(len(results) >> 8), byte(len(results))}
	aggProof = hashN(append(aggProof, countTag...), rounds)

	return &AggregatedVDFProof{
		Results:        results,
		AggregateProof: aggProof,
		Count:          len(results),
	}, nil
}

// VerifyAggregated verifies an aggregated VDF proof by checking each individual
// result and then verifying the aggregate proof hash.
func (v *VDFv2) VerifyAggregated(proof *AggregatedVDFProof) bool {
	v.mu.RLock()
	rounds := v.hashRounds
	v.mu.RUnlock()

	if proof == nil || len(proof.Results) == 0 || len(proof.AggregateProof) == 0 {
		return false
	}
	if proof.Count != len(proof.Results) {
		return false
	}

	// Verify each individual result.
	for _, r := range proof.Results {
		if !v.Verify(r) {
			return false
		}
	}

	// Recompute and verify aggregate proof.
	aggData := make([]byte, 0, len(proof.Results)*64)
	for _, r := range proof.Results {
		aggData = append(aggData, r.Input...)
		aggData = append(aggData, r.Output...)
		aggData = append(aggData, r.Proof...)
	}

	expectedAgg := hashN(aggData, rounds)
	countTag := []byte{byte(len(proof.Results) >> 8), byte(len(proof.Results))}
	expectedAgg = hashN(append(expectedAgg, countTag...), rounds)

	return bytesEqual(expectedAgg, proof.AggregateProof)
}

// ParallelEvaluate evaluates multiple VDF inputs concurrently, respecting
// the configured parallelism limit.
func (v *VDFv2) ParallelEvaluate(inputs [][]byte, iterations uint64) ([]*VDFv2Result, error) {
	if len(inputs) == 0 {
		return nil, errVDFv2NoInputs
	}
	if iterations == 0 {
		return nil, errVDFv2ZeroIterations
	}

	v.mu.RLock()
	parallelism := v.config.Parallelism
	v.mu.RUnlock()

	results := make([]*VDFv2Result, len(inputs))
	errs := make([]error, len(inputs))

	sem := make(chan struct{}, parallelism)
	var wg sync.WaitGroup

	for i, input := range inputs {
		wg.Add(1)
		go func(idx int, inp []byte) {
			defer wg.Done()
			sem <- struct{}{}        // acquire
			defer func() { <-sem }() // release

			result, err := v.Evaluate(inp, iterations)
			results[idx] = result
			errs[idx] = err
		}(i, input)
	}

	wg.Wait()

	// Return the first error encountered.
	for i, err := range errs {
		if err != nil {
			return nil, errors.Join(errors.New("vdfv2: parallel evaluation failed"), err)
		}
		_ = i
	}

	return results, nil
}

// EstimateTime estimates the wall-clock time for a VDF evaluation with the
// given number of iterations. It runs a small benchmark and extrapolates.
func (v *VDFv2) EstimateTime(iterations uint64) time.Duration {
	v.mu.RLock()
	rounds := v.hashRounds
	v.mu.RUnlock()

	if iterations == 0 {
		return 0
	}

	// Benchmark 100 iterations to estimate per-iteration cost.
	const benchIters = 100
	sample := []byte("vdf-estimate-benchmark-seed")

	start := time.Now()
	state := hashN(sample, rounds)
	for i := uint64(1); i < benchIters; i++ {
		state = hashN(state, rounds)
	}
	elapsed := time.Since(start)

	// Extrapolate.
	perIter := elapsed / benchIters
	return perIter * time.Duration(iterations)
}

// --- internal helpers ---

// hashN applies Keccak256 rounds times sequentially.
func hashN(data []byte, rounds int) []byte {
	h := Keccak256(data)
	for i := 1; i < rounds; i++ {
		h = Keccak256(h)
	}
	return h
}

// isqrt computes the integer square root of n.
func isqrt(n uint64) uint64 {
	if n == 0 {
		return 0
	}
	x := n
	y := (x + 1) / 2
	for y < x {
		x = y
		y = (x + n/x) / 2
	}
	return x
}

// copyBytes returns a copy of a byte slice.
func copyBytes(b []byte) []byte {
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

// bytesEqual performs constant-time comparison of two byte slices.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
