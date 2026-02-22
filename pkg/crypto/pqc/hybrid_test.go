package pqc

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/crypto"
)

func TestVerifyHybridNilHybridSig(t *testing.T) {
	ecKey, _ := crypto.GenerateKey()
	if VerifyHybrid(&ecKey.PublicKey, []byte("pk"), []byte("msg"), nil) {
		t.Error("should reject nil hybrid signature")
	}
}

func TestVerifyHybridNilPQSig(t *testing.T) {
	ecKey, _ := crypto.GenerateKey()
	hybrid := &HybridSignature{
		ECDSASig: make([]byte, 65),
		PQSig:    nil,
	}
	if VerifyHybrid(&ecKey.PublicKey, []byte("pk"), []byte("msg"), hybrid) {
		t.Error("should reject nil PQSig")
	}
}

func TestVerifyHybridNilECDSAPublicKey(t *testing.T) {
	hybrid := &HybridSignature{
		ECDSASig: make([]byte, 65),
		PQSig: &PQSignature{
			Algorithm: DILITHIUM3,
			Signature: make([]byte, Dilithium3SigSize),
		},
	}
	if VerifyHybrid(nil, []byte("pk"), []byte("msg"), hybrid) {
		t.Error("should reject nil ECDSA public key")
	}
}

func TestVerifyHybridWrongECDSASigLength(t *testing.T) {
	ecKey, _ := crypto.GenerateKey()
	hybrid := &HybridSignature{
		ECDSASig: make([]byte, 64), // should be 65
		PQSig: &PQSignature{
			Algorithm: DILITHIUM3,
			Signature: make([]byte, Dilithium3SigSize),
		},
	}
	if VerifyHybrid(&ecKey.PublicKey, []byte("pk"), []byte("msg"), hybrid) {
		t.Error("should reject ECDSA sig with wrong length")
	}
}

func TestVerifyHybridInvalidECDSAPublicKey(t *testing.T) {
	// Create a public key with zero coordinates, which FromECDSAPub returns nil for.
	badPub := &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     new(big.Int),
		Y:     new(big.Int),
	}

	pqSigner := &DilithiumSigner{}
	kp, _ := pqSigner.GenerateKey()
	pqSig, _ := pqSigner.Sign(kp.SecretKey, []byte("msg"))

	hybrid := &HybridSignature{
		ECDSASig: make([]byte, 65),
		PQSig: &PQSignature{
			Algorithm: DILITHIUM3,
			PublicKey: kp.PublicKey,
			Signature: pqSig,
		},
	}

	if VerifyHybrid(badPub, kp.PublicKey, []byte("msg"), hybrid) {
		t.Error("should reject invalid ECDSA public key")
	}
}

func TestVerifyHybridECDSAPassPQFails(t *testing.T) {
	ecKey, _ := crypto.GenerateKey()
	msg := crypto.Keccak256([]byte("hybrid test ecdsa pass pq fail"))
	ecSig, _ := crypto.Sign(msg, ecKey)

	// All-zero PQ signature should fail stubVerify.
	badPQSig := make([]byte, Dilithium3SigSize)
	kp, _ := (&DilithiumSigner{}).GenerateKey()

	hybrid := &HybridSignature{
		ECDSASig: ecSig,
		PQSig: &PQSignature{
			Algorithm: DILITHIUM3,
			PublicKey: kp.PublicKey,
			Signature: badPQSig,
		},
	}

	if VerifyHybrid(&ecKey.PublicKey, kp.PublicKey, msg, hybrid) {
		t.Error("should fail when PQ signature is invalid")
	}
}

func TestVerifyHybridPQPassECDSAFails(t *testing.T) {
	ecKey, _ := crypto.GenerateKey()
	otherKey, _ := crypto.GenerateKey()
	msg := crypto.Keccak256([]byte("hybrid test pq pass ecdsa fail"))

	// Sign with the wrong ECDSA key.
	ecSig, _ := crypto.Sign(msg, otherKey)

	pqSigner := &DilithiumSigner{}
	kp, _ := pqSigner.GenerateKey()
	pqSig, _ := pqSigner.Sign(kp.SecretKey, msg)

	hybrid := &HybridSignature{
		ECDSASig: ecSig,
		PQSig: &PQSignature{
			Algorithm: DILITHIUM3,
			PublicKey: kp.PublicKey,
			Signature: pqSig,
		},
	}

	if VerifyHybrid(&ecKey.PublicKey, kp.PublicKey, msg, hybrid) {
		t.Error("should fail when ECDSA signature is from wrong key")
	}
}

func TestVerifyHybridUnknownPQAlgorithm(t *testing.T) {
	ecKey, _ := crypto.GenerateKey()
	msg := crypto.Keccak256([]byte("hybrid unknown alg"))
	ecSig, _ := crypto.Sign(msg, ecKey)

	hybrid := &HybridSignature{
		ECDSASig: ecSig,
		PQSig: &PQSignature{
			Algorithm: PQAlgorithm(99), // unknown
			Signature: []byte("fake"),
		},
	}

	if VerifyHybrid(&ecKey.PublicKey, []byte("pk"), msg, hybrid) {
		t.Error("should fail for unknown PQ algorithm")
	}
}

func TestVerifyHybridValidWithDilithium(t *testing.T) {
	ecKey, _ := crypto.GenerateKey()
	msg := crypto.Keccak256([]byte("full hybrid dilithium test"))
	ecSig, _ := crypto.Sign(msg, ecKey)

	pqSigner := &DilithiumSigner{}
	kp, _ := pqSigner.GenerateKey()
	pqSig, _ := pqSigner.Sign(kp.SecretKey, msg)

	hybrid := &HybridSignature{
		ECDSASig: ecSig,
		PQSig: &PQSignature{
			Algorithm: DILITHIUM3,
			PublicKey: kp.PublicKey,
			Signature: pqSig,
		},
	}

	if !VerifyHybrid(&ecKey.PublicKey, kp.PublicKey, msg, hybrid) {
		t.Error("valid hybrid signature should verify")
	}
}

func TestVerifyHybridValidWithFalcon(t *testing.T) {
	ecKey, _ := crypto.GenerateKey()
	msg := crypto.Keccak256([]byte("full hybrid falcon test"))
	ecSig, _ := crypto.Sign(msg, ecKey)

	pqSigner := &FalconSigner{}
	kp, _ := pqSigner.GenerateKey()
	pqSig, _ := pqSigner.Sign(kp.SecretKey, msg)

	hybrid := &HybridSignature{
		ECDSASig: ecSig,
		PQSig: &PQSignature{
			Algorithm: FALCON512,
			PublicKey: kp.PublicKey,
			Signature: pqSig,
		},
	}

	if !VerifyHybrid(&ecKey.PublicKey, kp.PublicKey, msg, hybrid) {
		t.Error("valid hybrid falcon signature should verify")
	}
}

func TestHybridSignatureStructFields(t *testing.T) {
	ecSig := make([]byte, 65)
	ecSig[0] = 0xAB
	pqSig := &PQSignature{
		Algorithm: FALCON512,
		PublicKey: []byte{0x01},
		Signature: []byte{0x02},
	}

	hs := HybridSignature{
		ECDSASig: ecSig,
		PQSig:    pqSig,
	}

	if len(hs.ECDSASig) != 65 {
		t.Errorf("ECDSASig length = %d, want 65", len(hs.ECDSASig))
	}
	if hs.ECDSASig[0] != 0xAB {
		t.Errorf("ECDSASig[0] = %x, want 0xAB", hs.ECDSASig[0])
	}
	if hs.PQSig.Algorithm != FALCON512 {
		t.Errorf("PQSig.Algorithm = %d, want %d", hs.PQSig.Algorithm, FALCON512)
	}
}
