package crypto

import (
	"bytes"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// testKeystoreConfig returns a config with low ScryptN for fast tests.
func testKeystoreConfig() KeystoreConfig {
	return KeystoreConfig{
		ScryptN: 1024,
		ScryptR: 8,
		ScryptP: 1,
		KeyDir:  "test-keystore",
	}
}

func TestKeystoreStoreAndLoadRoundtrip(t *testing.T) {
	ks := NewKeystore(testKeystoreConfig())

	// Generate a private key.
	priv, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	privBytes := make([]byte, 32)
	blob := priv.D.Bytes()
	copy(privBytes[32-len(blob):], blob)

	passphrase := "test-passphrase-123"

	ek, err := ks.StoreKey(privBytes, passphrase)
	if err != nil {
		t.Fatalf("StoreKey: %v", err)
	}

	if ek.Version != 3 {
		t.Errorf("Version = %d, want 3", ek.Version)
	}
	if ek.ID == "" {
		t.Error("ID should not be empty")
	}
	if len(ek.CipherText) != 32 {
		t.Errorf("CipherText length = %d, want 32", len(ek.CipherText))
	}
	if len(ek.IV) != 16 {
		t.Errorf("IV length = %d, want 16", len(ek.IV))
	}
	if len(ek.Salt) != 32 {
		t.Errorf("Salt length = %d, want 32", len(ek.Salt))
	}
	if len(ek.MAC) != 32 {
		t.Errorf("MAC length = %d, want 32", len(ek.MAC))
	}

	// Load and verify roundtrip.
	loaded, err := ks.LoadKey(ek.Address, passphrase)
	if err != nil {
		t.Fatalf("LoadKey: %v", err)
	}
	if !bytes.Equal(loaded, privBytes) {
		t.Error("loaded key does not match stored key")
	}
}

func TestKeystoreWrongPassphrase(t *testing.T) {
	ks := NewKeystore(testKeystoreConfig())

	priv, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	privBytes := make([]byte, 32)
	blob := priv.D.Bytes()
	copy(privBytes[32-len(blob):], blob)

	ek, err := ks.StoreKey(privBytes, "correct-password")
	if err != nil {
		t.Fatalf("StoreKey: %v", err)
	}

	// Try to load with wrong passphrase.
	_, err = ks.LoadKey(ek.Address, "wrong-password")
	if err == nil {
		t.Fatal("expected error with wrong passphrase, got nil")
	}
}

func TestKeystoreListAddresses(t *testing.T) {
	ks := NewKeystore(testKeystoreConfig())

	// Store multiple keys.
	var storedAddrs []types.Address
	for i := 0; i < 3; i++ {
		priv, err := GenerateKey()
		if err != nil {
			t.Fatalf("GenerateKey: %v", err)
		}
		privBytes := make([]byte, 32)
		blob := priv.D.Bytes()
		copy(privBytes[32-len(blob):], blob)

		ek, err := ks.StoreKey(privBytes, "pass")
		if err != nil {
			t.Fatalf("StoreKey: %v", err)
		}
		storedAddrs = append(storedAddrs, ek.Address)
	}

	addrs := ks.ListAddresses()
	if len(addrs) != 3 {
		t.Fatalf("ListAddresses returned %d, want 3", len(addrs))
	}

	// Every stored address should be in the list.
	for _, stored := range storedAddrs {
		found := false
		for _, addr := range addrs {
			if addr == stored {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("address %s not found in list", stored.Hex())
		}
	}
}

func TestKeystoreHasKey(t *testing.T) {
	ks := NewKeystore(testKeystoreConfig())

	priv, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	privBytes := make([]byte, 32)
	blob := priv.D.Bytes()
	copy(privBytes[32-len(blob):], blob)

	addr := DeriveAddress(privBytes)

	if ks.HasKey(addr) {
		t.Error("HasKey should return false before storing")
	}

	_, err = ks.StoreKey(privBytes, "pass")
	if err != nil {
		t.Fatalf("StoreKey: %v", err)
	}

	if !ks.HasKey(addr) {
		t.Error("HasKey should return true after storing")
	}
}

func TestKeystoreDeleteKey(t *testing.T) {
	ks := NewKeystore(testKeystoreConfig())

	priv, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	privBytes := make([]byte, 32)
	blob := priv.D.Bytes()
	copy(privBytes[32-len(blob):], blob)

	ek, err := ks.StoreKey(privBytes, "pass")
	if err != nil {
		t.Fatalf("StoreKey: %v", err)
	}

	if !ks.HasKey(ek.Address) {
		t.Error("expected key to exist")
	}

	if err := ks.DeleteKey(ek.Address); err != nil {
		t.Fatalf("DeleteKey: %v", err)
	}

	if ks.HasKey(ek.Address) {
		t.Error("expected key to be deleted")
	}

	// Deleting a non-existent key should return an error.
	if err := ks.DeleteKey(ek.Address); err == nil {
		t.Error("expected error deleting non-existent key")
	}
}

func TestKeystoreChangePassphrase(t *testing.T) {
	ks := NewKeystore(testKeystoreConfig())

	priv, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	privBytes := make([]byte, 32)
	blob := priv.D.Bytes()
	copy(privBytes[32-len(blob):], blob)

	ek, err := ks.StoreKey(privBytes, "old-pass")
	if err != nil {
		t.Fatalf("StoreKey: %v", err)
	}

	// Change passphrase.
	if err := ks.ChangePassphrase(ek.Address, "old-pass", "new-pass"); err != nil {
		t.Fatalf("ChangePassphrase: %v", err)
	}

	// Old passphrase should fail.
	_, err = ks.LoadKey(ek.Address, "old-pass")
	if err == nil {
		t.Fatal("expected old passphrase to fail after change")
	}

	// New passphrase should work.
	loaded, err := ks.LoadKey(ek.Address, "new-pass")
	if err != nil {
		t.Fatalf("LoadKey with new pass: %v", err)
	}
	if !bytes.Equal(loaded, privBytes) {
		t.Error("loaded key does not match original after passphrase change")
	}
}

func TestKeystoreChangePassphraseWrongOld(t *testing.T) {
	ks := NewKeystore(testKeystoreConfig())

	priv, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	privBytes := make([]byte, 32)
	blob := priv.D.Bytes()
	copy(privBytes[32-len(blob):], blob)

	ek, err := ks.StoreKey(privBytes, "correct-pass")
	if err != nil {
		t.Fatalf("StoreKey: %v", err)
	}

	// Attempt change with wrong old passphrase.
	err = ks.ChangePassphrase(ek.Address, "wrong-pass", "new-pass")
	if err == nil {
		t.Fatal("expected error with wrong old passphrase")
	}

	// Original passphrase should still work (key not deleted on failure).
	loaded, err := ks.LoadKey(ek.Address, "correct-pass")
	if err != nil {
		t.Fatalf("LoadKey after failed change: %v", err)
	}
	if !bytes.Equal(loaded, privBytes) {
		t.Error("key should remain unchanged after failed passphrase change")
	}
}

func TestDeriveAddress(t *testing.T) {
	priv, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	privBytes := make([]byte, 32)
	blob := priv.D.Bytes()
	copy(privBytes[32-len(blob):], blob)

	addr := DeriveAddress(privBytes)
	expected := PubkeyToAddress(priv.PublicKey)

	if addr != expected {
		t.Errorf("DeriveAddress = %s, want %s", addr.Hex(), expected.Hex())
	}
}

func TestDeriveAddressInvalidKey(t *testing.T) {
	// Invalid key length should return zero address.
	addr := DeriveAddress([]byte{1, 2, 3})
	if addr != (types.Address{}) {
		t.Error("DeriveAddress with invalid key should return zero address")
	}
}

func TestKeystoreStoreInvalidKey(t *testing.T) {
	ks := NewKeystore(testKeystoreConfig())

	// Wrong-length key.
	_, err := ks.StoreKey([]byte{1, 2, 3}, "pass")
	if err == nil {
		t.Error("expected error storing invalid-length key")
	}
}

func TestKeystoreLoadNonExistentKey(t *testing.T) {
	ks := NewKeystore(testKeystoreConfig())

	_, err := ks.LoadKey(types.Address{}, "pass")
	if err == nil {
		t.Error("expected error loading non-existent key")
	}
}

func TestKeystoreDefaultConfig(t *testing.T) {
	cfg := DefaultKeystoreConfig()
	if cfg.ScryptN != 262144 {
		t.Errorf("ScryptN = %d, want 262144", cfg.ScryptN)
	}
	if cfg.ScryptR != 8 {
		t.Errorf("ScryptR = %d, want 8", cfg.ScryptR)
	}
	if cfg.ScryptP != 1 {
		t.Errorf("ScryptP = %d, want 1", cfg.ScryptP)
	}
	if cfg.KeyDir != "keystore" {
		t.Errorf("KeyDir = %q, want %q", cfg.KeyDir, "keystore")
	}
}

func TestNewKeystoreDefaultsZeroConfig(t *testing.T) {
	ks := NewKeystore(KeystoreConfig{})
	if ks.config.ScryptN != 262144 {
		t.Errorf("ScryptN = %d, want 262144", ks.config.ScryptN)
	}
	if ks.config.ScryptR != 8 {
		t.Errorf("ScryptR = %d, want 8", ks.config.ScryptR)
	}
	if ks.config.ScryptP != 1 {
		t.Errorf("ScryptP = %d, want 1", ks.config.ScryptP)
	}
	if ks.config.KeyDir != "keystore" {
		t.Errorf("KeyDir = %q, want %q", ks.config.KeyDir, "keystore")
	}
}

func TestEncryptedKeyUUID(t *testing.T) {
	ks := NewKeystore(testKeystoreConfig())

	priv, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	privBytes := make([]byte, 32)
	blob := priv.D.Bytes()
	copy(privBytes[32-len(blob):], blob)

	ek, err := ks.StoreKey(privBytes, "pass")
	if err != nil {
		t.Fatalf("StoreKey: %v", err)
	}

	// UUID should be well-formed (8-4-4-4-12).
	if len(ek.ID) != 36 {
		t.Errorf("UUID length = %d, want 36", len(ek.ID))
	}
	if ek.ID[8] != '-' || ek.ID[13] != '-' || ek.ID[18] != '-' || ek.ID[23] != '-' {
		t.Errorf("UUID format invalid: %s", ek.ID)
	}
}
