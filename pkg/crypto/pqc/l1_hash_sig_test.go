package pqc

import (
	"sync"
	"testing"
)

func TestL1HashSignerNew(t *testing.T) {
	signer, err := NewL1HashSigner(DefaultL1HashSignerConfig())
	if err != nil {
		t.Fatalf("NewL1HashSigner: %v", err)
	}
	if signer == nil {
		t.Fatal("NewL1HashSigner returned nil")
	}
}

func TestL1HashSignerInvalidHeight(t *testing.T) {
	tests := []int{0, -1, 21, 100}
	for _, h := range tests {
		cfg := DefaultL1HashSignerConfig()
		cfg.TreeHeight = h
		_, err := NewL1HashSigner(cfg)
		if err != ErrL1HashInvalidTreeHeight {
			t.Errorf("NewL1HashSigner(height=%d): got %v, want %v", h, err, ErrL1HashInvalidTreeHeight)
		}
	}
}

func TestL1HashSignerValidHeights(t *testing.T) {
	for _, h := range []int{1, 5, 10, 20} {
		cfg := DefaultL1HashSignerConfig()
		cfg.TreeHeight = h
		s, err := NewL1HashSigner(cfg)
		if err != nil {
			t.Errorf("NewL1HashSigner(height=%d): unexpected error: %v", h, err)
		}
		if s == nil {
			t.Errorf("NewL1HashSigner(height=%d) returned nil", h)
		}
	}
}

func TestL1HashGenerateKeyPair(t *testing.T) {
	cfg := DefaultL1HashSignerConfig()
	cfg.TreeHeight = 4 // small tree for fast tests
	signer, _ := NewL1HashSigner(cfg)

	kp, err := signer.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if len(kp.PublicRoot) != 32 {
		t.Errorf("PublicRoot len = %d, want 32", len(kp.PublicRoot))
	}
	if len(kp.PrivateSeeds) != l1SeedSize {
		t.Errorf("PrivateSeeds len = %d, want %d", len(kp.PrivateSeeds), l1SeedSize)
	}
	if kp.RemainingSignatures != 16 { // 2^4
		t.Errorf("RemainingSignatures = %d, want 16", kp.RemainingSignatures)
	}
	if kp.CreatedTime.IsZero() {
		t.Error("CreatedTime is zero")
	}
}

func TestL1HashSignAndVerify(t *testing.T) {
	cfg := DefaultL1HashSignerConfig()
	cfg.TreeHeight = 4
	signer, _ := NewL1HashSigner(cfg)
	kp, _ := signer.GenerateKeyPair()

	msg := []byte("test message for L1 hash-based signing")
	sig, err := signer.Sign(msg)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if sig.LeafIndex != 0 {
		t.Errorf("LeafIndex = %d, want 0", sig.LeafIndex)
	}
	if len(sig.AuthPath) != 4 {
		t.Errorf("AuthPath len = %d, want 4", len(sig.AuthPath))
	}
	if len(sig.OTSSignature) != l1ChainLen16 {
		t.Errorf("OTSSignature chains = %d, want %d", len(sig.OTSSignature), l1ChainLen16)
	}

	valid, err := signer.Verify(msg, sig, kp.PublicRoot)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !valid {
		t.Error("Verify returned false for valid signature")
	}
}

func TestL1HashSignMultiple(t *testing.T) {
	cfg := DefaultL1HashSignerConfig()
	cfg.TreeHeight = 3 // 8 signatures max
	signer, _ := NewL1HashSigner(cfg)
	kp, _ := signer.GenerateKeyPair()

	for i := 0; i < 8; i++ {
		msg := []byte{byte(i), 'h', 'e', 'l', 'l', 'o'}
		sig, err := signer.Sign(msg)
		if err != nil {
			t.Fatalf("Sign(%d): %v", i, err)
		}
		if sig.LeafIndex != uint32(i) {
			t.Errorf("Sign(%d): LeafIndex = %d, want %d", i, sig.LeafIndex, i)
		}
		valid, err := signer.Verify(msg, sig, kp.PublicRoot)
		if err != nil || !valid {
			t.Fatalf("Verify(%d) failed: valid=%v err=%v", i, valid, err)
		}
	}

	// 9th signature should fail.
	_, err := signer.Sign([]byte("overflow"))
	if err != ErrL1HashKeysExhausted {
		t.Errorf("Sign after exhaustion: got %v, want %v", err, ErrL1HashKeysExhausted)
	}
}

func TestL1HashVerifyWrongMessage(t *testing.T) {
	cfg := DefaultL1HashSignerConfig()
	cfg.TreeHeight = 4
	signer, _ := NewL1HashSigner(cfg)
	kp, _ := signer.GenerateKeyPair()

	sig, _ := signer.Sign([]byte("correct message"))

	valid, _ := signer.Verify([]byte("wrong message"), sig, kp.PublicRoot)
	if valid {
		t.Error("Verify should fail with wrong message")
	}
}

func TestL1HashVerifyWrongPubRoot(t *testing.T) {
	cfg := DefaultL1HashSignerConfig()
	cfg.TreeHeight = 4
	signer, _ := NewL1HashSigner(cfg)
	signer.GenerateKeyPair()

	sig, _ := signer.Sign([]byte("message"))

	wrongRoot := make([]byte, 32)
	wrongRoot[0] = 0xff
	valid, _ := signer.Verify([]byte("message"), sig, wrongRoot)
	if valid {
		t.Error("Verify should fail with wrong public root")
	}
}

func TestL1HashVerifyNilInputs(t *testing.T) {
	cfg := DefaultL1HashSignerConfig()
	cfg.TreeHeight = 4
	signer, _ := NewL1HashSigner(cfg)

	// Nil message.
	valid, err := signer.Verify(nil, &L1HashSignature{}, []byte("root"))
	if valid || err != ErrL1HashInvalidSignature {
		t.Error("should reject nil message")
	}

	// Nil signature.
	valid, err = signer.Verify([]byte("msg"), nil, []byte("root"))
	if valid || err != ErrL1HashInvalidSignature {
		t.Error("should reject nil signature")
	}

	// Empty pub root.
	valid, err = signer.Verify([]byte("msg"), &L1HashSignature{}, nil)
	if valid || err != ErrL1HashInvalidSignature {
		t.Error("should reject empty pub root")
	}
}

func TestL1HashSignEmptyMessage(t *testing.T) {
	cfg := DefaultL1HashSignerConfig()
	cfg.TreeHeight = 4
	signer, _ := NewL1HashSigner(cfg)
	signer.GenerateKeyPair()

	_, err := signer.Sign([]byte{})
	if err != ErrL1HashInvalidSignature {
		t.Errorf("Sign empty: got %v, want %v", err, ErrL1HashInvalidSignature)
	}
}

func TestL1HashRemainingSignatures(t *testing.T) {
	cfg := DefaultL1HashSignerConfig()
	cfg.TreeHeight = 3 // 8 max
	signer, _ := NewL1HashSigner(cfg)

	// Before init.
	if signer.RemainingSignatures() != 0 {
		t.Errorf("RemainingSignatures before init = %d, want 0", signer.RemainingSignatures())
	}

	signer.GenerateKeyPair()
	if signer.RemainingSignatures() != 8 {
		t.Errorf("RemainingSignatures = %d, want 8", signer.RemainingSignatures())
	}

	signer.Sign([]byte("msg1"))
	signer.Sign([]byte("msg2"))
	if signer.RemainingSignatures() != 6 {
		t.Errorf("RemainingSignatures after 2 sigs = %d, want 6", signer.RemainingSignatures())
	}
}

func TestL1HashIsExhausted(t *testing.T) {
	cfg := DefaultL1HashSignerConfig()
	cfg.TreeHeight = 1 // only 2 signatures
	signer, _ := NewL1HashSigner(cfg)

	// Before init.
	if !signer.IsExhausted() {
		t.Error("IsExhausted before init should be true")
	}

	signer.GenerateKeyPair()
	if signer.IsExhausted() {
		t.Error("IsExhausted after keygen should be false")
	}

	signer.Sign([]byte("msg1"))
	if signer.IsExhausted() {
		t.Error("IsExhausted after 1 sig should be false")
	}

	signer.Sign([]byte("msg2"))
	if !signer.IsExhausted() {
		t.Error("IsExhausted after 2 sigs should be true")
	}
}

func TestL1HashSignBeforeInit(t *testing.T) {
	cfg := DefaultL1HashSignerConfig()
	cfg.TreeHeight = 4
	signer, _ := NewL1HashSigner(cfg)

	_, err := signer.Sign([]byte("message"))
	if err != ErrL1HashKeysExhausted {
		t.Errorf("Sign before init: got %v, want %v", err, ErrL1HashKeysExhausted)
	}
}

func TestL1HashWinternitzW4(t *testing.T) {
	cfg := L1HashSignerConfig{
		TreeHeight:  3,
		WinternitzW: 4,
	}
	signer, err := NewL1HashSigner(cfg)
	if err != nil {
		t.Fatalf("NewL1HashSigner(w=4): %v", err)
	}
	kp, _ := signer.GenerateKeyPair()

	msg := []byte("winternitz w=4 test")
	sig, err := signer.Sign(msg)
	if err != nil {
		t.Fatalf("Sign(w=4): %v", err)
	}
	if len(sig.OTSSignature) != 133 {
		t.Errorf("OTSSignature chains = %d, want 133 for w=4", len(sig.OTSSignature))
	}

	valid, err := signer.Verify(msg, sig, kp.PublicRoot)
	if err != nil {
		t.Fatalf("Verify(w=4): %v", err)
	}
	if !valid {
		t.Error("Verify(w=4) returned false for valid signature")
	}
}

func TestL1HashMaxSignaturesCap(t *testing.T) {
	cfg := L1HashSignerConfig{
		TreeHeight:    4, // would allow 16
		MaxSignatures: 3, // but capped at 3
		WinternitzW:   16,
	}
	signer, _ := NewL1HashSigner(cfg)
	signer.GenerateKeyPair()

	if signer.RemainingSignatures() != 3 {
		t.Errorf("RemainingSignatures = %d, want 3", signer.RemainingSignatures())
	}

	for i := 0; i < 3; i++ {
		_, err := signer.Sign([]byte{byte(i)})
		if err != nil {
			t.Fatalf("Sign(%d): %v", i, err)
		}
	}

	_, err := signer.Sign([]byte("overflow"))
	if err != ErrL1HashKeysExhausted {
		t.Errorf("Sign beyond cap: got %v, want %v", err, ErrL1HashKeysExhausted)
	}
}

func TestL1HashConcurrentSign(t *testing.T) {
	cfg := DefaultL1HashSignerConfig()
	cfg.TreeHeight = 6 // 64 sigs
	signer, _ := NewL1HashSigner(cfg)
	kp, _ := signer.GenerateKeyPair()

	type sigResult struct {
		sig *L1HashSignature
		msg []byte
	}

	var wg sync.WaitGroup
	results := make(chan sigResult, 64)

	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			msg := []byte{byte(idx), 'c', 'o', 'n', 'c'}
			sig, err := signer.Sign(msg)
			if err != nil {
				return
			}
			results <- sigResult{sig: sig, msg: msg}
		}(i)
	}
	wg.Wait()
	close(results)

	// All 64 signatures should be unique leaf indices.
	seen := make(map[uint32]bool)
	for r := range results {
		if seen[r.sig.LeafIndex] {
			t.Errorf("duplicate leaf index: %d", r.sig.LeafIndex)
		}
		seen[r.sig.LeafIndex] = true

		valid, err := signer.Verify(r.msg, r.sig, kp.PublicRoot)
		if err != nil || !valid {
			t.Errorf("concurrent sig at leaf %d failed verification", r.sig.LeafIndex)
		}
	}

	if signer.IsExhausted() != true {
		t.Error("signer should be exhausted after 64 signatures")
	}
}

func TestL1HashKeyPairRegeneration(t *testing.T) {
	cfg := DefaultL1HashSignerConfig()
	cfg.TreeHeight = 3
	signer, _ := NewL1HashSigner(cfg)

	kp1, _ := signer.GenerateKeyPair()

	// Sign once with first key.
	signer.Sign([]byte("first"))

	// Re-generate resets the signer.
	kp2, _ := signer.GenerateKeyPair()

	if signer.RemainingSignatures() != 8 {
		t.Errorf("RemainingSignatures after regen = %d, want 8", signer.RemainingSignatures())
	}

	// Public roots should differ (different random seeds).
	same := true
	for i := range kp1.PublicRoot {
		if kp1.PublicRoot[i] != kp2.PublicRoot[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("regenerated key pair has same public root (extremely unlikely)")
	}
}

func TestL1HashDefaultConfig(t *testing.T) {
	cfg := DefaultL1HashSignerConfig()
	if cfg.TreeHeight != l1HeightDefault {
		t.Errorf("TreeHeight = %d, want %d", cfg.TreeHeight, l1HeightDefault)
	}
	if cfg.WinternitzW != l1WDefault {
		t.Errorf("WinternitzW = %d, want %d", cfg.WinternitzW, l1WDefault)
	}
	if cfg.HashFunction != "keccak256" {
		t.Errorf("HashFunction = %s, want keccak256", cfg.HashFunction)
	}
}

func TestL1HashInvalidWinternitzDefaultsTo16(t *testing.T) {
	cfg := L1HashSignerConfig{
		TreeHeight:  3,
		WinternitzW: 8, // invalid, should default to 16
	}
	signer, err := NewL1HashSigner(cfg)
	if err != nil {
		t.Fatalf("NewL1HashSigner: %v", err)
	}
	if signer.winternitzW != 16 {
		t.Errorf("winternitzW = %d, want 16 (defaulted)", signer.winternitzW)
	}
}
