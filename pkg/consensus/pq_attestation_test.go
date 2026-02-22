package consensus

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto/pqc"
)

func TestDefaultPQAttestationConfig(t *testing.T) {
	cfg := DefaultPQAttestationConfig()
	if !cfg.UsePQSignatures {
		t.Error("UsePQSignatures should be true by default")
	}
	if !cfg.FallbackToClassic {
		t.Error("FallbackToClassic should be true by default")
	}
	if cfg.MinPQValidators != 0 {
		t.Errorf("MinPQValidators = %d, want 0", cfg.MinPQValidators)
	}
}

func TestCreatePQAttestation(t *testing.T) {
	pqKey, err := pqc.GenerateDilithiumKey()
	if err != nil {
		t.Fatalf("GenerateDilithiumKey: %v", err)
	}

	blockRoot := types.HexToHash("0xdeadbeef")
	att, err := CreatePQAttestation(100, 5, blockRoot, 3, 4, 42, pqKey)
	if err != nil {
		t.Fatalf("CreatePQAttestation: %v", err)
	}
	if att.Slot != 100 {
		t.Errorf("Slot = %d, want 100", att.Slot)
	}
	if att.CommitteeIndex != 5 {
		t.Errorf("CommitteeIndex = %d, want 5", att.CommitteeIndex)
	}
	if att.BeaconBlockRoot != blockRoot {
		t.Errorf("BeaconBlockRoot mismatch")
	}
	if att.SourceEpoch != 3 {
		t.Errorf("SourceEpoch = %d, want 3", att.SourceEpoch)
	}
	if att.TargetEpoch != 4 {
		t.Errorf("TargetEpoch = %d, want 4", att.TargetEpoch)
	}
	if att.ValidatorIndex != 42 {
		t.Errorf("ValidatorIndex = %d, want 42", att.ValidatorIndex)
	}
	if len(att.PQSignature) != pqc.DilithiumSignatureSize() {
		t.Errorf("PQSignature length = %d, want %d", len(att.PQSignature), pqc.DilithiumSignatureSize())
	}
	if len(att.ClassicSignature) != 0 {
		t.Errorf("ClassicSignature should be empty, got length %d", len(att.ClassicSignature))
	}
}

func TestCreatePQAttestationNilKey(t *testing.T) {
	blockRoot := types.HexToHash("0xdeadbeef")
	_, err := CreatePQAttestation(100, 5, blockRoot, 3, 4, 42, nil)
	if err != ErrPQAttNilKey {
		t.Errorf("CreatePQAttestation nil key: got %v, want %v", err, ErrPQAttNilKey)
	}
}

func TestVerifyPQAttestation(t *testing.T) {
	pqKey, err := pqc.GenerateDilithiumKey()
	if err != nil {
		t.Fatalf("GenerateDilithiumKey: %v", err)
	}

	blockRoot := types.HexToHash("0xabcdef01")
	att, err := CreatePQAttestation(200, 10, blockRoot, 6, 7, 99, pqKey)
	if err != nil {
		t.Fatalf("CreatePQAttestation: %v", err)
	}

	verifier := NewPQAttestationVerifier(DefaultPQAttestationConfig())
	ok, err := verifier.VerifyAttestation(att)
	if err != nil {
		t.Fatalf("VerifyAttestation: %v", err)
	}
	if !ok {
		t.Error("VerifyAttestation returned false for valid PQ attestation")
	}
}

func TestVerifyClassicFallback(t *testing.T) {
	// Create an attestation with only a classic (ECDSA-style) signature.
	att := &PQAttestation{
		Slot:             50,
		CommitteeIndex:   2,
		BeaconBlockRoot:  types.HexToHash("0x1234"),
		SourceEpoch:      1,
		TargetEpoch:      2,
		ValidatorIndex:   7,
		ClassicSignature: make([]byte, 65),
	}
	// Fill with non-zero bytes.
	for i := range att.ClassicSignature {
		att.ClassicSignature[i] = byte(i + 1)
	}

	verifier := NewPQAttestationVerifier(DefaultPQAttestationConfig())
	ok, err := verifier.VerifyAttestation(att)
	if err != nil {
		t.Fatalf("VerifyAttestation: %v", err)
	}
	if !ok {
		t.Error("VerifyAttestation should accept classic fallback signature")
	}
}

func TestVerifyNoSignature(t *testing.T) {
	att := &PQAttestation{
		Slot:            50,
		CommitteeIndex:  2,
		BeaconBlockRoot: types.HexToHash("0x1234"),
		SourceEpoch:     1,
		TargetEpoch:     2,
		ValidatorIndex:  7,
	}

	verifier := NewPQAttestationVerifier(DefaultPQAttestationConfig())
	ok, err := verifier.VerifyAttestation(att)
	if ok {
		t.Error("VerifyAttestation should reject attestation with no signature")
	}
	if err != ErrPQAttNoSignature {
		t.Errorf("expected ErrPQAttNoSignature, got %v", err)
	}
}

func TestVerifyTamperedPQSig(t *testing.T) {
	pqKey, err := pqc.GenerateDilithiumKey()
	if err != nil {
		t.Fatalf("GenerateDilithiumKey: %v", err)
	}

	blockRoot := types.HexToHash("0xfeed")
	att, err := CreatePQAttestation(300, 1, blockRoot, 10, 11, 55, pqKey)
	if err != nil {
		t.Fatalf("CreatePQAttestation: %v", err)
	}

	// Tamper: zero out the signature (all-zero fails VerifyDilithium).
	for i := range att.PQSignature {
		att.PQSignature[i] = 0
	}

	// Disable classic fallback so tampered PQ sig must fail.
	cfg := DefaultPQAttestationConfig()
	cfg.FallbackToClassic = false
	verifier := NewPQAttestationVerifier(cfg)

	ok, err := verifier.VerifyAttestation(att)
	if ok {
		t.Error("VerifyAttestation should reject tampered (zeroed) PQ signature")
	}
	if err != ErrPQAttInvalidPQSig {
		t.Errorf("expected ErrPQAttInvalidPQSig, got %v", err)
	}
}

func TestVerifierStats(t *testing.T) {
	verifier := NewPQAttestationVerifier(DefaultPQAttestationConfig())

	// Initial stats should be zero.
	v, f := verifier.Stats()
	if v != 0 || f != 0 {
		t.Errorf("initial stats: verified=%d, failed=%d; want 0, 0", v, f)
	}

	// Verify a valid PQ attestation.
	pqKey, _ := pqc.GenerateDilithiumKey()
	att, _ := CreatePQAttestation(1, 0, types.Hash{}, 0, 1, 0, pqKey)
	verifier.VerifyAttestation(att)

	v, f = verifier.Stats()
	if v != 1 {
		t.Errorf("verified = %d, want 1", v)
	}
	if f != 0 {
		t.Errorf("failed = %d, want 0", f)
	}

	// Fail: no signature.
	verifier.VerifyAttestation(&PQAttestation{})

	v, f = verifier.Stats()
	if v != 1 {
		t.Errorf("verified = %d, want 1", v)
	}
	if f != 1 {
		t.Errorf("failed = %d, want 1", f)
	}

	// Fail: nil attestation.
	verifier.VerifyAttestation(nil)
	v, f = verifier.Stats()
	if f != 2 {
		t.Errorf("failed = %d, want 2", f)
	}
}

func TestVerifyNilAttestation(t *testing.T) {
	verifier := NewPQAttestationVerifier(DefaultPQAttestationConfig())
	ok, err := verifier.VerifyAttestation(nil)
	if ok {
		t.Error("VerifyAttestation(nil) should return false")
	}
	if err != ErrPQAttNoSignature {
		t.Errorf("expected ErrPQAttNoSignature, got %v", err)
	}
}

func TestValidatePQAttestation(t *testing.T) {
	// Valid attestation.
	att := &PQAttestation{
		SourceEpoch:     5,
		TargetEpoch:     6,
		BeaconBlockRoot: types.Hash{0x01},
		PQSignature:     make([]byte, 32),
	}
	if err := ValidatePQAttestation(att); err != nil {
		t.Errorf("valid attestation: %v", err)
	}

	// Nil.
	if err := ValidatePQAttestation(nil); err == nil {
		t.Error("expected error for nil attestation")
	}

	// No signature.
	noSig := &PQAttestation{SourceEpoch: 5, TargetEpoch: 6, BeaconBlockRoot: types.Hash{0x01}}
	if err := ValidatePQAttestation(noSig); err == nil {
		t.Error("expected error for no signature")
	}

	// Target before source.
	badEpoch := &PQAttestation{
		SourceEpoch: 10, TargetEpoch: 5,
		BeaconBlockRoot: types.Hash{0x01}, PQSignature: make([]byte, 32),
	}
	if err := ValidatePQAttestation(badEpoch); err == nil {
		t.Error("expected error for target before source")
	}
}
