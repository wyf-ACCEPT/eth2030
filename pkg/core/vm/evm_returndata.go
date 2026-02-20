package vm

// evm_returndata.go implements return data buffer management for the EVM.
// Per EIP-211, the return data from the last CALL/CREATE operation is
// available via RETURNDATASIZE and RETURNDATACOPY until the next such
// operation replaces it. This file provides the ReturnDataManager which
// tracks the return data lifecycle and implements the opcode handlers.

import (
	"errors"
	"math/big"
)

// Return data errors.
var (
	ErrReturnDataCopyOutOfBounds = errors.New("return data: copy offset + size exceeds buffer length")
	ErrReturnDataSizeOverflow    = errors.New("return data: offset + size overflow")
)

// ReturnDataManager manages the return data buffer lifecycle across call
// frames. Each CALL/CREATE sets new return data; RETURNDATASIZE and
// RETURNDATACOPY read it. The buffer is cleared on each new CALL/CREATE
// to prevent stale data from being accessed.
type ReturnDataManager struct {
	data []byte
}

// NewReturnDataManager creates a new empty ReturnDataManager.
func NewReturnDataManager() *ReturnDataManager {
	return &ReturnDataManager{}
}

// SetReturnData replaces the current return data buffer with a copy of the
// provided data. A nil or empty input clears the buffer.
func (rdm *ReturnDataManager) SetReturnData(data []byte) {
	if len(data) == 0 {
		rdm.data = nil
		return
	}
	rdm.data = make([]byte, len(data))
	copy(rdm.data, data)
}

// ReturnData returns the current return data. May be nil.
func (rdm *ReturnDataManager) ReturnData() []byte {
	return rdm.data
}

// Size returns the length of the current return data buffer.
func (rdm *ReturnDataManager) Size() uint64 {
	return uint64(len(rdm.data))
}

// Copy reads size bytes starting at offset from the return data buffer.
// Returns ErrReturnDataCopyOutOfBounds if the requested region exceeds the
// available data, per EIP-211 (which requires a bounds check before copy).
func (rdm *ReturnDataManager) Copy(offset, size uint64) ([]byte, error) {
	if size == 0 {
		return nil, nil
	}

	end := offset + size
	if end < offset {
		return nil, ErrReturnDataSizeOverflow
	}
	if end > uint64(len(rdm.data)) {
		return nil, ErrReturnDataCopyOutOfBounds
	}

	out := make([]byte, size)
	copy(out, rdm.data[offset:end])
	return out, nil
}

// Clear resets the return data buffer to nil.
func (rdm *ReturnDataManager) Clear() {
	rdm.data = nil
}

// OpReturnDataSize implements the RETURNDATASIZE opcode (0x3d).
// It pushes the size of the return data from the last CALL/CREATE onto the
// stack. Added in Byzantium (EIP-211).
func OpReturnDataSize(rdm *ReturnDataManager, st *Stack) error {
	size := rdm.Size()
	return st.Push(new(big.Int).SetUint64(size))
}

// OpReturnDataCopy implements the RETURNDATACOPY opcode (0x3e).
// It copies data from the return data buffer into EVM memory.
// Stack: [destOffset, offset, size]
// Added in Byzantium (EIP-211).
//
// The opcode reverts the entire call frame if offset + size exceeds the
// return data buffer length. This is a critical security check that prevents
// reading beyond the buffer.
func OpReturnDataCopy(rdm *ReturnDataManager, st *Stack, mem *Memory) error {
	destOffset := st.Pop().Uint64()
	offset := st.Pop().Uint64()
	size := st.Pop().Uint64()

	if size == 0 {
		return nil
	}

	// Bounds check per EIP-211.
	data, err := rdm.Copy(offset, size)
	if err != nil {
		return err
	}

	// Write to memory (memory expansion should be handled before this call).
	mem.Set(destOffset, size, data)
	return nil
}

// ReturnDataTracker tracks return data across nested call frames. Each
// frame can have its own return data, and when a frame completes, its
// return data becomes available to the parent frame.
type ReturnDataTracker struct {
	stack []*ReturnDataManager
}

// NewReturnDataTracker creates a tracker with one initial frame.
func NewReturnDataTracker() *ReturnDataTracker {
	return &ReturnDataTracker{
		stack: []*ReturnDataManager{NewReturnDataManager()},
	}
}

// Current returns the ReturnDataManager for the current call frame.
func (rdt *ReturnDataTracker) Current() *ReturnDataManager {
	if len(rdt.stack) == 0 {
		return nil
	}
	return rdt.stack[len(rdt.stack)-1]
}

// PushFrame creates a new return data frame for a child call. The child
// starts with empty return data.
func (rdt *ReturnDataTracker) PushFrame() {
	rdt.stack = append(rdt.stack, NewReturnDataManager())
}

// PopFrame removes the current frame and sets its return data as the return
// data available to the parent frame's ReturnDataManager.
func (rdt *ReturnDataTracker) PopFrame(returnData []byte) {
	if len(rdt.stack) <= 1 {
		// Do not pop the root frame.
		rdt.stack[0].SetReturnData(returnData)
		return
	}
	// Remove current frame.
	rdt.stack = rdt.stack[:len(rdt.stack)-1]
	// Set return data on the (now current) parent frame.
	rdt.stack[len(rdt.stack)-1].SetReturnData(returnData)
}

// Depth returns the current number of frames in the tracker.
func (rdt *ReturnDataTracker) Depth() int {
	return len(rdt.stack)
}

// ReturnOutput encapsulates the return data from a RETURN or REVERT opcode.
type ReturnOutput struct {
	Data     []byte
	Reverted bool
}

// NewReturnOutput creates a ReturnOutput from the memory region specified by
// the RETURN/REVERT opcode.
func NewReturnOutput(mem *Memory, offset, size uint64) *ReturnOutput {
	if size == 0 {
		return &ReturnOutput{}
	}
	data := make([]byte, size)
	if uint64(mem.Len()) > offset {
		available := uint64(mem.Len()) - offset
		if available > size {
			available = size
		}
		copy(data, mem.Data()[offset:offset+available])
	}
	return &ReturnOutput{Data: data}
}

// NewRevertOutput creates a ReturnOutput marked as reverted.
func NewRevertOutput(mem *Memory, offset, size uint64) *ReturnOutput {
	ro := NewReturnOutput(mem, offset, size)
	ro.Reverted = true
	return ro
}

// AsError converts the return output to an error if it was a revert,
// otherwise returns nil.
func (ro *ReturnOutput) AsError() error {
	if ro.Reverted {
		return ErrExecutionReverted
	}
	return nil
}

// ValidateReturnDataCopy checks that a RETURNDATACOPY operation would not
// read beyond the return data buffer. Returns nil if the operation is valid,
// or ErrReturnDataOutOfBounds if it would exceed the buffer.
func ValidateReturnDataCopy(returnDataSize, offset, size uint64) error {
	if size == 0 {
		return nil
	}
	end := offset + size
	if end < offset {
		return ErrReturnDataOutOfBounds
	}
	if end > returnDataSize {
		return ErrReturnDataOutOfBounds
	}
	return nil
}

// CalcReturnDataCopyGas computes the gas cost for a RETURNDATACOPY operation.
// This is the copy gas (3 per 32-byte word) plus any memory expansion cost.
func CalcReturnDataCopyGas(currentMemSize, destOffset, size uint64) (uint64, bool) {
	if size == 0 {
		return 0, true
	}

	// Copy gas: 3 per word.
	words := (size + 31) / 32
	copyGas := safeMul(GasCopy, words)

	// Memory expansion.
	end := destOffset + size
	if end < destOffset {
		return 0, false // overflow
	}

	if end > currentMemSize {
		expandGas, ok := CalcMemoryExpansionGas(currentMemSize, end)
		if !ok {
			return 0, false
		}
		return safeAdd(copyGas, expandGas), true
	}
	return copyGas, true
}

// ReturnDataFromExecution processes the return data from a completed
// call/create execution. It stores the return data in the given manager
// and returns the data for further use.
func ReturnDataFromExecution(rdm *ReturnDataManager, data []byte) []byte {
	rdm.SetReturnData(data)
	return rdm.ReturnData()
}
