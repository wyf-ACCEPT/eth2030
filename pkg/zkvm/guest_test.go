package zkvm

import (
	"bytes"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestNewGuestContextDefaults(t *testing.T) {
	stateRoot := types.Hash{0x01}
	witness := []byte("witness-data")

	ctx := NewGuestContext(stateRoot, witness)
	if ctx == nil {
		t.Fatal("NewGuestContext returned nil")
	}
	if ctx.StateRoot() != stateRoot {
		t.Errorf("StateRoot() = %x, want %x", ctx.StateRoot(), stateRoot)
	}
	if !bytes.Equal(ctx.Witness(), witness) {
		t.Error("Witness() mismatch")
	}
	if ctx.ChainID() != 0 {
		t.Errorf("ChainID() = %d, want 0", ctx.ChainID())
	}
	if ctx.IsExecuted() {
		t.Error("new context should not be executed")
	}
}

func TestNewGuestContextNilWitness(t *testing.T) {
	ctx := NewGuestContext(types.Hash{}, nil)
	if ctx == nil {
		t.Fatal("NewGuestContext returned nil with nil witness")
	}
	if ctx.Witness() != nil {
		t.Error("expected nil witness")
	}
}

func TestNewGuestContextWithChainID(t *testing.T) {
	stateRoot := types.Hash{0xAA}
	witness := []byte("w")

	ctx := NewGuestContextWithChain(stateRoot, witness, 1337)
	if ctx.ChainID() != 1337 {
		t.Errorf("ChainID() = %d, want 1337", ctx.ChainID())
	}
	if ctx.StateRoot() != stateRoot {
		t.Error("state root mismatch")
	}
	if !bytes.Equal(ctx.Witness(), witness) {
		t.Error("witness mismatch")
	}
}

func TestNewGuestContextWithChainZero(t *testing.T) {
	ctx := NewGuestContextWithChain(types.Hash{}, nil, 0)
	if ctx.ChainID() != 0 {
		t.Errorf("ChainID() = %d, want 0", ctx.ChainID())
	}
}

func TestExecuteBlockSetsExecutedFlag(t *testing.T) {
	ctx := NewGuestContext(types.Hash{0x01}, []byte("w"))
	if ctx.IsExecuted() {
		t.Fatal("context should not be executed initially")
	}

	_, err := ExecuteBlock(ctx, []byte("block"))
	if err != nil {
		t.Fatalf("ExecuteBlock: %v", err)
	}

	if !ctx.IsExecuted() {
		t.Error("context should be marked executed after ExecuteBlock")
	}
}

func TestExecuteBlockNilContextError(t *testing.T) {
	_, err := ExecuteBlock(nil, []byte("block"))
	if err != ErrNilGuestContext {
		t.Errorf("expected ErrNilGuestContext, got %v", err)
	}
}

func TestExecuteBlockEmptyBlockData(t *testing.T) {
	ctx := NewGuestContext(types.Hash{}, nil)

	_, err := ExecuteBlock(ctx, nil)
	if err != ErrEmptyBlockData {
		t.Errorf("nil block data: expected ErrEmptyBlockData, got %v", err)
	}

	// Should not mark context as executed on error.
	if ctx.IsExecuted() {
		t.Error("context should not be executed after error")
	}

	_, err = ExecuteBlock(ctx, []byte{})
	if err != ErrEmptyBlockData {
		t.Errorf("empty block data: expected ErrEmptyBlockData, got %v", err)
	}
}

func TestExecuteBlockDoubleExecutionFails(t *testing.T) {
	ctx := NewGuestContext(types.Hash{0x10}, []byte("witness"))

	_, err := ExecuteBlock(ctx, []byte("block1"))
	if err != nil {
		t.Fatalf("first execution: %v", err)
	}

	_, err = ExecuteBlock(ctx, []byte("block2"))
	if err == nil {
		t.Error("second execution should return error")
	}
}

func TestExecuteBlockDeterministicOutput(t *testing.T) {
	stateRoot := types.Hash{0xBB}
	witness := []byte("deterministic-witness")
	block := []byte("deterministic-block")

	ctx1 := NewGuestContext(stateRoot, witness)
	ctx2 := NewGuestContext(stateRoot, witness)

	r1, err1 := ExecuteBlock(ctx1, block)
	r2, err2 := ExecuteBlock(ctx2, block)

	if err1 != nil || err2 != nil {
		t.Fatalf("unexpected errors: %v, %v", err1, err2)
	}

	if r1 != r2 {
		t.Errorf("deterministic output mismatch: %x vs %x", r1, r2)
	}
}

func TestExecuteBlockDifferentInputsDifferentOutputs(t *testing.T) {
	ctx1 := NewGuestContext(types.Hash{0x01}, []byte("w1"))
	ctx2 := NewGuestContext(types.Hash{0x02}, []byte("w2"))

	r1, _ := ExecuteBlock(ctx1, []byte("block"))
	r2, _ := ExecuteBlock(ctx2, []byte("block"))

	if r1 == r2 {
		t.Error("different state roots should produce different post-states")
	}
}

func TestExecuteBlockDifferentBlockData(t *testing.T) {
	ctx1 := NewGuestContext(types.Hash{0x01}, []byte("w"))
	ctx2 := NewGuestContext(types.Hash{0x01}, []byte("w"))

	r1, _ := ExecuteBlock(ctx1, []byte("block-a"))
	r2, _ := ExecuteBlock(ctx2, []byte("block-b"))

	if r1 == r2 {
		t.Error("different block data should produce different post-states")
	}
}

func TestExecuteBlockPostStateNonZero(t *testing.T) {
	ctx := NewGuestContext(types.Hash{0xFF}, []byte("witness"))
	result, err := ExecuteBlock(ctx, []byte("block"))
	if err != nil {
		t.Fatalf("ExecuteBlock: %v", err)
	}

	if result == (types.Hash{}) {
		t.Error("expected non-zero post-state root")
	}
}

func TestExecuteBlockPostStateDiffersFromPre(t *testing.T) {
	stateRoot := types.Hash{0xAA, 0xBB}
	ctx := NewGuestContext(stateRoot, []byte("w"))
	result, _ := ExecuteBlock(ctx, []byte("block"))

	if result == stateRoot {
		t.Error("post-state should differ from pre-state")
	}
}

func TestExecuteBlockFullSuccess(t *testing.T) {
	stateRoot := types.Hash{0xCC}
	ctx := NewGuestContext(stateRoot, []byte("witness"))

	result, err := ExecuteBlockFull(ctx, []byte("block"))
	if err != nil {
		t.Fatalf("ExecuteBlockFull: %v", err)
	}

	if !result.Success {
		t.Error("expected Success = true")
	}
	if result.PreStateRoot != stateRoot {
		t.Error("pre-state root mismatch")
	}
	if result.PostStateRoot == (types.Hash{}) {
		t.Error("expected non-zero post-state root")
	}
	if result.ReceiptsRoot == (types.Hash{}) {
		t.Error("expected non-zero receipts root")
	}
	if result.GasUsed == 0 {
		t.Error("expected non-zero gas used")
	}
}

func TestExecuteBlockFullFailurePreservesPreState(t *testing.T) {
	ctx := NewGuestContext(types.Hash{0xDD}, nil)
	result, err := ExecuteBlockFull(ctx, nil)

	if err == nil {
		t.Error("expected error for nil block data")
	}
	if result == nil {
		t.Fatal("expected non-nil result even on failure")
	}
	if result.Success {
		t.Error("expected Success = false on failure")
	}
	if result.PreStateRoot != (types.Hash{0xDD}) {
		t.Error("pre-state root should be set even on failure")
	}
}

func TestExecuteBlockFullReceiptsRootDeterministic(t *testing.T) {
	stateRoot := types.Hash{0xEE}
	witness := []byte("w")
	block := []byte("block")

	ctx1 := NewGuestContext(stateRoot, witness)
	ctx2 := NewGuestContext(stateRoot, witness)

	r1, _ := ExecuteBlockFull(ctx1, block)
	r2, _ := ExecuteBlockFull(ctx2, block)

	if r1.ReceiptsRoot != r2.ReceiptsRoot {
		t.Error("receipts root should be deterministic")
	}
	if r1.PostStateRoot != r2.PostStateRoot {
		t.Error("post-state root should be deterministic")
	}
}

func TestGuestErrorMessages(t *testing.T) {
	if ErrNilGuestContext.Error() != "zkvm: nil guest context" {
		t.Errorf("unexpected error message: %s", ErrNilGuestContext.Error())
	}
	if ErrEmptyBlockData.Error() != "zkvm: empty block data" {
		t.Errorf("unexpected error message: %s", ErrEmptyBlockData.Error())
	}
	if ErrGuestPanicked.Error() != "zkvm: guest execution panicked" {
		t.Errorf("unexpected error message: %s", ErrGuestPanicked.Error())
	}
}
