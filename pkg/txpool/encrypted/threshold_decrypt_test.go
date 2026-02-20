package encrypted

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"io"
	"sync"
	"testing"

	ethcrypto "github.com/eth2028/eth2028/crypto"
)

// helper: create a decryption share for testing.
func makeShare(validatorIdx int, data []byte, epoch uint64) *DecryptionShare {
	return &DecryptionShare{
		ValidatorIndex: validatorIdx,
		ShareBytes:     data,
		Epoch:          epoch,
	}
}

// helper: encrypt with AES-GCM using a known key and nonce.
func testEncrypt(key, plaintext []byte) (ciphertext, nonce []byte) {
	block, _ := aes.NewCipher(key)
	aesGCM, _ := cipher.NewGCM(block)
	nonce = make([]byte, aesGCM.NonceSize())
	io.ReadFull(rand.Reader, nonce)
	ciphertext = aesGCM.Seal(nil, nonce, plaintext, nil)
	return
}

func TestNewThresholdDecryptorValid(t *testing.T) {
	td, err := NewThresholdDecryptor(2, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if td.Threshold() != 2 {
		t.Errorf("Threshold() = %d, want 2", td.Threshold())
	}
	if td.TotalParts() != 3 {
		t.Errorf("TotalParts() = %d, want 3", td.TotalParts())
	}
}

func TestNewThresholdDecryptorInvalid(t *testing.T) {
	tests := []struct {
		t, n int
	}{
		{0, 3},
		{-1, 3},
		{4, 3},
		{1, 0},
	}
	for _, tc := range tests {
		_, err := NewThresholdDecryptor(tc.t, tc.n)
		if err != ErrThresholdInvalid {
			t.Errorf("NewThresholdDecryptor(%d, %d): got %v, want ErrThresholdInvalid", tc.t, tc.n, err)
		}
	}
}

func TestAddShareBasic(t *testing.T) {
	td, _ := NewThresholdDecryptor(2, 3)
	td.SetEpoch(1)

	met, err := td.AddShare(makeShare(0, []byte{0x01, 0x02}, 1))
	if err != nil {
		t.Fatalf("AddShare: %v", err)
	}
	if met {
		t.Error("threshold should not be met with 1 share")
	}
	if td.PendingShareCount() != 1 {
		t.Errorf("PendingShareCount() = %d, want 1", td.PendingShareCount())
	}
}

func TestAddShareThresholdReached(t *testing.T) {
	td, _ := NewThresholdDecryptor(2, 3)
	td.SetEpoch(5)

	td.AddShare(makeShare(0, []byte{0x01}, 5))
	met, err := td.AddShare(makeShare(1, []byte{0x02}, 5))
	if err != nil {
		t.Fatalf("AddShare: %v", err)
	}
	if !met {
		t.Error("threshold should be met with 2 shares (t=2)")
	}
	if !td.ThresholdMet() {
		t.Error("ThresholdMet() should return true")
	}
}

func TestAddShareDuplicate(t *testing.T) {
	td, _ := NewThresholdDecryptor(2, 3)
	td.SetEpoch(1)

	td.AddShare(makeShare(0, []byte{0x01}, 1))
	_, err := td.AddShare(makeShare(0, []byte{0x02}, 1))
	if err != ErrShareAlreadyAdded {
		t.Errorf("got %v, want ErrShareAlreadyAdded", err)
	}
}

func TestAddShareNil(t *testing.T) {
	td, _ := NewThresholdDecryptor(2, 3)

	_, err := td.AddShare(nil)
	if err != ErrInvalidShareData {
		t.Errorf("AddShare(nil): got %v, want ErrInvalidShareData", err)
	}

	_, err = td.AddShare(&DecryptionShare{ValidatorIndex: 0, ShareBytes: nil})
	if err != ErrInvalidShareData {
		t.Errorf("AddShare(empty): got %v, want ErrInvalidShareData", err)
	}
}

func TestAddShareEpochMismatch(t *testing.T) {
	td, _ := NewThresholdDecryptor(2, 3)
	td.SetEpoch(5)

	_, err := td.AddShare(makeShare(0, []byte{0x01}, 3))
	if err != ErrEpochMismatch {
		t.Errorf("got %v, want ErrEpochMismatch", err)
	}
}

func TestAddShareInvalidValidatorIndex(t *testing.T) {
	td, _ := NewThresholdDecryptor(2, 3)
	td.SetEpoch(1)

	_, err := td.AddShare(makeShare(-1, []byte{0x01}, 1))
	if err != ErrInvalidValidatorIdx {
		t.Errorf("got %v, want ErrInvalidValidatorIdx", err)
	}
}

func TestTryDecryptSuccess(t *testing.T) {
	td, _ := NewThresholdDecryptor(2, 3)
	td.SetEpoch(1)

	// Build shares and compute the expected key.
	share1 := makeShare(0, []byte{0xAA, 0xBB, 0xCC}, 1)
	share2 := makeShare(1, []byte{0x11, 0x22, 0x33}, 1)

	shares := []DecryptionShare{*share1, *share2}
	key := ComputeDecryptionKey(shares)

	// Encrypt test data with the expected key.
	plaintext := []byte("secret transaction data")
	ciphertext, nonce := testEncrypt(key, plaintext)

	td.SetCiphertext(ciphertext, nonce)
	td.AddShare(share1)
	td.AddShare(share2)

	result, err := td.TryDecrypt()
	if err != nil {
		t.Fatalf("TryDecrypt: %v", err)
	}
	if string(result) != string(plaintext) {
		t.Errorf("decrypted = %q, want %q", result, plaintext)
	}
}

func TestTryDecryptInsufficientShares(t *testing.T) {
	td, _ := NewThresholdDecryptor(3, 5)
	td.SetEpoch(1)
	td.AddShare(makeShare(0, []byte{0x01}, 1))

	_, err := td.TryDecrypt()
	if err != ErrThresholdNotMet {
		t.Errorf("got %v, want ErrThresholdNotMet", err)
	}
}

func TestTryDecryptNoCiphertext(t *testing.T) {
	td, _ := NewThresholdDecryptor(1, 1)
	td.SetEpoch(1)
	td.AddShare(makeShare(0, []byte{0x01}, 1))

	_, err := td.TryDecrypt()
	if err != ErrNoCiphertext {
		t.Errorf("got %v, want ErrNoCiphertext", err)
	}
}

func TestVerifyShareValid(t *testing.T) {
	share := makeShare(5, []byte{0xDE, 0xAD, 0xBE, 0xEF}, 1)
	commitment, err := MakeCommitment(share)
	if err != nil {
		t.Fatalf("MakeCommitment: %v", err)
	}

	ok, err := VerifyShare(share, commitment)
	if err != nil {
		t.Fatalf("VerifyShare: %v", err)
	}
	if !ok {
		t.Error("VerifyShare should return true for valid commitment")
	}
}

func TestVerifyShareInvalid(t *testing.T) {
	share := makeShare(5, []byte{0xDE, 0xAD}, 1)
	wrongCommitment := ethcrypto.Keccak256([]byte("wrong data"))

	ok, err := VerifyShare(share, wrongCommitment)
	if err != nil {
		t.Fatalf("VerifyShare: %v", err)
	}
	if ok {
		t.Error("VerifyShare should return false for invalid commitment")
	}
}

func TestVerifyShareNilInputs(t *testing.T) {
	_, err := VerifyShare(nil, []byte{0x01})
	if err != ErrInvalidShareData {
		t.Errorf("VerifyShare(nil): got %v, want ErrInvalidShareData", err)
	}

	share := makeShare(0, []byte{0x01}, 1)
	_, err = VerifyShare(share, nil)
	if err != ErrInvalidCommitment {
		t.Errorf("VerifyShare(nil commitment): got %v, want ErrInvalidCommitment", err)
	}
}

func TestResetEpoch(t *testing.T) {
	td, _ := NewThresholdDecryptor(2, 3)
	td.SetEpoch(1)
	td.AddShare(makeShare(0, []byte{0x01}, 1))
	td.SetCiphertext([]byte("data"), []byte("nonce"))

	td.ResetEpoch(2)

	if td.PendingShareCount() != 0 {
		t.Errorf("PendingShareCount after reset = %d, want 0", td.PendingShareCount())
	}
	if td.CurrentEpoch() != 2 {
		t.Errorf("CurrentEpoch after reset = %d, want 2", td.CurrentEpoch())
	}
	if td.ThresholdMet() {
		t.Error("ThresholdMet should be false after reset")
	}
}

func TestComputeDecryptionKeyDeterministic(t *testing.T) {
	shares := []DecryptionShare{
		{ValidatorIndex: 0, ShareBytes: []byte{0xAA, 0xBB}},
		{ValidatorIndex: 1, ShareBytes: []byte{0xCC, 0xDD}},
	}

	key1 := ComputeDecryptionKey(shares)
	key2 := ComputeDecryptionKey(shares)

	if len(key1) != 32 {
		t.Fatalf("key length = %d, want 32", len(key1))
	}
	for i := range key1 {
		if key1[i] != key2[i] {
			t.Fatal("ComputeDecryptionKey is not deterministic")
		}
	}
}

func TestComputeDecryptionKeyEmpty(t *testing.T) {
	key := ComputeDecryptionKey(nil)
	if len(key) != 32 {
		t.Fatalf("key length = %d, want 32", len(key))
	}
}

func TestComputeDecryptionKeyDifferentShares(t *testing.T) {
	key1 := ComputeDecryptionKey([]DecryptionShare{
		{ValidatorIndex: 0, ShareBytes: []byte{0x01}},
	})
	key2 := ComputeDecryptionKey([]DecryptionShare{
		{ValidatorIndex: 0, ShareBytes: []byte{0x02}},
	})

	equal := true
	for i := range key1 {
		if key1[i] != key2[i] {
			equal = false
			break
		}
	}
	if equal {
		t.Error("different shares should produce different keys")
	}
}

func TestSharesForValidators(t *testing.T) {
	td, _ := NewThresholdDecryptor(3, 5)
	td.SetEpoch(1)

	td.AddShare(makeShare(2, []byte{0x01}, 1))
	td.AddShare(makeShare(4, []byte{0x02}, 1))

	validators := td.SharesForValidators()
	if len(validators) != 2 {
		t.Fatalf("SharesForValidators len = %d, want 2", len(validators))
	}

	found := map[int]bool{}
	for _, v := range validators {
		found[v] = true
	}
	if !found[2] || !found[4] {
		t.Errorf("expected validators 2 and 4, got %v", validators)
	}
}

func TestRemainingShares(t *testing.T) {
	td, _ := NewThresholdDecryptor(3, 5)
	td.SetEpoch(1)

	if td.RemainingShares() != 3 {
		t.Errorf("RemainingShares() = %d, want 3", td.RemainingShares())
	}

	td.AddShare(makeShare(0, []byte{0x01}, 1))
	if td.RemainingShares() != 2 {
		t.Errorf("RemainingShares() = %d, want 2", td.RemainingShares())
	}

	td.AddShare(makeShare(1, []byte{0x02}, 1))
	td.AddShare(makeShare(2, []byte{0x03}, 1))
	if td.RemainingShares() != 0 {
		t.Errorf("RemainingShares() = %d, want 0", td.RemainingShares())
	}

	// Extra shares beyond threshold.
	td.AddShare(makeShare(3, []byte{0x04}, 1))
	if td.RemainingShares() != 0 {
		t.Errorf("RemainingShares() after extra = %d, want 0", td.RemainingShares())
	}
}

func TestConcurrentAddShare(t *testing.T) {
	td, _ := NewThresholdDecryptor(50, 100)
	td.SetEpoch(1)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			td.AddShare(makeShare(idx, []byte{byte(idx)}, 1))
		}(i)
	}
	wg.Wait()

	if td.PendingShareCount() != 100 {
		t.Errorf("PendingShareCount() = %d, want 100", td.PendingShareCount())
	}
	if !td.ThresholdMet() {
		t.Error("ThresholdMet should be true after 100 shares (t=50)")
	}
}

func TestThresholdOneOfOne(t *testing.T) {
	td, _ := NewThresholdDecryptor(1, 1)
	td.SetEpoch(0)

	shareData := []byte{0xFF, 0xEE, 0xDD}
	share := makeShare(0, shareData, 0)

	key := ComputeDecryptionKey([]DecryptionShare{*share})
	plaintext := []byte("1-of-1 secret")
	ct, nonce := testEncrypt(key, plaintext)

	td.SetCiphertext(ct, nonce)
	met, _ := td.AddShare(share)
	if !met {
		t.Fatal("threshold should be met with 1-of-1")
	}

	result, err := td.TryDecrypt()
	if err != nil {
		t.Fatalf("TryDecrypt: %v", err)
	}
	if string(result) != string(plaintext) {
		t.Errorf("decrypted = %q, want %q", result, plaintext)
	}
}

func TestMakeCommitmentNilShare(t *testing.T) {
	_, err := MakeCommitment(nil)
	if err != ErrInvalidShareData {
		t.Errorf("MakeCommitment(nil): got %v, want ErrInvalidShareData", err)
	}

	_, err = MakeCommitment(&DecryptionShare{ValidatorIndex: 0})
	if err != ErrInvalidShareData {
		t.Errorf("MakeCommitment(empty): got %v, want ErrInvalidShareData", err)
	}
}

func TestResetEpochClearsCiphertext(t *testing.T) {
	td, _ := NewThresholdDecryptor(1, 1)
	td.SetEpoch(1)
	td.SetCiphertext([]byte("data"), []byte("nonce123nonce"))
	td.AddShare(makeShare(0, []byte{0x01}, 1))

	td.ResetEpoch(2)

	// After reset, TryDecrypt should fail because ciphertext is cleared.
	td.AddShare(makeShare(0, []byte{0x01}, 2))
	_, err := td.TryDecrypt()
	if err != ErrNoCiphertext {
		t.Errorf("TryDecrypt after reset: got %v, want ErrNoCiphertext", err)
	}
}
