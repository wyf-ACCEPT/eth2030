package types

import (
	"math/big"
	"testing"
)

func TestHeaderRLPRoundTrip(t *testing.T) {
	blobGasUsed := uint64(131072)
	excessBlobGas := uint64(0)
	beaconRoot := HexToHash("0xbeac")
	reqHash := HexToHash("0x7685")

	h := &Header{
		ParentHash:       HexToHash("0x1111"),
		UncleHash:        EmptyUncleHash,
		Coinbase:         HexToAddress("0xaabbcc"),
		Root:             EmptyRootHash,
		TxHash:           EmptyRootHash,
		ReceiptHash:      EmptyRootHash,
		Difficulty:        big.NewInt(0),
		Number:           big.NewInt(100),
		GasLimit:         30_000_000,
		GasUsed:          21_000,
		Time:             1700000000,
		Extra:            []byte("eth2028"),
		BaseFee:          big.NewInt(1_000_000_000),
		WithdrawalsHash:  &EmptyRootHash,
		BlobGasUsed:      &blobGasUsed,
		ExcessBlobGas:    &excessBlobGas,
		ParentBeaconRoot: &beaconRoot,
		RequestsHash:     &reqHash,
	}

	enc, err := h.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP failed: %v", err)
	}
	if len(enc) == 0 {
		t.Fatal("EncodeRLP returned empty bytes")
	}

	decoded, err := DecodeHeaderRLP(enc)
	if err != nil {
		t.Fatalf("DecodeHeaderRLP failed: %v", err)
	}

	if decoded.ParentHash != h.ParentHash {
		t.Fatalf("ParentHash mismatch: got %x, want %x", decoded.ParentHash, h.ParentHash)
	}
	if decoded.UncleHash != h.UncleHash {
		t.Fatal("UncleHash mismatch")
	}
	if decoded.Coinbase != h.Coinbase {
		t.Fatal("Coinbase mismatch")
	}
	if decoded.Root != h.Root {
		t.Fatal("Root mismatch")
	}
	if decoded.TxHash != h.TxHash {
		t.Fatal("TxHash mismatch")
	}
	if decoded.ReceiptHash != h.ReceiptHash {
		t.Fatal("ReceiptHash mismatch")
	}
	if decoded.Bloom != h.Bloom {
		t.Fatal("Bloom mismatch")
	}
	if decoded.Difficulty.Cmp(h.Difficulty) != 0 {
		t.Fatalf("Difficulty mismatch: got %v, want %v", decoded.Difficulty, h.Difficulty)
	}
	if decoded.Number.Cmp(h.Number) != 0 {
		t.Fatalf("Number mismatch: got %v, want %v", decoded.Number, h.Number)
	}
	if decoded.GasLimit != h.GasLimit {
		t.Fatalf("GasLimit mismatch: got %d, want %d", decoded.GasLimit, h.GasLimit)
	}
	if decoded.GasUsed != h.GasUsed {
		t.Fatalf("GasUsed mismatch: got %d, want %d", decoded.GasUsed, h.GasUsed)
	}
	if decoded.Time != h.Time {
		t.Fatalf("Time mismatch: got %d, want %d", decoded.Time, h.Time)
	}
	if string(decoded.Extra) != string(h.Extra) {
		t.Fatal("Extra mismatch")
	}
	if decoded.MixDigest != h.MixDigest {
		t.Fatal("MixDigest mismatch")
	}
	if decoded.Nonce != h.Nonce {
		t.Fatal("Nonce mismatch")
	}
	if decoded.BaseFee.Cmp(h.BaseFee) != 0 {
		t.Fatalf("BaseFee mismatch: got %v, want %v", decoded.BaseFee, h.BaseFee)
	}
	if decoded.WithdrawalsHash == nil || *decoded.WithdrawalsHash != *h.WithdrawalsHash {
		t.Fatal("WithdrawalsHash mismatch")
	}
	if decoded.BlobGasUsed == nil || *decoded.BlobGasUsed != *h.BlobGasUsed {
		t.Fatal("BlobGasUsed mismatch")
	}
	if decoded.ExcessBlobGas == nil || *decoded.ExcessBlobGas != *h.ExcessBlobGas {
		t.Fatal("ExcessBlobGas mismatch")
	}
	if decoded.ParentBeaconRoot == nil || *decoded.ParentBeaconRoot != *h.ParentBeaconRoot {
		t.Fatal("ParentBeaconRoot mismatch")
	}
	if decoded.RequestsHash == nil || *decoded.RequestsHash != *h.RequestsHash {
		t.Fatal("RequestsHash mismatch")
	}
}

func TestHeaderRLPMinimalFields(t *testing.T) {
	// Pre-London header with no optional fields.
	h := &Header{
		Difficulty: big.NewInt(1000000),
		Number:     big.NewInt(42),
		GasLimit:   8_000_000,
		GasUsed:    21_000,
		Time:       1600000000,
		Extra:      []byte{},
	}

	enc, err := h.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP failed: %v", err)
	}

	decoded, err := DecodeHeaderRLP(enc)
	if err != nil {
		t.Fatalf("DecodeHeaderRLP failed: %v", err)
	}

	if decoded.Number.Cmp(h.Number) != 0 {
		t.Fatalf("Number mismatch: got %v, want %v", decoded.Number, h.Number)
	}
	if decoded.Difficulty.Cmp(h.Difficulty) != 0 {
		t.Fatalf("Difficulty mismatch: got %v, want %v", decoded.Difficulty, h.Difficulty)
	}
	if decoded.BaseFee != nil {
		t.Fatal("BaseFee should be nil for pre-London header")
	}
	if decoded.WithdrawalsHash != nil {
		t.Fatal("WithdrawalsHash should be nil for pre-Shanghai header")
	}
	if decoded.BlobGasUsed != nil {
		t.Fatal("BlobGasUsed should be nil for pre-Cancun header")
	}
}

func TestHeaderHashConsistency(t *testing.T) {
	h := &Header{
		ParentHash: HexToHash("0xabcdef"),
		Number:     big.NewInt(1),
		Difficulty: big.NewInt(0),
		GasLimit:   30_000_000,
		Time:       1700000000,
	}

	hash1 := h.Hash()
	hash2 := h.Hash()
	if hash1 != hash2 {
		t.Fatal("Hash() should be consistent")
	}
	// Hash should not be zero since the header has actual data.
	if hash1.IsZero() {
		t.Fatal("Hash() should not return zero hash for a populated header")
	}
}

func TestHeaderHashDifferentHeaders(t *testing.T) {
	h1 := &Header{
		Number:     big.NewInt(1),
		Difficulty: big.NewInt(0),
	}
	h2 := &Header{
		Number:     big.NewInt(2),
		Difficulty: big.NewInt(0),
	}

	if h1.Hash() == h2.Hash() {
		t.Fatal("Different headers should have different hashes")
	}
}

func TestHeaderRLPNilBigInts(t *testing.T) {
	// Test that nil Difficulty/Number are handled gracefully (encoded as 0).
	h := &Header{}

	enc, err := h.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP failed with nil big.Int fields: %v", err)
	}

	decoded, err := DecodeHeaderRLP(enc)
	if err != nil {
		t.Fatalf("DecodeHeaderRLP failed: %v", err)
	}

	if decoded.Difficulty.Sign() != 0 {
		t.Fatal("Decoded nil Difficulty should be zero")
	}
	if decoded.Number.Sign() != 0 {
		t.Fatal("Decoded nil Number should be zero")
	}
}
