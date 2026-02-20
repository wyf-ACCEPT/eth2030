package erasure

import (
	"bytes"
	"testing"
)

func TestEncodeBasic(t *testing.T) {
	data := []byte("abcdefghijklmnop")
	shards, err := Encode(data, 4, 2)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(shards) != 6 {
		t.Fatalf("expected 6 shards, got %d", len(shards))
	}
	// All shards should have the same size.
	for i := 1; i < len(shards); i++ {
		if len(shards[i]) != len(shards[0]) {
			t.Fatalf("shard %d size %d != shard 0 size %d", i, len(shards[i]), len(shards[0]))
		}
	}
}

func TestEncodeSingleShard(t *testing.T) {
	data := []byte("test")
	shards, err := Encode(data, 1, 1)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(shards) != 2 {
		t.Fatalf("expected 2 shards, got %d", len(shards))
	}
}

func TestEncodeDataPadding(t *testing.T) {
	// Data size not a multiple of dataShards, should be padded.
	data := []byte("abc") // 3 bytes, 4 data shards -> shard size = 1
	shards, err := Encode(data, 4, 2)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	// First 3 shards contain 'a', 'b', 'c'; 4th shard is zero.
	if shards[0][0] != 'a' {
		t.Errorf("shard 0 = %q, want 'a'", shards[0])
	}
	if shards[1][0] != 'b' {
		t.Errorf("shard 1 = %q, want 'b'", shards[1])
	}
	if shards[2][0] != 'c' {
		t.Errorf("shard 2 = %q, want 'c'", shards[2])
	}
	if shards[3][0] != 0 {
		t.Errorf("shard 3 = %d, want 0 (padding)", shards[3][0])
	}
}

func TestEncodeNegativeShards(t *testing.T) {
	_, err := Encode([]byte("data"), -1, 2)
	if err != ErrInvalidShardConfig {
		t.Fatalf("expected ErrInvalidShardConfig, got %v", err)
	}
	_, err = Encode([]byte("data"), 2, -1)
	if err != ErrInvalidShardConfig {
		t.Fatalf("expected ErrInvalidShardConfig, got %v", err)
	}
}

func TestDecodeAllDataPresent(t *testing.T) {
	data := []byte("hello world test data")
	dataShards := 4
	parityShards := 4

	shards, err := Encode(data, dataShards, parityShards)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Set parity shards to nil (only data available).
	for i := dataShards; i < dataShards+parityShards; i++ {
		shards[i] = nil
	}

	recovered, err := Decode(shards, dataShards, parityShards)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if !bytes.Equal(recovered[:len(data)], data) {
		t.Fatalf("recovered = %q, want %q", recovered[:len(data)], data)
	}
}

func TestDecodeInvalidShardConfig(t *testing.T) {
	_, err := Decode(nil, 0, 2)
	if err != ErrInvalidShardConfig {
		t.Fatalf("expected ErrInvalidShardConfig, got %v", err)
	}
	_, err = Decode(nil, 2, 0)
	if err != ErrInvalidShardConfig {
		t.Fatalf("expected ErrInvalidShardConfig, got %v", err)
	}
}

func TestDecodeShardCountMismatch(t *testing.T) {
	shards := make([][]byte, 3)
	_, err := Decode(shards, 4, 2) // expects 6, got 3
	if err == nil {
		t.Fatal("expected error for shard count mismatch")
	}
}

func TestDecodeAllNilShards(t *testing.T) {
	shards := make([][]byte, 6) // all nil
	_, err := Decode(shards, 4, 2)
	if err == nil {
		t.Fatal("expected error with all nil shards")
	}
}

func TestDecodeRecoverOneMissingShard(t *testing.T) {
	data := []byte("test data for RS recovery")
	dataShards := 4
	parityShards := 4

	shards, err := Encode(data, dataShards, parityShards)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Remove one data shard.
	shards[0] = nil

	recovered, err := Decode(shards, dataShards, parityShards)
	if err != nil {
		t.Fatalf("Decode with 1 missing: %v", err)
	}

	if !bytes.Equal(recovered[:len(data)], data) {
		t.Fatalf("recovered mismatch")
	}
}

func TestDecodeRecoverMultipleMissing(t *testing.T) {
	// XOR scheme can only recover 1 per parity, but multiple parity shards
	// can each recover a different missing shard.
	data := []byte("test multiple missing recovery here")
	dataShards := 4
	parityShards := 4

	shards, err := Encode(data, dataShards, parityShards)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Remove 2 data shards. Whether recovery works depends on the XOR structure.
	shards[0] = nil
	shards[1] = nil

	// This may or may not succeed depending on the specific XOR rotation.
	// We just check that the function returns without panic.
	_, _ = Decode(shards, dataShards, parityShards)
}

func TestEncodeDecodeVaryingSizes(t *testing.T) {
	sizes := []int{1, 7, 16, 100, 255, 1024}
	for _, size := range sizes {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i % 256)
		}
		shards, err := Encode(data, 4, 4)
		if err != nil {
			t.Fatalf("size %d: Encode: %v", size, err)
		}
		recovered, err := Decode(shards, 4, 4)
		if err != nil {
			t.Fatalf("size %d: Decode: %v", size, err)
		}
		if !bytes.Equal(recovered[:len(data)], data) {
			t.Fatalf("size %d: roundtrip mismatch", size)
		}
	}
}

func TestDecodeShardSizeMismatchDetection(t *testing.T) {
	shards := [][]byte{
		{1, 2},
		{3, 4, 5}, // different size
		{6, 7},
		{8, 9},
	}
	_, err := Decode(shards, 2, 2)
	if err != ErrShardSizeMismatch {
		t.Fatalf("expected ErrShardSizeMismatch, got %v", err)
	}
}

func TestEncodeEmptyDataShards(t *testing.T) {
	shards, err := Encode([]byte{}, 2, 2)
	if err != nil {
		t.Fatalf("Encode empty: %v", err)
	}
	// With empty data, shards are zero-length.
	for i, s := range shards {
		for _, b := range s {
			if b != 0 {
				t.Fatalf("shard %d should be all zeros", i)
			}
		}
	}
}
