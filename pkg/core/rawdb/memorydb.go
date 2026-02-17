package rawdb

import (
	"bytes"
	"sort"
	"sync"
)

// MemoryDB is an in-memory key-value database implementing the Database interface.
// It is safe for concurrent use. Intended for testing and development.
type MemoryDB struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// NewMemoryDB creates a new in-memory database.
func NewMemoryDB() *MemoryDB {
	return &MemoryDB{data: make(map[string][]byte)}
}

func (db *MemoryDB) Has(key []byte) (bool, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	_, ok := db.data[string(key)]
	return ok, nil
}

func (db *MemoryDB) Get(key []byte) ([]byte, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	val, ok := db.data[string(key)]
	if !ok {
		return nil, ErrNotFound
	}
	ret := make([]byte, len(val))
	copy(ret, val)
	return ret, nil
}

func (db *MemoryDB) Put(key, value []byte) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	cp := make([]byte, len(value))
	copy(cp, value)
	db.data[string(key)] = cp
	return nil
}

func (db *MemoryDB) Delete(key []byte) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	delete(db.data, string(key))
	return nil
}

func (db *MemoryDB) Close() error { return nil }

// NewBatch creates a new batch writer.
func (db *MemoryDB) NewBatch() Batch {
	return &memBatch{db: db}
}

// NewIterator returns an iterator over all keys with the given prefix.
func (db *MemoryDB) NewIterator(prefix []byte) Iterator {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var keys []string
	for k := range db.data {
		if bytes.HasPrefix([]byte(k), prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	items := make([]kv, len(keys))
	for i, k := range keys {
		val := make([]byte, len(db.data[k]))
		copy(val, db.data[k])
		items[i] = kv{key: []byte(k), value: val}
	}
	return &memIterator{items: items, pos: -1}
}

// Len returns the number of entries in the database.
func (db *MemoryDB) Len() int {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return len(db.data)
}

// --- Batch ---

type memBatch struct {
	db    *MemoryDB
	ops   []batchOp
	size  int
}

type batchOp struct {
	key    []byte
	value  []byte
	delete bool
}

func (b *memBatch) Put(key, value []byte) error {
	b.ops = append(b.ops, batchOp{key: append([]byte{}, key...), value: append([]byte{}, value...)})
	b.size += len(key) + len(value)
	return nil
}

func (b *memBatch) Delete(key []byte) error {
	b.ops = append(b.ops, batchOp{key: append([]byte{}, key...), delete: true})
	b.size += len(key)
	return nil
}

func (b *memBatch) ValueSize() int { return b.size }

func (b *memBatch) Write() error {
	b.db.mu.Lock()
	defer b.db.mu.Unlock()
	for _, op := range b.ops {
		if op.delete {
			delete(b.db.data, string(op.key))
		} else {
			b.db.data[string(op.key)] = op.value
		}
	}
	return nil
}

func (b *memBatch) Reset() {
	b.ops = b.ops[:0]
	b.size = 0
}

// --- Iterator ---

type kv struct {
	key, value []byte
}

type memIterator struct {
	items []kv
	pos   int
}

func (it *memIterator) Next() bool {
	it.pos++
	return it.pos < len(it.items)
}

func (it *memIterator) Key() []byte {
	if it.pos < 0 || it.pos >= len(it.items) {
		return nil
	}
	return it.items[it.pos].key
}

func (it *memIterator) Value() []byte {
	if it.pos < 0 || it.pos >= len(it.items) {
		return nil
	}
	return it.items[it.pos].value
}

func (it *memIterator) Release() {}
