package proofs

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func makeTestProofs(n int) []ExecutionProof {
	proofs := make([]ExecutionProof, n)
	for i := 0; i < n; i++ {
		proofs[i] = ExecutionProof{
			StateRoot: types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
			BlockHash: types.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
			ProofData: []byte{byte(i), 0xab, 0xcd},
			ProverID:  "test-prover",
			Type:      ZKSNARK,
		}
		proofs[i].StateRoot[0] = byte(i)
	}
	return proofs
}

func TestProofTypeString(t *testing.T) {
	tests := []struct {
		pt   ProofType
		want string
	}{
		{ZKSNARK, "ZK-SNARK"},
		{ZKSTARK, "ZK-STARK"},
		{IPA, "IPA"},
		{KZG, "KZG"},
		{ProofType(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.pt.String(); got != tt.want {
			t.Errorf("ProofType(%d).String() = %q, want %q", tt.pt, got, tt.want)
		}
	}
}

func TestSimpleAggregatorAggregate(t *testing.T) {
	agg := NewSimpleAggregator()
	proofs := makeTestProofs(3)

	result, err := agg.Aggregate(proofs)
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if !result.Valid {
		t.Fatal("expected valid result")
	}
	if result.AggregateRoot == (types.Hash{}) {
		t.Fatal("expected non-zero aggregate root")
	}
	if len(result.Proofs) != 3 {
		t.Fatalf("expected 3 proofs, got %d", len(result.Proofs))
	}
}

func TestSimpleAggregatorVerify(t *testing.T) {
	agg := NewSimpleAggregator()
	proofs := makeTestProofs(3)

	result, err := agg.Aggregate(proofs)
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}

	valid, err := agg.Verify(result)
	if err != nil {
		t.Fatalf("Verify error: %v", err)
	}
	if !valid {
		t.Fatal("expected valid verification")
	}
}

func TestSimpleAggregatorVerifyTampered(t *testing.T) {
	agg := NewSimpleAggregator()
	proofs := makeTestProofs(3)

	result, err := agg.Aggregate(proofs)
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}

	// Tamper with the aggregate root.
	result.AggregateRoot[0] ^= 0xff

	valid, err := agg.Verify(result)
	if err != nil {
		t.Fatalf("Verify error: %v", err)
	}
	if valid {
		t.Fatal("expected invalid verification for tampered proof")
	}
}

func TestSimpleAggregatorNoProofs(t *testing.T) {
	agg := NewSimpleAggregator()

	_, err := agg.Aggregate(nil)
	if err != ErrNoProofs {
		t.Fatalf("expected ErrNoProofs, got %v", err)
	}

	_, err = agg.Aggregate([]ExecutionProof{})
	if err != ErrNoProofs {
		t.Fatalf("expected ErrNoProofs, got %v", err)
	}
}

func TestSimpleAggregatorVerifyNil(t *testing.T) {
	agg := NewSimpleAggregator()

	_, err := agg.Verify(nil)
	if err != ErrNilProof {
		t.Fatalf("expected ErrNilProof, got %v", err)
	}
}

func TestSimpleAggregatorVerifyEmpty(t *testing.T) {
	agg := NewSimpleAggregator()

	_, err := agg.Verify(&AggregatedProof{})
	if err != ErrNoProofs {
		t.Fatalf("expected ErrNoProofs, got %v", err)
	}
}

func TestSimpleAggregatorDeterministic(t *testing.T) {
	agg := NewSimpleAggregator()
	proofs := makeTestProofs(5)

	r1, _ := agg.Aggregate(proofs)
	r2, _ := agg.Aggregate(proofs)

	if r1.AggregateRoot != r2.AggregateRoot {
		t.Fatal("aggregate root should be deterministic")
	}
}

func TestSimpleAggregatorSingleProof(t *testing.T) {
	agg := NewSimpleAggregator()
	proofs := makeTestProofs(1)

	result, err := agg.Aggregate(proofs)
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}

	valid, err := agg.Verify(result)
	if err != nil {
		t.Fatalf("Verify error: %v", err)
	}
	if !valid {
		t.Fatal("expected valid verification for single proof")
	}
}

func TestMockAggregator(t *testing.T) {
	mock := NewMockAggregator()
	proofs := makeTestProofs(2)

	result, err := mock.Aggregate(proofs)
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	if !result.Valid {
		t.Fatal("expected valid result")
	}
	if result.AggregateRoot == (types.Hash{}) {
		t.Fatal("expected non-zero mock root")
	}

	valid, err := mock.Verify(result)
	if err != nil {
		t.Fatalf("Verify error: %v", err)
	}
	if !valid {
		t.Fatal("mock should always verify true")
	}
}

func TestMockAggregatorNoProofs(t *testing.T) {
	mock := NewMockAggregator()

	_, err := mock.Aggregate(nil)
	if err != ErrNoProofs {
		t.Fatalf("expected ErrNoProofs, got %v", err)
	}
}

func TestMockAggregatorVerifyNil(t *testing.T) {
	mock := NewMockAggregator()

	_, err := mock.Verify(nil)
	if err != ErrNilProof {
		t.Fatalf("expected ErrNilProof, got %v", err)
	}
}

func TestMockAggregatorCustomRoot(t *testing.T) {
	mock := &MockAggregator{
		AggregateRoot: types.HexToHash("0xdeadbeef"),
	}
	proofs := makeTestProofs(1)

	result, err := mock.Aggregate(proofs)
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	if result.AggregateRoot != types.HexToHash("0xdeadbeef") {
		t.Fatal("expected custom root")
	}
}

func TestProverRegistry(t *testing.T) {
	reg := NewProverRegistry()

	simple := NewSimpleAggregator()
	mock := NewMockAggregator()

	if err := reg.Register("simple", simple); err != nil {
		t.Fatalf("Register error: %v", err)
	}
	if err := reg.Register("mock", mock); err != nil {
		t.Fatalf("Register error: %v", err)
	}

	// Duplicate registration.
	if err := reg.Register("simple", simple); err != ErrAggregatorExists {
		t.Fatalf("expected ErrAggregatorExists, got %v", err)
	}

	// Get existing.
	got, err := reg.Get("simple")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil aggregator")
	}

	// Get missing.
	_, err = reg.Get("nonexistent")
	if err != ErrAggregatorNotFound {
		t.Fatalf("expected ErrAggregatorNotFound, got %v", err)
	}

	// Names.
	names := reg.Names()
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}
}

func TestProverRegistryNames(t *testing.T) {
	reg := NewProverRegistry()
	names := reg.Names()
	if len(names) != 0 {
		t.Fatalf("expected 0 names for empty registry, got %d", len(names))
	}
}

// Test that ProofAggregator interface is satisfied.
var _ ProofAggregator = (*SimpleAggregator)(nil)
var _ ProofAggregator = (*MockAggregator)(nil)
