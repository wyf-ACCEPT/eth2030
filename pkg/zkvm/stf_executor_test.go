package zkvm

import (
	"encoding/binary"
	"errors"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// makeSTFTestBlock creates a minimal block with the given number of transactions.
func makeSTFExecTestBlock(numTx int) *types.Block {
	header := &types.Header{
		Number:     big.NewInt(1),
		Difficulty: big.NewInt(1),
		GasLimit:   30_000_000,
		Time:       1700000000,
		Extra:      []byte("stf_executor_test"),
	}
	txs := make([]*types.Transaction, numTx)
	for i := range txs {
		to := types.Address{byte(i + 1)}
		txs[i] = types.NewTransaction(&types.LegacyTx{
			Nonce:    uint64(i),
			To:       &to,
			Value:    big.NewInt(1000),
			Gas:      21000,
			GasPrice: big.NewInt(1),
		})
	}
	body := &types.Body{Transactions: txs}
	return types.NewBlock(header, body)
}

// buildSTFExecTestProgram builds a minimal RISC-V program for STF testing.
func buildSTFExecTestProgram() []byte {
	instrs := []uint32{
		EncodeIType(0x13, 17, 0, 0, 0), // a7 = 0 (halt)
		EncodeIType(0x13, 10, 0, 0, 0), // a0 = 0 (exit code)
		0x00000073,                     // ECALL
	}
	code := make([]byte, len(instrs)*4)
	for i, instr := range instrs {
		binary.LittleEndian.PutUint32(code[i*4:], instr)
	}
	return code
}

func TestRealSTFExec_NewRealSTFExecutor(t *testing.T) {
	reg := NewGuestRegistry()
	config := DefaultRealSTFConfig()
	exec, err := NewRealSTFExecutor(config, reg)
	if err != nil {
		t.Fatalf("NewRealSTFExecutor: %v", err)
	}
	if exec == nil {
		t.Fatal("executor is nil")
	}
}

func TestRealSTFExec_NilRegistry(t *testing.T) {
	config := DefaultRealSTFConfig()
	_, err := NewRealSTFExecutor(config, nil)
	if !errors.Is(err, ErrRealSTFNilRegistry) {
		t.Fatalf("expected ErrRealSTFNilRegistry, got %v", err)
	}
}

func TestRealSTFExec_RegisterSTFProgram(t *testing.T) {
	reg := NewGuestRegistry()
	config := DefaultRealSTFConfig()
	exec, err := NewRealSTFExecutor(config, reg)
	if err != nil {
		t.Fatalf("NewRealSTFExecutor: %v", err)
	}

	program := buildSTFExecTestProgram()
	id, err := exec.RegisterSTFProgram(program)
	if err != nil {
		t.Fatalf("RegisterSTFProgram: %v", err)
	}
	if id == (types.Hash{}) {
		t.Fatal("program ID is zero hash")
	}

	// Re-register same program should succeed (already registered).
	id2, err := exec.RegisterSTFProgram(program)
	if err != nil {
		t.Fatalf("re-register: %v", err)
	}
	if id != id2 {
		t.Error("program IDs should match on re-registration")
	}
}

func TestRealSTFExec_ExecuteSTF(t *testing.T) {
	reg := NewGuestRegistry()
	config := DefaultRealSTFConfig()
	exec, err := NewRealSTFExecutor(config, reg)
	if err != nil {
		t.Fatalf("NewRealSTFExecutor: %v", err)
	}

	program := buildSTFExecTestProgram()
	if _, err := exec.RegisterSTFProgram(program); err != nil {
		t.Fatalf("RegisterSTFProgram: %v", err)
	}

	block := makeSTFExecTestBlock(2)
	preState := types.Hash{0xAA}

	// Generate the witness from the block.
	stfInput, err := exec.GenerateSTFWitness(preState, block)
	if err != nil {
		t.Fatalf("GenerateSTFWitness: %v", err)
	}

	output, err := exec.ExecuteSTF(stfInput)
	if err != nil {
		t.Fatalf("ExecuteSTF: %v", err)
	}
	if !output.Valid {
		t.Error("expected valid transition")
	}
	if output.PostRoot == (types.Hash{}) {
		t.Error("post root should not be zero")
	}
	if output.CycleCount == 0 {
		t.Error("cycle count should be > 0")
	}
	if len(output.ProofData) == 0 {
		t.Error("proof data should not be empty")
	}
}

func TestRealSTFExec_ExecuteSTFMismatch(t *testing.T) {
	reg := NewGuestRegistry()
	config := DefaultRealSTFConfig()
	exec, err := NewRealSTFExecutor(config, reg)
	if err != nil {
		t.Fatalf("NewRealSTFExecutor: %v", err)
	}

	program := buildSTFExecTestProgram()
	if _, err := exec.RegisterSTFProgram(program); err != nil {
		t.Fatalf("RegisterSTFProgram: %v", err)
	}

	block := makeSTFExecTestBlock(1)
	preState := types.Hash{0xBB}

	stfInput, err := exec.GenerateSTFWitness(preState, block)
	if err != nil {
		t.Fatalf("GenerateSTFWitness: %v", err)
	}

	// Tamper with the post-state root.
	stfInput.PostStateRoot = types.Hash{0xFF}

	output, err := exec.ExecuteSTF(stfInput)
	if !errors.Is(err, ErrRealSTFRootMismatch) {
		t.Fatalf("expected ErrRealSTFRootMismatch, got %v", err)
	}
	if output.Valid {
		t.Error("output should be invalid for mismatched root")
	}
}

func TestRealSTFExec_ExecuteSTFNilInput(t *testing.T) {
	reg := NewGuestRegistry()
	config := DefaultRealSTFConfig()
	exec, err := NewRealSTFExecutor(config, reg)
	if err != nil {
		t.Fatalf("NewRealSTFExecutor: %v", err)
	}

	_, err = exec.ExecuteSTF(nil)
	if !errors.Is(err, ErrRealSTFNilInput) {
		t.Fatalf("expected ErrRealSTFNilInput, got %v", err)
	}
}

func TestRealSTFExec_NoSTFProgram(t *testing.T) {
	reg := NewGuestRegistry()
	config := DefaultRealSTFConfig()
	exec, err := NewRealSTFExecutor(config, reg)
	if err != nil {
		t.Fatalf("NewRealSTFExecutor: %v", err)
	}

	block := makeSTFExecTestBlock(1)
	stfInput, err := exec.GenerateSTFWitness(types.Hash{}, block)
	if err != nil {
		t.Fatalf("GenerateSTFWitness: %v", err)
	}

	_, err = exec.ExecuteSTF(stfInput)
	if !errors.Is(err, ErrRealSTFNoSTFProgram) {
		t.Fatalf("expected ErrRealSTFNoSTFProgram, got %v", err)
	}
}

func TestRealSTFExec_VerifySTFProof(t *testing.T) {
	reg := NewGuestRegistry()
	config := DefaultRealSTFConfig()
	exec, err := NewRealSTFExecutor(config, reg)
	if err != nil {
		t.Fatalf("NewRealSTFExecutor: %v", err)
	}

	program := buildSTFExecTestProgram()
	if _, err := exec.RegisterSTFProgram(program); err != nil {
		t.Fatalf("RegisterSTFProgram: %v", err)
	}

	block := makeSTFExecTestBlock(2)
	preState := types.Hash{0xCC}

	stfInput, err := exec.GenerateSTFWitness(preState, block)
	if err != nil {
		t.Fatalf("GenerateSTFWitness: %v", err)
	}

	output, err := exec.ExecuteSTF(stfInput)
	if err != nil {
		t.Fatalf("ExecuteSTF: %v", err)
	}

	if err := exec.VerifySTFProof(output); err != nil {
		t.Fatalf("VerifySTFProof: %v", err)
	}
}

func TestRealSTFExec_EncodeDecodeSTFInput(t *testing.T) {
	block := makeSTFExecTestBlock(2)
	preState := types.Hash{0xDD}

	input := &STFInput{
		PreStateRoot:  preState,
		PostStateRoot: types.Hash{0xEE},
		BlockHeader:   block.Header(),
		Transactions:  block.Transactions(),
		Witnesses:     [][]byte{{1, 2, 3}, {4, 5, 6}},
	}

	encoded := encodeSTFInput(input)
	if len(encoded) == 0 {
		t.Fatal("encoded data is empty")
	}

	pre, post, err := decodeSTFPublicInputs(encoded)
	if err != nil {
		t.Fatalf("decodeSTFPublicInputs: %v", err)
	}
	if pre != preState {
		t.Errorf("pre state root mismatch")
	}
	if post != (types.Hash{0xEE}) {
		t.Errorf("post state root mismatch")
	}
}

func TestRealSTFExec_ComputeSTFCommitment(t *testing.T) {
	pre := types.Hash{0x01}
	post := types.Hash{0x02}
	blockHash := types.Hash{0x03}

	c1 := ComputeSTFCommitment(pre, post, blockHash)
	c2 := ComputeSTFCommitment(pre, post, blockHash)
	if c1 != c2 {
		t.Error("commitment should be deterministic")
	}

	// Different inputs should produce different commitments.
	c3 := ComputeSTFCommitment(pre, types.Hash{0xFF}, blockHash)
	if c1 == c3 {
		t.Error("different inputs should produce different commitments")
	}
}

func TestRealSTFExec_GenerateSTFWitness(t *testing.T) {
	reg := NewGuestRegistry()
	config := DefaultRealSTFConfig()
	exec, err := NewRealSTFExecutor(config, reg)
	if err != nil {
		t.Fatalf("NewRealSTFExecutor: %v", err)
	}

	block := makeSTFExecTestBlock(3)
	preState := types.Hash{0xAA}

	input, err := exec.GenerateSTFWitness(preState, block)
	if err != nil {
		t.Fatalf("GenerateSTFWitness: %v", err)
	}
	if input.PreStateRoot != preState {
		t.Error("pre-state root mismatch")
	}
	if len(input.Transactions) != 3 {
		t.Errorf("transaction count: got %d, want 3", len(input.Transactions))
	}
	if len(input.Witnesses) != 3 {
		t.Errorf("witness count: got %d, want 3", len(input.Witnesses))
	}

	// Verify the post-state root is correctly computed.
	expectedPost := computePostStateRoot(preState, input.Transactions, input.Witnesses)
	if input.PostStateRoot != expectedPost {
		t.Error("post-state root does not match expected computation")
	}
}

func TestRealSTFExec_GenerateSTFWitnessNilBlock(t *testing.T) {
	reg := NewGuestRegistry()
	config := DefaultRealSTFConfig()
	exec, err := NewRealSTFExecutor(config, reg)
	if err != nil {
		t.Fatalf("NewRealSTFExecutor: %v", err)
	}

	_, err = exec.GenerateSTFWitness(types.Hash{}, nil)
	if !errors.Is(err, ErrRealSTFNilBlock) {
		t.Fatalf("expected ErrRealSTFNilBlock, got %v", err)
	}
}

func TestRealSTFExec_DefaultConfig(t *testing.T) {
	config := DefaultRealSTFConfig()
	if config.GasLimit == 0 {
		t.Error("GasLimit should be > 0")
	}
	if config.MaxWitnessSize == 0 {
		t.Error("MaxWitnessSize should be > 0")
	}
	if config.ProofSystem == "" {
		t.Error("ProofSystem should not be empty")
	}
}

// Ensure the crypto import is used.
var _ = crypto.Keccak256

func TestRealSTFExec_FullRoundTrip(t *testing.T) {
	// Full round-trip: state transition -> witness -> proof -> verify.
	reg := NewGuestRegistry()
	config := DefaultRealSTFConfig()
	exec, err := NewRealSTFExecutor(config, reg)
	if err != nil {
		t.Fatalf("NewRealSTFExecutor: %v", err)
	}

	program := buildSTFExecTestProgram()
	if _, err := exec.RegisterSTFProgram(program); err != nil {
		t.Fatalf("RegisterSTFProgram: %v", err)
	}

	t.Run("basic round-trip", func(t *testing.T) {
		block := makeSTFExecTestBlock(3)
		preState := types.Hash{0xDD}

		// Step 1: Generate witness.
		stfInput, err := exec.GenerateSTFWitness(preState, block)
		if err != nil {
			t.Fatalf("GenerateSTFWitness: %v", err)
		}

		// Step 2: Execute and produce proof.
		output, err := exec.ExecuteSTF(stfInput)
		if err != nil {
			t.Fatalf("ExecuteSTF: %v", err)
		}
		if !output.Valid {
			t.Error("expected valid transition")
		}
		if len(output.ProofData) == 0 {
			t.Fatal("proof data should not be empty")
		}

		// Step 3: Verify the proof.
		if err := exec.VerifySTFProof(output); err != nil {
			t.Fatalf("VerifySTFProof: %v", err)
		}
	})

	t.Run("commitment determinism", func(t *testing.T) {
		block := makeSTFExecTestBlock(2)
		preState := types.Hash{0xEE}

		stfInput, _ := exec.GenerateSTFWitness(preState, block)
		c1 := ComputeSTFCommitment(stfInput.PreStateRoot, stfInput.PostStateRoot, block.Header().Hash())
		c2 := ComputeSTFCommitment(stfInput.PreStateRoot, stfInput.PostStateRoot, block.Header().Hash())
		if c1 != c2 {
			t.Error("commitment should be deterministic")
		}
	})

	t.Run("tampered proof fails verification", func(t *testing.T) {
		block := makeSTFExecTestBlock(1)
		preState := types.Hash{0xFF}

		stfInput, _ := exec.GenerateSTFWitness(preState, block)
		output, err := exec.ExecuteSTF(stfInput)
		if err != nil {
			t.Fatalf("ExecuteSTF: %v", err)
		}
		// Tamper with the proof.
		output.ProofData[0] ^= 0xFF
		if err := exec.VerifySTFProof(output); err == nil {
			t.Error("expected error for tampered proof")
		}
	})
}

func TestValidateSTFInput(t *testing.T) {
	// Nil input.
	if err := ValidateSTFInput(nil); err == nil {
		t.Fatal("expected error for nil input")
	}

	// Nil block header.
	input := &STFInput{
		PreStateRoot: types.Hash{0x01},
		Transactions: []*types.Transaction{{}},
	}
	if err := ValidateSTFInput(input); err == nil {
		t.Fatal("expected error for nil block header")
	}

	// Zero pre-state root.
	input.BlockHeader = &types.Header{}
	input.PreStateRoot = types.Hash{}
	if err := ValidateSTFInput(input); err == nil {
		t.Fatal("expected error for zero pre-state root")
	}

	// Valid input.
	input.PreStateRoot = types.Hash{0x01}
	if err := ValidateSTFInput(input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
