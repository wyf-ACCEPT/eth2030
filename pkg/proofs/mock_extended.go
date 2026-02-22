// mock_extended.go adds configurable mock proof generation and verification,
// latency simulation, and multi-proof-type coverage for testing.
package proofs

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// MockConfig allows fine-grained control over mock proof behavior.
type MockConfig struct {
	// AlwaysValid makes Verify always return true when set.
	AlwaysValid bool
	// FailTypes causes verification to fail for these proof types.
	FailTypes map[ProofType]bool
	// SimulatedLatency is the artificial delay added to Aggregate/Verify.
	SimulatedLatency time.Duration
	// MaxProofs limits how many proofs can be aggregated. 0 = unlimited.
	MaxProofs int
}

// DefaultMockConfig returns a config where all proofs are valid with no latency.
func DefaultMockConfig() MockConfig {
	return MockConfig{
		AlwaysValid: true,
		FailTypes:   make(map[ProofType]bool),
	}
}

// ConfigurableMockAggregator is a mock proof aggregator with configurable
// behavior for testing proof systems and 3-of-5 verification flows.
type ConfigurableMockAggregator struct {
	mu     sync.Mutex
	config MockConfig

	// Metrics.
	aggregateCount int
	verifyCount    int
	failCount      int
}

// NewConfigurableMockAggregator creates a new configurable mock aggregator.
func NewConfigurableMockAggregator(config MockConfig) *ConfigurableMockAggregator {
	return &ConfigurableMockAggregator{config: config}
}

// Aggregate creates an aggregated proof, optionally simulating latency.
func (m *ConfigurableMockAggregator) Aggregate(proofs []ExecutionProof) (*AggregatedProof, error) {
	m.mu.Lock()
	m.aggregateCount++
	cfg := m.config
	m.mu.Unlock()

	if len(proofs) == 0 {
		return nil, ErrNoProofs
	}
	if cfg.MaxProofs > 0 && len(proofs) > cfg.MaxProofs {
		return nil, errors.New("mock: too many proofs")
	}

	if cfg.SimulatedLatency > 0 {
		time.Sleep(cfg.SimulatedLatency)
	}

	root := computeMockRoot(proofs)

	return &AggregatedProof{
		Proofs:        proofs,
		AggregateRoot: root,
		Valid:         true,
	}, nil
}

// Verify checks the aggregated proof, respecting configuration for
// per-type failures and latency simulation.
func (m *ConfigurableMockAggregator) Verify(proof *AggregatedProof) (bool, error) {
	m.mu.Lock()
	m.verifyCount++
	cfg := m.config
	m.mu.Unlock()

	if proof == nil {
		return false, ErrNilProof
	}

	if cfg.SimulatedLatency > 0 {
		time.Sleep(cfg.SimulatedLatency)
	}

	// Check if any proof type is configured to fail.
	for _, p := range proof.Proofs {
		if cfg.FailTypes[p.Type] {
			m.mu.Lock()
			m.failCount++
			m.mu.Unlock()
			return false, nil
		}
	}

	if cfg.AlwaysValid {
		return true, nil
	}

	// Recompute and verify the root.
	expected := computeMockRoot(proof.Proofs)
	valid := expected == proof.AggregateRoot
	if !valid {
		m.mu.Lock()
		m.failCount++
		m.mu.Unlock()
	}
	return valid, nil
}

// Stats returns aggregation and verification counts.
func (m *ConfigurableMockAggregator) Stats() (aggregated, verified, failed int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.aggregateCount, m.verifyCount, m.failCount
}

// SetConfig updates the mock configuration.
func (m *ConfigurableMockAggregator) SetConfig(cfg MockConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = cfg
}

// computeMockRoot derives a deterministic root from proof data.
func computeMockRoot(proofs []ExecutionProof) types.Hash {
	h := sha256.New()
	for _, p := range proofs {
		h.Write(p.StateRoot[:])
		h.Write(p.BlockHash[:])
		h.Write(p.ProofData)
		h.Write([]byte(p.ProverID))
		var tb [1]byte
		tb[0] = byte(p.Type)
		h.Write(tb[:])
	}
	var root types.Hash
	copy(root[:], h.Sum(nil))
	return root
}

// TypeCoverageMock generates proof sets covering all proof types for testing
// the mandatory 3-of-5 verification. Returns 5 proofs, one per type.
func TypeCoverageMock(blockHash types.Hash) []ExecutionProof {
	allTypes := []ProofType{ZKSNARK, ZKSTARK, IPA, KZG}
	proofs := make([]ExecutionProof, 0, len(allTypes)+1)
	for i, pt := range allTypes {
		var data [4]byte
		binary.LittleEndian.PutUint32(data[:], uint32(i))
		proofs = append(proofs, ExecutionProof{
			StateRoot: blockHash,
			BlockHash: blockHash,
			ProofData: data[:],
			ProverID:  pt.String(),
			Type:      pt,
		})
	}
	// Add a fifth proof (duplicate type but different prover).
	var data [4]byte
	binary.LittleEndian.PutUint32(data[:], uint32(len(allTypes)))
	proofs = append(proofs, ExecutionProof{
		StateRoot: blockHash,
		BlockHash: blockHash,
		ProofData: data[:],
		ProverID:  "extra-prover",
		Type:      ZKSNARK,
	})
	return proofs
}

// SequentialMockVerifier verifies proofs sequentially, returning the count
// of valid and invalid proofs. Useful for testing 3-of-5 thresholds.
func SequentialMockVerifier(agg ProofAggregator, proofs []ExecutionProof) (valid, invalid int) {
	for _, p := range proofs {
		ap := &AggregatedProof{
			Proofs:        []ExecutionProof{p},
			AggregateRoot: computeMockRoot([]ExecutionProof{p}),
			Valid:         true,
		}
		ok, err := agg.Verify(ap)
		if err == nil && ok {
			valid++
		} else {
			invalid++
		}
	}
	return
}
