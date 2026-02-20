package crypto

import (
	"math/big"
	"testing"
)

func TestParseCompactSignature(t *testing.T) {
	sig := make([]byte, 65)
	sig[0] = 0xAA  // first byte of R
	sig[32] = 0xBB // first byte of S
	sig[64] = 1    // V

	cs, err := ParseCompactSignature(sig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cs.R[0] != 0xAA {
		t.Fatalf("R[0] = %x, want 0xAA", cs.R[0])
	}
	if cs.S[0] != 0xBB {
		t.Fatalf("S[0] = %x, want 0xBB", cs.S[0])
	}
	if cs.V != 1 {
		t.Fatalf("V = %d, want 1", cs.V)
	}
}

func TestParseCompactSignatureTooShort(t *testing.T) {
	_, err := ParseCompactSignature(make([]byte, 64))
	if err != ErrSigRecoverInvalidLength {
		t.Fatalf("expected ErrSigRecoverInvalidLength, got %v", err)
	}
}

func TestCompactSignatureRoundTrip(t *testing.T) {
	orig := make([]byte, 65)
	for i := range orig {
		orig[i] = byte(i)
	}
	orig[64] = 0 // valid V

	cs, err := ParseCompactSignature(orig)
	if err != nil {
		t.Fatal(err)
	}
	encoded := cs.Bytes()
	if len(encoded) != 65 {
		t.Fatalf("encoded length = %d, want 65", len(encoded))
	}
	for i := range orig {
		if encoded[i] != orig[i] {
			t.Fatalf("byte %d: %x != %x", i, encoded[i], orig[i])
		}
	}
}

func TestNormalizeVRaw(t *testing.T) {
	for _, v := range []int64{0, 1} {
		rawV, chainID := NormalizeV(big.NewInt(v))
		if rawV != byte(v) {
			t.Errorf("NormalizeV(%d): rawV = %d, want %d", v, rawV, v)
		}
		if chainID.Sign() != 0 {
			t.Errorf("NormalizeV(%d): chainID = %s, want 0", v, chainID)
		}
	}
}

func TestNormalizeVLegacy(t *testing.T) {
	rawV, chainID := NormalizeV(big.NewInt(27))
	if rawV != 0 {
		t.Errorf("rawV = %d, want 0", rawV)
	}
	if chainID.Sign() != 0 {
		t.Errorf("chainID = %s, want 0", chainID)
	}

	rawV, chainID = NormalizeV(big.NewInt(28))
	if rawV != 1 {
		t.Errorf("rawV = %d, want 1", rawV)
	}
	if chainID.Sign() != 0 {
		t.Errorf("chainID = %s, want 0", chainID)
	}
}

func TestNormalizeVEIP155(t *testing.T) {
	// Chain ID 1 (mainnet): v = 37 -> rawV=0, chainID=1
	rawV, chainID := NormalizeV(big.NewInt(37))
	if rawV != 0 {
		t.Errorf("rawV = %d, want 0", rawV)
	}
	if chainID.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("chainID = %s, want 1", chainID)
	}

	// Chain ID 1: v = 38 -> rawV=1, chainID=1
	rawV, chainID = NormalizeV(big.NewInt(38))
	if rawV != 1 {
		t.Errorf("rawV = %d, want 1", rawV)
	}
	if chainID.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("chainID = %s, want 1", chainID)
	}

	// Chain ID 5 (Goerli): v = 45 -> rawV=0, chainID=5
	rawV, chainID = NormalizeV(big.NewInt(45))
	if rawV != 0 {
		t.Errorf("rawV = %d, want 0", rawV)
	}
	if chainID.Cmp(big.NewInt(5)) != 0 {
		t.Errorf("chainID = %s, want 5", chainID)
	}
}

func TestEncodeVLegacy(t *testing.T) {
	if EncodeVLegacy(0) != 27 {
		t.Fatal("EncodeVLegacy(0) != 27")
	}
	if EncodeVLegacy(1) != 28 {
		t.Fatal("EncodeVLegacy(1) != 28")
	}
}

func TestEncodeVEIP155(t *testing.T) {
	// Chain ID 1: 35 + 2*1 + 0 = 37
	v := EncodeVEIP155(0, big.NewInt(1))
	if v.Cmp(big.NewInt(37)) != 0 {
		t.Fatalf("EncodeVEIP155(0, 1) = %s, want 37", v)
	}
	// Chain ID 1: 35 + 2*1 + 1 = 38
	v = EncodeVEIP155(1, big.NewInt(1))
	if v.Cmp(big.NewInt(38)) != 0 {
		t.Fatalf("EncodeVEIP155(1, 1) = %s, want 38", v)
	}
}

func TestEncodeDecodeVEIP155RoundTrip(t *testing.T) {
	for _, chainID := range []int64{1, 5, 137, 10, 42161} {
		for _, rawV := range []byte{0, 1} {
			encoded := EncodeVEIP155(rawV, big.NewInt(chainID))
			gotV, gotChain := NormalizeV(encoded)
			if gotV != rawV {
				t.Errorf("chainID=%d rawV=%d: gotV=%d", chainID, rawV, gotV)
			}
			if gotChain.Int64() != chainID {
				t.Errorf("chainID=%d rawV=%d: gotChain=%s", chainID, rawV, gotChain)
			}
		}
	}
}

func TestValidateSignatureComponents(t *testing.T) {
	// Valid: mid-range R and S in lower half.
	r := new(big.Int).Div(secp256k1N, big.NewInt(2))
	s := new(big.Int).Div(secp256k1N, big.NewInt(4))
	if err := validateSigComponents(r, s, 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Invalid V.
	if err := validateSigComponents(r, s, 2); err != ErrSigRecoverInvalidV {
		t.Fatalf("expected ErrSigRecoverInvalidV, got %v", err)
	}

	// R = 0.
	if err := validateSigComponents(big.NewInt(0), s, 0); err != ErrSigRecoverInvalidR {
		t.Fatalf("expected ErrSigRecoverInvalidR, got %v", err)
	}

	// R = n.
	if err := validateSigComponents(new(big.Int).Set(secp256k1N), s, 0); err != ErrSigRecoverInvalidR {
		t.Fatalf("expected ErrSigRecoverInvalidR, got %v", err)
	}

	// S = 0.
	if err := validateSigComponents(r, big.NewInt(0), 0); err != ErrSigRecoverInvalidS {
		t.Fatalf("expected ErrSigRecoverInvalidS, got %v", err)
	}

	// S in upper half.
	highS := new(big.Int).Add(secp256k1halfN, big.NewInt(1))
	if err := validateSigComponents(r, highS, 0); err != ErrSigRecoverMalleable {
		t.Fatalf("expected ErrSigRecoverMalleable, got %v", err)
	}
}

func TestNormalizeSFlipsToLowerHalf(t *testing.T) {
	// Create a signature with S in the upper half.
	highS := new(big.Int).Add(secp256k1halfN, big.NewInt(100))
	cs := &CompactSignature{V: 0}
	copy(cs.R[:], big.NewInt(42).Bytes())
	sBytes := highS.Bytes()
	copy(cs.S[32-len(sBytes):], sBytes)

	cs.NormalizeS()

	normalizedS := cs.SBigInt()
	if normalizedS.Cmp(secp256k1halfN) > 0 {
		t.Fatal("S still in upper half after normalization")
	}
	if cs.V != 1 {
		t.Fatalf("V should be flipped to 1, got %d", cs.V)
	}
}

func TestNormalizeSNoOpForLowerHalf(t *testing.T) {
	lowS := new(big.Int).Div(secp256k1halfN, big.NewInt(2))
	cs := &CompactSignature{V: 0}
	sBytes := lowS.Bytes()
	copy(cs.S[32-len(sBytes):], sBytes)

	cs.NormalizeS()

	if cs.SBigInt().Cmp(lowS) != 0 {
		t.Fatal("S should not change when already in lower half")
	}
	if cs.V != 0 {
		t.Fatalf("V should remain 0, got %d", cs.V)
	}
}

func TestSignatureRecoverRoundTrip(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	hash := Keccak256([]byte("test message"))
	sig, err := Sign(hash, key)
	if err != nil {
		t.Fatal(err)
	}

	sr := NewSigRecover()
	cs, err := ParseCompactSignature(sig)
	if err != nil {
		t.Fatal(err)
	}

	// Recover address.
	addr, err := sr.SignatureToAddress(hash, cs)
	if err != nil {
		t.Fatalf("SignatureToAddress: %v", err)
	}

	expectedAddr := PubkeyToAddress(key.PublicKey)
	if addr != expectedAddr {
		t.Fatalf("recovered address %s != expected %s", addr.Hex(), expectedAddr.Hex())
	}
}

func TestRecoverPublicKeyRoundTripSigRecover(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	hash := Keccak256([]byte("hello ethereum"))
	sig, err := Sign(hash, key)
	if err != nil {
		t.Fatal(err)
	}

	sr := NewSigRecover()
	cs, err := ParseCompactSignature(sig)
	if err != nil {
		t.Fatal(err)
	}

	pub, err := sr.RecoverPublicKey(hash, cs)
	if err != nil {
		t.Fatalf("RecoverPublicKey: %v", err)
	}

	expected := FromECDSAPub(&key.PublicKey)
	if len(pub) != len(expected) {
		t.Fatalf("pubkey length %d != %d", len(pub), len(expected))
	}
	for i := range pub {
		if pub[i] != expected[i] {
			t.Fatalf("pubkey byte %d: %x != %x", i, pub[i], expected[i])
		}
	}
}

func TestEcRecoverPrecompile(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	hash := Keccak256([]byte("ecrecover test"))
	sig, err := Sign(hash, key)
	if err != nil {
		t.Fatal(err)
	}

	// Build 128-byte precompile input: hash(32) || v(32) || r(32) || s(32).
	input := make([]byte, 128)
	copy(input[:32], hash)
	// V in 32-byte form: legacy encoding (27 or 28).
	v := big.NewInt(int64(sig[64] + 27))
	vBytes := v.Bytes()
	copy(input[64-len(vBytes):64], vBytes)
	// R.
	copy(input[64:96], sig[:32])
	// S.
	copy(input[96:128], sig[32:64])

	sr := NewSigRecover()
	result := sr.EcRecoverPrecompile(input)
	if result == nil {
		t.Fatal("EcRecoverPrecompile returned nil")
	}
	if len(result) != 32 {
		t.Fatalf("result length = %d, want 32", len(result))
	}

	expectedAddr := PubkeyToAddress(key.PublicKey)
	for i := 0; i < 20; i++ {
		if result[12+i] != expectedAddr[i] {
			t.Fatalf("address byte %d: %x != %x", i, result[12+i], expectedAddr[i])
		}
	}
}

func TestEcRecoverPrecompileInvalidV(t *testing.T) {
	sr := NewSigRecover()

	input := make([]byte, 128)
	input[63] = 26 // V = 26 (invalid, must be 27 or 28)
	result := sr.EcRecoverPrecompile(input)
	if result != nil {
		t.Fatal("should return nil for invalid V")
	}
}

func TestEcRecoverPrecompileShortInput(t *testing.T) {
	sr := NewSigRecover()
	// Short input should be zero-padded.
	result := sr.EcRecoverPrecompile(make([]byte, 64))
	// V will be 0, which is neither 27 nor 28.
	if result != nil {
		t.Fatal("should return nil for zero V")
	}
}

func TestBatchSignatureVerification(t *testing.T) {
	sr := NewSigRecover()
	n := 10
	hashes := make([][]byte, n)
	sigs := make([]*CompactSignature, n)
	expectedAddrs := make([]string, n)

	for i := 0; i < n; i++ {
		key, err := GenerateKey()
		if err != nil {
			t.Fatal(err)
		}
		hash := Keccak256([]byte{byte(i), byte(i + 1)})
		sig, err := Sign(hash, key)
		if err != nil {
			t.Fatal(err)
		}
		cs, err := ParseCompactSignature(sig)
		if err != nil {
			t.Fatal(err)
		}
		hashes[i] = hash
		sigs[i] = cs
		expectedAddrs[i] = PubkeyToAddress(key.PublicKey).Hex()
	}

	results, err := sr.BatchSignatureVerification(hashes, sigs)
	if err != nil {
		t.Fatalf("batch verification: %v", err)
	}
	if len(results) != n {
		t.Fatalf("results length = %d, want %d", len(results), n)
	}
	for i, r := range results {
		if r.Err != nil {
			t.Fatalf("result %d: unexpected error %v", i, r.Err)
		}
		if r.Address.Hex() != expectedAddrs[i] {
			t.Fatalf("result %d: address %s != %s", i, r.Address.Hex(), expectedAddrs[i])
		}
	}
}

func TestBatchSignatureVerificationEmpty(t *testing.T) {
	sr := NewSigRecover()
	_, err := sr.BatchSignatureVerification(nil, nil)
	if err != ErrSigRecoverBatchEmpty {
		t.Fatalf("expected ErrSigRecoverBatchEmpty, got %v", err)
	}
}

func TestBatchSignatureVerificationMismatch(t *testing.T) {
	sr := NewSigRecover()
	_, err := sr.BatchSignatureVerification(
		[][]byte{{1}},
		[]*CompactSignature{{}, {}},
	)
	if err != ErrSigRecoverBatchMismatch {
		t.Fatalf("expected ErrSigRecoverBatchMismatch, got %v", err)
	}
}

func TestIsValidSignature(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	hash := Keccak256([]byte("valid"))
	sig, err := Sign(hash, key)
	if err != nil {
		t.Fatal(err)
	}
	if !IsValidSignature(sig) {
		t.Fatal("valid signature not recognized")
	}

	// Invalid: too short.
	if IsValidSignature(sig[:64]) {
		t.Fatal("short signature should be invalid")
	}

	// Invalid: zero R.
	badSig := make([]byte, 65)
	copy(badSig, sig)
	for i := 0; i < 32; i++ {
		badSig[i] = 0
	}
	if IsValidSignature(badSig) {
		t.Fatal("zero R should be invalid")
	}
}

func TestRecoverEIP155Sender(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	chainID := big.NewInt(1)
	hash := Keccak256([]byte("eip155 test"))
	sig, err := Sign(hash, key)
	if err != nil {
		t.Fatal(err)
	}

	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:64])
	rawV := sig[64]
	v := EncodeVEIP155(rawV, chainID)

	sr := NewSigRecover()
	addr, err := sr.RecoverEIP155Sender(hash, v, r, s, chainID)
	if err != nil {
		t.Fatalf("RecoverEIP155Sender: %v", err)
	}

	expected := PubkeyToAddress(key.PublicKey)
	if addr != expected {
		t.Fatalf("address %s != %s", addr.Hex(), expected.Hex())
	}
}

func TestRecoverEIP155WrongChainID(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	hash := Keccak256([]byte("wrong chain"))
	sig, err := Sign(hash, key)
	if err != nil {
		t.Fatal(err)
	}

	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:64])
	rawV := sig[64]

	// Encode for chain ID 1, but expect chain ID 5.
	v := EncodeVEIP155(rawV, big.NewInt(1))

	sr := NewSigRecover()
	_, err = sr.RecoverEIP155Sender(hash, v, r, s, big.NewInt(5))
	if err != ErrSigRecoverInvalidV {
		t.Fatalf("expected ErrSigRecoverInvalidV, got %v", err)
	}
}

func TestRecoverCompressed(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	hash := Keccak256([]byte("compressed"))
	sig, err := Sign(hash, key)
	if err != nil {
		t.Fatal(err)
	}

	sr := NewSigRecover()
	cs, _ := ParseCompactSignature(sig)
	compressed, err := sr.RecoverCompressed(hash, cs)
	if err != nil {
		t.Fatalf("RecoverCompressed: %v", err)
	}
	if len(compressed) != 33 {
		t.Fatalf("compressed length = %d, want 33", len(compressed))
	}
	if compressed[0] != 0x02 && compressed[0] != 0x03 {
		t.Fatalf("invalid prefix: %x", compressed[0])
	}
}
