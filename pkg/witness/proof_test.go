package witness

import (
	"testing"
)

func TestExecutionProofValidate(t *testing.T) {
	tests := []struct {
		name    string
		proof   ExecutionProof
		wantErr error
	}{
		{
			name:    "valid SP1 proof",
			proof:   ExecutionProof{ProofType: ProofTypeSP1, ProofBytes: make([]byte, 100)},
			wantErr: nil,
		},
		{
			name:    "valid ZisK proof",
			proof:   ExecutionProof{ProofType: ProofTypeZisK, ProofBytes: make([]byte, 1024)},
			wantErr: nil,
		},
		{
			name:    "valid RISC0 proof",
			proof:   ExecutionProof{ProofType: ProofTypeRISC0, ProofBytes: make([]byte, MaxProofSize)},
			wantErr: nil,
		},
		{
			name:    "empty proof bytes",
			proof:   ExecutionProof{ProofType: ProofTypeSP1, ProofBytes: nil},
			wantErr: ErrEmptyProof,
		},
		{
			name:    "proof too large",
			proof:   ExecutionProof{ProofType: ProofTypeSP1, ProofBytes: make([]byte, MaxProofSize+1)},
			wantErr: ErrProofTooLarge,
		},
		{
			name:    "unknown proof type",
			proof:   ExecutionProof{ProofType: 99, ProofBytes: make([]byte, 100)},
			wantErr: ErrUnknownProofType,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.proof.Validate()
			if tt.wantErr == nil && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("expected error %v, got nil", tt.wantErr)
				}
			}
		})
	}
}

func TestProofSize(t *testing.T) {
	p := ExecutionProof{ProofBytes: make([]byte, 42)}
	if p.Size() != 42 {
		t.Errorf("Size() = %d, want 42", p.Size())
	}
}

func TestProofTypeName(t *testing.T) {
	tests := []struct {
		pt   uint8
		want string
	}{
		{ProofTypeSP1, "SP1"},
		{ProofTypeZisK, "ZisK"},
		{ProofTypeRISC0, "RISC0"},
		{99, "Unknown(99)"},
	}
	for _, tt := range tests {
		if got := ProofTypeName(tt.pt); got != tt.want {
			t.Errorf("ProofTypeName(%d) = %q, want %q", tt.pt, got, tt.want)
		}
	}
}

func TestMaxProofSizeConstant(t *testing.T) {
	if MaxProofSize != 300*1024 {
		t.Errorf("MaxProofSize = %d, want %d", MaxProofSize, 300*1024)
	}
}
