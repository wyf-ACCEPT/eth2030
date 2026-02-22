// tx_jrnl.go implements TxJrnl, a rotating transaction journal with periodic
// flush, corruption recovery, metrics tracking, and RLP-based persistence.
// Unlike the simpler TxJournal, TxJrnl supports configurable auto-flush,
// rotation by entry count or age, and exposes detailed operational metrics.
package txpool

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/rlp"
)

// JrnlConfig configures the TxJrnl behaviour.
type JrnlConfig struct {
	// Path is the file path for the primary journal file.
	Path string

	// FlushCount triggers a sync to disk after this many buffered writes.
	// Zero means flush on every write.
	FlushCount int

	// FlushInterval triggers a sync to disk on a timer. Zero disables.
	FlushInterval time.Duration

	// RotateCount triggers a journal rotation after this many entries.
	// Zero disables count-based rotation.
	RotateCount int

	// RotateAge triggers rotation when the journal file is older than this
	// duration. Zero disables age-based rotation.
	RotateAge time.Duration
}

// DefaultJrnlConfig returns sensible defaults for a transaction journal.
func DefaultJrnlConfig() JrnlConfig {
	return JrnlConfig{
		Path:          "txjournal.rlp",
		FlushCount:    64,
		FlushInterval: 5 * time.Second,
		RotateCount:   8192,
		RotateAge:     30 * time.Minute,
	}
}

// JrnlMetrics exposes operational metrics for the journal.
type JrnlMetrics struct {
	// TotalWrites is the cumulative number of entries written.
	TotalWrites atomic.Int64
	// TotalReplays is the cumulative number of entries successfully replayed.
	TotalReplays atomic.Int64
	// CorruptionCount is the number of corrupt entries skipped during replay.
	CorruptionCount atomic.Int64
	// RotationCount is the number of journal rotations performed.
	RotationCount atomic.Int64
	// FlushCount is the number of explicit disk flushes.
	FlushCount atomic.Int64
	// JournalBytes is the approximate current journal file size.
	JournalBytes atomic.Int64
}

// jrnlEntry is the RLP-encoded representation of a single journal record.
// Fields are exported to satisfy the RLP encoder.
type jrnlEntry struct {
	TxRLP     []byte // RLP-encoded transaction
	Sender    []byte // 20-byte sender address (may be empty)
	Timestamp uint64 // unix timestamp
	Local     bool   // whether the tx was submitted locally
}

// JrnlError wraps corruption errors with offset information.
type JrnlError struct {
	Offset int64
	Err    error
}

func (e *JrnlError) Error() string {
	return "journal corruption at offset " + itoa(e.Offset) + ": " + e.Err.Error()
}

func (e *JrnlError) Unwrap() error { return e.Err }

// itoa is a simple int64 to string conversion without fmt dependency.
func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	buf := make([]byte, 0, 20)
	for v > 0 {
		buf = append(buf, byte('0'+v%10))
		v /= 10
	}
	if neg {
		buf = append(buf, '-')
	}
	// Reverse.
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}

// TxJrnl is a rotating transaction journal that persists pending transactions
// to disk for crash recovery. Each entry is length-prefixed RLP, enabling
// corruption recovery by skipping invalid frames. The journal supports
// periodic automatic flushing and rotation based on entry count or file age.
type TxJrnl struct {
	mu     sync.Mutex
	config JrnlConfig
	file   *os.File
	closed bool

	// writesSinceFlush tracks unflushed writes for batch syncing.
	writesSinceFlush int

	// entriesSinceRotate tracks writes since last rotation.
	entriesSinceRotate int

	// createdAt records when the current journal file was opened.
	createdAt time.Time

	// stopFlush signals the background flush goroutine to stop.
	stopFlush chan struct{}

	// Metrics tracks operational statistics.
	Metrics JrnlMetrics
}

// Sentinel errors for TxJrnl.
var (
	ErrJrnlClosed = errors.New("journal is closed")
	ErrJrnlEmpty  = errors.New("journal file is empty")
)

// NewTxJrnl creates and opens a new rotating transaction journal at the
// configured path. The parent directory is created if needed. If a flush
// interval is configured, a background goroutine is started for periodic
// syncing.
func NewTxJrnl(config JrnlConfig) (*TxJrnl, error) {
	dir := filepath.Dir(config.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	f, err := os.OpenFile(config.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}

	j := &TxJrnl{
		config:    config,
		file:      f,
		createdAt: info.ModTime(),
		stopFlush: make(chan struct{}),
	}
	j.Metrics.JournalBytes.Store(info.Size())

	if config.FlushInterval > 0 {
		go j.flushLoop()
	}

	return j, nil
}

// flushLoop periodically syncs the journal file to disk.
func (j *TxJrnl) flushLoop() {
	ticker := time.NewTicker(j.config.FlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			j.Flush()
		case <-j.stopFlush:
			return
		}
	}
}

// Write appends a single transaction to the journal. The transaction is
// RLP-encoded, then wrapped in a length-prefixed frame for corruption
// recovery. If the configured flush count is reached, the journal is
// synced to disk.
func (j *TxJrnl) Write(tx *types.Transaction, local bool) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.closed {
		return ErrJrnlClosed
	}

	data, err := j.encodeEntry(tx, local)
	if err != nil {
		return err
	}

	if _, err := j.file.Write(data); err != nil {
		return err
	}

	j.writesSinceFlush++
	j.entriesSinceRotate++
	j.Metrics.TotalWrites.Add(1)
	j.Metrics.JournalBytes.Add(int64(len(data)))

	// Auto-flush when the configured threshold is reached.
	if j.config.FlushCount > 0 && j.writesSinceFlush >= j.config.FlushCount {
		if err := j.file.Sync(); err != nil {
			return err
		}
		j.writesSinceFlush = 0
		j.Metrics.FlushCount.Add(1)
	}

	return nil
}

// WriteBatch appends multiple transactions to the journal in one call.
func (j *TxJrnl) WriteBatch(txs []*types.Transaction, local bool) error {
	for _, tx := range txs {
		if err := j.Write(tx, local); err != nil {
			return err
		}
	}
	return nil
}

// encodeEntry serializes a transaction into a length-prefixed RLP frame.
// Frame format: [4-byte big-endian length][RLP(jrnlEntry)]
func (j *TxJrnl) encodeEntry(tx *types.Transaction, local bool) ([]byte, error) {
	txRLP, err := tx.EncodeRLP()
	if err != nil {
		return nil, err
	}

	var senderBytes []byte
	if from := tx.Sender(); from != nil {
		senderBytes = from[:]
	}

	entry := jrnlEntry{
		TxRLP:     txRLP,
		Sender:    senderBytes,
		Timestamp: uint64(time.Now().Unix()),
		Local:     local,
	}

	encoded, err := rlp.EncodeToBytes(entry)
	if err != nil {
		return nil, err
	}

	// Length-prefix the entry for framing.
	frame := make([]byte, 4+len(encoded))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(encoded)))
	copy(frame[4:], encoded)

	return frame, nil
}

// Flush syncs any buffered writes to disk.
func (j *TxJrnl) Flush() error {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.closed {
		return ErrJrnlClosed
	}
	if j.writesSinceFlush == 0 {
		return nil
	}

	err := j.file.Sync()
	if err == nil {
		j.writesSinceFlush = 0
		j.Metrics.FlushCount.Add(1)
	}
	return err
}

// Replay reads the journal file and returns all valid transactions. Corrupt
// or undecodable entries are skipped, and the corruption count metric is
// incremented for each. The journal is read from the configured path.
func (j *TxJrnl) Replay() ([]*types.Transaction, error) {
	j.mu.Lock()
	path := j.config.Path
	j.mu.Unlock()

	return ReplayJrnl(path, &j.Metrics)
}

// ReplayJrnl reads a journal file and returns all valid transactions. If
// metrics is non-nil, replay and corruption counts are updated. This is a
// standalone function so it can be used without a live TxJrnl instance.
func ReplayJrnl(path string, metrics *JrnlMetrics) ([]*types.Transaction, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}

	var txs []*types.Transaction
	offset := 0
	for offset < len(data) {
		// Need at least 4 bytes for the length prefix.
		if offset+4 > len(data) {
			if metrics != nil {
				metrics.CorruptionCount.Add(1)
			}
			break
		}

		frameLen := int(binary.BigEndian.Uint32(data[offset : offset+4]))
		offset += 4

		// Sanity check frame length.
		if frameLen <= 0 || frameLen > len(data)-offset {
			if metrics != nil {
				metrics.CorruptionCount.Add(1)
			}
			// Try to skip forward to find the next valid frame.
			offset = scanForNextFrame(data, offset)
			continue
		}

		frameData := data[offset : offset+frameLen]
		offset += frameLen

		var entry jrnlEntry
		if err := rlp.DecodeBytes(frameData, &entry); err != nil {
			if metrics != nil {
				metrics.CorruptionCount.Add(1)
			}
			continue
		}

		tx, err := types.DecodeTxRLP(entry.TxRLP)
		if err != nil {
			if metrics != nil {
				metrics.CorruptionCount.Add(1)
			}
			continue
		}

		// Restore sender cache if present.
		if len(entry.Sender) == types.AddressLength {
			var addr types.Address
			copy(addr[:], entry.Sender)
			tx.SetSender(addr)
		}

		txs = append(txs, tx)
		if metrics != nil {
			metrics.TotalReplays.Add(1)
		}
	}

	return txs, nil
}

// scanForNextFrame scans ahead in data looking for what could be a valid
// frame length prefix. Returns the new offset, or len(data) if none found.
func scanForNextFrame(data []byte, start int) int {
	// Heuristic: try each byte position and check if the length prefix
	// points to a plausible subsequent frame.
	for i := start; i+4 <= len(data); i++ {
		frameLen := int(binary.BigEndian.Uint32(data[i : i+4]))
		if frameLen > 0 && frameLen < 1<<20 && i+4+frameLen <= len(data) {
			return i
		}
	}
	return len(data)
}

// NeedsRotation returns true if the journal should be rotated based on
// the configured count or age thresholds.
func (j *TxJrnl) NeedsRotation() bool {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.closed {
		return false
	}
	if j.config.RotateCount > 0 && j.entriesSinceRotate >= j.config.RotateCount {
		return true
	}
	if j.config.RotateAge > 0 && time.Since(j.createdAt) >= j.config.RotateAge {
		return true
	}
	return false
}

// Rotate replaces the journal file with only the provided pending transactions.
// The old journal is atomically replaced. After rotation, the journal is ready
// for continued writing. The rotation count metric is incremented.
func (j *TxJrnl) Rotate(pending map[types.Address][]*types.Transaction) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.closed {
		return ErrJrnlClosed
	}

	// Sync and close the current file.
	if err := j.file.Sync(); err != nil {
		return err
	}
	if err := j.file.Close(); err != nil {
		return err
	}

	// Write fresh journal to a temp file.
	tmpPath := j.config.Path + ".rotating"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		j.reopenFile()
		return err
	}

	written := 0
	for _, txs := range pending {
		for _, tx := range txs {
			data, err := j.encodeEntry(tx, true)
			if err != nil {
				continue
			}
			if _, err := tmpFile.Write(data); err != nil {
				tmpFile.Close()
				os.Remove(tmpPath)
				j.reopenFile()
				return err
			}
			written++
		}
	}

	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		j.reopenFile()
		return err
	}

	info, _ := tmpFile.Stat()
	var newSize int64
	if info != nil {
		newSize = info.Size()
	}
	tmpFile.Close()

	// Atomic replace.
	if err := os.Rename(tmpPath, j.config.Path); err != nil {
		os.Remove(tmpPath)
		j.reopenFile()
		return err
	}

	// Reopen for appending.
	j.file, err = os.OpenFile(j.config.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	j.entriesSinceRotate = written
	j.writesSinceFlush = 0
	j.createdAt = time.Now()
	j.Metrics.RotationCount.Add(1)
	j.Metrics.JournalBytes.Store(newSize)

	return nil
}

// reopenFile attempts to reopen the journal for appending after a failure.
func (j *TxJrnl) reopenFile() {
	j.file, _ = os.OpenFile(j.config.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
}

// Close stops the background flush goroutine, syncs pending writes, and
// closes the journal file. After Close, all Write calls return ErrJrnlClosed.
func (j *TxJrnl) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.closed {
		return nil
	}
	j.closed = true

	// Stop background flusher.
	select {
	case j.stopFlush <- struct{}{}:
	default:
	}

	if j.file != nil {
		if err := j.file.Sync(); err != nil {
			j.file.Close()
			return err
		}
		return j.file.Close()
	}
	return nil
}

// IsClosed returns whether the journal has been closed.
func (j *TxJrnl) IsClosed() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.closed
}

// Path returns the configured journal file path.
func (j *TxJrnl) Path() string {
	return j.config.Path
}

// EntriesSinceRotate returns the number of entries written since the
// last rotation (or since the journal was opened).
func (j *TxJrnl) EntriesSinceRotate() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.entriesSinceRotate
}

// Exists returns whether the journal file exists on disk.
func (j *TxJrnl) Exists() bool {
	_, err := os.Stat(j.config.Path)
	return err == nil
}

// maxFrameLen caps the maximum decoded frame to avoid OOM on corrupt data.
const maxFrameLen = 1 << 20 // 1 MiB

// ValidateJournal checks a journal file for corruption without loading
// transactions into memory. Returns the total entry count, the number of
// corrupt entries, and the first error encountered (if any).
func ValidateJournal(path string) (total int, corrupt int, firstErr error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}

	offset := 0
	for offset < len(data) {
		if offset+4 > len(data) {
			corrupt++
			if firstErr == nil {
				firstErr = &JrnlError{Offset: int64(offset), Err: errors.New("truncated frame header")}
			}
			break
		}

		frameLen := int(binary.BigEndian.Uint32(data[offset : offset+4]))
		offset += 4

		if frameLen <= 0 || frameLen > maxFrameLen || offset+frameLen > len(data) {
			corrupt++
			if firstErr == nil {
				firstErr = &JrnlError{Offset: int64(offset - 4), Err: errors.New("invalid frame length")}
			}
			offset = scanForNextFrame(data, offset)
			continue
		}

		// Try decoding to verify validity.
		var entry jrnlEntry
		if err := rlp.DecodeBytes(data[offset:offset+frameLen], &entry); err != nil {
			corrupt++
			if firstErr == nil {
				firstErr = &JrnlError{Offset: int64(offset), Err: err}
			}
		} else {
			total++
		}
		offset += frameLen
	}

	return total, corrupt, firstErr
}

// JrnlSize returns the current on-disk size of the journal in bytes. Returns
// 0 if the file does not exist or cannot be read. This reads from the
// metrics cache rather than stat'ing the file.
func (j *TxJrnl) JrnlSize() int64 {
	v := j.Metrics.JournalBytes.Load()
	if v < 0 {
		return 0
	}
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return v
}
