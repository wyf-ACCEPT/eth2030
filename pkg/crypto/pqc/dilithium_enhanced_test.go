package pqc

import (
	"testing"
)

func newLevel2Signer() *EnhancedDilithiumSigner {
	return NewEnhancedDilithiumSigner(DefaultEnhancedDilithiumConfig(2))
}

func newLevel3Signer() *EnhancedDilithiumSigner {
	return NewEnhancedDilithiumSigner(DefaultEnhancedDilithiumConfig(3))
}

func newLevel5Signer() *EnhancedDilithiumSigner {
	return NewEnhancedDilithiumSigner(DefaultEnhancedDilithiumConfig(5))
}

func TestEnhancedDilithiumKeyGenLevel2(t *testing.T) {
	s := newLevel2Signer()
	kp, err := s.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if kp == nil {
		t.Fatal("expected non-nil keypair")
	}
	if len(kp.PublicKey) == 0 {
		t.Fatal("empty public key")
	}
	if len(kp.SecretKey) == 0 {
		t.Fatal("empty secret key")
	}
	if len(kp.PublicT) != kp.Config.K*kp.Config.N {
		t.Fatalf("wrong t length: got %d, want %d", len(kp.PublicT), kp.Config.K*kp.Config.N)
	}
}

func TestEnhancedDilithiumKeyGenLevel3(t *testing.T) {
	s := newLevel3Signer()
	kp, err := s.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(kp.SecretS1) != kp.Config.L*kp.Config.N {
		t.Fatalf("wrong s1 length: got %d, want %d", len(kp.SecretS1), kp.Config.L*kp.Config.N)
	}
}

func TestEnhancedDilithiumKeyGenLevel5(t *testing.T) {
	s := newLevel5Signer()
	kp, err := s.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if kp.Config.SecurityLevel != 5 {
		t.Fatalf("wrong security level: got %d, want 5", kp.Config.SecurityLevel)
	}
}

func TestEnhancedDilithiumSignAndVerify(t *testing.T) {
	s := newLevel2Signer()
	kp, err := s.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	msg := []byte("hello lattice world")
	sig, err := s.Sign(kp, msg)
	if err != nil {
		t.Fatal(err)
	}
	if sig == nil {
		t.Fatal("nil signature")
	}
	if len(sig.ChallengeHash) != 32 {
		t.Fatalf("challenge hash length: got %d, want 32", len(sig.ChallengeHash))
	}
	if len(sig.Z) != kp.Config.L*kp.Config.N {
		t.Fatalf("z vector length mismatch")
	}

	ok, err := s.Verify(kp.PublicKey, msg, sig)
	if err != nil {
		t.Fatalf("verify error: %v", err)
	}
	if !ok {
		t.Fatal("valid signature rejected")
	}
}

func TestEnhancedDilithiumVerifyWrongMessage(t *testing.T) {
	s := newLevel2Signer()
	kp, err := s.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	msg := []byte("original message")
	sig, err := s.Sign(kp, msg)
	if err != nil {
		t.Fatal(err)
	}

	ok, err := s.Verify(kp.PublicKey, []byte("different message"), sig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("signature verified for wrong message")
	}
}

func TestEnhancedDilithiumVerifyWrongKey(t *testing.T) {
	s := newLevel2Signer()
	kp1, err := s.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	kp2, err := s.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	msg := []byte("test message")
	sig, err := s.Sign(kp1, msg)
	if err != nil {
		t.Fatal(err)
	}

	ok, err := s.Verify(kp2.PublicKey, msg, sig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("signature verified with wrong key")
	}
}

func TestEnhancedDilithiumSignNilKey(t *testing.T) {
	s := newLevel2Signer()
	_, err := s.Sign(nil, []byte("test"))
	if err != ErrEnhancedDilithiumNilKey {
		t.Fatalf("expected ErrEnhancedDilithiumNilKey, got %v", err)
	}
}

func TestEnhancedDilithiumSignEmptyMessage(t *testing.T) {
	s := newLevel2Signer()
	kp, err := s.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Sign(kp, []byte{})
	if err != ErrEnhancedDilithiumEmptyMsg {
		t.Fatalf("expected ErrEnhancedDilithiumEmptyMsg, got %v", err)
	}
}

func TestEnhancedDilithiumVerifyNilSig(t *testing.T) {
	s := newLevel2Signer()
	_, err := s.Verify([]byte("pubkey"), []byte("msg"), nil)
	if err != ErrEnhancedDilithiumBadSig {
		t.Fatalf("expected ErrEnhancedDilithiumBadSig, got %v", err)
	}
}

func TestEnhancedDilithiumVerifyEmptyMessage(t *testing.T) {
	s := newLevel2Signer()
	sig := &EnhancedDilithiumSig{
		Z:             make([]int64, 256),
		ChallengeHash: make([]byte, 32),
	}
	_, err := s.Verify([]byte("pubkey"), []byte{}, sig)
	if err != ErrEnhancedDilithiumEmptyMsg {
		t.Fatalf("expected ErrEnhancedDilithiumEmptyMsg, got %v", err)
	}
}

func TestEnhancedDilithiumVerifyBadPubkeyLength(t *testing.T) {
	s := newLevel2Signer()
	sig := &EnhancedDilithiumSig{
		Z:             make([]int64, 256),
		ChallengeHash: make([]byte, 32),
	}
	ok, err := s.Verify([]byte("short"), []byte("msg"), sig)
	if err != ErrEnhancedDilithiumBadSig {
		t.Fatalf("expected ErrEnhancedDilithiumBadSig, got %v (ok=%v)", err, ok)
	}
}

func TestEnhancedDilithiumModQ(t *testing.T) {
	q := int64(8380417)
	tests := []struct {
		input    int64
		expected int64
	}{
		{0, 0},
		{1, 1},
		{q, 0},
		{q + 1, 1},
		{-1, q - 1},
		{-q, 0},
	}
	for _, tc := range tests {
		got := modQ(tc.input, q)
		if got != tc.expected {
			t.Errorf("modQ(%d, %d) = %d, want %d", tc.input, q, got, tc.expected)
		}
	}
}

func TestEnhancedDilithiumCenteredMod(t *testing.T) {
	q := int64(8380417)
	tests := []struct {
		input    int64
		expected int64
	}{
		{0, 0},
		{1, 1},
		{q / 2, q / 2},
		{q/2 + 1, q/2 + 1 - q},
		{q - 1, -1},
	}
	for _, tc := range tests {
		got := centeredMod(tc.input, q)
		if got != tc.expected {
			t.Errorf("centeredMod(%d, %d) = %d, want %d", tc.input, q, got, tc.expected)
		}
	}
}

func TestEnhancedDilithiumPolyAddSub(t *testing.T) {
	q := int64(8380417)
	a := []int64{1, 2, 3, 4}
	b := []int64{5, 6, 7, 8}

	sum := polyAddModQ(a, b, q)
	if sum[0] != 6 || sum[1] != 8 || sum[2] != 10 || sum[3] != 12 {
		t.Fatalf("polyAdd unexpected: %v", sum)
	}

	diff := polySubModQ(sum, b, q)
	for i := range a {
		if diff[i] != a[i] {
			t.Fatalf("polySubModQ[%d]: got %d, want %d", i, diff[i], a[i])
		}
	}
}

func TestEnhancedDilithiumPolyMul(t *testing.T) {
	q := int64(8380417)
	// Multiply (1 + x) * (1 + x) mod (x^4 + 1) = 1 + 2x + x^2
	a := []int64{1, 1, 0, 0}
	b := []int64{1, 1, 0, 0}
	c := polyMulModQ(a, b, 4, q)
	if c[0] != 1 || c[1] != 2 || c[2] != 1 || c[3] != 0 {
		t.Fatalf("polyMul unexpected: %v", c)
	}
}

func TestEnhancedDilithiumPolyMulReduction(t *testing.T) {
	// Test that X^N wraps with negation (mod X^N + 1).
	q := int64(8380417)
	n := 4
	// a = x^2, b = x^3 -> a*b = x^5 = -x (mod x^4+1)
	a := []int64{0, 0, 1, 0}
	b := []int64{0, 0, 0, 1}
	c := polyMulModQ(a, b, n, q)
	// x^5 mod (x^4+1) = -x^1
	expected := []int64{0, q - 1, 0, 0}
	for i := range c {
		if c[i] != expected[i] {
			t.Fatalf("polyMul reduction [%d]: got %d, want %d", i, c[i], expected[i])
		}
	}
}

func TestEnhancedDilithiumMultipleSignVerify(t *testing.T) {
	s := newLevel2Signer()
	kp, err := s.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	messages := []string{
		"message one",
		"message two",
		"a much longer message that exercises the signing code with more data",
	}

	for _, m := range messages {
		msg := []byte(m)
		sig, err := s.Sign(kp, msg)
		if err != nil {
			t.Fatalf("sign %q: %v", m, err)
		}
		ok, err := s.Verify(kp.PublicKey, msg, sig)
		if err != nil {
			t.Fatalf("verify %q: %v", m, err)
		}
		if !ok {
			t.Fatalf("valid signature rejected for %q", m)
		}
	}
}

func TestEnhancedDilithiumKeySizes(t *testing.T) {
	for _, level := range []int{2, 3, 5} {
		ks := EnhancedKeySize(level)
		if ks <= 0 {
			t.Fatalf("EnhancedKeySize(%d) = %d, want > 0", level, ks)
		}
		ss := EnhancedSigSize(level)
		if ss <= 0 {
			t.Fatalf("EnhancedSigSize(%d) = %d, want > 0", level, ss)
		}
	}
	// Level 5 keys should be larger than level 2 keys.
	if EnhancedKeySize(5) <= EnhancedKeySize(2) {
		t.Fatal("level 5 key should be larger than level 2")
	}
}

func TestEnhancedDilithiumConfigDefaults(t *testing.T) {
	c2 := DefaultEnhancedDilithiumConfig(2)
	c3 := DefaultEnhancedDilithiumConfig(3)
	c5 := DefaultEnhancedDilithiumConfig(5)

	if c2.Q != 8380417 || c3.Q != 8380417 || c5.Q != 8380417 {
		t.Fatal("all levels should use Q = 8380417")
	}
	if c2.K >= c3.K || c3.K >= c5.K {
		t.Fatal("K should increase with security level")
	}
}

func TestEnhancedDilithiumVerifyTamperedZ(t *testing.T) {
	s := newLevel2Signer()
	kp, err := s.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	msg := []byte("tamper test")
	sig, err := s.Sign(kp, msg)
	if err != nil {
		t.Fatal(err)
	}

	// Tamper with z vector.
	sig.Z[0] = sig.Z[0] + 1000

	ok, err := s.Verify(kp.PublicKey, msg, sig)
	if err != nil {
		// Either error or false is acceptable for tampered sig.
		return
	}
	if ok {
		t.Fatal("tampered signature should not verify")
	}
}

func TestEnhancedDilithiumSigBadChallengeHash(t *testing.T) {
	s := newLevel2Signer()
	sig := &EnhancedDilithiumSig{
		Z:             make([]int64, 256),
		ChallengeHash: []byte{1, 2, 3}, // too short
	}
	kp, err := s.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Verify(kp.PublicKey, []byte("msg"), sig)
	if err != ErrEnhancedDilithiumBadSig {
		t.Fatalf("expected ErrEnhancedDilithiumBadSig for short challenge hash, got %v", err)
	}
}

func TestEnhancedDilithiumDifferentKeysProduceDifferentSigs(t *testing.T) {
	s := newLevel2Signer()
	kp1, _ := s.GenerateKey()
	kp2, _ := s.GenerateKey()
	msg := []byte("same message")
	sig1, err := s.Sign(kp1, msg)
	if err != nil {
		t.Fatal(err)
	}
	sig2, err := s.Sign(kp2, msg)
	if err != nil {
		t.Fatal(err)
	}
	// Challenge hashes should differ (different keys -> different public key hash).
	same := true
	for i := range sig1.ChallengeHash {
		if sig1.ChallengeHash[i] != sig2.ChallengeHash[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("different keys should produce different challenge hashes")
	}
}
