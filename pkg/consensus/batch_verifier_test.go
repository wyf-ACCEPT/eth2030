package consensus

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// mockVerifyValid is a mock verify function that always returns true.
func mockVerifyValid(_ [48]byte, _ []byte, _ [96]byte) bool {
	return true
}

// mockVerifyByTag is a mock verify function that checks if sig[0] == 0xAA
// (valid marker) or sig[0] == 0xBB (invalid marker).
func mockVerifyByTag(_ [48]byte, _ []byte, sig [96]byte) bool {
	return sig[0] == 0xAA
}

// makeValidMockEntry creates a mock entry that will pass mockVerifyByTag.
func makeValidMockEntry(index byte) BatchVerifyEntry {
	var pk [48]byte
	pk[0] = index
	var sig [96]byte
	sig[0] = 0xAA // valid marker
	return BatchVerifyEntry{
		Pubkey:    pk,
		Message:   []byte{index},
		Signature: sig,
	}
}

// makeInvalidMockEntry creates a mock entry that will fail mockVerifyByTag.
func makeInvalidMockEntry(index byte) BatchVerifyEntry {
	var pk [48]byte
	pk[0] = index
	var sig [96]byte
	sig[0] = 0xBB // invalid marker
	return BatchVerifyEntry{
		Pubkey:    pk,
		Message:   []byte{index},
		Signature: sig,
	}
}

func TestBatchVerifier_NewBatchVerifier(t *testing.T) {
	bv := NewBatchVerifier(nil)
	if bv == nil {
		t.Fatal("expected non-nil batch verifier")
	}
	if bv.config.BatchSize != DefaultBatchVerifySize {
		t.Errorf("expected batch size %d, got %d", DefaultBatchVerifySize, bv.config.BatchSize)
	}
}

func TestBatchVerifier_EmptyBatch(t *testing.T) {
	bv := NewBatchVerifier(&BatchVerifierConfig{
		BatchSize:      128,
		EnableFallback: true,
		VerifyFn:       mockVerifyValid,
	})
	result := bv.BatchVerify(nil)
	if !result.Valid {
		t.Error("expected empty batch to be valid")
	}
	if result.BatchSize != 0 {
		t.Errorf("expected batch size 0, got %d", result.BatchSize)
	}
}

func TestBatchVerifier_SingleValid(t *testing.T) {
	bv := NewBatchVerifier(&BatchVerifierConfig{
		BatchSize:      128,
		EnableFallback: true,
		VerifyFn:       mockVerifyByTag,
	})

	entry := makeValidMockEntry(1)
	result := bv.BatchVerify([]BatchVerifyEntry{entry})
	if !result.Valid {
		t.Error("expected valid verification result")
	}
	if result.BatchSize != 1 {
		t.Errorf("expected batch size 1, got %d", result.BatchSize)
	}
}

func TestBatchVerifier_SingleInvalid(t *testing.T) {
	bv := NewBatchVerifier(&BatchVerifierConfig{
		BatchSize:      128,
		EnableFallback: true,
		VerifyFn:       mockVerifyByTag,
	})

	entry := makeInvalidMockEntry(1)
	result := bv.BatchVerify([]BatchVerifyEntry{entry})
	if result.Valid {
		t.Error("expected invalid verification result")
	}
	if len(result.InvalidIdxs) != 1 {
		t.Errorf("expected 1 invalid index, got %d", len(result.InvalidIdxs))
	}
	if result.InvalidIdxs[0] != 0 {
		t.Errorf("expected invalid index 0, got %d", result.InvalidIdxs[0])
	}
}

func TestBatchVerifier_MultipleValid(t *testing.T) {
	bv := NewBatchVerifier(&BatchVerifierConfig{
		BatchSize:      128,
		EnableFallback: true,
		VerifyFn:       mockVerifyByTag,
	})

	entries := make([]BatchVerifyEntry, 5)
	for i := 0; i < 5; i++ {
		entries[i] = makeValidMockEntry(byte(i))
	}

	result := bv.BatchVerify(entries)
	if !result.Valid {
		t.Error("expected valid batch verification")
	}
	if result.BatchSize != 5 {
		t.Errorf("expected batch size 5, got %d", result.BatchSize)
	}
}

func TestBatchVerifier_MixedValidInvalid(t *testing.T) {
	bv := NewBatchVerifier(&BatchVerifierConfig{
		BatchSize:      128,
		EnableFallback: true,
		VerifyFn:       mockVerifyByTag,
	})

	// 3 valid + 1 invalid.
	entries := make([]BatchVerifyEntry, 4)
	for i := 0; i < 3; i++ {
		entries[i] = makeValidMockEntry(byte(i))
	}
	entries[3] = makeInvalidMockEntry(3)

	result := bv.BatchVerify(entries)
	if result.Valid {
		t.Error("expected invalid result for mixed batch")
	}
	if result.UsedFallback {
		// Check that invalid index is identified.
		found := false
		for _, idx := range result.InvalidIdxs {
			if idx == 3 {
				found = true
			}
		}
		if !found {
			t.Error("expected index 3 to be in invalid indices")
		}
	}
}

func TestBatchVerifier_LargeBatchWithFallback(t *testing.T) {
	bv := NewBatchVerifier(&BatchVerifierConfig{
		BatchSize:      256,
		EnableFallback: true,
		VerifyFn:       mockVerifyByTag,
	})

	// 9 valid + 1 invalid (exceeds MinBatchSize of 4).
	entries := make([]BatchVerifyEntry, 10)
	for i := 0; i < 9; i++ {
		entries[i] = makeValidMockEntry(byte(i))
	}
	entries[9] = makeInvalidMockEntry(9)

	result := bv.BatchVerify(entries)
	if result.Valid {
		t.Error("expected invalid result")
	}
	if !result.UsedFallback {
		t.Error("expected fallback to be used for large batch")
	}
	// Invalid index should be 9.
	found := false
	for _, idx := range result.InvalidIdxs {
		if idx == 9 {
			found = true
		}
	}
	if !found {
		t.Error("expected index 9 in invalid indices")
	}
}

func TestBatchVerifier_AddAndFlush(t *testing.T) {
	bv := NewBatchVerifier(&BatchVerifierConfig{
		BatchSize:      3,
		EnableFallback: true,
		VerifyFn:       mockVerifyValid,
	})

	entry := makeValidMockEntry(1)

	full := bv.Add(entry)
	if full {
		t.Error("expected not full after 1 entry")
	}
	if bv.Pending() != 1 {
		t.Errorf("expected 1 pending, got %d", bv.Pending())
	}

	bv.Add(entry)
	full = bv.Add(entry)
	if !full {
		t.Error("expected full after 3 entries")
	}

	result := bv.Flush()
	if result.BatchSize != 3 {
		t.Errorf("expected batch size 3, got %d", result.BatchSize)
	}
	if bv.Pending() != 0 {
		t.Errorf("expected 0 pending after flush, got %d", bv.Pending())
	}
}

func TestBatchVerifier_FlushEmpty(t *testing.T) {
	bv := NewBatchVerifier(&BatchVerifierConfig{
		BatchSize:      128,
		EnableFallback: true,
		VerifyFn:       mockVerifyValid,
	})
	result := bv.Flush()
	if !result.Valid {
		t.Error("expected empty flush to be valid")
	}
	if result.BatchSize != 0 {
		t.Errorf("expected batch size 0, got %d", result.BatchSize)
	}
}

func TestBatchVerifier_FallbackDisabled(t *testing.T) {
	bv := NewBatchVerifier(&BatchVerifierConfig{
		BatchSize:      128,
		EnableFallback: false,
		VerifyFn:       mockVerifyByTag,
	})

	// Create a batch with an invalid signature (5 entries, > MinBatchSize).
	entries := make([]BatchVerifyEntry, 5)
	for i := 0; i < 4; i++ {
		entries[i] = makeValidMockEntry(byte(i))
	}
	entries[4] = makeInvalidMockEntry(4)

	result := bv.BatchVerify(entries)
	if result.Valid {
		t.Error("expected invalid result")
	}
	if result.UsedFallback {
		t.Error("expected no fallback when disabled")
	}
	// InvalidIdxs should be nil when fallback is disabled.
	if len(result.InvalidIdxs) != 0 {
		t.Errorf("expected no invalid indices without fallback, got %d", len(result.InvalidIdxs))
	}
}

func TestBatchVerifier_VerifyAttestationSigNil(t *testing.T) {
	var pk [48]byte
	if VerifyAttestationSig(pk, nil) {
		t.Error("expected false for nil attestation")
	}
}

func TestBatchVerifier_AttestationSigningRoot(t *testing.T) {
	att := &AggregateAttestation{
		Data: AttestationData{
			Slot:            Slot(10),
			BeaconBlockRoot: types.Hash{0x01},
			Source:          Checkpoint{Epoch: 1, Root: types.Hash{0x02}},
			Target:          Checkpoint{Epoch: 2, Root: types.Hash{0x03}},
		},
		AggregationBits: []byte{0xFF},
	}

	root1 := attestationSigningRoot(att)
	root2 := attestationSigningRoot(att)

	// Same attestation should produce same signing root.
	if len(root1) != len(root2) {
		t.Error("signing roots have different lengths")
	}
	for i := range root1 {
		if root1[i] != root2[i] {
			t.Error("signing roots differ for same attestation")
			break
		}
	}

	// Different attestation should produce different signing root.
	att2 := &AggregateAttestation{
		Data: AttestationData{
			Slot:            Slot(20),
			BeaconBlockRoot: types.Hash{0x04},
			Source:          Checkpoint{Epoch: 3, Root: types.Hash{0x05}},
			Target:          Checkpoint{Epoch: 4, Root: types.Hash{0x06}},
		},
		AggregationBits: []byte{0xFF},
	}
	root3 := attestationSigningRoot(att2)
	same := true
	for i := range root1 {
		if i < len(root3) && root1[i] != root3[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("different attestations produced same signing root")
	}
}

func TestBatchVerifier_Metrics(t *testing.T) {
	bv := NewBatchVerifier(&BatchVerifierConfig{
		BatchSize:      128,
		EnableFallback: true,
		VerifyFn:       mockVerifyValid,
	})

	entry := makeValidMockEntry(0)
	bv.BatchVerify([]BatchVerifyEntry{entry})

	verified, batches, fallbacks := bv.Metrics()
	if verified != 1 {
		t.Errorf("expected 1 verified, got %d", verified)
	}
	if batches != 1 {
		t.Errorf("expected 1 batch, got %d", batches)
	}
	_ = fallbacks
}

func TestBatchVerifier_DefaultVerifyFn(t *testing.T) {
	// Verify that the default config uses crypto.BLSVerify.
	bv := NewBatchVerifier(nil)
	if bv.config.VerifyFn == nil {
		t.Error("expected non-nil default verify function")
	}

	// Verify the function pointer is set (we cannot compare func values directly,
	// but we can verify it is not nil and the default config is correct).
	cfg := DefaultBatchVerifierConfig()
	if cfg.BatchSize != DefaultBatchVerifySize {
		t.Errorf("expected default batch size %d, got %d", DefaultBatchVerifySize, cfg.BatchSize)
	}
	if !cfg.EnableFallback {
		t.Error("expected default fallback enabled")
	}
}

func TestBatchVerifier_SmallBatchUsesIndividual(t *testing.T) {
	// Batches smaller than MinBatchSize should use individual verification.
	verifyCount := 0
	mockCount := func(_ [48]byte, _ []byte, _ [96]byte) bool {
		verifyCount++
		return true
	}

	bv := NewBatchVerifier(&BatchVerifierConfig{
		BatchSize:      128,
		EnableFallback: true,
		VerifyFn:       mockCount,
	})

	// 3 entries (< MinBatchSize of 4).
	entries := make([]BatchVerifyEntry, 3)
	for i := range entries {
		entries[i] = makeValidMockEntry(byte(i))
	}

	result := bv.BatchVerify(entries)
	if !result.Valid {
		t.Error("expected valid result")
	}
	if verifyCount != 3 {
		t.Errorf("expected 3 individual verifications, got %d", verifyCount)
	}
}

// Ensure the crypto import is used (VerifyAttestationSig calls crypto.BLSVerify).
var _ = crypto.Keccak256
