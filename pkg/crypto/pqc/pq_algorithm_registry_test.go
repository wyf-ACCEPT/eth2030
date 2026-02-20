package pqc

import (
	"testing"
)

func TestPQAlgRegistryGlobalInit(t *testing.T) {
	reg := GlobalRegistry()
	if reg == nil {
		t.Fatal("global registry should not be nil")
	}
	if reg.Size() < 5 {
		t.Errorf("global registry size = %d, want >= 5", reg.Size())
	}
}

func TestPQAlgRegistrySupportedAlgorithms(t *testing.T) {
	reg := GlobalRegistry()
	algTypes := reg.SupportedAlgorithms()
	if len(algTypes) < 5 {
		t.Errorf("supported algorithms = %d, want >= 5", len(algTypes))
	}

	// Check that all expected types are present.
	expected := []AlgorithmType{MLDSA44, MLDSA65, MLDSA87, ALG_FALCON512, ALG_SLHDSA}
	for _, e := range expected {
		if !reg.IsRegistered(e) {
			t.Errorf("algorithm type %d not registered", e)
		}
	}
}

func TestPQAlgRegistryGetAlgorithm(t *testing.T) {
	reg := GlobalRegistry()

	desc, err := reg.GetAlgorithm(MLDSA65)
	if err != nil {
		t.Fatalf("GetAlgorithm(MLDSA65) error: %v", err)
	}
	if desc.Name != "ML-DSA-65" {
		t.Errorf("name = %s, want ML-DSA-65", desc.Name)
	}
	if desc.SigSize != MLDSASignatureSize {
		t.Errorf("sig size = %d, want %d", desc.SigSize, MLDSASignatureSize)
	}
	if desc.PubKeySize != MLDSAPublicKeySize {
		t.Errorf("pubkey size = %d, want %d", desc.PubKeySize, MLDSAPublicKeySize)
	}
}

func TestPQAlgRegistryGasCost(t *testing.T) {
	reg := GlobalRegistry()

	tests := []struct {
		alg  AlgorithmType
		cost uint64
	}{
		{MLDSA44, GasCostMLDSA44},
		{MLDSA65, GasCostMLDSA65},
		{MLDSA87, GasCostMLDSA87},
		{ALG_FALCON512, GasCostFalcon512},
		{ALG_SLHDSA, GasCostSLHDSA},
	}

	for _, tt := range tests {
		cost, err := reg.GasCost(tt.alg)
		if err != nil {
			t.Errorf("GasCost(%d) error: %v", tt.alg, err)
			continue
		}
		if cost != tt.cost {
			t.Errorf("GasCost(%d) = %d, want %d", tt.alg, cost, tt.cost)
		}
	}
}

func TestPQAlgRegistryGasCostUnknown(t *testing.T) {
	reg := GlobalRegistry()
	_, err := reg.GasCost(AlgorithmType(99))
	if err == nil {
		t.Error("GasCost for unknown algorithm should return error")
	}
}

func TestPQAlgRegistryTotalGasCost(t *testing.T) {
	reg := GlobalRegistry()
	total, err := reg.TotalGasCost([]AlgorithmType{MLDSA65, ALG_FALCON512})
	if err != nil {
		t.Fatalf("TotalGasCost error: %v", err)
	}
	expected := GasCostMLDSA65 + GasCostFalcon512
	if total != expected {
		t.Errorf("TotalGasCost = %d, want %d", total, expected)
	}
}

func TestPQAlgRegistryVerifySignatureMLDSA65(t *testing.T) {
	reg := GlobalRegistry()
	signer := NewMLDSASigner()
	kp, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	msg := []byte("registry verify test")
	sig, err := signer.Sign(kp, msg)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	valid, err := reg.VerifySignature(MLDSA65, kp.PublicKey, msg, sig)
	if err != nil {
		t.Fatalf("VerifySignature error: %v", err)
	}
	if !valid {
		t.Error("VerifySignature should return true for valid signature")
	}
}

func TestPQAlgRegistryVerifySignatureSizeMismatch(t *testing.T) {
	reg := GlobalRegistry()
	_, err := reg.VerifySignature(MLDSA65, make([]byte, MLDSAPublicKeySize), []byte("msg"), make([]byte, 10))
	if err == nil {
		t.Error("VerifySignature should error on size mismatch")
	}
}

func TestPQAlgRegistryVerifySignaturePKMismatch(t *testing.T) {
	reg := GlobalRegistry()
	_, err := reg.VerifySignature(MLDSA65, make([]byte, 10), []byte("msg"), make([]byte, MLDSASignatureSize))
	if err == nil {
		t.Error("VerifySignature should error on pubkey size mismatch")
	}
}

func TestPQAlgRegistryRecoverPublicKey(t *testing.T) {
	reg := GlobalRegistry()
	_, err := reg.RecoverPublicKey(MLDSA65, []byte("msg"), make([]byte, MLDSASignatureSize))
	if err != ErrAlgRecoverFail {
		t.Errorf("RecoverPublicKey error = %v, want ErrAlgRecoverFail", err)
	}
}

func TestPQAlgRegistryRecoverPublicKeyUnknown(t *testing.T) {
	reg := GlobalRegistry()
	_, err := reg.RecoverPublicKey(AlgorithmType(99), []byte("msg"), []byte("sig"))
	if err == nil {
		t.Error("RecoverPublicKey for unknown algorithm should return error")
	}
}

func TestPQAlgRegistryAlgorithmName(t *testing.T) {
	reg := GlobalRegistry()

	tests := []struct {
		alg  AlgorithmType
		name string
	}{
		{MLDSA44, "ML-DSA-44"},
		{MLDSA65, "ML-DSA-65"},
		{MLDSA87, "ML-DSA-87"},
		{ALG_FALCON512, "Falcon-512"},
		{ALG_SLHDSA, "SLH-DSA-SHA2-128f"},
		{AlgorithmType(99), "unknown"},
	}

	for _, tt := range tests {
		name := reg.AlgorithmName(tt.alg)
		if name != tt.name {
			t.Errorf("AlgorithmName(%d) = %s, want %s", tt.alg, name, tt.name)
		}
	}
}

func TestPQAlgRegistryRegisterDuplicate(t *testing.T) {
	reg := NewPQAlgorithmRegistry()
	err := reg.RegisterAlgorithm(MLDSA44, "test", 100, 100, 1000, func(_, _, _ []byte) bool { return true })
	if err != nil {
		t.Fatalf("first register failed: %v", err)
	}
	err = reg.RegisterAlgorithm(MLDSA44, "test2", 100, 100, 1000, func(_, _, _ []byte) bool { return true })
	if err != ErrAlgAlreadyExists {
		t.Errorf("duplicate register error = %v, want ErrAlgAlreadyExists", err)
	}
}

func TestPQAlgRegistryRegisterNilVerifyFn(t *testing.T) {
	reg := NewPQAlgorithmRegistry()
	err := reg.RegisterAlgorithm(MLDSA44, "test", 100, 100, 1000, nil)
	if err != ErrAlgNilVerifyFn {
		t.Errorf("nil verify fn error = %v, want ErrAlgNilVerifyFn", err)
	}
}

func TestPQAlgRegistryIsRegistered(t *testing.T) {
	reg := GlobalRegistry()
	if !reg.IsRegistered(MLDSA65) {
		t.Error("MLDSA65 should be registered")
	}
	if reg.IsRegistered(AlgorithmType(99)) {
		t.Error("type 99 should not be registered")
	}
}
