package verkle

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/crypto"
)

// --- PedersenConfig tests ---

func TestNewPedersenConfig_DefaultWidth(t *testing.T) {
	pc := DefaultPedersenConfig()
	if pc.Width() != NodeWidth {
		t.Errorf("Width() = %d, want %d", pc.Width(), NodeWidth)
	}
}

func TestNewPedersenConfig_CustomWidth(t *testing.T) {
	pc := NewPedersenConfig(16)
	if pc.Width() != 16 {
		t.Errorf("Width() = %d, want 16", pc.Width())
	}
}

func TestNewPedersenConfig_ClampToMax(t *testing.T) {
	pc := NewPedersenConfig(1024) // exceeds NodeWidth
	if pc.Width() != NodeWidth {
		t.Errorf("Width() = %d, want %d (clamped)", pc.Width(), NodeWidth)
	}
}

func TestNewPedersenConfig_ZeroWidth(t *testing.T) {
	pc := NewPedersenConfig(0) // should default to NodeWidth
	if pc.Width() != NodeWidth {
		t.Errorf("Width() = %d, want %d (default)", pc.Width(), NodeWidth)
	}
}

func TestNewPedersenConfig_GeneratorsNotNil(t *testing.T) {
	pc := DefaultPedersenConfig()
	for i := 0; i < pc.Width(); i++ {
		if pc.Generator(i) == nil {
			t.Errorf("Generator(%d) is nil", i)
		}
	}
}

func TestNewPedersenConfig_GeneratorBytesConsistent(t *testing.T) {
	pc := DefaultPedersenConfig()
	for i := 0; i < 8; i++ {
		expected := crypto.BanderMapToBytes(pc.Generator(i))
		got := pc.GeneratorBytes(i)
		if got != expected {
			t.Errorf("GeneratorBytes(%d) mismatch", i)
		}
	}
}

func TestNewPedersenConfig_GeneratorsDistinct(t *testing.T) {
	pc := NewPedersenConfig(16)
	for i := 0; i < 16; i++ {
		for j := i + 1; j < 16; j++ {
			bi := pc.GeneratorBytes(i)
			bj := pc.GeneratorBytes(j)
			if bi == bj {
				t.Errorf("Generator(%d) == Generator(%d)", i, j)
			}
		}
	}
}

// --- HashToCurve tests ---

func TestHashToCurve_Deterministic(t *testing.T) {
	input := []byte("test input data")
	h1 := HashToCurve(input)
	h2 := HashToCurve(input)
	if h1 != h2 {
		t.Error("HashToCurve should be deterministic")
	}
}

func TestHashToCurve_DifferentInputs(t *testing.T) {
	h1 := HashToCurve([]byte("input A"))
	h2 := HashToCurve([]byte("input B"))
	if h1 == h2 {
		t.Error("different inputs should produce different curve points")
	}
}

func TestHashToCurve_EmptyInput(t *testing.T) {
	h := HashToCurve([]byte{})
	var zero [32]byte
	// The hash of an empty input should produce a valid non-zero point.
	if h == zero {
		t.Error("HashToCurve(empty) should produce a non-zero result")
	}
}

func TestHashToCurvePoint_ReturnsValidPoint(t *testing.T) {
	pt := HashToCurvePoint([]byte("hello verkle"))
	if pt == nil {
		t.Fatal("HashToCurvePoint returned nil")
	}
	// Verify the point serialization is consistent.
	b := crypto.BanderMapToBytes(pt)
	expected := HashToCurve([]byte("hello verkle"))
	if b != expected {
		t.Error("HashToCurvePoint and HashToCurve should be consistent")
	}
}

func TestHashToCurveIndex_Deterministic(t *testing.T) {
	pt1 := HashToCurveIndex(42)
	pt2 := HashToCurveIndex(42)
	b1 := crypto.BanderMapToBytes(pt1)
	b2 := crypto.BanderMapToBytes(pt2)
	if b1 != b2 {
		t.Error("HashToCurveIndex should be deterministic")
	}
}

func TestHashToCurveIndex_DifferentIndices(t *testing.T) {
	pt1 := HashToCurveIndex(0)
	pt2 := HashToCurveIndex(1)
	b1 := crypto.BanderMapToBytes(pt1)
	b2 := crypto.BanderMapToBytes(pt2)
	if b1 == b2 {
		t.Error("different indices should produce different points")
	}
}

// --- PedersenHash tests ---

func TestPedersenHash_ZeroValues(t *testing.T) {
	pc := NewPedersenConfig(4)
	values := make([][]byte, 4)
	for i := range values {
		values[i] = make([]byte, 32)
	}
	h := pc.PedersenHash(values)
	var zero [32]byte
	if h != zero {
		t.Error("PedersenHash of zero values should be zero (identity point)")
	}
}

func TestPedersenHash_NonZero(t *testing.T) {
	pc := NewPedersenConfig(4)
	values := make([][]byte, 4)
	for i := range values {
		values[i] = make([]byte, 32)
	}
	values[0] = big.NewInt(42).Bytes()
	h := pc.PedersenHash(values)
	var zero [32]byte
	if h == zero {
		t.Error("PedersenHash with non-zero value should not be zero")
	}
}

func TestPedersenHash_Deterministic(t *testing.T) {
	pc := NewPedersenConfig(4)
	values := [][]byte{
		big.NewInt(1).Bytes(),
		big.NewInt(2).Bytes(),
		big.NewInt(3).Bytes(),
		big.NewInt(4).Bytes(),
	}
	h1 := pc.PedersenHash(values)
	h2 := pc.PedersenHash(values)
	if h1 != h2 {
		t.Error("PedersenHash should be deterministic")
	}
}

func TestPedersenHash_DifferentValues(t *testing.T) {
	pc := NewPedersenConfig(4)
	v1 := [][]byte{big.NewInt(1).Bytes(), big.NewInt(2).Bytes()}
	v2 := [][]byte{big.NewInt(3).Bytes(), big.NewInt(4).Bytes()}
	h1 := pc.PedersenHash(v1)
	h2 := pc.PedersenHash(v2)
	if h1 == h2 {
		t.Error("different values should produce different hashes")
	}
}

func TestPedersenHash_NilValues(t *testing.T) {
	pc := NewPedersenConfig(4)
	// nil entries should be treated as zero.
	values := [][]byte{nil, nil, nil, nil}
	h := pc.PedersenHash(values)
	var zero [32]byte
	if h != zero {
		t.Error("PedersenHash of nil values should equal zero commitment")
	}
}

func TestPedersenHashPoint_Consistency(t *testing.T) {
	pc := NewPedersenConfig(4)
	values := [][]byte{
		big.NewInt(5).Bytes(),
		big.NewInt(10).Bytes(),
	}
	h := pc.PedersenHash(values)
	pt := pc.PedersenHashPoint(values)
	hFromPt := crypto.BanderMapToBytes(pt)
	if h != hFromPt {
		t.Error("PedersenHash and PedersenHashPoint should be consistent")
	}
}

// --- PedersenCommitFixed tests ---

func TestPedersenCommitFixed_ZeroValues(t *testing.T) {
	pc := NewPedersenConfig(4)
	values := make([][32]byte, 4)
	h := pc.PedersenCommitFixed(values)
	var zero [32]byte
	if h != zero {
		t.Error("PedersenCommitFixed of zeros should be zero")
	}
}

func TestPedersenCommitFixed_SingleValue(t *testing.T) {
	pc := NewPedersenConfig(4)
	values := make([][32]byte, 4)
	values[0][31] = 1
	h := pc.PedersenCommitFixed(values)
	var zero [32]byte
	if h == zero {
		t.Error("PedersenCommitFixed with non-zero should not be zero")
	}
}

func TestPedersenCommitFixed_Deterministic(t *testing.T) {
	pc := NewPedersenConfig(4)
	values := make([][32]byte, 4)
	values[0][31] = 7
	values[1][31] = 13
	h1 := pc.PedersenCommitFixed(values)
	h2 := pc.PedersenCommitFixed(values)
	if h1 != h2 {
		t.Error("PedersenCommitFixed should be deterministic")
	}
}

// --- PedersenUpdate tests ---

func TestPedersenUpdate_NoChange(t *testing.T) {
	pc := NewPedersenConfig(4)
	values := make([][32]byte, 4)
	values[0][31] = 42
	original := pc.PedersenCommitFixed(values)

	updated := pc.PedersenUpdate(original, 0, values[0], values[0])
	if updated != original {
		t.Error("PedersenUpdate with no change should return same commitment")
	}
}

func TestPedersenUpdate_OutOfRange(t *testing.T) {
	pc := NewPedersenConfig(4)
	var orig [32]byte
	orig[31] = 1
	var oldVal, newVal [32]byte
	newVal[31] = 99

	result := pc.PedersenUpdate(orig, -1, oldVal, newVal)
	if result != orig {
		t.Error("PedersenUpdate with negative index should return unchanged")
	}
	result = pc.PedersenUpdate(orig, 999, oldVal, newVal)
	if result != orig {
		t.Error("PedersenUpdate with out-of-range index should return unchanged")
	}
}

func TestPedersenUpdatePoint_SingleSlotChange(t *testing.T) {
	pc := NewPedersenConfig(4)
	values := make([][32]byte, 4)
	values[0][31] = 10
	values[1][31] = 20
	values[2][31] = 30
	values[3][31] = 40

	origPt := pc.PedersenCommitFixedPoint(values)

	// Update slot 2 from 30 to 99.
	var oldVal, newVal [32]byte
	oldVal[31] = 30
	newVal[31] = 99
	updatedPt := pc.PedersenUpdatePoint(origPt, 2, oldVal, newVal)

	// Compute expected commitment from scratch.
	expectedValues := make([][32]byte, 4)
	copy(expectedValues[0][:], values[0][:])
	copy(expectedValues[1][:], values[1][:])
	expectedValues[2][31] = 99
	copy(expectedValues[3][:], values[3][:])
	expectedPt := pc.PedersenCommitFixedPoint(expectedValues)

	updatedBytes := crypto.BanderMapToBytes(updatedPt)
	expectedBytes := crypto.BanderMapToBytes(expectedPt)
	if updatedBytes != expectedBytes {
		t.Error("PedersenUpdatePoint should match recomputed commitment")
	}
}

// --- StemCommitment tests ---

func TestStemCommitment_Basic(t *testing.T) {
	pc := DefaultPedersenConfig()
	var stem [StemSize]byte
	stem[0] = 0xAB
	stem[30] = 0xCD

	values := make([][32]byte, 3)
	values[0][31] = 1
	values[1][31] = 2
	values[2][31] = 3

	c := pc.StemCommitment(stem, values)
	var zero [32]byte
	if c == zero {
		t.Error("StemCommitment with non-zero stem should not be zero")
	}
}

func TestStemCommitment_DifferentStems(t *testing.T) {
	pc := DefaultPedersenConfig()
	var stem1, stem2 [StemSize]byte
	stem1[0] = 0x01
	stem2[0] = 0x02

	values := make([][32]byte, 2)
	values[0][31] = 42

	c1 := pc.StemCommitment(stem1, values)
	c2 := pc.StemCommitment(stem2, values)
	if c1 == c2 {
		t.Error("different stems should produce different commitments")
	}
}

func TestStemCommitment_Deterministic(t *testing.T) {
	pc := DefaultPedersenConfig()
	var stem [StemSize]byte
	stem[5] = 0xFF

	values := make([][32]byte, 1)
	values[0][31] = 99

	c1 := pc.StemCommitment(stem, values)
	c2 := pc.StemCommitment(stem, values)
	if c1 != c2 {
		t.Error("StemCommitment should be deterministic")
	}
}

func TestStemCommitmentPoint_Consistency(t *testing.T) {
	pc := DefaultPedersenConfig()
	var stem [StemSize]byte
	stem[0] = 0x42

	values := make([][32]byte, 4)
	values[0][31] = 1
	values[1][31] = 2

	c := pc.StemCommitment(stem, values)
	pt := pc.StemCommitmentPoint(stem, values)
	cFromPt := crypto.BanderMapToBytes(pt)
	if c != cFromPt {
		t.Error("StemCommitment and StemCommitmentPoint should be consistent")
	}
}

// --- StemCommitmentSparse tests ---

func TestStemCommitmentSparse_Basic(t *testing.T) {
	pc := DefaultPedersenConfig()
	var stem [StemSize]byte
	stem[0] = 0x01

	indices := []byte{0, 5, 10}
	values := make([][32]byte, 3)
	values[0][31] = 1
	values[1][31] = 2
	values[2][31] = 3

	c := pc.StemCommitmentSparse(stem, indices, values)
	var zero [32]byte
	if c == zero {
		t.Error("StemCommitmentSparse should not be zero with non-zero values")
	}
}

func TestStemCommitmentSparse_MismatchedLengths(t *testing.T) {
	pc := DefaultPedersenConfig()
	var stem [StemSize]byte
	indices := []byte{0, 1}
	values := make([][32]byte, 3)

	c := pc.StemCommitmentSparse(stem, indices, values)
	var zero [32]byte
	if c != zero {
		t.Error("StemCommitmentSparse with mismatched lengths should return zero")
	}
}

// --- VerifyCommitment test ---

func TestVerifyCommitment_Valid(t *testing.T) {
	pc := NewPedersenConfig(4)
	values := make([][32]byte, 4)
	values[0][31] = 7
	values[1][31] = 13

	c := pc.PedersenCommitFixed(values)
	if !pc.VerifyCommitment(c, values) {
		t.Error("VerifyCommitment should return true for correctly computed commitment")
	}
}

func TestVerifyCommitment_Invalid(t *testing.T) {
	pc := NewPedersenConfig(4)
	values := make([][32]byte, 4)
	values[0][31] = 7

	c := pc.PedersenCommitFixed(values)

	// Tamper with a value.
	values[0][31] = 8
	if pc.VerifyCommitment(c, values) {
		t.Error("VerifyCommitment should return false for tampered values")
	}
}

// --- CommitmentToBytes test ---

func TestCommitmentToBytes_Generator(t *testing.T) {
	g := crypto.BanderGenerator()
	b := CommitmentToBytes(g)
	expected := crypto.BanderMapToBytes(g)
	if b != expected {
		t.Error("CommitmentToBytes should match BanderMapToBytes")
	}
}
