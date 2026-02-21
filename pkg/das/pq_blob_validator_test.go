package das

import (
	"bytes"
	"testing"
)

func TestPQBlobValidatorNew(t *testing.T) {
	tests := []struct {
		alg     string
		wantNil bool
	}{
		{PQAlgDilithium, false}, {PQAlgFalcon, false}, {PQAlgSPHINCS, false},
		{"rsa", true}, {"", true},
	}
	for _, tt := range tests {
		v := NewPQBlobValidator(tt.alg)
		if tt.wantNil && v != nil {
			t.Fatalf("expected nil for %q", tt.alg)
		}
		if !tt.wantNil && v == nil {
			t.Fatalf("expected non-nil for %q", tt.alg)
		}
		if v != nil && v.Algorithm() != tt.alg {
			t.Fatalf("algorithm: got %q, want %q", v.Algorithm(), tt.alg)
		}
	}
}

func TestPQBlobValidatorCommitAndVerify(t *testing.T) {
	for _, alg := range SupportedPQAlgorithms() {
		t.Run(alg, func(t *testing.T) {
			v := NewPQBlobValidator(alg)
			blob := pqvMakeBlob(256, 251)
			commitment, err := CommitBlob(blob)
			if err != nil {
				t.Fatalf("CommitBlob: %v", err)
			}
			if err := v.ValidateBlobCommitment(blob, commitment.Digest[:]); err != nil {
				t.Fatalf("ValidateBlobCommitment: %v", err)
			}
		})
	}
}

func TestPQBlobValidatorInvalidCommitment(t *testing.T) {
	v := NewPQBlobValidator(PQAlgDilithium)
	blob := []byte("test blob data for invalid commitment check")
	err := v.ValidateBlobCommitment(blob, make([]byte, PQCommitmentSize))
	if err != ErrPQValidatorMismatch {
		t.Fatalf("expected ErrPQValidatorMismatch, got %v", err)
	}
}

func TestPQBlobValidatorTamperedBlob(t *testing.T) {
	v := NewPQBlobValidator(PQAlgFalcon)
	blob := pqvMakeBlob(128, 256)
	commitment, err := CommitBlob(blob)
	if err != nil {
		t.Fatalf("CommitBlob: %v", err)
	}
	tampered := make([]byte, len(blob))
	copy(tampered, blob)
	tampered[0] ^= 0xFF
	if err := v.ValidateBlobCommitment(tampered, commitment.Digest[:]); err == nil {
		t.Fatal("expected error for tampered blob")
	}
}

func TestPQBlobValidatorProofGenAndVerify(t *testing.T) {
	for _, alg := range SupportedPQAlgorithms() {
		t.Run(alg, func(t *testing.T) {
			v := NewPQBlobValidator(alg)
			blob := pqvMakeBlob(512, 256)
			proof, err := v.GenerateCommitmentProof(blob)
			if err != nil {
				t.Fatalf("GenerateCommitmentProof: %v", err)
			}
			if proof.Algorithm != alg {
				t.Fatalf("algorithm: got %q, want %q", proof.Algorithm, alg)
			}
			if len(proof.Signature) == 0 || len(proof.PublicKey) == 0 || len(proof.MerkleProof) == 0 {
				t.Fatal("proof has empty fields")
			}
			if err := v.ValidateBlobProof(blob, proof, proof.MerkleRoot[:]); err != nil {
				t.Fatalf("ValidateBlobProof: %v", err)
			}
		})
	}
}

func TestPQBlobValidatorInvalidProof(t *testing.T) {
	v := NewPQBlobValidator(PQAlgDilithium)
	blob := pqvMakeBlob(256, 256)
	proof, err := v.GenerateCommitmentProof(blob)
	if err != nil {
		t.Fatalf("GenerateCommitmentProof: %v", err)
	}

	// Tamper with MerkleRoot so it mismatches the commitment.
	tampered := &PQBlobProofV2{
		Algorithm: proof.Algorithm, Signature: append([]byte{}, proof.Signature...),
		PublicKey: proof.PublicKey, MerkleProof: proof.MerkleProof,
		BlobIndex: proof.BlobIndex, SlotNumber: proof.SlotNumber,
	}
	copy(tampered.MerkleRoot[:], proof.MerkleRoot[:])
	for i := range tampered.MerkleRoot {
		tampered.MerkleRoot[i] ^= 0xFF
	}
	if err := v.ValidateBlobProof(blob, tampered, proof.MerkleRoot[:]); err == nil {
		t.Fatal("expected error for tampered merkle root")
	}

	// Zeroed signature (stub verify rejects all-zero first 32 bytes).
	zeroSig := &PQBlobProofV2{
		Algorithm: proof.Algorithm, Signature: make([]byte, len(proof.Signature)),
		PublicKey: proof.PublicKey, MerkleRoot: proof.MerkleRoot,
		MerkleProof: proof.MerkleProof, BlobIndex: proof.BlobIndex, SlotNumber: proof.SlotNumber,
	}
	if err := v.ValidateBlobProof(blob, zeroSig, proof.MerkleRoot[:]); err == nil {
		t.Fatal("expected error for zeroed signature")
	}
}

func TestPQBlobValidatorBatchValidate(t *testing.T) {
	v := NewPQBlobValidator(PQAlgDilithium)
	n := 8
	blobs := make([][]byte, n)
	commitments := make([][]byte, n)
	for i := 0; i < n; i++ {
		blobs[i] = pqvMakeBlob(64+i*32, 256)
		c, err := CommitBlob(blobs[i])
		if err != nil {
			t.Fatalf("CommitBlob[%d]: %v", i, err)
		}
		commitments[i] = c.Digest[:]
	}
	results, err := v.BatchValidateCommitments(blobs, commitments)
	if err != nil {
		t.Fatalf("BatchValidateCommitments: %v", err)
	}
	for i, valid := range results {
		if !valid {
			t.Fatalf("blob %d should be valid", i)
		}
	}
}

func TestPQBlobValidatorBatchPartialFailure(t *testing.T) {
	v := NewPQBlobValidator(PQAlgFalcon)
	n := 6
	blobs := make([][]byte, n)
	commitments := make([][]byte, n)
	for i := 0; i < n; i++ {
		blobs[i] = pqvMakeBlob(64+i*16, 256)
		c, err := CommitBlob(blobs[i])
		if err != nil {
			t.Fatalf("CommitBlob[%d]: %v", i, err)
		}
		commitments[i] = c.Digest[:]
	}
	commitments[1] = make([]byte, PQCommitmentSize) // invalid
	commitments[3] = make([]byte, PQCommitmentSize) // invalid
	results, err := v.BatchValidateCommitments(blobs, commitments)
	if err != nil {
		t.Fatalf("BatchValidateCommitments: %v", err)
	}
	expected := []bool{true, false, true, false, true, true}
	for i, want := range expected {
		if results[i] != want {
			t.Fatalf("blob %d: got %v, want %v", i, results[i], want)
		}
	}
}

func TestPQBlobValidatorEmptyBlob(t *testing.T) {
	v := NewPQBlobValidator(PQAlgDilithium)
	if err := v.ValidateBlobCommitment(nil, []byte{1}); err != ErrPQValidatorNilBlob {
		t.Fatalf("nil blob: got %v", err)
	}
	if err := v.ValidateBlobCommitment([]byte{}, []byte{1}); err != ErrPQValidatorEmptyBlob {
		t.Fatalf("empty blob: got %v", err)
	}
	if err := v.ValidateBlobCommitment([]byte{1}, nil); err != ErrPQValidatorNilCommit {
		t.Fatalf("nil commit: got %v", err)
	}
	if err := v.ValidateBlobCommitment([]byte{1}, []byte{}); err != ErrPQValidatorNilCommit {
		t.Fatalf("empty commit: got %v", err)
	}
	if _, err := v.GenerateCommitmentProof(nil); err != ErrPQValidatorNilBlob {
		t.Fatalf("nil proof gen: got %v", err)
	}
	if _, err := v.GenerateCommitmentProof([]byte{}); err != ErrPQValidatorEmptyBlob {
		t.Fatalf("empty proof gen: got %v", err)
	}
}

func TestPQBlobValidatorLargeBlob(t *testing.T) {
	v := NewPQBlobValidator(PQAlgDilithium)
	blob := pqvMakeBlob(MaxBlobSize, 199)
	commitment, err := CommitBlob(blob)
	if err != nil {
		t.Fatalf("CommitBlob: %v", err)
	}
	if err := v.ValidateBlobCommitment(blob, commitment.Digest[:]); err != nil {
		t.Fatalf("large blob validation: %v", err)
	}
	tooBig := make([]byte, MaxBlobSize+1)
	if err := v.ValidateBlobCommitment(tooBig, commitment.Digest[:]); err != ErrPQValidatorBlobTooLarge {
		t.Fatalf("oversized blob: got %v", err)
	}
}

func TestPQBlobValidatorGasEstimation(t *testing.T) {
	tests := []struct {
		alg    string
		size   int
		sigGas uint64
	}{
		{PQAlgDilithium, 1024, pqDilithiumVerifyGas},
		{PQAlgFalcon, 1024, pqFalconVerifyGas},
		{PQAlgSPHINCS, 1024, pqSPHINCSVerifyGas},
		{PQAlgDilithium, 0, pqDilithiumVerifyGas},
		{PQAlgSPHINCS, MaxBlobSize, pqSPHINCSVerifyGas},
	}
	for _, tt := range tests {
		gas := EstimateValidationGas(tt.alg, tt.size)
		want := uint64(pqValidateBaseGas) + uint64(tt.size)*uint64(pqValidatePerByteGas) + tt.sigGas + pqMerkleProofGas
		if gas != want {
			t.Fatalf("%s/%d: gas %d != %d", tt.alg, tt.size, gas, want)
		}
	}
	if EstimateValidationGas(PQAlgSPHINCS, 4096) <= EstimateValidationGas(PQAlgFalcon, 4096) {
		t.Fatal("SPHINCS+ gas should exceed Falcon gas")
	}
}

func TestPQBlobValidatorSupportedAlgorithms(t *testing.T) {
	algs := SupportedPQAlgorithms()
	if len(algs) != 3 {
		t.Fatalf("expected 3 algorithms, got %d", len(algs))
	}
	want := map[string]bool{PQAlgDilithium: true, PQAlgFalcon: true, PQAlgSPHINCS: true}
	for _, alg := range algs {
		if !want[alg] {
			t.Fatalf("unexpected algorithm: %s", alg)
		}
		if NewPQBlobValidator(alg) == nil {
			t.Fatalf("validator nil for %q", alg)
		}
	}
}

func TestPQBlobValidatorDilithiumAlgorithm(t *testing.T) {
	pqBlobValidatorAlgTest(t, PQAlgDilithium, "dilithium test blob data for verification")
}

func TestPQBlobValidatorFalconAlgorithm(t *testing.T) {
	pqBlobValidatorAlgTest(t, PQAlgFalcon, "falcon test blob data for pq verification")
}

func TestPQBlobValidatorSPHINCSAlgorithm(t *testing.T) {
	pqBlobValidatorAlgTest(t, PQAlgSPHINCS, "sphincs test blob data for hash-based verification")
}

func TestPQBlobValidatorUnsupportedAlgorithm(t *testing.T) {
	for _, alg := range []string{"rsa", "ecdsa", "ed25519", "ntru", "", "DILITHIUM"} {
		if NewPQBlobValidator(alg) != nil {
			t.Fatalf("expected nil for %q", alg)
		}
	}
}

func TestPQBlobValidatorBatchEmptyAndMismatch(t *testing.T) {
	v := NewPQBlobValidator(PQAlgDilithium)
	if _, err := v.BatchValidateCommitments(nil, nil); err != ErrPQValidatorBatchEmpty {
		t.Fatalf("nil batch: got %v", err)
	}
	if _, err := v.BatchValidateCommitments([][]byte{}, [][]byte{}); err != ErrPQValidatorBatchEmpty {
		t.Fatalf("empty batch: got %v", err)
	}
	if _, err := v.BatchValidateCommitments([][]byte{{1}}, [][]byte{{1}, {2}}); err != ErrPQValidatorBatchLen {
		t.Fatalf("len mismatch: got %v", err)
	}
}

func TestPQBlobValidatorProofNilInputs(t *testing.T) {
	v := NewPQBlobValidator(PQAlgDilithium)
	commit := make([]byte, PQCommitmentSize)
	if err := v.ValidateBlobProof(nil, &PQBlobProofV2{}, commit); err != ErrPQValidatorNilBlob {
		t.Fatalf("nil blob: got %v", err)
	}
	if err := v.ValidateBlobProof([]byte{}, &PQBlobProofV2{}, commit); err != ErrPQValidatorEmptyBlob {
		t.Fatalf("empty blob: got %v", err)
	}
	if err := v.ValidateBlobProof([]byte("x"), nil, commit); err != ErrPQValidatorNilProof {
		t.Fatalf("nil proof: got %v", err)
	}
	if err := v.ValidateBlobProof([]byte("x"), &PQBlobProofV2{}, nil); err != ErrPQValidatorNilCommit {
		t.Fatalf("nil commit: got %v", err)
	}
}

func TestPQBlobValidatorSerializeChunkProof(t *testing.T) {
	blob := pqvMakeBlob(128, 256)
	proof, err := GenerateBlobProof(blob, 0)
	if err != nil {
		t.Fatalf("GenerateBlobProof: %v", err)
	}
	serialized := serializeChunkProof(proof)
	wantLen := 4 + 32 + PQProofSize + PQCommitmentSize
	if len(serialized) != wantLen {
		t.Fatalf("length: got %d, want %d", len(serialized), wantLen)
	}
	if serializeChunkProof(nil) != nil {
		t.Fatal("expected nil for nil proof")
	}
}

func TestPQBlobValidatorCommitmentDigestBytes(t *testing.T) {
	blob := []byte("test data for digest extraction")
	commitment, err := CommitBlob(blob)
	if err != nil {
		t.Fatalf("CommitBlob: %v", err)
	}
	digest := commitmentDigestBytes(commitment)
	if !bytes.Equal(digest, commitment.Digest[:]) {
		t.Fatal("digest mismatch")
	}
	if commitmentDigestBytes(nil) != nil {
		t.Fatal("expected nil for nil commitment")
	}
}

// pqBlobValidatorAlgTest is a shared helper for per-algorithm tests.
func pqBlobValidatorAlgTest(t *testing.T, alg string, blobData string) {
	t.Helper()
	v := NewPQBlobValidator(alg)
	if v == nil {
		t.Fatalf("nil validator for %s", alg)
	}
	if v.Algorithm() != alg {
		t.Fatalf("algorithm: got %q, want %q", v.Algorithm(), alg)
	}
	blob := []byte(blobData)
	commitment, err := CommitBlob(blob)
	if err != nil {
		t.Fatalf("CommitBlob: %v", err)
	}
	if err := v.ValidateBlobCommitment(blob, commitment.Digest[:]); err != nil {
		t.Fatalf("validation: %v", err)
	}
	proof, err := v.GenerateCommitmentProof(blob)
	if err != nil {
		t.Fatalf("proof gen: %v", err)
	}
	if proof.Algorithm != alg {
		t.Fatalf("proof alg: got %q, want %q", proof.Algorithm, alg)
	}
}

// makeTestBlob creates a deterministic test blob of the given size.
func pqvMakeBlob(size, mod int) []byte {
	blob := make([]byte, size)
	for i := range blob {
		blob[i] = byte(i % mod)
	}
	return blob
}
