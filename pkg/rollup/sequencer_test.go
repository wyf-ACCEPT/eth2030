package rollup

import (
	"sync"
	"testing"
	"time"
)

func TestDefaultSequencerConfig(t *testing.T) {
	cfg := DefaultSequencerConfig()

	if cfg.MaxBatchSize != 1000 {
		t.Errorf("MaxBatchSize: got %d, want 1000", cfg.MaxBatchSize)
	}
	if cfg.BatchTimeout != 2*time.Second {
		t.Errorf("BatchTimeout: got %v, want 2s", cfg.BatchTimeout)
	}
	if cfg.L1SubmissionInterval != 6*time.Second {
		t.Errorf("L1SubmissionInterval: got %v, want 6s", cfg.L1SubmissionInterval)
	}
	if cfg.CompressPayload {
		t.Error("CompressPayload should default to false")
	}
}

func TestAddTransaction(t *testing.T) {
	seq := NewSequencer(DefaultSequencerConfig())

	err := seq.AddTransaction([]byte{0x01, 0x02, 0x03})
	if err != nil {
		t.Fatalf("AddTransaction error: %v", err)
	}

	if seq.PendingCount() != 1 {
		t.Errorf("PendingCount: got %d, want 1", seq.PendingCount())
	}

	// Add a second transaction.
	err = seq.AddTransaction([]byte{0x04, 0x05})
	if err != nil {
		t.Fatalf("AddTransaction error: %v", err)
	}
	if seq.PendingCount() != 2 {
		t.Errorf("PendingCount: got %d, want 2", seq.PendingCount())
	}
}

func TestAddTransactionEmpty(t *testing.T) {
	seq := NewSequencer(DefaultSequencerConfig())
	err := seq.AddTransaction(nil)
	if err != ErrTxEmpty {
		t.Errorf("expected ErrTxEmpty, got %v", err)
	}
	err = seq.AddTransaction([]byte{})
	if err != ErrTxEmpty {
		t.Errorf("expected ErrTxEmpty, got %v", err)
	}
}

func TestSealBatch(t *testing.T) {
	seq := NewSequencer(DefaultSequencerConfig())

	// Add some transactions.
	for i := 0; i < 5; i++ {
		if err := seq.AddTransaction([]byte{byte(i), 0xab}); err != nil {
			t.Fatalf("AddTransaction error: %v", err)
		}
	}

	batch, err := seq.SealBatch()
	if err != nil {
		t.Fatalf("SealBatch error: %v", err)
	}
	if batch == nil {
		t.Fatal("batch is nil")
	}
	if len(batch.Transactions) != 5 {
		t.Errorf("expected 5 transactions, got %d", len(batch.Transactions))
	}
	if batch.ID.IsZero() {
		t.Error("expected non-zero batch ID")
	}
	if batch.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}

	// Pending should be cleared after sealing.
	if seq.PendingCount() != 0 {
		t.Errorf("expected 0 pending after seal, got %d", seq.PendingCount())
	}
}

func TestSealBatchEmpty(t *testing.T) {
	seq := NewSequencer(DefaultSequencerConfig())

	_, err := seq.SealBatch()
	if err != ErrBatchEmpty {
		t.Errorf("expected ErrBatchEmpty, got %v", err)
	}
}

func TestBatchHistory(t *testing.T) {
	seq := NewSequencer(DefaultSequencerConfig())

	// Seal two batches.
	seq.AddTransaction([]byte{0x01})
	b1, _ := seq.SealBatch()

	seq.AddTransaction([]byte{0x02})
	b2, _ := seq.SealBatch()

	history := seq.BatchHistory()
	if len(history) != 2 {
		t.Fatalf("expected 2 batches in history, got %d", len(history))
	}
	if history[0].ID != b1.ID {
		t.Error("first batch ID mismatch")
	}
	if history[1].ID != b2.ID {
		t.Error("second batch ID mismatch")
	}
}

func TestVerifyBatch(t *testing.T) {
	seq := NewSequencer(DefaultSequencerConfig())

	seq.AddTransaction([]byte{0x01, 0x02})
	seq.AddTransaction([]byte{0x03, 0x04})
	batch, _ := seq.SealBatch()

	if !seq.VerifyBatch(batch) {
		t.Error("expected batch to verify successfully")
	}
}

func TestVerifyBatchTampered(t *testing.T) {
	seq := NewSequencer(DefaultSequencerConfig())

	seq.AddTransaction([]byte{0x01, 0x02})
	seq.AddTransaction([]byte{0x03, 0x04})
	batch, _ := seq.SealBatch()

	// Tamper with a transaction.
	batch.Transactions[0] = []byte{0xff, 0xff}

	if seq.VerifyBatch(batch) {
		t.Error("expected tampered batch to fail verification")
	}
}

func TestVerifyBatchNil(t *testing.T) {
	seq := NewSequencer(DefaultSequencerConfig())

	if seq.VerifyBatch(nil) {
		t.Error("expected nil batch to fail verification")
	}
	if seq.VerifyBatch(&SequencerBatch{}) {
		t.Error("expected empty batch to fail verification")
	}
}

func TestMaxBatchSize(t *testing.T) {
	cfg := DefaultSequencerConfig()
	cfg.MaxBatchSize = 3
	seq := NewSequencer(cfg)

	for i := 0; i < 3; i++ {
		err := seq.AddTransaction([]byte{byte(i)})
		if err != nil {
			t.Fatalf("AddTransaction %d error: %v", i, err)
		}
	}

	// The 4th transaction should be rejected since batch is full.
	err := seq.AddTransaction([]byte{0xff})
	if err != ErrBatchFull {
		t.Errorf("expected ErrBatchFull, got %v", err)
	}

	if seq.PendingCount() != 3 {
		t.Errorf("expected 3 pending, got %d", seq.PendingCount())
	}
}

func TestConcurrentAddTx(t *testing.T) {
	seq := NewSequencer(DefaultSequencerConfig())

	var wg sync.WaitGroup
	n := 100
	wg.Add(n)

	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			seq.AddTransaction([]byte{byte(idx), 0x01})
		}(i)
	}

	wg.Wait()

	if seq.PendingCount() != n {
		t.Errorf("expected %d pending after concurrent adds, got %d", n, seq.PendingCount())
	}

	batch, err := seq.SealBatch()
	if err != nil {
		t.Fatalf("SealBatch error: %v", err)
	}
	if len(batch.Transactions) != n {
		t.Errorf("expected %d txs in sealed batch, got %d", n, len(batch.Transactions))
	}
}

func TestSealBatchWithCompression(t *testing.T) {
	cfg := DefaultSequencerConfig()
	cfg.CompressPayload = true
	seq := NewSequencer(cfg)

	seq.AddTransaction([]byte{0x01, 0x02, 0x03, 0x04, 0x05})
	seq.AddTransaction([]byte{0x06, 0x07, 0x08, 0x09, 0x0a})

	batch, err := seq.SealBatch()
	if err != nil {
		t.Fatalf("SealBatch error: %v", err)
	}
	if !batch.Compressed {
		t.Error("expected Compressed=true")
	}
	if len(batch.CompressedData) == 0 {
		t.Error("expected non-empty CompressedData")
	}
}

func TestSealBatchDeterministicID(t *testing.T) {
	// Two sequencers with the same transactions should produce the same batch ID.
	cfg := DefaultSequencerConfig()

	seq1 := NewSequencer(cfg)
	seq2 := NewSequencer(cfg)

	txs := [][]byte{{0x01, 0x02}, {0x03, 0x04}, {0x05}}
	for _, tx := range txs {
		seq1.AddTransaction(tx)
		seq2.AddTransaction(tx)
	}

	b1, _ := seq1.SealBatch()
	b2, _ := seq2.SealBatch()

	if b1.ID != b2.ID {
		t.Error("expected deterministic batch IDs for same transactions")
	}
}
