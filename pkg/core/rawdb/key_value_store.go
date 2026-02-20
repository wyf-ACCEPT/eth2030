// key_value_store.go provides an enhanced in-memory key-value store with
// atomic write batches, prefix-scoped iteration, and a prefixed store
// wrapper. It complements the existing MemoryDB and Table implementations
// with a simpler, standalone API.
package rawdb

import (
	"bytes"
	"errors"
	"sort"
	"sync"
)

// KVStore errors.
var (
	ErrKVNotFound     = errors.New("kv_store: key not found")
	ErrKVBatchApplied = errors.New("kv_store: batch already written")
)

// KVStore is the interface for a simple key-value store with batch and
// iteration support.
type KVStore interface {
	Get(key []byte) ([]byte, error)
	Put(key, value []byte) error
	Delete(key []byte) error
	Has(key []byte) (bool, error)
	NewBatch() *WriteBatch
	NewKVIterator(prefix, start []byte) KVIterator
	Close() error
}

// KVIterator iterates over key-value pairs in ascending key order.
type KVIterator interface {
	Next() bool
	Key() []byte
	Value() []byte
	Release()
}

// MemoryKVStore is an in-memory implementation of KVStore. It is
// safe for concurrent use and suitable for testing.
type MemoryKVStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// NewMemoryKVStore creates a new in-memory key-value store.
func NewMemoryKVStore() *MemoryKVStore {
	return &MemoryKVStore{
		data: make(map[string][]byte),
	}
}

// Get retrieves the value for a key. Returns ErrKVNotFound if absent.
func (m *MemoryKVStore) Get(key []byte) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	val, ok := m.data[string(key)]
	if !ok {
		return nil, ErrKVNotFound
	}
	cp := make([]byte, len(val))
	copy(cp, val)
	return cp, nil
}

// Put stores a key-value pair. Both key and value are copied.
func (m *MemoryKVStore) Put(key, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cp := make([]byte, len(value))
	copy(cp, value)
	m.data[string(key)] = cp
	return nil
}

// Delete removes a key from the store. It is a no-op if the key does
// not exist.
func (m *MemoryKVStore) Delete(key []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, string(key))
	return nil
}

// Has returns whether the key exists in the store.
func (m *MemoryKVStore) Has(key []byte) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.data[string(key)]
	return ok, nil
}

// Len returns the number of entries.
func (m *MemoryKVStore) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.data)
}

// Close is a no-op for the in-memory store.
func (m *MemoryKVStore) Close() error { return nil }

// NewBatch creates a new WriteBatch targeting this store.
func (m *MemoryKVStore) NewBatch() *WriteBatch {
	return &WriteBatch{store: m}
}

// NewKVIterator returns an iterator over all keys matching the given
// prefix, starting at or after the start key. Keys are returned in
// ascending lexicographic order.
func (m *MemoryKVStore) NewKVIterator(prefix, start []byte) KVIterator {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var keys []string
	for k := range m.data {
		kb := []byte(k)
		if len(prefix) > 0 && !bytes.HasPrefix(kb, prefix) {
			continue
		}
		if len(start) > 0 && bytes.Compare(kb, start) < 0 {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	items := make([]kvItem, len(keys))
	for i, k := range keys {
		val := make([]byte, len(m.data[k]))
		copy(val, m.data[k])
		items[i] = kvItem{key: []byte(k), value: val}
	}
	return &kvIterator{items: items, pos: -1}
}

// --- WriteBatch ---

// writeBatchOp represents a single put or delete operation.
type writeBatchOp struct {
	key    []byte
	value  []byte
	delete bool
}

// WriteBatch buffers put and delete operations for atomic application
// to the backing store. Operations are applied in order when Write is
// called.
type WriteBatch struct {
	store   *MemoryKVStore
	ops     []writeBatchOp
	size    int
	written bool
}

// Put adds a key-value pair to the batch.
func (wb *WriteBatch) Put(key, value []byte) {
	keyCp := make([]byte, len(key))
	copy(keyCp, key)
	valCp := make([]byte, len(value))
	copy(valCp, value)
	wb.ops = append(wb.ops, writeBatchOp{key: keyCp, value: valCp})
	wb.size += len(key) + len(value)
}

// Delete adds a key deletion to the batch.
func (wb *WriteBatch) Delete(key []byte) {
	keyCp := make([]byte, len(key))
	copy(keyCp, key)
	wb.ops = append(wb.ops, writeBatchOp{key: keyCp, delete: true})
	wb.size += len(key)
}

// Write applies all buffered operations atomically to the backing store.
// It holds the store's write lock for the duration to ensure atomicity.
// A batch can only be written once; subsequent calls return ErrKVBatchApplied.
func (wb *WriteBatch) Write() error {
	if wb.written {
		return ErrKVBatchApplied
	}
	wb.written = true

	wb.store.mu.Lock()
	defer wb.store.mu.Unlock()

	for _, op := range wb.ops {
		if op.delete {
			delete(wb.store.data, string(op.key))
		} else {
			wb.store.data[string(op.key)] = op.value
		}
	}
	return nil
}

// Reset clears all buffered operations so the batch can be reused.
func (wb *WriteBatch) Reset() {
	wb.ops = wb.ops[:0]
	wb.size = 0
	wb.written = false
}

// Len returns the number of buffered operations.
func (wb *WriteBatch) Len() int {
	return len(wb.ops)
}

// Size returns the total byte size of all buffered keys and values.
func (wb *WriteBatch) Size() int {
	return wb.size
}

// --- Iterator ---

// kvItem is a key-value pair for iteration.
type kvItem struct {
	key   []byte
	value []byte
}

// kvIterator provides ordered iteration over a snapshot of key-value pairs.
type kvIterator struct {
	items []kvItem
	pos   int
}

// Next advances the iterator to the next entry. Returns false when there
// are no more entries.
func (it *kvIterator) Next() bool {
	it.pos++
	return it.pos < len(it.items)
}

// Key returns the key at the current position, or nil if the iterator
// is not positioned on a valid entry.
func (it *kvIterator) Key() []byte {
	if it.pos < 0 || it.pos >= len(it.items) {
		return nil
	}
	return it.items[it.pos].key
}

// Value returns the value at the current position, or nil if the iterator
// is not positioned on a valid entry.
func (it *kvIterator) Value() []byte {
	if it.pos < 0 || it.pos >= len(it.items) {
		return nil
	}
	return it.items[it.pos].value
}

// Release is a no-op for the in-memory iterator.
func (it *kvIterator) Release() {}

// --- PrefixedStore ---

// PrefixedStore wraps a MemoryKVStore and transparently prepends a fixed
// prefix to all keys. This allows logical namespacing within a single store.
type PrefixedStore struct {
	inner  *MemoryKVStore
	prefix []byte
}

// NewPrefixedStore creates a PrefixedStore wrapping the given store with
// the specified prefix.
func NewPrefixedStore(store *MemoryKVStore, prefix []byte) *PrefixedStore {
	pfx := make([]byte, len(prefix))
	copy(pfx, prefix)
	return &PrefixedStore{
		inner:  store,
		prefix: pfx,
	}
}

// prefixKey prepends the prefix to the given key.
func (ps *PrefixedStore) prefixKey(key []byte) []byte {
	result := make([]byte, len(ps.prefix)+len(key))
	copy(result, ps.prefix)
	copy(result[len(ps.prefix):], key)
	return result
}

// Get retrieves the value for the prefixed key.
func (ps *PrefixedStore) Get(key []byte) ([]byte, error) {
	return ps.inner.Get(ps.prefixKey(key))
}

// Put stores a value at the prefixed key.
func (ps *PrefixedStore) Put(key, value []byte) error {
	return ps.inner.Put(ps.prefixKey(key), value)
}

// Delete removes the prefixed key.
func (ps *PrefixedStore) Delete(key []byte) error {
	return ps.inner.Delete(ps.prefixKey(key))
}

// Has checks if the prefixed key exists.
func (ps *PrefixedStore) Has(key []byte) (bool, error) {
	return ps.inner.Has(ps.prefixKey(key))
}

// NewBatch returns a batch that applies prefix-aware operations.
// The returned WriteBatch targets the inner store directly; callers
// must use PrefixedBatch for prefix-aware batching.
func (ps *PrefixedStore) NewBatch() *WriteBatch {
	return ps.inner.NewBatch()
}

// NewKVIterator returns an iterator over the prefixed namespace, starting
// at the given key within the namespace.
func (ps *PrefixedStore) NewKVIterator(prefix, start []byte) KVIterator {
	fullPrefix := ps.prefixKey(prefix)
	fullStart := ps.prefixKey(start)
	inner := ps.inner.NewKVIterator(fullPrefix, fullStart)
	return &prefixedIterator{
		inner:    inner,
		prefixSz: len(ps.prefix),
	}
}

// Close is a no-op; the underlying store should be closed separately.
func (ps *PrefixedStore) Close() error { return nil }

// Prefix returns the prefix used by this store.
func (ps *PrefixedStore) Prefix() []byte {
	cp := make([]byte, len(ps.prefix))
	copy(cp, ps.prefix)
	return cp
}

// prefixedIterator wraps an inner iterator and strips the prefix from keys.
type prefixedIterator struct {
	inner    KVIterator
	prefixSz int
}

func (it *prefixedIterator) Next() bool  { return it.inner.Next() }
func (it *prefixedIterator) Release()    { it.inner.Release() }
func (it *prefixedIterator) Value() []byte { return it.inner.Value() }

func (it *prefixedIterator) Key() []byte {
	key := it.inner.Key()
	if key == nil || len(key) < it.prefixSz {
		return key
	}
	return key[it.prefixSz:]
}
