package vm

// call_frame.go implements call frame management for the EVM, tracking the
// execution context at each CALL/CREATE depth. It handles call depth limits
// (max 1024), gas forwarding via the EIP-150 63/64 rule, return data
// propagation, and memory expansion for call input/output regions.

import (
	"math/big"

	"github.com/eth2030/eth2030/core/types"
)

// CallFrameType enumerates the different types of EVM call frames.
type CallFrameType uint8

const (
	FrameCall         CallFrameType = iota // CALL opcode
	FrameStaticCall                        // STATICCALL opcode
	FrameDelegateCall                      // DELEGATECALL opcode
	FrameCallCode                          // CALLCODE opcode
	FrameCreate                            // CREATE opcode
	FrameCreate2                           // CREATE2 opcode
)

// String returns the human-readable name of the call frame type.
func (ft CallFrameType) String() string {
	switch ft {
	case FrameCall:
		return "CALL"
	case FrameStaticCall:
		return "STATICCALL"
	case FrameDelegateCall:
		return "DELEGATECALL"
	case FrameCallCode:
		return "CALLCODE"
	case FrameCreate:
		return "CREATE"
	case FrameCreate2:
		return "CREATE2"
	default:
		return "UNKNOWN"
	}
}

// IsCreate returns true if this frame type is a contract creation.
func (ft CallFrameType) IsCreate() bool {
	return ft == FrameCreate || ft == FrameCreate2
}

// CallFrame represents a single execution frame in the EVM call stack.
// Each CALL, STATICCALL, DELEGATECALL, CALLCODE, CREATE, or CREATE2
// instruction creates a new frame.
type CallFrame struct {
	Type       CallFrameType
	Caller     types.Address // address that initiated this frame
	To         types.Address // target address (for calls) or new contract (for creates)
	Value      *big.Int      // ETH value transferred
	GasStart   uint64        // gas available when this frame started
	GasUsed    uint64        // gas consumed so far in this frame
	Input      []byte        // calldata sent to this frame
	ReturnData []byte        // data returned by this frame (set on completion)
	Depth      int           // call depth (0 = top-level transaction)
	ReadOnly   bool          // true if this frame is in a static context
	SnapshotID int           // state snapshot taken at frame entry
}

// GasRemaining returns the gas remaining in this frame.
func (cf *CallFrame) GasRemaining() uint64 {
	if cf.GasUsed > cf.GasStart {
		return 0
	}
	return cf.GasStart - cf.GasUsed
}

// CallFrameStack manages a stack of call frames, enforcing the maximum
// call depth and providing frame lifecycle management.
type CallFrameStack struct {
	frames   []*CallFrame
	maxDepth int
}

// NewCallFrameStack creates a CallFrameStack with the standard 1024 depth limit.
func NewCallFrameStack() *CallFrameStack {
	return &CallFrameStack{
		frames:   make([]*CallFrame, 0, 16),
		maxDepth: MaxCallDepth,
	}
}

// NewCallFrameStackWithLimit creates a CallFrameStack with a custom depth limit.
func NewCallFrameStackWithLimit(maxDepth int) *CallFrameStack {
	return &CallFrameStack{
		frames:   make([]*CallFrame, 0, 16),
		maxDepth: maxDepth,
	}
}

// Depth returns the current call depth (number of active frames).
func (cfs *CallFrameStack) Depth() int {
	return len(cfs.frames)
}

// CanPush returns true if a new frame can be pushed without exceeding
// the maximum call depth.
func (cfs *CallFrameStack) CanPush() bool {
	return len(cfs.frames) < cfs.maxDepth
}

// Push creates and pushes a new call frame onto the stack. Returns
// ErrMaxCallDepthExceeded if the depth limit would be exceeded.
func (cfs *CallFrameStack) Push(frame *CallFrame) error {
	if len(cfs.frames) >= cfs.maxDepth {
		return ErrMaxCallDepthExceeded
	}
	frame.Depth = len(cfs.frames)
	cfs.frames = append(cfs.frames, frame)
	return nil
}

// Pop removes and returns the top frame from the stack. Returns nil if
// the stack is empty.
func (cfs *CallFrameStack) Pop() *CallFrame {
	n := len(cfs.frames)
	if n == 0 {
		return nil
	}
	frame := cfs.frames[n-1]
	cfs.frames = cfs.frames[:n-1]
	return frame
}

// Current returns the frame at the top of the stack without removing it.
// Returns nil if the stack is empty.
func (cfs *CallFrameStack) Current() *CallFrame {
	n := len(cfs.frames)
	if n == 0 {
		return nil
	}
	return cfs.frames[n-1]
}

// Parent returns the frame one level below the current frame. Returns nil
// if the stack has fewer than two frames.
func (cfs *CallFrameStack) Parent() *CallFrame {
	n := len(cfs.frames)
	if n < 2 {
		return nil
	}
	return cfs.frames[n-2]
}

// AtDepth returns the frame at the specified depth. Returns nil if the
// depth is out of bounds.
func (cfs *CallFrameStack) AtDepth(depth int) *CallFrame {
	if depth < 0 || depth >= len(cfs.frames) {
		return nil
	}
	return cfs.frames[depth]
}

// IsStatic returns true if any frame in the stack is in a read-only
// (static) context.
func (cfs *CallFrameStack) IsStatic() bool {
	for _, f := range cfs.frames {
		if f.ReadOnly {
			return true
		}
	}
	return false
}

// ForwardGas computes the gas to forward to a child call using the EIP-150
// 63/64 rule. The caller retains at least 1/64 of its remaining gas.
//
//	maxForward = available - floor(available / 64)
//	forwarded  = min(requested, maxForward)
//
// If the call transfers value, the 2300 gas stipend is added to the child
// and is not deducted from the caller.
func ForwardGas(available, requested uint64, transfersValue bool) (childGas, callerDeduction uint64) {
	// EIP-150: cap at 63/64 of available gas.
	retained := available / CallGasFraction
	maxForward := available - retained
	if requested > maxForward {
		requested = maxForward
	}

	callerDeduction = requested

	// Stipend: when value is transferred, the callee receives an additional
	// 2300 gas that is not deducted from the caller.
	if transfersValue {
		requested = safeAddGas(requested, CallStipend)
	}

	return requested, callerDeduction
}

// safeAddGas adds two uint64 values, capping at max uint64 on overflow.
func safeAddGas(a, b uint64) uint64 {
	sum := a + b
	if sum < a {
		return ^uint64(0)
	}
	return sum
}

// CallMemoryRegion describes a memory region for call input or return data.
type CallMemoryRegion struct {
	Offset uint64
	Size   uint64
}

// End returns offset + size, the first byte beyond this region. Returns 0
// if size is 0 (no memory needed).
func (r CallMemoryRegion) End() uint64 {
	if r.Size == 0 {
		return 0
	}
	return r.Offset + r.Size
}

// CallMemoryExpansion computes the required memory size for a CALL-family
// opcode that has both input and output memory regions. Returns the maximum
// of the two region endpoints, which is the minimum memory size required.
func CallMemoryExpansion(input, output CallMemoryRegion) uint64 {
	inEnd := input.End()
	outEnd := output.End()
	if inEnd > outEnd {
		return inEnd
	}
	return outEnd
}

// ReturnDataBuffer manages the return data from the last call. Per EIP-211,
// the return data is available via RETURNDATASIZE and RETURNDATACOPY until
// the next CALL/CREATE instruction replaces it.
type ReturnDataBuffer struct {
	data []byte
}

// NewReturnDataBuffer creates an empty return data buffer.
func NewReturnDataBuffer() *ReturnDataBuffer {
	return &ReturnDataBuffer{}
}

// Set replaces the return data with a copy of the given bytes.
func (rdb *ReturnDataBuffer) Set(data []byte) {
	if len(data) == 0 {
		rdb.data = nil
		return
	}
	rdb.data = make([]byte, len(data))
	copy(rdb.data, data)
}

// Data returns the current return data. May be nil.
func (rdb *ReturnDataBuffer) Data() []byte {
	return rdb.data
}

// Size returns the length of the current return data.
func (rdb *ReturnDataBuffer) Size() uint64 {
	return uint64(len(rdb.data))
}

// Slice returns a copy of return data[offset:offset+size]. Returns
// ErrReturnDataOutOfBounds if the range exceeds the available data.
func (rdb *ReturnDataBuffer) Slice(offset, size uint64) ([]byte, error) {
	if size == 0 {
		return nil, nil
	}
	end := offset + size
	if end < offset || end > uint64(len(rdb.data)) {
		return nil, ErrReturnDataOutOfBounds
	}
	out := make([]byte, size)
	copy(out, rdb.data[offset:end])
	return out, nil
}

// Clear resets the return data buffer.
func (rdb *ReturnDataBuffer) Clear() {
	rdb.data = nil
}
