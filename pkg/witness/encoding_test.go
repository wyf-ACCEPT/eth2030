package witness

import (
	"bytes"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestEncodeDecodeEmptyWitness(t *testing.T) {
	w := NewExecutionWitness(types.HexToHash("0xabcd"))

	data, err := EncodeWitness(w)
	if err != nil {
		t.Fatalf("EncodeWitness: %v", err)
	}

	w2, err := DecodeWitness(data)
	if err != nil {
		t.Fatalf("DecodeWitness: %v", err)
	}

	if w2.ParentRoot != w.ParentRoot {
		t.Errorf("parent root mismatch: %s vs %s", w2.ParentRoot, w.ParentRoot)
	}
	if len(w2.State) != 0 {
		t.Errorf("expected 0 stems, got %d", len(w2.State))
	}
}

func TestEncodeDecodeWitnessWithData(t *testing.T) {
	root := types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	w := &ExecutionWitness{
		ParentRoot: root,
		State: []StemStateDiff{
			{
				Stem: [31]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31},
				Suffixes: []SuffixStateDiff{
					{
						Suffix:       0,
						CurrentValue: &[32]byte{0xaa},
						NewValue:     &[32]byte{0xbb},
					},
					{
						Suffix:       42,
						CurrentValue: &[32]byte{0xcc},
						NewValue:     nil, // read only
					},
				},
			},
			{
				Stem: [31]byte{0xff},
				Suffixes: []SuffixStateDiff{
					{
						Suffix:       255,
						CurrentValue: nil, // new creation
						NewValue:     &[32]byte{0xdd},
					},
				},
			},
		},
	}

	data, err := EncodeWitness(w)
	if err != nil {
		t.Fatalf("EncodeWitness: %v", err)
	}

	w2, err := DecodeWitness(data)
	if err != nil {
		t.Fatalf("DecodeWitness: %v", err)
	}

	// Verify parent root
	if w2.ParentRoot != root {
		t.Errorf("parent root mismatch")
	}

	// Verify stem count
	if len(w2.State) != 2 {
		t.Fatalf("expected 2 stems, got %d", len(w2.State))
	}

	// Verify first stem
	if w2.State[0].Stem != w.State[0].Stem {
		t.Errorf("stem 0 mismatch")
	}
	if len(w2.State[0].Suffixes) != 2 {
		t.Fatalf("expected 2 suffixes in stem 0, got %d", len(w2.State[0].Suffixes))
	}

	// Verify first suffix (read+write)
	s0 := w2.State[0].Suffixes[0]
	if s0.Suffix != 0 {
		t.Errorf("suffix index mismatch: %d", s0.Suffix)
	}
	if s0.CurrentValue == nil || s0.CurrentValue[0] != 0xaa {
		t.Errorf("current value mismatch")
	}
	if s0.NewValue == nil || s0.NewValue[0] != 0xbb {
		t.Errorf("new value mismatch")
	}

	// Verify second suffix (read only)
	s1 := w2.State[0].Suffixes[1]
	if s1.Suffix != 42 {
		t.Errorf("suffix index mismatch: %d", s1.Suffix)
	}
	if s1.CurrentValue == nil || s1.CurrentValue[0] != 0xcc {
		t.Errorf("current value mismatch")
	}
	if s1.NewValue != nil {
		t.Errorf("expected nil new value")
	}

	// Verify second stem
	if len(w2.State[1].Suffixes) != 1 {
		t.Fatalf("expected 1 suffix in stem 1")
	}
	s2 := w2.State[1].Suffixes[0]
	if s2.CurrentValue != nil {
		t.Errorf("expected nil current value for new creation")
	}
	if s2.NewValue == nil || s2.NewValue[0] != 0xdd {
		t.Errorf("new value mismatch for creation")
	}
}

func TestDecodeWitnessTruncated(t *testing.T) {
	// Too short for header
	_, err := DecodeWitness(make([]byte, 10))
	if err != ErrTruncatedData {
		t.Errorf("expected ErrTruncatedData, got %v", err)
	}

	// Valid header but claims stems that don't exist
	data := make([]byte, 36)
	data[35] = 1 // 1 stem claimed
	_, err = DecodeWitness(data)
	if err != ErrTruncatedData {
		t.Errorf("expected ErrTruncatedData for missing stem data, got %v", err)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	// Create a non-trivial witness
	oldVal := [32]byte{}
	newVal := [32]byte{}
	for i := range oldVal {
		oldVal[i] = byte(i)
		newVal[i] = byte(255 - i)
	}

	w := &ExecutionWitness{
		ParentRoot: types.HexToHash("0xdeadbeef"),
		State: []StemStateDiff{
			{
				Stem: [31]byte{0x01, 0x02, 0x03},
				Suffixes: []SuffixStateDiff{
					{Suffix: 0, CurrentValue: &oldVal, NewValue: &newVal},
					{Suffix: 1, CurrentValue: &oldVal},
					{Suffix: 2, NewValue: &newVal},
				},
			},
		},
	}

	encoded, err := EncodeWitness(w)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := DecodeWitness(encoded)
	if err != nil {
		t.Fatal(err)
	}

	// Re-encode and compare bytes
	reencoded, err := EncodeWitness(decoded)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(encoded, reencoded) {
		t.Error("round-trip encoding mismatch")
	}
}
