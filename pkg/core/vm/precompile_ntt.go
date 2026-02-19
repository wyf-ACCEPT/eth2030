package vm

import (
	"errors"
	"math/big"
	"math/bits"

	"github.com/eth2028/eth2028/core/types"
)

// NTT precompile over BN254 scalar field.
// Address: 0x15 (registered for I+ fork).

// BN254 scalar field modulus.
var bn254ScalarField, _ = new(big.Int).SetString(
	"21888242871839275222246405745257275088548364400416034343698204186575808495617", 10)

// Known primitive root of the BN254 scalar field (a generator of the multiplicative group).
// 5 is a primitive root mod bn254ScalarField.
var bn254PrimitiveRoot = big.NewInt(5)

// NTT gas cost constants.
const (
	NTTBaseCost       uint64 = 1000
	NTTPerElementCost uint64 = 10
	NTTMaxDegree             = 1 << 16 // 65536
)

// NTT errors.
var (
	ErrNTTInvalidInput  = errors.New("ntt: invalid input")
	ErrNTTNotPowerOfTwo = errors.New("ntt: size must be a power of two")
	ErrNTTTooLarge      = errors.New("ntt: exceeds maximum degree")
	ErrNTTInvalidOpType = errors.New("ntt: invalid operation type")
	ErrNTTNoRootOfUnity = errors.New("ntt: no root of unity for given size")
)

type nttPrecompile struct{}

func (c *nttPrecompile) RequiredGas(input []byte) uint64 {
	if len(input) < 1 {
		return 0
	}
	nBytes := len(input) - 1
	n := nBytes / 32
	if n == 0 {
		return NTTBaseCost
	}
	// gas = base + n * log2(n) * perElement
	log2n := uint64(bits.Len(uint(n)) - 1)
	if log2n == 0 {
		log2n = 1
	}
	return NTTBaseCost + uint64(n)*log2n*NTTPerElementCost
}

func (c *nttPrecompile) Run(input []byte) ([]byte, error) {
	if len(input) < 1 {
		return nil, ErrNTTInvalidInput
	}

	opType := input[0]
	if opType > 1 {
		return nil, ErrNTTInvalidOpType
	}

	coeffData := input[1:]
	if len(coeffData)%32 != 0 {
		return nil, ErrNTTInvalidInput
	}
	n := len(coeffData) / 32
	if n == 0 {
		return nil, ErrNTTInvalidInput
	}
	if n > NTTMaxDegree {
		return nil, ErrNTTTooLarge
	}
	if n&(n-1) != 0 {
		return nil, ErrNTTNotPowerOfTwo
	}

	// Parse coefficients as big-endian 32-byte big.Ints, reduced mod p.
	coeffs := make([]*big.Int, n)
	for i := 0; i < n; i++ {
		val := new(big.Int).SetBytes(coeffData[i*32 : (i+1)*32])
		val.Mod(val, bn254ScalarField)
		coeffs[i] = val
	}

	// Find root of unity.
	omega, err := findRootOfUnity(n, bn254ScalarField)
	if err != nil {
		return nil, err
	}

	var result []*big.Int
	if opType == 0x00 {
		result = nttForward(coeffs, omega, bn254ScalarField)
	} else {
		result = nttInverse(coeffs, omega, bn254ScalarField)
	}

	// Encode output: n * 32-byte big-endian values.
	out := make([]byte, n*32)
	for i, val := range result {
		b := val.Bytes()
		copy(out[i*32+(32-len(b)):], b)
	}
	return out, nil
}

// findRootOfUnity finds a primitive n-th root of unity mod p.
// For BN254 scalar field, p-1 = 2^28 * m, so we can support up to n = 2^28.
// omega = g^((p-1)/n) where g is a primitive root.
func findRootOfUnity(n int, p *big.Int) (*big.Int, error) {
	if n <= 0 || n&(n-1) != 0 {
		return nil, ErrNTTNotPowerOfTwo
	}

	// Check that n divides p-1.
	pMinus1 := new(big.Int).Sub(p, big.NewInt(1))
	nBig := big.NewInt(int64(n))

	if new(big.Int).Mod(pMinus1, nBig).Sign() != 0 {
		return nil, ErrNTTNoRootOfUnity
	}

	// omega = g^((p-1)/n) mod p
	exp := new(big.Int).Div(pMinus1, nBig)
	omega := new(big.Int).Exp(bn254PrimitiveRoot, exp, p)

	// Verify: omega^n == 1 mod p
	check := new(big.Int).Exp(omega, nBig, p)
	if check.Cmp(big.NewInt(1)) != 0 {
		return nil, ErrNTTNoRootOfUnity
	}

	return omega, nil
}

// nttForward performs the forward NTT using the Cooley-Tukey butterfly algorithm.
func nttForward(coeffs []*big.Int, omega *big.Int, p *big.Int) []*big.Int {
	n := len(coeffs)
	if n == 1 {
		result := make([]*big.Int, 1)
		result[0] = new(big.Int).Set(coeffs[0])
		return result
	}

	// Bit-reversal permutation.
	result := make([]*big.Int, n)
	logN := bits.Len(uint(n)) - 1
	for i := range result {
		result[i] = new(big.Int).Set(coeffs[bitReverse(i, logN)])
	}

	// Cooley-Tukey butterfly.
	for size := 2; size <= n; size *= 2 {
		halfSize := size / 2
		step := new(big.Int).Exp(omega, big.NewInt(int64(n/size)), p)
		w := big.NewInt(1)
		for j := 0; j < halfSize; j++ {
			for k := j; k < n; k += size {
				u := new(big.Int).Set(result[k])
				v := new(big.Int).Mul(result[k+halfSize], w)
				v.Mod(v, p)
				result[k] = new(big.Int).Add(u, v)
				result[k].Mod(result[k], p)
				result[k+halfSize] = new(big.Int).Sub(u, v)
				result[k+halfSize].Mod(result[k+halfSize], p)
				if result[k+halfSize].Sign() < 0 {
					result[k+halfSize].Add(result[k+halfSize], p)
				}
			}
			w = new(big.Int).Mul(w, step)
			w.Mod(w, p)
		}
	}

	return result
}

// nttInverse performs the inverse NTT.
func nttInverse(evals []*big.Int, omega *big.Int, p *big.Int) []*big.Int {
	// Compute inverse of omega: omega^(-1) = omega^(p-2) mod p (Fermat's little theorem).
	omegaInv := new(big.Int).Exp(omega, new(big.Int).Sub(p, big.NewInt(2)), p)

	result := nttForward(evals, omegaInv, p)

	// Divide each element by n.
	n := len(evals)
	nBig := big.NewInt(int64(n))
	nInv := new(big.Int).Exp(nBig, new(big.Int).Sub(p, big.NewInt(2)), p)
	for i := range result {
		result[i].Mul(result[i], nInv)
		result[i].Mod(result[i], p)
	}

	return result
}

// bitReverse reverses the lower numBits bits of v.
func bitReverse(v, numBits int) int {
	result := 0
	for i := 0; i < numBits; i++ {
		result = (result << 1) | (v & 1)
		v >>= 1
	}
	return result
}

// PrecompiledContractsIPlus adds NTT precompile for I+ fork.
var PrecompiledContractsIPlus = func() map[types.Address]PrecompiledContract {
	m := make(map[types.Address]PrecompiledContract)
	for addr, c := range PrecompiledContractsGlamsterdan {
		m[addr] = c
	}
	m[types.BytesToAddress([]byte{0x15})] = &nttPrecompile{}
	return m
}()
