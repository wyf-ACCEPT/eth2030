package vm

import (
	"errors"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// NII (Number-Theoretic) precompiles for efficient field arithmetic.
// These precompiles provide accelerated modular arithmetic operations
// needed for proof verification and zkVM execution (J+ upgrade).

// NII precompile addresses in the 0x02xx range.
var (
	NiiModExpAddr      = types.BytesToAddress([]byte{0x02, 0x01})
	NiiFieldMulAddr    = types.BytesToAddress([]byte{0x02, 0x02})
	NiiFieldInvAddr    = types.BytesToAddress([]byte{0x02, 0x03})
	NiiBatchVerifyAddr = types.BytesToAddress([]byte{0x02, 0x04})
)

// NII precompile errors.
var (
	ErrNiiInputTooShort   = errors.New("nii: input too short")
	ErrNiiZeroModulus     = errors.New("nii: zero modulus")
	ErrNiiZeroFieldSize   = errors.New("nii: zero field size")
	ErrNiiFieldSizeTooLarge = errors.New("nii: field size exceeds 256 bytes")
	ErrNiiNoInverse       = errors.New("nii: no modular inverse exists")
	ErrNiiInvalidSigCount = errors.New("nii: invalid signature count")
)

// Maximum field size in bytes (2048 bits).
const niiMaxFieldSize = 256

// --- NiiModExpPrecompile (address 0x0201) ---
// Modular exponentiation for arbitrary field sizes: base^exp mod modulus.
// Input: baseLen(32) || expLen(32) || modLen(32) || base || exp || mod
// Output: result (modLen bytes)

type NiiModExpPrecompile struct{}

func (c *NiiModExpPrecompile) RequiredGas(input []byte) uint64 {
	input = padRight(input, 96)

	baseLen := new(big.Int).SetBytes(input[0:32]).Uint64()
	expLen := new(big.Int).SetBytes(input[32:64]).Uint64()
	modLen := new(big.Int).SetBytes(input[64:96]).Uint64()

	// Gas model based on operand sizes.
	maxLen := baseLen
	if modLen > maxLen {
		maxLen = modLen
	}
	if maxLen == 0 {
		return 200
	}

	words := (maxLen + 7) / 8
	multComplexity := words * words

	adjExpLen := uint64(1)
	if expLen > 0 {
		// Use first 32 bytes of exponent to estimate bit length.
		data := input[96:]
		expData := getDataSlice(data, baseLen, min64(expLen, 32))
		exp := new(big.Int).SetBytes(expData)
		if exp.Sign() > 0 {
			adjExpLen = uint64(exp.BitLen())
		}
		if expLen > 32 {
			adjExpLen += 8 * (expLen - 32)
		}
	}

	gas := multComplexity * adjExpLen / 3
	if gas < 200 {
		gas = 200
	}
	return gas
}

func (c *NiiModExpPrecompile) Run(input []byte) ([]byte, error) {
	input = padRight(input, 96)

	baseLenBig := new(big.Int).SetBytes(input[0:32])
	expLenBig := new(big.Int).SetBytes(input[32:64])
	modLenBig := new(big.Int).SetBytes(input[64:96])

	// Sanity check lengths.
	if baseLenBig.BitLen() > 32 || expLenBig.BitLen() > 32 || modLenBig.BitLen() > 32 {
		return nil, errors.New("nii modexp: length overflow")
	}
	bLen := baseLenBig.Uint64()
	eLen := expLenBig.Uint64()
	mLen := modLenBig.Uint64()

	data := input[96:]
	base := getDataSlice(data, 0, bLen)
	exp := getDataSlice(data, bLen, eLen)
	mod := getDataSlice(data, bLen+eLen, mLen)

	modVal := new(big.Int).SetBytes(mod)
	if modVal.Sign() == 0 {
		return make([]byte, mLen), nil
	}

	baseVal := new(big.Int).SetBytes(base)
	expVal := new(big.Int).SetBytes(exp)

	result := new(big.Int).Exp(baseVal, expVal, modVal)

	out := result.Bytes()
	if uint64(len(out)) < mLen {
		padded := make([]byte, mLen)
		copy(padded[mLen-uint64(len(out)):], out)
		return padded, nil
	}
	return out[:mLen], nil
}

// --- NiiFieldMulPrecompile (address 0x0202) ---
// Field multiplication: (a * b) mod modulus.
// Input: fieldSize(32) || a(fieldSize) || b(fieldSize) || modulus(fieldSize)
// Output: product mod modulus (fieldSize bytes)

type NiiFieldMulPrecompile struct{}

func (c *NiiFieldMulPrecompile) RequiredGas(input []byte) uint64 {
	return 100
}

func (c *NiiFieldMulPrecompile) Run(input []byte) ([]byte, error) {
	if len(input) < 32 {
		return nil, ErrNiiInputTooShort
	}

	fieldSize := new(big.Int).SetBytes(input[0:32])
	if fieldSize.Sign() == 0 {
		return nil, ErrNiiZeroFieldSize
	}
	if fieldSize.BitLen() > 32 || fieldSize.Uint64() > niiMaxFieldSize {
		return nil, ErrNiiFieldSizeTooLarge
	}
	fs := fieldSize.Uint64()

	// Need fieldSize(32) + a(fs) + b(fs) + modulus(fs) = 32 + 3*fs bytes.
	expectedLen := 32 + 3*fs
	input = padRight(input, int(expectedLen))

	a := new(big.Int).SetBytes(input[32 : 32+fs])
	b := new(big.Int).SetBytes(input[32+fs : 32+2*fs])
	modulus := new(big.Int).SetBytes(input[32+2*fs : 32+3*fs])

	if modulus.Sign() == 0 {
		return nil, ErrNiiZeroModulus
	}

	// product = (a * b) mod modulus
	product := new(big.Int).Mul(a, b)
	product.Mod(product, modulus)

	// Left-pad result to fieldSize bytes.
	out := product.Bytes()
	result := make([]byte, fs)
	if len(out) > 0 {
		copy(result[fs-uint64(len(out)):], out)
	}
	return result, nil
}

// --- NiiFieldInvPrecompile (address 0x0203) ---
// Modular inverse: value^(-1) mod modulus (using extended Euclidean algorithm).
// Input: fieldSize(32) || value(fieldSize) || modulus(fieldSize)
// Output: inverse mod modulus (fieldSize bytes)

type NiiFieldInvPrecompile struct{}

func (c *NiiFieldInvPrecompile) RequiredGas(input []byte) uint64 {
	return 200
}

func (c *NiiFieldInvPrecompile) Run(input []byte) ([]byte, error) {
	if len(input) < 32 {
		return nil, ErrNiiInputTooShort
	}

	fieldSize := new(big.Int).SetBytes(input[0:32])
	if fieldSize.Sign() == 0 {
		return nil, ErrNiiZeroFieldSize
	}
	if fieldSize.BitLen() > 32 || fieldSize.Uint64() > niiMaxFieldSize {
		return nil, ErrNiiFieldSizeTooLarge
	}
	fs := fieldSize.Uint64()

	// Need fieldSize(32) + value(fs) + modulus(fs) = 32 + 2*fs bytes.
	expectedLen := 32 + 2*fs
	input = padRight(input, int(expectedLen))

	value := new(big.Int).SetBytes(input[32 : 32+fs])
	modulus := new(big.Int).SetBytes(input[32+fs : 32+2*fs])

	if modulus.Sign() == 0 {
		return nil, ErrNiiZeroModulus
	}

	// Check if value is zero (no inverse).
	if value.Sign() == 0 {
		return nil, ErrNiiNoInverse
	}

	// Compute modular inverse using ModInverse (extended Euclidean).
	inv := new(big.Int).ModInverse(value, modulus)
	if inv == nil {
		return nil, ErrNiiNoInverse
	}

	// Left-pad result to fieldSize bytes.
	out := inv.Bytes()
	result := make([]byte, fs)
	if len(out) > 0 {
		copy(result[fs-uint64(len(out)):], out)
	}
	return result, nil
}

// --- NiiBatchVerifyPrecompile (address 0x0204) ---
// Batch ECDSA signature verification.
// Input: count(32) || (hash(32) || v(1) || r(32) || s(32)) * count
// Output: 0x01 if all signatures are valid, 0x00 if any is invalid.

const niiBatchVerifySigSize = 32 + 1 + 32 + 32 // hash + v + r + s = 97 bytes

type NiiBatchVerifyPrecompile struct{}

func (c *NiiBatchVerifyPrecompile) RequiredGas(input []byte) uint64 {
	if len(input) < 32 {
		return 0
	}
	count := new(big.Int).SetBytes(input[0:32])
	if count.BitLen() > 32 {
		return 0
	}
	return 3000 * count.Uint64()
}

func (c *NiiBatchVerifyPrecompile) Run(input []byte) ([]byte, error) {
	if len(input) < 32 {
		return nil, ErrNiiInputTooShort
	}

	countBig := new(big.Int).SetBytes(input[0:32])
	if countBig.BitLen() > 32 {
		return nil, ErrNiiInvalidSigCount
	}
	count := countBig.Uint64()
	if count == 0 {
		return nil, ErrNiiInvalidSigCount
	}

	// Verify input length: 32 + count * 97 bytes.
	expectedLen := 32 + count*niiBatchVerifySigSize
	if uint64(len(input)) < expectedLen {
		return nil, ErrNiiInputTooShort
	}

	// Verify each signature.
	for i := uint64(0); i < count; i++ {
		offset := 32 + i*niiBatchVerifySigSize
		hash := input[offset : offset+32]
		v := input[offset+32]
		r := input[offset+33 : offset+65]
		s := input[offset+65 : offset+97]

		// Build 65-byte signature [R || S || V] for Ecrecover.
		// v must be 27 or 28 (Ethereum convention), convert to 0 or 1.
		var recoveryID byte
		if v == 27 {
			recoveryID = 0
		} else if v == 28 {
			recoveryID = 1
		} else if v <= 1 {
			// Also accept raw recovery ID (0 or 1).
			recoveryID = v
		} else {
			// Invalid v value.
			return []byte{0x00}, nil
		}

		sig := make([]byte, 65)
		copy(sig[0:32], r)
		copy(sig[32:64], s)
		sig[64] = recoveryID

		// Attempt recovery. If it fails, the signature is invalid.
		_, err := crypto.Ecrecover(hash, sig)
		if err != nil {
			return []byte{0x00}, nil
		}
	}

	return []byte{0x01}, nil
}

// min64 returns the smaller of a and b.
func min64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

// PrecompiledContractsJPlus adds NII precompiles for J+ fork.
var PrecompiledContractsJPlus = func() map[types.Address]PrecompiledContract {
	m := make(map[types.Address]PrecompiledContract)
	for addr, c := range PrecompiledContractsIPlus {
		m[addr] = c
	}
	m[NiiModExpAddr] = &NiiModExpPrecompile{}
	m[NiiFieldMulAddr] = &NiiFieldMulPrecompile{}
	m[NiiFieldInvAddr] = &NiiFieldInvPrecompile{}
	m[NiiBatchVerifyAddr] = &NiiBatchVerifyPrecompile{}
	return m
}()
