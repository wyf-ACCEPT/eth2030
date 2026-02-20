package pqc

import (
	"sync"
	"testing"
)

func TestCustodyReplacerNew(t *testing.T) {
	cr := NewCustodyReplacer(DefaultCustodyReplacerConfig())
	if cr == nil {
		t.Fatal("NewCustodyReplacer returned nil")
	}
	if cr.ActiveKeys() != 0 {
		t.Errorf("ActiveKeys() = %d, want 0", cr.ActiveKeys())
	}
}

func TestCustodyReplacerDefaultConfig(t *testing.T) {
	cfg := DefaultCustodyReplacerConfig()
	if cfg.SecurityLevel != 128 {
		t.Errorf("SecurityLevel = %d, want 128", cfg.SecurityLevel)
	}
	if cfg.HashFunction != "keccak256" {
		t.Errorf("HashFunction = %s, want keccak256", cfg.HashFunction)
	}
	if cfg.MaxCustodySlots != defaultMaxSlots {
		t.Errorf("MaxCustodySlots = %d, want %d", cfg.MaxCustodySlots, defaultMaxSlots)
	}
}

func TestCustodyGenerateKeyPair(t *testing.T) {
	cr := NewCustodyReplacer(DefaultCustodyReplacerConfig())

	kp, err := cr.GenerateKeyPair(1)
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if len(kp.PublicKey) != 32 {
		t.Errorf("PublicKey len = %d, want 32", len(kp.PublicKey))
	}
	if len(kp.PrivateKey) != custodyKeySize {
		t.Errorf("PrivateKey len = %d, want %d", len(kp.PrivateKey), custodyKeySize)
	}
	if kp.ValidatorIndex != 1 {
		t.Errorf("ValidatorIndex = %d, want 1", kp.ValidatorIndex)
	}
	if cr.ActiveKeys() != 1 {
		t.Errorf("ActiveKeys() = %d, want 1", cr.ActiveKeys())
	}
}

func TestCustodyGenerateMultipleKeys(t *testing.T) {
	cr := NewCustodyReplacer(DefaultCustodyReplacerConfig())

	for i := uint64(0); i < 5; i++ {
		_, err := cr.GenerateKeyPair(i)
		if err != nil {
			t.Fatalf("GenerateKeyPair(%d): %v", i, err)
		}
	}
	if cr.ActiveKeys() != 5 {
		t.Errorf("ActiveKeys() = %d, want 5", cr.ActiveKeys())
	}
}

func TestCustodyGenerateKeyPairOverwrite(t *testing.T) {
	cr := NewCustodyReplacer(DefaultCustodyReplacerConfig())

	kp1, _ := cr.GenerateKeyPair(1)
	kp2, _ := cr.GenerateKeyPair(1)

	// Keys should differ (random generation).
	if cr.ActiveKeys() != 1 {
		t.Errorf("ActiveKeys() = %d after overwrite, want 1", cr.ActiveKeys())
	}
	// Public keys should differ with overwhelming probability.
	same := true
	for i := range kp1.PublicKey {
		if kp1.PublicKey[i] != kp2.PublicKey[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("overwritten key pair has same public key (extremely unlikely)")
	}
}

func TestCustodyProveSlot(t *testing.T) {
	cr := NewCustodyReplacer(DefaultCustodyReplacerConfig())
	cr.GenerateKeyPair(1)

	proof, err := cr.ProveSlot(100, 1, []byte("test data"))
	if err != nil {
		t.Fatalf("ProveSlot: %v", err)
	}
	if proof.Slot != 100 {
		t.Errorf("Slot = %d, want 100", proof.Slot)
	}
	if proof.ValidatorIndex != 1 {
		t.Errorf("ValidatorIndex = %d, want 1", proof.ValidatorIndex)
	}
	if len(proof.ProofBytes) != custodyProofSize {
		t.Errorf("ProofBytes len = %d, want %d", len(proof.ProofBytes), custodyProofSize)
	}
	if len(proof.Commitment) != 32 {
		t.Errorf("Commitment len = %d, want 32", len(proof.Commitment))
	}
}

func TestCustodyProveSlotNoKey(t *testing.T) {
	cr := NewCustodyReplacer(DefaultCustodyReplacerConfig())

	_, err := cr.ProveSlot(100, 999, []byte("data"))
	if err != ErrCustodyKeyNotFound {
		t.Errorf("ProveSlot without key: got %v, want %v", err, ErrCustodyKeyNotFound)
	}
}

func TestCustodyVerifyProof(t *testing.T) {
	cr := NewCustodyReplacer(DefaultCustodyReplacerConfig())
	kp, _ := cr.GenerateKeyPair(1)

	proof, err := cr.ProveSlot(42, 1, []byte("custody data"))
	if err != nil {
		t.Fatalf("ProveSlot: %v", err)
	}

	valid, err := cr.VerifyProof(proof, kp.PublicKey)
	if err != nil {
		t.Fatalf("VerifyProof: %v", err)
	}
	if !valid {
		t.Error("VerifyProof returned false for valid proof")
	}
}

func TestCustodyVerifyProofWrongPubKey(t *testing.T) {
	cr := NewCustodyReplacer(DefaultCustodyReplacerConfig())
	cr.GenerateKeyPair(1)

	proof, _ := cr.ProveSlot(42, 1, []byte("data"))

	wrongKey := make([]byte, 32)
	wrongKey[0] = 0xff
	valid, err := cr.VerifyProof(proof, wrongKey)
	if valid {
		t.Error("VerifyProof should fail with wrong public key")
	}
	if err != ErrCustodyProofInvalid {
		t.Errorf("expected ErrCustodyProofInvalid, got %v", err)
	}
}

func TestCustodyVerifyProofNilInputs(t *testing.T) {
	cr := NewCustodyReplacer(DefaultCustodyReplacerConfig())

	// Nil proof.
	valid, err := cr.VerifyProof(nil, []byte("key"))
	if valid || err != ErrCustodyProofInvalid {
		t.Error("should reject nil proof")
	}

	// Empty pub key.
	valid, err = cr.VerifyProof(&CustodyProofPQ{ProofBytes: make([]byte, custodyProofSize)}, nil)
	if valid || err != ErrCustodyProofInvalid {
		t.Error("should reject empty pub key")
	}

	// Wrong proof size.
	valid, err = cr.VerifyProof(&CustodyProofPQ{
		ProofBytes: []byte("short"),
		Commitment: []byte("c"),
	}, []byte("key"))
	if valid || err != ErrCustodyProofInvalid {
		t.Error("should reject wrong proof size")
	}
}

func TestCustodyRotateKeys(t *testing.T) {
	cr := NewCustodyReplacer(DefaultCustodyReplacerConfig())
	kp1, _ := cr.GenerateKeyPair(1)

	// Create a proof with the old key.
	cr.ProveSlot(10, 1, []byte("old data"))

	kp2, err := cr.RotateKeys(1)
	if err != nil {
		t.Fatalf("RotateKeys: %v", err)
	}

	// Keys should differ.
	same := true
	for i := range kp1.PublicKey {
		if kp1.PublicKey[i] != kp2.PublicKey[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("rotated key pair has same public key")
	}

	// Active keys count should remain 1.
	if cr.ActiveKeys() != 1 {
		t.Errorf("ActiveKeys() = %d after rotation, want 1", cr.ActiveKeys())
	}
}

func TestCustodyRotateKeysNotFound(t *testing.T) {
	cr := NewCustodyReplacer(DefaultCustodyReplacerConfig())

	_, err := cr.RotateKeys(999)
	if err != ErrCustodyKeyNotFound {
		t.Errorf("RotateKeys for missing key: got %v, want %v", err, ErrCustodyKeyNotFound)
	}
}

func TestCustodyRevokeKey(t *testing.T) {
	cr := NewCustodyReplacer(DefaultCustodyReplacerConfig())
	cr.GenerateKeyPair(1)
	cr.GenerateKeyPair(2)

	if err := cr.RevokeKey(1); err != nil {
		t.Fatalf("RevokeKey: %v", err)
	}
	if cr.ActiveKeys() != 1 {
		t.Errorf("ActiveKeys() = %d after revoke, want 1", cr.ActiveKeys())
	}

	// Prove with revoked key should fail.
	_, err := cr.ProveSlot(10, 1, []byte("data"))
	if err != ErrCustodyKeyNotFound {
		t.Errorf("ProveSlot after revoke: got %v, want %v", err, ErrCustodyKeyNotFound)
	}
}

func TestCustodyRevokeKeyNotFound(t *testing.T) {
	cr := NewCustodyReplacer(DefaultCustodyReplacerConfig())

	err := cr.RevokeKey(999)
	if err != ErrCustodyKeyNotFound {
		t.Errorf("RevokeKey for missing key: got %v, want %v", err, ErrCustodyKeyNotFound)
	}
}

func TestCustodyMaxSlotsLimit(t *testing.T) {
	cfg := CustodyReplacerConfig{
		MaxCustodySlots: 2,
		HashFunction:    "keccak256",
		ProofSizeTarget: custodyProofSize,
	}
	cr := NewCustodyReplacer(cfg)

	cr.GenerateKeyPair(1)
	cr.GenerateKeyPair(2)

	_, err := cr.GenerateKeyPair(3)
	if err != ErrCustodyMaxSlots {
		t.Errorf("GenerateKeyPair beyond max: got %v, want %v", err, ErrCustodyMaxSlots)
	}
}

func TestCustodyGetPublicKey(t *testing.T) {
	cr := NewCustodyReplacer(DefaultCustodyReplacerConfig())
	kp, _ := cr.GenerateKeyPair(1)

	pub, err := cr.GetPublicKey(1)
	if err != nil {
		t.Fatalf("GetPublicKey: %v", err)
	}
	if len(pub) != len(kp.PublicKey) {
		t.Fatalf("pub key length mismatch")
	}
	for i := range pub {
		if pub[i] != kp.PublicKey[i] {
			t.Fatal("GetPublicKey returned different key")
		}
	}
}

func TestCustodyGetPublicKeyNotFound(t *testing.T) {
	cr := NewCustodyReplacer(DefaultCustodyReplacerConfig())
	_, err := cr.GetPublicKey(999)
	if err != ErrCustodyKeyNotFound {
		t.Errorf("GetPublicKey for missing: got %v, want %v", err, ErrCustodyKeyNotFound)
	}
}

func TestCustodyProofDeterminism(t *testing.T) {
	// Two proofs for the same slot/validator/data with the same key
	// should produce the same proof.
	cr := NewCustodyReplacer(DefaultCustodyReplacerConfig())
	cr.GenerateKeyPair(1)

	proof1, _ := cr.ProveSlot(42, 1, []byte("data"))
	proof2, _ := cr.ProveSlot(42, 1, []byte("data"))

	for i := range proof1.ProofBytes {
		if proof1.ProofBytes[i] != proof2.ProofBytes[i] {
			t.Fatal("same inputs produced different proofs")
		}
	}
}

func TestCustodyDifferentSlotsDifferentProofs(t *testing.T) {
	cr := NewCustodyReplacer(DefaultCustodyReplacerConfig())
	cr.GenerateKeyPair(1)

	proof1, _ := cr.ProveSlot(1, 1, []byte("data"))
	proof2, _ := cr.ProveSlot(2, 1, []byte("data"))

	same := true
	for i := range proof1.ProofBytes {
		if proof1.ProofBytes[i] != proof2.ProofBytes[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("different slots produced identical proofs")
	}
}

func TestCustodyConcurrentAccess(t *testing.T) {
	cr := NewCustodyReplacer(DefaultCustodyReplacerConfig())

	var wg sync.WaitGroup
	for i := uint64(0); i < 20; i++ {
		wg.Add(1)
		go func(idx uint64) {
			defer wg.Done()
			cr.GenerateKeyPair(idx)
			cr.ProveSlot(idx, idx, []byte("concurrent data"))
			cr.ActiveKeys()
		}(i)
	}
	wg.Wait()

	if cr.ActiveKeys() != 20 {
		t.Errorf("ActiveKeys() = %d after concurrent ops, want 20", cr.ActiveKeys())
	}
}

func TestCustodyConfigDefaults(t *testing.T) {
	// Zero/empty config fields get defaulted.
	cr := NewCustodyReplacer(CustodyReplacerConfig{})
	if cr.config.MaxCustodySlots != defaultMaxSlots {
		t.Errorf("MaxCustodySlots defaulted to %d, want %d", cr.config.MaxCustodySlots, defaultMaxSlots)
	}
	if cr.config.ProofSizeTarget != custodyProofSize {
		t.Errorf("ProofSizeTarget defaulted to %d, want %d", cr.config.ProofSizeTarget, custodyProofSize)
	}
	if cr.config.HashFunction != "keccak256" {
		t.Errorf("HashFunction defaulted to %s, want keccak256", cr.config.HashFunction)
	}
}
