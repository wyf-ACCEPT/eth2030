package vm

import (
	"math/big"
	"math/bits"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestNTTForwardInverseRoundtrip(t *testing.T) {
	// [1, 2, 3, 4] -> forward NTT -> inverse NTT -> [1, 2, 3, 4]
	coeffs := []*big.Int{big.NewInt(1), big.NewInt(2), big.NewInt(3), big.NewInt(4)}
	omega, err := findRootOfUnity(4, bn254ScalarField)
	if err != nil {
		t.Fatal(err)
	}

	fwd := nttForward(coeffs, omega, bn254ScalarField)
	inv := nttInverse(fwd, omega, bn254ScalarField)

	for i, v := range inv {
		if v.Cmp(coeffs[i]) != 0 {
			t.Errorf("roundtrip[%d]: got %v, want %v", i, v, coeffs[i])
		}
	}
}

func TestNTTPolynomialMultiplication(t *testing.T) {
	// Multiply polynomials a(x) = 1 + 2x and b(x) = 3 + 4x
	// Product: 3 + 10x + 8x^2
	// Use size 4 (next power of two for length 3 result).
	a := []*big.Int{big.NewInt(1), big.NewInt(2), big.NewInt(0), big.NewInt(0)}
	b := []*big.Int{big.NewInt(3), big.NewInt(4), big.NewInt(0), big.NewInt(0)}

	omega, err := findRootOfUnity(4, bn254ScalarField)
	if err != nil {
		t.Fatal(err)
	}

	fa := nttForward(a, omega, bn254ScalarField)
	fb := nttForward(b, omega, bn254ScalarField)

	// Pointwise multiply.
	fc := make([]*big.Int, 4)
	for i := range fc {
		fc[i] = new(big.Int).Mul(fa[i], fb[i])
		fc[i].Mod(fc[i], bn254ScalarField)
	}

	result := nttInverse(fc, omega, bn254ScalarField)

	expected := []*big.Int{big.NewInt(3), big.NewInt(10), big.NewInt(8), big.NewInt(0)}
	for i, v := range result {
		if v.Cmp(expected[i]) != 0 {
			t.Errorf("convolution[%d]: got %v, want %v", i, v, expected[i])
		}
	}
}

func TestNTTPrecompileForwardInverse(t *testing.T) {
	p := &nttPrecompile{}

	// Build input: op_type(0x00) || 4 coefficients [1, 2, 3, 4] as 32-byte big-endian.
	coeffs := []int64{1, 2, 3, 4}
	input := make([]byte, 1+4*32)
	input[0] = 0x00 // forward
	for i, c := range coeffs {
		b := big.NewInt(c).Bytes()
		copy(input[1+i*32+(32-len(b)):], b)
	}

	fwdOut, err := p.Run(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(fwdOut) != 4*32 {
		t.Fatalf("expected %d bytes output, got %d", 4*32, len(fwdOut))
	}

	// Now inverse: op_type(0x01) || forward output.
	invInput := make([]byte, 1+len(fwdOut))
	invInput[0] = 0x01 // inverse
	copy(invInput[1:], fwdOut)

	invOut, err := p.Run(invInput)
	if err != nil {
		t.Fatal(err)
	}

	// Verify roundtrip.
	for i, c := range coeffs {
		val := new(big.Int).SetBytes(invOut[i*32 : (i+1)*32])
		if val.Cmp(big.NewInt(c)) != 0 {
			t.Errorf("precompile roundtrip[%d]: got %v, want %d", i, val, c)
		}
	}
}

func TestNTTPrecompileGasCost(t *testing.T) {
	p := &nttPrecompile{}

	tests := []struct {
		name     string
		nElems   int
		wantGas  uint64
	}{
		{"4 elements", 4, NTTBaseCost + 4*2*NTTPerElementCost},           // log2(4) = 2
		{"8 elements", 8, NTTBaseCost + 8*3*NTTPerElementCost},           // log2(8) = 3
		{"16 elements", 16, NTTBaseCost + 16*4*NTTPerElementCost},        // log2(16) = 4
		{"1 element", 1, NTTBaseCost + 1*1*NTTPerElementCost},            // log2(1) = 0 -> clamped to 1
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := make([]byte, 1+tt.nElems*32)
			input[0] = 0x00
			got := p.RequiredGas(input)
			if got != tt.wantGas {
				t.Errorf("gas for %d elements: got %d, want %d", tt.nElems, got, tt.wantGas)
			}
		})
	}
}

func TestNTTPrecompileMaxSize(t *testing.T) {
	p := &nttPrecompile{}

	// Create input exceeding max degree.
	n := NTTMaxDegree + 1
	input := make([]byte, 1+n*32)
	input[0] = 0x00

	_, err := p.Run(input)
	if err != ErrNTTTooLarge {
		t.Errorf("expected ErrNTTTooLarge, got %v", err)
	}
}

func TestNTTPrecompileNotPowerOfTwo(t *testing.T) {
	p := &nttPrecompile{}

	// 3 elements = not a power of two.
	input := make([]byte, 1+3*32)
	input[0] = 0x00

	_, err := p.Run(input)
	if err != ErrNTTNotPowerOfTwo {
		t.Errorf("expected ErrNTTNotPowerOfTwo, got %v", err)
	}
}

func TestNTTPrecompileInvalidOpType(t *testing.T) {
	p := &nttPrecompile{}

	input := make([]byte, 1+4*32)
	input[0] = 0x02 // invalid

	_, err := p.Run(input)
	if err != ErrNTTInvalidOpType {
		t.Errorf("expected ErrNTTInvalidOpType, got %v", err)
	}
}

func TestNTTPrecompileEmptyInput(t *testing.T) {
	p := &nttPrecompile{}

	_, err := p.Run(nil)
	if err != ErrNTTInvalidInput {
		t.Errorf("expected ErrNTTInvalidInput, got %v", err)
	}

	_, err = p.Run([]byte{0x00})
	if err != ErrNTTInvalidInput {
		t.Errorf("expected ErrNTTInvalidInput for zero coefficients, got %v", err)
	}
}

func TestNTTPrecompileRegisteredIPlus(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x15})
	p, ok := PrecompiledContractsIPlus[addr]
	if !ok {
		t.Fatal("NTT precompile not found at address 0x15 in I+ fork")
	}
	if p == nil {
		t.Fatal("NTT precompile is nil")
	}
}

func TestNTTPrecompileViaRunPrecompiled(t *testing.T) {
	// Test through the registry for I+ fork.
	addr := types.BytesToAddress([]byte{0x15})
	p, ok := PrecompiledContractsIPlus[addr]
	if !ok {
		t.Skip("NTT precompile not in I+ fork")
	}

	// Forward NTT on [1, 0, 0, 0] should give [1, 1, 1, 1].
	input := make([]byte, 1+4*32)
	input[0] = 0x00
	input[32] = 1 // coefficients[0] = 1 (big-endian, so last byte of first 32-byte chunk)

	gas := p.RequiredGas(input)
	out, err := p.Run(input)
	if err != nil {
		t.Fatal(err)
	}
	if gas == 0 {
		t.Error("gas should be non-zero")
	}

	// NTT of [1, 0, 0, 0] evaluates the constant polynomial 1 at 4 roots
	// of unity, yielding [1, 1, 1, 1].
	for i := 0; i < 4; i++ {
		val := new(big.Int).SetBytes(out[i*32 : (i+1)*32])
		if val.Cmp(big.NewInt(1)) != 0 {
			t.Errorf("NTT([1,0,0,0])[%d] = %v, want 1", i, val)
		}
	}
}

func TestNTTSize8(t *testing.T) {
	// Test with a larger size (8 elements).
	n := 8
	coeffs := make([]*big.Int, n)
	for i := range coeffs {
		coeffs[i] = big.NewInt(int64(i + 1))
	}

	omega, err := findRootOfUnity(n, bn254ScalarField)
	if err != nil {
		t.Fatal(err)
	}

	fwd := nttForward(coeffs, omega, bn254ScalarField)
	inv := nttInverse(fwd, omega, bn254ScalarField)

	for i, v := range inv {
		if v.Cmp(coeffs[i]) != 0 {
			t.Errorf("roundtrip[%d]: got %v, want %v", i, v, coeffs[i])
		}
	}
}

func TestFindRootOfUnity(t *testing.T) {
	for _, n := range []int{1, 2, 4, 8, 16, 32, 64} {
		omega, err := findRootOfUnity(n, bn254ScalarField)
		if err != nil {
			t.Fatalf("findRootOfUnity(%d): %v", n, err)
		}

		// Verify omega^n == 1 mod p.
		check := new(big.Int).Exp(omega, big.NewInt(int64(n)), bn254ScalarField)
		if check.Cmp(big.NewInt(1)) != 0 {
			t.Errorf("omega^%d != 1 mod p", n)
		}

		// For n > 1, verify omega^(n/2) != 1 (primitive root).
		if n > 1 {
			half := new(big.Int).Exp(omega, big.NewInt(int64(n/2)), bn254ScalarField)
			if half.Cmp(big.NewInt(1)) == 0 {
				t.Errorf("omega^(%d/2) == 1, not a primitive root", n)
			}
		}
	}
}

func TestBitReverse(t *testing.T) {
	tests := []struct {
		v, numBits, want int
	}{
		{0, 2, 0},
		{1, 2, 2},
		{2, 2, 1},
		{3, 2, 3},
		{0, 3, 0},
		{1, 3, 4},
		{4, 3, 1},
		{7, 3, 7},
	}
	for _, tt := range tests {
		got := bitReverse(tt.v, tt.numBits)
		if got != tt.want {
			t.Errorf("bitReverse(%d, %d) = %d, want %d", tt.v, tt.numBits, got, tt.want)
		}
	}
}

func TestNTTGasCalculation(t *testing.T) {
	p := &nttPrecompile{}

	// Verify empty input.
	if g := p.RequiredGas(nil); g != 0 {
		t.Errorf("gas for nil input: got %d, want 0", g)
	}

	// Verify single byte (no coefficients).
	if g := p.RequiredGas([]byte{0x00}); g != NTTBaseCost {
		t.Errorf("gas for 0 elements: got %d, want %d", g, NTTBaseCost)
	}

	// Verify scaling: 256 elements.
	n := 256
	input := make([]byte, 1+n*32)
	log2n := uint64(bits.Len(uint(n)) - 1)
	expected := NTTBaseCost + uint64(n)*log2n*NTTPerElementCost
	if g := p.RequiredGas(input); g != expected {
		t.Errorf("gas for %d elements: got %d, want %d", n, g, expected)
	}
}
