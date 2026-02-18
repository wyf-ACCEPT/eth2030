// FileDB implements a persistent file-based key-value store using a flat
// directory layout with a write-ahead log (WAL) for crash safety. Keys are
// hex-encoded and stored as individual files under a data/ subdirectory.
// An in-memory index is maintained for fast lookups and rebuilt from disk
// on open. A file lock prevents concurrent process access.
//
// Layout:
//
//	<dir>/
//	  LOCK          - flock-based exclusive lock
//	  wal           - write-ahead log (binary, append-only)
//	  data/         - key-value files (filename = hex(key))
package rawdb

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
)

// WAL record types.
const (
	walPut    byte = 0x01
	walDelete byte = 0x02
	walCommit byte = 0x03
)

// FileDB is a file-based persistent key-value store implementing the Database
// interface. It is safe for concurrent use from multiple goroutines within
// a single process. Cross-process safety is provided by a file lock.
type FileDB struct {
	mu      sync.RWMutex
	dir     string            // root directory
	dataDir string            // dir + "/data"
	index   map[string][]byte // in-memory cache of all key-value pairs
	walFile *os.File          // write-ahead log file handle
	lockFd  int               // file descriptor for LOCK file
	closed  bool
}

// NewFileDB opens or creates a file-based key-value database at dir.
// The directory is created if it does not exist. An exclusive file lock
// is acquired to prevent concurrent access from other processes.
func NewFileDB(dir string) (*FileDB, error) {
	// Create directory structure.
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("filedb: mkdir: %w", err)
	}

	// Acquire exclusive file lock.
	lockPath := filepath.Join(dir, "LOCK")
	lockFd, err := acquireLock(lockPath)
	if err != nil {
		return nil, fmt.Errorf("filedb: lock: %w", err)
	}

	db := &FileDB{
		dir:     dir,
		dataDir: dataDir,
		index:   make(map[string][]byte),
		lockFd:  lockFd,
	}

	// Load existing data files into in-memory index.
	if err := db.loadIndex(); err != nil {
		releaseLock(lockFd)
		return nil, fmt.Errorf("filedb: load index: %w", err)
	}

	// Replay WAL to recover any uncommitted operations, then rebuild.
	if err := db.replayWAL(); err != nil {
		releaseLock(lockFd)
		return nil, fmt.Errorf("filedb: replay wal: %w", err)
	}

	// Open WAL for new writes (truncate after successful replay).
	walPath := filepath.Join(dir, "wal")
	walFile, err := os.OpenFile(walPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		releaseLock(lockFd)
		return nil, fmt.Errorf("filedb: open wal: %w", err)
	}
	db.walFile = walFile

	return db, nil
}

// Has returns true if the key exists in the database.
func (db *FileDB) Has(key []byte) (bool, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.closed {
		return false, errors.New("filedb: database closed")
	}
	_, ok := db.index[string(key)]
	return ok, nil
}

// Get retrieves the value for a key. Returns ErrNotFound if the key does not exist.
func (db *FileDB) Get(key []byte) ([]byte, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.closed {
		return nil, errors.New("filedb: database closed")
	}
	val, ok := db.index[string(key)]
	if !ok {
		return nil, ErrNotFound
	}
	ret := make([]byte, len(val))
	copy(ret, val)
	return ret, nil
}

// Put stores a key-value pair. The write is persisted to the WAL and data
// directory before returning.
func (db *FileDB) Put(key, value []byte) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed {
		return errors.New("filedb: database closed")
	}
	return db.putLocked(key, value)
}

// putLocked performs Put while the caller already holds db.mu.
func (db *FileDB) putLocked(key, value []byte) error {
	// Write WAL record.
	if err := db.walWrite(walPut, key, value); err != nil {
		return err
	}
	// Persist to data file.
	if err := db.writeDataFile(key, value); err != nil {
		return err
	}
	// Write commit marker.
	if err := db.walWriteCommit(); err != nil {
		return err
	}
	// Update in-memory index.
	cp := make([]byte, len(value))
	copy(cp, value)
	db.index[string(key)] = cp
	return nil
}

// Delete removes a key. Returns nil even if the key does not exist.
func (db *FileDB) Delete(key []byte) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed {
		return errors.New("filedb: database closed")
	}
	return db.deleteLocked(key)
}

// deleteLocked performs Delete while the caller already holds db.mu.
func (db *FileDB) deleteLocked(key []byte) error {
	// Write WAL record.
	if err := db.walWrite(walDelete, key, nil); err != nil {
		return err
	}
	// Remove data file (ignore not-exist errors).
	path := db.keyPath(key)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("filedb: remove data file: %w", err)
	}
	// Write commit marker.
	if err := db.walWriteCommit(); err != nil {
		return err
	}
	// Update in-memory index.
	delete(db.index, string(key))
	return nil
}

// Close closes the database, syncs the WAL, and releases the file lock.
func (db *FileDB) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed {
		return nil
	}
	db.closed = true
	var errs []error
	if db.walFile != nil {
		if err := db.walFile.Sync(); err != nil {
			errs = append(errs, err)
		}
		if err := db.walFile.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	releaseLock(db.lockFd)
	if len(errs) > 0 {
		return fmt.Errorf("filedb: close: %v", errs)
	}
	return nil
}

// NewBatch creates a new batch writer for atomic batch operations.
func (db *FileDB) NewBatch() Batch {
	return &fileBatch{db: db}
}

// NewIterator returns an iterator over all keys with the given prefix,
// in ascending key order.
func (db *FileDB) NewIterator(prefix []byte) Iterator {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var keys []string
	for k := range db.index {
		if bytes.HasPrefix([]byte(k), prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	items := make([]kv, len(keys))
	for i, k := range keys {
		val := make([]byte, len(db.index[k]))
		copy(val, db.index[k])
		items[i] = kv{key: []byte(k), value: val}
	}
	return &memIterator{items: items, pos: -1}
}

// Len returns the number of entries in the database.
func (db *FileDB) Len() int {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return len(db.index)
}

// --- WAL operations ---

// walWrite appends a put or delete record to the WAL.
// Format: [type:1][keyLen:4][key:N][valLen:4][val:M]
// For delete, valLen is 0 and val is omitted.
func (db *FileDB) walWrite(op byte, key, value []byte) error {
	var buf []byte
	buf = append(buf, op)

	// Key length + key.
	kl := make([]byte, 4)
	binary.BigEndian.PutUint32(kl, uint32(len(key)))
	buf = append(buf, kl...)
	buf = append(buf, key...)

	// Value length + value.
	vl := make([]byte, 4)
	binary.BigEndian.PutUint32(vl, uint32(len(value)))
	buf = append(buf, vl...)
	buf = append(buf, value...)

	if _, err := db.walFile.Write(buf); err != nil {
		return fmt.Errorf("filedb: wal write: %w", err)
	}
	return nil
}

// walWriteCommit writes a commit marker to the WAL and syncs.
func (db *FileDB) walWriteCommit() error {
	if _, err := db.walFile.Write([]byte{walCommit}); err != nil {
		return fmt.Errorf("filedb: wal commit write: %w", err)
	}
	if err := db.walFile.Sync(); err != nil {
		return fmt.Errorf("filedb: wal sync: %w", err)
	}
	return nil
}

// replayWAL reads the WAL file and replays committed operations.
// Uncommitted operations (those without a trailing commit marker) are
// discarded. After replay, the data files and index are consistent.
func (db *FileDB) replayWAL() error {
	walPath := filepath.Join(db.dir, "wal")
	f, err := os.Open(walPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No WAL, nothing to replay.
		}
		return err
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}

	// Parse WAL records into transactions. Each transaction is a sequence
	// of put/delete records followed by a commit marker.
	type walRecord struct {
		op    byte
		key   []byte
		value []byte
	}

	var pending []walRecord
	pos := 0
	for pos < len(data) {
		op := data[pos]
		pos++

		switch op {
		case walCommit:
			// Apply all pending records.
			for _, rec := range pending {
				switch rec.op {
				case walPut:
					if err := db.writeDataFile(rec.key, rec.value); err != nil {
						return err
					}
					cp := make([]byte, len(rec.value))
					copy(cp, rec.value)
					db.index[string(rec.key)] = cp
				case walDelete:
					path := db.keyPath(rec.key)
					if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
						return err
					}
					delete(db.index, string(rec.key))
				}
			}
			pending = pending[:0]

		case walPut, walDelete:
			// Read key.
			if pos+4 > len(data) {
				return nil // Truncated WAL, discard.
			}
			kl := binary.BigEndian.Uint32(data[pos : pos+4])
			pos += 4
			if pos+int(kl) > len(data) {
				return nil
			}
			key := make([]byte, kl)
			copy(key, data[pos:pos+int(kl)])
			pos += int(kl)

			// Read value.
			if pos+4 > len(data) {
				return nil
			}
			vl := binary.BigEndian.Uint32(data[pos : pos+4])
			pos += 4
			if pos+int(vl) > len(data) {
				return nil
			}
			value := make([]byte, vl)
			copy(value, data[pos:pos+int(vl)])
			pos += int(vl)

			pending = append(pending, walRecord{op: op, key: key, value: value})

		default:
			// Unknown record type -- WAL is corrupted past this point.
			return nil
		}
	}
	// Any remaining pending records without a commit marker are discarded.
	return nil
}

// --- Data file operations ---

// keyPath returns the filesystem path for a key's data file.
func (db *FileDB) keyPath(key []byte) string {
	return filepath.Join(db.dataDir, hex.EncodeToString(key))
}

// writeDataFile atomically writes a value to the data file for key.
// It writes to a temporary file first, then renames for atomicity.
func (db *FileDB) writeDataFile(key, value []byte) error {
	path := db.keyPath(key)
	tmpPath := path + ".tmp"

	if err := os.WriteFile(tmpPath, value, 0o644); err != nil {
		return fmt.Errorf("filedb: write tmp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath) // Best-effort cleanup.
		return fmt.Errorf("filedb: rename: %w", err)
	}
	return nil
}

// loadIndex scans the data directory and loads all key-value pairs into
// the in-memory index.
func (db *FileDB) loadIndex() error {
	entries, err := os.ReadDir(db.dataDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Skip temporary files from incomplete writes.
		if len(name) > 4 && name[len(name)-4:] == ".tmp" {
			os.Remove(filepath.Join(db.dataDir, name))
			continue
		}
		key, err := hex.DecodeString(name)
		if err != nil {
			continue // Skip non-hex filenames.
		}
		value, err := os.ReadFile(filepath.Join(db.dataDir, name))
		if err != nil {
			return err
		}
		db.index[string(key)] = value
	}
	return nil
}

// --- File locking ---

// acquireLock opens (or creates) the lock file and acquires an exclusive
// flock on it. Returns the file descriptor.
func acquireLock(path string) (int, error) {
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR, 0o644)
	if err != nil {
		return -1, fmt.Errorf("open lock file: %w", err)
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		syscall.Close(fd)
		return -1, fmt.Errorf("acquire flock: %w", err)
	}
	return fd, nil
}

// releaseLock releases the file lock and closes the file descriptor.
func releaseLock(fd int) {
	syscall.Flock(fd, syscall.LOCK_UN)
	syscall.Close(fd)
}

// --- Batch ---

// fileBatch buffers Put and Delete operations and applies them atomically.
type fileBatch struct {
	db   *FileDB
	ops  []fileBatchOp
	size int
}

type fileBatchOp struct {
	key    []byte
	value  []byte
	delete bool
}

func (b *fileBatch) Put(key, value []byte) error {
	b.ops = append(b.ops, fileBatchOp{
		key:   append([]byte{}, key...),
		value: append([]byte{}, value...),
	})
	b.size += len(key) + len(value)
	return nil
}

func (b *fileBatch) Delete(key []byte) error {
	b.ops = append(b.ops, fileBatchOp{
		key:    append([]byte{}, key...),
		delete: true,
	})
	b.size += len(key)
	return nil
}

func (b *fileBatch) ValueSize() int { return b.size }

// Write applies all buffered operations atomically. All WAL records for the
// batch are written before the commit marker, so either all operations in
// the batch are applied or none are (in case of a crash mid-write).
func (b *fileBatch) Write() error {
	b.db.mu.Lock()
	defer b.db.mu.Unlock()
	if b.db.closed {
		return errors.New("filedb: database closed")
	}

	// Write all WAL records first (without commit markers).
	for _, op := range b.ops {
		if op.delete {
			if err := b.db.walWrite(walDelete, op.key, nil); err != nil {
				return err
			}
		} else {
			if err := b.db.walWrite(walPut, op.key, op.value); err != nil {
				return err
			}
		}
	}

	// Apply all data file changes.
	for _, op := range b.ops {
		if op.delete {
			path := b.db.keyPath(op.key)
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("filedb: batch remove: %w", err)
			}
		} else {
			if err := b.db.writeDataFile(op.key, op.value); err != nil {
				return err
			}
		}
	}

	// Write commit marker and sync.
	if err := b.db.walWriteCommit(); err != nil {
		return err
	}

	// Update in-memory index.
	for _, op := range b.ops {
		if op.delete {
			delete(b.db.index, string(op.key))
		} else {
			cp := make([]byte, len(op.value))
			copy(cp, op.value)
			b.db.index[string(op.key)] = cp
		}
	}
	return nil
}

func (b *fileBatch) Reset() {
	b.ops = b.ops[:0]
	b.size = 0
}

// Compile-time interface checks.
var _ Database = (*FileDB)(nil)
