// freezer.go implements an ancient data freezer that moves old blocks and
// receipts from the active database into append-only flat files for efficient
// long-term storage. Each table (headers, bodies, receipts, hashes) is stored
// as a separate flat file with a fixed-size index for O(1) item retrieval.
package rawdb

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Freezer table names.
const (
	FreezerHeaderTable  = "headers"
	FreezerBodyTable    = "bodies"
	FreezerReceiptTable = "receipts"
	FreezerHashTable    = "hashes"
)

// Index entry size: 8 bytes offset + 4 bytes length.
const indexEntrySize = 12

// Freezer errors.
var (
	ErrFreezerClosed        = errors.New("freezer: database closed")
	ErrFreezerReadOnly      = errors.New("freezer: read-only mode")
	ErrFreezerOutOfBounds   = errors.New("freezer: item number out of bounds")
	ErrFreezerEmpty         = errors.New("freezer: table is empty")
	ErrFreezerCorrupted     = errors.New("freezer: data corrupted")
	ErrFreezerNotSequential = errors.New("freezer: items must be appended sequentially")
)

// indexEntry is a single entry in the freezer table index.
type indexEntry struct {
	Offset uint64 // byte offset in the data file
	Length uint32 // byte length of the item
}

// encode serializes the index entry to bytes.
func (e *indexEntry) encode() []byte {
	buf := make([]byte, indexEntrySize)
	binary.BigEndian.PutUint64(buf[0:8], e.Offset)
	binary.BigEndian.PutUint32(buf[8:12], e.Length)
	return buf
}

// decodeIndexEntry deserializes an index entry from bytes.
func decodeIndexEntry(data []byte) indexEntry {
	return indexEntry{
		Offset: binary.BigEndian.Uint64(data[0:8]),
		Length: binary.BigEndian.Uint32(data[8:12]),
	}
}

// freezerTable is a single append-only flat file with an index.
//
// Item numbering:
//   - tail: first logically valid item number
//   - head: one past the last valid item number (next to be appended)
//   - indexBase: item number that corresponds to index file position 0
//
// The physical index position for item N is (N - indexBase).
// Valid range: [tail, head). After tail truncation, indexBase stays the
// same (we do not compact), so entries before tail remain in the index
// but are considered invalid. After head truncation, the index is
// physically truncated.
type freezerTable struct {
	name      string
	dataFile  *os.File
	indexFile *os.File
	tail      uint64 // first valid item number
	head      uint64 // next item number to append
	indexBase uint64 // item number at index position 0
	dataSize  uint64 // current byte size of data file
}

// validItems returns the number of logically valid items.
func (t *freezerTable) validItems() uint64 {
	if t.head <= t.tail {
		return 0
	}
	return t.head - t.tail
}

// indexEntries returns the number of physical entries in the index.
func (t *freezerTable) indexEntries() uint64 {
	return t.head - t.indexBase
}

// Freezer manages append-only flat file storage for ancient blockchain data.
type Freezer struct {
	mu       sync.RWMutex
	dir      string
	tables   map[string]*freezerTable
	frozen   uint64 // highest frozen item (head of header table)
	tail     uint64 // first available block number
	closed   bool
	readOnly bool
}

// NewFreezer creates or opens a freezer database at the given directory.
func NewFreezer(dir string, readOnly bool) (*Freezer, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("freezer: mkdir: %w", err)
	}

	f := &Freezer{
		dir:      dir,
		tables:   make(map[string]*freezerTable),
		readOnly: readOnly,
	}

	tableNames := []string{
		FreezerHeaderTable, FreezerBodyTable,
		FreezerReceiptTable, FreezerHashTable,
	}
	for _, name := range tableNames {
		table, err := openFreezerTable(dir, name, readOnly)
		if err != nil {
			f.Close()
			return nil, err
		}
		f.tables[name] = table
	}

	// Determine frozen count from the header table.
	if ht, ok := f.tables[FreezerHeaderTable]; ok {
		f.frozen = ht.head
		f.tail = ht.tail
	}

	return f, nil
}

// openFreezerTable opens or creates a freezer table.
func openFreezerTable(dir, name string, readOnly bool) (*freezerTable, error) {
	flags := os.O_RDWR | os.O_CREATE
	if readOnly {
		flags = os.O_RDONLY
	}

	dataPath := filepath.Join(dir, name+".dat")
	indexPath := filepath.Join(dir, name+".idx")

	dataFile, err := os.OpenFile(dataPath, flags, 0o644)
	if err != nil {
		return nil, fmt.Errorf("freezer: open data %s: %w", name, err)
	}

	indexFile, err := os.OpenFile(indexPath, flags, 0o644)
	if err != nil {
		dataFile.Close()
		return nil, fmt.Errorf("freezer: open index %s: %w", name, err)
	}

	dataStat, _ := dataFile.Stat()
	indexStat, _ := indexFile.Stat()

	dataSize := uint64(0)
	indexCount := uint64(0)
	if dataStat != nil {
		dataSize = uint64(dataStat.Size())
	}
	if indexStat != nil && indexStat.Size() > 0 {
		indexCount = uint64(indexStat.Size()) / indexEntrySize
	}

	return &freezerTable{
		name:      name,
		dataFile:  dataFile,
		indexFile: indexFile,
		tail:      0,
		head:      indexCount, // items [0, indexCount)
		indexBase: 0,
		dataSize:  dataSize,
	}, nil
}

// Frozen returns the highest frozen item number.
func (f *Freezer) Frozen() uint64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.frozen
}

// Tail returns the first available block number.
func (f *Freezer) Tail() uint64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.tail
}

// HasTable returns true if the freezer has the named table.
func (f *Freezer) HasTable(name string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.tables[name]
	return ok
}

// Freeze appends a single item to the specified table.
func (f *Freezer) Freeze(table string, item uint64, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return ErrFreezerClosed
	}
	if f.readOnly {
		return ErrFreezerReadOnly
	}

	t, ok := f.tables[table]
	if !ok {
		return fmt.Errorf("freezer: unknown table %q", table)
	}

	if item != t.head {
		return fmt.Errorf("%w: expected item %d, got %d", ErrFreezerNotSequential, t.head, item)
	}

	return f.appendToTable(t, data)
}

// FreezeRange appends multiple consecutive items.
func (f *Freezer) FreezeRange(table string, start uint64, items [][]byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return ErrFreezerClosed
	}
	if f.readOnly {
		return ErrFreezerReadOnly
	}

	t, ok := f.tables[table]
	if !ok {
		return fmt.Errorf("freezer: unknown table %q", table)
	}

	if start != t.head {
		return fmt.Errorf("%w: expected start %d, got %d", ErrFreezerNotSequential, t.head, start)
	}

	for _, data := range items {
		if err := f.appendToTable(t, data); err != nil {
			return err
		}
	}

	if ht, ok := f.tables[FreezerHeaderTable]; ok {
		f.frozen = ht.head
	}

	return nil
}

// appendToTable writes data and index entry.
func (f *Freezer) appendToTable(t *freezerTable, data []byte) error {
	offset := t.dataSize
	if _, err := t.dataFile.WriteAt(data, int64(offset)); err != nil {
		return fmt.Errorf("freezer: write data %s: %w", t.name, err)
	}

	entry := indexEntry{Offset: offset, Length: uint32(len(data))}
	physIdx := t.head - t.indexBase
	if _, err := t.indexFile.WriteAt(entry.encode(), int64(physIdx)*indexEntrySize); err != nil {
		return fmt.Errorf("freezer: write index %s: %w", t.name, err)
	}

	t.head++
	t.dataSize += uint64(len(data))
	return nil
}

// Retrieve reads a single item by item number.
func (f *Freezer) Retrieve(table string, item uint64) ([]byte, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if f.closed {
		return nil, ErrFreezerClosed
	}

	t, ok := f.tables[table]
	if !ok {
		return nil, fmt.Errorf("freezer: unknown table %q", table)
	}

	if t.validItems() == 0 {
		return nil, ErrFreezerEmpty
	}

	if item < t.tail || item >= t.head {
		return nil, fmt.Errorf("%w: item %d (range [%d, %d))", ErrFreezerOutOfBounds, item, t.tail, t.head)
	}

	// Physical index position.
	physIdx := item - t.indexBase

	idxBuf := make([]byte, indexEntrySize)
	if _, err := t.indexFile.ReadAt(idxBuf, int64(physIdx)*indexEntrySize); err != nil {
		return nil, fmt.Errorf("freezer: read index %s: %w", t.name, err)
	}
	entry := decodeIndexEntry(idxBuf)

	if entry.Length == 0 {
		return []byte{}, nil
	}

	data := make([]byte, entry.Length)
	if _, err := t.dataFile.ReadAt(data, int64(entry.Offset)); err != nil {
		return nil, fmt.Errorf("freezer: read data %s: %w", t.name, err)
	}

	return data, nil
}

// TruncateHead removes items from the end, keeping items below the given number.
func (f *Freezer) TruncateHead(item uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return ErrFreezerClosed
	}
	if f.readOnly {
		return ErrFreezerReadOnly
	}

	for _, t := range f.tables {
		if item >= t.head {
			continue
		}
		if item <= t.tail {
			// Truncate everything.
			t.head = t.tail
			t.dataSize = 0
			t.indexBase = t.tail
			t.dataFile.Truncate(0)
			t.indexFile.Truncate(0)
			continue
		}

		// Keep items [tail, item). Physical index entries to keep = item - indexBase.
		keepPhys := item - t.indexBase
		if keepPhys > 0 {
			idxBuf := make([]byte, indexEntrySize)
			if _, err := t.indexFile.ReadAt(idxBuf, int64(keepPhys-1)*indexEntrySize); err != nil {
				return fmt.Errorf("freezer: truncate head read index %s: %w", t.name, err)
			}
			entry := decodeIndexEntry(idxBuf)
			newDataSize := entry.Offset + uint64(entry.Length)
			t.dataFile.Truncate(int64(newDataSize))
			t.dataSize = newDataSize
		} else {
			t.dataFile.Truncate(0)
			t.dataSize = 0
		}
		t.indexFile.Truncate(int64(keepPhys) * indexEntrySize)
		t.head = item
	}

	if ht, ok := f.tables[FreezerHeaderTable]; ok {
		f.frozen = ht.head
	}
	return nil
}

// TruncateTail removes items from the beginning, discarding items
// before the given item number. This does not compact the files;
// it only adjusts the logical tail pointer.
func (f *Freezer) TruncateTail(item uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return ErrFreezerClosed
	}
	if f.readOnly {
		return ErrFreezerReadOnly
	}

	for _, t := range f.tables {
		if item <= t.tail {
			continue
		}
		if item >= t.head {
			// Truncate everything.
			t.tail = item
			t.head = item
			t.indexBase = item
			t.dataSize = 0
			t.dataFile.Truncate(0)
			t.indexFile.Truncate(0)
			continue
		}
		// Only move the logical tail; do not compact the index/data files.
		t.tail = item
	}

	f.tail = item
	return nil
}

// Sync flushes all files to disk.
func (f *Freezer) Sync() error {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if f.closed {
		return ErrFreezerClosed
	}

	for _, t := range f.tables {
		if err := t.dataFile.Sync(); err != nil {
			return fmt.Errorf("freezer: sync data %s: %w", t.name, err)
		}
		if err := t.indexFile.Sync(); err != nil {
			return fmt.Errorf("freezer: sync index %s: %w", t.name, err)
		}
	}
	return nil
}

// Close shuts down the freezer.
func (f *Freezer) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return nil
	}
	f.closed = true

	var firstErr error
	for _, t := range f.tables {
		if err := t.dataFile.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		if err := t.indexFile.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// TableSize returns the total byte size (data + index) of a table.
func (f *Freezer) TableSize(table string) (uint64, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	t, ok := f.tables[table]
	if !ok {
		return 0, fmt.Errorf("freezer: unknown table %q", table)
	}
	return t.dataSize + t.indexEntries()*indexEntrySize, nil
}

// ItemCount returns the number of valid items in a table.
func (f *Freezer) ItemCount(table string) (uint64, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	t, ok := f.tables[table]
	if !ok {
		return 0, fmt.Errorf("freezer: unknown table %q", table)
	}
	return t.validItems(), nil
}
