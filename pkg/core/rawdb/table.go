// table.go provides namespace-isolated database tables by key prefixing.
// A Table wraps an underlying KeyValueStore, transparently prepending a
// fixed prefix to every key. This allows multiple logical databases to
// share a single physical store without key collisions.
//
// Common use cases:
//   - Isolating chain data (headers, bodies, receipts, TDs) by chain ID
//   - Separating state trie nodes from chain data
//   - Supporting multiple data domains in a single LevelDB/Pebble instance
package rawdb

import (
	"bytes"
	"sort"
	"sync"
)

// Table wraps a KeyValueStore, prepending a fixed prefix to every key.
// It implements KeyValueStore, Batcher, and KeyValueIterator so it can
// be used anywhere the underlying store is expected.
type Table struct {
	db     KeyValueStore
	prefix []byte
}

// NewTable creates a new Table with the given prefix over the backing store.
// All keys written through this Table will have the prefix prepended, and
// all reads will automatically add the prefix before querying the backing store.
func NewTable(db KeyValueStore, prefix string) *Table {
	return &Table{
		db:     db,
		prefix: []byte(prefix),
	}
}

// prefixKey prepends the table prefix to the given key.
func (t *Table) prefixKey(key []byte) []byte {
	prefixed := make([]byte, len(t.prefix)+len(key))
	copy(prefixed, t.prefix)
	copy(prefixed[len(t.prefix):], key)
	return prefixed
}

// Has checks whether the prefixed key exists in the backing store.
func (t *Table) Has(key []byte) (bool, error) {
	return t.db.Has(t.prefixKey(key))
}

// Get retrieves the value for the prefixed key from the backing store.
func (t *Table) Get(key []byte) ([]byte, error) {
	return t.db.Get(t.prefixKey(key))
}

// Put stores a key-value pair with the prefixed key in the backing store.
func (t *Table) Put(key, value []byte) error {
	return t.db.Put(t.prefixKey(key), value)
}

// Delete removes the prefixed key from the backing store.
func (t *Table) Delete(key []byte) error {
	return t.db.Delete(t.prefixKey(key))
}

// Close is a no-op for Table; the caller should close the underlying store.
func (t *Table) Close() error {
	return nil
}

// NewBatch creates a new tableBatch that prepends the prefix to all operations.
func (t *Table) NewBatch() Batch {
	return &tableBatch{
		table: t,
	}
}

// Prefix returns the prefix string used by this table.
func (t *Table) Prefix() string {
	return string(t.prefix)
}

// --- tableBatch ---

// tableBatch wraps batch operations to transparently apply the table prefix.
type tableBatch struct {
	table *Table
	ops   []tableBatchOp
	size  int
}

type tableBatchOp struct {
	key    []byte
	value  []byte
	delete bool
}

func (b *tableBatch) Put(key, value []byte) error {
	prefixed := b.table.prefixKey(key)
	b.ops = append(b.ops, tableBatchOp{
		key:   prefixed,
		value: append([]byte{}, value...),
	})
	b.size += len(prefixed) + len(value)
	return nil
}

func (b *tableBatch) Delete(key []byte) error {
	prefixed := b.table.prefixKey(key)
	b.ops = append(b.ops, tableBatchOp{
		key:    prefixed,
		delete: true,
	})
	b.size += len(prefixed)
	return nil
}

func (b *tableBatch) ValueSize() int {
	return b.size
}

func (b *tableBatch) Write() error {
	for _, op := range b.ops {
		if op.delete {
			if err := b.table.db.Delete(op.key); err != nil {
				return err
			}
		} else {
			if err := b.table.db.Put(op.key, op.value); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *tableBatch) Reset() {
	b.ops = b.ops[:0]
	b.size = 0
}

// --- Iteration support ---

// NewIterator returns an iterator over all keys in this table's namespace.
// Keys returned by the iterator have the table prefix stripped, so callers
// see the same keys they originally wrote.
func (t *Table) NewIterator(prefix []byte) Iterator {
	// Combine table prefix with the caller's prefix.
	fullPrefix := t.prefixKey(prefix)

	// Delegate to underlying store if it supports iteration.
	if iter, ok := t.db.(KeyValueIterator); ok {
		inner := iter.NewIterator(fullPrefix)
		return &tableIterator{
			inner:       inner,
			tablePrefix: t.prefix,
		}
	}

	// Fallback for stores without native iteration: scan all keys.
	return t.scanIterator(fullPrefix)
}

// scanIterator builds an in-memory iterator by scanning the backing store.
// This fallback is used when the backing store does not implement
// KeyValueIterator. It requires the backing store to be a MemoryDB.
func (t *Table) scanIterator(fullPrefix []byte) Iterator {
	mdb, ok := t.db.(*MemoryDB)
	if !ok {
		return &memIterator{pos: -1} // empty iterator
	}

	mdb.mu.RLock()
	defer mdb.mu.RUnlock()

	var keys []string
	for k := range mdb.data {
		if bytes.HasPrefix([]byte(k), fullPrefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	items := make([]kv, len(keys))
	for i, k := range keys {
		// Strip the table prefix from the key.
		strippedKey := []byte(k)[len(t.prefix):]
		val := make([]byte, len(mdb.data[k]))
		copy(val, mdb.data[k])
		items[i] = kv{key: strippedKey, value: val}
	}
	return &memIterator{items: items, pos: -1}
}

// tableIterator wraps an underlying iterator, stripping the table prefix
// from returned keys.
type tableIterator struct {
	inner       Iterator
	tablePrefix []byte
}

func (it *tableIterator) Next() bool {
	return it.inner.Next()
}

func (it *tableIterator) Key() []byte {
	key := it.inner.Key()
	if key == nil {
		return nil
	}
	// Strip the table prefix.
	if len(key) >= len(it.tablePrefix) {
		return key[len(it.tablePrefix):]
	}
	return key
}

func (it *tableIterator) Value() []byte {
	return it.inner.Value()
}

func (it *tableIterator) Release() {
	it.inner.Release()
}

// --- Predefined table constructors ---

// ChainDataNamespace is the standard prefix for chain data (headers, bodies,
// receipts, total difficulty) in a multi-chain or isolated database.
const ChainDataNamespace = "chaindata/"

// StateTrieNamespace is the prefix for state trie node storage.
const StateTrieNamespace = "statetrie/"

// TxIndexNamespace is the prefix for transaction index data.
const TxIndexNamespace = "txindex/"

// NewChainDataTable creates a table for chain data storage (headers, bodies,
// receipts, TDs) using the standard "chaindata/" prefix.
func NewChainDataTable(db KeyValueStore) *Table {
	return NewTable(db, ChainDataNamespace)
}

// NewStateTrieTable creates a table for state trie node storage.
func NewStateTrieTable(db KeyValueStore) *Table {
	return NewTable(db, StateTrieNamespace)
}

// NewTxIndexTable creates a table for transaction index data.
func NewTxIndexTable(db KeyValueStore) *Table {
	return NewTable(db, TxIndexNamespace)
}

// --- Compaction hints ---

// CompactionHint provides guidance to the underlying database about key
// ranges that can be compacted. This is useful after bulk deletions
// (e.g., history pruning) to reclaim disk space.
type CompactionHint struct {
	Start []byte // inclusive start key
	Limit []byte // exclusive end key
}

// CompactionHintForTable returns a compaction hint covering all keys
// in the given table's namespace.
func CompactionHintForTable(t *Table) CompactionHint {
	start := make([]byte, len(t.prefix))
	copy(start, t.prefix)

	// Compute the exclusive upper bound by incrementing the last byte.
	limit := make([]byte, len(t.prefix))
	copy(limit, t.prefix)
	incrementBytes(limit)

	return CompactionHint{
		Start: start,
		Limit: limit,
	}
}

// CompactionHintForPrefix returns a compaction hint covering all keys
// with the given prefix within the table's namespace.
func CompactionHintForPrefix(t *Table, prefix []byte) CompactionHint {
	start := t.prefixKey(prefix)
	limit := make([]byte, len(start))
	copy(limit, start)
	incrementBytes(limit)

	return CompactionHint{
		Start: start,
		Limit: limit,
	}
}

// incrementBytes increments a byte slice as a big-endian number by 1.
// Used to compute exclusive upper bounds for range operations.
func incrementBytes(b []byte) {
	for i := len(b) - 1; i >= 0; i-- {
		b[i]++
		if b[i] != 0 {
			return
		}
	}
}

// --- Multi-table database ---

// TableDB holds a set of named tables sharing a single backing store.
// It provides convenient access to isolated namespaces for different
// types of chain data.
type TableDB struct {
	mu     sync.RWMutex
	db     KeyValueStore
	tables map[string]*Table
}

// NewTableDB creates a new TableDB over the given backing store.
func NewTableDB(db KeyValueStore) *TableDB {
	return &TableDB{
		db:     db,
		tables: make(map[string]*Table),
	}
}

// Table returns a table for the given namespace, creating it if needed.
func (tdb *TableDB) Table(namespace string) *Table {
	tdb.mu.RLock()
	if t, ok := tdb.tables[namespace]; ok {
		tdb.mu.RUnlock()
		return t
	}
	tdb.mu.RUnlock()

	tdb.mu.Lock()
	defer tdb.mu.Unlock()

	// Double-check after acquiring write lock.
	if t, ok := tdb.tables[namespace]; ok {
		return t
	}

	t := NewTable(tdb.db, namespace)
	tdb.tables[namespace] = t
	return t
}

// Namespaces returns a sorted list of all registered namespace prefixes.
func (tdb *TableDB) Namespaces() []string {
	tdb.mu.RLock()
	defer tdb.mu.RUnlock()

	ns := make([]string, 0, len(tdb.tables))
	for name := range tdb.tables {
		ns = append(ns, name)
	}
	sort.Strings(ns)
	return ns
}

// Close closes the underlying database. All tables become invalid.
func (tdb *TableDB) Close() error {
	return tdb.db.Close()
}

// Compile-time interface checks.
var _ KeyValueStore = (*Table)(nil)
var _ Batcher = (*Table)(nil)
