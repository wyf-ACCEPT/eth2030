// freezer_table.go implements an ancient data table with snappy-style
// compression, supporting append-only indexed storage for frozen chain data.
// Each FreezerTableManager manages a single logical table with an index file
// for O(1) item retrieval and a data file with optional lightweight compression.
package rawdb

import (
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// Compression modes for freezer table data.
const (
	CompressionNone    = 0
	CompressionDeflate = 1
)

// FreezerTableConfig configures a single freezer table.
type FreezerTableConfig struct {
	Dir         string // directory for data and index files
	Name        string // table name (used for filenames)
	ReadOnly    bool
	Compression int // CompressionNone or CompressionDeflate
}

// FreezerTableManager manages a single append-only indexed table for frozen
// chain data. It provides random access reads, sequential appends, and
// truncation support for reorgs. All methods are safe for concurrent use.
type FreezerTableManager struct {
	mu     sync.RWMutex
	config FreezerTableConfig

	dataFile  *os.File
	indexFile *os.File

	tail      uint64 // first valid item
	head      uint64 // next item number to be written
	indexBase uint64 // item number at index position 0
	dataSize  uint64 // current data file size in bytes

	closed bool
}

// OpenFreezerTable opens or creates a freezer table at the configured path.
func OpenFreezerTable(config FreezerTableConfig) (*FreezerTableManager, error) {
	if err := os.MkdirAll(config.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("freezer_table: mkdir: %w", err)
	}

	flags := os.O_RDWR | os.O_CREATE
	if config.ReadOnly {
		flags = os.O_RDONLY
	}

	dataPath := filepath.Join(config.Dir, config.Name+".cdat")
	indexPath := filepath.Join(config.Dir, config.Name+".cidx")

	dataFile, err := os.OpenFile(dataPath, flags, 0o644)
	if err != nil {
		return nil, fmt.Errorf("freezer_table: open data %s: %w", config.Name, err)
	}
	indexFile, err := os.OpenFile(indexPath, flags, 0o644)
	if err != nil {
		dataFile.Close()
		return nil, fmt.Errorf("freezer_table: open index %s: %w", config.Name, err)
	}

	dataStat, _ := dataFile.Stat()
	indexStat, _ := indexFile.Stat()

	var dataSize uint64
	var indexCount uint64
	if dataStat != nil {
		dataSize = uint64(dataStat.Size())
	}
	if indexStat != nil && indexStat.Size() > 0 {
		indexCount = uint64(indexStat.Size()) / ftIndexEntrySize
	}

	return &FreezerTableManager{
		config:    config,
		dataFile:  dataFile,
		indexFile: indexFile,
		tail:      0,
		head:      indexCount,
		indexBase: 0,
		dataSize:  dataSize,
	}, nil
}

// ftIndexEntrySize is the size of a compressed table index entry.
// 8 bytes offset + 4 bytes compressed length + 4 bytes uncompressed length.
const ftIndexEntrySize = 16

// ftIndexEntry is an index entry for the compressed table.
type ftIndexEntry struct {
	Offset          uint64
	CompressedLen   uint32
	UncompressedLen uint32
}

func (e *ftIndexEntry) encode() []byte {
	buf := make([]byte, ftIndexEntrySize)
	binary.BigEndian.PutUint64(buf[0:8], e.Offset)
	binary.BigEndian.PutUint32(buf[8:12], e.CompressedLen)
	binary.BigEndian.PutUint32(buf[12:16], e.UncompressedLen)
	return buf
}

func decodeFTIndexEntry(data []byte) ftIndexEntry {
	return ftIndexEntry{
		Offset:          binary.BigEndian.Uint64(data[0:8]),
		CompressedLen:   binary.BigEndian.Uint32(data[8:12]),
		UncompressedLen: binary.BigEndian.Uint32(data[12:16]),
	}
}

// Append adds an item to the end of the table. The item must have the
// expected sequential number (equal to Head()).
func (ft *FreezerTableManager) Append(item uint64, data []byte) error {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	if ft.closed {
		return ErrFreezerClosed
	}
	if ft.config.ReadOnly {
		return ErrFreezerReadOnly
	}
	if item != ft.head {
		return fmt.Errorf("%w: expected item %d, got %d", ErrFreezerNotSequential, ft.head, item)
	}

	// Compress if configured.
	stored, uncompLen := ft.compress(data)

	offset := ft.dataSize
	if _, err := ft.dataFile.WriteAt(stored, int64(offset)); err != nil {
		return fmt.Errorf("freezer_table: write data: %w", err)
	}

	entry := ftIndexEntry{
		Offset:          offset,
		CompressedLen:   uint32(len(stored)),
		UncompressedLen: uint32(uncompLen),
	}
	physIdx := ft.head - ft.indexBase
	if _, err := ft.indexFile.WriteAt(entry.encode(), int64(physIdx)*ftIndexEntrySize); err != nil {
		return fmt.Errorf("freezer_table: write index: %w", err)
	}

	ft.head++
	ft.dataSize += uint64(len(stored))
	return nil
}

// AppendBatch adds multiple sequential items starting at the given number.
func (ft *FreezerTableManager) AppendBatch(start uint64, items [][]byte) error {
	for i, data := range items {
		if err := ft.Append(start+uint64(i), data); err != nil {
			return err
		}
	}
	return nil
}

// Retrieve reads a single item by its number.
func (ft *FreezerTableManager) Retrieve(item uint64) ([]byte, error) {
	ft.mu.RLock()
	defer ft.mu.RUnlock()

	if ft.closed {
		return nil, ErrFreezerClosed
	}
	if ft.head <= ft.tail || item < ft.tail || item >= ft.head {
		return nil, fmt.Errorf("%w: item %d (range [%d, %d))", ErrFreezerOutOfBounds, item, ft.tail, ft.head)
	}

	physIdx := item - ft.indexBase
	idxBuf := make([]byte, ftIndexEntrySize)
	if _, err := ft.indexFile.ReadAt(idxBuf, int64(physIdx)*ftIndexEntrySize); err != nil {
		return nil, fmt.Errorf("freezer_table: read index: %w", err)
	}
	entry := decodeFTIndexEntry(idxBuf)

	if entry.CompressedLen == 0 {
		return []byte{}, nil
	}

	compressed := make([]byte, entry.CompressedLen)
	if _, err := ft.dataFile.ReadAt(compressed, int64(entry.Offset)); err != nil {
		return nil, fmt.Errorf("freezer_table: read data: %w", err)
	}

	return ft.decompress(compressed, entry.UncompressedLen)
}

// RetrieveRange reads multiple sequential items.
func (ft *FreezerTableManager) RetrieveRange(start, count uint64) ([][]byte, error) {
	results := make([][]byte, 0, count)
	for i := uint64(0); i < count; i++ {
		data, err := ft.Retrieve(start + i)
		if err != nil {
			return results, err
		}
		results = append(results, data)
	}
	return results, nil
}

// Head returns the next item number to be written.
func (ft *FreezerTableManager) Head() uint64 {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	return ft.head
}

// Tail returns the first valid item number.
func (ft *FreezerTableManager) Tail() uint64 {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	return ft.tail
}

// Count returns the number of valid items.
func (ft *FreezerTableManager) Count() uint64 {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	if ft.head <= ft.tail {
		return 0
	}
	return ft.head - ft.tail
}

// DataSize returns the total data file size in bytes.
func (ft *FreezerTableManager) DataSize() uint64 {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	return ft.dataSize
}

// TruncateHead removes items from the end, keeping items below the given number.
func (ft *FreezerTableManager) TruncateHead(below uint64) error {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	if ft.closed {
		return ErrFreezerClosed
	}
	if ft.config.ReadOnly {
		return ErrFreezerReadOnly
	}
	if below >= ft.head {
		return nil // nothing to truncate
	}
	if below <= ft.tail {
		// Truncate everything.
		ft.head = ft.tail
		ft.indexBase = ft.tail
		ft.dataSize = 0
		ft.dataFile.Truncate(0)
		ft.indexFile.Truncate(0)
		return nil
	}

	// Keep items [tail, below).
	keepPhys := below - ft.indexBase
	if keepPhys > 0 {
		idxBuf := make([]byte, ftIndexEntrySize)
		if _, err := ft.indexFile.ReadAt(idxBuf, int64(keepPhys-1)*ftIndexEntrySize); err != nil {
			return fmt.Errorf("freezer_table: truncate head read index: %w", err)
		}
		entry := decodeFTIndexEntry(idxBuf)
		newDataSize := entry.Offset + uint64(entry.CompressedLen)
		ft.dataFile.Truncate(int64(newDataSize))
		ft.dataSize = newDataSize
	} else {
		ft.dataFile.Truncate(0)
		ft.dataSize = 0
	}
	ft.indexFile.Truncate(int64(keepPhys) * ftIndexEntrySize)
	ft.head = below
	return nil
}

// TruncateTail logically removes items from the beginning by advancing the
// tail pointer. The physical files are not compacted.
func (ft *FreezerTableManager) TruncateTail(newTail uint64) error {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	if ft.closed {
		return ErrFreezerClosed
	}
	if ft.config.ReadOnly {
		return ErrFreezerReadOnly
	}
	if newTail <= ft.tail {
		return nil // already past this point
	}
	if newTail >= ft.head {
		ft.tail = newTail
		ft.head = newTail
		ft.indexBase = newTail
		ft.dataSize = 0
		ft.dataFile.Truncate(0)
		ft.indexFile.Truncate(0)
		return nil
	}
	ft.tail = newTail
	return nil
}

// Sync flushes data and index files to disk.
func (ft *FreezerTableManager) Sync() error {
	ft.mu.RLock()
	defer ft.mu.RUnlock()

	if ft.closed {
		return ErrFreezerClosed
	}
	if err := ft.dataFile.Sync(); err != nil {
		return err
	}
	return ft.indexFile.Sync()
}

// Close shuts down the table.
func (ft *FreezerTableManager) Close() error {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	if ft.closed {
		return nil
	}
	ft.closed = true

	var firstErr error
	if err := ft.dataFile.Close(); err != nil {
		firstErr = err
	}
	if err := ft.indexFile.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// FreezerTableStats holds statistics about a FreezerTableManager.
type FreezerTableStats struct {
	Name            string
	Items           uint64
	Tail            uint64
	Head            uint64
	DataBytes       uint64
	IndexBytes      uint64
	CompressionMode int
}

// Stats returns statistics about this table.
func (ft *FreezerTableManager) Stats() FreezerTableStats {
	ft.mu.RLock()
	defer ft.mu.RUnlock()

	items := uint64(0)
	if ft.head > ft.tail {
		items = ft.head - ft.tail
	}
	indexBytes := uint64(0)
	if ft.head > ft.indexBase {
		indexBytes = (ft.head - ft.indexBase) * ftIndexEntrySize
	}
	return FreezerTableStats{
		Name:            ft.config.Name,
		Items:           items,
		Tail:            ft.tail,
		Head:            ft.head,
		DataBytes:       ft.dataSize,
		IndexBytes:      indexBytes,
		CompressionMode: ft.config.Compression,
	}
}

// compress applies the configured compression to data. Returns compressed
// bytes and the original uncompressed length.
func (ft *FreezerTableManager) compress(data []byte) ([]byte, int) {
	if ft.config.Compression == CompressionNone || len(data) == 0 {
		return data, len(data)
	}
	// Use flate (deflate) compression from stdlib.
	var buf []byte
	w, err := flate.NewWriter(nil, flate.BestSpeed)
	if err != nil {
		return data, len(data)
	}
	// Write to a byte buffer.
	pr, pw := io.Pipe()
	go func() {
		w.Reset(pw)
		w.Write(data)
		w.Close()
		pw.Close()
	}()
	buf, _ = io.ReadAll(pr)
	// Only use compressed form if it is actually smaller.
	if len(buf) >= len(data) {
		return data, len(data)
	}
	return buf, len(data)
}

// decompress restores original data from compressed bytes.
func (ft *FreezerTableManager) decompress(data []byte, uncompressedLen uint32) ([]byte, error) {
	if ft.config.Compression == CompressionNone || uint32(len(data)) == uncompressedLen {
		return data, nil
	}
	reader := flate.NewReader(io.NopCloser(
		io.NewSectionReader(
			readerAtFromBytes(data), 0, int64(len(data)),
		),
	))
	defer reader.Close()

	result := make([]byte, 0, uncompressedLen)
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			result = append(result, buf[:n]...)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("freezer_table: decompress: %w", err)
		}
	}
	return result, nil
}

// readerAtFromBytes wraps a byte slice as a ReaderAt.
type bytesReaderAt struct {
	data []byte
}

func readerAtFromBytes(data []byte) *bytesReaderAt {
	return &bytesReaderAt{data: data}
}

func (r *bytesReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(r.data)) {
		return 0, io.EOF
	}
	n := copy(p, r.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}
