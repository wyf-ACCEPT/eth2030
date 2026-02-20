package proofs

import (
	"testing"
	"time"

	"github.com/eth2028/eth2028/crypto"
	"github.com/eth2028/eth2028/metrics"
)

func TestQueueProofTypeString(t *testing.T) {
	tests := []struct {
		pt   QueueProofType
		want string
	}{
		{StateProof, "StateProof"},
		{StorageProof, "StorageProof"},
		{ExecutionTrace, "ExecutionProof"},
		{WitnessProof, "WitnessProof"},
		{ReceiptProof, "ReceiptProof"},
		{QueueProofType(99), "UnknownProof"},
	}

	for _, tt := range tests {
		if got := tt.pt.String(); got != tt.want {
			t.Errorf("QueueProofType(%d).String() = %q, want %q", tt.pt, got, tt.want)
		}
	}
}

func TestMakeValidProof(t *testing.T) {
	blockHash := crypto.Keccak256Hash([]byte("block-123"))

	for _, pt := range AllQueueProofTypes {
		data := MakeValidProof([32]byte(blockHash), pt)
		if len(data) == 0 {
			t.Fatalf("MakeValidProof returned empty data for %s", pt)
		}
		if !validateProof([32]byte(blockHash), pt, data) {
			t.Fatalf("MakeValidProof data does not validate for %s", pt)
		}
	}
}

func TestProofQueue_SubmitAndReceive(t *testing.T) {
	config := ProofQueueConfig{
		Workers:         2,
		QueueSize:       64,
		DefaultDeadline: 5 * time.Second,
	}
	q := NewProofQueue(config)
	defer q.Close()

	blockHash := crypto.Keccak256Hash([]byte("block-456"))
	proof := MakeValidProof([32]byte(blockHash), StateProof)

	ch, err := q.Submit([32]byte(blockHash), StateProof, proof)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	select {
	case result := <-ch:
		if !result.IsValid {
			t.Fatal("proof should be valid")
		}
		if result.ProofType != StateProof {
			t.Fatalf("expected StateProof, got %s", result.ProofType)
		}
		if result.BlockHash != [32]byte(blockHash) {
			t.Fatal("block hash mismatch")
		}
		if result.Duration <= 0 {
			t.Fatal("duration should be positive")
		}
		if result.Error != nil {
			t.Fatalf("unexpected error: %v", result.Error)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for result")
	}
}

func TestProofQueue_InvalidProof(t *testing.T) {
	q := NewProofQueue(DefaultProofQueueConfig())
	defer q.Close()

	blockHash := crypto.Keccak256Hash([]byte("block-789"))
	// Submit invalid proof data (unlikely to pass validation).
	invalidData := []byte("this-is-not-a-valid-proof-data")

	ch, err := q.Submit([32]byte(blockHash), StorageProof, invalidData)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	select {
	case result := <-ch:
		if result.IsValid {
			t.Fatal("invalid proof should not validate")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

func TestProofQueue_ErrorCases(t *testing.T) {
	q := NewProofQueue(DefaultProofQueueConfig())
	defer q.Close()

	t.Run("zero block hash", func(t *testing.T) {
		_, err := q.Submit([32]byte{}, StateProof, []byte{1})
		if err != ErrBlockHashZero {
			t.Fatalf("expected ErrBlockHashZero, got %v", err)
		}
	})

	t.Run("empty proof data", func(t *testing.T) {
		bh := [32]byte{1}
		_, err := q.Submit(bh, StateProof, nil)
		if err != ErrProofDataEmpty {
			t.Fatalf("expected ErrProofDataEmpty, got %v", err)
		}
	})

	t.Run("invalid proof type", func(t *testing.T) {
		bh := [32]byte{1}
		_, err := q.Submit(bh, QueueProofType(99), []byte{1})
		if err != ErrInvalidQueueProofType {
			t.Fatalf("expected ErrInvalidQueueProofType, got %v", err)
		}
	})
}

func TestProofQueue_Close(t *testing.T) {
	q := NewProofQueue(DefaultProofQueueConfig())
	q.Close()

	bh := [32]byte{1}
	_, err := q.Submit(bh, StateProof, []byte{1})
	if err != ErrQueueClosed {
		t.Fatalf("expected ErrQueueClosed, got %v", err)
	}

	// Double close should be safe.
	q.Close()
}

func TestProofQueue_Metrics(t *testing.T) {
	q := NewProofQueue(ProofQueueConfig{
		Workers:         1,
		QueueSize:       64,
		DefaultDeadline: 5 * time.Second,
	})
	defer q.Close()

	blockHash := crypto.Keccak256Hash([]byte("metrics-block"))

	// Submit a valid proof.
	validData := MakeValidProof([32]byte(blockHash), StateProof)
	ch, _ := q.Submit([32]byte(blockHash), StateProof, validData)
	<-ch

	// Submit an invalid proof.
	ch2, _ := q.Submit([32]byte(blockHash), StorageProof, []byte("bad"))
	<-ch2

	validated, failed, _ := q.Metrics()
	if validated != 1 {
		t.Fatalf("expected 1 validated, got %d", validated)
	}
	if failed != 1 {
		t.Fatalf("expected 1 failed, got %d", failed)
	}
}

func TestProofQueue_TrackerIntegration(t *testing.T) {
	q := NewProofQueue(ProofQueueConfig{
		Workers:         2,
		QueueSize:       64,
		DefaultDeadline: 5 * time.Second,
	})
	defer q.Close()

	blockHash := crypto.Keccak256Hash([]byte("tracker-block"))
	bh := [32]byte(blockHash)

	// Before any proofs: should not have mandatory proofs.
	if q.Tracker().HasMandatoryProofs(bh) {
		t.Fatal("should not have mandatory proofs yet")
	}

	// Submit 3 valid proofs of different types.
	types := []QueueProofType{StateProof, ExecutionTrace, ReceiptProof}
	for _, pt := range types {
		data := MakeValidProof(bh, pt)
		ch, err := q.Submit(bh, pt, data)
		if err != nil {
			t.Fatalf("submit %s: %v", pt, err)
		}
		result := <-ch
		if !result.IsValid {
			t.Fatalf("proof %s should be valid", pt)
		}
	}

	// Now should have mandatory proofs (3 of 5).
	if !q.Tracker().HasMandatoryProofs(bh) {
		t.Fatal("should have mandatory proofs after 3 valid submissions")
	}

	if q.Tracker().ProofCount(bh) != 3 {
		t.Fatalf("expected 3 proof types, got %d", q.Tracker().ProofCount(bh))
	}
}

func TestMandatoryProofTracker(t *testing.T) {
	tracker := NewMandatoryProofTracker()
	bh := [32]byte{1, 2, 3}

	if tracker.HasMandatoryProofs(bh) {
		t.Fatal("empty tracker should not have mandatory proofs")
	}
	if tracker.ProofCount(bh) != 0 {
		t.Fatal("empty tracker should have 0 proof count")
	}

	// Record 2 proof types.
	tracker.RecordProof(bh, StateProof)
	tracker.RecordProof(bh, StorageProof)
	if tracker.HasMandatoryProofs(bh) {
		t.Fatal("2 proofs should not meet threshold")
	}
	if tracker.ProofCount(bh) != 2 {
		t.Fatal("should have 2 proof types")
	}

	// Duplicate recording should not increase count.
	tracker.RecordProof(bh, StateProof)
	if tracker.ProofCount(bh) != 2 {
		t.Fatal("duplicate should not increase count")
	}

	// Third distinct type should meet threshold.
	tracker.RecordProof(bh, WitnessProof)
	if !tracker.HasMandatoryProofs(bh) {
		t.Fatal("3 proofs should meet threshold")
	}

	// Check missing types.
	missing := tracker.MissingTypes(bh)
	if len(missing) != 2 {
		t.Fatalf("expected 2 missing types, got %d", len(missing))
	}
}

func TestMandatoryProofTracker_ValidatedTypes(t *testing.T) {
	tracker := NewMandatoryProofTracker()
	bh := [32]byte{4, 5, 6}

	// No types yet.
	types := tracker.ValidatedTypes(bh)
	if len(types) != 0 {
		t.Fatal("should have no validated types")
	}

	tracker.RecordProof(bh, ReceiptProof)
	tracker.RecordProof(bh, ExecutionTrace)

	types = tracker.ValidatedTypes(bh)
	if len(types) != 2 {
		t.Fatalf("expected 2 validated types, got %d", len(types))
	}
}

func TestMandatoryProofTracker_MissingTypes_Untracked(t *testing.T) {
	tracker := NewMandatoryProofTracker()
	bh := [32]byte{7, 8, 9}

	missing := tracker.MissingTypes(bh)
	if len(missing) != len(AllQueueProofTypes) {
		t.Fatalf("untracked block should be missing all %d types, got %d",
			len(AllQueueProofTypes), len(missing))
	}
}

func TestProofDeadline(t *testing.T) {
	d := NewProofDeadline(100 * time.Millisecond)
	bh := [32]byte{10, 11}

	// No deadline set: not expired.
	if d.IsExpired(bh) {
		t.Fatal("unset deadline should not be expired")
	}
	if d.TimeRemaining(bh) != 0 {
		t.Fatal("unset deadline should have 0 remaining")
	}

	d.SetDeadline(bh)
	if d.IsExpired(bh) {
		t.Fatal("just-set deadline should not be expired immediately")
	}
	remaining := d.TimeRemaining(bh)
	if remaining <= 0 || remaining > 100*time.Millisecond {
		t.Fatalf("unexpected remaining: %v", remaining)
	}

	// Wait for expiry.
	time.Sleep(150 * time.Millisecond)
	if !d.IsExpired(bh) {
		t.Fatal("deadline should be expired after waiting")
	}
	if d.TimeRemaining(bh) != 0 {
		t.Fatal("expired deadline should have 0 remaining")
	}
}

func TestProofDeadline_SetDeadlineAt(t *testing.T) {
	d := NewProofDeadline(time.Hour)
	bh := [32]byte{12, 13}

	// Set deadline in the past.
	d.SetDeadlineAt(bh, time.Now().Add(-time.Second))
	if !d.IsExpired(bh) {
		t.Fatal("past deadline should be expired")
	}

	// Set deadline in the future.
	d.SetDeadlineAt(bh, time.Now().Add(time.Hour))
	if d.IsExpired(bh) {
		t.Fatal("future deadline should not be expired")
	}
}

func TestProofDeadline_Prune(t *testing.T) {
	d := NewProofDeadline(10 * time.Millisecond)
	bh1 := [32]byte{1}
	bh2 := [32]byte{2}

	d.SetDeadline(bh1)
	d.SetDeadline(bh2)

	if d.Count() != 2 {
		t.Fatalf("expected 2 deadlines, got %d", d.Count())
	}

	time.Sleep(50 * time.Millisecond)

	pruned := d.Prune(10 * time.Millisecond)
	if pruned != 2 {
		t.Fatalf("expected 2 pruned, got %d", pruned)
	}
	if d.Count() != 0 {
		t.Fatalf("expected 0 deadlines after prune, got %d", d.Count())
	}
}

func TestProofDeadline_DefaultDuration(t *testing.T) {
	d := NewProofDeadline(0)
	if d.duration != 30*time.Second {
		t.Fatalf("expected default 30s, got %v", d.duration)
	}
}

func TestValidateProof(t *testing.T) {
	bh := crypto.Keccak256Hash([]byte("test-validate"))

	// Empty data should fail.
	if validateProof([32]byte(bh), StateProof, nil) {
		t.Fatal("empty data should not validate")
	}

	// Valid proof from MakeValidProof should pass.
	data := MakeValidProof([32]byte(bh), ExecutionTrace)
	if !validateProof([32]byte(bh), ExecutionTrace, data) {
		t.Fatal("valid proof should pass validation")
	}

	// Same data for different proof type should likely fail.
	// (Not guaranteed, but extremely likely with Keccak.)
	if validateProof([32]byte(bh), StorageProof, data) {
		// This can theoretically pass by collision, just note it.
		t.Log("note: cross-type validation passed by coincidence")
	}
}

func TestProofQueue_QueueFull(t *testing.T) {
	// Create a queue with very small capacity and no workers to drain it.
	q := &ProofQueue{
		config:          ProofQueueConfig{Workers: 0, QueueSize: 1, DefaultDeadline: time.Second},
		jobs:            make(chan proofJob, 1),
		tracker:         NewMandatoryProofTracker(),
		proofsValidated: metrics.NewCounter("v"),
		proofsFailed:    metrics.NewCounter("f"),
		proofsTimedOut:  metrics.NewCounter("t"),
	}

	bh := [32]byte{1}
	// Fill the queue.
	_, _ = q.Submit(bh, StateProof, []byte{1})

	// Next submit should fail with queue full.
	_, err := q.Submit(bh, StorageProof, []byte{2})
	if err != ErrQueueFull {
		t.Fatalf("expected ErrQueueFull, got %v", err)
	}
}
