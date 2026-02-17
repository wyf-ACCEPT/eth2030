package core

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

// --- IsDelegated tests ---

func TestIsDelegated_ValidDelegation(t *testing.T) {
	// Valid delegation code: 0xef0100 + 20 bytes address
	code := make([]byte, 23)
	code[0] = 0xef
	code[1] = 0x01
	code[2] = 0x00
	// Fill address bytes with a recognizable pattern
	for i := 3; i < 23; i++ {
		code[i] = byte(i)
	}

	if !IsDelegated(code) {
		t.Error("IsDelegated should return true for valid delegation code")
	}
}

func TestIsDelegated_EmptyCode(t *testing.T) {
	if IsDelegated(nil) {
		t.Error("IsDelegated should return false for nil code")
	}
	if IsDelegated([]byte{}) {
		t.Error("IsDelegated should return false for empty code")
	}
}

func TestIsDelegated_TooShort(t *testing.T) {
	// Only 2 bytes - shorter than the prefix
	code := []byte{0xef, 0x01}
	if IsDelegated(code) {
		t.Error("IsDelegated should return false for code shorter than prefix")
	}
}

func TestIsDelegated_WrongPrefix(t *testing.T) {
	// Wrong first byte
	code := make([]byte, 23)
	code[0] = 0xff
	code[1] = 0x01
	code[2] = 0x00
	if IsDelegated(code) {
		t.Error("IsDelegated should return false for code with wrong prefix byte 0")
	}

	// Wrong second byte
	code[0] = 0xef
	code[1] = 0x02
	if IsDelegated(code) {
		t.Error("IsDelegated should return false for code with wrong prefix byte 1")
	}

	// Wrong third byte
	code[1] = 0x01
	code[2] = 0x01
	if IsDelegated(code) {
		t.Error("IsDelegated should return false for code with wrong prefix byte 2")
	}
}

func TestIsDelegated_PrefixOnly(t *testing.T) {
	// Just the 3-byte prefix with no address - still starts with the prefix
	code := []byte{0xef, 0x01, 0x00}
	if !IsDelegated(code) {
		t.Error("IsDelegated should return true for code that has the prefix (even without full address)")
	}
}

func TestIsDelegated_LongerCode(t *testing.T) {
	// Code longer than 23 bytes but still starts with delegation prefix
	code := make([]byte, 50)
	code[0] = 0xef
	code[1] = 0x01
	code[2] = 0x00
	if !IsDelegated(code) {
		t.Error("IsDelegated should return true for code starting with delegation prefix regardless of length")
	}
}

func TestIsDelegated_RegularContractCode(t *testing.T) {
	// Regular EVM bytecode (PUSH1 0x60 PUSH1 0x40 ...)
	code := []byte{0x60, 0x80, 0x60, 0x40, 0x52}
	if IsDelegated(code) {
		t.Error("IsDelegated should return false for regular contract code")
	}
}

// --- ResolveDelegation tests ---

func TestResolveDelegation_Valid(t *testing.T) {
	targetAddr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	code := makeDelegationCode(targetAddr)

	resolved, ok := ResolveDelegation(code)
	if !ok {
		t.Fatal("ResolveDelegation should return true for valid delegation code")
	}
	if resolved != targetAddr {
		t.Errorf("ResolveDelegation returned wrong address: got %v, want %v", resolved.Hex(), targetAddr.Hex())
	}
}

func TestResolveDelegation_ZeroAddress(t *testing.T) {
	targetAddr := types.Address{}
	code := makeDelegationCode(targetAddr)

	resolved, ok := ResolveDelegation(code)
	if !ok {
		t.Fatal("ResolveDelegation should return true for delegation to zero address")
	}
	if resolved != targetAddr {
		t.Errorf("ResolveDelegation returned wrong address: got %v, want %v", resolved.Hex(), targetAddr.Hex())
	}
}

func TestResolveDelegation_EmptyCode(t *testing.T) {
	_, ok := ResolveDelegation(nil)
	if ok {
		t.Error("ResolveDelegation should return false for nil code")
	}
	_, ok = ResolveDelegation([]byte{})
	if ok {
		t.Error("ResolveDelegation should return false for empty code")
	}
}

func TestResolveDelegation_TooShort(t *testing.T) {
	// Only prefix, no address
	code := []byte{0xef, 0x01, 0x00}
	_, ok := ResolveDelegation(code)
	if ok {
		t.Error("ResolveDelegation should return false for code that is too short (prefix only)")
	}
}

func TestResolveDelegation_TooLong(t *testing.T) {
	// 24 bytes - one byte too long
	code := make([]byte, 24)
	code[0] = 0xef
	code[1] = 0x01
	code[2] = 0x00
	_, ok := ResolveDelegation(code)
	if ok {
		t.Error("ResolveDelegation should return false for code that is too long")
	}
}

func TestResolveDelegation_WrongPrefix(t *testing.T) {
	code := make([]byte, 23)
	code[0] = 0xff
	code[1] = 0x01
	code[2] = 0x00
	_, ok := ResolveDelegation(code)
	if ok {
		t.Error("ResolveDelegation should return false for code with wrong prefix")
	}
}

func TestResolveDelegation_RegularCode(t *testing.T) {
	code := []byte{0x60, 0x80, 0x60, 0x40, 0x52}
	_, ok := ResolveDelegation(code)
	if ok {
		t.Error("ResolveDelegation should return false for regular contract code")
	}
}

// --- makeDelegationCode tests ---

func TestMakeDelegationCode_Structure(t *testing.T) {
	addr := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	code := makeDelegationCode(addr)

	if len(code) != 23 {
		t.Fatalf("delegation code should be 23 bytes, got %d", len(code))
	}

	// Check prefix
	if !bytes.Equal(code[:3], []byte{0xef, 0x01, 0x00}) {
		t.Errorf("delegation code should start with 0xef0100, got %x", code[:3])
	}

	// Check address
	var extractedAddr types.Address
	copy(extractedAddr[:], code[3:])
	if extractedAddr != addr {
		t.Errorf("delegation code should contain target address, got %v", extractedAddr.Hex())
	}
}

// --- RLP encoding tests ---

func TestEncodeUint64RLP(t *testing.T) {
	tests := []struct {
		name string
		val  uint64
		want []byte
	}{
		{"zero", 0, []byte{0x80}},
		{"one", 1, []byte{0x01}},
		{"small_value", 127, []byte{0x7f}},
		{"single_byte_boundary", 128, []byte{0x81, 0x80}},
		{"two_fifty_five", 255, []byte{0x81, 0xff}},
		{"two_fifty_six", 256, []byte{0x82, 0x01, 0x00}},
		{"large_value", 1024, []byte{0x82, 0x04, 0x00}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := encodeUint64RLP(tt.val)
			if !bytes.Equal(got, tt.want) {
				t.Errorf("encodeUint64RLP(%d) = %x, want %x", tt.val, got, tt.want)
			}
		})
	}
}

func TestEncodeBigIntRLP(t *testing.T) {
	tests := []struct {
		name string
		val  *big.Int
		want []byte
	}{
		{"nil", nil, []byte{0x80}},
		{"zero", big.NewInt(0), []byte{0x80}},
		{"one", big.NewInt(1), []byte{0x01}},
		{"small", big.NewInt(127), []byte{0x7f}},
		{"boundary", big.NewInt(128), []byte{0x81, 0x80}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := encodeBigIntRLP(tt.val)
			if !bytes.Equal(got, tt.want) {
				t.Errorf("encodeBigIntRLP(%v) = %x, want %x", tt.val, got, tt.want)
			}
		})
	}
}

// --- ProcessAuthorizations tests ---

func TestProcessAuthorizations_EmptyList(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	err := ProcessAuthorizations(statedb, nil, big.NewInt(1))
	if err != nil {
		t.Fatalf("ProcessAuthorizations should succeed with nil list: %v", err)
	}

	err = ProcessAuthorizations(statedb, []types.Authorization{}, big.NewInt(1))
	if err != nil {
		t.Fatalf("ProcessAuthorizations should succeed with empty list: %v", err)
	}
}

func TestProcessAuthorizations_ChainIDMismatch(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	// Authorization with chain ID 2 on chain 1 should be skipped (not error)
	auths := []types.Authorization{
		{
			ChainID: big.NewInt(2),
			Address: types.HexToAddress("0x1111111111111111111111111111111111111111"),
			Nonce:   0,
			V:       big.NewInt(0),
			R:       big.NewInt(1),
			S:       big.NewInt(1),
		},
	}

	err := ProcessAuthorizations(statedb, auths, big.NewInt(1))
	if err != nil {
		t.Fatalf("ProcessAuthorizations should not error on chain ID mismatch (should skip): %v", err)
	}
}

func TestProcessAuthorizations_ZeroChainIDAcceptsAnyChain(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	// Authorization with chain ID 0 should be accepted on any chain.
	// The signature recovery will fail (ecrecover not implemented), so
	// the authorization will be skipped due to that, but not due to chain ID.
	auths := []types.Authorization{
		{
			ChainID: big.NewInt(0),
			Address: types.HexToAddress("0x1111111111111111111111111111111111111111"),
			Nonce:   0,
			V:       big.NewInt(0),
			R:       big.NewInt(1),
			S:       big.NewInt(1),
		},
	}

	// Should not error (invalid authorizations are skipped, not fatal)
	err := ProcessAuthorizations(statedb, auths, big.NewInt(42))
	if err != nil {
		t.Fatalf("ProcessAuthorizations should not error: %v", err)
	}
}

func TestProcessAuthorizations_NilChainIDAcceptsAnyChain(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	auths := []types.Authorization{
		{
			ChainID: nil, // nil treated as zero
			Address: types.HexToAddress("0x1111111111111111111111111111111111111111"),
			Nonce:   0,
			V:       big.NewInt(0),
			R:       big.NewInt(1),
			S:       big.NewInt(1),
		},
	}

	err := ProcessAuthorizations(statedb, auths, big.NewInt(42))
	if err != nil {
		t.Fatalf("ProcessAuthorizations should not error: %v", err)
	}
}

func TestProcessAuthorizations_InvalidVValue(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	// V > 1 should be rejected
	auths := []types.Authorization{
		{
			ChainID: big.NewInt(1),
			Address: types.HexToAddress("0x1111111111111111111111111111111111111111"),
			Nonce:   0,
			V:       big.NewInt(28), // invalid: must be 0 or 1
			R:       big.NewInt(1),
			S:       big.NewInt(1),
		},
	}

	err := ProcessAuthorizations(statedb, auths, big.NewInt(1))
	if err != nil {
		t.Fatalf("ProcessAuthorizations should not error (invalid auths are skipped): %v", err)
	}
}

func TestProcessAuthorizations_NilRorS(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	// nil R and S should be rejected by ValidateSignatureValues
	auths := []types.Authorization{
		{
			ChainID: big.NewInt(1),
			Address: types.HexToAddress("0x1111111111111111111111111111111111111111"),
			Nonce:   0,
			V:       big.NewInt(0),
			R:       nil,
			S:       nil,
		},
	}

	err := ProcessAuthorizations(statedb, auths, big.NewInt(1))
	if err != nil {
		t.Fatalf("ProcessAuthorizations should not error (invalid auths are skipped): %v", err)
	}
}

func TestProcessAuthorizations_MatchingChainID(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	// Matching chain ID should pass chain ID check (signature will fail)
	auths := []types.Authorization{
		{
			ChainID: big.NewInt(1),
			Address: types.HexToAddress("0x1111111111111111111111111111111111111111"),
			Nonce:   0,
			V:       big.NewInt(0),
			R:       big.NewInt(1),
			S:       big.NewInt(1),
		},
	}

	// Should not error (signature recovery failure causes skip, not error)
	err := ProcessAuthorizations(statedb, auths, big.NewInt(1))
	if err != nil {
		t.Fatalf("ProcessAuthorizations should not error: %v", err)
	}
}

func TestProcessAuthorizations_MultipleAuthorizations(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	// Multiple authorizations: some invalid, should all be skipped gracefully
	auths := []types.Authorization{
		{
			ChainID: big.NewInt(999), // wrong chain
			Address: types.HexToAddress("0x1111111111111111111111111111111111111111"),
			Nonce:   0,
			V:       big.NewInt(0),
			R:       big.NewInt(1),
			S:       big.NewInt(1),
		},
		{
			ChainID: big.NewInt(1), // right chain, but ecrecover fails
			Address: types.HexToAddress("0x2222222222222222222222222222222222222222"),
			Nonce:   0,
			V:       big.NewInt(0),
			R:       big.NewInt(1),
			S:       big.NewInt(1),
		},
		{
			ChainID: big.NewInt(0), // any-chain, but ecrecover fails
			Address: types.HexToAddress("0x3333333333333333333333333333333333333333"),
			Nonce:   0,
			V:       big.NewInt(0),
			R:       big.NewInt(1),
			S:       big.NewInt(1),
		},
	}

	err := ProcessAuthorizations(statedb, auths, big.NewInt(1))
	if err != nil {
		t.Fatalf("ProcessAuthorizations should not error with multiple invalid auths: %v", err)
	}
}

// --- processOneAuthorization unit tests ---

func TestProcessOneAuthorization_ChainIDMismatch(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	auth := &types.Authorization{
		ChainID: big.NewInt(5),
		Address: types.HexToAddress("0x1111111111111111111111111111111111111111"),
		Nonce:   0,
		V:       big.NewInt(0),
		R:       big.NewInt(1),
		S:       big.NewInt(1),
	}

	err := processOneAuthorization(statedb, auth, big.NewInt(1))
	if err == nil {
		t.Fatal("processOneAuthorization should error on chain ID mismatch")
	}
	if err != ErrAuthChainID {
		t.Errorf("expected ErrAuthChainID, got: %v", err)
	}
}

func TestProcessOneAuthorization_ZeroChainIDPassesCheck(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	auth := &types.Authorization{
		ChainID: big.NewInt(0),
		Address: types.HexToAddress("0x1111111111111111111111111111111111111111"),
		Nonce:   0,
		V:       big.NewInt(0),
		R:       big.NewInt(1),
		S:       big.NewInt(1),
	}

	err := processOneAuthorization(statedb, auth, big.NewInt(42))
	// Should fail on ecrecover, not on chain ID
	if err == ErrAuthChainID {
		t.Error("processOneAuthorization should not fail on chain ID for zero chain ID")
	}
}

func TestProcessOneAuthorization_InvalidSigValues(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	auth := &types.Authorization{
		ChainID: big.NewInt(1),
		Address: types.HexToAddress("0x1111111111111111111111111111111111111111"),
		Nonce:   0,
		V:       big.NewInt(5), // invalid V
		R:       big.NewInt(1),
		S:       big.NewInt(1),
	}

	err := processOneAuthorization(statedb, auth, big.NewInt(1))
	if err != ErrAuthInvalidSig {
		t.Errorf("expected ErrAuthInvalidSig, got: %v", err)
	}
}

// --- Roundtrip tests ---

func TestDelegationCodeRoundtrip(t *testing.T) {
	addresses := []types.Address{
		types.HexToAddress("0x0000000000000000000000000000000000000000"),
		types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678"),
		types.HexToAddress("0xffffffffffffffffffffffffffffffffffffffff"),
		types.HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"),
	}

	for _, addr := range addresses {
		t.Run(addr.Hex(), func(t *testing.T) {
			code := makeDelegationCode(addr)

			if !IsDelegated(code) {
				t.Fatal("IsDelegated should return true for generated delegation code")
			}

			resolved, ok := ResolveDelegation(code)
			if !ok {
				t.Fatal("ResolveDelegation should return true for generated delegation code")
			}
			if resolved != addr {
				t.Errorf("roundtrip failed: got %v, want %v", resolved.Hex(), addr.Hex())
			}
		})
	}
}

// --- Authorization hash tests ---

func TestComputeAuthorizationHash_Deterministic(t *testing.T) {
	auth := &types.Authorization{
		ChainID: big.NewInt(1),
		Address: types.HexToAddress("0x1111111111111111111111111111111111111111"),
		Nonce:   42,
	}

	hash1 := computeAuthorizationHash(auth)
	hash2 := computeAuthorizationHash(auth)

	if !bytes.Equal(hash1, hash2) {
		t.Error("computeAuthorizationHash should be deterministic")
	}

	if len(hash1) != 32 {
		t.Errorf("authorization hash should be 32 bytes, got %d", len(hash1))
	}
}

func TestComputeAuthorizationHash_DifferentInputs(t *testing.T) {
	auth1 := &types.Authorization{
		ChainID: big.NewInt(1),
		Address: types.HexToAddress("0x1111111111111111111111111111111111111111"),
		Nonce:   0,
	}
	auth2 := &types.Authorization{
		ChainID: big.NewInt(1),
		Address: types.HexToAddress("0x2222222222222222222222222222222222222222"),
		Nonce:   0,
	}
	auth3 := &types.Authorization{
		ChainID: big.NewInt(1),
		Address: types.HexToAddress("0x1111111111111111111111111111111111111111"),
		Nonce:   1,
	}
	auth4 := &types.Authorization{
		ChainID: big.NewInt(2),
		Address: types.HexToAddress("0x1111111111111111111111111111111111111111"),
		Nonce:   0,
	}

	hash1 := computeAuthorizationHash(auth1)
	hash2 := computeAuthorizationHash(auth2)
	hash3 := computeAuthorizationHash(auth3)
	hash4 := computeAuthorizationHash(auth4)

	if bytes.Equal(hash1, hash2) {
		t.Error("different addresses should produce different hashes")
	}
	if bytes.Equal(hash1, hash3) {
		t.Error("different nonces should produce different hashes")
	}
	if bytes.Equal(hash1, hash4) {
		t.Error("different chain IDs should produce different hashes")
	}
}
