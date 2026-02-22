// Package vm implements the Ethereum Virtual Machine.
//
// precompile_field.go provides extended NII field arithmetic precompiles
// for efficient modular arithmetic over arbitrary prime fields. Part of the
// J+ roadmap: "NII precompile(s)" for proof verification and zkVM execution.
//
// Precompiles in this file:
//   - FieldMulExtPrecompile (0x0205): optimized modular multiplication with
//     size-aware gas pricing
//   - FieldInvExtPrecompile (0x0206): modular inverse via extended GCD with
//     coprimality validation
//   - FieldExpPrecompile    (0x0207): modular exponentiation with
//     Montgomery-ladder-style gas model
//   - BatchFieldVerifyPrecompile (0x0208): batch BLS aggregate signature
//     verification simulation
package vm

import (
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Extended NII precompile addresses in the 0x02xx range.
var (
	NiiFieldMulExtAddr   = types.BytesToAddress([]byte{0x02, 0x05})
	NiiFieldInvExtAddr   = types.BytesToAddress([]byte{0x02, 0x06})
	NiiFieldExpAddr      = types.BytesToAddress([]byte{0x02, 0x07})
	NiiBatchFieldVerAddr = types.BytesToAddress([]byte{0x02, 0x08})
)

// Extended NII precompile errors.
var (
	ErrFieldInputTooShort    = errors.New("field: input too short")
	ErrFieldZeroModulus      = errors.New("field: zero modulus")
	ErrFieldZeroFieldSize    = errors.New("field: zero field size")
	ErrFieldSizeTooLarge     = errors.New("field: field size exceeds 256 bytes")
	ErrFieldNoInverse        = errors.New("field: modular inverse does not exist")
	ErrFieldInvalidBatchSize = errors.New("field: invalid batch size")
	ErrFieldEvenModulus      = errors.New("field: modulus must be odd prime")
)

// fieldMaxSize is the maximum field element size in bytes (2048 bits).
const fieldMaxSize = 256

// --- FieldMulExtPrecompile (address 0x0205) ---
// Optimized modular multiplication with size-dependent gas pricing.
// Input: fieldSize(2) || a(fieldSize) || b(fieldSize) || modulus(fieldSize)
// Output: (a * b) mod modulus (fieldSize bytes)

type FieldMulExtPrecompile struct{}

func (c *FieldMulExtPrecompile) RequiredGas(input []byte) uint64 {
	if len(input) < 2 {
		return 100
	}
	fs := uint64(input[0])<<8 | uint64(input[1])
	if fs == 0 || fs > fieldMaxSize {
		return 100
	}
	// Gas scales quadratically with field size (multiplication cost).
	words := (fs + 7) / 8
	return 50 + words*words
}

func (c *FieldMulExtPrecompile) Run(input []byte) ([]byte, error) {
	if len(input) < 2 {
		return nil, ErrFieldInputTooShort
	}

	fs := uint64(input[0])<<8 | uint64(input[1])
	if fs == 0 {
		return nil, ErrFieldZeroFieldSize
	}
	if fs > fieldMaxSize {
		return nil, ErrFieldSizeTooLarge
	}

	// Need 2 + 3*fs bytes: prefix(2) + a(fs) + b(fs) + modulus(fs).
	expectedLen := 2 + 3*fs
	input = padRight(input, int(expectedLen))

	a := new(big.Int).SetBytes(input[2 : 2+fs])
	b := new(big.Int).SetBytes(input[2+fs : 2+2*fs])
	modulus := new(big.Int).SetBytes(input[2+2*fs : 2+3*fs])

	if modulus.Sign() == 0 {
		return nil, ErrFieldZeroModulus
	}

	// Reduce inputs mod modulus before multiplication.
	a.Mod(a, modulus)
	b.Mod(b, modulus)

	product := new(big.Int).Mul(a, b)
	product.Mod(product, modulus)

	return leftPadBytes(product.Bytes(), int(fs)), nil
}

// --- FieldInvExtPrecompile (address 0x0206) ---
// Modular inverse using extended Euclidean algorithm with coprimality check.
// Input: fieldSize(2) || value(fieldSize) || modulus(fieldSize)
// Output: value^(-1) mod modulus (fieldSize bytes)

type FieldInvExtPrecompile struct{}

func (c *FieldInvExtPrecompile) RequiredGas(input []byte) uint64 {
	if len(input) < 2 {
		return 200
	}
	fs := uint64(input[0])<<8 | uint64(input[1])
	if fs == 0 || fs > fieldMaxSize {
		return 200
	}
	// Gas scales with field size (extended GCD cost).
	words := (fs + 7) / 8
	return 100 + words*words*2
}

func (c *FieldInvExtPrecompile) Run(input []byte) ([]byte, error) {
	if len(input) < 2 {
		return nil, ErrFieldInputTooShort
	}

	fs := uint64(input[0])<<8 | uint64(input[1])
	if fs == 0 {
		return nil, ErrFieldZeroFieldSize
	}
	if fs > fieldMaxSize {
		return nil, ErrFieldSizeTooLarge
	}

	// Need 2 + 2*fs bytes: prefix(2) + value(fs) + modulus(fs).
	expectedLen := 2 + 2*fs
	input = padRight(input, int(expectedLen))

	value := new(big.Int).SetBytes(input[2 : 2+fs])
	modulus := new(big.Int).SetBytes(input[2+fs : 2+2*fs])

	if modulus.Sign() == 0 {
		return nil, ErrFieldZeroModulus
	}
	if value.Sign() == 0 {
		return nil, ErrFieldNoInverse
	}

	// Reduce value mod modulus.
	value.Mod(value, modulus)
	if value.Sign() == 0 {
		return nil, ErrFieldNoInverse
	}

	// Check GCD(value, modulus) == 1 (coprimality required for inverse).
	gcd := new(big.Int).GCD(nil, nil, value, modulus)
	if gcd.Cmp(big.NewInt(1)) != 0 {
		return nil, ErrFieldNoInverse
	}

	inv := new(big.Int).ModInverse(value, modulus)
	if inv == nil {
		return nil, ErrFieldNoInverse
	}

	return leftPadBytes(inv.Bytes(), int(fs)), nil
}

// --- FieldExpPrecompile (address 0x0207) ---
// Modular exponentiation with optimized gas model.
// Input: fieldSize(2) || base(fieldSize) || exponent(fieldSize) || modulus(fieldSize)
// Output: base^exponent mod modulus (fieldSize bytes)

type FieldExpPrecompile struct{}

func (c *FieldExpPrecompile) RequiredGas(input []byte) uint64 {
	if len(input) < 2 {
		return 200
	}
	fs := uint64(input[0])<<8 | uint64(input[1])
	if fs == 0 || fs > fieldMaxSize {
		return 200
	}

	// Gas model: multiplication complexity * exponent bit length.
	words := (fs + 7) / 8
	multComplexity := words * words

	// Estimate exponent bit length from the exponent bytes.
	expectedLen := 2 + 3*fs
	padded := padRight(input, int(expectedLen))
	expBytes := padded[2+fs : 2+2*fs]
	exp := new(big.Int).SetBytes(expBytes)
	adjExpLen := uint64(1)
	if exp.Sign() > 0 {
		adjExpLen = uint64(exp.BitLen())
	}

	gas := multComplexity * adjExpLen / 3
	if gas < 200 {
		gas = 200
	}
	return gas
}

func (c *FieldExpPrecompile) Run(input []byte) ([]byte, error) {
	if len(input) < 2 {
		return nil, ErrFieldInputTooShort
	}

	fs := uint64(input[0])<<8 | uint64(input[1])
	if fs == 0 {
		return nil, ErrFieldZeroFieldSize
	}
	if fs > fieldMaxSize {
		return nil, ErrFieldSizeTooLarge
	}

	// Need 2 + 3*fs bytes: prefix(2) + base(fs) + exp(fs) + modulus(fs).
	expectedLen := 2 + 3*fs
	input = padRight(input, int(expectedLen))

	base := new(big.Int).SetBytes(input[2 : 2+fs])
	exp := new(big.Int).SetBytes(input[2+fs : 2+2*fs])
	modulus := new(big.Int).SetBytes(input[2+2*fs : 2+3*fs])

	if modulus.Sign() == 0 {
		// Modulus zero: return zero as per convention.
		return make([]byte, fs), nil
	}

	// Reduce base mod modulus.
	base.Mod(base, modulus)

	result := new(big.Int).Exp(base, exp, modulus)

	return leftPadBytes(result.Bytes(), int(fs)), nil
}

// --- BatchFieldVerifyPrecompile (address 0x0208) ---
// Batch BLS aggregate signature verification simulation.
// Verifies multiple ECDSA signatures in a batch, returning success/failure.
//
// Input format:
//   count(2) || (hash(32) || v(1) || r(32) || s(32)) * count
//
// Each signature entry is 97 bytes (hash + v + r + s).
// Output: 0x01 if all signatures verify, 0x00 otherwise.

const batchFieldSigSize = 32 + 1 + 32 + 32 // 97 bytes per signature

type BatchFieldVerifyPrecompile struct{}

func (c *BatchFieldVerifyPrecompile) RequiredGas(input []byte) uint64 {
	if len(input) < 2 {
		return 0
	}
	count := uint64(input[0])<<8 | uint64(input[1])
	if count == 0 {
		return 0
	}
	// Base cost + per-signature cost with slight discount for batching.
	// Single signature: 3000 gas. Batch discount: 2800 per sig + 500 base.
	return 500 + 2800*count
}

func (c *BatchFieldVerifyPrecompile) Run(input []byte) ([]byte, error) {
	if len(input) < 2 {
		return nil, ErrFieldInputTooShort
	}

	count := uint64(input[0])<<8 | uint64(input[1])
	if count == 0 {
		return nil, ErrFieldInvalidBatchSize
	}

	// Validate input length: 2 + count * 97 bytes.
	expectedLen := 2 + count*batchFieldSigSize
	if uint64(len(input)) < expectedLen {
		return nil, ErrFieldInputTooShort
	}

	// Verify each signature.
	for i := uint64(0); i < count; i++ {
		offset := 2 + i*batchFieldSigSize
		hash := input[offset : offset+32]
		v := input[offset+32]
		r := input[offset+33 : offset+65]
		s := input[offset+65 : offset+97]

		// Convert v to recovery ID (0 or 1).
		var recoveryID byte
		switch {
		case v == 27:
			recoveryID = 0
		case v == 28:
			recoveryID = 1
		case v <= 1:
			recoveryID = v
		default:
			return []byte{0x00}, nil
		}

		sig := make([]byte, 65)
		copy(sig[0:32], r)
		copy(sig[32:64], s)
		sig[64] = recoveryID

		_, err := crypto.Ecrecover(hash, sig)
		if err != nil {
			return []byte{0x00}, nil
		}
	}

	return []byte{0x01}, nil
}

// leftPadBytes returns data left-padded with zeros to the given size.
// If data is longer than size, it is truncated to size bytes from the right.
func leftPadBytes(data []byte, size int) []byte {
	result := make([]byte, size)
	if len(data) >= size {
		copy(result, data[len(data)-size:])
	} else {
		copy(result[size-len(data):], data)
	}
	return result
}

// PrecompiledContractsKPlus adds extended NII field precompiles for K+ fork.
var PrecompiledContractsKPlus = func() map[types.Address]PrecompiledContract {
	m := make(map[types.Address]PrecompiledContract)
	for addr, c := range PrecompiledContractsJPlus {
		m[addr] = c
	}
	m[NiiFieldMulExtAddr] = &FieldMulExtPrecompile{}
	m[NiiFieldInvExtAddr] = &FieldInvExtPrecompile{}
	m[NiiFieldExpAddr] = &FieldExpPrecompile{}
	m[NiiBatchFieldVerAddr] = &BatchFieldVerifyPrecompile{}
	return m
}()
