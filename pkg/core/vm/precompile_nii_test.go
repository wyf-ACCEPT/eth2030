package vm

import (
	"crypto/ecdsa"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// --- NiiModExpPrecompile tests ---

func TestNiiModExpSimple(t *testing.T) {
	// 2^10 mod 1000 = 1024 mod 1000 = 24
	p := &NiiModExpPrecompile{}

	base := big.NewInt(2)
	exp := big.NewInt(10)
	mod := big.NewInt(1000)

	input := buildNiiModExpInput(base, exp, mod)
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := new(big.Int).SetBytes(out)
	if result.Cmp(big.NewInt(24)) != 0 {
		t.Errorf("2^10 mod 1000: got %v, want 24", result)
	}
}

func TestNiiModExpLargeValues(t *testing.T) {
	// Test with 256-bit values.
	p := &NiiModExpPrecompile{}

	// base = 2^255, exp = 3, mod = 2^256 - 189 (a prime)
	base := new(big.Int).Lsh(big.NewInt(1), 255)
	exp := big.NewInt(3)
	mod, _ := new(big.Int).SetString("115792089237316195423570985008687907853269984665640564039457584007913129639747", 10)

	input := buildNiiModExpInput(base, exp, mod)
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := new(big.Int).SetBytes(out)

	// Verify: result == base^exp mod mod.
	expected := new(big.Int).Exp(base, exp, mod)
	if result.Cmp(expected) != 0 {
		t.Errorf("large modexp: got %v, want %v", result, expected)
	}
}

func TestNiiModExpGas(t *testing.T) {
	p := &NiiModExpPrecompile{}

	// Small operands should still cost at least 200 gas.
	input := buildNiiModExpInput(big.NewInt(2), big.NewInt(10), big.NewInt(1000))
	gas := p.RequiredGas(input)
	if gas < 200 {
		t.Errorf("gas should be at least 200, got %d", gas)
	}

	// Larger operands should cost more gas.
	// Use a 256-bit base, a 256-bit exponent, and a 256-bit modulus.
	largeMod, _ := new(big.Int).SetString("115792089237316195423570985008687907853269984665640564039457584007913129639747", 10)
	largeBase := new(big.Int).Sub(largeMod, big.NewInt(1))
	largeExp := new(big.Int).Sub(largeMod, big.NewInt(2))
	largeInput := buildNiiModExpInput(largeBase, largeExp, largeMod)
	largeGas := p.RequiredGas(largeInput)
	if largeGas <= gas {
		t.Errorf("larger operands should cost more gas: small=%d, large=%d", gas, largeGas)
	}
}

// --- NiiFieldMulPrecompile tests ---

func TestNiiFieldMul(t *testing.T) {
	p := &NiiFieldMulPrecompile{}

	// 7 * 5 mod 17 = 35 mod 17 = 1
	fieldSize := uint64(32)
	a := big.NewInt(7)
	b := big.NewInt(5)
	mod := big.NewInt(17)

	input := buildNiiFieldMulInput(fieldSize, a, b, mod)
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := new(big.Int).SetBytes(out)
	if result.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("7 * 5 mod 17: got %v, want 1", result)
	}
}

func TestNiiFieldMulOverflow(t *testing.T) {
	// Test multiplication with values that wrap around the modulus.
	p := &NiiFieldMulPrecompile{}

	fieldSize := uint64(32)
	// (2^128 + 1) * 3 mod (2^128 + 2) should wrap.
	mod := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(2))
	a := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1))
	b := big.NewInt(3)

	input := buildNiiFieldMulInput(fieldSize, a, b, mod)
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := new(big.Int).SetBytes(out)
	expected := new(big.Int).Mul(a, b)
	expected.Mod(expected, mod)

	if result.Cmp(expected) != 0 {
		t.Errorf("field mul overflow: got %v, want %v", result, expected)
	}
}

func TestNiiFieldMulGas(t *testing.T) {
	p := &NiiFieldMulPrecompile{}
	gas := p.RequiredGas(nil)
	if gas != 100 {
		t.Errorf("expected fixed gas 100, got %d", gas)
	}
}

// --- NiiFieldInvPrecompile tests ---

func TestNiiFieldInv(t *testing.T) {
	// Compute modular inverse of 3 mod 7: 3^-1 mod 7 = 5 (since 3*5 = 15 = 1 mod 7).
	p := &NiiFieldInvPrecompile{}

	fieldSize := uint64(32)
	value := big.NewInt(3)
	mod := big.NewInt(7)

	input := buildNiiFieldInvInput(fieldSize, value, mod)
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	inv := new(big.Int).SetBytes(out)

	// Verify: value * inv mod mod == 1.
	check := new(big.Int).Mul(value, inv)
	check.Mod(check, mod)
	if check.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("3 * 3^-1 mod 7 = %v, want 1 (inv=%v)", check, inv)
	}
}

func TestNiiFieldInvLarger(t *testing.T) {
	// Test with a larger prime.
	p := &NiiFieldInvPrecompile{}

	fieldSize := uint64(32)
	prime := big.NewInt(104729) // a prime
	value := big.NewInt(42)

	input := buildNiiFieldInvInput(fieldSize, value, prime)
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	inv := new(big.Int).SetBytes(out)

	// Verify: value * inv mod prime == 1.
	check := new(big.Int).Mul(value, inv)
	check.Mod(check, prime)
	if check.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("42 * 42^-1 mod 104729 = %v, want 1", check)
	}
}

func TestNiiFieldInvZero(t *testing.T) {
	p := &NiiFieldInvPrecompile{}

	fieldSize := uint64(32)
	value := big.NewInt(0)
	mod := big.NewInt(7)

	input := buildNiiFieldInvInput(fieldSize, value, mod)
	_, err := p.Run(input)
	if err != ErrNiiNoInverse {
		t.Errorf("expected ErrNiiNoInverse for zero value, got %v", err)
	}
}

func TestNiiFieldInvGas(t *testing.T) {
	p := &NiiFieldInvPrecompile{}
	gas := p.RequiredGas(nil)
	if gas != 200 {
		t.Errorf("expected fixed gas 200, got %d", gas)
	}
}

// --- NiiBatchVerifyPrecompile tests ---

func TestNiiBatchVerifySingle(t *testing.T) {
	p := &NiiBatchVerifyPrecompile{}

	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	hash := crypto.Keccak256([]byte("test message"))
	sig, err := crypto.Sign(hash, key)
	if err != nil {
		t.Fatalf("failed to sign: %v", err)
	}

	input := buildNiiBatchVerifyInput([]*batchSigEntry{
		{hash: hash, v: sig[64], r: sig[0:32], s: sig[32:64]},
	})

	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 || out[0] != 0x01 {
		t.Errorf("expected 0x01 for valid signature, got %x", out)
	}
}

func TestNiiBatchVerifyMultiple(t *testing.T) {
	p := &NiiBatchVerifyPrecompile{}

	entries := make([]*batchSigEntry, 3)
	for i := 0; i < 3; i++ {
		key, err := crypto.GenerateKey()
		if err != nil {
			t.Fatalf("failed to generate key %d: %v", i, err)
		}

		msg := []byte("message " + string(rune('A'+i)))
		hash := crypto.Keccak256(msg)
		sig, err := crypto.Sign(hash, key)
		if err != nil {
			t.Fatalf("failed to sign %d: %v", i, err)
		}

		entries[i] = &batchSigEntry{
			hash: hash,
			v:    sig[64],
			r:    sig[0:32],
			s:    sig[32:64],
		}
	}

	input := buildNiiBatchVerifyInput(entries)
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 || out[0] != 0x01 {
		t.Errorf("expected 0x01 for all valid signatures, got %x", out)
	}
}

func TestNiiBatchVerifyInvalid(t *testing.T) {
	p := &NiiBatchVerifyPrecompile{}

	// Create one valid and one invalid signature.
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	hash1 := crypto.Keccak256([]byte("valid message"))
	sig1, err := crypto.Sign(hash1, key)
	if err != nil {
		t.Fatalf("failed to sign: %v", err)
	}

	// Invalid: use wrong hash with the signature.
	hash2 := crypto.Keccak256([]byte("wrong message"))

	entries := []*batchSigEntry{
		{hash: hash1, v: sig1[64], r: sig1[0:32], s: sig1[32:64]},
		// This signature was made for hash1 but we say it's for hash2.
		// Ecrecover will succeed (it recovers *some* pubkey) but that's fine;
		// the batch verify just checks recoverability. Let's make a truly invalid sig.
		{hash: hash2, v: 0xFF, r: sig1[0:32], s: sig1[32:64]}, // invalid v
	}

	input := buildNiiBatchVerifyInput(entries)
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 || out[0] != 0x00 {
		t.Errorf("expected 0x00 for invalid batch, got %x", out)
	}
}

func TestNiiBatchVerifyGas(t *testing.T) {
	p := &NiiBatchVerifyPrecompile{}

	// 5 signatures should cost 5 * 3000 = 15000.
	count := big.NewInt(5)
	input := make([]byte, 32)
	countBytes := count.Bytes()
	copy(input[32-len(countBytes):], countBytes)

	gas := p.RequiredGas(input)
	if gas != 15000 {
		t.Errorf("expected gas 15000 for 5 sigs, got %d", gas)
	}
}

func TestNiiBatchVerifyInputTooShort(t *testing.T) {
	p := &NiiBatchVerifyPrecompile{}

	_, err := p.Run(nil)
	if err != ErrNiiInputTooShort {
		t.Errorf("expected ErrNiiInputTooShort for nil input, got %v", err)
	}

	_, err = p.Run([]byte{0x01})
	if err != ErrNiiInputTooShort {
		t.Errorf("expected ErrNiiInputTooShort for short input, got %v", err)
	}
}

func TestNiiBatchVerifyZeroCount(t *testing.T) {
	p := &NiiBatchVerifyPrecompile{}

	input := make([]byte, 32) // count = 0
	_, err := p.Run(input)
	if err != ErrNiiInvalidSigCount {
		t.Errorf("expected ErrNiiInvalidSigCount for zero count, got %v", err)
	}
}

// --- Precompile registration tests ---

func TestNiiPrecompilesRegisteredJPlus(t *testing.T) {
	addrs := []types.Address{NiiModExpAddr, NiiFieldMulAddr, NiiFieldInvAddr, NiiBatchVerifyAddr}
	for _, addr := range addrs {
		if _, ok := PrecompiledContractsJPlus[addr]; !ok {
			t.Errorf("NII precompile not found at address %s in J+ fork", addr.Hex())
		}
	}
}

func TestNiiJPlusIncludesIPlusPrecompiles(t *testing.T) {
	// J+ should include all I+ precompiles.
	for addr := range PrecompiledContractsIPlus {
		if _, ok := PrecompiledContractsJPlus[addr]; !ok {
			t.Errorf("I+ precompile at %s not found in J+ fork", addr.Hex())
		}
	}
}

// --- Test helpers ---

// buildNiiModExpInput constructs the input for the NII modexp precompile.
func buildNiiModExpInput(base, exp, mod *big.Int) []byte {
	baseBytes := base.Bytes()
	expBytes := exp.Bytes()
	modBytes := mod.Bytes()

	bLen := len(baseBytes)
	eLen := len(expBytes)
	mLen := len(modBytes)

	input := make([]byte, 96+bLen+eLen+mLen)

	// Encode lengths as 32-byte big-endian.
	bLenBig := big.NewInt(int64(bLen))
	eLenBig := big.NewInt(int64(eLen))
	mLenBig := big.NewInt(int64(mLen))

	bLenBytes := bLenBig.Bytes()
	eLenBytes := eLenBig.Bytes()
	mLenBytes := mLenBig.Bytes()

	copy(input[32-len(bLenBytes):32], bLenBytes)
	copy(input[64-len(eLenBytes):64], eLenBytes)
	copy(input[96-len(mLenBytes):96], mLenBytes)
	copy(input[96:96+bLen], baseBytes)
	copy(input[96+bLen:96+bLen+eLen], expBytes)
	copy(input[96+bLen+eLen:], modBytes)

	return input
}

// buildNiiFieldMulInput constructs the input for the NII field mul precompile.
func buildNiiFieldMulInput(fieldSize uint64, a, b, mod *big.Int) []byte {
	fs := int(fieldSize)
	input := make([]byte, 32+3*fs)

	fsBig := big.NewInt(int64(fieldSize))
	fsBytes := fsBig.Bytes()
	copy(input[32-len(fsBytes):32], fsBytes)

	aBytes := a.Bytes()
	bBytes := b.Bytes()
	modBytes := mod.Bytes()

	copy(input[32+fs-len(aBytes):32+fs], aBytes)
	copy(input[32+2*fs-len(bBytes):32+2*fs], bBytes)
	copy(input[32+3*fs-len(modBytes):32+3*fs], modBytes)

	return input
}

// buildNiiFieldInvInput constructs the input for the NII field inv precompile.
func buildNiiFieldInvInput(fieldSize uint64, value, mod *big.Int) []byte {
	fs := int(fieldSize)
	input := make([]byte, 32+2*fs)

	fsBig := big.NewInt(int64(fieldSize))
	fsBytes := fsBig.Bytes()
	copy(input[32-len(fsBytes):32], fsBytes)

	valBytes := value.Bytes()
	modBytes := mod.Bytes()

	copy(input[32+fs-len(valBytes):32+fs], valBytes)
	copy(input[32+2*fs-len(modBytes):32+2*fs], modBytes)

	return input
}

// batchSigEntry holds a single signature for batch verification.
type batchSigEntry struct {
	hash []byte
	v    byte
	r    []byte
	s    []byte
}

// buildNiiBatchVerifyInput constructs the input for the NII batch verify precompile.
func buildNiiBatchVerifyInput(entries []*batchSigEntry) []byte {
	count := len(entries)
	input := make([]byte, 32+count*niiBatchVerifySigSize)

	countBig := big.NewInt(int64(count))
	countBytes := countBig.Bytes()
	copy(input[32-len(countBytes):32], countBytes)

	for i, e := range entries {
		offset := 32 + i*niiBatchVerifySigSize
		copy(input[offset:offset+32], e.hash)
		input[offset+32] = e.v
		copy(input[offset+33:offset+65], e.r)
		copy(input[offset+65:offset+97], e.s)
	}

	return input
}

// Ensure the test imports are used.
var _ *ecdsa.PrivateKey
