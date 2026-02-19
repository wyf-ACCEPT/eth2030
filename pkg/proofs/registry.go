package proofs

import (
	"errors"
	"sync"
)

// Registry errors.
var (
	ErrAggregatorExists   = errors.New("proofs: aggregator already registered")
	ErrAggregatorNotFound = errors.New("proofs: aggregator not found")
)

// ProverRegistry manages named proof aggregators.
type ProverRegistry struct {
	mu          sync.RWMutex
	aggregators map[string]ProofAggregator
}

// NewProverRegistry creates a new empty ProverRegistry.
func NewProverRegistry() *ProverRegistry {
	return &ProverRegistry{
		aggregators: make(map[string]ProofAggregator),
	}
}

// Register adds an aggregator under the given name.
func (r *ProverRegistry) Register(name string, agg ProofAggregator) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.aggregators[name]; exists {
		return ErrAggregatorExists
	}
	r.aggregators[name] = agg
	return nil
}

// Get retrieves an aggregator by name.
func (r *ProverRegistry) Get(name string) (ProofAggregator, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	agg, ok := r.aggregators[name]
	if !ok {
		return nil, ErrAggregatorNotFound
	}
	return agg, nil
}

// Names returns all registered aggregator names.
func (r *ProverRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.aggregators))
	for name := range r.aggregators {
		names = append(names, name)
	}
	return names
}
