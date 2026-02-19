package pqc

import (
	"sync"
	"testing"
)

func TestNewPQSignatureScheme(t *testing.T) {
	tests := []struct {
		name      string
		threshold int
		total     int
		wantErr   error
	}{
		{"valid 2-of-3", 2, 3, nil},
		{"valid 1-of-1", 1, 1, nil},
		{"valid 3-of-5", 3, 5, nil},
		{"valid n-of-n", 5, 5, nil},
		{"zero threshold", 0, 3, ErrPQSigInvalidThreshold},
		{"negative threshold", -1, 3, ErrPQSigInvalidThreshold},
		{"threshold > total", 4, 3, ErrPQSigInvalidThreshold},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme, err := NewPQSignatureScheme(tt.threshold, tt.total)
			if err != tt.wantErr {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
			if err == nil {
				if scheme.Threshold() != tt.threshold {
					t.Errorf("threshold: want %d, got %d", tt.threshold, scheme.Threshold())
				}
				if scheme.TotalSigners() != tt.total {
					t.Errorf("totalSigners: want %d, got %d", tt.total, scheme.TotalSigners())
				}
			}
		})
	}
}

func TestGenerateKeys(t *testing.T) {
	scheme, err := NewPQSignatureScheme(2, 3)
	if err != nil {
		t.Fatalf("NewPQSignatureScheme: %v", err)
	}

	keySet, err := scheme.GenerateKeys()
	if err != nil {
		t.Fatalf("GenerateKeys: %v", err)
	}

	if keySet == nil {
		t.Fatal("keySet is nil")
	}
	if len(keySet.PublicKey) != 32 {
		t.Errorf("public key length: want 32, got %d", len(keySet.PublicKey))
	}
	if keySet.Threshold != 2 {
		t.Errorf("threshold: want 2, got %d", keySet.Threshold)
	}
	if len(keySet.KeyShares) != 3 {
		t.Fatalf("key shares count: want 3, got %d", len(keySet.KeyShares))
	}

	for i, share := range keySet.KeyShares {
		if share.Index != i+1 {
			t.Errorf("share %d index: want %d, got %d", i, i+1, share.Index)
		}
		if len(share.ShareData) != 32 {
			t.Errorf("share %d data length: want 32, got %d", i, len(share.ShareData))
		}
		if len(share.VerificationKey) != 32 {
			t.Errorf("share %d verification key length: want 32, got %d", i, len(share.VerificationKey))
		}
	}
}

func TestGenerateKeysUniqueness(t *testing.T) {
	scheme, err := NewPQSignatureScheme(2, 5)
	if err != nil {
		t.Fatalf("NewPQSignatureScheme: %v", err)
	}

	ks1, err := scheme.GenerateKeys()
	if err != nil {
		t.Fatalf("first GenerateKeys: %v", err)
	}
	ks2, err := scheme.GenerateKeys()
	if err != nil {
		t.Fatalf("second GenerateKeys: %v", err)
	}

	if string(ks1.PublicKey) == string(ks2.PublicKey) {
		t.Error("two key generation rounds should produce different public keys")
	}

	// Shares within the same keyset should be distinct.
	for i := 0; i < len(ks1.KeyShares); i++ {
		for j := i + 1; j < len(ks1.KeyShares); j++ {
			if string(ks1.KeyShares[i].ShareData) == string(ks1.KeyShares[j].ShareData) {
				t.Errorf("shares %d and %d have identical data", i, j)
			}
		}
	}
}

func TestSignShareAndVerify(t *testing.T) {
	scheme, err := NewPQSignatureScheme(2, 3)
	if err != nil {
		t.Fatalf("NewPQSignatureScheme: %v", err)
	}

	keySet, err := scheme.GenerateKeys()
	if err != nil {
		t.Fatalf("GenerateKeys: %v", err)
	}

	message := []byte("hello post-quantum world")

	for _, share := range keySet.KeyShares {
		sig, err := scheme.SignShare(share, message)
		if err != nil {
			t.Fatalf("SignShare(%d): %v", share.Index, err)
		}

		if sig.Index != share.Index {
			t.Errorf("sig index: want %d, got %d", share.Index, sig.Index)
		}
		if len(sig.ShareSignature) == 0 {
			t.Error("share signature is empty")
		}
		if len(sig.VerificationData) == 0 {
			t.Error("verification data is empty")
		}

		// Verify the share signature.
		if !scheme.VerifySignatureShare(keySet.PublicKey, sig, message) {
			t.Errorf("valid signature share %d failed verification", share.Index)
		}
	}
}

func TestSignShareDeterministic(t *testing.T) {
	scheme, err := NewPQSignatureScheme(2, 3)
	if err != nil {
		t.Fatalf("NewPQSignatureScheme: %v", err)
	}

	keySet, err := scheme.GenerateKeys()
	if err != nil {
		t.Fatalf("GenerateKeys: %v", err)
	}

	message := []byte("determinism check")
	share := keySet.KeyShares[0]

	sig1, err := scheme.SignShare(share, message)
	if err != nil {
		t.Fatalf("first SignShare: %v", err)
	}
	sig2, err := scheme.SignShare(share, message)
	if err != nil {
		t.Fatalf("second SignShare: %v", err)
	}

	if string(sig1.ShareSignature) != string(sig2.ShareSignature) {
		t.Error("same share + message should produce same signature")
	}
}

func TestSignShareErrors(t *testing.T) {
	scheme, err := NewPQSignatureScheme(2, 3)
	if err != nil {
		t.Fatalf("NewPQSignatureScheme: %v", err)
	}

	_, err = scheme.SignShare(nil, []byte("msg"))
	if err != ErrPQSigNilShare {
		t.Errorf("nil share: want ErrPQSigNilShare, got %v", err)
	}

	_, err = scheme.SignShare(&PQKeyShare{}, []byte("msg"))
	if err != ErrPQSigNilShare {
		t.Errorf("empty share: want ErrPQSigNilShare, got %v", err)
	}

	keySet, _ := scheme.GenerateKeys()
	_, err = scheme.SignShare(keySet.KeyShares[0], nil)
	if err != ErrPQSigEmptyMessage {
		t.Errorf("nil message: want ErrPQSigEmptyMessage, got %v", err)
	}
	_, err = scheme.SignShare(keySet.KeyShares[0], []byte{})
	if err != ErrPQSigEmptyMessage {
		t.Errorf("empty message: want ErrPQSigEmptyMessage, got %v", err)
	}
}

func TestVerifySignatureShareRejectsInvalid(t *testing.T) {
	scheme, _ := NewPQSignatureScheme(2, 3)

	if scheme.VerifySignatureShare(nil, &PQSignatureShare{}, []byte("msg")) {
		t.Error("nil public key should fail")
	}
	if scheme.VerifySignatureShare([]byte("pk"), nil, []byte("msg")) {
		t.Error("nil share should fail")
	}
	if scheme.VerifySignatureShare([]byte("pk"), &PQSignatureShare{}, nil) {
		t.Error("nil message should fail")
	}
	if scheme.VerifySignatureShare([]byte("pk"), &PQSignatureShare{
		ShareSignature: []byte{}, VerificationData: []byte("vd"),
	}, []byte("msg")) {
		t.Error("empty share signature should fail")
	}
	if scheme.VerifySignatureShare([]byte("pk"), &PQSignatureShare{
		ShareSignature: make([]byte, 32), VerificationData: make([]byte, 32),
	}, []byte("msg")) {
		t.Error("all-zero share signature should fail")
	}
}

func TestCombineSignatures(t *testing.T) {
	scheme, err := NewPQSignatureScheme(2, 3)
	if err != nil {
		t.Fatalf("NewPQSignatureScheme: %v", err)
	}

	keySet, err := scheme.GenerateKeys()
	if err != nil {
		t.Fatalf("GenerateKeys: %v", err)
	}

	message := []byte("combine test message")

	// Sign with all 3 shares.
	sigShares := make([]*PQSignatureShare, 3)
	for i, share := range keySet.KeyShares {
		sig, err := scheme.SignShare(share, message)
		if err != nil {
			t.Fatalf("SignShare(%d): %v", i, err)
		}
		sigShares[i] = sig
	}

	// Combine with threshold (2) shares.
	fullSig, err := scheme.CombineSignatures(sigShares[:2], message)
	if err != nil {
		t.Fatalf("CombineSignatures: %v", err)
	}

	if len(fullSig) != 64 {
		t.Errorf("full signature length: want 64, got %d", len(fullSig))
	}

	// Verify the full signature.
	if !scheme.VerifyFullSignature(keySet.PublicKey, fullSig, message) {
		t.Fatal("valid full signature failed verification")
	}
}

func TestCombineSignaturesAllShares(t *testing.T) {
	scheme, err := NewPQSignatureScheme(3, 5)
	if err != nil {
		t.Fatalf("NewPQSignatureScheme: %v", err)
	}

	keySet, err := scheme.GenerateKeys()
	if err != nil {
		t.Fatalf("GenerateKeys: %v", err)
	}

	message := []byte("all shares combine")
	sigShares := make([]*PQSignatureShare, len(keySet.KeyShares))
	for i, share := range keySet.KeyShares {
		sig, err := scheme.SignShare(share, message)
		if err != nil {
			t.Fatalf("SignShare(%d): %v", i, err)
		}
		sigShares[i] = sig
	}

	fullSig, err := scheme.CombineSignatures(sigShares, message)
	if err != nil {
		t.Fatalf("CombineSignatures: %v", err)
	}

	if !scheme.VerifyFullSignature(keySet.PublicKey, fullSig, message) {
		t.Fatal("full signature from all shares failed verification")
	}
}

func TestCombineSignaturesErrors(t *testing.T) {
	scheme, _ := NewPQSignatureScheme(3, 5)
	keySet, _ := scheme.GenerateKeys()
	message := []byte("error cases")
	sig1, _ := scheme.SignShare(keySet.KeyShares[0], message)
	sig2, _ := scheme.SignShare(keySet.KeyShares[1], message)

	if _, err := scheme.CombineSignatures([]*PQSignatureShare{sig1, sig2}, message); err != ErrPQSigInsufficientSigs {
		t.Errorf("insufficient shares: want ErrPQSigInsufficientSigs, got %v", err)
	}
	if _, err := scheme.CombineSignatures([]*PQSignatureShare{sig1, sig2, sig2}, nil); err != ErrPQSigEmptyMessage {
		t.Errorf("empty message: want ErrPQSigEmptyMessage, got %v", err)
	}
	if _, err := scheme.CombineSignatures([]*PQSignatureShare{sig1, sig2, sig1}, message); err != ErrPQSigDuplicateIndex {
		t.Errorf("duplicate index: want ErrPQSigDuplicateIndex, got %v", err)
	}
	if _, err := scheme.CombineSignatures([]*PQSignatureShare{sig1, nil, sig2}, message); err != ErrPQSigInvalidShare {
		t.Errorf("nil share: want ErrPQSigInvalidShare, got %v", err)
	}
}

func TestVerifyFullSignature(t *testing.T) {
	scheme, err := NewPQSignatureScheme(2, 3)
	if err != nil {
		t.Fatalf("NewPQSignatureScheme: %v", err)
	}

	keySet, err := scheme.GenerateKeys()
	if err != nil {
		t.Fatalf("GenerateKeys: %v", err)
	}

	message := []byte("verify full sig test")

	sigShares := make([]*PQSignatureShare, 2)
	for i := 0; i < 2; i++ {
		sig, _ := scheme.SignShare(keySet.KeyShares[i], message)
		sigShares[i] = sig
	}

	fullSig, err := scheme.CombineSignatures(sigShares, message)
	if err != nil {
		t.Fatalf("CombineSignatures: %v", err)
	}

	// Valid signature.
	if !scheme.VerifyFullSignature(keySet.PublicKey, fullSig, message) {
		t.Fatal("valid signature should verify")
	}

	// Wrong message.
	if scheme.VerifyFullSignature(keySet.PublicKey, fullSig, []byte("wrong message")) {
		t.Fatal("wrong message should fail verification")
	}

	// Tampered signature.
	tampered := make([]byte, len(fullSig))
	copy(tampered, fullSig)
	tampered[0] ^= 0xff
	if scheme.VerifyFullSignature(keySet.PublicKey, tampered, message) {
		t.Fatal("tampered signature should fail verification")
	}

	// Wrong length.
	if scheme.VerifyFullSignature(keySet.PublicKey, fullSig[:32], message) {
		t.Fatal("truncated signature should fail verification")
	}

	// Empty inputs.
	if scheme.VerifyFullSignature(nil, fullSig, message) {
		t.Fatal("nil public key should fail")
	}
	if scheme.VerifyFullSignature(keySet.PublicKey, nil, message) {
		t.Fatal("nil signature should fail")
	}
	if scheme.VerifyFullSignature(keySet.PublicKey, fullSig, nil) {
		t.Fatal("nil message should fail")
	}
}

func TestDifferentShareSubsets(t *testing.T) {
	// Different subsets of shares should produce different full signatures.
	scheme, err := NewPQSignatureScheme(2, 4)
	if err != nil {
		t.Fatalf("NewPQSignatureScheme: %v", err)
	}

	keySet, err := scheme.GenerateKeys()
	if err != nil {
		t.Fatalf("GenerateKeys: %v", err)
	}

	message := []byte("subset comparison")

	sigShares := make([]*PQSignatureShare, 4)
	for i, share := range keySet.KeyShares {
		sig, _ := scheme.SignShare(share, message)
		sigShares[i] = sig
	}

	// Combine shares {0,1} and {2,3}.
	full1, err := scheme.CombineSignatures(sigShares[:2], message)
	if err != nil {
		t.Fatalf("CombineSignatures(0,1): %v", err)
	}
	full2, err := scheme.CombineSignatures(sigShares[2:4], message)
	if err != nil {
		t.Fatalf("CombineSignatures(2,3): %v", err)
	}

	if string(full1) == string(full2) {
		t.Error("different share subsets should produce different signatures")
	}

	// Both should still verify.
	if !scheme.VerifyFullSignature(keySet.PublicKey, full1, message) {
		t.Fatal("full1 should verify")
	}
	if !scheme.VerifyFullSignature(keySet.PublicKey, full2, message) {
		t.Fatal("full2 should verify")
	}
}

func TestConcurrentSignAndVerify(t *testing.T) {
	scheme, err := NewPQSignatureScheme(2, 5)
	if err != nil {
		t.Fatalf("NewPQSignatureScheme: %v", err)
	}

	keySet, err := scheme.GenerateKeys()
	if err != nil {
		t.Fatalf("GenerateKeys: %v", err)
	}

	message := []byte("concurrent test")

	var wg sync.WaitGroup
	errs := make(chan error, len(keySet.KeyShares))

	for _, share := range keySet.KeyShares {
		wg.Add(1)
		go func(s *PQKeyShare) {
			defer wg.Done()
			sig, err := scheme.SignShare(s, message)
			if err != nil {
				errs <- err
				return
			}
			if !scheme.VerifySignatureShare(keySet.PublicKey, sig, message) {
				errs <- ErrPQSigInvalidShare
			}
		}(share)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent error: %v", err)
	}
}

func TestHmacKeccak(t *testing.T) {
	key, msg := []byte("test key"), []byte("test message")
	h1, h2 := hmacKeccak(key, msg), hmacKeccak(key, msg)
	if string(h1) != string(h2) {
		t.Fatal("hmacKeccak should be deterministic")
	}
	if len(h1) != 32 {
		t.Errorf("hmac output length: want 32, got %d", len(h1))
	}
	if string(h1) == string(hmacKeccak([]byte("other key"), msg)) {
		t.Error("different keys should produce different HMAC")
	}
	if string(h1) == string(hmacKeccak(key, []byte("other message"))) {
		t.Error("different messages should produce different HMAC")
	}
}

func TestEvalHashPoly(t *testing.T) {
	c1 := make([]byte, 32)
	c2 := make([]byte, 32)
	for i := range c1 {
		c1[i] = byte(i + 1)
		c2[i] = byte(32 - i)
	}
	coeffs := [][]byte{c1, c2}
	r1, r2 := evalHashPoly(coeffs, 1), evalHashPoly(coeffs, 2)
	if len(r1) != 32 {
		t.Errorf("poly eval length: want 32, got %d", len(r1))
	}
	if string(r1) == string(r2) {
		t.Error("different evaluation points should produce different results")
	}
	if string(r1) != string(evalHashPoly(coeffs, 1)) {
		t.Error("evalHashPoly should be deterministic")
	}
}

func TestOneOfOneScheme(t *testing.T) {
	// Edge case: 1-of-1 threshold.
	scheme, err := NewPQSignatureScheme(1, 1)
	if err != nil {
		t.Fatalf("NewPQSignatureScheme: %v", err)
	}

	keySet, err := scheme.GenerateKeys()
	if err != nil {
		t.Fatalf("GenerateKeys: %v", err)
	}

	if len(keySet.KeyShares) != 1 {
		t.Fatalf("want 1 share, got %d", len(keySet.KeyShares))
	}

	message := []byte("single signer")
	sig, err := scheme.SignShare(keySet.KeyShares[0], message)
	if err != nil {
		t.Fatalf("SignShare: %v", err)
	}

	fullSig, err := scheme.CombineSignatures([]*PQSignatureShare{sig}, message)
	if err != nil {
		t.Fatalf("CombineSignatures: %v", err)
	}

	if !scheme.VerifyFullSignature(keySet.PublicKey, fullSig, message) {
		t.Fatal("1-of-1 signature should verify")
	}
}
