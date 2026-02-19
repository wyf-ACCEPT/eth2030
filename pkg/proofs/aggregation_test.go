package proofs

import (
	"testing"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

func TestDefaultAggregationConfig(t *testing.T) {
	cfg := DefaultAggregationConfig()

	if cfg.MaxProofs != 64 {
		t.Errorf("MaxProofs: got %d, want 64", cfg.MaxProofs)
	}
	if cfg.VerificationTimeout != 5*time.Second {
		t.Errorf("VerificationTimeout: got %v, want 5s", cfg.VerificationTimeout)
	}
	if !cfg.ParallelVerify {
		t.Error("ParallelVerify should default to true")
	}
}

func TestBatchAggregatorAddProof(t *testing.T) {
	ba := NewBatchAggregator(DefaultAggregationConfig(), nil)

	p := ExecutionProof{
		StateRoot: types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
		BlockHash: types.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
		ProofData: []byte{0x01, 0x02},
		ProverID:  "test",
		Type:      ZKSNARK,
	}

	err := ba.AddProof(p)
	if err != nil {
		t.Fatalf("AddProof error: %v", err)
	}
}

func TestBatchAggregatorAddProofFull(t *testing.T) {
	cfg := DefaultAggregationConfig()
	cfg.MaxProofs = 2
	ba := NewBatchAggregator(cfg, nil)

	p := ExecutionProof{
		StateRoot: types.Hash{0x01},
		BlockHash: types.Hash{0x02},
		ProofData: []byte{0x03},
		ProverID:  "test",
		Type:      ZKSNARK,
	}

	ba.AddProof(p)
	ba.AddProof(p)

	err := ba.AddProof(p)
	if err != ErrBatchFull {
		t.Errorf("expected ErrBatchFull, got %v", err)
	}
}

func TestBatchAggregatorAggregateBatch(t *testing.T) {
	ba := NewBatchAggregator(DefaultAggregationConfig(), nil)

	proofs := makeTestProofs(3)
	for _, p := range proofs {
		ba.AddProof(p)
	}

	batch, err := ba.AggregateBatch()
	if err != nil {
		t.Fatalf("AggregateBatch error: %v", err)
	}
	if batch == nil {
		t.Fatal("batch is nil")
	}
	if len(batch.Proofs) != 3 {
		t.Errorf("expected 3 proofs, got %d", len(batch.Proofs))
	}
	if batch.AggregateHash.IsZero() {
		t.Error("expected non-zero AggregateHash")
	}
	if batch.Verified {
		t.Error("batch should not be verified before VerifyBatch call")
	}
}

func TestBatchAggregatorAggregateBatchEmpty(t *testing.T) {
	ba := NewBatchAggregator(DefaultAggregationConfig(), nil)

	_, err := ba.AggregateBatch()
	if err != ErrBatchEmpty {
		t.Errorf("expected ErrBatchEmpty, got %v", err)
	}
}

func TestBatchAggregatorVerifyBatch(t *testing.T) {
	ba := NewBatchAggregator(DefaultAggregationConfig(), NewSimpleAggregator())

	proofs := makeTestProofs(4)
	for _, p := range proofs {
		ba.AddProof(p)
	}

	batch, _ := ba.AggregateBatch()

	valid, err := ba.VerifyBatch(batch)
	if err != nil {
		t.Fatalf("VerifyBatch error: %v", err)
	}
	if !valid {
		t.Error("expected valid batch")
	}
	if !batch.Verified {
		t.Error("expected Verified=true after successful VerifyBatch")
	}
	if batch.VerifiedAt.IsZero() {
		t.Error("expected non-zero VerifiedAt")
	}
}

func TestBatchAggregatorVerifyBatchTampered(t *testing.T) {
	ba := NewBatchAggregator(DefaultAggregationConfig(), NewSimpleAggregator())

	proofs := makeTestProofs(3)
	for _, p := range proofs {
		ba.AddProof(p)
	}

	batch, _ := ba.AggregateBatch()

	// Tamper with the aggregate hash.
	batch.AggregateHash[0] ^= 0xff

	valid, err := ba.VerifyBatch(batch)
	if err != nil {
		t.Fatalf("VerifyBatch error: %v", err)
	}
	if valid {
		t.Error("expected tampered batch to fail verification")
	}
}

func TestBatchAggregatorVerifyBatchNil(t *testing.T) {
	ba := NewBatchAggregator(DefaultAggregationConfig(), nil)

	_, err := ba.VerifyBatch(nil)
	if err != ErrBatchEmpty {
		t.Errorf("expected ErrBatchEmpty for nil batch, got %v", err)
	}
}

func TestBatchAggregatorStats(t *testing.T) {
	ba := NewBatchAggregator(DefaultAggregationConfig(), NewSimpleAggregator())

	// Initial stats should be zero.
	batched, verified, failed := ba.Stats()
	if batched != 0 || verified != 0 || failed != 0 {
		t.Errorf("initial stats: batched=%d verified=%d failed=%d", batched, verified, failed)
	}

	// Add and aggregate some proofs.
	proofs := makeTestProofs(5)
	for _, p := range proofs {
		ba.AddProof(p)
	}
	batch, _ := ba.AggregateBatch()

	batched, _, _ = ba.Stats()
	if batched != 5 {
		t.Errorf("expected 5 batched, got %d", batched)
	}

	// Verify the batch.
	ba.VerifyBatch(batch)

	_, verified, _ = ba.Stats()
	if verified != 5 {
		t.Errorf("expected 5 verified, got %d", verified)
	}

	// Create a tampered batch to increment failed count.
	proofs2 := makeTestProofs(3)
	for _, p := range proofs2 {
		ba.AddProof(p)
	}
	batch2, _ := ba.AggregateBatch()
	batch2.AggregateHash[0] ^= 0xff
	ba.VerifyBatch(batch2)

	_, _, failed = ba.Stats()
	if failed != 3 {
		t.Errorf("expected 3 failed, got %d", failed)
	}
}

func TestBatchAggregatorDeterministicHash(t *testing.T) {
	ba1 := NewBatchAggregator(DefaultAggregationConfig(), nil)
	ba2 := NewBatchAggregator(DefaultAggregationConfig(), nil)

	proofs := makeTestProofs(4)
	for _, p := range proofs {
		ba1.AddProof(p)
		ba2.AddProof(p)
	}

	batch1, _ := ba1.AggregateBatch()
	batch2, _ := ba2.AggregateBatch()

	if batch1.AggregateHash != batch2.AggregateHash {
		t.Error("expected deterministic AggregateHash for same proofs")
	}
}

func TestBatchAggregatorClearsPending(t *testing.T) {
	ba := NewBatchAggregator(DefaultAggregationConfig(), nil)

	proofs := makeTestProofs(3)
	for _, p := range proofs {
		ba.AddProof(p)
	}

	ba.AggregateBatch()

	// Second aggregate should fail since pending is cleared.
	_, err := ba.AggregateBatch()
	if err != ErrBatchEmpty {
		t.Errorf("expected ErrBatchEmpty after clearing, got %v", err)
	}
}
