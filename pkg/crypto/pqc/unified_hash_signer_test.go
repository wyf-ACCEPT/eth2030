package pqc

import (
	"testing"
)

func TestXMSSGenerateKeyPairH10(t *testing.T) {
	pk, sk, err := GenerateXMSSKeyPair(XMSSHeight10)
	if err != nil {
		t.Fatalf("GenerateXMSSKeyPair: %v", err)
	}
	if pk == nil || sk == nil {
		t.Fatal("expected non-nil key pair")
	}
	if pk.Height != XMSSHeight10 {
		t.Errorf("pk height: got %d, want %d", pk.Height, XMSSHeight10)
	}
	if sk.MaxLeaves != 1024 {
		t.Errorf("max leaves: got %d, want 1024", sk.MaxLeaves)
	}
	if sk.UsedLeaves != 0 {
		t.Errorf("used leaves: got %d, want 0", sk.UsedLeaves)
	}
}

func TestXMSSGenerateKeyPairInvalidHeight(t *testing.T) {
	_, _, err := GenerateXMSSKeyPair(5) // not 10, 16, or 20
	if err != ErrXMSSInvalidHeight {
		t.Errorf("expected ErrXMSSInvalidHeight, got %v", err)
	}
	_, _, err = GenerateXMSSKeyPair(0)
	if err != ErrXMSSInvalidHeight {
		t.Errorf("expected ErrXMSSInvalidHeight for 0, got %v", err)
	}
}

func TestXMSSSignAndVerify(t *testing.T) {
	pk, sk, err := GenerateXMSSKeyPair(XMSSHeight10)
	if err != nil {
		t.Fatalf("GenerateXMSSKeyPair: %v", err)
	}

	msg := []byte("hello, XMSS world!")
	sig, err := XMSSSign(sk, msg)
	if err != nil {
		t.Fatalf("XMSSSign: %v", err)
	}
	if sig == nil {
		t.Fatal("expected non-nil signature")
	}
	if sig.LeafIndex != 0 {
		t.Errorf("leaf index: got %d, want 0", sig.LeafIndex)
	}
	if len(sig.AuthPath) != XMSSHeight10 {
		t.Errorf("auth path len: got %d, want %d", len(sig.AuthPath), XMSSHeight10)
	}
	if len(sig.OTSSignature) != xmssChainLen {
		t.Errorf("OTS sig len: got %d, want %d", len(sig.OTSSignature), xmssChainLen)
	}

	if !XMSSVerify(pk, msg, sig) {
		t.Error("valid signature should verify")
	}
}

func TestXMSSSignMultiple(t *testing.T) {
	pk, sk, _ := GenerateXMSSKeyPair(XMSSHeight10)

	for i := 0; i < 3; i++ {
		msg := []byte{byte(i), 0x01, 0x02}
		sig, err := XMSSSign(sk, msg)
		if err != nil {
			t.Fatalf("XMSSSign[%d]: %v", i, err)
		}
		if sig.LeafIndex != uint32(i) {
			t.Errorf("leaf index[%d]: got %d, want %d", i, sig.LeafIndex, i)
		}
		if !XMSSVerify(pk, msg, sig) {
			t.Errorf("signature[%d] should verify", i)
		}
	}
	if sk.UsedLeaves != 3 {
		t.Errorf("used leaves: got %d, want 3", sk.UsedLeaves)
	}
}

func TestXMSSSignVerifyWrongMessage(t *testing.T) {
	pk, sk, _ := GenerateXMSSKeyPair(XMSSHeight10)

	sig, _ := XMSSSign(sk, []byte("correct message"))
	if XMSSVerify(pk, []byte("wrong message"), sig) {
		t.Error("wrong message should not verify")
	}
}

func TestXMSSSignNilSK(t *testing.T) {
	_, err := XMSSSign(nil, []byte("msg"))
	if err != ErrXMSSNotInitialized {
		t.Errorf("expected ErrXMSSNotInitialized, got %v", err)
	}
}

func TestXMSSSignEmptyMsg(t *testing.T) {
	_, sk, _ := GenerateXMSSKeyPair(XMSSHeight10)
	_, err := XMSSSign(sk, nil)
	if err != ErrXMSSEmptyMessage {
		t.Errorf("expected ErrXMSSEmptyMessage, got %v", err)
	}
	_, err = XMSSSign(sk, []byte{})
	if err != ErrXMSSEmptyMessage {
		t.Errorf("expected ErrXMSSEmptyMessage for empty, got %v", err)
	}
}

func TestXMSSVerifyNilInputs(t *testing.T) {
	pk, sk, _ := GenerateXMSSKeyPair(XMSSHeight10)
	sig, _ := XMSSSign(sk, []byte("msg"))

	if XMSSVerify(nil, []byte("msg"), sig) {
		t.Error("nil pk should not verify")
	}
	if XMSSVerify(pk, nil, sig) {
		t.Error("nil msg should not verify")
	}
	if XMSSVerify(pk, []byte("msg"), nil) {
		t.Error("nil sig should not verify")
	}
}

func TestXMSSVerifyBadAuthPathLen(t *testing.T) {
	pk, sk, _ := GenerateXMSSKeyPair(XMSSHeight10)
	sig, _ := XMSSSign(sk, []byte("msg"))
	sig.AuthPath = sig.AuthPath[:5] // truncate
	if XMSSVerify(pk, []byte("msg"), sig) {
		t.Error("truncated auth path should not verify")
	}
}

func TestXMSSVerifyBadOTSLen(t *testing.T) {
	pk, sk, _ := GenerateXMSSKeyPair(XMSSHeight10)
	sig, _ := XMSSSign(sk, []byte("msg"))
	sig.OTSSignature = sig.OTSSignature[:10] // truncate
	if XMSSVerify(pk, []byte("msg"), sig) {
		t.Error("truncated OTS sig should not verify")
	}
}

func TestXMSSKeyManagerCreate(t *testing.T) {
	mgr, err := NewXMSSKeyManager(XMSSHeight10)
	if err != nil {
		t.Fatalf("NewXMSSKeyManager: %v", err)
	}
	if mgr.TreeCount() != 1 {
		t.Errorf("tree count: got %d, want 1", mgr.TreeCount())
	}
	remaining := mgr.RemainingSignatures()
	if remaining != 1024 {
		t.Errorf("remaining: got %d, want 1024", remaining)
	}
}

func TestXMSSKeyManagerSign(t *testing.T) {
	mgr, _ := NewXMSSKeyManager(XMSSHeight10)
	pk := mgr.ActivePublicKey()
	if pk == nil {
		t.Fatal("expected non-nil active public key")
	}

	sig, err := mgr.Sign([]byte("test message"))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if !XMSSVerify(pk, []byte("test message"), sig) {
		t.Error("manager-signed message should verify")
	}
}

func TestXMSSKeyManagerNeedsRotation(t *testing.T) {
	mgr, _ := NewXMSSKeyManager(XMSSHeight10)
	if mgr.NeedsRotation() {
		t.Error("fresh manager should not need rotation")
	}
}

func TestXMSSKeyManagerPublicKeys(t *testing.T) {
	mgr, _ := NewXMSSKeyManager(XMSSHeight10)
	keys := mgr.XMSSPublicKeys()
	if len(keys) != 1 {
		t.Errorf("public keys count: got %d, want 1", len(keys))
	}
}

func TestXMSSKeyManagerInvalidHeight(t *testing.T) {
	_, err := NewXMSSKeyManager(7)
	if err != ErrXMSSInvalidHeight {
		t.Errorf("expected ErrXMSSInvalidHeight, got %v", err)
	}
}

func TestXMSSMaxSignaturesForHeight(t *testing.T) {
	tests := []struct {
		height int
		want   uint64
	}{
		{10, 1024},
		{16, 65536},
		{20, 1048576},
		{0, 0},
		{25, 0},
	}
	for _, tt := range tests {
		got := MaxSignaturesForHeight(tt.height)
		if got != tt.want {
			t.Errorf("MaxSignaturesForHeight(%d): got %d, want %d", tt.height, got, tt.want)
		}
	}
}

func TestXMSSDomainSeparation(t *testing.T) {
	// Same message signed with different seed should produce different signatures.
	pk1, sk1, _ := GenerateXMSSKeyPair(XMSSHeight10)
	pk2, sk2, _ := GenerateXMSSKeyPair(XMSSHeight10)

	msg := []byte("same message")
	sig1, _ := XMSSSign(sk1, msg)
	sig2, _ := XMSSSign(sk2, msg)

	// Both should verify with their own keys.
	if !XMSSVerify(pk1, msg, sig1) {
		t.Error("sig1 should verify with pk1")
	}
	if !XMSSVerify(pk2, msg, sig2) {
		t.Error("sig2 should verify with pk2")
	}

	// Cross-verify should fail.
	if XMSSVerify(pk1, msg, sig2) {
		t.Error("sig2 should not verify with pk1")
	}
	if XMSSVerify(pk2, msg, sig1) {
		t.Error("sig1 should not verify with pk2")
	}
}

func TestXMSSMessageDigitsChecksum(t *testing.T) {
	msg := make([]byte, 32)
	msg[0] = 0xab
	msg[31] = 0xcd

	digits := xmssMessageDigits(msg)
	if len(digits) != xmssChainLen {
		t.Errorf("digits len: got %d, want %d", len(digits), xmssChainLen)
	}

	// Verify all digits are in range [0, w-1].
	for i, d := range digits {
		if d < 0 || d >= xmssWOTSW {
			t.Errorf("digit[%d] = %d, out of range [0, %d)", i, d, xmssWOTSW)
		}
	}
}
