package vm

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
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
	if err == nil {
		t.Error("expected error for coordinate >= p, got nil")
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
	if err == nil {
		t.Error("expected error for field element >= p, got nil")
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

// --- Additional tests exercising actual crypto operations via precompile dispatcher ---

// TestBLS12G1AddThroughDispatcher tests G1 addition of actual points via RunPrecompiledContract.
func TestBLS12G1AddThroughDispatcher(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x0b})
	// Use the G1 generator point (encoded as per EIP-2537).
	genX := blsG1GenX()
	genY := blsG1GenY()

	genEnc := make([]byte, bls12G1PointSize)
	copy(genEnc[:bls12FpSize], padFp(genX))
	copy(genEnc[bls12FpSize:], padFp(genY))

	// G + G should return 2*G (on the curve, not infinity).
	input := append(genEnc, genEnc...)
	result, _, err := RunPrecompiledContract(addr, input, 100000)
	if err != nil {
		t.Fatalf("G1Add(G, G) error: %v", err)
	}
	if isZeroBytes(result) {
		t.Error("G + G should not be infinity")
	}
	if len(result) != bls12G1PointSize {
		t.Fatalf("result length = %d, want %d", len(result), bls12G1PointSize)
	}
}

// TestBLS12G1MulThroughDispatcher tests G1 scalar mul via RunPrecompiledContract.
func TestBLS12G1MulThroughDispatcher(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x0c})
	genEnc := makeG1GenEncoded()

	// 1 * G = G
	scalar := make([]byte, bls12ScalarSize)
	scalar[31] = 1
	input := append(genEnc, scalar...)
	result, _, err := RunPrecompiledContract(addr, input, 100000)
	if err != nil {
		t.Fatalf("G1Mul(G, 1) error: %v", err)
	}
	if !bytesEq(result, genEnc) {
		t.Error("1 * G should equal G")
	}

	// r * G = infinity (order of the group)
	rBytes := bls12Order.Bytes()
	rScalar := make([]byte, bls12ScalarSize)
	copy(rScalar[bls12ScalarSize-len(rBytes):], rBytes)
	input = append(genEnc, rScalar...)
	result, _, err = RunPrecompiledContract(addr, input, 100000)
	if err != nil {
		t.Fatalf("G1Mul(G, r) error: %v", err)
	}
	if !isZeroBytes(result) {
		t.Error("[r] * G should be infinity")
	}
}

// TestBLS12G1MSMThroughDispatcher tests G1 MSM: 2*G + 3*G = 5*G.
func TestBLS12G1MSMThroughDispatcher(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x0d})
	genEnc := makeG1GenEncoded()

	// Pair 1: G * 2
	scalar2 := make([]byte, bls12ScalarSize)
	scalar2[31] = 2
	// Pair 2: G * 3
	scalar3 := make([]byte, bls12ScalarSize)
	scalar3[31] = 3

	input := make([]byte, 0, 2*(bls12G1PointSize+bls12ScalarSize))
	input = append(input, genEnc...)
	input = append(input, scalar2...)
	input = append(input, genEnc...)
	input = append(input, scalar3...)

	msmResult, _, err := RunPrecompiledContract(addr, input, 100000)
	if err != nil {
		t.Fatalf("G1MSM error: %v", err)
	}

	// Compare with 5*G via G1Mul.
	mulAddr := types.BytesToAddress([]byte{0x0c})
	scalar5 := make([]byte, bls12ScalarSize)
	scalar5[31] = 5
	mulInput := append(genEnc, scalar5...)
	mulResult, _, err := RunPrecompiledContract(mulAddr, mulInput, 100000)
	if err != nil {
		t.Fatalf("G1Mul error: %v", err)
	}

	if !bytesEq(msmResult, mulResult) {
		t.Error("MSM(G,2; G,3) should equal 5*G")
	}
}

// TestBLS12G2AddThroughDispatcher tests G2 addition via dispatcher.
func TestBLS12G2AddThroughDispatcher(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x0e})
	// Two infinity G2 points.
	input := make([]byte, 2*bls12G2PointSize)
	result, _, err := RunPrecompiledContract(addr, input, 100000)
	if err != nil {
		t.Fatalf("G2Add(inf, inf) error: %v", err)
	}
	if !isZeroBytes(result) {
		t.Error("inf + inf should be inf")
	}
}

// TestBLS12G2MulThroughDispatcher tests G2 scalar mul via dispatcher.
func TestBLS12G2MulThroughDispatcher(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x0f})
	// Infinity * 5 = infinity.
	input := make([]byte, bls12G2PointSize+bls12ScalarSize)
	input[bls12G2PointSize+31] = 5
	result, _, err := RunPrecompiledContract(addr, input, 100000)
	if err != nil {
		t.Fatalf("G2Mul(inf, 5) error: %v", err)
	}
	if !isZeroBytes(result) {
		t.Error("inf * 5 should be inf")
	}
}

// TestBLS12MapFpToG1ThroughDispatcher tests map-to-G1 via dispatcher.
func TestBLS12MapFpToG1ThroughDispatcher(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x12})
	input := make([]byte, bls12FpSize)
	input[63] = 1 // u = 1

	result, _, err := RunPrecompiledContract(addr, input, 100000)
	if err != nil {
		t.Fatalf("MapFpToG1(1) error: %v", err)
	}
	if len(result) != bls12G1PointSize {
		t.Fatalf("result length = %d, want %d", len(result), bls12G1PointSize)
	}
	// Result should be a valid point (not all zeros for non-trivial input).
	// The point is in the subgroup after cofactor clearing.
}

// TestBLS12MapFp2ToG2ThroughDispatcher tests map-to-G2 via dispatcher.
func TestBLS12MapFp2ToG2ThroughDispatcher(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x13})
	input := make([]byte, bls12Fp2Size)
	input[63] = 1  // imaginary part = 1
	input[127] = 2 // real part = 2

	result, _, err := RunPrecompiledContract(addr, input, 200000)
	if err != nil {
		t.Fatalf("MapFp2ToG2(1+2u) error: %v", err)
	}
	if len(result) != bls12G2PointSize {
		t.Fatalf("result length = %d, want %d", len(result), bls12G2PointSize)
	}
}

// TestBLS12PairingTwoInfinityPairs tests pairing with two pairs of infinity points.
func TestBLS12PairingTwoInfinityPairs(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x11})
	pairSize := bls12G1PointSize + bls12G2PointSize
	input := make([]byte, 2*pairSize) // two pairs, all zeros

	result, _, err := RunPrecompiledContract(addr, input, 200000)
	if err != nil {
		t.Fatalf("Pairing error: %v", err)
	}
	if result[31] != 1 {
		t.Error("pairing with all-infinity pairs should return true")
	}
}

// TestBLS12G2MSMThroughDispatcher tests G2 MSM with infinity.
func TestBLS12G2MSMThroughDispatcher(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x10})
	pairSize := bls12G2PointSize + bls12ScalarSize

	// Single pair: infinity * 1 = infinity.
	input := make([]byte, pairSize)
	input[bls12G2PointSize+31] = 1

	result, _, err := RunPrecompiledContract(addr, input, 200000)
	if err != nil {
		t.Fatalf("G2MSM error: %v", err)
	}
	if !isZeroBytes(result) {
		t.Error("MSM(inf, 1) should be infinity")
	}
}

// --- helpers for tests ---

// blsG1GenX returns the G1 generator x coordinate bytes (48 bytes).
func blsG1GenX() []byte {
	x, _ := new(big.Int).SetString(
		"17f1d3a73197d7942695638c4fa9ac0fc3688c4f9774b905a14e3a3f171bac586c55e83ff97a1aeffb3af00adb22c6bb", 16)
	return x.Bytes()
}

// blsG1GenY returns the G1 generator y coordinate bytes (48 bytes).
func blsG1GenY() []byte {
	y, _ := new(big.Int).SetString(
		"08b3f481e3aaa0f1a09e30ed741d8ae4fcf5e095d5d00af600db18cb2c04b3edd03cc744a2888ae40caa232946c5e7e1", 16)
	return y.Bytes()
}

// padFp pads a coordinate to 64 bytes (16 zero bytes + 48 coordinate bytes).
func padFp(b []byte) []byte {
	out := make([]byte, bls12FpSize)
	copy(out[bls12FpSize-len(b):], b)
	return out
}

// makeG1GenEncoded returns the encoded G1 generator (128 bytes).
func makeG1GenEncoded() []byte {
	enc := make([]byte, bls12G1PointSize)
	copy(enc[:bls12FpSize], padFp(blsG1GenX()))
	copy(enc[bls12FpSize:], padFp(blsG1GenY()))
	return enc
}

// bytesEq compares two byte slices.
func bytesEq(a, b []byte) bool {
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

// Ensure big is used (imported for bls12Modulus in tests above).
var _ = new(big.Int)
