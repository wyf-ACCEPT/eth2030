package pqc

import (
	"math/big"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// helper: create a sample legacy transaction for testing.
func testLegacyTx() *types.Transaction {
	to := types.HexToAddress("0xdead")
	return types.NewTransaction(&types.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(1_000_000_000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(1_000_000),
		Data:     []byte("hello pq"),
	})
}

// --- PQSignatureType tests ---

func TestSignatureSizeForType(t *testing.T) {
	tests := []struct {
		st   PQSignatureType
		want int
		err  bool
	}{
		{SigDilithium3, Dilithium3SignatureSize, false},
		{SigFalcon512, Falcon512SignatureSize, false},
		{SigSPHINCS128, SPHINCS128SignatureSize, false},
		{PQSignatureType(99), 0, true},
	}
	for _, tt := range tests {
		got, err := SignatureSizeForType(tt.st)
		if (err != nil) != tt.err {
			t.Errorf("SignatureSizeForType(%d): error=%v, wantErr=%v", tt.st, err, tt.err)
		}
		if got != tt.want {
			t.Errorf("SignatureSizeForType(%d) = %d, want %d", tt.st, got, tt.want)
		}
	}
}

// --- NewPQTxSigner tests ---

func TestNewPQTxSigner_ValidSchemes(t *testing.T) {
	for _, scheme := range []PQSignatureType{SigDilithium3, SigFalcon512, SigSPHINCS128} {
		s, err := NewPQTxSigner(scheme)
		if err != nil {
			t.Fatalf("NewPQTxSigner(%d): %v", scheme, err)
		}
		if s.Scheme() != scheme {
			t.Errorf("Scheme() = %d, want %d", s.Scheme(), scheme)
		}
	}
}

func TestNewPQTxSigner_InvalidScheme(t *testing.T) {
	_, err := NewPQTxSigner(PQSignatureType(42))
	if err != ErrUnsupportedScheme {
		t.Errorf("expected ErrUnsupportedScheme, got %v", err)
	}
}

// --- GenerateKey tests ---

func TestGenerateKey_AllSchemes(t *testing.T) {
	schemes := []PQSignatureType{SigDilithium3, SigFalcon512, SigSPHINCS128}
	for _, scheme := range schemes {
		signer, _ := NewPQTxSigner(scheme)
		priv, pub, err := signer.GenerateKey()
		if err != nil {
			t.Fatalf("GenerateKey(%d): %v", scheme, err)
		}
		if priv.Scheme != scheme {
			t.Errorf("priv.Scheme = %d, want %d", priv.Scheme, scheme)
		}
		if pub.Scheme != scheme {
			t.Errorf("pub.Scheme = %d, want %d", pub.Scheme, scheme)
		}
		if len(priv.Raw) == 0 {
			t.Error("empty private key")
		}
		if len(pub.Raw) == 0 {
			t.Error("empty public key")
		}
	}
}

func TestGenerateKey_UniqueKeys(t *testing.T) {
	signer, _ := NewPQTxSigner(SigDilithium3)
	priv1, _, _ := signer.GenerateKey()
	priv2, _, _ := signer.GenerateKey()

	// Keys from random generation should differ.
	if pqBytesEqual(priv1.Raw, priv2.Raw) {
		t.Error("two generated keys should not be identical")
	}
}

// --- SignTransaction tests ---

func TestSignTransaction_Dilithium3(t *testing.T) {
	testSignVerifyScheme(t, SigDilithium3, Dilithium3SignatureSize)
}

func TestSignTransaction_Falcon512(t *testing.T) {
	testSignVerifyScheme(t, SigFalcon512, Falcon512SignatureSize)
}

func TestSignTransaction_SPHINCS128(t *testing.T) {
	testSignVerifyScheme(t, SigSPHINCS128, SPHINCS128SignatureSize)
}

func testSignVerifyScheme(t *testing.T, scheme PQSignatureType, expectedSigSize int) {
	t.Helper()
	signer, _ := NewPQTxSigner(scheme)
	priv, pub, _ := signer.GenerateKey()
	tx := testLegacyTx()

	sig, err := signer.SignTransaction(tx, priv)
	if err != nil {
		t.Fatalf("SignTransaction: %v", err)
	}
	if len(sig) != expectedSigSize {
		t.Errorf("sig length = %d, want %d", len(sig), expectedSigSize)
	}

	ok, err := signer.VerifyTransaction(tx, sig, pub)
	if err != nil {
		t.Fatalf("VerifyTransaction: %v", err)
	}
	if !ok {
		t.Error("VerifyTransaction returned false for valid signature")
	}
}

func TestSignTransaction_NilTx(t *testing.T) {
	signer, _ := NewPQTxSigner(SigDilithium3)
	priv, _, _ := signer.GenerateKey()
	_, err := signer.SignTransaction(nil, priv)
	if err != ErrNilTransaction {
		t.Errorf("expected ErrNilTransaction, got %v", err)
	}
}

func TestSignTransaction_NilKey(t *testing.T) {
	signer, _ := NewPQTxSigner(SigDilithium3)
	tx := testLegacyTx()
	_, err := signer.SignTransaction(tx, nil)
	if err != ErrNilKey {
		t.Errorf("expected ErrNilKey, got %v", err)
	}
}

func TestSignTransaction_SchemeMismatch(t *testing.T) {
	signer, _ := NewPQTxSigner(SigDilithium3)
	// Generate key for a different scheme.
	falconSigner, _ := NewPQTxSigner(SigFalcon512)
	priv, _, _ := falconSigner.GenerateKey()

	tx := testLegacyTx()
	_, err := signer.SignTransaction(tx, priv)
	if err != ErrSchemeMismatch {
		t.Errorf("expected ErrSchemeMismatch, got %v", err)
	}
}

// --- VerifyTransaction tests ---

func TestVerifyTransaction_WrongSigSize(t *testing.T) {
	signer, _ := NewPQTxSigner(SigDilithium3)
	_, pub, _ := signer.GenerateKey()
	tx := testLegacyTx()

	// Provide a signature with wrong size.
	badSig := []byte("too short")
	ok, err := signer.VerifyTransaction(tx, badSig, pub)
	if err != ErrInvalidSignature {
		t.Errorf("expected ErrInvalidSignature, got %v", err)
	}
	if ok {
		t.Error("should not verify with wrong sig size")
	}
}

func TestVerifyTransaction_NilInputs(t *testing.T) {
	signer, _ := NewPQTxSigner(SigDilithium3)
	_, pub, _ := signer.GenerateKey()
	tx := testLegacyTx()

	ok, err := signer.VerifyTransaction(nil, make([]byte, Dilithium3SignatureSize), pub)
	if err != ErrNilTransaction {
		t.Errorf("expected ErrNilTransaction, got %v", err)
	}
	if ok {
		t.Error("should not verify nil tx")
	}

	sig := make([]byte, Dilithium3SignatureSize)
	ok, err = signer.VerifyTransaction(tx, sig, nil)
	if err != ErrNilKey {
		t.Errorf("expected ErrNilKey, got %v", err)
	}
	if ok {
		t.Error("should not verify nil pubkey")
	}
}

func TestVerifyTransaction_SchemeMismatch(t *testing.T) {
	signer, _ := NewPQTxSigner(SigDilithium3)
	falconSigner, _ := NewPQTxSigner(SigFalcon512)
	_, pub, _ := falconSigner.GenerateKey()
	tx := testLegacyTx()

	sig := make([]byte, Dilithium3SignatureSize)
	ok, err := signer.VerifyTransaction(tx, sig, pub)
	if err != ErrSchemeMismatch {
		t.Errorf("expected ErrSchemeMismatch, got %v", err)
	}
	if ok {
		t.Error("should not verify with mismatched scheme")
	}
}

// --- Deterministic signing tests ---

func TestSignTransaction_Deterministic(t *testing.T) {
	signer, _ := NewPQTxSigner(SigDilithium3)
	priv, _, _ := signer.GenerateKey()
	tx := testLegacyTx()

	sig1, _ := signer.SignTransaction(tx, priv)
	sig2, _ := signer.SignTransaction(tx, priv)

	if !pqBytesEqual(sig1, sig2) {
		t.Error("signing same tx with same key should produce identical signatures")
	}
}

func TestSignTransaction_DifferentTxs(t *testing.T) {
	signer, _ := NewPQTxSigner(SigDilithium3)
	priv, _, _ := signer.GenerateKey()

	tx1 := testLegacyTx()
	to2 := types.HexToAddress("0xbeef")
	tx2 := types.NewTransaction(&types.LegacyTx{
		Nonce:    2,
		GasPrice: big.NewInt(2_000_000_000),
		Gas:      42000,
		To:       &to2,
		Value:    big.NewInt(2_000_000),
		Data:     []byte("different data"),
	})

	sig1, _ := signer.SignTransaction(tx1, priv)
	sig2, _ := signer.SignTransaction(tx2, priv)

	if pqBytesEqual(sig1, sig2) {
		t.Error("different transactions should produce different signatures")
	}
}

// --- VerifyBatch tests ---

func TestVerifyBatch_AllValid(t *testing.T) {
	signer, _ := NewPQTxSigner(SigDilithium3)
	n := 5
	txs := make([]*types.Transaction, n)
	sigs := make([][]byte, n)
	pubs := make([]*PQPublicKey, n)

	for i := 0; i < n; i++ {
		priv, pub, _ := signer.GenerateKey()
		to := types.BytesToAddress([]byte{byte(i + 1)})
		tx := types.NewTransaction(&types.LegacyTx{
			Nonce:    uint64(i),
			GasPrice: big.NewInt(1_000_000_000),
			Gas:      21000,
			To:       &to,
			Value:    big.NewInt(int64(i + 1)),
		})
		sig, _ := signer.SignTransaction(tx, priv)
		txs[i] = tx
		sigs[i] = sig
		pubs[i] = pub
	}

	results, err := signer.VerifyBatch(txs, sigs, pubs)
	if err != nil {
		t.Fatalf("VerifyBatch: %v", err)
	}
	for i, ok := range results {
		if !ok {
			t.Errorf("result[%d] = false, want true", i)
		}
	}
}

func TestVerifyBatch_SomeInvalid(t *testing.T) {
	signer, _ := NewPQTxSigner(SigDilithium3)
	priv, pub, _ := signer.GenerateKey()
	tx := testLegacyTx()
	sig, _ := signer.SignTransaction(tx, priv)

	// Second entry has an all-zero signature (invalid).
	badSig := make([]byte, Dilithium3SignatureSize)

	txs := []*types.Transaction{tx, tx}
	sigs := [][]byte{sig, badSig}
	pubs := []*PQPublicKey{pub, pub}

	results, err := signer.VerifyBatch(txs, sigs, pubs)
	if err != nil {
		t.Fatalf("VerifyBatch: %v", err)
	}
	if !results[0] {
		t.Error("result[0] should be true")
	}
	if results[1] {
		t.Error("result[1] should be false (all-zero sig)")
	}
}

func TestVerifyBatch_LengthMismatch(t *testing.T) {
	signer, _ := NewPQTxSigner(SigDilithium3)
	_, err := signer.VerifyBatch(
		make([]*types.Transaction, 2),
		make([][]byte, 3),
		make([]*PQPublicKey, 2),
	)
	if err != ErrBatchLenMismatch {
		t.Errorf("expected ErrBatchLenMismatch, got %v", err)
	}
}

func TestVerifyBatch_Empty(t *testing.T) {
	signer, _ := NewPQTxSigner(SigDilithium3)
	results, err := signer.VerifyBatch(nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results for empty batch, got %v", results)
	}
}

// --- Thread-safety tests ---

func TestPQTxSigner_ConcurrentSignVerify(t *testing.T) {
	signer, _ := NewPQTxSigner(SigFalcon512)
	priv, pub, _ := signer.GenerateKey()
	tx := testLegacyTx()

	var wg sync.WaitGroup
	errs := make(chan error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sig, err := signer.SignTransaction(tx, priv)
			if err != nil {
				errs <- err
				return
			}
			ok, err := signer.VerifyTransaction(tx, sig, pub)
			if err != nil {
				errs <- err
				return
			}
			if !ok {
				errs <- ErrInvalidSignature
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent sign/verify error: %v", err)
	}
}

// --- DynamicFeeTx signing test ---

func TestSignTransaction_DynamicFeeTx(t *testing.T) {
	signer, _ := NewPQTxSigner(SigSPHINCS128)
	priv, pub, _ := signer.GenerateKey()

	to := types.HexToAddress("0xcafe")
	tx := types.NewTransaction(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     42,
		GasTipCap: big.NewInt(1_500_000_000),
		GasFeeCap: big.NewInt(30_000_000_000),
		Gas:       100_000,
		To:        &to,
		Value:     big.NewInt(5_000_000),
		Data:      []byte("eip1559 pq test"),
	})

	sig, err := signer.SignTransaction(tx, priv)
	if err != nil {
		t.Fatalf("SignTransaction: %v", err)
	}
	if len(sig) != SPHINCS128SignatureSize {
		t.Errorf("sig length = %d, want %d", len(sig), SPHINCS128SignatureSize)
	}

	ok, err := signer.VerifyTransaction(tx, sig, pub)
	if err != nil {
		t.Fatalf("VerifyTransaction: %v", err)
	}
	if !ok {
		t.Error("VerifyTransaction returned false for valid SPHINCS+ signature")
	}
}

// --- keySizesForScheme tests ---

func TestKeySizesForScheme(t *testing.T) {
	tests := []struct {
		scheme  PQSignatureType
		privSz  int
		pubSz   int
		wantErr bool
	}{
		{SigDilithium3, Dilithium3SecKeySize, Dilithium3PubKeySize, false},
		{SigFalcon512, Falcon512SecKeySize, Falcon512PubKeySize, false},
		{SigSPHINCS128, 64, 32, false},
		{PQSignatureType(99), 0, 0, true},
	}
	for _, tt := range tests {
		priv, pub, err := keySizesForScheme(tt.scheme)
		if (err != nil) != tt.wantErr {
			t.Errorf("keySizesForScheme(%d): err=%v, wantErr=%v", tt.scheme, err, tt.wantErr)
		}
		if priv != tt.privSz || pub != tt.pubSz {
			t.Errorf("keySizesForScheme(%d) = (%d,%d), want (%d,%d)", tt.scheme, priv, pub, tt.privSz, tt.pubSz)
		}
	}
}

// helper - uses pqBytesEqual to avoid collision with bytesEqual in pubkey_registry.go.
func pqBytesEqual(a, b []byte) bool {
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
