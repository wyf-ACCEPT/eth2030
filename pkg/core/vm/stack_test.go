package vm

import (
	"math/big"
	"testing"
)

func TestStackPushPop(t *testing.T) {
	st := NewStack()
	st.Push(big.NewInt(42))
	st.Push(big.NewInt(99))

	if st.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", st.Len())
	}

	val := st.Pop()
	if val.Int64() != 99 {
		t.Errorf("Pop() = %d, want 99", val.Int64())
	}

	val = st.Pop()
	if val.Int64() != 42 {
		t.Errorf("Pop() = %d, want 42", val.Int64())
	}

	if st.Len() != 0 {
		t.Errorf("Len() = %d, want 0", st.Len())
	}
}

func TestStackPeek(t *testing.T) {
	st := NewStack()
	st.Push(big.NewInt(10))
	st.Push(big.NewInt(20))
	st.Push(big.NewInt(30))

	if st.Peek().Int64() != 30 {
		t.Errorf("Peek() = %d, want 30", st.Peek().Int64())
	}
	if st.PeekN(0).Int64() != 30 {
		t.Errorf("PeekN(0) = %d, want 30", st.PeekN(0).Int64())
	}
	if st.PeekN(1).Int64() != 20 {
		t.Errorf("PeekN(1) = %d, want 20", st.PeekN(1).Int64())
	}
	if st.PeekN(2).Int64() != 10 {
		t.Errorf("PeekN(2) = %d, want 10", st.PeekN(2).Int64())
	}
}

func TestStackBack(t *testing.T) {
	st := NewStack()
	st.Push(big.NewInt(1))
	st.Push(big.NewInt(2))
	st.Push(big.NewInt(3))

	if st.Back(0).Int64() != 3 {
		t.Errorf("Back(0) = %d, want 3", st.Back(0).Int64())
	}
	if st.Back(2).Int64() != 1 {
		t.Errorf("Back(2) = %d, want 1", st.Back(2).Int64())
	}
}

func TestStackDup(t *testing.T) {
	st := NewStack()
	st.Push(big.NewInt(10))
	st.Push(big.NewInt(20))
	st.Push(big.NewInt(30))

	st.Dup(2) // duplicate the 2nd from top (20)
	if st.Len() != 4 {
		t.Fatalf("Len() = %d, want 4", st.Len())
	}
	if st.Peek().Int64() != 20 {
		t.Errorf("after Dup(2), top = %d, want 20", st.Peek().Int64())
	}

	// Original should not be affected by modifying the dup
	st.Peek().SetInt64(999)
	if st.PeekN(2).Int64() != 20 {
		t.Errorf("Dup should create independent copy")
	}
}

func TestStackSwap(t *testing.T) {
	st := NewStack()
	st.Push(big.NewInt(1))
	st.Push(big.NewInt(2))
	st.Push(big.NewInt(3))

	st.Swap(2) // swap top (3) with 2nd below (1)
	if st.Peek().Int64() != 1 {
		t.Errorf("after Swap(2), top = %d, want 1", st.Peek().Int64())
	}
	if st.PeekN(2).Int64() != 3 {
		t.Errorf("after Swap(2), bottom = %d, want 3", st.PeekN(2).Int64())
	}
}

func TestStackOverflow(t *testing.T) {
	st := NewStack()
	for i := 0; i < 1024; i++ {
		if err := st.Push(big.NewInt(int64(i))); err != nil {
			t.Fatalf("Push(%d) failed: %v", i, err)
		}
	}
	if err := st.Push(big.NewInt(9999)); err == nil {
		t.Error("expected stack overflow error, got nil")
	}
}
