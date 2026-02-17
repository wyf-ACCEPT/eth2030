package witness

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestNewExecutionWitness(t *testing.T) {
	root := types.HexToHash("0x1234")
	w := NewExecutionWitness(root)
	if w.ParentRoot != root {
		t.Errorf("parent root mismatch: got %s, want %s", w.ParentRoot, root)
	}
	if len(w.State) != 0 {
		t.Errorf("expected empty state, got %d stems", len(w.State))
	}
}

func TestSuffixStateDiffIsRead(t *testing.T) {
	val := [32]byte{1}
	tests := []struct {
		name string
		diff SuffixStateDiff
		read bool
	}{
		{"no values", SuffixStateDiff{Suffix: 0}, false},
		{"current only", SuffixStateDiff{Suffix: 0, CurrentValue: &val}, true},
		{"new only", SuffixStateDiff{Suffix: 0, NewValue: &val}, false},
		{"both", SuffixStateDiff{Suffix: 0, CurrentValue: &val, NewValue: &val}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.diff.IsRead(); got != tt.read {
				t.Errorf("IsRead() = %v, want %v", got, tt.read)
			}
		})
	}
}

func TestSuffixStateDiffIsWrite(t *testing.T) {
	val := [32]byte{1}
	tests := []struct {
		name  string
		diff  SuffixStateDiff
		write bool
	}{
		{"no values", SuffixStateDiff{Suffix: 0}, false},
		{"current only", SuffixStateDiff{Suffix: 0, CurrentValue: &val}, false},
		{"new only", SuffixStateDiff{Suffix: 0, NewValue: &val}, true},
		{"both", SuffixStateDiff{Suffix: 0, CurrentValue: &val, NewValue: &val}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.diff.IsWrite(); got != tt.write {
				t.Errorf("IsWrite() = %v, want %v", got, tt.write)
			}
		})
	}
}

func TestSuffixStateDiffIsModified(t *testing.T) {
	val1 := [32]byte{1}
	val2 := [32]byte{2}
	tests := []struct {
		name     string
		diff     SuffixStateDiff
		modified bool
	}{
		{"no values", SuffixStateDiff{}, false},
		{"same value", SuffixStateDiff{CurrentValue: &val1, NewValue: &val1}, false},
		{"different values", SuffixStateDiff{CurrentValue: &val1, NewValue: &val2}, true},
		{"only current", SuffixStateDiff{CurrentValue: &val1}, false},
		{"only new", SuffixStateDiff{NewValue: &val2}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.diff.IsModified(); got != tt.modified {
				t.Errorf("IsModified() = %v, want %v", got, tt.modified)
			}
		})
	}
}

func TestWitnessNumStems(t *testing.T) {
	w := NewExecutionWitness(types.Hash{})
	if w.NumStems() != 0 {
		t.Fatalf("expected 0 stems, got %d", w.NumStems())
	}

	w.State = append(w.State, StemStateDiff{})
	w.State = append(w.State, StemStateDiff{})
	if w.NumStems() != 2 {
		t.Fatalf("expected 2 stems, got %d", w.NumStems())
	}
}

func TestWitnessNumSuffixes(t *testing.T) {
	w := &ExecutionWitness{
		State: []StemStateDiff{
			{Suffixes: []SuffixStateDiff{{Suffix: 0}, {Suffix: 1}}},
			{Suffixes: []SuffixStateDiff{{Suffix: 5}}},
		},
	}
	if w.NumSuffixes() != 3 {
		t.Fatalf("expected 3 suffixes, got %d", w.NumSuffixes())
	}
}
