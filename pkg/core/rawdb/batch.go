// batch.go provides a BatchWriter that buffers write and delete operations
// and flushes them atomically to a backing KeyValueStore. It supports
// configurable max batch size with automatic flushing when the threshold
// is exceeded.
package rawdb

import (
	"errors"
	"sync"
)

const (
	// DefaultMaxBatchSize is the default maximum batch size in bytes (4 MB).
	DefaultMaxBatchSize = 4 * 1024 * 1024
)

var (
	// ErrBatchClosed is returned when operating on a closed BatchWriter.
	ErrBatchClosed = errors.New("batch: writer is closed")
)

// batchOp represents a single buffered write or delete operation.
type batchWriterOp struct {
	key    []byte
	value  []byte
	delete bool
}

// BatchWriter buffers Put and Delete operations and flushes them atomically
// to a backing KeyValueStore. It is safe for concurrent use.
//
// When the buffered data exceeds MaxBatchSize, the batch is automatically
// flushed. This prevents unbounded memory growth when processing large
// numbers of writes.
type BatchWriter struct {
	mu           sync.Mutex
	db           KeyValueStore
	ops          []batchWriterOp
	size         int
	MaxBatchSize int
	closed       bool
}

// NewBatchWriter creates a new BatchWriter that writes to the given store.
// The default max batch size is 4 MB.
func NewBatchWriter(db KeyValueStore) *BatchWriter {
	return &BatchWriter{
		db:           db,
		MaxBatchSize: DefaultMaxBatchSize,
	}
}

// Put adds a key-value pair to the batch buffer. If the batch size exceeds
// MaxBatchSize after this operation, the batch is automatically flushed.
func (bw *BatchWriter) Put(key, value []byte) error {
	bw.mu.Lock()
	defer bw.mu.Unlock()

	if bw.closed {
		return ErrBatchClosed
	}

	// Copy key and value to prevent external mutation.
	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)
	valCopy := make([]byte, len(value))
	copy(valCopy, value)

	bw.ops = append(bw.ops, batchWriterOp{
		key:   keyCopy,
		value: valCopy,
	})
	bw.size += len(key) + len(value)

	// Auto-flush if batch exceeds max size.
	if bw.MaxBatchSize > 0 && bw.size >= bw.MaxBatchSize {
		return bw.flushLocked()
	}
	return nil
}

// Delete marks a key for deletion in the batch buffer. If the batch size
// exceeds MaxBatchSize after this operation, the batch is automatically
// flushed.
func (bw *BatchWriter) Delete(key []byte) error {
	bw.mu.Lock()
	defer bw.mu.Unlock()

	if bw.closed {
		return ErrBatchClosed
	}

	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)

	bw.ops = append(bw.ops, batchWriterOp{
		key:    keyCopy,
		delete: true,
	})
	bw.size += len(key)

	// Auto-flush if batch exceeds max size.
	if bw.MaxBatchSize > 0 && bw.size >= bw.MaxBatchSize {
		return bw.flushLocked()
	}
	return nil
}

// Size returns the current batch size in bytes (sum of all buffered keys
// and values).
func (bw *BatchWriter) Size() int {
	bw.mu.Lock()
	defer bw.mu.Unlock()
	return bw.size
}

// Len returns the number of buffered operations.
func (bw *BatchWriter) Len() int {
	bw.mu.Lock()
	defer bw.mu.Unlock()
	return len(bw.ops)
}

// Flush writes all buffered operations atomically to the backing store.
// After a successful flush, the batch buffer is reset.
func (bw *BatchWriter) Flush() error {
	bw.mu.Lock()
	defer bw.mu.Unlock()

	if bw.closed {
		return ErrBatchClosed
	}

	return bw.flushLocked()
}

// flushLocked performs the actual flush. Caller must hold bw.mu.
func (bw *BatchWriter) flushLocked() error {
	if len(bw.ops) == 0 {
		return nil
	}

	// Use the store's batch interface if available for atomic writes.
	if batcher, ok := bw.db.(Batcher); ok {
		batch := batcher.NewBatch()
		for _, op := range bw.ops {
			if op.delete {
				if err := batch.Delete(op.key); err != nil {
					return err
				}
			} else {
				if err := batch.Put(op.key, op.value); err != nil {
					return err
				}
			}
		}
		if err := batch.Write(); err != nil {
			return err
		}
	} else {
		// Fall back to individual writes if no batch support.
		for _, op := range bw.ops {
			if op.delete {
				if err := bw.db.Delete(op.key); err != nil {
					return err
				}
			} else {
				if err := bw.db.Put(op.key, op.value); err != nil {
					return err
				}
			}
		}
	}

	bw.resetLocked()
	return nil
}

// Reset discards all pending operations without writing them.
func (bw *BatchWriter) Reset() {
	bw.mu.Lock()
	defer bw.mu.Unlock()
	bw.resetLocked()
}

// resetLocked clears the batch buffer. Caller must hold bw.mu.
func (bw *BatchWriter) resetLocked() {
	bw.ops = bw.ops[:0]
	bw.size = 0
}

// Close flushes any remaining operations and marks the writer as closed.
// Further operations on a closed BatchWriter return ErrBatchClosed.
func (bw *BatchWriter) Close() error {
	bw.mu.Lock()
	defer bw.mu.Unlock()

	if bw.closed {
		return nil
	}

	err := bw.flushLocked()
	bw.closed = true
	return err
}
