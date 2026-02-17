// Package witness defines execution witness and proof types for stateless
// block validation (EIP-8025, EIP-6800).
package witness

import (
	"github.com/eth2028/eth2028/core/types"
)

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
