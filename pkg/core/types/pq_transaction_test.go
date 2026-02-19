package types

import (
	"bytes"
	"math/big"
	"testing"
)

func TestNewPQTransaction(t *testing.T) {
	to := HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	tx := NewPQTransaction(
		big.NewInt(1),
		42,
		&to,
		big.NewInt(1000),
		21000,
		big.NewInt(20),
		[]byte{0xca, 0xfe},
	)

	if tx.ChainID.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("ChainID: got %v, want 1", tx.ChainID)
	}
	if tx.Nonce != 42 {
		t.Errorf("Nonce: got %d, want 42", tx.Nonce)
	}
	if tx.To == nil || *tx.To != to {
		t.Errorf("To: got %v, want %v", tx.To, to)
	}
	if tx.Value.Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("Value: got %v, want 1000", tx.Value)
	}
	if tx.Gas != 21000 {
		t.Errorf("Gas: got %d, want 21000", tx.Gas)
	}
	if tx.GasPrice.Cmp(big.NewInt(20)) != 0 {
		t.Errorf("GasPrice: got %v, want 20", tx.GasPrice)
	}
	if !bytes.Equal(tx.Data, []byte{0xca, 0xfe}) {
		t.Errorf("Data: got %x, want cafe", tx.Data)
	}
}

func TestPQTransactionHash(t *testing.T) {
	to := HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	tx := NewPQTransaction(big.NewInt(1), 0, &to, big.NewInt(0), 21000, big.NewInt(1), nil)
	tx.SignWithPQ(PQSigDilithium, make([]byte, 32), make([]byte, DilithiumSigSize))

	h1 := tx.Hash()
	h2 := tx.Hash()

	if h1.IsZero() {
		t.Error("hash should not be zero")
	}
	if h1 != h2 {
		t.Error("hash should be deterministic")
	}
}

func TestPQTransactionType(t *testing.T) {
	tx := NewPQTransaction(big.NewInt(1), 0, nil, big.NewInt(0), 21000, big.NewInt(1), nil)
	if tx.Type() != PQTransactionType {
		t.Errorf("Type: got 0x%02x, want 0x%02x", tx.Type(), PQTransactionType)
	}
	if tx.Type() != 0x07 {
		t.Errorf("Type: got 0x%02x, want 0x07", tx.Type())
	}
}

func TestPQSignWithDilithium(t *testing.T) {
	tx := NewPQTransaction(big.NewInt(1), 0, nil, big.NewInt(0), 21000, big.NewInt(1), nil)

	pubKey := make([]byte, 1952) // Dilithium public key size
	sig := make([]byte, DilithiumSigSize)
	// Fill with non-zero data.
	for i := range sig {
		sig[i] = byte(i % 256)
	}
	for i := range pubKey {
		pubKey[i] = byte(i % 256)
	}

	tx.SignWithPQ(PQSigDilithium, pubKey, sig)

	if tx.PQSignatureType != PQSigDilithium {
		t.Errorf("PQSignatureType: got %d, want %d", tx.PQSignatureType, PQSigDilithium)
	}
	if !bytes.Equal(tx.PQSignature, sig) {
		t.Error("PQSignature mismatch")
	}
	if !bytes.Equal(tx.PQPublicKey, pubKey) {
		t.Error("PQPublicKey mismatch")
	}
}

func TestPQSignWithFalcon(t *testing.T) {
	tx := NewPQTransaction(big.NewInt(1), 0, nil, big.NewInt(0), 21000, big.NewInt(1), nil)

	pubKey := make([]byte, 897) // Falcon-512 public key size
	sig := make([]byte, FalconSigSize)
	for i := range sig {
		sig[i] = byte((i + 7) % 256)
	}
	for i := range pubKey {
		pubKey[i] = byte((i + 3) % 256)
	}

	tx.SignWithPQ(PQSigFalcon, pubKey, sig)

	if tx.PQSignatureType != PQSigFalcon {
		t.Errorf("PQSignatureType: got %d, want %d", tx.PQSignatureType, PQSigFalcon)
	}
	if len(tx.PQSignature) != FalconSigSize {
		t.Errorf("PQSignature length: got %d, want %d", len(tx.PQSignature), FalconSigSize)
	}
}

func TestPQTransactionEncodeDecode(t *testing.T) {
	to := HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")
	tx := NewPQTransaction(
		big.NewInt(42),
		100,
		&to,
		big.NewInt(5000),
		50000,
		big.NewInt(100),
		[]byte{0x01, 0x02, 0x03},
	)

	pubKey := make([]byte, 64)
	sig := make([]byte, DilithiumSigSize)
	for i := range sig {
		sig[i] = byte(i % 256)
	}
	for i := range pubKey {
		pubKey[i] = byte(i % 256)
	}
	tx.SignWithPQ(PQSigDilithium, pubKey, sig)
	tx.ClassicSignature = []byte{0xaa, 0xbb, 0xcc}

	encoded, err := tx.EncodePQ()
	if err != nil {
		t.Fatalf("EncodePQ failed: %v", err)
	}
	if encoded[0] != PQTransactionType {
		t.Errorf("first byte: got 0x%02x, want 0x%02x", encoded[0], PQTransactionType)
	}

	decoded, err := DecodePQTransaction(encoded)
	if err != nil {
		t.Fatalf("DecodePQTransaction failed: %v", err)
	}

	if decoded.ChainID.Cmp(tx.ChainID) != 0 {
		t.Errorf("ChainID: got %v, want %v", decoded.ChainID, tx.ChainID)
	}
	if decoded.Nonce != tx.Nonce {
		t.Errorf("Nonce: got %d, want %d", decoded.Nonce, tx.Nonce)
	}
	if decoded.To == nil || *decoded.To != *tx.To {
		t.Error("To address mismatch")
	}
	if decoded.Value.Cmp(tx.Value) != 0 {
		t.Errorf("Value: got %v, want %v", decoded.Value, tx.Value)
	}
	if decoded.Gas != tx.Gas {
		t.Errorf("Gas: got %d, want %d", decoded.Gas, tx.Gas)
	}
	if decoded.GasPrice.Cmp(tx.GasPrice) != 0 {
		t.Errorf("GasPrice: got %v, want %v", decoded.GasPrice, tx.GasPrice)
	}
	if !bytes.Equal(decoded.Data, tx.Data) {
		t.Error("Data mismatch")
	}
	if decoded.PQSignatureType != tx.PQSignatureType {
		t.Errorf("PQSignatureType: got %d, want %d", decoded.PQSignatureType, tx.PQSignatureType)
	}
	if !bytes.Equal(decoded.PQSignature, tx.PQSignature) {
		t.Error("PQSignature mismatch")
	}
	if !bytes.Equal(decoded.PQPublicKey, tx.PQPublicKey) {
		t.Error("PQPublicKey mismatch")
	}
	if !bytes.Equal(decoded.ClassicSignature, tx.ClassicSignature) {
		t.Error("ClassicSignature mismatch")
	}
}

func TestPQVerifySignature(t *testing.T) {
	tx := NewPQTransaction(big.NewInt(1), 0, nil, big.NewInt(0), 21000, big.NewInt(1), nil)

	pubKey := make([]byte, 64)
	for i := range pubKey {
		pubKey[i] = byte(i)
	}

	// Dilithium: correct size.
	sig := make([]byte, DilithiumSigSize)
	for i := range sig {
		sig[i] = byte(i % 256)
	}
	tx.SignWithPQ(PQSigDilithium, pubKey, sig)
	if !tx.VerifyPQSignature() {
		t.Error("Dilithium signature with correct size should verify")
	}

	// Falcon: correct size.
	sig = make([]byte, FalconSigSize)
	for i := range sig {
		sig[i] = byte(i % 256)
	}
	tx.SignWithPQ(PQSigFalcon, pubKey, sig)
	if !tx.VerifyPQSignature() {
		t.Error("Falcon signature with correct size should verify")
	}

	// SPHINCS+: correct size.
	sig = make([]byte, SPHINCSPlusSigSize)
	for i := range sig {
		sig[i] = byte(i % 256)
	}
	tx.SignWithPQ(PQSigSPHINCS, pubKey, sig)
	if !tx.VerifyPQSignature() {
		t.Error("SPHINCS+ signature with correct size should verify")
	}
}

func TestPQVerifySignatureEmpty(t *testing.T) {
	tx := NewPQTransaction(big.NewInt(1), 0, nil, big.NewInt(0), 21000, big.NewInt(1), nil)

	// No signature attached.
	if tx.VerifyPQSignature() {
		t.Error("empty signature should not verify")
	}

	// Signature present but no public key.
	tx.PQSignature = make([]byte, DilithiumSigSize)
	tx.PQSignatureType = PQSigDilithium
	if tx.VerifyPQSignature() {
		t.Error("signature without public key should not verify")
	}
}

func TestPQVerifySignatureWrongSize(t *testing.T) {
	tx := NewPQTransaction(big.NewInt(1), 0, nil, big.NewInt(0), 21000, big.NewInt(1), nil)
	pubKey := make([]byte, 64)
	for i := range pubKey {
		pubKey[i] = byte(i)
	}

	// Dilithium with wrong size.
	tx.SignWithPQ(PQSigDilithium, pubKey, make([]byte, 100))
	if tx.VerifyPQSignature() {
		t.Error("Dilithium with wrong size should not verify")
	}

	// Falcon with wrong size.
	tx.SignWithPQ(PQSigFalcon, pubKey, make([]byte, 100))
	if tx.VerifyPQSignature() {
		t.Error("Falcon with wrong size should not verify")
	}

	// SPHINCS+ with wrong size.
	tx.SignWithPQ(PQSigSPHINCS, pubKey, make([]byte, 100))
	if tx.VerifyPQSignature() {
		t.Error("SPHINCS+ with wrong size should not verify")
	}

	// Unknown signature type.
	tx.SignWithPQ(99, pubKey, make([]byte, DilithiumSigSize))
	if tx.VerifyPQSignature() {
		t.Error("unknown signature type should not verify")
	}
}
