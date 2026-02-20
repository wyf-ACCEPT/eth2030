package vm

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// --- FieldMulExtPrecompile tests ---

func TestFieldMulExtSimple(t *testing.T) {
	p := &FieldMulExtPrecompile{}

	// 7 * 5 mod 17 = 35 mod 17 = 1
	fs := uint64(32)
	a := big.NewInt(7)
	b := big.NewInt(5)
	mod := big.NewInt(17)

	input := buildFieldMulExtInput(fs, a, b, mod)
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := new(big.Int).SetBytes(out)
	if result.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("7 * 5 mod 17: got %v, want 1", result)
	}
}

func TestFieldMulExtLargeValues(t *testing.T) {
	p := &FieldMulExtPrecompile{}

	fs := uint64(32)
	// Large 256-bit prime.
	mod, _ := new(big.Int).SetString("115792089237316195423570985008687907853269984665640564039457584007913129639747", 10)
	a := new(big.Int).Sub(mod, big.NewInt(1))
	b := new(big.Int).Sub(mod, big.NewInt(2))

	input := buildFieldMulExtInput(fs, a, b, mod)
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := new(big.Int).SetBytes(out)
	expected := new(big.Int).Mul(a, b)
	expected.Mod(expected, mod)
	if result.Cmp(expected) != 0 {
		t.Errorf("large field mul: got %v, want %v", result, expected)
	}
}

func TestFieldMulExtZeroModulus(t *testing.T) {
	p := &FieldMulExtPrecompile{}
	input := buildFieldMulExtInput(32, big.NewInt(5), big.NewInt(3), big.NewInt(0))
	_, err := p.Run(input)
	if err != ErrFieldZeroModulus {
		t.Errorf("expected ErrFieldZeroModulus, got %v", err)
	}
}

func TestFieldMulExtZeroFieldSize(t *testing.T) {
	p := &FieldMulExtPrecompile{}
	input := []byte{0x00, 0x00} // fs = 0
	_, err := p.Run(input)
	if err != ErrFieldZeroFieldSize {
		t.Errorf("expected ErrFieldZeroFieldSize, got %v", err)
	}
}

func TestFieldMulExtFieldSizeTooLarge(t *testing.T) {
	p := &FieldMulExtPrecompile{}
	// fs = 257 > 256
	input := []byte{0x01, 0x01}
	_, err := p.Run(input)
	if err != ErrFieldSizeTooLarge {
		t.Errorf("expected ErrFieldSizeTooLarge, got %v", err)
	}
}

func TestFieldMulExtInputTooShort(t *testing.T) {
	p := &FieldMulExtPrecompile{}
	_, err := p.Run(nil)
	if err != ErrFieldInputTooShort {
		t.Errorf("expected ErrFieldInputTooShort, got %v", err)
	}
	_, err = p.Run([]byte{0x01})
	if err != ErrFieldInputTooShort {
		t.Errorf("expected ErrFieldInputTooShort for 1 byte, got %v", err)
	}
}

func TestFieldMulExtGas(t *testing.T) {
	p := &FieldMulExtPrecompile{}

	// Small field: fs=8, words=1, gas = 50 + 1 = 51
	input := []byte{0x00, 0x08}
	gas := p.RequiredGas(input)
	if gas != 51 {
		t.Errorf("gas for fs=8: got %d, want 51", gas)
	}

	// Larger field: fs=32, words=4, gas = 50 + 16 = 66
	input = []byte{0x00, 0x20}
	gas = p.RequiredGas(input)
	if gas != 66 {
		t.Errorf("gas for fs=32: got %d, want 66", gas)
	}

	// Even larger: fs=64, words=8, gas = 50 + 64 = 114
	input = []byte{0x00, 0x40}
	gas = p.RequiredGas(input)
	if gas != 114 {
		t.Errorf("gas for fs=64: got %d, want 114", gas)
	}
}

func TestFieldMulExtIdentity(t *testing.T) {
	p := &FieldMulExtPrecompile{}

	// a * 1 mod p = a
	fs := uint64(32)
	mod := big.NewInt(97)
	a := big.NewInt(42)

	input := buildFieldMulExtInput(fs, a, big.NewInt(1), mod)
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result := new(big.Int).SetBytes(out)
	if result.Cmp(big.NewInt(42)) != 0 {
		t.Errorf("42 * 1 mod 97: got %v, want 42", result)
	}
}

func TestFieldMulExtZero(t *testing.T) {
	p := &FieldMulExtPrecompile{}

	// a * 0 mod p = 0
	fs := uint64(32)
	mod := big.NewInt(97)

	input := buildFieldMulExtInput(fs, big.NewInt(42), big.NewInt(0), mod)
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result := new(big.Int).SetBytes(out)
	if result.Sign() != 0 {
		t.Errorf("42 * 0 mod 97: got %v, want 0", result)
	}
}

// --- FieldInvExtPrecompile tests ---

func TestFieldInvExtSimple(t *testing.T) {
	p := &FieldInvExtPrecompile{}

	// 3^-1 mod 7 = 5 (since 3*5 = 15 = 1 mod 7)
	fs := uint64(32)
	input := buildFieldInvExtInput(fs, big.NewInt(3), big.NewInt(7))
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	inv := new(big.Int).SetBytes(out)
	check := new(big.Int).Mul(big.NewInt(3), inv)
	check.Mod(check, big.NewInt(7))
	if check.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("3 * 3^-1 mod 7 = %v, want 1 (inv=%v)", check, inv)
	}
}

func TestFieldInvExtLargePrime(t *testing.T) {
	p := &FieldInvExtPrecompile{}

	fs := uint64(32)
	prime, _ := new(big.Int).SetString("115792089237316195423570985008687907853269984665640564039457584007913129639747", 10)
	value := big.NewInt(12345678)

	input := buildFieldInvExtInput(fs, value, prime)
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	inv := new(big.Int).SetBytes(out)
	check := new(big.Int).Mul(value, inv)
	check.Mod(check, prime)
	if check.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("value * inv mod prime = %v, want 1", check)
	}
}

func TestFieldInvExtNoInverseZero(t *testing.T) {
	p := &FieldInvExtPrecompile{}
	fs := uint64(32)
	input := buildFieldInvExtInput(fs, big.NewInt(0), big.NewInt(7))
	_, err := p.Run(input)
	if err != ErrFieldNoInverse {
		t.Errorf("expected ErrFieldNoInverse for zero value, got %v", err)
	}
}

func TestFieldInvExtNoInverseNonCoprime(t *testing.T) {
	p := &FieldInvExtPrecompile{}
	// 6 has no inverse mod 9 since gcd(6,9)=3.
	fs := uint64(32)
	input := buildFieldInvExtInput(fs, big.NewInt(6), big.NewInt(9))
	_, err := p.Run(input)
	if err != ErrFieldNoInverse {
		t.Errorf("expected ErrFieldNoInverse for non-coprime, got %v", err)
	}
}

func TestFieldInvExtZeroModulus(t *testing.T) {
	p := &FieldInvExtPrecompile{}
	fs := uint64(32)
	input := buildFieldInvExtInput(fs, big.NewInt(5), big.NewInt(0))
	_, err := p.Run(input)
	if err != ErrFieldZeroModulus {
		t.Errorf("expected ErrFieldZeroModulus, got %v", err)
	}
}

func TestFieldInvExtGas(t *testing.T) {
	p := &FieldInvExtPrecompile{}

	// fs=32, words=4, gas = 100 + 4*4*2 = 132
	input := []byte{0x00, 0x20}
	gas := p.RequiredGas(input)
	if gas != 132 {
		t.Errorf("gas for fs=32: got %d, want 132", gas)
	}
}

func TestFieldInvExtSelf(t *testing.T) {
	p := &FieldInvExtPrecompile{}

	// 1^-1 mod p = 1
	fs := uint64(32)
	input := buildFieldInvExtInput(fs, big.NewInt(1), big.NewInt(97))
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	inv := new(big.Int).SetBytes(out)
	if inv.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("1^-1 mod 97: got %v, want 1", inv)
	}
}

// --- FieldExpPrecompile tests ---

func TestFieldExpSimple(t *testing.T) {
	p := &FieldExpPrecompile{}

	// 2^10 mod 1000 = 1024 mod 1000 = 24
	fs := uint64(32)
	input := buildFieldExpInput(fs, big.NewInt(2), big.NewInt(10), big.NewInt(1000))
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := new(big.Int).SetBytes(out)
	if result.Cmp(big.NewInt(24)) != 0 {
		t.Errorf("2^10 mod 1000: got %v, want 24", result)
	}
}

func TestFieldExpLarge(t *testing.T) {
	p := &FieldExpPrecompile{}

	fs := uint64(32)
	mod, _ := new(big.Int).SetString("115792089237316195423570985008687907853269984665640564039457584007913129639747", 10)
	base := new(big.Int).Lsh(big.NewInt(1), 255)
	exp := big.NewInt(65537)

	input := buildFieldExpInput(fs, base, exp, mod)
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := new(big.Int).SetBytes(out)
	expected := new(big.Int).Exp(base, exp, mod)
	if result.Cmp(expected) != 0 {
		t.Errorf("large modexp: got %v, want %v", result, expected)
	}
}

func TestFieldExpZeroExponent(t *testing.T) {
	p := &FieldExpPrecompile{}

	// x^0 mod p = 1 (for any nonzero x and p > 1)
	fs := uint64(32)
	input := buildFieldExpInput(fs, big.NewInt(42), big.NewInt(0), big.NewInt(97))
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result := new(big.Int).SetBytes(out)
	if result.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("42^0 mod 97: got %v, want 1", result)
	}
}

func TestFieldExpZeroBase(t *testing.T) {
	p := &FieldExpPrecompile{}

	// 0^n mod p = 0 (for n > 0)
	fs := uint64(32)
	input := buildFieldExpInput(fs, big.NewInt(0), big.NewInt(5), big.NewInt(97))
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result := new(big.Int).SetBytes(out)
	if result.Sign() != 0 {
		t.Errorf("0^5 mod 97: got %v, want 0", result)
	}
}

func TestFieldExpZeroModulus(t *testing.T) {
	p := &FieldExpPrecompile{}

	// Zero modulus returns zero.
	fs := uint64(32)
	input := buildFieldExpInput(fs, big.NewInt(2), big.NewInt(10), big.NewInt(0))
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, b := range out {
		if b != 0 {
			t.Errorf("expected all zeros for zero modulus, got %x", out)
			break
		}
	}
}

func TestFieldExpGas(t *testing.T) {
	p := &FieldExpPrecompile{}

	// fs=32, small exponent: gas should be at least 200 (minimum).
	fs := uint64(32)
	input := buildFieldExpInput(fs, big.NewInt(2), big.NewInt(3), big.NewInt(97))
	gas := p.RequiredGas(input)
	if gas < 200 {
		t.Errorf("gas should be at least 200, got %d", gas)
	}

	// Larger exponent should cost more.
	largeExp := new(big.Int).Lsh(big.NewInt(1), 200)
	largeInput := buildFieldExpInput(fs, big.NewInt(2), largeExp, big.NewInt(97))
	largeGas := p.RequiredGas(largeInput)
	if largeGas <= gas {
		t.Errorf("larger exponent should cost more gas: small=%d, large=%d", gas, largeGas)
	}
}

func TestFieldExpFermat(t *testing.T) {
	p := &FieldExpPrecompile{}

	// Fermat's little theorem: a^(p-1) = 1 mod p for prime p.
	fs := uint64(32)
	prime := big.NewInt(104729)
	a := big.NewInt(42)
	exp := new(big.Int).Sub(prime, big.NewInt(1))

	input := buildFieldExpInput(fs, a, exp, prime)
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result := new(big.Int).SetBytes(out)
	if result.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("Fermat's little theorem: 42^(p-1) mod p = %v, want 1", result)
	}
}

// --- BatchFieldVerifyPrecompile tests ---

func TestBatchFieldVerifySingle(t *testing.T) {
	p := &BatchFieldVerifyPrecompile{}

	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	hash := crypto.Keccak256([]byte("test message"))
	sig, err := crypto.Sign(hash, key)
	if err != nil {
		t.Fatalf("failed to sign: %v", err)
	}

	input := buildBatchFieldVerifyInput([]*batchFieldEntry{
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

func TestBatchFieldVerifyMultiple(t *testing.T) {
	p := &BatchFieldVerifyPrecompile{}

	entries := make([]*batchFieldEntry, 5)
	for i := 0; i < 5; i++ {
		key, err := crypto.GenerateKey()
		if err != nil {
			t.Fatalf("failed to generate key %d: %v", i, err)
		}
		msg := []byte("msg-" + string(rune('0'+i)))
		hash := crypto.Keccak256(msg)
		sig, err := crypto.Sign(hash, key)
		if err != nil {
			t.Fatalf("failed to sign %d: %v", i, err)
		}
		entries[i] = &batchFieldEntry{
			hash: hash, v: sig[64], r: sig[0:32], s: sig[32:64],
		}
	}

	input := buildBatchFieldVerifyInput(entries)
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 || out[0] != 0x01 {
		t.Errorf("expected 0x01 for all valid sigs, got %x", out)
	}
}

func TestBatchFieldVerifyInvalid(t *testing.T) {
	p := &BatchFieldVerifyPrecompile{}

	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	hash := crypto.Keccak256([]byte("valid"))
	sig, err := crypto.Sign(hash, key)
	if err != nil {
		t.Fatalf("failed to sign: %v", err)
	}

	// One valid, one with invalid v.
	entries := []*batchFieldEntry{
		{hash: hash, v: sig[64], r: sig[0:32], s: sig[32:64]},
		{hash: hash, v: 0xFF, r: sig[0:32], s: sig[32:64]},
	}

	input := buildBatchFieldVerifyInput(entries)
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 || out[0] != 0x00 {
		t.Errorf("expected 0x00 for invalid batch, got %x", out)
	}
}

func TestBatchFieldVerifyZeroCount(t *testing.T) {
	p := &BatchFieldVerifyPrecompile{}
	input := []byte{0x00, 0x00}
	_, err := p.Run(input)
	if err != ErrFieldInvalidBatchSize {
		t.Errorf("expected ErrFieldInvalidBatchSize for zero count, got %v", err)
	}
}

func TestBatchFieldVerifyInputTooShort(t *testing.T) {
	p := &BatchFieldVerifyPrecompile{}
	_, err := p.Run(nil)
	if err != ErrFieldInputTooShort {
		t.Errorf("expected ErrFieldInputTooShort for nil, got %v", err)
	}
	_, err = p.Run([]byte{0x01})
	if err != ErrFieldInputTooShort {
		t.Errorf("expected ErrFieldInputTooShort for 1 byte, got %v", err)
	}
}

func TestBatchFieldVerifyShortPayload(t *testing.T) {
	p := &BatchFieldVerifyPrecompile{}
	// Count says 1 but no signature data follows.
	input := []byte{0x00, 0x01}
	_, err := p.Run(input)
	if err != ErrFieldInputTooShort {
		t.Errorf("expected ErrFieldInputTooShort for short payload, got %v", err)
	}
}

func TestBatchFieldVerifyGas(t *testing.T) {
	p := &BatchFieldVerifyPrecompile{}

	// 3 sigs: gas = 500 + 2800*3 = 8900
	input := []byte{0x00, 0x03}
	gas := p.RequiredGas(input)
	if gas != 8900 {
		t.Errorf("gas for 3 sigs: got %d, want 8900", gas)
	}
}

// --- Registration test ---

func TestFieldPrecompilesRegisteredKPlus(t *testing.T) {
	addrs := []types.Address{
		NiiFieldMulExtAddr, NiiFieldInvExtAddr,
		NiiFieldExpAddr, NiiBatchFieldVerAddr,
	}
	for _, addr := range addrs {
		if _, ok := PrecompiledContractsKPlus[addr]; !ok {
			t.Errorf("field precompile not found at %s in K+ fork", addr.Hex())
		}
	}
}

func TestKPlusIncludesJPlusPrecompiles(t *testing.T) {
	for addr := range PrecompiledContractsJPlus {
		if _, ok := PrecompiledContractsKPlus[addr]; !ok {
			t.Errorf("J+ precompile at %s not found in K+ fork", addr.Hex())
		}
	}
}

// --- leftPadBytes tests ---

func TestLeftPadBytes(t *testing.T) {
	tests := []struct {
		data []byte
		size int
		want []byte
	}{
		{[]byte{0x01}, 4, []byte{0x00, 0x00, 0x00, 0x01}},
		{[]byte{0xFF, 0xAB}, 4, []byte{0x00, 0x00, 0xFF, 0xAB}},
		{nil, 3, []byte{0x00, 0x00, 0x00}},
		{[]byte{0x01, 0x02, 0x03}, 3, []byte{0x01, 0x02, 0x03}},
	}
	for _, tt := range tests {
		got := leftPadBytes(tt.data, tt.size)
		if len(got) != tt.size {
			t.Errorf("leftPadBytes(%x, %d): len = %d, want %d", tt.data, tt.size, len(got), tt.size)
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("leftPadBytes(%x, %d) = %x, want %x", tt.data, tt.size, got, tt.want)
				break
			}
		}
	}
}

// --- Test helpers ---

func buildFieldMulExtInput(fs uint64, a, b, mod *big.Int) []byte {
	size := int(fs)
	input := make([]byte, 2+3*size)
	input[0] = byte(fs >> 8)
	input[1] = byte(fs)

	aBytes := a.Bytes()
	bBytes := b.Bytes()
	modBytes := mod.Bytes()

	copy(input[2+size-len(aBytes):2+size], aBytes)
	copy(input[2+2*size-len(bBytes):2+2*size], bBytes)
	copy(input[2+3*size-len(modBytes):2+3*size], modBytes)
	return input
}

func buildFieldInvExtInput(fs uint64, value, mod *big.Int) []byte {
	size := int(fs)
	input := make([]byte, 2+2*size)
	input[0] = byte(fs >> 8)
	input[1] = byte(fs)

	valBytes := value.Bytes()
	modBytes := mod.Bytes()

	copy(input[2+size-len(valBytes):2+size], valBytes)
	copy(input[2+2*size-len(modBytes):2+2*size], modBytes)
	return input
}

func buildFieldExpInput(fs uint64, base, exp, mod *big.Int) []byte {
	size := int(fs)
	input := make([]byte, 2+3*size)
	input[0] = byte(fs >> 8)
	input[1] = byte(fs)

	baseBytes := base.Bytes()
	expBytes := exp.Bytes()
	modBytes := mod.Bytes()

	copy(input[2+size-len(baseBytes):2+size], baseBytes)
	copy(input[2+2*size-len(expBytes):2+2*size], expBytes)
	copy(input[2+3*size-len(modBytes):2+3*size], modBytes)
	return input
}

type batchFieldEntry struct {
	hash []byte
	v    byte
	r    []byte
	s    []byte
}

func buildBatchFieldVerifyInput(entries []*batchFieldEntry) []byte {
	count := len(entries)
	input := make([]byte, 2+count*batchFieldSigSize)
	input[0] = byte(count >> 8)
	input[1] = byte(count)

	for i, e := range entries {
		offset := 2 + i*batchFieldSigSize
		copy(input[offset:offset+32], e.hash)
		input[offset+32] = e.v
		copy(input[offset+33:offset+65], e.r)
		copy(input[offset+65:offset+97], e.s)
	}
	return input
}
