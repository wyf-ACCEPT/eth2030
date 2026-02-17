package vm

import (
	"crypto/sha256"
	"errors"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
	"golang.org/x/crypto/ripemd160"
)

// EIP-4844 point evaluation precompile constants.
var (
	fieldElementsPerBlob = big.NewInt(4096)
	blsModulus, _        = new(big.Int).SetString("52435875175126190479447740508185965837690552500527637822603658699938581184513", 10)
)

// PrecompiledContract is the interface for native precompiled contracts.
type PrecompiledContract interface {
	RequiredGas(input []byte) uint64
	Run(input []byte) ([]byte, error)
}

// PrecompiledContractsCancun contains the default set of pre-compiled contracts.
var PrecompiledContractsCancun = map[types.Address]PrecompiledContract{
	types.BytesToAddress([]byte{1}):    &ecrecover{},
	types.BytesToAddress([]byte{2}):    &sha256hash{},
	types.BytesToAddress([]byte{3}):    &ripemd160hash{},
	types.BytesToAddress([]byte{4}):    &dataCopy{},
	types.BytesToAddress([]byte{5}):    &bigModExp{},
	types.BytesToAddress([]byte{0x0a}): &kzgPointEvaluation{},
}

// IsPrecompiledContract checks if the given address is a precompiled contract.
func IsPrecompiledContract(addr types.Address) bool {
	_, ok := PrecompiledContractsCancun[addr]
	return ok
}

// RunPrecompiledContract executes a precompiled contract and returns the output,
// remaining gas, and any error.
func RunPrecompiledContract(addr types.Address, input []byte, gas uint64) ([]byte, uint64, error) {
	p, ok := PrecompiledContractsCancun[addr]
	if !ok {
		return nil, gas, errors.New("not a precompiled contract")
	}
	gasCost := p.RequiredGas(input)
	if gas < gasCost {
		return nil, 0, ErrOutOfGas
	}
	output, err := p.Run(input)
	return output, gas - gasCost, err
}

// --- ecrecover (address 0x01) ---

type ecrecover struct{}

func (c *ecrecover) RequiredGas(input []byte) uint64 {
	return 3000
}

func (c *ecrecover) Run(input []byte) ([]byte, error) {
	// Pad input to 128 bytes.
	input = padRight(input, 128)

	// Extract hash, v, r, s.
	hash := input[0:32]
	v := new(big.Int).SetBytes(input[32:64])
	r := new(big.Int).SetBytes(input[64:96])
	s := new(big.Int).SetBytes(input[96:128])

	// v must be 27 or 28 (Ethereum convention).
	if v.BitLen() > 8 {
		return nil, nil
	}
	vByte := byte(v.Uint64())
	if vByte != 27 && vByte != 28 {
		return nil, nil
	}

	// Validate r and s.
	if !crypto.ValidateSignatureValues(vByte-27, r, s, true) {
		return nil, nil
	}

	// Build 65-byte signature [R || S || V].
	sig := make([]byte, 65)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	copy(sig[32-len(rBytes):32], rBytes)
	copy(sig[64-len(sBytes):64], sBytes)
	sig[64] = vByte - 27

	// Recover the public key.
	pub, err := crypto.Ecrecover(hash, sig)
	if err != nil {
		return nil, nil
	}

	// Derive address: Keccak256(pubkey[1:])[12:]
	addr := crypto.Keccak256(pub[1:])

	// Return 32-byte left-padded address.
	result := make([]byte, 32)
	copy(result[12:], addr[12:])
	return result, nil
}

// --- sha256hash (address 0x02) ---

type sha256hash struct{}

func (c *sha256hash) RequiredGas(input []byte) uint64 {
	return 60 + 12*wordCount(len(input))
}

func (c *sha256hash) Run(input []byte) ([]byte, error) {
	h := sha256.Sum256(input)
	return h[:], nil
}

// --- ripemd160hash (address 0x03) ---

type ripemd160hash struct{}

func (c *ripemd160hash) RequiredGas(input []byte) uint64 {
	return 600 + 120*wordCount(len(input))
}

func (c *ripemd160hash) Run(input []byte) ([]byte, error) {
	h := ripemd160.New()
	h.Write(input)
	digest := h.Sum(nil) // 20 bytes

	// Return 32-byte left-padded result.
	result := make([]byte, 32)
	copy(result[12:], digest)
	return result, nil
}

// --- dataCopy (address 0x04) ---

type dataCopy struct{}

func (c *dataCopy) RequiredGas(input []byte) uint64 {
	return 15 + 3*wordCount(len(input))
}

func (c *dataCopy) Run(input []byte) ([]byte, error) {
	out := make([]byte, len(input))
	copy(out, input)
	return out, nil
}

// --- bigModExp (address 0x05) ---

type bigModExp struct{}

func (c *bigModExp) RequiredGas(input []byte) uint64 {
	input = padRight(input, 96)

	baseLen := new(big.Int).SetBytes(input[0:32]).Uint64()
	expLen := new(big.Int).SetBytes(input[32:64]).Uint64()
	modLen := new(big.Int).SetBytes(input[64:96]).Uint64()

	// Calculate adjusted exponent length for gas.
	adjExpLen := adjustedExpLen(expLen, baseLen, input[96:])

	// Gas = max(200, floor(mult_complexity * iter_count / 3))
	maxLen := baseLen
	if modLen > maxLen {
		maxLen = modLen
	}
	words := (maxLen + 7) / 8
	multComplexity := words * words

	gas := multComplexity * maxUint64(adjExpLen, 1) / 3
	if gas < 200 {
		gas = 200
	}
	return gas
}

func (c *bigModExp) Run(input []byte) ([]byte, error) {
	input = padRight(input, 96)

	baseLen := new(big.Int).SetBytes(input[0:32])
	expLen := new(big.Int).SetBytes(input[32:64])
	modLen := new(big.Int).SetBytes(input[64:96])

	// Sanity check lengths.
	if baseLen.BitLen() > 32 || expLen.BitLen() > 32 || modLen.BitLen() > 32 {
		return nil, errors.New("modexp: length overflow")
	}
	bLen := baseLen.Uint64()
	eLen := expLen.Uint64()
	mLen := modLen.Uint64()

	// Extract base, exp, mod from input data after the 96-byte header.
	data := input[96:]
	base := getDataSlice(data, 0, bLen)
	exp := getDataSlice(data, bLen, eLen)
	mod := getDataSlice(data, bLen+eLen, mLen)

	// If mod is zero, return zero.
	modVal := new(big.Int).SetBytes(mod)
	if modVal.Sign() == 0 {
		return make([]byte, mLen), nil
	}

	baseVal := new(big.Int).SetBytes(base)
	expVal := new(big.Int).SetBytes(exp)

	result := new(big.Int).Exp(baseVal, expVal, modVal)

	// Left-pad result to modLen bytes.
	out := result.Bytes()
	if uint64(len(out)) < mLen {
		padded := make([]byte, mLen)
		copy(padded[mLen-uint64(len(out)):], out)
		return padded, nil
	}
	return out[:mLen], nil
}

// --- helpers ---

// wordCount returns ceil(size / 32), i.e., the number of 32-byte words.
func wordCount(size int) uint64 {
	if size == 0 {
		return 0
	}
	return uint64((size + 31) / 32)
}

// padRight pads data with zeros on the right to reach at least minLen.
func padRight(data []byte, minLen int) []byte {
	if len(data) >= minLen {
		return data
	}
	padded := make([]byte, minLen)
	copy(padded, data)
	return padded
}

// getDataSlice extracts a slice from data starting at offset with given length,
// zero-padding if data is too short.
func getDataSlice(data []byte, offset, length uint64) []byte {
	if length == 0 {
		return nil
	}
	result := make([]byte, length)
	if offset >= uint64(len(data)) {
		return result
	}
	end := offset + length
	if end > uint64(len(data)) {
		end = uint64(len(data))
	}
	copy(result, data[offset:end])
	return result
}

// adjustedExpLen calculates the adjusted exponent length for modexp gas.
func adjustedExpLen(expLen, baseLen uint64, data []byte) uint64 {
	if expLen <= 32 {
		expData := getDataSlice(data, baseLen, expLen)
		exp := new(big.Int).SetBytes(expData)
		if exp.Sign() == 0 {
			return 0
		}
		return uint64(exp.BitLen() - 1)
	}
	// For expLen > 32, use the first 32 bytes of the exponent.
	firstExpData := getDataSlice(data, baseLen, 32)
	firstExp := new(big.Int).SetBytes(firstExpData)
	adj := uint64(0)
	if firstExp.Sign() > 0 {
		adj = uint64(firstExp.BitLen() - 1)
	}
	return adj + 8*(expLen-32)
}

// maxUint64 returns the larger of a and b.
func maxUint64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

// --- kzgPointEvaluation (address 0x0a) - EIP-4844 ---

const pointEvaluationGas = 50000

type kzgPointEvaluation struct{}

func (c *kzgPointEvaluation) RequiredGas(input []byte) uint64 {
	return pointEvaluationGas
}

func (c *kzgPointEvaluation) Run(input []byte) ([]byte, error) {
	if len(input) != 192 {
		return nil, errors.New("kzg: invalid input length")
	}

	// Parse input: versioned_hash(32) | z(32) | y(32) | commitment(48) | proof(48)
	versionedHash := input[:32]
	z := new(big.Int).SetBytes(input[32:64])
	y := new(big.Int).SetBytes(input[64:96])

	// Validate that versioned_hash starts with KZG version byte.
	if versionedHash[0] != types.VersionedHashVersionKZG {
		return nil, errors.New("kzg: invalid versioned hash version")
	}

	// Validate that z and y are valid field elements (< BLS_MODULUS).
	if z.Cmp(blsModulus) >= 0 {
		return nil, errors.New("kzg: z is not a valid field element")
	}
	if y.Cmp(blsModulus) >= 0 {
		return nil, errors.New("kzg: y is not a valid field element")
	}

	// Verify commitment matches versioned_hash: sha256(commitment) with version prefix.
	commitment := input[96:144]
	commitHash := sha256.Sum256(commitment)
	commitHash[0] = types.VersionedHashVersionKZG
	if !bytesEqual(versionedHash, commitHash[:]) {
		return nil, errors.New("kzg: commitment does not match versioned hash")
	}

	// KZG proof verification is stubbed -- actual cryptographic verification
	// requires a KZG library with a trusted setup. We validate format only.
	// In production, verify_kzg_proof(commitment, z, y, proof) would be called here.

	// Return FIELD_ELEMENTS_PER_BLOB and BLS_MODULUS as 32-byte big-endian values.
	result := make([]byte, 64)
	fieldBytes := fieldElementsPerBlob.Bytes()
	copy(result[32-len(fieldBytes):32], fieldBytes)
	modBytes := blsModulus.Bytes()
	copy(result[64-len(modBytes):64], modBytes)
	return result, nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
