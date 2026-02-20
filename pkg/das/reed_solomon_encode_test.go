package das

import (
	"bytes"
	"testing"
)

func TestNewRSEncoder(t *testing.T) {
	enc, err := NewRSEncoder(4, 4)
	if err != nil {
		t.Fatalf("NewRSEncoder(4, 4): %v", err)
	}
	if enc.DataShards() != 4 {
		t.Errorf("DataShards() = %d, want 4", enc.DataShards())
	}
	if enc.ParityShards() != 4 {
		t.Errorf("ParityShards() = %d, want 4", enc.ParityShards())
	}
	if enc.TotalShards() != 8 {
		t.Errorf("TotalShards() = %d, want 8", enc.TotalShards())
	}
	if enc.GeneratorDegree() != 4 {
		t.Errorf("GeneratorDegree() = %d, want 4", enc.GeneratorDegree())
	}
}

func TestNewRSEncoderInvalid(t *testing.T) {
	tests := []struct {
		name   string
		data   int
		parity int
	}{
		{"zero data", 0, 4},
		{"zero parity", 4, 0},
		{"negative data", -1, 4},
		{"negative parity", 4, -1},
	}
	for _, tt := range tests {
		_, err := NewRSEncoder(tt.data, tt.parity)
		if err == nil {
			t.Errorf("NewRSEncoder(%d, %d): expected error for %s", tt.data, tt.parity, tt.name)
		}
	}
}

func TestRSEncodeProducesCorrectShardCount(t *testing.T) {
	enc, err := NewRSEncoder(4, 4)
	if err != nil {
		t.Fatalf("NewRSEncoder: %v", err)
	}

	shardSize := 8
	data := make([][]byte, 4)
	for i := range data {
		data[i] = make([]byte, shardSize)
		for j := range data[i] {
			data[i][j] = byte(i*shardSize + j + 1)
		}
	}

	encoded, err := enc.Encode(data)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	if len(encoded) != 8 {
		t.Fatalf("encoded shard count = %d, want 8", len(encoded))
	}

	// All shards should have the same size.
	for i, s := range encoded {
		if len(s) != shardSize {
			t.Errorf("shard %d size = %d, want %d", i, len(s), shardSize)
		}
	}

	// Not all shards should be zero.
	allZero := true
	for _, s := range encoded {
		for _, b := range s {
			if b != 0 {
				allZero = false
				break
			}
		}
	}
	if allZero {
		t.Error("all shards are zero; encoding likely failed")
	}
}

func TestRSEncodeVerifyParity(t *testing.T) {
	enc, err := NewRSEncoder(4, 4)
	if err != nil {
		t.Fatalf("NewRSEncoder: %v", err)
	}

	shardSize := 10
	data := make([][]byte, 4)
	for i := range data {
		data[i] = make([]byte, shardSize)
		for j := range data[i] {
			data[i][j] = byte((i*7 + j*13) & 0xFF)
		}
	}

	encoded, err := enc.Encode(data)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Parity should verify.
	ok, err := enc.VerifyParity(encoded)
	if err != nil {
		t.Fatalf("VerifyParity: %v", err)
	}
	if !ok {
		t.Error("parity verification failed for freshly encoded data")
	}

	// Corrupt a parity shard and verify it fails.
	corrupted := make([][]byte, len(encoded))
	for i, s := range encoded {
		corrupted[i] = make([]byte, len(s))
		copy(corrupted[i], s)
	}
	corrupted[5][0] ^= 0xFF // flip bits in a parity shard

	ok, err = enc.VerifyParity(corrupted)
	if err != nil {
		t.Fatalf("VerifyParity on corrupted: %v", err)
	}
	if ok {
		t.Error("parity verification should fail for corrupted data")
	}
}

func TestRSEncodeDecodeRoundtrip(t *testing.T) {
	dataShards := 4
	parityShards := 4
	enc, err := NewRSEncoder(dataShards, parityShards)
	if err != nil {
		t.Fatalf("NewRSEncoder: %v", err)
	}

	shardSize := 6
	original := make([][]byte, dataShards)
	for i := range original {
		original[i] = make([]byte, shardSize)
		for j := range original[i] {
			original[i][j] = byte(i*10 + j + 1)
		}
	}

	encoded, err := enc.Encode(original)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Simulate losing some shards: drop shards 1 and 3 (two data evaluations).
	// Keep shards 0, 2, 5, 7 (mix of data and parity evaluations).
	availableData := [][]byte{encoded[0], encoded[2], encoded[5], encoded[7]}
	availableIndices := []int{0, 2, 5, 7}

	recovered, err := RSRecoverData(availableData, availableIndices, dataShards, parityShards)
	if err != nil {
		t.Fatalf("RSRecoverData: %v", err)
	}

	// Recovered shards should match all original encoded shards.
	if len(recovered) != len(encoded) {
		t.Fatalf("recovered shard count = %d, want %d", len(recovered), len(encoded))
	}

	for i := 0; i < len(encoded); i++ {
		if !bytes.Equal(recovered[i], encoded[i]) {
			t.Errorf("recovered shard %d differs from encoded: got %v, want %v",
				i, recovered[i], encoded[i])
		}
	}
}

func TestRSEncodeDecodeAllParityLost(t *testing.T) {
	dataShards := 4
	parityShards := 4
	enc, err := NewRSEncoder(dataShards, parityShards)
	if err != nil {
		t.Fatalf("NewRSEncoder: %v", err)
	}

	shardSize := 8
	original := make([][]byte, dataShards)
	for i := range original {
		original[i] = make([]byte, shardSize)
		for j := range original[i] {
			original[i][j] = byte(i*17 + j + 3)
		}
	}

	encoded, err := enc.Encode(original)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Keep only data shards (indices 0..3), all parity lost.
	availableData := encoded[:dataShards]
	availableIndices := []int{0, 1, 2, 3}

	recovered, err := RSRecoverData(availableData, availableIndices, dataShards, parityShards)
	if err != nil {
		t.Fatalf("RSRecoverData: %v", err)
	}

	// All shards (including parity) should be recovered.
	for i := 0; i < len(encoded); i++ {
		if !bytes.Equal(recovered[i], encoded[i]) {
			t.Errorf("shard %d mismatch after recovery from data-only", i)
		}
	}
}

func TestRSEncodeDecodeAllDataLost(t *testing.T) {
	dataShards := 4
	parityShards := 4
	enc, err := NewRSEncoder(dataShards, parityShards)
	if err != nil {
		t.Fatalf("NewRSEncoder: %v", err)
	}

	shardSize := 6
	original := make([][]byte, dataShards)
	for i := range original {
		original[i] = make([]byte, shardSize)
		for j := range original[i] {
			original[i][j] = byte(i*23 + j + 5)
		}
	}

	encoded, err := enc.Encode(original)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Keep only parity shards (indices 4..7), all data lost.
	availableData := encoded[dataShards:]
	availableIndices := []int{4, 5, 6, 7}

	recovered, err := RSRecoverData(availableData, availableIndices, dataShards, parityShards)
	if err != nil {
		t.Fatalf("RSRecoverData: %v", err)
	}

	// Data shards should be fully recovered.
	for i := 0; i < len(encoded); i++ {
		if !bytes.Equal(recovered[i], encoded[i]) {
			t.Errorf("shard %d mismatch after recovery from parity-only", i)
		}
	}
}

func TestRSEncodeBlobRoundtrip(t *testing.T) {
	numData := 4
	numParity := 4
	enc, err := NewRSEncoder(numData, numParity)
	if err != nil {
		t.Fatalf("NewRSEncoder: %v", err)
	}

	blob := []byte("Hello, PeerDAS erasure coding over GF(2^16)!")
	encoded, err := enc.EncodeBlob(blob, numData)
	if err != nil {
		t.Fatalf("EncodeBlob: %v", err)
	}

	if len(encoded) != 8 {
		t.Fatalf("encoded shard count = %d, want 8", len(encoded))
	}

	// Recover from first 4 shards (all "data" evaluations).
	availableData := [][]byte{encoded[0], encoded[1], encoded[2], encoded[3]}
	availableIndices := []int{0, 1, 2, 3}

	recovered, err := RSRecoverData(availableData, availableIndices, numData, numParity)
	if err != nil {
		t.Fatalf("RSRecoverData: %v", err)
	}

	// All recovered shards should match encoded.
	for i := 0; i < len(encoded); i++ {
		if !bytes.Equal(recovered[i], encoded[i]) {
			t.Errorf("shard %d mismatch in blob roundtrip", i)
		}
	}
}

func TestRSEncodeEmptyInput(t *testing.T) {
	enc, err := NewRSEncoder(4, 4)
	if err != nil {
		t.Fatalf("NewRSEncoder: %v", err)
	}

	_, err = enc.Encode(nil)
	if err == nil {
		t.Error("expected error for nil input")
	}

	_, err = enc.EncodeBlob(nil, 4)
	if err == nil {
		t.Error("expected error for nil blob")
	}

	_, err = enc.EncodeBlob([]byte{}, 4)
	if err == nil {
		t.Error("expected error for empty blob")
	}
}

func TestRSEncodeShardSizeMismatch(t *testing.T) {
	enc, err := NewRSEncoder(3, 3)
	if err != nil {
		t.Fatalf("NewRSEncoder: %v", err)
	}

	data := [][]byte{
		{1, 2, 3, 4},
		{5, 6, 7, 8},
		{9, 10}, // different size
	}

	_, err = enc.Encode(data)
	if err == nil {
		t.Error("expected error for mismatched shard sizes")
	}
}

func TestRSRecoverDataInsufficient(t *testing.T) {
	_, err := RSRecoverData(
		[][]byte{{1, 2}},
		[]int{0},
		4, 4,
	)
	if err == nil {
		t.Error("expected error for insufficient shards")
	}
}

func TestComputeGeneratorPoly(t *testing.T) {
	initGFTables()

	// Generator polynomial of degree n should have n+1 coefficients
	// with leading coefficient 1.
	for n := 1; n <= 8; n++ {
		gen := computeGeneratorPoly(n)
		if len(gen) != n+1 {
			t.Errorf("computeGeneratorPoly(%d): got %d coeffs, want %d", n, len(gen), n+1)
			continue
		}
		// Leading coefficient should be 1 (monic).
		if gen[n] != 1 {
			t.Errorf("computeGeneratorPoly(%d): leading coeff = %d, want 1", n, gen[n])
		}
		// The generator should have roots at a^0, a^1, ..., a^{n-1}.
		for i := 0; i < n; i++ {
			root := GFExp(i)
			val := GFPolyEval(gen, root)
			if val != 0 {
				t.Errorf("computeGeneratorPoly(%d): g(a^%d) = %d, want 0", n, i, val)
			}
		}
	}
}

func TestRSVerifyParityWrongShardCount(t *testing.T) {
	enc, err := NewRSEncoder(4, 4)
	if err != nil {
		t.Fatalf("NewRSEncoder: %v", err)
	}

	_, err = enc.VerifyParity([][]byte{{1}, {2}})
	if err == nil {
		t.Error("expected error for wrong shard count")
	}
}

func TestRSEncodeDecodeSmall(t *testing.T) {
	// Minimal config: 2 data, 2 parity.
	dataShards := 2
	parityShards := 2
	enc, err := NewRSEncoder(dataShards, parityShards)
	if err != nil {
		t.Fatalf("NewRSEncoder: %v", err)
	}

	data := [][]byte{{0xAB, 0xCD}, {0x12, 0x34}}
	encoded, err := enc.Encode(data)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Recover from shards 0 and 3 (one data, one parity).
	availableData := [][]byte{encoded[0], encoded[3]}
	availableIndices := []int{0, 3}

	recovered, err := RSRecoverData(availableData, availableIndices, dataShards, parityShards)
	if err != nil {
		t.Fatalf("RSRecoverData: %v", err)
	}

	for i := 0; i < len(encoded); i++ {
		if !bytes.Equal(recovered[i], encoded[i]) {
			t.Errorf("shard %d: got %v, want %v", i, recovered[i], encoded[i])
		}
	}
}
