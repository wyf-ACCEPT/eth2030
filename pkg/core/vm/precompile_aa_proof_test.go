package vm

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestAAProofRequiredGas(t *testing.T) {
	p := &AAProofPrecompile{}

	// Empty input: base gas only.
	if gas := p.RequiredGas(nil); gas != 5000 {
		t.Errorf("empty input gas: got %d, want 5000", gas)
	}

	// 1-byte input (just type, no data): base gas.
	if gas := p.RequiredGas([]byte{0x01}); gas != 5000 {
		t.Errorf("1-byte input gas: got %d, want 5000", gas)
	}

	// 33 bytes (type + 32 bytes = 1 item): base + 1 * 1000 = 6000.
	input := make([]byte, 33)
	input[0] = AAProofCodeHash
	if gas := p.RequiredGas(input); gas != 6000 {
		t.Errorf("33-byte input gas: got %d, want 6000", gas)
	}

	// 65 bytes (type + 64 bytes = 2 items): base + 2 * 1000 = 7000.
	input = make([]byte, 65)
	input[0] = AAProofStorageProof
	if gas := p.RequiredGas(input); gas != 7000 {
		t.Errorf("65-byte input gas: got %d, want 7000", gas)
	}
}

func TestAAProofCodeHash(t *testing.T) {
	p := &AAProofPrecompile{}

	// Valid code hash (32 non-zero bytes).
	input := make([]byte, 33)
	input[0] = AAProofCodeHash
	for i := 1; i <= 32; i++ {
		input[i] = byte(i)
	}

	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(out, []byte{0x01}) {
		t.Errorf("valid code hash: got %x, want 01", out)
	}

	// Zero code hash should fail.
	input = make([]byte, 33)
	input[0] = AAProofCodeHash
	out, err = p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(out, []byte{0x00}) {
		t.Errorf("zero code hash: got %x, want 00", out)
	}
}

func TestAAProofStorageProof(t *testing.T) {
	p := &AAProofPrecompile{}

	// Valid storage proof: key[32] || value[32].
	input := make([]byte, 65)
	input[0] = AAProofStorageProof
	// Non-zero key.
	input[1] = 0xff
	// Value can be anything.
	input[33] = 0x42

	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(out, []byte{0x01}) {
		t.Errorf("valid storage proof: got %x, want 01", out)
	}

	// Too short storage proof.
	input = make([]byte, 33) // Only 32 bytes of proof data (need 64).
	input[0] = AAProofStorageProof
	input[1] = 0xff
	out, err = p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(out, []byte{0x00}) {
		t.Errorf("short storage proof: got %x, want 00", out)
	}
}

func TestAAProofValidationResult(t *testing.T) {
	p := &AAProofPrecompile{}

	// Valid validation result: status=0x01, validAfter=100, validUntil=200.
	input := make([]byte, 18)
	input[0] = AAProofValidationResult
	input[1] = 0x01                               // status
	binary.BigEndian.PutUint64(input[2:10], 100)  // validAfter
	binary.BigEndian.PutUint64(input[10:18], 200) // validUntil

	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(out, []byte{0x01}) {
		t.Errorf("valid result: got %x, want 01", out)
	}
}

func TestAAProofValidResultTimeRange(t *testing.T) {
	p := &AAProofPrecompile{}

	// Valid: validAfter=0, validUntil=max.
	input := make([]byte, 18)
	input[0] = AAProofValidationResult
	input[1] = 0x01
	binary.BigEndian.PutUint64(input[2:10], 0)
	binary.BigEndian.PutUint64(input[10:18], ^uint64(0))

	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(out, []byte{0x01}) {
		t.Errorf("valid wide time range: got %x, want 01", out)
	}
}

func TestAAProofExpiredTimeRange(t *testing.T) {
	p := &AAProofPrecompile{}

	// Expired: validAfter=200, validUntil=100 (after >= until).
	input := make([]byte, 18)
	input[0] = AAProofValidationResult
	input[1] = 0x01
	binary.BigEndian.PutUint64(input[2:10], 200)
	binary.BigEndian.PutUint64(input[10:18], 100)

	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(out, []byte{0x00}) {
		t.Errorf("expired time range: got %x, want 00", out)
	}

	// Equal: validAfter == validUntil.
	binary.BigEndian.PutUint64(input[2:10], 500)
	binary.BigEndian.PutUint64(input[10:18], 500)
	out, err = p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(out, []byte{0x00}) {
		t.Errorf("equal time range: got %x, want 00", out)
	}
}

func TestAAProofInvalidType(t *testing.T) {
	p := &AAProofPrecompile{}

	// Unknown proof type 0xFF.
	input := []byte{0xFF, 0x01, 0x02, 0x03}
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(out, []byte{0x00}) {
		t.Errorf("invalid type: got %x, want 00", out)
	}

	// Unknown proof type 0x00.
	input = []byte{0x00, 0x01, 0x02}
	out, err = p.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(out, []byte{0x00}) {
		t.Errorf("type 0x00: got %x, want 00", out)
	}
}

func TestAAProofShortInput(t *testing.T) {
	p := &AAProofPrecompile{}

	// Completely empty input.
	_, err := p.Run(nil)
	if err != ErrAAProofShortInput {
		t.Errorf("nil input: got %v, want ErrAAProofShortInput", err)
	}

	_, err = p.Run([]byte{})
	if err != ErrAAProofShortInput {
		t.Errorf("empty input: got %v, want ErrAAProofShortInput", err)
	}
}
