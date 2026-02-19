package vm

import (
	"bytes"
	"encoding/binary"
	"math/big"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// makeTestAuth creates a signed EIP-7702 authorization for testing.
func makeTestAuth(t *testing.T, chainID, nonce uint64, target types.Address) *Authorization7702 {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	msg := make([]byte, 1+8+20+8)
	msg[0] = types.AuthMagic
	binary.BigEndian.PutUint64(msg[1:9], chainID)
	copy(msg[9:29], target[:])
	binary.BigEndian.PutUint64(msg[29:37], nonce)
	hash := crypto.Keccak256(msg)
	sig, err := crypto.Sign(hash, key)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	return &Authorization7702{
		ChainID: chainID, Address: target, Nonce: nonce,
		V: []byte{sig[64]}, R: sig[0:32], S: sig[32:64],
	}
}

// buildPrecompileInput builds the full precompile input for a slice of authorizations.
func buildPrecompileInput(t *testing.T, auths []*Authorization7702) []byte {
	t.Helper()
	input := make([]byte, 32)
	binary.BigEndian.PutUint64(input[24:32], uint64(len(auths)))
	for _, auth := range auths {
		encoded, err := EncodeAuthorization(auth)
		if err != nil {
			t.Fatalf("EncodeAuthorization: %v", err)
		}
		input = append(input, encoded...)
	}
	return input
}

func TestParseAuthorization(t *testing.T) {
	target := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	auth := &Authorization7702{
		ChainID: 1, Address: target, Nonce: 42,
		V: []byte{0}, R: make([]byte, 32), S: make([]byte, 32),
	}
	auth.R[31] = 1
	auth.S[31] = 1

	encoded, err := EncodeAuthorization(auth)
	if err != nil {
		t.Fatalf("EncodeAuthorization: %v", err)
	}
	parsed, err := ParseAuthorization(encoded)
	if err != nil {
		t.Fatalf("ParseAuthorization: %v", err)
	}
	if parsed.ChainID != 1 || parsed.Address != target || parsed.Nonce != 42 {
		t.Error("field mismatch after parse")
	}
	if parsed.V[0] != 0 || !bytes.Equal(parsed.R, auth.R) || !bytes.Equal(parsed.S, auth.S) {
		t.Error("signature field mismatch after parse")
	}
}

func TestParseAuthorization_TooShort(t *testing.T) {
	if _, err := ParseAuthorization(make([]byte, 10)); err != ErrEIP7702InputTooShort {
		t.Errorf("expected ErrEIP7702InputTooShort, got %v", err)
	}
}

func TestValidateAuthorization(t *testing.T) {
	target := types.HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

	// Valid authorization.
	auth := makeTestAuth(t, 1, 0, target)
	if err := ValidateAuthorization(auth, types.Address{}); err != nil {
		t.Errorf("expected valid, got %v", err)
	}

	// Invalid V.
	bad := makeTestAuth(t, 1, 0, target)
	bad.V = []byte{2}
	if err := ValidateAuthorization(bad, types.Address{}); err != ErrEIP7702InvalidV {
		t.Errorf("expected ErrEIP7702InvalidV, got %v", err)
	}

	// Zero R.
	bad2 := makeTestAuth(t, 1, 0, target)
	bad2.R = make([]byte, 32)
	if err := ValidateAuthorization(bad2, types.Address{}); err != ErrEIP7702InvalidSignature {
		t.Errorf("expected ErrEIP7702InvalidSignature for zero R, got %v", err)
	}

	// Zero S.
	bad3 := makeTestAuth(t, 1, 0, target)
	bad3.S = make([]byte, 32)
	if err := ValidateAuthorization(bad3, types.Address{}); err != ErrEIP7702InvalidSignature {
		t.Errorf("expected ErrEIP7702InvalidSignature for zero S, got %v", err)
	}

	// Zero address.
	bad4 := makeTestAuth(t, 1, 0, types.Address{})
	bad4.Address = types.Address{}
	if err := ValidateAuthorization(bad4, types.Address{}); err != ErrEIP7702ZeroAddress {
		t.Errorf("expected ErrEIP7702ZeroAddress, got %v", err)
	}

	// Nil authorization.
	if err := ValidateAuthorization(nil, types.Address{}); err != ErrEIP7702InputTooShort {
		t.Errorf("expected ErrEIP7702InputTooShort for nil, got %v", err)
	}
}

func TestRecoverSigner(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	expectedAddr := crypto.PubkeyToAddress(key.PublicKey)
	target := types.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")

	msg := make([]byte, 1+8+20+8)
	msg[0] = types.AuthMagic
	binary.BigEndian.PutUint64(msg[1:9], 1)
	copy(msg[9:29], target[:])
	binary.BigEndian.PutUint64(msg[29:37], 7)

	hash := crypto.Keccak256(msg)
	sig, err := crypto.Sign(hash, key)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	auth := &Authorization7702{
		ChainID: 1, Address: target, Nonce: 7,
		V: []byte{sig[64]}, R: sig[0:32], S: sig[32:64],
	}
	recovered, err := RecoverSigner(auth)
	if err != nil {
		t.Fatalf("RecoverSigner: %v", err)
	}
	if recovered != expectedAddr {
		t.Errorf("recovered %s, want %s", recovered, expectedAddr)
	}
}

func TestRecoverSigner_BadSignature(t *testing.T) {
	auth := &Authorization7702{
		ChainID: 1,
		Address: types.HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"),
		V: []byte{0}, R: make([]byte, 32), S: make([]byte, 32),
	}
	auth.R[0] = 0xFF
	auth.S[0] = 0xFF
	if _, err := RecoverSigner(auth); err == nil {
		t.Error("expected error for bad signature, got nil")
	}
}

func TestEncodeAuthorization(t *testing.T) {
	auth := &Authorization7702{
		ChainID: 42,
		Address: types.HexToAddress("0x1111111111111111111111111111111111111111"),
		Nonce: 99, V: []byte{1}, R: make([]byte, 32), S: make([]byte, 32),
	}
	auth.R[31] = 0xAB
	auth.S[31] = 0xCD

	encoded, err := EncodeAuthorization(auth)
	if err != nil {
		t.Fatalf("EncodeAuthorization: %v", err)
	}
	if len(encoded) != authorizationEncodedSize {
		t.Fatalf("encoded length = %d, want %d", len(encoded), authorizationEncodedSize)
	}
	if binary.BigEndian.Uint64(encoded[0:8]) != 42 {
		t.Error("chainID mismatch")
	}
	if binary.BigEndian.Uint64(encoded[28:36]) != 99 {
		t.Error("nonce mismatch")
	}
	if encoded[36] != 1 {
		t.Error("v mismatch")
	}
}

func TestEncodeAuthorization_Errors(t *testing.T) {
	if _, err := EncodeAuthorization(nil); err != ErrEIP7702InputTooShort {
		t.Errorf("nil: expected ErrEIP7702InputTooShort, got %v", err)
	}
	auth := &Authorization7702{V: []byte{0}, R: []byte{1}, S: make([]byte, 32)}
	if _, err := EncodeAuthorization(auth); err != ErrEIP7702InvalidSignature {
		t.Errorf("bad R len: expected ErrEIP7702InvalidSignature, got %v", err)
	}
}

func TestSetCode7702(t *testing.T) {
	target := types.HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	addr, err := SetCode7702(&Authorization7702{Address: target})
	if err != nil {
		t.Fatalf("SetCode7702: %v", err)
	}
	if addr != target {
		t.Errorf("got %s, want %s", addr, target)
	}

	if _, err := SetCode7702(nil); err != ErrEIP7702InputTooShort {
		t.Errorf("nil: expected ErrEIP7702InputTooShort, got %v", err)
	}
	if _, err := SetCode7702(&Authorization7702{}); err != ErrEIP7702ZeroAddress {
		t.Errorf("zero addr: expected ErrEIP7702ZeroAddress, got %v", err)
	}
}

func TestExecute7702_SingleAuth(t *testing.T) {
	target := types.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")
	auth := makeTestAuth(t, 1, 0, target)
	p := &EIP7702Precompile{ChainID: 1}
	input := buildPrecompileInput(t, []*Authorization7702{auth})

	gas := EIP7702BaseGas + EIP7702PerAuthGas
	output, remaining, err := p.Execute7702(input, gas)
	if err != nil {
		t.Fatalf("Execute7702: %v", err)
	}
	if remaining != 0 {
		t.Errorf("remaining gas = %d, want 0", remaining)
	}
	if len(output) != 72 {
		t.Fatalf("output length = %d, want 72", len(output))
	}
	if binary.BigEndian.Uint64(output[24:32]) != 1 {
		t.Error("output count mismatch")
	}
	var outTarget types.Address
	copy(outTarget[:], output[52:72])
	if outTarget != target {
		t.Errorf("output target = %s, want %s", outTarget, target)
	}
}

func TestExecute7702_MultipleAuths(t *testing.T) {
	t1 := types.HexToAddress("0x1111111111111111111111111111111111111111")
	t2 := types.HexToAddress("0x2222222222222222222222222222222222222222")
	p := &EIP7702Precompile{ChainID: 1}
	input := buildPrecompileInput(t, []*Authorization7702{
		makeTestAuth(t, 1, 0, t1), makeTestAuth(t, 1, 1, t2),
	})

	gas := EIP7702BaseGas + 2*EIP7702PerAuthGas
	output, remaining, err := p.Execute7702(input, gas)
	if err != nil {
		t.Fatalf("Execute7702: %v", err)
	}
	if remaining != 0 {
		t.Errorf("remaining gas = %d, want 0", remaining)
	}
	if len(output) != 112 {
		t.Fatalf("output length = %d, want 112", len(output))
	}
	if binary.BigEndian.Uint64(output[24:32]) != 2 {
		t.Error("output count mismatch")
	}
}

func TestExecute7702_Errors(t *testing.T) {
	p := &EIP7702Precompile{ChainID: 1}

	// Out of gas.
	target := types.HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	auth := makeTestAuth(t, 1, 0, target)
	input := buildPrecompileInput(t, []*Authorization7702{auth})
	if _, _, err := p.Execute7702(input, 100); err != ErrOutOfGas {
		t.Errorf("expected ErrOutOfGas, got %v", err)
	}

	// Empty input.
	if _, _, err := p.Execute7702(nil, 100000); err != ErrEIP7702InputTooShort {
		t.Errorf("expected ErrEIP7702InputTooShort, got %v", err)
	}

	// Zero count.
	if _, _, err := p.Execute7702(make([]byte, 32), 100000); err != ErrEIP7702ZeroCount {
		t.Errorf("expected ErrEIP7702ZeroCount, got %v", err)
	}

	// Count exceeds data.
	shortInput := make([]byte, 32)
	binary.BigEndian.PutUint64(shortInput[24:32], 5)
	if _, _, err := p.Execute7702(shortInput, 1000000); err != ErrEIP7702InputMismatch {
		t.Errorf("expected ErrEIP7702InputMismatch, got %v", err)
	}
}

func TestRequiredGas(t *testing.T) {
	p := &EIP7702Precompile{}
	if gas := p.RequiredGas(nil); gas != EIP7702BaseGas {
		t.Errorf("RequiredGas(nil) = %d, want %d", gas, EIP7702BaseGas)
	}
	input := make([]byte, 32)
	binary.BigEndian.PutUint64(input[24:32], 3)
	if gas := p.RequiredGas(input); gas != EIP7702BaseGas+3*EIP7702PerAuthGas {
		t.Errorf("RequiredGas(3) = %d, want %d", gas, EIP7702BaseGas+3*EIP7702PerAuthGas)
	}
}

func TestPrecompiledContractInterface(t *testing.T) {
	var _ PrecompiledContract = (*EIP7702Precompile)(nil)
}

func TestRun_DelegatesToExecute(t *testing.T) {
	target := types.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")
	auth := makeTestAuth(t, 1, 0, target)
	p := &EIP7702Precompile{ChainID: 1}
	output, err := p.Run(buildPrecompileInput(t, []*Authorization7702{auth}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(output) != 72 {
		t.Fatalf("output length = %d, want 72", len(output))
	}
}

func TestExecute7702_ThreadSafety(t *testing.T) {
	target := types.HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	p := &EIP7702Precompile{ChainID: 1}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			auth := makeTestAuth(t, 1, 0, target)
			input := buildPrecompileInput(t, []*Authorization7702{auth})
			if _, _, err := p.Execute7702(input, EIP7702BaseGas+EIP7702PerAuthGas); err != nil {
				t.Errorf("concurrent Execute7702: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestGasCostConstants(t *testing.T) {
	if EIP7702BaseGas != 25000 {
		t.Errorf("EIP7702BaseGas = %d, want 25000", EIP7702BaseGas)
	}
	if EIP7702PerAuthGas != 2600 {
		t.Errorf("EIP7702PerAuthGas = %d, want 2600", EIP7702PerAuthGas)
	}
}

func TestEncodeDecodeRoundtrip(t *testing.T) {
	target := types.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")
	auth := makeTestAuth(t, 42, 7, target)
	encoded, err := EncodeAuthorization(auth)
	if err != nil {
		t.Fatalf("EncodeAuthorization: %v", err)
	}
	decoded, err := ParseAuthorization(encoded)
	if err != nil {
		t.Fatalf("ParseAuthorization: %v", err)
	}
	if decoded.ChainID != 42 || decoded.Address != target || decoded.Nonce != 7 {
		t.Error("field mismatch after roundtrip")
	}
	if !bytes.Equal(decoded.R, auth.R) || !bytes.Equal(decoded.S, auth.S) || decoded.V[0] != auth.V[0] {
		t.Error("signature mismatch after roundtrip")
	}
	s1, _ := RecoverSigner(auth)
	s2, _ := RecoverSigner(decoded)
	if s1 != s2 {
		t.Errorf("signer mismatch: %s != %s", s1, s2)
	}
}

func TestValidateAuthorization_HighSValue(t *testing.T) {
	target := types.HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	auth := makeTestAuth(t, 1, 0, target)
	n, _ := new(big.Int).SetString("fffffffffffffffffffffffffffffffebaaedce6af48a03bbfd25e8cd0364141", 16)
	highS := new(big.Int).Add(new(big.Int).Div(n, big.NewInt(2)), big.NewInt(1))
	auth.S = make([]byte, 32)
	sBytes := highS.Bytes()
	copy(auth.S[32-len(sBytes):], sBytes)
	if err := ValidateAuthorization(auth, types.Address{}); err != ErrEIP7702InvalidSignature {
		t.Errorf("expected ErrEIP7702InvalidSignature for high S, got %v", err)
	}
}

func TestEIP7702PrecompileAddr(t *testing.T) {
	if EIP7702PrecompileAddr != types.BytesToAddress([]byte{0x0a}) {
		t.Errorf("address mismatch: %s", EIP7702PrecompileAddr)
	}
}
