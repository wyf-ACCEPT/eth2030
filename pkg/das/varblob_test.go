package das

import (
	"bytes"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

func TestNewVarBlob(t *testing.T) {
	data := make([]byte, 1000)
	for i := range data {
		data[i] = byte(i % 256)
	}

	cfg := DefaultVarBlobConfig()
	vb, err := NewVarBlob(data, cfg.MinChunkSize)
	if err != nil {
		t.Fatalf("NewVarBlob() error = %v", err)
	}

	// 1000 / 128 = 7.8125 -> 8 chunks.
	if vb.NumChunks != 8 {
		t.Errorf("NumChunks = %d, want 8", vb.NumChunks)
	}
	if vb.ChunkSize != 128 {
		t.Errorf("ChunkSize = %d, want 128", vb.ChunkSize)
	}
	// Padded data should be 8 * 128 = 1024 bytes.
	if len(vb.Data) != 1024 {
		t.Errorf("len(Data) = %d, want 1024", len(vb.Data))
	}
	if vb.BlobHash.IsZero() {
		t.Error("BlobHash should not be zero")
	}
}

func TestNewVarBlobCustomChunk(t *testing.T) {
	data := make([]byte, 5000)
	for i := range data {
		data[i] = byte(i % 256)
	}

	vb, err := NewVarBlob(data, 4096)
	if err != nil {
		t.Fatalf("NewVarBlob() error = %v", err)
	}

	// 5000 / 4096 = 1.22 -> 2 chunks.
	if vb.NumChunks != 2 {
		t.Errorf("NumChunks = %d, want 2", vb.NumChunks)
	}
	if vb.ChunkSize != 4096 {
		t.Errorf("ChunkSize = %d, want 4096", vb.ChunkSize)
	}
	// Padded: 2 * 4096 = 8192.
	if len(vb.Data) != 8192 {
		t.Errorf("len(Data) = %d, want 8192", len(vb.Data))
	}
}

func TestNewVarBlobTooLarge(t *testing.T) {
	cfg := DefaultVarBlobConfig()
	data := make([]byte, cfg.MaxBlobSize+1)

	_, err := NewVarBlob(data, cfg.MinChunkSize)
	if err == nil {
		t.Fatal("expected error for oversized blob, got nil")
	}
}

func TestNewVarBlobInvalidChunkSize(t *testing.T) {
	data := make([]byte, 256)

	tests := []struct {
		name      string
		chunkSize int
	}{
		{"non-power-of-2", 100},
		{"too small", 64},
		{"too large", 8192},
		{"zero", 0},
		{"negative equivalent", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewVarBlob(data, tt.chunkSize)
			if err == nil {
				t.Errorf("expected error for chunkSize=%d, got nil", tt.chunkSize)
			}
		})
	}
}

func TestVarBlobChunks(t *testing.T) {
	// Use data that doesn't evenly divide into chunks to test padding.
	data := []byte("hello world, this is a test of variable blob chunking!")
	vb, err := NewVarBlob(data, 128)
	if err != nil {
		t.Fatalf("NewVarBlob() error = %v", err)
	}

	chunks := vb.Chunks()
	if len(chunks) != vb.NumChunks {
		t.Fatalf("len(chunks) = %d, want %d", len(chunks), vb.NumChunks)
	}

	// Each chunk must be exactly ChunkSize bytes.
	for i, chunk := range chunks {
		if len(chunk) != vb.ChunkSize {
			t.Errorf("chunk[%d] length = %d, want %d", i, len(chunk), vb.ChunkSize)
		}
	}

	// Reconstruct data from chunks and verify it matches padded data.
	var reconstructed []byte
	for _, chunk := range chunks {
		reconstructed = append(reconstructed, chunk...)
	}
	if !bytes.Equal(reconstructed, vb.Data) {
		t.Error("reconstructed data does not match padded blob data")
	}

	// Original data should be a prefix of padded data.
	if !bytes.HasPrefix(vb.Data, data) {
		t.Error("original data is not a prefix of padded data")
	}

	// Padding bytes should be zero.
	for i := len(data); i < len(vb.Data); i++ {
		if vb.Data[i] != 0 {
			t.Errorf("padding byte at index %d is %d, want 0", i, vb.Data[i])
			break
		}
	}
}

func TestVarBlobEncodeDecode(t *testing.T) {
	data := make([]byte, 500)
	for i := range data {
		data[i] = byte(i % 256)
	}

	original, err := NewVarBlob(data, 256)
	if err != nil {
		t.Fatalf("NewVarBlob() error = %v", err)
	}

	encoded := original.Encode()
	decoded, err := DecodeVarBlob(encoded)
	if err != nil {
		t.Fatalf("DecodeVarBlob() error = %v", err)
	}

	if decoded.ChunkSize != original.ChunkSize {
		t.Errorf("ChunkSize = %d, want %d", decoded.ChunkSize, original.ChunkSize)
	}
	if decoded.NumChunks != original.NumChunks {
		t.Errorf("NumChunks = %d, want %d", decoded.NumChunks, original.NumChunks)
	}
	if !bytes.Equal(decoded.Data, original.Data) {
		t.Error("decoded data does not match original")
	}
}

func TestVarBlobVerify(t *testing.T) {
	data := []byte("verify this blob data for integrity")
	vb, err := NewVarBlob(data, 128)
	if err != nil {
		t.Fatalf("NewVarBlob() error = %v", err)
	}

	// The BlobHash is computed from the original data, but Verify checks
	// the padded data. We verify against the padded data hash.
	paddedHash := crypto.Keccak256Hash(vb.Data)
	if !vb.Verify(paddedHash) {
		t.Error("Verify() returned false for correct hash")
	}
}

func TestVarBlobVerifyTampered(t *testing.T) {
	data := []byte("original blob data that should not be tampered with")
	vb, err := NewVarBlob(data, 128)
	if err != nil {
		t.Fatalf("NewVarBlob() error = %v", err)
	}

	// Record the correct hash.
	correctHash := crypto.Keccak256Hash(vb.Data)

	// Tamper with data.
	vb.Data[0] ^= 0xFF

	// Verify should fail against the original hash.
	if vb.Verify(correctHash) {
		t.Error("Verify() returned true for tampered data")
	}

	// Also check with a completely wrong hash.
	wrongHash := types.Hash{0x01, 0x02, 0x03}
	if vb.Verify(wrongHash) {
		t.Error("Verify() returned true for wrong hash")
	}
}

func TestEstimateVarBlobGas(t *testing.T) {
	tests := []struct {
		name      string
		blobSize  int
		chunkSize int
		wantGas   uint64
	}{
		{
			name:      "single chunk exactly",
			blobSize:  128,
			chunkSize: 128,
			wantGas:   21000 + 512*1, // 1 chunk
		},
		{
			name:      "two chunks",
			blobSize:  256,
			chunkSize: 128,
			wantGas:   21000 + 512*2,
		},
		{
			name:      "partial last chunk",
			blobSize:  129,
			chunkSize: 128,
			wantGas:   21000 + 512*2, // ceil(129/128) = 2
		},
		{
			name:      "large blob small chunks",
			blobSize:  4096,
			chunkSize: 128,
			wantGas:   21000 + 512*32, // 4096/128 = 32
		},
		{
			name:      "large blob large chunks",
			blobSize:  4096,
			chunkSize: 4096,
			wantGas:   21000 + 512*1,
		},
		{
			name:      "zero chunk size fallback",
			blobSize:  1000,
			chunkSize: 0,
			wantGas:   21000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gas := EstimateVarBlobGas(tt.blobSize, tt.chunkSize)
			if gas != tt.wantGas {
				t.Errorf("EstimateVarBlobGas(%d, %d) = %d, want %d",
					tt.blobSize, tt.chunkSize, gas, tt.wantGas)
			}
		})
	}
}

func TestNewVarBlobEmpty(t *testing.T) {
	_, err := NewVarBlob(nil, 128)
	if err == nil {
		t.Error("expected error for nil data, got nil")
	}

	_, err = NewVarBlob([]byte{}, 128)
	if err == nil {
		t.Error("expected error for empty data, got nil")
	}
}

func TestDecodeVarBlobErrors(t *testing.T) {
	// Too short.
	_, err := DecodeVarBlob([]byte{1, 2, 3})
	if err == nil {
		t.Error("expected error for short data")
	}

	// Length mismatch: declare 2 chunks of 128 but provide only 10 bytes of data.
	bad := make([]byte, 8+10)
	bad[0], bad[1], bad[2], bad[3] = 0, 0, 0, 128 // chunkSize = 128
	bad[4], bad[5], bad[6], bad[7] = 0, 0, 0, 2   // numChunks = 2
	_, err = DecodeVarBlob(bad)
	if err == nil {
		t.Error("expected error for length mismatch")
	}
}

func TestVarBlobConfig(t *testing.T) {
	cfg := DefaultVarBlobConfig()

	if cfg.MinChunkSize != 128 {
		t.Errorf("MinChunkSize = %d, want 128", cfg.MinChunkSize)
	}
	if cfg.MaxChunkSize != 4096 {
		t.Errorf("MaxChunkSize = %d, want 4096", cfg.MaxChunkSize)
	}
	if cfg.MaxBlobSize != 128*1024 {
		t.Errorf("MaxBlobSize = %d, want %d", cfg.MaxBlobSize, 128*1024)
	}
}
