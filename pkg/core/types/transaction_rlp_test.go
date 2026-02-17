package types

import (
	"bytes"
	"math/big"
	"testing"
)

func TestLegacyTxRoundTrip(t *testing.T) {
	to := HexToAddress("0xdead")
	inner := &LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(20_000_000_000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(1_000_000_000_000_000_000),
		Data:     []byte{0xca, 0xfe},
		V:        big.NewInt(37),
		R:        big.NewInt(123456789),
		S:        big.NewInt(987654321),
	}
	tx := NewTransaction(inner)

	enc, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP: %v", err)
	}
	// Legacy tx should start with an RLP list prefix (>= 0xc0).
	if enc[0] < 0xc0 {
		t.Fatalf("legacy tx encoding should start with list prefix, got 0x%02x", enc[0])
	}

	decoded, err := DecodeTxRLP(enc)
	if err != nil {
		t.Fatalf("DecodeTxRLP: %v", err)
	}
	assertTxEqual(t, tx, decoded)
}

func TestAccessListTxRoundTrip(t *testing.T) {
	to := HexToAddress("0xbeef")
	inner := &AccessListTx{
		ChainID:  big.NewInt(1),
		Nonce:    5,
		GasPrice: big.NewInt(10_000_000_000),
		Gas:      50000,
		To:       &to,
		Value:    big.NewInt(1000),
		Data:     []byte{0x01, 0x02, 0x03},
		AccessList: AccessList{
			{
				Address:     HexToAddress("0xaaaa"),
				StorageKeys: []Hash{HexToHash("0x01"), HexToHash("0x02")},
			},
		},
		V: big.NewInt(1),
		R: big.NewInt(111111),
		S: big.NewInt(222222),
	}
	tx := NewTransaction(inner)

	enc, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP: %v", err)
	}
	// Typed tx: first byte = type.
	if enc[0] != AccessListTxType {
		t.Fatalf("expected type byte 0x%02x, got 0x%02x", AccessListTxType, enc[0])
	}

	decoded, err := DecodeTxRLP(enc)
	if err != nil {
		t.Fatalf("DecodeTxRLP: %v", err)
	}
	assertTxEqual(t, tx, decoded)

	// Verify access list survives.
	if len(decoded.AccessList()) != 1 {
		t.Fatalf("expected 1 access tuple, got %d", len(decoded.AccessList()))
	}
	if decoded.AccessList()[0].Address != HexToAddress("0xaaaa") {
		t.Fatal("access list address mismatch")
	}
	if len(decoded.AccessList()[0].StorageKeys) != 2 {
		t.Fatalf("expected 2 storage keys, got %d", len(decoded.AccessList()[0].StorageKeys))
	}
}

func TestDynamicFeeTxRoundTrip(t *testing.T) {
	to := HexToAddress("0xcafe")
	inner := &DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     10,
		GasTipCap: big.NewInt(2_000_000_000),
		GasFeeCap: big.NewInt(100_000_000_000),
		Gas:       21000,
		To:        &to,
		Value:     big.NewInt(0),
		Data:      nil,
		AccessList: AccessList{
			{
				Address:     HexToAddress("0x1234"),
				StorageKeys: nil,
			},
		},
		V: big.NewInt(0),
		R: big.NewInt(999999),
		S: big.NewInt(888888),
	}
	tx := NewTransaction(inner)

	enc, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP: %v", err)
	}
	if enc[0] != DynamicFeeTxType {
		t.Fatalf("expected type byte 0x%02x, got 0x%02x", DynamicFeeTxType, enc[0])
	}

	decoded, err := DecodeTxRLP(enc)
	if err != nil {
		t.Fatalf("DecodeTxRLP: %v", err)
	}
	assertTxEqual(t, tx, decoded)
}

func TestBlobTxRoundTrip(t *testing.T) {
	inner := &BlobTx{
		ChainID:    big.NewInt(1),
		Nonce:      0,
		GasTipCap:  big.NewInt(1_000_000_000),
		GasFeeCap:  big.NewInt(50_000_000_000),
		Gas:        21000,
		To:         HexToAddress("0xblobaddr"),
		Value:      big.NewInt(0),
		Data:       []byte{0xff},
		AccessList: nil,
		BlobFeeCap: big.NewInt(1_000_000),
		BlobHashes: []Hash{HexToHash("0x01"), HexToHash("0x02")},
		V:          big.NewInt(0),
		R:          big.NewInt(42),
		S:          big.NewInt(43),
	}
	tx := NewTransaction(inner)

	enc, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP: %v", err)
	}
	if enc[0] != BlobTxType {
		t.Fatalf("expected type byte 0x%02x, got 0x%02x", BlobTxType, enc[0])
	}

	decoded, err := DecodeTxRLP(enc)
	if err != nil {
		t.Fatalf("DecodeTxRLP: %v", err)
	}
	assertTxEqual(t, tx, decoded)

	// Verify blob-specific fields.
	blobInner := decoded.inner.(*BlobTx)
	if blobInner.BlobFeeCap.Cmp(big.NewInt(1_000_000)) != 0 {
		t.Fatal("BlobFeeCap mismatch")
	}
	if len(blobInner.BlobHashes) != 2 {
		t.Fatalf("expected 2 blob hashes, got %d", len(blobInner.BlobHashes))
	}
}

func TestSetCodeTxRoundTrip(t *testing.T) {
	inner := &SetCodeTx{
		ChainID:   big.NewInt(1),
		Nonce:     0,
		GasTipCap: big.NewInt(1_000_000_000),
		GasFeeCap: big.NewInt(50_000_000_000),
		Gas:       100000,
		To:        HexToAddress("0x7702"),
		Value:     big.NewInt(0),
		Data:      nil,
		AuthorizationList: []Authorization{
			{
				ChainID: big.NewInt(1),
				Address: HexToAddress("0xdelegated"),
				Nonce:   0,
				V:       big.NewInt(27),
				R:       big.NewInt(12345),
				S:       big.NewInt(67890),
			},
		},
		V: big.NewInt(0),
		R: big.NewInt(111),
		S: big.NewInt(222),
	}
	tx := NewTransaction(inner)

	enc, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP: %v", err)
	}
	if enc[0] != SetCodeTxType {
		t.Fatalf("expected type byte 0x%02x, got 0x%02x", SetCodeTxType, enc[0])
	}

	decoded, err := DecodeTxRLP(enc)
	if err != nil {
		t.Fatalf("DecodeTxRLP: %v", err)
	}
	assertTxEqual(t, tx, decoded)

	// Verify authorization list.
	setCodeInner := decoded.inner.(*SetCodeTx)
	if len(setCodeInner.AuthorizationList) != 1 {
		t.Fatalf("expected 1 authorization, got %d", len(setCodeInner.AuthorizationList))
	}
	auth := setCodeInner.AuthorizationList[0]
	if auth.ChainID.Int64() != 1 {
		t.Fatal("auth ChainID mismatch")
	}
	if auth.Address != HexToAddress("0xdelegated") {
		t.Fatal("auth Address mismatch")
	}
}

func TestLegacyTxContractCreationRoundTrip(t *testing.T) {
	inner := &LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      100000,
		To:       nil, // contract creation
		Value:    big.NewInt(0),
		Data:     []byte{0x60, 0x80, 0x60, 0x40, 0x52},
		V:        big.NewInt(27),
		R:        big.NewInt(1),
		S:        big.NewInt(1),
	}
	tx := NewTransaction(inner)

	enc, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP: %v", err)
	}

	decoded, err := DecodeTxRLP(enc)
	if err != nil {
		t.Fatalf("DecodeTxRLP: %v", err)
	}
	if decoded.To() != nil {
		t.Fatal("decoded contract creation should have nil To")
	}
	if !bytes.Equal(decoded.Data(), inner.Data) {
		t.Fatal("Data mismatch")
	}
}

func TestDynamicFeeTxContractCreation(t *testing.T) {
	inner := &DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     0,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(1),
		Gas:       100000,
		To:        nil, // contract creation
		Value:     big.NewInt(0),
		Data:      []byte{0x60, 0x80},
		V:         big.NewInt(0),
		R:         big.NewInt(1),
		S:         big.NewInt(1),
	}
	tx := NewTransaction(inner)

	enc, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP: %v", err)
	}

	decoded, err := DecodeTxRLP(enc)
	if err != nil {
		t.Fatalf("DecodeTxRLP: %v", err)
	}
	if decoded.To() != nil {
		t.Fatal("decoded contract creation should have nil To")
	}
}

func TestEmptyDataRoundTrip(t *testing.T) {
	to := HexToAddress("0xdead")
	inner := &LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(0),
		Data:     nil,
		V:        big.NewInt(27),
		R:        big.NewInt(1),
		S:        big.NewInt(1),
	}
	tx := NewTransaction(inner)

	enc, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP: %v", err)
	}

	decoded, err := DecodeTxRLP(enc)
	if err != nil {
		t.Fatalf("DecodeTxRLP: %v", err)
	}
	// nil and empty []byte are equivalent in RLP.
	if len(decoded.Data()) != 0 {
		t.Fatal("decoded empty data should have length 0")
	}
}

func TestTransactionHashConsistency(t *testing.T) {
	to := HexToAddress("0xdead")
	inner := &DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     42,
		GasTipCap: big.NewInt(2_000_000_000),
		GasFeeCap: big.NewInt(100_000_000_000),
		Gas:       21000,
		To:        &to,
		Value:     big.NewInt(1_000_000),
		Data:      []byte{0x01, 0x02},
		V:         big.NewInt(0),
		R:         big.NewInt(12345),
		S:         big.NewInt(67890),
	}
	tx := NewTransaction(inner)

	h1 := tx.Hash()
	h2 := tx.Hash()
	if h1 != h2 {
		t.Fatal("Hash() should return consistent results")
	}
	if h1.IsZero() {
		t.Fatal("hash should not be zero")
	}

	// Reconstruct from RLP and verify hash matches.
	enc, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP: %v", err)
	}
	decoded, err := DecodeTxRLP(enc)
	if err != nil {
		t.Fatalf("DecodeTxRLP: %v", err)
	}
	if decoded.Hash() != h1 {
		t.Fatal("decoded transaction should produce the same hash")
	}
}

func TestTransactionHashIsKeccak(t *testing.T) {
	to := HexToAddress("0xdead")
	inner := &LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(100),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(500),
		V:        big.NewInt(27),
		R:        big.NewInt(1),
		S:        big.NewInt(1),
	}
	tx := NewTransaction(inner)
	h := tx.Hash()

	// Keccak-256 produces 32 bytes; verify non-zero and length.
	if len(h) != 32 {
		t.Fatalf("expected 32-byte hash, got %d", len(h))
	}
	if h.IsZero() {
		t.Fatal("hash should not be zero")
	}
}

func TestLegacyTxZeroValues(t *testing.T) {
	// Transaction with all zero/nil values (except Gas for validity).
	inner := &LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(0),
		Gas:      0,
		To:       nil,
		Value:    big.NewInt(0),
		Data:     nil,
		V:        big.NewInt(0),
		R:        big.NewInt(0),
		S:        big.NewInt(0),
	}
	tx := NewTransaction(inner)

	enc, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP: %v", err)
	}

	decoded, err := DecodeTxRLP(enc)
	if err != nil {
		t.Fatalf("DecodeTxRLP: %v", err)
	}
	if decoded.Nonce() != 0 {
		t.Fatal("nonce mismatch")
	}
	if decoded.Gas() != 0 {
		t.Fatal("gas mismatch")
	}
	if decoded.To() != nil {
		t.Fatal("To should be nil")
	}
}

func TestDecodeInvalidData(t *testing.T) {
	// Empty data should fail.
	_, err := DecodeTxRLP(nil)
	if err == nil {
		t.Fatal("expected error for nil data")
	}
	_, err = DecodeTxRLP([]byte{})
	if err == nil {
		t.Fatal("expected error for empty data")
	}

	// Unknown type byte.
	_, err = DecodeTxRLP([]byte{0x05, 0xc0})
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}

	// Truncated typed tx.
	_, err = DecodeTxRLP([]byte{0x02})
	if err == nil {
		t.Fatal("expected error for truncated typed tx")
	}
}

// assertTxEqual compares two transactions' core fields.
func assertTxEqual(t *testing.T, expected, actual *Transaction) {
	t.Helper()
	if expected.Type() != actual.Type() {
		t.Fatalf("Type: expected %d, got %d", expected.Type(), actual.Type())
	}
	if expected.Nonce() != actual.Nonce() {
		t.Fatalf("Nonce: expected %d, got %d", expected.Nonce(), actual.Nonce())
	}
	if expected.Gas() != actual.Gas() {
		t.Fatalf("Gas: expected %d, got %d", expected.Gas(), actual.Gas())
	}
	if cmpBigInt(expected.GasPrice(), actual.GasPrice()) != 0 {
		t.Fatalf("GasPrice: expected %s, got %s", expected.GasPrice(), actual.GasPrice())
	}
	if cmpBigInt(expected.GasTipCap(), actual.GasTipCap()) != 0 {
		t.Fatalf("GasTipCap: expected %s, got %s", expected.GasTipCap(), actual.GasTipCap())
	}
	if cmpBigInt(expected.GasFeeCap(), actual.GasFeeCap()) != 0 {
		t.Fatalf("GasFeeCap: expected %s, got %s", expected.GasFeeCap(), actual.GasFeeCap())
	}
	if cmpBigInt(expected.Value(), actual.Value()) != 0 {
		t.Fatalf("Value: expected %s, got %s", expected.Value(), actual.Value())
	}
	if !bytes.Equal(expected.Data(), actual.Data()) {
		t.Fatalf("Data: expected %x, got %x", expected.Data(), actual.Data())
	}
	if expected.To() == nil && actual.To() != nil {
		t.Fatal("To: expected nil, got non-nil")
	}
	if expected.To() != nil && actual.To() == nil {
		t.Fatal("To: expected non-nil, got nil")
	}
	if expected.To() != nil && *expected.To() != *actual.To() {
		t.Fatalf("To: expected %s, got %s", expected.To(), actual.To())
	}
}

func cmpBigInt(a, b *big.Int) int {
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return -b.Sign()
	}
	if b == nil {
		return a.Sign()
	}
	return a.Cmp(b)
}
