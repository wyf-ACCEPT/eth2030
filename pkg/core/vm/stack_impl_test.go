package vm

import (
	"errors"
	"math/big"
	"testing"
)

func TestEVMStack_PushPop(t *testing.T) {
	s := NewEVMStack()

	if err := s.Push(big.NewInt(10)); err != nil {
		t.Fatalf("Push(10): %v", err)
	}
	if err := s.Push(big.NewInt(20)); err != nil {
		t.Fatalf("Push(20): %v", err)
	}
	if s.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", s.Len())
	}

	val, err := s.Pop()
	if err != nil {
		t.Fatalf("Pop: %v", err)
	}
	if val.Int64() != 20 {
		t.Errorf("Pop() = %d, want 20", val.Int64())
	}

	val, err = s.Pop()
	if err != nil {
		t.Fatalf("Pop: %v", err)
	}
	if val.Int64() != 10 {
		t.Errorf("Pop() = %d, want 10", val.Int64())
	}

	if s.Len() != 0 {
		t.Errorf("Len() = %d, want 0 after popping all", s.Len())
	}
}

func TestEVMStack_PopUnderflow(t *testing.T) {
	s := NewEVMStack()
	_, err := s.Pop()
	if err == nil {
		t.Fatal("expected underflow error")
	}
	if !errors.Is(err, ErrEVMStackUnderflow) {
		t.Errorf("got %v, want ErrEVMStackUnderflow", err)
	}
}

func TestEVMStack_Peek(t *testing.T) {
	s := NewEVMStack()

	// Peek on empty stack.
	_, err := s.Peek()
	if !errors.Is(err, ErrEVMStackUnderflow) {
		t.Errorf("Peek on empty: got %v, want ErrEVMStackUnderflow", err)
	}

	s.Push(big.NewInt(42))
	s.Push(big.NewInt(99))

	val, err := s.Peek()
	if err != nil {
		t.Fatalf("Peek: %v", err)
	}
	if val.Int64() != 99 {
		t.Errorf("Peek() = %d, want 99", val.Int64())
	}
	// Peek should not remove the element.
	if s.Len() != 2 {
		t.Errorf("Len() = %d after Peek, want 2", s.Len())
	}
}

func TestEVMStack_Overflow(t *testing.T) {
	s := NewEVMStack()

	for i := 0; i < 1024; i++ {
		if err := s.Push(big.NewInt(int64(i))); err != nil {
			t.Fatalf("Push(%d): %v", i, err)
		}
	}

	if s.Len() != 1024 {
		t.Fatalf("Len() = %d, want 1024", s.Len())
	}

	err := s.Push(big.NewInt(9999))
	if err == nil {
		t.Fatal("expected overflow error")
	}
	if !errors.Is(err, ErrEVMStackOverflow) {
		t.Errorf("got %v, want ErrEVMStackOverflow", err)
	}
}

func TestEVMStack_Swap(t *testing.T) {
	s := NewEVMStack()
	// Push values: bottom [1, 2, 3, 4, 5] top
	for i := 1; i <= 5; i++ {
		s.Push(big.NewInt(int64(i)))
	}

	// SWAP1: swap top (5) with 1 below (4)
	if err := s.Swap(1); err != nil {
		t.Fatalf("Swap(1): %v", err)
	}
	top, _ := s.Peek()
	if top.Int64() != 4 {
		t.Errorf("after Swap(1), top = %d, want 4", top.Int64())
	}

	// SWAP4: swap top (4) with 4 below (1)
	if err := s.Swap(4); err != nil {
		t.Fatalf("Swap(4): %v", err)
	}
	top, _ = s.Peek()
	if top.Int64() != 1 {
		t.Errorf("after Swap(4), top = %d, want 1", top.Int64())
	}
}

func TestEVMStack_SwapErrors(t *testing.T) {
	s := NewEVMStack()
	s.Push(big.NewInt(1))

	// Swap with only 1 element (need at least 2 for SWAP1).
	err := s.Swap(1)
	if !errors.Is(err, ErrEVMStackUnderflow) {
		t.Errorf("Swap(1) with 1 element: got %v, want ErrEVMStackUnderflow", err)
	}

	// Out of range.
	s.Push(big.NewInt(2))
	err = s.Swap(0)
	if !errors.Is(err, ErrEVMSwapOutOfRange) {
		t.Errorf("Swap(0): got %v, want ErrEVMSwapOutOfRange", err)
	}
	err = s.Swap(17)
	if !errors.Is(err, ErrEVMSwapOutOfRange) {
		t.Errorf("Swap(17): got %v, want ErrEVMSwapOutOfRange", err)
	}
}

func TestEVMStack_Dup(t *testing.T) {
	s := NewEVMStack()
	// Push [10, 20, 30] (top = 30)
	s.Push(big.NewInt(10))
	s.Push(big.NewInt(20))
	s.Push(big.NewInt(30))

	// DUP1: duplicate top (30)
	if err := s.Dup(1); err != nil {
		t.Fatalf("Dup(1): %v", err)
	}
	if s.Len() != 4 {
		t.Fatalf("Len() = %d after Dup(1), want 4", s.Len())
	}
	top, _ := s.Peek()
	if top.Int64() != 30 {
		t.Errorf("after Dup(1), top = %d, want 30", top.Int64())
	}

	// Modifying the dup should not affect the original.
	top.SetInt64(999)
	s.Pop() // remove the modified dup
	orig, _ := s.Peek()
	if orig.Int64() != 30 {
		t.Errorf("original should be 30, got %d (alias issue)", orig.Int64())
	}
}

func TestEVMStack_DupDeep(t *testing.T) {
	s := NewEVMStack()
	// Push [1, 2, 3, 4, 5] (top = 5)
	for i := 1; i <= 5; i++ {
		s.Push(big.NewInt(int64(i)))
	}

	// DUP5: duplicate 5th from top (which is 1)
	if err := s.Dup(5); err != nil {
		t.Fatalf("Dup(5): %v", err)
	}
	top, _ := s.Peek()
	if top.Int64() != 1 {
		t.Errorf("after Dup(5), top = %d, want 1", top.Int64())
	}
	if s.Len() != 6 {
		t.Errorf("Len() = %d, want 6", s.Len())
	}
}

func TestEVMStack_DupErrors(t *testing.T) {
	s := NewEVMStack()

	// DUP on empty stack.
	err := s.Dup(1)
	if !errors.Is(err, ErrEVMStackUnderflow) {
		t.Errorf("Dup(1) on empty: got %v, want ErrEVMStackUnderflow", err)
	}

	// Out of range.
	s.Push(big.NewInt(1))
	err = s.Dup(0)
	if !errors.Is(err, ErrEVMDupOutOfRange) {
		t.Errorf("Dup(0): got %v, want ErrEVMDupOutOfRange", err)
	}
	err = s.Dup(17)
	if !errors.Is(err, ErrEVMDupOutOfRange) {
		t.Errorf("Dup(17): got %v, want ErrEVMDupOutOfRange", err)
	}
}

func TestEVMStack_DupOverflow(t *testing.T) {
	s := NewEVMStack()
	// Fill the stack to capacity.
	for i := 0; i < 1024; i++ {
		s.Push(big.NewInt(int64(i)))
	}

	err := s.Dup(1)
	if !errors.Is(err, ErrEVMStackOverflow) {
		t.Errorf("Dup on full stack: got %v, want ErrEVMStackOverflow", err)
	}
}

func TestEVMStack_Reset(t *testing.T) {
	s := NewEVMStack()
	s.Push(big.NewInt(1))
	s.Push(big.NewInt(2))
	s.Push(big.NewInt(3))

	s.Reset()
	if s.Len() != 0 {
		t.Errorf("Len() after Reset = %d, want 0", s.Len())
	}

	// Should be usable again after reset.
	if err := s.Push(big.NewInt(42)); err != nil {
		t.Fatalf("Push after Reset: %v", err)
	}
	val, _ := s.Peek()
	if val.Int64() != 42 {
		t.Errorf("Peek after Reset+Push = %d, want 42", val.Int64())
	}
}

func TestEVMStack_PushCopiesValue(t *testing.T) {
	s := NewEVMStack()

	original := big.NewInt(100)
	s.Push(original)

	// Modifying the original should not affect the stack.
	original.SetInt64(999)

	val, _ := s.Peek()
	if val.Int64() != 100 {
		t.Errorf("stack value changed after modifying original: got %d, want 100", val.Int64())
	}
}

func TestEVMStack_Swap16WithEnoughElements(t *testing.T) {
	s := NewEVMStack()
	// Push 17 elements (need n+1 = 17 for SWAP16).
	for i := 0; i < 17; i++ {
		s.Push(big.NewInt(int64(i)))
	}

	// SWAP16: swap top (16) with 16th below (0).
	if err := s.Swap(16); err != nil {
		t.Fatalf("Swap(16): %v", err)
	}
	top, _ := s.Peek()
	if top.Int64() != 0 {
		t.Errorf("after Swap(16), top = %d, want 0", top.Int64())
	}
}

func TestEVMStack_Dup16WithEnoughElements(t *testing.T) {
	s := NewEVMStack()
	// Push 16 elements.
	for i := 1; i <= 16; i++ {
		s.Push(big.NewInt(int64(i)))
	}

	// DUP16: duplicate 16th from top (which is 1).
	if err := s.Dup(16); err != nil {
		t.Fatalf("Dup(16): %v", err)
	}
	if s.Len() != 17 {
		t.Fatalf("Len() = %d, want 17", s.Len())
	}
	top, _ := s.Peek()
	if top.Int64() != 1 {
		t.Errorf("after Dup(16), top = %d, want 1", top.Int64())
	}
}

func TestEVMStack_LargeValues(t *testing.T) {
	s := NewEVMStack()

	// uint256 max: 2^256 - 1
	maxU256 := new(big.Int).Sub(
		new(big.Int).Lsh(big.NewInt(1), 256),
		big.NewInt(1),
	)

	if err := s.Push(maxU256); err != nil {
		t.Fatalf("Push maxU256: %v", err)
	}

	val, err := s.Pop()
	if err != nil {
		t.Fatalf("Pop: %v", err)
	}
	if val.Cmp(maxU256) != 0 {
		t.Errorf("popped value differs from pushed maxU256")
	}
}
