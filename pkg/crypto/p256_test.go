package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"testing"
)

func TestP256VerifyValid(t *testing.T) {
	// Generate a fresh P-256 key pair and sign a message.
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	msg := sha256.Sum256([]byte("hello p256"))
	r, s, err := ecdsa.Sign(rand.Reader, priv, msg[:])
	if err != nil {
		t.Fatal(err)
	}
	if !P256Verify(msg[:], r, s, priv.PublicKey.X, priv.PublicKey.Y) {
		t.Fatal("valid signature rejected")
	}
}

func TestP256VerifyInvalid(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	msg := sha256.Sum256([]byte("hello p256"))
	r, s, err := ecdsa.Sign(rand.Reader, priv, msg[:])
	if err != nil {
		t.Fatal(err)
	}

	// Flip a bit in the hash.
	badHash := make([]byte, 32)
	copy(badHash, msg[:])
	badHash[0] ^= 0xff
	if P256Verify(badHash, r, s, priv.PublicKey.X, priv.PublicKey.Y) {
		t.Fatal("invalid hash accepted")
	}

	// Bad r value.
	badR := new(big.Int).Add(r, big.NewInt(1))
	if P256Verify(msg[:], badR, s, priv.PublicKey.X, priv.PublicKey.Y) {
		t.Fatal("invalid r accepted")
	}

	// Bad s value.
	badS := new(big.Int).Add(s, big.NewInt(1))
	if P256Verify(msg[:], r, badS, priv.PublicKey.X, priv.PublicKey.Y) {
		t.Fatal("invalid s accepted")
	}
}

func TestP256VerifyBadPoint(t *testing.T) {
	msg := sha256.Sum256([]byte("test"))
	r := big.NewInt(1)
	s := big.NewInt(1)

	// Nil coordinates.
	if P256Verify(msg[:], r, s, nil, nil) {
		t.Fatal("nil point accepted")
	}

	// Point not on curve.
	if P256Verify(msg[:], r, s, big.NewInt(1), big.NewInt(1)) {
		t.Fatal("off-curve point accepted")
	}
}

func TestP256VerifyTestVector(t *testing.T) {
	// go-ethereum p256Verify.json test vector #1:
	// Input = hash(32) + r(32) + s(32) + x(32) + y(32) = 160 bytes.
	inputHex := "4cee90eb86eaa050036147a12d49004b6b9c72bd725d39d4785011fe190f0b4d" +
		"a73bd4903f0ce3b639bbbf6e8e80d16931ff4bcf5993d58468e8fb19086e8cac" +
		"36dbcd03009df8c59286b162af3bd7fcc0450c9aa81be5d10d312af6c66b1d60" +
		"4aebd3099c618202fcfe16ae7770b0c49ab5eadf74b754204a3bb6060e44eff3" +
		"7618b065f9832de4ca6ca971a7a1adc826d0f7c00181a5fb2ddf79ae00b4e10e"

	input, err := hex.DecodeString(inputHex)
	if err != nil {
		t.Fatal(err)
	}
	if len(input) != 160 {
		t.Fatalf("test vector length = %d, want 160", len(input))
	}

	hash := input[0:32]
	r := new(big.Int).SetBytes(input[32:64])
	s := new(big.Int).SetBytes(input[64:96])
	x := new(big.Int).SetBytes(input[96:128])
	y := new(big.Int).SetBytes(input[128:160])

	if !P256Verify(hash, r, s, x, y) {
		t.Fatal("test vector signature rejected")
	}
}

func TestP256VerifyZeroRS(t *testing.T) {
	// r=0, s=0 should fail (from go-ethereum test vectors).
	msg := sha256.Sum256([]byte("test"))
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	if P256Verify(msg[:], big.NewInt(0), big.NewInt(0), priv.PublicKey.X, priv.PublicKey.Y) {
		t.Fatal("zero r,s should be rejected")
	}
	if P256Verify(msg[:], big.NewInt(0), big.NewInt(1), priv.PublicKey.X, priv.PublicKey.Y) {
		t.Fatal("zero r should be rejected")
	}
	if P256Verify(msg[:], big.NewInt(1), big.NewInt(0), priv.PublicKey.X, priv.PublicKey.Y) {
		t.Fatal("zero s should be rejected")
	}
}
