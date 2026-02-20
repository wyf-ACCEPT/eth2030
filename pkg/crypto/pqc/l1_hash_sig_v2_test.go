package pqc

import (
	"sync"
	"testing"
)

func TestL1HashSignerV2New(t *testing.T) {
	signer := NewL1HashSignerV2(DefaultL1SignerConfig())
	if signer == nil {
		t.Fatal("NewL1HashSignerV2 returned nil")
	}
	if signer.height != l1v2HeightDefault {
		t.Errorf("height = %d, want %d", signer.height, l1v2HeightDefault)
	}
	if signer.winternitzW != l1v2WDefault {
		t.Errorf("winternitzW = %d, want %d", signer.winternitzW, l1v2WDefault)
	}
}

func TestL1HashSignerV2DefaultConfig(t *testing.T) {
	cfg := DefaultL1SignerConfig()
	if cfg.TreeHeight != l1v2HeightDefault {
		t.Errorf("TreeHeight = %d, want %d", cfg.TreeHeight, l1v2HeightDefault)
	}
	if cfg.WinternitzParam != l1v2WDefault {
		t.Errorf("WinternitzParam = %d, want %d", cfg.WinternitzParam, l1v2WDefault)
	}
	if cfg.KeyLifetimeBlocks != 0 {
		t.Errorf("KeyLifetimeBlocks = %d, want 0", cfg.KeyLifetimeBlocks)
	}
}

func TestL1HashSignerV2InvalidHeightDefaults(t *testing.T) {
	// Height < 1 defaults.
	s1 := NewL1HashSignerV2(L1SignerConfig{TreeHeight: 0})
	if s1.height != l1v2HeightDefault {
		t.Errorf("height=0 defaulted to %d, want %d", s1.height, l1v2HeightDefault)
	}

	// Height > 20 clamped.
	s2 := NewL1HashSignerV2(L1SignerConfig{TreeHeight: 25})
	if s2.height != 20 {
		t.Errorf("height=25 clamped to %d, want 20", s2.height)
	}
}

func TestL1HashSignerV2InvalidWinternitzDefaults(t *testing.T) {
	s := NewL1HashSignerV2(L1SignerConfig{TreeHeight: 3, WinternitzParam: 8})
	if s.winternitzW != l1v2WDefault {
		t.Errorf("invalid w=8 defaulted to %d, want %d", s.winternitzW, l1v2WDefault)
	}
}

func TestL1V2GenerateKeyPair(t *testing.T) {
	signer := NewL1HashSignerV2(L1SignerConfig{TreeHeight: 4, WinternitzParam: 16})
	kp, err := signer.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if len(kp.PublicRoot) != 32 {
		t.Errorf("PublicRoot len = %d, want 32", len(kp.PublicRoot))
	}
	if len(kp.PrivateSeeds) != l1v2SeedSize {
		t.Errorf("PrivateSeeds len = %d, want %d", len(kp.PrivateSeeds), l1v2SeedSize)
	}
	if kp.TreeHeight != 4 {
		t.Errorf("TreeHeight = %d, want 4", kp.TreeHeight)
	}
	if kp.MaxUses != 16 {
		t.Errorf("MaxUses = %d, want 16", kp.MaxUses)
	}
	if kp.UsedCount != 0 {
		t.Errorf("UsedCount = %d, want 0", kp.UsedCount)
	}
}

func TestL1V2GenerateKeyPairUniqueness(t *testing.T) {
	signer := NewL1HashSignerV2(L1SignerConfig{TreeHeight: 3, WinternitzParam: 16})
	kp1, _ := signer.GenerateKeyPair()
	kp2, _ := signer.GenerateKeyPair()

	same := true
	for i := range kp1.PublicRoot {
		if kp1.PublicRoot[i] != kp2.PublicRoot[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("two key pairs have same public root (extremely unlikely)")
	}
}

func TestL1V2SignAndVerify(t *testing.T) {
	signer := NewL1HashSignerV2(L1SignerConfig{TreeHeight: 4, WinternitzParam: 16})
	kp, _ := signer.GenerateKeyPair()

	msg := []byte("test message for L1 V2 signing")
	sig, err := signer.Sign(kp, msg)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if sig.LeafIndex != 0 {
		t.Errorf("LeafIndex = %d, want 0", sig.LeafIndex)
	}
	if len(sig.AuthPath) != 4 {
		t.Errorf("AuthPath len = %d, want 4", len(sig.AuthPath))
	}
	if len(sig.OTSSigBytes) != l1v2ChainLen16 {
		t.Errorf("OTSSigBytes chains = %d, want %d", len(sig.OTSSigBytes), l1v2ChainLen16)
	}
	if len(sig.MessageHash) != 32 {
		t.Errorf("MessageHash len = %d, want 32", len(sig.MessageHash))
	}

	valid, err := signer.Verify(sig, msg)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !valid {
		t.Error("Verify returned false for valid signature")
	}
}

func TestL1V2SignAndVerifyW4(t *testing.T) {
	signer := NewL1HashSignerV2(L1SignerConfig{TreeHeight: 3, WinternitzParam: 4})
	kp, _ := signer.GenerateKeyPair()

	msg := []byte("winternitz w=4 V2 test")
	sig, err := signer.Sign(kp, msg)
	if err != nil {
		t.Fatalf("Sign(w=4): %v", err)
	}
	if len(sig.OTSSigBytes) != l1v2ChainLen4 {
		t.Errorf("OTSSigBytes for w=4 = %d, want %d", len(sig.OTSSigBytes), l1v2ChainLen4)
	}

	valid, err := signer.Verify(sig, msg)
	if err != nil {
		t.Fatalf("Verify(w=4): %v", err)
	}
	if !valid {
		t.Error("Verify(w=4) returned false for valid signature")
	}
}

func TestL1V2SignMultiple(t *testing.T) {
	signer := NewL1HashSignerV2(L1SignerConfig{TreeHeight: 3, WinternitzParam: 16})
	kp, _ := signer.GenerateKeyPair()

	for i := 0; i < 8; i++ {
		msg := []byte{byte(i), 'h', 'e', 'l', 'l', 'o'}
		sig, err := signer.Sign(kp, msg)
		if err != nil {
			t.Fatalf("Sign(%d): %v", i, err)
		}
		if sig.LeafIndex != uint32(i) {
			t.Errorf("Sign(%d): LeafIndex = %d", i, sig.LeafIndex)
		}
		valid, err := signer.Verify(sig, msg)
		if err != nil || !valid {
			t.Fatalf("Verify(%d) failed: valid=%v err=%v", i, valid, err)
		}
	}

	// 9th should fail.
	_, err := signer.Sign(kp, []byte("overflow"))
	if err != ErrL1KeyExhausted {
		t.Errorf("Sign after exhaustion: got %v, want %v", err, ErrL1KeyExhausted)
	}
}

func TestL1V2VerifyWrongMessage(t *testing.T) {
	signer := NewL1HashSignerV2(L1SignerConfig{TreeHeight: 4, WinternitzParam: 16})
	kp, _ := signer.GenerateKeyPair()

	sig, _ := signer.Sign(kp, []byte("correct message"))
	valid, _ := signer.Verify(sig, []byte("wrong message"))
	if valid {
		t.Error("Verify should fail with wrong message")
	}
}

func TestL1V2VerifyTamperedSignature(t *testing.T) {
	signer := NewL1HashSignerV2(L1SignerConfig{TreeHeight: 4, WinternitzParam: 16})
	kp, _ := signer.GenerateKeyPair()

	msg := []byte("test message")
	sig, _ := signer.Sign(kp, msg)

	// Tamper with OTS signature.
	sig.OTSSigBytes[0][0] ^= 0xff
	valid, _ := signer.Verify(sig, msg)
	if valid {
		t.Error("Verify should reject tampered OTS signature")
	}
}

func TestL1V2VerifyWrongPublicRoot(t *testing.T) {
	signer := NewL1HashSignerV2(L1SignerConfig{TreeHeight: 4, WinternitzParam: 16})
	kp, _ := signer.GenerateKeyPair()

	msg := []byte("message")
	sig, _ := signer.Sign(kp, msg)

	sig.PublicRoot = make([]byte, 32)
	sig.PublicRoot[0] = 0xff
	valid, _ := signer.Verify(sig, msg)
	if valid {
		t.Error("Verify should fail with wrong public root")
	}
}

func TestL1V2VerifyNilInputs(t *testing.T) {
	signer := NewL1HashSignerV2(L1SignerConfig{TreeHeight: 4, WinternitzParam: 16})

	// Nil signature.
	valid, err := signer.Verify(nil, []byte("msg"))
	if valid || err != ErrL1InvalidSignature {
		t.Error("should reject nil signature")
	}

	// Empty message.
	valid, err = signer.Verify(&L1Signature{}, nil)
	if valid || err != ErrL1InvalidSignature {
		t.Error("should reject nil message")
	}

	// Empty pub root.
	valid, err = signer.Verify(&L1Signature{PublicRoot: nil}, []byte("msg"))
	if valid || err != ErrL1InvalidSignature {
		t.Error("should reject empty pub root")
	}
}

func TestL1V2VerifyBadAuthPathLength(t *testing.T) {
	signer := NewL1HashSignerV2(L1SignerConfig{TreeHeight: 4, WinternitzParam: 16})
	kp, _ := signer.GenerateKeyPair()

	msg := []byte("test")
	sig, _ := signer.Sign(kp, msg)

	// Truncate auth path.
	sig.AuthPath = sig.AuthPath[:2]
	valid, err := signer.Verify(sig, msg)
	if valid {
		t.Error("Verify should reject wrong auth path length")
	}
	if err != ErrL1InvalidAuthPath {
		t.Errorf("expected ErrL1InvalidAuthPath, got %v", err)
	}
}

func TestL1V2VerifyBadLeafIndex(t *testing.T) {
	signer := NewL1HashSignerV2(L1SignerConfig{TreeHeight: 4, WinternitzParam: 16})
	kp, _ := signer.GenerateKeyPair()

	msg := []byte("test")
	sig, _ := signer.Sign(kp, msg)

	sig.LeafIndex = 999 // way out of range
	valid, err := signer.Verify(sig, msg)
	if valid {
		t.Error("Verify should reject out-of-range leaf index")
	}
	if err != ErrL1InvalidAuthPath {
		t.Errorf("expected ErrL1InvalidAuthPath, got %v", err)
	}
}

func TestL1V2SignNilKeyPair(t *testing.T) {
	signer := NewL1HashSignerV2(DefaultL1SignerConfig())

	_, err := signer.Sign(nil, []byte("message"))
	if err != ErrL1KeyExhausted {
		t.Errorf("Sign with nil key: got %v, want %v", err, ErrL1KeyExhausted)
	}
}

func TestL1V2SignEmptyMessage(t *testing.T) {
	signer := NewL1HashSignerV2(L1SignerConfig{TreeHeight: 4, WinternitzParam: 16})
	kp, _ := signer.GenerateKeyPair()

	_, err := signer.Sign(kp, []byte{})
	if err != ErrL1InvalidSignature {
		t.Errorf("Sign empty: got %v, want %v", err, ErrL1InvalidSignature)
	}
}

func TestL1V2RemainingSignatures(t *testing.T) {
	signer := NewL1HashSignerV2(L1SignerConfig{TreeHeight: 3, WinternitzParam: 16})
	kp, _ := signer.GenerateKeyPair()

	if signer.RemainingSignatures(kp) != 8 {
		t.Errorf("RemainingSignatures = %d, want 8", signer.RemainingSignatures(kp))
	}

	signer.Sign(kp, []byte("msg1"))
	signer.Sign(kp, []byte("msg2"))
	if signer.RemainingSignatures(kp) != 6 {
		t.Errorf("RemainingSignatures after 2 sigs = %d, want 6", signer.RemainingSignatures(kp))
	}
}

func TestL1V2RemainingSignaturesNilKeyPair(t *testing.T) {
	signer := NewL1HashSignerV2(DefaultL1SignerConfig())
	if signer.RemainingSignatures(nil) != 0 {
		t.Errorf("RemainingSignatures(nil) = %d, want 0", signer.RemainingSignatures(nil))
	}
}

func TestL1V2IsKeyExhausted(t *testing.T) {
	signer := NewL1HashSignerV2(L1SignerConfig{TreeHeight: 1, WinternitzParam: 16})

	// Nil key.
	if !signer.IsKeyExhausted(nil) {
		t.Error("IsKeyExhausted(nil) should be true")
	}

	kp, _ := signer.GenerateKeyPair()
	if signer.IsKeyExhausted(kp) {
		t.Error("IsKeyExhausted after keygen should be false")
	}

	signer.Sign(kp, []byte("msg1"))
	if signer.IsKeyExhausted(kp) {
		t.Error("IsKeyExhausted after 1 sig should be false")
	}

	signer.Sign(kp, []byte("msg2"))
	if !signer.IsKeyExhausted(kp) {
		t.Error("IsKeyExhausted after 2 sigs should be true")
	}
}

func TestL1V2CrossVerification(t *testing.T) {
	signer := NewL1HashSignerV2(L1SignerConfig{TreeHeight: 3, WinternitzParam: 16})
	kp, _ := signer.GenerateKeyPair()

	msg1 := []byte("first")
	msg2 := []byte("second")

	sig1, _ := signer.Sign(kp, msg1)
	sig2, _ := signer.Sign(kp, msg2)

	// Each verifies with its own message.
	v1, _ := signer.Verify(sig1, msg1)
	if !v1 {
		t.Error("sig1 should verify with msg1")
	}
	v2, _ := signer.Verify(sig2, msg2)
	if !v2 {
		t.Error("sig2 should verify with msg2")
	}

	// Cross-verify should fail.
	v3, _ := signer.Verify(sig1, msg2)
	if v3 {
		t.Error("sig1 should not verify with msg2")
	}
	v4, _ := signer.Verify(sig2, msg1)
	if v4 {
		t.Error("sig2 should not verify with msg1")
	}
}

func TestL1V2ConcurrentSign(t *testing.T) {
	signer := NewL1HashSignerV2(L1SignerConfig{TreeHeight: 6, WinternitzParam: 16})
	kp, _ := signer.GenerateKeyPair()

	type sigMsg struct {
		sig *L1Signature
		msg []byte
	}

	var wg sync.WaitGroup
	results := make(chan sigMsg, 64)

	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			msg := []byte{byte(idx), 'c', 'o', 'n', 'c'}
			sig, err := signer.Sign(kp, msg)
			if err != nil {
				return
			}
			results <- sigMsg{sig: sig, msg: msg}
		}(i)
	}
	wg.Wait()
	close(results)

	// All 64 signatures should have unique leaf indices.
	seen := make(map[uint32]bool)
	for sm := range results {
		if seen[sm.sig.LeafIndex] {
			t.Errorf("duplicate leaf index: %d", sm.sig.LeafIndex)
		}
		seen[sm.sig.LeafIndex] = true

		valid, err := signer.Verify(sm.sig, sm.msg)
		if err != nil || !valid {
			t.Errorf("concurrent sig at leaf %d failed verification", sm.sig.LeafIndex)
		}
	}

	if !signer.IsKeyExhausted(kp) {
		t.Error("key should be exhausted after 64 signatures")
	}
}

func TestL1V2VerifyBadOTSSigLength(t *testing.T) {
	signer := NewL1HashSignerV2(L1SignerConfig{TreeHeight: 4, WinternitzParam: 16})
	kp, _ := signer.GenerateKeyPair()

	msg := []byte("test")
	sig, _ := signer.Sign(kp, msg)

	// Truncate OTS signature.
	sig.OTSSigBytes = sig.OTSSigBytes[:5]
	valid, err := signer.Verify(sig, msg)
	if valid {
		t.Error("Verify should reject wrong OTS sig length")
	}
	if err != ErrL1InvalidSignature {
		t.Errorf("expected ErrL1InvalidSignature, got %v", err)
	}
}
