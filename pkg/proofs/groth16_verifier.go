// Groth16 verification layer for the mandatory proof system (K+ milestone).
// Implements BLS12-381 Groth16 verification: e(A,B) = e(Alpha,Beta) * e(IC,Gamma) * e(C,Delta).
// The existing Groth16Proof in aa_proof_circuits.go uses BN254; this uses BLS12-381.

package proofs

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/crypto"
)

var (
	ErrGroth16NilProof      = errors.New("groth16: nil proof")
	ErrGroth16NilVK         = errors.New("groth16: nil verifying key")
	ErrGroth16InvalidA      = errors.New("groth16: invalid A (G1)")
	ErrGroth16InvalidB      = errors.New("groth16: invalid B (G2)")
	ErrGroth16InvalidC      = errors.New("groth16: invalid C (G1)")
	ErrGroth16InvalidAlpha  = errors.New("groth16: invalid Alpha (G1)")
	ErrGroth16InvalidBeta   = errors.New("groth16: invalid Beta (G2)")
	ErrGroth16InvalidGamma  = errors.New("groth16: invalid Gamma (G2)")
	ErrGroth16InvalidDelta  = errors.New("groth16: invalid Delta (G2)")
	ErrGroth16NoIC          = errors.New("groth16: no IC points")
	ErrGroth16ICMismatch    = errors.New("groth16: IC length mismatch")
	ErrGroth16PairingFailed = errors.New("groth16: pairing failed")
)

// BLS12-381 precompile encoding sizes.
const (
	blsG16G1Enc  = 128 // G1 uncompressed: 2 * 64 bytes
	blsG16G2Enc  = 256 // G2 uncompressed: 2 * 128 bytes
	blsG16Scalar = 32
)

// BLSGroth16Proof: three BLS12-381 group elements (A in G1, B in G2, C in G1).
type BLSGroth16Proof struct {
	A *crypto.BlsG1Point
	B *crypto.BlsG2Point
	C *crypto.BlsG1Point
}

// BLSGroth16VerifyingKey holds the BLS12-381 verification key.
type BLSGroth16VerifyingKey struct {
	Alpha *crypto.BlsG1Point
	Beta  *crypto.BlsG2Point
	Gamma *crypto.BlsG2Point
	Delta *crypto.BlsG2Point
	IC    []*crypto.BlsG1Point // IC[0] constant + IC[1..n] per public input
}

// BLSGroth16ProvingKey is a placeholder for the prover key.
type BLSGroth16ProvingKey struct {
	CircuitName    string
	KeyData        []byte
	NumConstraints int
}

// BLSGroth16Backend defines pluggable BLS12-381 Groth16 verification.
type BLSGroth16Backend interface {
	Verify(vk *BLSGroth16VerifyingKey, proof *BLSGroth16Proof, publicInputs [][]byte) (bool, error)
	Setup(circuit *CircuitDefinition) (*BLSGroth16ProvingKey, *BLSGroth16VerifyingKey, error)
	Name() string
}

var (
	groth16BackendMu      sync.RWMutex
	activeGroth16Backend  BLSGroth16Backend
	defaultGroth16Backend = &PureGoGroth16Backend{}
)

func DefaultGroth16Backend() BLSGroth16Backend {
	groth16BackendMu.RLock()
	defer groth16BackendMu.RUnlock()
	if activeGroth16Backend != nil {
		return activeGroth16Backend
	}
	return defaultGroth16Backend
}

func SetGroth16Backend(b BLSGroth16Backend) {
	groth16BackendMu.Lock()
	defer groth16BackendMu.Unlock()
	activeGroth16Backend = b
}

func Groth16IntegrationStatus() string { return DefaultGroth16Backend().Name() }

// --- Validation ---

func ValidateGroth16Proof(proof *BLSGroth16Proof) error {
	if proof == nil {
		return ErrGroth16NilProof
	}
	if proof.A == nil {
		return ErrGroth16InvalidA
	}
	if proof.B == nil {
		return ErrGroth16InvalidB
	}
	if proof.C == nil {
		return ErrGroth16InvalidC
	}
	return nil
}

func ValidateVerifyingKey(vk *BLSGroth16VerifyingKey) error {
	if vk == nil {
		return ErrGroth16NilVK
	}
	if vk.Alpha == nil {
		return ErrGroth16InvalidAlpha
	}
	if vk.Beta == nil {
		return ErrGroth16InvalidBeta
	}
	if vk.Gamma == nil {
		return ErrGroth16InvalidGamma
	}
	if vk.Delta == nil {
		return ErrGroth16InvalidDelta
	}
	if len(vk.IC) == 0 {
		return ErrGroth16NoIC
	}
	return nil
}

// --- PureGoGroth16Backend ---

type PureGoGroth16Backend struct{}

func (b *PureGoGroth16Backend) Name() string { return "pure-go-groth16" }

// Verify checks: e(-A,B) * e(Alpha,Beta) * e(IC_input,Gamma) * e(C,Delta) == 1.
func (b *PureGoGroth16Backend) Verify(vk *BLSGroth16VerifyingKey, proof *BLSGroth16Proof, publicInputs [][]byte) (bool, error) {
	if err := ValidateGroth16Proof(proof); err != nil {
		return false, err
	}
	if err := ValidateVerifyingKey(vk); err != nil {
		return false, err
	}
	if len(vk.IC) != len(publicInputs)+1 {
		return false, fmt.Errorf("%w: got %d inputs, need %d", ErrGroth16ICMismatch, len(publicInputs), len(vk.IC)-1)
	}
	icInput, err := g16ComputeIC(vk.IC, publicInputs)
	if err != nil {
		return false, fmt.Errorf("groth16: IC computation: %w", err)
	}
	negA := g16NegateG1(proof.A)
	ps := blsG16G1Enc + blsG16G2Enc
	input := make([]byte, 4*ps)
	g16PutG1(input[0:], negA)
	g16PutG2(input[blsG16G1Enc:], proof.B)
	g16PutG1(input[ps:], vk.Alpha)
	g16PutG2(input[ps+blsG16G1Enc:], vk.Beta)
	g16PutG1(input[2*ps:], icInput)
	g16PutG2(input[2*ps+blsG16G1Enc:], vk.Gamma)
	g16PutG1(input[3*ps:], proof.C)
	g16PutG2(input[3*ps+blsG16G1Enc:], vk.Delta)
	result, err := crypto.BLS12Pairing(input)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrGroth16PairingFailed, err)
	}
	if len(result) != 32 {
		return false, ErrGroth16PairingFailed
	}
	return result[31] == 1, nil
}

func (b *PureGoGroth16Backend) Setup(circuit *CircuitDefinition) (*BLSGroth16ProvingKey, *BLSGroth16VerifyingKey, error) {
	if circuit == nil {
		return nil, nil, ErrCircuitNilDef
	}
	g1, g2 := crypto.BlsG1Generator(), crypto.BlsG2Generator()
	ic := make([]*crypto.BlsG1Point, circuit.PublicInputCount+1)
	for i := range ic {
		ic[i] = g16ScalarMulG1(g1, int64(i+2))
	}
	vk := &BLSGroth16VerifyingKey{Alpha: g1, Beta: g2, Gamma: g2, Delta: g2, IC: ic}
	pk := &BLSGroth16ProvingKey{CircuitName: circuit.Name, NumConstraints: circuit.ConstraintCount()}
	return pk, vk, nil
}

// --- GnarkGroth16Backend (build-tag-ready) ---

type GnarkGroth16Backend struct{ CurveID string }

func (b *GnarkGroth16Backend) Name() string { return "gnark-groth16" }
func (b *GnarkGroth16Backend) Verify(vk *BLSGroth16VerifyingKey, proof *BLSGroth16Proof, pi [][]byte) (bool, error) {
	return (&PureGoGroth16Backend{}).Verify(vk, proof, pi)
}
func (b *GnarkGroth16Backend) Setup(c *CircuitDefinition) (*BLSGroth16ProvingKey, *BLSGroth16VerifyingKey, error) {
	return (&PureGoGroth16Backend{}).Setup(c)
}

// --- Gas estimation ---

func EstimateGroth16VerifyGas(numPublicInputs int) uint64 {
	if numPublicInputs < 0 {
		numPublicInputs = 0
	}
	return 21000 + 113000*4 + 12500*uint64(numPublicInputs)
}

// --- Serialization ---

func SerializeBLSGroth16Proof(proof *BLSGroth16Proof) ([]byte, error) {
	if err := ValidateGroth16Proof(proof); err != nil {
		return nil, err
	}
	a := crypto.SerializeG1(proof.A)
	b := crypto.SerializeG2(proof.B)
	c := crypto.SerializeG1(proof.C)
	out := make([]byte, 0, 192)
	out = append(out, a[:]...)
	out = append(out, b[:]...)
	out = append(out, c[:]...)
	return out, nil
}

func DeserializeBLSGroth16Proof(data []byte) (*BLSGroth16Proof, error) {
	if len(data) != 192 {
		return nil, fmt.Errorf("groth16: invalid length %d, want 192", len(data))
	}
	var ab [48]byte
	copy(ab[:], data[:48])
	a := crypto.DeserializeG1(ab)
	if a == nil {
		return nil, ErrGroth16InvalidA
	}
	var bb [96]byte
	copy(bb[:], data[48:144])
	b := crypto.DeserializeG2(bb)
	if b == nil {
		return nil, ErrGroth16InvalidB
	}
	var cb [48]byte
	copy(cb[:], data[144:])
	c := crypto.DeserializeG1(cb)
	if c == nil {
		return nil, ErrGroth16InvalidC
	}
	return &BLSGroth16Proof{A: a, B: b, C: c}, nil
}

func BLSGroth16ProofFingerprint(proof *BLSGroth16Proof) ([32]byte, error) {
	data, err := SerializeBLSGroth16Proof(proof)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(data), nil
}

func EncodePublicInput(v int64) []byte {
	var buf [32]byte
	binary.BigEndian.PutUint64(buf[24:], uint64(v))
	return buf[:]
}

// --- Internal helpers ---

var blsP381, _ = new(big.Int).SetString("1a0111ea397fe69a4b1ba7b6434bacd764774b84f38512bf6730d2a0f6b0f6241eabfffeb153ffffb9feffffffffaaab", 16)

func g16ComputeIC(ic []*crypto.BlsG1Point, inputs [][]byte) (*crypto.BlsG1Point, error) {
	result := ic[0]
	for i, inp := range inputs {
		mul := make([]byte, blsG16G1Enc+blsG16Scalar)
		g16PutG1(mul, ic[i+1])
		s := new(big.Int).SetBytes(inp).Bytes()
		copy(mul[blsG16G1Enc+blsG16Scalar-len(s):], s)
		mr, err := crypto.BLS12G1Mul(mul)
		if err != nil {
			return nil, err
		}
		add := make([]byte, 2*blsG16G1Enc)
		g16PutG1(add, result)
		copy(add[blsG16G1Enc:], mr)
		ar, err := crypto.BLS12G1Add(add)
		if err != nil {
			return nil, err
		}
		result = g16BytesToG1(ar)
	}
	return result, nil
}

func g16PutG1(dst []byte, p *crypto.BlsG1Point) {
	ser := crypto.SerializeG1(p)
	if ser[0]&0x40 != 0 {
		for i := 0; i < blsG16G1Enc; i++ {
			dst[i] = 0
		}
		return
	}
	xb := make([]byte, 48)
	copy(xb, ser[:])
	xb[0] &= 0x1F
	x := new(big.Int).SetBytes(xb)
	sortF := ser[0]&0x20 != 0
	x3 := new(big.Int).Exp(x, big.NewInt(3), blsP381)
	rhs := new(big.Int).Add(x3, big.NewInt(4))
	rhs.Mod(rhs, blsP381)
	exp := new(big.Int).Add(blsP381, big.NewInt(1))
	exp.Rsh(exp, 2)
	y := new(big.Int).Exp(rhs, exp, blsP381)
	hp := new(big.Int).Rsh(blsP381, 1)
	if sortF != (y.Cmp(hp) > 0) {
		y.Sub(blsP381, y)
	}
	for i := 0; i < blsG16G1Enc; i++ {
		dst[i] = 0
	}
	xB := x.Bytes()
	copy(dst[64-len(xB):64], xB)
	yB := y.Bytes()
	copy(dst[128-len(yB):128], yB)
}

func g16PutG2(dst []byte, p *crypto.BlsG2Point) {
	ser := crypto.SerializeG2(p)
	if ser[0]&0x40 != 0 {
		for i := 0; i < blsG16G2Enc; i++ {
			dst[i] = 0
		}
		return
	}
	sortF := ser[0]&0x20 != 0
	xc1b := make([]byte, 48)
	copy(xc1b, ser[:48])
	xc1b[0] &= 0x1F
	xc1 := new(big.Int).SetBytes(xc1b)
	xc0 := new(big.Int).SetBytes(ser[48:96])
	// y^2 = x^3 + 4(1+u) in Fp2
	t0, t1 := fp2Mul(xc0, xc1, xc0, xc1)
	x3c0, x3c1 := fp2Mul(t0, t1, xc0, xc1)
	rc0 := new(big.Int).Add(x3c0, big.NewInt(4))
	rc0.Mod(rc0, blsP381)
	rc1 := new(big.Int).Add(x3c1, big.NewInt(4))
	rc1.Mod(rc1, blsP381)
	yc0, yc1 := fp2SqrtG16(rc0, rc1)
	if yc0 == nil {
		for i := 0; i < blsG16G2Enc; i++ {
			dst[i] = 0
		}
		return
	}
	hp := new(big.Int).Rsh(blsP381, 1)
	flip := yc1.Cmp(hp) > 0 || (yc1.Sign() == 0 && yc0.Cmp(hp) > 0)
	if sortF != flip {
		yc0.Sub(blsP381, yc0)
		yc1.Sub(blsP381, yc1)
		yc0.Mod(yc0, blsP381)
		yc1.Mod(yc1, blsP381)
	}
	w64 := func(d []byte, v *big.Int) {
		for i := range d {
			d[i] = 0
		}
		b := v.Bytes()
		copy(d[64-len(b):], b)
	}
	w64(dst[0:64], xc1)
	w64(dst[64:128], xc0)
	w64(dst[128:192], yc1)
	w64(dst[192:256], yc0)
}

func fp2Mul(a0, a1, b0, b1 *big.Int) (*big.Int, *big.Int) {
	c0 := new(big.Int).Mul(a0, b0)
	c0.Sub(c0, new(big.Int).Mul(a1, b1))
	c0.Mod(c0, blsP381)
	c1 := new(big.Int).Mul(a0, b1)
	c1.Add(c1, new(big.Int).Mul(a1, b0))
	c1.Mod(c1, blsP381)
	return c0, c1
}

func fp2SqrtG16(a, b *big.Int) (*big.Int, *big.Int) {
	exp := new(big.Int).Add(blsP381, big.NewInt(1))
	exp.Rsh(exp, 2)
	if b.Sign() == 0 {
		c0 := new(big.Int).Exp(a, exp, blsP381)
		if new(big.Int).Exp(c0, big.NewInt(2), blsP381).Cmp(new(big.Int).Mod(a, blsP381)) == 0 {
			return c0, big.NewInt(0)
		}
		negA := new(big.Int).Sub(blsP381, new(big.Int).Mod(a, blsP381))
		c1 := new(big.Int).Exp(negA, exp, blsP381)
		if new(big.Int).Exp(c1, big.NewInt(2), blsP381).Cmp(negA) == 0 {
			return big.NewInt(0), c1
		}
		return nil, nil
	}
	norm := new(big.Int).Add(new(big.Int).Exp(a, big.NewInt(2), blsP381), new(big.Int).Exp(b, big.NewInt(2), blsP381))
	norm.Mod(norm, blsP381)
	alpha := new(big.Int).Exp(norm, exp, blsP381)
	if new(big.Int).Exp(alpha, big.NewInt(2), blsP381).Cmp(norm) != 0 {
		return nil, nil
	}
	inv2 := new(big.Int).ModInverse(big.NewInt(2), blsP381)
	delta := new(big.Int).Add(a, alpha)
	delta.Mul(delta, inv2).Mod(delta, blsP381)
	c0 := new(big.Int).Exp(delta, exp, blsP381)
	if new(big.Int).Exp(c0, big.NewInt(2), blsP381).Cmp(delta) != 0 {
		delta = new(big.Int).Sub(a, alpha)
		delta.Mod(delta, blsP381).Mul(delta, inv2).Mod(delta, blsP381)
		c0 = new(big.Int).Exp(delta, exp, blsP381)
	}
	if c0.Sign() == 0 {
		return nil, nil
	}
	c1 := new(big.Int).Mul(b, new(big.Int).ModInverse(c0, blsP381))
	c1.Mul(c1, inv2).Mod(c1, blsP381)
	return c0, c1
}

func g16NegateG1(p *crypto.BlsG1Point) *crypto.BlsG1Point {
	ser := crypto.SerializeG1(p)
	if ser[0]&0x40 != 0 {
		return p
	}
	ser[0] ^= 0x20
	if r := crypto.DeserializeG1(ser); r != nil {
		return r
	}
	return p
}

func g16ScalarMulG1(base *crypto.BlsG1Point, k int64) *crypto.BlsG1Point {
	if k == 0 {
		return crypto.BlsG1Infinity()
	}
	inp := make([]byte, blsG16G1Enc+blsG16Scalar)
	g16PutG1(inp, base)
	s := new(big.Int).SetInt64(k).Bytes()
	copy(inp[blsG16G1Enc+blsG16Scalar-len(s):], s)
	if r, err := crypto.BLS12G1Mul(inp); err == nil {
		return g16BytesToG1(r)
	}
	return crypto.BlsG1Infinity()
}

func g16BytesToG1(data []byte) *crypto.BlsG1Point {
	if len(data) != blsG16G1Enc {
		return crypto.BlsG1Infinity()
	}
	allZ := true
	for _, b := range data {
		if b != 0 {
			allZ = false
			break
		}
	}
	if allZ {
		return crypto.BlsG1Infinity()
	}
	x := new(big.Int).SetBytes(data[:64])
	y := new(big.Int).SetBytes(data[64:128])
	hp := new(big.Int).Rsh(blsP381, 1)
	var c [48]byte
	xB := x.Bytes()
	copy(c[48-len(xB):], xB)
	c[0] |= 0x80
	if y.Cmp(hp) > 0 {
		c[0] |= 0x20
	}
	return crypto.DeserializeG1(c)
}
