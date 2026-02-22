// MPT-to-Verkle state migration converter.
//
// This file implements the MPTConverter, which converts Merkle Patricia Trie
// (MPT) state data into Verkle tree key-value pairs. It is the core conversion
// engine used by the migration scheduler to transition Ethereum state from MPT
// to Verkle trees as specified in EIP-6800.
//
// The converter handles:
//   - Account data: nonce, balance, code hash, storage root -> Verkle keys
//   - Storage slots: slot key/value -> Verkle storage keys
//   - Batch conversion with progress tracking
//   - Final root commitment computation
//
// Usage:
//
//	converter := NewMPTConverter(config, sourceReader)
//	converter.ConvertAccount(address, nonce, balance, codeHash, storageRoot)
//	converter.ConvertStorageSlot(address, slot, value)
//	root, err := converter.Finalize()

package verkle

import (
	"encoding/binary"
	"errors"
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// ConverterConfig controls the MPT-to-Verkle conversion process.
type ConverterConfig struct {
	// BatchSize is the maximum number of entries to process per batch call.
	BatchSize int

	// MaxPendingKeys is the limit on keys buffered before flushing to the tree.
	MaxPendingKeys int

	// ProgressCallback, if non-nil, is called after each batch with the
	// number of processed and total entries.
	ProgressCallback func(processed, total int)
}

// DefaultConverterConfig returns a ConverterConfig with sensible defaults.
func DefaultConverterConfig() ConverterConfig {
	return ConverterConfig{
		BatchSize:      1000,
		MaxPendingKeys: 10000,
	}
}

// MPTSourceReader provides read access to the source MPT state.
// It maps a Verkle tree key to the corresponding value in the MPT.
type MPTSourceReader func(key []byte) ([]byte, error)

// MPTConverter manages the conversion of MPT state data to a Verkle tree.
// It maintains an internal Verkle tree and tracks conversion progress.
type MPTConverter struct {
	mu           sync.Mutex
	config       ConverterConfig
	sourceReader MPTSourceReader
	verkleRoot   *VerkleInternalNode
	pedConfig    *PedersenConfig

	// Progress tracking.
	converted int
	remaining int
	errors    int
	lastErr   error

	// Pending batch buffer.
	pendingKeys   [][]byte
	pendingValues [][]byte
}

// NewMPTConverter creates a new MPT-to-Verkle converter.
// The sourceReader function is used to look up values by key in the source
// MPT. If sourceReader is nil, only explicit conversion calls are supported.
func NewMPTConverter(config ConverterConfig, sourceReader MPTSourceReader) *MPTConverter {
	pedConfig := DefaultPedersenConfig()
	return &MPTConverter{
		config:       config,
		sourceReader: sourceReader,
		verkleRoot:   NewVerkleInternalNode(0, pedConfig),
		pedConfig:    pedConfig,
	}
}

// NewMPTConverterWithConfig creates an MPTConverter with a specific
// PedersenConfig (useful for testing with smaller widths).
func NewMPTConverterWithConfig(
	config ConverterConfig,
	sourceReader MPTSourceReader,
	pedConfig *PedersenConfig,
) *MPTConverter {
	if pedConfig == nil {
		pedConfig = DefaultPedersenConfig()
	}
	return &MPTConverter{
		config:       config,
		sourceReader: sourceReader,
		verkleRoot:   NewVerkleInternalNode(0, pedConfig),
		pedConfig:    pedConfig,
	}
}

// ConvertAccount converts an MPT account into Verkle tree entries.
// It writes the version, balance, nonce, and code hash fields under
// the account's EIP-6800 stem.
func (c *MPTConverter) ConvertAccount(
	address [20]byte,
	nonce uint64,
	balance []byte,
	codeHash [32]byte,
	storageRoot [32]byte,
) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	addr := types.BytesToAddress(address[:])

	// Write version = 0.
	versionKey := GetTreeKeyForVersion(addr)
	var versionVal [ValueSize]byte
	if err := c.insertKV(versionKey[:], versionVal[:]); err != nil {
		return c.recordError(err)
	}

	// Write balance (big-endian, left-padded to 32 bytes).
	balanceKey := GetTreeKeyForBalance(addr)
	var balanceVal [ValueSize]byte
	if len(balance) > 0 {
		bal := new(big.Int).SetBytes(balance)
		b := bal.Bytes()
		if len(b) <= ValueSize {
			copy(balanceVal[ValueSize-len(b):], b)
		}
	}
	if err := c.insertKV(balanceKey[:], balanceVal[:]); err != nil {
		return c.recordError(err)
	}

	// Write nonce (little-endian in first 8 bytes).
	nonceKey := GetTreeKeyForNonce(addr)
	var nonceVal [ValueSize]byte
	binary.LittleEndian.PutUint64(nonceVal[:8], nonce)
	if err := c.insertKV(nonceKey[:], nonceVal[:]); err != nil {
		return c.recordError(err)
	}

	// Write code hash.
	codeHashKey := GetTreeKeyForCodeHash(addr)
	var codeHashVal [ValueSize]byte
	copy(codeHashVal[:], codeHash[:])
	if err := c.insertKV(codeHashKey[:], codeHashVal[:]); err != nil {
		return c.recordError(err)
	}

	c.converted += 4 // four fields per account
	return nil
}

// ConvertStorageSlot converts a single MPT storage slot to a Verkle tree
// entry. The slot is mapped to a Verkle storage key per EIP-6800.
func (c *MPTConverter) ConvertStorageSlot(
	address [20]byte,
	slot [32]byte,
	value [32]byte,
) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	addr := types.BytesToAddress(address[:])
	slotNum := slotToUint64(slot)
	treeKey := GetTreeKeyForStorageSlot(addr, slotNum)

	var val [ValueSize]byte
	copy(val[:], value[:])

	if err := c.insertKV(treeKey[:], val[:]); err != nil {
		return c.recordError(err)
	}

	c.converted++
	return nil
}

// BatchConvert converts a batch of key-value pairs into the Verkle tree.
// Keys and values must be 32 bytes each. Returns the number of successfully
// converted entries and any error encountered.
func (c *MPTConverter) BatchConvert(keys [][]byte, values [][]byte) (int, error) {
	if len(keys) != len(values) {
		return 0, errors.New("mpt_converter: keys and values length mismatch")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	converted := 0
	batchLimit := c.config.BatchSize
	if batchLimit <= 0 {
		batchLimit = len(keys)
	}

	for i := 0; i < len(keys) && converted < batchLimit; i++ {
		if len(keys[i]) != KeySize {
			c.errors++
			c.lastErr = errors.New("mpt_converter: key must be 32 bytes")
			continue
		}
		if len(values[i]) != ValueSize {
			c.errors++
			c.lastErr = errors.New("mpt_converter: value must be 32 bytes")
			continue
		}

		if err := c.insertKV(keys[i], values[i]); err != nil {
			c.errors++
			c.lastErr = err
			continue
		}

		converted++
		c.converted++
	}

	// Call progress callback if configured.
	if c.config.ProgressCallback != nil {
		c.config.ProgressCallback(c.converted, c.converted+c.remaining)
	}

	return converted, nil
}

// SetRemaining sets the expected total number of remaining entries.
// This is used for progress reporting.
func (c *MPTConverter) SetRemaining(remaining int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.remaining = remaining
}

// GetVerkleRoot returns the internal Verkle tree root node.
// The tree may still be dirty (uncommitted).
func (c *MPTConverter) GetVerkleRoot() *VerkleInternalNode {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.verkleRoot
}

// ComputeProgress returns the number of converted entries and remaining
// entries for progress tracking.
func (c *MPTConverter) ComputeProgress() (converted, remaining int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.converted, c.remaining
}

// ErrorCount returns the number of errors encountered during conversion.
func (c *MPTConverter) ErrorCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.errors
}

// LastError returns the most recent error, or nil.
func (c *MPTConverter) LastError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastErr
}

// Finalize computes the final root commitment of the Verkle tree.
// This recursively commits all dirty nodes and returns the 32-byte
// root commitment. After finalization, the tree is fully committed.
func (c *MPTConverter) Finalize() ([32]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Flush any pending entries.
	if err := c.flushPending(); err != nil {
		return [32]byte{}, err
	}

	commitment := c.verkleRoot.NodeCommitment()
	return commitment, nil
}

// AddPending adds a key-value pair to the pending buffer. When the buffer
// exceeds MaxPendingKeys, it is flushed to the tree.
func (c *MPTConverter) AddPending(key, value []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.pendingKeys = append(c.pendingKeys, key)
	c.pendingValues = append(c.pendingValues, value)

	if c.config.MaxPendingKeys > 0 && len(c.pendingKeys) >= c.config.MaxPendingKeys {
		return c.flushPending()
	}
	return nil
}

// FlushPending writes all buffered pending entries to the tree.
func (c *MPTConverter) FlushPending() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.flushPending()
}

// flushPending writes pending entries (must hold mu).
func (c *MPTConverter) flushPending() error {
	for i := range c.pendingKeys {
		if len(c.pendingKeys[i]) != KeySize || len(c.pendingValues[i]) != ValueSize {
			c.errors++
			continue
		}
		if err := c.insertKV(c.pendingKeys[i], c.pendingValues[i]); err != nil {
			c.errors++
			c.lastErr = err
			continue
		}
		c.converted++
	}
	c.pendingKeys = c.pendingKeys[:0]
	c.pendingValues = c.pendingValues[:0]
	return nil
}

// insertKV inserts a key-value pair into the Verkle tree (must hold mu).
func (c *MPTConverter) insertKV(key, value []byte) error {
	return c.verkleRoot.Insert(key, value)
}

// recordError records an error and returns it.
func (c *MPTConverter) recordError(err error) error {
	c.errors++
	c.lastErr = err
	return err
}

// slotToUint64 converts a 32-byte storage slot hash to a uint64 by
// reading the last 8 bytes. This matches the storage key derivation
// in the statedb.
func slotToUint64(slot [32]byte) uint64 {
	return binary.BigEndian.Uint64(slot[24:])
}

// ConvertAccountFromSource reads account data from the source reader
// and converts it to Verkle tree entries. Requires a source reader.
func (c *MPTConverter) ConvertAccountFromSource(address [20]byte) error {
	if c.sourceReader == nil {
		return errors.New("mpt_converter: no source reader configured")
	}

	addr := types.BytesToAddress(address[:])

	// Read balance from source.
	balKey := GetTreeKeyForBalance(addr)
	balBytes, err := c.sourceReader(balKey[:])
	if err != nil {
		return c.recordError(err)
	}

	// Read nonce from source.
	nonceKey := GetTreeKeyForNonce(addr)
	nonceBytes, err := c.sourceReader(nonceKey[:])
	if err != nil {
		return c.recordError(err)
	}

	// Read code hash from source.
	chKey := GetTreeKeyForCodeHash(addr)
	chBytes, err := c.sourceReader(chKey[:])
	if err != nil {
		return c.recordError(err)
	}

	var nonce uint64
	if nonceBytes != nil && len(nonceBytes) >= 8 {
		nonce = binary.LittleEndian.Uint64(nonceBytes[:8])
	}

	var codeHash [32]byte
	if chBytes != nil {
		copy(codeHash[:], chBytes)
	}

	return c.ConvertAccount(address, nonce, balBytes, codeHash, [32]byte{})
}

// ConverterStats holds a snapshot of the converter's progress.
type ConverterStats struct {
	Converted int
	Remaining int
	Errors    int
	LastError error
}

// Stats returns a snapshot of the converter's current progress.
func (c *MPTConverter) Stats() ConverterStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return ConverterStats{
		Converted: c.converted,
		Remaining: c.remaining,
		Errors:    c.errors,
		LastError: c.lastErr,
	}
}
