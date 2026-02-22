package proofs

import "github.com/eth2030/eth2030/core/types"

// MockAggregator is a test aggregator that always returns valid results.
type MockAggregator struct {
	// AggregateRoot is used as the fixed aggregate root if non-zero.
	AggregateRoot types.Hash
}

// NewMockAggregator creates a new MockAggregator.
func NewMockAggregator() *MockAggregator {
	return &MockAggregator{}
}

// Aggregate returns a proof that is always valid.
func (m *MockAggregator) Aggregate(proofs []ExecutionProof) (*AggregatedProof, error) {
	if len(proofs) == 0 {
		return nil, ErrNoProofs
	}

	root := m.AggregateRoot
	if root == (types.Hash{}) {
		// Use a deterministic mock root based on proof count.
		root[0] = byte(len(proofs))
		root[31] = 0xff
	}

	return &AggregatedProof{
		Proofs:        proofs,
		AggregateRoot: root,
		Valid:         true,
	}, nil
}

// Verify always returns true for MockAggregator.
func (m *MockAggregator) Verify(proof *AggregatedProof) (bool, error) {
	if proof == nil {
		return false, ErrNilProof
	}
	return true, nil
}
