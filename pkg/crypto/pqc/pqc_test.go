package pqc

import (
	"crypto/ecdsa"
	"testing"

	"github.com/eth2030/eth2030/crypto"
)

// --- Type and size tests ---

func TestPubKeySize(t *testing.T) {
	tests := []struct {
		alg  PQAlgorithm
		want int
	}{
		{DILITHIUM3, 1952},
		{FALCON512, 897},
		{SPHINCSSHA256, 32},
		{PQAlgorithm(99), 0},
	}
	for _, tt := range tests {
		if got := PubKeySize(tt.alg); got != tt.want {
			t.Errorf("PubKeySize(%d) = %d, want %d", tt.alg, got, tt.want)
		}
	}
}

func TestSecKeySize(t *testing.T) {
	tests := []struct {
		alg  PQAlgorithm
		want int
	}{
		{DILITHIUM3, 4000},
		{FALCON512, 1281},
		{SPHINCSSHA256, 64},
	}
	for _, tt := range tests {
		if got := SecKeySize(tt.alg); got != tt.want {
			t.Errorf("SecKeySize(%d) = %d, want %d", tt.alg, got, tt.want)
		}
	}
}

func TestSigSize(t *testing.T) {
	tests := []struct {
		alg  PQAlgorithm
		want int
	}{
		{DILITHIUM3, 3293},
		{FALCON512, 690},
		{SPHINCSSHA256, 49216},
	}
	for _, tt := range tests {
		if got := SigSize(tt.alg); got != tt.want {
			t.Errorf("SigSize(%d) = %d, want %d", tt.alg, got, tt.want)
		}
	}
}

// --- GetSigner tests ---

func TestGetSigner(t *testing.T) {
	if s := GetSigner(DILITHIUM3); s == nil {
		t.Fatal("GetSigner(DILITHIUM3) returned nil")
	}
	if s := GetSigner(FALCON512); s == nil {
		t.Fatal("GetSigner(FALCON512) returned nil")
	}
	if s := GetSigner(SPHINCSSHA256); s != nil {
		t.Fatal("GetSigner(SPHINCSSHA256) should return nil (no signer yet)")
	}
	if s := GetSigner(PQAlgorithm(99)); s != nil {
		t.Fatal("GetSigner(99) should return nil")
	}
}

// --- Dilithium3 tests ---

func TestDilithiumKeyGen(t *testing.T) {
	signer := &DilithiumSigner{}
	kp, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if kp.Algorithm != DILITHIUM3 {
		t.Errorf("Algorithm = %d, want %d", kp.Algorithm, DILITHIUM3)
	}
	if len(kp.PublicKey) != Dilithium3PubKeySize {
		t.Errorf("PublicKey len = %d, want %d", len(kp.PublicKey), Dilithium3PubKeySize)
	}
	if len(kp.SecretKey) != Dilithium3SecKeySize {
		t.Errorf("SecretKey len = %d, want %d", len(kp.SecretKey), Dilithium3SecKeySize)
	}
}

func TestDilithiumSignVerify(t *testing.T) {
	signer := &DilithiumSigner{}
	kp, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	msg := []byte("test message for dilithium signing")
	sig, err := signer.Sign(kp.SecretKey, msg)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if len(sig) != Dilithium3SigSize {
		t.Errorf("Signature len = %d, want %d", len(sig), Dilithium3SigSize)
	}

	if !signer.Verify(kp.PublicKey, msg, sig) {
		t.Error("Verify returned false for valid signature")
	}
}

func TestDilithiumSignInvalidKeySize(t *testing.T) {
	signer := &DilithiumSigner{}
	_, err := signer.Sign([]byte("short"), []byte("msg"))
	if err != ErrInvalidKeySize {
		t.Errorf("Sign with short key: got %v, want %v", err, ErrInvalidKeySize)
	}
}

func TestDilithiumVerifyInvalidSizes(t *testing.T) {
	signer := &DilithiumSigner{}
	// Wrong pubkey size.
	if signer.Verify([]byte("short"), []byte("msg"), make([]byte, Dilithium3SigSize)) {
		t.Error("Verify accepted short pubkey")
	}
	// Wrong sig size.
	if signer.Verify(make([]byte, Dilithium3PubKeySize), []byte("msg"), []byte("short")) {
		t.Error("Verify accepted short signature")
	}
}

func TestDilithiumDeterministic(t *testing.T) {
	signer := &DilithiumSigner{}
	kp1, _ := signer.GenerateKey()
	kp2, _ := signer.GenerateKey()

	// Stub key gen is deterministic from fixed seed.
	if len(kp1.PublicKey) != len(kp2.PublicKey) {
		t.Fatal("key lengths differ")
	}
	for i := range kp1.PublicKey {
		if kp1.PublicKey[i] != kp2.PublicKey[i] {
			t.Fatal("deterministic keygen produced different public keys")
		}
	}

	// Signing the same message with the same key produces the same signature.
	msg := []byte("determinism test")
	sig1, _ := signer.Sign(kp1.SecretKey, msg)
	sig2, _ := signer.Sign(kp2.SecretKey, msg)
	for i := range sig1 {
		if sig1[i] != sig2[i] {
			t.Fatal("deterministic signing produced different signatures")
		}
	}
}

// --- Falcon512 tests ---

func TestFalconKeyGen(t *testing.T) {
	signer := &FalconSigner{}
	kp, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if kp.Algorithm != FALCON512 {
		t.Errorf("Algorithm = %d, want %d", kp.Algorithm, FALCON512)
	}
	if len(kp.PublicKey) != Falcon512PubKeySize {
		t.Errorf("PublicKey len = %d, want %d", len(kp.PublicKey), Falcon512PubKeySize)
	}
	if len(kp.SecretKey) != Falcon512SecKeySize {
		t.Errorf("SecretKey len = %d, want %d", len(kp.SecretKey), Falcon512SecKeySize)
	}
}

func TestFalconSignVerify(t *testing.T) {
	signer := &FalconSigner{}
	kp, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	msg := []byte("test message for falcon signing")
	sig, err := signer.Sign(kp.SecretKey, msg)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if len(sig) != Falcon512SigSize {
		t.Errorf("Signature len = %d, want %d", len(sig), Falcon512SigSize)
	}

	if !signer.Verify(kp.PublicKey, msg, sig) {
		t.Error("Verify returned false for valid signature")
	}
}

func TestFalconSignInvalidKeySize(t *testing.T) {
	signer := &FalconSigner{}
	_, err := signer.Sign([]byte("short"), []byte("msg"))
	if err != ErrInvalidKeySize {
		t.Errorf("Sign with short key: got %v, want %v", err, ErrInvalidKeySize)
	}
}

func TestFalconVerifyInvalidSizes(t *testing.T) {
	signer := &FalconSigner{}
	if signer.Verify([]byte("short"), []byte("msg"), make([]byte, Falcon512SigSize)) {
		t.Error("Verify accepted short pubkey")
	}
	if signer.Verify(make([]byte, Falcon512PubKeySize), []byte("msg"), []byte("short")) {
		t.Error("Verify accepted short signature")
	}
}

// --- Hybrid verification tests ---

func TestHybridVerifyBothPass(t *testing.T) {
	// Generate ECDSA key and sign.
	ecKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	msg := crypto.Keccak256([]byte("hybrid test message"))
	ecSig, err := crypto.Sign(msg, ecKey)
	if err != nil {
		t.Fatalf("ECDSA Sign: %v", err)
	}

	// Generate PQ key and sign.
	pqSigner := &DilithiumSigner{}
	pqKP, _ := pqSigner.GenerateKey()
	pqSig, err := pqSigner.Sign(pqKP.SecretKey, msg)
	if err != nil {
		t.Fatalf("PQ Sign: %v", err)
	}

	hybrid := &HybridSignature{
		ECDSASig: ecSig,
		PQSig: &PQSignature{
			Algorithm: DILITHIUM3,
			PublicKey: pqKP.PublicKey,
			Signature: pqSig,
		},
	}

	if !VerifyHybrid(&ecKey.PublicKey, pqKP.PublicKey, msg, hybrid) {
		t.Error("VerifyHybrid failed for valid hybrid signature")
	}
}

func TestHybridVerifyECDSAFails(t *testing.T) {
	ecKey, _ := crypto.GenerateKey()
	otherKey, _ := crypto.GenerateKey()

	msg := crypto.Keccak256([]byte("hybrid ecdsa fail test"))
	// Sign with the wrong key.
	ecSig, _ := crypto.Sign(msg, otherKey)

	pqSigner := &DilithiumSigner{}
	pqKP, _ := pqSigner.GenerateKey()
	pqSig, _ := pqSigner.Sign(pqKP.SecretKey, msg)

	hybrid := &HybridSignature{
		ECDSASig: ecSig,
		PQSig: &PQSignature{
			Algorithm: DILITHIUM3,
			PublicKey: pqKP.PublicKey,
			Signature: pqSig,
		},
	}

	if VerifyHybrid(&ecKey.PublicKey, pqKP.PublicKey, msg, hybrid) {
		t.Error("VerifyHybrid should fail when ECDSA sig is from wrong key")
	}
}

func TestHybridVerifyPQFails(t *testing.T) {
	ecKey, _ := crypto.GenerateKey()
	msg := crypto.Keccak256([]byte("hybrid pq fail test"))
	ecSig, _ := crypto.Sign(msg, ecKey)

	// Create an invalid PQ signature (all zeros).
	badPQSig := make([]byte, Dilithium3SigSize)
	pqKP, _ := (&DilithiumSigner{}).GenerateKey()

	hybrid := &HybridSignature{
		ECDSASig: ecSig,
		PQSig: &PQSignature{
			Algorithm: DILITHIUM3,
			PublicKey: pqKP.PublicKey,
			Signature: badPQSig,
		},
	}

	if VerifyHybrid(&ecKey.PublicKey, pqKP.PublicKey, msg, hybrid) {
		t.Error("VerifyHybrid should fail when PQ sig is invalid")
	}
}

func TestHybridVerifyNilInputs(t *testing.T) {
	if VerifyHybrid(nil, nil, nil, nil) {
		t.Error("should fail on nil hybrid")
	}
	if VerifyHybrid(&ecdsa.PublicKey{}, nil, nil, &HybridSignature{}) {
		t.Error("should fail on nil PQSig")
	}
}

// --- Algorithm method tests ---

func TestDilithiumAlgorithm(t *testing.T) {
	s := &DilithiumSigner{}
	if s.Algorithm() != DILITHIUM3 {
		t.Errorf("Algorithm() = %d, want %d", s.Algorithm(), DILITHIUM3)
	}
}

func TestFalconAlgorithm(t *testing.T) {
	s := &FalconSigner{}
	if s.Algorithm() != FALCON512 {
		t.Errorf("Algorithm() = %d, want %d", s.Algorithm(), FALCON512)
	}
}
