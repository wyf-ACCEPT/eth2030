package pqc

import (
	"testing"
)

func TestMLDSASignerKeyGen(t *testing.T) {
	signer := NewMLDSASigner()
	kp, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	if len(kp.PublicKey) != MLDSAPublicKeySize {
		t.Errorf("public key size = %d, want %d", len(kp.PublicKey), MLDSAPublicKeySize)
	}
	if len(kp.SecretKey) != MLDSAPrivateKeySize {
		t.Errorf("secret key size = %d, want %d", len(kp.SecretKey), MLDSAPrivateKeySize)
	}
}

func TestMLDSASignerKeyGenUniqueness(t *testing.T) {
	signer := NewMLDSASigner()
	kp1, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey 1 failed: %v", err)
	}
	kp2, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey 2 failed: %v", err)
	}
	// Keys should be different (random seed).
	same := true
	for i := range kp1.PublicKey {
		if kp1.PublicKey[i] != kp2.PublicKey[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("two generated key pairs should not be identical")
	}
}

func TestMLDSASignerSignAndVerify(t *testing.T) {
	signer := NewMLDSASigner()
	kp, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	msg := []byte("test message for ML-DSA-65 signing")
	sig, err := signer.Sign(kp, msg)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	if len(sig) != MLDSASignatureSize {
		t.Errorf("signature size = %d, want %d", len(sig), MLDSASignatureSize)
	}

	// Verify the signature.
	if !signer.Verify(kp.PublicKey, msg, sig) {
		t.Error("Verify returned false for valid signature")
	}
}

func TestMLDSASignerVerifyWrongMessage(t *testing.T) {
	signer := NewMLDSASigner()
	kp, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	msg := []byte("original message")
	sig, err := signer.Sign(kp, msg)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	wrongMsg := []byte("different message")
	if signer.Verify(kp.PublicKey, wrongMsg, sig) {
		t.Error("Verify should return false for wrong message")
	}
}

func TestMLDSASignerVerifyWrongKey(t *testing.T) {
	signer := NewMLDSASigner()
	kp1, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey 1 failed: %v", err)
	}
	kp2, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey 2 failed: %v", err)
	}

	msg := []byte("test message")
	sig, err := signer.Sign(kp1, msg)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	// Verify with wrong public key should fail.
	if signer.Verify(kp2.PublicKey, msg, sig) {
		t.Error("Verify should return false for wrong public key")
	}
}

func TestMLDSASignerNilKeyErrors(t *testing.T) {
	signer := NewMLDSASigner()

	_, err := signer.Sign(nil, []byte("msg"))
	if err != ErrMLDSANilKey {
		t.Errorf("Sign(nil key) error = %v, want ErrMLDSANilKey", err)
	}

	kp, _ := signer.GenerateKey()
	_, err = signer.Sign(kp, nil)
	if err != ErrMLDSAEmptyMsg {
		t.Errorf("Sign(nil msg) error = %v, want ErrMLDSAEmptyMsg", err)
	}

	_, err = signer.Sign(kp, []byte{})
	if err != ErrMLDSAEmptyMsg {
		t.Errorf("Sign(empty msg) error = %v, want ErrMLDSAEmptyMsg", err)
	}
}

func TestMLDSASignerVerifyBadInputs(t *testing.T) {
	signer := NewMLDSASigner()

	// Empty inputs.
	if signer.Verify(nil, []byte("msg"), make([]byte, MLDSASignatureSize)) {
		t.Error("Verify should reject nil pubkey")
	}
	if signer.Verify(make([]byte, MLDSAPublicKeySize), nil, make([]byte, MLDSASignatureSize)) {
		t.Error("Verify should reject nil message")
	}
	if signer.Verify(make([]byte, MLDSAPublicKeySize), []byte("msg"), nil) {
		t.Error("Verify should reject nil signature")
	}

	// Wrong sizes.
	if signer.Verify(make([]byte, 10), []byte("msg"), make([]byte, MLDSASignatureSize)) {
		t.Error("Verify should reject short pubkey")
	}
	if signer.Verify(make([]byte, MLDSAPublicKeySize), []byte("msg"), make([]byte, 10)) {
		t.Error("Verify should reject short signature")
	}
}

func TestMLDSASignerDeterministicSigning(t *testing.T) {
	signer := NewMLDSASigner()
	kp, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	msg := []byte("deterministic test")
	sig1, err := signer.Sign(kp, msg)
	if err != nil {
		t.Fatalf("Sign 1 failed: %v", err)
	}
	sig2, err := signer.Sign(kp, msg)
	if err != nil {
		t.Fatalf("Sign 2 failed: %v", err)
	}

	// Both signatures should verify.
	if !signer.Verify(kp.PublicKey, msg, sig1) {
		t.Error("sig1 verification failed")
	}
	if !signer.Verify(kp.PublicKey, msg, sig2) {
		t.Error("sig2 verification failed")
	}
}

func TestMLDSASignerAlgorithm(t *testing.T) {
	signer := NewMLDSASigner()
	if signer.Algorithm() != DILITHIUM3 {
		t.Errorf("Algorithm() = %d, want DILITHIUM3 (%d)", signer.Algorithm(), DILITHIUM3)
	}
}

func TestMLDSASignerConstants(t *testing.T) {
	// N=64, 4 bytes per coefficient: pk = rho(32) + k*N*4 = 32+6*64*4 = 1568.
	if MLDSAPublicKeySize != 1568 {
		t.Errorf("MLDSAPublicKeySize = %d, want 1568", MLDSAPublicKeySize)
	}
	// sk = rho(32) + K(32) + tr(64) + s1(5*64*4) + s2(6*64*4) + t0(6*64*4) = 4480.
	if MLDSAPrivateKeySize != 4480 {
		t.Errorf("MLDSAPrivateKeySize = %d, want 4480", MLDSAPrivateKeySize)
	}
	// sig = cTilde(32) + z(5*64*4) + hint = 1376.
	if MLDSASignatureSize != 1376 {
		t.Errorf("MLDSASignatureSize = %d, want 1376", MLDSASignatureSize)
	}
}

func TestMLDSASignerLatticeParameters(t *testing.T) {
	// Verify ML-DSA-65 lattice parameters. N=64 for schoolbook mul efficiency,
	// k=6/l=5 match FIPS 204, Q is the Dilithium prime.
	if mldsaN != 64 {
		t.Errorf("N = %d, want 64", mldsaN)
	}
	if mldsaQ != 8380417 {
		t.Errorf("Q = %d, want 8380417", mldsaQ)
	}
	if mldsaK != 6 {
		t.Errorf("K = %d, want 6", mldsaK)
	}
	if mldsaL != 5 {
		t.Errorf("L = %d, want 5", mldsaL)
	}
	if mldsaEta != 4 {
		t.Errorf("Eta = %d, want 4", mldsaEta)
	}
	if mldsaTau != 12 {
		t.Errorf("Tau = %d, want 12", mldsaTau)
	}
}

func TestMLDSAModQ(t *testing.T) {
	tests := []struct {
		input    int64
		expected int64
	}{
		{0, 0},
		{1, 1},
		{mldsaQ, 0},
		{mldsaQ + 1, 1},
		{-1, mldsaQ - 1},
		{-mldsaQ, 0},
	}
	for _, tt := range tests {
		got := mldsaModQ(tt.input)
		if got != tt.expected {
			t.Errorf("mldsaModQ(%d) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

func TestMLDSAPolyAddSub(t *testing.T) {
	var a, b mldsaPoly
	a[0] = 100
	a[1] = 200
	b[0] = 50
	b[1] = mldsaQ - 50 // -50 mod Q

	sum := mldsaPolyAdd(a, b)
	if sum[0] != 150 {
		t.Errorf("add[0] = %d, want 150", sum[0])
	}
	if sum[1] != 150 {
		t.Errorf("add[1] = %d, want 150", sum[1])
	}

	diff := mldsaPolySub(a, b)
	if diff[0] != 50 {
		t.Errorf("sub[0] = %d, want 50", diff[0])
	}
	if diff[1] != 250 {
		t.Errorf("sub[1] = %d, want 250", diff[1])
	}
}

func TestMLDSACheckNorm(t *testing.T) {
	var p mldsaPoly
	p[0] = 100
	p[1] = 200
	if !mldsaCheckNorm(p, 300) {
		t.Error("checkNorm should pass for small coefficients")
	}
	p[2] = mldsaQ - 50 // equivalent to -50
	if !mldsaCheckNorm(p, 300) {
		t.Error("checkNorm should pass for small negative coefficients")
	}
}

func TestMLDSASHAKE256(t *testing.T) {
	out := mldsaSHAKE256([]byte("test"), 64)
	if len(out) != 64 {
		t.Errorf("SHAKE256 output length = %d, want 64", len(out))
	}
	// Should be non-zero.
	allZero := true
	for _, b := range out {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("SHAKE256 output should not be all zeros")
	}
}

func TestMLDSAExpandA(t *testing.T) {
	rho := mldsaSHAKE256([]byte("test-rho"), 32)
	a := mldsaExpandA(rho)
	if len(a) != mldsaK {
		t.Fatalf("A matrix rows = %d, want %d", len(a), mldsaK)
	}
	for i := 0; i < mldsaK; i++ {
		if len(a[i]) != mldsaL {
			t.Fatalf("A matrix cols[%d] = %d, want %d", i, len(a[i]), mldsaL)
		}
	}
	// Check coefficients are in range [0, Q).
	for i := 0; i < mldsaK; i++ {
		for j := 0; j < mldsaL; j++ {
			for k := 0; k < mldsaN; k++ {
				if a[i][j][k] < 0 || a[i][j][k] >= mldsaQ {
					t.Errorf("A[%d][%d][%d] = %d out of range [0, Q)", i, j, k, a[i][j][k])
				}
			}
		}
	}
}

func TestMLDSASampleInBall(t *testing.T) {
	seed := mldsaSHAKE256([]byte("challenge-seed"), 32)
	c := mldsaSampleInBall(seed)

	// Count non-zero coefficients.
	nonZero := 0
	for i := 0; i < mldsaN; i++ {
		if c[i] != 0 {
			nonZero++
		}
	}
	// Should have non-zero entries (tau positions shuffled via Fisher-Yates).
	if nonZero == 0 {
		t.Error("challenge polynomial should have non-zero coefficients")
	}
	if nonZero > mldsaN {
		t.Errorf("challenge polynomial has %d non-zero coeffs, exceeds N=%d", nonZero, mldsaN)
	}
}
