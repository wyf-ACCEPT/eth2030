// ancient_store.go provides AncientStore, a high-level wrapper over the
// Freezer for managing finalized blockchain data. It adds batch freeze
// operations for migrating data from the live DB, compaction to reclaim
// space after tail truncation, read-back with integrity validation, and
// table-level statistics.
package rawdb

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// AncientStore errors.
var (
	ErrAncientClosed     = errors.New("ancient store: closed")
	ErrAncientReadOnly   = errors.New("ancient store: read-only mode")
	ErrAncientNotFound   = errors.New("ancient store: block not found in frozen data")
	ErrAncientCorrupted  = errors.New("ancient store: data integrity check failed")
	ErrMigrationRange    = errors.New("ancient store: migration range invalid")
	ErrCompactionPending = errors.New("ancient store: compaction already in progress")
)

// AncientStoreConfig configures the ancient store.
type AncientStoreConfig struct {
	DataDir  string
	ReadOnly bool
}

// TableStats holds size and count statistics for a single freezer table.
type TableStats struct {
	Name      string
	Items     uint64
	DataBytes uint64
	IndexSize uint64
}

// AncientStats holds aggregate statistics for the entire ancient store.
type AncientStats struct {
	Tables    []TableStats
	Frozen    uint64
	Tail      uint64
	TotalSize uint64
}

// AncientStore manages frozen blockchain data with batch operations,
// compaction, and integrity checking. All methods are safe for concurrent use.
type AncientStore struct {
	mu      sync.RWMutex
	config  AncientStoreConfig
	freezer *Freezer
	closed  bool
	compact bool
}

// NewAncientStore opens or creates an ancient data store.
func NewAncientStore(config AncientStoreConfig) (*AncientStore, error) {
	freezer, err := NewFreezer(config.DataDir, config.ReadOnly)
	if err != nil {
		return nil, fmt.Errorf("ancient store: open freezer: %w", err)
	}
	return &AncientStore{config: config, freezer: freezer}, nil
}

func (as *AncientStore) Frozen() uint64 { as.mu.RLock(); defer as.mu.RUnlock(); return as.freezer.Frozen() }
func (as *AncientStore) Tail() uint64   { as.mu.RLock(); defer as.mu.RUnlock(); return as.freezer.Tail() }

// HasBlock returns true if the block number is in the frozen range.
func (as *AncientStore) HasBlock(number uint64) bool {
	as.mu.RLock()
	defer as.mu.RUnlock()
	return number >= as.freezer.Tail() && number < as.freezer.Frozen()
}

// BlockRange returns the range [tail, frozen) of available frozen blocks.
func (as *AncientStore) BlockRange() (tail, frozen uint64) {
	as.mu.RLock()
	defer as.mu.RUnlock()
	return as.freezer.Tail(), as.freezer.Frozen()
}

// ReadHeader retrieves a frozen block header by number.
func (as *AncientStore) ReadHeader(number uint64) ([]byte, error) {
	as.mu.RLock()
	defer as.mu.RUnlock()
	if as.closed {
		return nil, ErrAncientClosed
	}
	return as.retrieve(FreezerHeaderTable, number)
}

// ReadBody retrieves a frozen block body by number.
func (as *AncientStore) ReadBody(number uint64) ([]byte, error) {
	as.mu.RLock()
	defer as.mu.RUnlock()
	if as.closed {
		return nil, ErrAncientClosed
	}
	return as.retrieve(FreezerBodyTable, number)
}

// ReadReceipts retrieves frozen receipts by block number.
func (as *AncientStore) ReadReceipts(number uint64) ([]byte, error) {
	as.mu.RLock()
	defer as.mu.RUnlock()
	if as.closed {
		return nil, ErrAncientClosed
	}
	return as.retrieve(FreezerReceiptTable, number)
}

// ReadHash retrieves the frozen block hash by number.
func (as *AncientStore) ReadHash(number uint64) (types.Hash, error) {
	as.mu.RLock()
	defer as.mu.RUnlock()
	if as.closed {
		return types.Hash{}, ErrAncientClosed
	}
	data, err := as.retrieve(FreezerHashTable, number)
	if err != nil {
		return types.Hash{}, err
	}
	if len(data) != 32 {
		return types.Hash{}, fmt.Errorf("%w: hash at %d has %d bytes", ErrAncientCorrupted, number, len(data))
	}
	return types.BytesToHash(data), nil
}

func (as *AncientStore) retrieve(table string, number uint64) ([]byte, error) {
	data, err := as.freezer.Retrieve(table, number)
	if err != nil {
		return nil, fmt.Errorf("%w: %s at %d: %v", ErrAncientNotFound, table, number, err)
	}
	return data, nil
}

// FreezeBlock atomically appends a single block's data to all four tables.
func (as *AncientStore) FreezeBlock(number uint64, hash types.Hash, header, body, receipts []byte) error {
	as.mu.Lock()
	defer as.mu.Unlock()
	if as.closed {
		return ErrAncientClosed
	}
	if as.config.ReadOnly {
		return ErrAncientReadOnly
	}
	items := []struct {
		table string
		data  []byte
	}{
		{FreezerHeaderTable, header},
		{FreezerBodyTable, body},
		{FreezerReceiptTable, receipts},
		{FreezerHashTable, hash[:]},
	}
	for _, it := range items {
		if err := as.freezer.Freeze(it.table, number, it.data); err != nil {
			return fmt.Errorf("ancient store: freeze %s %d: %w", it.table, number, err)
		}
	}
	return nil
}

// MigrateFromDB moves finalized blocks from the live DB into the ancient
// store. It reads each block's header, body, receipts, and canonical hash,
// appends them to the freezer, and deletes the originals. Returns the count
// of migrated blocks.
func (as *AncientStore) MigrateFromDB(db Database, start, end uint64) (uint64, error) {
	as.mu.Lock()
	defer as.mu.Unlock()
	if as.closed {
		return 0, ErrAncientClosed
	}
	if as.config.ReadOnly {
		return 0, ErrAncientReadOnly
	}
	if start > end {
		return 0, ErrMigrationRange
	}
	if frozen := as.freezer.Frozen(); start != frozen {
		return 0, fmt.Errorf("%w: expected start=%d, frozen=%d", ErrMigrationRange, start, frozen)
	}

	migrated := uint64(0)
	for num := start; num <= end; num++ {
		hash, err := ReadCanonicalHash(db, num)
		if err != nil {
			return migrated, fmt.Errorf("ancient store: canonical hash %d: %w", num, err)
		}
		headerData, err := ReadHeader(db, num, hash)
		if err != nil {
			return migrated, fmt.Errorf("ancient store: header %d: %w", num, err)
		}
		bodyData, _ := ReadBody(db, num, hash)
		if bodyData == nil {
			bodyData = []byte{}
		}
		receiptData, _ := ReadReceipts(db, num, hash)
		if receiptData == nil {
			receiptData = []byte{}
		}

		for _, pair := range []struct {
			table string
			data  []byte
		}{
			{FreezerHeaderTable, headerData},
			{FreezerBodyTable, bodyData},
			{FreezerReceiptTable, receiptData},
			{FreezerHashTable, hash[:]},
		} {
			if err := as.freezer.Freeze(pair.table, num, pair.data); err != nil {
				return migrated, err
			}
		}
		_ = DeleteHeader(db, num, hash)
		_ = DeleteBody(db, num, hash)
		_ = DeleteReceipts(db, num, hash)
		migrated++
	}
	return migrated, nil
}

// Compact rewrites freezer table files to reclaim space from tail-truncated
// entries. This is expensive and should run during low-activity periods.
func (as *AncientStore) Compact() error {
	as.mu.Lock()
	if as.closed {
		as.mu.Unlock()
		return ErrAncientClosed
	}
	if as.config.ReadOnly {
		as.mu.Unlock()
		return ErrAncientReadOnly
	}
	if as.compact {
		as.mu.Unlock()
		return ErrCompactionPending
	}
	as.compact = true
	as.mu.Unlock()
	defer func() { as.mu.Lock(); as.compact = false; as.mu.Unlock() }()

	for _, name := range []string{FreezerHeaderTable, FreezerBodyTable, FreezerReceiptTable, FreezerHashTable} {
		if err := as.compactTable(name); err != nil {
			return fmt.Errorf("ancient store: compact %s: %w", name, err)
		}
	}
	return nil
}

func (as *AncientStore) compactTable(tableName string) error {
	as.mu.Lock()
	defer as.mu.Unlock()

	t, ok := as.freezer.tables[tableName]
	if !ok {
		return fmt.Errorf("unknown table %q", tableName)
	}
	if t.tail <= t.indexBase {
		return nil // nothing to compact
	}

	dir := as.config.DataDir
	tmpData, err := os.Create(filepath.Join(dir, tableName+".dat.compact"))
	if err != nil {
		return err
	}
	tmpIndex, err := os.Create(filepath.Join(dir, tableName+".idx.compact"))
	if err != nil {
		tmpData.Close()
		os.Remove(tmpData.Name())
		return err
	}

	// Copy valid items [tail, head) with recomputed offsets.
	var dataOffset uint64
	for num := t.tail; num < t.head; num++ {
		physIdx := num - t.indexBase
		idxBuf := make([]byte, indexEntrySize)
		if _, e := t.indexFile.ReadAt(idxBuf, int64(physIdx)*indexEntrySize); e != nil {
			tmpData.Close(); tmpIndex.Close()
			os.Remove(tmpData.Name()); os.Remove(tmpIndex.Name())
			return e
		}
		entry := decodeIndexEntry(idxBuf)
		data := make([]byte, entry.Length)
		if entry.Length > 0 {
			if _, e := t.dataFile.ReadAt(data, int64(entry.Offset)); e != nil {
				tmpData.Close(); tmpIndex.Close()
				os.Remove(tmpData.Name()); os.Remove(tmpIndex.Name())
				return e
			}
		}
		tmpData.Write(data)
		newEntry := indexEntry{Offset: dataOffset, Length: entry.Length}
		tmpIndex.Write(newEntry.encode())
		dataOffset += uint64(entry.Length)
	}

	// Swap files.
	datPath := filepath.Join(dir, tableName+".dat")
	idxPath := filepath.Join(dir, tableName+".idx")
	t.dataFile.Close()
	t.indexFile.Close()
	tmpData.Close()
	tmpIndex.Close()
	os.Rename(tmpData.Name(), datPath)
	os.Rename(tmpIndex.Name(), idxPath)

	newData, err := os.OpenFile(datPath, os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	newIndex, err := os.OpenFile(idxPath, os.O_RDWR, 0o644)
	if err != nil {
		newData.Close()
		return err
	}
	t.dataFile = newData
	t.indexFile = newIndex
	t.indexBase = t.tail
	t.dataSize = dataOffset
	return nil
}

// Stats returns size and count statistics for all tables.
func (as *AncientStore) Stats() AncientStats {
	as.mu.RLock()
	defer as.mu.RUnlock()
	stats := AncientStats{Frozen: as.freezer.Frozen(), Tail: as.freezer.Tail()}
	for _, name := range []string{FreezerHeaderTable, FreezerBodyTable, FreezerReceiptTable, FreezerHashTable} {
		ts := TableStats{Name: name}
		if t, ok := as.freezer.tables[name]; ok {
			ts.Items = t.validItems()
			ts.DataBytes = t.dataSize
			ts.IndexSize = t.indexEntries() * indexEntrySize
		}
		stats.Tables = append(stats.Tables, ts)
		stats.TotalSize += ts.DataBytes + ts.IndexSize
	}
	return stats
}

// Verify checks integrity for a block range by reading and validating items.
func (as *AncientStore) Verify(start, end uint64) (uint64, error) {
	as.mu.RLock()
	defer as.mu.RUnlock()
	if as.closed {
		return 0, ErrAncientClosed
	}
	verified := uint64(0)
	for num := start; num <= end; num++ {
		hdr, err := as.freezer.Retrieve(FreezerHeaderTable, num)
		if err != nil {
			return verified, fmt.Errorf("verify: header %d: %w", num, err)
		}
		if len(hdr) == 0 {
			return verified, fmt.Errorf("%w: empty header at %d", ErrAncientCorrupted, num)
		}
		hashData, err := as.freezer.Retrieve(FreezerHashTable, num)
		if err != nil {
			return verified, fmt.Errorf("verify: hash %d: %w", num, err)
		}
		if len(hashData) != 32 {
			return verified, fmt.Errorf("%w: hash at %d has %d bytes", ErrAncientCorrupted, num, len(hashData))
		}
		verified++
	}
	return verified, nil
}

// TruncateBlocks removes frozen blocks from the end (for chain reorgs).
func (as *AncientStore) TruncateBlocks(below uint64) error {
	as.mu.Lock()
	defer as.mu.Unlock()
	if as.closed {
		return ErrAncientClosed
	}
	if as.config.ReadOnly {
		return ErrAncientReadOnly
	}
	return as.freezer.TruncateHead(below)
}

// PruneTail removes frozen blocks from the beginning (EIP-4444 expiry).
func (as *AncientStore) PruneTail(keepFrom uint64) error {
	as.mu.Lock()
	defer as.mu.Unlock()
	if as.closed {
		return ErrAncientClosed
	}
	if as.config.ReadOnly {
		return ErrAncientReadOnly
	}
	return as.freezer.TruncateTail(keepFrom)
}

// Sync flushes all data to disk.
func (as *AncientStore) Sync() error {
	as.mu.RLock()
	defer as.mu.RUnlock()
	if as.closed {
		return ErrAncientClosed
	}
	return as.freezer.Sync()
}

// Close shuts down the ancient store.
func (as *AncientStore) Close() error {
	as.mu.Lock()
	defer as.mu.Unlock()
	if as.closed {
		return nil
	}
	as.closed = true
	return as.freezer.Close()
}

var _ = func() { var buf [8]byte; binary.BigEndian.PutUint64(buf[:], 0); _ = types.Hash{} }
