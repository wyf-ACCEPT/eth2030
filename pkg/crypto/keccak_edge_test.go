package crypto

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

// TestKeccak256NilInput tests Keccak256 with nil input.
func TestKeccak256NilInput(t *testing.T) {
	hash := Keccak256(nil)
	got := hex.EncodeToString(hash)
	// nil should behave like empty, since no data is written.
	want := "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470"
	if got != want {
		t.Errorf("Keccak256(nil) = %s, want %s", got, want)
	}
}

// TestKeccak256NoArguments tests Keccak256 with no arguments at all.
func TestKeccak256NoArguments(t *testing.T) {
	hash := Keccak256()
	got := hex.EncodeToString(hash)
	want := "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470"
	if got != want {
		t.Errorf("Keccak256() = %s, want %s", got, want)
	}
}

// TestKeccak256SingleByte tests Keccak256 with various single byte inputs.
func TestKeccak256SingleByte(t *testing.T) {
	tests := []struct {
		input byte
		want  string
	}{
		{0x00, "bc36789e7a1e281436464229828f817d6612f7b477d66591ff96a9e064bcc98a"},
		{0x01, "5fe7f977e71dba2ea1a68e21057beebb9be2ac30c6410aa38d4f3fbe41dcffd2"},
		{0xff, "8b1a944cf13a9a1c08facb2c9e98623ef3254d2ddb48113885c3e8e97fec8db9"},
	}
	for _, tc := range tests {
		got := hex.EncodeToString(Keccak256([]byte{tc.input}))
		if got != tc.want {
			t.Errorf("Keccak256(0x%02x) = %s, want %s", tc.input, got, tc.want)
		}
	}
}

// TestKeccak256LargeInput tests Keccak256 with a large input (1MB).
func TestKeccak256LargeInput(t *testing.T) {
	data := make([]byte, 1024*1024) // 1MB of zeros
	hash := Keccak256(data)
	if len(hash) != 32 {
		t.Fatalf("Keccak256(large) length = %d, want 32", len(hash))
	}
	// Same input should always produce the same output.
	hash2 := Keccak256(data)
	if !bytes.Equal(hash, hash2) {
		t.Error("Keccak256 not deterministic for large input")
	}
}

// TestKeccak256KnownEthereumVectors tests against known Ethereum test vectors.
func TestKeccak256KnownEthereumVectors(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470",
		},
		{
			name:  "hello",
			input: "hello",
			want:  "1c8aff950685c2ed4bc3174f3472287b56d9517b9c948127319a09a7a36deac8",
		},
		{
			name:  "solidity selector transfer(address,uint256)",
			input: "transfer(address,uint256)",
			want:  "a9059cbb2ab09eb219583f4a59a5d0623ade346d962bcd4e46b11da047c9049b",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := hex.EncodeToString(Keccak256([]byte(tc.input)))
			if got != tc.want {
				t.Errorf("Keccak256(%q) = %s, want %s", tc.input, got, tc.want)
			}
		})
	}
}

// TestKeccak256MultipleEmptyInputs tests Keccak256 with multiple empty slices.
func TestKeccak256MultipleEmptyInputs(t *testing.T) {
	hash := Keccak256([]byte{}, []byte{}, []byte{})
	got := hex.EncodeToString(hash)
	want := "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470"
	if got != want {
		t.Errorf("Keccak256(empty, empty, empty) = %s, want %s", got, want)
	}
}

// TestKeccak256Incremental tests that splitting input across arguments
// produces the same result as concatenating.
func TestKeccak256Incremental(t *testing.T) {
	data := []byte("the quick brown fox jumps over the lazy dog")
	// Split at various points.
	for i := 0; i <= len(data); i++ {
		combined := Keccak256(data)
		split := Keccak256(data[:i], data[i:])
		if !bytes.Equal(combined, split) {
			t.Errorf("Split at %d: combined != split", i)
		}
	}
}

// TestKeccak256HashConsistency verifies Keccak256Hash and Keccak256 return
// equivalent results.
func TestKeccak256HashConsistency(t *testing.T) {
	inputs := []string{"", "hello", "test", strings.Repeat("a", 1000)}
	for _, input := range inputs {
		raw := Keccak256([]byte(input))
		h := Keccak256Hash([]byte(input))
		if !bytes.Equal(raw, h[:]) {
			t.Errorf("Keccak256 and Keccak256Hash mismatch for %q", input)
		}
	}
}

// TestKeccak256HashMultipleInputs tests Keccak256Hash with multiple inputs.
func TestKeccak256HashMultipleInputs(t *testing.T) {
	h1 := Keccak256Hash([]byte("hello"), []byte("world"))
	h2 := Keccak256Hash([]byte("helloworld"))
	if h1 != h2 {
		t.Errorf("Keccak256Hash multi-input mismatch: %s != %s", h1, h2)
	}
}

// TestKeccak256CollisionResistance verifies different inputs produce different hashes.
func TestKeccak256CollisionResistance(t *testing.T) {
	seen := make(map[string]string)
	inputs := []string{
		"", "a", "b", "ab", "ba", "abc", "hello", "world",
		"0", "1", "00", "01", "10", "11",
	}
	for _, input := range inputs {
		h := hex.EncodeToString(Keccak256([]byte(input)))
		if prev, ok := seen[h]; ok {
			t.Errorf("Collision: %q and %q both hash to %s", prev, input, h)
		}
		seen[h] = input
	}
}
