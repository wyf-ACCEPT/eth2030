// Package witness defines execution witness and proof types for stateless
// block validation (EIP-8025, EIP-6800).
package witness

import (
	"math/big"

	"github.com/eth2030/eth2030/core/types"
)

// --- Stateless block validation witness types ---

// BlockWitness captures all state data needed to verify a block without
// having the full state trie. It records pre-state values for every account
// and storage slot accessed during block execution.
type BlockWitness struct {
	// State contains pre-state values for all accessed accounts, keyed by address.
	State map[types.Address]*AccountWitness

	// Codes maps code hashes to their bytecode for all contracts read during
	// execution.
	Codes map[types.Hash][]byte

	// Headers contains ancestor block headers needed for the BLOCKHASH opcode,
	// keyed by block number.
	Headers map[uint64]*types.Header
}

// AccountWitness stores the pre-state of an account and its accessed storage
// slots. It records values at the time they were first read during execution.
type AccountWitness struct {
	// Balance is the account balance before execution.
	Balance *big.Int
	// Nonce is the account nonce before execution.
	Nonce uint64
	// CodeHash is the code hash before execution.
	CodeHash types.Hash
	// Storage maps storage slots to their pre-state values.
	Storage map[types.Hash]types.Hash
	// Exists records whether the account existed in state before execution.
	Exists bool
}

// NewBlockWitness creates an empty block witness.
func NewBlockWitness() *BlockWitness {
	return &BlockWitness{
		State:   make(map[types.Address]*AccountWitness),
		Codes:   make(map[types.Hash][]byte),
		Headers: make(map[uint64]*types.Header),
	}
}

// TouchAccount ensures an AccountWitness entry exists for the given address.
// Returns the existing or newly created entry.
func (w *BlockWitness) TouchAccount(addr types.Address) *AccountWitness {
	if aw, ok := w.State[addr]; ok {
		return aw
	}
	aw := &AccountWitness{
		Balance: new(big.Int),
		Storage: make(map[types.Hash]types.Hash),
	}
	w.State[addr] = aw
	return aw
}

// AddCode stores bytecode in the witness, keyed by its hash.
func (w *BlockWitness) AddCode(codeHash types.Hash, code []byte) {
	if _, ok := w.Codes[codeHash]; ok {
		return
	}
	cp := make([]byte, len(code))
	copy(cp, code)
	w.Codes[codeHash] = cp
}

// AddHeader stores an ancestor block header in the witness.
func (w *BlockWitness) AddHeader(num uint64, header *types.Header) {
	if _, ok := w.Headers[num]; ok {
		return
	}
	w.Headers[num] = header
}

// ExecutionWitness contains all data needed for stateless block validation.
// It captures the state accessed during block execution, enabling a node
// without the full state to verify execution correctness.
type ExecutionWitness struct {
	// State is the set of state diffs organized by Verkle tree stem.
	State []StemStateDiff

	// ParentRoot is the state root of the parent block.
	ParentRoot types.Hash
}

// StemStateDiff captures state diffs at a single Verkle tree stem (31 bytes).
// A stem identifies a group of 256 adjacent leaves in the Verkle tree.
type StemStateDiff struct {
	// Stem is the 31-byte Verkle tree stem.
	Stem [31]byte

	// Suffixes contains the leaf-level diffs under this stem.
	Suffixes []SuffixStateDiff
}

// SuffixStateDiff captures an individual leaf-level state diff.
type SuffixStateDiff struct {
	// Suffix is the leaf index within the stem (0-255).
	Suffix byte

	// CurrentValue is the value before execution. Nil if the leaf was not read.
	CurrentValue *[32]byte

	// NewValue is the value after execution. Nil if the leaf was not modified.
	NewValue *[32]byte
}

// NewExecutionWitness creates an empty execution witness with the given parent root.
func NewExecutionWitness(parentRoot types.Hash) *ExecutionWitness {
	return &ExecutionWitness{
		ParentRoot: parentRoot,
	}
}

// AddStemDiff adds a stem state diff to the witness.
func (w *ExecutionWitness) AddStemDiff(diff StemStateDiff) {
	w.Stem().Suffixes = append(w.Stem().Suffixes, diff.Suffixes...)
	w.State = append(w.State, diff)
}

// Stem returns the last StemStateDiff (helper for building witnesses).
func (w *ExecutionWitness) Stem() *StemStateDiff {
	if len(w.State) == 0 {
		return nil
	}
	return &w.State[len(w.State)-1]
}

// NumStems returns the number of stems in the witness.
func (w *ExecutionWitness) NumStems() int {
	return len(w.State)
}

// NumSuffixes returns the total number of suffix diffs across all stems.
func (w *ExecutionWitness) NumSuffixes() int {
	count := 0
	for _, s := range w.State {
		count += len(s.Suffixes)
	}
	return count
}

// IsRead returns true if the suffix diff represents a read (has current value).
func (d *SuffixStateDiff) IsRead() bool {
	return d.CurrentValue != nil
}

// IsWrite returns true if the suffix diff represents a write (has new value).
func (d *SuffixStateDiff) IsWrite() bool {
	return d.NewValue != nil
}

// IsModified returns true if the suffix diff represents a modification
// (both old and new values exist and differ).
func (d *SuffixStateDiff) IsModified() bool {
	if d.CurrentValue == nil || d.NewValue == nil {
		return false
	}
	return *d.CurrentValue != *d.NewValue
}
