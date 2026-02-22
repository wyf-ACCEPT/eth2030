package vm

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestCallFrameType_String(t *testing.T) {
	tests := []struct {
		ft   CallFrameType
		want string
	}{
		{FrameCall, "CALL"},
		{FrameStaticCall, "STATICCALL"},
		{FrameDelegateCall, "DELEGATECALL"},
		{FrameCallCode, "CALLCODE"},
		{FrameCreate, "CREATE"},
		{FrameCreate2, "CREATE2"},
		{CallFrameType(255), "UNKNOWN"},
	}
	for _, tc := range tests {
		if got := tc.ft.String(); got != tc.want {
			t.Errorf("CallFrameType(%d).String() = %q, want %q", tc.ft, got, tc.want)
		}
	}
}

func TestCallFrameType_IsCreate(t *testing.T) {
	tests := []struct {
		ft   CallFrameType
		want bool
	}{
		{FrameCall, false},
		{FrameStaticCall, false},
		{FrameDelegateCall, false},
		{FrameCallCode, false},
		{FrameCreate, true},
		{FrameCreate2, true},
	}
	for _, tc := range tests {
		if got := tc.ft.IsCreate(); got != tc.want {
			t.Errorf("%s.IsCreate() = %v, want %v", tc.ft, got, tc.want)
		}
	}
}

func TestCallFrame_GasRemaining(t *testing.T) {
	tests := []struct {
		name     string
		start    uint64
		used     uint64
		expected uint64
	}{
		{"no gas used", 10000, 0, 10000},
		{"partial use", 10000, 3000, 7000},
		{"all used", 10000, 10000, 0},
		{"overflow protection", 100, 200, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cf := &CallFrame{GasStart: tc.start, GasUsed: tc.used}
			if got := cf.GasRemaining(); got != tc.expected {
				t.Errorf("GasRemaining() = %d, want %d", got, tc.expected)
			}
		})
	}
}

func TestCallFrameStack_PushAndDepth(t *testing.T) {
	stack := NewCallFrameStack()
	if stack.Depth() != 0 {
		t.Fatalf("empty stack depth = %d, want 0", stack.Depth())
	}

	frame := &CallFrame{
		Type:     FrameCall,
		Caller:   types.HexToAddress("0xaaa"),
		To:       types.HexToAddress("0xbbb"),
		Value:    big.NewInt(0),
		GasStart: 1000000,
	}
	if err := stack.Push(frame); err != nil {
		t.Fatalf("Push failed: %v", err)
	}
	if stack.Depth() != 1 {
		t.Errorf("depth after push = %d, want 1", stack.Depth())
	}
	if frame.Depth != 0 {
		t.Errorf("first frame depth = %d, want 0", frame.Depth)
	}
}

func TestCallFrameStack_MaxDepthEnforcement(t *testing.T) {
	limit := 4
	stack := NewCallFrameStackWithLimit(limit)

	for i := 0; i < limit; i++ {
		err := stack.Push(&CallFrame{
			Type:     FrameCall,
			GasStart: 1000,
			Value:    big.NewInt(0),
		})
		if err != nil {
			t.Fatalf("Push at depth %d failed: %v", i, err)
		}
	}

	// Exceeding max depth should fail.
	err := stack.Push(&CallFrame{Type: FrameCall, Value: big.NewInt(0)})
	if err != ErrMaxCallDepthExceeded {
		t.Errorf("expected ErrMaxCallDepthExceeded, got %v", err)
	}
	if stack.Depth() != limit {
		t.Errorf("depth = %d, want %d after failed push", stack.Depth(), limit)
	}
}

func TestCallFrameStack_StandardMaxDepth(t *testing.T) {
	stack := NewCallFrameStack()
	// Verify the standard limit is 1024.
	if stack.maxDepth != MaxCallDepth {
		t.Errorf("default maxDepth = %d, want %d", stack.maxDepth, MaxCallDepth)
	}
}

func TestCallFrameStack_CanPush(t *testing.T) {
	stack := NewCallFrameStackWithLimit(2)
	if !stack.CanPush() {
		t.Error("should be able to push on empty stack")
	}
	stack.Push(&CallFrame{Value: big.NewInt(0)})
	if !stack.CanPush() {
		t.Error("should be able to push at depth 1 with limit 2")
	}
	stack.Push(&CallFrame{Value: big.NewInt(0)})
	if stack.CanPush() {
		t.Error("should not be able to push at depth 2 with limit 2")
	}
}

func TestCallFrameStack_Pop(t *testing.T) {
	stack := NewCallFrameStack()

	// Pop from empty stack returns nil.
	if f := stack.Pop(); f != nil {
		t.Error("Pop from empty stack should return nil")
	}

	stack.Push(&CallFrame{Type: FrameCall, Value: big.NewInt(0)})
	stack.Push(&CallFrame{Type: FrameCreate, Value: big.NewInt(0)})

	f := stack.Pop()
	if f == nil || f.Type != FrameCreate {
		t.Error("Pop should return the top frame (CREATE)")
	}
	if stack.Depth() != 1 {
		t.Errorf("depth after pop = %d, want 1", stack.Depth())
	}
}

func TestCallFrameStack_Current(t *testing.T) {
	stack := NewCallFrameStack()

	if stack.Current() != nil {
		t.Error("Current on empty stack should be nil")
	}

	f1 := &CallFrame{Type: FrameCall, Value: big.NewInt(0)}
	f2 := &CallFrame{Type: FrameStaticCall, Value: big.NewInt(0)}
	stack.Push(f1)
	stack.Push(f2)

	cur := stack.Current()
	if cur != f2 {
		t.Error("Current should return the topmost frame")
	}
}

func TestCallFrameStack_Parent(t *testing.T) {
	stack := NewCallFrameStack()

	if stack.Parent() != nil {
		t.Error("Parent on empty stack should be nil")
	}

	f1 := &CallFrame{Type: FrameCall, Value: big.NewInt(0)}
	stack.Push(f1)
	if stack.Parent() != nil {
		t.Error("Parent with single frame should be nil")
	}

	f2 := &CallFrame{Type: FrameCreate, Value: big.NewInt(0)}
	stack.Push(f2)
	if stack.Parent() != f1 {
		t.Error("Parent should return the frame below the top")
	}
}

func TestCallFrameStack_AtDepth(t *testing.T) {
	stack := NewCallFrameStack()
	f0 := &CallFrame{Type: FrameCall, Value: big.NewInt(0)}
	f1 := &CallFrame{Type: FrameCreate, Value: big.NewInt(0)}
	stack.Push(f0)
	stack.Push(f1)

	if stack.AtDepth(0) != f0 {
		t.Error("AtDepth(0) should return first frame")
	}
	if stack.AtDepth(1) != f1 {
		t.Error("AtDepth(1) should return second frame")
	}
	if stack.AtDepth(-1) != nil {
		t.Error("AtDepth(-1) should return nil")
	}
	if stack.AtDepth(2) != nil {
		t.Error("AtDepth(2) out of bounds should return nil")
	}
}

func TestCallFrameStack_IsStatic(t *testing.T) {
	stack := NewCallFrameStack()
	stack.Push(&CallFrame{Type: FrameCall, ReadOnly: false, Value: big.NewInt(0)})

	if stack.IsStatic() {
		t.Error("stack without static frames should not be static")
	}

	stack.Push(&CallFrame{Type: FrameStaticCall, ReadOnly: true, Value: big.NewInt(0)})
	if !stack.IsStatic() {
		t.Error("stack with a static frame should be static")
	}

	// Pop the static frame, check again.
	stack.Pop()
	if stack.IsStatic() {
		t.Error("after removing static frame, stack should not be static")
	}
}

func TestCallFrameStack_DepthAssignment(t *testing.T) {
	stack := NewCallFrameStack()
	for i := 0; i < 5; i++ {
		f := &CallFrame{Type: FrameCall, Value: big.NewInt(0)}
		stack.Push(f)
		if f.Depth != i {
			t.Errorf("frame at push %d has Depth=%d, want %d", i, f.Depth, i)
		}
	}
}

func TestForwardGas_63_64Rule(t *testing.T) {
	tests := []struct {
		name           string
		available      uint64
		requested      uint64
		transfersValue bool
		wantChild      uint64
		wantDeduction  uint64
	}{
		{
			name:          "request less than max",
			available:     6400,
			requested:     1000,
			wantChild:     1000,
			wantDeduction: 1000,
		},
		{
			name:          "request exactly max forward",
			available:     6400,
			requested:     6300, // 6400 - 6400/64 = 6400 - 100 = 6300
			wantChild:     6300,
			wantDeduction: 6300,
		},
		{
			name:          "request more than max forward, capped",
			available:     6400,
			requested:     10000,
			wantChild:     6300, // capped at 6400 - 100
			wantDeduction: 6300,
		},
		{
			name:           "value transfer adds stipend",
			available:      6400,
			requested:      1000,
			transfersValue: true,
			wantChild:      1000 + CallStipend,
			wantDeduction:  1000,
		},
		{
			name:          "zero available gas",
			available:     0,
			requested:     1000,
			wantChild:     0,
			wantDeduction: 0,
		},
		{
			name:          "small available gas",
			available:     64,
			requested:     100,
			wantChild:     63, // 64 - 64/64 = 63
			wantDeduction: 63,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			child, deduction := ForwardGas(tc.available, tc.requested, tc.transfersValue)
			if child != tc.wantChild {
				t.Errorf("childGas = %d, want %d", child, tc.wantChild)
			}
			if deduction != tc.wantDeduction {
				t.Errorf("callerDeduction = %d, want %d", deduction, tc.wantDeduction)
			}
		})
	}
}

func TestCallMemoryRegion_End(t *testing.T) {
	tests := []struct {
		name   string
		region CallMemoryRegion
		want   uint64
	}{
		{"zero size", CallMemoryRegion{Offset: 100, Size: 0}, 0},
		{"normal", CallMemoryRegion{Offset: 32, Size: 64}, 96},
		{"offset zero", CallMemoryRegion{Offset: 0, Size: 32}, 32},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.region.End(); got != tc.want {
				t.Errorf("End() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestCallMemoryExpansion(t *testing.T) {
	tests := []struct {
		name   string
		input  CallMemoryRegion
		output CallMemoryRegion
		want   uint64
	}{
		{
			name:   "input larger",
			input:  CallMemoryRegion{Offset: 0, Size: 100},
			output: CallMemoryRegion{Offset: 0, Size: 50},
			want:   100,
		},
		{
			name:   "output larger",
			input:  CallMemoryRegion{Offset: 0, Size: 32},
			output: CallMemoryRegion{Offset: 0, Size: 64},
			want:   64,
		},
		{
			name:   "both zero size",
			input:  CallMemoryRegion{Offset: 0, Size: 0},
			output: CallMemoryRegion{Offset: 0, Size: 0},
			want:   0,
		},
		{
			name:   "different offsets",
			input:  CallMemoryRegion{Offset: 64, Size: 32},
			output: CallMemoryRegion{Offset: 0, Size: 128},
			want:   128, // output ends at 128 > input ends at 96
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := CallMemoryExpansion(tc.input, tc.output); got != tc.want {
				t.Errorf("CallMemoryExpansion = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestReturnDataBuffer(t *testing.T) {
	rdb := NewReturnDataBuffer()

	if rdb.Size() != 0 {
		t.Errorf("initial size = %d, want 0", rdb.Size())
	}
	if rdb.Data() != nil {
		t.Error("initial data should be nil")
	}

	// Set some data.
	rdb.Set([]byte{0xde, 0xad, 0xbe, 0xef})
	if rdb.Size() != 4 {
		t.Errorf("size after set = %d, want 4", rdb.Size())
	}

	// Slice within bounds.
	data, err := rdb.Slice(1, 2)
	if err != nil {
		t.Fatalf("Slice(1,2) error: %v", err)
	}
	if len(data) != 2 || data[0] != 0xad || data[1] != 0xbe {
		t.Errorf("Slice(1,2) = %x, want [ad be]", data)
	}

	// Slice out of bounds.
	_, err = rdb.Slice(2, 10)
	if err != ErrReturnDataOutOfBounds {
		t.Errorf("expected ErrReturnDataOutOfBounds, got %v", err)
	}

	// Slice with zero size returns nil.
	data, err = rdb.Slice(0, 0)
	if err != nil || data != nil {
		t.Error("Slice(0,0) should return nil, nil")
	}

	// Clear.
	rdb.Clear()
	if rdb.Size() != 0 {
		t.Errorf("size after clear = %d, want 0", rdb.Size())
	}
}

func TestReturnDataBuffer_SetEmpty(t *testing.T) {
	rdb := NewReturnDataBuffer()
	rdb.Set([]byte{0x01, 0x02})
	rdb.Set([]byte{})
	if rdb.Data() != nil {
		t.Error("setting empty data should result in nil")
	}
}

func TestReturnDataBuffer_SetCopiesData(t *testing.T) {
	rdb := NewReturnDataBuffer()
	original := []byte{0x01, 0x02, 0x03}
	rdb.Set(original)
	original[0] = 0xff
	if rdb.Data()[0] == 0xff {
		t.Error("Set should copy data, not reference original")
	}
}

func TestReturnDataBuffer_SliceCopiesData(t *testing.T) {
	rdb := NewReturnDataBuffer()
	rdb.Set([]byte{0x01, 0x02, 0x03})
	sliced, _ := rdb.Slice(0, 3)
	sliced[0] = 0xff
	if rdb.Data()[0] == 0xff {
		t.Error("Slice should return a copy, not reference internal data")
	}
}
