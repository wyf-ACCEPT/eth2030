package vm

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestAAContextConstants(t *testing.T) {
	if AARoleSenderDeployment != 0xA0 {
		t.Errorf("AARoleSenderDeployment = 0x%02x, want 0xA0", AARoleSenderDeployment)
	}
	if AARoleSenderValidation != 0xA1 {
		t.Errorf("AARoleSenderValidation = 0x%02x, want 0xA1", AARoleSenderValidation)
	}
	if AARolePaymasterValidation != 0xA2 {
		t.Errorf("AARolePaymasterValidation = 0x%02x, want 0xA2", AARolePaymasterValidation)
	}
	if AARoleSenderExecution != 0xA3 {
		t.Errorf("AARoleSenderExecution = 0x%02x, want 0xA3", AARoleSenderExecution)
	}
	if AARolePaymasterPostOp != 0xA4 {
		t.Errorf("AARolePaymasterPostOp = 0x%02x, want 0xA4", AARolePaymasterPostOp)
	}
}

func TestAAOpcodeValues(t *testing.T) {
	if CURRENT_ROLE != 0xab {
		t.Errorf("CURRENT_ROLE = 0x%02x, want 0xab", CURRENT_ROLE)
	}
	if ACCEPT_ROLE != 0xac {
		t.Errorf("ACCEPT_ROLE = 0x%02x, want 0xac", ACCEPT_ROLE)
	}
}

func TestAAEntryPointAddress(t *testing.T) {
	expected := types.HexToAddress("0x0000000000000000000000000000000000007701")
	if AAEntryPointAddress != expected {
		t.Errorf("AAEntryPointAddress = %x, want %x", AAEntryPointAddress, expected)
	}
}

func TestIsValidRole(t *testing.T) {
	tests := []struct {
		role  uint64
		valid bool
	}{
		{AARoleSenderDeployment, true},
		{AARoleSenderValidation, true},
		{AARolePaymasterValidation, true},
		{AARoleSenderExecution, true},
		{AARolePaymasterPostOp, true},
		{0x00, false},
		{0x9F, false},
		{0xA5, false},
		{0xFF, false},
	}

	for _, tt := range tests {
		if got := isValidRole(tt.role); got != tt.valid {
			t.Errorf("isValidRole(0x%02x) = %v, want %v", tt.role, got, tt.valid)
		}
	}
}

func makeTestEVM() *EVM {
	return NewEVM(BlockContext{}, TxContext{}, Config{})
}

func TestOpCurrentRole_NoAAContext(t *testing.T) {
	evm := makeTestEVM()
	stack := NewStack()
	mem := NewMemory()
	var pc uint64

	// No AA context set: should return ROLE_SENDER_EXECUTION.
	_, err := opCurrentRole(&pc, evm, nil, mem, stack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := stack.Pop()
	if result.Uint64() != AARoleSenderExecution {
		t.Errorf("opCurrentRole (no context) = 0x%02x, want 0x%02x", result.Uint64(), AARoleSenderExecution)
	}
}

func TestOpCurrentRole_WithAAContext(t *testing.T) {
	evm := makeTestEVM()
	stack := NewStack()
	mem := NewMemory()
	var pc uint64

	ctx := &AAContext{CurrentRole: AARoleSenderValidation}
	SetAAContext(evm, ctx)
	defer ClearAAContext(evm)

	_, err := opCurrentRole(&pc, evm, nil, mem, stack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := stack.Pop()
	if result.Uint64() != AARoleSenderValidation {
		t.Errorf("opCurrentRole = 0x%02x, want 0x%02x", result.Uint64(), AARoleSenderValidation)
	}
}

func TestOpCurrentRole_RoleTransition(t *testing.T) {
	evm := makeTestEVM()
	stack := NewStack()
	mem := NewMemory()
	var pc uint64

	ctx := &AAContext{CurrentRole: AARoleSenderValidation}
	SetAAContext(evm, ctx)
	defer ClearAAContext(evm)

	// Check initial role.
	opCurrentRole(&pc, evm, nil, mem, stack)
	r1 := stack.Pop()
	if r1.Uint64() != AARoleSenderValidation {
		t.Errorf("initial role = 0x%02x, want 0x%02x", r1.Uint64(), AARoleSenderValidation)
	}

	// Transition to execution.
	ctx.TransitionRole(AARoleSenderExecution)

	opCurrentRole(&pc, evm, nil, mem, stack)
	r2 := stack.Pop()
	if r2.Uint64() != AARoleSenderExecution {
		t.Errorf("after transition = 0x%02x, want 0x%02x", r2.Uint64(), AARoleSenderExecution)
	}
}

func TestOpAcceptRole_Success(t *testing.T) {
	evm := makeTestEVM()
	stack := NewStack()
	mem := NewMemory()
	var pc uint64

	ctx := &AAContext{CurrentRole: AARoleSenderValidation}
	SetAAContext(evm, ctx)
	defer ClearAAContext(evm)

	// Write some return data to memory.
	mem.Resize(64)
	mem.Set(0, 4, []byte{0xDE, 0xAD, 0xBE, 0xEF})

	// Push stack: frame_role, offset, length.
	stack.Push(new(big.Int).SetUint64(4))                       // length
	stack.Push(new(big.Int).SetUint64(0))                       // offset
	stack.Push(new(big.Int).SetUint64(AARoleSenderValidation))  // frame_role

	ret, err := opAcceptRole(&pc, evm, nil, mem, stack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !ctx.RoleAccepted {
		t.Error("role should be accepted")
	}

	if len(ret) != 4 || ret[0] != 0xDE || ret[1] != 0xAD || ret[2] != 0xBE || ret[3] != 0xEF {
		t.Errorf("return data = %x, want DEADBEEF", ret)
	}
}

func TestOpAcceptRole_RoleMismatch(t *testing.T) {
	evm := makeTestEVM()
	stack := NewStack()
	mem := NewMemory()
	var pc uint64

	ctx := &AAContext{CurrentRole: AARoleSenderValidation}
	SetAAContext(evm, ctx)
	defer ClearAAContext(evm)

	mem.Resize(32)

	// Push wrong role.
	stack.Push(new(big.Int).SetUint64(0))                      // length
	stack.Push(new(big.Int).SetUint64(0))                      // offset
	stack.Push(new(big.Int).SetUint64(AARoleSenderExecution))  // wrong role

	_, err := opAcceptRole(&pc, evm, nil, mem, stack)
	if err != ErrRoleMismatch {
		t.Errorf("expected ErrRoleMismatch, got %v", err)
	}

	if ctx.RoleAccepted {
		t.Error("role should not be accepted")
	}
}

func TestOpAcceptRole_AlreadyAccepted(t *testing.T) {
	evm := makeTestEVM()
	stack := NewStack()
	mem := NewMemory()
	var pc uint64

	ctx := &AAContext{
		CurrentRole:  AARoleSenderValidation,
		RoleAccepted: true, // already accepted
	}
	SetAAContext(evm, ctx)
	defer ClearAAContext(evm)

	mem.Resize(32)

	stack.Push(new(big.Int).SetUint64(0))
	stack.Push(new(big.Int).SetUint64(0))
	stack.Push(new(big.Int).SetUint64(AARoleSenderValidation))

	_, err := opAcceptRole(&pc, evm, nil, mem, stack)
	if err != ErrRoleAlreadyAccepted {
		t.Errorf("expected ErrRoleAlreadyAccepted, got %v", err)
	}
}

func TestOpAcceptRole_NoAAContext(t *testing.T) {
	evm := makeTestEVM()
	stack := NewStack()
	mem := NewMemory()
	var pc uint64

	mem.Resize(32)

	stack.Push(new(big.Int).SetUint64(0))
	stack.Push(new(big.Int).SetUint64(0))
	stack.Push(new(big.Int).SetUint64(AARoleSenderValidation))

	_, err := opAcceptRole(&pc, evm, nil, mem, stack)
	if err != ErrNoAAContext {
		t.Errorf("expected ErrNoAAContext, got %v", err)
	}
}

func TestAAContext_TransitionRole(t *testing.T) {
	ctx := &AAContext{
		CurrentRole:  AARoleSenderValidation,
		RoleAccepted: true,
	}

	ctx.TransitionRole(AARoleSenderExecution)

	if ctx.CurrentRole != AARoleSenderExecution {
		t.Errorf("CurrentRole = 0x%02x, want 0x%02x", ctx.CurrentRole, AARoleSenderExecution)
	}
	if ctx.RoleAccepted {
		t.Error("RoleAccepted should be reset to false")
	}
}

func TestAAContextRegistry(t *testing.T) {
	evm1 := makeTestEVM()
	evm2 := makeTestEVM()

	ctx1 := &AAContext{CurrentRole: AARoleSenderValidation}
	ctx2 := &AAContext{CurrentRole: AARolePaymasterPostOp}

	// Initially empty.
	if got := GetAAContext(evm1); got != nil {
		t.Error("expected nil for unregistered EVM")
	}

	// Set and get.
	SetAAContext(evm1, ctx1)
	SetAAContext(evm2, ctx2)

	if got := GetAAContext(evm1); got != ctx1 {
		t.Error("got wrong context for evm1")
	}
	if got := GetAAContext(evm2); got != ctx2 {
		t.Error("got wrong context for evm2")
	}

	// Clear.
	ClearAAContext(evm1)
	if got := GetAAContext(evm1); got != nil {
		t.Error("expected nil after clear")
	}
	if got := GetAAContext(evm2); got != ctx2 {
		t.Error("evm2 should be unaffected by evm1 clear")
	}

	ClearAAContext(evm2)
}

func TestNewAAContext(t *testing.T) {
	sender := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	paymaster := types.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	deployer := types.HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc")

	tx := &types.AATx{
		ChainID:                big.NewInt(1),
		Nonce:                  42,
		Sender:                 sender,
		SenderValidationData:   []byte{0x01},
		SenderExecutionData:    []byte{0x02},
		Paymaster:              &paymaster,
		PaymasterData:          []byte{0x03},
		Deployer:               &deployer,
		DeployerData:           []byte{0x04},
		MaxPriorityFeePerGas:   big.NewInt(1_000_000_000),
		MaxFeePerGas:           big.NewInt(30_000_000_000),
		SenderValidationGas:    100_000,
		PaymasterValidationGas: 50_000,
		SenderExecutionGas:     200_000,
		PaymasterPostOpGas:     20_000,
	}

	sigHash := types.Hash{0xFF}
	ctx := NewAAContext(tx, sigHash)

	if ctx.CurrentRole != AARoleSenderValidation {
		t.Errorf("initial role should be SenderValidation")
	}
	if ctx.RoleAccepted {
		t.Error("role should not be accepted initially")
	}
	if ctx.Sender != sender {
		t.Error("sender mismatch")
	}
	if ctx.Paymaster != paymaster {
		t.Error("paymaster mismatch")
	}
	if ctx.Deployer != deployer {
		t.Error("deployer mismatch")
	}
	if ctx.Nonce != 42 {
		t.Errorf("nonce = %d, want 42", ctx.Nonce)
	}
	if ctx.TxSigHash != sigHash {
		t.Error("sig hash mismatch")
	}
	if ctx.SenderValidationGas != 100_000 {
		t.Errorf("SenderValidationGas = %d", ctx.SenderValidationGas)
	}
	if ctx.MaxFeePerGas.Cmp(big.NewInt(30_000_000_000)) != 0 {
		t.Error("MaxFeePerGas mismatch")
	}
}

func TestEIP7701Operations(t *testing.T) {
	ops := EIP7701Operations()

	// Check CURRENT_ROLE operation.
	crOp, ok := ops[CURRENT_ROLE]
	if !ok {
		t.Fatal("CURRENT_ROLE operation not found")
	}
	if crOp.execute == nil {
		t.Error("CURRENT_ROLE execute is nil")
	}
	if crOp.minStack != 0 {
		t.Errorf("CURRENT_ROLE minStack = %d, want 0", crOp.minStack)
	}
	if crOp.maxStack != 1023 {
		t.Errorf("CURRENT_ROLE maxStack = %d, want 1023", crOp.maxStack)
	}
	if crOp.halts {
		t.Error("CURRENT_ROLE should not halt")
	}

	// Check ACCEPT_ROLE operation.
	arOp, ok := ops[ACCEPT_ROLE]
	if !ok {
		t.Fatal("ACCEPT_ROLE operation not found")
	}
	if arOp.execute == nil {
		t.Error("ACCEPT_ROLE execute is nil")
	}
	if arOp.minStack != 3 {
		t.Errorf("ACCEPT_ROLE minStack = %d, want 3", arOp.minStack)
	}
	if !arOp.halts {
		t.Error("ACCEPT_ROLE should halt (like RETURN)")
	}
	if arOp.memorySize == nil {
		t.Error("ACCEPT_ROLE should have memorySize function")
	}
}

func TestMemoryAcceptRole(t *testing.T) {
	stack := NewStack()

	// Stack layout for ACCEPT_ROLE: [frame_role, offset, length]
	// Back(0) = frame_role, Back(1) = offset, Back(2) = length
	stack.Push(new(big.Int).SetUint64(32))                     // length (Back(2))
	stack.Push(new(big.Int).SetUint64(64))                     // offset (Back(1))
	stack.Push(new(big.Int).SetUint64(AARoleSenderValidation)) // frame_role (Back(0))

	size, overflow := memoryAcceptRole(stack)
	if overflow {
		t.Fatal("memoryAcceptRole returned overflow")
	}
	// offset(64) + length(32) = 96
	if size != 96 {
		t.Errorf("memoryAcceptRole = %d, want 96", size)
	}
}
