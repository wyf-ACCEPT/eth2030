package types

import (
	"math/big"
	"testing"
)

func TestHeaderFields(t *testing.T) {
	parentHash := HexToHash("0x1111")
	uncleHash := EmptyUncleHash
	coinbase := HexToAddress("0xaabbcc")
	blobGasUsed := uint64(131072)
	excessBlobGas := uint64(0)
	beaconRoot := HexToHash("0xbeac")
	reqHash := HexToHash("0x7685")
	balHash := HexToHash("0x7928")

	h := &Header{
		ParentHash:          parentHash,
		UncleHash:           uncleHash,
		Coinbase:            coinbase,
		Root:                EmptyRootHash,
		TxHash:              EmptyRootHash,
		ReceiptHash:         EmptyRootHash,
		Difficulty:          big.NewInt(0),
		Number:              big.NewInt(100),
		GasLimit:            30_000_000,
		GasUsed:             21_000,
		Time:                1700000000,
		Extra:               []byte("eth2030"),
		BaseFee:             big.NewInt(1_000_000_000),
		BlobGasUsed:         &blobGasUsed,
		ExcessBlobGas:       &excessBlobGas,
		ParentBeaconRoot:    &beaconRoot,
		RequestsHash:        &reqHash,
		BlockAccessListHash: &balHash,
	}

	if h.ParentHash != parentHash {
		t.Fatal("ParentHash mismatch")
	}
	if h.UncleHash != uncleHash {
		t.Fatal("UncleHash mismatch")
	}
	if h.Coinbase != coinbase {
		t.Fatal("Coinbase mismatch")
	}
	if h.Number.Int64() != 100 {
		t.Fatal("Number mismatch")
	}
	if h.GasLimit != 30_000_000 {
		t.Fatal("GasLimit mismatch")
	}
	if h.GasUsed != 21_000 {
		t.Fatal("GasUsed mismatch")
	}
	if h.Time != 1700000000 {
		t.Fatal("Time mismatch")
	}
	if string(h.Extra) != "eth2030" {
		t.Fatal("Extra mismatch")
	}
	if h.BaseFee.Int64() != 1_000_000_000 {
		t.Fatal("BaseFee mismatch")
	}
	if *h.BlobGasUsed != 131072 {
		t.Fatal("BlobGasUsed mismatch")
	}
	if *h.ExcessBlobGas != 0 {
		t.Fatal("ExcessBlobGas mismatch")
	}
	if *h.ParentBeaconRoot != beaconRoot {
		t.Fatal("ParentBeaconRoot mismatch")
	}
	if *h.RequestsHash != reqHash {
		t.Fatal("RequestsHash mismatch")
	}
	if *h.BlockAccessListHash != balHash {
		t.Fatal("BlockAccessListHash mismatch")
	}
}

func TestHeaderHash(t *testing.T) {
	h := &Header{
		Number: big.NewInt(1),
	}
	// Placeholder returns empty hash; just verify it doesn't panic and is cacheable.
	hash1 := h.Hash()
	hash2 := h.Hash()
	if hash1 != hash2 {
		t.Fatal("Hash() should be consistent")
	}
}

func TestHeaderSize(t *testing.T) {
	h := &Header{
		Difficulty: big.NewInt(1),
		Number:     big.NewInt(1),
		BaseFee:    big.NewInt(1),
		Extra:      make([]byte, 32),
	}
	size := h.Size()
	if size == 0 {
		t.Fatal("Header size should be non-zero")
	}
	// Should be cached on second call.
	size2 := h.Size()
	if size != size2 {
		t.Fatal("Header size should be cached")
	}
}

func TestHeaderNilOptionalFields(t *testing.T) {
	h := &Header{
		Difficulty: big.NewInt(0),
		Number:     big.NewInt(0),
	}
	if h.WithdrawalsHash != nil {
		t.Fatal("WithdrawalsHash should be nil for pre-Shanghai")
	}
	if h.BlobGasUsed != nil {
		t.Fatal("BlobGasUsed should be nil for pre-Cancun")
	}
	if h.ExcessBlobGas != nil {
		t.Fatal("ExcessBlobGas should be nil for pre-Cancun")
	}
	if h.ParentBeaconRoot != nil {
		t.Fatal("ParentBeaconRoot should be nil for pre-Cancun")
	}
	if h.RequestsHash != nil {
		t.Fatal("RequestsHash should be nil for pre-Prague")
	}
	if h.BlockAccessListHash != nil {
		t.Fatal("BlockAccessListHash should be nil for pre-EIP-7928")
	}
}
