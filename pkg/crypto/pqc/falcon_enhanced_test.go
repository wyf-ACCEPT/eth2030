package pqc

import (
	"bytes"
	"testing"
)

func falconSigner512() *FalconEnhancedSigner {
	return NewFalconEnhancedSigner(DefaultFalconEnhancedParams())
}

func falconSigner1024() *FalconEnhancedSigner {
	return NewFalconEnhancedSigner(Falcon1024EnhancedParams())
}

func TestFalconEnhancedKeyGen512(t *testing.T) {
	s := falconSigner512()
	kp, err := s.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if kp == nil {
		t.Fatal("nil keypair")
	}
	p := DefaultFalconEnhancedParams()
	if len(kp.PublicKey) != FalconEnhancedKeySize(p) {
		t.Fatalf("pk size: got %d, want %d", len(kp.PublicKey), FalconEnhancedKeySize(p))
	}
	if len(kp.SecretKey) != p.N*2 {
		t.Fatalf("sk size: got %d, want %d", len(kp.SecretKey), p.N*2)
	}
	if len(kp.F) != p.N || len(kp.G) != p.N || len(kp.H) != p.N {
		t.Fatal("internal polynomial length mismatch")
	}
}

func TestFalconEnhancedKeyGen1024(t *testing.T) {
	s := falconSigner1024()
	kp, err := s.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	p := Falcon1024EnhancedParams()
	if len(kp.PublicKey) != FalconEnhancedKeySize(p) {
		t.Fatalf("pk size: got %d, want %d", len(kp.PublicKey), FalconEnhancedKeySize(p))
	}
}

func TestFalconEnhancedSignAndVerify(t *testing.T) {
	s := falconSigner512()
	kp, err := s.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("hello falcon lattice")
	sig, err := s.Sign(kp, msg)
	if err != nil {
		t.Fatal(err)
	}
	if sig == nil {
		t.Fatal("nil signature")
	}
	if len(sig.SigPoly) != kp.Params.N*2 {
		t.Fatalf("sig poly size: got %d, want %d", len(sig.SigPoly), kp.Params.N*2)
	}
	if len(sig.Salt) != 32 {
		t.Fatalf("challenge hash size: got %d, want 32", len(sig.Salt))
	}
	ok, err := s.Verify(kp.PublicKey, msg, sig)
	if err != nil {
		t.Fatalf("verify error: %v", err)
	}
	if !ok {
		t.Fatal("valid signature rejected")
	}
}

func TestFalconEnhancedVerifyWrongMessage(t *testing.T) {
	s := falconSigner512()
	kp, err := s.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	sig, err := s.Sign(kp, []byte("original"))
	if err != nil {
		t.Fatal(err)
	}
	ok, err := s.Verify(kp.PublicKey, []byte("tampered"), sig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("signature verified for wrong message")
	}
}

func TestFalconEnhancedVerifyWrongKey(t *testing.T) {
	s := falconSigner512()
	kp1, err := s.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	kp2, err := s.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("key test")
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

func TestFalconEnhancedSignNilKey(t *testing.T) {
	s := falconSigner512()
	_, err := s.Sign(nil, []byte("test"))
	if err != ErrFalconEnhancedNilKey {
		t.Fatalf("expected ErrFalconEnhancedNilKey, got %v", err)
	}
}

func TestFalconEnhancedSignEmptyMessage(t *testing.T) {
	s := falconSigner512()
	kp, err := s.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Sign(kp, []byte{})
	if err != ErrFalconEnhancedEmptyMsg {
		t.Fatalf("expected ErrFalconEnhancedEmptyMsg, got %v", err)
	}
}

func TestFalconEnhancedVerifyNilSig(t *testing.T) {
	s := falconSigner512()
	_, err := s.Verify(make([]byte, 256), []byte("msg"), nil)
	if err != ErrFalconEnhancedBadSig {
		t.Fatalf("expected ErrFalconEnhancedBadSig, got %v", err)
	}
}

func TestFalconEnhancedVerifyEmptyMessage(t *testing.T) {
	s := falconSigner512()
	sig := &FalconEnhancedSig{
		SigPoly: make([]byte, 128),
		Salt:    make([]byte, 32),
		Nonce:   make([]byte, 40),
	}
	_, err := s.Verify(make([]byte, 256), []byte{}, sig)
	if err != ErrFalconEnhancedEmptyMsg {
		t.Fatalf("expected ErrFalconEnhancedEmptyMsg, got %v", err)
	}
}

func TestFalconEnhancedVerifyBadPubkeyLength(t *testing.T) {
	s := falconSigner512()
	sig := &FalconEnhancedSig{
		SigPoly: make([]byte, 128),
		Salt:    make([]byte, 32),
		Nonce:   make([]byte, 40),
	}
	_, err := s.Verify([]byte("short"), []byte("msg"), sig)
	if err != ErrFalconEnhancedBadSig {
		t.Fatalf("expected ErrFalconEnhancedBadSig, got %v", err)
	}
}

func TestFalconEnhancedVerifyBadSaltLength(t *testing.T) {
	s := falconSigner512()
	sig := &FalconEnhancedSig{
		SigPoly: make([]byte, 128),
		Salt:    []byte{1, 2, 3}, // too short
		Nonce:   make([]byte, 40),
	}
	_, err := s.Verify(make([]byte, 256), []byte("msg"), sig)
	if err != ErrFalconEnhancedBadSig {
		t.Fatalf("expected ErrFalconEnhancedBadSig, got %v", err)
	}
}

func TestFalconEnhancedMultipleMessages(t *testing.T) {
	s := falconSigner512()
	kp, err := s.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	messages := []string{
		"message one",
		"message two",
		"a longer message to exercise the signing code with more data bytes",
		"short",
		"yet another test message for Falcon",
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

func TestFalconEnhancedDifferentKeysProduceDifferentSigs(t *testing.T) {
	s := falconSigner512()
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
	if bytes.Equal(sig1.Salt, sig2.Salt) && bytes.Equal(sig1.SigPoly, sig2.SigPoly) {
		t.Fatal("different keys should produce different signatures")
	}
}

func TestFalconEnhancedSigNotDeterministic(t *testing.T) {
	s := falconSigner512()
	kp, _ := s.GenerateKey()
	msg := []byte("determinism test")
	sig1, err := s.Sign(kp, msg)
	if err != nil {
		t.Fatal(err)
	}
	sig2, err := s.Sign(kp, msg)
	if err != nil {
		t.Fatal(err)
	}
	// Signatures should differ due to random masking.
	if bytes.Equal(sig1.SigPoly, sig2.SigPoly) {
		t.Fatal("expected different signatures due to random masking")
	}
	// But both should verify.
	ok1, _ := s.Verify(kp.PublicKey, msg, sig1)
	ok2, _ := s.Verify(kp.PublicKey, msg, sig2)
	if !ok1 || !ok2 {
		t.Fatal("both signatures should verify")
	}
}

func TestFalconEnhancedVerifyTamperedZ(t *testing.T) {
	s := falconSigner512()
	kp, _ := s.GenerateKey()
	msg := []byte("tamper test")
	sig, err := s.Sign(kp, msg)
	if err != nil {
		t.Fatal(err)
	}
	// Tamper with z polynomial bytes.
	sig.SigPoly[0] ^= 0xFF
	sig.SigPoly[1] ^= 0xFF
	ok, err := s.Verify(kp.PublicKey, msg, sig)
	if err != nil {
		return // error is acceptable for tampered sig
	}
	if ok {
		t.Fatal("tampered signature should not verify")
	}
}

func TestFalconEnhancedVerifyTamperedChallenge(t *testing.T) {
	s := falconSigner512()
	kp, _ := s.GenerateKey()
	msg := []byte("challenge tamper")
	sig, err := s.Sign(kp, msg)
	if err != nil {
		t.Fatal(err)
	}
	sig.Salt[0] ^= 0xFF
	ok, err := s.Verify(kp.PublicKey, msg, sig)
	if err != nil {
		return
	}
	if ok {
		t.Fatal("tampered challenge should not verify")
	}
}

func TestFalconEnhancedKeySizes(t *testing.T) {
	p512 := DefaultFalconEnhancedParams()
	p1024 := Falcon1024EnhancedParams()
	ks512 := FalconEnhancedKeySize(p512)
	ks1024 := FalconEnhancedKeySize(p1024)
	if ks512 <= 0 {
		t.Fatalf("key size 512 = %d, want > 0", ks512)
	}
	if ks1024 <= ks512 {
		t.Fatal("1024 key size should be larger than 512")
	}
	ss512 := FalconEnhancedSigSize(p512)
	ss1024 := FalconEnhancedSigSize(p1024)
	if ss512 <= 0 || ss1024 <= ss512 {
		t.Fatalf("sig sizes: 512=%d, 1024=%d", ss512, ss1024)
	}
}

func TestFalconEnhancedModQ(t *testing.T) {
	q := int64(12289)
	tests := []struct {
		in, want int64
	}{
		{0, 0}, {1, 1}, {q, 0}, {q + 1, 1}, {-1, q - 1}, {-q, 0},
	}
	for _, tc := range tests {
		got := fMod(tc.in, q)
		if got != tc.want {
			t.Errorf("fMod(%d, %d) = %d, want %d", tc.in, q, got, tc.want)
		}
	}
}

func TestFalconEnhancedCenterMod(t *testing.T) {
	q := int64(12289)
	tests := []struct {
		in, want int64
	}{
		{0, 0}, {1, 1}, {q / 2, q / 2}, {q/2 + 1, q/2 + 1 - q}, {q - 1, -1},
	}
	for _, tc := range tests {
		got := fCenter(tc.in, q)
		if got != tc.want {
			t.Errorf("fCenter(%d, %d) = %d, want %d", tc.in, q, got, tc.want)
		}
	}
}

func TestFalconEnhancedPolyMulIdentity(t *testing.T) {
	q := int64(12289)
	n := 4
	// Multiply by identity polynomial [1, 0, 0, 0].
	a := []int64{3, 7, 11, 2}
	id := []int64{1, 0, 0, 0}
	c := fRingMul(a, id, n, q)
	for i := range a {
		if c[i] != a[i] {
			t.Fatalf("poly * 1 [%d]: got %d, want %d", i, c[i], a[i])
		}
	}
}

func TestFalconEnhancedPolyMulReduction(t *testing.T) {
	q := int64(12289)
	n := 4
	// x^2 * x^3 = x^5 = -x (mod x^4+1)
	a := []int64{0, 0, 1, 0}
	b := []int64{0, 0, 0, 1}
	c := fRingMul(a, b, n, q)
	expected := []int64{0, q - 1, 0, 0}
	for i := range c {
		if c[i] != expected[i] {
			t.Fatalf("poly mul reduction [%d]: got %d, want %d", i, c[i], expected[i])
		}
	}
}

func TestFalconEnhancedRingInvert(t *testing.T) {
	q := int64(12289)
	n := 4
	f := []int64{1, 1, 0, 0} // 1 + x
	fInv, err := fRingInvert(f, n, q)
	if err != nil {
		t.Fatalf("fRingInvert failed: %v", err)
	}
	chk := fRingMul(f, fInv, n, q)
	if chk[0] != 1 {
		t.Fatalf("f*fInv[0] = %d, want 1", chk[0])
	}
	for i := 1; i < n; i++ {
		if chk[i] != 0 {
			t.Fatalf("f*fInv[%d] = %d, want 0", i, chk[i])
		}
	}
}

func TestFalconEnhancedDefaultParams(t *testing.T) {
	p := DefaultFalconEnhancedParams()
	if p.Q != 12289 {
		t.Fatalf("Q = %d, want 12289", p.Q)
	}
	if p.N != 64 {
		t.Fatalf("N = %d, want 64", p.N)
	}
	p2 := Falcon1024EnhancedParams()
	if p2.N <= p.N {
		t.Fatal("1024 params N should be > 512 params N")
	}
}

func TestFalconEnhancedNormSqCentered(t *testing.T) {
	q := int64(12289)
	// All zeros -> norm 0.
	zeros := make([]int64, 4)
	if fNormSqCentered(zeros, q) != 0 {
		t.Fatal("norm of zero poly should be 0")
	}
	// [1, 0, 0, 0] -> norm 1.
	one := []int64{1, 0, 0, 0}
	if fNormSqCentered(one, q) != 1 {
		t.Fatal("norm of [1,0,0,0] should be 1")
	}
	// [q-1, 0, 0, 0] centered = [-1, 0, 0, 0] -> norm 1.
	qm1 := []int64{q - 1, 0, 0, 0}
	if fNormSqCentered(qm1, q) != 1 {
		t.Fatalf("norm of [q-1,0,0,0] should be 1, got %v", fNormSqCentered(qm1, q))
	}
}

func TestFalconEnhancedCrossMessageVerify(t *testing.T) {
	s := falconSigner512()
	kp, _ := s.GenerateKey()
	msg1 := []byte("message one")
	msg2 := []byte("message two")
	sig1, err := s.Sign(kp, msg1)
	if err != nil {
		t.Fatal(err)
	}
	sig2, err := s.Sign(kp, msg2)
	if err != nil {
		t.Fatal(err)
	}
	// sig1 should not verify with msg2 and vice versa.
	ok, _ := s.Verify(kp.PublicKey, msg2, sig1)
	if ok {
		t.Fatal("sig1 should not verify with msg2")
	}
	ok, _ = s.Verify(kp.PublicKey, msg1, sig2)
	if ok {
		t.Fatal("sig2 should not verify with msg1")
	}
}

func TestFalconEnhancedSignAndVerify1024(t *testing.T) {
	s := falconSigner1024()
	kp, err := s.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("falcon 1024 test")
	sig, err := s.Sign(kp, msg)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := s.Verify(kp.PublicKey, msg, sig)
	if err != nil {
		t.Fatalf("verify error: %v", err)
	}
	if !ok {
		t.Fatal("valid 1024 signature rejected")
	}
}
