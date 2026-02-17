// Package rawdb provides low-level database interfaces and accessor
// functions for storing and retrieving blockchain data.
//
// Architecture follows go-ethereum's prefix-based schema where each
// data type uses a distinct single-byte key prefix to avoid collisions.
package rawdb

import "errors"

var (
	ErrNotFound = errors.New("not found")
)

// KeyValueReader wraps the Has and Get methods of a backing data store.
type KeyValueReader interface {
	Has(key []byte) (bool, error)
	Get(key []byte) ([]byte, error)
}

// KeyValueWriter wraps the Put and Delete methods of a backing data store.
type KeyValueWriter interface {
	Put(key, value []byte) error
	Delete(key []byte) error
}

// KeyValueStore combines read and write access to a backing data store.
type KeyValueStore interface {
	KeyValueReader
	KeyValueWriter
	Close() error
}

// Iterator iterates over a database's key/value pairs in ascending key order.
type Iterator interface {
	Next() bool
	Key() []byte
	Value() []byte
	Release()
}

// KeyValueIterator adds iteration capability to a store.
type KeyValueIterator interface {
	KeyValueStore
	NewIterator(prefix []byte) Iterator
}

// Batch is a write-only database that commits changes atomically.
type Batch interface {
	KeyValueWriter
	ValueSize() int
	Write() error
	Reset()
}

// Batcher wraps the NewBatch method of a backing data store.
type Batcher interface {
	NewBatch() Batch
}

// Database is the full database interface combining all capabilities.
type Database interface {
	KeyValueStore
	Batcher
}
