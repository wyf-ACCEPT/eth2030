package txpool

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// Journal error codes.
var (
	ErrJournalClosed   = errors.New("journal is closed")
	ErrJournalCorrupt  = errors.New("journal file is corrupt")
	ErrJournalNotFound = errors.New("journal file not found")
)

// JournalEntry represents a single transaction journal record written to disk.
// Each entry contains a serialized transaction and metadata needed for replay.
type JournalEntry struct {
	TxRLP     []byte     `json:"tx_rlp"`
	Sender    []byte     `json:"sender"`
	Timestamp time.Time  `json:"timestamp"`
	Local     bool       `json:"local"`
	Hash      types.Hash `json:"hash"`
}

// TxJournal writes pending transactions to disk for crash recovery.
// On startup, the journal is replayed to recover transactions that were
// pending before the crash. The journal is periodically rotated to
// remove entries for transactions that have been mined.
type TxJournal struct {
	mu   sync.Mutex
	path string
	file *os.File

	closed bool
	count  int // number of entries written since last rotation
}

// NewTxJournal creates a new transaction journal backed by the given file path.
// The parent directory is created if it does not exist. The file is opened in
// append mode so entries survive process restarts.
func NewTxJournal(path string) (*TxJournal, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	return &TxJournal{
		path: path,
		file: f,
	}, nil
}

// Insert appends a transaction to the journal. The transaction is RLP-encoded
// and written as a single JSON line. Returns an error if the journal is closed
// or the write fails.
func (j *TxJournal) Insert(tx *types.Transaction, local bool) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.closed {
		return ErrJournalClosed
	}

	rlpData, err := tx.EncodeRLP()
	if err != nil {
		return err
	}

	var senderBytes []byte
	if from := tx.Sender(); from != nil {
		senderBytes = from[:]
	}

	entry := JournalEntry{
		TxRLP:     rlpData,
		Sender:    senderBytes,
		Timestamp: time.Now(),
		Local:     local,
		Hash:      tx.Hash(),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	if _, err := j.file.Write(data); err != nil {
		return err
	}

	j.count++
	return nil
}

// InsertBatch appends multiple transactions to the journal in a single call.
func (j *TxJournal) InsertBatch(txs []*types.Transaction, local bool) error {
	for _, tx := range txs {
		if err := j.Insert(tx, local); err != nil {
			return err
		}
	}
	return nil
}

// Load reads the journal file and returns all decoded transactions.
// Malformed entries are skipped. Returns ErrJournalNotFound if the
// file does not exist.
func Load(path string) ([]*types.Transaction, []JournalEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, ErrJournalNotFound
		}
		return nil, nil, err
	}

	var txs []*types.Transaction
	var entries []JournalEntry

	start := 0
	for i := 0; i <= len(data); i++ {
		if i == len(data) || data[i] == '\n' {
			line := data[start:i]
			start = i + 1
			if len(line) == 0 {
				continue
			}
			var entry JournalEntry
			if err := json.Unmarshal(line, &entry); err != nil {
				continue // skip corrupt entries
			}
			tx, err := types.DecodeTxRLP(entry.TxRLP)
			if err != nil {
				continue // skip undecodable transactions
			}
			// Restore sender cache if available.
			if len(entry.Sender) == 20 {
				var addr types.Address
				copy(addr[:], entry.Sender)
				tx.SetSender(addr)
			}
			txs = append(txs, tx)
			entries = append(entries, entry)
		}
	}

	return txs, entries, nil
}

// Rotate replaces the journal with only the transactions currently in the pool.
// This compacts the file by removing entries for transactions that have already
// been mined or evicted. The old file is atomically replaced.
func (j *TxJournal) Rotate(pending map[types.Address][]*types.Transaction) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.closed {
		return ErrJournalClosed
	}

	// Close the current file.
	if err := j.file.Sync(); err != nil {
		return err
	}
	if err := j.file.Close(); err != nil {
		return err
	}

	// Write a fresh journal with only the active transactions.
	tmpPath := j.path + ".tmp"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		// Reopen original to avoid leaving journal in broken state.
		j.file, _ = os.OpenFile(j.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		return err
	}

	written := 0
	for _, txs := range pending {
		for _, tx := range txs {
			rlpData, err := tx.EncodeRLP()
			if err != nil {
				continue
			}
			var senderBytes []byte
			if from := tx.Sender(); from != nil {
				senderBytes = from[:]
			}
			entry := JournalEntry{
				TxRLP:     rlpData,
				Sender:    senderBytes,
				Timestamp: time.Now(),
				Local:     true,
				Hash:      tx.Hash(),
			}
			data, err := json.Marshal(entry)
			if err != nil {
				continue
			}
			data = append(data, '\n')
			if _, err := tmpFile.Write(data); err != nil {
				tmpFile.Close()
				os.Remove(tmpPath)
				j.file, _ = os.OpenFile(j.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				return err
			}
			written++
		}
	}

	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		j.file, _ = os.OpenFile(j.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		return err
	}
	tmpFile.Close()

	// Atomically replace old journal.
	if err := os.Rename(tmpPath, j.path); err != nil {
		os.Remove(tmpPath)
		j.file, _ = os.OpenFile(j.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		return err
	}

	// Reopen journal for continued appending.
	j.file, err = os.OpenFile(j.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	j.count = written
	return nil
}

// Close flushes and closes the journal file. After Close, all Insert calls
// return ErrJournalClosed.
func (j *TxJournal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.closed {
		return nil
	}
	j.closed = true

	if j.file != nil {
		if err := j.file.Sync(); err != nil {
			j.file.Close()
			return err
		}
		return j.file.Close()
	}
	return nil
}

// Path returns the file path of the journal.
func (j *TxJournal) Path() string {
	return j.path
}

// Count returns the number of entries written since the last rotation.
func (j *TxJournal) Count() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.count
}

// Exists returns true if the journal file exists on disk.
func (j *TxJournal) Exists() bool {
	_, err := os.Stat(j.path)
	return err == nil
}

// IsClosed returns true if the journal has been closed.
func (j *TxJournal) IsClosed() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.closed
}
