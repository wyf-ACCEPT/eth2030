package crypto

import (
	"crypto/ecdsa"
	"math/big"
	"testing"
)

// FuzzKeccak256 hashes random data with Keccak-256.
// It must never panic and must always return exactly 32 bytes.
func FuzzKeccak256(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x00})
	f.Add([]byte{0xff})
	f.Add([]byte("hello world"))
	f.Add(make([]byte, 32))
	f.Add(make([]byte, 256))

	f.Fuzz(func(t *testing.T, data []byte) {
		h := Keccak256(data)
		if len(h) != 32 {
			t.Fatalf("Keccak256 output length: got %d, want 32", len(h))
		}

		// Determinism: same input always produces same output.
		h2 := Keccak256(data)
		for i := range h {
			if h[i] != h2[i] {
				t.Fatalf("Keccak256 non-deterministic at byte %d", i)
			}
		}

		// Multi-part hash: Keccak256(a, b) == Keccak256(concat(a, b)).
		if len(data) >= 2 {
			mid := len(data) / 2
			multi := Keccak256(data[:mid], data[mid:])
			single := Keccak256(data)
			for i := range multi {
				if multi[i] != single[i] {
					t.Fatalf("Keccak256 multi-part mismatch at byte %d", i)
				}
			}
		}

		// KeccakHash wrapper must also produce 32 bytes.
		hh := Keccak256Hash(data)
		if len(hh) != 32 {
			t.Fatalf("Keccak256Hash output length: got %d, want 32", len(hh))
		}
	})
}

// FuzzECDSASignVerify generates a key, signs random 32-byte hashes, and
// verifies that the signature roundtrips correctly.
func FuzzECDSASignVerify(f *testing.F) {
	// Seeds: valid 32-byte hashes (non-zero to avoid edge cases with zero hash).
	h1 := make([]byte, 32)
	h1[0] = 0xca
	h1[31] = 0xfe
	f.Add(h1)
	h2 := make([]byte, 32)
	for i := range h2 {
		h2[i] = byte(i + 1)
	}
	f.Add(h2)
	h3 := make([]byte, 32)
	for i := range h3 {
		h3[i] = 0xab
	}
	f.Add(h3)

	// Generate a private key once, reuse across fuzz iterations.
	prv, err := GenerateKey()
	if err != nil {
		f.Fatalf("GenerateKey: %v", err)
	}

	f.Fuzz(func(t *testing.T, hash []byte) {
		// Sign only accepts 32-byte hashes.
		if len(hash) != 32 {
			return
		}

		sig, err := Sign(hash, prv)
		if err != nil {
			t.Fatalf("Sign failed: %v", err)
		}
		if len(sig) != 65 {
			t.Fatalf("Signature length: got %d, want 65", len(sig))
		}

		// V must be 0 or 1.
		if sig[64] > 1 {
			t.Fatalf("Signature V byte: got %d, want 0 or 1", sig[64])
		}

		// Ecrecover must return the same public key.
		recovered, err := Ecrecover(hash, sig)
		if err != nil {
			t.Fatalf("Ecrecover failed: %v", err)
		}

		expectedPub := FromECDSAPub(&prv.PublicKey)
		if len(recovered) != len(expectedPub) {
			t.Fatalf("Recovered pubkey length: got %d, want %d", len(recovered), len(expectedPub))
		}
		for i := range recovered {
			if recovered[i] != expectedPub[i] {
				t.Fatalf("Recovered pubkey mismatch at byte %d", i)
			}
		}

		// ValidateSignature must return true.
		if !ValidateSignature(expectedPub, hash, sig[:64]) {
			t.Fatal("ValidateSignature returned false for valid signature")
		}

		// S must be in lower half (EIP-2 normalization).
		s := new(big.Int).SetBytes(sig[32:64])
		if s.Cmp(secp256k1halfN) > 0 {
			t.Fatal("Signature S not normalized to lower half")
		}
	})
}

// FuzzECDSARecoverRobustness feeds random data to Ecrecover and SigToPub.
// They must never panic on arbitrary input.
func FuzzECDSARecoverRobustness(f *testing.F) {
	// Seed: valid-length but random content.
	f.Add(make([]byte, 97)) // 32 (hash) + 65 (sig)
	// Seed: short data.
	f.Add([]byte{0x01, 0x02, 0x03})
	// Seed: empty.
	f.Add([]byte{})
	// Seed: a plausible but invalid signature.
	seed := make([]byte, 97)
	seed[0] = 0xaa
	seed[64] = 0x01 // V = 1 within the "sig" portion (bytes 32..96 of seed)
	f.Add(seed)

	f.Fuzz(func(t *testing.T, data []byte) {
		// Split data into hash-like and sig-like portions for Ecrecover.
		if len(data) >= 97 {
			hash := data[:32]
			sig := data[32:97]
			// Must not panic.
			_, _ = Ecrecover(hash, sig)
			_, _ = SigToPub(hash, sig)
		}

		// Also try with exact sizes but arbitrary content.
		if len(data) >= 32 {
			hash := data[:32]
			// Short sig: must not panic.
			_, _ = Ecrecover(hash, data)
			_, _ = SigToPub(hash, data)
		}

		// ValidateSignatureValues with arbitrary big.Ints: must not panic.
		if len(data) >= 3 {
			v := data[0]
			r := new(big.Int).SetBytes(data[1 : len(data)/2+1])
			s := new(big.Int).SetBytes(data[len(data)/2+1:])
			_ = ValidateSignatureValues(v, r, s, true)
			_ = ValidateSignatureValues(v, r, s, false)
		}

		// DecompressPubkey: must not panic.
		_, _ = DecompressPubkey(data)

		// CompressPubkey: must not panic on nil or empty.
		_ = CompressPubkey(nil)
		if len(data) >= 64 {
			pub := &ecdsa.PublicKey{
				Curve: S256(),
				X:     new(big.Int).SetBytes(data[:32]),
				Y:     new(big.Int).SetBytes(data[32:64]),
			}
			_ = CompressPubkey(pub)
		}
	})
}

// FuzzBN254AddRobustness feeds random 128-byte inputs to BN254Add.
// It must never panic on arbitrary input.
func FuzzBN254AddRobustness(f *testing.F) {
	// Seed: all zeros (point at infinity + point at infinity).
	f.Add(make([]byte, 128))
	// Seed: generator point (1, 2) + zero.
	seed := make([]byte, 128)
	seed[31] = 1 // x1 = 1
	seed[63] = 2 // y1 = 2
	f.Add(seed)
	// Seed: two generators.
	seed2 := make([]byte, 128)
	seed2[31] = 1
	seed2[63] = 2
	seed2[95] = 1
	seed2[127] = 2
	f.Add(seed2)
	// Seed: short input (tests padding).
	f.Add([]byte{0x01})
	// Seed: invalid point (random).
	f.Add([]byte{0xff, 0xff, 0xff, 0xff})
	// Seed: empty.
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		// Limit to reasonable size.
		if len(data) > 256 {
			return
		}

		// BN254Add: must not panic.
		_, _ = BN254Add(data)

		// BN254ScalarMul: must not panic.
		if len(data) >= 96 {
			_, _ = BN254ScalarMul(data[:96])
		}
		_, _ = BN254ScalarMul(data)

		// BN254PairingCheck with valid-length multiples: must not panic.
		if len(data) >= 192 && len(data)%192 == 0 {
			_, _ = BN254PairingCheck(data)
		}
	})
}

// FuzzBLS12G1AddRobustness feeds random data to BLS12-381 G1 Add.
// It must never panic on arbitrary input.
func FuzzBLS12G1AddRobustness(f *testing.F) {
	// Seed: all zeros (infinity + infinity).
	f.Add(make([]byte, 256)) // 2 * 128 bytes
	// Seed: short input.
	f.Add([]byte{0x01})
	// Seed: empty.
	f.Add([]byte{})
	// Seed: 256 bytes of 0xff (invalid field elements, top 16 bytes non-zero).
	allFF := make([]byte, 256)
	for i := range allFF {
		allFF[i] = 0xff
	}
	f.Add(allFF)
	// Seed: valid G1 generator encoding.
	// G1 generator x and y, each padded to 64 bytes (16 zero bytes + 48 byte coordinate).
	genSeed := make([]byte, 256)
	// x coordinate of BLS12-381 G1 generator (48 bytes).
	gx, _ := new(big.Int).SetString(
		"17f1d3a73197d7942695638c4fa9ac0fc3688c4f9774b905a14e3a3f171bac586c55e83ff97a1aeffb3af00adb22c6bb", 16)
	gy, _ := new(big.Int).SetString(
		"08b3f481e3aaa0f1a09e30ed741d8ae4fcf5e095d5d00af600db18cb2c04b3edd03cc744a2888ae40caa232946c5e7e1", 16)
	gxBytes := gx.Bytes()
	gyBytes := gy.Bytes()
	copy(genSeed[64-len(gxBytes):64], gxBytes)
	copy(genSeed[128-len(gyBytes):128], gyBytes)
	// Second point is infinity (zeros), already set.
	f.Add(genSeed)

	f.Fuzz(func(t *testing.T, data []byte) {
		// Limit to reasonable size.
		if len(data) > 1024 {
			return
		}

		// BLS12G1Add requires exactly 256 bytes; test both exact and wrong sizes.
		_, _ = BLS12G1Add(data)

		// BLS12G1Mul requires exactly 160 bytes (128 point + 32 scalar).
		if len(data) >= 160 {
			_, _ = BLS12G1Mul(data[:160])
		}

		// BLS12G1MSM requires multiples of 160 bytes.
		if len(data) >= 160 && len(data)%160 == 0 {
			_, _ = BLS12G1MSM(data)
		}

		// BLS12G2Add requires exactly 512 bytes.
		if len(data) >= 512 {
			_, _ = BLS12G2Add(data[:512])
		}

		// BLS12MapFpToG1 requires exactly 64 bytes.
		if len(data) >= 64 {
			_, _ = BLS12MapFpToG1(data[:64])
		}

		// BLS12MapFp2ToG2 requires exactly 128 bytes.
		if len(data) >= 128 {
			_, _ = BLS12MapFp2ToG2(data[:128])
		}
	})
}
