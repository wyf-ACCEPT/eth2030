package crypto

import (
	"testing"
	"time"
)

func TestDefaultVDFv2Config(t *testing.T) {
	cfg := DefaultVDFv2Config()
	if cfg.SecurityLevel != 128 {
		t.Errorf("SecurityLevel: want 128, got %d", cfg.SecurityLevel)
	}
	if cfg.Parallelism != 4 {
		t.Errorf("Parallelism: want 4, got %d", cfg.Parallelism)
	}
	if cfg.ProofSize != 32 {
		t.Errorf("ProofSize: want 32, got %d", cfg.ProofSize)
	}
}

func TestNewVDFv2_Defaults(t *testing.T) {
	vdf := NewVDFv2(DefaultVDFv2Config())
	cfg := vdf.Config()
	if cfg.SecurityLevel != 128 {
		t.Errorf("SecurityLevel: want 128, got %d", cfg.SecurityLevel)
	}
}

func TestNewVDFv2_ClampInvalidConfig(t *testing.T) {
	// Low security level should be clamped to 128.
	vdf := NewVDFv2(VDFv2Config{SecurityLevel: 50, Parallelism: 0, ProofSize: 10})
	cfg := vdf.Config()
	if cfg.SecurityLevel < 128 {
		t.Errorf("SecurityLevel should be clamped to >= 128, got %d", cfg.SecurityLevel)
	}
	if cfg.Parallelism < 1 {
		t.Errorf("Parallelism should be clamped to >= 1, got %d", cfg.Parallelism)
	}
	if cfg.ProofSize < 32 {
		t.Errorf("ProofSize should be clamped to >= 32, got %d", cfg.ProofSize)
	}
}

func TestVDFv2_EvaluateAndVerify(t *testing.T) {
	vdf := NewVDFv2(DefaultVDFv2Config())
	input := []byte("enhanced vdf test input")

	result, err := vdf.Evaluate(input, 50)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if result.Iterations != 50 {
		t.Errorf("Iterations: want 50, got %d", result.Iterations)
	}
	if len(result.Output) != 32 {
		t.Errorf("Output length: want 32, got %d", len(result.Output))
	}
	if len(result.Proof) == 0 {
		t.Fatal("Proof is empty")
	}
	if result.Duration <= 0 {
		t.Error("Duration should be positive")
	}
	if string(result.Input) != string(input) {
		t.Error("Input should match original")
	}

	if !vdf.Verify(result) {
		t.Fatal("valid result failed verification")
	}
}

func TestVDFv2_EvaluateErrors(t *testing.T) {
	vdf := NewVDFv2(DefaultVDFv2Config())

	_, err := vdf.Evaluate(nil, 10)
	if err != errVDFv2NilInput {
		t.Errorf("nil input: want errVDFv2NilInput, got %v", err)
	}

	_, err = vdf.Evaluate([]byte{}, 10)
	if err != errVDFv2EmptyInput {
		t.Errorf("empty input: want errVDFv2EmptyInput, got %v", err)
	}

	_, err = vdf.Evaluate([]byte("test"), 0)
	if err != errVDFv2ZeroIterations {
		t.Errorf("zero iterations: want errVDFv2ZeroIterations, got %v", err)
	}
}

func TestVDFv2_VerifyRejectsNil(t *testing.T) {
	vdf := NewVDFv2(DefaultVDFv2Config())
	if vdf.Verify(nil) {
		t.Fatal("nil result should fail")
	}
	if vdf.Verify(&VDFv2Result{}) {
		t.Fatal("empty result should fail")
	}
	if vdf.Verify(&VDFv2Result{Input: []byte("a"), Output: []byte("b")}) {
		t.Fatal("result without proof should fail")
	}
}

func TestVDFv2_VerifyRejectsTampered(t *testing.T) {
	vdf := NewVDFv2(DefaultVDFv2Config())
	result, err := vdf.Evaluate([]byte("tamper test"), 30)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	// Tamper output.
	if vdf.Verify(&VDFv2Result{Input: result.Input, Output: append([]byte{0xff}, result.Output[1:]...), Proof: result.Proof, Iterations: result.Iterations}) {
		t.Fatal("tampered output should fail")
	}
	// Tamper proof.
	if vdf.Verify(&VDFv2Result{Input: result.Input, Output: result.Output, Proof: append([]byte{0xff}, result.Proof[1:]...), Iterations: result.Iterations}) {
		t.Fatal("tampered proof should fail")
	}
	// Wrong iterations.
	if vdf.Verify(&VDFv2Result{Input: result.Input, Output: result.Output, Proof: result.Proof, Iterations: result.Iterations + 1}) {
		t.Fatal("wrong iterations should fail")
	}
}

func TestVDFv2_Deterministic(t *testing.T) {
	vdf := NewVDFv2(DefaultVDFv2Config())
	input := []byte("deterministic check")

	r1, err := vdf.Evaluate(input, 20)
	if err != nil {
		t.Fatalf("first Evaluate: %v", err)
	}
	r2, err := vdf.Evaluate(input, 20)
	if err != nil {
		t.Fatalf("second Evaluate: %v", err)
	}

	if string(r1.Output) != string(r2.Output) {
		t.Fatal("same input + iterations should produce same output")
	}
	if string(r1.Proof) != string(r2.Proof) {
		t.Fatal("same input + iterations should produce same proof")
	}
}

func TestVDFv2_DifferentInputs(t *testing.T) {
	vdf := NewVDFv2(DefaultVDFv2Config())

	r1, _ := vdf.Evaluate([]byte("input A"), 20)
	r2, _ := vdf.Evaluate([]byte("input B"), 20)

	if string(r1.Output) == string(r2.Output) {
		t.Fatal("different inputs should produce different outputs")
	}
}

func TestVDFv2_DifferentIterations(t *testing.T) {
	vdf := NewVDFv2(DefaultVDFv2Config())
	input := []byte("iter comparison")

	r1, _ := vdf.Evaluate(input, 10)
	r2, _ := vdf.Evaluate(input, 20)

	if string(r1.Output) == string(r2.Output) {
		t.Fatal("different iteration counts should produce different outputs")
	}
}

func TestVDFv2_SecurityLevels(t *testing.T) {
	for _, level := range []int{128, 192, 256} {
		t.Run("", func(t *testing.T) {
			cfg := VDFv2Config{SecurityLevel: level, Parallelism: 2, ProofSize: 32}
			vdf := NewVDFv2(cfg)
			input := []byte("security level test")

			result, err := vdf.Evaluate(input, 20)
			if err != nil {
				t.Fatalf("Evaluate at level %d: %v", level, err)
			}
			if !vdf.Verify(result) {
				t.Fatalf("verification failed at level %d", level)
			}
		})
	}
}

func TestVDFv2_64ByteProof(t *testing.T) {
	cfg := VDFv2Config{SecurityLevel: 128, Parallelism: 2, ProofSize: 64}
	vdf := NewVDFv2(cfg)
	input := []byte("64 byte proof test")

	result, err := vdf.Evaluate(input, 25)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if len(result.Proof) != 64 {
		t.Errorf("proof size: want 64, got %d", len(result.Proof))
	}

	if !vdf.Verify(result) {
		t.Fatal("64-byte proof failed verification")
	}
}

func TestVDFv2_AggregateProofs(t *testing.T) {
	vdf := NewVDFv2(DefaultVDFv2Config())

	results := make([]*VDFv2Result, 3)
	for i := 0; i < 3; i++ {
		input := []byte{byte('a' + i), byte(i), 0x42}
		r, err := vdf.Evaluate(input, 15)
		if err != nil {
			t.Fatalf("Evaluate(%d): %v", i, err)
		}
		results[i] = r
	}

	agg, err := vdf.AggregateProofs(results)
	if err != nil {
		t.Fatalf("AggregateProofs: %v", err)
	}

	if agg.Count != 3 {
		t.Errorf("Count: want 3, got %d", agg.Count)
	}
	if len(agg.AggregateProof) == 0 {
		t.Fatal("AggregateProof is empty")
	}
	if len(agg.Results) != 3 {
		t.Errorf("Results count: want 3, got %d", len(agg.Results))
	}

	if !vdf.VerifyAggregated(agg) {
		t.Fatal("valid aggregated proof failed verification")
	}
}

func TestVDFv2_AggregateProofsErrors(t *testing.T) {
	vdf := NewVDFv2(DefaultVDFv2Config())
	if _, err := vdf.AggregateProofs(nil); err != errVDFv2NoResults {
		t.Errorf("nil results: want errVDFv2NoResults, got %v", err)
	}
	if _, err := vdf.AggregateProofs([]*VDFv2Result{}); err != errVDFv2NoResults {
		t.Errorf("empty results: want errVDFv2NoResults, got %v", err)
	}
	if _, err := vdf.AggregateProofs([]*VDFv2Result{nil}); err != errVDFv2NilResult {
		t.Errorf("nil in list: want errVDFv2NilResult, got %v", err)
	}
}

func TestVDFv2_VerifyAggregatedRejectsInvalid(t *testing.T) {
	vdf := NewVDFv2(DefaultVDFv2Config())
	if vdf.VerifyAggregated(nil) {
		t.Fatal("nil should fail")
	}
	if vdf.VerifyAggregated(&AggregatedVDFProof{}) {
		t.Fatal("empty should fail")
	}
	r1, _ := vdf.Evaluate([]byte("test"), 10)
	agg, _ := vdf.AggregateProofs([]*VDFv2Result{r1})
	agg.Count = 99
	if vdf.VerifyAggregated(agg) {
		t.Fatal("mismatched count should fail")
	}
	agg2, _ := vdf.AggregateProofs([]*VDFv2Result{r1})
	agg2.AggregateProof[0] ^= 0xff
	if vdf.VerifyAggregated(agg2) {
		t.Fatal("tampered aggregate proof should fail")
	}
}

func TestVDFv2_ParallelEvaluate(t *testing.T) {
	vdf := NewVDFv2(VDFv2Config{
		SecurityLevel: 128,
		Parallelism:   2,
		ProofSize:     32,
	})

	inputs := [][]byte{
		[]byte("parallel input 1"),
		[]byte("parallel input 2"),
		[]byte("parallel input 3"),
		[]byte("parallel input 4"),
	}

	results, err := vdf.ParallelEvaluate(inputs, 15)
	if err != nil {
		t.Fatalf("ParallelEvaluate: %v", err)
	}

	if len(results) != 4 {
		t.Fatalf("results count: want 4, got %d", len(results))
	}

	// Verify each result.
	for i, r := range results {
		if r == nil {
			t.Fatalf("result %d is nil", i)
		}
		if !vdf.Verify(r) {
			t.Errorf("result %d failed verification", i)
		}
		if string(r.Input) != string(inputs[i]) {
			t.Errorf("result %d input mismatch", i)
		}
	}

	// Results should be different for different inputs.
	if string(results[0].Output) == string(results[1].Output) {
		t.Error("different inputs should produce different outputs in parallel")
	}
}

func TestVDFv2_ParallelEvaluateErrors(t *testing.T) {
	vdf := NewVDFv2(DefaultVDFv2Config())
	if _, err := vdf.ParallelEvaluate(nil, 10); err != errVDFv2NoInputs {
		t.Errorf("nil inputs: want errVDFv2NoInputs, got %v", err)
	}
	if _, err := vdf.ParallelEvaluate([][]byte{}, 10); err != errVDFv2NoInputs {
		t.Errorf("empty inputs: want errVDFv2NoInputs, got %v", err)
	}
	if _, err := vdf.ParallelEvaluate([][]byte{[]byte("test")}, 0); err != errVDFv2ZeroIterations {
		t.Errorf("zero iterations: want errVDFv2ZeroIterations, got %v", err)
	}
	if _, err := vdf.ParallelEvaluate([][]byte{nil}, 10); err == nil {
		t.Error("nil input in list should fail")
	}
}

func TestVDFv2_ParallelEvaluateConsistency(t *testing.T) {
	// Results from ParallelEvaluate should match sequential Evaluate.
	vdf := NewVDFv2(DefaultVDFv2Config())
	inputs := [][]byte{
		[]byte("consistency A"),
		[]byte("consistency B"),
	}
	iters := uint64(20)

	parallel, err := vdf.ParallelEvaluate(inputs, iters)
	if err != nil {
		t.Fatalf("ParallelEvaluate: %v", err)
	}

	for i, input := range inputs {
		sequential, err := vdf.Evaluate(input, iters)
		if err != nil {
			t.Fatalf("Evaluate(%d): %v", i, err)
		}
		if string(parallel[i].Output) != string(sequential.Output) {
			t.Errorf("input %d: parallel and sequential outputs differ", i)
		}
		if string(parallel[i].Proof) != string(sequential.Proof) {
			t.Errorf("input %d: parallel and sequential proofs differ", i)
		}
	}
}

func TestVDFv2_EstimateTime(t *testing.T) {
	vdf := NewVDFv2(DefaultVDFv2Config())

	// Zero iterations.
	if est := vdf.EstimateTime(0); est != 0 {
		t.Errorf("zero iterations should return 0, got %v", est)
	}

	// Non-zero iterations should return a positive duration.
	est := vdf.EstimateTime(1000)
	if est <= 0 {
		t.Errorf("expected positive duration, got %v", est)
	}

	// More iterations should take longer (or equal).
	est2 := vdf.EstimateTime(10000)
	if est2 < est {
		t.Errorf("more iterations should take longer: %v < %v", est2, est)
	}
}

func TestVDFv2_SingleIteration(t *testing.T) {
	vdf := NewVDFv2(DefaultVDFv2Config())
	input := []byte("single iteration")

	result, err := vdf.Evaluate(input, 1)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if !vdf.Verify(result) {
		t.Fatal("single iteration result failed verification")
	}
}

func TestVDFv2_AggregateAndVerifyEndToEnd(t *testing.T) {
	vdf := NewVDFv2(VDFv2Config{
		SecurityLevel: 192,
		Parallelism:   3,
		ProofSize:     32,
	})

	// Parallel evaluate, then aggregate, then verify.
	inputs := [][]byte{
		[]byte("end-to-end 1"),
		[]byte("end-to-end 2"),
		[]byte("end-to-end 3"),
	}

	results, err := vdf.ParallelEvaluate(inputs, 10)
	if err != nil {
		t.Fatalf("ParallelEvaluate: %v", err)
	}

	agg, err := vdf.AggregateProofs(results)
	if err != nil {
		t.Fatalf("AggregateProofs: %v", err)
	}

	if !vdf.VerifyAggregated(agg) {
		t.Fatal("end-to-end aggregated verification failed")
	}
}

func TestVDFv2_ConcurrentEvaluateAndVerify(t *testing.T) {
	vdf := NewVDFv2(DefaultVDFv2Config())

	const goroutines = 8
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			input := []byte{byte(idx), 0xAB, 0xCD}
			result, err := vdf.Evaluate(input, 15)
			if err != nil {
				errs <- err
				return
			}
			if !vdf.Verify(result) {
				errs <- errVDFv2NilResult // reuse as generic "failed" error
				return
			}
			errs <- nil
		}(i)
	}

	for i := 0; i < goroutines; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("goroutine error: %v", err)
		}
	}
}

func TestIsqrt(t *testing.T) {
	tests := []struct {
		input uint64
		want  uint64
	}{
		{0, 0},
		{1, 1},
		{4, 2},
		{9, 3},
		{10, 3},
		{16, 4},
		{100, 10},
		{1000000, 1000},
	}

	for _, tt := range tests {
		got := isqrt(tt.input)
		if got != tt.want {
			t.Errorf("isqrt(%d): want %d, got %d", tt.input, tt.want, got)
		}
	}
}

func TestBytesEqual(t *testing.T) {
	a, b, c := []byte{1, 2, 3}, []byte{1, 2, 3}, []byte{1, 2, 4}
	if !bytesEqual(a, b) {
		t.Error("identical slices should be equal")
	}
	if bytesEqual(a, c) {
		t.Error("different slices should not be equal")
	}
	if bytesEqual(a, []byte{1, 2}) {
		t.Error("different length slices should not be equal")
	}
	if !bytesEqual(nil, nil) {
		t.Error("two nils should be equal")
	}
}

func TestCopyBytes(t *testing.T) {
	orig := []byte{1, 2, 3, 4, 5}
	c := copyBytes(orig)
	if string(orig) != string(c) {
		t.Fatal("copy should match original")
	}
	c[0] = 0xff
	if orig[0] == 0xff {
		t.Fatal("copy should be independent of original")
	}
}

func TestHashN(t *testing.T) {
	data := []byte("hashN test")
	h1 := hashN(data, 1)
	if string(h1) != string(Keccak256(data)) {
		t.Fatal("hashN(1) should equal Keccak256")
	}
	h2 := hashN(data, 2)
	if string(h2) != string(Keccak256(Keccak256(data))) {
		t.Fatal("hashN(2) should be double Keccak256")
	}
	if string(h1) == string(h2) {
		t.Fatal("different round counts should produce different results")
	}
}

func TestVDFv2_EvaluateDuration(t *testing.T) {
	vdf := NewVDFv2(DefaultVDFv2Config())

	r, err := vdf.Evaluate([]byte("duration test"), 100)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if r.Duration <= 0 {
		t.Error("Duration should be positive for non-trivial iteration count")
	}

	// Duration should be of type time.Duration (compile-time check).
	var _ time.Duration = r.Duration
}
