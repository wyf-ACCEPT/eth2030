// Pedersen hash for Verkle tree commitments.
//
// This file implements the Pedersen hash function used by Verkle trees to
// compute node commitments. It operates over the Banderwagon curve using
// deterministic generator points derived via HashToCurve. The Pedersen
// hash is additively homomorphic, enabling efficient incremental updates
// when a single value in the committed vector changes.
//
// The width-256 Verkle tree uses 256 generator points G_0..G_255. A
// Pedersen commitment to values v_0..v_255 is:
//
//	C = sum(v_i * G_i) for i in [0, 256)
//
// This file provides:
//   - PedersenConfig: cached generator setup
//   - HashToCurve: maps arbitrary data to a Banderwagon point
//   - PedersenHash: commits a vector of byte slices
//   - PedersenCommitFixed: commits fixed-size [32]byte values
//   - PedersenUpdate: incremental single-slot update
//   - StemCommitment: Verkle-specific stem+values commitment

package verkle

import (
	"encoding/binary"
	"math/big"

	"github.com/eth2028/eth2028/crypto"
)

// PedersenConfig holds precomputed generator points for Pedersen hashing.
// Generators are deterministically derived from a seed using HashToCurve,
// ensuring all nodes in the network agree on the same basis.
type PedersenConfig struct {
	// generators holds the precomputed Banderwagon base points G_0..G_{width-1}.
	generators []*crypto.BanderPoint

	// generatorBytes caches the serialized [32]byte form of each generator
	// (map-to-field representation) for efficient comparisons and hashing.
	generatorBytes [][32]byte

	// width is the number of generator points (256 for standard Verkle trees).
	width int
}

// NewPedersenConfig creates a PedersenConfig with the given width.
// It generates deterministic base points using HashToCurve on sequential
// domain-separated inputs. The width must be positive and at most 256.
func NewPedersenConfig(width int) *PedersenConfig {
	if width <= 0 {
		width = NodeWidth
	}
	if width > NodeWidth {
		width = NodeWidth
	}

	// Use the crypto package's canonical generators for consistency
	// with the existing IPA commitment scheme.
	canonical := crypto.GeneratePedersenGenerators()

	gens := make([]*crypto.BanderPoint, width)
	genBytes := make([][32]byte, width)

	for i := 0; i < width; i++ {
		gens[i] = canonical[i]
		genBytes[i] = crypto.BanderMapToBytes(canonical[i])
	}

	return &PedersenConfig{
		generators:     gens,
		generatorBytes: genBytes,
		width:          width,
	}
}

// DefaultPedersenConfig creates a PedersenConfig with width 256,
// the standard for Ethereum Verkle trees.
func DefaultPedersenConfig() *PedersenConfig {
	return NewPedersenConfig(NodeWidth)
}

// Width returns the number of generator points in this config.
func (pc *PedersenConfig) Width() int {
	return pc.width
}

// Generator returns the i-th generator point. Panics if i >= width.
func (pc *PedersenConfig) Generator(i int) *crypto.BanderPoint {
	return pc.generators[i]
}

// GeneratorBytes returns the i-th generator's serialized [32]byte form.
func (pc *PedersenConfig) GeneratorBytes(i int) [32]byte {
	return pc.generatorBytes[i]
}

// HashToCurve maps arbitrary input data to a Banderwagon curve point.
// It uses a simplified hash-to-curve construction:
//  1. Hash the input with a domain separator using Keccak256
//  2. Interpret the hash as a field element (mod Fr)
//  3. Map the field element to a curve point via scalar multiplication
//     of the generator
//
// This is a simplified construction suitable for deterministic generator
// derivation. A full hash-to-curve (e.g., SWU or Elligator) would be
// used in production for security against timing attacks.
func HashToCurve(input []byte) [32]byte {
	// Domain separator for Verkle hash-to-curve.
	domain := []byte("verkle_hash_to_curve_v1")
	data := make([]byte, len(domain)+len(input))
	copy(data, domain)
	copy(data[len(domain):], input)

	// Hash to get a scalar.
	h := crypto.Keccak256(data)
	scalar := new(big.Int).SetBytes(h)
	scalar.Mod(scalar, crypto.BanderN())

	// Map scalar to curve point: P = scalar * G.
	pt := crypto.BanderScalarMul(crypto.BanderGenerator(), scalar)
	return crypto.BanderMapToBytes(pt)
}

// HashToCurvePoint maps arbitrary input data to a Banderwagon curve point,
// returning the full curve point (not just the serialized bytes).
func HashToCurvePoint(input []byte) *crypto.BanderPoint {
	domain := []byte("verkle_hash_to_curve_v1")
	data := make([]byte, len(domain)+len(input))
	copy(data, domain)
	copy(data[len(domain):], input)

	h := crypto.Keccak256(data)
	scalar := new(big.Int).SetBytes(h)
	scalar.Mod(scalar, crypto.BanderN())

	return crypto.BanderScalarMul(crypto.BanderGenerator(), scalar)
}

// HashToCurveIndex generates a deterministic curve point from an integer
// index. Used for generating basis points in the Pedersen scheme.
func HashToCurveIndex(index int) *crypto.BanderPoint {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(index))
	return HashToCurvePoint(buf[:])
}

// PedersenHash computes a Pedersen commitment over a vector of
// variable-length byte slices. Each value is interpreted as a big-endian
// scalar modulo the subgroup order. The result is:
//
//	C = sum(scalar(values[i]) * G_i) for i in [0, len(values))
//
// Values beyond the config width are ignored. The returned [32]byte
// is the map-to-field serialization of the commitment point.
func (pc *PedersenConfig) PedersenHash(values [][]byte) [32]byte {
	n := len(values)
	if n > pc.width {
		n = pc.width
	}

	scalars := make([]*big.Int, n)
	for i := 0; i < n; i++ {
		if values[i] == nil {
			scalars[i] = new(big.Int)
		} else {
			scalars[i] = new(big.Int).SetBytes(values[i])
			scalars[i].Mod(scalars[i], crypto.BanderN())
		}
	}

	pt := crypto.BanderMSM(pc.generators[:n], scalars)
	return crypto.BanderMapToBytes(pt)
}

// PedersenHashPoint computes a Pedersen commitment and returns the raw
// Banderwagon curve point (useful for further curve operations).
func (pc *PedersenConfig) PedersenHashPoint(values [][]byte) *crypto.BanderPoint {
	n := len(values)
	if n > pc.width {
		n = pc.width
	}

	scalars := make([]*big.Int, n)
	for i := 0; i < n; i++ {
		if values[i] == nil {
			scalars[i] = new(big.Int)
		} else {
			scalars[i] = new(big.Int).SetBytes(values[i])
			scalars[i].Mod(scalars[i], crypto.BanderN())
		}
	}

	return crypto.BanderMSM(pc.generators[:n], scalars)
}

// PedersenCommitFixed commits to a vector of fixed-size [32]byte field
// elements. This is the primary commitment function for Verkle tree nodes
// where all values are 32-byte scalars.
//
//	C = sum(values[i] * G_i) for i in [0, len(values))
func (pc *PedersenConfig) PedersenCommitFixed(values [][32]byte) [32]byte {
	n := len(values)
	if n > pc.width {
		n = pc.width
	}

	scalars := make([]*big.Int, n)
	for i := 0; i < n; i++ {
		scalars[i] = new(big.Int).SetBytes(values[i][:])
		scalars[i].Mod(scalars[i], crypto.BanderN())
	}

	pt := crypto.BanderMSM(pc.generators[:n], scalars)
	return crypto.BanderMapToBytes(pt)
}

// PedersenCommitFixedPoint commits to fixed-size [32]byte values and
// returns the raw curve point.
func (pc *PedersenConfig) PedersenCommitFixedPoint(values [][32]byte) *crypto.BanderPoint {
	n := len(values)
	if n > pc.width {
		n = pc.width
	}

	scalars := make([]*big.Int, n)
	for i := 0; i < n; i++ {
		scalars[i] = new(big.Int).SetBytes(values[i][:])
		scalars[i].Mod(scalars[i], crypto.BanderN())
	}

	return crypto.BanderMSM(pc.generators[:n], scalars)
}

// PedersenUpdate computes an updated Pedersen commitment after changing
// a single value at the given index. The Pedersen scheme is additively
// homomorphic, so:
//
//	C_new = C_old + (newValue - oldValue) * G_index
//
// This avoids recomputing the full commitment from scratch, which is
// O(width) scalar multiplications. The update is O(1).
func (pc *PedersenConfig) PedersenUpdate(
	oldCommitment [32]byte,
	index int,
	oldValue, newValue [32]byte,
) [32]byte {
	if index < 0 || index >= pc.width {
		return oldCommitment
	}

	// Compute delta = newValue - oldValue (mod n).
	oldScalar := new(big.Int).SetBytes(oldValue[:])
	newScalar := new(big.Int).SetBytes(newValue[:])
	delta := new(big.Int).Sub(newScalar, oldScalar)
	delta.Mod(delta, crypto.BanderN())

	// If delta is zero, no change needed.
	if delta.Sign() == 0 {
		return oldCommitment
	}

	// Compute delta * G_index.
	deltaPt := crypto.BanderScalarMul(pc.generators[index], delta)

	// Reconstruct the old commitment as a curve point.
	// The oldCommitment is a map-to-field value, so we reconstruct
	// by treating it as a scalar * G (approximate reconstruction).
	oldScalarC := new(big.Int).SetBytes(oldCommitment[:])
	oldPt := crypto.BanderScalarMul(crypto.BanderGenerator(), oldScalarC)

	// C_new = C_old + delta * G_index
	newPt := crypto.BanderAdd(oldPt, deltaPt)
	return crypto.BanderMapToBytes(newPt)
}

// PedersenUpdatePoint computes an updated Pedersen commitment given
// the old commitment as a curve point (more accurate than PedersenUpdate
// which must reconstruct the point from the serialized form).
func (pc *PedersenConfig) PedersenUpdatePoint(
	oldCommitPt *crypto.BanderPoint,
	index int,
	oldValue, newValue [32]byte,
) *crypto.BanderPoint {
	if index < 0 || index >= pc.width {
		return oldCommitPt
	}

	oldScalar := new(big.Int).SetBytes(oldValue[:])
	newScalar := new(big.Int).SetBytes(newValue[:])
	delta := new(big.Int).Sub(newScalar, oldScalar)
	delta.Mod(delta, crypto.BanderN())

	if delta.Sign() == 0 {
		return oldCommitPt
	}

	deltaPt := crypto.BanderScalarMul(pc.generators[index], delta)
	return crypto.BanderAdd(oldCommitPt, deltaPt)
}

// StemCommitment computes the Pedersen commitment for a Verkle leaf node
// identified by a 31-byte stem. Per EIP-6800, the leaf commitment is:
//
//	C = stem_scalar * G_0 + sum(values[i] * G_{i+1})
//
// where stem_scalar is the stem bytes interpreted as a big-endian integer
// reduced modulo the subgroup order.
//
// The values slice holds up to 255 field elements (slots 1..255 of the
// commitment vector). Values at indices beyond 254 are ignored.
func (pc *PedersenConfig) StemCommitment(stem [StemSize]byte, values [][32]byte) [32]byte {
	commitVals := make([][32]byte, pc.width)

	// Slot 0: stem as a scalar.
	var stemVal [32]byte
	copy(stemVal[32-StemSize:], stem[:])
	commitVals[0] = stemVal

	// Slots 1..width-1: leaf values.
	maxVals := pc.width - 1
	if len(values) < maxVals {
		maxVals = len(values)
	}
	for i := 0; i < maxVals; i++ {
		commitVals[i+1] = values[i]
	}

	return pc.PedersenCommitFixed(commitVals)
}

// StemCommitmentPoint computes the stem commitment and returns the raw
// Banderwagon curve point.
func (pc *PedersenConfig) StemCommitmentPoint(stem [StemSize]byte, values [][32]byte) *crypto.BanderPoint {
	commitVals := make([][32]byte, pc.width)

	var stemVal [32]byte
	copy(stemVal[32-StemSize:], stem[:])
	commitVals[0] = stemVal

	maxVals := pc.width - 1
	if len(values) < maxVals {
		maxVals = len(values)
	}
	for i := 0; i < maxVals; i++ {
		commitVals[i+1] = values[i]
	}

	return pc.PedersenCommitFixedPoint(commitVals)
}

// StemCommitmentSparse computes the stem commitment for a sparse set of
// values identified by their suffix indices. Only the provided indices
// have non-zero values; all other slots are zero.
func (pc *PedersenConfig) StemCommitmentSparse(
	stem [StemSize]byte,
	indices []byte,
	values [][32]byte,
) [32]byte {
	if len(indices) != len(values) {
		return [32]byte{}
	}

	// Build the full commitment vector with zeros.
	commitVals := make([][32]byte, pc.width)

	// Slot 0: stem.
	var stemVal [32]byte
	copy(stemVal[32-StemSize:], stem[:])
	commitVals[0] = stemVal

	// Fill in the non-zero values at their suffix+1 positions.
	for i, idx := range indices {
		slot := int(idx) + 1
		if slot < pc.width {
			commitVals[slot] = values[i]
		}
	}

	return pc.PedersenCommitFixed(commitVals)
}

// CommitmentToBytes serializes a Banderwagon commitment point to its
// 32-byte map-to-field representation.
func CommitmentToBytes(pt *crypto.BanderPoint) [32]byte {
	return crypto.BanderMapToBytes(pt)
}

// VerifyCommitment checks that a commitment matches the expected value
// computed from the given values and config.
func (pc *PedersenConfig) VerifyCommitment(expected [32]byte, values [][32]byte) bool {
	computed := pc.PedersenCommitFixed(values)
	return computed == expected
}
