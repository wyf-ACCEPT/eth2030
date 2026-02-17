package vm

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"golang.org/x/crypto/ripemd160"
)

func TestIsPrecompiledContract(t *testing.T) {
	// Addresses 1-5 should be precompiles.
	for i := 1; i <= 5; i++ {
		addr := types.BytesToAddress([]byte{byte(i)})
		if !IsPrecompiledContract(addr) {
			t.Errorf("address %d should be a precompiled contract", i)
		}
	}
	// Addresses 0 and 6+ should not be precompiles.
	for _, i := range []int{0, 6, 7, 10, 255} {
		addr := types.BytesToAddress([]byte{byte(i)})
		if IsPrecompiledContract(addr) {
			t.Errorf("address %d should not be a precompiled contract", i)
		}
	}
}

func TestRunPrecompiledContract_NotFound(t *testing.T) {
	addr := types.BytesToAddress([]byte{99})
	_, _, err := RunPrecompiledContract(addr, nil, 1000000)
	if err == nil {
		t.Fatal("expected error for non-precompile address")
	}
}

func TestRunPrecompiledContract_OutOfGas(t *testing.T) {
	addr := types.BytesToAddress([]byte{1}) // ecrecover costs 3000
	_, _, err := RunPrecompiledContract(addr, nil, 100)
	if err != ErrOutOfGas {
		t.Fatalf("expected ErrOutOfGas, got %v", err)
	}
}

func TestEcrecoverGas(t *testing.T) {
	c := &ecrecover{}
	if g := c.RequiredGas(nil); g != 3000 {
		t.Errorf("ecrecover gas = %d, want 3000", g)
	}
}

func TestEcrecoverInvalidInput(t *testing.T) {
	c := &ecrecover{}

	// Empty input: v will be 0, which is not 27 or 28 -> nil result.
	out, err := c.Run(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != nil {
		t.Errorf("expected nil output for empty input, got %x", out)
	}

	// Invalid v value (not 27 or 28).
	input := make([]byte, 128)
	input[63] = 26 // v = 26
	out, err = c.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != nil {
		t.Errorf("expected nil output for invalid v, got %x", out)
	}
}

func TestSha256(t *testing.T) {
	c := &sha256hash{}

	tests := []struct {
		input []byte
	}{
		{[]byte{}},
		{[]byte("hello")},
		{[]byte("The quick brown fox jumps over the lazy dog")},
	}

	for _, tt := range tests {
		out, err := c.Run(tt.input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := sha256.Sum256(tt.input)
		if !bytes.Equal(out, expected[:]) {
			t.Errorf("sha256(%q) = %x, want %x", tt.input, out, expected)
		}
	}
}

func TestSha256Gas(t *testing.T) {
	c := &sha256hash{}

	tests := []struct {
		inputLen int
		want     uint64
	}{
		{0, 60},          // 60 + 12*0
		{1, 72},          // 60 + 12*1
		{32, 72},         // 60 + 12*1
		{33, 84},         // 60 + 12*2
		{64, 84},         // 60 + 12*2
		{100, 60 + 12*4}, // ceil(100/32) = 4
	}

	for _, tt := range tests {
		input := make([]byte, tt.inputLen)
		if g := c.RequiredGas(input); g != tt.want {
			t.Errorf("sha256 gas for %d bytes = %d, want %d", tt.inputLen, g, tt.want)
		}
	}
}

func TestRipemd160(t *testing.T) {
	c := &ripemd160hash{}

	tests := []struct {
		input   string
		wantHex string // 20-byte RIPEMD-160 hash, hex encoded
	}{
		{"", "9c1185a5c5e9fc54612808977ee8f548b2258d31"},
		{"hello", "108f07b8382412612c048d07d13f814118445acd"},
	}

	for _, tt := range tests {
		out, err := c.Run([]byte(tt.input))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out) != 32 {
			t.Fatalf("ripemd160 output length = %d, want 32", len(out))
		}
		// First 12 bytes should be zero (left-padding).
		for i := 0; i < 12; i++ {
			if out[i] != 0 {
				t.Errorf("ripemd160 output byte %d = %d, want 0", i, out[i])
			}
		}
		// Remaining 20 bytes should be the hash.
		gotHex := hex.EncodeToString(out[12:])
		if gotHex != tt.wantHex {
			t.Errorf("ripemd160(%q) = %s, want %s", tt.input, gotHex, tt.wantHex)
		}
	}
}

func TestRipemd160MatchesStdlib(t *testing.T) {
	c := &ripemd160hash{}
	input := []byte("The quick brown fox jumps over the lazy dog")

	out, err := c.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	h := ripemd160.New()
	h.Write(input)
	expected := h.Sum(nil)

	if !bytes.Equal(out[12:], expected) {
		t.Errorf("ripemd160 hash mismatch: got %x, want %x", out[12:], expected)
	}
}

func TestRipemd160Gas(t *testing.T) {
	c := &ripemd160hash{}

	tests := []struct {
		inputLen int
		want     uint64
	}{
		{0, 600},           // 600 + 120*0
		{1, 720},           // 600 + 120*1
		{32, 720},          // 600 + 120*1
		{33, 840},          // 600 + 120*2
		{100, 600 + 120*4}, // ceil(100/32) = 4
	}

	for _, tt := range tests {
		input := make([]byte, tt.inputLen)
		if g := c.RequiredGas(input); g != tt.want {
			t.Errorf("ripemd160 gas for %d bytes = %d, want %d", tt.inputLen, g, tt.want)
		}
	}
}

func TestDataCopy(t *testing.T) {
	c := &dataCopy{}

	tests := [][]byte{
		{},
		{1, 2, 3, 4, 5},
		make([]byte, 100),
	}

	for _, input := range tests {
		out, err := c.Run(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !bytes.Equal(out, input) {
			t.Errorf("dataCopy(%x) = %x, want %x", input, out, input)
		}
		// Verify it's a copy, not the same slice.
		if len(input) > 0 && &out[0] == &input[0] {
			t.Error("dataCopy returned same slice, expected a copy")
		}
	}
}

func TestDataCopyGas(t *testing.T) {
	c := &dataCopy{}

	tests := []struct {
		inputLen int
		want     uint64
	}{
		{0, 15},         // 15 + 3*0
		{1, 18},         // 15 + 3*1
		{32, 18},        // 15 + 3*1
		{33, 21},        // 15 + 3*2
		{100, 15 + 3*4}, // ceil(100/32) = 4
	}

	for _, tt := range tests {
		input := make([]byte, tt.inputLen)
		if g := c.RequiredGas(input); g != tt.want {
			t.Errorf("dataCopy gas for %d bytes = %d, want %d", tt.inputLen, g, tt.want)
		}
	}
}

func TestBigModExp(t *testing.T) {
	c := &bigModExp{}

	// Test: 2^10 % 1000 = 24
	base := big.NewInt(2)
	exp := big.NewInt(10)
	mod := big.NewInt(1000)

	input := buildModExpInput(base.Bytes(), exp.Bytes(), mod.Bytes())
	out, err := c.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := new(big.Int).SetBytes(out)
	if result.Cmp(big.NewInt(24)) != 0 {
		t.Errorf("modexp(2, 10, 1000) = %s, want 24", result)
	}
}

func TestBigModExpZeroMod(t *testing.T) {
	c := &bigModExp{}

	// 2^10 % 0 = 0
	base := big.NewInt(2)
	exp := big.NewInt(10)
	mod := big.NewInt(0)

	input := buildModExpInput(base.Bytes(), exp.Bytes(), mod.Bytes())
	out, err := c.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if new(big.Int).SetBytes(out).Sign() != 0 {
		t.Errorf("modexp with zero mod should return 0, got %x", out)
	}
}

func TestBigModExpLargeValues(t *testing.T) {
	c := &bigModExp{}

	// 123456789^65537 % 998244353
	base := big.NewInt(123456789)
	exp := big.NewInt(65537)
	mod := big.NewInt(998244353)

	input := buildModExpInput(base.Bytes(), exp.Bytes(), mod.Bytes())
	out, err := c.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := new(big.Int).Exp(base, exp, mod)
	result := new(big.Int).SetBytes(out)
	if result.Cmp(expected) != 0 {
		t.Errorf("modexp(123456789, 65537, 998244353) = %s, want %s", result, expected)
	}
}

func TestBigModExpGas(t *testing.T) {
	c := &bigModExp{}

	// Simple case: small values should cost minimum 200 gas.
	base := big.NewInt(2)
	exp := big.NewInt(10)
	mod := big.NewInt(1000)

	input := buildModExpInput(base.Bytes(), exp.Bytes(), mod.Bytes())
	gas := c.RequiredGas(input)
	if gas < 200 {
		t.Errorf("modexp gas = %d, want >= 200", gas)
	}
}

func TestRunPrecompiledContractGasAccounting(t *testing.T) {
	// Run sha256 with plenty of gas and verify remaining gas.
	addr := types.BytesToAddress([]byte{2})
	input := []byte("test")
	suppliedGas := uint64(10000)

	out, remainingGas, err := RunPrecompiledContract(addr, input, suppliedGas)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == nil {
		t.Fatal("expected non-nil output")
	}

	expectedGasCost := uint64(60 + 12*1) // 1 word
	if remainingGas != suppliedGas-expectedGasCost {
		t.Errorf("remaining gas = %d, want %d", remainingGas, suppliedGas-expectedGasCost)
	}
}

func TestWordCount(t *testing.T) {
	tests := []struct {
		size int
		want uint64
	}{
		{0, 0},
		{1, 1},
		{31, 1},
		{32, 1},
		{33, 2},
		{64, 2},
		{65, 3},
	}

	for _, tt := range tests {
		if got := wordCount(tt.size); got != tt.want {
			t.Errorf("wordCount(%d) = %d, want %d", tt.size, got, tt.want)
		}
	}
}

// buildModExpInput constructs the 96-byte header + data for the modexp precompile.
func buildModExpInput(base, exp, mod []byte) []byte {
	input := make([]byte, 96+len(base)+len(exp)+len(mod))

	// base_length (32 bytes)
	bLen := big.NewInt(int64(len(base)))
	copy(input[32-len(bLen.Bytes()):32], bLen.Bytes())

	// exp_length (32 bytes)
	eLen := big.NewInt(int64(len(exp)))
	copy(input[64-len(eLen.Bytes()):64], eLen.Bytes())

	// mod_length (32 bytes)
	mLen := big.NewInt(int64(len(mod)))
	copy(input[96-len(mLen.Bytes()):96], mLen.Bytes())

	// base + exp + mod
	offset := 96
	copy(input[offset:], base)
	offset += len(base)
	copy(input[offset:], exp)
	offset += len(exp)
	copy(input[offset:], mod)

	return input
}
