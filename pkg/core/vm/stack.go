package vm

import (
	"fmt"
	"math/big"
)

const stackLimit = 1024

// Stack is the EVM operand stack (max 1024 items, 256-bit words).
type Stack struct {
	data []*big.Int
}

// NewStack returns a new empty stack.
func NewStack() *Stack {
	return &Stack{data: make([]*big.Int, 0, 16)}
}

// Push pushes a value onto the stack.
func (st *Stack) Push(val *big.Int) error {
	if len(st.data) >= stackLimit {
		return fmt.Errorf("stack overflow")
	}
	st.data = append(st.data, val)
	return nil
}

// Pop removes and returns the top element.
func (st *Stack) Pop() *big.Int {
	ret := st.data[len(st.data)-1]
	st.data = st.data[:len(st.data)-1]
	return ret
}

// Peek returns the top element without removing it.
func (st *Stack) Peek() *big.Int {
	return st.data[len(st.data)-1]
}

// PeekN returns the nth element from the top (0-indexed: 0 = top).
func (st *Stack) PeekN(n int) *big.Int {
	return st.data[len(st.data)-1-n]
}

// Back returns the nth element from the top (0-indexed: 0 = top).
func (st *Stack) Back(n int) *big.Int {
	return st.data[len(st.data)-1-n]
}

// Swap swaps the top element with the nth element from the top.
func (st *Stack) Swap(n int) {
	top := len(st.data) - 1
	st.data[top], st.data[top-n] = st.data[top-n], st.data[top]
}

// Dup duplicates the nth element from the top and pushes it.
func (st *Stack) Dup(n int) {
	val := new(big.Int).Set(st.data[len(st.data)-n])
	st.data = append(st.data, val)
}

// Len returns the number of items on the stack.
func (st *Stack) Len() int {
	return len(st.data)
}

// Data returns the underlying stack slice (bottom to top).
func (st *Stack) Data() []*big.Int {
	return st.data
}
