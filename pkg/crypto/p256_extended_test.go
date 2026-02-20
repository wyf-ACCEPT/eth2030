package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"math/big"
	"testing"
)

func TestP256GenerateKey(t *testing.T) {
	prv, err := P256GenerateKey()
	if err != nil {
		t.Fatalf("P256GenerateKey: %v", err)
	}
	if prv.Curve != elliptic.P256() {
		t.Error("key should be on P-256 curve")
	}
	if !elliptic.P256().IsOnCurve(prv.PublicKey.X, prv.PublicKey.Y) {
		t.Error("public key not on curve")
	}
}

func TestP256SignAndVerifyCompact(t *testing.T) {
	prv, _ := P256GenerateKey()
	hash := sha256.Sum256([]byte("test p256 sign"))
	sig, err := P256Sign(hash[:], prv)
	if err != nil {
		t.Fatalf("P256Sign: %v", err)
	}
	if len(sig) != 64 {
		t.Fatalf("signature length = %d, want 64", len(sig))
	}
	if !P256VerifyCompact(hash[:], sig, &prv.PublicKey) {
		t.Error("valid signature rejected")
	}
}

func TestP256SignLowS(t *testing.T) {
	prv, _ := P256GenerateKey()
	hash := sha256.Sum256([]byte("low-s test"))

	for i := 0; i < 20; i++ {
		sig, _ := P256Sign(hash[:], prv)
		s := new(big.Int).SetBytes(sig[32:64])
		if s.Cmp(p256HalfN) > 0 {
			t.Fatal("P256Sign should normalize S to lower half")
		}
	}
}

func TestP256SignInvalidHash(t *testing.T) {
	prv, _ := P256GenerateKey()
	_, err := P256Sign([]byte("short"), prv)
	if err == nil {
		t.Error("should reject short hash")
	}
}

func TestP256SignNilKey(t *testing.T) {
	hash := sha256.Sum256([]byte("test"))
	_, err := P256Sign(hash[:], nil)
	if err == nil {
		t.Error("should reject nil key")
	}
}

func TestP256SignWrongCurve(t *testing.T) {
	// Generate a key on a different curve.
	prv, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	hash := sha256.Sum256([]byte("test"))
	_, err := P256Sign(hash[:], prv)
	if err == nil {
		t.Error("should reject non-P256 key")
	}
}

func TestP256VerifyCompactInvalid(t *testing.T) {
	prv, _ := P256GenerateKey()
	hash := sha256.Sum256([]byte("test"))
	sig, _ := P256Sign(hash[:], prv)

	// Wrong hash.
	badHash := sha256.Sum256([]byte("other"))
	if P256VerifyCompact(badHash[:], sig, &prv.PublicKey) {
		t.Error("should reject wrong hash")
	}

	// Wrong key.
	prv2, _ := P256GenerateKey()
	if P256VerifyCompact(hash[:], sig, &prv2.PublicKey) {
		t.Error("should reject wrong key")
	}

	// Short sig.
	if P256VerifyCompact(hash[:], sig[:63], &prv.PublicKey) {
		t.Error("should reject short signature")
	}

	// Nil pubkey.
	if P256VerifyCompact(hash[:], sig, nil) {
		t.Error("should reject nil pubkey")
	}
}

func TestP256DERSignAndVerify(t *testing.T) {
	prv, _ := P256GenerateKey()
	hash := sha256.Sum256([]byte("der test"))

	der, err := P256SignDER(hash[:], prv)
	if err != nil {
		t.Fatalf("P256SignDER: %v", err)
	}
	if len(der) == 0 {
		t.Fatal("DER signature is empty")
	}

	if !P256VerifyDER(hash[:], der, &prv.PublicKey) {
		t.Error("valid DER signature rejected")
	}
}

func TestP256VerifyDERInvalid(t *testing.T) {
	prv, _ := P256GenerateKey()
	hash := sha256.Sum256([]byte("test"))

	if P256VerifyDER(hash[:], []byte{0x30, 0x00}, &prv.PublicKey) {
		t.Error("should reject invalid DER")
	}
	if P256VerifyDER(hash[:], nil, &prv.PublicKey) {
		t.Error("should reject nil DER")
	}
	if P256VerifyDER([]byte("short"), []byte{0x30}, &prv.PublicKey) {
		t.Error("should reject short hash")
	}
}

func TestP256MarshalUnmarshalDER(t *testing.T) {
	r := big.NewInt(12345)
	s := big.NewInt(67890)

	der, err := P256MarshalDER(r, s)
	if err != nil {
		t.Fatalf("P256MarshalDER: %v", err)
	}

	r2, s2, err := P256UnmarshalDER(der)
	if err != nil {
		t.Fatalf("P256UnmarshalDER: %v", err)
	}
	if r.Cmp(r2) != 0 || s.Cmp(s2) != 0 {
		t.Error("round-trip DER marshal/unmarshal failed")
	}
}

func TestP256MarshalDERInvalid(t *testing.T) {
	_, err := P256MarshalDER(nil, big.NewInt(1))
	if err == nil {
		t.Error("should reject nil r")
	}
	_, err = P256MarshalDER(big.NewInt(0), big.NewInt(1))
	if err == nil {
		t.Error("should reject zero r")
	}
	_, err = P256MarshalDER(big.NewInt(-1), big.NewInt(1))
	if err == nil {
		t.Error("should reject negative r")
	}
}

func TestP256UnmarshalDERInvalid(t *testing.T) {
	_, _, err := P256UnmarshalDER([]byte{0x01})
	if err == nil {
		t.Error("should reject invalid DER")
	}
	_, _, err = P256UnmarshalDER(nil)
	if err == nil {
		t.Error("should reject nil DER")
	}
}

func TestP256CompressDecompressPubkey(t *testing.T) {
	prv, _ := P256GenerateKey()
	compressed, err := P256CompressPubkey(&prv.PublicKey)
	if err != nil {
		t.Fatalf("P256CompressPubkey: %v", err)
	}
	if len(compressed) != 33 {
		t.Fatalf("compressed key length = %d, want 33", len(compressed))
	}
	if compressed[0] != 0x02 && compressed[0] != 0x03 {
		t.Errorf("invalid prefix: 0x%02x", compressed[0])
	}

	recovered, err := P256DecompressPubkey(compressed)
	if err != nil {
		t.Fatalf("P256DecompressPubkey: %v", err)
	}
	if recovered.X.Cmp(prv.PublicKey.X) != 0 || recovered.Y.Cmp(prv.PublicKey.Y) != 0 {
		t.Error("decompressed key does not match original")
	}
}

func TestP256CompressDecompressMultipleKeys(t *testing.T) {
	for i := 0; i < 10; i++ {
		prv, _ := P256GenerateKey()
		compressed, _ := P256CompressPubkey(&prv.PublicKey)
		recovered, err := P256DecompressPubkey(compressed)
		if err != nil {
			t.Fatalf("iteration %d: decompress: %v", i, err)
		}
		if recovered.X.Cmp(prv.PublicKey.X) != 0 || recovered.Y.Cmp(prv.PublicKey.Y) != 0 {
			t.Fatalf("iteration %d: key mismatch", i)
		}
	}
}

func TestP256CompressPubkeyNil(t *testing.T) {
	_, err := P256CompressPubkey(nil)
	if err == nil {
		t.Error("should reject nil key")
	}
}

func TestP256DecompressPubkeyInvalid(t *testing.T) {
	_, err := P256DecompressPubkey([]byte{0x02})
	if err == nil {
		t.Error("should reject short compressed key")
	}
	bad := make([]byte, 33)
	bad[0] = 0x05 // invalid prefix
	_, err = P256DecompressPubkey(bad)
	if err == nil {
		t.Error("should reject invalid prefix")
	}
}

func TestP256MarshalUncompressed(t *testing.T) {
	prv, _ := P256GenerateKey()
	uncompressed, err := P256MarshalUncompressed(&prv.PublicKey)
	if err != nil {
		t.Fatalf("P256MarshalUncompressed: %v", err)
	}
	if len(uncompressed) != 65 {
		t.Fatalf("uncompressed length = %d, want 65", len(uncompressed))
	}
	if uncompressed[0] != 0x04 {
		t.Errorf("prefix = 0x%02x, want 0x04", uncompressed[0])
	}
}

func TestP256UnmarshalPubkeyUncompressed(t *testing.T) {
	prv, _ := P256GenerateKey()
	data, _ := P256MarshalUncompressed(&prv.PublicKey)
	pub, err := P256UnmarshalPubkey(data)
	if err != nil {
		t.Fatalf("P256UnmarshalPubkey: %v", err)
	}
	if pub.X.Cmp(prv.PublicKey.X) != 0 || pub.Y.Cmp(prv.PublicKey.Y) != 0 {
		t.Error("unmarshaled key does not match")
	}
}

func TestP256UnmarshalPubkeyCompressed(t *testing.T) {
	prv, _ := P256GenerateKey()
	compressed, _ := P256CompressPubkey(&prv.PublicKey)
	pub, err := P256UnmarshalPubkey(compressed)
	if err != nil {
		t.Fatalf("P256UnmarshalPubkey compressed: %v", err)
	}
	if pub.X.Cmp(prv.PublicKey.X) != 0 || pub.Y.Cmp(prv.PublicKey.Y) != 0 {
		t.Error("unmarshaled compressed key does not match")
	}
}

func TestP256UnmarshalPubkeyInvalidLength(t *testing.T) {
	_, err := P256UnmarshalPubkey([]byte{0x04, 0x01, 0x02})
	if err == nil {
		t.Error("should reject invalid length")
	}
}

func TestP256RecoverPubkey(t *testing.T) {
	prv, _ := P256GenerateKey()
	hash := sha256.Sum256([]byte("recover test"))
	sig, _ := P256Sign(hash[:], prv)

	// Try both recovery IDs.
	for recID := byte(0); recID <= 1; recID++ {
		pub, err := P256RecoverPubkey(hash[:], sig, recID)
		if err != nil {
			continue
		}
		if pub.X.Cmp(prv.PublicKey.X) == 0 && pub.Y.Cmp(prv.PublicKey.Y) == 0 {
			return // Success: found the correct recovery ID.
		}
	}
	t.Error("failed to recover public key with either recovery ID")
}

func TestP256RecoverPubkeyInvalidInputs(t *testing.T) {
	_, err := P256RecoverPubkey([]byte("short"), make([]byte, 64), 0)
	if err == nil {
		t.Error("should reject short hash")
	}
	hash := sha256.Sum256([]byte("test"))
	_, err = P256RecoverPubkey(hash[:], make([]byte, 63), 0)
	if err == nil {
		t.Error("should reject short signature")
	}
	_, err = P256RecoverPubkey(hash[:], make([]byte, 64), 2)
	if err == nil {
		t.Error("should reject invalid recID")
	}
}

func TestP256RecoverPubkeyZeroRS(t *testing.T) {
	hash := sha256.Sum256([]byte("test"))
	sig := make([]byte, 64) // all zeros
	_, err := P256RecoverPubkey(hash[:], sig, 0)
	if err == nil {
		t.Error("should reject zero r,s")
	}
}

func TestP256ValidateSignatureValues(t *testing.T) {
	tests := []struct {
		r, s *big.Int
		lowS bool
		want bool
	}{
		{big.NewInt(1), big.NewInt(1), false, true},
		{nil, big.NewInt(1), false, false},
		{big.NewInt(1), nil, false, false},
		{big.NewInt(0), big.NewInt(1), false, false},
		{big.NewInt(1), big.NewInt(0), false, false},
		{big.NewInt(-1), big.NewInt(1), false, false},
		{new(big.Int).Set(p256N), big.NewInt(1), false, false},
		{big.NewInt(1), new(big.Int).Set(p256N), false, false},
	}
	for i, tc := range tests {
		got := P256ValidateSignatureValues(tc.r, tc.s, tc.lowS)
		if got != tc.want {
			t.Errorf("case %d: got %v, want %v", i, got, tc.want)
		}
	}
}

func TestP256ValidateSignatureValuesLowS(t *testing.T) {
	// Value just above half N.
	highS := new(big.Int).Add(p256HalfN, big.NewInt(1))
	if P256ValidateSignatureValues(big.NewInt(1), highS, true) {
		t.Error("high S should be rejected with lowS=true")
	}
	if !P256ValidateSignatureValues(big.NewInt(1), highS, false) {
		t.Error("high S should be accepted with lowS=false")
	}
}

func TestP256ScalarBaseMult(t *testing.T) {
	// k=1 should return the generator point.
	gx, gy := P256ScalarBaseMult(big.NewInt(1))
	if !p256Curve.IsOnCurve(gx, gy) {
		t.Error("1*G should be on curve")
	}
	if gx.Cmp(p256Params.Gx) != 0 || gy.Cmp(p256Params.Gy) != 0 {
		t.Error("1*G should equal generator")
	}
}

func TestP256ScalarMult(t *testing.T) {
	gx, gy := p256Params.Gx, p256Params.Gy
	// 2*G via scalar mult.
	x2, y2 := P256ScalarMult(gx, gy, big.NewInt(2))
	if !p256Curve.IsOnCurve(x2, y2) {
		t.Error("2*G should be on curve")
	}
	// 2*G via point addition.
	ax, ay := P256PointAdd(gx, gy, gx, gy)
	if x2.Cmp(ax) != 0 || y2.Cmp(ay) != 0 {
		t.Error("2*G via ScalarMult != G+G via PointAdd")
	}
}

func TestP256IsOnCurve(t *testing.T) {
	if !P256IsOnCurve(p256Params.Gx, p256Params.Gy) {
		t.Error("generator should be on curve")
	}
	if P256IsOnCurve(big.NewInt(0), big.NewInt(0)) {
		t.Error("origin should not be on curve")
	}
}
