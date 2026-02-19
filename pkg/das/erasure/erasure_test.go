package erasure

import (
	"bytes"
	"testing"
)

func TestEncodeDecodeRoundtrip(t *testing.T) {
	data := []byte("hello world, this is test data for erasure coding!")
	dataShards := 4
	parityShards := 2

	shards, err := Encode(data, dataShards, parityShards)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(shards) != dataShards+parityShards {
		t.Fatalf("expected %d shards, got %d", dataShards+parityShards, len(shards))
	}

	// Verify shard sizes are uniform.
	for i := 1; i < len(shards); i++ {
		if len(shards[i]) != len(shards[0]) {
			t.Fatalf("shard %d size %d != shard 0 size %d", i, len(shards[i]), len(shards[0]))
		}
	}

	// Decode with all shards present.
	recovered, err := Decode(shards, dataShards, parityShards)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	// Recovered data is padded to shard boundary.
	if !bytes.Equal(recovered[:len(data)], data) {
		t.Fatalf("recovered data mismatch: got %q, want %q", recovered[:len(data)], data)
	}
}

func TestDecodeWithMissingShard(t *testing.T) {
	data := []byte("test data for recovery with missing shards")
	dataShards := 4
	parityShards := 4

	shards, err := Encode(data, dataShards, parityShards)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Remove one data shard.
	shards[2] = nil

	recovered, err := Decode(shards, dataShards, parityShards)
	if err != nil {
		t.Fatalf("Decode with missing shard: %v", err)
	}

	if !bytes.Equal(recovered[:len(data)], data) {
		t.Fatalf("recovered data mismatch after erasure")
	}
}

func TestDecodeTooFewShards(t *testing.T) {
	data := []byte("short data")
	dataShards := 4
	parityShards := 2

	shards, err := Encode(data, dataShards, parityShards)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Remove too many shards.
	shards[0] = nil
	shards[1] = nil
	shards[2] = nil

	_, err = Decode(shards, dataShards, parityShards)
	if err == nil {
		t.Fatal("expected error with too few shards")
	}
}

func TestEncodeInvalidConfig(t *testing.T) {
	_, err := Encode([]byte("data"), 0, 2)
	if err != ErrInvalidShardConfig {
		t.Fatalf("expected ErrInvalidShardConfig, got %v", err)
	}

	_, err = Encode([]byte("data"), 2, 0)
	if err != ErrInvalidShardConfig {
		t.Fatalf("expected ErrInvalidShardConfig, got %v", err)
	}
}

func TestDecodeInvalidShardCount(t *testing.T) {
	_, err := Decode([][]byte{{1}, {2}}, 4, 2)
	if err == nil {
		t.Fatal("expected error for wrong shard count")
	}
}

func TestDecodeShardSizeMismatch(t *testing.T) {
	shards := [][]byte{
		{1, 2, 3},
		{4, 5},
		{6, 7, 8},
		{9, 10, 11},
		{12, 13, 14},
		{15, 16, 17},
	}
	_, err := Decode(shards, 4, 2)
	if err != ErrShardSizeMismatch {
		t.Fatalf("expected ErrShardSizeMismatch, got %v", err)
	}
}

func TestEncodeEmptyData(t *testing.T) {
	shards, err := Encode([]byte{}, 2, 1)
	if err != nil {
		t.Fatalf("Encode empty: %v", err)
	}
	// All shards should be zero-filled.
	for i, s := range shards {
		for _, b := range s {
			if b != 0 {
				t.Fatalf("shard %d should be all zeros", i)
			}
		}
	}
}

func TestEncodeLargeData(t *testing.T) {
	// Test with data larger than typical.
	data := make([]byte, 1024)
	for i := range data {
		data[i] = byte(i % 256)
	}

	shards, err := Encode(data, 8, 4)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	recovered, err := Decode(shards, 8, 4)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if !bytes.Equal(recovered[:len(data)], data) {
		t.Fatal("large data roundtrip failed")
	}
}
