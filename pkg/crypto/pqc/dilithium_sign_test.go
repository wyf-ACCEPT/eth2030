package pqc

import (
	"testing"
)

func TestDilithiumKeypairGeneration(t *testing.T) {
	pk, sk := DilithiumKeypair()
	if len(pk) != DSign3PubKeyBytes {
		t.Fatalf("public key length = %d, want %d", len(pk), DSign3PubKeyBytes)
	}
	if len(sk) != DSign3SecKeyBytes {
		t.Fatalf("secret key length = %d, want %d", len(sk), DSign3SecKeyBytes)
	}
}

func TestDilithiumKeypairUniqueness(t *testing.T) {
	pk1, sk1 := DilithiumKeypair()
	pk2, sk2 := DilithiumKeypair()

	if dsTestBytesEqual(pk1, pk2) {
		t.Fatal("two keypairs should have different public keys")
	}
	if dsTestBytesEqual(sk1, sk2) {
		t.Fatal("two keypairs should have different secret keys")
	}
}

func TestDilithiumSignVerifyReal(t *testing.T) {
	pk, sk := DilithiumKeypair()
	msg := []byte("lattice-based digital signature test")

	sig := DilithiumSign(sk, msg)
	if sig == nil {
		t.Fatal("signing returned nil")
	}
	if len(sig) != DSign3SigBytes {
		t.Fatalf("signature length = %d, want %d", len(sig), DSign3SigBytes)
	}

	if !DilithiumVerify(pk, msg, sig) {
		t.Fatal("valid signature rejected")
	}
}

func TestDilithiumSignDifferentMessages(t *testing.T) {
	pk, sk := DilithiumKeypair()

	msgs := []string{
		"first message",
		"second message",
		"a longer third message with additional content for testing",
	}

	for _, m := range msgs {
		msg := []byte(m)
		sig := DilithiumSign(sk, msg)
		if sig == nil {
			t.Fatalf("signing %q returned nil", m)
		}
		if !DilithiumVerify(pk, msg, sig) {
			t.Fatalf("valid signature rejected for %q", m)
		}
	}
}

func TestDilithiumVerifyWrongMessageReal(t *testing.T) {
	pk, sk := DilithiumKeypair()
	sig := DilithiumSign(sk, []byte("correct message"))
	if sig == nil {
		t.Fatal("signing returned nil")
	}

	if DilithiumVerify(pk, []byte("wrong message"), sig) {
		t.Fatal("signature verified for wrong message")
	}
}

func TestDilithiumVerifyWrongKey(t *testing.T) {
	_, sk1 := DilithiumKeypair()
	pk2, _ := DilithiumKeypair()

	msg := []byte("test message")
	sig := DilithiumSign(sk1, msg)
	if sig == nil {
		t.Fatal("signing returned nil")
	}

	if DilithiumVerify(pk2, msg, sig) {
		t.Fatal("signature verified with wrong public key")
	}
}

func TestDilithiumVerifyTamperedSignature(t *testing.T) {
	pk, sk := DilithiumKeypair()
	msg := []byte("tamper test")
	sig := DilithiumSign(sk, msg)
	if sig == nil {
		t.Fatal("signing returned nil")
	}

	// Tamper with the signature.
	tampered := make(DSign3Signature, len(sig))
	copy(tampered, sig)
	tampered[0] ^= 0xFF

	if DilithiumVerify(pk, msg, tampered) {
		t.Fatal("tampered signature should not verify")
	}
}

func TestDilithiumSignEmptyMessage(t *testing.T) {
	_, sk := DilithiumKeypair()
	sig := DilithiumSign(sk, []byte{})
	if sig != nil {
		t.Fatal("signing empty message should return nil")
	}
}

func TestDilithiumSignNilMessage(t *testing.T) {
	_, sk := DilithiumKeypair()
	sig := DilithiumSign(sk, nil)
	if sig != nil {
		t.Fatal("signing nil message should return nil")
	}
}

func TestDilithiumSignShortKey(t *testing.T) {
	sig := DilithiumSign(DSign3PrivateKey([]byte{1, 2, 3}), []byte("test"))
	if sig != nil {
		t.Fatal("signing with short key should return nil")
	}
}

func TestDilithiumVerifyShortPK(t *testing.T) {
	if DilithiumVerify(DSign3PublicKey([]byte{1}), []byte("msg"), make(DSign3Signature, DSign3SigBytes)) {
		t.Fatal("verification with short PK should fail")
	}
}

func TestDilithiumVerifyShortSig(t *testing.T) {
	pk, _ := DilithiumKeypair()
	if DilithiumVerify(pk, []byte("msg"), DSign3Signature([]byte{1, 2, 3})) {
		t.Fatal("verification with short signature should fail")
	}
}

func TestDilithiumBatchVerify(t *testing.T) {
	const n = 5
	pks := make([]DSign3PublicKey, n)
	msgs := make([][]byte, n)
	sigs := make([]DSign3Signature, n)

	for i := 0; i < n; i++ {
		pk, sk := DilithiumKeypair()
		msg := []byte("batch message " + string(rune('A'+i)))
		sig := DilithiumSign(sk, msg)
		if sig == nil {
			t.Fatalf("signing message %d returned nil", i)
		}
		pks[i] = pk
		msgs[i] = msg
		sigs[i] = sig
	}

	if !DilithiumBatchVerify(pks, msgs, sigs) {
		t.Fatal("batch verification of valid signatures failed")
	}
}

func TestDilithiumBatchVerifyOneBad(t *testing.T) {
	const n = 3
	pks := make([]DSign3PublicKey, n)
	msgs := make([][]byte, n)
	sigs := make([]DSign3Signature, n)

	for i := 0; i < n; i++ {
		pk, sk := DilithiumKeypair()
		msg := []byte("batch message " + string(rune('A'+i)))
		sig := DilithiumSign(sk, msg)
		if sig == nil {
			t.Fatalf("signing message %d returned nil", i)
		}
		pks[i] = pk
		msgs[i] = msg
		sigs[i] = sig
	}

	// Corrupt one signature.
	sigs[1] = make(DSign3Signature, DSign3SigBytes) // all zeros
	if DilithiumBatchVerify(pks, msgs, sigs) {
		t.Fatal("batch verification should fail with one bad signature")
	}
}

func TestDilithiumBatchVerifyEmpty(t *testing.T) {
	if DilithiumBatchVerify(nil, nil, nil) {
		t.Fatal("batch verification of empty inputs should fail")
	}
}

func TestDilithiumBatchVerifyMismatchedLengths(t *testing.T) {
	pk, _ := DilithiumKeypair()
	if DilithiumBatchVerify(
		[]DSign3PublicKey{pk},
		[][]byte{[]byte("msg1"), []byte("msg2")},
		[]DSign3Signature{make(DSign3Signature, DSign3SigBytes)},
	) {
		t.Fatal("mismatched lengths should fail")
	}
}

func TestDilithiumConstants(t *testing.T) {
	// Verify Dilithium-3 parameter constants.
	if DSign3N != 256 {
		t.Fatalf("N = %d, want 256", DSign3N)
	}
	if DSign3Q != 8380417 {
		t.Fatalf("Q = %d, want 8380417", DSign3Q)
	}
	if DSign3K != 6 {
		t.Fatalf("K = %d, want 6", DSign3K)
	}
	if DSign3L != 5 {
		t.Fatalf("L = %d, want 5", DSign3L)
	}
	if DSign3Beta != DSign3Tau*DSign3Eta {
		t.Fatalf("Beta = %d, want tau*eta = %d", DSign3Beta, DSign3Tau*DSign3Eta)
	}
}

func TestDs3PolyArithmetic(t *testing.T) {
	q := int64(DSign3Q)

	// Test addition.
	a := []int64{1, 2, 3, 4}
	b := []int64{5, 6, 7, 8}
	sum := ds3PolyAdd(a, b, q)
	if sum[0] != 6 || sum[1] != 8 || sum[2] != 10 || sum[3] != 12 {
		t.Fatalf("polyAdd: %v", sum)
	}

	// Test subtraction.
	diff := ds3PolySub(sum, b, q)
	for i := range a {
		if diff[i] != a[i] {
			t.Fatalf("polySub[%d] = %d, want %d", i, diff[i], a[i])
		}
	}

	// Test multiplication: (1+x)^2 = 1 + 2x + x^2 mod (x^4+1).
	c := []int64{1, 1, 0, 0}
	prod := ds3PolyMul(c, c, 4, q)
	if prod[0] != 1 || prod[1] != 2 || prod[2] != 1 || prod[3] != 0 {
		t.Fatalf("polyMul: %v", prod)
	}
}

func TestDs3ModArithmetic(t *testing.T) {
	q := int64(DSign3Q)

	if ds3Mod(0, q) != 0 {
		t.Fatal("mod(0) != 0")
	}
	if ds3Mod(q, q) != 0 {
		t.Fatal("mod(Q) != 0")
	}
	if ds3Mod(-1, q) != q-1 {
		t.Fatalf("mod(-1) = %d, want %d", ds3Mod(-1, q), q-1)
	}
	if ds3Center(q-1, q) != -1 {
		t.Fatalf("center(Q-1) = %d, want -1", ds3Center(q-1, q))
	}
}

// dsTestBytesEqual checks if two byte slices are equal.
func dsTestBytesEqual(a, b []byte) bool {
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
