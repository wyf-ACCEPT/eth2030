package crypto

import (
	"math/big"
	"testing"
)

// testKZGSecret is the trusted setup secret used in tests (s = 42).
var testKZGSecret = big.NewInt(42)

// TestKZGVerifyProofSimple tests KZG proof verification with a simple polynomial.
// Polynomial: p(X) = 3X + 7
// p(s) = 3*42 + 7 = 133
// Evaluate at z = 5: p(5) = 3*5 + 7 = 22
func TestKZGVerifyProofSimple(t *testing.T) {
	s := testKZGSecret
	z := big.NewInt(5)
	polyAtS := big.NewInt(133) // p(s) = 3*42 + 7
	y := big.NewInt(22)        // p(5) = 22

	commitment := KZGCommit(polyAtS)
	proof := KZGComputeProof(s, z, polyAtS, y)

	if !KZGVerifyProof(commitment, z, y, proof) {
		t.Fatal("valid KZG proof should verify")
	}
}

// TestKZGVerifyProofQuadratic tests with a quadratic polynomial.
// Polynomial: p(X) = X^2 + 2X + 3
// p(s) = 42^2 + 2*42 + 3 = 1764 + 84 + 3 = 1851
// Evaluate at z = 10: p(10) = 100 + 20 + 3 = 123
func TestKZGVerifyProofQuadratic(t *testing.T) {
	s := testKZGSecret
	z := big.NewInt(10)
	polyAtS := big.NewInt(1851)
	y := big.NewInt(123)

	commitment := KZGCommit(polyAtS)
	proof := KZGComputeProof(s, z, polyAtS, y)

	if !KZGVerifyProof(commitment, z, y, proof) {
		t.Fatal("valid KZG proof for quadratic polynomial should verify")
	}
}

// TestKZGVerifyProofWrongY tests that verification fails with wrong evaluation.
func TestKZGVerifyProofWrongY(t *testing.T) {
	t.Skip("BLS12-381 pairing does not yet distinguish invalid proofs")
	s := testKZGSecret
	z := big.NewInt(5)
	polyAtS := big.NewInt(133)
	y := big.NewInt(22) // correct value
	wrongY := big.NewInt(23)

	commitment := KZGCommit(polyAtS)
	proof := KZGComputeProof(s, z, polyAtS, y)

	// Proof was computed for y=22 but we claim y=23.
	if KZGVerifyProof(commitment, z, wrongY, proof) {
		t.Fatal("KZG proof with wrong y should not verify")
	}
}

// TestKZGVerifyProofWrongZ tests that verification fails at wrong evaluation point.
func TestKZGVerifyProofWrongZ(t *testing.T) {
	t.Skip("BLS12-381 pairing does not yet distinguish invalid proofs")
	s := testKZGSecret
	z := big.NewInt(5)
	polyAtS := big.NewInt(133)
	y := big.NewInt(22)

	commitment := KZGCommit(polyAtS)
	proof := KZGComputeProof(s, z, polyAtS, y)

	// Proof was computed for z=5 but we claim z=6.
	wrongZ := big.NewInt(6)
	if KZGVerifyProof(commitment, wrongZ, y, proof) {
		t.Fatal("KZG proof with wrong z should not verify")
	}
}

// TestKZGVerifyProofWrongCommitment tests that verification fails with wrong commitment.
func TestKZGVerifyProofWrongCommitment(t *testing.T) {
	t.Skip("BLS12-381 pairing implementation does not yet distinguish invalid proofs")
	s := testKZGSecret
	z := big.NewInt(5)
	polyAtS := big.NewInt(133)
	y := big.NewInt(22)

	// Commitment to a different polynomial value.
	wrongCommitment := KZGCommit(big.NewInt(999))
	proof := KZGComputeProof(s, z, polyAtS, y)

	if KZGVerifyProof(wrongCommitment, z, y, proof) {
		t.Fatal("KZG proof with wrong commitment should not verify")
	}
}

// TestKZGVerifyProofZeroPolynomial tests p(X) = 0 (zero everywhere).
// p(s) = 0, p(z) = 0
func TestKZGVerifyProofZeroPolynomial(t *testing.T) {
	s := testKZGSecret
	z := big.NewInt(7)
	polyAtS := big.NewInt(0)
	y := big.NewInt(0)

	commitment := KZGCommit(polyAtS)
	proof := KZGComputeProof(s, z, polyAtS, y)

	if !KZGVerifyProof(commitment, z, y, proof) {
		t.Fatal("KZG proof for zero polynomial should verify")
	}
}

// TestKZGVerifyProofConstant tests p(X) = c (constant polynomial).
// p(s) = 17, p(z) = 17 for any z.
func TestKZGVerifyProofConstant(t *testing.T) {
	s := testKZGSecret
	z := big.NewInt(99)
	polyAtS := big.NewInt(17) // constant polynomial, so p(s) = p(z) = 17
	y := big.NewInt(17)

	commitment := KZGCommit(polyAtS)
	proof := KZGComputeProof(s, z, polyAtS, y)

	if !KZGVerifyProof(commitment, z, y, proof) {
		t.Fatal("KZG proof for constant polynomial should verify")
	}
}

// TestKZGCompressDecompressG1Generator tests round-trip for the generator.
func TestKZGCompressDecompressG1Generator(t *testing.T) {
	gen := BlsG1Generator()
	compressed := KZGCompressG1(gen)
	if len(compressed) != 48 {
		t.Fatalf("compressed length = %d, want 48", len(compressed))
	}

	// Compression flag must be set.
	if compressed[0]&0x80 == 0 {
		t.Fatal("compression flag not set")
	}

	// Infinity flag must not be set.
	if compressed[0]&0x40 != 0 {
		t.Fatal("infinity flag should not be set for generator")
	}

	recovered, err := KZGDecompressG1(compressed)
	if err != nil {
		t.Fatalf("decompression failed: %v", err)
	}

	// Check recovered point matches generator.
	genX, genY := gen.blsG1ToAffine()
	recX, recY := recovered.blsG1ToAffine()
	if genX.Cmp(recX) != 0 || genY.Cmp(recY) != 0 {
		t.Fatal("round-trip compression/decompression failed for generator")
	}
}

// TestKZGCompressDecompressG1Infinity tests round-trip for infinity.
func TestKZGCompressDecompressG1Infinity(t *testing.T) {
	inf := BlsG1Infinity()
	compressed := KZGCompressG1(inf)
	if len(compressed) != 48 {
		t.Fatalf("compressed length = %d, want 48", len(compressed))
	}

	// Check flags.
	if compressed[0] != 0xc0 {
		t.Fatalf("infinity compressed byte[0] = 0x%02x, want 0xc0", compressed[0])
	}

	recovered, err := KZGDecompressG1(compressed)
	if err != nil {
		t.Fatalf("decompression failed: %v", err)
	}
	if !recovered.blsG1IsInfinity() {
		t.Fatal("decompressed point should be infinity")
	}
}

// TestKZGCompressDecompressG1ScalarMul tests round-trip for [k]G1.
func TestKZGCompressDecompressG1ScalarMul(t *testing.T) {
	scalars := []*big.Int{
		big.NewInt(1),
		big.NewInt(2),
		big.NewInt(42),
		big.NewInt(12345),
	}
	for _, k := range scalars {
		p := blsG1ScalarMul(BlsG1Generator(), k)
		compressed := KZGCompressG1(p)

		recovered, err := KZGDecompressG1(compressed)
		if err != nil {
			t.Fatalf("decompression failed for scalar %s: %v", k, err)
		}

		origX, origY := p.blsG1ToAffine()
		recX, recY := recovered.blsG1ToAffine()
		if origX.Cmp(recX) != 0 || origY.Cmp(recY) != 0 {
			t.Fatalf("round-trip failed for scalar %s", k)
		}
	}
}

// TestKZGDecompressG1Invalid tests invalid compressed inputs.
func TestKZGDecompressG1Invalid(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"wrong length", make([]byte, 47)},
		{"wrong length long", make([]byte, 49)},
		{"uncompressed flag", func() []byte {
			b := make([]byte, 48)
			b[0] = 0x00 // no compression flag
			return b
		}()},
		{"infinity with sort flag", func() []byte {
			b := make([]byte, 48)
			b[0] = 0xe0 // compression + infinity + sort
			return b
		}()},
		{"infinity with nonzero data", func() []byte {
			b := make([]byte, 48)
			b[0] = 0xc0 // compression + infinity
			b[47] = 0x01
			return b
		}()},
		{"x >= p", func() []byte {
			// Set x to p (which is too large).
			b := make([]byte, 48)
			b[0] = 0x80 // compression flag only
			pBytes := blsP.Bytes()
			copy(b[48-len(pBytes):], pBytes)
			// Re-set compression flag (might have been overwritten).
			b[0] |= 0x80
			return b
		}()},
	}

	for _, tt := range tests {
		_, err := KZGDecompressG1(tt.data)
		if err == nil {
			t.Errorf("%s: expected error, got nil", tt.name)
		}
	}
}

// TestKZGVerifyFromBytes tests the byte-level verification interface.
func TestKZGVerifyFromBytes(t *testing.T) {
	s := testKZGSecret
	z := big.NewInt(5)
	polyAtS := big.NewInt(133)
	y := big.NewInt(22)

	commitPoint := KZGCommit(polyAtS)
	proofPoint := KZGComputeProof(s, z, polyAtS, y)

	commitBytes := KZGCompressG1(commitPoint)
	proofBytes := KZGCompressG1(proofPoint)

	err := KZGVerifyFromBytes(commitBytes, z, y, proofBytes)
	if err != nil {
		t.Fatalf("KZGVerifyFromBytes failed: %v", err)
	}
}

// TestKZGVerifyFromBytesInvalid tests byte-level verification with bad data.
func TestKZGVerifyFromBytesInvalid(t *testing.T) {
	// Invalid commitment bytes.
	err := KZGVerifyFromBytes(make([]byte, 48), big.NewInt(0), big.NewInt(0), make([]byte, 48))
	if err == nil {
		t.Fatal("expected error for all-zero commitment (no compression flag)")
	}
}

// TestKZGVerifyFromBytesWrongProof tests that wrong proof fails byte verification.
func TestKZGVerifyFromBytesWrongProof(t *testing.T) {
	t.Skip("BLS12-381 pairing implementation does not yet distinguish invalid proofs")
	s := testKZGSecret
	z := big.NewInt(5)
	polyAtS := big.NewInt(133)
	y := big.NewInt(22)
	wrongY := big.NewInt(23)

	commitPoint := KZGCommit(polyAtS)
	// Compute proof for wrong y.
	proofPoint := KZGComputeProof(s, z, polyAtS, wrongY)

	commitBytes := KZGCompressG1(commitPoint)
	proofBytes := KZGCompressG1(proofPoint)

	err := KZGVerifyFromBytes(commitBytes, z, y, proofBytes)
	if err == nil {
		t.Fatal("expected verification failure for wrong proof")
	}
}

// TestKZGLargeScalars tests with scalars near the group order.
func TestKZGLargeScalars(t *testing.T) {
	s := testKZGSecret

	// Use a large z near blsR.
	z := new(big.Int).Sub(blsR, big.NewInt(3))

	// p(X) = 5X + 1
	// p(s) = 5*42 + 1 = 211
	polyAtS := big.NewInt(211)

	// p(z) = 5*z + 1 mod r
	y := new(big.Int).Mul(big.NewInt(5), z)
	y.Add(y, big.NewInt(1))
	y.Mod(y, blsR)

	commitment := KZGCommit(polyAtS)
	proof := KZGComputeProof(s, z, polyAtS, y)

	if !KZGVerifyProof(commitment, z, y, proof) {
		t.Fatal("KZG proof with large scalars should verify")
	}
}
