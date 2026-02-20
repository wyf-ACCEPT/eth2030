// evm_stack_ops.go implements stack manipulation operations for the EVM:
// DUP1-DUP16, SWAP1-SWAP16, PUSH0-PUSH32, with full stack bound validation,
// gas cost calculation, and call-context depth tracking.
package vm

import (
	"errors"
	"fmt"
	"math/big"
)

// Stack operation errors.
var (
	ErrStackOpOverflow       = errors.New("stack op: push would exceed 1024 limit")
	ErrStackOpUnderflow      = errors.New("stack op: insufficient items on stack")
	ErrStackOpDupOutOfBounds = errors.New("stack op: DUP position exceeds stack depth")
	ErrStackOpSwapOutOfBounds = errors.New("stack op: SWAP position exceeds stack depth")
	ErrStackOpPushTooLarge   = errors.New("stack op: push size exceeds 32 bytes")
	ErrStackOpInvalidPC      = errors.New("stack op: program counter out of code bounds")
)

// StackOpConfig holds gas cost and limit parameters for stack operations.
type StackOpConfig struct {
	GasPush0    uint64 // gas for PUSH0 (EIP-3855): 2
	GasPushN    uint64 // gas for PUSH1-PUSH32: 3
	GasDupN     uint64 // gas for DUP1-DUP16: 3
	GasSwapN    uint64 // gas for SWAP1-SWAP16: 3
	MaxStack    int    // maximum stack depth (1024)
	MaxPushSize int    // maximum push operand size (32)
}

// DefaultStackOpConfig returns a StackOpConfig with standard Ethereum gas costs.
func DefaultStackOpConfig() StackOpConfig {
	return StackOpConfig{
		GasPush0:    GasPush0,  // 2
		GasPushN:    GasPush,   // 3
		GasDupN:     GasDup,    // 3
		GasSwapN:    GasSwap,   // 3
		MaxStack:    1024,
		MaxPushSize: 32,
	}
}

// StackOpExecutor executes DUP, SWAP, and PUSH operations against an EVMStack
// with explicit error checking and gas cost tracking. It is designed for use
// in validation, debugging, and standalone execution contexts.
type StackOpExecutor struct {
	Config StackOpConfig
}

// NewStackOpExecutor creates a StackOpExecutor with default gas costs.
func NewStackOpExecutor() *StackOpExecutor {
	return &StackOpExecutor{Config: DefaultStackOpConfig()}
}

// ExecPush0 pushes a zero value onto the stack (EIP-3855, Shanghai).
// Gas cost: GasPush0 (2).
func (e *StackOpExecutor) ExecPush0(stack *EVMStack) (uint64, error) {
	if stack.Len() >= e.Config.MaxStack {
		return 0, fmt.Errorf("%w: depth %d", ErrStackOpOverflow, stack.Len())
	}
	if err := stack.Push(new(big.Int)); err != nil {
		return 0, err
	}
	return e.Config.GasPush0, nil
}

// ExecPushN pushes n bytes from the code at the given offset onto the stack.
// n must be in [1, 32]. Bytes beyond the end of code are treated as zero.
// Gas cost: GasPushN (3).
func (e *StackOpExecutor) ExecPushN(stack *EVMStack, code []byte, offset uint64, n int) (uint64, error) {
	if n < 1 || n > e.Config.MaxPushSize {
		return 0, fmt.Errorf("%w: size %d", ErrStackOpPushTooLarge, n)
	}
	if stack.Len() >= e.Config.MaxStack {
		return 0, fmt.Errorf("%w: depth %d", ErrStackOpOverflow, stack.Len())
	}

	// Extract n bytes from code starting at offset+1 (byte after the opcode).
	start := offset + 1
	data := make([]byte, n)
	codeLen := uint64(len(code))
	if start < codeLen {
		end := start + uint64(n)
		if end > codeLen {
			end = codeLen
		}
		copy(data, code[start:end])
		// Remaining bytes are already zero from make().
	}

	val := new(big.Int).SetBytes(data)
	if err := stack.Push(val); err != nil {
		return 0, err
	}
	return e.Config.GasPushN, nil
}

// ExecDup duplicates the nth element from the top of the stack.
// n must be in [1, 16] (DUP1 = copy top, DUP16 = copy 16th from top).
// Gas cost: GasDupN (3).
func (e *StackOpExecutor) ExecDup(stack *EVMStack, n int) (uint64, error) {
	if n < 1 || n > 16 {
		return 0, fmt.Errorf("%w: DUP%d", ErrStackOpDupOutOfBounds, n)
	}
	if stack.Len() < n {
		return 0, fmt.Errorf("%w: DUP%d needs %d items, have %d",
			ErrStackOpUnderflow, n, n, stack.Len())
	}
	if stack.Len() >= e.Config.MaxStack {
		return 0, fmt.Errorf("%w: DUP%d at depth %d",
			ErrStackOpOverflow, n, stack.Len())
	}
	if err := stack.Dup(n); err != nil {
		return 0, err
	}
	return e.Config.GasDupN, nil
}

// ExecSwap swaps the top element with the (n+1)th element from the top.
// n must be in [1, 16] (SWAP1 swaps top with 2nd, SWAP16 swaps top with 17th).
// Gas cost: GasSwapN (3).
func (e *StackOpExecutor) ExecSwap(stack *EVMStack, n int) (uint64, error) {
	if n < 1 || n > 16 {
		return 0, fmt.Errorf("%w: SWAP%d", ErrStackOpSwapOutOfBounds, n)
	}
	if stack.Len() < n+1 {
		return 0, fmt.Errorf("%w: SWAP%d needs %d items, have %d",
			ErrStackOpUnderflow, n, n+1, stack.Len())
	}
	if err := stack.Swap(n); err != nil {
		return 0, err
	}
	return e.Config.GasSwapN, nil
}

// StackBoundsChecker validates stack constraints before executing an operation.
// It checks both underflow (insufficient items) and overflow (exceeding 1024).
type StackBoundsChecker struct {
	maxDepth int
}

// NewStackBoundsChecker creates a StackBoundsChecker with the standard 1024 limit.
func NewStackBoundsChecker() *StackBoundsChecker {
	return &StackBoundsChecker{maxDepth: 1024}
}

// CheckPush validates that a push operation will not overflow the stack.
func (c *StackBoundsChecker) CheckPush(currentDepth int) error {
	if currentDepth >= c.maxDepth {
		return fmt.Errorf("%w: depth %d >= %d",
			ErrStackOpOverflow, currentDepth, c.maxDepth)
	}
	return nil
}

// CheckPop validates that a pop operation will not underflow the stack.
func (c *StackBoundsChecker) CheckPop(currentDepth, count int) error {
	if currentDepth < count {
		return fmt.Errorf("%w: need %d items, have %d",
			ErrStackOpUnderflow, count, currentDepth)
	}
	return nil
}

// CheckDup validates bounds for a DUP operation (needs n items, produces n+1).
func (c *StackBoundsChecker) CheckDup(currentDepth, n int) error {
	if n < 1 || n > 16 {
		return fmt.Errorf("%w: DUP%d out of [1,16]", ErrStackOpDupOutOfBounds, n)
	}
	if currentDepth < n {
		return fmt.Errorf("%w: DUP%d needs %d items, have %d",
			ErrStackOpUnderflow, n, n, currentDepth)
	}
	if currentDepth >= c.maxDepth {
		return fmt.Errorf("%w: DUP%d at depth %d",
			ErrStackOpOverflow, n, currentDepth)
	}
	return nil
}

// CheckSwap validates bounds for a SWAP operation (needs n+1 items).
func (c *StackBoundsChecker) CheckSwap(currentDepth, n int) error {
	if n < 1 || n > 16 {
		return fmt.Errorf("%w: SWAP%d out of [1,16]", ErrStackOpSwapOutOfBounds, n)
	}
	if currentDepth < n+1 {
		return fmt.Errorf("%w: SWAP%d needs %d items, have %d",
			ErrStackOpUnderflow, n, n+1, currentDepth)
	}
	return nil
}

// CallStackTracker tracks the call depth across nested EVM call contexts.
// Each CALL/CREATE/DELEGATECALL increments the depth; return decrements it.
// The EVM enforces a maximum call depth of 1024.
type CallStackTracker struct {
	depth    int
	maxDepth int
}

// NewCallStackTracker creates a tracker with the standard 1024 max depth.
func NewCallStackTracker() *CallStackTracker {
	return &CallStackTracker{maxDepth: MaxCallDepth}
}

// Enter increments the call depth and returns an error if the limit is reached.
func (t *CallStackTracker) Enter() error {
	if t.depth >= t.maxDepth {
		return fmt.Errorf("call stack: depth %d exceeds max %d", t.depth, t.maxDepth)
	}
	t.depth++
	return nil
}

// Leave decrements the call depth. It is a no-op if depth is already 0.
func (t *CallStackTracker) Leave() {
	if t.depth > 0 {
		t.depth--
	}
}

// Depth returns the current call depth.
func (t *CallStackTracker) Depth() int {
	return t.depth
}

// CanEnter returns true if there is room for another call frame.
func (t *CallStackTracker) CanEnter() bool {
	return t.depth < t.maxDepth
}

// PushDataExtractor extracts push operand bytes from EVM bytecode. It handles
// boundary conditions where the push operand extends beyond the code length.
type PushDataExtractor struct{}

// Extract returns the n-byte push operand starting at code[pc+1].
// If the code is shorter than needed, missing bytes are zero-padded.
// n must be in [0, 32]; n=0 returns nil (PUSH0).
func (PushDataExtractor) Extract(code []byte, pc uint64, n int) []byte {
	if n == 0 {
		return nil
	}
	if n < 0 || n > 32 {
		return nil
	}

	start := pc + 1
	codeLen := uint64(len(code))
	result := make([]byte, n)

	if start >= codeLen {
		return result // all zeros
	}

	end := start + uint64(n)
	if end > codeLen {
		end = codeLen
	}
	copy(result, code[start:end])
	return result
}

// StackGasCosts returns the gas cost for each class of stack operation.
type StackGasCosts struct {
	Push0Gas uint64 // PUSH0: base gas (2)
	PushGas  uint64 // PUSH1-PUSH32: verylow gas (3)
	DupGas   uint64 // DUP1-DUP16: verylow gas (3)
	SwapGas  uint64 // SWAP1-SWAP16: verylow gas (3)
}

// DefaultStackGasCosts returns the standard gas costs for stack operations.
func DefaultStackGasCosts() StackGasCosts {
	return StackGasCosts{
		Push0Gas: GasPush0, // 2 (Gbase)
		PushGas:  GasPush,  // 3 (Gverylow)
		DupGas:   GasDup,   // 3 (Gverylow)
		SwapGas:  GasSwap,  // 3 (Gverylow)
	}
}

// GasCostForPush returns the gas cost for a PUSH opcode with the given operand size.
// PUSH0 (size=0) costs GasPush0 (2); PUSH1-PUSH32 cost GasPush (3).
func (g StackGasCosts) GasCostForPush(size int) uint64 {
	if size == 0 {
		return g.Push0Gas
	}
	return g.PushGas
}

// GasCostForDup returns the gas cost for a DUP opcode. All DUP1-DUP16 cost
// the same: GasDup (3).
func (g StackGasCosts) GasCostForDup(n int) uint64 {
	return g.DupGas
}

// GasCostForSwap returns the gas cost for a SWAP opcode. All SWAP1-SWAP16
// cost the same: GasSwap (3).
func (g StackGasCosts) GasCostForSwap(n int) uint64 {
	return g.SwapGas
}

// BatchStackValidator validates a sequence of stack operations for a code
// segment, tracking the minimum and maximum stack heights reached. It is
// useful for static analysis of code sections.
type BatchStackValidator struct {
	initialDepth int
	currentDepth int
	minDepth     int
	maxDepth     int
	checker      *StackBoundsChecker
}

// NewBatchStackValidator creates a validator starting at the given initial depth.
func NewBatchStackValidator(initialDepth int) *BatchStackValidator {
	return &BatchStackValidator{
		initialDepth: initialDepth,
		currentDepth: initialDepth,
		minDepth:     initialDepth,
		maxDepth:     initialDepth,
		checker:      NewStackBoundsChecker(),
	}
}

// ValidatePush checks and records a push operation (+1 depth).
func (v *BatchStackValidator) ValidatePush() error {
	if err := v.checker.CheckPush(v.currentDepth); err != nil {
		return err
	}
	v.currentDepth++
	v.updateBounds()
	return nil
}

// ValidatePop checks and records a pop operation (-count depth).
func (v *BatchStackValidator) ValidatePop(count int) error {
	if err := v.checker.CheckPop(v.currentDepth, count); err != nil {
		return err
	}
	v.currentDepth -= count
	v.updateBounds()
	return nil
}

// ValidateDup checks and records a DUP operation.
func (v *BatchStackValidator) ValidateDup(n int) error {
	if err := v.checker.CheckDup(v.currentDepth, n); err != nil {
		return err
	}
	v.currentDepth++ // DUP pushes one copy
	v.updateBounds()
	return nil
}

// ValidateSwap checks and records a SWAP operation (no net depth change).
func (v *BatchStackValidator) ValidateSwap(n int) error {
	if err := v.checker.CheckSwap(v.currentDepth, n); err != nil {
		return err
	}
	// SWAP does not change depth.
	return nil
}

// CurrentDepth returns the current tracked stack depth.
func (v *BatchStackValidator) CurrentDepth() int {
	return v.currentDepth
}

// MinDepth returns the minimum stack depth observed.
func (v *BatchStackValidator) MinDepth() int {
	return v.minDepth
}

// MaxDepth returns the maximum stack depth observed.
func (v *BatchStackValidator) MaxDepth() int {
	return v.maxDepth
}

func (v *BatchStackValidator) updateBounds() {
	if v.currentDepth < v.minDepth {
		v.minDepth = v.currentDepth
	}
	if v.currentDepth > v.maxDepth {
		v.maxDepth = v.currentDepth
	}
}

// DecodePushOpcode returns the push operand size for a PUSH opcode.
// PUSH0 -> 0, PUSH1 -> 1, ..., PUSH32 -> 32.
// Returns -1 if the opcode is not a PUSH instruction.
func DecodePushOpcode(op OpCode) int {
	if op == PUSH0 {
		return 0
	}
	if op >= PUSH1 && op <= PUSH32 {
		return int(op-PUSH1) + 1
	}
	return -1
}

// DecodeDupOpcode returns the DUP operand for a DUP opcode.
// DUP1 -> 1, DUP2 -> 2, ..., DUP16 -> 16.
// Returns -1 if the opcode is not a DUP instruction.
func DecodeDupOpcode(op OpCode) int {
	if op >= DUP1 && op <= DUP16 {
		return int(op-DUP1) + 1
	}
	return -1
}

// DecodeSwapOpcode returns the SWAP operand for a SWAP opcode.
// SWAP1 -> 1, SWAP2 -> 2, ..., SWAP16 -> 16.
// Returns -1 if the opcode is not a SWAP instruction.
func DecodeSwapOpcode(op OpCode) int {
	if op >= SWAP1 && op <= SWAP16 {
		return int(op-SWAP1) + 1
	}
	return -1
}
