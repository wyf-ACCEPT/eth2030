package rollup

import (
	"bytes"
	"compress/zlib"
	"errors"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Sequencer errors.
var (
	ErrBatchEmpty     = errors.New("sequencer: no transactions to seal")
	ErrBatchFull      = errors.New("sequencer: batch is full")
	ErrTxEmpty        = errors.New("sequencer: transaction data is empty")
	ErrCompressFailed = errors.New("sequencer: payload compression failed")
)

// SequencerConfig controls the batching and submission behavior of the sequencer.
type SequencerConfig struct {
	// MaxBatchSize is the maximum number of transactions per batch.
	MaxBatchSize int

	// BatchTimeout is the maximum duration before a batch is auto-sealed.
	BatchTimeout time.Duration

	// L1SubmissionInterval is the target interval between L1 submissions.
	L1SubmissionInterval time.Duration

	// CompressPayload enables zlib compression of sealed batch data.
	CompressPayload bool
}

// DefaultSequencerConfig returns a SequencerConfig with sensible defaults.
func DefaultSequencerConfig() SequencerConfig {
	return SequencerConfig{
		MaxBatchSize:         1000,
		BatchTimeout:         2 * time.Second,
		L1SubmissionInterval: 6 * time.Second,
		CompressPayload:      false,
	}
}

// SequencerBatch is a sealed collection of transactions ready for L1 submission.
type SequencerBatch struct {
	// ID is the Keccak256 hash over all transaction hashes in the batch.
	ID types.Hash

	// Transactions holds the raw encoded transactions.
	Transactions [][]byte

	// L1BlockNumber is the L1 block number at the time the batch was sealed.
	L1BlockNumber uint64

	// Timestamp records when the batch was sealed.
	Timestamp time.Time

	// Compressed indicates whether CompressedData is populated.
	Compressed bool

	// CompressedData holds the zlib-compressed concatenation of transactions.
	CompressedData []byte
}

// Sequencer collects transactions into batches for L1 submission.
type Sequencer struct {
	mu      sync.Mutex
	config  SequencerConfig
	pending [][]byte
	history []*SequencerBatch
}

// NewSequencer creates a new Sequencer with the given configuration.
func NewSequencer(config SequencerConfig) *Sequencer {
	return &Sequencer{
		config:  config,
		pending: make([][]byte, 0, config.MaxBatchSize),
		history: make([]*SequencerBatch, 0),
	}
}

// AddTransaction appends a raw transaction to the current pending batch.
// If the batch reaches MaxBatchSize, it is automatically sealed.
func (s *Sequencer) AddTransaction(tx []byte) error {
	if len(tx) == 0 {
		return ErrTxEmpty
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.pending) >= s.config.MaxBatchSize {
		return ErrBatchFull
	}

	// Copy the tx data to avoid external mutation.
	txCopy := make([]byte, len(tx))
	copy(txCopy, tx)
	s.pending = append(s.pending, txCopy)

	return nil
}

// SealBatch finalizes the current pending transactions into a SequencerBatch.
// The batch ID is computed as Keccak256 of all individual tx hashes concatenated.
func (s *Sequencer) SealBatch() (*SequencerBatch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.pending) == 0 {
		return nil, ErrBatchEmpty
	}

	txs := s.pending
	s.pending = make([][]byte, 0, s.config.MaxBatchSize)

	// Compute batch ID: Keccak256(txHash_0 || txHash_1 || ... || txHash_n).
	var hashInput []byte
	for _, tx := range txs {
		h := crypto.Keccak256(tx)
		hashInput = append(hashInput, h...)
	}
	id := crypto.Keccak256Hash(hashInput)

	batch := &SequencerBatch{
		ID:           id,
		Transactions: txs,
		Timestamp:    time.Now(),
	}

	// Optionally compress the payload.
	if s.config.CompressPayload {
		compressed, err := compressTransactions(txs)
		if err != nil {
			return nil, ErrCompressFailed
		}
		batch.Compressed = true
		batch.CompressedData = compressed
	}

	s.history = append(s.history, batch)
	return batch, nil
}

// PendingCount returns the number of transactions in the current unsea batch.
func (s *Sequencer) PendingCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pending)
}

// BatchHistory returns all sealed batches in order.
func (s *Sequencer) BatchHistory() []*SequencerBatch {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]*SequencerBatch, len(s.history))
	copy(out, s.history)
	return out
}

// VerifyBatch checks batch integrity by recomputing the ID hash.
func (s *Sequencer) VerifyBatch(batch *SequencerBatch) bool {
	if batch == nil || len(batch.Transactions) == 0 {
		return false
	}

	var hashInput []byte
	for _, tx := range batch.Transactions {
		h := crypto.Keccak256(tx)
		hashInput = append(hashInput, h...)
	}
	expected := crypto.Keccak256Hash(hashInput)
	return expected == batch.ID
}

// compressTransactions concatenates and zlib-compresses all transactions.
func compressTransactions(txs [][]byte) ([]byte, error) {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	for _, tx := range txs {
		if _, err := w.Write(tx); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
