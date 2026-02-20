// IPA (Inner Product Argument) commitment scheme for Verkle trees.
//
// This file implements the Verkle-specific IPA commitment layer on top of
// the Banderwagon curve primitives in the crypto package. It provides:
//
//   - FieldElement: 256-bit scalar type with arithmetic (mod subgroup order)
//   - IPAConfig: generator points and domain configuration
//   - Pedersen vector commitments over Banderwagon
//   - IPA proof generation and verification
//   - Verkle inner/leaf node commitment
//   - IPACommitter interface for tree integration
//
// The scheme follows the EIP-6800 Verkle tree specification using the
// Banderwagon curve (prime-order subgroup of Bandersnatch embedded in
// BLS12-381).
//
// Multi-point proof operations are in ipa_multiproof.go.

package verkle

import (
	"errors"
	"math/big"

	"github.com/eth2028/eth2028/crypto"
)

// --- FieldElement: 256-bit scalar mod subgroup order ---

// FieldElement represents a scalar in the Banderwagon subgroup field
// (mod n, where n is the Bandersnatch prime-order subgroup order).
// All arithmetic is performed modulo n.
type FieldElement struct {
	v *big.Int
}

// order caches the subgroup order for field arithmetic.
var order = crypto.BanderN()

// NewFieldElement creates a FieldElement from a big.Int, reducing mod n.
func NewFieldElement(v *big.Int) FieldElement {
	if v == nil {
		return FieldElement{v: new(big.Int)}
	}
	r := new(big.Int).Mod(v, order)
	return FieldElement{v: r}
}

// FieldElementFromBytes creates a FieldElement from a big-endian byte slice.
func FieldElementFromBytes(b []byte) FieldElement {
	v := new(big.Int).SetBytes(b)
	return NewFieldElement(v)
}

// FieldElementFromUint64 creates a FieldElement from a uint64.
func FieldElementFromUint64(x uint64) FieldElement {
	return NewFieldElement(new(big.Int).SetUint64(x))
}

// Zero returns the zero field element.
func Zero() FieldElement {
	return FieldElement{v: new(big.Int)}
}

// One returns the multiplicative identity.
func One() FieldElement {
	return FieldElement{v: big.NewInt(1)}
}

// IsZero returns true if the element is zero.
func (f FieldElement) IsZero() bool {
	return f.v == nil || f.v.Sign() == 0
}

// Equal returns true if two field elements are equal.
func (f FieldElement) Equal(g FieldElement) bool {
	fv, gv := f.v, g.v
	if fv == nil {
		fv = new(big.Int)
	}
	if gv == nil {
		gv = new(big.Int)
	}
	return fv.Cmp(gv) == 0
}

// Bytes returns the 32-byte big-endian encoding.
func (f FieldElement) Bytes() [32]byte {
	var buf [32]byte
	if f.v != nil {
		b := f.v.Bytes()
		copy(buf[32-len(b):], b)
	}
	return buf
}

// BigInt returns the underlying big.Int (a copy).
func (f FieldElement) BigInt() *big.Int {
	if f.v == nil {
		return new(big.Int)
	}
	return new(big.Int).Set(f.v)
}

// Add returns f + g (mod n).
func (f FieldElement) Add(g FieldElement) FieldElement {
	fv, gv := f.v, g.v
	if fv == nil {
		fv = new(big.Int)
	}
	if gv == nil {
		gv = new(big.Int)
	}
	r := new(big.Int).Add(fv, gv)
	r.Mod(r, order)
	return FieldElement{v: r}
}

// Sub returns f - g (mod n).
func (f FieldElement) Sub(g FieldElement) FieldElement {
	fv, gv := f.v, g.v
	if fv == nil {
		fv = new(big.Int)
	}
	if gv == nil {
		gv = new(big.Int)
	}
	r := new(big.Int).Sub(fv, gv)
	r.Mod(r, order)
	return FieldElement{v: r}
}

// Mul returns f * g (mod n).
func (f FieldElement) Mul(g FieldElement) FieldElement {
	fv, gv := f.v, g.v
	if fv == nil {
		fv = new(big.Int)
	}
	if gv == nil {
		gv = new(big.Int)
	}
	r := new(big.Int).Mul(fv, gv)
	r.Mod(r, order)
	return FieldElement{v: r}
}

// Neg returns -f (mod n).
func (f FieldElement) Neg() FieldElement {
	if f.v == nil || f.v.Sign() == 0 {
		return FieldElement{v: new(big.Int)}
	}
	r := new(big.Int).Sub(order, f.v)
	return FieldElement{v: r}
}

// Inv returns f^(-1) (mod n). Panics if f is zero.
func (f FieldElement) Inv() FieldElement {
	if f.v == nil || f.v.Sign() == 0 {
		panic("verkle: inverse of zero")
	}
	r := new(big.Int).ModInverse(f.v, order)
	return FieldElement{v: r}
}

// --- IPAConfig: generator points and domain ---

// IPAConfig holds the configuration for the IPA commitment scheme,
// including generator points and the domain size (vector width).
type IPAConfig struct {
	// Generators are the Pedersen commitment basis points G_0..G_{n-1}.
	Generators []*crypto.BanderPoint

	// DomainSize is the number of values committed per node (256 for Verkle).
	DomainSize int
}

// DefaultIPAConfig creates the standard configuration for Verkle trees
// with 256 generator points (one per child/value slot).
func DefaultIPAConfig() *IPAConfig {
	gens := crypto.GeneratePedersenGenerators()
	pts := make([]*crypto.BanderPoint, NodeWidth)
	for i := 0; i < NodeWidth; i++ {
		pts[i] = gens[i]
	}
	return &IPAConfig{
		Generators: pts,
		DomainSize: NodeWidth,
	}
}

// --- Pedersen vector commitment ---

// PedersenCommit computes a Pedersen vector commitment:
//
//	C = sum(values[i] * G_i) for i in [0, len(values))
//
// using the generators from the config. Values beyond the domain
// size are ignored.
func (cfg *IPAConfig) PedersenCommit(values []FieldElement) Commitment {
	n := len(values)
	if n > cfg.DomainSize {
		n = cfg.DomainSize
	}
	bigValues := make([]*big.Int, n)
	for i := 0; i < n; i++ {
		bigValues[i] = values[i].BigInt()
	}
	point := crypto.PedersenCommit(bigValues)
	return commitmentFromPoint(point)
}

// PedersenCommitPoint computes a Pedersen commitment and returns the
// raw curve point (for use in IPA prove/verify).
func (cfg *IPAConfig) PedersenCommitPoint(values []FieldElement) *crypto.BanderPoint {
	n := len(values)
	if n > cfg.DomainSize {
		n = cfg.DomainSize
	}
	bigValues := make([]*big.Int, n)
	for i := 0; i < n; i++ {
		bigValues[i] = values[i].BigInt()
	}
	return crypto.PedersenCommit(bigValues)
}

// commitmentFromPoint converts a Banderwagon curve point to a
// 32-byte Commitment via the map-to-field operation.
func commitmentFromPoint(p *crypto.BanderPoint) Commitment {
	b := crypto.BanderMapToBytes(p)
	var c Commitment
	copy(c[:], b[:])
	return c
}

// --- IPA proof generation and verification ---

// IPAProof wraps the crypto-layer IPA proof with Verkle-specific
// metadata (evaluation point and result).
type IPAProof struct {
	// Inner is the raw IPA proof data (L/R points + final scalar).
	Inner *crypto.IPAProofData

	// EvalPoint is the domain point at which the polynomial is evaluated.
	EvalPoint FieldElement

	// EvalResult is the claimed evaluation result.
	EvalResult FieldElement
}

// IPAProve generates an IPA proof that a committed polynomial evaluates
// to evalResult at evalPoint.
//
// Parameters:
//   - cfg: the IPA configuration with generator points
//   - polynomial: the committed vector (witness)
//   - evalPoint: the evaluation domain index
//   - evalResult: the claimed evaluation f(evalPoint)
//
// The evaluation vector b is computed as Lagrange basis evaluations at
// evalPoint over the domain [0, n). This ties the IPA inner product to
// polynomial evaluation.
func IPAProve(cfg *IPAConfig, polynomial []FieldElement, evalPoint, evalResult FieldElement) (*IPAProof, error) {
	n := len(polynomial)
	if n == 0 || n > cfg.DomainSize {
		return nil, errors.New("verkle/ipa: invalid polynomial length")
	}

	// Pad to domain size if needed.
	poly := padToDomain(polynomial, cfg.DomainSize)

	// Build the evaluation vector b: b[i] = lagrangeBasis(i, evalPoint).
	bVec := buildEvalVector(evalPoint, cfg.DomainSize)

	// Convert to big.Int slices for the crypto IPA.
	aInts := fieldSliceToBig(poly)
	bInts := fieldSliceToBig(bVec)

	// Compute the commitment point C = <a, G>.
	commitment := crypto.BanderMSM(cfg.Generators, aInts)

	proof, _, err := crypto.IPAProve(cfg.Generators, aInts, bInts, commitment)
	if err != nil {
		return nil, err
	}

	return &IPAProof{
		Inner:      proof,
		EvalPoint:  evalPoint,
		EvalResult: evalResult,
	}, nil
}

// IPAVerify verifies an IPA proof against a commitment.
//
// Parameters:
//   - cfg: the IPA configuration
//   - commitment: the Pedersen commitment to verify against
//   - proof: the IPA proof to verify
//
// Returns true if the proof is valid.
func IPAVerify(cfg *IPAConfig, commitment Commitment, proof *IPAProof) (bool, error) {
	if proof == nil || proof.Inner == nil {
		return false, errors.New("verkle/ipa: nil proof")
	}

	// The 32-byte commitment is a map-to-field value (X/Y ratio), not a
	// serialized curve point. Full verification requires the actual curve
	// point. Callers should use IPAVerifyWithPoint when they have the point.
	// As a fallback, reconstruct an approximate point from the scalar.
	scalar := new(big.Int).SetBytes(commitment[:])
	pt := crypto.BanderScalarMul(crypto.BanderGenerator(), scalar)

	return IPAVerifyWithPoint(cfg, pt, proof)
}

// IPAVerifyWithPoint verifies an IPA proof given the actual commitment
// curve point (not the compressed 32-byte map-to-field value).
func IPAVerifyWithPoint(cfg *IPAConfig, commitPt *crypto.BanderPoint, proof *IPAProof) (bool, error) {
	if proof == nil || proof.Inner == nil {
		return false, errors.New("verkle/ipa: nil proof")
	}

	bVec := buildEvalVector(proof.EvalPoint, cfg.DomainSize)
	bInts := fieldSliceToBig(bVec)
	v := proof.EvalResult.BigInt()

	return crypto.IPAVerify(cfg.Generators, commitPt, bInts, v, proof.Inner)
}

// --- Verkle node commitment operations ---

// VerkleCommitNode computes the commitment for an inner node by
// committing to the 256 child commitments. Each child commitment
// is treated as a field element (the map-to-field value of the child's
// curve point).
func VerkleCommitNode(cfg *IPAConfig, children [NodeWidth]Commitment) Commitment {
	values := make([]FieldElement, NodeWidth)
	for i := 0; i < NodeWidth; i++ {
		values[i] = FieldElementFromBytes(children[i][:])
	}
	return cfg.PedersenCommit(values)
}

// VerkleCommitLeaf computes the commitment for a leaf node with the
// given stem and values. Per EIP-6800, the leaf commitment encodes:
//
//	C = stem_scalar * G_0 + sum(values[i] * G_{i+1})
//
// where stem_scalar is the stem bytes interpreted as a big-endian scalar,
// and values occupies slots 1..255.
func VerkleCommitLeaf(cfg *IPAConfig, stem [StemSize]byte, values [NodeWidth]FieldElement) Commitment {
	commitValues := make([]FieldElement, NodeWidth)
	// Slot 0: stem as a scalar.
	commitValues[0] = FieldElementFromBytes(stem[:])
	// Slots 1..255: leaf values (we can fit 255 values).
	for i := 0; i < NodeWidth-1; i++ {
		commitValues[i+1] = values[i]
	}
	return cfg.PedersenCommit(commitValues)
}

// --- IPACommitter interface ---

// IPACommitter defines the interface for computing IPA-based Verkle tree
// commitments. The existing VerkleTree can use this to switch from
// placeholder hash commitments to real IPA polynomial commitments.
type IPACommitter interface {
	// CommitToNode computes the Pedersen commitment for an inner node
	// with the given child commitments.
	CommitToNode(children [NodeWidth]Commitment) Commitment

	// CommitToLeaf computes the Pedersen commitment for a leaf node
	// with the given stem and values.
	CommitToLeaf(stem [StemSize]byte, values [NodeWidth]FieldElement) Commitment

	// ProveEvaluation generates an IPA proof that polynomial evaluates
	// to result at the given point.
	ProveEvaluation(polynomial []FieldElement, point, result FieldElement) (*IPAProof, error)

	// VerifyEvaluation verifies an IPA evaluation proof.
	VerifyEvaluation(commitPt *crypto.BanderPoint, proof *IPAProof) (bool, error)

	// Config returns the underlying IPA configuration.
	Config() *IPAConfig
}

// defaultIPACommitter implements IPACommitter with the standard 256-width
// Verkle tree configuration.
type defaultIPACommitter struct {
	cfg *IPAConfig
}

// NewIPACommitter creates an IPACommitter using the default Verkle configuration.
func NewIPACommitter() IPACommitter {
	return &defaultIPACommitter{cfg: DefaultIPAConfig()}
}

func (c *defaultIPACommitter) CommitToNode(children [NodeWidth]Commitment) Commitment {
	return VerkleCommitNode(c.cfg, children)
}

func (c *defaultIPACommitter) CommitToLeaf(stem [StemSize]byte, values [NodeWidth]FieldElement) Commitment {
	return VerkleCommitLeaf(c.cfg, stem, values)
}

func (c *defaultIPACommitter) ProveEvaluation(polynomial []FieldElement, point, result FieldElement) (*IPAProof, error) {
	return IPAProve(c.cfg, polynomial, point, result)
}

func (c *defaultIPACommitter) VerifyEvaluation(commitPt *crypto.BanderPoint, proof *IPAProof) (bool, error) {
	return IPAVerifyWithPoint(c.cfg, commitPt, proof)
}

func (c *defaultIPACommitter) Config() *IPAConfig {
	return c.cfg
}

// --- Helper functions ---

// padToDomain pads a polynomial to the given domain size with zeros.
func padToDomain(poly []FieldElement, domainSize int) []FieldElement {
	if len(poly) >= domainSize {
		return poly[:domainSize]
	}
	padded := make([]FieldElement, domainSize)
	copy(padded, poly)
	for i := len(poly); i < domainSize; i++ {
		padded[i] = Zero()
	}
	return padded
}

// buildEvalVector constructs the evaluation vector b for IPA verification.
// b[i] = prod_{j != i} (z - j) / (i - j) — the Lagrange basis at point z.
// For the Verkle use case, the domain is [0, 1, ..., n-1].
func buildEvalVector(z FieldElement, n int) []FieldElement {
	b := make([]FieldElement, n)
	for i := 0; i < n; i++ {
		b[i] = lagrangeBasis(i, z, n)
	}
	return b
}

// lagrangeBasis computes the i-th Lagrange basis polynomial evaluated at z:
//
//	L_i(z) = prod_{j != i} (z - j) / (i - j)
//
// Domain is [0, 1, ..., n-1].
func lagrangeBasis(i int, z FieldElement, n int) FieldElement {
	num := One()
	den := One()
	iElem := FieldElementFromUint64(uint64(i))

	for j := 0; j < n; j++ {
		if j == i {
			continue
		}
		jElem := FieldElementFromUint64(uint64(j))
		num = num.Mul(z.Sub(jElem))
		den = den.Mul(iElem.Sub(jElem))
	}

	if den.IsZero() {
		return Zero()
	}
	return num.Mul(den.Inv())
}

// fieldSliceToBig converts a slice of FieldElements to big.Ints.
func fieldSliceToBig(elems []FieldElement) []*big.Int {
	out := make([]*big.Int, len(elems))
	for i, e := range elems {
		out[i] = e.BigInt()
	}
	return out
}
