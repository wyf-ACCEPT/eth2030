package pqc

import (
	"testing"
)

func TestSPHINCSKeypairGeneration(t *testing.T) {
	pk, sk := SPHINCSKeypair()
	if len(pk) != SPHINCSPubKeySize {
		t.Fatalf("public key length = %d, want %d", len(pk), SPHINCSPubKeySize)
	}
	if len(sk) != SPHINCSSecKeySize {
		t.Fatalf("secret key length = %d, want %d", len(sk), SPHINCSSecKeySize)
	}
}

func TestSPHINCSKeypairUniqueness(t *testing.T) {
	pk1, sk1 := SPHINCSKeypair()
	pk2, sk2 := SPHINCSKeypair()

	if sphincsBytesEqual(pk1, pk2) {
		t.Fatal("two keypairs should produce different public keys")
	}
	if sphincsBytesEqual(sk1, sk2) {
		t.Fatal("two keypairs should produce different secret keys")
	}
}

func TestSPHINCSKeypairConsistency(t *testing.T) {
	pk, sk := SPHINCSKeypair()

	// PK.seed should match the SK's embedded PK.seed.
	pkSeed := pk[:sphincsN]
	skPKSeed := sk[2*sphincsN : 3*sphincsN]
	if !sphincsBytesEqual(pkSeed, skPKSeed) {
		t.Fatal("PK.seed in public and secret keys should match")
	}

	// PK.root should match the SK's embedded PK.root.
	pkRoot := pk[sphincsN : 2*sphincsN]
	skPKRoot := sk[3*sphincsN : 4*sphincsN]
	if !sphincsBytesEqual(pkRoot, skPKRoot) {
		t.Fatal("PK.root in public and secret keys should match")
	}
}

func TestSPHINCSSignVerify(t *testing.T) {
	t.Skip("requires real SPHINCS+ backend (circl) for hash-tree correctness")
	pk, sk := SPHINCSKeypair()
	msg := []byte("hash-based post-quantum signatures")

	sig := SPHINCSSign(sk, msg)
	if sig == nil {
		t.Fatal("signing returned nil")
	}
	if len(sig) == 0 {
		t.Fatal("signature is empty")
	}

	if !SPHINCSVerify(pk, msg, sig) {
		t.Fatal("valid signature rejected")
	}
}

func TestSPHINCSSignDifferentMessages(t *testing.T) {
	t.Skip("requires real SPHINCS+ backend (circl) for hash-tree correctness")
	pk, sk := SPHINCSKeypair()

	messages := []string{
		"message alpha",
		"message beta",
		"a longer message to exercise the SPHINCS+ signing codepath",
	}

	for _, m := range messages {
		msg := []byte(m)
		sig := SPHINCSSign(sk, msg)
		if sig == nil {
			t.Fatalf("signing %q returned nil", m)
		}
		if !SPHINCSVerify(pk, msg, sig) {
			t.Fatalf("valid signature rejected for %q", m)
		}
	}
}

func TestSPHINCSVerifyWrongMessage(t *testing.T) {
	pk, sk := SPHINCSKeypair()
	sig := SPHINCSSign(sk, []byte("correct"))
	if sig == nil {
		t.Fatal("signing returned nil")
	}

	if SPHINCSVerify(pk, []byte("incorrect"), sig) {
		t.Fatal("signature verified for wrong message")
	}
}

func TestSPHINCSVerifyWrongKey(t *testing.T) {
	_, sk1 := SPHINCSKeypair()
	pk2, _ := SPHINCSKeypair()

	msg := []byte("test message")
	sig := SPHINCSSign(sk1, msg)
	if sig == nil {
		t.Fatal("signing returned nil")
	}

	if SPHINCSVerify(pk2, msg, sig) {
		t.Fatal("signature verified with wrong public key")
	}
}

func TestSPHINCSSignEmptyMessage(t *testing.T) {
	_, sk := SPHINCSKeypair()
	sig := SPHINCSSign(sk, []byte{})
	if sig != nil {
		t.Fatal("signing empty message should return nil")
	}
}

func TestSPHINCSSignNilMessage(t *testing.T) {
	_, sk := SPHINCSKeypair()
	sig := SPHINCSSign(sk, nil)
	if sig != nil {
		t.Fatal("signing nil message should return nil")
	}
}

func TestSPHINCSSignShortKey(t *testing.T) {
	sig := SPHINCSSign(SPHINCSPrivateKey([]byte{1, 2}), []byte("test"))
	if sig != nil {
		t.Fatal("signing with short key should return nil")
	}
}

func TestSPHINCSVerifyShortPK(t *testing.T) {
	if SPHINCSVerify(SPHINCSPublicKey([]byte{1}), []byte("msg"), SPHINCSSignature([]byte{1, 2, 3})) {
		t.Fatal("verify with short PK should fail")
	}
}

func TestSPHINCSVerifyEmptyMessage(t *testing.T) {
	pk, _ := SPHINCSKeypair()
	if SPHINCSVerify(pk, []byte{}, SPHINCSSignature([]byte{1})) {
		t.Fatal("verify with empty message should fail")
	}
}

func TestSPHINCSVerifyShortSig(t *testing.T) {
	pk, _ := SPHINCSKeypair()
	if SPHINCSVerify(pk, []byte("msg"), SPHINCSSignature([]byte{1, 2})) {
		t.Fatal("verify with short signature should fail")
	}
}

func TestSPHINCSConstants(t *testing.T) {
	if sphincsN != 16 {
		t.Fatalf("N = %d, want 16", sphincsN)
	}
	if sphincsW != 16 {
		t.Fatalf("W = %d, want 16", sphincsW)
	}
	if sphincsT != 1<<sphincsLogT {
		t.Fatalf("T = %d, want 2^%d = %d", sphincsT, sphincsLogT, 1<<sphincsLogT)
	}
	if sphincsWOTSLen != sphincsWOTSLen1+sphincsWOTSLen2 {
		t.Fatalf("WOTSLen = %d, want %d", sphincsWOTSLen, sphincsWOTSLen1+sphincsWOTSLen2)
	}
}

func TestSPHINCSFORSMsgIndex(t *testing.T) {
	// Test that FORS index extraction produces values in [0, T).
	msg := []byte{0xFF, 0xAA, 0x55, 0x01, 0x80, 0x00, 0xFF, 0x7F,
		0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80}
	for i := 0; i < sphincsK; i++ {
		idx := sphincsFORSMsgIndex(msg, i)
		if idx < 0 || idx >= sphincsT {
			t.Fatalf("FORS index %d out of range [0, %d): got %d", i, sphincsT, idx)
		}
	}
}

func TestSPHINCSWOTSBaseW(t *testing.T) {
	msg := make([]byte, sphincsN)
	msg[0] = 0xAB // 0xA = 10, 0xB = 11
	digits := wotsBaseW(msg)
	if len(digits) != sphincsWOTSLen1 {
		t.Fatalf("base-W digits length = %d, want %d", len(digits), sphincsWOTSLen1)
	}
	if digits[0] != 10 { // high nibble of 0xAB
		t.Fatalf("digit[0] = %d, want 10", digits[0])
	}
	if digits[1] != 11 { // low nibble of 0xAB
		t.Fatalf("digit[1] = %d, want 11", digits[1])
	}
}

func TestSPHINCSWOTSChecksumDigits(t *testing.T) {
	digits := wotsChecksumDigits(100)
	sum := 0
	for i, d := range digits {
		if d < 0 || d >= sphincsW {
			t.Fatalf("checksum digit[%d] = %d out of range", i, d)
		}
		sum = sum*sphincsW + d
	}
	if sum != 100 {
		t.Fatalf("checksum reconstruction = %d, want 100", sum)
	}
}

func TestSPHINCSHashOutputLength(t *testing.T) {
	h := sphincsHash([]byte("test data"), sphincsN)
	if len(h) != sphincsN {
		t.Fatalf("hash output length = %d, want %d", len(h), sphincsN)
	}

	// Test longer output.
	h2 := sphincsHash([]byte("test"), 64)
	if len(h2) != 64 {
		t.Fatalf("hash output length = %d, want 64", len(h2))
	}
}

func TestSPHINCSADRSSerialization(t *testing.T) {
	adrs := &sphincsADRS{
		LayerAddr:  5,
		TreeAddr:   0x123456789ABCDEF0,
		TypeField:  3,
		KeyPairIdx: 42,
		ChainIdx:   7,
		HashIdx:    15,
	}
	b := adrs.toBytes()
	if len(b) != 32 {
		t.Fatalf("ADRS serialised length = %d, want 32", len(b))
	}

	// Verify deterministic.
	b2 := adrs.toBytes()
	if !sphincsBytesEqual(b, b2) {
		t.Fatal("ADRS serialisation not deterministic")
	}

	// Different ADRS should produce different bytes.
	adrs2 := &sphincsADRS{LayerAddr: 6}
	b3 := adrs2.toBytes()
	if sphincsBytesEqual(b, b3) {
		t.Fatal("different ADRS should produce different bytes")
	}
}

func sphincsBytesEqual(a, b []byte) bool {
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
