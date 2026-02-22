package types

import (
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/rlp"
	"golang.org/x/crypto/sha3"
)

var (
	errInvalidSig         = errors.New("invalid transaction signature")
	errInvalidChainID     = errors.New("invalid chain ID for signer")
	errTxTypeNotSupported = errors.New("transaction type not supported by signer")
	errNoRecovery         = errors.New("public key recovery failed")
)

// secp256k1 curve order, used for signature validation.
var secp256k1NCopy, _ = new(big.Int).SetString(
	"fffffffffffffffffffffffffffffffebaaedce6af48a03bbfd25e8cd0364141", 16,
)

// secp256k1 curve parameters for local recovery (avoids importing crypto).
var (
	secp256k1P, _  = new(big.Int).SetString("fffffffffffffffffffffffffffffffffffffffffffffffffffffffefffffc2f", 16)
	secp256k1Gx, _ = new(big.Int).SetString("79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798", 16)
	secp256k1Gy, _ = new(big.Int).SetString("483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d08ffb10d4b8", 16)
	secp256k1B     = big.NewInt(7)
)

// Signer provides methods for hashing transactions and recovering the sender.
type Signer interface {
	// ChainID returns the chain ID this signer operates on.
	ChainID() uint64

	// Hash returns the signing hash for the given transaction.
	Hash(tx *Transaction) Hash

	// SignatureValues parses a 65-byte raw signature [R || S || V] into
	// its component r, s values and a normalized v byte (0 or 1).
	SignatureValues(sig []byte) (r, s *big.Int, v byte, err error)

	// Sender recovers the sender address from the transaction's signature.
	Sender(tx *Transaction) (Address, error)
}

// EIP155Signer implements Signer for legacy EIP-155 replay-protected txs.
type EIP155Signer struct {
	chainID    uint64
	chainIDBig *big.Int
}

// NewEIP155Signer creates a signer for EIP-155 legacy transactions.
func NewEIP155Signer(chainID uint64) EIP155Signer {
	return EIP155Signer{
		chainID:    chainID,
		chainIDBig: new(big.Int).SetUint64(chainID),
	}
}

// ChainID returns the chain ID.
func (s EIP155Signer) ChainID() uint64 { return s.chainID }

// Hash returns the signing hash for a legacy transaction.
func (s EIP155Signer) Hash(tx *Transaction) Hash {
	if tx.Type() != LegacyTxType {
		return Hash{}
	}
	return tx.SigningHash()
}

// SignatureValues parses a 65-byte [R||S||V] signature.
func (s EIP155Signer) SignatureValues(sig []byte) (r, s2 *big.Int, v byte, err error) {
	return parseSignatureValues(sig)
}

// Sender recovers the sender address from a legacy transaction's signature.
func (s EIP155Signer) Sender(tx *Transaction) (Address, error) {
	if tx.Type() != LegacyTxType {
		return Address{}, errTxTypeNotSupported
	}
	v, r, rs := tx.RawSignatureValues()
	if v == nil || r == nil || rs == nil {
		return Address{}, errInvalidSig
	}

	var recovery byte
	vVal := v.Uint64()
	if vVal == 27 || vVal == 28 {
		recovery = byte(vVal - 27)
	} else {
		// EIP-155: V = chainID*2 + 35 + recoveryID
		recovery = byte(vVal - 35 - 2*s.chainID)
	}
	if recovery > 1 {
		return Address{}, errInvalidSig
	}

	sigHash := tx.SigningHash()
	return RecoverPlain(sigHash, r, rs, recovery)
}

// LondonSigner implements Signer for EIP-1559 dynamic fee transactions
// and also supports legacy, access-list, blob, and set-code txs.
type LondonSigner struct {
	chainID    uint64
	chainIDBig *big.Int
}

// NewLondonSigner creates a signer that supports all tx types.
func NewLondonSigner(chainID uint64) LondonSigner {
	return LondonSigner{
		chainID:    chainID,
		chainIDBig: new(big.Int).SetUint64(chainID),
	}
}

// ChainID returns the chain ID.
func (s LondonSigner) ChainID() uint64 { return s.chainID }

// Hash returns the signing hash for the given transaction.
func (s LondonSigner) Hash(tx *Transaction) Hash {
	return tx.SigningHash()
}

// SignatureValues parses a 65-byte [R||S||V] signature.
func (s LondonSigner) SignatureValues(sig []byte) (r, s2 *big.Int, v byte, err error) {
	return parseSignatureValues(sig)
}

// Sender recovers the sender address from the transaction's signature.
func (s LondonSigner) Sender(tx *Transaction) (Address, error) {
	v, r, rs := tx.RawSignatureValues()
	if r == nil || rs == nil {
		return Address{}, errInvalidSig
	}

	var recovery byte
	switch tx.Type() {
	case LegacyTxType:
		if v == nil {
			return Address{}, errInvalidSig
		}
		vVal := v.Uint64()
		if vVal == 27 || vVal == 28 {
			recovery = byte(vVal - 27)
		} else {
			recovery = byte(vVal - 35 - 2*s.chainID)
		}
	case AccessListTxType, DynamicFeeTxType, BlobTxType, SetCodeTxType:
		if v == nil {
			recovery = 0
		} else {
			recovery = byte(v.Uint64())
		}
		txChainID := tx.ChainId()
		if txChainID != nil && txChainID.Uint64() != s.chainID {
			return Address{}, errInvalidChainID
		}
	default:
		return Address{}, errTxTypeNotSupported
	}

	if recovery > 1 {
		return Address{}, errInvalidSig
	}

	sigHash := tx.SigningHash()
	return RecoverPlain(sigHash, r, rs, recovery)
}

// LatestSigner returns the most feature-complete signer for the given chain ID.
func LatestSigner(chainID uint64) Signer {
	return NewLondonSigner(chainID)
}

// MakeSigner returns the appropriate signer for a given chain ID and tx type.
func MakeSigner(chainID uint64, txType uint8) Signer {
	switch txType {
	case LegacyTxType:
		return NewEIP155Signer(chainID)
	default:
		return NewLondonSigner(chainID)
	}
}

// SigningHash computes the EIP-155 signing hash for a legacy transaction
// from its individual fields:
// Keccak256(RLP([nonce, gasPrice, gas, to, value, data, chainID, 0, 0]))
func SigningHash(chainID uint64, nonce uint64, to *Address, value *big.Int,
	gas uint64, data []byte) Hash {

	toBytes := make([]byte, 0)
	if to != nil {
		toBytes = to[:]
	}

	var items [][]byte
	enc := func(v interface{}) {
		b, _ := rlp.EncodeToBytes(v)
		items = append(items, b)
	}

	enc(nonce)
	enc(bigOrZero(value))
	enc(gas)
	enc(toBytes)
	enc(bigOrZero(value))
	enc(data)

	chainBig := new(big.Int).SetUint64(chainID)
	if chainBig.Sign() > 0 {
		enc(chainBig)
		enc(uint(0))
		enc(uint(0))
	}

	var payload []byte
	for _, item := range items {
		payload = append(payload, item...)
	}
	encoded := rlp.WrapList(payload)

	d := sha3.NewLegacyKeccak256()
	d.Write(encoded)
	var h Hash
	copy(h[:], d.Sum(nil))
	return h
}

// RecoverPlain recovers the sender address from an ECDSA signature.
// sighash is the 32-byte message hash, r and s are the signature values,
// and v is the recovery ID (0 or 1).
func RecoverPlain(sighash Hash, r, s *big.Int, v byte) (Address, error) {
	if v > 1 {
		return Address{}, errInvalidSig
	}
	if r.Sign() <= 0 || s.Sign() <= 0 {
		return Address{}, errInvalidSig
	}
	if r.Cmp(secp256k1NCopy) >= 0 || s.Cmp(secp256k1NCopy) >= 0 {
		return Address{}, errInvalidSig
	}

	pub, err := recoverPubkey(sighash[:], r, s, v)
	if err != nil {
		return Address{}, err
	}

	// Address = Keccak256(pub[1:])[12:] where pub is 65-byte uncompressed.
	d := sha3.NewLegacyKeccak256()
	d.Write(pub[1:])
	hash := d.Sum(nil)
	return BytesToAddress(hash[12:]), nil
}

// parseSignatureValues validates and parses a 65-byte [R||S||V] signature.
func parseSignatureValues(sig []byte) (*big.Int, *big.Int, byte, error) {
	if len(sig) != 65 {
		return nil, nil, 0, errInvalidSig
	}
	r := new(big.Int).SetBytes(sig[0:32])
	s := new(big.Int).SetBytes(sig[32:64])
	v := sig[64]
	if v > 1 {
		return nil, nil, 0, errInvalidSig
	}
	if r.Sign() <= 0 || s.Sign() <= 0 {
		return nil, nil, 0, errInvalidSig
	}
	if r.Cmp(secp256k1NCopy) >= 0 || s.Cmp(secp256k1NCopy) >= 0 {
		return nil, nil, 0, errInvalidSig
	}
	return r, s, v, nil
}

// recoverPubkey recovers the uncompressed public key (65 bytes, 0x04 prefix)
// from a hash, signature r/s values, and recovery ID v.
// Uses pure big.Int EC math to avoid the crypto package import cycle.
func recoverPubkey(hash []byte, r, s *big.Int, v byte) ([]byte, error) {
	// Step 1: R point x-coordinate = r.
	x := new(big.Int).Set(r)
	if x.Cmp(secp256k1P) >= 0 {
		return nil, errNoRecovery
	}

	// Step 2: Compute y from x: y^2 = x^3 + 7 (mod p).
	y := signerComputeY(x)
	if y == nil {
		return nil, errNoRecovery
	}

	// Choose y parity based on v.
	if y.Bit(0) != uint(v&1) {
		y.Sub(secp256k1P, y)
	}

	// Step 3: Recover Q = r^{-1} * (s*R - e*G).
	rInv := new(big.Int).ModInverse(r, secp256k1NCopy)
	if rInv == nil {
		return nil, errNoRecovery
	}
	e := new(big.Int).SetBytes(hash)

	// s*R
	sRx, sRy := signerScalarMult(x, y, s)

	// e*G
	eGx, eGy := signerScalarMult(secp256k1Gx, secp256k1Gy, e)

	// -e*G (negate y)
	negEGy := new(big.Int).Sub(secp256k1P, eGy)
	negEGy.Mod(negEGy, secp256k1P)

	// s*R + (-e*G)
	diffX, diffY := signerPointAdd(sRx, sRy, eGx, negEGy)

	// Q = r^{-1} * (s*R - e*G)
	qx, qy := signerScalarMult(diffX, diffY, rInv)

	if qx.Sign() == 0 && qy.Sign() == 0 {
		return nil, errNoRecovery
	}

	// Verify recovered key with ecdsa.Verify.
	// We use a minimal CurveParams just for Verify (which doesn't call ScalarMult).
	if !signerVerify(hash, r, s, qx, qy) {
		return nil, errNoRecovery
	}

	// Marshal to 65-byte uncompressed: [0x04 || X(32) || Y(32)].
	pub := make([]byte, 65)
	pub[0] = 0x04
	xBytes := qx.Bytes()
	yBytes := qy.Bytes()
	copy(pub[1+32-len(xBytes):33], xBytes)
	copy(pub[33+32-len(yBytes):65], yBytes)
	return pub, nil
}

// signerComputeY computes y = sqrt(x^3 + 7) mod p for secp256k1.
func signerComputeY(x *big.Int) *big.Int {
	x3 := new(big.Int).Mul(x, x)
	x3.Mod(x3, secp256k1P)
	x3.Mul(x3, x)
	x3.Mod(x3, secp256k1P)
	x3.Add(x3, secp256k1B)
	x3.Mod(x3, secp256k1P)

	// p = 3 mod 4, so sqrt(a) = a^((p+1)/4).
	exp := new(big.Int).Add(secp256k1P, big.NewInt(1))
	exp.Rsh(exp, 2)
	y := new(big.Int).Exp(x3, exp, secp256k1P)

	// Verify.
	y2 := new(big.Int).Mul(y, y)
	y2.Mod(y2, secp256k1P)
	if y2.Cmp(x3) != 0 {
		return nil
	}
	return y
}

// signerPointAdd adds two points on the secp256k1 curve.
func signerPointAdd(x1, y1, x2, y2 *big.Int) (*big.Int, *big.Int) {
	if x1.Sign() == 0 && y1.Sign() == 0 {
		return new(big.Int).Set(x2), new(big.Int).Set(y2)
	}
	if x2.Sign() == 0 && y2.Sign() == 0 {
		return new(big.Int).Set(x1), new(big.Int).Set(y1)
	}
	if x1.Cmp(x2) == 0 && y1.Cmp(y2) == 0 {
		return signerPointDouble(x1, y1)
	}
	if x1.Cmp(x2) == 0 {
		return new(big.Int), new(big.Int)
	}
	p := secp256k1P
	dy := new(big.Int).Sub(y2, y1)
	dy.Mod(dy, p)
	dx := new(big.Int).Sub(x2, x1)
	dx.Mod(dx, p)
	dxInv := new(big.Int).ModInverse(dx, p)
	if dxInv == nil {
		return new(big.Int), new(big.Int)
	}
	slope := new(big.Int).Mul(dy, dxInv)
	slope.Mod(slope, p)
	x3 := new(big.Int).Mul(slope, slope)
	x3.Sub(x3, x1)
	x3.Sub(x3, x2)
	x3.Mod(x3, p)
	y3 := new(big.Int).Sub(x1, x3)
	y3.Mul(y3, slope)
	y3.Sub(y3, y1)
	y3.Mod(y3, p)
	return x3, y3
}

// signerPointDouble doubles a point on the secp256k1 curve.
func signerPointDouble(x1, y1 *big.Int) (*big.Int, *big.Int) {
	if y1.Sign() == 0 {
		return new(big.Int), new(big.Int)
	}
	p := secp256k1P
	x1sq := new(big.Int).Mul(x1, x1)
	x1sq.Mod(x1sq, p)
	num := new(big.Int).Mul(big.NewInt(3), x1sq)
	num.Mod(num, p)
	den := new(big.Int).Mul(big.NewInt(2), y1)
	den.Mod(den, p)
	denInv := new(big.Int).ModInverse(den, p)
	if denInv == nil {
		return new(big.Int), new(big.Int)
	}
	slope := new(big.Int).Mul(num, denInv)
	slope.Mod(slope, p)
	x3 := new(big.Int).Mul(slope, slope)
	x3.Sub(x3, new(big.Int).Mul(big.NewInt(2), x1))
	x3.Mod(x3, p)
	y3 := new(big.Int).Sub(x1, x3)
	y3.Mul(y3, slope)
	y3.Sub(y3, y1)
	y3.Mod(y3, p)
	return x3, y3
}

// signerScalarMult computes k * (px, py) on secp256k1 using double-and-add.
func signerScalarMult(px, py, k *big.Int) (*big.Int, *big.Int) {
	scalar := new(big.Int).Set(k)
	scalar.Mod(scalar, secp256k1NCopy)
	if scalar.Sign() == 0 {
		return new(big.Int), new(big.Int)
	}
	rx, ry := new(big.Int), new(big.Int)
	bx, by := new(big.Int).Set(px), new(big.Int).Set(py)
	for i := scalar.BitLen() - 1; i >= 0; i-- {
		rx, ry = signerPointDouble(rx, ry)
		if scalar.Bit(i) == 1 {
			rx, ry = signerPointAdd(rx, ry, bx, by)
		}
	}
	return rx, ry
}

// signerVerify verifies an ECDSA signature using the recovered public key.
// This avoids elliptic.CurveParams.ScalarMult which panics for secp256k1.
func signerVerify(hash []byte, r, s, qx, qy *big.Int) bool {
	n := secp256k1NCopy
	if r.Sign() <= 0 || r.Cmp(n) >= 0 {
		return false
	}
	if s.Sign() <= 0 || s.Cmp(n) >= 0 {
		return false
	}
	e := new(big.Int).SetBytes(hash)
	sInv := new(big.Int).ModInverse(s, n)
	if sInv == nil {
		return false
	}
	u1 := new(big.Int).Mul(e, sInv)
	u1.Mod(u1, n)
	u2 := new(big.Int).Mul(r, sInv)
	u2.Mod(u2, n)

	// u1*G + u2*Q
	x1, y1 := signerScalarMult(secp256k1Gx, secp256k1Gy, u1)
	x2, y2 := signerScalarMult(qx, qy, u2)
	rx, _ := signerPointAdd(x1, y1, x2, y2)

	rx.Mod(rx, n)
	return rx.Cmp(r) == 0
}

// Ensure EIP155Signer and LondonSigner satisfy the Signer interface.
var (
	_ Signer = EIP155Signer{}
	_ Signer = LondonSigner{}
)

