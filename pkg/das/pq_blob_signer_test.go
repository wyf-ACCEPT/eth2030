package das

import (
	"testing"

	"github.com/eth2030/eth2030/crypto/pqc"
)

func TestPQBlobSignerCreation(t *testing.T) {
	// Falcon signer.
	s, err := NewPQBlobSigner(PQBlobAlgFalcon)
	if err != nil {
		t.Fatalf("NewPQBlobSigner(Falcon) failed: %v", err)
	}
	if s == nil {
		t.Fatal("signer is nil")
	}
	if s.algID != PQBlobAlgFalcon {
		t.Fatalf("algID = %d, want %d", s.algID, PQBlobAlgFalcon)
	}

	// MLDSA signer.
	s2, err := NewPQBlobSigner(PQBlobAlgMLDSA)
	if err != nil {
		t.Fatalf("NewPQBlobSigner(MLDSA) failed: %v", err)
	}
	if s2.algID != PQBlobAlgMLDSA {
		t.Fatalf("algID = %d, want %d", s2.algID, PQBlobAlgMLDSA)
	}

	// Unknown algorithm.
	_, err = NewPQBlobSigner(99)
	if err == nil {
		t.Fatal("expected error for unknown algorithm")
	}
}

func TestPQBlobSignerWithKey(t *testing.T) {
	falcon := &pqc.FalconSigner{}
	kp, err := falcon.GenerateKey()
	if err != nil {
		t.Fatalf("Falcon key gen failed: %v", err)
	}

	s, err := NewPQBlobSignerWithKey(PQBlobAlgFalcon, kp)
	if err != nil {
		t.Fatalf("NewPQBlobSignerWithKey failed: %v", err)
	}
	if s == nil {
		t.Fatal("signer is nil")
	}

	// Nil key should fail.
	_, err = NewPQBlobSignerWithKey(PQBlobAlgFalcon, nil)
	if err == nil {
		t.Fatal("expected error for nil key")
	}
}

func TestSignBlobCommitment(t *testing.T) {
	s, err := NewPQBlobSigner(PQBlobAlgFalcon)
	if err != nil {
		t.Fatalf("signer creation failed: %v", err)
	}

	// Create a test commitment.
	var commitment [PQCommitmentSize]byte
	for i := range commitment {
		commitment[i] = byte(i + 1)
	}

	sig, err := SignBlobCommitment(commitment, s)
	if err != nil {
		t.Fatalf("SignBlobCommitment failed: %v", err)
	}

	if sig.Algorithm != PQBlobAlgFalcon {
		t.Fatalf("sig algorithm = %d, want %d", sig.Algorithm, PQBlobAlgFalcon)
	}
	if len(sig.PublicKey) == 0 {
		t.Fatal("sig public key is empty")
	}
	if len(sig.Signature) == 0 {
		t.Fatal("sig signature is empty")
	}
	if sig.Commitment == [32]byte{} {
		t.Fatal("sig commitment is zero")
	}
}

func TestSignBlobCommitmentNilSigner(t *testing.T) {
	var commitment [PQCommitmentSize]byte
	_, err := SignBlobCommitment(commitment, nil)
	if err == nil {
		t.Fatal("expected error for nil signer")
	}
}

func TestVerifyBlobSignature(t *testing.T) {
	s, err := NewPQBlobSigner(PQBlobAlgFalcon)
	if err != nil {
		t.Fatalf("signer creation failed: %v", err)
	}

	var commitment [PQCommitmentSize]byte
	for i := range commitment {
		commitment[i] = byte(i + 0xAA)
	}

	sig, err := SignBlobCommitment(commitment, s)
	if err != nil {
		t.Fatalf("signing failed: %v", err)
	}

	if !VerifyBlobSignature(commitment, sig) {
		t.Fatal("valid signature rejected")
	}
}

func TestVerifyBlobSignatureWrongCommitment(t *testing.T) {
	s, err := NewPQBlobSigner(PQBlobAlgFalcon)
	if err != nil {
		t.Fatalf("signer creation failed: %v", err)
	}

	var commitment [PQCommitmentSize]byte
	for i := range commitment {
		commitment[i] = byte(i)
	}

	sig, err := SignBlobCommitment(commitment, s)
	if err != nil {
		t.Fatalf("signing failed: %v", err)
	}

	// Verify with different commitment.
	var wrongCommitment [PQCommitmentSize]byte
	for i := range wrongCommitment {
		wrongCommitment[i] = byte(i + 0xFF)
	}

	if VerifyBlobSignature(wrongCommitment, sig) {
		t.Fatal("signature verified for wrong commitment")
	}
}

func TestVerifyBlobSignatureEmptyInputs(t *testing.T) {
	var commitment [PQCommitmentSize]byte

	// Empty public key.
	sig := PQBlobSignature{
		Algorithm: PQBlobAlgFalcon,
		PublicKey: nil,
		Signature: []byte{1, 2, 3},
	}
	if VerifyBlobSignature(commitment, sig) {
		t.Fatal("verify with empty pk should fail")
	}

	// Empty signature.
	sig.PublicKey = []byte{1, 2, 3}
	sig.Signature = nil
	if VerifyBlobSignature(commitment, sig) {
		t.Fatal("verify with empty sig should fail")
	}
}

func TestBatchVerifyBlobSignatures(t *testing.T) {
	s, err := NewPQBlobSigner(PQBlobAlgFalcon)
	if err != nil {
		t.Fatalf("signer creation failed: %v", err)
	}

	n := 5
	commitments := make([][PQCommitmentSize]byte, n)
	sigs := make([]PQBlobSignature, n)

	for i := 0; i < n; i++ {
		for j := range commitments[i] {
			commitments[i][j] = byte(i*16 + j)
		}
		sig, err := SignBlobCommitment(commitments[i], s)
		if err != nil {
			t.Fatalf("signing %d failed: %v", i, err)
		}
		sigs[i] = sig
	}

	valid, err := BatchVerifyBlobSignatures(commitments, sigs)
	if err != nil {
		t.Fatalf("batch verify error: %v", err)
	}
	if valid != n {
		t.Fatalf("batch verify: %d valid, want %d", valid, n)
	}
}

func TestBatchVerifyBlobSignaturesSmall(t *testing.T) {
	s, err := NewPQBlobSigner(PQBlobAlgMLDSA)
	if err != nil {
		t.Fatalf("signer creation failed: %v", err)
	}

	// Small batch (sequential path).
	n := 3
	commitments := make([][PQCommitmentSize]byte, n)
	sigs := make([]PQBlobSignature, n)

	for i := 0; i < n; i++ {
		commitments[i][0] = byte(i + 1)
		sig, err := SignBlobCommitment(commitments[i], s)
		if err != nil {
			t.Fatalf("signing %d failed: %v", i, err)
		}
		sigs[i] = sig
	}

	valid, err := BatchVerifyBlobSignatures(commitments, sigs)
	if err != nil {
		t.Fatalf("batch verify error: %v", err)
	}
	if valid != n {
		t.Fatalf("batch verify: %d valid, want %d", valid, n)
	}
}

func TestBatchVerifyBlobSignaturesEmpty(t *testing.T) {
	_, err := BatchVerifyBlobSignatures(nil, nil)
	if err == nil {
		t.Fatal("expected error for empty batch")
	}
}

func TestBatchVerifyBlobSignaturesMismatch(t *testing.T) {
	commitments := make([][PQCommitmentSize]byte, 2)
	sigs := make([]PQBlobSignature, 3)

	_, err := BatchVerifyBlobSignatures(commitments, sigs)
	if err == nil {
		t.Fatal("expected error for mismatched lengths")
	}
}

func TestPQBlobSignatureSize(t *testing.T) {
	if PQBlobSignatureSize(PQBlobAlgFalcon) != pqc.Falcon512SigSize {
		t.Fatalf("Falcon sig size = %d, want %d",
			PQBlobSignatureSize(PQBlobAlgFalcon), pqc.Falcon512SigSize)
	}
	if PQBlobSignatureSize(PQBlobAlgSPHINCS) != pqc.SPHINCSSha256SigSize {
		t.Fatalf("SPHINCS sig size = %d, want %d",
			PQBlobSignatureSize(PQBlobAlgSPHINCS), pqc.SPHINCSSha256SigSize)
	}
	if PQBlobSignatureSize(PQBlobAlgMLDSA) != pqc.Dilithium3SigSize {
		t.Fatalf("MLDSA sig size = %d, want %d",
			PQBlobSignatureSize(PQBlobAlgMLDSA), pqc.Dilithium3SigSize)
	}
	if PQBlobSignatureSize(0) != 0 {
		t.Fatal("unknown alg sig size should be 0")
	}
}

func TestPQBlobPublicKeySize(t *testing.T) {
	if PQBlobPublicKeySize(PQBlobAlgFalcon) != pqc.Falcon512PubKeySize {
		t.Fatal("wrong Falcon pk size")
	}
	if PQBlobPublicKeySize(PQBlobAlgMLDSA) != pqc.Dilithium3PubKeySize {
		t.Fatal("wrong MLDSA pk size")
	}
}

func TestEncodDecodePQBlobSignature(t *testing.T) {
	original := PQBlobSignature{
		Algorithm: PQBlobAlgFalcon,
		PublicKey: []byte{1, 2, 3, 4, 5},
		Signature: []byte{10, 20, 30, 40, 50, 60},
		Commitment: [32]byte{0xAA, 0xBB, 0xCC},
	}

	encoded := EncodePQBlobSignature(original)
	if len(encoded) == 0 {
		t.Fatal("encoded data is empty")
	}

	decoded, err := DecodePQBlobSignature(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if decoded.Algorithm != original.Algorithm {
		t.Fatalf("algorithm mismatch: %d vs %d", decoded.Algorithm, original.Algorithm)
	}
	if !pqBlobBytesEqual(decoded.PublicKey, original.PublicKey) {
		t.Fatal("public key mismatch")
	}
	if !pqBlobBytesEqual(decoded.Signature, original.Signature) {
		t.Fatal("signature mismatch")
	}
	if decoded.Commitment != original.Commitment {
		t.Fatal("commitment mismatch")
	}
}

func TestDecodePQBlobSignatureBadData(t *testing.T) {
	// Too short.
	_, err := DecodePQBlobSignature([]byte{1, 2})
	if err == nil {
		t.Fatal("expected error for short data")
	}

	// PK length exceeds data.
	data := make([]byte, 10)
	data[0] = PQBlobAlgFalcon
	data[1] = 0
	data[2] = 0
	data[3] = 0xFF // pk_len = 255, too large
	data[4] = 0xFF
	_, err = DecodePQBlobSignature(data)
	if err == nil {
		t.Fatal("expected error for invalid pk length")
	}
}

func TestPQBlobSignerAlgorithmName(t *testing.T) {
	if PQBlobSignerAlgorithmName(PQBlobAlgFalcon) != "Falcon-512" {
		t.Fatal("wrong name for Falcon")
	}
	if PQBlobSignerAlgorithmName(PQBlobAlgSPHINCS) != "SPHINCS+-SHA256" {
		t.Fatal("wrong name for SPHINCS")
	}
	if PQBlobSignerAlgorithmName(PQBlobAlgMLDSA) != "ML-DSA-65" {
		t.Fatal("wrong name for MLDSA")
	}
	if PQBlobSignerAlgorithmName(0) != "unknown" {
		t.Fatal("wrong name for unknown")
	}
}

func pqBlobBytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
