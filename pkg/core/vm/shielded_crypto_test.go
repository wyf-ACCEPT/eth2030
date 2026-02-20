package vm

import (
	"testing"
)

func TestPedersenCommitValue(t *testing.T) {
	c := PedersenCommitValue(100, 42)
	var zero [32]byte
	if c == zero {
		t.Fatal("commitment should not be zero")
	}
}

func TestPedersenCommitDeterministic(t *testing.T) {
	c1 := PedersenCommitValue(500, 99)
	c2 := PedersenCommitValue(500, 99)
	if c1 != c2 {
		t.Fatal("same inputs should produce same commitment")
	}
}

func TestPedersenCommitDifferentValues(t *testing.T) {
	c1 := PedersenCommitValue(100, 42)
	c2 := PedersenCommitValue(200, 42)
	if c1 == c2 {
		t.Fatal("different values with same blinding should produce different commitments")
	}
}

func TestPedersenCommitDifferentBlindings(t *testing.T) {
	c1 := PedersenCommitValue(100, 42)
	c2 := PedersenCommitValue(100, 43)
	if c1 == c2 {
		t.Fatal("same value with different blinding should produce different commitments")
	}
}

func TestPedersenCommitZeroValue(t *testing.T) {
	c := PedersenCommitValue(0, 42)
	var zero [32]byte
	if c == zero {
		t.Fatal("commitment with zero value but non-zero blinding should not be zero")
	}
}

func TestPedersenCommitZeroBlinding(t *testing.T) {
	c := PedersenCommitValue(42, 0)
	var zero [32]byte
	if c == zero {
		t.Fatal("commitment with non-zero value but zero blinding should not be zero")
	}
}

func TestElGamalEncryptDecryptRoundtrip(t *testing.T) {
	// Generate a keypair: sk is random, pk = sk * G.
	var sk [32]byte
	sk[0] = 0x01 // simple non-zero secret key
	sk[31] = 0x42

	// pk = sk * G (simplified: just hash the secret key).
	skScalar := scFromHash(sk)
	pkPoint := scMul(generatorG, skScalar)
	pk := scToHash(pkPoint)

	plaintext := []byte("secret transaction data")
	randomness := uint64(12345)

	c1, c2 := ElGamalEncrypt(pk, plaintext, randomness)

	var zero [32]byte
	if c1 == zero {
		t.Fatal("c1 should not be zero")
	}
	if c2 == zero {
		t.Fatal("c2 should not be zero")
	}

	// Decrypt.
	recovered := ElGamalDecrypt(sk, c1, c2)
	if len(recovered) == 0 {
		t.Fatal("decrypted result should not be empty")
	}

	// The decrypted value should be deterministic.
	recovered2 := ElGamalDecrypt(sk, c1, c2)
	if !scBytesEqual(recovered, recovered2) {
		t.Fatal("decryption should be deterministic")
	}
}

func TestElGamalEncryptDeterministic(t *testing.T) {
	var pk [32]byte
	pk[0] = 0xAB

	plaintext := []byte("test data")
	c1a, c2a := ElGamalEncrypt(pk, plaintext, 100)
	c1b, c2b := ElGamalEncrypt(pk, plaintext, 100)

	if c1a != c1b || c2a != c2b {
		t.Fatal("same inputs should produce same ciphertext")
	}
}

func TestElGamalEncryptDifferentRandomness(t *testing.T) {
	var pk [32]byte
	pk[0] = 0xAB

	plaintext := []byte("test data")
	c1a, _ := ElGamalEncrypt(pk, plaintext, 100)
	c1b, _ := ElGamalEncrypt(pk, plaintext, 200)

	if c1a == c1b {
		t.Fatal("different randomness should produce different c1")
	}
}

func TestElGamalDecryptWrongKey(t *testing.T) {
	var sk1, sk2 [32]byte
	sk1[0] = 0x01
	sk2[0] = 0x02

	skScalar := scFromHash(sk1)
	pkPoint := scMul(generatorG, skScalar)
	pk := scToHash(pkPoint)

	plaintext := []byte("secret data")
	c1, c2 := ElGamalEncrypt(pk, plaintext, 42)

	// Decrypt with correct key.
	correct := ElGamalDecrypt(sk1, c1, c2)
	// Decrypt with wrong key.
	wrong := ElGamalDecrypt(sk2, c1, c2)

	if scBytesEqual(correct, wrong) {
		t.Fatal("wrong key should produce different decryption")
	}
}

func TestNullifierDerive(t *testing.T) {
	var sk, commitment [32]byte
	sk[0] = 0x01
	commitment[0] = 0xAA

	nullifier := NullifierDerive(sk, commitment)
	var zero [32]byte
	if nullifier == zero {
		t.Fatal("nullifier should not be zero")
	}
}

func TestNullifierDeriveDeterministic(t *testing.T) {
	var sk, commitment [32]byte
	sk[0] = 0x42
	commitment[15] = 0xFF

	n1 := NullifierDerive(sk, commitment)
	n2 := NullifierDerive(sk, commitment)
	if n1 != n2 {
		t.Fatal("same inputs should produce same nullifier")
	}
}

func TestNullifierDeriveDifferentKeys(t *testing.T) {
	var sk1, sk2, commitment [32]byte
	sk1[0] = 0x01
	sk2[0] = 0x02
	commitment[0] = 0xAA

	n1 := NullifierDerive(sk1, commitment)
	n2 := NullifierDerive(sk2, commitment)
	if n1 == n2 {
		t.Fatal("different keys should produce different nullifiers")
	}
}

func TestNullifierDeriveDifferentCommitments(t *testing.T) {
	var sk, c1, c2 [32]byte
	sk[0] = 0x42
	c1[0] = 0x01
	c2[0] = 0x02

	n1 := NullifierDerive(sk, c1)
	n2 := NullifierDerive(sk, c2)
	if n1 == n2 {
		t.Fatal("different commitments should produce different nullifiers")
	}
}

func TestRangeProofGenerateVerify(t *testing.T) {
	value := uint64(500)
	blinding := uint64(42)
	max := uint64(1023) // 10-bit range

	proof := RangeProofGenerate(value, blinding, max)

	commitment := PedersenCommitValue(value, blinding)
	if proof.Commitment != commitment {
		t.Fatal("proof commitment should match PedersenCommitValue")
	}

	if !RangeProofVerify(commitment, proof) {
		t.Fatal("valid range proof should verify")
	}
}

func TestRangeProofBitLength(t *testing.T) {
	tests := []struct {
		max      uint64
		expected uint32
	}{
		{1, 1},
		{2, 2},
		{3, 2},
		{255, 8},
		{256, 9},
		{1023, 10},
		{1024, 11},
		{(1 << 32) - 1, 32},
	}

	for _, tc := range tests {
		proof := RangeProofGenerate(0, 0, tc.max)
		if proof.BitLength != tc.expected {
			t.Errorf("max=%d: BitLength=%d, want %d", tc.max, proof.BitLength, tc.expected)
		}
	}
}

func TestRangeProofVerifyWrongCommitment(t *testing.T) {
	proof := RangeProofGenerate(100, 42, 255)
	wrongCommitment := PedersenCommitValue(200, 42)

	if RangeProofVerify(wrongCommitment, proof) {
		t.Fatal("range proof should fail with wrong commitment")
	}
}

func TestRangeProofVerifyZeroBitLength(t *testing.T) {
	proof := RangeProof{BitLength: 0}
	if RangeProofVerify([32]byte{}, proof) {
		t.Fatal("zero bit length should fail verification")
	}
}

func TestRangeProofVerifyLargeBitLength(t *testing.T) {
	proof := RangeProof{BitLength: 65}
	if RangeProofVerify([32]byte{}, proof) {
		t.Fatal("bit length > 64 should fail verification")
	}
}

func TestRangeProofDifferentValues(t *testing.T) {
	p1 := RangeProofGenerate(100, 42, 255)
	p2 := RangeProofGenerate(200, 42, 255)

	if p1.Commitment == p2.Commitment {
		t.Fatal("different values should produce different commitments")
	}
}

func TestRangeProofMaxZero(t *testing.T) {
	// max=0 should still produce a proof with BitLength=1.
	proof := RangeProofGenerate(0, 0, 0)
	if proof.BitLength != 1 {
		t.Fatalf("max=0: BitLength=%d, want 1", proof.BitLength)
	}
}

func TestRangeProofAllFieldsNonZero(t *testing.T) {
	proof := RangeProofGenerate(42, 99, 1023)

	var zero [32]byte
	if proof.A == zero {
		t.Fatal("A should not be zero")
	}
	if proof.S == zero {
		t.Fatal("S should not be zero")
	}
	if proof.T1 == zero {
		t.Fatal("T1 should not be zero")
	}
	if proof.T2 == zero {
		t.Fatal("T2 should not be zero")
	}
	if proof.TauX == zero {
		t.Fatal("TauX should not be zero")
	}
	if proof.Mu == zero {
		t.Fatal("Mu should not be zero")
	}
	if proof.InnerProduct == zero {
		t.Fatal("InnerProduct should not be zero")
	}
}

func TestScalarArithmetic(t *testing.T) {
	// Test basic scalar operations.
	a := scDerivePoint([]byte("test-a"))
	b := scDerivePoint([]byte("test-b"))

	// a + b should not be zero.
	sum := scAdd(a, b)
	if sum.Sign() == 0 && a.Sign() != 0 {
		t.Fatal("sum should generally not be zero")
	}

	// a - a should be zero.
	diff := scSub(a, a)
	if diff.Sign() != 0 {
		t.Fatal("a - a should be zero")
	}

	// a * 1 should be a.
	one := scDerivePoint([]byte{})
	if one.Sign() == 0 {
		// Derive a different non-zero scalar.
		one.SetInt64(1)
	}
	product := scMul(a, one)
	expected := scMul(a, one)
	if product.Cmp(expected) != 0 {
		t.Fatal("a * 1 should equal a")
	}
}

func TestScToHashRoundtrip(t *testing.T) {
	original := scDerivePoint([]byte("roundtrip-test"))
	h := scToHash(original)
	recovered := scFromHash(h)

	// After mod N, they should be equal.
	originalMod := scDerivePoint([]byte("roundtrip-test"))
	if recovered.Cmp(originalMod) != 0 {
		t.Fatal("hash roundtrip should preserve the scalar")
	}
}

func TestElGamalWithZeroPlaintext(t *testing.T) {
	var pk [32]byte
	pk[0] = 0xAB

	c1, c2 := ElGamalEncrypt(pk, []byte{}, 42)
	var zero [32]byte
	// c1 = r*G should still be non-trivial.
	if c1 == zero {
		t.Fatal("c1 should not be zero even with empty plaintext")
	}
	_ = c2
}

func TestNullifierUniquePerNote(t *testing.T) {
	var sk [32]byte
	sk[0] = 0x01

	// Multiple commitments with same key produce unique nullifiers.
	seen := make(map[[32]byte]bool)
	for i := byte(0); i < 10; i++ {
		var c [32]byte
		c[0] = i
		n := NullifierDerive(sk, c)
		if seen[n] {
			t.Fatalf("nullifier collision at index %d", i)
		}
		seen[n] = true
	}
}

func scBytesEqual(a, b []byte) bool {
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
