package pqc

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

func testPQPubKey() []byte {
	return []byte("test-pq-public-key-material-32b!")
}

func testClassicPubKey() []byte {
	return []byte("classic-pub-key-material-33bytes")
}

// TestRegisterKeyBasic verifies basic key registration.
func TestRegisterKeyBasic(t *testing.T) {
	reg := NewPQKeyRegistry()

	err := reg.RegisterKey(0, testPQPubKey(), testClassicPubKey())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reg.Size() != 1 {
		t.Errorf("expected size 1, got %d", reg.Size())
	}

	entry, err := reg.GetKey(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.ValidatorIndex != 0 {
		t.Errorf("expected validator index 0, got %d", entry.ValidatorIndex)
	}
	if entry.Status != StatusActive {
		t.Errorf("expected status %q, got %q", StatusActive, entry.Status)
	}
	if entry.RegisteredAt == 0 {
		t.Error("expected non-zero registration timestamp")
	}
}

// TestRegisterKeyDuplicate verifies that re-registering fails.
func TestRegisterKeyDuplicate(t *testing.T) {
	reg := NewPQKeyRegistry()

	if err := reg.RegisterKey(42, testPQPubKey(), testClassicPubKey()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	err := reg.RegisterKey(42, testPQPubKey(), testClassicPubKey())
	if err != ErrValidatorAlreadyRegistered {
		t.Errorf("expected ErrValidatorAlreadyRegistered, got %v", err)
	}
}

// TestRegisterKeyEmptyKeys verifies that empty keys are rejected.
func TestRegisterKeyEmptyKeys(t *testing.T) {
	reg := NewPQKeyRegistry()

	err := reg.RegisterKey(0, nil, testClassicPubKey())
	if err != ErrEmptyPQPubKey {
		t.Errorf("expected ErrEmptyPQPubKey for nil PQ key, got %v", err)
	}

	err = reg.RegisterKey(0, []byte{}, testClassicPubKey())
	if err != ErrEmptyPQPubKey {
		t.Errorf("expected ErrEmptyPQPubKey for empty PQ key, got %v", err)
	}

	err = reg.RegisterKey(0, testPQPubKey(), nil)
	if err != ErrEmptyClassicPubKey {
		t.Errorf("expected ErrEmptyClassicPubKey for nil classic key, got %v", err)
	}

	err = reg.RegisterKey(0, testPQPubKey(), []byte{})
	if err != ErrEmptyClassicPubKey {
		t.Errorf("expected ErrEmptyClassicPubKey for empty classic key, got %v", err)
	}
}

// TestGetKeyNotFound verifies that lookup of non-existent validator fails.
func TestGetKeyNotFound(t *testing.T) {
	reg := NewPQKeyRegistry()

	_, err := reg.GetKey(999)
	if err != ErrValidatorNotFound {
		t.Errorf("expected ErrValidatorNotFound, got %v", err)
	}
}

// TestGetKeyReturnsCopy verifies that modifying the returned entry does
// not affect the registry's internal state.
func TestGetKeyReturnsCopy(t *testing.T) {
	reg := NewPQKeyRegistry()
	pq := testPQPubKey()

	if err := reg.RegisterKey(5, pq, testClassicPubKey()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entry, _ := reg.GetKey(5)
	// Mutate the returned entry.
	entry.PQPubKey[0] = 0xFF
	entry.Status = StatusRevoked

	// Original should be unchanged.
	original, _ := reg.GetKey(5)
	if original.PQPubKey[0] == 0xFF {
		t.Error("modifying returned entry should not affect registry")
	}
	if original.Status != StatusActive {
		t.Error("modifying returned entry status should not affect registry")
	}
}

// TestVerifyRegistration verifies the signature verification logic.
func TestVerifyRegistration(t *testing.T) {
	reg := NewPQKeyRegistry()
	classic := testClassicPubKey()

	if err := reg.RegisterKey(10, testPQPubKey(), classic); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Compute the expected signature: keccak256(classicPubKey || validatorIndex).
	sig := registrationDigest(classic, 10)
	if !reg.VerifyRegistration(10, sig) {
		t.Error("expected valid registration verification")
	}

	// Wrong signature.
	badSig := make([]byte, len(sig))
	copy(badSig, sig)
	badSig[0] ^= 0xFF
	if reg.VerifyRegistration(10, badSig) {
		t.Error("expected invalid verification with wrong signature")
	}

	// Non-existent validator.
	if reg.VerifyRegistration(999, sig) {
		t.Error("expected false for non-existent validator")
	}

	// Wrong length signature.
	if reg.VerifyRegistration(10, sig[:10]) {
		t.Error("expected false for wrong-length signature")
	}
}

// TestVerifyRegistrationRevoked verifies that revoked entries fail verification.
func TestVerifyRegistrationRevoked(t *testing.T) {
	reg := NewPQKeyRegistry()
	classic := testClassicPubKey()

	if err := reg.RegisterKey(7, testPQPubKey(), classic); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := reg.RevokeEntry(7); err != nil {
		t.Fatalf("unexpected revoke error: %v", err)
	}

	sig := registrationDigest(classic, 7)
	if reg.VerifyRegistration(7, sig) {
		t.Error("expected false for revoked entry")
	}
}

// TestMigrateKey verifies key migration.
func TestMigrateKey(t *testing.T) {
	reg := NewPQKeyRegistry()
	oldKey := testPQPubKey()
	newKey := []byte("new-pq-public-key-material-32b!!")

	if err := reg.RegisterKey(1, oldKey, testClassicPubKey()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Migrate to new key.
	if err := reg.MigrateKey(1, newKey); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entry, _ := reg.GetKey(1)
	if entry.Status != StatusMigrating {
		t.Errorf("expected status %q, got %q", StatusMigrating, entry.Status)
	}
	if !bytesEqual(entry.PQPubKey, newKey) {
		t.Error("PQ public key should be updated to new key")
	}

	// Activate after migration.
	if err := reg.ActivateEntry(1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	entry, _ = reg.GetKey(1)
	if entry.Status != StatusActive {
		t.Errorf("expected status %q after activation, got %q", StatusActive, entry.Status)
	}
}

// TestMigrateKeyErrors verifies migration error conditions.
func TestMigrateKeyErrors(t *testing.T) {
	reg := NewPQKeyRegistry()
	pq := testPQPubKey()

	// Non-existent validator.
	err := reg.MigrateKey(999, []byte("newkey"))
	if err != ErrValidatorNotFound {
		t.Errorf("expected ErrValidatorNotFound, got %v", err)
	}

	// Register and try migrating with empty key.
	if err := reg.RegisterKey(1, pq, testClassicPubKey()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	err = reg.MigrateKey(1, nil)
	if err != ErrInvalidMigrationKey {
		t.Errorf("expected ErrInvalidMigrationKey for nil key, got %v", err)
	}

	// Same key.
	err = reg.MigrateKey(1, pq)
	if err != ErrInvalidMigrationKey {
		t.Errorf("expected ErrInvalidMigrationKey for same key, got %v", err)
	}

	// Revoked entry.
	if err := reg.RevokeEntry(1); err != nil {
		t.Fatalf("unexpected revoke error: %v", err)
	}
	err = reg.MigrateKey(1, []byte("newkey"))
	if err != ErrEntryRevoked {
		t.Errorf("expected ErrEntryRevoked, got %v", err)
	}
}

// TestGetRegistryRoot verifies Merkle root computation.
func TestGetRegistryRoot(t *testing.T) {
	reg := NewPQKeyRegistry()

	// Empty registry should return the empty root hash.
	root := reg.GetRegistryRoot()
	if root != types.EmptyRootHash {
		t.Error("expected empty root hash for empty registry")
	}

	// Single entry.
	if err := reg.RegisterKey(0, testPQPubKey(), testClassicPubKey()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	root1 := reg.GetRegistryRoot()
	if root1 == types.EmptyRootHash {
		t.Error("expected non-empty root hash for non-empty registry")
	}
	if root1.IsZero() {
		t.Error("expected non-zero root hash")
	}

	// Adding a second entry should change the root.
	if err := reg.RegisterKey(1, []byte("another-pq-key!!"), []byte("another-classic-key")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	root2 := reg.GetRegistryRoot()
	if root2 == root1 {
		t.Error("root should change after adding a second entry")
	}

	// Root is deterministic: same entries produce the same root.
	root2again := reg.GetRegistryRoot()
	if root2 != root2again {
		t.Error("root should be deterministic")
	}
}

// TestGetRegistryRootDeterministic verifies that the root depends on
// entry content, not insertion order (since it's sorted by validator index).
func TestGetRegistryRootDeterministic(t *testing.T) {
	pqA := []byte("pq-key-for-validator-a-padding!!")
	pqB := []byte("pq-key-for-validator-b-padding!!")
	classic := testClassicPubKey()

	// Registry 1: insert A then B.
	reg1 := NewPQKeyRegistry()
	reg1.RegisterKey(0, pqA, classic)
	reg1.RegisterKey(1, pqB, classic)

	// Registry 2: insert B then A.
	reg2 := NewPQKeyRegistry()
	reg2.RegisterKey(1, pqB, classic)
	reg2.RegisterKey(0, pqA, classic)

	root1 := reg1.GetRegistryRoot()
	root2 := reg2.GetRegistryRoot()
	if root1 != root2 {
		t.Error("root should be the same regardless of insertion order")
	}
}

// TestRevokeEntry verifies entry revocation.
func TestRevokeEntry(t *testing.T) {
	reg := NewPQKeyRegistry()

	if err := reg.RegisterKey(3, testPQPubKey(), testClassicPubKey()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := reg.RevokeEntry(3); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entry, _ := reg.GetKey(3)
	if entry.Status != StatusRevoked {
		t.Errorf("expected status %q, got %q", StatusRevoked, entry.Status)
	}

	// Revoke non-existent.
	err := reg.RevokeEntry(999)
	if err != ErrValidatorNotFound {
		t.Errorf("expected ErrValidatorNotFound, got %v", err)
	}
}

// TestRegistrationDigest verifies the digest computation is consistent.
func TestRegistrationDigest(t *testing.T) {
	classic := testClassicPubKey()
	d1 := registrationDigest(classic, 42)
	d2 := registrationDigest(classic, 42)

	if !bytesEqual(d1, d2) {
		t.Error("registration digest should be deterministic")
	}

	// Different validator index -> different digest.
	d3 := registrationDigest(classic, 43)
	if bytesEqual(d1, d3) {
		t.Error("different validator index should produce different digest")
	}

	// Verify it matches manual keccak256 computation.
	buf := make([]byte, 8)
	buf[0] = 0
	buf[1] = 0
	buf[2] = 0
	buf[3] = 0
	buf[4] = 0
	buf[5] = 0
	buf[6] = 0
	buf[7] = 42
	expected := crypto.Keccak256(classic, buf)
	if !bytesEqual(d1, expected) {
		t.Error("digest does not match manual keccak256 computation")
	}
}
