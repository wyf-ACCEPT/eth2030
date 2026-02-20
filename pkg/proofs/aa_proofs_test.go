package proofs

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// canonical entry point used throughout AA proof tests.
var testEntryPoint = types.HexToAddress("0x0000000000000000000000000000000000007701")

func makeTestUserOp() *UserOperation {
	return &UserOperation{
		Sender:               types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		Nonce:                42,
		InitCode:             []byte{0x60, 0x00},
		CallData:             []byte{0xa9, 0x05, 0x9c, 0xbb},
		CallGasLimit:         100_000,
		VerificationGasLimit: 200_000,
		PreVerificationGas:   21_000,
		MaxFeePerGas:         30_000_000_000,
		MaxPriorityFeePerGas: 1_000_000_000,
		PaymasterAndData:     nil,
		Signature:            []byte{0x01, 0x02, 0x03, 0x04},
	}
}

func TestUserOperationHash(t *testing.T) {
	op := makeTestUserOp()
	h := op.Hash()
	if h.IsZero() {
		t.Fatal("user operation hash should not be zero")
	}

	// Determinism: same inputs produce the same hash.
	h2 := op.Hash()
	if h != h2 {
		t.Fatalf("hash not deterministic: %s vs %s", h.Hex(), h2.Hex())
	}

	// Changing the nonce must change the hash.
	op.Nonce = 43
	h3 := op.Hash()
	if h3 == h {
		t.Fatal("different nonces should produce different hashes")
	}
}

func TestDefaultAAProofConfig(t *testing.T) {
	cfg := DefaultAAProofConfig()
	if cfg.MaxVerificationGas != DefaultMaxVerificationGas {
		t.Errorf("MaxVerificationGas: got %d, want %d", cfg.MaxVerificationGas, DefaultMaxVerificationGas)
	}
	if len(cfg.AllowedEntryPoints) != 1 {
		t.Fatalf("expected 1 allowed entry point, got %d", len(cfg.AllowedEntryPoints))
	}
	if cfg.AllowedEntryPoints[0] != testEntryPoint {
		t.Errorf("unexpected default entry point: %s", cfg.AllowedEntryPoints[0].Hex())
	}
}

func TestGenerateValidationProof(t *testing.T) {
	gen := NewAAProofGenerator(nil)
	op := makeTestUserOp()

	proof, err := gen.GenerateValidationProof(op, testEntryPoint)
	if err != nil {
		t.Fatalf("GenerateValidationProof: %v", err)
	}
	if proof == nil {
		t.Fatal("proof should not be nil")
	}
	if proof.Commitment.IsZero() {
		t.Error("commitment should not be zero")
	}
	if proof.ValidationHash.IsZero() {
		t.Error("validation hash should not be zero")
	}
	if proof.EntryPoint != testEntryPoint {
		t.Errorf("entry point: got %s, want %s", proof.EntryPoint.Hex(), testEntryPoint.Hex())
	}
	if proof.GasUsed == 0 {
		t.Error("gas used should be non-zero")
	}
	if len(proof.ProofData) == 0 {
		t.Error("proof data should not be empty")
	}
}

func TestGenerateValidationProofNilOp(t *testing.T) {
	gen := NewAAProofGenerator(nil)
	_, err := gen.GenerateValidationProof(nil, testEntryPoint)
	if err != ErrAANilUserOp {
		t.Errorf("expected ErrAANilUserOp, got %v", err)
	}
}

func TestGenerateValidationProofZeroEntryPoint(t *testing.T) {
	gen := NewAAProofGenerator(nil)
	op := makeTestUserOp()
	_, err := gen.GenerateValidationProof(op, types.Address{})
	if err != ErrAAEmptyEntryPoint {
		t.Errorf("expected ErrAAEmptyEntryPoint, got %v", err)
	}
}

func TestGenerateValidationProofNotAllowed(t *testing.T) {
	cfg := &AAProofConfig{
		MaxVerificationGas: DefaultMaxVerificationGas,
		AllowedEntryPoints: []types.Address{types.HexToAddress("0xdead")},
	}
	gen := NewAAProofGenerator(cfg)
	op := makeTestUserOp()

	_, err := gen.GenerateValidationProof(op, testEntryPoint)
	if err != ErrAAEntryPointNotAllowed {
		t.Errorf("expected ErrAAEntryPointNotAllowed, got %v", err)
	}
}

func TestGenerateValidationProofExceedsGas(t *testing.T) {
	cfg := &AAProofConfig{
		MaxVerificationGas: 100, // very low
		AllowedEntryPoints: []types.Address{testEntryPoint},
	}
	gen := NewAAProofGenerator(cfg)
	op := makeTestUserOp()
	op.VerificationGasLimit = 200

	_, err := gen.GenerateValidationProof(op, testEntryPoint)
	if err != ErrAAExceedsMaxGas {
		t.Errorf("expected ErrAAExceedsMaxGas, got %v", err)
	}
}

func TestVerifyValidationProof(t *testing.T) {
	gen := NewAAProofGenerator(nil)
	op := makeTestUserOp()

	proof, err := gen.GenerateValidationProof(op, testEntryPoint)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if !VerifyValidationProof(proof) {
		t.Fatal("valid proof should verify")
	}
}

func TestVerifyValidationProofTampered(t *testing.T) {
	gen := NewAAProofGenerator(nil)
	op := makeTestUserOp()

	proof, _ := gen.GenerateValidationProof(op, testEntryPoint)

	// Tamper with proof data.
	proof.ProofData[0] ^= 0xff
	if VerifyValidationProof(proof) {
		t.Fatal("tampered proof should not verify")
	}
}

func TestVerifyValidationProofNil(t *testing.T) {
	if VerifyValidationProof(nil) {
		t.Fatal("nil proof should not verify")
	}
}

func TestVerifyValidationProofZeroFields(t *testing.T) {
	proof := &AAProof{
		ProofData: []byte{0x01},
	}
	if VerifyValidationProof(proof) {
		t.Fatal("proof with zero commitment should not verify")
	}
}

func TestBatchVerifyAAProofs(t *testing.T) {
	gen := NewAAProofGenerator(nil)

	var proofs []*AAProof
	for i := 0; i < 5; i++ {
		op := makeTestUserOp()
		op.Nonce = uint64(i)
		p, err := gen.GenerateValidationProof(op, testEntryPoint)
		if err != nil {
			t.Fatalf("generate proof %d: %v", i, err)
		}
		proofs = append(proofs, p)
	}

	valid, invalid := BatchVerifyAAProofs(proofs)
	if valid != 5 {
		t.Errorf("expected 5 valid, got %d", valid)
	}
	if invalid != 0 {
		t.Errorf("expected 0 invalid, got %d", invalid)
	}

	// Tamper with one proof.
	proofs[2].ProofData[0] ^= 0xff
	valid, invalid = BatchVerifyAAProofs(proofs)
	if valid != 4 {
		t.Errorf("expected 4 valid, got %d", valid)
	}
	if invalid != 1 {
		t.Errorf("expected 1 invalid, got %d", invalid)
	}
}

func TestBatchVerifyAAProofsEmpty(t *testing.T) {
	valid, invalid := BatchVerifyAAProofs(nil)
	if valid != 0 || invalid != 0 {
		t.Errorf("empty batch: valid=%d invalid=%d", valid, invalid)
	}
}

func TestAAProofSize(t *testing.T) {
	gen := NewAAProofGenerator(nil)
	op := makeTestUserOp()

	proof, _ := gen.GenerateValidationProof(op, testEntryPoint)
	size := proof.ProofSize()

	// 32 (commitment) + 32 (validationHash) + 20 (entryPoint) + 8 (gas) + 32 (proof data) = 124
	expected := 32 + 32 + 20 + 8 + len(proof.ProofData)
	if size != expected {
		t.Errorf("ProofSize: got %d, want %d", size, expected)
	}
}

func TestAAProofSizeNil(t *testing.T) {
	var p *AAProof
	if p.ProofSize() != 0 {
		t.Error("nil proof should have zero size")
	}
}

func TestCompressDecompressProof(t *testing.T) {
	gen := NewAAProofGenerator(nil)
	op := makeTestUserOp()

	proof, _ := gen.GenerateValidationProof(op, testEntryPoint)

	compressed, err := CompressProof(proof)
	if err != nil {
		t.Fatalf("CompressProof: %v", err)
	}
	if len(compressed) == 0 {
		t.Fatal("compressed data should not be empty")
	}

	decompressed, err := DecompressProof(compressed)
	if err != nil {
		t.Fatalf("DecompressProof: %v", err)
	}

	if decompressed.Commitment != proof.Commitment {
		t.Error("commitment mismatch after round-trip")
	}
	if decompressed.ValidationHash != proof.ValidationHash {
		t.Error("validation hash mismatch after round-trip")
	}
	if decompressed.EntryPoint != proof.EntryPoint {
		t.Error("entry point mismatch after round-trip")
	}
	if decompressed.GasUsed != proof.GasUsed {
		t.Error("gas used mismatch after round-trip")
	}
	if len(decompressed.ProofData) != len(proof.ProofData) {
		t.Fatalf("proof data length mismatch: %d vs %d", len(decompressed.ProofData), len(proof.ProofData))
	}
	for i := range decompressed.ProofData {
		if decompressed.ProofData[i] != proof.ProofData[i] {
			t.Fatalf("proof data byte %d mismatch", i)
		}
	}

	// Decompressed proof should still verify.
	if !VerifyValidationProof(decompressed) {
		t.Fatal("decompressed proof should verify")
	}
}

func TestCompressProofNil(t *testing.T) {
	_, err := CompressProof(nil)
	if err != ErrAANilProof {
		t.Errorf("expected ErrAANilProof, got %v", err)
	}
}

func TestCompressProofEmptyData(t *testing.T) {
	proof := &AAProof{
		Commitment: types.HexToHash("0x01"),
	}
	_, err := CompressProof(proof)
	if err != ErrAACompressFailed {
		t.Errorf("expected ErrAACompressFailed, got %v", err)
	}
}

func TestDecompressProofTooShort(t *testing.T) {
	_, err := DecompressProof([]byte{0x01, 0x02})
	if err != ErrAADecompressTooShort {
		t.Errorf("expected ErrAADecompressTooShort, got %v", err)
	}
}

func TestDecompressProofBadVersion(t *testing.T) {
	buf := make([]byte, compressedHeaderSize+32)
	buf[0] = 0xFF // wrong version
	_, err := DecompressProof(buf)
	if err != ErrAADecompressFailed {
		t.Errorf("expected ErrAADecompressFailed, got %v", err)
	}
}

func TestGenerateProofDeterministic(t *testing.T) {
	gen := NewAAProofGenerator(nil)
	op := makeTestUserOp()

	p1, _ := gen.GenerateValidationProof(op, testEntryPoint)
	p2, _ := gen.GenerateValidationProof(op, testEntryPoint)

	if p1.Commitment != p2.Commitment {
		t.Error("same inputs should produce same commitment")
	}
	if p1.ValidationHash != p2.ValidationHash {
		t.Error("same inputs should produce same validation hash")
	}
}

func TestGenerateProofDifferentOps(t *testing.T) {
	gen := NewAAProofGenerator(nil)

	op1 := makeTestUserOp()
	op1.Nonce = 1
	op2 := makeTestUserOp()
	op2.Nonce = 2

	p1, _ := gen.GenerateValidationProof(op1, testEntryPoint)
	p2, _ := gen.GenerateValidationProof(op2, testEntryPoint)

	if p1.Commitment == p2.Commitment {
		t.Error("different ops should produce different commitments")
	}
}

func TestAAProofConfigEmptyAllowedEntryPoints(t *testing.T) {
	cfg := &AAProofConfig{
		MaxVerificationGas: DefaultMaxVerificationGas,
		AllowedEntryPoints: nil, // empty = allow all
	}
	gen := NewAAProofGenerator(cfg)
	op := makeTestUserOp()

	anyAddr := types.HexToAddress("0xdeadbeef")
	proof, err := gen.GenerateValidationProof(op, anyAddr)
	if err != nil {
		t.Fatalf("should allow any entry point when AllowedEntryPoints is empty: %v", err)
	}
	if proof.EntryPoint != anyAddr {
		t.Errorf("entry point: got %s, want %s", proof.EntryPoint.Hex(), anyAddr.Hex())
	}
}
