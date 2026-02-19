package types

import (
	"math/big"
	"testing"
)

func TestNewBlobTx(t *testing.T) {
	to := HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	hashes := []Hash{
		HexToHash("0x0100000000000000000000000000000000000000000000000000000000000001"),
		HexToHash("0x0100000000000000000000000000000000000000000000000000000000000002"),
	}
	tx := NewBlobTx(
		1,     // chainID
		42,    // nonce
		21000, // gas
		&to,
		big.NewInt(1000),          // value
		big.NewInt(50_000_000_000), // maxFee
		big.NewInt(1_000_000_000),  // maxPriority
		big.NewInt(5_000_000),      // maxBlobFee
		[]byte{0xca, 0xfe},        // data
		hashes,
	)

	if tx.ChainID.Uint64() != 1 {
		t.Fatalf("ChainID = %d, want 1", tx.ChainID.Uint64())
	}
	if tx.Nonce != 42 {
		t.Fatalf("Nonce = %d, want 42", tx.Nonce)
	}
	if tx.Gas != 21000 {
		t.Fatalf("Gas = %d, want 21000", tx.Gas)
	}
	if tx.To != to {
		t.Fatal("To address mismatch")
	}
	if tx.Value.Cmp(big.NewInt(1000)) != 0 {
		t.Fatal("Value mismatch")
	}
	if tx.GasFeeCap.Cmp(big.NewInt(50_000_000_000)) != 0 {
		t.Fatal("GasFeeCap mismatch")
	}
	if tx.GasTipCap.Cmp(big.NewInt(1_000_000_000)) != 0 {
		t.Fatal("GasTipCap mismatch")
	}
	if tx.BlobFeeCap.Cmp(big.NewInt(5_000_000)) != 0 {
		t.Fatal("BlobFeeCap mismatch")
	}
	if len(tx.Data) != 2 || tx.Data[0] != 0xca || tx.Data[1] != 0xfe {
		t.Fatal("Data mismatch")
	}
	if len(tx.BlobHashes) != 2 {
		t.Fatalf("BlobHashes len = %d, want 2", len(tx.BlobHashes))
	}
}

func TestNewBlobTxNilParams(t *testing.T) {
	tx := NewBlobTx(1, 0, 21000, nil, nil, nil, nil, nil, nil, nil)
	if tx.ChainID.Uint64() != 1 {
		t.Fatal("ChainID should be 1")
	}
	if tx.Value.Sign() != 0 {
		t.Fatal("Value should be zero")
	}
	if tx.GasFeeCap.Sign() != 0 {
		t.Fatal("GasFeeCap should be zero")
	}
	if tx.GasTipCap.Sign() != 0 {
		t.Fatal("GasTipCap should be zero")
	}
	if tx.BlobFeeCap.Sign() != 0 {
		t.Fatal("BlobFeeCap should be zero")
	}
}

func TestNewBlobTxCopyIndependence(t *testing.T) {
	data := []byte{0x01, 0x02}
	hashes := []Hash{HexToHash("0x0100000000000000000000000000000000000000000000000000000000000001")}
	tx := NewBlobTx(1, 0, 21000, nil, big.NewInt(100), nil, nil, nil, data, hashes)

	// Mutate original slices.
	data[0] = 0xff
	hashes[0] = Hash{}

	if tx.Data[0] != 0x01 {
		t.Fatal("Data should be independent of original slice")
	}
	if tx.BlobHashes[0][0] != 0x01 {
		t.Fatal("BlobHashes should be independent of original slice")
	}
}

func TestBlobTxHash(t *testing.T) {
	to := HexToAddress("0xdead")
	tx := NewBlobTx(
		1, 1, 21000, &to,
		big.NewInt(0),
		big.NewInt(1000),
		big.NewInt(100),
		big.NewInt(50),
		nil,
		[]Hash{HexToHash("0x0100000000000000000000000000000000000000000000000000000000000001")},
	)

	h1 := BlobTxHash(tx)
	h2 := BlobTxHash(tx)
	if h1.IsZero() {
		t.Fatal("hash should not be zero")
	}
	if h1 != h2 {
		t.Fatal("hash should be deterministic")
	}

	// Different nonce should produce different hash.
	tx2 := NewBlobTx(
		1, 2, 21000, &to,
		big.NewInt(0),
		big.NewInt(1000),
		big.NewInt(100),
		big.NewInt(50),
		nil,
		[]Hash{HexToHash("0x0100000000000000000000000000000000000000000000000000000000000001")},
	)
	h3 := BlobTxHash(tx2)
	if h1 == h3 {
		t.Fatal("different nonces should produce different hashes")
	}
}

func TestBlobTxHashMatchesTransactionHash(t *testing.T) {
	to := HexToAddress("0xdead")
	inner := &BlobTx{
		ChainID:    big.NewInt(1),
		Nonce:      5,
		GasTipCap:  big.NewInt(100),
		GasFeeCap:  big.NewInt(1000),
		Gas:        21000,
		To:         to,
		Value:      big.NewInt(0),
		BlobFeeCap: big.NewInt(50),
		BlobHashes: []Hash{
			HexToHash("0x0100000000000000000000000000000000000000000000000000000000000001"),
		},
	}
	wrappedTx := NewTransaction(inner)
	directHash := BlobTxHash(inner)
	wrappedHash := wrappedTx.Hash()

	if directHash != wrappedHash {
		t.Fatalf("BlobTxHash and Transaction.Hash() differ: %s vs %s", directHash.Hex(), wrappedHash.Hex())
	}
}

func TestBlobGasFunc(t *testing.T) {
	tests := []struct {
		numBlobs int
		want     uint64
	}{
		{0, 0},
		{1, BlobTxBlobGasPerBlob},
		{3, 3 * BlobTxBlobGasPerBlob},
		{6, MaxBlobGasPerBlock},
	}

	for _, tt := range tests {
		hashes := make([]Hash, tt.numBlobs)
		tx := &BlobTx{BlobHashes: hashes}
		got := BlobGas(tx)
		if got != tt.want {
			t.Errorf("BlobGas(tx with %d blobs) = %d, want %d", tt.numBlobs, got, tt.want)
		}
	}
}

func TestValidateBlobVersionedHashes(t *testing.T) {
	// Valid hashes.
	validTx := &BlobTx{
		BlobHashes: []Hash{
			HexToHash("0x0100000000000000000000000000000000000000000000000000000000000001"),
			HexToHash("0x0100000000000000000000000000000000000000000000000000000000000002"),
			HexToHash("0x0100000000000000000000000000000000000000000000000000000000000003"),
		},
	}
	if err := ValidateBlobVersionedHashes(validTx); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Empty hashes.
	emptyTx := &BlobTx{BlobHashes: []Hash{}}
	if err := ValidateBlobVersionedHashes(emptyTx); err == nil {
		t.Fatal("expected error for empty blob hashes")
	}

	// Nil hashes.
	nilTx := &BlobTx{}
	if err := ValidateBlobVersionedHashes(nilTx); err == nil {
		t.Fatal("expected error for nil blob hashes")
	}

	// Invalid version byte (0x00 instead of 0x01).
	invalidTx := &BlobTx{
		BlobHashes: []Hash{
			HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001"),
		},
	}
	if err := ValidateBlobVersionedHashes(invalidTx); err == nil {
		t.Fatal("expected error for invalid version byte")
	}

	// Too many blobs.
	tooMany := make([]Hash, MaxBlobsPerBlock+1)
	for i := range tooMany {
		tooMany[i] = HexToHash("0x0100000000000000000000000000000000000000000000000000000000000001")
	}
	tooManyTx := &BlobTx{BlobHashes: tooMany}
	if err := ValidateBlobVersionedHashes(tooManyTx); err == nil {
		t.Fatal("expected error for too many blobs")
	}

	// Mixed: first valid, second invalid.
	mixedTx := &BlobTx{
		BlobHashes: []Hash{
			HexToHash("0x0100000000000000000000000000000000000000000000000000000000000001"),
			HexToHash("0x0200000000000000000000000000000000000000000000000000000000000002"),
		},
	}
	if err := ValidateBlobVersionedHashes(mixedTx); err == nil {
		t.Fatal("expected error for mixed valid/invalid hashes")
	}
}

func TestEncodeBlobTxDecodeBlobTx(t *testing.T) {
	to := HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	original := NewBlobTx(
		1, 42, 21000, &to,
		big.NewInt(1000),
		big.NewInt(50_000_000_000),
		big.NewInt(1_000_000_000),
		big.NewInt(5_000_000),
		[]byte{0xca, 0xfe},
		[]Hash{
			HexToHash("0x0100000000000000000000000000000000000000000000000000000000000001"),
			HexToHash("0x0100000000000000000000000000000000000000000000000000000000000002"),
		},
	)
	original.V = big.NewInt(1)
	original.R = big.NewInt(123456)
	original.S = big.NewInt(654321)

	encoded, err := EncodeBlobTx(original)
	if err != nil {
		t.Fatalf("EncodeBlobTx failed: %v", err)
	}
	if encoded[0] != BlobTxType {
		t.Fatalf("first byte should be 0x03, got 0x%02x", encoded[0])
	}

	decoded, err := DecodeBlobTx(encoded)
	if err != nil {
		t.Fatalf("DecodeBlobTx failed: %v", err)
	}

	// Verify all fields.
	if decoded.ChainID.Uint64() != 1 {
		t.Fatalf("ChainID = %d, want 1", decoded.ChainID.Uint64())
	}
	if decoded.Nonce != 42 {
		t.Fatalf("Nonce = %d, want 42", decoded.Nonce)
	}
	if decoded.Gas != 21000 {
		t.Fatalf("Gas = %d, want 21000", decoded.Gas)
	}
	if decoded.To != to {
		t.Fatal("To address mismatch")
	}
	if decoded.Value.Cmp(big.NewInt(1000)) != 0 {
		t.Fatal("Value mismatch")
	}
	if decoded.GasFeeCap.Cmp(big.NewInt(50_000_000_000)) != 0 {
		t.Fatal("GasFeeCap mismatch")
	}
	if decoded.GasTipCap.Cmp(big.NewInt(1_000_000_000)) != 0 {
		t.Fatal("GasTipCap mismatch")
	}
	if decoded.BlobFeeCap.Cmp(big.NewInt(5_000_000)) != 0 {
		t.Fatal("BlobFeeCap mismatch")
	}
	if len(decoded.Data) != 2 {
		t.Fatal("Data mismatch")
	}
	if len(decoded.BlobHashes) != 2 {
		t.Fatal("BlobHashes mismatch")
	}
	if decoded.V.Int64() != 1 {
		t.Fatal("V mismatch")
	}
	if decoded.R.Int64() != 123456 {
		t.Fatal("R mismatch")
	}
	if decoded.S.Int64() != 654321 {
		t.Fatal("S mismatch")
	}
}

func TestDecodeBlobTxErrors(t *testing.T) {
	// Too short.
	if _, err := DecodeBlobTx([]byte{}); err == nil {
		t.Fatal("expected error for empty data")
	}
	if _, err := DecodeBlobTx([]byte{0x03}); err == nil {
		t.Fatal("expected error for single byte")
	}

	// Wrong type byte.
	if _, err := DecodeBlobTx([]byte{0x02, 0xc0}); err == nil {
		t.Fatal("expected error for wrong type byte")
	}

	// Invalid RLP payload.
	if _, err := DecodeBlobTx([]byte{0x03, 0xff, 0xff}); err == nil {
		t.Fatal("expected error for invalid RLP")
	}
}

func TestEncodeBlobTxRoundtripHashPreservation(t *testing.T) {
	to := HexToAddress("0xaaaa")
	tx := NewBlobTx(
		1, 0, 100000, &to,
		big.NewInt(0),
		big.NewInt(100),
		big.NewInt(10),
		big.NewInt(5),
		nil,
		[]Hash{
			HexToHash("0x0100000000000000000000000000000000000000000000000000000000000001"),
		},
	)

	hashBefore := BlobTxHash(tx)

	encoded, err := EncodeBlobTx(tx)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := DecodeBlobTx(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	hashAfter := BlobTxHash(decoded)
	if hashBefore != hashAfter {
		t.Fatalf("hash changed after encode/decode: %s vs %s", hashBefore.Hex(), hashAfter.Hex())
	}
}

func TestBlobGasPrice(t *testing.T) {
	// Zero excess should produce minimum price.
	price := BlobGasPrice(0)
	if price.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("BlobGasPrice(0) = %s, want 1", price)
	}

	// Price should increase with excess.
	priceLow := BlobGasPrice(BlobTxBlobGasPerBlob)
	priceHigh := BlobGasPrice(BlobBaseFeeUpdateFraction * 3)
	if priceHigh.Cmp(priceLow) <= 0 {
		t.Fatalf("expected price to increase with excess: low=%s high=%s", priceLow, priceHigh)
	}
}

func TestBlobGasPriceConsistency(t *testing.T) {
	// BlobGasPrice should match CalcBlobFee for the same input.
	for _, excess := range []uint64{0, 131072, 393216, 786432, 3338477} {
		price := BlobGasPrice(excess)
		fee := CalcBlobFee(excess)
		if price.Cmp(fee) != 0 {
			t.Fatalf("BlobGasPrice(%d) = %s, CalcBlobFee(%d) = %s", excess, price, excess, fee)
		}
	}
}

func TestMaxBlobsPerBlock(t *testing.T) {
	if MaxBlobsPerBlock != 6 {
		t.Fatalf("MaxBlobsPerBlock = %d, want 6", MaxBlobsPerBlock)
	}
}

func TestBlobTxTypeValue(t *testing.T) {
	if BlobTxType != 0x03 {
		t.Fatalf("BlobTxType = 0x%02x, want 0x03", BlobTxType)
	}
}

func TestBlobTxTxType(t *testing.T) {
	tx := &BlobTx{}
	if tx.txType() != BlobTxType {
		t.Fatalf("txType() = 0x%02x, want 0x%02x", tx.txType(), BlobTxType)
	}
}

func TestValidateBlobVersionedHashesExactMaxBlobs(t *testing.T) {
	// Exactly MaxBlobsPerBlock should be valid.
	hashes := make([]Hash, MaxBlobsPerBlock)
	for i := range hashes {
		hashes[i][0] = VersionedHashVersionKZG
		hashes[i][31] = byte(i + 1)
	}
	tx := &BlobTx{BlobHashes: hashes}
	if err := ValidateBlobVersionedHashes(tx); err != nil {
		t.Fatalf("expected no error for exactly max blobs, got: %v", err)
	}
}

func TestEncodeBlobTxWithAccessList(t *testing.T) {
	to := HexToAddress("0xbbbb")
	tx := NewBlobTx(
		1, 10, 50000, &to,
		big.NewInt(500),
		big.NewInt(2000),
		big.NewInt(200),
		big.NewInt(100),
		[]byte{0x01},
		[]Hash{
			HexToHash("0x0100000000000000000000000000000000000000000000000000000000000001"),
		},
	)
	tx.AccessList = AccessList{
		{
			Address:     HexToAddress("0xcccc"),
			StorageKeys: []Hash{HexToHash("0x01"), HexToHash("0x02")},
		},
	}

	encoded, err := EncodeBlobTx(tx)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := DecodeBlobTx(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.AccessList) != 1 {
		t.Fatalf("AccessList len = %d, want 1", len(decoded.AccessList))
	}
	if len(decoded.AccessList[0].StorageKeys) != 2 {
		t.Fatalf("StorageKeys len = %d, want 2", len(decoded.AccessList[0].StorageKeys))
	}
}
