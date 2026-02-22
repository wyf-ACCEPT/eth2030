package core

import (
	"bytes"
	"crypto/ecdsa"
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
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

// --- EIP-7702 intrinsic gas tests ---

func TestIntrinsicGas_NoAuthorizations(t *testing.T) {
	// Without authorizations, the intrinsic gas should match the old behavior.
	data := []byte{0x00, 0x01, 0x00, 0xff}
	isCreate := false

	gas := intrinsicGas(data, isCreate, false, 0, 0)
	// TxGas(21000) + 2*TxDataZeroGas(4) + 2*TxDataNonZeroGas(16) = 21040
	expected := TxGas + 2*TxDataZeroGas + 2*TxDataNonZeroGas
	if gas != expected {
		t.Errorf("intrinsicGas without auths: got %d, want %d", gas, expected)
	}
}

func TestIntrinsicGas_WithAuthorizations(t *testing.T) {
	data := []byte{}
	isCreate := false

	// 3 authorizations, 0 empty accounts
	gas := intrinsicGas(data, isCreate, false, 3, 0)
	expected := TxGas + 3*PerAuthBaseCost
	if gas != expected {
		t.Errorf("intrinsicGas with 3 auths, 0 empty: got %d, want %d", gas, expected)
	}

	// 3 authorizations, 2 empty accounts
	gas = intrinsicGas(data, isCreate, false, 3, 2)
	expected = TxGas + 3*PerAuthBaseCost + 2*PerEmptyAccountCost
	if gas != expected {
		t.Errorf("intrinsicGas with 3 auths, 2 empty: got %d, want %d", gas, expected)
	}
}

func TestIntrinsicGas_SingleAuthEmptyAccount(t *testing.T) {
	data := []byte{}
	gas := intrinsicGas(data, false, false, 1, 1)
	// TxGas + PerAuthBaseCost + PerEmptyAccountCost
	expected := TxGas + PerAuthBaseCost + PerEmptyAccountCost
	if gas != expected {
		t.Errorf("intrinsicGas with 1 auth, 1 empty: got %d, want %d", gas, expected)
	}
}

func TestIntrinsicGas_AuthWithDataAndCreate(t *testing.T) {
	data := []byte{0x60, 0x80, 0x60, 0x40} // 4 non-zero bytes
	gas := intrinsicGas(data, true, false, 2, 1)
	expected := TxGas + TxCreateGas + 4*TxDataNonZeroGas + 2*PerAuthBaseCost + 1*PerEmptyAccountCost
	if gas != expected {
		t.Errorf("intrinsicGas with create+auth: got %d, want %d", gas, expected)
	}
}

// --- SetCode tx processing integration tests ---

// signAuthorization creates a properly signed EIP-7702 authorization.
func signAuthorization(privKey *ecdsa.PrivateKey, chainID *big.Int, targetAddr types.Address, nonce uint64) types.Authorization {
	auth := types.Authorization{
		ChainID: chainID,
		Address: targetAddr,
		Nonce:   nonce,
	}

	authHash := computeAuthorizationHash(&auth)
	sig, err := crypto.Sign(authHash, privKey)
	if err != nil {
		panic("failed to sign authorization: " + err.Error())
	}

	auth.R = new(big.Int).SetBytes(sig[:32])
	auth.S = new(big.Int).SetBytes(sig[32:64])
	auth.V = new(big.Int).SetUint64(uint64(sig[64]))

	return auth
}

func TestSetCodeTx_AuthorizationProcessedInApplyMessage(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	// Generate a key pair for the authorization signer.
	privKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	signerAddr := crypto.PubkeyToAddress(privKey.PublicKey)

	// Fund the signer account so it exists in state (authorization checks nonce).
	statedb.AddBalance(signerAddr, big.NewInt(0))

	// The transaction sender (different from the authorization signer).
	sender := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	tenETH := new(big.Int).Mul(big.NewInt(10), new(big.Int).SetUint64(1e18))
	statedb.AddBalance(sender, tenETH)

	// The target contract address that the signer delegates to.
	targetContract := types.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	// Create a properly signed authorization.
	chainID := TestConfig.ChainID
	auth := signAuthorization(privKey, chainID, targetContract, 0)

	// Build a SetCode transaction message directly (bypass Transaction struct).
	to := types.HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc")
	msg := Message{
		From:      sender,
		To:        &to,
		Nonce:     0,
		Value:     new(big.Int),
		GasLimit:  100000,
		GasFeeCap: big.NewInt(1_000_000_000),
		GasTipCap: big.NewInt(0),
		Data:      nil,
		AuthList:  []types.Authorization{auth},
		TxType:    types.SetCodeTxType,
	}

	header := newTestHeader()
	gp := new(GasPool).AddGas(header.GasLimit)

	result, err := applyMessage(TestConfig, nil, statedb, header, &msg, gp)
	if err != nil {
		t.Fatalf("applyMessage failed: %v", err)
	}

	// The tx should succeed (or at least not be a protocol error).
	_ = result

	// Verify that the signer's code was set to the delegation designator.
	code := statedb.GetCode(signerAddr)
	if !IsDelegated(code) {
		t.Fatalf("signer code should be a delegation designator after SetCode tx, got %x", code)
	}

	resolved, ok := ResolveDelegation(code)
	if !ok {
		t.Fatal("ResolveDelegation should succeed on signer's code")
	}
	if resolved != targetContract {
		t.Errorf("delegation target: got %v, want %v", resolved.Hex(), targetContract.Hex())
	}

	// Signer nonce should be incremented by the authorization processing.
	if statedb.GetNonce(signerAddr) != 1 {
		t.Errorf("signer nonce: got %d, want 1", statedb.GetNonce(signerAddr))
	}
}

func TestSetCodeTx_IntrinsicGasIncludesAuthCosts(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	tenETH := new(big.Int).Mul(big.NewInt(10), new(big.Int).SetUint64(1e18))
	statedb.AddBalance(sender, tenETH)

	// Build a SetCode message with one authorization to a non-existent (empty) address.
	to := types.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	emptyTarget := types.HexToAddress("0x1111111111111111111111111111111111111111")

	msg := Message{
		From:      sender,
		To:        &to,
		Nonce:     0,
		Value:     new(big.Int),
		GasLimit:  100000,
		GasFeeCap: big.NewInt(1),
		GasTipCap: big.NewInt(0),
		GasPrice:  big.NewInt(1),
		Data:      nil,
		AuthList: []types.Authorization{
			{
				ChainID: big.NewInt(1),
				Address: emptyTarget,
				Nonce:   0,
				V:       big.NewInt(0),
				R:       big.NewInt(1),
				S:       big.NewInt(1),
			},
		},
		TxType: types.SetCodeTxType,
	}

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 10_000_000,
		Time:     1000,
		BaseFee:  big.NewInt(1),
		Coinbase: types.HexToAddress("0xfee"),
	}
	gp := new(GasPool).AddGas(header.GasLimit)

	result, err := applyMessage(TestConfig, nil, statedb, header, &msg, gp)
	if err != nil {
		t.Fatalf("applyMessage failed: %v", err)
	}

	// Expected intrinsic gas: TxGas + PerAuthBaseCost + PerEmptyAccountCost
	// (emptyTarget doesn't exist, so it counts as empty)
	expectedIntrinsic := TxGas + PerAuthBaseCost + PerEmptyAccountCost
	if result.UsedGas < expectedIntrinsic {
		t.Errorf("gas used %d is less than expected intrinsic %d", result.UsedGas, expectedIntrinsic)
	}
}

func TestSetCodeTx_IntrinsicGasTooLow(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	tenETH := new(big.Int).Mul(big.NewInt(10), new(big.Int).SetUint64(1e18))
	statedb.AddBalance(sender, tenETH)

	to := types.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	// Set gas limit to just the base TxGas, which is insufficient for a
	// SetCode tx with authorizations.
	msg := Message{
		From:      sender,
		To:        &to,
		Nonce:     0,
		Value:     new(big.Int),
		GasLimit:  TxGas, // 21000 - not enough for auth costs
		GasFeeCap: big.NewInt(1),
		GasTipCap: big.NewInt(0),
		GasPrice:  big.NewInt(1),
		Data:      nil,
		AuthList: []types.Authorization{
			{
				ChainID: big.NewInt(1),
				Address: types.HexToAddress("0x1111111111111111111111111111111111111111"),
				Nonce:   0,
				V:       big.NewInt(0),
				R:       big.NewInt(1),
				S:       big.NewInt(1),
			},
		},
		TxType: types.SetCodeTxType,
	}

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 10_000_000,
		Time:     1000,
		BaseFee:  big.NewInt(1),
		Coinbase: types.HexToAddress("0xfee"),
	}
	gp := new(GasPool).AddGas(header.GasLimit)

	result, err := applyMessage(TestConfig, nil, statedb, header, &msg, gp)
	if err != nil {
		t.Fatalf("applyMessage should not return protocol error, got: %v", err)
	}
	// Should fail with intrinsic gas error, consuming all gas.
	if !result.Failed() {
		t.Fatal("SetCode tx with insufficient gas should fail")
	}
	if result.UsedGas != TxGas {
		t.Errorf("all gas should be consumed: got %d, want %d", result.UsedGas, TxGas)
	}
}

func TestSetCodeTx_NonExistentAccountAuthGas(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	tenETH := new(big.Int).Mul(big.NewInt(10), new(big.Int).SetUint64(1e18))
	statedb.AddBalance(sender, tenETH)

	// One auth targeting an existing account, one targeting a non-existent account.
	existingAddr := types.HexToAddress("0x2222222222222222222222222222222222222222")
	statedb.AddBalance(existingAddr, big.NewInt(1)) // Make it exist and non-empty.

	nonExistentAddr := types.HexToAddress("0x3333333333333333333333333333333333333333")

	to := types.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	msg := Message{
		From:      sender,
		To:        &to,
		Nonce:     0,
		Value:     new(big.Int),
		GasLimit:  200000,
		GasFeeCap: big.NewInt(1),
		GasTipCap: big.NewInt(0),
		GasPrice:  big.NewInt(1),
		Data:      nil,
		AuthList: []types.Authorization{
			{
				ChainID: big.NewInt(1),
				Address: existingAddr,
				Nonce:   0,
				V:       big.NewInt(0),
				R:       big.NewInt(1),
				S:       big.NewInt(1),
			},
			{
				ChainID: big.NewInt(1),
				Address: nonExistentAddr,
				Nonce:   0,
				V:       big.NewInt(0),
				R:       big.NewInt(1),
				S:       big.NewInt(1),
			},
		},
		TxType: types.SetCodeTxType,
	}

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 10_000_000,
		Time:     1000,
		BaseFee:  big.NewInt(1),
		Coinbase: types.HexToAddress("0xfee"),
	}
	gp := new(GasPool).AddGas(header.GasLimit)

	result, err := applyMessage(TestConfig, nil, statedb, header, &msg, gp)
	if err != nil {
		t.Fatalf("applyMessage failed: %v", err)
	}

	// Expected: TxGas + 2*PerAuthBaseCost + 1*PerEmptyAccountCost
	// (existingAddr is non-empty, nonExistentAddr is empty)
	expectedIntrinsic := TxGas + 2*PerAuthBaseCost + 1*PerEmptyAccountCost
	if result.UsedGas < expectedIntrinsic {
		t.Errorf("gas used %d is less than expected intrinsic %d", result.UsedGas, expectedIntrinsic)
	}
}

func TestSetCodeTx_LegacyTxUnaffected(t *testing.T) {
	// Verify that a legacy transaction (type 0x00) is not affected by
	// EIP-7702 processing even if the message has no auth list.
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	tenETH := new(big.Int).Mul(big.NewInt(10), new(big.Int).SetUint64(1e18))
	statedb.AddBalance(sender, tenETH)

	to := types.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	msg := Message{
		From:     sender,
		To:       &to,
		Nonce:    0,
		Value:    new(big.Int),
		GasLimit: 21000,
		GasPrice: big.NewInt(1),
		Data:     nil,
		TxType:   types.LegacyTxType,
	}

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 10_000_000,
		Time:     1000,
		BaseFee:  big.NewInt(1),
		Coinbase: types.HexToAddress("0xfee"),
	}
	gp := new(GasPool).AddGas(header.GasLimit)

	result, err := applyMessage(TestConfig, nil, statedb, header, &msg, gp)
	if err != nil {
		t.Fatalf("applyMessage failed: %v", err)
	}
	// Should use exactly TxGas for a simple transfer.
	if result.UsedGas != TxGas {
		t.Errorf("legacy tx gas: got %d, want %d", result.UsedGas, TxGas)
	}
}

func TestTransactionToMessage_SetCodeTx(t *testing.T) {
	to := types.HexToAddress("0xdead")
	inner := &types.SetCodeTx{
		ChainID:   big.NewInt(1),
		Nonce:     5,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(20_000_000_000),
		Gas:       100000,
		To:        to,
		Value:     big.NewInt(0),
		Data:      []byte{0xab},
		AuthorizationList: []types.Authorization{
			{
				ChainID: big.NewInt(1),
				Address: types.HexToAddress("0xbeef"),
				Nonce:   0,
				V:       big.NewInt(0),
				R:       big.NewInt(1),
				S:       big.NewInt(2),
			},
		},
	}
	tx := types.NewTransaction(inner)
	msg := TransactionToMessage(tx)

	if msg.TxType != types.SetCodeTxType {
		t.Errorf("TxType: got %d, want %d", msg.TxType, types.SetCodeTxType)
	}
	if len(msg.AuthList) != 1 {
		t.Fatalf("AuthList length: got %d, want 1", len(msg.AuthList))
	}
	if msg.AuthList[0].Address != types.HexToAddress("0xbeef") {
		t.Errorf("AuthList[0].Address: got %v, want 0xbeef", msg.AuthList[0].Address.Hex())
	}
	if msg.AuthList[0].Nonce != 0 {
		t.Errorf("AuthList[0].Nonce: got %d, want 0", msg.AuthList[0].Nonce)
	}
}

func TestTransactionToMessage_LegacyTxHasNoAuthList(t *testing.T) {
	to := types.HexToAddress("0xdead")
	inner := &types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(0),
	}
	tx := types.NewTransaction(inner)
	msg := TransactionToMessage(tx)

	if msg.TxType != types.LegacyTxType {
		t.Errorf("TxType: got %d, want %d", msg.TxType, types.LegacyTxType)
	}
	if msg.AuthList != nil {
		t.Errorf("AuthList should be nil for legacy tx, got %v", msg.AuthList)
	}
}

func TestSetCodeTx_MultipleAuthsWithSignatures(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	// Generate two key pairs.
	privKey1, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key1: %v", err)
	}
	signer1 := crypto.PubkeyToAddress(privKey1.PublicKey)

	privKey2, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key2: %v", err)
	}
	signer2 := crypto.PubkeyToAddress(privKey2.PublicKey)

	// Ensure both signers exist in state.
	statedb.AddBalance(signer1, big.NewInt(0))
	statedb.AddBalance(signer2, big.NewInt(0))

	// Fund the tx sender.
	sender := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	tenETH := new(big.Int).Mul(big.NewInt(10), new(big.Int).SetUint64(1e18))
	statedb.AddBalance(sender, tenETH)

	target1 := types.HexToAddress("0x1111111111111111111111111111111111111111")
	target2 := types.HexToAddress("0x2222222222222222222222222222222222222222")

	chainID := TestConfig.ChainID
	auth1 := signAuthorization(privKey1, chainID, target1, 0)
	auth2 := signAuthorization(privKey2, chainID, target2, 0)

	to := types.HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc")
	msg := Message{
		From:      sender,
		To:        &to,
		Nonce:     0,
		Value:     new(big.Int),
		GasLimit:  200000,
		GasFeeCap: big.NewInt(1_000_000_000),
		GasTipCap: big.NewInt(0),
		Data:      nil,
		AuthList:  []types.Authorization{auth1, auth2},
		TxType:    types.SetCodeTxType,
	}

	header := newTestHeader()
	gp := new(GasPool).AddGas(header.GasLimit)

	_, err = applyMessage(TestConfig, nil, statedb, header, &msg, gp)
	if err != nil {
		t.Fatalf("applyMessage failed: %v", err)
	}

	// Both signers should have delegation code set.
	code1 := statedb.GetCode(signer1)
	if !IsDelegated(code1) {
		t.Errorf("signer1 code should be delegated, got %x", code1)
	}
	resolved1, ok := ResolveDelegation(code1)
	if !ok || resolved1 != target1 {
		t.Errorf("signer1 delegation target: got %v, want %v", resolved1.Hex(), target1.Hex())
	}

	code2 := statedb.GetCode(signer2)
	if !IsDelegated(code2) {
		t.Errorf("signer2 code should be delegated, got %x", code2)
	}
	resolved2, ok := ResolveDelegation(code2)
	if !ok || resolved2 != target2 {
		t.Errorf("signer2 delegation target: got %v, want %v", resolved2.Hex(), target2.Hex())
	}

	// Both nonces should be incremented.
	if statedb.GetNonce(signer1) != 1 {
		t.Errorf("signer1 nonce: got %d, want 1", statedb.GetNonce(signer1))
	}
	if statedb.GetNonce(signer2) != 1 {
		t.Errorf("signer2 nonce: got %d, want 1", statedb.GetNonce(signer2))
	}
}
