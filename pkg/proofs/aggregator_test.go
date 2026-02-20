package proofs

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestNewSimpleAggregator(t *testing.T) {
	agg := NewSimpleAggregator()
	if agg == nil {
		t.Fatal("NewSimpleAggregator returned nil")
	}
}

func TestSimpleAggregatorInterface(t *testing.T) {
	// Verify SimpleAggregator implements ProofAggregator.
	var _ ProofAggregator = (*SimpleAggregator)(nil)
}

func TestSimpleAggregatorAggregateEmpty(t *testing.T) {
	agg := NewSimpleAggregator()
	_, err := agg.Aggregate(nil)
	if err != ErrNoProofs {
		t.Errorf("Aggregate(nil) error = %v, want ErrNoProofs", err)
	}
	_, err = agg.Aggregate([]ExecutionProof{})
	if err != ErrNoProofs {
		t.Errorf("Aggregate(empty) error = %v, want ErrNoProofs", err)
	}
}

func TestSimpleAggregatorAggregateSingle(t *testing.T) {
	agg := NewSimpleAggregator()
	proofs := []ExecutionProof{
		{
			StateRoot: types.BytesToHash([]byte{0x01}),
			BlockHash: types.BytesToHash([]byte{0x02}),
			ProofData: []byte{0x03, 0x04},
			ProverID:  "prover-1",
			Type:      ZKSNARK,
		},
	}

	result, err := agg.Aggregate(proofs)
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	if result == nil {
		t.Fatal("Aggregate returned nil result")
	}
	if len(result.Proofs) != 1 {
		t.Errorf("Proofs count = %d, want 1", len(result.Proofs))
	}
	if !result.Valid {
		t.Error("expected Valid=true")
	}
	if result.AggregateRoot == (types.Hash{}) {
		t.Error("AggregateRoot should not be zero")
	}
}

func TestSimpleAggregatorAggregateMultiple(t *testing.T) {
	agg := NewSimpleAggregator()
	proofs := []ExecutionProof{
		{
			StateRoot: types.BytesToHash([]byte{0x01}),
			BlockHash: types.BytesToHash([]byte{0x02}),
			ProofData: []byte{0x03},
			ProverID:  "prover-1",
			Type:      ZKSNARK,
		},
		{
			StateRoot: types.BytesToHash([]byte{0x11}),
			BlockHash: types.BytesToHash([]byte{0x12}),
			ProofData: []byte{0x13},
			ProverID:  "prover-2",
			Type:      ZKSTARK,
		},
		{
			StateRoot: types.BytesToHash([]byte{0x21}),
			BlockHash: types.BytesToHash([]byte{0x22}),
			ProofData: []byte{0x23},
			ProverID:  "prover-3",
			Type:      KZG,
		},
	}

	result, err := agg.Aggregate(proofs)
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	if len(result.Proofs) != 3 {
		t.Errorf("Proofs count = %d, want 3", len(result.Proofs))
	}
}

func TestSimpleAggregatorVerifyValid(t *testing.T) {
	agg := NewSimpleAggregator()
	proofs := []ExecutionProof{
		{
			StateRoot: types.BytesToHash([]byte{0x01}),
			BlockHash: types.BytesToHash([]byte{0x02}),
			ProofData: []byte{0x03},
			ProverID:  "prover-1",
			Type:      IPA,
		},
	}

	result, err := agg.Aggregate(proofs)
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}

	valid, err := agg.Verify(result)
	if err != nil {
		t.Fatalf("Verify error: %v", err)
	}
	if !valid {
		t.Error("expected Verify=true for freshly aggregated proof")
	}
}

func TestSimpleAggregatorVerifyNilProof(t *testing.T) {
	agg := NewSimpleAggregator()
	_, err := agg.Verify(nil)
	if err != ErrNilProof {
		t.Errorf("Verify(nil) error = %v, want ErrNilProof", err)
	}
}

func TestSimpleAggregatorVerifyEmptyProofs(t *testing.T) {
	agg := NewSimpleAggregator()
	_, err := agg.Verify(&AggregatedProof{Proofs: []ExecutionProof{}})
	if err != ErrNoProofs {
		t.Errorf("Verify(empty proofs) error = %v, want ErrNoProofs", err)
	}
}

func TestSimpleAggregatorVerifyTamperedRoot(t *testing.T) {
	agg := NewSimpleAggregator()
	proofs := []ExecutionProof{
		{
			StateRoot: types.BytesToHash([]byte{0x01}),
			BlockHash: types.BytesToHash([]byte{0x02}),
			ProofData: []byte{0x03},
			ProverID:  "prover-1",
			Type:      ZKSNARK,
		},
	}

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
		t.Error("expected Verify=false for tampered proof")
	}
}

func TestSimpleAggregatorDeterministicOutput(t *testing.T) {
	agg := NewSimpleAggregator()
	proofs := []ExecutionProof{
		{
			StateRoot: types.BytesToHash([]byte{0x01}),
			BlockHash: types.BytesToHash([]byte{0x02}),
			ProofData: []byte{0x03, 0x04, 0x05},
			ProverID:  "test-prover",
			Type:      ZKSTARK,
		},
	}

	result1, _ := agg.Aggregate(proofs)
	result2, _ := agg.Aggregate(proofs)

	if result1.AggregateRoot != result2.AggregateRoot {
		t.Error("aggregation should be deterministic: roots differ")
	}
}

func TestSimpleAggregatorDifferentInputsDifferentRoots(t *testing.T) {
	agg := NewSimpleAggregator()
	proofs1 := []ExecutionProof{
		{
			StateRoot: types.BytesToHash([]byte{0x01}),
			BlockHash: types.BytesToHash([]byte{0x02}),
			ProofData: []byte{0x03},
			ProverID:  "p1",
			Type:      ZKSNARK,
		},
	}
	proofs2 := []ExecutionProof{
		{
			StateRoot: types.BytesToHash([]byte{0xaa}),
			BlockHash: types.BytesToHash([]byte{0xbb}),
			ProofData: []byte{0xcc},
			ProverID:  "p2",
			Type:      ZKSTARK,
		},
	}

	result1, _ := agg.Aggregate(proofs1)
	result2, _ := agg.Aggregate(proofs2)

	if result1.AggregateRoot == result2.AggregateRoot {
		t.Error("different inputs should produce different aggregate roots")
	}
}

func TestAggregationErrors(t *testing.T) {
	if ErrNoProofs.Error() == "" {
		t.Error("ErrNoProofs should have non-empty message")
	}
	if ErrNilProof.Error() == "" {
		t.Error("ErrNilProof should have non-empty message")
	}
	if ErrProofInvalid.Error() == "" {
		t.Error("ErrProofInvalid should have non-empty message")
	}
}
