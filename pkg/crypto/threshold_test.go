package crypto

import (
	"math/big"
	"testing"
)

func TestNewThresholdScheme(t *testing.T) {
	// Valid: 3-of-5.
	ts, err := NewThresholdScheme(3, 5)
	if err != nil {
		t.Fatalf("NewThresholdScheme(3,5): %v", err)
	}
	if ts.T != 3 || ts.N != 5 {
		t.Errorf("got T=%d, N=%d, want T=3, N=5", ts.T, ts.N)
	}

	// Valid: 1-of-1.
	_, err = NewThresholdScheme(1, 1)
	if err != nil {
		t.Fatalf("NewThresholdScheme(1,1): %v", err)
	}

	// Invalid: t=0.
	_, err = NewThresholdScheme(0, 5)
	if err != ErrInvalidThreshold {
		t.Errorf("NewThresholdScheme(0,5): got %v, want %v", err, ErrInvalidThreshold)
	}

	// Invalid: t > n.
	_, err = NewThresholdScheme(6, 5)
	if err != ErrInvalidThreshold {
		t.Errorf("NewThresholdScheme(6,5): got %v, want %v", err, ErrInvalidThreshold)
	}
}

func TestKeyGeneration3of5(t *testing.T) {
	ts, err := NewThresholdScheme(3, 5)
	if err != nil {
		t.Fatalf("NewThresholdScheme: %v", err)
	}

	result, err := ts.KeyGeneration()
	if err != nil {
		t.Fatalf("KeyGeneration: %v", err)
	}

	// Should have 5 shares.
	if len(result.Shares) != 5 {
		t.Fatalf("got %d shares, want 5", len(result.Shares))
	}

	// Should have 3 commitments (degree t-1 polynomial has t coefficients).
	if len(result.Commitments) != 3 {
		t.Fatalf("got %d commitments, want 3", len(result.Commitments))
	}

	// Public key should be non-nil and non-zero.
	if result.PublicKey == nil || result.PublicKey.Sign() == 0 {
		t.Fatal("PublicKey is nil or zero")
	}

	// Each share index should be 1-based and unique.
	for i, s := range result.Shares {
		if s.Index != i+1 {
			t.Errorf("share %d has index %d, want %d", i, s.Index, i+1)
		}
		if s.Value == nil || s.Value.Sign() == 0 {
			t.Errorf("share %d has nil/zero value", i)
		}
	}
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	ts, err := NewThresholdScheme(3, 5)
	if err != nil {
		t.Fatalf("NewThresholdScheme: %v", err)
	}

	result, err := ts.KeyGeneration()
	if err != nil {
		t.Fatalf("KeyGeneration: %v", err)
	}

	// Encrypt a message.
	message := []byte("encrypted mempool transaction data for MEV resistance")
	encrypted, err := ShareEncrypt(result.PublicKey, message)
	if err != nil {
		t.Fatalf("ShareEncrypt: %v", err)
	}

	if encrypted.Ephemeral == nil || encrypted.Ephemeral.Sign() == 0 {
		t.Fatal("Ephemeral is nil/zero")
	}
	if len(encrypted.Ciphertext) == 0 {
		t.Fatal("Ciphertext is empty")
	}

	// Each party computes their decryption share.
	decShares := make([]DecryptionShare, 5)
	for i, share := range result.Shares {
		decShares[i] = ShareDecrypt(share, encrypted.Ephemeral)
	}

	// Use first 3 shares (threshold = 3) to decrypt.
	plaintext, err := CombineShares(decShares[:3], encrypted)
	if err != nil {
		t.Fatalf("CombineShares with 3 shares: %v", err)
	}

	if string(plaintext) != string(message) {
		t.Errorf("decrypted = %q, want %q", plaintext, message)
	}
}

func TestTMinusOneSharesInsufficient(t *testing.T) {
	ts, err := NewThresholdScheme(3, 5)
	if err != nil {
		t.Fatalf("NewThresholdScheme: %v", err)
	}

	result, err := ts.KeyGeneration()
	if err != nil {
		t.Fatalf("KeyGeneration: %v", err)
	}

	message := []byte("secret transaction")
	encrypted, err := ShareEncrypt(result.PublicKey, message)
	if err != nil {
		t.Fatalf("ShareEncrypt: %v", err)
	}

	// Compute decryption shares for all parties.
	decShares := make([]DecryptionShare, 5)
	for i, share := range result.Shares {
		decShares[i] = ShareDecrypt(share, encrypted.Ephemeral)
	}

	// Only use 2 shares (t-1 = 2): should fail to decrypt correctly.
	plaintext, err := CombineShares(decShares[:2], encrypted)
	if err == nil && string(plaintext) == string(message) {
		t.Error("t-1 shares should NOT correctly decrypt the message")
	}
	// It's expected to either error or produce wrong plaintext.
}

func TestTSharesSufficient(t *testing.T) {
	ts, err := NewThresholdScheme(3, 5)
	if err != nil {
		t.Fatalf("NewThresholdScheme: %v", err)
	}

	result, err := ts.KeyGeneration()
	if err != nil {
		t.Fatalf("KeyGeneration: %v", err)
	}

	message := []byte("threshold encrypted data")
	encrypted, err := ShareEncrypt(result.PublicKey, message)
	if err != nil {
		t.Fatalf("ShareEncrypt: %v", err)
	}

	decShares := make([]DecryptionShare, 5)
	for i, share := range result.Shares {
		decShares[i] = ShareDecrypt(share, encrypted.Ephemeral)
	}

	// Test with exactly t shares from different combinations.
	combos := [][]int{
		{0, 1, 2},
		{0, 2, 4},
		{1, 3, 4},
		{2, 3, 4},
	}

	for _, combo := range combos {
		subset := make([]DecryptionShare, len(combo))
		for i, idx := range combo {
			subset[i] = decShares[idx]
		}
		plaintext, err := CombineShares(subset, encrypted)
		if err != nil {
			t.Errorf("CombineShares with indices %v: %v", combo, err)
			continue
		}
		if string(plaintext) != string(message) {
			t.Errorf("CombineShares with indices %v: got %q, want %q", combo, plaintext, message)
		}
	}
}

func TestVerifiableShareVerification(t *testing.T) {
	ts, err := NewThresholdScheme(3, 5)
	if err != nil {
		t.Fatalf("NewThresholdScheme: %v", err)
	}

	result, err := ts.KeyGeneration()
	if err != nil {
		t.Fatalf("KeyGeneration: %v", err)
	}

	// All shares should verify against the commitments.
	for i, share := range result.Shares {
		if !VerifyShare(share, result.Commitments) {
			t.Errorf("share %d failed verification", i)
		}
	}

	// A tampered share should fail verification.
	tampered := Share{
		Index: result.Shares[0].Index,
		Value: new(big.Int).Add(result.Shares[0].Value, big.NewInt(1)),
	}
	if VerifyShare(tampered, result.Commitments) {
		t.Error("tampered share should fail verification")
	}

	// Wrong index should fail verification.
	wrongIndex := Share{
		Index: 99,
		Value: result.Shares[0].Value,
	}
	if VerifyShare(wrongIndex, result.Commitments) {
		t.Error("share with wrong index should fail verification")
	}
}

func TestLagrangeInterpolation(t *testing.T) {
	ts, err := NewThresholdScheme(3, 5)
	if err != nil {
		t.Fatalf("NewThresholdScheme: %v", err)
	}

	result, err := ts.KeyGeneration()
	if err != nil {
		t.Fatalf("KeyGeneration: %v", err)
	}

	// The secret is recoverable from any 3 shares via Lagrange interpolation.
	// We verify by interpolating f(0) from shares and checking g^{f(0)} == publicKey.
	g := thresholdParams.g
	p := thresholdParams.p

	combos := [][]int{
		{0, 1, 2},
		{0, 2, 4},
		{1, 3, 4},
	}

	for _, combo := range combos {
		subset := make([]Share, len(combo))
		for i, idx := range combo {
			subset[i] = result.Shares[idx]
		}

		secret, err := LagrangeInterpolate(subset)
		if err != nil {
			t.Errorf("LagrangeInterpolate with indices %v: %v", combo, err)
			continue
		}

		// Verify: g^secret mod p == publicKey.
		recovered := new(big.Int).Exp(g, secret, p)
		if recovered.Cmp(result.PublicKey) != 0 {
			t.Errorf("LagrangeInterpolate with indices %v: recovered secret does not match", combo)
		}
	}
}

func TestLagrangeInterpolationDuplicateIndex(t *testing.T) {
	shares := []Share{
		{Index: 1, Value: big.NewInt(5)},
		{Index: 1, Value: big.NewInt(7)},
		{Index: 3, Value: big.NewInt(9)},
	}

	_, err := LagrangeInterpolate(shares)
	if err != ErrDuplicateShareIndex {
		t.Errorf("got %v, want %v", err, ErrDuplicateShareIndex)
	}
}

func TestLagrangeInterpolationEmpty(t *testing.T) {
	_, err := LagrangeInterpolate(nil)
	if err != ErrInsufficientShares {
		t.Errorf("got %v, want %v", err, ErrInsufficientShares)
	}
}

func TestCombineSharesDuplicate(t *testing.T) {
	dup := []DecryptionShare{
		{Index: 1, Value: big.NewInt(10)},
		{Index: 1, Value: big.NewInt(20)},
	}
	_, err := CombineShares(dup, &EncryptedMessage{
		Ephemeral:  big.NewInt(1),
		Ciphertext: []byte("test"),
		Nonce:      make([]byte, 12),
	})
	if err != ErrDuplicateShareIndex {
		t.Errorf("got %v, want %v", err, ErrDuplicateShareIndex)
	}
}

func TestVerifyShareEdgeCases(t *testing.T) {
	// Nil commitments.
	if VerifyShare(Share{Index: 1, Value: big.NewInt(5)}, nil) {
		t.Error("nil commitments should fail")
	}

	// Nil value.
	if VerifyShare(Share{Index: 1, Value: nil}, []*big.Int{big.NewInt(1)}) {
		t.Error("nil value should fail")
	}
}

func TestEncryptNilPublicKey(t *testing.T) {
	_, err := ShareEncrypt(nil, []byte("test"))
	if err == nil {
		t.Error("ShareEncrypt with nil public key should fail")
	}
}

func TestCombineSharesNilEncrypted(t *testing.T) {
	_, err := CombineShares([]DecryptionShare{{Index: 1, Value: big.NewInt(1)}}, nil)
	if err != ErrInvalidCiphertext {
		t.Errorf("got %v, want %v", err, ErrInvalidCiphertext)
	}
}

func TestAllNSharesSufficient(t *testing.T) {
	ts, err := NewThresholdScheme(3, 5)
	if err != nil {
		t.Fatalf("NewThresholdScheme: %v", err)
	}

	result, err := ts.KeyGeneration()
	if err != nil {
		t.Fatalf("KeyGeneration: %v", err)
	}

	message := []byte("all shares decrypt")
	encrypted, err := ShareEncrypt(result.PublicKey, message)
	if err != nil {
		t.Fatalf("ShareEncrypt: %v", err)
	}

	// Use all 5 shares.
	decShares := make([]DecryptionShare, 5)
	for i, share := range result.Shares {
		decShares[i] = ShareDecrypt(share, encrypted.Ephemeral)
	}

	plaintext, err := CombineShares(decShares, encrypted)
	if err != nil {
		t.Fatalf("CombineShares with all shares: %v", err)
	}
	if string(plaintext) != string(message) {
		t.Errorf("got %q, want %q", plaintext, message)
	}
}
