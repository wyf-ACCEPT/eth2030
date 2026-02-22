package erasure

import (
	"bytes"
	"testing"
)

// --- Encoder creation tests ---

func TestNewRSEncoderGF256Valid(t *testing.T) {
	enc, err := NewRSEncoderGF256(4, 2)
	if err != nil {
		t.Fatalf("NewRSEncoderGF256: %v", err)
	}
	if enc.DataShards() != 4 {
		t.Fatalf("expected 4 data shards, got %d", enc.DataShards())
	}
	if enc.ParityShards() != 2 {
		t.Fatalf("expected 2 parity shards, got %d", enc.ParityShards())
	}
	if enc.TotalShards() != 6 {
		t.Fatalf("expected 6 total shards, got %d", enc.TotalShards())
	}
}

func TestNewRSEncoderGF256InvalidConfig(t *testing.T) {
	tests := []struct {
		name   string
		data   int
		parity int
	}{
		{"zero data", 0, 2},
		{"zero parity", 2, 0},
		{"negative data", -1, 2},
		{"negative parity", 2, -1},
	}
	for _, tt := range tests {
		_, err := NewRSEncoderGF256(tt.data, tt.parity)
		if err == nil {
			t.Fatalf("%s: expected error", tt.name)
		}
	}
}

func TestNewRSEncoderGF256ExceedsMaxShards(t *testing.T) {
	_, err := NewRSEncoderGF256(200, 100) // 300 > 255
	if err == nil {
		t.Fatal("expected error for exceeding MaxGF256Shards")
	}
}

// --- Encode/Decode roundtrip tests ---

func TestRSEncoderGF256EncodeDecodeRoundtrip(t *testing.T) {
	enc, err := NewRSEncoderGF256(4, 2)
	if err != nil {
		t.Fatalf("NewRSEncoderGF256: %v", err)
	}

	data := []byte("hello world, this is Reed-Solomon over GF(256)!")
	shards, err := enc.Encode(data)
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

	// Reconstruct with all shards present.
	recovered, err := enc.ReconstructData(shards)
	if err != nil {
		t.Fatalf("ReconstructData: %v", err)
	}

	if !bytes.Equal(recovered[:len(data)], data) {
		t.Fatalf("data mismatch:\n  got:  %q\n  want: %q", recovered[:len(data)], data)
	}
}

func TestRSEncoderGF256ReconstructOneMissing(t *testing.T) {
	enc, err := NewRSEncoderGF256(4, 4)
	if err != nil {
		t.Fatalf("NewRSEncoderGF256: %v", err)
	}

	data := []byte("reconstruct with one missing shard test data")
	shards, err := enc.Encode(data)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Remove one shard.
	shards[1] = nil

	recovered, err := enc.ReconstructData(shards)
	if err != nil {
		t.Fatalf("ReconstructData: %v", err)
	}

	if !bytes.Equal(recovered[:len(data)], data) {
		t.Fatalf("data mismatch after reconstruction")
	}
}

func TestRSEncoderGF256ReconstructMultipleMissing(t *testing.T) {
	enc, err := NewRSEncoderGF256(4, 4)
	if err != nil {
		t.Fatalf("NewRSEncoderGF256: %v", err)
	}

	data := []byte("multiple missing shards recovery test")
	shards, err := enc.Encode(data)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Remove 3 shards (still have 5 >= 4 required).
	shards[0] = nil
	shards[2] = nil
	shards[5] = nil

	recovered, err := enc.ReconstructData(shards)
	if err != nil {
		t.Fatalf("ReconstructData: %v", err)
	}

	if !bytes.Equal(recovered[:len(data)], data) {
		t.Fatalf("data mismatch after multi-shard reconstruction")
	}
}

func TestRSEncoderGF256ReconstructMaxErasure(t *testing.T) {
	enc, err := NewRSEncoderGF256(4, 4)
	if err != nil {
		t.Fatalf("NewRSEncoderGF256: %v", err)
	}

	data := []byte("max erasure: exactly k shards remaining")
	shards, err := enc.Encode(data)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Remove all parity shards (keep exactly k=4 data shards).
	shards[4] = nil
	shards[5] = nil
	shards[6] = nil
	shards[7] = nil

	recovered, err := enc.ReconstructData(shards)
	if err != nil {
		t.Fatalf("ReconstructData: %v", err)
	}

	if !bytes.Equal(recovered[:len(data)], data) {
		t.Fatalf("data mismatch at max erasure")
	}
}

func TestRSEncoderGF256ReconstructFromParityOnly(t *testing.T) {
	enc, err := NewRSEncoderGF256(3, 5)
	if err != nil {
		t.Fatalf("NewRSEncoderGF256: %v", err)
	}

	data := []byte("recover from only parity shards!")
	shards, err := enc.Encode(data)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Remove all "data" shards, keep only the last 5 (enough for k=3).
	shards[0] = nil
	shards[1] = nil
	shards[2] = nil

	recovered, err := enc.ReconstructData(shards)
	if err != nil {
		t.Fatalf("ReconstructData: %v", err)
	}

	if !bytes.Equal(recovered[:len(data)], data) {
		t.Fatalf("data mismatch when reconstructing from parity only")
	}
}

func TestRSEncoderGF256TooFewShards(t *testing.T) {
	enc, err := NewRSEncoderGF256(4, 2)
	if err != nil {
		t.Fatalf("NewRSEncoderGF256: %v", err)
	}

	data := []byte("too few shards test")
	shards, err := enc.Encode(data)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Remove too many: 3 missing out of 6, only 3 remaining < 4 required.
	shards[0] = nil
	shards[2] = nil
	shards[4] = nil

	_, err = enc.ReconstructData(shards)
	if err == nil {
		t.Fatal("expected error with too few shards")
	}
}

// --- Integrity verification tests ---

func TestRSEncoderGF256VerifyIntegrityValid(t *testing.T) {
	enc, err := NewRSEncoderGF256(4, 3)
	if err != nil {
		t.Fatalf("NewRSEncoderGF256: %v", err)
	}

	data := []byte("integrity check valid data here!")
	shards, err := enc.Encode(data)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	valid, err := enc.VerifyIntegrity(shards)
	if err != nil {
		t.Fatalf("VerifyIntegrity: %v", err)
	}
	if !valid {
		t.Fatal("expected valid integrity")
	}
}

func TestRSEncoderGF256VerifyIntegrityCorrupted(t *testing.T) {
	enc, err := NewRSEncoderGF256(4, 3)
	if err != nil {
		t.Fatalf("NewRSEncoderGF256: %v", err)
	}

	data := []byte("integrity check will detect corruption")
	shards, err := enc.Encode(data)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Corrupt a parity shard.
	shards[5][0] ^= 0xFF

	valid, err := enc.VerifyIntegrity(shards)
	if err != nil {
		t.Fatalf("VerifyIntegrity: %v", err)
	}
	if valid {
		t.Fatal("expected invalid integrity after corruption")
	}
}

// --- Corruption detection tests ---

func TestRSEncoderGF256DetectCorruption(t *testing.T) {
	enc, err := NewRSEncoderGF256(4, 4)
	if err != nil {
		t.Fatalf("NewRSEncoderGF256: %v", err)
	}

	data := []byte("detect which parity shards are corrupted")
	shards, err := enc.Encode(data)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Corrupt parity shard 6 (index 6).
	shards[6][0] ^= 0xAA

	corrupted, err := enc.DetectCorruption(shards)
	if err != nil {
		t.Fatalf("DetectCorruption: %v", err)
	}
	if len(corrupted) == 0 {
		t.Fatal("expected to detect corruption")
	}

	found := false
	for _, idx := range corrupted {
		if idx == 6 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected shard 6 in corrupted list, got %v", corrupted)
	}
}

func TestRSEncoderGF256DetectNoCorruption(t *testing.T) {
	enc, err := NewRSEncoderGF256(4, 2)
	if err != nil {
		t.Fatalf("NewRSEncoderGF256: %v", err)
	}

	data := []byte("no corruption here")
	shards, err := enc.Encode(data)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	corrupted, err := enc.DetectCorruption(shards)
	if err != nil {
		t.Fatalf("DetectCorruption: %v", err)
	}
	if len(corrupted) != 0 {
		t.Fatalf("expected no corruption, got %v", corrupted)
	}
}

// --- Varying data sizes ---

func TestRSEncoderGF256VaryingSizes(t *testing.T) {
	enc, err := NewRSEncoderGF256(4, 4)
	if err != nil {
		t.Fatalf("NewRSEncoderGF256: %v", err)
	}

	sizes := []int{1, 3, 7, 16, 64, 255, 512, 1024}
	for _, size := range sizes {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte((i*7 + 13) % 256)
		}

		shards, err := enc.Encode(data)
		if err != nil {
			t.Fatalf("size %d: Encode: %v", size, err)
		}

		// Remove 2 shards (still 6 >= 4).
		shards[1] = nil
		shards[5] = nil

		recovered, err := enc.ReconstructData(shards)
		if err != nil {
			t.Fatalf("size %d: ReconstructData: %v", size, err)
		}

		if !bytes.Equal(recovered[:len(data)], data) {
			t.Fatalf("size %d: data mismatch", size)
		}
	}
}

// --- Edge cases ---

func TestRSEncoderGF256EncodeEmptyData(t *testing.T) {
	enc, err := NewRSEncoderGF256(4, 2)
	if err != nil {
		t.Fatalf("NewRSEncoderGF256: %v", err)
	}

	_, err = enc.Encode([]byte{})
	if err != ErrRSEncEmptyInput {
		t.Fatalf("expected ErrRSEncEmptyInput, got %v", err)
	}
}

func TestRSEncoderGF256ReconstructWrongShardCount(t *testing.T) {
	enc, err := NewRSEncoderGF256(4, 2)
	if err != nil {
		t.Fatalf("NewRSEncoderGF256: %v", err)
	}

	shards := make([][]byte, 3) // wrong count
	_, err = enc.Reconstruct(shards)
	if err == nil {
		t.Fatal("expected error for wrong shard count")
	}
}

func TestRSEncoderGF256ReconstructAllNil(t *testing.T) {
	enc, err := NewRSEncoderGF256(4, 2)
	if err != nil {
		t.Fatalf("NewRSEncoderGF256: %v", err)
	}

	shards := make([][]byte, 6) // all nil
	_, err = enc.Reconstruct(shards)
	if err == nil {
		t.Fatal("expected error with all nil shards")
	}
}

func TestRSEncoderGF256SingleDataShard(t *testing.T) {
	enc, err := NewRSEncoderGF256(1, 2)
	if err != nil {
		t.Fatalf("NewRSEncoderGF256: %v", err)
	}

	data := []byte("single shard")
	shards, err := enc.Encode(data)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Remove the first shard, keep others.
	shards[0] = nil

	recovered, err := enc.ReconstructData(shards)
	if err != nil {
		t.Fatalf("ReconstructData: %v", err)
	}

	if !bytes.Equal(recovered[:len(data)], data) {
		t.Fatalf("single shard recovery mismatch")
	}
}

func TestRSEncoderGF256MixedIndicesReconstruction(t *testing.T) {
	// Test reconstruction using a mix of data and parity shards.
	enc, err := NewRSEncoderGF256(4, 4)
	if err != nil {
		t.Fatalf("NewRSEncoderGF256: %v", err)
	}

	data := []byte("mixed indices: keep shards 0, 3, 5, 7")
	shards, err := enc.Encode(data)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Keep only shards 0, 3, 5, 7 (exactly k=4).
	shards[1] = nil
	shards[2] = nil
	shards[4] = nil
	shards[6] = nil

	recovered, err := enc.ReconstructData(shards)
	if err != nil {
		t.Fatalf("ReconstructData: %v", err)
	}

	if !bytes.Equal(recovered[:len(data)], data) {
		t.Fatalf("mixed indices reconstruction mismatch")
	}
}

func TestRSEncoderGF256VerifyIntegrityShardCountError(t *testing.T) {
	enc, err := NewRSEncoderGF256(4, 2)
	if err != nil {
		t.Fatalf("NewRSEncoderGF256: %v", err)
	}

	_, err = enc.VerifyIntegrity(make([][]byte, 3))
	if err == nil {
		t.Fatal("expected error for wrong shard count")
	}
}

func TestRSEncoderGF256EncodeShardsWrongCount(t *testing.T) {
	enc, err := NewRSEncoderGF256(4, 2)
	if err != nil {
		t.Fatalf("NewRSEncoderGF256: %v", err)
	}

	// Pass wrong number of data shards.
	_, err = enc.EncodeShards([][]byte{{1, 2}, {3, 4}})
	if err == nil {
		t.Fatal("expected error for wrong data shard count")
	}
}

func TestRSEncoderGF256LargeDataReconstruct(t *testing.T) {
	enc, err := NewRSEncoderGF256(8, 4)
	if err != nil {
		t.Fatalf("NewRSEncoderGF256: %v", err)
	}

	data := make([]byte, 2048)
	for i := range data {
		data[i] = byte(i % 256)
	}

	shards, err := enc.Encode(data)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Remove 4 shards (still have 8 >= 8 needed).
	shards[0] = nil
	shards[3] = nil
	shards[7] = nil
	shards[10] = nil

	recovered, err := enc.ReconstructData(shards)
	if err != nil {
		t.Fatalf("ReconstructData: %v", err)
	}

	if !bytes.Equal(recovered[:len(data)], data) {
		t.Fatal("large data roundtrip mismatch")
	}
}

func TestRSEncoderGF256ReconstructAllShards(t *testing.T) {
	// Verify that Reconstruct properly regenerates all shards.
	enc, err := NewRSEncoderGF256(4, 4)
	if err != nil {
		t.Fatalf("NewRSEncoderGF256: %v", err)
	}

	data := []byte("test reconstruct all shards")
	original, err := enc.Encode(data)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Remove some shards.
	partial := make([][]byte, len(original))
	copy(partial, original)
	partial[1] = nil
	partial[3] = nil

	reconstructed, err := enc.Reconstruct(partial)
	if err != nil {
		t.Fatalf("Reconstruct: %v", err)
	}

	// Verify all shards match the original.
	for i := range original {
		if !bytes.Equal(reconstructed[i], original[i]) {
			t.Fatalf("shard %d mismatch after full reconstruction", i)
		}
	}
}

func TestRSEncoderGF256ReconstructDataWrongShardCount(t *testing.T) {
	enc, err := NewRSEncoderGF256(4, 2)
	if err != nil {
		t.Fatalf("NewRSEncoderGF256: %v", err)
	}

	_, err = enc.ReconstructData(make([][]byte, 3))
	if err == nil {
		t.Fatal("expected error for wrong shard count")
	}
}
