package vm

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// TestBLS12PrecompilesRegistered verifies all BLS12-381 precompiles are registered.
func TestBLS12PrecompilesRegistered(t *testing.T) {
	for addr := byte(0x0b); addr <= 0x13; addr++ {
		a := types.BytesToAddress([]byte{addr})
		if !IsPrecompiledContract(a) {
			t.Errorf("BLS12-381 precompile 0x%02x not registered", addr)
		}
	}
}

// TestBLS12G1AddGas verifies gas cost for G1 addition.
func TestBLS12G1AddGas(t *testing.T) {
	c := &bls12G1Add{}
	input := make([]byte, 2*bls12G1PointSize)
	if got := c.RequiredGas(input); got != bls12G1AddGas {
		t.Errorf("G1Add gas = %d, want %d", got, bls12G1AddGas)
	}
}

// TestBLS12G1AddInvalidInput verifies input length validation.
func TestBLS12G1AddInvalidInput(t *testing.T) {
	c := &bls12G1Add{}
	_, err := c.Run(make([]byte, 100))
	if err != ErrBLS12InvalidInput {
		t.Errorf("expected ErrBLS12InvalidInput, got %v", err)
	}
}

// TestBLS12G1AddInfinity verifies adding two points at infinity returns infinity.
func TestBLS12G1AddInfinity(t *testing.T) {
	c := &bls12G1Add{}
	input := make([]byte, 2*bls12G1PointSize) // all zeros = two infinity points
	result, err := c.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != bls12G1PointSize {
		t.Fatalf("result length = %d, want %d", len(result), bls12G1PointSize)
	}
	if !isZeroBytes(result) {
		t.Error("expected point at infinity (all zeros)")
	}
}

// TestBLS12G1AddInvalidCoord verifies coordinate >= p is rejected.
func TestBLS12G1AddInvalidCoord(t *testing.T) {
	c := &bls12G1Add{}
	input := make([]byte, 2*bls12G1PointSize)
	// Set first coordinate to p (the field modulus), which is invalid.
	pBytes := bls12Modulus.Bytes()
	copy(input[bls12FpSize-len(pBytes):bls12FpSize], pBytes)
	_, err := c.Run(input)
	if err != ErrBLS12InvalidPoint {
		t.Errorf("expected ErrBLS12InvalidPoint, got %v", err)
	}
}

// TestBLS12G1MulGas verifies gas cost for G1 scalar multiplication.
func TestBLS12G1MulGas(t *testing.T) {
	c := &bls12G1Mul{}
	input := make([]byte, bls12G1PointSize+bls12ScalarSize)
	if got := c.RequiredGas(input); got != bls12G1MulGas {
		t.Errorf("G1Mul gas = %d, want %d", got, bls12G1MulGas)
	}
}

// TestBLS12G1MulZeroScalar verifies scalar=0 returns infinity.
func TestBLS12G1MulZeroScalar(t *testing.T) {
	c := &bls12G1Mul{}
	input := make([]byte, bls12G1PointSize+bls12ScalarSize)
	// Point at infinity + scalar 0.
	result, err := c.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isZeroBytes(result) {
		t.Error("expected point at infinity for scalar=0")
	}
}

// TestBLS12G2AddGas verifies gas cost for G2 addition.
func TestBLS12G2AddGas(t *testing.T) {
	c := &bls12G2Add{}
	input := make([]byte, 2*bls12G2PointSize)
	if got := c.RequiredGas(input); got != bls12G2AddGas {
		t.Errorf("G2Add gas = %d, want %d", got, bls12G2AddGas)
	}
}

// TestBLS12G2AddInfinity verifies adding two G2 infinity points.
func TestBLS12G2AddInfinity(t *testing.T) {
	c := &bls12G2Add{}
	input := make([]byte, 2*bls12G2PointSize)
	result, err := c.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isZeroBytes(result) {
		t.Error("expected G2 point at infinity")
	}
}

// TestBLS12PairingGas verifies gas cost scales with number of pairs.
func TestBLS12PairingGas(t *testing.T) {
	c := &bls12Pairing{}
	pairSize := bls12G1PointSize + bls12G2PointSize

	tests := []struct {
		pairs    int
		expected uint64
	}{
		{0, bls12PairingBaseGas},
		{1, bls12PairingBaseGas + bls12PairingPerPairGas},
		{2, bls12PairingBaseGas + 2*bls12PairingPerPairGas},
	}

	for _, tt := range tests {
		input := make([]byte, tt.pairs*pairSize)
		if got := c.RequiredGas(input); got != tt.expected {
			t.Errorf("Pairing gas for %d pairs = %d, want %d", tt.pairs, got, tt.expected)
		}
	}
}

// TestBLS12PairingTrivial verifies all-zero pairing returns true.
func TestBLS12PairingTrivial(t *testing.T) {
	c := &bls12Pairing{}
	pairSize := bls12G1PointSize + bls12G2PointSize
	input := make([]byte, pairSize) // one pair, all zeros
	result, err := c.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 32 {
		t.Fatalf("result length = %d, want 32", len(result))
	}
	if result[31] != 1 {
		t.Error("all-zero pairing should return true (1)")
	}
}

// TestBLS12PairingInvalidLength verifies non-multiple of pair size is rejected.
func TestBLS12PairingInvalidLength(t *testing.T) {
	c := &bls12Pairing{}
	_, err := c.Run(make([]byte, 100))
	if err != ErrBLS12InvalidInput {
		t.Errorf("expected ErrBLS12InvalidInput, got %v", err)
	}
}

// TestBLS12MapFpToG1InvalidInput verifies wrong input length is rejected.
func TestBLS12MapFpToG1InvalidInput(t *testing.T) {
	c := &bls12MapFpToG1{}
	_, err := c.Run(make([]byte, 32))
	if err != ErrBLS12InvalidInput {
		t.Errorf("expected ErrBLS12InvalidInput, got %v", err)
	}
}

// TestBLS12MapFpToG1InvalidField verifies field element >= p is rejected.
func TestBLS12MapFpToG1InvalidField(t *testing.T) {
	c := &bls12MapFpToG1{}
	input := make([]byte, bls12FpSize)
	pBytes := bls12Modulus.Bytes()
	copy(input[bls12FpSize-len(pBytes):], pBytes)
	_, err := c.Run(input)
	if err != ErrBLS12InvalidPoint {
		t.Errorf("expected ErrBLS12InvalidPoint, got %v", err)
	}
}

// TestBLS12MapFp2ToG2InvalidInput verifies wrong input length.
func TestBLS12MapFp2ToG2InvalidInput(t *testing.T) {
	c := &bls12MapFp2ToG2{}
	_, err := c.Run(make([]byte, 64))
	if err != ErrBLS12InvalidInput {
		t.Errorf("expected ErrBLS12InvalidInput, got %v", err)
	}
}

// TestBLS12MSMDiscount verifies the MSM discount table.
func TestBLS12MSMDiscount(t *testing.T) {
	tests := []struct {
		k        uint64
		expected uint64
	}{
		{0, 0},
		{1, 1200},
		{2, 888},
		{5, 594},
		{10, 423},
		{128, 2},
		{200, 2}, // beyond table
	}
	for _, tt := range tests {
		if got := msmDiscount(tt.k); got != tt.expected {
			t.Errorf("msmDiscount(%d) = %d, want %d", tt.k, got, tt.expected)
		}
	}
}

// TestBLS12G1MSMGas verifies MSM gas calculation.
func TestBLS12G1MSMGas(t *testing.T) {
	c := &bls12G1MSM{}

	// 2 pairs: discount=888, gas = (12000 * 2 * 888) / 1000 = 21312
	pairSize := bls12G1PointSize + bls12ScalarSize
	input := make([]byte, 2*pairSize)
	expected := uint64((bls12G1MSMBaseGas * 2 * 888) / 1000)
	if got := c.RequiredGas(input); got != expected {
		t.Errorf("G1MSM gas for 2 pairs = %d, want %d", got, expected)
	}
}

// TestBLS12Constants verifies BLS12-381 field constants are correct.
func TestBLS12Constants(t *testing.T) {
	// Field modulus p should be 381 bits.
	if bls12Modulus.BitLen() != 381 {
		t.Errorf("BLS12-381 modulus bit length = %d, want 381", bls12Modulus.BitLen())
	}

	// Subgroup order r should be 255 bits.
	if bls12Order.BitLen() != 255 {
		t.Errorf("BLS12-381 order bit length = %d, want 255", bls12Order.BitLen())
	}

	// p should be prime (probabilistic check).
	if !bls12Modulus.ProbablyPrime(20) {
		t.Error("BLS12-381 modulus does not appear to be prime")
	}

	// r should be prime.
	if !bls12Order.ProbablyPrime(20) {
		t.Error("BLS12-381 order does not appear to be prime")
	}
}

// TestRunPrecompiledContractBLS12 verifies BLS12 precompiles are callable
// through the RunPrecompiledContract dispatcher.
func TestRunPrecompiledContractBLS12(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x0b})
	input := make([]byte, 2*bls12G1PointSize) // all zeros = infinity + infinity

	result, gas, err := RunPrecompiledContract(addr, input, 100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gas != 100000-bls12G1AddGas {
		t.Errorf("remaining gas = %d, want %d", gas, 100000-bls12G1AddGas)
	}
	if !isZeroBytes(result) {
		t.Error("expected infinity result")
	}
}

// TestRunPrecompiledContractBLS12OutOfGas verifies OOG for BLS12 precompiles.
func TestRunPrecompiledContractBLS12OutOfGas(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x0b})
	input := make([]byte, 2*bls12G1PointSize)

	_, _, err := RunPrecompiledContract(addr, input, 100) // too little gas
	if err != ErrOutOfGas {
		t.Errorf("expected ErrOutOfGas, got %v", err)
	}
}

// Ensure big is used (imported for bls12Modulus in tests above).
var _ = new(big.Int)
