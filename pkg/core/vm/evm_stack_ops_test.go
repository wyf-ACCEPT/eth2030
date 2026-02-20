package vm

import (
	"math/big"
	"testing"
)

func TestStackOpExecutor_ExecPush0(t *testing.T) {
	exec := NewStackOpExecutor()
	stack := NewEVMStack()

	gas, err := exec.ExecPush0(stack)
	if err != nil {
		t.Fatalf("ExecPush0 unexpected error: %v", err)
	}
	if gas != GasPush0 {
		t.Errorf("gas = %d, want %d", gas, GasPush0)
	}
	if stack.Len() != 1 {
		t.Errorf("stack depth = %d, want 1", stack.Len())
	}
	val, _ := stack.Peek()
	if val.Sign() != 0 {
		t.Errorf("pushed value = %s, want 0", val)
	}
}

func TestStackOpExecutor_ExecPush0_Overflow(t *testing.T) {
	exec := NewStackOpExecutor()
	stack := NewEVMStack()

	// Fill stack to capacity.
	for i := 0; i < 1024; i++ {
		if err := stack.Push(big.NewInt(int64(i))); err != nil {
			t.Fatalf("setup push failed at %d: %v", i, err)
		}
	}

	_, err := exec.ExecPush0(stack)
	if err == nil {
		t.Fatal("expected overflow error, got nil")
	}
}

func TestStackOpExecutor_ExecPushN(t *testing.T) {
	exec := NewStackOpExecutor()

	tests := []struct {
		name     string
		code     []byte
		offset   uint64
		n        int
		expected int64
	}{
		{
			name:     "PUSH1 value 0x42",
			code:     []byte{0x60, 0x42},
			offset:   0,
			n:        1,
			expected: 0x42,
		},
		{
			name:     "PUSH2 value 0x0102",
			code:     []byte{0x61, 0x01, 0x02},
			offset:   0,
			n:        2,
			expected: 0x0102,
		},
		{
			name:     "PUSH1 past code end (zero-padded)",
			code:     []byte{0x60},
			offset:   0,
			n:        1,
			expected: 0,
		},
		{
			name:     "PUSH4 partial code",
			code:     []byte{0x63, 0xAA, 0xBB},
			offset:   0,
			n:        4,
			expected: 0xAABB0000,
		},
		{
			name:     "PUSH32 all zeros",
			code:     make([]byte, 1),
			offset:   0,
			n:        32,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stack := NewEVMStack()
			gas, err := exec.ExecPushN(stack, tt.code, tt.offset, tt.n)
			if err != nil {
				t.Fatalf("ExecPushN error: %v", err)
			}
			if gas != GasPush {
				t.Errorf("gas = %d, want %d", gas, GasPush)
			}
			val, _ := stack.Peek()
			if val.Int64() != tt.expected {
				t.Errorf("value = %d, want %d", val.Int64(), tt.expected)
			}
		})
	}
}

func TestStackOpExecutor_ExecPushN_InvalidSize(t *testing.T) {
	exec := NewStackOpExecutor()
	stack := NewEVMStack()

	_, err := exec.ExecPushN(stack, []byte{0x60}, 0, 0)
	if err == nil {
		t.Error("expected error for size 0")
	}

	_, err = exec.ExecPushN(stack, []byte{0x60}, 0, 33)
	if err == nil {
		t.Error("expected error for size 33")
	}
}

func TestStackOpExecutor_ExecDup(t *testing.T) {
	exec := NewStackOpExecutor()

	tests := []struct {
		name    string
		n       int
		items   []int64
		wantTop int64
	}{
		{"DUP1", 1, []int64{10}, 10},
		{"DUP2", 2, []int64{20, 10}, 20},
		{"DUP3", 3, []int64{30, 20, 10}, 30},
		{"DUP16", 16, make([]int64, 16), 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stack := NewEVMStack()
			for _, v := range tt.items {
				stack.Push(big.NewInt(v))
			}
			// For DUP16 test, set the bottom element.
			if tt.n == 16 {
				stack.data[0] = big.NewInt(99)
				tt.wantTop = 99
			}

			origLen := stack.Len()
			gas, err := exec.ExecDup(stack, tt.n)
			if err != nil {
				t.Fatalf("ExecDup error: %v", err)
			}
			if gas != GasDup {
				t.Errorf("gas = %d, want %d", gas, GasDup)
			}
			if stack.Len() != origLen+1 {
				t.Errorf("stack depth = %d, want %d", stack.Len(), origLen+1)
			}
			val, _ := stack.Peek()
			if val.Int64() != tt.wantTop {
				t.Errorf("top = %d, want %d", val.Int64(), tt.wantTop)
			}
		})
	}
}

func TestStackOpExecutor_ExecDup_Underflow(t *testing.T) {
	exec := NewStackOpExecutor()
	stack := NewEVMStack()

	_, err := exec.ExecDup(stack, 1)
	if err == nil {
		t.Error("expected underflow error for DUP1 on empty stack")
	}

	stack.Push(big.NewInt(1))
	_, err = exec.ExecDup(stack, 2)
	if err == nil {
		t.Error("expected underflow error for DUP2 with 1 item")
	}
}

func TestStackOpExecutor_ExecDup_Overflow(t *testing.T) {
	exec := NewStackOpExecutor()
	stack := NewEVMStack()

	for i := 0; i < 1024; i++ {
		stack.Push(big.NewInt(int64(i)))
	}

	_, err := exec.ExecDup(stack, 1)
	if err == nil {
		t.Error("expected overflow error for DUP on full stack")
	}
}

func TestStackOpExecutor_ExecSwap(t *testing.T) {
	exec := NewStackOpExecutor()

	tests := []struct {
		name     string
		n        int
		items    []int64
		wantTop  int64
		wantNth  int64
	}{
		{"SWAP1", 1, []int64{1, 2}, 1, 2},
		{"SWAP2", 2, []int64{1, 2, 3}, 1, 3},
		{"SWAP3", 3, []int64{1, 2, 3, 4}, 1, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stack := NewEVMStack()
			for _, v := range tt.items {
				stack.Push(big.NewInt(v))
			}

			origLen := stack.Len()
			gas, err := exec.ExecSwap(stack, tt.n)
			if err != nil {
				t.Fatalf("ExecSwap error: %v", err)
			}
			if gas != GasSwap {
				t.Errorf("gas = %d, want %d", gas, GasSwap)
			}
			if stack.Len() != origLen {
				t.Errorf("stack depth changed: %d -> %d", origLen, stack.Len())
			}
			val, _ := stack.Peek()
			if val.Int64() != tt.wantTop {
				t.Errorf("top = %d, want %d", val.Int64(), tt.wantTop)
			}
		})
	}
}

func TestStackOpExecutor_ExecSwap_Underflow(t *testing.T) {
	exec := NewStackOpExecutor()
	stack := NewEVMStack()

	stack.Push(big.NewInt(1))
	_, err := exec.ExecSwap(stack, 1)
	if err == nil {
		t.Error("expected underflow error for SWAP1 with 1 item")
	}
}

func TestStackOpExecutor_ExecSwap_OutOfRange(t *testing.T) {
	exec := NewStackOpExecutor()
	stack := NewEVMStack()

	_, err := exec.ExecSwap(stack, 0)
	if err == nil {
		t.Error("expected error for SWAP0")
	}

	_, err = exec.ExecSwap(stack, 17)
	if err == nil {
		t.Error("expected error for SWAP17")
	}
}

func TestStackBoundsChecker_CheckPush(t *testing.T) {
	checker := NewStackBoundsChecker()

	if err := checker.CheckPush(0); err != nil {
		t.Errorf("expected no error at depth 0: %v", err)
	}
	if err := checker.CheckPush(1023); err != nil {
		t.Errorf("expected no error at depth 1023: %v", err)
	}
	if err := checker.CheckPush(1024); err == nil {
		t.Error("expected overflow at depth 1024")
	}
}

func TestStackBoundsChecker_CheckPop(t *testing.T) {
	checker := NewStackBoundsChecker()

	if err := checker.CheckPop(1, 1); err != nil {
		t.Errorf("expected no error: %v", err)
	}
	if err := checker.CheckPop(0, 1); err == nil {
		t.Error("expected underflow when popping from empty stack")
	}
	if err := checker.CheckPop(5, 6); err == nil {
		t.Error("expected underflow when popping more than available")
	}
}

func TestStackBoundsChecker_CheckDup(t *testing.T) {
	checker := NewStackBoundsChecker()

	if err := checker.CheckDup(5, 3); err != nil {
		t.Errorf("valid DUP3 with 5 items failed: %v", err)
	}
	if err := checker.CheckDup(2, 3); err == nil {
		t.Error("expected underflow for DUP3 with 2 items")
	}
	if err := checker.CheckDup(10, 0); err == nil {
		t.Error("expected out of bounds for DUP0")
	}
	if err := checker.CheckDup(10, 17); err == nil {
		t.Error("expected out of bounds for DUP17")
	}
}

func TestStackBoundsChecker_CheckSwap(t *testing.T) {
	checker := NewStackBoundsChecker()

	if err := checker.CheckSwap(5, 3); err != nil {
		t.Errorf("valid SWAP3 with 5 items failed: %v", err)
	}
	if err := checker.CheckSwap(3, 3); err == nil {
		t.Error("expected underflow for SWAP3 with 3 items (need 4)")
	}
	if err := checker.CheckSwap(10, 0); err == nil {
		t.Error("expected out of bounds for SWAP0")
	}
}

func TestCallStackTracker(t *testing.T) {
	tracker := NewCallStackTracker()

	if tracker.Depth() != 0 {
		t.Errorf("initial depth = %d, want 0", tracker.Depth())
	}
	if !tracker.CanEnter() {
		t.Error("should be able to enter at depth 0")
	}

	// Enter a few levels.
	for i := 0; i < 10; i++ {
		if err := tracker.Enter(); err != nil {
			t.Fatalf("Enter failed at depth %d: %v", i, err)
		}
	}
	if tracker.Depth() != 10 {
		t.Errorf("depth = %d, want 10", tracker.Depth())
	}

	// Leave a few levels.
	tracker.Leave()
	tracker.Leave()
	if tracker.Depth() != 8 {
		t.Errorf("depth = %d, want 8", tracker.Depth())
	}

	// Leave at zero should be safe.
	for i := 0; i < 20; i++ {
		tracker.Leave()
	}
	if tracker.Depth() != 0 {
		t.Errorf("depth = %d, want 0 after excess Leave calls", tracker.Depth())
	}
}

func TestCallStackTracker_MaxDepth(t *testing.T) {
	tracker := NewCallStackTracker()

	for i := 0; i < MaxCallDepth; i++ {
		if err := tracker.Enter(); err != nil {
			t.Fatalf("Enter failed at depth %d: %v", i, err)
		}
	}

	if tracker.CanEnter() {
		t.Error("should not be able to enter at max depth")
	}

	err := tracker.Enter()
	if err == nil {
		t.Error("expected error at max call depth")
	}
}

func TestPushDataExtractor(t *testing.T) {
	extractor := PushDataExtractor{}

	tests := []struct {
		name     string
		code     []byte
		pc       uint64
		n        int
		expected []byte
	}{
		{"PUSH0", []byte{0x5f}, 0, 0, nil},
		{"PUSH1 normal", []byte{0x60, 0xAA}, 0, 1, []byte{0xAA}},
		{"PUSH2 normal", []byte{0x61, 0xAA, 0xBB}, 0, 2, []byte{0xAA, 0xBB}},
		{"PUSH1 past end", []byte{0x60}, 0, 1, []byte{0x00}},
		{"PUSH4 partial", []byte{0x63, 0x11, 0x22}, 0, 4, []byte{0x11, 0x22, 0x00, 0x00}},
		{"PUSH1 at offset", []byte{0x00, 0x60, 0xFF}, 1, 1, []byte{0xFF}},
		{"invalid n=-1", nil, 0, -1, nil},
		{"invalid n=33", nil, 0, 33, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractor.Extract(tt.code, tt.pc, tt.n)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}
			if len(result) != len(tt.expected) {
				t.Fatalf("length = %d, want %d", len(result), len(tt.expected))
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("byte[%d] = 0x%02x, want 0x%02x", i, result[i], tt.expected[i])
				}
			}
		})
	}
}

func TestDefaultStackGasCosts(t *testing.T) {
	costs := DefaultStackGasCosts()

	if costs.GasCostForPush(0) != GasPush0 {
		t.Errorf("PUSH0 cost = %d, want %d", costs.GasCostForPush(0), GasPush0)
	}
	for n := 1; n <= 32; n++ {
		if costs.GasCostForPush(n) != GasPush {
			t.Errorf("PUSH%d cost = %d, want %d", n, costs.GasCostForPush(n), GasPush)
		}
	}
	for n := 1; n <= 16; n++ {
		if costs.GasCostForDup(n) != GasDup {
			t.Errorf("DUP%d cost = %d, want %d", n, costs.GasCostForDup(n), GasDup)
		}
		if costs.GasCostForSwap(n) != GasSwap {
			t.Errorf("SWAP%d cost = %d, want %d", n, costs.GasCostForSwap(n), GasSwap)
		}
	}
}

func TestDecodePushOpcode(t *testing.T) {
	if n := DecodePushOpcode(PUSH0); n != 0 {
		t.Errorf("PUSH0 = %d, want 0", n)
	}
	if n := DecodePushOpcode(PUSH1); n != 1 {
		t.Errorf("PUSH1 = %d, want 1", n)
	}
	if n := DecodePushOpcode(PUSH32); n != 32 {
		t.Errorf("PUSH32 = %d, want 32", n)
	}
	if n := DecodePushOpcode(ADD); n != -1 {
		t.Errorf("ADD = %d, want -1", n)
	}
}

func TestDecodeDupOpcode(t *testing.T) {
	if n := DecodeDupOpcode(DUP1); n != 1 {
		t.Errorf("DUP1 = %d, want 1", n)
	}
	if n := DecodeDupOpcode(DUP16); n != 16 {
		t.Errorf("DUP16 = %d, want 16", n)
	}
	if n := DecodeDupOpcode(ADD); n != -1 {
		t.Errorf("ADD = %d, want -1", n)
	}
}

func TestDecodeSwapOpcode(t *testing.T) {
	if n := DecodeSwapOpcode(SWAP1); n != 1 {
		t.Errorf("SWAP1 = %d, want 1", n)
	}
	if n := DecodeSwapOpcode(SWAP16); n != 16 {
		t.Errorf("SWAP16 = %d, want 16", n)
	}
	if n := DecodeSwapOpcode(ADD); n != -1 {
		t.Errorf("ADD = %d, want -1", n)
	}
}

func TestBatchStackValidator(t *testing.T) {
	v := NewBatchStackValidator(0)

	// Simulate: PUSH, PUSH, DUP1, SWAP1, POP
	if err := v.ValidatePush(); err != nil {
		t.Fatalf("push 1 failed: %v", err)
	}
	if err := v.ValidatePush(); err != nil {
		t.Fatalf("push 2 failed: %v", err)
	}
	if v.CurrentDepth() != 2 {
		t.Errorf("depth = %d, want 2", v.CurrentDepth())
	}

	if err := v.ValidateDup(1); err != nil {
		t.Fatalf("dup 1 failed: %v", err)
	}
	if v.CurrentDepth() != 3 {
		t.Errorf("depth = %d, want 3", v.CurrentDepth())
	}

	if err := v.ValidateSwap(1); err != nil {
		t.Fatalf("swap 1 failed: %v", err)
	}
	if v.CurrentDepth() != 3 {
		t.Errorf("depth after swap = %d, want 3", v.CurrentDepth())
	}

	if err := v.ValidatePop(1); err != nil {
		t.Fatalf("pop failed: %v", err)
	}
	if v.CurrentDepth() != 2 {
		t.Errorf("depth = %d, want 2", v.CurrentDepth())
	}

	if v.MaxDepth() != 3 {
		t.Errorf("max depth = %d, want 3", v.MaxDepth())
	}
	if v.MinDepth() != 0 {
		t.Errorf("min depth = %d, want 0", v.MinDepth())
	}
}

func TestBatchStackValidator_Underflow(t *testing.T) {
	v := NewBatchStackValidator(0)

	err := v.ValidatePop(1)
	if err == nil {
		t.Error("expected underflow on empty stack pop")
	}
}

func TestBatchStackValidator_Overflow(t *testing.T) {
	v := NewBatchStackValidator(1024)

	err := v.ValidatePush()
	if err == nil {
		t.Error("expected overflow at max depth")
	}
}
