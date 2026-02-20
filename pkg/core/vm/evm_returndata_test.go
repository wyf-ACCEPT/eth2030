package vm

import (
	"errors"
	"math/big"
	"testing"
)

func TestReturnDataManagerSetAndGet(t *testing.T) {
	rdm := NewReturnDataManager()

	if rdm.Size() != 0 {
		t.Fatalf("initial size: got %d, want 0", rdm.Size())
	}

	data := []byte{0x01, 0x02, 0x03, 0x04}
	rdm.SetReturnData(data)

	if rdm.Size() != 4 {
		t.Fatalf("size after set: got %d, want 4", rdm.Size())
	}

	got := rdm.ReturnData()
	if len(got) != 4 {
		t.Fatalf("data length: got %d, want 4", len(got))
	}
	for i := 0; i < 4; i++ {
		if got[i] != data[i] {
			t.Fatalf("byte %d: got %x, want %x", i, got[i], data[i])
		}
	}
}

func TestReturnDataManagerSetCopiesData(t *testing.T) {
	rdm := NewReturnDataManager()
	data := []byte{0xAA, 0xBB}
	rdm.SetReturnData(data)

	// Modify original: should not affect stored data.
	data[0] = 0xFF
	got := rdm.ReturnData()
	if got[0] != 0xAA {
		t.Fatal("SetReturnData did not copy data")
	}
}

func TestReturnDataManagerSetNil(t *testing.T) {
	rdm := NewReturnDataManager()
	rdm.SetReturnData([]byte{1, 2, 3})
	rdm.SetReturnData(nil)

	if rdm.Size() != 0 {
		t.Fatalf("size after nil set: got %d, want 0", rdm.Size())
	}
	if rdm.ReturnData() != nil {
		t.Fatal("expected nil return data")
	}
}

func TestReturnDataManagerSetEmpty(t *testing.T) {
	rdm := NewReturnDataManager()
	rdm.SetReturnData([]byte{1, 2, 3})
	rdm.SetReturnData([]byte{})

	if rdm.Size() != 0 {
		t.Fatalf("size after empty set: got %d, want 0", rdm.Size())
	}
}

func TestReturnDataManagerCopy(t *testing.T) {
	rdm := NewReturnDataManager()
	rdm.SetReturnData([]byte{0x10, 0x20, 0x30, 0x40, 0x50})

	// Copy bytes [1..3).
	got, err := rdm.Copy(1, 2)
	if err != nil {
		t.Fatalf("Copy failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("copy length: got %d, want 2", len(got))
	}
	if got[0] != 0x20 || got[1] != 0x30 {
		t.Fatalf("copy content: got %x, want [20, 30]", got)
	}
}

func TestReturnDataManagerCopyZeroSize(t *testing.T) {
	rdm := NewReturnDataManager()
	rdm.SetReturnData([]byte{1, 2, 3})

	got, err := rdm.Copy(0, 0)
	if err != nil {
		t.Fatalf("zero-size copy should succeed: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for zero-size copy")
	}
}

func TestReturnDataManagerCopyOutOfBounds(t *testing.T) {
	rdm := NewReturnDataManager()
	rdm.SetReturnData([]byte{1, 2, 3})

	// Offset + size exceeds buffer.
	_, err := rdm.Copy(2, 5)
	if err == nil {
		t.Fatal("expected out-of-bounds error")
	}
	if !errors.Is(err, ErrReturnDataCopyOutOfBounds) {
		t.Fatalf("expected ErrReturnDataCopyOutOfBounds, got %v", err)
	}
}

func TestReturnDataManagerCopyOverflow(t *testing.T) {
	rdm := NewReturnDataManager()
	rdm.SetReturnData([]byte{1, 2, 3})

	// offset + size overflows uint64.
	_, err := rdm.Copy(^uint64(0), 1)
	if err == nil {
		t.Fatal("expected overflow error")
	}
	if !errors.Is(err, ErrReturnDataSizeOverflow) {
		t.Fatalf("expected ErrReturnDataSizeOverflow, got %v", err)
	}
}

func TestReturnDataManagerClear(t *testing.T) {
	rdm := NewReturnDataManager()
	rdm.SetReturnData([]byte{1, 2, 3})
	rdm.Clear()

	if rdm.Size() != 0 {
		t.Fatalf("size after clear: got %d, want 0", rdm.Size())
	}
}

func TestOpReturnDataSize(t *testing.T) {
	rdm := NewReturnDataManager()
	rdm.SetReturnData([]byte{1, 2, 3, 4, 5})

	st := NewStack()
	if err := OpReturnDataSize(rdm, st); err != nil {
		t.Fatalf("OpReturnDataSize failed: %v", err)
	}

	if st.Len() != 1 {
		t.Fatalf("stack length: got %d, want 1", st.Len())
	}
	if st.Peek().Uint64() != 5 {
		t.Fatalf("RETURNDATASIZE: got %d, want 5", st.Peek().Uint64())
	}
}

func TestOpReturnDataSizeEmpty(t *testing.T) {
	rdm := NewReturnDataManager()
	st := NewStack()
	if err := OpReturnDataSize(rdm, st); err != nil {
		t.Fatalf("OpReturnDataSize failed: %v", err)
	}
	if st.Peek().Uint64() != 0 {
		t.Fatalf("expected 0 for empty buffer, got %d", st.Peek().Uint64())
	}
}

func TestOpReturnDataCopy(t *testing.T) {
	rdm := NewReturnDataManager()
	rdm.SetReturnData([]byte{0xAA, 0xBB, 0xCC, 0xDD})

	mem := NewMemory()
	mem.Resize(64)
	st := NewStack()

	// Stack: [destOffset=0, offset=1, size=2]
	st.Push(big.NewInt(2))  // size
	st.Push(big.NewInt(1))  // offset
	st.Push(big.NewInt(0))  // destOffset

	if err := OpReturnDataCopy(rdm, st, mem); err != nil {
		t.Fatalf("OpReturnDataCopy failed: %v", err)
	}

	data := mem.Data()
	if data[0] != 0xBB || data[1] != 0xCC {
		t.Fatalf("memory content: got [%x, %x], want [BB, CC]", data[0], data[1])
	}
}

func TestOpReturnDataCopyZeroSize(t *testing.T) {
	rdm := NewReturnDataManager()
	rdm.SetReturnData([]byte{1, 2, 3})

	mem := NewMemory()
	mem.Resize(32)
	st := NewStack()

	// size=0
	st.Push(big.NewInt(0)) // size
	st.Push(big.NewInt(0)) // offset
	st.Push(big.NewInt(0)) // destOffset

	if err := OpReturnDataCopy(rdm, st, mem); err != nil {
		t.Fatalf("zero-size copy should succeed: %v", err)
	}
}

func TestOpReturnDataCopyOutOfBounds(t *testing.T) {
	rdm := NewReturnDataManager()
	rdm.SetReturnData([]byte{1, 2, 3})

	mem := NewMemory()
	mem.Resize(32)
	st := NewStack()

	// offset=2, size=5 exceeds buffer (3 bytes).
	st.Push(big.NewInt(5)) // size
	st.Push(big.NewInt(2)) // offset
	st.Push(big.NewInt(0)) // destOffset

	err := OpReturnDataCopy(rdm, st, mem)
	if err == nil {
		t.Fatal("expected out-of-bounds error")
	}
}

func TestReturnDataTracker(t *testing.T) {
	rdt := NewReturnDataTracker()
	if rdt.Depth() != 1 {
		t.Fatalf("initial depth: got %d, want 1", rdt.Depth())
	}

	// Set return data in root frame.
	rdt.Current().SetReturnData([]byte{0x11, 0x22})
	if rdt.Current().Size() != 2 {
		t.Fatal("expected size 2 in root frame")
	}

	// Push a child frame.
	rdt.PushFrame()
	if rdt.Depth() != 2 {
		t.Fatalf("depth after push: got %d, want 2", rdt.Depth())
	}
	if rdt.Current().Size() != 0 {
		t.Fatal("child frame should start empty")
	}

	// Pop frame with return data. Parent gets the child's data.
	rdt.PopFrame([]byte{0x33, 0x44, 0x55})
	if rdt.Depth() != 1 {
		t.Fatalf("depth after pop: got %d, want 1", rdt.Depth())
	}
	if rdt.Current().Size() != 3 {
		t.Fatalf("parent should have child's return data: size %d", rdt.Current().Size())
	}
}

func TestReturnDataTrackerPopRoot(t *testing.T) {
	rdt := NewReturnDataTracker()

	// Popping the root frame should not panic; it sets data on root.
	rdt.PopFrame([]byte{0xAA})
	if rdt.Depth() != 1 {
		t.Fatal("should not pop below 1")
	}
	if rdt.Current().Size() != 1 {
		t.Fatal("expected data set on root after pop")
	}
}

func TestReturnDataTrackerNestedFrames(t *testing.T) {
	rdt := NewReturnDataTracker()

	// Push 3 frames.
	rdt.PushFrame()
	rdt.PushFrame()
	rdt.PushFrame()
	if rdt.Depth() != 4 {
		t.Fatalf("depth: got %d, want 4", rdt.Depth())
	}

	// Pop all with data.
	rdt.PopFrame([]byte{0x01})
	if rdt.Depth() != 3 {
		t.Fatalf("depth: got %d, want 3", rdt.Depth())
	}
	rdt.PopFrame([]byte{0x02})
	rdt.PopFrame([]byte{0x03})
	if rdt.Depth() != 1 {
		t.Fatalf("depth: got %d, want 1", rdt.Depth())
	}
}

func TestNewReturnOutput(t *testing.T) {
	mem := NewMemory()
	mem.Resize(64)
	mem.Set(0, 4, []byte{0x11, 0x22, 0x33, 0x44})

	ro := NewReturnOutput(mem, 0, 4)
	if len(ro.Data) != 4 {
		t.Fatalf("data length: got %d, want 4", len(ro.Data))
	}
	if ro.Reverted {
		t.Fatal("should not be reverted")
	}
	if ro.AsError() != nil {
		t.Fatal("expected nil error for non-reverted")
	}
}

func TestNewRevertOutput(t *testing.T) {
	mem := NewMemory()
	mem.Resize(64)
	mem.Set(0, 4, []byte{0x55, 0x66, 0x77, 0x88})

	ro := NewRevertOutput(mem, 0, 4)
	if !ro.Reverted {
		t.Fatal("should be reverted")
	}
	if !errors.Is(ro.AsError(), ErrExecutionReverted) {
		t.Fatal("expected ErrExecutionReverted")
	}
}

func TestNewReturnOutputZeroSize(t *testing.T) {
	mem := NewMemory()
	ro := NewReturnOutput(mem, 0, 0)
	if len(ro.Data) != 0 {
		t.Fatal("expected empty data for zero size")
	}
}

func TestValidateReturnDataCopy(t *testing.T) {
	// Valid copy.
	if err := ValidateReturnDataCopy(10, 0, 10); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}

	// Exact boundary.
	if err := ValidateReturnDataCopy(10, 5, 5); err != nil {
		t.Fatalf("expected valid at boundary, got %v", err)
	}

	// Zero size.
	if err := ValidateReturnDataCopy(0, 0, 0); err != nil {
		t.Fatalf("expected valid for zero size, got %v", err)
	}

	// Out of bounds.
	if err := ValidateReturnDataCopy(10, 5, 6); err == nil {
		t.Fatal("expected out-of-bounds error")
	}

	// Overflow.
	if err := ValidateReturnDataCopy(10, ^uint64(0), 1); err == nil {
		t.Fatal("expected overflow error")
	}
}

func TestCalcReturnDataCopyGas(t *testing.T) {
	// Zero size.
	gas, ok := CalcReturnDataCopyGas(0, 0, 0)
	if !ok || gas != 0 {
		t.Fatalf("zero size: got (%d, %v), want (0, true)", gas, ok)
	}

	// 32 bytes into existing memory.
	gas, ok = CalcReturnDataCopyGas(64, 0, 32)
	if !ok {
		t.Fatal("expected ok")
	}
	expectedCopyGas := uint64(3) // 1 word * 3
	if gas != expectedCopyGas {
		t.Fatalf("copy gas: got %d, want %d", gas, expectedCopyGas)
	}

	// 32 bytes requiring expansion.
	gas, ok = CalcReturnDataCopyGas(0, 0, 32)
	if !ok {
		t.Fatal("expected ok with expansion")
	}
	if gas <= expectedCopyGas {
		t.Fatal("expected more gas with expansion")
	}
}

func TestCalcReturnDataCopyGasOverflow(t *testing.T) {
	_, ok := CalcReturnDataCopyGas(0, ^uint64(0), 1)
	if ok {
		t.Fatal("expected overflow failure")
	}
}

func TestReturnDataFromExecution(t *testing.T) {
	rdm := NewReturnDataManager()
	data := []byte{0x01, 0x02, 0x03}

	result := ReturnDataFromExecution(rdm, data)
	if len(result) != 3 {
		t.Fatalf("expected 3 bytes, got %d", len(result))
	}
	if rdm.Size() != 3 {
		t.Fatalf("manager size: got %d, want 3", rdm.Size())
	}

	// nil return data.
	result = ReturnDataFromExecution(rdm, nil)
	if result != nil {
		t.Fatal("expected nil")
	}
	if rdm.Size() != 0 {
		t.Fatalf("manager size: got %d, want 0", rdm.Size())
	}
}
