package pqc

import (
	"sync"
	"testing"
)

func TestCustodyReplacerV2New(t *testing.T) {
	cr := NewCustodyReplacerV2(DefaultCustodyReplacerV2Config())
	if cr == nil {
		t.Fatal("NewCustodyReplacerV2 returned nil")
	}
	if cr.ActiveKeys() != 0 {
		t.Errorf("ActiveKeys() = %d, want 0", cr.ActiveKeys())
	}
}

func TestCustodyReplacerV2DefaultConfig(t *testing.T) {
	cfg := DefaultCustodyReplacerV2Config()
	if cfg.SecurityLevel != 128 {
		t.Errorf("SecurityLevel = %d, want 128", cfg.SecurityLevel)
	}
	if cfg.HashFunction != "keccak256" {
		t.Errorf("HashFunction = %s, want keccak256", cfg.HashFunction)
	}
	if cfg.KeyRotationEpoch != 256 {
		t.Errorf("KeyRotationEpoch = %d, want 256", cfg.KeyRotationEpoch)
	}
	if cfg.ProofSizeLimit != crv2DefaultLimit {
		t.Errorf("ProofSizeLimit = %d, want %d", cfg.ProofSizeLimit, crv2DefaultLimit)
	}
}

func TestCustodyReplacerV2ConfigDefaults(t *testing.T) {
	// Zero/empty config fields get defaulted.
	cr := NewCustodyReplacerV2(CustodyReplacerV2Config{})
	if cr.config.ProofSizeLimit != crv2DefaultLimit {
		t.Errorf("ProofSizeLimit defaulted to %d, want %d", cr.config.ProofSizeLimit, crv2DefaultLimit)
	}
	if cr.config.KeyRotationEpoch != 256 {
		t.Errorf("KeyRotationEpoch defaulted to %d, want 256", cr.config.KeyRotationEpoch)
	}
	if cr.config.HashFunction != "keccak256" {
		t.Errorf("HashFunction defaulted to %s, want keccak256", cr.config.HashFunction)
	}
	if cr.config.SecurityLevel != 128 {
		t.Errorf("SecurityLevel defaulted to %d, want 128", cr.config.SecurityLevel)
	}
}

func TestCustodyV2GenerateKey(t *testing.T) {
	cr := NewCustodyReplacerV2(DefaultCustodyReplacerV2Config())

	key, err := cr.GenerateKey(100)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if len(key.PublicKey) != 32 {
		t.Errorf("PublicKey len = %d, want 32", len(key.PublicKey))
	}
	if len(key.PrivateKey) != crv2KeySize {
		t.Errorf("PrivateKey len = %d, want %d", len(key.PrivateKey), crv2KeySize)
	}
	if key.CreatedEpoch != 100 {
		t.Errorf("CreatedEpoch = %d, want 100", key.CreatedEpoch)
	}
	if key.Expired {
		t.Error("new key should not be expired")
	}
	if key.KeyID == "" {
		t.Error("KeyID is empty")
	}
	if cr.ActiveKeys() != 1 {
		t.Errorf("ActiveKeys() = %d, want 1", cr.ActiveKeys())
	}
}

func TestCustodyV2GenerateMultipleKeys(t *testing.T) {
	cr := NewCustodyReplacerV2(DefaultCustodyReplacerV2Config())

	for i := uint64(0); i < 5; i++ {
		_, err := cr.GenerateKey(i)
		if err != nil {
			t.Fatalf("GenerateKey(%d): %v", i, err)
		}
	}
	if cr.ActiveKeys() != 5 {
		t.Errorf("ActiveKeys() = %d, want 5", cr.ActiveKeys())
	}
}

func TestCustodyV2GenerateKeyUniqueness(t *testing.T) {
	cr := NewCustodyReplacerV2(DefaultCustodyReplacerV2Config())

	k1, _ := cr.GenerateKey(1)
	k2, _ := cr.GenerateKey(1)

	// Keys should have different IDs (random generation).
	if k1.KeyID == k2.KeyID {
		t.Error("two keys generated at same epoch have same ID (extremely unlikely)")
	}
	// Public keys should differ.
	same := true
	for i := range k1.PublicKey {
		if k1.PublicKey[i] != k2.PublicKey[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("two keys have same public key (extremely unlikely)")
	}
}

func TestCustodyV2CreateProof(t *testing.T) {
	cr := NewCustodyReplacerV2(DefaultCustodyReplacerV2Config())
	key, _ := cr.GenerateKey(10)

	proof, err := cr.CreateProof(key.KeyID, []byte("test data"))
	if err != nil {
		t.Fatalf("CreateProof: %v", err)
	}
	if proof.ProverID != key.KeyID {
		t.Errorf("ProverID = %s, want %s", proof.ProverID, key.KeyID)
	}
	if len(proof.ProofBytes) != crv2ProofSize {
		t.Errorf("ProofBytes len = %d, want %d", len(proof.ProofBytes), crv2ProofSize)
	}
	if len(proof.DataCommitment) != 32 {
		t.Errorf("DataCommitment len = %d, want 32", len(proof.DataCommitment))
	}
	if proof.Epoch != 10 {
		t.Errorf("Epoch = %d, want 10", proof.Epoch)
	}
	if proof.Timestamp == 0 {
		t.Error("Timestamp is zero")
	}
}

func TestCustodyV2CreateProofKeyNotFound(t *testing.T) {
	cr := NewCustodyReplacerV2(DefaultCustodyReplacerV2Config())

	_, err := cr.CreateProof("nonexistent", []byte("data"))
	if err != ErrCustodyKeyNotFound2 {
		t.Errorf("CreateProof with missing key: got %v, want %v", err, ErrCustodyKeyNotFound2)
	}
}

func TestCustodyV2CreateProofExpiredKey(t *testing.T) {
	cr := NewCustodyReplacerV2(DefaultCustodyReplacerV2Config())
	key, _ := cr.GenerateKey(0)

	// Manually expire the key.
	cr.mu.Lock()
	cr.keys[key.KeyID].Expired = true
	cr.mu.Unlock()

	_, err := cr.CreateProof(key.KeyID, []byte("data"))
	if err != ErrCustodyKeyExpired {
		t.Errorf("CreateProof with expired key: got %v, want %v", err, ErrCustodyKeyExpired)
	}
}

func TestCustodyV2VerifyProof(t *testing.T) {
	cr := NewCustodyReplacerV2(DefaultCustodyReplacerV2Config())
	key, _ := cr.GenerateKey(42)

	proof, err := cr.CreateProof(key.KeyID, []byte("custody data"))
	if err != nil {
		t.Fatalf("CreateProof: %v", err)
	}

	valid, err := cr.VerifyProof(proof, key.PublicKey)
	if err != nil {
		t.Fatalf("VerifyProof: %v", err)
	}
	if !valid {
		t.Error("VerifyProof returned false for valid proof")
	}
}

func TestCustodyV2VerifyProofWrongPubKey(t *testing.T) {
	cr := NewCustodyReplacerV2(DefaultCustodyReplacerV2Config())
	key, _ := cr.GenerateKey(1)

	proof, _ := cr.CreateProof(key.KeyID, []byte("data"))

	wrongKey := make([]byte, 32)
	wrongKey[0] = 0xff
	valid, err := cr.VerifyProof(proof, wrongKey)
	if valid {
		t.Error("VerifyProof should fail with wrong public key")
	}
	if err != ErrCustodyProofInvalid2 {
		t.Errorf("expected ErrCustodyProofInvalid2, got %v", err)
	}
}

func TestCustodyV2VerifyProofNilInputs(t *testing.T) {
	cr := NewCustodyReplacerV2(DefaultCustodyReplacerV2Config())

	// Nil proof.
	valid, err := cr.VerifyProof(nil, []byte("key"))
	if valid || err != ErrCustodyProofInvalid2 {
		t.Error("should reject nil proof")
	}

	// Empty pub key.
	valid, err = cr.VerifyProof(&PQCustodyProof{ProofBytes: make([]byte, crv2ProofSize)}, nil)
	if valid || err != ErrCustodyProofInvalid2 {
		t.Error("should reject empty pub key")
	}

	// Wrong proof size.
	valid, err = cr.VerifyProof(&PQCustodyProof{
		ProofBytes:     []byte("short"),
		DataCommitment: []byte("c"),
	}, []byte("key"))
	if valid || err != ErrCustodyProofInvalid2 {
		t.Error("should reject wrong proof size")
	}

	// Empty commitment.
	valid, err = cr.VerifyProof(&PQCustodyProof{
		ProofBytes: make([]byte, crv2ProofSize),
	}, []byte("key"))
	if valid || err != ErrCustodyProofInvalid2 {
		t.Error("should reject empty commitment")
	}
}

func TestCustodyV2VerifyProofTooLarge(t *testing.T) {
	cfg := CustodyReplacerV2Config{
		ProofSizeLimit: 10, // very small limit
	}
	cr := NewCustodyReplacerV2(cfg)

	proof := &PQCustodyProof{
		ProofBytes:     make([]byte, 64),
		DataCommitment: []byte("commit"),
	}
	valid, err := cr.VerifyProof(proof, []byte("key"))
	if valid {
		t.Error("should reject oversized proof")
	}
	if err != ErrCustodyProofTooLarge {
		t.Errorf("expected ErrCustodyProofTooLarge, got %v", err)
	}
}

func TestCustodyV2RotateKeys(t *testing.T) {
	cfg := DefaultCustodyReplacerV2Config()
	cfg.KeyRotationEpoch = 10
	cr := NewCustodyReplacerV2(cfg)

	// Create keys at different epochs.
	cr.GenerateKey(0)
	cr.GenerateKey(5)
	cr.GenerateKey(15)
	cr.GenerateKey(20)

	if cr.ActiveKeys() != 4 {
		t.Fatalf("ActiveKeys() = %d, want 4", cr.ActiveKeys())
	}

	// Rotate at epoch 20: threshold = 20-10 = 10.
	// Keys at epoch 0 and 5 should expire (< 10).
	expired, err := cr.RotateKeys(20)
	if err != nil {
		t.Fatalf("RotateKeys: %v", err)
	}
	if expired != 2 {
		t.Errorf("RotateKeys expired %d, want 2", expired)
	}
	if cr.ActiveKeys() != 2 {
		t.Errorf("ActiveKeys() after rotation = %d, want 2", cr.ActiveKeys())
	}
}

func TestCustodyV2RotateKeysNoExpiry(t *testing.T) {
	cfg := DefaultCustodyReplacerV2Config()
	cfg.KeyRotationEpoch = 100
	cr := NewCustodyReplacerV2(cfg)

	cr.GenerateKey(50)
	cr.GenerateKey(60)

	// Rotate at epoch 80: threshold = 80-100 = 0 (underflow handled).
	// No keys should expire.
	expired, err := cr.RotateKeys(80)
	if err != nil {
		t.Fatalf("RotateKeys: %v", err)
	}
	if expired != 0 {
		t.Errorf("RotateKeys expired %d, want 0", expired)
	}
	if cr.ActiveKeys() != 2 {
		t.Errorf("ActiveKeys() = %d, want 2", cr.ActiveKeys())
	}
}

func TestCustodyV2RotateKeysIdempotent(t *testing.T) {
	cfg := DefaultCustodyReplacerV2Config()
	cfg.KeyRotationEpoch = 5
	cr := NewCustodyReplacerV2(cfg)

	cr.GenerateKey(0)
	cr.GenerateKey(1)

	// First rotation at epoch 10: threshold=5, expires keys at 0 and 1.
	expired1, _ := cr.RotateKeys(10)
	if expired1 != 2 {
		t.Errorf("first rotation expired %d, want 2", expired1)
	}

	// Second rotation at same epoch: nothing more to expire.
	expired2, _ := cr.RotateKeys(10)
	if expired2 != 0 {
		t.Errorf("second rotation expired %d, want 0", expired2)
	}
}

func TestCustodyV2GetKey(t *testing.T) {
	cr := NewCustodyReplacerV2(DefaultCustodyReplacerV2Config())
	key, _ := cr.GenerateKey(1)

	retrieved, err := cr.GetKey(key.KeyID)
	if err != nil {
		t.Fatalf("GetKey: %v", err)
	}
	if retrieved.KeyID != key.KeyID {
		t.Errorf("KeyID = %s, want %s", retrieved.KeyID, key.KeyID)
	}
	if len(retrieved.PublicKey) != len(key.PublicKey) {
		t.Fatal("pub key length mismatch")
	}
	for i := range retrieved.PublicKey {
		if retrieved.PublicKey[i] != key.PublicKey[i] {
			t.Fatal("GetKey returned different public key")
		}
	}
}

func TestCustodyV2GetKeyNotFound(t *testing.T) {
	cr := NewCustodyReplacerV2(DefaultCustodyReplacerV2Config())
	_, err := cr.GetKey("nonexistent")
	if err != ErrCustodyKeyNotFound2 {
		t.Errorf("GetKey for missing: got %v, want %v", err, ErrCustodyKeyNotFound2)
	}
}

func TestCustodyV2ProofDeterminism(t *testing.T) {
	cr := NewCustodyReplacerV2(DefaultCustodyReplacerV2Config())
	key, _ := cr.GenerateKey(1)

	proof1, _ := cr.CreateProof(key.KeyID, []byte("data"))
	proof2, _ := cr.CreateProof(key.KeyID, []byte("data"))

	for i := range proof1.ProofBytes {
		if proof1.ProofBytes[i] != proof2.ProofBytes[i] {
			t.Fatal("same inputs produced different proofs")
		}
	}
}

func TestCustodyV2DifferentDataDifferentProofs(t *testing.T) {
	cr := NewCustodyReplacerV2(DefaultCustodyReplacerV2Config())
	key, _ := cr.GenerateKey(1)

	proof1, _ := cr.CreateProof(key.KeyID, []byte("data1"))
	proof2, _ := cr.CreateProof(key.KeyID, []byte("data2"))

	same := true
	for i := range proof1.ProofBytes {
		if proof1.ProofBytes[i] != proof2.ProofBytes[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("different data produced identical proofs")
	}
}

func TestCustodyV2RotateInvalidatesProofs(t *testing.T) {
	cfg := DefaultCustodyReplacerV2Config()
	cfg.KeyRotationEpoch = 5
	cr := NewCustodyReplacerV2(cfg)

	key, _ := cr.GenerateKey(0)
	cr.CreateProof(key.KeyID, []byte("old data"))

	// Rotate to expire the key.
	expired, _ := cr.RotateKeys(100)
	if expired != 1 {
		t.Errorf("expected 1 key expired, got %d", expired)
	}

	// Creating proof with expired key should fail.
	_, err := cr.CreateProof(key.KeyID, []byte("new data"))
	if err != ErrCustodyKeyExpired {
		t.Errorf("CreateProof after rotation: got %v, want %v", err, ErrCustodyKeyExpired)
	}
}

func TestCustodyV2ConcurrentAccess(t *testing.T) {
	cr := NewCustodyReplacerV2(DefaultCustodyReplacerV2Config())

	var wg sync.WaitGroup
	keys := make(chan *PQCustodyKey, 20)

	// Concurrent key generation.
	for i := uint64(0); i < 20; i++ {
		wg.Add(1)
		go func(epoch uint64) {
			defer wg.Done()
			key, err := cr.GenerateKey(epoch)
			if err != nil {
				return
			}
			keys <- key
		}(i)
	}
	wg.Wait()
	close(keys)

	// Collect keys.
	var allKeys []*PQCustodyKey
	for key := range keys {
		allKeys = append(allKeys, key)
	}

	if cr.ActiveKeys() != 20 {
		t.Errorf("ActiveKeys() = %d after concurrent ops, want 20", cr.ActiveKeys())
	}

	// Concurrent proof creation and verification.
	var wg2 sync.WaitGroup
	for _, key := range allKeys {
		wg2.Add(1)
		go func(k *PQCustodyKey) {
			defer wg2.Done()
			proof, err := cr.CreateProof(k.KeyID, []byte("concurrent data"))
			if err != nil {
				return
			}
			cr.VerifyProof(proof, k.PublicKey)
			cr.ActiveKeys()
		}(key)
	}
	wg2.Wait()
}

func TestCustodyV2ProofVerificationRoundtrip(t *testing.T) {
	cr := NewCustodyReplacerV2(DefaultCustodyReplacerV2Config())

	// Generate multiple keys and verify proofs for each.
	for i := uint64(0); i < 5; i++ {
		key, _ := cr.GenerateKey(i)
		data := []byte{byte(i), 'r', 'o', 'u', 'n', 'd'}
		proof, err := cr.CreateProof(key.KeyID, data)
		if err != nil {
			t.Fatalf("CreateProof(%d): %v", i, err)
		}
		valid, err := cr.VerifyProof(proof, key.PublicKey)
		if err != nil || !valid {
			t.Fatalf("VerifyProof(%d): valid=%v err=%v", i, valid, err)
		}
	}
}
