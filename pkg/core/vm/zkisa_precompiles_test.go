package vm

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestRegisterZkISAPrecompiles(t *testing.T) {
	registry := RegisterZkISAPrecompiles()

	expectedAddrs := []types.Address{
		types.BytesToAddress([]byte{0x01}), // ecrecover
		types.BytesToAddress([]byte{0x02}), // sha256
		types.BytesToAddress([]byte{0x05}), // modexp
		types.BytesToAddress([]byte{0x06}), // bn128add
		types.BytesToAddress([]byte{0x07}), // bn128mul
		types.BytesToAddress([]byte{0x08}), // bn128pairing
	}

	if len(registry) != len(expectedAddrs) {
		t.Fatalf("registry has %d entries, want %d", len(registry), len(expectedAddrs))
	}

	for _, addr := range expectedAddrs {
		p, ok := registry[addr]
		if !ok {
			t.Errorf("missing precompile at address %s", addr.Hex())
			continue
		}
		if p.Address() != addr {
			t.Errorf("precompile.Address() = %s, want %s", p.Address().Hex(), addr.Hex())
		}
		if p.Name() == "" {
			t.Error("precompile.Name() is empty")
		}
	}
}

func TestZkISASha256Execute(t *testing.T) {
	z := &ZkISASha256{}

	tests := []struct {
		name  string
		input []byte
	}{
		{"empty", []byte{}},
		{"hello", []byte("hello world")},
		{"zeros", make([]byte, 64)},
		{"large", bytes.Repeat([]byte("test"), 100)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := z.Execute(tt.input)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if len(output) != 32 {
				t.Fatalf("output len = %d, want 32", len(output))
			}

			// Verify determinism.
			output2, _ := z.Execute(tt.input)
			if !bytes.Equal(output, output2) {
				t.Error("Execute is not deterministic")
			}
		})
	}
}

func TestZkISASha256ProveExecution(t *testing.T) {
	z := &ZkISASha256{}
	input := []byte("test sha256 proof generation")

	proof, err := z.ProveExecution(input)
	if err != nil {
		t.Fatalf("ProveExecution: %v", err)
	}

	if proof.PrecompileAddr != z.Address() {
		t.Errorf("PrecompileAddr = %s, want %s", proof.PrecompileAddr.Hex(), z.Address().Hex())
	}
	if len(proof.Witness) == 0 {
		t.Error("Witness is empty")
	}
	if proof.StepCount == 0 {
		t.Error("StepCount is 0")
	}

	// Verify the proof.
	output, _ := z.Execute(input)
	if !VerifyExecutionProof(proof, input, output) {
		t.Error("VerifyExecutionProof returned false for valid proof")
	}
}

func TestZkISASha256NilInput(t *testing.T) {
	z := &ZkISASha256{}

	// nil input should still work (treated as empty).
	output, err := z.Execute(nil)
	if err != nil {
		t.Fatalf("Execute(nil): %v", err)
	}
	if len(output) != 32 {
		t.Fatalf("output len = %d, want 32", len(output))
	}
}

func TestZkISAModexpExecute(t *testing.T) {
	z := &ZkISAModexp{}

	// Compute 2^10 mod 1000 = 1024 mod 1000 = 24.
	base := big.NewInt(2)
	exp := big.NewInt(10)
	mod := big.NewInt(1000)

	input := buildModexpInput(base, exp, mod)
	output, err := z.Execute(input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	result := new(big.Int).SetBytes(output)
	if result.Int64() != 24 {
		t.Errorf("2^10 mod 1000 = %d, want 24", result.Int64())
	}
}

func TestZkISAModexpZeroMod(t *testing.T) {
	z := &ZkISAModexp{}

	base := big.NewInt(5)
	exp := big.NewInt(3)
	mod := big.NewInt(0)

	input := buildModexpInput(base, exp, mod)
	output, err := z.Execute(input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Mod 0 should return 0 of the appropriate length.
	for _, b := range output {
		if b != 0 {
			t.Error("output should be all zeros for mod=0")
			break
		}
	}
}

func TestZkISAModexpProveExecution(t *testing.T) {
	z := &ZkISAModexp{}

	base := big.NewInt(3)
	exp := big.NewInt(7)
	mod := big.NewInt(100)

	input := buildModexpInput(base, exp, mod)
	proof, err := z.ProveExecution(input)
	if err != nil {
		t.Fatalf("ProveExecution: %v", err)
	}

	output, _ := z.Execute(input)
	if !VerifyExecutionProof(proof, input, output) {
		t.Error("VerifyExecutionProof returned false for valid modexp proof")
	}
}

func TestZkISAModexpNilInput(t *testing.T) {
	z := &ZkISAModexp{}
	_, err := z.Execute(nil)
	if err != ErrZkISANilInput {
		t.Errorf("Execute(nil) error = %v, want %v", err, ErrZkISANilInput)
	}
}

func TestZkISAEcrecoverNilInput(t *testing.T) {
	z := &ZkISAEcrecover{}
	_, err := z.Execute(nil)
	if err != ErrZkISANilInput {
		t.Errorf("Execute(nil) error = %v, want %v", err, ErrZkISANilInput)
	}
}

func TestZkISAEcrecoverInvalidV(t *testing.T) {
	z := &ZkISAEcrecover{}

	// Invalid v value (not 27 or 28).
	input := make([]byte, 128)
	input[63] = 26 // v = 26
	output, err := z.Execute(input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Invalid v should return nil output.
	if output != nil {
		t.Errorf("expected nil output for invalid v, got %x", output)
	}
}

func TestZkISAEcrecoverProveExecution(t *testing.T) {
	z := &ZkISAEcrecover{}

	// Use a zeroed input (will produce a nil/empty output due to invalid sig).
	input := make([]byte, 128)
	input[63] = 27 // valid v but invalid r,s

	proof, err := z.ProveExecution(input)
	if err != nil {
		t.Fatalf("ProveExecution: %v", err)
	}

	if proof.PrecompileAddr != z.Address() {
		t.Errorf("PrecompileAddr mismatch")
	}
	if len(proof.Witness) == 0 {
		t.Error("Witness is empty")
	}
}

func TestZkISABn128AddExecute(t *testing.T) {
	z := &ZkISABn128Add{}

	input := make([]byte, 128)
	// Set some non-zero coordinates.
	input[31] = 1  // x1 = 1
	input[63] = 2  // y1 = 2
	input[95] = 3  // x2 = 3
	input[127] = 4 // y2 = 4

	output, err := z.Execute(input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(output) != 64 {
		t.Fatalf("output len = %d, want 64", len(output))
	}

	// Verify determinism.
	output2, _ := z.Execute(input)
	if !bytes.Equal(output, output2) {
		t.Error("bn128add Execute is not deterministic")
	}
}

func TestZkISABn128AddNilInput(t *testing.T) {
	z := &ZkISABn128Add{}
	_, err := z.Execute(nil)
	if err != ErrZkISANilInput {
		t.Errorf("Execute(nil) error = %v, want %v", err, ErrZkISANilInput)
	}
}

func TestZkISABn128AddProveExecution(t *testing.T) {
	z := &ZkISABn128Add{}

	input := make([]byte, 128)
	input[31] = 5
	input[63] = 6

	proof, err := z.ProveExecution(input)
	if err != nil {
		t.Fatalf("ProveExecution: %v", err)
	}

	output, _ := z.Execute(input)
	if !VerifyExecutionProof(proof, input, output) {
		t.Error("VerifyExecutionProof failed for bn128add proof")
	}
}

func TestZkISABn128MulExecute(t *testing.T) {
	z := &ZkISABn128Mul{}

	input := make([]byte, 96)
	input[31] = 1  // x = 1
	input[63] = 2  // y = 2
	input[95] = 10 // scalar = 10

	output, err := z.Execute(input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(output) != 64 {
		t.Fatalf("output len = %d, want 64", len(output))
	}
}

func TestZkISABn128MulNilInput(t *testing.T) {
	z := &ZkISABn128Mul{}
	_, err := z.Execute(nil)
	if err != ErrZkISANilInput {
		t.Errorf("Execute(nil) error = %v, want %v", err, ErrZkISANilInput)
	}
}

func TestZkISABn128MulProveExecution(t *testing.T) {
	z := &ZkISABn128Mul{}

	input := make([]byte, 96)
	input[31] = 7
	input[63] = 11
	input[95] = 3

	proof, err := z.ProveExecution(input)
	if err != nil {
		t.Fatalf("ProveExecution: %v", err)
	}

	output, _ := z.Execute(input)
	if !VerifyExecutionProof(proof, input, output) {
		t.Error("VerifyExecutionProof failed for bn128mul proof")
	}
}

func TestZkISABn128PairingExecute(t *testing.T) {
	z := &ZkISABn128Pairing{}

	// Empty input (zero pairs): should return success (1).
	output, err := z.Execute([]byte{})
	if err != nil {
		t.Fatalf("Execute(empty): %v", err)
	}
	if len(output) != 32 {
		t.Fatalf("output len = %d, want 32", len(output))
	}
	if output[31] != 1 {
		t.Error("empty pairing should return 1 (success)")
	}
}

func TestZkISABn128PairingOnePair(t *testing.T) {
	z := &ZkISABn128Pairing{}

	input := make([]byte, 192)
	for i := range input {
		input[i] = byte(i % 256)
	}

	output, err := z.Execute(input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(output) != 32 {
		t.Fatalf("output len = %d, want 32", len(output))
	}
}

func TestZkISABn128PairingInvalidLength(t *testing.T) {
	z := &ZkISABn128Pairing{}

	// Not a multiple of 192.
	input := make([]byte, 100)
	_, err := z.Execute(input)
	if err == nil {
		t.Error("expected error for input not multiple of 192")
	}
}

func TestZkISABn128PairingNilInput(t *testing.T) {
	z := &ZkISABn128Pairing{}
	_, err := z.Execute(nil)
	if err != ErrZkISANilInput {
		t.Errorf("Execute(nil) error = %v, want %v", err, ErrZkISANilInput)
	}
}

func TestZkISABn128PairingProveExecution(t *testing.T) {
	z := &ZkISABn128Pairing{}

	input := make([]byte, 192)
	input[0] = 0xab

	proof, err := z.ProveExecution(input)
	if err != nil {
		t.Fatalf("ProveExecution: %v", err)
	}

	output, _ := z.Execute(input)
	if !VerifyExecutionProof(proof, input, output) {
		t.Error("VerifyExecutionProof failed for bn128pairing proof")
	}
}

func TestVerifyExecutionProofNil(t *testing.T) {
	if VerifyExecutionProof(nil, []byte("input"), []byte("output")) {
		t.Error("VerifyExecutionProof(nil) should return false")
	}
}

func TestVerifyExecutionProofEmptyWitness(t *testing.T) {
	proof := &ExecutionProof{
		Witness: nil,
	}
	if VerifyExecutionProof(proof, []byte("input"), []byte("output")) {
		t.Error("VerifyExecutionProof with empty witness should return false")
	}
}

func TestVerifyExecutionProofWrongInput(t *testing.T) {
	z := &ZkISASha256{}
	input := []byte("original input")
	proof, _ := z.ProveExecution(input)

	output, _ := z.Execute(input)
	if VerifyExecutionProof(proof, []byte("wrong input"), output) {
		t.Error("VerifyExecutionProof should fail for wrong input")
	}
}

func TestVerifyExecutionProofWrongOutput(t *testing.T) {
	z := &ZkISASha256{}
	input := []byte("test input for output check")
	proof, _ := z.ProveExecution(input)

	if VerifyExecutionProof(proof, input, []byte("wrong output")) {
		t.Error("VerifyExecutionProof should fail for wrong output")
	}
}

func TestZkISAGasCosts(t *testing.T) {
	tests := []struct {
		name       string
		precompile ZkISAPrecompile
		input      []byte
		minGas     uint64
	}{
		{"ecrecover", &ZkISAEcrecover{}, make([]byte, 128), 3000},
		{"sha256-empty", &ZkISASha256{}, []byte{}, 60},
		{"sha256-32bytes", &ZkISASha256{}, make([]byte, 32), 72},
		{"modexp", &ZkISAModexp{}, make([]byte, 96), 200},
		{"bn128add", &ZkISABn128Add{}, make([]byte, 128), 150},
		{"bn128mul", &ZkISABn128Mul{}, make([]byte, 96), 6000},
		{"bn128pairing-empty", &ZkISABn128Pairing{}, []byte{}, 45000},
		{"bn128pairing-1pair", &ZkISABn128Pairing{}, make([]byte, 192), 79000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gas := tt.precompile.GasCost(tt.input)
			if gas < tt.minGas {
				t.Errorf("GasCost = %d, want >= %d", gas, tt.minGas)
			}
		})
	}
}

func TestZkISAPrecompileNames(t *testing.T) {
	precompiles := []ZkISAPrecompile{
		&ZkISAEcrecover{},
		&ZkISASha256{},
		&ZkISAModexp{},
		&ZkISABn128Add{},
		&ZkISABn128Mul{},
		&ZkISABn128Pairing{},
	}

	names := make(map[string]bool)
	for _, p := range precompiles {
		name := p.Name()
		if name == "" {
			t.Error("precompile has empty name")
		}
		if names[name] {
			t.Errorf("duplicate name: %s", name)
		}
		names[name] = true
	}
}

func TestZkISADifferentInputsDifferentProofs(t *testing.T) {
	z := &ZkISASha256{}

	p1, _ := z.ProveExecution([]byte("input one"))
	p2, _ := z.ProveExecution([]byte("input two"))

	if p1.ProofDigest == p2.ProofDigest {
		t.Error("different inputs should produce different proof digests")
	}
	if p1.InputHash == p2.InputHash {
		t.Error("different inputs should produce different input hashes")
	}
}

func TestVerifyExecutionProofAllPrecompiles(t *testing.T) {
	registry := RegisterZkISAPrecompiles()

	inputs := map[types.Address][]byte{
		types.BytesToAddress([]byte{0x01}): make([]byte, 128),                                             // ecrecover
		types.BytesToAddress([]byte{0x02}): []byte("test data"),                                           // sha256
		types.BytesToAddress([]byte{0x05}): buildModexpInput(big.NewInt(2), big.NewInt(3), big.NewInt(5)), // modexp
		types.BytesToAddress([]byte{0x06}): make([]byte, 128),                                             // bn128add
		types.BytesToAddress([]byte{0x07}): make([]byte, 96),                                              // bn128mul
		types.BytesToAddress([]byte{0x08}): make([]byte, 192),                                             // bn128pairing
	}

	for addr, p := range registry {
		input := inputs[addr]
		if input == nil {
			t.Errorf("no test input for %s", addr.Hex())
			continue
		}

		proof, err := p.ProveExecution(input)
		if err != nil {
			t.Errorf("%s.ProveExecution: %v", p.Name(), err)
			continue
		}

		output, _ := p.Execute(input)
		if output == nil {
			output = []byte{}
		}
		if !VerifyExecutionProof(proof, input, output) {
			t.Errorf("VerifyExecutionProof failed for %s", p.Name())
		}
	}
}

// buildModexpInput constructs a modexp precompile input from base, exp, mod.
func buildModexpInput(base, exp, mod *big.Int) []byte {
	bBytes := base.Bytes()
	eBytes := exp.Bytes()
	mBytes := mod.Bytes()

	// Header: 3 x 32-byte lengths.
	input := make([]byte, 96+len(bBytes)+len(eBytes)+len(mBytes))

	bLen := big.NewInt(int64(len(bBytes)))
	eLen := big.NewInt(int64(len(eBytes)))
	mLen := big.NewInt(int64(len(mBytes)))

	copy(input[32-len(bLen.Bytes()):32], bLen.Bytes())
	copy(input[64-len(eLen.Bytes()):64], eLen.Bytes())
	copy(input[96-len(mLen.Bytes()):96], mLen.Bytes())

	offset := 96
	copy(input[offset:], bBytes)
	offset += len(bBytes)
	copy(input[offset:], eBytes)
	offset += len(eBytes)
	copy(input[offset:], mBytes)

	return input
}
