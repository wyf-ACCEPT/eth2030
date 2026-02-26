// migration.go implements MPT-to-binary trie migration with batch processing,
// address space splitting for parallelism, state proof generation during
// migration, crash-recovery checkpointing, and gas accounting.
package trie

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Migration errors.
var (
	ErrMigrationNilSource    = errors.New("migration: nil source trie")
	ErrMigrationAlreadyDone  = errors.New("migration: migration already complete")
	ErrMigrationNoCheckpoint = errors.New("migration: no checkpoint to restore from")
	ErrMigrationInvalidBatch = errors.New("migration: invalid batch size")
	ErrMigrationInvalidSplit = errors.New("migration: invalid split count")
	ErrMigrationGasExceeded  = errors.New("migration: gas budget exceeded")
)

// BatchConverter processes accounts in configurable batch sizes during
// MPT-to-binary migration. Each batch collects key-value pairs and
// inserts them into the destination trie.
type BatchConverter struct {
	batchSize int
	converted uint64
}

// NewBatchConverter creates a batch converter with the given batch size.
func NewBatchConverter(batchSize int) (*BatchConverter, error) {
	if batchSize <= 0 {
		return nil, ErrMigrationInvalidBatch
	}
	return &BatchConverter{batchSize: batchSize}, nil
}

// BatchSize returns the configured batch size.
func (bc *BatchConverter) BatchSize() int { return bc.batchSize }

// Converted returns the total number of keys converted so far.
func (bc *BatchConverter) Converted() uint64 { return bc.converted }

// ConvertBatch extracts up to batchSize key-value pairs from the source
// iterator starting at the given offset, and inserts them into dest.
// Returns the number of keys processed and whether the source is exhausted.
func (bc *BatchConverter) ConvertBatch(pairs []migrationPair, dest *BinaryTrie) (int, bool) {
	if len(pairs) == 0 {
		return 0, true
	}

	count := bc.batchSize
	if count > len(pairs) {
		count = len(pairs)
	}

	for i := 0; i < count; i++ {
		dest.PutHashed(pairs[i].key, pairs[i].value)
	}
	bc.converted += uint64(count)
	return count, count >= len(pairs)
}

// migrationPair is a key-value pair for migration.
type migrationPair struct {
	key   types.Hash
	value []byte
}

// AddressRange represents a contiguous range of the address space.
type AddressRange struct {
	Start types.Hash
	End   types.Hash
}

// AddressSpaceSplitter splits the 256-bit address space into N equal ranges
// for parallel migration.
type AddressSpaceSplitter struct {
	ranges []AddressRange
}

// NewAddressSpaceSplitter creates a splitter that divides the address space
// into n equal ranges.
func NewAddressSpaceSplitter(n int) (*AddressSpaceSplitter, error) {
	if n <= 0 {
		return nil, ErrMigrationInvalidSplit
	}
	if n > 256 {
		n = 256
	}

	ranges := make([]AddressRange, n)
	// Simple split: divide first byte evenly.
	step := 256 / n
	if step == 0 {
		step = 1
	}

	for i := 0; i < n; i++ {
		var start, end types.Hash
		startByte := byte(i * step)
		start[0] = startByte

		if i == n-1 {
			// Last range goes to max.
			for j := range end {
				end[j] = 0xFF
			}
		} else {
			endByte := byte((i+1)*step - 1)
			end[0] = endByte
			for j := 1; j < 32; j++ {
				end[j] = 0xFF
			}
		}
		ranges[i] = AddressRange{Start: start, End: end}
	}

	return &AddressSpaceSplitter{ranges: ranges}, nil
}

// Ranges returns the computed address ranges.
func (s *AddressSpaceSplitter) Ranges() []AddressRange { return s.ranges }

// NumRanges returns the number of ranges.
func (s *AddressSpaceSplitter) NumRanges() int { return len(s.ranges) }

// InRange checks whether a key falls within the given range.
func InRange(key types.Hash, r AddressRange) bool {
	for i := 0; i < 32; i++ {
		if key[i] < r.Start[i] {
			return false
		}
		if key[i] > r.Start[i] {
			break
		}
	}
	for i := 0; i < 32; i++ {
		if key[i] > r.End[i] {
			return false
		}
		if key[i] < r.End[i] {
			break
		}
	}
	return true
}

// StateProofGenerator generates MPT proofs during migration for verification.
type StateProofGenerator struct {
	source *Trie
	proofs map[types.Hash][][]byte
	mu     sync.Mutex
}

// NewStateProofGenerator creates a proof generator for the given source trie.
func NewStateProofGenerator(source *Trie) *StateProofGenerator {
	return &StateProofGenerator{
		source: source,
		proofs: make(map[types.Hash][][]byte),
	}
}

// GenerateProof generates and caches an MPT proof for the given key.
func (g *StateProofGenerator) GenerateProof(key []byte) ([][]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	hk := crypto.Keccak256Hash(key)
	if p, ok := g.proofs[hk]; ok {
		return p, nil
	}

	proof, err := g.source.Prove(key)
	if err != nil {
		return nil, fmt.Errorf("migration: proof generation failed: %w", err)
	}

	g.proofs[hk] = proof
	return proof, nil
}

// ProofCount returns the number of cached proofs.
func (g *StateProofGenerator) ProofCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.proofs)
}

// MigrationCheckpoint captures the state of a migration for crash recovery.
type MigrationCheckpoint struct {
	// KeysMigrated is the number of keys successfully migrated.
	KeysMigrated uint64
	// LastKeyHash is the hash of the last migrated key.
	LastKeyHash types.Hash
	// SourceRoot is the MPT root at checkpoint time.
	SourceRoot types.Hash
	// DestRoot is the binary trie root at checkpoint time.
	DestRoot types.Hash
	// BatchNumber is the batch number at checkpoint time.
	BatchNumber int
}

// MigrationCheckpointer saves and restores migration progress.
type MigrationCheckpointer struct {
	mu          sync.Mutex
	checkpoints []MigrationCheckpoint
}

// NewMigrationCheckpointer creates a new checkpointer.
func NewMigrationCheckpointer() *MigrationCheckpointer {
	return &MigrationCheckpointer{}
}

// Save saves a checkpoint.
func (mc *MigrationCheckpointer) Save(cp MigrationCheckpoint) {
	mc.mu.Lock()
	mc.checkpoints = append(mc.checkpoints, cp)
	mc.mu.Unlock()
}

// Latest returns the most recent checkpoint. Returns an error if none exist.
func (mc *MigrationCheckpointer) Latest() (MigrationCheckpoint, error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if len(mc.checkpoints) == 0 {
		return MigrationCheckpoint{}, ErrMigrationNoCheckpoint
	}
	return mc.checkpoints[len(mc.checkpoints)-1], nil
}

// Count returns the number of saved checkpoints.
func (mc *MigrationCheckpointer) Count() int {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	return len(mc.checkpoints)
}

// GasAccountant tracks the gas cost of migration operations.
type GasAccountant struct {
	mu       sync.Mutex
	total    uint64
	budget   uint64
	perRead  uint64
	perWrite uint64
	perProof uint64
}

// NewGasAccountant creates a gas accountant with a budget and per-op costs.
func NewGasAccountant(budget, perRead, perWrite, perProof uint64) *GasAccountant {
	return &GasAccountant{
		budget:   budget,
		perRead:  perRead,
		perWrite: perWrite,
		perProof: perProof,
	}
}

// ChargeRead charges gas for a read operation. Returns error if budget exceeded.
func (ga *GasAccountant) ChargeRead() error {
	ga.mu.Lock()
	defer ga.mu.Unlock()
	ga.total += ga.perRead
	if ga.budget > 0 && ga.total > ga.budget {
		return ErrMigrationGasExceeded
	}
	return nil
}

// ChargeWrite charges gas for a write operation.
func (ga *GasAccountant) ChargeWrite() error {
	ga.mu.Lock()
	defer ga.mu.Unlock()
	ga.total += ga.perWrite
	if ga.budget > 0 && ga.total > ga.budget {
		return ErrMigrationGasExceeded
	}
	return nil
}

// ChargeProof charges gas for a proof generation.
func (ga *GasAccountant) ChargeProof() error {
	ga.mu.Lock()
	defer ga.mu.Unlock()
	ga.total += ga.perProof
	if ga.budget > 0 && ga.total > ga.budget {
		return ErrMigrationGasExceeded
	}
	return nil
}

// TotalGas returns the total gas consumed.
func (ga *GasAccountant) TotalGas() uint64 {
	ga.mu.Lock()
	defer ga.mu.Unlock()
	return ga.total
}

// Remaining returns the remaining gas budget. Returns 0 if no budget is set.
func (ga *GasAccountant) Remaining() uint64 {
	ga.mu.Lock()
	defer ga.mu.Unlock()
	if ga.budget == 0 {
		return 0
	}
	if ga.total >= ga.budget {
		return 0
	}
	return ga.budget - ga.total
}

// MPTToBinaryTrieMigrator orchestrates full MPT-to-binary-trie migration
// with batching, checkpointing, parallelism, and gas accounting.
type MPTToBinaryTrieMigrator struct {
	mu           sync.Mutex
	source       *Trie
	dest         *BinaryTrie
	converter    *BatchConverter
	checkpointer *MigrationCheckpointer
	gasAcct      *GasAccountant
	proofGen     *StateProofGenerator
	complete     bool
	pairs        []migrationPair
	offset       int
	batchNum     int
	keysMigrated atomic.Uint64
}

// NewMPTToBinaryTrieMigrator creates a new full-featured migrator.
func NewMPTToBinaryTrieMigrator(
	source *Trie,
	batchSize int,
	gasBudget uint64,
) (*MPTToBinaryTrieMigrator, error) {
	if source == nil {
		return nil, ErrMigrationNilSource
	}
	bc, err := NewBatchConverter(batchSize)
	if err != nil {
		return nil, err
	}

	return &MPTToBinaryTrieMigrator{
		source:       source,
		dest:         NewBinaryTrie(),
		converter:    bc,
		checkpointer: NewMigrationCheckpointer(),
		gasAcct:      NewGasAccountant(gasBudget, 200, 5000, 3000),
		proofGen:     NewStateProofGenerator(source),
	}, nil
}

// MigrateBatch migrates one batch. Returns number migrated and done flag.
func (m *MPTToBinaryTrieMigrator) MigrateBatch() (int, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.complete {
		return 0, true, ErrMigrationAlreadyDone
	}

	// Lazily collect pairs on first call.
	if m.pairs == nil {
		m.pairs = m.collectPairs()
	}

	remaining := m.pairs[m.offset:]
	count, done := m.converter.ConvertBatch(remaining, m.dest)

	// Charge gas for each write.
	for i := 0; i < count; i++ {
		if err := m.gasAcct.ChargeRead(); err != nil {
			return i, false, err
		}
		if err := m.gasAcct.ChargeWrite(); err != nil {
			return i, false, err
		}
	}

	m.offset += count
	m.batchNum++
	m.keysMigrated.Add(uint64(count))

	// Save checkpoint after each batch.
	var lastKey types.Hash
	if count > 0 {
		lastKey = remaining[count-1].key
	}
	m.checkpointer.Save(MigrationCheckpoint{
		KeysMigrated: m.keysMigrated.Load(),
		LastKeyHash:  lastKey,
		SourceRoot:   m.source.Hash(),
		DestRoot:     m.dest.Hash(),
		BatchNumber:  m.batchNum,
	})

	if done {
		m.complete = true
	}
	return count, done, nil
}

// Destination returns the destination binary trie.
func (m *MPTToBinaryTrieMigrator) Destination() *BinaryTrie {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.dest
}

// KeysMigrated returns the count of keys migrated.
func (m *MPTToBinaryTrieMigrator) KeysMigrated() uint64 {
	return m.keysMigrated.Load()
}

// Checkpointer returns the migration checkpointer.
func (m *MPTToBinaryTrieMigrator) Checkpointer() *MigrationCheckpointer {
	return m.checkpointer
}

// GasAccountant returns the gas accountant.
func (m *MPTToBinaryTrieMigrator) GasAccountant() *GasAccountant {
	return m.gasAcct
}

// ProofGenerator returns the state proof generator.
func (m *MPTToBinaryTrieMigrator) ProofGenerator() *StateProofGenerator {
	return m.proofGen
}

func (m *MPTToBinaryTrieMigrator) collectPairs() []migrationPair {
	it := NewIterator(m.source)
	var pairs []migrationPair
	for it.Next() {
		hk := crypto.Keccak256Hash(it.Key)
		val := make([]byte, len(it.Value))
		copy(val, it.Value)
		pairs = append(pairs, migrationPair{key: hk, value: val})
	}
	return pairs
}
