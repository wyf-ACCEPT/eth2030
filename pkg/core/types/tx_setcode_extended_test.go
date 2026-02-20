package types

import (
	"math/big"
	"testing"
)

func TestAuthorizationHash_Deterministic(t *testing.T) {
	auth := &Authorization{
		ChainID: big.NewInt(1),
		Address: HexToAddress("0x1234567890abcdef1234567890abcdef12345678"),
		Nonce:   42,
	}
	h1 := AuthorizationHash(auth)
	h2 := AuthorizationHash(auth)
	if h1 != h2 {
		t.Error("AuthorizationHash should be deterministic")
	}
	if h1.IsZero() {
		t.Error("AuthorizationHash should not be zero")
	}
}

func TestAuthorizationHash_DifferentChainID(t *testing.T) {
	auth1 := &Authorization{
		ChainID: big.NewInt(1),
		Address: HexToAddress("0x1234567890abcdef1234567890abcdef12345678"),
		Nonce:   42,
	}
	auth2 := &Authorization{
		ChainID: big.NewInt(5),
		Address: HexToAddress("0x1234567890abcdef1234567890abcdef12345678"),
		Nonce:   42,
	}
	h1 := AuthorizationHash(auth1)
	h2 := AuthorizationHash(auth2)
	if h1 == h2 {
		t.Error("different chain IDs should produce different hashes")
	}
}

func TestAuthorizationHash_DifferentNonce(t *testing.T) {
	auth1 := &Authorization{
		ChainID: big.NewInt(1),
		Address: HexToAddress("0xdead"),
		Nonce:   0,
	}
	auth2 := &Authorization{
		ChainID: big.NewInt(1),
		Address: HexToAddress("0xdead"),
		Nonce:   1,
	}
	h1 := AuthorizationHash(auth1)
	h2 := AuthorizationHash(auth2)
	if h1 == h2 {
		t.Error("different nonces should produce different hashes")
	}
}

func TestAuthorizationHash_Nil(t *testing.T) {
	h := AuthorizationHash(nil)
	if !h.IsZero() {
		t.Error("nil auth should return zero hash")
	}
}

func TestAuthorizationHash_NilChainID(t *testing.T) {
	auth := &Authorization{
		ChainID: nil,
		Address: HexToAddress("0xdead"),
		Nonce:   1,
	}
	h := AuthorizationHash(auth)
	if h.IsZero() {
		t.Error("nil chainID (treated as 0) should still produce a non-zero hash")
	}
}

func TestValidateAuthorizationSignature_Nil(t *testing.T) {
	if err := ValidateAuthorizationSignature(nil); err != ErrSetCodeInvalidAuthSig {
		t.Errorf("nil auth: want ErrSetCodeInvalidAuthSig, got %v", err)
	}
}

func TestValidateAuthorizationSignature_NilRS(t *testing.T) {
	auth := &Authorization{ChainID: big.NewInt(1), Address: HexToAddress("0xdead")}
	if err := ValidateAuthorizationSignature(auth); err != ErrSetCodeInvalidAuthSig {
		t.Errorf("nil R/S: want ErrSetCodeInvalidAuthSig, got %v", err)
	}
}

func TestValidateAuthorizationSignature_ZeroR(t *testing.T) {
	auth := &Authorization{
		ChainID: big.NewInt(1),
		Address: HexToAddress("0xdead"),
		R:       big.NewInt(0),
		S:       big.NewInt(1),
	}
	if err := ValidateAuthorizationSignature(auth); err != ErrSetCodeInvalidAuthSig {
		t.Errorf("zero R: want ErrSetCodeInvalidAuthSig, got %v", err)
	}
}

func TestValidateAuthorizationSignature_VTooLarge(t *testing.T) {
	auth := &Authorization{
		ChainID: big.NewInt(1),
		Address: HexToAddress("0xdead"),
		R:       big.NewInt(1),
		S:       big.NewInt(1),
		V:       big.NewInt(5),
	}
	if err := ValidateAuthorizationSignature(auth); err != ErrSetCodeInvalidAuthSig {
		t.Errorf("V too large: want ErrSetCodeInvalidAuthSig, got %v", err)
	}
}

func TestValidateAuthorizationSignature_RExceedsOrder(t *testing.T) {
	auth := &Authorization{
		ChainID: big.NewInt(1),
		Address: HexToAddress("0xdead"),
		R:       new(big.Int).Set(secp256k1NCopy), // exactly N
		S:       big.NewInt(1),
		V:       big.NewInt(0),
	}
	if err := ValidateAuthorizationSignature(auth); err != ErrSetCodeInvalidAuthSig {
		t.Errorf("R >= N: want ErrSetCodeInvalidAuthSig, got %v", err)
	}
}

func TestComputeSetCodeIntrinsicGas(t *testing.T) {
	tests := []struct {
		name              string
		data              []byte
		authCount         int
		emptyAccountCount int
		want              uint64
	}{
		{
			name:              "empty tx",
			data:              nil,
			authCount:         1,
			emptyAccountCount: 0,
			want:              21000 + 12500,
		},
		{
			name:              "with calldata",
			data:              []byte{0x00, 0xff, 0x00, 0xff},
			authCount:         1,
			emptyAccountCount: 0,
			want:              21000 + 4 + 16 + 4 + 16 + 12500,
		},
		{
			name:              "multiple auths",
			data:              nil,
			authCount:         3,
			emptyAccountCount: 0,
			want:              21000 + 3*12500,
		},
		{
			name:              "with empty accounts",
			data:              nil,
			authCount:         2,
			emptyAccountCount: 1,
			want:              21000 + 2*12500 + 25000,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeSetCodeIntrinsicGas(tt.data, tt.authCount, tt.emptyAccountCount)
			if got != tt.want {
				t.Errorf("ComputeSetCodeIntrinsicGas() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestIsDelegated(t *testing.T) {
	addr := HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	code := AddressToDelegation(addr)

	got, ok := IsDelegated(code)
	if !ok {
		t.Fatal("should recognize delegation code")
	}
	if got != addr {
		t.Errorf("IsDelegated() = %s, want %s", got.Hex(), addr.Hex())
	}

	// Non-delegation code.
	_, ok = IsDelegated([]byte{0x60, 0x00})
	if ok {
		t.Error("should not recognize non-delegation code")
	}

	// Wrong length.
	_, ok = IsDelegated(append(code, 0x00))
	if ok {
		t.Error("should not recognize wrong-length code")
	}
}

func TestBuildDelegationCode(t *testing.T) {
	addr := HexToAddress("0xaaaa")
	code := BuildDelegationCode(addr)
	if len(code) != DelegationCodeLength {
		t.Errorf("delegation code length = %d, want %d", len(code), DelegationCodeLength)
	}
	parsed, ok := ParseDelegation(code)
	if !ok {
		t.Fatal("BuildDelegationCode should produce parseable delegation")
	}
	if parsed != addr {
		t.Errorf("roundtrip failed: got %s, want %s", parsed.Hex(), addr.Hex())
	}
}

func TestResolveDelegationChain_Direct(t *testing.T) {
	addr := HexToAddress("0xaaaa")
	code := AddressToDelegation(addr)

	// The lookup returns non-delegation code for the target.
	lookup := func(a Address) []byte {
		return []byte{0x60, 0x00} // regular contract code
	}
	target, depth, err := ResolveDelegationChain(code, lookup, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target != addr {
		t.Errorf("target = %s, want %s", target.Hex(), addr.Hex())
	}
	if depth != 1 {
		t.Errorf("depth = %d, want 1", depth)
	}
}

func TestResolveDelegationChain_TwoLevels(t *testing.T) {
	addrA := HexToAddress("0xaaaa")
	addrB := HexToAddress("0xbbbb")
	startCode := AddressToDelegation(addrA)

	lookup := func(a Address) []byte {
		if a == addrA {
			return AddressToDelegation(addrB) // A delegates to B
		}
		return []byte{0x60, 0x00} // B has regular code
	}

	target, depth, err := ResolveDelegationChain(startCode, lookup, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target != addrB {
		t.Errorf("target = %s, want %s", target.Hex(), addrB.Hex())
	}
	if depth != 2 {
		t.Errorf("depth = %d, want 2", depth)
	}
}

func TestResolveDelegationChain_Circular(t *testing.T) {
	addrA := HexToAddress("0xaaaa")
	addrB := HexToAddress("0xbbbb")
	startCode := AddressToDelegation(addrA)

	lookup := func(a Address) []byte {
		if a == addrA {
			return AddressToDelegation(addrB)
		}
		return AddressToDelegation(addrA) // B -> A = cycle
	}

	_, _, err := ResolveDelegationChain(startCode, lookup, 10)
	if err == nil {
		t.Fatal("expected error for circular delegation")
	}
}

func TestResolveDelegationChain_MaxDepth(t *testing.T) {
	addr := HexToAddress("0xaaaa")
	startCode := AddressToDelegation(addr)

	// Each address delegates to the next one.
	counter := byte(0)
	lookup := func(a Address) []byte {
		counter++
		next := Address{}
		next[19] = counter
		return AddressToDelegation(next)
	}

	_, _, err := ResolveDelegationChain(startCode, lookup, 3)
	if err == nil {
		t.Fatal("expected error for exceeding max depth")
	}
}

func TestResolveDelegationChain_NotDelegation(t *testing.T) {
	_, _, err := ResolveDelegationChain([]byte{0x60, 0x00}, nil, 10)
	if err == nil {
		t.Fatal("expected error for non-delegation code")
	}
}

func TestNewAuthorization(t *testing.T) {
	auth := NewAuthorization(big.NewInt(1), HexToAddress("0xdead"), 42)
	if auth.ChainID.Int64() != 1 {
		t.Errorf("ChainID = %s, want 1", auth.ChainID)
	}
	if auth.Address != HexToAddress("0xdead") {
		t.Error("Address mismatch")
	}
	if auth.Nonce != 42 {
		t.Errorf("Nonce = %d, want 42", auth.Nonce)
	}
	if auth.V != nil || auth.R != nil || auth.S != nil {
		t.Error("signature fields should be nil")
	}
}

func TestNewAuthorization_NilChainID(t *testing.T) {
	auth := NewAuthorization(nil, HexToAddress("0xbeef"), 0)
	if auth.ChainID != nil {
		t.Error("ChainID should be nil when nil is passed")
	}
}

func TestAuthorizationListGas(t *testing.T) {
	tests := []struct {
		count int
		want  uint64
	}{
		{0, 0},
		{1, 12500},
		{5, 62500},
		{256, 256 * 12500},
	}
	for _, tt := range tests {
		got := AuthorizationListGas(tt.count)
		if got != tt.want {
			t.Errorf("AuthorizationListGas(%d) = %d, want %d", tt.count, got, tt.want)
		}
	}
}

func TestSetCodeConstants(t *testing.T) {
	if MaxAuthorizationListSize != 256 {
		t.Errorf("MaxAuthorizationListSize = %d, want 256", MaxAuthorizationListSize)
	}
	if DelegationCodeLength != 23 {
		t.Errorf("DelegationCodeLength = %d, want 23", DelegationCodeLength)
	}
	if SetCodeTxIntrinsicGas != 21000 {
		t.Errorf("SetCodeTxIntrinsicGas = %d, want 21000", SetCodeTxIntrinsicGas)
	}
}
